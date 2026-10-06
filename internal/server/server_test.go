package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"q3vigilai/internal/ai"
	"q3vigilai/internal/chat"
	"q3vigilai/internal/fetch"
	"q3vigilai/internal/i18n"
	"q3vigilai/internal/pipeline"
	"q3vigilai/internal/store"
)

type env struct {
	t     *testing.T
	s     *Server
	ts    *httptest.Server
	web   *httptest.Server // fake news site
	st    *store.Store
	reply string
}

type stubAI struct{ e *env }

func (a stubAI) Name() string { return "stub" }
func (a stubAI) Complete(ctx context.Context, r ai.Request) (string, error) {
	return a.e.reply, nil
}
func (a stubAI) Stream(ctx context.Context, r ai.Request, on func(string)) (string, error) {
	for _, part := range strings.SplitAfter(a.e.reply, " ") {
		on(part)
	}
	return a.e.reply, nil
}

func newEnv(t *testing.T) *env {
	e := &env{t: t, reply: "OK"}
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "q3.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e.st = st
	e.web = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/feed.rss":
			fmt.Fprintf(w, `<rss><channel><item><title>Chính phủ ban hành quy định mới về thuế giá trị gia tăng</title>
<link>%s/bai/1</link><description>Mức thuế mới áp dụng từ năm sau.</description><pubDate>%s</pubDate></item></channel></rss>`,
				"http://"+r.Host, time.Now().Add(-time.Hour).Format(time.RFC1123Z))
		case "/bai/1":
			w.Write([]byte(`<html><body><article><p>` + strings.Repeat("Quy định mới về thuế giá trị gia tăng được áp dụng cho doanh nghiệp. ", 6) + `</p></article></body></html>`))
		case "/trang":
			w.Write([]byte(`<html><body><a href="/bai/1">Chính phủ ban hành quy định mới về thuế giá trị gia tăng</a></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(e.web.Close)

	fc := fetch.New()
	fc.Insecure = true
	fc.RetryDelay = time.Millisecond
	eng := &pipeline.Engine{St: st, Fetch: fc, DataDir: dir}
	notif := &pipeline.Notifier{St: st, Show: func(pipeline.Toast) {}}
	var provider ai.Provider
	e.s = &Server{St: st, Engine: eng, Fetch: fc, DataDir: dir, Version: "test", Token: NewToken(),
		Sched: &pipeline.Scheduler{Engine: eng, Notifier: notif, St: st},
		Chat:  &chat.Service{St: st, Provider: func() ai.Provider { return provider }},
		Web:   fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>Q3VigilAI</title>")}},
		AI: func() (ai.Provider, string) {
			if provider == nil {
				return nil, "Không tìm thấy AI nào"
			}
			return provider, "stub"
		},
	}
	e.s.ReloadAI = func() {
		if st.Setting("ai_provider") == "http-openai" {
			provider = stubAI{e}
		} else {
			provider = nil
		}
	}
	eng.Provider = func() ai.Provider { return provider }
	eng.Emit = e.s.Broadcast
	e.ts = httptest.NewServer(e.s.Handler())
	t.Cleanup(e.ts.Close)
	u, _ := url.Parse(e.ts.URL)
	fmt.Sscanf(u.Port(), "%d", &e.s.port)
	return e
}

// call makes an authorized API request and decodes the JSON reply.
func (e *env) call(method, path string, in any) (int, map[string]any) {
	e.t.Helper()
	var rd io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.ts.URL+path, rd)
	req.Header.Set("X-Q3-Token", e.s.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	raw, _ := io.ReadAll(resp.Body)
	json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func errCode(out map[string]any) string {
	if e, ok := out["error"].(map[string]any); ok {
		return e["code"].(string)
	}
	return ""
}

func TestAuthAndGuards(t *testing.T) {
	e := newEnv(t)
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	resp, _ := http.Get(e.ts.URL + "/api/status")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("API without token: %d", resp.StatusCode)
	}
	resp, _ = noRedirect.Get(e.ts.URL + "/auth?t=wrong")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("auth with a wrong token: %d", resp.StatusCode)
	}
	resp, _ = noRedirect.Get(e.s.URL("alerts"))
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/#/alerts" {
		t.Fatalf("auth: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "q3t" {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie: %+v", cookie)
	}
	// The redirect target is restricted to view names.
	resp, _ = noRedirect.Get(e.s.URL("") + "&go=" + url.QueryEscape("//evil.example"))
	if loc := resp.Header.Get("Location"); strings.Contains(loc, "evil") && !strings.HasPrefix(loc, "/#/") {
		t.Errorf("open redirect: %s", loc)
	}
	req, _ := http.NewRequest("GET", e.ts.URL+"/api/status", nil)
	req.AddCookie(cookie)
	if resp, _ = http.DefaultClient.Do(req); resp.StatusCode != 200 {
		t.Errorf("API with cookie: %d", resp.StatusCode)
	}

	// DNS rebinding: a request that reached us under another host name.
	req, _ = http.NewRequest("GET", e.ts.URL+"/api/status", nil)
	req.Host = "attacker.example"
	req.Header.Set("X-Q3-Token", e.s.Token)
	if resp, _ = http.DefaultClient.Do(req); resp.StatusCode != http.StatusForbidden {
		t.Errorf("foreign Host accepted: %d", resp.StatusCode)
	}
	// Cross-site write.
	req, _ = http.NewRequest("POST", e.ts.URL+"/api/alerts/mark-read", nil)
	req.Header.Set("X-Q3-Token", e.s.Token)
	req.Header.Set("Origin", "https://evil.example")
	if resp, _ = http.DefaultClient.Do(req); resp.StatusCode != http.StatusForbidden {
		t.Errorf("foreign Origin accepted: %d", resp.StatusCode)
	}

	resp, _ = http.Get(e.ts.URL + "/")
	page, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(page), "Q3VigilAI") {
		t.Errorf("index: %d", resp.StatusCode)
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") || strings.Contains(csp, "unsafe-inline") {
		t.Errorf("CSP: %q", csp)
	}
	if code, out := e.call("GET", "/api/nope", nil); code != 404 || errCode(out) != "not_found" {
		t.Errorf("unknown API path: %d %v", code, out)
	}
}

func TestTopicsAPI(t *testing.T) {
	e := newEnv(t)
	for name, in := range map[string]map[string]any{
		"topic_name":  {"name": " ", "keywords": []string{"thuế"}},
		"topic_empty": {"name": "Rỗng"},
		"topic_doc":   {"name": "X", "watched_docs": []string{"nghị định 13"}},
		"topic_field": {"name": "X", "fields": []string{"Không có"}},
		"topic_kind":  {"name": "X", "keywords": []string{"a"}, "kinds": []string{"forum"}},
	} {
		if code, out := e.call("POST", "/api/topics", in); code != 400 || errCode(out) != name {
			t.Errorf("%s: %d %v", name, code, out)
		}
	}
	code, out := e.call("POST", "/api/topics", map[string]any{"name": " Thuế ", "keywords": []string{"thuế", " Thuế ", "hóa  đơn"},
		"fields": []string{"Thuế"}, "watched_docs": []string{"13/2023/nđ-cp"}, "enabled": true, "remind_days": 5})
	if code != 200 || out["name"] != "Thuế" || len(out["keywords"].([]any)) != 2 || out["version"].(float64) != 1 {
		t.Fatalf("create: %d %v", code, out)
	}
	id := int(out["id"].(float64))
	out["keywords"] = []string{"thuế GTGT"}
	if code, out = e.call("PUT", fmt.Sprintf("/api/topics/%d", id), out); code != 200 || out["version"].(float64) != 2 {
		t.Errorf("update: %d %v", code, out)
	}
	if code, _ = e.call("PUT", "/api/topics/999", map[string]any{"name": "x", "keywords": []string{"a"}}); code != 404 {
		t.Errorf("update missing: %d", code)
	}
	_, out = e.call("GET", "/api/topics", nil)
	if len(out["topics"].([]any)) != 1 {
		t.Errorf("list: %v", out)
	}
	if code, _ = e.call("DELETE", fmt.Sprintf("/api/topics/%d", id), nil); code != 200 {
		t.Errorf("delete: %d", code)
	}
	if code, _ = e.call("GET", fmt.Sprintf("/api/topics/%d", id), nil); code != 404 {
		t.Errorf("get after delete: %d", code)
	}
}

func TestSourcesAPI(t *testing.T) {
	e := newEnv(t)
	code, out := e.call("POST", "/api/sources/test", map[string]string{"url": "https://www.facebook.com/groups/luat"})
	if code != 400 || errCode(out) != "source_blocked" {
		t.Errorf("social network accepted as a source: %d %v", code, out)
	}
	if code, out = e.call("POST", "/api/sources/test", map[string]string{"url": "https://diendan.luatvietnam.example/rss"}); errCode(out) != "source_blocked" {
		t.Errorf("forum accepted: %d %v", code, out)
	}
	code, out = e.call("POST", "/api/sources/test", map[string]string{"url": e.web.URL + "/feed.rss"})
	if code != 200 || out["connector"] != "rss" || out["count"].(float64) != 1 {
		t.Fatalf("probe feed: %d %v", code, out)
	}
	code, out = e.call("POST", "/api/sources/test", map[string]string{"url": e.web.URL + "/trang"})
	if code != 200 || out["connector"] != "htmllist" {
		t.Errorf("probe page: %d %v", code, out)
	}
	if code, out = e.call("POST", "/api/sources/test", map[string]string{"url": e.web.URL + "/missing"}); errCode(out) != "source_unreachable" {
		t.Errorf("probe 404: %d %v", code, out)
	}
	// The whitelist is restored after a probe.
	if e.s.Fetch.Allowed("127.0.0.1") {
		t.Error("probe left its host on the whitelist")
	}

	// A user cannot create a tier-1 source.
	code, out = e.call("POST", "/api/sources", map[string]any{"name": "Báo thử", "url": e.web.URL + "/feed.rss", "tier": 1})
	if code != 200 || out["tier"].(float64) != 3 || out["builtin"] != false || out["domain"] != "127.0.0.1" {
		t.Fatalf("add: %d %v", code, out)
	}
	id := int(out["id"].(float64))
	if code, out = e.call("PATCH", fmt.Sprintf("/api/sources/%d", id), map[string]any{"interval_minutes": 5}); errCode(out) != "bad_interval" {
		t.Errorf("interval below the floor: %d %v", code, out)
	}
	if code, out = e.call("PATCH", fmt.Sprintf("/api/sources/%d", id), map[string]any{"enabled": false, "interval_minutes": 120}); code != 200 ||
		out["enabled"] != false || out["interval_minutes"].(float64) != 120 {
		t.Errorf("patch: %d %v", code, out)
	}
	e.st.UpsertBuiltin(store.Source{Key: "k", Name: "Dựng sẵn", Domain: "x.vn", Tier: 3, Kind: "press", Connector: "rss", Config: "{}", IntervalMinutes: 60})
	list, _ := e.st.Sources()
	for _, src := range list {
		if src.Builtin {
			if code, out = e.call("DELETE", fmt.Sprintf("/api/sources/%d", src.ID), nil); errCode(out) != "builtin_source" {
				t.Errorf("builtin deleted: %d %v", code, out)
			}
		}
	}
	if code, _ = e.call("DELETE", fmt.Sprintf("/api/sources/%d", id), nil); code != 200 {
		t.Errorf("delete custom: %d", code)
	}
}

func TestSettingsAPI(t *testing.T) {
	e := newEnv(t)
	_, out := e.call("GET", "/api/settings", nil)
	if out["active_start"] != "07:00" || out["has_api_key"] != false {
		t.Errorf("defaults: %v", out)
	}
	if _, leaked := out["ai_api_key"]; leaked {
		t.Error("API key field exposed")
	}
	for _, bad := range []map[string]string{
		{"active_start": "7h"}, {"quiet_end": "25:00"}, {"ai_provider": "skynet"}, {"interval_press": "1"}, {"notify_info": "yes"},
		{"last_daily_at": "x"}, {"unknown": "1"}, {"ai_base_url": "ftp://x"}, {"pause_until": "mai"}, {"ai_max_calls": "abc"},
	} {
		if code, out := e.call("PUT", "/api/settings", bad); code != 400 || errCode(out) != "bad_setting" {
			t.Errorf("%v accepted: %d %v", bad, code, out)
		}
	}
	autostart := ""
	e.s.SetAutostart = func(on bool) error { autostart = fmt.Sprint(on); return nil }
	code, out := e.call("PUT", "/api/settings", map[string]string{"active_start": "08:00", "interval_press": "30", "autostart": "1",
		"ai_provider": "http-openai", "ai_api_key": "sk-secret-123", "ai_model": "m"})
	if code != 200 || out["active_start"] != "08:00" || out["has_api_key"] != true || autostart != "true" {
		t.Fatalf("put: %d %v autostart=%s", code, out, autostart)
	}
	stored := e.st.Setting("ai_api_key")
	if stored == "" || strings.Contains(stored, "sk-secret") {
		t.Errorf("API key not encrypted at rest: %q", stored)
	}
	if plain, err := ai.Unprotect(stored); err != nil || plain != "sk-secret-123" {
		t.Errorf("stored key does not decrypt: %q %v", plain, err)
	}
	_, out = e.call("GET", "/api/providers", nil)
	if out["current"] != "stub" {
		t.Errorf("provider not rebuilt after the settings change: %v", out)
	}
	_, out = e.call("POST", "/api/providers/test", nil)
	if out["ok"] != true {
		t.Errorf("provider test: %v", out)
	}
	e.call("PUT", "/api/settings", map[string]string{"ai_provider": "none"})
	if _, out = e.call("POST", "/api/providers/test", nil); out["ok"] != false || out["message"] == "" {
		t.Errorf("provider test without AI: %v", out)
	}
}

func TestScanAlertsAndChatFlow(t *testing.T) {
	e := newEnv(t)
	e.call("POST", "/api/sources", map[string]any{"name": "Báo thử", "url": e.web.URL + "/feed.rss"})
	e.call("POST", "/api/topics", map[string]any{"name": "Thuế", "keywords": []string{"thuế giá trị gia tăng"}, "enabled": true})

	// Listen to live events before starting the scan.
	req, _ := http.NewRequest("GET", e.ts.URL+"/api/events", nil)
	req.Header.Set("X-Q3-Token", e.s.Token)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan string, 50)
	go func() {
		sc := bufio.NewScanner(stream.Body)
		for sc.Scan() {
			if name, ok := strings.CutPrefix(sc.Text(), "event: "); ok {
				events <- name
			}
		}
	}()
	time.Sleep(100 * time.Millisecond)

	if code, _ := e.call("POST", "/api/scan", nil); code != http.StatusAccepted {
		t.Fatalf("scan start: %d", code)
	}
	deadline := time.After(20 * time.Second)
	seen := map[string]bool{}
	for !seen["scan.finished"] {
		select {
		case name := <-events:
			seen[name] = true
		case <-deadline:
			t.Fatalf("scan did not finish; events seen: %v", seen)
		}
	}
	if !seen["scan.started"] || !seen["alert.created"] {
		t.Errorf("live events: %v", seen)
	}

	_, out := e.call("GET", "/api/alerts", nil)
	alerts := out["alerts"].([]any)
	if len(alerts) != 1 || out["unread"].(float64) != 1 {
		t.Fatalf("alerts: %v", out)
	}
	a := alerts[0].(map[string]any)
	id := int(a["id"].(float64))
	if a["source_name"] != "Báo thử" || a["topic_name"] != "Thuế" || a["state"] != "unread" {
		t.Errorf("alert: %v", a)
	}
	if _, out = e.call("GET", "/api/alerts?q=khong-co", nil); len(out["alerts"].([]any)) != 0 {
		t.Error("query filter")
	}
	if code, out := e.call("PATCH", fmt.Sprintf("/api/alerts/%d", id), map[string]string{"state": "bogus"}); code != 400 {
		t.Errorf("bad state: %d %v", code, out)
	}
	if code, out := e.call("PATCH", fmt.Sprintf("/api/alerts/%d", id), map[string]string{"state": "pinned", "feedback": "irrelevant"}); code != 200 ||
		out["state"] != "pinned" || out["feedback"] != "irrelevant" || len(out["items"].([]any)) != 1 {
		t.Errorf("patch alert: %d %v", code, out)
	}
	_, out = e.call("GET", "/api/status", nil)
	if out["unread"].(float64) != 0 || out["scanning"] != false || out["last_scan"] == nil || out["topics"].(float64) != 1 {
		t.Errorf("status: %v", out)
	}
	if _, out = e.call("GET", "/api/scans", nil); len(out["scans"].([]any)) != 1 {
		t.Errorf("scan log: %v", out)
	}
	// Topic preview over what was collected.
	_, out = e.call("POST", "/api/topics/preview", map[string]any{"name": "Thử", "keywords": []string{"thuế"}})
	if out["matched"].(float64) != 1 || out["scanned"].(float64) != 1 {
		t.Errorf("preview: %v", out)
	}

	// Chat about the alert, with a stub model.
	e.call("PUT", "/api/settings", map[string]string{"ai_provider": "http-openai", "ai_model": "m"})
	e.reply = "Quy định mới về thuế áp dụng cho doanh nghiệp [1]."
	code, out := e.call("POST", "/api/chat/sessions", map[string]any{"scope": map[string]any{"kind": "alert", "id": id}})
	if code != 200 {
		t.Fatalf("new chat: %d %v", code, out)
	}
	sid := int(out["id"].(float64))
	if code, out = e.call("POST", "/api/chat/sessions", map[string]any{"scope": map[string]any{"kind": "web"}}); errCode(out) != "bad_scope" {
		t.Errorf("bad scope: %d %v", code, out)
	}
	b, _ := json.Marshal(map[string]string{"content": "Quy định thuế mới áp dụng cho ai?"})
	creq, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/chat/sessions/%d/messages", e.ts.URL, sid), bytes.NewReader(b))
	creq.Header.Set("X-Q3-Token", e.s.Token)
	cresp, err := http.DefaultClient.Do(creq)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(cresp.Body)
	cresp.Body.Close()
	sse := string(raw)
	if strings.Count(sse, "event: delta") < 2 || !strings.Contains(sse, "event: done") {
		t.Fatalf("chat stream:\n%s", sse)
	}
	_, out = e.call("GET", fmt.Sprintf("/api/chat/sessions/%d", sid), nil)
	msgs := out["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("chat history: %v", out)
	}
	answer := msgs[1].(map[string]any)
	cites := answer["citations"].([]any)
	if len(cites) == 0 || cites[0].(map[string]any)["used"] != true || cites[0].(map[string]any)["source"] != "Báo thử" {
		t.Errorf("citations: %v", cites)
	}
	if code, out = e.call("POST", fmt.Sprintf("/api/chat/sessions/%d/messages", sid), map[string]string{"content": "  "}); errCode(out) != "empty" {
		t.Errorf("empty question: %d %v", code, out)
	}
	if code, _ = e.call("POST", "/api/chat/sessions/999/messages", map[string]string{"content": "x"}); code != 404 {
		t.Errorf("chat in a missing session: %d", code)
	}
	if code, _ = e.call("POST", "/api/alerts/mark-read", nil); code != 200 {
		t.Errorf("mark-read: %d", code)
	}
}

func TestDocumentsAPI(t *testing.T) {
	e := newEnv(t)
	if code, out := e.call("POST", "/api/documents/fetch", map[string]string{"number": "nghị định về thuế"}); code != 400 || errCode(out) != "bad_number" {
		t.Errorf("bad number: %d %v", code, out)
	}
	dir := filepath.Join(e.s.DataDir, "docs", "1_2026_ND-CP")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "1.pdf"), []byte("%PDF-1.4 x"), 0o644)
	// A stored path that climbs out of the docs folder must never be served.
	id, _ := e.st.SaveDocument(store.Document{DocNumber: "1/2026/NĐ-CP", Title: "Thử",
		Files: []string{"docs/1_2026_ND-CP/1.pdf", "../q3.db", "docs/../q3.db"}})
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/documents/%d/file/0", e.ts.URL, id), nil)
	req.Header.Set("X-Q3-Token", e.s.Token)
	resp, _ := http.DefaultClient.Do(req)
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.HasPrefix(string(data), "%PDF-") {
		t.Errorf("file: %d %q", resp.StatusCode, data)
	}
	for _, n := range []string{"1", "2", "7", "-1", "x"} {
		if code, _ := e.call("GET", fmt.Sprintf("/api/documents/%d/file/%s", id, n), nil); code != 404 {
			t.Errorf("file %s: %d, want 404", n, code)
		}
	}
	_, out := e.call("GET", "/api/documents?q=1/2026", nil)
	if out["total"].(float64) != 1 {
		t.Errorf("list: %v", out)
	}
	_, out = e.call("GET", fmt.Sprintf("/api/documents/%d", id), nil)
	if out["document"].(map[string]any)["doc_number"] != "1/2026/NĐ-CP" {
		t.Errorf("detail: %v", out)
	}
	opened := ""
	e.s.OpenPath = func(p string) error { opened = p; return nil }
	if code, _ := e.call("POST", fmt.Sprintf("/api/documents/%d/open-folder", id), nil); code != 200 || opened != dir {
		t.Errorf("open folder: %d %q", code, opened)
	}
	if code, out := e.call("POST", "/api/backup", nil); code != 200 || out["path"] == "" {
		t.Errorf("backup: %d %v", code, out)
	} else if _, err := os.Stat(out["path"].(string)); err != nil {
		t.Errorf("backup file: %v", err)
	}
	_, out = e.call("GET", "/api/meta", nil)
	if len(out["fields"].([]any)) < 10 {
		t.Errorf("meta: %v", out)
	}
}

func TestNotifyTestEndpoint(t *testing.T) {
	e := newEnv(t)
	var shown []pipeline.Toast
	e.s.Sched.Notifier.Show = func(t pipeline.Toast) { shown = append(shown, t) }
	e.st.SetSettings(map[string]string{"quiet_start": "00:00", "quiet_end": "23:59"}) // muted all day
	if code, out := e.call("POST", "/api/notify/test", nil); code != 200 || out["sent"] != true {
		t.Fatalf("notify test: %d %v", code, out)
	}
	if len(shown) != 1 || !strings.Contains(shown[0].Title, "thông báo thử") {
		t.Errorf("the test toast must show even during quiet hours: %+v", shown)
	}
}

func TestIdleForIgnoresTheEventStream(t *testing.T) {
	e := newEnv(t)
	time.Sleep(60 * time.Millisecond)
	if d := e.s.IdleFor(); d < 50*time.Millisecond {
		t.Fatalf("a fresh server should count idle from its start: %v", d)
	}
	e.call("GET", "/api/status", nil)
	if d := e.s.IdleFor(); d > 40*time.Millisecond {
		t.Errorf("a request did not count as use: idle %v", d)
	}
	time.Sleep(60 * time.Millisecond)
	// The stream that stays open for as long as a window does is not "use".
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", e.ts.URL+"/api/events", nil)
	req.Header.Set("X-Q3-Token", e.s.Token)
	if resp, err := http.DefaultClient.Do(req); err == nil {
		defer resp.Body.Close()
	}
	if d := e.s.IdleFor(); d < 50*time.Millisecond {
		t.Errorf("opening the event stream reset the idle clock: %v", d)
	}
}

func TestLanguageSwitch(t *testing.T) {
	e := newEnv(t)
	defer i18n.SetLang(i18n.Vi)

	// Vietnamese is the default and carries no dictionary: the Vietnamese text is the key.
	_, out := e.call("GET", "/api/i18n", nil)
	if out["lang"] != "vi" || len(out["dict"].(map[string]any)) != 0 {
		t.Fatalf("default language: %v", out["lang"])
	}
	if _, out = e.call("GET", "/api/settings", nil); out["language"] != "vi" {
		t.Errorf("language setting default: %v", out["language"])
	}

	// An unsupported language is refused and nothing changes.
	if code, out := e.call("PUT", "/api/settings", map[string]string{"language": "fr"}); code != 400 || errCode(out) != "bad_setting" {
		t.Errorf("language fr accepted: %d %v", code, out)
	}
	if i18n.Lang() != i18n.Vi || e.st.Setting("language") != "vi" {
		t.Error("a refused language was applied")
	}

	// English applies at once and is remembered.
	if code, out := e.call("PUT", "/api/settings", map[string]string{"language": "en"}); code != 200 || out["language"] != "en" {
		t.Fatalf("switch to English: %d %v", code, out)
	}
	if i18n.Lang() != i18n.En || e.st.Setting("language") != "en" {
		t.Error("English was not applied or stored")
	}
	_, out = e.call("GET", "/api/i18n", nil)
	dict := out["dict"].(map[string]any)
	if out["lang"] != "en" || dict["Thoát"] != "Quit" || len(dict) < 300 {
		t.Errorf("English dictionary: lang=%v entries=%d", out["lang"], len(dict))
	}

	// API errors are worded in the chosen language, and with their arguments.
	code, out := e.call("PUT", "/api/settings", map[string]string{"unknown": "1"})
	msg := out["error"].(map[string]any)["message"]
	if code != 400 || msg != "No such setting: unknown" {
		t.Errorf("English error: %d %v", code, msg)
	}
	e.call("PUT", "/api/settings", map[string]string{"language": "vi"})
	_, out = e.call("PUT", "/api/settings", map[string]string{"unknown": "1"})
	if msg := out["error"].(map[string]any)["message"]; msg != "Không có cài đặt này: unknown" {
		t.Errorf("Vietnamese error: %v", msg)
	}
	_, out = e.call("GET", "/api/i18n", nil)
	if len(out["dict"].(map[string]any)) != 0 {
		t.Error("the Vietnamese UI was sent a dictionary it does not need")
	}
}

func TestLanguageReachesTheToasts(t *testing.T) {
	e := newEnv(t)
	defer i18n.SetLang(i18n.Vi)
	var shown []pipeline.Toast
	e.s.Sched.Notifier.Show = func(t pipeline.Toast) { shown = append(shown, t) }
	e.call("PUT", "/api/settings", map[string]string{"language": "en"})
	if code, _ := e.call("POST", "/api/notify/test", nil); code != 200 || len(shown) != 1 {
		t.Fatalf("notify test: %d %d", code, len(shown))
	}
	if shown[0].Title != "Q3VigilAI: test notification" || !strings.HasPrefix(shown[0].Body, "If you can read this") {
		t.Errorf("toast not in English: %+v", shown[0])
	}
}

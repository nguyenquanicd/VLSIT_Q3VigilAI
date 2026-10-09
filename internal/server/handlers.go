package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"q3vigilai/internal/ai"
	"q3vigilai/internal/fetch"
	"q3vigilai/internal/i18n"
	"q3vigilai/internal/pipeline"
	"q3vigilai/internal/sources"
	"q3vigilai/internal/store"
	"q3vigilai/internal/textutil"
)

// ---- status ----------------------------------------------------------------

func (s *Server) getStatus(w http.ResponseWriter, r *http.Request) error {
	provider, note := s.AI()
	name := ""
	if provider != nil {
		name = provider.Name()
	}
	var last *store.Scan
	if scans, _ := s.St.Scans(1); len(scans) > 0 {
		last = &scans[0]
	}
	failing := 0
	list, _ := s.St.Sources()
	for _, src := range list {
		if src.Enabled && (src.FailCount >= 3 || src.EmptyCount >= 3) {
			failing++
		}
	}
	topics, _ := s.St.Topics(false)
	return ok(w, map[string]any{
		"version": s.Version, "unread": s.St.UnreadCount(), "scanning": s.Engine.Running(), "last_scan": last,
		"ai":              map[string]string{"provider": name, "note": note, "error": s.Engine.AIError()},
		"sources_failing": failing, "topics": len(topics), "data_dir": s.DataDir,
		"muted": s.Sched.Notifier.Muted(), "pause_until": s.St.Setting("pause_until"),
	})
}

// getI18n gives the UI its language and, for English, the dictionary: the UI
// source text is Vietnamese, so Vietnamese needs no dictionary.
func (s *Server) getI18n(w http.ResponseWriter, r *http.Request) error {
	dict := map[string]string{}
	if i18n.Lang() == i18n.En {
		dict = i18n.Dict()
	}
	return ok(w, map[string]any{"lang": i18n.Lang(), "dict": dict})
}

func (s *Server) getMeta(w http.ResponseWriter, r *http.Request) error {
	return ok(w, map[string]any{
		"fields": pipeline.Fields(), "field_keywords": pipeline.FieldKeywords,
		"severity": pipeline.SeverityLabel, "legal_status": pipeline.StatusLabel,
	})
}

// ---- alerts ----------------------------------------------------------------

func (s *Server) listAlerts(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	f := store.AlertFilter{Severity: q.Get("severity"), Kind: q.Get("kind"), State: q.Get("state"), Query: q.Get("q"),
		TopicID: int64(qInt(r, "topic", 0)), Limit: qInt(r, "limit", 50), Offset: qInt(r, "offset", 0)}
	list, total, err := s.St.Alerts(f)
	if err != nil {
		return err
	}
	return ok(w, map[string]any{"alerts": list, "total": total, "unread": s.St.UnreadCount()})
}

func (s *Server) getAlert(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	a, err := s.St.Alert(id)
	if err != nil {
		return err
	}
	return ok(w, a)
}

func (s *Server) patchAlert(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	var in struct {
		State    *string `json:"state"`
		Feedback *string `json:"feedback"`
	}
	if err := body(r, &in); err != nil {
		return err
	}
	if in.State != nil {
		if err := s.St.SetAlertState(id, *in.State); err != nil {
			if err == store.ErrNotFound {
				return err
			}
			return bad("bad_state", "Trạng thái không hợp lệ.")
		}
	}
	if in.Feedback != nil {
		if *in.Feedback != "" && *in.Feedback != "irrelevant" {
			return bad("bad_feedback", "Phản hồi không hợp lệ.")
		}
		if err := s.St.SetAlertFeedback(id, *in.Feedback); err != nil {
			return err
		}
	}
	s.changed()
	return s.getAlert(w, r)
}

func (s *Server) markRead(w http.ResponseWriter, r *http.Request) error {
	if err := s.St.MarkAllRead(); err != nil {
		return err
	}
	s.changed()
	return ok(w, map[string]any{"unread": 0})
}

// ---- topics ----------------------------------------------------------------

func (s *Server) listTopics(w http.ResponseWriter, r *http.Request) error {
	list, err := s.St.Topics(false)
	if err != nil {
		return err
	}
	return ok(w, map[string]any{"topics": list})
}

func (s *Server) getTopic(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	t, err := s.St.Topic(id)
	if err != nil {
		return err
	}
	return ok(w, t)
}

func clean(list []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, v := range list {
		v = strings.Join(strings.Fields(v), " ")
		if k := textutil.Lower(v); v != "" && !seen[k] {
			seen[k] = true
			out = append(out, v)
		}
	}
	return out
}

var validKinds = map[string]bool{"press": true, "official": true, "draft": true}

func validateTopic(t *store.Topic) error {
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" {
		return bad("topic_name", "Chủ đề cần có tên.")
	}
	t.Keywords, t.ExcludeKeywords = clean(t.Keywords), clean(t.ExcludeKeywords)
	t.Fields, t.WatchedDocs = clean(t.Fields), clean(t.WatchedDocs)
	for _, f := range t.Fields {
		if _, known := pipeline.FieldKeywords[f]; !known {
			return bad("topic_field", "Lĩnh vực không có trong danh sách: {0}", f)
		}
	}
	for i, d := range t.WatchedDocs {
		// Numbers are stored in their usual upper-case form however they were typed.
		up := strings.ToUpper(d)
		if got := textutil.ExtractDocNumbers(up); len(got) != 1 || textutil.NormalizeDocNumber(got[0]) != textutil.NormalizeDocNumber(up) {
			return bad("topic_doc", "Số hiệu văn bản không đúng dạng (ví dụ 13/2023/NĐ-CP): {0}", d)
		}
		t.WatchedDocs[i] = up
	}
	if len(t.Keywords)+len(t.Fields)+len(t.WatchedDocs) == 0 {
		return bad("topic_empty", "Chủ đề cần ít nhất một từ khóa, một lĩnh vực hoặc một văn bản theo dõi.")
	}
	if len(t.Kinds) == 0 {
		t.Kinds = []string{"press", "official", "draft"}
	}
	for _, k := range t.Kinds {
		if !validKinds[k] {
			return bad("topic_kind", "Loại tin không hợp lệ: {0}", k)
		}
	}
	if t.RemindDays < 0 || t.RemindDays > 90 {
		return bad("topic_remind", "Số ngày nhắc trước phải từ 0 đến 90.")
	}
	return nil
}

func (s *Server) saveTopic(w http.ResponseWriter, r *http.Request) error {
	var t store.Topic
	if err := body(r, &t); err != nil {
		return err
	}
	t.ID = 0
	if r.Method == http.MethodPut {
		id, err := pathID(r)
		if err != nil {
			return err
		}
		t.ID = id
	}
	if err := validateTopic(&t); err != nil {
		return err
	}
	id, err := s.St.SaveTopic(t)
	if err != nil {
		return err
	}
	saved, err := s.St.Topic(id)
	if err != nil {
		return err
	}
	if s.TopicSaved != nil && saved.Enabled {
		s.TopicSaved(id)
	}
	return ok(w, saved)
}

func (s *Server) deleteTopic(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := s.St.DeleteTopic(id); err != nil {
		return err
	}
	s.changed()
	return ok(w, map[string]bool{"deleted": true})
}

// previewTopic runs an unsaved topic over the items of the last 30 days, so
// the user can tune keywords before saving.
func (s *Server) previewTopic(w http.ResponseWriter, r *http.Request) error {
	var t store.Topic
	if err := body(r, &t); err != nil {
		return err
	}
	if err := validateTopic(&t); err != nil {
		return err
	}
	items, err := s.St.RecentItems(time.Now().AddDate(0, 0, -30), 5000)
	if err != nil {
		return err
	}
	kinds := map[int64]string{}
	names := map[int64]string{}
	list, _ := s.St.Sources()
	for _, src := range list {
		kinds[src.ID], names[src.ID] = src.Kind, src.Name
	}
	type sample struct {
		Title    string   `json:"title"`
		Source   string   `json:"source"`
		Keywords []string `json:"keywords"`
		URL      string   `json:"url"`
	}
	samples := []sample{}
	count := 0
	for _, it := range items {
		m := pipeline.MatchTopic(t, it.SourceID, kinds[it.SourceID], it.Title+". "+it.Summary)
		if !m.OK() {
			continue
		}
		count++
		if len(samples) < 15 {
			samples = append(samples, sample{it.Title, names[it.SourceID], append(m.Keywords, m.Watched...), it.URL})
		}
	}
	return ok(w, map[string]any{"scanned": len(items), "matched": count, "samples": samples})
}

// ---- sources ---------------------------------------------------------------

func (s *Server) listSources(w http.ResponseWriter, r *http.Request) error {
	list, err := s.St.Sources()
	if err != nil {
		return err
	}
	return ok(w, map[string]any{"sources": list})
}

func (s *Server) patchSource(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	src, err := s.St.Source(id)
	if err != nil {
		return err
	}
	var in struct {
		Enabled         *bool `json:"enabled"`
		IntervalMinutes *int  `json:"interval_minutes"`
	}
	if err := body(r, &in); err != nil {
		return err
	}
	if in.Enabled != nil {
		src.Enabled = *in.Enabled
	}
	if in.IntervalMinutes != nil {
		if *in.IntervalMinutes < 15 || *in.IntervalMinutes > 7*24*60 {
			return bad("bad_interval", "Tần suất quét phải từ 15 phút đến 7 ngày.")
		}
		src.IntervalMinutes = *in.IntervalMinutes
	}
	if err := s.St.UpdateSourceUser(id, src.Enabled, src.IntervalMinutes); err != nil {
		return err
	}
	src, _ = s.St.Source(id)
	return ok(w, src)
}

func (s *Server) deleteSource(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := s.St.DeleteSource(id); err != nil {
		if err == store.ErrNotFound {
			return bad("builtin_source", "Nguồn dựng sẵn không xóa được, chỉ có thể tắt.")
		}
		return err
	}
	return ok(w, map[string]bool{"deleted": true})
}

type sourceProbe struct {
	Connector string   `json:"connector"`
	Domain    string   `json:"domain"`
	Count     int      `json:"count"`
	Sample    []string `json:"sample"`
	Config    string   `json:"-"`
}

// probeSource reads a candidate source once, as a feed first and as a
// category page second. It refuses blocked hosts and non-https addresses
// before any request leaves the machine.
func (s *Server) probeSource(ctx context.Context, raw string) (*sourceProbe, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return nil, bad("bad_url", "Địa chỉ không hợp lệ.")
	}
	if u.Scheme != "https" && !s.Fetch.Insecure {
		return nil, bad("bad_url", "Chỉ chấp nhận địa chỉ https.")
	}
	host := strings.ToLower(u.Hostname())
	if fetch.Blocked(host) {
		return nil, bad("source_blocked", "Tên miền này thuộc nhóm mạng xã hội, diễn đàn hoặc blog nên không được dùng làm nguồn.")
	}
	domain := strings.TrimPrefix(host, "www.")
	list, _ := s.St.Sources()
	s.Fetch.SetAllowed(append(sources.AllowedDomains(list), domain))
	defer func() { s.Fetch.SetAllowed(sources.AllowedDomains(list)) }()

	resp, err := s.Fetch.Get(ctx, u.String(), fetch.Options{})
	if err != nil {
		return nil, bad("source_unreachable", "Không đọc được địa chỉ này: {0}", err.Error())
	}
	p := &sourceProbe{Domain: domain}
	var refs []sources.Ref
	if feed, ferr := sources.ParseFeed(resp.Body, resp.URL); ferr == nil && len(feed) > 0 {
		p.Connector, refs = "rss", feed
		cfg, _ := json.Marshal(map[string]any{"urls": []string{u.String()}})
		p.Config = string(cfg)
	} else {
		p.Connector, refs = "htmllist", sources.ParseLinkList(resp.Body, resp.URL, nil)
		cfg, _ := json.Marshal(map[string]string{"url": u.String()})
		p.Config = string(cfg)
	}
	if len(refs) == 0 {
		return nil, bad("source_empty", "Không tìm thấy tin nào ở địa chỉ này. Hãy dùng địa chỉ RSS hoặc trang chuyên mục.")
	}
	p.Count = len(refs)
	for i := 0; i < len(refs) && i < 5; i++ {
		p.Sample = append(p.Sample, refs[i].Title)
	}
	return p, nil
}

func (s *Server) testSource(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		URL string `json:"url"`
	}
	if err := body(r, &in); err != nil {
		return err
	}
	p, err := s.probeSource(r.Context(), in.URL)
	if err != nil {
		return err
	}
	return ok(w, p)
}

func (s *Server) addSource(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Name            string `json:"name"`
		URL             string `json:"url"`
		Tier            int    `json:"tier"`
		IntervalMinutes int    `json:"interval_minutes"`
	}
	if err := body(r, &in); err != nil {
		return err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return bad("source_name", "Nguồn cần có tên.")
	}
	// A user-added source can never be tier 1: only the shipped official
	// sources carry that weight.
	if in.Tier != 2 {
		in.Tier = 3
	}
	if in.IntervalMinutes == 0 {
		in.IntervalMinutes = s.St.SettingInt("interval_press")
	}
	if in.IntervalMinutes < 15 || in.IntervalMinutes > 7*24*60 {
		return bad("bad_interval", "Tần suất quét phải từ 15 phút đến 7 ngày.")
	}
	p, err := s.probeSource(r.Context(), in.URL)
	if err != nil {
		return err
	}
	id, err := s.St.AddSource(store.Source{Name: in.Name, Domain: p.Domain, Tier: in.Tier, Kind: "press", Connector: p.Connector,
		Config: p.Config, IntervalMinutes: in.IntervalMinutes})
	if err != nil {
		return err
	}
	src, _ := s.St.Source(id)
	return ok(w, src)
}

// ---- documents -------------------------------------------------------------

func (s *Server) listDocuments(w http.ResponseWriter, r *http.Request) error {
	list, total, err := s.St.Documents(r.URL.Query().Get("q"), qInt(r, "limit", 50), qInt(r, "offset", 0))
	if err != nil {
		return err
	}
	return ok(w, map[string]any{"documents": list, "total": total})
}

func (s *Server) getDocument(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	d, err := s.St.Document(id)
	if err != nil {
		return err
	}
	alerts, _ := s.St.DocumentAlerts(id)
	return ok(w, map[string]any{"document": d, "alerts": alerts})
}

func (s *Server) fetchDocument(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Number       string `json:"number"`
		ConfirmLarge bool   `json:"confirm_large"`
	}
	if err := body(r, &in); err != nil {
		return err
	}
	nums := textutil.ExtractDocNumbers(strings.ToUpper(in.Number))
	if len(nums) != 1 {
		return bad("bad_number", "Hãy nhập đúng một số hiệu văn bản, ví dụ 13/2023/NĐ-CP.")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	d, estimate, confirmationRequired, err := s.Engine.FetchByNumber(ctx, nums[0], in.ConfirmLarge)
	if err == store.ErrNotFound {
		return apiErr(http.StatusNotFound, "not_on_portal", "Không tìm thấy văn bản số {0} trên Cổng Thông tin điện tử Chính phủ. Cổng này có thể chưa đăng, hoặc không lưu văn bản địa phương và văn bản rất cũ.", nums[0])
	}
	if err != nil {
		return bad("portal_error", "Không tra được trên cổng Chính phủ: {0}", err.Error())
	}
	if confirmationRequired {
		return ok(w, map[string]any{"confirmation_required": true, "estimate": estimate})
	}
	return ok(w, d)
}

// docFile resolves the n-th stored file of a document to a path that is
// guaranteed to lie inside the data directory.
func (s *Server) docFile(r *http.Request) (string, error) {
	id, err := pathID(r)
	if err != nil {
		return "", err
	}
	d, err := s.St.Document(id)
	if err != nil {
		return "", err
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 0 || n >= len(d.Files) {
		return "", store.ErrNotFound
	}
	root, _ := filepath.Abs(filepath.Join(s.DataDir, "docs"))
	p, _ := filepath.Abs(filepath.Join(s.DataDir, filepath.FromSlash(d.Files[n])))
	if !strings.HasPrefix(p, root+string(filepath.Separator)) {
		return "", store.ErrNotFound
	}
	if _, err := os.Stat(p); err != nil {
		return "", store.ErrNotFound
	}
	return p, nil
}

func (s *Server) getDocumentFile(w http.ResponseWriter, r *http.Request) error {
	p, err := s.docFile(r)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Disposition", "inline; filename*=UTF-8''"+url.PathEscape(filepath.Base(p)))
	// The viewer is the browser's own PDF reader, which needs looser rules
	// than the app pages.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; object-src 'self'; frame-ancestors 'self'")
	http.ServeFile(w, r, p)
	return nil
}

func (s *Server) openDocumentFolder(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	d, err := s.St.Document(id)
	if err != nil {
		return err
	}
	if len(d.Files) == 0 {
		return bad("no_files", "Văn bản này chưa có tệp nào được tải về.")
	}
	dir := filepath.Dir(filepath.Join(s.DataDir, filepath.FromSlash(d.Files[0])))
	if s.OpenPath != nil {
		if err := s.OpenPath(dir); err != nil {
			return err
		}
	}
	return ok(w, map[string]string{"path": dir})
}

// ---- scans -----------------------------------------------------------------

func (s *Server) startScan(w http.ResponseWriter, r *http.Request) error {
	if s.Engine.Running() {
		return pipeline.ErrBusy
	}
	go s.Sched.ScanNow(context.Background(), nil)
	w.WriteHeader(http.StatusAccepted)
	return ok(w, map[string]bool{"started": true})
}

func (s *Server) listScans(w http.ResponseWriter, r *http.Request) error {
	list, err := s.St.Scans(qInt(r, "limit", 100))
	if err != nil {
		return err
	}
	return ok(w, map[string]any{"scans": list})
}

// ---- chat ------------------------------------------------------------------

func (s *Server) listChats(w http.ResponseWriter, r *http.Request) error {
	list, err := s.St.ChatSessions()
	if err != nil {
		return err
	}
	return ok(w, map[string]any{"sessions": list})
}

func (s *Server) newChat(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Title string `json:"title"`
		Scope struct {
			Kind string `json:"kind"`
			ID   int64  `json:"id"`
		} `json:"scope"`
	}
	if err := body(r, &in); err != nil {
		return err
	}
	switch in.Scope.Kind {
	case "", "all":
		in.Scope.Kind, in.Scope.ID = "all", 0
	case "document", "alert":
		if in.Scope.ID <= 0 {
			return bad("bad_scope", "Phạm vi không hợp lệ.")
		}
	default:
		return bad("bad_scope", "Phạm vi không hợp lệ.")
	}
	scope, _ := json.Marshal(in.Scope)
	cs, err := s.St.NewChatSession(strings.TrimSpace(in.Title), string(scope))
	if err != nil {
		return err
	}
	return ok(w, cs)
}

func (s *Server) getChat(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	cs, err := s.St.ChatSession(id)
	if err != nil {
		return err
	}
	msgs, err := s.St.ChatMessages(id)
	if err != nil {
		return err
	}
	return ok(w, map[string]any{"session": cs, "messages": msgs})
}

func (s *Server) deleteChat(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := s.St.DeleteChatSession(id); err != nil {
		return err
	}
	return ok(w, map[string]bool{"deleted": true})
}

// postChatMessage streams the answer as server-sent events: "delta" pieces,
// then "done" with the stored message, or "error".
func (s *Server) postChatMessage(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	var in struct {
		Content string `json:"content"`
	}
	if err := body(r, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Content) == "" {
		return bad("empty", "Hãy nhập câu hỏi.")
	}
	if len([]rune(in.Content)) > 4000 {
		return bad("too_long", "Câu hỏi quá dài (tối đa 4000 ký tự).")
	}
	if _, err := s.St.ChatSession(id); err != nil {
		return err
	}
	fl, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	send := func(event string, v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		if fl != nil {
			fl.Flush()
		}
	}
	msg, err := s.Chat.Ask(r.Context(), id, in.Content, func(delta string) { send("delta", map[string]string{"text": delta}) })
	if err != nil {
		send("error", map[string]any{"message": err.Error(), "stored": msg})
		return nil
	}
	send("done", msg)
	return nil
}

// ---- settings --------------------------------------------------------------

var hiddenSettings = map[string]bool{"last_daily_at": true, "last_system_notice_at": true, "ai_api_key": true}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) error {
	all := s.St.Settings()
	out := map[string]any{"has_api_key": all["ai_api_key"] != ""}
	for k, v := range all {
		if !hiddenSettings[k] {
			out[k] = v
		}
	}
	return ok(w, out)
}

var (
	reClock   = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
	providers = map[string]bool{"auto": true, "none": true, "claude-cli": true, "codex-cli": true, "ollama": true,
		"http-openai": true, "http-anthropic": true}
	intRanges = map[string][2]int{
		"interval_press": {15, 10080}, "interval_official": {15, 10080}, "lookback_days": {1, 60}, "retention_days": {7, 365},
		"ai_max_calls": {0, 500}, "ai_timeout_seconds": {20, 600}, "verify_max_per_scan": {0, 50}, "notify_group_over": {0, 50},
	}
	flags = map[string]bool{"notify_info": true, "notify_notice": true, "notify_warning": true, "notify_system": true, "autostart": true}
)

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) error {
	var in map[string]string
	if err := body(r, &in); err != nil {
		return err
	}
	aiChanged, langChanged := false, false
	for k, v := range in {
		v = strings.TrimSpace(v)
		in[k] = v
		switch {
		case k == "ai_api_key":
			// Stored encrypted for this Windows account; never sent back.
			enc, err := ai.Protect(v)
			if err != nil {
				return err
			}
			in[k], aiChanged = enc, true
		case hiddenSettings[k]:
			return bad("bad_setting", "Không thể đặt giá trị này: {0}", k)
		case k == "active_start" || k == "active_end" || k == "quiet_start" || k == "quiet_end":
			if !reClock.MatchString(v) {
				return bad("bad_setting", "Giờ phải có dạng HH:MM (ví dụ 07:00).")
			}
		case k == "pause_until":
			if v != "" {
				if _, err := time.Parse(time.RFC3339, v); err != nil {
					return bad("bad_setting", "Thời điểm tạm dừng không hợp lệ.")
				}
			}
		case k == "ai_provider":
			if !providers[v] {
				return bad("bad_setting", "Loại AI không hợp lệ.")
			}
			aiChanged = true
		case k == "ai_base_url":
			if v != "" {
				if u, err := url.Parse(v); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
					return bad("bad_setting", "Địa chỉ máy chủ AI không hợp lệ.")
				}
			}
			aiChanged = true
		case k == "language":
			if v != i18n.Vi && v != i18n.En {
				return bad("bad_setting", "Ngôn ngữ không được hỗ trợ: {0}", v)
			}
			langChanged = true
		case k == "ai_cli_path" || k == "ai_model":
			aiChanged = true
		case flags[k]:
			if v != "0" && v != "1" {
				return bad("bad_setting", "Giá trị bật/tắt không hợp lệ: {0}", k)
			}
		default:
			rng, known := intRanges[k]
			if !known {
				return bad("bad_setting", "Không có cài đặt này: {0}", k)
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < rng[0] || n > rng[1] {
				return bad("bad_setting", "{0} phải là số từ {1} đến {2}.", k, rng[0], rng[1])
			}
			if k == "ai_timeout_seconds" {
				aiChanged = true
			}
		}
	}
	if v, has := in["autostart"]; has && s.SetAutostart != nil {
		if err := s.SetAutostart(v == "1"); err != nil {
			return bad("autostart", "Không đặt được chế độ khởi động cùng Windows: {0}", err.Error())
		}
	}
	if err := s.St.SetSettings(in); err != nil {
		return err
	}
	// A new default interval applies to every source of that kind.
	if v, has := in["interval_press"]; has {
		n, _ := strconv.Atoi(v)
		s.St.SetIntervalByKind("press", n)
	}
	if v, has := in["interval_official"]; has {
		n, _ := strconv.Atoi(v)
		s.St.SetIntervalByKind("official", n)
	}
	if langChanged {
		i18n.SetLang(in["language"])
		aiChanged = true // the provider's status text is worded in the language
	}
	if aiChanged {
		if s.ReloadAI != nil {
			s.ReloadAI()
		}
		s.Engine.ResetAI()
		s.Broadcast("provider.changed", map[string]string{})
	}
	s.changed()
	return s.getSettings(w, r)
}

func (s *Server) getProviders(w http.ResponseWriter, r *http.Request) error {
	provider, note := s.AI()
	name := ""
	if provider != nil {
		name = provider.Name()
	}
	return ok(w, map[string]any{
		"current": name, "note": note, "error": s.Engine.AIError(),
		"detected":                map[string]any{"claude_cli": ai.FindClaude(), "codex_cli": ai.FindCodex(), "ollama": ai.OllamaRunning()},
		"default_anthropic_model": ai.DefaultAnthropicModel,
	})
}

// testProvider makes one small real call so the user learns right away
// whether the configured AI works (signed in, key valid, model present).
func (s *Server) testProvider(w http.ResponseWriter, r *http.Request) error {
	provider, note := s.AI()
	if provider == nil {
		return ok(w, map[string]any{"ok": false, "message": note})
	}
	started := time.Now()
	err := ai.Check(r.Context(), provider)
	s.Engine.ReportAI(err)
	s.changed()
	if err != nil {
		return ok(w, map[string]any{"ok": false, "provider": provider.Name(), "message": err.Error()})
	}
	return ok(w, map[string]any{"ok": true, "provider": provider.Name(),
		"message": i18n.T("Kết nối tốt, phản hồi sau {0} giây.", fmt.Sprintf("%.1f", time.Since(started).Seconds()))})
}

func (s *Server) backup(w http.ResponseWriter, r *http.Request) error {
	dir := filepath.Join(s.DataDir, "backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(dir, "q3vigilai-"+time.Now().Format("20060102-150405")+".db")
	if err := s.St.Backup(dest); err != nil {
		return err
	}
	return ok(w, map[string]string{"path": dest})
}

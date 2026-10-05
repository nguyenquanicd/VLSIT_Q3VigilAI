package pipeline

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"q3vnlaw/internal/ai"
	"q3vnlaw/internal/fetch"
	"q3vnlaw/internal/sources"
	"q3vnlaw/internal/store"
)

// world is a fake web: news feeds, article pages and the Government portal.
type world struct {
	t   *testing.T
	srv *httptest.Server
	mu  sync.Mutex

	feeds    map[string][]feedItem
	portal   []portalDoc
	failFeed map[string]bool

	st      *store.Store
	eng     *Engine
	notif   *Notifier
	toasts  []Toast
	clock   time.Time
	pressID int64
	portID  int64
}

type feedItem struct {
	title, slug, desc string
	age               time.Duration // before the fake "now"
	body              string
}

type portalDoc struct {
	id, number, issued, effective, abstract string
	class                                   int
}

var vnZone = time.FixedZone("ICT", 7*3600)

func newWorld(t *testing.T) *world {
	w := &world{t: t, feeds: map[string][]feedItem{}, failFeed: map[string]bool{},
		clock: time.Date(2026, 10, 4, 10, 0, 0, 0, vnZone)}
	w.srv = httptest.NewServer(http.HandlerFunc(w.serve))
	t.Cleanup(w.srv.Close)
	store.Clock = func() time.Time { return w.clock }
	t.Cleanup(func() { store.Clock = time.Now })
	old := sources.VanbanBase
	sources.VanbanBase = w.srv.URL
	t.Cleanup(func() { sources.VanbanBase = old })

	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "q3.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	w.st = st
	fc := fetch.New()
	fc.Insecure = true
	fc.RetryDelay = time.Millisecond
	w.eng = &Engine{St: st, Fetch: fc, DataDir: dir, Now: func() time.Time { return w.clock }}
	w.notif = &Notifier{St: st, Now: func() time.Time { return w.clock }, Show: func(toast Toast) { w.toasts = append(w.toasts, toast) }}

	w.pressID, _ = st.AddSource(store.Source{Name: "Báo A", Domain: "127.0.0.1", Tier: 3, Kind: "press", Connector: "rss",
		Config: fmt.Sprintf(`{"urls":["%s/feed/a.rss"]}`, w.srv.URL), IntervalMinutes: 60})
	w.portID, _ = st.AddSource(store.Source{Name: "Cổng Chính phủ", Domain: "127.0.0.1", Tier: 1, Kind: "official", Connector: "vanban",
		Config: `{"class":1}`, IntervalMinutes: 180})
	// A daily run is not wanted in most tests; mark it as just done.
	st.SetSettings(map[string]string{"last_daily_at": store.FormatTime(w.clock)})
	return w
}

func (w *world) serve(rw http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	defer w.mu.Unlock()
	base := w.srv.URL
	row := func(d portalDoc) string {
		return fmt.Sprintf(`<tr><td><span class="code">%s</span></td><td><span class="issued-date">%s</span></td><td>
<a href="/?pageid=27160&docid=%s"><span class="substract">%s</span></a><div class="bl-doc-file"> <a href="%s/files/%s.pdf">f</a></div></td></tr>`,
			d.number, d.issued, d.id, d.abstract, base, d.id)
	}
	switch {
	case strings.HasPrefix(r.URL.Path, "/feed/"):
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/feed/"), ".rss")
		if w.failFeed[name] {
			http.Error(rw, "down", http.StatusInternalServerError)
			return
		}
		var b strings.Builder
		b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel>`)
		for _, it := range w.feeds[name] {
			fmt.Fprintf(&b, `<item><title><![CDATA[%s]]></title><link>%s/bai/%s</link><description><![CDATA[%s]]></description><pubDate>%s</pubDate></item>`,
				it.title, base, it.slug, it.desc, w.clock.Add(-it.age).Format(time.RFC1123Z))
		}
		b.WriteString(`</channel></rss>`)
		rw.Header().Set("Content-Type", "application/xml")
		rw.Write([]byte(b.String()))
	case strings.HasPrefix(r.URL.Path, "/bai/"):
		slug := strings.TrimPrefix(r.URL.Path, "/bai/")
		for _, items := range w.feeds {
			for _, it := range items {
				if it.slug == slug {
					fmt.Fprintf(rw, `<html><body><nav><p>Trang chủ</p></nav><article><h1>%s</h1><p>%s</p><p>%s</p></article></body></html>`,
						it.title, it.desc, it.body)
					return
				}
			}
		}
		http.NotFound(rw, r)
	case r.URL.Path == "/he-thong-van-ban":
		class := 1
		if r.URL.Query().Get("classid") == "2" {
			class = 2
		}
		q := ""
		if r.Method == http.MethodPost {
			r.ParseForm()
			q = strings.ToLower(r.PostForm.Get("ctrl_1_1$txtSearchKeyword"))
		}
		var b strings.Builder
		b.WriteString(`<form><input type="hidden" name="__VIEWSTATE" value="v" /><input name="ctrl_1_1$txtSearchKeyword" /><table>`)
		for _, d := range w.portal {
			if d.class != class {
				continue
			}
			if q == "" || strings.Contains(strings.ToLower(d.number+" "+d.abstract), q) {
				b.WriteString(row(d))
			}
		}
		b.WriteString(`</table></form>`)
		rw.Write([]byte(b.String()))
	case r.URL.Path == "/" && r.URL.Query().Get("docid") != "":
		for _, d := range w.portal {
			if d.id == r.URL.Query().Get("docid") {
				eff := ""
				if d.effective != "" {
					eff = fmt.Sprintf(`<tr id="c_tr_ngaycohieuluc"><td class="col1">Ngày có hiệu lực</td><td>%s</td></tr>`, d.effective)
				}
				fmt.Fprintf(rw, `<h4 class="title"><span>Nghị định số %s: %s</span></h4><table>
<tr><td class="col1">Số ký hiệu</td><td>%s</td></tr><tr><td class="col1">Ngày ban hành</td><td>%s</td></tr>%s
<tr><td class="col1">Loại văn bản</td><td>Nghị định</td></tr><tr><td class="col1">Cơ quan ban hành</td><td>Chính phủ</td></tr>
<tr><td class="col1">Trích yếu</td><td>%s</td></tr>
<tr><td class="col1">Tài liệu đính kèm</td><td><a href="%s/files/%s.pdf" class="view-file">f</a></td></tr></table>`,
					d.number, d.abstract, d.number, strings.ReplaceAll(d.issued, "/", "-"), eff, d.abstract, base, d.id)
				return
			}
		}
		http.NotFound(rw, r)
	case strings.HasPrefix(r.URL.Path, "/files/"):
		rw.Write([]byte("%PDF-1.4 scanned, no text layer"))
	default:
		http.NotFound(rw, r)
	}
}

func (w *world) topic(tp store.Topic) store.Topic {
	w.t.Helper()
	if tp.Name == "" {
		tp.Name = "Thuế"
	}
	tp.Enabled = true
	if tp.RemindDays == 0 {
		tp.RemindDays = 7
	}
	id, err := w.st.SaveTopic(tp)
	if err != nil {
		w.t.Fatal(err)
	}
	out, _ := w.st.Topic(id)
	return out
}

func (w *world) scan() store.Scan {
	w.t.Helper()
	sc, err := w.eng.Scan(context.Background(), "manual", nil)
	if err != nil {
		w.t.Fatal(err)
	}
	return sc
}

func (w *world) alerts(state string) []store.Alert {
	list, _, err := w.st.Alerts(store.AlertFilter{State: state, Limit: 100})
	if err != nil {
		w.t.Fatal(err)
	}
	return list
}

func byTitle(list []store.Alert, part string) *store.Alert {
	for i := range list {
		if strings.Contains(list[i].Title, part) {
			return &list[i]
		}
	}
	return nil
}

// fakeAI answers the classifier from a script keyed by a substring of the title.
type fakeAI struct {
	mu      sync.Mutex
	calls   int
	replies map[string][]string // title substring -> successive replies
	err     error
}

func (f *fakeAI) Name() string { return "fake" }
func (f *fakeAI) Complete(ctx context.Context, r ai.Request) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return "", f.err
	}
	for key, list := range f.replies {
		if strings.Contains(r.Prompt, key) && len(list) > 0 {
			f.replies[key] = list[1:]
			if len(list) == 1 {
				f.replies[key] = list // the last reply repeats
			}
			return list[0], nil
		}
	}
	return `{"relevant": false, "legal_status": "other", "reason": "không có kịch bản"}`, nil
}
func (f *fakeAI) Stream(ctx context.Context, r ai.Request, on func(string)) (string, error) {
	s, err := f.Complete(ctx, r)
	on(s)
	return s, err
}

const (
	decreeTitle   = "Chính phủ ban hành Nghị định 381/2026/NĐ-CP về hóa đơn điện tử"
	proposalTitle = "Bộ Tài chính đề xuất tăng thuế tiêu thụ đặc biệt với đồ uống có đường"
	crimeTitle    = "Bắt nghi phạm cướp tiệm vàng sau 6 giờ gây án"
	oldTitle      = "Quy định về thuế đã được nói tới tháng trước"
)

func standardFeed() []feedItem {
	return []feedItem{
		{title: decreeTitle, slug: "nd381", age: time.Hour,
			desc: "Nghị định 381/2026/NĐ-CP quy định về hóa đơn điện tử, có hiệu lực từ ngày 01/12/2026.",
			body: strings.Repeat("Theo nghị định, doanh nghiệp phải sử dụng hóa đơn điện tử có mã của cơ quan thuế cho mọi giao dịch bán hàng. ", 4)},
		{title: crimeTitle, slug: "cuop", age: 2 * time.Hour, desc: "Công an đã bắt giữ nghi phạm.", body: "Chi tiết vụ án."},
		{title: proposalTitle, slug: "dexuat", age: 3 * time.Hour, desc: "Đề xuất áp thuế 10% từ năm 2028.",
			body: strings.Repeat("Bộ Tài chính cho rằng cần tăng thuế để bảo vệ sức khỏe cộng đồng và đang lấy ý kiến. ", 4)},
		{title: oldTitle, slug: "cu", age: 30 * 24 * time.Hour, desc: "Tin cũ về thuế.", body: "Cũ."},
	}
}

var decreeOnPortal = portalDoc{id: "900", number: "381/2026/NĐ-CP", issued: "01/10/2026", effective: "01-12-2026",
	abstract: "Quy định về hóa đơn điện tử và chứng từ điện tử", class: 1}

func TestKeywordOnlyScan(t *testing.T) {
	w := newWorld(t)
	w.feeds["a"] = standardFeed()
	w.topic(store.Topic{Keywords: []string{"hóa đơn điện tử", "thuế"}})
	w.st.UpdateSourceUser(w.portID, false, 180) // listing off; verification still reaches the portal
	w.portal = []portalDoc{decreeOnPortal}

	sc := w.scan()
	if sc.ItemsNew != 3 { // the month-old item is outside the look-back window
		t.Errorf("new items: %d, want 3", sc.ItemsNew)
	}
	if sc.AlertsNew != 2 || sc.AICalls != 0 || sc.SourcesOK != 1 || sc.SourcesFailed != 0 {
		t.Errorf("scan: %+v", sc)
	}
	list := w.alerts("")
	if len(list) != 2 {
		t.Fatalf("alerts: %d", len(list))
	}
	if byTitle(list, "cướp") != nil {
		t.Error("an item without any keyword raised an alert")
	}
	d := byTitle(list, "381/2026")
	if d == nil {
		t.Fatal("decree alert missing")
	}
	if d.Severity != "notice" || d.LegalStatus != "issued" || d.Kind != "press" {
		t.Errorf("decree alert: severity=%s status=%s kind=%s", d.Severity, d.LegalStatus, d.Kind)
	}
	// The press report named a decree that exists on the official portal.
	if d.Verified != "confirmed" || d.DocumentID == 0 || d.EffectiveAt != "2026-12-01" {
		t.Errorf("verification: verified=%s doc=%d eff=%s", d.Verified, d.DocumentID, d.EffectiveAt)
	}
	doc, err := w.st.Document(d.DocumentID)
	if err != nil || doc.DocNumber != "381/2026/NĐ-CP" || doc.Validity != "not_yet" || doc.ValidityBasis != "inferred" {
		t.Errorf("document: %+v %v", doc, err)
	}
	if len(doc.Files) != 1 || doc.HasText {
		t.Errorf("files: %v hasText=%v", doc.Files, doc.HasText)
	}
	if _, err := os.Stat(filepath.Join(w.eng.DataDir, filepath.FromSlash(doc.Files[0]))); err != nil {
		t.Errorf("downloaded file missing: %v", err)
	}
	p := byTitle(list, "đề xuất")
	if p == nil || p.LegalStatus != "proposal" || p.Severity != "info" || p.Verified != "n/a" {
		t.Errorf("proposal alert: %+v", p)
	}
	if !strings.Contains(p.Reason, "Chưa được AI đánh giá") {
		t.Errorf("keyword-only alerts must say so: %q", p.Reason)
	}

	// Toasts: the info-level alert is not announced by default.
	if n := w.notif.Flush(); n != 1 || len(w.toasts) != 1 {
		t.Fatalf("toasts shown: %d (%d recorded)", n, len(w.toasts))
	}
	if toast := w.toasts[0]; !strings.Contains(toast.Title, "Cần chú ý") || !strings.Contains(toast.Title, "Đã ban hành") ||
		!strings.Contains(toast.Body, "381/2026") || !strings.Contains(toast.Body, "Báo A") {
		t.Errorf("toast: %+v", toast)
	}
	if w.notif.Flush() != 0 {
		t.Error("toast shown twice")
	}

	// A second scan sees nothing new.
	sc = w.scan()
	if sc.ItemsNew != 0 || sc.AlertsNew != 0 {
		t.Errorf("rescan: %+v", sc)
	}
	// The article body was indexed for search.
	if hits, _ := w.st.SearchChunks(`"ma" AND "co quan thue"`, store.ChunkScope{}, 5); len(hits) == 0 {
		t.Error("article text not searchable")
	}
}

func verdict(relevant bool, relevance, status, summary string, docs ...string) string {
	quoted := []string{}
	for _, d := range docs {
		quoted = append(quoted, `"`+d+`"`)
	}
	return fmt.Sprintf(`{"relevant": %v, "relevance": %q, "legal_status": %q, "summary": %q, "who_is_affected": "Doanh nghiệp",
"effective_date": null, "doc_numbers": [%s], "evidence_quote": "doanh nghiệp phải sử dụng hóa đơn điện tử có mã của cơ quan thuế", "reason": "lý do"}`,
		relevant, relevance, status, summary, strings.Join(quoted, ","))
}

func TestAIClassification(t *testing.T) {
	w := newWorld(t)
	w.feeds["a"] = standardFeed()
	w.st.UpdateSourceUser(w.portID, false, 180)
	w.topic(store.Topic{Keywords: []string{"hóa đơn điện tử", "thuế"}})
	fake := &fakeAI{replies: map[string][]string{
		// First reply breaks the schema; the engine must retry once.
		"381/2026": {"Xin lỗi, tôi không chắc.", verdict(true, "high", "issued", "Nghị định mới bắt buộc hóa đơn điện tử.", "381/2026/NĐ-CP", "999/2026/NĐ-CP")},
		"đề xuất":  {verdict(false, "low", "proposal", "")},
	}}
	w.eng.Provider = func() ai.Provider { return fake }

	sc := w.scan()
	if sc.AICalls != 3 || fake.calls != 3 {
		t.Errorf("AI calls: scan=%d provider=%d, want 3 (one retry)", sc.AICalls, fake.calls)
	}
	if sc.AlertsNew != 1 {
		t.Errorf("alerts: %d, want 1", sc.AlertsNew)
	}
	d := byTitle(w.alerts(""), "381/2026")
	if d == nil || d.Summary != "Nghị định mới bắt buộc hóa đơn điện tử." || d.AIProvider != "fake" || d.Relevance != "high" || d.Severity != "notice" {
		t.Fatalf("AI alert: %+v", d)
	}
	// The quote exists in the article, so it is kept; the invented document
	// number does not, so it is dropped.
	if d.Evidence == "" {
		t.Error("genuine evidence quote was dropped")
	}
	if len(d.DocNumbers) != 1 || d.DocNumbers[0] != "381/2026/NĐ-CP" {
		t.Errorf("hallucinated document number kept: %v", d.DocNumbers)
	}
	if d.Verified != "unconfirmed" { // the fake portal has no such decree yet
		t.Errorf("verified: %s", d.Verified)
	}
	filtered := w.alerts("filtered")
	if len(filtered) != 1 || !strings.Contains(filtered[0].Title, "đề xuất") {
		t.Fatalf("filtered list: %+v", filtered)
	}

	// Later the decree appears on the portal: the daily re-check confirms the
	// alert and queues a toast.
	w.notif.Flush()
	w.toasts = nil
	w.portal = []portalDoc{decreeOnPortal}
	w.clock = w.clock.Add(25 * time.Hour)
	w.st.SetSettings(map[string]string{"last_daily_at": store.FormatTime(w.clock)})
	w.scan()
	d = byTitle(w.alerts(""), "381/2026")
	if d.Verified != "confirmed" || d.DocumentID == 0 {
		t.Errorf("re-check did not confirm: %+v", d)
	}
	if w.notif.Flush() != 1 {
		t.Error("confirmation was not announced")
	}
	if due, _ := w.st.DueVerifications(10); len(due) != 0 {
		t.Errorf("verification still queued: %+v", due)
	}

	// Editing the topic invalidates cached verdicts; an unchanged topic reuses them.
	if _, _, ok := w.st.CachedVerdict(mustItemHash(t, w, "nd381"), d.TopicID, 1); !ok {
		t.Error("verdict was not cached")
	}
}

func mustItemHash(t *testing.T, w *world, slug string) string {
	t.Helper()
	it, err := w.st.Item(w.st.ItemID(w.srv.URL + "/bai/" + slug))
	if err != nil || it.ContentHash == "" {
		t.Fatalf("item %s: %+v %v", slug, it, err)
	}
	return it.ContentHash
}

func TestAIAuthFailureFallsBackToKeywords(t *testing.T) {
	w := newWorld(t)
	w.feeds["a"] = standardFeed()
	w.st.UpdateSourceUser(w.portID, false, 180)
	w.topic(store.Topic{Keywords: []string{"hóa đơn điện tử", "thuế"}})
	fake := &fakeAI{err: fmt.Errorf("%w: Claude CLI chưa đăng nhập", ai.ErrAuth)}
	w.eng.Provider = func() ai.Provider { return fake }

	sc := w.scan()
	if fake.calls != 1 {
		t.Errorf("kept calling an unauthenticated provider: %d calls", fake.calls)
	}
	if sc.AlertsNew != 2 {
		t.Errorf("keyword fallback produced %d alerts, want 2", sc.AlertsNew)
	}
	if !strings.Contains(sc.Note, "chưa đăng nhập") || w.eng.AIError() == "" {
		t.Errorf("the failure must be visible: note=%q", sc.Note)
	}
	// While blocked, the provider is not consulted at all.
	w.feeds["a"] = append(w.feeds["a"], feedItem{title: "Thông tư mới về thuế thu nhập", slug: "tt", age: time.Minute, desc: "x"})
	w.scan()
	if fake.calls != 1 {
		t.Errorf("provider called while blocked: %d", fake.calls)
	}
	w.eng.ResetAI()
	fake.err = nil
	w.feeds["a"] = append(w.feeds["a"], feedItem{title: "Hướng dẫn mới về thuế nhà thầu", slug: "nt", age: time.Minute, desc: "x"})
	w.scan()
	if fake.calls != 2 {
		t.Errorf("provider not used after reset: %d", fake.calls)
	}
}

func TestSameEventFromTwoSourcesIsOneAlert(t *testing.T) {
	w := newWorld(t)
	w.st.UpdateSourceUser(w.portID, false, 180)
	w.st.AddSource(store.Source{Name: "Báo B", Domain: "127.0.0.1", Tier: 3, Kind: "press", Connector: "rss",
		Config: fmt.Sprintf(`{"urls":["%s/feed/b.rss"]}`, w.srv.URL), IntervalMinutes: 60})
	w.feeds["a"] = standardFeed()[:1]
	w.feeds["b"] = []feedItem{{title: "Từ 1/12, bắt buộc dùng hóa đơn điện tử theo Nghị định 381/2026/NĐ-CP", slug: "b1", age: time.Hour,
		desc: "Quy định mới về hóa đơn điện tử."}}
	w.topic(store.Topic{Keywords: []string{"hóa đơn điện tử"}})
	sc := w.scan()
	if sc.AlertsNew != 1 {
		t.Fatalf("alerts: %d, want 1", sc.AlertsNew)
	}
	a, _ := w.st.Alert(w.alerts("")[0].ID)
	if len(a.Items) != 2 || a.SourceCount != 2 {
		t.Errorf("both reports should hang off one alert: %+v", a.Items)
	}
}

func TestOfficialListingAndWatchedDocument(t *testing.T) {
	w := newWorld(t)
	w.st.UpdateSourceUser(w.pressID, false, 60)
	base := portalDoc{id: "100", number: "13/2023/NĐ-CP", issued: "17/04/2023", effective: "01-07-2023", abstract: "Bảo vệ dữ liệu cá nhân", class: 1}
	w.portal = []portalDoc{base}
	tp := w.topic(store.Topic{Name: "Dữ liệu", Keywords: []string{"dữ liệu cá nhân"}, WatchedDocs: []string{"13/2023/ND-CP"}})

	// First scan: the 2023 decree is outside the look-back window, no alert.
	// The daily job baselines the watched document and stores it.
	w.st.SetSettings(map[string]string{"last_daily_at": ""})
	sc := w.scan()
	if sc.AlertsNew != 0 {
		t.Fatalf("baseline raised %d alerts", sc.AlertsNew)
	}
	if _, err := w.st.DocumentByNumber("13/2023/NĐ-CP"); err != nil {
		t.Fatal("watched document was not stored in the library")
	}

	// An amending decree is published.
	w.portal = append([]portalDoc{{id: "200", number: "400/2026/NĐ-CP", issued: "03/10/2026", effective: "01-01-2027",
		abstract: "Sửa đổi, bổ sung một số điều của Nghị định số 13/2023/NĐ-CP về bảo vệ dữ liệu cá nhân", class: 1}}, w.portal...)
	w.clock = w.clock.Add(4 * time.Hour)
	sc = w.scan()
	if sc.AlertsNew != 1 {
		t.Fatalf("amendment raised %d alerts, want 1", sc.AlertsNew)
	}
	a := w.alerts("")[0]
	if a.Kind != "doc_changed" || a.Severity != "warning" || a.Verified != "confirmed" || a.LegalStatus != "issued" || a.EffectiveAt != "2027-01-01" {
		t.Errorf("watched-document alert: %+v", a)
	}
	if a.SourceTier != 1 || !strings.Contains(a.URL, "docid=200") {
		t.Errorf("source: tier=%d url=%s", a.SourceTier, a.URL)
	}
	// The stored base decree is now linked and flagged as amended (inferred).
	b, _ := w.st.DocumentByNumber("13/2023/NĐ-CP")
	full, _ := w.st.Document(b.ID)
	if full.Validity != "amended" || full.ValidityBasis != "inferred" || len(full.Relations) != 1 || full.Relations[0].Relation != "amends" {
		t.Errorf("base document after amendment: validity=%s/%s relations=%+v", full.Validity, full.ValidityBasis, full.Relations)
	}

	// The daily search must not raise the same amendment a second time.
	w.clock = w.clock.Add(24 * time.Hour)
	w.st.SetSettings(map[string]string{"last_daily_at": ""})
	if sc = w.scan(); sc.AlertsNew != 0 {
		t.Errorf("duplicate alert from the daily search: %d", sc.AlertsNew)
	}

	// An amendment that scrolled out of the listing is still found by the
	// daily search for the watched number.
	w.portal = append(w.portal, portalDoc{id: "300", number: "55/2026/TT-BCA", issued: "20/09/2026", effective: "01-11-2026",
		abstract: "Hướng dẫn thi hành Nghị định 13/2023/NĐ-CP", class: 2})
	w.clock = w.clock.Add(24 * time.Hour)
	w.st.SetSettings(map[string]string{"last_daily_at": ""})
	if sc = w.scan(); sc.AlertsNew != 1 {
		t.Fatalf("daily search for the watched document raised %d alerts, want 1", sc.AlertsNew)
	}
	got := byTitle(w.alerts(""), "55/2026/TT-BCA")
	if got == nil || got.Kind != "doc_changed" || got.TopicID != tp.ID || !strings.Contains(got.Summary, "hướng dẫn") {
		t.Errorf("daily watched alert: %+v", got)
	}
}

func TestEffectiveSoonReminder(t *testing.T) {
	w := newWorld(t)
	w.st.UpdateSourceUser(w.pressID, false, 60)
	w.portal = []portalDoc{{id: "500", number: "390/2026/NĐ-CP", issued: "02/10/2026", effective: "20-10-2026",
		abstract: "Quy định mức lương tối thiểu vùng", class: 1}}
	w.topic(store.Topic{Name: "Lao động", Fields: []string{"Lao động"}, RemindDays: 7})
	if sc := w.scan(); sc.AlertsNew != 1 {
		t.Fatalf("listing alert: %d", sc.AlertsNew)
	}
	run := func(day int) int {
		w.clock = time.Date(2026, 10, day, 10, 0, 0, 0, vnZone)
		w.st.SetSettings(map[string]string{"last_daily_at": ""})
		return w.scan().AlertsNew
	}
	if n := run(10); n != 0 { // ten days ahead: too early
		t.Errorf("reminder too early: %d", n)
	}
	if n := run(14); n != 1 { // six days ahead
		t.Fatalf("reminder not raised: %d", n)
	}
	r := byTitle(w.alerts(""), "Sắp có hiệu lực")
	if r == nil || r.Kind != "effective_soon" || !strings.Contains(r.Title, "20/10/2026") || r.URL == "" {
		t.Errorf("reminder: %+v", r)
	}
	if n := run(15); n != 0 {
		t.Errorf("reminder repeated: %d", n)
	}
}

func TestDraftsRespectTopicKinds(t *testing.T) {
	w := newWorld(t)
	w.st.UpdateSourceUser(w.portID, false, 180)
	w.feeds["a"] = standardFeed()
	w.topic(store.Topic{Keywords: []string{"thuế"}, Kinds: []string{"press", "official"}}) // no drafts
	w.scan()
	if p := byTitle(w.alerts(""), "đề xuất"); p != nil {
		t.Error("a proposal reached the inbox of a topic that does not follow drafts")
	}
	if p := byTitle(w.alerts("filtered"), "đề xuất"); p == nil {
		t.Error("the filtered proposal must stay visible in the filtered list")
	}
}

func TestFailuresAreVisibleNotSilent(t *testing.T) {
	w := newWorld(t)
	w.st.UpdateSourceUser(w.portID, false, 180)
	w.topic(store.Topic{Keywords: []string{"thuế"}})
	w.failFeed["a"] = true
	for i := 0; i < 3; i++ {
		sc := w.scan()
		if sc.SourcesFailed != 1 || len(sc.Errors) != 1 || !strings.Contains(sc.Errors[0].Error, "500") {
			t.Fatalf("scan %d: %+v", i, sc)
		}
	}
	src, _ := w.st.Source(w.pressID)
	if src.FailCount != 3 || src.LastStatus != "error" {
		t.Errorf("failure tracking: %+v", src)
	}
	w.notif.SystemCheck()
	if len(w.toasts) != 1 || w.toasts[0].Level != "system" || !strings.Contains(w.toasts[0].Body, "Báo A") {
		t.Fatalf("system toast: %+v", w.toasts)
	}
	w.notif.SystemCheck()
	if len(w.toasts) != 1 {
		t.Error("system toast repeated within a day")
	}

	// A feed that answers but lists nothing is tracked as "empty".
	w.failFeed["a"] = false
	w.feeds["a"] = nil
	w.scan()
	src, _ = w.st.Source(w.pressID)
	if src.LastStatus != "empty" || src.EmptyCount != 1 || src.FailCount != 0 {
		t.Errorf("empty tracking: %+v", src)
	}
}

func TestOfflineIsNotASourceFailure(t *testing.T) {
	w := newWorld(t)
	w.topic(store.Topic{Keywords: []string{"thuế"}})
	w.srv.Close() // nothing listens any more: every request fails to connect
	sc := w.scan()
	if !strings.Contains(sc.Note, "Không có kết nối mạng") || sc.SourcesFailed != 0 {
		t.Errorf("offline scan: %+v", sc)
	}
	src, _ := w.st.Source(w.pressID)
	if src.FailCount != 0 || src.LastScanAt != "" {
		t.Errorf("offline round counted against the source: %+v", src)
	}
}

func TestQuietHoursHoldToasts(t *testing.T) {
	w := newWorld(t)
	w.st.UpdateSourceUser(w.portID, false, 180)
	w.feeds["a"] = standardFeed()
	w.topic(store.Topic{Keywords: []string{"hóa đơn điện tử"}})
	w.clock = time.Date(2026, 10, 4, 22, 30, 0, 0, vnZone) // inside 21:00–07:00
	w.scan()
	if w.notif.Flush() != 0 || len(w.toasts) != 0 {
		t.Fatal("toast shown during quiet hours")
	}
	w.clock = time.Date(2026, 10, 5, 7, 5, 0, 0, vnZone)
	if w.notif.Flush() != 1 {
		t.Error("held toast not delivered after quiet hours")
	}
	// Pause works the same way.
	w.feeds["a"] = append(w.feeds["a"], feedItem{title: "Thêm quy định về hóa đơn điện tử ban hành", slug: "x2", age: 0, desc: "y"})
	w.st.SetSettings(map[string]string{"pause_until": store.FormatTime(w.clock.Add(time.Hour))})
	w.scan()
	if w.notif.Flush() != 0 {
		t.Error("toast shown while paused")
	}
	w.clock = w.clock.Add(2 * time.Hour)
	if w.notif.Flush() != 1 {
		t.Error("paused toast not delivered")
	}
}

func TestDigestWhenManyAlerts(t *testing.T) {
	w := newWorld(t)
	w.st.UpdateSourceUser(w.portID, false, 180)
	for i := 0; i < 5; i++ {
		w.feeds["a"] = append(w.feeds["a"], feedItem{title: fmt.Sprintf("Chính phủ ban hành quy định số %d về thuế", i),
			slug: fmt.Sprintf("s%d", i), age: time.Hour, desc: "d"})
	}
	w.topic(store.Topic{Keywords: []string{"thuế"}})
	w.scan()
	if n := w.notif.Flush(); n != 1 || len(w.toasts) != 1 {
		t.Fatalf("expected one digest toast, got %d", len(w.toasts))
	}
	if !strings.Contains(w.toasts[0].Title, "5 cảnh báo") || !strings.Contains(w.toasts[0].Body, "và 3 tin khác") {
		t.Errorf("digest: %+v", w.toasts[0])
	}
}

func TestScheduler(t *testing.T) {
	w := newWorld(t)
	w.feeds["a"] = standardFeed()
	w.topic(store.Topic{Keywords: []string{"thuế"}})
	s := &Scheduler{Engine: w.eng, Notifier: w.notif, St: w.st, Now: func() time.Time { return w.clock }}

	ids, catchup := s.Due(w.clock)
	if len(ids) != 2 || catchup {
		t.Fatalf("never-scanned sources must be due: %v catchup=%v", ids, catchup)
	}
	// Outside the active hours nothing runs.
	w.clock = time.Date(2026, 10, 4, 23, 0, 0, 0, vnZone)
	if s.Step(context.Background()) {
		t.Fatal("scan ran outside the active hours")
	}
	w.clock = time.Date(2026, 10, 5, 8, 0, 0, 0, vnZone)
	if !s.Step(context.Background()) {
		t.Fatal("due scan did not run")
	}
	if scans, _ := w.st.Scans(5); len(scans) != 1 || scans[0].Trigger != "schedule" {
		t.Fatalf("scan log: %+v", scans)
	}
	// Right after a scan nothing is due.
	w.clock = w.clock.Add(10 * time.Minute)
	if ids, _ := s.Due(w.clock); len(ids) != 0 {
		t.Errorf("sources due 10 minutes after a scan: %v", ids)
	}
	// After 70 minutes the hourly press source is due, the 3-hourly portal is not.
	w.clock = w.clock.Add(60 * time.Minute)
	if ids, _ := s.Due(w.clock); len(ids) != 1 || ids[0] != w.pressID {
		t.Errorf("due after 70 minutes: %v", ids)
	}
	// After a long sleep everything is due and the rounds collapse into one catch-up scan.
	w.clock = w.clock.Add(26 * time.Hour)
	ids, catchup = s.Due(w.clock)
	if len(ids) != 2 || !catchup {
		t.Errorf("after sleep: %v catchup=%v", ids, catchup)
	}
	if !s.Step(context.Background()) {
		t.Fatal("catch-up scan did not run")
	}
	if scans, _ := w.st.Scans(5); len(scans) != 2 || scans[0].Trigger != "catchup" {
		t.Errorf("scan log: %+v", scans)
	}
	// The retry gap stops a second attempt in the same minute.
	w.st.UpdateSourceUser(w.pressID, true, 5)
	w.clock = w.clock.Add(time.Minute)
	if s.Step(context.Background()) {
		t.Error("retry gap ignored")
	}
}

func TestScanIsExclusive(t *testing.T) {
	w := newWorld(t)
	w.eng.mu.Lock()
	w.eng.running = true
	w.eng.mu.Unlock()
	if _, err := w.eng.Scan(context.Background(), "manual", nil); err != ErrBusy {
		t.Errorf("concurrent scan: %v", err)
	}
}

func TestMatchingRules(t *testing.T) {
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, vnZone)
	tp := store.Topic{Keywords: []string{"thuế"}, ExcludeKeywords: []string{"bóng đá"}, Fields: []string{"Lao động"},
		WatchedDocs: []string{"45/2019/QH14"}, Kinds: []string{"press"}, SourceIDs: []int64{7}}
	if m := MatchTopic(tp, 7, "press", "Quy định mới về thuế và lương tối thiểu theo Bộ luật 45/2019/QH14"); !m.OK() ||
		len(m.Keywords) != 2 || len(m.Watched) != 1 {
		t.Errorf("match: %+v", m)
	}
	for name, got := range map[string]Match{
		"other source":   MatchTopic(tp, 8, "press", "thuế"),
		"other kind":     MatchTopic(tp, 7, "official", "thuế"),
		"excluded":       MatchTopic(tp, 7, "press", "Cầu thủ bóng đá nộp thuế"),
		"tone mismatch":  MatchTopic(tp, 7, "press", "Cho thuê nhà giá rẻ"),
		"no keyword":     MatchTopic(tp, 7, "press", "Tin thời tiết"),
		"partial number": MatchTopic(store.Topic{WatchedDocs: []string{"5/2019/QH14"}, Kinds: []string{"press"}}, 7, "press", "Bộ luật 45/2019/QH14"),
	} {
		if got.OK() {
			t.Errorf("%s: matched %+v", name, got)
		}
	}
	status := map[string]string{
		"Dự thảo nghị định đang lấy ý kiến.":                   "draft",
		"Bộ Tài chính đề xuất giảm thuế":                       "proposal",
		"Nghị định có hiệu lực từ ngày 1/1":                    "issued",
		"Chính phủ ban hành nghị định":                         "issued",
		"Điểm mới của Thông tư 12/2026/TT-BTC":                 "issued",
		"Nhắc lại quy định tại Luật 59/2020/QH14":              "unknown",
		"Sắp có nghị định về giảm thuế thu nhập doanh nghiệp":  "draft",
		"Giá vàng hôm nay":                                     "unknown",
		"Kết quả thi hành Nghị định 13/2023/NĐ-CP sau hai năm": "unknown",
	}
	for in, want := range status {
		if got := GuessStatus(in, now); got != want {
			t.Errorf("GuessStatus(%q) = %s, want %s", in, got, want)
		}
	}
	if got := PrimaryDocs("Tin về Nghị định 381/2026/NĐ-CP", "căn cứ Luật 59/2020/QH14", now); len(got) != 1 || got[0] != "381/2026/NĐ-CP" {
		t.Errorf("PrimaryDocs title: %v", got)
	}
	if got := PrimaryDocs("Quy định mới", "theo Luật 59/2020/QH14 và Thông tư 12/2026/TT-BTC", now); len(got) != 1 || got[0] != "12/2026/TT-BTC" {
		t.Errorf("PrimaryDocs body: %v", got)
	}
	windows := []struct {
		h          int
		start, end string
		want       bool
	}{{10, "07:00", "21:00", true}, {22, "07:00", "21:00", false}, {22, "21:00", "07:00", true}, {6, "21:00", "07:00", true},
		{12, "21:00", "07:00", false}, {3, "", "", true}, {3, "08:00", "08:00", true}}
	for _, c := range windows {
		if got := InWindow(time.Date(2026, 1, 1, c.h, 0, 0, 0, vnZone), c.start, c.end); got != c.want {
			t.Errorf("InWindow(%d, %s-%s) = %v", c.h, c.start, c.end, got)
		}
	}
	if len(Fields()) != len(FieldKeywords) {
		t.Error("Fields")
	}
}

// A topic created after the items were collected must still see them.
func TestBackfillAppliesNewTopicToCollectedItems(t *testing.T) {
	w := newWorld(t)
	w.st.UpdateSourceUser(w.portID, false, 180)
	w.feeds["a"] = standardFeed()
	w.portal = []portalDoc{decreeOnPortal}

	// First scan with no topic at all: items are stored, nothing is raised.
	if sc := w.scan(); sc.ItemsNew != 3 || sc.AlertsNew != 0 {
		t.Fatalf("scan without topics: %+v", sc)
	}
	tp := w.topic(store.Topic{Keywords: []string{"hóa đơn điện tử", "thuế"}})
	if sc := w.scan(); sc.AlertsNew != 0 {
		t.Fatalf("a plain rescan must not re-evaluate seen items: %+v", sc)
	}
	sc, err := w.eng.Backfill(context.Background(), tp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sc.Trigger != "backfill" || sc.AlertsNew != 2 {
		t.Fatalf("backfill: %+v", sc)
	}
	d := byTitle(w.alerts(""), "381/2026")
	if d == nil || d.Verified != "confirmed" || d.DocumentID == 0 {
		t.Errorf("backfilled alert should be verified like any other: %+v", d)
	}
	// Running it again, e.g. after editing the topic, raises nothing twice.
	if sc, _ = w.eng.Backfill(context.Background(), tp.ID); sc.AlertsNew != 0 {
		t.Errorf("second backfill raised %d alerts", sc.AlertsNew)
	}
	if n := len(w.alerts("")); n != 2 {
		t.Errorf("alerts after a repeated backfill: %d", n)
	}
	// It is logged like a scan and refuses to overlap one.
	if scans, _ := w.st.Scans(1); scans[0].Trigger != "backfill" || !strings.Contains(scans[0].Note, tp.Name) {
		t.Errorf("scan log: %+v", scans[0])
	}
	w.eng.mu.Lock()
	w.eng.running = true
	w.eng.mu.Unlock()
	if _, err := w.eng.Backfill(context.Background(), tp.ID); err != ErrBusy {
		t.Errorf("backfill during a scan: %v", err)
	}
}

func TestBackfillOfficialItems(t *testing.T) {
	w := newWorld(t)
	w.st.UpdateSourceUser(w.pressID, false, 60)
	w.portal = []portalDoc{{id: "500", number: "390/2026/NĐ-CP", issued: "02/10/2026", effective: "20-10-2026",
		abstract: "Quy định mức lương tối thiểu vùng", class: 1}}
	w.scan()
	tp := w.topic(store.Topic{Name: "Lao động", Fields: []string{"Lao động"}})
	sc, err := w.eng.Backfill(context.Background(), tp.ID)
	if err != nil || sc.AlertsNew != 1 {
		t.Fatalf("backfill of a portal listing: %+v %v", sc, err)
	}
	a := w.alerts("")[0]
	if a.Kind != "official" || a.DocumentID == 0 || a.EffectiveAt != "2026-10-20" || a.Verified != "confirmed" {
		t.Errorf("official alert from backfill: %+v", a)
	}
}

// "No toast" must never be silent. A scan the user asked for always answers,
// and a level that is switched off is logged rather than just dropped.
func TestManualScanAlwaysAnswers(t *testing.T) {
	w := newWorld(t)
	w.st.UpdateSourceUser(w.portID, false, 180)
	s := &Scheduler{Engine: w.eng, Notifier: w.notif, St: w.st, Now: func() time.Time { return w.clock }}

	// Nothing matches: the user is told so.
	w.topic(store.Topic{Keywords: []string{"điều không bao giờ xuất hiện"}})
	w.feeds["a"] = standardFeed()
	if _, err := s.ScanNow(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(w.toasts) != 1 || !strings.Contains(w.toasts[0].Body, "Không có tin mới khớp") {
		t.Fatalf("empty manual scan: %+v", w.toasts)
	}

	// Alerts exist but their level is switched off: the user is told why.
	w.toasts = nil
	w.topic(store.Topic{Name: "Thuế 2", Keywords: []string{"thuế"}})
	w.feeds["a"] = append(w.feeds["a"], feedItem{title: "Quy định mới về thuế khoán", slug: "k1", age: time.Minute, desc: "d"})
	sc, _ := s.ScanNow(context.Background(), nil)
	if sc.AlertsNew == 0 {
		t.Fatalf("expected alerts: %+v", sc)
	}
	var found bool
	for _, toast := range w.toasts {
		if strings.Contains(toast.Body, "đang tắt") {
			found = true
		}
	}
	if !found {
		t.Errorf("no explanation for suppressed alerts: %+v", w.toasts)
	}

	// Even in quiet hours the answer to a manual scan is shown.
	w.toasts = nil
	w.clock = time.Date(2026, 10, 5, 23, 0, 0, 0, vnZone)
	s.ScanNow(context.Background(), nil)
	if len(w.toasts) == 0 {
		t.Error("manual scan answered nothing during quiet hours")
	}
	w.toasts = nil
	w.notif.Test()
	if len(w.toasts) != 1 || !strings.Contains(w.toasts[0].Title, "thông báo thử") {
		t.Errorf("test toast: %+v", w.toasts)
	}
}

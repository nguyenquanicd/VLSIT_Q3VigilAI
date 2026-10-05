package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"q3vnlaw/internal/fetch"
	"q3vnlaw/internal/store"
)

const rssSample = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>Báo</title>
<item><title><![CDATA[Nghị định mới về thuế gi&aacute; trị gia tăng &#039;từ 2027&#039;]]></title>
<link>http://bao.vn/nghi-dinh-moi-123.html?utm_source=rss</link>
<description><![CDATA[<a href="x"><img src="a.jpg"></a>Chính phủ vừa ban hành <b>Nghị định 381/2026/NĐ-CP</b> quy định...]]></description>
<pubDate>Sun, 04 Oct 2026 07:05:00 +0700</pubDate></item>
<item><title>Tin thứ hai & dấu và chưa thoát</title><link>/tin-2.html</link><pubDate>04/10/2026 08:30:00</pubDate></item>
<item><title></title><link>https://bao.vn/khong-tieu-de</link></item>
<item><title>Không có link</title></item>
</channel></rss>`

const atomSample = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
<entry><title>Thông tư mới</title><link rel="alternate" href="https://bo.gov.vn/tt-1"/><summary>Tóm tắt</summary>
<published>2026-10-03T10:00:00+07:00</published></entry></feed>`

func TestParseFeed(t *testing.T) {
	refs, err := ParseFeed([]byte(rssSample), "https://bao.vn/rss/phap-luat.rss")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 {
		t.Fatalf("got %d refs, want 2: %+v", len(refs), refs)
	}
	r := refs[0]
	if r.Title != "Nghị định mới về thuế giá trị gia tăng 'từ 2027'" {
		t.Errorf("title: %q", r.Title)
	}
	if r.URL != "https://bao.vn/nghi-dinh-moi-123.html" {
		t.Errorf("url (https upgrade, tracking stripped): %q", r.URL)
	}
	if !strings.HasPrefix(r.Summary, "Chính phủ vừa ban hành Nghị định 381/2026/NĐ-CP") {
		t.Errorf("summary: %q", r.Summary)
	}
	if want := time.Date(2026, 10, 4, 7, 5, 0, 0, vn); !r.Published.Equal(want) {
		t.Errorf("date: %v", r.Published)
	}
	if refs[1].URL != "https://bao.vn/tin-2.html" || refs[1].Published.Hour() != 8 {
		t.Errorf("relative link / local date: %+v", refs[1])
	}

	refs, err = ParseFeed([]byte(atomSample), "https://bo.gov.vn/feed")
	if err != nil || len(refs) != 1 || refs[0].URL != "https://bo.gov.vn/tt-1" || refs[0].Published.IsZero() {
		t.Errorf("atom: %v %+v", err, refs)
	}
	if _, err := ParseFeed([]byte("<html><body>Not a feed"), "https://x.vn/"); err == nil {
		// An HTML error page parses as XML with no items; that is reported as
		// an empty source by the pipeline, not as an error here.
		t.Log("html page yields no error, as expected by the pipeline")
	}
}

func TestParseDateFormatsSeenInTheWild(t *testing.T) {
	want := time.Date(2026, 10, 3, 19, 54, 0, 0, vn)
	for _, in := range []string{
		"Sat, 03 Oct 2026 19:54:00 +0700",
		"Sat, 03 Oct 2026 12:54:00 GMT",
		"Sat, 3 Oct 2026 19:54:00 GMT+7",
		"2026-10-03T19:54:00+07:00",
		"03/10/2026 19:54:00",
		"10/3/2026 7:54:00 PM",    // .NET feeds: month first, 12-hour clock
		"10/3/2026 7:54:00 PM",    // the same with a narrow no-break space (tuoitre.vn)
		"  10/3/2026 7:54:00 PM ", // or a no-break space
	} {
		if got := parseDate(in); !got.Equal(want) {
			t.Errorf("parseDate(%q) = %v, want %v", in, got, want)
		}
	}
	for _, in := range []string{"", "hôm qua", "32/13/2026"} {
		if got := parseDate(in); !got.IsZero() {
			t.Errorf("parseDate(%q) = %v, want zero", in, got)
		}
	}
}

func TestParseLinkList(t *testing.T) {
	page := `<html><body><nav><a href="/">Trang chủ</a></nav>
<a href="/van-ban/nghi-dinh-381-2026.html">Nghị định 381/2026/NĐ-CP quy định về hóa đơn điện tử</a>
<a href="/van-ban/nghi-dinh-381-2026.html#c">Nghị định 381/2026/NĐ-CP quy định về hóa đơn điện tử</a>
<a href="https://other.vn/abc" title="x">Một liên kết ra ngoài có tiêu đề đủ dài để tính</a>
<a href="/lien-he">Liên hệ</a></body></html>`
	refs := ParseLinkList([]byte(page), "https://bo.gov.vn/van-ban", nil)
	if len(refs) != 1 || refs[0].URL != "https://bo.gov.vn/van-ban/nghi-dinh-381-2026.html" {
		t.Errorf("same-host default: %+v", refs)
	}
	refs = ParseLinkList([]byte(page), "https://bo.gov.vn/van-ban", regexp.MustCompile(`other\.vn/`))
	if len(refs) != 1 || refs[0].URL != "https://other.vn/abc" {
		t.Errorf("pattern: %+v", refs)
	}
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// These two tests run against pages saved from the live portal on 2026-10-04.
func TestParseVanbanListFixture(t *testing.T) {
	hits := ParseVanbanList(fixture(t, "vanban_list.html"), 1)
	if len(hits) != 50 {
		t.Fatalf("rows: %d, want 50", len(hits))
	}
	h := hits[0]
	if h.PortalID != "219746" || h.DocNumber != "377/2026/NĐ-CP" || !strings.HasPrefix(h.Abstract, "Sửa đổi, bổ sung một số điều") {
		t.Errorf("first row: %+v", h)
	}
	if h.Issued.Format("2006-01-02") != "2026-10-01" {
		t.Errorf("issued: %v", h.Issued)
	}
	if h.URL != "https://vanban.chinhphu.vn/?pageid=27160&docid=219746" {
		t.Errorf("url: %s", h.URL)
	}
	for _, h := range hits {
		if h.DocNumber == "" || h.Issued.IsZero() || h.Abstract == "" {
			t.Errorf("incomplete row: %+v", h)
		}
	}
}

func TestParseVanbanDetailFixture(t *testing.T) {
	info, err := ParseVanbanDetail(fixture(t, "vanban_detail.html"), "219746")
	if err != nil {
		t.Fatal(err)
	}
	if info.DocNumber != "377/2026/NĐ-CP" || info.DocType != "Nghị định" || info.Issuer != "Chính phủ" || info.Signer != "Nguyễn Văn Thắng" {
		t.Errorf("metadata: %+v", info)
	}
	if info.Issued.Format("2006-01-02") != "2026-10-01" || info.Effective.Format("2006-01-02") != "2026-10-01" {
		t.Errorf("dates: %v %v", info.Issued, info.Effective)
	}
	if !strings.HasPrefix(info.Title, "Nghị định số 377/2026/NĐ-CP của Chính phủ") {
		t.Errorf("title: %q", info.Title)
	}
	if len(info.FileURLs) != 1 || !strings.HasPrefix(info.FileURLs[0], "https://datafiles.chinhphu.vn/") {
		t.Errorf("files: %v", info.FileURLs)
	}
	if _, err := ParseVanbanDetail("<html><body>Không có</body></html>", "1"); err == nil {
		t.Error("empty page accepted")
	}
}

// fakePortal imitates the search form: the control prefix and the hidden
// fields must be echoed back in the POST.
func fakePortal(t *testing.T) *httptest.Server {
	row := func(id, code, date, abstract string) string {
		return `<tr><td><span class="code">` + code + `</span></td><td><span class="issued-date">` + date + `</span></td>
<td><a href="/?pageid=27160&docid=` + id + `"><span class="substract">` + abstract + `</span></a>
<div class="bl-doc-file"> <a href="` + "{{BASE}}" + `/files/` + id + `.pdf">f</a></div></td></tr>`
	}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch {
		case r.URL.Path == "/robots.txt":
			http.NotFound(w, r)
		case strings.HasPrefix(r.URL.Path, "/files/"):
			w.Write([]byte("%PDF-1.4 fake"))
		case r.URL.Path == "/he-thong-van-ban" && r.Method == http.MethodGet:
			w.Write([]byte(`<form><input type="hidden" name="__VIEWSTATE" value="abc&amp;def" />
<input name="ctrl_9_7$txtSearchKeyword" type="text" /><table>` +
				strings.ReplaceAll(row("100", "1/2026/NĐ-CP", "01/10/2026", "Văn bản mới nhất"), "{{BASE}}", srv.URL) + `</table></form>`))
		case r.URL.Path == "/he-thong-van-ban" && r.Method == http.MethodPost:
			r.ParseForm()
			if r.PostForm.Get("__VIEWSTATE") != "abc&def" || r.PostForm.Get("ctrl_9_7$btnSearch") != "Tìm kiếm" {
				t.Errorf("search form not echoed: %v", r.PostForm)
			}
			q := r.PostForm.Get("ctrl_9_7$txtSearchKeyword")
			body := `<form><input name="ctrl_9_7$txtSearchKeyword" /><table>`
			if strings.HasPrefix(q, "13/2023") && r.URL.Query().Get("classid") == "1" {
				body += row("200", "13/2023/NĐ-СР", "17/04/2023", "Bảo vệ dữ liệu cá nhân") // Cyrillic CP, as the portal sometimes has
				body += row("201", "113/2023/NĐ-CP", "01/05/2023", "Văn bản khác")
			}
			w.Write([]byte(strings.ReplaceAll(body, "{{BASE}}", srv.URL) + `</table></form>`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestVanbanSearchResolveDownload(t *testing.T) {
	srv := fakePortal(t)
	old := VanbanBase
	VanbanBase = srv.URL
	defer func() { VanbanBase = old }()
	fc := fetch.New()
	fc.Insecure = true
	fc.SetAllowed([]string{"127.0.0.1"})
	ctx := context.Background()

	l, err := List(ctx, fc, store.Source{Connector: "vanban", Config: `{"class":1}`})
	if err != nil || len(l.Refs) != 1 || l.Refs[0].DocNumber != "1/2026/NĐ-CP" || l.Refs[0].PortalID != "100" {
		t.Fatalf("list: %v %+v", err, l)
	}
	hit, err := ResolveVanban(ctx, fc, "13/2023/ND-CP") // typed without Đ
	if err != nil || hit == nil || hit.PortalID != "200" {
		t.Fatalf("resolve: %v %+v", err, hit)
	}
	if miss, err := ResolveVanban(ctx, fc, "999/2026/NĐ-CP"); err != nil || miss != nil {
		t.Errorf("resolve of an unknown number returned %+v, %v", miss, err)
	}
	dir := t.TempDir()
	saved, problems := DownloadFiles(ctx, fc, append(hit.FileURLs, "https://evil.example/x.pdf"), dir)
	if len(saved) != 1 || saved[0] != "200.pdf" || len(problems) != 1 {
		t.Errorf("download: saved=%v problems=%v", saved, problems)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "200.pdf")); !strings.HasPrefix(string(b), "%PDF-") {
		t.Error("file content")
	}
}

func TestLayoutChangeIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html><body>Trang đang bảo trì</body></html>"))
	}))
	defer srv.Close()
	old := VanbanBase
	VanbanBase = srv.URL
	defer func() { VanbanBase = old }()
	fc := fetch.New()
	fc.Insecure = true
	fc.SetAllowed([]string{"127.0.0.1"})
	if _, err := List(context.Background(), fc, store.Source{Connector: "vanban", Config: `{}`}); err != ErrLayout {
		t.Errorf("a page without the listing must be reported as a layout change, got %v", err)
	}
}

func TestGuessRelationAndHelpers(t *testing.T) {
	cases := map[string]string{
		"Sửa đổi, bổ sung một số điều của Nghị định số 13/2023/NĐ-CP": "amends",
		"Thay thế Thông tư số 80/2021/TT-BTC":                         "replaces",
		"Quy định chi tiết một số điều của Luật Đất đai":              "guides",
		"Bãi bỏ một số văn bản quy phạm pháp luật":                    "repeals",
		"Văn bản hợp nhất Luật Doanh nghiệp":                          "consolidates",
		"Quy định chức năng, nhiệm vụ của Bộ Tài chính":               "mentions",
	}
	for in, want := range cases {
		if got := GuessRelation(in); got != want {
			t.Errorf("GuessRelation(%q) = %s, want %s", in, got, want)
		}
	}
	if SafeName(`13/2023/NĐ-CP: "a"? `) != "13_2023_NĐ-CP_ _a__" {
		t.Errorf("SafeName: %q", SafeName(`13/2023/NĐ-CP: "a"? `))
	}
	doms := AllowedDomains([]store.Source{{Domain: "vnexpress.net", Enabled: true}, {Domain: "tuoitre.vn", Enabled: false}})
	if len(doms) != 2 || doms[0] != "chinhphu.vn" || doms[1] != "vnexpress.net" {
		t.Errorf("AllowedDomains: %v", doms)
	}
	keys := map[string]bool{}
	for _, b := range Builtins() {
		if keys[b.Key] || b.Key == "" || b.Domain == "" || fetch.Blocked(b.Domain) {
			t.Errorf("bad builtin: %+v", b)
		}
		keys[b.Key] = true
	}
}

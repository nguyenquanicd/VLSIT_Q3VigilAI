package sources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"q3vnlaw/internal/fetch"
	"q3vnlaw/internal/store"
	"q3vnlaw/internal/textutil"
)

// The Government portal (vanban.chinhphu.vn) is an ASP.NET site without an
// API. Its robots.txt allows automated access. The parsing below mirrors the
// han-download-law-doc skill, which was developed against the live site.

// VanbanBase is the portal root; tests point it at a local server.
var VanbanBase = "https://vanban.chinhphu.vn"

// ErrLayout means the portal's page structure no longer matches the parser.
var ErrLayout = errors.New("cấu trúc trang vanban.chinhphu.vn đã thay đổi, bộ nối cần cập nhật")

// Hit is one row of a portal listing or search result.
type Hit struct {
	PortalID  string
	DocNumber string
	Issued    time.Time
	Abstract  string
	URL       string
	FileURLs  []string
	Class     int
}

// DocInfo is the detail page of one document.
type DocInfo struct {
	PortalID  string
	DocNumber string
	Title     string
	Abstract  string
	DocType   string
	Issuer    string
	Signer    string
	Issued    time.Time
	Effective time.Time
	URL       string
	Meta      map[string]string
	FileURLs  []string
}

var (
	reRow       = regexp.MustCompile(`<tr[\s>]`)
	reDocID     = regexp.MustCompile(`docid=(\d+)`)
	reCode      = regexp.MustCompile(`(?s)class="code">(.*?)</span>`)
	reIssued    = regexp.MustCompile(`(?s)class="issued-date">(.*?)</span>`)
	reAbstract  = regexp.MustCompile(`(?s)class="substract">(.*?)</span>`)
	reRowFile   = regexp.MustCompile(`class="bl-doc-file">\s*<a[^>]*href="([^"]+)"`)
	rePrefix    = regexp.MustCompile(`name="(ctrl_\d+_\d+)\$txtSearchKeyword"`)
	reHidden    = regexp.MustCompile(`<input[^>]*type="hidden"[^>]*>`)
	reName      = regexp.MustCompile(`name="([^"]*)"`)
	reValue     = regexp.MustCompile(`value="([^"]*)"`)
	reTitle     = regexp.MustCompile(`(?s)<h4 class="title">(.*?)</h4>`)
	reMetaRow   = regexp.MustCompile(`(?s)<td class="col1"[^>]*>(.*?)</td>\s*<td[^>]*>(.*?)</td>`)
	reViewFile  = regexp.MustCompile(`<a[^>]*href="([^"]+)"[^>]*class="view-file"`)
	reEffective = regexp.MustCompile(`(?s)tr_ngaycohieuluc"[^>]*>\s*<td[^>]*>.*?</td>\s*<td[^>]*>\s*(\d[\d\-/]+)`)
	reVNDate    = regexp.MustCompile(`^\s*(\d{1,2})[-/](\d{1,2})[-/](\d{4})`)
)

// parseVNDate reads dd/MM/yyyy (lists) and dd-MM-yyyy (detail pages).
func parseVNDate(s string) time.Time {
	m := reVNDate.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}
	}
	d, _ := strconv.Atoi(m[1])
	mo, _ := strconv.Atoi(m[2])
	y, _ := strconv.Atoi(m[3])
	if mo < 1 || mo > 12 || d < 1 || d > 31 {
		return time.Time{}
	}
	return time.Date(y, time.Month(mo), d, 0, 0, 0, 0, vn)
}

func detailURL(id string) string { return VanbanBase + "/?pageid=27160&docid=" + id }

func listURL(class int) string {
	return fmt.Sprintf("%s/he-thong-van-ban?classid=%d&mode=1", VanbanBase, class)
}

func first(re *regexp.Regexp, s string) string {
	if m := re.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

// ParseVanbanList extracts the document rows of a listing or search page.
func ParseVanbanList(page string, class int) []Hit {
	var out []Hit
	for _, chunk := range reRow.Split(page, -1) {
		if !strings.Contains(chunk, `class="code"`) {
			continue
		}
		id := first(reDocID, chunk)
		if id == "" {
			continue
		}
		h := Hit{
			PortalID:  id,
			DocNumber: textutil.StripTags(first(reCode, chunk)),
			Issued:    parseVNDate(textutil.StripTags(first(reIssued, chunk))),
			Abstract:  textutil.StripTags(first(reAbstract, chunk)),
			URL:       detailURL(id),
			Class:     class,
		}
		for _, m := range reRowFile.FindAllStringSubmatch(chunk, -1) {
			h.FileURLs = append(h.FileURLs, html.UnescapeString(m[1]))
		}
		out = append(out, h)
	}
	return out
}

type vanbanConfig struct {
	Class int `json:"class"`
}

func listVanban(ctx context.Context, fc *fetch.Client, src store.Source) (Listing, error) {
	var cfg vanbanConfig
	json.Unmarshal([]byte(src.Config), &cfg)
	if cfg.Class == 0 {
		cfg.Class = 1
	}
	resp, err := fc.Get(ctx, listURL(cfg.Class), fetch.Options{})
	if err != nil {
		return Listing{}, err
	}
	page := string(resp.Body)
	hits := ParseVanbanList(page, cfg.Class)
	if len(hits) == 0 && !rePrefix.MatchString(page) {
		return Listing{}, ErrLayout
	}
	var out Listing
	for _, h := range hits {
		out.Refs = append(out.Refs, Ref{URL: h.URL, Title: h.DocNumber + " – " + h.Abstract, Summary: h.Abstract,
			Published: h.Issued, DocNumber: h.DocNumber, PortalID: h.PortalID, FileURLs: h.FileURLs})
	}
	return out, nil
}

// SearchVanban runs the portal's search box. The portal matches text
// literally, so callers should pass short, accented phrases or a number.
func SearchVanban(ctx context.Context, fc *fetch.Client, text string, class, limit int) ([]Hit, error) {
	hits, err := searchVanban(ctx, fc, text, class, limit, true)
	if err == ErrLayout {
		// The cached form may have gone stale (the portal was redeployed):
		// read the form afresh once before reporting a layout change.
		hits, err = searchVanban(ctx, fc, text, class, limit, false)
	}
	return hits, err
}

// searchForm is the state of the portal's search form: the hidden ASP.NET
// fields and the control prefix. It is reused for a few minutes so a run of
// searches costs one request each instead of two.
type searchForm struct {
	prefix string
	hidden url.Values
	at     time.Time
}

var (
	formMu    sync.Mutex
	formCache = map[string]searchForm{}
)

func loadForm(ctx context.Context, fc *fetch.Client, u string, useCache bool) (searchForm, error) {
	formMu.Lock()
	f, ok := formCache[u]
	formMu.Unlock()
	if useCache && ok && time.Since(f.at) < 10*time.Minute {
		return f, nil
	}
	resp, err := fc.Get(ctx, u, fetch.Options{})
	if err != nil {
		return searchForm{}, err
	}
	page := string(resp.Body)
	// The search box belongs to an ASP.NET control whose id prefix changes
	// when the portal is redeployed, so it is discovered, not hard-coded.
	f = searchForm{prefix: first(rePrefix, page), hidden: url.Values{}, at: time.Now()}
	if f.prefix == "" {
		return f, ErrLayout
	}
	for _, in := range reHidden.FindAllString(page, -1) {
		if name := first(reName, in); name != "" {
			f.hidden.Set(name, html.UnescapeString(first(reValue, in)))
		}
	}
	formMu.Lock()
	formCache[u] = f
	formMu.Unlock()
	return f, nil
}

func searchVanban(ctx context.Context, fc *fetch.Client, text string, class, limit int, useCache bool) ([]Hit, error) {
	u := listURL(class)
	f, err := loadForm(ctx, fc, u, useCache)
	if err != nil {
		return nil, err
	}
	prefix := f.prefix
	pageSize := 500
	for _, n := range []int{50, 100, 200, 500} {
		if n >= limit {
			pageSize = n
			break
		}
	}
	form := url.Values{}
	for k, v := range f.hidden {
		form[k] = v
	}
	form.Set("__EVENTTARGET", "")
	form.Set("__EVENTARGUMENT", "")
	form.Set(prefix+"$txtSearchKeyword", text)
	form.Set(prefix+"$drdDocCategory", "0")
	form.Set(prefix+"$drdDocOrg", "0")
	form.Set(prefix+"$drdDocYear", "0")
	form.Set(prefix+"$drdRecordPerPage", strconv.Itoa(pageSize))
	form.Set(prefix+"$btnSearch", "Tìm kiếm")
	resp, err := fc.Get(ctx, u, fetch.Options{UsePost: true, Form: form})
	if err != nil {
		return nil, err
	}
	result := string(resp.Body)
	hits := ParseVanbanList(result, class)
	// A reply without rows and without the search form is not "no results".
	if len(hits) == 0 && !rePrefix.MatchString(result) {
		return nil, ErrLayout
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

// ResolveVanban finds the portal entry whose number equals code exactly after
// normalization. Handing back the wrong law is worse than handing back none,
// so near matches are never returned.
func ResolveVanban(ctx context.Context, fc *fetch.Client, code string) (*Hit, error) {
	want := textutil.NormalizeDocNumber(code)
	if want == "" {
		return nil, nil
	}
	// "13/2023/ND-CP" typed without the Vietnamese Đ finds nothing by itself,
	// so the "number/year" part is tried as well.
	queries := []string{code}
	if m := regexp.MustCompile(`^\s*\d+[^/]*/\d{4}`).FindString(code); m != "" && strings.TrimSpace(m) != strings.TrimSpace(code) {
		queries = append(queries, strings.TrimSpace(m))
	}
	for _, class := range []int{1, 2} {
		for _, q := range queries {
			hits, err := SearchVanban(ctx, fc, q, class, 100)
			if err != nil {
				return nil, err
			}
			for i := range hits {
				if textutil.NormalizeDocNumber(hits[i].DocNumber) == want {
					return &hits[i], nil
				}
			}
		}
	}
	return nil, nil
}

// VanbanInfo reads the detail page of a document.
func VanbanInfo(ctx context.Context, fc *fetch.Client, portalID string) (*DocInfo, error) {
	if _, err := strconv.Atoi(portalID); err != nil {
		return nil, fmt.Errorf("mã văn bản không hợp lệ: %q", portalID)
	}
	resp, err := fc.Get(ctx, detailURL(portalID), fetch.Options{})
	if err != nil {
		return nil, err
	}
	return ParseVanbanDetail(string(resp.Body), portalID)
}

// ParseVanbanDetail extracts the metadata of a detail page.
func ParseVanbanDetail(page, portalID string) (*DocInfo, error) {
	info := &DocInfo{PortalID: portalID, URL: detailURL(portalID), Meta: map[string]string{}}
	info.Title = textutil.StripTags(first(reTitle, page))
	for _, m := range reMetaRow.FindAllStringSubmatch(page, -1) {
		if strings.Contains(m[2], "view-file") {
			continue // the attachment row is read separately
		}
		label := textutil.StripTags(m[1])
		if label == "" {
			continue
		}
		val := textutil.StripTags(m[2])
		info.Meta[label] = val
		switch textutil.Fold(label) {
		case "so ky hieu":
			info.DocNumber = val
		case "ngay ban hanh":
			info.Issued = parseVNDate(val)
		case "loai van ban":
			info.DocType = val
		case "co quan ban hanh":
			info.Issuer = val
		case "nguoi ky":
			info.Signer = val
		case "trich yeu":
			info.Abstract = val
		}
	}
	info.Effective = parseVNDate(first(reEffective, page))
	seen := map[string]bool{}
	for _, m := range reViewFile.FindAllStringSubmatch(page, -1) {
		if f := html.UnescapeString(m[1]); !seen[f] {
			seen[f] = true
			info.FileURLs = append(info.FileURLs, f)
		}
	}
	if info.Title == "" && len(info.Meta) == 0 {
		return nil, fmt.Errorf("không tìm thấy văn bản có mã %s trên cổng", portalID)
	}
	if info.DocNumber == "" {
		return nil, ErrLayout
	}
	return info, nil
}

var unsafeName = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

// SafeName makes a string usable as a Windows file or folder name.
func SafeName(s string) string {
	s = strings.TrimRight(strings.TrimSpace(unsafeName.ReplaceAllString(s, "_")), ". ")
	if s == "" {
		return "van-ban"
	}
	return s
}

// DownloadFiles saves the attachments of a document under dir and returns
// the saved file names. Only https links on the Government's own domain are
// fetched, whatever the page says.
func DownloadFiles(ctx context.Context, fc *fetch.Client, fileURLs []string, dir string) (saved []string, problems []string) {
	for _, raw := range fileURLs {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "https" && !fc.Insecure) || !fc.Allowed(u.Host) {
			problems = append(problems, "bỏ qua (không phải liên kết https của cổng Chính phủ): "+raw)
			continue
		}
		name, _ := url.PathUnescape(path.Base(u.Path))
		name = SafeName(name)
		target := filepath.Join(dir, name)
		if fi, err := os.Stat(target); err == nil && fi.Size() > 0 {
			saved = append(saved, name)
			continue
		}
		resp, err := fc.Get(ctx, raw, fetch.Options{MaxBytes: fetch.MaxFile})
		if err != nil {
			problems = append(problems, fmt.Sprintf("không tải được %s: %v", name, err))
			continue
		}
		if strings.HasSuffix(strings.ToLower(name), ".pdf") && !strings.HasPrefix(string(resp.Body[:min(5, len(resp.Body))]), "%PDF-") {
			problems = append(problems, "tệp tải về không phải PDF hợp lệ: "+name)
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			problems = append(problems, err.Error())
			return
		}
		if err := os.WriteFile(target, resp.Body, 0o644); err != nil {
			problems = append(problems, err.Error())
			continue
		}
		saved = append(saved, name)
	}
	return
}

// GuessRelation reads the standard wording of an abstract to tell how a
// document acts on another one it names.
func GuessRelation(abstract string) string {
	f := textutil.Fold(abstract)
	switch {
	case strings.Contains(f, "van ban hop nhat") || strings.Contains(f, "hop nhat"):
		return "consolidates"
	case strings.Contains(f, "bai bo"):
		return "repeals"
	case strings.Contains(f, "thay the"):
		return "replaces"
	case strings.Contains(f, "sua doi") || strings.Contains(f, "bo sung"):
		return "amends"
	case strings.Contains(f, "quy dinh chi tiet") || strings.Contains(f, "huong dan"):
		return "guides"
	}
	return "mentions"
}

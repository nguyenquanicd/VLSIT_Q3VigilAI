package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/net/html/charset"

	"q3vnlaw/internal/fetch"
	"q3vnlaw/internal/store"
	"q3vnlaw/internal/textutil"
)

type rssConfig struct {
	URLs []string `json:"urls"`
	URL  string   `json:"url"`
}

func (c rssConfig) all() []string {
	if c.URL != "" {
		return append([]string{c.URL}, c.URLs...)
	}
	return c.URLs
}

type feedDoc struct {
	// RSS 2.0
	Items []feedItem `xml:"channel>item"`
	// Atom
	Entries []feedEntry `xml:"entry"`
}

type feedItem struct {
	Title   string `xml:"title"`
	Link    string `xml:"link"`
	GUID    string `xml:"guid"`
	Desc    string `xml:"description"`
	PubDate string `xml:"pubDate"`
	Date    string `xml:"http://purl.org/dc/elements/1.1/ date"`
}

type feedEntry struct {
	Title string `xml:"title"`
	Links []struct {
		Href string `xml:"href,attr"`
		Rel  string `xml:"rel,attr"`
	} `xml:"link"`
	Summary   string `xml:"summary"`
	Content   string `xml:"content"`
	Published string `xml:"published"`
	Updated   string `xml:"updated"`
}

var dateLayouts = []string{
	time.RFC1123Z, time.RFC1123, "Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04:05 MST",
	"Mon, 02 Jan 2006 15:04:05 Z07:00", "Mon, 02 Jan 06 15:04:05 -0700", time.RFC822Z, time.RFC822,
	time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "02/01/2006 15:04:05", "02/01/2006 15:04", "02/01/2006",
	"1/2/2006 3:04:05 PM",
}

// vn is the fallback zone for timestamps that carry none.
var vn = time.FixedZone("ICT", 7*3600)

func parseDate(s string) time.Time {
	// Some feeds put a narrow no-break space before AM/PM (seen live on
	// tuoitre.vn); every kind of space becomes a plain one.
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, s))
	if s == "" {
		return time.Time{}
	}
	// Some feeds write "GMT+7" where RFC 1123 wants "+0700".
	s = strings.NewReplacer("GMT+7", "+0700", "GMT+07:00", "+0700").Replace(s)
	for _, l := range dateLayouts {
		if t, err := time.ParseInLocation(l, s, vn); err == nil {
			return t
		}
	}
	return time.Time{}
}

// ParseFeed reads an RSS 2.0 or Atom document. base resolves relative links.
func ParseFeed(data []byte, base string) ([]Ref, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	dec.Entity = xml.HTMLEntity
	dec.CharsetReader = func(label string, in io.Reader) (io.Reader, error) { return charset.NewReaderLabel(label, in) }
	var doc feedDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("không đọc được RSS: %w", err)
	}
	baseURL, _ := url.Parse(base)
	resolve := func(link string) string {
		link = strings.TrimSpace(link)
		if link == "" {
			return ""
		}
		u, err := url.Parse(link)
		if err != nil {
			return ""
		}
		if baseURL != nil {
			u = baseURL.ResolveReference(u)
		}
		// Feeds still carrying http links are upgraded; the fetcher is
		// https-only. Loopback is left alone for the test servers.
		loopback := u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"
		if u.Scheme == "http" && !loopback {
			u.Scheme = "https"
		}
		if (u.Scheme != "https" && !loopback) || u.Host == "" {
			return ""
		}
		return u.String()
	}
	var out []Ref
	for _, it := range doc.Items {
		link := resolve(it.Link)
		if link == "" && strings.HasPrefix(strings.TrimSpace(it.GUID), "http") {
			link = resolve(it.GUID)
		}
		date := it.PubDate
		if date == "" {
			date = it.Date
		}
		if r, ok := makeRef(link, it.Title, it.Desc, date); ok {
			out = append(out, r)
		}
	}
	for _, e := range doc.Entries {
		link := ""
		for _, l := range e.Links {
			if l.Rel == "" || l.Rel == "alternate" {
				link = resolve(l.Href)
				break
			}
		}
		sum := e.Summary
		if sum == "" {
			sum = e.Content
		}
		date := e.Published
		if date == "" {
			date = e.Updated
		}
		if r, ok := makeRef(link, e.Title, sum, date); ok {
			out = append(out, r)
		}
	}
	return out, nil
}

func makeRef(link, title, summary, date string) (Ref, bool) {
	title = textutil.StripTags(title)
	if link == "" || title == "" {
		return Ref{}, false
	}
	norm, err := textutil.NormalizeURL(link)
	if err != nil {
		return Ref{}, false
	}
	return Ref{URL: norm, Title: title, Summary: textutil.Truncate(textutil.StripTags(summary), 600), Published: parseDate(date)}, true
}

func listRSS(ctx context.Context, fc *fetch.Client, src store.Source) (Listing, error) {
	var cfg rssConfig
	if err := json.Unmarshal([]byte(src.Config), &cfg); err != nil || len(cfg.all()) == 0 {
		return Listing{}, errors.New("nguồn RSS chưa có địa chỉ feed")
	}
	state := loadState(src.ETag)
	next := condState{}
	var out Listing
	seen := map[string]bool{}
	unchanged, failed := 0, 0
	var lastErr error
	urls := cfg.all()
	for _, u := range urls {
		prev := state[u]
		resp, err := fc.Get(ctx, u, fetch.Options{ETag: prev[0], LastModified: prev[1]})
		if err != nil {
			failed++
			lastErr = err
			continue
		}
		if resp.NotModified {
			unchanged++
			next[u] = prev
			continue
		}
		refs, err := ParseFeed(resp.Body, resp.URL)
		if err != nil {
			failed++
			lastErr = err
			continue
		}
		next[u] = [2]string{resp.ETag, resp.LastModified}
		for _, r := range refs {
			if !seen[r.URL] {
				seen[r.URL] = true
				out.Refs = append(out.Refs, r)
			}
		}
	}
	// One dead feed of several is reported; the source only fails as a whole
	// when none of its feeds could be read.
	if failed == len(urls) {
		return Listing{}, lastErr
	}
	out.State = next.String()
	out.NotModified = unchanged == len(urls)-failed && len(out.Refs) == 0
	return out, nil
}

// ---- htmllist --------------------------------------------------------------

type htmlListConfig struct {
	URL         string `json:"url"`
	LinkPattern string `json:"link_pattern"` // regular expression the article URL must match; empty = same host
}

// ParseLinkList collects the article links of a category page: anchors whose
// resolved URL matches pattern and whose text is long enough to be a headline.
func ParseLinkList(page []byte, base string, pattern *regexp.Regexp) []Ref {
	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return nil
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return nil
	}
	var out []Ref
	seen := map[string]bool{}
	var text func(*html.Node, *strings.Builder)
	text = func(n *html.Node, b *strings.Builder) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			text(c, b)
		}
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.A {
			href, title := "", ""
			for _, a := range n.Attr {
				switch a.Key {
				case "href":
					href = a.Val
				case "title":
					title = a.Val
				}
			}
			var b strings.Builder
			text(n, &b)
			label := strings.Join(strings.Fields(b.String()), " ")
			if len([]rune(label)) < len([]rune(title)) {
				label = strings.Join(strings.Fields(title), " ")
			}
			if u, err := url.Parse(strings.TrimSpace(href)); err == nil && href != "" && len([]rune(label)) >= 25 {
				abs := baseURL.ResolveReference(u)
				// https only, except loopback, which the test servers use.
				secure := abs.Scheme == "https" || abs.Hostname() == "127.0.0.1" || abs.Hostname() == "localhost"
				ok := secure && abs.Host == baseURL.Host && abs.Path != baseURL.Path
				if pattern != nil {
					ok = secure && pattern.MatchString(abs.String())
				}
				if norm, err := textutil.NormalizeURL(abs.String()); ok && err == nil && !seen[norm] {
					seen[norm] = true
					out = append(out, Ref{URL: norm, Title: label})
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out
}

func listHTML(ctx context.Context, fc *fetch.Client, src store.Source) (Listing, error) {
	var cfg htmlListConfig
	if err := json.Unmarshal([]byte(src.Config), &cfg); err != nil || cfg.URL == "" {
		return Listing{}, errors.New("nguồn trang danh mục chưa có địa chỉ")
	}
	var pattern *regexp.Regexp
	if cfg.LinkPattern != "" {
		p, err := regexp.Compile(cfg.LinkPattern)
		if err != nil {
			return Listing{}, fmt.Errorf("mẫu đường dẫn không hợp lệ: %w", err)
		}
		pattern = p
	}
	resp, err := fc.Get(ctx, cfg.URL, fetch.Options{})
	if err != nil {
		return Listing{}, err
	}
	return Listing{Refs: ParseLinkList(resp.Body, resp.URL, pattern)}, nil
}

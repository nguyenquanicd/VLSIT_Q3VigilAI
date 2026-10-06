package fetch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(domains ...string) *Client {
	c := New()
	c.Insecure = true
	c.RetryDelay = time.Millisecond
	c.SetAllowed(domains)
	return c
}

func TestBlocklist(t *testing.T) {
	for _, h := range []string{"facebook.com", "m.facebook.com", "www.voz.vn", "forum.example.vn", "diendan.luat.vn", "abc.blogspot.com", "t.me"} {
		if !Blocked(h) {
			t.Errorf("%s should be blocked", h)
		}
	}
	for _, h := range []string{"vanban.chinhphu.vn", "vnexpress.net", "blogger-news.vn", "xfacebook.com.vn"} {
		if Blocked(h) {
			t.Errorf("%s should not be blocked", h)
		}
	}
	// A blocked domain cannot be whitelisted.
	c := testClient("facebook.com", "chinhphu.vn")
	if c.Allowed("facebook.com") {
		t.Error("blocklisted domain was whitelisted")
	}
	if !c.Allowed("vanban.chinhphu.vn") || !c.Allowed("CHINHPHU.VN:443") {
		t.Error("subdomain of a whitelisted domain rejected")
	}
	if c.Allowed("chinhphu.vn.evil.com") || c.Allowed("notchinhphu.vn") {
		t.Error("look-alike host accepted")
	}
}

func TestWhitelistAndHTTPSOnly(t *testing.T) {
	c := New()
	c.SetAllowed([]string{"chinhphu.vn"})
	if _, err := c.Get(context.Background(), "https://example.com/", Options{}); !errors.Is(err, ErrBlocked) {
		t.Errorf("non-whitelisted host: %v", err)
	}
	if _, err := c.Get(context.Background(), "http://vanban.chinhphu.vn/", Options{}); !errors.Is(err, ErrBlocked) {
		t.Errorf("plain http: %v", err)
	}
}

func TestRedirectOutOfWhitelistIsBlocked(t *testing.T) {
	outside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("secret"))
	}))
	defer outside.Close()
	// httptest servers share 127.0.0.1, so "localhost" stands for the outside host.
	outsideURL := strings.Replace(outside.URL, "127.0.0.1", "localhost", 1)

	inside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/out":
			http.Redirect(w, r, outsideURL+"/x", http.StatusFound)
		case "/in":
			http.Redirect(w, r, "/final", http.StatusFound)
		case "/final":
			w.Write([]byte("ok"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer inside.Close()

	c := testClient("127.0.0.1")
	if _, err := c.Get(context.Background(), inside.URL+"/out", Options{}); !errors.Is(err, ErrBlocked) {
		t.Errorf("redirect to a non-whitelisted host was followed: %v", err)
	}
	resp, err := c.Get(context.Background(), inside.URL+"/in", Options{})
	if err != nil || string(resp.Body) != "ok" || !strings.HasSuffix(resp.URL, "/final") {
		t.Errorf("same-host redirect: %v %+v", err, resp)
	}
}

func TestRobotsConditionalAndLimits(t *testing.T) {
	var hits, flaky atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("User-agent: *\nDisallow: /private/\nAllow: /private/open\nDisallow: /*.zip$\n"))
		case "/feed":
			hits.Add(1)
			if r.Header.Get("If-None-Match") == `"v1"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			if got := r.Header.Get("User-Agent"); !strings.HasPrefix(got, "Q3VigilAI/") {
				t.Errorf("user agent: %q", got)
			}
			w.Header().Set("ETag", `"v1"`)
			w.Write([]byte("\xef\xbb\xbf<rss/>"))
		case "/big":
			w.Write([]byte(strings.Repeat("x", 2000)))
		case "/flaky":
			if flaky.Add(1) < 3 {
				http.Error(w, "busy", http.StatusBadGateway)
				return
			}
			w.Write([]byte("fine"))
		case "/gone":
			http.Error(w, "gone", http.StatusGone)
		case "/form":
			r.ParseForm()
			w.Write([]byte(r.Method + ":" + r.PostForm.Get("q")))
		default:
			w.Write([]byte("page"))
		}
	}))
	defer srv.Close()
	c := testClient("127.0.0.1")
	ctx := context.Background()

	if _, err := c.Get(ctx, srv.URL+"/private/x", Options{}); !errors.Is(err, ErrRobots) {
		t.Errorf("robots disallow ignored: %v", err)
	}
	if _, err := c.Get(ctx, srv.URL+"/private/open/page", Options{}); err != nil {
		t.Errorf("robots allow override: %v", err)
	}
	if _, err := c.Get(ctx, srv.URL+"/files/a.zip", Options{}); !errors.Is(err, ErrRobots) {
		t.Errorf("robots wildcard: %v", err)
	}

	resp, err := c.Get(ctx, srv.URL+"/feed", Options{})
	if err != nil || string(resp.Body) != "<rss/>" || resp.ETag != `"v1"` {
		t.Fatalf("feed: %v %+v", err, resp)
	}
	resp, err = c.Get(ctx, srv.URL+"/feed", Options{ETag: resp.ETag})
	if err != nil || !resp.NotModified {
		t.Errorf("conditional get: %v %+v", err, resp)
	}

	if _, err := c.Get(ctx, srv.URL+"/big", Options{MaxBytes: 1000}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("size cap: %v", err)
	}
	resp, err = c.Get(ctx, srv.URL+"/flaky", Options{})
	if err != nil || string(resp.Body) != "fine" || flaky.Load() != 3 {
		t.Errorf("retry on 5xx: %v, attempts=%d", err, flaky.Load())
	}
	before := hits.Load()
	if _, err := c.Get(ctx, srv.URL+"/gone", Options{}); err == nil {
		t.Error("4xx returned no error")
	}
	_ = before
	resp, err = c.Get(ctx, srv.URL+"/form", Options{UsePost: true, Form: url.Values{"q": {"thuế"}}})
	if err != nil || string(resp.Body) != "POST:thuế" {
		t.Errorf("post: %v %s", err, resp.Body)
	}
}

func TestRobotsParsing(t *testing.T) {
	r := parseRobots("User-agent: Googlebot\nDisallow: /\n\nUser-agent: *\nAllow: /\nDisallow: /admin\n")
	if !r.allows("/tin-tuc") || r.allows("/admin/x") {
		t.Error("star group not applied")
	}
	mine := parseRobots("User-agent: *\nDisallow: /\n\nUser-agent: Q3VigilAI\nDisallow: /x\n")
	if !mine.allows("/tin") || mine.allows("/x/1") {
		t.Error("own group should take precedence over *")
	}
	if !parseRobots("User-agent: *\nDisallow:\n").allows("/anything") {
		t.Error("empty Disallow must allow all")
	}
	var none *robots
	if !none.allows("/") {
		t.Error("nil robots must allow")
	}
}

func TestOfflineDetection(t *testing.T) {
	c := testClient("invalid")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, err := c.Get(ctx, "http://no-such-host.invalid/", Options{SkipRobots: true})
	if !IsOffline(err) {
		t.Errorf("DNS failure not classified as offline: %v", err)
	}
	if IsOffline(errors.New("HTTP 500")) || IsOffline(nil) {
		t.Error("false positive")
	}
}

type countSink struct {
	n       int64
	closed  bool
	aborted bool
}

func (s *countSink) Write(p []byte) (int, error) { s.n += int64(len(p)); return len(p), nil }
func (s *countSink) Close() error                { s.closed = true; return nil }
func (s *countSink) Abort()                      { s.aborted = true }

func TestSinkStreamsTheBodyInsteadOfBufferingIt(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/big":
			w.Write([]byte(strings.Repeat("x", 3<<20)))
		case "/flaky":
			if calls.Add(1) == 1 {
				http.Error(w, "busy", http.StatusBadGateway)
				return
			}
			w.Write([]byte("ok"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := testClient("127.0.0.1")
	ctx := context.Background()

	var sinks []*countSink
	open := func() (io.WriteCloser, error) { s := &countSink{}; sinks = append(sinks, s); return s, nil }
	resp, err := c.Get(ctx, srv.URL+"/big", Options{MaxBytes: 10 << 20, Sink: open})
	if err != nil || len(resp.Body) != 0 || resp.Size != 3<<20 || !sinks[0].closed || sinks[0].n != 3<<20 {
		t.Fatalf("streamed download: %v size=%d body=%d sink=%+v", err, resp.Size, len(resp.Body), sinks[0])
	}

	// Over the cap: the sink is told to throw its partial file away.
	sinks = nil
	if _, err := c.Get(ctx, srv.URL+"/big", Options{MaxBytes: 1 << 20, Sink: open}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("size cap with a sink: %v", err)
	}
	if len(sinks) != 1 || !sinks[0].aborted || sinks[0].closed {
		t.Errorf("oversized download not aborted: %+v", sinks)
	}

	// A retry after a server error starts a fresh sink; the failed attempt
	// never reached the sink at all.
	sinks = nil
	if resp, err := c.Get(ctx, srv.URL+"/flaky", Options{Sink: open}); err != nil || resp.Size != 2 || len(sinks) != 1 {
		t.Errorf("retry with a sink: %v %+v sinks=%d", err, resp, len(sinks))
	}
}

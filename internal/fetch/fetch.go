// Package fetch is the only way the app reaches the web for source data. It
// enforces the domain whitelist (including across redirects), robots.txt, a
// per-host rate limit and size caps.
package fetch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"
)

// UserAgent identifies the app honestly to the sites it reads.
const UserAgent = "Q3VNLaw/0.1 (Vietnamese legal update monitor; personal use)"

// Size caps for the two kinds of download.
const (
	MaxPage = 5 << 20
	MaxFile = 50 << 20
)

// ErrBlocked is returned for a host outside the whitelist or on the blocklist.
var ErrBlocked = errors.New("tên miền không nằm trong danh sách nguồn được phép")

// ErrRobots is returned when robots.txt disallows the path.
var ErrRobots = errors.New("robots.txt của trang không cho phép truy cập tự động đường dẫn này")

// ErrTooLarge is returned when a response exceeds its size cap.
var ErrTooLarge = errors.New("nội dung vượt quá giới hạn kích thước")

// blockedDomains are never fetched, whatever the user configures: social
// networks, forums and blog hosts are not sources of law.
var blockedDomains = []string{
	"facebook.com", "fb.com", "fb.watch", "messenger.com", "instagram.com", "threads.net", "zalo.me", "zaloapp.com",
	"tiktok.com", "youtube.com", "youtu.be", "twitter.com", "x.com", "reddit.com", "telegram.org", "t.me",
	"linkedin.com", "quora.com", "pinterest.com", "discord.com", "discord.gg",
	"voz.vn", "tinhte.vn", "webtretho.com", "otofun.net", "lamchame.com", "vozforums.com", "linkhay.com",
	"blogspot.com", "wordpress.com", "medium.com", "substack.com", "tumblr.com", "wixsite.com", "weebly.com",
}

// blockedLabels block any host that has one of these as a DNS label.
var blockedLabels = map[string]bool{"forum": true, "forums": true, "diendan": true, "blog": true, "blogs": true, "community": true}

func hostOf(h string) string {
	h = strings.ToLower(h)
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	return strings.TrimSuffix(h, ".")
}

func under(host, domain string) bool {
	return host == domain || strings.HasSuffix(host, "."+domain)
}

// Blocked reports whether a host is on the permanent blocklist.
func Blocked(host string) bool {
	h := hostOf(host)
	for _, d := range blockedDomains {
		if under(h, d) {
			return true
		}
	}
	for _, label := range strings.Split(h, ".") {
		if blockedLabels[label] {
			return true
		}
	}
	return false
}

// Client fetches pages from whitelisted hosts.
type Client struct {
	// Insecure allows plain http and skips the politeness delay. Tests only.
	Insecure bool
	// MinDelay is the minimum gap between two requests to the same host.
	MinDelay time.Duration
	// RetryDelay is the base back-off between attempts.
	RetryDelay time.Duration

	http *http.Client

	mu      sync.Mutex
	allowed []string
	hosts   map[string]*hostState
}

type hostState struct {
	mu       sync.Mutex // serializes requests to one host
	last     time.Time
	robots   *robots
	robotsAt time.Time
}

// New returns a client with an empty whitelist.
func New() *Client {
	c := &Client{MinDelay: time.Second, RetryDelay: 2 * time.Second, hosts: map[string]*hostState{}}
	jar, _ := cookiejar.New(nil)
	c.http = &http.Client{
		Timeout: 60 * time.Second,
		Jar:     jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			// A redirect must not be a way out of the whitelist.
			return c.check(req.URL)
		},
	}
	return c
}

// SetAllowed replaces the whitelist with the given domains. Subdomains of a
// whitelisted domain are allowed.
func (c *Client) SetAllowed(domains []string) {
	list := make([]string, 0, len(domains))
	for _, d := range domains {
		if d = hostOf(strings.TrimSpace(d)); d != "" && !Blocked(d) {
			list = append(list, d)
		}
	}
	c.mu.Lock()
	c.allowed = list
	c.mu.Unlock()
}

// Allowed reports whether the host may be fetched.
func (c *Client) Allowed(host string) bool {
	h := hostOf(host)
	if h == "" || Blocked(h) {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, d := range c.allowed {
		if under(h, d) {
			return true
		}
	}
	return false
}

func (c *Client) check(u *url.URL) error {
	if u.Scheme != "https" && !(c.Insecure && u.Scheme == "http") {
		return fmt.Errorf("%w: chỉ chấp nhận https (%s)", ErrBlocked, u.Redacted())
	}
	if !c.Allowed(u.Host) {
		return fmt.Errorf("%w: %s", ErrBlocked, u.Hostname())
	}
	return nil
}

func (c *Client) host(h string) *hostState {
	c.mu.Lock()
	defer c.mu.Unlock()
	hs := c.hosts[h]
	if hs == nil {
		hs = &hostState{}
		c.hosts[h] = hs
	}
	return hs
}

// Options tune one request.
type Options struct {
	ETag         string // sent as If-None-Match
	LastModified string // sent as If-Modified-Since
	MaxBytes     int64  // defaults to MaxPage
	Form         url.Values
	UsePost      bool
	SkipRobots   bool // robots.txt itself, and nothing else
}

// Response is a completed fetch.
type Response struct {
	Status       int
	Body         []byte
	ETag         string
	LastModified string
	NotModified  bool
	URL          string // after redirects
	ContentType  string
}

// Get fetches a URL from a whitelisted host.
func (c *Client) Get(ctx context.Context, rawURL string, opt Options) (*Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if err := c.check(u); err != nil {
		return nil, err
	}
	if opt.MaxBytes == 0 {
		opt.MaxBytes = MaxPage
	}
	hs := c.host(hostOf(u.Host))
	if !opt.SkipRobots {
		if !c.robotsFor(ctx, u, hs).allows(u.EscapedPath()) {
			return nil, fmt.Errorf("%w: %s", ErrRobots, u.Redacted())
		}
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.RetryDelay * time.Duration(attempt)):
			}
		}
		resp, retry, err := c.once(ctx, u, hs, opt)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !retry {
			break
		}
	}
	return nil, lastErr
}

// once performs a single attempt; retry reports whether trying again may help.
func (c *Client) once(ctx context.Context, u *url.URL, hs *hostState, opt Options) (resp *Response, retry bool, err error) {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	if !c.Insecure {
		if wait := c.MinDelay - time.Since(hs.last); wait > 0 {
			select {
			case <-ctx.Done():
				return nil, false, ctx.Err()
			case <-time.After(wait):
			}
		}
	}
	defer func() { hs.last = time.Now() }()

	method, body := http.MethodGet, io.Reader(nil)
	if opt.UsePost {
		method, body = http.MethodPost, strings.NewReader(opt.Form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept-Language", "vi-VN,vi;q=0.9")
	if opt.UsePost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if opt.ETag != "" {
		req.Header.Set("If-None-Match", opt.ETag)
	}
	if opt.LastModified != "" {
		req.Header.Set("If-Modified-Since", opt.LastModified)
	}
	r, err := c.http.Do(req)
	if err != nil {
		// A whitelist rejection inside a redirect is final; network errors are not.
		return nil, !errors.Is(err, ErrBlocked) && ctx.Err() == nil, err
	}
	defer r.Body.Close()
	out := &Response{Status: r.StatusCode, ETag: r.Header.Get("ETag"), LastModified: r.Header.Get("Last-Modified"),
		URL: r.Request.URL.String(), ContentType: r.Header.Get("Content-Type")}
	switch {
	case r.StatusCode == http.StatusNotModified:
		out.NotModified = true
		return out, false, nil
	case r.StatusCode >= 500:
		return nil, true, fmt.Errorf("HTTP %d từ %s", r.StatusCode, u.Hostname())
	case r.StatusCode >= 400:
		return nil, false, fmt.Errorf("HTTP %d từ %s", r.StatusCode, u.Hostname())
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, opt.MaxBytes+1))
	if err != nil {
		return nil, true, err
	}
	if int64(len(data)) > opt.MaxBytes {
		return nil, false, ErrTooLarge
	}
	out.Body = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	return out, false, nil
}

// IsOffline reports whether err looks like "this machine has no network"
// rather than "that site is broken".
func IsOffline(err error) bool {
	if err == nil {
		return false
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return true
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		return true
	}
	return false
}

// ---- robots.txt ------------------------------------------------------------

type robotsRule struct {
	allow bool
	path  string
}

type robots struct{ rules []robotsRule }

// allows applies the longest-match rule; Allow wins a tie.
func (r *robots) allows(path string) bool {
	if r == nil {
		return true
	}
	if path == "" {
		path = "/"
	}
	best, allow := -1, true
	for _, rule := range r.rules {
		if !robotsMatch(rule.path, path) {
			continue
		}
		if n := len(rule.path); n > best || (n == best && rule.allow) {
			best, allow = n, rule.allow
		}
	}
	return allow
}

// robotsMatch supports the two wildcards used in practice: "*" and a
// trailing "$".
func robotsMatch(pattern, path string) bool {
	anchored := strings.HasSuffix(pattern, "$")
	pattern = strings.TrimSuffix(pattern, "$")
	parts := strings.Split(pattern, "*")
	if !strings.HasPrefix(path, parts[0]) {
		return false
	}
	rest := path[len(parts[0]):]
	for i, p := range parts[1:] {
		last := i == len(parts)-2
		if last && anchored {
			return strings.HasSuffix(rest, p)
		}
		j := strings.Index(rest, p)
		if j < 0 {
			return false
		}
		rest = rest[j+len(p):]
	}
	return !anchored || len(parts) > 1 || rest == ""
}

// parseRobots keeps the rules of the group naming this app, or failing that
// the "*" group.
func parseRobots(body string) *robots {
	var mine, star []robotsRule
	var agents []string
	inRules := false
	for _, line := range strings.Split(body, "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, val = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(val)
		switch key {
		case "user-agent":
			if inRules {
				agents, inRules = nil, false
			}
			agents = append(agents, strings.ToLower(val))
		case "allow", "disallow":
			inRules = true
			if val == "" {
				continue // "Disallow:" with no path allows everything
			}
			rule := robotsRule{allow: key == "allow", path: val}
			for _, a := range agents {
				if a == "*" {
					star = append(star, rule)
				} else if a == "q3vnlaw" {
					mine = append(mine, rule)
				}
			}
		}
	}
	if len(mine) > 0 {
		return &robots{rules: mine}
	}
	return &robots{rules: star}
}

func (c *Client) robotsFor(ctx context.Context, u *url.URL, hs *hostState) *robots {
	c.mu.Lock()
	r, at := hs.robots, hs.robotsAt
	c.mu.Unlock()
	if !at.IsZero() && time.Since(at) < 24*time.Hour {
		return r
	}
	// A missing or unreadable robots.txt means no restrictions.
	r = &robots{}
	ru := *u
	ru.Path, ru.RawPath, ru.RawQuery, ru.Fragment = "/robots.txt", "", "", ""
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if resp, _, err := c.once(cctx, &ru, hs, Options{MaxBytes: 512 << 10, SkipRobots: true}); err == nil && resp.Status == 200 &&
		!strings.Contains(strings.ToLower(resp.ContentType), "html") {
		r = parseRobots(string(resp.Body))
	}
	c.mu.Lock()
	hs.robots, hs.robotsAt = r, time.Now()
	c.mu.Unlock()
	return r
}

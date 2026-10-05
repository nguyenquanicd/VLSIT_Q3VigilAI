// Package sources knows where legal news and documents come from and how to
// read each kind of source.
package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"q3vnlaw/internal/fetch"
	"q3vnlaw/internal/store"
)

// Ref is one entry found in a source listing.
type Ref struct {
	URL       string
	Title     string
	Summary   string
	Published time.Time // zero when the source gives no date

	// Set for official document listings.
	DocNumber string
	PortalID  string
	FileURLs  []string
}

// Listing is the result of reading a source once.
type Listing struct {
	Refs        []Ref
	NotModified bool
	State       string // opaque conditional-request state, stored in sources.etag
}

// List reads a source with the connector named in its definition.
func List(ctx context.Context, fc *fetch.Client, src store.Source) (Listing, error) {
	switch src.Connector {
	case "rss":
		return listRSS(ctx, fc, src)
	case "htmllist":
		return listHTML(ctx, fc, src)
	case "vanban":
		return listVanban(ctx, fc, src)
	}
	return Listing{}, fmt.Errorf("bộ nối không hỗ trợ: %s", src.Connector)
}

// Builtins returns the sources shipped with the app. Every feed address was
// checked against the live site on 2026-10-04; a site that changes its feed
// shows up as a failing or empty source, never silently.
func Builtins() []store.Source {
	rss := func(key, name, domain string, tier int, urls ...string) store.Source {
		cfg, _ := json.Marshal(map[string]any{"urls": urls})
		return store.Source{Key: key, Name: name, Domain: domain, Tier: tier, Kind: "press", Connector: "rss",
			Config: string(cfg), IntervalMinutes: 60, Enabled: true}
	}
	vanban := func(key, name string, class int) store.Source {
		return store.Source{Key: key, Name: name, Domain: "chinhphu.vn", Tier: 1, Kind: "official", Connector: "vanban",
			Config: fmt.Sprintf(`{"class":%d}`, class), IntervalMinutes: 180, Enabled: true}
	}
	return []store.Source{
		vanban("vanban-qppl", "Cổng Chính phủ – Văn bản quy phạm pháp luật", 1),
		vanban("vanban-cddh", "Cổng Chính phủ – Văn bản chỉ đạo điều hành", 2),
		rss("xaydungchinhsach", "Cổng Chính phủ – Xây dựng chính sách, pháp luật", "chinhphu.vn", 1,
			"https://xaydungchinhsach.chinhphu.vn/chinh-sach-moi.rss"),
		rss("baochinhphu", "Báo Điện tử Chính phủ – Chính sách mới", "baochinhphu.vn", 1,
			"https://baochinhphu.vn/chinh-sach-moi.rss"),
		rss("nhandan", "Báo Nhân Dân – Pháp luật", "nhandan.vn", 3, "https://nhandan.vn/rss/phapluat-1287.rss"),
		rss("vietnamplus", "VietnamPlus (TTXVN)", "vietnamplus.vn", 3, "https://www.vietnamplus.vn/rss/home.rss"),
		rss("vov", "VOV – Pháp luật, Kinh tế", "vov.vn", 3, "https://vov.vn/rss/phap-luat.rss", "https://vov.vn/rss/kinh-te.rss"),
		rss("vtv", "VTV Online", "vtv.vn", 3, "https://vtv.vn/rss/home.rss"),
		rss("vnexpress", "VnExpress – Pháp luật, Kinh doanh", "vnexpress.net", 3,
			"https://vnexpress.net/rss/phap-luat.rss", "https://vnexpress.net/rss/kinh-doanh.rss"),
		rss("tuoitre", "Tuổi Trẻ – Pháp luật, Kinh doanh", "tuoitre.vn", 3,
			"https://tuoitre.vn/rss/phap-luat.rss", "https://tuoitre.vn/rss/kinh-doanh.rss"),
		rss("thanhnien", "Thanh Niên – Pháp luật, Kinh tế", "thanhnien.vn", 3,
			"https://thanhnien.vn/rss/thoi-su/phap-luat.rss", "https://thanhnien.vn/rss/kinh-te.rss"),
		rss("dantri", "Dân trí – Pháp luật, Kinh doanh", "dantri.com.vn", 3,
			"https://dantri.com.vn/rss/phap-luat.rss", "https://dantri.com.vn/rss/kinh-doanh.rss"),
		rss("vietnamnet", "VietNamNet – Pháp luật", "vietnamnet.vn", 3, "https://vietnamnet.vn/rss/phap-luat.rss"),
		rss("vneconomy", "VnEconomy", "vneconomy.vn", 3, "https://vneconomy.vn/tin-moi.rss"),
		rss("thoibaotaichinh", "Thời báo Tài chính Việt Nam", "thoibaotaichinhvietnam.vn", 3,
			"https://thoibaotaichinhvietnam.vn/rss_feed/trang-chu"),
	}
}

// Sync installs the built-in sources into the database and removes built-ins
// that are no longer shipped.
func Sync(st *store.Store) error {
	list := Builtins()
	keys := make([]string, 0, len(list))
	for _, src := range list {
		if src.Kind == "official" {
			src.IntervalMinutes = st.SettingInt("interval_official")
		} else {
			src.IntervalMinutes = st.SettingInt("interval_press")
		}
		if err := st.UpsertBuiltin(src); err != nil {
			return err
		}
		keys = append(keys, src.Key)
	}
	return st.RemoveBuiltinsExcept(keys)
}

// OfficialDomains are always whitelisted: verification and document downloads
// go to the Government portal even when the user disabled its listings.
var OfficialDomains = []string{"chinhphu.vn"}

// AllowedDomains returns the whitelist implied by the enabled sources.
func AllowedDomains(list []store.Source) []string {
	seen := map[string]bool{}
	var out []string
	add := func(d string) {
		d = strings.ToLower(strings.TrimSpace(d))
		if d != "" && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	for _, d := range OfficialDomains {
		add(d)
	}
	for _, src := range list {
		if src.Enabled {
			add(src.Domain)
		}
	}
	return out
}

// condState is the per-URL conditional request state of a multi-feed source.
type condState map[string][2]string // url -> {etag, last-modified}

func loadState(raw string) condState {
	st := condState{}
	json.Unmarshal([]byte(raw), &st)
	return st
}

func (c condState) String() string {
	b, _ := json.Marshal(c)
	return string(b)
}

package sources

import (
	"context"
	"os"
	"testing"
	"time"

	"q3vigilai/internal/fetch"
)

// The live tests reach the real sites. They are skipped unless Q3_LIVE=1,
// and are the way to find out which built-in source changed its feed or its
// page layout: run `.\build.ps1 -Live`.
func liveClient(t *testing.T) *fetch.Client {
	t.Helper()
	if os.Getenv("Q3_LIVE") != "1" {
		t.Skip("set Q3_LIVE=1 to run against the real sources")
	}
	fc := fetch.New()
	fc.SetAllowed(AllowedDomains(Builtins()))
	return fc
}

func TestLiveBuiltinSources(t *testing.T) {
	fc := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	for _, src := range Builtins() {
		l, err := List(ctx, fc, src)
		if err != nil {
			t.Errorf("%s (%s): %v", src.Name, src.Key, err)
			continue
		}
		if len(l.Refs) == 0 {
			t.Errorf("%s (%s): the source answered but listed nothing", src.Name, src.Key)
			continue
		}
		dated := 0
		for _, r := range l.Refs {
			if !r.Published.IsZero() {
				dated++
			}
		}
		// Undated items cannot be filtered by the look-back window.
		if dated == 0 {
			t.Errorf("%s (%s): no item carries a readable date", src.Name, src.Key)
		}
		t.Logf("%-52s %4d items, %4d dated, newest: %s", src.Name, len(l.Refs), dated, l.Refs[0].Title)
	}
}

func TestLivePortalSearch(t *testing.T) {
	fc := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	// Typed without the Vietnamese Đ, as users often do.
	hit, err := ResolveVanban(ctx, fc, "13/2023/ND-CP")
	if err != nil {
		t.Fatal(err)
	}
	if hit == nil {
		t.Fatal("13/2023/NĐ-CP was not found on the Government portal")
	}
	info, err := VanbanInfo(ctx, fc, hit.PortalID)
	if err != nil {
		t.Fatal(err)
	}
	if info.DocType == "" || info.Issued.IsZero() || len(info.FileURLs) == 0 {
		t.Errorf("detail page parsed incompletely: %+v", info)
	}
	t.Logf("%s | %s | issued %s | effective %s | %d file(s)", info.DocNumber, info.Title, info.Issued.Format("2006-01-02"),
		info.Effective.Format("2006-01-02"), len(info.FileURLs))
	if miss, err := ResolveVanban(ctx, fc, "99999/2026/NĐ-CP"); err != nil || miss != nil {
		t.Errorf("a number that does not exist returned %+v, %v", miss, err)
	}
}

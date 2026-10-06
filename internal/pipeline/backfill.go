package pipeline

import (
	"context"
	"net/url"
	"time"

	"q3vigilai/internal/i18n"
	"q3vigilai/internal/sources"
	"q3vigilai/internal/store"
)

// Backfill applies one topic to the items already collected inside the
// look-back window. Without it a new or edited topic would only see items
// that arrive after it was saved: everything stored earlier is skipped by the
// scan as "already seen". It runs as a scan of its own, so it shows in the
// scan log and cannot overlap a regular scan.
func (e *Engine) Backfill(ctx context.Context, topicID int64) (store.Scan, error) {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return store.Scan{}, ErrBusy
	}
	e.running = true
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		e.running = false
		e.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	t, err := e.St.Topic(topicID)
	if err != nil {
		return store.Scan{}, err
	}
	id, err := e.St.StartScan("backfill")
	if err != nil {
		return store.Scan{}, err
	}
	sc := store.Scan{ID: id, Trigger: "backfill", StartedAt: store.Now(), Errors: []store.SourceError{},
		Note: i18n.T("Áp dụng chủ đề \"{0}\" lên các tin đã thu thập", t.Name)}
	e.emit("scan.started", map[string]any{"id": id, "trigger": "backfill"})
	defer func() {
		e.St.FinishScan(sc)
		sc.FinishedAt = store.Now()
		e.emit("scan.finished", sc)
	}()
	if !t.Enabled {
		return sc, nil
	}

	all, err := e.St.Sources()
	if err != nil {
		return sc, err
	}
	e.Fetch.SetAllowed(sources.AllowedDomains(all))
	byID := map[int64]store.Source{}
	for _, s := range all {
		byID[s.ID] = s
	}
	now := e.now()
	lookback := now.AddDate(0, 0, -e.St.SettingInt("lookback_days"))
	items, err := e.St.RecentItems(lookback, 5000)
	if err != nil {
		return sc, err
	}
	st := &scanState{sc: &sc, provider: e.provider(), aiBudget: e.St.SettingInt("ai_max_calls")}
	// Oldest first, so that alerts come out in the order the news did.
	for i := len(items) - 1; i >= 0 && ctx.Err() == nil; i-- {
		it := items[i]
		src, ok := byID[it.SourceID]
		if !ok {
			continue
		}
		if pub := store.ParseTime(it.PublishedAt); !pub.IsZero() && pub.Before(lookback) {
			continue
		}
		m := MatchTopic(t, src.ID, src.Kind, it.Title+". "+it.Summary)
		if !m.OK() || e.St.ItemAlerted(t.ID, it.ID) {
			continue // not a candidate, or already raised under an earlier version of the topic
		}
		fi := &fresh{item: it, src: src}
		if src.Connector == "vanban" {
			if u, err := url.Parse(it.URL); err == nil {
				fi.ref.PortalID = u.Query().Get("docid")
			}
			if len(it.DocNumbers) > 0 {
				fi.ref.DocNumber = it.DocNumbers[0]
			}
		}
		e.enrich(ctx, fi)
		e.raise(ctx, st, fi, t, m)
	}
	budget := e.St.SettingInt("verify_max_per_scan")
	e.verifyNew(ctx, st.verify, &budget)
	if len(t.WatchedDocs) > 0 {
		e.checkWatched(ctx, st)
	}
	if st.provider == nil && e.AIError() != "" {
		sc.Note += ". " + i18n.T("AI không dùng được: {0}", e.AIError())
	}
	return sc, nil
}

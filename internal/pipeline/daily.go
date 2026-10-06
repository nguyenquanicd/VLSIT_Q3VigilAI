package pipeline

import (
	"context"
	"regexp"
	"strings"
	"time"

	"q3vigilai/internal/i18n"
	"q3vigilai/internal/sources"
	"q3vigilai/internal/store"
	"q3vigilai/internal/textutil"
)

// verifyGiveUp is how long an unconfirmed document number keeps being looked
// for on the official portal.
const verifyGiveUp = 30 * 24 * time.Hour

// confirm looks one document number up, first among stored documents, then
// on the Government portal. On success the alert is marked confirmed.
//
// sole says the alert is about this one document; only then does the
// document's effective date become the alert's. When a report names several
// documents, stamping it with the date of whichever was found first would
// present a guess as a fact.
func (e *Engine) confirm(ctx context.Context, alertID int64, number string, notify, sole bool) (bool, error) {
	eff := func(d store.Document) string {
		if sole {
			return d.EffectiveAt
		}
		return ""
	}
	if d, err := e.St.DocumentByNumber(number); err == nil {
		return true, e.St.ConfirmAlert(alertID, d.ID, eff(d), notify)
	}
	hit, err := sources.ResolveVanban(ctx, e.Fetch, number)
	if err != nil || hit == nil {
		return false, err
	}
	d, _, err := e.SaveOfficial(ctx, hit.PortalID, 0)
	if err != nil {
		return false, err
	}
	return true, e.St.ConfirmAlert(alertID, d.ID, eff(d), notify)
}

// verifyNew checks the press alerts raised by this scan. What cannot be
// confirmed now is queued and looked for again every day.
func (e *Engine) verifyNew(ctx context.Context, checks []pendingCheck, budget *int) {
	now := e.now()
	for _, c := range checks {
		confirmed := false
		for _, n := range c.numbers {
			if !IsRecent(n, now) {
				// An old law cited in passing: confirm only if already stored.
				if d, err := e.St.DocumentByNumber(n); err == nil && !confirmed {
					e.St.ConfirmAlert(c.alertID, d.ID, "", false)
					confirmed = true
				}
				continue
			}
			if confirmed {
				break
			}
			if *budget <= 0 || ctx.Err() != nil {
				e.St.QueueVerify(c.alertID, n, textutil.NormalizeDocNumber(n), now)
				continue
			}
			*budget--
			ok, err := e.confirm(ctx, c.alertID, n, false, len(c.numbers) == 1)
			if ok {
				confirmed = true
				continue
			}
			next := now.Add(24 * time.Hour)
			if err != nil {
				next = now.Add(2 * time.Hour) // the portal failed; that says nothing about the document
			}
			e.St.QueueVerify(c.alertID, n, textutil.NormalizeDocNumber(n), next)
		}
	}
}

// verifyDue re-checks queued document numbers whose time has come.
func (e *Engine) verifyDue(ctx context.Context, budget *int) {
	if *budget <= 0 {
		return
	}
	due, err := e.St.DueVerifications(*budget)
	if err != nil {
		return
	}
	now := e.now()
	for _, p := range due {
		if ctx.Err() != nil {
			return
		}
		if now.Sub(store.ParseTime(p.CreatedAt)) > verifyGiveUp {
			e.St.DropVerify(p.ID)
			continue
		}
		*budget--
		sole := false
		if a, err := e.St.Alert(p.AlertID); err == nil {
			sole = len(a.DocNumbers) == 1
		}
		ok, err := e.confirm(ctx, p.AlertID, p.DocNumber, true, sole)
		switch {
		case ok:
			e.St.DropVerifyByNorm(p.Norm)
			e.emit("alert.created", map[string]any{"id": p.AlertID, "confirmed": true})
		case err != nil:
			e.St.RescheduleVerify(p.ID, now.Add(2*time.Hour))
		default:
			e.St.RescheduleVerify(p.ID, now.Add(24*time.Hour))
		}
	}
}

// daily runs the once-a-day jobs.
func (e *Engine) daily(ctx context.Context, st *scanState) {
	e.checkWatched(ctx, st)
	e.remindEffective(st)
	keep := e.St.SettingInt("retention_days")
	if keep < 1 {
		keep = 30
	}
	e.St.PurgeItems(e.now().AddDate(0, 0, -keep))
	e.St.PurgeScans(500)
}

// checkWatched searches the portal for documents that name a watched
// document. The listing scan only sees the newest rows of the portal; this
// search also finds amendments that scrolled past between two scans.
func (e *Engine) checkWatched(ctx context.Context, st *scanState) {
	topics, _ := e.St.Topics(true)
	now := e.now()
	srcID := e.officialSourceID()
	for _, t := range topics {
		for _, watched := range t.WatchedDocs {
			if ctx.Err() != nil {
				return
			}
			norm := textutil.NormalizeDocNumber(watched)
			if norm == "" {
				continue
			}
			// The portal matches text literally, so a number typed as
			// "13/2023/ND-CP" finds nothing. Search on the "13/2023" part and
			// keep the rows that really carry or name the watched number.
			query := watched
			if m := reNumberYear.FindString(watched); m != "" {
				query = strings.TrimSpace(m)
			}
			var hits []sources.Hit
			failed := false
			for _, class := range []int{1, 2} {
				found, err := sources.SearchVanban(ctx, e.Fetch, query, class, 200)
				if err != nil {
					failed = true
					break
				}
				for _, h := range found {
					if mentions(h, norm) {
						hits = append(hits, h)
					}
				}
			}
			if failed {
				continue // try again tomorrow rather than baseline on partial data
			}
			baselined := e.St.WatchedBaselined(t.ID, norm)
			for _, h := range hits {
				if textutil.NormalizeDocNumber(h.DocNumber) == norm {
					// The watched document itself: make sure it is in the library.
					if _, err := e.St.DocumentByNumber(watched); err != nil {
						e.SaveOfficial(ctx, h.PortalID, srcID)
					}
					continue
				}
				seen, err := e.St.WatchedHitSeen(t.ID, norm, h.PortalID)
				if err != nil || seen || !baselined {
					continue // the first check only records what already exists
				}
				e.raiseWatched(ctx, st, t, watched, h, srcID, now)
			}
			e.St.WatchedHitSeen(t.ID, norm, "_baseline")
		}
	}
}

var reNumberYear = regexp.MustCompile(`^\s*\d+[^/]*/\d{4}`)

// mentions reports whether a portal row is the document with the given
// normalized number or names it in its abstract.
func mentions(h sources.Hit, norm string) bool {
	if textutil.NormalizeDocNumber(h.DocNumber) == norm {
		return true
	}
	for _, n := range textutil.ExtractDocNumbers(h.Abstract) {
		if textutil.NormalizeDocNumber(n) == norm {
			return true
		}
	}
	return false
}

var relationLabel = map[string]string{
	"amends": i18n.N("sửa đổi, bổ sung"), "replaces": i18n.N("thay thế"), "repeals": i18n.N("bãi bỏ"), "guides": i18n.N("quy định chi tiết, hướng dẫn"),
	"consolidates": i18n.N("hợp nhất"), "mentions": i18n.N("có nhắc tới"),
}

func (e *Engine) raiseWatched(ctx context.Context, st *scanState, t store.Topic, watched string, h sources.Hit, srcID int64, now time.Time) {
	key := "doc:" + textutil.NormalizeDocNumber(h.DocNumber)
	if _, ok := e.St.ClusterAlert(t.ID, key, now.Add(-clusterWindow)); ok {
		return // the listing scan already raised it
	}
	itemID := e.St.ItemID(h.URL)
	if itemID == 0 {
		it := store.Item{SourceID: srcID, URL: h.URL, Title: h.DocNumber + " – " + h.Abstract, Summary: h.Abstract,
			DocNumbers: []string{h.DocNumber}}
		if !h.Issued.IsZero() {
			it.PublishedAt = store.FormatTime(h.Issued)
		}
		itemID, _ = e.St.AddItem(it)
	}
	a := store.Alert{TopicID: t.ID, Kind: "doc_changed", Severity: "warning", LegalStatus: "issued", Verified: "confirmed",
		Title: h.DocNumber + " – " + h.Abstract, MatchedKeywords: []string{watched}, DocNumbers: []string{h.DocNumber},
		ClusterKey: key, PrimaryItemID: itemID, State: "unread", NeedsNotify: true}
	rel := sources.GuessRelation(h.Abstract)
	a.Summary = i18n.T("Văn bản {0} ({1}) trên Cổng Chính phủ {2} văn bản đang theo dõi {3}.", h.DocNumber,
		h.Issued.Format("02/01/2006"), i18n.T(relationLabel[rel]), watched)
	a.Reason = i18n.T("Quan hệ được suy ra từ câu chữ của trích yếu, cần đọc văn bản để khẳng định.")
	if d, _, err := e.SaveOfficial(ctx, h.PortalID, srcID); err == nil {
		a.DocumentID, a.EffectiveAt = d.ID, d.EffectiveAt
		if d.EffectiveAt != "" && d.EffectiveAt <= now.Format("2006-01-02") {
			a.LegalStatus = "effective"
		}
	}
	if id, err := e.St.AddAlert(a); err == nil {
		st.sc.AlertsNew++
		e.emit("alert.created", map[string]any{"id": id, "severity": a.Severity})
	}
}

// remindEffective raises a reminder for each alerted document that takes
// effect within the topic's reminder window.
func (e *Engine) remindEffective(st *scanState) {
	pairs, err := e.St.AlertedDocuments()
	if err != nil {
		return
	}
	now := e.now()
	today := now.Format("2006-01-02")
	for _, p := range pairs {
		t, err := e.St.Topic(p.TopicID)
		if err != nil || !t.Enabled || t.RemindDays <= 0 {
			continue
		}
		d, err := e.St.Document(p.DocumentID)
		if err != nil || d.EffectiveAt == "" || d.EffectiveAt <= today {
			continue
		}
		if d.EffectiveAt > now.AddDate(0, 0, t.RemindDays).Format("2006-01-02") || e.St.EffectiveSoonExists(t.ID, d.ID) {
			continue
		}
		eff, _ := time.Parse("2006-01-02", d.EffectiveAt)
		a := store.Alert{TopicID: t.ID, Kind: "effective_soon", Severity: "notice", LegalStatus: "issued", Verified: "confirmed",
			Title:   i18n.T("Sắp có hiệu lực từ {0}: {1}", eff.Format("02/01/2006"), d.DocNumber),
			Summary: strings.TrimSpace(d.Title), EffectiveAt: d.EffectiveAt, DocNumbers: []string{d.DocNumber},
			DocumentID: d.ID, ClusterKey: "effective:" + d.Norm, State: "unread", NeedsNotify: true}
		if id, err := e.St.AddAlert(a); err == nil {
			st.sc.AlertsNew++
			e.emit("alert.created", map[string]any{"id": id, "severity": a.Severity})
		}
	}
}

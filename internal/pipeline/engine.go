package pipeline

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"q3vigilai/internal/ai"
	"q3vigilai/internal/extract"
	"q3vigilai/internal/fetch"
	"q3vigilai/internal/i18n"
	"q3vigilai/internal/sources"
	"q3vigilai/internal/store"
	"q3vigilai/internal/textutil"
)

// ErrBusy is returned when a scan is requested while one is running.
var ErrBusy = i18n.Err("đang có một lượt quét chạy")

// clusterWindow is how long reports of the same event keep merging into one alert.
const clusterWindow = 14 * 24 * time.Hour

// aiTextLimit caps the text handed to the classifier, in runes.
const aiTextLimit = 12000

// Engine runs scans.
type Engine struct {
	St      *store.Store
	Fetch   *fetch.Client
	DataDir string
	// Provider returns the AI provider currently configured, or nil.
	Provider func() ai.Provider
	// Emit publishes a live event to the UI. May be nil.
	Emit func(event string, data any)
	// Now is the clock; tests replace it.
	Now func() time.Time

	mu             sync.Mutex
	running        bool
	aiBlockedUntil time.Time
	aiError        string
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *Engine) emit(event string, data any) {
	if e.Emit != nil {
		e.Emit(event, data)
	}
}

// Running reports whether a scan is in progress.
func (e *Engine) Running() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

// AIError returns the last AI failure that made the engine fall back to
// keyword-only mode, or "".
func (e *Engine) AIError() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.aiError
}

// ResetAI clears a remembered AI failure, e.g. after the settings changed.
func (e *Engine) ResetAI() {
	e.mu.Lock()
	e.aiBlockedUntil, e.aiError = time.Time{}, ""
	e.mu.Unlock()
}

// ReportAI records the outcome of an AI check made outside a scan (at
// start-up, or from the settings screen), so the UI shows a provider that
// does not work as not working instead of waiting for the next scan to fail.
func (e *Engine) ReportAI(err error) {
	if err == nil {
		e.ResetAI()
		return
	}
	d := 10 * time.Minute
	if errors.Is(err, ai.ErrAuth) {
		d = 30 * time.Minute
	}
	e.blockAI(d, err)
}

func (e *Engine) provider() ai.Provider {
	if e.Provider == nil {
		return nil
	}
	e.mu.Lock()
	blocked := e.now().Before(e.aiBlockedUntil)
	e.mu.Unlock()
	if blocked {
		return nil
	}
	return e.Provider()
}

func (e *Engine) blockAI(d time.Duration, err error) {
	e.mu.Lock()
	e.aiBlockedUntil, e.aiError = e.now().Add(d), err.Error()
	e.mu.Unlock()
	e.emit("provider.changed", map[string]string{"error": err.Error()})
}

// fresh is a newly stored item on its way through the pipeline.
type fresh struct {
	item store.Item
	ref  sources.Ref
	src  store.Source
	text string          // full text used for classification and search
	doc  *store.Document // set for official documents
	done bool            // enrich has run
}

// scanState is the per-scan budget and bookkeeping.
type scanState struct {
	sc         *store.Scan
	provider   ai.Provider
	aiBudget   int
	aiFailures int
	verify     []pendingCheck
}

type pendingCheck struct {
	alertID int64
	numbers []string
}

// Scan runs the pipeline once over the enabled sources, or over only the
// given source ids.
func (e *Engine) Scan(ctx context.Context, trigger string, only []int64) (store.Scan, error) {
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
	id, err := e.St.StartScan(trigger)
	if err != nil {
		return store.Scan{}, err
	}
	sc := store.Scan{ID: id, Trigger: trigger, StartedAt: store.Now(), Errors: []store.SourceError{}}
	e.emit("scan.started", map[string]any{"id": id, "trigger": trigger})
	defer func() {
		e.St.FinishScan(sc)
		sc.FinishedAt = store.Now()
		e.emit("scan.finished", sc)
	}()

	all, err := e.St.Sources()
	if err != nil {
		return sc, err
	}
	e.Fetch.SetAllowed(sources.AllowedDomains(all))
	var todo []store.Source
	for _, s := range all {
		if s.Enabled && (only == nil || slices.Contains(only, s.ID)) {
			todo = append(todo, s)
		}
	}
	topics, err := e.St.Topics(true)
	if err != nil {
		return sc, err
	}

	// 1. List every source, a few at a time. Requests to one host are
	// serialized and spaced by the fetch client.
	type listed struct {
		l   sources.Listing
		err error
	}
	results := make([]listed, len(todo))
	var wg sync.WaitGroup
	// Two at a time: each parsed feed is a few megabytes, and a scan that takes
	// a few seconds longer is no loss for a program that runs once an hour.
	sem := make(chan struct{}, 2)
	var done int
	var dmu sync.Mutex
	for i := range todo {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i].l, results[i].err = sources.List(ctx, e.Fetch, todo[i])
			dmu.Lock()
			done++
			e.emit("scan.progress", map[string]any{"phase": "list", "done": done, "total": len(todo)})
			dmu.Unlock()
		}(i)
	}
	wg.Wait()

	// No network at all is not a fault of the sources: skip the round
	// without counting failures against them.
	offline := 0
	for _, r := range results {
		if fetch.IsOffline(r.err) {
			offline++
		}
	}
	if len(todo) > 0 && offline == len(todo) {
		sc.Note = i18n.T("Không có kết nối mạng, bỏ lượt quét này")
		return sc, nil
	}

	now := e.now()
	lookback := now.AddDate(0, 0, -e.St.SettingInt("lookback_days"))
	var items []*fresh
	for i, src := range todo {
		r := results[i]
		if r.err != nil {
			sc.SourcesFailed++
			sc.Errors = append(sc.Errors, store.SourceError{Source: src.Name, Error: r.err.Error()})
			e.St.RecordSourceScan(src.ID, store.ScanOutcome{Status: "error", Error: r.err.Error()})
			continue
		}
		sc.SourcesOK++
		switch {
		case r.l.NotModified:
			e.St.RecordSourceScan(src.ID, store.ScanOutcome{Status: "unchanged", ETag: r.l.State})
			continue
		case len(r.l.Refs) == 0:
			// A source that answers but lists nothing is suspicious: its
			// page layout may have changed. It is tracked, not ignored.
			e.St.RecordSourceScan(src.ID, store.ScanOutcome{Status: "empty"})
			continue
		}
		firstScan := src.LastOKAt == ""
		for _, ref := range r.l.Refs {
			if !ref.Published.IsZero() && ref.Published.Before(lookback) {
				continue
			}
			if e.St.ItemSeen(ref.URL) {
				continue
			}
			it := store.Item{SourceID: src.ID, URL: ref.URL, Title: ref.Title, Summary: ref.Summary,
				DocNumbers: textutil.ExtractDocNumbers(ref.Title + " " + ref.Summary)}
			if !ref.Published.IsZero() {
				it.PublishedAt = store.FormatTime(ref.Published)
			}
			if it.ID, err = e.St.AddItem(it); err != nil || it.ID == 0 {
				continue
			}
			sc.ItemsNew++
			// The undated backlog of a source seen for the first time is
			// recorded but not alerted on: it is old news of unknown age.
			if ref.Published.IsZero() && firstScan {
				continue
			}
			items = append(items, &fresh{item: it, ref: ref, src: src})
		}
		e.St.RecordSourceScan(src.ID, store.ScanOutcome{Status: "ok", ETag: r.l.State})
	}

	// 2. Match without AI, then judge the matches.
	if len(topics) == 0 {
		sc.Note = i18n.T("Chưa có chủ đề theo dõi nào đang bật")
	}
	st := &scanState{sc: &sc, provider: e.provider(), aiBudget: e.St.SettingInt("ai_max_calls")}
	for n, fi := range items {
		if ctx.Err() != nil {
			break
		}
		text := fi.item.Title + ". " + fi.item.Summary
		for _, t := range topics {
			m := MatchTopic(t, fi.src.ID, fi.src.Kind, text)
			if !m.OK() {
				continue
			}
			e.enrich(ctx, fi)
			e.raise(ctx, st, fi, t, m)
		}
		if n%10 == 0 {
			e.emit("scan.progress", map[string]any{"phase": "match", "done": n + 1, "total": len(items)})
		}
	}

	// 3. Confirm press reports against the official portal.
	budget := e.St.SettingInt("verify_max_per_scan")
	e.verifyNew(ctx, st.verify, &budget)
	e.verifyDue(ctx, &budget)

	// 4. Once a day: watched documents, effective-date reminders, clean-up.
	if last := store.ParseTime(e.St.Setting("last_daily_at")); now.Sub(last) > 20*time.Hour && only == nil {
		e.daily(ctx, st)
		e.St.SetSettings(map[string]string{"last_daily_at": store.FormatTime(now)})
	}
	if st.provider == nil && e.AIError() != "" {
		sc.Note = strings.TrimSpace(sc.Note + " " + i18n.T("AI không dùng được: {0}", e.AIError()))
	}
	return sc, nil
}

// enrich loads what the listing did not give: the article body, or the
// official document with its files.
func (e *Engine) enrich(ctx context.Context, fi *fresh) {
	if fi.done {
		return
	}
	fi.done = true
	fi.text = fi.item.Summary
	if fi.src.Connector == "vanban" && fi.ref.PortalID != "" {
		doc, text, err := e.SaveOfficial(ctx, fi.ref.PortalID, fi.src.ID)
		if err == nil {
			fi.doc = &doc
			fi.item.Title = doc.DocNumber + " – " + doc.Title
			fi.text = strings.TrimSpace(doc.Title + "\n" + text)
		}
	} else if resp, err := e.Fetch.Get(ctx, fi.item.URL, fetch.Options{}); err == nil {
		if body := textutil.ArticleText(string(resp.Body)); len([]rune(body)) > 200 {
			fi.text = body
			e.St.ReplaceChunks("item", fi.item.ID, extract.ParagraphChunks(fi.item.Title+"\n"+body, ""))
		}
	}
	if fi.text == "" {
		fi.text = fi.item.Title
	}
	fi.item.ContentHash = textutil.Hash(fi.item.Title + "\n" + fi.text)
	fi.item.DocNumbers = textutil.ExtractDocNumbers(fi.item.Title + " " + fi.text)
	e.St.SetItemContent(fi.item.ID, fi.text, fi.item.ContentHash, fi.item.DocNumbers)
}

// raise turns a matched item into an alert, or merges it into the alert
// already raised for the same event.
func (e *Engine) raise(ctx context.Context, st *scanState, fi *fresh, t store.Topic, m Match) {
	now := e.now()
	official := fi.src.Kind == "official"
	a := store.Alert{TopicID: t.ID, PrimaryItemID: fi.item.ID, Title: fi.item.Title, Kind: fi.src.Kind,
		MatchedKeywords: append(slices.Clone(m.Keywords), m.Watched...), State: "unread", NeedsNotify: true,
		LegalStatus: "unknown", Verified: "n/a"}

	var primary []string
	verdict, provider := e.classify(ctx, st, t, fi)
	if verdict != nil {
		a.AIProvider = provider
		a.LegalStatus, a.Relevance = verdict.LegalStatus, verdict.Relevance
		a.Summary, a.Affected, a.Reason = verdict.Summary, verdict.WhoIsAffected, verdict.Reason
		a.EffectiveAt = verdict.Effective()
		// A quote the model cannot have copied from the text is dropped.
		if quoteIn(verdict.EvidenceQuote, fi.item.Title+"\n"+fi.text) {
			a.Evidence = verdict.EvidenceQuote
		}
		// Likewise, only document numbers that really occur in the item count.
		present := map[string]bool{}
		for _, n := range fi.item.DocNumbers {
			present[textutil.NormalizeDocNumber(n)] = true
		}
		for _, n := range verdict.DocNumbers {
			if present[textutil.NormalizeDocNumber(n)] {
				primary = append(primary, n)
			}
		}
		if !verdict.Relevant {
			a.State, a.NeedsNotify = "filtered", false
			if a.Summary == "" {
				a.Summary = textutil.Truncate(fi.item.Summary, 400)
			}
		}
	} else {
		a.LegalStatus = GuessStatus(fi.item.Title+". "+fi.item.Summary, now)
		a.Summary = textutil.Truncate(fi.item.Summary, 400)
		if a.Summary == "" {
			a.Summary = textutil.Truncate(fi.text, 400)
		}
		a.Reason = i18n.T("Khớp từ khóa: {0}. Chưa được AI đánh giá; trạng thái pháp lý là phỏng đoán theo câu chữ.", strings.Join(a.MatchedKeywords, ", "))
	}

	if official {
		a.Verified = "confirmed"
		if len(m.Watched) > 0 {
			a.Kind = "doc_changed"
		}
		if fi.doc != nil {
			primary = []string{fi.doc.DocNumber}
			a.DocumentID, a.EffectiveAt = fi.doc.ID, fi.doc.EffectiveAt
			// For an official document the dates decide, not the wording.
			a.LegalStatus = "issued"
			if fi.doc.EffectiveAt != "" && fi.doc.EffectiveAt <= now.Format("2006-01-02") {
				a.LegalStatus = "effective"
			}
			if a.Summary == "" {
				a.Summary = fi.doc.Title
			}
		} else if fi.ref.DocNumber != "" {
			primary = []string{fi.ref.DocNumber}
			a.LegalStatus = "issued"
		}
	} else {
		if len(primary) == 0 {
			// Without an AI verdict only the headline and the lead count: a
			// number buried in the body is usually a law cited in passing,
			// and confirming it would say nothing about the news itself.
			primary = PrimaryDocs(fi.item.Title, fi.item.Summary, now)
		}
		for _, n := range primary {
			if IsRecent(n, now) {
				a.Verified = "unconfirmed"
			}
		}
	}
	if (a.LegalStatus == "draft" || a.LegalStatus == "proposal") && !slices.Contains(t.Kinds, "draft") && a.State == "unread" {
		a.State, a.NeedsNotify = "filtered", false
		a.Reason = strings.TrimSpace(a.Reason + " " + i18n.T("Chủ đề này không theo dõi dự thảo, đề xuất."))
	}
	a.DocNumbers = primary
	a.Severity = Severity(fi.src.Kind, m, a.LegalStatus, a.Relevance, verdict != nil)
	a.ClusterKey = ClusterKey(primary, fi.item.Title)

	// The same event reported again joins the existing alert. When the new
	// report is the official document itself, it confirms the earlier one.
	if existing, ok := e.St.ClusterAlert(t.ID, a.ClusterKey, now.Add(-clusterWindow)); ok {
		e.St.AttachItem(existing, fi.item.ID)
		if official && fi.doc != nil {
			e.St.ConfirmAlert(existing, fi.doc.ID, fi.doc.EffectiveAt, true)
			e.St.DropVerifyByNorm(textutil.NormalizeDocNumber(fi.doc.DocNumber))
		}
		return
	}
	id, err := e.St.AddAlert(a)
	if err != nil {
		return
	}
	if a.State == "filtered" {
		return
	}
	st.sc.AlertsNew++
	e.emit("alert.created", map[string]any{"id": id, "severity": a.Severity})
	if a.Verified == "unconfirmed" {
		st.verify = append(st.verify, pendingCheck{alertID: id, numbers: primary})
	}
}

// classify asks the AI whether the item is relevant to the topic. It returns
// nil when there is no usable verdict, in which case the caller falls back
// to keyword mode for this item.
func (e *Engine) classify(ctx context.Context, st *scanState, t store.Topic, fi *fresh) (*ai.Verdict, string) {
	if raw, prov, ok := e.St.CachedVerdict(fi.item.ContentHash, t.ID, t.Version); ok {
		if v, err := ai.ParseVerdict(raw); err == nil {
			return &v, prov
		}
	}
	if st.provider == nil || st.aiBudget <= 0 {
		return nil, ""
	}
	req := ai.ClassifyRequest(ai.ClassifyInput{
		TopicName: t.Name, TopicKeywords: t.Keywords, TopicFields: t.Fields, TopicContext: t.AIContext, WatchedDocs: t.WatchedDocs,
		SourceName: fi.src.Name, SourceKind: fi.src.Kind, Title: fi.item.Title, Published: fi.item.PublishedAt,
		Text: string([]rune(fi.text)[:min(aiTextLimit, len([]rune(fi.text)))]), Today: e.now().Format("2006-01-02"),
	})
	// A reply that breaks the schema is retried once, then given up on.
	for attempt := 0; attempt < 2 && st.aiBudget > 0; attempt++ {
		st.aiBudget--
		st.sc.AICalls++
		reply, err := st.provider.Complete(ctx, req)
		if err != nil {
			if errors.Is(err, ai.ErrAuth) {
				// Not signed in: no point asking again until the user acts.
				e.blockAI(30*time.Minute, err)
				st.provider = nil
			} else if st.aiFailures++; st.aiFailures >= 3 {
				e.blockAI(10*time.Minute, err)
				st.provider = nil
			}
			return nil, ""
		}
		st.aiFailures = 0
		v, perr := ai.ParseVerdict(reply)
		if perr != nil {
			continue
		}
		if raw, ok := ai.ExtractJSON(reply); ok {
			e.St.CacheVerdict(fi.item.ContentHash, t.ID, t.Version, st.provider.Name(), raw)
		}
		return &v, st.provider.Name()
	}
	return nil, ""
}

// quoteIn reports whether quote occurs in text, ignoring case and spacing.
func quoteIn(quote, text string) bool {
	q := textutil.Lower(quote)
	return len([]rune(q)) >= 12 && strings.Contains(textutil.Lower(text), q)
}

// officialSourceID returns the id of a Government portal source, used as the
// owner of documents found by search.
func (e *Engine) officialSourceID() int64 {
	list, _ := e.St.Sources()
	for _, s := range list {
		if s.Connector == "vanban" {
			return s.ID
		}
	}
	return 0
}

// SaveOfficial fetches a document from the Government portal by its portal
// id, stores its metadata, downloads its files, indexes its text when it has
// a text layer and links it to the stored documents its abstract names.
func (e *Engine) SaveOfficial(ctx context.Context, portalID string, sourceID int64) (store.Document, string, error) {
	info, err := sources.VanbanInfo(ctx, e.Fetch, portalID)
	if err != nil {
		return store.Document{}, "", err
	}
	if sourceID == 0 {
		sourceID = e.officialSourceID()
	}
	today := e.now().Format("2006-01-02")
	d := store.Document{DocNumber: info.DocNumber, Title: info.Title, DocType: info.DocType, Issuer: info.Issuer,
		Signer: info.Signer, SourceID: sourceID, SourceURL: info.URL, PortalID: portalID, FileURLs: info.FileURLs, Meta: info.Meta}
	if d.Title == "" {
		d.Title = info.Abstract
	}
	if !info.Issued.IsZero() {
		d.IssuedAt = info.Issued.Format("2006-01-02")
	}
	if !info.Effective.IsZero() {
		d.EffectiveAt = info.Effective.Format("2006-01-02")
		// The portal states dates, not status: "in force" is an inference
		// from the effective date and is labelled as one.
		d.Validity, d.ValidityBasis = "in_force", "inferred"
		if d.EffectiveAt > today {
			d.Validity = "not_yet"
		}
	}
	id, err := e.St.SaveDocument(d)
	if err != nil {
		return d, "", err
	}
	d.ID = id

	dirName := sources.SafeName(strings.ReplaceAll(info.DocNumber, "/", "_"))
	dir := filepath.Join(e.DataDir, "docs", dirName)
	saved, _ := sources.DownloadFiles(ctx, e.Fetch, info.FileURLs, dir)
	var files []string
	var text strings.Builder
	for _, name := range saved {
		files = append(files, filepath.ToSlash(filepath.Join("docs", dirName, name)))
		if strings.HasSuffix(strings.ToLower(name), ".pdf") {
			if t, ok := extract.PDFText(filepath.Join(dir, name)); ok {
				text.WriteString(t)
				text.WriteByte('\n')
			}
		}
	}
	hasText := text.Len() > 0
	e.St.SetDocumentFiles(id, files, hasText)
	d.Files, d.HasText = files, hasText

	head := fmt.Sprintf("%s. %s", info.DocNumber, d.Title)
	if d.EffectiveAt != "" {
		head += fmt.Sprintf(" Ban hành ngày %s, có hiệu lực từ ngày %s.", d.IssuedAt, d.EffectiveAt)
	}
	chunks := []store.Chunk{{Path: "Thông tin văn bản", Text: head}}
	if hasText {
		chunks = append(chunks, extract.LegalChunks(text.String())...)
	}
	e.St.ReplaceChunks("document", id, chunks)

	// The abstract of an amending document names what it amends.
	for _, n := range textutil.ExtractDocNumbers(info.Abstract + " " + info.Title) {
		if textutil.NormalizeDocNumber(n) == textutil.NormalizeDocNumber(info.DocNumber) {
			continue
		}
		target, err := e.St.DocumentByNumber(n)
		if err != nil {
			continue
		}
		rel := sources.GuessRelation(info.Abstract)
		e.St.AddRelation(id, target.ID, rel, "stated")
		switch rel {
		case "amends":
			e.St.SetDocumentValidity(target.ID, "amended", "inferred")
		case "replaces", "repeals":
			e.St.SetDocumentValidity(target.ID, "superseded", "inferred")
		}
	}
	return d, text.String(), nil
}

// FetchByNumber finds a document on the Government portal by its number and
// stores it. It returns store.ErrNotFound when the portal does not have it.
func (e *Engine) FetchByNumber(ctx context.Context, number string) (store.Document, error) {
	all, _ := e.St.Sources()
	e.Fetch.SetAllowed(sources.AllowedDomains(all))
	hit, err := sources.ResolveVanban(ctx, e.Fetch, number)
	if err != nil {
		return store.Document{}, err
	}
	if hit == nil {
		return store.Document{}, store.ErrNotFound
	}
	d, _, err := e.SaveOfficial(ctx, hit.PortalID, 0)
	return d, err
}

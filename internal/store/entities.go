package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// ---- sources ---------------------------------------------------------------

// Source is a site the app is allowed to scan.
type Source struct {
	ID              int64  `json:"id"`
	Key             string `json:"key"`
	Name            string `json:"name"`
	Domain          string `json:"domain"`
	Tier            int    `json:"tier"`      // 1 official, 2 legal database, 3 licensed press
	Kind            string `json:"kind"`      // press | official
	Connector       string `json:"connector"` // rss | htmllist | vanban
	Config          string `json:"config"`    // connector-specific JSON
	IntervalMinutes int    `json:"interval_minutes"`
	Builtin         bool   `json:"builtin"`
	Enabled         bool   `json:"enabled"`
	ETag            string `json:"-"`
	LastModified    string `json:"-"`
	LastScanAt      string `json:"last_scan_at"`
	LastOKAt        string `json:"last_ok_at"`
	LastStatus      string `json:"last_status"` // "", ok, unchanged, empty, error
	LastError       string `json:"last_error"`
	FailCount       int    `json:"fail_count"`
	EmptyCount      int    `json:"empty_count"`
}

const sourceCols = `id, key, name, domain, tier, kind, connector, config, interval_minutes, builtin, enabled,
 etag, last_modified, last_scan_at, last_ok_at, last_status, last_error, fail_count, empty_count`

func scanSource(r interface{ Scan(...any) error }) (Source, error) {
	var s Source
	err := r.Scan(&s.ID, &s.Key, &s.Name, &s.Domain, &s.Tier, &s.Kind, &s.Connector, &s.Config, &s.IntervalMinutes,
		&s.Builtin, &s.Enabled, &s.ETag, &s.LastModified, &s.LastScanAt, &s.LastOKAt, &s.LastStatus, &s.LastError,
		&s.FailCount, &s.EmptyCount)
	return s, err
}

// Sources lists all sources, official ones first.
func (s *Store) Sources() ([]Source, error) {
	rows, err := s.db.Query(`SELECT ` + sourceCols + ` FROM sources ORDER BY tier, builtin DESC, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Source{}
	for rows.Next() {
		src, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

// Source returns one source.
func (s *Store) Source(id int64) (Source, error) {
	src, err := scanSource(s.db.QueryRow(`SELECT `+sourceCols+` FROM sources WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return src, ErrNotFound
	}
	return src, err
}

// UpsertBuiltin creates or refreshes a built-in source by key. The user's
// enabled flag and interval survive an upgrade; the definition does not.
func (s *Store) UpsertBuiltin(src Source) error {
	_, err := s.db.Exec(`INSERT INTO sources(key, name, domain, tier, kind, connector, config, interval_minutes, builtin, enabled)
 VALUES(?, ?, ?, ?, ?, ?, ?, ?, 1, ?)
 ON CONFLICT(key) WHERE key <> '' DO UPDATE SET name = excluded.name, domain = excluded.domain, tier = excluded.tier,
   kind = excluded.kind, connector = excluded.connector, config = excluded.config, builtin = 1`,
		src.Key, src.Name, src.Domain, src.Tier, src.Kind, src.Connector, src.Config, src.IntervalMinutes, b2i(src.Enabled))
	return err
}

// RemoveBuiltinsExcept deletes built-in sources whose key is no longer shipped.
func (s *Store) RemoveBuiltinsExcept(keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	args := make([]any, len(keys))
	for i, k := range keys {
		args[i] = k
	}
	_, err := s.db.Exec(`DELETE FROM sources WHERE builtin = 1 AND key NOT IN (?`+strings.Repeat(",?", len(keys)-1)+`)`, args...)
	return err
}

// AddSource inserts a user-defined source.
func (s *Store) AddSource(src Source) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO sources(name, domain, tier, kind, connector, config, interval_minutes, builtin, enabled)
 VALUES(?, ?, ?, ?, ?, ?, ?, 0, 1)`, src.Name, src.Domain, src.Tier, src.Kind, src.Connector, src.Config, src.IntervalMinutes)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateSourceUser changes the two fields the user controls.
func (s *Store) UpdateSourceUser(id int64, enabled bool, intervalMinutes int) error {
	_, err := s.db.Exec(`UPDATE sources SET enabled = ?, interval_minutes = ? WHERE id = ?`, b2i(enabled), intervalMinutes, id)
	return err
}

// DeleteSource removes a user-defined source; built-in ones can only be disabled.
func (s *Store) DeleteSource(id int64) error {
	res, err := s.db.Exec(`DELETE FROM sources WHERE id = ? AND builtin = 0`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ScanOutcome is what one scan of one source produced.
type ScanOutcome struct {
	Status       string // ok | unchanged | empty | error | offline
	Error        string
	ETag         string
	LastModified string
}

// RecordSourceScan stores the outcome of scanning a source. A source that
// used to return items and now keeps returning none is tracked separately
// from one that fails outright: both must be visible to the user.
func (s *Store) RecordSourceScan(id int64, o ScanOutcome) error {
	now := Now()
	switch o.Status {
	case "ok", "unchanged":
		_, err := s.db.Exec(`UPDATE sources SET last_scan_at = ?, last_ok_at = ?, last_status = ?, last_error = '',
 fail_count = 0, empty_count = 0, etag = ?, last_modified = ? WHERE id = ?`, now, now, o.Status, o.ETag, o.LastModified, id)
		return err
	case "empty":
		_, err := s.db.Exec(`UPDATE sources SET last_scan_at = ?, last_status = 'empty', last_error = '',
 fail_count = 0, empty_count = empty_count + 1 WHERE id = ?`, now, id)
		return err
	case "offline":
		return nil
	default:
		_, err := s.db.Exec(`UPDATE sources SET last_scan_at = ?, last_status = 'error', last_error = ?,
 fail_count = fail_count + 1 WHERE id = ?`, now, o.Error, id)
		return err
	}
}

// ---- topics ----------------------------------------------------------------

// Topic is a set of conditions the user wants to be alerted about.
type Topic struct {
	ID              int64    `json:"id"`
	Name            string   `json:"name"`
	Keywords        []string `json:"keywords"`
	ExcludeKeywords []string `json:"exclude_keywords"`
	Fields          []string `json:"fields"`
	WatchedDocs     []string `json:"watched_docs"`
	AIContext       string   `json:"ai_context"`
	SourceIDs       []int64  `json:"source_ids"` // empty = all sources
	Kinds           []string `json:"kinds"`      // press, official, draft
	RemindDays      int      `json:"remind_days"`
	Version         int      `json:"version"`
	Enabled         bool     `json:"enabled"`
	CreatedAt       string   `json:"created_at"`
}

const topicCols = `id, name, keywords, exclude_keywords, fields, watched_docs, ai_context, source_ids, kinds, remind_days, version, enabled, created_at`

func scanTopic(r interface{ Scan(...any) error }) (Topic, error) {
	var t Topic
	var kw, ex, fl, wd, si, kd string
	err := r.Scan(&t.ID, &t.Name, &kw, &ex, &fl, &wd, &t.AIContext, &si, &kd, &t.RemindDays, &t.Version, &t.Enabled, &t.CreatedAt)
	t.Keywords, t.ExcludeKeywords, t.Fields, t.WatchedDocs = strList(kw), strList(ex), strList(fl), strList(wd)
	t.SourceIDs, t.Kinds = intList(si), strList(kd)
	return t, err
}

// Topics lists topics; onlyEnabled restricts to the active ones.
func (s *Store) Topics(onlyEnabled bool) ([]Topic, error) {
	q := `SELECT ` + topicCols + ` FROM topics`
	if onlyEnabled {
		q += ` WHERE enabled = 1`
	}
	rows, err := s.db.Query(q + ` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Topic{}
	for rows.Next() {
		t, err := scanTopic(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Topic returns one topic.
func (s *Store) Topic(id int64) (Topic, error) {
	t, err := scanTopic(s.db.QueryRow(`SELECT `+topicCols+` FROM topics WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

// SaveTopic inserts a topic (ID 0) or updates it. Every update bumps the
// version, which invalidates cached AI verdicts made under the old definition.
func (s *Store) SaveTopic(t Topic) (int64, error) {
	if t.Kinds == nil {
		t.Kinds = []string{"press", "official", "draft"}
	}
	args := []any{t.Name, toJSON(nz(t.Keywords)), toJSON(nz(t.ExcludeKeywords)), toJSON(nz(t.Fields)), toJSON(nz(t.WatchedDocs)),
		t.AIContext, toJSON(nzInt(t.SourceIDs)), toJSON(t.Kinds), t.RemindDays, b2i(t.Enabled)}
	if t.ID == 0 {
		res, err := s.db.Exec(`INSERT INTO topics(name, keywords, exclude_keywords, fields, watched_docs, ai_context, source_ids,
 kinds, remind_days, enabled, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, append(args, Now())...)
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	res, err := s.db.Exec(`UPDATE topics SET name = ?, keywords = ?, exclude_keywords = ?, fields = ?, watched_docs = ?,
 ai_context = ?, source_ids = ?, kinds = ?, remind_days = ?, enabled = ?, version = version + 1 WHERE id = ?`, append(args, t.ID)...)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, ErrNotFound
	}
	return t.ID, nil
}

// DeleteTopic removes a topic and, by cascade, its alerts.
func (s *Store) DeleteTopic(id int64) error {
	res, err := s.db.Exec(`DELETE FROM topics WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func nz(l []string) []string {
	if l == nil {
		return []string{}
	}
	return l
}

func nzInt(l []int64) []int64 {
	if l == nil {
		return []int64{}
	}
	return l
}

// ---- items -----------------------------------------------------------------

// Item is one article or one document listing fetched from a source.
type Item struct {
	ID          int64    `json:"id"`
	SourceID    int64    `json:"source_id"`
	URL         string   `json:"url"`
	Title       string   `json:"title"`
	Summary     string   `json:"summary"`
	PublishedAt string   `json:"published_at"`
	FetchedAt   string   `json:"fetched_at"`
	Content     string   `json:"-"`
	ContentHash string   `json:"-"`
	DocNumbers  []string `json:"doc_numbers"`
	Matched     bool     `json:"-"`
}

// ItemSeen reports whether an item with this normalized URL is already stored.
func (s *Store) ItemSeen(url string) bool {
	var id int64
	return s.db.QueryRow(`SELECT id FROM items WHERE url = ?`, url).Scan(&id) == nil
}

// AddItem stores a new item and returns its id. A duplicate URL returns 0.
func (s *Store) AddItem(it Item) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO items(source_id, url, title, summary, published_at, fetched_at, content, content_hash, doc_numbers)
 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(url) DO NOTHING`,
		it.SourceID, it.URL, it.Title, it.Summary, it.PublishedAt, Now(), it.Content, it.ContentHash, toJSON(nz(it.DocNumbers)))
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, nil
	}
	return res.LastInsertId()
}

// SetItemContent stores the full text fetched for an item that matched a topic.
func (s *Store) SetItemContent(id int64, content, hash string, docNumbers []string) error {
	_, err := s.db.Exec(`UPDATE items SET content = ?, content_hash = ?, doc_numbers = ?, matched = 1 WHERE id = ?`,
		content, hash, toJSON(nz(docNumbers)), id)
	return err
}

// Item returns one item.
func (s *Store) Item(id int64) (Item, error) {
	var it Item
	var dn string
	err := s.db.QueryRow(`SELECT id, source_id, url, title, summary, published_at, fetched_at, content, content_hash, doc_numbers, matched
 FROM items WHERE id = ?`, id).Scan(&it.ID, &it.SourceID, &it.URL, &it.Title, &it.Summary, &it.PublishedAt, &it.FetchedAt,
		&it.Content, &it.ContentHash, &dn, &it.Matched)
	it.DocNumbers = strList(dn)
	if errors.Is(err, sql.ErrNoRows) {
		return it, ErrNotFound
	}
	return it, err
}

// RecentItems returns items fetched since the given time, for topic previews.
func (s *Store) RecentItems(since time.Time, limit int) ([]Item, error) {
	rows, err := s.db.Query(`SELECT id, source_id, url, title, summary, published_at, fetched_at, doc_numbers
 FROM items WHERE fetched_at >= ? ORDER BY id DESC LIMIT ?`, FormatTime(since), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		var it Item
		var dn string
		if err := rows.Scan(&it.ID, &it.SourceID, &it.URL, &it.Title, &it.Summary, &it.PublishedAt, &it.FetchedAt, &dn); err != nil {
			return nil, err
		}
		it.DocNumbers = strList(dn)
		out = append(out, it)
	}
	return out, rows.Err()
}

// PurgeItems deletes items older than the cutoff that never produced an
// alert, together with their search chunks, and returns how many were removed.
func (s *Store) PurgeItems(before time.Time) (int64, error) {
	cut := FormatTime(before)
	const stale = `SELECT id FROM items WHERE fetched_at < ? AND id NOT IN (SELECT item_id FROM alert_items)
 AND id NOT IN (SELECT primary_item_id FROM alerts)`
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM chunks_fts WHERE rowid IN (SELECT id FROM chunks WHERE owner_kind = 'item' AND owner_id IN (`+stale+`))`, cut); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`DELETE FROM chunks WHERE owner_kind = 'item' AND owner_id IN (`+stale+`)`, cut); err != nil {
		return 0, err
	}
	res, err := tx.Exec(`DELETE FROM items WHERE id IN (`+stale+`)`, cut)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, tx.Commit()
}

// ---- AI verdict cache ------------------------------------------------------

// CachedVerdict returns the stored AI result for this content under this
// version of the topic.
func (s *Store) CachedVerdict(hash string, topicID int64, version int) (result, provider string, ok bool) {
	err := s.db.QueryRow(`SELECT result, provider FROM ai_results WHERE content_hash = ? AND topic_id = ? AND topic_version = ?`,
		hash, topicID, version).Scan(&result, &provider)
	return result, provider, err == nil
}

// CacheVerdict stores an AI result.
func (s *Store) CacheVerdict(hash string, topicID int64, version int, provider, result string) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO ai_results(content_hash, topic_id, topic_version, provider, result, created_at)
 VALUES(?, ?, ?, ?, ?, ?)`, hash, topicID, version, provider, result, Now())
	return err
}

// ---- scans -----------------------------------------------------------------

// SourceError is one source's failure inside a scan.
type SourceError struct {
	Source string `json:"source"`
	Error  string `json:"error"`
}

// Scan is one run of the pipeline.
type Scan struct {
	ID            int64         `json:"id"`
	StartedAt     string        `json:"started_at"`
	FinishedAt    string        `json:"finished_at"`
	Trigger       string        `json:"trigger"` // schedule | manual | catchup
	SourcesOK     int           `json:"sources_ok"`
	SourcesFailed int           `json:"sources_failed"`
	ItemsNew      int           `json:"items_new"`
	AlertsNew     int           `json:"alerts_new"`
	AICalls       int           `json:"ai_calls"`
	Errors        []SourceError `json:"errors"`
	Note          string        `json:"note"`
}

// StartScan opens a scan record.
func (s *Store) StartScan(trigger string) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO scans(started_at, trigger) VALUES(?, ?)`, Now(), trigger)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FinishScan closes a scan record with its totals.
func (s *Store) FinishScan(sc Scan) error {
	if sc.Errors == nil {
		sc.Errors = []SourceError{}
	}
	_, err := s.db.Exec(`UPDATE scans SET finished_at = ?, sources_ok = ?, sources_failed = ?, items_new = ?, alerts_new = ?,
 ai_calls = ?, errors = ?, note = ? WHERE id = ?`, Now(), sc.SourcesOK, sc.SourcesFailed, sc.ItemsNew, sc.AlertsNew, sc.AICalls,
		toJSON(sc.Errors), sc.Note, sc.ID)
	return err
}

// Scans returns the most recent scans, newest first.
func (s *Store) Scans(limit int) ([]Scan, error) {
	rows, err := s.db.Query(`SELECT id, started_at, finished_at, trigger, sources_ok, sources_failed, items_new, alerts_new,
 ai_calls, errors, note FROM scans ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Scan{}
	for rows.Next() {
		var sc Scan
		var errs string
		if err := rows.Scan(&sc.ID, &sc.StartedAt, &sc.FinishedAt, &sc.Trigger, &sc.SourcesOK, &sc.SourcesFailed, &sc.ItemsNew,
			&sc.AlertsNew, &sc.AICalls, &errs, &sc.Note); err != nil {
			return nil, err
		}
		sc.Errors = []SourceError{}
		jsonUnmarshal(errs, &sc.Errors)
		out = append(out, sc)
	}
	return out, rows.Err()
}

// PurgeScans keeps only the newest n scan records.
func (s *Store) PurgeScans(keep int) error {
	_, err := s.db.Exec(`DELETE FROM scans WHERE id NOT IN (SELECT id FROM scans ORDER BY id DESC LIMIT ?)`, keep)
	return err
}

// ItemID returns the id of the item stored under a normalized URL, or 0.
func (s *Store) ItemID(url string) int64 {
	var id int64
	s.db.QueryRow(`SELECT id FROM items WHERE url = ?`, url).Scan(&id)
	return id
}

// SetIntervalByKind applies one scan interval to every source of a kind.
func (s *Store) SetIntervalByKind(kind string, minutes int) error {
	_, err := s.db.Exec(`UPDATE sources SET interval_minutes = ? WHERE kind = ?`, minutes, kind)
	return err
}

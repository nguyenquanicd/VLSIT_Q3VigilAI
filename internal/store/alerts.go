package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

func jsonUnmarshal(raw string, v any) { json.Unmarshal([]byte(raw), v) }

// Alert is a notification produced when something matched a topic.
type Alert struct {
	ID              int64    `json:"id"`
	TopicID         int64    `json:"topic_id"`
	TopicName       string   `json:"topic_name"`
	Kind            string   `json:"kind"`         // press | official | effective_soon | doc_changed
	Severity        string   `json:"severity"`     // info | notice | warning
	LegalStatus     string   `json:"legal_status"` // proposal | draft | issued | effective | other | unknown
	Verified        string   `json:"verified"`     // confirmed | unconfirmed | n/a
	Title           string   `json:"title"`
	Summary         string   `json:"summary"`
	Affected        string   `json:"affected"`
	EffectiveAt     string   `json:"effective_at"`
	Relevance       string   `json:"relevance"`
	Reason          string   `json:"reason"`
	Evidence        string   `json:"evidence"`
	MatchedKeywords []string `json:"matched_keywords"`
	DocNumbers      []string `json:"doc_numbers"`
	ClusterKey      string   `json:"-"`
	DocumentID      int64    `json:"document_id"`
	PrimaryItemID   int64    `json:"primary_item_id"`
	AIProvider      string   `json:"ai_provider"`
	State           string   `json:"state"` // unread | read | pinned | dismissed | filtered
	Feedback        string   `json:"feedback"`
	NeedsNotify     bool     `json:"-"`
	NotifiedAt      string   `json:"notified_at"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`

	// Filled by list and detail queries.
	SourceName  string        `json:"source_name"`
	SourceTier  int           `json:"source_tier"`
	URL         string        `json:"url"`
	SourceCount int           `json:"source_count"`
	Items       []AlertSource `json:"items,omitempty"`
	Document    *Document     `json:"document,omitempty"`
}

// AlertSource is one article or listing behind an alert.
type AlertSource struct {
	ItemID      int64  `json:"item_id"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	SourceName  string `json:"source_name"`
	SourceTier  int    `json:"source_tier"`
	PublishedAt string `json:"published_at"`
}

// AddAlert stores a new alert and links its primary item.
func (s *Store) AddAlert(a Alert) (int64, error) {
	now := Now()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO alerts(topic_id, kind, severity, legal_status, verified, title, summary, affected, effective_at,
 relevance, reason, evidence, matched_keywords, doc_numbers, cluster_key, document_id, primary_item_id, ai_provider, state,
 needs_notify, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.TopicID, a.Kind, a.Severity, a.LegalStatus, a.Verified, a.Title, a.Summary, a.Affected, a.EffectiveAt, a.Relevance,
		a.Reason, a.Evidence, toJSON(nz(a.MatchedKeywords)), toJSON(nz(a.DocNumbers)), a.ClusterKey, a.DocumentID,
		a.PrimaryItemID, a.AIProvider, a.State, b2i(a.NeedsNotify), now, now)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if a.PrimaryItemID != 0 {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO alert_items(alert_id, item_id) VALUES(?, ?)`, id, a.PrimaryItemID); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

// ClusterAlert finds an existing alert of the same topic about the same
// event (same cluster key) created since the given time.
func (s *Store) ClusterAlert(topicID int64, clusterKey string, since time.Time) (int64, bool) {
	if clusterKey == "" {
		return 0, false
	}
	var id int64
	err := s.db.QueryRow(`SELECT id FROM alerts WHERE topic_id = ? AND cluster_key = ? AND created_at >= ? ORDER BY id LIMIT 1`,
		topicID, clusterKey, FormatTime(since)).Scan(&id)
	return id, err == nil
}

// AttachItem records another source reporting the same event.
func (s *Store) AttachItem(alertID, itemID int64) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO alert_items(alert_id, item_id) VALUES(?, ?)`, alertID, itemID)
	return err
}

// AlertFilter narrows an alert listing.
type AlertFilter struct {
	Severity string
	Kind     string
	TopicID  int64
	State    string // "" = inbox (everything but dismissed and filtered)
	Query    string
	Limit    int
	Offset   int
}

const alertCols = `a.id, a.topic_id, COALESCE(t.name, ''), a.kind, a.severity, a.legal_status, a.verified, a.title, a.summary, a.affected,
 a.effective_at, a.relevance, a.reason, a.evidence, a.matched_keywords, a.doc_numbers, a.cluster_key, a.document_id,
 a.primary_item_id, a.ai_provider, a.state, a.feedback, a.needs_notify, a.notified_at, a.created_at, a.updated_at,
 COALESCE(src.name, CASE WHEN a.document_id <> 0 THEN 'Cổng Thông tin điện tử Chính phủ' ELSE '' END),
 COALESCE(src.tier, CASE WHEN a.document_id <> 0 THEN 1 ELSE 0 END), COALESCE(i.url, doc.source_url, ''),
 (SELECT COUNT(*) FROM alert_items ai WHERE ai.alert_id = a.id)`

const alertFrom = ` FROM alerts a LEFT JOIN topics t ON t.id = a.topic_id
 LEFT JOIN items i ON i.id = a.primary_item_id LEFT JOIN sources src ON src.id = i.source_id
 LEFT JOIN documents doc ON doc.id = a.document_id`

func scanAlert(r interface{ Scan(...any) error }) (Alert, error) {
	var a Alert
	var mk, dn string
	err := r.Scan(&a.ID, &a.TopicID, &a.TopicName, &a.Kind, &a.Severity, &a.LegalStatus, &a.Verified, &a.Title, &a.Summary,
		&a.Affected, &a.EffectiveAt, &a.Relevance, &a.Reason, &a.Evidence, &mk, &dn, &a.ClusterKey, &a.DocumentID,
		&a.PrimaryItemID, &a.AIProvider, &a.State, &a.Feedback, &a.NeedsNotify, &a.NotifiedAt, &a.CreatedAt, &a.UpdatedAt,
		&a.SourceName, &a.SourceTier, &a.URL, &a.SourceCount)
	a.MatchedKeywords, a.DocNumbers = strList(mk), strList(dn)
	return a, err
}

// Alerts lists alerts, newest first, with the total number matching the filter.
func (s *Store) Alerts(f AlertFilter) ([]Alert, int, error) {
	where := []string{"1 = 1"}
	var args []any
	switch f.State {
	case "":
		where = append(where, `a.state NOT IN ('dismissed', 'filtered')`)
	case "all":
	default:
		where = append(where, `a.state = ?`)
		args = append(args, f.State)
	}
	if f.Severity != "" {
		where = append(where, `a.severity = ?`)
		args = append(args, f.Severity)
	}
	if f.Kind != "" {
		where = append(where, `a.kind = ?`)
		args = append(args, f.Kind)
	}
	if f.TopicID != 0 {
		where = append(where, `a.topic_id = ?`)
		args = append(args, f.TopicID)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, `(a.title LIKE ? OR a.summary LIKE ? OR a.doc_numbers LIKE ?)`)
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	cond := " WHERE " + strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM alerts a`+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	rows, err := s.db.Query(`SELECT `+alertCols+alertFrom+cond+` ORDER BY (a.state = 'pinned') DESC, a.id DESC LIMIT ? OFFSET ?`,
		append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Alert{}
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, a)
	}
	return out, total, rows.Err()
}

// Alert returns one alert with every source that reported it and its document.
func (s *Store) Alert(id int64) (Alert, error) {
	a, err := scanAlert(s.db.QueryRow(`SELECT `+alertCols+alertFrom+` WHERE a.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	rows, err := s.db.Query(`SELECT i.id, i.title, i.url, src.name, src.tier, i.published_at FROM alert_items ai
 JOIN items i ON i.id = ai.item_id JOIN sources src ON src.id = i.source_id WHERE ai.alert_id = ? ORDER BY src.tier, i.id`, id)
	if err != nil {
		return a, err
	}
	a.Items = []AlertSource{}
	for rows.Next() {
		var as AlertSource
		if err := rows.Scan(&as.ItemID, &as.Title, &as.URL, &as.SourceName, &as.SourceTier, &as.PublishedAt); err != nil {
			rows.Close()
			return a, err
		}
		a.Items = append(a.Items, as)
	}
	rows.Close()
	if a.DocumentID != 0 {
		if d, err := s.Document(a.DocumentID); err == nil {
			a.Document = &d
		}
	}
	return a, nil
}

var alertStates = map[string]bool{"unread": true, "read": true, "pinned": true, "dismissed": true, "filtered": true}

// SetAlertState changes the reading state of an alert.
func (s *Store) SetAlertState(id int64, state string) error {
	if !alertStates[state] {
		return errors.New("invalid alert state")
	}
	res, err := s.db.Exec(`UPDATE alerts SET state = ?, updated_at = ? WHERE id = ?`, state, Now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetAlertFeedback records the user's verdict on an alert ("irrelevant" or "").
func (s *Store) SetAlertFeedback(id int64, feedback string) error {
	_, err := s.db.Exec(`UPDATE alerts SET feedback = ?, updated_at = ? WHERE id = ?`, feedback, Now(), id)
	return err
}

// MarkAllRead marks every unread alert as read.
func (s *Store) MarkAllRead() error {
	_, err := s.db.Exec(`UPDATE alerts SET state = 'read', updated_at = ? WHERE state = 'unread'`, Now())
	return err
}

// UnreadCount returns the number of unread alerts.
func (s *Store) UnreadCount() int {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM alerts WHERE state = 'unread'`).Scan(&n)
	return n
}

// PendingNotifications returns alerts waiting for a toast, oldest first.
func (s *Store) PendingNotifications() ([]Alert, error) {
	rows, err := s.db.Query(`SELECT ` + alertCols + alertFrom + ` WHERE a.needs_notify = 1 AND a.state = 'unread' ORDER BY a.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Alert{}
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// MarkNotified clears the pending-toast flag of the given alerts.
func (s *Store) MarkNotified(ids []int64) error {
	now := Now()
	for _, id := range ids {
		if _, err := s.db.Exec(`UPDATE alerts SET needs_notify = 0, notified_at = ? WHERE id = ?`, now, id); err != nil {
			return err
		}
	}
	return nil
}

// ConfirmAlert marks a press alert as confirmed by an official document and
// optionally queues a follow-up toast.
func (s *Store) ConfirmAlert(alertID, documentID int64, effectiveAt string, notify bool) error {
	_, err := s.db.Exec(`UPDATE alerts SET verified = 'confirmed', document_id = ?,
 effective_at = CASE WHEN ? <> '' THEN ? ELSE effective_at END,
 needs_notify = CASE WHEN ? = 1 AND state = 'unread' THEN 1 ELSE needs_notify END, updated_at = ? WHERE id = ?`,
		documentID, effectiveAt, effectiveAt, b2i(notify), Now(), alertID)
	return err
}

// EffectiveSoonExists reports whether a reminder for this document already
// exists for the topic.
func (s *Store) EffectiveSoonExists(topicID, documentID int64) bool {
	var id int64
	return s.db.QueryRow(`SELECT id FROM alerts WHERE topic_id = ? AND document_id = ? AND kind = 'effective_soon'`,
		topicID, documentID).Scan(&id) == nil
}

// DocumentWatch pairs a topic with a document one of its alerts refers to.
type DocumentWatch struct {
	TopicID    int64
	DocumentID int64
}

// AlertedDocuments returns every (topic, document) pair linked by an alert
// the user has not dismissed.
func (s *Store) AlertedDocuments() ([]DocumentWatch, error) {
	rows, err := s.db.Query(`SELECT DISTINCT topic_id, document_id FROM alerts
 WHERE document_id <> 0 AND state NOT IN ('dismissed', 'filtered')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DocumentWatch
	for rows.Next() {
		var w DocumentWatch
		if err := rows.Scan(&w.TopicID, &w.DocumentID); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ---- pending verification --------------------------------------------------

// PendingVerify is a document number the press mentioned that has not been
// found on an official source yet.
type PendingVerify struct {
	ID        int64
	AlertID   int64
	DocNumber string
	Norm      string
	Tries     int
	CreatedAt string
}

// QueueVerify schedules a document number for re-checking.
func (s *Store) QueueVerify(alertID int64, docNumber, norm string, next time.Time) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO pending_verify(alert_id, doc_number, norm, next_check_at, created_at)
 VALUES(?, ?, ?, ?, ?)`, alertID, docNumber, norm, FormatTime(next), Now())
	return err
}

// DueVerifications lists the verifications whose next check time has passed.
func (s *Store) DueVerifications(limit int) ([]PendingVerify, error) {
	rows, err := s.db.Query(`SELECT id, alert_id, doc_number, norm, tries, created_at FROM pending_verify
 WHERE next_check_at <= ? ORDER BY next_check_at LIMIT ?`, Now(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingVerify
	for rows.Next() {
		var p PendingVerify
		if err := rows.Scan(&p.ID, &p.AlertID, &p.DocNumber, &p.Norm, &p.Tries, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// RescheduleVerify pushes a verification to a later time.
func (s *Store) RescheduleVerify(id int64, next time.Time) error {
	_, err := s.db.Exec(`UPDATE pending_verify SET tries = tries + 1, next_check_at = ? WHERE id = ?`, FormatTime(next), id)
	return err
}

// DropVerify removes a verification that succeeded or expired.
func (s *Store) DropVerify(id int64) error {
	_, err := s.db.Exec(`DELETE FROM pending_verify WHERE id = ?`, id)
	return err
}

// DropVerifyByNorm removes every pending verification of one document number.
func (s *Store) DropVerifyByNorm(norm string) error {
	_, err := s.db.Exec(`DELETE FROM pending_verify WHERE norm = ?`, norm)
	return err
}

// ---- watched documents -----------------------------------------------------

// WatchedHitSeen reports whether a portal hit for a watched document was
// already recorded, and records it if not.
func (s *Store) WatchedHitSeen(topicID int64, docNorm, hitKey string) (bool, error) {
	res, err := s.db.Exec(`INSERT OR IGNORE INTO watched_hits(topic_id, doc_norm, hit_key, seen_at) VALUES(?, ?, ?, ?)`,
		topicID, docNorm, hitKey, Now())
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 0, nil
}

// WatchedBaselined reports whether a watched document has been checked before.
func (s *Store) WatchedBaselined(topicID int64, docNorm string) bool {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM watched_hits WHERE topic_id = ? AND doc_norm = ?`, topicID, docNorm).Scan(&n)
	return n > 0
}

// ItemAlerted reports whether an item already belongs to an alert of a topic.
func (s *Store) ItemAlerted(topicID, itemID int64) bool {
	var id int64
	return s.db.QueryRow(`SELECT a.id FROM alerts a JOIN alert_items ai ON ai.alert_id = a.id
 WHERE a.topic_id = ? AND ai.item_id = ? LIMIT 1`, topicID, itemID).Scan(&id) == nil
}

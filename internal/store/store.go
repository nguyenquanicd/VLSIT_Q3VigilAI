// Package store is the SQLite persistence layer.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"time"

	_ "modernc.org/sqlite"
)

// Store wraps the application database.
type Store struct {
	db   *sql.DB
	path string
}

// Now returns the current time in the format every timestamp column uses.
// RFC 3339 in UTC sorts lexically, so SQL can compare timestamps as text.
func Now() string { return FormatTime(Clock()) }

// Clock is the time source of every stored timestamp. Tests replace it so
// stored times and the engine's clock agree.
var Clock = time.Now

// FormatTime formats t for storage.
func FormatTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// ParseTime parses a stored timestamp; the zero time means "never".
func ParseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// Open opens (creating if needed) the database at path and applies migrations.
func Open(path string) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(abs)}
	// The app sits idle for hours and nothing here needs to be fast, so every
	// memory knob is turned toward small: a 512 KB page cache per connection
	// (the default is 2 MB), no memory-mapped file, and sort/temp data kept on
	// disk. A search that takes a few milliseconds longer costs nothing.
	dsn := u.String() + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)" +
		"&_pragma=cache_size(-512)&_pragma=mmap_size(0)&_pragma=temp_store(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection: SQLite's memory (cache, prepared statements) lives
	// outside the Go heap and is paid for once per connection. Requests wait
	// for each other, which is fine for one user. Code must therefore never
	// run a query while another query's rows are still open.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{db: db, path: abs}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate %s: %w", abs, err)
	}
	return s, nil
}

// Close flushes and closes the database.
func (s *Store) Close() error {
	s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	return s.db.Close()
}

// Path returns the database file path.
func (s *Store) Path() string { return s.path }

// Backup writes a consistent copy of the database to dest.
func (s *Store) Backup(dest string) error {
	_, err := s.db.Exec(`VACUUM INTO ?`, dest)
	return err
}

var migrations = []string{
	// 1: initial schema
	`
CREATE TABLE sources (
  id INTEGER PRIMARY KEY,
  key TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL,
  domain TEXT NOT NULL,
  tier INTEGER NOT NULL,
  kind TEXT NOT NULL,
  connector TEXT NOT NULL,
  config TEXT NOT NULL DEFAULT '{}',
  interval_minutes INTEGER NOT NULL,
  builtin INTEGER NOT NULL DEFAULT 0,
  enabled INTEGER NOT NULL DEFAULT 1,
  etag TEXT NOT NULL DEFAULT '',
  last_modified TEXT NOT NULL DEFAULT '',
  last_scan_at TEXT NOT NULL DEFAULT '',
  last_ok_at TEXT NOT NULL DEFAULT '',
  last_status TEXT NOT NULL DEFAULT '',
  last_error TEXT NOT NULL DEFAULT '',
  fail_count INTEGER NOT NULL DEFAULT 0,
  empty_count INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX sources_key ON sources(key) WHERE key <> '';

CREATE TABLE topics (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  keywords TEXT NOT NULL DEFAULT '[]',
  exclude_keywords TEXT NOT NULL DEFAULT '[]',
  fields TEXT NOT NULL DEFAULT '[]',
  watched_docs TEXT NOT NULL DEFAULT '[]',
  ai_context TEXT NOT NULL DEFAULT '',
  source_ids TEXT NOT NULL DEFAULT '[]',
  kinds TEXT NOT NULL DEFAULT '["press","official","draft"]',
  remind_days INTEGER NOT NULL DEFAULT 7,
  version INTEGER NOT NULL DEFAULT 1,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL
);

CREATE TABLE items (
  id INTEGER PRIMARY KEY,
  source_id INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
  url TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL,
  summary TEXT NOT NULL DEFAULT '',
  published_at TEXT NOT NULL DEFAULT '',
  fetched_at TEXT NOT NULL,
  content TEXT NOT NULL DEFAULT '',
  content_hash TEXT NOT NULL DEFAULT '',
  doc_numbers TEXT NOT NULL DEFAULT '[]',
  matched INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX items_fetched ON items(fetched_at);

CREATE TABLE documents (
  id INTEGER PRIMARY KEY,
  doc_number TEXT NOT NULL,
  norm TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL DEFAULT '',
  doc_type TEXT NOT NULL DEFAULT '',
  issuer TEXT NOT NULL DEFAULT '',
  signer TEXT NOT NULL DEFAULT '',
  issued_at TEXT NOT NULL DEFAULT '',
  effective_at TEXT NOT NULL DEFAULT '',
  validity TEXT NOT NULL DEFAULT 'unknown',
  validity_basis TEXT NOT NULL DEFAULT 'none',
  source_id INTEGER NOT NULL DEFAULT 0,
  source_url TEXT NOT NULL DEFAULT '',
  portal_id TEXT NOT NULL DEFAULT '',
  file_urls TEXT NOT NULL DEFAULT '[]',
  files TEXT NOT NULL DEFAULT '[]',
  meta TEXT NOT NULL DEFAULT '{}',
  has_text INTEGER NOT NULL DEFAULT 0,
  fetched_at TEXT NOT NULL
);

CREATE TABLE doc_relations (
  from_doc INTEGER NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  to_doc INTEGER NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  relation TEXT NOT NULL,
  basis TEXT NOT NULL,
  PRIMARY KEY (from_doc, to_doc, relation)
);

CREATE TABLE alerts (
  id INTEGER PRIMARY KEY,
  topic_id INTEGER NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  severity TEXT NOT NULL,
  legal_status TEXT NOT NULL DEFAULT 'unknown',
  verified TEXT NOT NULL DEFAULT 'n/a',
  title TEXT NOT NULL,
  summary TEXT NOT NULL DEFAULT '',
  affected TEXT NOT NULL DEFAULT '',
  effective_at TEXT NOT NULL DEFAULT '',
  relevance TEXT NOT NULL DEFAULT '',
  reason TEXT NOT NULL DEFAULT '',
  evidence TEXT NOT NULL DEFAULT '',
  matched_keywords TEXT NOT NULL DEFAULT '[]',
  doc_numbers TEXT NOT NULL DEFAULT '[]',
  cluster_key TEXT NOT NULL DEFAULT '',
  document_id INTEGER NOT NULL DEFAULT 0,
  primary_item_id INTEGER NOT NULL DEFAULT 0,
  ai_provider TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT 'unread',
  feedback TEXT NOT NULL DEFAULT '',
  needs_notify INTEGER NOT NULL DEFAULT 0,
  notified_at TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX alerts_created ON alerts(created_at);
CREATE INDEX alerts_cluster ON alerts(topic_id, cluster_key);

CREATE TABLE alert_items (
  alert_id INTEGER NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
  item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  PRIMARY KEY (alert_id, item_id)
);

CREATE TABLE pending_verify (
  id INTEGER PRIMARY KEY,
  alert_id INTEGER NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
  doc_number TEXT NOT NULL,
  norm TEXT NOT NULL,
  tries INTEGER NOT NULL DEFAULT 0,
  next_check_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE (alert_id, norm)
);

CREATE TABLE watched_hits (
  topic_id INTEGER NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
  doc_norm TEXT NOT NULL,
  hit_key TEXT NOT NULL,
  seen_at TEXT NOT NULL,
  PRIMARY KEY (topic_id, doc_norm, hit_key)
);

CREATE TABLE ai_results (
  content_hash TEXT NOT NULL,
  topic_id INTEGER NOT NULL,
  topic_version INTEGER NOT NULL,
  provider TEXT NOT NULL,
  result TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (content_hash, topic_id, topic_version)
);

CREATE TABLE scans (
  id INTEGER PRIMARY KEY,
  started_at TEXT NOT NULL,
  finished_at TEXT NOT NULL DEFAULT '',
  trigger TEXT NOT NULL,
  sources_ok INTEGER NOT NULL DEFAULT 0,
  sources_failed INTEGER NOT NULL DEFAULT 0,
  items_new INTEGER NOT NULL DEFAULT 0,
  alerts_new INTEGER NOT NULL DEFAULT 0,
  ai_calls INTEGER NOT NULL DEFAULT 0,
  errors TEXT NOT NULL DEFAULT '[]',
  note TEXT NOT NULL DEFAULT ''
);

CREATE TABLE chat_sessions (
  id INTEGER PRIMARY KEY,
  title TEXT NOT NULL DEFAULT '',
  scope TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL
);
CREATE TABLE chat_messages (
  id INTEGER PRIMARY KEY,
  session_id INTEGER NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
  role TEXT NOT NULL,
  content TEXT NOT NULL,
  citations TEXT NOT NULL DEFAULT '[]',
  provider TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);

CREATE TABLE chunks (
  id INTEGER PRIMARY KEY,
  owner_kind TEXT NOT NULL,
  owner_id INTEGER NOT NULL,
  path TEXT NOT NULL DEFAULT '',
  text TEXT NOT NULL
);
CREATE INDEX chunks_owner ON chunks(owner_kind, owner_id);
-- The index holds diacritic-folded text (see textutil.Fold): the built-in
-- remove_diacritics option does not fold "đ", so folding is done in Go.
CREATE VIRTUAL TABLE chunks_fts USING fts5(folded, tokenize = 'unicode61');

CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
`,
}

func (s *Store) migrate() error {
	var v int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	for i := v; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(`PRAGMA user_version = ` + strconv.Itoa(i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// ---- settings --------------------------------------------------------------

// Defaults lists every setting with its default value. Unknown keys are
// rejected by SetSettings so the UI cannot store arbitrary data.
var Defaults = map[string]string{
	"language":            "vi",    // vi | en: the language of the interface, the tray and the notifications
	"active_start":        "07:00", // scans run only inside this window
	"active_end":          "21:00",
	"quiet_start":         "21:00", // no toast inside this window
	"quiet_end":           "07:00",
	"interval_press":      "60",
	"interval_official":   "180",
	"lookback_days":       "7",
	"notify_info":         "0",
	"notify_notice":       "1",
	"notify_warning":      "1",
	"notify_system":       "1",
	"notify_group_over":   "3",
	"pause_until":         "",
	"autostart":           "0",
	"retention_days":      "30",
	"ai_provider":         "auto", // auto | none | claude-cli | codex-cli | ollama | http-openai | http-anthropic
	"ai_cli_path":         "",
	"ai_model":            "",
	"ai_base_url":         "",
	"ai_api_key":          "", // DPAPI-protected, base64
	"ai_max_calls":        "30",
	"ai_timeout_seconds":  "120",
	"verify_max_per_scan": "10",
	// Internal bookkeeping, not shown in the settings screen.
	"last_daily_at":         "",
	"last_system_notice_at": "",
}

// Setting returns the stored value of key or its default.
func (s *Store) Setting(key string) string {
	var v string
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v); err == nil {
		return v
	}
	return Defaults[key]
}

// SettingInt returns a setting as an integer, falling back to the default.
func (s *Store) SettingInt(key string) int {
	if n, err := strconv.Atoi(s.Setting(key)); err == nil {
		return n
	}
	n, _ := strconv.Atoi(Defaults[key])
	return n
}

// Settings returns all settings with defaults applied.
func (s *Store) Settings() map[string]string {
	out := make(map[string]string, len(Defaults))
	for k, v := range Defaults {
		out[k] = v
	}
	rows, err := s.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) == nil {
			if _, known := Defaults[k]; known {
				out[k] = v
			}
		}
	}
	return out
}

// SetSettings stores the given known settings.
func (s *Store) SetSettings(values map[string]string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, v := range values {
		if _, known := Defaults[k]; !known {
			return fmt.Errorf("unknown setting %q", k)
		}
		if _, err := tx.Exec(`INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---- helpers ---------------------------------------------------------------

func toJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

func strList(raw string) []string {
	out := []string{}
	json.Unmarshal([]byte(raw), &out)
	if out == nil {
		out = []string{}
	}
	return out
}

func intList(raw string) []int64 {
	out := []int64{}
	json.Unmarshal([]byte(raw), &out)
	if out == nil {
		out = []int64{}
	}
	return out
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

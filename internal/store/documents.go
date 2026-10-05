package store

import (
	"database/sql"
	"errors"
	"strings"

	"q3vnlaw/internal/textutil"
)

// ---- documents -------------------------------------------------------------

// Document is an official legal document taken from a tier-1 source.
type Document struct {
	ID            int64             `json:"id"`
	DocNumber     string            `json:"doc_number"`
	Norm          string            `json:"-"`
	Title         string            `json:"title"`
	DocType       string            `json:"doc_type"`
	Issuer        string            `json:"issuer"`
	Signer        string            `json:"signer"`
	IssuedAt      string            `json:"issued_at"`    // YYYY-MM-DD
	EffectiveAt   string            `json:"effective_at"` // YYYY-MM-DD, may be empty
	Validity      string            `json:"validity"`     // in_force | not_yet | superseded | amended | unknown
	ValidityBasis string            `json:"validity_basis"`
	SourceID      int64             `json:"source_id"`
	SourceURL     string            `json:"source_url"`
	PortalID      string            `json:"portal_id"`
	FileURLs      []string          `json:"file_urls"`
	Files         []string          `json:"files"` // paths relative to the data directory
	Meta          map[string]string `json:"meta"`
	HasText       bool              `json:"has_text"`
	FetchedAt     string            `json:"fetched_at"`

	Relations []Relation `json:"relations,omitempty"`
}

// Relation links two documents.
type Relation struct {
	Relation  string `json:"relation"`  // amends | replaces | guides | repeals | consolidates | mentions
	Basis     string `json:"basis"`     // stated | inferred
	Direction string `json:"direction"` // out: this document acts on the other; in: the other acts on this one
	DocID     int64  `json:"doc_id"`
	DocNumber string `json:"doc_number"`
	Title     string `json:"title"`
	IssuedAt  string `json:"issued_at"`
}

const docCols = `id, doc_number, norm, title, doc_type, issuer, signer, issued_at, effective_at, validity, validity_basis,
 source_id, source_url, portal_id, file_urls, files, meta, has_text, fetched_at`

func scanDoc(r interface{ Scan(...any) error }) (Document, error) {
	var d Document
	var fu, fs, meta string
	err := r.Scan(&d.ID, &d.DocNumber, &d.Norm, &d.Title, &d.DocType, &d.Issuer, &d.Signer, &d.IssuedAt, &d.EffectiveAt,
		&d.Validity, &d.ValidityBasis, &d.SourceID, &d.SourceURL, &d.PortalID, &fu, &fs, &meta, &d.HasText, &d.FetchedAt)
	d.FileURLs, d.Files = strList(fu), strList(fs)
	d.Meta = map[string]string{}
	jsonUnmarshal(meta, &d.Meta)
	return d, err
}

// SaveDocument inserts or refreshes a document, keyed by its normalized
// number, and returns its id. Downloaded files and extracted text are kept
// when the incoming record carries none.
func (s *Store) SaveDocument(d Document) (int64, error) {
	d.Norm = textutil.NormalizeDocNumber(d.DocNumber)
	if d.Norm == "" {
		return 0, errors.New("document without a number")
	}
	if d.Validity == "" {
		d.Validity = "unknown"
	}
	if d.ValidityBasis == "" {
		d.ValidityBasis = "none"
	}
	if d.Meta == nil {
		d.Meta = map[string]string{}
	}
	_, err := s.db.Exec(`INSERT INTO documents(doc_number, norm, title, doc_type, issuer, signer, issued_at, effective_at, validity,
 validity_basis, source_id, source_url, portal_id, file_urls, files, meta, has_text, fetched_at)
 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
 ON CONFLICT(norm) DO UPDATE SET title = excluded.title, doc_type = excluded.doc_type, issuer = excluded.issuer,
   signer = excluded.signer, issued_at = excluded.issued_at, effective_at = excluded.effective_at,
   validity = CASE WHEN excluded.validity_basis = 'none' AND documents.validity_basis <> 'none' THEN documents.validity ELSE excluded.validity END,
   validity_basis = CASE WHEN excluded.validity_basis = 'none' THEN documents.validity_basis ELSE excluded.validity_basis END,
   source_id = excluded.source_id, source_url = excluded.source_url, portal_id = excluded.portal_id,
   file_urls = excluded.file_urls, meta = excluded.meta,
   files = CASE WHEN excluded.files = '[]' THEN documents.files ELSE excluded.files END,
   has_text = MAX(documents.has_text, excluded.has_text), fetched_at = excluded.fetched_at`,
		d.DocNumber, d.Norm, d.Title, d.DocType, d.Issuer, d.Signer, d.IssuedAt, d.EffectiveAt, d.Validity, d.ValidityBasis,
		d.SourceID, d.SourceURL, d.PortalID, toJSON(nz(d.FileURLs)), toJSON(nz(d.Files)), toJSON(d.Meta), b2i(d.HasText), Now())
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.db.QueryRow(`SELECT id FROM documents WHERE norm = ?`, d.Norm).Scan(&id)
	return id, err
}

// SetDocumentFiles records downloaded files and whether text was extracted.
func (s *Store) SetDocumentFiles(id int64, files []string, hasText bool) error {
	_, err := s.db.Exec(`UPDATE documents SET files = ?, has_text = ? WHERE id = ?`, toJSON(nz(files)), b2i(hasText), id)
	return err
}

// SetDocumentValidity records an inferred or official validity status.
func (s *Store) SetDocumentValidity(id int64, validity, basis string) error {
	_, err := s.db.Exec(`UPDATE documents SET validity = ?, validity_basis = ? WHERE id = ?`, validity, basis, id)
	return err
}

// Document returns one document with its relations.
func (s *Store) Document(id int64) (Document, error) {
	d, err := scanDoc(s.db.QueryRow(`SELECT `+docCols+` FROM documents WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	d.Relations, err = s.relations(id)
	return d, err
}

// DocumentByNumber looks a document up by any spelling of its number.
func (s *Store) DocumentByNumber(number string) (Document, error) {
	d, err := scanDoc(s.db.QueryRow(`SELECT `+docCols+` FROM documents WHERE norm = ?`, textutil.NormalizeDocNumber(number)))
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	return d, err
}

// Documents lists stored documents, newest issue date first.
func (s *Store) Documents(query string, limit, offset int) ([]Document, int, error) {
	cond, args := "", []any{}
	if q := strings.TrimSpace(query); q != "" {
		cond = ` WHERE title LIKE ? OR doc_number LIKE ? OR norm LIKE ?`
		like := "%" + q + "%"
		args = append(args, like, like, "%"+textutil.NormalizeDocNumber(q)+"%")
	}
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM documents`+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT `+docCols+` FROM documents`+cond+` ORDER BY issued_at DESC, id DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Document{}
	for rows.Next() {
		d, err := scanDoc(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, d)
	}
	return out, total, rows.Err()
}

// AddRelation records that document from acts on document to.
func (s *Store) AddRelation(from, to int64, relation, basis string) error {
	if from == to || from == 0 || to == 0 {
		return nil
	}
	_, err := s.db.Exec(`INSERT OR IGNORE INTO doc_relations(from_doc, to_doc, relation, basis) VALUES(?, ?, ?, ?)`,
		from, to, relation, basis)
	return err
}

func (s *Store) relations(id int64) ([]Relation, error) {
	rows, err := s.db.Query(`SELECT r.relation, r.basis, 'out', d.id, d.doc_number, d.title, d.issued_at
 FROM doc_relations r JOIN documents d ON d.id = r.to_doc WHERE r.from_doc = ?
 UNION ALL
 SELECT r.relation, r.basis, 'in', d.id, d.doc_number, d.title, d.issued_at
 FROM doc_relations r JOIN documents d ON d.id = r.from_doc WHERE r.to_doc = ? ORDER BY 7 DESC`, id, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Relation{}
	for rows.Next() {
		var r Relation
		if err := rows.Scan(&r.Relation, &r.Basis, &r.Direction, &r.DocID, &r.DocNumber, &r.Title, &r.IssuedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DocumentAlerts lists the alerts that refer to a document.
func (s *Store) DocumentAlerts(documentID int64) ([]Alert, error) {
	rows, err := s.db.Query(`SELECT `+alertCols+alertFrom+` WHERE a.document_id = ? ORDER BY a.id DESC LIMIT 50`, documentID)
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

// ---- search chunks ---------------------------------------------------------

// Chunk is a searchable passage of a document or an article.
type Chunk struct {
	ID        int64   `json:"id"`
	OwnerKind string  `json:"owner_kind"` // document | item
	OwnerID   int64   `json:"owner_id"`
	Path      string  `json:"path"` // e.g. "Điều 12 > Khoản 3"
	Text      string  `json:"text"`
	Score     float64 `json:"-"`
}

// ReplaceChunks replaces all chunks of one owner.
func (s *Store) ReplaceChunks(ownerKind string, ownerID int64, chunks []Chunk) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM chunks_fts WHERE rowid IN (SELECT id FROM chunks WHERE owner_kind = ? AND owner_id = ?)`,
		ownerKind, ownerID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM chunks WHERE owner_kind = ? AND owner_id = ?`, ownerKind, ownerID); err != nil {
		return err
	}
	for _, c := range chunks {
		res, err := tx.Exec(`INSERT INTO chunks(owner_kind, owner_id, path, text) VALUES(?, ?, ?, ?)`, ownerKind, ownerID, c.Path, c.Text)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		if _, err := tx.Exec(`INSERT INTO chunks_fts(rowid, folded) VALUES(?, ?)`, id, textutil.Fold(c.Path+" "+c.Text)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ChunkScope restricts a search to one owner, or to everything when zero.
type ChunkScope struct {
	OwnerKind string
	OwnerID   int64
}

// SearchChunks runs a full-text query (FTS5 syntax over folded text) and
// returns the best passages, most relevant first.
func (s *Store) SearchChunks(match string, scope ChunkScope, limit int) ([]Chunk, error) {
	if strings.TrimSpace(match) == "" {
		return []Chunk{}, nil
	}
	q := `SELECT c.id, c.owner_kind, c.owner_id, c.path, c.text, bm25(chunks_fts) FROM chunks_fts
 JOIN chunks c ON c.id = chunks_fts.rowid WHERE chunks_fts MATCH ?`
	args := []any{match}
	if scope.OwnerKind != "" {
		q += ` AND c.owner_kind = ?`
		args = append(args, scope.OwnerKind)
		if scope.OwnerID != 0 {
			q += ` AND c.owner_id = ?`
			args = append(args, scope.OwnerID)
		}
	}
	rows, err := s.db.Query(q+` ORDER BY bm25(chunks_fts) LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Chunk{}
	for rows.Next() {
		var c Chunk
		if err := rows.Scan(&c.ID, &c.OwnerKind, &c.OwnerID, &c.Path, &c.Text, &c.Score); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// OwnerChunks returns the chunks of one owner in document order.
func (s *Store) OwnerChunks(ownerKind string, ownerID int64, limit int) ([]Chunk, error) {
	rows, err := s.db.Query(`SELECT id, owner_kind, owner_id, path, text FROM chunks WHERE owner_kind = ? AND owner_id = ?
 ORDER BY id LIMIT ?`, ownerKind, ownerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Chunk{}
	for rows.Next() {
		var c Chunk
		if err := rows.Scan(&c.ID, &c.OwnerKind, &c.OwnerID, &c.Path, &c.Text); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---- chat ------------------------------------------------------------------

// ChatSession is one conversation.
type ChatSession struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Scope     string `json:"scope"` // JSON: {"kind":"all|document|alert","id":0}
	CreatedAt string `json:"created_at"`
}

// Citation is a passage an answer relies on.
type Citation struct {
	N         int    `json:"n"`
	ChunkID   int64  `json:"chunk_id"`
	OwnerKind string `json:"owner_kind"`
	OwnerID   int64  `json:"owner_id"`
	Label     string `json:"label"` // document number or article title
	Path      string `json:"path"`
	Source    string `json:"source"`
	Tier      int    `json:"tier"`
	URL       string `json:"url"`
	Quote     string `json:"quote"`
	Used      bool   `json:"used"`
}

// ChatMessage is one turn of a conversation.
type ChatMessage struct {
	ID        int64      `json:"id"`
	SessionID int64      `json:"session_id"`
	Role      string     `json:"role"` // user | assistant
	Content   string     `json:"content"`
	Citations []Citation `json:"citations"`
	Provider  string     `json:"provider"`
	CreatedAt string     `json:"created_at"`
}

// NewChatSession creates a conversation.
func (s *Store) NewChatSession(title, scope string) (ChatSession, error) {
	if scope == "" {
		scope = `{"kind":"all"}`
	}
	cs := ChatSession{Title: title, Scope: scope, CreatedAt: Now()}
	res, err := s.db.Exec(`INSERT INTO chat_sessions(title, scope, created_at) VALUES(?, ?, ?)`, cs.Title, cs.Scope, cs.CreatedAt)
	if err != nil {
		return cs, err
	}
	cs.ID, _ = res.LastInsertId()
	return cs, nil
}

// ChatSessions lists conversations, newest first.
func (s *Store) ChatSessions() ([]ChatSession, error) {
	rows, err := s.db.Query(`SELECT id, title, scope, created_at FROM chat_sessions ORDER BY id DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatSession{}
	for rows.Next() {
		var c ChatSession
		if err := rows.Scan(&c.ID, &c.Title, &c.Scope, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ChatSession returns one conversation.
func (s *Store) ChatSession(id int64) (ChatSession, error) {
	var c ChatSession
	err := s.db.QueryRow(`SELECT id, title, scope, created_at FROM chat_sessions WHERE id = ?`, id).Scan(&c.ID, &c.Title, &c.Scope, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// SetChatTitle renames a conversation.
func (s *Store) SetChatTitle(id int64, title string) error {
	_, err := s.db.Exec(`UPDATE chat_sessions SET title = ? WHERE id = ?`, title, id)
	return err
}

// DeleteChatSession removes a conversation and its messages.
func (s *Store) DeleteChatSession(id int64) error {
	_, err := s.db.Exec(`DELETE FROM chat_sessions WHERE id = ?`, id)
	return err
}

// AddChatMessage appends a message to a conversation.
func (s *Store) AddChatMessage(m ChatMessage) (ChatMessage, error) {
	if m.Citations == nil {
		m.Citations = []Citation{}
	}
	m.CreatedAt = Now()
	res, err := s.db.Exec(`INSERT INTO chat_messages(session_id, role, content, citations, provider, created_at) VALUES(?, ?, ?, ?, ?, ?)`,
		m.SessionID, m.Role, m.Content, toJSON(m.Citations), m.Provider, m.CreatedAt)
	if err != nil {
		return m, err
	}
	m.ID, _ = res.LastInsertId()
	return m, nil
}

// ChatMessages returns the messages of a conversation in order.
func (s *Store) ChatMessages(sessionID int64) ([]ChatMessage, error) {
	rows, err := s.db.Query(`SELECT id, session_id, role, content, citations, provider, created_at FROM chat_messages
 WHERE session_id = ? ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatMessage{}
	for rows.Next() {
		var m ChatMessage
		var cit string
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &cit, &m.Provider, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.Citations = []Citation{}
		jsonUnmarshal(cit, &m.Citations)
		out = append(out, m)
	}
	return out, rows.Err()
}

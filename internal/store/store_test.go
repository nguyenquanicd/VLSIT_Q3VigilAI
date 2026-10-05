package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	// A directory with a space and Vietnamese letters, as a user's folder may have.
	dir := filepath.Join(t.TempDir(), "thư mục dữ liệu")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := Open(filepath.Join(dir, "q3vnlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenTwiceKeepsData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a b", "q3.db")
	os.MkdirAll(filepath.Dir(path), 0o755)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSettings(map[string]string{"active_start": "08:30"}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path) // migrations must not run again
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := s.Setting("active_start"); got != "08:30" {
		t.Errorf("setting lost: %q", got)
	}
	if got := s.Setting("active_end"); got != "21:00" {
		t.Errorf("default: %q", got)
	}
	if err := s.SetSettings(map[string]string{"nope": "1"}); err == nil {
		t.Error("unknown setting accepted")
	}
	if s.SettingInt("ai_max_calls") != 30 {
		t.Error("SettingInt default")
	}
}

func TestSources(t *testing.T) {
	s := open(t)
	b := Source{Key: "vnexpress", Name: "VnExpress", Domain: "vnexpress.net", Tier: 3, Kind: "press", Connector: "rss",
		Config: `{"url":"https://vnexpress.net/rss/phap-luat.rss"}`, IntervalMinutes: 60, Enabled: true}
	if err := s.UpsertBuiltin(b); err != nil {
		t.Fatal(err)
	}
	list, _ := s.Sources()
	if len(list) != 1 || !list[0].Builtin || !list[0].Enabled {
		t.Fatalf("sources: %+v", list)
	}
	id := list[0].ID
	// The user disables it and changes the interval; an upgrade must not undo that.
	if err := s.UpdateSourceUser(id, false, 120); err != nil {
		t.Fatal(err)
	}
	b.Name = "VnExpress Pháp luật"
	if err := s.UpsertBuiltin(b); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Source(id)
	if got.Enabled || got.IntervalMinutes != 120 || got.Name != "VnExpress Pháp luật" {
		t.Errorf("after upgrade: %+v", got)
	}
	if err := s.DeleteSource(id); err != ErrNotFound {
		t.Errorf("builtin source deleted: %v", err)
	}
	cid, err := s.AddSource(Source{Name: "Custom", Domain: "example.vn", Tier: 3, Kind: "press", Connector: "rss", Config: "{}", IntervalMinutes: 60})
	if err != nil {
		t.Fatal(err)
	}
	s.RecordSourceScan(cid, ScanOutcome{Status: "error", Error: "boom"})
	s.RecordSourceScan(cid, ScanOutcome{Status: "error", Error: "boom"})
	got, _ = s.Source(cid)
	if got.FailCount != 2 || got.LastStatus != "error" || got.LastError != "boom" {
		t.Errorf("fail tracking: %+v", got)
	}
	s.RecordSourceScan(cid, ScanOutcome{Status: "ok", ETag: `"abc"`})
	got, _ = s.Source(cid)
	if got.FailCount != 0 || got.LastOKAt == "" || got.ETag != `"abc"` {
		t.Errorf("ok tracking: %+v", got)
	}
	if err := s.DeleteSource(cid); err != nil {
		t.Error(err)
	}
	if err := s.RemoveBuiltinsExcept([]string{"other"}); err != nil {
		t.Fatal(err)
	}
	if list, _ = s.Sources(); len(list) != 0 {
		t.Errorf("stale builtin kept: %+v", list)
	}
}

func seed(t *testing.T, s *Store) (srcID, topicID int64) {
	t.Helper()
	srcID, err := s.AddSource(Source{Name: "Báo A", Domain: "a.vn", Tier: 3, Kind: "press", Connector: "rss", Config: "{}", IntervalMinutes: 60})
	if err != nil {
		t.Fatal(err)
	}
	topicID, err = s.SaveTopic(Topic{Name: "Thuế", Keywords: []string{"thuế"}, Enabled: true, RemindDays: 7})
	if err != nil {
		t.Fatal(err)
	}
	return
}

func TestTopics(t *testing.T) {
	s := open(t)
	_, id := seed(t, s)
	tp, err := s.Topic(id)
	if err != nil || tp.Version != 1 || len(tp.Kinds) != 3 || tp.Keywords[0] != "thuế" {
		t.Fatalf("topic: %+v %v", tp, err)
	}
	tp.Keywords = append(tp.Keywords, "hóa đơn")
	tp.WatchedDocs = []string{"13/2023/NĐ-CP"}
	if _, err := s.SaveTopic(tp); err != nil {
		t.Fatal(err)
	}
	tp, _ = s.Topic(id)
	if tp.Version != 2 || len(tp.Keywords) != 2 || tp.WatchedDocs[0] != "13/2023/NĐ-CP" {
		t.Errorf("after update: %+v", tp)
	}
	tp.Enabled = false
	s.SaveTopic(tp)
	if on, _ := s.Topics(true); len(on) != 0 {
		t.Errorf("disabled topic listed: %+v", on)
	}
	if _, err := s.SaveTopic(Topic{ID: 999, Name: "x"}); err != ErrNotFound {
		t.Errorf("update missing: %v", err)
	}
}

func TestItemsAndAlerts(t *testing.T) {
	s := open(t)
	src, topic := seed(t, s)
	id1, err := s.AddItem(Item{SourceID: src, URL: "https://a.vn/1", Title: "Nghị định mới về thuế", DocNumbers: []string{"1/2026/NĐ-CP"}})
	if err != nil || id1 == 0 {
		t.Fatal(id1, err)
	}
	if dup, _ := s.AddItem(Item{SourceID: src, URL: "https://a.vn/1", Title: "dup"}); dup != 0 {
		t.Error("duplicate URL inserted")
	}
	if !s.ItemSeen("https://a.vn/1") || s.ItemSeen("https://a.vn/none") {
		t.Error("ItemSeen")
	}
	id2, _ := s.AddItem(Item{SourceID: src, URL: "https://a.vn/2", Title: "Báo khác đưa cùng tin"})

	aid, err := s.AddAlert(Alert{TopicID: topic, Kind: "press", Severity: "notice", LegalStatus: "issued", Verified: "unconfirmed",
		Title: "Nghị định mới về thuế", ClusterKey: "doc:1/2026/ND-CP", PrimaryItemID: id1, State: "unread", NeedsNotify: true,
		DocNumbers: []string{"1/2026/NĐ-CP"}, MatchedKeywords: []string{"thuế"}})
	if err != nil {
		t.Fatal(err)
	}
	found, ok := s.ClusterAlert(topic, "doc:1/2026/ND-CP", time.Now().Add(-time.Hour))
	if !ok || found != aid {
		t.Errorf("cluster lookup: %d %v", found, ok)
	}
	if _, ok := s.ClusterAlert(topic, "doc:1/2026/ND-CP", time.Now().Add(time.Hour)); ok {
		t.Error("cluster lookup ignored the time window")
	}
	s.AttachItem(aid, id2)
	s.AttachItem(aid, id2) // idempotent

	a, err := s.Alert(aid)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Items) != 2 || a.SourceCount != 2 || a.TopicName != "Thuế" || a.SourceName != "Báo A" || a.URL != "https://a.vn/1" {
		t.Errorf("alert detail: %+v", a)
	}
	if s.UnreadCount() != 1 {
		t.Error("unread count")
	}
	pend, _ := s.PendingNotifications()
	if len(pend) != 1 {
		t.Fatalf("pending: %d", len(pend))
	}
	s.MarkNotified([]int64{aid})
	if pend, _ = s.PendingNotifications(); len(pend) != 0 {
		t.Error("still pending after MarkNotified")
	}

	// A filtered alert stays out of the inbox but is listed on request.
	fid, _ := s.AddAlert(Alert{TopicID: topic, Kind: "press", Severity: "info", Title: "Không liên quan", State: "filtered"})
	inbox, total, _ := s.Alerts(AlertFilter{})
	if total != 1 || len(inbox) != 1 || inbox[0].ID != aid {
		t.Errorf("inbox: total=%d %+v", total, inbox)
	}
	filtered, _, _ := s.Alerts(AlertFilter{State: "filtered"})
	if len(filtered) != 1 || filtered[0].ID != fid {
		t.Errorf("filtered: %+v", filtered)
	}
	if hits, _, _ := s.Alerts(AlertFilter{Query: "1/2026"}); len(hits) != 1 {
		t.Errorf("query: %+v", hits)
	}
	if err := s.SetAlertState(aid, "bogus"); err == nil {
		t.Error("invalid state accepted")
	}
	s.SetAlertState(aid, "read")
	if s.UnreadCount() != 0 {
		t.Error("unread after read")
	}

	// Items behind an alert survive the purge; the others do not.
	old, _ := s.AddItem(Item{SourceID: src, URL: "https://a.vn/old", Title: "cũ"})
	s.ReplaceChunks("item", old, []Chunk{{Text: "nội dung cũ"}})
	n, err := s.PurgeItems(time.Now().Add(time.Hour))
	if err != nil || n != 1 {
		t.Errorf("purge removed %d (%v), want 1", n, err)
	}
	if _, err := s.Item(id1); err != nil {
		t.Error("alerted item purged")
	}
	if c, _ := s.OwnerChunks("item", old, 10); len(c) != 0 {
		t.Error("chunks of purged item kept")
	}

	// Deleting the topic removes its alerts.
	s.DeleteTopic(topic)
	if _, err := s.Alert(aid); err != ErrNotFound {
		t.Errorf("alert survived topic deletion: %v", err)
	}
}

func TestDocumentsAndRelations(t *testing.T) {
	s := open(t)
	base, err := s.SaveDocument(Document{DocNumber: "13/2023/NĐ-CP", Title: "Nghị định bảo vệ dữ liệu cá nhân", IssuedAt: "2023-04-17",
		EffectiveAt: "2023-07-01", Files: []string{"docs/13_2023_ND-CP/13.pdf"}, HasText: true})
	if err != nil {
		t.Fatal(err)
	}
	// Saving again without files keeps the downloaded ones.
	again, _ := s.SaveDocument(Document{DocNumber: "13/2023/nd-cp", Title: "Nghị định BVDLCN (cập nhật)", IssuedAt: "2023-04-17"})
	if again != base {
		t.Fatalf("normalized number not matched: %d vs %d", again, base)
	}
	d, _ := s.DocumentByNumber("13/2023/ND-CP")
	if len(d.Files) != 1 || !d.HasText || d.Title != "Nghị định BVDLCN (cập nhật)" {
		t.Errorf("merge: %+v", d)
	}
	amend, _ := s.SaveDocument(Document{DocNumber: "99/2026/NĐ-CP", Title: "Sửa đổi Nghị định 13/2023/NĐ-CP", IssuedAt: "2026-09-01"})
	s.AddRelation(amend, base, "amends", "stated")
	d, _ = s.Document(base)
	if len(d.Relations) != 1 || d.Relations[0].Direction != "in" || d.Relations[0].DocNumber != "99/2026/NĐ-CP" {
		t.Errorf("relations: %+v", d.Relations)
	}
	s.SetDocumentValidity(base, "amended", "inferred")
	// A later refresh without validity knowledge must not erase the inference.
	s.SaveDocument(Document{DocNumber: "13/2023/NĐ-CP", Title: "x"})
	d, _ = s.Document(base)
	if d.Validity != "amended" || d.ValidityBasis != "inferred" {
		t.Errorf("validity overwritten: %s/%s", d.Validity, d.ValidityBasis)
	}
	list, total, _ := s.Documents("13/2023", 10, 0)
	if total != 2 || len(list) != 2 { // both the base and the amendment mention it
		t.Errorf("search: total=%d", total)
	}
}

func TestChunkSearchIsDiacriticInsensitive(t *testing.T) {
	s := open(t)
	err := s.ReplaceChunks("document", 1, []Chunk{
		{Path: "Điều 5", Text: "Người sử dụng đất được cấp giấy chứng nhận quyền sử dụng đất đai."},
		{Path: "Điều 6", Text: "Thuế giá trị gia tăng áp dụng mức 10%."},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.ReplaceChunks("item", 7, []Chunk{{Text: "Bài báo nói về đất đai và nhà ở."}})

	hits, err := s.SearchChunks(`"dat" AND "dai"`, ChunkScope{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("folded search found %d chunks, want 2", len(hits))
	}
	hits, _ = s.SearchChunks(`"dat dai"`, ChunkScope{OwnerKind: "document", OwnerID: 1}, 10)
	if len(hits) != 1 || hits[0].Path != "Điều 5" {
		t.Errorf("scoped search: %+v", hits)
	}
	// Replacing chunks must remove the old index entries.
	s.ReplaceChunks("document", 1, []Chunk{{Path: "Điều 1", Text: "Phạm vi điều chỉnh."}})
	if hits, _ = s.SearchChunks(`"thue"`, ChunkScope{}, 10); len(hits) != 0 {
		t.Errorf("stale index entries: %+v", hits)
	}
}

func TestChatAndScansAndVerify(t *testing.T) {
	s := open(t)
	_, topic := seed(t, s)
	cs, err := s.NewChatSession("Hỏi về thuế", "")
	if err != nil {
		t.Fatal(err)
	}
	s.AddChatMessage(ChatMessage{SessionID: cs.ID, Role: "user", Content: "Thuế GTGT bao nhiêu?"})
	s.AddChatMessage(ChatMessage{SessionID: cs.ID, Role: "assistant", Content: "10% [1]", Citations: []Citation{{N: 1, Label: "Luật", Quote: "mức 10%"}}})
	msgs, _ := s.ChatMessages(cs.ID)
	if len(msgs) != 2 || msgs[1].Citations[0].Quote != "mức 10%" || len(msgs[0].Citations) != 0 {
		t.Errorf("messages: %+v", msgs)
	}
	s.DeleteChatSession(cs.ID)
	if msgs, _ = s.ChatMessages(cs.ID); len(msgs) != 0 {
		t.Error("messages survived session deletion")
	}

	id, _ := s.StartScan("manual")
	s.FinishScan(Scan{ID: id, SourcesOK: 3, SourcesFailed: 1, ItemsNew: 12, AlertsNew: 2, Errors: []SourceError{{Source: "Báo A", Error: "timeout"}}})
	scans, _ := s.Scans(10)
	if len(scans) != 1 || scans[0].FinishedAt == "" || scans[0].Errors[0].Error != "timeout" {
		t.Errorf("scans: %+v", scans)
	}

	aid, _ := s.AddAlert(Alert{TopicID: topic, Kind: "press", Severity: "info", Title: "t", State: "unread"})
	s.QueueVerify(aid, "1/2026/NĐ-CP", "1/2026/ND-CP", time.Now().Add(-time.Minute))
	s.QueueVerify(aid, "1/2026/NĐ-CP", "1/2026/ND-CP", time.Now().Add(-time.Minute)) // no duplicate
	due, _ := s.DueVerifications(10)
	if len(due) != 1 {
		t.Fatalf("due: %d", len(due))
	}
	s.RescheduleVerify(due[0].ID, time.Now().Add(time.Hour))
	if due, _ = s.DueVerifications(10); len(due) != 0 {
		t.Error("rescheduled verification still due")
	}
	doc, _ := s.SaveDocument(Document{DocNumber: "1/2026/NĐ-CP", EffectiveAt: "2026-12-01"})
	s.ConfirmAlert(aid, doc, "2026-12-01", true)
	a, _ := s.Alert(aid)
	if a.Verified != "confirmed" || a.Document == nil || a.EffectiveAt != "2026-12-01" {
		t.Errorf("confirm: %+v", a)
	}
	if pend, _ := s.PendingNotifications(); len(pend) != 1 {
		t.Error("confirmation did not queue a toast")
	}

	seen, _ := s.WatchedHitSeen(topic, "13/2023/ND-CP", "200001")
	seenAgain, _ := s.WatchedHitSeen(topic, "13/2023/ND-CP", "200001")
	if seen || !seenAgain || !s.WatchedBaselined(topic, "13/2023/ND-CP") {
		t.Errorf("watched hits: %v %v", seen, seenAgain)
	}

	if _, _, ok := s.CachedVerdict("h", topic, 1); ok {
		t.Error("cache hit on empty cache")
	}
	s.CacheVerdict("h", topic, 1, "fake", `{"relevant":true}`)
	if r, p, ok := s.CachedVerdict("h", topic, 1); !ok || p != "fake" || r == "" {
		t.Error("cache miss")
	}
	if _, _, ok := s.CachedVerdict("h", topic, 2); ok {
		t.Error("cache ignored the topic version")
	}

	dest := filepath.Join(t.TempDir(), "backup.db")
	if err := s.Backup(dest); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(dest); err != nil || fi.Size() == 0 {
		t.Error("backup file missing")
	}
}

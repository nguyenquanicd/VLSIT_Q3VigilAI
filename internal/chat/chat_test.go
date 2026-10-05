package chat

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"q3vnlaw/internal/ai"
	"q3vnlaw/internal/store"
)

type fake struct {
	reply   string
	err     error
	prompts []ai.Request
}

func (f *fake) Name() string { return "fake" }
func (f *fake) Complete(ctx context.Context, r ai.Request) (string, error) {
	f.prompts = append(f.prompts, r)
	return f.reply, f.err
}
func (f *fake) Stream(ctx context.Context, r ai.Request, on func(string)) (string, error) {
	f.prompts = append(f.prompts, r)
	if f.err != nil {
		return "", f.err
	}
	on(f.reply)
	return f.reply, nil
}

func setup(t *testing.T) (*store.Store, int64, int64) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "q3.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	docID, _ := st.SaveDocument(store.Document{DocNumber: "381/2026/NĐ-CP", Title: "Nghị định về hóa đơn điện tử",
		SourceURL: "https://vanban.chinhphu.vn/?pageid=27160&docid=900", EffectiveAt: "2026-12-01"})
	st.ReplaceChunks("document", docID, []store.Chunk{
		{Path: "Điều 4", Text: "Điều 4. Người bán phải lập hóa đơn điện tử có mã của cơ quan thuế khi bán hàng hóa, cung cấp dịch vụ."},
		{Path: "Điều 20", Text: "Điều 20. Nghị định này có hiệu lực thi hành từ ngày 01 tháng 12 năm 2026."},
	})
	srcID, _ := st.AddSource(store.Source{Name: "Báo A", Domain: "a.vn", Tier: 3, Kind: "press", Connector: "rss", Config: "{}", IntervalMinutes: 60})
	itemID, _ := st.AddItem(store.Item{SourceID: srcID, URL: "https://a.vn/1", Title: "Từ 1/12 bắt buộc dùng hóa đơn điện tử"})
	st.ReplaceChunks("item", itemID, []store.Chunk{{Text: "Doanh nghiệp cần chuẩn bị phần mềm hóa đơn điện tử trước ngày 1/12."}})
	other, _ := st.AddItem(store.Item{SourceID: srcID, URL: "https://a.vn/2", Title: "Giá vàng hôm nay"})
	st.ReplaceChunks("item", other, []store.Chunk{{Text: "Giá vàng miếng tăng mạnh trong phiên sáng."}})
	return st, docID, itemID
}

func TestMatchQuery(t *testing.T) {
	q := MatchQuery("Hóa đơn điện tử có hiệu lực từ khi nào?")
	for _, want := range []string{`"hoa"`, `"don"`, `"dien"`, `"hoa don"`, `"dien tu"`, `"hieu luc"`} {
		if !strings.Contains(q, want) {
			t.Errorf("query %q lacks %s", q, want)
		}
	}
	// "tử" of "điện tử" is kept; "từ" (from) is dropped, so no "luc tu" phrase.
	for _, stop := range []string{`"co"`, `"khi"`, `"nao"`, `"luc tu"`, `"tu khi"`} {
		if strings.Contains(q, stop+" ") || strings.HasSuffix(q, stop) {
			t.Errorf("stopword %s kept in %q", stop, q)
		}
	}
	if MatchQuery("là gì?") != "" {
		t.Errorf("only stopwords: %q", MatchQuery("là gì?"))
	}
	// FTS syntax characters in a question must not break the query.
	if q := MatchQuery(`"thuế" AND (NOT x) * NEAR`); strings.ContainsAny(strings.ReplaceAll(q, `"`, ""), "()*") {
		t.Errorf("unsafe query: %q", q)
	}
}

func TestRetrievePrefersOfficialAndHonoursScope(t *testing.T) {
	st, docID, itemID := setup(t)
	s := &Service{St: st}
	cites, err := s.Retrieve("hóa đơn điện tử có mã của cơ quan thuế", Scope{Kind: "all"})
	if err != nil || len(cites) < 2 {
		t.Fatalf("retrieve: %v %+v", err, cites)
	}
	if cites[0].OwnerKind != "document" || cites[0].Label != "381/2026/NĐ-CP" || cites[0].Tier != 1 || cites[0].Path != "Điều 4" {
		t.Errorf("first citation: %+v", cites[0])
	}
	for _, c := range cites {
		if strings.Contains(c.Quote, "vàng") {
			t.Errorf("irrelevant passage retrieved: %+v", c)
		}
		if c.URL == "" || c.Source == "" {
			t.Errorf("citation without source: %+v", c)
		}
	}
	// A question naming the decree pulls it in even with no other keyword.
	cites, _ = s.Retrieve("381/2026/ND-CP nói gì?", Scope{Kind: "all"})
	if len(cites) == 0 || cites[0].OwnerID != docID {
		t.Errorf("lookup by number: %+v", cites)
	}
	// Scope to the document: the press item must not appear.
	cites, _ = s.Retrieve("hóa đơn điện tử", Scope{Kind: "document", ID: docID})
	for _, c := range cites {
		if c.OwnerKind != "document" {
			t.Errorf("scope leak: %+v", c)
		}
	}
	// Scope to an alert: its article and nothing else.
	topic, _ := st.SaveTopic(store.Topic{Name: "T", Enabled: true})
	alert, _ := st.AddAlert(store.Alert{TopicID: topic, Kind: "press", Severity: "info", Title: "x", State: "unread", PrimaryItemID: itemID})
	cites, _ = s.Retrieve("câu hỏi không khớp từ nào", Scope{Kind: "alert", ID: alert})
	if len(cites) != 1 || cites[0].OwnerID != itemID {
		t.Errorf("alert scope fallback: %+v", cites)
	}
}

func TestAskWithCitations(t *testing.T) {
	st, _, _ := setup(t)
	f := &fake{reply: "Người bán phải lập hóa đơn điện tử có mã [1]. Quy định có hiệu lực từ 01/12/2026 [2]. Xem thêm [9]."}
	s := &Service{St: st, Provider: func() ai.Provider { return f }}
	sess, _ := st.NewChatSession("", "")
	var streamed strings.Builder
	msg, err := s.Ask(context.Background(), sess.ID, "Hóa đơn điện tử có hiệu lực từ khi nào?", func(d string) { streamed.WriteString(d) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(msg.Content, "[9]") {
		t.Errorf("citation mark pointing at nothing was kept: %q", msg.Content)
	}
	used := 0
	for _, c := range msg.Citations {
		if c.Used {
			used++
		}
	}
	if used != 2 || msg.Provider != "fake" || streamed.Len() == 0 {
		t.Errorf("used=%d provider=%s streamed=%d", used, msg.Provider, streamed.Len())
	}
	// The prompt carries the passages and the rules, and fences web content.
	p := f.prompts[0]
	if !strings.Contains(p.System, "Chỉ trả lời dựa trên các ĐOẠN TRÍCH") || !strings.Contains(p.Prompt, `<doan so="1" loai="văn bản chính thức"`) ||
		!strings.Contains(p.Prompt, "CÂU HỎI: Hóa đơn điện tử") {
		t.Errorf("prompt:\n%s", p.Prompt)
	}
	msgs, _ := st.ChatMessages(sess.ID)
	if len(msgs) != 2 || msgs[0].Role != "user" || msgs[1].Role != "assistant" {
		t.Fatalf("stored messages: %+v", msgs)
	}
	if got, _ := st.ChatSession(sess.ID); got.Title == "" {
		t.Error("session not titled from the first question")
	}
	// A follow-up sees the earlier turns.
	s.Ask(context.Background(), sess.ID, "Còn hóa đơn giấy thì sao?", func(string) {})
	if !strings.Contains(f.prompts[1].Prompt, "HỘI THOẠI TRƯỚC ĐÓ") {
		t.Error("history missing from the follow-up prompt")
	}
}

func TestAskWithoutBasisDoesNotCallTheModel(t *testing.T) {
	st, _, _ := setup(t)
	f := &fake{reply: "Theo tôi nhớ thì..."}
	s := &Service{St: st, Provider: func() ai.Provider { return f }}
	sess, _ := st.NewChatSession("", "")
	msg, err := s.Ask(context.Background(), sess.ID, "Quy định về nuôi chim yến ở đô thị?", func(string) {})
	if err != nil || msg.Content != NoBasis || len(f.prompts) != 0 {
		t.Errorf("no-basis answer: %q calls=%d err=%v", msg.Content, len(f.prompts), err)
	}
}

func TestAskWithoutAIAndOnFailure(t *testing.T) {
	st, _, _ := setup(t)
	s := &Service{St: st}
	sess, _ := st.NewChatSession("", "")
	msg, err := s.Ask(context.Background(), sess.ID, "hóa đơn điện tử", func(string) {})
	if err != nil || !strings.Contains(msg.Content, "Chưa có AI") || len(msg.Citations) == 0 || !msg.Citations[0].Used {
		t.Errorf("search-only mode: %+v %v", msg, err)
	}
	f := &fake{err: errors.New("hết hạn mức")}
	s.Provider = func() ai.Provider { return f }
	msg, err = s.Ask(context.Background(), sess.ID, "hóa đơn điện tử", func(string) {})
	if err == nil || !strings.Contains(msg.Content, "hết hạn mức") {
		t.Errorf("failure must be stored and returned: %+v %v", msg, err)
	}
	if _, err := s.Ask(context.Background(), 9999, "x", func(string) {}); err != store.ErrNotFound {
		t.Errorf("missing session: %v", err)
	}
}

func TestAuditLabelsUncitedAnswers(t *testing.T) {
	cites := []store.Citation{{N: 1}, {N: 2}}
	text, out := Audit("Câu trả lời không có dấu trích dẫn.", cites)
	if !strings.Contains(text, "không dẫn tới đoạn căn cứ") || out[0].Used || out[1].Used {
		t.Errorf("uncited answer: %q %+v", text, out)
	}
	if cites[0].Used {
		t.Error("Audit modified its input")
	}
}

// Package pipeline is the scan engine: list sources, keep what is new, match
// it against the user's topics without AI, let the AI judge the matches,
// confirm against official sources and raise alerts.
package pipeline

import (
	"slices"
	"strings"
	"time"

	"q3vigilai/internal/store"
	"q3vigilai/internal/textutil"
)

// FieldKeywords maps each selectable field of law to the phrases that
// signal it. Choosing a field in a topic is the same as adding its phrases.
var FieldKeywords = map[string][]string{
	"Thuế":                       {"thuế", "hóa đơn điện tử", "quyết toán thuế", "hoàn thuế", "khấu trừ thuế", "kê khai thuế", "lệ phí môn bài"},
	"Lao động":                   {"hợp đồng lao động", "người lao động", "tiền lương", "lương tối thiểu", "tuổi nghỉ hưu", "làm thêm giờ", "an toàn lao động", "trợ cấp thôi việc"},
	"Bảo hiểm xã hội":            {"bảo hiểm xã hội", "BHXH", "bảo hiểm y tế", "BHYT", "bảo hiểm thất nghiệp", "lương hưu"},
	"Doanh nghiệp":               {"luật doanh nghiệp", "đăng ký doanh nghiệp", "đăng ký kinh doanh", "hộ kinh doanh", "vốn điều lệ", "giải thể doanh nghiệp"},
	"Đầu tư":                     {"luật đầu tư", "ưu đãi đầu tư", "đăng ký đầu tư", "đầu tư nước ngoài", "đối tác công tư"},
	"Kế toán – Kiểm toán":        {"chế độ kế toán", "chuẩn mực kế toán", "báo cáo tài chính", "kiểm toán"},
	"Hải quan – Xuất nhập khẩu":  {"hải quan", "xuất khẩu", "nhập khẩu", "thuế nhập khẩu", "xuất xứ hàng hóa"},
	"Đất đai – Xây dựng":         {"đất đai", "quyền sử dụng đất", "giấy phép xây dựng", "bồi thường giải phóng mặt bằng"},
	"Dữ liệu – An ninh mạng":     {"dữ liệu cá nhân", "an ninh mạng", "an toàn thông tin", "chữ ký số", "giao dịch điện tử"},
	"Sở hữu trí tuệ":             {"sở hữu trí tuệ", "sáng chế", "nhãn hiệu", "quyền tác giả"},
	"Công nghệ – Bán dẫn":        {"công nghiệp bán dẫn", "vi mạch", "công nghệ cao", "công nghệ số", "trí tuệ nhân tạo", "chuyển đổi số"},
	"Xử phạt vi phạm hành chính": {"xử phạt vi phạm hành chính", "mức phạt"},
}

// Fields returns the selectable field names in a stable order.
func Fields() []string {
	out := make([]string, 0, len(FieldKeywords))
	for k := range FieldKeywords {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Match is the result of testing an item against a topic without AI.
type Match struct {
	Keywords []string // topic keywords and field phrases found
	Watched  []string // watched document numbers the item mentions
}

// OK reports whether the item is a candidate for the topic.
func (m Match) OK() bool { return len(m.Keywords) > 0 || len(m.Watched) > 0 }

// MatchTopic applies the topic's source, kind, exclusion and keyword rules to
// an item's text. sourceKind is "press" or "official".
func MatchTopic(t store.Topic, sourceID int64, sourceKind, text string) Match {
	if len(t.SourceIDs) > 0 && !slices.Contains(t.SourceIDs, sourceID) {
		return Match{}
	}
	if !slices.Contains(t.Kinds, sourceKind) {
		return Match{}
	}
	prepared := textutil.Prepare(text)
	if len(prepared.Matches(t.ExcludeKeywords)) > 0 {
		return Match{}
	}
	var m Match
	m.Keywords = prepared.Matches(t.Keywords)
	for _, f := range t.Fields {
		for _, k := range prepared.Matches(FieldKeywords[f]) {
			if !slices.Contains(m.Keywords, k) {
				m.Keywords = append(m.Keywords, k)
			}
		}
	}
	if len(t.WatchedDocs) > 0 {
		found := map[string]bool{}
		for _, n := range textutil.ExtractDocNumbers(text) {
			found[textutil.NormalizeDocNumber(n)] = true
		}
		for _, w := range t.WatchedDocs {
			if found[textutil.NormalizeDocNumber(w)] {
				m.Watched = append(m.Watched, w)
			}
		}
	}
	return m
}

// GuessStatus reads the standard wording of Vietnamese legal news to place
// an item on the path from proposal to law. It is used only when no AI is
// available; the result is a keyword heuristic and is labelled as such.
func GuessStatus(text string, now time.Time) string {
	// Punctuation becomes spaces so "dự thảo." still matches " du thao ".
	f := " " + strings.Join(strings.FieldsFunc(textutil.Fold(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	}), " ") + " "
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(f, w) {
				return true
			}
		}
		return false
	}
	switch {
	case has(" du thao ", " lay y kien ", " dang soan thao ", " sap co ", " sap ban hanh ", " se ban hanh ", " dang xay dung ",
		" trinh chinh phu ", " trinh quoc hoi "):
		return "draft"
	case has(" de xuat ", " kien nghi ", " du kien ", " nghien cuu sua doi "):
		return "proposal"
	case has(" co hieu luc tu ", " co hieu luc thi hanh ", " chinh thuc ap dung ", " bat dau ap dung ", " chinh thuc co hieu luc "):
		return "issued"
	case has(" ban hanh ", " vua ky ", " da ky ", " thong qua luat ", " quoc hoi thong qua "):
		return "issued"
	}
	for _, n := range textutil.ExtractDocNumbers(text) {
		if y := textutil.DocNumberYear(n); y >= now.Year()-1 && y <= now.Year() {
			return "issued"
		}
	}
	return "unknown"
}

// PrimaryDocs picks the document numbers an item is about, as opposed to
// laws it merely cites: numbers in the title first, otherwise recent numbers
// in the body. At most three are returned.
func PrimaryDocs(title, body string, now time.Time) []string {
	out := textutil.ExtractDocNumbers(title)
	if len(out) == 0 {
		for _, n := range textutil.ExtractDocNumbers(body) {
			if y := textutil.DocNumberYear(n); y >= now.Year()-1 && y <= now.Year() {
				out = append(out, n)
			}
		}
	}
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

// IsRecent reports whether a document number carries this year or last year.
func IsRecent(number string, now time.Time) bool {
	y := textutil.DocNumberYear(number)
	return y >= now.Year()-1 && y <= now.Year()
}

// ClusterKey groups reports of the same event: by the document they are
// about, or failing that by their headline.
func ClusterKey(primary []string, title string) string {
	if len(primary) > 0 {
		return "doc:" + textutil.NormalizeDocNumber(primary[0])
	}
	return "title:" + textutil.Hash(textutil.Fold(title))
}

// Severity applies the alert rules of the specification.
func Severity(sourceKind string, m Match, legalStatus, relevance string, hasAI bool) string {
	status := legalStatus == "issued" || legalStatus == "effective"
	switch {
	case sourceKind == "official" && len(m.Watched) > 0:
		return "warning"
	case sourceKind == "official":
		return "notice"
	case len(m.Watched) > 0:
		return "notice"
	case hasAI && relevance == "high" && status:
		return "notice"
	case !hasAI && status:
		return "notice"
	}
	return "info"
}

// InWindow reports whether the clock time of now lies in [start, end), both
// "HH:MM". A window whose end is not after its start wraps past midnight; an
// empty or equal pair means "always".
func InWindow(now time.Time, start, end string) bool {
	s, ok1 := clock(start)
	e, ok2 := clock(end)
	if !ok1 || !ok2 || s == e {
		return true
	}
	cur := now.Hour()*60 + now.Minute()
	if s < e {
		return cur >= s && cur < e
	}
	return cur >= s || cur < e
}

func clock(hhmm string) (int, bool) {
	t, err := time.Parse("15:04", strings.TrimSpace(hhmm))
	if err != nil {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}

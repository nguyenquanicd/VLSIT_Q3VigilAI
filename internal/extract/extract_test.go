package extract

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var law = `CHÍNH PHỦ
NGHỊ ĐỊNH
Quy định về hóa đơn, chứng từ và các nội dung liên quan đến việc quản lý thuế của cơ quan nhà nước có thẩm quyền.

Chương I
QUY ĐỊNH CHUNG

Điều 1. Phạm vi điều chỉnh
Nghị định này quy định việc quản lý, sử dụng hóa đơn khi bán hàng hóa, cung cấp dịch vụ.

Điều 2. Đối tượng áp dụng
1. Tổ chức, cá nhân bán hàng hóa, cung cấp dịch vụ.
2. Cơ quan thuế các cấp.

Chương II
HÓA ĐƠN ĐIỆN TỬ

Điều 3. Nguyên tắc lập hóa đơn
1. Khi bán hàng hóa, người bán phải lập hóa đơn để giao cho người mua. ` + longText + `
2. Hóa đơn được lập theo thứ tự liên tục từ số nhỏ đến số lớn. ` + longText + `
3. Người bán chịu trách nhiệm về tính chính xác của hóa đơn. ` + longText + `

Điều 4. Hiệu lực thi hành
Nghị định này có hiệu lực thi hành từ ngày 01 tháng 01 năm 2027.
`

var longText = strings.Repeat("Nội dung chi tiết của khoản này được quy định cụ thể như sau đây. ", 14)

func TestLegalChunks(t *testing.T) {
	chunks := LegalChunks(law)
	paths := []string{}
	for _, c := range chunks {
		paths = append(paths, c.Path)
		if len([]rune(c.Text)) > maxChunk+50 {
			t.Errorf("oversized chunk %s: %d runes", c.Path, len([]rune(c.Text)))
		}
	}
	want := []string{"Phần mở đầu", "Chương I > Điều 1", "Chương I > Điều 2",
		"Chương II > Điều 3 > Khoản 1", "Chương II > Điều 3 > Khoản 2", "Chương II > Điều 3 > Khoản 3", "Chương II > Điều 4"}
	if strings.Join(paths, "|") != strings.Join(want, "|") {
		t.Fatalf("paths:\n got %v\nwant %v", paths, want)
	}
	// The chapter heading must not leak into the previous article.
	if strings.Contains(chunks[2].Text, "Chương II") || strings.Contains(chunks[2].Text, "HÓA ĐƠN ĐIỆN TỬ") {
		t.Errorf("chapter heading leaked into Điều 2: %q", chunks[2].Text)
	}
	// Each clause repeats the article heading so it can be read alone.
	if !strings.HasPrefix(chunks[4].Text, "Điều 3. Nguyên tắc lập hóa đơn") || !strings.Contains(chunks[4].Text, "2. Hóa đơn được lập") {
		t.Errorf("clause chunk: %q", chunks[4].Text[:120])
	}
	if !strings.Contains(chunks[6].Text, "01 tháng 01 năm 2027") {
		t.Errorf("last article: %q", chunks[6].Text)
	}
}

func TestParagraphChunks(t *testing.T) {
	text := strings.Repeat("Đoạn văn của một bài báo về chính sách thuế mới. ", 8)
	article := strings.Join([]string{text, text, text, text, text}, "\n")
	chunks := ParagraphChunks(article, "")
	if len(chunks) < 2 {
		t.Fatalf("expected several chunks, got %d", len(chunks))
	}
	total := 0
	for _, c := range chunks {
		total += len([]rune(c.Text))
		if len([]rune(c.Text)) > maxChunk {
			t.Errorf("oversized: %d", len([]rune(c.Text)))
		}
	}
	if total < len([]rune(article))-20 {
		t.Errorf("text lost: %d of %d runes", total, len([]rune(article)))
	}
	huge := strings.Repeat("từ ", 3000)
	for _, c := range ParagraphChunks(huge, "x") {
		if len([]rune(c.Text)) > maxChunk {
			t.Errorf("oversized single paragraph chunk: %d", len([]rune(c.Text)))
		}
	}
	if got := LegalChunks("Văn bản không có điều khoản nào.\nChỉ là một thông báo ngắn."); len(got) != 1 {
		t.Errorf("text without articles: %d chunks", len(got))
	}
}

func TestLooksVietnamese(t *testing.T) {
	if !LooksVietnamese(law) {
		t.Error("real legal text rejected")
	}
	if LooksVietnamese("ngắn") {
		t.Error("short text accepted")
	}
	if LooksVietnamese(strings.Repeat("Ð¿Ñ\u0080Ð¸ ¦§¨ 1234 %%%% ", 40)) {
		t.Error("garbled text accepted")
	}
	if LooksVietnamese(strings.Repeat("The quick brown fox jumps over the lazy dog. ", 20)) {
		t.Error("non-Vietnamese text accepted")
	}
}

func TestPDFTextOnBadFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.pdf")
	os.WriteFile(p, []byte("%PDF-1.4 this is not really a pdf"), 0o644)
	if text, ok := PDFText(p); ok || text != "" {
		t.Errorf("bad file: %q %v", text, ok)
	}
	if _, ok := PDFText(filepath.Join(t.TempDir(), "missing.pdf")); ok {
		t.Error("missing file")
	}
}

// A real resolution downloaded from the Government portal (public document).
// It guards the reading order, which one extraction method got wrong.
func TestPDFTextOnRealPortalFile(t *testing.T) {
	text, ok := PDFText(filepath.Join("testdata", "nq43_2026.pdf"))
	if !ok {
		t.Fatal("no text layer found in a PDF that has one")
	}
	for _, want := range []string{
		"Số: 43/2026/NQ-CP",
		"Căn cứ Luật Tổ chức Chính phủ số 63/2025/QH15;",
		"Căn cứ Nghị quyết số 25/2026/NQ-CP của Chính phủ về việc kéo dài thời hạn áp dụng của Nghị định số 72/2026/NĐ-CP;",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing or scrambled: %q", want)
		}
	}
	if strings.ContainsRune(text, '�') {
		t.Error("replacement characters left in the text")
	}
	chunks := LegalChunks(text)
	var arts []string
	for _, c := range chunks {
		arts = append(arts, c.Path)
	}
	joined := strings.Join(arts, "|")
	if !strings.Contains(joined, "Điều 1") || !strings.Contains(joined, "Điều 2") {
		t.Errorf("articles not recognised: %v", arts)
	}
	t.Logf("%d runes, %d chunks: %v", len([]rune(text)), len(chunks), arts)
}

// minimalPDF builds a valid PDF whose pages are empty except where text is given.
func minimalPDF(pages []string) []byte {
	var objs []string
	kids := make([]string, len(pages))
	for i := range pages {
		kids[i] = fmt.Sprintf("%d 0 R", 4+2*i)
	}
	objs = append(objs,
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(pages)),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	for i, text := range pages {
		content := ""
		if text != "" {
			content = "BT /F1 11 Tf 40 700 Td (" + text + ") Tj ET"
		}
		objs = append(objs,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", 5+2*i),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, x)
	return b.Bytes()
}

func TestPDFTextProbesFirstPagesToSpotScans(t *testing.T) {
	prose := strings.Repeat("Quy dinh cua cac co quan duoc ap dung theo cac dieu khoan va trong pham vi nghi dinh nay. ", 4)
	dir := t.TempDir()
	write := func(name string, pages []string) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, minimalPDF(pages), 0o644)
		return p
	}
	// Text from the first page: read normally.
	if text, ok := PDFText(write("text.pdf", []string{prose, prose, prose, prose})); !ok || !strings.Contains(text, "nghi dinh nay") {
		t.Fatalf("a PDF with text on page one was not read: ok=%v %q", ok, text)
	}
	// Three blank pages: a scan. It is given up on without reading further, even
	// though page four has text (the price of not reading all of a 30 MB scan).
	if text, ok := PDFText(write("scan.pdf", []string{"", "", "", prose})); ok || text != "" {
		t.Errorf("a scan was not recognised from its first pages: ok=%v %q", ok, text)
	}
	// Text that starts on page two or three still counts.
	if _, ok := PDFText(write("late.pdf", []string{"", prose, prose, prose})); !ok {
		t.Error("text on the second page was missed")
	}
}

package textutil

import (
	"reflect"
	"strings"
	"testing"
)

func TestFold(t *testing.T) {
	cases := map[string]string{
		"Nghị định  số 13/2023/NĐ-CP": "nghi dinh so 13/2023/nd-cp",
		"ĐẤT ĐAI":                     "dat dai",
		"Thuế giá trị gia tăng":       "thue gia tri gia tang",
		// decomposed input (NFD) must fold the same way
		"thuế": "thue",
		"":      "",
	}
	for in, want := range cases {
		if got := Fold(in); got != want {
			t.Errorf("Fold(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKeywordMatching(t *testing.T) {
	text := Prepare("Chính phủ ban hành Nghị định mới về thuế giá trị gia tăng; quy định thuê đất không đổi.")
	cases := []struct {
		kw   string
		want bool
	}{
		{"thuế giá trị gia tăng", true},
		{"Thuế Giá Trị Gia Tăng", true},
		{"thue gia tri gia tang", true}, // typed without diacritics
		{"thuê đất", true},
		{"thuế đất", false}, // tone matters when the user typed diacritics
		{"gia", true},
		{"gi", false}, // not a whole word
		{"nghị định", true},
		{"bảo hiểm xã hội", false},
		{"", false},
	}
	for _, c := range cases {
		if got := text.Has(c.kw); got != c.want {
			t.Errorf("Has(%q) = %v, want %v", c.kw, got, c.want)
		}
	}
	got := text.Matches([]string{"thuế", "lao động", "đất"})
	if !reflect.DeepEqual(got, []string{"thuế", "đất"}) {
		t.Errorf("Matches = %v", got)
	}
}

func TestExtractDocNumbers(t *testing.T) {
	in := "Theo Nghị định số 13/2023/NĐ-CP ngày 17/4/2023 và Luật 59/2020/QH14, " +
		"Thông tư 80/2021/TT-BTC; Chỉ thị 05/CT-TTg, công văn 1234/BTC-TCT. " +
		"Nhắc lại 13/2023/NĐ-CP. Ngày 4/10/2026, tỉ lệ 24/7, phòng 1/A, Thông tư 01/2024/TT-BKHĐT."
	want := []string{"13/2023/NĐ-CP", "59/2020/QH14", "80/2021/TT-BTC", "05/CT-TTg", "1234/BTC-TCT", "01/2024/TT-BKHĐT"}
	if got := ExtractDocNumbers(in); !reflect.DeepEqual(got, want) {
		t.Errorf("ExtractDocNumbers = %v\nwant %v", got, want)
	}
	if got := ExtractDocNumbers("Nghị định 381/2026/NĐ-CP quy định"); len(got) != 1 || got[0] != "381/2026/NĐ-CP" {
		t.Errorf("start-of-token case: %v", got)
	}
}

func TestNormalizeDocNumber(t *testing.T) {
	want := "13/2023/ND-CP"
	for _, in := range []string{"13/2023/NĐ-CP", "13/2023/nd-cp", "13/2023/NĐ – CP", " 13/2023/NĐ-СР "} {
		if got := NormalizeDocNumber(in); got != want {
			t.Errorf("NormalizeDocNumber(%q) = %q, want %q", in, got, want)
		}
	}
	if y := DocNumberYear("13/2023/NĐ-CP"); y != 2023 {
		t.Errorf("year = %d", y)
	}
	if y := DocNumberYear("05/CT-TTg"); y != 0 {
		t.Errorf("year = %d", y)
	}
}

func TestNormalizeURL(t *testing.T) {
	a, _ := NormalizeURL("https://VnExpress.net/bai-viet-123.html?utm_source=fb&b=2&a=1&fbclid=xyz#comments")
	b, _ := NormalizeURL("https://vnexpress.net/bai-viet-123.html?a=1&b=2")
	if a != b {
		t.Errorf("%q != %q", a, b)
	}
}

func TestArticleText(t *testing.T) {
	page := `<html><head><title> Tiêu đề bài </title><script>var x = "<p>bad</p>";</script></head><body>
<nav><p>Trang chủ</p><p>Pháp luật</p></nav>
<div class="wrap"><aside><p>Tin liên quan rất dài dòng không thuộc bài viết chính</p></aside>
<article><h1>Nghị định mới</h1>
<p>Đoạn một của bài viết   có nhiều   khoảng trắng.</p>
<p>Đoạn hai nói về <b>thuế</b> &amp; phí.</p>
<figure><figcaption>Chú thích ảnh</figcaption></figure>
</article></div>
<footer><p>Bản quyền thuộc về báo</p></footer></body></html>`
	got := ArticleText(page)
	want := "Đoạn một của bài viết có nhiều khoảng trắng.\nĐoạn hai nói về thuế & phí."
	if !strings.Contains(got, want) {
		t.Errorf("ArticleText = %q", got)
	}
	for _, bad := range []string{"Trang chủ", "Bản quyền", "Tin liên quan", "bad", "Chú thích"} {
		if strings.Contains(got, bad) {
			t.Errorf("ArticleText leaked %q: %q", bad, got)
		}
	}
	if PageTitle(page) != "Tiêu đề bài" {
		t.Errorf("PageTitle = %q", PageTitle(page))
	}
	if s := StripTags("<![CDATA[x]]> <b>Thời cơ v&agrave;ng</b> &#039;ok&#039;"); !strings.Contains(s, "Thời cơ vàng 'ok'") {
		t.Errorf("StripTags = %q", s)
	}
}

func TestTruncate(t *testing.T) {
	if got := Truncate("một hai ba bốn năm", 100); got != "một hai ba bốn năm" {
		t.Errorf("short: %q", got)
	}
	got := Truncate(strings.Repeat("chữ ", 100), 50)
	if len([]rune(got)) > 51 || !strings.HasSuffix(got, "…") {
		t.Errorf("long: %q", got)
	}
}

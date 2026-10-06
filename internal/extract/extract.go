// Package extract turns downloaded files and article bodies into searchable
// passages.
package extract

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/ledongthuc/pdf"

	"q3vigilai/internal/store"
	"q3vigilai/internal/textutil"
)

// A PDF whose first probePages pages hold fewer than probeRunes characters of
// text is treated as a scan.
const (
	probePages = 3
	probeRunes = 60
)

// PDFText returns the text layer of a PDF. ok is false when the file has no
// usable text layer, which is the normal case for a scanned document: those
// need OCR and are stored as files only.
func PDFText(path string) (text string, ok bool) {
	// The PDF library panics on some malformed files; a bad attachment must
	// not take the scan down.
	defer func() {
		if recover() != nil {
			text, ok = "", false
		}
	}()
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	var b strings.Builder
	pages := r.NumPage()
	for i := 1; i <= pages && i <= 600; i++ {
		b.WriteString(pageText(r, i))
		b.WriteByte('\n')
		// A scanned document has no text layer. Walking all of a 30 MB scan to
		// find that out costs more memory than the rest of a scan cycle, so
		// the first pages decide: with no text on them, stop.
		if i == probePages && len([]rune(strings.TrimSpace(b.String()))) < probeRunes {
			return "", false
		}
	}
	text = strings.ReplaceAll(b.String(), "�", "")
	return text, LooksVietnamese(text)
}

// pageText reads one page in content-stream order, which for the PDFs the
// Government portal publishes is the reading order. (The library's
// position-based extraction was tried on real portal files and scrambles
// lines: it reports zero-width glyphs at the same coordinates.)
func pageText(r *pdf.Reader, n int) (text string) {
	defer func() {
		if recover() != nil {
			text = ""
		}
	}()
	p := r.Page(n)
	if p.V.IsNull() {
		return ""
	}
	text, _ = p.GetPlainText(nil)
	return text
}

var commonWords = []string{" cua ", " va ", " cac ", " duoc ", " quy dinh ", " theo ", " trong "}

// LooksVietnamese rejects text layers that are empty or garbled (wrong font
// encoding yields letters that are not words).
func LooksVietnamese(text string) bool {
	if len([]rune(text)) < 200 {
		return false
	}
	letters, total := 0, 0
	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		total++
		if unicode.IsLetter(r) {
			letters++
		}
	}
	if total == 0 || float64(letters)/float64(total) < 0.6 {
		return false
	}
	folded := " " + textutil.Fold(text) + " "
	hits := 0
	for _, w := range commonWords {
		if strings.Contains(folded, w) {
			hits++
		}
	}
	return hits >= 4
}

var (
	reArticle = regexp.MustCompile(`(?m)^[ \t]*Điều[ \t]+(\d+[a-zđ]?)[.:]`)
	reChapter = regexp.MustCompile(`(?m)^[ \t]*Chương[ \t]+([IVXLC]+|\d+)\b[^\n]*`)
	reClause  = regexp.MustCompile(`(?m)^[ \t]*(\d{1,2})\.[ \t]+\S`)
)

const maxChunk = 1800

// LegalChunks splits the text of a legal document along its own structure:
// one passage per Điều, and per Khoản when an article is long. Each passage
// carries its path so an answer can cite "Điều 12 > Khoản 3".
func LegalChunks(text string) []store.Chunk {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	arts := reArticle.FindAllStringSubmatchIndex(text, -1)
	if len(arts) == 0 {
		return ParagraphChunks(text, "")
	}
	var out []store.Chunk
	if pre := strings.TrimSpace(text[:arts[0][0]]); len([]rune(pre)) > 80 {
		out = append(out, ParagraphChunks(pre, "Phần mở đầu")...)
	}
	chapters := reChapter.FindAllStringSubmatchIndex(text, -1)
	chapterAt := func(pos int) string {
		name := ""
		for _, c := range chapters {
			if c[0] > pos {
				break
			}
			name = "Chương " + text[c[2]:c[3]]
		}
		return name
	}
	for i, a := range arts {
		end := len(text)
		if i+1 < len(arts) {
			end = arts[i+1][0]
		}
		body := strings.TrimSpace(text[a[0]:end])
		// A chapter heading sitting at the end belongs to the next article.
		if c := reChapter.FindStringIndex(body); c != nil && c[0] > 0 {
			body = strings.TrimSpace(body[:c[0]])
		}
		path := "Điều " + text[a[2]:a[3]]
		if ch := chapterAt(a[0]); ch != "" {
			path = ch + " > " + path
		}
		if len([]rune(body)) <= maxChunk {
			out = append(out, store.Chunk{Path: path, Text: body})
			continue
		}
		clauses := reClause.FindAllStringSubmatchIndex(body, -1)
		if len(clauses) < 2 {
			out = append(out, ParagraphChunks(body, path)...)
			continue
		}
		// The heading line of the article is repeated in every clause so each
		// passage is understandable on its own.
		head := strings.TrimSpace(body[:clauses[0][0]])
		for j, c := range clauses {
			cend := len(body)
			if j+1 < len(clauses) {
				cend = clauses[j+1][0]
			}
			cl := strings.TrimSpace(body[c[0]:cend])
			sub := fmt.Sprintf("%s > Khoản %s", path, body[c[2]:c[3]])
			out = append(out, ParagraphChunks(textutil.Truncate(head, 200)+"\n"+cl, sub)...)
		}
	}
	return out
}

// ParagraphChunks packs paragraphs into passages of readable size.
func ParagraphChunks(text, path string) []store.Chunk {
	var out []store.Chunk
	var cur bytes.Buffer
	n := 0
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, store.Chunk{Path: path, Text: s})
		}
		cur.Reset()
		n = 0
	}
	for _, para := range strings.Split(text, "\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		runes := []rune(para)
		// A single oversized paragraph is cut on sentence-ish boundaries.
		for len(runes) > maxChunk {
			cut := maxChunk
			for k := maxChunk; k > maxChunk-300; k-- {
				if runes[k] == '.' || runes[k] == ';' || runes[k] == ' ' {
					cut = k + 1
					break
				}
			}
			flush()
			out = append(out, store.Chunk{Path: path, Text: strings.TrimSpace(string(runes[:cut]))})
			runes = runes[cut:]
		}
		if n+len(runes) > maxChunk-600 && n > 0 {
			flush()
		}
		cur.WriteString(string(runes))
		cur.WriteByte('\n')
		n += len(runes)
	}
	flush()
	return out
}

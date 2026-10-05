// Package textutil holds the Vietnamese text helpers shared by the pipeline:
// diacritic folding, keyword matching, document-number and URL normalization.
package textutil

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Fold lowercases s, strips Vietnamese diacritics (đ becomes d) and collapses
// whitespace. Folding loses tone marks, so "thuế" and "thuê" fold to the same
// string; use MatchKeyword when that distinction matters.
func Fold(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range norm.NFD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
			continue
		case r == 'đ' || r == 'Đ':
			r = 'd'
		case unicode.IsSpace(r):
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// Lower returns s in NFC, lowercased, with whitespace collapsed. Diacritics
// are kept.
func Lower(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(norm.NFC.String(s))), " ")
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// containsWord reports whether needle occurs in hay on word boundaries.
func containsWord(hay, needle string) bool {
	if needle == "" {
		return false
	}
	from := 0
	for {
		i := strings.Index(hay[from:], needle)
		if i < 0 {
			return false
		}
		start := from + i
		end := start + len(needle)
		okBefore, okAfter := true, true
		if start > 0 {
			r, _ := lastRune(hay[:start])
			okBefore = !isWordRune(r)
		}
		if end < len(hay) {
			r := []rune(hay[end:min(end+4, len(hay))])[0]
			okAfter = !isWordRune(r)
		}
		if okBefore && okAfter {
			return true
		}
		from = start + 1
		for from < len(hay) && !isRuneStart(hay[from]) {
			from++
		}
		if from >= len(hay) {
			return false
		}
	}
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

func lastRune(s string) (rune, int) {
	i := len(s) - 1
	for i > 0 && !isRuneStart(s[i]) {
		i--
	}
	r := []rune(s[i:])
	return r[0], len(s) - i
}

// Text is a document prepared for repeated keyword matching.
type Text struct {
	lower  string
	folded string
}

// Prepare builds the matching forms of s once.
func Prepare(s string) Text {
	l := Lower(s)
	return Text{lower: l, folded: Fold(l)}
}

// Has reports whether keyword occurs in the text as a whole word or phrase.
// A keyword typed with diacritics must match with diacritics ("thuế" does not
// match "thuê"); a keyword typed without them matches any accented form.
func (t Text) Has(keyword string) bool {
	k := Lower(keyword)
	if k == "" {
		return false
	}
	if isASCII(k) {
		return containsWord(t.folded, k)
	}
	return containsWord(t.lower, k)
}

// Matches returns the keywords found in the text, in input order.
func (t Text) Matches(keywords []string) []string {
	var out []string
	for _, k := range keywords {
		if t.Has(k) {
			out = append(out, k)
		}
	}
	return out
}

// Vietnamese legal document numbers: "13/2023/NĐ-CP", "59/2020/QH14",
// "80/2021/TT-BTC", and the two-part form "05/CT-TTg", "1234/BTC-TCT".
// The part after the last slash must start with a letter, which keeps dates
// such as 4/10/2026 out.
var docNumberRe = regexp.MustCompile(`(?:^|[^\p{L}\p{N}/])(\d{1,5}(?:/\d{4})?/[A-ZĐ][A-ZĐa-z0-9]*(?:[-–][A-ZĐ][A-ZĐa-z0-9]*)*)`)

// NormalizeDocNumber folds the hand-typed variations seen on portals and in
// the press: case, spaces, Đ/D, dashes and Cyrillic look-alike letters.
func NormalizeDocNumber(code string) string {
	c := strings.ToUpper(norm.NFC.String(code))
	c = strings.NewReplacer(
		"Đ", "D",
		"С", "C", "Р", "P", "Н", "H", "Т", "T",
		"–", "-", "—", "-",
	).Replace(c)
	return strings.Join(strings.Fields(c), "")
}

// DocNumberYear returns the year embedded in a three-part number, or 0.
func DocNumberYear(code string) int {
	parts := strings.Split(code, "/")
	if len(parts) == 3 {
		if y, err := strconv.Atoi(parts[1]); err == nil {
			return y
		}
	}
	return 0
}

// ExtractDocNumbers returns the distinct document numbers in s, in order of
// first appearance, as written in the text.
func ExtractDocNumbers(s string) []string {
	s = norm.NFC.String(s)
	var out []string
	seen := map[string]bool{}
	for _, m := range docNumberRe.FindAllStringSubmatch(s, -1) {
		code := strings.ReplaceAll(m[1], "–", "-")
		// A lone uppercase letter after the slash ("1/A") is not a document number.
		tail := code[strings.LastIndex(code, "/")+1:]
		if len([]rune(tail)) < 2 {
			continue
		}
		n := NormalizeDocNumber(code)
		if !seen[n] {
			seen[n] = true
			out = append(out, code)
		}
	}
	return out
}

var trackingParams = map[string]bool{"fbclid": true, "gclid": true, "zarsrc": true, "vn_source": true, "vn_campaign": true, "vn_medium": true, "vn_term": true, "vn_thumb": true}

// NormalizeURL makes two links to the same article compare equal: lowercase
// scheme and host, no fragment, no tracking parameters, sorted query.
func NormalizeURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	q := u.Query()
	for k := range q {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "utm_") || trackingParams[lk] {
			q.Del(k)
		}
	}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		for _, v := range q[k] {
			parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}
	u.RawQuery = strings.Join(parts, "&")
	return u.String(), nil
}

// Hash returns a short stable content hash.
func Hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:16])
}

// Truncate cuts s to at most n runes, on a word boundary when possible.
func Truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	cut := n
	for i := n; i > n-40 && i > 0; i-- {
		if unicode.IsSpace(r[i]) {
			cut = i
			break
		}
	}
	return strings.TrimSpace(string(r[:cut])) + "…"
}

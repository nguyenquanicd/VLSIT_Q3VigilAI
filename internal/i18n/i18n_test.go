package i18n

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestTranslateAndFallBack(t *testing.T) {
	old := en
	defer func() { en = old; SetLang(Vi) }()
	en = map[string]string{"Quét xong: {0} tin mới, {1} cảnh báo.": "Scan done: {0} new items, {1} alerts.", "Thoát": "Quit"}

	SetLang("vi")
	if got := T("Quét xong: {0} tin mới, {1} cảnh báo.", 3, 2); got != "Quét xong: 3 tin mới, 2 cảnh báo." {
		t.Errorf("vi: %q", got)
	}
	SetLang("en")
	if got := T("Quét xong: {0} tin mới, {1} cảnh báo.", 3, 2); got != "Scan done: 3 new items, 2 alerts." {
		t.Errorf("en: %q", got)
	}
	if got := T("Không có bản dịch"); got != "Không có bản dịch" {
		t.Errorf("a message without a translation must be shown as written: %q", got)
	}
	if got := In(Vi, "Thoát"); got != "Thoát" {
		t.Errorf("explicit language: %q", got)
	}
	if Normalize("en-US") != En || Normalize("EN") != En || Normalize("vi") != Vi || Normalize("fr") != Vi || Normalize("") != Vi {
		t.Error("Normalize")
	}
	if N("giữ nguyên") != "giữ nguyên" {
		t.Error("N must not translate")
	}
}

func TestErrFollowsTheLanguageAndKeepsIdentity(t *testing.T) {
	old := en
	defer func() { en = old; SetLang(Vi) }()
	en = map[string]string{"Hết giờ": "Timed out"}
	sentinel := Err("Hết giờ")
	wrapped := fmt.Errorf("lượt quét: %w", sentinel)
	if !errors.Is(wrapped, sentinel) {
		t.Error("errors.Is lost the sentinel")
	}
	SetLang("vi")
	if sentinel.Error() != "Hết giờ" {
		t.Error(sentinel.Error())
	}
	SetLang("en")
	if sentinel.Error() != "Timed out" {
		t.Error(sentinel.Error())
	}
}

// ---- completeness: every message in the code has an English translation ----

var (
	goKeyRe = []*regexp.Regexp{
		regexp.MustCompile("i18n\\.(?:T|N|Err)\\(\\s*(\"(?:[^\"\\\\]|\\\\.)*\"|`[^`]*`)"),
		regexp.MustCompile("i18n\\.In\\([^,()]+,\\s*(\"(?:[^\"\\\\]|\\\\.)*\")"),
		// server helpers that take a message as their last literal: bad("code", "msg") and apiErr(status, "code", "msg")
		regexp.MustCompile("\\bbad\\(\\s*\"[a-z_]+\"\\s*,\\s*(\"(?:[^\"\\\\]|\\\\.)*\")"),
		regexp.MustCompile("\\bapiErr\\([^,]+,\\s*\"[a-z_]+\"\\s*,\\s*(\"(?:[^\"\\\\]|\\\\.)*\")"),
	}
	jsKeyRe = regexp.MustCompile(`\b(?:t|N)\(\s*('(?:[^'\\]|\\.)*'|"(?:[^"\\]|\\.)*")`)
	placeRe = regexp.MustCompile(`\{\d+\}`)
)

func jsUnquote(lit string) string {
	body := lit[1 : len(lit)-1]
	r := strings.NewReplacer(`\'`, `'`, `\"`, `"`, `\\`, `\`, `\n`, "\n")
	return r.Replace(body)
}

// sourceKeys returns every message literal found in the Go and JS sources,
// with the files it was found in.
func sourceKeys(t *testing.T) map[string][]string {
	t.Helper()
	root := filepath.Join("..", "..")
	keys := map[string][]string{}
	add := func(k, file string) {
		if k != "" {
			keys[k] = append(keys[k], file)
		}
	}
	walk := func(dir, ext string, each func(path string, src string)) {
		filepath.Walk(filepath.Join(root, dir), func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(p, ext) || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			b, _ := os.ReadFile(p)
			each(p, string(b))
			return nil
		})
	}
	for _, dir := range []string{"cmd", "internal"} {
		walk(dir, ".go", func(p, src string) {
			for _, re := range goKeyRe {
				for _, m := range re.FindAllStringSubmatch(src, -1) {
					k, err := strconv.Unquote(m[1])
					if err != nil {
						t.Errorf("%s: cannot read the literal %s", p, m[1])
						continue
					}
					add(k, p)
				}
			}
		})
	}
	walk("web/js", ".js", func(p, src string) {
		for _, m := range jsKeyRe.FindAllStringSubmatch(src, -1) {
			add(jsUnquote(m[1]), p)
		}
	})
	return keys
}

func TestEveryMessageInTheCodeIsTranslated(t *testing.T) {
	keys := sourceKeys(t)
	if len(keys) < 50 {
		t.Fatalf("found only %d messages in the sources: the extraction patterns have stopped matching", len(keys))
	}
	var missing []string
	for k, files := range keys {
		// A message with no letters (a bare format such as "{0} / {1}") has nothing to translate.
		if !strings.ContainsFunc(k, func(r rune) bool { return r > 127 }) && !strings.ContainsAny(k, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") {
			continue
		}
		if !Has(k) {
			missing = append(missing, fmt.Sprintf("%q  (%s)", k, strings.TrimPrefix(filepath.ToSlash(files[0]), "../../")))
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		show := missing
		if len(show) > 40 {
			show = show[:40]
		}
		t.Errorf("%d messages have no English translation in en.json, for example:\n  %s", len(missing), strings.Join(show, "\n  "))
	}
}

func TestDictionaryIsConsistent(t *testing.T) {
	var raw map[string]string
	if err := json.Unmarshal(enRaw, &raw); err != nil {
		t.Fatal(err)
	}
	for k, v := range raw {
		if strings.TrimSpace(v) == "" {
			t.Errorf("empty translation for %q", k)
			continue
		}
		a, b := placeRe.FindAllString(k, -1), placeRe.FindAllString(v, -1)
		sort.Strings(a)
		sort.Strings(b)
		if strings.Join(a, ",") != strings.Join(b, ",") {
			t.Errorf("placeholders differ: %q -> %q", k, v)
		}
		if k == v && strings.ContainsFunc(k, func(r rune) bool { return r > 0x24F }) {
			t.Errorf("not translated: %q", k)
		}
	}
}

// DumpKeys writes the message list to the file named by Q3_DUMP_KEYS, in the
// shape of en.json with empty values: a starting point for new translations.
func TestDumpKeys(t *testing.T) {
	out := os.Getenv("Q3_DUMP_KEYS")
	if out == "" {
		t.Skip("set Q3_DUMP_KEYS=<file> to list the messages that need translating")
	}
	keys := sourceKeys(t)
	list := make([]string, 0, len(keys))
	for k := range keys {
		if !Has(k) {
			list = append(list, k)
		}
	}
	sort.Strings(list)
	var b strings.Builder
	b.WriteString("{\n")
	for i, k := range list {
		q, _ := json.Marshal(k)
		fmt.Fprintf(&b, "  %s: \"\"", q)
		if i < len(list)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d messages written to %s", len(list), out)
}

package web

import (
	"fmt"
	"io/fs"
	"strings"
	"testing"
)

// The UI has no build step and no JavaScript toolchain, so nothing else would
// notice a missing bracket before the window opens blank. This is a cheap
// guard: it walks each script, skipping strings, template literals and
// comments, and checks that every bracket closes and in the right order.
func TestScriptsHaveBalancedBrackets(t *testing.T) {
	n := 0
	err := fs.WalkDir(FS, "js", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".js") {
			return err
		}
		n++
		src, err := fs.ReadFile(FS, path)
		if err != nil {
			return err
		}
		if msg := unbalanced(string(src)); msg != "" {
			t.Errorf("%s: %s", path, msg)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n < 10 {
		t.Fatalf("only %d scripts found", n)
	}
}

func TestUnbalancedDetectsAMissingBracket(t *testing.T) {
	for _, src := range []string{"f(a, g(b)", "x = [1, 2", "if (a) { b()", "f(a))", "f(a]", "s = 'a(b'; g(", "`${f(}`"} {
		if unbalanced(src) == "" {
			t.Errorf("not detected: %s", src)
		}
	}
	for _, src := range []string{"f(a, g(b))", "s = ')' + \"(\" + '\\''", "// ( \n x = 1", "/* [ */ y = {}", "`a ${f(1)} (`", "h('p', null, `x`)"} {
		if msg := unbalanced(src); msg != "" {
			t.Errorf("false alarm on %q: %s", src, msg)
		}
	}
}

// unbalanced returns a description of the first bracket problem, or "".
func unbalanced(src string) string {
	type open struct {
		c    byte
		line int
	}
	var stack []open
	line := 1
	closer := map[byte]byte{')': '(', ']': '[', '}': '{'}
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case c == '\n':
			line++
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			line++
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			for i += 2; i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/'); i++ {
				if src[i] == '\n' {
					line++
				}
			}
			i++
		case c == '\'' || c == '"':
			for i++; i < len(src) && src[i] != c; i++ {
				if src[i] == '\\' {
					i++
				} else if src[i] == '\n' {
					return fmt.Sprintf("line %d: a string is not closed", line)
				}
			}
		case c == '`':
			// A template literal: text, with ${ … } holding code, which is scanned too.
			depth := 0
			for i++; i < len(src); i++ {
				if src[i] == '\n' {
					line++
				}
				if src[i] == '\\' {
					i++
					continue
				}
				if depth == 0 && src[i] == '`' {
					break
				}
				if src[i] == '$' && i+1 < len(src) && src[i+1] == '{' {
					stack = append(stack, open{'{', line})
					depth++
					i++
				} else if depth > 0 && src[i] == '{' {
					stack = append(stack, open{'{', line})
					depth++
				} else if depth > 0 && src[i] == '}' {
					stack = stack[:len(stack)-1]
					depth--
				} else if depth > 0 && (src[i] == '(' || src[i] == '[') {
					stack = append(stack, open{src[i], line})
				} else if depth > 0 && (src[i] == ')' || src[i] == ']') {
					if len(stack) == 0 || stack[len(stack)-1].c != closer[src[i]] {
						return fmt.Sprintf("line %d: unexpected %c inside a template", line, src[i])
					}
					stack = stack[:len(stack)-1]
				}
			}
		case c == '(' || c == '[' || c == '{':
			stack = append(stack, open{c, line})
		case c == ')' || c == ']' || c == '}':
			if len(stack) == 0 {
				return fmt.Sprintf("line %d: %c closes nothing", line, c)
			}
			top := stack[len(stack)-1]
			if top.c != closer[c] {
				return fmt.Sprintf("line %d: %c does not match %c opened on line %d", line, c, top.c, top.line)
			}
			stack = stack[:len(stack)-1]
		}
	}
	if len(stack) > 0 {
		top := stack[len(stack)-1]
		return fmt.Sprintf("%c opened on line %d is never closed", top.c, top.line)
	}
	return ""
}

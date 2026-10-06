// Package i18n translates the text the app shows. The source language is
// Vietnamese: a Vietnamese message is its own key, and en.json maps each key
// to its English text. A message with no entry is shown as written, so a
// missing translation is a Vietnamese sentence, never a blank.
//
// The same dictionary serves the Go code (tray menu, notifications, API
// errors, alert texts) and the web UI, which fetches it from /api/i18n.
// Placeholders are {0}, {1}, … and must appear in both languages.
//
// A single process-wide language is used: the app has one user and one
// window, and the code that builds a message is often far from the request.
package i18n

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
)

// The two languages.
const (
	Vi = "vi"
	En = "en"
)

//go:embed en.json
var enRaw []byte

var (
	en      = load()
	current atomic.Value // string
)

func init() { current.Store(Vi) }

func load() map[string]string {
	m := map[string]string{}
	if err := json.Unmarshal(enRaw, &m); err != nil {
		panic("i18n: en.json is not valid: " + err.Error())
	}
	return m
}

// Normalize maps any language tag to a supported language.
func Normalize(lang string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(lang)), "en") {
		return En
	}
	return Vi
}

// SetLang selects the language of every message built from now on.
func SetLang(lang string) { current.Store(Normalize(lang)) }

// Lang returns the current language.
func Lang() string { return current.Load().(string) }

// T returns msg in the current language with its placeholders filled in.
func T(msg string, args ...any) string { return In(Lang(), msg, args...) }

// In returns msg in the given language.
func In(lang, msg string, args ...any) string {
	out := msg
	if lang == En {
		if tr, ok := en[msg]; ok && tr != "" {
			out = tr
		}
	}
	for i, a := range args {
		out = strings.ReplaceAll(out, "{"+strconv.Itoa(i)+"}", fmt.Sprint(a))
	}
	return out
}

// TC translates a message that needs a context to be told apart from another
// with the same Vietnamese text: the dictionary holds it as "context|message",
// and falls back to the plain message. "Cảnh báo" is both the name of the
// alerts list and the name of the highest alert level.
func TC(context, msg string, args ...any) string {
	if Lang() == En {
		if tr, ok := en[context+"|"+msg]; ok && tr != "" {
			return In(En, context+"|"+msg, args...)
		}
	}
	return In(Lang(), msg, args...)
}

// N marks a message for translation without translating it where it stands:
// a table of labels is declared once and translated each time it is read.
func N(msg string) string { return msg }

// Dict returns the English translations for the web UI.
func Dict() map[string]string {
	out := make(map[string]string, len(en))
	for k, v := range en {
		out[k] = v
	}
	return out
}

// Has reports whether msg has an English translation.
func Has(msg string) bool { return en[msg] != "" }

// msgError is an error whose text follows the language at the moment it is
// read, which for a package-level sentinel is long after it was created.
type msgError struct{ key string }

func (e *msgError) Error() string { return T(e.key) }

// Err returns an error that compares by identity (so errors.Is works) and is
// worded in the current language each time it is printed.
func Err(msg string) error { return &msgError{key: msg} }

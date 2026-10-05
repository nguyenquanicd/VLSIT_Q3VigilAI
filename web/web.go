// Package web carries the user interface inside the executable.
package web

import "embed"

// FS holds the static files of the UI. There is no build step: what is in
// this folder is what the app serves.
//
//go:embed index.html css js
var FS embed.FS

// Package web embeds the single-file web UI.
package web

import "embed"

// Files contains the static web assets served by the application.
//
//go:embed index.html
var Files embed.FS

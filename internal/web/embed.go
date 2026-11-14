// Package web embeds the single-page Vue web UI and its dependencies.
package web

import "embed"

// Files contains the static web assets served by the application.
//
//go:embed index.html
//go:embed vue.global.prod.js
var Files embed.FS

// Package web holds the embedded WebRTC dev client served by `unmute dev`.
// The client is vanilla HTML/CSS/JS (no build step); it POSTs WebRTC offers to
// /api/offer, which `unmute dev` reverse-proxies to the local Pipecat runner.
package web

import "embed"

// FS contains index.html and logo.png, served at the dev server root.
//
//go:embed index.html logo.png
var FS embed.FS

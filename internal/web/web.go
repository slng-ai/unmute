// Package web holds the one embedded dev client served by `unmute dev`. It is
// vanilla HTML/CSS/JS (no build step): index.html fetches GET /api/session to
// learn its transport, then either POSTs a WebRTC offer to /api/offer
// (reverse-proxied to the containerized pipecat bot) or joins a LiveKit room
// with the vendored livekit-client UMD build. One page, one transport adapter.
package web

import "embed"

// FS holds the dev client, its logo, and the vendored livekit-client build.
//
//go:embed index.html logo.svg livekit-client.umd.js
var FS embed.FS

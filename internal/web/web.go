// Package web holds the embedded dev clients served by `unmute dev`. Both are
// vanilla HTML/CSS/JS (no build step): the pipecat client (index.html) POSTs
// WebRTC offers to /api/offer, reverse-proxied to the local Pipecat runner; the
// livekit client (livekit.html + the vendored livekit-client UMD) fetches a
// token from /api/token and joins a LiveKit room directly.
package web

import "embed"

// FS holds both dev clients and the vendored livekit-client build.
//
//go:embed index.html logo.png livekit.html livekit-client.umd.js
var FS embed.FS

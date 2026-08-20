// Package devmetrics owns the measurement line a generated agent prints and the
// dev server reads. One end of that line is this Go struct and the other is
// generated Python, so nothing can derive it from a single source: it is a
// written contract with an agreement test, described in
// specs/016-dev-ui-metrics-logs/contracts/metric-record.md.
//
// When a producer and this struct disagree, the producer is wrong.
package devmetrics

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Sentinel marks a measurement line on the agent's stdout. It is searched for
// with [bytes.Index] rather than tested as a prefix, because a container runtime
// prefixes every line it relays ("agent-1  | UNMUTE_METRIC {...}"), so an
// anchored test would pass on one target and fail on the other.
const Sentinel = "UNMUTE_METRIC "

// Env gates the producers. Both target templates read this name and the dev loop
// sets it, in three files that never import each other, so it is declared once
// here and referenced rather than spelled again.
const Env = "UNMUTE_DEV_METRICS"

// Record kinds.
const (
	KindTurn    = "turn"
	KindSession = "session"
)

// Record is one measurement line. Every timing is a pointer because a target
// reports what it reports and stays silent about the rest: absent has to stay
// absent, since a missing measurement rendered as zero reads as a fast one.
// Units are seconds, on both targets, everywhere.
type Record struct {
	Kind string `json:"kind"`
	Seq  int    `json:"seq,omitempty"`

	// kind: turn
	E2E      *float64 `json:"e2e,omitempty"`
	UserTurn *float64 `json:"user_turn,omitempty"`
	// Transcription is livekit only, TextAggregation is pipecat only. Each target
	// reports what it reports; a field neither sets does not belong here at all.
	Transcription   *float64 `json:"transcription,omitempty"`
	TextAggregation *float64 `json:"text_aggregation,omitempty"`
	Interrupted     bool     `json:"interrupted,omitempty"`
	Stages          []Stage  `json:"stages,omitempty"`
	Tools           []Tool   `json:"tools,omitempty"`

	// kind: session
	FirstSpeech *float64 `json:"first_speech,omitempty"`
}

// Stage is one service in a turn: the transcriber, the model, the voice.
type Stage struct {
	Name  string   `json:"name"`
	Kind  string   `json:"kind"` // stt | llm | tts | other
	Model string   `json:"model,omitempty"`
	TTFB  *float64 `json:"ttfb,omitempty"`
	Total *float64 `json:"total,omitempty"`
}

// Tool is one tool call the turn made, however it was reached.
type Tool struct {
	Name    string   `json:"name"`
	Seconds *float64 `json:"seconds,omitempty"`
}

// Extract returns the record carried by one output line.
//
// found reports whether the line carries the sentinel at all. A line that
// carries it but will not decode returns found true with an error, and the
// caller must surface that line as ordinary flagged output: dropping it silently
// would hide the one thing that explains why no measurements are appearing.
func Extract(line []byte) (Record, bool, error) {
	i := bytes.Index(line, []byte(Sentinel))
	if i < 0 {
		return Record{}, false, nil
	}
	payload := bytes.TrimSpace(line[i+len(Sentinel):])
	var rec Record
	if err := json.Unmarshal(payload, &rec); err != nil {
		return Record{}, true, fmt.Errorf("decoding metric payload: %w", err)
	}
	if rec.Kind != KindTurn && rec.Kind != KindSession {
		return Record{}, true, fmt.Errorf("unknown metric kind %q", rec.Kind)
	}
	return rec, true, nil
}

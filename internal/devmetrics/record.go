// Package devmetrics owns the local event records generated agents print and
// the dev server reads. Agreement and SDK smoke tests bind the Python producers
// to these Go types.
//
// When a producer and this struct disagree, the producer is wrong.
package devmetrics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"
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
	KindCall        = "call"
	KindExchange    = "exchange"
	KindText        = "text"
	KindOperation   = "operation"
	KindMeasurement = "measurement"
	KindBreakdown   = "breakdown"
	KindTurn        = "turn"
	KindSession     = "session"
)

// Record is one measurement line. Every timing is a pointer because a target
// reports what it reports and stays silent about the rest: absent has to stay
// absent, since a missing measurement rendered as zero reads as a fast one.
// Units are seconds, on both targets, everywhere.
type Record struct {
	Version     int          `json:"version,omitempty"`
	CallID      string       `json:"call_id,omitempty"`
	ID          string       `json:"id,omitempty"`
	Revision    int          `json:"revision,omitempty"`
	Order       int          `json:"order,omitempty"`
	Call        *Call        `json:"call,omitempty"`
	Exchange    *Exchange    `json:"exchange,omitempty"`
	Text        *Text        `json:"text,omitempty"`
	Operation   *Operation   `json:"operation,omitempty"`
	Measurement *Measurement `json:"measurement,omitempty"`
	Breakdown   *Breakdown   `json:"breakdown,omitempty"`
	Kind        string       `json:"kind"`
	Seq         int          `json:"seq,omitempty"`

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
	if len(line) > MaxRecordBytes {
		return Record{}, true, fmt.Errorf("metric record exceeds %d bytes", MaxRecordBytes)
	}
	payload := bytes.TrimSpace(line[i+len(Sentinel):])
	if !utf8.Valid(payload) {
		return Record{}, true, fmt.Errorf("metric record is not UTF-8")
	}
	var rec Record
	if err := json.Unmarshal(payload, &rec); err != nil {
		return Record{}, true, fmt.Errorf("decoding metric payload: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return Record{}, true, fmt.Errorf("decoding metric object: %w", err)
	}
	if _, versioned := fields["version"]; versioned {
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&rec); err != nil {
			return Record{}, true, fmt.Errorf("decoding v2 metric: %w", err)
		}
		for key := range fields {
			if !slices.Contains([]string{"version", "kind", "call_id", "id", "revision", "order", rec.Kind}, key) {
				return Record{}, true, fmt.Errorf("unexpected v2 field %q", key)
			}
		}
		if rec.Kind == KindText {
			var textFields map[string]json.RawMessage
			if err := json.Unmarshal(fields[KindText], &textFields); err != nil {
				return Record{}, true, fmt.Errorf("decoding text: %w", err)
			}
			for _, key := range []string{"text", "separator_before"} {
				if raw, ok := textFields[key]; !ok || bytes.Equal(raw, []byte("null")) {
					return Record{}, true, fmt.Errorf("missing text field %q", key)
				}
			}
		}
		if err := rec.validate(); err != nil {
			return Record{}, true, err
		}
		return rec, true, nil
	}
	if rec.Kind != KindTurn && rec.Kind != KindSession {
		return Record{}, true, fmt.Errorf("unknown metric kind %q", rec.Kind)
	}
	return rec, true, nil
}

// MaxRecordBytes bounds a complete framed line before decoding and replay storage.
const MaxRecordBytes = 512 << 10

// Call describes source coverage independently of the audio connection.
type Call struct {
	Target          string `json:"target"`
	State           string `json:"state"`
	Reason          string `json:"reason,omitempty"`
	InputBoundaries string `json:"input_boundaries"`
	ModelCalls      string `json:"model_calls"`
	GeneratedText   string `json:"generated_text"`
}

// Exchange is one input or response; only a proven native link supplies InputID.
type Exchange struct {
	Role       string `json:"role"`
	State      string `json:"state"`
	InputID    string `json:"input_id,omitempty"`
	LinkStatus string `json:"link_status"`
	Playback   string `json:"playback,omitempty"`
}

// Text replaces one identified segment, preserving its source spacing.
type Text struct {
	ExchangeID      string `json:"exchange_id,omitempty"`
	MessageID       string `json:"message_id"`
	Speaker         string `json:"speaker"`
	Text            string `json:"text"`
	State           string `json:"state"`
	Origin          string `json:"origin"`
	SeparatorBefore string `json:"separator_before"`
}

// Operation is an observed SDK invocation, independent of provider request IDs.
type Operation struct {
	ExchangeID        string `json:"exchange_id,omitempty"`
	Type              string `json:"type"`
	Name              string `json:"name"`
	Model             string `json:"model,omitempty"`
	Provider          string `json:"provider,omitempty"`
	SourceRequestID   string `json:"source_request_id,omitempty"`
	ParentOperationID string `json:"parent_operation_id,omitempty"`
	State             string `json:"state"`
	// Reason says what went wrong, and belongs only on a row that ended badly:
	// a tool's error text, or the deadline it ran past. On a returned row there
	// is nothing to explain, so carrying one there is a producer bug.
	Reason string `json:"reason,omitempty"`
}

// Breakdown is one measured reply split into the parts that make it up, in time
// order. It carries no exchange: the framework's own observer measures audio in
// and audio out and holds no request id, and attaching one by timing would be a
// guess the reader could not tell from a fact.
//
// The parts account for the whole interval, so their durations sum to the total.
// That is what makes a gap visible: time no service measured is a part of its
// own, owned by the pipeline, rather than quietly missing from the list.
type Breakdown struct {
	// MeasuredFrom is what the interval was anchored on, so a greeting is never
	// compared with a reply to a caller who spoke.
	MeasuredFrom string  `json:"measured_from"`
	TotalSecs    float64 `json:"total_secs"`
	Parts        []Part  `json:"parts"`
}

// Part is one stretch of a reply's interval.
type Part struct {
	// Key is stable across a label being reworded, so it is what to group on.
	Key   string `json:"key"`
	Label string `json:"label"`
	// Owner is the service that spent the time, or the setting that governs it.
	Owner string `json:"owner"`
	// OwnerKind answers where the time went without reading the owner's name:
	// a service you could swap, a setting you chose, the bot's own code, or the
	// pipeline between them.
	OwnerKind    string  `json:"owner_kind"`
	StartTime    float64 `json:"start_time"`
	DurationSecs float64 `json:"duration_secs"`
}

// Measurement is one independently arriving quantity. Absence is never zero.
type Measurement struct {
	ExchangeID  string   `json:"exchange_id,omitempty"`
	OperationID string   `json:"operation_id,omitempty"`
	Scope       string   `json:"scope"`
	Metric      string   `json:"metric"`
	Value       *float64 `json:"value,omitempty"`
	Unit        string   `json:"unit"`
	State       string   `json:"state"`
	Source      string   `json:"source"`
	Reason      string   `json:"reason,omitempty"`
	Model       string   `json:"model,omitempty"`
	Provider    string   `json:"provider,omitempty"`
}

func validID(id string, optional bool) bool {
	if id == "" {
		return optional
	}
	if len(id) > 128 {
		return false
	}
	for _, c := range id {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}

func (r Record) validate() error {
	bad := func() error { return fmt.Errorf("invalid v2 %s record", r.Kind) }
	if r.Version != 2 || !validID(r.CallID, false) || !validID(r.ID, false) || r.Revision <= 0 || r.Order <= 0 {
		return bad()
	}
	if strings.Trim(r.CallID, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_") != "" {
		return bad()
	}
	count := 0
	for _, present := range []bool{r.Call != nil, r.Exchange != nil, r.Text != nil, r.Operation != nil, r.Measurement != nil, r.Breakdown != nil} {
		if present {
			count++
		}
	}
	if count != 1 {
		return bad()
	}
	one := func(value string, allowed ...string) bool { return slices.Contains(allowed, value) }
	switch r.Kind {
	case KindCall:
		p := r.Call
		if p == nil || !one(p.Target, "livekit", "pipecat") || !one(p.State, "open", "ended", "error") || !one(p.InputBoundaries, "known", "partial") || !one(p.ModelCalls, "known", "partial") || !one(p.GeneratedText, "available", "limited") {
			return bad()
		}
	case KindExchange:
		p := r.Exchange
		if p == nil || !one(p.Role, "input", "response") || !one(p.State, "open", "ended", "interrupted", "incomplete") || !one(p.LinkStatus, "known", "unavailable") || !validID(p.InputID, true) || !one(p.Playback, "", "not_started", "speaking", "ended", "interrupted", "unknown") {
			return bad()
		}
		if p.InputID != "" && (p.Role != "response" || p.LinkStatus != "known") {
			return bad()
		}
	case KindText:
		p := r.Text
		if p == nil || !validID(p.ExchangeID, true) || !validID(p.MessageID, false) || !one(p.Speaker, "user", "assistant") || !one(p.State, "provisional", "final", "incomplete") || !one(p.Origin, "recognition", "generated", "transcription") || !one(p.SeparatorBefore, "", " ", "\n") {
			return bad()
		}
	case KindOperation:
		p := r.Operation
		if p == nil || !validID(p.ExchangeID, true) || !validID(p.ParentOperationID, true) || !validID(p.SourceRequestID, true) || p.Name == "" || !one(p.Type, "llm", "stt", "tts", "tool", "handoff") || !one(p.State, "running", "ended", "returned", "failed", "timed_out", "cancelled", "incomplete") {
			return bad()
		}
		if p.Reason != "" && !one(p.State, "failed", "timed_out", "cancelled") {
			return bad()
		}
	case KindMeasurement:
		p := r.Measurement
		if p == nil || !validID(p.ExchangeID, true) || !validID(p.OperationID, true) || !one(p.Scope, "call", "input", "response", "operation", "unassigned") || !one(p.Metric, "reply_latency", "first_response", "request_duration", "speech_duration", "tool_duration", "turn_detection", "transcription_delay", "text_aggregation", "first_speech", "playback_delay") || p.Unit != "seconds" || p.Source == "" || !one(p.State, "pending", "measured", "unavailable") {
			return bad()
		}
		if (p.State == "measured") != (p.Value != nil) {
			return bad()
		}
		if p.Value != nil && (math.IsNaN(*p.Value) || math.IsInf(*p.Value, 0) || *p.Value < 0) {
			return bad()
		}
		if p.Scope == "operation" && p.OperationID == "" || (p.Scope == "input" || p.Scope == "response") && p.ExchangeID == "" {
			return bad()
		}
		if p.Scope != "operation" && p.OperationID != "" || (p.Scope == "call" || p.Scope == "unassigned") && p.ExchangeID != "" {
			return bad()
		}
	case KindBreakdown:
		p := r.Breakdown
		if p == nil || !one(p.MeasuredFrom, "user_silence", "client_connected") || len(p.Parts) == 0 {
			return bad()
		}
		if !finite(p.TotalSecs) {
			return bad()
		}
		sum := 0.0
		for _, part := range p.Parts {
			if part.Key == "" || part.Label == "" || part.Owner == "" || !one(part.OwnerKind, "service", "setting", "bot", "pipeline") {
				return bad()
			}
			if !finite(part.DurationSecs) || !finite(part.StartTime) {
				return bad()
			}
			sum += part.DurationSecs
		}
		// The parts account for the whole interval, so a producer that filters
		// one out and keeps the framework's total is reporting a timeline that
		// does not add up. That is the one way this record can lie, and it is
		// arithmetic, so it is checked here rather than trusted.
		if math.Abs(sum-p.TotalSecs) > 1e-6 {
			return bad()
		}
	default:
		return bad()
	}
	return nil
}

// finite refuses the values a duration can never be: not a number, infinite, or
// negative. A negative span would render as a part that gave time back.
func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

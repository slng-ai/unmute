package cli

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/slng-ai/unmute/internal/devmetrics"
)

// Ring bounds. A container build is a few hundred lines and a long run is
// unbounded, so the buffer holds enough to explain a startup and then forgets.
const (
	devStreamMaxLines = 1000
	devStreamMaxBytes = devmetrics.MaxRecordBytes
	// Per-subscriber queue. Generous, because the alternative to dropping is
	// blocking, and this writer sits on the path the agent's own output takes:
	// a page that stops reading must never stall the runtime.
	devStreamQueue = 256
)

// devEventKind values, as they appear on the wire.
const (
	devEventLog    = "log"
	devEventMetric = "metric"
	devEventState  = "state"
	devEventGap    = "gap"
)

// Run states. `failed` is terminal.
const (
	devStateStarting = "starting"
	devStateReady    = "ready"
	devStateFailed   = "failed"
)

// devFailureMarkers is one substring test, deliberately. Parsing log levels
// needs per-SDK format knowledge that breaks on any release that reformats a
// line, and a flag that quietly stops working is worse than a crude one.
var devFailureMarkers = []string{"ERROR", "Traceback", "error:"}

// devEvent is one message on the stream. The zero value is not meaningful; use
// the constructors below.
type devEvent struct {
	T        string             `json:"t"`
	Seq      int                `json:"seq,omitempty"`
	StreamID string             `json:"stream_id,omitempty"`
	Text     string             `json:"text,omitempty"`
	Flagged  bool               `json:"flagged,omitempty"`
	Record   *devmetrics.Record `json:"record,omitempty"`
	State    string             `json:"state,omitempty"`
	Reason   string             `json:"reason,omitempty"`
	size     int
}

type devSubscription struct {
	backlog []devEvent
	events  chan devEvent
	done    chan struct{}
}

// devStream is the run's output, buffered for replay and fanned out to every
// connected page. It is an io.Writer, so it tees off the same sink that already
// writes dev.log: the file keeps everything the process printed, including
// measurement lines, because a log that does not match the process makes the
// measurement path itself undebuggable.
type devStream struct {
	mu         sync.Mutex
	events     []devEvent
	bytes      int
	nextSeq    int
	id         string
	state      string
	partial    []byte
	discarding bool
	subs       map[int]*devSubscription
	nextSub    int
}

func newDevStream() *devStream {
	return &devStream{state: devStateStarting, id: rand.Text(), subs: map[int]*devSubscription{}}
}

// Write consumes output a line at a time. A trailing partial line is held until
// its newline arrives, so a sentinel split across two writes still decodes.
func (s *devStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(p)
	for len(p) > 0 {
		end := bytes.IndexByte(p, '\n')
		complete := end >= 0
		if !complete {
			end = len(p)
		}
		if !s.discarding {
			if len(s.partial)+end > devStreamMaxBytes {
				s.partial = nil
				s.discarding = true
				s.publishLocked(devOversizedEvent())
			} else {
				s.partial = append(s.partial, p[:end]...)
			}
		}
		if !complete {
			break
		}
		if !s.discarding {
			s.publishLocked(classifyDevLine(s.partial))
		}
		s.partial = s.partial[:0]
		s.discarding = false
		p = p[end+1:]
	}
	return n, nil
}

func devOversizedEvent() devEvent {
	return devEvent{T: devEventLog, Flagged: true, Reason: "oversized-record",
		Text: fmt.Sprintf("dev output exceeds %d bytes; live data is incomplete (full output is in dev.log)", devStreamMaxBytes)}
}

// classifyDevLine turns one output line into the event the page should see.
func classifyDevLine(line []byte) devEvent {
	text := strings.TrimRight(string(line), "\r")
	if record, found, err := devmetrics.Extract(line); found {
		if err == nil {
			return devEvent{T: devEventMetric, Record: &record}
		}
		// Carried the sentinel and would not decode. Never dropped: this line is
		// the only evidence of why no measurements are appearing.
		return devEvent{T: devEventLog, Text: text, Flagged: true, Reason: "invalid-record"}
	}
	return devEvent{T: devEventLog, Text: text, Flagged: devLineLooksLikeFailure(text)}
}

func devLineLooksLikeFailure(text string) bool {
	for _, marker := range devFailureMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// SetState records the run's lifecycle and tells every page. `failed` is
// terminal: a later state cannot un-fail a run.
func (s *devStream) SetState(state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == devStateFailed {
		return
	}
	s.state = state
	s.broadcastLocked(devEvent{T: devEventState, State: state, StreamID: s.id})
}

func (s *devStream) State() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Callers hold mu through framing and publication, preserving writer order even
// when stdout and stderr write concurrently. No subscriber send can block.
func (s *devStream) publishLocked(ev devEvent) {
	s.nextSeq++
	ev.Seq = s.nextSeq
	ev.StreamID = s.id
	payload, err := json.Marshal(ev)
	if err != nil || len(payload) > devStreamMaxBytes {
		ev = devOversizedEvent()
		if err != nil {
			ev.Reason = "invalid-record"
			ev.Text = "dev event could not be encoded; live data is incomplete"
		}
		ev.Seq, ev.StreamID = s.nextSeq, s.id
		payload, _ = json.Marshal(ev) // the diagnostic contains only strings and integers
	}
	ev.size = len(payload)
	s.events = append(s.events, ev)
	s.bytes += ev.size
	for len(s.events) > devStreamMaxLines || s.bytes > devStreamMaxBytes {
		s.bytes -= s.events[0].size
		s.events[0] = devEvent{}
		s.events = s.events[1:]
	}
	if ev.Reason != "" {
		s.broadcastLocked(devEvent{T: devEventGap, StreamID: s.id, Reason: ev.Reason})
	}
	s.broadcastLocked(ev)
}

func (s *devStream) broadcastLocked(ev devEvent) {
	// Sent under the lock on purpose. Every send is non-blocking, so this cannot
	// stall, and it is the only thing that makes closing a subscriber safe: an
	// unlocked send races with cancel() closing the same channel.
	for id, sub := range s.subs {
		select {
		case sub.events <- ev:
		default:
			delete(s.subs, id)
			close(sub.done)
			close(sub.events)
		}
	}
}

// Subscribe returns the backlog a page needs plus a channel of what comes next.
// Startup output is produced before a browser finishes loading, so without the
// backlog the most interesting lines are the ones nobody sees.
//
// lastID is the browser's Last-Event-ID. A missing range is explicitly reported
// before replay; an invalid cursor is refused rather than silently reset.
func (s *devStream) Subscribe(lastID string) (*devSubscription, func(), error) {
	streamID, after := "", 0
	if lastID != "" {
		var raw string
		var ok bool
		streamID, raw, ok = strings.Cut(lastID, ":")
		n, err := strconv.Atoi(raw)
		if !ok || streamID == "" || len(streamID) > 128 || strings.Trim(streamID, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_") != "" || err != nil || n < 1 || strconv.Itoa(n) != raw {
			return nil, nil, fmt.Errorf("invalid Last-Event-ID: expected stream-id:sequence")
		}
		after = n
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if streamID == s.id && after > s.nextSeq {
		return nil, nil, fmt.Errorf("Last-Event-ID is ahead of the dev stream")
	}
	sub := &devSubscription{events: make(chan devEvent, devStreamQueue), done: make(chan struct{})}
	// The current state first, so a page that connects late is not told it is
	// still starting by the absence of an event it missed.
	sub.backlog = append(sub.backlog, devEvent{T: devEventState, State: s.state, StreamID: s.id})
	reason := ""
	if streamID != "" && streamID != s.id {
		reason = "stream-restarted"
		after = 0
	} else if len(s.events) > 0 && after < s.events[0].Seq-1 {
		reason = "history-evicted"
	}
	if reason != "" {
		sub.backlog = append(sub.backlog, devEvent{T: devEventGap, StreamID: s.id, Reason: reason})
	}
	for _, ev := range s.events {
		if ev.Seq > after {
			if ev.Reason != "" {
				sub.backlog = append(sub.backlog, devEvent{T: devEventGap, StreamID: s.id, Reason: ev.Reason})
			}
			sub.backlog = append(sub.backlog, ev)
		}
	}

	id := s.nextSub
	s.nextSub++
	s.subs[id] = sub
	return sub, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if existing, ok := s.subs[id]; ok {
			delete(s.subs, id)
			close(existing.done)
			close(existing.events)
		}
	}, nil
}

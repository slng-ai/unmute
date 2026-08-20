package cli

import (
	"bytes"
	"strings"
	"sync"

	"github.com/slng-ai/unmute/internal/devmetrics"
)

// Ring bounds. A container build is a few hundred lines and a long run is
// unbounded, so the buffer holds enough to explain a startup and then forgets.
const (
	devStreamMaxLines = 1000
	devStreamMaxBytes = 512 << 10
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
	T       string             `json:"t"`
	Seq     int                `json:"seq,omitempty"`
	Text    string             `json:"text,omitempty"`
	Flagged bool               `json:"flagged,omitempty"`
	Record  *devmetrics.Record `json:"record,omitempty"`
	State   string             `json:"state,omitempty"`
}

// devStream is the run's output, buffered for replay and fanned out to every
// connected page. It is an io.Writer, so it tees off the same sink that already
// writes dev.log: the file keeps everything the process printed, including
// measurement lines, because a log that does not match the process makes the
// measurement path itself undebuggable.
type devStream struct {
	mu      sync.Mutex
	events  []devEvent
	bytes   int
	nextSeq int
	state   string
	partial []byte
	subs    map[int]chan devEvent
	nextSub int
}

func newDevStream() *devStream {
	return &devStream{state: devStateStarting, subs: map[int]chan devEvent{}}
}

// Write consumes output a line at a time. A trailing partial line is held until
// its newline arrives, so a sentinel split across two writes still decodes.
func (s *devStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.partial = append(s.partial, p...)
	var lines [][]byte
	for {
		i := bytes.IndexByte(s.partial, '\n')
		if i < 0 {
			break
		}
		line := make([]byte, i)
		copy(line, s.partial[:i])
		lines = append(lines, line)
		s.partial = s.partial[i+1:]
	}
	// Bound the carry-over: a runtime that never emits a newline must not grow
	// this without limit.
	if len(s.partial) > devStreamMaxBytes {
		s.partial = s.partial[:0]
	}
	s.mu.Unlock()

	for _, line := range lines {
		s.publish(classifyDevLine(line))
	}
	return len(p), nil
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
		return devEvent{T: devEventLog, Text: text, Flagged: true}
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
	if s.state == devStateFailed {
		s.mu.Unlock()
		return
	}
	s.state = state
	s.mu.Unlock()
	s.publish(devEvent{T: devEventState, State: state})
}

func (s *devStream) State() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *devStream) publish(ev devEvent) {
	s.mu.Lock()
	s.nextSeq++
	ev.Seq = s.nextSeq
	if ev.T != devEventState { // state is latched, not history
		s.events = append(s.events, ev)
		s.bytes += len(ev.Text)
		for len(s.events) > devStreamMaxLines || (s.bytes > devStreamMaxBytes && len(s.events) > 1) {
			s.bytes -= len(s.events[0].Text)
			s.events = s.events[1:]
		}
	}
	// Sent under the lock on purpose. Every send is non-blocking, so this cannot
	// stall, and it is the only thing that makes closing a subscriber safe: an
	// unlocked send races with cancel() closing the same channel.
	for _, ch := range s.subs {
		select {
		case ch <- ev:
		default: // see devStreamQueue: dropping beats stalling the runtime
		}
	}
	s.mu.Unlock()
}

// Subscribe returns the backlog a page needs plus a channel of what comes next.
// Startup output is produced before a browser finishes loading, so without the
// backlog the most interesting lines are the ones nobody sees.
//
// after is a Last-Event-ID: the stream resumes past it when it is still buffered,
// and otherwise replays what it still has.
func (s *devStream) Subscribe(after int) (backlog []devEvent, ch <-chan devEvent, cancel func()) {
	s.mu.Lock()
	defer s.mu.Unlock()

	backlog = make([]devEvent, 0, len(s.events)+1)
	// The current state first, so a page that connects late is not told it is
	// still starting by the absence of an event it missed.
	backlog = append(backlog, devEvent{T: devEventState, State: s.state})
	for _, ev := range s.events {
		if ev.Seq > after {
			backlog = append(backlog, ev)
		}
	}

	queue := make(chan devEvent, devStreamQueue)
	id := s.nextSub
	s.nextSub++
	s.subs[id] = queue
	return backlog, queue, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if existing, ok := s.subs[id]; ok {
			delete(s.subs, id)
			close(existing)
		}
	}
}

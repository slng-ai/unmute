package cli

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/slng-ai/unmute/internal/devmetrics"
)

func TestDevStreamSplitsMetricsOutOfTheLogStream(t *testing.T) {
	s := newDevStream()
	// The compose prefix is the case that matters: one target relays every line
	// through a container runtime, so the sentinel is not at the start.
	_, _ = s.Write([]byte("INFO  registered worker\n"))
	_, _ = s.Write([]byte(`agent-1  | ` + devmetrics.Sentinel + `{"kind":"turn","seq":1,"e2e":0.5}` + "\n"))
	_, _ = s.Write([]byte("ERROR  provider refused the key\n"))
	_, _ = s.Write([]byte(devmetrics.Sentinel + "{not json\n"))

	backlog, _, cancel := s.Subscribe(0)
	defer cancel()

	var kinds []string
	for _, ev := range backlog {
		kinds = append(kinds, ev.T)
	}
	want := []string{devEventState, devEventLog, devEventMetric, devEventLog, devEventLog}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	if rec := backlog[2].Record; rec == nil || rec.Seq != 1 || rec.E2E == nil || *rec.E2E != 0.5 {
		t.Errorf("metric event carries %+v", backlog[2].Record)
	}
	if backlog[1].Flagged {
		t.Error("an ordinary line was flagged")
	}
	if !backlog[3].Flagged {
		t.Error("an ERROR line was not flagged")
	}
	// A sentinel that will not decode must survive as a visible line: it is the
	// only evidence of why measurements have stopped appearing.
	if !backlog[4].Flagged || !strings.Contains(backlog[4].Text, "not json") {
		t.Errorf("undecodable sentinel line was not surfaced: %+v", backlog[4])
	}
}

func TestDevStreamHoldsALineSplitAcrossWrites(t *testing.T) {
	s := newDevStream()
	// A sentinel split mid-payload still has to decode, because a writer boundary
	// has nothing to do with a line boundary.
	_, _ = s.Write([]byte(devmetrics.Sentinel + `{"kind":"turn",`))
	_, _ = s.Write([]byte(`"seq":9}` + "\n"))

	backlog, _, cancel := s.Subscribe(0)
	defer cancel()
	if len(backlog) != 2 || backlog[1].T != devEventMetric {
		t.Fatalf("backlog = %+v", backlog)
	}
	if backlog[1].Record.Seq != 9 {
		t.Errorf("seq = %d, want 9", backlog[1].Record.Seq)
	}
}

func TestDevStreamEvictsOldestFirst(t *testing.T) {
	s := newDevStream()
	for i := 0; i < devStreamMaxLines+50; i++ {
		_, _ = fmt.Fprintf(s, "line %d\n", i)
	}
	backlog, _, cancel := s.Subscribe(0)
	defer cancel()

	logs := backlog[1:] // first entry is the latched state
	if len(logs) != devStreamMaxLines {
		t.Fatalf("kept %d lines, want %d", len(logs), devStreamMaxLines)
	}
	if !strings.Contains(logs[0].Text, "line 50") {
		t.Errorf("oldest kept line is %q, want line 50", logs[0].Text)
	}
	if !strings.Contains(logs[len(logs)-1].Text, fmt.Sprintf("line %d", devStreamMaxLines+49)) {
		t.Errorf("newest kept line is %q", logs[len(logs)-1].Text)
	}
}

func TestDevStreamFansOutToEveryPage(t *testing.T) {
	s := newDevStream()
	_, chA, cancelA := s.Subscribe(0)
	defer cancelA()
	_, chB, cancelB := s.Subscribe(0)
	defer cancelB()

	_, _ = s.Write([]byte("shared line\n"))

	for name, ch := range map[string]<-chan devEvent{"first": chA, "second": chB} {
		select {
		case ev := <-ch:
			if ev.Text != "shared line" {
				t.Errorf("%s page got %q", name, ev.Text)
			}
		default:
			t.Errorf("%s page received nothing", name)
		}
	}
}

func TestDevStreamResumesAfterLastEventID(t *testing.T) {
	s := newDevStream()
	_, _ = s.Write([]byte("one\ntwo\nthree\n"))

	backlog, _, cancel := s.Subscribe(2) // seq 1 is "one", so resume past "two"
	defer cancel()
	var texts []string
	for _, ev := range backlog {
		if ev.T == devEventLog {
			texts = append(texts, ev.Text)
		}
	}
	if strings.Join(texts, ",") != "three" {
		t.Fatalf("resumed with %v, want just three", texts)
	}
}

func TestDevStreamLatchesFailure(t *testing.T) {
	s := newDevStream()
	s.SetState(devStateFailed)
	s.SetState(devStateReady) // must not un-fail the run
	if got := s.State(); got != devStateFailed {
		t.Fatalf("state = %q, want %q", got, devStateFailed)
	}
	backlog, _, cancel := s.Subscribe(0)
	defer cancel()
	if backlog[0].T != devEventState || backlog[0].State != devStateFailed {
		t.Errorf("a page connecting late is told %+v", backlog[0])
	}
}

func TestDevStreamNeverBlocksTheRuntime(t *testing.T) {
	s := newDevStream()
	// A page that stops reading fills its queue. The writer is the path the
	// agent's own output takes, so it has to keep going regardless.
	_, _, cancel := s.Subscribe(0)
	defer cancel()
	// No assertion needed: if the writer blocks on the full queue, this never
	// returns and the test fails by timing out, which is the failure to catch.
	for i := 0; i < devStreamQueue*3; i++ {
		_, _ = s.Write([]byte("noisy\n"))
	}
}

func TestDevStreamWritesConcurrently(t *testing.T) {
	// Compose gives stdout and stderr the same sink, so two goroutines write.
	s := newDevStream()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_, _ = fmt.Fprintf(s, "writer %d line %d\n", n, j)
			}
		}(i)
	}
	wg.Wait()
	backlog, _, cancel := s.Subscribe(0)
	defer cancel()
	if got := len(backlog) - 1; got != 200 {
		t.Fatalf("kept %d lines, want 200", got)
	}
}

package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/slng-ai/unmute/internal/devmetrics"
)

func subscribeDev(t *testing.T, s *devStream, cursor string) ([]devEvent, <-chan devEvent, func()) {
	t.Helper()
	sub, cancel, err := s.Subscribe(cursor)
	if err != nil {
		t.Fatal(err)
	}
	return sub.backlog, sub.events, cancel
}

func TestDevStreamCountsSerializedMetricBytes(t *testing.T) {
	s := newDevStream()
	line := devmetrics.Sentinel + `{"version":2,"kind":"text","call_id":"call-1","id":"text-1","revision":1,"order":1,"text":{"message_id":"message-1","speaker":"assistant","text":"` + strings.Repeat("x", 200000) + `","state":"final","origin":"generated","separator_before":""}}` + "\n"
	for range 4 {
		_, _ = s.Write([]byte(line))
	}
	backlog, _, cancel := subscribeDev(t, s, "")
	defer cancel()
	total, records := 0, 0
	for _, ev := range backlog {
		if ev.T == devEventMetric {
			payload, err := json.Marshal(ev)
			if err != nil {
				t.Fatal(err)
			}
			total += len(payload)
			records++
		}
	}
	if records != 2 || total > devStreamMaxBytes || s.bytes != total {
		t.Fatalf("retained %d records, %d encoded bytes, accounted %d; want two records within %d", records, total, s.bytes, devStreamMaxBytes)
	}
}

func TestDevStreamRejectsOversizedLinesAndRecoversAtNewline(t *testing.T) {
	for _, split := range []bool{false, true} {
		t.Run(fmt.Sprintf("split=%t", split), func(t *testing.T) {
			s := newDevStream()
			line := devmetrics.Sentinel + strings.Repeat("x", devStreamMaxBytes)
			if split {
				_, _ = s.Write([]byte(line[:len(line)/2]))
				_, _ = s.Write([]byte(line[len(line)/2:]))
				// The tail is still part of the refused line, not a new record.
				_, _ = s.Write([]byte("ignored tail\nrecovered\n"))
			} else {
				_, _ = s.Write([]byte(line + "\nrecovered\n"))
			}
			backlog, _, cancel := subscribeDev(t, s, "")
			defer cancel()
			flagged, gaps, recovered := 0, 0, 0
			for _, ev := range backlog {
				if ev.Flagged {
					flagged++
					if len(ev.Text) > 1024 {
						t.Error("oversized contents were copied into the diagnostic")
					}
				}
				if ev.T == "gap" {
					gaps++
				}
				if ev.Text == "recovered" {
					recovered++
				}
				if ev.Text == "ignored tail" {
					t.Error("refused line's tail became a new log event")
				}
			}
			if flagged != 1 || gaps != 1 || recovered != 1 {
				t.Fatalf("flagged=%d gaps=%d recovered=%d; want one of each", flagged, gaps, recovered)
			}
			if s.bytes > devStreamMaxBytes || len(s.partial) > devStreamMaxBytes {
				t.Fatal("oversized input escaped the memory bound")
			}
		})
	}
}

func TestDevStreamMalformedRecordSignalsIncompleteData(t *testing.T) {
	s := newDevStream()
	_, live, cancel := subscribeDev(t, s, "")
	defer cancel()
	_, _ = s.Write([]byte(devmetrics.Sentinel + "{not json\n"))
	if ev := <-live; ev.T != "gap" || ev.Seq != 0 {
		t.Fatalf("malformed record did not first signal an ID-less gap: %+v", ev)
	}
}

func TestDevStreamStateDoesNotConsumeDataSequence(t *testing.T) {
	s := newDevStream()
	_, live, cancel := subscribeDev(t, s, "")
	defer cancel()
	s.SetState(devStateReady)
	if ev := <-live; ev.Seq != 0 {
		t.Fatalf("state consumed data sequence %d", ev.Seq)
	}
	_, _ = s.Write([]byte("first data\n"))
	if ev := <-live; ev.Seq != 1 {
		t.Fatalf("first data sequence = %d, want 1", ev.Seq)
	}
}

func TestDevStreamOverflowRemovesTheSubscriber(t *testing.T) {
	s := newDevStream()
	sub, cancel, err := s.Subscribe("")
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	for range devStreamQueue + 1 {
		_, _ = s.Write([]byte("fill queue\n"))
	}
	s.mu.Lock()
	remaining := len(s.subs)
	s.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("overflow left %d stalled subscribers attached", remaining)
	}
	select {
	case <-sub.done:
	default:
		t.Fatal("overflow did not signal termination before draining the queue")
	}
	if len(sub.events) != devStreamQueue {
		t.Fatal("test did not leave queued events behind the terminal signal")
	}
}

func TestDevStreamReportsMissingReplayRanges(t *testing.T) {
	s := newDevStream()
	for range devStreamMaxLines + 2 {
		_, _ = s.Write([]byte("line\n"))
	}
	for _, tc := range []struct{ cursor, reason string }{
		{s.id + ":1", "history-evicted"},
		{s.id + ":2", ""}, // cursor itself is evicted; every later event survives
		{"previous:1", "stream-restarted"},
		{"", "history-evicted"},
	} {
		t.Run(tc.cursor, func(t *testing.T) {
			sub, cancel, err := s.Subscribe(tc.cursor)
			if err != nil {
				t.Fatal(err)
			}
			defer cancel()
			first := sub.backlog[1]
			if tc.reason == "" {
				if first.T != devEventLog || first.Seq != 3 {
					t.Fatalf("complete replay started at %+v", first)
				}
			} else if first.T != devEventGap || first.Reason != tc.reason || first.Seq != 0 {
				t.Fatalf("gap = %+v, want %q before retained data", first, tc.reason)
			}
		})
	}
	if _, _, err := s.Subscribe(s.id + ":999999"); err == nil {
		t.Fatal("accepted a cursor ahead of this stream")
	}
}

func TestDevStreamConcurrentCancellation(t *testing.T) {
	s := newDevStream()
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 100 {
				sub, cancel, err := s.Subscribe("")
				if err != nil {
					t.Error(err)
					return
				}
				_, _ = s.Write([]byte("concurrent\n"))
				cancel()
				cancel() // cancellation remains safe after overflow or another cancel
				<-sub.done
			}
		})
	}
	wg.Wait()
	if len(s.subs) != 0 {
		t.Fatal("cancelled subscribers remain attached")
	}
}

func TestDevStreamSplitsMetricsOutOfTheLogStream(t *testing.T) {
	s := newDevStream()
	// The compose prefix is the case that matters: one target relays every line
	// through a container runtime, so the sentinel is not at the start.
	_, _ = s.Write([]byte("INFO  registered worker\n"))
	_, _ = s.Write([]byte(`agent-1  | ` + devmetrics.Sentinel + `{"kind":"turn","seq":1,"e2e":0.5}` + "\n"))
	_, _ = s.Write([]byte("ERROR  provider refused the key\n"))
	_, _ = s.Write([]byte(devmetrics.Sentinel + "{not json\n"))

	backlog, _, cancel := subscribeDev(t, s, "")
	defer cancel()

	var kinds []string
	for _, ev := range backlog {
		kinds = append(kinds, ev.T)
	}
	want := []string{devEventState, devEventLog, devEventMetric, devEventLog, devEventGap, devEventLog}
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
	if !backlog[5].Flagged || !strings.Contains(backlog[5].Text, "not json") {
		t.Errorf("undecodable sentinel line was not surfaced: %+v", backlog[5])
	}
}

func TestDevStreamHoldsALineSplitAcrossWrites(t *testing.T) {
	s := newDevStream()
	// A sentinel split mid-payload still has to decode, because a writer boundary
	// has nothing to do with a line boundary.
	_, _ = s.Write([]byte(devmetrics.Sentinel + `{"kind":"turn",`))
	_, _ = s.Write([]byte(`"seq":9}` + "\n"))

	backlog, _, cancel := subscribeDev(t, s, "")
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
	backlog, _, cancel := subscribeDev(t, s, "")
	defer cancel()

	logs := backlog[2:] // state, then the reported eviction gap
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
	_, chA, cancelA := subscribeDev(t, s, "")
	defer cancelA()
	_, chB, cancelB := subscribeDev(t, s, "")
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

	backlog, _, cancel := subscribeDev(t, s, s.id+":2") // seq 1 is "one", so resume past "two"
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
	backlog, _, cancel := subscribeDev(t, s, "")
	defer cancel()
	if backlog[0].T != devEventState || backlog[0].State != devStateFailed {
		t.Errorf("a page connecting late is told %+v", backlog[0])
	}
}

func TestDevStreamNeverBlocksTheRuntime(t *testing.T) {
	s := newDevStream()
	// A page that stops reading fills its queue. The writer is the path the
	// agent's own output takes, so it has to keep going regardless.
	_, _, cancel := subscribeDev(t, s, "")
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
	backlog, _, cancel := subscribeDev(t, s, "")
	defer cancel()
	if got := len(backlog) - 1; got != 200 {
		t.Fatalf("kept %d lines, want 200", got)
	}
}

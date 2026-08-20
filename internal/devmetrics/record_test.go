package devmetrics

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The two fixtures are the agreement half of this contract: the producers live in
// generated Python that no Go test can import, so a captured line is the only
// thing that can catch the two ends drifting apart. `make smoke` asserts a real
// run still emits a line this decoder accepts; these assert the decoder handles
// what each target actually reports, including what it stays silent about.
func readFixture(t *testing.T, name string) []Record {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	var out []Record
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := scan.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		rec, found, err := Extract([]byte(line))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !found {
			t.Fatalf("%s: line carries no sentinel: %s", name, line)
		}
		out = append(out, rec)
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatalf("%s: no records", name)
	}
	return out
}

func TestPipecatFixtureReportsWhatPipecatMeasures(t *testing.T) {
	recs := readFixture(t, "sample-pipecat.jsonl")
	turn := recs[0]
	if turn.Kind != KindTurn || turn.Seq != 1 {
		t.Fatalf("first record is %+v, want a turn with seq 1", turn)
	}
	if turn.E2E == nil || turn.UserTurn == nil || turn.TextAggregation == nil {
		t.Error("pipecat turn is missing a measurement pipecat reports")
	}
	// Pipecat does not report this. It must stay absent rather than arrive as
	// zero, which would read as an unusually fast stage.
	if turn.Transcription != nil {
		t.Errorf("transcription should be absent on pipecat, got %v", *turn.Transcription)
	}
	if len(turn.Stages) != 3 {
		t.Fatalf("want three stages, got %d", len(turn.Stages))
	}
	for _, s := range turn.Stages {
		if s.TTFB == nil {
			t.Errorf("stage %q has no ttfb", s.Name)
		}
		if s.Total != nil {
			t.Errorf("stage %q reports a total, which pipecat does not measure", s.Name)
		}
		if s.Kind != "stt" && s.Kind != "llm" && s.Kind != "tts" {
			t.Errorf("stage %q has kind %q", s.Name, s.Kind)
		}
	}
	if len(turn.Tools) != 1 || turn.Tools[0].Seconds == nil {
		t.Errorf("want one tool with a duration, got %+v", turn.Tools)
	}
	if last := recs[len(recs)-1]; last.Kind != KindSession || last.FirstSpeech == nil {
		t.Errorf("want a session record carrying first_speech, got %+v", last)
	}
}

func TestLiveKitFixtureReportsWhatLiveKitMeasures(t *testing.T) {
	recs := readFixture(t, "sample-livekit.jsonl")
	turn := recs[0]
	if turn.E2E == nil || turn.UserTurn == nil || turn.Transcription == nil {
		t.Error("livekit turn is missing a measurement livekit reports")
	}
	if turn.TextAggregation != nil {
		t.Errorf("text_aggregation should be absent on livekit, got %v", *turn.TextAggregation)
	}
	// livekit names its stages by provider and reports a total only for the voice,
	// which is the agent's own speech window. The transcriber reports a model and
	// no timing at all, and that stage still belongs in the record.
	var sawTotal, sawModelOnly bool
	for _, s := range turn.Stages {
		if s.Model == "" {
			t.Errorf("stage %q carries no model", s.Name)
		}
		if s.Total != nil {
			sawTotal = true
		}
		if s.TTFB == nil && s.Total == nil {
			sawModelOnly = true
		}
	}
	if !sawTotal {
		t.Error("no stage reports a total, so the reply's stream time is missing")
	}
	if !sawModelOnly {
		t.Error("expected the transcriber stage to appear with a model and no timing")
	}
	if !recs[1].Interrupted {
		t.Error("second livekit turn should be marked interrupted")
	}
	if recs[0].Interrupted {
		t.Error("first livekit turn should not be marked interrupted")
	}
}

func TestExtractFindsTheSentinelBehindAContainerPrefix(t *testing.T) {
	// docker compose relays every line with a service prefix, so the sentinel is
	// not at the start on one of the two targets. An anchored test would pass the
	// suite and fail the run.
	line := []byte(`agent-1  | ` + Sentinel + `{"kind":"turn","seq":7,"e2e":0.5}`)
	rec, found, err := Extract(line)
	if err != nil || !found {
		t.Fatalf("prefixed line: found=%v err=%v", found, err)
	}
	if rec.Seq != 7 {
		t.Errorf("seq = %d, want 7", rec.Seq)
	}
}

func TestExtractFlagsBadPayloadsInsteadOfDroppingThem(t *testing.T) {
	cases := []struct {
		name  string
		line  string
		found bool
	}{
		{"ordinary output", "INFO  registered worker", false},
		{"truncated json", Sentinel + `{"kind":"turn","seq":`, true},
		{"not an object", Sentinel + `"nope"`, true},
		{"unknown kind", Sentinel + `{"kind":"weather"}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, found, err := Extract([]byte(tc.line))
			if found != tc.found {
				t.Fatalf("found = %v, want %v", found, tc.found)
			}
			// A line carrying the sentinel is either a record or an error the
			// caller shows. It is never silently discarded.
			if tc.found && err == nil {
				t.Error("bad payload decoded without complaint")
			}
			if !tc.found && err != nil {
				t.Errorf("ordinary output produced an error: %v", err)
			}
		})
	}
}

func TestAbsentTimingStaysNil(t *testing.T) {
	rec, _, err := Extract([]byte(Sentinel + `{"kind":"turn","seq":1}`))
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]*float64{
		"e2e":              rec.E2E,
		"user_turn":        rec.UserTurn,
		"transcription":    rec.Transcription,
		"text_aggregation": rec.TextAggregation,
		"first_speech":     rec.FirstSpeech,
	} {
		if got != nil {
			t.Errorf("%s decoded to %v, want nil: an unreported stage must not read as zero", name, *got)
		}
	}
}

package devmetrics

import (
	"bufio"
	"encoding/json"
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

func TestStreamingContract(t *testing.T) {
	for _, target := range []string{"livekit", "pipecat"} {
		records := readFixture(t, "streaming-"+target+".jsonl")
		kinds := map[string]bool{}
		for _, record := range records {
			kinds[record.Kind] = true
		}
		if len(kinds) != 5 {
			t.Fatalf("%s: got %v", target, kinds)
		}
	}
}

func TestStreamingValidation(t *testing.T) {
	valid := `{"version":2,"kind":"measurement","call_id":"call-a","id":"m:1","revision":1,"order":1,"measurement":{"scope":"unassigned","metric":"first_response","value":0,"unit":"seconds","state":"measured","source":"sdk"}}`
	for _, replace := range [][2]string{
		{`"version":2`, `"version":3`}, {`"revision":1`, `"revision":0`},
		{`"order":1`, `"order":-1`}, {`"call-a"`, `""`},
		{`"m:1"`, `"` + strings.Repeat("x", 129) + `"`},
		{`"measured"`, `"banana"`}, {`"value":0`, `"value":-1`},
		{`"value":0`, `"value":1e999`}, {`"value":0,`, ``},
		{`"measured"`, `"unavailable"`}, {`"kind":"measurement"`, `"kind":"text"`},
		{`"measurement":`, `"text":{},"measurement":`},
	} {
		bad := strings.Replace(valid, replace[0], replace[1], 1)
		if _, found, err := Extract([]byte(Sentinel + bad)); !found || err == nil {
			t.Errorf("accepted invalid record: %s", bad)
		}
	}
	for _, input := range []string{valid, strings.Replace(valid, `"value":0,`, ``, 1)} {
		if !strings.Contains(input, `"value"`) {
			input = strings.Replace(input, `"measured"`, `"unavailable"`, 1)
		}
		if _, _, err := Extract([]byte("container | " + Sentinel + input)); err != nil {
			t.Error(err)
		}
	}
	if _, found, err := Extract([]byte(Sentinel + strings.Repeat(" ", 512<<10) + valid)); !found || err == nil {
		t.Error("oversized framed record accepted")
	}
}

func TestStreamingPayloadStates(t *testing.T) {
	for _, kind := range []string{"call", "exchange", "text", "operation"} {
		records, err := os.ReadFile("testdata/streaming-livekit.jsonl")
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(records), "\n") {
			if !strings.Contains(line, `"kind":"`+kind+`"`) {
				continue
			}
			var record map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, Sentinel)), &record); err != nil {
				t.Fatal(err)
			}
			payload := record[kind].(map[string]any)
			payload["state"] = "invalid"
			encoded, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := Extract(append([]byte(Sentinel), encoded...)); err == nil {
				t.Errorf("accepted invalid %s state", kind)
			}
			break
		}
	}
}

func TestStreamingRejectsMissingTextAndScope(t *testing.T) {
	text := `{"version":2,"kind":"text","call_id":"call-a","id":"s","revision":1,"order":1,"text":{"message_id":"m","speaker":"user","text":"","state":"final","origin":"recognition","separator_before":""}}`
	for _, bad := range []string{
		strings.Replace(text, `"text":"",`, "", 1),
		strings.Replace(text, `,"separator_before":""`, "", 1),
		strings.Replace(text, `"message_id":"m"`, `"message_id":""`, 1),
		strings.Replace(text, `"call-a"`, `"call with spaces"`, 1),
		strings.Replace(text, `"origin":"recognition"`, `"origin":"thought"`, 1),
		strings.Replace(text, `"text":""`, `"text":"`+string([]byte{0xff})+`"`, 1),
		strings.Replace(text, `"version":2`, `"version":0`, 1),
		strings.Replace(text, `"version":2`, `"version":2,"e2e":0`, 1),
		strings.Replace(text, `"speaker":"user"`, `"speaker":"user","prompt":"private"`, 1),
	} {
		if _, _, err := Extract([]byte(Sentinel + bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	if _, _, err := Extract([]byte(Sentinel + text)); err != nil {
		t.Errorf("empty correction: %v", err)
	}
	measurement := `{"version":2,"kind":"measurement","call_id":"call-a","id":"m","revision":1,"order":1,"measurement":{"scope":"operation","metric":"first_response","unit":"seconds","state":"unavailable","source":"sdk"}}`
	if _, _, err := Extract([]byte(Sentinel + measurement)); err == nil {
		t.Error("operation measurement without its operation accepted")
	}
}

func TestPlaybackDelayKeepsItsOwnQuantity(t *testing.T) {
	line := Sentinel + `{"version":2,"kind":"measurement","call_id":"call-a","id":"playback","revision":1,"order":1,"measurement":{"scope":"response","exchange_id":"reply","metric":"playback_delay","value":0.002,"unit":"seconds","state":"measured","source":"LiveKit playback_latency"}}`
	record, found, err := Extract([]byte(line))
	if err != nil || !found {
		t.Fatalf("native playback delay lost: %v", err)
	}
	if record.Measurement.Metric != "playback_delay" || *record.Measurement.Value != 0.002 {
		t.Fatal("playback delay became another quantity")
	}
}

// A control hands over to a task rather than returning a result, so it carries
// an operation type of its own: a row the reader can see, with no duration. A
// decoder that refuses the type drops the row, and the model call the control
// caused reads as a duplicate.
func TestHandoffIsAnOperationTypeOfItsOwn(t *testing.T) {
	row := `{"version":2,"kind":"operation","call_id":"call-a","id":"handoff-1","revision":1,"order":1,` +
		`"operation":{"type":"handoff","name":"verify_customer","state":"running"}}`
	record, found, err := Extract([]byte(Sentinel + row))
	if err != nil || !found {
		t.Fatalf("a handoff row was refused: %v", err)
	}
	if record.Operation.Type != "handoff" {
		t.Errorf("type %q, want handoff", record.Operation.Type)
	}
	if _, _, err := Extract([]byte(Sentinel + strings.Replace(row, `"handoff"`, `"transfer"`, 1))); err == nil {
		t.Error("accepted an operation type no producer emits")
	}
}

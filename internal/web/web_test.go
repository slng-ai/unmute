package web

import (
	"strings"
	"testing"
)

func TestV16PipecatUsesRTVI2SegmentUpdates(t *testing.T) {
	raw, err := FS.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, want := range []string{
		`type:"client-ready"`,
		`version:"2.0.0"`,
		`new Map()`,
		`t === "bot-output"`,
		`d.segment_id`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("web client missing %q", want)
		}
	}
	if strings.Contains(source, `t === "bot-transcription"`) {
		t.Error("web client still renders deprecated bot-transcription frames")
	}
}

func TestV17LiveKitUpdatesTranscriptionSegment(t *testing.T) {
	raw, err := FS.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	if !strings.Contains(source, `setBotSegment(seg.id, text)`) {
		t.Error("LiveKit remote transcription does not update by segment id")
	}
	if strings.Contains(source, `else pushTurn("bot", text)`) {
		t.Error("LiveKit remote transcription still appends every update")
	}
}

func TestPipecatWaitsForARealConnectionAndStartsFresh(t *testing.T) {
	raw, err := FS.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, want := range []string{
		`pcId = null;`,
		`await waitForPeer(pc);`,
		`clientReadyTimer = setInterval(sendClientReady, 500)`,
		`t === "bot-ready"`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("Pipecat reconnect/ready contract missing %q", want)
		}
	}
}

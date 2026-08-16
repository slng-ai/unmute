package tui

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var updateGolden = flag.Bool("update", false, "update golden frame files")

// TestV49ConsoleFrameGolden pins the composed console frame at 80x24 under a
// NO_COLOR profile, so the whole layout (header badge, sidebar tree, editor,
// footer) is a deterministic snapshot.
func TestV49ConsoleFrameGolden(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	req := fieldReq{
		kind: kindSelect, title: "Models", backable: true,
		ctx: viewCtx{
			breadcrumb: "remy > Models", target: "pipecat",
			sidebar: []sideItem{
				{label: "Identity"}, {label: "Models", active: true},
				{label: "Listen", child: true}, {label: "Reason", child: true}, {label: "Speak", child: true},
				{label: "Behavior"}, {label: "Integrations"}, {label: "Lifecycle"},
			},
		},
		choices: []choice{
			{"Listen (STT)  ·  deepgram", "listen"},
			{"Reason (LLM)  ·  openai", "reason"},
			{"Speak (TTS)  ·  cartesia", "speak"},
			{"← Back", actionBack},
		},
	}
	got := renderField(t, 80, 24, req)
	golden := filepath.Join("testdata", "golden", "console_models_80x24.txt")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run `go test -run TestV49ConsoleFrameGolden -update`): %v", err)
	}
	if got != string(want) {
		t.Errorf("console frame changed; run with -update to refresh.\n--- got ---\n%s", got)
	}
}

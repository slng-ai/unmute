package tui

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/huh"
)

var updateAccessible = flag.Bool("update-accessible", false, "rewrite the accessible transcript golden")

// The accessible/headless console is the path a screen reader drives, and until
// this file existed it was covered only by substring assertions ("does the
// output contain :back?"). Those pass on output that has lost its spacing, its
// ordinals, or its ordering, which is exactly what a rewrite of the prompt loop
// is most likely to change.
//
// So the whole transcript is pinned. When the renderer behind these calls is
// replaced, this golden is the acceptance test: it must reproduce byte for
// byte, and refreshing it during that work is the failure rather than the fix.
//
// The flag is `-update-accessible` rather than `-update` because the package
// already owns `-update` for the interactive frame golden, and one flag that
// rewrites both would let a careless refresh of one silently rewrite the other.

// accessibleCase is one screen driven end to end through the accessible runner.
type accessibleCase struct {
	name  string
	input string
	run   func(*fieldRunner) error
}

func accessibleCases() []accessibleCase {
	return []accessibleCase{
		{
			// Ordinal selection. The transcript has to show every option with a
			// stable selector, because that is what a screen reader reads out.
			name:  "select",
			input: "2\n",
			run: func(r *fieldRunner) error {
				_, _, err := r.selectOne("Models", "Pick the role to edit.", []huh.Option[string]{
					huh.NewOption("Listen (STT)  ·  deepgram", "listen"),
					huh.NewOption("Reason (LLM)  ·  openai", "reason"),
					huh.NewOption("← Back", actionBack),
				}, true)
				return err
			},
		},
		{
			name:  "select_not_backable",
			input: "1\n",
			run: func(r *fieldRunner) error {
				_, _, err := r.selectOne("Target", "", []huh.Option[string]{
					huh.NewOption("pipecat", "pipecat"),
					huh.NewOption("livekit", "livekit"),
				}, false)
				return err
			},
		},
		{
			name:  "input",
			input: "remy\n",
			run: func(r *fieldRunner) error {
				value := "assistant"
				_, err := r.input("Agent name", "Lowercase, no spaces.", &value, validateBasic)
				return err
			},
		},
		{
			// The escape has to be stated in the prompt, not merely accepted.
			name:  "input_back",
			input: ":back\n",
			run: func(r *fieldRunner) error {
				value := "assistant"
				_, err := r.input("Agent name", "Lowercase, no spaces.", &value, validateBasic)
				return err
			},
		},
		{
			name:  "text",
			input: "You are helpful.\n",
			run: func(r *fieldRunner) error {
				value := ""
				_, err := r.text("Instructions", "What the agent should do.", &value)
				return err
			},
		},
		{
			name:  "confirm",
			input: "1\n",
			run: func(r *fieldRunner) error {
				_, err := confirmChoice(r, "Delete variable caller_name?", "Delete")
				return err
			},
		},
	}
}

// TestAccessibleTranscriptGolden pins the full accessible transcript for every
// screen kind. Behaviour, not just the presence of a phrase.
func TestAccessibleTranscriptGolden(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var out strings.Builder
	for _, tc := range accessibleCases() {
		var buf bytes.Buffer
		err := tc.run(newRunner(strings.NewReader(tc.input), &buf, true))
		fmt.Fprintf(&out, "=== %s (input %q)\n", tc.name, tc.input)
		out.WriteString(buf.String())
		if err != nil {
			fmt.Fprintf(&out, "\n[error] %v\n", err)
		}
		out.WriteString("\n--- end ---\n\n")
	}
	got := out.String()

	golden := filepath.Join("testdata", "golden", "accessible_transcript.txt")
	if *updateAccessible {
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
		t.Fatalf("read golden (run `go test ./internal/tui/ -run TestAccessibleTranscriptGolden -update-accessible`): %v", err)
	}
	if got != string(want) {
		t.Errorf("accessible transcript changed.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestAccessibleEndOfInputAborts covers the case the one-byte reader exists for:
// input that stops in the middle of a prompt. It must abort with the recorded
// message, and it must neither hang nor panic. A rewrite that reads ahead, or
// that treats end-of-input as an empty answer, fails here.
func TestAccessibleEndOfInputAborts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		run   func(*fieldRunner) error
	}{
		{"select", "", func(r *fieldRunner) error {
			_, _, err := r.selectOne("Models", "", []huh.Option[string]{
				huh.NewOption("Listen", "listen"),
				huh.NewOption("← Back", actionBack),
			}, true)
			return err
		}},
		{"input", "", func(r *fieldRunner) error {
			value := ""
			_, err := r.input("Agent name", "", &value, validateBasic)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			done := make(chan error, 1)
			go func() { done <- tc.run(newRunner(strings.NewReader(tc.input), io.Discard, true)) }()
			err := <-done
			if err == nil {
				t.Fatal("end of input was accepted as an answer; it must abort")
			}
			if !strings.Contains(err.Error(), "menu") && !errors.Is(err, huh.ErrUserAborted) {
				t.Errorf("abort error is not the recorded one: %v", err)
			}
		})
	}
}

// TestAccessibleScreenReaderAffordances is FR-007, asserted directly rather than
// left to ride on the byte-level golden. A golden proves the output did not
// change; this proves the output is usable, so a future rewrite cannot satisfy
// the golden by regenerating it and quietly drop the affordances.
func TestAccessibleScreenReaderAffordances(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	var buf bytes.Buffer
	_, _, err := newRunner(strings.NewReader("2\n"), &buf, true).selectOne(
		"Models", "Pick the role to edit.", []huh.Option[string]{
			huh.NewOption("Listen (STT)", "listen"),
			huh.NewOption("Reason (LLM)", "reason"),
			huh.NewOption("← Back", actionBack),
		}, true)
	if err != nil {
		t.Fatal(err)
	}
	transcript := buf.String()

	// Every option needs a selector the reader can say back. Without ordinals a
	// screen-reader user has no way to name the option they want.
	for _, want := range []string{"1.", "2.", "3."} {
		if !strings.Contains(transcript, want) {
			t.Errorf("list prompt omits the %q selector, so an option cannot be chosen by name:\n%s", want, transcript)
		}
	}
	for _, want := range []string{"Listen (STT)", "Reason (LLM)", "Back"} {
		if !strings.Contains(transcript, want) {
			t.Errorf("list prompt omits option label %q:\n%s", want, transcript)
		}
	}

	// The escape has to be stated, not merely accepted. A user who cannot see
	// the screen cannot discover an unadvertised keyword.
	var textBuf bytes.Buffer
	value := ""
	if _, err := newRunner(strings.NewReader(":back\n"), &textBuf, true).
		input("Agent name", "Lowercase.", &value, validateBasic); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textBuf.String(), actionBack) {
		t.Errorf("free-text prompt does not state the %q escape:\n%s", actionBack, textBuf.String())
	}
}

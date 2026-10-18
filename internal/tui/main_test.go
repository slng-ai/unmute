package tui

import (
	"context"
	"os"
	"testing"

	"github.com/slng-ai/unmute/internal/stateschema"
)

// TestMain reads state.py from the repository's recordings and gives every
// local tool an empty object in and out, so these tests need no Python. What
// they check is the files the console writes, not the types Python reads.
func TestMain(m *testing.M) {
	stateReader = recordedState{stateschema.RecordedInRepo()}
	os.Exit(m.Run())
}

type recordedState struct{ stateschema.Recorded }

func (recordedState) ReadTools(_ context.Context, _ string, _ []byte, refs []stateschema.ToolRef) (map[string]stateschema.ToolTypes, error) {
	types := make(map[string]stateschema.ToolTypes, len(refs))
	for _, ref := range refs {
		types[ref.Name] = stateschema.ToolTypes{
			Description: ref.Name,
			Input:       map[string]any{"type": "object", "properties": map[string]any{}},
			Output:      map[string]any{"type": "object", "properties": map[string]any{}},
		}
	}
	return types, nil
}

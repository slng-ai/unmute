package generate

import "testing"

func TestLiveKitToolArrayInputs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		items map[string]any
		want  string
	}{
		{"dishes", map[string]any{"type": "string"}, "list[str]"},
		{"quantities", map[string]any{"type": "integer"}, "list[int]"},
		{"records", map[string]any{"type": "object"}, "list[dict]"},
		{"nested", map[string]any{"type": "array", "items": map[string]any{"type": "number"}}, "list[list[float]]"},
		{"untyped", nil, "list"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := livekitToolArgs(map[string]any{
				"properties": map[string]any{"items": map[string]any{"type": "array", "items": tc.items}},
				"required":   []any{"items"},
			})
			if len(args) != 1 || args[0].Anno != tc.want || !args[0].Required {
				t.Fatalf("array input: got %+v, want required %s", args, tc.want)
			}
		})
	}
}

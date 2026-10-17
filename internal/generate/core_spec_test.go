package generate

import (
	"slices"
	"testing"
)

func TestALineReadsWhatItsToolGivesIt(t *testing.T) {
	input := map[string][]string{"booking": {"booking"}, "policy_number": {"policy_number"}}
	result := map[string][]string{"appointment": {"appointment"}, "claim_number": {"filed", "number"}}
	for _, tc := range []struct{ line, want string }{
		{"Book {{booking__day}} at {{booking__time}}?", "Book {{input.booking.day}} at {{input.booking.time}}?"},
		{"Is it {{policy_number}}?", "Is it {{input.policy_number}}?"},
		{"Set for {{appointment__spoken}}.", "Set for {{result.appointment.spoken}}."},
		{"Your number is {{claim_number}}.", "Your number is {{result.filed.number}}."},
		{"Thanks, {{caller_name}}.", "Thanks, {{caller_name}}."},
		{"Call {{call__session_id}}.", "Call {{call.session_id}}."},
	} {
		if got := rewriteLines([]string{tc.line}, input, result)[0]; got != tc.want {
			t.Errorf("%q: got %q, want %q", tc.line, got, tc.want)
		}
	}
}

func TestAMatchTakesTheTypeTheToolDeclares(t *testing.T) {
	output := map[string]any{"properties": map[string]any{
		"found": map[string]any{"type": "boolean"}, "tries": map[string]any{"type": "integer"},
		"status": map[string]any{"type": "string"},
	}}
	got, err := typedMatch(output, map[string][]string{"found": {"true"}, "tries": {"2"}, "status": {"ok"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got["found"], []any{true}) || !slices.Equal(got["tries"], []any{2}) || !slices.Equal(got["status"], []any{"ok"}) {
		t.Fatalf("got %v", got)
	}
}

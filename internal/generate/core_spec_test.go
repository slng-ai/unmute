package generate

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
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

func TestAWebhookToolCompilesWithItsEndpointAndRefusesReplies(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "webhook_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := buildWithState(t, pkg)
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
		if err != nil {
			t.Fatalf("%s: %v", provider, err)
		}
		emitted := artifactFile(t, artifact, "agent.json")
		for _, want := range []string{`"url_env": "ORDERS_API_URL"`, `"path": "/orders"`, `"kind": "bearer"`, `"call": "{{call.session_id}}"`} {
			if !strings.Contains(emitted, want) {
				t.Errorf("%s: agent.json lacks %s", provider, want)
			}
		}
		report := artifactFile(t, artifact, "compile-report.json")
		for _, name := range []string{"ORDERS_API_URL", "ORDERS_API_TOKEN"} {
			if !strings.Contains(report, `"`+name+`"`) {
				t.Errorf("%s: required_env lacks %s", provider, name)
			}
		}
	}
	tool := agent.Tools["look_up_order"]
	tool.Replies = []ir.Reply{{When: map[string][]string{"status": {"shipped"}}, Say: []string{"It has shipped."}}}
	agent.Tools["look_up_order"] = tool
	if _, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default()); err == nil || !strings.Contains(err.Error(), "webhook tool takes neither") {
		t.Fatalf("a webhook with replies compiled, or said something else: %v", err)
	}
}

package generate

import (
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
)

// TestSalonConciergeV3HandsEverySeamItsRequest pins the typed-inputs
// verification package the way TestSalonConciergeV2ScopesEveryStep pins the
// control. v3 exists to prove that a step or a specialist handed what the
// caller asked for can run on `reset` without asking again, so a package that
// stops exercising a part of that fails here rather than passing quietly.
func TestSalonConciergeV3HandsEverySeamItsRequest(t *testing.T) {
	resolved := loadExample(t, "salon-concierge-v3")
	if resolved.Name != "salon-concierge-v3" {
		t.Errorf("name = %q, want salon-concierge-v3: two packages sharing a name are one deployment overwritten twice", resolved.Name)
	}

	// Every seam on reset. That is the point of the package: with inputs, a
	// reset step is no longer only for a step that needs nothing.
	for name, got := range map[string]ir.History{
		"verify_customer":  resolved.Tasks["verify_customer"].Context.History,
		"manage_booking":   resolved.Tasks["manage_booking"].Context.History,
		"handle_complaint": resolved.Tasks["handle_complaint"].Context.History,
		"to_complaints":    resolved.Controls["to_complaints"].(*ir.AgentTransfer).Context.History,
		"to_concierge":     resolved.Controls["to_concierge"].(*ir.AgentTransfer).Context.History,
	} {
		if got != ir.HistoryReset {
			t.Errorf("%s runs on history %q, want %q: this package hands every seam its request so none needs the conversation", name, got, ir.HistoryReset)
		}
	}

	// The inputs on the four seams, by name, type and optionality. Only the
	// action is required on the booking step: a caller cancelling names no day,
	// and a required value the caller never gave is one the model invents.
	type field struct {
		typ      string
		optional bool
	}
	want := map[string]map[string]field{
		"manage_booking": {
			"action":         {`Literal["create", "modify", "cancel"]`, false},
			"service":        {`Literal["haircut", "haircolor", "haircut_and_haircolor", "dry_cut"] | None`, true},
			"requested_day":  {"str | None", true},
			"requested_time": {"str | None", true},
		},
		"handle_complaint": {"problem": {"str", false}, "about": {"Appointment | None", true}},
		"to_complaints":    {"problem": {"str", false}, "about": {"Appointment | None", true}},
		"to_concierge":     {"outcome": {"str", false}, "next_request": {"str", false}},
	}
	inputsOf := func(site string) []ir.InputField {
		if task, ok := resolved.Tasks[site]; ok {
			return task.Inputs
		}
		return resolved.Controls[site].(*ir.AgentTransfer).Inputs
	}
	for site, fields := range want {
		got := inputsOf(site)
		if len(got) != len(fields) {
			t.Errorf("%s declares %d inputs, want %d: %+v", site, len(got), len(fields), got)
		}
		for _, input := range got {
			expect, ok := fields[input.Name]
			if !ok {
				t.Errorf("%s declares an input %q this gate does not know", site, input.Name)
				continue
			}
			if input.Type.String() != expect.typ || input.Optional != expect.optional {
				t.Errorf("%s input %q is %s (optional %v), want %s (optional %v)", site, input.Name, input.Type.String(), input.Optional, expect.typ, expect.optional)
			}
			if input.Description == "" {
				t.Errorf("%s input %q carries no description, so the agent filling it is told nothing", site, input.Name)
			}
		}
	}
	if got := resolved.Tasks["verify_customer"].Inputs; len(got) != 0 {
		t.Errorf("verify_customer declares inputs %v; reading a number back needs none", got)
	}

	// The caller-reason list and the per-step reason are gone: each appointment
	// and each complaint already records what was done.
	if _, declared := resolved.Variables["caller_reason"]; declared {
		t.Error("caller_reason is still declared; the appointments and complaints already record what was done")
	}
	for name, task := range resolved.Tasks {
		if _, has := task.Result["reason"]; has {
			t.Errorf("task %q still hands back a reason field", name)
		}
	}

	// The specialist's brief is the union of what reaches it, and the
	// concierge's is what comes back.
	if got := inputNamesOf(resolved.Agents["complaint_specialist"].Inputs); !slices.Equal(got, []string{"problem", "about"}) {
		t.Errorf("specialist brief = %v, want problem and about", got)
	}
	if got := inputNamesOf(resolved.Agents["concierge"].Inputs); !slices.Equal(got, []string{"outcome", "next_request"}) {
		t.Errorf("concierge brief = %v, want outcome and next_request", got)
	}

	// The local render path, which the router fixture cannot hold: this package
	// thinks straight at the provider, so on LiveKit the concierge's prompt is
	// rendered on entry and again after its own steps write state, from the
	// shared state that also holds its brief.
	livekit := emitted(t, resolved, ir.ProviderLiveKit)
	concierge := after(t, livekit, "class Concierge(")
	concierge = concierge[:strings.Index(concierge, "\nclass ")]
	if !strings.Contains(concierge, "async def _refresh_prompt(self) -> None:") {
		t.Error("livekit: the concierge has steps that write state and no _refresh_prompt, so its brief and its state would freeze on entry")
	}
	if !strings.Contains(concierge, "_render(CONCIERGE_PROMPT, self.session.userdata)") {
		t.Error("livekit: the concierge's prompt is not rendered from the shared state")
	}
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		module := emitted(t, resolved, provider)
		for _, want := range []string{
			`"manage_booking": {`, `"handle_complaint": {`, `"to_complaints": {`, `"to_concierge": {`,
			"1. Action: {{action}}", "1. Problem: {{problem}}", "1. Outcome: {{outcome}}",
		} {
			if !strings.Contains(module, want) {
				t.Errorf("%s: the emitted module does not carry %q", provider, want)
			}
		}
	}
}

func inputNamesOf(inputs []ir.InputField) []string {
	names := make([]string, 0, len(inputs))
	for _, input := range inputs {
		names = append(names, input.Name)
	}
	return names
}

package ir

import (
	"strings"
	"testing"

	targetcap "github.com/slng-ai/unmute/internal/target"
)

// A per-tool dependency list reaches SLNG's code-tool environment and reaches
// nothing at all on a code target, whose driver builds one project-wide list out
// of the provider catalogue. Refused there rather than dropped.
func TestToolDependenciesAreSlngOnly(t *testing.T) {
	withDeps := func(t *testing.T) *Agent {
		t.Helper()
		agent := slngAgent(t)
		agent.Tools["check_order"] = Tool{
			Description: "Look one up.", Execution: ToolLocal,
			Handler: "tools/check_order.py", HandlerSource: "def check_order() -> dict:\n    return {}\n",
			Dependencies: []string{"orjson==3.11.4"},
			Input:        map[string]any{"type": "object", "properties": map[string]any{}},
			Interruption: ToolProviderDefault, Effect: ToolReturnsData,
		}
		entry := agent.Agents["support"]
		entry.Tools = append(entry.Tools, "check_order")
		agent.Agents["support"] = entry
		return agent
	}

	if row := validateSlng(t, withDeps(t)); len(row.Errors) > 0 {
		t.Errorf("a pinned dependency was refused on slng, which installs one: %#v", row.Errors)
	}

	agent := withDeps(t)
	target := targetFor(agent, ProviderSlng)
	target.Provider, target.Name, target.Version = ProviderLiveKit, "livekit", "1.6.10"
	report, err := Validate(agent, []Target{target}, targetcap.Default())
	if err == nil {
		t.Fatal("a per-tool dependency passed on livekit, whose driver reads none")
	}
	joined := strings.Join(reportFor(report, ProviderLiveKit).Errors, "\n")
	for _, want := range []string{"reads no per-tool pins", "pyproject.toml", "compile to slng"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the refusal does not say %q:\n%s", want, joined)
		}
	}
}

// The pin shape is checked wherever it is declared, because a range or a URL is
// not a dependency on any platform. That keeps the message about the pin rather
// than about the target that happened to be validated first.
func TestMalformedDependencyIsRefusedOnEveryTarget(t *testing.T) {
	for _, test := range []struct{ pin, want string }{
		{"orjson>=3.11", "not an exact pin"},
		{"orjson @ https://example.com/o.whl", "no URL"},
		{"orjson==3.*", "no wildcard"},
		{"orjson==v3.11.4", "in the form SLNG stores"},
	} {
		agent := slngAgent(t)
		agent.Tools["check_order"] = Tool{
			Description: "Look one up.", Execution: ToolLocal,
			Handler: "tools/check_order.py", HandlerSource: "def check_order() -> dict:\n    return {}\n",
			Dependencies: []string{test.pin},
			Input:        map[string]any{"type": "object", "properties": map[string]any{}},
			Interruption: ToolProviderDefault, Effect: ToolReturnsData,
		}
		entry := agent.Agents["support"]
		entry.Tools = append(entry.Tools, "check_order")
		agent.Agents["support"] = entry
		row := validateSlng(t, agent)
		joined := strings.Join(row.Errors, "\n")
		if !strings.Contains(joined, test.want) {
			t.Errorf("pin %q: no error contains %q; got %#v", test.pin, test.want, row.Errors)
		}
		if !strings.Contains(joined, `tool "check_order" dependency`) {
			t.Errorf("pin %q: the message does not name the tool: %#v", test.pin, row.Errors)
		}
	}
}

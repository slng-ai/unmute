package ir

import (
	"path/filepath"
	"strings"
	"testing"

	packagespec "github.com/slng/unmute/internal/spec"
)

func TestBuildSafeCore(t *testing.T) {
	agent, err := Build(loadSafeCore(t))
	if err != nil {
		t.Fatal(err)
	}
	if agent.EntryAgent != "intake" || len(agent.Targets) != 5 {
		t.Fatalf("unexpected IR: entry=%q targets=%d", agent.EntryAgent, len(agent.Targets))
	}
	if agent.Language != "en" {
		t.Fatalf("language = %q", agent.Language)
	}
	if !strings.Contains(agent.Agents["intake"].Instructions, "front desk") {
		t.Fatal("prompt path was not composed")
	}
	if _, ok := agent.Controls["to_billing"].(*AgentTransfer); !ok {
		t.Fatalf("control union = %T", agent.Controls["to_billing"])
	}
	if agent.Tools["lookup_customer"].Effect != ToolReturnsData {
		t.Fatal("tool defaults were not applied")
	}
}

func TestBuildDefaultsLanguage(t *testing.T) {
	pkg := loadSafeCore(t)
	pkg.Agent.Language = ""
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if agent.Language != "en" {
		t.Fatalf("language = %q", agent.Language)
	}
}

func TestBuildReportsUnresolvedReferenceAtSource(t *testing.T) { // V1
	pkg := loadSafeCore(t)
	intake := pkg.Agent.Agents["intake"]
	intake.Model = "missing_model"
	pkg.Agent.Agents["intake"] = intake
	_, err := Build(pkg)
	if err == nil || !strings.Contains(err.Error(), "agent.yaml") || !strings.Contains(err.Error(), "missing_model") {
		t.Fatalf("got %v", err)
	}
}

func TestBuildRejectsBadAndCollidingNames(t *testing.T) { // V7
	tests := []struct {
		name   string
		mutate func(*packagespec.Package)
		want   string
	}{
		{
			name: "reserved underscore",
			mutate: func(pkg *packagespec.Package) {
				pkg.Agent.Models["_private"] = packagespec.ModelDef{Provider: "openai", Model: "x"}
			},
			want: "lowercase snake_case",
		},
		{
			name: "tool control collision",
			mutate: func(pkg *packagespec.Package) {
				destination, mode := "billing_line", "cold"
				pkg.Agent.Controls["lookup_customer"] = packagespec.Control{Kind: "human_transfer", Destination: &destination, Mode: &mode}
			},
			want: "collide",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pkg := loadSafeCore(t)
			test.mutate(pkg)
			_, err := Build(pkg)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestBuildRejectsFieldsFromAnotherControlKind(t *testing.T) { // V3
	pkg := loadSafeCore(t)
	control := pkg.Agent.Controls["to_billing"]
	control.Task = new(string)
	pkg.Agent.Controls["to_billing"] = control
	_, err := Build(pkg)
	if err == nil || !strings.Contains(err.Error(), `field "task" is illegal with control kind "agent_transfer"`) {
		t.Fatalf("got %v", err)
	}
}

func TestBuildFlattensAndRejectsFallbackCycles(t *testing.T) { // V10
	pkg := loadSafeCore(t)
	fast := pkg.Agent.Models["fast_reasoning"]
	fast.Fallback = []string{"careful_reasoning"}
	pkg.Agent.Models["fast_reasoning"] = fast
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if got := agent.Models["fast_reasoning"].Fallback; len(got) != 1 || got[0] != "careful_reasoning" {
		t.Fatalf("flattened fallback = %v", got)
	}

	careful := pkg.Agent.Models["careful_reasoning"]
	careful.Fallback = []string{"fast_reasoning"}
	pkg.Agent.Models["careful_reasoning"] = careful
	_, err = Build(pkg)
	if err == nil || !strings.Contains(err.Error(), "fallback cycle") {
		t.Fatalf("got %v", err)
	}
}

func TestBuildEnforcesModelReferenceContract(t *testing.T) { // V22
	tests := []struct {
		name   string
		mutate func(*packagespec.Package)
		want   string
	}{
		{
			name: "dead declaration",
			mutate: func(pkg *packagespec.Package) {
				pkg.Agent.Models["orphan"] = packagespec.ModelDef{Provider: "openai", Model: "gpt-4o"}
			},
			want: "defined but never referenced",
		},
		{
			name: "kind conflict",
			mutate: func(pkg *packagespec.Package) {
				intake := pkg.Agent.Agents["intake"]
				intake.Voice = "fast_reasoning" // also referenced as a think model
				pkg.Agent.Agents["intake"] = intake
			},
			want: "referenced as both",
		},
		{
			name: "unknown override key",
			mutate: func(pkg *packagespec.Package) {
				target := pkg.Targets["pipecat"]
				target.Models["ghost"] = packagespec.ModelDef{Provider: "openai", Model: "gpt-4o"}
				pkg.Targets["pipecat"] = target
			},
			want: "not a defined model",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pkg := loadSafeCore(t)
			test.mutate(pkg)
			_, err := Build(pkg)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestBuildValidatesDestinationValues(t *testing.T) {
	for _, test := range []struct {
		value string
		valid bool
	}{
		{"+14155550123", true},
		{"sip:billing@example.com", true},
		{"sips:billing@example.com", true},
		{"", false},
		{"billing@example.com", false},
		{"not-a-phone", false},
	} {
		if got := validDestination(test.value); got != test.valid {
			t.Errorf("validDestination(%q) = %t", test.value, got)
		}
	}

	pkg := loadSafeCore(t)
	target := pkg.Targets["livekit"]
	target.Destinations["billing_line"] = ""
	pkg.Targets["livekit"] = target
	_, err := Build(pkg)
	if err == nil || !strings.Contains(err.Error(), "E.164 phone number or SIP URI") {
		t.Fatalf("got %v", err)
	}
}

func loadSafeCore(t *testing.T) *packagespec.Package {
	t.Helper()
	pkg, err := packagespec.Load(filepath.Join("..", "..", "examples", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

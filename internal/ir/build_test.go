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
				pkg.Agent.Models["_private"] = packagespec.ModelProfile{Placement: "api"}
			},
			want: "lowercase snake_case",
		},
		{
			name: "tool control collision",
			mutate: func(pkg *packagespec.Package) {
				pkg.Agent.Controls["lookup_customer"] = packagespec.Control{Kind: "human_transfer", Destination: "billing_line", Mode: "cold"}
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

func loadSafeCore(t *testing.T) *packagespec.Package {
	t.Helper()
	pkg, err := packagespec.Load(filepath.Join("..", "..", "examples", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

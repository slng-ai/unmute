package generate

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// A warning, never a refusal, and never silence.
//
// Silence is what made this a bug worth gating: the author wrote two params, the
// package validated clean, and one target sent them to an endpoint that has no
// such fields. Refusing is not the answer either, because one think binding
// serving both targets is the shape this change exists to allow.
func TestResponsesOnlyParamsWarnOnTheTargetThatCannotUseThem(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "responses_params"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := buildWithState(t, pkg)
	if err != nil {
		t.Fatal(err)
	}
	var declared []ir.Target
	for _, name := range slices.Sorted(maps.Keys(agent.Targets)) {
		declared = append(declared, agent.Targets[name])
	}
	report, err := ir.Validate(agent, declared, target.Default())
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	for _, row := range report.PerTarget {
		warnings := strings.Join(row.Warnings, "\n")
		if len(row.Errors) > 0 {
			t.Errorf("target %q reports errors, and these params must never refuse a package: %v", row.Name, row.Errors)
		}
		switch row.Provider {
		case ir.ProviderLiveKit:
			for _, name := range ir.ResponsesOnlyParams {
				if strings.Contains(warnings, "params."+name) {
					t.Errorf("target %q warns about params.%s, which it acts on", row.Name, name)
				}
			}
		case ir.ProviderPipecat:
			for _, name := range ir.ResponsesOnlyParams {
				if !strings.Contains(warnings, "params."+name) {
					t.Errorf("target %q does not warn that params.%s reaches nothing here: %v", row.Name, name, row.Warnings)
				}
			}
		}
	}
}

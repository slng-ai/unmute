package generate

import (
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/stateschema"
)

// walkTypes visits every type reachable from the agent's variables, each once
// per place it is reached.
func walkTypes(agent *ir.Agent, visit func(*stateschema.Type)) {
	var walk func(*stateschema.Type)
	walk = func(typ *stateschema.Type) {
		if typ == nil {
			return
		}
		visit(typ)
		walk(typ.Items)
		for _, field := range typ.Fields {
			walk(field.Type)
		}
	}
	for _, name := range agent.VariableOrder {
		walk(agent.Variables[name].Schema)
	}
}

// buildWithState is ir.Build for a loaded package: it reads the package's
// state.py from the repository's recorded reports first, which is the only way
// a default-suite test may read one (a real read needs uv).
func buildWithState(t testing.TB, pkg *spec.Package) (*ir.Agent, error) {
	t.Helper()
	if pkg.State == nil {
		if err := pkg.ReadState(t.Context(), stateschema.RecordedInRepo()); err != nil {
			return nil, err
		}
	}
	return ir.Build(pkg)
}

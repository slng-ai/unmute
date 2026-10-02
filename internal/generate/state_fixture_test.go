package generate

import (
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/stateschema"
)

// stringType and boolType are the result field types a fixture hands a task
// when it builds an ir.Task by hand rather than loading a package. A closed set
// of words makes the string one a Literal.
func stringType(enum ...string) *stateschema.Type {
	return &stateschema.Type{Kind: stateschema.KindString, Enum: enum}
}

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

// modelNamed finds a BaseModel by class name among the agent's variable types.
func modelNamed(agent *ir.Agent, name string) *stateschema.Type {
	var found *stateschema.Type
	walkTypes(agent, func(typ *stateschema.Type) {
		if found == nil && typ.Model == name && typ.Kind == stateschema.KindObject {
			found = typ
		}
	})
	return found
}

func objectType() *stateschema.Type {
	return &stateschema.Type{Kind: stateschema.KindObject}
}

func boolType() *stateschema.Type {
	return &stateschema.Type{Kind: stateschema.KindBoolean}
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

// addCarrierVariables declares the three values a dispatch or a carrier fills,
// on a package whose state.py does not hold them: the fields go onto the read
// model in memory, because editing state.py would need a new recording.
func addCarrierVariables(t testing.TB, pkg *spec.Package) {
	t.Helper()
	if err := pkg.ReadState(t.Context(), stateschema.RecordedInRepo()); err != nil {
		t.Fatal(err)
	}
	text := &stateschema.Type{Kind: stateschema.KindString}
	pkg.State.Fields = append(pkg.State.Fields,
		stateschema.Field{Name: "campaign_id", Type: text, Default: []byte(`"manual"`)},
		stateschema.Field{Name: "provider_call_id", Type: text, Default: []byte(`""`)},
		stateschema.Field{Name: "call_direction", Type: text, Default: []byte(`""`)},
	)
	pkg.Agent.Variables = append(pkg.Agent.Variables,
		spec.Variable{Name: "campaign_id", Source: "call_start"},
		spec.Variable{Name: "provider_call_id", Source: "call_id"},
		spec.Variable{Name: "call_direction", Source: "direction"},
	)
}

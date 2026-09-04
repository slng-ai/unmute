package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// typedStateMarkers is every distinctive line the declared-state block emits.
// Exhaustive on purpose: the byte-identical gate below asserts that a package
// declaring nothing structured carries none of them, so a marker missing from
// this list is a hole in that gate.
var typedStateMarkers = []string{
	"# --- declared state",
	"class _StateRefused",
	"def _typed(",
	"def _plain(",
	"def _typed_result(",
	"def _state_text(",
	"_FINISH_TYPES",
	"_STATE_STRUCTURED",
	"_STATE_EMPTY",
	"TypeAdapter(",
	"AfterValidator(",
	"BaseModel",
	"_SHAPE_PHONE",
	"_SHAPE_DATE",
	"_SHAPE_TIME",
	"_SHAPE_ID",
	"field(default_factory=list)",
}

func loadTypedState(t *testing.T) *ir.Agent {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "typed_state"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

func loadShapeless(t *testing.T) *ir.Agent {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

// emitted returns the module both drivers write, so a test asserts on the same
// question twice rather than once per target.
func emitted(t *testing.T, agent *ir.Agent, provider ir.Provider) string {
	t.Helper()
	artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
	if err != nil {
		t.Fatalf("generate %s: %v", provider, err)
	}
	if provider == ir.ProviderLiveKit {
		return artifactFile(t, artifact, "agent.py")
	}
	return artifactFile(t, artifact, "bot.py")
}

// TestTypedStateEmitsNothingForAPackageThatDeclaresNone is FR-015, and it is
// the only real protection every shipped example has from this feature.
//
// A package declaring no shape and no structured type must emit exactly what it
// emitted before, so the block, its constants, its imports and the list default
// appear only when something is authored. The golden files hold the byte
// comparison; this holds the reason a byte would change.
func TestTypedStateEmitsNothingForAPackageThatDeclaresNone(t *testing.T) {
	agent := loadShapeless(t)
	block, err := TypedState(agent)
	if err != nil {
		t.Fatal(err)
	}
	if block.Source != "" {
		t.Errorf("a package declaring nothing structured rendered a block:\n%s", block.Source)
	}
	if len(block.Structured) != 0 {
		t.Errorf("a package declaring nothing structured named %v as structured", block.Structured)
	}
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		module := emitted(t, agent, provider)
		for _, marker := range typedStateMarkers {
			if strings.Contains(module, marker) {
				t.Errorf("%s emits %q for a package declaring nothing structured", provider, marker)
			}
		}
		// The import lines the block needs must not appear either: an unused
		// import is a byte that changed, and on this tree it is also a lint
		// failure in the emitted project.
		for _, unwanted := range []string{"from pydantic import AfterValidator", "dataclass, field"} {
			if strings.Contains(module, unwanted) {
				t.Errorf("%s emits %q for a package declaring nothing structured", provider, unwanted)
			}
		}
	}
}

// TestTypedStateEmitsTheBlockWhenAuthored is the other half, and it is what
// stops the gate above passing because nothing is ever emitted.
func TestTypedStateEmitsTheBlockWhenAuthored(t *testing.T) {
	agent := loadTypedState(t)
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		module := emitted(t, agent, provider)
		for _, want := range []string{
			"# --- declared state",
			"class Appointment(BaseModel):",
			"class _StateRefused(Exception):",
			"def _typed_result(step, values):",
			"_STATE_STRUCTURED = {",
			`Phone = Annotated[str, AfterValidator(_shape_phone)]`,
			"field(default_factory=list)",
		} {
			if !strings.Contains(module, want) {
				t.Errorf("%s does not emit %q", provider, want)
			}
		}
		// The shape's own fields, in declaration order, and the description the
		// model reads.
		if !strings.Contains(module, "scheduled_date: Date") {
			t.Errorf("%s does not annotate scheduled_date with its shaped type", provider)
		}
		// A text type nothing declares emits no alias and no pattern.
		if strings.Contains(module, "_SHAPE_ID") {
			t.Errorf("%s emits the Id alias, which this package never declares", provider)
		}
	}
}

// TestTypedStatePutsNoShapeKeywordInAnEmittedSchema is research section 20,
// and it fails in no other check.
//
// A `format` or a `pattern` in the schema the model is sent survives one
// target's strict converter, which is that target's default, and the provider
// rejects it. So it passes every local check and fails on the first real call.
// The shape lives in an AfterValidator, which contributes nothing to
// model_json_schema(), and that is what this holds.
func TestTypedStatePutsNoShapeKeywordInAnEmittedSchema(t *testing.T) {
	agent := loadTypedState(t)
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		// Comment lines are dropped first, the way the colour-literal gate reads
		// through the AST: the block's own comment explains why a pattern= is
		// never written, and a gate that could not tell an explanation from an
		// instance would forbid saying so.
		module := withoutComments(emitted(t, agent, provider))
		for _, keyword := range []string{`"format"`, `"pattern"`, "StringConstraints", "pattern=", "format="} {
			if strings.Contains(module, keyword) {
				t.Errorf("%s emits %s; one target's strict converter keeps it and the provider rejects it",
					provider, keyword)
			}
		}
	}
	// And the patterns themselves are raw-string safe, because a pattern that
	// needs escaping would compile and then match the wrong thing.
	for kind, pattern := range ShapedPatterns() {
		if !RawStringSafe(pattern) {
			t.Errorf("the %s pattern %q cannot be written as a Python raw string", kind, pattern)
		}
	}
}

// TestTypedStateCarriesEveryDeclaredDescription is FR-014, which nothing else
// asserts: a field's description reaches the model, so the model knows what the
// field means without the author repeating it in prose.
func TestTypedStateCarriesEveryDeclaredDescription(t *testing.T) {
	agent := loadTypedState(t)
	var wanted []string
	for _, shape := range agent.Shapes {
		for _, field := range shape.Fields {
			if field.Description != "" {
				wanted = append(wanted, field.Description)
			}
		}
	}
	if len(wanted) == 0 {
		t.Fatal("the fixture declares no field description, so this gate proves nothing")
	}
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		module := emitted(t, agent, provider)
		for _, description := range wanted {
			if !strings.Contains(module, description) {
				t.Errorf("%s does not carry the field description %q into the schema", provider, description)
			}
			if !strings.Contains(module, "Field(description=") {
				t.Errorf("%s carries no Field(description=...), so no description reaches the model", provider)
			}
		}
	}
	// A shape's own description reaches the model as the class docstring.
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		if !strings.Contains(emitted(t, agent, provider), `"""One thing being booked, moved or cancelled."""`) {
			t.Errorf("%s drops the shape's own description", provider)
		}
	}
}

// TestTypedStateBlockIsByteIdenticalOnBothTargets is FR-006 where it is
// cheapest to hold: the declared-state block is rendered once, in shapes.go,
// and inserted into both modules verbatim. Rendering it twice is how the two
// targets would drift, and this is what notices.
func TestTypedStateBlockIsByteIdenticalOnBothTargets(t *testing.T) {
	agent := loadTypedState(t)
	block, err := TypedState(agent)
	if err != nil {
		t.Fatal(err)
	}
	if block.Source == "" {
		t.Fatal("the fixture rendered no block, so this gate proves nothing")
	}
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		if !strings.Contains(emitted(t, agent, provider), block.Source) {
			t.Errorf("%s does not carry the rendered block verbatim, so the two targets can differ", provider)
		}
	}
}

// withoutComments drops every full-line Python comment, so a gate over emitted
// code does not fire on emitted prose about that code.
func withoutComments(module string) string {
	var kept []string
	for _, line := range strings.Split(module, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

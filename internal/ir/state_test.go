package ir

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	packagespec "github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/stateschema"
)

// loadRecorded is spec.Load followed by reading state.py through the recorded
// reports, so no test needs Python. A package with no state.py loads as before.
func loadRecorded(dir string) (*packagespec.Package, error) {
	pkg, err := packagespec.Load(dir)
	if err != nil {
		return nil, err
	}
	if err := pkg.ReadState(context.Background(), stateschema.RecordedInRepo()); err != nil {
		return nil, err
	}
	return pkg, nil
}

// Scalar, list and model types for synthetic state, built the way the Pydantic
// reader reports them.
var (
	stringType = &stateschema.Type{Kind: stateschema.KindString}
	boolType   = &stateschema.Type{Kind: stateschema.KindBoolean}
)

func nullable(typ *stateschema.Type) *stateschema.Type {
	copied := *typ
	copied.Nullable = true
	return &copied
}

func listOf(typ *stateschema.Type) *stateschema.Type {
	return &stateschema.Type{Kind: stateschema.KindArray, Items: typ}
}

// optionalField is a `name: <type> | None = None` field.
func optionalField(name string, typ *stateschema.Type) stateschema.Field {
	return stateschema.Field{Name: name, Type: nullable(typ), Default: json.RawMessage("null")}
}

// stringField is a `name: str = ""` field.
func stringField(name string) stateschema.Field {
	return stateschema.Field{Name: name, Type: stringType, Default: json.RawMessage(`""`)}
}

// withState gives a package a state model and the source that stands for it.
func withState(pkg *packagespec.Package, fields ...stateschema.Field) {
	pkg.State = &stateschema.Model{Fields: fields}
	pkg.StateSource = []byte("class State(BaseModel): ...\n")
}

// addState adds fields to a package's existing model, replacing one of the
// same name.
func addState(pkg *packagespec.Package, fields ...stateschema.Field) {
	if pkg.State == nil {
		withState(pkg, fields...)
		return
	}
	for _, field := range fields {
		replaced := false
		for i := range pkg.State.Fields {
			if pkg.State.Fields[i].Name == field.Name {
				pkg.State.Fields[i], replaced = field, true
			}
		}
		if !replaced {
			pkg.State.Fields = append(pkg.State.Fields, field)
		}
	}
}

func TestBuildStateRefusals(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*packagespec.Package)
		want   string
	}{
		{
			name: "a field with no default",
			mutate: func(pkg *packagespec.Package) {
				withState(pkg, stateschema.Field{Name: "note", Type: stringType, Required: true})
			},
			want: "State.note has no default",
		},
		{
			name: "an entry naming no field",
			mutate: func(pkg *packagespec.Package) {
				withState(pkg, stringField("note"))
				pkg.Agent.Variables = packagespec.Variables{{Name: "nope", Source: "conversation"}}
			},
			want: `variables: names "nope", which State in state.py does not declare. It declares note`,
		},
		{
			name: "an entry with neither source nor confirm",
			mutate: func(pkg *packagespec.Package) {
				withState(pkg, stringField("note"))
				pkg.Agent.Variables = packagespec.Variables{{Name: "note"}}
			},
			want: `the variables: entry for "note" says nothing`,
		},
		{
			name: "a duplicate entry",
			mutate: func(pkg *packagespec.Package) {
				withState(pkg, stringField("note"))
				pkg.Agent.Variables = packagespec.Variables{{Name: "note", Source: "conversation"}, {Name: "note", Source: "conversation"}}
			},
			want: `lists "note" twice`,
		},
		{
			name: "shapes still present",
			mutate: func(pkg *packagespec.Package) {
				withState(pkg, stringField("note"))
				pkg.Agent.Shapes = []any{}
			},
			want: "shapes: is retired",
		},
		{
			name: "variables with no state.py",
			mutate: func(pkg *packagespec.Package) {
				pkg.State, pkg.StateSource = nil, nil
				pkg.Agent.Variables = packagespec.Variables{{Name: "note", Source: "conversation"}}
			},
			want: "this package has no state.py to declare it in",
		},
		{
			name: "state.py never read",
			mutate: func(pkg *packagespec.Package) {
				pkg.State, pkg.StateSource = nil, []byte("x")
			},
			want: "state.py was never read",
		},
		{
			name: "a reserved name",
			mutate: func(pkg *packagespec.Package) {
				withState(pkg, stringField("model_config"))
			},
			want: "a name the generated state class keeps for itself",
		},
		{
			name: "a builtin type name",
			mutate: func(pkg *packagespec.Package) {
				withState(pkg, stringField("str"))
			},
			want: "a name the generated state class keeps for itself",
		},
		{
			name: "the state prefix itself",
			mutate: func(pkg *packagespec.Package) {
				withState(pkg, stringField("state"))
			},
			want: "a name the generated state class keeps for itself",
		},
		{
			name: "a name that is not lowercase words",
			mutate: func(pkg *packagespec.Package) {
				withState(pkg, stringField("Caller__Name"))
			},
			want: "not a name a prompt can carry",
		},
	}
	// Every name the generated class defines is refused, so a field cannot
	// silently replace one of its methods.
	for _, member := range CallStateMembers {
		tests = append(tests, struct {
			name   string
			mutate func(*packagespec.Package)
			want   string
		}{
			name:   "a generated class member " + member,
			mutate: func(pkg *packagespec.Package) { withState(pkg, stringField(member)) },
			want:   "State." + member + " has a name the generated state class keeps for itself",
		})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pkg := loadSafeCore(t)
			pkg.Agent.Variables = nil
			test.mutate(pkg)
			_, err := Build(pkg)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Build error = %v, want it to contain %q", err, test.want)
			}
		})
	}
}

func TestBuildStateCarriesSourceAndConfirm(t *testing.T) {
	pkg := loadSafeCore(t)
	pkg.Agent.Variables = packagespec.Variables{{Name: "note", Source: "conversation"}}
	withState(pkg, stringField("note"))
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	variable := agent.Variables["note"]
	if variable.Source != "conversation" || variable.Type != PrimitiveString || variable.Schema == nil {
		t.Errorf("variable = %+v", variable)
	}
	if agent.State == nil || agent.StateSource == "" {
		t.Error("the agent carries no state model")
	}
}

func TestStateFieldNeedsThePrefix(t *testing.T) {
	pkg := loadSafeCore(t)
	withState(pkg, stringField("note"))
	pkg.Markdown["instructions.md"] += "\nThe note is {{note}}.\n"
	_, err := Build(pkg)
	if err == nil || !strings.Contains(err.Error(), "names a value on State; write {{state.note}}") {
		t.Fatalf("bare placeholder: %v", err)
	}
	pkg = loadSafeCore(t)
	withState(pkg, stringField("note"))
	pkg.Markdown["instructions.md"] += "\nThe note is {{state.note}}.\n"
	if _, err := Build(pkg); err != nil {
		t.Fatalf("prefixed placeholder refused: %v", err)
	}
	pkg = loadSafeCore(t)
	withState(pkg, stringField("note"))
	pkg.Markdown["instructions.md"] += "\nThe note is {{state.nope}}.\n"
	if _, err := Build(pkg); err == nil || !strings.Contains(err.Error(), "does not declare") {
		t.Fatalf("unknown state name: %v", err)
	}
}

// declareVariable adds a State field and the variables: entry that gives it a
// source, replacing an entry already there.
func declareVariable(pkg *packagespec.Package, field stateschema.Field, source string) {
	addState(pkg, field)
	pkg.Agent.Variables = withEntry(pkg.Agent.Variables, field.Name, func(v *packagespec.Variable) { v.Source = source })
}

// setConfirm sets the confirm: of one field's entry, adding the entry when the
// field has none.
func setConfirm(pkg *packagespec.Package, name, confirm string) {
	pkg.Agent.Variables = withEntry(pkg.Agent.Variables, name, func(v *packagespec.Variable) { v.Confirm = confirm })
}

func withEntry(entries packagespec.Variables, name string, change func(*packagespec.Variable)) packagespec.Variables {
	for i := range entries {
		if entries[i].Name == name {
			change(&entries[i])
			return entries
		}
	}
	entry := packagespec.Variable{Name: name}
	change(&entry)
	return append(entries, entry)
}

// A whole value the step assigns is required of a served finish; an appended
// entry and a field marked `?` are not. The `?` is the author's word that the
// step may finish without it.
func TestAssignOptionalDecidesWhetherAResultFieldIsRequired(t *testing.T) {
	typedState := filepath.Join("..", "testdata", "typed_state")
	pkg, err := loadRecorded(typedState)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	// An appended entry and a `?` field are optional; a whole value is not.
	result := agent.Tasks["book"].Result
	for field, want := range map[string]bool{"reason": false, "appointment": false} {
		if got := result[field].Required; got != want {
			t.Errorf("book result.%s Required = %v, want %v", field, got, want)
		}
	}
	// The same entry spelled with `?` is optional, and without it required.
	for _, tc := range []struct {
		value string
		want  bool
	}{{"result.caller_phone", true}, {"result.caller_phone?", false}} {
		pkg, err := loadRecorded(typedState)
		if err != nil {
			t.Fatal(err)
		}
		task := pkg.Tasks["confirm_number"]
		task.Assign = []packagespec.Pair{{Key: "caller_phone", Value: tc.value}}
		pkg.Tasks["confirm_number"] = task
		agent, err := Build(pkg)
		if err != nil {
			t.Fatal(err)
		}
		if got := agent.Tasks["confirm_number"].Result["caller_phone"].Required; got != tc.want {
			t.Errorf("assign %q: Required = %v, want %v", tc.value, got, tc.want)
		}
	}
}

// Two assigns of one result field into destinations of different kinds cannot
// both be right, and the compiler says so rather than picking one.
func TestAssignRefusesConflictingDestinations(t *testing.T) {
	pkg, err := loadRecorded(filepath.Join("..", "testdata", "typed_state"))
	if err != nil {
		t.Fatal(err)
	}
	task := pkg.Tasks["confirm_number"]
	task.Assign = []packagespec.Pair{{Key: "caller_phone", Value: "result.x"}, {Key: "count", Value: "result.x"}}
	pkg.Tasks["confirm_number"] = task
	if _, err := Build(pkg); err == nil || !strings.Contains(err.Error(), "conflicting destination types") {
		t.Fatalf("Build error = %v, want a conflicting destination refusal", err)
	}
}

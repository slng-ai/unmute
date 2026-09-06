package ir

import (
	"path/filepath"
	"strings"
	"testing"

	packagespec "github.com/slng-ai/unmute/internal/spec"
)

// TestFlattenPathsAndPathRoot holds the two forms of a path: authored with dots,
// emitted with double underscores, and the same root read from either.
func TestFlattenPathsAndPathRoot(t *testing.T) {
	if got := FlattenPaths("Hi {{ customer.status }}, {{$ACME_KEY}} {{note}} {{a.b.c}}"); got != "Hi {{ customer__status }}, {{$ACME_KEY}} {{note}} {{a__b__c}}" {
		t.Errorf("FlattenPaths = %q", got)
	}
	for ref, root := range map[string]string{
		"customer.status": "customer", "customer__status": "customer", "a.b.c": "a", "a__b__c": "a",
		"note": "note", "$ACME__KEY": "$ACME__KEY",
	} {
		if got := PathRoot(ref); got != root {
			t.Errorf("PathRoot(%q) = %q, want %q", ref, got, root)
		}
	}
	if got := PathFields("a__b__c"); strings.Join(got, "/") != "b/c" {
		t.Errorf("PathFields = %v", got)
	}
	if got := PathFields("note"); len(got) != 0 {
		t.Errorf("PathFields of a flat reference = %v, want none", got)
	}
}

// TestBuildResolvesAPlaceholderPath is spec 005 US1 and US2 at the compiler: a
// path into a declared shape is accepted and stored flat, and every wrong path
// is refused with the token as written and what to write instead. Every rule
// that holds for a whole value holds for the root of a path.
func TestBuildResolvesAPlaceholderPath(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(pkg *packagespec.Package)
		want   string
	}{
		{
			name: "a field of a shaped variable, stored in its emitted form",
			mutate: func(pkg *packagespec.Package) {
				pkg.Markdown["instructions.md"] += "\nThe last booking, if any, was on {{last_appointment.scheduled_date}}.\n"
			},
		},
		{
			name: "an unknown field lists the fields the shape declares",
			mutate: func(pkg *packagespec.Package) {
				pkg.Markdown["instructions.md"] += "\n{{last_appointment.kind}}\n"
			},
			want: `references {{last_appointment.kind}}: shape "Appointment" declares no field "kind". It declares scheduled_date, scheduled_time, appointment_type`,
		},
		{
			name: "a path through a list names the root and points at assign",
			mutate: func(pkg *packagespec.Package) {
				pkg.Markdown["instructions.md"] += "\n{{appointments.scheduled_date}}\n"
			},
			want: "references {{appointments.scheduled_date}}: appointments is list[Appointment], and a path cannot name a field inside a list: nothing says which entry it means. Record the entry you need into its own variable with assign:",
		},
		{
			name: "a plain variable has no fields",
			mutate: func(pkg *packagespec.Package) {
				pkg.Agent.Variables["note"] = packagespec.Variable{Type: "string", Description: "A note."}
				pkg.Markdown["instructions.md"] += "\n{{note.first}}\n"
			},
			want: "references {{note.first}}: note is a plain string with no fields to name; write {{note}}",
		},
		{
			name: "a text type has no fields, in the one prompt that may name it",
			mutate: func(pkg *packagespec.Package) {
				pkg.Markdown["tasks/confirm_number.md"] += "\n{{caller_phone.digits}}\n"
			},
			want: "references {{caller_phone.digits}}: caller_phone is Phone, which has no fields to name; write {{caller_phone}}",
		},
		{
			name: "the confirm rule is about the root",
			mutate: func(pkg *packagespec.Package) {
				pkg.Markdown["instructions.md"] += "\n{{caller_phone.digits}}\n"
			},
			want: "references {{caller_phone.digits}}, which the caller has not confirmed yet",
		},
		{
			name: "the greeting rule is about the root",
			mutate: func(pkg *packagespec.Package) {
				pkg.Agent.Conversation.Greeting.Text = "Welcome back, your last visit was {{last_appointment.scheduled_date}}."
			},
			want: "references {{last_appointment.scheduled_date}}, which has no value when the prompt is built",
		},
		{
			name: "an undeclared root is the undeclared-name refusal, token as written",
			mutate: func(pkg *packagespec.Package) {
				pkg.Markdown["instructions.md"] += "\n{{custmer.status}}\n"
			},
			want: "references {{custmer.status}}, which is not a declared variable",
		},
		{
			name: "a placeholder carries no logic",
			mutate: func(pkg *packagespec.Package) {
				pkg.Markdown["instructions.md"] += "\n{{last_appointment.scheduled_date | upper}}\n"
			},
			want: `shape "Appointment" declares no field "scheduled_date | upper"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg, err := packagespec.Load(filepath.Join("..", "testdata", "typed_state"))
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(pkg)
			agent, err := Build(pkg)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("Build refused a legal path: %v", err)
				}
				got := agent.Agents["desk"].Instructions
				if !strings.Contains(got, "{{last_appointment__scheduled_date}}") || strings.Contains(got, "{{last_appointment.scheduled_date}}") {
					t.Errorf("the IR does not carry the path in its emitted form:\n%s", got)
				}
				return
			}
			if err == nil {
				t.Fatalf("Build accepted the path, want %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("refusal = %q\nwant it to contain %q", err.Error(), tc.want)
			}
			if !strings.Contains(err.Error(), "instructions.md") && !strings.Contains(err.Error(), "agent.yaml") && !strings.Contains(err.Error(), "confirm_number.md") {
				t.Errorf("refusal names no file: %q", err.Error())
			}
		})
	}
}

// TestBuildResolvesAPathIntoAnExpectedValue is the same rule for the other kind
// of root: a field of a brief renders in the prompt that was handed the brief
// and nowhere else, and its fields are checked against the declared shape.
func TestBuildResolvesAPathIntoAnExpectedValue(t *testing.T) {
	load := func(t *testing.T) *packagespec.Package {
		pkg, err := packagespec.Load(filepath.Join("..", "testdata", "typed_inputs"))
		if err != nil {
			t.Fatal(err)
		}
		return pkg
	}
	t.Run("the receiving prompt names a field of its brief", func(t *testing.T) {
		pkg := load(t)
		pkg.Markdown["agents/specialist.md"] += "\nThe thing named, if any, is {{thing.label}}.\n"
		agent, err := Build(pkg)
		if err != nil {
			t.Fatalf("Build refused a legal path into a brief: %v", err)
		}
		if got := agent.Agents["specialist"].Instructions; !strings.Contains(got, "{{thing__label}}") {
			t.Errorf("the IR does not carry the path flat:\n%s", got)
		}
	})
	t.Run("another prompt may not, and the refusal names the root", func(t *testing.T) {
		pkg := load(t)
		pkg.Markdown["instructions.md"] += "\n{{thing.label}}\n"
		_, err := Build(pkg)
		want := `references {{thing.label}}, and thing is a value task "do_thing" instructions and agent "specialist" instructions expects to be handed`
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("refusal = %v, want it to contain %q", err, want)
		}
	})
	t.Run("an unknown field of the brief lists the fields", func(t *testing.T) {
		pkg := load(t)
		pkg.Markdown["agents/specialist.md"] += "\n{{thing.size}}\n"
		_, err := Build(pkg)
		want := `references {{thing.size}}: shape "Thing" declares no field "size". It declares label, count`
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("refusal = %v, want it to contain %q", err, want)
		}
	})
}

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
		// flat is the emitted form an accepted path must be stored as, dotted
		// the authored form that must be gone. Per case, because each one names
		// its own path and a single expectation would pass on the fixture's
		// other placeholders.
		flat, dotted string
		want         string
	}{
		{
			name: "a field of a shaped variable, stored in its emitted form",
			mutate: func(pkg *packagespec.Package) {
				pkg.Markdown["instructions.md"] += "\nThe last booking, if any, was on {{last_appointment.scheduled_date}}.\n"
			},
			flat:   "{{last_appointment__scheduled_date}}",
			dotted: "{{last_appointment.scheduled_date}}",
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
			// A path into a shape the compiler supplies, which resolves through
			// the same catalog a declared shape does.
			name: "a field of a supplied shape",
			mutate: func(pkg *packagespec.Package) {
				pkg.Markdown["instructions.md"] += "\nThe address on file is {{booked_for.email}}.\n"
			},
			flat:   "{{booked_for__email}}",
			dotted: "{{booked_for.email}}",
		},
		{
			name: "an unknown field of a supplied shape lists what it declares",
			mutate: func(pkg *packagespec.Package) {
				pkg.Markdown["instructions.md"] += "\n{{booked_for.address}}\n"
			},
			want: `shape "NameEmail" declares no field "address"`,
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
				if !strings.Contains(got, tc.flat) || strings.Contains(got, tc.dotted) {
					t.Errorf("the IR does not carry %s as %s:\n%s", tc.dotted, tc.flat, got)
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

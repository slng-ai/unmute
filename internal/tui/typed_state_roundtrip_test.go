package tui

import (
	"github.com/slng-ai/unmute/internal/spec"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/scaffold"
)

// TestMaintainKeepsDeclaredShapes guards the same silent data loss as the
// agent-name and task-handoffs round-trips, over the key this feature added.
//
// `unmute maintain` rewrites agent.yaml from scaffold.Data, so a field that
// struct does not carry is a field the console deletes. Left undone, an author
// who opens the console on a package declaring `shapes:` is offered the
// deletion of every shape and of the types that name them, and the offer is
// made at exit 0.
//
// The console offers no editor for shapes, exactly as it offers none for
// `knowledge:` or for `name:`. Carrying them is not optional either way.
func TestMaintainKeepsDeclaredShapes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pkg")
	data := scaffold.Data{
		Name: "pkg", AgentName: "acme-salon",
		Instructions: "Take appointment calls for one salon.",
		Shapes: []scaffold.Shape{{
			Name:        "Appointment",
			Description: "One thing being booked, moved or cancelled.",
			Fields: []scaffold.ShapeField{
				{Name: "scheduled_date", Type: "Date"},
				{Name: "scheduled_time", Type: "Time"},
				{
					Name: "appointment_type", Type: `Literal["haircut", "dry_cut"]`,
					Description: "The service the caller asked for.",
				},
				// A description holding a colon, which is the shape that breaks a
				// plain YAML scalar. It is also the shape a real description
				// takes: every one in the verification package explains a value
				// by naming its cases after a colon.
				{
					Name: "status", Type: `Literal["existing", "created"]`,
					Description: "What the lookup found: existing for a record already there, created for a new one.",
				},
			},
		}},
		Variables: []scaffold.Variable{
			{Name: "appointments", Type: "list[Appointment]"},
			{Name: "caller_reason", Type: `list[Literal["create_booking", "cancel_booking"]]`},
		},
	}
	data.SetTarget("livekit")
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}

	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	// Filtered the way the precedents filter, and to the same words: a
	// synthetic fixture's instructions.md is re-rendered from a template, so it
	// always differs, and that is not what this test is about.
	for _, loss := range agent.losses {
		for _, about := range []string{"shape", "field", "type", "variable"} {
			if strings.Contains(loss, about) {
				t.Errorf("the console cannot preserve %q, so maintain would delete it", loss)
			}
		}
	}
	if len(agent.data.Shapes) != 1 {
		t.Fatalf("read back %d shapes, want 1: %+v", len(agent.data.Shapes), agent.data.Shapes)
	}
	shape := agent.data.Shapes[0]
	if shape.Name != "Appointment" || shape.Description == "" {
		t.Errorf("shape = %+v, want Appointment with its description", shape)
	}
	if len(shape.Fields) != 4 {
		t.Fatalf("shape declares %d fields, want 4: %+v", len(shape.Fields), shape.Fields)
	}
	// The long form survives as the long form, and the short form as the short
	// form: a description read back onto a field with no `description:` would be
	// invented, and one dropped from a field that has one is deleted prose the
	// model was reading.
	if shape.Fields[0] != (scaffold.ShapeField{Name: "scheduled_date", Type: "Date"}) {
		t.Errorf("first field = %+v, want the one-line form intact", shape.Fields[0])
	}
	if got := shape.Fields[2]; got.Description != "The service the caller asked for." {
		t.Errorf("third field = %+v, want its description carried", got)
	}
	// A typed `type:` is carried by the field that already existed, and this is
	// what proves the expression is not reshaped on the way through.
	for _, want := range []string{"list[Appointment]", `list[Literal["create_booking", "cancel_booking"]]`} {
		found := false
		for _, variable := range agent.data.Variables {
			if variable.Type == want {
				found = true
			}
		}
		if !found {
			t.Errorf("no variable read back with type %q: %+v", want, agent.data.Variables)
		}
	}
	// And the file the console would write on the way out still says it.
	again := filepath.Join(t.TempDir(), "again")
	if _, err := scaffold.Write(again, agent.data); err != nil {
		t.Fatal(err)
	}
	rewritten, err := os.ReadFile(filepath.Join(again, "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(rewritten)
	for _, want := range []string{"shapes:", "- name: Appointment", "- scheduled_date: Date", "type: list[Appointment]"} {
		if !strings.Contains(source, want) {
			t.Errorf("rewritten agent.yaml missing %q:\n%s", want, source)
		}
	}
}

func TestMaintainKeepsAssignmentPathsAndOrder(t *testing.T) {
	data := scaffold.Data{
		Name: "saved-record", AgentName: "saved-record", Instructions: "Handle appointments.",
		Shapes:    []scaffold.Shape{{Name: "Appointment", Fields: []scaffold.ShapeField{{Name: "date", Type: "Date"}}}},
		Variables: []scaffold.Variable{{Name: "appointments", Type: "list[Appointment]"}, {Name: "selected", Type: "Appointment | None"}, {Name: "date", Type: "Date | None"}},
		Tasks: []scaffold.Task{{Name: "choose", Agent: "assistant", When: "Choose an appointment", Instructions: "Choose an appointment.", History: "reset",
			Assign: []spec.Pair{{Key: "date", Value: "result.appointment.date"}, {Key: "appointments+", Value: "result.appointment"}, {Key: "selected", Value: "result.appointment"}}}},
	}
	data.SetTarget("livekit")
	root := filepath.Join(t.TempDir(), "package")
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	maintained, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(maintained.data.Tasks) != 1 || !reflect.DeepEqual(maintained.data.Tasks[0].Assign, data.Tasks[0].Assign) {
		t.Fatalf("assignment paths/order changed: %+v", maintained.data.Tasks)
	}
	for _, loss := range maintained.losses {
		if strings.Contains(loss, "assign") {
			t.Fatal(loss)
		}
	}
}

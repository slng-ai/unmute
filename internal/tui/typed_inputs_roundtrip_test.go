package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/scaffold"
)

// TestMaintainKeepsATasksInputs and the handoff twin below guard the same
// silent data loss the shapes round-trip guards, over the key this feature
// added: `unmute maintain` rewrites agent.yaml from scaffold.Data, so a field
// that struct does not carry is a field the console deletes at exit 0. Both
// authored forms go round, because the console writes a field back the way a
// shape's fields are written, one line without a description and a block with.
func TestMaintainKeepsATasksInputs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pkg")
	data := scaffold.Data{
		Name: "pkg", AgentName: "acme-salon",
		Instructions: "Take appointment calls for one salon.",
		Tasks: []scaffold.Task{{
			Name: "manage_booking", Instructions: "Book it.", Agent: "assistant",
			When: "The caller wants a booking.",
			Input: []scaffold.ShapeField{
				{Name: "action", Type: `Literal["create", "modify", "cancel"]`},
				{Name: "service", Type: `Literal["haircut", "dry_cut"] | None`, Description: "Only what the caller said: leave it out otherwise."},
			},
			Result: `{"summary": "string"}`, History: "reset",
		}},
	}
	data.SetTarget("livekit")
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, loss := range agent.losses {
		if strings.Contains(loss, "input") {
			t.Errorf("the console cannot preserve %q, so maintain would delete it", loss)
		}
	}
	var task *scaffold.Task
	for i := range agent.data.Tasks {
		if agent.data.Tasks[i].Name == "manage_booking" {
			task = &agent.data.Tasks[i]
		}
	}
	if task == nil {
		t.Fatal("the task did not survive the round trip at all")
	}
	if len(task.Input) != 2 {
		t.Fatalf("task inputs = %+v, want both carried through", task.Input)
	}
	if task.Input[0] != data.Tasks[0].Input[0] || task.Input[1] != data.Tasks[0].Input[1] {
		t.Errorf("task inputs = %+v, want %+v", task.Input, data.Tasks[0].Input)
	}
}

func TestMaintainKeepsAHandoffsInputs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pkg")
	data := scaffold.Data{
		Name: "pkg", AgentName: "acme-salon",
		Agents: []scaffold.Agent{{Name: "specialist", Instructions: "Handle complaints."}},
		Handoffs: []scaffold.Handoff{{
			Name: "to_specialist", Source: "assistant", To: "specialist", When: "A complaint.",
			Input: []scaffold.ShapeField{
				{Name: "problem", Type: "str"},
				{Name: "about", Type: "str | None", Description: "Which booking, if the caller named one."},
			},
			History: "reset",
		}},
	}
	data.SetTarget("livekit")
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, loss := range agent.losses {
		if strings.Contains(loss, "input") {
			t.Errorf("the console cannot preserve %q, so maintain would delete it", loss)
		}
	}
	if len(agent.data.Handoffs) != 1 {
		t.Fatalf("read back %d handoffs, want 1", len(agent.data.Handoffs))
	}
	got := agent.data.Handoffs[0].Input
	if len(got) != 2 || got[0] != data.Handoffs[0].Input[0] || got[1] != data.Handoffs[0].Input[1] {
		t.Errorf("handoff inputs = %+v, want %+v", got, data.Handoffs[0].Input)
	}
}

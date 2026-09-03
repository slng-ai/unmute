package tui

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/scaffold"
)

// TestMaintainKeepsTheAgentName guards the same silent data loss as the
// knowledge round-trip, over the one field where losing it costs a live agent.
//
// `unmute maintain` rewrites agent.yaml from scaffold.Data, so a field that
// struct does not carry is a field the console deletes. `name:` is the deployed
// agent's identity on SLNG, and a package that loses it either stops compiling
// for that target or, worse, gets a different name and pushes a second agent
// beside the running one.
//
// The console offers no editor for it, exactly as it offers none for
// `knowledge:`. Carrying it is not optional either way.
func TestMaintainKeepsTheAgentName(t *testing.T) {
	root := filepath.Join(t.TempDir(), "some-folder")
	data := scaffold.Data{Name: "some-folder", AgentName: "acme-support"}
	data.SetTarget("livekit")
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}

	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, loss := range agent.losses {
		if strings.Contains(loss, "name") {
			t.Errorf("the console cannot preserve %q, so maintain would delete it", loss)
		}
	}
	// Read back verbatim, and not from the folder: the folder is called
	// something else here on purpose, because inferring the name from it is the
	// mistake this field exists to end.
	if agent.data.AgentName != "acme-support" {
		t.Errorf("AgentName = %q, want the authored name", agent.data.AgentName)
	}
}

// TestMaintainKeepsATasksHandoffs guards the same silent data loss over the key
// this feature added.
//
// A task's handoffs used to ride its `tools:` list, so scaffold.Data carried
// them for free. Splitting the lists moved them onto their own key, and a
// scaffold.Task without a Handoffs field would have made `unmute maintain`
// delete the shipped salon package's `to_complaints` on the way out, quietly and
// at exit 0. The salon package is the fixture because it is the one that
// actually authors the shape.
func TestMaintainKeepsATasksHandoffs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pkg")
	data := scaffold.Data{
		Name: "pkg", AgentName: "acme-support",
		Agents:   []scaffold.Agent{{Name: "billing", Instructions: "Handle billing."}},
		Handoffs: []scaffold.Handoff{{Name: "to_billing", Source: "assistant", To: "billing", When: "Billing.", History: "full", AllVariables: true}},
		Tasks: []scaffold.Task{{
			Name: "collect", Instructions: "Collect the details.", Agent: "assistant",
			When: "Collect first.", Handoffs: []string{"to_billing"},
			Result: `{"done": "boolean"}`, History: "full",
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
		if strings.Contains(loss, "handoff") {
			t.Errorf("the console cannot preserve %q, so maintain would delete it", loss)
		}
	}
	var booking *scaffold.Task
	for i := range agent.data.Tasks {
		if agent.data.Tasks[i].Name == "collect" {
			booking = &agent.data.Tasks[i]
		}
	}
	if booking == nil {
		t.Fatal("the task did not survive the round trip at all")
	}
	if !slices.Contains(booking.Handoffs, "to_billing") {
		t.Errorf("task handoffs = %v, want to_billing carried through", booking.Handoffs)
	}
	// And it must not have leaked into tools:, which is where it used to live.
	if slices.Contains(booking.Tools, "to_billing") {
		t.Error("to_billing is in the task's tools: list; a handoff rides handoffs:")
	}
}

// TestMaintainKeepsATasksAnnounce guards the same silent data loss over
// another key this feature added.
//
// A task's announce: is the line it speaks as the step is entered, so the two
// model requests it takes to enter one are not silence. A scaffold.Task without
// an Announce field would have made `unmute maintain` delete the salon
// package's "Let me pull up the diary." on the way out, quietly and at exit 0.
func TestMaintainKeepsATasksAnnounce(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pkg")
	data := scaffold.Data{
		Name: "pkg", AgentName: "acme-support",
		Agents: []scaffold.Agent{{Name: "billing", Instructions: "Handle billing."}},
		Tasks: []scaffold.Task{{
			Name: "collect", Instructions: "Collect the details.", Agent: "assistant",
			When: "Collect first.", Announce: "One moment while I check.",
			Result: `{"done": "boolean"}`, History: "full",
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
		if strings.Contains(loss, "announce") {
			t.Errorf("the console cannot preserve %q, so maintain would delete it", loss)
		}
	}
	var collect *scaffold.Task
	for i := range agent.data.Tasks {
		if agent.data.Tasks[i].Name == "collect" {
			collect = &agent.data.Tasks[i]
		}
	}
	if collect == nil {
		t.Fatal("the task did not survive the round trip at all")
	}
	if collect.Announce != "One moment while I check." {
		t.Errorf("task announce = %q, want it carried through", collect.Announce)
	}
}

// TestMaintainKeepsATasksRequires guards the same silent data loss over the
// prerequisite guard itself.
//
// A task's requires: names the variables that must hold a value before the
// step may start; the driver refuses the step to the model rather than to the
// caller. A scaffold.Task without a Requires field would have made `unmute
// maintain` delete that guard on the way out, quietly and at exit 0, leaving
// the step free to run on a value nobody has confirmed.
func TestMaintainKeepsATasksRequires(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pkg")
	data := scaffold.Data{
		Name: "pkg", AgentName: "acme-support",
		Agents: []scaffold.Agent{{Name: "billing", Instructions: "Handle billing."}},
		Tasks: []scaffold.Task{{
			Name: "collect", Instructions: "Collect the details.", Agent: "assistant",
			When: "Collect first.", Requires: []string{"customer_phone"},
			Result: `{"done": "boolean"}`, History: "full",
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
		if strings.Contains(loss, "requires") {
			t.Errorf("the console cannot preserve %q, so maintain would delete it", loss)
		}
	}
	var collect *scaffold.Task
	for i := range agent.data.Tasks {
		if agent.data.Tasks[i].Name == "collect" {
			collect = &agent.data.Tasks[i]
		}
	}
	if collect == nil {
		t.Fatal("the task did not survive the round trip at all")
	}
	if !slices.Contains(collect.Requires, "customer_phone") {
		t.Errorf("task requires = %v, want customer_phone carried through", collect.Requires)
	}
}

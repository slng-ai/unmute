package ir

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	packagespec "github.com/slng-ai/unmute/internal/spec"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

const v2Package = "../testdata/v2_core"

func loadV2(t *testing.T) *packagespec.Package {
	t.Helper()
	pkg, err := loadRecorded(v2Package)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestV2Golden(t *testing.T) {
	agent, err := Build(loadV2(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(agent, allTargets(agent), targetcap.Default()); err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(agent, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "golden", "v2_core.json")
	if *updateCompilerGolden {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("v2 IR golden differs; run go test ./internal/ir -run TestV2Golden -update")
	}
}

func TestV2Refusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*packagespec.Package)
		want string
	}{
		{"a read-back reads the tool input", withTask(func(t *packagespec.Task) { t.Finish[0].Confirm = []string{"Book {{input.booking.day}}?"} }), `add "- booking: input.booking" under assign: of task "manage_booking", then write {{state.booking.day}}`},
		{"a read-back reads a result", withTask(func(t *packagespec.Task) { t.Finish[0].Confirm = []string{"Book {{result.appointment.spoken}}?"} }), "a read-back runs before the tool, so there is no result yet"},
		{"a tool reply reads its result", withTool("find_slots", func(t *packagespec.Tool) { t.Replies[0].Say = []string{"I have {{result.slots}}."} }), `add "- slots: result.slots" under assign: in tools/find_slots.yaml, then write {{state.slots}}`},
		{"a line names state without the prefix", withTask(func(t *packagespec.Task) { t.Finish[0].Say = []string{"Done, {{caller_name}}."} }), "a spoken line reads State as {{state.caller_name}}"},
		{"a line names a value State lacks", withTask(func(t *packagespec.Task) { t.Finish[0].Say = []string{"Done, {{state.nickname}}."} }), "which State in state.py does not declare"},
		{"an inject reads a call value the library does not carry", withTool("save_booking", func(t *packagespec.Tool) { t.Inject[0].Value = "{{call.room}}" }), "a call value is {{call.<field>}}, one of agent_id, session_id"},
		{"a reply waits for a value the tool never returns", withTool("find_slots", func(t *packagespec.Tool) { t.Replies[0].When[0].Values = []string{"gone"} }), "find_slots never returns status: gone"},
		{"a step reply names a tool outside the step", withTask(func(t *packagespec.Task) { t.Replies[0].Tool = "lookup_account" }), `a reply names tool "lookup_account", which the step does not list`},
		{"hidden names a field the tool never returns", withTool("find_slots", func(t *packagespec.Tool) { t.Hidden = []string{"secret"} }), `hidden names "secret", which find_slots never returns`},
		{"a tool assign reads a field the output lacks", withTool("find_slots", func(t *packagespec.Tool) { t.Assign[0].Value = "result.times" }), `has no field "times"`},
		{"a tool assign does not fit its variable", withTool("find_slots", func(t *packagespec.Tool) { t.Assign[0].Key = "caller_name" }), `does not fit variable "caller_name"`},
		{"an input assign reads an input the tool lacks", withTask(func(t *packagespec.Task) { t.Assign[0].Value = "input.order" }), "no tool the step finishes on takes that input (save_booking)"},
		{"opens_with passes a parameter the tool lacks", withTask(func(t *packagespec.Task) { t.OpensWith.Arguments[0].Key = "date" }), `opens_with passes "date", which find_slots does not take`},
		{"an inputs entry holds two keys", withTask(func(t *packagespec.Task) { t.Inputs[0].Tool = "account" }), "exactly one of memory:, tool: and input:"},
		{"via names a tool outside the step", withTask(func(t *packagespec.Task) { t.Via = []string{"lookup_account"} }), `via names "lookup_account"`},
		{"non_blocking names a tool outside the group", func(p *packagespec.Package) {
			group := p.Agent.TaskGroups["check_in"]
			group.NonBlocking = []string{"manage_booking"}
			p.Agent.TaskGroups["check_in"] = group
		}, `non_blocking names "manage_booking", which is not one of its steps`},
		{"a handoff saves a value State lacks", withHandoff(packagespec.Handoff{To: "desk", Saves: []packagespec.Pair{{Key: "nickname", Value: "x"}}}), `saves "nickname", which State in state.py does not declare`},
		{"a handoff opens with a tool the next agent lacks", withHandoff(packagespec.Handoff{To: "desk", OpensWith: &packagespec.OpensWith{Tool: "lookup_account"}}), `opens_with names "lookup_account", which agent "desk" does not list under tools`},
		{"mode is neither record nor status", func(p *packagespec.Package) {
			p.Agent.Mode = "fast"
		}, "mode: fast; write record or status"},
		{"call_start is neither required nor optional", func(p *packagespec.Package) {
			p.Agent.Variables[0].CallStart = "always"
		}, "call_start: always; write required or optional"},
		{"inject names no handler parameter", withTool("save_booking", func(t *packagespec.Tool) { t.Inject[0].Key = "shop" }), `inject "shop" names no parameter of save_booking`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg := loadV2(t)
			tc.edit(pkg)
			_, err := Build(pkg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v\nwant it to say %s", err, tc.want)
			}
		})
	}
}

// withTask edits the booking step. A map holds a copy, so the edit is written
// back.
func withTask(edit func(*packagespec.Task)) func(*packagespec.Package) {
	return func(p *packagespec.Package) {
		task := p.Tasks["manage_booking"]
		edit(&task)
		p.Tasks["manage_booking"] = task
	}
}

// withTool edits one tool file, the same way.
func withTool(name string, edit func(*packagespec.Tool)) func(*packagespec.Package) {
	return func(p *packagespec.Package) {
		tool := p.Tools[name]
		edit(&tool)
		p.Tools[name] = tool
	}
}

// withHandoff adds one handoff, back to the desk, and offers it from the desk.
func withHandoff(handoff packagespec.Handoff) func(*packagespec.Package) {
	return func(p *packagespec.Package) {
		p.Agent.Handoffs = map[string]packagespec.Handoff{"back": handoff}
		desk := p.Agent.Agents["desk"]
		desk.Handoffs = []string{"back"}
		p.Agent.Agents["desk"] = desk
	}
}

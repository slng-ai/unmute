package spec

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

// lowered decodes one step and lowers it, as Load does.
func lowered(t *testing.T, body string) (*Task, error) {
	t.Helper()
	var task Task
	if err := yaml.UnmarshalWithOptions([]byte(body), &task, yaml.Strict()); err != nil {
		t.Fatalf("decode: %v", err)
	}
	pkg := &Package{Tools: map[string]Tool{}}
	return &task, pkg.lowerTask(&task)
}

// A step's outcomes lower to the rules unmute-core runs: then: done and then:
// failed to finish rules, the rest to replies, in the order written.
func TestLowerTaskOutcomes(t *testing.T) {
	task, err := lowered(t, `name: book
tools:
  - name: book_it
    confirm:
      - Shall I book {{input.day}}?
    outcomes:
      - if:
          - status: booked
        save:
          - booking: result.booking
        then: done
      - if:
          - status: refused
        then: failed
      - if:
          - status: taken
        say:
          - That time just went.
      - else:
        say:
          - Something went wrong.
  - look_up
start:
  say:
    - One moment.
  listen: true
  call: look_up
  with:
    - day: state.requested_day?
    - shop: state.shop
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(task.Finish) != 2 || task.Finish[0].Ends != "completed" || task.Finish[1].Ends != "failed" {
		t.Fatalf("finish = %+v, want a done rule then a failed rule", task.Finish)
	}
	if got := task.Finish[0].Confirm; len(got) != 1 || len(task.Finish[1].Confirm) != 1 {
		t.Errorf("confirm = %v, want it on every rule of the tool", got)
	}
	if len(task.Replies) != 2 || len(task.Replies[0].When) != 1 || len(task.Replies[1].When) != 0 {
		t.Errorf("replies = %+v, want taken, then else with no match", task.Replies)
	}
	if len(task.Assign) != 1 || task.Assign[0].Key != "booking" {
		t.Errorf("assign = %+v, want the outcome's save as the step's declared set", task.Assign)
	}
	if strings.Join(task.Tools, ",") != "book_it,look_up" {
		t.Errorf("tools = %v", task.Tools)
	}
	if task.Opening != "listen" || len(task.Announce) != 1 {
		t.Errorf("opening = %q announce = %v, want listen and the say: line", task.Opening, task.Announce)
	}
	opens := task.OpensWith
	if opens == nil || opens.Tool != "look_up" || len(opens.Arguments) != 2 || opens.Arguments[0].Value != "requested_day" {
		t.Fatalf("opens_with = %+v, want look_up with State names", opens)
	}
	if strings.Join(opens.Optional, ",") != "day" {
		t.Errorf("optional = %v, want day only", opens.Optional)
	}
}

// Each shape the compiler cannot keep in the order it is written, or that
// would save what nobody meant, is refused with what to write instead.
func TestLowerTaskRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, tools, want string
	}{
		{"else not last", `
  - name: t
    outcomes:
      - else:
        say: [x]
      - if: [status: a]
        say: [y]`, "else: is the last outcome of t"},
		{"else done", `
  - name: t
    outcomes:
      - if: [status: a]
        then: done
      - else:
        then: done`, "A step completes only on a result an if: names"},
		{"else failed hides a reply", `
  - name: t
    outcomes:
      - if: [status: a]
        say: [x]
      - else:
        then: failed`, "this else: would hide that one"},
		{"ending after non-ending", `
  - name: t
    outcomes:
      - if: [status: a]
        say: [x]
      - if: [status: b]
        then: done`, "Write the outcomes that end the step first"},
		{"neither if nor else", `
  - name: t
    outcomes:
      - say: [x]`, "starts with if: or else:, and this one has neither"},
		{"an unknown then", `
  - name: t
    outcomes:
      - if: [status: a]
        then: stop`, "write done or failed"},
		{"a line-less reply", `
  - name: t
    outcomes:
      - if: [status: a]
        save: [x: result.x]`, "neither says a line nor ends the step"},
		{"confirm with nothing to end", `
  - name: t
    confirm: [Sure?]
    outcomes:
      - if: [status: a]
        say: [x]`, "has confirm: and no outcome with then: done"},
		{"a done outcome that saves beside one that does not", `
  - name: t
    outcomes:
      - if: [status: a]
        save: [x: result.x]
        then: done
      - if: [status: b]
        then: done`, "An outcome with no save: saves every value of the step"},
		{"package keys on a step tool", `
  - name: t
    hidden: [x]`, "go on the tool's entry in the top-level tools list"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := lowered(t, "name: s\ntools:"+tc.tools+"\n")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want it to say %q", err, tc.want)
			}
		})
	}
}

// A with: value reads State as state.<name>, and a start: with with: and no
// call: has nothing to pass it to.
func TestLowerStartRefusals(t *testing.T) {
	for _, tc := range []struct{ name, start, want string }{
		{"a bare name", "  call: t\n  with:\n    - day: requested_day", "it reads State as state.<name>"},
		{"with and no call", "  with:\n    - day: state.requested_day", "start: has with: and no call:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := lowered(t, "name: s\nstart:\n"+tc.start+"\n")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want it to say %q", err, tc.want)
			}
		})
	}
}

// A tool at the top of agent.yaml runs outside any step: its outcomes say
// lines, and save: on the entry saves on every result.
func TestLowerPackageOutcomes(t *testing.T) {
	replies, err := packageOutcomes(ToolEntry{Name: "grade", Outcomes: []Outcome{
		{If: []SuccessPair{{Field: "ok", Values: []string{"true"}}}, Say: []string{"Right."}},
		{Else: true, Say: []string{"Not quite."}, Save: []Pair{{Key: "hint", Value: "result.hint"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(replies) != 2 || len(replies[1].Save) != 1 {
		t.Fatalf("replies = %+v, want both, the else saving its hint", replies)
	}
	for _, tc := range []struct {
		name    string
		outcome Outcome
		want    string
	}{
		{"then outside a step", Outcome{If: []SuccessPair{{Field: "ok", Values: []string{"true"}}}, Say: []string{"x"}, Then: "done"}, "it has no step to end"},
		{"no line", Outcome{Else: true, Save: []Pair{{Key: "x", Value: "result.x"}}}, "write save: on the tool's entry"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := packageOutcomes(ToolEntry{Name: "t", Outcomes: []Outcome{tc.outcome}})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want it to say %q", err, tc.want)
			}
		})
	}
}

// An outcome's else: is a key with no value, so the decoder records that it
// was written, and refuses a value.
func TestOutcomeElseDecodes(t *testing.T) {
	var outcomes []Outcome
	if err := yaml.Unmarshal([]byte("- else:\n  say: [x]\n- if: [status: a]\n  say: [y]\n"), &outcomes); err != nil {
		t.Fatal(err)
	}
	if !outcomes[0].Else || outcomes[1].Else {
		t.Errorf("else = %v, %v; want true, false", outcomes[0].Else, outcomes[1].Else)
	}
	var bad []Outcome
	if err := yaml.Unmarshal([]byte("- else: done\n"), &bad); err == nil || !strings.Contains(err.Error(), "else: takes no value") {
		t.Errorf("else with a value = %v, want it refused", err)
	}
	written, err := yaml.Marshal(outcomes)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "else: null") && !strings.Contains(string(written), "else:\n") {
		t.Errorf("else did not survive a round trip:\n%s", written)
	}
}

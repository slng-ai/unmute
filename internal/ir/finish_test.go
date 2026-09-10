package ir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	packagespec "github.com/slng-ai/unmute/internal/spec"
)

// finishPackage writes the smallest package that holds a task, a tool with a
// declared output, and a variable to save into. Every case here is about what
// the compiler reads out of a real tool file, so the fixture writes one rather
// than building a Tool struct by hand.
func finishPackage(t *testing.T, variables, task, output string) *packagespec.Package {
	t.Helper()
	root := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("agent.yaml", `version: 1

name: finish-refusal

entry_agent: desk

secrets:
  - OPENAI_API_KEY

`+variables+`
agents:
  desk:
    instructions: instructions.md
    think: reasoning
    speak: voice
    tools:
      - look_up
    tasks:
`+task+`
tools:
  - book_it
  - look_up

models:
  think:
    reasoning:
      provider: openai
      model: gpt-4o-mini
  speak:
    voice:
      provider: openai
      model: tts-1
      voice: alloy
  listen:
    transcriber:
      provider: openai
      model: whisper-1
  turn:
    vad:
      provider: local
      model: silero

channels:
  web:
    kind: realtime_audio

capacity:
  peak_sessions: 5
  max_sessions: 10
  avg_session_duration: 5m
`)
	write("instructions.md", "# Desk\n\nTake appointment calls for one salon.\n")
	write("steps.md", "# Step\n\nBook the caller in.\n")
	write("targets.yaml", "targets:\n  livekit:\n    provider: livekit\n    version: \"1.6.10\"\n    sdk_language: python\n")
	write("tools/book_it.yaml", "description: Book one appointment.\n\ninput:\n  type: object\n  properties:\n    when:\n      type: string\n  required:\n    - when\n\noutput:\n"+output+"\nlocal:\n  handler: tools/salon.py\n\neffect: writes_data\n")
	write("tools/look_up.yaml", "description: Read the diary.\n\ninput:\n  type: object\n  properties: {}\n\noutput:\n  type: object\n  properties:\n    status:\n      type: string\n      enum:\n        - found\n  required:\n    - status\n\nlocal:\n  handler: tools/salon.py\n\neffect: returns_data\n")
	write("tools/salon.py", "def book_it(when: str) -> dict:\n    return {\"status\": \"booked\", \"reference\": when}\n\n\ndef look_up() -> dict:\n    return {\"status\": \"found\"}\n")
	pkg, err := packagespec.Load(root)
	if err != nil {
		t.Fatalf("the fixture itself does not load: %v", err)
	}
	return pkg
}

const finishOKOutput = `  type: object
  properties:
    status:
      type: string
      enum:
        - booked
        - slot_unavailable
    reference:
      type: string
  required:
    - status
    - reference
`

const finishVariables = `variables:
  booking_reference:
    type: str
    default: ""
    description: The reference of the booking just made.

`

// A step that ends on its tool compiles, and the resolved entry carries the
// tool and the values that mean success.
func TestBuildLowersFinish(t *testing.T) {
	pkg := finishPackage(t, finishVariables, `      - name: take_booking
        when: The caller wants an appointment.
        instructions: steps.md
        tools:
          - book_it
          - look_up
        finish:
          - tool: book_it
            success:
              - status: booked
        assign:
          - booking_reference: result.reference
`, finishOKOutput)
	agent, err := Build(pkg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	task := agent.Tasks["take_booking"]
	if len(task.Finish) != 1 {
		t.Fatalf("finish = %+v, want one entry", task.Finish)
	}
	if task.Finish[0].Tool != "book_it" {
		t.Errorf("tool = %q", task.Finish[0].Tool)
	}
	if got := task.Finish[0].Success["status"]; len(got) != 1 || got[0] != "booked" {
		t.Errorf("success = %v", task.Finish[0].Success)
	}
	if got := task.EndsOnTools(); len(got) != 1 || got[0] != "book_it" {
		t.Errorf("EndsOnTools = %v", got)
	}
}

// Each refusal names the tool, what is wrong, and what to write instead. A step
// whose finish: is wrong does not fail at runtime, it simply never ends by
// itself, which reads as the model being slow. Compile time is the only place
// an author finds out.
func TestBuildRefusesEveryBrokenFinishDeclaration(t *testing.T) {
	for _, tc := range []struct {
		name      string
		variables string
		task      string
		output    string
		phrases   []string
	}{
		{
			name:      "a tool the task does not hold",
			variables: finishVariables,
			task: `      - name: take_booking
        when: The caller wants an appointment.
        instructions: steps.md
        tools:
          - look_up
        finish:
          - tool: book_it
            success:
              - status: booked
        assign:
          - booking_reference: result.reference
`,
			output:  finishOKOutput,
			phrases: []string{"book_it", "take_booking", "does not list under tools:"},
		},
		{
			// propertyResultField types an output as text unless the schema
			// says integer, number or boolean, so an object read as text and
			// passed, and the mismatch surfaced after the business tool ran.
			name:      "an object output on a plain destination",
			variables: finishVariables,
			task: `      - name: take_booking
        when: The caller wants an appointment.
        instructions: steps.md
        tools:
          - book_it
        finish:
          - tool: book_it
            success:
              - status: booked
        assign:
          - booking_reference: result.reference
`,
			output: `  type: object
  properties:
    status:
      type: string
      enum:
        - booked
        - slot_unavailable
    reference:
      type: object
      properties:
        id:
          type: string
  required:
    - status
    - reference
`,
			phrases: []string{"book_it", "reference", "object", "one plain value"},
		},
		{
			name:      "an array output on a plain destination",
			variables: finishVariables,
			task: `      - name: take_booking
        when: The caller wants an appointment.
        instructions: steps.md
        tools:
          - book_it
        finish:
          - tool: book_it
            success:
              - status: booked
        assign:
          - booking_reference: result.reference
`,
			output: `  type: object
  properties:
    status:
      type: string
      enum:
        - booked
        - slot_unavailable
    reference:
      type: array
      items:
        type: string
  required:
    - status
    - reference
`,
			phrases: []string{"book_it", "reference", "array", "one plain value"},
		},
		{
			name:      "a success field with no enum",
			variables: finishVariables,
			task: `      - name: take_booking
        when: The caller wants an appointment.
        instructions: steps.md
        tools:
          - book_it
        finish:
          - tool: book_it
            success:
              - reference: anything
        assign:
          - booking_reference: result.reference
`,
			output:  finishOKOutput,
			phrases: []string{"reference on book_it has no enum", "give the output property an enum:"},
		},
		{
			name:      "a success value outside the enum",
			variables: finishVariables,
			task: `      - name: take_booking
        when: The caller wants an appointment.
        instructions: steps.md
        tools:
          - book_it
        finish:
          - tool: book_it
            success:
              - status: done
        assign:
          - booking_reference: result.reference
`,
			output:  finishOKOutput,
			phrases: []string{"book_it never returns status: done", "booked, slot_unavailable"},
		},
		{
			name:      "a tool that does not return what assign saves",
			variables: finishVariables,
			task: `      - name: take_booking
        when: The caller wants an appointment.
        instructions: steps.md
        tools:
          - book_it
        finish:
          - tool: book_it
            success:
              - status: booked
        assign:
          - booking_reference: result.reference
`,
			output: `  type: object
  properties:
    status:
      type: string
      enum:
        - booked
  required:
    - status
`,
			phrases: []string{"book_it returns no reference", "every tool under finish:"},
		},
		{
			name:      "a result field of the wrong type",
			variables: finishVariables,
			task: `      - name: take_booking
        when: The caller wants an appointment.
        instructions: steps.md
        tools:
          - book_it
        finish:
          - tool: book_it
            success:
              - status: booked
        assign:
          - booking_reference: result.reference
`,
			output: `  type: object
  properties:
    status:
      type: string
      enum:
        - booked
    reference:
      type: integer
  required:
    - status
    - reference
`,
			phrases: []string{"book_it returns reference as integer", "the variable is str"},
		},
		{
			name: "an object where the shape declares a field the tool omits",
			variables: `shapes:
  - name: Booking
    fields:
      - name: reference
        type: str
      - name: service
        type: str

variables:
  booking:
    type: Booking | None
    description: The booking just made.

`,
			task: `      - name: take_booking
        when: The caller wants an appointment.
        instructions: steps.md
        tools:
          - book_it
        finish:
          - tool: book_it
            success:
              - status: booked
        assign:
          - booking: result.booking
`,
			output: `  type: object
  properties:
    status:
      type: string
      enum:
        - booked
    booking:
      type: object
      properties:
        reference:
          type: string
  required:
    - status
`,
			phrases: []string{"book_it returns booking without service", "the shape Booking declares it"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg := finishPackage(t, tc.variables, tc.task, tc.output)
			_, err := Build(pkg)
			if err == nil {
				t.Fatal("want a refusal")
			}
			for _, phrase := range tc.phrases {
				if !strings.Contains(err.Error(), phrase) {
					t.Errorf("message does not say %q:\n%v", phrase, err)
				}
			}
			if !strings.Contains(err.Error(), "agent.yaml") {
				t.Errorf("message names no file:\n%v", err)
			}
		})
	}
}

// A shaped object the tool does return, field for field, compiles: this is the
// salon's own booking, and it is the case the object walk exists to allow.
func TestBuildAcceptsAShapedTerminalResult(t *testing.T) {
	pkg := finishPackage(t, `shapes:
  - name: Booking
    fields:
      - name: reference
        type: str
      - name: service
        type: str

variables:
  booking:
    type: Booking | None
    description: The booking just made.

`, `      - name: take_booking
        when: The caller wants an appointment.
        instructions: steps.md
        tools:
          - book_it
        finish:
          - tool: book_it
            success:
              - status: booked
        assign:
          - booking: result.booking
`, `  type: object
  properties:
    status:
      type: string
      enum:
        - booked
    booking:
      type: object
      properties:
        reference:
          type: string
        service:
          type: string
  required:
    - status
`)
	if _, err := Build(pkg); err != nil {
		t.Fatalf("build: %v", err)
	}
}

// A skip reads one thing: a confirmation, made by the step being skipped.
// Everything else is refused, because the emitted code could not honour it and
// the failure at run time is a step that runs forever or never.
func TestBuildRefusesASkipOnAnUnconfirmedVariable(t *testing.T) {
	for _, tc := range []struct {
		name    string
		skip    string
		phrases []string
	}{
		{
			name:    "a variable nothing declares",
			skip:    "nobody",
			phrases: []string{"skip_when_confirmed names \"nobody\"", "no variables: entry declares"},
		},
		{
			name:    "a variable with no confirm",
			skip:    "booking_reference",
			phrases: []string{"booking_reference carries no confirm:", "name a variable a step confirms"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg := skipPackage(t, tc.skip, "take_booking")
			_, err := Build(pkg)
			if err == nil {
				t.Fatal("want a refusal")
			}
			for _, phrase := range tc.phrases {
				if !strings.Contains(err.Error(), phrase) {
					t.Errorf("message does not say %q:\n%v", phrase, err)
				}
			}
		})
	}
}

// A variable another task confirms is refused: skipping this step would turn on
// somebody else's work, which is how a caller ends up booked unidentified.
func TestBuildRefusesASkipOnAnotherTasksConfirmation(t *testing.T) {
	pkg := skipPackage(t, "confirmed_phone", "second_step")
	_, err := Build(pkg)
	if err == nil {
		t.Fatal("want a refusal")
	}
	for _, phrase := range []string{"confirmed by take_booking, not by second_step", "put skip_when_confirmed: on that step"} {
		if !strings.Contains(err.Error(), phrase) {
			t.Errorf("message does not say %q:\n%v", phrase, err)
		}
	}
}

// A task some group names this way withdraws what it confirms on every entry,
// standalone included. Marked on the task so both drivers read one fact.
func TestBuildMarksAWithdrawingTask(t *testing.T) {
	pkg := skipPackage(t, "confirmed_phone", "take_booking")
	agent, err := Build(pkg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !agent.Tasks["take_booking"].Withdraws {
		t.Error("the skipped step does not withdraw its confirmation on entry")
	}
	if agent.Tasks["second_step"].Withdraws {
		t.Error("a step no group skips must not withdraw anything")
	}
	group := agent.TaskGroups["book"]
	if len(group.Steps) != 2 || group.Steps[0].SkipWhenConfirmed != "confirmed_phone" {
		t.Errorf("steps = %+v", group.Steps)
	}
	if group.Steps[1].SkipWhenConfirmed != "" {
		t.Errorf("a bare step must always run: %+v", group.Steps[1])
	}
}

// skipPackage writes a two-step group whose first step is named with the given
// skip variable, so each refusal is checked against a real file.
func skipPackage(t *testing.T, skip, on string) *packagespec.Package {
	t.Helper()
	variables := `variables:
  booking_reference:
    type: str
    default: ""
    description: The reference of the booking just made.

  confirmed_phone:
    type: str
    default: ""
    confirm: take_booking
    description: The number the caller agreed to.

`
	steps := "      - " + map[bool]string{true: "task: take_booking\n        skip_when_confirmed: " + skip, false: "take_booking"}[on == "take_booking"] + "\n" +
		"      - " + map[bool]string{true: "task: second_step\n        skip_when_confirmed: " + skip, false: "second_step"}[on == "second_step"] + "\n"
	task := `      - name: take_booking
        when: The caller has not been identified.
        instructions: steps.md
        tools:
          - book_it
        assign:
          - booking_reference: result.reference
          - confirmed_phone: result.reference

      - name: second_step
        instructions: steps.md
        tools:
          - look_up

    task_groups:
      - book
`
	pkg := finishPackage(t, variables, task, finishOKOutput)
	source, err := os.ReadFile(filepath.Join(pkg.Root, "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	rewritten := strings.Replace(string(source), "\ntools:\n", "\ntask_groups:\n  book:\n    when: The caller wants an appointment.\n    steps:\n"+steps+"    context_scope: shared\n    then: return\n\ntools:\n", 1)
	if err := os.WriteFile(filepath.Join(pkg.Root, "agent.yaml"), []byte(rewritten), 0o644); err != nil {
		t.Fatal(err)
	}
	reloaded, err := packagespec.Load(pkg.Root)
	if err != nil {
		t.Fatalf("the fixture itself does not load: %v", err)
	}
	return reloaded
}

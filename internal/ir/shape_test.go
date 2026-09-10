package ir

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	packagespec "github.com/slng-ai/unmute/internal/spec"
)

// The gate under every refusal a declared shape or a type expression produces.
//
// Each case asserts the **line and the column**, not just the sentence. The
// line alone is `type: list[Literal["create_booking", "cancel_bookng"]]` and
// says nothing about which of the two typos it is; the column points at the
// token. internal/spec supplies the column and this package joins it to the
// file and the line, and that join is what these tests hold.

// typedPackage writes the smallest package that loads, with the given `shapes:`
// and `variables:` blocks spliced in, so a refusal is checked against a real
// file rather than against a struct with no position.
func typedPackage(t *testing.T, blocks string) *packagespec.Package {
	t.Helper()
	return typedPackageWithTasks(t, blocks, "")
}

// typedPackageWithTasks is the same fixture with a `tasks:` block under the one
// agent, for the refusals that are about an assignment or a guard rather than
// about a type.
func typedPackageWithTasks(t *testing.T, blocks, tasks string) *packagespec.Package {
	t.Helper()
	root := t.TempDir()
	source := `version: 1

name: typed-refusal

entry_agent: desk

secrets:
  - OPENAI_API_KEY

` + blocks + `
agents:
  desk:
    instructions: instructions.md
    think: reasoning
    speak: voice
` + tasks + `
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
`
	write := func(name, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("agent.yaml", source)
	write("instructions.md", "# Desk\n\nTake appointment calls for one salon.\n")
	write("targets.yaml", "targets:\n  livekit:\n    provider: livekit\n    version: \"1.6.10\"\n    sdk_language: python\n")
	pkg, err := packagespec.Load(root)
	if err != nil {
		t.Fatalf("the fixture itself does not load: %v", err)
	}
	return pkg
}

// lineOf is the 1-based line the given text sits on in the package's agent.yaml,
// so a case names the text it means rather than a line number that moves when
// the fixture's header changes.
func lineOf(t *testing.T, pkg *packagespec.Package, needle string) int {
	t.Helper()
	source, err := os.ReadFile(filepath.Join(pkg.Root, "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(source), "\n") {
		if strings.Contains(line, needle) {
			return i + 1
		}
	}
	t.Fatalf("the fixture holds no line containing %q", needle)
	return 0
}

func TestBuildRefusesEveryTypeOutsideTheScope(t *testing.T) {
	for _, tc := range []struct {
		name string
		// blocks is the shapes:/variables: text, at, the text the refusal must
		// point at, col the column inside the expression, and phrases what the
		// message must say.
		blocks  string
		at      string
		col     int
		phrases []string
	}{
		{
			name: "a native temporal type",
			blocks: `variables:
  when:
    type: datetime
`,
			at: "type: datetime", col: 1,
			phrases: []string{`"datetime" is not a type`, `write "Date" and "Time"`, "never written into the schema"},
		},
		{
			name: "a native identifier type",
			blocks: `variables:
  booking:
    type: UUID
`,
			at: "type: UUID", col: 1, phrases: []string{`write "Id"`},
		},
		{
			// The two names a person reaches for before they know the spelling.
			// Both say what to write, and both name the pair as well, because an
			// author asking for "email" often wants the name beside it.
			name: "the lower-case email spelling",
			blocks: `variables:
  contact:
    type: email
`,
			at: "type: email", col: 1,
			phrases: []string{`write "EmailStr"`, `"NameEmail"`},
		},
		{
			name: "the capitalised email spelling",
			blocks: `variables:
  contact:
    type: Email
`,
			at: "type: Email", col: 1,
			phrases: []string{`write "EmailStr"`, `"NameEmail"`},
		},
		{
			name: "a secret as a type",
			blocks: `variables:
  token:
    type: SecretStr
`,
			at: "type: SecretStr", col: 1, phrases: []string{"never travels through state", "*_env"},
		},
		{
			name: "a dictionary",
			blocks: `variables:
  extras:
    type: dict[str, str]
`,
			at: "type: dict[str, str]", col: 1, phrases: []string{`declare the fields as a shape`, `"shapes:"`},
		},
		{
			name: "the capitalised typing spellings",
			blocks: `variables:
  reasons:
    type: List[str]
`,
			at: "type: List[str]", col: 1, phrases: []string{`write "list[...]"`, "lower case"},
		},
		{
			name: "Optional rather than a union",
			blocks: `variables:
  customer:
    type: Optional[str]
`,
			at: "type: Optional[str]", col: 1, phrases: []string{`write "T | None"`},
		},
		{
			name: "a shape nobody declared",
			blocks: `variables:
  customer:
    type: Customer
`,
			at: "type: Customer", col: 1,
			phrases: []string{
				`no shape named "Customer" is declared`, `"shapes:"`, "Literal[...]", "list[...]",
				// Every name the scope takes, or an author who mistyped one of
				// them is never told it exists.
				"EmailStr", "NameEmail",
			},
		},
		// The column is the whole point of this one: the mistake is inside the
		// expression and the line is identical either way.
		{
			name: "a shape nobody declared, nested inside a list",
			blocks: `variables:
  appointments:
    type: list[Appointmnt]
`,
			at: "type: list[Appointmnt]", col: 6,
			phrases: []string{`no shape named "Appointmnt" is declared`},
		},
		{
			name: "a Literal with no entries",
			blocks: `variables:
  reason:
    type: Literal[]
`,
			at: "type: Literal[]", col: 1, phrases: []string{"with no entries", "closed set"},
		},
		{
			name: "a list with no element type",
			blocks: `variables:
  reasons:
    type: list[]
`,
			at: "type: list[]", col: 1, phrases: []string{"no element type", "list[Appointment]"},
		},
		{
			name: "a Literal entry written twice",
			blocks: `variables:
  reason:
    type: Literal["book", "book"]
`,
			at: `type: Literal["book", "book"]`, col: 17, phrases: []string{`the entry "book" twice`, "a set"},
		},
		{
			name: "a union of two real types",
			blocks: `variables:
  reason:
    type: str | int
`,
			at: "type: str | int", col: 7, phrases: []string{"a union of str and int", "| None"},
		},
		{
			name: "None and nothing else",
			blocks: `variables:
  nothing:
    type: None
`,
			at: "type: None", col: 1, phrases: []string{"not a value", "name the type it holds"},
		},
		{
			name: "a list that may be absent",
			blocks: `variables:
  reasons:
    type: list[str] | None
`,
			at: "type: list[str] | None", col: 1, phrases: []string{"starts empty", "never absent"},
		},
		{
			name: "a list of lists",
			blocks: `variables:
  grid:
    type: list[list[str]]
`,
			at: "type: list[list[str]]", col: 1, phrases: []string{"a list of lists", "declare a shape"},
		},
		{
			name: "brackets on a primitive",
			blocks: `variables:
  reason:
    type: str[int]
`,
			at: "type: str[int]", col: 1, phrases: []string{`"str" takes no brackets`},
		},
		{
			// The one case that lands on the fallback line: the type was written
			// quoted, so the decoded text does not appear in the file and the
			// refusal names the variable's own line instead. Stated here rather
			// than left as a surprise.
			name: "a quoted entry where a type belongs",
			blocks: `variables:
  reason:
    type: "\"book\""
`,
			at: "  reason:", col: 1, phrases: []string{"where a type belongs", `Literal["a", "b"]`},
		},
		{
			name: "a field type outside the scope",
			blocks: `shapes:
  - name: Appointment
    fields:
      - scheduled_date: date
variables:
  appointments:
    type: list[Appointment]
`,
			at: "- scheduled_date: date", col: 1,
			phrases: []string{`shape "Appointment" field "scheduled_date"`, `write "Date"`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg := typedPackage(t, tc.blocks)
			_, err := Build(pkg)
			if err == nil {
				t.Fatalf("built, want a refusal")
			}
			message := err.Error()
			// The line the author has to open. `at` is the text the refusal is
			// about, and the fixture is searched for it, so a change to the
			// fixture's header cannot make this test wrong.
			wantLine := fmt.Sprintf("agent.yaml:%d:", lineOf(t, pkg, tc.at))
			if !strings.Contains(message, wantLine) {
				t.Errorf("refusal %q does not name %s", message, wantLine)
			}
			if want := fmt.Sprintf("column %d:", tc.col); !strings.Contains(message, want) {
				t.Errorf("refusal %q does not name %s", message, want)
			}
			for _, phrase := range tc.phrases {
				if !strings.Contains(message, phrase) {
					t.Errorf("refusal %q does not say %q", message, phrase)
				}
			}
		})
	}
}

// TestBuildRefusesEveryBrokenShapeDeclaration covers the refusals about the
// declaration rather than about one type inside it. These carry the file and
// the line; only a type expression has a column.
func TestBuildRefusesEveryBrokenShapeDeclaration(t *testing.T) {
	for _, tc := range []struct {
		name    string
		blocks  string
		at      string
		phrases []string
	}{
		{
			name: "a field declared twice",
			blocks: `shapes:
  - name: Appointment
    fields:
      - scheduled_date: Date
      - scheduled_date: Time
variables:
  appointments:
    type: list[Appointment]
`,
			at:      "- name: Appointment",
			phrases: []string{`field "scheduled_date" twice`, "silently replace"},
		},
		{
			// Two underscores in a row would make the flat emitted form of a
			// path, value__field, read as a deeper path.
			name: "a field named outside the name grammar",
			blocks: `shapes:
  - name: Appointment
    fields:
      - scheduled__date: Date
variables:
  appointments:
    type: list[Appointment]
`,
			at:      "- name: Appointment",
			phrases: []string{`field "scheduled__date" is not a name this scope takes`, "like scheduled_date"},
		},
		{
			name: "a shape declared twice",
			blocks: `shapes:
  - name: Appointment
    fields:
      - scheduled_date: Date
  - name: Appointment
    fields:
      - scheduled_time: Time
variables:
  appointments:
    type: list[Appointment]
`,
			at:      "- name: Appointment",
			phrases: []string{"declared twice", "merge the two"},
		},
		{
			name: "a shape with no fields",
			blocks: `shapes:
  - name: Appointment
    fields: []
variables:
  appointments:
    type: list[Appointment]
`,
			at:      "- name: Appointment",
			phrases: []string{"declares no fields", "tells the model nothing"},
		},
		{
			// Adding EmailStr to the shaped set reserves the name for free,
			// through the same map reservedShapeName already reads.
			name: "a shape named after the email text type",
			blocks: `shapes:
  - name: EmailStr
    fields:
      - address: str
variables:
  contact:
    type: EmailStr
`,
			at:      "- name: EmailStr",
			phrases: []string{"text types with a validated shape", "CapWords"},
		},
		{
			// The supplied pair needs its own arm: it is a shape rather than a
			// text type, and both declarations would generate one class name.
			name: "a shape named after a shape the compiler supplies",
			blocks: `shapes:
  - name: NameEmail
    fields:
      - who: str
variables:
  contact:
    type: NameEmail
`,
			at:      "- name: NameEmail",
			phrases: []string{"shapes this compiler supplies", "same class", "CapWords"},
		},
		{
			name: "a shape named after part of the grammar",
			blocks: `shapes:
  - name: Literal
    fields:
      - scheduled_date: Date
variables:
  appointments:
    type: list[Literal]
`,
			at:      "- name: Literal",
			phrases: []string{"part of the type grammar", "CapWords"},
		},
		{
			name: "a shape named after a primitive",
			blocks: `shapes:
  - name: str
    fields:
      - scheduled_date: Date
variables:
  appointments:
    type: list[str]
`,
			at:      "- name: str",
			phrases: []string{"one of the primitive types"},
		},
		{
			name: "a shape that refers to itself",
			blocks: `shapes:
  - name: Appointment
    fields:
      - scheduled_date: Date
      - follows: Appointment
variables:
  appointments:
    type: list[Appointment]
`,
			at:      "- name: Appointment",
			phrases: []string{"refers to itself", "Appointment -> Appointment", "no bottom"},
		},
		{
			name: "a cycle through a second shape",
			blocks: `shapes:
  - name: Appointment
    fields:
      - customer: Customer
  - name: Customer
    fields:
      - booked: list[Appointment]
variables:
  appointments:
    type: list[Appointment]
`,
			at:      "- name: Appointment",
			phrases: []string{"refers to itself", "Appointment -> Customer -> Appointment"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg := typedPackage(t, tc.blocks)
			_, err := Build(pkg)
			if err == nil {
				t.Fatalf("built, want a refusal")
			}
			message := err.Error()
			wantLine := fmt.Sprintf("agent.yaml:%d:", lineOf(t, pkg, tc.at))
			if !strings.Contains(message, wantLine) {
				t.Errorf("refusal %q does not name %s", message, wantLine)
			}
			for _, phrase := range tc.phrases {
				if !strings.Contains(message, phrase) {
					t.Errorf("refusal %q does not say %q", message, phrase)
				}
			}
		})
	}
}

// TestBuildResolvesTheDeclaredShapes is the positive half: what the resolved
// catalog holds, and that a bare primitive keeps Shape nil, which is the one
// thing FR-015 rests on.
func TestBuildResolvesTheDeclaredShapes(t *testing.T) {
	pkg, err := packagespec.Load(filepath.Join("..", "testdata", "typed_state"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	shape, ok := agent.Shapes["Appointment"]
	if !ok {
		t.Fatalf("no Appointment shape resolved: %v", agent.Shapes)
	}
	if len(shape.Fields) != 3 {
		t.Fatalf("Appointment declares %d fields, want 3", len(shape.Fields))
	}
	// Declaration order, not sorted: a class whose fields move under a reader is
	// a class whose diff says nothing.
	for i, want := range []string{"scheduled_date", "scheduled_time", "appointment_type"} {
		if shape.Fields[i].Name != want {
			t.Errorf("field %d = %q, want %q", i, shape.Fields[i].Name, want)
		}
	}
	if got := shape.Fields[0].Type.Shaped; got != ShapedDate {
		t.Errorf("scheduled_date lowers to %q, want Date", got)
	}
	// FR-014: the description is carried this far, because the model reads it.
	if shape.Fields[2].Description == "" {
		t.Error("appointment_type lost its description, so the model is never told what the field means")
	}
	if got := agent.Variables["appointments"].Shape.String(); got != "list[Appointment]" {
		t.Errorf("appointments resolves to %q", got)
	}
	if got := agent.Variables["caller_reason"].Shape.String(); got != `list[Literal["create_booking", "cancel_booking"]]` {
		t.Errorf("caller_reason resolves to %q", got)
	}
	if got := agent.Variables["caller_phone"].Shape.String(); got != "Phone" {
		t.Errorf("caller_phone resolves to %q", got)
	}
	// A structured value reaches a prompt as text, which is what every reader
	// that existed before this feature does with Type.
	if got := agent.Variables["appointments"].Type; got != PrimitiveString {
		t.Errorf("appointments Type = %q, want the primitive a prompt renders", got)
	}
	if got := agent.Variables["reminder_email"].Shape.String(); got != "EmailStr" {
		t.Errorf("reminder_email resolves to %q", got)
	}
	// The supplied pair resolves as a shape reference, exactly like the declared
	// one above, which is what lets every path, assign and per-target row read
	// the catalog and know nothing about built-ins.
	if got := agent.Variables["booked_for"].Shape.String(); got != "NameEmail" {
		t.Errorf("booked_for resolves to %q", got)
	}
	supplied, ok := agent.Shapes["NameEmail"]
	if !ok {
		t.Fatalf("NameEmail was named and never reached the catalog: %v", sortedKeys(agent.Shapes))
	}
	if !supplied.Builtin {
		t.Error("the NameEmail in the catalog is not marked as supplied, so it emits no parser")
	}
	if len(supplied.Fields) != 2 || supplied.Fields[0].Name != "name" || supplied.Fields[1].Name != "email" {
		t.Errorf("NameEmail resolved %d fields, want name then email: %+v", len(supplied.Fields), supplied.Fields)
	}
	// The pair's own field carries the email type, which is how the alias counts
	// as used in a package that declares no bare EmailStr.
	if got := supplied.Fields[1].Type.Shaped; got != ShapedEmail {
		t.Errorf("NameEmail.email lowers to %q, want EmailStr", got)
	}

	// And the order the block will number them in is the authored order.
	want := []string{
		"count", "accepted", "caller_reason", "appointments", "caller_phone",
		"last_appointment", "reminder_email", "booked_for",
	}
	if len(agent.VariableOrder) != len(want) {
		t.Fatalf("VariableOrder = %v, want %v", agent.VariableOrder, want)
	}
	for i := range want {
		if agent.VariableOrder[i] != want[i] {
			t.Errorf("VariableOrder = %v, want %v", agent.VariableOrder, want)
			break
		}
	}
}

// TestBuildLeavesAPrimitiveVariableAlone is FR-015 at the resolution boundary:
// a package declaring nothing structured resolves exactly as it did, so nothing
// downstream can emit a byte differently.
func TestBuildLeavesAPrimitiveVariableAlone(t *testing.T) {
	agent, err := Build(loadSafeCore(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(agent.Shapes) != 0 {
		t.Errorf("a package declaring no shapes resolved %d of them", len(agent.Shapes))
	}
	for name, variable := range agent.Variables {
		if variable.Shape != nil {
			t.Errorf("variable %q resolved a shape %q from a bare primitive", name, variable.Shape.String())
		}
	}
	// Both spellings resolve to one primitive, so `type: string` and `type: str`
	// are the same declaration and no existing package changes.
	for spelling, want := range map[string]PrimitiveType{
		"string": PrimitiveString, "str": PrimitiveString,
		"boolean": PrimitiveBoolean, "bool": PrimitiveBoolean,
		"integer": PrimitiveInteger, "int": PrimitiveInteger,
		"number": PrimitiveNumber, "float": PrimitiveNumber,
	} {
		ref, err := resolveType(spelling, nil)
		if err != nil {
			t.Fatalf("%q: %v", spelling, err)
		}
		if ref.Structured() {
			t.Errorf("%q resolved as structured, so a package writing it would stop being byte-identical", spelling)
		}
		if ref.Primitive != want {
			t.Errorf("%q resolved to %q, want %q", spelling, ref.Primitive, want)
		}
	}
}

// TestBuildRefusesAnAppendOnSomethingThatIsNotAList and the requires-path case
// below are the two refusals the accumulating half of this feature adds.
func TestBuildRefusesAnAppendOnSomethingThatIsNotAList(t *testing.T) {
	// caller_phone is a Phone, not a list, and the `+` is what asks to append.
	// Without the refusal the value is replaced by a one-item list on the first
	// write and every reader of it breaks at run time.
	pkg := typedPackageWithTasks(t, `variables:
  caller_phone:
    type: Phone
    default: ""
`, `    tasks:
      - name: confirm_number
        when: Read the number back.
        instructions: instructions.md
        assign:
          - caller_phone+: result.caller_phone
`)
	_, err := Build(pkg)
	if err == nil {
		t.Fatal("an append onto a Phone built, and the value would have been replaced by a one-item list")
	}
	for _, phrase := range []string{"appends to", "caller_phone", "declare a list", "remove +"} {
		if !strings.Contains(err.Error(), phrase) {
			t.Errorf("refusal %q does not say %q", err.Error(), phrase)
		}
	}
}

// TestBuildAssignAcceptsASubFieldOfAShapedResult is gap 1 of the scoped
// variables feature: a step's result can hand back a whole shape, and an
// assign: may now name one field inside it rather than only the shape as a
// whole. The walk is FieldPath, reached from assign:.
func TestBuildAssignAcceptsASubFieldOfAShapedResult(t *testing.T) {
	pkg := typedPackageWithTasks(t, `shapes:
  - name: Appointment
    fields:
      - scheduled_date: Date
variables:
  last_appointment:
    type: Appointment
  last_booking_day:
    type: Date
`, `    tasks:
      - name: book
        when: The caller wants an appointment.
        instructions: instructions.md
        assign:
          - last_appointment: result.appointment
          - last_booking_day: result.appointment.scheduled_date
`)
	if _, err := Build(pkg); err != nil {
		t.Fatalf("assign into a sub-field of a shaped result was refused: %v", err)
	}
}

// The same walk into a shape the compiler supplies rather than one this package
// declares, and it is the seeded catalog that makes it work: FieldPath reads
// agent.Shapes and nothing else, so a reference with no entry there is told it
// has no fields to name.
//
// The picked field's type has to fit where it lands, which is the other half:
// the pair's `email` is an EmailStr, so it assigns into an EmailStr and is
// refused by a Date. That is assignableInto, the same predicate a prefetch
// entry uses.
func TestBuildAssignAcceptsASubFieldOfASuppliedShape(t *testing.T) {
	for _, tc := range []struct {
		name, declared, want string
	}{
		{name: "into the field's own type", declared: "EmailStr"},
		{
			name:     "into a type the field does not fit",
			declared: "Date",
			want:     `the result field is EmailStr and the variable is Date`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg := typedPackageWithTasks(t, `variables:
  booked_for:
    type: NameEmail
  reminder_email:
    type: `+tc.declared+`
`, `    tasks:
      - name: book
        when: The caller wants an appointment.
        instructions: instructions.md
        assign:
          - booked_for: result.contact
          - reminder_email: result.contact.email
`)
			_, err := Build(pkg)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("assign into a field of a supplied shape was refused: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want a refusal saying %q", err, tc.want)
			}
		})
	}
}

// TestBuildRefusesAnAssignPathIntoAList: a path cannot say which entry of a
// list it means, so it is refused rather than left as a write nothing can
// ever satisfy.
func TestBuildRefusesAnAssignPathIntoAList(t *testing.T) {
	pkg := typedPackageWithTasks(t, `shapes:
  - name: Appointment
    fields:
      - scheduled_date: Date
      - name: services
        type: list[str]
variables:
  last_appointment:
    type: Appointment
  last_booking_day:
    type: Date
`, `    tasks:
      - name: book
        when: The caller wants an appointment.
        instructions: instructions.md
        assign:
          - last_appointment: result.appointment
          - last_booking_day: result.appointment.services.name
`)
	_, err := Build(pkg)
	if err == nil {
		t.Fatal("an assign path through a list built, and nothing says which entry it meant")
	}
	for _, phrase := range []string{"services", "is a list", "cannot name a field inside one"} {
		if !strings.Contains(err.Error(), phrase) {
			t.Errorf("refusal %q does not say %q", err.Error(), phrase)
		}
	}
}

// TestBuildRefusesAnAssignPathToAnUnknownField is the misspelled-field half:
// without the resolution this is a write that can never be right, and nothing
// says so until a real call sits waiting on it.
func TestBuildRefusesAnAssignPathToAnUnknownField(t *testing.T) {
	pkg := typedPackageWithTasks(t, `shapes:
  - name: Appointment
    fields:
      - scheduled_date: Date
variables:
  last_appointment:
    type: Appointment
  last_booking_day:
    type: Date
`, `    tasks:
      - name: book
        when: The caller wants an appointment.
        instructions: instructions.md
        assign:
          - last_appointment: result.appointment
          - last_booking_day: result.appointment.appointment_time
`)
	_, err := Build(pkg)
	if err == nil {
		t.Fatal("an assign path naming no field built")
	}
	for _, phrase := range []string{`shape "Appointment" declares no field "appointment_time"`, "scheduled_date"} {
		if !strings.Contains(err.Error(), phrase) {
			t.Errorf("refusal %q does not say %q", err.Error(), phrase)
		}
	}
}

// TestBuildRefusesAnAssignPathThroughAnEnumField is the third way a dotted
// assign path can fail to resolve: an enum result field, like a raw JSON
// Schema field, has no declared shape, so it has no fields for a path to name.
func TestBuildRefusesAnAssignPathThroughAnEnumField(t *testing.T) {
	pkg := typedPackageWithTasks(t, `variables:
  status:
    type: Literal["pending", "confirmed"]
  last_booking_day:
    type: Date
`, `    tasks:
      - name: book
        when: The caller wants an appointment.
        instructions: instructions.md
        assign:
          - status: result.status
          - last_booking_day: result.status.label
`)
	_, err := Build(pkg)
	if err == nil {
		t.Fatal("an assign path through an enum field built, and it has no fields to walk into")
	}
	for _, phrase := range []string{`Literal["pending", "confirmed"]`, "has no fields to name"} {
		if !strings.Contains(err.Error(), phrase) {
			t.Errorf("refusal %q does not say %q", err.Error(), phrase)
		}
	}
}

// TestBuildAssignPropagatesOptionalAlongAPath is the rest of gap 1: a shape
// field the step may not have produced yet makes every field inside it
// equally absent, so a path walked through one comes back Optional even where
// the field at the end of it is declared plainly, and that has to reach the
// variable it is written into.
func TestBuildAssignPropagatesOptionalAlongAPath(t *testing.T) {
	shapes := `shapes:
  - name: Appointment
    fields:
      - scheduled_date: Date
`
	tasks := `    tasks:
      - name: book
        when: The caller wants an appointment.
        instructions: instructions.md
        assign:
          - appointment: result.appointment
          - last_booking_day: result.appointment.scheduled_date
`
	t.Run("refused into a variable declared without | None", func(t *testing.T) {
		pkg := typedPackageWithTasks(t, shapes+`variables:
  appointment:
    type: Appointment | None
  last_booking_day:
    type: Date
`, tasks)
		_, err := Build(pkg)
		if err == nil {
			t.Fatal("a path through an optional field assigned into a non-optional variable built")
		}
		for _, phrase := range []string{"Date | None", "the variable is Date"} {
			if !strings.Contains(err.Error(), phrase) {
				t.Errorf("refusal %q does not say %q", err.Error(), phrase)
			}
		}
	})
	t.Run("accepted once the variable is declared | None", func(t *testing.T) {
		pkg := typedPackageWithTasks(t, shapes+`variables:
  appointment:
    type: Appointment | None
  last_booking_day:
    type: Date | None
`, tasks)
		if _, err := Build(pkg); err != nil {
			t.Fatalf("a path through an optional field into an equally optional variable was refused: %v", err)
		}
	})
}

// TestTypeRefSchemaPublishesEveryField is the drift guard the hand-wired
// definition in schema.go needs: reflection cannot follow TypeRef into itself,
// so its schema is written by hand, and a field added to the struct without a
// line in that schema would leave the debug schema describing a shape the
// compiler no longer produces.
func TestTypeRefSchemaPublishesEveryField(t *testing.T) {
	published := typeRefSchema().Properties
	structure := reflect.TypeFor[TypeRef]()
	for i := range structure.NumField() {
		field := structure.Field(i)
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		if _, ok := published[name]; !ok {
			t.Errorf("TypeRef.%s (%q) has no line in typeRefSchema, so the derived schema no longer describes it",
				field.Name, name)
		}
	}
	if len(published) != structure.NumField() {
		t.Errorf("typeRefSchema publishes %d properties for %d fields", len(published), structure.NumField())
	}
	// And the whole schema still marshals, which is what the debug output does
	// with it.
	whole, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(whole)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), typeRefPointer) {
		t.Errorf("the derived schema carries no reference to %s", typeRefPointer)
	}
}

func TestTaskResultDerivesFromAssignments(t *testing.T) {
	pkg := loadSafeCore(t)
	pkg.Agent.Variables["appointment_date"] = packagespec.Variable{Type: "Date", Description: "The day the caller chose."}
	pkg.Agent.Variables["same_date"] = packagespec.Variable{Type: "Date", Description: "A different description."}
	attachStep(pkg, "intake", packagespec.Task{
		Name: "choose_day", When: "Choose a day", Instructions: "choose.md",
		Assign: []packagespec.Pair{{Key: "appointment_date", Value: "result.date"}, {Key: "same_date", Value: "result.date"}},
	}, "Choose the requested date.")
	got, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	field, ok := got.Tasks["choose_day"].Result["date"]
	if !ok || field.Shape == nil || field.Shape.String() != "Date" {
		t.Fatalf("destination Date did not reach finish: %+v", field)
	}
	encoded, err := json.Marshal(field)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "The day the caller chose.") || strings.Contains(string(encoded), "A different description.") {
		t.Fatalf("destination description did not reach finish: %s", encoded)
	}
}

func TestTaskResultDerivationPaths(t *testing.T) {
	for _, tc := range []struct {
		name, whole string
		pairs       []packagespec.Pair
		want        string
	}{
		{"projection before anchor", "Appointment", []packagespec.Pair{{Key: "date", Value: "result.appointment.date"}, {Key: "record", Value: "result.appointment"}}, ""},
		{"missing anchor", "Appointment", []packagespec.Pair{{Key: "date", Value: "result.appointment.date"}}, "whole"},
		{"optional parent", "Appointment | None", []packagespec.Pair{{Key: "date", Value: "result.appointment.date"}, {Key: "record", Value: "result.appointment"}}, "does not fit"},
		{"unknown field", "Appointment", []packagespec.Pair{{Key: "date", Value: "result.appointment.missing"}, {Key: "record", Value: "result.appointment"}}, "missing"},
		{"conflicting root", "Appointment", []packagespec.Pair{{Key: "date", Value: "result.value"}, {Key: "record", Value: "result.value"}}, "conflict"},
		{"duplicate destination", "Appointment", []packagespec.Pair{{Key: "records", Value: "result.all"}, {Key: "records+", Value: "result.one"}}, "twice"},
		{"optional append copy", "Appointment", []packagespec.Pair{{Key: "records+", Value: "result.one"}}, ""},
		{"no output", "Appointment", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg := loadSafeCore(t)
			pkg.Agent.Shapes = []packagespec.Shape{{Name: "Appointment", Fields: []packagespec.Field{{Name: "date", Type: "Date"}}}}
			pkg.Agent.Variables["record"] = packagespec.Variable{Type: tc.whole}
			pkg.Agent.Variables["records"] = packagespec.Variable{Type: "list[Appointment]"}
			pkg.Agent.Variables["date"] = packagespec.Variable{Type: "Date"}
			attachStep(pkg, "intake", packagespec.Task{Name: "choose", When: "Choose", Instructions: "choose.md", Assign: tc.pairs}, "Choose.")
			got, err := Build(pkg)
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("got %v, want %q", err, tc.want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "optional append copy" {
				if !got.Tasks["choose"].Result["one"].Shape.Optional || got.Variables["records"].Shape.List.Optional {
					t.Fatal("append must use an optional copy of the item type")
				}
			}
		})
	}
}

// The shaped-type vocabulary is named on four surfaces: the ShapedText
// constants, the spelling map an author's `type:` resolves through, the derived
// debug schema's enum, and the emitted aliases in internal/generate. They used
// to be four hand-written lists, which is how a fifth type gets half-added: a
// name missing from the spelling map resolves as an undeclared shape, one
// missing from the enum quietly stops being described in the debug schema, and
// one missing from the emission order is written into an annotation no module
// defines.
//
// They all read shapedTextOrder now. This gate is what keeps that true, because
// the cheap way to add a type is still to declare a constant and stop.
func TestEveryShapedTypeReachesEverySurface(t *testing.T) {
	order := ShapedTextOrder()
	if len(order) == 0 {
		t.Fatal("no shaped text types, so this gate proves nothing")
	}
	// One constant, one spelling, and the spelling is the constant's own text:
	// the name an author writes is the name a refusal prints.
	for _, kind := range order {
		resolved, ok := shapedSpellings[string(kind)]
		if !ok {
			t.Errorf("%q is a shaped type an author cannot write: add it to shapedSpellings", kind)
			continue
		}
		if resolved != kind {
			t.Errorf("the spelling %q resolves to %q", kind, resolved)
		}
	}
	if len(shapedSpellings) != len(order) {
		t.Errorf("%d spellings for %d shaped types; every spelling is one type's own name",
			len(shapedSpellings), len(order))
	}
	// The refusal vocabulary offers every one of them, plus every supplied
	// shape. An author who mistyped a name is told what the scope takes, and a
	// name absent from that list is a name nobody discovers.
	offered := scopeNames(nil)
	for _, name := range append(shapedNames(order), BuiltinShapeNames()...) {
		if !slices.Contains(offered, name) {
			t.Errorf("the refusal vocabulary does not offer %q: %v", name, offered)
		}
	}
	// The derived schema publishes the same set, in the same order.
	published := typeRefSchema().Properties["shaped"].Enum
	if len(published) != len(order) {
		t.Fatalf("the debug schema publishes %d shaped types for %d: %v", len(published), len(order), published)
	}
	for i, kind := range order {
		if published[i] != kind {
			t.Errorf("the debug schema publishes %v at %d, want %q", published[i], i, kind)
		}
	}
	// And every supplied shape is a shape a `type:` resolves to, is marked as
	// supplied, and declares at least one field. A supplied shape with none
	// would generate a class that tells the model nothing, which is refused for
	// an authored one and would be silent here.
	for _, name := range BuiltinShapeNames() {
		shape := builtinShapes[name]
		if shape.Name != name {
			t.Errorf("the supplied shape keyed %q calls itself %q", name, shape.Name)
		}
		if !shape.Builtin {
			t.Errorf("the supplied shape %q is not marked as supplied, so it emits no parser", name)
		}
		if len(shape.Fields) == 0 {
			t.Errorf("the supplied shape %q declares no fields", name)
		}
		if reason := reservedShapeName(name); reason == "" {
			t.Errorf("an author may declare a shape called %q, and both would generate one class", name)
		}
		ref, err := resolveType(name, nil)
		if err != nil {
			t.Errorf("%q does not resolve as a type: %v", name, err)
			continue
		}
		if ref.Shape != name {
			t.Errorf("%q resolves to %+v, want a reference to the shape of that name", name, ref)
		}
	}
}

func shapedNames(order []ShapedText) []string {
	out := make([]string, 0, len(order))
	for _, kind := range order {
		out = append(out, string(kind))
	}
	return out
}

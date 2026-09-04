package ir

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
			phrases: []string{`no shape named "Customer" is declared`, `"shapes:"`, "Literal[...]", "list[...]"},
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
	// And the order the block will number them in is the authored order.
	want := []string{"caller_reason", "appointments", "caller_phone"}
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
        result:
          caller_phone: Phone
          summary: string
`)
	_, err := Build(pkg)
	if err == nil {
		t.Fatal("an append onto a Phone built, and the value would have been replaced by a one-item list")
	}
	for _, phrase := range []string{"appends to", "caller_phone", "rather than a list", "list[...]"} {
		if !strings.Contains(err.Error(), phrase) {
			t.Errorf("refusal %q does not say %q", err.Error(), phrase)
		}
	}
}

func TestBuildRefusesARequiresPathThatDoesNotResolve(t *testing.T) {
	// A path into a declared shape, with the field misspelled. Without the
	// resolution this is a guard that can never pass, and nothing says so until
	// a real call sits waiting on it.
	pkg := typedPackageWithTasks(t, `shapes:
  - name: Customer
    fields:
      - customer_name: str
variables:
  customer:
    type: Customer | None
`, `    tasks:
      - name: book
        when: The caller wants an appointment.
        instructions: instructions.md
        requires:
          - customer.customer_nam
        result:
          summary: string
`)
	_, err := Build(pkg)
	if err == nil {
		t.Fatal("a requires: path naming no field built, and the guard could never pass")
	}
	for _, phrase := range []string{"customer.customer_nam", `shape "Customer" declares no field`, "customer_name"} {
		if !strings.Contains(err.Error(), phrase) {
			t.Errorf("refusal %q does not say %q", err.Error(), phrase)
		}
	}
}

// TestBuildAcceptsARequiresPathThatResolves is the other half: a path that
// names a real field is legal, and the deleted "no route can fill this"
// refusal must not come back with it.
func TestBuildAcceptsARequiresPathThatResolves(t *testing.T) {
	pkg := typedPackageWithTasks(t, `shapes:
  - name: Customer
    fields:
      - customer_name: str
variables:
  customer:
    type: Customer | None
`, `    tasks:
      - name: book
        when: The caller wants an appointment.
        instructions: instructions.md
        requires:
          - customer.customer_name
        result:
          summary: string
`)
	if _, err := Build(pkg); err != nil {
		t.Fatalf("a requires: path naming a declared field was refused: %v", err)
	}
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

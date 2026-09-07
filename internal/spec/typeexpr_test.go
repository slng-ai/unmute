package spec

import (
	"errors"
	"strings"
	"testing"
)

// TestParseTypeReadsTheGrammar is the gate under the type grammar. Every
// expression the authoring contract lists is here, and so is every expression
// it refuses for a reason a reader can see in the text.
//
// The four out-of-scope temporal and identifier names are in the accepted table
// on purpose. This parser is structure only: `datetime` is a name like any
// other and it parses, carrying its column, so internal/ir can refuse it by
// name and say to write `Date` instead. A parser that refused it here would own
// half the type scope and internal/ir would own the other half.
func TestParseTypeReadsTheGrammar(t *testing.T) {
	for _, tc := range []struct {
		expr  string
		write string // how the parse writes itself back, when it differs from expr
	}{
		// The four primitives that exist today.
		{expr: "str"}, {expr: "int"}, {expr: "float"}, {expr: "bool"},
		// Text with a validated shape.
		{expr: "Phone"}, {expr: "Date"}, {expr: "Time"}, {expr: "Id"},
		// The headline shape, which is what killed go/parser.
		{expr: `Literal["a", "b"]`},
		{expr: `list[Literal["a", "b"]]`},
		{expr: `list[Literal["create_booking", "modify_booking", "cancel_booking"]]`},
		{expr: `Literal["a","b"]`, write: `Literal["a", "b"]`},
		// Shapes, lists of shapes, and nullability on either side.
		{expr: "Appointment"},
		{expr: "list[Appointment]"},
		{expr: "Customer | None"},
		{expr: "None | Customer", write: "None | Customer"},
		{expr: "list[Appointment] | None"},
		{expr: "Customer|None", write: "Customer | None"},
		{expr: "list[ Appointment ]", write: "list[Appointment]"},
		{expr: "list[list[Appointment]]"},
		// Out of scope, and they parse. internal/ir refuses each by name.
		{expr: "datetime"}, {expr: "date"}, {expr: "time"}, {expr: "UUID"},
		{expr: "SecretStr"}, {expr: "PaymentCardNumber"},
		{expr: "dict[str, str]"}, {expr: "set[str]"}, {expr: "tuple[str, int]"},
		// Empty brackets parse too, so internal/ir can say what each one needs.
		{expr: "list[]"}, {expr: "Literal[]"},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			parsed, err := ParseType(tc.expr)
			if err != nil {
				t.Fatalf("ParseType(%q) = %v, want it to parse", tc.expr, err)
			}
			want := tc.write
			if want == "" {
				want = tc.expr
			}
			if got := parsed.String(); got != want {
				t.Errorf("ParseType(%q).String() = %q, want %q", tc.expr, got, want)
			}
		})
	}
}

// TestParseTypeRefusesWithItsColumn holds the half of Fail Loud a type
// expression owns: the refusal points at the token, not at the line. A typo
// inside a nested expression is the case that matters, because the line alone
// is `type: list[Literal["a", "b"]]` and says nothing.
func TestParseTypeRefusesWithItsColumn(t *testing.T) {
	for _, tc := range []struct {
		name   string
		expr   string
		col    int
		phrase string
	}{
		{name: "empty", expr: "", col: 1, phrase: "an empty type"},
		{name: "blank", expr: "   ", col: 1, phrase: "an empty type"},
		{name: "unclosed bracket", expr: "list[Appointment", col: 17, phrase: `closes the "["`},
		{name: "unclosed nested bracket", expr: `list[Literal["a"`, col: 17, phrase: `closes the "["`},
		{name: "bracket opened and abandoned", expr: "list[", col: 6, phrase: `closes the "["`},
		{name: "stray closing bracket", expr: "list[str]]", col: 10, phrase: "after the type has ended"},
		{name: "comma where a type belongs", expr: "list[,str]", col: 6, phrase: "where a name belongs"},
		{name: "union with nothing after it", expr: "str |", col: 6, phrase: "ends where a name belongs"},
		{name: "union with nothing before it", expr: "| str", col: 1, phrase: "where a name belongs"},
		{name: "two names with no operator", expr: "str int", col: 5, phrase: "after the type has ended"},
		{name: "single-quoted entry", expr: `Literal['a']`, col: 9, phrase: "double quotes"},
		{name: "a character no type uses", expr: "list<str>", col: 5, phrase: "not part of a type expression"},
		// The column of the *inner* token is the whole point of this test: the
		// mistake is at column 25 and the line is the same either way.
		{name: "typo deep inside", expr: `list[Literal["a", "b"] | 7]`, col: 26, phrase: "not part of a type expression"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseType(tc.expr)
			if err == nil {
				t.Fatalf("ParseType(%q) parsed, want a refusal", tc.expr)
			}
			var refusal *TypeError
			if !errors.As(err, &refusal) {
				t.Fatalf("ParseType(%q) = %v, want a *TypeError so the caller can join the file and the line", tc.expr, err)
			}
			if refusal.Col != tc.col {
				t.Errorf("ParseType(%q) refused at column %d, want column %d (%s)", tc.expr, refusal.Col, tc.col, refusal.Msg)
			}
			if !strings.Contains(refusal.Msg, tc.phrase) {
				t.Errorf("ParseType(%q) said %q, want it to contain %q", tc.expr, refusal.Msg, tc.phrase)
			}
		})
	}
}

// TestParseTypeKeepsLiteralEntriesInOrder is the shape internal/ir walks, and
// order is what a Literal set means: the refusal for a value outside the set
// lists the entries, and it lists them the way the author wrote them.
func TestParseTypeKeepsLiteralEntriesInOrder(t *testing.T) {
	parsed, err := ParseType(`list[Literal["create_booking", "modify_booking", "cancel_booking"]]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Union) != 1 {
		t.Fatalf("union holds %d atoms, want 1", len(parsed.Union))
	}
	list := parsed.Union[0]
	if list.Name != "list" || !list.Bracket || len(list.Args) != 1 {
		t.Fatalf("outer atom is %+v, want list[...] with one argument", list)
	}
	literal := list.Args[0].Union[0]
	if literal.Name != "Literal" {
		t.Fatalf("inner atom is %q, want Literal", literal.Name)
	}
	var entries []string
	for _, arg := range literal.Args {
		atom := arg.Union[0]
		if !atom.Quoted {
			t.Fatalf("Literal argument %q is not a quoted entry", atom.Name)
		}
		entries = append(entries, atom.Entry)
	}
	want := []string{"create_booking", "modify_booking", "cancel_booking"}
	if len(entries) != len(want) {
		t.Fatalf("entries = %v, want %v", entries, want)
	}
	for i := range want {
		if entries[i] != want[i] {
			t.Errorf("entry %d = %q, want %q", i, entries[i], want[i])
		}
	}
}

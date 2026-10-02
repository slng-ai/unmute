package stateschema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readModel reads one test model through its recording, the way every test
// that loads a package reads its state.py.
func readModel(t *testing.T, name string) (*Model, error) {
	t.Helper()
	dir := t.TempDir()
	source, err := os.ReadFile(filepath.Join("testdata/models", name))
	if err != nil {
		t.Fatal(err)
	}
	return Recorded{Dir: recordedDir}.Read(t.Context(), dir, source)
}

func TestReadsEveryKind(t *testing.T) {
	model, err := readModel(t, "kinds.py")
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ name, typ string }{
		{"caller_phone", "str (phone) | None"},
		{"verified", "bool"},
		{"visits", "int"},
		{"spend", "float"},
		{"contact", "str (name-email) | None"},
		{"tier", `Tier | None`},
		{"enquiry", `Literal["new", "existing"] | None`},
		{"single", `Literal["only"]`},
		{"notes", "list[str]"},
		{"customer", "Customer | None"},
		{"history", "list[Appointment]"},
		{"currency", "Literal[" + `"AED"`}, // a prefix: ISO 4217 is long
		{"language", "str | None"},
		{"greeting", "str"},
	}
	if len(model.Fields) != len(want) {
		t.Fatalf("got %d fields, want %d", len(model.Fields), len(want))
	}
	for i, w := range want {
		field := model.Fields[i]
		if field.Name != w.name {
			t.Fatalf("field %d is %s, want %s: the declared order is lost", i, field.Name, w.name)
		}
		if got := field.Type.String(); !strings.HasPrefix(got, w.typ) {
			t.Errorf("%s is %s, want %s", w.name, got, w.typ)
		}
		if field.Required {
			t.Errorf("%s reads as required, and it has a default", w.name)
		}
	}
	if phone, _ := model.Field("caller_phone"); phone.Description != "The caller's number in E.164." {
		t.Errorf("caller_phone description is %q", phone.Description)
	}
	if language, _ := model.Field("language"); language.Type.Pattern == "" {
		t.Error("LanguageAlpha2 lost its pattern")
	}
	if strings.Join(model.ExtraTypes, ",") != "currency_code,language_code,phone_numbers" {
		t.Errorf("extra types are %v", model.ExtraTypes)
	}
	notes, _ := model.Field("notes")
	greeting, _ := model.Field("greeting")
	visits, _ := model.Field("visits")
	contact, _ := model.Field("contact")
	if !notes.HasValue() || !greeting.HasValue() || !visits.HasValue() || contact.HasValue() {
		t.Errorf("HasValue: notes %v greeting %v visits %v contact %v", notes.HasValue(), greeting.HasValue(), visits.HasValue(), contact.HasValue())
	}
}

func TestPathWalksNestedModels(t *testing.T) {
	model, err := readModel(t, "kinds.py")
	if err != nil {
		t.Fatal(err)
	}
	customer, _ := model.Field("customer")
	email, err := customer.Type.Path([]string{"email"})
	if err != nil || email.String() != "str (email) | None" {
		t.Fatalf("customer.email is %v, %v", email, err)
	}
	name, err := customer.Type.Path([]string{"name"})
	if err != nil || name.String() != "str | None" {
		t.Fatalf("customer.name is %v, %v: a field under an optional model may be None", name, err)
	}
	for _, tc := range []struct {
		path []string
		want string
	}{
		{[]string{"nope"}, `has no field "nope"; it has name, email, bookings`},
		{[]string{"bookings", "day"}, "bookings is a list"},
		{[]string{"name", "first"}, "name is str"},
	} {
		if _, err := customer.Type.Path(tc.path); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: got %v, want %q", tc.path, err, tc.want)
		}
	}
}

func TestRefusesWhatAStateCannotHold(t *testing.T) {
	for file, want := range map[string]string{
		"union.py":       "either is a union of 2 types",
		"dict_field.py":  "tags is a dict",
		"recursive.py":   "holds itself through Node",
		"bad_default.py": "State.enquiry has a default its own type refuses",
		"aliased.py":     "State.phone sets an alias",
		"frozen.py":      "State is frozen",
	} {
		if _, err := readModel(t, file); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want %q", file, err, want)
		}
	}
	model, err := readModel(t, "no_default.py")
	if err != nil {
		t.Fatal(err)
	}
	if phone, _ := model.Field("phone"); !phone.Required {
		t.Error("a field with no default reads as having one")
	}
}

func TestFits(t *testing.T) {
	str := &Type{Kind: KindString}
	optional := &Type{Kind: KindString, Nullable: true}
	set := &Type{Kind: KindString, Enum: []string{"a", "b"}}
	for _, tc := range []struct {
		name     string
		src, dst *Type
		want     string
	}{
		{"same", str, str, ""},
		{"into optional", str, optional, ""},
		{"optional into plain", optional, str, "may be None"},
		{"int into float", &Type{Kind: KindInteger}, &Type{Kind: KindNumber}, ""},
		{"text into set", str, set, "allows only a, b"},
		{"subset", &Type{Kind: KindString, Enum: []string{"a"}}, set, ""},
		{"outside set", &Type{Kind: KindString, Enum: []string{"c"}}, set, "can be c"},
		{"list items", &Type{Kind: KindArray, Items: str}, &Type{Kind: KindArray, Items: set}, "allows only"},
		{"object field", &Type{Kind: KindObject}, &Type{Kind: KindObject, Fields: []Field{{Name: "id", Type: str, Required: true}}}, "has no id"},
		{"kind", &Type{Kind: KindBoolean}, str, "the value is bool"},
	} {
		err := Fits(tc.src, tc.dst)
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("%s: got %v, want %q", tc.name, err, tc.want)
		}
	}
}

func TestExtraTypesRefusesAnUnlistedModule(t *testing.T) {
	_, err := ExtraTypes([]byte("from pydantic_extra_types.color import Color\n"))
	if err == nil || !strings.Contains(err.Error(), "pydantic_extra_types.color") {
		t.Fatalf("got %v", err)
	}
}

func TestParsePropertyReadsAToolOutput(t *testing.T) {
	typ, err := ParseProperty(map[string]any{"type": "object", "properties": map[string]any{
		"status": map[string]any{"enum": []any{"done"}}, "tags": map[string]any{"type": "array"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	status, _ := typ.Field("status")
	tags, _ := typ.Field("tags")
	if status.Type.String() != `Literal["done"]` || tags.Type.String() != "list[str]" {
		t.Fatalf("status %s, tags %s", status.Type, tags.Type)
	}
}

package target

import (
	"strings"
	"testing"
)

// The pin rules are SLNG's normalize_exact_dependencies (tool.py:270), and this
// holds unmute's copy against every branch of it. The reason it matters is
// narrow and exact: the server canonicalises whatever it is given and stores the
// result, so a body carrying an uncanonical pin is not the body that exists once
// it lands.
func TestCanonicalSlngPin(t *testing.T) {
	for _, test := range []struct{ in, want string }{
		{"orjson==3.11.4", "orjson==3.11.4"},
		// PEP 503 name canonicalisation: runs of -, _ and . collapse to one dash,
		// and the whole name lowercases.
		{"Pydantic_Core==2.41.5", "pydantic-core==2.41.5"},
		{"zope..interface==5.0", "zope-interface==5.0"},
		{"  ruff == 0.15.7  ", "ruff==0.15.7"},
		// Versions already in normal form, across the shapes people write.
		{"a==1", "a==1"},
		{"a==1.4.0rc1", "a==1.4.0rc1"},
		{"a==1.2.3.post1", "a==1.2.3.post1"},
		{"a==0.1.0.dev0", "a==0.1.0.dev0"},
	} {
		got, err := CanonicalSlngPin(test.in)
		if err != nil {
			t.Errorf("%q: %v", test.in, err)
			continue
		}
		if got != test.want {
			t.Errorf("%q canonicalised to %q, want %q", test.in, got, test.want)
		}
	}
}

func TestCanonicalSlngPinRefusals(t *testing.T) {
	for _, test := range []struct{ in, want string }{
		// Every token normalize_exact_dependencies rejects outright.
		{"pkg @ https://example.com/pkg.whl", "no URL"},
		{"pkg==1.0 ; python_version < '3.12'", "no URL"},
		{"./local/pkg", "no URL"},
		{`c:\pkg`, "no URL"},
		// Extras: SLNG installs the distribution, not a variant of it.
		{"pkg[fast]==1.0", "extras"},
		// Not an exact pin.
		{"pkg>=1.0", "not an exact pin"},
		{"pkg", "not an exact pin"},
		{"pkg==1.*", "no wildcard"},
		{"pkg==1.0,<2", "no wildcard"},
		// Shapes this repository deliberately does not normalise, refused by name
		// rather than guessed at.
		{"pkg==v1.0", "in the form SLNG stores"},
		{"pkg==1.0.0-1", "in the form SLNG stores"},
		{"pkg==1!2.0", "in the form SLNG stores"},
		{"pkg==1.0+local", "in the form SLNG stores"},
		{"pkg==RC1", "in the form SLNG stores"},
		{"==1.0", "not a package name"},
	} {
		got, err := CanonicalSlngPin(test.in)
		if err == nil {
			t.Errorf("%q was accepted as %q", test.in, got)
			continue
		}
		if !strings.Contains(err.Error(), test.want) {
			t.Errorf("%q: message %q does not contain %q", test.in, err, test.want)
		}
	}
}

// Sorted by canonical name, and a duplicate is refused after canonicalisation
// rather than before: Foo_Bar and foo-bar are one package, and pinning both is a
// contradiction rather than a repetition.
func TestCanonicalSlngPinsSortAndDeduplicate(t *testing.T) {
	pins, err := CanonicalSlngPins([]string{"zope==1.0", "Acme_Client==2.0", "middle==3.0"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"acme-client==2.0", "middle==3.0", "zope==1.0"}
	for i, pin := range want {
		if pins[i] != pin {
			t.Errorf("pins = %v, want %v", pins, want)
			break
		}
	}
	if _, err := CanonicalSlngPins([]string{"Foo_Bar==1.0", "foo-bar==2.0"}); err == nil {
		t.Error("two spellings of one package were both accepted")
	} else if !strings.Contains(err.Error(), "same package") {
		t.Errorf("the message does not say they are the same package: %v", err)
	}
	// An empty list is normal, not an error: most tools need nothing.
	if pins, err := CanonicalSlngPins(nil); err != nil || len(pins) != 0 {
		t.Errorf("an empty dependency list must be legal: %v %v", pins, err)
	}
}

package ir

import (
	"strings"
	"testing"
)

// TestBuildDefaultsAnOmittedVariablesToAll is FR-013 and SC-006 at the
// resolved layer: a handoff with no `variables:` line resolves exactly as one
// writing `all`, and `all` written out is still accepted.
func TestBuildDefaultsAnOmittedVariablesToAll(t *testing.T) {
	withAll := inputsAgent(t)
	pkg := inputsPackage(t, func(yaml string) string {
		if strings.Count(yaml, "      variables: all\n") != 2 {
			t.Fatalf("the fixture no longer writes variables: all on both handoffs")
		}
		return strings.ReplaceAll(yaml, "      variables: all\n", "")
	})
	omitted, err := Build(pkg)
	if err != nil {
		t.Fatalf("a handoff with no variables: line is refused: %v", err)
	}
	for _, name := range []string{"to_specialist", "to_front"} {
		got := omitted.Controls[name].(*AgentTransfer).Context.Variables
		want := withAll.Controls[name].(*AgentTransfer).Context.Variables
		if !got.All || len(got.Names) != 0 {
			t.Errorf("%s with no variables: line resolved to %+v, want all", name, got)
		}
		if got.All != want.All || len(got.Names) != len(want.Names) {
			t.Errorf("%s resolves differently with and without the line: %+v vs %+v", name, got, want)
		}
	}
}

// TestBuildRefusesAnEmptyVariablesListNamingWhatToWrite keeps the one spelling
// that reads as if it kept something while resetting everything refused, and
// the refusal says what the two legal spellings mean.
func TestBuildRefusesAnEmptyVariablesListNamingWhatToWrite(t *testing.T) {
	pkg := inputsPackage(t, replaceOnce(t, "      variables: all\n", "      variables: []\n"))
	_, err := Build(pkg)
	if err == nil {
		t.Fatal("an empty variables: list built")
	}
	for _, fragment := range []string{`handoff "to_specialist"`, "empty list", "Leave the field out", "list the names to keep"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Errorf("refusal does not say %q:\n%v", fragment, err)
		}
	}
}

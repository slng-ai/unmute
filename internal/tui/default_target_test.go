package tui

import (
	"testing"

	"github.com/slng-ai/unmute/internal/scaffold"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// The console states the scaffold default a second time, by ordering: selectOne
// preselects options[0] positionally and never reads the current value, so head
// position is the preselect. Wherever one fact is stated twice an agreement
// test is mandatory, and this is that test.
func TestCreateMenuLeadsWithTheScaffoldDefault(t *testing.T) {
	options := createTargetOptions()
	if len(options) == 0 {
		t.Fatal("the create target menu is empty")
	}
	if got := options[0].value; got != scaffold.DefaultTarget {
		t.Errorf("the create menu preselects %q while the scaffold writes %q; "+
			"the console and the constant have drifted apart", got, scaffold.DefaultTarget)
	}
}

// The maintain menu edits a package that already has a target, so it must lead
// with that one. Leading with the scaffold default here would offer to convert
// the author's package every time they opened the menu.
//
// Both cases matter. Pipecat has historically sat at options[0], so a Pipecat
// package looked right by accident; the LiveKit case is the one that was
// actually broken.
func TestMaintainMenuLeadsWithThePackagesOwnTarget(t *testing.T) {
	for _, current := range []string{
		string(targetcap.Pipecat),
		string(targetcap.LiveKit),
	} {
		t.Run(current, func(t *testing.T) {
			options := maintainTargetOptions(current)
			if len(options) == 0 {
				t.Fatal("the maintain target menu is empty")
			}
			if got := options[0].value; got != current {
				t.Errorf("opening a %s package preselects %q; the maintain menu must "+
					"follow the package, not the scaffold default", current, got)
			}
		})
	}
}

// Both menus must keep offering every shipped target. An ordering change that
// quietly dropped one would satisfy the tests above.
func TestBothMenusStillOfferEveryShippedTarget(t *testing.T) {
	want := map[string]bool{
		string(targetcap.Pipecat): true,
		string(targetcap.LiveKit): true,
	}
	for name, options := range map[string][]menuChoice{
		"create":   createTargetOptions(),
		"maintain": maintainTargetOptions(string(targetcap.LiveKit)),
	} {
		seen := map[string]bool{}
		for _, option := range options {
			seen[option.value] = true
		}
		for value := range want {
			if !seen[value] {
				t.Errorf("the %s menu no longer offers %q", name, value)
			}
		}
	}
}

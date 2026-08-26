package tui

import (
	"strings"
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
// Every case matters, so the cases are read from targetcap.Providers. Pipecat
// has historically sat at options[0], so a Pipecat package looked right by
// accident; LiveKit is the case that was actually broken once, and slng is the
// case a two-literal list would have missed entirely.
func TestMaintainMenuLeadsWithThePackagesOwnTarget(t *testing.T) {
	for _, provider := range targetcap.Providers {
		current := string(provider)
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

// Both menus must keep offering every shipped target, and name each one
// correctly. An ordering change that quietly dropped one would satisfy the tests
// above.
//
// The want set is read from targetcap.Providers, not written here. It used to be
// two hardcoded literals, which made this the only check that could notice a
// dropped target and unable to notice one: adding a third provider left it
// green while both menus offered two, and while every screen labelled the third
// one "Pipecat". A gate whose expectation is a copy of the thing it guards
// cannot fail.
func TestBothMenusStillOfferEveryShippedTarget(t *testing.T) {
	for name, options := range map[string][]menuChoice{
		"create":   createTargetOptions(),
		"maintain": maintainTargetOptions(string(targetcap.LiveKit)),
	} {
		seen := map[string]bool{}
		for _, option := range options {
			seen[option.value] = true
		}
		for _, provider := range targetcap.Providers {
			if !seen[string(provider)] {
				t.Errorf("the %s menu no longer offers %q", name, provider)
			}
		}
	}
	// A label that reads as another target's name is worse than a missing menu
	// row, because nothing about it looks wrong.
	labels := map[string]string{}
	for _, provider := range targetcap.Providers {
		label := targetLabel(string(provider))
		if label == "" {
			t.Errorf("target %q has no label", provider)
		}
		if other, ok := labels[label]; ok {
			t.Errorf("targets %q and %q both label as %q", other, provider, label)
		}
		labels[label] = string(provider)
	}
}

// TestConsoleOffersNothingValidateRefuses covers the two remaining places the
// console gated an option by naming a provider. Both were correct with two
// targets and wrong with three, and both produced a package the author could
// not compile: the console wrote a field, and `unmute validate` then refused it.
//
// The rule is one sentence. The console may hide an option the table refuses; it
// may never offer one.
func TestConsoleOffersNothingValidateRefuses(t *testing.T) {
	table := targetcap.Default()
	for _, provider := range targetcap.Providers {
		t.Run(string(provider), func(t *testing.T) {
			// Warm transfer, formerly `data.Target != pipecat`.
			support := table.Control(targetcap.WarmTransfer, provider, "", "")
			refused := support.Tag == targetcap.Gated || support.Tag == targetcap.Provisional
			if offersWarmTransfer(string(provider)) == refused {
				t.Errorf("the transfer-mode menu and the capability table disagree: menu offers warm=%v, table refuses it=%v",
					offersWarmTransfer(string(provider)), refused)
			}
			// Advanced target settings, formerly offered to everyone. A target that
			// emits no project is refused a version, an SDK language and pins by
			// ir.validateDriverValues, so the form must not collect them.
			var data scaffold.Data
			data.Target = string(provider)
			regions := ""
			offered := advancedTargetFields(&data, &regions)
			for _, field := range offered {
				projectOnly := field.title == "Target version" ||
					strings.HasPrefix(field.title, "SDK language") ||
					strings.HasPrefix(field.title, "Pins")
				if projectOnly && !targetcap.EmitsProject(provider) {
					t.Errorf("advanced settings offers %q on %s, which emits no project and is refused it at validate", field.title, provider)
				}
			}
			if len(offered) == 0 {
				t.Error("advanced settings offers nothing at all; every target deploys somewhere")
			}
		})
	}
}

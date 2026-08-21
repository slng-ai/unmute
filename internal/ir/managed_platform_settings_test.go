package ir

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/spec"
)

// Gate C6, FR-024: a setting only a managed platform can honour is refused on a
// route that has none, and **never** on a route that has one.
//
// The enrolled list is empty on purpose (FR-024a): no setting may be added until
// somebody has confirmed it reaches no artifact on the routes being refused. So
// these tests cover the predicate and the message, with a setting enrolled for
// the length of the test, which is the only way an empty-by-design rule can have
// a gate at all.
func TestManagedPlatformSettingRefusedOnlyWhereItCannotBeHonoured(t *testing.T) {
	restore := managedPlatformOnlySettings
	managedPlatformOnlySettings = []managedPlatformSetting{{
		Name:     "test_only_setting",
		Reason:   "it is read by the managed platform's deploy command and by nothing else",
		Declared: func(*TelephonyPlan) bool { return true },
	}}
	t.Cleanup(func() { managedPlatformOnlySettings = restore })

	for name, tc := range map[string]struct {
		cloudDeploys bool
		wantRefusal  bool
	}{
		"a route with no managed platform refuses it": {false, true},
		"a route with a managed platform accepts it":  {true, false},
	} {
		t.Run(name, func(t *testing.T) {
			plan := &TelephonyPlan{
				Key:          TelephonyKey{Provider: ProviderPipecat, Transport: "sip", Carrier: "twilio"},
				CloudDeploys: tc.cloudDeploys,
			}
			var row TargetValidation
			validateManagedPlatformSettings(plan, &row)
			refused := len(row.Errors) > 0
			if refused != tc.wantRefusal {
				t.Fatalf("refused=%v, want %v: %v", refused, tc.wantRefusal, row.Errors)
			}
			if !refused {
				return
			}
			// The message has to name the route and the setting, or the author
			// cannot tell which of several targets is being complained about.
			for _, want := range []string{
				"(pipecat, sip, twilio)", "test_only_setting",
				"no managed-platform deployment path", "reaches no emitted artifact",
			} {
				if !strings.Contains(row.Errors[0], want) {
					t.Errorf("the refusal does not say %q: %s", want, row.Errors[0])
				}
			}
		})
	}
}

// The half that would have been got wrong by inferring the predicate from the
// provider. examples/salon-concierge declares a deployment region on a SIP
// route, and that route deploys either to LiveKit Cloud or to a server the author
// runs, using the same route: the region is load-bearing there. It must still
// compile with the rule live.
func TestALiveKitSIPRouteWithADeploymentRegionStillCompiles(t *testing.T) {
	restore := managedPlatformOnlySettings
	managedPlatformOnlySettings = []managedPlatformSetting{{
		Name:     "test_only_setting",
		Reason:   "enrolled for this test only",
		Declared: func(*TelephonyPlan) bool { return true },
	}}
	t.Cleanup(func() { managedPlatformOnlySettings = restore })

	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "livekit-human-transfer"))
	if err != nil {
		t.Skipf("the example is not readable from here: %v", err)
	}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatalf("the example stopped building: %v", err)
	}
	for name, resolved := range agent.Targets {
		if resolved.Telephony == nil {
			continue
		}
		if !resolved.Telephony.CloudDeploys {
			t.Errorf("target %q on route (%s, %s) reports no managed-platform path; this example's "+
				"LiveKit SIP route deploys either way and the field must say so",
				name, resolved.Telephony.Key.Provider, resolved.Telephony.Key.Transport)
			continue
		}
		var row TargetValidation
		validateManagedPlatformSettings(resolved.Telephony, &row)
		if len(row.Errors) > 0 {
			t.Errorf("target %q was refused a setting its route can honour: %v", name, row.Errors)
		}
	}
}

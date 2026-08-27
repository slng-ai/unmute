package ir

import (
	"strings"
	"testing"
)

// A package states its own name, on every target, and unmute infers none.
//
// It used to name a deployed agent after the target instance, and every package
// following the docs calls its target `slng`, `livekit` or `pipecat`. On SLNG
// and Pipecat Cloud a name is unique per organisation and a deploy updates the
// resource matching it, so two packages meant one live agent the second deploy
// overwrote. On LiveKit the same name reaches worker registration and the SIP
// dispatch rule, so two packages in one project fought over who answers a call.
func TestBuildRefusesAPackageWithNoNameOfItsOwn(t *testing.T) {
	for _, test := range []struct{ name, value string }{
		{"absent", ""},
		{"blank", "   "},
	} {
		t.Run(test.name, func(t *testing.T) {
			pkg := loadSlngCore(t)
			pkg.Agent.Name = test.value
			_, err := Build(pkg)
			if err == nil {
				t.Fatal("an unnamed package built: its deployment would be named after something that is not an identity")
			}
			// The refusal has to name the field and the shape, because "needs a
			// name" does not tell an author what they may write.
			for _, fragment := range []string{"`name:`", "lowercase letters, digits and single hyphens", "acme-support"} {
				if !strings.Contains(err.Error(), fragment) {
					t.Errorf("refusal does not contain %q: %v", fragment, err)
				}
			}
		})
	}
}

// The shape is held at one floor rather than sanitized per target, so the name
// in agent.yaml is the name that gets deployed.
func TestBuildRefusesANameThatCannotBeDeployed(t *testing.T) {
	for _, test := range []struct{ name, value, want string }{
		{"space", "acme support", "cannot be deployed as written"},
		{"uppercase", "Acme-Support", "cannot be deployed as written"},
		{"underscore", "acme_support", "cannot be deployed as written"},
		{"leading digit", "1acme", "cannot be deployed as written"},
		{"double hyphen", "acme--support", "cannot be deployed as written"},
		{"trailing hyphen", "acme-", "cannot be deployed as written"},
		{"too short", "ab", "cannot be deployed as written"},
		{"too long", strings.Repeat("a", 65), "cannot be deployed as written"},
	} {
		t.Run(test.name, func(t *testing.T) {
			pkg := loadSlngCore(t)
			pkg.Agent.Name = test.value
			_, err := Build(pkg)
			if err == nil {
				t.Fatalf("name %q built", test.value)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Errorf("refusal for %q does not contain %q: %v", test.value, test.want, err)
			}
			if !strings.Contains(err.Error(), PackageNameShape) {
				t.Errorf("refusal for %q does not say what a name may be: %v", test.value, err)
			}
		})
	}
}

// The target half exists for the collision the package half cannot solve: one
// package with two targets of the same provider would otherwise deploy the same
// name twice and overwrite itself.
func TestDeployNameJoinsThePackageToItsTarget(t *testing.T) {
	agent := &Agent{Name: "acme-support"}
	for _, test := range []struct{ target, want string }{
		{"slng", "acme-support-slng"},
		{"livekit", "acme-support-livekit"},
		// Target names are snake_case everywhere else in a package, and half the
		// sinks this lands in refuse an underscore.
		{"livekit_eu", "acme-support-livekit-eu"},
	} {
		if got := agent.DeployName(Target{Name: test.target}); got != test.want {
			t.Errorf("DeployName(%q) = %q, want %q", test.target, got, test.want)
		}
	}
	eu := agent.DeployName(Target{Name: "livekit_eu"})
	us := agent.DeployName(Target{Name: "livekit_us"})
	if eu == us {
		t.Error("two targets of one provider deploy one name, so the package overwrites itself")
	}
}

// Load's own decoding of the field, so the wiring from agent.yaml is held too:
// every test above sets Name on a loaded package, and would still pass if
// spec.Load never read the key.
func TestPackageNameIsReadFromAgentYAML(t *testing.T) {
	agent, err := Build(loadSlngCore(t))
	if err != nil {
		t.Fatal(err)
	}
	if agent.Name != "slng-core-fixture" {
		t.Errorf("Name = %q, want the name in the fixture's agent.yaml", agent.Name)
	}
}

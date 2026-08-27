package ir

import (
	"fmt"
	"regexp"
	"strings"

	packagespec "github.com/slng-ai/unmute/internal/spec"
)

// PackageNamePattern is the shape a package name may take.
//
// Lowercase letters, digits and single hyphens, because the name is joined to
// the target name and the result lands in four sinks with four different rules:
// a PEP 508 `name =` in pyproject.toml, a Pipecat Cloud agent name, a LiveKit
// agent_name, and an SLNG agent name. SLNG is the loosest (its own fixture uses
// "Support agent", spaces and all) and pyproject the strictest, so the compiler
// holds every package to a shape that is legal in all of them rather than
// sanitizing per target, which would mean the name in agent.yaml is not the name
// that gets deployed.
var PackageNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

// PackageNameShape says the same thing in words, for the author-facing message.
const PackageNameShape = "lowercase letters, digits and single hyphens, starting with a letter, 3 to 64 characters, such as acme-support"

// ValidPackageName reports whether a name can be deployed as written. One
// owner, because the scaffold has to ask the same question before it seeds a
// name from a folder.
func ValidPackageName(name string) bool {
	return len(name) >= 3 && len(name) <= 64 && PackageNamePattern.MatchString(name)
}

// DeployName is what one target's deployment of this package is called: the
// package's name joined to the target instance it was compiled for.
//
// The target half is there for the collision the package half cannot solve. One
// package with two targets of the same provider — `livekit_eu` and `livekit_us`,
// or the two slng targets a package needs to reach two SLNG regions — otherwise
// deploys the same name twice and collides with itself.
//
// Underscores in the target name become hyphens, because target names are
// snake_case everywhere else in a package and half of this result's sinks refuse
// an underscore.
func (a *Agent) DeployName(target Target) string {
	return a.Name + "-" + strings.ReplaceAll(target.Name, "_", "-")
}

// checkPackageName refuses a package that states no name, or states one that
// cannot survive the journey to every platform that will hold it.
//
// A name is required on every target because the alternative is inference, and
// both candidates are worse than they look. Unmute used to name a deployment
// after the target instance, and every package following the docs calls its
// target `slng`, `livekit` or `pipecat`: on SLNG and Pipecat Cloud a deploy
// resolves the resource to update by matching the name, so the second package
// overwrote the first, and on LiveKit two packages in one project fought over
// which worker answered a call. The folder on disk is no better: it is named by
// whoever cloned the repository and it changes silently.
func checkPackageName(pkg *packagespec.Package) error {
	name := strings.TrimSpace(pkg.Agent.Name)
	where := pkg.Location("agent.yaml", "name:")
	if name == "" {
		return fmt.Errorf("%s: agent.yaml needs a `name:`, which is what this agent is called where it is deployed: %s. "+
			"It has to be yours to choose, because the two names unmute could infer are not identities: every package calls its target `slng`, `livekit` or `pipecat`, "+
			"and the folder is named by whoever cloned the repository", where, PackageNameShape)
	}
	if !ValidPackageName(name) {
		return fmt.Errorf("%s: name %q cannot be deployed as written: a package name is %s. "+
			"It is joined to the target name and written into a pyproject `name =`, a Pipecat Cloud agent, a LiveKit agent_name and an SLNG agent, "+
			"and unmute holds one shape all four accept rather than quietly rewriting yours", where, name, PackageNameShape)
	}
	return nil
}

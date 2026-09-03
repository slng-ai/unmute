package ir

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	targetcap "github.com/slng-ai/unmute/internal/target"
)

// MirrorDigest is the offline half of the honesty check: the digest of the
// bytes a hosted tool's mirror was read from, compared against the pin the tool
// file records.
//
// Deliberately not a call to skill.Hash, which is the same three lines and was
// the first choice: internal/skill's own test imports internal/ir, so importing
// skill from here is an import cycle in test. Three lines of stdlib beat
// rearranging two packages to share them, and the algorithm is pinned locally
// by TestHostedDigestAlgorithmIsPinned for exactly the reason skill_test.go
// pins its own: a change of hash or encoding must fail in a test rather than
// silently invalidate every pin already committed.
//
// The platform's own content_hash answers a different question, needs the
// network, and warns rather than refuses. The two never meet.
func MirrorDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// validateHostedTool checks one `slng:` tool with no network at all, which is
// the requirement the whole design rests on: nothing in CI has an SLNG
// credential, so a build that cannot verify its own inputs offline cannot be
// verified at all.
func validateHostedTool(name string, tool Tool, errors *[]string) {
	sidecar := "tools/" + name + ".slng.json"
	toolFile := "tools/" + name + ".yaml"

	if tool.Mirror == nil {
		*errors = add(*errors, fmt.Sprintf(
			"tool %q: `slng:` names a tool SLNG hosts and no mirror of it is committed, so there is nothing to compile: run `unmute pull` to fetch it, and commit what it writes", name))
		return
	}
	if tool.MirrorPin == "" {
		*errors = add(*errors, fmt.Sprintf(
			"tool %q: %s is committed and %s pins no hash, so nothing proves the two belong together: run `unmute pull` to record one", name, sidecar, toolFile))
		return
	}
	// Naming both files matters. One of them is wrong and the author is the only
	// one who knows which, so the message gives both recoveries rather than
	// picking.
	if got := MirrorDigest(tool.MirrorBytes); got != tool.MirrorPin {
		mirrored := sidecar
		if tool.Mirror.Code != "" {
			mirrored = "tools/" + name + ".slng.py"
		}
		*errors = add(*errors, fmt.Sprintf(
			"tool %q: %s does not match the hash %s pins, so the committed mirror is not the one this package means: run `unmute pull` and read the diff, or `git checkout` the mirror if the edit was a mistake",
			name, mirrored, toolFile))
	}
	if tool.Mirror.Source == "curated" {
		*errors = add(*errors, fmt.Sprintf(
			"tool %q: %s mirrors a capability SLNG curates, which has no definition to mirror: attach it with `builtin: %s` instead, which needs no pull", name, sidecar, tool.Mirror.Name))
	}
	switch tool.Mirror.ToolType {
	case "code", "api_request":
	case "":
		*errors = add(*errors, fmt.Sprintf(
			"tool %q: %s records no tool_type, so no target knows how to run it: run `unmute pull` again", name, sidecar))
	default:
		*errors = add(*errors, fmt.Sprintf(
			"tool %q: %s is a %s tool, and a hosted reference carries a `code` or an `api_request` tool: reach a curated capability with `builtin:` instead",
			name, sidecar, tool.Mirror.ToolType))
	}
	// The pin shape is checked wherever dependencies are declared, not per
	// target: a range or a URL is not a dependency on any platform. Which
	// targets can install one is the separate question FieldToolDependencies
	// answers, and it answers it with a refusal on both code targets.
	if _, err := targetcap.CanonicalSlngPins(tool.Dependencies); err != nil {
		*errors = add(*errors, fmt.Sprintf("tool %q mirrored dependency: %v", name, err))
	}
}

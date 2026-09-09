package ir

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
func validateHostedMirror(name string, tool Tool, errors *[]string) {
	sidecar := "tools/" + name + ".slng.json"
	// pinFile is where the pin this mirror is checked against lives, and
	// pinMissing is how to say there is not one there yet: a legacy reference's
	// pin is the tool file's own `hash:` line, a scalar reference's is
	// generated metadata the tool file never carries, so "pins no hash" would
	// misdescribe a file that does not exist at all.
	pinFile := "tools/" + name + ".yaml"
	pinMissing := fmt.Sprintf("%s pins no hash", pinFile)
	if tool.MirrorScalar {
		pinFile = "tools/" + name + ".slng.meta.json"
		pinMissing = fmt.Sprintf("no hash is recorded in %s yet", pinFile)
	}

	if tool.MirrorFailure != "" {
		// Read before the absent case, because a mirror that is there and
		// unreadable is not a missing one: sending the author to `unmute pull`
		// for a file they already have hides the edit or the truncation that
		// actually broke it.
		*errors = add(*errors, fmt.Sprintf("tool %q: %s", name, tool.MirrorFailure))
		return
	}
	if tool.Mirror == nil {
		*errors = add(*errors, fmt.Sprintf(
			"tool %q: `slng:` names a tool SLNG hosts and this target builds a tool out of its committed mirror, and none is committed: run `unmute pull` to fetch it and commit what it writes, or compile this package to slng, which references the published tool and needs no mirror", name))
		return
	}
	if tool.MirrorPin == "" {
		*errors = add(*errors, fmt.Sprintf(
			"tool %q: %s is committed and %s, so nothing proves the two belong together: run `unmute pull` to record one", name, sidecar, pinMissing))
		return
	}
	// The mirror on disk has to still be the one this reference means, which a
	// matching digest alone does not prove: `unmute pull` names both the
	// digest and the tool it belongs to, so a reference that changed since the
	// last pull is caught here even on the astronomically unlikely chance the
	// stale bytes still happen to satisfy the digest check below.
	if tool.Mirror.Name != "" && tool.Mirror.Name != tool.HostedName {
		*errors = add(*errors, fmt.Sprintf(
			"tool %q: %s mirrors %q and this reference now means %q: the hosted name changed since the last `unmute pull`, so %s cannot be reused for it: run `unmute pull` to fetch %q's own mirror",
			name, sidecar, tool.Mirror.Name, tool.HostedName, sidecar, tool.HostedName))
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
			name, mirrored, pinFile))
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
}

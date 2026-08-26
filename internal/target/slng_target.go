package target

import (
	"fmt"
	"slices"
	"strings"
)

// The SLNG *target*'s own facts, as opposed to the SLNG *model vendor*'s, which
// live next door in slng_router.go. The word does three jobs in this repository
// now — a model vendor in the catalogues, a router provider, and a target
// provider — which is why every diagnostic this target produces opens with
// "slng target" and why these two files sit side by side rather than merged.
//
// One home for the platform facts a package is checked against, so a driver and
// ir.Validate cannot disagree about them.

// SlngTargetVerified is when the values below were last read out of
// slng-ai/backend@develop. Re-read before relying on any of them.
const SlngTargetVerified = "2026-08-25"

// SlngRegions is every deployment region SLNG accepts on a create body, from the
// pattern on VoiceAgentCreate.region (app/schemas/voice_agent.py:955).
//
// `any` is a write-time value, not a stored one: normalize_public_region
// persists it as eu-central with routing_mode unpinned and reverses it on read
// (voice_agent_regions.py:271-278). So an author who writes `any` gets `any`
// back, and SLNG picks the region per call.
var SlngRegions = []string{"any", "us-east", "eu-central", "ap-south"}

// The push tool's own surface, read from slng-ai/sdks on 2026-08-25: the CLI
// lives in that monorepo under cli/, ships as the `voiceai` binary, and reads
// its credential from the environment.
//
// These are constants rather than strings written into the runbook template,
// because the same commands appear on four surfaces — the emitted runbook, the
// example README, the docs-site page and the shipped skill — and a command that
// is wrong on one of them is wrong in the only place a reader looks.
// SlngPushCommandsAgree in slng_target_test.go is the gate.
const (
	// SlngPushCredentialEnv is the environment variable the push tool reads. It
	// is not SLNG_API_KEY, which is the Context Router's key (SlngRouterKeyEnv);
	// giving the runbook the wrong one costs an afternoon.
	SlngPushCredentialEnv = "VOICEAI_API_KEY"
	// SlngLoginCommand is the alternative to exporting the key by hand. The CLI
	// also takes `voiceai config set apiKey <token>`.
	SlngLoginCommand = "voiceai login"
	// SlngCreateCommand posts a create body. `--file -` reads stdin.
	SlngCreateCommand = "voiceai agents create --file %s --json"
	// SlngWebSessionCommand opens a browser session against one agent.
	//
	// It takes two things that both look optional and are not. The agent id is
	// positional, and leaving it off is an error rather than a shorter spelling.
	// And --file is required in practice: AgentWebSessionCreate has no required
	// properties, but the endpoint declares requestBody required, and the CLI
	// sends no body at all when --file is absent. Measured 2026-08-25: without it
	// the call fails AGENT_VALIDATION_FAILED with an empty field path, which reads
	// like a problem with the agent and is a problem with the request.
	SlngWebSessionCommand = "voiceai agents web-sessions create <agent_id> --file session.json"
)

// SlngReservedToolNames are the tool names SLNG keeps for its own curated
// capabilities (shared_tool_contract.py:545). A package tool carrying one of
// these collides with a builtin, so the value here is what to use instead.
var SlngReservedToolNames = map[string]string{
	"end_call":                   "the end_call builtin, written as `builtin: end_call`",
	"detected_answering_machine": "SLNG's voicemail_detection capability, attached to the agent in the dashboard",
	"get_current_datetime":       "SLNG's current_datetime capability, attached to the agent in the dashboard",
	"get_user_phone_number":      "SLNG's user_phone_number capability, attached to the agent in the dashboard",
	"set_runtime_variables":      "SLNG's own runtime variables, which the platform sets",
}

// SlngNetworkModules are the network clients a code tool may not import. Custom
// code on SLNG has no internet access at all; CodeConfig.egress survives only
// for historical compatibility and validates nothing (tool.py:191). A handler
// that reaches the network has to become a webhook tool.
var SlngNetworkModules = []string{"requests", "httpx", "urllib", "urllib3", "aiohttp", "http.client", "socket"}

// SlngSchemaLimits are the JSON Schema limits from the published policy manifest
// (contracts/shared_tools/v1/policy_manifest.json), checkable offline so an
// oversized tool input fails at validate rather than at push.
var SlngSchemaLimits = struct {
	Bytes      int
	Depth      int
	Nodes      int
	Properties int
	Branches   int
}{Bytes: 65536, Depth: 32, Nodes: 2048, Properties: 256, Branches: 32}

// SlngVaultNamePattern is the shape of a name in SLNG's secret store, stated in
// words because it appears in author-facing messages. The pattern itself is
// checked with vaultNamePattern in internal/ir.
const SlngVaultNamePattern = "an uppercase name starting with a letter, such as ACME_API_KEY, at most 64 characters"

// SlngDiagnostic prefixes a message so a reader can tell it from a message about
// the SLNG model vendor. Every refusal the target produces goes through here,
// which is what makes the prefix a property of the code rather than of whoever
// wrote the string (spec FR-004).
func SlngDiagnostic(format string, args ...any) string {
	message := fmt.Sprintf(format, args...)
	if strings.HasPrefix(message, "slng target") {
		return message
	}
	return "slng target " + message
}

// CheckSlngRegion reports whether a deployment region is one SLNG accepts. The
// message lists all four, because the useful part of "eu-west is wrong" is which
// ones are right.
//
// This is the only region *value* check in the tree: validateRegions checks for
// an empty or duplicated entry and forwards whatever else it is given, because
// every other target's platform owns its own region names.
func CheckSlngRegion(region string) error {
	if slices.Contains(SlngRegions, region) {
		return nil
	}
	if region == "" {
		return fmt.Errorf("%s", SlngDiagnostic("requires a deployment_region: one of %s, where any lets SLNG route the call itself",
			strings.Join(SlngRegions, ", ")))
	}
	return fmt.Errorf("%s", SlngDiagnostic("does not deploy to region %q: use one of %s, where any lets SLNG route the call itself",
		region, strings.Join(SlngRegions, ", ")))
}

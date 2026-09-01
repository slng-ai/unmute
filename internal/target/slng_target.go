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
	// SlngPushCredentialEnv is the environment variable the push tool reads.
	//
	// It is a different *name* from SlngRouterKeyEnv, not a different key: one SLNG
	// key serves every SLNG role, and the same token authenticates the Context
	// Router and the agents API. Measured 2026-08-27 by pushing with the value out
	// of an example's SLNG_API_KEY. What still costs an afternoon is exporting one
	// name and expecting the other tool to see it, which is why `unmute deploy`
	// reads both and passes on whichever it found.
	SlngPushCredentialEnv = "VOICEAI_API_KEY"
	// SlngLoginCommand is the alternative to exporting the key by hand. The CLI
	// also takes `voiceai config set apiKey <token>`.
	SlngLoginCommand = "voiceai login"
	// SlngPushPackageCommand pushes a compiled package. It takes the package root
	// or its build/slng directory, and it is the command that turns each name in
	// `tool_refs` into the identifier the API wants.
	//
	// `voiceai agents create --file build/slng/agent.json` is the wrong path and
	// deliberately not named here: it posts the body verbatim, names included, and
	// the API refuses it. That was the known gap when this target shipped; push
	// closed it.
	SlngPushPackageCommand = "voiceai agents push %s"
	// SlngDeployCommand is what unmute offers over the line above: it validates
	// the package, compiles it, and pushes it, reading the key from
	// SlngRouterKeyEnv first. One command, and the guidance a refused push carries
	// is relayed in unmute's own format.
	SlngDeployCommand = "unmute deploy %s"
	// SlngListCommand lists the organisation's agents. It is in the runbook for
	// one reason: an agent's name is its identity here, names are unique per
	// organisation, and a push under a name that already exists writes that agent
	// rather than adding one. This is how an author checks a name is free.
	SlngListCommand = "voiceai agents list"
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

// SlngResourcesVerified is when the shared-resource commands below were last
// read out of a released `voiceai` and run against a live organisation. The
// commands are stable; the *shapes* they return are the part with a shelf life,
// and internal/cli/testdata/voiceai holds captures from the same session.
const SlngResourcesVerified = "2026-08-31"

// SlngCommand is one `voiceai` invocation: the argv after the binary name.
//
// It is a slice rather than a string because both forms are needed and only one
// of them may be the owner. `deploy` execs these, and a refusal quotes them back
// to the author. Writing the argv here and the sentence somewhere else is how
// the web-session command shipped without its agent id: two copies, one of them
// wrong, and no test that could see the difference.
type SlngCommand []string

// String renders the command the way an author would type it, which is what a
// diagnostic prints and what the agreement test greps the four surfaces for.
func (c SlngCommand) String() string { return SlngPushBinary + " " + strings.Join(c, " ") }

// With appends the arguments a command takes at the call site: a secret's name,
// a server's name, an agent id. The receiver is never modified, because these
// are package-level values and one caller's append would be every caller's.
func (c SlngCommand) With(args ...string) SlngCommand {
	return append(slices.Clip(slices.Clone(c)), args...)
}

// SlngPushBinary is the tool that owns the account, the credential and every
// write. unmute opens no socket to the SLNG API and shells out to this instead.
const SlngPushBinary = "voiceai"

// The shared-resource commands, read from `voiceai` 0.1.15 on 2026-08-31.
//
// Note the singular `tool` and `mcp` against the plural `trunks`: that is the
// CLI's own spelling, not a typo here, and TestSlngPushCommandsAgree guards the
// plural `voiceai tools`, which still does not exist.
var (
	// SlngWhoami names the account before anything else runs. A lightweight auth
	// probe that spends no TTS or STT credits, so it is cheap enough to run on
	// every deploy and is the only way to say which organisation a run resolved
	// *before* writing to it. An environment key and a stored profile can belong
	// to different ones.
	SlngWhoami = SlngCommand{"whoami"}

	// SlngSecretList answers every vault question in one read: each entry carries
	// its name, its kind and whether it holds a value, so absent, empty and
	// wrong-kind are all decidable without a lookup per name.
	SlngSecretList = SlngCommand{"secret", "list"}

	// SlngSecretCreate takes a name with .With(). There is deliberately no value
	// argument: the CLI has no --value flag because argv lands in shell history
	// and in `ps`, so the value is prompted for without echo or read from stdin.
	SlngSecretCreate = SlngCommand{"secret", "create"}

	// SlngToolList is what makes a builtin check positive rather than a guess.
	// Curated capabilities appear here as ordinary tools with ids, so a reference
	// either resolves or is positively absent.
	SlngToolList = SlngCommand{"tool", "list"}

	// SlngMCPList and SlngMCPTools both read the stored capability probe rather
	// than calling the server. A server can be listed healthy and be unreachable,
	// so neither result may be rendered as a promise that the server works.
	SlngMCPList  = SlngCommand{"mcp", "list"}
	SlngMCPTools = SlngCommand{"mcp", "tools"}

	// SlngTrunksList carries `in_use_by`, one agent name, which answers "which
	// number reaches my agent" on its own. `voiceai trunks get` adds a per-agent
	// breakdown and costs the same reads, because there is no per-trunk route and
	// both enumerate every agent, so it buys nothing on the deploy path.
	SlngTrunksList = SlngCommand{"trunks", "list"}

	// SlngCallDispatch takes an agent id and a destination with .With(). It rings
	// a real phone and costs real money, so nothing calls it unasked.
	SlngCallDispatch = SlngCommand{"agents", "calls", "dispatch"}

	// SlngAgentUpdate PATCHes one agent: partial, so a body naming one field
	// changes that field and nothing else. It is how an existing SIP trunk is
	// pointed at a deployed agent, which is the one carrier-adjacent thing unmute
	// does. It still creates no trunk and buys no number; both live in the
	// dashboard, and the CLI has no command for either.
	SlngAgentUpdate = SlngCommand{"agents", "update"}
)

// SlngTrunkFields are the agent fields that attach a SIP trunk, by direction.
// Read from a live agent body on 2026-08-31. unmute's own create body emits
// neither, so a trunk is attached after the push rather than declared in the
// package: the package stays portable and names no carrier state.
var SlngTrunkFields = map[string]string{
	"inbound":  "sip_inbound_trunk_id",
	"outbound": "sip_outbound_trunk_id",
}

// SlngProfileFlag selects a stored credential profile. It is a *root* option on
// the CLI, so it goes before the subcommand and not after it. Getting that
// backwards is not an error the tool reports; it is an unknown flag on the
// subcommand, or worse, a silently different account from the one the push
// writes to.
const SlngProfileFlag = "--profile"

// SlngVaultDashboardURL is the page that fixes a missing vault entry. It is
// known because the push tool returns it on a `vault_missing` blocker.
//
// There is deliberately no constant for attaching an MCP server or creating a
// curated tool. Those pages exist, but this repository has never seen their
// URLs, and the preflight runs before the push so it cannot borrow one from a
// blocker the way printPushBlockers does. A guessed link 404s in front of an
// author who is already stuck, which is worse than a sentence naming the
// dashboard.
const SlngVaultDashboardURL = "https://app.slng.ai/vault/secrets"

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

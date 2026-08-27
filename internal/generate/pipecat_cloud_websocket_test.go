package generate

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// The offline proofs for (pipecat, cloud-websocket, twilio): the route where the
// operator hosts nothing.
//
// The fixtures are safe_core on this route, so the plain Pipecat Cloud build in
// pipecat_v1_test.go stays the baseline it already is. Each test names the
// assertion in its own name, because a promise nobody checks is a promise this
// route cannot make: "nothing is hosted by you" is a claim about absence, and
// absence is exactly what a reader cannot see in a diff.

// cloudWebsocketOptions selects a declaration shape.
type cloudWebsocketOptions struct {
	inbound    bool
	outbound   bool
	transfer   bool
	connection bool // false is the pure-inbound shape: no connection at all
	region     string
}

// cloudWebsocketArtifact is safe_core on this route in the requested shape.
func cloudWebsocketArtifact(t *testing.T, opts cloudWebsocketOptions) Artifact {
	t.Helper()
	agent, resolved := cloudWebsocketTarget(t, opts)
	artifact, err := Generate(agent, resolved, target.Default())
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

func cloudWebsocketTarget(t *testing.T, opts cloudWebsocketOptions) (*ir.Agent, ir.Target) {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	controls := []string{"hangup"}
	if opts.transfer {
		controls = append(controls, "cold_transfer")
	}
	pkg.Agent.Channels["phone"] = spec.Channel{
		Kind: "telephony", Inbound: &opts.inbound, Outbound: &opts.outbound,
		RequiredControls: controls,
	}
	configured := pkg.Targets["pipecat"]
	if opts.region != "" {
		configured.DeploymentRegion = []string{opts.region}
	}
	// Both shapes name the connection, because the connection is where the route
	// is written. What the receive-only shape drops is the `environment:` block:
	// on this route the platform terminates the carrier's stream itself, so
	// receiving a call needs no credentials (spec FR-009a).
	configured.Connection = "twilio_voice"
	pkg.Connections = map[string]spec.Connection{"twilio_voice": {
		Transport: "cloud-websocket", Carrier: "twilio",
	}}
	if opts.connection {
		conn := pkg.Connections["twilio_voice"]
		conn.Environment = map[string]string{
			"account_sid": "TWILIO_ACCOUNT_SID", "auth_token": "TWILIO_AUTH_TOKEN",
			"from_number": "TWILIO_PHONE_NUMBER",
		}
		pkg.Connections["twilio_voice"] = conn
	}
	if opts.transfer {
		// The env-name destination form: a route that composes markup out of the
		// destination is exactly where a literal would show up in emitted output.
		addColdHumanTransfer(pkg)
		control := pkg.Agent.Controls["to_human"]
		control.Cold.OnUnavailable = string(ir.OnUnavailableHangup)
		pkg.Agent.Controls["to_human"] = control
	}
	pkg.Targets = map[string]spec.Target{"pipecat": configured}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent, agent.Targets["pipecat"]
}

// telephonySection is the emitted README's route section: from its heading to the
// next top-level heading. The runbook contract is about that section, so the test
// reads that section rather than the whole file.
func telephonySection(t *testing.T, artifact Artifact) string {
	t.Helper()
	readme := artifactFile(t, artifact, "README.md")
	start := strings.Index(readme, "## Telephony setup")
	if start < 0 {
		t.Fatal("the emitted README has no Telephony setup section")
	}
	rest := readme[start+len("## Telephony setup"):]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end]
	}
	return "## Telephony setup" + rest
}

// TestCloudWebsocketEmitsNoProcessArtifact is the route's whole reason to exist,
// stated as a file list: the build is byte-for-byte the *same set of files* a
// plain Pipecat Cloud build emits. Not "no helper", not "no compose file":
// nothing new at all, because any new file is something an operator has to wonder
// about (spec FR-001, SC-005).
func TestCloudWebsocketEmitsNoProcessArtifact(t *testing.T) {
	withPhone := artifactPaths(cloudWebsocketArtifact(t, cloudWebsocketOptions{
		inbound: true, outbound: true, transfer: true, connection: true,
	}))
	// The comparison is the plain Pipecat Cloud build of the same fixture: same
	// agent, no telephony at all.
	plain := artifactPaths(plainPipecatArtifact(t))
	if !slices.Equal(withPhone, plain) {
		t.Errorf("this route's file list differs from a plain Pipecat Cloud build:\n  route: %v\n  plain: %v", withPhone, plain)
	}
	if !slices.Contains(withPhone, "pcc-deploy.toml") {
		t.Error("the build lost its deploy manifest, so there is nothing to deploy")
	}
}

// TestCloudWebsocketManifestDeclaresWebsocketAuth: the security posture is a
// visible, versioned choice on this route, and nowhere else (research D3/F7).
func TestCloudWebsocketManifestDeclaresWebsocketAuth(t *testing.T) {
	manifest := artifactFile(t, cloudWebsocketArtifact(t, cloudWebsocketOptions{inbound: true}), "pcc-deploy.toml")
	if !strings.Contains(manifest, `websocket_auth = "none"`) {
		t.Errorf("manifest does not declare websocket_auth:\n%s", manifest)
	}
	// The reason rides with the value, so nobody deletes it as noise.
	for _, want := range []string{"cannot fetch a token", "capability"} {
		if !strings.Contains(manifest, want) {
			t.Errorf("manifest states websocket_auth without %q, so the next reader has to guess why", want)
		}
	}
	// No other route's manifest gains the line: this is a claim about a security
	// setting, and one leaking onto the Daily routes would be a real change.
	for name, other := range map[string]Artifact{
		"plain Pipecat Cloud": pipecatArtifact(t, nil),
		"Daily with carrier":  dailyCarrierArtifact(t, "twilio", true),
	} {
		if strings.Contains(artifactFile(t, other, "pcc-deploy.toml"), "websocket_auth") {
			t.Errorf("the %s manifest gained websocket_auth", name)
		}
	}
}

// TestCloudWebsocketBinMarkupIsDictated: the operator substitutes exactly one
// value, and the markup they paste is complete (spec FR-003, contracts §1).
func TestCloudWebsocketBinMarkupIsDictated(t *testing.T) {
	section := telephonySection(t, cloudWebsocketArtifact(t, cloudWebsocketOptions{inbound: true}))
	for _, want := range []string{
		"<Say>Connecting you now.</Say>",                                               // cold start is never dead air (SC-002)
		`<Stream url="wss://api.pipecat.daily.co/ws/twilio">`,                          // the platform's own endpoint
		`<Parameter name="_pipecatCloudServiceHost" value="safe-core-fixture-pipecat.`, // the deployed agent name is compiled in
		"YOUR_ORGANIZATION",                // the one substitution
		"pipecat cloud organizations list", // where the one value comes from
		// Which value, because the command prints several columns whose headings
		// differ between CLI versions, and the wrong one is the most common way this
		// route fails (2026-08-13, from a live attempt: the display name was pasted
		// where the organization slug belongs, and the agent log stayed empty).
		"machine slug, not your display name",
		"three-random-words-12345",
		"never the human-readable name",
		"twilio api core incoming-phone-numbers list", // read the state, do not listen
	} {
		if !strings.Contains(section, want) {
			t.Errorf("the dictated setup is missing %q", want)
		}
	}
	// The spoken line has to come before the stream, or it plays into a call that
	// has already been handed over.
	say, connect := strings.Index(section, "<Say>Connecting you now."), strings.Index(section, "<Connect>")
	if say < 0 || connect < 0 || say > connect {
		t.Error("the spoken line does not precede <Connect>, so a cold start is dead air")
	}
	// An outbound-only package has no Bin at all: its markup rides the command.
	outbound := telephonySection(t, cloudWebsocketArtifact(t, cloudWebsocketOptions{outbound: true, connection: true}))
	if strings.Contains(outbound, "Create a TwiML Bin") {
		t.Error("an outbound-only package is told to create a Bin it never receives a call through")
	}
}

// TestCloudWebsocketMarkupCarriesNoUnreadParameter: every <Parameter> the runbook
// dictates has a reader in the emitted Python, or it is markup an operator pastes
// and then trusts. `_pipecatCloudServiceHost` is the one exemption, because the
// platform reads it before the connection ever reaches this agent.
//
// This is what from_number and to_number were: added in 815793a for a transfer
// caller ID, left behind when that signature changed, and still telling readers
// they were "how this agent learns who called" a year later (2026-08-27).
func TestCloudWebsocketMarkupCarriesNoUnreadParameter(t *testing.T) {
	artifact := cloudWebsocketArtifact(t, cloudWebsocketOptions{
		inbound: true, outbound: true, transfer: true, connection: true,
	})
	bot := artifactFile(t, artifact, "bot.py")
	names := regexp.MustCompile(`<Parameter name="([^"]+)"`).FindAllStringSubmatch(telephonySection(t, artifact), -1)
	if len(names) == 0 {
		t.Fatal("no markup parameter is dictated anywhere, so this is asserting nothing")
	}
	for _, match := range names {
		if match[1] == "_pipecatCloudServiceHost" {
			continue
		}
		if !strings.Contains(bot, match[1]) {
			t.Errorf("the dictated markup carries %q and no emitted module reads it", match[1])
		}
	}
}

// TestCloudWebsocketRegionPicksTheEndpoint: an operator who declared a region
// never learns regions exist, because every rendered address is already the right
// one (spec FR-004).
func TestCloudWebsocketRegionPicksTheEndpoint(t *testing.T) {
	regional := cloudWebsocketArtifact(t, cloudWebsocketOptions{
		inbound: true, outbound: true, transfer: true, connection: true, region: "eu-central",
	})
	host := regexp.MustCompile(`wss://[a-z0-9.\-]*api\.pipecat\.daily\.co/ws/twilio`)
	found := host.FindAllString(artifactFile(t, regional, "README.md")+artifactFile(t, regional, "bot.py"), -1)
	if len(found) == 0 {
		t.Fatal("no stream address is rendered anywhere, so the operator has nothing to paste")
	}
	for _, address := range found {
		if address != "wss://eu-central.api.pipecat.daily.co/ws/twilio" {
			t.Errorf("rendered address %q is not the declared region's; a call to the wrong region fails or degrades", address)
		}
	}
	plain := cloudWebsocketArtifact(t, cloudWebsocketOptions{inbound: true, transfer: true, connection: true})
	for _, address := range host.FindAllString(artifactFile(t, plain, "README.md")+artifactFile(t, plain, "bot.py"), -1) {
		if address != "wss://api.pipecat.daily.co/ws/twilio" {
			t.Errorf("with no region declared the rendered address is %q, not the default", address)
		}
	}

	// One declaration, and every place a region lands agrees with it. The platform
	// requires that: a regional stream endpoint routes only to agents deployed in
	// that region, and an agent can only read a secret set from its own region. So
	// the whole chain is asserted together rather than one artifact at a time.
	manifest := artifactFile(t, regional, "pcc-deploy.toml")
	if !strings.Contains(manifest, `region = "eu-central"`) {
		t.Errorf("the deploy manifest does not name the declared region:\n%s", manifest)
	}
	readme := artifactFile(t, regional, "README.md")
	if !strings.Contains(readme, "--region eu-central") {
		t.Error("the secret-set command does not carry the declared region, so the agent could not read its own secrets")
	}
	// And the README says the chain out loud, because an invariant nobody states is
	// one an operator breaks by hand on the first region change.
	for _, want := range []string{"One region, three places", "globally unique across regions", "region-scoped"} {
		if !strings.Contains(readme, want) {
			t.Errorf("the runbook does not explain the region chain: %q is missing", want)
		}
	}
}

// TestCloudWebsocketRunbookContract covers contracts/runbook.md. The counts are
// asserted against the list that follows them, because a count that drifts from
// its own list is worse than no count (spec SC-001).
func TestCloudWebsocketRunbookContract(t *testing.T) {
	section := telephonySection(t, cloudWebsocketArtifact(t, cloudWebsocketOptions{
		inbound: true, outbound: true, transfer: true, connection: true,
	}))
	if strings.Contains(section, "StartFrame not received yet") {
		t.Error("the runbook must not excuse a BusBridge startup race")
	}
	if !strings.Contains(section, "**four steps**") {
		t.Error("the section does not state the count up front")
	}
	for _, step := range []string{"\n1. ", "\n2. ", "\n3. ", "\n4. "} {
		if !strings.Contains(section, step) {
			t.Errorf("the carrier list is missing step %q, so the stated count of four is wrong", strings.TrimSpace(step))
		}
	}
	if strings.Contains(section, "\n5. ") {
		t.Error("the carrier list has a fifth step, so the stated count of four is wrong")
	}
	for _, want := range []string{
		"Nothing runs on your side and nothing is hosted by you", // part zero
		"**Nothing, in production.**",                            // part two
		"still on a SIP trunk",                                   // 006's hard-won lesson, carried over
		`websocket_auth = "none"`,                                // part five
		"like a capability",                                      //
		"pipecat-examples/tree/main/twilio-chatbot",              // the three grounding sources
		"twilio.com/docs/voice/media-streams/websocket-messages", //
		"help.twilio.com/articles/360043489573",                  //
		"voice-capable number you own in this Twilio account",    // caller identity, where the number is asked for
		"When something does not work",                           // part seven
	} {
		if !strings.Contains(section, want) {
			t.Errorf("the runbook is missing %q", want)
		}
	}
	// Every named cause in the spec's Edge Cases has a row.
	for _, cause := range []string{
		"still on a SIP trunk", "service host in your markup is wrong", "cold start", "different region",
	} {
		if !strings.Contains(section, cause) {
			t.Errorf("the troubleshooting map does not name %q", cause)
		}
	}
	// The words that would mean somebody is hosting something. Nothing on this
	// route does, and there is no local phone run left to make an exception for,
	// so none of them may appear anywhere in the runbook (spec FR-015).
	for _, forbidden := range []string{"tunnel", "cloudflared", "ngrok", "helper"} {
		if strings.Contains(section, forbidden) {
			t.Errorf("the runbook mentions %q; this route hosts nothing and has no local phone run", forbidden)
		}
	}
	// Where the reader is sent to hear this agent, and where they are told the
	// phone leg actually starts. Pinned rather than left to prose, because a
	// runbook that stays quiet about the second one is a runbook somebody reads
	// as "the browser session proves the phone works".
	before := strings.Index(section, "### Hear it before the phone")
	if before < 0 {
		t.Fatal("the runbook does not tell the reader how to hear this agent at all")
	}
	for _, want := range []string{
		"unmute dev <source-dir>",
		"no phone involved",
		"it starts at a deployed agent",
		"nothing running on your laptop can",
	} {
		if !strings.Contains(section[before:], want) {
			t.Errorf("the runbook does not say %q", want)
		}
	}
}

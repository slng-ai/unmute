package generate

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/spec"
	"github.com/slng/unmute/internal/target"
)

// The offline proofs for (pipecat, cloud-websocket, twilio): the route where the
// operator hosts nothing (SCHEMA N38, specs/007-pipecat-native-websocket).
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
	configured.Transport, configured.Carrier = "cloud-websocket", "twilio"
	if opts.region != "" {
		configured.DeploymentRegion = []string{opts.region}
	}
	if opts.connection {
		configured.Connection = "twilio_voice"
		pkg.Connections = map[string]spec.Connection{"twilio_voice": {
			Kind: "telephony", Environment: map[string]string{
				"account_sid": "TWILIO_ACCOUNT_SID", "auth_token": "TWILIO_AUTH_TOKEN",
				"from_number": "TWILIO_PHONE_NUMBER",
			},
		}}
	} else {
		configured.Connection = ""
		pkg.Connections = map[string]spec.Connection{}
	}
	if opts.transfer {
		// The env-name destination form, not safe_core's literal: a route that
		// composes markup out of the destination is exactly where a literal would
		// show up in emitted output.
		configured.Destinations = map[string]string{"billing_line": "BILLING_PHONE_NUMBER"}
	} else {
		configured.Destinations = nil
		dropHumanTransfer(pkg)
	}
	pkg.Targets = map[string]spec.Target{"pipecat": configured}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent, agent.Targets["pipecat"]
}

// The shipped example, compiled. Used where the assertion is about what an
// operator actually gets rather than about a shape the fixtures can vary.
func compileCloudWebsocketExample(t *testing.T) Artifact {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "human-transfer-cloud-twilio"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, agent.Targets["pipecat"], target.Default())
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

func hasArtifactFile(artifact Artifact, path string) bool {
	return slices.Contains(artifactPaths(artifact), path)
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
	plain := artifactPaths(dailyArtifact(t))
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
		"Daily, no carrier":   dailyArtifact(t),
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
		"<Say>Connecting you now.</Say>",                             // cold start is never dead air (SC-002)
		`<Stream url="wss://api.pipecat.daily.co/ws/twilio">`,        // the platform's own endpoint
		`<Parameter name="_pipecatCloudServiceHost" value="pipecat.`, // the agent name is compiled in
		"YOUR_ORGANIZATION",                                          // the one substitution
		`<Parameter name="from_number" value="{{From}}"/>`,           // Twilio's own Bin templating
		`<Parameter name="to_number" value="{{To}}"/>`,               //
		"pipecat cloud organizations list",                           // where the one value comes from
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
}

// TestCloudWebsocketRunbookContract covers contracts/runbook.md. The counts are
// asserted against the list that follows them, because a count that drifts from
// its own list is worse than no count (spec SC-001).
func TestCloudWebsocketRunbookContract(t *testing.T) {
	section := telephonySection(t, cloudWebsocketArtifact(t, cloudWebsocketOptions{
		inbound: true, outbound: true, transfer: true, connection: true,
	}))
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
	// The words that would mean somebody is hosting something. "helper" and
	// "ngrok" appear nowhere at all; the tunnel words appear only in the local
	// development subsection (spec FR-015).
	local := strings.Index(section, "### Hear it locally first")
	if local < 0 {
		t.Fatal("there is no local development subsection, so the tunnel words have nowhere legal to be")
	}
	production := section[:local]
	for _, forbidden := range []string{"tunnel", "cloudflared", "ngrok", "helper"} {
		if strings.Contains(production, forbidden) {
			t.Errorf("the production part of the runbook mentions %q; this route hosts nothing", forbidden)
		}
	}
	for _, forbidden := range []string{"ngrok", "helper"} {
		if strings.Contains(section, forbidden) {
			t.Errorf("the runbook mentions %q, which exists nowhere on this route", forbidden)
		}
	}
	for _, want := range []string{"cloudflared", "restored", "TwiML Bin is never touched"} {
		if !strings.Contains(section[local:], want) {
			t.Errorf("the local development subsection does not mention %q", want)
		}
	}
}

// TestCloudWebsocketEmitsNoSecretLiterals is FR-011's guard on this route: names
// only, in every emitted file. The route composes markup carrying account
// identifiers and phone numbers, so this is the one that would catch a literal
// creeping into a template.
func TestCloudWebsocketEmitsNoSecretLiterals(t *testing.T) {
	artifact := cloudWebsocketArtifact(t, cloudWebsocketOptions{
		inbound: true, outbound: true, transfer: true, connection: true,
	})
	// An E.164 literal, a Twilio account identifier, and the two common API key
	// shapes. The example destination in the runbook's outbound command is the one
	// deliberate exception and it is a documentation placeholder, so it is excluded
	// by name rather than by pattern.
	e164 := regexp.MustCompile(`\+[1-9][0-9]{9,14}`)
	for _, file := range artifact.Files {
		content := string(file.Content)
		for _, forbidden := range []string{"AC0", "sk-", "pk_", "SK0"} {
			if strings.Contains(content, forbidden) {
				t.Errorf("%s contains a secret-looking literal %q", file.Path, forbidden)
			}
		}
		for _, number := range e164.FindAllString(content, -1) {
			if number == "+15551230000" {
				continue // the reserved-range placeholder in the outbound command
			}
			t.Errorf("%s contains the phone number literal %s; destinations are environment names", file.Path, number)
		}
	}
}

// TestCloudWebsocketPureInboundAsksForNothing is the emitted half of FR-005. The
// IR tests prove the declaration is legal; this proves the build agrees, which is
// what an operator actually reads.
func TestCloudWebsocketPureInboundAsksForNothing(t *testing.T) {
	artifact := cloudWebsocketArtifact(t, cloudWebsocketOptions{inbound: true})
	env := artifactFile(t, artifact, ".env.example")
	report := artifactFile(t, artifact, "compile-report.json")
	for _, name := range []string{"TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN", "TWILIO_PHONE_NUMBER", "PIPECAT_CLOUD_ORGANIZATION"} {
		if strings.Contains(env, name) {
			t.Errorf(".env.example asks a pure-inbound package for %s; the platform receives the call without it", name)
		}
		if strings.Contains(report, name) {
			t.Errorf("the compile report lists %s for a pure-inbound package", name)
		}
	}
	// And the per-call check is empty rather than absent: the shape is the same on
	// every declaration, only its contents shrink.
	bot := artifactFile(t, artifact, "bot.py")
	// The one branch this shape needs, and the reason it exists (research F15): the
	// framework's transport factory refuses to build a carrier transport without
	// credentials for REST hangup, so this shape builds one itself with automatic
	// hangup off. Found by running the image, so it is asserted rather than trusted.
	for _, want := range []string{
		"_carrier_transport", "auto_hang_up=False", "FastAPIWebsocketTransport",
		"parse_telephony_websocket",
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("a package with no carrier credentials does not build its own transport: %q is absent", want)
		}
	}
	if !strings.Contains(bot, "CALL_REQUIRED_ENV") {
		t.Error("the emitted bot has no per-call check at all")
	}
	if !strings.Contains(bot, "_phone_session") {
		t.Error("the emitted bot never decides whether it is on a phone call")
	}
	// A transfer-declaring package's check and its report name the same set: one
	// source, asserted equal (contracts/environment.md).
	full := cloudWebsocketArtifact(t, cloudWebsocketOptions{inbound: true, outbound: true, transfer: true, connection: true})
	checked := callRequiredEnv(t, artifactFile(t, full, "bot.py"))
	want := []string{"PIPECAT_CLOUD_ORGANIZATION", "TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN", "TWILIO_PHONE_NUMBER"}
	if !slices.Equal(checked, want) {
		t.Errorf("the per-call check names %v, want %v", checked, want)
	}
	fullReport := artifactFile(t, full, "compile-report.json")
	for _, name := range want {
		if !strings.Contains(fullReport, name) {
			t.Errorf("the compile report omits %s, which the emitted bot checks for", name)
		}
	}
	// The other side of the same branch: a package that *has* credentials uses the
	// framework's path, so it must not carry the local one.
	fullBot := artifactFile(t, full, "bot.py")
	for _, forbidden := range []string{"_carrier_transport", "auto_hang_up"} {
		if strings.Contains(fullBot, forbidden) {
			t.Errorf("a credentialed package still builds its own transport (%q): the framework's path handles it", forbidden)
		}
	}
	if !strings.Contains(fullBot, "create_transport(runner_args, transport_params)") {
		t.Error("a credentialed package does not use the framework's transport factory")
	}
}

// callRequiredEnv reads the names out of the emitted CALL_REQUIRED_ENV list.
func callRequiredEnv(t *testing.T, bot string) []string {
	t.Helper()
	start := strings.Index(bot, "CALL_REQUIRED_ENV = [")
	if start < 0 {
		t.Fatal("the emitted bot has no CALL_REQUIRED_ENV")
	}
	body := bot[start:]
	body = body[:strings.Index(body, "]")]
	var names []string
	for _, match := range regexp.MustCompile(`"([A-Z][A-Z0-9_]*)"`).FindAllStringSubmatch(body, -1) {
		names = append(names, match[1])
	}
	return names
}

// TestCloudWebsocketOutboundIsPlacedAtTheCarrier: the call originates at Twilio,
// because nothing of the operator's exists to originate it (spec FR-006,
// research D6).
func TestCloudWebsocketOutboundIsPlacedAtTheCarrier(t *testing.T) {
	section := telephonySection(t, cloudWebsocketArtifact(t, cloudWebsocketOptions{
		inbound: true, outbound: true, connection: true,
	}))
	command := outboundCommand(t, section)
	for _, want := range []string{
		"api.twilio.com/2010-04-01/Accounts/", // at the carrier, not at the platform
		"/Calls.json",                         //
		`name="direction" value="outbound"`,   // how the bot knows (D6)
		"$TWILIO_PHONE_NUMBER",                // caller identity read from the environment
		"$PIPECAT_CLOUD_ORGANIZATION",         // the service host's other half
		"To=+15551230000",                     // the one thing typed
	} {
		if !strings.Contains(command, want) {
			t.Errorf("the outbound command is missing %q:\n%s", want, command)
		}
	}
	if strings.Contains(command, "api.pipecat.daily.co/v1/public") {
		t.Error("the outbound command starts a platform session; on this route the call must originate at the carrier")
	}
	// The caller-identity definition rides with the command, wherever the number is
	// asked for (spec FR-006a).
	for _, want := range []string{
		"voice-capable number you own in this Twilio account", "verified on it",
		"Geographic Permissions", "21219",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("the outbound section does not state %q", want)
		}
	}
}

func outboundCommand(t *testing.T, section string) string {
	t.Helper()
	start := strings.Index(section, "### Place an outbound call")
	if start < 0 {
		t.Fatal("the runbook has no outbound section")
	}
	rest := section[start:]
	if end := strings.Index(rest, "\n### "); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

// TestCloudWebsocketOutboundIsAbsentWhenUndeclared: a package that never calls
// out is never told how to (spec FR-006).
func TestCloudWebsocketOutboundIsAbsentWhenUndeclared(t *testing.T) {
	artifact := cloudWebsocketArtifact(t, cloudWebsocketOptions{inbound: true})
	section := telephonySection(t, artifact)
	if strings.Contains(section, "Place an outbound call") {
		t.Error("an inbound-only package's runbook tells it how to place outbound calls")
	}
	if strings.Contains(section, "Calls.json") {
		t.Error("an inbound-only package's runbook carries a call-creation request")
	}
	bot := artifactFile(t, artifact, "bot.py")
	if strings.Contains(bot, `"direction", "outbound"`) || strings.Contains(bot, "direction=outbound") {
		t.Error("an inbound-only package emits an outbound code path")
	}
}

// TestCloudWebsocketTransferUpdatesTheLiveCall: announce, then one request keyed
// on the call's own id (research F5, D7).
func TestCloudWebsocketTransferUpdatesTheLiveCall(t *testing.T) {
	bot := artifactFile(t, cloudWebsocketArtifact(t, cloudWebsocketOptions{
		inbound: true, transfer: true, connection: true,
	}), "bot.py")
	for _, want := range []string{
		`<Say>Connecting you to a colleague now.</Say>`, // the announcement, first
		`<Dial answerOnBridge="true" timeout="25">`,     // ringback rather than dead air
		`os.environ["BILLING_PHONE_NUMBER"]`,            // an env name, never a literal
		`/Calls/{call_id}.json`,                         // keyed on the live call
		`data={"Twiml": twiml}`,                         // TwiML update, the platform's own mechanism
		`_PHONE_CALL["call_id"]`,                        // the id from the parsed handshake
		"_TRANSFER_RESULT",                              // one attempt per call
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("the emitted transfer is missing %q", want)
		}
	}
	// The announcement must be inside the document, not spoken by the agent: the
	// update tears the stream down, so an agent-spoken line is cut off by its own
	// transfer (contracts/carrier-markup.md §3, amended 2026-08-13).
	if strings.Contains(bot, "transferring them to a colleague now and to please hold") {
		t.Error("the agent speaks the announcement itself; on this route the update cuts it off mid-word")
	}
	// The Daily primitive must not appear: it is the other route's mechanism.
	for _, forbidden := range []string{"sip_call_transfer", "_TRANSPORT", "DailyTransport"} {
		if strings.Contains(bot, forbidden) {
			t.Errorf("the emitted bot references %q, which belongs to the Daily route", forbidden)
		}
	}
}

// TestCloudWebsocketTransferFailurePathIsSequential: the failure verbs follow the
// dial in one document, because branching would need a hosted endpoint (D7).
func TestCloudWebsocketTransferFailurePathIsSequential(t *testing.T) {
	artifact := cloudWebsocketArtifact(t, cloudWebsocketOptions{inbound: true, transfer: true, connection: true})
	bot := artifactFile(t, artifact, "bot.py")
	dial := strings.Index(bot, "<Dial answerOnBridge")
	failure := strings.Index(bot, "Sorry, we could not reach anyone.")
	reconnect := strings.Index(bot, "<Connect><Stream")
	if dial < 0 || failure < 0 || reconnect < 0 {
		t.Fatal("the transfer markup is missing its dial, its failure line, or its reconnect")
	}
	if dial >= failure || failure >= reconnect {
		t.Error("the failure verbs do not follow the dial, so a declined transfer leaves the caller in silence")
	}
	// The reconnect names the same service host as the Bin: one helper, one value,
	// no chance of the two disagreeing (data-model section 3).
	if !strings.Contains(bot, `_pipecatCloudServiceHost" value="{_service_host()}`) {
		t.Error("the reconnect does not name the service host, so a failed transfer reaches nothing")
	}
	section := telephonySection(t, artifact)
	binHost := strings.Contains(section, `value="pipecat.YOUR_ORGANIZATION"`)
	botHost := strings.Contains(bot, `return "pipecat." + os.environ["PIPECAT_CLOUD_ORGANIZATION"]`)
	if !binHost || !botHost {
		t.Errorf("the Bin and the bot do not compose the same service host (bin=%v bot=%v)", binHost, botHost)
	}
}

// TestCloudWebsocketTransferHonestyIsWritten: the two limits are in the emitted
// README, in operator words, whenever a transfer is declared (FR-007, runbook
// part six).
func TestCloudWebsocketTransferHonestyIsWritten(t *testing.T) {
	section := telephonySection(t, cloudWebsocketArtifact(t, cloudWebsocketOptions{
		inbound: true, transfer: true, connection: true,
	}))
	for _, want := range []string{
		"brings back a fresh agent", "does not remember", "hangs up first", "Daily carrier route",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("the transfer section does not state %q", want)
		}
	}
	// Absent when no transfer is declared: nobody needs a limit of a feature they
	// did not ask for.
	plain := telephonySection(t, cloudWebsocketArtifact(t, cloudWebsocketOptions{inbound: true}))
	if strings.Contains(plain, "brings back a fresh agent") {
		t.Error("a package with no transfer is told about the transfer's limits")
	}
}

// TestCloudWebsocketRefusesWarmTransferSayingWhatItWouldTake: the compile-time
// half of the row's refusal, which is where an author actually meets it.
func TestCloudWebsocketRefusesWarmTransferSayingWhatItWouldTake(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	inbound, outbound := true, true
	pkg.Agent.Channels["phone"] = spec.Channel{
		Kind: "telephony", Inbound: &inbound, Outbound: &outbound,
		RequiredControls: []string{"warm_transfer"},
	}
	configured := pkg.Targets["pipecat"]
	configured.Transport, configured.Carrier, configured.Connection = "cloud-websocket", "twilio", "twilio_voice"
	configured.Destinations = map[string]string{"billing_line": "BILLING_PHONE_NUMBER"}
	pkg.Targets = map[string]spec.Target{"pipecat": configured}
	pkg.Connections = map[string]spec.Connection{"twilio_voice": {
		Kind: "telephony", Environment: map[string]string{
			"account_sid": "TWILIO_ACCOUNT_SID", "auth_token": "TWILIO_AUTH_TOKEN",
			"from_number": "TWILIO_PHONE_NUMBER",
		},
	}}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	// Validate returns an error *because* the target has errors, which is the point
	// here: the errors are what this test reads.
	report, _ := ir.Validate(agent, []ir.Target{agent.Targets["pipecat"]}, target.Default())
	if len(report.PerTarget) == 0 {
		t.Fatal("validate produced no rows")
	}
	joined := strings.Join(report.PerTarget[0].Errors, "\n")
	if !strings.Contains(joined, "warm transfer") {
		t.Fatalf("a warm transfer on this route validates clean: %v", report.PerTarget[0].Errors)
	}
	for _, want := range []string{"callback endpoint you host", "livekit, sip"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the warm refusal does not say %q, so the author learns the no without the fix:\n%s", want, joined)
		}
	}
}

// TestCloudWebsocketSeamIsWordsNotShapes: this route's work reaches no other
// target's output. The markers are what it added; a marker somewhere else means
// the change was made unconditionally (spec FR-014).
func TestCloudWebsocketSeamIsWordsNotShapes(t *testing.T) {
	markers := []string{
		"_pipecatCloudServiceHost", "websocket_auth", "_phone_session", "_PHONE_CALL",
		"api.pipecat.daily.co/ws/twilio", "_transfer_twiml",
	}
	for name, artifact := range map[string]Artifact{
		"plain Pipecat Cloud": pipecatArtifact(t, nil),
		"Daily, no carrier":   dailyArtifact(t),
		"Daily with carrier":  dailyCarrierArtifact(t, "twilio", true),
		"carrier-websocket":   carrierWebsocketArtifact(t, "twilio"),
	} {
		for _, file := range artifact.Files {
			for _, marker := range markers {
				if strings.Contains(string(file.Content), marker) {
					t.Errorf("%s emits %q in %s: this route's work must reach this route only", name, marker, file.Path)
				}
			}
		}
	}
	// And the carrier-specific console paths stay in the carrier half of the
	// runbook: a Twilio console path in the deploy section would be a seam in the
	// wrong place.
	readme := artifactFile(t, cloudWebsocketArtifact(t, cloudWebsocketOptions{inbound: true}), "README.md")
	deploy := readme[strings.Index(readme, "## Deploy to Pipecat Cloud"):strings.Index(readme, "## Telephony setup")]
	for _, forbidden := range []string{"console.twilio.com", "TwiML Bin", "Active Numbers"} {
		if strings.Contains(deploy, forbidden) {
			t.Errorf("the deploy section carries the carrier console path %q", forbidden)
		}
	}
}

// TestTwilioRouteComparisonNamesEveryRoute: an author with a Twilio number and a
// Pipecat target has three ways to connect them, and one section has to answer
// which (spec FR-012, SC-007).
func TestTwilioRouteComparisonNamesEveryRoute(t *testing.T) {
	doc := readRepoDoc(t, "docs", "TELEPHONY.md")
	for _, want := range []string{
		"cloud-websocket", "daily-sip", "carrier-websocket",
		// What each hosts, which is the deciding difference.
		"nothing", "helper", "Recommendation",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("docs/TELEPHONY.md's route comparison does not mention %q", want)
		}
	}
	// The claim a reader most needs, stated once and plainly.
	if !strings.Contains(doc, "no Pipecat route offers warm transfer") {
		t.Error("the comparison does not state that no Pipecat route offers warm transfer today")
	}
}

// readRepoDoc reads a document from the repository root, so a docs claim can be
// asserted rather than assumed (Principle IV).
func readRepoDoc(t *testing.T, parts ...string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

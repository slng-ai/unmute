package generate

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/spec"
	"github.com/slng/unmute/internal/target"
)

// The (pipecat, daily-sip, twilio) contracts: the emitted helper
// (contracts/forwarding-helper.md), the environment split
// (contracts/environment.md), and the README runbook (contracts/runbook.md).
//
// Every fixture here is safe_core on the carrier form, so the no-carrier
// comparisons in pipecat_v1_test.go stay the baseline they already are.

// dailyCarrierArtifact is safe_core on the Daily route with a carrier leg. The
// four Connection keys are the ones SCHEMA N37 fixed for this route, and the
// phone channel is what the carrier form makes declarable.
func dailyCarrierArtifact(t *testing.T, carrier string, outbound bool) Artifact {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	inbound := true
	pkg.Agent.Channels["phone"] = spec.Channel{
		Kind: "telephony", Inbound: &inbound, Outbound: &outbound,
		RequiredControls: []string{"cold_transfer", "hangup"},
	}
	configured := pkg.Targets["pipecat"]
	configured.Carrier, configured.Connection = carrier, "twilio_sip_daily"
	// The env-name destination form, as the shipped example uses: it is the one
	// that makes the runtime composition observable, and a committed fixture
	// should not carry a dialable literal anyway.
	configured.Destinations = map[string]string{"billing_line": "BILLING_PHONE_NUMBER"}
	pkg.Targets = map[string]spec.Target{"pipecat": configured}
	pkg.Connections = map[string]spec.Connection{"twilio_sip_daily": {
		Kind: "telephony", Environment: map[string]string{
			"account_sid": "TWILIO_ACCOUNT_SID", "auth_token": "TWILIO_AUTH_TOKEN",
			"sip_address": "SIP_TRUNK_HOSTNAME", "from_number": "SIP_FROM_NUMBER",
		},
	}}
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

// T018 / contracts/forwarding-helper.md: the helper is in the build exactly when
// a carrier is declared, and the carrier build is a *Daily* build, not a
// carrier-websocket one.
//
// The four forbidden files are the assertion that matters most. They are what a
// carrier build silently gains if any site that reads the carrier-websocket
// telephony data is left thinking this route is one of those (research item 1).
func TestCarrierHelperIsEmittedExactlyWhenACarrierIsDeclared(t *testing.T) {
	carrier := dailyCarrierArtifact(t, "twilio", true)
	paths := artifactPaths(carrier)
	if !slices.Contains(paths, "telephony_helper.py") {
		t.Fatalf("a carrier build emits no helper, so nothing answers the carrier: %v", paths)
	}
	if slices.Contains(artifactPaths(dailyArtifact(t)), "telephony_helper.py") {
		t.Error("the no-carrier Daily build emits the helper; on that form Daily's own infrastructure delivers the call")
	}
	// A carrier build still deploys to Pipecat Cloud.
	if !slices.Contains(paths, "pcc-deploy.toml") {
		t.Error("the carrier build lost its deploy manifest")
	}
	for _, forbidden := range []string{
		"telephony.py", "telephony_shared.py", "telephony_state.py", "compose.telephony.yaml",
	} {
		if slices.Contains(paths, forbidden) {
			t.Errorf("the carrier build emits %s: that is the carrier-websocket artifact set, and this route is the Daily route", forbidden)
		}
	}
	if report := artifactFile(t, carrier, "compile-report.json"); !strings.Contains(report, "telephony_helper.py") {
		t.Error("compile-report.json does not list the helper among the generated files")
	}

	helper := artifactFile(t, carrier, "telephony_helper.py")
	// Names only, never values, and no dependency on somebody else's host.
	for _, forbidden := range []string{
		"sip_username", "sip_password", // nothing on this route could authenticate with them
		"AC0", "sk-", "pk_", // secret-looking literals
		"https://hosting.", ".mp3", // a baked-in third-party asset would play silence the day it moved
	} {
		if strings.Contains(helper, forbidden) {
			t.Errorf("the rendered helper carries %q", forbidden)
		}
	}
	// The startup check names every required value, so a missing one stops the
	// process rather than failing on a live call.
	for _, want := range []string{
		"REQUIRED_ENV = [", "PIPECAT_CLOUD_API_KEY_ENV", "raise SystemExit(1)",
	} {
		if !strings.Contains(helper, want) {
			t.Errorf("the helper's startup check is missing %q", want)
		}
	}
	// And it names nothing else. The Daily key in particular: the room belongs to
	// the platform, so a helper that read a Daily key would be a helper that could
	// make a room nobody joins.
	if strings.Contains(helper, "DAILY_API_KEY") {
		t.Error("the helper reads DAILY_API_KEY, so it can create rooms the platform never tells the agent about")
	}
}

// T019 / contracts/environment.md: who reads which name. The split exists so a
// key that starts agent sessions does not travel to the deployed agent.
func TestCarrierEnvironmentSplitsAgentSideFromHelperSide(t *testing.T) {
	carrier := dailyCarrierArtifact(t, "twilio", true)
	env := artifactFile(t, carrier, ".env.example")
	readme := artifactFile(t, carrier, "README.md")
	report := artifactFile(t, carrier, "compile-report.json")

	// One helper-side name, not two. There is no outbound trigger token because
	// there is no endpoint here that places a call.
	helperOnly := []string{"PIPECAT_CLOUD_API_KEY"}
	optional := []string{"UNMUTE_HOLD_AUDIO_URL", "UNMUTE_DAILY_ROOM_GEO"}
	agentSide := []string{"TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN", "SIP_TRUNK_HOSTNAME", "DAILY_API_KEY"}

	for _, name := range append(append(slices.Clone(helperOnly), optional...), agentSide...) {
		if !strings.Contains(env, name) {
			t.Errorf(".env.example does not carry %q", name)
		}
	}
	for _, name := range append(slices.Clone(helperOnly), agentSide...) {
		if !strings.Contains(report, name) {
			t.Errorf("compile-report.json does not carry %q", name)
		}
	}
	// The optional names are marked optional rather than presented as required:
	// unset is a supported state on both.
	for _, name := range optional {
		if !strings.Contains(env, "# "+name+"=") {
			t.Errorf(".env.example presents %q as required; it is optional", name)
		}
		if strings.Contains(report, `"`+name+`"`) {
			t.Errorf("compile-report.json lists optional %q as required environment", name)
		}
	}
	// The secret-set instructions name only what the deployed agent reads.
	deploy := readme[strings.Index(readme, "## Deploy to Pipecat Cloud"):]
	deploy = deploy[:strings.Index(deploy, "## Telephony setup")]
	for _, name := range helperOnly {
		if strings.Contains(deploy, name) {
			t.Errorf("the deploy section names helper-only %q; the deployed agent never reads it", name)
		}
	}
	if !strings.Contains(readme, "Agent side:") || !strings.Contains(readme, "Helper side:") {
		t.Error("the README's required-environment section does not mark which side reads which name")
	}
	// The no-carrier build's env file must not have moved at all.
	if got := artifactFile(t, dailyArtifact(t), ".env.example"); strings.Contains(got, "helper side") {
		t.Errorf("the no-carrier .env.example grew carrier wording:\n%s", got)
	}
}

// T024 / contracts/runbook.md: the runbook exists once, states its own cost, and
// keeps the carrier out of the platform half.
func TestCarrierRunbookContract(t *testing.T) {
	readme := artifactFile(t, dailyCarrierArtifact(t, "twilio", true), "README.md")
	if got := strings.Count(readme, "## Telephony setup"); got != 1 {
		t.Fatalf("the runbook appears %d times, want exactly once", got)
	}
	if strings.Contains(artifactFile(t, dailyArtifact(t), "README.md"), "## Telephony setup") {
		t.Error("the no-carrier Daily README grew a carrier runbook")
	}
	// The stated counts have to match what is rendered. Four carrier actions
	// because this fixture declares outbound and a cold transfer; two commands.
	if !strings.Contains(readme, "four actions in your carrier's") {
		t.Error("the runbook does not state its carrier action count up front")
	}
	if !strings.Contains(readme, "two commands to run here") {
		t.Error("the runbook does not state its command count up front")
	}
	section := readme[strings.Index(readme, "## Telephony setup"):]
	carrierPart := section[strings.Index(section, "### At your carrier"):strings.Index(section, "### On this side")]
	for _, n := range []string{"1. **", "2. **", "3. **", "4. **"} {
		if !strings.Contains(carrierPart, n) {
			t.Errorf("the carrier part is missing dictated action %s", n)
		}
	}
	if strings.Contains(carrierPart, "5. **") {
		t.Error("the carrier part renders more actions than it states")
	}

	// The platform half must read correctly for any SIP-capable carrier.
	platform := section[strings.Index(section, "### On this side"):]
	for _, name := range []string{"Twilio", "twilio", "Telnyx", "Plivo"} {
		if strings.Contains(platform, name) {
			t.Errorf("the platform part names %q, so it would not read correctly for another carrier", name)
		}
	}

	// Forbidden content: nothing on this route could hold termination credentials,
	// and its transfer sends the carrier no REFER.
	for _, forbidden := range []string{
		"Credential List", "credential list you", "SIP REFER) to Enabled", "Enable PSTN Transfer",
	} {
		if strings.Contains(section, forbidden) {
			t.Errorf("the runbook carries %q, which is false on this route", forbidden)
		}
	}
	// Every environment reference is a name.
	for _, want := range []string{
		"`SIP_TRUNK_HOSTNAME`",
		"One number serves one target at a time",
		"Take it off the trunk first, then set the webhook", // moving a number here
		"put it back on the trunk",                          // and moving it back
		"both legs keep billing",                            // the transfer cost
		"the recipient sees is governed",                    // caller identity, carrier-governed
		"https://ip-info.daily.co/ips/ip-info.json",         // the allow-list source
		"three days ahead",                                  // its change lead
		"When something does not work",                      // the troubleshooting map
		// The end-to-end loop, so nothing has to be pieced together: recompile,
		// move in, secrets, deploy, then one named test per declared flow.
		"unmute compile <source-dir>",
		"cd <source-dir>/build/",
		"pipecat cloud secrets set",
		"pipecat cloud agent status",
		"Test an incoming call", "Test an outgoing call", "Test a transfer, both ways",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("the runbook is missing %q", want)
		}
	}
	// The token is gone along with the endpoint it guarded. A README that still
	// names it would send an operator looking for a value nothing reads.
	if strings.Contains(readme, "UNMUTE_OUTBOUND_TOKEN") {
		t.Error("the README names an outbound token; the helper has no endpoint that places a call")
	}
}

// T025 / US1 scenario 3: however many times the room's SIP leg signals ready, the
// live call moves once.
//
// Asserted against the rendered text the same way TestUS2_DailyTransferAttemptsOnce
// asserts the transfer guard, and asserted *structurally*: the guard has to be
// claimed before the request is issued, because a second signal arriving while the
// first forward is in flight would otherwise slip past it.
func TestCarrierCallIsForwardedOnce(t *testing.T) {
	bot := artifactFile(t, dailyCarrierArtifact(t, "twilio", false), "bot.py")
	handler := strings.Index(bot, "async def on_dialin_ready(")
	if handler < 0 {
		t.Fatal("the carrier bot registers no ready handler, so the call is never forwarded")
	}
	body := bot[handler:]
	body = body[:strings.Index(body, "logger.info(\"carrier call")]
	claim := strings.Index(body, "_CALL_FORWARDED = True")
	request := strings.Index(body, "_forward_carrier_call(")
	if claim < 0 || request < 0 {
		t.Fatalf("the ready handler does not both claim the guard and forward:\n%s", body)
	}
	if claim > request {
		t.Errorf("the guard is claimed after the forwarding request, so a second ready signal can forward twice:\n%s", body)
	}
	// And the second event returns before doing anything.
	early := strings.Index(body, "if _CALL_FORWARDED:")
	if early < 0 || early > claim {
		t.Errorf("nothing stops a second ready event from re-entering the handler:\n%s", body)
	}
	if !strings.Contains(body[early:claim], "return") {
		t.Errorf("the guard check does not return early:\n%s", body[early:claim])
	}
}

// T033 / US2: outbound is started against the platform, not against the helper.
//
// The helper exists because an *incoming* call needs a room whose SIP address does
// not exist yet. Dialling out has no such problem, so it takes one command against
// the platform's start endpoint, exactly as it does on a Daily-provisioned number.
// The helper therefore has no endpoint that spends money, and so needs no bearer
// token to guard one: the value an operator never has to invent.
func TestCarrierOutboundIsStartedAgainstThePlatformNotTheHelper(t *testing.T) {
	artifact := dailyCarrierArtifact(t, "twilio", true)
	helper := artifactFile(t, artifact, "telephony_helper.py")
	for _, forbidden := range []string{"/outbound", "start_dialout", "UNMUTE_OUTBOUND_TOKEN", "status_code=401"} {
		if strings.Contains(helper, forbidden) {
			t.Errorf("the helper carries %q; placing calls is not its job", forbidden)
		}
	}
	if strings.Contains(helper, "pstn.twilio.com") {
		t.Error("the helper bakes in a termination address; that value is the operator's and travels by name")
	}

	// The README's one command, reading the trunk address from the environment so
	// only the destination is ever typed.
	readme := artifactFile(t, artifact, "README.md")
	for _, want := range []string{
		"### Place an outbound call", "/v1/public/",
		`\"enable_dialout\": true`,
		`\"direction\": \"outbound\"`,
		`@$SIP_TRUNK_HOSTNAME`,
		"$PIPECAT_CLOUD_API_KEY",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("the outbound command is missing %q", want)
		}
	}

	// And the bot's side of the leg, which is where the dial actually happens.
	bot := artifactFile(t, artifact, "bot.py")
	for _, want := range []string{
		"await transport.start_dialout(", `"provider": "daily"`,
		`carrier_call["dialout"]["sip_uri"]`,
		"on_dialout_connected", "on_dialout_stopped", "on_dialout_warning",
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("the carrier bot's outbound block is missing %q", want)
		}
	}
	// A package with no outbound gets neither the command nor the handlers.
	quiet := dailyCarrierArtifact(t, "twilio", false)
	if strings.Contains(artifactFile(t, quiet, "README.md"), "### Place an outbound call") {
		t.Error("a package declaring no outbound gets an outbound command anyway")
	}
	if strings.Contains(artifactFile(t, quiet, "bot.py"), "start_dialout") {
		t.Error("a package declaring no outbound emits dial-out code anyway")
	}
}

// The room an incoming carrier call joins is the platform's, asked for by the
// helper and handed to the agent the ordinary way. Proven on 2026-08-13 against
// the platform's own base image: a start with no room reaches the bot as
// PipecatSessionArguments, which the runner's transport factory refuses by name,
// so a helper-made room would have left every inbound call joining nothing.
//
// The consequence this test guards is that the room is named in exactly one
// place — the platform's own hand-over — and the room's SIP address in exactly
// one place: the event that says it is live.
func TestCarrierInboundJoinsThePlatformsRoom(t *testing.T) {
	carrier := dailyCarrierArtifact(t, "twilio", true)
	helper := artifactFile(t, carrier, "telephony_helper.py")
	for _, want := range []string{
		`"createDailyRoom": True`,
		`"dailyRoomProperties": _room_properties(caller)`,
		`"sip_mode": "dial-in"`,
		`"provider": "daily"`, // the carrier interconnect, not a Daily-bought number
		`"body": {"direction": "inbound", "call_sid": call_sid}`,
	} {
		if !strings.Contains(helper, want) {
			t.Errorf("the helper's start request is missing %q", want)
		}
	}
	// The helper asks for a room; it does not make one. Either of these names
	// means it went back to minting rooms the platform knows nothing about.
	for _, forbidden := range []string{"configure(", "sip_endpoint"} {
		if strings.Contains(helper, forbidden) {
			t.Errorf("the helper carries %q, so it is creating the room itself again", forbidden)
		}
	}

	bot := artifactFile(t, carrier, "bot.py")
	entry := bot[strings.Index(bot, "async def bot("):]
	entry = entry[:strings.Index(entry, "async def console_main")]
	if !strings.Contains(entry, "create_transport(runner_args, transport_params)") {
		t.Errorf("the entry point does not take its transport from the runner:\n%s", entry)
	}
	if strings.Contains(entry, "DailyTransport(") {
		t.Errorf("the entry point builds a transport by hand, so it is not in the room the platform made:\n%s", entry)
	}
	// One field on an inbound body: the call to reach back to. The room and its SIP
	// address both arrive from elsewhere, so requiring them here would be
	// requiring the helper to know things it is not told.
	if !strings.Contains(bot, `required = ("call_sid",)`) {
		t.Error("an inbound call body requires more than the call it has to reach back to")
	}
	// The forward uses the address the ready event carries, not one copied through
	// the body: the address is only usable once that event says so.
	if !strings.Contains(bot, "async def on_dialin_ready(transport, sip_endpoint)") {
		t.Error("the dial-in-ready handler ignores the address the event gives it")
	}
	if !strings.Contains(bot, `_forward_carrier_call(carrier_call["call_sid"], sip_endpoint)`) {
		t.Error("the forward does not use the address the ready event carried")
	}
	// Nothing here needs the transport class, so a carrier build without a transfer
	// must not import it.
	quiet := artifactFile(t, dailyCarrierArtifactWithoutTransfer(t), "bot.py")
	if strings.Contains(quiet, "import DailyParams, DailyTransport") {
		t.Error("a carrier build with no transfer imports DailyTransport without using it")
	}
}

// T034 / US2 scenario 1 and the anti-banner rule specs/004 set: the carrier form
// inherits the dial-out prerequisite when it needs one, and carries no
// prerequisite text at all when it does not.
func TestCarrierReportsTheDialOutPrerequisiteOnlyWhenNeeded(t *testing.T) {
	needs := dailyCarrierArtifact(t, "twilio", true)
	for _, path := range []string{"README.md", "compile-report.json"} {
		content := artifactFile(t, needs, path)
		for _, want := range []string{"daily_dialout", "dial-out"} {
			if !strings.Contains(content, want) {
				t.Errorf("%s does not name %q for a package that dials out and transfers", path, want)
			}
		}
	}
	// T006's wording: the approval covers a SIP address as well as a number, and a
	// carrier target needs no purchased Daily number.
	readme := artifactFile(t, needs, "README.md")
	for _, want := range []string{"dialling a SIP URI", "needs no purchased Daily number"} {
		if !strings.Contains(readme, want) {
			t.Errorf("the prerequisite summary does not say %q, which is the whole reason it applies here", want)
		}
	}

	// Neither outbound nor a transfer: no prerequisite anywhere.
	quiet := dailyCarrierArtifactWithoutTransfer(t)
	for _, path := range []string{"README.md", "compile-report.json"} {
		content := artifactFile(t, quiet, path)
		for _, forbidden := range []string{"daily_dialout", "Account prerequisites", "route_prerequisites"} {
			if strings.Contains(content, forbidden) {
				t.Errorf("%s names %q for a carrier package that never dials out", path, forbidden)
			}
		}
	}
}

// dailyCarrierArtifactWithoutTransfer is the inbound-only carrier form: no
// outbound direction and no transfer, so nothing on it dials out.
func dailyCarrierArtifactWithoutTransfer(t *testing.T) Artifact {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	inbound, outbound := true, false
	pkg.Agent.Channels["phone"] = spec.Channel{
		Kind: "telephony", Inbound: &inbound, Outbound: &outbound,
		RequiredControls: []string{"hangup"},
	}
	dropHumanTransfer(pkg)
	phone := pkg.Agent.Channels["phone"]
	phone.Inbound, phone.Outbound = &inbound, &outbound
	pkg.Agent.Channels["phone"] = phone
	configured := pkg.Targets["pipecat"]
	configured.Carrier, configured.Connection = "twilio", "twilio_sip_daily"
	pkg.Targets = map[string]spec.Target{"pipecat": configured}
	pkg.Connections = map[string]spec.Connection{"twilio_sip_daily": {
		Kind: "telephony", Environment: map[string]string{
			"account_sid": "TWILIO_ACCOUNT_SID", "auth_token": "TWILIO_AUTH_TOKEN",
			"sip_address": "SIP_TRUNK_HOSTNAME", "from_number": "SIP_FROM_NUMBER",
		},
	}}
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

// T039 / US3: on a carrier target the transfer leaves through the operator's own
// trunk; on a Daily-only target it is byte-identical to today.
func TestCarrierColdTransferDialsThroughTheOperatorTrunk(t *testing.T) {
	carrier := artifactFile(t, dailyCarrierArtifact(t, "twilio", true), "bot.py")
	daily := artifactFile(t, dailyArtifact(t), "bot.py")

	if !strings.Contains(carrier, `{"toEndPoint": _carrier_sip(os.environ["BILLING_PHONE_NUMBER"])}`) {
		t.Error("the carrier transfer does not compose its destination at the trunk's termination address")
	}
	if !strings.Contains(carrier, `f"sip:{destination}@{os.environ[\"SIP_TRUNK_HOSTNAME\"]}"`) &&
		!strings.Contains(carrier, `os.environ["SIP_TRUNK_HOSTNAME"]`) {
		t.Error("the composition does not read the Connection's sip_address name")
	}
	// A destination that is already a SIP URI is dialled as written, not wrapped.
	if !strings.Contains(carrier, `destination.startswith(("sip:", "sips:"))`) {
		t.Error("a destination that is already a SIP URI would be composed a second time")
	}
	if strings.Contains(daily, "_carrier_sip") {
		t.Error("the Daily-only transfer changed shape; it dials E.164 through Daily exactly as before")
	}
	// specs/004 already proved these two, so this only pins that they did not move.
	for _, want := range []string{
		"_TRANSFER_RESULT",
		`_TRANSFER_RESULT = {"transferred": True}`,
	} {
		for label, bot := range map[string]string{"carrier": carrier, "daily": daily} {
			if !strings.Contains(bot, want) {
				t.Errorf("%s bot lost the at-most-one-attempt guard piece %q", label, want)
			}
		}
	}
	// And the caller-stays-connected branch is still there on both.
	for label, bot := range map[string]string{"carrier": carrier, "daily": daily} {
		if !strings.Contains(bot, "The transfer could not be completed") {
			t.Errorf("%s bot lost its transfer failure branch", label)
		}
	}
}

// T040 / specs/004 FR-011: everything the transfer path reads is in the startup
// check, so a missing value fails by name rather than as a failed transfer on a
// call somebody is paying for.
func TestCarrierTransferValuesAreInTheStartupCheck(t *testing.T) {
	bot := artifactFile(t, dailyCarrierArtifact(t, "twilio", true), "bot.py")
	// Two checks, and every name is in exactly one of them. REQUIRED_ENV runs on
	// every session; CALL_REQUIRED_ENV runs only once the body says this is a phone
	// call, because a browser or console session on this package reads the carrier
	// names not at all and must not be asked for them (US1 scenario 5). Both run
	// before the call is answered, which is what the requirement is about.
	list := func(name string) string {
		out := bot[strings.Index(bot, name+" = ["):]
		return out[:strings.Index(out, "]")]
	}
	required := list("REQUIRED_ENV") + list("CALL_REQUIRED_ENV")
	for _, name := range []string{
		"BILLING_PHONE_NUMBER",                    // the destination
		"SIP_TRUNK_HOSTNAME",                      // the trunk the transfer leg leaves through
		"DAILY_API_KEY",                           // the transfer primitive's own key
		"TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN", // the forwarding request
	} {
		if !strings.Contains(required, name) {
			t.Errorf("the startup check omits %q, so a missing value first shows up as a failed call:\n%s", name, required)
		}
	}
	// No name in both: a value listed twice invites the two lists drifting apart.
	for _, name := range []string{"DAILY_API_KEY", "BILLING_PHONE_NUMBER", "TWILIO_ACCOUNT_SID"} {
		if strings.Count(required, `"`+name+`"`) != 1 {
			t.Errorf("%q appears in both startup checks", name)
		}
	}
	// And a browser session on this package is not asked for a carrier credential.
	if strings.Contains(list("REQUIRED_ENV"), "TWILIO_") {
		t.Error("the process-wide check demands a carrier credential, so `unmute dev` in the browser would refuse to start")
	}
}

// T046 / US4: adding a carrier later is writing words, not shapes.
//
// The route table grants (pipecat, daily-sip) to Twilio only, so a second carrier
// cannot compile: that is the deliberate gate, and it is what makes the seam
// testable at the template level rather than through Generate. Rendered directly
// with a second carrier's data, the platform half must come out byte-identical and
// the carrier half must fall back to generic prose.
func TestCarrierSeamIsWordsNotShapes(t *testing.T) {
	base := dailyCarrierArtifact(t, "twilio", true)
	data := carrierRenderData(t)

	twilio, err := renderPipecatV1("README.md", data)
	if err != nil {
		t.Fatal(err)
	}
	data.DailyCarrier.Carrier = "telnyx"
	second, err := renderPipecatV1("README.md", data)
	if err != nil {
		t.Fatal(err)
	}

	platform := func(readme string) string {
		section := readme[strings.Index(readme, "## Telephony setup"):]
		return section[strings.Index(section, "### On this side"):]
	}
	if platform(string(twilio)) != platform(string(second)) {
		t.Error("the platform half of the runbook changed with the carrier, so it is not a seam")
	}
	secondCarrierPart := string(second)
	secondCarrierPart = secondCarrierPart[strings.Index(secondCarrierPart, "### At your carrier"):strings.Index(secondCarrierPart, "### On this side")]
	if strings.Contains(secondCarrierPart, "Twilio Console") {
		t.Error("a second carrier gets Twilio's console paths")
	}
	if !strings.Contains(secondCarrierPart, "https://docs.pipecat.ai/pipecat/telephony/daily-sip") {
		t.Error("a second carrier's generic prose does not point at the route's documentation")
	}
	// The obligations are still dictated, in the carrier's own terms.
	for _, want := range []string{"voice-capable number", "https://ip-info.daily.co/ips/ip-info.json"} {
		if !strings.Contains(secondCarrierPart, want) {
			t.Errorf("a second carrier's block drops the obligation %q", want)
		}
	}

	// And the helper names no carrier at all: it is the same file whoever carries
	// the call.
	helper := artifactFile(t, base, "telephony_helper.py")
	for _, name := range []string{"Twilio", "twilio", "Telnyx", "telnyx", "Plivo", "plivo"} {
		if strings.Contains(helper, name) {
			t.Errorf("the rendered helper names carrier %q", name)
		}
	}
}

// carrierRenderData is the carrier build's template data, for template-level
// assertions that need a carrier the route table does not grant.
func carrierRenderData(t *testing.T) pipecatData {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	inbound, outbound := true, true
	pkg.Agent.Channels["phone"] = spec.Channel{
		Kind: "telephony", Inbound: &inbound, Outbound: &outbound,
		RequiredControls: []string{"cold_transfer", "hangup"},
	}
	configured := pkg.Targets["pipecat"]
	configured.Carrier, configured.Connection = "twilio", "twilio_sip_daily"
	pkg.Targets = map[string]spec.Target{"pipecat": configured}
	pkg.Connections = map[string]spec.Connection{"twilio_sip_daily": {
		Kind: "telephony", Environment: map[string]string{
			"account_sid": "TWILIO_ACCOUNT_SID", "auth_token": "TWILIO_AUTH_TOKEN",
			"sip_address": "SIP_TRUNK_HOSTNAME", "from_number": "SIP_FROM_NUMBER",
		},
	}}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	data, err := buildPipecatData(agent, agent.Targets["pipecat"])
	if err != nil {
		t.Fatal(err)
	}
	return data
}

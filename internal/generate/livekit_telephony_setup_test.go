package generate

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
)

// Telephony setup is one runbook in the README and two record inputs next to it.
// None of it can be proven by a live call in CI, so the operator-facing
// behaviour is pinned here.

// livekitSIPFixture builds a LiveKit SIP package on one carrier. inbound and
// outbound are the phone channel's directions; cold selects safe_core's own cold
// transfer over a warm one, which is what turns the carrier transfer toggles
// into a required step.
func livekitSIPFixture(t *testing.T, carrier string, inbound, outbound, cold bool) (*ir.Agent, ir.Target) {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	addColdHumanTransfer(pkg)
	controls := []string{"hangup"}
	if cold {
		controls = append(controls, "cold_transfer")
	} else {
		controls = append(controls, "warm_transfer")
	}
	pkg.Agent.Channels["phone"] = spec.Channel{
		Kind: "telephony", Inbound: &inbound, Outbound: &outbound, RequiredControls: controls,
	}
	configured := pkg.Targets["livekit"]
	configured.Connection = "primary_phone"
	setConnectionRoute(pkg, "primary_phone", "sip", carrier)
	pkg.Targets = map[string]spec.Target{"livekit": configured}
	connection := pkg.Connections["primary_phone"]
	connection.Environment = map[string]string{
		"sip_address": "SIP_TRUNK_HOSTNAME", "sip_username": "SIP_AUTH_USERNAME",
		"sip_password": "SIP_AUTH_PASSWORD", "from_number": "SIP_FROM_NUMBER",
	}
	pkg.Connections["primary_phone"] = connection
	if !cold {
		human := pkg.Agent.Escalations["to_human"]
		human.Cold = nil
		human.Warm = &spec.WarmTransfer{Destination: "billing_line"}
		pkg.Agent.Escalations["to_human"] = human
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent, agent.Targets["livekit"]
}

func generateSIPFixture(t *testing.T, carrier string, inbound, outbound, cold bool) Artifact {
	t.Helper()
	agent, resolved := livekitSIPFixture(t, carrier, inbound, outbound, cold)
	artifact, err := GenerateLiveKit(agent, resolved, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

// section returns one markdown section of a README, heading line included, so a
// test can compare or search inside it without matching the whole file. It ends
// at the next heading of the same or a higher level; a `#` comment inside a code
// fence is not one, because those are never two or more hashes.
func section(t *testing.T, readme, heading string) string {
	t.Helper()
	lines := strings.Split(readme, "\n")
	start := slices.Index(lines, heading)
	if start < 0 {
		t.Fatalf("README has no %q section", heading)
	}
	level := strings.Count(strings.Fields(heading)[0], "#")
	for i := start + 1; i < len(lines); i++ {
		fields := strings.Fields(lines[i])
		if len(fields) == 0 {
			continue
		}
		hashes := strings.Count(fields[0], "#")
		if fields[0] == strings.Repeat("#", hashes) && hashes >= 2 && hashes <= level {
			return strings.Join(lines[start:i], "\n")
		}
	}
	return strings.Join(lines[start:], "\n")
}

// FR-004: the two record inputs, and the fact that nothing shells out to `lk`
// on the author's behalf any more.
//
// The retired script is why this is a contract and not a preference. It called
// bare `lk`, which reads the CLI's default project and takes no flag to override
// it, so on a machine whose default was another account it created both records
// in the wrong project and printed success. Its reuse check asked only whether
// some rule named the trunk, so a rule with an empty agent list counted as a hit
// and a re-run fixed nothing.
func TestTelephonyRecordInputsHoldTheirContract(t *testing.T) {
	artifact := generateSIPFixture(t, "twilio", true, true, true)
	if artifactHasFile(artifact, "telephony-setup.sh") {
		t.Error("the build still ships a setup script; the runbook's own commands are the contract")
	}
	dispatch := artifactFile(t, artifact, "sip-dispatch-rule.json")
	if !strings.Contains(dispatch, `"${UNMUTE_SIP_TRUNK_ID}"`) {
		t.Error("sip-dispatch-rule.json does not carry the substitution token")
	}
	// FR-005, the wildcard ban: an absent or empty trunk list matches every
	// trunk in the project, so the key is always present and always populated.
	if strings.Contains(dispatch, `"trunk_ids": []`) || !strings.Contains(dispatch, `"trunk_ids"`) {
		t.Error("sip-dispatch-rule.json has an empty or missing trunk_ids")
	}
	trunk := artifactFile(t, artifact, "sip-inbound-trunk.json")
	if !strings.Contains(trunk, `"${SIP_FROM_NUMBER}"`) {
		t.Error("sip-inbound-trunk.json does not carry the phone-number token the runbook substitutes")
	}
	// Digest auth on the inbound trunk rejects every carrier call, because
	// origination identifies itself by source IP and sends no credentials.
	for _, forbidden := range []string{"authUsername", "authPassword", "auth_username", "auth_password"} {
		if strings.Contains(trunk, forbidden) {
			t.Errorf("sip-inbound-trunk.json sets %q; carrier origination sends no credentials", forbidden)
		}
	}
	// The two tokens the runbook substitutes are the only ones in either file,
	// so nothing else has to be looked up by hand.
	token := regexp.MustCompile(`\$\{([A-Z][A-Z0-9_]*)\}`)
	for path, content := range map[string]string{"sip-inbound-trunk.json": trunk, "sip-dispatch-rule.json": dispatch} {
		for _, found := range token.FindAllStringSubmatch(content, -1) {
			if found[1] != "UNMUTE_SIP_TRUNK_ID" && found[1] != "SIP_FROM_NUMBER" {
				t.Errorf("%s carries an unexpected token %q", path, found[1])
			}
		}
	}
}

// The records are inbound, so a package that only places calls must not receive
// them. Nor must the connector route, which accepts inbound calls but has no SIP
// trunk of any kind to claim a number on.
func TestTelephonyRecordInputsOnlyForInboundSIPRoutes(t *testing.T) {
	outboundOnly := generateSIPFixture(t, "twilio", false, true, true)
	if artifactHasFile(outboundOnly, "sip-inbound-trunk.json") {
		t.Error("outbound-only package got an inbound trunk input")
	}
	if artifactHasFile(outboundOnly, "sip-dispatch-rule.json") {
		t.Error("outbound-only package got a dispatch rule input")
	}
	if readme := artifactFile(t, outboundOnly, "README.md"); strings.Contains(readme, "### At LiveKit") {
		t.Error("outbound-only README tells the operator to create inbound records")
	}

	agent, resolved := configuredLiveKitConnector(t)
	connector, err := GenerateLiveKit(agent, resolved, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"telephony-setup.sh", "sip-inbound-trunk.json", "sip-dispatch-rule.json"} {
		if artifactHasFile(connector, path) {
			t.Errorf("connector route got %s; it has no SIP trunk", path)
		}
	}
	readme := artifactFile(t, connector, "README.md")
	for _, forbidden := range []string{"## Telephony setup", "telephony-setup.sh", "UNMUTE_SIP_TRUNK_ID"} {
		if strings.Contains(readme, forbidden) {
			t.Errorf("connector README carries %q from the SIP runbook", forbidden)
		}
	}
}

// FR-002, FR-003, FR-003a, FR-007: the runbook's shape and the claims it makes.
func TestTelephonySetupRunbookHoldsItsContract(t *testing.T) {
	artifact := generateSIPFixture(t, "twilio", true, true, true)
	readme := artifactFile(t, artifact, "README.md")
	if strings.Contains(readme, "Configure self-hosted LiveKit SIP") {
		t.Error("README still heads the section with a name a LiveKit Cloud operator would skip")
	}
	runbook := section(t, readme, "## Telephony setup")
	for _, want := range []string{
		// The whole cost, stated up front (SC-003).
		"six actions in",
		"then two commands here",
		// Prerequisites, and nothing else.
		"`lk`, the LiveKit CLI",
		"`jq`",
		// Part one, Twilio, dictated.
		"### At your carrier (Twilio)",
		"Elastic SIP Trunking",
		"pstn.twilio.com",
		"Credential List",
		"`SIP_TRUNK_HOSTNAME`; there is no second address",
		";transport=tcp",
		"lk project list --json",
		// FR-003a: the one step that differs when LiveKit is self-hosted.
		"*Self-hosted LiveKit:*",
		"Call Transfer (SIP REFER)",
		"Enable PSTN Transfer",
		// Runnable half: how to get the origination URI, and how to set every
		// carrier-side value without opening the console. Verified against
		// twilio-cli 6.2.4 and lk 2.18.2 on 2026-08-12.
		"### Get your origination URI",
		"drop the `p_` prefix",
		"sip:\\(.ProjectId | sub(\"^p_\";\"\")).sip.livekit.cloud;transport=tcp",
		"cannot be guessed from `LIVEKIT_URL`",
		"twilio api:trunking:v1:trunks:create",
		"twilio api:trunking:v1:trunks:origination-urls:create",
		"twilio api:core:sip:credential-lists:credentials:create",
		"twilio api:trunking:v1:trunks:credential-lists:create",
		"twilio api:trunking:v1:trunks:phone-numbers:create",
		// The step an author gets stuck on: the number is attached from inside
		// the trunk, on a tab, not from the number's own page.
		"**Numbers** tab",
		"**Add a Number**",
		// from-transferor, not from-transferee. Measured on a live call
		// 2026-08-26: a UK trunk presenting the caller's Spanish number to a
		// Spanish carrier had every transfer refused as it was offered, seen as
		// `486 Busy Here` and a zero second, zero cost leg in the Twilio log.
		// The runbook has to name the value that connects, and say what the
		// other one costs.
		"--transfer-mode enable-all --transfer-caller-id from-transferor",
		"486 Busy",
		"#### Check the carrier side",
		// The password is typed at a prompt, never written into the block or a file.
		"read -rsp \"SIP password: \" SIP_PASSWORD",
		// Part two, LiveKit. Two explicit commands, each naming the project,
		// because `lk` reads its own default when no project is named and that
		// default is frequently not the one the agent deploys to.
		"### At LiveKit",
		`lk --project "$LK_PROJECT" sip inbound create -`,
		`lk --project "$LK_PROJECT" sip dispatch create -`,
		"lk cloud auth",
		// The two flag sets that look equivalent and are not: one makes a rule
		// with no agent, the other makes a trunk that rejects every carrier call.
		"--individual",
		"--auth-user/--auth-pass",
		// The symptom table, and how to undo the two records.
		"### If the call does not arrive",
		"### Taking it down",
		"sip dispatch delete",
		// There is no local phone half any more. What the runbook has to say
		// instead is where verification happens, and why here is not it.
		"### Verifying it",
		"no local phone step",
		"publicly routable SIP signalling and RTP ingress",
		"laptop behind normal NAT",
		"talk to it in the browser",
		// FR-007: what cold transfer needs, and what its failure line means.
		"Cold transfer needs nothing at LiveKit",
		"cannot be tested from the Agent Console",
		"cold transfer failed after <n>s",
		"PSTN transfer is off on the trunk",
	} {
		if !strings.Contains(runbook, want) {
			t.Errorf("the Telephony setup section is missing %q", want)
		}
	}
	if strings.Contains(runbook, "envsubst") {
		t.Error("the runbook still tells the operator to run envsubst")
	}
	for _, part := range []string{"### At your carrier (Twilio)", "### At LiveKit", "### What transfers need"} {
		for _, dash := range []string{"—", "–"} {
			if strings.Contains(section(t, readme, part), dash) {
				t.Errorf("%s contains %q; plain wording only", part, dash)
			}
		}
	}
}

// FR-006 and SC-002: the retired name survives in exactly one sentence, whose
// whole job is telling an operator of an earlier build to delete it.
func TestTelephonySetupRetiresTheInboundTrunkName(t *testing.T) {
	const retired = "LIVEKIT_SIP_INBOUND_TRUNK"
	artifact := generateSIPFixture(t, "twilio", true, true, true)
	for _, file := range artifact.Files {
		count := strings.Count(string(file.Content), retired)
		switch file.Path {
		case "README.md":
			if count != 1 {
				t.Errorf("README.md names %s %d times; the retirement sentence is the only permitted use", retired, count)
			}
			if !strings.Contains(string(file.Content), "That variable is retired") {
				t.Error("README.md names the retired variable without saying it is retired")
			}
		default:
			if count != 0 {
				t.Errorf("%s still carries %s", file.Path, retired)
			}
		}
	}
}

// User story 3: a second carrier changes words, not shapes. The carrier-specific
// half is the only half that moves.
func TestTelephonySetupCarrierSeamHoldsForASecondCarrier(t *testing.T) {
	twilio := artifactFile(t, generateSIPFixture(t, "twilio", true, true, true), "README.md")
	telnyx := generateSIPFixture(t, "telnyx", true, true, true)
	telnyxReadme := artifactFile(t, telnyx, "README.md")

	if strings.Contains(telnyxReadme, "### At your carrier (Twilio)") {
		t.Error("telnyx README got the Twilio console block")
	}
	generic := section(t, telnyxReadme, "### At your carrier")
	for _, want := range []string{"provider guide", "https://docs.livekit.io/telephony/start/providers/telnyx/", "`SIP_TRUNK_HOSTNAME`"} {
		if !strings.Contains(generic, want) {
			t.Errorf("the generic carrier block is missing %q", want)
		}
	}
	// The LiveKit half is carrier-neutral: same bytes, no carrier named, for
	// every carrier the capability table declares.
	if got, want := section(t, telnyxReadme, "### At LiveKit"), section(t, twilio, "### At LiveKit"); got != want {
		t.Errorf("the At LiveKit part differs by carrier:\n%s\nwant:\n%s", got, want)
	}
	for _, carrier := range []string{"twilio", "telnyx", "plivo", "exotel"} {
		if strings.Contains(strings.ToLower(section(t, telnyxReadme, "### At LiveKit")), carrier) {
			t.Errorf("the At LiveKit part names the carrier %q", carrier)
		}
	}
	// Same artifact set: adding a carrier adds instructions, not files.
	for _, path := range []string{"sip-inbound-trunk.json", "sip-dispatch-rule.json"} {
		if !artifactHasFile(telnyx, path) {
			t.Errorf("telnyx package is missing %s", path)
		}
	}
}

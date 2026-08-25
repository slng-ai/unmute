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

// Telephony setup is one runbook in the README and one emitted script. None of
// it can be proven by a live call in CI, so the operator-facing behaviour is
// pinned here.

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
		human := pkg.Agent.Controls["to_human"]
		human.Cold = nil
		human.Warm = &spec.WarmTransfer{Destination: "billing_line"}
		pkg.Agent.Controls["to_human"] = human
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

// FR-004, and the script contract's own test list.
func TestTelephonySetupScriptHoldsItsContract(t *testing.T) {
	artifact := generateSIPFixture(t, "twilio", true, true, true)
	script := artifactFile(t, artifact, "telephony-setup.sh")
	for _, want := range []string{
		"set -euo pipefail",
		"command -v lk",
		"command -v jq",
		"lk sip inbound list --json",
		"lk sip dispatch list --json",
		`number_env="SIP_FROM_NUMBER"`,
		// The guard: no dispatch rule is ever created without a resolved trunk.
		`[ -n "$trunk" ] ||`,
		"(created)",
		"(reused)",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("telephony-setup.sh is missing %q", want)
		}
	}
	// set -x would print the phone number and every command; sourcing the env
	// file would read every secret in it and die on one non-identifier line.
	for _, forbidden := range []string{"set -x", "source ", ". ./.env", "envsubst"} {
		if strings.Contains(script, forbidden) {
			t.Errorf("telephony-setup.sh contains %q", forbidden)
		}
	}
	// The only substitution token in the script is the dispatch rule's trunk ID.
	// The phone number reaches sed through a shell variable, never a token.
	tokens := regexp.MustCompile(`\$\{([A-Z][A-Z0-9_]*)\}`).FindAllStringSubmatch(script, -1)
	for _, token := range tokens {
		if token[1] != "UNMUTE_SIP_TRUNK_ID" {
			t.Errorf("telephony-setup.sh substitutes unexpected token %q", token[1])
		}
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
}

// The script provisions inbound records, so a package that only places calls
// must not receive it. Nor must the connector route, which accepts inbound calls
// but has no SIP trunk of any kind to claim a number on.
func TestTelephonySetupScriptOnlyForInboundSIPRoutes(t *testing.T) {
	outboundOnly := generateSIPFixture(t, "twilio", false, true, true)
	if artifactHasFile(outboundOnly, "telephony-setup.sh") {
		t.Error("outbound-only package got a provisioning script for records it never needs")
	}
	if artifactHasFile(outboundOnly, "sip-inbound-trunk.json") {
		t.Error("outbound-only package got an inbound trunk input")
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
		"then one command here",
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
		"--transfer-mode enable-all --transfer-caller-id from-transferee",
		"#### Check the carrier side",
		// The password is typed at a prompt, never written into the block or a file.
		"read -rsp \"SIP password: \" SIP_PASSWORD",
		// Part two, LiveKit.
		"### At LiveKit",
		"bash telephony-setup.sh",
		"lk cloud auth",
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
	script := artifactFile(t, telnyx, "telephony-setup.sh")
	for _, carrier := range []string{"twilio", "telnyx", "plivo", "exotel"} {
		if strings.Contains(strings.ToLower(section(t, telnyxReadme, "### At LiveKit")), carrier) {
			t.Errorf("the At LiveKit part names the carrier %q", carrier)
		}
		if strings.Contains(strings.ToLower(script), carrier) {
			t.Errorf("telephony-setup.sh names the carrier %q", carrier)
		}
	}
	// Same artifact set: adding a carrier adds instructions, not files.
	for _, path := range []string{"telephony-setup.sh", "sip-inbound-trunk.json", "sip-dispatch-rule.json"} {
		if !artifactHasFile(telnyx, path) {
			t.Errorf("telnyx package is missing %s", path)
		}
	}
}

package generate

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/spec"
	"github.com/slng/unmute/internal/target"
)

// A generated LiveKit project dials out with the carrier's own trunk settings,
// passed inline with every call, and uses no stored LiveKit outbound trunk
// (SCHEMA N33, 2026-08-12). These assertions are the contract in
// specs/002-inline-sip-trunk/contracts/, held here because the failure they
// prevent only shows up on a live call: on 2026-08-12 a deployed agent held a
// full conversation and then raised, from inside the prebuilt's constructor,
// "`LIVEKIT_SIP_OUTBOUND_TRUNK` environment variable, `sip_trunk_id`, or
// `sip_connection` must be set". There is no laptop path to a transfer, so
// everything a live call cannot reach has to be pinned here.

// configuredLiveKitSIPWarmOnly is the shape with no fixture before this
// feature: one warm transfer, no outbound channel, no cold transfer. It is the
// only shape where `from livekit import api` depends on the warm transfer
// alone, and getting that wrong is a NameError on the first transfer.
func configuredLiveKitSIPWarmOnly(t *testing.T) (*ir.Agent, ir.Target) {
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
	configured := pkg.Targets["livekit"]
	configured.Transport, configured.Carrier, configured.Connection = "sip", "twilio", "primary_phone"
	pkg.Targets = map[string]spec.Target{"livekit": configured}
	connection := pkg.Connections["primary_phone"]
	connection.Environment = map[string]string{
		"sip_address": "SIP_TRUNK_HOSTNAME", "sip_username": "SIP_AUTH_USERNAME",
		"sip_password": "SIP_AUTH_PASSWORD", "from_number": "SIP_FROM_NUMBER",
	}
	pkg.Connections["primary_phone"] = connection
	human := pkg.Agent.Controls["to_human"]
	human.Cold = nil
	human.Warm = &spec.WarmTransfer{Destination: "billing_line"}
	pkg.Agent.Controls["to_human"] = human

	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent, agent.Targets["livekit"]
}

// A warm transfer on its own must bring `api` into scope, because the inline
// trunk configuration is an api.SIPOutboundConfig. Before 2026-08-12 the import
// was emitted only for an outbound channel or a cold transfer, which was
// invisible in examples/human-transfer: it has a cold transfer as well as a
// warm one, so it never exercised this shape.
func TestInlineTrunkWarmOnlyPackageBringsAPIIntoScope(t *testing.T) {
	agent, resolved := configuredLiveKitSIPWarmOnly(t)
	artifact, err := GenerateLiveKit(agent, resolved, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	agentPy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		"from livekit import api",
		"def _sip_trunk() -> api.SIPOutboundConfig:",
		"def _sip_number() -> str:",
		"sip_connection=_sip_trunk(),",
		"sip_number=_sip_number(),",
		`os.environ["SIP_TRUNK_HOSTNAME"]`,
		`os.environ["SIP_AUTH_USERNAME"]`,
		`os.environ["SIP_AUTH_PASSWORD"]`,
		`os.environ["SIP_FROM_NUMBER"]`,
	} {
		if !strings.Contains(agentPy, want) {
			t.Errorf("warm-only agent.py missing %q", want)
		}
	}
	// The import must come from the warm transfer, not from a cold transfer or
	// an outbound channel, or this fixture proves nothing.
	if strings.Contains(agentPy, "_refer_uri") {
		t.Error("warm-only fixture has a cold transfer; it no longer isolates the import")
	}
	if strings.Contains(agentPy, "CreateSIPParticipantRequest") {
		t.Error("warm-only fixture has an outbound channel; it no longer isolates the import")
	}
}

// Cold acts on the caller's existing leg through SIP REFER and dials nobody, so
// a cold-only package needs no trunk of either kind and must get no dial-out
// helper (FR-007, SC-007).
func TestInlineTrunkColdOnlyPackageGetsNoDialOutHelper(t *testing.T) {
	agent := loadCompilerAgent(t)
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatal(err)
	}
	agentPy := artifactFile(t, artifact, "agent.py")
	if !strings.Contains(agentPy, "_refer_uri(") {
		t.Fatal("fixture is not a cold transfer package")
	}
	for _, forbidden := range []string{"_sip_trunk", "_sip_number", "SIPOutboundConfig", "LIVEKIT_SIP_OUTBOUND_TRUNK"} {
		if strings.Contains(agentPy, forbidden) {
			t.Errorf("cold-only agent.py contains %q; cold needs no trunk of any kind", forbidden)
		}
	}
	env := artifactFile(t, artifact, ".env.example")
	if strings.Contains(env, "LIVEKIT_SIP_OUTBOUND_TRUNK") {
		t.Error("cold-only .env.example asks for an outbound trunk id")
	}
}

// FR-008: the connector route carries plain audio over a carrier WebSocket,
// grants no transfer feature, and places outbound calls in the bridge. Nothing
// this feature adds may reach it.
func TestInlineTrunkLeavesTheConnectorRouteAlone(t *testing.T) {
	agent, resolved := configuredLiveKitConnector(t)
	artifact, err := GenerateLiveKit(agent, resolved, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range artifact.Files {
		for _, forbidden := range []string{"_sip_trunk", "_sip_number", "SIPOutboundConfig", "sip-outbound-trunk.json"} {
			if strings.Contains(string(file.Content), forbidden) {
				t.Errorf("connector %s contains %q", file.Path, forbidden)
			}
		}
	}
}

// FR-004 and User Story 3 scenario 2: a deployment that still sets the retired
// name behaves identically to one that does not. The prebuilt ignores it once
// sip_connection is passed, by its own documented precedence, verified in
// warm_transfer.py on 2026-08-12:
//
//	elif self._sip_connection is not None:
//	    # explicit sip_connection: don't override with the env var trunk
//	    self._sip_trunk_id = None
//
// So this needs no code. It needs a test, because upstream could change its
// mind and the failure would land on a live call.
func TestInlineTrunkRetiredNameIsInert(t *testing.T) {
	t.Setenv("LIVEKIT_SIP_OUTBOUND_TRUNK", "ST_this_could_not_possibly_work")
	agent, resolved := configuredLiveKitSIP(t)
	artifact, err := GenerateLiveKit(agent, resolved, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range artifact.Files {
		// The README names the retired variable on purpose, to tell an operator
		// who set up an earlier build that it is gone. Everything else must not
		// mention it at all.
		if file.Path != "README.md" && strings.Contains(string(file.Content), "LIVEKIT_SIP_OUTBOUND_TRUNK") {
			t.Errorf("%s reads the retired outbound trunk name", file.Path)
		}
		if strings.Contains(string(file.Content), "ST_this_could_not_possibly_work") {
			t.Errorf("%s baked an environment value into the artifact", file.Path)
		}
	}
	readme := artifactFile(t, artifact, "README.md")
	if !strings.Contains(readme, "no longer part of it") {
		t.Error("README.md names the retired variable without saying it is retired")
	}
	agentPy := artifactFile(t, artifact, "agent.py")
	if !strings.Contains(agentPy, "sip_connection=_sip_trunk(),") {
		t.Error("agent.py does not pass the inline trunk, so the retired name would win")
	}
}

// FR-002 and SC-003: one helper, every dial site. Three copies of the same
// three environment names would be three chances for a credential rotation to
// fix one path and break another.
func TestInlineTrunkIsReadInOnePlaceAndCalledEverywhere(t *testing.T) {
	agent, resolved := configuredLiveKitSIP(t)
	artifact, err := GenerateLiveKit(agent, resolved, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	agentPy := artifactFile(t, artifact, "agent.py")
	if got := strings.Count(agentPy, "def _sip_trunk("); got != 1 {
		t.Errorf("agent.py defines _sip_trunk %d times, want 1", got)
	}
	if got := strings.Count(agentPy, "def _sip_number("); got != 1 {
		t.Errorf("agent.py defines _sip_number %d times, want 1", got)
	}
	// One outbound dial and one warm dial render for this fixture, because the
	// two create_sip_participant sites in the template are the two arms of the
	// call-start-variables branch.
	if got := strings.Count(agentPy, "trunk=_sip_trunk(),"); got != 1 {
		t.Errorf("agent.py has %d outbound dial sites carrying the inline trunk, want 1", got)
	}
	if got := strings.Count(agentPy, "sip_connection=_sip_trunk(),"); got != 1 {
		t.Errorf("agent.py has %d warm dial sites carrying the inline trunk, want 1", got)
	}
	if got := strings.Count(agentPy, "sip_number=_sip_number(),"); got != 2 {
		t.Errorf("agent.py has %d dial sites carrying the from-number, want 2", got)
	}
	// Both arms of that branch must carry it, and only one of them renders per
	// package, so the template itself is where that is checkable. A change
	// applied to one arm and not the other is exactly the drift FR-002 exists
	// to prevent, and no single artifact would show it.
	tmpl, err := livekitV1Templates.ReadFile("templates/livekit_v1/agent.py.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(tmpl), "trunk=_sip_trunk(),"); got != 2 {
		t.Errorf("the template has %d outbound dial sites carrying the inline trunk, want 2", got)
	}
	if got := strings.Count(string(tmpl), "sip_number=_sip_number(),"); got != 3 {
		t.Errorf("the template has %d dial sites carrying the from-number, want 3", got)
	}
	if strings.Contains(string(tmpl), "sip_trunk_id") {
		t.Error("the template still passes a stored trunk id somewhere")
	}
	// The from-number is not optional with an inline trunk: the prebuilt's own
	// fallback chain ends at "", which the SIP service rejects.
	for _, name := range []string{"sip_trunk_id", "LIVEKIT_SIP_NUMBER"} {
		if strings.Contains(agentPy, name) {
			t.Errorf("agent.py still references %q", name)
		}
	}
	// FR-017: exactly what is needed to dial. Region and transport are optional
	// on the platform and no Connection declares either.
	for _, forbidden := range []string{"destination_country", "transport="} {
		if strings.Contains(agentPy, forbidden) {
			t.Errorf("agent.py emits %q, which no Connection declared", forbidden)
		}
	}
}

// FR-004, FR-018, SC-004 and SC-011: the retired name and its input file leave
// every emitted surface, including the compile report, and the inbound records
// stay exactly as they were.
func TestInlineTrunkLeavesNoTraceInAnyArtifact(t *testing.T) {
	agent, resolved := configuredLiveKitSIP(t)
	artifact, err := GenerateLiveKit(agent, resolved, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range artifact.Files {
		if file.Path == "sip-outbound-trunk.json" {
			t.Error("sip-outbound-trunk.json is still emitted")
		}
		// README.md names it once, to say it is retired (see
		// TestInlineTrunkRetiredNameIsInert).
		if file.Path != "README.md" && strings.Contains(string(file.Content), "LIVEKIT_SIP_OUTBOUND_TRUNK") {
			t.Errorf("%s still names the retired outbound trunk", file.Path)
		}
	}
	// Inbound is untouched: an unsolicited call arrives with no request of ours
	// for configuration to travel with, so the platform has to already hold it.
	for _, path := range []string{"sip-inbound-trunk.json", "sip-dispatch-rule.json"} {
		if content := artifactFile(t, artifact, path); !strings.Contains(content, "${LIVEKIT_SIP_INBOUND_TRUNK}") && path == "sip-dispatch-rule.json" {
			t.Errorf("%s no longer scopes itself to the inbound trunk", path)
		}
	}
}

// SC-010: the compiler knows none of the four names. It carries whatever a
// Connection declares, which is what makes the same emitted code dial through
// any SIP carrier, and what keeps a package written before the shipped example
// moved to the plain names working unchanged.
func TestInlineTrunkNamesAreNotCompilerKnowledge(t *testing.T) {
	names := []string{"SIP_TRUNK_HOSTNAME", "SIP_AUTH_USERNAME", "SIP_AUTH_PASSWORD", "SIP_FROM_NUMBER"}
	root := filepath.Join("..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, name := range names {
			if strings.Contains(string(content), name) {
				t.Errorf("%s names %s; the compiler must read whatever the Connection declares", path, name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// SC-010, the other half: a Connection still declaring carrier-prefixed names
// compiles and dials with those names. configuredLiveKitSIP keeps
// TWILIO_SIP_ADDRESS and friends deliberately for exactly this reason.
func TestInlineTrunkHonoursCarrierPrefixedNames(t *testing.T) {
	agent, resolved := configuredLiveKitSIP(t)
	artifact, err := GenerateLiveKit(agent, resolved, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	agentPy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		`hostname=os.environ["TWILIO_SIP_ADDRESS"]`,
		`auth_username=os.environ["TWILIO_SIP_USERNAME"]`,
		`auth_password=os.environ["TWILIO_SIP_PASSWORD"]`,
		`return os.environ["TWILIO_PHONE_NUMBER"]`,
	} {
		if !strings.Contains(agentPy, want) {
			t.Errorf("agent.py missing %q; the compiler did not carry the authored name through", want)
		}
	}
}

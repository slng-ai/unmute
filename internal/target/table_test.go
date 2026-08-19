package target

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestDefaultTableIsCompleteAndTyped(t *testing.T) {
	table := Default()
	for field, providers := range table.Fields {
		for _, provider := range Providers {
			if providers[provider].Tag == "" {
				t.Errorf("%s missing %s tag", field, provider)
			}
		}
	}
	for control, providers := range table.Controls {
		for _, provider := range Providers {
			if providers[provider].Tag == "" {
				t.Errorf("%s missing %s tag", control, provider)
			}
		}
	}
	if table.Role(Turn, Vapi) != Integrated {
		t.Fatal("Vapi turn role must be integrated")
	}
	if err := DefaultCatalog().CheckVendor(LiveKit, Speak, "elevenlabs", false); err != nil {
		t.Fatalf("livekit speak vendor elevenlabs must validate: %v", err)
	}
	if got := table.HistorySupport(HistorySummary, Vapi); got.Kind != HistoryFail || got.Note == "" {
		t.Fatalf("Vapi summary history = %#v", got)
	}
	for _, provider := range Providers {
		if table.Capability(FieldFutureProvisional, provider).Tag != Provisional {
			t.Errorf("provisional field passed on %s", provider)
		}
	}
	if table.Capability(FieldInactivity, LiveKit).Tag != Warn || table.Capability(FieldMaxDuration, Deepgram).Tag != Warn {
		t.Fatal("warn-tagged lifecycle fields must produce target warnings")
	}
	if table.Capability(FieldTracingLangfuse, LiveKit).Tag != Core || table.Capability(FieldTracingLangfuse, Pipecat).Tag != Core {
		t.Fatal("Langfuse tracing must pass on code drivers")
	}
	for _, provider := range []Provider{Vapi, Deepgram} {
		if table.Capability(FieldTracingLangfuse, provider).Tag != Gated {
			t.Errorf("Langfuse tracing passed on %s", provider)
		}
	}
}

func TestBuiltinToolsPassOnCodeDriversOnly(t *testing.T) {
	table := Default()
	if table.Capability(FieldToolBuiltin, LiveKit).Tag != Core || table.Capability(FieldToolBuiltin, Pipecat).Tag != Core {
		t.Fatal("builtin tools must pass on LiveKit and Pipecat")
	}
	for _, provider := range []Provider{Vapi, Deepgram} {
		if table.Capability(FieldToolBuiltin, provider).Tag != Gated {
			t.Errorf("builtin tools passed on %s", provider)
		}
	}
}

func TestAgentTransferAnnouncePassesOnGeneratedDriversOnly(t *testing.T) {
	table := Default()
	for _, provider := range []Provider{LiveKit, Pipecat} {
		if got := table.Capability(FieldTransferAnnounce, provider); got.Tag != Core {
			t.Errorf("agent transfer announce gated on %s: %#v", provider, got)
		}
	}
	for _, provider := range []Provider{Vapi, Deepgram} {
		if got := table.Capability(FieldTransferAnnounce, provider); got.Tag != Gated || got.Note == "" {
			t.Errorf("agent transfer announce passed on %s: %#v", provider, got)
		}
	}
}

func TestTelephonyControlsResolveCarrierAndTransport(t *testing.T) {
	table := Default()
	tests := []struct {
		name      string
		control   TelephonyControl
		provider  Provider
		transport string
		carrier   string
		want      Tag
	}{
		{"pipecat cold missing Daily", ColdTransfer, Pipecat, "twilio", "", Gated},
		{"pipecat cold without phone plan", ColdTransfer, Pipecat, "daily-sip", "", Gated},
		// Vapi and Deepgram have no route and no connection, so after the route
		// moved into the connection file no author can write a carrier these
		// rows would see. The four rows that conditioned on one lost the
		// condition rather than becoming impossible to satisfy; each keeps its
		// Twilio requirement as a comment in table.go for whoever builds the
		// driver (spec FR-001a, research R11).
		{"vapi warm with no carrier", WarmTransfer, Vapi, "", "", Core},
		{"deepgram warm with no carrier", WarmTransfer, Deepgram, "", "", Core},
		{"deepgram cold with no carrier", ColdTransfer, Deepgram, "", "", Core},
		{"deepgram voicemail with no carrier", VoicemailDetection, Deepgram, "", "", Core},
		// The routed controls still gate, and on both halves of the route. A
		// driverless target has no transport either, so removing carrier from
		// targets changed nothing here (task T015c).
		{"DTMF missing route", DTMFSend, LiveKit, "", "", Gated},
		{"DTMF carrier only", DTMFSend, LiveKit, "", "twilio", Gated},
		{"DTMF exact route", DTMFSend, LiveKit, "daily-sip", "twilio", Core},
		{"DTMF unknown carrier", DTMFSend, LiveKit, "", "made-up", Gated},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := table.Control(test.control, test.provider, test.transport, test.carrier); got.Tag != test.want {
				t.Fatalf("capability = %#v", got)
			}
		})
	}
}

func TestTelephonyControlRequiresExactCarrierAndTransport(t *testing.T) {
	table := Table{Controls: map[TelephonyControl]map[Provider]ControlCapability{
		ColdTransfer: {
			Pipecat: {
				Capability: Capability{Tag: Core}, Carrier: "twilio", Transport: "carrier-websocket",
				ConditionNote: "exact route required",
			},
		},
	}}
	for _, route := range []struct{ transport, carrier string }{
		{"carrier-websocket", "telnyx"},
		{"sip", "twilio"},
	} {
		if got := table.Control(ColdTransfer, Pipecat, route.transport, route.carrier); got.Tag != Gated {
			t.Fatalf("partial route match passed: %#v", got)
		}
	}
}

func TestTelephonyRouteEvidenceIsExactAndProvisionalWithoutSmoke(t *testing.T) {
	exact := TelephonyKey{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "twilio"}
	if got := ResolveTelephonyFeature(exact, TelephonyInbound); got.Tag != Provisional || got.Docs == "" || got.Verified == "" || got.Smoke {
		t.Fatalf("exact route evidence = %#v", got)
	}
	for _, key := range []TelephonyKey{
		{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "telnyx"},
		{Provider: Pipecat, Transport: "sip", Carrier: "twilio"},
	} {
		if got := ResolveTelephonyFeature(key, TelephonyFeature(WarmTransfer)); got.Tag != Gated {
			t.Fatalf("partial or unsupported route passed: %#v", got)
		}
	}
	// The LiveKit Twilio connector is a usable route (its own open-source
	// bridge): inbound, outbound, and hangup are provisional like the other
	// live routes; transfers and voicemail stay gated. A transfer needs a
	// platform primitive and this route has none (SPEC C1, V1).
	connector := TelephonyKey{Provider: LiveKit, Transport: "connector", Carrier: "twilio"}
	for _, feature := range []TelephonyFeature{TelephonyRouteSelected, TelephonyInbound, TelephonyOutbound, TelephonyFeature(Hangup)} {
		if got := ResolveTelephonyFeature(connector, feature); got.Tag != Provisional {
			t.Fatalf("connector feature %s = %#v, want provisional", feature, got)
		}
	}
	for _, feature := range []TelephonyFeature{TelephonyFeature(WarmTransfer), TelephonyFeature(ColdTransfer), TelephonyFeature(VoicemailDetection)} {
		if got := ResolveTelephonyFeature(connector, feature); got.Tag != Gated {
			t.Fatalf("connector unsupported feature %s = %#v, want gated", feature, got)
		}
	}
	required, optional, ok := TelephonyEnvironment(connector)
	if !ok || len(optional) != 0 || strings.Join(required, ",") != "account_sid,auth_token,from_number" {
		t.Fatalf("connector environment vocabulary = required %v optional %v ok %v", required, optional, ok)
	}
	if route := TelephonyRoutes()[connector]; len(route.Processes) != 1 || len(route.PublicEndpoints) != 4 || route.AutoWebhookEndpoint != "inbound" {
		t.Fatalf("connector must advertise runtime facts: %#v", route)
	}
	required, optional, ok = TelephonyEnvironment(exact)
	if !ok || len(optional) != 0 || strings.Join(required, ",") != "account_sid,auth_token,from_number" {
		t.Fatalf("exact environment vocabulary = required %v optional %v ok %v", required, optional, ok)
	}
	runtime := TelephonyRoutes()[exact]
	if len(runtime.Processes) != 1 || len(runtime.PublicEndpoints) != 4 || len(runtime.ManualSteps) == 0 {
		t.Fatalf("Pipecat route runtime facts = %#v", runtime)
	}
	// Every runtime name on this route is supplied rather than authored: the
	// Compose graph starts the Redis, and `unmute dev` mints the public URL and
	// the outbound token. The last two were missing from this list while the dev
	// command already minted them, so .env.example asked the author to fill in
	// blanks nobody fills in (FR-018c). ir.Build scopes the list to what a given
	// package's route actually requires, so an inbound-only package drops the
	// outbound token.
	if strings.Join(runtime.LocallySuppliedEnvironment, ",") != "REDIS_URL,UNMUTE_OUTBOUND_TOKEN,UNMUTE_PUBLIC_URL" {
		t.Fatalf("Pipecat locally supplied environment = %v", runtime.LocallySuppliedEnvironment)
	}
	// The Daily carrier leg (SCHEMA N37): five provisional features, no call
	// sources, and every granted feature carries its docs and its date.
	dailyCarrier := TelephonyKey{Provider: Pipecat, Transport: "daily-sip", Carrier: "twilio"}
	for _, feature := range []TelephonyFeature{
		TelephonyRouteSelected, TelephonyInbound, TelephonyOutbound,
		TelephonyFeature(ColdTransfer), TelephonyFeature(Hangup),
	} {
		got := ResolveTelephonyFeature(dailyCarrier, feature)
		if got.Tag != Provisional || got.Docs == "" || got.Verified == "" || got.Smoke {
			t.Fatalf("daily carrier feature %s = %#v, want provisional with docs and a date", feature, got)
		}
	}
	for _, feature := range []TelephonyFeature{
		TelephonyFeature(WarmTransfer), TelephonyFeature(VoicemailDetection),
		"source.from_number", "source.to_number", "source.call_id", "source.direction",
	} {
		if got := ResolveTelephonyFeature(dailyCarrier, feature); got.Tag != Gated {
			t.Fatalf("daily carrier feature %s = %#v, want gated", feature, got)
		}
	}
	telnyx := TelephonyKey{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "telnyx"}
	required, optional, ok = TelephonyEnvironment(telnyx)
	if !ok || len(optional) != 0 || strings.Join(required, ",") != "api_key,public_key,connection_id,from_number" {
		t.Fatalf("Telnyx environment vocabulary = required %v optional %v ok %v", required, optional, ok)
	}
	plivo := TelephonyKey{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "plivo"}
	required, optional, ok = TelephonyEnvironment(plivo)
	if !ok || len(optional) != 0 || strings.Join(required, ",") != "auth_id,auth_token,from_number" {
		t.Fatalf("Plivo environment vocabulary = required %v optional %v ok %v", required, optional, ok)
	}
	for _, carrier := range []string{"twilio", "telnyx", "plivo"} {
		livekitSIP := TelephonyKey{Provider: LiveKit, Transport: "sip", Carrier: carrier}
		required, optional, ok = TelephonyEnvironment(livekitSIP)
		if !ok || len(optional) != 0 || strings.Join(required, ",") != "sip_address,sip_username,sip_password,from_number" {
			t.Fatalf("LiveKit SIP %s environment vocabulary = required %v optional %v ok %v", carrier, required, optional, ok)
		}
		if got := ResolveTelephonyFeature(livekitSIP, TelephonyFeature(WarmTransfer)); got.Tag != Provisional || got.Smoke || !strings.Contains(got.Docs, carrier) {
			t.Fatalf("LiveKit SIP %s warm transfer evidence = %#v", carrier, got)
		}
	}
	exotel := TelephonyKey{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "exotel"}
	if got := ResolveTelephonyFeature(exotel, TelephonyRouteSelected); got.Tag != Gated || !strings.Contains(got.Note, "does not support route") {
		t.Fatalf("Exotel unauthenticated WebSocket route = %#v", got)
	}
	if got := ResolveTelephonyFeature(TelephonyKey{Provider: LiveKit, Transport: "sip", Carrier: "exotel"}, TelephonyRouteSelected); got.Tag != Gated {
		t.Fatalf("unproven Exotel SIP route = %#v", got)
	}
	// No transfers on the carrier-websocket routes, either shape: a transfer
	// needs a platform primitive and Pipecat's websocket transports have none
	// (SPEC C1, V1). Every carrier refuses both by name.
	for _, carrier := range []string{"twilio", "telnyx", "plivo"} {
		key := TelephonyKey{Provider: Pipecat, Transport: "carrier-websocket", Carrier: carrier}
		for _, feature := range []TelephonyFeature{TelephonyFeature(ColdTransfer), TelephonyFeature(WarmTransfer)} {
			if got := ResolveTelephonyFeature(key, feature); got.Tag != Gated {
				t.Fatalf("Pipecat %s %s = %#v, want gated", carrier, feature, got)
			}
		}
	}
}

func TestMCPResolvesSDKLanguageFromTable(t *testing.T) {
	table := Default()
	if got := table.CapabilityForValue(FieldToolMCP, LiveKit, "go"); got.Tag != Gated {
		t.Fatalf("Go MCP capability = %#v", got)
	}
	if got := table.CapabilityForValue(FieldToolMCP, LiveKit, "python"); got.Tag != Core {
		t.Fatalf("Python MCP capability = %#v", got)
	}
}

// TestV1_TransfersCompileOnlyOnNativeRoutes is SPEC V1: a transfer control
// compiles only where the platform documents the primitive. LiveKit SIP
// trunks grant both shapes; every other telephony route refuses both, and the
// refusal names the routes that work (SPEC C1). On the non-telephony side,
// the Pipecat control rows allow cold on the Daily transport alone and deny
// warm everywhere.
func TestV1_TransfersCompileOnlyOnNativeRoutes(t *testing.T) {
	cold, warm := TelephonyFeature(ColdTransfer), TelephonyFeature(WarmTransfer)
	for _, carrier := range []string{"twilio", "telnyx", "plivo"} {
		sip := TelephonyKey{Provider: LiveKit, Transport: "sip", Carrier: carrier}
		for _, feature := range []TelephonyFeature{cold, warm} {
			if got := ResolveTelephonyFeature(sip, feature); got.Tag != Provisional {
				t.Errorf("livekit sip %s %s = %#v, want provisional", carrier, feature, got)
			}
		}
	}
	noPrimitive := []TelephonyKey{
		{Provider: LiveKit, Transport: "connector", Carrier: "twilio"},
		{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "twilio"},
		{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "telnyx"},
		{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "plivo"},
	}
	for _, key := range noPrimitive {
		coldGot := ResolveTelephonyFeature(key, cold)
		if coldGot.Tag != Gated || !strings.Contains(coldGot.Note, "(livekit, sip)") || !strings.Contains(coldGot.Note, "daily-sip") {
			t.Errorf("%v cold = %#v, want gated with the supported routes named", key, coldGot)
		}
		warmGot := ResolveTelephonyFeature(key, warm)
		if warmGot.Tag != Gated || !strings.Contains(warmGot.Note, "(livekit, sip) trunks") {
			t.Errorf("%v warm = %#v, want gated with the supported routes named", key, warmGot)
		}
	}
	table := Default()
	if got := table.Control(ColdTransfer, Pipecat, "daily-sip", ""); got.Tag != Gated || !strings.Contains(got.Note, "active channels.phone Connection") {
		t.Errorf("pipecat cold without a phone plan = %#v, want gated with the required phone leg named", got)
	}
	if got := table.Control(ColdTransfer, Pipecat, "carrier-websocket", "twilio"); got.Tag != Gated {
		t.Errorf("pipecat cold off daily-sip = %#v, want gated", got)
	}
	for _, transport := range []string{"daily-sip", "carrier-websocket", ""} {
		if got := table.Control(WarmTransfer, Pipecat, transport, ""); got.Tag != Gated || !strings.Contains(got.Note, "(livekit, sip)") {
			t.Errorf("pipecat warm on %q = %#v, want gated naming (livekit, sip)", transport, got)
		}
	}
	if got := table.Capability(FieldTransferBriefing, Pipecat); got.Tag != Gated {
		t.Errorf("pipecat briefing field = %#v, want gated (briefing rides the warm row)", got)
	}
}

// Prerequisites are keyed by the exact route triple. The removed carrierless
// shape has none; the supported Twilio route has the Daily account requirement.
func TestRouteAccountPrerequisitesAreRouteScoped(t *testing.T) {
	got := RouteAccountPrerequisites(Pipecat, "daily-sip", "")
	if len(got) != 0 {
		t.Fatalf("carrierless daily-sip prerequisites = %+v, want none for the rejected route", got)
	}
	got = RouteAccountPrerequisites(Pipecat, "daily-sip", "twilio")
	if len(got) != 1 || got[0].Name != "daily_dialout" {
		t.Fatalf("pipecat daily-sip twilio prerequisites = %+v, want the one daily_dialout row", got)
	}
	if !got[0].Needs([]TelephonyFeature{TelephonyFeature(ColdTransfer)}) || !got[0].Needs([]TelephonyFeature{TelephonyOutbound}) {
		t.Error("daily_dialout must cover carrier-backed cold transfer and outbound calling")
	}
	if got[0].Needs([]TelephonyFeature{TelephonyInbound}) {
		t.Error("daily_dialout must not apply to an inbound-only package")
	}
	for _, carrier := range []string{"twilio", "telnyx", "plivo"} {
		if got := RouteAccountPrerequisites(Pipecat, "carrier-websocket", carrier); len(got) != 0 {
			t.Errorf("pipecat carrier-websocket %s prerequisites = %+v, want none", carrier, got)
		}
	}
	if got := RouteAccountPrerequisites(LiveKit, "sip", "twilio"); len(got) != 0 {
		t.Errorf("livekit sip prerequisites = %+v, want none", got)
	}
}

// Every prerequisite is recorded the way every other provider claim in this
// rulebook is: with the page it came from and the date it was checked. An empty
// NeededBy would be a prerequisite nothing needs, which must not exist.
func TestRouteAccountPrerequisitesAreEvidenced(t *testing.T) {
	table := Default()
	for _, rule := range routePrerequisites {
		p := rule.prereq
		if p.Name == "" || p.Summary == "" {
			t.Errorf("prerequisite %+v needs a name and an actionable summary", p)
		}
		if len(p.NeededBy) == 0 {
			t.Errorf("prerequisite %q has an empty NeededBy: a prerequisite nothing needs must not exist", p.Name)
		}
		if p.Docs == "" || p.Verified == "" {
			t.Errorf("prerequisite %q = docs %q verified %q, want both set", p.Name, p.Docs, p.Verified)
		}
		// A prerequisite for a capability the route refuses is a prerequisite for
		// something no package can reach, so it could never be reported. Checked
		// against the control rows rather than restated, so the rulebook stays the
		// one place the route's support is described.
		for _, feature := range p.NeededBy {
			control := TelephonyControl(feature)
			if _, isControl := table.Controls[control]; !isControl {
				continue
			}
			tag := table.Control(control, rule.provider, rule.transport, rule.carrier).Tag
			key := TelephonyKey{Provider: rule.provider, Transport: rule.transport, Carrier: rule.carrier}
			if rule.carrier != "" {
				if _, ok := TelephonyRoutes()[key]; ok {
					tag = ResolveTelephonyFeature(key, feature).Tag
				}
			}
			if tag == Gated {
				t.Errorf("prerequisite %q is needed by %q, which (%s, %s) refuses",
					p.Name, feature, rule.provider, rule.transport)
			}
		}
	}
}

// TestNoVendorVariableWearsTheUnmutePrefix is the naming rule as a grep. A
// variable that configures a vendor's service is named after that vendor, and a
// name carrying both the Unmute prefix and a vendor token claims two owners in
// one string. (No such name is spelled out here: this test greps itself too.)
//
// The argument is Principle I, not tidiness: a generated project must run with
// Unmute absent, so an UNMUTE_ prefix on a knob that configures Daily or LiveKit
// is the dependency-shaped thing that principle forbids, whether or not any
// reader mistakes it for a credential (research D11).
//
// Variables the generated agent itself owns, belonging to no vendor, keep
// UNMUTE_. Those names are hidden from every author-facing file by FR-018, so
// the prefix stops being something a reader meets.
func TestNoVendorVariableWearsTheUnmutePrefix(t *testing.T) {
	vendors := []string{"DAILY", "LIVEKIT", "TWILIO", "TELNYX", "PLIVO", "OPENAI", "SLNG"}
	unmuteEnv := regexp.MustCompile(`\bUNMUTE_[A-Z0-9_]+\b`)

	root := filepath.Join("..", "..")
	// `.claude` holds git worktrees, which are whole other checkouts of this
	// same repository. Walking them scans a stale copy of every file this walk
	// already covers, so a worktree on disk fails the run for reasons that have
	// nothing to do with the tree under test.
	skip := map[string]bool{".git": true, ".claude": true, "specs": true, "bin": true, "build": true, "node_modules": true}
	seen := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skip[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Ext(path) {
		case ".go", ".md", ".mdx", ".yaml", ".yml", ".tmpl", ".py", ".txt", ".json":
		default:
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, name := range unmuteEnv.FindAllString(string(content), -1) {
			seen++
			for _, vendor := range vendors {
				if strings.Contains(strings.TrimPrefix(name, "UNMUTE_"), vendor) {
					t.Errorf("%s: %s configures %s, so %s owns the name", filepath.ToSlash(path), name, vendor, vendor)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen == 0 {
		t.Fatal("found no UNMUTE_ names anywhere, so this test would pass for the wrong reason")
	}
}

// TestToolAnnounceCapabilityRows: only the two code drivers emit a tool
// announcement, and each provider that cannot gets a note saying why, because a
// gated row with no note tells the author nothing. The task scope is its own row:
// LiveKit reaches agent tools and task tools through one lowering, Pipecat does
// not (FR-007, FR-014).
func TestToolAnnounceCapabilityRows(t *testing.T) {
	table := Default()
	for provider, want := range map[Provider]Tag{
		LiveKit: Core, Pipecat: Core, Vapi: Gated, Deepgram: Gated,
	} {
		got := table.Capability(FieldToolAnnounce, provider)
		if got.Tag != want {
			t.Errorf("%s announce tag = %q, want %q", provider, got.Tag, want)
		}
		if want == Gated && strings.TrimSpace(got.Note) == "" {
			t.Errorf("%s announce is gated with no note", provider)
		}
	}
	// Pipecat only: the task row exists for a driver that emits the field but
	// cannot emit it in that scope. Vapi and Deepgram stay core here because the
	// field row above already stops them, the same shape as FieldToolMCPTask.
	for provider, want := range map[Provider]Tag{
		LiveKit: Core, Pipecat: Gated, Vapi: Core, Deepgram: Core,
	} {
		got := table.Capability(FieldToolAnnounceTask, provider)
		if got.Tag != want {
			t.Errorf("%s task-scoped announce tag = %q, want %q", provider, got.Tag, want)
		}
	}
	// The Pipecat note has to say where to put the tool instead, or the author
	// only learns that something is wrong.
	if note := table.Capability(FieldToolAnnounceTask, Pipecat).Note; !strings.Contains(note, "list it on the agent instead") {
		t.Errorf("Pipecat task-scope note must name the fix: %q", note)
	}
}

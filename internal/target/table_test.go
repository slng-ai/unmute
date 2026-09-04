package target

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// fieldConstant matches one Field constant declaration. gofmt fixes the shape,
// so a regexp reads it as reliably as a parser would and costs three lines
// instead of thirty. It takes the block form and the standalone one, because a
// constant declared outside a const block is still a constant: matching only the
// block would fail this test on a plain refactor and blame a missing row for it.
var fieldConstant = regexp.MustCompile(`(?m)^(?:\t|const )?(Field\w+)\s+Field = "(.+)"$`)

// TestEveryFieldConstantHasARow is the complement of
// TestDefaultTableIsCompleteAndTyped, and neither can stand in for the other:
// that test ranges table.Fields, so it visits keys, and a Field constant that is
// never a key is never visited.
//
// FieldReasonLocal spent its whole life in that blind spot. It was declared,
// read by ir.Validate on every locally placed reason model, and claimed as
// emitted by both drivers, while never being a key. Table.Capability handed back
// a zero Capability, whose empty tag matches no case in applyCapabilityValue, so
// an author who wrote `placement: local` on a think model got
// `capability "models.placement.local" has no livekit tag` — an internal message
// where a capability answer belonged — on both shipped targets.
//
// So this reads the constants out of the source rather than out of the map. It
// reads the whole package, not just table.go: a scan that only knows one file
// fails whenever a constant is declared next door, and reports it as a missing
// row, which sends the reader to the wrong place. The count check closes the
// other half: a row keyed by a bare string, with no constant behind it, is
// nothing ir.Validate can ever ask for.
func TestEveryFieldConstantHasARow(t *testing.T) {
	fields := Default().Fields
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var declared [][]string
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		content, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		declared = append(declared, fieldConstant.FindAllStringSubmatch(string(content), -1)...)
	}
	// Also fails at zero, which is what a stale regexp looks like.
	if len(declared) != len(fields) {
		t.Errorf("this package declares %d Field constants but Default().Fields holds %d rows", len(declared), len(fields))
	}
	for _, match := range declared {
		if _, ok := fields[Field(match[2])]; !ok {
			t.Errorf("%s (%q) is declared but has no row in Default().Fields, so it resolves to an untagged capability", match[1], match[2])
		}
	}
}

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
	if err := DefaultCatalog().CheckVendor(LiveKit, Speak, "elevenlabs", false); err != nil {
		t.Fatalf("livekit speak vendor elevenlabs must validate: %v", err)
	}
	// The Provisional tag is produced by telephony routes that have a real
	// adapter but no credentialed smoke yet. It used to be exercised through a
	// synthetic capability field invented for the purpose; that field is gone,
	// so the tag is checked where it actually comes from.
	provisionalSeen := false
	for _, route := range TelephonyRoutes() {
		for _, evidence := range route.Features {
			if evidence.Tag == Provisional {
				provisionalSeen = true
				if evidence.Note == "" || evidence.Docs == "" || evidence.Verified == "" {
					t.Errorf("provisional route %v feature %s lacks evidence: %#v", route.Key, evidence.Feature, evidence)
				}
			}
		}
	}
	if !provisionalSeen {
		t.Error("no route carries the Provisional tag; the tag has lost its only producer")
	}
	// Warn means the target takes the field and then does something other than
	// what the author wrote. It used to be spent on notes about how a framework
	// works and on driver TODOs addressed to us, which printed on every run of a
	// correct package and taught readers to skip the whole block; those rows are
	// gone. What is left has to keep earning the tag, so the gate names the row
	// rather than the count: a per-tool interruption preference is authored,
	// accepted, and then not honoured, which is exactly the silent difference a
	// warning exists to say out loud.
	if table.Capability(FieldToolInterruption, LiveKit).Tag != Warn {
		t.Fatal("FieldToolInterruption on LiveKit must warn: the preference is accepted and not enforced")
	}
	warnRows := 0
	for _, byProvider := range table.Fields {
		for _, capability := range byProvider {
			if capability.Tag == Warn {
				warnRows++
			}
		}
	}
	// A ratchet on noise. Warn is the one tag that fires on a package with
	// nothing wrong with it, so growing this number is a decision somebody makes
	// on purpose, with a reader in mind, and not a place to park a note to self.
	if warnRows > 1 {
		t.Errorf("the table carries %d warn rows, want 1: a new one prints on every run of a correct package, so it needs a fix an author can act on", warnRows)
	}
	// Tracing needs a process to instrument, which every remaining target has.
	// The other half of this check used to assert both fields were Gated on the
	// two managed targets; those are retired, so only the passing half is left.
	for _, field := range []Field{FieldTracingLangfuse, FieldTracingCoval} {
		if table.Capability(field, LiveKit).Tag != Core || table.Capability(field, Pipecat).Tag != Core {
			t.Fatalf("%s must pass on code drivers", field)
		}
	}

	// A gated row with no words leaves the author guessing, which is the thing
	// "fail loud" exists to prevent. This used to be checked only on the two
	// managed targets; it is a property of every gated row, so it is checked on
	// every gated row.
	for field, byProvider := range table.Fields {
		for provider, capability := range byProvider {
			if capability.Tag == Gated && capability.Note == "" {
				t.Errorf("%s denies %s without saying why", field, provider)
			}
		}
	}
}

// TestSlngEmitsNoProject pins the two questions the rest of the tree asks about
// a target instead of asking "which provider is it". Both functions are explicit
// two-value switches, so neither needed an edit for slng — which is exactly why
// this test exists: nothing would have failed if one had silently started
// returning true, and a target that claims to emit a project is asked for a
// framework version, dependency pins and an SDK language it has none of.
func TestSlngEmitsNoProject(t *testing.T) {
	if IsCode(Slng) {
		t.Error("IsCode(Slng) is true; the slng driver emits JSON and Markdown, not Python")
	}
	if EmitsProject(Slng) {
		t.Error("EmitsProject(Slng) is true; the slng driver emits a deployment body, not a runnable project")
	}
	if _, ok := Window(Slng); ok {
		t.Error("slng has a framework support window; SLNG owns its own runtime version")
	}
}

// TestEveryProviderHasAFallbackSlot exists because FallbackSlots is the one
// provider-keyed structure with no constructor to widen. ir.validateFallbacks
// reads it unconditionally at the top of every target validation and errors with
// "target has no fallback slot kind" when the key is missing, so a forgotten
// entry breaks every package on that target with a message about the table
// rather than about the package. Nothing else forces the key to exist.
func TestEveryProviderHasAFallbackSlot(t *testing.T) {
	slots := Default().FallbackSlots
	for _, provider := range Providers {
		if slots[provider] == "" {
			t.Errorf("%s has no fallback slot kind, so every package fails on it", provider)
		}
	}
}

// TestSlngRowsAreDeliberate is the completeness check for the provider that
// field() deliberately does not seed, and it holds three separate properties.
//
// The first is that field() really does leave slng out. That is the mechanism
// every forced decision rests on: restore the seed and 35 gated rows silently
// become Core, while TestDefaultTableIsCompleteAndTyped stays green because the
// tag is no longer empty. So the mechanism is asserted directly, not inferred
// from its effect.
//
// The second is that the structures with no constructor and no field-style
// completeness loop — Roles and History — carry a value for every provider.
//
// The third is spec FR-007: a gated row says what SLNG cannot do *and* what to
// do instead. table_test already checks a gated note is non-empty, which catches
// silence and not uselessness. The shape checked here is the one every slng note
// is written to: "slng target <what it cannot do>: <what to do instead>".
func TestSlngRowsAreDeliberate(t *testing.T) {
	if got := field()[Slng]; got.Tag != "" {
		t.Fatalf("field() seeds slng with %q; every undecided row is now silently supported", got.Tag)
	}
	table := Default()
	for role, byProvider := range table.Roles {
		for _, provider := range Providers {
			if byProvider[provider] == "" {
				t.Errorf("role %s has no %s kind; the zero value reads as integrated", role, provider)
			}
		}
	}
	for history, byProvider := range table.History {
		for _, provider := range Providers {
			if byProvider[provider].Kind == "" {
				t.Errorf("history %s has no %s kind; ir.validateContext reports it as an unknown value", history, provider)
			}
		}
	}
	// Every note slng speaks, from all three structures that carry one. A Warn is
	// included because it reaches the author too, on stderr.
	notes := map[string]string{}
	for field, byProvider := range table.Fields {
		if tag := byProvider[Slng].Tag; tag == Gated || tag == Warn {
			notes[string(field)] = byProvider[Slng].Note
		}
	}
	for control, byProvider := range table.Controls {
		if tag := byProvider[Slng].Tag; tag == Gated || tag == Warn {
			notes["control "+string(control)] = byProvider[Slng].Note
		}
	}
	for history, byProvider := range table.History {
		if byProvider[Slng].Kind == HistoryFail {
			notes["history "+string(history)] = byProvider[Slng].Note
		}
	}
	if len(notes) == 0 {
		t.Fatal("no slng row says anything, which is what a broken collection loop looks like")
	}
	for name, note := range notes {
		if !strings.HasPrefix(note, "slng target") {
			t.Errorf("%s: slng note does not start with %q, so a reader cannot tell it from a message about the slng model vendor: %q", name, "slng target", note)
		}
		split := strings.LastIndex(note, ": ")
		if split < 0 || strings.TrimSpace(note[split+2:]) == "" {
			t.Errorf("%s: slng note names no alternative, so it says what cannot be done and not what to do: %q", name, note)
		}
	}
}

func TestBuiltinToolsPassOnCodeDriversOnly(t *testing.T) {
	table := Default()
	if table.Capability(FieldToolBuiltin, LiveKit).Tag != Core || table.Capability(FieldToolBuiltin, Pipecat).Tag != Core {
		t.Fatal("builtin tools must pass on LiveKit and Pipecat")
	}
}

func TestAgentTransferAnnouncePassesOnGeneratedDriversOnly(t *testing.T) {
	table := Default()
	for _, provider := range []Provider{LiveKit, Pipecat} {
		if got := table.Capability(FieldTransferAnnounce, provider); got.Tag != Core {
			t.Errorf("agent transfer announce gated on %s: %#v", provider, got)
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
		{"cloud-websocket", "telnyx"},
		{"sip", "twilio"},
	} {
		if got := table.Control(ColdTransfer, Pipecat, route.transport, route.carrier); got.Tag != Gated {
			t.Fatalf("partial route match passed: %#v", got)
		}
	}
}

func TestTelephonyRouteEvidenceIsExactAndProvisionalWithoutSmoke(t *testing.T) {
	exact := TelephonyKey{Provider: Pipecat, Transport: "cloud-websocket", Carrier: "twilio"}
	if got := ResolveTelephonyFeature(exact, TelephonyInbound); got.Tag != Provisional || got.Docs == "" || got.Verified == "" || got.Smoke {
		t.Fatalf("exact route evidence = %#v", got)
	}
	for _, key := range []TelephonyKey{
		// A carrier the transport does not have, and a transport the provider does
		// not have. Both must resolve gated rather than partially matching.
		{Provider: Pipecat, Transport: "cloud-websocket", Carrier: "telnyx"},
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
	// The opposite of the connector's runtime facts, and the defining property of
	// this route: the platform terminates the carrier's stream, so the package
	// hosts no process, no endpoint and no supplied environment name of its own.
	// The block that used to sit here asserted a process, four endpoints and three
	// supplied names, all facts of the carrier-websocket route that is now gone.
	runtime := TelephonyRoutes()[exact]
	if len(runtime.Processes) != 0 || len(runtime.PublicEndpoints) != 0 {
		t.Fatalf("the platform-hosted route must host nothing: %#v", runtime)
	}
	if len(runtime.ManualSteps) == 0 {
		t.Fatal("the carrier steps on this route are dictated, so the row must summarise them")
	}
	if len(runtime.LocallySuppliedEnvironment) != 0 {
		t.Fatalf("nothing is supplied locally on this route: %v", runtime.LocallySuppliedEnvironment)
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
	// Telnyx and Plivo had their own REST vocabularies on carrier-websocket. That
	// transport is gone and Pipecat's surviving phone routes are Twilio only, so
	// the per-carrier vocabulary that remains is LiveKit's, asserted below.
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
	exotel := TelephonyKey{Provider: LiveKit, Transport: "sip", Carrier: "exotel"}
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
		LiveKit: Core, Pipecat: Core,
	} {
		got := table.Capability(FieldToolAnnounce, provider)
		if got.Tag != want {
			t.Errorf("%s announce tag = %q, want %q", provider, got.Tag, want)
		}
		if want == Gated && strings.TrimSpace(got.Note) == "" {
			t.Errorf("%s announce is gated with no note", provider)
		}
	}
	// The task row carries no override: a driver that can announce at all can
	// announce in either scope. Pipecat used to be gated here with "list it on
	// the agent instead", which was a gap in this compiler written as a limit of
	// the provider; a task tool is a flows handler and FlowManager.worker is the
	// documented seam for queueing a frame from inside one. Vapi and Deepgram
	// stay core because the field row above already stops them, the same shape as
	// FieldToolMCPTask.
	for _, provider := range Providers {
		if got := table.Capability(FieldToolAnnounceTask, provider); got.Tag != Core {
			t.Errorf("%s task-scoped announce tag = %q, want %q", provider, got.Tag, Core)
		}
	}
}

// TestKnowledgeCapabilityRows: the agent-scoped kind is Core on both targets,
// because both drivers emit a lowering. The task-scoped kind stays gated on
// Pipecat, and for a different reason: a Pipecat task tool is a flows handler
// holding a FlowManager rather than a decorated function holding
// FunctionCallParams, which is the same seam FieldToolMCPTask records.
//
// The agent row started denied on Pipecat and was flipped on 2026-08-25 when the
// lowering landed. It started that way because constitution 5.0.0 retired the
// vapi and deepgram targets for the inverse condition: they validated, then
// failed at compile with "driver is not implemented".
//
// internal/generate/knowledge_agreement_test.go asserts the same thing from the
// emitter's side, so this row and the two lowerings cannot drift apart.
func TestKnowledgeCapabilityRows(t *testing.T) {
	table := Default()
	// The two drivers that emit a runtime, named rather than ranged over: a third
	// target that emits none must not quietly satisfy this by being added to
	// Providers. slng is checked separately below, and refused.
	for _, provider := range []Provider{LiveKit, Pipecat} {
		if got := table.Capability(FieldToolKnowledge, provider); got.Tag != Core {
			t.Errorf("%s on %s = %q, want %q", FieldToolKnowledge, provider, got.Tag, Core)
		}
	}
	// slng emits a README and pushes a spec, so there is no image to carry the
	// documents and no process to index them. Refused, with a reason, both scopes.
	for _, f := range []Field{FieldToolKnowledge, FieldToolKnowledgeTask} {
		got := table.Capability(f, Slng)
		if got.Tag != Gated {
			t.Errorf("%s on slng = %q, want %q: the target emits no runtime to search in", f, got.Tag, Gated)
		}
		if strings.TrimSpace(got.Note) == "" {
			t.Errorf("%s is refused on slng with no note, so the author is told nothing", f)
		}
	}
	if got := table.Capability(FieldToolKnowledgeTask, LiveKit); got.Tag != Core {
		t.Errorf("task-scoped knowledge on livekit = %q, want %q", got.Tag, Core)
	}
	got := table.Capability(FieldToolKnowledgeTask, Pipecat)
	if got.Tag != Gated {
		t.Errorf("task-scoped knowledge on pipecat = %q, want %q", got.Tag, Gated)
	}
	if strings.TrimSpace(got.Note) == "" {
		t.Error("task-scoped knowledge is gated on pipecat with no note, so the author is told nothing")
	}
}

// TestEmbeddingServiceTable: every row carries the facts the emitted project and
// the docs need, and the default resolves. The Verified date is not decoration:
// CLAUDE.md requires provider claims to be checked against current official
// documentation, and a row with no date cannot be audited.
func TestEmbeddingServiceTable(t *testing.T) {
	names := EmbeddingServiceNames()
	if len(names) == 0 {
		t.Fatal("no embedding services, so this test would pass for the wrong reason")
	}
	for _, name := range names {
		service, ok := LookupEmbeddingService(name)
		if !ok {
			t.Fatalf("%s is listed but does not resolve", name)
		}
		if service.Name != name {
			t.Errorf("%s: Name = %q", name, service.Name)
		}
		for label, value := range map[string]string{
			"Model": service.Model, "PythonDep": service.PythonDep,
			"PythonModule": service.PythonModule, "PythonClass": service.PythonClass,
			"Docs": service.Docs, "Verified": service.Verified,
		} {
			if strings.TrimSpace(value) == "" {
				t.Errorf("%s: %s is empty", name, label)
			}
		}
		if !strings.HasPrefix(service.PythonDep, "llama-index-embeddings-") {
			t.Errorf("%s: PythonDep = %q, want a llama-index-embeddings-* package", name, service.PythonDep)
		}
	}
	if _, ok := LookupEmbeddingService(DefaultEmbeddingService); !ok {
		t.Errorf("the default embedding service %q is not in the table", DefaultEmbeddingService)
	}
	if _, ok := LookupEmbeddingService("cohere"); ok {
		t.Error("an unsupported service resolved")
	}
}

// The Pipecat history row, per value, and the one refusal left on it.
//
// Pinned per value rather than left to the completeness loop above, because
// the loop only asks that a cell holds something. Which cell holds what is the
// whole of what an author can express: before the driver read
// ir.TaskContext.History at all, four of the five values failed here as a
// maturity gate, and that gate is what this row stops being.
//
// `summary` still fails, and its note has to name what Pipecat does support.
// The old note covered four failing values with "emits history: full only",
// which stopped being true the moment three of them started working.
func TestPipecatHistoryRowIsPerValue(t *testing.T) {
	table := Default()
	for history, want := range map[History]HistoryKind{
		HistoryFull:     HistoryOK,
		HistoryMessages: HistoryOK,
		HistoryLastN:    HistoryOK,
		HistoryReset:    HistoryOK,
		HistorySummary:  HistoryFail,
	} {
		if got := table.HistorySupport(history, Pipecat).Kind; got != want {
			t.Errorf("history %s on pipecat = %s, want %s", history, got, want)
		}
	}
	// Every livekit value keeps working, because Story 2 is Pipecat catching up
	// and a table edit is one place to break both.
	for history, want := range map[History]HistoryKind{
		HistoryFull:     HistoryOK,
		HistoryMessages: HistoryOK,
		HistoryLastN:    HistoryOK,
		HistoryReset:    HistoryOK,
		HistorySummary:  HistoryGenerated,
	} {
		if got := table.HistorySupport(history, LiveKit).Kind; got != want {
			t.Errorf("history %s on livekit = %s, want %s", history, got, want)
		}
	}
	note := table.HistorySupport(HistorySummary, Pipecat).Note
	for _, want := range []string{"messages", "last_n", "reset", "full"} {
		if !strings.Contains(note, want) {
			t.Errorf("the pipecat summary refusal does not name %q, so it says what fails and not what works: %q", want, note)
		}
	}
	// A value that passes carries no note: a note on a passing row reads as a
	// warning the author cannot act on.
	for _, history := range []History{HistoryFull, HistoryMessages, HistoryLastN, HistoryReset} {
		if note := table.HistorySupport(history, Pipecat).Note; note != "" {
			t.Errorf("history %s passes on pipecat and still carries a note: %q", history, note)
		}
	}
}

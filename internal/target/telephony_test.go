package target

import (
	"slices"
	"strings"
	"testing"
)

// The auto-webhook fact is carrier data backed by a CLI implementation. The
// only implementation is Twilio (internal/cli/dev_twilio.go), so exactly the
// Twilio routes carry it (Pipecat carrier-websocket and the LiveKit connector),
// each naming a declared public endpoint. A fact on any non-Twilio route means
// someone added data without a CLI implementation (SPEC V3, C5).
func TestTelephonyAutoWebhookIsATwilioFactOnly(t *testing.T) {
	routes := TelephonyRoutes()
	twilioRoutes := map[TelephonyKey]bool{
		{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "twilio"}: true,
		{Provider: LiveKit, Transport: "connector", Carrier: "twilio"}:         true,
	}
	for key, route := range routes {
		if route.AutoWebhookEndpoint == "" {
			continue
		}
		if !twilioRoutes[key] {
			t.Fatalf("route %v carries auto-webhook fact %q without a CLI implementation", key, route.AutoWebhookEndpoint)
		}
		if route.AutoWebhookEndpoint != "inbound" {
			t.Fatalf("route %v auto-webhook endpoint = %q, want inbound", key, route.AutoWebhookEndpoint)
		}
		if !slices.ContainsFunc(route.PublicEndpoints, func(rule TelephonyEndpointRule) bool {
			return rule.Name == route.AutoWebhookEndpoint
		}) {
			t.Fatalf("route %v auto-webhook fact names no declared endpoint: %#v", key, route.PublicEndpoints)
		}
	}
	for key := range twilioRoutes {
		if routes[key].AutoWebhookEndpoint != "inbound" {
			t.Fatalf("Twilio route %v must carry the auto-webhook fact", key)
		}
	}
}

// The Daily carrier route's row, in full (SCHEMA N37). The key set is the one
// fact this route adds to the telephony vocabulary, and the two absences are as
// deliberate as the four presences.
func TestPipecatDailyCarrierRouteRow(t *testing.T) {
	key := TelephonyKey{Provider: Pipecat, Transport: "daily-sip", Carrier: "twilio"}
	route, ok := TelephonyRoutes()[key]
	if !ok {
		t.Fatal("the Daily carrier route has no row, so nothing on it can validate")
	}
	if got := strings.Join(route.RequiredEnvironment, ","); got != "account_sid,auth_token,sip_address,from_number" {
		t.Fatalf("required environment = %q, want account_sid,auth_token,sip_address,from_number in that order", got)
	}
	// Daily's dial-out carries no SIP credential auth on any documented surface,
	// so a key here would promise authentication nothing performs (research F3).
	for _, refused := range []string{"sip_username", "sip_password"} {
		if slices.Contains(route.RequiredEnvironment, refused) || slices.Contains(route.OptionalEnvironment, refused) {
			t.Errorf("route accepts %q; carrier termination on this route authenticates by IP allow-list", refused)
		}
	}
	// The CLI never writes a carrier webhook here: the helper's public URL is the
	// operator's to choose, so pointing the number at it is a dictated step.
	if route.AutoWebhookEndpoint != "" {
		t.Errorf("auto-webhook endpoint = %q, want none", route.AutoWebhookEndpoint)
	}
	// RuntimeEnvironment is what the *deployed agent* reads. No trunk ID, and no
	// Redis: this route keeps no shared control record.
	for _, rule := range route.RuntimeEnvironment {
		if rule.Name != "DAILY_API_KEY" {
			t.Errorf("runtime environment carries %q; only the deployed agent's own names belong here", rule.Name)
		}
	}
	if len(route.Processes) != 1 || route.Processes[0].Name != "telephony-helper" {
		t.Fatalf("processes = %#v, want the one operator-run helper", route.Processes)
	}
	if len(route.ManualSteps) == 0 {
		t.Error("the carrier steps are dictated, so the row must summarise them")
	}
}

// No route asks the operator for a trunk ID in either direction (SCHEMA N33 for
// outbound, N36 for inbound). Dialling out carries the carrier's trunk settings
// inline with every call. Inbound cannot work that way, because an unsolicited
// call arrives with no request of ours for configuration to travel with, so it
// keeps its two platform records, but the emitted telephony-setup.sh resolves
// them by phone number at provisioning time. An environment name that carried an
// ID is the thing this feature retired, so the table must never grow one back.
func TestTelephonyRuntimeEnvironmentCarriesNoTrunkIDs(t *testing.T) {
	for key, route := range TelephonyRoutes() {
		for _, rule := range route.RuntimeEnvironment {
			if strings.Contains(rule.Name, "TRUNK") {
				t.Errorf("route %v runtime environment carries the trunk ID %s", key, rule.Name)
			}
		}
		for _, name := range route.LocallySuppliedEnvironment {
			if strings.Contains(name, "TRUNK") {
				t.Errorf("route %v locally supplied environment carries the trunk ID %s", key, name)
			}
		}
	}
}

// FR-016 draws a line the 2026-08-12 rename must not cross. The four SIP values
// are standard SIP trunk settings, so they lost their carrier prefix and the
// same emitted code dials through any carrier with them. A carrier's REST
// account credentials are genuinely that one carrier's, so they keep theirs.
// The route keys themselves never move: renaming one breaks a written package.
func TestTelephonyRouteEnvironmentKeysHoldTheRenameLine(t *testing.T) {
	routes := TelephonyRoutes()
	want := map[TelephonyKey][]string{
		{Provider: LiveKit, Transport: "sip", Carrier: "twilio"}:               {"sip_address", "sip_username", "sip_password", "from_number"},
		{Provider: LiveKit, Transport: "sip", Carrier: "telnyx"}:               {"sip_address", "sip_username", "sip_password", "from_number"},
		{Provider: LiveKit, Transport: "sip", Carrier: "plivo"}:                {"sip_address", "sip_username", "sip_password", "from_number"},
		{Provider: LiveKit, Transport: "connector", Carrier: "twilio"}:         {"account_sid", "auth_token", "from_number"},
		{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "twilio"}: {"account_sid", "auth_token", "from_number"},
		{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "telnyx"}: {"api_key", "public_key", "connection_id", "from_number"},
		{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "plivo"}:  {"auth_id", "auth_token", "from_number"},
	}
	for key, expected := range want {
		route, ok := routes[key]
		if !ok {
			t.Errorf("route %v is missing from the table", key)
			continue
		}
		if got := strings.Join(route.RequiredEnvironment, ","); got != strings.Join(expected, ",") {
			t.Errorf("route %v required environment = %q, want %q", key, got, strings.Join(expected, ","))
		}
	}
}

func TestLiveKitSIPProcessUsesSupportedWorkerCommand(t *testing.T) {
	route := TelephonyRoutes()[TelephonyKey{Provider: LiveKit, Transport: "sip", Carrier: "twilio"}]
	if len(route.Processes) != 1 {
		t.Fatalf("processes = %#v", route.Processes)
	}
	if got := strings.Join(route.Processes[0].Command, " "); got != "uv run python -m livekit.agents start agent.py" {
		t.Fatalf("agent command = %q", got)
	}
}

// TestPipecatCloudWebsocketRouteRow pins the row's defining difference from every
// other telephony row: it declares no process and no public endpoint, because the
// operator hosts nothing (spec FR-001, data-model section 1). A future edit that
// gives this route a process has changed what the route *is*, and that is worth
// failing a test over.
func TestPipecatCloudWebsocketRouteRow(t *testing.T) {
	key := TelephonyKey{Provider: Pipecat, Transport: "cloud-websocket", Carrier: "twilio"}
	route, ok := TelephonyRoutes()[key]
	if !ok {
		t.Fatal("the Pipecat Cloud websocket route is missing from the table")
	}
	want := []TelephonyFeature{
		TelephonyRouteSelected, TelephonyInbound, TelephonyOutbound,
		TelephonyFeature(ColdTransfer), TelephonyFeature(Hangup),
	}
	if len(route.Features) != len(want) {
		t.Errorf("route grants %d features, want exactly %d: %v", len(route.Features), len(want), route.Features)
	}
	for _, feature := range want {
		evidence, granted := route.Features[feature]
		if !granted {
			t.Errorf("feature %q is not granted", feature)
			continue
		}
		// The tag tracks a credentialed smoke in CI, which no route in this table
		// has, so it stays provisional even after the live calls of 2026-08-13.
		if evidence.Tag != Provisional {
			t.Errorf("feature %q tag = %q, want provisional until a credentialed smoke runs in CI", feature, evidence.Tag)
		}
		if evidence.Docs == "" || evidence.Verified == "" {
			t.Errorf("feature %q lacks docs or a verification date", feature)
		}
		// The note tracks what a human actually did, which is the half a reader
		// trusts. Both clauses are required: what ran, and what is still unrun. A
		// note carrying only the good half is how a row starts reading as a record
		// of success.
		for _, clause := range []string{"live inbound and cold transfer run 2026-08-13", "decline path", "has not been run"} {
			if !strings.Contains(evidence.Note, clause) {
				t.Errorf("feature %q note = %q, want it to name %q", feature, evidence.Note, clause)
			}
		}
	}
	if len(route.Processes) != 0 {
		t.Errorf("route declares %d process(es); zero operator-hosted infrastructure is the feature", len(route.Processes))
	}
	if len(route.PublicEndpoints) != 0 {
		t.Errorf("route declares %d public endpoint(s); the operator hosts none", len(route.PublicEndpoints))
	}
	if route.AutoWebhookEndpoint != "" {
		t.Errorf("route names auto-webhook endpoint %q, but production points the number at a console object", route.AutoWebhookEndpoint)
	}
	if got := strings.Join(route.RequiredEnvironment, ","); got != "account_sid,auth_token,from_number" {
		t.Errorf("required environment = %q, want the three carrier keys", got)
	}
	if len(route.RuntimeEnvironment) != 1 || route.RuntimeEnvironment[0].Name != "PIPECAT_CLOUD_ORGANIZATION" {
		t.Errorf("runtime environment = %+v, want only PIPECAT_CLOUD_ORGANIZATION", route.RuntimeEnvironment)
	}
	if len(route.ManualSteps) == 0 {
		t.Error("route has no dictated carrier steps, so `unmute validate` can tell nobody what to do")
	}
	// The Daily carrier row is the comparison the docs make, so the difference is
	// asserted rather than described: that one runs a helper, this one runs nothing.
	daily := TelephonyRoutes()[TelephonyKey{Provider: Pipecat, Transport: "daily-sip", Carrier: "twilio"}]
	if len(daily.Processes) == 0 {
		t.Error("the Daily carrier row lost its helper process, so this comparison no longer means anything")
	}
}

// The refusal has to name what it would take. An author reading "no warm
// transfer" needs to know whether to change route or change platform.
func TestPipecatCloudWebsocketRefusesWarmTransferByNamingTheCost(t *testing.T) {
	key := TelephonyKey{Provider: Pipecat, Transport: "cloud-websocket", Carrier: "twilio"}
	evidence := ResolveTelephonyFeature(key, TelephonyFeature(WarmTransfer))
	if evidence.Tag != Gated {
		t.Fatalf("warm transfer tag = %q, want gated", evidence.Tag)
	}
	for _, want := range []string{"callback endpoint you host", "hosting\nnothing", "livekit, sip"} {
		if !strings.Contains(evidence.Note, strings.ReplaceAll(want, "\n", " ")) {
			t.Errorf("warm refusal %q does not mention %q", evidence.Note, want)
		}
	}
	if strings.Contains(evidence.Note, "cannot") {
		t.Errorf("warm refusal %q blames the platform; it is this project that has not built it", evidence.Note)
	}
	// A call source is refused too, and its refusal already names the routes that
	// fill one, so an author who wants the caller's number has somewhere to go.
	source := ResolveTelephonyFeature(key, "source.from_number")
	if source.Tag != Gated || !strings.Contains(source.Note, "carrier-websocket") {
		t.Errorf("call-source refusal = %q (%s), want gated and naming where sources work", source.Note, source.Tag)
	}
}

// SelectableTelephonyRoutes is the single home for "which routes can an author
// actually pick" (research R6). The two Exotel rows are the reason it exists:
// they carry a real environment vocabulary and an empty feature map, so
// ResolveTelephonyFeature refuses them. Offering one in a "did you mean" list
// or a TUI picker walks the author into a second, different refusal.
func TestSelectableTelephonyRoutesExcludesPlaceholderRows(t *testing.T) {
	selectable := SelectableTelephonyRoutes()

	for _, key := range []TelephonyKey{
		{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "exotel"},
		{Provider: LiveKit, Transport: "sip", Carrier: "exotel"},
	} {
		if _, ok := selectable[key]; ok {
			t.Errorf("%+v has no route feature and must not be selectable", key)
		}
		if _, ok := TelephonyRoutes()[key]; !ok {
			t.Errorf("%+v left the catalog; this test guards the wrong rows now", key)
		}
	}

	// The other direction: every row the table marks selectable is offered.
	for key, route := range TelephonyRoutes() {
		_, declared := route.Features[TelephonyRouteSelected]
		_, offered := selectable[key]
		if declared != offered {
			t.Errorf("%+v: route feature %v but selectable %v", key, declared, offered)
		}
	}

	if len(selectable) == 0 {
		t.Fatal("no selectable routes: the predicate is inverted")
	}
}

// routePlaneExpectation is every row's two new facts, written out. A table of
// expected values rather than a "is it non-zero" check, because that is the
// only shape that fails when somebody adds a route and decides nothing: an
// unlisted key is a failure here, and so is a listed key whose value moved.
//
// Gate P1 (contracts/local-planes.md) and task T104.
var routePlaneExpectation = map[TelephonyKey]struct {
	plane        TelephonyLocalPlane
	cloudDeploys bool
}{
	// Pipecat's one self-hosted transport: a plain container and a Compose file,
	// no deployment manifest.
	{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "twilio"}: {LocalPlaneMediaWebsocket, false},
	{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "telnyx"}: {LocalPlaneMediaWebsocket, false},
	{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "plivo"}:  {LocalPlaneMediaWebsocket, false},
	// The two rows with an empty feature map. They compile to nothing, so they
	// have no local loop to offer, and they are listed because the fields are
	// assigned on every row.
	{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "exotel"}: {LocalPlaneNone, false},
	{Provider: LiveKit, Transport: "sip", Carrier: "exotel"}:               {LocalPlaneNone, true},
	// Pipecat on the same plane, and the one row where CloudDeploys being false
	// is the whole point: this route has no managed-platform path anywhere, which
	// is what makes it the self-hosted answer (US3).
	{Provider: Pipecat, Transport: "sip", Carrier: "twilio"}: {LocalPlaneSIP, false},
	{Provider: Pipecat, Transport: "sip", Carrier: "telnyx"}: {LocalPlaneSIP, false},
	{Provider: Pipecat, Transport: "sip", Carrier: "plivo"}:  {LocalPlaneSIP, false},
	// A real SIP trunk in production, a real SIP stack locally.
	{Provider: LiveKit, Transport: "sip", Carrier: "twilio"}: {LocalPlaneSIP, true},
	{Provider: LiveKit, Transport: "sip", Carrier: "telnyx"}: {LocalPlaneSIP, true},
	{Provider: LiveKit, Transport: "sip", Carrier: "plivo"}:  {LocalPlaneSIP, true},
	// The carrier leg terminates in a third-party hosted service, so there is
	// nothing on the machine to stand in for it.
	{Provider: Pipecat, Transport: "daily-sip", Carrier: "twilio"}: {LocalPlaneNone, true},
	// The platform terminates the same protocol the carrier-websocket routes use.
	{Provider: Pipecat, Transport: "cloud-websocket", Carrier: "twilio"}: {LocalPlaneMediaWebsocket, true},
	// The carrier streams media over a WebSocket into our own bridge.
	{Provider: LiveKit, Transport: "connector", Carrier: "twilio"}: {LocalPlaneMediaWebsocket, true},
}

// Gate P1, first half: the plane is decided on every row, including the rows
// that refuse. LocalPlaneNone is a real value rather than the zero value
// precisely so this test can tell a decision from an omission.
func TestTelephonyLocalPlaneIsDecidedOnEveryRoute(t *testing.T) {
	routes := TelephonyRoutes()
	for key, route := range routes {
		want, known := routePlaneExpectation[key]
		if !known {
			t.Errorf("route %+v has no expected plane: a new route must decide its "+
				"local plane, and LocalPlaneNone is a legitimate answer", key)
			continue
		}
		if route.LocalPlane == "" {
			t.Errorf("route %+v has the zero-value plane, which means nobody decided; "+
				"use LocalPlaneNone to say there is no local loop", key)
		}
		if route.LocalPlane != want.plane {
			t.Errorf("route %+v plane = %q, want %q", key, route.LocalPlane, want.plane)
		}
	}
	for key := range routePlaneExpectation {
		if _, ok := routes[key]; !ok {
			t.Errorf("expected route %+v left the table; this test guards a row that no longer exists", key)
		}
	}
}

// Gate P1, second half: the plane presents the mechanism the route's transport
// actually uses. A route may not pick a more convenient plane than its carrier.
func TestTelephonyLocalPlaneMatchesTheRouteTransport(t *testing.T) {
	byTransport := map[string]TelephonyLocalPlane{
		"sip":               LocalPlaneSIP,
		"carrier-websocket": LocalPlaneMediaWebsocket,
		"cloud-websocket":   LocalPlaneMediaWebsocket,
		"connector":         LocalPlaneMediaWebsocket,
		// The carrier leg is a hosted service, so no plane can present it.
		"daily-sip": LocalPlaneNone,
	}
	for key, route := range SelectableTelephonyRoutes() {
		want, known := byTransport[key.Transport]
		if !known {
			t.Errorf("transport %q has no plane mechanism recorded", key.Transport)
			continue
		}
		if route.LocalPlane != want {
			t.Errorf("route %+v on transport %q has plane %q, want %q: the plane must "+
				"present the same call mechanism the carrier uses in production",
				key, key.Transport, route.LocalPlane, want)
		}
	}
}

// T104. FR-024's refusal reads CloudDeploys and must never fire on a route that
// can deploy to a managed platform, so the assertion covers both providers
// rather than only the one that motivated the field. Inferring the value from
// the provider would have rejected examples/salon-concierge.
func TestTelephonyCloudDeploysIsDecidedAndNeverFalseOnLiveKit(t *testing.T) {
	for key, route := range TelephonyRoutes() {
		want, known := routePlaneExpectation[key]
		if !known {
			t.Errorf("route %+v has no expected CloudDeploys value", key)
			continue
		}
		if route.CloudDeploys != want.cloudDeploys {
			t.Errorf("route %+v CloudDeploys = %v, want %v", key, route.CloudDeploys, want.cloudDeploys)
		}
		if key.Provider == LiveKit && !route.CloudDeploys {
			t.Errorf("LiveKit route %+v has CloudDeploys false. Every LiveKit route "+
				"deploys either to LiveKit Cloud or to a server the author runs, "+
				"chosen by where LIVEKIT_URL points, and FR-024's refusal must not "+
				"fire on it", key)
		}
	}
}

// Gate C7. `make rig` is deliberately credential-free, and the tag tracks a
// credentialed check in CI. So a green rig must never promote a capability out
// of provisional: FR-020 limits the change to the note.
func TestTelephonyCapabilityTagsStayProvisional(t *testing.T) {
	seen := 0
	for key, route := range TelephonyRoutes() {
		for feature, evidence := range route.Features {
			seen++
			if evidence.Tag != Provisional {
				t.Errorf("route %+v feature %q tag = %q, want provisional: no route in "+
					"this table has a credentialed smoke in CI, and the credential-free "+
					"rig does not count as one", key, feature, evidence.Tag)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no capabilities inspected: the table is empty or the loop is wrong")
	}
}

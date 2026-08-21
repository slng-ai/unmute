package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

// fakeSIPAdmin is an in-memory Twirp livekit.SIP server: List* returns the
// stored records, Create* stores and returns the record with a fresh ID.
type fakeSIPAdmin struct {
	t          *testing.T
	inbound    []map[string]any
	outbound   []map[string]any
	rules      []map[string]any
	requests   []string
	bodies     map[string]string
	auth       map[string]string
	dispatches []map[string]any
}

func newFakeSIPAdmin(t *testing.T) (*fakeSIPAdmin, *httptest.Server) {
	t.Helper()
	fake := &fakeSIPAdmin{t: t, bodies: map[string]string{}, auth: map[string]string{}}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	restore := liveKitSIPAdminBase
	liveKitSIPAdminBase = func([]string) string { return server.URL }
	t.Cleanup(func() { liveKitSIPAdminBase = restore })
	return fake, server
}

func (f *fakeSIPAdmin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return
	}
	var payload map[string]any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	write := func(v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	// AgentDispatchService is a separate Twirp service (outbound dial-out).
	if dispatchMethod, ok := strings.CutPrefix(r.URL.Path, "/twirp/livekit.AgentDispatchService/"); ok {
		f.requests = append(f.requests, dispatchMethod)
		f.bodies[dispatchMethod] = string(body)
		f.auth[dispatchMethod] = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if dispatchMethod == "CreateDispatch" {
			f.dispatches = append(f.dispatches, payload)
			write(map[string]any{"id": "AD_1", "agent_name": payload["agent_name"], "room": payload["room"]})
			return
		}
		http.Error(w, "unknown method "+dispatchMethod, http.StatusNotFound)
		return
	}
	method := strings.TrimPrefix(r.URL.Path, "/twirp/livekit.SIP/")
	f.requests = append(f.requests, method)
	f.bodies[method] = string(body)
	switch method {
	case "ListSIPInboundTrunk":
		write(map[string]any{"items": f.inbound})
	case "ListSIPOutboundTrunk":
		write(map[string]any{"items": f.outbound})
	case "ListSIPDispatchRule":
		write(map[string]any{"items": f.rules})
	case "CreateSIPInboundTrunk":
		record, _ := payload["trunk"].(map[string]any)
		record["sipTrunkId"] = "ST_in_1"
		f.inbound = append(f.inbound, record)
		write(record)
	case "CreateSIPOutboundTrunk":
		record, _ := payload["trunk"].(map[string]any)
		record["sipTrunkId"] = "ST_out_1"
		// A real server never echoes auth back in listings.
		stored := map[string]any{"sipTrunkId": "ST_out_1", "name": record["name"], "address": record["address"], "numbers": record["numbers"]}
		f.outbound = append(f.outbound, stored)
		write(record)
	case "CreateSIPDispatchRule":
		record, _ := payload["dispatch_rule"].(map[string]any)
		record["sipDispatchRuleId"] = "SDR_1"
		f.rules = append(f.rules, record)
		write(record)
	case "UpdateSIPInboundTrunk":
		id, _ := payload["sip_trunk_id"].(string)
		replacement, _ := payload["replace"].(map[string]any)
		for i, trunk := range f.inbound {
			trunkID, _ := trunk["sipTrunkId"].(string)
			if snake, _ := trunk["sip_trunk_id"].(string); trunkID == "" {
				trunkID = snake
			}
			if trunkID == id {
				replacement["sipTrunkId"] = id
				f.inbound[i] = replacement
			}
		}
		write(replacement)
	case "UpdateSIPDispatchRule":
		id, _ := payload["sipDispatchRuleId"].(string)
		replacement, _ := payload["replace"].(map[string]any)
		for i, rule := range f.rules {
			ruleID, _ := rule["sipDispatchRuleId"].(string)
			if snake, _ := rule["sip_dispatch_rule_id"].(string); ruleID == "" {
				ruleID = snake
			}
			if ruleID == id {
				replacement["sipDispatchRuleId"] = id
				f.rules[i] = replacement
			}
		}
		write(replacement)
	default:
		http.Error(w, "unknown method "+method, http.StatusNotFound)
	}
}

func livekitSIPPlan() *generate.TelephonyRuntimePlan {
	return &generate.TelephonyRuntimePlan{
		Route:       ir.TelephonyKey{Provider: ir.ProviderLiveKit, Transport: "sip", Carrier: "twilio"},
		RequiredEnv: []string{"TWILIO_SIP_ADDRESS", "TWILIO_SIP_USERNAME", "TWILIO_SIP_PASSWORD", "TWILIO_PHONE_NUMBER"},
		Environment: map[string]string{
			"sip_address": "TWILIO_SIP_ADDRESS", "sip_username": "TWILIO_SIP_USERNAME",
			"sip_password": "TWILIO_SIP_PASSWORD", "from_number": "TWILIO_PHONE_NUMBER",
		},
		Evidence:     []ir.TelephonyFeatureEvidence{{Feature: "inbound", Tag: "core"}, {Feature: "outbound", Tag: "core"}},
		Services:     []string{"application", "livekit_server", "livekit_sip", "redis"},
		Coordination: "shared",
		LocalPlane:   string(target.LocalPlaneSIP),
		PlaneSubnet:  "10.185.61.0/24", PlaneSIPAddress: "10.185.61.10",
		LocalEndpoints: []ir.TelephonyLocalEndpoint{
			{
				Role: ir.TelephonyRoleCaller, Name: "caller", Service: "telephony_caller",
				Address: "10.185.61.20", Port: 5060, Recording: "caller.wav",
			},
			{
				Role: ir.TelephonyRoleDestination, Name: "billing_line", Service: "telephony_destinations",
				Address: "10.185.61.21", Port: 5060, Recording: "billing_line.wav",
				EnvName: "BILLING_PHONE_NUMBER",
			},
		},
	}
}

// testDialCredential is a fixed credential, so a test can assert on the value
// the run would have minted. Never what production uses: newDialCredential is.
func testDialCredential() dialCredential {
	return dialCredential{Username: "dev-a3k9xz", Password: "mnp4x7q2rtvw"}
}

func sipTestEnv() []string {
	return []string{
		"TWILIO_SIP_ADDRESS=example.pstn.twilio.com", "TWILIO_SIP_USERNAME=user",
		"TWILIO_SIP_PASSWORD=sip-sekrit-88", "TWILIO_PHONE_NUMBER=+15550001111",
	}
}

// V4: first run lists (empty) and creates the two inbound records; second run
// reuses both and creates nothing.
//
// No outbound trunk is created, since 2026-08-12 (SCHEMA N33): the generated
// agent dials with the carrier's trunk settings inline, so local development
// uses the same mechanism a deployment does. The SIP password therefore no
// longer travels in any request body here, and the leak check below matters
// more rather than less, because the values are still in the environment.
func TestEnsureLiveKitSIPRecordsIsIdempotent(t *testing.T) {
	fake, _ := newFakeSIPAdmin(t)
	plan := livekitSIPPlan()
	var out strings.Builder

	if err := ensureLiveKitSIPRecords(context.Background(), &out, "phone", plan, sipTestEnv(), testDialCredential()); err != nil {
		t.Fatal(err)
	}
	first := strings.Join(fake.requests, ",")
	for _, want := range []string{"ListSIPInboundTrunk,CreateSIPInboundTrunk", "ListSIPDispatchRule,CreateSIPDispatchRule"} {
		if !strings.Contains(first, want) {
			t.Errorf("first run requests missing list-before-create %q: %s", want, first)
		}
	}
	if strings.Contains(first, "SIPOutboundTrunk") {
		t.Fatalf("first run touched the outbound trunk API: %s", first)
	}
	if !strings.Contains(fake.bodies["CreateSIPDispatchRule"], `"roomPrefix":"call-"`) || !strings.Contains(fake.bodies["CreateSIPDispatchRule"], `"agentName":"phone"`) {
		t.Fatalf("dispatch rule create = %s", fake.bodies["CreateSIPDispatchRule"])
	}
	printed := out.String()
	if strings.Contains(printed, "sip-sekrit-88") {
		t.Fatalf("printed output leaks the SIP password:\n%s", printed)
	}
	if !strings.Contains(printed, "LiveKit inbound trunk ST_in_1 (created)") {
		t.Fatalf("printed output = %s", printed)
	}

	fake.requests = nil
	out.Reset()
	if err := ensureLiveKitSIPRecords(context.Background(), &out, "phone", plan, sipTestEnv(), testDialCredential()); err != nil {
		t.Fatal(err)
	}
	second := strings.Join(fake.requests, ",")
	if strings.Contains(second, "Create") {
		t.Fatalf("second run duplicated records: %s", second)
	}
	if !strings.Contains(out.String(), "(reused)") {
		t.Fatalf("second run output = %s", out.String())
	}
}

// V4: snake_case Twirp responses resolve the same records (protojson
// accepts and some servers emit proto field names).
func TestEnsureLiveKitSIPRecordsReadsSnakeCaseResponses(t *testing.T) {
	fake, _ := newFakeSIPAdmin(t)
	fake.inbound = []map[string]any{{"sip_trunk_id": "ST_in_9", "numbers": []any{"+15550001111"}}}
	fake.outbound = []map[string]any{{"sip_trunk_id": "ST_out_9", "address": "example.pstn.twilio.com", "numbers": []any{"+15550001111"}}}
	fake.rules = []map[string]any{{
		"sip_dispatch_rule_id": "SDR_9",
		"trunk_ids":            []any{"ST_in_9"},
		"rule":                 map[string]any{"dispatch_rule_individual": map[string]any{"room_prefix": "call-"}},
		"room_config":          map[string]any{"agents": []any{map[string]any{"agent_name": "phone"}}},
	}}
	var out strings.Builder
	if err := ensureLiveKitSIPRecords(context.Background(), &out, "phone", livekitSIPPlan(), sipTestEnv(), testDialCredential()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "LiveKit inbound trunk ST_in_9 (reused)") {
		t.Fatalf("snake_case record was not resolved: %s", out.String())
	}
	if strings.Contains(strings.Join(fake.requests, ","), "Create") {
		t.Fatalf("snake_case records were not reused: %v", fake.requests)
	}
}

// V4/B2: a dispatch rule with the right trunk and prefix but the wrong
// dispatched agent is replaced in place, never reused and never duplicated.
func TestEnsureLiveKitSIPRecordsReplacesDispatchRuleOnAgentMismatch(t *testing.T) {
	fake, _ := newFakeSIPAdmin(t)
	fake.inbound = []map[string]any{{"sipTrunkId": "ST_in_1", "numbers": []any{"+15550001111"}}}
	fake.outbound = []map[string]any{{"sipTrunkId": "ST_out_1", "address": "example.pstn.twilio.com", "numbers": []any{"+15550001111"}}}
	fake.rules = []map[string]any{{
		"sipDispatchRuleId": "SDR_stale",
		"trunkIds":          []any{"ST_in_1"},
		"rule":              map[string]any{"dispatchRuleIndividual": map[string]any{"roomPrefix": "call-"}},
		"roomConfig":        map[string]any{"agents": []any{map[string]any{"agentName": "old-target"}}},
	}}
	var out strings.Builder
	if err := ensureLiveKitSIPRecords(context.Background(), &out, "phone", livekitSIPPlan(), sipTestEnv(), testDialCredential()); err != nil {
		t.Fatal(err)
	}
	requests := strings.Join(fake.requests, ",")
	if strings.Contains(requests, "CreateSIPDispatchRule") || !strings.Contains(requests, "UpdateSIPDispatchRule") {
		t.Fatalf("mismatched rule was not replaced in place: %s", requests)
	}
	if !strings.Contains(fake.bodies["UpdateSIPDispatchRule"], `"sipDispatchRuleId":"SDR_stale"`) ||
		!strings.Contains(fake.bodies["UpdateSIPDispatchRule"], `"agentName":"phone"`) {
		t.Fatalf("replace body = %s", fake.bodies["UpdateSIPDispatchRule"])
	}
	if !strings.Contains(out.String(), `dispatch rule "unmute phone inbound" (updated)`) {
		t.Fatalf("output = %s", out.String())
	}
}

// T086: the dispatch rule carries no roomConfig on a route whose agent is not a
// LiveKit worker, because a worker is the only thing it can dispatch and this
// route registers none. It is a record of something that would never happen.
//
// Not the thing that answers a call, deliberately: that is a subscribed audio
// track in the room (livekit/sip waitSubscribe), which is what the room webhook
// and the agent joining provide.
func TestDispatchRuleNamesNoAgentWhenTheAgentIsNotAWorker(t *testing.T) {
	fake, _ := newFakeSIPAdmin(t)
	plan := livekitSIPPlan()
	plan.Route = ir.TelephonyKey{Provider: ir.ProviderPipecat, Transport: "sip", Carrier: "twilio"}
	var out strings.Builder
	if err := ensureLiveKitSIPRecords(context.Background(), &out, "phone", plan, sipTestEnv(), testDialCredential()); err != nil {
		t.Fatal(err)
	}
	body := fake.bodies["CreateSIPDispatchRule"]
	if !strings.Contains(body, `"roomPrefix":"call-"`) {
		t.Fatalf("the rule does not name the room prefix an inbound call lands on: %s", body)
	}
	if strings.Contains(body, "roomConfig") || strings.Contains(body, "agentName") {
		t.Fatalf("the rule promises an agent this route never registers: %s", body)
	}
	// And the LiveKit route keeps its worker, because that route's agent is one.
	fake.bodies = map[string]string{}
	fake.rules = nil
	if err := ensureLiveKitSIPRecords(context.Background(), &out, "phone", livekitSIPPlan(), sipTestEnv(), testDialCredential()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fake.bodies["CreateSIPDispatchRule"], `"agentName":"phone"`) {
		t.Fatalf("the LiveKit route lost its worker dispatch: %s", fake.bodies["CreateSIPDispatchRule"])
	}
}

// The Redis volume outlives a run, so a rule left by a LiveKit run on the same
// machine is still there when a Pipecat package starts. Reusing it would hand
// this route the ringing-forever rule it just stopped creating, so a rule whose
// dispatched agent does not match what the route wants is replaced either way.
func TestDispatchRuleReplacesAWorkerRuleForANonWorkerRoute(t *testing.T) {
	fake, _ := newFakeSIPAdmin(t)
	fake.inbound = []map[string]any{{"sipTrunkId": "ST_in_1", "numbers": []any{"+15550001111"}}}
	fake.rules = []map[string]any{{
		"sipDispatchRuleId": "SDR_worker",
		"trunkIds":          []any{"ST_in_1"},
		"rule":              map[string]any{"dispatchRuleIndividual": map[string]any{"roomPrefix": "call-"}},
		"roomConfig":        map[string]any{"agents": []any{map[string]any{"agentName": "phone"}}},
	}}
	plan := livekitSIPPlan()
	plan.Route = ir.TelephonyKey{Provider: ir.ProviderPipecat, Transport: "sip", Carrier: "twilio"}
	var out strings.Builder
	if err := ensureLiveKitSIPRecords(context.Background(), &out, "phone", plan, sipTestEnv(), testDialCredential()); err != nil {
		t.Fatal(err)
	}
	replace := fake.bodies["UpdateSIPDispatchRule"]
	if replace == "" {
		t.Fatalf("a stale worker rule was reused rather than replaced: %v", fake.requests)
	}
	if strings.Contains(replace, "roomConfig") {
		t.Fatalf("the replacement still promises a worker: %s", replace)
	}
}

// The gate that switches on the two-phase local startup: infrastructure, then
// these records, then the application. It used to read a dev-supplied
// environment name, which is retired (SCHEMA N36), so it now reads the route and
// the inbound feature. This test exists because deleting the gate rather than
// moving it would silently start the application before any record exists, and
// nothing else in the suite would notice.
func TestPlanCreatesLiveKitSIPRecordsOnlyForInboundSIP(t *testing.T) {
	inboundOutbound := livekitSIPPlan()
	outboundOnly := livekitSIPPlan()
	outboundOnly.Evidence = []ir.TelephonyFeatureEvidence{{Feature: "outbound", Tag: "core"}}
	connector := livekitSIPPlan()
	connector.Route = ir.TelephonyKey{Provider: ir.ProviderLiveKit, Transport: "connector", Carrier: "twilio"}
	connector.LocalPlane = string(target.LocalPlaneMediaWebsocket)
	websocket := livekitSIPPlan()
	websocket.Route = ir.TelephonyKey{Provider: ir.ProviderPipecat, Transport: "carrier-websocket", Carrier: "twilio"}
	websocket.LocalPlane = string(target.LocalPlaneMediaWebsocket)
	// The route whose agent is a Pipecat bot on the same plane. It needs the same
	// records for the same reason, and reading the provider instead of the plane
	// is what left it without them.
	pipecatSIP := livekitSIPPlan()
	pipecatSIP.Route = ir.TelephonyKey{Provider: ir.ProviderPipecat, Transport: "sip", Carrier: "twilio"}

	for _, tc := range []struct {
		name string
		plan *generate.TelephonyRuntimePlan
		want bool
	}{
		{"livekit sip inbound", inboundOutbound, true},
		{"livekit sip outbound only", outboundOnly, false},
		{"livekit connector, inbound but no trunk", connector, false},
		{"pipecat carrier websocket, inbound but no trunk", websocket, false},
		{"pipecat sip inbound, same plane", pipecatSIP, true},
	} {
		if got := planCreatesLiveKitSIPRecords(tc.plan); got != tc.want {
			t.Errorf("%s: creates records = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// C1/C2: the SIP admin token is an HS256 JWT whose claims carry the sip
// admin grant and the api key as issuer.
func TestMintLiveKitSIPAdminTokenCarriesSIPAdminGrant(t *testing.T) {
	token, err := mintLiveKitSIPAdminToken("devkey", "secret", time.Unix(1_800_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		Iss string `json:"iss"`
		Exp int64  `json:"exp"`
		SIP struct {
			Admin bool `json:"admin"`
		} `json:"sip"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Iss != "devkey" || !claims.SIP.Admin || claims.Exp <= 1_800_000_000 {
		t.Fatalf("claims = %+v", claims)
	}
}

// V5: --to on a SIP plan places the call by dispatching the agent on the local
// server (no /telephony/outbound POST exists for this route). Exactly one
// CreateDispatch fires, with agent_name = target, a fresh call- room, and
// outbound metadata carrying the number. Its roomAdmin token is scoped to that
// exact room, and the SIP auth never appears in output.
func TestV5_PlaceLiveKitDispatchScopesRoomAdminToRequestRoom(t *testing.T) {
	fake, _ := newFakeSIPAdmin(t)
	var out strings.Builder
	env := append(sipTestEnv(), `UNMUTE_CALL_START={"name":"Ada","attempts":2}`)
	if err := placeLiveKitDispatch(context.Background(), &out, "phone", "+15557778888", env); err != nil {
		t.Fatal(err)
	}
	if len(fake.dispatches) != 1 {
		t.Fatalf("expected one CreateDispatch, got %d: %v", len(fake.dispatches), fake.requests)
	}
	got := fake.dispatches[0]
	if got["agent_name"] != "phone" {
		t.Errorf("agent_name = %v", got["agent_name"])
	}
	room, _ := got["room"].(string)
	if !strings.HasPrefix(room, "call-") {
		t.Errorf("room = %q, want a fresh call- room", room)
	}
	parts := strings.Split(fake.auth["CreateDispatch"], ".")
	if len(parts) != 3 {
		t.Fatalf("dispatch token has %d parts", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		Video struct {
			RoomAdmin bool   `json:"roomAdmin"`
			Room      string `json:"room"`
		} `json:"video"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatal(err)
	}
	if !claims.Video.RoomAdmin || claims.Video.Room != room {
		t.Fatalf("dispatch grant = %+v, want roomAdmin scoped to request room %q", claims.Video, room)
	}
	metadata, _ := got["metadata"].(string)
	for _, want := range []string{`"direction":"outbound"`, `"phone_number":"+15557778888"`, `"call_start":{"attempts":2,"name":"Ada"}`} {
		if !strings.Contains(metadata, want) {
			t.Errorf("metadata missing %q: %s", want, metadata)
		}
	}
	printed := out.String()
	if !strings.Contains(printed, "calling +15557778888") || !strings.Contains(printed, "dispatch AD_1") {
		t.Errorf("call line = %q", printed)
	}
	if strings.Contains(printed, "sip-sekrit") {
		t.Errorf("output leaks SIP auth: %s", printed)
	}
}

// The dispatch token is an HS256 JWT with the room-scoped roomAdmin grant the
// AgentDispatchService requires.
func TestMintLiveKitDispatchTokenCarriesRoomAdmin(t *testing.T) {
	token, err := mintLiveKitDispatchToken("devkey", "secret", "call-test", time.Unix(1_800_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		Iss   string `json:"iss"`
		Video struct {
			RoomAdmin bool   `json:"roomAdmin"`
			Room      string `json:"room"`
		} `json:"video"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Iss != "devkey" || !claims.Video.RoomAdmin || claims.Video.Room != "call-test" {
		t.Fatalf("claims = %+v", claims)
	}
}

// V4 end to end through the post-gate core: infra services come up first, the
// records are created against the local server, and only then does the
// application start. The application receives no trunk ID, because none exists
// as an environment name any more (SCHEMA N36); the ordering is the whole point,
// since an application that starts first has nowhere for a call to land.
func TestExecDevTelephonySIPCreatesRecordsBetweenInfraAndApp(t *testing.T) {
	newFakeSIPAdmin(t)
	root, trace := fakeTelephonyRoot(t, strings.Join(sipTestEnv(), "\n"))
	fakeDocker(t, root)
	allowHeldPorts(t)
	refuseTunnel(t)

	cmd, out := telephonyTestCommand(t)
	if err := execDevTelephony(cmd, root, "phone", livekitSIPPlan(), composeFiles, devTelephonyOptions{botPort: "8081"}); err != nil {
		t.Fatalf("execDevTelephony: %v\n%s", err, out.String())
	}
	raw, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) < 3 {
		t.Fatalf("trace too short:\n%s", raw)
	}
	if !strings.Contains(lines[0], "--wait livekit_server livekit_sip redis") {
		t.Fatalf("infra services did not come up first: %q", lines[0])
	}
	if !strings.HasSuffix(strings.Split(lines[1], " | ")[0], "--wait") {
		t.Fatalf("the full graph did not come up second: %q", lines[1])
	}
	// The records were created in between, which is what the two phases exist
	// for. No trunk ID reaches the application: nothing reads one.
	for _, want := range []string{"LiveKit inbound trunk ST_in_1 (created)", `LiveKit dispatch rule "unmute phone inbound" (created)`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("records were not created between the phases, output:\n%s", out.String())
		}
	}
	if strings.Contains(string(raw), "ST_in_1") {
		t.Fatalf("a trunk ID reached the container environment:\n%s", raw)
	}
	// This assertion used to be the other way round: it required the run to say
	// a real inbound call needed publicly reachable SIP and RTP ingress, and to
	// print no number. That line was true and it was the defect, so the feature
	// is that it is gone and a dialable address is here instead.
	if strings.Contains(out.String(), "publicly reachable SIP and RTP ingress") {
		t.Errorf("the run still tells the reader it cannot be called:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "dial sip:"+planeLocalNumber+"@") {
		t.Errorf("the run prints no address to dial:\n%s", out.String())
	}
	// The carrier's own number is not what a local call reaches, so printing it
	// would be an invitation to dial something that is not listening.
	if strings.Contains(out.String(), "call +15550001111") {
		t.Errorf("the local run named the carrier's number:\n%s", out.String())
	}
}

// Gate S3: the trunk that accepts a call carries this run's own credential.
// Without it the plane answers anybody who can reach the port, and on the
// softphone profile that port is published on a real interface of the machine.
func TestInboundTrunkCarriesThisRunsCredential(t *testing.T) {
	fake, _ := newFakeSIPAdmin(t)
	credential := testDialCredential()
	var out strings.Builder
	if err := ensureLiveKitSIPRecords(context.Background(), &out, "phone", livekitSIPPlan(), sipTestEnv(), credential); err != nil {
		t.Fatal(err)
	}
	body := fake.bodies["CreateSIPInboundTrunk"]
	for _, want := range []string{
		`"auth_username":"` + credential.Username + `"`,
		`"auth_password":"` + credential.Password + `"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the inbound trunk request does not carry %s: %s", want, body)
		}
	}
	// A run with no plane has no per-run credential, and must not send an empty
	// one: a trunk with a blank username set is a trunk that refuses every call.
	fake.requests, fake.inbound = nil, nil
	fake.bodies = map[string]string{}
	if err := ensureLiveKitSIPRecords(context.Background(), &out, "phone", livekitSIPPlan(), sipTestEnv(), dialCredential{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fake.bodies["CreateSIPInboundTrunk"], "auth_username") {
		t.Errorf("a run with no credential still sent an auth field: %s", fake.bodies["CreateSIPInboundTrunk"])
	}
}

// Gate S3, the other half: the credential is printed, because somebody has to
// type it into a softphone, and it reaches no file the run writes.
func TestDialCredentialIsPrintedAndWrittenNowhere(t *testing.T) {
	newFakeSIPAdmin(t)
	root, _ := fakeTelephonyRoot(t, strings.Join(sipTestEnv(), "\n"))
	fakeDocker(t, root)
	allowHeldPorts(t)
	refuseTunnel(t)
	var report runReport
	cmd, out := telephonyTestCommand(t)
	if err := execDevTelephony(cmd, root, "phone", livekitSIPPlan(), composeFiles,
		devTelephonyOptions{botPort: "8083", report: &report}); err != nil {
		t.Fatalf("plane run: %v\n%s", err, out.String())
	}
	credential := report.DialCredential
	if credential == "" {
		t.Fatal("the run recorded no dial credential, so there is nothing for a caller to authenticate with")
	}
	if !strings.Contains(out.String(), credential) {
		t.Errorf("the credential was never printed, so nobody can place a call:\n%s", out.String())
	}
	// Every file the run touched, not just the ones it meant to write.
	var found []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if strings.Contains(string(content), credential) {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) > 0 {
		t.Errorf("the dial credential was written to %v; it is printed and never stored", found)
	}
}

// Gate S7: the trunk the agent dials transfers through points at the plane's
// own endpoint, not at a carrier. On this route the trunk is not a record at
// all, it is four environment names the agent reads and sends inline, so this
// is where an outbound trunk pointing somewhere local exists or does not.
func TestPlaneTrunkPointsAtItsOwnEndpointsAndNotACarrier(t *testing.T) {
	plan := livekitSIPPlan()
	supplied := planeEnvironment(plan, testDialCredential())

	if got := supplied["TWILIO_SIP_ADDRESS"]; got != "10.185.61.21" {
		t.Errorf("the dial-out trunk points at %q, want the plane's destinations endpoint", got)
	}
	if got := supplied["BILLING_PHONE_NUMBER"]; got != "sip:billing_line@10.185.61.21:5060" {
		t.Errorf("the destination resolves to %q; a cold transfer sends this as a Refer-To and a bare number becomes tel:, which no local caller can route", got)
	}
	if got := supplied["TWILIO_PHONE_NUMBER"]; got != planeLocalNumber {
		t.Errorf("the plane answers on %q, want the fictional local number %q", got, planeLocalNumber)
	}
	// Nothing in the supplied set names a carrier. If it did, the default loop
	// would be reaching for an account the author is not required to have.
	for name, value := range supplied {
		for _, carrier := range []string{"pstn.twilio.com", "telnyx", "plivo", "exotel", ".com"} {
			if strings.Contains(value, carrier) {
				t.Errorf("%s is set to %q, which names something off this machine", name, value)
			}
		}
	}
	// Every name it supplies is one the author no longer has to provide, which
	// is what makes the default loop runnable with no accounts at all.
	for _, name := range plan.RequiredEnv {
		if _, ok := supplied[name]; !ok {
			t.Errorf("the plane does not supply %s, so a default run still demands it from the author", name)
		}
	}
}

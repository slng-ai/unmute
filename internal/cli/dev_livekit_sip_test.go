package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/slng/unmute/internal/generate"
	"github.com/slng/unmute/internal/ir"
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
	dispatches []map[string]any
}

func newFakeSIPAdmin(t *testing.T) (*fakeSIPAdmin, *httptest.Server) {
	t.Helper()
	fake := &fakeSIPAdmin{t: t, bodies: map[string]string{}}
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
	}
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

	if err := ensureLiveKitSIPRecords(context.Background(), &out, "phone", plan, sipTestEnv()); err != nil {
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
	if err := ensureLiveKitSIPRecords(context.Background(), &out, "phone", plan, sipTestEnv()); err != nil {
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
	if err := ensureLiveKitSIPRecords(context.Background(), &out, "phone", livekitSIPPlan(), sipTestEnv()); err != nil {
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
	if err := ensureLiveKitSIPRecords(context.Background(), &out, "phone", livekitSIPPlan(), sipTestEnv()); err != nil {
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
	pipecat := livekitSIPPlan()
	pipecat.Route = ir.TelephonyKey{Provider: ir.ProviderPipecat, Transport: "carrier-websocket", Carrier: "twilio"}

	for _, tc := range []struct {
		name string
		plan *generate.TelephonyRuntimePlan
		want bool
	}{
		{"livekit sip inbound", inboundOutbound, true},
		{"livekit sip outbound only", outboundOnly, false},
		{"livekit connector, inbound but no trunk", connector, false},
		{"pipecat carrier websocket, inbound but no trunk", pipecat, false},
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

// V7: --to on a SIP plan places the call by dispatching the agent on the local
// server (no /telephony/outbound POST exists for this route). Exactly one
// CreateDispatch fires, with agent_name = target, a fresh call- room, and
// outbound metadata carrying the number; the SIP auth never appears in output.
func TestPlaceLiveKitDispatchOutbound(t *testing.T) {
	fake, _ := newFakeSIPAdmin(t)
	var out strings.Builder
	if err := placeLiveKitDispatch(context.Background(), &out, "phone", "+15557778888", sipTestEnv()); err != nil {
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
	metadata, _ := got["metadata"].(string)
	for _, want := range []string{`"direction":"outbound"`, `"phone_number":"+15557778888"`, `"call_start":{}`} {
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

// The dispatch token is an HS256 JWT with the server-wide roomAdmin grant the
// AgentDispatchService requires; no room is scoped so it can create any room.
func TestMintLiveKitDispatchTokenCarriesRoomAdmin(t *testing.T) {
	token, err := mintLiveKitDispatchToken("devkey", "secret", time.Unix(1_800_000_000, 0))
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
			RoomAdmin bool `json:"roomAdmin"`
		} `json:"video"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Iss != "devkey" || !claims.Video.RoomAdmin {
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
	restoreLook := tunnelLookPath
	tunnelLookPath = func(string) (string, error) {
		t.Error("SIP routes must not start a tunnel")
		return "", os.ErrNotExist
	}
	t.Cleanup(func() { tunnelLookPath = restoreLook })

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
	if !strings.Contains(out.String(), "call +15550001111") {
		t.Fatalf("call line missing:\n%s", out.String())
	}
}

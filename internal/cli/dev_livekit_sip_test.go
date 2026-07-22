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
	t        *testing.T
	inbound  []map[string]any
	outbound []map[string]any
	rules    []map[string]any
	requests []string
	bodies   map[string]string
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
	method := strings.TrimPrefix(r.URL.Path, "/twirp/livekit.SIP/")
	body, _ := io.ReadAll(r.Body)
	f.requests = append(f.requests, method)
	f.bodies[method] = string(body)
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
	default:
		http.Error(w, "unknown method "+method, http.StatusNotFound)
	}
}

func livekitSIPPlan() *generate.TelephonyRuntimePlan {
	return &generate.TelephonyRuntimePlan{
		Route:       ir.TelephonyKey{Provider: ir.ProviderLiveKit, Transport: "sip", Carrier: "twilio"},
		RequiredEnv: []string{"LIVEKIT_SIP_INBOUND_TRUNK", "LIVEKIT_SIP_OUTBOUND_TRUNK", "TWILIO_SIP_ADDRESS", "TWILIO_SIP_USERNAME", "TWILIO_SIP_PASSWORD", "TWILIO_PHONE_NUMBER"},
		Environment: map[string]string{
			"sip_address": "TWILIO_SIP_ADDRESS", "sip_username": "TWILIO_SIP_USERNAME",
			"sip_password": "TWILIO_SIP_PASSWORD", "from_number": "TWILIO_PHONE_NUMBER",
		},
		DevSuppliedEnv: []string{"LIVEKIT_SIP_INBOUND_TRUNK", "LIVEKIT_SIP_OUTBOUND_TRUNK"},
		Evidence:       []ir.TelephonyFeatureEvidence{{Feature: "inbound", Tag: "core"}, {Feature: "outbound", Tag: "core"}},
		Services:       []string{"application", "livekit_server", "livekit_sip", "redis"},
		Coordination:   "shared",
	}
}

func sipTestEnv() []string {
	return []string{
		"TWILIO_SIP_ADDRESS=example.pstn.twilio.com", "TWILIO_SIP_USERNAME=user",
		"TWILIO_SIP_PASSWORD=sip-sekrit-88", "TWILIO_PHONE_NUMBER=+15550001111",
	}
}

// V4: first run lists (empty), creates all three records with auth only in
// the outbound request body; second run reuses every record and creates
// nothing.
func TestEnsureLiveKitSIPRecordsIsIdempotent(t *testing.T) {
	fake, _ := newFakeSIPAdmin(t)
	plan := livekitSIPPlan()
	var out strings.Builder

	injected, err := ensureLiveKitSIPRecords(context.Background(), &out, "phone", plan, sipTestEnv())
	if err != nil {
		t.Fatal(err)
	}
	if injected["LIVEKIT_SIP_INBOUND_TRUNK"] != "ST_in_1" || injected["LIVEKIT_SIP_OUTBOUND_TRUNK"] != "ST_out_1" {
		t.Fatalf("injected = %v", injected)
	}
	first := strings.Join(fake.requests, ",")
	for _, want := range []string{"ListSIPInboundTrunk,CreateSIPInboundTrunk", "ListSIPDispatchRule,CreateSIPDispatchRule", "ListSIPOutboundTrunk,CreateSIPOutboundTrunk"} {
		if !strings.Contains(first, want) {
			t.Errorf("first run requests missing list-before-create %q: %s", want, first)
		}
	}
	if !strings.Contains(fake.bodies["CreateSIPOutboundTrunk"], `"authPassword":"sip-sekrit-88"`) {
		t.Fatalf("outbound create did not carry auth in the body: %s", fake.bodies["CreateSIPOutboundTrunk"])
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
	injected, err = ensureLiveKitSIPRecords(context.Background(), &out, "phone", plan, sipTestEnv())
	if err != nil {
		t.Fatal(err)
	}
	if injected["LIVEKIT_SIP_INBOUND_TRUNK"] != "ST_in_1" || injected["LIVEKIT_SIP_OUTBOUND_TRUNK"] != "ST_out_1" {
		t.Fatalf("second run injected = %v", injected)
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
	}}
	var out strings.Builder
	injected, err := ensureLiveKitSIPRecords(context.Background(), &out, "phone", livekitSIPPlan(), sipTestEnv())
	if err != nil {
		t.Fatal(err)
	}
	if injected["LIVEKIT_SIP_INBOUND_TRUNK"] != "ST_in_9" || injected["LIVEKIT_SIP_OUTBOUND_TRUNK"] != "ST_out_9" {
		t.Fatalf("injected = %v", injected)
	}
	if strings.Contains(strings.Join(fake.requests, ","), "Create") {
		t.Fatalf("snake_case records were not reused: %v", fake.requests)
	}
}

// C1/C2: the SIP admin token is an HS256 JWT whose claims carry the sip
// admin grant and the api key as issuer.
func TestMintLiveKitSIPAdminTokenCarriesSIPAdminGrant(t *testing.T) {
	token, err := mintLiveKitSIPAdminToken("devkey", "devsecret-local-only", time.Unix(1_800_000_000, 0))
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

// V4 end to end through the post-gate core: infra services come up first,
// records are ensured against the local server, and the application phase
// receives both trunk IDs in its environment.
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
	if !strings.Contains(lines[0], "--wait livekit_server livekit_sip redis") || !strings.Contains(lines[0], "TRUNKS=/") {
		t.Fatalf("infra up = %q", lines[0])
	}
	if !strings.Contains(lines[1], "TRUNKS=ST_in_1/ST_out_1") {
		t.Fatalf("application up did not receive trunk IDs: %q", lines[1])
	}
	if !strings.Contains(out.String(), "call +15550001111") {
		t.Fatalf("call line missing:\n%s", out.String())
	}
}

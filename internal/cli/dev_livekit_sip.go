package cli

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/slng/unmute/internal/generate"
)

// Automatic LiveKit SIP trunk and dispatch records for local development
// (SPEC V4). The dev command creates them against the generated local stack
// only, with the non-production key pair the Compose template hardcodes.
// Production keeps the emitted sip-*.json + lk manual path.
//
// API shapes verified 2026-07-22 against docs.livekit.io
// /reference/telephony/sip-api/ and livekit/protocol
// protobufs/livekit_sip.proto (request wrappers) + auth/grants.go (the
// `"sip": {"admin": true}` claim):
//
//	POST <server>/twirp/livekit.SIP/<Method>, JSON body, Bearer JWT.
//	CreateSIPInboundTrunk  {"trunk": {...}} ; empty numbers = wildcard
//	CreateSIPOutboundTrunk {"trunk": {...}} ; auth in body only
//	CreateSIPDispatchRule  {"dispatch_rule": {...}} (non-deprecated field)
//	List*                  {} -> {"items": [...]}

// The generated local key pair, hardcoded by
// internal/generate/templates/livekit_v1/compose.telephony.yaml.tmpl and
// pinned by its golden. This is the documented `livekit-server --dev` pair
// (docs.livekit.io/transport/self-hosting/local): the dev server accepts only
// devkey/secret, so admin tokens and worker registration must sign with it
// (B2/V10). Never valid outside the generated local stack.
const (
	liveKitSIPComposeKey    = "devkey"
	liveKitSIPComposeSecret = "secret"
)

// liveKitSIPAdminBase is a seam: tests point it at an httptest server.
var liveKitSIPAdminBase = func(env []string) string {
	port := envValue(env, "UNMUTE_LIVEKIT_PORT")
	if port == "" {
		port = "7880"
	}
	return "http://127.0.0.1:" + port
}

// ensureLiveKitSIPRecords creates or reuses the local trunk and dispatch
// records and returns the env entries to inject. Idempotent by content:
// listing runs first and a content-identical record is reused, because the
// Redis volume persists across restarts (V4).
func ensureLiveKitSIPRecords(ctx context.Context, out io.Writer, targetName string, plan *generate.TelephonyRuntimePlan, env []string) (map[string]string, error) {
	base := liveKitSIPAdminBase(env)
	token, err := mintLiveKitSIPAdminToken(liveKitSIPComposeKey, liveKitSIPComposeSecret, time.Now())
	if err != nil {
		return nil, err
	}
	client := &sipAdminClient{base: base, token: token}
	number := envValue(env, plan.Environment["from_number"])
	needs := func(name string) bool { return slices.Contains(plan.DevSuppliedEnv, name) }
	injected := map[string]string{}

	if needs("LIVEKIT_SIP_INBOUND_TRUNK") {
		numbers := []string{}
		if number != "" {
			numbers = append(numbers, number)
		}
		trunkID, reused, err := client.ensureRecord(ctx, "ListSIPInboundTrunk", "CreateSIPInboundTrunk",
			map[string]any{"trunk": map[string]any{"name": "unmute " + targetName + " inbound", "numbers": numbers}},
			"trunk",
			func(item sipRecord) bool { return slices.Equal(item.strings("numbers"), numbers) },
			func(item sipRecord) string { return item.string("sipTrunkId", "sip_trunk_id") },
		)
		if err != nil {
			return nil, fmt.Errorf("ensure LiveKit inbound trunk: %w", err)
		}
		injected["LIVEKIT_SIP_INBOUND_TRUNK"] = trunkID
		fmt.Fprintf(out, "%s: LiveKit inbound trunk %s (%s)\n", targetName, trunkID, createdOrReused(reused))

		dispatchName := "unmute " + targetName + " inbound"
		action, err := client.ensureDispatchRule(ctx, dispatchName, targetName, trunkID)
		if err != nil {
			return nil, fmt.Errorf("ensure LiveKit dispatch rule: %w", err)
		}
		fmt.Fprintf(out, "%s: LiveKit dispatch rule %q (%s)\n", targetName, dispatchName, action)
	}

	if needs("LIVEKIT_SIP_OUTBOUND_TRUNK") {
		address := envValue(env, plan.Environment["sip_address"])
		trunkID, reused, err := client.ensureRecord(ctx, "ListSIPOutboundTrunk", "CreateSIPOutboundTrunk",
			map[string]any{"trunk": map[string]any{
				"name": "unmute " + targetName + " outbound", "address": address, "numbers": []string{number},
				// Auth goes into the request body only; never into emitted
				// files, compose files, or printed output (V6, C6).
				"authUsername": envValue(env, plan.Environment["sip_username"]),
				"authPassword": envValue(env, plan.Environment["sip_password"]),
			}},
			"trunk",
			// ponytail: list responses may redact auth fields, so identity is
			// address+numbers; a changed password on the same address reuses
			// the old record until the trunk is deleted by hand.
			func(item sipRecord) bool {
				return item.string("address") == address && slices.Equal(item.strings("numbers"), []string{number})
			},
			func(item sipRecord) string { return item.string("sipTrunkId", "sip_trunk_id") },
		)
		if err != nil {
			return nil, fmt.Errorf("ensure LiveKit outbound trunk: %w", err)
		}
		injected["LIVEKIT_SIP_OUTBOUND_TRUNK"] = trunkID
		fmt.Fprintf(out, "%s: LiveKit outbound trunk %s (%s)\n", targetName, trunkID, createdOrReused(reused))
	}
	return injected, nil
}

func createdOrReused(reused bool) string {
	if reused {
		return "reused"
	}
	return "created"
}

// mintLiveKitSIPAdminToken returns a short-lived HS256 JWT with the SIP
// admin grant (livekit/protocol auth/grants.go SIPGrant under the "sip"
// claim), reusing the hand-rolled JWT approach from mintLiveKitToken.
func mintLiveKitSIPAdminToken(apiKey, apiSecret string, now time.Time) (string, error) {
	claims := struct {
		Iss string `json:"iss"`
		Nbf int64  `json:"nbf"`
		Exp int64  `json:"exp"`
		SIP struct {
			Admin bool `json:"admin"`
		} `json:"sip"`
	}{Iss: apiKey, Nbf: now.Unix(), Exp: now.Add(10 * time.Minute).Unix()}
	claims.SIP.Admin = true
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := b64url([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." + b64url(payload)
	mac := hmac.New(sha256.New, []byte(apiSecret))
	mac.Write([]byte(signingInput))
	return signingInput + "." + b64url(mac.Sum(nil)), nil
}

type sipAdminClient struct {
	base  string
	token string
}

// ensureDispatchRule locates the dispatch rule by identity (bound trunk +
// individual `call-` prefix, V4) and reconciles its content: reuse when it
// already dispatches agentName, replace in place when it dispatches
// something else (creating a second `call-` rule on the same trunk would
// conflict, and reusing it would dispatch the wrong agent, B2), create when
// absent. Returns the action taken.
func (c *sipAdminClient) ensureDispatchRule(ctx context.Context, name, agentName, trunkID string) (string, error) {
	desired := map[string]any{
		"name":     name,
		"trunkIds": []string{trunkID},
		"rule":     map[string]any{"dispatchRuleIndividual": map[string]any{"roomPrefix": "call-"}},
		"roomConfig": map[string]any{"agents": []map[string]any{{
			"agentName": agentName, "metadata": `{"direction":"inbound"}`,
		}}},
	}
	var listing struct {
		Items []sipRecord `json:"items"`
	}
	if err := c.call(ctx, "ListSIPDispatchRule", map[string]any{}, &listing); err != nil {
		return "", err
	}
	for _, item := range listing.Items {
		individual := item.record("rule").record("dispatchRuleIndividual", "dispatch_rule_individual")
		if individual == nil || individual.string("roomPrefix", "room_prefix") != "call-" {
			continue
		}
		if !slices.Equal(item.strings("trunkIds", "trunk_ids"), []string{trunkID}) {
			continue
		}
		id := item.string("sipDispatchRuleId", "sip_dispatch_rule_id")
		if id == "" {
			continue
		}
		agents := item.record("roomConfig", "room_config").list("agents")
		if len(agents) == 1 && agents[0].string("agentName", "agent_name") == agentName {
			return "reused", nil
		}
		if err := c.call(ctx, "UpdateSIPDispatchRule", map[string]any{"sipDispatchRuleId": id, "replace": desired}, &sipRecord{}); err != nil {
			return "", err
		}
		return "updated", nil
	}
	var created sipRecord
	if err := c.call(ctx, "CreateSIPDispatchRule", map[string]any{"dispatch_rule": desired}, &created); err != nil {
		return "", err
	}
	if created.string("sipDispatchRuleId", "sip_dispatch_rule_id") == "" {
		return "", fmt.Errorf("CreateSIPDispatchRule returned no record ID")
	}
	return "created", nil
}

// ensureRecord lists existing records, reuses the first match, and creates
// otherwise. The create response is the record itself (Twirp returns the
// created info object).
func (c *sipAdminClient) ensureRecord(ctx context.Context, listMethod, createMethod string, createBody map[string]any, createField string, match func(sipRecord) bool, id func(sipRecord) string) (string, bool, error) {
	var listing struct {
		Items []sipRecord `json:"items"`
	}
	if err := c.call(ctx, listMethod, map[string]any{}, &listing); err != nil {
		return "", false, err
	}
	for _, item := range listing.Items {
		if match(item) {
			if existing := id(item); existing != "" {
				return existing, true, nil
			}
		}
	}
	var created sipRecord
	if err := c.call(ctx, createMethod, createBody, &created); err != nil {
		return "", false, err
	}
	createdID := id(created)
	if createdID == "" {
		return "", false, fmt.Errorf("%s returned no record ID (field %s)", createMethod, createField)
	}
	return createdID, false, nil
}

func (c *sipAdminClient) call(ctx context.Context, method string, payload, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/twirp/livekit.SIP/"+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")
	return doTelephonyJSON(request, result)
}

// sipRecord reads Twirp JSON tolerantly: protojson emits lowerCamelCase
// field names by default but proto names (snake_case) are also valid, so
// every accessor takes the acceptable key spellings.
type sipRecord map[string]any

func (r sipRecord) string(keys ...string) string {
	for _, key := range keys {
		if value, ok := r[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func (r sipRecord) strings(keys ...string) []string {
	for _, key := range keys {
		values, ok := r[key].([]any)
		if !ok {
			continue
		}
		out := make([]string, 0, len(values))
		for _, value := range values {
			if s, ok := value.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return []string{}
}

func (r sipRecord) record(keys ...string) sipRecord {
	for _, key := range keys {
		if value, ok := r[key].(map[string]any); ok {
			return sipRecord(value)
		}
	}
	return nil
}

func (r sipRecord) list(keys ...string) []sipRecord {
	for _, key := range keys {
		values, ok := r[key].([]any)
		if !ok {
			continue
		}
		out := make([]sipRecord, 0, len(values))
		for _, value := range values {
			if record, ok := value.(map[string]any); ok {
				out = append(out, sipRecord(record))
			}
		}
		return out
	}
	return nil
}

// placeLiveKitDispatch places an outbound LiveKit SIP call by dispatching the
// agent into a fresh room with outbound metadata (SPEC I.sipdial). The worker
// then dials `to` through the stored outbound trunk. API shape verified
// 2026-07-23 against docs.livekit.io /reference/agents/agent-dispatch-service-api:
//
//	POST <server>/twirp/livekit.AgentDispatchService/CreateDispatch
//	{"agent_name","room","metadata"} ; Bearer JWT with roomAdmin ; room auto-created.
func placeLiveKitDispatch(ctx context.Context, out io.Writer, targetName, to string, env []string) error {
	room, err := randomRoomName("call")
	if err != nil {
		return err
	}
	token, err := mintLiveKitDispatchToken(liveKitSIPComposeKey, liveKitSIPComposeSecret, time.Now())
	if err != nil {
		return err
	}
	client := &sipAdminClient{base: liveKitSIPAdminBase(env), token: token}
	// The worker reads phone_number, direction, and call_start from job metadata
	// (agent.py connector/SIP branch). call_start stays empty here; drive it from
	// your own application when the agent declares required call_start variables.
	metadata, err := json.Marshal(map[string]any{
		"direction": "outbound", "phone_number": to, "call_start": map[string]any{},
	})
	if err != nil {
		return err
	}
	var created sipRecord
	if err := client.callDispatch(ctx, "CreateDispatch", map[string]any{
		"agent_name": targetName, "room": room, "metadata": string(metadata),
	}, &created); err != nil {
		return fmt.Errorf("place outbound call to %s: %w", to, err)
	}
	id := created.string("id")
	fmt.Fprintf(out, "\n  \033[1;32m▸\033[0m calling %s  (room %s, dispatch %s)  ·  ctrl-c to stop\n\n", to, room, id)
	return nil
}

// mintLiveKitDispatchToken returns a short-lived HS256 JWT with server-wide
// roomAdmin (the AgentDispatchService grant), reusing the hand-rolled JWT
// approach from mintLiveKitToken. No room is scoped so it can create any room.
func mintLiveKitDispatchToken(apiKey, apiSecret string, now time.Time) (string, error) {
	claims := struct {
		Iss   string `json:"iss"`
		Nbf   int64  `json:"nbf"`
		Exp   int64  `json:"exp"`
		Video struct {
			RoomAdmin bool `json:"roomAdmin"`
		} `json:"video"`
	}{Iss: apiKey, Nbf: now.Unix(), Exp: now.Add(10 * time.Minute).Unix()}
	claims.Video.RoomAdmin = true
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := b64url([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." + b64url(payload)
	mac := hmac.New(sha256.New, []byte(apiSecret))
	mac.Write([]byte(signingInput))
	return signingInput + "." + b64url(mac.Sum(nil)), nil
}

// callDispatch posts to the AgentDispatchService Twirp endpoint. Separate from
// call() because that one is hardwired to the livekit.SIP service path.
func (c *sipAdminClient) callDispatch(ctx context.Context, method string, payload, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/twirp/livekit.AgentDispatchService/"+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")
	return doTelephonyJSON(request, result)
}

// telephonyInfraServices is the Compose graph minus the application: the
// services that must be healthy before trunk records can be created.
func telephonyInfraServices(plan *generate.TelephonyRuntimePlan) []string {
	services := make([]string, 0, len(plan.Services))
	for _, service := range plan.Services {
		if service != "application" {
			services = append(services, service)
		}
	}
	return services
}

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

// Automatic LiveKit SIP trunk and dispatch records for local development
// (compiler.md V36). The dev command creates them against the generated local stack
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
	port := envValue(env, "LIVEKIT_HOST_PORT")
	if port == "" {
		port = "7880"
	}
	return "http://127.0.0.1:" + port
}

// ensureLiveKitSIPRecords creates or reuses the local trunk and dispatch
// records. Idempotent by content: listing runs first and a content-identical
// record is reused, because the Redis volume persists across restarts (V4).
//
// Nothing is injected into the child environment. The records are platform state
// the local LiveKit SIP service reads for itself, and no emitted Python looks up
// their IDs.
func ensureLiveKitSIPRecords(ctx context.Context, out io.Writer, targetName string, plan *generate.TelephonyRuntimePlan, env []string, credential dialCredential) error {
	base := liveKitSIPAdminBase(env)
	token, err := mintLiveKitSIPAdminToken(liveKitSIPComposeKey, liveKitSIPComposeSecret, time.Now())
	if err != nil {
		return err
	}
	client := &sipAdminClient{base: base, token: token}
	number := envValue(env, plan.Environment["from_number"])
	numbers := []string{}
	if number != "" {
		numbers = append(numbers, number)
	}
	// The trunk that accepts the call carries this run's own credential (gate
	// S3). Without it the plane answers anybody who can reach the port, and on
	// the softphone profile that port is published on a real interface. The
	// credential is minted per run, printed for the developer to type, and
	// written to no file.
	desired := map[string]any{"name": "unmute " + targetName + " inbound", "numbers": numbers}
	if credential.Username != "" {
		desired["auth_username"] = credential.Username
		desired["auth_password"] = credential.Password
	}
	trunkID, reused, err := client.ensureRecord(ctx, "ListSIPInboundTrunk", "CreateSIPInboundTrunk",
		map[string]any{"trunk": desired},
		"trunk",
		func(item sipRecord) bool { return slices.Equal(item.strings("numbers"), numbers) },
		func(item sipRecord) string { return item.string("sipTrunkId", "sip_trunk_id") },
	)
	if err != nil {
		return fmt.Errorf("ensure LiveKit inbound trunk: %w", err)
	}
	// A trunk is reused by the numbers it claims, and the Redis volume outlives
	// a run, so a reused trunk still holds the previous run's credential. It has
	// to be replaced or the printed credential is one the plane will refuse,
	// which is the worst kind of wrong: a correct-looking instruction that
	// cannot work.
	if reused && credential.Username != "" {
		if err := client.call(ctx, "UpdateSIPInboundTrunk", map[string]any{
			"sip_trunk_id": trunkID, "replace": desired,
		}, &sipRecord{}); err != nil {
			return fmt.Errorf("give the reused LiveKit inbound trunk this run's credential: %w", err)
		}
	}
	verb := "created"
	if reused {
		verb = "reused"
	}
	fmt.Fprintf(out, "%s: LiveKit inbound trunk %s (%s)\n", targetName, trunkID, verb)

	dispatchName := "unmute " + targetName + " inbound"
	// The agent name, or empty on a route whose agent is not a LiveKit worker.
	// See ensureDispatchRule for what that omission is worth.
	dispatchAgent := ""
	if planAgentIsLiveKitWorker(plan) {
		dispatchAgent = targetName
	}
	action, err := client.ensureDispatchRule(ctx, dispatchName, dispatchAgent, trunkID)
	if err != nil {
		return fmt.Errorf("ensure LiveKit dispatch rule: %w", err)
	}
	fmt.Fprintf(out, "%s: LiveKit dispatch rule %q (%s)\n", targetName, dispatchName, action)

	// No outbound trunk is registered here, on the plane or off it, and gate S7
	// is satisfied all the same. The generated agent dials with its trunk
	// settings inline, so a registered record would be state nothing reads: the
	// thing that has to point at the plane's endpoints is the *environment* the
	// agent reads them from, which planeEnvironment sets. Local development
	// therefore uses the same dial path a deployment does, and a transfer that
	// works in one cannot fail in the other for want of a record
	// (SCHEMA N33, 2026-08-12; local case added 2026-08-20).
	return nil
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
	return signJWT(apiSecret, claims)
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
// sipDispatchRoomPrefix is the room name prefix the individual dispatch rule
// gives every inbound call. It is load-bearing beyond naming: a room with this
// prefix exists only because the trunk accepted the call and this rule matched
// it, which is why the rig reads arrival from it.
const sipDispatchRoomPrefix = target.SIPCallRoomPrefix

func (c *sipAdminClient) ensureDispatchRule(ctx context.Context, name, agentName, trunkID string) (string, error) {
	desired := map[string]any{
		"name":     name,
		"trunkIds": []string{trunkID},
		"rule":     map[string]any{"dispatchRuleIndividual": map[string]any{"roomPrefix": sipDispatchRoomPrefix}},
	}
	// roomConfig only on a route whose agent is a LiveKit worker, because that is
	// the only kind of agent it can dispatch. Naming one on a route with no worker
	// is a record that describes something that never happens: the server attempts
	// a dispatch on every inbound call and no worker is ever there to take it.
	//
	// It is deliberately *not* what decides whether a call is answered, and an
	// earlier reading of this that said so was wrong. livekit/sip
	// pkg/sip/inbound.go joins the room at status ringing, publishes the caller's
	// track, and then waits in waitSubscribe until it can subscribe to another
	// participant's audio track; only then does it send 200 OK. Nothing joining is
	// what holds a call at 180 Ringing, and after defaultRingingTimeout (three
	// minutes, pkg/sip/participant.go) it terminates with "cannot-subscribe".
	// Measured 2026-08-21 with no roomConfig and no agent: rang for exactly three
	// minutes and was cut off. What answers a call on this route is the agent
	// joining the room, which the room webhook is what triggers.
	if agentName != "" {
		desired["roomConfig"] = map[string]any{"agents": []map[string]any{{
			"agentName": agentName, "metadata": `{"direction":"inbound"}`,
		}}}
	}
	var listing struct {
		Items []sipRecord `json:"items"`
	}
	if err := c.call(ctx, "ListSIPDispatchRule", map[string]any{}, &listing); err != nil {
		return "", err
	}
	for _, item := range listing.Items {
		individual := item.record("rule").record("dispatchRuleIndividual", "dispatch_rule_individual")
		if individual == nil || individual.string("roomPrefix", "room_prefix") != sipDispatchRoomPrefix {
			continue
		}
		if !slices.Equal(item.strings("trunkIds", "trunk_ids"), []string{trunkID}) {
			continue
		}
		id := item.string("sipDispatchRuleId", "sip_dispatch_rule_id")
		if id == "" {
			continue
		}
		if ruleDispatchesAgent(item, agentName) {
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

// ruleDispatchesAgent reports whether an existing rule already dispatches what
// this route wants: the one worker it names, or nothing at all on a route whose
// agent is not a worker. The second case is not a detail — a rule left over from
// a LiveKit run on the same Redis volume still names that worker, and reusing it
// would hold every call at ringing.
func ruleDispatchesAgent(item sipRecord, agentName string) bool {
	agents := item.record("roomConfig", "room_config").list("agents")
	if agentName == "" {
		return len(agents) == 0
	}
	return len(agents) == 1 && agents[0].string("agentName", "agent_name") == agentName
}

// planAgentIsLiveKitWorker reports whether this route's agent registers with the
// LiveKit server as an agent worker, which is the only kind of agent a dispatch
// rule can dispatch. The LiveKit driver emits one; the Pipecat driver emits a bot
// that speaks no worker protocol, so on `(pipecat, sip)` this is false even
// though the plane underneath is the same.
func planAgentIsLiveKitWorker(plan *generate.TelephonyRuntimePlan) bool {
	return plan != nil && plan.Route.Provider == ir.ProviderLiveKit
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

// call posts to the SIP service, the only one most of this file talks to.
func (c *sipAdminClient) call(ctx context.Context, method string, payload, result any) error {
	return c.callService(ctx, "livekit.SIP", method, payload, result)
}

// callService is one Twirp request: JSON body, bearer token, JSON reply. The
// service name is the only thing that ever varies between the two endpoints
// this package uses.
func (c *sipAdminClient) callService(ctx context.Context, service, method string, payload, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/twirp/"+service+"/"+method, bytes.NewReader(body))
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
//	{"agent_name","room","metadata"} ; Bearer JWT with room-scoped roomAdmin ; room auto-created.
func placeLiveKitDispatch(ctx context.Context, out io.Writer, targetName, to string, env []string) error {
	room, err := randomRoomName("call")
	if err != nil {
		return err
	}
	token, err := mintLiveKitDispatchToken(liveKitSIPComposeKey, liveKitSIPComposeSecret, room, time.Now())
	if err != nil {
		return err
	}
	client := &sipAdminClient{base: liveKitSIPAdminBase(env), token: token}
	// The worker reads phone_number, direction, and call_start from job metadata
	// (agent.py connector/SIP branch).
	callStart, err := callStartFromEnv(env)
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(map[string]any{
		"direction": "outbound", "phone_number": to, "call_start": callStart,
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

// mintLiveKitDispatchToken returns a short-lived HS256 JWT with roomAdmin
// scoped to the AgentDispatchService request room, matching LiveKit's SDK.
func mintLiveKitDispatchToken(apiKey, apiSecret, room string, now time.Time) (string, error) {
	claims := struct {
		Iss   string `json:"iss"`
		Nbf   int64  `json:"nbf"`
		Exp   int64  `json:"exp"`
		Video struct {
			RoomAdmin bool   `json:"roomAdmin"`
			Room      string `json:"room"`
		} `json:"video"`
	}{Iss: apiKey, Nbf: now.Unix(), Exp: now.Add(10 * time.Minute).Unix()}
	claims.Video.RoomAdmin = true
	claims.Video.Room = room
	return signJWT(apiSecret, claims)
}

// callDispatch posts to the AgentDispatchService, which is the same request
// against a different service name.
func (c *sipAdminClient) callDispatch(ctx context.Context, method string, payload, result any) error {
	return c.callService(ctx, "livekit.AgentDispatchService", method, payload, result)
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

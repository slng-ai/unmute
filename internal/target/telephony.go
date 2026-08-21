package target

import (
	"fmt"
	"slices"
	"strings"
)

type TelephonyKey struct {
	Provider  Provider `json:"provider"`
	Transport string   `json:"transport"`
	Carrier   string   `json:"carrier"`
}

type TelephonyFeature string

const (
	TelephonyRouteSelected TelephonyFeature = "route"
	TelephonyInbound       TelephonyFeature = "inbound"
	TelephonyOutbound      TelephonyFeature = "outbound"
	// SCHEMA N25 removed the briefing mode enum (it was Vapi's transferPlan
	// vocabulary and mapped to no code target), so there are no briefing.*
	// features: a free-text briefing rides the warm_transfer control row.
	TelephonySourcePrefix = "source."
)

type TelephonyEvidence struct {
	Feature  TelephonyFeature `json:"feature"`
	Tag      Tag              `json:"tag"`
	Note     string           `json:"note,omitempty"`
	Docs     string           `json:"docs,omitempty"`
	Verified string           `json:"verified,omitempty"`
	Smoke    bool             `json:"smoke"`
}

// LocalPlaneEnvName holds the carrier stand-in's own base address on a run of
// `unmute dev --telephony`, and is unset in production. The emitted agent reads
// it twice, and both readings are the same question: where is the carrier?
//
// Set at all, it means build the carrier transport directly rather than through
// the framework, because the framework's path cannot be built without carrier
// credentials and writes to the carrier's REST API when a call ends (gate P2,
// research R7 addendum). Its value is where call control goes, so a cold
// transfer replaces the call's document on this machine instead of posting to
// api.twilio.com, which is the same gate on the other exit path.
//
// One name carrying an address rather than one name carrying "1" beside a
// second one carrying the address: there is only ever one answer to give.
//
// It lives here rather than in internal/generate or internal/cli because both
// of those need the same string: one renders it into the agent, the other sets
// it. A second copy is the copy that goes stale.
//
// It is deliberately absent from every route's environment rules, so it reaches
// no .env.example and no deployment manifest (gate C4).
const LocalPlaneEnvName = "UNMUTE_LOCAL_PLANE"

// LocalPlaneNumber is the number the local plane answers on. It is in the range
// reserved for fiction on purpose: it belongs to nobody, it is never dialled off
// the machine, and a reader who sees it in a log can tell at a glance that no
// real number was involved.
//
// Here rather than in internal/cli, because the emitted Compose file needs the
// same string: the caller endpoint dials it, and the command prints it. Two
// copies would be two things to keep in step.
const LocalPlaneNumber = "+15550000000"

// LocalPlaneNoAnswerNumber is a destination the local plane rings and never
// answers. Transfer one here and the run takes the route's unavailable path
// instead of its happy one, which is otherwise unreachable without a real
// destination that really does not pick up.
//
// It exists because "what my agent does when nobody answers" is the branch
// people find out about in production. FR-013 says a run reports that outcome;
// this is what makes there be one to report. Same reserved range and the same
// reasoning as LocalPlaneNumber, and one digit apart from it so a log line
// reads unambiguously either way.
const LocalPlaneNoAnswerNumber = "+15550000009"

// SIPCallRoomPrefix is the room-name prefix the inbound dispatch rule gives
// every call that arrives on a LiveKit SIP trunk. It is load-bearing beyond
// naming: a room with this prefix exists only because a trunk accepted a call and
// the rule matched it, so it is how an agent that was not dispatched into the
// room tells a call apart from any other room on the same server.
//
// Here rather than in internal/generate or internal/cli because all three need
// it: the emitted provisioning JSON writes it, the dev command's own records
// write it, and the emitted webhook reads it. Three copies would be three things
// to keep in step.
const SIPCallRoomPrefix = "call-"

// SIPRoomWebhookPath is where a LiveKit Server announces its rooms to an agent
// that a dispatch rule cannot dispatch. A LiveKit worker is put in the room for
// it and needs no announcement; a Pipecat bot on the same plane speaks no worker
// protocol, and an inbound call is answered only once something joins the room
// and publishes audio, so without this the call rings until it times out.
//
// Here for the same reason as the prefix: the emitted Compose file points a
// server at it and the emitted application serves it.
const SIPRoomWebhookPath = "/telephony/livekit"

// TelephonyLocalPlane names how a route is exercised on a developer's machine
// with no carrier involved. It is a route fact: the plane must present the same
// call mechanism the route's carrier uses in production, so a route never gets
// to choose a more convenient one.
type TelephonyLocalPlane string

const (
	// LocalPlaneNone: no carrier-free loop exists for this route, so the route
	// keeps the refusal it prints today.
	//
	// This is deliberately not the empty string. A zero value that means "the
	// author decided none" is indistinguishable from "the author forgot", and
	// the totality test below has to fail on the second one. So the zero value
	// is invalid and every row states its plane.
	LocalPlaneNone TelephonyLocalPlane = "none"
	// LocalPlaneSIP: a real SIP stack on the machine. The caller is either the
	// developer's softphone or a headless endpoint; the transfer destination is
	// always a headless endpoint, because the plane dials it (research R6).
	LocalPlaneSIP TelephonyLocalPlane = "sip"
	// LocalPlaneMediaWebsocket: the CLI speaks the carrier's media-streaming
	// WebSocket protocol to the agent over loopback (research R7).
	LocalPlaneMediaWebsocket TelephonyLocalPlane = "media-websocket"
)

type TelephonyRoute struct {
	Key                        TelephonyKey
	Features                   map[TelephonyFeature]TelephonyEvidence
	RequiredEnvironment        []string
	OptionalEnvironment        []string
	Processes                  []TelephonyProcess
	PublicEndpoints            []TelephonyEndpointRule
	RuntimeEnvironment         []TelephonyEnvironmentRule
	LocallySuppliedEnvironment []string
	ManualSteps                []string
	// AutoWebhookEndpoint names the public endpoint the dev command sets as
	// the carrier's voice webhook automatically on every start. Empty means
	// the carrier keeps printed manual steps. This is a carrier fact, not a
	// framework: only carriers with a CLI implementation may carry it.
	AutoWebhookEndpoint string
	// LocalPlane is how `unmute dev --telephony` exercises this route with no
	// carrier. LocalPlaneNone means there is no carrier-free loop and the
	// command keeps refusing, which is the honest answer for a route whose
	// carrier leg terminates in a third-party hosted service we do not run.
	LocalPlane TelephonyLocalPlane
	// CloudDeploys reports whether this route has a managed-platform deployment
	// path. It is a route fact and it does not follow from the provider.
	//
	// LiveKit routes are all true: the same route deploys to LiveKit Cloud or to
	// a LiveKit Server the author runs, chosen at deploy time by where
	// LIVEKIT_URL points, and the emitted runbook passes deployment_region to
	// `lk agent create` either way. Pipecat routes split: cloud-websocket and
	// daily-sip are true, carrier-websocket is false.
	//
	// It does NOT say which manifest, and a gate must not read it as though it
	// did. pcc-deploy.toml is a Pipecat artifact: no LiveKit route emits one,
	// true though CloudDeploys is on every LiveKit row.
	CloudDeploys bool
}

type TelephonyProcess struct {
	Name      string
	Command   []string
	Health    string
	Readiness string
}

type TelephonyEndpointRule struct {
	Name        string
	Method      string
	Path        string
	AnyFeatures []TelephonyFeature
}

type TelephonyEnvironmentRule struct {
	Name        string
	AnyFeatures []TelephonyFeature
}

func TelephonyRoutes() map[TelephonyKey]TelephonyRoute {
	routes := make(map[TelephonyKey]TelephonyRoute)
	add := func(provider Provider, transport, carrier, docs string, features ...TelephonyFeature) {
		key := TelephonyKey{Provider: provider, Transport: transport, Carrier: carrier}
		route := TelephonyRoute{Key: key, Features: make(map[TelephonyFeature]TelephonyEvidence, len(features))}
		for _, feature := range features {
			route.Features[feature] = TelephonyEvidence{
				Feature: feature, Tag: Provisional,
				Note: "route has not passed its credentialed smoke",
				Docs: docs, Verified: "2026-07-20",
			}
		}
		routes[key] = route
	}
	sourcesWithStream := []TelephonyFeature{
		"source.session_id", "source.carrier", "source.connection", "source.call_id",
		"source.stream_id", "source.direction", "source.from_number", "source.to_number",
	}
	sourcesWithoutStream := []TelephonyFeature{
		"source.session_id", "source.carrier", "source.connection", "source.call_id",
		"source.direction", "source.from_number", "source.to_number",
	}
	pipecat := "https://docs.pipecat.ai/pipecat/learn/transports"
	// No transfers on the carrier-websocket routes: Pipecat's own docs say the
	// websocket transports have no call-transfer control, and a transfer needs
	// a platform primitive (SPEC C1, V1). Cold on Pipecat is the Daily route.
	for _, carrier := range []string{"twilio", "telnyx", "plivo"} {
		features := append([]TelephonyFeature{TelephonyRouteSelected, TelephonyInbound, TelephonyOutbound, TelephonyFeature(Hangup)}, sourcesWithStream...)
		add(Pipecat, "carrier-websocket", carrier, pipecat, features...)
		key := TelephonyKey{Provider: Pipecat, Transport: "carrier-websocket", Carrier: carrier}
		route := routes[key]
		route.LocalPlane = LocalPlaneMediaWebsocket
		// The one Pipecat transport with no managed-platform path: it emits a
		// plain container plus a Compose file and no deployment manifest.
		route.CloudDeploys = false
		// What the credential-free check covers on this route, and nothing more
		// (FR-020). The tag stays provisional: it tracks a *credentialed* check in
		// CI, and `make rig` is deliberately credential-free, so a green rig can
		// never promote a capability (gate C7).
		if carrier == "twilio" {
			for feature, evidence := range route.Features {
				evidence.Note = "make rig, 2026-08-20: inbound call, two-way audio and a cold " +
					"transfer carried out against the real emitted agent on the local plane, with no " +
					"accounts. Nothing about carrier reachability, and no live carrier call"
				route.Features[feature] = evidence
			}
		}
		routes[key] = route
	}
	pipecatProcess := []TelephonyProcess{{
		Name: "agent", Command: []string{"uv", "run", "uvicorn", "telephony:app", "--host", "0.0.0.0", "--port", "7860"},
		Health: "/healthz", Readiness: "/readyz",
	}}
	pipecatEndpoints := []TelephonyEndpointRule{
		{Name: "inbound", Method: "POST", Path: "/telephony/inbound", AnyFeatures: []TelephonyFeature{TelephonyInbound}},
		{Name: "media", Method: "WS", Path: "/telephony/ws/{token}"},
		{Name: "outbound", Method: "POST", Path: "/telephony/outbound", AnyFeatures: []TelephonyFeature{TelephonyOutbound}},
		{Name: "status", Method: "POST", Path: "/telephony/status"},
	}
	pipecatEnvironment := []TelephonyEnvironmentRule{
		{Name: "REDIS_URL"},
		{Name: "UNMUTE_OUTBOUND_TOKEN", AnyFeatures: []TelephonyFeature{TelephonyOutbound}},
		{Name: "UNMUTE_PUBLIC_URL"},
	}
	setPipecatRuntime := func(key TelephonyKey, steps []string, extra ...TelephonyEndpointRule) {
		route := routes[key]
		route.Processes = slices.Clone(pipecatProcess)
		route.PublicEndpoints = append(slices.Clone(pipecatEndpoints), extra...)
		route.RuntimeEnvironment = slices.Clone(pipecatEnvironment)
		// Every name in pipecatEnvironment is supplied rather than authored:
		// `unmute dev` mints the public URL and the outbound token at
		// internal/cli/dev_telephony.go and starts the Redis the Compose graph
		// declares, and a deployment's platform or operator supplies all three.
		// The two UNMUTE_* names were missing from this list even though the dev
		// command already minted them, which is why they rendered as blanks in
		// .env.example for the author to fill in (FR-018c).
		route.LocallySuppliedEnvironment = []string{"REDIS_URL", "UNMUTE_OUTBOUND_TOKEN", "UNMUTE_PUBLIC_URL"}
		route.ManualSteps = steps
		routes[key] = route
	}
	twilio := TelephonyKey{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "twilio"}
	route := routes[twilio]
	route.RequiredEnvironment = []string{"account_sid", "auth_token", "from_number"}
	routes[twilio] = route
	setPipecatRuntime(twilio, []string{
		"get the Account SID and Auth Token from the Twilio Console account dashboard and select a Voice-capable number",
		"for production, configure the Twilio number voice webhook as POST to the reported inbound endpoint (unmute dev --telephony sets it automatically and prints the previous value)",
		"configure Twilio call status callbacks as POST to the reported status endpoint",
	})
	route = routes[twilio]
	route.AutoWebhookEndpoint = "inbound"
	routes[twilio] = route
	telnyx := TelephonyKey{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "telnyx"}
	route = routes[telnyx]
	route.RequiredEnvironment = []string{"api_key", "public_key", "connection_id", "from_number"}
	routes[telnyx] = route
	setPipecatRuntime(telnyx, []string{
		"get an API key and public key from Telnyx Mission Control, then select a Voice API Application and phone number",
		"set the Voice API Application webhook URL to the reported inbound endpoint and use API version 2",
		"assign the selected phone number to that Voice API Application; generated outbound calls report status to the reported status endpoint",
	})
	plivo := TelephonyKey{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "plivo"}
	route = routes[plivo]
	route.RequiredEnvironment = []string{"auth_id", "auth_token", "from_number"}
	routes[plivo] = route
	setPipecatRuntime(plivo, []string{
		"get the Auth ID and Auth Token from the Plivo Console dashboard and select a Voice-capable number",
		"create a Voice XML Application whose Answer URL is POST to the reported inbound endpoint",
		"assign the selected phone number to that XML Application and configure its Hangup URL as POST to the reported status endpoint",
	},
		TelephonyEndpointRule{Name: "outbound-answer", Method: "POST", Path: "/telephony/answer/{token}", AnyFeatures: []TelephonyFeature{TelephonyOutbound}},
	)
	exotel := TelephonyKey{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "exotel"}
	routes[exotel] = TelephonyRoute{Key: exotel, Features: map[TelephonyFeature]TelephonyEvidence{}, RequiredEnvironment: []string{
		"api_key", "api_token", "account_sid", "subdomain", "from_number", "app_id",
	},
		// One of the two rows with an empty feature map, which
		// ResolveTelephonyFeature refuses, so it compiles to nothing and has no
		// local loop to offer. Assigned anyway: the field is set on every row so
		// the zero value never claims a plane for a route that refuses.
		LocalPlane: LocalPlaneNone, CloudDeploys: false,
	}
	sipRoutes := []struct{ carrier, docs string }{
		{"twilio", "https://docs.livekit.io/telephony/start/providers/twilio/"},
		{"telnyx", "https://docs.livekit.io/telephony/start/providers/telnyx/"},
		{"plivo", "https://docs.livekit.io/telephony/start/providers/plivo/"},
	}
	for _, selected := range sipRoutes {
		features := append([]TelephonyFeature{
			TelephonyRouteSelected, TelephonyInbound, TelephonyOutbound, TelephonyFeature(ColdTransfer),
			TelephonyFeature(WarmTransfer), TelephonyFeature(Hangup),
			TelephonyFeature(VoicemailDetection),
		}, sourcesWithoutStream...)
		add(LiveKit, "sip", selected.carrier, selected.docs, features...)
		key := TelephonyKey{Provider: LiveKit, Transport: "sip", Carrier: selected.carrier}
		route := routes[key]
		route.LocalPlane = LocalPlaneSIP
		// True on every LiveKit row: LIVEKIT_URL decides at deploy time whether
		// this is LiveKit Cloud or a server the author runs, and the runbook
		// passes deployment_region either way. It does not mean this route emits
		// a Pipecat deployment manifest, and no LiveKit route emits one.
		route.CloudDeploys = true
		route.RequiredEnvironment = []string{"sip_address", "sip_username", "sip_password", "from_number"}
		route.Processes = []TelephonyProcess{{
			Name: "agent", Command: []string{"uv", "run", "python", "-m", "livekit.agents", "start", "agent.py"}, Health: "/", Readiness: "/",
		}}
		// No trunk ID here. Dialling out needs no stored trunk: the emitted agent
		// passes the carrier's trunk settings inline with every call, from the
		// four names the Connection declares (SCHEMA N33, 2026-08-12). Inbound
		// does need its two platform records, because an unsolicited call arrives
		// with no request of ours for configuration to travel with, but the
		// emitted telephony-setup.sh resolves them by phone number at
		// provisioning time, so no environment name carries the ID (SCHEMA N36,
		// 2026-08-12).
		route.RuntimeEnvironment = []TelephonyEnvironmentRule{
			{Name: "LIVEKIT_API_KEY"},
			{Name: "LIVEKIT_API_SECRET"},
			{Name: "LIVEKIT_URL"},
			{Name: "REDIS_URL"},
		}
		route.LocallySuppliedEnvironment = []string{"LIVEKIT_API_KEY", "LIVEKIT_API_SECRET", "LIVEKIT_URL", "REDIS_URL"}
		route.ManualSteps = []string{
			"get LIVEKIT_URL and the API key pair from the LiveKit Cloud project settings, or from a self-hosted LiveKit Server configuration; a self-hosted deployment configures LiveKit Server and LiveKit SIP with the same Redis deployment",
			"point the carrier's origination URI at the LiveKit project SIP URI with transport=tcp (the LiveKit Cloud project settings page, or lk project list --json with the p_ prefix dropped from ProjectId); for a self-hosted deployment, deploy LiveKit SIP with public SIP signaling and RTP ports and point origination at that public SIP endpoint instead",
			"get the selected carrier SIP address, username, password, and phone number from its SIP trunking console; these four reach the deployed agent's dial-out path directly, so no outbound trunk is registered",
			"for inbound calls only, run bash telephony-setup.sh from the build directory: it resolves the inbound trunk by phone number and creates the trunk and dispatch rule, so no record ID is ever copied by hand (unmute dev --telephony creates the local records itself)",
		}
		routes[key] = route
	}
	// Pipecat over the same SIP plane: a self-hosted phone route with no managed
	// platform anywhere in it (US3).
	//
	// The transport is Pipecat's own LiveKit transport, so the agent joins a room
	// on a LiveKit Server the operator runs and LiveKit SIP terminates the
	// carrier's trunk in front of it. Verified against pipecat-ai 1.7.0 on
	// 2026-08-20: LiveKitTransport(url, token, room_name, params=LiveKitParams()),
	// installed by the package's own `livekit` extra. No new protocol work.
	//
	// **No warm transfer**, deliberately, and it is not the platform's fault: a
	// warm handoff needs the agent to hold both legs and merge them, and this
	// project has not built that on Pipecat. The refusal says so and names where
	// warm does compile (research D1).
	for _, selected := range sipRoutes {
		// **No `source.*` bindings.** The call-source table is filled by the
		// carrier-websocket adapter, which this route does not emit: here the call
		// arrives as a SIP participant in a room and its from/to live in the
		// participant's own attributes, which nothing reads yet. Granting them
		// would be granting a feature with no code path, which the emitter
		// agreement exists to catch.
		// **No outbound.** Not a gap being tolerated: nothing in this driver emits
		// a dial-out path at all. The LiveKit driver's agent calls
		// create_sip_participant; no Pipecat template calls it on any route, and
		// the transport here only ever *joins* a room somebody else's call
		// created. The feature was granted before that was true and a route that
		// claims a capability with no code behind it is the thing the emitter
		// agreement exists to catch, so it is claimed no longer (2026-08-21).
		//
		// A package declaring channels.phone outbound: true on this route is
		// therefore refused, and the refusal names the routes that do dial out.
		add(Pipecat, "sip", selected.carrier, "https://docs.pipecat.ai/pipecat/learn/transports",
			TelephonyRouteSelected, TelephonyInbound,
			TelephonyFeature(ColdTransfer), TelephonyFeature(Hangup))
		key := TelephonyKey{Provider: Pipecat, Transport: "sip", Carrier: selected.carrier}
		route := routes[key]
		route.LocalPlane = LocalPlaneSIP
		// No managed-platform path at all, which is the point of the route: it
		// emits a plain container and a Compose file and no deployment manifest.
		// FR-024's refusal reads this field, so it is what makes a
		// managed-platform-only setting an error here and fine on (livekit, sip).
		route.CloudDeploys = false
		route.RequiredEnvironment = []string{"sip_address", "sip_username", "sip_password", "from_number"}
		route.Processes = []TelephonyProcess{{
			Name: "agent", Command: []string{"uv", "run", "uvicorn", "telephony:app", "--host", "0.0.0.0", "--port", "7860"},
			Health: "/healthz", Readiness: "/readyz",
		}}
		// The same platform values the LiveKit rows need, for the same reason: the
		// agent joins a room on a server, and locally the plane supplies all four.
		route.RuntimeEnvironment = []TelephonyEnvironmentRule{
			{Name: "LIVEKIT_API_KEY"},
			{Name: "LIVEKIT_API_SECRET"},
			{Name: "LIVEKIT_URL"},
			{Name: "REDIS_URL"},
		}
		route.LocallySuppliedEnvironment = []string{"LIVEKIT_API_KEY", "LIVEKIT_API_SECRET", "LIVEKIT_URL", "REDIS_URL"}
		route.ManualSteps = []string{
			"deploy LiveKit Server and LiveKit SIP yourself, sharing one Redis deployment, with LiveKit SIP reachable on public SIP signaling and RTP ports; this route has no hosted option, which is what it is for",
			"point the carrier's origination URI at your LiveKit SIP endpoint with transport=tcp",
			"get the selected carrier SIP address, username, password, and phone number from its SIP trunking console; the trunk's own values, which a cold transfer sends the caller's leg through",
			"for inbound calls only, run bash telephony-setup.sh from the build directory: it resolves the inbound trunk by phone number and creates the trunk and dispatch rule (unmute dev --telephony creates the local records itself)",
			"for inbound calls only, set your LiveKit Server's webhook config to POST to " + SIPRoomWebhookPath + " on this agent, with webhook.api_key one of the server's own keys; without it the agent is never told a call arrived and every call rings until it times out, because the dispatch rule cannot dispatch a Pipecat bot (unmute dev --telephony configures this for you)",
		}
		for feature, evidence := range route.Features {
			// Nobody has called through this route. Said plainly rather than with
			// the generic line, because the specific gap is the useful thing: the
			// plane runs it, and no carrier has.
			evidence.Verified = "2026-08-20"
			evidence.Note = "built and offline-proven, and run on the local plane; " +
				"no call has been placed through a carrier trunk on this route"
			route.Features[feature] = evidence
		}
		routes[key] = route
	}
	// Twilio is the one SIP carrier anybody has called through. Both transfer shapes
	// were run on a real trunk and a deployed agent on 2026-08-12, and each run
	// found a defect no offline test had
	// (SCHEMA N33 and N35). The tag still says provisional because that tracks a
	// credentialed smoke in CI, which no route here has; the note is what tracks
	// whether a human made a call, and leaving the generic line on this row would
	// under-report the only route with live evidence on both shapes. Telnyx and
	// Plivo keep the generic note, because nobody has called through them.
	sipTwilio := TelephonyKey{Provider: LiveKit, Transport: "sip", Carrier: "twilio"}
	route = routes[sipTwilio]
	for feature, evidence := range route.Features {
		evidence.Verified = "2026-08-12"
		evidence.Note = "live inbound, cold transfer and warm transfer run 2026-08-12; " +
			"no credentialed smoke runs in CI on any route"
		route.Features[feature] = evidence
	}
	routes[sipTwilio] = route
	exotel = TelephonyKey{Provider: LiveKit, Transport: "sip", Carrier: "exotel"}
	routes[exotel] = TelephonyRoute{Key: exotel, Features: map[TelephonyFeature]TelephonyEvidence{}, RequiredEnvironment: []string{
		"sip_address", "sip_username", "sip_password", "from_number",
	},
		// The other empty-feature row. Same reason as the Pipecat one above.
		LocalPlane: LocalPlaneNone, CloudDeploys: true,
	}
	// LiveKit Twilio connector: Twilio Media Streams over WebSocket, bridged
	// into a local LiveKit room by the generated telephony_bridge.py. Same
	// Twilio surface as the Pipecat carrier-websocket route (HTTPS webhook +
	// media WebSocket + dial-out), so it tunnels and auto-configures the same
	// way, but the agent runs as a LiveKit worker. No SIP trunk and no Redis:
	// only the application container and a local `livekit-server --dev`. This
	// bridge is our own open-source implementation of the Media Streams
	// protocol; LiveKit's hosted connector is Cloud-only.
	// The Pipecat Daily route with a carrier leg (SCHEMA N37). The carrier owns
	// the number and forwards the call over SIP into a per-call Daily room, so the
	// agent and the transfer primitive share the existing SIP phone leg.
	//
	// No `source.*` features. The generated bot reads call sources out of a
	// context table that only the carrier-websocket adapters fill, and this route
	// emits no adapter, so granting them would let a package validate green and
	// receive empty values on a live call (research D11/R14, 2026-08-12).
	dailyCarrier := TelephonyKey{Provider: Pipecat, Transport: "daily-sip", Carrier: "twilio"}
	add(Pipecat, "daily-sip", "twilio", "https://docs.pipecat.ai/pipecat/telephony/daily-sip",
		TelephonyRouteSelected, TelephonyInbound, TelephonyOutbound,
		TelephonyFeature(ColdTransfer), TelephonyFeature(Hangup))
	route = routes[dailyCarrier]
	for feature, evidence := range route.Features {
		evidence.Verified = "2026-08-12"
		// Every feature here is built and offline-proven and none has had a live
		// call, so all five stay provisional. The note says what is missing rather
		// than repeating the generic line, because on this route the gap is one
		// specific thing: nobody has placed a call through a carrier trunk yet.
		// A row loses provisional only against dated live evidence.
		evidence.Note = "built and offline-proven; no call has been placed through a carrier trunk yet"
		route.Features[feature] = evidence
	}
	// No carrier-free loop: the carrier leg terminates in a third-party hosted
	// service, so there is nothing to stand in for on the machine. Keeps its
	// current refusal.
	route.LocalPlane = LocalPlaneNone
	route.CloudDeploys = true
	route.RequiredEnvironment = []string{"account_sid", "auth_token", "sip_address", "from_number"}
	// Processes, PublicEndpoints, and Services below describe the *operator-run
	// helper*, not the deployed agent. Every other route means the application by
	// these fields and a reader will assume the same here, so: the agent deployed
	// to Pipecat Cloud still exposes nothing of its own. The helper is an emitted
	// artifact the operator runs beside the build, and it is truthfully the one
	// process this route runs (data-model section 1, research D5).
	route.Processes = []TelephonyProcess{{
		Name: "telephony-helper", Command: []string{"uv", "run", "telephony_helper.py"},
		Health: "/healthz", Readiness: "/healthz",
	}}
	// The helper answers signed incoming carrier calls and nothing else. Dialling
	// out is started against the platform directly, so there is no outbound
	// endpoint here.
	route.PublicEndpoints = []TelephonyEndpointRule{
		{Name: "inbound", Method: "POST", Path: "/call", AnyFeatures: []TelephonyFeature{TelephonyInbound}},
		{Name: "health", Method: "GET", Path: "/healthz"},
	}
	// Only what the *deployed agent* reads. The helper's own platform key, public
	// signature origin, and two optional knobs are a driver fact registered by
	// the emitter, not here. The Twilio auth token is shared with the agent.
	route.RuntimeEnvironment = []TelephonyEnvironmentRule{{Name: "DAILY_API_KEY"}}
	// No AutoWebhookEndpoint: the CLI never writes a carrier webhook on this
	// route. Pointing the number at the helper is a dictated carrier action,
	// because the helper's public URL is the operator's to choose.
	route.ManualSteps = []string{
		"select a Voice-capable number in the Twilio Console (or reuse the one the LiveKit setup already uses; a number serves one target at a time)",
		"set that number's \"A call comes in\" to a webhook, POST, at https://<your helper host>/call",
		"create or reuse an Elastic SIP Trunk and note its termination address; that value is the sip_address environment name, and the prefix should be one nobody can guess because termination here authenticates by address list rather than by password",
		"create an IP access control list holding the sip.hosts entries from https://ip-info.daily.co/ips/ip-info.json and attach it to the trunk's termination; the list is dynamic and changes are published in the same file three days ahead",
	}
	routes[dailyCarrier] = route
	// The Pipecat Cloud native carrier WebSocket (SCHEMA N38). The carrier streams
	// the call's audio straight to the platform's own endpoint, named by a piece of
	// static markup in the carrier's console, and the platform starts the agent.
	//
	// Read the two empty fields below as the feature: `Processes` and
	// `PublicEndpoints` are empty because **the operator hosts nothing**. Every
	// other route fills at least one of them. This is the only row in the table
	// where a reader should expect both to be empty, and validate has a matching
	// branch so an empty process list is a valid plan here and nowhere else.
	//
	// No `source.*` features, same reason as the Daily carrier row: the call-source
	// table is filled by the carrier-websocket adapters, and this route emits no
	// adapter at all. The Bin's from_number/to_number parameters reach the bot's
	// call_data, which is a different thing from a bound spec variable.
	cloudWebsocket := TelephonyKey{Provider: Pipecat, Transport: "cloud-websocket", Carrier: "twilio"}
	add(Pipecat, "cloud-websocket", "twilio", "https://docs.pipecat.ai/pipecat-cloud/guides/telephony/twilio-websocket",
		TelephonyRouteSelected, TelephonyInbound, TelephonyOutbound,
		TelephonyFeature(ColdTransfer), TelephonyFeature(Hangup))
	route = routes[cloudWebsocket]
	for feature, evidence := range route.Features {
		evidence.Verified = "2026-08-13"
		// The tag stays provisional and the note stopped being generic on the same
		// day, for two different reasons, and conflating them is what made the old
		// note wrong.
		//
		// The **tag** tracks whether a credentialed smoke runs in CI. None does, on
		// any route in this table, so every row here is provisional and lifting one
		// is a CI change rather than a phone call.
		//
		// The **note** tracks what anybody actually did. Live inbound and a live
		// cold transfer were run on a deployed agent on 2026-08-13, so the previous
		// note, "no call has been placed
		// through this endpoint yet", became false and is the kind of stale line a
		// reader is right to trust. What is genuinely unrun is the decline path.
		evidence.Note = "live inbound and cold transfer run 2026-08-13; " +
			"the transfer's decline path is emitted and offline-proven but has not been run; " +
			"no credentialed smoke runs in CI on any route"
		route.Features[feature] = evidence
	}
	// Required only when the package places or redirects calls. Receiving a call
	// needs none of the three: the platform terminates the carrier's stream itself
	// and the emitted bot never speaks to the carrier's API (research F4, D4).
	// ir.Build carries that conditionality; the row states the vocabulary.
	// The platform terminates the same protocol and hands the bot the same
	// interface, so the same stand-in serves this route.
	route.LocalPlane = LocalPlaneMediaWebsocket
	route.CloudDeploys = true
	route.RequiredEnvironment = []string{"account_sid", "auth_token", "from_number"}
	// The organization name completes the service host in outbound markup. The
	// compiler knows the agent name and cannot know this, so it is read by name
	// when the operator places the call.
	route.RuntimeEnvironment = []TelephonyEnvironmentRule{{
		Name:        "PIPECAT_CLOUD_ORGANIZATION",
		AnyFeatures: []TelephonyFeature{TelephonyOutbound},
	}}
	// No AutoWebhookEndpoint: in production the number points at a TwiML Bin, which
	// is a console object rather than a URL, so there is nothing for the CLI to
	// write. `unmute dev --telephony` does point the number at its tunnel, and it
	// does that against the runner's own local webhook, not against a route
	// endpoint, which is why no endpoint appears above.
	route.ManualSteps = []string{
		"select a Voice-capable number you own in this Twilio account (or reuse the one another target already uses; a number serves one target at a time), and take it off any SIP trunk first, because a number on a trunk ignores its voice configuration silently",
		"run `pipecat cloud organizations list` and note the organization name; it is the one value the compiler cannot know",
		"create a TwiML Bin holding the exact markup the generated README dictates, with the organization name pasted in (Twilio Console, TwiML Bins, create)",
		"point the number's \"A call comes in\" at that TwiML Bin (Phone Numbers, Manage, Active Numbers, your number, Voice Configuration)",
	}
	routes[cloudWebsocket] = route
	connector := TelephonyKey{Provider: LiveKit, Transport: "connector", Carrier: "twilio"}
	// No transfers on this route: a transfer needs a platform primitive, and
	// the connector has none — LiveKit's SIP prebuilts need a SIP participant
	// and an outbound trunk, neither of which exists here (SPEC C1, V1).
	connectorFeatures := append([]TelephonyFeature{
		TelephonyRouteSelected, TelephonyInbound, TelephonyOutbound, TelephonyFeature(Hangup),
	}, sourcesWithStream...)
	add(LiveKit, "connector", "twilio", "https://docs.livekit.io/telephony/connectors/twilio/", connectorFeatures...)
	route = routes[connector]
	// Same mechanism as the Pipecat carrier-websocket route, different framework.
	route.LocalPlane = LocalPlaneMediaWebsocket
	route.CloudDeploys = true
	route.RequiredEnvironment = []string{"account_sid", "auth_token", "from_number"}
	route.Processes = []TelephonyProcess{{
		// One container runs both roles: the agent worker (LiveKit) and the
		// Twilio bridge web server. The bridge is what the carrier reaches.
		Name: "application", Command: []string{"sh", "-c", "python -m livekit.agents start agent.py & exec python telephony_bridge.py"},
		Health: "/", Readiness: "/",
	}}
	route.PublicEndpoints = []TelephonyEndpointRule{
		{Name: "inbound", Method: "POST", Path: "/telephony/inbound", AnyFeatures: []TelephonyFeature{TelephonyInbound}},
		{Name: "media", Method: "WS", Path: "/telephony/ws/{token}"},
		{Name: "outbound", Method: "POST", Path: "/telephony/outbound", AnyFeatures: []TelephonyFeature{TelephonyOutbound}},
		{Name: "status", Method: "POST", Path: "/telephony/status"},
	}
	route.RuntimeEnvironment = []TelephonyEnvironmentRule{
		{Name: "LIVEKIT_API_KEY"},
		{Name: "LIVEKIT_API_SECRET"},
		{Name: "LIVEKIT_URL"},
		{Name: "UNMUTE_OUTBOUND_TOKEN", AnyFeatures: []TelephonyFeature{TelephonyOutbound}},
		{Name: "UNMUTE_PUBLIC_URL"},
	}
	// The local Compose supplies the LiveKit key pair and URL (the documented
	// --dev pair); UNMUTE_PUBLIC_URL and UNMUTE_OUTBOUND_TOKEN are dev-supplied
	// by `unmute dev` itself, exactly like the Pipecat route. The comment said so
	// while the list left the last two out, which is why they rendered as blanks
	// in .env.example for the author to fill in (FR-018c).
	route.LocallySuppliedEnvironment = []string{
		"LIVEKIT_API_KEY", "LIVEKIT_API_SECRET", "LIVEKIT_URL",
		"UNMUTE_OUTBOUND_TOKEN", "UNMUTE_PUBLIC_URL",
	}
	route.AutoWebhookEndpoint = "inbound"
	route.ManualSteps = []string{
		"get the Account SID and Auth Token from the Twilio Console account dashboard and select a Voice-capable number",
		"for production, configure the Twilio number voice webhook as POST to the reported inbound endpoint (unmute dev --telephony sets it automatically and prints the previous value)",
		"deploy a self-hosted LiveKit Server and set LIVEKIT_URL and the API key pair to it; the bridge and worker connect out to it, so it needs no public SIP or RTP",
	}
	routes[connector] = route
	return routes
}

// SelectableTelephonyRoutes returns the routes an author can actually name in a
// connection file: those the table marks with the `route` feature.
//
// TelephonyRoutes carries rows with a real environment vocabulary and an empty
// feature map — the two Exotel entries — which ResolveTelephonyFeature refuses.
// Suggesting one in a "provider supports" list sends the author into a second,
// different refusal, so every place that offers a route to a human reads this
// and never re-derives the predicate (research R6).
func SelectableTelephonyRoutes() map[TelephonyKey]TelephonyRoute {
	out := make(map[TelephonyKey]TelephonyRoute)
	for key, route := range TelephonyRoutes() {
		if _, ok := route.Features[TelephonyRouteSelected]; ok {
			out[key] = route
		}
	}
	return out
}

// RouteAccountPrerequisite is a platform feature the provider grants on request
// rather than by default, which a route cannot work without.
//
// It carries no tag on purpose. The four tags describe whether *unmute* can
// honour a field: core never fails, warn prints, gated refuses, provisional
// fails until proven. An account permission is none of those — unmute compiles
// the package perfectly, and whether the author's account has the feature is
// unknowable at compile time. Gating it would refuse correct packages; warning
// would print on every compile forever and train authors to ignore stderr. So
// it states a fact about the route and never claims a failure (research D3).
type RouteAccountPrerequisite struct {
	Name     string             `json:"name"`
	Summary  string             `json:"summary"`
	NeededBy []TelephonyFeature `json:"needed_by"`
	Docs     string             `json:"docs"`
	Verified string             `json:"verified"`
}

// Needs reports whether the package uses anything this prerequisite is needed
// by. Both directions matter: a prerequisite that always prints is a banner
// rather than a warning, which is the noise failure mode research D3 rejects.
func (p RouteAccountPrerequisite) Needs(used []TelephonyFeature) bool {
	for _, feature := range used {
		if slices.Contains(p.NeededBy, feature) {
			return true
		}
	}
	return false
}

type routePrerequisiteRule struct {
	provider  Provider
	transport string
	carrier   string // "" matches any carrier on the transport
	prereq    RouteAccountPrerequisite
}

// routePrerequisites is the only home for these facts. The emitter, the docs,
// and the validate report all read them from here (Principle III).
var routePrerequisites = []routePrerequisiteRule{{
	provider: Pipecat, transport: "daily-sip", carrier: "twilio",
	prereq: RouteAccountPrerequisite{
		Name: "daily_dialout",
		Summary: "Ask Daily to enable dial-out on the domain the agent's rooms belong to: " +
			"it is a paid feature granted on request, per domain, and international dial-out is enabled separately. " +
			"It covers dialling a SIP URI as well as a PSTN number, so a target that carries its calls " +
			"through its own carrier trunk needs the same approval and needs no purchased Daily number.",
		NeededBy: []TelephonyFeature{TelephonyFeature(ColdTransfer), TelephonyOutbound},
		Docs:     "https://docs.pipecat.ai/pipecat-cloud/guides/telephony/daily-dial-out",
		Verified: "2026-08-12",
	},
}}

// RouteAccountPrerequisites returns the account features a route needs.
//
// It takes the route triple so validation and emitted runbooks read one source.
func RouteAccountPrerequisites(provider Provider, transport, carrier string) []RouteAccountPrerequisite {
	var out []RouteAccountPrerequisite
	for _, rule := range routePrerequisites {
		if rule.provider != provider || rule.transport != transport {
			continue
		}
		if rule.carrier != "" && rule.carrier != carrier {
			continue
		}
		out = append(out, rule.prereq)
	}
	return out
}

func TelephonyEnvironment(key TelephonyKey) (required, optional []string, ok bool) {
	route, ok := TelephonyRoutes()[key]
	if !ok {
		return nil, nil, false
	}
	return slices.Clone(route.RequiredEnvironment), slices.Clone(route.OptionalEnvironment), true
}

func ResolveTelephonyFeature(key TelephonyKey, feature TelephonyFeature) TelephonyEvidence {
	route, ok := TelephonyRoutes()[key]
	if !ok {
		return TelephonyEvidence{
			Feature: feature, Tag: Gated,
			Note: fmt.Sprintf("unsupported telephony route (%s, %s, %s)", key.Provider, key.Transport, key.Carrier),
		}
	}
	evidence, ok := route.Features[feature]
	if !ok {
		note := fmt.Sprintf("telephony route (%s, %s, %s) does not support %s", key.Provider, key.Transport, key.Carrier, feature)
		// Transfers ride platform primitives, so the refusal names where they
		// exist (SPEC C1, V1): the author learns the fix, not just the no.
		switch {
		case feature == TelephonyFeature(ColdTransfer):
			note += "; cold transfer compiles on (livekit, sip) trunks and on both Pipecat carrier routes (transport daily-sip and transport cloud-websocket)"
		case feature == TelephonyFeature(WarmTransfer):
			if key.Provider == Pipecat && key.Transport == "cloud-websocket" {
				// Say what it would take, not that it cannot be done. A warm transfer
				// needs to branch on how the destination's leg ended, and on this route
				// the only thing that could branch is a callback endpoint the operator
				// hosts, which is the exact cost this route exists to remove. So the
				// refusal names the trade rather than blaming the platform (SCHEMA N34).
				return TelephonyEvidence{Feature: feature, Tag: Gated, Note: fmt.Sprintf(
					"telephony route (%s, %s, %s) does not emit warm transfer: a warm handoff has to act on how the "+
						"destination's leg ended, which on this route needs a callback endpoint you host, and hosting "+
						"nothing is what this route is for; warm transfer compiles on (livekit, sip) trunks today",
					key.Provider, key.Transport, key.Carrier)}
			}
			if key.Provider == Pipecat && key.Transport == "sip" {
				// The self-hosted trunk route. The stack under it can do a warm
				// handoff, so a message implying otherwise would be false and would
				// send an author looking for a workaround that does not exist.
				return TelephonyEvidence{Feature: feature, Tag: Gated, Note: fmt.Sprintf(
					"telephony route (%s, %s, %s): this project has not built warm transfer on Pipecat "+
						"yet. The stack this route runs on can do it: a warm handoff needs the agent to "+
						"hold both legs and merge them, and that is not built here. Warm transfer "+
						"compiles on (livekit, sip) trunks today",
					key.Provider, key.Transport, key.Carrier)}
			}
			if key.Provider == Pipecat && key.Transport == "daily-sip" {
				// Daily documents warm on this route and this project has not built
				// it. Saying the platform cannot do it would send an author looking
				// for a different platform when what they need is the feature that
				// emits it (SCHEMA N34's wording rule).
				note = fmt.Sprintf("telephony route (%s, %s, %s) does not emit warm transfer yet: Daily documents the pattern and this project has not built it",
					key.Provider, key.Transport, key.Carrier)
			}
			note += "; warm transfer compiles on (livekit, sip) trunks today"
		case strings.HasPrefix(string(feature), TelephonySourcePrefix):
			// A call source is filled by an emitted adapter reading the carrier's
			// own payload. The routes that emit one are the routes that grant it,
			// so the refusal names them: an author who wants the caller's number
			// has a route to move to, not just a no.
			note += "; telephony call sources are filled by the emitted carrier adapter, so they compile on Pipecat's carrier-websocket routes and on (livekit, sip) and (livekit, connector) trunks"
		}
		return TelephonyEvidence{Feature: feature, Tag: Gated, Note: note}
	}
	return evidence
}

package target

import (
	"fmt"
	"slices"
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

type TelephonyRoute struct {
	Key                        TelephonyKey
	Features                   map[TelephonyFeature]TelephonyEvidence
	RequiredEnvironment        []string
	OptionalEnvironment        []string
	Processes                  []TelephonyProcess
	PublicEndpoints            []TelephonyEndpointRule
	RuntimeEnvironment         []TelephonyEnvironmentRule
	LocallySuppliedEnvironment []string
	// DevSuppliedEnvironment names required env values that `unmute dev
	// --telephony` supplies itself for local runs (users never set them
	// locally; production still supplies real values).
	DevSuppliedEnvironment []string
	ManualSteps            []string
	// AutoWebhookEndpoint names the public endpoint the dev command sets as
	// the carrier's voice webhook automatically on every start. Empty means
	// the carrier keeps printed manual steps. This is a carrier fact, not a
	// framework: only carriers with a CLI implementation may carry it.
	AutoWebhookEndpoint string
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
	for _, carrier := range []string{"twilio", "telnyx", "plivo"} {
		features := append([]TelephonyFeature{TelephonyRouteSelected, TelephonyInbound, TelephonyOutbound, TelephonyFeature(Hangup)}, sourcesWithStream...)
		features = append(features, TelephonyFeature(ColdTransfer))
		add(Pipecat, "carrier-websocket", carrier, pipecat, features...)
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
		route.LocallySuppliedEnvironment = []string{"REDIS_URL"}
		route.ManualSteps = steps
		routes[key] = route
	}
	twilio := TelephonyKey{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "twilio"}
	route := routes[twilio]
	route.RequiredEnvironment = []string{"account_sid", "auth_token", "from_number"}
	// Warm transfer is Twilio-only on this transport: the lowering is a bridge
	// between two media WebSockets, and only Twilio's leg of it has been built
	// and linted (human-transfer.md C7/C9). Telnyx and plivo keep failing warm
	// in their own route's words until each has its own bridge and smoke.
	route.Features[TelephonyFeature(WarmTransfer)] = TelephonyEvidence{
		Feature: TelephonyFeature(WarmTransfer), Tag: Provisional,
		Note: "route has not passed its credentialed smoke",
		Docs: pipecat, Verified: "2026-08-11",
	}
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
		TelephonyEndpointRule{Name: "transfer", Method: "POST", Path: "/telephony/transfer/{token}", AnyFeatures: []TelephonyFeature{TelephonyFeature(ColdTransfer)}},
	)
	exotel := TelephonyKey{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "exotel"}
	routes[exotel] = TelephonyRoute{Key: exotel, Features: map[TelephonyFeature]TelephonyEvidence{}, RequiredEnvironment: []string{
		"api_key", "api_token", "account_sid", "subdomain", "from_number", "app_id",
	}}
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
		route.RequiredEnvironment = []string{"sip_address", "sip_username", "sip_password", "from_number"}
		route.Processes = []TelephonyProcess{{
			Name: "agent", Command: []string{"uv", "run", "agent.py", "dev"}, Health: "/", Readiness: "/",
		}}
		route.RuntimeEnvironment = []TelephonyEnvironmentRule{
			{Name: "LIVEKIT_API_KEY"},
			{Name: "LIVEKIT_API_SECRET"},
			{Name: "LIVEKIT_SIP_INBOUND_TRUNK", AnyFeatures: []TelephonyFeature{TelephonyInbound}},
			{Name: "LIVEKIT_SIP_OUTBOUND_TRUNK", AnyFeatures: []TelephonyFeature{TelephonyOutbound, TelephonyFeature(WarmTransfer)}},
			{Name: "LIVEKIT_URL"},
			{Name: "REDIS_URL"},
		}
		route.LocallySuppliedEnvironment = []string{"LIVEKIT_API_KEY", "LIVEKIT_API_SECRET", "LIVEKIT_URL", "REDIS_URL"}
		route.DevSuppliedEnvironment = []string{"LIVEKIT_SIP_INBOUND_TRUNK", "LIVEKIT_SIP_OUTBOUND_TRUNK"}
		route.ManualSteps = []string{
			"get LIVEKIT_URL and the API key pair from the self-hosted LiveKit Server configuration; configure LiveKit Server and LiveKit SIP with the same Redis deployment",
			"deploy LiveKit SIP with public SIP signaling and RTP ports, then point the carrier's origination URI at that public SIP endpoint",
			"get the selected carrier SIP address, username, password, and phone number from its SIP trunking console",
			"for production, materialize the generated SIP JSON inputs, create the LiveKit trunks and dispatch rule with lk, and copy the returned trunk IDs into the reported environment variables (unmute dev --telephony creates the local records and supplies both IDs itself)",
		}
		routes[key] = route
	}
	exotel = TelephonyKey{Provider: LiveKit, Transport: "sip", Carrier: "exotel"}
	routes[exotel] = TelephonyRoute{Key: exotel, Features: map[TelephonyFeature]TelephonyEvidence{}, RequiredEnvironment: []string{
		"sip_address", "sip_username", "sip_password", "from_number",
	}}
	// LiveKit Twilio connector: Twilio Media Streams over WebSocket, bridged
	// into a local LiveKit room by the generated telephony_bridge.py. Same
	// Twilio surface as the Pipecat carrier-websocket route (HTTPS webhook +
	// media WebSocket + dial-out), so it tunnels and auto-configures the same
	// way, but the agent runs as a LiveKit worker. No SIP trunk and no Redis:
	// only the application container and a local `livekit-server --dev`. This
	// bridge is our own open-source implementation of the Media Streams
	// protocol; LiveKit's hosted connector is Cloud-only.
	connector := TelephonyKey{Provider: LiveKit, Transport: "connector", Carrier: "twilio"}
	// No transfers on this route: a transfer needs a platform primitive, and
	// the connector has none — LiveKit's SIP prebuilts need a SIP participant
	// and an outbound trunk, neither of which exists here (SPEC C1, V1).
	connectorFeatures := append([]TelephonyFeature{
		TelephonyRouteSelected, TelephonyInbound, TelephonyOutbound, TelephonyFeature(Hangup),
	}, sourcesWithStream...)
	add(LiveKit, "connector", "twilio", "https://docs.livekit.io/telephony/connectors/twilio/", connectorFeatures...)
	route = routes[connector]
	route.RequiredEnvironment = []string{"account_sid", "auth_token", "from_number"}
	route.Processes = []TelephonyProcess{{
		// One container runs both roles: the agent worker (LiveKit) and the
		// Twilio bridge web server. The bridge is what the carrier reaches.
		Name: "application", Command: []string{"sh", "-c", "python agent.py start & exec python telephony_bridge.py"},
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
	// by `unmute dev` itself, exactly like the Pipecat route.
	route.LocallySuppliedEnvironment = []string{"LIVEKIT_API_KEY", "LIVEKIT_API_SECRET", "LIVEKIT_URL"}
	route.AutoWebhookEndpoint = "inbound"
	route.ManualSteps = []string{
		"get the Account SID and Auth Token from the Twilio Console account dashboard and select a Voice-capable number",
		"for production, configure the Twilio number voice webhook as POST to the reported inbound endpoint (unmute dev --telephony sets it automatically and prints the previous value)",
		"deploy a self-hosted LiveKit Server and set LIVEKIT_URL and the API key pair to it; the bridge and worker connect out to it, so it needs no public SIP or RTP",
	}
	routes[connector] = route
	return routes
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
		return TelephonyEvidence{
			Feature: feature, Tag: Gated,
			Note: fmt.Sprintf("telephony route (%s, %s, %s) does not support %s", key.Provider, key.Transport, key.Carrier, feature),
		}
	}
	return evidence
}

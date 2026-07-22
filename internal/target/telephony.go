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
	TelephonyRouteSelected   TelephonyFeature = "route"
	TelephonyInbound         TelephonyFeature = "inbound"
	TelephonyOutbound        TelephonyFeature = "outbound"
	TelephonyBriefingSummary TelephonyFeature = "briefing.summary"
	TelephonyBriefingMessage TelephonyFeature = "briefing.message"
	TelephonyBriefingWait    TelephonyFeature = "briefing.wait"
	TelephonySourcePrefix                     = "source."
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
			TelephonyFeature(VoicemailDetection), TelephonyBriefingSummary,
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
			{Name: "LIVEKIT_SIP_URI"},
			{Name: "LIVEKIT_URL"},
			{Name: "REDIS_URL"},
		}
		route.LocallySuppliedEnvironment = []string{"LIVEKIT_API_KEY", "LIVEKIT_API_SECRET", "LIVEKIT_URL", "REDIS_URL"}
		route.DevSuppliedEnvironment = []string{"LIVEKIT_SIP_INBOUND_TRUNK", "LIVEKIT_SIP_OUTBOUND_TRUNK"}
		route.ManualSteps = []string{
			"get LIVEKIT_URL and the API key pair from the self-hosted LiveKit Server configuration; configure LiveKit Server and LiveKit SIP with the same Redis deployment",
			"deploy LiveKit SIP with public SIP signaling and RTP ports, then set LIVEKIT_SIP_URI to that public SIP endpoint",
			"get the selected carrier SIP address, username, password, and phone number from its SIP trunking console",
			"for production, materialize the generated SIP JSON inputs, create the LiveKit trunks and dispatch rule with lk, and copy the returned trunk IDs into the reported environment variables (unmute dev --telephony creates the local records and supplies both IDs itself)",
		}
		routes[key] = route
	}
	exotel = TelephonyKey{Provider: LiveKit, Transport: "sip", Carrier: "exotel"}
	routes[exotel] = TelephonyRoute{Key: exotel, Features: map[TelephonyFeature]TelephonyEvidence{}, RequiredEnvironment: []string{
		"sip_address", "sip_username", "sip_password", "from_number",
	}}
	key := TelephonyKey{Provider: LiveKit, Transport: "connector", Carrier: "twilio"}
	routes[key] = TelephonyRoute{Key: key, Features: map[TelephonyFeature]TelephonyEvidence{}, RequiredEnvironment: []string{
		"account_sid", "auth_token", "from_number",
	}}
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

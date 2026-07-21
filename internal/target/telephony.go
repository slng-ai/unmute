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
	Key                 TelephonyKey
	Features            map[TelephonyFeature]TelephonyEvidence
	RequiredEnvironment []string
	OptionalEnvironment []string
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
	twilio := TelephonyKey{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "twilio"}
	route := routes[twilio]
	route.RequiredEnvironment = []string{"account_sid", "auth_token", "from_number"}
	routes[twilio] = route
	telnyx := TelephonyKey{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "telnyx"}
	route = routes[telnyx]
	route.RequiredEnvironment = []string{"api_key", "public_key", "connection_id", "from_number"}
	routes[telnyx] = route
	plivo := TelephonyKey{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "plivo"}
	route = routes[plivo]
	route.RequiredEnvironment = []string{"auth_id", "auth_token", "from_number"}
	routes[plivo] = route
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

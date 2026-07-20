package target

import "fmt"

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
	Key      TelephonyKey
	Features map[TelephonyFeature]TelephonyEvidence
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
	for _, carrier := range []string{"twilio", "telnyx", "plivo", "exotel"} {
		features := append([]TelephonyFeature{TelephonyRouteSelected, TelephonyInbound, TelephonyOutbound, TelephonyFeature(Hangup)}, sourcesWithStream...)
		if carrier != "exotel" {
			features = append(features, TelephonyFeature(ColdTransfer), TelephonyFeature(WarmTransfer), TelephonyBriefingSummary)
		}
		add(Pipecat, "carrier-websocket", carrier, pipecat, features...)
	}
	sip := "https://docs.livekit.io/transport/self-hosting/sip-server/"
	for _, carrier := range []string{"twilio", "telnyx", "plivo", "exotel"} {
		features := append([]TelephonyFeature{
			TelephonyRouteSelected, TelephonyInbound, TelephonyOutbound, TelephonyFeature(ColdTransfer),
			TelephonyFeature(WarmTransfer), TelephonyFeature(Hangup),
			TelephonyFeature(VoicemailDetection), TelephonyBriefingSummary,
		}, sourcesWithoutStream...)
		add(LiveKit, "sip", carrier, sip, features...)
	}
	connector := "https://docs.livekit.io/telephony/connectors/twilio/"
	add(LiveKit, "connector", "twilio", connector,
		append([]TelephonyFeature{TelephonyRouteSelected, TelephonyInbound, TelephonyOutbound, TelephonyFeature(Hangup)}, sourcesWithoutStream...)...,
	)
	return routes
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

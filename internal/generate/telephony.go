package generate

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/slng/unmute/internal/ir"
)

type TelephonyRuntimePlan struct {
	Route           ir.TelephonyKey                  `json:"route"`
	Processes       []TelephonyProcess               `json:"processes"`
	PublicEndpoints []TelephonyEndpoint              `json:"public_endpoints,omitempty"`
	RequiredEnv     []string                         `json:"required_env"`
	ManualSteps     []string                         `json:"manual_steps,omitempty"`
	Evidence        []ir.TelephonyFeatureEvidence    `json:"evidence"`
	Services        []string                         `json:"services"`
	Coordination    string                           `json:"coordination"`
	Reasons         []ir.TelephonyCoordinationReason `json:"coordination_reasons"`
	AdmissionOwner  string                           `json:"admission_owner"`
}

type TelephonyProcess struct {
	Name      string   `json:"name"`
	Command   []string `json:"command"`
	Health    string   `json:"health,omitempty"`
	Readiness string   `json:"readiness,omitempty"`
}

type TelephonyEndpoint struct {
	Name   string `json:"name"`
	Method string `json:"method"`
	Path   string `json:"path"`
}

func TelephonyRuntimePlanFor(target ir.Target) *TelephonyRuntimePlan {
	plan := target.Telephony
	if plan == nil {
		return nil
	}
	runtime := &TelephonyRuntimePlan{
		Route: plan.Key, Evidence: slices.Clone(plan.Evidence), Coordination: plan.Coordination,
		Services: slices.Clone(plan.Services), Reasons: slices.Clone(plan.CoordinationReasons),
		AdmissionOwner: plan.AdmissionOwner,
		ManualSteps:    []string{"configure the selected carrier or SIP trunk to the reported public endpoints"},
	}
	if plan.Key.Provider == ir.ProviderLiveKit && plan.Key.Transport == "sip" {
		runtime.ManualSteps = []string{
			"get LIVEKIT_URL and the API key pair from the self-hosted LiveKit Server configuration; configure LiveKit Server and LiveKit SIP with the same Redis deployment",
			"deploy LiveKit SIP with public SIP signaling and RTP ports, then set LIVEKIT_SIP_URI to that public SIP endpoint",
			"get the selected carrier SIP address, username, password, and phone number from its SIP trunking console",
			"materialize the generated SIP JSON inputs, create the LiveKit trunks and dispatch rule with lk, and copy the returned trunk IDs into the reported environment variables",
		}
		runtime.RequiredEnv = append(runtime.RequiredEnv,
			"LIVEKIT_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET", "LIVEKIT_SIP_URI",
		)
		if telephonyHasFeature(plan, "inbound") {
			runtime.RequiredEnv = append(runtime.RequiredEnv, "LIVEKIT_SIP_INBOUND_TRUNK")
		}
		if telephonyHasFeature(plan, "outbound") || telephonyHasFeature(plan, "warm_transfer") {
			runtime.RequiredEnv = append(runtime.RequiredEnv, "LIVEKIT_SIP_OUTBOUND_TRUNK")
		}
	}
	if plan.Key.Provider == ir.ProviderPipecat && plan.Key.Transport == "carrier-websocket" && plan.Key.Carrier == "twilio" {
		runtime.ManualSteps = []string{
			"get the Account SID and Auth Token from the Twilio Console account dashboard and select a Voice-capable number",
			"configure the Twilio number voice webhook as POST to the reported inbound endpoint",
			"configure Twilio call status callbacks as POST to the reported status endpoint",
		}
	}
	if plan.Key.Provider == ir.ProviderPipecat && plan.Key.Transport == "carrier-websocket" && plan.Key.Carrier == "telnyx" {
		runtime.ManualSteps = []string{
			"get an API key and public key from Telnyx Mission Control, then select a Voice API Application and phone number",
			"set the Voice API Application webhook URL to the reported inbound endpoint and use API version 2",
			"assign the selected phone number to that Voice API Application; generated outbound calls report status to the reported status endpoint",
		}
	}
	if plan.Key.Provider == ir.ProviderPipecat && plan.Key.Transport == "carrier-websocket" && plan.Key.Carrier == "plivo" {
		runtime.ManualSteps = []string{
			"get the Auth ID and Auth Token from the Plivo Console dashboard and select a Voice-capable number",
			"create a Voice XML Application whose Answer URL is POST to the reported inbound endpoint",
			"assign the selected phone number to that XML Application and configure its Hangup URL as POST to the reported status endpoint",
		}
	}
	for _, name := range plan.Environment {
		runtime.RequiredEnv = append(runtime.RequiredEnv, name)
	}
	for _, evidence := range plan.Evidence {
		if evidence.Feature == "outbound" {
			runtime.RequiredEnv = append(runtime.RequiredEnv, "UNMUTE_OUTBOUND_TOKEN")
		}
	}
	if target.Transport == "carrier-websocket" {
		runtime.RequiredEnv = append(runtime.RequiredEnv, "UNMUTE_PUBLIC_URL")
	}
	if plan.Coordination == "shared" {
		runtime.RequiredEnv = append(runtime.RequiredEnv, "REDIS_URL")
	}
	slices.Sort(runtime.RequiredEnv)
	switch target.Provider {
	case ir.ProviderPipecat:
		command := []string{"uv", "run", "bot.py", "--host", "0.0.0.0", "--port", "7860"}
		if target.Telephony != nil {
			command = []string{"uv", "run", "uvicorn", "telephony:app", "--host", "0.0.0.0", "--port", "7860"}
		}
		runtime.Processes = []TelephonyProcess{{
			Name: "agent", Command: command,
			Health: "/healthz", Readiness: "/readyz",
		}}
	case ir.ProviderLiveKit:
		runtime.Processes = []TelephonyProcess{{
			Name: "agent", Command: []string{"uv", "run", "agent.py", "dev"},
			Health: "/", Readiness: "/",
		}}
	}
	switch target.Transport {
	case "carrier-websocket":
		if telephonyHasFeature(plan, "inbound") {
			runtime.PublicEndpoints = append(runtime.PublicEndpoints, TelephonyEndpoint{Name: "inbound", Method: "POST", Path: "/telephony/inbound"})
		}
		runtime.PublicEndpoints = append(runtime.PublicEndpoints, TelephonyEndpoint{Name: "media", Method: "WS", Path: "/telephony/ws/{token}"})
		if telephonyHasFeature(plan, "outbound") {
			runtime.PublicEndpoints = append(runtime.PublicEndpoints, TelephonyEndpoint{Name: "outbound", Method: "POST", Path: "/telephony/outbound"})
		}
		runtime.PublicEndpoints = append(runtime.PublicEndpoints, TelephonyEndpoint{Name: "status", Method: "POST", Path: "/telephony/status"})
		if plan.Key.Carrier == "plivo" && telephonyHasFeature(plan, "outbound") {
			runtime.PublicEndpoints = append(runtime.PublicEndpoints, TelephonyEndpoint{Name: "outbound-answer", Method: "POST", Path: "/telephony/answer/{token}"})
		}
		if plan.Key.Carrier == "plivo" && telephonyHasFeature(plan, "cold_transfer") {
			runtime.PublicEndpoints = append(runtime.PublicEndpoints, TelephonyEndpoint{Name: "transfer", Method: "POST", Path: "/telephony/transfer/{token}"})
		}
	case "connector":
		runtime.PublicEndpoints = []TelephonyEndpoint{
			{Name: "inbound", Method: "POST", Path: "/telephony/inbound"},
			{Name: "outbound", Method: "POST", Path: "/telephony/outbound"},
			{Name: "status", Method: "POST", Path: "/telephony/status"},
		}
	}
	slices.Sort(runtime.RequiredEnv)
	runtime.RequiredEnv = slices.Compact(runtime.RequiredEnv)
	return runtime
}

func telephonyHasFeature(plan *ir.TelephonyPlan, feature string) bool {
	for _, evidence := range plan.Evidence {
		if evidence.Feature == feature {
			return true
		}
	}
	return false
}

func withTelephonyReport(files []File, plan *TelephonyRuntimePlan) ([]File, error) {
	if plan == nil {
		return files, nil
	}
	for i := range files {
		if files[i].Path != "compile-report.json" {
			continue
		}
		var report map[string]any
		if err := json.Unmarshal(files[i].Content, &report); err != nil {
			return nil, fmt.Errorf("decode compile report: %w", err)
		}
		seen := make(map[string]bool, len(plan.RequiredEnv))
		for _, name := range plan.RequiredEnv {
			seen[name] = true
		}
		if required, ok := report["required_env"].([]any); ok {
			for _, value := range required {
				name, ok := value.(string)
				if ok && !seen[name] {
					seen[name] = true
					plan.RequiredEnv = append(plan.RequiredEnv, name)
				}
			}
			slices.Sort(plan.RequiredEnv)
		}
		report["telephony"] = plan
		content, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("encode compile report: %w", err)
		}
		files[i].Content = append(content, '\n')
		return files, nil
	}
	return nil, fmt.Errorf("compile-report.json missing from code artifact")
}

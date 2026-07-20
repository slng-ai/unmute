package generate

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/slng/unmute/internal/ir"
)

type TelephonyRuntimePlan struct {
	Route           ir.TelephonyKey               `json:"route"`
	Processes       []TelephonyProcess            `json:"processes"`
	PublicEndpoints []TelephonyEndpoint           `json:"public_endpoints,omitempty"`
	RequiredEnv     []string                      `json:"required_env"`
	ManualSteps     []string                      `json:"manual_steps,omitempty"`
	Evidence        []ir.TelephonyFeatureEvidence `json:"evidence"`
	Coordination    string                        `json:"coordination"`
	AdmissionOwner  string                        `json:"admission_owner"`
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
		AdmissionOwner: plan.AdmissionOwner,
		ManualSteps:    []string{"configure the selected carrier or SIP trunk to the reported public endpoints"},
	}
	if plan.Key.Provider == ir.ProviderPipecat && plan.Key.Transport == "carrier-websocket" && plan.Key.Carrier == "twilio" {
		runtime.ManualSteps = []string{
			"get the Account SID and Auth Token from the Twilio Console account dashboard and select a Voice-capable number",
			"configure the Twilio number voice webhook as POST to the reported inbound endpoint",
			"configure Twilio call status callbacks as POST to the reported status endpoint",
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
			Health: "/healthz", Readiness: "/readyz",
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
	case "connector":
		runtime.PublicEndpoints = []TelephonyEndpoint{
			{Name: "inbound", Method: "POST", Path: "/telephony/inbound"},
			{Name: "outbound", Method: "POST", Path: "/telephony/outbound"},
			{Name: "status", Method: "POST", Path: "/telephony/status"},
		}
	}
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

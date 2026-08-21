package generate

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/slng-ai/unmute/internal/ir"
)

type TelephonyRuntimePlan struct {
	Route            ir.TelephonyKey     `json:"route"`
	Processes        []TelephonyProcess  `json:"processes"`
	PublicEndpoints  []TelephonyEndpoint `json:"public_endpoints,omitempty"`
	RequiredEnv      []string            `json:"required_env"`
	LocalEnvironment []string            `json:"locally_supplied_environment"`
	// Environment maps the Connection's carrier vocabulary keys to the env
	// var names the user chose (names only, never values).
	Environment map[string]string `json:"environment,omitempty"`
	// AutoWebhookEndpoint names the public endpoint the dev command sets as
	// the carrier voice webhook automatically; empty keeps manual steps.
	AutoWebhookEndpoint string   `json:"auto_webhook_endpoint,omitempty"`
	ManualSteps         []string `json:"manual_steps,omitempty"`
	// LocalPlane is the route's carrier-free development plane, carried through
	// so the compile report states it and the dev command reads one structure.
	LocalPlane string `json:"local_plane"`
	// The plane's own topology, derived in internal/ir. The emitted Compose file
	// and the dev command both read it, which is what keeps the address the
	// plane advertises and the address the command prints from drifting apart.
	PlaneSubnet     string                           `json:"plane_subnet,omitempty"`
	PlaneSIPAddress string                           `json:"plane_sip_address,omitempty"`
	LocalEndpoints  []ir.TelephonyLocalEndpoint      `json:"local_endpoints,omitempty"`
	Evidence        []ir.TelephonyFeatureEvidence    `json:"evidence"`
	Services        []string                         `json:"services"`
	Coordination    string                           `json:"coordination"`
	Reasons         []ir.TelephonyCoordinationReason `json:"coordination_reasons"`
	AdmissionOwner  string                           `json:"admission_owner"`
}

type TelephonyProcess = ir.TelephonyProcess

type TelephonyEndpoint = ir.TelephonyEndpoint

func TelephonyRuntimePlanFor(target ir.Target) *TelephonyRuntimePlan {
	plan := target.Telephony
	if plan == nil {
		return nil
	}
	processes := make([]TelephonyProcess, len(plan.Processes))
	for i, process := range plan.Processes {
		processes[i] = process
		processes[i].Command = slices.Clone(process.Command)
	}
	reasons := make([]ir.TelephonyCoordinationReason, len(plan.CoordinationReasons))
	for i, reason := range plan.CoordinationReasons {
		reasons[i] = reason
		reasons[i].Consumers = slices.Clone(reason.Consumers)
	}
	runtime := &TelephonyRuntimePlan{
		Route: plan.Key, Evidence: slices.Clone(plan.Evidence), Coordination: plan.Coordination,
		Processes: processes, PublicEndpoints: slices.Clone(plan.PublicEndpoints),
		RequiredEnv: slices.Clone(plan.RequiredEnvironment), LocalEnvironment: slices.Clone(plan.LocalEnvironment),
		Environment:         maps.Clone(plan.Environment),
		AutoWebhookEndpoint: plan.AutoWebhookEndpoint,
		LocalPlane:          plan.LocalPlane,
		PlaneSubnet:         plan.PlaneSubnet, PlaneSIPAddress: plan.PlaneSIPAddress,
		LocalEndpoints: slices.Clone(plan.LocalEndpoints),
		ManualSteps:    slices.Clone(plan.ManualSteps), Services: slices.Clone(plan.Services),
		Reasons: reasons, AdmissionOwner: plan.AdmissionOwner,
	}
	return runtime
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

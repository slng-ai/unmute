package ir

import (
	"fmt"
	"slices"
	"strings"

	packagespec "github.com/slng-ai/unmute/internal/spec"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// ValidateManifest checks the complete declaration, including palette alternatives
// and target overrides which a selected artifact would otherwise never read.
func ValidateManifest(agent *Agent) (errors, warnings []string) {
	m := agent.Manifest
	if m == nil {
		return nil, nil
	}
	check := func(path, value, rule string, allow *packagespec.ManifestAllow) {
		if allow != nil && !slices.Contains(allow.Allow, value) {
			errors = append(errors, fmt.Sprintf("%s: %q violates manifest %q %s; allowed: %v", path, value, m.Name, rule, allow.Allow))
		}
	}
	checkModel := func(path string, model ModelDef, platform Provider) {
		role := string(model.Kind)
		var rows []packagespec.ManifestModel
		switch model.Kind {
		case KindListen:
			rows = m.Models.Listen
		case KindSpeak:
			rows = m.Models.Speak
		case KindThink:
			rows = m.Models.Think
		default:
			return
		}
		binding := toBinding(model)
		catalogRole := targetcap.Role(role)
		if model.Kind == KindThink {
			catalogRole = targetcap.Reason
		}
		entry, known := targetcap.DefaultCatalog().Lookup(targetcap.Provider(platform), catalogRole, binding.Provider)
		modelID := binding.Model
		if known && entry.Call != nil && entry.Call.Model.Arg != "" {
			if override, ok := binding.Params[entry.Call.Model.Arg]; ok {
				modelID = fmt.Sprint(override)
			}
		}
		if rows != nil {
			allowed := []string{}
			for _, row := range rows {
				if row.Provider == binding.Provider {
					allowed = row.Allow
					break
				}
			}
			if allowed != nil {
				check(path+".model", modelID, "models."+role+" (provider "+binding.Provider+")", &packagespec.ManifestAllow{Allow: allowed})
			}
		}
		if m.Languages != nil && (model.Kind == KindListen || model.Kind == KindSpeak) {
			language := binding.Language
			verifiable := platform == "" // base fields are checked too; target rows resolve actual parameter precedence.
			if known && entry.Call != nil && !entry.Call.NoLanguage && entry.Call.Language.Arg != "" {
				verifiable = true
				if override, ok := binding.Params[entry.Call.Language.Arg]; ok {
					language, _ = override.(string)
				}
			}
			if len(m.Languages.Allow) == 0 {
				errors = append(errors, fmt.Sprintf("%s.language: manifest %q languages.allow permits no speech languages; remove this speech model or allow a language", path, m.Name))
			} else if !verifiable || language == "" || strings.EqualFold(language, "auto") || strings.EqualFold(language, "multi") {
				warnings = append(warnings, fmt.Sprintf("%s.language: manifest %q languages.allow cannot be verified; select a model with an explicit supported language setting", path, m.Name))
			} else {
				allowed := false
				for _, value := range m.Languages.Allow {
					allowed = allowed || strings.EqualFold(value, language)
				}
				if !allowed {
					errors = append(errors, fmt.Sprintf("%s.language: %q violates manifest %q languages.allow; allowed: %v", path, language, m.Name, m.Languages.Allow))
				}
			}
		}
		if m.Regions != nil {
			for _, row := range m.Regions.Models {
				if row.Role != role || row.Provider != binding.Provider {
					continue
				}
				region := manifestModelRegion(binding, model.Kind, platform)
				if len(row.Allow) == 0 {
					errors = append(errors, fmt.Sprintf("%s.region: manifest %q regions.models permits no region for %s/%s; remove this model or allow a region", path, m.Name, role, binding.Provider))
				} else if region == "" {
					warnings = append(warnings, fmt.Sprintf("%s.region: manifest %q regions.models cannot be verified; set a supported explicit region for %s", path, m.Name, binding.Provider))
				} else {
					check(path+".region", region, "regions.models "+role+"/"+binding.Provider, &packagespec.ManifestAllow{Allow: row.Allow})
				}
			}
		}
	}
	for _, name := range sortedKeys(agent.Models) {
		checkModel("models."+name, agent.Models[name], "")
	}
	for _, name := range sortedKeys(agent.Targets) {
		target := agent.Targets[name]
		path := "targets." + name
		check(path+".provider", string(target.Provider), "targets.allow", m.Targets)
		for _, profile := range sortedKeys(agent.Models) {
			model := agent.Models[profile]
			if override, ok := target.ManifestModels[profile]; ok {
				model = override
			}
			checkModel(path+".models."+profile, model, target.Provider)
		}
		if m.Regions != nil {
			for _, row := range m.Regions.Deployments {
				if row.Provider != string(target.Provider) {
					continue
				}
				if len(row.Allow) == 0 {
					errors = append(errors, fmt.Sprintf("%s.deployment_region: manifest %q regions.deployments permits no region for %s; remove this target or allow a region", path, m.Name, row.Provider))
					continue
				}
				if len(target.DeploymentRegions) == 0 {
					warnings = append(warnings, fmt.Sprintf("%s.deployment_region: manifest %q regions.deployments cannot be verified; set an explicit deployment_region", path, m.Name))
				}
				for _, region := range target.DeploymentRegions {
					check(path+".deployment_region", region, "regions.deployments "+row.Provider, &packagespec.ManifestAllow{Allow: row.Allow})
				}
			}
		}
	}
	if agent.Tracing != nil {
		check("tracing.provider", agent.Tracing.Provider, "tracing.allow", m.Tracing)
	}
	if m.Tools != nil {
		for _, name := range sortedKeys(agent.Tools) {
			tool := agent.Tools[name]
			path := "tools." + name
			check(path, name, "tools.names.allow", m.Tools.Names)
			check(path, string(tool.Execution), "tools.kinds.allow", m.Tools.Kinds)
			if tool.Execution == ToolBuiltin {
				check(path+".builtin", tool.Builtin, "tools.builtin.allow", m.Tools.Builtin)
			}
			if tool.Execution == ToolSlngHosted {
				hosted := tool.HostedName
				if hosted == "" {
					hosted = name
				}
				check(path+".slng", hosted, "tools.slng.allow", m.Tools.Slng)
			}
		}
	}
	return errors, warnings
}

// Only fields with a compiler-known reader count as evidence. An arbitrary
// params.region on another integration is passthrough, not a residency promise.
func manifestModelRegion(binding Binding, kind ModelKind, platform Provider) string {
	key := ManifestModelRegionParam(binding.Provider, kind, platform)
	if key == "" {
		return ""
	}
	region, _ := binding.Params[key].(string)
	if binding.EndpointEnv != "" {
		return ""
	}
	return region
}

// ManifestModelRegionParam names the native region field the compiler knows.
// Empty means no verifiable field, so creation must not invent one.
func ManifestModelRegionParam(provider string, kind ModelKind, platform Provider) string {
	if platform != "" && platform != ProviderLiveKit && (platform != ProviderPipecat || provider != "slng") {
		return ""
	}
	switch provider {
	case "slng":
		return "world_part"
	case "aws":
		if kind == KindThink {
			return "region"
		}
		return ""
	default:
		return ""
	}
}

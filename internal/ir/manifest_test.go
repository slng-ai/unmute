package ir

import (
	"strings"
	"testing"

	packagespec "github.com/slng-ai/unmute/internal/spec"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

func TestManifestChecksEveryRule(t *testing.T) {
	manifest, err := packagespec.ParseManifest([]byte(`manifest: acme
version: 1
models:
  listen:
    - provider: slng
      allow: [approved]
languages:
  allow: [en]
regions:
  models:
    - role: listen
      provider: slng
      allow: [eu-north]
  deployments:
    - provider: livekit
      allow: [eu-central]
targets:
  allow: [livekit]
tools:
  kinds:
    allow: [builtin]
  names:
    allow: [finish]
  builtin:
    allow: [end_call]
  slng:
    allow: [hosted-approved]
tracing:
  allow: [langfuse]
`))
	if err != nil {
		t.Fatal(err)
	}
	agent := &Agent{Manifest: manifest,
		Models:  map[string]ModelDef{"unused": {Kind: KindListen, Provider: "slng", Model: "denied", Language: "es", Params: map[string]any{"world_part": "us-east"}}},
		Targets: map[string]Target{"other": {Provider: ProviderPipecat}, "primary": {Provider: ProviderLiveKit, DeploymentRegions: []string{"us-east"}}},
		Tools:   map[string]Tool{"wrong_builtin": {Execution: ToolBuiltin, Builtin: "hangup"}, "remote": {Execution: ToolSlngHosted, HostedName: "denied-hosted"}},
		Tracing: &Tracing{Provider: "coval"},
	}
	errors, _ := ValidateManifest(agent)
	joined := strings.Join(errors, "\n")
	for _, want := range []string{"models.listen", "languages.allow", "regions.models", "regions.deployments", "targets.allow", "tools.names.allow", "tools.kinds.allow", "tools.builtin.allow", "tools.slng.allow", "tracing.allow"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s in %s", want, joined)
		}
	}
	agent.Manifest = nil
	if errors, warnings := ValidateManifest(agent); len(errors)+len(warnings) != 0 {
		t.Fatal("no manifest changed behavior")
	}
}

func TestManifestRefusesUnusedOverrideOnUnselectedTarget(t *testing.T) {
	pkg := loadSafeCore(t)
	pkg.Agent.Listen = "transcriber"
	pkg.Manifest = &packagespec.Manifest{Name: "acme", Version: 1, Models: packagespec.ManifestModels{Listen: []packagespec.ManifestModel{{Provider: "slng", Allow: []string{"approved"}}}}}
	pkg.Agent.Models.Listen["unused"] = packagespec.ModelDef{Provider: "slng", Model: "approved"}
	for name, target := range pkg.Targets {
		if target.Provider == "pipecat" {
			if target.Models == nil {
				target.Models = map[string]packagespec.ModelDef{}
			}
			target.Models["unused"] = packagespec.ModelDef{Provider: "slng", Model: "forbidden"}
			pkg.Targets[name] = target
		}
	}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	var selected Target
	for _, target := range agent.Targets {
		if target.Provider == ProviderLiveKit {
			selected = target
		}
	}
	report, err := Validate(agent, []Target{selected}, targetcap.Default())
	if err == nil {
		t.Fatal("unselected target override escaped")
	}
	if !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "forbidden") {
		t.Fatalf("override not checked: %#v", report.PerTarget)
	}
}

func TestManifestUnknownSettingsWarnAndLanguageParamWins(t *testing.T) {
	m := &packagespec.Manifest{Name: "acme", Version: 1, Languages: &packagespec.ManifestAllow{Allow: []string{"en"}}}
	agent := &Agent{Manifest: m, Models: map[string]ModelDef{"speech": {Kind: KindListen, Provider: "deepgram", Model: "nova", Language: "en", Params: map[string]any{"language": "es"}}}, Targets: map[string]Target{"livekit": {Provider: ProviderLiveKit}}}
	errors, _ := ValidateManifest(agent)
	if !strings.Contains(strings.Join(errors, "\n"), `"es"`) {
		t.Fatalf("language override escaped: %v", errors)
	}
	model := agent.Models["speech"]
	model.Language = ""
	model.Params = nil
	agent.Models["speech"] = model
	errors, warnings := ValidateManifest(agent)
	if len(errors) != 0 || len(warnings) == 0 {
		t.Fatalf("unknown language: errors=%v warnings=%v", errors, warnings)
	}
	model.Language = "EN"
	agent.Models["speech"] = model
	errors, _ = ValidateManifest(agent)
	if len(errors) != 0 {
		t.Fatalf("case-insensitive tag: %v", errors)
	}
	m.Languages.Allow = []string{}
	errors, _ = ValidateManifest(agent)
	if len(errors) == 0 {
		t.Fatal("empty language allowlist allowed en")
	}
}

func TestManifestThinkModelParamAndRegionEvidence(t *testing.T) {
	agent := &Agent{Manifest: &packagespec.Manifest{Name: "acme", Version: 1, Models: packagespec.ManifestModels{Think: []packagespec.ManifestModel{{Provider: "openai", Allow: []string{"approved"}}}}}, Models: map[string]ModelDef{"think": {Kind: KindThink, Provider: "openai", Model: "approved", Params: map[string]any{"model": "forbidden"}}}, Targets: map[string]Target{"pipecat": {Provider: ProviderPipecat}}}
	errors, _ := ValidateManifest(agent)
	if !strings.Contains(strings.Join(errors, "\n"), "forbidden") {
		t.Fatalf("think params.model escaped: %v", errors)
	}
	for _, tc := range []struct {
		provider string
		kind     ModelKind
		platform Provider
		want     string
	}{
		{"azure", KindThink, ProviderLiveKit, ""}, {"bedrock", KindThink, ProviderLiveKit, ""},
		{"aws", KindThink, ProviderPipecat, ""}, {"aws", KindListen, ProviderLiveKit, ""},
		{"aws", KindThink, ProviderLiveKit, "eu-west"}, {"slng", KindListen, ProviderSlng, ""},
	} {
		got := manifestModelRegion(Binding{Provider: tc.provider, Params: map[string]any{"region": "eu-west", "world_part": "eu-west"}}, tc.kind, tc.platform)
		if got != tc.want {
			t.Errorf("%s %s on %s region = %q, want %q", tc.provider, tc.kind, tc.platform, got, tc.want)
		}
	}
}

func TestManifestEmptyAllowlistsRefuseUnknownValues(t *testing.T) {
	for _, tc := range []struct {
		name     string
		manifest *packagespec.Manifest
	}{
		{"languages.allow", &packagespec.Manifest{Languages: &packagespec.ManifestAllow{Allow: []string{}}}},
		{"regions.models", &packagespec.Manifest{Regions: &packagespec.ManifestRegions{Models: []packagespec.ManifestModelRegion{{Role: "listen", Provider: "slng", Allow: []string{}}}}}},
		{"regions.deployments", &packagespec.Manifest{Regions: &packagespec.ManifestRegions{Deployments: []packagespec.ManifestDeploymentRegion{{Provider: "livekit", Allow: []string{}}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.manifest.Name = "acme"
			tc.manifest.Version = 1
			agent := &Agent{Manifest: tc.manifest, Models: map[string]ModelDef{"speech": {Kind: KindListen, Provider: "slng", Model: "speech"}}, Targets: map[string]Target{"livekit": {Provider: ProviderLiveKit}}}
			errors, warnings := ValidateManifest(agent)
			if !strings.Contains(strings.Join(errors, "\n"), tc.name) {
				t.Fatalf("empty allowlist bypassed: errors=%v warnings=%v", errors, warnings)
			}
			if len(warnings) != 0 {
				t.Fatalf("known deny-all treated as unknown: %v", warnings)
			}
		})
	}
	if got := ManifestModelRegionParam("aws", KindThink, ProviderPipecat); got != "" {
		t.Fatalf("unsupported AWS region param suggested: %s", got)
	}
}

func TestManifestProviderWideApprovalKeepsOtherRestrictions(t *testing.T) {
	for _, role := range []string{"listen", "speak", "think"} {
		for _, allow := range []string{"", "      allow: []\n", "      allow: [approved]\n"} {
			manifest, err := packagespec.ParseManifest([]byte("manifest: acme\nversion: 1\nmodels:\n  " + role + ":\n    - provider: slng\n" + allow + "languages:\n  allow: [en]\n"))
			if err != nil {
				t.Fatal(err)
			}
			for _, provider := range []string{"slng", "deepgram"} {
				agent := &Agent{Manifest: manifest, Models: map[string]ModelDef{"test": {Kind: ModelKind(role), Provider: provider, Model: "future/model", Language: "en"}}}
				errs, _ := ValidateManifest(agent)
				permitted := allow == "" && provider == "slng"
				if (len(errs) == 0) != permitted {
					t.Fatalf("%s/%s/%q: %v", role, provider, allow, errs)
				}
				if permitted {
					agent.Manifest.Regions = &packagespec.ManifestRegions{Models: []packagespec.ManifestModelRegion{{Role: role, Provider: "slng", Allow: []string{}}}}
					errs, _ = ValidateManifest(agent)
					if !strings.Contains(strings.Join(errs, "\n"), "regions.models") {
						t.Fatal("all models bypassed region restriction")
					}
					agent.Manifest.Regions = nil
				}
				if permitted && role != "think" {
					agent.Models["test"] = ModelDef{Kind: ModelKind(role), Provider: provider, Model: "future/model", Language: "es"}
					errs, _ = ValidateManifest(agent)
					if !strings.Contains(strings.Join(errs, "\n"), "languages.allow") {
						t.Fatal("all models bypassed language restriction")
					}
				}
			}
		}
	}
}

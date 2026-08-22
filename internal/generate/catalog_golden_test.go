package generate

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

var updateCatalog = flag.Bool("update-catalog", false, "rewrite the catalogue resolution golden")

// TestCatalogResolutionGolden renders every catalogue entry through the real
// resolver with a synthetic binding and pins the result: class, ordered args,
// nested settings, import, install, env. It iterates DefaultCatalog, so a new
// entry automatically demands golden coverage (add the entry, run
// -update-catalog, eyeball the new block). Call-less matrix rows are listed
// too, so allowlist changes show up in the same diff.
func TestCatalogResolutionGolden(t *testing.T) {
	entries := append([]targetcap.Entry{}, defaultCatalog.Entries()...)
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Framework != b.Framework {
			return a.Framework < b.Framework
		}
		if a.Role != b.Role {
			return a.Role < b.Role
		}
		return a.Vendor < b.Vendor
	})

	var out strings.Builder
	for _, entry := range entries {
		fmt.Fprintf(&out, "=== %s %s %s ===\n", entry.Framework, entry.Role, entry.Vendor)
		if entry.Call == nil {
			out.WriteString("matrix row (no code injection)\n\n")
			continue
		}
		binding, vendorLabel := sampleBinding(entry)
		env := newEnvSet()
		// Language is per-model (N16) and gated where the entry has no slot;
		// exercise it only on entries that expose one.
		if (entry.Role == targetcap.Listen || entry.Role == targetcap.Speak) &&
			entry.Call != nil && entry.Call.Language.Arg != "" && !entry.Call.NoLanguage {
			binding.Language = "es-MX"
		}
		call, resolved, err := resolveService(entry.Framework, entry.Role, binding, env, sampleSlngSite(entry))
		if err != nil {
			t.Errorf("%s %s %s: resolve: %v", entry.Framework, entry.Role, entry.Vendor, err)
			continue
		}
		if resolved.Vendor != entry.Vendor {
			t.Errorf("%s %s %s: resolved through entry %q", entry.Framework, entry.Role, entry.Vendor, resolved.Vendor)
		}
		fmt.Fprintf(&out, "binding:  provider=%s%s\n", vendorLabel, describeBinding(binding))
		fmt.Fprintf(&out, "class:    %s\n", call.Class)
		fmt.Fprintf(&out, "args:     %s\n", joinKVs(call.Args))
		if len(call.SettingsArgs) > 0 {
			fmt.Fprintf(&out, "settings: %s\n", joinKVs(call.SettingsArgs))
		}
		if entry.Import != "" {
			fmt.Fprintf(&out, "import:   %s\n", entry.Import)
		}
		fmt.Fprintf(&out, "install:  %s\n", installLabel(entry))
		if envs := env.sorted(); len(envs) > 0 {
			fmt.Fprintf(&out, "env:      %s\n", strings.Join(envs, ", "))
		}
		out.WriteString("\n")
	}

	path := filepath.Join("testdata", "golden", "catalog_resolution.txt")
	if *updateCatalog {
		if err := os.WriteFile(path, []byte(out.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want) {
		t.Fatalf("catalogue resolution golden differs; run: go test ./internal/generate -run TestCatalogResolutionGolden -update-catalog")
	}
}

func TestLanguageLoweringUsesCataloguedSlot(t *testing.T) {
	for _, tc := range []struct {
		name      string
		framework targetcap.Provider
		role      targetcap.Role
		binding   ir.Binding
		agentLang string
		want      string
	}{
		{"pipecat settings", targetcap.Pipecat, targetcap.Listen, ir.Binding{Provider: "deepgram", Model: "nova-3"}, "es-MX", `"es-MX"`},
		{"livekit kwargs", targetcap.LiveKit, targetcap.Speak, ir.Binding{Provider: "slng", Model: "slng/deepgram/aura:2-en"}, "es-MX", `"es-MX"`},
		{"target override", targetcap.Pipecat, targetcap.Listen, ir.Binding{Provider: "deepgram", Model: "nova-3", Params: map[string]any{"language": "multi"}}, "es-MX", `"multi"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			binding := tc.binding
			binding.Language = tc.agentLang // per-model language (N16)
			call, _, err := resolveService(tc.framework, tc.role, binding, newEnvSet(), slngSite{})
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, kv := range append(call.Args, call.SettingsArgs...) {
				if kv.Key == "language" {
					count++
					if kv.Value != tc.want {
						t.Errorf("language = %s, want %s", kv.Value, tc.want)
					}
				}
			}
			if count != 1 {
				t.Fatalf("language kwargs = %d", count)
			}
		})
	}
}

// sampleBinding synthesizes the minimal binding that exercises an entry:
// slng models keep the route form to show the prefix transform, wildcards get
// an unlisted vendor (plus an endpoint where required).
// sampleSlngSite is the driver-supplied half of a router construction, spelled
// the way Pipecat spells it: a session-id argument, the call state, and one
// agent's own cache scope. A scope is not optional here even though it is a
// synthetic sample: a site with no scope would emit an empty header value, which
// the router rejects on the first turn of a call, and this golden is where a
// reader looks to see what a construction actually carries.
func sampleSlngSite(entry targetcap.Entry) slngSite {
	if entry.Vendor != "slng" || entry.Role != targetcap.Reason {
		return slngSite{}
	}
	return slngSite{
		SessionExpr: "slng_session_id", StateExpr: "state",
		Names: []string{"salon_name"}, ConfigFunc: slngConfigFunc("reasoning"),
		Scope: targetcap.SlngScope("catalog-sample-v1",
			targetcap.SlngSite{Kind: targetcap.SlngSiteAgent, Name: "concierge"}),
	}
}

func sampleBinding(entry targetcap.Entry) (ir.Binding, string) {
	binding := ir.Binding{Provider: entry.Vendor, Params: map[string]any{"sample_rate": 24000}}
	label := entry.Vendor
	if entry.Wildcard() {
		binding.Provider, label = "acme", `"*" as acme`
	}
	switch {
	case entry.Vendor == "slng" && entry.Role == targetcap.Speak:
		binding.Model = "slng/deepgram/aura:2-en"
	case entry.Vendor == "slng" && entry.Role == targetcap.Reason:
		// The router row needs the fields a router binding always carries: the
		// region it consumes into the base URL, the agent id, and the upstream
		// that serves the model. Without them this is not a legal binding, and
		// resolution refuses it the same way validate would.
		binding.Model = "gpt-5.6-luna"
		binding.AgentID = "catalog-sample-v1"
		binding.Upstream = &ir.Upstream{Provider: "openai"}
		binding.Params["world_part_override"] = "eu"
	case entry.Vendor == "slng":
		binding.Model = "slng/deepgram/nova:3"
	default:
		binding.Model = "model-1"
	}
	if entry.Call.Voice.Arg != "" {
		binding.Voice = "voice-1"
	}
	if entry.RequiresEndpoint {
		binding.EndpointEnv = "ACME_BASE_URL"
	}
	return binding, label
}

func describeBinding(binding ir.Binding) string {
	s := " model=" + binding.Model
	if binding.Voice != "" {
		s += " voice=" + binding.Voice
	}
	if binding.EndpointEnv != "" {
		s += " endpoint_env=" + binding.EndpointEnv
	}
	return s
}

func joinKVs(kvs []pyKV) string {
	parts := make([]string, len(kvs))
	for i, kv := range kvs {
		parts[i] = kv.Key + "=" + kv.Value
	}
	return strings.Join(parts, ", ")
}

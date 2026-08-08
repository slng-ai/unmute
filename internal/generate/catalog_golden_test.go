package generate

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/slng/unmute/internal/ir"
	targetcap "github.com/slng/unmute/internal/target"
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
		envRef := pipecatEnvRef
		if entry.Framework == targetcap.LiveKit {
			envRef = livekitEnvRef
		}
		env := newEnvSet()
		// Language is per-model (N16) and gated where the entry has no slot;
		// exercise it only on entries that expose one.
		if (entry.Role == targetcap.Listen || entry.Role == targetcap.Speak) &&
			entry.Call != nil && entry.Call.Language.Arg != "" && !entry.Call.NoLanguage {
			binding.Language = "es-MX"
		}
		call, resolved, err := resolveService(defaultCatalog, entry.Framework, entry.Role, binding, envRef, env)
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
		// Declared generation-param slots (N20), so changing one shows up in a
		// reviewable diff rather than only in a behaviour change.
		if slots := generationSlots(entry); slots != "" {
			fmt.Fprintf(&out, "gen:      %s\n", slots)
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
			envRef := pipecatEnvRef
			if tc.framework == targetcap.LiveKit {
				envRef = livekitEnvRef
			}
			binding := tc.binding
			binding.Language = tc.agentLang // per-model language (N16)
			call, _, err := resolveService(defaultCatalog, tc.framework, tc.role, binding, envRef, newEnvSet())
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

// TestGenerationParamLoweringUsesCataloguedSlot is the generate half of N20:
// resolveService lowers a generation param through the entry's own kwarg and
// refuses one with no slot, using the same Catalog.LowerParams that ir.Validate
// calls. A green validate therefore cannot fail here.
func TestGenerationParamLoweringUsesCataloguedSlot(t *testing.T) {
	for _, tc := range []struct {
		name      string
		framework targetcap.Provider
		role      targetcap.Role
		binding   ir.Binding
		wantKV    pyKV   // expected emitted kwarg
		wantErr   string // substring; set instead of wantKV
	}{
		{
			name: "pipecat renames speed", framework: targetcap.Pipecat, role: targetcap.Speak,
			binding: typedBinding(ir.Binding{Provider: "rime", Model: "mistv2", Voice: "cove"}, map[string]any{"speed": 1.2}),
			wantKV:  pyKV{Key: "speedAlpha", Value: "1.2"},
		},
		{
			name: "livekit renames speed", framework: targetcap.LiveKit, role: targetcap.Speak,
			binding: typedBinding(ir.Binding{Provider: "rime", Model: "mistv2", Voice: "cove"}, map[string]any{"speed": 1.2}),
			wantKV:  pyKV{Key: "speed_alpha", Value: "1.2"},
		},
		{
			name: "temperature keeps its name", framework: targetcap.Pipecat, role: targetcap.Reason,
			binding: typedBinding(ir.Binding{Provider: "openai", Model: "gpt-4o-mini"}, map[string]any{"temperature": 0.4}),
			wantKV:  pyKV{Key: "temperature", Value: "0.4"},
		},
		{
			// The author's own key keeps its spelling even where a typed field
			// would have been renamed: params is forwarded verbatim (D2).
			name: "author speed is emitted as written", framework: targetcap.Pipecat, role: targetcap.Speak,
			binding: ir.Binding{Provider: "rime", Model: "mistv2", Voice: "cove", Params: map[string]any{"speed": 0.9}},
			wantKV:  pyKV{Key: "speed", Value: "0.9"},
		},
		{
			name: "typed field with no slot is an error", framework: targetcap.Pipecat, role: targetcap.Speak,
			binding: typedBinding(ir.Binding{Provider: "cartesia", Model: "sonic-3", Voice: "voice-1"}, map[string]any{"speed": 1.1}),
			wantErr: "speed has no slot here",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			envRef := pipecatEnvRef
			if tc.framework == targetcap.LiveKit {
				envRef = livekitEnvRef
			}
			call, _, err := resolveService(defaultCatalog, tc.framework, tc.role, tc.binding, envRef, newEnvSet())
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, kv := range append(call.Args, call.SettingsArgs...) {
				if kv.Key == tc.wantKV.Key {
					if kv.Value != tc.wantKV.Value {
						t.Fatalf("%s = %s, want %s", kv.Key, kv.Value, tc.wantKV.Value)
					}
					return
				}
			}
			t.Fatalf("no %q kwarg emitted; args=%s settings=%s", tc.wantKV.Key, joinKVs(call.Args), joinKVs(call.SettingsArgs))
		})
	}
}

// typedBinding puts params on a binding as if Build had folded them from the
// typed generation fields, which is what makes them slot-checked rather than
// forwarded verbatim.
func typedBinding(binding ir.Binding, params map[string]any) ir.Binding {
	binding.Params = params
	binding.Generation = params
	return binding
}

// sampleBinding synthesizes the minimal binding that exercises an entry:
// slng models keep the route form to show the prefix transform, wildcards get
// an unlisted vendor (plus an endpoint where required).
func sampleBinding(entry targetcap.Entry) (ir.Binding, string) {
	binding := ir.Binding{Provider: entry.Vendor, Params: map[string]any{"sample_rate": 24000}}
	label := entry.Vendor
	if entry.Wildcard() {
		binding.Provider, label = "acme", `"*" as acme`
	}
	switch {
	case entry.Vendor == "slng" && entry.Role == targetcap.Speak:
		binding.Model = "slng/deepgram/aura:2-en"
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

// generationSlots renders an entry's declared generation-param slots as
// "unmute name -> vendor kwarg" pairs, empty when it declares none.
// GenerationParams is already sorted, so iterating it needs no second sort.
func generationSlots(entry targetcap.Entry) string {
	slots := entry.Call.ParamSlots()
	var parts []string
	for _, name := range targetcap.GenerationParams {
		if arg, ok := slots[name]; ok {
			parts = append(parts, name+"="+arg)
		}
	}
	return strings.Join(parts, ", ")
}

func joinKVs(kvs []pyKV) string {
	parts := make([]string, len(kvs))
	for i, kv := range kvs {
		parts[i] = kv.Key + "=" + kv.Value
	}
	return strings.Join(parts, ", ")
}

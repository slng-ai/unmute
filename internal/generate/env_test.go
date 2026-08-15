package generate

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// exampleArtifact compiles one shipped example for one provider.
func exampleArtifact(t *testing.T, example string, provider ir.Provider) Artifact {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", example))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
	if err != nil {
		t.Fatalf("%s on %s: %v", example, provider, err)
	}
	return artifact
}

// envFileNames lists the names an env file asks the reader for. A commented-out
// name is still the file mentioning it, and this file's whole job is to be a
// to-do list whose every line is a to-do, so both forms count.
func envFileNames(content string) []string {
	name := regexp.MustCompile(`(?m)^#?\s*([A-Z][A-Z0-9_]*)=`)
	var names []string
	for _, hit := range name.FindAllStringSubmatch(content, -1) {
		names = append(names, hit[1])
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// readmeEnvNames lists the names the emitted README asks the reader to set.
func readmeEnvNames(content string) []string {
	item := regexp.MustCompile("(?m)^- `([A-Z][A-Z0-9_]*)`")
	var names []string
	for _, hit := range item.FindAllStringSubmatch(content, -1) {
		names = append(names, hit[1])
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// TestEnvExampleListsOnlyAuthorNames holds FR-018: `build/<target>/.env.example`
// contains only names the author supplies. Everything else is absent, not
// relabelled and not commented out.
//
// The classification already exists. internal/target/telephony.go carries
// LocallySuppliedEnvironment per route, cloned into the IR as LocalEnvironment,
// and three things ignore it: the LiveKit template labels rather than excludes,
// the Pipecat template does not read it at all, and UNMUTE_PUBLIC_URL and
// UNMUTE_OUTBOUND_TOKEN are missing from the data even though `unmute dev` mints
// both. So the same REDIS_URL is explained on one target and silently demanded
// on the other, from one piece of data (research D11).
func TestEnvExampleListsOnlyAuthorNames(t *testing.T) {
	authorSet := []string{
		"BILLING_PHONE_NUMBER", "OPENAI_API_KEY", "SIP_AUTH_PASSWORD", "SIP_AUTH_USERNAME",
		"SIP_FROM_NUMBER", "SIP_TRUNK_HOSTNAME", "SLNG_API_KEY", "SUPERVISOR_PHONE_NUMBER",
	}
	t.Run("livekit sip names exactly the eight the author sets", func(t *testing.T) {
		env := artifactFile(t, exampleArtifact(t, "livekit-human-transfer", ir.ProviderLiveKit), ".env.example")
		if got := envFileNames(env); !slices.Equal(got, authorSet) {
			t.Errorf(".env.example names %v, want exactly %v", got, authorSet)
		}
	})

	// Both drivers read one piece of data, so both must reach the same answer.
	// outbound-reminder is the sharpest fixture: it declares both targets, and
	// on Pipecat it is the carrier-websocket route whose REDIS_URL is supplied by
	// the Compose graph and by nothing the author writes.
	for _, provider := range []ir.Provider{ir.ProviderPipecat, ir.ProviderLiveKit} {
		t.Run("no locally supplied name survives on "+string(provider), func(t *testing.T) {
			env := artifactFile(t, exampleArtifact(t, "outbound-reminder", provider), ".env.example")
			for _, hidden := range []string{"REDIS_URL", "UNMUTE_PUBLIC_URL", "UNMUTE_OUTBOUND_TOKEN", "LIVEKIT_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET"} {
				if strings.Contains(env, hidden) {
					t.Errorf("%s asks for %s, which `unmute dev` sets locally and the platform sets at deploy time:\n%s", provider, hidden, env)
				}
			}
			if len(envFileNames(env)) == 0 {
				t.Error("the file names nothing at all, so this test would pass for the wrong reason")
			}
		})
	}

	// T013b: the README list and the env file are two views of one fact.
	t.Run("the README set-these list matches the env file", func(t *testing.T) {
		artifact := exampleArtifact(t, "outbound-reminder", ir.ProviderPipecat)
		env := envFileNames(artifactFile(t, artifact, ".env.example"))
		readme := readmeEnvNames(artifactFile(t, artifact, "README.md"))
		if !slices.Equal(env, readme) {
			t.Errorf("the env file asks for %v and the README asks for %v; they are two views of one fact", env, readme)
		}
	})

	// T013c: hiding is not deleting. The machine-readable form stays complete, so
	// an operator deploying by hand can still recover every name (FR-018e).
	t.Run("compile-report keeps every hidden name", func(t *testing.T) {
		artifact := exampleArtifact(t, "outbound-reminder", ir.ProviderPipecat)
		var report struct {
			RequiredEnv []string `json:"required_env"`
		}
		if err := json.Unmarshal([]byte(artifactFile(t, artifact, "compile-report.json")), &report); err != nil {
			t.Fatal(err)
		}
		for _, hidden := range []string{"REDIS_URL", "UNMUTE_PUBLIC_URL", "UNMUTE_OUTBOUND_TOKEN"} {
			if !slices.Contains(report.RequiredEnv, hidden) {
				t.Errorf("compile-report.json dropped %s; hiding a name from the env file must not delete it: %v", hidden, report.RequiredEnv)
			}
		}
	})
}

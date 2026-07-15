package generate

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/spec"
	"github.com/slng/unmute/internal/target"
)

var updateElevenLabs = flag.Bool("update-elevenlabs", false, "rewrite the elevenlabs golden")

// TestElevenLabsGolden emits the safe_core package to elevenlabs and locks the
// full branch-aware ApplyPlan (driver-elevenlabs T7, V10). Zero Python.
func TestElevenLabsGolden(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderElevenLabs), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if artifact.Kind != ManagedTarget || artifact.Apply == nil {
		t.Fatalf("kind=%q apply=%v", artifact.Kind, artifact.Apply)
	}
	assertGolden(t, "elevenlabs.txt", renderApply(artifact), updateElevenLabs, "TestElevenLabsGolden", "update-elevenlabs")
}

func renderApply(a Artifact) string {
	var out strings.Builder
	out.WriteString("credential_env: " + a.Apply.CredentialEnv + "\n")
	for _, note := range a.Notes.Notes {
		out.WriteString("note: " + note + "\n")
	}
	for i, step := range a.Apply.Steps {
		out.WriteString("\n=== step ")
		out.WriteByte(byte('1' + i))
		out.WriteString(": " + step.Method + " " + step.Endpoint)
		if step.Branch != "" {
			out.WriteString(" [branch=" + step.Branch + "]")
		}
		if step.CaptureID != "" {
			out.WriteString(" (capture=" + step.CaptureID + ")")
		}
		out.WriteString(" ===\n")
		out.Write(step.Body)
		out.WriteByte('\n')
	}
	return out.String()
}

func assertGolden(t *testing.T, name, got string, update *bool, testName, flagName string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("%s golden differs; run: go test ./internal/generate -run %s -%s", name, testName, flagName)
	}
}

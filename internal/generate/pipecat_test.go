package generate

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng/unmute/internal/ir"
	spec "github.com/slng/unmute/internal/legacyspec"
	"github.com/slng/unmute/internal/scaffold"
)

var update = flag.Bool("update", false, "rewrite golden files")

func TestGeneratePipecat_golden(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "support-bot")
	if _, err := scaffold.Write(dir, scaffold.Data{Name: "support-bot"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env.local"), []byte("SLNG_API_KEY=sk-secret\nOPENAI_API_KEY=sk-openai\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	input := loadPipecatInput(t, dir)

	result, err := GeneratePipecat(input)
	if err != nil {
		t.Fatal(err)
	}
	got := manifest(result.Artifacts)
	for _, forbidden := range []string{"sk-secret", "sk-openai"} {
		if strings.Contains(string(got), forbidden) {
			t.Fatalf("generated artifacts leaked secret value %q", forbidden)
		}
	}
	if len(result.Warnings) != 1 || result.OmittedTools[0] != "lookup_order" {
		t.Fatalf("warnings = %v omitted = %v", result.Warnings, result.OmittedTools)
	}

	golden := "testdata/golden/pipecat.txt"
	if *update {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("missing golden; run: go test ./internal/generate -update")
	}
	if !bytes.Equal(got, want) {
		t.Error("pipecat golden drift; run: go test ./internal/generate -update")
	}
}

func TestGeneratePipecat_rejectsNonOpenAILLM(t *testing.T) {
	input := PipecatInput{
		AgentName: "x",
		LLM: ir.LLMModelConfig{
			Model: "anthropic/claude",
		},
	}
	_, err := GeneratePipecat(input)
	if err == nil {
		t.Fatal("expected unsupported llm error")
	}
	if !strings.Contains(err.Error(), "only openai/ routes") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func loadPipecatInput(t *testing.T, dir string) PipecatInput {
	t.Helper()
	agent, err := spec.LoadAgentConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	stt, llm, tts, err := spec.LoadModels(dir)
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := spec.LoadEnvSecrets(dir)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := spec.LoadPipecatTargetProfile(dir)
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := spec.ComposePrompt(dir)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := spec.LoadTools(dir)
	if err != nil {
		t.Fatal(err)
	}
	return PipecatInput{
		AgentName: filepath.Base(dir),
		Prompt:    prompt,
		Agent:     agent,
		STT:       stt,
		LLM:       llm,
		TTS:       tts,
		Secrets:   secrets,
		Profile:   profile,
		Tools:     tools,
	}
}

func manifest(artifacts []File) []byte {
	var b bytes.Buffer
	for _, artifact := range artifacts {
		b.WriteString("=== " + artifact.Path + " ===\n")
		b.Write(artifact.Content)
		b.WriteByte('\n')
	}
	return b.Bytes()
}

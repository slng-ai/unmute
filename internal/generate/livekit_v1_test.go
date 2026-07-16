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

var updateLiveKitV1 = flag.Bool("update-livekit", false, "rewrite the livekit v1 golden")

// TestLiveKitV1RemyGolden emits the Remy example (agent handoff + task groups +
// the SLNG plugin) to LiveKit and compares the full file set byte-for-byte
// (driver-livekit T8/T9/T10, V11/V12). Zero Python.
func TestLiveKitV1RemyGolden(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	var out strings.Builder
	for _, file := range artifact.Files {
		out.WriteString("=== " + file.Path + " ===\n")
		out.Write(file.Content)
		if !strings.HasSuffix(string(file.Content), "\n") {
			out.WriteByte('\n')
		}
	}

	path := filepath.Join("testdata", "golden", "livekit_v1_remy.txt")
	if *updateLiveKitV1 {
		if err := os.WriteFile(path, []byte(out.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want) {
		t.Fatalf("livekit v1 golden differs; run: go test ./internal/generate -run TestLiveKitV1RemyGolden -update-livekit")
	}
}

// TestLiveKitV1EmitsSlngPlugin asserts the scaffold example (Remy, all-SLNG
// bindings) emits the SLNG plugin and LiveKit Inference: the first generation
// stays a real SLNG agent (driver-livekit V12). Since the C8 amendment SLNG is
// the default, not the only route; TestLiveKitV1MultiVendor covers the rest.
func TestLiveKitV1EmitsSlngPlugin(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		"from livekit.plugins import silero, slng",
		"slng.STT(",
		"slng.TTS(",
		"inference.LLM(",
		"from livekit.agents.beta.workflows import TaskGroup",
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
}

// TestLiveKitV1MultiVendor proves the catalogue path end to end: the safe_core
// livekit target binds Deepgram listen and ElevenLabs speak (per-vendor
// plugins), one voice is rebound to Cartesia in-code, and the emitted project
// carries the right constructors, merged plugin import, extras dep, and env.
func TestLiveKitV1MultiVendor(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	// Provider resolution is the subject here; interruption shaping, agent
	// tools, and human transfer are separate livekit maturity gates.
	agent.Conversation.Interruption = nil
	for name, def := range agent.Agents {
		var kept []string
		for _, ref := range def.Tools {
			if _, ok := agent.Controls[ref].(*ir.AgentTransfer); ok {
				kept = append(kept, ref)
			}
		}
		def.Tools = kept
		agent.Agents[name] = def
	}
	tgt := targetByProvider(t, agent, ir.ProviderLiveKit)
	tgt.Models.Speak["specialist"] = ir.Binding{
		Provider: "cartesia", Model: "sonic-3", Voice: "f786b574-daa5-4673-aa0c-cbe3e8534c02",
	}
	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		"from livekit.plugins import cartesia, deepgram, elevenlabs, silero",
		`stt=deepgram.STT(api_key=os.environ.get("DEEPGRAM_API_KEY"), model="nova-3")`,
		`tts=elevenlabs.TTS(api_key=os.environ.get("ELEVEN_API_KEY"), voice_id="cgSgspJ2msm6clMCkdW9")`,
		`tts=cartesia.TTS(api_key=os.environ.get("CARTESIA_API_KEY"), voice="f786b574-daa5-4673-aa0c-cbe3e8534c02", model="sonic-3")`,
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
	pyproject := artifactFile(t, artifact, "pyproject.toml")
	if !strings.Contains(pyproject, `"livekit-agents[cartesia,deepgram,elevenlabs]>=1.5"`) {
		t.Errorf("pyproject.toml missing merged extras dep:\n%s", pyproject)
	}
	if strings.Contains(pyproject, "livekit-plugins-slng") {
		t.Error("pyproject.toml pulls the slng plugin without an slng binding")
	}
}

// TestLiveKitV1UnknownVendorFailsWithMatrix asserts the no-slot diagnostic
// quotes the support matrix instead of guessing a substitute service.
func TestLiveKitV1UnknownVendorFailsWithMatrix(t *testing.T) {
	env := newEnvSet()
	_, err := livekitSTTService(&ir.Binding{Provider: "acme", Model: "m"}, env)
	if err == nil || !strings.Contains(err.Error(), "listen providers on livekit: deepgram, slng") {
		t.Fatalf("want a matrix-quoting error, got %v", err)
	}
}

func artifactFile(t *testing.T, artifact Artifact, path string) string {
	t.Helper()
	for _, file := range artifact.Files {
		if file.Path == path {
			return string(file.Content)
		}
	}
	t.Fatalf("%s not emitted", path)
	return ""
}

// TestCheckLiveKitVersion pins the template-compatible range (>=1.5, <2.0):
// beta.workflows TaskGroup + AgentTask + inference are present from 1.5.x.
func TestCheckLiveKitVersion(t *testing.T) {
	for _, tc := range []struct {
		version string
		ok      bool
	}{
		{"1.5.2", true},
		{"1.5", true},
		{"1.6.0", true},
		{"1.2", false},
		{"1.4.9", false},
		{"0.0.108", false},
		{"2.0.0", false},
		{"", false},
		{"latest", false},
	} {
		err := checkLiveKitVersion(tc.version)
		if (err == nil) != tc.ok {
			t.Errorf("checkLiveKitVersion(%q): ok=%v, err=%v", tc.version, tc.ok, err)
		}
	}
}

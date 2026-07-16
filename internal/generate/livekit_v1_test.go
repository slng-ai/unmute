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

// TestLiveKitV1DelegateThenTransferAndEnd covers the two non-return `then`
// lowerings (SCHEMA §4.7, N13): the delegate must not return to the owner, so it
// emits a handoff (transfer) or session shutdown (end) instead of the typed
// results, and its tool description must say control does not come back. Reuses
// the Remy package and rewrites its two groups' `then` in-memory.
func TestLiveKitV1DelegateThenTransferAndEnd(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	// do_reserve -> reserve_group (transfer to the greeter); do_event -> events_group (end).
	reserve := agent.TaskGroups["reserve_group"]
	reserve.Then, reserve.ThenTarget = ir.GroupTransfer, "greeter"
	agent.TaskGroups["reserve_group"] = reserve
	events := agent.TaskGroups["events_group"]
	events.Then, events.ThenTarget = ir.GroupEnd, ""
	agent.TaskGroups["events_group"] = events

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var botpy string
	for _, file := range artifact.Files {
		if file.Path == "agent.py" {
			botpy = string(file.Content)
		}
	}
	if botpy == "" {
		t.Fatal("agent.py not emitted")
	}

	for _, want := range []string{
		// transfer: hands off to the target, does not return; no typed-result return.
		"async def do_reserve(self, ctx: RunContext):",
		"return Greeter(chat_ctx=owner_ctx)",
		"when it finishes the caller is handed to the greeter.",
		// end: shuts the session down, does not return.
		"self.session.shutdown()",
		"when it finishes the call ends.",
		"does not return to you",
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
	// Neither non-return path may hand back the typed results (N13/§4.7), and a
	// transfer/end delegate is not typed `-> dict`.
	for _, forbidden := range []string{"return result.task_results", "async def do_reserve(self, ctx: RunContext) -> dict:"} {
		if strings.Contains(botpy, forbidden) {
			t.Errorf("agent.py must not contain %q for a non-return delegate", forbidden)
		}
	}
}

// TestLiveKitPluginCatalogCoverage guards the native provider catalogues (V13):
// every provider the driver advertises must have a well-formed entry (module,
// callable, api-key env, package) after normalisation, or generate emits a
// broken agent.py. Mirrors pipecat's TestServiceInfoCoversEveryMappedClass.
func TestLiveKitPluginCatalogCoverage(t *testing.T) {
	for _, tc := range []struct {
		name  string
		table map[string]lkPlugin
		class string
		want  []string
	}{
		{"stt", livekitSTTPlugins, "STT", []string{"assemblyai", "cartesia", "deepgram", "elevenlabs", "gradium", "sarvam", "soniox", "speechmatics"}},
		{"tts", livekitTTSPlugins, "TTS", []string{"cartesia", "deepgram", "elevenlabs", "gemini", "google", "gradium", "inworld", "rime", "sarvam", "soniox", "speechmatics"}},
		{"llm", livekitLLMPlugins, "LLM", []string{"anthropic", "aws", "azure", "groq", "mistralai", "openai-compatible", "openrouter", "sarvam"}},
	} {
		for _, key := range tc.want {
			p, ok := tc.table[key]
			if !ok {
				t.Errorf("%s catalogue missing provider %q", tc.name, key)
				continue
			}
			n := p.norm(tc.class)
			if n.Module == "" {
				t.Errorf("%s[%q]: empty module", tc.name, key)
			}
			if n.Callable == "" {
				t.Errorf("%s[%q]: empty callable", tc.name, key)
			}
			if len(n.Env) == 0 {
				t.Errorf("%s[%q]: no api-key env", tc.name, key)
			}
			for _, ev := range n.Env {
				if ev == "" {
					t.Errorf("%s[%q]: empty env var", tc.name, key)
				}
			}
		}
	}
}

// TestLiveKitPluginCtorShapes pins the non-obvious constructor branches: soniox
// nests model in STTOptions, speechmatics takes no model, elevenlabs/gradium use
// model_id/model_name, gemini is google.beta.GeminiTTS, openai-compatible adds
// base_url, and the OpenAI-hub vendors use with_<vendor>. Facts per the live
// docs (2026-07-16); a drift here emits wrong Python that only L4 smoke catches.
func TestLiveKitPluginCtorShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  string
		want string
	}{
		{"soniox-stt-nested", livekitSTTPlugins["soniox"].norm("STT").call("stt-rt-v4", "", nil, ""), `soniox.STT(params=soniox.STTOptions(model="stt-rt-v4"))`},
		{"speechmatics-stt-nomodel", livekitSTTPlugins["speechmatics"].norm("STT").call("ignored", "", nil, ""), `speechmatics.STT()`},
		{"elevenlabs-stt-modelid", livekitSTTPlugins["elevenlabs"].norm("STT").call("scribe_v2_realtime", "", nil, ""), `elevenlabs.STT(model_id="scribe_v2_realtime")`},
		{"speechmatics-tts-voice", livekitTTSPlugins["speechmatics"].norm("TTS").call("ignored", "sarah", nil, ""), `speechmatics.TTS(voice="sarah")`},
		{"deepgram-tts-novoice", livekitTTSPlugins["deepgram"].norm("TTS").call("aura-2-asteria-en", "dropped", nil, ""), `deepgram.TTS(model="aura-2-asteria-en")`},
		{"gemini-tts-class", livekitTTSPlugins["gemini"].norm("TTS").call("gemini-3-flash-tts", "Zephyr", nil, ""), `google.beta.GeminiTTS(model="gemini-3-flash-tts", voice_name="Zephyr")`},
		{"gradium-tts-kwargs", livekitTTSPlugins["gradium"].norm("TTS").call("default", "YTpq7expH9539ERJ", nil, ""), `gradium.TTS(model_name="default", voice_id="YTpq7expH9539ERJ")`},
		{"openai-compatible-baseurl", livekitLLMPlugins["openai-compatible"].norm("LLM").call("my-model", "", nil, "LLM_BASE_URL"), `openai.LLM(model="my-model", base_url=os.environ.get("LLM_BASE_URL"))`},
		{"openrouter-with", livekitLLMPlugins["openrouter"].norm("LLM").call("anthropic/claude-sonnet-4.5", "", nil, ""), `openai.LLM.with_openrouter(model="anthropic/claude-sonnet-4.5")`},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

// TestLiveKitV1NativePlugins proves the native per-vendor path (C8/V11 amended
// 2026-07-16): rebinding Remy's roles to deepgram/cartesia/anthropic emits their
// livekit.plugins imports, constructors, pinned deps, and api-key env — and drops
// the slng plugin when no slng binding remains.
func TestLiveKitV1NativePlugins(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	tgt := targetByProvider(t, agent, ir.ProviderLiveKit)
	tgt.Models.Listen = &ir.Binding{Provider: "deepgram", Model: "nova-3", Params: map[string]any{"language": "en"}}
	for name, b := range tgt.Models.Speak {
		b.Provider, b.Model, b.Voice, b.VoiceID = "cartesia", "sonic-3", "f786b574-daa5-4673-aa0c-cbe3e8534c02", ""
		tgt.Models.Speak[name] = b
	}
	for name, b := range tgt.Models.Reason {
		b.Provider, b.Model, b.Params = "anthropic", "claude-sonnet-4-6", nil
		tgt.Models.Reason[name] = b
	}

	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	files := map[string]string{}
	for _, f := range artifact.Files {
		files[f.Path] = string(f.Content)
	}

	agentpy := files["agent.py"]
	for _, want := range []string{
		"from livekit.plugins import anthropic, cartesia, deepgram, silero",
		`stt=deepgram.STT(model="nova-3", language="en")`,
		`cartesia.TTS(model="sonic-3", voice="f786b574-daa5-4673-aa0c-cbe3e8534c02")`,
		`llm=anthropic.LLM(model="claude-sonnet-4-6")`,
	} {
		if !strings.Contains(agentpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
	for _, forbidden := range []string{"slng.STT(", "slng.TTS(", "inference.LLM("} {
		if strings.Contains(agentpy, forbidden) {
			t.Errorf("agent.py still routes through %q after native rebind", forbidden)
		}
	}
	for _, want := range []string{"livekit-plugins-deepgram>=1.5", "livekit-plugins-cartesia>=1.5", "livekit-plugins-anthropic>=1.5"} {
		if !strings.Contains(files["pyproject.toml"], want) {
			t.Errorf("pyproject.toml missing %q", want)
		}
	}
	if strings.Contains(files["pyproject.toml"], "livekit-plugins-slng") {
		t.Errorf("pyproject.toml should not pin slng after native rebind")
	}
	for _, want := range []string{"DEEPGRAM_API_KEY=", "CARTESIA_API_KEY=", "ANTHROPIC_API_KEY="} {
		if !strings.Contains(files[".env.example"], want) {
			t.Errorf(".env.example missing %q", want)
		}
	}
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

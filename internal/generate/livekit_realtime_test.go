package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

// realtimeArtifact compiles the realtime fixture to LiveKit, after the mutation
// a case needs. The mutation takes the resolved target as well as the agent,
// because turn_detection and the speak binding live on the target's bindings.
func realtimeArtifact(t *testing.T, mutate func(*ir.Agent, *ir.Target)) Artifact {
	t.Helper()
	agent := agentFor(t, "realtime_model")
	tgt := targetByProvider(t, agent, ir.ProviderLiveKit)
	if mutate != nil {
		mutate(agent, &tgt)
	}
	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

// setTurnDetection writes one value onto the fixture's realtime binding. Every
// case below is this package with one field changed, so a difference in the
// emitted module is that field's doing and nothing else's.
func setTurnDetection(value string) func(*ir.Agent, *ir.Target) {
	return func(_ *ir.Agent, tgt *ir.Target) {
		binding := tgt.Models.Realtime["voice"]
		binding.TurnDetection = value
		tgt.Models.Realtime["voice"] = binding
	}
}

// bindHalfCascadeVoice takes the voice off the model and gives the agent a
// synthesizer instead, which is exactly what an author writes for the half
// cascade: `speak:` on the agent and no `voice:` on the entry.
func bindHalfCascadeVoice(agent *ir.Agent, tgt *ir.Target) {
	binding := tgt.Models.Realtime["voice"]
	binding.Voice = ""
	tgt.Models.Realtime["voice"] = binding

	def := agent.Agents["desk"]
	def.Voice = "house"
	agent.Agents["desk"] = def
	if tgt.Models.Speak == nil {
		tgt.Models.Speak = map[string]ir.Binding{}
	}
	tgt.Models.Speak["house"] = ir.Binding{Provider: "cartesia", Model: "sonic-2", Voice: "a-voice-id"}
}

// realtimeModule compiles the realtime fixture, optionally after the mutation a
// case needs, and hands back the emitted agent.py with its comments stripped.
//
// codeOnly (internal/generate/livekit_live_test.go) is not caution for its own
// sake here: the emitted session carries a comment naming the two arguments it
// deliberately does not pass, so a plain substring search for `turn_detection=`
// finds the comment and reports the absence as a presence.
func realtimeModule(t *testing.T, mutate func(*ir.Agent, *ir.Target)) string {
	t.Helper()
	return codeOnly(artifactFile(t, realtimeArtifact(t, mutate), "agent.py"))
}

// TestLiveKitRealtimeEmitsOneModelAndNothingItReplaces is the spec 024 contract
// read off the emitted module: FR-002, and the half of FR-005/FR-004 that says
// what a realtime session is not given.
func TestLiveKitRealtimeEmitsOneModelAndNothingItReplaces(t *testing.T) {
	code := realtimeModule(t, nil)

	for _, want := range []string{
		"from livekit.plugins.openai.realtime import RealtimeModel",
		"llm=RealtimeModel(",
		`api_key=os.environ["OPENAI_API_KEY"]`,
		`model="gpt-realtime"`,
		`voice="marin"`,
		// Tools run natively over the same socket, so they are registered the
		// way a cascaded package registers them. There is no backend here.
		"async def lookup_customer(",
		"async def check_availability(",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("agent.py is missing %q", want)
		}
	}

	// Each absence is something a reader would reasonably expect to find, and
	// each would do damage rather than nothing.
	for _, absent := range []string{
		// FR-002: no transcriber and no reasoning model are built.
		"stt=", "sttexpr", "llm.FallbackAdapter", "inference.LLM(",
		// The model speaks in its own voice on this fixture, so a synthesizer
		// would give the call two voices.
		"tts=",
		// temperature is accepted by the constructor and silently discarded
		// ("deprecated, unused in v1", realtime_model.py:382), so emitting it
		// would look like a setting and be none.
		"temperature=",
		// api_version flips the client into Azure mode and then demands an
		// Azure endpoint (realtime_model.py:466-476).
		"api_version=",
		// modalities is only written for the half cascade: ["text","audio"] is
		// the constructor's own default and ["audio"] is identical on the wire.
		"modalities=",
	} {
		if strings.Contains(code, absent) {
			t.Errorf("agent.py carries %q, which architecture: realtime replaces or must never send", absent)
		}
	}
}

// TestLiveKitRealtimeLocalTurnPassesNoTurnDetection is FR-004, and it is the one
// lowering in this tree whose correct emission is silence.
//
// Passing any turn_detection argument, including one identical to the vendor's
// own default, sets can_disable_turn_detection=False
// (realtime_model.py:484, livekit-plugins-openai 1.8.1) and the framework can no
// longer take the turn decision back. The detector and the VAD below are the
// other half of the same requirement: agent_activity.py:383-392 switches the
// model's own detection off only when a VAD is loaded AND the session was given
// an explicit client-side turn detector.
func TestLiveKitRealtimeLocalTurnPassesNoTurnDetection(t *testing.T) {
	code := realtimeModule(t, nil) // the fixture writes turn_detection: local

	if strings.Contains(code, "turn_detection=openai_realtime") {
		t.Error("agent.py passes a turn-detection argument for turn_detection: local, which costs the framework the right to decide the turn")
	}
	if strings.Contains(code, "openai.types.realtime") {
		t.Error("agent.py imports the vendor's turn-detection types for a package that sends none")
	}
	for _, want := range []string{
		"turn_handling=TurnHandlingOptions(",
		"turn_detection=inference.TurnDetector(",
		`vad=ctx.proc.userdata["vad"]`,
		"silero.VAD.load(",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("agent.py is missing %q, without which turn_detection: local compiles clean and changes nothing on the call", want)
		}
	}
}

// TestLiveKitRealtimeSendsTheVendorDetectorItWasAskedFor covers the other two
// values and the omitted one (FR-003, FR-005). Each is the same package with one
// field changed, so a difference in the emitted module is that field's doing.
func TestLiveKitRealtimeSendsTheVendorDetectorItWasAskedFor(t *testing.T) {
	for _, tc := range []struct {
		name    string
		value   string
		want    string
		wantImp string
	}{
		{"server_vad", "server_vad", `turn_detection=openai_realtime.ServerVad(type="server_vad")`, "from openai.types.realtime import realtime_audio_input_turn_detection as openai_realtime"},
		{"semantic", "semantic", `turn_detection=openai_realtime.SemanticVad(type="semantic_vad")`, "from openai.types.realtime import realtime_audio_input_turn_detection as openai_realtime"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code := realtimeModule(t, setTurnDetection(tc.value))
			if !strings.Contains(code, tc.want) {
				t.Errorf("agent.py is missing %q", tc.want)
			}
			if !strings.Contains(code, tc.wantImp) {
				t.Errorf("agent.py is missing the import %q the expression names", tc.wantImp)
			}
			// The model decides the turn, so nothing of ours may claim it.
			for _, absent := range []string{"turn_handling=", "vad=", "silero", "inference.TurnDetector"} {
				if strings.Contains(code, absent) {
					t.Errorf("agent.py carries %q while the model decides the turn", absent)
				}
			}
		})
	}

	t.Run("omitted", func(t *testing.T) {
		code := realtimeModule(t, setTurnDetection(""))
		if strings.Contains(code, "turn_detection=openai_realtime") {
			t.Error("agent.py sends a turn-detection argument for a package that wrote none; the vendor's own default must stand")
		}
		if strings.Contains(code, "silero") {
			t.Error("agent.py loads a voice activity detector for a package whose model decides the turn")
		}
	})
}

// TestLiveKitRealtimeHalfCascadeAsksForTextAndBuildsAVoice is FR-006, user story
// 2. A reply carrying no audio modality is routed through tts_node
// (agent_activity.py:4180, livekit-agents 1.8.1), and LiveKit logs an error at
// agent_activity.py:1139-1144 when audio output is on with neither an audio
// modality nor a TTS, so this pair is emitted together or not at all.
func TestLiveKitRealtimeHalfCascadeAsksForTextAndBuildsAVoice(t *testing.T) {
	code := realtimeModule(t, bindHalfCascadeVoice)
	for _, want := range []string{
		`modalities=["text"]`,
		"tts=cartesia.TTS(",
		"from livekit.plugins import cartesia",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("agent.py is missing %q", want)
		}
	}
	if strings.Contains(code, `voice="marin"`) {
		t.Error("agent.py still asks the model for a voice while a synthesizer speaks for it")
	}
	// With a synthesizer present the greeting is spoken in the words the package
	// wrote: session.say() raises without a TTS and works with one
	// (agent_activity.py:1555-1565).
	if !strings.Contains(code, "dev_say(self.session,") {
		t.Error("agent.py does not speak the greeting through the synthesizer it built")
	}
}

// TestLiveKitRealtimeGreetsThroughTheModel: with no synthesizer there is nothing
// to say the greeting with, so it is put to the model as an instruction.
func TestLiveKitRealtimeGreetsThroughTheModel(t *testing.T) {
	code := realtimeModule(t, nil)
	for _, want := range []string{
		"self.session.generate_reply(",
		"Open the call with this greeting",
		"Hi, this is Sage and Stone Salon. How can I help?",
		"The caller went quiet",
		"user_away_timeout=",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("agent.py is missing %q", want)
		}
	}
	if strings.Contains(code, "dev_say(") {
		t.Error("agent.py tries to say the greeting with a synthesizer it never built")
	}
}

// TestLiveKitRealtimeDependsOnWhatItImports: an import with no dependency is an
// ImportError at worker startup, and a dependency with no import is a model
// download in every image that nothing loads.
func TestLiveKitRealtimeDependsOnWhatItImports(t *testing.T) {
	pyproject := artifactFile(t, realtimeArtifact(t, nil), "pyproject.toml")
	if !strings.Contains(pyproject, "livekit-agents[openai]==") {
		t.Errorf("pyproject.toml does not install the openai extra:\n%s", pyproject)
	}
	// This fixture writes turn_detection: local, so it loads a VAD and must
	// install the plugin that provides it.
	if !strings.Contains(pyproject, "livekit-plugins-silero") {
		t.Errorf("pyproject.toml does not install silero for a package that loads one:\n%s", pyproject)
	}

	if pyproject := artifactFile(t, realtimeArtifact(t, setTurnDetection("semantic")), "pyproject.toml"); strings.Contains(pyproject, "livekit-plugins-silero") {
		t.Errorf("pyproject.toml installs silero for a package that loads none:\n%s", pyproject)
	}
}

// TestLiveKitRealtimeRunbookSaysWhoDecidesTheTurn holds the fourth documentation
// surface: the runbook is the only one generated per package, so it is the one
// that can name this package's own answer.
func TestLiveKitRealtimeRunbookSaysWhoDecidesTheTurn(t *testing.T) {
	local := artifactFile(t, realtimeArtifact(t, nil), "README.md")
	for _, want := range []string{
		"## The realtime model",
		"`RealtimeModel` sits in the session",
		"This package decides the turn",
		"## Turn taking",
	} {
		if !strings.Contains(local, want) {
			t.Errorf("the realtime runbook is missing %q", want)
		}
	}

	vendor := artifactFile(t, realtimeArtifact(t, setTurnDetection("semantic")), "README.md")
	if !strings.Contains(vendor, "The model decides the turn") {
		t.Error("the realtime runbook does not say the model decides the turn")
	}
	if strings.Contains(vendor, "## Turn taking") {
		t.Error("the realtime runbook explains a turn-taking window no setting of the package reaches")
	}
}

// TestLiveKitCascadeStillDecidesItsOwnTurn is the control for every absence
// above. Without it they would pass just as well if the realtime branch
// swallowed the pipeline on every package, which is the failure the compat
// digests catch tree-wide and this catches by name.
func TestLiveKitCascadeStillDecidesItsOwnTurn(t *testing.T) {
	code := codeOnly(artifactFile(t, generateFor(t, "simple-prompt", ir.ProviderLiveKit), "agent.py"))
	for _, want := range []string{
		"stt=", "tts=", "turn_handling=", "vad=", "silero",
		"inference.TurnDetector", "TurnHandlingOptions", "dev_say(",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("a cascaded package no longer emits %q; the realtime branch is leaking", want)
		}
	}
	if strings.Contains(code, "RealtimeModel") {
		t.Error("a cascaded package builds a realtime model")
	}
}

// TestSpeechToSpeechStillBuildsItsKnowledgeIndexes.
//
// A speech-to-speech package builds no voice activity detector, so prewarm has
// nothing to load and used to return early. The index build sat below that
// return, and the emitted Python read:
//
//	def prewarm(proc: JobProcess) -> None:
//	    # ...no voice activity detector...
//	    return
//	    knowledge.build_indexes()
//
// which compiled clean, validated clean, and indexed nothing. Every knowledge
// search on the call would then run against an empty index. Nothing about
// either architecture stops knowledge working: a search is a tool, and both
// shapes call tools.
//
// Asserted as "the call is reachable" rather than "the call is present",
// because presence is what was true while it was broken.
func TestSpeechToSpeechStillBuildsItsKnowledgeIndexes(t *testing.T) {
	for _, fixture := range []string{"live_model", "realtime_model"} {
		t.Run(fixture, func(t *testing.T) {
			agent := agentFor(t, fixture)
			// The smallest resolved base that reaches the template. Built here
			// rather than added to a fixture on disk, because every fixture's
			// bytes are pinned by the compat digests and this asks a question
			// about the template, not about a package.
			agent.Knowledge = map[string]ir.KnowledgeBase{"refunds": {
				Name: "refunds", Documents: "knowledge/refunds", Embed: "openai",
				Files: []string{"policy.md"}, ChunkSize: 220, ChunkOverlap: 40, TopK: 3, Mode: "meaning",
			}}
			artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
			if err != nil {
				t.Fatal(err)
			}
			module := artifactFile(t, artifact, "agent.py")
			body, ok := prewarmBody(module)
			if !ok {
				t.Fatal("agent.py emits no prewarm")
			}
			if !strings.Contains(body, "knowledge.build_indexes()") {
				t.Fatalf("prewarm never builds the knowledge indexes:\n%s", body)
			}
			if reachable := codeOnly(body); strings.Contains(reachable, "return") &&
				strings.Index(reachable, "return") < strings.Index(reachable, "knowledge.build_indexes()") {
				t.Errorf("prewarm returns before it builds the indexes, so they are never built:\n%s", body)
			}
		})
	}
}

// prewarmBody is prewarm's own lines, up to the next top-level statement.
func prewarmBody(module string) (string, bool) {
	_, after, ok := strings.Cut(module, "def prewarm(proc: JobProcess) -> None:\n")
	if !ok {
		return "", false
	}
	var kept []string
	for _, line := range strings.Split(after, "\n") {
		if line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			break
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n"), true
}

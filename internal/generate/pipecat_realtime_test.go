package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// realtimePipecatBot compiles internal/testdata/realtime_pipecat to the Pipecat
// target and returns the emitted bot.py. mutate gets the resolved package first,
// so one fixture covers every shape a realtime package can take: the three
// turn-detection answers, the absent one, and the half cascade. Writing a
// directory per shape would make the fixture set grow with the question rather
// than with the feature.
func realtimePipecatBot(t *testing.T, mutate func(agent *ir.Agent, target *ir.Target)) string {
	t.Helper()
	agent := agentFor(t, "realtime_pipecat")
	resolved := targetByProvider(t, agent, ir.ProviderPipecat)
	if mutate != nil {
		mutate(agent, &resolved)
	}
	artifact, err := Generate(agent, resolved, targetcap.Default())
	if err != nil {
		t.Fatal(err)
	}
	return artifactFile(t, artifact, "bot.py")
}

// turnDetection rewrites the fixture's authored value. The binding and the model
// definition both carry it, and the driver reads the binding, so both are set:
// a test that moved one would be asserting against a package no author can
// write.
func turnDetection(value string) func(*ir.Agent, *ir.Target) {
	return func(agent *ir.Agent, resolved *ir.Target) {
		binding := resolved.Models.Realtime["voice"]
		binding.TurnDetection = value
		resolved.Models.Realtime["voice"] = binding
		def := agent.Models["voice"]
		def.TurnDetection = value
		agent.Models["voice"] = def
	}
}

// halfCascade binds a synthesizer on the agent and takes the voice off the
// entry, which is exactly the pair validation holds an author to.
func halfCascade(agent *ir.Agent, resolved *ir.Target) {
	binding := resolved.Models.Realtime["voice"]
	binding.Voice = ""
	resolved.Models.Realtime["voice"] = binding
	def := agent.Models["voice"]
	def.Voice = ""
	agent.Models["voice"] = def
	if resolved.Models.Speak == nil {
		resolved.Models.Speak = map[string]ir.Binding{}
	}
	resolved.Models.Speak["tts"] = ir.Binding{Provider: "cartesia", Model: "sonic-3", Voice: "a0e99841-438c-4a64-b679-ae501e7d6091"}
	desk := agent.Agents["desk"]
	desk.Voice = "tts"
	agent.Agents["desk"] = desk
}

// TestPipecatRealtimeBuildsOneServiceAndNothingItReplaces reads the emitted
// construction, which is the whole of FR-002 on this target.
func TestPipecatRealtimeBuildsOneServiceAndNothingItReplaces(t *testing.T) {
	code := codeOnly(realtimePipecatBot(t, nil))

	for _, want := range []string{
		"from pipecat.services.openai.realtime.llm import OpenAIRealtimeLLMService",
		"from pipecat.services.openai.realtime import events as realtime_events",
		"return OpenAIRealtimeLLMService(",
		`api_key=os.environ["OPENAI_API_KEY"]`,
		// Inside settings=, which is the whole point of the next test.
		"settings=OpenAIRealtimeLLMService.Settings(",
		`model="gpt-realtime-2.1"`,
		"system_instruction=DESK_PROMPT",
		"session_properties=realtime_events.SessionProperties(",
		`output=realtime_events.AudioOutput(voice="marin")`,
		// The package's tools are registered the way a cascaded package
		// registers them, so they run over the same socket.
		"async def lookup_customer(",
		"async def check_availability(",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("bot.py is missing %q", want)
		}
	}

	// The absences are the contract. A transcriber or a synthesizer would give
	// the call a second pair of ears or a second voice; a build_stt() with
	// nothing to build is a NameError at startup.
	for _, absent := range []string{
		"def build_stt(", "STTService", "TTSService", "dev.observe_stt(build_stt())",
		"def build_desk_llm(", "def build_desk_tts(",
	} {
		if strings.Contains(code, absent) {
			t.Errorf("bot.py carries %q, which architecture: realtime replaces", absent)
		}
	}
}

// TestPipecatRealtimeQueuesNoUnimplementedFrame is the one absence that is a
// silent failure rather than a crash.
//
// pipecat-ai 1.10.0 routes LLMMessagesAppendFrame to a handler that is one line
// long (services/openai/realtime/llm.py:693-694):
//
//	async def _handle_messages_append(self, frame):
//	    logger.error("!!! NEED TO IMPLEMENT MESSAGES APPEND")
//
// The frame is accepted, logged about and dropped. Every other shape this driver
// emits queues that frame, the greeting and the idle nudge included, so the
// realtime branch had to find another route for both and this is what holds it
// to them.
func TestPipecatRealtimeQueuesNoUnimplementedFrame(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*ir.Agent, *ir.Target)
	}{
		{"local turns", nil},
		{"vendor turns", turnDetection(targetcap.TurnDetectionSemantic)},
		{"half cascade", halfCascade},
	} {
		t.Run(tc.name, func(t *testing.T) {
			module := realtimePipecatBot(t, tc.mutate)
			// The whole module, comments stripped: the import line is as much a
			// finding as the call, and a comment naming the frame is not.
			if strings.Contains(codeOnly(module), "LLMMessagesAppendFrame") {
				t.Error("bot.py names LLMMessagesAppendFrame; on this service that frame is logged and dropped")
			}
			// The two routes that replace it.
			for _, want := range []string{
				`messages=[{"role": "developer", "content": "Open the call with this greeting`,
				"await worker.queue_frame(LLMRunFrame())",
				"await realtime.send_client_event(realtime_events.ConversationItemCreateEvent(",
				"await realtime.send_client_event(realtime_events.ResponseCreateEvent())",
			} {
				if !strings.Contains(module, want) {
					t.Errorf("bot.py is missing %q, so nothing replaces the dropped frame", want)
				}
			}
		})
	}
}

// TestPipecatRealtimeTurnDetectionLowersThroughTheTable reads FR-003 and FR-005
// off the emitted session properties.
func TestPipecatRealtimeTurnDetectionLowersThroughTheTable(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  string
		// absent is what must NOT appear, because on this target every wrong
		// answer is another legal expression rather than a syntax error.
		absent []string
	}{
		{targetcap.TurnDetectionLocal, "turn_detection=False", []string{"TurnDetection()", "SemanticTurnDetection()"}},
		{targetcap.TurnDetectionServerVAD, "turn_detection=realtime_events.TurnDetection()", []string{"turn_detection=False", "SemanticTurnDetection"}},
		{targetcap.TurnDetectionSemantic, "turn_detection=realtime_events.SemanticTurnDetection()", []string{"turn_detection=False"}},
	} {
		t.Run(tc.value, func(t *testing.T) {
			code := codeOnly(realtimePipecatBot(t, turnDetection(tc.value)))
			if !strings.Contains(code, tc.want) {
				t.Errorf("turn_detection %q did not emit %q", tc.value, tc.want)
			}
			for _, absent := range tc.absent {
				if strings.Contains(code, absent) {
					t.Errorf("turn_detection %q also emitted %q", tc.value, absent)
				}
			}
		})
	}

	// An omitted value emits nothing, so the vendor's own default stands and
	// this compiler pins nothing it was not asked to pin (FR-005).
	code := codeOnly(realtimePipecatBot(t, turnDetection("")))
	if strings.Contains(code, "turn_detection") {
		t.Error("a package writing no turn_detection still emitted one, which pins the vendor's default")
	}
}

// TestPipecatRealtimeLocalTurnsBuildTheLocalDetector is the other half of
// `local`: switching the vendor's detection off is only useful if something else
// decides the turn, and on this target that is the same VAD and end-of-turn
// analyzer a cascaded package builds.
func TestPipecatRealtimeLocalTurnsBuildTheLocalDetector(t *testing.T) {
	local := codeOnly(realtimePipecatBot(t, nil))
	for _, want := range []string{
		"vad_analyzer=SileroVADAnalyzer(params=VADParams(stop_secs=",
		"user_turn_strategies=UserTurnStrategies(",
		"TurnAnalyzerUserTurnStopStrategy(",
	} {
		if !strings.Contains(local, want) {
			t.Errorf("turn_detection: local is missing %q, so nothing decides the turn", want)
		}
	}

	// And the mirror image: a package that left the decision with the vendor
	// builds none of it, and passes no strategies of its own, because the
	// aggregator ignores the service's recommended strategies the moment any are
	// passed (llm.py class docstring, read 2026-09-13).
	vendor := codeOnly(realtimePipecatBot(t, turnDetection(targetcap.TurnDetectionServerVAD)))
	for _, absent := range []string{
		"user_turn_strategies=", "LocalSmartTurnAnalyzerV3", "VADParams(",
	} {
		if strings.Contains(vendor, absent) {
			t.Errorf("turn_detection: server_vad still emitted %q, which takes the decision back off the vendor", absent)
		}
	}
	// The VAD itself stays, for the speaking frames the idle timer and the
	// latency measurement anchor on.
	if !strings.Contains(vendor, "vad_analyzer=SileroVADAnalyzer()") {
		t.Error("turn_detection: server_vad dropped the VAD, which the idle timer and the latency measurement read")
	}
}

// TestPipecatRealtimeAsksForCallerTranscription is a deliberate cost, recorded
// as a gate so it cannot be dropped as an optimisation.
//
// SessionProperties leaves audio None (events.py:191-237). With no transcription
// configured the server sends no conversation.item.input_audio_transcription.*
// events, and llm.py:967-971 is the only place the caller's words become frames.
// Without this the conversation context and the dev page would hold the model's
// side of every call and none of the caller's.
func TestPipecatRealtimeAsksForCallerTranscription(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*ir.Agent, *ir.Target)
	}{
		{"model speaks", nil},
		{"half cascade", halfCascade},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code := codeOnly(realtimePipecatBot(t, tc.mutate))
			if !strings.Contains(code, "transcription=realtime_events.InputAudioTranscription()") {
				t.Error("the session asks for no caller transcription, so the caller's words reach neither the context nor the dev page")
			}
		})
	}
}

// TestPipecatRealtimeHalfCascadeAsksForTextAndBuildsAVoice is User Story 2 read
// off the module: the model writes, the bound synthesizer speaks.
func TestPipecatRealtimeHalfCascadeAsksForTextAndBuildsAVoice(t *testing.T) {
	code := codeOnly(realtimePipecatBot(t, halfCascade))
	for _, want := range []string{
		// llm.py:1044-1050: with text as the only modality the service pushes
		// plain LLMTextFrames, which an ordinary TTS service consumes.
		`output_modalities=["text"]`,
		"def build_desk_tts(",
		"CartesiaTTSService(",
		"dev.observe_tts(build_desk_tts())",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("the half cascade is missing %q", want)
		}
	}
	// The model has no voice of its own here, and asking for one is how a call
	// would get two.
	if strings.Contains(code, "AudioOutput(voice=") {
		t.Error("the half cascade still gave the model a voice, so the call would have two")
	}

	// And the package that keeps the model's own voice asks for no text-only
	// output, because that would leave nothing speaking at all.
	if strings.Contains(codeOnly(realtimePipecatBot(t, nil)), "output_modalities") {
		t.Error("a realtime package with its own voice set output_modalities, which decides what speaks")
	}
}

// TestPipecatRealtimePrerollFollowsTheTurnStartStrategy covers the one
// constructor argument this driver writes conditionally.
//
// In manual turn mode the service replays a little recent audio after an
// interruption clears the input buffer. Left unset it auto-sizes that window
// from the upstream VAD's start_secs, but only because it assumes the default
// VAD turn-start strategy (llm.py:278-289). A minimum word count replaces that
// strategy, so the assumption stops holding and the window is written out at the
// service's own fallback (llm.py:78).
func TestPipecatRealtimePrerollFollowsTheTurnStartStrategy(t *testing.T) {
	if code := codeOnly(realtimePipecatBot(t, nil)); strings.Contains(code, "user_audio_preroll_secs") {
		t.Error("a package with the default turn-start strategy pinned the preroll, which the service sizes better itself")
	}
	minWords := func(agent *ir.Agent, _ *ir.Target) {
		enabled := true
		agent.Conversation.Interruption = &ir.Interruption{Enabled: &enabled, MinimumWords: 3}
	}
	code := codeOnly(realtimePipecatBot(t, minWords))
	if !strings.Contains(code, "user_audio_preroll_secs=0.5") {
		t.Error("a minimum word count replaced the VAD turn-start strategy and the preroll was left to auto-size off it")
	}
	if !strings.Contains(code, "MinWordsUserTurnStartStrategy(min_words=3)") {
		t.Error("the minimum word count did not reach the aggregator, so this test is asserting against nothing")
	}
}

// TestPipecatRealtimeEmitsNothingForAPackageThatDeclaresNone is the byte-for-byte
// half of FR-012, held on the shipped cascaded package the compat digests also
// cover. A new template branch is the one change that can move every package's
// output at once, which is why this asserts on the whole file rather than on a
// marker.
func TestPipecatRealtimeEmitsNothingForAPackageThatDeclaresNone(t *testing.T) {
	for _, name := range []string{"safe_core", "live_model"} {
		t.Run(name, func(t *testing.T) {
			module := artifactFile(t, generateFor(t, name, ir.ProviderPipecat), "bot.py")
			for _, marker := range []string{
				"realtime_events", "OpenAIRealtimeLLMService", "_realtime()",
			} {
				if strings.Contains(module, marker) {
					t.Errorf("%s names %q and declares no realtime model", name, marker)
				}
			}
		})
	}
}

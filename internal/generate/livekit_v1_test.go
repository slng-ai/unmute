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
		"from livekit.plugins import openai, silero, slng",
		"slng.STT(",
		"slng.TTS(",
		"openai.LLM(", // native plugin, not Inference: console runs on OPENAI_API_KEY alone (B6/V19)
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
		"from livekit.plugins import cartesia, deepgram, elevenlabs, openai, silero",
		`stt=deepgram.STT(api_key=os.environ.get("DEEPGRAM_API_KEY"), model="nova-3", language="en")`,
		`tts=elevenlabs.TTS(api_key=os.environ.get("ELEVEN_API_KEY"), voice_id="cgSgspJ2msm6clMCkdW9", language="en")`,
		`tts=cartesia.TTS(api_key=os.environ.get("CARTESIA_API_KEY"), voice="f786b574-daa5-4673-aa0c-cbe3e8534c02", model="sonic-3", language="en")`,
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
	pyproject := artifactFile(t, artifact, "pyproject.toml")
	if !strings.Contains(pyproject, `"livekit-agents[cartesia,deepgram,elevenlabs,openai]>=1.5"`) {
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
	_, err := livekitSTTService(&ir.Binding{Provider: "acme", Model: "m"}, "en", env)
	if err == nil || !strings.Contains(err.Error(), "listen providers on livekit: assemblyai, cartesia, deepgram, elevenlabs, gradium, sarvam, slng, soniox, speechmatics") {
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

// TestLiveKitV1SingleTaskDelegate covers the T12 lowering (V1/V3): a delegate
// with `task:` awaits the AgentTask directly, applies `assign` into the typed
// userdata, and returns the typed result to the owner — no TaskGroup involved.
func TestLiveKitV1SingleTaskDelegate(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	agent.Controls["do_find"] = &ir.Delegate{
		Kind: ir.ControlDelegate, Task: "find_slot",
		When:   "The caller only wants to check for a slot, not book yet.",
		Assign: map[string]string{"caller_phone": "result.date"},
	}
	def := agent.Agents["reservations"]
	def.Tools = append(def.Tools, "do_find")
	agent.Agents["reservations"] = def

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		"async def do_find(self, ctx: RunContext) -> dict:",
		"result = await FindSlot(chat_ctx=self.chat_ctx.copy(exclude_instructions=True))",
		`ctx.userdata.caller_phone = result["date"]`,
		"@dataclass\nclass Userdata:",
		"caller_phone: str | None = None",
		"session = AgentSession[Userdata](",
		"userdata=Userdata(),",
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
}

// TestLiveKitV1IsolatedGroup covers the T13 lowering (V2/C3): an isolated
// task_group compiles to a sequence of standalone AgentTasks, each starting
// with a fresh context — never a TaskGroup, which always shares context.
func TestLiveKitV1IsolatedGroup(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	reserve := agent.TaskGroups["reserve_group"]
	reserve.ContextScope = ir.ContextIsolated
	agent.TaskGroups["reserve_group"] = reserve

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		// the isolated flow: fresh AgentTasks, results dict, typed return
		"async def do_reserve(self, ctx: RunContext) -> dict:",
		`task_results["find_slot"] = await FindSlot()`,
		`task_results["confirm_booking"] = await ConfirmBooking()`,
		"return task_results",
		// events_group stays shared, so TaskGroup is still imported and used
		"from livekit.agents.beta.workflows import TaskGroup",
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
	if strings.Contains(botpy, `group.add(\n            lambda: FindSlot`) {
		t.Error("isolated group must not lower to TaskGroup.add")
	}

	// With every group isolated, the TaskGroup import must disappear.
	events := agent.TaskGroups["events_group"]
	events.ContextScope = ir.ContextIsolated
	agent.TaskGroups["events_group"] = events
	artifact, err = Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate all-isolated: %v", err)
	}
	botpy = artifactFile(t, artifact, "agent.py")
	if strings.Contains(botpy, "TaskGroup") {
		t.Error("all-isolated project must not import or use TaskGroup")
	}
}

// TestLiveKitV1PerTaskModel covers the T14 lowering (B1/V1/V15): a task with
// its own model profile gets llm= on the AgentTask, resolved through the
// catalogue; a task on the entry agent's profile stays on the session LLM.
func TestLiveKitV1PerTaskModel(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	agent.Models["fast"] = ir.ModelProfile{Placement: ir.PlacementAPI}
	task := agent.Tasks["find_slot"]
	task.Model = "fast"
	agent.Tasks["find_slot"] = task
	tgt := targetByProvider(t, agent, ir.ProviderLiveKit)
	tgt.Models.Reason["fast"] = ir.Binding{Provider: "openai", Model: "gpt-4o-mini"}

	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	want := `super().__init__(instructions=FIND_SLOT_PROMPT, chat_ctx=chat_ctx, llm=openai.LLM(api_key=os.environ.get("OPENAI_API_KEY"), model="gpt-4o-mini"))`
	if !strings.Contains(botpy, want) {
		t.Errorf("agent.py missing per-task llm override %q", want)
	}
	// The other tasks keep the session LLM: no stray llm= kwarg.
	if !strings.Contains(botpy, "super().__init__(instructions=CONFIRM_BOOKING_PROMPT, chat_ctx=chat_ctx)") {
		t.Error("confirm_booking must stay on the session LLM")
	}
}

// TestLiveKitV1HistoryShapingAndFallback covers the T5 lowerings (V4/V5):
// every history value compiles, include_tool_calls and variables subsets
// shape the handoff, and a fallback chain lowers to llm.FallbackAdapter.
func TestLiveKitV1HistoryShapingAndFallback(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	// Fallback chain on the session profile.
	profile := agent.Models["reasoning"]
	profile.Fallback = []string{"backup"}
	agent.Models["reasoning"] = profile
	agent.Models["backup"] = ir.ModelProfile{Placement: ir.PlacementAPI}
	// Shape each transfer differently.
	agent.Variables["visit_count"] = ir.Variable{Type: ir.PrimitiveInteger}
	toRes := agent.Controls["to_reservations"].(*ir.AgentTransfer)
	toRes.Context.History = ir.HistoryMessages
	toRes.Context.Variables = ir.VariableSelection{Names: []string{"caller_phone"}} // visit_count not carried
	toEvents := agent.Controls["to_events"].(*ir.AgentTransfer)
	toEvents.Context.History = ir.HistoryLastN
	toEvents.Context.MaxMessages = 6
	back := agent.Controls["back_to_greeter"].(*ir.AgentTransfer)
	back.Context.History = ir.HistorySummary
	back.Context.Summarizer = "backup"

	tgt := targetByProvider(t, agent, ir.ProviderLiveKit)
	tgt.Models.Reason["backup"] = ir.Binding{Provider: "openai", Model: "gpt-4o"}

	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		// V4: native adapter around the chain, everywhere the profile binds.
		`llm=llm.FallbackAdapter(llm=[openai.LLM(api_key=os.environ.get("OPENAI_API_KEY"), model="gpt-4o-mini", temperature=0.4), openai.LLM(api_key=os.environ.get("OPENAI_API_KEY"), model="gpt-4o")])`,
		// V5: messages / last_n / summary shaping.
		`return Reservations(chat_ctx=llm.ChatContext(items=[m for m in self.chat_ctx.messages() if m.role in ("user", "assistant")]))`,
		`return Events(chat_ctx=_last_n(self.chat_ctx, 6))`,
		"def _last_n(source: llm.ChatContext",
		`summary_ctx = await _summarize(self.chat_ctx, openai.LLM(api_key=os.environ.get("OPENAI_API_KEY"), model="gpt-4o"))`,
		"return Greeter(chat_ctx=summary_ctx)",
		"async def _summarize(source: llm.ChatContext",
		// D7: an uncarried variable resets on the transfer.
		"ctx.userdata.visit_count = None  # context.variables: not carried on this transfer",
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
}

// TestLiveKitV1HistoryResetAndToolCallShaping covers reset transfers and
// include_tool_calls: false (V5): reset hands the target a fresh context;
// exclude_function_call strips tool traffic from a full handoff.
func TestLiveKitV1HistoryResetAndToolCallShaping(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	noCalls := false
	toRes := agent.Controls["to_reservations"].(*ir.AgentTransfer)
	toRes.Context.History = ir.HistoryFull
	toRes.Context.IncludeToolCalls = &noCalls
	back := agent.Controls["back_to_greeter"].(*ir.AgentTransfer)
	back.Context.History = ir.HistoryReset

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		`return Reservations(chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_function_call=True))`,
		"# history: reset — the target starts fresh (a handoff marker still lands).",
		"return Greeter()",
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
}

// TestLiveKitV1ConversationShapingAndAgentTools covers the T15 lowerings
// (V16): agent-level webhook tools, interruption enabled/min_words via
// TurnHandlingOptions, the generated ignore-phrase stt_node filter, thinking
// audio via BackgroundAudioPlayer, and effect: ends_conversation.
func TestLiveKitV1ConversationShapingAndAgentTools(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	def := agent.Agents["greeter"]
	def.Tools = append(def.Tools, "check_availability")
	agent.Agents["greeter"] = def
	enabled := true
	agent.Conversation.Interruption = &ir.Interruption{
		Enabled: &enabled, MinimumWords: 2, IgnorePhrases: []string{"uh-huh", "OK"},
	}
	agent.Conversation.ThinkingAudio = ir.ThinkingSubtle
	tool := agent.Tools["send_confirmation"]
	tool.Effect = ir.ToolEndsConversation
	agent.Tools["send_confirmation"] = tool

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		// Agent-level webhook tool on the greeter class.
		"class Greeter(IgnorePhrasesMixin, Agent):",
		"async def check_availability(self, ctx: RunContext, date: str, party_size: int) -> dict:",
		// Interruption options ride turn_handling.
		`interruption={"enabled": True, "min_words": 2},`,
		// Generated ignore-phrase filter (lowercased phrases).
		`IGNORE_PHRASES = ["uh-huh", "ok"]`,
		"stt.SpeechEventType.FINAL_TRANSCRIPT",
		"class FindSlot(IgnorePhrasesMixin, AgentTask[dict]):",
		// Thinking audio.
		"background_audio = BackgroundAudioPlayer(",
		"thinking_sound=BuiltinAudioClip.KEYBOARD_TYPING,  # thinking_audio: subtle",
		"await background_audio.start(room=ctx.room, agent_session=session)",
		// effect: ends_conversation on a webhook tool.
		"self.session.shutdown()  # effect: ends_conversation",
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
}

// TestLiveKitV1HumanTransferColdAndWarm covers the T6 control lowerings (V6):
// cold is a SIP REFER through the job context with the resolved destination;
// warm awaits the prebuilt WarmTransferTask and registers the trunk env.
func TestLiveKitV1HumanTransferColdAndWarm(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate cold: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		"async def to_human(self, ctx: RunContext) -> str:",
		"job_ctx = get_job_context()",
		`await job_ctx.transfer_sip_participant(identity, "+14155550123", play_dialtone=True)`,
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("cold agent.py missing %q", want)
		}
	}

	human := agent.Controls["to_human"].(*ir.HumanTransfer)
	human.Mode = ir.TransferWarm
	human.Briefing = ir.BriefingSummary
	artifact, err = Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate warm: %v", err)
	}
	botpy = artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		"from livekit.agents.beta.workflows import WarmTransferTask",
		`result = await WarmTransferTask(sip_call_to="+14155550123")`,
		"result.human_agent_identity",
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("warm agent.py missing %q", want)
		}
	}
	envExample := artifactFile(t, artifact, ".env.example")
	if !strings.Contains(envExample, "LIVEKIT_SIP_OUTBOUND_TRUNK") {
		t.Error(".env.example missing LIVEKIT_SIP_OUTBOUND_TRUNK for warm transfer")
	}
}

// TestLiveKitV1RequiresGuard covers V7: a transfer with requires: emits a
// machine-checked guard that refuses and names the unmet variables.
func TestLiveKitV1RequiresGuard(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	toRes := agent.Controls["to_reservations"].(*ir.AgentTransfer)
	toRes.Requires = []string{"caller_phone"}

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		`missing = [n for n, v in (("caller_phone", ctx.userdata.caller_phone), ) if v is None]`,
		`return "Cannot transfer yet; missing required information: " + ", ".join(missing)`,
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
}

// TestLiveKitV1OutboundVoicemail covers V8/N6: an outbound telephony channel
// emits the AMD dial-out flow; on_voicemail picks the machine-vm branch.
func TestLiveKitV1OutboundVoicemail(t *testing.T) {
	for _, tc := range []struct {
		action ir.VoicemailAction
		want   string
	}{
		{ir.VoicemailLeaveMessage, `ctx.shutdown("voicemail: left a message")  # on_voicemail: leave_message`},
		{ir.VoicemailHangup, `ctx.shutdown("voicemail detected")  # on_voicemail: hangup`},
	} {
		pkg, err := spec.Load(filepath.Join("..", "..", "examples", "remy"))
		if err != nil {
			t.Fatal(err)
		}
		agent, err := ir.Build(pkg)
		if err != nil {
			t.Fatal(err)
		}
		inbound, outbound := false, true
		agent.Channels["phone"] = ir.Channel{Kind: ir.ChannelTelephony, Inbound: &inbound, Outbound: &outbound, OnVoicemail: tc.action}

		artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
		if err != nil {
			t.Fatalf("generate %s: %v", tc.action, err)
		}
		botpy := artifactFile(t, artifact, "agent.py")
		for _, want := range []string{
			"async with AMD(session, participant_identity=\"phone_user\") as detector:",
			"api.CreateSIPParticipantRequest(",
			`sip_trunk_id=os.environ["LIVEKIT_SIP_OUTBOUND_TRUNK"],`,
			"wait_until_answered=True,",
			"result = await detector.execute()",
			tc.want,
		} {
			if !strings.Contains(botpy, want) {
				t.Errorf("%s agent.py missing %q", tc.action, want)
			}
		}
	}
}

// TestLiveKitV1PinsAndSDKLanguage covers the T7 remainders (C6/C1): plugin
// pins raise dep floors and are range-checked; a non-python sdk_language
// fails loud instead of emitting a silently-wrong Python project.
func TestLiveKitV1PinsAndSDKLanguage(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	tgt := targetByProvider(t, agent, ir.ProviderLiveKit)
	tgt.Pins = map[string]string{"livekit-plugins-slng": "1.7.0"}
	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatalf("generate with pin: %v", err)
	}
	if pyproject := artifactFile(t, artifact, "pyproject.toml"); !strings.Contains(pyproject, `"livekit-plugins-slng>=1.7.0"`) {
		t.Errorf("pin did not raise the plugin floor:\n%s", pyproject)
	}
	if !strings.Contains(artifactFile(t, artifact, "livekit.toml"), `id = "livekit-dev"`) {
		t.Error("livekit.toml missing the agent id")
	}

	tgt.Pins = map[string]string{"livekit-plugins-slng": "1.0.0"}
	if _, err := Generate(agent, tgt, target.Default()); err == nil || !strings.Contains(err.Error(), "below the catalogue floor") {
		t.Fatalf("below-floor pin must fail, got %v", err)
	}
	tgt.Pins = map[string]string{"left-pad": "1.0.0"}
	if _, err := Generate(agent, tgt, target.Default()); err == nil || !strings.Contains(err.Error(), "not a pinnable package") {
		t.Fatalf("unknown pin must fail, got %v", err)
	}
	tgt.Pins = nil
	tgt.SDKLanguage = "node"
	if _, err := Generate(agent, tgt, target.Default()); err == nil || !strings.Contains(err.Error(), "python projects only") {
		t.Fatalf("sdk_language node must fail loud, got %v", err)
	}
}

// TestLiveKitV1LocalAndMCPTools covers the tool executions beyond webhook:
// local copies the package handler into tools/<name>.py and wraps it (SCHEMA
// §5, code targets); mcp mounts MCPServerHTTP off url_env with allowed_tools
// (B3/D8). The local handler rides spec.Load like instructions do.
func TestLiveKitV1LocalAndMCPTools(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	agent.Tools["fetch_notes"] = ir.Tool{
		Description: "Fetch the caller's saved notes.",
		Input:       map[string]any{"type": "object", "properties": map[string]any{"topic": map[string]any{"type": "string"}}, "required": []any{"topic"}},
		Execution:   ir.ToolLocal, Handler: "tools/fetch_notes.py",
		HandlerSource: "def fetch_notes(topic):\n    return {\"notes\": []}\n",
		Interruption:  ir.ToolProviderDefault, Effect: ir.ToolReturnsData,
	}
	agent.Tools["book_table"] = ir.Tool{
		Description: "Book the table through the bookings MCP server.",
		Input:       map[string]any{"type": "object"},
		Execution:   ir.ToolMCP, URLEnv: "BOOKINGS_MCP_URL",
		Interruption: ir.ToolProviderDefault, Effect: ir.ToolReturnsData,
	}
	def := agent.Agents["greeter"]
	def.Tools = append(def.Tools, "fetch_notes", "book_table")
	agent.Agents["greeter"] = def

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		"import inspect",
		"import tools.fetch_notes",
		"async def fetch_notes(self, ctx: RunContext, topic: str) -> dict:",
		"result = tools.fetch_notes.fetch_notes(topic=topic)",
		"if inspect.isawaitable(result):",
		`mcp_servers=[mcp.MCPServerHTTP(url=os.environ["BOOKINGS_MCP_URL"], allowed_tools=["book_table"])],`,
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
	if handler := artifactFile(t, artifact, "tools/fetch_notes.py"); !strings.Contains(handler, "def fetch_notes(topic):") {
		t.Errorf("handler not copied verbatim:\n%s", handler)
	}
	artifactFile(t, artifact, "tools/__init__.py")
	if env := artifactFile(t, artifact, ".env.example"); !strings.Contains(env, "BOOKINGS_MCP_URL") {
		t.Error(".env.example missing the MCP server env")
	}
}

// TestLiveKitEmitterMatchesCapabilityTable is the table↔emitter agreement test
// (V15, mirroring pipecat's): the emitter's declared code paths must equal the
// table's non-gated LiveKit rows, so no field is validate-green yet silently
// unemitted (B1's class).
func TestLiveKitEmitterMatchesCapabilityTable(t *testing.T) {
	table := target.Default()
	for field := range table.Fields {
		capability := table.Capability(field, target.LiveKit)
		supported := capability.Tag != target.Gated && capability.Tag != target.Provisional
		if livekitEmittedFields[field] != supported {
			t.Errorf("field %q: emitter emits=%v, table supported=%v (tag %q) — implement or gate to reconcile",
				field, livekitEmittedFields[field], supported, capability.Tag)
		}
	}
}

// TestLiveKitV1ParityFixture is the V14 fixture: one package loaded with every
// SCHEMA §7 livekit-ok feature at once must validate green AND generate — no
// validate-green/generate-fail is representable (B2's class).
func TestLiveKitV1ParityFixture(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	inbound, outbound, enabled, noCalls := false, true, true, false
	agent.Channels["phone"] = ir.Channel{Kind: ir.ChannelTelephony, Inbound: &inbound, Outbound: &outbound, OnVoicemail: ir.VoicemailLeaveMessage}
	agent.Conversation.Interruption = &ir.Interruption{Enabled: &enabled, MinimumWords: 2, IgnorePhrases: []string{"uh-huh"}}
	agent.Conversation.ThinkingAudio = ir.ThinkingSubtle
	agent.Variables["visit_count"] = ir.Variable{Type: ir.PrimitiveInteger}
	agent.Models["backup"] = ir.ModelProfile{Placement: ir.PlacementAPI}
	profile := agent.Models["reasoning"]
	profile.Fallback = []string{"backup"}
	agent.Models["reasoning"] = profile
	toRes := agent.Controls["to_reservations"].(*ir.AgentTransfer)
	toRes.Requires = []string{"caller_phone"}
	toRes.Context.History = ir.HistoryLastN
	toRes.Context.MaxMessages = 6
	toRes.Context.IncludeToolCalls = &noCalls
	toRes.Context.Variables = ir.VariableSelection{Names: []string{"caller_phone"}}
	back := agent.Controls["back_to_greeter"].(*ir.AgentTransfer)
	back.Context.History = ir.HistorySummary
	back.Context.Summarizer = "backup"
	agent.Controls["to_human"] = &ir.HumanTransfer{Kind: ir.ControlHumanTransfer, Destination: "line", Mode: ir.TransferWarm, Briefing: ir.BriefingSummary}
	agent.Controls["to_human_cold"] = &ir.HumanTransfer{Kind: ir.ControlHumanTransfer, Destination: "line", Mode: ir.TransferCold}
	agent.Tools["fetch_notes"] = ir.Tool{
		Description: "Fetch the caller's saved notes.",
		Input:       map[string]any{"type": "object", "properties": map[string]any{"topic": map[string]any{"type": "string"}}},
		Execution:   ir.ToolLocal, Handler: "tools/fetch_notes.py", HandlerSource: "def fetch_notes(topic):\n    return {}\n",
		Interruption: ir.ToolProviderDefault, Effect: ir.ToolReturnsData,
	}
	agent.Tools["book_table"] = ir.Tool{
		Description: "Book through the bookings MCP server.", Input: map[string]any{"type": "object"},
		Execution: ir.ToolMCP, URLEnv: "BOOKINGS_MCP_URL",
		Interruption: ir.ToolProviderDefault, Effect: ir.ToolReturnsData,
	}
	def := agent.Agents["greeter"]
	def.Tools = append(def.Tools, "to_human", "to_human_cold", "fetch_notes", "book_table")
	agent.Agents["greeter"] = def
	reserve := agent.TaskGroups["reserve_group"]
	reserve.ContextScope = ir.ContextIsolated
	agent.TaskGroups["reserve_group"] = reserve
	task := agent.Tasks["find_slot"]
	task.Model = "backup"
	task.Result["details"] = ir.ResultField{Schema: map[string]any{"type": "object"}}
	agent.Tasks["find_slot"] = task
	agent.Controls["do_find"] = &ir.Delegate{Kind: ir.ControlDelegate, Task: "find_slot", Assign: map[string]string{"caller_phone": "result.date"}}
	resDef := agent.Agents["reservations"]
	resDef.Tools = append(resDef.Tools, "do_find")
	agent.Agents["reservations"] = resDef

	tgt := targetByProvider(t, agent, ir.ProviderLiveKit)
	tgt.Models.Reason["backup"] = ir.Binding{Model: "openai/gpt-4o"}
	tgt.Destinations = map[string]string{"line": "+14155550123"}
	tgt.Pins = map[string]string{"livekit-plugins-slng": "1.7.0"}

	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatalf("the fully-loaded fixture must generate: %v", err)
	}
	if len(artifact.Files) == 0 {
		t.Fatal("no files emitted")
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

// TestV17_SlngRouteVerbatim (driver-livekit V17, B4): slng routes reach the
// plugin's model= argument verbatim — the slng/ prefix names the SLNG-hosted
// route family and is part of the URL path, so stripping it 404s.
func TestV17_SlngRouteVerbatim(t *testing.T) {
	for _, tc := range []struct {
		role  string
		route string
	}{
		{"listen", "slng/deepgram/nova:3-en"},
		{"speak", "slng/deepgram/aura:2-en"},
	} {
		binding := ir.Binding{Provider: "slng", Model: tc.route}
		var svc livekitService
		var err error
		if tc.role == "listen" {
			svc, err = livekitSTTService(&binding, "en", newEnvSet())
		} else {
			binding.Voice = "aura-2-thalia-en"
			svc, err = livekitTTSService(binding, "en", newEnvSet())
		}
		if err != nil {
			t.Fatalf("%s: %v", tc.role, err)
		}
		found := false
		for _, kv := range svc.Call.Args {
			if kv.Key == "model" {
				found = true
				if kv.Value != pyQuote(tc.route) {
					t.Errorf("%s model = %s, want %s (route must pass verbatim)", tc.role, kv.Value, pyQuote(tc.route))
				}
			}
		}
		if !found {
			t.Errorf("%s call has no model kwarg", tc.role)
		}
	}
}

// TestV18_TurnBindingLowersToDetectorVersion (driver-livekit V18, B5): the
// target's turn: binding maps to the inference.TurnDetector version — mini is
// fully local (no LiveKit Cloud creds), absent means SDK auto-select (C5),
// and an unrecognized model fails loud instead of being silently dropped.
func TestV18_TurnBindingLowersToDetectorVersion(t *testing.T) {
	for _, tc := range []struct {
		binding *ir.Binding
		want    string
		wantErr bool
	}{
		{nil, "", false},
		{&ir.Binding{Provider: "livekit"}, "", false},
		{&ir.Binding{Provider: "livekit", Model: "turn-detector-mini"}, "v1-mini", false},
		{&ir.Binding{Provider: "livekit", Model: "turn-detector"}, "v1", false},
		{&ir.Binding{Provider: "livekit", Model: "eou-9000"}, "", true},
	} {
		got, err := livekitTurnVersion(tc.binding)
		if (err != nil) != tc.wantErr {
			t.Errorf("livekitTurnVersion(%+v): err=%v, wantErr=%v", tc.binding, err, tc.wantErr)
			continue
		}
		if got != tc.want {
			t.Errorf("livekitTurnVersion(%+v) = %q, want %q", tc.binding, got, tc.want)
		}
	}
}

// TestV19_NativeReasonBeatsInferenceWildcard (driver-livekit V19, B6): a
// reason vendor with a native catalogue entry binds that plugin with its own
// key env — never the Inference wildcard — so the scaffold default runs
// console with no LiveKit Cloud creds. provider: livekit stays the deliberate
// Inference spelling with the model passed verbatim.
func TestV19_NativeReasonBeatsInferenceWildcard(t *testing.T) {
	env := newEnvSet()
	svc, err := livekitLLMService(ir.Binding{Provider: "openai", Model: "gpt-4.1-mini"}, env)
	if err != nil {
		t.Fatal(err)
	}
	if svc.Call.Class != "openai.LLM" {
		t.Errorf("openai reason class = %q, want openai.LLM", svc.Call.Class)
	}
	if !env.seen["OPENAI_API_KEY"] {
		t.Error("openai reason binding did not register OPENAI_API_KEY")
	}
	if env.seen["LIVEKIT_API_KEY"] {
		t.Error("openai reason binding registered LIVEKIT_API_KEY (wildcard leak)")
	}

	svc, err = livekitLLMService(ir.Binding{Provider: "livekit", Model: "openai/gpt-4o-mini"}, newEnvSet())
	if err != nil {
		t.Fatal(err)
	}
	if svc.Call.Class != "inference.LLM" {
		t.Errorf("provider livekit class = %q, want inference.LLM", svc.Call.Class)
	}
	for _, kv := range svc.Call.Args {
		if kv.Key == "model" && kv.Value != pyQuote("openai/gpt-4o-mini") {
			t.Errorf("inference model = %s, want %s (verbatim, no livekit/ join)", kv.Value, pyQuote("openai/gpt-4o-mini"))
		}
	}
}

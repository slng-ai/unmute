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
		`stt=deepgram.STT(api_key=os.environ.get("DEEPGRAM_API_KEY"), model="nova-3", language="en")`,
		`tts=elevenlabs.TTS(api_key=os.environ.get("ELEVEN_API_KEY"), voice_id="cgSgspJ2msm6clMCkdW9", language="en")`,
		`tts=cartesia.TTS(api_key=os.environ.get("CARTESIA_API_KEY"), voice="f786b574-daa5-4673-aa0c-cbe3e8534c02", model="sonic-3", language="en")`,
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
	_, err := livekitSTTService(&ir.Binding{Provider: "acme", Model: "m"}, "en", env)
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
	tgt.Models.Reason["fast"] = ir.Binding{Model: "openai/gpt-4o-mini"}

	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	want := `super().__init__(instructions=FIND_SLOT_PROMPT, chat_ctx=chat_ctx, llm=inference.LLM(model="openai/gpt-4o-mini"))`
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
	tgt.Models.Reason["backup"] = ir.Binding{Model: "openai/gpt-4o"}

	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		// V4: native adapter around the chain, everywhere the profile binds.
		`llm=llm.FallbackAdapter(llm=[inference.LLM(model="openai/gpt-4o-mini", extra_kwargs={"temperature": 0.4}), inference.LLM(model="openai/gpt-4o")])`,
		// V5: messages / last_n / summary shaping.
		`return Reservations(chat_ctx=llm.ChatContext(items=[m for m in self.chat_ctx.messages() if m.role in ("user", "assistant")]))`,
		`return Events(chat_ctx=_last_n(self.chat_ctx, 6))`,
		"def _last_n(source: llm.ChatContext",
		`summary_ctx = await _summarize(self.chat_ctx, inference.LLM(model="openai/gpt-4o"))`,
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

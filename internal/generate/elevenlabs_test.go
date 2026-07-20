package generate

import (
	"encoding/json"
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
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
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

// TestElevenLabsWorkflowGolden exercises the Workflow graph safe_core omits:
// a task (assign via a JSON-returning tool), a task_group (then: return), and a
// model-written opening, built in-code (driver-elevenlabs T4/T5, V1/V2/V6).
func TestElevenLabsWorkflowGolden(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}

	// A tool that returns JSON is the only mid-call write path (C3), so assign compiles.
	agent.Tools["save_tier"] = ir.Tool{
		Description: "Persist the caller's tier and return it as JSON.",
		Input:       map[string]any{"type": "object"},
		Output:      map[string]any{"type": "object"},
		Execution:   ir.ToolWebhook, URLEnv: "SAVE_TIER_URL",
		Interruption: ir.ToolProviderDefault, Effect: ir.ToolReturnsData,
	}
	agent.Tasks["collect"] = ir.Task{
		Instructions: "Collect the caller's account tier.",
		Tools:        []string{"save_tier"},
		Result:       map[string]ir.ResultField{"tier": {Type: ir.PrimitiveString}},
		Context:      ir.TaskContext{History: ir.HistoryFull},
	}
	agent.TaskGroups["triage"] = ir.TaskGroup{
		Steps: []string{"collect"}, ContextScope: ir.ContextShared, Then: ir.GroupReturn, Merge: ir.GroupMergeResults,
	}
	agent.Controls["run_collect"] = &ir.Delegate{
		Kind: ir.ControlDelegate, Task: "collect", When: "Collect the caller's account details.",
		Assign: map[string]string{"verified": "result.tier"},
	}
	agent.Controls["run_triage"] = &ir.Delegate{Kind: ir.ControlDelegate, Group: "triage", When: "Run the triage group."}
	intake := agent.Agents["intake"]
	intake.Tools = append(intake.Tools, "run_collect", "run_triage")
	agent.Agents["intake"] = intake
	// Model-written opening: drop the fixed greeting text (V6).
	agent.Conversation.Greeting.Text = ""

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderElevenLabs), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	assertGolden(t, "elevenlabs_workflow.txt", renderApply(artifact), updateElevenLabs, "TestElevenLabsWorkflowGolden", "update-elevenlabs")
}

// TestElevenLabsAssignNeedsJSONTool gates a delegate assign with no JSON-returning
// tool on the task (C3/V1): there is no other mid-call variable write path.
func TestElevenLabsAssignNeedsJSONTool(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	agent.Tasks["collect"] = ir.Task{
		Instructions: "Collect a value.", Result: map[string]ir.ResultField{"tier": {Type: ir.PrimitiveString}},
		Context: ir.TaskContext{History: ir.HistoryFull},
	}
	agent.Controls["run_collect"] = &ir.Delegate{
		Kind: ir.ControlDelegate, Task: "collect", When: "collect", Assign: map[string]string{"verified": "result.tier"},
	}
	intake := agent.Agents["intake"]
	intake.Tools = append(intake.Tools, "run_collect")
	agent.Agents["intake"] = intake

	_, err = Generate(agent, targetByProvider(t, agent, ir.ProviderElevenLabs), target.Default())
	if err == nil || !strings.Contains(err.Error(), "assign requires a tool returning JSON") {
		t.Fatalf("expected assign-without-json-tool gate, got %v", err)
	}
}

// TestElevenLabsBranchPinGated refuses a branch_id pin loudly: branch/draft
// targeting needs the drafts/branches API, not an invented query param (item 3).
func TestElevenLabsBranchPinGated(t *testing.T) {
	agent := elWorkAgent(t)
	tgt := targetByProvider(t, agent, ir.ProviderElevenLabs)
	tgt.Pins = map[string]string{"branch_id": "preview"}
	_, err := Generate(agent, tgt, target.Default())
	if err == nil || !strings.Contains(err.Error(), "branch") {
		t.Fatalf("expected branch_id pin to be gated, got %v", err)
	}
}

// elBody generates the elevenlabs artifact and returns the create-step body for
// one agent as a decoded map.
func elBody(t *testing.T, agent *ir.Agent, tgt ir.Target, capture string) map[string]any {
	t.Helper()
	art, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, step := range art.Apply.Steps {
		if step.CaptureID == capture {
			var body map[string]any
			if err := json.Unmarshal(step.Body, &body); err != nil {
				t.Fatalf("unmarshal %s body: %v", capture, err)
			}
			return body
		}
	}
	t.Fatalf("no step captured %q", capture)
	return nil
}

// promptOf digs conversation_config.agent.prompt out of an agent body.
func promptOf(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	cc, _ := body["conversation_config"].(map[string]any)
	ab, _ := cc["agent"].(map[string]any)
	p, ok := ab["prompt"].(map[string]any)
	if !ok {
		t.Fatalf("no prompt in body %v", body)
	}
	return p
}

func TestElevenLabsFallbackBackupLLM(t *testing.T) { // V5
	agent := elWorkAgent(t)
	profile := agent.Models["fast_reasoning"]
	profile.Fallback = []string{"careful_reasoning"}
	agent.Models["fast_reasoning"] = profile

	prompt := promptOf(t, elBody(t, agent, targetByProvider(t, agent, ir.ProviderElevenLabs), "intake"))
	backup, ok := prompt["backup_llm_config"].(map[string]any)
	if !ok || backup["preference"] != "override" {
		t.Fatalf("backup_llm_config = %v", prompt["backup_llm_config"])
	}
	order, _ := backup["order"].([]any)
	if len(order) != 1 || order[0] != "claude-sonnet-4-5" {
		t.Fatalf("order = %v", backup["order"])
	}
	if prompt["cascade_timeout_seconds"] != float64(elCascadeTimeoutDefault) {
		t.Fatalf("cascade_timeout_seconds = %v", prompt["cascade_timeout_seconds"])
	}
}

func TestElevenLabsUserSpeaksFirst(t *testing.T) { // V6
	agent := elWorkAgent(t)
	agent.Conversation.Greeting = &ir.Greeting{SpeaksFirst: ir.SpeaksFirstUser}
	body := elBody(t, agent, targetByProvider(t, agent, ir.ProviderElevenLabs), "intake")
	cc := body["conversation_config"].(map[string]any)
	ab := cc["agent"].(map[string]any)
	if fm, ok := ab["first_message"]; !ok || fm != "" {
		t.Fatalf("expected empty first_message (native wait), got %v (present=%v)", fm, ok)
	}
}

func TestElevenLabsLanguage(t *testing.T) {
	agent := elWorkAgent(t)
	agent.Language = "es-MX"
	body := elBody(t, agent, targetByProvider(t, agent, ir.ProviderElevenLabs), "intake")
	cc := body["conversation_config"].(map[string]any)
	ab := cc["agent"].(map[string]any)
	if ab["language"] != "es-MX" {
		t.Fatalf("language = %v", ab["language"])
	}
}

func TestElevenLabsCustomLLMEndpoint(t *testing.T) { // V9/C1
	agent := elWorkAgent(t)
	profile := agent.Models["fast_reasoning"]
	profile.Placement = ir.PlacementLocal
	agent.Models["fast_reasoning"] = profile
	tgt := targetByProvider(t, agent, ir.ProviderElevenLabs)
	binding := tgt.Models.Reason["fast_reasoning"]
	binding.Placement = ir.PlacementLocal
	binding.EndpointEnv = "CUSTOM_LLM_URL"
	tgt.Models.Reason["fast_reasoning"] = binding

	prompt := promptOf(t, elBody(t, agent, tgt, "intake"))
	custom, ok := prompt["custom_llm"].(map[string]any)
	if !ok || custom["url"] != "{{env:CUSTOM_LLM_URL}}" {
		t.Fatalf("custom_llm = %v", prompt["custom_llm"])
	}
	if _, hasLLM := prompt["llm"]; hasLLM {
		t.Fatalf("custom-LLM prompt must not also set llm: %v", prompt)
	}
}

func TestElevenLabsVoicemailLeaveMessage(t *testing.T) { // V7
	agent := elWorkAgent(t)
	yes := true
	agent.Channels["phone"] = ir.Channel{
		Kind: ir.ChannelTelephony, Inbound: &yes, Outbound: &yes, OnVoicemail: ir.VoicemailLeaveMessage,
	}
	body := elBody(t, agent, targetByProvider(t, agent, ir.ProviderElevenLabs), "intake")
	vm := builtInToolNamed(t, body, "voicemail_detection")
	if vm == nil {
		t.Fatal("voicemail_detection tool not emitted")
	}
	params, _ := vm["params"].(map[string]any)
	if _, ok := params["voicemail_message"]; !ok {
		t.Fatalf("leave_message must set voicemail_message: %v", params)
	}
	if params["system_tool_type"] != "voicemail_detection" {
		t.Fatalf("built_in_tools entry missing system_tool_type: %v", params)
	}
}

func TestElevenLabsHumanTransferWarmBriefing(t *testing.T) { // V8
	agent := elWorkAgent(t)
	agent.Controls["to_human"] = &ir.HumanTransfer{
		Kind: ir.ControlHumanTransfer, Destination: "billing_line", Mode: ir.TransferWarm,
		Briefing: ir.BriefingMessage, When: "Read this to the operator.",
	}
	tgt := targetByProvider(t, agent, ir.ProviderElevenLabs)
	tgt.Carrier = "twilio" // briefing:message is native-Twilio only (V8)

	body := elBody(t, agent, tgt, "billing")
	xfer := builtInToolNamed(t, body, "transfer_to_number")
	if xfer == nil {
		t.Fatal("transfer_to_number not emitted")
	}
	rule := xfer["params"].(map[string]any)["transfers"].([]any)[0].(map[string]any)
	if rule["transfer_type"] != "conference" {
		t.Fatalf("warm transfer must be conference, got %v", rule["transfer_type"])
	}
	if rule["agent_message"] != "Read this to the operator." {
		t.Fatalf("briefing:message must set agent_message, got %v", rule["agent_message"])
	}
}

// builtInToolNamed digs a system tool out of prompt.built_in_tools by name.
func builtInToolNamed(t *testing.T, body map[string]any, name string) map[string]any {
	t.Helper()
	bi, _ := promptOf(t, body)["built_in_tools"].(map[string]any)
	tool, _ := bi[name].(map[string]any)
	return tool
}

// elWorkAgent is safe_core built for in-code mutation by the T5 unit tests.
func elWorkAgent(t *testing.T) *ir.Agent {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent
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

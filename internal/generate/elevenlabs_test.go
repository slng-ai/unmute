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

// TestElevenLabsWorkflowGolden exercises the Workflow graph safe_core omits:
// a task (assign via a JSON-returning tool), a task_group (then: return), and a
// model-written opening, built in-code (driver-elevenlabs T4/T5, V1/V2/V6).
func TestElevenLabsWorkflowGolden(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "safe_core"))
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
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "safe_core"))
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

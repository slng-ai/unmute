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

var updatePipecatV1 = flag.Bool("update-pipecat", false, "rewrite the pipecat v1 golden")

// TestPipecatV1Golden emits the safe_core project to pipecat and compares the
// full file set byte-for-byte (driver-pipecat T8, V10). Zero Python.
func TestPipecatV1Golden(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
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

	path := filepath.Join("testdata", "golden", "pipecat_v1.txt")
	if *updatePipecatV1 {
		if err := os.WriteFile(path, []byte(out.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want) {
		t.Fatalf("pipecat v1 golden differs; run: go test ./internal/generate -run TestPipecatV1Golden -update-pipecat")
	}
}

// TestPipecatV1TasksGolden exercises the T4 agency level (tasks, task_group,
// delegates) that safe_core omits, by building the IR in-code (driver-pipecat T4).
func TestPipecatV1TasksGolden(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}

	agent.Tasks["collect"] = ir.Task{
		Instructions: "Collect the caller's account tier from the conversation so far.",
		Model:        "careful_reasoning", // per-task model override
		Result: map[string]ir.ResultField{
			"verified_flag": {Type: ir.PrimitiveBoolean},
			"tier":          {Type: ir.PrimitiveString, Enum: []string{"free", "pro"}},
		},
		Context: ir.TaskContext{History: ir.HistoryFull},
	}
	agent.TaskGroups["triage"] = ir.TaskGroup{
		Steps: []string{"collect"}, ContextScope: ir.ContextIsolated, Then: ir.GroupReturn, Merge: ir.GroupMergeResults,
	}
	agent.Controls["run_collect"] = &ir.Delegate{
		Kind: ir.ControlDelegate, Task: "collect", When: "Collect the caller's account details.",
		Assign: map[string]string{"verified": "result.verified_flag"},
	}
	agent.Controls["run_triage"] = &ir.Delegate{Kind: ir.ControlDelegate, Group: "triage", When: "Run the triage group."}
	intake := agent.Agents["intake"]
	intake.Tools = append(intake.Tools, "run_collect", "run_triage")
	agent.Agents["intake"] = intake

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var bot string
	for _, file := range artifact.Files {
		if file.Path == "bot.py" {
			bot = string(file.Content)
		}
	}
	if bot == "" {
		t.Fatal("bot.py not emitted")
	}

	path := filepath.Join("testdata", "golden", "pipecat_v1_tasks_bot.py")
	if *updatePipecatV1 {
		if err := os.WriteFile(path, []byte(bot), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bot != string(want) {
		t.Fatalf("pipecat v1 tasks golden differs; run: go test ./internal/generate -run TestPipecatV1TasksGolden -update-pipecat")
	}
}

// TestPipecatEmitterMatchesCapabilityTable is the table↔emitter agreement test
// (compiler V19 / T12): the emitter's declared code paths must equal the table's
// non-gated Pipecat rows, so no field is validate-green yet silently unemitted.
func TestPipecatEmitterMatchesCapabilityTable(t *testing.T) {
	table := target.Default()
	for field := range table.Fields {
		capability := table.Capability(field, target.Pipecat)
		supported := capability.Tag != target.Gated && capability.Tag != target.Provisional
		if pipecatEmittedFields[field] != supported {
			t.Errorf("field %q: emitter emits=%v, table supported=%v (tag %q) — implement or gate to reconcile",
				field, pipecatEmittedFields[field], supported, capability.Tag)
		}
	}
}

// TestServiceInfoCoversEveryMappedClass guards the provider→service mapping: any
// class the sttService/llmService/ttsService switches can emit MUST have a
// serviceInfo entry, or collectImportsExtras silently drops its import and the
// emitted bot.py won't run. (This is the gap that let `provider: slng` fall
// through to OpenAITTSService with no real SlngTTSService import.)
func TestServiceInfoCoversEveryMappedClass(t *testing.T) {
	mapped := []string{
		"DeepgramSTTService", "OpenAISTTService", "SlngSTTService",
		"OpenAILLMService",
		"ElevenLabsTTSService", "CartesiaTTSService", "SlngTTSService", "OpenAITTSService",
	}
	for _, class := range mapped {
		info, ok := serviceInfo[class]
		if !ok {
			t.Errorf("service class %q has no serviceInfo entry (no import → broken bot.py)", class)
			continue
		}
		if info.Import == "" {
			t.Errorf("service class %q has an empty import line", class)
		}
		if (info.Extra == "") == (info.Dep == "") {
			t.Errorf("service class %q must set exactly one of Extra (pipecat-ai) or Dep (plugin), got extra=%q dep=%q", class, info.Extra, info.Dep)
		}
	}
}

// TestCheckPipecatVersion pins the template-compatible range. The workers model
// + Flows-in-core landed in 1.5.0 (the first 1.x release), so anything below is
// rejected at compile time — this is the guard that the bogus `1.0.3` pin
// (which never existed on PyPI) slipped past before.
func TestCheckPipecatVersion(t *testing.T) {
	for _, tc := range []struct {
		version string
		ok      bool
	}{
		{"1.5.0", true},
		{"1.5.3", true},
		{"1.6.0", true},
		{"1.0.3", false}, // never existed on PyPI; workers API not present
		{"1.4.9", false},
		{"1", false}, // too vague / pre-1.5
		{"0.0.108", false},
		{"2.0.0", false},
		{"", false},
		{"latest", false},
	} {
		err := checkPipecatVersion(tc.version)
		if (err == nil) != tc.ok {
			t.Errorf("checkPipecatVersion(%q): ok=%v, err=%v", tc.version, tc.ok, err)
		}
	}
}

func targetByProvider(t *testing.T, agent *ir.Agent, provider ir.Provider) ir.Target {
	t.Helper()
	for _, resolved := range agent.Targets {
		if resolved.Provider == provider {
			return resolved
		}
	}
	t.Fatalf("no target for provider %q", provider)
	return ir.Target{}
}

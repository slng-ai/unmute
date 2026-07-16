package ir

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	targetcap "github.com/slng/unmute/internal/target"
)

var updateCompilerGolden = flag.Bool("update", false, "rewrite compiler golden files")

func TestCompilerGolden(t *testing.T) { // V4, V11, V14
	safe := safeAgent(t)
	tests := []compilerGoldenCase{
		{"safe_core", safe, allTargets(safe), false},
		thenReturnGolden(t),
		historyGolden(t),
		localPlacementGolden(t),
		mcpGolden(t),
		thinkingAudioGolden(t),
	}

	var output strings.Builder
	for i, test := range tests {
		if i > 0 {
			output.WriteByte('\n')
		}
		report, err := Validate(test.agent, test.targets, targetcap.Default())
		if (err != nil) != test.wantFail {
			t.Fatalf("%s: err=%v report=%#v", test.name, err, report.PerTarget)
		}
		fmt.Fprintf(&output, "CASE %s\n", test.name)
		for _, row := range report.PerTarget {
			status := "PASS"
			if len(row.Errors) > 0 {
				status = "FAIL"
			}
			fmt.Fprintf(&output, "%s (%s): %s\n", row.Name, row.Provider, status)
			warnings := append([]string(nil), row.Warnings...)
			errors := append([]string(nil), row.Errors...)
			slices.Sort(warnings)
			slices.Sort(errors)
			for _, warning := range warnings {
				fmt.Fprintf(&output, "  warning: %s\n", warning)
			}
			for _, validationError := range errors {
				fmt.Fprintf(&output, "  error: %s\n", validationError)
			}
		}
	}

	path := filepath.Join("testdata", "golden", "compiler.txt")
	if *updateCompilerGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(output.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if output.String() != string(want) {
		t.Fatalf("compiler golden differs; run go test ./internal/ir -run TestCompilerGolden -update")
	}
}

type compilerGoldenCase struct {
	name     string
	agent    *Agent
	targets  []Target
	wantFail bool
}

func thenReturnGolden(t *testing.T) compilerGoldenCase {
	agent := safeAgent(t)
	agent.Tasks["collect"] = Task{
		Instructions: "collect", Result: map[string]ResultField{"done": {Type: PrimitiveBoolean}},
		Context: TaskContext{History: HistoryFull},
	}
	agent.TaskGroups["collect_then_return"] = TaskGroup{
		Steps: []string{"collect"}, ContextScope: ContextShared, Then: GroupReturn, Merge: GroupMergeResults,
	}
	return compilerGoldenCase{"then_return", agent, []Target{targetFor(agent, ProviderVapi)}, true}
}

func historyGolden(t *testing.T) compilerGoldenCase {
	agent := safeAgent(t)
	agent.Controls["to_billing"].(*AgentTransfer).Context.History = HistoryMessages
	return compilerGoldenCase{"history_messages", agent, []Target{targetFor(agent, ProviderElevenLabs)}, true}
}

func localPlacementGolden(t *testing.T) compilerGoldenCase {
	agent := safeAgent(t)
	agent.Pipeline.Listen.Placement = PlacementLocal
	return compilerGoldenCase{"local_listen", agent, []Target{targetFor(agent, ProviderVapi), targetFor(agent, ProviderElevenLabs)}, true}
}

func mcpGolden(t *testing.T) compilerGoldenCase {
	agent := safeAgent(t)
	tool := agent.Tools["lookup_customer"]
	tool.Execution = ToolMCP
	tool.URLEnv = ""
	agent.Tools["lookup_customer"] = tool
	return compilerGoldenCase{"mcp", agent, []Target{targetFor(agent, ProviderDeepgram)}, true}
}

func thinkingAudioGolden(t *testing.T) compilerGoldenCase {
	agent := safeAgent(t)
	agent.Conversation.ThinkingAudio = ThinkingSubtle
	return compilerGoldenCase{"thinking_audio", agent, []Target{targetFor(agent, ProviderVapi), targetFor(agent, ProviderDeepgram)}, true}
}

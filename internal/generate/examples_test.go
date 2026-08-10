package generate

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/spec"
	"github.com/slng/unmute/internal/target"
)

func TestExampleMatrixCompilesForCodeTargets(t *testing.T) {
	cases := []struct {
		name                  string
		agents, tasks, groups int
	}{
		{"simple-prompt", 1, 0, 0},
		{"multi-task", 1, 2, 0},
		{"task-groups", 1, 3, 1},
		{"subagents", 2, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pkg, err := spec.Load(filepath.Join("..", "..", "examples", tc.name))
			if err != nil {
				t.Fatal(err)
			}
			agent, err := ir.Build(pkg)
			if err != nil {
				t.Fatal(err)
			}
			if len(agent.Agents) != tc.agents || len(agent.Tasks) != tc.tasks || len(agent.TaskGroups) != tc.groups {
				t.Fatalf("got agents/tasks/groups %d/%d/%d, want %d/%d/%d", len(agent.Agents), len(agent.Tasks), len(agent.TaskGroups), tc.agents, tc.tasks, tc.groups)
			}
			if agent.Tracing == nil || agent.Tracing.Provider != "langfuse" {
				t.Fatalf("tracing = %#v, want langfuse", agent.Tracing)
			}
			if len(agent.Tools) != 5 {
				t.Fatalf("got %d tools, want 5", len(agent.Tools))
			}
			for name, def := range agent.Agents {
				for _, required := range []string{"## Voice contract", "Never speak or emit", "immediately and silently", "Never ask the caller to wait"} {
					if !strings.Contains(def.Instructions, required) {
						t.Errorf("agent %q is missing prompt contract %q", name, required)
					}
				}
			}
			for name, task := range agent.Tasks {
				for _, required := range []string{"## Voice contract", "Never speak or emit", "immediately and silently", "runtime-only"} {
					if !strings.Contains(task.Instructions, required) {
						t.Errorf("task %q is missing prompt contract %q", name, required)
					}
				}
			}
			toolContracts := map[string]string{
				"lookup_customer":    "before any availability",
				"create_customer":    "explicitly gave permission",
				"check_availability": "real, nonempty customer_id",
				"book_appointment":   "Never invent either ID",
				"cancel_appointment": "explicit confirmation",
			}
			for name, tool := range agent.Tools {
				if tool.Execution != ir.ToolLocal || tool.URLEnv != "" {
					t.Errorf("tool %q execution/url = %q/%q, want local/empty", name, tool.Execution, tool.URLEnv)
				}
				if !strings.Contains(tool.Description, toolContracts[name]) {
					t.Errorf("tool %q is missing workflow contract %q", name, toolContracts[name])
				}
			}
			if tc.name == "multi-task" {
				parent := agent.Agents["appointment_desk"].Instructions
				if !strings.Contains(parent, "Don't ask for the caller's name or phone number first") ||
					!strings.Contains(parent, "service, date, or appointment ID first") {
					t.Error("multi-task parent can collect task-owned fields before delegation")
				}
				customer := agent.Tasks["customer_record"].Instructions
				appointment := agent.Tasks["appointment_request"].Instructions
				if !strings.Contains(customer, "Never guess an ID") ||
					!strings.Contains(customer, "explicit permission") ||
					!strings.Contains(appointment, "nonempty customer ID") ||
					!strings.Contains(appointment, "Only after booking succeeds") {
					t.Error("multi-task workflow does not enforce customer and appointment gates")
				}
			}
			for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
				t.Run(string(provider), func(t *testing.T) {
					if _, err := Generate(agent, targetByProvider(t, agent, provider), target.Default()); err != nil {
						t.Fatal(err)
					}
				})
			}
		})
	}
}

// TestPublicExamplesValidateAndGenerate is the shipped-example gate (compiler.md V36):
// every package under examples/ must load, build, validate its **declared**
// targets with zero errors, and generate for each one. Generating without
// validating first would let an example ship that `unmute validate` rejects,
// which is the command a reader runs before anything else.
func TestPublicExamplesValidateAndGenerate(t *testing.T) {
	root := filepath.Join("..", "..", "examples")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			pkg, err := spec.Load(filepath.Join(root, entry.Name()))
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			agent, err := ir.Build(pkg)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if len(agent.Targets) == 0 {
				t.Fatal("example declares no target")
			}
			var declared []ir.Target
			for _, name := range slices.Sorted(maps.Keys(agent.Targets)) {
				declared = append(declared, agent.Targets[name])
			}
			report, err := ir.Validate(agent, declared, target.Default())
			if err != nil {
				t.Fatalf("validate: %v\n%#v", err, report.PerTarget)
			}
			for _, row := range report.PerTarget {
				if len(row.Errors) > 0 {
					t.Errorf("target %q has errors: %v", row.Name, row.Errors)
				}
			}
			for _, resolved := range declared {
				if _, err := Generate(agent, resolved, target.Default()); err != nil {
					t.Errorf("generate %q: %v", resolved.Name, err)
				}
			}
		})
	}
}

func TestPublicExamplePackages(t *testing.T) {
	root := filepath.Join("..", "..", "examples")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var directories []string
	for _, entry := range entries {
		if entry.IsDir() {
			directories = append(directories, entry.Name())
		}
	}
	want := []string{"human-transfer", "multi-task", "outbound-reminder", "salon-support", "simple-prompt", "subagents", "task-groups", "telephony-hello"}
	if !slices.Equal(directories, want) {
		t.Fatalf("public example directories = %v, want %v", directories, want)
	}
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Name() == ".DS_Store" || entry.IsDir() && entry.Name() == "build" {
			t.Errorf("forbidden public example artifact: %s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// The shipped telephony example (telephony-hello) is a complete,
// schema-faithful package with real adapters on both targets: Pipecat
// carrier-websocket and the LiveKit Twilio connector. Both are provisional
// (adapter present, no credentialed smoke yet) and usable, so both generate.
func TestTelephonyExampleGeneratesProvisionalRoute(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "telephony-hello"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	livekit, ok := agent.Targets["livekit"]
	if !ok || livekit.Telephony == nil || livekit.Transport != "connector" {
		t.Fatalf("livekit target is not the resolved connector route: %#v", livekit.Telephony)
	}
	for _, name := range []string{"livekit", "pipecat"} {
		resolved := agent.Targets[name]
		if _, err := Generate(agent, resolved, target.Default()); err != nil {
			t.Fatalf("provisional telephony route %q must generate, got %v", name, err)
		}
	}
}

func TestExampleToolExposure(t *testing.T) {
	domainTools := []string{"lookup_customer", "create_customer", "check_availability", "book_appointment", "cancel_appointment"}
	cases := []struct {
		name       string
		agentTools map[string][]string
		taskTools  map[string][]string
	}{
		{
			name:       "simple-prompt",
			agentTools: map[string][]string{"appointment_desk": domainTools},
		},
		{
			name:       "multi-task",
			agentTools: map[string][]string{"appointment_desk": {"check_customer", "manage_appointment"}},
			taskTools: map[string][]string{
				"customer_record":     {"lookup_customer", "create_customer"},
				"appointment_request": {"check_availability", "book_appointment", "cancel_appointment"},
			},
		},
		{
			name:       "task-groups",
			agentTools: map[string][]string{"appointment_desk": {"manage_appointment"}},
			taskTools: map[string][]string{
				"identify_customer":    {"lookup_customer", "create_customer"},
				"select_appointment":   {"check_availability"},
				"finalize_appointment": {"book_appointment", "cancel_appointment"},
			},
		},
		{
			name: "subagents",
			agentTools: map[string][]string{
				"booking_desk":        {"lookup_customer", "create_customer", "check_availability", "book_appointment", "to_appointment_manager"},
				"appointment_manager": {"lookup_customer", "create_customer", "check_availability", "book_appointment", "cancel_appointment", "to_booking_desk"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pkg, err := spec.Load(filepath.Join("..", "..", "examples", tc.name))
			if err != nil {
				t.Fatal(err)
			}
			agent, err := ir.Build(pkg)
			if err != nil {
				t.Fatal(err)
			}
			for name, want := range tc.agentTools {
				if got := agent.Agents[name].Tools; !slices.Equal(got, want) {
					t.Errorf("agent %q tools = %v, want %v", name, got, want)
				}
			}
			for name, want := range tc.taskTools {
				if got := agent.Tasks[name].Tools; !slices.Equal(got, want) {
					t.Errorf("task %q tools = %v, want %v", name, got, want)
				}
			}
		})
	}
}

// TestFixturePackagesValidate holds the internal fixtures to the same bar as the
// public examples (SPEC V14): safe_core and remy back most of the suite, so a
// fixture that stops validating would otherwise only surface as a confusing
// failure somewhere downstream.
func TestFixturePackagesValidate(t *testing.T) {
	root := filepath.Join("..", "testdata")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, entry.Name(), "agent.yaml")); err != nil {
			continue // not a package (golden dirs, handler fixtures)
		}
		t.Run(entry.Name(), func(t *testing.T) {
			pkg, err := spec.Load(filepath.Join(root, entry.Name()))
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			agent, err := ir.Build(pkg)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			var declared []ir.Target
			for _, name := range slices.Sorted(maps.Keys(agent.Targets)) {
				declared = append(declared, agent.Targets[name])
			}
			report, err := ir.Validate(agent, declared, target.Default())
			if err != nil {
				t.Fatalf("validate: %v\n%#v", err, report.PerTarget)
			}
		})
	}
}

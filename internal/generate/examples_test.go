package generate

import (
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
	want := []string{"multi-task", "simple-prompt", "subagents", "task-groups", "telephony-multi-task"}
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

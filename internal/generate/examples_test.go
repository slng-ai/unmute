package generate

import (
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

// FR-031 / SC-008: an existing package keeps its meaning. The Daily-route work
// must reach the Daily route and nothing else.
//
// Written as a scoping property rather than against a stored copy of every
// example's old output. A byte-for-byte baseline would need a golden per example
// per file, and it would go stale on the first unrelated template edit, at which
// point the honest question ("did this feature widen?") gets buried in noise.
// The property is the same and it is checkable directly: none of this feature's
// additions may appear on any target that is not the Daily route.
//
// A failure here means the change was made unconditionally. Narrow it, do not
// extend this list.
func TestDailyRouteWorkDoesNotReachOtherTargets(t *testing.T) {
	// Every marker this feature adds to an emitted project.
	markers := []string{
		"DailyParams", "pipecat.transports.daily",
		"## Phone calls",
		"Account prerequisites", "route_prerequisites", "daily_dialout",
		// The carrier leg's own markers (SCHEMA N37): the emitted helper, the
		// forward-once guard, and the runbook's opening line. Carrier work must not
		// leak onto a target that is not the Daily route either.
		//
		// The runbook's *heading* would be the obvious third marker and it is the
		// wrong one: the LiveKit SIP route heads its own runbook "Telephony setup"
		// too (specs/005), so it names a shape both drivers share rather than
		// anything this feature added.
		"telephony_helper.py", "_CALL_FORWARDED", "One piece runs outside the platform",
	}
	// _TRANSFER_RESULT used to be on that list and is not any more. It is the
	// one-attempt-per-call guard, and specs/007 reuses the same discipline on the
	// Pipecat Cloud websocket route deliberately (data-model section 6), so it now
	// marks "a route that emits a cold transfer" rather than "the Daily route".
	// The Daily-only markers above are what still scopes this test.
	root := filepath.Join("..", "..", "examples")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	checkedDaily := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pkg, err := spec.Load(filepath.Join(root, entry.Name()))
		if err != nil {
			t.Fatalf("%s: load: %v", entry.Name(), err)
		}
		agent, err := ir.Build(pkg)
		if err != nil {
			t.Fatalf("%s: build: %v", entry.Name(), err)
		}
		for _, name := range slices.Sorted(maps.Keys(agent.Targets)) {
			resolved := agent.Targets[name]
			daily := resolved.Provider == ir.ProviderPipecat && resolved.Transport == "daily-sip"
			artifact, err := Generate(agent, resolved, target.Default())
			if err != nil {
				t.Fatalf("%s/%s: generate: %v", entry.Name(), name, err)
			}
			for _, file := range artifact.Files {
				for _, marker := range markers {
					if !strings.Contains(string(file.Content), marker) {
						continue
					}
					if !daily {
						t.Errorf("%s/%s (%s, transport %q) emits %q: this feature must reach the Daily route only",
							entry.Name(), name, resolved.Provider, resolved.Transport, file.Path)
					}
					checkedDaily = checkedDaily || daily
				}
			}
		}
	}
	// A test that finds the markers nowhere would pass while proving nothing.
	if !checkedDaily {
		t.Error("no example exercises the Daily route, so this test cannot tell scoped from absent")
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
	// One telephony example per use case (spec 007 FR-016): warm+inbound on
	// LiveKit (human-transfer), cold+inbound on Pipecat over Twilio with nothing
	// hosted (human-transfer-cloud-twilio), inbound+outbound (telephony-hello).
	// human-transfer-daily is the no-carrier Daily form and is untouched.
	// human-transfer-daily-twilio was removed with feature 007; its route keeps its
	// guards against internal/testdata/daily_carrier instead.
	want := []string{"human-transfer", "human-transfer-cloud-twilio", "human-transfer-daily", "multi-task", "outbound-reminder", "salon-support", "simple-prompt", "subagents", "task-groups", "telephony-hello"}
	if !slices.Equal(directories, want) {
		t.Fatalf("public example directories = %v, want %v", directories, want)
	}
	// Artifacts must not be *committed*; compiling an example locally is normal
	// and must not fail the suite, so ask git what is tracked rather than
	// walking the working tree (B5).
	tracked, err := exec.Command("git", "ls-files", "examples").Output()
	if err != nil {
		t.Skipf("git ls-files unavailable: %v", err)
	}
	for _, path := range strings.Split(strings.TrimSpace(string(tracked)), "\n") {
		if strings.Contains(path, "/build/") || strings.HasSuffix(path, ".DS_Store") {
			t.Errorf("forbidden committed example artifact: %s", path)
		}
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

// V16/B9: a committed example never ships a literal transfer destination. Any
// literal a repository can contain is a number nobody answers, so a live test
// of the example dials a stranger or a carrier intercept ("el número marcado
// no existe") and the call dies right where the demo should land. Every
// destination in examples/*/targets.yaml is an UPPER_SNAKE env var name,
// resolved from the tester's own environment at call time.
func TestV16_ExampleDestinationsAreEnvironmentNames(t *testing.T) {
	envName := regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	root := filepath.Join("..", "..", "examples")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pkg, err := spec.Load(filepath.Join(root, entry.Name()))
		if err != nil {
			t.Fatalf("load %s: %v", entry.Name(), err)
		}
		for targetName, target := range pkg.Targets {
			for symbol, value := range target.Destinations {
				if !envName.MatchString(value) {
					t.Errorf("%s/%s destination %q = %q is a literal; committed examples must name an env var", entry.Name(), targetName, symbol, value)
				}
			}
		}
	}
}

// TestV11_TransfersDocListsEveryRequiredEnv is SPEC V11/C9: docs/TRANSFERS.md
// is the one place that answers "which secrets do I need", so its tables must
// name every env var the transfer examples' generated .env.example requires.
// A new required name that is not documented fails here, not on a live rig.
func TestV11_TransfersDocListsEveryRequiredEnv(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "TRANSFERS.md"))
	if err != nil {
		t.Fatal(err)
	}
	for example, provider := range map[string]ir.Provider{
		"human-transfer":       ir.ProviderLiveKit,
		"human-transfer-daily": ir.ProviderPipecat,
	} {
		pkg, err := spec.Load(filepath.Join("..", "..", "examples", example))
		if err != nil {
			t.Fatal(err)
		}
		agent, err := ir.Build(pkg)
		if err != nil {
			t.Fatal(err)
		}
		artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
		if err != nil {
			t.Fatalf("%s: %v", example, err)
		}
		for _, line := range strings.Split(artifactFile(t, artifact, ".env.example"), "\n") {
			name, _, found := strings.Cut(line, "=")
			if !found || name == "" || strings.HasPrefix(name, "#") {
				continue
			}
			if !strings.Contains(string(doc), name) {
				t.Errorf("%s requires %s, which docs/TRANSFERS.md does not document (V11)", example, name)
			}
		}
	}
}

// The generated README is the runbook, and almost nobody reads it before they
// have already read the example's own page and the docs. So those two have to stay
// true on their own, and "stay true" is a thing a test can hold rather than a
// thing a person remembers.
//
// These two are deliberately narrow. They do not check prose: a document is free
// to say that an example was removed, which is history worth keeping. They check
// the two claims that go stale silently and mislead a reader who acts on them: a
// link that no longer resolves, and a README describing a route its package no
// longer declares.

// Every relative link an example page offers must resolve, and any link anywhere
// in the docs that points into examples/ must resolve too. Deleting or renaming an
// example fails this until every page that sends a reader there is fixed.
func TestExampleAndDocLinksIntoExamplesResolve(t *testing.T) {
	link := regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)`)
	check := func(page string, onlyExamples bool) {
		raw, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range link.FindAllStringSubmatch(string(raw), -1) {
			target, _, _ := strings.Cut(match[1], "#")
			if target == "" || strings.HasPrefix(target, "http://") ||
				strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			if onlyExamples && !strings.Contains(target, "examples/") {
				continue
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(page), target)); err != nil {
				t.Errorf("%s links to %q, which does not exist", page, target)
			}
		}
	}
	for _, root := range []string{filepath.Join("..", "..", "examples"), filepath.Join("..", "..", "docs")} {
		onlyExamples := strings.HasSuffix(root, "docs")
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() && entry.Name() == "build" {
				return fs.SkipDir
			}
			if !entry.IsDir() && strings.HasSuffix(path, ".md") {
				check(path, onlyExamples)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// An example's own README must name every route its targets declare. This is the
// one that would have caught `telephony-hello` describing a carrier-websocket
// Pipecat target for the length of the feature that moved it to another route: the
// generated runbook was right the whole time, and the page a reader opens first
// was wrong.
func TestExampleReadmesNameTheirDeclaredTransports(t *testing.T) {
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
			var routed []string
			for name, target := range pkg.Targets {
				if target.Transport != "" {
					routed = append(routed, name)
				}
			}
			if len(routed) == 0 {
				// Nothing declares a route, so there is no route claim to keep true.
				// The four structural examples live in the index table in
				// examples/README.md and need no page of their own.
				return
			}
			readme, err := os.ReadFile(filepath.Join(root, entry.Name(), "README.md"))
			if err != nil {
				t.Fatalf("this example declares a route (%s) and has no README to describe it: %v", strings.Join(routed, ", "), err)
			}
			for name, target := range pkg.Targets {
				if target.Transport == "" {
					continue // browser-only targets declare no route to describe
				}
				if !strings.Contains(string(readme), target.Transport) {
					t.Errorf("target %q declares transport %q, which this example's README never mentions", name, target.Transport)
				}
			}
		})
	}
}

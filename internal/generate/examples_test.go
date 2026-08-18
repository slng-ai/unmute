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

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

func TestV3_OutboundReminderBusinessToolsAreSelfContained(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "outbound-reminder"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range agent.Secrets {
		if strings.HasPrefix(secret, "SALON_API_") {
			t.Errorf("outbound live example depends on unrelated salon API secret %q", secret)
		}
	}
	for _, name := range []string{"confirm_appointment", "reschedule_appointment", "cancel_appointment"} {
		tool := agent.Tools[name]
		if tool.Execution != ir.ToolLocal || tool.HandlerSource == "" || tool.URLEnv != "" {
			t.Errorf("tool %q execution/handler/url = %q/%t/%q, want local/nonempty/empty", name, tool.Execution, tool.HandlerSource != "", tool.URLEnv)
		}
	}
}

func TestSalonConciergeFeatureContract(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "salon-concierge"))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}

	if resolved.Tracing == nil || resolved.Tracing.Provider != "langfuse" {
		t.Fatalf("tracing = %#v, want Langfuse", resolved.Tracing)
	}
	for _, name := range []string{"LANGFUSE_BASE_URL", "LANGFUSE_PUBLIC_KEY", "LANGFUSE_SECRET_KEY"} {
		if !slices.Contains(resolved.Secrets, name) {
			t.Errorf("secrets omit %s", name)
		}
	}
	manager, ok := resolved.Controls["to_manager"].(*ir.HumanTransfer)
	if !ok || manager.Mode != ir.TransferCold || manager.OnUnavailable != ir.OnUnavailableHangup {
		t.Fatalf("manager transfer = %#v, want cold transfer with hangup fallback", resolved.Controls["to_manager"])
	}

	for name, control := range resolved.Controls {
		transfer, ok := control.(*ir.AgentTransfer)
		if !ok {
			continue
		}
		if transfer.Announce != "" || transfer.Context.History != ir.HistoryFull ||
			!transfer.Context.Variables.All || !slices.Equal(transfer.Requires, []string{"customer_id"}) {
			t.Errorf("internal handoff %q must stay silent and carry verified context: %#v", name, transfer)
		}
	}

	for name, agent := range resolved.Agents {
		if (name == "chat_with_me") != slices.Contains(agent.Tools, "web_search") {
			t.Errorf("agent %q has unexpected web_search access: %v", name, agent.Tools)
		}
	}
	for name, task := range resolved.Tasks {
		if slices.Contains(task.Tools, "web_search") {
			t.Errorf("task %q exposes web_search", name)
		}
	}
	for name, tool := range resolved.Tools {
		if name == "web_search" {
			if tool.Execution != ir.ToolMCP || tool.URLEnv != "FIRECRAWL_MCP_URL" ||
				tool.Auth == nil || tool.Auth.Type != ir.ToolAuthBearer ||
				tool.Auth.TokenEnv != "FIRECRAWL_API_KEY" ||
				!slices.Equal(tool.MCPTools, []string{"firecrawl_search"}) {
				t.Errorf("web_search = %#v, want Firecrawl MCP", tool)
			}
		} else if tool.Execution != ir.ToolLocal || tool.Handler != "tools/salon.py" {
			t.Errorf("tool %q = %#v, want shared local Python handler", name, tool)
		}
	}
	for _, name := range []string{"create_booking", "modify_booking", "cancel_booking"} {
		tool := resolved.Tools[name]
		properties := tool.Input["properties"].(map[string]any)
		confirmed := properties["confirmed"].(map[string]any)
		required := tool.Input["required"].([]any)
		if confirmed["type"] != "boolean" || !slices.Contains(required, any("confirmed")) {
			t.Errorf("tool %q must require boolean confirmed: %#v", name, tool.Input)
		}
	}

	requireText := func(name, text string, wants ...string) {
		t.Helper()
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Errorf("%s omits %q", name, want)
			}
		}
	}
	requireText("verification", resolved.Tasks["customer_verification"].Instructions,
		"Accumulate the full name and phone number across turns",
		"A complete phone has 10 to 15 digits", "incomplete fragment",
		"consume the one invalid-value retry",
		"one initial customer lookup only after both the full name")
	requireText("prepare booking", resolved.Tasks["prepare_booking"].Instructions,
		"Only an unambiguous yes", "an unclear answer, silence, a topic change")
	requireText("apply booking", resolved.Tasks["apply_booking"].Instructions,
		"false, missing", "anything other than true", "Do not call a mutation",
		"shared preparation result's exact `confirmed`")
	requireText("chat", resolved.Agents["chat_with_me"].Instructions,
		"required before the session greets the caller", "current information is unavailable")
	requireText("complaints", resolved.Agents["complaint_specialist"].Instructions,
		"no active phone leg", "reaches the carrier", "route may hang up")
}

func TestExampleMatrixCompilesForCodeTargets(t *testing.T) {
	// Among these four structural comparison packages, tracing stays on only
	// simple-prompt. It used to be on all four, which made
	// the first package in the table impossible to run without a Langfuse
	// account: three third-party values before a first-time reader hears a word
	// (research D12). Which one keeps it is a table row in examples/README.md,
	// and this is the test that holds the count at one.
	cases := []struct {
		name                  string
		agents, tasks, groups int
		tracing               bool
	}{
		{"simple-prompt", 1, 0, 0, true},
		{"multi-task", 1, 2, 0, false},
		{"task-groups", 1, 3, 1, false},
		{"subagents", 2, 0, 0, false},
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
			if tracing := agent.Tracing != nil && agent.Tracing.Provider == "langfuse"; tracing != tc.tracing {
				t.Fatalf("tracing = %t, want %t: only simple-prompt in this comparison configures it", tracing, tc.tracing)
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
				"check_availability": "This tool accepts only service and date",
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
		if _, err := os.Stat(filepath.Join(root, entry.Name(), "agent.yaml")); err != nil {
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
		// too, so it names a shape both drivers share rather than one route.
		"telephony_helper.py", "call_forwarded", "One piece runs outside the platform",
	}
	// _transfer_result used to be on that list and is not any more. It is the
	// one-attempt-per-call guard shared by both Pipecat transfer routes, so it now
	// marks "a route that emits a cold transfer" rather than "the Daily route".
	// The Daily-only markers above are what still scopes this test. Public
	// examples prove they do not leak; an internal fixture proves they still emit.
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
		if _, err := os.Stat(filepath.Join(root, entry.Name(), "agent.yaml")); err != nil {
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
	dailyPkg, err := spec.Load(filepath.Join("..", "testdata", "daily_carrier"))
	if err != nil {
		t.Fatal(err)
	}
	dailyAgent, err := ir.Build(dailyPkg)
	if err != nil {
		t.Fatal(err)
	}
	dailyArtifact, err := Generate(dailyAgent, dailyAgent.Targets["pipecat"], target.Default())
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range dailyArtifact.Files {
		for _, marker := range markers {
			checkedDaily = checkedDaily || strings.Contains(string(file.Content), marker)
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
			if _, err := os.Stat(filepath.Join(root, entry.Name(), "agent.yaml")); err != nil {
				continue
			}
			directories = append(directories, entry.Name())
		}
	}
	// The focused telephony examples stay one per use case (spec 007 FR-016):
	// warm+inbound on LiveKit (livekit-human-transfer), cold+inbound on Pipecat
	// over Twilio with nothing hosted (pipecat-human-transfer-twilio), and
	// inbound+outbound (twilio-telephony-hello). salon-concierge is the composite
	// release fixture. Daily route guards remain against internal test fixtures.
	//
	// A telephony example whose behaviour is one provider's names that provider
	// first, because the route is the thing a reader is choosing between.
	want := []string{"livekit-human-transfer", "mcp-example", "multi-task", "outbound-reminder", "pipecat-human-transfer-twilio", "regional-infrastructure", "salon-concierge", "salon-support", "simple-prompt", "subagents", "task-groups", "twilio-telephony-hello"}
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

func TestRepositoryKeepsSpecsPrivateAndDocsFocused(t *testing.T) {
	repo := filepath.Join("..", "..")
	tracked, err := exec.Command("git", "-C", repo, "ls-files", "--", "specs", "docs").Output()
	if err != nil {
		t.Fatalf("list tracked specs and docs: %v", err)
	}
	want := "docs/ARCHITECTURE.md\ndocs/HARNESS_TEST.md"
	if got := strings.TrimSpace(string(tracked)); got != want {
		t.Errorf("tracked specs and docs = %q, want the focused allow-list %q", got, want)
	}
	if err := exec.Command("git", "-C", repo, "check-ignore", "-q", "--", "specs/.unmute-ignore-probe/spec.md").Run(); err != nil {
		t.Errorf("specs/ is not ignored: %v", err)
	}
}

// The shipped telephony example (twilio-telephony-hello) is a complete,
// schema-faithful package carrying the route each platform recommends for Twilio:
// Pipecat on the platform's own carrier stream, and LiveKit on a SIP trunk. Both
// are provisional (adapter present, no credentialed smoke yet) and usable, so both
// generate.
//
// The transports are asserted by name because that pairing is the example's whole
// subject. It used to pair cloud-websocket with the LiveKit Twilio connector, which
// tested better on a laptop and taught a route with no transfer primitive; the
// connector keeps its own coverage through examples/outbound-reminder.
func TestTelephonyExampleGeneratesProvisionalRoute(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "twilio-telephony-hello"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	for name, transport := range map[string]string{"livekit": "sip", "pipecat": "cloud-websocket"} {
		resolved, ok := agent.Targets[name]
		if !ok || resolved.Telephony == nil || resolved.Transport != transport {
			t.Fatalf("target %q is not the resolved %s route: %#v", name, transport, resolved.Telephony)
		}
		if resolved.Carrier != "twilio" {
			t.Fatalf("target %q carrier = %q, want twilio", name, resolved.Carrier)
		}
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
		if _, err := os.Stat(filepath.Join(root, entry.Name(), "agent.yaml")); err != nil {
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

// Every environment variable a telephony example's generated .env.example lists
// must be accounted for in that example's own README and on the secrets
// reference page. A reader who sets everything both pages name has a package
// that runs; one who does not finds out on a live call, which is the failure
// this check exists to make impossible (spec FR-005f, FR-027a).
//
// docs-site/reference/secrets.mdx is the public page that answers "which
// variables does this agent need".
//
// DAILY_API_KEY is the case that forced this. It is exempt from `secrets:`
// because no author writes it — the route's own runtime supplies it — and it is
// still required at runtime, so the only place it can be explained is prose.
//
// Scoped to the five telephony examples on purpose (FR-005f0): four of the
// other examples ship no README at all, so widening this is a separate change
// with its own writing to do, not a flag to flip here.
//
// The two halves are scoped differently, because they answer different
// questions. The example's own README must account for **every** name, since it
// is the page a reader of that example follows. The shared secrets page must
// account for every name the package never declares in `secrets:` — the ones the
// runtime supplies, like DAILY_API_KEY and REDIS_URL. Those are exactly the
// names nothing in the package mentions, so a shared page is the only place they
// can be explained. A tool's own webhook credentials are the README's job.
//
// One direction only. It never fails on a name a page mentions and
// .env.example does not: a page is free to name a variable to say the reader
// does not set it, or to teach a name that is not a variable at all.
func TestTelephonyExampleDocsAccountForEveryRequiredEnv(t *testing.T) {
	sharedPage := filepath.Join("..", "..", "docs-site", "reference", "secrets.mdx")
	secretsPage, err := os.ReadFile(sharedPage)
	if err != nil {
		t.Fatal(err)
	}
	for example, providers := range map[string][]ir.Provider{
		"twilio-telephony-hello":        {ir.ProviderPipecat, ir.ProviderLiveKit},
		"livekit-human-transfer":        {ir.ProviderLiveKit},
		"pipecat-human-transfer-twilio": {ir.ProviderPipecat},
		"outbound-reminder":             {ir.ProviderPipecat, ir.ProviderLiveKit},
		"salon-concierge":               {ir.ProviderPipecat, ir.ProviderLiveKit},
	} {
		t.Run(example, func(t *testing.T) {
			readme, err := os.ReadFile(filepath.Join("..", "..", "examples", example, "README.md"))
			if err != nil {
				t.Fatal(err)
			}
			pkg, err := spec.Load(filepath.Join("..", "..", "examples", example))
			if err != nil {
				t.Fatal(err)
			}
			agent, err := ir.Build(pkg)
			if err != nil {
				t.Fatal(err)
			}
			for _, provider := range providers {
				artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
				if err != nil {
					t.Fatalf("%s: %v", provider, err)
				}
				for _, line := range strings.Split(artifactFile(t, artifact, ".env.example"), "\n") {
					name, _, found := strings.Cut(line, "=")
					name = strings.TrimSpace(name)
					if !found || name == "" || strings.HasPrefix(name, "#") {
						continue
					}
					if !strings.Contains(string(readme), name) {
						t.Errorf("%s needs %s, which this example's README never names", provider, name)
					}
					if slices.Contains(agent.Secrets, name) {
						continue // the package declares it, so the package explains it
					}
					if !strings.Contains(string(secretsPage), name) {
						t.Errorf("%s needs %s, which nothing in the package declares and "+
							"%s never names", provider, name, sharedPage)
					}
				}
			}
		})
	}
}

// US2 / FR-018: a phone package runs in the browser with nothing but model
// keys unless it exposes warm transfer there. The emitted startup check is
// where that is decided, so it is where it is asserted for packages whose
// browser tools do not dial through the route. The warm-transfer exception has
// its exact five-name contract in livekit_phone_env_test.go.
//
// This already worked and nothing wrote it down, which is what made it safe to
// move route resolution underneath it. It is the most common workflow in the
// project and it had no test.
func TestBrowserPathStartupCheckAsksForNoRouteEnvironment(t *testing.T) {
	routeOnly := []string{
		"TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN", "TWILIO_PHONE_NUMBER",
		"SIP_TRUNK_HOSTNAME", "SIP_AUTH_USERNAME", "SIP_AUTH_PASSWORD", "SIP_FROM_NUMBER",
		"UNMUTE_PUBLIC_URL", "UNMUTE_OUTBOUND_TOKEN", "REDIS_URL",
		"PIPECAT_CLOUD_ORGANIZATION", "MANAGER_PHONE_NUMBER",
	}
	for example, providers := range map[string][]ir.Provider{
		"twilio-telephony-hello":        {ir.ProviderPipecat, ir.ProviderLiveKit},
		"pipecat-human-transfer-twilio": {ir.ProviderPipecat},
		"outbound-reminder":             {ir.ProviderPipecat, ir.ProviderLiveKit},
		"salon-concierge":               {ir.ProviderPipecat, ir.ProviderLiveKit},
	} {
		t.Run(example, func(t *testing.T) {
			pkg, err := spec.Load(filepath.Join("..", "..", "examples", example))
			if err != nil {
				t.Fatal(err)
			}
			agent, err := ir.Build(pkg)
			if err != nil {
				t.Fatal(err)
			}
			for _, provider := range providers {
				resolved := targetByProvider(t, agent, provider)
				if resolved.Telephony == nil {
					t.Fatalf("%s declares no route, so it is the wrong fixture", provider)
				}
				artifact, err := Generate(agent, resolved, target.Default())
				if err != nil {
					t.Fatalf("%s: %v", provider, err)
				}
				entry := "bot.py"
				if provider == ir.ProviderLiveKit {
					entry = "agent.py"
				}
				source := artifactFile(t, artifact, entry)
				start := strings.Index(source, "REQUIRED_ENV = [")
				if start < 0 {
					t.Fatalf("%s: %s has no REQUIRED_ENV startup check", provider, entry)
				}
				block := source[start : start+strings.Index(source[start:], "]")]
				for _, name := range routeOnly {
					if strings.Contains(block, `"`+name+`"`) {
						t.Errorf("%s: the browser path's startup check demands %q, so a package "+
							"that only wants a browser session cannot start:\n%s", provider, name, block)
					}
				}
			}
		})
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

// Every relative link an example page offers must resolve, and any link in the
// architecture or docs site that points into examples/ must resolve too. Deleting
// or renaming an example fails this until every page that sends a reader there is
// fixed.
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
	for _, source := range []struct {
		root, extension string
		onlyExamples    bool
	}{
		{filepath.Join("..", "..", "examples"), ".md", false},
		{filepath.Join("..", "..", "docs"), ".md", true},
		{filepath.Join("..", "..", "docs-site"), ".mdx", true},
	} {
		err := filepath.WalkDir(source.root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() && entry.Name() == "build" {
				return fs.SkipDir
			}
			if !entry.IsDir() && strings.HasSuffix(path, source.extension) {
				check(path, source.onlyExamples)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// An example's own README must name every route its targets declare. This is the
// one that would have caught `twilio-telephony-hello` describing a carrier-websocket
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
		if _, err := os.Stat(filepath.Join(root, entry.Name(), "agent.yaml")); err != nil {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			pkg, err := spec.Load(filepath.Join(root, entry.Name()))
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			// Read the route from the connections, which is where it is declared.
			// Reading it off the targets here, as this test did before feature
			// 008, would now find nothing on every example and pass without
			// checking anything (spec FR-027).
			var routed []string
			for name, connection := range pkg.Connections {
				if connection.Transport != "" {
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
			for name, connection := range pkg.Connections {
				if connection.Transport == "" {
					continue
				}
				if !strings.Contains(string(readme), connection.Transport) {
					t.Errorf("connection %q declares transport %q, which this example's README never mentions", name, connection.Transport)
				}
			}
		})
	}
}

package scaffold

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

var update = flag.Bool("update", false, "rewrite golden files")

// manifest renders the created tree to a single deterministic blob: every file
// sorted, prefixed by its relative path.
func manifest(t *testing.T, dir string, created []string) []byte {
	t.Helper()
	sort.Strings(created)
	var b bytes.Buffer
	for _, p := range created {
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			t.Fatal(err)
		}
		c, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		b.WriteString("=== " + rel + " ===\n")
		b.Write(c)
		b.WriteByte('\n')
	}
	return b.Bytes()
}

// TestWrite_golden covers what `unmute init` actually writes, tools included.
// It used to call Write with no tools while internal/cli/init.go passes
// DefaultTools(), so the two `tools:` blocks and tools/end_call.yaml that every
// real init writes had no golden coverage at all: a change to the default tool
// template was invisible to `go test ./...`.
func TestWrite_golden(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "support-bot")
	created, err := Write(dir, Data{Name: "support-bot", AgentName: "support-bot", Tools: DefaultTools()})
	if err != nil {
		t.Fatal(err)
	}
	got := manifest(t, dir, created)

	golden := "testdata/golden/init.txt"
	if *update {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("missing golden; run: go test ./internal/scaffold -update")
	}
	if !bytes.Equal(got, want) {
		t.Errorf("scaffold drift; run: go test ./internal/scaffold -update")
	}
}

func TestWriteDefaultFileSet(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agent")
	created, err := Write(dir, Data{Name: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, path := range created {
		name, err := filepath.Rel(dir, path)
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	// .gitignore is one of the five, and it is not optional: agent.yaml and
	// .env.example both tell the author that `.env` is not committed, and
	// nothing was making that true.
	want := []string{".env.example", ".gitignore", "agent.yaml", "instructions.md", "targets.yaml"}
	if !slices.Equal(names, want) {
		t.Fatalf("default files = %v, want %v", names, want)
	}
	for path, forbidden := range map[string]string{"agent.yaml": "tracing:", ".env.example": "LANGFUSE_"} {
		content, err := os.ReadFile(filepath.Join(dir, path))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(content), forbidden) {
			t.Errorf("%s contains opt-in tracing token %q", path, forbidden)
		}
	}
}

// TestScaffoldAgreesWithItsOwnBuild compiles what `unmute init` wrote and holds
// the two `.env.example` files to the same set of names.
//
// They share no code and they cannot be unified cheaply: the package-root file
// renders from the wizard struct, the generated one from the resolved IR, and
// the root file reads a Transport the IR never sees because the scaffold never
// writes it to disk. That is structurally why they can disagree — the root file
// asked for DAILY_API_KEY and the build did not (reproduction.md F). A test is
// the shortest thing that fails if the drift comes back (research D9).
// Run for both shipped targets, not just the default. Keyed on DefaultTarget
// alone this covered whichever target happened to be the default and silently
// dropped the other the day that constant moved.
func TestScaffoldAgreesWithItsOwnBuild(t *testing.T) {
	want := []string{"OPENAI_API_KEY", "SLNG_API_KEY"}
	for _, name := range []string{"pipecat", "livekit"} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "hello-agent")
			data := Data{Name: "hello-agent", Tools: DefaultTools()}
			data.SetTarget(name)
			if _, err := Write(dir, data); err != nil {
				t.Fatal(err)
			}
			pkg, err := spec.Load(dir)
			if err != nil {
				t.Fatalf("the scaffold wrote a package the compiler cannot load: %v", err)
			}
			agent, err := ir.Build(pkg)
			if err != nil {
				t.Fatalf("the scaffold wrote a package the compiler cannot build: %v", err)
			}
			artifact, err := generate.Generate(agent, agent.Targets[name], targetcap.Default())
			if err != nil {
				t.Fatalf("the scaffold wrote a package the compiler cannot generate: %v", err)
			}

			root, err := os.ReadFile(filepath.Join(dir, ".env.example"))
			if err != nil {
				t.Fatal(err)
			}
			files := map[string][]byte{}
			for _, file := range artifact.Files {
				files[file.Path] = file.Content
			}
			built := files[".env.example"]
			if built == nil {
				t.Fatal("the build has no .env.example to agree with")
			}
			if got := envNames(string(root)); !slices.Equal(got, want) {
				t.Errorf("the package root asks for %v, want exactly %v", got, want)
			}
			if got := envNames(string(built)); !slices.Equal(got, want) {
				t.Errorf("build/%s asks for %v, want exactly %v", name, got, want)
			}

			if name == "livekit" {
				var report struct {
					RequiredEnv []string `json:"required_env"`
				}
				if err := json.Unmarshal(files["compile-report.json"], &report); err != nil {
					t.Fatal(err)
				}
				startupEnv := regexp.MustCompile(`(?s)REQUIRED_ENV = \[.*?\]`).Find(files["agent.py"])
				if startupEnv == nil {
					t.Fatal("agent.py has no REQUIRED_ENV startup check")
				}
				for _, platform := range []string{"LIVEKIT_API_KEY", "LIVEKIT_API_SECRET", "LIVEKIT_URL"} {
					if !slices.Contains(report.RequiredEnv, platform) {
						t.Errorf("compile-report.json lost runtime requirement %s: %v", platform, report.RequiredEnv)
					}
					if !bytes.Contains(startupEnv, []byte(platform)) {
						t.Errorf("agent.py startup checks lost runtime requirement %s", platform)
					}
				}
			}
		})
	}
}

// envNames pulls the UPPER_SNAKE names out of an env file, ignoring comments and
// blanks. A commented-out name is not asked for, so it does not count.
func envNames(content string) []string {
	var names []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if name, _, ok := strings.Cut(line, "="); ok {
			names = append(names, strings.TrimSpace(name))
		}
	}
	sort.Strings(names)
	return names
}

// TestScaffoldKeepsItsPromiseAboutDotEnv holds a claim two scaffolded files
// make. `agent.yaml` says values go in "`.env`, which is gitignored" and
// `.env.example` says "`.env` is never committed". Neither was true: `unmute
// init` wrote no `.gitignore`, so a first-time author who followed those
// instructions and ran `git add -A` staged their keys (Wave C, 2026-08-15).
func TestScaffoldKeepsItsPromiseAboutDotEnv(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hello-agent")
	if _, err := Write(dir, Data{Name: "hello-agent", Tools: DefaultTools()}); err != nil {
		t.Fatal(err)
	}
	ignore, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("two scaffolded files promise .env is gitignored: %v", err)
	}
	for _, want := range []string{".env", "!.env.example", "build/"} {
		if !strings.Contains(string(ignore), want) {
			t.Errorf(".gitignore does not carry %q:\n%s", want, ignore)
		}
	}
	// The claim is only worth making where it is made, so check both files still
	// make it. If one of them stops, this test should be deleted with it.
	for _, path := range []string{"agent.yaml", ".env.example"} {
		content, err := os.ReadFile(filepath.Join(dir, path))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), ".env") {
			t.Errorf("%s no longer mentions .env; is this test still holding anything?", path)
		}
	}
}

// TestScaffoldPromptIsOnlyForTheAgent holds the instructions file to being what
// it is: the system prompt, sent verbatim. A note addressed to the author
// reaches the model instead, which is then told to edit a file it cannot see —
// and the first draft of this scaffold shipped exactly that (Wave C).
func TestScaffoldPromptIsOnlyForTheAgent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hello-agent")
	if _, err := Write(dir, Data{Name: "hello-agent", Tools: DefaultTools()}); err != nil {
		t.Fatal(err)
	}
	prompt, err := os.ReadFile(filepath.Join(dir, "instructions.md"))
	if err != nil {
		t.Fatal(err)
	}
	// Second person addressed at the reader of the repository rather than at the
	// agent. "you are", "you did not hear" and the like are the agent's own
	// instructions and are fine; these are not.
	for _, addressed := range []string{
		"Replace this file", "replace this file", "this file with what",
		"unmute ", "agent.yaml", "targets.yaml", ".env",
	} {
		if strings.Contains(string(prompt), addressed) {
			t.Errorf("instructions.md says %q; it is the system prompt, so that reaches the model:\n%s", addressed, prompt)
		}
	}
}

// TestScaffoldPromptMatchesItsChannel holds the scaffold to the channel it
// actually writes. `AllChannels()` hardcodes one web realtime_audio channel and
// there is no scaffold path that emits telephony, so a prompt about a phone call
// describes something the package cannot do. The mechanism was worse than it
// looked: withDefaults set Instructions before it set Channel, so the channel was
// not merely ignored, it was unreadable at the point the prompt was chosen.
func TestScaffoldPromptMatchesItsChannel(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hello-agent")
	if _, err := Write(dir, Data{Name: "hello-agent", Tools: DefaultTools()}); err != nil {
		t.Fatal(err)
	}
	pkg, err := spec.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	for name, channel := range pkg.Agent.Channels {
		if channel.Kind == "telephony" {
			t.Fatalf("channel %q is telephony, so this test is checking the wrong thing", name)
		}
	}
	instructions, err := os.ReadFile(filepath.Join(dir, "instructions.md"))
	if err != nil {
		t.Fatal(err)
	}
	phone := regexp.MustCompile(`(?i)\b(call|calling|caller|phone)\b`)
	for _, surface := range []struct{ what, text string }{
		{"the greeting", pkg.Agent.Conversation.Greeting.Text},
		{"instructions.md", string(instructions)},
	} {
		if got := phone.FindString(surface.text); got != "" {
			t.Errorf("%s says %q, and the only channel this package has is a browser: %q", surface.what, got, surface.text)
		}
	}
}

func TestWrite_customData(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "support-bot")
	data := Data{Name: "support-bot", Greeting: `Hello \ caller?`, Instructions: "Be brief and warm."}
	if _, err := Write(dir, data); err != nil {
		t.Fatal(err)
	}

	agentYAML, err := os.ReadFile(filepath.Join(dir, "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(agentYAML, &parsed); err != nil {
		t.Fatal(err)
	}
	greeting := parsed["conversation"].(map[string]any)["greeting"].(map[string]any)
	if greeting["text"] != data.Greeting {
		t.Errorf("greeting.text = %v, want %q", greeting["text"], data.Greeting)
	}

	instructions, err := os.ReadFile(filepath.Join(dir, "instructions.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(instructions)) != data.Instructions {
		t.Errorf("instructions.md = %q, want %q", strings.TrimSpace(string(instructions)), data.Instructions)
	}
}

func TestWrite_targetChoices(t *testing.T) {
	for _, tc := range []struct {
		target      string
		targetsWant []string // infrastructure + listen/turn plumbing (N15)
		agentWant   []string // model definitions live in agent.yaml now
		env         []string
	}{
		{
			target:      "livekit",
			targetsWant: []string{"livekit:", "provider: livekit", "sdk_language: python"},
			// params arrives as compact JSON from the wizard and is rendered as
			// block YAML like everything else, with `1` still an integer rather
			// than widened to `1.0`.
			agentWant: []string{`voice: "aura-2-thalia-en"`, "      params:\n        speed: 1\n", "provider: slng", "listen:", "turn:"},
			env:       []string{"OPENAI_API_KEY=", "SLNG_API_KEY="},
		},
	} {
		t.Run(tc.target, func(t *testing.T) {
			data := Data{Name: "agent", Channel: "web"}
			data.SetTarget(tc.target)
			data.Speak.Params = `{"speed":1}`
			data.Speak.Language = "es-MX" // per-model language (N16)
			dir := filepath.Join(t.TempDir(), "agent")
			if _, err := Write(dir, data); err != nil {
				t.Fatal(err)
			}
			targets, err := os.ReadFile(filepath.Join(dir, "targets.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			var parsed map[string]any
			if err := yaml.Unmarshal(targets, &parsed); err != nil {
				t.Fatalf("targets.yaml: %v\n%s", err, targets)
			}
			if strings.Contains(string(targets), "-dev") {
				t.Errorf("targets.yaml instance must be named after the provider, not -dev:\n%s", targets)
			}
			for _, want := range tc.targetsWant {
				if !strings.Contains(string(targets), want) {
					t.Errorf("targets.yaml missing %q:\n%s", want, targets)
				}
			}
			agent, err := os.ReadFile(filepath.Join(dir, "agent.yaml"))
			if err != nil || !strings.Contains(string(agent), "language: es-MX") {
				t.Fatalf("agent language: err=%v\n%s", err, agent)
			}
			for _, want := range tc.agentWant {
				if !strings.Contains(string(agent), want) {
					t.Errorf("agent.yaml missing %q:\n%s", want, agent)
				}
			}
			env, err := os.ReadFile(filepath.Join(dir, ".env.example"))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.env {
				if !strings.Contains(string(env), want) {
					t.Errorf(".env.example missing %q:\n%s", want, env)
				}
			}
		})
	}
}

func TestDefaultToolsScaffoldsEndCall(t *testing.T) {
	defaults := DefaultTools()
	if len(defaults) != 1 || defaults[0].Name != "end_call" || defaults[0].Execution != "builtin" || defaults[0].Builtin != "end_call" {
		t.Fatalf("DefaultTools() = %#v", defaults)
	}

	data := Data{Name: "agent", Tools: DefaultTools()}
	data.SetTarget("pipecat")
	dir := filepath.Join(t.TempDir(), "agent")
	if _, err := Write(dir, data); err != nil {
		t.Fatal(err)
	}

	agent, err := os.ReadFile(filepath.Join(dir, "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(agent), "- end_call") {
		t.Errorf("entry agent must reference end_call:\n%s", agent)
	}
	tool, err := os.ReadFile(filepath.Join(dir, "tools", "end_call.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"builtin:", "  id: end_call"} {
		if !strings.Contains(string(tool), want) {
			t.Errorf("end_call.yaml missing %q:\n%s", want, tool)
		}
	}
	if strings.Contains(string(tool), "input:") || strings.Contains(string(tool), "url_env:") {
		t.Errorf("builtin tool must not carry input/url_env:\n%s", tool)
	}
	if _, err := Preflight(data); err != nil {
		t.Fatalf("Preflight() = %v", err)
	}
}

func TestWriteVariablesAndTools(t *testing.T) {
	data := Data{
		Name:    "agent",
		Channel: "web",
		Variables: []Variable{{
			Name: "customer_id", Type: "string", Default: `"guest"`, Source: "call_start",
		}},
		Tools: []Tool{{
			Name: "lookup_customer", Description: "Look up the caller", URLEnv: "LOOKUP_URL", Input: `{"type":"object"}`,
		}},
	}
	data.SetTarget("pipecat")
	dir := filepath.Join(t.TempDir(), "agent")
	if _, err := Write(dir, data); err != nil {
		t.Fatal(err)
	}

	agent, err := os.ReadFile(filepath.Join(dir, "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"customer_id:", "source: call_start", "- lookup_customer"} {
		if !strings.Contains(string(agent), want) {
			t.Errorf("agent.yaml missing %q:\n%s", want, agent)
		}
	}
	tool, err := os.ReadFile(filepath.Join(dir, "tools", "lookup_customer.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`description: "Look up the caller"`, "url_env: LOOKUP_URL"} {
		if !strings.Contains(string(tool), want) {
			t.Errorf("tool manifest missing %q:\n%s", want, tool)
		}
	}
	if _, err := Preflight(data); err != nil {
		t.Fatalf("Preflight() = %v", err)
	}
}

func TestWriteLocalToolManifest(t *testing.T) {
	data := Data{Name: "agent", Tools: []Tool{{
		Name: "lookup_customer", Description: "Look up the caller", Execution: "local",
		Handler: "tools/lookup_customer.py", Input: `{"type":"object"}`,
	}}}
	data.SetTarget("pipecat")
	dir := filepath.Join(t.TempDir(), "agent")
	if _, err := Write(dir, data); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "tools", "lookup_customer.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := string(raw)
	for _, want := range []string{"local:", "handler: tools/lookup_customer.py"} {
		if !strings.Contains(manifest, want) {
			t.Errorf("local tool manifest missing %q:\n%s", want, manifest)
		}
	}
	if strings.Contains(manifest, "url_env:") {
		t.Errorf("local tool manifest contains webhook URL:\n%s", manifest)
	}
}

func TestPreflightAdditionalAgentAndHandoff(t *testing.T) {
	for _, provider := range []string{"pipecat", "livekit"} {
		t.Run(provider, func(t *testing.T) {
			data := Data{Name: "agent", Channel: "web"}
			data.SetTarget(provider)
			data.Agents = []Agent{{
				Name: "billing", Instructions: "You are the billing specialist.", Reason: data.Reason, Speak: data.Speak,
			}}
			data.Handoffs = []Handoff{{
				Name: "to_billing", Source: "assistant", To: "billing", When: "The caller needs billing help.", Announce: "I’ll connect you to billing now.", History: "full", AllVariables: true,
			}}
			dir := filepath.Join(t.TempDir(), "agent")
			if _, err := Write(dir, data); err != nil {
				t.Fatal(err)
			}
			agentYAML, err := os.ReadFile(filepath.Join(dir, "agent.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"billing:", "to_billing:", `announce: "I’ll connect you to billing now."`, "history: full", "variables: all"} {
				if !strings.Contains(string(agentYAML), want) {
					t.Errorf("agent.yaml missing %q:\n%s", want, agentYAML)
				}
			}
			if _, err := os.Stat(filepath.Join(dir, "agents", "billing.md")); err != nil {
				t.Fatal(err)
			}
			if _, err := Preflight(data); err != nil {
				t.Fatalf("Preflight() = %v", err)
			}
		})
	}
}

func TestPreflightTaskAndOrderedGroup(t *testing.T) {
	for _, provider := range []string{"pipecat", "livekit"} {
		t.Run(provider, func(t *testing.T) {
			data := Data{Name: "agent", Channel: "web"}
			data.SetTarget(provider)
			data.Tasks = []Task{{
				Name: "collect", Instructions: "Return the caller tier.", Result: `{"tier":{"enum":["free","pro"]}}`,
				History: "full", Agent: "assistant", When: "Classify the caller.",
			}}
			data.TaskGroups = []TaskGroup{{
				Name: "triage", Steps: []string{"collect"}, ContextScope: "shared", Then: "return", Agent: "assistant", When: "Run triage.",
			}}
			dir := filepath.Join(t.TempDir(), "agent")
			if _, err := Write(dir, data); err != nil {
				t.Fatal(err)
			}
			agentYAML, err := os.ReadFile(filepath.Join(dir, "agent.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"tasks:", "run_collect:", "task_groups:", "- collect", "run_triage:"} {
				if !strings.Contains(string(agentYAML), want) {
					t.Errorf("agent.yaml missing %q:\n%s", want, agentYAML)
				}
			}
			if _, err := os.Stat(filepath.Join(dir, "tasks", "collect.md")); err != nil {
				t.Fatal(err)
			}
			// Single-task delegates compile on every scaffolded target since
			// driver-livekit T12 lifted the group-only refusal.
			if _, err := Preflight(data); err != nil {
				t.Fatalf("Preflight() = %v", err)
			}
		})
	}
}

func TestPreflightTelephonyAndHumanTransfer(t *testing.T) {
	for _, provider := range []string{"pipecat"} {
		t.Run(provider, func(t *testing.T) {
			data := Data{Name: "agent"}
			data.SetTarget(provider)
			data.Channels = []Channel{
				{Name: "web", Kind: "realtime_audio"},
				{Name: "phone", Kind: "telephony", Inbound: true, RequiredControls: []string{"cold_transfer", "hangup"}},
			}
			data.HumanTransfers = []HumanTransfer{{
				Name: "to_human", Agent: "assistant", When: "The caller requests a person.",
				Destination: "support_line", Value: "SUPPORT_PHONE_NUMBER", Mode: "cold",
			}}
			report, err := Preflight(data)
			if provider == "pipecat" {
				// The scaffold starts with daily-sip, but a carrier is still
				// required. Preflight fails closed and names the supported route.
				if err == nil || !strings.Contains(err.Error(), "daily-sip with twilio") {
					t.Fatalf("Preflight() = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Preflight() = %v", err)
			}
			if report.TargetName != "elevenlabs" {
				t.Fatalf("target = %q", report.TargetName)
			}
		})
	}
}

// A scaffolded Pipecat phone route starts with daily-sip, but remains incomplete
// until the author selects a supported carrier.
func TestPreflightRejectsIncompleteDailyRoute(t *testing.T) {
	data := Data{Name: "agent"}
	data.SetTarget("pipecat")
	data.Channels = []Channel{{Name: "phone", Kind: "telephony", Outbound: true, OnVoicemail: "hangup"}}
	_, err := Preflight(data)
	if err == nil {
		t.Fatal("an incomplete Daily route served a phone channel")
	}
	for _, want := range []string{"connections/phone.yaml", "declares no carrier", "daily-sip with twilio"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal is missing %q: %v", want, err)
		}
	}
}

func TestPreflightCustomizedTarget(t *testing.T) {
	enabled := true
	data := Data{
		Name: "agent", SpeaksFirst: "user", ModelGreeting: true, Interruption: &enabled,
		MinimumWords: 2, IgnorePhrases: []string{"okay"}, NudgeAfter: "20s", EndAfter: "1m", MaxDuration: "10m",
		Capacity: Capacity{PeakSessions: 5, MaxSessions: 10, AvgSessionDuration: "4m"},
	}
	data.SetTarget("livekit")
	data.Fallbacks = []ModelFallback{{
		Name: "backup_model", Profile: "assistant_model", Binding: data.Reason,
	}}
	report, err := Preflight(data)
	if err != nil {
		t.Fatalf("Preflight() = %v", err)
	}
	if report.TargetName != "livekit" {
		t.Fatalf("report = %#v", report)
	}
	dir := filepath.Join(t.TempDir(), "agent")
	if _, err := Write(dir, data); err != nil {
		t.Fatal(err)
	}
	agent, err := os.ReadFile(filepath.Join(dir, "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"fallback:", "- backup_model", "speaks_first: user", "minimum_words: 2", "peak_sessions: 5"} {
		if !strings.Contains(string(agent), want) {
			t.Errorf("agent.yaml missing %q:\n%s", want, agent)
		}
	}
}

func TestPreflightShippedTargets(t *testing.T) {
	for _, provider := range []string{"pipecat", "livekit"} {
		t.Run(provider, func(t *testing.T) {
			data := Data{Name: "agent", Channel: "web"}
			data.SetTarget(provider)
			report, err := Preflight(data)
			if err != nil {
				t.Fatal(err)
			}
			if report.TargetName != provider || len(report.Bindings) == 0 || len(report.RequiredEnv) == 0 {
				t.Fatalf("report = %#v", report)
			}
		})
	}
}

func TestPreflightRejectsInvalidCandidateBeforeWrite(t *testing.T) {
	t.Chdir(t.TempDir())
	data := Data{Name: "agent", Channel: "web"}
	data.SetTarget("livekit")
	data.Speak = Binding{Provider: "elevenlabs", Model: "eleven_multilingual_v2"}
	if _, err := Preflight(data); err == nil || !strings.Contains(err.Error(), "missing a voice") {
		t.Fatalf("Preflight() error = %v", err)
	}
	if _, err := os.Stat("agent"); !os.IsNotExist(err) {
		t.Fatalf("preflight wrote destination: %v", err)
	}
}

func TestWrite_deterministic(t *testing.T) {
	a := filepath.Join(t.TempDir(), "x")
	b := filepath.Join(t.TempDir(), "x")
	ca, err := Write(a, Data{Name: "x"})
	if err != nil {
		t.Fatal(err)
	}
	cb, err := Write(b, Data{Name: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(manifest(t, a, ca), manifest(t, b, cb)) {
		t.Error("two renders of the same name differ; output is not deterministic")
	}
}

func TestWrite_refusesNonEmpty(t *testing.T) {
	dir := t.TempDir() // exists and (after first write) non-empty
	if _, err := Write(dir, Data{Name: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(dir, Data{Name: "x"}); err == nil {
		t.Fatal("expected refusal to overwrite a non-empty dir")
	}
}

// flowStyle matches a YAML flow mapping or flow sequence opening at a value
// position or a list item. The two empty forms `client: {}` and
// `provider_hosted: {}` are deliberately excluded: block style has no empty
// form, so they are the only spelling N19 leaves for "intentionally empty"
// (SPEC.md C3).
var flowStyle = regexp.MustCompile(`(^|[-:][ \t]+)(\[[^\]]|\{[^}])`)

// TestScaffoldToolManifestsAreBlockStyle pins the house rule for every
// execution kind a tool file can carry, including the webhook `auth:` block.
// The block-style restyle (PR #67) was written before the execution-keyed tool
// shape landed, so it never saw `auth:` at all, and the two changes met for the
// first time in the template. A regression here re-emits flow style from the
// one place that regenerates packages.
func TestScaffoldToolManifestsAreBlockStyle(t *testing.T) {
	// speed is an integer on purpose: yamlBlock decodes as YAML rather than
	// encoding/json so `1` does not widen to `1.0`. Params are forwarded to the
	// provider verbatim under D10, so widening is a behaviour change (SPEC.md V6).
	const input = `{"type":"object","properties":{"q":{"type":"string","enum":["a","b"]},"speed":{"type":"integer","default":1}},"required":["q"]}`
	const output = `{"type":"object","properties":{"ok":{"type":"boolean"}}}`

	for _, tc := range []struct {
		name string
		tool Tool
	}{
		{"local", Tool{Execution: "local", Handler: "tools/t.py"}},
		{"webhook", Tool{Execution: "webhook", URLEnv: "T_URL"}},
		{"webhook_bearer", Tool{Execution: "webhook", URLEnv: "T_URL",
			Auth: &spec.ToolAuth{Type: "bearer", TokenEnv: "T_TOKEN"}}},
		{"webhook_api_key", Tool{Execution: "webhook", URLEnv: "T_URL",
			Auth: &spec.ToolAuth{Type: "api_key", TokenEnv: "T_KEY", Header: "X-Api-Key"}}},
		// mcp is not in this table: N40 gives it no input or output to style.
		// TestScaffoldMCPToolIsBlockOnly covers the shape it does write.
		{"client", Tool{Execution: "client"}},
		{"provider_hosted", Tool{Execution: "provider_hosted"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := tc.tool
			tool.Name, tool.Description = "t", "A tool"
			tool.Input, tool.Output = input, output

			data := Data{Name: "agent", Tools: []Tool{tool}}
			data.SetTarget("pipecat")
			dir := filepath.Join(t.TempDir(), "agent")
			if _, err := Write(dir, data); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(dir, "tools", "t.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			manifest := string(raw)

			for i, line := range strings.Split(manifest, "\n") {
				if flowStyle.MatchString(line) {
					t.Errorf("line %d is flow style: %q\n%s", i+1, line, manifest)
				}
			}

			// Block style is only worth anything if it still parses to the
			// schema that went in.
			var got struct {
				Input map[string]any `yaml:"input"`
			}
			if err := yaml.Unmarshal(raw, &got); err != nil {
				t.Fatalf("emitted tool file does not parse: %v\n%s", err, manifest)
			}
			// Sequences indent, the way every hand-authored package writes them.
			// goccy's default dash column is legal YAML but would make the
			// scaffold the one place that differs.
			if !strings.Contains(manifest, "      enum:\n        - a\n        - b\n") {
				t.Errorf("want an indented block sequence for enum:\n%s", manifest)
			}
			// V6 is a claim about the emitted text, so assert on the text:
			// encoding/json would widen 1 to float64 and re-emit `default: 1.0`.
			if !strings.Contains(manifest, "default: 1\n") || strings.Contains(manifest, "default: 1.0") {
				t.Errorf("want `default: 1`, not a widened float:\n%s", manifest)
			}
			props, _ := got.Input["properties"].(map[string]any)
			speed, _ := props["speed"].(map[string]any)
			switch speed["default"].(type) {
			case float32, float64:
				t.Errorf("speed default decoded as %T — yamlBlock must not widen ints", speed["default"])
			}
			enum, _ := props["q"].(map[string]any)
			if got, want := enum["enum"], []any{"a", "b"}; !reflect.DeepEqual(got, want) {
				t.Errorf("enum = %#v, want %#v", got, want)
			}
		})
	}
}

// TestScaffoldMCPToolIsBlockOnly pins N40 in the one place that regenerates
// packages: an mcp tool file is the `mcp:` block and nothing else, and every
// field the console carries through (transport, auth, selection) rides inside
// it. The file the scaffold writes must load, so this asserts on the text and
// on the decode.
func TestScaffoldMCPToolIsBlockOnly(t *testing.T) {
	data := Data{Name: "agent", Tools: []Tool{{
		Name: "web_search", Execution: "mcp", URLEnv: "FIRECRAWL_MCP_URL",
		MCPTransport: "streamable_http", MCPTools: []string{"firecrawl_search"},
		Auth: &spec.ToolAuth{Type: "bearer", TokenEnv: "FIRECRAWL_API_KEY"},
	}}}
	data.SetTarget("pipecat")
	dir := filepath.Join(t.TempDir(), "agent")
	if _, err := Write(dir, data); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "tools", "web_search.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := string(raw)
	for _, illegal := range []string{"description:", "input:", "output:", "interruption:", "effect:"} {
		if strings.Contains(manifest, illegal) {
			t.Errorf("an mcp file carries no %s\n%s", illegal, manifest)
		}
	}
	var got struct {
		MCP *spec.ToolMCP `yaml:"mcp"`
	}
	if err := yaml.Unmarshal(raw, &got); err != nil {
		t.Fatalf("emitted tool file does not parse: %v\n%s", err, manifest)
	}
	if got.MCP == nil || got.MCP.URLEnv != "FIRECRAWL_MCP_URL" || got.MCP.Transport != "streamable_http" {
		t.Fatalf("block did not round-trip:\n%s", manifest)
	}
	if got.MCP.Auth == nil || got.MCP.Auth.TokenEnv != "FIRECRAWL_API_KEY" {
		t.Errorf("auth did not round-trip:\n%s", manifest)
	}
	if len(got.MCP.Tools) != 1 || got.MCP.Tools[0] != "firecrawl_search" {
		t.Errorf("selection did not round-trip:\n%s", manifest)
	}
}

// The scaffold writes the shape block with the transfer's settings inside it,
// `destination` included (SCHEMA N27). Checked by decoding rather than by
// grepping, so an indentation slip is a failure and not a passing substring.
func TestWriteHumanTransferPutsDestinationInTheBlock(t *testing.T) {
	dir := t.TempDir()
	data := Data{Name: "agent"}
	data.SetTarget("pipecat")
	data.Channels = []Channel{{Name: "phone", Kind: "telephony", Inbound: true}}
	data.HumanTransfers = []HumanTransfer{
		{Name: "to_human", Agent: "assistant", When: "The caller asks for a person.",
			Destination: "support_line", Value: "SUPPORT_PHONE_NUMBER", Mode: "cold"},
		{Name: "to_manager", Agent: "assistant", When: "The caller asks for a manager.",
			Destination: "manager_line", Value: "MANAGER_PHONE_NUMBER", Mode: "warm",
			Briefing: "Say who is calling.", RingTimeout: "20s", OnUnavailable: "hangup"},
	}
	if _, err := Write(dir, data); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Escalations map[string]struct {
			Destination *string `yaml:"destination"`
			Cold        *struct {
				Destination string `yaml:"destination"`
			} `yaml:"cold"`
			Warm *struct {
				Destination   string `yaml:"destination"`
				Briefing      string `yaml:"briefing"`
				RingTimeout   string `yaml:"ring_timeout"`
				OnUnavailable string `yaml:"on_unavailable"`
			} `yaml:"warm"`
		} `yaml:"escalations"`
	}
	if err := yaml.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("scaffolded agent.yaml does not parse: %v\n%s", err, raw)
	}
	cold := decoded.Escalations["to_human"]
	if cold.Destination != nil {
		t.Error("destination must live inside the shape block, not above it")
	}
	if cold.Cold == nil || cold.Cold.Destination != "support_line" {
		t.Errorf("cold block = %+v", cold.Cold)
	}
	warm := decoded.Escalations["to_manager"].Warm
	if warm == nil || warm.Destination != "manager_line" || warm.Briefing != "Say who is calling." ||
		warm.RingTimeout != "20s" || warm.OnUnavailable != "hangup" {
		t.Errorf("warm block = %+v", warm)
	}
}

// A wizard-built phone route starts from a per-target transport. The carrier is
// still missing, and naming the transport lets the refusal list what works.
func TestDefaultTransportPerTarget(t *testing.T) {
	for target, want := range map[string]string{
		"pipecat":  "daily-sip",
		"livekit":  "sip",
		"vapi":     "",
		"deepgram": "",
	} {
		if got := DefaultTransport(target); got != want {
			t.Errorf("DefaultTransport(%q) = %q, want %q", target, got, want)
		}
	}
}

// withDefaults fills the transport in only for a package that actually uses a
// phone route: on a browser-only package it would put a credential line in
// .env.example that nothing reads.
func TestPhoneRouteGetsItsTargetsTransport(t *testing.T) {
	for _, target := range []string{"pipecat", "livekit"} {
		t.Run(target, func(t *testing.T) {
			browser := Data{Name: "agent", Channel: "web", Tools: DefaultTools()}
			browser.SetTarget(target)
			if got := browser.withDefaults().Transport; got != "" {
				t.Errorf("a browser-only package must declare no transport, got %q", got)
			}

			// A telephony channel needs an explicit Channels entry: the bare
			// Channel field defaults to realtime_audio, which is not a phone
			// route (AllChannels).
			phone := Data{
				Name:     "agent",
				Channels: []Channel{{Name: "phone", Kind: "telephony", Inbound: true}},
				Tools:    DefaultTools(),
			}
			phone.SetTarget(target)
			if got, want := phone.withDefaults().Transport, DefaultTransport(target); got != want {
				t.Errorf("a phone package on %s got transport %q, want %q", target, got, want)
			}
		})
	}
}

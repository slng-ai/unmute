package scaffold

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
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

func TestWrite_golden(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "support-bot")
	created, err := Write(dir, Data{Name: "support-bot"})
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
		target string
		want   []string
		env    []string
	}{
		{
			target: "livekit",
			want:   []string{"provider: livekit", "sdk_language: python", "provider: slng", `voice: "aura-2-thalia-en"`},
			env:    []string{"LIVEKIT_API_KEY=", "LIVEKIT_API_SECRET=", "LIVEKIT_URL=", "SLNG_API_KEY="},
		},
		{
			target: "elevenlabs",
			want:   []string{"provider: elevenlabs", `voice_id: "cgSgspJ2msm6clMCkdW9"`, `model: "gemini-2.5-flash"`},
			env:    []string{"ELEVENLABS_API_KEY="},
		},
	} {
		t.Run(tc.target, func(t *testing.T) {
			data := Data{Name: "agent", Language: "es-MX", Channel: "web"}
			data.SetTarget(tc.target)
			data.Speak.Params = `{"speed":1}`
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
			for _, want := range append(tc.want, `params: {"speed":1}`) {
				if !strings.Contains(string(targets), want) {
					t.Errorf("targets.yaml missing %q:\n%s", want, targets)
				}
			}
			agent, err := os.ReadFile(filepath.Join(dir, "agent.yaml"))
			if err != nil || !strings.Contains(string(agent), "language: es-MX") {
				t.Fatalf("agent language: err=%v\n%s", err, agent)
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

func TestWriteVariablesAndTools(t *testing.T) {
	data := Data{
		Name:     "agent",
		Language: "en",
		Channel:  "web",
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

func TestPreflightAdditionalAgentAndHandoff(t *testing.T) {
	for _, provider := range []string{"pipecat", "livekit", "elevenlabs"} {
		t.Run(provider, func(t *testing.T) {
			data := Data{Name: "agent", Language: "en", Channel: "web"}
			data.SetTarget(provider)
			data.Agents = []Agent{{
				Name: "billing", Instructions: "You are the billing specialist.", Reason: data.Reason, Speak: data.Speak,
			}}
			data.Handoffs = []Handoff{{
				Name: "to_billing", Source: "assistant", To: "billing", When: "The caller needs billing help.", History: "full", AllVariables: true,
			}}
			dir := filepath.Join(t.TempDir(), "agent")
			if _, err := Write(dir, data); err != nil {
				t.Fatal(err)
			}
			agentYAML, err := os.ReadFile(filepath.Join(dir, "agent.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"billing:", "to_billing:", "history: full", "variables: all"} {
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

func TestPreflightShippedTargets(t *testing.T) {
	for _, provider := range []string{"pipecat", "livekit", "elevenlabs"} {
		t.Run(provider, func(t *testing.T) {
			data := Data{Name: "agent", Language: "en", Channel: "web"}
			data.SetTarget(provider)
			report, err := Preflight(data)
			if err != nil {
				t.Fatal(err)
			}
			if report.TargetName != provider+"-dev" || len(report.Bindings) == 0 || len(report.RequiredEnv) == 0 {
				t.Fatalf("report = %#v", report)
			}
		})
	}
}

func TestPreflightRejectsInvalidCandidateBeforeWrite(t *testing.T) {
	t.Chdir(t.TempDir())
	data := Data{Name: "agent", Language: "en", Channel: "web"}
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

package scaffold

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/slng/unmute/internal/ir"
)

var update = flag.Bool("update", false, "rewrite golden files")

var (
	templatePlaceholderPattern = regexp.MustCompile(`\{\{[^{}]*\}\}`)
	slngSimplePlaceholder      = regexp.MustCompile(`^\{\{[A-Za-z_][A-Za-z0-9_]*\}\}$`)
)

// manifest renders the created tree to a single deterministic blob: every file
// sorted, prefixed by its relative path. Stable order = no map iteration (V4).
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

func TestWrite_golden(t *testing.T) { // V4, V6
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

func TestWrite_customData(t *testing.T) { // V10
	dir := filepath.Join(t.TempDir(), "support-bot")
	data := Data{
		Name:     "support: bot",
		Greeting: `Hello \ caller?`,
		Language: "en-US",
		LLMModel: "openai/custom:beta",
		STTModel: "stt/custom:beta",
		TTSModel: "tts/custom:beta",
		TTSVoice: "voice #1",
	}
	if _, err := Write(dir, data); err != nil {
		t.Fatal(err)
	}

	checks := []struct {
		rel  string
		key  string
		want string
	}{
		{"project.yaml", "name", data.Name},
		{"agent/agent.yaml", "greeting", data.Greeting},
		{"agent/agent.yaml", "language", data.Language},
		{"agent/models/llm.yaml", "model", data.LLMModel},
		{"agent/models/stt.yaml", "model", data.STTModel},
		{"agent/models/tts.yaml", "model", data.TTSModel},
		{"agent/models/tts.yaml", "voice", data.TTSVoice},
	}
	for _, check := range checks {
		content, err := os.ReadFile(filepath.Join(dir, check.rel))
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := yaml.Unmarshal(content, &got); err != nil {
			t.Fatalf("%s: %v", check.rel, err)
		}
		if got[check.key] != check.want {
			t.Errorf("%s %s = %v, want %q", check.rel, check.key, got[check.key], check.want)
		}
	}
}

func TestWrite_deterministic(t *testing.T) { // V4
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

func TestWrite_fragmentSetMatchesIR(t *testing.T) { // V2: templates ⇔ ir.PromptFragments
	dir := filepath.Join(t.TempDir(), "x")
	created, err := Write(dir, Data{Name: "x"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, p := range created {
		d, f := filepath.Split(p)
		if filepath.Base(filepath.Clean(d)) == "prompt" {
			got[f] = true
		}
	}
	if len(got) != len(ir.PromptFragments) {
		t.Errorf("prompt fragment count = %d, want %d (ir.PromptFragments)", len(got), len(ir.PromptFragments))
	}
	for _, f := range ir.PromptFragments {
		if !got[f+".md"] {
			t.Errorf("missing prompt fragment %s.md", f)
		}
	}
}

func TestWrite_promptPlaceholdersAreSLNGSimple(t *testing.T) { // V61
	dir := filepath.Join(t.TempDir(), "support-bot")
	if _, err := Write(dir, Data{Name: "support-bot"}); err != nil {
		t.Fatal(err)
	}

	for _, fragment := range ir.PromptFragments {
		rel := filepath.Join("agent", "prompt", fragment+".md")
		content, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			t.Fatal(err)
		}
		text := string(content)
		matches := templatePlaceholderPattern.FindAllString(text, -1)
		if bytes.Count(content, []byte("{{")) != len(matches) || bytes.Count(content, []byte("}}")) != len(matches) {
			t.Errorf("%s contains malformed template braces:\n%s", rel, content)
		}
		for _, match := range matches {
			if !slngSimplePlaceholder.MatchString(match) {
				t.Errorf("%s contains backend-invalid placeholder %q", rel, match)
			}
		}
	}
}

func TestWrite_modelFiles(t *testing.T) { // V13, V16, V22
	dir := filepath.Join(t.TempDir(), "support-bot")
	if _, err := Write(dir, Data{Name: "support-bot"}); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{
		"agent/models/stt.yaml",
		"agent/models/llm.yaml",
		"agent/models/tts.yaml",
	} {
		content, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			t.Errorf("missing %s: %v", rel, err)
			continue
		}
		for _, want := range [][]byte{[]byte("config:"), []byte("fallbacks:")} {
			if !bytes.Contains(content, want) {
				t.Errorf("%s missing %q; got:\n%s", rel, want, content)
			}
		}
		if !bytes.Contains(content, []byte("config: {}")) {
			t.Errorf("%s should keep optional model config empty by default (V22); got:\n%s", rel, content)
		}
		if rel == "agent/models/llm.yaml" && bytes.Contains(content, []byte("kwargs:")) {
			t.Errorf("%s should use config, not kwargs; got:\n%s", rel, content)
		}
	}

	tts, err := os.ReadFile(filepath.Join(dir, "agent/models/tts.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(tts, []byte("config.speed")) || bytes.Contains(tts, []byte("speed:")) {
		t.Errorf("Rime scaffold must not emit config.speed; got:\n%s", tts)
	}
}

func TestWrite_runtimeFiles(t *testing.T) { // V23, V24, V25, V26, V27
	dir := filepath.Join(t.TempDir(), "support-bot")
	if _, err := Write(dir, Data{Name: "support-bot"}); err != nil {
		t.Fatal(err)
	}

	agent, err := os.ReadFile(filepath.Join(dir, "agent/agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(agent, []byte("language: en")) {
		t.Errorf("agent.yaml missing language default; got:\n%s", agent)
	}

	compliance, err := os.ReadFile(filepath.Join(dir, "agent/compliance.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range [][]byte{[]byte("region: ap-south")} {
		if !bytes.Contains(compliance, want) {
			t.Errorf("compliance.yaml missing %q; got:\n%s", want, compliance)
		}
	}
	if bytes.Count(compliance, []byte(":")) != 1 {
		t.Errorf("compliance.yaml should only declare region; got:\n%s", compliance)
	}
	if info, err := os.Stat(filepath.Join(dir, "agent/compliance")); err == nil && info.IsDir() {
		t.Error("compliance must be a singleton agent/compliance.yaml, not an agent/compliance/ dir")
	}

	idle, err := os.ReadFile(filepath.Join(dir, "agent/overrides/idle.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range [][]byte{
		[]byte("enabled: true"),
		[]byte("first_nudge_delay_seconds: 15"),
		[]byte("second_nudge_delay_seconds: 30"),
		[]byte("hangup_delay_seconds: 15"),
		[]byte(`first_nudge_text: "Are you still there?"`),
		[]byte(`second_nudge_text: "I'm still here. If you need a moment, just let me know."`),
		[]byte(`final_hangup_text: "I'll end the call now. Please feel free to call back when you're ready."`),
	} {
		if !bytes.Contains(idle, want) {
			t.Errorf("idle.yaml missing %q; got:\n%s", want, idle)
		}
	}

	interruption, err := os.ReadFile(filepath.Join(dir, "agent/overrides/interruption.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(interruption, []byte("enabled: true")) {
		t.Errorf("interruption.yaml missing enabled default; got:\n%s", interruption)
	}
}

func TestWrite_sipFiles(t *testing.T) { // V29, V30, V31, V32
	dir := filepath.Join(t.TempDir(), "support-bot")
	if _, err := Write(dir, Data{Name: "support-bot"}); err != nil {
		t.Fatal(err)
	}

	sip, err := os.ReadFile(filepath.Join(dir, "agent/channels/sip.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range [][]byte{
		[]byte("provider: twilio"),
		[]byte("connection: twilio"),
	} {
		if !bytes.Contains(sip, want) {
			t.Errorf("sip.yaml missing %q; got:\n%s", want, sip)
		}
	}
	for _, forbidden := range [][]byte{
		[]byte("direction:"),
		[]byte("dial_in:"),
		[]byte("dial_out:"),
		[]byte("auth:"),
	} {
		if bytes.Contains(sip, forbidden) {
			t.Errorf("sip.yaml must stay trunk-only and reference connection config; found %q in:\n%s", forbidden, sip)
		}
	}

	twilio, err := os.ReadFile(filepath.Join(dir, "agent/connections/twilio.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range [][]byte{
		[]byte("type: twilio"),
		[]byte("auth:"),
		[]byte("account_sid_secret: TWILIO_ACCOUNT_SID"),
		[]byte("auth_token_secret: TWILIO_AUTH_TOKEN"),
		[]byte("sip:"),
		[]byte("enabled: true"),
	} {
		if !bytes.Contains(twilio, want) {
			t.Errorf("twilio.yaml missing %q; got:\n%s", want, twilio)
		}
	}
	for _, line := range bytes.Split(twilio, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if bytes.HasPrefix(trimmed, []byte("account_sid:")) ||
			bytes.HasPrefix(trimmed, []byte("auth_token:")) ||
			bytes.HasPrefix(trimmed, []byte("token:")) {
			t.Errorf("twilio.yaml must contain secret refs only, got line: %s", line)
		}
	}
	if bytes.Contains(twilio, []byte(".env")) {
		t.Errorf("twilio.yaml must not direct secret values to .env; got:\n%s", twilio)
	}
}

func TestWrite_envAndTargetFiles(t *testing.T) { // V35, V36, V37, V38
	dir := filepath.Join(t.TempDir(), "support-bot")
	if _, err := Write(dir, Data{Name: "support-bot"}); err != nil {
		t.Fatal(err)
	}

	gitignore, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range [][]byte{
		[]byte(".env.local"),
		[]byte(".venv"),
		[]byte("__pycache__"),
	} {
		if !bytes.Contains(gitignore, want) {
			t.Errorf(".gitignore missing %q; got:\n%s", want, gitignore)
		}
	}

	envLocal, err := os.ReadFile(filepath.Join(dir, ".env.local"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range bytes.Split(bytes.TrimSpace(envLocal), []byte("\n")) {
		if !bytes.HasSuffix(line, []byte("=")) {
			t.Errorf(".env.local placeholders must stay blank, got line: %s", line)
		}
	}

	secrets, err := os.ReadFile(filepath.Join(dir, "env/secrets.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range [][]byte{
		[]byte("env_file: .env.local"),
		[]byte("SLNG_API_KEY:"),
		[]byte("local_key: SLNG_API_KEY"),
		[]byte("OPENAI_API_KEY:"),
		[]byte("local_key: OPENAI_API_KEY"),
	} {
		if !bytes.Contains(secrets, want) {
			t.Errorf("secrets.yaml missing %q; got:\n%s", want, secrets)
		}
	}

	profile, err := os.ReadFile(filepath.Join(dir, "targets/pipecat/pipecat.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range [][]byte{
		[]byte("docker:"),
		[]byte("image: support-bot"),
		[]byte("pcc:"),
		[]byte("agent_name: support-bot"),
		[]byte("secret_set: support-bot-local"),
		[]byte("min_agents: 1"),
		[]byte("kubernetes:"),
		[]byte("secret_name: support-bot-secrets"),
		[]byte("local:"),
		[]byte("env_file: .env.local"),
	} {
		if !bytes.Contains(profile, want) {
			t.Errorf("pipecat.yaml missing %q; got:\n%s", want, profile)
		}
	}
}

package cli

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/huh"
	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/scaffold"
	"github.com/slng/unmute/internal/spec"
	"github.com/spf13/cobra"
)

func TestSupportedCompileTargets(t *testing.T) { // V11
	want := []string{"pipecat", "slng"}
	if !reflect.DeepEqual(SupportedCompileTargets, want) {
		t.Fatalf("SupportedCompileTargets = %v, want %v", SupportedCompileTargets, want)
	}
}

func TestRunInitWizardRechecksExistingDirectory(t *testing.T) { // V2, V6
	t.Chdir(t.TempDir())
	reader := &hookReader{
		Reader: strings.NewReader("1\nagent\n8\n\n"),
		after:  4,
		hook: func() {
			if err := os.Mkdir("agent", 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join("agent", "keep"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
	}
	cmd := &cobra.Command{}
	cmd.SetIn(reader)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := runInitWizard(cmd)
	if !errors.Is(err, scaffold.ErrExists) {
		t.Fatalf("runInitWizard() = %v, want ErrExists", err)
	}
	entries, readErr := os.ReadDir("agent")
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 1 || entries[0].Name() != "keep" {
		t.Fatalf("TOCTOU guard changed directory: %v", entries)
	}
}

type hookReader struct {
	io.Reader
	after int
	seen  int
	hook  func()
}

func (r *hookReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	for _, b := range p[:n] {
		if b == '\n' {
			r.seen++
			if r.seen == r.after {
				r.hook()
			}
		}
	}
	return n, err
}

// run executes a fresh command tree (rule 1) and returns captured output + err.
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func runWithInput(t *testing.T, input string, args ...string) (string, error) {
	t.Helper()
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetIn(strings.NewReader(input))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestInitWithoutTTYRequiresName(t *testing.T) { // V7
	_, err := run(t, "init")
	if err == nil || err.Error() != "agent name required" {
		t.Fatalf("init error = %v, want agent name required", err)
	}
}

func TestInitWithInjectedReaderRunsWizard(t *testing.T) { // I.init, V5
	t.Chdir(t.TempDir())
	if _, err := runWithInput(t, "1\nagent\n8\n\n", "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join("agent", "project.yaml")); err != nil {
		t.Fatalf("wizard did not scaffold: %v", err)
	}
}

func TestWriteHeader(t *testing.T) { // C6
	t.Setenv("NO_COLOR", "")
	var out bytes.Buffer
	writeHeader(&out, true)
	if !strings.Contains(out.String(), "\x1b[38;2;0;0;0;48;2;245;201;110m") ||
		!strings.Contains(out.String(), "____") || !strings.Contains(out.String(), "//  //") {
		t.Fatalf("wordmark missing black/yellow ANSI or SLNG // art: %q", out.String())
	}

	out.Reset()
	writeHeader(&out, false)
	if out.Len() != 0 {
		t.Fatalf("non-TTY header = %q, want empty", out.String())
	}

	t.Setenv("NO_COLOR", "1")
	writeHeader(&out, true)
	if out.Len() != 0 {
		t.Fatalf("NO_COLOR header = %q, want empty", out.String())
	}
}

func TestDiscoverLocalAgents(t *testing.T) { // V13
	root := t.TempDir()
	for path, name := range map[string]string{"b-agent": "Beta", "a-agent": "Alpha"} {
		if _, err := scaffold.Write(filepath.Join(root, path), scaffold.Data{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "broken"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "broken", "project.yaml"), []byte("name: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := scaffold.Write(filepath.Join(root, "parent", "nested"), scaffold.Data{Name: "Nested"}); err != nil {
		t.Fatal(err)
	}

	var warnings bytes.Buffer
	agents, err := discoverLocalAgents(root, &warnings)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := []string{agents[0].Data.Name, agents[1].Data.Name}, []string{"Alpha", "Beta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("agent names = %v, want %v", got, want)
	}
	if !strings.Contains(warnings.String(), "broken") {
		t.Fatalf("missing malformed-agent warning: %q", warnings.String())
	}
	for _, agent := range agents {
		if agent.Data.Name == "Nested" {
			t.Fatal("recursive discovery included nested grandchild")
		}
	}
}

func TestDiscoverLocalAgentAtRoot(t *testing.T) { // V13
	root := t.TempDir()
	if _, err := scaffold.Write(root, scaffold.Data{Name: "Root agent"}); err != nil {
		t.Fatal(err)
	}
	agents, err := discoverLocalAgents(root, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0].Data.Name != "Root agent" || agents[0].Path != root {
		t.Fatalf("root agents = %#v", agents)
	}
}

func TestSaveLocalAgentPreservesOtherContent(t *testing.T) { // V16
	root := filepath.Join(t.TempDir(), "agent")
	if _, err := scaffold.Write(root, scaffold.Data{Name: "Agent"}); err != nil {
		t.Fatal(err)
	}
	before, err := loadLocalAgent(root)
	if err != nil {
		t.Fatal(err)
	}
	projectBefore, err := os.ReadFile(filepath.Join(root, "project.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	llmBefore, err := os.ReadFile(filepath.Join(root, "agent", "models", "llm.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	after := before
	after.Data.Greeting = "Hello there?"
	after.Data.Language = "es"
	after.Data.LLMModel = "openai/gpt-4o"
	after.Data.STTModel = "custom/stt"
	after.Data.TTSModel = "custom/tts"
	after.Data.TTSVoice = "voice-two"
	after.Prompt = "You are the edited agent."
	if err := saveLocalAgent(before, after); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadLocalAgent(root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Data, after.Data) || loaded.Prompt != after.Prompt {
		t.Fatalf("loaded edit = %#v, want %#v", loaded, after)
	}
	projectAfter, err := os.ReadFile(filepath.Join(root, "project.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(projectBefore, projectAfter) {
		t.Fatal("save changed untouched project.yaml")
	}
	llmAfter, err := os.ReadFile(filepath.Join(root, "agent", "models", "llm.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	wantLLM := bytes.Replace(llmBefore, []byte("model: "+scaffold.DefaultLLMModel), []byte("model: openai/gpt-4o"), 1)
	if !bytes.Equal(llmAfter, wantLLM) {
		t.Fatalf("llm.yaml changed outside model line:\n%s", llmAfter)
	}
}

func TestSaveLocalAgentPreparesBeforeWriting(t *testing.T) { // C12, V16
	root := filepath.Join(t.TempDir(), "agent")
	if _, err := scaffold.Write(root, scaffold.Data{Name: "Agent"}); err != nil {
		t.Fatal(err)
	}
	before, err := loadLocalAgent(root)
	if err != nil {
		t.Fatal(err)
	}
	agentPath := filepath.Join(root, "agent", "agent.yaml")
	agentBefore, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatal(err)
	}
	ttsPath := filepath.Join(root, "agent", "models", "tts.yaml")
	tts, err := os.ReadFile(ttsPath)
	if err != nil {
		t.Fatal(err)
	}
	tts = bytes.Replace(tts, []byte("voice: "+scaffold.DefaultTTSVoice+"\n"), nil, 1)
	if err := os.WriteFile(ttsPath, tts, 0o644); err != nil {
		t.Fatal(err)
	}
	after := before
	after.Data.Greeting = "Changed"
	after.Data.TTSVoice = "new-voice"
	if err := saveLocalAgent(before, after); err == nil {
		t.Fatal("expected missing voice key error")
	}
	agentAfter, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(agentBefore, agentAfter) {
		t.Fatal("failed save partially wrote agent.yaml")
	}
}

func TestInitWizardDefaultsMatchDirectInit(t *testing.T) { // V1, C5, C9
	t.Chdir(t.TempDir())
	for _, parent := range []string{"direct", "wizard"} {
		if err := os.Mkdir(parent, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := run(t, "init", filepath.Join("direct", "agent")); err != nil {
		t.Fatal(err)
	}
	input := "1\nwizard/agent\n8\n\n"
	if _, err := runWithInput(t, input, "init"); err != nil {
		t.Fatal(err)
	}
	if direct, wizard := treeManifest(t, filepath.Join("direct", "agent")), treeManifest(t, filepath.Join("wizard", "agent")); direct != wizard {
		t.Fatal("wizard defaults differ from init <name>")
	}
}

func TestInitWizardEOFAbortsWithoutWriting(t *testing.T) { // V2, C3, C8
	t.Chdir(t.TempDir())
	_, err := runWithInput(t, "1\nagent\n", "init")
	if !errors.Is(err, huh.ErrUserAborted) {
		t.Fatalf("init error = %v, want ErrUserAborted", err)
	}
	if _, statErr := os.Stat("agent"); !os.IsNotExist(statErr) {
		t.Fatalf("aborted wizard wrote agent: %v", statErr)
	}
}

func TestInitWizardDeclineWritesNothing(t *testing.T) { // V2, C8
	t.Chdir(t.TempDir())
	output, err := runWithInput(t, "1\nagent\n8\nn\n9\n2\n", "init")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(output, "agent") < 2 {
		t.Fatalf("decline did not return to agent menu: %q", output)
	}
	if _, statErr := os.Stat("agent"); !os.IsNotExist(statErr) {
		t.Fatalf("declined wizard wrote agent: %v", statErr)
	}
}

func TestInitWizardReportsExistingDirectoryAtNameStep(t *testing.T) { // V6
	t.Chdir(t.TempDir())
	if err := os.Mkdir("taken", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("taken", "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	output, err := runWithInput(t, "1\ntaken\n", "init")
	if !errors.Is(err, huh.ErrUserAborted) {
		t.Fatalf("init error = %v, want ErrUserAborted", err)
	}
	if !strings.Contains(output, scaffold.ErrExists.Error()) {
		t.Fatalf("name-step output = %q", output)
	}
}

func TestInitWizardCompilesDefaultTargets(t *testing.T) { // V3, V11, C9
	t.Chdir(t.TempDir())
	input := "1\nagent\n7\n2\n0\n8\n\n"
	if _, err := runWithInput(t, input, "init"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"agent/targets/pipecat/generated/bot.py",
		"agent/targets/slng/generated/agent.json",
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing %s: %v", path, err)
		}
	}
}

func TestInitWizardRequiresCompileTarget(t *testing.T) { // C9
	t.Chdir(t.TempDir())
	input := "1\nagent\n7\n2\n1\n2\n0\n1\n0\n8\nn\n9\n2\n"
	output, err := runWithInput(t, input, "init")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "select at least one target") {
		t.Fatalf("target validation output = %q", output)
	}
	if _, statErr := os.Stat("agent"); !os.IsNotExist(statErr) {
		t.Fatalf("declined wizard wrote agent: %v", statErr)
	}
}

func TestInitWizardCompileFailureKeepsScaffold(t *testing.T) { // V9
	t.Chdir(t.TempDir())
	input := "1\nagent\n2\n5\nanthropic/claude\n7\n2\n2\n0\n8\n\n"
	_, err := runWithInput(t, input, "init")
	if err == nil {
		t.Fatal("expected pipecat capability error")
	}
	if !strings.Contains(err.Error(), "agent scaffold kept") || !strings.Contains(err.Error(), "unmute compile agent pipecat") {
		t.Fatalf("compile error lacks recovery instructions: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join("agent", "project.yaml")); statErr != nil {
		t.Fatalf("compile failure removed scaffold: %v", statErr)
	}
}

func TestInitWizardRejectsHostileInput(t *testing.T) { // V8
	t.Chdir(t.TempDir())
	input := "1\nagent\n5\nbad\"greeting\nHello\n9\n2\n"
	output, err := runWithInput(t, input, "init")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "value must not contain quotes or newlines") {
		t.Fatalf("validation output = %q", output)
	}
	if _, statErr := os.Stat("agent"); !os.IsNotExist(statErr) {
		t.Fatalf("declined wizard wrote agent: %v", statErr)
	}
}

func TestInitWizardLLMOptionsMatchCatalog(t *testing.T) { // V4
	t.Chdir(t.TempDir())
	output, err := runWithInput(t, "1\nagent\n2\n", "init")
	if !errors.Is(err, huh.ErrUserAborted) {
		t.Fatalf("init error = %v, want ErrUserAborted", err)
	}
	for _, option := range append(ir.LLMCatalogModels(), "other (type route)") {
		if !strings.Contains(output, option) {
			t.Errorf("LLM options missing %q:\n%s", option, output)
		}
	}
}

func TestInitMenuEditsExistingAgent(t *testing.T) { // V13, V14, V16
	t.Chdir(t.TempDir())
	if _, err := scaffold.Write("local", scaffold.Data{Name: "Local agent"}); err != nil {
		t.Fatal(err)
	}
	output, err := runWithInput(t, "2\n5\nChanged greeting\n7\n\n", "init")
	if err != nil {
		t.Fatal(err)
	}
	config, err := spec.LoadAgentConfig("local")
	if err != nil {
		t.Fatal(err)
	}
	if config.Greeting != "Changed greeting" {
		t.Fatalf("greeting = %q", config.Greeting)
	}
	if !strings.Contains(output, "Local agent") || !strings.Contains(output, "updated") {
		t.Fatalf("edit output = %q", output)
	}
}

func TestInitMenuBackLeavesExistingAgentUntouched(t *testing.T) { // V14, V16
	t.Chdir(t.TempDir())
	if _, err := scaffold.Write("local", scaffold.Data{Name: "Local agent"}); err != nil {
		t.Fatal(err)
	}
	before := treeManifest(t, "local")
	if _, err := runWithInput(t, "2\n5\n:back\n8\n3\n", "init"); err != nil {
		t.Fatal(err)
	}
	if after := treeManifest(t, "local"); after != before {
		t.Fatal("Back changed existing agent")
	}
}

func treeManifest(t *testing.T, root string) string {
	t.Helper()
	var manifest bytes.Buffer
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		manifest.WriteString(rel)
		manifest.WriteByte(0)
		manifest.Write(content)
		manifest.WriteByte(0)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return manifest.String()
}

func TestInit_createsTree(t *testing.T) { // V1, V2, V5, V8
	dir := filepath.Join(t.TempDir(), "support-bot")
	if _, err := run(t, "init", dir); err != nil {
		t.Fatalf("init: %v", err)
	}

	want := []string{
		".env.local",
		".gitignore",
		"project.yaml",
		"env/secrets.yaml",
		"targets/pipecat/pipecat.yaml",
		"agent/agent.yaml",
		"agent/compliance.yaml",
		"agent/connections/twilio.yaml",
		"agent/channels/sip.yaml",
		"agent/channels/webrtc.yaml",
		"agent/models/stt.yaml",
		"agent/models/llm.yaml",
		"agent/models/tts.yaml",
		"agent/overrides/idle.yaml",
		"agent/overrides/interruption.yaml",
		"agent/tools/lookup_order.yaml",
		"agent/variables.yaml",
	}
	for _, f := range ir.PromptFragments { // V2: exactly the canonical set
		want = append(want, "agent/prompt/"+f+".md")
	}
	for _, rel := range want {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}

	// V10/V11: the scaffolded tool is the http portable default, follows Eve
	// handler-as-reference conventions (filename = name, no `name:` field), and
	// no inline .py is scaffolded.
	tool, err := os.ReadFile(filepath.Join(dir, "agent/tools/lookup_order.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(tool, []byte("type: http")) {
		t.Errorf("tool handler should default to http; got:\n%s", tool)
	}
	for _, line := range bytes.Split(tool, []byte("\n")) {
		if bytes.HasPrefix(bytes.TrimSpace(line), []byte("name:")) {
			t.Errorf("tool must not carry a name field (filename = tool name); got line: %s", line)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "agent/tools/lookup_order.py")); !os.IsNotExist(err) {
		t.Error("no inline .py handler should be scaffolded (portable http default, V10)")
	}

	// V8: ADR-0004 — no instructions.md, prompt lives only in prompt/.
	if _, err := os.Stat(filepath.Join(dir, "agent/instructions.md")); !os.IsNotExist(err) {
		t.Error("instructions.md must not be written (ADR-0004)")
	}

	// V5: agent name inlined literally in identity.md, not as a {{ }} var.
	id, err := os.ReadFile(filepath.Join(dir, "agent/prompt/identity.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(id, []byte("support-bot")) {
		t.Errorf("identity.md missing inlined name; got:\n%s", id)
	}
	if bytes.Contains(id, []byte("[[")) {
		t.Errorf("identity.md has an unrendered delimiter:\n%s", id)
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
	for _, want := range [][]byte{
		[]byte("region: ap-south"),
	} {
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

	variables, err := os.ReadFile(filepath.Join(dir, "agent/variables.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range [][]byte{
		[]byte("user:"),
		[]byte("system:"),
		[]byte("user_name:"),
		[]byte("source: call_id"),
		[]byte("source: current_datetime"),
	} {
		if !bytes.Contains(variables, want) {
			t.Errorf("variables.yaml missing %q; got:\n%s", want, variables)
		}
	}
	if bytes.Contains(variables, []byte(".env")) || bytes.Contains(variables, []byte("env/config.yaml")) {
		t.Errorf("variables.yaml must not direct variable values to env files (V20); got:\n%s", variables)
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
	for _, want := range [][]byte{
		[]byte("SLNG_API_KEY="),
		[]byte("OPENAI_API_KEY="),
		[]byte("GRADIUM_API_KEY="),
		[]byte("TWILIO_ACCOUNT_SID="),
		[]byte("TWILIO_AUTH_TOKEN="),
	} {
		if !bytes.Contains(envLocal, want) {
			t.Errorf(".env.local missing %q; got:\n%s", want, envLocal)
		}
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
		[]byte("GRADIUM_API_KEY:"),
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
		[]byte("image: support-bot"),
		[]byte("tag: latest"),
		[]byte("agent_name: support-bot"),
		[]byte("secret_set: support-bot-local"),
		[]byte("agent_profile: voice-agent"),
		[]byte("min_agents: 1"),
		[]byte("namespace: default"),
		[]byte("secret_name: support-bot-secrets"),
		[]byte("replicas: 1"),
		[]byte("env_file: .env.local"),
	} {
		if !bytes.Contains(profile, want) {
			t.Errorf("pipecat.yaml missing %q; got:\n%s", want, profile)
		}
	}

	stt, err := os.ReadFile(filepath.Join(dir, "agent/models/stt.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(stt, []byte("model: slng/deepgram/nova:3-en")) {
		t.Errorf("stt.yaml missing default model route; got:\n%s", stt)
	}
	assertModelCommonShape(t, "stt.yaml", stt)
	assertModelConfigEmpty(t, "stt.yaml", stt)

	llm, err := os.ReadFile(filepath.Join(dir, "agent/models/llm.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(llm, []byte("model: openai/gpt-4.1")) {
		t.Errorf("llm.yaml missing default model route; got:\n%s", llm)
	}
	assertModelCommonShape(t, "llm.yaml", llm)
	assertModelConfigEmpty(t, "llm.yaml", llm)
	if bytes.Contains(llm, []byte("kwargs:")) {
		t.Errorf("llm.yaml should use config, not kwargs; got:\n%s", llm)
	}

	tts, err := os.ReadFile(filepath.Join(dir, "agent/models/tts.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range [][]byte{
		[]byte("model: cartesia/sonic:3"),
		[]byte("voice: db6b0ed5-d5d3-463d-ae85-518a07d3c2b4"),
	} {
		if !bytes.Contains(tts, want) {
			t.Errorf("tts.yaml missing %q; got:\n%s", want, tts)
		}
	}
	assertModelCommonShape(t, "tts.yaml", tts)
	assertModelConfigEmpty(t, "tts.yaml", tts)
	if bytes.Contains(tts, []byte("speed:")) {
		t.Errorf("Cartesia Sonic scaffold must not emit speed; got:\n%s", tts)
	}
}

func assertModelCommonShape(t *testing.T, name string, content []byte) {
	t.Helper()
	for _, want := range [][]byte{
		[]byte("config:"),
		[]byte("fallbacks:"),
	} {
		if !bytes.Contains(content, want) {
			t.Errorf("%s missing %q; got:\n%s", name, want, content)
		}
	}
}

func assertModelConfigEmpty(t *testing.T, name string, content []byte) {
	t.Helper()
	if !bytes.Contains(content, []byte("config: {}")) {
		t.Errorf("%s should keep optional model config empty by default (V22); got:\n%s", name, content)
	}
}

func TestInit_refusesExisting(t *testing.T) { // V7
	dir := filepath.Join(t.TempDir(), "taken")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(keep, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := run(t, "init", dir)
	if !errors.Is(err, scaffold.ErrExists) {
		t.Fatalf("want ErrExists, got %v", err)
	}
	// No overwrite, no partial write: dir still holds only the original file.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("refuse should leave dir untouched, got %d entries", len(entries))
	}
}

func TestCompilePipecat_generatesTargetDir(t *testing.T) { // V39, V40, V45
	dir := filepath.Join(t.TempDir(), "support-bot")
	if _, err := run(t, "init", dir); err != nil {
		t.Fatalf("init: %v", err)
	}

	output, err := run(t, "compile", dir, "pipecat")
	if err != nil {
		t.Fatalf("compile: %v\n%s", err, output)
	}
	if !bytes.Contains([]byte(output), []byte("warning: tool \"lookup_order\" uses http handler and is omitted")) {
		t.Fatalf("compile missing HTTP tool warning:\n%s", output)
	}

	for _, rel := range []string{
		".unmute-generated",
		"bot.py",
		"pyproject.toml",
		"Dockerfile",
		"pcc-deploy.toml",
		"k8s/deployment.yaml",
		"k8s/secret.yaml",
		"compile-report.json",
		"README.md",
	} {
		if _, err := os.Stat(filepath.Join(dir, "targets", "pipecat", "generated", rel)); err != nil {
			t.Errorf("missing generated %s: %v", rel, err)
		}
	}
	for _, forbidden := range []string{
		"bot.py",
		"agent/bot.py",
	} {
		if _, err := os.Stat(filepath.Join(dir, forbidden)); !os.IsNotExist(err) {
			t.Errorf("generated file escaped target dir: %s", forbidden)
		}
	}
}

func TestCompilePipecat_refusesUserOwnedGeneratedDir(t *testing.T) { // V41
	dir := filepath.Join(t.TempDir(), "support-bot")
	if _, err := run(t, "init", dir); err != nil {
		t.Fatalf("init: %v", err)
	}
	generated := filepath.Join(dir, "targets", "pipecat", "generated")
	if err := os.MkdirAll(generated, 0o755); err != nil {
		t.Fatal(err)
	}
	userFile := filepath.Join(generated, "notes.txt")
	if err := os.WriteFile(userFile, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := run(t, "compile", dir, "pipecat")
	if err == nil {
		t.Fatal("expected sentinel error")
	}
	if !strings.Contains(err.Error(), ".unmute-generated") {
		t.Fatalf("error missing sentinel hint: %v", err)
	}
	content, readErr := os.ReadFile(userFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "mine" {
		t.Fatalf("user file changed: %q", content)
	}
}

func TestCompilePipecat_rewritesSentinelGeneratedDir(t *testing.T) { // V41
	dir := filepath.Join(t.TempDir(), "support-bot")
	if _, err := run(t, "init", dir); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := run(t, "compile", dir, "pipecat"); err != nil {
		t.Fatalf("compile: %v", err)
	}
	stale := filepath.Join(dir, "targets", "pipecat", "generated", "stale.txt")
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := run(t, "compile", dir, "pipecat"); err != nil {
		t.Fatalf("compile again: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale file should be removed, got err %v", err)
	}
}

func TestCompilePipecat_missingRequiredSecret(t *testing.T) { // V37
	dir := filepath.Join(t.TempDir(), "support-bot")
	if _, err := run(t, "init", dir); err != nil {
		t.Fatalf("init: %v", err)
	}
	content := []byte(`local:
  env_file: .env.local
secrets:
  SLNG_API_KEY:
    local_key: SLNG_API_KEY
`)
	if err := os.WriteFile(filepath.Join(dir, "env", "secrets.yaml"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := run(t, "compile", dir, "pipecat")
	if err == nil {
		t.Fatal("expected missing secret error")
	}
	if !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("error missing secret name: %v", err)
	}
}

func TestCompilePipecat_noDeployShapedFlags(t *testing.T) { // V39
	dir := filepath.Join(t.TempDir(), "support-bot")
	if _, err := run(t, "init", dir); err != nil {
		t.Fatalf("init: %v", err)
	}

	_, err := run(t, "compile", dir, "pipecat", "--image", "example")
	if err == nil {
		t.Fatal("expected unknown flag error")
	}
	if !strings.Contains(err.Error(), "unknown flag: --image") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompileSLNG_generatesNamedJSONOffline(t *testing.T) { // V47, V48, V52
	dir := filepath.Join(t.TempDir(), "directory-name")
	if _, err := run(t, "init", dir); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "project.yaml"), []byte("name: Casavo - Casa\nversion: 0.1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "env")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "targets")); err != nil {
		t.Fatal(err)
	}

	output, err := run(t, "compile", dir, "slng")
	if err != nil {
		t.Fatalf("compile slng: %v\n%s", err, output)
	}
	if !bytes.Contains([]byte(output), []byte(`warning: tool "lookup_order" uses http handler ref "orders" and is omitted`)) {
		t.Fatalf("compile missing unsupported tool warning:\n%s", output)
	}

	generatedDir := filepath.Join(dir, "targets", "slng", "generated")
	entries, err := os.ReadDir(generatedDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "Casavo - Casa.json" {
		t.Fatalf("generated entries = %v, want [Casavo - Casa.json]", entries)
	}
	if _, err := os.Stat(filepath.Join(generatedDir, "compile-report.json")); !os.IsNotExist(err) {
		t.Fatalf("slng compile must not write compile-report.json, got err %v", err)
	}

	payload, err := os.ReadFile(filepath.Join(generatedDir, "Casavo - Casa.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(payload, []byte(`"name": "Casavo - Casa"`)) {
		t.Fatalf("payload should use project.yaml.name, got:\n%s", payload)
	}
	if bytes.Contains(payload, []byte(`"tools"`)) {
		t.Fatalf("unsupported relative HTTP tool should be omitted, got:\n%s", payload)
	}
}

func TestCompileSLNG_invalidProjectNameNoPartialWrite(t *testing.T) { // V49
	dir := filepath.Join(t.TempDir(), "support-bot")
	if _, err := run(t, "init", dir); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "project.yaml"), []byte("name: bad/name\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := run(t, "compile", dir, "slng")
	if err == nil {
		t.Fatal("expected invalid project name error")
	}
	if !strings.Contains(err.Error(), "must not contain path separators") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "targets", "slng")); !os.IsNotExist(statErr) {
		t.Fatalf("invalid name should not write partial slng output, stat err %v", statErr)
	}
}

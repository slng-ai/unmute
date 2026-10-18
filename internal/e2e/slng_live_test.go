//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestSlngLive deploys each test package whose targets.yaml names an slng
// target to SLNG with the real key, then holds every scenario as a real web
// call and checks the call record SLNG keeps:
//
//  1. `unmute deploy --target slng` publishes the package's Python tools as
//     code tools and pushes the agent, as an author's deploy does.
//  2. `voiceai agents web-sessions create` opens a session, and slng_caller.py
//     joins it as the caller, speaking each scripted line with SLNG speech.
//  3. `voiceai agents calls get` returns the record: the call completed, each
//     tool the scenario names ran and succeeded, and the agent said each line
//     the scenario names, as the caller heard it.
//
// It needs SLNG_API_KEY, voiceai 0.1.23 or newer and uv, so it runs in Nightly
// and before a release, never on a pull request:
//
//	make e2e-slng
//	make e2e-slng PKG=slng-live
//
// The agent keeps one name across runs, so each run replaces the last one's.
func TestSlngLive(t *testing.T) {
	root := repoRoot(t)
	loadDotEnv(t, filepath.Join(root, ".env"))
	if os.Getenv("SLNG_API_KEY") == "" {
		t.Skip("set SLNG_API_KEY in .env to run it")
	}
	for _, tool := range []string{"voiceai", "uv"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("install %s to run it", tool)
		}
	}
	// The key is regional, and voiceai refuses one sent to another region.
	if os.Getenv("VOICEAI_REGION") == "" {
		t.Setenv("VOICEAI_REGION", "eu-north")
	}
	t.Setenv("VOICEAI_API_KEY", os.Getenv("SLNG_API_KEY"))
	unmute := buildUnmute(t, root)
	caller := filepath.Join(root, "internal", "e2e", "slng_caller.py")
	for _, pkg := range scenarioPackages(t, root) {
		if !targetsSlng(t, pkg) {
			continue
		}
		t.Run(filepath.Base(pkg), func(t *testing.T) {
			agentID := deploySlng(t, unmute, pkg)
			scenarios, _ := filepath.Glob(filepath.Join(pkg, "scenarios", "*.json"))
			for _, scenario := range scenarios {
				t.Run(strings.TrimSuffix(filepath.Base(scenario), ".json"), func(t *testing.T) {
					runSlngScenario(t, caller, agentID, scenario)
				})
			}
		})
	}
}

// slngScenario is a scenario file: the caller's lines, and what the call
// record has to show.
type slngScenario struct {
	Name  string `json:"name"`
	Turns []struct {
		Caller string `json:"caller"`
	} `json:"turns"`
	// Tools each ran at least once and succeeded.
	Tools []string `json:"tools"`
	// Says are lines the agent said, as the caller heard them.
	Says []string `json:"says"`
}

// callerReport is what slng_caller.py prints.
type callerReport struct {
	Greeting string `json:"greeting"`
	Turns    []struct {
		Caller string `json:"caller"`
		Agent  string `json:"agent"`
	} `json:"turns"`
	AgentEnded bool `json:"agent_ended"`
}

// callRecord is the part of `voiceai agents calls get` the test reads.
type callRecord struct {
	Status         string    `json:"status"`
	EndReason      string    `json:"call_end_reason"`
	FinalizedAt    string    `json:"finalized_at"`
	ToolExecutions []toolRun `json:"tool_executions"`
}

// toolRun is one tool execution in a call record.
type toolRun struct {
	Name    string `json:"tool_name"`
	Outcome string `json:"outcome"`
}

func runSlngScenario(t *testing.T, caller, agentID, path string) {
	var scenario slngScenario
	readJSON(t, path, &scenario)
	var session struct {
		CallID string `json:"call_id"`
		URL    string `json:"livekit_url"`
		Token  string `json:"livekit_token"`
	}
	voiceai(t, &session, "agents", "web-sessions", "create", agentID)

	cmd := exec.Command("uv", "run", "--quiet", "--script", caller, path)
	// The timeline of agent states and transcript segments, printed when a
	// check fails, so a race shows its own evidence.
	cmd.Env = append(os.Environ(), "LIVEKIT_URL="+session.URL, "LIVEKIT_TOKEN="+session.Token, "SLNG_CALLER_TRACE=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("caller: %v\n%s", err, tail(stderr.String(), 40))
	}
	var heard callerReport
	if err := json.Unmarshal(stdout.Bytes(), &heard); err != nil {
		t.Fatalf("caller printed no report: %v\n%s", err, stdout.String())
	}
	t.Logf("%s\n  agent : %s", scenario.Name, heard.Greeting)
	for _, turn := range heard.Turns {
		t.Logf("  caller: %s\n  agent : %s", turn.Caller, turn.Agent)
	}

	record := finalizedCall(t, agentID, session.CallID)
	if !heard.AgentEnded {
		t.Errorf("the agent never left the call: it should end it with end_call")
	}
	if record.Status != "completed" {
		t.Errorf("call status %q (%s), want completed", record.Status, record.EndReason)
	}
	for _, want := range scenario.Tools {
		if !slices.Contains(record.ToolExecutions, toolRun{Name: want, Outcome: "succeeded"}) {
			t.Errorf("tool %s never ran and succeeded; the record shows %+v", want, record.ToolExecutions)
		}
	}
	said := heard.Greeting
	for _, turn := range heard.Turns {
		said += "\n" + turn.Agent
	}
	for _, want := range scenario.Says {
		if !strings.Contains(said, want) {
			t.Errorf("the caller never heard %q", want)
		}
	}
	if t.Failed() {
		t.Logf("caller timeline (call %s):\n%s", session.CallID, stderr.String())
	}
}

// deploySlng deploys one package's slng target from a scratch copy and returns
// the agent's id, read from the deploy report.
func deploySlng(t *testing.T, unmute, pkg string) string {
	t.Helper()
	work := filepath.Join(t.TempDir(), filepath.Base(pkg))
	if err := os.CopyFS(work, os.DirFS(pkg)); err != nil {
		t.Fatalf("copy %s: %v", pkg, err)
	}
	cmd := exec.Command(unmute, "deploy", ".", "--target", "slng")
	cmd.Dir = work
	cmd.Stdin = strings.NewReader("") // no trunk is attached: the call is a web session
	out, err := cmd.CombinedOutput()
	t.Logf("unmute deploy\n%s", out)
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	var report struct {
		AgentID string `json:"agent_id"`
		Outcome string `json:"outcome"`
	}
	readJSON(t, filepath.Join(work, "build", "slng", "deploy-report.json"), &report)
	if report.Outcome != "deployed" || report.AgentID == "" {
		t.Fatalf("deploy report says %q with agent %q", report.Outcome, report.AgentID)
	}
	return report.AgentID
}

// finalizedCall reads the call record once SLNG has finalized it. SLNG writes
// the record a moment after the call ends, so it is read until finalized_at is
// set, within a deadline.
func finalizedCall(t *testing.T, agentID, callID string) callRecord {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		var record callRecord
		voiceai(t, &record, "agents", "calls", "get", agentID, callID)
		if record.FinalizedAt != "" {
			return record
		}
		if time.Now().After(deadline) {
			t.Fatalf("call %s was not finalized within a minute; status %q", callID, record.Status)
		}
		time.Sleep(2 * time.Second)
	}
}

// voiceai runs one read with --json and decodes it.
func voiceai(t *testing.T, out any, args ...string) {
	t.Helper()
	cmd := exec.Command("voiceai", append(args, "--json")...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("voiceai %s: %v\n%s", strings.Join(args[:2], " "), err, stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), out); err != nil {
		t.Fatalf("voiceai %s: %v", strings.Join(args[:2], " "), err)
	}
}

func readJSON(t *testing.T, path string, out any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// targetsSlng reports whether a package's targets.yaml names the slng target.
func targetsSlng(t *testing.T, pkg string) bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(pkg, "targets.yaml"))
	if err != nil {
		t.Fatalf("read targets.yaml: %v", err)
	}
	return bytes.Contains(data, []byte("provider: slng"))
}

func tail(text string, lines int) string {
	all := strings.Split(strings.TrimSpace(text), "\n")
	return strings.Join(all[max(0, len(all)-lines):], "\n")
}

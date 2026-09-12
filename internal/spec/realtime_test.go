package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realtimePackage writes the smallest package that binds a live model, with the
// realtime entry spelled by the caller, and loads it.
func realtimePackage(t *testing.T, realtimeEntry string) (*Package, error) {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"instructions.md": "Help the caller.\n",
		"agent.yaml": "version: 1\nname: live-test\nentry_agent: desk\n" +
			"models:\n  realtime:\n" + realtimeEntry +
			"  think:\n    fast:\n      provider: openai\n      model: gpt-5.6-terra\n" +
			"agents:\n  desk:\n    instructions: instructions.md\n    realtime: live\n" +
			"channels:\n  web:\n    kind: realtime_audio\n" +
			"capacity:\n  peak_sessions: 2\n  max_sessions: 4\n  avg_session_duration: 3m\n",
		"targets.yaml": "targets:\n  pipecat:\n    provider: pipecat\n    version: \"1.10.0\"\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return Load(dir)
}

// TestRealtimeEntryDecodesItsSixFields is the contract block from specs/021
// contracts/authoring.md, decoded field for field.
func TestRealtimeEntryDecodesItsSixFields(t *testing.T) {
	pkg, err := realtimePackage(t, "    - name: live\n      provider: openai\n      model: gpt-live-1\n      voice: marin\n      think: fast\n      description: the front desk\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Agent.Models.Realtime) != 1 {
		t.Fatalf("realtime entries = %d, want 1", len(pkg.Agent.Models.Realtime))
	}
	got := pkg.Agent.Models.Realtime[0]
	want := RealtimeDef{Name: "live", Provider: "openai", Model: "gpt-live-1", Voice: "marin", Think: "fast", Description: "the front desk"}
	if got != want {
		t.Errorf("decoded %+v, want %+v", got, want)
	}
	if pkg.Agent.Agents["desk"].Realtime != "live" || pkg.Agent.Agents["desk"].Think != "" {
		t.Errorf("agent decoded to %+v", pkg.Agent.Agents["desk"])
	}
}

// TestRealtimeEntryRefusesEveryOtherModelFieldWithItsLine: the strict decoder
// is what refuses temperature, language, speed and params, so an author gets
// the file, the line and the column rather than a validate rule per field.
func TestRealtimeEntryRefusesEveryOtherModelFieldWithItsLine(t *testing.T) {
	for _, field := range []string{"temperature: 0.4", "language: en", "speed: 1.1", "params: {a: 1}", "pace: snappy", "endpoint_env: X"} {
		_, err := realtimePackage(t, "    - name: live\n      provider: openai\n      model: gpt-live-1\n      "+field+"\n")
		if err == nil {
			t.Errorf("%s: a realtime entry accepted a field it has no slot for", field)
			continue
		}
		key := strings.SplitN(field, ":", 2)[0]
		if !strings.Contains(err.Error(), key) || !strings.Contains(err.Error(), "agent.yaml") {
			t.Errorf("%s: refusal names neither the field nor the file: %v", field, err)
		}
		// goccy/go-yaml's strict error carries the line of the offending key.
		if !strings.Contains(err.Error(), "[9:7]") {
			t.Errorf("%s: refusal carries no line: %v", field, err)
		}
	}
}

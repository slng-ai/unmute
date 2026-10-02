package tui

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/scaffold"
)

// TestMaintainKeepsAnAuthorsStatePy guards the same silent data loss as the
// agent-name and task-handoffs round-trips, over the file typed state lives in.
//
// `unmute maintain` rewrites the package from scaffold.Data, so a file that
// struct does not carry is a file the console deletes. state.py is author
// Python, so it has to come back byte for byte, and the `variables:` entries
// that give a field a source or a confirming task have to come back with it.
// The console offers no editor for either, exactly as it offers none for
// `knowledge:`.
func TestMaintainKeepsAnAuthorsStatePy(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pkg")
	state := []byte("from pydantic import BaseModel\n\n\nclass State(BaseModel):\n    caller_phone: str = \"\"\n    reason: str = \"\"\n")
	data := scaffold.Data{
		Name: "pkg", AgentName: "acme-salon",
		Instructions: "Take appointment calls for one salon.",
		State:        state,
		Variables: []scaffold.Variable{
			{Name: "caller_phone", Source: "from_number"},
		},
	}
	data.SetTarget("livekit")
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}

	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, loss := range agent.losses {
		for _, about := range []string{"state.py", "variable"} {
			if strings.Contains(loss, about) {
				t.Errorf("the console cannot preserve %q, so maintain would delete it", loss)
			}
		}
	}
	if !bytes.Equal(agent.data.State, state) {
		t.Errorf("state.py read back as %q, want it byte for byte", agent.data.State)
	}

	again := filepath.Join(t.TempDir(), "again")
	if _, err := scaffold.Write(again, agent.data); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(filepath.Join(again, "state.py"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(written, state) {
		t.Errorf("rewritten state.py = %q, want the author's file", written)
	}
	rewritten, err := os.ReadFile(filepath.Join(again, "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"variables:", "- name: caller_phone", "source: from_number"} {
		if !strings.Contains(string(rewritten), want) {
			t.Errorf("rewritten agent.yaml missing %q:\n%s", want, rewritten)
		}
	}
}

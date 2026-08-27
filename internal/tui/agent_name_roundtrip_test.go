package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/scaffold"
)

// TestMaintainKeepsTheAgentName guards the same silent data loss as the
// knowledge round-trip, over the one field where losing it costs a live agent.
//
// `unmute maintain` rewrites agent.yaml from scaffold.Data, so a field that
// struct does not carry is a field the console deletes. `name:` is the deployed
// agent's identity on SLNG, and a package that loses it either stops compiling
// for that target or, worse, gets a different name and pushes a second agent
// beside the running one.
//
// The console offers no editor for it, exactly as it offers none for
// `knowledge:`. Carrying it is not optional either way.
func TestMaintainKeepsTheAgentName(t *testing.T) {
	root := filepath.Join(t.TempDir(), "some-folder")
	data := scaffold.Data{Name: "some-folder", AgentName: "acme-support"}
	data.SetTarget("livekit")
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}

	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, loss := range agent.losses {
		if strings.Contains(loss, "name") {
			t.Errorf("the console cannot preserve %q, so maintain would delete it", loss)
		}
	}
	// Read back verbatim, and not from the folder: the folder is called
	// something else here on purpose, because inferring the name from it is the
	// mistake this field exists to end.
	if agent.data.AgentName != "acme-support" {
		t.Errorf("AgentName = %q, want the authored name", agent.data.AgentName)
	}
}

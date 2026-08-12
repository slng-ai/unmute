package spec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// FR-030 locks the authoring surface this feature deliberately did not grow.
//
// Pipecat Cloud telephony added a route, not a field. Two things were proposed
// and rejected: a hosting-model selector (there is one hosting model per driver,
// so an author has nothing to choose) and a phone channel on the Daily route
// (the compiler derives the route facts, and a channel would drag `capacity`
// in with it per SCHEMA §4.10). Both rejections were written down in the spec
// and in contracts/authoring.md, and a fact stated twice with nothing enforcing
// it is the shape Principle III exists to prevent. So they are tested.
//
// If you are here because this test failed, you are adding authoring surface.
// That is allowed, but the constitution prices it: a numbered dated SCHEMA
// amendment, the derived schemas, a capability row, the agreement tests, the
// scaffold templates, the interactive console, the examples, and docs/user/, all
// in one commit. Delete a line here only alongside that work.

func TestAuthoringSchemaHasNoHostingModelField(t *testing.T) {
	schema, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"hosting", "hosting_model", "deployment_model", "managed", "cloud", "self_hosted",
	} {
		if found := searchSchema(decoded, name); found != nil {
			t.Errorf("derived authoring schema grew a %q property: %v", name, found)
		}
	}
}

// The Daily route declares a transport and nothing else. No connection, because
// Daily's own infrastructure delivers the call; no phone channel, because the
// compiler derives what the route needs from the transport. Both absences are
// load-time facts, so they are asserted where an author would hit them.
func TestDailyRouteNeedsNoConnectionOrChannel(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"instructions.md": "Help the caller.\n",
		"agent.yaml":      "version: 1\nentry_agent: intake\nagents:\n  intake:\n    instructions: instructions.md\n",
		"targets.yaml": "targets:\n  pipecat:\n    provider: pipecat\n    version: \"1.5.0\"\n" +
			"    transport: daily-sip\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pkg, err := Load(dir)
	if err != nil {
		t.Fatalf("a Daily target with only a transport must load: %v", err)
	}
	got := pkg.Targets["pipecat"]
	if got.Transport != "daily-sip" {
		t.Fatalf("transport = %q, want daily-sip", got.Transport)
	}
	if got.Connection != "" {
		t.Errorf("connection = %q, want empty: the Daily route has no carrier connection", got.Connection)
	}
	if pkg.Agent.Channels != nil {
		t.Errorf("channels = %+v, want none declared", pkg.Agent.Channels)
	}
}

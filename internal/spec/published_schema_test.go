package spec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/goccy/go-yaml"
)

// The published schema accepts what the decoder accepts. Two authoring shapes
// are decoded by hand rather than by reflection, a `success:` item and a
// group's `steps:` item, and the reflected schema published neither: a group
// written the way every group was written before `skip_when_confirmed:`
// existed failed the schema, and `- status: booked` failed it too. Held on
// whole packages, because a fragment cannot show which key the schema lost.
func TestPublishedSchemaAcceptsWhatTheDecoderAccepts(t *testing.T) {
	resolved := resolvedSchema(t)
	for _, dir := range []string{
		filepath.Join("..", "testdata", "terminal_step"),
		filepath.Join("..", "testdata", "remy"),
		filepath.Join("..", "..", "examples", "salon-concierge"),
	} {
		if err := resolved.Validate(authoredInstance(t, dir)); err != nil {
			t.Errorf("%s: the published schema refuses a package the decoder accepts: %v", dir, err)
		}
	}
}

// The schema says the "exactly one" part too, which is the refusal a dropped
// indent hits, so a reader validating their file sees it before the decoder.
func TestPublishedSchemaRefusesASuccessItemHoldingTwoKeys(t *testing.T) {
	resolved := resolvedSchema(t)
	pkg := authoredInstance(t, filepath.Join("..", "testdata", "terminal_step"))
	doc := pkg["agent"]
	agents := doc.(map[string]any)["agents"].(map[string]any)
	tasks := agents["desk"].(map[string]any)["tasks"].([]any)
	finish := tasks[0].(map[string]any)["finish"].([]any)
	success := finish[0].(map[string]any)["success"].([]any)
	success[0] = map[string]any{"status": "existing", "summary": "found"}
	if err := resolved.Validate(pkg); err == nil {
		t.Fatal("a success item holding two keys passed the published schema")
	}
}

// authoredInstance is the package as the schema publishes it: the agent.yaml
// document as written, every tool file as written, keyed by name, and the
// targets Load resolves from them. The two YAML documents are read raw rather
// than through Load, because the shapes under test are the ones the decoder
// rewrites on the way in.
func authoredInstance(t *testing.T, dir string) map[string]any {
	t.Helper()
	pkg, err := Load(dir)
	if err != nil {
		t.Fatalf("%s: the decoder refuses what this test assumes it accepts: %v", dir, err)
	}
	doc := loadInstance(t, filepath.Join(dir, "agent.yaml"))
	tools := map[string]any{}
	for _, item := range doc.(map[string]any)["tools"].([]any) {
		name := item.(string)
		tools[name] = loadInstance(t, filepath.Join(dir, "tools", name+".yaml"))
	}
	return map[string]any{"agent": doc, "tools": tools, "targets": jsonValue(t, pkg.Targets)}
}

func jsonValue(t *testing.T, value any) any {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func loadInstance(t *testing.T, path string) any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	// Through JSON, so every number and nested map takes the shape the
	// validator reads: it validates JSON values, and YAML decodes to more.
	return jsonValue(t, doc)
}

func resolvedSchema(t *testing.T) interface{ Validate(any) error } {
	t.Helper()
	schema, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

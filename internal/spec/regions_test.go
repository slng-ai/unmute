package spec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

// deployment_region takes one region or several (N32). The scalar form is the
// one N18 shipped, so it must keep loading unchanged; the list form is new.

func loadRegions(t *testing.T, value string) (Regions, error) {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"agent.yaml":   "version: 1\nentry_agent: intake\n",
		"targets.yaml": "targets:\n  livekit:\n    provider: livekit\n    deployment_region: " + value + "\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pkg, err := Load(dir)
	if err != nil {
		return nil, err
	}
	return pkg.Targets["livekit"].DeploymentRegion, nil
}

func TestDeploymentRegionAcceptsOneOrMany(t *testing.T) { // N32
	for _, tc := range []struct {
		name  string
		value string
		want  []string
	}{
		{"scalar", "us-east", []string{"us-east"}},
		{"flow list", "[us-east, eu-central]", []string{"us-east", "eu-central"}},
		{"block list", "\n      - us-east\n      - eu-central", []string{"us-east", "eu-central"}},
		{"one-element list", "[us-east]", []string{"us-east"}},
		{"absent", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := loadRegions(t, tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("deployment_region = %q, want %q", got, tc.want)
			}
		})
	}
}

// A value that is neither shape must fail with goccy's own position, which is
// why Regions implements InterfaceUnmarshaler rather than BytesUnmarshaler.
func TestDeploymentRegionMappingFailsWithPosition(t *testing.T) { // N32
	_, err := loadRegions(t, "\n      region: us-east")
	if err == nil {
		t.Fatal("want an error for a mapping value")
	}
	message := err.Error()
	if !strings.Contains(message, "targets.yaml") || !strings.Contains(message, "5:13") {
		t.Fatalf("want targets.yaml plus line:col, got %v", err)
	}
}

// A single region round-trips as the bare scalar the author wrote: the TUI
// rewrites targets.yaml from this value, and reshaping someone's file because
// they opened an unrelated form is a change they never asked for.
func TestDeploymentRegionMarshalsOneAsScalar(t *testing.T) { // N32
	one, err := yaml.Marshal(map[string]Regions{"deployment_region": {"us-east"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(one); got != "deployment_region: us-east\n" {
		t.Fatalf("one region marshalled as %q", got)
	}
	several, err := yaml.Marshal(map[string]Regions{"deployment_region": {"us-east", "eu-central"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(several); !strings.Contains(got, "- us-east") || !strings.Contains(got, "- eu-central") {
		t.Fatalf("several regions marshalled as %q", got)
	}
}

// The published authoring schema is derived, so the one-or-many shape has to
// survive derivation. Mirrors how internal/ir/schema_test.go pins its unions.
func TestSchemaKeepsBothRegionShapes(t *testing.T) { // N32
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
	region := findSchemaProperty(t, decoded, "deployment_region")
	oneOf, ok := region["oneOf"].([]any)
	if !ok || len(oneOf) != 2 {
		t.Fatalf("deployment_region is not a two-way oneOf: %v", region)
	}
	kinds := make([]string, 0, 2)
	for _, alternative := range oneOf {
		kinds = append(kinds, alternative.(map[string]any)["type"].(string))
	}
	if strings.Join(kinds, ",") != "string,array" {
		t.Fatalf("deployment_region oneOf types = %v, want string then array", kinds)
	}
}

// findSchemaProperty walks the derived schema for one named property, wherever
// the library placed it ($defs or inline).
func findSchemaProperty(t *testing.T, node map[string]any, name string) map[string]any {
	t.Helper()
	if properties, ok := node["properties"].(map[string]any); ok {
		if found, ok := properties[name].(map[string]any); ok {
			return found
		}
	}
	for _, value := range node {
		switch typed := value.(type) {
		case map[string]any:
			if found := searchSchema(typed, name); found != nil {
				return found
			}
		}
	}
	t.Fatalf("property %q is not in the derived schema", name)
	return nil
}

func searchSchema(node map[string]any, name string) map[string]any {
	if properties, ok := node["properties"].(map[string]any); ok {
		if found, ok := properties[name].(map[string]any); ok {
			return found
		}
	}
	for _, value := range node {
		if child, ok := value.(map[string]any); ok {
			if found := searchSchema(child, name); found != nil {
				return found
			}
		}
	}
	return nil
}

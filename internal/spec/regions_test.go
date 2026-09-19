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
			if strings.Join(got.Names(), ",") != strings.Join(tc.want, ",") {
				t.Fatalf("deployment_region = %q, want %q", got.Names(), tc.want)
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
	one, err := yaml.Marshal(map[string]Regions{"deployment_region": {{Name: "us-east"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(one); got != "deployment_region: us-east\n" {
		t.Fatalf("one region marshalled as %q", got)
	}
	several, err := yaml.Marshal(map[string]Regions{"deployment_region": {{Name: "us-east"}, {Name: "eu-central"}}})
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
	// An item of the list form is a bare region or a region carrying its swaps,
	// so the swap shape has to survive derivation too.
	items, ok := oneOf[1].(map[string]any)["items"].(map[string]any)
	if !ok {
		t.Fatalf("the list form has no items schema: %v", oneOf[1])
	}
	itemOneOf, ok := items["oneOf"].([]any)
	if !ok || len(itemOneOf) != 2 {
		t.Fatalf("a region item is not a two-way oneOf: %v", items)
	}
	withSwaps := itemOneOf[1].(map[string]any)
	if withSwaps["maxProperties"] != float64(1) {
		t.Fatalf("a region with swaps may name more than one region: %v", withSwaps)
	}
	swaps, ok := withSwaps["additionalProperties"].(map[string]any)
	if !ok || swaps["type"] != "array" {
		t.Fatalf("a region's swaps are not a list: %v", withSwaps)
	}
}

// A region may carry the model names it swaps, which is what lets one package
// run one agent in several places with different models behind the same names.
func TestDeploymentRegionCarriesModelSwaps(t *testing.T) {
	got, err := loadRegions(t, "\n      - us-west\n      - eu-north:\n          - transcriber: transcriber_fr\n          - voice: voice_fr")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.Names(), ",") != "us-west,eu-north" {
		t.Fatalf("regions = %v", got.Names())
	}
	if len(got[0].Swaps) != 0 {
		t.Fatalf("a bare region carries swaps: %v", got[0].Swaps)
	}
	if len(got[1].Swaps) != 2 {
		t.Fatalf("eu-north swaps = %v, want two", got[1].Swaps)
	}
	if got[1].Swaps[0].Key != "transcriber" || got[1].Swaps[0].Value != "transcriber_fr" {
		t.Fatalf("first swap = %+v", got[1].Swaps[0])
	}
	if got[1].Swaps[1].Key != "voice" || got[1].Swaps[1].Value != "voice_fr" {
		t.Fatalf("second swap = %+v", got[1].Swaps[1])
	}
}

// The refusals a swap list can hit at decode. Each one names its line, because
// a region item is two bare words and the mistake is an indent or a typo.
func TestDeploymentRegionRefusesMalformedItems(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  string
	}{
		{
			"two regions in one item",
			"\n      - us-west: []\n        eu-north: []",
			"naming 2 regions",
		},
		{
			"swaps are not a list",
			"\n      - eu-north:\n          transcriber: transcriber_fr",
			"model swaps are a list",
		},
		{
			"a swap value is not a model name",
			"\n      - eu-north:\n          - transcriber: 7",
			"names another model entry",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadRegions(t, tc.value)
			if err == nil {
				t.Fatal("want a refusal")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v does not say %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), "targets.yaml") {
				t.Fatalf("error %v does not name the file", err)
			}
		})
	}
}

// A region with swaps round-trips as the block an author wrote, so the console
// rewriting targets.yaml for an unrelated edit does not drop them.
func TestDeploymentRegionMarshalsSwaps(t *testing.T) {
	encoded, err := yaml.Marshal(map[string]Regions{"deployment_region": {
		{Name: "us-west"},
		{Name: "eu-north", Swaps: []Pair{{Key: "voice", Value: "voice_fr"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"- us-west", "- eu-north:", "- voice: voice_fr"} {
		if !strings.Contains(string(encoded), want) {
			t.Fatalf("marshalled regions %q do not contain %q", encoded, want)
		}
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

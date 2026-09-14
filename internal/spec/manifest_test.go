package spec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestParseManifestContract(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"minimal", "", ""},
		{"empty allow", "models:\n  listen: []\nlanguages:\n  allow: []\n", ""},
		{"null list", "languages:\n  allow: null\n", "null"},
		{"null section", "languages: null\n", "null"},
		{"missing allow", "languages: {}\n", "allowlist"},
		{"unknown field", "owner: team\n", "unknown"},
		{"duplicate key", "version: 2\n", "already defined"},
		{"duplicate values", "languages:\n  allow: [en, EN]\n", "duplicate"},
		{"duplicate provider", "models:\n  listen:\n    - provider: slng\n      allow: []\n    - provider: slng\n      allow: []\n", "duplicate"},
		{"bad role", "regions:\n  models:\n    - role: tts\n      provider: slng\n      allow: []\n", "choose listen"},
		{"bad language", "languages:\n  allow: [english_words]\n", "language tag"},
		{"bad target", "targets:\n  allow: [livekitt]\n", "unknown value"},
		{"bad kind", "tools:\n  kinds:\n    allow: [webhooks]\n", "unknown value"},
		{"bad tracing", "tracing:\n  allow: [logs]\n", "unknown value"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := ParseManifest([]byte("manifest: acme-corp\nversion: 1\n" + tc.body))
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("error = %v, want %s", err, tc.want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if m.Name != "acme-corp" || m.Version != 1 {
				t.Fatalf("identity: %#v", m)
			}
			if tc.name == "empty allow" && (m.Models.Listen == nil || m.Languages.Allow == nil) {
				t.Fatal("empty lists lost")
			}
			if tc.name == "minimal" && (m.Models.Listen != nil || m.Languages != nil) {
				t.Fatal("omitted restrictions invented")
			}
		})
	}
	if _, err := ManifestSchema(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadManifestLink(t *testing.T) {
	for _, tc := range []struct {
		name, link string
		file       bool
		want       string
	}{
		{"legacy", "", false, ""}, {"linked", "manifest", true, ""},
		{"unlinked", "", true, "link it"}, {"missing", "manifest", false, "manifest"},
		{"elsewhere", "../manifest", true, "package-root"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			agent := "version: 1\nmodels: {}\nagents: {}\nchannels: {}\n"
			if tc.link != "" {
				agent += "manifest: " + tc.link + "\n"
			}
			data := []byte("# company contract\nmanifest: acme-corp\nversion: 1\n")
			for name, contents := range map[string][]byte{"agent.yaml": []byte(agent), "targets.yaml": []byte("targets: {}\n")} {
				if err := os.WriteFile(filepath.Join(dir, name), contents, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.file {
				if err := os.WriteFile(filepath.Join(dir, "manifest"), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			pkg, err := Load(dir)
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.file && (pkg.Manifest == nil || string(pkg.ManifestBytes) != string(data)) {
				t.Fatal("manifest copy or identity lost")
			}
		})
	}
}

func TestManifestRoundTripPreservesEmptyRoleAllowlist(t *testing.T) {
	for _, body := range []string{"", "models:\n  listen: []\n"} {
		manifest, err := ParseManifest([]byte("manifest: acme\nversion: 1\n" + body))
		if err != nil {
			t.Fatal(err)
		}
		for _, marshal := range []func(any) ([]byte, error){json.Marshal, yaml.Marshal} {
			data, err := marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := ParseManifest(data)
			if err != nil {
				t.Fatalf("%s: %v", data, err)
			}
			if (manifest.Models.Listen == nil) != (restored.Models.Listen == nil) {
				t.Fatalf("empty role lost its restriction: %s", data)
			}
		}
	}
}

func TestManifestSchemaMatchesNullAndIdentityRules(t *testing.T) {
	schema, err := ManifestSchema()
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		body  string
		valid bool
	}{
		{`{"manifest":"acme","version":1}`, true},
		{`{"manifest":"acme","version":1,"models":{"listen":[]}}`, true},
		{`{"manifest":"acme","version":1,"languages":{"allow":[]}}`, true},
		{`{"manifest":"acme","version":1,"languages":null}`, false},
		{`{"manifest":"acme","version":1,"languages":{"allow":null}}`, false},
		{`{"manifest":"acme","version":1,"models":{"listen":null}}`, false},
		{`{"manifest":"acme","version":1,"models":{"listen":[null]}}`, false},
		{`{"manifest":"acme","version":0}`, false},
		{`{"manifest":"  ","version":1}`, false},
	} {
		var value any
		if err := json.Unmarshal([]byte(tc.body), &value); err != nil {
			t.Fatal(err)
		}
		if err := resolved.Validate(value); (err == nil) != tc.valid {
			t.Errorf("schema validity %v for %s: %v", tc.valid, tc.body, err)
		}
	}
}

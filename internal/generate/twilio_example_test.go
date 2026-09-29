package generate

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var twilioExample = filepath.Join("..", "..", "examples", "twilio")

// yamlFence is one ```yaml block of a README.
var yamlFence = regexp.MustCompile("(?s)```yaml\n(.*?)```")

// twilioThinkVariant compiles the complete direct-provider binding documented
// in the README, so a nested upstream provider cannot match by accident.
func twilioThinkVariant(t *testing.T, provider string) string {
	t.Helper()
	readme, err := os.ReadFile(filepath.Join(twilioExample, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	var block string
	for _, match := range yamlFence.FindAllStringSubmatch(string(readme), -1) {
		if strings.Contains(match[1], "\n      provider: "+provider+"\n") {
			block = match[1]
			break
		}
	}
	start := strings.Index(block, "    reasoning:\n")
	if start < 0 {
		t.Fatalf("the README shows no direct %s reasoning: block", provider)
	}
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(twilioExample)); err != nil {
		t.Fatal(err)
	}
	_ = os.RemoveAll(filepath.Join(dir, "build"))
	path := filepath.Join(dir, "agent.yaml")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	from, to := strings.Index(text, "    reasoning:\n"), strings.Index(text, "  listen:\n")
	if from < 0 || to < from {
		t.Fatal("agent.yaml has no reasoning: entry before listen:")
	}
	text = text[:from] + block[start:] + text[to:]
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Each build installs its model SDK and requests exactly the binding's keys.
func TestTwilioExampleCompilesOnThinkProviders(t *testing.T) {
	for _, tc := range []struct {
		name, dir     string
		sdk, otherSDK string
		keys          []string
		absentKeys    []string
	}{
		{"slng", twilioExample, "openai==", "google-genai", []string{"SLNG_API_KEY", "OPENAI_API_KEY"}, []string{"GOOGLE_API_KEY"}},
		{"openai", twilioThinkVariant(t, "openai"), "openai==", "google-genai", []string{"OPENAI_API_KEY"}, []string{"SLNG_API_KEY", "GOOGLE_API_KEY"}},
		{"gemini", twilioThinkVariant(t, "google"), "google-genai==", "openai==", []string{"GOOGLE_API_KEY"}, []string{"SLNG_API_KEY", "OPENAI_API_KEY"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			artifact := twilioArtifact(t, tc.dir, "twilio")
			pyproject := artifactFile(t, artifact, "pyproject.toml")
			if strings.Contains(pyproject, `name = "twilio"`) {
				t.Fatal("the example's Python project name shadows its Twilio SDK dependency")
			}
			if !strings.Contains(pyproject, tc.sdk) || strings.Contains(pyproject, tc.otherSDK) {
				t.Errorf("pyproject.toml should pin %s and not %s:\n%s", tc.sdk, tc.otherSDK, pyproject)
			}
			env := artifactFile(t, artifact, ".env.example")
			for _, key := range tc.keys {
				if !strings.Contains(env, key+"=") {
					t.Errorf(".env.example should ask for %s:\n%s", key, env)
				}
			}
			for _, key := range tc.absentKeys {
				if strings.Contains(env, key) {
					t.Errorf(".env.example should not ask for %s:\n%s", key, env)
				}
			}
		})
	}
}

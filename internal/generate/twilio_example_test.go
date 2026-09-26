package generate

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var twilioExample = filepath.Join("..", "..", "examples", "twilio-conversation-relay")

// yamlFence is one ```yaml block of a README.
var yamlFence = regexp.MustCompile("(?s)```yaml\n(.*?)```")

// twilioGeminiVariant is the public example with its think binding replaced by
// the Gemini block its README tells a reader to paste. The block comes from
// the README itself, so the README cannot show one that does not compile.
func twilioGeminiVariant(t *testing.T) string {
	t.Helper()
	readme, err := os.ReadFile(filepath.Join(twilioExample, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	var block string
	for _, match := range yamlFence.FindAllStringSubmatch(string(readme), -1) {
		if strings.Contains(match[1], "provider: google") {
			block = match[1]
			break
		}
	}
	start := strings.Index(block, "    reasoning:\n")
	if start < 0 {
		t.Fatal("the README shows no Gemini reasoning: block")
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

// Each build installs the one model SDK its binding names and asks for that
// provider's key only.
func TestTwilioExampleCompilesOnBothThinkProviders(t *testing.T) {
	for _, tc := range []struct {
		name, dir     string
		sdk, otherSDK string
		key, otherKey string
	}{
		{"openai", twilioExample, "openai==", "google-genai", "OPENAI_API_KEY", "GOOGLE_API_KEY"},
		{"gemini", twilioGeminiVariant(t), "google-genai==", "openai==", "GOOGLE_API_KEY", "OPENAI_API_KEY"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			artifact := twilioArtifact(t, tc.dir, "twilio")
			pyproject := artifactFile(t, artifact, "pyproject.toml")
			if !strings.Contains(pyproject, tc.sdk) || strings.Contains(pyproject, tc.otherSDK) {
				t.Errorf("pyproject.toml should pin %s and not %s:\n%s", tc.sdk, tc.otherSDK, pyproject)
			}
			env := artifactFile(t, artifact, ".env.example")
			if !strings.Contains(env, tc.key+"=") || strings.Contains(env, tc.otherKey) {
				t.Errorf(".env.example should ask for %s and not %s:\n%s", tc.key, tc.otherKey, env)
			}
		})
	}
}

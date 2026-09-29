//go:build smoke

package generate

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTwilioSmoke proves the emitted apps are real Python: each installs from
// its own pins, passes ruff, and passes every offline ConversationRelay case in
// scripts/text_run_twilio.py against a scripted model built from the real SDK
// types. No network beyond the package index, no key, no Twilio account. It
// runs the acceptance package's two targets and the public example on both
// think providers.
func TestTwilioSmoke(t *testing.T) {
	harness, err := filepath.Abs(filepath.Join("..", "..", "scripts", "text_run_twilio.py"))
	if err != nil {
		t.Fatal(err)
	}
	for _, build := range []struct{ name, dir, instance string }{
		{"relay-desk/twilio-openai", relayDesk, "twilio-openai"},
		{"relay-desk/twilio-gemini", relayDesk, "twilio-gemini"},
		{"example/openai", twilioExample, "twilio"},
		{"example/gemini", twilioGeminiVariant(t), "twilio"},
	} {
		t.Run(build.name, func(t *testing.T) {
			artifact := twilioArtifact(t, build.dir, build.instance)
			dir := t.TempDir()
			for _, file := range artifact.Files {
				path := filepath.Join(dir, file.Path)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, file.Content, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			for _, args := range [][]string{
				{"run", "ruff", "check", "."},
				{"run", "python", harness, dir, "--fake"},
			} {
				cmd := uvCommand(args...)
				cmd.Dir = dir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("uv %v: %v\n%s", args, err, out)
				}
			}
		})
	}
}

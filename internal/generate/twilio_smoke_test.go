//go:build smoke

package generate

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTwilioSmoke proves both emitted apps are real Python: each installs from
// its own pins, passes ruff, and passes every offline ConversationRelay case in
// scripts/text_run_twilio.py against a scripted model built from the real SDK
// types. No network beyond the package index, no key, no Twilio account.
func TestTwilioSmoke(t *testing.T) {
	harness, err := filepath.Abs(filepath.Join("..", "..", "scripts", "text_run_twilio.py"))
	if err != nil {
		t.Fatal(err)
	}
	for _, instance := range []string{"twilio-openai", "twilio-gemini"} {
		t.Run(instance, func(t *testing.T) {
			artifact := twilioArtifact(t, relayDesk, instance)
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

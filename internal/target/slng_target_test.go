package target

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The push tool's commands appear on four surfaces: the emitted runbook, the
// example README, the docs-site page and the shipped skill. A reader meets
// exactly one of them, so a command that is wrong on one is wrong in the only
// place that reader looks.
//
// This is not hypothetical. The web-session command shipped here without its
// agent id, which the CLI requires, so every one of the four told an author to
// run something that fails. It was caught by reading slng-ai/sdks, not by a
// test, which is why there is now a test.
//
// The constants in slng_target.go are the owner. This holds the surfaces to
// them.
func TestSlngPushCommandsAgree(t *testing.T) {
	root := filepath.Join("..", "..")
	surfaces := map[string]string{
		"the emitted runbook": filepath.Join(root, "internal", "generate", "templates", "slng_v1", "README.md.tmpl"),
		"the example README":  filepath.Join(root, "examples", "slng-support", "README.md"),
		"the docs-site page":  filepath.Join(root, "docs-site", "targets", "slng.mdx"),
		"the shipped skill":   filepath.Join(root, "internal", "skill", "assets", "references", "package.md"),
	}
	for name, path := range surfaces {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s is missing: %v", name, err)
		}
		content := string(raw)
		// The runbook reaches these through template fields rather than literals,
		// so it is checked for the field names instead of the rendered text. The
		// rendered text itself is held by the goldens in internal/generate.
		if strings.HasSuffix(path, ".tmpl") {
			for _, field := range []string{"{{.CredentialEnv}}", "{{.WebSessionCommand}}", "{{.PushCommand}}", "{{.LoginCommand}}"} {
				if !strings.Contains(content, field) {
					t.Errorf("%s does not render %s, so it states a command instead of reading the one owner", name, field)
				}
			}
			continue
		}
		if !strings.Contains(content, SlngPushCredentialEnv) {
			t.Errorf("%s does not name %s, which is the key the push tool reads", name, SlngPushCredentialEnv)
		}
		// The web-session command takes an agent id. Every surface that names the
		// command has to name the id with it.
		if strings.Contains(content, "web-sessions create") && !strings.Contains(content, SlngWebSessionCommand) {
			t.Errorf("%s writes the web-session command without its agent id; %q is the whole command and the id is not optional",
				name, SlngWebSessionCommand)
		}
		// Nothing may claim a command the CLI does not have. `voiceai tools` is
		// the one an author would most reasonably expect and the one that does not
		// exist yet (slng-ai/sdks cli/src/commands, read 2026-08-25).
		if strings.Contains(content, "voiceai tools") {
			t.Errorf("%s names `voiceai tools`, which the CLI does not have: tool bodies are created through the API until it grows one", name)
		}
	}
}

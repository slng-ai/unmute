package target

import (
	"os"
	"path/filepath"
	"regexp"
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
			for _, field := range []string{"{{.CredentialEnv}}", "{{.WebSessionCommand}}", "{{.PushCommand}}", "{{.DeployCommand}}", "{{.LoginCommand}}"} {
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

// Every `voiceai` command every surface names must be one the CLI actually has.
//
// TestSlngPushCommandsAgree above holds the *wording* of the four commands that
// shipped wrong once. This holds something cheaper and broader: that no surface
// invents a command at all. The failure it exists for is plausible spelling. An
// author writing about the vault reaches for `voiceai secrets list`, and the
// CLI's command is `secret`, singular. The same trap sits on `tool` and `mcp`,
// both singular, next to `trunks`, which is plural. Nothing about that is
// guessable, so nothing about it should be guessed.
//
// Read from `voiceai --help` at 0.1.16 on 2026-09-03.
func TestEveryVoiceaiCommandNamedExists(t *testing.T) {
	groups := map[string]bool{
		"tts": true, "stt": true, "config": true, "models": true, "voices": true,
		"whoami": true, "login": true, "agents": true, "tool": true, "mcp": true,
		"secret": true, "trunks": true, "help": true,
	}
	// The subcommands under the groups this repository drives. A group not listed
	// here is not checked past its first word, because unmute does not use it.
	subcommands := map[string]map[string]bool{
		// `run` executes one already-published tool against its real dependencies,
		// and nothing runs without --confirm-side-effects. It is what an author
		// iterates a code tool with between pushes, so the runbook names it and
		// this must know it. Arrived in 0.1.16; the `tool` group had list and get
		// only at 0.1.15.
		"tool": {"list": true, "get": true, "run": true},
		// `run` connects to the server and refreshes its stored capability
		// snapshot. It is the fix unmute points at when a push refuses a snapshot
		// as stale, so it is a command the surfaces name and this must know.
		"mcp":    {"list": true, "get": true, "tools": true, "run": true},
		"secret": {"list": true, "get": true, "create": true},
		"trunks": {"list": true, "get": true},
		"agents": {"list": true, "get": true, "create": true, "update": true, "replace": true,
			"duplicate": true, "delete": true, "calls": true, "web-sessions": true, "push": true},
	}

	check := func(where, group, sub string) {
		if !groups[group] {
			t.Errorf("%s names `voiceai %s`, which is not a command the CLI has", where, group)
			return
		}
		if known, checked := subcommands[group]; checked && sub != "" && !known[sub] {
			t.Errorf("%s names `voiceai %s %s`, and %q has no %q subcommand", where, group, sub, group, sub)
		}
	}

	// The constants first, because they are the owner and a typo in one reaches
	// every surface at once. This is not circular: the two maps above come from
	// `voiceai --help`, not from these values.
	for _, command := range []SlngCommand{
		SlngWhoami, SlngSecretList, SlngSecretCreate, SlngToolList, SlngToolGet,
		SlngMCPList, SlngMCPTools, SlngTrunksList, SlngCallDispatch,
	} {
		sub := ""
		if len(command) > 1 {
			sub = command[1]
		}
		check("SlngCommand "+command.String(), command[0], sub)
	}

	// Then the author-facing surfaces. Deliberately not this package's own Go
	// source: its comments discuss commands that do *not* exist, which is the
	// point of them, and a prose scan cannot tell a warning from a claim.
	root := filepath.Join("..", "..")
	surfaces := []string{
		filepath.Join(root, "internal", "generate", "templates", "slng_v1", "README.md.tmpl"),
		filepath.Join(root, "examples", "slng-support", "README.md"),
		filepath.Join(root, "docs-site", "targets", "slng.mdx"),
		filepath.Join(root, "internal", "skill", "assets", "references", "package.md"),
	}
	// A word, then optionally a second, skipping any leading root flag so that
	// `voiceai --profile work whoami` is read as `whoami`.
	mention := regexp.MustCompile(`voiceai (?:--[a-z-]+ \S+ )*([a-z][a-z0-9-]*)(?: ([a-z][a-z0-9-]*))?`)
	for _, path := range surfaces {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s is missing: %v", path, err)
		}
		for _, match := range mention.FindAllStringSubmatch(string(raw), -1) {
			check(filepath.Base(path), match[1], match[2])
		}
	}
}

// The resource commands are argv first and prose second, so that a diagnostic
// quoting one cannot drift from the one that runs. This holds the rendering,
// and that .With() leaves its receiver alone: these are package-level values,
// and one caller's append would otherwise be every caller's.
func TestSlngCommandRendersAndDoesNotAlias(t *testing.T) {
	if got, want := SlngSecretList.String(), "voiceai secret list"; got != want {
		t.Errorf("SlngSecretList renders %q, want %q", got, want)
	}
	if got, want := SlngSecretCreate.With("ACME_API_KEY").String(), "voiceai secret create ACME_API_KEY"; got != want {
		t.Errorf("With() renders %q, want %q", got, want)
	}
	first := SlngMCPTools.With("one")
	second := SlngMCPTools.With("two")
	if first.String() != "voiceai mcp tools one" || second.String() != "voiceai mcp tools two" {
		t.Errorf("With() aliased its receiver: %q then %q", first, second)
	}
	if len(SlngMCPTools) != 2 {
		t.Errorf("With() grew the package-level value to %v", SlngMCPTools)
	}
}

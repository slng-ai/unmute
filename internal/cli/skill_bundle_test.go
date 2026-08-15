package cli

import (
	"regexp"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/skill"
	"github.com/spf13/cobra"
)

// TestSkillBundleNamesRealCommands is the fifth agreement test on the bundle.
// The other four live in internal/skill; this one lives here because the cobra
// tree is unexported and internal/cli already owns it.
//
// Every command and flag the bundle tells an assistant to run has to exist. A
// renamed flag that nobody re-documented fails here rather than reaching a user
// as a command that does not work.
func TestSkillBundleNamesRealCommands(t *testing.T) {
	root := newRootCmd()

	for name, content := range bundleFiles(t) {
		for _, invocation := range unmuteInvocations(string(content)) {
			words, flags := splitInvocation(invocation)
			path := longestCommandPath(root, words)
			if len(words) > 0 && len(path) == 0 {
				t.Errorf("%s names `unmute %s`, whose first word is not a command", name, invocation)
				continue
			}
			for _, flag := range flags {
				if !flagExists(root, flag) {
					t.Errorf("%s names `--%s` on `unmute %s`, which no command has", name, flag, strings.Join(path, " "))
				}
			}
		}
	}
}

// bundleFiles reads both destinations' content out of the embedded bundle.
func bundleFiles(t *testing.T) map[string][]byte {
	t.Helper()
	bundle := skill.New("test")
	files, err := bundle.Files(skill.Canonical)
	if err != nil {
		t.Fatal(err)
	}
	pointer, err := bundle.Files(skill.Pointer)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range pointer {
		files["pointer/"+name] = content
	}
	return files
}

var (
	inlineCode = regexp.MustCompile("`([^`\n]+)`")
	fence      = regexp.MustCompile("(?s)```[a-z]*\n(.*?)```")
)

// unmuteInvocations pulls every `unmute ...` out of a reference's inline code
// and fenced blocks. Prose is left alone: this test holds what the bundle tells
// someone to type, not every sentence that mentions the tool.
func unmuteInvocations(content string) []string {
	var out []string
	add := func(line string) {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "unmute "); ok {
			out = append(out, after)
		}
	}
	for _, hit := range inlineCode.FindAllStringSubmatch(content, -1) {
		add(hit[1])
	}
	for _, block := range fence.FindAllStringSubmatch(content, -1) {
		for _, line := range strings.Split(block[1], "\n") {
			add(line)
		}
	}
	return out
}

// splitInvocation reads the leading words off the front and the flags off the
// rest. Which of those words are subcommands and which are arguments is decided
// by the tree, not guessed here.
func splitInvocation(invocation string) (words []string, flags []string) {
	leading := true
	for _, field := range strings.Fields(invocation) {
		switch {
		case strings.HasPrefix(field, "--"):
			leading = false
			flags = append(flags, strings.TrimPrefix(strings.SplitN(field, "=", 2)[0], "--"))
		case leading:
			words = append(words, field)
		}
	}
	return words, flags
}

// longestCommandPath returns the longest prefix of words that resolves to a real
// command. `unmute init my-agent` gives ["init"], and `unmute skill install`
// gives both. An unknown first word gives nothing, which is the failure.
func longestCommandPath(root *cobra.Command, words []string) []string {
	best := []string{}
	for length := 1; length <= len(words); length++ {
		candidate := words[:length]
		cmd, _, err := root.Find(candidate)
		if err != nil || cmd.Name() != candidate[length-1] {
			break
		}
		best = candidate
	}
	return best
}

func flagExists(root *cobra.Command, name string) bool {
	found := false
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		if cmd.Flags().Lookup(name) != nil || cmd.PersistentFlags().Lookup(name) != nil {
			found = true
		}
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root)
	return found
}

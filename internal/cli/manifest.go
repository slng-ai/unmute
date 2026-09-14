package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"unicode"

	"github.com/slng-ai/unmute/internal/manifest"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/spf13/cobra"
)

var manifestStore = manifest.DefaultStore

func newManifestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "manifest", Args: cobra.NoArgs, Short: "Save organization contracts on this computer."}
	cmd.AddCommand(&cobra.Command{Use: "create [name]", Short: "Create a saved manifest in your editor.", Args: cobra.MaximumNArgs(1), RunE: createManifest})
	cmd.AddCommand(&cobra.Command{Use: "use <name>", Short: "Choose the default manifest for new agents.", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		printHeader(cmd.OutOrStdout(), "manifest use")
		store, err := manifestStore()
		if err != nil {
			return err
		}
		if err := store.Use(args[0]); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "default manifest %s\n", args[0])
		return nil
	}})
	return cmd
}

func createManifest(cmd *cobra.Command, args []string) error {
	printHeader(cmd.OutOrStdout(), "manifest create")
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		return fmt.Errorf("set VISUAL or EDITOR to your editor, for example `export EDITOR='code --wait'`, then run `unmute manifest create`")
	}
	words, err := editorWords(editor)
	if err != nil {
		return err
	}
	input := bufio.NewReader(cmd.InOrStdin())
	name := ""
	if len(args) > 0 {
		name = args[0]
	} else {
		fmt.Fprint(cmd.OutOrStdout(), "Manifest name: ")
		line, err := input.ReadString('\n')
		if err != nil && line == "" {
			return fmt.Errorf("read manifest name: %w", err)
		}
		name = strings.TrimSpace(line)
	}
	// Validate names before launching an editor or creating a draft.
	if err := manifest.ValidateName(name); err != nil {
		return err
	}
	store, err := manifestStore()
	if err != nil {
		return err
	}
	// List validates the saved library and avoids opening an editor for an existing name.
	saved, err := store.List()
	if err != nil {
		return err
	}
	for _, item := range saved {
		if item.Name == name {
			return fmt.Errorf("manifest %q already exists; edit %s", name, item.Path)
		}
	}
	previous, err := store.Default()
	if err != nil {
		return err
	}
	draft, err := os.CreateTemp("", "unmute-manifest-*.yaml")
	if err != nil {
		return fmt.Errorf("create manifest draft: %w", err)
	}
	if _, err := draft.WriteString("manifest: " + strconv.Quote(name) + "\nversion: 1\n" + manifestStarter); err != nil {
		_ = draft.Close()
		return fmt.Errorf("write draft %s: %w", draft.Name(), err)
	}
	if err := draft.Close(); err != nil {
		return fmt.Errorf("close draft %s: %w", draft.Name(), err)
	}
	process := exec.Command(words[0], append(words[1:], draft.Name())...)
	// Only a terminal/file belongs to the editor; copying scripted answers
	// into its stdin would consume the later default-selection response.
	if in, ok := cmd.InOrStdin().(*os.File); ok {
		process.Stdin = in
	}
	process.Stdout = cmd.OutOrStdout()
	process.Stderr = cmd.ErrOrStderr()
	if err := process.Run(); err != nil {
		return fmt.Errorf("editor failed; draft kept at %s: %w", draft.Name(), err)
	}
	data, err := os.ReadFile(draft.Name())
	if err != nil {
		return fmt.Errorf("read draft %s: %w", draft.Name(), err)
	}
	if _, err := spec.ParseManifest(data); err != nil {
		return fmt.Errorf("invalid manifest; draft kept at %s: %w", draft.Name(), err)
	}
	path, err := store.Create(name, data)
	if err != nil {
		return fmt.Errorf("draft kept at %s: %w", draft.Name(), err)
	}
	_ = os.Remove(draft.Name())
	fmt.Fprintf(cmd.OutOrStdout(), "created %s\n", path)
	if previous == nil {
		fmt.Fprintf(cmd.OutOrStdout(), "default manifest %s\n", name)
		return nil
	}
	fmt.Fprint(cmd.OutOrStdout(), "Use as the default for new agents? [y/N]: ")
	line, err := input.ReadString('\n')
	if err != nil && line == "" {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(line), "y") || strings.EqualFold(strings.TrimSpace(line), "yes") {
		if err := store.Use(name); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "default manifest %s\n", name)
	}
	return nil
}

// editorWords accepts quoted executable paths and arguments without executing shell code.
func editorWords(value string) ([]string, error) {
	var words []string
	var word strings.Builder
	var quote rune
	escaped, started := false, false
	runes := []rune(value)
	for i, r := range runes {
		if escaped {
			word.WriteRune(r)
			escaped = false
			started = true
			continue
		}
		if r == '\\' && quote != '\'' && (i+1 == len(runes) || runes[i+1] == quote || quote == 0 && (unicode.IsSpace(runes[i+1]) || runes[i+1] == '"' || runes[i+1] == '\'' || runes[i+1] == '\\')) {
			escaped = true
			started = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			started = true
			continue
		}
		if unicode.IsSpace(r) {
			if started {
				words = append(words, word.String())
				word.Reset()
				started = false
			}
			continue
		}
		word.WriteRune(r)
		started = true
	}
	if escaped || quote != 0 {
		return nil, fmt.Errorf("VISUAL/EDITOR has an unfinished quote or escape")
	}
	if started {
		words = append(words, word.String())
	}
	if len(words) == 0 || words[0] == "" {
		return nil, fmt.Errorf("VISUAL/EDITOR must name an executable")
	}
	return words, nil
}

const manifestStarter = `
# This is a company contract revision, not an Unmute or SDK version.
# Omitted rules add no restriction. An empty allow list permits nothing.
# Uncomment and replace examples with your organization's approved values.
# models:
#   listen:
#     - provider: your-provider
#       allow:
#         - your-stt-model
#   speak:
#     - provider: your-provider
#       allow:
#         - your-tts-model
#   think:
#     - provider: your-provider
#       allow:
#         - your-llm-model
# languages:
#   allow:
#     - en
#     - es
# regions:
#   models:
#     - role: listen
#       provider: your-provider
#       allow:
#         - your-region
#   deployments:
#     - provider: livekit
#       allow:
#         - your-region
# targets:
#   allow:
#     - livekit
#     - pipecat
# tools:
#   kinds:
#     allow:
#       - webhook
#       - local
#       - knowledge
#       - builtin
#   names:
#     allow:
#       - check_booking
#       - end_call
#   builtin:
#     allow:
#       - end_call
#   slng:
#     allow:
#       - your-hosted-tool
# tracing:
#   allow:
#     - langfuse
#     - coval
`

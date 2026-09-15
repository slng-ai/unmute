package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"unicode"

	"github.com/slng-ai/unmute/internal/manifest"
	"github.com/slng-ai/unmute/internal/manifestui"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/spf13/cobra"
)

var manifestStore = manifest.DefaultStore

func newManifestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "manifest", Args: cobra.NoArgs, Short: "Save organization contracts on this computer."}
	for _, editing := range []bool{false, true} {
		use, short := "create [name]", "Create a saved manifest with guided setup."
		args := cobra.MaximumNArgs(1)
		if editing {
			use, short, args = "edit <name>", "Edit a saved manifest with guided setup.", cobra.ExactArgs(1)
		}
		var external bool
		child := &cobra.Command{Use: use, Short: short, Args: args, RunE: func(cmd *cobra.Command, args []string) error {
			return runManifest(cmd, args, editing, external)
		}}
		child.Flags().BoolVar(&external, "editor", false, "Edit YAML in VISUAL or EDITOR instead.")
		cmd.AddCommand(child)
	}
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

func runManifest(cmd *cobra.Command, args []string, editing, external bool) error {
	action := "create"
	if editing {
		action = "edit"
	}
	printHeader(cmd.OutOrStdout(), "manifest "+action)
	store, err := manifestStore()
	if err != nil {
		return err
	}
	name := ""
	if len(args) > 0 {
		name = args[0]
	}
	if external {
		return editManifestExternally(cmd, store, name, editing)
	}
	if cmd.InOrStdin() == os.Stdin && (!isTTY(os.Stdin) || !isTTY(cmd.OutOrStdout())) {
		return fmt.Errorf("manifest %s needs an interactive terminal; use --editor to edit YAML", action)
	}
	var initial *spec.Manifest
	var previous *manifest.Saved
	if editing {
		saved, err := store.Load(name)
		if err != nil {
			return fmt.Errorf("%w; use `unmute manifest edit %s --editor` to repair the file", err, name)
		}
		initial = saved.Rules
	} else {
		previous, err = store.Default()
		if err != nil {
			return err
		}
	}
	checkName := func(name string) error {
		path, err := store.Path(name)
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Dir(path)); err == nil {
			return fmt.Errorf("manifest %q already exists; run `unmute manifest edit %s`", name, name)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("check manifest name: %w", err)
		}
		return nil
	}
	if name != "" && !editing {
		if err := checkName(name); err != nil {
			return err
		}
	}
	var path, backup string
	var savedDefault bool
	// Remember a successful create if selecting its default fails, so Retry does
	// not attempt to create the same directory again.
	created := false
	save := func(localName string, rules spec.Manifest, makeDefault bool) error {
		data, err := manifestui.Encode(rules)
		if err != nil {
			return err
		}
		if editing {
			if reflect.DeepEqual(initial, &rules) {
				return nil
			}
			path, backup, err = store.Update(localName, data)
		} else if !created {
			path, err = store.Create(localName, data)
			created = path != ""
		} else {
			path, backup, err = store.Update(localName, data)
		}
		if err != nil {
			return err
		}
		if !editing && (previous == nil || makeDefault) {
			if err := store.Use(localName); err != nil {
				return fmt.Errorf("manifest saved at %s; could not select its default: %w", path, err)
			}
			savedDefault = true
		}
		name = localName
		return nil
	}
	err = manifestui.Run(cmd.InOrStdin(), cmd.OutOrStdout(), cmd.InOrStdin() != os.Stdin || os.Getenv("TERM") == "dumb", manifestui.Options{
		Name: name, Rules: initial, Editing: editing, HasDefault: previous != nil,
		Destination: func(name string) string { return filepath.Join(store.Root, "manifests", name, "manifest") },
		ValidateName: func(name string) error {
			if created {
				return nil
			}
			return checkName(name)
		}, Save: save,
	})
	if path != "" {
		verb := "created"
		if editing {
			verb = "saved"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", verb, path)
	}
	if backup != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "backup %s\n", backup)
	}
	if savedDefault {
		fmt.Fprintf(cmd.OutOrStdout(), "default manifest %s\n", name)
	}
	return err
}

func editManifestExternally(cmd *cobra.Command, store manifest.Store, name string, editing bool) error {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		return fmt.Errorf("set VISUAL or EDITOR, for example `export EDITOR='code --wait'`, or omit --editor for guided setup")
	}
	words, err := editorWords(editor)
	if err != nil {
		return err
	}
	input := bufio.NewReader(cmd.InOrStdin())
	if name == "" {
		fmt.Fprint(cmd.OutOrStdout(), "Manifest name: ")
		line, err := input.ReadString('\n')
		if err != nil && line == "" {
			return fmt.Errorf("read manifest name: %w", err)
		}
		name = strings.TrimSpace(line)
	}
	path, err := store.Path(name)
	if err != nil {
		return err
	}
	var previous *manifest.Saved
	var data []byte
	if editing {
		data, err = os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read manifest: %w", err)
		}
	} else {
		if _, err := os.Stat(filepath.Dir(path)); err == nil {
			return fmt.Errorf("manifest %q already exists; run `unmute manifest edit %s`", name, name)
		} else if !os.IsNotExist(err) {
			return err
		}
		previous, err = store.Default()
		if err != nil {
			return err
		}
		data = []byte("manifest: " + strconv.Quote(name) + "\nversion: 1\n" + manifestStarter)
	}
	draft, err := os.CreateTemp("", "unmute-manifest-*.yaml")
	if err != nil {
		return fmt.Errorf("create manifest draft: %w", err)
	}
	if _, err := draft.Write(data); err != nil {
		_ = draft.Close()
		return fmt.Errorf("write draft %s: %w", draft.Name(), err)
	}
	if err := draft.Close(); err != nil {
		return fmt.Errorf("close draft %s: %w", draft.Name(), err)
	}
	process := exec.Command(words[0], append(words[1:], draft.Name())...)
	if in, ok := cmd.InOrStdin().(*os.File); ok {
		process.Stdin = in
	}
	process.Stdout, process.Stderr = cmd.OutOrStdout(), cmd.ErrOrStderr()
	if err := process.Run(); err != nil {
		return fmt.Errorf("editor failed; draft kept at %s: %w", draft.Name(), err)
	}
	data, err = os.ReadFile(draft.Name())
	if err != nil {
		return fmt.Errorf("read draft %s: %w", draft.Name(), err)
	}
	if _, err := spec.ParseManifest(data); err != nil {
		return fmt.Errorf("invalid manifest; draft kept at %s: %w", draft.Name(), err)
	}
	backup := ""
	if editing {
		path, backup, err = store.Update(name, data)
	} else {
		path, err = store.Create(name, data)
	}
	if err != nil {
		return fmt.Errorf("draft kept at %s: %w", draft.Name(), err)
	}
	_ = os.Remove(draft.Name())
	action := "created"
	if editing {
		action = "saved"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", action, path)
	if backup != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "backup %s\n", backup)
	}
	if editing {
		return nil
	}
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

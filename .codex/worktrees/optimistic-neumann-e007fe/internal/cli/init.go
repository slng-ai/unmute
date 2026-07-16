package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/slng/unmute/internal/ir"
	spec "github.com/slng/unmute/internal/legacyspec"
	"github.com/slng/unmute/internal/scaffold"
	"github.com/slng/unmute/internal/tui"
	"github.com/spf13/cobra"
)

const slngWordmark = "\x1b[38;2;0;0;0;48;2;245;201;110m" +
	"  ____  _     _   _  ____       //  // \n" +
	" / ___|| |   | \\ | |/ ___|     //  //  \n" +
	" \\___ \\| |   |  \\| | |  _     //  //   \n" +
	"  ___) | |___| |\\  | |_| |   //  //    \n" +
	" |____/|_____|_| \\_|\\____|  //  //     \x1b[0m\n"

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init [name]",
		Short: "Scaffold a new agent directory.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				if cmd.InOrStdin() == os.Stdin && (!isCharDevice(os.Stdin) || !isCharDevice(cmd.OutOrStdout())) {
					return fmt.Errorf("agent name required")
				}
				return runInitWizard(cmd)
			}
			dir := args[0]
			return writeScaffold(cmd, dir, scaffold.Data{Name: filepath.Base(dir)})
		},
	}
}

func isCharDevice(value any) bool {
	file, ok := value.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func runInitWizard(cmd *cobra.Command) error {
	in := cmd.InOrStdin()
	errOut := cmd.ErrOrStderr()
	writeHeader(errOut, isCharDevice(errOut))
	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("init menu: %w", err)
	}
	agents, err := discoverLocalAgents(root, errOut)
	if err != nil {
		return err
	}
	result, err := tui.Run(in, cmd.OutOrStdout(), in != os.Stdin, SupportedCompileTargets, agents)
	if err != nil {
		return fmt.Errorf("init menu: %w", err)
	}
	if !result.Confirmed {
		return nil
	}
	if !result.Create {
		if err := saveLocalAgent(result.Original, result.Agent); err != nil {
			return fmt.Errorf("save %s: %w", result.Agent.Data.Name, err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), "updated", result.Agent.Path)
		return nil
	}

	dir := result.Agent.Path
	if err := writeScaffold(cmd, dir, result.Agent.Data); err != nil {
		return err
	}
	if result.Agent.Prompt != "" {
		path := filepath.Join(dir, "agent", "prompt", "identity.md")
		if err := writeFileAtomic(path, []byte(strings.TrimRight(result.Agent.Prompt, "\r\n")+"\n")); err != nil {
			return fmt.Errorf("agent scaffold kept; edit %s manually: %w", path, err)
		}
	}
	for _, target := range result.Targets {
		if err := compileTarget(cmd, dir, target); err != nil {
			return fmt.Errorf("agent scaffold kept; fix it and run %q: %w", "unmute compile "+dir+" "+target, err)
		}
	}
	return nil
}

func writeHeader(out io.Writer, tty bool) {
	if !tty || os.Getenv("NO_COLOR") != "" {
		return
	}
	fmt.Fprint(out, slngWordmark)
}

func writeScaffold(cmd *cobra.Command, dir string, data scaffold.Data) error {
	created, err := scaffold.Write(dir, data)
	if err != nil {
		return fmt.Errorf("init %s: %w", dir, err)
	}
	for _, path := range created {
		fmt.Fprintln(cmd.OutOrStdout(), "created", path)
	}
	return nil
}

func discoverLocalAgents(root string, warnings io.Writer) ([]tui.Agent, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("discover local agents: %w", err)
	}

	var candidates []string
	addCandidate := func(path string) {
		if _, err := os.Stat(filepath.Join(path, "project.yaml")); err == nil {
			candidates = append(candidates, path)
		} else if !os.IsNotExist(err) && warnings != nil {
			fmt.Fprintf(warnings, "warning: skip %s: %v\n", path, err)
		}
	}
	addCandidate(root)
	for _, entry := range entries {
		if entry.IsDir() {
			addCandidate(filepath.Join(root, entry.Name()))
		}
	}
	slices.Sort(candidates)

	agents := make([]tui.Agent, 0, len(candidates))
	for _, path := range candidates {
		agent, err := loadLocalAgent(path)
		if err != nil {
			if warnings != nil {
				fmt.Fprintf(warnings, "warning: skip %s: %v\n", path, err)
			}
			continue
		}
		agents = append(agents, agent)
	}
	return agents, nil
}

func loadLocalAgent(path string) (tui.Agent, error) {
	project, err := spec.LoadProjectConfig(path)
	if err != nil {
		return tui.Agent{}, err
	}
	if strings.TrimSpace(project.Name) == "" {
		return tui.Agent{}, fmt.Errorf("project.yaml: name must not be empty")
	}
	agent, err := spec.LoadAgentConfig(path)
	if err != nil {
		return tui.Agent{}, err
	}
	stt, llm, tts, err := spec.LoadModels(path)
	if err != nil {
		return tui.Agent{}, err
	}
	prompt, err := os.ReadFile(filepath.Join(path, "agent", "prompt", "identity.md"))
	if err != nil {
		return tui.Agent{}, fmt.Errorf("load identity prompt: %w", err)
	}
	return tui.Agent{
		Path: path,
		Data: scaffold.Data{
			Name:     project.Name,
			Greeting: agent.Greeting,
			Language: agent.Language,
			LLMModel: llm.Model,
			STTModel: stt.Model,
			TTSModel: tts.Model,
			TTSVoice: tts.Voice,
		},
		Prompt: strings.TrimRight(string(prompt), "\r\n"),
	}, nil
}

type yamlEdit struct {
	key   string
	value string
}

type pendingWrite struct {
	path string
	data []byte
}

func saveLocalAgent(before, after tui.Agent) error {
	var writes []pendingWrite
	prepare := func(rel string, edits []yamlEdit, out any) error {
		if len(edits) == 0 {
			return nil
		}
		path := filepath.Join(before.Path, rel)
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("edit %s: %w", path, err)
		}
		for _, edit := range edits {
			content, err = replaceYAMLScalar(content, edit.key, edit.value)
			if err != nil {
				return fmt.Errorf("edit %s: %w", path, err)
			}
		}
		if err := yaml.UnmarshalWithOptions(content, out, yaml.Strict()); err != nil {
			return fmt.Errorf("validate %s: %w", path, err)
		}
		writes = append(writes, pendingWrite{path: path, data: content})
		return nil
	}

	var agentEdits []yamlEdit
	if before.Data.Greeting != after.Data.Greeting {
		agentEdits = append(agentEdits, yamlEdit{"greeting", after.Data.Greeting})
	}
	if before.Data.Language != after.Data.Language {
		agentEdits = append(agentEdits, yamlEdit{"language", after.Data.Language})
	}
	if err := prepare(filepath.Join("agent", "agent.yaml"), agentEdits, &ir.AgentConfig{}); err != nil {
		return err
	}

	if before.Data.LLMModel != after.Data.LLMModel {
		if err := prepare(filepath.Join("agent", "models", "llm.yaml"), []yamlEdit{{"model", after.Data.LLMModel}}, &ir.LLMModelConfig{}); err != nil {
			return err
		}
	}
	if before.Data.STTModel != after.Data.STTModel {
		if err := prepare(filepath.Join("agent", "models", "stt.yaml"), []yamlEdit{{"model", after.Data.STTModel}}, &ir.STTModelConfig{}); err != nil {
			return err
		}
	}
	var ttsEdits []yamlEdit
	if before.Data.TTSModel != after.Data.TTSModel {
		ttsEdits = append(ttsEdits, yamlEdit{"model", after.Data.TTSModel})
	}
	if before.Data.TTSVoice != after.Data.TTSVoice {
		ttsEdits = append(ttsEdits, yamlEdit{"voice", after.Data.TTSVoice})
	}
	if err := prepare(filepath.Join("agent", "models", "tts.yaml"), ttsEdits, &ir.TTSModelConfig{}); err != nil {
		return err
	}

	if before.Prompt != after.Prompt {
		path := filepath.Join(before.Path, "agent", "prompt", "identity.md")
		writes = append(writes, pendingWrite{path: path, data: []byte(strings.TrimRight(after.Prompt, "\r\n") + "\n")})
	}
	for _, write := range writes {
		if err := writeFileAtomic(write.path, write.data); err != nil {
			return err
		}
	}
	return nil
}

func replaceYAMLScalar(content []byte, key, value string) ([]byte, error) {
	encoded, err := yaml.Marshal(value)
	if err != nil {
		return nil, err
	}
	lines := bytes.SplitAfter(content, []byte("\n"))
	prefix := []byte(key + ":")
	for index, line := range lines {
		if !bytes.HasPrefix(line, prefix) {
			continue
		}
		ending := []byte(nil)
		if bytes.HasSuffix(line, []byte("\n")) {
			ending = []byte("\n")
		}
		line = append(append(append([]byte{}, prefix...), ' '), bytes.TrimSpace(encoded)...)
		lines[index] = append(line, ending...)
		return bytes.Join(lines, nil), nil
	}
	return nil, fmt.Errorf("top-level key %q not found", key)
}

func writeFileAtomic(path string, content []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("edit %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("edit %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		tmp.Close()
		return fmt.Errorf("edit %s: %w", path, err)
	}
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return fmt.Errorf("edit %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("edit %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("edit %s: %w", path, err)
	}
	return nil
}

package legacyspec

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/slng/unmute/internal/ir"
)

// LoadProjectConfig reads project.yaml.
func LoadProjectConfig(root string) (ir.ProjectConfig, error) {
	var config ir.ProjectConfig
	if err := readYAML(filepath.Join(root, "project.yaml"), &config); err != nil {
		return ir.ProjectConfig{}, err
	}
	return config, nil
}

// LoadEnvSecrets reads env/secrets.yaml using strict YAML decoding.
func LoadEnvSecrets(root string) (ir.EnvSecrets, error) {
	path := filepath.Join(root, "env", "secrets.yaml")
	var secrets ir.EnvSecrets
	if err := readYAML(path, &secrets); err != nil {
		return ir.EnvSecrets{}, err
	}
	if secrets.Local.EnvFile == "" {
		secrets.Local.EnvFile = ".env.local"
	}
	return secrets, nil
}

// LoadPipecatTargetProfile reads targets/pipecat/pipecat.yaml and applies
// stable defaults for fields the author omitted.
func LoadPipecatTargetProfile(root string) (ir.PipecatTargetProfile, error) {
	path := filepath.Join(root, "targets", "pipecat", "pipecat.yaml")
	var profile ir.PipecatTargetProfile
	if err := readYAML(path, &profile); err != nil {
		return ir.PipecatTargetProfile{}, err
	}
	profile.ApplyDefaults(filepath.Base(root))
	return profile, nil
}

// LoadAgentConfig reads agent/agent.yaml.
func LoadAgentConfig(root string) (ir.AgentConfig, error) {
	var config ir.AgentConfig
	if err := readYAML(filepath.Join(root, "agent", "agent.yaml"), &config); err != nil {
		return ir.AgentConfig{}, err
	}
	return config, nil
}

// LoadComplianceConfig reads agent/compliance.yaml.
func LoadComplianceConfig(root string) (ir.ComplianceConfig, error) {
	var config ir.ComplianceConfig
	if err := readYAML(filepath.Join(root, "agent", "compliance.yaml"), &config); err != nil {
		return ir.ComplianceConfig{}, err
	}
	return config, nil
}

// LoadIdleNudgesConfig reads agent/overrides/idle.yaml.
func LoadIdleNudgesConfig(root string) (ir.IdleNudgesConfig, error) {
	var config ir.IdleNudgesConfig
	if err := readYAML(filepath.Join(root, "agent", "overrides", "idle.yaml"), &config); err != nil {
		return ir.IdleNudgesConfig{}, err
	}
	return config, nil
}

// LoadInterruptionConfig reads agent/overrides/interruption.yaml.
func LoadInterruptionConfig(root string) (ir.InterruptionConfig, error) {
	var config ir.InterruptionConfig
	if err := readYAML(filepath.Join(root, "agent", "overrides", "interruption.yaml"), &config); err != nil {
		return ir.InterruptionConfig{}, err
	}
	return config, nil
}

// LoadModels reads the split modality model config files.
func LoadModels(root string) (ir.STTModelConfig, ir.LLMModelConfig, ir.TTSModelConfig, error) {
	var stt ir.STTModelConfig
	if err := readYAML(filepath.Join(root, "agent", "models", "stt.yaml"), &stt); err != nil {
		return ir.STTModelConfig{}, ir.LLMModelConfig{}, ir.TTSModelConfig{}, err
	}
	var llm ir.LLMModelConfig
	if err := readYAML(filepath.Join(root, "agent", "models", "llm.yaml"), &llm); err != nil {
		return ir.STTModelConfig{}, ir.LLMModelConfig{}, ir.TTSModelConfig{}, err
	}
	var tts ir.TTSModelConfig
	if err := readYAML(filepath.Join(root, "agent", "models", "tts.yaml"), &tts); err != nil {
		return ir.STTModelConfig{}, ir.LLMModelConfig{}, ir.TTSModelConfig{}, err
	}
	return stt, llm, tts, nil
}

// LoadVariables reads agent/variables.yaml.
func LoadVariables(root string) (ir.Variables, error) {
	var variables ir.Variables
	if err := readYAML(filepath.Join(root, "agent", "variables.yaml"), &variables); err != nil {
		return ir.Variables{}, err
	}
	return variables, nil
}

// ComposePrompt concatenates prompt fragments in canonical order, then appends
// unknown extra .md files alphabetically.
func ComposePrompt(root string) (string, error) {
	promptDir := filepath.Join(root, "agent", "prompt")
	entries, err := os.ReadDir(promptDir)
	if err != nil {
		return "", fmt.Errorf("%s: %w", promptDir, err)
	}

	files := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		files[strings.TrimSuffix(entry.Name(), ".md")] = true
	}

	var ordered []string
	for _, name := range ir.PromptFragments {
		if files[name] {
			ordered = append(ordered, name)
		}
	}
	var extras []string
	for name := range files {
		if !slices.Contains(ir.PromptFragments, name) {
			extras = append(extras, name)
		}
	}
	slices.Sort(extras)
	ordered = append(ordered, extras...)

	var parts []string
	for _, name := range ordered {
		path := filepath.Join(promptDir, name+".md")
		content, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("%s: %w", path, err)
		}
		parts = append(parts, strings.TrimRight(string(content), "\n"))
	}
	return strings.Join(parts, "\n\n"), nil
}

// LoadTools reads agent/tools/*.yaml in filename order.
func LoadTools(root string) ([]ir.ToolFile, error) {
	toolsDir := filepath.Join(root, "agent", "tools")
	entries, err := os.ReadDir(toolsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", toolsDir, err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		names = append(names, entry.Name())
	}
	slices.Sort(names)

	var tools []ir.ToolFile
	for _, filename := range names {
		path := filepath.Join(toolsDir, filename)
		var decl ir.ToolDeclaration
		if err := readYAML(path, &decl); err != nil {
			return nil, err
		}
		tools = append(tools, ir.ToolFile{
			Name:        strings.TrimSuffix(filename, ".yaml"),
			Declaration: decl,
		})
	}
	return tools, nil
}

func readYAML(path string, out any) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := yaml.UnmarshalWithOptions(content, out, yaml.Strict()); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

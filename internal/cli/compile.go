package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/slng/unmute/internal/generate"
	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/spec"
	"github.com/spf13/cobra"
)

const (
	targetPipecat = "pipecat"
	targetSLNG    = "slng"
)

// SupportedCompileTargets is the target list shared by compile and init.
var SupportedCompileTargets = []string{targetPipecat, targetSLNG}

func newCompileCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "compile <agent-dir> <target>",
		Short: "Compile an agent directory to a target artifact set.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return compileTarget(cmd, args[0], args[1])
		},
	}
}

func compileTarget(cmd *cobra.Command, root, target string) error {
	if !slices.Contains(SupportedCompileTargets, target) {
		return fmt.Errorf("unsupported compile target %q", target)
	}
	switch target {
	case targetPipecat:
		result, outDir, err := compilePipecat(root)
		if err != nil {
			return err
		}
		for _, warning := range result.Warnings {
			fmt.Fprintln(cmd.ErrOrStderr(), "warning:", warning)
		}
		for _, artifact := range result.Artifacts {
			fmt.Fprintln(cmd.OutOrStdout(), "generated", filepath.Join(outDir, artifact.Path))
		}
	case targetSLNG:
		result, outPath, err := compileSLNG(root)
		if err != nil {
			return err
		}
		for _, warning := range result.Warnings {
			fmt.Fprintln(cmd.ErrOrStderr(), "warning:", warning)
		}
		fmt.Fprintln(cmd.OutOrStdout(), "generated", outPath)
	}
	return nil
}

// compilePipecat loads the agent, generates the Pipecat artifact set, and writes
// it to targets/pipecat/generated/. Shared by `compile` and `dev`.
func compilePipecat(root string) (generate.PipecatResult, string, error) {
	input, err := loadPipecatInput(root)
	if err != nil {
		return generate.PipecatResult{}, "", fmt.Errorf("compile %s pipecat: %w", root, err)
	}
	if missing := input.Secrets.MissingRequired(ir.RequiredPipecatSecrets); len(missing) > 0 {
		return generate.PipecatResult{}, "", fmt.Errorf("compile %s pipecat: missing required secrets in env/secrets.yaml: %s", root, strings.Join(missing, ", "))
	}
	result, err := generate.GeneratePipecat(input)
	if err != nil {
		return generate.PipecatResult{}, "", fmt.Errorf("compile %s pipecat: %w", root, err)
	}
	outDir := filepath.Join(root, "targets", "pipecat", "generated")
	if err := writeGenerated(outDir, result.Artifacts); err != nil {
		return generate.PipecatResult{}, "", fmt.Errorf("compile %s pipecat: %w", root, err)
	}
	return result, outDir, nil
}

func compileSLNG(root string) (generate.SLNGResult, string, error) {
	input, err := loadSLNGInput(root)
	if err != nil {
		return generate.SLNGResult{}, "", fmt.Errorf("compile %s slng: %w", root, err)
	}
	result, err := generate.GenerateSLNG(input)
	if err != nil {
		return generate.SLNGResult{}, "", fmt.Errorf("compile %s slng: %w", root, err)
	}
	outPath := filepath.Join(root, "targets", "slng", "generated", result.Filename)
	if err := writeFile(outPath, result.Content); err != nil {
		return generate.SLNGResult{}, "", fmt.Errorf("compile %s slng: %w", root, err)
	}
	return result, outPath, nil
}

func loadPipecatInput(root string) (generate.PipecatInput, error) {
	agent, err := spec.LoadAgentConfig(root)
	if err != nil {
		return generate.PipecatInput{}, err
	}
	stt, llm, tts, err := spec.LoadModels(root)
	if err != nil {
		return generate.PipecatInput{}, err
	}
	secrets, err := spec.LoadEnvSecrets(root)
	if err != nil {
		return generate.PipecatInput{}, err
	}
	profile, err := spec.LoadPipecatTargetProfile(root)
	if err != nil {
		return generate.PipecatInput{}, err
	}
	prompt, err := spec.ComposePrompt(root)
	if err != nil {
		return generate.PipecatInput{}, err
	}
	tools, err := spec.LoadTools(root)
	if err != nil {
		return generate.PipecatInput{}, err
	}
	return generate.PipecatInput{
		AgentName: filepath.Base(root),
		Prompt:    prompt,
		Agent:     agent,
		STT:       stt,
		LLM:       llm,
		TTS:       tts,
		Secrets:   secrets,
		Profile:   profile,
		Tools:     tools,
	}, nil
}

func loadSLNGInput(root string) (generate.SLNGInput, error) {
	project, err := spec.LoadProjectConfig(root)
	if err != nil {
		return generate.SLNGInput{}, err
	}
	agent, err := spec.LoadAgentConfig(root)
	if err != nil {
		return generate.SLNGInput{}, err
	}
	compliance, err := spec.LoadComplianceConfig(root)
	if err != nil {
		return generate.SLNGInput{}, err
	}
	idle, err := spec.LoadIdleNudgesConfig(root)
	if err != nil {
		return generate.SLNGInput{}, err
	}
	interruption, err := spec.LoadInterruptionConfig(root)
	if err != nil {
		return generate.SLNGInput{}, err
	}
	stt, llm, tts, err := spec.LoadModels(root)
	if err != nil {
		return generate.SLNGInput{}, err
	}
	variables, err := spec.LoadVariables(root)
	if err != nil {
		return generate.SLNGInput{}, err
	}
	prompt, err := spec.ComposePrompt(root)
	if err != nil {
		return generate.SLNGInput{}, err
	}
	tools, err := spec.LoadTools(root)
	if err != nil {
		return generate.SLNGInput{}, err
	}
	return generate.SLNGInput{
		Project:      project,
		Prompt:       prompt,
		Agent:        agent,
		Compliance:   compliance,
		Idle:         idle,
		Interruption: interruption,
		STT:          stt,
		LLM:          llm,
		TTS:          tts,
		Variables:    variables,
		Tools:        tools,
	}, nil
}

func writeGenerated(outDir string, artifacts []generate.Artifact) error {
	entries, err := os.ReadDir(outDir)
	if err == nil && len(entries) > 0 {
		hasSentinel := false
		for _, entry := range entries {
			if entry.Name() == generate.PipecatSentinel {
				hasSentinel = true
				break
			}
		}
		if !hasSentinel {
			return fmt.Errorf("%s is non-empty and missing %s sentinel", outDir, generate.PipecatSentinel)
		}
		if err := os.RemoveAll(outDir); err != nil {
			return err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}

	for _, artifact := range artifacts {
		path := filepath.Join(outDir, artifact.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, artifact.Content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func writeFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

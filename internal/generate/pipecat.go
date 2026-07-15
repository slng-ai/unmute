package generate

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"text/template"

	"github.com/slng/unmute/internal/ir"
)

const PipecatSentinel = ".unmute-generated"

//go:embed templates/pipecat/* templates/pipecat/.unmute-generated.tmpl templates/pipecat/k8s/*
var pipecatTemplates embed.FS

type PipecatInput struct {
	AgentName string
	Prompt    string
	Agent     ir.AgentConfig
	STT       ir.STTModelConfig
	LLM       ir.LLMModelConfig
	TTS       ir.TTSModelConfig
	Secrets   ir.EnvSecrets
	Profile   ir.PipecatTargetProfile
	Tools     []ir.ToolFile
}

type Artifact struct {
	Path    string
	Content []byte
}

type PipecatResult struct {
	Artifacts    []Artifact
	Warnings     []string
	OmittedTools []string
}

type compileReport struct {
	Target          string   `json:"target"`
	OutputDir       string   `json:"output_dir"`
	GeneratedFiles  []string `json:"generated_files"`
	RequiredSecrets []string `json:"required_secrets"`
	Warnings        []string `json:"warnings"`
	OmittedTools    []string `json:"omitted_tools"`
}

// GeneratePipecat renders a deterministic Pipecat project artifact set.
func GeneratePipecat(input PipecatInput) (PipecatResult, error) {
	if !strings.HasPrefix(input.LLM.Model, "openai/") {
		return PipecatResult{}, fmt.Errorf("pipecat compile: unsupported llm route %q: only openai/ routes are supported in v1", input.LLM.Model)
	}

	warnings, omittedTools := pipecatWarnings(input.Tools)
	data := struct {
		PipecatInput
		LLMModelName    string
		RequiredSecrets []string
		Warnings        []string
		OmittedTools    []string
	}{
		PipecatInput:    input,
		LLMModelName:    ir.TranslateLLMRoute(input.LLM.Model, "pipecat"),
		RequiredSecrets: ir.RequiredPipecatSecrets,
		Warnings:        warnings,
		OmittedTools:    omittedTools,
	}

	paths := []string{
		PipecatSentinel,
		"Dockerfile",
		"README.md",
		"bot.py",
		"k8s/deployment.yaml",
		"k8s/secret.yaml",
		"pcc-deploy.toml",
		"pyproject.toml",
	}
	var artifacts []Artifact
	for _, path := range paths {
		content, err := renderPipecatTemplate(path, data)
		if err != nil {
			return PipecatResult{}, err
		}
		artifacts = append(artifacts, Artifact{Path: path, Content: content})
	}

	generatedFiles := append([]string(nil), paths...)
	generatedFiles = append(generatedFiles, "compile-report.json")
	slices.Sort(generatedFiles)
	report := compileReport{
		Target:          "pipecat",
		OutputDir:       filepath.ToSlash(filepath.Join("targets", "pipecat", "generated")),
		GeneratedFiles:  generatedFiles,
		RequiredSecrets: append([]string(nil), ir.RequiredPipecatSecrets...),
		Warnings:        warnings,
		OmittedTools:    omittedTools,
	}
	reportJSON, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return PipecatResult{}, err
	}
	reportJSON = append(reportJSON, '\n')
	artifacts = append(artifacts, Artifact{Path: "compile-report.json", Content: reportJSON})
	slices.SortFunc(artifacts, func(a, b Artifact) int {
		return strings.Compare(a.Path, b.Path)
	})

	return PipecatResult{
		Artifacts:    artifacts,
		Warnings:     warnings,
		OmittedTools: omittedTools,
	}, nil
}

func pipecatWarnings(tools []ir.ToolFile) ([]string, []string) {
	var warnings []string
	var omitted []string
	for _, tool := range tools {
		switch tool.Declaration.Handler.Type {
		case "http":
			omitted = append(omitted, tool.Name)
			warnings = append(warnings, fmt.Sprintf("tool %q uses http handler and is omitted: Pipecat HTTP tool invocation is not implemented yet", tool.Name))
		case "mcp":
			omitted = append(omitted, tool.Name)
			warnings = append(warnings, fmt.Sprintf("tool %q uses mcp handler and is omitted: Pipecat MCP tool invocation is not implemented yet", tool.Name))
		case "python":
			warnings = append(warnings, fmt.Sprintf("tool %q uses inline python handler; generated project does not copy handler code yet", tool.Name))
		}
	}
	return warnings, omitted
}

func renderPipecatTemplate(path string, data any) ([]byte, error) {
	templatePath := filepath.ToSlash(filepath.Join("templates", "pipecat", path+".tmpl"))
	content, err := pipecatTemplates.ReadFile(templatePath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", templatePath, err)
	}
	tmpl, err := template.New(path).Parse(string(content))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", templatePath, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("%s: %w", templatePath, err)
	}
	return buf.Bytes(), nil
}

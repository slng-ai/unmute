package generate

import (
	"embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

//go:embed templates/agora_v1/*.tmpl
var agoraTemplates embed.FS

type agoraModel struct {
	Vendor   string `json:"vendor"`
	Model    string `json:"model"`
	Language string `json:"language,omitempty"`
	Voice    string `json:"voice,omitempty"`
}
type agoraSettings struct {
	Name              string     `json:"name"`
	Instructions      string     `json:"instructions"`
	Greeting          string     `json:"greeting"`
	Interruptions     bool       `json:"interruptions"`
	STT               agoraModel `json:"stt"`
	LLM               agoraModel `json:"llm"`
	TTS               agoraModel `json:"tts"`
	MaxSessionSeconds int        `json:"max_session_seconds"`
}

// GenerateAgora emits a standalone project; no provider requests occur here.
func GenerateAgora(agent *ir.Agent, tgt ir.Target) (Artifact, error) {
	if tgt.Provider != ir.ProviderAgora {
		return Artifact{}, fmt.Errorf("agora generator requires provider agora, got %q", tgt.Provider)
	}
	report, err := ir.Validate(agent, []ir.Target{tgt}, target.Default())
	if err != nil {
		return Artifact{}, fmt.Errorf("agora validation: %w (%s)", err, targetDiagnostics(report))
	}
	entry := agent.Agents[agent.EntryAgent]
	listen := tgt.Models.Listen
	reason, speak := tgt.Models.Reason[entry.Model], tgt.Models.Speak[entry.Voice]
	seconds := 600
	if duration := agent.Conversation.MaxDuration; duration != "" {
		parsed, _ := time.ParseDuration(string(duration))
		seconds = int(parsed / time.Second)
	}
	settings := agoraSettings{
		Name: agent.Name, Instructions: entry.Instructions, Greeting: agent.Conversation.Greeting.Text,
		Interruptions: *agent.Conversation.Interruption.Enabled, MaxSessionSeconds: seconds,
		STT: agoraModel{Vendor: listen.Provider, Model: listen.Model, Language: listen.Language},
		LLM: agoraModel{Vendor: reason.Provider, Model: reason.Model},
		TTS: agoraModel{Vendor: speak.Provider, Model: speak.Model, Voice: speak.Voice},
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return Artifact{}, fmt.Errorf("agora settings: %w", err)
	}
	artifact := Artifact{Kind: CodeTarget, Files: []File{{Path: "settings.json", Content: append(data, '\n')}}}
	entries, err := agoraTemplates.ReadDir("templates/agora_v1")
	if err != nil {
		return Artifact{}, fmt.Errorf("agora templates: %w", err)
	}
	for _, entry := range entries {
		content, err := agoraTemplates.ReadFile("templates/agora_v1/" + entry.Name())
		if err != nil {
			return Artifact{}, fmt.Errorf("agora template %s: %w", entry.Name(), err)
		}
		name := strings.TrimSuffix(entry.Name(), ".tmpl")
		switch name {
		case "dockerignore":
			name = ".dockerignore"
		case "env.example":
			name = ".env.example"
		case "requirements.txt":
			content = []byte(strings.ReplaceAll(string(content), "{{AGORA_SDK_VERSION}}", target.AgoraSDKVersion))
		}
		artifact.Files = append(artifact.Files, File{Path: name, Content: content})
	}
	var generated []string
	for _, file := range artifact.Files {
		generated = append(generated, file.Path)
	}
	generated = append(generated, "compile-report.json")
	slices.Sort(generated)
	window := supportedRange(target.Agora)
	compileReport := struct {
		Target      string                `json:"target"`
		Provider    string                `json:"provider"`
		Version     string                `json:"version"`
		Supported   *reportSupported      `json:"supported"`
		EntryAgent  string                `json:"entry_agent"`
		Files       []string              `json:"generated_files"`
		RequiredEnv []string              `json:"required_env"`
		Bindings    []ir.ForwardedBinding `json:"bindings"`
		Notes       []string              `json:"notes"`
	}{tgt.Name, "agora", target.AgoraSDKVersion, window, agent.EntryAgent, generated,
		[]string{"AGORA_APP_ID", "AGORA_APP_CERTIFICATE"}, report.ForwardedBindings,
		[]string{"Local browser demonstration; hosted account/model availability and a real voice call remain to be verified.", "Built-in dev and deploy are unsupported; follow README.md to launch server.py."}}
	data, err = json.MarshalIndent(compileReport, "", "  ")
	if err != nil {
		return Artifact{}, fmt.Errorf("agora compile report: %w", err)
	}
	artifact.Files = append(artifact.Files, File{Path: "compile-report.json", Content: append(data, '\n')})
	artifact.Notes.ForwardedBindings = report.ForwardedBindings
	return artifact, nil
}

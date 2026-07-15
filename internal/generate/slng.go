package generate

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/slng/unmute/internal/ir"
)

var slngToolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// SLNGInput is the current Unmute agent directory loaded for the SLNG target.
type SLNGInput struct {
	Project      ir.ProjectConfig
	Prompt       string
	Agent        ir.AgentConfig
	Compliance   ir.ComplianceConfig
	Idle         ir.IdleNudgesConfig
	Interruption ir.InterruptionConfig
	STT          ir.STTModelConfig
	LLM          ir.LLMModelConfig
	TTS          ir.TTSModelConfig
	Variables    ir.Variables
	Tools        []ir.ToolFile
}

// SLNGResult is the generated Voice Agents API payload JSON and warnings.
type SLNGResult struct {
	Filename     string
	Content      []byte
	Warnings     []string
	OmittedTools []string
}

type slngCreateAgentPayload struct {
	Name                string                `json:"name"`
	SystemPrompt        string                `json:"system_prompt"`
	Greeting            string                `json:"greeting"`
	Language            string                `json:"language,omitempty"`
	Region              string                `json:"region"`
	EnableInterruptions bool                  `json:"enable_interruptions"`
	Models              ir.VoiceAgentsModels  `json:"models"`
	IdleNudges          ir.IdleNudgesConfig   `json:"idle_nudges"`
	Tools               []slngWebhookTool     `json:"tools,omitempty"`
	RuntimeVariables    []slngRuntimeVariable `json:"runtime_variables,omitempty"`
	TemplateDefaults    map[string]string     `json:"template_defaults,omitempty"`
}

type slngRuntimeVariable struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type slngWebhookTool struct {
	Type             string         `json:"type"`
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Description      string         `json:"description"`
	URL              string         `json:"url"`
	Parameters       map[string]any `json:"parameters"`
	HTTPMethod       string         `json:"http_method"`
	WebhookFormat    string         `json:"webhook_format"`
	TimeoutSeconds   float64        `json:"timeout_seconds"`
	WaitForResponse  bool           `json:"wait_for_response"`
	ShowResultsToLLM bool           `json:"show_results_to_llm"`
	Source           string         `json:"source"`
}

// GenerateSLNG renders the SLNG POST /v1/agents create-agent JSON body.
func GenerateSLNG(input SLNGInput) (SLNGResult, error) {
	filename, err := slngOutputFilename(input.Project.Name)
	if err != nil {
		return SLNGResult{}, err
	}

	tools, warnings, omitted, err := slngTools(input.Tools)
	if err != nil {
		return SLNGResult{}, err
	}
	enableInterruptions := input.Interruption.Enabled
	payload := slngCreateAgentPayload{
		Name:                input.Project.Name,
		SystemPrompt:        input.Prompt,
		Greeting:            input.Agent.Greeting,
		Language:            input.Agent.Language,
		Region:              input.Compliance.Region,
		EnableInterruptions: enableInterruptions,
		Models:              ir.ToVoiceAgentsModels(input.STT, ir.TranslateLLMConfig(input.LLM, "slng"), input.TTS),
		IdleNudges:          input.Idle,
		Tools:               tools,
		RuntimeVariables:    slngRuntimeVariables(input.Variables),
		TemplateDefaults:    slngTemplateDefaults(input.Variables),
	}

	content, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return SLNGResult{}, err
	}
	content = append(content, '\n')
	return SLNGResult{
		Filename:     filename,
		Content:      content,
		Warnings:     warnings,
		OmittedTools: omitted,
	}, nil
}

func slngOutputFilename(projectName string) (string, error) {
	if strings.TrimSpace(projectName) == "" {
		return "", fmt.Errorf("project.yaml name is required for slng output")
	}
	if strings.ContainsAny(projectName, `/\`) {
		return "", fmt.Errorf("project.yaml name %q must not contain path separators", projectName)
	}
	return projectName + ".json", nil
}

func slngRuntimeVariables(variables ir.Variables) []slngRuntimeVariable {
	// ponytail: SLNG rejects a name appearing in both runtime_variables and
	// template_defaults ("personalization variables"). Variables with a
	// default are emitted only as template_defaults; runtime_variables holds
	// the rest.
	names := make([]string, 0, len(variables.User))
	for name, v := range variables.User {
		if v.Default != "" {
			continue
		}
		names = append(names, name)
	}
	slices.Sort(names)

	rows := make([]slngRuntimeVariable, 0, len(names))
	for _, name := range names {
		rows = append(rows, slngRuntimeVariable{
			Name:        name,
			Description: variables.User[name].Description,
		})
	}
	return rows
}

func slngTemplateDefaults(variables ir.Variables) map[string]string {
	defaults := make(map[string]string)
	for name, variable := range variables.User {
		if variable.Default != "" {
			defaults[name] = variable.Default
		}
	}
	if len(defaults) == 0 {
		return nil
	}
	return defaults
}

func slngTools(toolFiles []ir.ToolFile) ([]slngWebhookTool, []string, []string, error) {
	var tools []slngWebhookTool
	var warnings []string
	var omitted []string
	for _, toolFile := range toolFiles {
		tool, warning, ok, err := slngTool(toolFile)
		if err != nil {
			return nil, nil, nil, err
		}
		if warning != "" {
			warnings = append(warnings, warning)
		}
		if !ok {
			omitted = append(omitted, toolFile.Name)
			continue
		}
		tools = append(tools, tool)
	}
	return tools, warnings, omitted, nil
}

func slngTool(toolFile ir.ToolFile) (slngWebhookTool, string, bool, error) {
	handler := toolFile.Declaration.Handler
	if handler.Type != "http" {
		return slngWebhookTool{}, fmt.Sprintf("tool %q uses %s handler and is omitted: SLNG compile v1 only maps absolute HTTP(S) webhook tools", toolFile.Name, handler.Type), false, nil
	}
	if !strings.HasPrefix(handler.Ref, "http://") && !strings.HasPrefix(handler.Ref, "https://") {
		return slngWebhookTool{}, fmt.Sprintf("tool %q uses http handler ref %q and is omitted: SLNG compile v1 requires an absolute HTTP(S) URL", toolFile.Name, handler.Ref), false, nil
	}
	if err := validateSLNGToolName(toolFile.Name); err != nil {
		return slngWebhookTool{}, "", false, fmt.Errorf("tool %q: %w", toolFile.Name, err)
	}
	if containsTemplateSyntax(toolFile.Declaration.Description) {
		return slngWebhookTool{}, "", false, fmt.Errorf("tool %q: personalization is not supported in tool field 'description'", toolFile.Name)
	}
	if err := validateSLNGWebhookURL(handler.Ref); err != nil {
		return slngWebhookTool{}, "", false, fmt.Errorf("tool %q url: %w", toolFile.Name, err)
	}
	parameters, err := normalizeSLNGParameters(toolFile.Declaration.Parameters)
	if err != nil {
		return slngWebhookTool{}, "", false, fmt.Errorf("tool %q parameters: %w", toolFile.Name, err)
	}

	return slngWebhookTool{
		Type:             "webhook",
		ID:               deterministicToolID(toolFile.Name),
		Name:             toolFile.Name,
		Description:      toolFile.Declaration.Description,
		URL:              handler.Ref,
		Parameters:       parameters,
		HTTPMethod:       "POST",
		WebhookFormat:    "envelope",
		TimeoutSeconds:   10,
		WaitForResponse:  true,
		ShowResultsToLLM: true,
		Source:           "contextual",
	}, "", true, nil
}

func validateSLNGToolName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("tool name is required")
	}
	if !slngToolNamePattern.MatchString(name) {
		return fmt.Errorf("tool name must use only letters, numbers, underscores, or dashes")
	}
	return nil
}

func validateSLNGWebhookURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("URL must start with http:// or https://")
	}
	if parsed.Host == "" {
		return fmt.Errorf("URL must include a host")
	}
	if parsed.User != nil {
		return fmt.Errorf("URL must not include user info")
	}
	if parsed.Fragment != "" {
		return fmt.Errorf("URL fragments are not supported")
	}
	if containsTemplateSyntax(parsed.Scheme) || containsTemplateSyntax(parsed.Host) {
		return fmt.Errorf("URL personalization is only supported in path segments and query parameter values")
	}
	for _, rawPair := range strings.Split(parsed.RawQuery, "&") {
		if rawPair == "" {
			continue
		}
		key, _, _ := strings.Cut(rawPair, "=")
		if containsTemplateSyntax(key) {
			return fmt.Errorf("URL personalization is not supported in query parameter names")
		}
	}
	return nil
}

func normalizeSLNGParameters(parameters map[string]any) (map[string]any, error) {
	if len(parameters) == 0 {
		return map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}, nil
	}

	schema := make(map[string]any, len(parameters)+2)
	for key, value := range parameters {
		schema[key] = value
	}
	schemaType, ok := schema["type"]
	if !ok || schemaType == nil || schemaType == "" {
		schema["type"] = "object"
	} else if schemaType != "object" {
		return nil, fmt.Errorf("parameters.type must be 'object'")
	}

	properties, ok := schema["properties"]
	if !ok || properties == nil {
		schema["properties"] = map[string]any{}
	} else if _, ok := properties.(map[string]any); !ok {
		return nil, fmt.Errorf("parameters.properties must be a JSON object")
	}

	required, ok := schema["required"]
	if ok && required != nil {
		requiredList, err := stringList(required)
		if err != nil {
			return nil, fmt.Errorf("parameters.required must be a list of strings")
		}
		schema["required"] = requiredList
		propertiesMap := schema["properties"].(map[string]any)
		var missing []string
		for _, name := range requiredList {
			if _, ok := propertiesMap[name]; !ok {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			return nil, fmt.Errorf("parameters.required contains keys not present in parameters.properties: %s", strings.Join(missing, ", "))
		}
	}
	return schema, nil
}

func stringList(value any) ([]string, error) {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...), nil
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("not a string")
			}
			out = append(out, text)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("not a list")
	}
}

func containsTemplateSyntax(value string) bool {
	return strings.Contains(value, "{{") || strings.Contains(value, "}}")
}

func deterministicToolID(name string) string {
	namespace := [16]byte{0x6f, 0x96, 0x19, 0xff, 0x8b, 0x86, 0xd0, 0x11, 0xb4, 0x2d, 0x00, 0xcf, 0x4f, 0xc9, 0x64, 0xff}
	h := sha1.New()
	h.Write(namespace[:])
	h.Write([]byte(name))
	sum := h.Sum(nil)
	uuid := append([]byte(nil), sum[:16]...)
	uuid[6] = (uuid[6] & 0x0f) | 0x50
	uuid[8] = (uuid[8] & 0x3f) | 0x80
	hexText := hex.EncodeToString(uuid)
	return hexText[0:8] + "-" + hexText[8:12] + "-" + hexText[12:16] + "-" + hexText[16:20] + "-" + hexText[20:32]
}

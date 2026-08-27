package generate

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"text/template"

	"github.com/slng-ai/unmute/internal/ir"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// The SLNG *target* driver. Next door, slng_router.go is the SLNG *model
// vendor*: the Context Router a package binds a think model to, on any target.
// The word does three jobs in this repository and this file is only the third
// one, which is why every message it produces opens with "slng target".
//
// Unlike the other two drivers this one emits no runnable project. It writes a
// deployment body for a platform that runs the agent, one tool body per tool
// that needs one, and a runbook. Nothing here opens a socket: unmute writes
// files and the voiceai CLI pushes them, which is the boundary the whole design
// rests on and internal/ir's TestNoSlngFileOpensASocket is the gate under it.
//
//go:embed templates/slng_v1/*.tmpl
var slngV1Templates embed.FS

// GenerateSlng writes build/<target>/: agent.json, tools/<name>.json for every
// tool that needs a body, and README.md.
func GenerateSlng(agent *ir.Agent, tgt ir.Target) (Artifact, error) {
	built, err := buildSlng(agent, tgt)
	if err != nil {
		return Artifact{}, err
	}
	body, err := marshalSlng(built.Body)
	if err != nil {
		return Artifact{}, fmt.Errorf("agent body: %w", err)
	}
	files := []File{{Path: "agent.json", Content: body}}
	for _, tool := range built.ToolFiles {
		content, err := marshalSlng(tool.Body)
		if err != nil {
			return Artifact{}, fmt.Errorf("tool %q body: %w", tool.Name, err)
		}
		files = append(files, File{Path: "tools/" + tool.Name + ".json", Content: content})
	}
	runbook, err := renderSlngRunbook(built.Runbook)
	if err != nil {
		return Artifact{}, err
	}
	files = append(files, File{Path: "README.md", Content: runbook})
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return Artifact{
		Kind:  BodyTarget,
		Files: files,
		Notes: GenerateReport{Notes: built.Notes, Warnings: built.Warnings},
	}, nil
}

// marshalSlng writes one JSON document. Indented and newline-terminated because
// a person reads these before pushing them, and HTML escaping off because a
// prompt containing `&` or `<` is prose, not markup, and & in a system
// prompt is a change to what the agent was told.
func marshalSlng(value any) ([]byte, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// marshalSortedMap encodes a map with its keys in sorted order. encoding/json
// already sorts map keys, so this exists for the other half: a nil map of a
// named type encodes as {} rather than null, which matters because SLNG reads
// the declared variable set from the union of two maps' keys and null there
// says something different from empty.
func marshalSortedMap[V any](values map[string]V) ([]byte, error) {
	if values == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(map[string]V(values))
}

func renderSlngRunbook(data slngRunbook) ([]byte, error) {
	tmpl, err := template.New("README.md.tmpl").ParseFS(slngV1Templates, "templates/slng_v1/README.md.tmpl")
	if err != nil {
		return nil, fmt.Errorf("slng runbook template: %w", err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return nil, fmt.Errorf("slng runbook: %w", err)
	}
	return out.Bytes(), nil
}

// --- tool bodies -----------------------------------------------------------

// The two tool types this driver emits. Each name is now written twice per body
// — as `tool_type` and as the `config` union tag — so it gets a constant rather
// than two literals that agree by hand.
const (
	slngToolTypeCode = "code"
	slngToolTypeAPI  = "api_request"
)

type slngToolFile struct {
	Name string
	Body any
}

type slngCodeTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	ToolType    string         `json:"tool_type"`
	Config      slngCodeConfig `json:"config"`
	CodeSrc     string         `json:"code_src"`
	Secrets     []string       `json:"declared_secrets"`
	// Dependencies are the handler's exact pins, canonical and sorted the way the
	// server stores them. Written even when empty, because an absent list and an
	// empty one say the same thing and the empty one says it out loud.
	Dependencies []string `json:"dependencies"`
}

// slngCodeConfig and slngAPIConfig both open with the discriminator, because
// `config` is a tagged union on the wire and the tag is the tool's own type.
//
// A create body gets away without it: `tool_type` sits beside `config` and the
// API infers the tag from it. An *update* does not. The push step PATCHes a tool
// that already exists and strips `tool_type` first, because a tool's type cannot
// change after it is created — at which point an untagged `config` has nothing
// left to infer from and the request fails:
//
//	422 config: Input tag '...' found using 'type' does not match any of the
//	expected tags: 'code', 'api_request', 'end_call', 'voicemail_detection',
//	'transfer_call', 'send_sms', 'current_datetime', 'user_phone_number'
//
// Measured against api.agents.slng.ai on 2026-08-27: the same PATCH is 422
// without the tag and 200 with it, and SLNG stores the tag on read-back. So an
// untagged body is not the body that exists once it lands, which is reason
// enough on its own. The first deploy of a package succeeded and every deploy
// after it failed, which is the worst shape a bug like this can have.
type slngCodeConfig struct {
	Type         string         `json:"type"`
	ImportProbes []string       `json:"import_probes"`
	Egress       map[string]any `json:"egress"`
}

type slngAPITool struct {
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	ToolType    string        `json:"tool_type"`
	Config      slngAPIConfig `json:"config"`
}

type slngAPIConfig struct {
	Type       string         `json:"type"`
	URL        string         `json:"url"`
	HTTPMethod string         `json:"http_method"`
	Auth       slngAPIAuth    `json:"auth"`
	Headers    []any          `json:"headers"`
	Parameters map[string]any `json:"parameters,omitempty"`
	Timeout    float64        `json:"timeout_seconds"`
	WaitFor    bool           `json:"wait_for_response"`
}

type slngAPIAuth struct {
	Type string `json:"type"`
	// SecretName is a Vault name. Never a value: unmute has none and would refuse
	// to write one.
	SecretName string `json:"secret_name,omitempty"`
}

// slngToolBody returns the file to write for one tool, and the per-agent config
// override its reference carries, if any.
//
// A builtin needs neither: it names a curated capability the platform already
// owns. An MCP tool never reaches here.
func slngToolBody(name string, tool ir.Tool) (*slngToolFile, map[string]any, error) {
	switch tool.Execution {
	case ir.ToolLocal:
		source, err := slngCodeSource(name, tool)
		if err != nil {
			return nil, nil, err
		}
		// Canonical and sorted, because the server canonicalises and sorts whatever
		// it is given and stores the result. A body that emits an uncanonical pin
		// is not the body that exists once it lands, and every digest comparison
		// against it drifts. ir.Validate has already refused a pin that cannot be
		// canonicalised, so this cannot fail here; it is checked anyway rather than
		// discarded, because "cannot fail" is a claim with a shelf life.
		pins, err := targetcap.CanonicalSlngPins(tool.Dependencies)
		if err != nil {
			return nil, nil, fmt.Errorf("tool %q dependencies: %w", name, err)
		}
		return &slngToolFile{Name: name, Body: slngCodeTool{
			Name:        name,
			Description: tool.Description,
			ToolType:    slngToolTypeCode,
			Config: slngCodeConfig{
				Type:         slngToolTypeCode,
				ImportProbes: []string{},
				// egress is documented as historical compatibility only and validates
				// nothing: custom code has no internet access at all. Writing an empty
				// object says "nothing configured" rather than implying a knob exists.
				Egress: map[string]any{},
			},
			CodeSrc:      source,
			Secrets:      slngToolSecrets(tool),
			Dependencies: pins,
		}}, nil, nil // a code tool never carries config_overrides: there is no code member in the union
	case ir.ToolWebhook:
		config, override := slngAPIConfigFor(tool)
		return &slngToolFile{Name: name, Body: slngAPITool{
			Name:        name,
			Description: tool.Description,
			ToolType:    slngToolTypeAPI,
			Config:      config,
		}}, override, nil
	default:
		// Builtin, client and provider_hosted. The last two are refused by the
		// capability table before this runs; a builtin needs no body.
		return nil, nil, nil
	}
}

func slngToolSecrets(tool ir.Tool) []string {
	secrets := []string{}
	if tool.Auth != nil && tool.Auth.TokenEnv != "" {
		secrets = append(secrets, tool.Auth.TokenEnv)
	}
	return secrets
}

// slngAPIConfigFor builds the api_request config and, separately, the per-agent
// override the reference carries.
//
// The split is the most expensive fact in this driver. validate_api_request_url
// requires a literal https host, and ApiRequestConfig.validate_request adds a
// tool-level-only rule that every token in the URL must start with `$`, so only
// a Vault variable may be templated there. ApiRequestOverrides calls the URL
// validator alone, without that loop, so an input token like {{customer_id}} is
// legal in a per-agent override and illegal in the tool body. Getting it
// backwards is a 422 with a confusing message.
func slngAPIConfigFor(tool ir.Tool) (slngAPIConfig, map[string]any) {
	config := slngAPIConfig{
		Type:       slngToolTypeAPI,
		URL:        slngWebhookURL(tool, false),
		HTTPMethod: "POST",
		Auth:       slngAuthFor(tool),
		Headers:    []any{},
		Parameters: tool.Input,
		Timeout:    10,
		WaitFor:    tool.Effect != ir.ToolEndsConversation,
	}
	templated := slngWebhookURL(tool, true)
	if templated == config.URL {
		return config, nil
	}
	return config, map[string]any{"type": "api_request", "url": templated}
}

// slngWebhookURL joins the literal base with the tool's path. withTokens keeps
// the path's {{variable}} tokens; without it, a path carrying one is dropped
// back to the base, because the token cannot live in the tool's own config.
func slngWebhookURL(tool ir.Tool, withTokens bool) string {
	base := strings.TrimSuffix(tool.BaseURL, "/")
	if tool.Path == "" {
		return base
	}
	path := tool.Path
	if !withTokens && ir.HasTemplate(path) {
		return base
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

// slngAuthFor maps the package's webhook auth to SLNG's. A package names an
// environment variable; SLNG reads a Vault name. The two are the same name, and
// neither is a value.
//
// The token never becomes an Authorization header: `authorization` is a
// protected header name SLNG rejects outright, with "auth, signature, and
// hop-by-hop headers are managed by the executor".
func slngAuthFor(tool ir.Tool) slngAPIAuth {
	if tool.Auth == nil || tool.Auth.TokenEnv == "" {
		return slngAPIAuth{Type: "none"}
	}
	return slngAPIAuth{Type: "bearer", SecretName: tool.Auth.TokenEnv}
}

// slngCodeSource assembles code_src in the order the contract fixes: the
// author's own handler file text unchanged, then a generated Input, a generated
// Output, and a short entry point calling the author's function by name.
//
// The author's text is unchanged on purpose. That is the rule that keeps one
// handler file working on all three targets: LiveKit and Pipecat import the file
// and call the function, and SLNG introspects the classes around it.
func slngCodeSource(name string, tool ir.Tool) (string, error) {
	var out strings.Builder
	out.WriteString(strings.TrimRight(tool.HandlerSource, "\n"))
	out.WriteString("\n\n\n")
	out.WriteString("# Generated by unmute. SLNG derives this tool's parameter and result\n")
	out.WriteString("# schemas by introspecting Input and Output, and calls handler().\n")
	out.WriteString("# Everything above this line is your own handler file, unchanged.\n")
	out.WriteString("from pydantic import BaseModel\n\n\n")
	out.WriteString(slngModelClass("Input", tool.Input))
	out.WriteString("\n\n")
	out.WriteString(slngModelClass("Output", tool.Output))
	out.WriteString("\n\n")
	out.WriteString("def handler(input: Input) -> Output:\n")
	arguments := slngSchemaFields(tool.Input)
	if len(arguments) == 0 {
		out.WriteString("    result = " + name + "()\n")
	} else {
		call := make([]string, 0, len(arguments))
		for _, field := range arguments {
			call = append(call, field+"=input."+field)
		}
		out.WriteString("    result = " + name + "(" + strings.Join(call, ", ") + ")\n")
	}
	if len(slngSchemaFields(tool.Output)) == 0 {
		out.WriteString("    return Output()\n")
		return out.String(), nil
	}
	out.WriteString("    return Output(**result)\n")
	return out.String(), nil
}

// slngModelClass renders one pydantic model from a JSON Schema object. Only the
// top level is modelled: a nested object becomes a dict, which is what the
// starter code does and what keeps this from growing into a code generator.
func slngModelClass(class string, schema map[string]any) string {
	fields := slngSchemaFields(schema)
	if len(fields) == 0 {
		return "class " + class + "(BaseModel):\n    pass\n"
	}
	required := map[string]bool{}
	if list, ok := schema["required"].([]any); ok {
		for _, name := range list {
			if text, ok := name.(string); ok {
				required[text] = true
			}
		}
	}
	properties, _ := schema["properties"].(map[string]any)
	var out strings.Builder
	out.WriteString("class " + class + "(BaseModel):\n")
	for _, field := range fields {
		annotation := slngAnnotation(properties[field])
		if required[field] {
			out.WriteString("    " + field + ": " + annotation + "\n")
			continue
		}
		out.WriteString("    " + field + ": " + annotation + " | None = None\n")
	}
	return out.String()
}

// slngSchemaFields lists a schema's property names in sorted order, so the same
// package always emits the same source.
func slngSchemaFields(schema map[string]any) []string {
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return nil
	}
	return slices.Sorted(maps.Keys(properties))
}

// slngAnnotation maps one property's declared type to a Python annotation.
// Anything that is not a named primitive becomes a permissive annotation rather
// than a guess: SLNG introspects these, so a wrong type is a wrong schema.
func slngAnnotation(property any) string {
	shape, ok := property.(map[string]any)
	if !ok {
		return "object"
	}
	switch declared := shape["type"].(type) {
	case string:
		switch declared {
		case "array":
			return "list"
		case "object":
			return "dict"
		default:
			return pyTypeForJSON(declared)
		}
	default:
		return "object"
	}
}

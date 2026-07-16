// Package scaffold renders the files `unmute init` writes: a v1 agent package
// (agent.yaml + instructions.md + targets.yaml). Templates live in templates/
// and are embedded; each runs through text/template with [[ ]] delimiters, so
// the voice-agent {{ }} variable syntax in prompts passes through verbatim.
package scaffold

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/goccy/go-yaml"
	targetcap "github.com/slng/unmute/internal/target"
)

//go:embed templates
var templates embed.FS

// ErrExists is returned when the target directory already exists and is not
// empty. init refuses rather than overwrite an existing agent.
var ErrExists = errors.New("directory already exists and is not empty")

const (
	DefaultGreeting     = "Hi, thanks for calling. How can I help you today?"
	DefaultInstructions = "You are a helpful voice assistant. This is a phone call, so keep every answer to one or two short sentences."
	DefaultTarget       = "pipecat"
	DefaultLanguage     = "en"
	DefaultChannel      = "web"
)

// Data is the v1 agent configuration rendered by the scaffold templates.
type Data struct {
	Name         string
	Target       string
	Language     string
	Channel      string
	Greeting     string
	Instructions string
	Listen       Binding
	Reason       Binding
	Speak        Binding
	Variables    []Variable
	Tools        []Tool
}

// Binding is one concrete role choice collected by the wizard. Params is an
// optional JSON object; JSON is valid YAML, so templates can forward it intact.
type Binding struct {
	Provider string
	Model    string
	Voice    string
	Params   string
}

type Variable struct {
	Name    string
	Type    string
	Default string // optional JSON primitive, rendered verbatim
	Source  string
}

type Tool struct {
	Name        string
	Description string
	URLEnv      string
	Input       string // JSON Schema object
	Output      string // optional JSON Schema object
}

// SetTarget selects an orchestrator and resets its target-dependent defaults.
func (d *Data) SetTarget(provider string) {
	d.Target = provider
	switch provider {
	case "elevenlabs":
		d.Listen = Binding{}
		d.Reason = Binding{Model: "gemini-2.5-flash"}
		d.Speak = Binding{Provider: "elevenlabs", Voice: "cgSgspJ2msm6clMCkdW9"}
	default: // Pipecat and LiveKit share the safe SLNG/OpenAI starter.
		d.Listen = Binding{Provider: "slng", Model: "slng/deepgram/nova:3-en"}
		d.Reason = Binding{Provider: "openai", Model: "gpt-4.1-mini"}
		d.Speak = Binding{Provider: "slng", Model: "slng/deepgram/aura:2-en", Voice: "aura-2-thalia-en"}
	}
}

func (d Data) withDefaults() Data {
	if d.Target == "" {
		d.SetTarget(DefaultTarget)
	} else if d.Listen == (Binding{}) && d.Reason == (Binding{}) && d.Speak == (Binding{}) {
		d.SetTarget(d.Target)
	}
	if d.Greeting == "" {
		d.Greeting = DefaultGreeting
	}
	if d.Instructions == "" {
		d.Instructions = DefaultInstructions
	}
	if d.Language == "" {
		d.Language = DefaultLanguage
	}
	if d.Channel == "" {
		d.Channel = DefaultChannel
	}
	return d
}

// RequiredEnv returns the starter env names implied by the selected target
// and catalogue entries. Values are never rendered.
func (d Data) RequiredEnv() []string {
	set := map[string]bool{}
	framework := targetcap.Provider(d.Target)
	switch framework {
	case targetcap.LiveKit:
		for _, name := range []string{"LIVEKIT_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET"} {
			set[name] = true
		}
	case targetcap.ElevenLabs:
		set["ELEVENLABS_API_KEY"] = true
	}
	for role, binding := range map[targetcap.Role]Binding{
		targetcap.Listen: d.Listen,
		targetcap.Reason: d.Reason,
		targetcap.Speak:  d.Speak,
	} {
		entry, ok := targetcap.DefaultCatalog().Lookup(framework, role, binding.Provider)
		if !ok || entry.Call == nil || entry.Call.APIKeyArg == "" {
			continue
		}
		name := entry.Call.APIKeyEnv
		if name == "" && binding.Provider != "" {
			name = strings.ToUpper(strings.ReplaceAll(binding.Provider, "-", "_")) + "_API_KEY"
		}
		if name != "" {
			set[name] = true
		}
	}
	for _, tool := range d.Tools {
		if tool.URLEnv != "" {
			set[tool.URLEnv] = true
		}
	}
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Write renders every embedded template into dir/. Returns the created file
// paths in deterministic (lexical) order. Refuses if dir already exists and is
// non-empty (no overwrite, no partial write).
func Write(dir string, d Data) ([]string, error) {
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return nil, fmt.Errorf("%s: %w", dir, ErrExists)
	}

	var created []string
	d = d.withDefaults()

	// WalkDir visits in lexical order → deterministic output.
	err := fs.WalkDir(templates, "templates", func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		rel := strings.TrimSuffix(strings.TrimPrefix(p, "templates/"), ".tmpl")
		if rel == "tool.yaml" {
			return nil // rendered once per Data.Tool below
		}
		if rel == "env.example" {
			rel = ".env.example" // dotfiles can't be embedded templates
		}
		out := filepath.Join(dir, rel)

		raw, err := templates.ReadFile(p)
		if err != nil {
			return err
		}
		tmpl, err := parseTemplate(rel, raw)
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, d); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
			return err
		}
		created = append(created, out)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scaffold: %w", err)
	}
	raw, err := templates.ReadFile("templates/tool.yaml.tmpl")
	if err != nil {
		return nil, fmt.Errorf("scaffold: %w", err)
	}
	tmpl, err := parseTemplate("tool.yaml", raw)
	if err != nil {
		return nil, fmt.Errorf("scaffold: %w", err)
	}
	tools := append([]Tool(nil), d.Tools...)
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	for _, tool := range tools {
		out := filepath.Join(dir, "tools", tool.Name+".yaml")
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, tool); err != nil {
			return nil, fmt.Errorf("scaffold: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return nil, fmt.Errorf("scaffold: %w", err)
		}
		if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
			return nil, fmt.Errorf("scaffold: %w", err)
		}
		created = append(created, out)
	}
	return created, nil
}

func parseTemplate(name string, raw []byte) (*template.Template, error) {
	return template.New(name).Funcs(template.FuncMap{
		"quote": strconv.Quote,
		"yaml":  yamlScalar,
	}).Delims("[[", "]]").Parse(string(raw))
}

func yamlScalar(value string) (string, error) {
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte("value: "+value+"\n"), &parsed); err == nil && parsed["value"] == value {
		return value, nil
	}
	encoded, err := yaml.Marshal(value)
	return strings.TrimSpace(string(encoded)), err
}

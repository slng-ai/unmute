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
	EntryAgent   string
	Greeting     string
	Instructions string
	Listen       Binding
	Reason       Binding
	Speak        Binding
	Variables    []Variable
	Tools        []Tool
	Agents       []Agent
	Handoffs     []Handoff
	Tasks        []Task
	TaskGroups   []TaskGroup
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
	AttachTo    []string
	AttachTasks []string
}

type Agent struct {
	Name         string
	Instructions string
	Reason       Binding
	Speak        Binding
}

func (a Agent) ModelProfile() string { return a.Name + "_model" }
func (a Agent) VoiceProfile() string { return a.Name + "_voice" }
func (a Agent) ModelDescription() string {
	if a.Name == "assistant" {
		return "default reasoning model"
	}
	return "reasoning model for " + a.Name
}
func (a Agent) VoiceDescription() string {
	if a.Name == "assistant" {
		return "default voice"
	}
	return "voice for " + a.Name
}
func (a Agent) PromptPath() string {
	if a.Name == "assistant" {
		return "instructions.md"
	}
	return "agents/" + a.Name + ".md"
}

type Handoff struct {
	Name             string
	Source           string
	To               string
	When             string
	Requires         []string
	History          string
	MaxMessages      int
	Summarizer       string
	IncludeToolCalls *bool
	AllVariables     bool
	Variables        []string
}

type Task struct {
	Name             string
	Instructions     string
	Tools            []string
	Model            string
	Result           string // flat typed result as a JSON object
	History          string
	MaxMessages      int
	Summarizer       string
	IncludeToolCalls *bool
	Agent            string
	When             string
	Assign           string // optional JSON object mapping variables to result fields
}

func (t Task) PromptPath() string { return "tasks/" + t.Name + ".md" }
func (t Task) RunName() string    { return "run_" + t.Name }

type TaskGroup struct {
	Name         string
	Steps        []string
	ContextScope string
	Then         string
	ThenTarget   string
	Agent        string
	When         string
}

func (g TaskGroup) RunName() string { return "run_" + g.Name }

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
	if d.EntryAgent == "" {
		d.EntryAgent = "assistant"
	}
	return d
}

// AllAgents returns the original assistant plus additional wizard agents in
// stable order. Keeping the starter fields on Data preserves noninteractive
// init compatibility.
func (d Data) AllAgents() []Agent {
	agents := []Agent{{Name: "assistant", Instructions: d.Instructions, Reason: d.Reason, Speak: d.Speak}}
	agents = append(agents, d.Agents...)
	sort.Slice(agents[1:], func(i, j int) bool { return agents[i+1].Name < agents[j+1].Name })
	return agents
}

func (d Data) AgentTools(name string) []string {
	var names []string
	seen := map[string]bool{}
	for _, tool := range d.Tools {
		attach := tool.AttachTo
		if len(attach) == 0 {
			attach = []string{"assistant"}
		}
		for _, agent := range attach {
			if agent == name && !seen[tool.Name] {
				names = append(names, tool.Name)
				seen[tool.Name] = true
			}
		}
	}
	for _, handoff := range d.Handoffs {
		if handoff.Source == name && !seen[handoff.Name] {
			names = append(names, handoff.Name)
			seen[handoff.Name] = true
		}
	}
	for _, task := range d.Tasks {
		if task.Agent == name && !seen[task.RunName()] {
			names = append(names, task.RunName())
			seen[task.RunName()] = true
		}
	}
	for _, group := range d.TaskGroups {
		if group.Agent == name && !seen[group.RunName()] {
			names = append(names, group.RunName())
			seen[group.RunName()] = true
		}
	}
	return names
}

func (d Data) TaskTools(name string) []string {
	seen := map[string]bool{}
	var names []string
	for _, task := range d.Tasks {
		if task.Name == name {
			for _, tool := range task.Tools {
				if !seen[tool] {
					names = append(names, tool)
					seen[tool] = true
				}
			}
		}
	}
	for _, tool := range d.Tools {
		for _, task := range tool.AttachTasks {
			if task == name && !seen[tool.Name] {
				names = append(names, tool.Name)
				seen[tool.Name] = true
			}
		}
	}
	return names
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
	bindings := []struct {
		role    targetcap.Role
		binding Binding
	}{{targetcap.Listen, d.Listen}}
	for _, agent := range d.AllAgents() {
		bindings = append(bindings, struct {
			role    targetcap.Role
			binding Binding
		}{targetcap.Reason, agent.Reason}, struct {
			role    targetcap.Role
			binding Binding
		}{targetcap.Speak, agent.Speak})
	}
	for _, item := range bindings {
		role, binding := item.role, item.binding
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
	agents := append([]Agent(nil), d.Agents...)
	sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })
	for _, agent := range agents {
		out := filepath.Join(dir, agent.PromptPath())
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return nil, fmt.Errorf("scaffold: %w", err)
		}
		if err := os.WriteFile(out, []byte(agent.Instructions+"\n"), 0o644); err != nil {
			return nil, fmt.Errorf("scaffold: %w", err)
		}
		created = append(created, out)
	}
	tasks := append([]Task(nil), d.Tasks...)
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].Name < tasks[j].Name })
	for _, task := range tasks {
		out := filepath.Join(dir, task.PromptPath())
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return nil, fmt.Errorf("scaffold: %w", err)
		}
		if err := os.WriteFile(out, []byte(task.Instructions+"\n"), 0o644); err != nil {
			return nil, fmt.Errorf("scaffold: %w", err)
		}
		created = append(created, out)
	}
	return created, nil
}

func parseTemplate(name string, raw []byte) (*template.Template, error) {
	return template.New(name).Funcs(template.FuncMap{
		"boolValue": func(value *bool) bool { return value != nil && *value },
		"quote":     strconv.Quote,
		"yaml":      yamlScalar,
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

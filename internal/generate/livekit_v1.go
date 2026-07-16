package generate

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/slng/unmute/internal/ir"
	targetcap "github.com/slng/unmute/internal/target"
)

// The LiveKit driver lowers the resolved IR into a runnable LiveKit Agents
// project (Python). Each agent is a livekit.agents.Agent; agent_transfer is a
// @function_tool returning the next Agent (native handoff); a task is an
// AgentTask[dict]; a task_group is a beta.workflows.TaskGroup. listen/speak
// resolve through the provider catalogue (internal/target/catalog_livekit.go):
// SLNG is the scaffold default, per-vendor plugins bind when the user picks
// them; reason lowers to LiveKit Inference. Python is emitted only through
// these templates (C1/ADR-0002).
//
//go:embed templates/livekit_v1/*.tmpl
var livekitV1Templates embed.FS

// beta.workflows (TaskGroup), AgentTask, and inference (LLM/TurnDetector) are all
// present from livekit-agents 1.5.x. Range: >=1.5, <2.0 (verified against the
// reference venv's 1.5.x, driver-livekit C7).
const (
	livekitVersionMajor    = 1
	livekitVersionMinMinor = 5
)

var livekitVersionPattern = regexp.MustCompile(`^(\d+)(?:\.(\d+))?(?:\.(\d+))?`)

// livekitService is one resolved binding: the rendered constructor plus its
// catalogue entry (imports/deps) and vendor (report labeling).
type livekitService struct {
	Call   ServiceCall
	Entry  targetcap.Entry
	Vendor string
}

type livekitAgent struct {
	Name        string
	Class       string
	PromptConst string
	IsEntry     bool
	LLM         *livekitService // set only when it differs from the session default
	TTS         *livekitService // set only when it differs from the session default
	Greeting    *livekitGreeting
	Transfers   []livekitTransfer
	Delegates   []livekitDelegate
}

// livekitGreeting drives the entry agent's on_enter: a fixed line, a
// model-written opening, or silence until the caller speaks.
type livekitGreeting struct {
	Say    string
	RunLLM bool
	Silent bool
}

type livekitTransfer struct {
	Method      string
	When        string
	TargetClass string
}

// livekitDelegate lowers a delegate control. A single task awaits its
// AgentTask directly (typed-result-only return, C4) and applies `assign`; a
// group builds a TaskGroup, awaits it, and hands control on per the group's
// `then`: return the typed results to the owner (merge: results), transfer to
// another agent, or end the call. N13: the flow's own turns never land in the
// owner's context regardless of which path is taken.
type livekitDelegate struct {
	Method    string
	When      string
	Task      *livekitSingleTask // set for a single-task delegate; Steps empty
	Steps     []livekitStep
	Isolated  bool   // context_scope: isolated — standalone-AgentTask sequence, no TaskGroup (C3)
	Then      string // "return" | "transfer" | "end"
	ThenClass string // target Agent class, set only for then: transfer
}

// livekitSingleTask is the task side of a single-task delegate: the AgentTask
// class to await plus the `assign` writes into the typed userdata (N5).
type livekitSingleTask struct {
	Class  string
	ID     string
	Assign []livekitAssign
}

type livekitAssign struct {
	Var   string
	Field string
}

// livekitVar is one typed shared-state field on the generated Userdata
// dataclass (SCHEMA 4.4; LiveKit session userdata).
type livekitVar struct {
	Name    string
	PyType  string
	Default string // Python literal; "None" when the spec declares none
}

type livekitStep struct {
	Class string
	ID    string
	Desc  string
}

type livekitTask struct {
	Name        string
	Class       string
	PromptConst string
	Result      []livekitArg // finish() args + the completed result dict
	Tools       []livekitTool
}

type livekitTool struct {
	Method      string
	Description string
	URLEnv      string
	Args        []livekitArg
}

type livekitArg struct {
	Name     string
	PyType   string
	Required bool
}

type livekitPrompt struct {
	Const string
	Body  string
}

type livekitData struct {
	Project       string
	Version       string
	AgentName     string
	EntryClass    string
	STT           livekitService
	SessionLLM    livekitService
	SessionTTS    livekitService
	TurnVersion   string
	Agents        []livekitAgent
	Tasks         []livekitTask
	Vars          []livekitVar
	Prompts       []livekitPrompt
	PluginModules []string // merged `from livekit.plugins import ...` names
	Deps          []string
	RequiredEnv   []string
	Notes         []string

	NeedsTasks      bool // AgentTask import
	NeedsTaskGroups bool // beta.workflows TaskGroup import
	NeedsHTTPX      bool // any webhook tool
	HasVars         bool // Userdata dataclass + session userdata
}

// GenerateLiveKit lowers a validated agent + livekit target into a project. The
// socket runs Validate(caps) first (V17), so this reads only agent+target.
func GenerateLiveKit(agent *ir.Agent, target ir.Target, bindings []ir.ForwardedBinding, sizing []ir.Sizing) (Artifact, error) {
	if err := checkLiveKitVersion(target.Version); err != nil {
		return Artifact{}, err
	}
	data, err := buildLiveKitData(agent, target)
	if err != nil {
		return Artifact{}, err
	}

	files, err := renderLiveKitFiles(data)
	if err != nil {
		return Artifact{}, err
	}
	report, err := livekitReport(data, files, bindings, sizing)
	if err != nil {
		return Artifact{}, err
	}
	files = append(files, File{Path: "compile-report.json", Content: report})
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	return Artifact{
		Kind:  CodeTarget,
		Files: files,
		Notes: GenerateReport{Notes: data.Notes},
	}, nil
}

// checkLiveKitVersion rejects a framework version outside the templates' range.
func checkLiveKitVersion(version string) error {
	if version == "" {
		return fmt.Errorf("livekit target requires a framework version")
	}
	match := livekitVersionPattern.FindStringSubmatch(version)
	if match == nil {
		return fmt.Errorf("livekit version %q is not a semantic version", version)
	}
	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	if major != livekitVersionMajor || minor < livekitVersionMinMinor {
		return fmt.Errorf("livekit version %q is outside the driver's template-compatible range (>=%d.%d, <%d.0)", version, livekitVersionMajor, livekitVersionMinMinor, livekitVersionMajor+1)
	}
	return nil
}

func renderLiveKitFiles(data livekitData) ([]File, error) {
	outputs := []struct{ tmpl, path string }{
		{"agent.py", "agent.py"},
		{"pyproject.toml", "pyproject.toml"},
		{"README.md", "README.md"},
		{"env.example", ".env.example"},
	}
	var files []File
	for _, o := range outputs {
		content, err := renderLiveKitV1(o.tmpl, data)
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: o.path, Content: content})
	}
	return files, nil
}

func renderLiveKitV1(name string, data livekitData) ([]byte, error) {
	raw, err := livekitV1Templates.ReadFile("templates/livekit_v1/" + name + ".tmpl")
	if err != nil {
		return nil, fmt.Errorf("livekit template %s: %w", name, err)
	}
	tmpl, err := template.New(name).Funcs(template.FuncMap{
		"pyq":    pyQuote,
		"join":   strings.Join,
		"triple": pyTriple,
	}).Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("livekit template %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("livekit template %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

// pyTriple renders a Go string as a Python triple-quoted string literal, safe
// for a multi-line prompt body.
func pyTriple(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"""`, `\"\"\"`)
	return `"""` + s + `"""`
}

type livekitReportJSON struct {
	Target      string                `json:"target"`
	Provider    string                `json:"provider"`
	Version     string                `json:"version"`
	EntryAgent  string                `json:"entry_agent"`
	Agents      []string              `json:"agents"`
	Tasks       []string              `json:"tasks,omitempty"`
	Files       []string              `json:"generated_files"`
	RequiredEnv []string              `json:"required_env"`
	Bindings    []ir.ForwardedBinding `json:"bindings,omitempty"`
	Sizing      []ir.Sizing           `json:"sizing,omitempty"`
	Notes       []string              `json:"notes,omitempty"`
}

func livekitReport(data livekitData, files []File, bindings []ir.ForwardedBinding, sizing []ir.Sizing) ([]byte, error) {
	generated := make([]string, 0, len(files)+1)
	for _, file := range files {
		generated = append(generated, file.Path)
	}
	generated = append(generated, "compile-report.json")
	slices.Sort(generated)
	agents := make([]string, 0, len(data.Agents))
	for _, a := range data.Agents {
		agents = append(agents, a.Name)
	}
	tasks := make([]string, 0, len(data.Tasks))
	for _, t := range data.Tasks {
		tasks = append(tasks, t.Name)
	}
	out, err := json.MarshalIndent(livekitReportJSON{
		Target: data.Project, Provider: "livekit", Version: data.Version, EntryAgent: data.EntryClass,
		Agents: agents, Tasks: tasks, Files: generated, RequiredEnv: data.RequiredEnv,
		Bindings: bindings, Sizing: sizing, Notes: data.Notes,
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

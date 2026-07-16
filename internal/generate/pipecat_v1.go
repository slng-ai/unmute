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

// The Pipecat driver lowers the resolved IR into a runnable project via the
// workers model (pipecat.workers): a main PipelineWorker owns transport + STT,
// each agent is an LLMWorker with its own LLM + TTS, and agent_transfer is
// activate_worker(). Python is emitted only through these templates (C1/ADR-0002).
//
//go:embed templates/pipecat_v1/*.tmpl
var pipecatV1Templates embed.FS

// The driver's templates target the Pipecat workers model (LLMWorker /
// activate_worker / @job) + Flows-in-core, which landed in 1.5.0 — the first
// 1.x release (versions jump 0.0.108 → 1.5.0). Range: >=1.5.0, <2.0.0.
const (
	pipecatVersionMajor    = 1
	pipecatVersionMinMinor = 5
)

var pipecatVersionPattern = regexp.MustCompile(`^(\d+)(?:\.(\d+))?(?:\.(\d+))?`)

// pyName turns a snake/kebab identifier into a safe Python class-name stem.
func pyName(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '_' || r == '-' })
	for i, part := range parts {
		if part != "" {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, "")
}

// pipecatService is a resolved provider binding: the rendered constructor
// (Call) plus its catalogue entry (imports/install), and the raw identity the
// task job-workers need to drive the OpenAI SDK directly. Model/voice/params
// are forwarded verbatim (C11); the catalogue only picks the code slot.
type pipecatService struct {
	Call      ServiceCall
	Entry     targetcap.Entry
	Vendor    string // resolved binding provider (report labeling)
	APIKeyEnv string
	BaseURL   string // env var name for base_url, empty if native
	Model     string
}

type pyKV struct {
	Key   string
	Value string // already a Python literal
}

// pipecatAgent is one LLMWorker: its class, worker name, LLM, TTS, prompt, and
// the tools/transfers/delegates it exposes as @tool methods.
type pipecatAgent struct {
	Name      string // worker name (the agent's snake_case id)
	Class     string // Python class name
	Prompt    string
	LLM       pipecatService
	TTS       pipecatService
	Tools     []pipecatTool
	Transfers []pipecatTransfer
	Delegates []pipecatDelegate
}

// pipecatTask is a delegate-and-return task lowered to a BaseWorker with a @job
// handler doing a structured OpenAI completion (non-voice, returns a typed result).
type pipecatTask struct {
	Name         string // worker + job name (the task's snake_case id)
	Class        string
	Prompt       string
	LLM          pipecatService
	ResultSchema string // Python literal: an OpenAI response_format json_schema
}

// pipecatDelegate is a delegate control: dispatch a task or an ordered group of
// tasks via @job, then return / transfer / end.
type pipecatDelegate struct {
	MethodName string
	When       string
	Task       string          // single-task delegate; "" if a group
	Assign     []pipecatAssign // result.<field> -> variable
	Group      string          // group delegate; "" if a single task
	Steps      []string        // ordered task/job names for a group
	Then       string          // "return" | "transfer" | "end"
	ThenTarget string          // target agent for then: transfer
	Isolated   bool            // context_scope: isolated (fresh payload per step)
}

type pipecatAssign struct {
	Var   string
	Field string
}

// pipecatTool is a webhook tool exposed as an @tool method that POSTs to url_env.
type pipecatTool struct {
	Name            string
	MethodName      string
	Description     string
	URLEnv          string
	Args            []pipecatArg
	EndsCall        bool
	Interruption    string // "cancel" | "continue" | "" (provider default)
	ColdDestination string // set for a cold human_transfer: the resolved number/SIP URI
}

type pipecatArg struct {
	Name     string
	Required bool
}

// pipecatTransfer is an agent_transfer control lowered to activate_worker.
type pipecatTransfer struct {
	MethodName string
	To         string   // target worker name
	When       string
	Reason     string   // developer message injected on activation
	Requires   []string // variables that must be set before the handoff (guard)
}

type pipecatVariable struct {
	Name    string
	PyType  string
	Default string // Python literal
}

type pipecatData struct {
	Project     string
	Version     string
	MainName    string
	EntryAgent  string
	EntryClass  string
	STT                 pipecatService
	Agents              []pipecatAgent
	Tasks               []pipecatTask
	Variables           []pipecatVariable
	GreetingInstruction string
	GreetingRunLLM      string // "True" or "False"
	Interrupt           *pipecatInterrupt
	Inactivity          *pipecatInactivity
	MaxDurationSecs     int
	HasColdTransfer     bool
	Transport           string
	Imports             []string
	Extras              []string
	Deps                []string // standalone pip deps for plugin services (e.g. pipecat-slng)
	RequiredEnv         []string
	Notes               []string

	// Import needs: keep bot.py free of unused imports (only what a given spec
	// actually exercises), so the emitted pipeline reads clean.
	NeedsAsyncio        bool // _end_after max-duration timer
	NeedsHTTPX          bool // any webhook tool
	NeedsFunctionCalls  bool // any @tool/transfer/delegate (FunctionCallParams)
	NeedsTurnStrategies bool // interruption min-words strategy
	NeedsEndFrame       bool
	NeedsAppendFrame    bool
}

// Provider → service facts (class, import, extra/dep, key env, constructor
// shape) live in the provider catalogue (internal/target/catalog_pipecat.go).
// V11's exactly-one-install and import-per-class rules are catalogue
// invariants now (TestCatalogInvariants).

type pipecatInterrupt struct {
	Enabled      bool
	MinWords     int
	IgnorePhrase []string
}

type pipecatInactivity struct {
	NudgeSecs int
	EndSecs   int
}

// pipecatEmittedFields declares every capability field the Pipecat emitter has a
// real code path for. It MUST equal the table's non-gated Pipecat rows — the T12
// agreement test enforces it, so a field can never be validate-green while the
// emitter silently drops it (compiler V19). Add a row here only with the code.
var pipecatEmittedFields = map[targetcap.Field]bool{
	targetcap.FieldListenLocal:           true, // placement forwarded (code target runs it locally)
	targetcap.FieldSpeakLocal:            true,
	targetcap.FieldReasonLocal:           true,
	targetcap.FieldSpeakEndpoint:         true, // base_url on the TTS service
	targetcap.FieldTurnPlacement:         true, // advisory (VAD/smart-turn supplied)
	targetcap.FieldSemanticEndpointing:   true, // advisory
	targetcap.FieldTask:                  true, // @job task-worker
	targetcap.FieldTaskModel:             true, // task-worker's own LLM
	targetcap.FieldTaskNestedResult:      true, // forwarded json_schema
	targetcap.FieldTaskGroup:             true, // sequential job dispatch
	targetcap.FieldTaskGroupReturn:       true,
	targetcap.FieldContextIsolated:       true, // fresh vs accumulated payload
	targetcap.FieldTransferRequires:      true, // guard before activate_worker
	targetcap.FieldGreetingUserFirst:     true,
	targetcap.FieldGreetingModelWritten:  true,
	targetcap.FieldGreetingAbsent:        true,
	targetcap.FieldInterruptionMinWords:  true, // MinWordsUserTurnStartStrategy
	targetcap.FieldInterruptionIgnore:    true, // IGNORE_PHRASES
	targetcap.FieldInactivity:            true, // user_idle_timeout
	targetcap.FieldMaxDuration:           true, // asyncio EndFrame timer
	targetcap.FieldToolOutput:            true, // tool returns response.json()
	targetcap.FieldToolInterruption:      true, // cancel_on_interruption
}

// GeneratePipecat lowers a validated agent + pipecat target into a project.
// The socket runs Validate(caps) first (V17), so this reads only agent+target.
func GeneratePipecat(agent *ir.Agent, target ir.Target) (Artifact, error) {
	if err := checkPipecatVersion(target.Version); err != nil {
		return Artifact{}, err
	}
	data, err := buildPipecatData(agent, target)
	if err != nil {
		return Artifact{}, err
	}

	files, err := renderPipecatFiles(data)
	if err != nil {
		return Artifact{}, err
	}
	report, err := pipecatReport(data, files)
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

// checkPipecatVersion rejects a target version outside the templates' range (V8).
func checkPipecatVersion(version string) error {
	if version == "" {
		return fmt.Errorf("pipecat target requires a framework version")
	}
	match := pipecatVersionPattern.FindStringSubmatch(version)
	if match == nil {
		return fmt.Errorf("pipecat version %q is not a semantic version", version)
	}
	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	if major != pipecatVersionMajor || minor < pipecatVersionMinMinor {
		return fmt.Errorf("pipecat version %q is outside the driver's template-compatible range (>=%d.%d, <%d.0)", version, pipecatVersionMajor, pipecatVersionMinMinor, pipecatVersionMajor+1)
	}
	return nil
}

func renderPipecatFiles(data pipecatData) ([]File, error) {
	// tmpl → output path (decoupled so .env.example can't be a dotfile template,
	// which Go's embed would skip).
	outputs := []struct{ tmpl, path string }{
		{"bot.py", "bot.py"},
		{"pyproject.toml", "pyproject.toml"},
		{"Dockerfile", "Dockerfile"},
		{"README.md", "README.md"},
		{"pcc-deploy.toml", "pcc-deploy.toml"},
		{"env.example", ".env.example"},
	}
	var files []File
	for _, o := range outputs {
		content, err := renderPipecatV1(o.tmpl, data)
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: o.path, Content: content})
	}
	return files, nil
}

func renderPipecatV1(name string, data pipecatData) ([]byte, error) {
	raw, err := pipecatV1Templates.ReadFile("templates/pipecat_v1/" + name + ".tmpl")
	if err != nil {
		return nil, fmt.Errorf("pipecat template %s: %w", name, err)
	}
	tmpl, err := template.New(name).Funcs(template.FuncMap{"pyq": pyQuote, "join": strings.Join}).Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("pipecat template %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("pipecat template %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

// pyQuote renders a Go string as a Python string literal.
func pyQuote(s string) string { return strconv.Quote(s) }

type pipecatReportJSON struct {
	Target      string   `json:"target"`
	Provider    string   `json:"provider"`
	Version     string   `json:"version"`
	EntryAgent  string   `json:"entry_agent"`
	Agents      []string `json:"agents"`
	Files       []string `json:"generated_files"`
	RequiredEnv []string `json:"required_env"`
	Notes       []string `json:"notes,omitempty"`
}

func pipecatReport(data pipecatData, files []File) ([]byte, error) {
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
	out, err := json.MarshalIndent(pipecatReportJSON{
		Target: data.Project, Provider: "pipecat", Version: data.Version, EntryAgent: data.EntryAgent,
		Agents: agents, Files: generated, RequiredEnv: data.RequiredEnv, Notes: data.Notes,
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

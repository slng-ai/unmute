package generate

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

type ArtifactKind string

const (
	CodeTarget ArtifactKind = "code"
)

type Artifact struct {
	Kind      ArtifactKind
	Files     []File
	Apply     *ApplyPlan
	Notes     GenerateReport
	Telephony *TelephonyRuntimePlan
}

type File struct {
	Path    string
	Content []byte
}

type ApplyPlan struct {
	CredentialEnv string
	Steps         []ApplyStep
}

type ApplyStep struct {
	Method    string
	Endpoint  string
	Body      json.RawMessage
	CaptureID string
}

type GenerateReport struct {
	Warnings          []string
	Notes             []string
	ForwardedBindings []ir.ForwardedBinding
	Sizing            []ir.Sizing
}

// Generate validates once, then dispatches to exactly one provider driver.
func Generate(agent *ir.Agent, resolved ir.Target, caps target.Table) (Artifact, error) {
	report, err := ir.Validate(agent, []ir.Target{resolved}, caps)
	if err != nil {
		// Surface the provider-vocabulary diagnostics, not just the failure count:
		// compile must show what validate shows (SCHEMA.md §6.2, rule 12).
		if diag := targetDiagnostics(report); diag != "" {
			return Artifact{}, fmt.Errorf("generate %s: %w (%s)", resolved.Name, err, diag)
		}
		return Artifact{}, fmt.Errorf("generate %s: %w", resolved.Name, err)
	}
	artifact := Artifact{Kind: artifactKind(resolved.Provider), Telephony: TelephonyRuntimePlanFor(resolved)}
	artifact.Notes.ForwardedBindings = report.ForwardedBindings
	artifact.Notes.Sizing = report.Sizing
	for _, row := range report.PerTarget {
		artifact.Notes.Warnings = append(artifact.Notes.Warnings, row.Warnings...)
	}
	switch resolved.Provider {
	case ir.ProviderLiveKit:
		emitted, err := GenerateLiveKit(agent, resolved, report.ForwardedBindings, report.Sizing)
		if err != nil {
			return Artifact{}, fmt.Errorf("generate %s livekit: %w", resolved.Name, err)
		}
		artifact.Files, err = withTelephonyReport(emitted.Files, artifact.Telephony)
		if err != nil {
			return Artifact{}, fmt.Errorf("generate %s livekit: %w", resolved.Name, err)
		}
		artifact.Files = append(artifact.Files, knowledgeFiles(agent)...)
		artifact.Notes.Notes = append(artifact.Notes.Notes, emitted.Notes.Notes...)
		artifact.Notes.Notes = append(artifact.Notes.Notes, knowledgeNotes(agent)...)
		artifact.Notes.Warnings = append(artifact.Notes.Warnings, emitted.Notes.Warnings...)
		return artifact, nil
	case ir.ProviderPipecat:
		emitted, err := GeneratePipecat(agent, resolved, report.ForwardedBindings, report.Sizing)
		if err != nil {
			return Artifact{}, fmt.Errorf("generate %s pipecat: %w", resolved.Name, err)
		}
		artifact.Files, err = withTelephonyReport(emitted.Files, artifact.Telephony)
		if err != nil {
			return Artifact{}, fmt.Errorf("generate %s pipecat: %w", resolved.Name, err)
		}
		artifact.Files = append(artifact.Files, knowledgeFiles(agent)...)
		artifact.Notes.Notes = append(artifact.Notes.Notes, emitted.Notes.Notes...)
		artifact.Notes.Notes = append(artifact.Notes.Notes, knowledgeNotes(agent)...)
		artifact.Notes.Warnings = append(artifact.Notes.Warnings, emitted.Notes.Warnings...)
		return artifact, nil
	default:
		return Artifact{}, fmt.Errorf("unsupported provider %q", resolved.Provider)
	}
}

// knowledgeFiles copies each knowledge base document into the artifact, byte for
// byte, at the path spec.Load already keyed it by.
//
// Verbatim is the requirement, not an implementation detail: a PDF is binary, and
// a transformation would corrupt it silently. The failure would then appear as
// bad retrieval on a phone call rather than as an error at compile, so the gate
// is a byte-equality test (internal/generate/knowledge_artifact_test.go).
//
// The documents travel in the image because a managed platform offers no mounted
// storage to read them from (FR-014).
func knowledgeFiles(agent *ir.Agent) []File {
	if len(agent.Documents) == 0 {
		return nil
	}
	files := make([]File, 0, len(agent.Documents))
	for _, path := range slices.Sorted(maps.Keys(agent.Documents)) {
		files = append(files, File{Path: path, Content: agent.Documents[path]})
	}
	return files
}

// knowledgeNotes is the compile report's knowledge section (FR-015): what the
// agent will read at every process start.
//
// No passage count. Splitting happens at startup, so a number here would be a
// guess presented as a fact; the document count is what the compiler knows.
func knowledgeNotes(agent *ir.Agent) []string {
	if len(agent.Knowledge) == 0 {
		return nil
	}
	notes := make([]string, 0, len(agent.Knowledge))
	for _, name := range slices.Sorted(maps.Keys(agent.Knowledge)) {
		base := agent.Knowledge[name]
		documents := "documents"
		if len(base.Files) == 1 {
			documents = "document"
		}
		notes = append(notes, fmt.Sprintf("knowledge %q: %d %s, embed %s, fixed until next compile",
			name, len(base.Files), documents, base.Embed))
	}
	return notes
}

// targetDiagnostics joins the per-target validation errors so the compile path
// prints the same provider-vocabulary diagnostic that validate does.
func targetDiagnostics(report ir.ValidateReport) string {
	var msgs []string
	for _, row := range report.PerTarget {
		for _, e := range row.Errors {
			msgs = append(msgs, fmt.Sprintf("%s: %s", row.Name, e))
		}
	}
	return strings.Join(msgs, "; ")
}

func artifactKind(provider ir.Provider) ArtifactKind {
	switch provider {
	case ir.ProviderLiveKit, ir.ProviderPipecat:
		return CodeTarget
	default:
		return ""
	}
}

// --- facts both code drivers share -----------------------------------------

// The framework version check both code drivers ask moved to
// internal/target.CheckVersion, because ir.Validate asks it too and a version
// outside the range used to validate green and fail compile.

// envRef renders the environment-lookup idiom the emitted Python uses. Both
// drivers emit the same expression, so there is one of it.
func envRef(name string) string { return "os.environ[" + pyQuote(name) + "]" }

// primitiveTypes is the one place a schema primitive is named. Three different
// outputs are needed from it — the JSON Schema name, the Python annotation, and
// the runtime isinstance check — so the table carries three columns and the
// accessors below stay separate. Merging the outputs themselves would be wrong;
// what was wrong was writing the key set out five times.
var primitiveTypes = map[ir.PrimitiveType]struct {
	json  string
	py    string
	check string
}{
	ir.PrimitiveBoolean: {"boolean", "bool", "isinstance(value, bool)"},
	ir.PrimitiveInteger: {"integer", "int", "isinstance(value, int) and not isinstance(value, bool)"},
	ir.PrimitiveNumber:  {"number", "float", "isinstance(value, (int, float)) and not isinstance(value, bool)"},
	ir.PrimitiveString:  {"string", "str", "isinstance(value, str)"},
}

// primitiveString is the fallback row: anything unrecognised is a string, which
// is what all five original switches did in their default arm.
var primitiveString = primitiveTypes[ir.PrimitiveString]

func primitiveRow(t ir.PrimitiveType) struct{ json, py, check string } {
	if row, ok := primitiveTypes[t]; ok {
		return row
	}
	return primitiveString
}

// jsonType is a primitive's JSON Schema name.
func jsonType(t ir.PrimitiveType) string { return primitiveRow(t).json }

// pyType is a primitive's Python annotation.
func pyType(t ir.PrimitiveType) string { return primitiveRow(t).py }

// pyTypeForJSON maps a JSON Schema name back to its Python annotation, for the
// places that only ever saw the JSON spelling.
func pyTypeForJSON(name string) string {
	for _, row := range primitiveTypes {
		if row.json == name {
			return row.py
		}
	}
	return primitiveString.py
}

// livekitTypeCheck is a primitive's runtime isinstance expression.
func livekitTypeCheck(t ir.PrimitiveType) string { return primitiveRow(t).check }

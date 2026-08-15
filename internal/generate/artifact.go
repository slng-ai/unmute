package generate

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

type ArtifactKind string

const (
	CodeTarget    ArtifactKind = "code"
	ManagedTarget ArtifactKind = "managed"
)

type Artifact struct {
	Kind      ArtifactKind
	Files     []File
	Apply     *ApplyPlan
	Notes     GenerateReport
	Telephony *TelephonyRuntimePlan

	// LiveKitInference lists the bindings that route through LiveKit Inference
	// (each a human-readable phrase). Non-empty means console mode needs
	// LIVEKIT_API_KEY/SECRET even though it never connects to a room (C2/C7);
	// nil means the target runs `console` on provider keys alone. Only the
	// livekit driver sets it; the CLI reads it so it never re-derives the fact.
	LiveKitInference []string
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
		artifact.LiveKitInference = emitted.LiveKitInference
		artifact.Notes.Notes = append(artifact.Notes.Notes, emitted.Notes.Notes...)
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
		artifact.Notes.Notes = append(artifact.Notes.Notes, emitted.Notes.Notes...)
		artifact.Notes.Warnings = append(artifact.Notes.Warnings, emitted.Notes.Warnings...)
		return artifact, nil
	case ir.ProviderVapi:
		return artifact, fmt.Errorf("vapi driver is not implemented")
	case ir.ProviderDeepgram:
		return artifact, fmt.Errorf("deepgram driver is not implemented")
	default:
		return Artifact{}, fmt.Errorf("unsupported provider %q", resolved.Provider)
	}
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
	case ir.ProviderLiveKit, ir.ProviderPipecat, ir.ProviderDeepgram:
		return CodeTarget
	case ir.ProviderVapi:
		return ManagedTarget
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

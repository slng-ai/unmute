package generate

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

type ArtifactKind string

const (
	// CodeTarget is a runnable project: Python, a Dockerfile, a runbook.
	CodeTarget ArtifactKind = "code"
	// BodyTarget is a deployment body for a platform that runs the agent: JSON
	// documents and a runbook, with nothing to run locally. The kinds are
	// separate because internal/cli writes them the same way and reports them
	// differently, and because an unhandled kind must be an error rather than a
	// silent no-op.
	BodyTarget ArtifactKind = "body"
)

type Artifact struct {
	Kind      ArtifactKind
	Files     []File
	Notes     GenerateReport
	Telephony *TelephonyRuntimePlan
	// Requires is what the account must already hold for this artifact to
	// deploy. Only the slng driver fills it: the other two targets emit a
	// project somebody else's platform runs, and that platform owns whatever it
	// needs to already have. `unmute deploy` checks these before writing
	// anything, and the emitted runbook prints the same value, so the two cannot
	// disagree about what a package needs.
	Requires Requirements
	// ArtifactID is the twilio driver's build identity, the value its app.py
	// serves on /healthz and its compile report records. Empty for every other
	// target.
	ArtifactID string
}

type File struct {
	Path    string
	Content []byte
	// Verbatim is the author's own code, copied as written: the write path
	// formats it no more than it would edit it.
	Verbatim bool
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
	artifact := Artifact{Kind: artifactKind(resolved.Provider)}
	artifact.Notes.ForwardedBindings = report.ForwardedBindings
	artifact.Notes.Sizing = report.Sizing
	for _, row := range report.PerTarget {
		artifact.Notes.Warnings = append(artifact.Notes.Warnings, row.Warnings...)
	}
	// LiveKit and Pipecat run on unmute-core. ir.Validate has already refused
	// version 1 on them.
	if resolved.Provider == ir.ProviderLiveKit || resolved.Provider == ir.ProviderPipecat {
		files, err := GenerateCore(agent, resolved)
		if err != nil {
			return Artifact{}, fmt.Errorf("generate %s %s: %w", resolved.Name, resolved.Provider, err)
		}
		artifact.Files = files
		return withManifestReport(artifact, agent)
	}
	// Twilio and SLNG keep their own runtime, and read version 2 with the keys it
	// can run.
	if agent.Version == ir.Version2 {
		if err := ownRuntimeRefusals(agent, resolved.Provider); err != nil {
			return Artifact{}, fmt.Errorf("generate %s %s: %w", resolved.Name, resolved.Provider, err)
		}
	}
	artifact.Telephony = TelephonyRuntimePlanFor(resolved)
	switch resolved.Provider {
	case ir.ProviderTwilio:
		emitted, err := GenerateTwilio(agent, resolved, report.ForwardedBindings, report.Sizing)
		if err != nil {
			return Artifact{}, fmt.Errorf("generate %s twilio: %w", resolved.Name, err)
		}
		artifact.Files, err = withTelephonyReport(emitted.Files, artifact.Telephony)
		if err != nil {
			return Artifact{}, fmt.Errorf("generate %s twilio: %w", resolved.Name, err)
		}
		artifact.ArtifactID = emitted.ArtifactID
		return withManifestReport(artifact, agent)
	case ir.ProviderSlng:
		// No withTelephonyReport: unmute writes slng no carrier state, so there is
		// no route plan to report and Telephony is nil above.
		emitted, err := GenerateSlng(agent, resolved)
		if err != nil {
			return Artifact{}, fmt.Errorf("generate %s slng: %w", resolved.Name, err)
		}
		artifact.Files = emitted.Files
		artifact.Notes.Notes = append(artifact.Notes.Notes, emitted.Notes.Notes...)
		artifact.Notes.Warnings = append(artifact.Notes.Warnings, emitted.Notes.Warnings...)
		// The only branch that sets this. A livekit or pipecat project is deployed
		// by that platform's own tool, which owns whatever it needs to find
		// already in place; unmute has nothing to check on its behalf.
		artifact.Requires = emitted.Requires
		return withManifestReport(artifact, agent)
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
	case ir.ProviderLiveKit, ir.ProviderPipecat, ir.ProviderTwilio:
		return CodeTarget
	case ir.ProviderSlng:
		return BodyTarget
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

package generate

import (
	"encoding/json"
	"fmt"

	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/target"
)

type ArtifactKind string

const (
	CodeTarget    ArtifactKind = "code"
	ManagedTarget ArtifactKind = "managed"
)

type Artifact struct {
	Kind  ArtifactKind
	Files []File
	Apply *ApplyPlan
	Notes GenerateReport
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
	Branch    string
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
		return Artifact{}, fmt.Errorf("generate %s: %w", resolved.Name, err)
	}
	artifact := Artifact{Kind: artifactKind(resolved.Provider)}
	artifact.Notes.ForwardedBindings = report.ForwardedBindings
	artifact.Notes.Sizing = report.Sizing
	for _, row := range report.PerTarget {
		artifact.Notes.Warnings = append(artifact.Notes.Warnings, row.Warnings...)
	}
	switch resolved.Provider {
	case ir.ProviderLiveKit:
		return artifact, fmt.Errorf("livekit driver is not implemented")
	case ir.ProviderPipecat:
		emitted, err := GeneratePipecat(agent, resolved)
		if err != nil {
			return Artifact{}, fmt.Errorf("generate %s pipecat: %w", resolved.Name, err)
		}
		artifact.Files = emitted.Files
		artifact.Notes.Notes = append(artifact.Notes.Notes, emitted.Notes.Notes...)
		artifact.Notes.Warnings = append(artifact.Notes.Warnings, emitted.Notes.Warnings...)
		return artifact, nil
	case ir.ProviderVapi:
		return artifact, fmt.Errorf("vapi driver is not implemented")
	case ir.ProviderElevenLabs:
		return artifact, fmt.Errorf("elevenlabs driver is not implemented")
	case ir.ProviderDeepgram:
		return artifact, fmt.Errorf("deepgram driver is not implemented")
	default:
		return Artifact{}, fmt.Errorf("unsupported provider %q", resolved.Provider)
	}
}

func artifactKind(provider ir.Provider) ArtifactKind {
	switch provider {
	case ir.ProviderLiveKit, ir.ProviderPipecat, ir.ProviderDeepgram:
		return CodeTarget
	case ir.ProviderVapi, ir.ProviderElevenLabs:
		return ManagedTarget
	default:
		return ""
	}
}

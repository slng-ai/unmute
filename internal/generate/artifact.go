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
	Warnings []string
	Notes    []string
}

// Generate validates once, then dispatches to exactly one provider driver.
func Generate(agent *ir.Agent, resolved ir.Target, caps target.Table) (Artifact, error) {
	if _, err := ir.Validate(agent, []ir.Target{resolved}, caps); err != nil {
		return Artifact{}, fmt.Errorf("generate %s: %w", resolved.Name, err)
	}
	switch resolved.Provider {
	case ir.ProviderLiveKit:
		return Artifact{}, fmt.Errorf("livekit driver is not implemented")
	case ir.ProviderPipecat:
		return Artifact{}, fmt.Errorf("pipecat driver is not implemented")
	case ir.ProviderVapi:
		return Artifact{}, fmt.Errorf("vapi driver is not implemented")
	case ir.ProviderElevenLabs:
		return Artifact{}, fmt.Errorf("elevenlabs driver is not implemented")
	case ir.ProviderDeepgram:
		return Artifact{}, fmt.Errorf("deepgram driver is not implemented")
	default:
		return Artifact{}, fmt.Errorf("unsupported provider %q", resolved.Provider)
	}
}

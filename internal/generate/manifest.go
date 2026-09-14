package generate

import (
	"encoding/json"
	"fmt"

	"github.com/slng-ai/unmute/internal/ir"
)

func withManifestReport(artifact Artifact, agent *ir.Agent) (Artifact, error) {
	if agent.Manifest == nil {
		return artifact, nil
	}
	_, warnings := ir.ValidateManifest(agent)
	for i, file := range artifact.Files {
		if file.Path != "compile-report.json" {
			continue
		}
		var report map[string]any
		if err := json.Unmarshal(file.Content, &report); err != nil {
			return Artifact{}, fmt.Errorf("decode manifest compile report: %w", err)
		}
		report["manifest"] = struct {
			Name     string   `json:"manifest"`
			Version  int      `json:"version"`
			Warnings []string `json:"warnings,omitempty"`
		}{agent.Manifest.Name, agent.Manifest.Version, warnings}
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return Artifact{}, fmt.Errorf("encode manifest compile report: %w", err)
		}
		artifact.Files[i].Content = append(data, '\n')
		return artifact, nil
	}
	return Artifact{}, fmt.Errorf("manifest compile report: compile-report.json is missing")
}

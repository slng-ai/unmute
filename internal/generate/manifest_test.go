package generate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
)

func TestManifestReportCarriesIdentityAndUnverifiedSettings(t *testing.T) {
	agent := &ir.Agent{Manifest: &spec.Manifest{Name: "acme", Version: 3, Languages: &spec.ManifestAllow{Allow: []string{"en"}}}, Models: map[string]ir.ModelDef{"speech": {Kind: ir.KindListen, Provider: "slng", Model: "speech"}}}
	artifact, err := withManifestReport(Artifact{Files: []File{{Path: "compile-report.json", Content: []byte(`{"target":"livekit"}`)}}}, agent)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Manifest struct {
			Name     string   `json:"manifest"`
			Version  int      `json:"version"`
			Warnings []string `json:"warnings"`
		}
	}
	if err := json.Unmarshal(artifact.Files[0].Content, &report); err != nil {
		t.Fatal(err)
	}
	if report.Manifest.Name != "acme" || report.Manifest.Version != 3 || !strings.Contains(strings.Join(report.Manifest.Warnings, "\n"), "cannot be verified") {
		t.Fatalf("report: %s", artifact.Files[0].Content)
	}
}

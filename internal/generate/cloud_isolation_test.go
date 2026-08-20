package generate

import (
	"slices"
	"strings"
	"testing"
)

// The runbook and the artifact set have to agree about where a build deploys.
// `pcc-deploy.toml` is emitted exactly for the routes with a managed-platform
// path, so a README that teaches `pipecat cloud deploy` without one sends the
// reader to a manifest that was never written. That is what shipped: every
// (pipecat, carrier-websocket, *) build carried the whole **Deploy to Pipecat
// Cloud** section, manifest sentence included, while emitting no manifest and
// building from a plain Python base image.
func TestPipecatReadmeTeachesCloudDeployExactlyWhenTheManifestIsEmitted(t *testing.T) {
	for name, artifact := range map[string]Artifact{
		"plain Pipecat Cloud":        plainPipecatArtifact(t),
		"Daily with carrier":         dailyCarrierArtifact(t, "twilio", true),
		"cloud-websocket":            cloudWebsocketArtifact(t, cloudWebsocketOptions{inbound: true}),
		"carrier-websocket (twilio)": carrierWebsocketArtifact(t, "twilio"),
		"carrier-websocket (telnyx)": carrierWebsocketArtifact(t, "telnyx"),
		"carrier-websocket (plivo)":  carrierWebsocketArtifact(t, "plivo"),
	} {
		manifest := slices.Contains(artifactPaths(artifact), "pcc-deploy.toml")
		readme := artifactFile(t, artifact, "README.md")
		switch teaches := strings.Contains(readme, "pipecat cloud deploy"); {
		case manifest && !teaches:
			t.Errorf("%s emits pcc-deploy.toml but its README never says how to deploy it", name)
		case !manifest && teaches:
			t.Errorf("%s emits no pcc-deploy.toml but its README teaches `pipecat cloud deploy`", name)
		}
		// The self-hosted route still has to say where it runs, or the runbook
		// simply stops after the environment list.
		if !manifest && !strings.Contains(readme, "## Deploy it yourself") {
			t.Errorf("%s has no managed-platform path and no self-hosted deploy section", name)
		}
	}
}

package generate

import (
	"encoding/json"
	"maps"
	"regexp"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

func relayDeskAgent(t *testing.T) *ir.Agent {
	t.Helper()
	pkg, err := spec.Load(relayDesk)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

func twilioID(t *testing.T, agent *ir.Agent, resolved ir.Target) string {
	t.Helper()
	artifact, err := Generate(agent, resolved, target.Default())
	if err != nil {
		t.Fatal(err)
	}
	return artifact.ArtifactID
}

// The id is one value in three places, and a recompile reproduces it.
func TestTwilioArtifactIDIsStableAndAgrees(t *testing.T) {
	for _, instance := range []string{"twilio-openai", "twilio-gemini"} {
		first := twilioArtifact(t, relayDesk, instance)
		again := twilioArtifact(t, relayDesk, instance)
		if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(first.ArtifactID) {
			t.Fatalf("%s artifact id %q is not sha256:<hex>", instance, first.ArtifactID)
		}
		if first.ArtifactID != again.ArtifactID {
			t.Errorf("%s recompiled to a different id: %s then %s", instance, first.ArtifactID, again.ArtifactID)
		}
		if !strings.Contains(artifactFile(t, first, "app.py"), `ARTIFACT_ID = "`+first.ArtifactID+`"`) {
			t.Errorf("%s app.py does not carry %s", instance, first.ArtifactID)
		}
		var report struct {
			ArtifactID string `json:"artifact_id"`
		}
		if err := json.Unmarshal([]byte(artifactFile(t, first, "compile-report.json")), &report); err != nil {
			t.Fatal(err)
		}
		if report.ArtifactID != first.ArtifactID {
			t.Errorf("%s compile report id %q, app.py id %q", instance, report.ArtifactID, first.ArtifactID)
		}
	}
	if twilioArtifact(t, relayDesk, "twilio-openai").ArtifactID == twilioArtifact(t, relayDesk, "twilio-gemini").ArtifactID {
		t.Error("two target instances with different models share one id")
	}
}

// Anything that changes what a call does changes the id.
func TestTwilioArtifactIDFollowsBehaviour(t *testing.T) {
	agent := relayDeskAgent(t)
	resolved := agent.Targets["twilio-openai"]
	base := twilioID(t, agent, resolved)

	for name, mutate := range map[string]func(*ir.Agent, *ir.Target){
		"instructions": func(a *ir.Agent, _ *ir.Target) {
			entry := a.Agents[a.EntryAgent]
			entry.Instructions += " Be brief."
			a.Agents[a.EntryAgent] = entry
		},
		"model": func(a *ir.Agent, r *ir.Target) {
			entry := a.Agents[a.EntryAgent]
			binding := r.Models.Reason[entry.Model]
			binding.Model += "-next"
			r.Models.Reason[entry.Model] = binding
		},
		"tool schema": func(a *ir.Agent, _ *ir.Target) {
			tool := a.Tools["opening_hours"]
			tool.Description += " Also weekends."
			a.Tools["opening_hours"] = tool
		},
		"tool handler": func(a *ir.Agent, _ *ir.Target) {
			tool := a.Tools["opening_hours"]
			tool.HandlerSource += "\n# changed\n"
			a.Tools["opening_hours"] = tool
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := relayDeskAgent(t)
			changedTarget := changed.Targets["twilio-openai"]
			changedTarget.Models.Reason = maps.Clone(changedTarget.Models.Reason)
			mutate(changed, &changedTarget)
			if got := twilioID(t, changed, changedTarget); got == base {
				t.Errorf("changing the %s kept id %s", name, base)
			}
		})
	}
	if _, ok := agent.Tools["opening_hours"]; !ok {
		t.Fatal("relay-desk no longer has the opening_hours tool this test changes")
	}
}

// Compiling reads no credential, so none can reach a file or the id.
func TestTwilioArtifactIDHoldsNoSecret(t *testing.T) {
	ids := map[string]bool{}
	for _, secret := range []string{"sekret-one-4f2a", "sekret-two-9c1b"} {
		for _, name := range []string{"TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN", "TWILIO_PHONE_NUMBER_SID", "TWILIO_PUBLIC_URL", "OPENAI_API_KEY"} {
			t.Setenv(name, secret)
		}
		artifact := twilioArtifact(t, relayDesk, "twilio-openai")
		ids[artifact.ArtifactID] = true
		for _, file := range artifact.Files {
			if strings.Contains(string(file.Content), secret) {
				t.Errorf("%s carries an environment value", file.Path)
			}
		}
	}
	if len(ids) != 1 {
		t.Errorf("environment values changed the id: %v", ids)
	}
}

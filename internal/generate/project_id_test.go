package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

const testProjectID = "123e4567-e89b-12d3-a456-426614174000"

// Per-request headers replace constructor ones, so a project header on some of
// a package's header dicts and not others bills part of a call to the key's own
// project. Every dict that carries the session id carries the project too, and
// a package that names no project sends no project header at all.
func TestSlngRouterSendsProjectIDOnEveryHeaderDict(t *testing.T) {
	entry := `"` + target.SlngProjectIDHeader + `": "` + testProjectID + `"`
	for _, tc := range routerTargets() {
		agent := routerFixture(t)
		_, without := emitAgentSource(t, agent, tc.provider, tc.module)
		if strings.Contains(without, target.SlngProjectIDHeader) {
			t.Errorf("%s: a package with no project_id sends %s", tc.provider, target.SlngProjectIDHeader)
		}
		agent.ProjectID = testProjectID
		_, with := emitAgentSource(t, agent, tc.provider, tc.module)
		sessions := strings.Count(with, `"`+target.SlngSessionIDHeader+`":`)
		if got := strings.Count(with, entry); sessions == 0 || got != sessions {
			t.Errorf("%s: %d header dicts carry the project, want all %d", tc.provider, got, sessions)
		}
	}
}

// The twilio brain builds its own header dicts, one on the model settings and
// one per request.
func TestTwilioRouterSendsProjectID(t *testing.T) {
	for _, id := range []string{"", testProjectID} {
		pkg, err := spec.Load(relayDesk)
		if err != nil {
			t.Fatal(err)
		}
		pkg.Agent.ProjectID = id
		agent, err := buildWithState(t, pkg)
		if err != nil {
			t.Fatal(err)
		}
		artifact, err := Generate(agent, agent.Targets["twilio-slng"], target.Default())
		if err != nil {
			t.Fatal(err)
		}
		var all strings.Builder
		for _, file := range artifact.Files {
			all.Write(file.Content)
		}
		scopes := strings.Count(all.String(), `"`+target.SlngAgentIDHeader+`": ROUTER_SCOPE`)
		got := strings.Count(all.String(), `"`+target.SlngProjectIDHeader+`": "`+testProjectID+`"`)
		want := 0
		if id != "" {
			want = scopes
		}
		if scopes == 0 || got != want {
			t.Errorf("project_id %q: %d of %d header dicts carry the project, want %d", id, got, scopes, want)
		}
		if id == "" && strings.Contains(all.String(), target.SlngProjectIDHeader) {
			t.Errorf("a package with no project_id sends %s", target.SlngProjectIDHeader)
		}
	}
}

// On version 2 the project travels in agent.json, and spec.py adds it to the
// router's headers only when it is there.
func TestCoreSpecCarriesProjectID(t *testing.T) {
	for _, id := range []string{"", testProjectID} {
		pkg, err := spec.Load(filepath.Join("..", "voice-agents-tests", "kestrel"))
		if err != nil {
			t.Fatal(err)
		}
		pkg.Agent.ProjectID = id
		agent, err := buildWithState(t, pkg)
		if err != nil {
			t.Fatal(err)
		}
		artifact, err := Generate(agent, agent.Targets["pipecat"], target.Default())
		if err != nil {
			t.Fatal(err)
		}
		data := artifactFile(t, artifact, "agent.json")
		if got := strings.Contains(data, `"project_id": "`+testProjectID+`"`); got != (id != "") {
			t.Errorf("project_id %q: agent.json carries it = %v", id, got)
		}
		if id == "" && strings.Contains(data, "project_id") {
			t.Error("a package with no project_id writes one into agent.json")
		}
		wantHeaders := `extra_headers={"` + target.SlngProjectIDHeader + `": PROJECT_ID} if PROJECT_ID else None,`
		if !strings.Contains(artifactFile(t, artifact, "spec.py"), wantHeaders) {
			t.Errorf("spec.py lacks %s", wantHeaders)
		}
	}
}

package generate

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// The hosted-tool fixtures. Two packages rather than one, and the reason is
// worth knowing before reading anything below: a code target requires a `turn:`
// binding and the slng target refuses one, because SLNG owns its own turn
// taking and its create body has no turn section. That is a pre-existing
// cross-target gap, not something hosted tools introduce, so the fixtures work
// around it rather than pretending it is not there.
//
// Both name the same two tools SLNG hosts and commit a mirror of each. Every
// test here compiles a real package rather than a hand-built value, because the
// mirror is read off disk and a hand-built one would prove the reading works by
// not doing it.
const (
	hostedSlngFixture = "slng_hosted"
	hostedCodeFixture = "slng_hosted_code"
)

func buildHosted(t *testing.T, fixture string) *ir.Agent {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := buildWithState(t, pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

// emitHosted compiles one hosted fixture to one provider and returns its files
// by path.
func emitHosted(t *testing.T, fixture string, provider ir.Provider) map[string]string {
	t.Helper()
	agent := buildHosted(t, fixture)
	artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
	if err != nil {
		t.Fatalf("generate %s: %v", provider, err)
	}
	files := map[string]string{}
	for _, file := range artifact.Files {
		files[file.Path] = string(file.Content)
	}
	return files
}

// TestSlngHostedToolEmitsARefAndNoBody: a hosted reference names a tool the
// platform owns, so the body carries the name and unmute creates nothing.
//
// The negative half is the one that matters. A body would try to create the
// tool, and the push would then own a definition the platform already has, at
// whatever version the package happened to mirror.
func TestSlngHostedToolEmitsARefAndNoBody(t *testing.T) {
	files := emitHosted(t, hostedSlngFixture, ir.ProviderSlng)

	var body struct {
		ToolRefs []struct {
			Tool        string `json:"tool"`
			Description string `json:"description"`
		} `json:"tool_refs"`
	}
	if err := json.Unmarshal([]byte(files["agent.json"]), &body); err != nil {
		t.Fatalf("agent.json is not JSON: %v", err)
	}
	// An attachment's description is an override, so only an authored one is
	// written and an inherited one is left out. check_order writes its own;
	// search_places_text writes none, so the published version's description
	// reaches the model and removing the field is what restores that.
	//
	// This inverts what the driver used to do. It emitted the mirror's
	// description as an override, which meant a mirror that had gone stale
	// pinned the platform's old words onto the attachment, and deleting
	// `description:` from the file could not restore inheritance because the
	// file was never what supplied it.
	want := map[string]string{
		"check_order":        "Look up an order by its number and return its status and delivery date.",
		"search_places_text": "",
	}
	seen := map[string]bool{}
	for _, ref := range body.ToolRefs {
		if expected, ok := want[ref.Tool]; ok {
			seen[ref.Tool] = true
			if ref.Description != expected {
				t.Errorf("%s reference description = %q, want %q", ref.Tool, ref.Description, expected)
			}
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("agent.json carries no reference to hosted tool %q", name)
		}
	}

	for path := range files {
		if strings.HasPrefix(path, "tools/") {
			t.Errorf("the slng target wrote %s for a hosted tool: a body would create a tool the platform already owns", path)
		}
	}
}

// TestUnhandledExecutionKindIsRefusedNotPosted is the guard for the failure mode
// that made this feature's Fail Loud story load-bearing, and it is not
// hypothetical on either code target.
//
// LiveKit's emitted tool dispatch ends in `{{else}}{{template "webhook_tool"
// .}}`, and Pipecat's agent method template branches the same way. So a
// lowering that returns a zero value for a kind it does not know does NOT
// render to nothing: it renders an HTTP POST to an environment name nobody
// registered. That is worse than an absent tool, because the emitted project
// imports, starts, answers a call and only fails when the model reaches for the
// tool, by which time the log says KeyError on a variable no reader can trace
// back to a missing template arm.
//
// Both builders therefore refuse the kind instead, and this test holds them to
// it by name.
// It deliberately uses a package with no hosted tool in it, and a kind that is
// not one of the real constants, so it holds the *default* rather than any one
// arm: a kind added to ir without a lowering fails here.
func TestUnhandledExecutionKindIsRefusedNotPosted(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := buildWithState(t, pkg)
	if err != nil {
		t.Fatal(err)
	}
	unknown := agent.Tools["lookup_customer"]
	unknown.Execution = ir.ToolExecution("some_future_kind")
	agent.Tools["lookup_customer"] = unknown

	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
		if err == nil {
			// Not just "it did not error": prove the thing we are afraid of.
			for _, file := range artifact.Files {
				if !strings.HasSuffix(file.Path, ".py") {
					continue
				}
				if strings.Contains(string(file.Content), "lookup_customer") {
					t.Fatalf("%s emitted %s naming lookup_customer for an unlowerable kind: the dispatch fell through, which on this target means an HTTP POST", provider, file.Path)
				}
			}
			t.Fatalf("%s generated an artifact for an execution kind it cannot lower", provider)
		}
		if !strings.Contains(err.Error(), "some_future_kind") {
			t.Errorf("%s refusal does not name the kind it could not lower: %v", provider, err)
		}
		if !strings.Contains(err.Error(), "lookup_customer") {
			t.Errorf("%s refusal does not name the tool: %v", provider, err)
		}
	}

}

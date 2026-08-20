package generate

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// A task prompt names the call that ends the step. This finds each of those
// openings so the test can check what follows it.
var taskFinishOpening = regexp.MustCompile("When this step is complete, call `([a-z_]+)`")

// TestEmittedTaskPromptsAlwaysOfferAnEscape holds the fix for a real dead end
// (B: salon compound request, 2026-08-20). A task advertises its declared tools
// plus finish, so a step that deliberately carries no handoff — a mutation step,
// say — has no route out when the caller asks for something else mid-step. The
// model refused, the caller pressed, and it refused again until the call died.
//
// The escape is finish itself: the parent agent owns the handoffs. This asserts
// the property rather than the wording, so the sentence can be reworded but not
// dropped: wherever an emitted prompt names its finish call, the escape for that
// same call has to be there too.
func TestEmittedTaskPromptsAlwaysOfferAnEscape(t *testing.T) {
	root := filepath.Join("..", "..", "examples")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, entry.Name(), "agent.yaml")); err != nil {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			pkg, err := spec.Load(filepath.Join(root, entry.Name()))
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			agent, err := ir.Build(pkg)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if len(agent.Tasks) == 0 {
				t.Skip("package declares no task")
			}
			for _, name := range slices.Sorted(maps.Keys(agent.Targets)) {
				resolved := agent.Targets[name]
				if resolved.Provider != ir.ProviderLiveKit && resolved.Provider != ir.ProviderPipecat {
					continue
				}
				artifact, err := Generate(agent, resolved, target.Default())
				if err != nil {
					t.Fatalf("generate %q: %v", name, err)
				}
				escapes := 0
				for _, file := range artifact.Files {
					// A LiveKit prompt is a triple-quoted string with real
					// newlines, a Pipecat node's role_message a one-line string
					// with escaped ones. Read both the same way.
					body := strings.ReplaceAll(string(file.Content), `\n`, "\n")
					for _, at := range taskFinishOpening.FindAllStringSubmatchIndex(body, -1) {
						call := body[at[2]:at[3]]
						rest := body[at[1]:]
						if next := taskFinishOpening.FindStringIndex(rest); next != nil {
							rest = rest[:next[0]]
						}
						escape := escapeFor(call)
						if escape == "" || !strings.Contains(rest, escape) {
							t.Errorf("%s: %s: a task prompt names %q with no escape from a request the step cannot serve", name, file.Path, call)
						}
						escapes++
					}
				}
				if escapes == 0 {
					t.Errorf("%s: package declares %d task(s) and no emitted prompt names its finish call", name, len(agent.Tasks))
				}
			}
		})
	}
}

// escapeFor is the escape sentence taskFinishContract writes for one finish
// call, read back out of the helper so a rewording moves both together.
func escapeFor(call string) string {
	_, escape, _ := strings.Cut(taskFinishContract(call, nil), ".\n\n")
	return escape
}

// TestEmittedFinishCarriesTheUnservedRequest is the other half of the escape:
// the prompt tells the model to name the request it could not serve, so the
// generated finish has to have somewhere to put it and the owning agent has to
// be told to read it. Four wiring points, one per target pair, all reachable
// from one example that has a locked-down step.
func TestEmittedFinishCarriesTheUnservedRequest(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "salon-concierge"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}

	livekit := generatedFile(t, agent, ir.ProviderLiveKit, "agent.py")
	finishes := 0
	for _, line := range strings.Split(livekit, "\n") {
		if !strings.Contains(line, "async def finish(") {
			continue
		}
		finishes++
		if !strings.Contains(line, ", "+ir.UnservedResultField+": ") || !strings.HasSuffix(line, `= "") -> None:`) {
			t.Errorf("agent.py: finish must take an optional %s: %s", ir.UnservedResultField, strings.TrimSpace(line))
		}
	}
	if finishes == 0 {
		t.Fatal("agent.py: no task finish emitted")
	}
	// One call per finish: a task that built its result dict directly would
	// take the field and drop it on the floor.
	if !strings.Contains(livekit, "def _task_result(values: dict") {
		t.Error("agent.py: no _task_result helper")
	}
	if got := strings.Count(livekit, "_task_result({"); got != finishes {
		t.Errorf("agent.py: %d of %d finishes route their result through _task_result", got, finishes)
	}
	if !strings.Contains(livekit, unservedOwnerRule) {
		t.Error("agent.py: no delegate tells its owner to read the handed-back request")
	}

	pipecat := generatedFile(t, agent, ir.ProviderPipecat, "bot.py")
	nodes := strings.Count(pipecat, `name="finish_`)
	if nodes == 0 {
		t.Fatal("bot.py: no task finish emitted")
	}
	if got := strings.Count(pipecat, `"`+ir.UnservedResultField+`": {"description"`); got != nodes {
		t.Errorf("bot.py: %d of %d finish schemas declare %s", got, nodes, ir.UnservedResultField)
	}
	// Optional, always: a required field would force the model to invent one.
	if required := regexp.MustCompile(`required=\[[^]]*` + ir.UnservedResultField).FindString(pipecat); required != "" {
		t.Errorf("bot.py: %s must stay optional: %s", ir.UnservedResultField, required)
	}
	if !strings.Contains(pipecat, unservedOwnerRule) {
		t.Error("bot.py: the results handback does not tell the owner to read the handed-back request")
	}
}

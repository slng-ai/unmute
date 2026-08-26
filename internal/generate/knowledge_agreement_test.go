package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

// TestKnowledgeContractIsTheSameOnBothTargets holds FR-026: the author sees one
// contract, and the two lowerings differ only where the frameworks force them to.
//
// The shared module is what makes this cheap to be true. Reading, splitting,
// indexing, both halves of the search and the merge all live in
// templates/knowledge/knowledge.py.tmpl, which both drivers render, so there is
// no second copy to drift. What differs is only how a tool is registered and
// where the index is built.
func TestKnowledgeContractIsTheSameOnBothTargets(t *testing.T) {
	agent := knowledgeAgent(t)
	modules := map[ir.Provider]string{}
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
		if err != nil {
			t.Fatalf("generate %s: %v", provider, err)
		}
		modules[provider] = artifactFile(t, artifact, "knowledge.py")
	}
	if modules[ir.ProviderLiveKit] != modules[ir.ProviderPipecat] {
		t.Error("the two targets emit different knowledge.py: they render one shared template, so any difference is a bug in how one of them passes its data")
	}
	// And the model-facing half agrees: one query parameter, the same appended
	// instruction, the same base named.
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
		if err != nil {
			t.Fatalf("generate %s: %v", provider, err)
		}
		entry := "agent.py"
		if provider == ir.ProviderPipecat {
			entry = "bot.py"
		}
		body := artifactFile(t, artifact, entry)
		for _, want := range []string{
			"Look up the salon's refund and complaints policy.",
			"higher number means a closer match",
			`knowledge.look_up("refunds", query)`,
			"query",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: %s must contain %q", provider, entry, want)
			}
		}
	}
}

// TestKnowledgeGateIsOpenOnBothTargets is the other half of the flip.
//
// internal/target/table_test.go asserts the row from the table's side; this
// asserts it from the emitter's, so opening the gate without a lowering, or
// landing a lowering without opening the gate, both fail. That pairing is what
// the vapi and deepgram retirement was about.
func TestKnowledgeGateIsOpenOnBothTargets(t *testing.T) {
	table := target.Default()
	for _, provider := range target.Providers {
		if got := table.Capability(target.FieldToolKnowledge, provider); got.Tag != target.Core {
			t.Errorf("%s is %q on %s but both drivers emit a lowering", target.FieldToolKnowledge, got.Tag, provider)
		}
	}
	if !livekitEmittedFields[target.FieldToolKnowledge] {
		t.Error("the LiveKit emitter does not declare the knowledge field")
	}
	if !pipecatEmittedFields[target.FieldToolKnowledge] {
		t.Error("the Pipecat emitter does not declare the knowledge field")
	}
}

// TestPipecatCopiesTheKnowledgeFiles holds FR-014 on the target whose image copies
// named files only.
//
// Both lines matter and for the same reason tools/ does: bot.py imports knowledge
// at module scope, and knowledge.py reads knowledge/<base>/ from the working
// directory. Leave either out and the container starts, raises, and the
// deployment never reaches ready while its own log looks perfectly healthy. A
// missing tools/ copy shipped that way in v0.1.0 through v0.1.2 and broke five of
// eleven examples.
func TestPipecatCopiesTheKnowledgeFiles(t *testing.T) {
	agent := knowledgeAgent(t)
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dockerfile := artifactFile(t, artifact, "Dockerfile")
	for _, want := range []string{"COPY knowledge.py ./", "COPY knowledge/ ./knowledge/"} {
		if !strings.Contains(dockerfile, want) {
			t.Errorf("Dockerfile must contain %q:\n%s", want, dockerfile)
		}
	}
	// Instructions only. The template carries a comment explaining why `COPY . .`
	// is never used here, and a check that reads comments fails on its own
	// rationale.
	for _, line := range strings.Split(dockerfile, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed == "COPY . ." && !strings.Contains(dockerfile, "FROM python:") {
			t.Error("the Pipecat Cloud image must copy named files only: /app belongs to the base image and copying over it replaces the server that answers /bot")
		}
	}
}

// TestPipecatIndexesAtModuleImport: once per container, not once per session.
//
// Pipecat Cloud imports this module and then calls bot() for each session, keeping
// a warm container for about five minutes after one ends, so a module-level build
// is amortised across every session that lands on it. Inside bot() it would run
// per session and a caller would wait for it.
func TestPipecatIndexesAtModuleImport(t *testing.T) {
	agent := knowledgeAgent(t)
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	bot := artifactFile(t, artifact, "bot.py")
	if strings.Count(bot, "knowledge.build_indexes()") != 1 {
		t.Errorf("build_indexes() must be called exactly once:\n%s", bot)
	}
	before, after, ok := strings.Cut(bot, "knowledge.build_indexes()")
	if !ok {
		t.Fatal("no build_indexes() call")
	}
	// Module scope: the call must not be indented, which is what would put it
	// inside bot() or another function.
	lines := strings.Split(before, "\n")
	if last := lines[len(lines)-1]; strings.TrimSpace(last) != "" {
		t.Errorf("build_indexes() is indented, so it runs per call rather than per container: %q", last)
	}
	if strings.Contains(after, "build_indexes") {
		t.Error("build_indexes() appears twice")
	}
}

// TestKnowledgeRunbookReachesTheEmittedREADME holds the fourth surface of
// CLAUDE.md's four-places rule: the generated README is the runbook, and a fact
// only true in a template is a fact nobody reads.
//
// It exists because this shipped broken once. The section was inserted using
// "## MCP tool sources" as its anchor, which sits *inside* a `{{if .NeedsMCP}}`
// block, so on the salon example — which has no MCP tool — the whole section
// vanished from every emitted README while the template grep showed it present.
// A test that read the template would have passed.
func TestKnowledgeRunbookReachesTheEmittedREADME(t *testing.T) {
	agent := salonAgent(t)
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		t.Run(string(provider), func(t *testing.T) {
			artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			readme := artifactFile(t, artifact, "README.md")
			for _, want := range []string{
				"## Knowledge bases",
				// Each declared base, by name and by folder.
				"`refunds`, from `knowledge/refunds/`",
				"`services`, from `knowledge/services/`",
				// The three facts an operator needs and cannot infer.
				"fixed until the next compile",
				"no text layer",
				"does not end the call",
			} {
				if !strings.Contains(readme, want) {
					t.Errorf("emitted README must state %q", want)
				}
			}
			// The credential is named, because an operator reads this file to
			// find out what to set.
			if !strings.Contains(readme, "OPENAI_API_KEY") {
				t.Error("emitted README must name the embedding credential")
			}
		})
	}

	// And a package with no knowledge base gets none of it.
	plain := authAgent(t, nil)
	artifact, err := Generate(plain, targetByProvider(t, plain, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if strings.Contains(artifactFile(t, artifact, "README.md"), "## Knowledge bases") {
		t.Error("a package with no knowledge base must not get the section")
	}
}

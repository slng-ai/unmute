package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// salonAgent builds the shipped salon example, which is the two-agent,
// two-knowledge-base case this feature exists for.
func salonAgent(t *testing.T) *ir.Agent {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "salon-concierge"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

// TestKnowledgeIsolationIsPerAgent holds SC-007, which is the headline claim of
// the feature and the one that would be a real leak if it were wrong.
//
// An agent reaches a knowledge base by being given its tool. There is no second
// mechanism, no allow list and nothing to configure, which is exactly why this is
// worth a test: the property is emergent from tool attachment, so nothing else
// enforces it.
//
// The salon example is the case. The concierge quotes prices and must not be able
// to quote refund policy; the complaint specialist is the other way round.
func TestKnowledgeIsolationIsPerAgent(t *testing.T) {
	agent := salonAgent(t)
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
		// Each lookup names exactly one base, and each base is named by exactly
		// one tool. A tool that reached both would be the leak.
		for tool, base := range map[string]string{
			"look_up_refund_policy": "refunds",
			"look_up_salon_info":    "services",
		} {
			def, _, ok := strings.Cut(body, "async def "+tool+"(")
			_ = def
			if !ok {
				t.Fatalf("%s: %s does not define %s", provider, entry, tool)
			}
			want := `knowledge.look_up("` + base + `", query)`
			if !strings.Contains(body, want) {
				t.Errorf("%s: %s must contain %q", provider, entry, want)
			}
			other := "refunds"
			if base == "refunds" {
				other = "services"
			}
			// The body between this tool's definition and the next one must not
			// reach the other base.
			after := body[strings.Index(body, "async def "+tool+"("):]
			if end := strings.Index(after[1:], "\n    async def "); end > 0 {
				after = after[:end]
			}
			if strings.Contains(after, `knowledge.look_up("`+other+`"`) {
				t.Errorf("%s: %s reaches knowledge base %q as well as %q", provider, tool, other, base)
			}
		}
	}
}

// TestKnowledgeBasesAreSeparateCollections: two bases, two collections, two
// folders, and no shared index.
//
// A single collection with a metadata filter would have been less code and a
// worse design: a filter that is ever wrong or ever forgotten leaks one client's
// documents into another agent's answers, and nothing in a call would show it.
func TestKnowledgeBasesAreSeparateCollections(t *testing.T) {
	agent := salonAgent(t)
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	py := artifactFile(t, artifact, "knowledge.py")
	for _, base := range []string{"refunds", "services"} {
		for _, want := range []string{
			`_INDEXES["` + base + `"] = _start("` + base + `"`,
			`_embed_` + base + `()`,
		} {
			if !strings.Contains(py, want) {
				t.Errorf("knowledge.py must contain %q", want)
			}
		}
	}
	// Two bases, two calls, and each call builds or loads its own index, so neither
	// base can see the other's passages.
	if got := strings.Count(py, "= _start("); got != 2 {
		t.Errorf("want two indexed bases, got %d", got)
	}
	// One construction site per phase, reached once per base: _index() builds at
	// startup and _bake_one() builds at image build time. A third would be a third
	// place for the separation to be got wrong.
	if got := strings.Count(py, "VectorStoreIndex("); got != 2 {
		t.Errorf("index construction must live in one place per phase (startup, bake), found %d call sites", got)
	}
	// Each base keeps its own baked directory, for the same reason it keeps its own
	// index: a shared one would need a filter to stay separate, and a filter that is
	// ever wrong leaks one client's documents into another agent's answers.
	if !strings.Contains(py, "INDEX_ROOT / name") {
		t.Error("a baked index must live under a per-base directory")
	}
	// Nothing filters by metadata, because there is nothing to filter: a filter
	// that is ever wrong or ever forgotten leaks one base into another's answers,
	// and nothing in a call would show it. Both spellings, the raw store's and
	// LlamaIndex's own.
	for _, filtered := range []string{`where={`, "filters="} {
		if strings.Contains(py, filtered) {
			t.Errorf("knowledge.py filters by %s: bases are separate indexes, not one index with a filter", filtered)
		}
	}

	// And the documents land in their own folders.
	folders := map[string]bool{}
	for _, file := range artifact.Files {
		if rest, ok := strings.CutPrefix(file.Path, "knowledge/"); ok {
			folders[strings.SplitN(rest, "/", 2)[0]] = true
		}
	}
	if !folders["refunds"] || !folders["services"] || len(folders) != 2 {
		t.Errorf("artifact knowledge folders = %v, want exactly refunds and services", folders)
	}
}

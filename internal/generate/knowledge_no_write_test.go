package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

// A knowledge lookup performs no write outside the container, on any path.
//
// The threat is specific and it is not hypothetical: LlamaIndex's documented way
// to keep an index is `StorageContext.persist()` writing a JSON docstore, and
// `BM25Retriever` has its own `persist()` besides. Either one reached for by habit
// would put call-derived data on a platform's ephemeral disk, survive nothing, and
// make cold start slower than the thing it was meant to speed up.
//
// The whole design depends on them being absent, which is why this is a gate and
// not a comment: with no storage context the vectors live in memory and the process
// is the storage lifetime. The Chroma names below are kept deliberately, because
// reintroducing that store is one of the ways this property could be lost.
func TestKnowledgePerformsNoWrite(t *testing.T) {
	code := pythonCodeOnly(emittedKnowledgePy(t, ir.ProviderLiveKit))
	// Split the module by phase. bake() and _bake_one() run at image build time and
	// are expected to write; everything else runs on a call and must not.
	runtime, buildTime := splitByPhase(t, code, "bake", "_bake_one")

	for _, forbidden := range []string{
		// Chroma persistence, in all three of its forms.
		"PersistentClient", "HttpClient", "persist_directory",
		// LlamaIndex persistence.
		".persist(", "SimpleDocumentStore",
		// Anything writing a file by hand.
		"Path.write", ".write_text", ".write_bytes", "shutil.",
		// Or reaching a network store.
		"requests.", "httpx.",
	} {
		if strings.Contains(runtime, forbidden) {
			t.Errorf("the run-time path of knowledge.py must not contain %q: a call writes nothing outside this container", forbidden)
		}
	}
	// Reading a baked index back is a read, and it belongs to the run-time path;
	// writing one does not.
	if !strings.Contains(runtime, "load_index_from_storage") {
		t.Error("the run-time path must load a baked index when the image carries one")
	}
	if !strings.Contains(buildTime, ".persist(") {
		t.Error("the build-time path must persist the index, or there is nothing to load")
	}
	// The bake writes exactly one thing the run-time path did not: its own
	// fingerprint. Anywhere else, a write_text is the defect this gate exists for.
	if !strings.Contains(buildTime, ".write_text") {
		t.Error("the build-time path must record what it baked, so a stale index is detectable")
	}
	if !strings.Contains(code, "VectorStoreIndex(") {
		t.Error("the vector half must construct VectorStoreIndex directly, so that the default in-memory store is what holds the embeddings")
	}
}

// splitByPhase returns the emitted module split in two: everything outside the
// named functions, and the named functions' own bodies.
//
// Crude on purpose. It relies only on top-level `def name(` starting a function and
// the next line beginning in column zero ending it, which is true of generated
// code and needs no Python parser in a Go test.
func splitByPhase(t *testing.T, code string, names ...string) (outside, inside string) {
	t.Helper()
	lines := strings.Split(code, "\n")
	wanted := map[string]bool{}
	for _, n := range names {
		wanted[n] = true
	}
	var out, in []string
	current := ""
	for _, line := range lines {
		if strings.HasPrefix(line, "def ") {
			name := strings.SplitN(strings.TrimPrefix(line, "def "), "(", 2)[0]
			current = ""
			if wanted[name] {
				current = name
				delete(wanted, name)
			}
		} else if line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, ")") {
			current = ""
		}
		if current != "" {
			in = append(in, line)
		} else {
			out = append(out, line)
		}
	}
	if len(wanted) != 0 {
		t.Fatalf("emitted knowledge.py defines no %v", wanted)
	}
	return strings.Join(out, "\n"), strings.Join(in, "\n")
}

// TestKnowledgeBakeIsBuildTimeOnly: nothing on a call path reaches the bake.
//
// The whole no-write property rests on this. If build_indexes() or look_up() ever
// called bake(), the module would write into the container on a call, and the
// phase split in the gate above would still pass because the write itself lives in
// the right function.
func TestKnowledgeBakeIsBuildTimeOnly(t *testing.T) {
	code := pythonCodeOnly(emittedKnowledgePy(t, ir.ProviderLiveKit))
	runtime, _ := splitByPhase(t, code, "bake", "_bake_one")
	for _, called := range []string{"bake()", "_bake_one("} {
		if strings.Contains(runtime, called) {
			t.Errorf("the run-time path calls %s: baking writes, and a call must not", called)
		}
	}
	// And the Dockerfile is what does call it, on both targets.
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		agent := knowledgeAgent(t)
		artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		if !strings.Contains(artifactFile(t, artifact, "Dockerfile"), "knowledge.bake()") {
			t.Errorf("%s: the Dockerfile must run the bake, or no image ever carries an index", provider)
		}
	}
}

// TestKnowledgeBakeIsGatedOnABuildArg holds the cache correctness of the bake step.
//
// BuildKit does not hash secret contents, so a RUN that only tested for a mounted
// secret file would produce one cache entry whichever way the first build went, and
// every later build would reuse it. Building once without the credential would then
// silently skip the bake forever. An ARG referenced inside the RUN is part of the
// cache key, which is why the decision has to be spelled as one.
func TestKnowledgeBakeIsGatedOnABuildArg(t *testing.T) {
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		t.Run(string(provider), func(t *testing.T) {
			agent := knowledgeAgent(t)
			artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			dockerfile := artifactFile(t, artifact, "Dockerfile")
			if !strings.Contains(dockerfile, "ARG KNOWLEDGE_BAKE=0") {
				t.Error("the bake must be gated on KNOWLEDGE_BAKE, defaulting to off")
			}
			if !strings.Contains(dockerfile, `"$KNOWLEDGE_BAKE"`) {
				t.Error("KNOWLEDGE_BAKE must be read inside the RUN, or it is not in the layer's cache key")
			}
			// One secret mount per credential actually in use, and the credential
			// must never be spelled as a plain ENV or ARG, which would bake it into
			// a layer.
			if !strings.Contains(dockerfile, "--mount=type=secret,id=OPENAI_API_KEY") {
				t.Error("the embedding credential must arrive as a build secret")
			}
			for _, leak := range []string{"ENV OPENAI_API_KEY", "ARG OPENAI_API_KEY"} {
				if strings.Contains(dockerfile, leak) {
					t.Errorf("%q would write the credential into an image layer", leak)
				}
			}
		})
	}
}

// TestKnowledgeBakedIndexIsFingerprinted: a baked index that does not match the
// settings it is loaded under is refused.
//
// Chunking and mode decide what the passages are, so an index baked under other
// settings is not a stale cache, it is a different corpus. Loading one answers from
// the wrong passages and nothing anywhere looks wrong, which is the exact failure
// this feature exists to prevent.
func TestKnowledgeBakedIndexIsFingerprinted(t *testing.T) {
	code := pythonCodeOnly(emittedKnowledgePy(t, ir.ProviderLiveKit))
	if !strings.Contains(code, "_fingerprint(name)") {
		t.Error("the loader must compare the baked settings against the module's own")
	}
	if !strings.Contains(code, "raise KnowledgeError") {
		t.Error("a settings mismatch must raise, not warn: the answers would be wrong and silent")
	}
	// The marker is written last, so a half-written index cannot read as complete.
	runtime, buildTime := splitByPhase(t, code, "bake", "_bake_one")
	if !strings.Contains(buildTime, "baked.json") || !strings.Contains(runtime, "baked.json") {
		t.Error("both phases must agree on the marker file that means a bake finished")
	}
}

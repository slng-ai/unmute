package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

// emittedKnowledgePy generates the fixture and returns the emitted knowledge.py.
func emittedKnowledgePy(t *testing.T, provider ir.Provider) string {
	t.Helper()
	agent := knowledgeAgent(t)
	artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return artifactFile(t, artifact, "knowledge.py")
}

// pythonCodeOnly strips comments and triple-quoted blocks from emitted Python.
//
// Every negative check in these files needs it. The emitted module explains its
// own decisions in docstrings, naming the exact things the checks forbid — the
// measured batch ceilings, `from_documents`, `raise`, "lower is closer" — so a
// check that reads prose fails on the rationale for the code being right. That
// mistake was made three times before this existed.
func pythonCodeOnly(py string) string {
	var out []string
	inDocstring := false
	for _, line := range strings.Split(py, "\n") {
		trimmed := strings.TrimSpace(line)
		// A docstring opening and closing on one line contributes nothing.
		if !inDocstring && strings.Count(trimmed, `"""`) >= 2 {
			continue
		}
		if strings.Contains(trimmed, `"""`) {
			inDocstring = !inDocstring
			continue
		}
		if inDocstring || trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// TestKnowledgeReaderGuardsTheSilentEmptyRead is the most important test in this
// feature, because it holds the one failure mode that produces no symptom.
//
// SimpleDirectoryReader returns an empty list for a path with a dot-component in
// it, and raises nothing. A build without the flag ships an agent that starts
// healthy, answers every question with "I don't have that information", and logs
// nothing at all. The flag alone is not enough either: it fixes one cause of an
// empty read, so the emptiness check has to be there too for any other cause.
//
// Both halves, or this test fails.
func TestKnowledgeReaderGuardsTheSilentEmptyRead(t *testing.T) {
	py := emittedKnowledgePy(t, ir.ProviderLiveKit)
	if !strings.Contains(py, "exclude_hidden=False") {
		t.Error("SimpleDirectoryReader must set exclude_hidden=False: the default returns nothing for a dot-path without raising")
	}
	if !strings.Contains(py, "if not documents:") {
		t.Error("an empty read must fail loudly: the flag stops one cause, not every cause")
	}
	// The failure has to stop the process, not degrade to an empty index.
	if !strings.Contains(py, "read no documents from") {
		t.Error("the empty-read failure must name the folder it read")
	}
}

// TestKnowledgeIndexesAtStartupNotPerLookup holds FR-017: reading, splitting and
// embedding happen once per process, and the lookup path touches none of them.
//
// The negative half is the point. A lookup that rebuilt the index would work
// perfectly in every test and add seconds to every caller's question.
func TestKnowledgeIndexesAtStartupNotPerLookup(t *testing.T) {
	py := emittedKnowledgePy(t, ir.ProviderLiveKit)
	build, lookUp, ok := strings.Cut(py, "async def look_up(")
	if !ok {
		t.Fatal("no look_up function in the emitted module")
	}
	for _, want := range []string{"VectorStoreIndex(", "nodes=nodes", "SentenceSplitter(", "SimpleDirectoryReader("} {
		if !strings.Contains(build, want) {
			t.Errorf("startup path must call %s", want)
		}
		if strings.Contains(lookUp, want) {
			t.Errorf("the lookup path must not call %s: indexing is startup work (FR-017)", want)
		}
	}
	// prewarm is LiveKit's documented seam for static data a job needs, and it
	// runs before any job is accepted, so a failure stops the worker reporting
	// ready rather than ruining a call.
	agentPy := func() string {
		agent := knowledgeAgent(t)
		artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		return artifactFile(t, artifact, "agent.py")
	}()
	prewarm, rest, ok := strings.Cut(agentPy, "server.setup_fnc = prewarm")
	if !ok {
		t.Fatal("no prewarm registration in the emitted agent")
	}
	if !strings.Contains(prewarm, "knowledge.build_indexes()") {
		t.Error("build_indexes() must be called from prewarm")
	}
	if strings.Contains(rest, "build_indexes()") {
		t.Error("build_indexes() must be called once, in prewarm, and nowhere on the call path")
	}
}

// TestKnowledgeStartupFailuresAreLoud holds FR-010 and FR-020: a knowledge base
// with nothing readable, and a missing credential, both stop the process.
//
// A scanned PDF is a deploy failure, deliberately the same class as a missing
// credential. That is a weaker guarantee than a compile failure, taken knowingly:
// deciding whether a PDF yields text needs a parser the compiler does not have.
func TestKnowledgeStartupFailuresAreLoud(t *testing.T) {
	py := emittedKnowledgePy(t, ir.ProviderLiveKit)
	for _, want := range []string{
		// No document yields text: fail, naming what was skipped.
		"no document yielded any text",
		// Some do and some do not: warn by name, keep running, say how many.
		"skipping %s, no text layer",
		"%d of %d documents indexed",
		// The credential, by name, before the first embedding call.
		"OPENAI_API_KEY is not set",
		// Raised, not logged: the deployment must never reach ready.
		"raise KnowledgeError",
	} {
		if !strings.Contains(py, want) {
			t.Errorf("emitted knowledge.py must contain %q", want)
		}
	}
	// The warning path and the failure path must be different: a warning that
	// kills the process and a failure that only warns are both wrong, and
	// neither is visible from the message alone.
	if !strings.Contains(py, "log.warning(") {
		t.Error("the partial-text case must warn, not fail")
	}
}

// TestKnowledgeLookupFailureNeverEndsTheCall holds FR-023: a failed lookup is a
// result the agent can talk about, not an exception that drops the call.
//
// And it carries nothing diagnostic. A provider name, a status code or a URL in
// the model's context is a thing a model can read down the phone to a caller.
func TestKnowledgeLookupFailureNeverEndsTheCall(t *testing.T) {
	py := emittedKnowledgePy(t, ir.ProviderLiveKit)
	if !strings.Contains(py, `{"error": "lookup unavailable"}`) {
		t.Error("a failed lookup must return the contract's error shape")
	}
	_, lookUp, _ := strings.Cut(pythonCodeOnly(py), "async def look_up(")
	for _, forbidden := range []string{"raise ", "status_code", "response.text", "shutdown()"} {
		if strings.Contains(lookUp, forbidden) {
			t.Errorf("the lookup path must not contain %q: a lookup failure continues the call and tells the model nothing diagnostic", forbidden)
		}
	}
}

// TestKnowledgeLogsNoCallContent holds FR-028: neither document text nor a
// caller's question reaches a log line that is on by default.
//
// A caller's own words are call content. An exception handler that helpfully
// logged the query would put every question anyone ever asked into whatever
// aggregates the container's stderr.
func TestKnowledgeLogsNoCallContent(t *testing.T) {
	py := emittedKnowledgePy(t, ir.ProviderLiveKit)
	for _, line := range strings.Split(py, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "log.") {
			continue
		}
		for _, forbidden := range []string{"query", "document.text", "get_content()", "node.node"} {
			if strings.Contains(trimmed, forbidden) {
				t.Errorf("log line must not carry call content (%s): %s", forbidden, trimmed)
			}
		}
	}
}

// TestKnowledgeRelevanceInstructionIsAppendedNotSubstituted holds FR-019 in both
// directions: the author's description survives, and the instruction is there
// whether or not they wrote one.
//
// Appended, not substituted, on the same reasoning the end_call prebuilt uses:
// the author's text is the part that says what is in the folder, and the fixed
// part is the part that says what to do when nothing matches. Losing either one
// costs an answer.
func TestKnowledgeRelevanceInstructionIsAppendedNotSubstituted(t *testing.T) {
	const authored = "Look up the salon's refund and complaints policy."
	for _, tc := range []struct{ name, description string }{
		{"author wrote one", authored},
		{"author wrote none", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := knowledgeAgent(t)
			tool := agent.Tools["lookup_customer"]
			tool.Description = tc.description
			agent.Tools["lookup_customer"] = tool
			// A description is required at validation, so the empty case is
			// checked at the lowering rather than through Generate.
			got := knowledgeDescription(tc.description)
			if !strings.Contains(got, "say you do not have that information") {
				t.Errorf("the relevance instruction is missing: %q", got)
			}
			if tc.description == "" {
				if strings.HasPrefix(got, "\n") {
					t.Errorf("no description must not leave a leading blank line: %q", got)
				}
				return
			}
			if !strings.HasPrefix(got, authored) {
				t.Errorf("the author's description must come first and survive intact: %q", got)
			}
			artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			agentPy := artifactFile(t, artifact, "agent.py")
			for _, want := range []string{authored, "say you do not have that information"} {
				if !strings.Contains(agentPy, want) {
					t.Errorf("agent.py must carry %q", want)
				}
			}
		})
	}
}

// TestKnowledgeToolShapeIsFixed: one string parameter, named query, and the
// author never writes a schema for it.
//
// "Use the caller's own words" is in the parameter description on purpose. The
// exact-term half of the search matches rare words from the query, so a model
// that paraphrases "balayage" into "hair colouring" throws away the term that
// would have matched.
func TestKnowledgeToolShapeIsFixed(t *testing.T) {
	agent := knowledgeAgent(t)
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	agentPy := artifactFile(t, artifact, "agent.py")
	if !strings.Contains(agentPy, `async def lookup_customer(self, ctx: RunContext, query: Annotated[str, Field(description="What to look up. Use the caller's own words.")]) -> dict:`) {
		t.Errorf("the knowledge tool signature is not the fixed one-string shape:\n%s", agentPy)
	}
	if !strings.Contains(agentPy, `return await knowledge.look_up("refunds", query)`) {
		t.Error("the tool must delegate to the knowledge module, naming its own base")
	}
	// Not query_engine. aquery() runs a second LLM call inside the tool to write
	// a paraphrase the agent is about to rewrite, which spends the whole latency
	// budget twice. This is the one place the LiveKit example's recommended
	// variant is deliberately not followed.
	py := artifactFile(t, artifact, "knowledge.py")
	for _, forbidden := range []string{"as_query_engine", "aquery(", "query_engine"} {
		if strings.Contains(py, forbidden) {
			t.Errorf("knowledge.py must not use %q: a retriever returns passages, a query engine spends a second LLM call writing prose", forbidden)
		}
	}
	if !strings.Contains(py, "as_retriever(similarity_top_k=top_k)") {
		t.Error("the lookup must use a retriever with the base's own result count")
	}
}

// TestKnowledgeEmittedOnlyWhenDeclared: a package with no knowledge: section
// gets no knowledge.py and no llama-index.
//
// This matters more than it looks. The stack measures about 178 MB installed, and
// Pipecat Cloud's warm-up time varies with image size, so every package without a
// knowledge base would pay a cold-start cost for a feature it never asked for.
func TestKnowledgeEmittedOnlyWhenDeclared(t *testing.T) {
	agent := knowledgeAgent(t)
	withKB, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.Contains(artifactFile(t, withKB, "pyproject.toml"), "llama-index-core") {
		t.Error("a package with a knowledge base must declare llama-index-core")
	}

	plain := authAgent(t, nil)
	without, err := Generate(plain, targetByProvider(t, plain, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, file := range without.Files {
		if file.Path == "knowledge.py" || strings.HasPrefix(file.Path, "knowledge/") {
			t.Errorf("a package with no knowledge: section emitted %s", file.Path)
		}
	}
	pyproject := artifactFile(t, without, "pyproject.toml")
	for _, forbidden := range []string{"llama-index", "chromadb"} {
		if strings.Contains(pyproject, forbidden) {
			t.Errorf("a package with no knowledge base must not declare %q", forbidden)
		}
	}
	agentPy := artifactFile(t, without, "agent.py")
	for _, forbidden := range []string{"import knowledge", "build_indexes"} {
		if strings.Contains(agentPy, forbidden) {
			t.Errorf("a package with no knowledge base must not reference %q", forbidden)
		}
	}
}

// TestKnowledgeScoreDirectionIsRight pins the one sentence in this feature that
// was measured wrong and shipped backwards in an earlier draft.
//
// A raw vector store deals in distance: lower is closer. The value LlamaIndex puts
// on a NodeWithScore is a similarity: higher is closer. Re-measured 2026-08-26
// against SimpleVectorStore on the salon corpus, answerable questions scored 0.211
// to 0.623 and off-topic ones 0.046 to 0.238, always ordered best first.
//
// An instruction with the direction inverted does not fail loudly. It tells the
// model to prefer the worst passage it was handed, on every call, and the only
// symptom is worse answers.
func TestKnowledgeScoreDirectionIsRight(t *testing.T) {
	got := knowledgeDescription("Look something up.")
	if !strings.Contains(got, "higher number means a closer match") {
		t.Errorf("the score direction must say higher is closer: %q", got)
	}
	if strings.Contains(got, "lower is closer") {
		t.Error("the instruction says lower is closer, which is the raw-distance direction, not the similarity LlamaIndex returns")
	}
	if !strings.Contains(got, "ordered best first") {
		t.Error("the model should be told the ordering, which is the part that cannot invert")
	}
	// And no threshold, in either direction. Filtering by score was measured and
	// rejected; a cutoff added later has to change this test on purpose.
	py := emittedKnowledgePy(t, ir.ProviderLiveKit)
	for _, forbidden := range []string{"score >", "score <", "MIN_SCORE", "THRESHOLD"} {
		if strings.Contains(py, forbidden) {
			t.Errorf("knowledge.py filters by score (%q): nothing is filtered by score", forbidden)
		}
	}
}

// TestKnowledgeMergeIsInterleaved holds the finding that saved the exact-term
// half from deletion.
//
// Measured 2026-08-25 on 15 questions written to defeat meaning-based search:
// dense alone 14/15, exact alone 13/15, dense-then-exact 14/15, interleaved
// 15/15, identical across three runs. Dense-then-exact adds nothing for an
// arithmetic reason: the dense retriever returns TOP_K results every time, so it
// fills every slot before the exact half is read, and the truncation then throws
// the exact half away whole.
//
// So this gate is not style. Reverting to concatenation silently returns the
// feature to 14/15 and makes Chroma's 250 MB buy nothing.
func TestKnowledgeMergeIsInterleaved(t *testing.T) {
	py := emittedKnowledgePy(t, ir.ProviderLiveKit)
	merge, _, ok := strings.Cut(py, "async def look_up(")
	if !ok {
		t.Fatal("no look_up in the emitted module")
	}
	_, merge, ok = strings.Cut(merge, "def _merge(")
	if !ok {
		t.Fatal("no _merge in the emitted module")
	}
	if !strings.Contains(merge, "for pair in zip(meaning") {
		t.Error("_merge must interleave the two halves; concatenating them returns 14/15 and wastes the keyword half entirely")
	}
	if strings.Contains(merge, "meaning + keyword") {
		t.Error("_merge concatenates instead of interleaving: measured 14/15 against 15/15")
	}
	// The keyword half is BM25 over the nodes, which needs nothing from the store.
	// It replaced a hand-rolled case-insensitive regex scan over Chroma's
	// where_document, and beat it on every question set measured: 15/15 against
	// 15/15 on the adversarial set but 10/10 against 9/10 on the normal one, and
	// 8/8 against 8/8 at scale while being a ranked retriever rather than a
	// longest-word-first scan.
	if !strings.Contains(py, "BM25Retriever.from_defaults(") {
		t.Error("the keyword half must be BM25 over the nodes")
	}
	if strings.Contains(pythonCodeOnly(py), "where_document") {
		t.Error("the regex scan over where_document is gone; BM25 replaced it")
	}
}

// TestKnowledgeStripsDocumentMetadata holds a startup crash that depends on how
// long the deployment's working directory happens to be.
//
// SentenceSplitter subtracts the metadata's token count from chunk_size, and
// SimpleDirectoryReader attaches the document's absolute path, so the metadata
// measured 343 characters. At CHUNK_SIZE = 90 tokens that raises ValueError
// before the first call. Measured 2026-08-25: it crashed when run from a
// checkout path and survived from /app, which is the worst possible shape for a
// bug, because CI and the container disagree.
//
// Keeping only file_name also stops the author's folder layout being embedded
// into every vector.
func TestKnowledgeStripsDocumentMetadata(t *testing.T) {
	py := emittedKnowledgePy(t, ir.ProviderLiveKit)
	if !strings.Contains(py, `document.metadata = {"file_name": source}`) {
		t.Error("documents must carry only file_name: the reader's absolute path is longer than CHUNK_SIZE and raises at startup")
	}
}

// TestKnowledgeCatchesAPDFReadAsBytes holds the failure mode that certifies
// itself as healthy.
//
// Without llama-index-readers-file installed, SimpleDirectoryReader reads a PDF
// as plain text and returns its own raw bytes, and raises nothing. Measured
// 2026-08-25 in a venv missing that package. The document is not empty, so every
// emptiness check passes, and the agent indexes PDF stream gibberish and answers
// from it with a confident voice.
//
// It is checked by content because there is nothing else to check it by.
func TestKnowledgeCatchesAPDFReadAsBytes(t *testing.T) {
	py := emittedKnowledgePy(t, ir.ProviderLiveKit)
	if !strings.Contains(py, "_PDF_MAGIC") {
		t.Error("the emitted module must detect a PDF read as raw bytes")
	}
	if !strings.Contains(py, "was read as raw PDF bytes, not as text") {
		t.Error("the failure must say what happened, because the symptom is otherwise a healthy-looking agent that knows nonsense")
	}
	// And the dependency that prevents it in the first place must be declared.
	agent := knowledgeAgent(t)
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	pyproject := artifactFile(t, artifact, "pyproject.toml")
	for _, dep := range []string{"llama-index-readers-file", "llama-index-core", "llama-index-retrievers-bm25", "llama-index-embeddings-openai"} {
		if !strings.Contains(pyproject, dep) {
			t.Errorf("pyproject must declare %q", dep)
		}
	}
	// And no vector-store package, on any mode. The in-memory store is part of
	// llama-index-core, so anything else here is 255 MB of transitive dependency
	// for a store that holds vectors and nothing else.
	for _, gone := range []string{"chromadb", "llama-index-vector-stores"} {
		if strings.Contains(pyproject, gone) {
			t.Errorf("pyproject must not declare %q: VectorStoreIndex with no storage context uses llama-index-core's own in-memory store", gone)
		}
	}
}

// TestKnowledgeIndexesInOneCallWithNoCeiling holds the reason a batching loop is
// not needed, so that nobody reintroduces one or the store that required it.
//
// The Chroma store this replaced refused more than get_max_batch_size() records
// per add, measured at 5461, while ChromaVectorStore chunked at its own
// MAX_CHUNK_SIZE of 41665, which is 7.6 times too coarse. That combination needed
// a hand-written loop over the nodes. Two things measured 2026-08-26 retired it:
// SimpleVectorStore took 6000 nodes in a single insert, and the Chroma ceiling
// turned out to be enforced only on the FastAPI client path anyway, so the loop
// was guarding a limit that moved between versions.
//
// A ceiling written as a literal is the failure mode to stay away from, whichever
// store is underneath.
func TestKnowledgeIndexesInOneCallWithNoCeiling(t *testing.T) {
	code := pythonCodeOnly(emittedKnowledgePy(t, ir.ProviderLiveKit))
	if strings.Contains(code, "get_max_batch_size") {
		t.Error("nothing should read a client batch size: the in-memory store has no such ceiling")
	}
	if strings.Contains(code, "for start in range(0, len(nodes), batch):") {
		t.Error("the batching loop existed for Chroma's 5461-record ceiling and should have gone with it")
	}
	if !strings.Contains(code, "VectorStoreIndex(") {
		t.Error("the vector half must build VectorStoreIndex in one call")
	}
	for _, forbidden := range []string{"5461", "41665"} {
		if strings.Contains(code, forbidden) {
			t.Errorf("batch ceiling %s is hardcoded in emitted code", forbidden)
		}
	}
}

// TestKnowledgeStripsLayoutArtefacts holds a defect that is invisible in every
// test that reads text and obvious to anyone listening.
//
// A PDF draws a bullet as a glyph. Extraction turns glyphs with no Unicode mapping
// into control characters, and the salon documents produce 47 DEL bytes (0x7f), one
// per list item. Nothing raises, every emptiness check passes, and the characters
// travel into the passage, the embedding and the model's context verbatim.
//
// Cleaning has to happen before splitting, or the text that was embedded and the
// text the model is shown stop being the same string.
func TestKnowledgeStripsLayoutArtefacts(t *testing.T) {
	py := emittedKnowledgePy(t, ir.ProviderLiveKit)
	code := pythonCodeOnly(py)
	if !strings.Contains(code, "def _clean(") {
		t.Fatal("the emitted module must clean document text")
	}
	// Applied to the document, not to a result: the embedding and the display must
	// come from one string.
	if !strings.Contains(code, "document.set_content(_clean(text))") {
		t.Error("cleaning must be applied to the document before splitting, so the embedded and displayed text agree")
	}
	build, lookUp, ok := strings.Cut(code, "async def look_up(")
	if !ok {
		t.Fatal("no look_up in the emitted module")
	}
	if !strings.Contains(build, "_clean(") {
		t.Error("cleaning belongs on the startup path")
	}
	if strings.Contains(lookUp, "_clean(") {
		t.Error("cleaning at lookup time would show the model text that was never embedded")
	}
	// Newlines and tabs survive: the splitter needs them to find boundaries.
	for _, kept := range []string{`\\x09`, `\\x0a`, `\\x0d`} {
		if strings.Contains(code, kept) {
			t.Errorf("the character class must not strip %s: the splitter needs it", kept)
		}
	}
}

// TestKnowledgeDeclaresEachDependencyOnce holds a defect that only shows on the
// target whose dependency list is a slice.
//
// Two knowledge bases on one embedding service is the ordinary case, and the
// embeddings package used to be appended once per base. LiveKit collects packages
// into a map, so it deduplicated by accident and the bug was invisible there;
// Pipecat writes the list it is given, so `llama-index-embeddings-openai` appeared
// twice in the emitted pyproject.toml. Both targets are checked here for that
// reason: one of them would have passed on its own.
func TestKnowledgeDeclaresEachDependencyOnce(t *testing.T) {
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		t.Run(string(provider), func(t *testing.T) {
			agent := salonAgent(t)
			artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			counts := map[string]int{}
			for _, line := range strings.Split(artifactFile(t, artifact, "pyproject.toml"), "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, `"`) && strings.HasSuffix(trimmed, `",`) {
					counts[trimmed]++
				}
			}
			for dep, n := range counts {
				if n > 1 {
					t.Errorf("dependency %s declared %d times", dep, n)
				}
			}
		})
	}
}

// TestKnowledgeOverridesPystemmerWhenBM25IsUsed holds a build failure that only
// appears on arm64, which means it only appears on a developer's machine and never
// in x86_64 CI.
//
// llama-index-retrievers-bm25 declares pystemmer<3. PyStemmer 2.2.0.3 publishes an
// x86_64 manylinux wheel and no aarch64 one, so on Apple Silicon the resolver takes
// the source distribution, and neither base image carries a compiler. The image
// build fails on `stdlib.h: No such file or directory`, which reads like a broken
// Dockerfile and is really a wheel that does not exist.
//
// The override is only correct where BM25 is actually installed, so a meaning-only
// package must not carry it: an override for an absent dependency is a claim about
// a resolution that never happens.
func TestKnowledgeOverridesPystemmerWhenBM25IsUsed(t *testing.T) {
	for _, tc := range []struct {
		mode ir.KnowledgeMode
		want bool
	}{
		{ir.KnowledgeHybrid, true},
		{ir.KnowledgeKeyword, true},
		{ir.KnowledgeMeaning, false},
	} {
		t.Run(string(tc.mode), func(t *testing.T) {
			for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
				agent := knowledgeAgent(t)
				base := agent.Knowledge["refunds"]
				base.Mode = tc.mode
				agent.Knowledge["refunds"] = base
				artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
				if err != nil {
					t.Fatalf("%s: generate: %v", provider, err)
				}
				pyproject := artifactFile(t, artifact, "pyproject.toml")
				got := strings.Contains(pyproject, "override-dependencies")
				if got != tc.want {
					t.Errorf("%s mode %q: override-dependencies present=%v, want %v",
						provider, tc.mode, got, tc.want)
				}
				if !tc.want {
					continue
				}
				// The override must not be silent about why it is there, and it must
				// stay in one [tool.uv] table rather than opening a second one.
				// Not the exact sentence, which will be reworded: the fact that
				// makes the override necessary, which will not be.
				if !strings.Contains(pyproject, "aarch64") {
					t.Errorf("%s mode %q: the override must say why it exists", provider, tc.mode)
				}
				if n := strings.Count(pyproject, "[tool.uv]"); n != 1 {
					t.Errorf("%s mode %q: %d [tool.uv] tables, want exactly 1", provider, tc.mode, n)
				}
			}
		})
	}
}

// TestKnowledgeInstallsWithoutHardlinks holds a startup failure that the image
// build cannot show, because the build succeeds and the indexing dies afterwards.
//
// uv installs by hardlinking from its cache (st_nlink=2). NLTK's path-hardening
// module refuses to open a multiply-linked file, and llama-index-core reads its
// bundled stopword list through NLTK, so every worker process dies in prewarm with
// a PermissionError that blames neither uv nor hardlinks. --link-mode=copy is the
// fix, and it has to be on both targets: they use different install invocations.
func TestKnowledgeInstallsWithoutHardlinks(t *testing.T) {
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		t.Run(string(provider), func(t *testing.T) {
			agent := knowledgeAgent(t)
			artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			dockerfile := artifactFile(t, artifact, "Dockerfile")
			for _, line := range strings.Split(dockerfile, "\n") {
				trimmed := strings.TrimSpace(line)
				if !strings.HasPrefix(trimmed, "RUN uv pip install") {
					continue
				}
				if !strings.Contains(trimmed, "--link-mode=copy") {
					t.Errorf("%s: uv install without --link-mode=copy, so NLTK refuses the hardlinked stopword list at startup: %s", provider, trimmed)
				}
			}
			if !strings.Contains(dockerfile, "st_nlink") {
				t.Errorf("%s: the Dockerfile must record why the link mode is set, or it reads as a preference", provider)
			}
		})
	}
}

// TestEveryEmbeddingServiceEmitsAValidConstruction: naming a different service
// changes the import, the class, the keyword and the credential check, and
// nothing else.
//
// The keyword is the part worth a test. The four classes do not agree —
// OpenAIEmbedding takes `model`, the other three take `model_name` — and passing
// the wrong one raises a TypeError at startup, on a path no Go test executes and
// no golden file would notice.
func TestEveryEmbeddingServiceEmitsAValidConstruction(t *testing.T) {
	for _, name := range target.EmbeddingServiceNames() {
		service, _ := target.LookupEmbeddingService(name)
		t.Run(name, func(t *testing.T) {
			agent := knowledgeAgent(t)
			base := agent.Knowledge["refunds"]
			base.Embed = name
			agent.Knowledge["refunds"] = base
			if service.CredentialEnv != "" {
				agent.Secrets = append(agent.Secrets, service.CredentialEnv)
			}
			artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
			if err != nil {
				t.Fatalf("a package naming embed %q must compile: %v", name, err)
			}
			py := artifactFile(t, artifact, "knowledge.py")
			for _, want := range []string{
				"from " + service.PythonModule + " import " + service.PythonClass,
				service.PythonClass + "(" + service.ModelKwarg + `="` + service.Model + `")`,
			} {
				if !strings.Contains(py, want) {
					t.Errorf("knowledge.py must contain %q:\n%s", want, py)
				}
			}
			// The dependency the import needs.
			if pyproject := artifactFile(t, artifact, "pyproject.toml"); !strings.Contains(pyproject, service.PythonDep) {
				t.Errorf("pyproject must declare %q", service.PythonDep)
			}
			// The credential check, or its deliberate absence for Bedrock.
			hasCheck := strings.Contains(py, "is not set, so documents")
			if service.CredentialEnv != "" {
				if !strings.Contains(py, service.CredentialEnv+" is not set") {
					t.Errorf("knowledge.py must fail at startup naming %s", service.CredentialEnv)
				}
			} else if hasCheck {
				t.Error("bedrock must emit no credential check: it resolves through the AWS chain, and checking one variable would refuse an instance role")
			}
			// The report and .env.example name it, or the author is never told.
			if service.CredentialEnv != "" {
				if env := artifactFile(t, artifact, ".env.example"); !strings.Contains(env, service.CredentialEnv) {
					t.Errorf(".env.example must name %s", service.CredentialEnv)
				}
			}
		})
	}
}

// TestKnowledgeSettingsAreEmittedPerBase: the three retrieval settings reach the
// emitted module per base, not as one global.
//
// Per base is the point. The salon's price list wants a wider window than its
// prose policy, which is the measurement that made these authorable at all: at
// the shared default of 90 tokens the list split mid-run and an opening-hours
// passage outranked the price a caller asked for.
func TestKnowledgeSettingsAreEmittedPerBase(t *testing.T) {
	agent := salonAgent(t)
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	py := artifactFile(t, artifact, "knowledge.py")
	// The example tunes services and leaves refunds on the defaults, so this
	// asserts both halves at once: an authored value travels, and an absent one
	// resolves.
	for _, want := range []string{
		`"mode": "hybrid"`,
		`"chunk_size": 90`,
		`"chunk_size": 220`,
		`"chunk_overlap": 40`,
		`"min_score": 0`,
	} {
		if !strings.Contains(py, want) {
			t.Errorf("knowledge.py must contain %s", want)
		}
	}
	// And nothing reads a module-wide constant any more, or one base would use
	// another's settings.
	code := pythonCodeOnly(py)
	for _, forbidden := range []string{"TOP_K", "CHUNK_SIZE", "CHUNK_OVERLAP"} {
		if strings.Contains(code, forbidden) {
			t.Errorf("emitted code still reads the global %s: settings are per base", forbidden)
		}
	}
	// The splitter and the retriever both read the base's own values.
	for _, want := range []string{
		`settings = SETTINGS[name]`,
		`chunk_size=settings["chunk_size"], chunk_overlap=settings["chunk_overlap"]`,
		`settings = SETTINGS[name]`,
		`top_k = settings["top_k"]`,
	} {
		if !strings.Contains(py, want) {
			t.Errorf("knowledge.py must contain %q", want)
		}
	}
}

// TestKnowledgeHidesFileNameFromTheEmbedding holds what made chunk_size safe to
// expose at all.
//
// SentenceSplitter subtracts the metadata's token count from chunk_size, so with
// the file name visible to the embedding, a chunk_size the compiler accepted could
// still raise at startup — and whether it did depended on how long the file names
// in the folder happened to be. `chunk_size: 16` worked for a short name and
// crashed for an 83-character one. Excluded from embed and llm metadata, the
// metadata length is 0 and any accepted chunk_size actually runs, while the file
// name stays readable for citations. Measured 2026-08-26.
func TestKnowledgeHidesFileNameFromTheEmbedding(t *testing.T) {
	py := emittedKnowledgePy(t, ir.ProviderLiveKit)
	for _, want := range []string{
		`document.excluded_embed_metadata_keys = ["file_name"]`,
		`document.excluded_llm_metadata_keys = ["file_name"]`,
		// Still present for the citation.
		`document.metadata = {"file_name": source}`,
	} {
		if !strings.Contains(py, want) {
			t.Errorf("knowledge.py must contain %q: without it an author's chunk_size depends on their file names", want)
		}
	}
}

// TestKnowledgeMinScoreFiltersOnlyScoredResults holds the one subtlety in the
// relevance cutoff.
//
// An exact-term-only hit carries no score, so there is nothing to compare it
// against, and it survives any cutoff. That is deliberate rather than an
// oversight: the passage was found because the caller said a rare word verbatim,
// which is stronger evidence than a similarity number, and filtering it would
// delete the exact-term half's entire contribution — the half measured at 15/15
// against 14/15.
//
// The drop is also logged. A cutoff set too high otherwise makes the agent say it
// does not know on every question with nothing anywhere to explain why, and the
// count is the only thing that makes it diagnosable. The caller's question stays
// out of the log.
func TestKnowledgeMinScoreFiltersOnlyScoredResults(t *testing.T) {
	py := emittedKnowledgePy(t, ir.ProviderLiveKit)
	if !strings.Contains(py, `if cutoff and score is not None and float(score) < cutoff:`) {
		t.Error("the filter must skip unscored results: an exact-only hit has no score to compare")
	}
	if !strings.Contains(py, `"knowledge %r: dropped %d result(s) below min_score %s"`) {
		t.Error("a dropped result must be logged, or a too-high cutoff is undiagnosable")
	}
	// The count, never the question.
	for _, line := range strings.Split(py, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "log.") && strings.Contains(trimmed, "min_score") && strings.Contains(trimmed, "query") {
			t.Errorf("the cutoff log line carries the caller's question: %s", trimmed)
		}
	}
	// Absent by default, so the measured 22/22 behaviour is what a package gets
	// without asking for anything.
	if !strings.Contains(py, `"min_score": 0`) {
		t.Error("min_score must default to 0, which filters nothing")
	}
}

// TestKnowledgeLogsThatRetrievalHappened holds the only run-time evidence that a
// lookup ran at all.
//
// Without it a successful lookup is silent, and the only signal available on a
// live call is whether the agent's answer sounds correct, which is the very thing
// under test. A count and a duration make it observable; the caller's question and
// the retrieved passages must stay out, exactly as on the failure path.
func TestKnowledgeLogsThatRetrievalHappened(t *testing.T) {
	py := emittedKnowledgePy(t, ir.ProviderLiveKit)
	if !strings.Contains(py, `"knowledge %r: returned %d result(s) in %d ms"`) {
		t.Error("a successful lookup must log its result count, or retrieval is invisible at run time")
	}
	// Every log line in the module, checked for call content. The question is a
	// local named `query`; a passage reaches a result as `text`.
	for _, line := range strings.Split(py, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "log.") {
			continue
		}
		for _, leak := range []string{"query", "result[", `results[`, "get_content"} {
			if strings.Contains(trimmed, leak) {
				t.Errorf("log line carries call content (%s): %s", leak, trimmed)
			}
		}
	}
}

// TestKnowledgeModeEmitsOnlyWhatItNeeds holds the three modes' emitted shape.
//
// It exists because two real bugs hid here, both caught by running ruff over the
// emitted file rather than by any Go test: a keyword-only base still emitted an
// `_embed_<name>` function referencing an `os` import and an embedder class the
// module no longer imported, and a refactor deleted `_merge` while leaving its
// call site. Neither is visible to a golden diff, and `ruff check .` in CI only
// covers checked-in Python, not generated output.
//
// So this asserts the shape by name: every symbol the emitted code references
// must be one the mode also defines.
func TestKnowledgeModeEmitsOnlyWhatItNeeds(t *testing.T) {
	for _, tc := range []struct {
		mode      ir.KnowledgeMode
		wants     []string
		forbidden []string
	}{
		{
			mode:  ir.KnowledgeKeyword,
			wants: []string{"BM25Retriever.from_defaults(", "def _merge("},
			// No embedding at all: no import, no store, no credential check, and
			// no per-base embedder function to reference them.
			forbidden: []string{
				"VectorStoreIndex", "_embed_refunds", "OpenAIEmbedding", "import os",
			},
		},
		{
			mode:  ir.KnowledgeMeaning,
			wants: []string{"VectorStoreIndex(", "_embed_refunds(", "def _merge("},
			// The keyword half is what a meaning-only base must not build.
			forbidden: []string{"BM25Retriever"},
		},
		{
			mode:  ir.KnowledgeHybrid,
			wants: []string{"VectorStoreIndex(", "BM25Retriever.from_defaults(", "_embed_refunds(", "def _merge("},
		},
	} {
		t.Run(string(tc.mode), func(t *testing.T) {
			agent := knowledgeAgent(t)
			base := agent.Knowledge["refunds"]
			base.Mode = tc.mode
			agent.Knowledge["refunds"] = base
			artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
			if err != nil {
				t.Fatalf("mode %q must compile: %v", tc.mode, err)
			}
			py := artifactFile(t, artifact, "knowledge.py")
			// Code only for both directions. The module explains each mode in
			// docstrings that name the store and BM25 whatever the mode is, so a
			// check that reads prose fails on the explanation.
			code := pythonCodeOnly(py)
			for _, want := range tc.wants {
				if !strings.Contains(code, want) {
					t.Errorf("mode %q must emit %q", tc.mode, want)
				}
			}
			for _, forbidden := range tc.forbidden {
				if strings.Contains(code, forbidden) {
					t.Errorf("mode %q must not emit %q: it references something the mode does not set up", tc.mode, forbidden)
				}
			}
			// Every call has a definition. This is the class of bug ruff caught on
			// the emitted file and no Go test saw.
			for _, symbol := range []string{"_merge", "_embed_refunds", "_index", "_read", "_with_text", "_source_name"} {
				if strings.Contains(code, symbol+"(") && !strings.Contains(code, "def "+symbol+"(") {
					t.Errorf("mode %q calls %s() with no definition", tc.mode, symbol)
				}
			}
			// A keyword-only package needs no credential, so the emitted project
			// must not demand one.
			pyproject := artifactFile(t, artifact, "pyproject.toml")
			if tc.mode == ir.KnowledgeKeyword {
				for _, forbidden := range []string{"llama-index-embeddings", "chromadb", "llama-index-vector-stores"} {
					if strings.Contains(pyproject, forbidden) {
						t.Errorf("a keyword-only base must not install %q: it embeds nothing, so it needs no embedder and no credential", forbidden)
					}
				}
			}
			if !strings.Contains(pyproject, "llama-index-retrievers-bm25") && tc.mode != ir.KnowledgeMeaning {
				t.Errorf("mode %q needs llama-index-retrievers-bm25", tc.mode)
			}
		})
	}
}

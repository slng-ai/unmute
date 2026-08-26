package generate

import (
	"bytes"
	"embed"
	"fmt"
	"maps"
	"slices"
	"text/template"

	"github.com/slng-ai/unmute/internal/ir"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// The emitted knowledge module, shared by both drivers.
//
// One file, not one per driver, because this one has no framework in it: it
// reads, splits, embeds and searches, and never mentions LiveKit or Pipecat. The
// parts that do differ — how a tool is registered, and where the index is built —
// stay in each driver's own agent.py or bot.py, where they genuinely differ.
//
// This follows templates/plane/, which is shared between the two SIP routes for
// the same stated reason: the second route "must not have a second copy of
// them". Two copies of an identical 250-line module would be two owners of one
// surface, and the drift would show up as one target answering questions better
// than the other.
//
//go:embed templates/knowledge/*.tmpl
var knowledgeTemplates embed.FS

// knowledgeBase is one knowledge base as the emitted module needs it. Every
// embedding fact comes from internal/target/embedding.go, which is the single
// owner, so a model id or credential cannot drift between the emitted project,
// the docs page and the skill.
type knowledgeBase struct {
	Name          string
	EmbedModule   string
	EmbedClass    string
	EmbedModel    string
	ModelKwarg    string
	CredentialEnv string
	EmbedDep      string
	EmbedDocs     string
	EmbedVerified string
	ChunkSize     int
	ChunkOverlap  int
	TopK          int
	MinScore      float64
	Mode          string
	// Embeds is the mode's own answer, precomputed because a Go template cannot
	// call a method on a type it does not import.
	Embeds bool
}

// pythonImport is one `from <Module> import <Class>` line.
type pythonImport struct {
	Module string
	Class  string
}

// knowledgeData is what the shared template renders from.
type knowledgeData struct {
	Knowledge        []knowledgeBase
	KnowledgeImports []pythonImport
	// AnyEmbeds is true when at least one base needs an embedding service. When it
	// is false the emitted module imports no vector store and no embedder, because
	// keyword-only retrieval needs neither.
	AnyEmbeds bool
	// AnyKeyword is true when at least one base ranks words directly. When it is
	// false the module imports no BM25 retriever and the project does not install
	// one, because meaning-only retrieval never calls it.
	AnyKeyword bool
	// Credentials is every distinct embedding credential the bases actually use,
	// sorted. The image build mounts one secret per entry so the corpus can be
	// embedded once at build time instead of once per worker process at startup.
	//
	// Empty for a keyword-only package, which embeds nothing and therefore bakes
	// its index with no secret at all.
	Credentials []string
}

// loweredKnowledge resolves every declared knowledge base, and the deduplicated
// import set across them: two bases on one service must not import the same class
// twice.
//
// Validate has already refused an unknown service, so a miss here means the IR
// was built in code. The credential is registered as read, which puts it in
// .env.example and the generated startup check by the route every other secret
// takes.
func loweredKnowledge(agent *ir.Agent, env *envSet) (knowledgeData, error) {
	if len(agent.Knowledge) == 0 {
		return knowledgeData{}, nil
	}
	var data knowledgeData
	seen := map[pythonImport]bool{}
	for _, name := range slices.Sorted(maps.Keys(agent.Knowledge)) {
		base := agent.Knowledge[name]
		service, ok := targetcap.LookupEmbeddingService(base.Embed)
		if !ok {
			return knowledgeData{}, fmt.Errorf("knowledge base %q: unknown embedding service %q", name, base.Embed)
		}
		// Only when the mode actually embeds: a keyword-only base sends no
		// embedding request, so naming its credential would be a claim about a
		// call the emitted project never makes.
		if base.Mode.Embeds() && service.CredentialEnv != "" {
			env.addRead(service.CredentialEnv)
		}
		data.Knowledge = append(data.Knowledge, knowledgeBase{
			Name: name, EmbedModule: service.PythonModule, EmbedClass: service.PythonClass,
			EmbedModel: service.Model, ModelKwarg: service.ModelKwarg,
			CredentialEnv: service.CredentialEnv,
			EmbedDep:      service.PythonDep, EmbedDocs: service.Docs, EmbedVerified: service.Verified,
			ChunkSize: base.ChunkSize, ChunkOverlap: base.ChunkOverlap, TopK: base.TopK,
			MinScore: base.MinScore,
			Mode:     string(base.Mode), Embeds: base.Mode.Embeds(),
		})
		if base.Mode.Keywords() {
			data.AnyKeyword = true
		}
		if !base.Mode.Embeds() {
			continue
		}
		data.AnyEmbeds = true
		if service.CredentialEnv != "" && !slices.Contains(data.Credentials, service.CredentialEnv) {
			data.Credentials = append(data.Credentials, service.CredentialEnv)
		}
		key := pythonImport{Module: service.PythonModule, Class: service.PythonClass}
		if !seen[key] {
			seen[key] = true
			data.KnowledgeImports = append(data.KnowledgeImports, key)
		}
	}
	return data, nil
}

// knowledgeDeps are the packages the emitted project installs for a knowledge
// base, plus one llama-index-embeddings-* per service in use.
//
// llama-index-readers-file is not optional and not obvious. Without it,
// SimpleDirectoryReader reads a .pdf as plain text and returns the raw PDF
// stream, raising nothing, and the agent indexes binary gibberish and answers
// from it (research.md R12).
func knowledgeDeps(data knowledgeData) []string {
	if len(data.Knowledge) == 0 {
		return nil
	}
	deps := []string{
		"llama-index-core",
		"llama-index-readers-file",
	}
	// BM25 for every mode but meaning. It replaced a hand-rolled regex scan it beat
	// on every question set measured, and it is what makes mode: keyword need no
	// credential at all.
	if data.AnyKeyword {
		deps = append(deps, "llama-index-retrievers-bm25")
	}
	if !data.AnyEmbeds {
		// Keyword-only: no embedder, so no embeddings package. Measured 2026-08-26,
		// this set installs 166 MB against 178 MB for a base that embeds.
		//
		// The image saving used to be a headline reason to choose keyword, and it is
		// not any more: the 255 MB it saved was chromadb's, and chromadb is gone. The
		// reasons that remain are the ones that always mattered more, no credential
		// and no network call.
		return deps
	}
	// No vector-store package. Given no storage context, VectorStoreIndex uses
	// LlamaIndex's own in-memory SimpleVectorStore, which ships inside
	// llama-index-core.
	//
	// This used to install llama-index-vector-stores-chroma and chromadb to hold
	// vectors and nothing else. Measured 2026-08-26, the swap left retrieval
	// identical (same top passage on 10 of 10 questions, same MRR) and took the
	// installed set from 433 MB to 178 MB, because chromadb pulls in 45 further
	// packages including onnxruntime, kubernetes and grpc.
	// One embeddings package per service, not one per base. Two bases on the same
	// service is the ordinary case (the salon example is exactly that), and without
	// this the package is named twice. LiveKit's builder collects into a map and
	// hides it; Pipecat's dependency list is a slice, so the duplicate reached the
	// emitted pyproject.toml as a repeated line.
	seen := map[string]bool{}
	for _, base := range data.Knowledge {
		if base.Embeds && !seen[base.EmbedDep] {
			seen[base.EmbedDep] = true
			deps = append(deps, base.EmbedDep)
		}
	}
	return deps
}

// renderKnowledgeModule renders the shared knowledge.py for either driver.
func renderKnowledgeModule(data knowledgeData) ([]byte, error) {
	raw, err := knowledgeTemplates.ReadFile("templates/knowledge/knowledge.py.tmpl")
	if err != nil {
		return nil, fmt.Errorf("knowledge template: %w", err)
	}
	tmpl, err := template.New("knowledge.py").Funcs(template.FuncMap{"pyq": pyQuote}).Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("knowledge template: %w", err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return nil, fmt.Errorf("knowledge template: %w", err)
	}
	return out.Bytes(), nil
}

// knowledgeRelevanceInstruction is appended to every knowledge tool's
// model-facing description (FR-019).
//
// The direction is measured, not assumed, and it is the opposite of what this
// string first said. A raw vector store deals in distance, where lower is closer;
// the value LlamaIndex puts on a NodeWithScore is a similarity, where higher is
// closer. Re-measured 2026-08-26 against SimpleVectorStore, on the salon corpus:
// answerable questions scored 0.211 to 0.623 and off-topic ones 0.046 to 0.238,
// always ordered best first. Telling the model "lower is closer" would have told
// it to prefer the worst result it was given.
//
// Nothing is filtered by score unless the author sets a min_score, which is absent
// by default. The separation above looks usable on one corpus of two documents,
// which is not evidence for a threshold, so the instruction below is what carries
// the decision. It says the same thing either way: a surviving result is still not
// necessarily an answer.
const knowledgeRelevanceInstruction = "The results may not answer the question. " +
	"They are ordered best first, and each carries a relevance score where a " +
	"higher number means a closer match. If nothing returned actually answers " +
	"what was asked, say you do not have that information rather than offering " +
	"the closest result."

// knowledgeQueryDescription is the one parameter's description, identical on both
// targets (FR-026).
//
// "Use the caller's own words" is not decoration: the exact-term half of the
// search matches rare words from the query, so a model that paraphrases
// "balayage" into "hair colouring" loses the term that would have matched.
const knowledgeQueryDescription = "What to look up. Use the caller's own words."

// knowledgeDescription appends the relevance instruction to the author's own
// description, rather than replacing it.
//
// The suffix is added whether or not the author wrote anything, the same way the
// end_call prebuilt puts the author's text on top of its own default instead of
// being replaced by it.
func knowledgeDescription(authored string) string {
	if authored == "" {
		return knowledgeRelevanceInstruction
	}
	return authored + "\n\n" + knowledgeRelevanceInstruction
}

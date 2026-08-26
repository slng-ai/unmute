package target

import "sort"

// EmbeddingService is one way to turn a knowledge base document into vectors.
// This table is the single owner of the facts a knowledge base needs: which
// model is pinned, which credential the author must declare, and which
// LlamaIndex package the emitted project installs. The docs-site page and the
// skill are held against it rather than repeating it.
//
// An embedding service is a model vendor, not a target. The constitution keeps
// that distinction load-bearing: adding a service here adds no provider and no
// driver.
type EmbeddingService struct {
	Name          string // the value an author writes in `embed:`
	Model         string // the embedding model id we pin
	CredentialEnv string // env var the author declares in `secrets:`; empty where a credential chain is used
	PythonDep     string // the llama-index-embeddings-* package the emitted project declares
	PythonModule  string // the module the emitted `knowledge.py` imports from
	PythonClass   string // the class it constructs
	// ModelKwarg is the constructor keyword the model id goes to. The classes do
	// not agree: OpenAIEmbedding takes `model`, and GoogleGenAIEmbedding,
	// HuggingFaceInferenceAPIEmbedding and BedrockEmbedding all take
	// `model_name`. Passing the wrong one is a TypeError at startup.
	ModelKwarg string
	Docs       string // the provider's own documentation
	Verified   string // date the model id and credential were last checked against it
}

// DefaultEmbeddingService is what `embed:` resolves to when an author omits it.
const DefaultEmbeddingService = "openai"

// embeddingServices is the closed set. Adding a service is one row here plus a
// construction arm in both knowledge.py templates; no new authoring surface.
var embeddingServices = map[string]EmbeddingService{
	"openai": {
		Name:          "openai",
		Model:         "text-embedding-3-small",
		CredentialEnv: "OPENAI_API_KEY",
		PythonDep:     "llama-index-embeddings-openai",
		PythonModule:  "llama_index.embeddings.openai",
		PythonClass:   "OpenAIEmbedding",
		ModelKwarg:    "model",
		Docs:          "https://platform.openai.com/docs/guides/embeddings",
		Verified:      "2026-08-25",
	},
	"gemini": {
		Name:  "gemini",
		Model: "gemini-embedding-2-preview",
		// GEMINI_API_KEY, not GOOGLE_API_KEY: the google-genai SDK's own docs
		// name this one for the Gemini Developer API. Verified against
		// googleapis/python-genai.
		CredentialEnv: "GEMINI_API_KEY",
		PythonDep:     "llama-index-embeddings-google-genai",
		PythonModule:  "llama_index.embeddings.google_genai",
		PythonClass:   "GoogleGenAIEmbedding",
		ModelKwarg:    "model_name",
		Docs:          "https://ai.google.dev/gemini-api/docs/embeddings",
		Verified:      "2026-08-25",
	},
	"huggingface": {
		Name: "huggingface",
		// The hosted Inference API, not a local model. A local sentence
		// transformer would put PyTorch in the image, which is gigabytes, and the
		// startup cost of loading it is the thing this feature moves off the call
		// path in the first place.
		Model:         "BAAI/bge-small-en-v1.5",
		CredentialEnv: "HF_TOKEN",
		PythonDep:     "llama-index-embeddings-huggingface-api",
		PythonModule:  "llama_index.embeddings.huggingface_api",
		PythonClass:   "HuggingFaceInferenceAPIEmbedding",
		ModelKwarg:    "model_name",
		Docs:          "https://huggingface.co/docs/api-inference/index",
		Verified:      "2026-08-25",
	},
	"bedrock": {
		Name:  "bedrock",
		Model: "cohere.embed-english-v3",
		// Empty on purpose. Bedrock authenticates through the AWS credential
		// chain, so there is no single variable to name: boto3 resolves an access
		// key pair, a profile, a role or an instance identity, and a region, in
		// its own documented order. Validation therefore requires nothing in
		// secrets: for this service, and the emitted module checks nothing at
		// startup, because there is nothing it could check that would be right
		// for every one of those paths.
		CredentialEnv: "",
		PythonDep:     "llama-index-embeddings-bedrock",
		PythonModule:  "llama_index.embeddings.bedrock",
		PythonClass:   "BedrockEmbedding",
		ModelKwarg:    "model_name",
		Docs:          "https://docs.aws.amazon.com/bedrock/latest/userguide/titan-embedding-models.html",
		Verified:      "2026-08-25",
	},
}

// LookupEmbeddingService returns the row for name, or ok=false if unknown.
func LookupEmbeddingService(name string) (EmbeddingService, bool) {
	service, ok := embeddingServices[name]
	return service, ok
}

// EmbeddingServiceNames is every supported value, sorted, so an error message
// listing them reads the same way twice.
func EmbeddingServiceNames() []string {
	names := make([]string, 0, len(embeddingServices))
	for name := range embeddingServices {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

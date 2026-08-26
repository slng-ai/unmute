package target

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestEveryEmbeddingServiceIsComplete: a row is only useful if the emitted
// project can be built from it, so every field it needs must be present and
// plausible.
//
// The constructor keyword is checked because the classes do not agree.
// OpenAIEmbedding takes `model`; GoogleGenAIEmbedding,
// HuggingFaceInferenceAPIEmbedding and BedrockEmbedding all take `model_name`.
// Passing the wrong one is a TypeError at startup, on a path no Go test executes.
func TestEveryEmbeddingServiceIsComplete(t *testing.T) {
	names := EmbeddingServiceNames()
	if len(names) < 4 {
		t.Fatalf("want the four specified services, got %v", names)
	}
	verified := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	for _, name := range names {
		service, ok := LookupEmbeddingService(name)
		if !ok {
			t.Fatalf("%s is listed but does not resolve", name)
		}
		switch service.ModelKwarg {
		case "model", "model_name":
		default:
			t.Errorf("%s: ModelKwarg = %q, want model or model_name", name, service.ModelKwarg)
		}
		if !strings.HasPrefix(service.PythonModule, "llama_index.embeddings.") {
			t.Errorf("%s: PythonModule = %q", name, service.PythonModule)
		}
		if !strings.HasSuffix(service.PythonClass, "Embedding") {
			t.Errorf("%s: PythonClass = %q, want a LlamaIndex embedding class", name, service.PythonClass)
		}
		if !strings.HasPrefix(service.Docs, "https://") {
			t.Errorf("%s: Docs = %q, want the provider's own documentation", name, service.Docs)
		}
		// A provider claim with no date cannot be audited, and CLAUDE.md requires
		// them checked against current official documentation. A date in the
		// future is a typo, not a verification.
		if !verified.MatchString(service.Verified) {
			t.Errorf("%s: Verified = %q, want YYYY-MM-DD", name, service.Verified)
		} else if when, err := time.Parse("2006-01-02", service.Verified); err == nil && when.After(time.Now().AddDate(0, 0, 1)) {
			t.Errorf("%s: Verified = %q is in the future", name, service.Verified)
		}
		// The package and the module have to be the same integration, or the
		// emitted project installs one thing and imports another.
		wantDep := "llama-index-embeddings-" + strings.ReplaceAll(strings.TrimPrefix(service.PythonModule, "llama_index.embeddings."), "_", "-")
		if service.PythonDep != wantDep {
			t.Errorf("%s: PythonDep = %q but PythonModule implies %q, so the project installs one integration and imports another",
				name, service.PythonDep, wantDep)
		}
	}
}

// TestBedrockDeclaresNoSingleCredential is a row whose empty field is the point.
//
// Bedrock authenticates through the AWS credential chain, so there is no single
// variable to require in secrets:. Filling this field with AWS_ACCESS_KEY_ID would
// refuse a package that authenticates by instance role, which is the normal way to
// run on AWS.
func TestBedrockDeclaresNoSingleCredential(t *testing.T) {
	bedrock, ok := LookupEmbeddingService("bedrock")
	if !ok {
		t.Fatal("bedrock is not in the table")
	}
	if bedrock.CredentialEnv != "" {
		t.Errorf("bedrock CredentialEnv = %q, want empty: requiring one variable would refuse a package using an instance role", bedrock.CredentialEnv)
	}
	// And every other service does name one, or nothing would check them.
	for _, name := range EmbeddingServiceNames() {
		if name == "bedrock" {
			continue
		}
		if service, _ := LookupEmbeddingService(name); service.CredentialEnv == "" {
			t.Errorf("%s names no credential, so nothing checks it is declared", name)
		}
	}
}

package generate

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// knowledgePDF is deliberately binary, including a NUL and two high bytes. A
// copy that went through a string round trip, a newline normalisation or a UTF-8
// validation would change these bytes, and a test comparing only file names
// would pass anyway.
var knowledgePDF = []byte("%PDF-1.7\n\x00\x01\x02 policy body \xff\xfe\ntrailer\n%%EOF\n")

// knowledgeAgent builds the safe_core fixture with one knowledge base and one
// tool searching it.
func knowledgeAgent(t *testing.T) *ir.Agent {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	pkg.Agent.Knowledge = map[string]spec.KnowledgeDef{
		"refunds": {Documents: "knowledge/refunds"},
	}
	pkg.Documents = map[string][]byte{
		"knowledge/refunds/policy.pdf":  knowledgePDF,
		"knowledge/refunds/addendum.md": []byte("# Addendum\n\nRedos within fourteen days.\n"),
	}
	pkg.Agent.Secrets = append(pkg.Agent.Secrets, "OPENAI_API_KEY")
	tool := pkg.Tools["lookup_customer"]
	tool.Webhook, tool.Input, tool.Output = nil, nil, nil
	tool.Knowledge = &spec.ToolKnowledge{Base: "refunds"}
	tool.Description = "Look up the salon's refund and complaints policy."
	tool.Announce = "Let me check the policy."
	pkg.Tools["lookup_customer"] = tool
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

// TestKnowledgeDocumentsCopiedVerbatim holds the artifact contract: the bytes in
// the image are the author's bytes.
//
// Byte equality, not presence. A mangled PDF produces no error at compile and no
// error at startup; it produces an agent that answers questions badly on a phone
// call, which is the hardest class of failure this repository has to debug.
func TestKnowledgeDocumentsCopiedVerbatim(t *testing.T) {
	agent := knowledgeAgent(t)
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	found := map[string][]byte{}
	for _, file := range artifact.Files {
		if strings.HasPrefix(file.Path, "knowledge/") {
			found[file.Path] = file.Content
		}
	}
	if len(found) != 2 {
		t.Fatalf("want 2 documents in the artifact, got %d: %v", len(found), found)
	}
	if got := found["knowledge/refunds/policy.pdf"]; !bytes.Equal(got, knowledgePDF) {
		t.Errorf("policy.pdf is not byte-identical to the source:\n got %q\nwant %q", got, knowledgePDF)
	}
	if got := string(found["knowledge/refunds/addendum.md"]); !strings.Contains(got, "fourteen days") {
		t.Errorf("addendum.md content = %q", got)
	}
}

// TestKnowledgeArtifactPathIsTheBaseName: the authored name is the subfolder name,
// so a base named `refunds` lands at knowledge/refunds/ and nowhere else. It is also
// the key the emitted module looks its settings up by, so the two cannot drift.
// Both targets write the same tree (FR-026).
func TestKnowledgeArtifactPathIsTheBaseName(t *testing.T) {
	agent := knowledgeAgent(t)
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var paths []string
	for _, file := range artifact.Files {
		if strings.HasPrefix(file.Path, "knowledge/") {
			paths = append(paths, file.Path)
		}
	}
	for _, path := range paths {
		if !strings.HasPrefix(path, "knowledge/refunds/") {
			t.Errorf("document at %q, want it under knowledge/refunds/", path)
		}
	}
	// Sorted, so the golden below reads the same way twice.
	if len(paths) != 2 || paths[0] != "knowledge/refunds/addendum.md" || paths[1] != "knowledge/refunds/policy.pdf" {
		t.Errorf("documents are not in sorted order: %v", paths)
	}
}

// TestKnowledgeReportLinesHaveNoPassageCount holds FR-015 in both directions: the
// line says what the compiler knows (documents, service, that content is fixed)
// and does not say what it cannot know.
//
// A passage count here would be a guess presented as a fact, because splitting
// happens at startup with a tokeniser the compiler does not run.
func TestKnowledgeReportLinesHaveNoPassageCount(t *testing.T) {
	agent := knowledgeAgent(t)
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	joined := strings.Join(artifact.Notes.Notes, "\n")
	for _, want := range []string{`knowledge "refunds"`, "2 documents", "embed openai", "fixed until next compile"} {
		if !strings.Contains(joined, want) {
			t.Errorf("report must state %q, got:\n%s", want, joined)
		}
	}
	for _, forbidden := range []string{"passage", "chunk", "node"} {
		if strings.Contains(strings.ToLower(joined), forbidden) {
			t.Errorf("report must not mention %q: splitting happens at startup, so any count here is a guess:\n%s", forbidden, joined)
		}
	}
}

// TestKnowledgeReportSingularDocument: one document reads "1 document", not
// "1 documents". A report an author cannot read carefully is a report they stop
// reading.
func TestKnowledgeReportSingularDocument(t *testing.T) {
	agent := knowledgeAgent(t)
	delete(agent.Documents, "knowledge/refunds/addendum.md")
	base := agent.Knowledge["refunds"]
	base.Files = []string{"policy.pdf"}
	agent.Knowledge["refunds"] = base
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	joined := strings.Join(artifact.Notes.Notes, "\n")
	if !strings.Contains(joined, "1 document,") {
		t.Errorf("want a singular document count, got:\n%s", joined)
	}
}

package skill

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The documentation site serves every page as Markdown and publishes two
// indexes. Mintlify does that on its own; what this repository owns is whether
// anybody is told, and whether the site declares the menu that exposes it.
//
// Both halves rot the same quiet way. Dropping "copy" from docs.json removes a
// button nobody notices is gone, and deleting a line from the coding agents
// page removes the only place a developer learns the endpoints exist. So each
// half gets a check, and each failure names the file to change.
//
// This lives next to coding_agents_docsite_test.go because that page is the
// surface being held, and this package already owns what a coding assistant
// reads.

// contextualOptions is the menu, in the order it renders. Mintlify draws the
// options in the order they are listed, so this comparison is ordered on
// purpose: copy first because it is the common case, then view, then the two
// chat handoffs.
var contextualOptions = []string{"copy", "view", "chatgpt", "claude"}

// markdownEndpoints is what the coding agents page has to name. The suffix is
// listed as a page that really exists, so mint broken-links checks it too.
var markdownEndpoints = []string{
	"https://unmute.mintlify.app/llms.txt",
	"https://unmute.mintlify.app/llms-full.txt",
	"https://unmute.mintlify.app/start/coding-agents.md",
}

func siteConfig(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs-site", "docs.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	return config
}

// TestContextualMenuIsDeclared holds the menu. Mintlify renders nothing unless
// docs.json asks for it, so the absence of this key is the absence of the
// feature.
func TestContextualMenuIsDeclared(t *testing.T) {
	config := siteConfig(t)

	raw, ok := config["contextual"]
	if !ok {
		t.Fatal("docs-site/docs.json has no `contextual` key, so no page offers to copy itself as Markdown or hand itself to an assistant")
	}
	var contextual struct {
		Options []string `json:"options"`
	}
	if err := json.Unmarshal(raw, &contextual); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(contextual.Options, contextualOptions) {
		t.Errorf("docs-site/docs.json contextual.options = %v, want %v; Mintlify renders them in the order listed, so order is part of the contract", contextual.Options, contextualOptions)
	}
}

// TestAgentInstructionsNameTheTwoFactsAgentsGetWrong holds the instructions
// block. It reaches every page's Markdown export and both indexes, which makes
// it the highest-leverage sentence on the site for an agent, and the easiest to
// quietly empty out.
//
// The two facts are the ones the rest of this package already defends: the
// target set, and who owns the schema. A model that gets either wrong writes a
// package that does not validate.
func TestAgentInstructionsNameTheTwoFactsAgentsGetWrong(t *testing.T) {
	config := siteConfig(t)

	raw, ok := config["markdown"]
	if !ok {
		t.Fatal("docs-site/docs.json has no `markdown` key, so llms.txt and every page export carry no agent instructions")
	}
	var section struct {
		Instructions []string `json:"instructions"`
	}
	if err := json.Unmarshal(raw, &section); err != nil {
		t.Fatal(err)
	}
	if len(section.Instructions) == 0 {
		t.Fatal("docs-site/docs.json markdown.instructions is empty; it is the one block every page export and both indexes carry")
	}
	joined := strings.ToLower(strings.Join(section.Instructions, "\n"))

	// The target set. Vapi and Deepgram were retired as targets on 2026-08-24
	// and Deepgram remains a model vendor, so the instructions have to say how
	// many there are and name each, not merely mention a target. slng joined on
	// 2026-08-25 and is the reason the count moved: it is a target and a model
	// vendor at once, which is the confusion this block exists to prevent.
	for _, want := range []string{"pipecat", "livekit agents", "slng", "three targets"} {
		if !strings.Contains(joined, want) {
			t.Errorf("docs-site/docs.json markdown.instructions never says %q; an agent that does not know the target set invents a provider name", want)
		}
	}
	// The schema owner. internal/spec and internal/ir own it, and `unmute
	// validate` is the final authority.
	for _, want := range []string{"internal/spec", "internal/ir", "unmute validate"} {
		if !strings.Contains(joined, want) {
			t.Errorf("docs-site/docs.json markdown.instructions never names %q; model memory of this schema is the biggest source of confident wrong answers", want)
		}
	}
}

// TestCodingAgentsPageNamesTheMarkdownEndpoints holds the telling. The
// capability existed and shipped undocumented, which is the same as not having
// it, so the page that a reader lands on for assistant setup has to name all
// three.
func TestCodingAgentsPageNamesTheMarkdownEndpoints(t *testing.T) {
	page := codingAgents(t)

	for _, endpoint := range markdownEndpoints {
		if !strings.Contains(page, endpoint) {
			t.Errorf("%s does not name %s; the endpoint works whether or not the page says so, and an endpoint nobody is told about is one nobody uses", codingAgentsPage, endpoint)
		}
	}
	// The suffix rule, not just the one example URL. A reader has to learn that
	// it applies to any page, otherwise they only ever fetch the example.
	if !strings.Contains(page, "add `.md` to any page URL") {
		t.Errorf("%s names an example .md URL but never states the rule; a reader needs to know the suffix works on every page, not just that one", codingAgentsPage)
	}
}

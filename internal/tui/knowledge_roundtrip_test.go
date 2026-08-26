package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/scaffold"
)

// TestMaintainLosesNothingFromAKnowledgePackage guards silent data loss.
//
// `unmute maintain` rewrites agent.yaml from scaffold.Data, so a section that
// struct does not carry is a section the console deletes. The console offers no
// editor for `knowledge:` — the folder is a path on disk it cannot check and the
// author already knows — but it still has to read and write it, or opening the
// console on a package with knowledge bases and changing anything at all would
// throw them away.
//
// Nothing would say so. The package would still compile; its agents would just
// stop being able to quote their documents.
//
// This asserts against the console's own round-trip check rather than driving the
// save menu, because that check is what actually decides whether a field survives:
// loadMaintained runs it and warns the author about every field it finds. An empty
// result is the strongest available statement that nothing is lost.
func TestMaintainLosesNothingFromAKnowledgePackage(t *testing.T) {
	root := filepath.Join(t.TempDir(), "agent")
	data := scaffold.Data{Name: "agent", Instructions: "You are the salon concierge. Be brief.\n"}
	data.SetTarget("livekit")
	data.Knowledge = []scaffold.KnowledgeBase{
		{Name: "refunds", Documents: "knowledge/refunds"},
		// A non-default service, because the console silently moving a package
		// back to the default would be its own quiet defect.
		{Name: "services", Documents: "knowledge/services", Embed: "gemini"},
	}
	data.Tools = append(data.Tools, scaffold.Tool{
		Name: "look_up_refund_policy", Description: "Look up the refund policy.",
		Execution: "knowledge", KnowledgeBase: "refunds", AttachTo: []string{"assistant"},
	})
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	// The folders have to exist, because spec.Load lists them.
	for _, base := range data.Knowledge {
		if err := os.MkdirAll(filepath.Join(root, base.Documents), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, base.Documents, "policy.md"), []byte("# Policy\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	// Scoped to what this feature owns. A freshly scaffolded package always
	// reports `instructions.md: content`, with or without a knowledge section —
	// verified by running this same check on a package that has none — so that
	// entry is a pre-existing quirk of the scaffold prompt and not something to
	// assert away here.
	for _, loss := range agent.losses {
		if strings.Contains(loss, "knowledge") || strings.Contains(loss, "embed") {
			t.Errorf("the console cannot preserve %q, so maintain would delete it", loss)
		}
	}

	// And the section is read back into the struct, which is what makes the
	// rewrite carry it.
	got := map[string]scaffold.KnowledgeBase{}
	for _, base := range agent.data.Knowledge {
		got[base.Name] = base
	}
	if len(got) != 2 {
		t.Fatalf("knowledge: was not read into scaffold.Data: %#v", agent.data.Knowledge)
	}
	if base := got["refunds"]; base.Documents != "knowledge/refunds" || base.Embed != "" {
		t.Errorf("refunds = %#v, want the folder and no embed (the default is applied at Build)", base)
	}
	if base := got["services"]; base.Documents != "knowledge/services" || base.Embed != "gemini" {
		t.Errorf("services = %#v, want its authored embed preserved", base)
	}
	for _, tool := range agent.data.Tools {
		if tool.Name == "look_up_refund_policy" && tool.KnowledgeBase != "refunds" {
			t.Errorf("the knowledge tool lost its base: %#v", tool)
		}
	}
}

package tui

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/scaffold"
	"github.com/slng-ai/unmute/internal/spec"
)

// TestMaintainKeepsTheAgentName guards the same silent data loss as the
// knowledge round-trip, over the one field where losing it costs a live agent.
//
// `unmute maintain` rewrites agent.yaml from scaffold.Data, so a field that
// struct does not carry is a field the console deletes. `name:` is the deployed
// agent's identity on SLNG, and a package that loses it either stops compiling
// for that target or, worse, gets a different name and pushes a second agent
// beside the running one.
//
// The console offers no editor for it, exactly as it offers none for
// `knowledge:`. Carrying it is not optional either way.
func TestMaintainKeepsTheAgentName(t *testing.T) {
	root := filepath.Join(t.TempDir(), "some-folder")
	data := scaffold.Data{Name: "some-folder", AgentName: "acme-support"}
	data.SetTarget("livekit")
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}

	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, loss := range agent.losses {
		if strings.Contains(loss, "name") {
			t.Errorf("the console cannot preserve %q, so maintain would delete it", loss)
		}
	}
	// Read back verbatim, and not from the folder: the folder is called
	// something else here on purpose, because inferring the name from it is the
	// mistake this field exists to end.
	if agent.data.AgentName != "acme-support" {
		t.Errorf("AgentName = %q, want the authored name", agent.data.AgentName)
	}
}

// TestMaintainKeepsATasksHandoffs guards the same silent data loss over the key
// this feature added.
//
// A task's handoffs used to ride its `tools:` list, so scaffold.Data carried
// them for free. Splitting the lists moved them onto their own key, and a
// scaffold.Task without a Handoffs field would have made `unmute maintain`
// delete the shipped salon package's `to_complaints` on the way out, quietly and
// at exit 0. The salon package is the fixture because it is the one that
// actually authors the shape.
func TestMaintainKeepsATasksHandoffs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pkg")
	data := scaffold.Data{
		Name: "pkg", AgentName: "acme-support",
		Agents:   []scaffold.Agent{{Name: "billing", Instructions: "Handle billing."}},
		Handoffs: []scaffold.Handoff{{Name: "to_billing", Source: "assistant", To: "billing", When: "Billing.", History: "full"}},
		Tasks: []scaffold.Task{{
			Name: "collect", Instructions: "Collect the details.", Agent: "assistant",
			When: "Collect first.", Handoffs: []string{"to_billing"},
			History: "full",
		}},
	}
	data.SetTarget("livekit")
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}

	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, loss := range agent.losses {
		if strings.Contains(loss, "handoff") {
			t.Errorf("the console cannot preserve %q, so maintain would delete it", loss)
		}
	}
	var booking *scaffold.Task
	for i := range agent.data.Tasks {
		if agent.data.Tasks[i].Name == "collect" {
			booking = &agent.data.Tasks[i]
		}
	}
	if booking == nil {
		t.Fatal("the task did not survive the round trip at all")
	}
	if !slices.Contains(booking.Handoffs, "to_billing") {
		t.Errorf("task handoffs = %v, want to_billing carried through", booking.Handoffs)
	}
	// And it must not have leaked into tools:, which is where it used to live.
	if slices.Contains(booking.Tools, "to_billing") {
		t.Error("to_billing is in the task's tools: list; a handoff rides handoffs:")
	}
}

// TestMaintainKeepsATasksAnnounce guards the same silent data loss over
// another key this feature added.
//
// A task's announce: is the line it speaks as the step is entered, so the two
// model requests it takes to enter one are not silence. A scaffold.Task without
// an Announce field would have made `unmute maintain` delete the salon
// package's "Let me pull up the diary." on the way out, quietly and at exit 0.
func TestMaintainKeepsATasksAnnounce(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pkg")
	data := scaffold.Data{
		Name: "pkg", AgentName: "acme-support",
		Agents: []scaffold.Agent{{Name: "billing", Instructions: "Handle billing."}},
		Tasks: []scaffold.Task{{
			Name: "collect", Instructions: "Collect the details.", Agent: "assistant",
			When: "Collect first.", Announce: "One moment while I check.",
			History: "full",
		}},
	}
	data.SetTarget("livekit")
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}

	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, loss := range agent.losses {
		if strings.Contains(loss, "announce") {
			t.Errorf("the console cannot preserve %q, so maintain would delete it", loss)
		}
	}
	var collect *scaffold.Task
	for i := range agent.data.Tasks {
		if agent.data.Tasks[i].Name == "collect" {
			collect = &agent.data.Tasks[i]
		}
	}
	if collect == nil {
		t.Fatal("the task did not survive the round trip at all")
	}
	if collect.Announce != "One moment while I check." {
		t.Errorf("task announce = %q, want it carried through", collect.Announce)
	}
}

// TestMaintainKeepsAScalarSlngTool is spec 007 US3 acceptance scenario 6: a
// scalar `slng:` reference, its description override, its announcement and
// its injection all have to survive a console round trip. Before
// scaffold.Tool grew SlngName, the console read `tool.Slng.Hash` alone, so a
// scalar reference lost its hosted name on the way out and `unmute maintain`
// would have rewritten `slng: check_order` as `slng: {}`.
func TestMaintainKeepsAScalarSlngTool(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pkg")
	data := scaffold.Data{
		Name: "pkg", AgentName: "acme-support",
		Tools: []scaffold.Tool{{
			Name: "order_status", Execution: "slng", SlngName: "check_order",
			Description: "Look up an order by its number.",
			Announce:    "One moment while I look that up.",
			Inject:      []spec.Pair{{Key: "limit", Value: 0}},
		}},
	}
	data.SetTarget("livekit")
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}

	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, loss := range agent.losses {
		if strings.Contains(loss, "order_status") || strings.Contains(loss, "Slng") || strings.Contains(loss, "Announce") {
			t.Errorf("the console cannot preserve %q, so maintain would delete it", loss)
		}
	}
	var tool *scaffold.Tool
	for i := range agent.data.Tools {
		if agent.data.Tools[i].Name == "order_status" {
			tool = &agent.data.Tools[i]
		}
	}
	if tool == nil {
		t.Fatal("order_status did not survive the round trip at all")
	}
	if tool.SlngName != "check_order" {
		t.Errorf("SlngName = %q, want the scalar value carried through", tool.SlngName)
	}
	if tool.SlngHash != "" {
		t.Errorf("SlngHash = %q, want empty: a scalar reference authors no hash", tool.SlngHash)
	}
	if tool.Announce != "One moment while I look that up." {
		t.Errorf("Announce = %q, want it carried through", tool.Announce)
	}
	if len(tool.Inject) != 1 || tool.Inject[0].Key != "limit" {
		t.Errorf("Inject = %+v, want the limit:0 pair carried through", tool.Inject)
	}

	// The written package decodes with the scalar form intact: Name set,
	// Hash empty, which is what proves the console wrote `slng: check_order`
	// rather than `slng: {}` or a legacy block.
	pkg, err := spec.Load(root)
	if err != nil {
		t.Fatalf("the package the console wrote does not load: %v", err)
	}
	written := pkg.Tools["order_status"]
	if written.Slng == nil || written.Slng.Name != "check_order" || written.Slng.Hash != "" {
		t.Errorf("tools/order_status.yaml's slng: block = %+v, want Name=check_order and no Hash", written.Slng)
	}
}

// TestMaintainKeepsALegacySlngToolsAnnounceAndInject is the legacy block's
// half of the same story: `slng: hash:` already survived maintenance before
// this feature (SlngHash was always carried), but its announce: and inject:
// did not, because scaffold.Tool had no Announce field at all until this
// feature added one.
func TestMaintainKeepsALegacySlngToolsAnnounceAndInject(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pkg")
	data := scaffold.Data{
		Name: "pkg", AgentName: "acme-support",
		Tools: []scaffold.Tool{{
			Name: "check_order", Execution: "slng",
			SlngHash:    "f169d60d6496768081448551f1a84286a34569063a61c480b0aa12475759a00f",
			Description: "Look up an order by its number.",
			Announce:    "One moment while I look that up.",
			Inject:      []spec.Pair{{Key: "limit", Value: 0}},
		}},
	}
	data.SetTarget("livekit")
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}

	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, loss := range agent.losses {
		if strings.Contains(loss, "check_order") || strings.Contains(loss, "Announce") {
			t.Errorf("the console cannot preserve %q, so maintain would delete it", loss)
		}
	}
	var tool *scaffold.Tool
	for i := range agent.data.Tools {
		if agent.data.Tools[i].Name == "check_order" {
			tool = &agent.data.Tools[i]
		}
	}
	if tool == nil {
		t.Fatal("check_order did not survive the round trip at all")
	}
	if tool.SlngName != "" {
		t.Errorf("SlngName = %q, want empty: this is the legacy block, not a scalar reference", tool.SlngName)
	}
	if tool.SlngHash != "f169d60d6496768081448551f1a84286a34569063a61c480b0aa12475759a00f" {
		t.Errorf("SlngHash = %q, want the authored pin carried through", tool.SlngHash)
	}
	if tool.Announce != "One moment while I look that up." {
		t.Errorf("Announce = %q, want it carried through", tool.Announce)
	}
	if len(tool.Inject) != 1 || tool.Inject[0].Key != "limit" {
		t.Errorf("Inject = %+v, want the limit:0 pair carried through", tool.Inject)
	}

	pkg, err := spec.Load(root)
	if err != nil {
		t.Fatalf("the package the console wrote does not load: %v", err)
	}
	written := pkg.Tools["check_order"]
	if written.Slng == nil || written.Slng.Name != "" || written.Slng.Hash == "" {
		t.Errorf("tools/check_order.yaml's slng: block = %+v, want the legacy hash form preserved", written.Slng)
	}
}

// TestMaintainKeepsAnMCPServerAndSelection guards the two mcp: fields
// maintain.go's tool loop dropped: Server (the platform's name for the server
// when it differs from the tool's own name) and Tools (the explicit
// selection). Both are what spec 007's hosted-MCP shape is written with
// (contracts/authoring.md), so losing either turns a working reference into
// one `unmute maintain` silently narrows or breaks.
func TestMaintainKeepsAnMCPServerAndSelection(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pkg")
	data := scaffold.Data{
		Name: "pkg", AgentName: "acme-support",
		Tools: []scaffold.Tool{{
			Name: "web_search", Execution: "mcp",
			MCPServer: "firecrawl-mcp-2",
			MCPTools:  []string{"firecrawl_scrape", "firecrawl_search"},
		}},
	}
	data.SetTarget("livekit")
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}

	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, loss := range agent.losses {
		if strings.Contains(loss, "web_search") || strings.Contains(loss, "MCPServer") || strings.Contains(loss, "MCPTools") {
			t.Errorf("the console cannot preserve %q, so maintain would delete it", loss)
		}
	}
	var tool *scaffold.Tool
	for i := range agent.data.Tools {
		if agent.data.Tools[i].Name == "web_search" {
			tool = &agent.data.Tools[i]
		}
	}
	if tool == nil {
		t.Fatal("web_search did not survive the round trip at all")
	}
	if tool.MCPServer != "firecrawl-mcp-2" {
		t.Errorf("MCPServer = %q, want it carried through", tool.MCPServer)
	}
	if !slices.Equal(tool.MCPTools, []string{"firecrawl_scrape", "firecrawl_search"}) {
		t.Errorf("MCPTools = %v, want the explicit selection carried through", tool.MCPTools)
	}

	pkg, err := spec.Load(root)
	if err != nil {
		t.Fatalf("the package the console wrote does not load: %v", err)
	}
	written := pkg.Tools["web_search"]
	if written.MCP == nil || written.MCP.Server != "firecrawl-mcp-2" {
		t.Fatalf("tools/web_search.yaml's mcp: block = %+v, want server: firecrawl-mcp-2", written.MCP)
	}
	if !slices.Equal(written.MCP.Tools, []string{"firecrawl_scrape", "firecrawl_search"}) {
		t.Errorf("tools/web_search.yaml's mcp.tools = %v, want the explicit selection", written.MCP.Tools)
	}
}

// TestMaintainKeepsNonalphabeticalToolOrder is the ordering half of spec 007
// US3: the package's top-level tools: catalogue and an agent's own tools:
// list are both authored, ordered lists, and `unmute maintain` used to
// alphabetise both on the way out (packageData walked pkg.Tools, a map, via
// slices.Sorted(maps.Keys(...))). A catalogue authored zebra-before-alpha
// round-tripping as alpha-before-zebra is silent, because both orders decode
// to the same set and nothing but a byte comparison catches it.
func TestMaintainKeepsNonalphabeticalToolOrder(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pkg")
	data := scaffold.Data{
		Name: "pkg", AgentName: "acme-support",
		Agents: []scaffold.Agent{{Name: "billing", Instructions: "Handle billing."}},
		Tools: []scaffold.Tool{
			{Name: "zebra_tool", Execution: "knowledge", KnowledgeBase: "docs", AttachTo: []string{"billing"}},
			{Name: "alpha_tool", Execution: "knowledge", KnowledgeBase: "docs", AttachTo: []string{"billing"}},
			{Name: "middle_tool", Execution: "knowledge", KnowledgeBase: "docs"},
		},
		Knowledge: []scaffold.KnowledgeBase{{Name: "docs", Documents: "knowledge/docs"}},
	}
	data.SetTarget("livekit")
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}

	// Read the written package directly: the strongest, most direct
	// assertion is the authored file's own order, not a struct the console
	// happens to preserve in memory.
	pkg, err := spec.Load(root)
	if err != nil {
		t.Fatalf("the package the console wrote does not load: %v", err)
	}
	want := []string{"zebra_tool", "alpha_tool", "middle_tool"}
	if !slices.Equal(pkg.Agent.Tools, want) {
		t.Errorf("agent.yaml's tools: catalogue = %v, want the authored order %v", pkg.Agent.Tools, want)
	}
	wantBilling := []string{"zebra_tool", "alpha_tool"}
	if !slices.Equal(pkg.Agent.Agents["billing"].Tools, wantBilling) {
		t.Errorf("billing's tools: list = %v, want the authored order %v", pkg.Agent.Agents["billing"].Tools, wantBilling)
	}

	// And the round trip itself reproduces the same order a second time,
	// which is what `unmute maintain` actually runs. Losses are filtered to
	// this feature's own fields rather than asserted at zero: a pre-existing,
	// unrelated bug double-appends a trailing newline to every agent's
	// instructions on a second write (scaffold.go always appends "\n", and
	// packageData reads an instructions.md that already ends in one), which
	// every other round-trip test in this file also carries and also does not
	// assert on for the same reason — fixing it is a general maintenance
	// repair this task does not own.
	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, loss := range agent.losses {
		if strings.Contains(loss, "tool") || strings.Contains(loss, "Tool") {
			t.Errorf("the console cannot preserve %q, so maintain would delete it", loss)
		}
	}
	var gotNames []string
	for _, tool := range agent.data.Tools {
		gotNames = append(gotNames, tool.Name)
	}
	if !slices.Equal(gotNames, want) {
		t.Errorf("scaffold.Data.Tools order = %v, want %v", gotNames, want)
	}
}

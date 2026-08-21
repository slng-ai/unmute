package skill

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/scaffold"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// The bundle restates facts that Go code already owns: the tool execution
// blocks, the catalogue's vendors, the provider set, the command tree, and the
// documentation pages. Constitution III says a fact stated twice gets an
// agreement test, so each of those lists is held here. Every failure names the
// bundle file that has to change.
//
// The command agreement test lives in internal/cli, because the cobra tree is
// unexported and internal/cli already imports this package.

// bundleFile reads one file from the shipped bundle.
func bundleFile(t *testing.T, name string) string {
	t.Helper()
	files, err := New("test").Files(Canonical)
	if err != nil {
		t.Fatal(err)
	}
	content, ok := files[name]
	if !ok {
		t.Fatalf("the bundle has no %s", name)
	}
	return string(content)
}

// referenceNames lists every file under references/ in the shipped bundle.
func referenceNames(t *testing.T) []string {
	t.Helper()
	files, err := New("test").Files(Canonical)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for name := range files {
		if strings.HasPrefix(name, "references/") {
			out = append(out, name)
		}
	}
	return out
}

// TestToolsReferenceMatchesExecutionBlocks holds references/tools.md against the
// Tool struct. A block added or removed in internal/spec fails here until the
// reference is updated, and a block the reference invents fails too.
func TestToolsReferenceMatchesExecutionBlocks(t *testing.T) {
	raw := bundleFile(t, "references/tools.md")

	row := regexp.MustCompile("^\\| `([a-z_]+):` \\|")
	documented := map[string]bool{}
	for _, line := range strings.Split(raw, "\n") {
		if m := row.FindStringSubmatch(line); m != nil {
			documented[m[1]] = true
		}
	}
	if len(documented) == 0 {
		t.Fatal("parsed no execution-block rows from references/tools.md: table format changed? update this parser")
	}

	blocks := map[string]bool{}
	tool := reflect.TypeOf(spec.Tool{})
	for i := range tool.NumField() {
		field := tool.Field(i)
		if field.Type.Kind() != reflect.Pointer {
			continue // the contract fields, which every block shares
		}
		name := strings.Split(field.Tag.Get("yaml"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		blocks[name] = true
	}
	if len(blocks) == 0 {
		t.Fatal("no execution blocks found on spec.Tool: the struct shape changed, so this test needs rewriting")
	}

	for name := range documented {
		if !blocks[name] {
			t.Errorf("references/tools.md documents execution block %q, which spec.Tool does not have", name)
		}
	}
	for name := range blocks {
		if !documented[name] {
			t.Errorf("spec.Tool has execution block %q, which references/tools.md does not document", name)
		}
	}
}

// TestToolOwnershipRuleStaysExplicit holds the two surfaces a coding agent can
// follow against the public pages an author reads. Tool and task output schemas
// are both maps, so prose is the only guard against copying one into the other.
func TestToolOwnershipRuleStaysExplicit(t *testing.T) {
	definitionRule := "Define each tool once."
	for name, content := range map[string]string{
		"SKILL.md":                           bundleFile(t, "SKILL.md"),
		"references/tools.md":                bundleFile(t, "references/tools.md"),
		"docs-site/build/tools/overview.mdx": trackedFile(t, "docs-site/build/tools/overview.mdx"),
		"docs-site/reference/agent-yaml.mdx": trackedFile(t, "docs-site/reference/agent-yaml.mdx"),
	} {
		if !strings.Contains(content, definitionRule) {
			t.Errorf("%s does not state %q", name, definitionRule)
		}
	}

	resultRule := "Task `result:` and tool `output:` are different contracts."
	for name, content := range map[string]string{
		"SKILL.md":                                bundleFile(t, "SKILL.md"),
		"references/orchestration.md":             bundleFile(t, "references/orchestration.md"),
		"references/tools.md":                     bundleFile(t, "references/tools.md"),
		"docs-site/build/orchestration/tasks.mdx": trackedFile(t, "docs-site/build/orchestration/tasks.mdx"),
		"docs-site/build/tools/overview.mdx":      trackedFile(t, "docs-site/build/tools/overview.mdx"),
		"docs-site/reference/agent-yaml.mdx":      trackedFile(t, "docs-site/reference/agent-yaml.mdx"),
	} {
		if !strings.Contains(content, resultRule) {
			t.Errorf("%s does not state %q", name, resultRule)
		}
	}
}

func TestTaskAuthoringContractStaysExplicit(t *testing.T) {
	const rule = "Every task, including a task inside a group, needs a non-empty `result:` and `context.history`."
	for name, content := range map[string]string{
		"SKILL.md":                                      bundleFile(t, "SKILL.md"),
		"references/orchestration.md":                   bundleFile(t, "references/orchestration.md"),
		"docs-site/build/orchestration/tasks.mdx":       trackedFile(t, "docs-site/build/orchestration/tasks.mdx"),
		"docs-site/build/orchestration/task-groups.mdx": trackedFile(t, "docs-site/build/orchestration/task-groups.mdx"),
	} {
		if !strings.Contains(content, rule) {
			t.Errorf("%s does not state %q", name, rule)
		}
	}
}

func TestTaskTransferAndSharedResultDocsStayAligned(t *testing.T) {
	transferDocs := map[string]string{
		"references/orchestration.md":                      bundleFile(t, "references/orchestration.md"),
		"docs-site/build/orchestration/tasks.mdx":          trackedFile(t, "docs-site/build/orchestration/tasks.mdx"),
		"docs-site/build/orchestration/task-groups.mdx":    trackedFile(t, "docs-site/build/orchestration/task-groups.mdx"),
		"docs-site/build/orchestration/handoffs.mdx":       trackedFile(t, "docs-site/build/orchestration/handoffs.mdx"),
		"internal/generate/templates/livekit_v1/README.md": trackedFile(t, "internal/generate/templates/livekit_v1/README.md.tmpl"),
		"internal/generate/templates/pipecat_v1/README.md": trackedFile(t, "internal/generate/templates/pipecat_v1/README.md.tmpl"),
	}
	for name, content := range transferDocs {
		if !strings.Contains(content, "agent_transfer") {
			t.Errorf("%s does not document task-scoped agent_transfer", name)
		}
	}
	for name, content := range map[string]string{
		"references/orchestration.md":                      transferDocs["references/orchestration.md"],
		"docs-site/build/orchestration/task-groups.mdx":    transferDocs["docs-site/build/orchestration/task-groups.mdx"],
		"examples/task-groups/README.md":                   trackedFile(t, "examples/task-groups/README.md"),
		"internal/generate/templates/livekit_v1/README.md": transferDocs["internal/generate/templates/livekit_v1/README.md"],
		"internal/generate/templates/pipecat_v1/README.md": transferDocs["internal/generate/templates/pipecat_v1/README.md"],
	} {
		for rule, pattern := range map[string]*regexp.Regexp{
			"intermediate result timing": regexp.MustCompile(`exact\s+typed\s+result\s+enters\s+(?:the\s+)?shared\s+context\s+before\s+the\s+next\s+task\s+starts`),
			"final result map":           regexp.MustCompile("final\\s+`merge: results`\\s+map\\s+is\\s+keyed\\s+by\\s+task\\s+name"),
			"isolated result boundary":   regexp.MustCompile(`(?i)isolated\s+group\s+carries\s+no\s+results\s+between\s+steps`),
		} {
			if !pattern.MatchString(content) {
				t.Errorf("%s does not state the %s rule", name, rule)
			}
		}
	}
}

func TestPipecatCloudTransferFallbackDocsStayAligned(t *testing.T) {
	const rule = "Pipecat `cloud-websocket` requires explicit `on_unavailable: hangup`; it cannot reconnect the original media stream."
	for name, content := range map[string]string{
		"references/transfers.md":                          bundleFile(t, "references/transfers.md"),
		"docs-site/transfers/pipecat-twilio.mdx":           trackedFile(t, "docs-site/transfers/pipecat-twilio.mdx"),
		"examples/pipecat-human-transfer-twilio/README.md": trackedFile(t, "examples/pipecat-human-transfer-twilio/README.md"),
		"internal/generate/templates/pipecat_v1/README.md": trackedFile(t, "internal/generate/templates/pipecat_v1/README.md.tmpl"),
	} {
		if !strings.Contains(strings.Join(strings.Fields(content), " "), rule) {
			t.Errorf("%s does not state the cloud transfer fallback rule", name)
		}
		if !strings.Contains(content, "`transfer_started`") {
			t.Errorf("%s does not distinguish REST acceptance from transfer completion", name)
		}
	}
}

func TestCapacityRequirementMatchesPublicGuide(t *testing.T) {
	const row = "| `capacity` | for telephony or code targets | your traffic estimate |"
	for name, content := range map[string]string{
		"references/package.md":              bundleFile(t, "references/package.md"),
		"docs-site/reference/agent-yaml.mdx": trackedFile(t, "docs-site/reference/agent-yaml.mdx"),
	} {
		if !strings.Contains(content, row) {
			t.Errorf("%s does not state the code-target capacity requirement", name)
		}
	}
}

func TestTracingPrivacyWarningStaysExplicit(t *testing.T) {
	const dataWarning = "Traces can contain caller speech, model input and output, and tool arguments and results."
	const fakeDataRule = "Use only fake identities and fake customer data for release tests."
	for name, content := range map[string]string{
		"references/package.md":                                 bundleFile(t, "references/package.md"),
		"docs-site/reference/agent-yaml.mdx":                    trackedFile(t, "docs-site/reference/agent-yaml.mdx"),
		"docs/HARNESS_TEST.md":                                  trackedFile(t, "docs/HARNESS_TEST.md"),
		"examples/simple-prompt/README.md":                      trackedFile(t, "examples/simple-prompt/README.md"),
		"internal/generate/templates/livekit_v1/README.md.tmpl": trackedFile(t, "internal/generate/templates/livekit_v1/README.md.tmpl"),
		"internal/generate/templates/pipecat_v1/README.md.tmpl": trackedFile(t, "internal/generate/templates/pipecat_v1/README.md.tmpl"),
	} {
		if !strings.Contains(content, dataWarning) {
			t.Errorf("%s does not warn what tracing records", name)
		}
		if !strings.Contains(content, fakeDataRule) {
			t.Errorf("%s does not require fake release-test data", name)
		}
	}
}

func TestPipecatTracingProviderOwnershipStaysExplicit(t *testing.T) {
	const rule = "Pipecat tracing owns the process OpenTelemetry provider and startup fails if another SDK provider is installed first."
	for name, content := range map[string]string{
		"references/package.md":                                 bundleFile(t, "references/package.md"),
		"docs-site/reference/agent-yaml.mdx":                    trackedFile(t, "docs-site/reference/agent-yaml.mdx"),
		"examples/simple-prompt/README.md":                      trackedFile(t, "examples/simple-prompt/README.md"),
		"internal/generate/templates/pipecat_v1/README.md.tmpl": trackedFile(t, "internal/generate/templates/pipecat_v1/README.md.tmpl"),
	} {
		if !strings.Contains(content, rule) {
			t.Errorf("%s does not state Pipecat tracing provider ownership", name)
		}
	}
}

func TestPipecatMCPTracingDocsStayAligned(t *testing.T) {
	const tracingRule = "With Langfuse tracing enabled, Pipecat MCP calls emit finite `tool:<name>` spans with tool arguments and, when completed, the result."
	const collisionRule = "Pipecat refuses to start when an agent tool, task function, or MCP source on the same agent exposes the same name."
	const cleanupRule = "Pipecat 1.7 has one cleanup limit: cancellation during `MCPClient.start()` may leave a partial transport open until async-generator or process cleanup; cancellation after `start()` returns is cleaned up normally."
	for name, content := range map[string]string{
		"references/tools.md":                                   bundleFile(t, "references/tools.md"),
		"docs-site/build/tools/mcp.mdx":                         trackedFile(t, "docs-site/build/tools/mcp.mdx"),
		"examples/mcp-example/README.md":                        trackedFile(t, "examples/mcp-example/README.md"),
		"internal/generate/templates/pipecat_v1/README.md.tmpl": trackedFile(t, "internal/generate/templates/pipecat_v1/README.md.tmpl"),
	} {
		if !strings.Contains(content, tracingRule) {
			t.Errorf("%s does not state the Pipecat MCP tracing contract", name)
		}
		if !strings.Contains(content, collisionRule) {
			t.Errorf("%s does not state the Pipecat MCP collision contract", name)
		}
		if !strings.Contains(content, cleanupRule) {
			t.Errorf("%s does not state the Pipecat MCP cleanup limit", name)
		}
	}
}

func TestExistingPackageWorkflowPrecedesWriting(t *testing.T) {
	steps := []string{
		"Inspect the existing package",
		"Run `unmute validate` before editing",
		"Fix invalid definitions",
		"Simplify",
		"Run `unmute validate` again",
		"Run `unmute compile`",
	}
	for name, content := range map[string]string{
		"SKILL.md":                          bundleFile(t, "SKILL.md"),
		"docs-site/start/coding-agents.mdx": trackedFile(t, "docs-site/start/coding-agents.mdx"),
	} {
		position := -1
		for _, step := range steps {
			next := strings.Index(content[position+1:], step)
			if next < 0 {
				t.Errorf("%s does not name maintenance step %q", name, step)
				break
			}
			position += next + 1
		}
	}
}

func TestAssistantAuthoredYAMLUsesBlockSequences(t *testing.T) {
	const rule = "Use block-style YAML sequences in assistant-authored packages. Do not use anchors or aliases."
	for name, content := range map[string]string{
		"SKILL.md":                          bundleFile(t, "SKILL.md"),
		"docs-site/start/coding-agents.mdx": trackedFile(t, "docs-site/start/coding-agents.mdx"),
	} {
		if !strings.Contains(content, rule) {
			t.Errorf("%s does not state %q", name, rule)
		}
	}

	flowTools := regexp.MustCompile(`(?m)^\s*tools:\s*\[`)
	for name, content := range authorFacingModelSurfaces(t) {
		if flowTools.MatchString(content) {
			t.Errorf("%s uses a flow-style tools list; assistant-facing examples use block sequences", name)
		}
	}
	for name, content := range map[string]string{
		"bundle/SKILL.md":                    bundleFile(t, "SKILL.md"),
		"bundle/references/package.md":       bundleFile(t, "references/package.md"),
		"bundle/references/tools.md":         bundleFile(t, "references/tools.md"),
		"bundle/references/orchestration.md": bundleFile(t, "references/orchestration.md"),
	} {
		if flowTools.MatchString(content) {
			t.Errorf("%s uses a flow-style tools list; assistant-facing examples use block sequences", name)
		}
	}
}

func TestToolClosedValuesMatchCode(t *testing.T) {
	values := map[string][]string{
		"type":         {string(ir.ToolAuthBearer), string(ir.ToolAuthAPIKey)},
		"transport":    {ir.MCPTransportSSE, ir.MCPTransportStreamableHTTP},
		"interruption": {string(ir.ToolProviderDefault), string(ir.ToolContinue), string(ir.ToolCancel)},
		"effect":       {string(ir.ToolReturnsData), string(ir.ToolEndsConversation)},
	}
	for name, content := range map[string]string{
		"references/tools.md":                bundleFile(t, "references/tools.md"),
		"docs-site/reference/agent-yaml.mdx": trackedFile(t, "docs-site/reference/agent-yaml.mdx"),
	} {
		for field, want := range values {
			if !tableRowHasExactValues(content, field, want) {
				t.Errorf("%s has no %q row with exactly %v", name, field, want)
			}
		}
	}
}

func tableRowHasExactValues(content, field string, want []string) bool {
	want = slices.Clone(want)
	slices.Sort(want)
	code := regexp.MustCompile("`([^`]+)`")
	for _, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "| `"+field+"` |") {
			continue
		}
		seen := map[string]bool{}
		for _, match := range code.FindAllStringSubmatch(line, -1) {
			if match[1] != field {
				seen[match[1]] = true
			}
		}
		if slices.Equal(slices.Sorted(maps.Keys(seen)), want) {
			return true
		}
	}
	return false
}

// TestModelsReferenceMatchesCatalog holds references/models.md against the
// provider catalogue, per target per role, and holds the one editorial rule the
// documentation site is written under: SLNG leads every list it appears in.
func TestModelsReferenceMatchesCatalog(t *testing.T) {
	raw := bundleFile(t, "references/models.md")

	// The reference names the roles the way an author writes them; the
	// catalogue keeps the internal name "reason" for the thinking kind.
	roles := map[string]target.Role{"listen": target.Listen, "speak": target.Speak, "think": target.Reason}
	providers := map[string]target.Provider{"pipecat": target.Pipecat, "livekit": target.LiveKit}

	row := regexp.MustCompile(`^\| (pipecat|livekit) \| (listen|speak|think) \| (.*) \|$`)
	vendor := regexp.MustCompile("`([a-z0-9_]+)`")

	documented := map[string][]string{}
	for _, line := range strings.Split(raw, "\n") {
		m := row.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		var vendors []string
		for _, hit := range vendor.FindAllStringSubmatch(m[3], -1) {
			vendors = append(vendors, hit[1])
		}
		documented[m[1]+" "+m[2]] = vendors
	}
	if len(documented) != 6 {
		t.Fatalf("parsed %d vendor rows from references/models.md, want 6 (two targets, three roles) — table format changed? update this parser", len(documented))
	}

	cat := target.DefaultCatalog()
	for key, vendors := range documented {
		parts := strings.Fields(key)
		fw, role := providers[parts[0]], roles[parts[1]]
		catalogued := cat.Vendors(fw, role)

		for _, name := range vendors {
			if !containsString(catalogued, name) {
				t.Errorf("references/models.md lists %s %s %q, which the catalogue does not have", parts[0], parts[1], name)
			}
		}
		for _, name := range catalogued {
			if !containsString(vendors, name) {
				t.Errorf("catalogue entry %s/%s/%s is missing from references/models.md", parts[0], parts[1], name)
			}
		}
		if containsString(catalogued, "slng") && len(vendors) > 0 && vendors[0] != "slng" {
			t.Errorf("references/models.md %s %s lists %q first; slng leads every list it appears in", parts[0], parts[1], vendors[0])
		}
	}

	// The turn role has no catalogue entries on either target, which is exactly
	// why the reference explains a mechanism instead of listing vendors. If that
	// ever changes, the reference has to grow a row.
	for _, fw := range []target.Provider{target.Pipecat, target.LiveKit} {
		if vendors := cat.Vendors(fw, target.Turn); len(vendors) != 0 {
			t.Errorf("the catalogue now has %s turn vendors %v: references/models.md must list them", fw, vendors)
		}
	}
}

func TestModelWildcardGuidanceMatchesCatalog(t *testing.T) {
	cat := target.DefaultCatalog()
	skill := bundleFile(t, "references/models.md")
	if !strings.Contains(skill, "unlisted listen, speak, or think provider") || !strings.Contains(skill, "endpoint_env") {
		t.Error("references/models.md does not explain the three endpoint-gated Pipecat wildcards")
	}
	public := map[target.Role]string{
		target.Listen: trackedFile(t, "docs-site/models/stt.mdx"),
		target.Speak:  trackedFile(t, "docs-site/models/tts.mdx"),
		target.Reason: trackedFile(t, "docs-site/models/llm.mdx"),
	}
	for _, role := range []target.Role{target.Listen, target.Speak, target.Reason} {
		entry, ok := cat.Lookup(target.Pipecat, role, "*")
		if !ok || !entry.Wildcard() || !entry.RequiresEndpoint {
			t.Fatalf("Pipecat %s wildcard contract changed: %#v, %v", role, entry, ok)
		}
		if content := public[role]; !strings.Contains(content, "unlisted provider") || !strings.Contains(content, "endpoint_env") {
			t.Errorf("public model page does not explain the endpoint-gated Pipecat %s wildcard", role)
		}
	}
	livekit, ok := cat.Lookup(target.LiveKit, target.Reason, "*")
	if !ok || !livekit.Wildcard() || livekit.RequiresEndpoint {
		t.Fatalf("LiveKit reasoning wildcard contract changed: %#v, %v", livekit, ok)
	}
	for name, content := range map[string]string{"references/models.md": skill, "docs-site/models/llm.mdx": public[target.Reason]} {
		if !strings.Contains(content, "LiveKit Inference") {
			t.Errorf("%s does not explain the LiveKit reasoning wildcard", name)
		}
	}
}

func TestOpenAIResponsesGuidanceMatchesExample(t *testing.T) {
	for name, content := range map[string]string{
		"references/models.md":     bundleFile(t, "references/models.md"),
		"docs-site/models/llm.mdx": trackedFile(t, "docs-site/models/llm.mdx"),
	} {
		for _, want := range []string{"api: responses", "reasoning_effort: none", "use_websocket: true"} {
			if !strings.Contains(content, want) {
				t.Errorf("%s omits %q", name, want)
			}
		}
	}
}

func TestModelFieldAndPassthroughGuidanceStaysExact(t *testing.T) {
	rows := []string{
		"| `voice`, `speed` | `speak` |",
		"| `language` | `speak`, `listen` |",
		"| `temperature`, `top_p`, `top_k` | `think` |",
		"| `semantic_endpointing` | `turn`: `required`, `preferred`, or `off` |",
		"| `endpointing_delay` | `turn`: a positive duration, the window of silence before the caller counts as finished. The floor on every turn. LiveKit refuses under `250ms`; defaults differ per target (LiveKit `550ms`, Pipecat `200ms`) |",
	}
	for name, content := range map[string]string{
		"references/package.md":              bundleFile(t, "references/package.md"),
		"docs-site/reference/agent-yaml.mdx": trackedFile(t, "docs-site/reference/agent-yaml.mdx"),
	} {
		for _, row := range rows {
			if !strings.Contains(content, row) {
				t.Errorf("%s is missing model-field row %q", name, row)
			}
		}
		if !strings.Contains(content, "Do not guess model ids, voice ids, or params.") {
			t.Errorf("%s does not forbid guessing provider passthrough values", name)
		}
	}
}

func TestRegionalGuidanceStaysExplicit(t *testing.T) {
	models := bundleFile(t, "references/models.md")
	for _, want := range []string{
		"world_part_override",
		"region_override",
		"`region_override` takes precedence over `world_part_override`",
		"https://docs.slng.ai/agents/livekit-plugin",
		"1.6.7 or newer",
		"https://docs.slng.ai/agents/pipecat-plugin",
		"0.4.0 or newer",
	} {
		if !strings.Contains(models, want) {
			t.Errorf("references/models.md does not state %q", want)
		}
	}

	packageReference := bundleFile(t, "references/package.md")
	for _, want := range []string{
		"`deployment_region` chooses where the agent worker runs",
		"A LiveKit target accepts one deployment region or a duplicate-free list.",
		"Pipecat accepts exactly one.",
		"For hard regional isolation, use one target instance per geography.",
	} {
		if !strings.Contains(packageReference, want) {
			t.Errorf("references/package.md does not state %q", want)
		}
	}

	if examples := bundleFile(t, "references/examples.md"); !strings.Contains(examples, "`examples/regional-infrastructure`") {
		t.Error("references/examples.md does not route regional work to examples/regional-infrastructure")
	}
}

func TestThinkingAudioValuesMatchCode(t *testing.T) {
	want := []string{string(ir.ThinkingNone), string(ir.ThinkingSubtle)}
	for name, content := range map[string]string{
		"references/conversation.md":         bundleFile(t, "references/conversation.md"),
		"docs-site/reference/agent-yaml.mdx": trackedFile(t, "docs-site/reference/agent-yaml.mdx"),
	} {
		if !tableRowHasExactValues(content, "thinking_audio", want) {
			t.Errorf("%s has no thinking_audio row with exactly %v", name, want)
		}
	}
}

// TestProvidersReferenceMatchesTargetSet holds the provider table in
// references/package.md: the set comes from internal/target, and every row says
// whether support means validation or generation.
func TestProvidersReferenceMatchesTargetSet(t *testing.T) {
	raw := bundleFile(t, "references/package.md")

	row := regexp.MustCompile("^\\| `([a-z]+)` \\| (yes|no) \\| (yes|no) \\|$")
	documented := map[target.Provider][2]string{}
	for _, line := range strings.Split(raw, "\n") {
		if m := row.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			documented[target.Provider(m[1])] = [2]string{m[2], m[3]}
		}
	}
	if len(documented) == 0 {
		t.Fatal("parsed no provider rows from references/package.md: table format changed? update this parser")
	}

	for provider := range documented {
		if !containsProvider(target.Providers, provider) {
			t.Errorf("references/package.md names provider %q, which internal/target does not have", provider)
		}
	}
	for _, provider := range target.Providers {
		cells, ok := documented[provider]
		if !ok {
			t.Errorf("provider %q is missing from the table in references/package.md", provider)
			continue
		}
		if cells[0] != "yes" {
			t.Errorf("references/package.md says %q does not validate; every provider validates", provider)
		}
		// Which providers have a shipped driver is target.EmitsProject, which is
		// the one list; ir.Validate reads the same one.
		want := "no"
		if target.EmitsProject(provider) {
			want = "yes"
		}
		if cells[1] != want {
			t.Errorf("references/package.md says %q generates %q, want %q", provider, cells[1], want)
		}
	}

	fieldRow := regexp.MustCompile("(?m)^\\| `provider` \\| (.*) \\|$").FindStringSubmatch(raw)
	if fieldRow == nil {
		t.Fatal("references/package.md has no targets provider field row")
	}
	fieldProviderSet := map[string]bool{}
	for _, match := range regexp.MustCompile("`([a-z]+)`").FindAllStringSubmatch(fieldRow[1], -1) {
		fieldProviderSet[match[1]] = true
	}
	fieldProviders := slices.Collect(maps.Keys(fieldProviderSet))
	wantProviders := make([]string, 0, len(target.Providers))
	for _, provider := range target.Providers {
		wantProviders = append(wantProviders, string(provider))
	}
	slices.Sort(fieldProviders)
	slices.Sort(wantProviders)
	if !slices.Equal(fieldProviders, wantProviders) {
		t.Errorf("references/package.md provider field = %v, want %v", fieldProviders, wantProviders)
	}
}

func trackedFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func nativeChoices(t *testing.T, name, content string) map[string][2]string {
	t.Helper()
	const heading = "## Choose the native shape\n"
	start := strings.Index(content, heading)
	if start < 0 {
		t.Fatalf("%s has no %q section", name, strings.TrimSpace(heading))
	}
	section := content[start+len(heading):]
	if end := strings.Index(section, "\n## "); end >= 0 {
		section = section[:end]
	}

	row := regexp.MustCompile("^\\| ([^|]+) \\| `([^`]+)` \\| ([^|]+) \\|$")
	choices := map[string][2]string{}
	for _, line := range strings.Split(section, "\n") {
		match := row.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		shape := match[2]
		if _, exists := choices[shape]; exists {
			t.Fatalf("%s documents native shape %q twice", name, shape)
		}
		choices[shape] = [2]string{match[1], match[3]}
	}
	return choices
}

// TestOrchestrationDecisionTableMatchesPublicGuide holds the five choices a
// coding agent makes before it writes files. The skill is the offline product
// surface and the site is the public one, so neither may grow a private sixth
// shape or give the same shape a different job.
func TestOrchestrationDecisionTableMatchesPublicGuide(t *testing.T) {
	skillChoices := nativeChoices(t, "references/orchestration.md", bundleFile(t, "references/orchestration.md"))
	publicChoices := nativeChoices(t, "docs-site/build/orchestration/choosing-a-structure.mdx",
		trackedFile(t, "docs-site/build/orchestration/choosing-a-structure.mdx"))

	want := []string{"agent handoff", "task", "task group", "tool", "variable"}
	if got := slices.Sorted(maps.Keys(skillChoices)); !slices.Equal(got, want) {
		t.Errorf("references/orchestration.md native shapes = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(skillChoices, publicChoices) {
		t.Errorf("native-choice tables disagree:\n  skill:  %v\n  public: %v", skillChoices, publicChoices)
	}
}

// TestEntryRoutesStructuredBriefBeforeScaffolding keeps architecture ahead of
// file generation. If this checkpoint moves below the build loop, assistants
// are already anchored on a one-agent scaffold when they discover ordering.
func TestEntryRoutesStructuredBriefBeforeScaffolding(t *testing.T) {
	entry := bundleFile(t, "SKILL.md")
	checkpoint := strings.Index(entry, "## Choose the structure before files")
	buildLoop := strings.Index(entry, "## The build loop")
	if checkpoint < 0 || buildLoop < 0 || checkpoint > buildLoop {
		t.Fatal("SKILL.md must route structured briefs through orchestration before the build loop")
	}
	section := entry[checkpoint:buildLoop]
	for _, want := range []string{"required order", "separate roles", "next step", "references/orchestration.md"} {
		if !strings.Contains(section, want) {
			t.Errorf("SKILL.md structure checkpoint does not name %q", want)
		}
	}
}

// TestOrchestrationGuidanceMatchesCodeOwnedFacts holds the small set of facts
// corrected with the native-choice guidance. Runtime code remains their owner.
func TestOrchestrationGuidanceMatchesCodeOwnedFacts(t *testing.T) {
	orchestration := bundleFile(t, "references/orchestration.md")
	variables := bundleFile(t, "references/variables.md")
	agentReference := trackedFile(t, "docs-site/reference/agent-yaml.mdx")

	if !strings.Contains(variables, "| a task's instructions | when that task starts |") {
		t.Error("references/variables.md must say task instructions render when the task starts")
	}

	delegateRow := regexp.MustCompile("(?m)^\\| `delegate` \\| (.*) \\|$").FindStringSubmatch(agentReference)
	if delegateRow == nil || delegateRow[1] != "`task` or `group`, `when`, `assign`" {
		t.Errorf("docs-site/reference/agent-yaml.mdx delegate fields = %q, want task/group, when, assign", delegateRow)
	}
	for _, match := range regexp.MustCompile("(?s)```yaml[^\\n]*\\n(.*?)```").FindAllStringSubmatch(orchestration, -1) {
		if strings.Contains(match[1], "kind: delegate") && strings.Contains(match[1], "requires:") {
			t.Error("references/orchestration.md puts requires on a delegate; code allows it on agent_transfer only")
		}
	}

	historyRow := regexp.MustCompile("(?m)^\\| `pipecat` \\| (.*) \\| (.*) \\|$").FindStringSubmatch(orchestration)
	if historyRow == nil {
		t.Fatal("references/orchestration.md has no Pipecat task-history row")
	}
	code := regexp.MustCompile("`([^`]+)`")
	read := func(cell string) []string {
		var values []string
		for _, match := range code.FindAllStringSubmatch(cell, -1) {
			values = append(values, match[1])
		}
		slices.Sort(values)
		return values
	}
	var supported, rejected []string
	table := target.Default()
	for _, history := range []target.History{
		target.HistoryFull, target.HistoryMessages, target.HistoryLastN, target.HistorySummary, target.HistoryReset,
	} {
		if table.HistorySupport(history, target.Pipecat).Kind == target.HistoryFail {
			rejected = append(rejected, string(history))
		} else {
			supported = append(supported, string(history))
		}
	}
	slices.Sort(supported)
	slices.Sort(rejected)
	if got := read(historyRow[1]); !slices.Equal(got, supported) {
		t.Errorf("references/orchestration.md Pipecat supported task history = %v, want %v", got, supported)
	}
	if got := read(historyRow[2]); !slices.Equal(got, rejected) {
		t.Errorf("references/orchestration.md Pipecat rejected task history = %v, want %v", got, rejected)
	}

	warning := table.Capability(target.FieldTaskGroup, target.LiveKit).Note
	if warning == "" || !strings.Contains(orchestration, "livekit: "+warning) {
		t.Error("references/orchestration.md dropped the LiveKit task-group warning")
	}
}

// sitePages lists every page path the documentation site carries, as the site
// addresses them: the path under docs-site/ with the .mdx dropped.
func sitePages(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "..", "docs-site")
	var pages []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".mdx" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		pages = append(pages, strings.TrimSuffix(filepath.ToSlash(rel), ".mdx"))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) == 0 {
		t.Fatal("found no pages under docs-site/, so this test would pass for the wrong reason")
	}
	return pages
}

// TestBundleNamesNoSitePage holds the inverse of what this test used to hold.
//
// The references once ended with a "Documentation:" line naming a site path.
// The installed bundle is version-matched to the CLI and must still work
// offline, so its task references carry the needed facts and `unmute validate`
// remains the final authority. This test keeps unresolved, versionless site
// paths out of that offline contract.
func TestBundleNamesNoSitePage(t *testing.T) {
	pages := sitePages(t)

	for _, name := range referenceNames(t) {
		content := bundleFile(t, name)

		if strings.Contains(content, "\nDocumentation:") {
			t.Errorf("%s carries a Documentation line; the site is not published, so its paths resolve to nothing for a reader", name)
		}
		for _, page := range pages {
			if strings.Contains(content, "`"+page+"`") {
				t.Errorf("%s names the site page %q, which a reader outside this repository cannot open; say the fact or point at another file in this bundle", name, page)
			}
		}
	}
}

// TestEntryDocumentBudget holds the layering. SKILL.md is read on every task, so
// it is a decision layer that routes to a reference, not a summary of all of
// them. Keep the entry under 100 lines and move detail into references.
func TestEntryDocumentBudget(t *testing.T) {
	lines := strings.Count(bundleFile(t, "SKILL.md"), "\n")
	if lines >= 100 {
		t.Errorf("SKILL.md is %d lines; the budget is under 100. Move detail into a reference rather than raising this number", lines)
	}
}

// TestNoOrphanReferences holds both halves of the routing table: every reference
// on disk is reachable from SKILL.md, and every reference SKILL.md names exists.
func TestNoOrphanReferences(t *testing.T) {
	entry := bundleFile(t, "SKILL.md")

	for _, name := range referenceNames(t) {
		if !strings.Contains(entry, name) {
			t.Errorf("%s is in the bundle but SKILL.md never names it: an assistant will never open it", name)
		}
	}

	named := regexp.MustCompile("`(references/[a-z0-9-]+\\.md)`")
	files, err := New("test").Files(Canonical)
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range named.FindAllStringSubmatch(entry, -1) {
		if _, ok := files[hit[1]]; !ok {
			t.Errorf("SKILL.md routes to %s, which the bundle does not carry", hit[1])
		}
	}
}

// frontmatterKeys returns the top-level YAML keys of a file's frontmatter.
func frontmatterKeys(t *testing.T, content string) []string {
	t.Helper()
	if !strings.HasPrefix(content, "---\n") {
		t.Fatal("the file does not open with YAML frontmatter")
	}
	end := strings.Index(content[4:], "\n---\n")
	if end < 0 {
		t.Fatal("the frontmatter is not closed")
	}
	key := regexp.MustCompile("^([a-z_]+):")
	var out []string
	for _, line := range strings.Split(content[4:4+end], "\n") {
		if m := key.FindStringSubmatch(line); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

func frontmatterValue(content, field string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, field+":") {
			return strings.TrimSpace(strings.TrimPrefix(line, field+":"))
		}
		if line == "---" && strings.Contains(content[:strings.Index(content, line)+1], field+":") {
			break
		}
	}
	return ""
}

// TestFrontmatterIsThePortableSet holds the one thing that decides whether a
// skill is seen at all. name, description, and metadata are the intersection
// every supported assistant accepts; anything outside that set errors on at
// least one of them.
func TestFrontmatterIsThePortableSet(t *testing.T) {
	canonical := bundleFile(t, "SKILL.md")

	pointerFiles, err := New("test").Files(Pointer)
	if err != nil {
		t.Fatal(err)
	}
	pointer := string(pointerFiles["SKILL.md"])

	want := []string{"name", "description", "metadata"}
	for _, file := range []struct {
		label   string
		content string
	}{{"SKILL.md", canonical}, {"pointer/SKILL.md", pointer}} {
		got := frontmatterKeys(t, file.content)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s frontmatter is %v, want exactly %v: anything else errors on at least one assistant", file.label, got, want)
		}
	}

	// The description is the activation trigger, so the pointer has to carry the
	// same one. A pointer that never activates is a pointer nobody follows.
	for _, field := range []string{"name", "description"} {
		if a, b := frontmatterValue(canonical, field), frontmatterValue(pointer, field); a != b {
			t.Errorf("the pointer's %s does not match the canonical one:\n  canonical: %s\n  pointer:   %s", field, a, b)
		}
	}
}

// TestNoSecretsInTheBundle holds the repository's hardest rule. The bundle
// teaches environment variable names and nothing else, so a value that looks
// like a credential is a defect wherever it appears.
func TestNoSecretsInTheBundle(t *testing.T) {
	credential := []struct {
		name    string
		pattern *regexp.Regexp
	}{
		{"an OpenAI-style key", regexp.MustCompile(`sk-[A-Za-z0-9_-]{16,}`)},
		{"an AWS access key id", regexp.MustCompile(`AKIA[0-9A-Z]{12,}`)},
		{"a GitHub token", regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{16,}`)},
		{"a Slack token", regexp.MustCompile(`xox[abprs]-[A-Za-z0-9-]{10,}`)},
		{"a bearer token literal", regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._-]{20,}`)},
		{"a long hex string", regexp.MustCompile(`\b[0-9a-f]{40,}\b`)},
	}
	// An E.164 number is a secret here too, and the only place one may appear is
	// a quoted refusal showing it being rejected.
	phone := regexp.MustCompile(`\+[1-9][0-9]{9,14}`)

	files, err := New("test").Files(Canonical)
	if err != nil {
		t.Fatal(err)
	}
	pointerFiles, err := New("test").Files(Pointer)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range pointerFiles {
		files["pointer/"+name] = content
	}

	for name, raw := range files {
		content := string(raw)
		for _, check := range credential {
			if hit := check.pattern.FindString(content); hit != "" {
				t.Errorf("%s contains %s (%q); the bundle carries environment variable names only", name, check.name, hit)
			}
		}
		for _, line := range strings.Split(content, "\n") {
			hit := phone.FindString(line)
			if hit == "" {
				continue
			}
			if strings.Contains(line, "literal") {
				continue // the documented refusal, which is the point of showing it
			}
			t.Errorf("%s carries the phone number %q outside a refusal example; a destination names an environment variable", name, hit)
		}
	}
}

// beginnerPath is every surface a first-time author meets before they have
// decided anything: the site's front door, the two sections that get them to a
// running agent, the repository README, everything `unmute init` writes, and the
// whole bundle a coding assistant reads. Modelled on TestNoSecretsInTheBundle,
// which is the same shape of prohibition over a whole tree.
func beginnerPath(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	site := filepath.Join("..", "..", "docs-site")
	for _, root := range []string{filepath.Join(site, "index.mdx"), filepath.Join(site, "start"), filepath.Join(site, "build")} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			out[filepath.ToSlash(path)] = string(content)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	out["README.md"] = string(readme)

	dir := filepath.Join(t.TempDir(), "hello-agent")
	created, err := scaffold.Write(dir, scaffold.Data{Name: "hello-agent", Tools: scaffold.DefaultTools()})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range created {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			t.Fatal(err)
		}
		out["unmute init/"+filepath.ToSlash(rel)] = string(content)
	}

	for _, form := range []Destination{Canonical, Pointer} {
		files, err := New("test").Files(form)
		if err != nil {
			t.Fatal(err)
		}
		for name, content := range files {
			out["bundle/"+name] = string(content)
		}
	}
	if len(out) < 10 {
		t.Fatalf("collected %d beginner-path files, so this test would pass for the wrong reason", len(out))
	}
	return out
}

// TestNoUnmuteEnvOnTheBeginnerPath keeps generated projects free of required
// Unmute runtime environment. UNMUTE_LOG_LEVEL is the sole optional operator
// knob; the generated process works without it.
func TestNoUnmuteEnvOnTheBeginnerPath(t *testing.T) {
	unmuteEnv := regexp.MustCompile(`\bUNMUTE_[A-Z0-9_]+\b`)
	for name, content := range beginnerPath(t) {
		for _, hit := range unmuteEnv.FindAllString(content, -1) {
			if hit != "UNMUTE_LOG_LEVEL" {
				t.Errorf("%s names unexpected Unmute runtime environment %s", name, hit)
			}
		}
	}
}

// TestOneModelIdEverywhere holds every author-facing surface to the single model
// identifier internal/scaffold owns. It fails on three things, not one: a stale
// identifier, a combined provider/model form, and a
// temperature on a think model, which OpenAI's reference does not state this
// model family accepts (research D10).
func TestOneModelIdEverywhere(t *testing.T) {
	// Two owned identifiers, both from internal/scaffold: the scaffold default
	// and the one the router example names. The comparison stays exact, so a
	// third identifier still fails. Widening the regexp, allowlisting a path, or
	// skipping a file would each let a stale identifier back in, which is the
	// ratchet rule (FR-032).
	want := []string{scaffold.DefaultReasonModel, scaffold.RouterExampleModel}
	for _, owned := range want {
		if owned == "" {
			t.Fatal("internal/scaffold owns the identifiers; an empty constant makes every check below vacuous")
		}
	}
	// Every OpenAI chat identifier shape, so a stale one is caught by shape and
	// not by a list somebody has to remember to extend.
	identifier := regexp.MustCompile(`\bgpt-[0-9][A-Za-z0-9.-]*\b`)
	combined := regexp.MustCompile(`\b(?:openai|slng)/gpt-[A-Za-z0-9.-]+\b`)

	for name, content := range authorFacingModelSurfaces(t) {
		for _, hit := range identifier.FindAllString(content, -1) {
			if !slices.Contains(want, hit) {
				t.Errorf("%s names the model %q; the owned identifiers are %v", name, hit, want)
			}
		}
		if hit := combined.FindString(content); hit != "" {
			t.Errorf("%s writes %q; provider and model are two fields, and a folded string reaches the SDK verbatim (SCHEMA N15)", name, hit)
		}
		for _, block := range thinkBlocks(content) {
			if strings.Contains(block, "temperature:") {
				t.Errorf("%s sets temperature on a think model; OpenAI does not state this family accepts it, so it stays off until it is verified", name)
			}
		}
	}
}

// authorFacingModelSurfaces is the 24-file set research D10 measured: everything
// an author reads or copies. Test fixtures, goldens, and the specs that record
// the drift as history are deliberately absent.
func authorFacingModelSurfaces(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	read := func(path string) {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		out[filepath.ToSlash(path)] = string(content)
	}
	repo := func(parts ...string) string {
		return filepath.Join(append([]string{"..", ".."}, parts...)...)
	}
	walk := func(root string, ext ...string) {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if entry.IsDir() && root == repo("examples") && entry.Name() == "build" {
				return fs.SkipDir
			}
			if err != nil || entry.IsDir() || !slices.Contains(ext, filepath.Ext(path)) {
				return err
			}
			read(path)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	walk(repo("examples"), ".yaml", ".md")
	walk(repo("docs-site"), ".mdx")
	walk(repo("docs"), ".md")
	read(repo("README.md"))
	read(repo("internal", "scaffold", "scaffold.go"))
	for _, name := range []string{"references/models.md", "references/package.md"} {
		out["bundle/"+name] = bundleFile(t, name)
	}
	return out
}

// thinkBlocks slices out each `think:` section of a YAML or fenced document, so
// a temperature on a speak entry is not mistaken for one on a reasoning model.
func thinkBlocks(content string) []string {
	var blocks []string
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "think:" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		var block []string
		for _, next := range lines[i+1:] {
			if strings.TrimSpace(next) == "" {
				block = append(block, next)
				continue
			}
			if len(next)-len(strings.TrimLeft(next, " ")) <= indent {
				break
			}
			block = append(block, next)
		}
		blocks = append(blocks, strings.Join(block, "\n"))
	}
	return blocks
}

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func containsProvider(list []target.Provider, want target.Provider) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// TestOneFrameworkVersionEverywhere is the version half of the same rule as
// TestOneModelIdEverywhere: what a release supports has one recorded home
// (target.Window), and no page an author reads may contradict it.
//
// The repository has been burned by this exact drift. Before the support window
// existed, the framework version lived in five unsynchronised places, and the
// examples disagreed with the docs, which disagreed with the tests: examples
// declared 1.6.4 while comments claimed verification against 1.6.9, because the
// emitted LiveKit pin floated instead of honouring what an author declared.
//
// Matching is by shape, not by a list of files someone has to remember to
// extend. Deliberately absent: goldens, internal/testdata fixtures, and specs/,
// which record older versions as history and must not fight this test.
func TestOneFrameworkVersionEverywhere(t *testing.T) {
	windows := target.Windows()
	if len(windows) == 0 {
		t.Fatal("internal/target owns the support window; an empty table makes every check below vacuous")
	}
	ceilings := map[string]target.Provider{}
	for provider, win := range windows {
		if win.Ceiling == "" {
			t.Fatalf("%s has no recorded ceiling", provider)
		}
		ceilings[win.Ceiling] = provider
	}

	// A dependency line naming a framework: the operator makes it a constraint
	// rather than prose, so dated verification notes ("verified against
	// livekit-agents 1.6.9") stay out of scope.
	constraint := regexp.MustCompile(`(livekit-agents|pipecat-ai)(\[[^\]]*\])?(==|>=|<=|~=)([0-9][0-9.]*)`)
	// A target's declared version in an authoring sample. Quoted and three-part,
	// so `version: 1` (the package schema version) and a project's own
	// `version = "0.1.0"` are not swept up.
	declared := regexp.MustCompile(`version:\s*"(\d+\.\d+\.\d+)"`)

	byPackage := map[string]target.Provider{}
	for provider := range windows {
		byPackage[target.FrameworkPackage(provider)] = provider
	}

	for name, content := range authorFacingVersionSurfaces(t) {
		for _, hit := range constraint.FindAllStringSubmatch(content, -1) {
			pkg, operator, version := hit[1], hit[3], hit[4]
			provider, ok := byPackage[pkg]
			if !ok {
				continue
			}
			win := windows[provider]
			if operator != "==" {
				t.Errorf("%s writes %q; a target installs exactly the version it declares, so an author-facing sample pins with == (%s==%s)",
					name, hit[0], pkg, win.Ceiling)
				continue
			}
			if version != win.Ceiling {
				t.Errorf("%s pins %s==%s; this release's %s ceiling is %s", name, pkg, version, pkg, win.Ceiling)
			}
		}
		for _, hit := range declared.FindAllStringSubmatch(content, -1) {
			if _, ok := ceilings[hit[1]]; !ok {
				t.Errorf("%s declares version %q, which is no framework's ceiling; the supported ceilings are %s",
					name, hit[1], strings.Join(sortedCeilings(windows), " and "))
			}
		}
	}
}

func sortedCeilings(windows map[target.Provider]target.SupportWindow) []string {
	var out []string
	for _, provider := range slices.Sorted(maps.Keys(windows)) {
		out = append(out, target.FrameworkPackage(provider)+" "+windows[provider].Ceiling)
	}
	return out
}

// authorFacingVersionSurfaces is everything an author reads or copies that could
// name a framework version: the model-id surfaces plus the rest of the skill
// bundle, which is what a coding assistant reads before it writes a package.
func authorFacingVersionSurfaces(t *testing.T) map[string]string {
	t.Helper()
	out := authorFacingModelSurfaces(t)
	for _, name := range []string{"references/telephony.md", "references/workflow.md", "references/conversation.md"} {
		out["bundle/"+name] = bundleFile(t, name)
	}
	return out
}

// TestRouterSurfacesSayOnlyWhatWasMeasured holds the SLNG Context Router's
// documentation against the four things Phase 0 either ruled out or could not
// confirm. Prose rots quietly, and each of these was true of a shipped surface at
// some point in this feature's history, so each gets a grep rather than a promise
// to remember (FR-025, FR-025a, FR-025b, FR-034e).
func TestRouterSurfacesSayOnlyWhatWasMeasured(t *testing.T) {
	surfaces := routerFacingSurfaces(t)
	if len(surfaces) == 0 {
		t.Fatal("no surface mentions the router, which makes every check below vacuous")
	}

	// FR-025. The body fields and that header were removed from the router, and
	// leak protection did not reproduce: a distinctive value supplied through
	// template_variables and echoed verbatim still cached and hit on repeat. None
	// of the three may appear anywhere shipped.
	for name, content := range surfaces {
		for _, banned := range []string{"cache_layer", "X-Cache-Layer", "leak protection"} {
			// x-slng-cache-layer is the response *header*, which is real and
			// documented; the removed body field and the removed X-Cache-Layer
			// header are what stay out.
			for _, hit := range findOutside(content, banned, "x-slng-cache-layer") {
				t.Errorf("%s writes %q, which was removed or never reproduced: %s", name, banned, hit)
			}
		}
		// FR-025a. The stored-org-configuration path never worked: two models
		// registered against the org still came back "no org config found" in all
		// four regions. The configuration travels inline now, so nothing shipped
		// may send a reader somewhere to register anything.
		//
		// "dashboard" alone is not bannable: LiveKit and Pipecat both have one and
		// the runbooks legitimately name them. What stays out is the pairing of a
		// registration verb with the model or the config.
		// The 400 the router returns when the inline configuration is missing does
		// contain the words "no org config found", and the runbooks quote it as a
		// symptom so a reader who sees it can search for it. Quoting an error is
		// not sending someone to register anything, so the ban is on the
		// instruction rather than on the string.
		for _, banned := range []string{
			"register the model", "register a model", "registered model",
			"register it against", "configure the model against", "byok", "slng dashboard",
		} {
			if strings.Contains(strings.ToLower(content), banned) {
				t.Errorf("%s mentions %q; the configuration travels inline and nothing is registered anywhere", name, banned)
			}
		}
		// FR-025b. Measured 2026-08-19: two pairs identical in everything a client
		// controls behaved differently, one caching on its second send and the
		// other never caching across eight while returning a byte-identical answer
		// each time. So a promise is banned outright, and a surface that explains
		// the cache at all has to carry the caveat rather than merely avoid the
		// promise. Requiring the caveat is the stronger half: a banned phrase list
		// can always be paraphrased around.
		for _, banned := range []string{
			"always cached", "always fast", "guaranteed cache", "every repeated turn is fast",
			"all repeats are fast", "will always come from",
		} {
			if strings.Contains(strings.ToLower(content), banned) {
				t.Errorf("%s promises %q; the router judges which turns are repeatable and some never cache", name, banned)
			}
		}
		// Any surface that mentions the cache at all carries a qualifier, however
		// short. "caches repeated turns" is an over-claim in a one-line pointer
		// just as much as in a page, and the shorter true version is no longer:
		// "caches the turns it judges repeatable".
		if strings.Contains(strings.ToLower(content), "cache") {
			qualified := false
			for _, marker := range []string{
				"judges", "a fault", "never cache", "some repeats", "decides which",
			} {
				if strings.Contains(strings.ToLower(content), marker) {
					qualified = true
				}
			}
			if !qualified {
				t.Errorf("%s mentions the cache with no qualifier; the router judges which turns are repeatable and a repeat served by the model is expected", name)
			}
		}
	}

	// FR-034e. Three of the four upstream provider kinds ship untested, so a
	// surface that lists the providers has to say which one has a live
	// measurement behind it. Otherwise it reads as though all five were run.
	for name, content := range surfaces {
		if !strings.Contains(content, "bedrock") {
			continue // not a surface that lists the upstreams
		}
		if !strings.Contains(content, "validated") && !strings.Contains(content, "exercised") &&
			!strings.Contains(content, "have not been run") {
			t.Errorf("%s lists the upstream providers without saying which kind was validated live", name)
		}
	}
}

// routerFacingSurfaces is every shipped surface that mentions the router: the
// docs site, the skill assets, the example READMEs, and both emitted README
// templates. The templates are included as source, because a claim that is only
// true in generated output is a claim the reader meets first.
func routerFacingSurfaces(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	repo := func(parts ...string) string {
		return filepath.Join(append([]string{"..", ".."}, parts...)...)
	}
	add := func(path string) {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(content), "Context Router") || strings.Contains(string(content), "context-router") {
			out[path] = string(content)
		}
	}
	for _, root := range []string{repo("docs-site"), repo("examples"), repo("internal", "generate", "templates")} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if entry != nil && entry.IsDir() && entry.Name() == "build" {
				return fs.SkipDir
			}
			if err != nil || entry.IsDir() {
				return err
			}
			switch filepath.Ext(path) {
			case ".md", ".mdx", ".tmpl":
				add(path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"references/models.md", "references/package.md"} {
		if content := bundleFile(t, name); strings.Contains(content, "Context Router") {
			out["bundle/"+name] = content
		}
	}
	return out
}

// findOutside returns each occurrence of needle that is not part of allowed, so a
// removed field name can be banned while the live header that contains it stays.
func findOutside(content, needle, allowed string) []string {
	var hits []string
	for _, line := range strings.Split(content, "\n") {
		if !strings.Contains(line, needle) {
			continue
		}
		if strings.Contains(strings.ToLower(line), allowed) {
			continue
		}
		hits = append(hits, strings.TrimSpace(line))
	}
	return hits
}

// The provider list is owned by Go (ir.TracingProviders), and the bundle and the
// public guide restate it. Constitution III says a fact stated twice gets an
// agreement test, so a provider added in Go without a matching doc edit fails
// here rather than shipping a skill that has never heard of it.
func TestTracingProvidersMatchTheCode(t *testing.T) {
	surfaces := map[string]string{
		"references/package.md":              bundleFile(t, "references/package.md"),
		"references/deploy.md":               bundleFile(t, "references/deploy.md"),
		"docs-site/reference/agent-yaml.mdx": trackedFile(t, "docs-site/reference/agent-yaml.mdx"),
		"docs-site/reference/secrets.mdx":    trackedFile(t, "docs-site/reference/secrets.mdx"),
		"docs-site/tracing/overview.mdx":     trackedFile(t, "docs-site/tracing/overview.mdx"),
	}
	for name, content := range surfaces {
		for _, provider := range ir.TracingProviders {
			if !strings.Contains(content, provider) {
				t.Errorf("%s does not name the tracing provider %q", name, provider)
			}
			// Naming the provider without naming what it needs leaves the reader
			// to discover the missing variable at run time.
			for _, secret := range ir.TracingSecrets[provider] {
				if !strings.Contains(content, secret) {
					t.Errorf("%s names %q without its required secret %q", name, provider, secret)
				}
			}
		}
	}
}

// Coval correlates a trace to a simulation, and the routes differ per target.
// A reader who does not know the route cannot make tracing work, so each surface
// that offers Coval has to say how the simulation ID arrives.
func TestCovalCorrelationRoutesStayDocumented(t *testing.T) {
	for name, content := range map[string]string{
		"references/package.md":                                 bundleFile(t, "references/package.md"),
		"docs-site/tracing/coval.mdx":                           trackedFile(t, "docs-site/tracing/coval.mdx"),
		"internal/generate/templates/livekit_v1/README.md.tmpl": trackedFile(t, "internal/generate/templates/livekit_v1/README.md.tmpl"),
		"internal/generate/templates/pipecat_v1/README.md.tmpl": trackedFile(t, "internal/generate/templates/pipecat_v1/README.md.tmpl"),
	} {
		for _, fact := range []string{"X-Coval-Simulation-Id", "COVAL_SIMULATION_ID"} {
			if !strings.Contains(content, fact) {
				t.Errorf("%s does not state %q, so the reader cannot wire correlation", name, fact)
			}
		}
	}
	// LiveKit Cloud runs the customer's own token server, so the emitted runbook
	// is the only place that can tell them what to forward.
	livekitReadme := trackedFile(t, "internal/generate/templates/livekit_v1/README.md.tmpl")
	for _, fact := range []string{"coval.simulation_id", "RoomAgentDispatch", "headers_to_attributes"} {
		if !strings.Contains(livekitReadme, fact) {
			t.Errorf("the LiveKit runbook does not tell the operator about %q", fact)
		}
	}
}

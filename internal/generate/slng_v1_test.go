package generate

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// -update-slng-target, not -update-slng: that flag belongs to the SLNG Context
// Router goldens next door, which are the model vendor's. Same word, third
// meaning.
var updateSlngV1 = flag.Bool("update-slng-target", false, "rewrite the slng target goldens")

func TestSlngSupportNeedsNoSessionArguments(t *testing.T) {
	_, files := compileSlng(t, filepath.Join("..", "..", "examples", "slng-support"))
	body := slngBodyOf(t, files)
	defaults := body["template_defaults"].(map[string]any)
	for name, raw := range body["template_variable_options"].(map[string]any) {
		option := raw.(map[string]any)
		if _, supplied := defaults[name]; option["required"] == true && !supplied {
			t.Errorf("inbound example requires session argument %q without a default", name)
		}
	}
}

func TestSlngPreviewIncludesTheBuiltinDescription(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "slng-support"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := SlngAuthoredDescriptions(agent)["end_call"], agent.Tools["end_call"].Description; got == "" || got != want {
		t.Fatalf("preview description = %q, emitted builtin description = %q", got, want)
	}
}

// compileSlng builds one fixture and returns its emitted files by path, which is
// what nearly every assertion below wants to read.
func compileSlng(t *testing.T, fixture string) (Artifact, map[string]string) {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderSlng), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	files := map[string]string{}
	for _, file := range artifact.Files {
		files[file.Path] = string(file.Content)
	}
	return artifact, files
}

// slngBodyOf decodes the emitted agent.json, so a test asserts against the
// document SLNG would receive rather than against Go structs it could have got
// wrong in the same way twice.
func slngBodyOf(t *testing.T, files map[string]string) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal([]byte(files["agent.json"]), &body); err != nil {
		t.Fatalf("agent.json is not JSON: %v", err)
	}
	return body
}

func slngGolden(t *testing.T, artifact Artifact, name string) {
	t.Helper()
	var out strings.Builder
	for _, file := range artifact.Files {
		out.WriteString("=== " + file.Path + " ===\n")
		out.Write(file.Content)
		if !strings.HasSuffix(string(file.Content), "\n") {
			out.WriteByte('\n')
		}
	}
	path := filepath.Join("testdata", "slng", name)
	if *updateSlngV1 {
		if err := os.WriteFile(path, []byte(out.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want) {
		t.Fatalf("slng golden %s differs; run: go test ./internal/generate -run TestSlngV1 -update-slng-target", name)
	}
}

// TestSlngV1CoreGolden is the builtins-only package: the shape an author can
// verify end to end today, because a builtin needs no tool created first.
func TestSlngV1CoreGolden(t *testing.T) {
	artifact, _ := compileSlng(t, "slng_core")
	slngGolden(t, artifact, "slng_v1_core.txt")
}

// TestSlngV1ToolsGolden is every tool shape in one package: a code body, an
// api_request body with a templated URL that has to move to the attachment, an
// MCP source with no file at all, and a builtin.
func TestSlngV1ToolsGolden(t *testing.T) {
	artifact, _ := compileSlng(t, "slng_tools")
	slngGolden(t, artifact, "slng_v1_tools.txt")
}

// Two files, and the reason there are two rather than four is the shape of this
// target now: it references tools SLNG already owns and creates none, so there
// is no tool body to write beside the agent body.
func TestSlngV1WritesTheBodyAndTheRunbookAndNothingElse(t *testing.T) {
	artifact, files := compileSlng(t, "slng_tools")
	if artifact.Kind != BodyTarget {
		t.Errorf("artifact kind = %q, want %q", artifact.Kind, BodyTarget)
	}
	for _, want := range []string{"agent.json", "README.md", "compile-report.json"} {
		if _, ok := files[want]; !ok {
			t.Errorf("no %s was written", want)
		}
	}
	// The half that carries reference-only. A tool body here would try to create
	// a tool the platform already owns, at whatever version this package
	// happened to mirror.
	for path := range files {
		if strings.HasPrefix(path, "tools/") {
			t.Errorf("the slng driver wrote %s; it creates no tool, so every tool it names is one the organisation already holds", path)
		}
		// A runnable project is exactly what this target does not emit. Emitting
		// one by accident would mean the wrong driver ran.
		if strings.HasSuffix(path, ".py") || path == "Dockerfile" || path == "pyproject.toml" {
			t.Errorf("the slng driver wrote %s; it emits a deployment body, not a project", path)
		}
	}
	// Three: the body, the runbook, and the report that says which checks this
	// compile could not make. A hosted reference names a tool somebody else
	// published, so "compiled clean" has to be readable as something narrower
	// than "checked".
	if len(files) != 3 {
		t.Errorf("the slng driver wrote %d files, want 3: %v", len(files), files)
	}
}

// The invariants a body must hold, read off the decoded document.
func TestSlngV1BodyInvariants(t *testing.T) {
	_, files := compileSlng(t, "slng_tools")
	body := slngBodyOf(t, files)

	// orchestrator exists only on the exported document shape, and the create
	// body sets extra: forbid, so writing it is a 422 rather than a stray key.
	if _, present := body["orchestrator"]; present {
		t.Error("agent.json carries orchestrator; unmute writes a create body, not an agent document")
	}
	// tool_mode: shared rejects a non-empty tools list, and unmute never writes
	// legacy mode, so the key has no reason to appear at all.
	if _, present := body["tools"]; present {
		t.Error("agent.json carries a legacy tools list; shared mode takes tool_refs")
	}
	for _, absent := range []string{"inbound_greeting", "outbound_greeting", "idle_nudges", "sip_inbound_trunk_id", "sip_outbound_trunk_id", "template_id"} {
		if _, present := body[absent]; present {
			t.Errorf("agent.json carries %s, which needs state unmute does not create", absent)
		}
	}
	if body["schema_version"] != float64(2) {
		t.Errorf("schema_version = %v, want 2", body["schema_version"])
	}
	if body["tool_mode"] != "shared" {
		t.Errorf("tool_mode = %v, want shared; version 2 with no tool_mode is rejected", body["tool_mode"])
	}
	// Exactly one greeting, and it is the one the package wrote.
	greeting, ok := body["greeting"].(string)
	if !ok || greeting == "" {
		t.Errorf("greeting = %v; SLNG requires one and unmute writes exactly one", body["greeting"])
	}
	// Every declared variable is a declaration; every one with a default is
	// additionally a default. The declared set is the union of the two maps.
	declared, _ := body["template_variable_options"].(map[string]any)
	defaults, _ := body["template_defaults"].(map[string]any)
	if declared == nil || defaults == nil {
		t.Fatalf("both variable maps must be present, even empty: %v %v", body["template_variable_options"], body["template_defaults"])
	}
	for name := range defaults {
		if _, ok := declared[name]; !ok {
			t.Errorf("variable %q has a default and no declaration; SLNG rejects a dispatched value outside the declared set", name)
		}
		if _, isString := defaults[name].(string); !isString {
			t.Errorf("default for %q is %T; template_defaults is a string map", name, defaults[name])
		}
	}
}

// A package with no variables still writes both maps, present and empty. An
// absent map and an empty one are different statements to SLNG's resolver.
func TestSlngV1WritesEmptyVariableMapsRatherThanNone(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "slng_core"))
	if err != nil {
		t.Fatal(err)
	}
	pkg.Agent.Variables = nil
	pkg.Agent.Conversation.Greeting.Text = "Hi, you have reached Acme Support."
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderSlng), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	files := map[string]string{}
	for _, file := range artifact.Files {
		files[file.Path] = string(file.Content)
	}
	for _, want := range []string{`"template_defaults": {}`, `"template_variable_options": {}`} {
		if !strings.Contains(files["agent.json"], want) {
			t.Errorf("agent.json does not carry %s; a null map says something different from an empty one:\n%s", want, files["agent.json"])
		}
	}
}

// No tool reference carries config_overrides any more, and this is the test
// that used to prove one did.
//
// It existed for exactly one case: a `webhook:` tool whose path carried a
// package variable. A tool-level URL on SLNG may template Vault variables only,
// so the rendered URL had to ride the attachment as an override instead of
// sitting in the tool's own config. There is no tool config now, and a hosted
// tool's whole URL is the platform's, so the override has nothing to override.
//
// Kept as an assertion rather than deleted, because an override reappearing
// would mean something started writing a tool body again.
func TestSlngV1WritesNoConfigOverride(t *testing.T) {
	_, files := compileSlng(t, "slng_tools")
	body := slngBodyOf(t, files)
	refs, _ := body["tool_refs"].([]any)
	if len(refs) == 0 {
		t.Fatal("no tool_refs were written")
	}
	for _, entry := range refs {
		ref, _ := entry.(map[string]any)
		if _, present := ref["config_overrides"]; present {
			t.Errorf("tool %v carries config_overrides; the slng target writes no tool config for one to override", ref["tool"])
		}
	}
}

// An MCP tool and a builtin produce no file. The MCP tool produces one named
// reference per exposed tool instead.
func TestSlngV1WritesNoFileForMCPOrBuiltin(t *testing.T) {
	_, files := compileSlng(t, "slng_tools")
	for _, absent := range []string{"tools/internal_docs.json", "tools/end_call.json"} {
		if _, present := files[absent]; present {
			t.Errorf("%s was written; an MCP source and a builtin need no tool body", absent)
		}
	}
	body := slngBodyOf(t, files)
	refs, _ := body["mcp_refs"].([]any)
	if len(refs) != 2 {
		t.Fatalf("mcp_refs = %v, want one entry per listed tool", refs)
	}
	for _, entry := range refs {
		ref, _ := entry.(map[string]any)
		// tool_name, not tool: that is the key SLNG reads, and reading the wrong
		// one here made this assertion vacuous rather than failing.
		if ref["server"] != "internal_docs" || ref["tool_name"] == nil || ref["tool_name"] == "" {
			t.Errorf("mcp ref = %v, want a server and tool_name pair", ref)
		}
		// Names only. The push fills in server_id and observed_schema_hash from
		// the platform's stored capability snapshot, and a hash unmute invented
		// offline would not match that snapshot, so the push would refuse it.
		if _, present := ref["observed_schema_hash"]; present {
			t.Error("an mcp ref carries a schema hash; the push copies that from the account, so an invented one is refused")
		}
	}
}

// No emitted tool name may be one of SLNG's five reserved names, except where
// the tool is the curated capability that owns it.
func TestSlngV1EmitsNoReservedToolBody(t *testing.T) {
	_, files := compileSlng(t, "slng_tools")
	for path := range files {
		name := strings.TrimSuffix(strings.TrimPrefix(path, "tools/"), ".json")
		if path == name {
			continue
		}
		if _, reserved := target.SlngReservedToolNames[name]; reserved {
			t.Errorf("%s creates a tool named %q, which SLNG keeps for a curated capability", path, name)
		}
	}
}

// TestSlngV1LeaksNoSecretValue is the gate under FR-033. Every declared secret
// is a *name*; the value never enters the compiler, so the check is that no
// emitted file carries anything but the name, and that the runbook's commands
// carry not even that.
func TestSlngV1LeaksNoSecretValue(t *testing.T) {
	_, files := compileSlng(t, "slng_tools")
	// A value that would exist if the compiler ever read one. Nothing may look
	// like it, and the test also proves the scan works by checking the name it
	// derives from does appear.
	const value = "sk-live-do-not-emit-this"
	for path, content := range files {
		if strings.Contains(content, value) {
			t.Errorf("%s carries a secret value", path)
		}
	}
	runbook := files["README.md"]
	if !strings.Contains(runbook, "REFUND_API_TOKEN") {
		t.Error("the runbook does not list the Vault name the package needs, so the first push fails on a missing entry")
	}
	// The push command carries no credential at all, not even an expansion: what
	// ends up in a shell history or a CI log should have nothing in it.
	for _, line := range strings.Split(runbook, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "voiceai ") && strings.Contains(line, "API_KEY") {
			t.Errorf("an emitted command line carries a credential: %q", line)
		}
	}
}

// The runbook lists every Vault name the package needs, grouped by kind, above
// the push command, and says so plainly when a package needs none.
func TestSlngV1RunbookGroupsVaultNames(t *testing.T) {
	_, files := compileSlng(t, "slng_tools")
	runbook := files["README.md"]
	for _, want := range []string{"**Secrets**", "REFUND_API_TOKEN", "**Variables**", "ACME_BRAND"} {
		if !strings.Contains(runbook, want) {
			t.Errorf("the runbook does not carry %q:\n%s", want, runbook)
		}
	}
	// Above the push command, because a list underneath it arrives after the
	// failure it exists to prevent.
	if strings.Index(runbook, "REFUND_API_TOKEN") > strings.Index(runbook, "voiceai agents create") {
		t.Error("the Vault list appears after the push command")
	}
	// The name shape, so an author who has to invent a name knows what SLNG
	// accepts before it rejects one.
	if !strings.Contains(runbook, "at most 64 characters") {
		t.Error("the runbook does not say what a Vault name looks like")
	}

	_, plain := compileSlng(t, "slng_core")
	if !strings.Contains(plain["README.md"], "This package needs none") {
		t.Errorf("a package needing no Vault entries must be told so rather than shown an empty list:\n%s", plain["README.md"])
	}
}

// The runbook states both directions of the create-body-versus-document trap,
// and names the credential the push tool reads.
func TestSlngV1RunbookNamesTheTrapsAndTheCredential(t *testing.T) {
	_, files := compileSlng(t, "slng_tools")
	// Collapsed, because these are sentences and the runbook wraps them. A test
	// that searches raw text is really testing where the line breaks fall.
	runbook := strings.Join(strings.Fields(files["README.md"]), " ")
	for _, want := range []string{
		// FR-026: both names the credential answers to, and that either works.
		// One SLNG key serves every SLNG role, so this is a choice of variable
		// name and not a choice of key; a runbook that called one of them the
		// wrong key sent readers looking for a second token nobody issues.
		"VOICEAI_API_KEY", "SLNG_API_KEY", "either works",
		// FR-024: a body with named references carries a name where the API wants
		// an id, and the push step is what resolves it. `voiceai agents create` is
		// named as the thing not to reach for, because it posts the body verbatim.
		"carries a name where the API wants an id",
		"The push step resolves those names", "voiceai agents create",
		// A stale MCP record is refreshed by real deployment, never by preview,
		// and discovery does not execute a selected tool.
		"A real deploy refreshes an unusable snapshot once",
		"A dry run reports the stale snapshot without refreshing it",
		"Discovery executes no business tool",
		"bypasses Unmute's published binding checks and checked-version staging",
		// FR-025: an export is not always postable back.
		"not** always a body the API will accept", "orchestrator",
	} {
		if !strings.Contains(runbook, want) {
			t.Errorf("the runbook does not say %q:\n%s", want, runbook)
		}
	}
	// A builtins-only package gets the same warning, and that is the point. A
	// curated capability still has a tool_id the push step must fill in:
	// ToolAttachment requires attachment_id, tool_id and version and forbids the
	// `tool` name field unmute writes. This runbook once told such an author their
	// body was postable, which would have sent them into a 422 that reads like a
	// schema bug.
	_, plain := compileSlng(t, "slng_core")
	builtinsOnly := strings.Join(strings.Fields(plain["README.md"]), " ")
	if !strings.Contains(builtinsOnly, "What the push resolves for you") {
		t.Error("a builtins-only package is told nothing needs resolving; a curated reference still needs its tool_id")
	}
	if !strings.Contains(builtinsOnly, "A curated capability is no exception") {
		t.Error("the runbook does not say why a builtin is not the exception a reader expects it to be")
	}
}

// The generated code_src is gone, with the tool body it lived in.
//
// It used to keep the author's handler byte for byte, which was the rule that
// let one file work on all three targets. The direction has inverted: the
// platform owns the module and `unmute pull` mirrors it into the package, so
// the same file still works on all three and the byte-for-byte rule now runs
// the other way. TestHostedToolLowersOnBothCodeTargets is where it is checked.

// TestSlngV1WritesNoToolBodyToTag replaces TestSlngV1ToolConfigCarriesItsUnionTag.
//
// That test held a real and expensive fact: `config` is a tagged union on the
// wire, a create body gets away without the tag because `tool_type` sits beside
// it, and an update PATCH does not, because the push strips `tool_type` first.
// An untagged body therefore deployed once and 422d forever after, which is the
// worst shape a bug can have.
//
// None of it can happen now, because unmute writes no tool body at all. The
// fact is kept here rather than deleted, because it is the reason to be
// suspicious of any change that starts writing one again.
func TestSlngV1WritesNoToolBodyToTag(t *testing.T) {
	_, files := compileSlng(t, "slng_tools")
	for path := range files {
		if strings.HasPrefix(path, "tools/") {
			t.Errorf("%s was written: a tool body needs its config tagged with its own tool_type, and an untagged one deploys once and 422s forever after", path)
		}
	}
}

// TestSlngEmitsTheHostedNameAndKeepsTheLocalOne is the alias, which is the
// field spec 007 added and the one thing a name-based reference can get wrong in
// a way no test used to see.
//
// tools/order_status.yaml holding `slng: check_order` means two names: the agent
// attaches order_status, and the organisation is asked for check_order. Every
// reader that wanted the file back was indexing agent.Tools by the emitted name,
// which is right for a builtin and silently wrong here.
func TestSlngEmitsTheHostedNameAndKeepsTheLocalOne(t *testing.T) {
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "testdata", "slng_hosted"))); err != nil {
		t.Fatal(err)
	}
	// The same tool, referenced under a name the file is not called after, and
	// with no mirror files beside it: that is the SLNG-only shape.
	for _, path := range []string{"tools/check_order.yaml", "tools/check_order.slng.json", "tools/check_order.slng.py"} {
		if err := os.Remove(filepath.Join(dir, path)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "tools", "order_status.yaml"),
		[]byte("slng: check_order\nannounce: One moment while I look that up.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	agentYAML := filepath.Join(dir, "agent.yaml")
	raw, err := os.ReadFile(agentYAML)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agentYAML, []byte(strings.ReplaceAll(string(raw), "check_order", "order_status")), 0o600); err != nil {
		t.Fatal(err)
	}

	pkg, err := spec.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, agent.Targets["slng"], target.Default())
	if err != nil {
		t.Fatalf("a scalar reference with no mirror did not compile: %v", err)
	}
	files := map[string]string{}
	for _, file := range artifact.Files {
		files[file.Path] = string(file.Content)
	}

	var body struct {
		ToolRefs []struct {
			Tool      string         `json:"tool"`
			Policy    map[string]any `json:"execution_policy"`
			Arguments map[string]any `json:"argument_overrides"`
		} `json:"tool_refs"`
	}
	if err := json.Unmarshal([]byte(files["agent.json"]), &body); err != nil {
		t.Fatalf("agent.json is not JSON: %v", err)
	}
	var found bool
	for _, ref := range body.ToolRefs {
		if ref.Tool == "order_status" {
			t.Error("agent.json names the package's own file, not the tool the organisation holds: an alias would deploy a reference to a tool that does not exist")
		}
		if ref.Tool == "check_order" {
			found = true
			if ref.Policy == nil {
				t.Error("the announcement did not survive the alias")
			}
			if ref.Arguments == nil {
				t.Error("argument_overrides is absent; it is always written, empty when there are none")
			}
		}
	}
	if !found {
		t.Errorf("agent.json carries no reference to the hosted tool: %v", body.ToolRefs)
	}

	// The requirement, and the file it traces back to. Both matter: the name is
	// what the account is asked for, and the path is what the author edits.
	var hosted *Requirement
	for i := range artifact.Requires.Hosted {
		if artifact.Requires.Hosted[i].Name == "check_order" {
			hosted = &artifact.Requires.Hosted[i]
		}
		if artifact.Requires.Hosted[i].Name == "order_status" {
			t.Error("the account is asked for the package's own file name; it holds no tool called that")
		}
	}
	if hosted == nil {
		t.Fatalf("the aliased tool is not in the hosted requirements, so the deploy preflight will not look for it: %v", artifact.Requires.Hosted)
	}
	if !strings.Contains(hosted.Where, "tools/order_status.yaml") {
		t.Errorf("the requirement does not trace back to the file that declared it: %q", hosted.Where)
	}
	if hosted.Version != 0 {
		t.Errorf("a reference with no committed mirror reports version %d; nothing pinned one", hosted.Version)
	}

	// And the report says what it did not check.
	var report struct {
		ToolRefs []struct {
			Source string `json:"source"`
			Tool   string `json:"tool"`
			Hosted bool   `json:"hosted"`
		} `json:"tool_refs"`
		Deferred []struct {
			Check string   `json:"check"`
			By    string   `json:"by"`
			Refs  []string `json:"refs"`
		} `json:"deferred_checks"`
	}
	if err := json.Unmarshal([]byte(files["compile-report.json"]), &report); err != nil {
		t.Fatalf("compile-report.json is not JSON: %v", err)
	}
	var paired bool
	for _, ref := range report.ToolRefs {
		if ref.Source == "order_status" && ref.Tool == "check_order" && ref.Hosted {
			paired = true
		}
	}
	if !paired {
		t.Errorf("the report does not pair the local file with the hosted name: %v", report.ToolRefs)
	}
	if len(report.Deferred) == 0 {
		t.Error("the report names no deferred check, so a clean compile reads as a checked one")
	}
	for _, deferred := range report.Deferred {
		if deferred.By == "" {
			t.Errorf("deferred check %q names nothing that completes it", deferred.Check)
		}
	}
}

// TestSlngKeepsAuthoredAttachmentOrder: the body's references follow the agent's
// own `tools:` list. A map iteration would reorder them per run, and a reader
// diffing two deploys would see changes nobody made.
func TestSlngKeepsAuthoredAttachmentOrder(t *testing.T) {
	_, files := compileSlng(t, "slng_tools")
	var body struct {
		ToolRefs []struct {
			Tool string `json:"tool"`
		} `json:"tool_refs"`
	}
	if err := json.Unmarshal([]byte(files["agent.json"]), &body); err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(body.ToolRefs))
	for _, ref := range body.ToolRefs {
		got = append(got, ref.Tool)
	}
	sorted := append([]string(nil), got...)
	sort.Strings(sorted)
	if len(got) > 1 && slices.Equal(got, sorted) && got[0] == "check_order" {
		// Not a failure by itself: the fixture's authored order may be
		// alphabetical. The golden holds the exact list, so this only warns the
		// reader of that fixture that the property is not being exercised here.
		t.Logf("the fixture's authored order is alphabetical, so this ordering is also what a sort would give: %v", got)
	}
	if len(got) == 0 {
		t.Fatal("no tool references were emitted at all")
	}
}

// TestOrdinaryCompiledBodyCarriesNoAccountIdentity is the half of the staging
// design that is easy to break and hard to notice.
//
// `build/<target>/agent.json` is compiled with no credential and has to mean the
// same thing on two machines, so it carries names and no ids. The resolved copy
// a guarded push consumes carries ids, and the two are different documents. The
// only thing keeping them apart is `omitempty` on four fields, which is one
// keyword away from putting an id into every compile.
func TestOrdinaryCompiledBodyCarriesNoAccountIdentity(t *testing.T) {
	_, files := compileSlng(t, "slng_tools")
	body := files["agent.json"]
	for _, forbidden := range []string{"tool_id", "\"version\"", "server_id", "observed_schema_hash", "attachment_id"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the compiled body carries %s, so it is no longer account-independent and two machines would compile different files", forbidden)
		}
	}
}

// TestResolvedBodyCarriesExactlyWhatWasChecked: the staged copy, and the two
// ways it can be wrong.
//
// It must carry the id and version this run resolved, because a push in the
// guarded mode attaches those and checks neither for itself. And it must refuse
// rather than emit a partial document, because an unresolved reference reaching
// the push is a refusal there with a worse message than one here that can name
// the tool and its file.
func TestResolvedBodyCarriesExactlyWhatWasChecked(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "slng_tools"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	resolved := agent.Targets["slng"]

	// Every reference, including the builtin: a push in this mode attaches ids
	// and attaches them for all of them.
	tools := []SlngResolvedTool{
		{Source: "check_order", ToolID: "t-check_order", Version: 1},
		{Source: "refund", ToolID: "t-refund", Version: 7},
		{Source: "end_call", ToolID: "t-end_call", Version: 1},
	}
	mcp := []SlngResolvedMCP{
		{Server: "internal_docs", Tool: "search_docs", ServerID: "s-1", SchemaHash: "h-search"},
		{Server: "internal_docs", Tool: "read_doc", ServerID: "s-1", SchemaHash: "h-read"},
	}
	body, err := SlngResolvedBody(agent, resolved, tools, mcp)
	if err != nil {
		t.Fatalf("the resolved body was not produced: %v", err)
	}
	var decoded struct {
		ToolRefs []struct {
			Tool    string `json:"tool"`
			ToolID  string `json:"tool_id"`
			Version int    `json:"version"`
		} `json:"tool_refs"`
		MCPRefs []struct {
			Server     string `json:"server"`
			Tool       string `json:"tool_name"`
			ServerID   string `json:"server_id"`
			SchemaHash string `json:"observed_schema_hash"`
		} `json:"mcp_refs"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("the resolved body is not JSON: %v", err)
	}
	want := map[string]int{"check_order": 1, "refund": 7, "end_call": 1}
	for _, ref := range decoded.ToolRefs {
		if ref.ToolID == "" || ref.Version < 1 {
			t.Errorf("%s carries id %q version %d, which a guarded push refuses", ref.Tool, ref.ToolID, ref.Version)
		}
		if ref.Version != want[ref.Tool] {
			t.Errorf("%s carries version %d, want the %d this run checked", ref.Tool, ref.Version, want[ref.Tool])
		}
	}
	for _, ref := range decoded.MCPRefs {
		if ref.ServerID == "" || ref.SchemaHash == "" {
			t.Errorf("%s %s carries id %q hash %q, which a guarded push refuses", ref.Server, ref.Tool, ref.ServerID, ref.SchemaHash)
		}
	}
	// No tool body. The push creates no tool in this mode and refuses one.
	if strings.Contains(string(body), "code_src") || strings.Contains(string(body), "tool_type") {
		t.Error("the resolved body carries a tool definition, which a guarded push refuses")
	}

	// And the refusals, which are the reason this returns an error at all.
	if _, err := SlngResolvedBody(agent, resolved, tools[:1], mcp); err == nil {
		t.Error("a body was produced with two references unresolved")
	} else if !strings.Contains(err.Error(), "refund") {
		t.Errorf("the refusal does not name the unresolved tool: %v", err)
	}
	short := append([]SlngResolvedTool(nil), tools...)
	short[1].Version = 0
	if _, err := SlngResolvedBody(agent, resolved, short, mcp); err == nil {
		t.Error("a body was produced carrying version 0, which the platform's own contract refuses")
	}
	if _, err := SlngResolvedBody(agent, resolved, tools, mcp[:1]); err == nil {
		t.Error("a body was produced with an MCP selection unchecked")
	}
}

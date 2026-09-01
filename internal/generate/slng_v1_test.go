package generate

import (
	"encoding/json"
	"flag"
	"maps"
	"os"
	"path/filepath"
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

func TestSlngV1WritesTheThreeArtifactKinds(t *testing.T) {
	artifact, files := compileSlng(t, "slng_tools")
	if artifact.Kind != BodyTarget {
		t.Errorf("artifact kind = %q, want %q", artifact.Kind, BodyTarget)
	}
	for _, want := range []string{"agent.json", "README.md", "tools/check_order.json", "tools/refund.json"} {
		if _, ok := files[want]; !ok {
			t.Errorf("no %s was written", want)
		}
	}
	// A runnable project is exactly what this target does not emit. Emitting one
	// by accident would mean the wrong driver ran.
	for path := range files {
		if strings.HasSuffix(path, ".py") || path == "Dockerfile" || path == "pyproject.toml" {
			t.Errorf("the slng driver wrote %s; it emits a deployment body, not a project", path)
		}
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

// A code tool never carries config_overrides, because ToolConfigOverrides is a
// seven-member union with no code member. A webhook tool with a templated path
// must carry one, because a tool-level URL may template Vault variables only.
func TestSlngV1ConfigOverridesFollowTheToolType(t *testing.T) {
	_, files := compileSlng(t, "slng_tools")
	body := slngBodyOf(t, files)
	refs, _ := body["tool_refs"].([]any)
	if len(refs) == 0 {
		t.Fatal("no tool_refs were written")
	}
	seen := map[string]map[string]any{}
	for _, entry := range refs {
		ref, _ := entry.(map[string]any)
		name, _ := ref["tool"].(string)
		seen[name] = ref
	}
	if _, present := seen["check_order"]["config_overrides"]; present {
		t.Error("the code tool carries config_overrides; SLNG has no override shape for a code tool")
	}
	override, _ := seen["refund"]["config_overrides"].(map[string]any)
	if override == nil {
		t.Fatal("the webhook tool's templated URL is missing from config_overrides")
	}
	if override["type"] != "api_request" {
		t.Errorf("config override type = %v, want api_request", override["type"])
	}
	if url, _ := override["url"].(string); !strings.Contains(url, "{{customer_id}}") {
		t.Errorf("config override url = %q; the input token belongs here, not in the tool body", url)
	}
	// And the reverse: the tool's own config carries the base with no token,
	// because ApiRequestConfig.validate_request refuses a non-Vault token there.
	var tool map[string]any
	if err := json.Unmarshal([]byte(files["tools/refund.json"]), &tool); err != nil {
		t.Fatal(err)
	}
	config, _ := tool["config"].(map[string]any)
	if url, _ := config["url"].(string); strings.Contains(url, "{{") {
		t.Errorf("the tool body's own url carries a token: %q", url)
	}
	// Auth reaches auth:, never a header: authorization is a protected header
	// name SLNG rejects outright.
	auth, _ := config["auth"].(map[string]any)
	if auth["type"] != "bearer" || auth["secret_name"] != "REFUND_API_TOKEN" {
		t.Errorf("webhook auth = %v, want a bearer secret_name", auth)
	}
	if headers, _ := config["headers"].([]any); len(headers) != 0 {
		t.Errorf("headers = %v; auth never becomes an Authorization header", headers)
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
		// R12: an MCP reference is a lookup like the others. The runbook has to say
		// nothing connects to the server, because it said the opposite for a year.
		"nothing here connects to the server",
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

// The generated code_src keeps the author's file byte for byte, which is the
// rule that lets one handler work on all three targets.
func TestSlngV1CodeSourceKeepsTheAuthorsFileUnchanged(t *testing.T) {
	_, files := compileSlng(t, "slng_tools")
	var tool map[string]any
	if err := json.Unmarshal([]byte(files["tools/check_order.json"]), &tool); err != nil {
		t.Fatal(err)
	}
	source, _ := tool["code_src"].(string)
	authored, err := os.ReadFile(filepath.Join("..", "testdata", "slng_tools", "tools", "check_order.py"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(source, strings.TrimRight(string(authored), "\n")) {
		t.Errorf("code_src does not open with the author's file unchanged:\n%s", source)
	}
	for _, want := range []string{
		"class Input(BaseModel):",
		"    order_id: str",
		"class Output(BaseModel):",
		"def handler(input: Input) -> Output:",
		"result = check_order(order_id=input.order_id)",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("code_src is missing %q:\n%s", want, source)
		}
	}
	if tool["tool_type"] != "code" {
		t.Errorf("tool_type = %v, want code", tool["tool_type"])
	}
}

// TestSlngV1ToolConfigCarriesItsUnionTag. `config` is a tagged union on the wire
// and the tag is the tool's own type. A create body gets away without it, because
// `tool_type` sits beside `config` and the API infers the tag; an update does not,
// because the push step strips `tool_type` before it PATCHes (a tool's type cannot
// change after creation).
//
// So an untagged body deploys once and fails on every deploy after it. That is the
// worst shape a bug can have: it passes the first time you try it.
//
// Measured against api.agents.slng.ai on 2026-08-27 — the same PATCH is 422
// without the tag and 200 with it, and SLNG stores the tag on read-back. The
// union's members, from the API's own 422: code, api_request, end_call,
// voicemail_detection, transfer_call, send_sms, current_datetime,
// user_phone_number. The two below are the ones this driver emits.
func TestSlngV1ToolConfigCarriesItsUnionTag(t *testing.T) {
	_, files := compileSlng(t, "slng_tools")
	found := map[string]string{}
	for path, content := range files {
		if !strings.HasPrefix(path, "tools/") {
			continue
		}
		var body struct {
			Name     string `json:"name"`
			ToolType string `json:"tool_type"`
			Config   struct {
				Type string `json:"type"`
			} `json:"config"`
		}
		if err := json.Unmarshal([]byte(content), &body); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if body.ToolType == "" {
			t.Errorf("%s carries no tool_type", path)
			continue
		}
		if body.Config.Type != body.ToolType {
			t.Errorf("%s has tool_type %q and config.type %q; the union tag is the tool's own type, and an update PATCH has nothing else to infer it from",
				path, body.ToolType, body.Config.Type)
		}
		found[body.Name] = body.ToolType
	}
	// Both shapes this driver emits have to be in the sample, or the assertion
	// above could pass by covering only one of them.
	if want := map[string]string{"check_order": "code", "refund": "api_request"}; !maps.Equal(found, want) {
		t.Errorf("emitted tool bodies = %v, want %v", found, want)
	}
}

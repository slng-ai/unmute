package generate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The published SLNG conformance fixtures and one real exported agent, held
// against what this compiler produces. Offline: they are vendored under
// testdata/slng/contracts/v1/ with a digests.txt beside them, and `make
// contracts` re-fetches and compares. Nothing here touches the network.
//
// Two things are checked, and they fail for different reasons.
//
// The **field shapes** come from the fixtures. A fixture is not a golden of
// unmute's output — it carries identifiers unmute cannot invent and fields only
// the exported document shape has — so the comparison is per field, over the
// fields unmute owns. This is what caught pre_action_message being an object
// rather than a string.
//
// The **absences** come from the create body's extra: forbid. A field the
// document has and the create body does not is a 422, so the test that unmute
// never writes one is as load-bearing as the test that it writes the rest.

func readSlngFixture(t *testing.T, path ...string) map[string]any {
	t.Helper()
	full := filepath.Join(append([]string{"testdata", "slng"}, path...)...)
	content, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(content, &value); err != nil {
		t.Fatalf("%s: %v", full, err)
	}
	return value
}

// TestSlngMatchesThePublishedPositiveFixture holds the emitted tool reference
// shape against the published one. The fixture exercises the corners: a
// system-invoked attachment, a pre-action message, an argument override, a
// config-override URL carrying an input token, and an MCP row.
func TestSlngMatchesThePublishedPositiveFixture(t *testing.T) {
	fixture := readSlngFixture(t, "contracts", "v1", "positive", "agent_config_v2.json")
	_, files := compileSlng(t, "slng_tools")
	body := slngBodyOf(t, files)

	// The scalar fields unmute owns must be the same kind of thing in both.
	for _, field := range []string{"schema_version", "tool_mode", "region", "language", "greeting", "system_prompt", "enable_interruptions"} {
		want, ours := fixture[field], body[field]
		if want == nil {
			t.Fatalf("the fixture no longer carries %q; re-read it before trusting this test", field)
		}
		if ours == nil {
			t.Errorf("unmute writes no %q, and the published body carries one", field)
			continue
		}
		if got, expected := jsonKind(ours), jsonKind(want); got != expected {
			t.Errorf("%s is %s in our body and %s in the published one", field, got, expected)
		}
	}

	// The pre-action message. It is an object with a segmented text, not a
	// string, and getting that wrong is a 422 that reads like a schema problem.
	fixtureRef := firstRefWithPolicy(t, fixture)
	ourRef := firstRefWithPolicy(t, body)
	comparePolicyShape(t, ourRef, fixtureRef)

	// An argument override is a plain map of name to value on both, and it is
	// present-and-empty rather than absent when a tool injects nothing. Checked on
	// every reference, because ours spreads the corners across two tools: a code
	// tool can carry a policy and never a config override, and the reverse.
	for _, item := range body["tool_refs"].([]any) {
		ref, _ := item.(map[string]any)
		if kind := jsonKind(ref["argument_overrides"]); kind != "object" {
			t.Errorf("tool %v: argument_overrides is %s; the published body carries an object", ref["tool"], kind)
		}
	}

	// A config override for an api_request tool carries a type discriminator and
	// a URL that may hold an input token. The tool body's own URL may not.
	override := findAPIRequestOverride(t, body)
	if override["type"] != "api_request" {
		t.Errorf("config override type = %v, want the api_request discriminator", override["type"])
	}
	if url, _ := override["url"].(string); !strings.Contains(url, "{{") {
		t.Errorf("config override url = %q; the published fixture puts the input token here", url)
	}

	// llm_router_enabled is the one field the published body carries and unmute
	// deliberately does not. Writing it false refused every BYOK model in the
	// organisation, measured 2026-08-25; leaving it out lets SLNG apply the default
	// an agent made in its own dashboard would get.
	if _, present := fixture["llm_router_enabled"]; !present {
		t.Fatal("the fixture no longer carries llm_router_enabled; re-read it before trusting this test")
	}
	if _, present := body["llm_router_enabled"]; present {
		t.Error("unmute writes llm_router_enabled; nothing in a package describes SLNG's server-side routing, and stating it off refuses the organisation's own models")
	}

	// The MCP row. Ours has names where the fixture has identifiers, and it is
	// missing the hash on purpose: the push copies that out of the platform's own
	// stored capability snapshot, so a value unmute made up offline is one the
	// push would compare against that snapshot and refuse.
	ourMCP := firstEntry(t, body, "mcp_refs")
	fixtureMCP := firstEntry(t, fixture, "mcp_refs")
	if _, ok := fixtureMCP["tool_name"]; !ok {
		t.Fatal("the fixture's mcp row no longer names tool_name; re-read it")
	}
	if _, ok := ourMCP["tool_name"]; !ok {
		t.Errorf("our mcp row does not carry tool_name: %v", ourMCP)
	}
	if _, present := ourMCP["observed_schema_hash"]; present {
		t.Error("our mcp row carries a schema hash; the push copies that from the account")
	}
}

// TestSlngReproducesTheRealExportedAgent is the other golden, and it fails for a
// different reason from the fixture above: not "the two repositories drifted"
// but "unmute stopped producing something the dashboard would recognise".
//
// A real export is the document shape, so it carries fields the create body
// forbids. What is compared is the set unmute owns, and what is asserted about
// the rest is that unmute writes none of it.
func TestSlngReproducesTheRealExportedAgent(t *testing.T) {
	export := readSlngFixture(t, "support-users.agent.json")
	_, files := compileSlng(t, "slng_core")
	body := slngBodyOf(t, files)

	for _, field := range []string{"schema_version", "tool_mode", "runtime_variables", "template_defaults", "template_variable_options", "models", "tool_refs", "mcp_refs"} {
		if _, ok := export[field]; !ok {
			t.Fatalf("the real export no longer carries %q; re-read it before trusting this test", field)
		}
		if _, ok := body[field]; !ok {
			t.Errorf("unmute writes no %q, and a real SLNG agent carries one", field)
		}
	}
	// The models block, field by field. A missing one here is an agent that
	// saves and then cannot hear, speak or think.
	exportModels, _ := export["models"].(map[string]any)
	ourModels, _ := body["models"].(map[string]any)
	for _, field := range []string{"stt", "llm", "tts", "tts_voice", "stt_kwargs", "llm_kwargs", "tts_kwargs"} {
		if _, ok := exportModels[field]; !ok {
			t.Fatalf("the real export's models block no longer carries %q", field)
		}
		if _, ok := ourModels[field]; !ok {
			t.Errorf("our models block has no %q", field)
		}
	}
	// A model string reaches SLNG with the vendor and the model in one string,
	// which is the shape the export shows and the reason the driver folds.
	for _, field := range []string{"stt", "llm", "tts"} {
		value, _ := ourModels[field].(string)
		if !strings.Contains(value, "/") {
			t.Errorf("models.%s = %q, and SLNG names a model with its vendor joined by a slash", field, value)
		}
	}
	// Every field the export has and the create body forbids.
	for _, field := range []string{"orchestrator", "tools", "idle_nudges", "inbound_greeting", "outbound_greeting"} {
		if _, present := body[field]; present {
			t.Errorf("unmute writes %q, which belongs to the exported document and not to a create body", field)
		}
	}
}

// TestSlngNegativeFixturesAreRejectable reads all eleven published negative
// fixtures and holds this compiler's rules against what each one is a case of.
//
// They are not packages, so unmute cannot be handed one directly. What they can
// do is name a rule: every fixture describes a shape the platform refuses, and
// for each shape this repository either refuses it too, or cannot express it at
// all. The mapping below is the value: an unmapped fixture is a rule this
// compiler has not thought about, and adding one upstream fails this test.
func TestSlngNegativeFixturesAreRejectable(t *testing.T) {
	// fixture name -> how this compiler makes the shape impossible.
	covered := map[string]string{
		"agent_config_legacy_tool_leaks.json":     "unmute never writes a legacy tools list; TestSlngV1BodyInvariants asserts the key is absent",
		"agent_config_mixed_mode.json":            "tool_mode is the constant \"shared\"; there is no code path that writes legacy mode",
		"dynamic_url_scheme.json":                 "ir.validateWebhookBaseURL refuses a base_url that is not https, and refuses any template token in it",
		"dynamic_url_authority.json":              "same rule: a template token anywhere in base_url is refused, so the authority can never be templated",
		"dynamic_url_host.json":                   "same rule, plus base_url with no host is refused by name",
		"dynamic_url_query_name.json":             "a package's webhook path is appended whole; unmute writes no query parameter names, templated or otherwise",
		"mcp_system_invocation.json":              "unmute writes invocation \"model\" only; slngInvocation is a constant and no package field selects system invocation",
		"runtime_code_call_end.json":              "unmute uses no system events at all, so no code tool can be wired to call_end",
		"runtime_backend_proxy_invalid_name.json": "every emitted tool name is a package tool name, which ir.Validate has already shape-checked",
		"runtime_backend_proxy_leaks_source.json": "unmute writes tool bodies and agent bodies, never a runtime proxy payload",
		"tool_execution_noncanonical_uuid.json":   "unmute writes no UUID anywhere: every identifier slot carries a name for the push step to resolve",
	}
	root := filepath.Join("testdata", "slng", "contracts", "v1", "negative")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		seen[name] = true
		if _, ok := covered[name]; !ok {
			t.Errorf("negative fixture %s has no rule in this compiler; either it names a shape unmute can emit, in which case add the refusal, or it cannot, in which case say why here", name)
		}
		// Each one must still parse, or the vendored copy is broken rather than
		// the compiler.
		readSlngFixture(t, "contracts", "v1", "negative", name)
	}
	for name := range covered {
		if !seen[name] {
			t.Errorf("this test claims to cover %s, which is no longer vendored", name)
		}
	}
	if len(seen) != 11 {
		t.Errorf("the vendored negative set has %d fixtures, and the published contract had 11 on 2026-08-25", len(seen))
	}
}

// TestSlngVendoredFixturesHaveDigests is the offline half of the drift check.
// `make contracts` re-fetches and compares; this proves the file it compares
// against is present and covers every vendored file, so a fixture added by hand
// with no digest cannot slip past the refresh.
func TestSlngVendoredFixturesHaveDigests(t *testing.T) {
	root := filepath.Join("testdata", "slng", "contracts", "v1")
	content, err := os.ReadFile(filepath.Join(root, "digests.txt"))
	if err != nil {
		t.Fatalf("no digests.txt beside the vendored fixtures: %v", err)
	}
	listed := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(content)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Errorf("digests.txt line is not a sha256 and a path: %q", line)
			continue
		}
		if len(fields[0]) != 64 {
			t.Errorf("digests.txt carries a %d character digest, want sha256: %q", len(fields[0]), fields[0])
		}
		listed[fields[1]] = true
	}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() == "digests.txt" {
			return err
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if !listed[filepath.ToSlash(relative)] {
			t.Errorf("%s is vendored with no digest, so `make contracts` would never notice it drifting", relative)
		}
		delete(listed, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for path := range listed {
		t.Errorf("digests.txt lists %s, which is not vendored", path)
	}
}

// --- small readers, so the tests above read as assertions ------------------

func jsonKind(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case bool:
		return "bool"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "unknown"
}

func firstEntry(t *testing.T, body map[string]any, field string) map[string]any {
	t.Helper()
	list, _ := body[field].([]any)
	if len(list) == 0 {
		t.Fatalf("%s is empty", field)
	}
	entry, _ := list[0].(map[string]any)
	return entry
}

func firstRefWithPolicy(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	list, _ := body["tool_refs"].([]any)
	for _, item := range list {
		ref, _ := item.(map[string]any)
		if policy, _ := ref["execution_policy"].(map[string]any); policy != nil {
			return ref
		}
	}
	t.Fatal("no tool reference carries an execution policy")
	return nil
}

func findAPIRequestOverride(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	list, _ := body["tool_refs"].([]any)
	for _, item := range list {
		ref, _ := item.(map[string]any)
		override, _ := ref["config_overrides"].(map[string]any)
		if override["type"] == "api_request" {
			return override
		}
	}
	t.Fatal("no tool reference carries an api_request config override")
	return nil
}

// comparePolicyShape holds our execution policy against the published one, key
// by key, one level down. The nesting is the whole point: pre_action_message
// looks like a string until you read a real body.
func comparePolicyShape(t *testing.T, ours, published map[string]any) {
	t.Helper()
	ourPolicy, _ := ours["execution_policy"].(map[string]any)
	theirPolicy, _ := published["execution_policy"].(map[string]any)
	ourMessage, _ := ourPolicy["pre_action_message"].(map[string]any)
	theirMessage, _ := theirPolicy["pre_action_message"].(map[string]any)
	if theirMessage == nil {
		t.Fatal("the published fixture's pre_action_message is no longer an object; re-read it")
	}
	if ourMessage == nil {
		t.Fatalf("our pre_action_message is %s, and the published one is an object with a segmented text", jsonKind(ourPolicy["pre_action_message"]))
	}
	for field, want := range theirMessage {
		if jsonKind(ourMessage[field]) != jsonKind(want) {
			t.Errorf("pre_action_message.%s is %s in our body and %s in the published one", field, jsonKind(ourMessage[field]), jsonKind(want))
		}
	}
	// And the segment carries the announce sentence, not a placeholder.
	text, _ := ourMessage["text"].(map[string]any)
	segments, _ := text["segments"].([]any)
	if len(segments) != 1 {
		t.Fatalf("want one literal segment, got %v", segments)
	}
	segment, _ := segments[0].(map[string]any)
	if segment["type"] != "literal" || segment["value"] == "" {
		t.Errorf("segment = %v, want a literal carrying the package's announce sentence", segment)
	}
}

package ir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	packagespec "github.com/slng-ai/unmute/internal/spec"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// loadSlngCore is the passing baseline every refusal test below starts from. It
// matters that it passes: a test that breaks one thing and reads one error knows
// the error came from the thing it broke.
func loadSlngCore(t *testing.T) *packagespec.Package {
	t.Helper()
	pkg, err := packagespec.Load(filepath.Join("..", "testdata", "slng_core"))
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func slngAgent(t *testing.T) *Agent {
	t.Helper()
	agent, err := Build(loadSlngCore(t))
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

// validateSlng runs one slng target and returns its row, so a test reads the
// errors rather than the error.
func validateSlng(t *testing.T, agent *Agent) TargetValidation {
	t.Helper()
	report, _ := Validate(agent, []Target{targetFor(agent, ProviderSlng)}, targetcap.Default())
	return reportFor(report, ProviderSlng)
}

func wantSlngError(t *testing.T, row TargetValidation, fragments ...string) {
	t.Helper()
	joined := strings.Join(row.Errors, "\n")
	for _, fragment := range fragments {
		if !strings.Contains(joined, fragment) {
			t.Errorf("no error contains %q; got:\n%s", fragment, joined)
		}
	}
	for _, message := range row.Errors {
		if !strings.HasPrefix(message, "slng target") {
			continue
		}
		if !strings.Contains(message, ": ") {
			t.Errorf("slng error names no alternative: %q", message)
		}
	}
}

func TestSlngCoreValidatesClean(t *testing.T) {
	row := validateSlng(t, slngAgent(t))
	if len(row.Errors) > 0 || len(row.Warnings) > 0 {
		t.Fatalf("the slng baseline must be clean: errors=%#v warnings=%#v", row.Errors, row.Warnings)
	}
}

// The four settings that used to pass in silence (research R5). Each is refused
// by name, because "your version field reached no artifact" is only useful if the
// author is told which field.
func TestSlngRefusesProjectOnlySettings(t *testing.T) {
	for _, test := range []struct {
		name    string
		mutate  func(*Target)
		wantOne string
	}{
		{"version", func(target *Target) { target.Version = "1.6.10" }, "does not take version"},
		{"pins", func(target *Target) { target.Pins = map[string]string{"livekit-agents": "1.6.10"} }, "does not take pins"},
		{"sdk_language", func(target *Target) { target.SDKLanguage = "python" }, "does not take sdk_language"},
		{"connection", func(target *Target) { target.Connection = "primary_phone" }, "does not take connection"},
	} {
		t.Run(test.name, func(t *testing.T) {
			agent := slngAgent(t)
			target := targetFor(agent, ProviderSlng)
			test.mutate(&target)
			report, err := Validate(agent, []Target{target}, targetcap.Default())
			if err == nil {
				t.Fatalf("%s passed in silence on a target that emits no project", test.name)
			}
			wantSlngError(t, reportFor(report, ProviderSlng), test.wantOne)
		})
	}
}

// The only region *value* check in the tree. validateRegions catches an empty or
// duplicated entry and forwards everything else, because every other platform
// owns its own region names.
func TestSlngRefusesRegionsOutsideTheFour(t *testing.T) {
	for _, test := range []struct {
		name    string
		regions []string
		want    string
	}{
		{"unknown", []string{"eu-west"}, `does not deploy to region "eu-west"`},
		{"absent", nil, "requires a deployment_region"},
		{"two", []string{"us-east", "eu-central"}, "takes exactly one deployment region"},
		{"two and one wrong", []string{"us-east", "atlantis"}, `does not deploy to region "atlantis"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			agent := slngAgent(t)
			target := targetFor(agent, ProviderSlng)
			target.DeploymentRegions = test.regions
			report, err := Validate(agent, []Target{target}, targetcap.Default())
			if err == nil {
				t.Fatalf("regions %v passed", test.regions)
			}
			row := reportFor(report, ProviderSlng)
			wantSlngError(t, row, test.want)
			// Every region message lists the accepted values, because the useful
			// half of "that one is wrong" is which ones are right.
			if test.name != "two" && !strings.Contains(strings.Join(row.Errors, "\n"), "ap-south") {
				t.Errorf("the message does not list the accepted regions: %#v", row.Errors)
			}
		})
	}
}

func TestSlngRefusesReservedToolNamesButNotTheBuiltinThatOwnsOne(t *testing.T) {
	// end_call is reserved, and `builtin: end_call` is how you reach the curated
	// capability that reserves it. The baseline carries exactly that and passes.
	if row := validateSlng(t, slngAgent(t)); len(row.Errors) > 0 {
		t.Fatalf("builtin: end_call was refused for using the name it owns: %#v", row.Errors)
	}
	agent := slngAgent(t)
	agent.Tools["get_current_datetime"] = Tool{
		Description: "Return the current date and time.",
		Execution:   ToolWebhook,
		URLEnv:      "CLOCK_URL",
	}
	entry := agent.Agents["support"]
	entry.Tools = append(entry.Tools, "get_current_datetime")
	agent.Agents["support"] = entry
	row := validateSlng(t, agent)
	wantSlngError(t, row, `tool "get_current_datetime" uses a name SLNG keeps`, "current_datetime")
}

func TestSlngRefusesANetworkImportInALocalHandler(t *testing.T) {
	for _, source := range []string{
		"import httpx\n\n\ndef check_order(order_id: str) -> dict:\n    return {}\n",
		"import json\nfrom requests import get\n\n\ndef check_order(order_id: str) -> dict:\n    return {}\n",
		"import urllib.request\n\n\ndef check_order(order_id: str) -> dict:\n    return {}\n",
	} {
		agent := slngAgent(t)
		addSlngLocalTool(agent, "check_order", source)
		row := validateSlng(t, agent)
		wantSlngError(t, row, "has no internet access", "webhook:")
	}
	// The stdlib is fine, and a handler that uses it must not be refused.
	agent := slngAgent(t)
	addSlngLocalTool(agent, "check_order", "import json\nimport datetime\n\n\ndef check_order(order_id: str) -> dict:\n    return {\"ok\": True}\n")
	if row := validateSlng(t, agent); len(row.Errors) > 0 {
		t.Errorf("a stdlib-only handler was refused: %#v", row.Errors)
	}
}

// SLNG calls a handler synchronously; LiveKit awaits an awaitable. Only the entry
// point matters, so a sync handler that runs its own loop inside is left alone.
func TestSlngRefusesAnAsyncEntryPointOnly(t *testing.T) {
	agent := slngAgent(t)
	addSlngLocalTool(agent, "check_order", "async def check_order(order_id: str) -> dict:\n    return {}\n")
	wantSlngError(t, validateSlng(t, agent), "synchronously", "asyncio.run")

	agent = slngAgent(t)
	addSlngLocalTool(agent, "check_order", "import asyncio\n\n\nasync def _fetch(order_id: str) -> dict:\n    return {}\n\n\ndef check_order(order_id: str) -> dict:\n    return asyncio.run(_fetch(order_id))\n")
	if row := validateSlng(t, agent); len(row.Errors) > 0 {
		t.Errorf("a sync entry point running its own loop was refused: %#v", row.Errors)
	}
}

func TestSlngRefusesBadFallbackChains(t *testing.T) {
	for _, test := range []struct {
		name     string
		fallback []string
		want     string
	}{
		{"itself", []string{"reasoning"}, "lists itself in its own fallback chain"},
		{"duplicate", []string{"backup", "backup"}, `lists "backup" twice`},
	} {
		t.Run(test.name, func(t *testing.T) {
			agent := slngAgent(t)
			agent.Models["backup"] = ModelDef{Kind: KindThink, Provider: "openai", Model: "gpt-4o", Description: "backup"}
			def := agent.Models["reasoning"]
			def.Fallback = test.fallback
			agent.Models["reasoning"] = def
			// Build resolves one binding per used model; a model added here has to
			// bring its own, or the shared "missing reason binding" check fires first
			// and the fallback rule never gets asked.
			target := targetFor(agent, ProviderSlng)
			target.Models.Reason["backup"] = Binding{Provider: "openai", Model: "gpt-4o"}
			report, err := Validate(agent, []Target{target}, targetcap.Default())
			if err == nil {
				t.Fatalf("fallback %v passed", test.fallback)
			}
			wantSlngError(t, reportFor(report, ProviderSlng), test.want)
		})
	}
}

func TestSlngRefusesNonStringAndTemplatedVariableDefaults(t *testing.T) {
	agent := slngAgent(t)
	agent.Variables["verified"] = Variable{Type: PrimitiveBoolean, Default: false}
	wantSlngError(t, validateSlng(t, agent), `variable "verified" has a boolean default`, "string")

	agent = slngAgent(t)
	agent.Variables["greeting_name"] = Variable{Type: PrimitiveString, Default: "{{customer_name}}"}
	wantSlngError(t, validateSlng(t, agent), "default containing a template reference")
}

func TestSlngRefusesAToolSchemaOverThePolicyLimits(t *testing.T) {
	limits := targetcap.SlngSchemaLimits
	properties := map[string]any{}
	for i := 0; i <= limits.Properties; i++ {
		properties["field_"+strings.Repeat("x", i%8)+string(rune('a'+i%26))+string(rune('a'+i/26))] = map[string]any{"type": "string"}
	}
	if len(properties) <= limits.Properties {
		t.Fatalf("the fixture built %d properties, which is not over the %d limit", len(properties), limits.Properties)
	}
	agent := slngAgent(t)
	agent.Tools["wide"] = Tool{
		Description: "Takes far too many fields.",
		Execution:   ToolWebhook,
		URLEnv:      "WIDE_URL",
		Input:       map[string]any{"type": "object", "properties": properties},
	}
	entry := agent.Agents["support"]
	entry.Tools = append(entry.Tools, "wide")
	agent.Agents["support"] = entry
	wantSlngError(t, validateSlng(t, agent), "properties on one object")

	// Depth, on a schema that is narrow and very deep.
	agent = slngAgent(t)
	deep := map[string]any{"type": "string"}
	for i := 0; i < limits.Depth; i++ {
		deep = map[string]any{"type": "object", "properties": map[string]any{"next": deep}}
	}
	agent.Tools["deep"] = Tool{Description: "Nested far too far.", Execution: ToolWebhook, URLEnv: "DEEP_URL", Input: deep}
	entry = agent.Agents["support"]
	entry.Tools = append(entry.Tools, "deep")
	agent.Agents["support"] = entry
	wantSlngError(t, validateSlng(t, agent), "levels deep")
}

// TestSlngRunsTheChannelBranchThatNeverRan covers ir.validateChannels' outbound
// block. A guard at the top of that function `continue`s for LiveKit and Pipecat
// whenever the telephony plan is nil, and both providers always have a plan when
// it is not, so this block has never executed for a shipped target (research R3).
// slng is the first provider to reach it, which makes it new code.
func TestSlngRunsTheChannelBranchThatNeverRan(t *testing.T) {
	outbound := true
	agent := slngAgent(t)
	agent.Channels["phone"] = Channel{Kind: ChannelTelephony, Outbound: &outbound, RequiredControls: []string{"hangup"}}
	row := validateSlng(t, agent)
	wantSlngError(t, row, "creates no carrier state")

	// on_voicemail additionally applies FieldVoicemail and resolves the
	// voicemail_detection control. The field is what refuses; the control is core,
	// so the author reads one message about voicemail rather than two.
	agent = slngAgent(t)
	agent.Channels["phone"] = Channel{Kind: ChannelTelephony, Outbound: &outbound, OnVoicemail: VoicemailHangup}
	row = validateSlng(t, agent)
	wantSlngError(t, row, "does not emit voicemail handling yet", "voicemail_detection")

	// A required control SLNG has no shape for is refused by name rather than by
	// route, because unmute writes slng no carrier and no transport at all.
	agent = slngAgent(t)
	agent.Channels["phone"] = Channel{Kind: ChannelTelephony, Outbound: &outbound, RequiredControls: []string{"dtmf_send"}}
	wantSlngError(t, validateSlng(t, agent), "emits no dtmf_send control")
}

// SLNG owns its own model list, so a model string passes through as written and
// needs no catalogue row. A future catalogue change must not start rejecting one
// (spec FR-005).
func TestSlngNeedsNoCatalogueEntry(t *testing.T) {
	agent := slngAgent(t)
	def := agent.Models["reasoning"]
	def.Provider = "a-vendor-this-repository-has-never-heard-of"
	def.Model = "some-model-v9"
	agent.Models["reasoning"] = def
	target := targetFor(agent, ProviderSlng)
	if binding := target.Models.Reason["reasoning"]; binding.Provider != "" {
		binding.Provider = def.Provider
		binding.Model = def.Model
		target.Models.Reason["reasoning"] = binding
	}
	report, err := Validate(agent, []Target{target}, targetcap.Default())
	if err != nil {
		t.Fatalf("an unknown vendor was refused on slng, which owns its own model list: %#v", reportFor(report, ProviderSlng).Errors)
	}
}

// TestNoSlngFileOpensASocket is the gate under the claim the whole design rests
// on: unmute writes files and never talks to the SLNG agents API (spec FR-003).
// A design constraint with no check is a comment.
func TestNoSlngFileOpensASocket(t *testing.T) {
	roots := map[string][]string{
		filepath.Join("..", "generate"): nil,
		filepath.Join("..", "target"):   nil,
		filepath.Join("..", "ir"):       nil,
	}
	forbidden := []string{`"net/http"`, `"net"`, `"net/url"`, `http.Get`, `http.Post`, `http.Client`, `net.Dial`}
	for root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if !strings.Contains(name, "slng") || !strings.HasSuffix(name, ".go") {
				continue
			}
			// A test may name what the code may not: this file's own forbidden list
			// is the point of it, and scanning itself would fail on its own words.
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			// The router files are the model vendor's, not the target's, and the
			// vendor is a thing the generated agent calls at run time.
			if strings.Contains(name, "router") {
				continue
			}
			content, err := os.ReadFile(filepath.Join(root, name))
			if err != nil {
				t.Fatal(err)
			}
			for _, pattern := range forbidden {
				if strings.Contains(string(content), pattern) {
					t.Errorf("%s mentions %s; unmute opens no socket to the SLNG agents API", filepath.Join(root, name), pattern)
				}
			}
			roots[root] = append(roots[root], name)
		}
	}
	// Also fails when the scan finds nothing, which is what a renamed file looks
	// like: a green run proving only that the glob missed.
	total := 0
	for _, found := range roots {
		total += len(found)
	}
	if total == 0 {
		t.Error("this scan found no slng target source at all; the file naming convention changed")
	}
}

func addSlngLocalTool(agent *Agent, name, source string) {
	agent.Tools[name] = Tool{
		Description:   "Look something up.",
		Execution:     ToolLocal,
		Handler:       "tools/" + name + ".py",
		HandlerSource: source,
		Input:         map[string]any{"type": "object", "properties": map[string]any{"order_id": map[string]any{"type": "string"}}},
		// Build settles both of these; a tool assembled in a test has to say them
		// out loud or it fails the shared shape checks instead of the slng ones.
		Interruption: ToolProviderDefault,
		Effect:       ToolReturnsData,
	}
	entry := agent.Agents["support"]
	entry.Tools = append(entry.Tools, name)
	agent.Agents["support"] = entry
}

// A {{$NAME}} token is a SLNG Vault variable. Before this, it fell through to
// "which is not a declared variable", which is true and sends the author to
// declare a variable that must not exist: SLNG holds the value.
func TestVaultTokenPassesOnSlngAndIsNamedElsewhere(t *testing.T) {
	agent := slngAgent(t)
	agent.Conversation.Greeting.Text = "Hi {{customer_name}}, you have reached {{$ACME_BRAND}}."

	// On slng it passes, and it reaches the emitted body unchanged; the emitter
	// side of that is covered in internal/generate.
	if row := validateSlng(t, agent); len(row.Errors) > 0 {
		t.Errorf("a Vault token was refused on the one target that resolves it: %#v", row.Errors)
	}

	// On a code target it fails, and the message names the token for what it is.
	target := targetFor(agent, ProviderSlng)
	target.Provider, target.Name, target.Version = ProviderLiveKit, "livekit", "1.6.10"
	report, err := Validate(agent, []Target{target}, targetcap.Default())
	if err == nil {
		t.Fatal("a Vault token passed on livekit, which cannot resolve one")
	}
	joined := strings.Join(reportFor(report, ProviderLiveKit).Errors, "\n")
	for _, want := range []string{"SLNG Vault variable", "only a slng target resolves"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the message does not contain %q, so it points at the wrong concept:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "is not a declared variable") {
		t.Errorf("the old message survived, which is the whole defect:\n%s", joined)
	}
}

// A Vault name SLNG's store would reject is refused at Build, before any target
// is chosen, because the name is wrong whatever the target.
func TestInvalidVaultNameIsRefusedWithItsShape(t *testing.T) {
	for _, token := range []string{"{{$lowercase}}", "{{$9LEADING_DIGIT}}", "{{$has-a-dash}}"} {
		pkg := loadSlngCore(t)
		pkg.Agent.Conversation.Greeting.Text = "Hi, you have reached " + token
		_, err := Build(pkg)
		if err == nil {
			t.Errorf("%s was accepted as a Vault name", token)
			continue
		}
		for _, want := range []string{"SLNG Vault variable", "uppercase", "ACME_API_KEY"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: the message does not say %q: %v", token, want, err)
			}
		}
	}
	// And the valid shape still builds, or the check above proves nothing.
	pkg := loadSlngCore(t)
	pkg.Agent.Conversation.Greeting.Text = "Hi, you have reached {{$ACME_BRAND}}."
	if _, err := Build(pkg); err != nil {
		t.Errorf("a valid Vault name was refused: %v", err)
	}
}

// A webhook tool needs a base from somewhere, and which shape depends on the
// target. Both are written on a package that names both kinds of target.
func TestWebhookBaseURLRulesArePerTarget(t *testing.T) {
	agent := slngAgent(t)
	agent.Tools["refund"] = Tool{
		Description: "Start a refund.", Execution: ToolWebhook,
		URLEnv: "REFUND_URL", Interruption: ToolProviderDefault, Effect: ToolReturnsData,
	}
	entry := agent.Agents["support"]
	entry.Tools = append(entry.Tools, "refund")
	agent.Agents["support"] = entry
	// url_env alone: fine for a code target, refused on slng.
	wantSlngError(t, validateSlng(t, agent), "names only url_env", "add base_url")

	// base_url alone: fine on slng, refused on a code target, which reads the
	// base from the environment at run time.
	agent = slngAgent(t)
	agent.Tools["refund"] = Tool{
		Description: "Start a refund.", Execution: ToolWebhook,
		BaseURL: "https://api.acme.example", Interruption: ToolProviderDefault, Effect: ToolReturnsData,
		Input: map[string]any{"type": "object", "properties": map[string]any{"reason": map[string]any{"type": "string"}}},
	}
	entry = agent.Agents["support"]
	entry.Tools = append(entry.Tools, "refund")
	agent.Agents["support"] = entry
	if row := validateSlng(t, agent); len(row.Errors) > 0 {
		t.Errorf("a literal base_url was refused on slng: %#v", row.Errors)
	}
	target := targetFor(agent, ProviderSlng)
	target.Provider, target.Name, target.Version = ProviderPipecat, "pipecat", "1.7.0"
	report, err := Validate(agent, []Target{target}, targetcap.Default())
	if err == nil {
		t.Fatal("a webhook with no url_env passed on pipecat, which reads the base from the environment")
	}
	if joined := strings.Join(reportFor(report, ProviderPipecat).Errors, "\n"); !strings.Contains(joined, "needs url_env") {
		t.Errorf("the message does not name url_env:\n%s", joined)
	}
}

// The shape rules on a literal base URL, which exist because SLNG's URL
// validator would reject each of these with a 422 at push.
func TestWebhookBaseURLShapeIsChecked(t *testing.T) {
	for _, test := range []struct{ base, want string }{
		{"http://api.acme.example", "must be https"},
		{"https://{{region}}.acme.example", "carries a template token"},
		{"https://", "has no host"},
		{"https://user:pass@api.acme.example", "carries userinfo"},
		{"https://api.acme.example/#anchor", "carries a fragment"},
	} {
		agent := slngAgent(t)
		agent.Tools["refund"] = Tool{
			Description: "Start a refund.", Execution: ToolWebhook,
			BaseURL: test.base, Interruption: ToolProviderDefault, Effect: ToolReturnsData,
		}
		entry := agent.Agents["support"]
		entry.Tools = append(entry.Tools, "refund")
		agent.Agents["support"] = entry
		row := validateSlng(t, agent)
		if !strings.Contains(strings.Join(row.Errors, "\n"), test.want) {
			t.Errorf("base_url %q: no error contains %q; got %#v", test.base, test.want, row.Errors)
		}
	}
}

// An MCP source on slng becomes one reference per tool, and unmute cannot ask
// the server what it offers, so "expose everything" has nothing to expand into.
func TestSlngRequiresAnExplicitMCPToolList(t *testing.T) {
	agent := slngAgent(t)
	agent.Tools["internal_docs"] = Tool{Execution: ToolMCP, URLEnv: "DOCS_MCP_URL", Interruption: ToolProviderDefault, Effect: ToolReturnsData}
	entry := agent.Agents["support"]
	entry.Tools = append(entry.Tools, "internal_docs")
	agent.Agents["support"] = entry
	wantSlngError(t, validateSlng(t, agent), "exposes every tool on its MCP server", "mcp.tools")

	agent.Tools["internal_docs"] = Tool{
		Execution: ToolMCP, URLEnv: "DOCS_MCP_URL", MCPTools: []string{"search_docs"},
		Interruption: ToolProviderDefault, Effect: ToolReturnsData,
	}
	row := validateSlng(t, agent)
	if len(row.Errors) > 0 {
		t.Errorf("an MCP source with an explicit tool list was refused: %#v", row.Errors)
	}
	// It is a caveat, not a refusal, and the author hears it before the push.
	if !strings.Contains(strings.Join(row.Warnings, "\n"), "connect to the server") {
		t.Errorf("no warning says the push step must reach the server: %#v", row.Warnings)
	}
}

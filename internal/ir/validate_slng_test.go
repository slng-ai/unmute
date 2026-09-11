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
	// slng_core names no `slng:` tool, so there is nothing this row's own
	// checks left for `unmute deploy` to confirm.
	if row.Scope != "" {
		t.Errorf("a package with no hosted tool has a non-empty Scope: %q", row.Scope)
	}
}

// TestSlngScopeNamesDeferredHostedChecks is T034/FR-003-004: a clean slng row
// over a package that references a hosted tool must say its checks are local
// only, because an offline compile cannot confirm the tool exists, that its
// published contract matches, or that its vault entries are present.
func TestSlngScopeNamesDeferredHostedChecks(t *testing.T) {
	agent, _ := hostedFixture(t, "slng_hosted")
	row := validateSlng(t, agent)
	if len(row.Errors) > 0 {
		t.Fatalf("the hosted fixture's slng row must be clean: %v", row.Errors)
	}
	for _, want := range []string{"local checks only", "existence", "argument contract", "vault", "unmute deploy"} {
		if !strings.Contains(row.Scope, want) {
			t.Errorf("Scope = %q, want it to say %q", row.Scope, want)
		}
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

// GATE. The slng target refuses to author a tool, and each refusal says how to
// get one onto the platform instead.
//
// This replaces two tests that are gone with the code they held: one refused a
// network import in an uploaded handler, because custom code on SLNG has no
// internet access, and the other refused an `async def` entry point, because
// SLNG calls a handler synchronously. Both facts are still true about the
// platform. Neither is unmute's to check any more, because unmute uploads no
// handler: the platform CLI has no `tool create`, so a tool is born in the SLNG
// dashboard and a package references it.
func TestSlngRefusesAuthoredToolBodies(t *testing.T) {
	for _, tc := range []struct {
		name string
		tool Tool
		want []string
	}{
		{
			name: "a local handler",
			tool: Tool{
				Description: "Look one up.", Execution: ToolLocal,
				Handler: "tools/check_order.py", HandlerSource: "def check_order() -> dict:\n    return {}\n",
				Input:        map[string]any{"type": "object", "properties": map[string]any{}},
				Interruption: ToolProviderDefault, Effect: ToolReturnsData,
			},
			want: []string{
				"does not create tools",
				"SLNG owns a tool's code, version and gate pipeline",
				"create the tool in the SLNG dashboard",
				"reference it with `slng:`",
				// It has to name the way out that keeps the handler, or the
				// author reads it as "your handler is unusable".
				"compile to livekit or pipecat",
			},
		},
		{
			name: "a webhook block",
			tool: Tool{
				Description: "Start a refund.", Execution: ToolWebhook,
				URLEnv: "REFUND_URL", BaseURL: "https://api.acme.example",
				Input:        map[string]any{"type": "object", "properties": map[string]any{}},
				Interruption: ToolProviderDefault, Effect: ToolReturnsData,
			},
			want: []string{
				"does not create tools",
				"write the URL and its credential into a tool body",
				"create the tool in the SLNG dashboard",
				"reference it with `slng:`",
				"compile to livekit or pipecat",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := slngAgent(t)
			agent.Tools["authored"] = tc.tool
			entry := agent.Agents["support"]
			entry.Tools = append(entry.Tools, "authored")
			agent.Agents["support"] = entry
			wantSlngError(t, validateSlng(t, agent), tc.want...)
		})
	}
}

// And the other direction: both still work exactly as before on a code target,
// which is what makes the refusal above a trade rather than a loss.
func TestCodeTargetsKeepAuthoredToolBodies(t *testing.T) {
	for _, provider := range []Provider{ProviderLiveKit, ProviderPipecat} {
		agent := slngAgent(t)
		agent.Tools["check_order"] = Tool{
			Description: "Look one up.", Execution: ToolLocal,
			Handler: "tools/check_order.py", HandlerSource: "def check_order() -> dict:\n    return {}\n",
			Input:        map[string]any{"type": "object", "properties": map[string]any{}},
			Interruption: ToolProviderDefault, Effect: ToolReturnsData,
		}
		entry := agent.Agents["support"]
		entry.Tools = append(entry.Tools, "check_order")
		agent.Agents["support"] = entry

		resolved := targetFor(agent, ProviderSlng)
		resolved.Provider, resolved.Name, resolved.Version = provider, string(provider), "1.6.10"
		if provider == ProviderPipecat {
			resolved.Version = "1.9.0"
		}
		report, _ := Validate(agent, []Target{resolved}, targetcap.Default())
		for _, err := range reportFor(report, provider).Errors {
			if strings.Contains(err, "does not create tools") {
				t.Errorf("%s inherited the slng refusal: %s", provider, err)
			}
		}
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
	wantSlngError(t, row, "writes no trunk and dials nothing from a package")

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

// A webhook tool needs a base from somewhere, and a code target needs the
// environment-variable form specifically: it emits `os.environ[...]` and reads
// base_url not at all.
//
// This used to check both halves of a per-target question. The slng half is
// gone with the target's ability to author a webhook tool at all, and
// TestSlngRefusesAuthoredToolBodies above is what covers it now. base_url stays
// in the schema, and is still what a hosted platform would store, so the shape
// rules below still apply to it.
func TestWebhookNeedsURLEnvOnACodeTarget(t *testing.T) {
	agent := slngAgent(t)
	agent.Tools["refund"] = Tool{
		Description: "Start a refund.", Execution: ToolWebhook,
		BaseURL: "https://api.acme.example", Interruption: ToolProviderDefault, Effect: ToolReturnsData,
		Input: map[string]any{"type": "object", "properties": map[string]any{"reason": map[string]any{"type": "string"}}},
	}
	entry := agent.Agents["support"]
	entry.Tools = append(entry.Tools, "refund")
	agent.Agents["support"] = entry

	resolved := targetFor(agent, ProviderSlng)
	resolved.Provider, resolved.Name, resolved.Version = ProviderPipecat, "pipecat", "1.9.0"
	report, err := Validate(agent, []Target{resolved}, targetcap.Default())
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

// An MCP source on slng becomes one reference per tool, and unmute compiles
// offline, so "expose everything" has nothing to expand into.
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
	// And no caveat about reaching the server. voiceai 0.1.16 resolves the server
	// by name and copies each tool's schema hash from the platform's own stored
	// capability snapshot, so nothing connects to it. unmute warned otherwise for
	// a year, and the same warning told authors an mcp: package could not deploy
	// at all, which stopped people trying something that works.
	if joined := strings.Join(row.Warnings, "\n"); strings.Contains(joined, "connect to the server") ||
		strings.Contains(joined, "must reach the server") {
		t.Errorf("a warning still says the push has to reach the MCP server: %#v", row.Warnings)
	}
}

// TestSlngRefusesADeclaredShape is FR-010 on the one target that cannot express
// declared state, and the message has to say what to write instead.
//
// A shape is a generated Pydantic class with a validator, and the validation
// runs where the value enters the state, which is inside a module this target
// never writes. So the refusal is about the missing module, not about the
// missing tasks: an author who flattens the shape into primitives gets a
// package that deploys.
func TestSlngRefusesADeclaredShape(t *testing.T) {
	agent := slngAgent(t)
	agent.Shapes = map[string]Shape{"Appointment": {
		Name:   "Appointment",
		Fields: []Field{{Name: "scheduled_date", Type: &TypeRef{Shaped: ShapedDate}}},
	}}
	variable := agent.Variables["caller_phone"]
	variable.Shape = &TypeRef{List: &TypeRef{Shape: "Appointment"}}
	agent.Variables["caller_phone"] = variable

	row := validateSlng(t, agent)
	wantSlngError(t, row, "slng target pushes a spec and emits no module of its own",
		"has nowhere to be declared or checked", "compile to livekit or pipecat")
}

// TestSlngRefusesAShapedTextType is the second row, refused for the same reason
// and fixed differently: this one asks the author to give up a check rather
// than to flatten a group of fields, so it says so separately.
func TestSlngRefusesAShapedTextType(t *testing.T) {
	agent := slngAgent(t)
	variable := agent.Variables["caller_phone"]
	variable.Shape = &TypeRef{Shaped: ShapedPhone}
	agent.Variables["caller_phone"] = variable

	row := validateSlng(t, agent)
	wantSlngError(t, row, "a value whose text has a validated shape",
		"declare the value as one of the primitive types")
}

// TestSlngRefusesAnEmailType is the same pair of rows reached by the two email
// types, and it is separate because each arrives by its own route.
//
// EmailStr is a text type with a validated shape, so it rides the shaped row.
// NameEmail is a shape the compiler supplies, and the only reason it rides the
// declared-shape row is that internal/ir seeds it into the catalog when a type
// names it: a reference with no entry there would leave the catalog empty, and
// the row that starts from whether the catalog is empty would pass a package
// this target cannot emit.
func TestSlngRefusesAnEmailType(t *testing.T) {
	for _, tc := range []struct {
		name    string
		shapes  map[string]Shape
		ref     *TypeRef
		phrases []string
	}{
		{
			name:    "the plain address",
			ref:     &TypeRef{Shaped: ShapedEmail},
			phrases: []string{"a value whose text has a validated shape", "declare the value as one of the primitive types"},
		},
		{
			name:    "the name and address pair",
			shapes:  map[string]Shape{"NameEmail": builtinShapes["NameEmail"]},
			ref:     &TypeRef{Shape: "NameEmail"},
			phrases: []string{"a value with a declared shape", "compile to livekit or pipecat"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := slngAgent(t)
			if tc.shapes != nil {
				agent.Shapes = tc.shapes
			}
			variable := agent.Variables["caller_phone"]
			variable.Shape = tc.ref
			agent.Variables["caller_phone"] = variable

			row := validateSlng(t, agent)
			wantSlngError(t, row, tc.phrases...)
		})
	}
}

// TestSlngAcceptsAPackageDeclaringNothingStructured is the control. Both rows
// above must fire only on a package that declares one, or every slng package in
// the tree stops validating.
func TestSlngAcceptsAPackageDeclaringNothingStructured(t *testing.T) {
	row := validateSlng(t, slngAgent(t))
	for _, message := range row.Errors {
		if strings.Contains(message, "emits no module of its own") {
			t.Errorf("a package declaring nothing structured was refused: %q", message)
		}
	}
}

// TestSlngHoldsAnOverrideToItsOwnExpressionContract.
//
// An `argument_overrides` entry is a fixed scalar or one whole variable token.
// That is the platform's rule, not ours: the value is stored on the attachment
// and substituted when a call starts, so there is nothing for a template to be
// concatenated into. A code target assembles the request itself and can
// interpolate, which is why this is a per-target check and why the second half
// of this test exists.
func TestSlngHoldsAnOverrideToItsOwnExpressionContract(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		want  []string
	}{
		{
			// The shape that would otherwise be stored and send the braces to
			// the tool as characters.
			name:  "embedded interpolation",
			value: "Order {{order_number}}",
			want:  []string{"as text with {{order_number}} inside it", "would reach the tool as characters", "on its own to send the value"},
		},
		{
			name:  "two references in one value",
			value: "{{first_name}} {{last_name}}",
			want:  []string{"2 template references in one value", "name one variable on its own"},
		},
		{
			name:  "a list",
			value: []any{"a", "b"},
			want:  []string{"one fixed scalar or one whole variable token", "record the value you need into its own variable"},
		},
		{
			name:  "a mapping",
			value: map[string]any{"a": "b"},
			want:  []string{"one fixed scalar or one whole variable token"},
		},
		{
			name:  "no value at all",
			value: nil,
			want:  []string{"with no value", "let the model supply the argument"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := slngAgent(t)
			tool := agent.Tools["check_order"]
			tool.Inject = map[string]any{"order_number": tc.value}
			agent.Tools["check_order"] = tool
			wantSlngError(t, validateSlng(t, agent), tc.want...)
		})
	}
}

// TestSlngAcceptsTheValuesAnOverrideIsMadeOf, and the two that matter most are
// `false` and `0`: both read as empty to a careless predicate, and refusing
// either would refuse a legitimate argument.
func TestSlngAcceptsTheValuesAnOverrideIsMadeOf(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"a fixed string", "MY_RANDOM_VALUE"},
		{"a fixed false", false},
		{"a fixed zero", 0},
		{"a fixed number", 42},
		{"one whole variable token", "{{customer_name}}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := slngAgent(t)
			// A variable the token can name, declared the ordinary way.
			tool := agent.Tools["check_order"]
			tool.Inject = map[string]any{"order_number": tc.value}
			agent.Tools["check_order"] = tool
			row := validateSlng(t, agent)
			for _, err := range row.Errors {
				if strings.Contains(err, "order_number") {
					t.Errorf("a legal override was refused: %s", err)
				}
			}
		})
	}
}

// TestCodeTargetsKeepTheirOwnExpressionRules is the other half: the SLNG
// restrictions above must not reach livekit or pipecat, which build the request
// themselves and can interpolate a value into a string.
//
// Without this, adding the platform's rule would have quietly narrowed what a
// package targeting a code target could write, which is the opposite of what
// this feature is for.
func TestCodeTargetsKeepTheirOwnExpressionRules(t *testing.T) {
	pkg, err := packagespec.Load(filepath.Join("..", "testdata", "slng_hosted_code"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	tool := agent.Tools["check_order"]
	// Embedded interpolation, which SLNG refuses above.
	tool.Inject = map[string]any{"order_number": "Order {{customer_name}}"}
	agent.Tools["check_order"] = tool

	for _, name := range []string{"livekit", "pipecat"} {
		resolved, ok := agent.Targets[name]
		if !ok {
			t.Fatalf("the fixture declares no %s target", name)
		}
		report, _ := Validate(agent, []Target{resolved}, targetcap.Default())
		for _, err := range report.PerTarget[0].Errors {
			if strings.Contains(err, "inside it") || strings.Contains(err, "template references in one value") {
				t.Errorf("%s inherited SLNG's override expression rule: %s", name, err)
			}
		}
	}
}

// The three keys spec 010 adds are code-target keys, for the same reason every
// other task key is: the slng target writes one agent with one prompt, so there
// is no step to end, no step to skip, and no step opening to choose.
//
// Each is its own row because each is fixed differently, and a row that said
// "tasks are refused" would be the refusal the author already had.
func TestSlngRefusesFinish(t *testing.T) {
	agent := slngAgent(t)
	if agent.Tasks == nil {
		agent.Tasks = map[string]Task{}
	}
	agent.Tasks["take_booking"] = Task{
		Instructions: "Book the caller in.",
		Finish:       []TerminalTool{{Tool: "book_it", Success: map[string][]string{"status": {"booked"}}}},
	}

	row := validateSlng(t, agent)
	wantSlngError(t, row, "a step that ends on its tool", "compile to livekit or pipecat")
}

func TestSlngRefusesAGroupSkip(t *testing.T) {
	agent := slngAgent(t)
	if agent.TaskGroups == nil {
		agent.TaskGroups = map[string]TaskGroup{}
	}
	agent.TaskGroups["book"] = TaskGroup{
		Steps:        []GroupStep{{Task: "verify", SkipWhenConfirmed: "caller_phone"}},
		ContextScope: ContextShared, Then: GroupReturn, Merge: GroupMergeResults,
	}

	row := validateSlng(t, agent)
	wantSlngError(t, row, "a skippable group step", "compile to livekit or pipecat")
}

func TestSlngRefusesAListeningOpening(t *testing.T) {
	agent := slngAgent(t)
	if agent.Tasks == nil {
		agent.Tasks = map[string]Task{}
	}
	agent.Tasks["take_note"] = Task{Instructions: "Take a note.", Opening: OpeningListen, Announce: "What shall I pass on?"}

	row := validateSlng(t, agent)
	wantSlngError(t, row, "a step opening", "compile to livekit or pipecat")
}

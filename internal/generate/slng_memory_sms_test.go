package generate

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// The slng_memory_sms fixture is the smallest package that records a value the
// model fills mid-call and texts it back. What the driver has to get right is
// where each authored thing lands: a `source: conversation` variable is a
// runtime variable and not a template one, and a builtin send_sms's `inject:`
// is the attachment's config override and not an argument.

func TestSlngEmitsAConversationVariableAsARuntimeVariable(t *testing.T) {
	_, files := compileSlng(t, "slng_memory_sms")
	body := slngBodyOf(t, files)

	runtime, _ := body["runtime_variables"].([]any)
	if len(runtime) != 1 {
		t.Fatalf("runtime_variables = %v, want the one conversation variable", body["runtime_variables"])
	}
	entry := runtime[0].(map[string]any)
	if entry["name"] != "caller_phone" || !strings.Contains(entry["description"].(string), "confirmed") {
		t.Errorf("runtime variable = %v, want caller_phone with its authored description", entry)
	}
	for _, field := range []string{"template_defaults", "template_variable_options"} {
		keys := sortedKeysOf(body[field].(map[string]any))
		if !slices.Equal(keys, []string{"customer_name"}) {
			t.Errorf("%s has %v, want only the session variable: SLNG refuses a name that is both a runtime and a template variable", field, keys)
		}
	}
}

func TestSlngWritesTheSendSmsSenderAsAConfigOverride(t *testing.T) {
	_, files := compileSlng(t, "slng_memory_sms")
	body := slngBodyOf(t, files)
	var sms map[string]any
	for _, raw := range body["tool_refs"].([]any) {
		if ref := raw.(map[string]any); ref["tool"] == "send_sms" {
			sms = ref
		}
	}
	if sms == nil {
		t.Fatal("no send_sms reference in the body")
	}
	config, _ := sms["config_overrides"].(map[string]any)
	if config["type"] != "send_sms" || config["from_number"] != "+447700900123" {
		t.Errorf("config_overrides = %v, want the send_sms union tag and the pinned sender", sms["config_overrides"])
	}
	if arguments, _ := sms["argument_overrides"].(map[string]any); len(arguments) != 0 {
		t.Errorf("argument_overrides = %v, want empty: the model supplies recipient and body", arguments)
	}
}

// GATE. A curated capability's own setting reaches the attachment's config, and
// the union tag goes with it.
//
// Live organisation, 2026-09-17: `current_datetime` ships
// `{"type": "current_datetime", "timezone": "UTC"}`, the record belongs to no
// organisation (`organisation_id: null`), and a package could pin nothing on it.
// A dental agent in San Francisco was told the date was tomorrow from four in
// the afternoon. The tag is what makes the body a union member: a create infers
// it from the tool's type and an update PATCH cannot, so an untagged body
// deploys once and 422s on every push after it.
func TestSlngWritesACuratedCapabilitySettingAsAConfigOverride(t *testing.T) {
	_, files := compileSlng(t, "slng_memory_sms")
	body := slngBodyOf(t, files)
	var clock map[string]any
	for _, raw := range body["tool_refs"].([]any) {
		if ref := raw.(map[string]any); ref["tool"] == "current_datetime" {
			clock = ref
		}
	}
	if clock == nil {
		t.Fatal("no current_datetime reference in the body")
	}
	config, _ := clock["config_overrides"].(map[string]any)
	if config["type"] != "current_datetime" || config["timezone"] != "America/Los_Angeles" {
		t.Errorf("config_overrides = %v, want the union tag and the pinned zone", clock["config_overrides"])
	}
	if arguments, _ := clock["argument_overrides"].(map[string]any); len(arguments) != 0 {
		t.Errorf("argument_overrides = %v, want empty: a curated capability publishes no argument schema", arguments)
	}
}

// GATE. Every kind of tool can speak before it runs, a builtin included.
//
// On slng a curated capability is an attachment like any other and carries the
// same execution_policy.pre_action_message, so refusing `announce:` on a
// `builtin:` file was a gap in this compiler stated as a limit of the platform.
// A code target still refuses it, because it builds the prebuilt from its own
// SDK and has no seam in front of it; that refusal is beside this one in
// internal/ir/validate.go.
func TestSlngAnnouncesEveryKindOfToolIncludingABuiltin(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "slng_memory_sms"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"current_datetime", "end_call", "send_sms"} {
		tool := pkg.Tools[name]
		tool.Announce = []string{"One moment."}
		pkg.Tools[name] = tool
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	tgt := targetByProvider(t, agent, ir.ProviderSlng)
	if report, _ := ir.Validate(agent, []ir.Target{tgt}, targetcap.Default()); len(report.PerTarget[0].Errors) > 0 {
		t.Fatalf("announce on a builtin was refused on slng: %v", report.PerTarget[0].Errors)
	}
	for name, line := range SlngAuthoredAnnouncements(agent) {
		if line == "" {
			t.Errorf("tool %q carries no announcement for the deploy preview to compare", name)
		}
	}
	for _, name := range []string{"current_datetime", "end_call", "send_sms"} {
		if SlngAuthoredAnnouncements(agent)[name] != "One moment." {
			t.Errorf("tool %q lost its announcement: %q", name, SlngAuthoredAnnouncements(agent)[name])
		}
	}

	// A tool that ends the conversation waits for its own sentence, and nothing
	// else does. end_call hangs up the moment it runs, so a goodbye spoken
	// alongside it is cut off mid-word; every other announcement covers a wait,
	// and waiting for one would add the silence it exists to fill.
	_, files := compileSlng(t, "slng_memory_sms")
	body := slngBodyOf(t, files)
	for _, raw := range body["tool_refs"].([]any) {
		ref := raw.(map[string]any)
		policy, _ := ref["execution_policy"].(map[string]any)
		if policy == nil {
			continue
		}
		pre := policy["pre_action_message"].(map[string]any)
		want := ref["tool"] == "end_call"
		if pre["wait"] != want {
			t.Errorf("tool %v pre_action_message.wait = %v, want %v", ref["tool"], pre["wait"], want)
		}
	}
}

func TestSlngSendSmsNeedsTheTwilioVaultEntries(t *testing.T) {
	artifact, files := compileSlng(t, "slng_memory_sms")
	got := names(artifact.Requires.Secrets)
	for _, want := range targetcap.SendSmsVaultSecrets {
		if !slices.Contains(got, want) {
			t.Errorf("requirements name %v, want %s: SLNG reads it for send_sms and the package declares it nowhere", got, want)
		}
		if !strings.Contains(files["README.md"], "`"+want+"`") {
			t.Errorf("the runbook does not name %s in its vault table", want)
		}
	}
}

func TestSlngRunbookNamesWhatTheModelRecords(t *testing.T) {
	_, files := compileSlng(t, "slng_memory_sms")
	readme := files["README.md"]
	for _, want := range []string{"records during the call: `caller_phone`", "set_runtime_variables", "memory_variables"} {
		if !strings.Contains(readme, want) {
			t.Errorf("runbook does not say %q", want)
		}
	}
	if strings.Contains(readme, "This agent declares: `caller_phone`") {
		t.Error("runbook lists the conversation variable as a session argument, which no session can supply")
	}
}

func TestSlngBuiltinInjectIsConfigNotAnArgumentForTheDeploy(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "slng_memory_sms"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := SlngInjectedArguments(agent)["send_sms"]; present {
		t.Error("the deploy would check the sender against send_sms's argument schema, which has none")
	}
	config := SlngAuthoredConfig(agent)["send_sms"]
	if config["from_number"] != "+447700900123" || config["type"] != "send_sms" {
		t.Errorf("authored config = %v, want the sender the preview compares", config)
	}
}

func sortedKeysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

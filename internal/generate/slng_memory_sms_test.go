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

package tui

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/huh"
	"github.com/slng/unmute/internal/scaffold"
	targetcap "github.com/slng/unmute/internal/target"
)

func TestRunCreateDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	var output bytes.Buffer
	// 1=create, name=agent, 16=Create agent, ""=confirm default (yes).
	got, err := Run(strings.NewReader("1\nagent\n16\n\n"), &output, true)
	if err != nil {
		t.Fatal(err)
	}
	data := scaffold.Data{Name: "agent", Language: scaffold.DefaultLanguage, Channel: scaffold.DefaultChannel, Greeting: scaffold.DefaultGreeting, Instructions: scaffold.DefaultInstructions}
	data.SetTarget(scaffold.DefaultTarget)
	agent := Agent{Path: "agent", Data: data}
	want := Result{Agent: agent, Create: true, Confirmed: true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Run() = %#v, want %#v", got, want)
	}
	if _, err := os.Stat("agent"); !os.IsNotExist(err) {
		t.Fatalf("TUI wrote agent directory: %v", err)
	}
	for _, label := range []string{"Target", "Language", "Models", "Instructions", "Greeting", "Variables", "Tools", "Agents", "Handoffs", "Tasks", "Task groups", "Caller channels", "Human transfers", "Customize", "Compile after create", "Create agent", "← Back"} {
		if !strings.Contains(output.String(), label) {
			t.Errorf("menu missing %q:\n%s", label, output.String())
		}
	}
	if !strings.Contains(output.String(), "Agent name") || !strings.Contains(output.String(), agentNameHelp) {
		t.Fatalf("name field missing guidance:\n%s", output.String())
	}
	for _, label := range []string{"Required env:", "Forwarded bindings:"} {
		if !strings.Contains(output.String(), label) {
			t.Errorf("review missing %q:\n%s", label, output.String())
		}
	}
}

func TestRunQuit(t *testing.T) {
	got, err := Run(strings.NewReader("2\n"), &bytes.Buffer{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Confirmed {
		t.Fatalf("quit returned a confirmed result: %#v", got)
	}
}

func TestRunCompileToggle(t *testing.T) {
	t.Chdir(t.TempDir())
	// 1=create, name, 15=toggle compile on, 16=Create agent, confirm.
	got, err := Run(strings.NewReader("1\nagent\n15\n16\n\n"), &bytes.Buffer{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Confirmed || !got.Compile {
		t.Fatalf("compile toggle result = %#v", got)
	}
}

func TestRunSelectTarget(t *testing.T) {
	t.Chdir(t.TempDir())
	var output bytes.Buffer
	// Create, name, Target, LiveKit, Create agent, confirm.
	got, err := Run(strings.NewReader("1\nagent\n1\n2\n16\n\n"), &output, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent.Data.Target != "livekit" {
		t.Fatalf("target = %q", got.Agent.Data.Target)
	}
	if !strings.Contains(output.String(), "Vapi and Deepgram are unavailable") {
		t.Fatalf("missing unavailable-driver explanation:\n%s", output.String())
	}
}

func TestRunEditModels(t *testing.T) {
	t.Chdir(t.TempDir())
	// Create, name, Models, Speak, cartesia, model, voice, params, Back, Create, confirm.
	got, err := Run(strings.NewReader("1\nagent\n3\n3\n1\n1\nsonic-3\nvoice-id\n{\"speed\":1}\n5\n16\n\n"), &bytes.Buffer{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent.Data.Speak != (scaffold.Binding{Provider: "cartesia", Model: "sonic-3", Voice: "voice-id", Params: `{"speed":1}`}) {
		t.Fatalf("speak binding = %#v", got.Agent.Data.Speak)
	}
}

func TestRunEditLanguage(t *testing.T) {
	t.Chdir(t.TempDir())
	got, err := Run(strings.NewReader("1\nagent\n2\nes-MX\n16\n\n"), &bytes.Buffer{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent.Data.Language != "es-MX" {
		t.Fatalf("language = %q", got.Agent.Data.Language)
	}
}

func TestRunAddVariableAndTool(t *testing.T) {
	t.Chdir(t.TempDir())
	input := "1\nagent\n" +
		"6\n1\ncustomer_id\n1\n\"guest\"\n2\n3\n" +
		"7\n1\nlookup_customer\nLook up the caller\n1\nLOOKUP_URL\n{\"type\":\"object\"}\n\n2\n3\n" +
		"16\n\n"
	got, err := Run(strings.NewReader(input), &bytes.Buffer{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Agent.Data.Variables) != 1 || got.Agent.Data.Variables[0].Source != "call_start" {
		t.Fatalf("variables = %#v", got.Agent.Data.Variables)
	}
	if len(got.Agent.Data.Tools) != 1 || got.Agent.Data.Tools[0].URLEnv != "LOOKUP_URL" {
		t.Fatalf("tools = %#v", got.Agent.Data.Tools)
	}
}

func TestV18VariablesMenuShowsSavedItems(t *testing.T) {
	data := scaffold.Data{Variables: []scaffold.Variable{{Name: "customer_id", Type: "string", Source: "call_start"}}}
	var output bytes.Buffer
	err := editVariables(newRunner(strings.NewReader(""), &output, true), &data)
	if !errors.Is(err, huh.ErrUserAborted) {
		t.Fatalf("editVariables() error = %v, want ErrUserAborted", err)
	}
	if !strings.Contains(output.String(), "customer_id") {
		t.Fatalf("saved variable is not visible:\n%s", output.String())
	}
}

func TestV18ToolsMenuUsesNeutralNameAndShowsExecution(t *testing.T) {
	data := scaffold.Data{Target: "pipecat"}
	var output bytes.Buffer
	err := editTools(newRunner(strings.NewReader("1\nlookup_customer\nLook up the caller.\n"), &output, true), &data)
	if !errors.Is(err, huh.ErrUserAborted) {
		t.Fatalf("editTools() error = %v, want ErrUserAborted", err)
	}
	for _, want := range []string{"Add tool", "Webhook", "Local Python"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("tools flow missing %q:\n%s", want, output.String())
		}
	}
}

func TestV18AgentMenuShowsAndEditsSavedAgent(t *testing.T) {
	data := scaffold.Data{Instructions: scaffold.DefaultInstructions}
	data.SetTarget("pipecat")
	data.Agents = []scaffold.Agent{{
		Name: "billing", Instructions: "Handle billing.", Reason: data.Reason, Speak: data.Speak,
	}}
	data.Tools = []scaffold.Tool{{Name: "lookup_customer", AttachTo: []string{"billing"}}}
	var output bytes.Buffer
	err := editAgents(newRunner(strings.NewReader("2\n1\nUpdated billing prompt.\n7\n5\n"), &output, true), &data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "billing") {
		t.Fatalf("saved agent is not visible:\n%s", output.String())
	}
	if !strings.Contains(output.String(), "lookup_customer") {
		t.Fatalf("saved agent tools are not visible:\n%s", output.String())
	}
	if got := data.Agents[0].Instructions; got != "Updated billing prompt." {
		t.Fatalf("billing prompt = %q", got)
	}
}

func TestV19HandoffShowsExistingVariablesAsChoices(t *testing.T) {
	data := scaffold.Data{Instructions: scaffold.DefaultInstructions, Variables: []scaffold.Variable{{Name: "customer_id", Type: "string"}}}
	data.SetTarget("pipecat")
	data.Agents = []scaffold.Agent{{Name: "billing", Instructions: "Handle billing.", Reason: data.Reason, Speak: data.Speak}}
	var output bytes.Buffer
	err := editHandoffs(newRunner(strings.NewReader("1\n1\n1\nto_billing\nCaller asks about billing.\n"), &output, true), &data)
	if !errors.Is(err, huh.ErrUserAborted) {
		t.Fatalf("editHandoffs() error = %v, want ErrUserAborted", err)
	}
	for _, want := range []string{"assistant", "billing", "customer_id"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("handoff choices missing %q:\n%s", want, output.String())
		}
	}
}

func TestV19TaskShowsExistingToolsAsChoices(t *testing.T) {
	data := scaffold.Data{Tools: []scaffold.Tool{{Name: "lookup_customer"}}}
	data.SetTarget("pipecat")
	var output bytes.Buffer
	err := editTasks(newRunner(strings.NewReader("1\ncollect\nCollect customer data.\n"), &output, true), &data)
	if !errors.Is(err, huh.ErrUserAborted) {
		t.Fatalf("editTasks() error = %v, want ErrUserAborted", err)
	}
	if !strings.Contains(output.String(), "lookup_customer") {
		t.Fatalf("task tool choices are not visible:\n%s", output.String())
	}
}

func TestV22TaskResultExplainsPrefilledShape(t *testing.T) {
	data := scaffold.Data{}
	data.SetTarget("pipecat")
	var output bytes.Buffer
	err := editTasks(newRunner(strings.NewReader("1\ncollect\nCollect customer data.\n1\n"), &output, true), &data)
	if !errors.Is(err, huh.ErrUserAborted) {
		t.Fatalf("editTasks() error = %v, want ErrUserAborted", err)
	}
	for _, want := range []string{`{"result":"string"}`, "Each key becomes one returned field"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("task result help missing %q:\n%s", want, output.String())
		}
	}
}

func TestV19TaskAssignmentPicksSavedVariableAndResultField(t *testing.T) {
	data := scaffold.Data{Variables: []scaffold.Variable{{Name: "verified", Type: "boolean"}, {Name: "tier", Type: "string"}}}
	task := scaffold.Task{Result: `{"verified":"boolean","tier":{"enum":["free","pro"]}}`}
	var output bytes.Buffer
	back, err := editTaskAssignments(newRunner(strings.NewReader("1\n3\n2\n"), &output, true), &data, &task)
	if err != nil {
		t.Fatal(err)
	}
	if back {
		t.Fatal("editTaskAssignments() unexpectedly went back")
	}
	if task.Assign != `{"verified":"result.verified"}` {
		t.Fatalf("task assignment = %s", task.Assign)
	}
	for _, want := range []string{"verified", "tier"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("task assignment choices missing %q:\n%s", want, output.String())
		}
	}
}

func TestV21PreflightFailureUsesDedicatedScreen(t *testing.T) {
	data := scaffold.Data{Name: "agent", Language: "en", Channel: "web"}
	data.SetTarget("livekit")
	data.Fallbacks = []scaffold.ModelFallback{{Name: "backup", Profile: "assistant_model", Binding: data.Reason}}
	var output bytes.Buffer
	_, back, err := editAgent(newRunner(strings.NewReader("16\n10\n17\n"), &output, true), Agent{Path: "agent", Data: data})
	if err != nil {
		t.Fatal(err)
	}
	if !back {
		t.Fatal("editAgent() did not return through Back")
	}
	for _, want := range []string{"Cannot create agent", "Fix the configuration, then go Back", "does not emit model fallback", "Review / delete model fallbacks"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("preflight screen missing %q:\n%s", want, output.String())
		}
	}
}

func TestRunHandoffsRequireTwoAgents(t *testing.T) {
	t.Chdir(t.TempDir())
	var output bytes.Buffer
	got, err := Run(strings.NewReader("1\nagent\n9\n1\n16\n\n"), &output, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Agent.Data.Handoffs) != 0 || !strings.Contains(output.String(), "Create a second agent first") {
		t.Fatalf("handoff gate missing: result=%#v\n%s", got, output.String())
	}
}

func TestRunAddAgentAndHandoff(t *testing.T) {
	t.Chdir(t.TempDir())
	input := "1\nagent\n" +
		"8\n2\nbilling\nYou handle billing questions.\n" +
		"1\ngpt-4.1-mini\n\n" +
		"2\nslng/deepgram/aura:2-en\naura-2-thalia-en\n\n" +
		"5\n" +
		"9\n1\n1\n1\nto_billing\nCaller asks about billing.\n1\n1\n3\n" +
		"16\n\n"
	got, err := Run(strings.NewReader(input), &bytes.Buffer{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Agent.Data.Agents) != 1 || got.Agent.Data.Agents[0].Name != "billing" {
		t.Fatalf("agents = %#v", got.Agent.Data.Agents)
	}
	if len(got.Agent.Data.Handoffs) != 1 || got.Agent.Data.Handoffs[0].Source != "assistant" || got.Agent.Data.Handoffs[0].To != "billing" {
		t.Fatalf("handoffs = %#v", got.Agent.Data.Handoffs)
	}
}

func TestRunTaskGroupsRequireTasks(t *testing.T) {
	t.Chdir(t.TempDir())
	var output bytes.Buffer
	if _, err := Run(strings.NewReader("1\nagent\n11\n1\n16\n\n"), &output, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Create at least one task first") {
		t.Fatalf("task-group gate missing:\n%s", output.String())
	}
}

func TestRunAddTaskAndOrderedGroup(t *testing.T) {
	t.Chdir(t.TempDir())
	input := "1\nagent\n" +
		"10\n1\ncollect\nCollect the caller tier.\n1\n{\"tier\":{\"enum\":[\"free\",\"pro\"]}}\n1\n1\n1\nClassify the caller.\n3\n" +
		"11\n1\ntriage\n1\n2\n1\n1\n1\nRun triage.\n3\n" +
		"16\n\n"
	got, err := Run(strings.NewReader(input), &bytes.Buffer{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Agent.Data.Tasks) != 1 || got.Agent.Data.Tasks[0].Name != "collect" {
		t.Fatalf("tasks = %#v", got.Agent.Data.Tasks)
	}
	if len(got.Agent.Data.TaskGroups) != 1 || !reflect.DeepEqual(got.Agent.Data.TaskGroups[0].Steps, []string{"collect"}) {
		t.Fatalf("task groups = %#v", got.Agent.Data.TaskGroups)
	}
}

func TestRunHumanTransfersRequireTelephony(t *testing.T) {
	t.Chdir(t.TempDir())
	var output bytes.Buffer
	if _, err := Run(strings.NewReader("1\nagent\n13\n1\n16\n\n"), &output, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Add a telephony caller channel first") {
		t.Fatalf("human-transfer gate missing:\n%s", output.String())
	}
}

func TestRunAddTelephonyAndHumanTransfer(t *testing.T) {
	t.Chdir(t.TempDir())
	input := "1\nagent\n" +
		"12\n2\n1\n6\n9\ndaily-sip\n\n" +
		"13\n1\nto_human\n1\nCaller requests a person.\nsupport_line\n+14155550123\n1\n3\n" +
		"16\n\n"
	got, err := Run(strings.NewReader(input), &bytes.Buffer{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !hasTelephony(&got.Agent.Data) || len(got.Agent.Data.HumanTransfers) != 1 {
		t.Fatalf("telephony result = %#v", got.Agent.Data)
	}
	if got.Agent.Data.HumanTransfers[0].Destination != "support_line" {
		t.Fatalf("human transfers = %#v", got.Agent.Data.HumanTransfers)
	}
}

func TestRunCustomizeCapacity(t *testing.T) {
	t.Chdir(t.TempDir())
	input := "1\nagent\n14\n3\n5\n10\n4m\n5\n16\n\n"
	got, err := Run(strings.NewReader(input), &bytes.Buffer{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent.Data.Capacity != (scaffold.Capacity{PeakSessions: 5, MaxSessions: 10, AvgSessionDuration: "4m"}) {
		t.Fatalf("capacity = %#v", got.Agent.Data.Capacity)
	}
}

func TestRunBackPreservesPriorEdits(t *testing.T) {
	t.Chdir(t.TempDir())
	input := "1\nagent\n2\nes-MX\n4\n:back\n16\n\n"
	got, err := Run(strings.NewReader(input), &bytes.Buffer{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent.Data.Language != "es-MX" || got.Agent.Data.Instructions != scaffold.DefaultInstructions {
		t.Fatalf("back lost edits: %#v", got.Agent.Data)
	}
}

func TestProviderOptionsMirrorCatalog(t *testing.T) {
	for _, tc := range []struct {
		framework targetcap.Provider
		role      targetcap.Role
	}{
		{targetcap.Pipecat, targetcap.Listen},
		{targetcap.Pipecat, targetcap.Reason},
		{targetcap.Pipecat, targetcap.Speak},
		{targetcap.LiveKit, targetcap.Listen},
		{targetcap.LiveKit, targetcap.Reason},
		{targetcap.LiveKit, targetcap.Speak},
		{targetcap.ElevenLabs, targetcap.Speak},
	} {
		options := providerOptions(tc.framework, tc.role)
		brands := targetcap.DefaultCatalog().Brands(tc.framework, tc.role)
		if len(options) != len(brands) {
			t.Fatalf("%s/%s options = %d, brands = %d", tc.framework, tc.role, len(options), len(brands))
		}
		for i := range brands {
			if options[i].Value != brands[i] {
				t.Errorf("%s/%s option %d = %q, want %q", tc.framework, tc.role, i, options[i].Value, brands[i])
			}
		}
	}
}

func TestV24ModelsLabelDeduplicatesProviderBrands(t *testing.T) {
	data := scaffold.Data{}
	data.SetTarget("livekit")
	if got := modelsLabel(data); got != "deepgram / openai" {
		t.Fatalf("models label = %q", got)
	}
}

func TestV24ProviderThenDistributorFlow(t *testing.T) {
	binding := scaffold.Binding{}
	input := "1\n2\nslng/cartesia/sonic-3\nvoice-id\n\n"
	if err := editBindingFor(newRunner(strings.NewReader(input), &bytes.Buffer{}, true), "pipecat", targetcap.Speak, &binding); err != nil {
		t.Fatal(err)
	}
	want := scaffold.Binding{Provider: "slng", Model: "slng/cartesia/sonic-3", Voice: "voice-id"}
	if binding != want {
		t.Fatalf("binding = %#v, want %#v", binding, want)
	}
}

func TestV25SavedResourcesOfferDelete(t *testing.T) {
	base := func() scaffold.Data {
		data := scaffold.Data{Name: "agent", Instructions: scaffold.DefaultInstructions}
		data.SetTarget("pipecat")
		data.Variables = []scaffold.Variable{{Name: "customer_id", Type: "string"}}
		data.Tools = []scaffold.Tool{{Name: "lookup_customer", Description: "Lookup", Input: `{}`}}
		data.Agents = []scaffold.Agent{{Name: "billing", Instructions: "Billing", Reason: data.Reason, Speak: data.Speak}}
		data.Handoffs = []scaffold.Handoff{{Name: "to_billing", Source: "assistant", To: "billing", When: "Billing", History: "full", AllVariables: true}}
		data.Tasks = []scaffold.Task{{Name: "collect", Instructions: "Collect", Result: `{"result":"string"}`, History: "full", Agent: "assistant", When: "Collect"}}
		data.TaskGroups = []scaffold.TaskGroup{{Name: "flow", Steps: []string{"collect"}, ContextScope: "shared", Then: "return", Agent: "assistant", When: "Flow"}}
		data.Channels = []scaffold.Channel{{Name: "phone", Kind: "telephony", Inbound: true}}
		data.HumanTransfers = []scaffold.HumanTransfer{{Name: "to_human", Agent: "assistant", When: "Human", Destination: "support", Value: "+14155550123", Mode: "cold"}}
		data.Fallbacks = []scaffold.ModelFallback{{Name: "backup", Profile: "assistant_model", Binding: data.Reason}}
		return data
	}

	tests := []struct {
		name  string
		input string
		open  func(*fieldRunner, *scaffold.Data) error
	}{
		{"variable", "1\n", editVariables},
		{"tool", "1\n", editTools},
		{"agent", "2\n", editAgents},
		{"handoff", "1\n", editHandoffs},
		{"task", "1\n", editTasks},
		{"task group", "1\n", editTaskGroups},
		{"human transfer", "1\n", editHumanTransfers},
		{"fallback", "1\n", editFallbacks},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := base()
			var output bytes.Buffer
			err := tc.open(newRunner(strings.NewReader(tc.input), &output, true), &data)
			if !errors.Is(err, huh.ErrUserAborted) {
				t.Fatalf("editor error = %v, want ErrUserAborted", err)
			}
			if !strings.Contains(output.String(), "Delete") {
				t.Fatalf("saved %s has no Delete action:\n%s", tc.name, output.String())
			}
		})
	}
}

func TestV25DeleteResourceCleansReferences(t *testing.T) {
	data := scaffold.Data{EntryAgent: "billing"}
	data.Agents = []scaffold.Agent{{Name: "billing"}}
	data.Variables = []scaffold.Variable{{Name: "customer_id", Type: "string"}}
	data.Tools = []scaffold.Tool{{Name: "lookup", AttachTo: []string{"billing"}, AttachTasks: []string{"collect"}}}
	data.Handoffs = []scaffold.Handoff{{Name: "to_billing", Source: "assistant", To: "billing", Requires: []string{"customer_id"}, Variables: []string{"customer_id"}}}
	data.Tasks = []scaffold.Task{{Name: "collect", Tools: []string{"lookup"}, Model: "billing_model", Assign: `{"customer_id":"result.result"}`, Agent: "billing"}}
	data.TaskGroups = []scaffold.TaskGroup{{Name: "flow", Steps: []string{"collect"}, Agent: "billing"}}
	data.HumanTransfers = []scaffold.HumanTransfer{{Name: "human", Agent: "billing"}}
	data.Fallbacks = []scaffold.ModelFallback{{Name: "backup", Profile: "billing_model"}}

	for _, item := range [][2]string{{"variable", "customer_id"}, {"tool", "lookup"}, {"agent", "billing"}, {"task", "collect"}} {
		if err := deleteResource(&data, item[0], item[1]); err != nil {
			t.Fatalf("delete %s: %v", item[0], err)
		}
	}
	if len(data.Variables)+len(data.Tools)+len(data.Agents)+len(data.Handoffs)+len(data.Tasks)+len(data.TaskGroups)+len(data.Fallbacks) != 0 {
		t.Fatalf("dangling resources after delete: %#v", data)
	}
	if data.EntryAgent != "assistant" || data.HumanTransfers[0].Agent != "assistant" {
		t.Fatalf("agent references were not reset: %#v", data)
	}
}

func TestV25InvalidSavedResourcesRemainAvailableForRepair(t *testing.T) {
	tests := []struct {
		name string
		data scaffold.Data
		open func(*fieldRunner, *scaffold.Data) error
	}{
		{"handoff without second agent", scaffold.Data{Handoffs: []scaffold.Handoff{{Name: "broken", Source: "assistant", To: "missing"}}}, editHandoffs},
		{"group without tasks", scaffold.Data{TaskGroups: []scaffold.TaskGroup{{Name: "broken", Steps: []string{"missing"}}}}, editTaskGroups},
		{"transfer without phone", scaffold.Data{HumanTransfers: []scaffold.HumanTransfer{{Name: "broken", Agent: "assistant"}}}, editHumanTransfers},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			err := tc.open(newRunner(strings.NewReader("1\n"), &output, true), &tc.data)
			if !errors.Is(err, huh.ErrUserAborted) {
				t.Fatalf("editor error = %v, want ErrUserAborted", err)
			}
			if !strings.Contains(output.String(), "Delete") {
				t.Fatalf("invalid saved resource cannot be repaired or deleted:\n%s", output.String())
			}
		})
	}
}

func TestValidateParams(t *testing.T) {
	for _, value := range []string{"", `{}`, `{"temperature":0.2}`} {
		if err := validateParams(value); err != nil {
			t.Errorf("validateParams(%q) = %v", value, err)
		}
	}
	for _, value := range []string{"[]", "nope", "null"} {
		if err := validateParams(value); err == nil {
			t.Errorf("validateParams(%q) accepted", value)
		}
	}
}

func TestValidateLanguage(t *testing.T) {
	for _, value := range []string{"en", "es-MX", "zh-Hans-CN"} {
		if err := validateLanguage(value); err != nil {
			t.Errorf("validateLanguage(%q) = %v", value, err)
		}
	}
	for _, value := range []string{"", "not_a_language"} {
		if err := validateLanguage(value); err == nil {
			t.Errorf("validateLanguage(%q) accepted", value)
		}
	}
}

func TestValidateVariableDefault(t *testing.T) {
	for _, tc := range []struct{ kind, value string }{
		{"string", `""`}, {"string", `"hello"`}, {"boolean", "false"},
		{"number", "1.5"}, {"integer", "2"},
	} {
		if err := validateVariableDefault(tc.kind, tc.value); err != nil {
			t.Errorf("%s %s: %v", tc.kind, tc.value, err)
		}
	}
	for _, tc := range []struct{ kind, value string }{{"string", "1"}, {"integer", "1.5"}, {"boolean", `"false"`}} {
		if err := validateVariableDefault(tc.kind, tc.value); err == nil {
			t.Errorf("%s %s accepted", tc.kind, tc.value)
		}
	}
}

func TestValidateTaskResult(t *testing.T) {
	for _, value := range []string{`{"ok":"boolean"}`, `{"tier":{"enum":["free","pro"]}}`} {
		if err := validateTaskResult(value); err != nil {
			t.Errorf("validateTaskResult(%s) = %v", value, err)
		}
	}
	for _, value := range []string{`{}`, `{"nested":{"type":"object"}}`, `{"score":"float"}`} {
		if err := validateTaskResult(value); err == nil {
			t.Errorf("validateTaskResult(%s) accepted", value)
		}
	}
}

func TestValidateDestination(t *testing.T) {
	for _, value := range []string{"+14155550123", "sip:agent@example.com", "sips:agent@example.com"} {
		if err := validateDestination(value); err != nil {
			t.Errorf("validateDestination(%q) = %v", value, err)
		}
	}
	for _, value := range []string{"14155550123", "https://example.com", "sip:no-host"} {
		if err := validateDestination(value); err == nil {
			t.Errorf("validateDestination(%q) accepted", value)
		}
	}
}

func TestValidateDuration(t *testing.T) {
	for _, value := range []string{"30s", "5m", "1h30m"} {
		if err := validateDuration(value); err != nil {
			t.Errorf("validateDuration(%q) = %v", value, err)
		}
	}
	for _, value := range []string{"", "5 minutes", "-1s", "0s"} {
		if err := validateDuration(value); err == nil {
			t.Errorf("validateDuration(%q) accepted", value)
		}
	}
}

func TestRunEOFAborts(t *testing.T) {
	t.Chdir(t.TempDir())
	_, err := Run(strings.NewReader("1\nagent\n"), &bytes.Buffer{}, true)
	if !errors.Is(err, huh.ErrUserAborted) {
		t.Fatalf("Run() error = %v, want ErrUserAborted", err)
	}
}

func TestBackKeyMapShowsFooterHint(t *testing.T) {
	help := backKeyMap().Input.Submit.Help()
	if !strings.Contains(help.Key, "esc back") || help.Desc != "submit" {
		t.Fatalf("input footer help = %#v", help)
	}
}

func TestValidateNameRefusesExistingDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("taken", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("taken", "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateName("taken"); !errors.Is(err, scaffold.ErrExists) {
		t.Fatalf("validateName() = %v, want ErrExists", err)
	}
}

func TestValidateBasicRejectsTemplateBreakers(t *testing.T) {
	for _, value := range []string{`bad"value`, "bad\nvalue", "bad\rvalue"} {
		if err := validateBasic(value); err == nil {
			t.Errorf("validateBasic(%q) accepted", value)
		}
	}
}

package tui

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/huh"
	"github.com/slng/unmute/internal/scaffold"
	targetcap "github.com/slng/unmute/internal/target"
)

func TestRunCreateDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	var output bytes.Buffer
	// 1=create, name=agent, 7=Create agent, ""=confirm default (yes).
	got, err := Run(strings.NewReader("1\nagent\n7\n\n"), &output, true)
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
	for _, label := range []string{"Identity", "Models", "Behavior", "Integrations", "Lifecycle", "Compile after create", "Create agent", "← Back"} {
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
	got, err := Run(strings.NewReader("3\n"), &bytes.Buffer{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Confirmed {
		t.Fatalf("quit returned a confirmed result: %#v", got)
	}
}

func TestV23WordmarkHasNoBackgroundColor(t *testing.T) { // docs/spec/tui.md V23
	if strings.Contains(slngWordmark, "48;") {
		t.Fatalf("wordmark still paints a background: %q", slngWordmark)
	}
	if !strings.Contains(slngWordmark, "38;2;245;201;110m") {
		t.Fatalf("wordmark lost the SLNG foreground color: %q", slngWordmark)
	}
}

func TestV26WordmarkRendersInsideHome(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	var output bytes.Buffer
	if _, err := Run(strings.NewReader("3\n"), &output, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "____") {
		t.Fatalf("home omitted wordmark:\n%s", output.String())
	}
}

func TestV26NoColorOmitsWordmark(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var output bytes.Buffer
	if _, err := Run(strings.NewReader("3\n"), &output, true); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "____") {
		t.Fatalf("NO_COLOR output contains wordmark:\n%s", output.String())
	}
}

func TestV27StepsRedrawInPersistentAltScreen(t *testing.T) {
	var output bytes.Buffer
	runner := newRunner(strings.NewReader(""), &output, false)
	if _, err := runner.runProgram(func() (Result, error) { return Result{}, nil }); err != nil {
		t.Fatal(err)
	}
	for _, sequence := range []string{"\x1b[?1049h", "\x1b[?1049l"} {
		if got := strings.Count(output.String(), sequence); got != 1 {
			t.Fatalf("alt-screen sequence %q count = %d, want 1", sequence, got)
		}
	}
}

func TestV35MenusDefaultToFirstActionAndBackLast(t *testing.T) { // docs/spec/tui.md V35
	runner := newRunner(strings.NewReader("\n"), &bytes.Buffer{}, true)
	choice, back, err := runner.selectOne("Actions", "", []huh.Option[string]{
		huh.NewOption("First action", "first"),
		huh.NewOption("Second action", "second"),
		huh.NewOption("← Back", actionBack),
	}, true)
	if err != nil || back || choice != "first" {
		t.Fatalf("default choice = %q, back = %v, error = %v; want first action", choice, back, err)
	}

	_, _, err = runner.selectOne("Broken", "", []huh.Option[string]{
		huh.NewOption("← Back", actionBack),
		huh.NewOption("Action", "action"),
	}, true)
	if err == nil || !strings.Contains(err.Error(), "Back action must be last") {
		t.Fatalf("misordered Back error = %v", err)
	}
}

func TestV34EditorSectionsStayGrouped(t *testing.T) { // docs/spec/tui.md V34
	data := scaffold.Data{}
	data.SetTarget(scaffold.DefaultTarget)
	options := editorSectionOptions(data)
	want := []struct {
		value, label string
	}{
		{"section:identity", "Identity  ·  target, language"},
		{"section:models", "Models  ·  "},
		{"section:behavior", "Behavior  ·  instructions, greeting, variables, advanced"},
		{"section:integrations", "Integrations  ·  tools, channels, human transfers"},
		{"section:lifecycle", "Lifecycle  ·  agents, handoffs, tasks, groups"},
	}
	if len(options) != len(want) {
		t.Fatalf("section count = %d, want %d", len(options), len(want))
	}
	for i := range want {
		if options[i].Value != want[i].value || !strings.HasPrefix(options[i].Key, want[i].label) {
			t.Errorf("section %d = (%q, %q), want (%q, prefix %q)", i, options[i].Value, options[i].Key, want[i].value, want[i].label)
		}
	}
}

func TestV36SectionsHaveNoPassThroughMenus(t *testing.T) { // docs/spec/tui.md V36
	data := scaffold.Data{}
	data.SetTarget(scaffold.DefaultTarget)
	var output bytes.Buffer
	if err := editModels(newRunner(strings.NewReader("5\n"), &output, true), &data); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Listen (STT)", "Reason (LLM)", "Speak (TTS)"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("Models section missing direct child %q:\n%s", want, output.String())
		}
	}
	if strings.Contains(output.String(), "Listen, reason, speak") {
		t.Fatalf("Models still contains a pass-through row:\n%s", output.String())
	}
}

func TestRunCompileToggle(t *testing.T) {
	t.Chdir(t.TempDir())
	// 1=create, name, 6=toggle compile on, 7=Create agent, confirm.
	got, err := Run(strings.NewReader("1\nagent\n6\n7\n\n"), &bytes.Buffer{}, true)
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
	got, err := Run(strings.NewReader("1\nagent\n1\n1\n2\n7\n\n"), &output, true)
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
	got, err := Run(strings.NewReader("1\nagent\n2\n3\n1\n1\nsonic-3\nvoice-id\n{\"speed\":1}\n5\n7\n\n"), &bytes.Buffer{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent.Data.Speak != (scaffold.Binding{Provider: "cartesia", Model: "sonic-3", Voice: "voice-id", Params: `{"speed":1}`}) {
		t.Fatalf("speak binding = %#v", got.Agent.Data.Speak)
	}
}

func TestRunEditLanguage(t *testing.T) {
	t.Chdir(t.TempDir())
	got, err := Run(strings.NewReader("1\nagent\n1\n2\nes-MX\n7\n\n"), &bytes.Buffer{}, true)
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
		"3\n3\n1\ncustomer_id\n1\n\"guest\"\n2\n3\n" +
		"4\n1\n1\nlookup_customer\nLook up the caller\n1\nLOOKUP_URL\n{\"type\":\"object\"}\n\n2\n3\n" +
		"7\n\n"
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

func TestV18VariablesMenuShowsSavedItems(t *testing.T) { // docs/spec/tui.md V18
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

func TestV18ToolsMenuUsesNeutralNameAndShowsExecution(t *testing.T) { // docs/spec/tui.md V18
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

func TestV18AgentMenuShowsAndEditsSavedAgent(t *testing.T) { // docs/spec/tui.md V18
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

func TestV19HandoffShowsExistingVariablesAsChoices(t *testing.T) { // docs/spec/tui.md V19
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

func TestV19TaskShowsExistingToolsAsChoices(t *testing.T) { // docs/spec/tui.md V19
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

func TestV22TaskResultExplainsPrefilledShape(t *testing.T) { // docs/spec/tui.md V22
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

func TestV19TaskAssignmentPicksSavedVariableAndResultField(t *testing.T) { // docs/spec/tui.md V19
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

func TestV21PreflightFailureUsesDedicatedScreen(t *testing.T) { // docs/spec/tui.md V21
	// Pipecat still gates fallback (driver-pipecat C9/B7); livekit emits it
	// natively since driver-livekit T5, so the failure fixture lives here.
	data := scaffold.Data{Name: "agent", Language: "en", Channel: "web"}
	data.SetTarget("pipecat")
	data.Fallbacks = []scaffold.ModelFallback{{Name: "backup", Profile: "assistant_model", Binding: data.Reason}}
	var output bytes.Buffer
	_, back, err := editAgent(newRunner(strings.NewReader("7\n10\n8\n"), &output, true), Agent{Path: "agent", Data: data})
	if err != nil {
		t.Fatal(err)
	}
	if !back {
		t.Fatal("editAgent() did not return through Back")
	}
	for _, want := range []string{"Cannot create agent", "Fix the configuration, then go Back", "does not emit generated fallback", "Review / delete model fallbacks"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("preflight screen missing %q:\n%s", want, output.String())
		}
	}
}

func TestRunHandoffsRequireTwoAgents(t *testing.T) {
	t.Chdir(t.TempDir())
	var output bytes.Buffer
	got, err := Run(strings.NewReader("1\nagent\n5\n2\n1\n7\n\n"), &output, true)
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
		"5\n1\n2\nbilling\nYou handle billing questions.\n" +
		"1\ngpt-4.1-mini\n\n" +
		"2\nslng/deepgram/aura:2-en\naura-2-thalia-en\n\n" +
		"5\n" +
		"5\n2\n1\n1\n1\nto_billing\nCaller asks about billing.\n1\n1\n3\n" +
		"7\n\n"
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
	if _, err := Run(strings.NewReader("1\nagent\n5\n4\n1\n7\n\n"), &output, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Create at least one task first") {
		t.Fatalf("task-group gate missing:\n%s", output.String())
	}
}

func TestRunAddTaskAndOrderedGroup(t *testing.T) {
	t.Chdir(t.TempDir())
	input := "1\nagent\n" +
		"5\n3\n1\ncollect\nCollect the caller tier.\n1\n{\"tier\":{\"enum\":[\"free\",\"pro\"]}}\n1\n1\n1\nClassify the caller.\n3\n" +
		"5\n4\n1\ntriage\n1\n2\n1\n1\n1\nRun triage.\n3\n" +
		"7\n\n"
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
	if _, err := Run(strings.NewReader("1\nagent\n4\n3\n1\n7\n\n"), &output, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Add a telephony caller channel first") {
		t.Fatalf("human-transfer gate missing:\n%s", output.String())
	}
}

func TestRunAddTelephonyAndHumanTransfer(t *testing.T) {
	t.Chdir(t.TempDir())
	input := "1\nagent\n" +
		"4\n2\n2\n1\n6\n9\ndaily-sip\n\n" +
		"4\n3\n1\nto_human\n1\nCaller requests a person.\nsupport_line\n+14155550123\n1\n3\n" +
		"7\n\n"
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
	input := "1\nagent\n3\n4\n3\n5\n10\n4m\n5\n7\n\n"
	got, err := Run(strings.NewReader(input), &bytes.Buffer{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent.Data.Capacity != (scaffold.Capacity{PeakSessions: 5, MaxSessions: 10, AvgSessionDuration: "4m"}) {
		t.Fatalf("capacity = %#v", got.Agent.Data.Capacity)
	}
}

func TestV20BackPreservesPriorEdits(t *testing.T) { // docs/spec/tui.md V20
	t.Chdir(t.TempDir())
	input := "1\nagent\n1\n2\nes-MX\n3\n1\n:back\n7\n\n"
	got, err := Run(strings.NewReader(input), &bytes.Buffer{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent.Data.Language != "es-MX" || got.Agent.Data.Instructions != scaffold.DefaultInstructions {
		t.Fatalf("back lost edits: %#v", got.Agent.Data)
	}
}

func TestV31ProviderOptionsMirrorCatalog(t *testing.T) {
	type pair struct {
		framework targetcap.Provider
		role      targetcap.Role
	}
	catalog := targetcap.DefaultCatalog()
	expected := map[pair][]string{}
	for _, entry := range catalog.Entries() {
		key := pair{entry.Framework, entry.Role}
		if entry.Wildcard() {
			if _, ok := expected[key]; !ok {
				expected[key] = nil
			}
			continue
		}
		brands := entry.Distributes
		if len(brands) == 0 {
			brands = []string{entry.Vendor}
		}
		expected[key] = append(expected[key], brands...)
	}
	for key, brands := range expected {
		slices.Sort(brands)
		brands = slices.Compact(brands)
		options := providerOptions(key.framework, key.role)
		got := make([]string, len(options))
		for i := range options {
			got[i] = options[i].Value
		}
		if !slices.Equal(got, brands) {
			t.Errorf("%s/%s options = %v, catalogue brands = %v", key.framework, key.role, got, brands)
		}
	}
}

func TestV31CatalogueHintUsesEntryArityAndLanguageSlot(t *testing.T) {
	entry, ok := targetcap.DefaultCatalog().Lookup(targetcap.Pipecat, targetcap.Listen, "deepgram")
	if !ok {
		t.Fatal("missing pipecat/listen/deepgram catalogue entry")
	}
	hint := catalogueEntryHint(entry)
	for _, want := range []string{"model → model (required)", "voice unavailable", "language → language (optional)"} {
		if !strings.Contains(hint, want) {
			t.Errorf("catalogue hint %q omits %q", hint, want)
		}
	}
}

func TestV29UnavailableChoiceNamesGateAndOffersBack(t *testing.T) {
	var output bytes.Buffer
	tool := scaffold.Tool{}
	back, err := chooseToolExecution(newRunner(strings.NewReader("2\n1\n3\n"), &output, true), string(targetcap.Pipecat), &tool)
	if err != nil {
		t.Fatal(err)
	}
	if !back {
		t.Fatal("unavailable choice did not permit Back")
	}
	want := targetcap.Default().Capability(targetcap.FieldToolLocal, targetcap.Pipecat).Note
	if !strings.Contains(output.String(), want) || !strings.Contains(output.String(), "← Back") {
		t.Fatalf("unavailable choice omitted exact gate or Back:\n%s", output.String())
	}
}

func TestV24ModelsLabelDeduplicatesProviderBrands(t *testing.T) { // docs/spec/tui.md V24
	data := scaffold.Data{}
	data.SetTarget("livekit")
	if got := modelsLabel(data); got != "deepgram / openai" {
		t.Fatalf("models label = %q", got)
	}
}

func TestV24ProviderThenDistributorFlow(t *testing.T) { // docs/spec/tui.md V24
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

func TestV24ProviderBrandsAreUniqueAndExposeDistributors(t *testing.T) { // docs/spec/tui.md V24
	type pair struct {
		framework targetcap.Provider
		role      targetcap.Role
	}
	catalog := targetcap.DefaultCatalog()
	pairs := map[pair]bool{}
	for _, entry := range catalog.Entries() {
		pairs[pair{entry.Framework, entry.Role}] = true
	}
	for key := range pairs {
		seen := map[string]bool{}
		for _, option := range providerOptions(key.framework, key.role) {
			if seen[option.Value] {
				t.Errorf("%s/%s repeats provider brand %q", key.framework, key.role, option.Value)
			}
			seen[option.Value] = true
			if routes := catalog.Distributors(key.framework, key.role, option.Value); len(routes) == 0 {
				t.Errorf("%s/%s provider brand %q has no distributor", key.framework, key.role, option.Value)
			}
		}
	}
}

func TestV25SavedResourcesOfferDelete(t *testing.T) { // docs/spec/tui.md V25
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

func TestV25DeleteResourceCleansReferences(t *testing.T) { // docs/spec/tui.md V25
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

func TestV25InvalidSavedResourcesRemainAvailableForRepair(t *testing.T) { // docs/spec/tui.md V25
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

func TestOpenDiscoversOnlyImmediateAgentPackages(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"b", "a", filepath.Join("nested", "agent")} {
		path := filepath.Join(root, dir)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "agent.yaml"), []byte("version: 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := discoverPackages(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(root, "a"), filepath.Join(root, "b")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discoverPackages() = %v, want %v", got, want)
	}
}

func TestV32MaintainNoOpSaveIsByteIdentical(t *testing.T) {
	root := filepath.Join(t.TempDir(), "agent")
	data := scaffold.Data{Name: "agent"}
	data.SetTarget(scaffold.DefaultTarget)
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	before := packageBytes(t, root)
	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := saveMaintained(newRunner(strings.NewReader("1\n"), &output, true), &agent); err != nil {
		t.Fatal(err)
	}
	after := packageBytes(t, root)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("no-op Save changed package bytes")
	}
	if !strings.Contains(output.String(), "No changes to save") {
		t.Fatalf("missing no-op notice:\n%s", output.String())
	}
}

func TestV32DestructiveSaveNamesFilesAndConfirms(t *testing.T) {
	root := filepath.Join(t.TempDir(), "agent")
	data := scaffold.Data{Name: "agent"}
	data.SetTarget(scaffold.DefaultTarget)
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	agent.data.Language = "es-MX"
	var output bytes.Buffer
	// Confirm rewrite, then Back from the saved notice.
	if err := saveMaintained(newRunner(strings.NewReader("1\n1\n"), &output, true), &agent); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(root, "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), "language: es-MX") {
		t.Fatalf("saved manifest omitted edit:\n%s", manifest)
	}
	if !strings.Contains(output.String(), "agent.yaml") || !strings.Contains(output.String(), "Rewrite listed files") {
		t.Fatalf("confirmation omitted affected file:\n%s", output.String())
	}
}

func TestV20MaintainBackPreservesPriorEdits(t *testing.T) { // docs/spec/tui.md V20
	root := filepath.Join(t.TempDir(), "agent")
	data := scaffold.Data{Name: "agent"}
	data.SetTarget(scaffold.DefaultTarget)
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	// Language, new value, Back from the maintain menu.
	if err := editMaintained(newRunner(strings.NewReader("1\n2\nes-MX\n9\n"), &bytes.Buffer{}, true), &agent); err != nil {
		t.Fatal(err)
	}
	if agent.data.Language != "es-MX" {
		t.Fatalf("Back lost maintain edit: %#v", agent.data)
	}
}

func TestV33ValidateStaysInConsole(t *testing.T) {
	agent := testMaintainedAgent(t)
	var output bytes.Buffer
	runner := newRunner(strings.NewReader("6\n1\n9\n"), &output, true)
	runner.actions = func(action, path string, out io.Writer) error {
		if action != "validate" || path != agent.path {
			t.Fatalf("action = %q, path = %q", action, path)
		}
		_, _ = io.WriteString(out, "TARGET\tPROVIDER\tRESULT\npipecat-dev\tpipecat\tpass\n")
		return nil
	}
	if err := editMaintained(runner, &agent); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "pipecat-dev") || strings.Count(output.String(), "Enter a number between 1 and 9") < 2 {
		t.Fatalf("Validate report did not return to maintain menu:\n%s", output.String())
	}
}

func TestV33CompileStaysInConsole(t *testing.T) {
	agent := testMaintainedAgent(t)
	var output bytes.Buffer
	runner := newRunner(strings.NewReader("7\n1\n9\n"), &output, true)
	runner.actions = func(action, path string, out io.Writer) error {
		if action != "compile" || path != agent.path {
			t.Fatalf("action = %q, path = %q", action, path)
		}
		_, _ = io.WriteString(out, "generated build/pipecat-dev/agent.py\n")
		return nil
	}
	if err := editMaintained(runner, &agent); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "generated build/pipecat-dev/agent.py") || strings.Count(output.String(), "Enter a number between 1 and 9") < 2 {
		t.Fatalf("Compile report did not return to maintain menu:\n%s", output.String())
	}
}

func testMaintainedAgent(t *testing.T) maintainedAgent {
	t.Helper()
	root := filepath.Join(t.TempDir(), "agent")
	data := scaffold.Data{Name: "agent"}
	data.SetTarget(scaffold.DefaultTarget)
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

func TestOpenReportsUnrepresentableFieldPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "agent")
	data := scaffold.Data{Name: "agent"}
	data.SetTarget(scaffold.DefaultTarget)
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "targets.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content = bytes.Replace(content, []byte(`model: "gpt-4.1-mini"`), []byte(`model: "gpt-4.1-mini", endpoint_env: CUSTOM_LLM_URL`), 1)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(agent.losses, "\n"); !strings.Contains(got, "endpoint_env") || !strings.Contains(got, "targets.yaml") {
		t.Fatalf("loss report = %q", got)
	}
}

func packageBytes(t *testing.T, root string) map[string]string {
	t.Helper()
	paths, err := knownPackageFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	result := make(map[string]string, len(paths))
	for _, path := range paths {
		content, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		result[path] = string(content)
	}
	return result
}

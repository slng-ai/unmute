package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// The L4 variable and webhook smokes drive a surface no shipped example has:
// salon-concierge declares one call_start variable and no webhook tool, and the
// package that had the rest (outbound-reminder) was deleted. So the fixtures
// here add that surface to the resolved package in memory. Nothing under
// examples/ is touched, and nothing external is contacted.
//
// They live in an untagged file on purpose: the guard at the bottom runs in the
// default suite, so a fixture that no longer generates fails in seconds instead
// of waiting for someone to run `make smoke`.

// addReminderVariables gives the package one variable per source the smokes
// care about: two that arrive with the dispatch, one the runtime owns, and one
// the model saves mid-call through the generated update_variables tool. The
// greeting gains a template because a session-start template is what makes the
// _render helper exist at all (renderNeeds).
//
// The two dispatch variables carry a default because the salon takes inbound
// calls, and nothing dispatches values into an inbound one: a call_start
// variable with no default is refused on an inbound package.
func addReminderVariables(agent *ir.Agent) {
	agent.Variables["name"] = ir.Variable{
		Type:        ir.PrimitiveString,
		Default:     "",
		Source:      ir.VariableSourceCallStart,
		Description: "Customer's first name, used in the greeting and the prompt.",
	}
	agent.Variables["appointment_time"] = ir.Variable{
		Type:        ir.PrimitiveString,
		Default:     "",
		Source:      ir.VariableSourceCallStart,
		Description: `Appointment start in spoken form, for example "tomorrow at 3 pm".`,
	}
	agent.Variables["dialed_number"] = ir.Variable{
		Type:   ir.PrimitiveString,
		Source: ir.VariableSourceToNumber,
	}
	agent.Variables["reschedule_to"] = ir.Variable{
		Type:        ir.PrimitiveString,
		Source:      ir.VariableSourceConversation,
		Description: "New slot the customer asks for, in spoken form. Save it as soon as the customer names one.",
	}
	if agent.Conversation != nil && agent.Conversation.Greeting != nil {
		agent.Conversation.Greeting.Text = "Hi {{name}}! This is Sage and Stone Salon about your appointment {{appointment_time}}."
	}
}

// useWebhookTools builds its two tools here rather than borrowing them from the
// example: it used to rewrite tools by name, and when the example stopped having
// those names the map miss handed it a zero-value ir.Tool, so every webhook
// smoke failed on eight validation errors (B: webhook smokes broken by the
// examples cut-down, 2026-08-24).
func useWebhookTools(agent *ir.Agent) {
	addReminderVariables(agent)
	agent.Tools["confirm_appointment"] = webhookTool(
		"Confirm that the existing appointment stays as booked. Call it when the customer says the time works.",
		"/customers/{{customer_phone}}/appointments/confirm",
		map[string]any{"customer_phone": "{{customer_phone}}", "dialed_number": "{{dialed_number}}", "channel": "phone"},
	)
	agent.Tools["reschedule_appointment"] = webhookTool(
		"Move the appointment to the slot the customer asked for. Save the slot with update_variables first; this tool reads it on its own.",
		"/customers/{{customer_phone}}/appointments",
		map[string]any{"customer_phone": "{{customer_phone}}", "new_time": "{{reschedule_to}}"},
	)
	def := agent.Agents[agent.EntryAgent]
	def.Tools = append(def.Tools, "confirm_appointment", "reschedule_appointment")
	agent.Agents[agent.EntryAgent] = def
	agent.Secrets = append(agent.Secrets, "SALON_API_URL", "SALON_API_TOKEN")
}

// webhookTool is one authenticated webhook the model calls with no arguments:
// every value it sends is injected, so nothing is left for the model to invent.
func webhookTool(description, path string, inject map[string]any) ir.Tool {
	return ir.Tool{
		Description:  description,
		Input:        map[string]any{"type": "object", "properties": map[string]any{}},
		Path:         path,
		Inject:       inject,
		Execution:    ir.ToolWebhook,
		URLEnv:       "SALON_API_URL",
		Auth:         &ir.ToolAuth{Type: ir.ToolAuthBearer, TokenEnv: "SALON_API_TOKEN"},
		Interruption: ir.ToolProviderDefault,
		Effect:       ir.ToolReturnsData,
	}
}

// useAPIKeyAuth switches the synthetic webhook fixture to the other supported
// auth scheme.
func useAPIKeyAuth(agent *ir.Agent) {
	useWebhookTools(agent)
	for _, name := range []string{"confirm_appointment", "reschedule_appointment"} {
		tool := agent.Tools[name]
		tool.Auth = &ir.ToolAuth{Type: ir.ToolAuthAPIKey, TokenEnv: "SALON_API_TOKEN", Header: ir.DefaultAPIKeyHeader}
		agent.Tools[name] = tool
	}
}

// TestSmokeFixturesGenerateAndKeepTheirPythonSurface is the default-suite guard
// for the three fixtures above. It proves each one still compiles the salon
// package, and that the Python names the smoke scripts reach for are in the
// emitted file. Both halves are how the four webhook smokes and the two
// variables smokes broke: the fixtures went on building an invalid package, and
// the scripts went on naming a class the example no longer emits.
func TestSmokeFixturesGenerateAndKeepTheirPythonSurface(t *testing.T) {
	fixtures := []struct {
		name   string
		mutate func(*ir.Agent)
	}{
		{"variables", addReminderVariables},
		{"webhook", useWebhookTools},
		{"api_key", useAPIKeyAuth},
	}
	drivers := []struct {
		provider ir.Provider
		file     string
		// entry is the emitted class for the entry agent, which every smoke
		// script constructs by name.
		entry string
	}{
		{ir.ProviderPipecat, "bot.py", "class ConciergeAgent("},
		{ir.ProviderLiveKit, "agent.py", "class Concierge("},
	}
	for _, fixture := range fixtures {
		for _, driver := range drivers {
			t.Run(fixture.name+"/"+string(driver.provider), func(t *testing.T) {
				pkg, err := spec.Load(filepath.Join("..", "..", "examples", "salon-concierge"))
				if err != nil {
					t.Fatal(err)
				}
				agent, err := ir.Build(pkg)
				if err != nil {
					t.Fatal(err)
				}
				fixture.mutate(agent)
				artifact, err := Generate(agent, targetByProvider(t, agent, driver.provider), target.Default())
				if err != nil {
					t.Fatalf("fixture %s no longer generates: %v", fixture.name, err)
				}
				emitted := artifactFile(t, artifact, driver.file)
				for _, symbol := range []string{driver.entry, "def _render(", "update_variables"} {
					if !strings.Contains(emitted, symbol) {
						t.Errorf("%s is missing %q, so the smoke script that names it cannot run", driver.file, symbol)
					}
				}
				// Every `{{name}}` these fixtures template must be a variable the
				// package actually declares.
				//
				// The fixtures mutate the resolved IR and call Generate directly,
				// which skips ir.Validate, so an undeclared name is not refused
				// here the way it would be in a real package: it compiles happily
				// into `state.<name>` and dies at runtime with an AttributeError,
				// deep inside a twenty-minute opt-in suite. That is exactly what
				// happened when salon-concierge dropped its customer_id variable
				// and these fixtures kept templating it.
				for toolName, tool := range agent.Tools {
					for _, site := range append([]string{tool.Path}, injectTemplates(tool)...) {
						for _, name := range ir.TemplateRefs(site) {
							if _, isVault := ir.VaultToken(name); isVault {
								continue
							}
							if _, ok := agent.Variables[name]; !ok {
								t.Errorf("fixture %s: tool %q templates {{%s}}, which the package does not declare; the smoke would compile fine and fail at runtime with an AttributeError", fixture.name, toolName, name)
							}
						}
					}
				}
			})
		}
	}
}

// TestKnowledgeSmokeKeepsItsPythonSurface is the same guard for the knowledge
// smoke, and it exists for the same reason: three commits once broke `make smoke`
// silently because the scripts named emitted symbols and nothing pinned them.
//
// The knowledge smoke reaches further into the module than the others do, because
// the parts worth proving are private: _exact and _merge are where the measured
// 15/15 lives, and an assertion on look_up alone with a mock embedder would not
// touch either. So every name it uses is pinned here, in the default suite, where
// a rename fails in seconds instead of waiting for someone to run `make smoke`.
func TestKnowledgeSmokeKeepsItsPythonSurface(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "salon-concierge"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if len(agent.Knowledge) == 0 {
		t.Fatal("the salon example declares no knowledge base, so the knowledge smoke is asserting nothing")
	}
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		t.Run(string(provider), func(t *testing.T) {
			artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
			if err != nil {
				t.Fatalf("the knowledge smoke fixture no longer generates: %v", err)
			}
			emitted := artifactFile(t, artifact, "knowledge.py")
			for _, symbol := range []string{
				"def build_indexes(", "async def look_up(", "def _merge(", "def _index(",
				"def _nodes(", "_INDEXES", "SETTINGS", "_embed_refunds(",
				"_embed_services(", "BM25Retriever.from_defaults(",
			} {
				if !strings.Contains(emitted, symbol) {
					t.Errorf("knowledge.py is missing %q, so the smoke script that names it cannot run", symbol)
				}
			}
			// The smoke reads entry["vector"] and entry["keyword"] off each index
			// map value, so the value has to still be a dict. It last was a
			// (index, collection) tuple, and unpacking a dict yields its keys
			// instead of raising, so the smoke failed inside Chroma's API rather
			// than saying the shape had changed.
			if !strings.Contains(emitted, "_INDEXES: dict[str, dict]") {
				t.Error("the index map shape changed; the smoke reads entry[\"vector\"] and entry[\"keyword\"]")
			}
			// And the documents have to be in the artifact, or the smoke indexes
			// nothing and passes for the wrong reason.
			var documents int
			for _, file := range artifact.Files {
				if strings.HasPrefix(file.Path, "knowledge/") {
					documents++
				}
			}
			if documents != 2 {
				t.Errorf("artifact carries %d knowledge documents, want the example's 2", documents)
			}
		})
	}
}

// injectTemplates lists every template site in a tool's inject block.
func injectTemplates(tool ir.Tool) []string {
	sites := make([]string, 0, len(tool.Inject))
	for _, value := range tool.Inject {
		if text, ok := value.(string); ok {
			sites = append(sites, text)
		}
	}
	return sites
}

// TestSalonJourneySmokeKeepsItsPythonSurface pins the emitted names the salon
// journey smokes call by name.
//
// It exists because the shape change that added this test broke those smokes in
// exactly the way nothing caught: the booking step moved from its own agent onto
// the entry agent, so `bot.BookingSpecialistAgent` stopped existing, and the
// only thing that noticed was an AttributeError thirty minutes into an opt-in
// suite nobody runs before pushing. The same commit renamed the caller
// identifier, and that surfaced the same way.
//
// Symbols, not behaviour. `make smoke` still owns whether the Python runs; this
// owns whether the names it types are still there, in seconds, in the suite that
// actually gates a PR.
func TestSalonJourneySmokeKeepsItsPythonSurface(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "salon-concierge"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	for _, driver := range []struct {
		provider ir.Provider
		file     string
		symbols  []string
	}{
		{ir.ProviderLiveKit, "agent.py", []string{
			"class Userdata:", "class Booking(", "class CustomerVerification(",
			"class ComplaintSpecialist(", "class _TaskTransfer(",
			"async def record_complaint(", "async def to_complaints(",
			// The smoke scripts drive the pre-fetch directly, because that is the
			// one part of it a conversation cannot reach: an entry that skips looks
			// exactly like an entry that ran, from outside.
			"async def _prefetch(",
		}},
		{ir.ProviderPipecat, "bot.py", []string{
			"class State:", "class ConciergeAgent(", "class ComplaintSpecialistAgent(",
			"def _flow_tool_cancel_booking(", "def _flow_tool_check_availability(",
			"def _flow_tool_create_booking(", "def _flow_tool_find_or_create_customer(",
			"def _flow_tool_list_bookings(", "async def _prefetch(",
			"_manage_booking_active_step", "_manage_booking_results",
			"_manage_booking_snapshot", "_manage_booking_finish_booking",
			"_manage_booking_transfer_booking_to_complaints",
			"_verify_customer_results", "_verify_customer_snapshot",
			"_verify_customer_finish_customer_verification",
		}},
	} {
		t.Run(string(driver.provider), func(t *testing.T) {
			artifact, err := Generate(agent, targetByProvider(t, agent, driver.provider), target.Default())
			if err != nil {
				t.Fatalf("the salon package no longer generates: %v", err)
			}
			emitted := artifactFile(t, artifact, driver.file)
			for _, symbol := range driver.symbols {
				if !strings.Contains(emitted, symbol) {
					t.Errorf("%s is missing %q, so the salon journey smoke that names it cannot run", driver.file, symbol)
				}
			}
		})
	}

	// The smokes construct Userdata and State with the caller identifier by
	// keyword, so a rename has to fail here rather than at runtime.
	if _, ok := agent.Variables["customer_phone"]; !ok {
		t.Error("the salon package no longer declares customer_phone; the journey smokes pass it by keyword")
	}
}

// TestSmokeStubbedNamesExistInTheEmittedModule closes the gap that let a
// twenty-minute suite be the first thing to notice a broken change.
//
// The gates above pin the names a smoke script *constructs*. They do not pin the
// names it *monkeypatches*, and a script patches a module attribute by name:
//
//	bot.SileroVADAnalyzer = lambda **kwargs: None
//
// A patch whose target no longer takes those arguments, or no longer exists,
// fails at import inside `make smoke` — opt-in, needs Python, and in one observed
// run the failure landed after the suite had already burned its whole budget.
//
// This holds the same contract in the default suite: every name the Pipecat smoke
// scripts replace is present in the emitted module, and the call the emitted
// module makes matches what the stub accepts.
func TestSmokeStubbedNamesExistInTheEmittedModule(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "salon-concierge"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
	if err != nil {
		t.Fatal(err)
	}
	emitted := artifactFile(t, artifact, "bot.py")

	// name -> whether the emitted module calls it with keyword arguments. A
	// stub written `lambda: None` breaks the moment the generator starts passing
	// any, which is exactly what happened when the VAD gained params=.
	for name, needsKwargs := range map[string]bool{
		"SileroVADAnalyzer":        true,
		"LocalSmartTurnAnalyzerV3": true,
		"LLMContextAggregatorPair": false,
		"WorkerRunner":             false,
		// The pre-fetch smoke scripts replace the pre-fetched lookup with a slow
		// stub and then with an exploding one, to drive the budget and the except
		// arm. Both stubs take **kwargs, so the emitted call has to pass some: a
		// generator that started calling it positionally would make both stubs
		// pass for the wrong reason.
		"tools.look_up_customer.look_up_customer": true,
	} {
		if !strings.Contains(emitted, name) {
			t.Errorf("bot.py no longer names %q, so the smoke script that patches bot.%s patches nothing", name, name)
			continue
		}
		if !needsKwargs {
			continue
		}
		// Find the call and confirm it passes something, so a stub taking no
		// kwargs would fail. The smoke stubs are `lambda **kwargs: None`.
		call := name + "("
		idx := strings.Index(emitted, call)
		if idx < 0 {
			t.Errorf("bot.py names %q but never calls it", name)
			continue
		}
		rest := emitted[idx+len(call):]
		end := strings.IndexByte(rest, '\n')
		if end < 0 {
			end = len(rest)
		}
		if strings.HasPrefix(strings.TrimSpace(rest[:end]), ")") {
			t.Errorf("bot.py calls %s() with no arguments; the smoke stub expects kwargs, so update one or the other deliberately", name)
		}
	}
}

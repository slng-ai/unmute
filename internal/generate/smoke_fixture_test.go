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
		"/customers/{{customer_id}}/appointments/confirm",
		map[string]any{"customer_id": "{{customer_id}}", "dialed_number": "{{dialed_number}}", "channel": "phone"},
	)
	agent.Tools["reschedule_appointment"] = webhookTool(
		"Move the appointment to the slot the customer asked for. Save the slot with update_variables first; this tool reads it on its own.",
		"/customers/{{customer_id}}/appointments",
		map[string]any{"customer_id": "{{customer_id}}", "new_time": "{{reschedule_to}}"},
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
			})
		}
	}
}

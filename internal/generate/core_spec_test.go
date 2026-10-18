package generate

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

func TestALineReadsWhatItsToolGivesIt(t *testing.T) {
	input := map[string][]string{"booking": {"booking"}, "policy_number": {"policy_number"}}
	result := map[string][]string{"appointment": {"appointment"}, "claim_number": {"filed", "number"}}
	for _, tc := range []struct{ line, want string }{
		{"Book {{booking__day}} at {{booking__time}}?", "Book {{input.booking.day}} at {{input.booking.time}}?"},
		{"Is it {{policy_number}}?", "Is it {{input.policy_number}}?"},
		{"Set for {{appointment__spoken}}.", "Set for {{result.appointment.spoken}}."},
		{"Your number is {{claim_number}}.", "Your number is {{result.filed.number}}."},
		{"Thanks, {{caller_name}}.", "Thanks, {{caller_name}}."},
		{"Call {{call__session_id}}.", "Call {{call.session_id}}."},
	} {
		if got := rewriteLines([]string{tc.line}, input, result)[0]; got != tc.want {
			t.Errorf("%q: got %q, want %q", tc.line, got, tc.want)
		}
	}
}

func TestAMatchTakesTheTypeTheToolDeclares(t *testing.T) {
	output := map[string]any{"properties": map[string]any{
		"found": map[string]any{"type": "boolean"}, "tries": map[string]any{"type": "integer"},
		"status": map[string]any{"type": "string"},
	}}
	got, err := typedMatch(output, map[string][]string{"found": {"true"}, "tries": {"2"}, "status": {"ok"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got["found"], []any{true}) || !slices.Equal(got["tries"], []any{2}) || !slices.Equal(got["status"], []any{"ok"}) {
		t.Fatalf("got %v", got)
	}
}

func TestAWebhookToolCompilesWithItsEndpointAndRefusesReplies(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "webhook_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := buildWithState(t, pkg)
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
		if err != nil {
			t.Fatalf("%s: %v", provider, err)
		}
		emitted := artifactFile(t, artifact, "agent.json")
		for _, want := range []string{`"url_env": "ORDERS_API_URL"`, `"path": "/orders"`, `"kind": "bearer"`, `"call": "{{call.session_id}}"`} {
			if !strings.Contains(emitted, want) {
				t.Errorf("%s: agent.json lacks %s", provider, want)
			}
		}
		report := artifactFile(t, artifact, "compile-report.json")
		for _, name := range []string{"ORDERS_API_URL", "ORDERS_API_TOKEN"} {
			if !strings.Contains(report, `"`+name+`"`) {
				t.Errorf("%s: required_env lacks %s", provider, name)
			}
		}
	}
	tool := agent.Tools["look_up_order"]
	tool.Replies = []ir.Reply{{When: map[string][]string{"status": {"shipped"}}, Say: []string{"It has shipped."}}}
	agent.Tools["look_up_order"] = tool
	if _, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default()); err == nil || !strings.Contains(err.Error(), "webhook tool takes neither") {
		t.Fatalf("a webhook with replies compiled, or said something else: %v", err)
	}
}

func TestSpeechThatIsNotSlngComesFromTheCatalogue(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "speech_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := buildWithState(t, pkg)
	if err != nil {
		t.Fatal(err)
	}
	for provider, wants := range map[ir.Provider][]string{
		ir.ProviderLiveKit: {"deepgram.STT(", "cartesia.TTS(", "elevenlabs.TTS(", "deepgram.TTS("},
		ir.ProviderPipecat: {"DeepgramSTTService(", "CartesiaTTSService(", "ElevenLabsTTSService(", "DeepgramTTSService("},
	} {
		artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
		if err != nil {
			t.Fatalf("%s: %v", provider, err)
		}
		speech := artifactFile(t, artifact, "speech.py")
		for _, want := range wants {
			if !strings.Contains(speech, want) {
				t.Errorf("%s: speech.py builds no %s", provider, want)
			}
		}
		// The eleven desk listens with its own model, by its name in agent.yaml.
		for _, want := range []string{`"transcriber": listen_transcriber`, `"nova_two": listen_nova_two`, `model="nova-2"`} {
			if !strings.Contains(speech, want) {
				t.Errorf("%s: speech.py has no %s", provider, want)
			}
		}
		if spec := artifactFile(t, artifact, "agent.json"); !strings.Contains(spec, `"listen": "nova_two"`) {
			t.Errorf("%s: agent.json does not give the eleven desk its own listen model", provider)
		}
		report := artifactFile(t, artifact, "compile-report.json")
		for _, name := range []string{"DEEPGRAM_API_KEY", "CARTESIA_API_KEY"} {
			if !strings.Contains(report, `"`+name+`"`) {
				t.Errorf("%s: required_env lacks %s", provider, name)
			}
		}
	}
}

func TestALiveModelHandsItsWorkToTheBackendItNames(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "takeaway-orders"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := buildWithState(t, pkg)
	if err != nil {
		t.Fatal(err)
	}
	for provider, want := range map[ir.Provider]string{
		ir.ProviderLiveKit: `delegation="responses", responses_options={"model": "gpt-5.6-terra"}`,
		ir.ProviderPipecat: `OpenAIResponsesLLMService.Settings(model="gpt-5.6-terra")`,
	} {
		artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
		if err != nil {
			t.Fatalf("%s: %v", provider, err)
		}
		if speech := artifactFile(t, artifact, "speech.py"); !strings.Contains(speech, want) {
			t.Errorf("%s: speech.py hands the live model's work to no backend:\n%s", provider, speech)
		}
		if spec := artifactFile(t, artifact, "agent.json"); !strings.Contains(spec, `"realtime": "counter_voice"`) {
			t.Errorf("%s: the agent does not run on its live model:\n%s", provider, spec)
		}
	}
}

func TestTwilioRefusesTheVersion2KeysItsRuntimeCannotRun(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", voiceAgentTestsDir, "relay-desk"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := buildWithState(t, pkg)
	if err != nil {
		t.Fatal(err)
	}
	resolved := targetByProvider(t, agent, ir.ProviderTwilio)
	if _, err := Generate(agent, resolved, target.Default()); err != nil {
		t.Fatalf("relay-desk does not compile for twilio: %v", err)
	}
	tool := agent.Tools["opening_hours"]
	tool.Replies = []ir.Reply{{When: map[string][]string{"open": {"true"}}, Say: []string{"We're open."}}}
	agent.Tools["opening_hours"] = tool
	if _, err := Generate(agent, resolved, target.Default()); err == nil || !strings.Contains(err.Error(), "cannot run agent.yaml tools: opening_hours outcomes") {
		t.Fatalf("a reply compiled for twilio, or was refused for another reason: %v", err)
	}
}

func TestAVersion2ProjectCarriesWhatUnmuteDevRuns(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "customer-intake"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := buildWithState(t, pkg)
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		resolved := targetByProvider(t, agent, provider)
		artifact, err := Generate(agent, resolved, target.Default())
		if err != nil {
			t.Fatalf("%s: %v", provider, err)
		}
		compose := artifactFile(t, artifact, "compose.dev.yaml")
		for _, want := range []string{"UNMUTE_DEV_METRICS", "UNMUTE_CALL_FACTS", "OPENAI_API_KEY"} {
			if !strings.Contains(compose, "- "+want) {
				t.Errorf("%s: compose.dev.yaml does not pass %s to the agent", provider, want)
			}
		}
		if provider == ir.ProviderLiveKit && !strings.Contains(compose, "LIVEKIT_AGENT_NAME="+agent.DeployName(resolved)) {
			t.Errorf("livekit: the worker does not register as %s, the name unmute dev dispatches to", agent.DeployName(resolved))
		}
		artifactFile(t, artifact, "Dockerfile")
	}
}

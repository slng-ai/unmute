package generate

import (
	"encoding/json"
	"encoding/xml"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

var updateTwilio = flag.Bool("update-twilio", false, "rewrite the twilio golden files")

// twilioArtifact compiles one target instance of a package.
func twilioArtifact(t *testing.T, dir, instance string) Artifact {
	t.Helper()
	pkg, err := spec.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, agent.Targets[instance], target.Default())
	if err != nil {
		t.Fatalf("generate %s: %v", instance, err)
	}
	return artifact
}

var relayDesk = filepath.Join("..", "voice-agents-tests", "relay-desk")

// One golden per think provider: OpenAI on the smallest fixture, Gemini on
// Vertex in the acceptance package. Everything the driver writes is in them.
func TestTwilioGolden(t *testing.T) {
	for _, tc := range []struct{ dir, instance, golden string }{
		{filepath.Join("..", "testdata", "twilio_relay"), "twilio", "twilio_v1_openai.txt"},
		{relayDesk, "twilio-gemini", "twilio_v1_gemini_vertex.txt"},
	} {
		t.Run(tc.golden, func(t *testing.T) {
			artifact := twilioArtifact(t, tc.dir, tc.instance)
			var out strings.Builder
			for _, file := range artifact.Files {
				out.WriteString("=== " + file.Path + " ===\n")
				out.Write(file.Content)
				if !strings.HasSuffix(string(file.Content), "\n") {
					out.WriteByte('\n')
				}
			}
			path := filepath.Join("testdata", "golden", tc.golden)
			if *updateTwilio {
				if err := os.WriteFile(path, []byte(out.String()), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if out.String() != string(want) {
				t.Fatalf("twilio golden %s differs; run: go test ./internal/generate -run TestTwilioGolden -update-twilio", tc.golden)
			}
		})
	}
}

// Each project carries only the SDK, dependency and key its think binding
// selects. The other provider's key may be absent from the machine entirely.
func TestTwilioArtifactCarriesOnlyTheSelectedProvider(t *testing.T) {
	for _, tc := range []struct{ instance, key, sdk, other, otherKey, otherSDK string }{
		{"twilio-openai", "OPENAI_API_KEY", "openai==", "google", "GOOGLE_API_KEY", "google-genai"},
		{"twilio-gemini", "GOOGLE_API_KEY", "google-genai==", "openai", "OPENAI_API_KEY", "openai=="},
	} {
		artifact := twilioArtifact(t, relayDesk, tc.instance)
		for _, path := range []string{"app.py", "pyproject.toml", ".env.example", "Dockerfile"} {
			body := artifactFile(t, artifact, path)
			if strings.Contains(body, tc.otherKey) || strings.Contains(body, tc.otherSDK) {
				t.Errorf("%s %s names the unselected provider (%s or %s)", tc.instance, path, tc.otherKey, tc.otherSDK)
			}
		}
		if !strings.Contains(artifactFile(t, artifact, "pyproject.toml"), tc.sdk) {
			t.Errorf("%s pyproject.toml does not pin %s", tc.instance, tc.sdk)
		}
		if !strings.Contains(artifactFile(t, artifact, ".env.example"), tc.key+"=") {
			t.Errorf("%s .env.example does not ask for %s", tc.instance, tc.key)
		}
		if strings.Contains(artifactFile(t, artifact, "app.py"), "import "+tc.other) {
			t.Errorf("%s app.py imports %s", tc.instance, tc.other)
		}
		var report struct {
			RequiredEnv []string `json:"required_env"`
			DeployEnv   []string `json:"deploy_env"`
		}
		if err := json.Unmarshal([]byte(artifactFile(t, artifact, "compile-report.json")), &report); err != nil {
			t.Fatal(err)
		}
		want := []string{tc.key, "TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN", "TWILIO_PUBLIC_URL"}
		slices.Sort(want)
		if !slices.Equal(report.RequiredEnv, want) {
			t.Errorf("%s required_env = %v, want %v", tc.instance, report.RequiredEnv, want)
		}
		if !slices.Equal(report.DeployEnv, []string{"TWILIO_PHONE_NUMBER_SID"}) {
			t.Errorf("%s deploy_env = %v, want the number SID only", tc.instance, report.DeployEnv)
		}
	}
}

// The TwiML template is XML a parser reads back exactly, with the greeting's
// quotes and ampersand intact, and every native setting the package asks for.
func TestTwilioTwiMLTemplate(t *testing.T) {
	artifact := twilioArtifact(t, filepath.Join("..", "testdata", "twilio_relay"), "twilio")
	var doc struct {
		Connect struct {
			Action string `xml:"action,attr"`
			Method string `xml:"method,attr"`
			Relay  struct {
				Attrs []xml.Attr `xml:",any,attr"`
			} `xml:"ConversationRelay"`
		} `xml:"Connect"`
		Hangup *struct{} `xml:"Hangup"`
	}
	if err := xml.Unmarshal([]byte(artifactFile(t, artifact, "conversation-relay.xml.tmpl")), &doc); err != nil {
		t.Fatalf("the TwiML template is not XML: %v", err)
	}
	attrs := map[string]string{}
	for _, attr := range doc.Connect.Relay.Attrs {
		attrs[attr.Name.Local] = attr.Value
	}
	for name, want := range map[string]string{
		"url":                          "__PUBLIC_WSS_ORIGIN__/conversation",
		"welcomeGreeting":              `Hello & welcome, say "desk" to start.`,
		"welcomeGreetingInterruptible": "speech",
		"transcriptionProvider":        "Deepgram",
		"speechModel":                  "nova-3-general",
		"transcriptionLanguage":        "en-US",
		"ttsProvider":                  "ElevenLabs",
		"voice":                        "UgBBYS2sOqTuMpoF3BR0-flash_v2_5",
		"ttsLanguage":                  "en-US",
		"interruptible":                "speech",
		"reportInputDuringAgentSpeech": "speech",
		"preemptible":                  "false",
		"dtmfDetection":                "false",
	} {
		if attrs[name] != want {
			t.Errorf("ConversationRelay %s = %q, want %q", name, attrs[name], want)
		}
	}
	if _, set := attrs["speechTimeout"]; set {
		t.Error("speechTimeout is set with no turn entry; omission must emit no override")
	}
	if doc.Connect.Action != "__PUBLIC_ORIGIN__/connect-action" || doc.Connect.Method != "POST" {
		t.Errorf("Connect action = %q %q", doc.Connect.Method, doc.Connect.Action)
	}
	if doc.Hangup == nil {
		t.Error("no Hangup after Connect")
	}
}

// Disabled interruption and a protected greeting map onto the documented
// attribute values.
func TestTwilioInterruptionSettings(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "twilio_relay"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	agent.Conversation.Interruption = &ir.Interruption{Enabled: &enabled, Protect: []ir.InterruptionProtect{ir.ProtectGreeting}}
	out, err := twilioRelayXML(agent, agent.Targets["twilio"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `welcomeGreetingInterruptible="none"`) || !strings.Contains(string(out), `interruptible="speech"`) {
		t.Errorf("a protected greeting did not map to none while speech stays interruptible:\n%s", out)
	}
	disabled := false
	agent.Conversation.Interruption = &ir.Interruption{Enabled: &disabled}
	out, err = twilioRelayXML(agent, agent.Targets["twilio"])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`interruptible="none"`, `reportInputDuringAgentSpeech="none"`, `welcomeGreetingInterruptible="none"`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("disabled interruption lacks %s:\n%s", want, out)
		}
	}
}

// The container runs one process as a non-root user, and neither dotenv files
// nor local state enter the build context.
func TestTwilioContainer(t *testing.T) {
	artifact := twilioArtifact(t, relayDesk, "twilio-openai")
	dockerfile := artifactFile(t, artifact, "Dockerfile")
	for _, want := range []string{"USER relay", "ca-certificates", `CMD ["python", "app.py"]`, "COPY tools/ ./tools/"} {
		if !strings.Contains(dockerfile, want) {
			t.Errorf("Dockerfile lacks %q", want)
		}
	}
	ignore := artifactFile(t, artifact, ".dockerignore")
	for _, want := range []string{".env", ".venv/"} {
		if !strings.Contains(ignore, want) {
			t.Errorf(".dockerignore lacks %q", want)
		}
	}
	if !strings.Contains(artifactFile(t, artifact, "tools/opening_hours.py"), "def opening_hours(") {
		t.Error("the real local handler was not copied")
	}
}

package generate

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/spec"
	"github.com/slng/unmute/internal/target"
)

// The LiveKit artifact has to be deployable on LiveKit Cloud with the commands
// its own README prints. These assertions are the contract in
// specs/001-livekit-cloud-deploy/contracts/artifacts.md, held here rather than
// only in the golden, so a regeneration cannot quietly accept a broken artifact.

func livekitArtifact(t *testing.T, regions []string) Artifact {
	t.Helper()
	agent := loadCompilerAgent(t)
	tgt := targetByProvider(t, agent, ir.ProviderLiveKit)
	tgt.DeploymentRegions = regions
	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

func TestLiveKitEmitsNoDeploymentConfigFile(t *testing.T) { // FR-008
	for _, regions := range [][]string{nil, {"us-east"}, {"us-east", "eu-central"}} {
		for _, file := range livekitArtifact(t, regions).Files {
			if strings.HasPrefix(file.Path, "livekit") && strings.HasSuffix(file.Path, ".toml") {
				t.Errorf("regions %v: emitted %s, which makes `lk agent create` refuse", regions, file.Path)
			}
		}
	}
}

func TestLiveKitReadmeDeploySection(t *testing.T) { // FR-003, FR-004, FR-005, FR-010, FR-019
	one := artifactFile(t, livekitArtifact(t, []string{"us-east"}), "README.md")
	for _, want := range []string{
		"lk agent create --region us-east --secrets-file .env", // first deploy, with the region
		"\nlk agent deploy\n", // redeploy, separately
		"lk agent update-secrets --secrets-file .env",
		"--overwrite",
		"lk agent config --id",
		"lk agent status",
		"lk project set-default",
		"LIVEKIT_API_SECRET` are supplied by the platform",
		"immutable",
		"self-hosted",
	} {
		if !strings.Contains(one, want) {
			t.Errorf("README missing %q", want)
		}
	}
	// The redeploy command takes no region, and the single-region flow must not
	// teach a config file name nobody needs to type.
	if strings.Contains(one, "lk agent deploy --region") {
		t.Error("README puts a region on the redeploy command; region is fixed at create")
	}
	if strings.Contains(one, "livekit.us-east.toml") {
		t.Error("README names a per-region config file for a single region")
	}
	for _, unwanted := range []string{"not the supported path", "unsupported"} {
		if strings.Contains(one, unwanted) {
			t.Errorf("README still disclaims a working path: %q", unwanted)
		}
	}

	// A one-element list is the scalar form, file names included.
	if scalar := artifactFile(t, livekitArtifact(t, []string{"us-east"}), "README.md"); scalar != one {
		t.Error("a one-element region list changed the README")
	}

	several := artifactFile(t, livekitArtifact(t, []string{"us-east", "eu-central"}), "README.md")
	for _, want := range []string{
		"lk agent create --region us-east --config livekit.us-east.toml --secrets-file .env",
		"lk agent create --region eu-central --config livekit.eu-central.toml --secrets-file .env",
		"lk agent deploy --config livekit.us-east.toml",
		"lk agent deploy --config livekit.eu-central.toml",
	} {
		if !strings.Contains(several, want) {
			t.Errorf("multi-region README missing %q", want)
		}
	}

	none := artifactFile(t, livekitArtifact(t, nil), "README.md")
	if strings.Contains(none, "--region") {
		t.Error("README carries a region flag with no region declared")
	}
	if !strings.Contains(none, "the first deploy asks which one to use") {
		t.Error("README does not say the platform prompts when no region is declared")
	}
}

func TestLiveKitContainerRequirements(t *testing.T) { // FR-011
	artifact := livekitArtifact(t, []string{"us-east"})
	dockerfile := artifactFile(t, artifact, "Dockerfile")
	if !strings.Contains(dockerfile, "\nUSER ") {
		t.Error("Dockerfile has no USER switch; LiveKit Cloud requires a non-root run user")
	}
	for _, injected := range []string{"LIVEKIT_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET"} {
		if strings.Contains(dockerfile, "ENV "+injected) || strings.Contains(dockerfile, injected+"=") {
			t.Errorf("Dockerfile sets %s, which the platform injects and the image must not", injected)
		}
	}
	if !strings.Contains(dockerfile, "WORKDIR /app") {
		t.Error("Dockerfile has no explicit WORKDIR")
	}
	if !strings.Contains(dockerfile, `CMD ["python", "agent.py", "start"]`) {
		t.Error("Dockerfile no longer launches the worker's start command directly")
	}
	ignore := artifactFile(t, artifact, ".dockerignore")
	for _, want := range []string{".env\n", ".env.*", ".venv/", "__pycache__/"} {
		if !strings.Contains(ignore, want) {
			t.Errorf(".dockerignore missing %q, so a local run inflates the upload context", want)
		}
	}
}

func TestLiveKitReportCarriesRegions(t *testing.T) { // FR-020
	report := func(t *testing.T, regions []string) map[string]any {
		t.Helper()
		var decoded map[string]any
		if err := json.Unmarshal([]byte(artifactFile(t, livekitArtifact(t, regions), "compile-report.json")), &decoded); err != nil {
			t.Fatal(err)
		}
		return decoded
	}
	several := report(t, []string{"us-east", "eu-central"})
	got, ok := several["deployment_regions"].([]any)
	if !ok || len(got) != 2 || got[0] != "us-east" || got[1] != "eu-central" {
		t.Fatalf("deployment_regions = %v, want both in declared order", several["deployment_regions"])
	}
	if _, present := report(t, nil)["deployment_regions"]; present {
		t.Error("deployment_regions is in the report with no region declared")
	}
}

// A LiveKit package with no webhook tools and no tracing pulls httpx from
// nowhere, and livekit-agents needs it: `inference/llm.py` imports httpx, the
// package declares none, and openai 3.0 replaced its httpx dependency with
// httpx2. Since `livekit.agents.__init__` imports `inference` eagerly, such a
// project cannot import the SDK at all. This is the exact shape of
// examples/human-transfer, which is the package the transfer rig deploys.
func TestLiveKitDeclaresHTTPXWithoutWebhooksOrTracing(t *testing.T) {
	agent := loadCompilerAgent(t)
	tgt := targetByProvider(t, agent, ir.ProviderLiveKit)
	agent.Tracing = nil
	for name, tool := range agent.Tools {
		if tool.Execution == ir.ToolWebhook {
			delete(agent.Tools, name)
			for agentName, def := range agent.Agents {
				def.Tools = slices.DeleteFunc(def.Tools, func(t string) bool { return t == name })
				agent.Agents[agentName] = def
			}
			for taskName, task := range agent.Tasks {
				task.Tools = slices.DeleteFunc(task.Tools, func(t string) bool { return t == name })
				agent.Tasks[taskName] = task
			}
		}
	}
	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatal(err)
	}
	pyproject := artifactFile(t, artifact, "pyproject.toml")
	if strings.Contains(pyproject, "langfuse") {
		t.Fatal("fixture still enables tracing, so httpx could arrive through langfuse")
	}
	if !strings.Contains(pyproject, `"httpx"`) {
		t.Errorf("pyproject omits httpx, so the deployed agent cannot import livekit.agents:\n%s", pyproject)
	}
}

// TransferSIPParticipant's transfer_to becomes the Refer-To of a SIP REFER, so
// it must be a URI. WarmTransferTask's sip_call_to is a number to dial, so it
// must not be. One destination, two positions, two shapes. Verified 2026-08-12
// against the call-forwarding and WarmTransferTask docs.
func TestLiveKitColdTransferSendsAURI(t *testing.T) {
	emit := func(t *testing.T, destination string, warm bool) string {
		t.Helper()
		pkg, err := spec.Load(filepath.Join("..", "..", "examples", "human-transfer"))
		if err != nil {
			t.Fatal(err)
		}
		agent, err := ir.Build(pkg)
		if err != nil {
			t.Fatal(err)
		}
		tgt := targetByProvider(t, agent, ir.ProviderLiveKit)
		for name, control := range agent.Controls {
			human, ok := control.(*ir.HumanTransfer)
			if !ok {
				continue
			}
			if warm != (human.Mode == ir.TransferWarm) {
				continue
			}
			tgt.Destinations[human.Destination] = destination
			_ = name
		}
		artifact, err := Generate(agent, tgt, target.Default())
		if err != nil {
			t.Fatal(err)
		}
		return artifactFile(t, artifact, "agent.py")
	}

	// A bare number gains the scheme at compile time; the helper is not needed.
	if got := emit(t, "+14155550123", false); !strings.Contains(got, `transfer_to="tel:+14155550123"`) {
		t.Error("a literal E.164 destination did not become a tel: URI")
	}
	// An authored URI is left exactly as written: double prefixing breaks it.
	if got := emit(t, "sip:+14155550123@my-trunk.zt.plivo.com", false); !strings.Contains(got, `transfer_to="sip:+14155550123@my-trunk.zt.plivo.com"`) {
		t.Error("an authored sip: destination was rewritten")
	}
	// An env destination is only known on the call, so it goes through the
	// emitted helper, which must also be defined.
	envForm := emit(t, "BILLING_PHONE_NUMBER", false)
	for _, want := range []string{
		`transfer_to=_refer_uri(os.environ["BILLING_PHONE_NUMBER"])`,
		"def _refer_uri(destination: str) -> str:",
		`if destination.startswith(("tel:", "sip:", "sips:")):`,
	} {
		if !strings.Contains(envForm, want) {
			t.Errorf("env destination missing %q", want)
		}
	}
	// The warm path dials a number, so no scheme and no helper on that argument.
	warm := emit(t, "SUPERVISOR_PHONE_NUMBER", true)
	if !strings.Contains(warm, `sip_call_to=os.environ["SUPERVISOR_PHONE_NUMBER"]`) {
		t.Error("warm sip_call_to is not a plain number expression")
	}
	if strings.Contains(warm, `sip_call_to=_refer_uri(`) || strings.Contains(warm, `sip_call_to="tel:`) {
		t.Error("warm sip_call_to carries a URI scheme, which it must not")
	}
}

// REDIS_URL and the LIVEKIT_* trio are not the operator's to supply on LiveKit
// Cloud: the platform injects the trio and drops it from any secrets file, and
// its managed SIP service owns Redis, which no emitted Python reads. Listing
// them beside real keys made a deployed agent look as though it needed a Redis
// it can never use.
func TestLiveKitEnvExampleSeparatesPlatformSuppliedNames(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "human-transfer"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatal(err)
	}
	env := artifactFile(t, artifact, ".env.example")
	label := strings.Index(env, "Supplied for you, not by you")
	if label < 0 {
		t.Fatal(".env.example does not separate platform-supplied names")
	}
	operator, platform := env[:label], env[label:]
	for _, name := range []string{"REDIS_URL", "LIVEKIT_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET"} {
		if !strings.Contains(platform, name+"=") {
			t.Errorf("%s is not in the platform-supplied section", name)
		}
		if strings.Contains(operator, name+"=") {
			t.Errorf("%s is still listed as the operator's to set", name)
		}
	}
	for _, name := range []string{"BILLING_PHONE_NUMBER", "SUPERVISOR_PHONE_NUMBER", "OPENAI_API_KEY", "TWILIO_SIP_ADDRESS"} {
		if !strings.Contains(operator, name+"=") {
			t.Errorf("%s must stay the operator's to set", name)
		}
	}
	// The report keeps the complete list: what the package requires has not
	// changed, only who supplies each name.
	report := artifactFile(t, artifact, "compile-report.json")
	for _, name := range []string{"REDIS_URL", "LIVEKIT_URL", "BILLING_PHONE_NUMBER"} {
		if !strings.Contains(report, name) {
			t.Errorf("compile-report.json lost %s from required_env", name)
		}
	}
}

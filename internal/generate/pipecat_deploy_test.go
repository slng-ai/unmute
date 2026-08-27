package generate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

// The Pipecat artifact has to be deployable with `pipecat cloud deploy` as the
// generated README prints it.

// pipecatArtifact carries one local tool, and that is the point. The fixture
// used to have none, so TestPipecatImageMeetsThePlatformContract asserted the
// spelling of the COPY lines that existed and never noticed the one that was
// missing: the image could not import its own emitted tools (reproduction.md D).
func pipecatArtifact(t *testing.T, regions []string, warm ...int) Artifact {
	t.Helper()
	agent := loadCompilerAgent(t)
	agent.Tools["fetch_notes"] = ir.Tool{
		Description:   "Fetch the caller's saved notes.",
		Input:         map[string]any{"type": "object", "properties": map[string]any{"topic": map[string]any{"type": "string"}}, "required": []any{"topic"}},
		Execution:     ir.ToolLocal,
		Handler:       "tools/fetch_notes.py",
		HandlerSource: "def fetch_notes(topic):\n    return {\"notes\": []}\n",
		Interruption:  ir.ToolProviderDefault,
		Effect:        ir.ToolReturnsData,
	}
	intake := agent.Agents[agent.EntryAgent]
	intake.Tools = append(intake.Tools, "fetch_notes")
	agent.Agents[agent.EntryAgent] = intake

	tgt := targetByProvider(t, agent, ir.ProviderPipecat)
	tgt.DeploymentRegions = regions
	if len(warm) > 0 {
		tgt.WarmInstances = warm[0]
	}
	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

func TestPipecatManifestIsDeployable(t *testing.T) { // FR-012, FR-013, FR-027
	manifest := artifactFile(t, pipecatArtifact(t, []string{"eu-west"}), "pcc-deploy.toml")
	// `image` switches off the cloud build the documented deploy depends on, and
	// the value we used to emit was not a resolvable image URL either.
	if strings.Contains(manifest, "image") {
		t.Errorf("manifest names an image, which switches cloud builds off:\n%s", manifest)
	}
	// A replica count nobody declared and nothing derived, which also bills for
	// a warm instance the platform would not keep by default.
	for _, unwanted := range []string{"min_agents", "[scaling]"} {
		if strings.Contains(manifest, unwanted) {
			t.Errorf("manifest still declares %q:\n%s", unwanted, manifest)
		}
	}
	for _, want := range []string{`agent_name = "pipecat"`, `region = "eu-west"`, `secret_set = "pipecat-secrets"`} {
		if !strings.Contains(manifest, want) {
			t.Errorf("manifest missing %q:\n%s", want, manifest)
		}
	}
	if got := strings.Count(manifest, "region ="); got != 1 {
		t.Errorf("manifest has %d region keys, want exactly one:\n%s", got, manifest)
	}
	if none := artifactFile(t, pipecatArtifact(t, nil), "pcc-deploy.toml"); strings.Contains(none, "region =") {
		t.Errorf("manifest names a region with none declared:\n%s", none)
	}
}

// The image the platform actually starts. Pipecat Cloud begins a session by
// POSTing /bot to the container, and it gates the deployment on readiness probes
// it serves itself; both live in the platform's base image. An image built on a
// plain Python base serves neither, so the deployment never reaches ready and
// every start is refused with PCC-1001 — while the container's own log shows a
// healthy boot and says nothing about why no call ever arrives. A real deploy
// failed exactly that way on 2026-08-13.
//
// Verified by building the emitted image and running it that day: GET /readyz
// answered 200, and POST /bot reached the emitted bot() with the platform's own
// session arguments.
func TestPipecatImageMeetsThePlatformContract(t *testing.T) {
	artifact := pipecatArtifact(t, nil)
	docker := artifactFile(t, artifact, "Dockerfile")
	// Instructions only. The comments in this file name the shapes being avoided,
	// so a check against the whole text would match the warning against the
	// mistake and never see the mistake itself.
	var instructions []string
	for _, line := range strings.Split(docker, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			instructions = append(instructions, line)
		}
	}
	build := strings.Join(instructions, "\n")

	if !strings.Contains(build, "FROM dailyco/pipecat-base:") {
		t.Errorf("the image is not built on the platform's base image, so nothing in it answers /bot:\n%s", docker)
	}
	// The base image owns /app. Copying the build directory over it replaces the
	// very server the platform calls.
	if strings.Contains(build, "COPY . .") {
		t.Error("the Dockerfile copies the build directory over the base image's own /app")
	}
	if !strings.Contains(build, "COPY bot.py ./") {
		t.Error("the Dockerfile does not copy bot.py, which is the one file the base image looks for")
	}
	// The invariant, not the spelling: every module the entrypoint imports has to
	// be reachable inside the image. Asserting one COPY line's wording is what let
	// `import tools.<name>` ship against an image with no tools/ directory, and
	// `compose.dev.yaml` has no bind mount, so its optional container run uses the same image.
	assertImportsAreCopied(t, artifact, build)
	// A CMD replaces the base image's server with something the platform cannot
	// call.
	if strings.Contains(build, "CMD ") {
		t.Errorf("the Dockerfile overrides the base image's own start command:\n%s", docker)
	}
	// Dependencies, not the project: installing this as a distribution would put
	// one named after the project beside the real pipecat package it imports.
	for _, forbidden := range []string{"install --system .", "pip install ."} {
		if strings.Contains(build, forbidden) {
			t.Errorf("the Dockerfile runs %q, installing the project instead of its dependencies", forbidden)
		}
	}
}

// assertImportsAreCopied reads the entrypoint's own top-level imports and
// requires every one that names an emitted file to be reachable in the image.
// It reads the Dockerfile rather than a list of names, so a new emitted module
// is covered the day it is emitted.
func assertImportsAreCopied(t *testing.T, artifact Artifact, build string) {
	t.Helper()
	emitted := make(map[string]string, len(artifact.Files))
	for _, file := range artifact.Files {
		emitted[file.Path] = string(file.Content)
	}
	for _, line := range strings.Split(emitted["bot.py"], "\n") {
		module, ok := strings.CutPrefix(strings.TrimSpace(line), "import ")
		if !ok {
			continue
		}
		module, _, _ = strings.Cut(module, " ") // `import x as y`
		path := strings.ReplaceAll(module, ".", "/") + ".py"
		if _, self := emitted[path]; !self {
			continue // a dependency from the image's own site-packages
		}
		// A package directory is copied whole; a single module by name.
		dir, _, nested := strings.Cut(module, ".")
		copied := strings.Contains(build, "COPY "+path) ||
			nested && (strings.Contains(build, "COPY "+dir+"/ ") || strings.Contains(build, "COPY "+dir+" "))
		if !copied {
			t.Errorf("bot.py runs %q and the image never receives %s, so the container cannot start:\n%s", strings.TrimSpace(line), path, build)
		}
	}
}

// A declared warm pool reaches the manifest, so it survives every later deploy.
// The flag it replaces applies to one deploy, and this file is rewritten by every
// compile, which is why a hand-added block was never an answer. The zero case is
// TestPipecatManifestIsDeployable above: nothing declared, nothing emitted, no
// standing bill.
func TestPipecatWarmInstancesReachTheManifest(t *testing.T) {
	manifest := artifactFile(t, pipecatArtifact(t, []string{"eu-west"}, 2), "pcc-deploy.toml")
	for _, want := range []string{"[scaling]", "min_agents = 2"} {
		if !strings.Contains(manifest, want) {
			t.Errorf("manifest missing %q, so the declared warm pool reaches nothing:\n%s", want, manifest)
		}
	}
	// In TOML every key after a table header belongs to that table, so a
	// top-level key emitted below `[scaling]` silently becomes `scaling.<key>`
	// and the platform never reads it. This caught exactly that: the block was
	// first written above `websocket_auth`.
	table := strings.Index(manifest, "\n[")
	for _, key := range []string{"agent_name", "region", "secret_set", "websocket_auth"} {
		at := strings.Index(manifest, "\n"+key+" =")
		if at < 0 && !strings.HasPrefix(manifest, key+" =") {
			continue // not emitted for this package
		}
		if table >= 0 && at > table {
			t.Errorf("%s is emitted after a table header, so TOML reads it as a key of that table:\n%s", key, manifest)
		}
	}

	// The runbook has to stop telling the operator to remember the flag once the
	// manifest carries the number, or the two disagree on the same page.
	readme := artifactFile(t, pipecatArtifact(t, []string{"eu-west"}, 2), "README.md")
	if strings.Contains(readme, "--min-agents") {
		t.Errorf("README still sends the operator to `--min-agents` while the manifest declares the pool:\n%s", readme)
	}
	if !strings.Contains(readme, "holds **2** instances ready") {
		t.Error("README does not name the warm pool this deployment holds")
	}

	// And with none declared it has to name the durable fix, not only the flag.
	none := artifactFile(t, pipecatArtifact(t, []string{"eu-west"}), "README.md")
	if !strings.Contains(none, "`warm_instances: 1`") {
		t.Error("README does not tell an operator with no warm pool which field to declare")
	}
}

func TestPipecatReadmeDeploySection(t *testing.T) { // FR-003, FR-004, FR-014, FR-018
	withRegion := artifactFile(t, pipecatArtifact(t, []string{"eu-west"}), "README.md")
	for _, want := range []string{
		"## Deploy to Pipecat Cloud",
		"pipecat cloud secrets set pipecat-secrets --file .env --region eu-west",
		"pipecat cloud deploy",
		"pipecat cloud agent status pipecat",
		"pipecat cloud regions list",
		"updates the existing agent",
		"--secrets <other-name>",
		"--min-agents",
		"globally unique",
	} {
		if !strings.Contains(withRegion, want) {
			t.Errorf("README missing %q", want)
		}
	}
	// The set has to exist before the deploy that references it.
	if secrets, deploy := strings.Index(withRegion, "secrets set"), strings.Index(withRegion, "pipecat cloud deploy\n"); secrets < 0 || deploy < 0 || secrets > deploy {
		t.Error("README does not order the secret set before the deploy")
	}

	noRegion := artifactFile(t, pipecatArtifact(t, nil), "README.md")
	for _, want := range []string{"organisation's default region", "`us-west`"} {
		if !strings.Contains(noRegion, want) {
			t.Errorf("README does not say what an undeclared region does: missing %q", want)
		}
	}
	if strings.Contains(noRegion, "--region eu-west") {
		t.Error("README carries a region with none declared")
	}
}

// The `pcc` CLI form is retired; nothing the compiler emits may still use it.
func TestPipecatArtifactUsesCurrentCLIName(t *testing.T) { // FR-007
	for _, file := range pipecatArtifact(t, []string{"eu-west"}).Files {
		for _, retired := range []string{"pcc deploy", "pcc secrets", "pcc auth", "pcc agent"} {
			if strings.Contains(string(file.Content), retired) {
				t.Errorf("%s uses the retired CLI form %q", file.Path, retired)
			}
		}
	}
}

func TestPipecatReportCarriesRegion(t *testing.T) { // FR-020
	var withRegion map[string]any
	if err := json.Unmarshal([]byte(artifactFile(t, pipecatArtifact(t, []string{"eu-west"}), "compile-report.json")), &withRegion); err != nil {
		t.Fatal(err)
	}
	got, ok := withRegion["deployment_regions"].([]any)
	if !ok || len(got) != 1 || got[0] != "eu-west" {
		t.Fatalf("deployment_regions = %v, want a one-element list", withRegion["deployment_regions"])
	}
	var none map[string]any
	if err := json.Unmarshal([]byte(artifactFile(t, pipecatArtifact(t, nil), "compile-report.json")), &none); err != nil {
		t.Fatal(err)
	}
	if _, present := none["deployment_regions"]; present {
		t.Error("deployment_regions is in the report with no region declared")
	}
}

// No emitted file carries a secret value on either driver: the artifact holds
// UPPER_SNAKE names, and the operator's own .env holds the values.
func TestNeitherDriverEmitsSecretValues(t *testing.T) { // FR-002, SC-007
	agent := loadCompilerAgent(t)
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		tgt := targetByProvider(t, agent, provider)
		artifact, err := Generate(agent, tgt, target.Default())
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range artifact.Files {
			if file.Path != ".env.example" {
				continue
			}
			for _, line := range strings.Split(string(file.Content), "\n") {
				if name, value, found := strings.Cut(line, "="); found && strings.TrimSpace(value) != "" {
					t.Errorf("%s: %s carries a value in .env.example", provider, name)
				}
			}
		}
	}
}

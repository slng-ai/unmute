package generate

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// The gates the SLNG Context Router's emitted shape needs. Two of them hold the
// rules with the worst silent cost: a split agent id splits one package's cache
// into that many namespaces, and a split session id ungroups one call's requests.
// Neither fails anything at run time. The agent is just never fast, or its
// support trail is unreadable, so only a check on what a driver actually wrote
// can catch a later refactor reintroducing composition.

var updateSlngRouter = flag.Bool("update-slng", false, "rewrite the SLNG Context Router goldens")

const (
	routerAgentID = "safe-core-router-v3"
	routerModel   = "gpt-5.6-luna"
)

// routerFixture is safe_core with both agents on one router think profile, two
// tasks, a task group, and a variable its instructions reference. That shape is
// the point: several agents, several tasks and a group are exactly what a
// composed id would split.
func routerFixture(t *testing.T) *ir.Agent {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	pkg.Agent.Secrets = append(pkg.Agent.Secrets, "SLNG_API_KEY")
	pkg.Agent.Models.Think["fast_reasoning"] = spec.ModelDef{
		Provider: "slng", Model: routerModel, AgentID: routerAgentID,
		Upstream: &spec.Upstream{Provider: "openai"},
		Params:   map[string]any{"world_part_override": "eu", "reasoning_effort": "none"},
	}
	// Both agents on the one router profile, repointed before Build so the
	// resolved bindings hold only the profile the package actually uses: on
	// livekit every router profile has to be the entry agent's, because that is
	// the only place the call's session id and state are in scope.
	billing := pkg.Agent.Agents["billing"]
	billing.Model = "fast_reasoning"
	pkg.Agent.Agents["billing"] = billing
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	// The placeholder goes on after Build, because the prompt bodies come from
	// files. It is what makes this fixture exercise the raw-prompt seam too.
	for _, name := range []string{"intake", "billing"} {
		def := agent.Agents[name]
		def.Instructions += "\n\nThe caller is {{customer_id}}."
		agent.Agents[name] = def
	}
	agent.Tasks["collect"] = ir.Task{
		Instructions: "Ask for the caller's email and confirm the account for {{customer_id}}.",
		Tools:        []string{"lookup_customer"},
		Result:       map[string]ir.ResultField{"tier": {Type: ir.PrimitiveString}},
		Context:      ir.TaskContext{History: ir.HistoryFull},
	}
	agent.Tasks["confirm"] = ir.Task{
		Instructions: "Read the booking back and ask the caller to confirm.",
		Result:       map[string]ir.ResultField{"confirmed": {Type: ir.PrimitiveBoolean}},
		Context:      ir.TaskContext{History: ir.HistoryFull},
	}
	agent.TaskGroups["triage"] = ir.TaskGroup{
		Steps: []string{"collect", "confirm"}, ContextScope: ir.ContextIsolated,
		Then: ir.GroupReturn, Merge: ir.GroupMergeResults,
	}
	agent.Controls["run_collect"] = &ir.Delegate{
		Kind: ir.ControlDelegate, Task: "collect", When: "Collect the caller's account details.",
	}
	agent.Controls["run_triage"] = &ir.Delegate{
		Kind: ir.ControlDelegate, Group: "triage", When: "Run the triage group.",
	}
	intake := agent.Agents["intake"]
	intake.Tools = append(intake.Tools, "run_collect", "run_triage")
	agent.Agents["intake"] = intake
	return agent
}

// emitAgentSource generates one target and returns its agent module, plus every
// emitted file joined, because "in the whole emitted project" is the claim.
func emitAgentSource(t *testing.T, agent *ir.Agent, provider ir.Provider, module string) (string, string) {
	t.Helper()
	artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
	if err != nil {
		t.Fatalf("%s: generate: %v", provider, err)
	}
	var source string
	var all strings.Builder
	for _, file := range artifact.Files {
		all.Write(file.Content)
		if file.Path == module {
			source = string(file.Content)
		}
	}
	if source == "" {
		t.Fatalf("%s: %s not emitted", provider, module)
	}
	return source, all.String()
}

func routerTargets() []struct {
	provider ir.Provider
	module   string
} {
	return []struct {
		provider ir.Provider
		module   string
	}{
		{ir.ProviderPipecat, "bot.py"},
		{ir.ProviderLiveKit, "agent.py"},
	}
}

// FR-010 and FR-029. Counting distinct values in the output is the right shape
// for this gate: validation sees the binding, this sees what a driver wrote, and
// a refactor that starts composing the id per agent or per task fails here no
// matter which driver does it.
func TestSlngRouterEmitsExactlyOneAgentID(t *testing.T) {
	agent := routerFixture(t)
	header := regexp.MustCompile(`"X-Slng-Agent-Id": "([^"]*)"`)
	for _, tc := range routerTargets() {
		_, all := emitAgentSource(t, agent, tc.provider, tc.module)
		found := map[string]bool{}
		for _, match := range header.FindAllStringSubmatch(all, -1) {
			found[match[1]] = true
		}
		if len(found) != 1 || !found[routerAgentID] {
			t.Errorf("%s: emitted agent ids %v, want exactly {%q}", tc.provider, found, routerAgentID)
		}
		// The composed forms a previous attempt shipped. Any of them is a split
		// cache, so they are named rather than left to the count alone.
		for _, name := range []string{"intake", "billing", "collect", "confirm", "triage"} {
			if strings.Contains(all, routerAgentID+"--"+name) || strings.Contains(all, routerAgentID+"-"+name) {
				t.Errorf("%s: the agent id is composed with %q; it is authored and carried verbatim", tc.provider, name)
			}
		}
	}
}

// FR-012, the twin of the rule above and the same class of silent defect. One
// expression, reached by argument at every construction site, and nothing
// process-wide holding it: a worker serving several calls at once has to keep
// them apart.
func TestSlngRouterEmitsExactlyOneSessionID(t *testing.T) {
	agent := routerFixture(t)
	for _, tc := range routerTargets() {
		source, all := emitAgentSource(t, agent, tc.provider, tc.module)
		expr := regexp.MustCompile(`"X-Slng-Session-Id": ([^,}]*)`)
		found := map[string]bool{}
		for _, match := range expr.FindAllStringSubmatch(all, -1) {
			found[strings.TrimSpace(match[1])] = true
		}
		if len(found) != 1 || !found["slng_session_id"] {
			t.Errorf("%s: session id expressions %v, want exactly {slng_session_id}", tc.provider, found)
		}
		// Created once, where the call begins.
		if got := strings.Count(source, "slng_session_id = str(uuid.uuid4())"); got != routerSessionSites(tc.provider) {
			t.Errorf("%s: creates the session id %d times, want %d", tc.provider, got, routerSessionSites(tc.provider))
		}
		// Never through anything the process shares between calls.
		for _, banned := range []string{"ContextVar", "global slng_session_id", "_SLNG_SESSION_ID ="} {
			if strings.Contains(source, banned) {
				t.Errorf("%s: the session id travels through %q rather than an argument", tc.provider, banned)
			}
		}
		// The name is taken twice over on these two templates, so a collision is
		// worth a check of its own rather than a comment.
		if strings.Contains(source, `"X-Slng-Session-Id": session_id`) {
			t.Errorf("%s: the header reads the wrong session_id; the router's is slng_session_id", tc.provider)
		}
	}
}

// routerSessionSites is how many run-once creation points a target has. Pipecat
// writes one run_bot per shape and only one is emitted; livekit has one
// entrypoint.
func routerSessionSites(ir.Provider) int { return 1 }

// FR-034b. Every credential the request body carries joins the startup check, so
// a missing value fails at boot rather than on the first turn of a live call.
// Bedrock is the widest row: three credentials plus a model id of its own.
func TestSlngRouterCredentialsReachRequiredEnv(t *testing.T) {
	for _, tc := range []struct {
		provider string
		upstream *spec.Upstream
		secrets  []string
		wants    []string
	}{
		{
			provider: "openai", upstream: &spec.Upstream{Provider: "openai"},
			wants: []string{"OPENAI_API_KEY", "SLNG_API_KEY"},
		},
		{
			provider: "bedrock",
			upstream: &spec.Upstream{
				Provider: "bedrock", AccessKeyIDEnv: "AWS_ACCESS_KEY_ID",
				SecretAccessKeyEnv: "AWS_SECRET_ACCESS_KEY", SessionTokenEnv: "AWS_SESSION_TOKEN",
				Region: "eu-central-1", ModelID: "anthropic.claude-3-5-sonnet-20241022-v2:0",
			},
			secrets: []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"},
			wants: []string{
				"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "SLNG_API_KEY",
			},
		},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			agent := routerFixtureWithUpstream(t, tc.upstream, tc.secrets)
			for _, target := range routerTargets() {
				source, _ := emitAgentSource(t, agent, target.provider, target.module)
				block := requiredEnvBlock(t, source)
				for _, want := range tc.wants {
					if !strings.Contains(block, fmt.Sprintf("%q", want)) {
						t.Errorf("%s: REQUIRED_ENV is missing %s:\n%s", target.provider, want, block)
					}
				}
				// Named, never written: the emitted code reads each value at run
				// time and no generated file holds one (FR-036).
				for _, want := range tc.wants {
					if !strings.Contains(source, fmt.Sprintf("os.environ[%q]", want)) && want != "SLNG_API_KEY" {
						t.Errorf("%s: %s is not read from the environment", target.provider, want)
					}
				}
			}
		})
	}
}

func routerFixtureWithUpstream(t *testing.T, upstream *spec.Upstream, secrets []string) *ir.Agent {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	pkg.Agent.Secrets = append(pkg.Agent.Secrets, "SLNG_API_KEY")
	pkg.Agent.Secrets = append(pkg.Agent.Secrets, secrets...)
	router := spec.ModelDef{
		Provider: "slng", Model: routerModel, AgentID: routerAgentID, Upstream: upstream,
		Params: map[string]any{"world_part_override": "eu", "reasoning_effort": "none"},
	}
	pkg.Agent.Models.Think["fast_reasoning"] = router
	// One router profile, the entry agent's, which is what livekit allows. The
	// second profile stays where it is and simply goes unused.
	billing := pkg.Agent.Agents["billing"]
	billing.Model = "fast_reasoning"
	pkg.Agent.Agents["billing"] = billing
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

// FR-035, one case per upstream, asserted key by key. The emitted configuration
// is the smallest shape that works: one tier, one entry, weight 100, and an
// endpoint holding exactly the resolved provider's fields. `provider` stays out
// when it resolves to the router's own default, `model_id` stays out unless the
// upstream requires it, and `cache_enabled` is never sent at all.
func TestSlngRouterUpstreamEndpointObject(t *testing.T) {
	for _, tc := range []struct {
		name     string
		upstream *ir.Upstream
		want     string
	}{
		{
			name: "openai supplies both defaults", upstream: &ir.Upstream{Provider: "openai"},
			want: `{"tiers": {"1": [{"endpoint": {"api_key": os.environ["OPENAI_API_KEY"], "url": "https://api.openai.com/v1"}, "model": "gpt-5.6-luna", "weight": 100}]}}`,
		},
		{
			name: "openai-compat sends no provider key",
			upstream: &ir.Upstream{
				Provider: "openai-compat", URL: "https://host/v1", KeyEnv: "HOST_KEY", AuthHeader: "x-goog-api-key",
			},
			want: `{"tiers": {"1": [{"endpoint": {"api_key": os.environ["HOST_KEY"], "auth_header": "x-goog-api-key", "url": "https://host/v1"}, "model": "gpt-5.6-luna", "weight": 100}]}}`,
		},
		{
			name: "azure names itself and its deployment",
			upstream: &ir.Upstream{
				Provider: "azure", URL: "https://r.cognitiveservices.azure.com/", KeyEnv: "AZURE_OPENAI_API_KEY",
				Deployment: "gpt-4o-deploy", APIVersion: "2024-12-01-preview",
			},
			want: `{"tiers": {"1": [{"endpoint": {"api_key": os.environ["AZURE_OPENAI_API_KEY"], "api_version": "2024-12-01-preview", "azure_deployment": "gpt-4o-deploy", "provider": "azure", "url": "https://r.cognitiveservices.azure.com/"}, "model": "gpt-5.6-luna", "weight": 100}]}}`,
		},
		{
			name: "vertex resolves its key through the helper",
			upstream: &ir.Upstream{
				Provider: "vertex", CredentialsEnv: "GCP_SERVICE_ACCOUNT", Location: "europe-west4",
			},
			want: `{"tiers": {"1": [{"endpoint": {"provider": "vertex", "vertex_credentials": _slng_vertex_credentials("GCP_SERVICE_ACCOUNT"), "vertex_location": "europe-west4"}, "model": "gpt-5.6-luna", "weight": 100}]}}`,
		},
		{
			name: "bedrock is the only row carrying model_id",
			upstream: &ir.Upstream{
				Provider: "bedrock", AccessKeyIDEnv: "AWS_ACCESS_KEY_ID",
				SecretAccessKeyEnv: "AWS_SECRET_ACCESS_KEY", SessionTokenEnv: "AWS_SESSION_TOKEN",
				Region: "eu-central-1", ModelID: "anthropic.claude-3-5-sonnet-20241022-v2:0",
			},
			want: `{"tiers": {"1": [{"endpoint": {"aws_access_key_id": os.environ["AWS_ACCESS_KEY_ID"], "aws_region": "eu-central-1", "aws_secret_access_key": os.environ["AWS_SECRET_ACCESS_KEY"], "aws_session_token": os.environ["AWS_SESSION_TOKEN"], "model_id": "anthropic.claude-3-5-sonnet-20241022-v2:0", "provider": "bedrock"}, "model": "gpt-5.6-luna", "weight": 100}]}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := slngConfigBody(ir.Binding{
				Provider: ir.ProviderSlngRouter, Model: routerModel, AgentID: routerAgentID,
				Upstream: tc.upstream,
			})
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("endpoint object drifted\n got: %s\nwant: %s", got, tc.want)
			}
			if strings.Contains(got, "cache_enabled") {
				t.Error("cache_enabled is never sent: it was measured to change nothing")
			}
			// No credential value is ever written, only named.
			for _, value := range []string{"sk-", "AKIA", "-----BEGIN"} {
				if strings.Contains(got, value) {
					t.Errorf("the configuration holds something that looks like a secret value: %q", value)
				}
			}
		})
	}
}

// FR-015 and FR-017. Only the prompt ships raw. Everything else a variable
// reaches keeps rendering locally, which this asserts on one package where a
// greeting, a tool argument and the instructions all name the same variable.
func TestSlngRouterShipsOnlyThePromptRaw(t *testing.T) {
	agent := routerFixture(t)
	agent.Conversation.Greeting.Text = "Hello {{customer_id}}, thanks for calling."
	lookup := agent.Tools["lookup_customer"]
	lookup.Path = "/customers/{{customer_id}}"
	agent.Tools["lookup_customer"] = lookup

	for _, tc := range routerTargets() {
		source, _ := emitAgentSource(t, agent, tc.provider, tc.module)
		// The prompt constant keeps its placeholder and is never wrapped in a
		// local render, because the router substitutes it.
		if !strings.Contains(source, "The caller is {{customer_id}}.") {
			t.Errorf("%s: the router prompt lost its placeholder", tc.provider)
		}
		for _, rendered := range []string{"_render(INTAKE_PROMPT", "_render(BILLING_PROMPT"} {
			if strings.Contains(source, rendered) {
				t.Errorf("%s: the router prompt is rendered locally by %s, so the router sees a different prompt every call", tc.provider, rendered)
			}
		}
		// The greeting and the tool path still render locally, unchanged by this
		// feature: they have their own lowering helpers and one consumer each.
		if !strings.Contains(source, "_render(") {
			t.Errorf("%s: nothing renders locally, so the greeting and the tool path lost their values", tc.provider)
		}
		if !strings.Contains(source, "customer_id") {
			t.Errorf("%s: the variable name reaches nothing", tc.provider)
		}
		// FR-016: the snapshot carries the name the prompt references.
		if !strings.Contains(source, `_slng_template_variables(`) || !strings.Contains(source, `"customer_id"`) {
			t.Errorf("%s: the request carries no template_variables for a prompt that references one", tc.provider)
		}
	}
}

// FR-019. A package with no router think binding emits none of it: not a header,
// not a body extension, not a helper. Held on safe_core itself, which is the
// package every other golden is built from.
func TestSlngRouterEmitsNothingWithoutABinding(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range routerTargets() {
		_, all := emitAgentSource(t, agent, tc.provider, tc.module)
		for _, banned := range []string{
			"X-Slng-Agent-Id", "X-Slng-Session-Id", "slng_config", "template_variables",
			"_slng_template_variables", "_slng_config_", "_slng_vertex_credentials",
			"slng_session_id", "_SLNG_VARIABLE_LIMIT", "context-router.slng.ai",
		} {
			if strings.Contains(all, banned) {
				t.Errorf("%s: a package with no router binding emits %q", tc.provider, banned)
			}
		}
	}
}

// The base URL comes from the region and nowhere else, and the request never
// takes the Responses path (FR-013).
func TestSlngRouterUsesTheRegionalChatCompletionsEndpoint(t *testing.T) {
	agent := routerFixture(t)
	for _, tc := range routerTargets() {
		source, _ := emitAgentSource(t, agent, tc.provider, tc.module)
		if !strings.Contains(source, `base_url="https://eu.context-router.slng.ai/v1"`) {
			t.Errorf("%s: the emitted client does not point at the regional router", tc.provider)
		}
		if !strings.Contains(source, `api_key=os.environ["SLNG_API_KEY"]`) {
			t.Errorf("%s: the router key is not read from the environment", tc.provider)
		}
		for _, banned := range []string{"openai.responses", "inference.LLM", "world_part_override"} {
			if strings.Contains(source, banned) {
				t.Errorf("%s: emitted %q, which a router binding never takes", tc.provider, banned)
			}
		}
	}
}

// The two goldens quickstart level 1 asks a reader to open. They pin the whole
// emitted think wiring for both targets: the base URL, the key, the model, both
// headers, the inline configuration and the variable snapshot. They are also the
// only place the two run-time-only helpers are visible to `go test`, because what
// those helpers *do* runs in `make smoke` (FR-018, FR-034f).
func TestSlngRouterGolden(t *testing.T) {
	agent := routerFixture(t)
	for _, tc := range []struct {
		provider ir.Provider
		module   string
		golden   string
	}{
		{ir.ProviderPipecat, "bot.py", "slng_pipecat.py"},
		{ir.ProviderLiveKit, "agent.py", "slng_livekit.py"},
	} {
		source, _ := emitAgentSource(t, agent, tc.provider, tc.module)
		path := filepath.Join("testdata", "golden", tc.golden)
		if *updateSlngRouter {
			if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if source != string(want) {
			t.Errorf("%s golden differs; run: go test ./internal/generate -run TestSlngRouterGolden -update-slng", tc.golden)
		}
	}
}

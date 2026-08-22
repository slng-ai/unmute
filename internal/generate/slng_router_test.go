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

// routerScopes is every cache scope the fixture must produce: one per agent, one
// per task. The group's two steps are tasks like any other, and they are what a
// per-agent-only fix would have missed.
func routerScopes() []string {
	return []string{
		routerAgentID + ":intake",
		routerAgentID + ":billing",
		routerAgentID + ":task.collect",
		routerAgentID + ":task.confirm",
	}
}

// FR-040 and FR-041, and the reverse of the rule this gate used to hold. It used
// to require exactly one agent id in the output and to name composed forms as
// failures; one value across every prompt site is the defect, measured on live
// calls, because the router's cache key carries no system prompt and the scope is
// the id. So the shape of the check is the same and the expectation is inverted:
// validation sees the binding, this sees what a driver wrote, and a refactor that
// collapses the sites back onto one scope fails here whichever driver does it.
func TestSlngRouterEmitsOneScopePerPromptSite(t *testing.T) {
	agent := routerFixture(t)
	header := regexp.MustCompile(`"X-Slng-Agent-Id": "([^"]*)"`)
	for _, tc := range routerTargets() {
		_, all := emitAgentSource(t, agent, tc.provider, tc.module)
		found := map[string]bool{}
		for _, match := range header.FindAllStringSubmatch(all, -1) {
			found[match[1]] = true
		}
		// On livekit the header dict is built once in the mixin from a per-class
		// constant, so the scopes are read off those constants instead.
		scope := regexp.MustCompile(`_slng_scope = "([^"]*)"`)
		for _, match := range scope.FindAllStringSubmatch(all, -1) {
			found[match[1]] = true
		}
		// Every emitted scope carries the authored prefix, so a log reader can
		// tell whose package it belongs to and a typo cannot silently orphan one.
		//
		// Read off the emitted values, before the loop below consumes them. An
		// earlier version of this ran over routerScopes(), which is this file's
		// own constant, so it compared a constant with itself and could never
		// fail whatever a driver emitted.
		for emitted := range found {
			if emitted == "" {
				continue // reported by name below, where the reason is specific
			}
			if !strings.HasPrefix(emitted, routerAgentID+target.SlngScopeSeparator) {
				t.Errorf("%s: emitted scope %q does not start with the authored id %q and the separator", tc.provider, emitted, routerAgentID)
			}
		}
		for _, want := range routerScopes() {
			if !found[want] {
				t.Errorf("%s: no site sends scope %q; found %v", tc.provider, want, found)
			}
			delete(found, want)
		}
		// The bare authored id is the defect itself, and an empty value is the
		// near miss a count alone would pass: the router rejects it on the first
		// turn of a live call.
		for leftover := range found {
			switch leftover {
			case routerAgentID:
				t.Errorf("%s: a site still sends the bare authored id %q, so two prompts share one cache", tc.provider, routerAgentID)
			case "":
				t.Errorf("%s: a site sends an empty scope, which the router refuses on the first turn", tc.provider)
			default:
				t.Errorf("%s: unexpected scope %q; the expected set is %v", tc.provider, leftover, routerScopes())
			}
		}
	}
}

// FR-040c. One package, one behaviour: a site compiled for either target reaches
// the router under the same scope, so a reader comparing two deployments of the
// same package is comparing like with like.
func TestSlngRouterScopesAgreeAcrossTargets(t *testing.T) {
	agent := routerFixture(t)
	perTarget := map[ir.Provider]map[string]bool{}
	header := regexp.MustCompile(`"X-Slng-Agent-Id": "([^"]*)"|_slng_scope = "([^"]*)"`)
	for _, tc := range routerTargets() {
		_, all := emitAgentSource(t, agent, tc.provider, tc.module)
		found := map[string]bool{}
		for _, match := range header.FindAllStringSubmatch(all, -1) {
			found[match[1]+match[2]] = true
		}
		perTarget[tc.provider] = found
	}
	for want := range perTarget[ir.ProviderPipecat] {
		if !perTarget[ir.ProviderLiveKit][want] {
			t.Errorf("pipecat sends scope %q and livekit does not", want)
		}
	}
	for want := range perTarget[ir.ProviderLiveKit] {
		if !perTarget[ir.ProviderPipecat][want] {
			t.Errorf("livekit sends scope %q and pipecat does not", want)
		}
	}
}

// The one thing a scope count cannot see. The plugin replaces the whole
// extra_headers entry from the constructor value rather than merging it
// (livekit-plugins-openai llm.py:960-962), so a router model still built with
// extra_headers would silently win over the per-request scope while the emitted
// source looked right. The summarizer is the exception and has none in this
// fixture, which uses full history.
func TestSlngRouterLiveKitPassesNoConstructorHeaders(t *testing.T) {
	agent := routerFixture(t)
	source, _ := emitAgentSource(t, agent, ir.ProviderLiveKit, "agent.py")
	if strings.Contains(source, "extra_headers={") {
		t.Errorf("livekit builds a router model with extra_headers at construction, which overwrites the per-request scope:\n%s", source)
	}
}

// FR-040d, the same defect pointing the other way. Pipecat carries the scope in
// persistent settings, so a task that took the service's scope has to give it
// back on every way out, or the owner answers as a task that already ended.
func TestSlngRouterPipecatRestoresTheOwnerScope(t *testing.T) {
	agent := routerFixture(t)
	source, _ := emitAgentSource(t, agent, ir.ProviderPipecat, "bot.py")
	owner := `"X-Slng-Agent-Id": "` + routerAgentID + `:intake"`
	// Entering the group's first step, then each step in turn, then back to the
	// owner: four swaps at least, and the owner's own construction on top.
	if got := strings.Count(source, owner); got < 2 {
		t.Errorf("the owner's scope appears %d times, so at least one exit from a task never restores it", got)
	}
	// The task-entry update has to be ahead of the frame that starts the step's
	// first completion, which is the request that collided.
	entry := strings.Index(source, `"X-Slng-Agent-Id": "`+routerAgentID+`:task.collect"`)
	initialize := strings.Index(source, "await flow.initialize(")
	if entry < 0 || initialize < 0 || entry > initialize {
		t.Errorf("the task's scope is not queued before the flow is initialised (scope at %d, initialize at %d)", entry, initialize)
	}
}

// FR-042. A fallback model on the same site is the same prompt asking, so it asks
// under the same scope: a failover must not move a site into a cache of its own,
// where its warmth would never build.
//
// This checks the structure rather than a fixture with a chain in it, for two
// reasons. A router fallback chain cannot be built today at all: validation holds
// every router profile in a livekit package to being the entry agent's, and
// Pipecat emits no generated fallback, so a test with one in it fails at
// validation rather than proving anything. And the structure is the stronger
// claim anyway. On livekit the scope lives on the asking class and the header dict
// is built in exactly one place, so no member of a chain can carry a different
// one; FallbackAdapter.chat forwards extra_kwargs to whichever inner service it
// picks (livekit-agents llm/fallback_adapter.py:152-192).
func TestSlngRouterFallbackCannotSplitASiteScope(t *testing.T) {
	agent := routerFixture(t)
	source, _ := emitAgentSource(t, agent, ir.ProviderLiveKit, "agent.py")
	if got := strings.Count(source, target.SlngAgentIDHeader); got != 1 {
		t.Errorf("the agent id header is written in %d places, want 1: with more than one, a chain member can carry a scope of its own", got)
	}
	if !strings.Contains(source, target.SlngAgentIDHeader+`": agent._slng_scope`) {
		t.Errorf("the one header site does not read the asking class's own scope:\n%s", source)
	}
}

// FR-040b. A task carries its own scope without its own model object, so adding
// one to a package does not add a service, a client or a connection pool.
func TestSlngRouterTaskAddsNoModelObject(t *testing.T) {
	withTask := routerFixture(t)
	withoutTask := routerFixture(t)
	delete(withoutTask.Tasks, "confirm")
	withoutTask.TaskGroups["triage"] = ir.TaskGroup{
		Steps: []string{"collect"}, ContextScope: ir.ContextIsolated,
		Then: ir.GroupReturn, Merge: ir.GroupMergeResults,
	}
	for _, tc := range routerTargets() {
		with, _ := emitAgentSource(t, withTask, tc.provider, tc.module)
		without, _ := emitAgentSource(t, withoutTask, tc.provider, tc.module)
		builder := "OpenAILLMService("
		if tc.provider == ir.ProviderLiveKit {
			builder = "openai.LLM("
		}
		if got, want := strings.Count(with, builder), strings.Count(without, builder); got != want {
			t.Errorf("%s: a task adds %d model objects (%d with, %d without); a scope is a header value, not a service", tc.provider, got-want, got, want)
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
		// The guarantees do not change and neither do the bans below: one
		// expression per target, created once where the call begins, never
		// through anything the process shares between calls. Only the spelling
		// changes, and only on the targets where it had to.
		//
		// On pipecat the value is still a builder argument, and the worker keeps
		// it on self so a task's header swap can carry it. On livekit it moved
		// onto the session's user data object, because the header set now travels
		// per request and every agent and task class has to reach it from a
		// method body.
		// A target may spell the one value more than once, the way a prompt is
		// already spelled twice, once for a constructor and once for a method
		// body. What is checked is that every spelling is a per-call read and
		// that the set is closed: an expression not on the list is a new route
		// to the value that nobody has thought about.
		wantExprs, wantCreate := []string{"slng_session_id", "self._slng_session_id"}, "slng_session_id = str(uuid.uuid4())"
		if tc.provider == ir.ProviderLiveKit {
			wantExprs = []string{"session.userdata.slng_session_id"}
			wantCreate = "Userdata(slng_session_id=str(uuid.uuid4()))"
		}
		for _, want := range wantExprs {
			delete(found, want)
		}
		for leftover := range found {
			t.Errorf("%s: session id expression %q is not one of %v", tc.provider, leftover, wantExprs)
		}
		// Created once, where the call begins.
		if got := strings.Count(source, wantCreate); got != routerSessionSites(tc.provider) {
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
			// The per-request scope machinery. A new symbol not on this list is a
			// symbol that could start leaking into a package with no router
			// binding without anything noticing.
			"_SlngScoped", "_slng_llm_node", "_slng_scope",
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

// TestSlngRouterPureProxyRidesTheBody is the gate on the one authored switch
// that suppresses the router's serve. It has to reach the request body: as a
// constructor kwarg the SDK would reject it, and silently dropped it would
// leave a package believing it was protected.
//
// Why a package would author it: the cache key is the (assistant speech, user
// speech) pair and carries no system prompt, so two agents in one package whose
// last exchange matches share a cache entry under one agent_id. Measured
// 2026-08-21 on three live calls of examples/salon-concierge, the booking
// specialist's opening turn was served the concierge's "what phone number
// should I use" — cache_layer l2_exact, 1.27ms, no model call.
func TestSlngRouterPureProxyRidesTheBody(t *testing.T) {
	for _, tc := range routerTargets() {
		t.Run(string(tc.provider), func(t *testing.T) {
			agent := routerFixture(t)
			for name, tgt := range agent.Targets {
				for profile, binding := range tgt.Models.Reason {
					if !binding.Router() {
						continue
					}
					binding.Params = map[string]any{
						"world_part_override": "eu",
						"reasoning_effort":    "none",
						"slng_pure_proxy":     true,
					}
					tgt.Models.Reason[profile] = binding
				}
				agent.Targets[name] = tgt
			}
			_, all := emitAgentSource(t, agent, tc.provider, tc.module)
			if !strings.Contains(all, `"slng_pure_proxy": True`) {
				t.Error("emitted source does not carry slng_pure_proxy in the request body")
			}
			// The consumed-param rule: never also a kwarg the SDK never heard of.
			if strings.Contains(all, "slng_pure_proxy=True") {
				t.Error("emitted source passes slng_pure_proxy as a constructor kwarg")
			}
			// Unset must leave the router serving, with no mention either way.
			if _, unset := emitAgentSource(t, routerFixture(t), tc.provider, tc.module); strings.Contains(unset, "slng_pure_proxy") {
				t.Error("emitted source mentions slng_pure_proxy with none authored")
			}
		})
	}
}

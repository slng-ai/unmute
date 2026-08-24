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
	//
	// Two variables, and the second one is here because its absence hid a defect.
	// customer_id is written when a task finishes; caller_alias is written by the
	// generated capture tool when the caller offers it. Those are different write
	// sites, and a fixture with only the first let a refresh that covered only the
	// first look complete.
	agent.Variables["caller_alias"] = ir.Variable{
		Type: ir.PrimitiveString, Source: ir.VariableSourceConversation,
		Description: "What the caller says to call them.",
	}
	for _, name := range []string{"intake", "billing"} {
		def := agent.Agents[name]
		def.Instructions += "\n\nThe caller is {{customer_id}}, who goes by {{caller_alias}}."
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

// The one thing a scope count cannot see, now for both dicts. The plugin copies
// the per-request extra_kwargs first and then overwrites extra_body and
// extra_headers from its own constructor options (livekit-plugins-openai
// llm.py:956-962), so either dict left at construction silently wins over the
// per-request one while the emitted source still looks right. For the headers
// that costs the per-site scope; for the body it costs FR-001, because the
// snapshot in a constructor body is the one taken when the call started.
//
// The summarizer is the exception and keeps both at construction. It is built
// inside an agent method at handoff time, so its construction is the moment of
// its request, and this fixture uses full history so it emits none at all.
func TestSlngRouterLiveKitPassesNoConstructorExtras(t *testing.T) {
	agent := routerFixture(t)
	source, _ := emitAgentSource(t, agent, ir.ProviderLiveKit, "agent.py")
	for _, banned := range []struct{ text, cost string }{
		{"extra_headers={", "overwrites the per-request scope"},
		{"extra_body={", "freezes the variable snapshot at the moment the call started"},
	} {
		if strings.Contains(source, banned.text) {
			t.Errorf("livekit builds a router model with %s at construction, which %s:\n%s", banned.text, banned.cost, source)
		}
	}
}

// FR-001 and FR-002. The per-request dict carries the whole body extension, not
// half of it, and it carries it on every site of both targets.
//
// Both halves in one gate on purpose: they travel together and the failure of
// leaving one behind at the constructor looks like nothing at all. A request
// missing slng_config reaches a router with no model configuration; a request
// missing template_variables reaches the model with an empty placeholder.
//
// The LiveKit task path is named explicitly because it is not the same code
// path. A task reaches the node through _RetryEmptyTaskResponseMixin, which
// overrides llm_node for its own placeholder stripping and dispatches to the
// module function, and PR #134 recorded that a second copy of that body is
// exactly how a task comes to send the wrong scope. One copy of the body means
// one snapshot too.
func TestSlngRouterSendsTheWholeBodyPerRequest(t *testing.T) {
	agent := routerFixture(t)
	for _, tc := range routerTargets() {
		source, _ := emitAgentSource(t, agent, tc.provider, tc.module)
		for _, want := range []string{`"slng_config": _slng_config_fast_reasoning()`, `"template_variables": _slng_template_variables(`} {
			if !strings.Contains(source, want) {
				t.Errorf("%s: the request body does not carry %s", tc.provider, want)
			}
		}
		if tc.provider != ir.ProviderLiveKit {
			continue
		}
		// One body, one node, and the retry mixin dispatching into it rather
		// than writing its own.
		if got := strings.Count(source, `"template_variables": _slng_template_variables(`); got != 1 {
			t.Errorf("livekit writes the snapshot in %d places, want 1: a second copy is how a task sends a stale one", got)
		}
		if !strings.Contains(source, "return _slng_llm_node(self, chat_ctx, tools, model_settings)") {
			t.Errorf("livekit's task retry path does not dispatch into the one node that builds the body:\n%s", source)
		}
	}
}

// FR-005, the half a scope gate cannot see. A router site whose prompts
// reference no variable sends no template_variables key at all, rather than an
// empty dict. An empty dict is not free: it is a field the router then scans, and
// it makes a package that authored nothing look like one that authored something.
func TestSlngRouterOmitsTheSnapshotWithNoNames(t *testing.T) {
	agent := routerFixture(t)
	// Same fixture with the placeholders taken back out of every prompt.
	for name, def := range agent.Agents {
		def.Instructions = strings.ReplaceAll(def.Instructions, "\n\nThe caller is {{customer_id}}.", "")
		agent.Agents[name] = def
	}
	for name, task := range agent.Tasks {
		task.Instructions = strings.ReplaceAll(task.Instructions, " for {{customer_id}}", "")
		agent.Tasks[name] = task
	}
	for _, tc := range routerTargets() {
		source, _ := emitAgentSource(t, agent, tc.provider, tc.module)
		if strings.Contains(source, "template_variables") && !strings.Contains(source, "# ") {
			t.Errorf("%s: a package whose prompts reference no variable still sends template_variables", tc.provider)
		}
		if !strings.Contains(source, "_slng_config_fast_reasoning()") {
			t.Errorf("%s: dropping the snapshot also dropped the model configuration", tc.provider)
		}
	}
}

// FR-001, the spelling of the defect this feature exists to fix. The snapshot is
// read from the object the call writes into, at the point of the request, and
// never from a name bound once in the entrypoint.
//
// `slng_state` is that entrypoint local on livekit. A snapshot rendered with it
// can only be evaluated where it is in scope, which is the session construction,
// which is once per call. So the presence of that name inside the per-request
// node is the exact shape of the freeze, and its absence is the fix.
func TestSlngRouterSnapshotReadsLiveState(t *testing.T) {
	agent := routerFixture(t)
	for _, tc := range routerTargets() {
		source, _ := emitAgentSource(t, agent, tc.provider, tc.module)
		want := "_slng_template_variables(session.userdata,"
		frozen := "_slng_template_variables(slng_state,"
		if tc.provider == ir.ProviderPipecat {
			want = "_slng_template_variables(state,"
			frozen = ""
		}
		if !strings.Contains(source, want) {
			t.Errorf("%s: the snapshot does not read the live state object (want %s)", tc.provider, want)
		}
		if frozen != "" && strings.Contains(source, frozen) {
			t.Errorf("%s: the snapshot reads %s, an entrypoint local bound once per call, which is the freeze this feature removes", tc.provider, frozen)
		}
	}
}

// FR-003 and FR-004, neither of which had a gate anywhere before this.
//
// FR-003 is the rule that keeps a live call alive: the router answers a request
// referencing a {{name}} it was not given with a 422, so a name whose value is
// not known yet is still supplied, as an empty string. The emitted helper is
// where that happens, and the tuple of names is what makes it happen for every
// referenced name rather than the ones that happen to be set.
//
// FR-004 is the truncation. Its only other check is in the opt-in smoke tier, so
// after moving the body this one holds the wiring in the default suite: the
// snapshot still routes through the helper that truncates, rather than a literal
// dict assembled at the call site.
func TestSlngRouterSuppliesEveryNameAndTruncates(t *testing.T) {
	agent := routerFixture(t)
	for _, tc := range routerTargets() {
		source, _ := emitAgentSource(t, agent, tc.provider, tc.module)
		// Every referenced name is passed, whether or not the call knows it yet,
		// and both write kinds are represented: one a task assigns, one the caller
		// offers.
		if !strings.Contains(source, `("caller_alias", "customer_id")`) {
			t.Errorf("%s: the snapshot does not supply every referenced name; an unsupplied name is a 422 mid-call", tc.provider)
		}
		// And the values come from the helper, which is what fills an unset name
		// with "" and truncates an over-long one.
		for _, want := range []string{
			`values[name] = text`,
			`text = "" if value is None else str(value)`,
			`if len(text) > _SLNG_VARIABLE_LIMIT:`,
			`text = text[:_SLNG_VARIABLE_LIMIT]`,
		} {
			if !strings.Contains(source, want) {
				t.Errorf("%s: the snapshot helper no longer %q; an unset name would send None and an over-long one would reach the router whole", tc.provider, want)
			}
		}
	}
}

// FR-001 on Pipecat, where the mechanism is a refresh rather than a hook. Every
// place the call writes a variable is followed by a settings delta carrying the
// body, and that delta carries no extra_headers key.
//
// The second half matters as much as the first. A settings update merges the
// `extra` dict key by key (pipecat services/settings.py apply_update: "keys
// present in the delta overwrite keys in the target"), so a body-only delta
// leaves the site's scope alone, and a delta that named extra_headers as well
// would replace the scope of whichever site is speaking.
func TestSlngRouterPipecatRefreshesTheBodyOnEveryWrite(t *testing.T) {
	agent := routerFixture(t)
	// The fixture's delegates assign nothing, so give one an assignment: that is
	// the only way a call writes a variable mid-conversation, and it is the whole
	// case this gate is about. The pairing is arbitrary because the compiler does
	// not care which field feeds which variable, only that a write is followed by
	// a refresh.
	agent.Controls["run_collect"] = &ir.Delegate{
		Kind: ir.ControlDelegate, Task: "collect", When: "Collect the caller's account details.",
		Assign: map[string]string{"customer_id": "result.tier"},
	}
	source, _ := emitAgentSource(t, agent, ir.ProviderPipecat, "bot.py")
	// Every place the emitted module assigns into the call state, not just the
	// ones this test remembered to think of. A task result is one; the generated
	// capture tool is another, and it is the one a caller-offered name arrives
	// through. Counting the writes rather than naming them is what makes a fourth
	// write site fail here instead of shipping.
	writes := regexp.MustCompile(`self\.state\.[a-z_]+ = `).FindAllString(source, -1)
	if len(writes) < 2 {
		t.Fatalf("the fixture exercises %d state write sites, want at least the task result and the capture tool: %v", len(writes), writes)
	}
	if got := strings.Count(source, `extra={"extra_body":`); got < len(writes) {
		t.Errorf("%d state writes (%v) and %d body refreshes; a value written where nothing refreshes never reaches the router", len(writes), writes, got)
	}
	// The refresh dict names the body only. Anything that also named the headers
	// would hand the speaking site somebody else's scope.
	for _, line := range strings.Split(source, "\n") {
		if strings.Contains(line, `"extra_body": {"slng_config"`) && strings.Contains(line, "X-Slng-Agent-Id") && strings.Contains(line, "LLMSettings") {
			t.Errorf("a refresh delta carries the scope header as well as the body, which replaces the speaking site's scope:\n%s", line)
		}
	}
}

// FR-001 for the one site that needs no per-request path. The summarizer is built
// inside an agent method at handoff time, so its construction is the moment of
// its request: its extras stay where they are, and they include the snapshot.
//
// It is here because it is the site the fix for the others could quietly break.
// Moving every body per request and forgetting this one leaves a summarizer with
// no model configuration at all.
func TestSlngRouterSummarizerKeepsConstructionExtras(t *testing.T) {
	agent := routerFixture(t)
	// history: summary on a handoff is what builds a summarizer at all, and it
	// needs a profile named to do the summarising. The router profile is the one
	// this package has, which is also the case worth gating: the summarizer then
	// asks the router too, under its own scope.
	for name, task := range agent.Tasks {
		task.Context = ir.TaskContext{History: ir.HistorySummary, Summarizer: "fast_reasoning"}
		agent.Tasks[name] = task
	}
	source, _ := emitAgentSource(t, agent, ir.ProviderLiveKit, "agent.py")
	if !strings.Contains(source, ":summary") {
		t.Fatalf("no summarizer scope in the emitted module, so this gate is watching nothing:\n%s", source)
	}
	// Its extras stay at construction, and they carry the whole body.
	for _, want := range []string{
		"_slng_template_variables(self.session.userdata,",
		"_slng_config_fast_reasoning()",
		`"X-Slng-Agent-Id": "` + routerAgentID + `:summary"`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("the summarizer construction does not carry %s; moving every body per request left this site with nothing:\n%s", want, source)
		}
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
	// Written, not merely mentioned. The provenance hook reads this header off the
	// request it is describing, which is a read and cannot carry a scope of its
	// own; counting every mention would fail on that and say nothing true.
	if got := strings.Count(source, target.SlngAgentIDHeader+`": `); got != 1 {
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
		if !strings.Contains(source, "The caller is {{customer_id}}, who goes by {{caller_alias}}.") {
			t.Errorf("%s: the router prompt lost its placeholders", tc.provider)
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

// slngProvenanceKeys is the line's field set and its order, from
// contracts/provenance-log.md. One list, read by two gates, because the claim is
// that both drivers write the same line.
var slngProvenanceKeys = []string{"scope=", "source=", "layer=", "model=", "request_id="}

// FR-008. One helper per target, and both targets logging the same fields in the
// same order.
//
// The order is part of the contract because a person greps this line and a script
// parses it. Two drivers drifting into two orders is the kind of difference nobody
// notices until a parser written against one is pointed at the other.
func TestSlngRouterProvenanceLineHasOneOwner(t *testing.T) {
	agent := routerFixture(t)
	for _, tc := range routerTargets() {
		source, _ := emitAgentSource(t, agent, tc.provider, tc.module)
		if got := strings.Count(source, "def _slng_log_provenance("); got != 1 {
			t.Errorf("%s: the provenance helper is defined %d times, want 1", tc.provider, got)
		}
		at := -1
		for _, key := range slngProvenanceKeys {
			next := strings.Index(source, key)
			if next < 0 {
				t.Errorf("%s: the provenance line has no %s field", tc.provider, key)
				continue
			}
			if next < at {
				t.Errorf("%s: %s is written out of the contract's order (%v)", tc.provider, key, slngProvenanceKeys)
			}
			at = next
		}
		// The prefix a reader greps for.
		if !strings.Contains(source, `"slng router: "`) {
			t.Errorf("%s: the line has no stable prefix to grep for", tc.provider)
		}
		// And the safety rules, each of which is a live-call defect if dropped:
		// async on an async client, headers only because the body is unread, and
		// never raising into the request path.
		for _, want := range []struct{ text, why string }{
			{"async def _slng_log_provenance(", "a sync hook on an AsyncClient is never awaited"},
			{"except Exception:", "a hook that raises fails the request it was only meant to describe"},
		} {
			if !strings.Contains(source, want.text) {
				t.Errorf("%s: the hook is missing %q: %s", tc.provider, want.text, want.why)
			}
		}
		if strings.Contains(source, "response.read()") || strings.Contains(source, "response.text") {
			t.Errorf("%s: the hook reads the response body, which the framework is about to stream", tc.provider)
		}
		// The discriminator, so the hook describes only what it can name.
		if !strings.Contains(source, "request.headers.get("+`"`+target.SlngAgentIDHeader+`"`) {
			t.Errorf("%s: the hook does not decide from the request's own scope header whether to log", tc.provider)
		}
	}
}

// FR-005 for the provenance line. A package with no router think binding emits no
// hook, no client of its own and no line, so nothing about it changes.
func TestSlngRouterProvenanceAbsentWithoutARouterBinding(t *testing.T) {
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
		for _, banned := range []string{"_slng_log_provenance", "slng router: ", "_slng_router_client"} {
			if strings.Contains(all, banned) {
				t.Errorf("%s: a package with no router binding emits %q", tc.provider, banned)
			}
		}
	}
}

// The one thing this hook costs, and the reason it is a gate. Passing a client to
// the plugin sets `_owns_client = False` and its aclose() then closes nothing
// (livekit-plugins-openai llm.py:161 and 178-180), so a client we build is a
// client we have to close. A worker serving call after call would otherwise leak
// one connection pool per session.
func TestSlngRouterLiveKitClosesTheClientItOwns(t *testing.T) {
	agent := routerFixture(t)
	source, _ := emitAgentSource(t, agent, ir.ProviderLiveKit, "agent.py")
	// Built once, in the entrypoint, and held on the call's own state object so a
	// construction inside an agent method reaches the same one.
	if got := strings.Count(source, "= _slng_router_client()"); got != 1 {
		t.Errorf("the router client is built in %d places, want 1: a second one is a second connection pool with no owner", got)
	}
	if !strings.Contains(source, "client=slng_state.slng_client") {
		t.Errorf("the session model is not given the client this call owns, so its response hook never runs:\n%s", source)
	}
	if !strings.Contains(source, "ctx.add_shutdown_callback") {
		t.Error("the emitted entrypoint registers no shutdown callback, so the client it owns is never closed")
	}
	if !strings.Contains(source, ".slng_client.close()") {
		t.Error("nothing closes the router client; the plugin closes only a client it built itself")
	}
}

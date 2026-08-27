package generate

import (
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// The salon package is the one the docs tell a reader to run, by target name. A
// target renamed, dropped, or repointed here turns those commands into commands
// that fail, which is the failure this gate exists to prevent. Both surviving
// targets resolve a telephony route and both generate.
func TestSalonConciergeTargetsResolveAndGenerate(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "salon-concierge"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pipecat", "livekit"} {
		t.Run(name, func(t *testing.T) {
			resolved, ok := agent.Targets[name]
			if !ok {
				t.Fatalf("no target %q, and the docs name it", name)
			}
			if resolved.Telephony == nil {
				t.Fatalf("target %q resolves no telephony route", name)
			}
			if _, err := Generate(agent, resolved, target.Default()); err != nil {
				t.Errorf("target %q does not generate: %v", name, err)
			}
		})
	}
}

// loadExample resolves one shipped package, so a test can assert on the same IR
// the compiler works from rather than on the YAML text.
func loadExample(t *testing.T, name string) *ir.Agent {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", name))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestSalonConciergeFeatureContract(t *testing.T) {
	resolved := loadExample(t, "salon-concierge")
	// The turn-latency contract on both targets: thinking goes through the SLNG
	// Context Router, the model does not think before its first token, and the
	// caller never hears another agent's line.
	//
	// The middle one is now held by a prompt directive rather than by a parameter,
	// and that is not a style choice. Three spellings of the thinking-off parameter
	// were sent to three of this model's hosts on 2026-08-27, nine requests: every
	// one accepted, every one ignored, hundreds of reasoning tokens each time. The
	// model's own /no_think directive in the system prompt is the only thing that
	// worked.
	//
	// The last of those was measured, not imagined. The router's cache key is the
	// (assistant speech, user speech) pair and carries no system prompt, so two
	// of this package's agents whose last exchange matched collided while they
	// shared one cache scope. 2026-08-21, three live calls: the booking
	// specialist's opening turn was served the concierge's "what phone number
	// should I use", cache_layer l2_exact, 1.27ms, no model call.
	//
	// The fix is one scope per prompt site, which is what the scope assertion at
	// the end of this function holds. It replaced slng_pure_proxy, which used to
	// be required here and which bought the same safety by turning cache serving
	// off entirely.
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		reason := targetByProvider(t, resolved, provider).Models.Reason["reasoning"]
		if !reason.Router() {
			t.Errorf("%s reasoning is not a router binding: %#v", provider, reason)
		}
		// reasoning_effort is not optional once the agent has tools: the GPT-5
		// family rejects function tools on chat completions without it, and every
		// tool turn comes back 400.
		if reason.Params["reasoning_effort"] != "none" {
			t.Errorf("%s reasoning params = %#v, want reasoning off before the first token", provider, reason.Params)
		}
		// No host pin and no prompt directive: this binding is on OpenAI's own
		// endpoint, which serves one implementation of the model.
		//
		// A qwen/qwen3-32b trial on OpenRouter was reverted on 2026-08-27. Measured
		// on this package's own prompt and tools, 12 reps: luna routes a multi-turn
		// booking request to the verification delegate 12/12 at p50 1.08s, qwen3-32b
		// managed 4-8/12 and improvised the flow the rest of the time, and on a live
		// call it told a caller their appointment was booked when no booking tool had
		// run. Four other candidates were screened and none cleared the bar. If you
		// are here to swap the model, screen it on multi-turn tool routing first:
		// latency and single-turn tool calls predict neither.
		if _, pinned := reason.Params["provider"]; pinned {
			t.Errorf("%s reasoning pins a host: %#v. OpenAI's endpoint serves one implementation", provider, reason.Params)
		}
		if reason.PromptSuffix != "" {
			t.Errorf("%s reasoning sets prompt_suffix = %q; this model takes reasoning_effort instead", provider, reason.PromptSuffix)
		}
		// This used to require slng_pure_proxy, which stops the router serving
		// from cache at all. It was a guard against a cross-agent cache hit
		// repeating an earlier agent's line to the caller, and it bought that
		// safety by giving up the speed the router exists for. The real fix is
		// one cache scope per prompt site, and with that in place the guard is a
		// workaround the example should not be teaching. If you are here because
		// you put it back: the collision it guarded against cannot happen, and
		// suppressing the serve means every turn goes to the model.
		if _, present := reason.Params["slng_pure_proxy"]; present {
			t.Errorf("%s reasoning sets slng_pure_proxy: %#v. The example demonstrates the router doing its job, and this switch stops it serving from cache", provider, reason.Params)
		}
	}

	// SC-008 on the package a reader actually opens. A fixture and the goldens are
	// gated elsewhere; nothing gated this until the collision had already shipped
	// in it.
	assertSalonScopes(t, resolved)

	// The other half of the turn-latency contract, and the bigger half. LiveKit's
	// turn detector reads the transcript to decide whether the caller has
	// finished, so a transcriber that has not finalised yet leaves it unsure, and
	// an unsure detector waits the full endpointing max_delay of 2.5s instead of
	// the 0.58s floor. Measured on identical audio, 12 clips each, 2026-08-21:
	// gradium/stt:default finalised in 0.999s mean and crossed the 0.55s line on
	// 12 of 12; deepgram/nova:3 finalised in 0.159s mean, worst 0.323s, and
	// crossed it on none, for the same words. Latency itself is not testable
	// without the network, so this pins the model that measurement chose and
	// fails loudly if someone swaps it back.
	transcriber := resolved.Models["transcriber"]
	if transcriber.Model != "deepgram/nova:3" {
		t.Errorf("transcriber model = %q, want deepgram/nova:3: a slower final transcript costs 2s of endpointing per turn", transcriber.Model)
	}

	// A warm TTS socket means the gateway's session init is already done when
	// the text arrives. Worth ~40ms of mean time to first audio and a much
	// better floor: gateway synthesis was 418ms mean / 221ms best without it
	// and 376ms mean / 58ms best with it, over 14 and 9 segments, with
	// standby_used true on 9 of 9 so the mechanism is confirmed. Off by default
	// in the plugin, so an author gets the slow path unless they ask by name.
	//
	// An earlier version of this comment claimed ~610ms. That was the wait for
	// a usable websocket, which LiveKit's tts ttfb does not contain: ttfb
	// tracks gateway synthesis alone, matching it to the millisecond across
	// three runs, so the handshake was already off the caller's path. Pinned
	// here for the floor and the confirmed mechanism, not for a headline.
	voice := resolved.Models["voice"]
	if voice.Params["warm_standby_enabled"] != true {
		t.Errorf("voice params = %#v, want warm_standby_enabled: without it gateway synthesis never drops below ~221ms", voice.Params)
	}

	// This example is the release-readiness package, so it carries the provider
	// that needs a real call to prove: Coval correlates a trace to the call that
	// produced it, and only a live call exercises that.
	if resolved.Tracing == nil || resolved.Tracing.Provider != "coval" {
		t.Fatalf("tracing = %#v, want Coval", resolved.Tracing)
	}
	if !slices.Contains(resolved.Secrets, "COVAL_API_KEY") {
		t.Errorf("secrets omit COVAL_API_KEY: %v", resolved.Secrets)
	}
	for _, name := range []string{"LANGFUSE_BASE_URL", "LANGFUSE_PUBLIC_KEY", "LANGFUSE_SECRET_KEY"} {
		if slices.Contains(resolved.Secrets, name) {
			t.Errorf("secrets still require %s after the move to Coval", name)
		}
	}
	manager, ok := resolved.Controls["to_manager"].(*ir.HumanTransfer)
	if !ok || manager.Mode != ir.TransferCold || manager.OnUnavailable != ir.OnUnavailableHangup {
		t.Fatalf("manager transfer = %#v, want cold transfer with hangup fallback", resolved.Controls["to_manager"])
	}

	// Every internal handoff stays silent and carries the whole conversation, so
	// the receiving agent never reintroduces itself and never re-asks a question
	// already answered.
	//
	// What is deliberately NOT asserted here is a prerequisite on every handoff.
	// This package used to gate all four on an identifier, which meant a caller
	// who opened with "I want to speak to a manager" was interviewed before
	// anyone would route them. A prerequisite belongs on the step that needs the
	// value, not on the act of changing who is speaking. T012 below holds the
	// escalation path specifically.
	for name, control := range resolved.Controls {
		transfer, ok := control.(*ir.AgentTransfer)
		if !ok {
			continue
		}
		if transfer.Announce != "" || transfer.Context.History != ir.HistoryFull ||
			!transfer.Context.Variables.All {
			t.Errorf("internal handoff %q must stay silent and carry full history and every variable: %#v", name, transfer)
		}
	}

	// FR-008 to FR-010: nothing on the path from the entry agent to a person
	// carries a prerequisite. The entry agent holds the escalation control
	// directly, and the handoff to customer care is ungated, so the two ways a
	// caller reaches a human both work on the first utterance.
	entry := resolved.Agents[resolved.EntryAgent]
	reachesAPerson := false
	for _, name := range entry.Tools {
		if _, ok := resolved.Controls[name].(*ir.HumanTransfer); ok {
			reachesAPerson = true
		}
	}
	if !reachesAPerson {
		t.Errorf("entry agent %q holds no human transfer: %v; a caller asking for a person must not have to pass through another agent to get one", resolved.EntryAgent, entry.Tools)
	}
	for _, name := range []string{"to_complaints", "to_concierge"} {
		transfer, ok := resolved.Controls[name].(*ir.AgentTransfer)
		if !ok {
			t.Fatalf("control %q is not an agent transfer: %#v", name, resolved.Controls[name])
		}
		if len(transfer.Requires) != 0 {
			t.Errorf("handoff %q carries prerequisites %v; hearing a complaint and returning to the entry agent must not be gated on identifying the caller", name, transfer.Requires)
		}
	}

	// Every action tool here is a local Python handler, so nothing remote has to
	// be reachable before the greeting. The MCP path is exercised by
	// examples/mcp-example instead; this example used to carry a Firecrawl
	// web_search tool and no longer does.
	//
	// The two knowledge tools are the exception, and they are not remote in the
	// same sense: they read documents compiled into the image. They do reach an
	// embedding service at startup, which is why the example declares
	// OPENAI_API_KEY, and never during a call.
	for name, agent := range resolved.Agents {
		if slices.Contains(agent.Tools, "web_search") {
			t.Errorf("agent %q still lists the removed web_search tool: %v", name, agent.Tools)
		}
	}
	for name, task := range resolved.Tasks {
		if slices.Contains(task.Tools, "web_search") {
			t.Errorf("task %q exposes web_search", name)
		}
	}
	for name, tool := range resolved.Tools {
		if tool.Execution == ir.ToolKnowledge {
			continue // checked below, by base and by which agent holds it
		}
		if tool.Execution != ir.ToolLocal || tool.Handler != "tools/salon.py" {
			t.Errorf("tool %q = %#v, want shared local Python handler", name, tool)
		}
	}
	// The two knowledge bases, and the isolation that is the point of having two.
	//
	// An agent gets a knowledge base by being given its tool, so this is the whole
	// access model: the concierge can quote prices and cannot quote refund policy,
	// and the complaint specialist is the other way round. A tool added to the
	// wrong agent here is a real leak, not a style slip, so it is a test.
	for base, wantFolder := range map[string]string{
		"refunds":  "knowledge/refunds",
		"services": "knowledge/services",
	} {
		declared, ok := resolved.Knowledge[base]
		if !ok {
			t.Errorf("knowledge base %q is not declared", base)
			continue
		}
		if declared.Documents != wantFolder {
			t.Errorf("knowledge base %q documents = %q, want %q", base, declared.Documents, wantFolder)
		}
		if declared.Embed != "openai" {
			t.Errorf("knowledge base %q embed = %q, want the resolved default", base, declared.Embed)
		}
		if len(declared.Files) == 0 {
			t.Errorf("knowledge base %q carries no documents, so the compiler read nothing", base)
		}
	}
	for tool, wantAgent := range map[string]string{
		"look_up_refund_policy": "complaint_specialist",
		"look_up_salon_info":    "concierge",
	} {
		for name, agent := range resolved.Agents {
			holds := slices.Contains(agent.Tools, tool)
			if holds && name != wantAgent {
				t.Errorf("agent %q holds %q, which only %q should: an agent given the tool can quote the document", name, tool, wantAgent)
			}
			if !holds && name == wantAgent {
				t.Errorf("agent %q does not hold %q", name, tool)
			}
		}
	}
	// chat_with_me answers from the model and routes onward, so everything it
	// holds has to be a handoff rather than a tool.
	for _, held := range resolved.Agents["chat_with_me"].Tools {
		if _, isTool := resolved.Tools[held]; isTool {
			t.Errorf("chat_with_me must hold handoffs only, got tool %q", held)
		}
	}
	// One booking task, not three chained steps. Each step boundary costs its own
	// LLM round trip to finish, which the caller hears as silence, and the
	// mutation tools already refuse an unconfirmed write, so the split bought
	// latency and no safety. Held here so it cannot drift back.
	booking, ok := resolved.Tasks["booking"]
	if !ok {
		t.Fatal("tasks omit booking")
	}
	for _, name := range []string{"prepare_booking", "confirm_booking", "apply_booking"} {
		if _, split := resolved.Tasks[name]; split {
			t.Errorf("booking is split again into %q; one task owns draft, confirm and apply", name)
		}
	}
	if len(resolved.TaskGroups) != 0 {
		t.Errorf("task groups = %v, want none: the booking flow is one task", slices.Sorted(maps.Keys(resolved.TaskGroups)))
	}
	// The one task has to reach every step it absorbed: read the diary, resolve a
	// relative date, offer times, and write exactly one of the three mutations.
	for _, want := range []string{
		"list_bookings", "get_current_date", "check_availability",
		"create_booking", "modify_booking", "cancel_booking",
	} {
		if !slices.Contains(booking.Tools, want) {
			t.Errorf("booking tools = %v, want %q", booking.Tools, want)
		}
	}
	wantBookingResult := []string{"action", "booking_id", "status", "summary"}
	if got := slices.Sorted(maps.Keys(booking.Result)); !slices.Equal(got, wantBookingResult) {
		t.Errorf("booking result = %v, want %v", got, wantBookingResult)
	}
	bookingDelegate, ok := resolved.Controls["manage_booking"].(*ir.Delegate)
	if !ok || bookingDelegate.Task != "booking" || bookingDelegate.Group != "" {
		t.Fatalf("manage_booking = %#v, want a delegate to the single booking task", resolved.Controls["manage_booking"])
	}
	currentDate, ok := resolved.Tools["get_current_date"]
	if !ok {
		t.Fatal("tools omit get_current_date")
	}
	inputProperties, ok := currentDate.Input["properties"].(map[string]any)
	if !ok || len(inputProperties) != 0 {
		t.Errorf("get_current_date input properties = %#v, want empty object", currentDate.Input["properties"])
	}
	outputProperties, ok := currentDate.Output["properties"].(map[string]any)
	if !ok {
		t.Fatalf("get_current_date output properties = %#v, want object", currentDate.Output["properties"])
	}
	dateProperty, ok := outputProperties["date"].(map[string]any)
	if !ok || dateProperty["type"] != "string" {
		t.Errorf("get_current_date date output = %#v, want string", outputProperties["date"])
	}
	if required, ok := currentDate.Output["required"].([]any); !ok || !slices.Contains(required, any("date")) {
		t.Errorf("get_current_date required output = %#v, want date", currentDate.Output["required"])
	}
	for _, name := range []string{"create_booking", "modify_booking", "cancel_booking"} {
		tool := resolved.Tools[name]
		properties := tool.Input["properties"].(map[string]any)
		confirmed := properties["confirmed"].(map[string]any)
		required := tool.Input["required"].([]any)
		if confirmed["type"] != "boolean" || !slices.Contains(required, any("confirmed")) {
			t.Errorf("tool %q must require boolean confirmed: %#v", name, tool.Input)
		}
	}

	requireText := func(name, text string, wants ...string) {
		t.Helper()
		text = strings.Join(strings.Fields(text), " ")
		for _, want := range wants {
			if !strings.Contains(text, strings.Join(strings.Fields(want), " ")) {
				t.Errorf("%s omits %q", name, want)
			}
		}
	}
	// A receiving specialist joins a conversation that is already running, so its
	// opening turn continues rather than greets. All three prompts used to open
	// with "Use their name once, early", which is an instruction to address the
	// caller up front: on a live Pipecat WebRTC call on 2026-08-24 that came out
	// as a bare "Hi", because a blank value drops the name and leaves the
	// greeting behind.
	for _, name := range []string{"complaint_specialist"} {
		body := resolved.Agents[name].Instructions
		requireText(name, body,
			"You join a conversation that is already running",
			"never open")
		for _, banned := range []string{"once, early", "early, and not again"} {
			if strings.Contains(body, banned) {
				t.Errorf("%s tells the agent to use the caller's name early, which turns a handoff into a greeting: %q", name, banned)
			}
		}
	}

	// The caller hears the booking once, not twice. The task's confirmation
	// question has to restate the service, the day and the time, because it is
	// the yes-gate and nothing said before it counts as a yes. That makes the
	// relay afterwards the redundant one, so the relay is what stays short.
	// Heard on a live call on 2026-08-24: "haircut at 15" in the confirmation
	// question and again in the outcome. Both halves are held, because either
	// one drifting alone brings the repetition back.
	requireText("concierge", resolved.Agents["concierge"].Instructions,
		"confirm it in one short sentence without repeating the service, the day and the time")
	requireText("booking task", resolved.Tasks["booking"].Instructions,
		"Say the whole thing back in one sentence and ask one yes-or-no question",
		"does not repeat the details")

	// One placeholder, in the one prompt that says the value. The compiler sends
	// the union of every name any prompt on the think profile references, so an
	// extra value reaches all six sites whatever their own prompt holds; and the
	// router's sharing scan refuses any cached answer still containing a value
	// sent for that call, matching whole words and word beginnings. So a short
	// value like a first name costs sharing across every site and buys it back
	// only where a prompt says it. A phone number in this shape is long and
	// specific enough not to. The blank rule is here because the value is absent
	// until verification returns, which is most of the call.
	requireText("concierge", resolved.Agents["concierge"].Instructions,
		"{{customer_phone}}", "empty until they have been identified")

	// FR-007c: an agent that holds a tool injecting a variable must be able to
	// SEE that variable, or it cannot tell whether the value is already there.
	//
	// This replaces an assertion that said the opposite. That one required the
	// placeholder to live in exactly one prompt, on the reasoning that a prompt
	// should not hold a value it never says. It cost a live call on 2026-08-27:
	// a caller who had already been identified during booking raised a complaint,
	// and customer care asked for their phone number again, because the number was
	// injected into its own record_complaint tool and invisible to its prompt.
	//
	// The old rule's stated reason does not hold either. The compiler sends the
	// union of names any router-bound prompt on the profile references, as one
	// tuple shared by every agent, so customer_phone was already being sent on
	// every request from every agent including this one. A second prompt
	// referencing it sends nothing new and costs nothing. The value was being
	// paid for and thrown away.
	injectedBy := map[string][]string{}
	for toolName, tool := range resolved.Tools {
		for _, value := range tool.Inject {
			text, ok := value.(string)
			if !ok {
				continue
			}
			injectedBy[toolName] = append(injectedBy[toolName], ir.TemplateRefs(text)...)
		}
	}
	// A step reached only through a guarded delegate is the one exemption, and it
	// is not a loophole: the guard has already refused the step unless the value
	// is set, so the prompt cannot be wrong about it. That is why the booking step
	// needs no placeholder and customer care does. Nothing guards the route to
	// customer care, deliberately, because a caller with a complaint must not be
	// interrogated before anyone will listen.
	guaranteed := map[string][]string{}
	for _, control := range resolved.Controls {
		delegate, ok := control.(*ir.Delegate)
		if !ok || len(delegate.Requires) == 0 {
			continue
		}
		if delegate.Task != "" {
			guaranteed[delegate.Task] = append(guaranteed[delegate.Task], delegate.Requires...)
		}
	}
	canSee := func(holder, kind, prompt string, tools []string, given []string) {
		for _, toolName := range tools {
			for _, variable := range injectedBy[toolName] {
				if strings.Contains(prompt, "{{"+variable+"}}") || slices.Contains(given, variable) {
					continue
				}
				t.Errorf("%s %q holds %q, which injects %s, but it can neither see {{%s}} nor is it reached through a guard that requires it: it has no way to tell whether the value is already collected, so it asks the caller for something it already has",
					kind, holder, toolName, variable, variable)
			}
		}
	}
	for name, def := range resolved.Agents {
		canSee(name, "agent", def.Instructions, def.Tools, nil)
	}
	for name, task := range resolved.Tasks {
		canSee(name, "task", task.Instructions, task.Tools, guaranteed[name])
	}

	// Verification is one phone number, nothing else. Spelling a name over a
	// transcriber is the slowest and least reliable thing a caller can be asked
	// to do, and the number alone identifies the record.
	verification := resolved.Tasks["customer_verification"]
	requireText("verification", verification.Instructions,
		"A complete number is 10 to 15 digits",
		"Never invent a country code",
		"Read every digit back once",
		"On a clear yes, look the number up")
	for _, banned := range []string{"first name", "surname", "one letter at a time", "spell"} {
		if strings.Contains(strings.ToLower(verification.Instructions), banned) {
			t.Errorf("verification still collects a name: prompt mentions %q", banned)
		}
	}
	if _, named := verification.Result["customer_name"]; named {
		t.Error("verification still returns customer_name; the phone number is the identity")
	}
	// The number this task returns is the value every later prompt substitutes,
	// so the shape it comes back in is part of the contract and not a style
	// note. Asking for the number is already in the prompt above; this is only
	// about how it is written down on the way out.
	requireText("verification", verification.Instructions,
		"digit groups separated by single spaces",
		"A country code is simply the first group")
	if _, returned := verification.Result["customer_phone"]; !returned {
		t.Error("verification no longer returns customer_phone, so the specialists' {{customer_phone}} has no value and the router answers their first request with a 422")
	}
	verificationDelegate, ok := resolved.Controls["verify_customer"].(*ir.Delegate)
	if !ok {
		t.Fatalf("verify_customer = %#v, want delegate", resolved.Controls["verify_customer"])
	}
	requireText("verification delegate", verificationDelegate.When,
		"reads the phone number back", "needs a yes before it looks anyone up")
	if _, assigned := verificationDelegate.Assign["customer_name"]; assigned {
		t.Error("verify_customer still assigns customer_name")
	}
	// Assigned from the result rather than captured from speech: a task result
	// lands on both drivers by the same path customer_id already proves, while a
	// conversation-sourced value depends on the capture tool firing, which is
	// the write site Pipecat missed once already.
	if got := verificationDelegate.Assign["customer_phone"]; got != "result.customer_phone" {
		t.Errorf("verify_customer assigns customer_phone from %q, want result.customer_phone", got)
	}
	lookup := resolved.Tools["find_or_create_customer"]
	requireText("customer lookup", lookup.Description,
		"exact confirmed phone number",
		"only after the digit readback got a clear yes",
		"Never guess a number or pass one the caller has not confirmed")
	lookupProperties, ok := lookup.Input["properties"].(map[string]any)
	if !ok {
		t.Fatalf("find_or_create_customer input properties = %#v, want object", lookup.Input["properties"])
	}
	if got := slices.Sorted(maps.Keys(lookupProperties)); !slices.Equal(got, []string{"phone"}) {
		t.Errorf("find_or_create_customer input = %v, want phone only", got)
	}
	// The confirmation gate moved from a step boundary into this one prompt, so
	// the prompt is now the only thing standing between a spoken time and a
	// written booking. Every clause that makes it a gate is held here.
	requireText("booking", booking.Instructions,
		"Say the whole thing back in one sentence and ask one yes-or-no question",
		"Nothing said before that question counts as a yes",
		"including the caller choosing the time",
		"On a clear yes, save it in the same turn with `confirmed` set to true",
		"On a no, or on a second unclear answer, finish with action `none` and save nothing",
		"call `get_current_date` first", "never guess today's date",
		"Never say a booking is saved, moved, or cancelled unless the matching tool ran in this turn")
	// Open chat is the entry agent's job now, and it holds exactly one lookup:
	// the salon's own documents. The prompt's job is the same as the deleted chat
	// agent's was, to stop it claiming a lookup it cannot perform, but the true
	// sentence changed with the tool list. Asserting the old wording here would
	// have pinned a claim that is no longer true of the agent that inherited the
	// work.
	requireText("concierge", resolved.Agents["concierge"].Instructions,
		"the only thing you can look anything up in",
		"say plainly that you cannot check it",
		"Never claim to have searched, browsed, or checked a live source")
	// Every agent that HOLDS the human transfer has to describe what it can and
	// cannot do, not just the one that held it first.
	//
	// This used to name complaint_specialist directly, and that is exactly how it
	// missed: the entry agent gained to_manager so a caller asking for a person
	// is not interrogated first, the truthfulness rules stayed behind on the
	// other agent, and on a live browser call the entry agent invented "please
	// call the salon directly" — advice that is merely useless in a browser and
	// actively wrong on a phone leg, where the caller has already done it.
	for name, def := range resolved.Agents {
		holdsTransfer := false
		for _, tool := range def.Tools {
			if _, ok := resolved.Controls[tool].(*ir.HumanTransfer); ok {
				holdsTransfer = true
			}
		}
		if !holdsTransfer {
			continue
		}
		requireText(name+" (holds the human transfer)", def.Instructions,
			"phone leg", "carrier", "hang up")
		if strings.Contains(def.Instructions, "call the salon directly") {
			t.Errorf("agent %q tells the caller to phone the salon; on the phone leg where this transfer works, they already have", name)
		}
	}

	// The shape, held as tightly as the old shape was held. Two agents, and the
	// booking step is a guarded delegate on the entry agent rather than an agent
	// of its own.
	if len(resolved.Agents) != 2 {
		t.Errorf("the example has %d agents, want 2: %v", len(resolved.Agents), slices.Sorted(maps.Keys(resolved.Agents)))
	}
	guardedStep, ok := resolved.Controls["manage_booking"].(*ir.Delegate)
	if !ok {
		t.Fatalf("manage_booking = %T, want a delegate", resolved.Controls["manage_booking"])
	}
	if !slices.Equal(guardedStep.Requires, []string{"customer_phone"}) {
		t.Errorf("manage_booking requires = %v, want [customer_phone]: the guard is what lets the booking step live on the entry agent", guardedStep.Requires)
	}
	if !slices.Contains(resolved.Agents[resolved.EntryAgent].Tools, "manage_booking") {
		t.Errorf("the entry agent does not hold manage_booking: %v", resolved.Agents[resolved.EntryAgent].Tools)
	}
}

// FR-012 and FR-013: every agent in this example earns its place.
//
// An agent is worth its turn when it holds a capability no other agent does: a
// tool, a knowledge base, or a permission. A control is not a capability. An
// agent that holds only controls is a routing table the caller has to be spoken
// through, and this package shipped two of them for months, purely because the
// compiler could not put a prerequisite on a step.
func TestSalonConciergeHasNoRoutingOnlyAgent(t *testing.T) {
	resolved := loadExample(t, "salon-concierge")

	capabilities := map[string][]string{}
	for name, agent := range resolved.Agents {
		for _, tool := range agent.Tools {
			if _, isControl := resolved.Controls[tool]; isControl {
				continue
			}
			capabilities[name] = append(capabilities[name], tool)
		}
	}

	for name, own := range capabilities {
		if len(own) == 0 {
			t.Errorf("agent %q holds only controls: it is a routing table, and a caller has to be spoken through it to reach anything", name)
		}
	}
	for name := range resolved.Agents {
		if _, ok := capabilities[name]; !ok {
			t.Errorf("agent %q holds nothing at all", name)
			continue
		}
		unique := false
		for _, tool := range capabilities[name] {
			held := 0
			for _, other := range capabilities {
				if slices.Contains(other, tool) {
					held++
				}
			}
			if held == 1 {
				unique = true
			}
		}
		if !unique {
			t.Errorf("agent %q holds no capability its peer does not: %v. Two agents that can do the same things are one agent and a turn of latency", name, capabilities[name])
		}
	}
}

// FR-007a and SC-005b: one caller identifier, and it is the phone number.
//
// The package used to carry two, a synthetic customer reference alongside the
// number that produced it. The reference was never spoken, never shown, and
// never did anything the number could not, so every tool carried it for nothing
// and every prompt had to be told to keep it quiet.
func TestSalonConciergeDeclaresOneIdentifier(t *testing.T) {
	resolved := loadExample(t, "salon-concierge")

	if _, ok := resolved.Variables["customer_id"]; ok {
		t.Error("the package still declares a customer_id variable")
	}
	if _, ok := resolved.Variables["customer_phone"]; !ok {
		t.Errorf("the package declares no customer_phone: %v", slices.Sorted(maps.Keys(resolved.Variables)))
	}
	for name, task := range resolved.Tasks {
		if _, ok := task.Result["customer_id"]; ok {
			t.Errorf("task %q still returns a customer_id field", name)
		}
	}
	for name, tool := range resolved.Tools {
		if _, ok := tool.Inject["customer_id"]; ok {
			t.Errorf("tool %q still injects customer_id", name)
		}
	}
}

func TestExampleMatrixCompilesForCodeTargets(t *testing.T) {
	// Among these four structural comparison packages, tracing stays on only
	// simple-prompt. It used to be on all four, which made
	// the first package in the table impossible to run without a Langfuse
	// account: three third-party values before a first-time reader hears a word
	// (research D12). Which one keeps it is a table row in examples/README.md,
	// and this is the test that holds the count at one.
	cases := []struct {
		name                  string
		agents, tasks, groups int
		tracing               bool
	}{
		{"simple-prompt", 1, 0, 0, true},
		{"multi-task", 1, 2, 0, false},
		{"task-groups", 1, 3, 1, false},
		{"subagents", 2, 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pkg, err := spec.Load(filepath.Join("..", "..", "examples", tc.name))
			if err != nil {
				t.Fatal(err)
			}
			agent, err := ir.Build(pkg)
			if err != nil {
				t.Fatal(err)
			}
			if len(agent.Agents) != tc.agents || len(agent.Tasks) != tc.tasks || len(agent.TaskGroups) != tc.groups {
				t.Fatalf("got agents/tasks/groups %d/%d/%d, want %d/%d/%d", len(agent.Agents), len(agent.Tasks), len(agent.TaskGroups), tc.agents, tc.tasks, tc.groups)
			}
			if tracing := agent.Tracing != nil && agent.Tracing.Provider == "langfuse"; tracing != tc.tracing {
				t.Fatalf("tracing = %t, want %t: only simple-prompt in this comparison configures it", tracing, tc.tracing)
			}
			if len(agent.Tools) != 5 {
				t.Fatalf("got %d tools, want 5", len(agent.Tools))
			}
			for name, def := range agent.Agents {
				for _, required := range []string{"## Voice contract", "Never speak or emit", "immediately and silently", "Never ask the caller to wait"} {
					if !strings.Contains(def.Instructions, required) {
						t.Errorf("agent %q is missing prompt contract %q", name, required)
					}
				}
			}
			for name, task := range agent.Tasks {
				for _, required := range []string{"## Voice contract", "Never speak or emit", "immediately and silently", "runtime-only"} {
					if !strings.Contains(task.Instructions, required) {
						t.Errorf("task %q is missing prompt contract %q", name, required)
					}
				}
			}
			toolContracts := map[string]string{
				"lookup_customer":    "before any availability",
				"create_customer":    "explicitly gave permission",
				"check_availability": "This tool accepts only service and date",
				"book_appointment":   "Never invent either ID",
				"cancel_appointment": "explicit confirmation",
			}
			for name, tool := range agent.Tools {
				if tool.Execution != ir.ToolLocal || tool.URLEnv != "" {
					t.Errorf("tool %q execution/url = %q/%q, want local/empty", name, tool.Execution, tool.URLEnv)
				}
				if !strings.Contains(tool.Description, toolContracts[name]) {
					t.Errorf("tool %q is missing workflow contract %q", name, toolContracts[name])
				}
			}
			if tc.name == "multi-task" {
				parent := agent.Agents["appointment_desk"].Instructions
				if !strings.Contains(parent, "Don't ask for the caller's name or phone number first") ||
					!strings.Contains(parent, "service, date, or appointment ID first") {
					t.Error("multi-task parent can collect task-owned fields before delegation")
				}
				customer := agent.Tasks["customer_record"].Instructions
				appointment := agent.Tasks["appointment_request"].Instructions
				if !strings.Contains(customer, "Never guess an ID") ||
					!strings.Contains(customer, "explicit permission") ||
					!strings.Contains(appointment, "nonempty customer ID") ||
					!strings.Contains(appointment, "Only after booking succeeds") {
					t.Error("multi-task workflow does not enforce customer and appointment gates")
				}
			}
			for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
				t.Run(string(provider), func(t *testing.T) {
					if _, err := Generate(agent, targetByProvider(t, agent, provider), target.Default()); err != nil {
						t.Fatal(err)
					}
				})
			}
		})
	}
}

// TestPublicExamplesValidateAndGenerate is the shipped-example gate (compiler.md V36):
// every package under examples/ must load, build, validate its **declared**
// targets with zero errors, and generate for each one. Generating without
// validating first would let an example ship that `unmute validate` rejects,
// which is the command a reader runs before anything else.
func TestPublicExamplesValidateAndGenerate(t *testing.T) {
	root := filepath.Join("..", "..", "examples")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, entry.Name(), "agent.yaml")); err != nil {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			pkg, err := spec.Load(filepath.Join(root, entry.Name()))
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			agent, err := ir.Build(pkg)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if len(agent.Targets) == 0 {
				t.Fatal("example declares no target")
			}
			var declared []ir.Target
			for _, name := range slices.Sorted(maps.Keys(agent.Targets)) {
				declared = append(declared, agent.Targets[name])
			}
			report, err := ir.Validate(agent, declared, target.Default())
			if err != nil {
				t.Fatalf("validate: %v\n%#v", err, report.PerTarget)
			}
			for _, row := range report.PerTarget {
				if len(row.Errors) > 0 {
					t.Errorf("target %q has errors: %v", row.Name, row.Errors)
				}
			}
			for _, resolved := range declared {
				if _, err := Generate(agent, resolved, target.Default()); err != nil {
					t.Errorf("generate %q: %v", resolved.Name, err)
				}
			}
		})
	}
}

// FR-031 / SC-008: an existing package keeps its meaning. The Daily-route work
// must reach the Daily route and nothing else.
//
// Written as a scoping property rather than against a stored copy of every
// example's old output. A byte-for-byte baseline would need a golden per example
// per file, and it would go stale on the first unrelated template edit, at which
// point the honest question ("did this feature widen?") gets buried in noise.
// The property is the same and it is checkable directly: none of this feature's
// additions may appear on any target that is not the Daily route.
//
// A failure here means the change was made unconditionally. Narrow it, do not
// extend this list.
func TestDailyRouteWorkDoesNotReachOtherTargets(t *testing.T) {
	// Every marker this feature adds to an emitted project.
	markers := []string{
		"DailyParams", "pipecat.transports.daily",
		"## Phone calls",
		"Account prerequisites", "route_prerequisites", "daily_dialout",
		// The carrier leg's own markers (SCHEMA N37): the emitted helper, the
		// forward-once guard, and the runbook's opening line. Carrier work must not
		// leak onto a target that is not the Daily route either.
		//
		// The runbook's *heading* would be the obvious third marker and it is the
		// wrong one: the LiveKit SIP route heads its own runbook "Telephony setup"
		// too, so it names a shape both drivers share rather than one route.
		"telephony_helper.py", "call_forwarded", "One piece runs outside the platform",
	}
	// _transfer_result used to be on that list and is not any more. It is the
	// one-attempt-per-call guard shared by both Pipecat transfer routes, so it now
	// marks "a route that emits a cold transfer" rather than "the Daily route".
	// The Daily-only markers above are what still scopes this test. Public
	// examples prove they do not leak; an internal fixture proves they still emit.
	root := filepath.Join("..", "..", "examples")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	checkedDaily := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, entry.Name(), "agent.yaml")); err != nil {
			continue
		}
		pkg, err := spec.Load(filepath.Join(root, entry.Name()))
		if err != nil {
			t.Fatalf("%s: load: %v", entry.Name(), err)
		}
		agent, err := ir.Build(pkg)
		if err != nil {
			t.Fatalf("%s: build: %v", entry.Name(), err)
		}
		for _, name := range slices.Sorted(maps.Keys(agent.Targets)) {
			resolved := agent.Targets[name]
			daily := resolved.Provider == ir.ProviderPipecat && resolved.Transport == "daily-sip"
			artifact, err := Generate(agent, resolved, target.Default())
			if err != nil {
				t.Fatalf("%s/%s: generate: %v", entry.Name(), name, err)
			}
			for _, file := range artifact.Files {
				for _, marker := range markers {
					if !strings.Contains(string(file.Content), marker) {
						continue
					}
					if !daily {
						t.Errorf("%s/%s (%s, transport %q) emits %q: this feature must reach the Daily route only",
							entry.Name(), name, resolved.Provider, resolved.Transport, file.Path)
					}
					checkedDaily = checkedDaily || daily
				}
			}
		}
	}
	dailyPkg, err := spec.Load(filepath.Join("..", "testdata", "daily_carrier"))
	if err != nil {
		t.Fatal(err)
	}
	dailyAgent, err := ir.Build(dailyPkg)
	if err != nil {
		t.Fatal(err)
	}
	dailyArtifact, err := Generate(dailyAgent, dailyAgent.Targets["pipecat"], target.Default())
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range dailyArtifact.Files {
		for _, marker := range markers {
			checkedDaily = checkedDaily || strings.Contains(string(file.Content), marker)
		}
	}
	// A test that finds the markers nowhere would pass while proving nothing.
	if !checkedDaily {
		t.Error("no example exercises the Daily route, so this test cannot tell scoped from absent")
	}
}

func TestPublicExamplePackages(t *testing.T) {
	root := filepath.Join("..", "..", "examples")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var directories []string
	for _, entry := range entries {
		if entry.IsDir() {
			if _, err := os.Stat(filepath.Join(root, entry.Name(), "agent.yaml")); err != nil {
				continue
			}
			directories = append(directories, entry.Name())
		}
	}
	// Seven packages: four structural, salon-concierge as the composite release
	// fixture that also carries the only shipped telephony route, and two that
	// emit no runnable project. The slng pair is deliberately a pair: support
	// references only builtins so its push creates nothing, and orders ships a
	// `local:` tool so the push has to create, run, publish and attach one. The
	// deploy walkthrough needs both, because the difference between them is the
	// thing it explains. The focused telephony, outbound, transfer, MCP and
	// regional examples were removed 2026-08-21; route guards remain against
	// internal test fixtures.
	want := []string{"multi-task", "salon-concierge", "simple-prompt", "slng-orders", "slng-support", "subagents", "task-groups"}
	if !slices.Equal(directories, want) {
		t.Fatalf("public example directories = %v, want %v", directories, want)
	}
	// Artifacts must not be *committed*; compiling an example locally is normal
	// and must not fail the suite, so ask git what is tracked rather than
	// walking the working tree (B5).
	tracked, err := exec.Command("git", "ls-files", "examples").Output()
	if err != nil {
		t.Skipf("git ls-files unavailable: %v", err)
	}
	for _, path := range strings.Split(strings.TrimSpace(string(tracked)), "\n") {
		if strings.Contains(path, "/build/") || strings.HasSuffix(path, ".DS_Store") {
			t.Errorf("forbidden committed example artifact: %s", path)
		}
	}
}

func TestRepositoryKeepsSpecsPrivateAndDocsFocused(t *testing.T) {
	repo := filepath.Join("..", "..")
	tracked, err := exec.Command("git", "-C", repo, "ls-files", "--", "specs", "docs").Output()
	if err != nil {
		t.Fatalf("list tracked specs and docs: %v", err)
	}
	// The allow-list is the point: a new file under docs/ is a deliberate
	// decision, not somewhere to park notes. Three earn their place. ARCHITECTURE
	// describes the system, and the other two are the two ways behaviour gets
	// checked: SELF_VERIFY without a caller, HARNESS_TEST with one.
	want := "docs/ARCHITECTURE.md\ndocs/HARNESS_TEST.md\ndocs/SELF_VERIFY.md"
	if got := strings.TrimSpace(string(tracked)); got != want {
		t.Errorf("tracked specs and docs = %q, want the focused allow-list %q", got, want)
	}
	if err := exec.Command("git", "-C", repo, "check-ignore", "-q", "--", "specs/.unmute-ignore-probe/spec.md").Run(); err != nil {
		t.Errorf("specs/ is not ignored: %v", err)
	}
}

func TestExampleToolExposure(t *testing.T) {
	domainTools := []string{"lookup_customer", "create_customer", "check_availability", "book_appointment", "cancel_appointment"}
	cases := []struct {
		name       string
		agentTools map[string][]string
		taskTools  map[string][]string
	}{
		{
			name:       "simple-prompt",
			agentTools: map[string][]string{"appointment_desk": domainTools},
		},
		{
			name:       "multi-task",
			agentTools: map[string][]string{"appointment_desk": {"check_customer", "manage_appointment"}},
			taskTools: map[string][]string{
				"customer_record":     {"lookup_customer", "create_customer"},
				"appointment_request": {"check_availability", "book_appointment", "cancel_appointment"},
			},
		},
		{
			name:       "task-groups",
			agentTools: map[string][]string{"appointment_desk": {"manage_appointment"}},
			taskTools: map[string][]string{
				"identify_customer":    {"lookup_customer", "create_customer"},
				"select_appointment":   {"check_availability"},
				"finalize_appointment": {"book_appointment", "cancel_appointment"},
			},
		},
		{
			name: "subagents",
			agentTools: map[string][]string{
				"booking_desk":        {"lookup_customer", "create_customer", "check_availability", "book_appointment", "to_appointment_manager"},
				"appointment_manager": {"lookup_customer", "create_customer", "check_availability", "book_appointment", "cancel_appointment", "to_booking_desk"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pkg, err := spec.Load(filepath.Join("..", "..", "examples", tc.name))
			if err != nil {
				t.Fatal(err)
			}
			agent, err := ir.Build(pkg)
			if err != nil {
				t.Fatal(err)
			}
			for name, want := range tc.agentTools {
				if got := agent.Agents[name].Tools; !slices.Equal(got, want) {
					t.Errorf("agent %q tools = %v, want %v", name, got, want)
				}
			}
			for name, want := range tc.taskTools {
				if got := agent.Tasks[name].Tools; !slices.Equal(got, want) {
					t.Errorf("task %q tools = %v, want %v", name, got, want)
				}
			}
		})
	}
}

// TestFixturePackagesValidate holds the internal fixtures to the same bar as the
// public examples (SPEC V14): safe_core and remy back most of the suite, so a
// fixture that stops validating would otherwise only surface as a confusing
// failure somewhere downstream.
func TestFixturePackagesValidate(t *testing.T) {
	root := filepath.Join("..", "testdata")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, entry.Name(), "agent.yaml")); err != nil {
			continue // not a package (golden dirs, handler fixtures)
		}
		t.Run(entry.Name(), func(t *testing.T) {
			pkg, err := spec.Load(filepath.Join(root, entry.Name()))
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			agent, err := ir.Build(pkg)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			var declared []ir.Target
			for _, name := range slices.Sorted(maps.Keys(agent.Targets)) {
				declared = append(declared, agent.Targets[name])
			}
			report, err := ir.Validate(agent, declared, target.Default())
			if err != nil {
				t.Fatalf("validate: %v\n%#v", err, report.PerTarget)
			}
		})
	}
}

// V16/B9: a committed example never ships a literal transfer destination. Any
// literal a repository can contain is a number nobody answers, so a live test
// of the example dials a stranger or a carrier intercept ("el número marcado
// no existe") and the call dies right where the demo should land. Every
// destination in examples/*/targets.yaml is an UPPER_SNAKE env var name,
// resolved from the tester's own environment at call time.
func TestV16_ExampleDestinationsAreEnvironmentNames(t *testing.T) {
	envName := regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	root := filepath.Join("..", "..", "examples")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, entry.Name(), "agent.yaml")); err != nil {
			continue
		}
		pkg, err := spec.Load(filepath.Join(root, entry.Name()))
		if err != nil {
			t.Fatalf("load %s: %v", entry.Name(), err)
		}
		for targetName, target := range pkg.Targets {
			for symbol, value := range target.Destinations {
				if !envName.MatchString(value) {
					t.Errorf("%s/%s destination %q = %q is a literal; committed examples must name an env var", entry.Name(), targetName, symbol, value)
				}
			}
		}
	}
}

func TestSalonConciergeTransferEnvironmentContract(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "salon-concierge"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range []ir.Provider{ir.ProviderPipecat, ir.ProviderLiveKit} {
		artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
		if err != nil {
			t.Fatalf("%s: %v", provider, err)
		}
		env := artifactFile(t, artifact, ".env.example")
		if !strings.Contains(env, "MANAGER_PHONE_NUMBER=") {
			t.Errorf("%s: .env.example does not require MANAGER_PHONE_NUMBER", provider)
		}
		if strings.Contains(env, "SUPERVISOR_PHONE_NUMBER=") {
			t.Errorf("%s: .env.example still accepts the old SUPERVISOR_PHONE_NUMBER alias", provider)
		}
	}
}

// US2 / FR-018: a phone package runs in the browser with nothing but model
// keys unless it exposes warm transfer there. The emitted startup check is
// where that is decided, so it is where it is asserted for packages whose
// browser tools do not dial through the route. The warm-transfer exception has
// its exact five-name contract in livekit_phone_env_test.go.
//
// This already worked and nothing wrote it down, which is what made it safe to
// move route resolution underneath it. It is the most common workflow in the
// project and it had no test.
func TestBrowserPathStartupCheckAsksForNoRouteEnvironment(t *testing.T) {
	routeOnly := []string{
		"TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN", "TWILIO_PHONE_NUMBER",
		"SIP_TRUNK_HOSTNAME", "SIP_AUTH_USERNAME", "SIP_AUTH_PASSWORD", "SIP_FROM_NUMBER",
		"UNMUTE_PUBLIC_URL", "UNMUTE_OUTBOUND_TOKEN", "REDIS_URL",
		"PIPECAT_CLOUD_ORGANIZATION", "MANAGER_PHONE_NUMBER",
	}
	for example, providers := range map[string][]ir.Provider{
		"salon-concierge": {ir.ProviderPipecat, ir.ProviderLiveKit},
	} {
		t.Run(example, func(t *testing.T) {
			pkg, err := spec.Load(filepath.Join("..", "..", "examples", example))
			if err != nil {
				t.Fatal(err)
			}
			agent, err := ir.Build(pkg)
			if err != nil {
				t.Fatal(err)
			}
			for _, provider := range providers {
				resolved := targetByProvider(t, agent, provider)
				if resolved.Telephony == nil {
					t.Fatalf("%s declares no route, so it is the wrong fixture", provider)
				}
				artifact, err := Generate(agent, resolved, target.Default())
				if err != nil {
					t.Fatalf("%s: %v", provider, err)
				}
				entry := "bot.py"
				if provider == ir.ProviderLiveKit {
					entry = "agent.py"
				}
				source := artifactFile(t, artifact, entry)
				start := strings.Index(source, "REQUIRED_ENV = [")
				if start < 0 {
					t.Fatalf("%s: %s has no REQUIRED_ENV startup check", provider, entry)
				}
				block := source[start : start+strings.Index(source[start:], "]")]
				for _, name := range routeOnly {
					if strings.Contains(block, `"`+name+`"`) {
						t.Errorf("%s: the browser path's startup check demands %q, so a package "+
							"that only wants a browser session cannot start:\n%s", provider, name, block)
					}
				}
			}
		})
	}
}

// The generated README is the runbook, and almost nobody reads it before they
// have already read the example's own page and the docs. So those two have to stay
// true on their own, and "stay true" is a thing a test can hold rather than a
// thing a person remembers.
//
// These two are deliberately narrow. They do not check prose: a document is free
// to say that an example was removed, which is history worth keeping. They check
// the two claims that go stale silently and mislead a reader who acts on them: a
// link that no longer resolves, and a README describing a route its package no
// longer declares.

// Every relative link an example page offers must resolve, and any link in the
// architecture or docs site that points into examples/ must resolve too. Deleting
// or renaming an example fails this until every page that sends a reader there is
// fixed.
func TestExampleAndDocLinksIntoExamplesResolve(t *testing.T) {
	link := regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)`)
	check := func(page string, onlyExamples bool) {
		raw, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range link.FindAllStringSubmatch(string(raw), -1) {
			target, _, _ := strings.Cut(match[1], "#")
			if target == "" || strings.HasPrefix(target, "http://") ||
				strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			if onlyExamples && !strings.Contains(target, "examples/") {
				continue
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(page), target)); err != nil {
				t.Errorf("%s links to %q, which does not exist", page, target)
			}
		}
	}
	for _, source := range []struct {
		root, extension string
		onlyExamples    bool
	}{
		{filepath.Join("..", "..", "examples"), ".md", false},
		{filepath.Join("..", "..", "docs"), ".md", true},
		{filepath.Join("..", "..", "docs-site"), ".mdx", true},
	} {
		err := filepath.WalkDir(source.root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() && entry.Name() == "build" {
				return fs.SkipDir
			}
			if !entry.IsDir() && strings.HasSuffix(path, source.extension) {
				check(path, source.onlyExamples)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// An example's own README must name every route its targets declare. This is the
// one that would have caught `twilio-telephony-hello` describing a carrier-websocket
// Pipecat target for the length of the feature that moved it to another route: the
// generated runbook was right the whole time, and the page a reader opens first
// was wrong.
func TestExampleReadmesNameTheirDeclaredTransports(t *testing.T) {
	root := filepath.Join("..", "..", "examples")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, entry.Name(), "agent.yaml")); err != nil {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			pkg, err := spec.Load(filepath.Join(root, entry.Name()))
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			// Read the route from the connections, which is where it is declared.
			// Reading it off the targets here, as this test did before feature
			// 008, would now find nothing on every example and pass without
			// checking anything (spec FR-027).
			var routed []string
			for name, connection := range pkg.Connections {
				if connection.Transport != "" {
					routed = append(routed, name)
				}
			}
			if len(routed) == 0 {
				// Nothing declares a route, so there is no route claim to keep true.
				// The four structural examples live in the index table in
				// examples/README.md and need no page of their own.
				return
			}
			readme, err := os.ReadFile(filepath.Join(root, entry.Name(), "README.md"))
			if err != nil {
				t.Fatalf("this example declares a route (%s) and has no README to describe it: %v", strings.Join(routed, ", "), err)
			}
			for name, connection := range pkg.Connections {
				if connection.Transport == "" {
					continue
				}
				if !strings.Contains(string(readme), connection.Transport) {
					t.Errorf("connection %q declares transport %q, which this example's README never mentions", name, connection.Transport)
				}
			}
		})
	}
}

// salonScopes is every cache scope the shipped example must produce: one per
// agent, one per task. Four agents and two tasks on one think profile is the
// shape that collided, so it is the shape worth naming here in full.
func salonScopes() []string {
	const id = "optimized-salon-concierge-v8"
	return []string{
		id + ":concierge",
		id + ":complaint_specialist",
		id + ":task.customer_verification",
		id + ":task.booking",
	}
}

// assertSalonScopes holds SC-008 on the package a reader opens: four distinct
// scopes on each target, the same four on both, each carrying the authored
// prefix, and the bare authored id sent by nobody.
//
// It was six. Two of them belonged to agents that existed only to hold a guard
// the compiler could not put on a step, and each one was a separate prompt site
// paying for its own cache.
func assertSalonScopes(t *testing.T, resolved *ir.Agent) {
	t.Helper()
	value := regexp.MustCompile(`"X-Slng-Agent-Id": "([^"]*)"|_slng_scope = "([^"]*)"`)
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		artifact, err := Generate(resolved, targetByProvider(t, resolved, provider), target.Default())
		if err != nil {
			t.Fatalf("%s: generate: %v", provider, err)
		}
		found := map[string]bool{}
		for _, file := range artifact.Files {
			if !strings.HasSuffix(file.Path, ".py") {
				continue
			}
			for _, match := range value.FindAllStringSubmatch(string(file.Content), -1) {
				found[match[1]+match[2]] = true
			}
		}
		for _, want := range salonScopes() {
			if !found[want] {
				t.Errorf("%s: the example sends no scope %q; found %v", provider, want, found)
			}
			delete(found, want)
		}
		for leftover := range found {
			t.Errorf("%s: the example sends unexpected scope %q; the four are %v", provider, leftover, salonScopes())
		}
	}
}

// The gate the four documentation surfaces never had. FR-047 and SC-005 are prose
// claims, and prose is where this feature's reversal is easiest to half-finish: a
// reader who lands on a stale line is told the opposite of what the compiler does.
// A human read still happens, because prose rots in ways no grep catches. This
// catches the one thing a grep can.
func TestRouterScopeSurfacesDoNotContradictTheCompiler(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, surface := range []string{
		filepath.Join("internal", "generate", "templates", "livekit_v1", "README.md.tmpl"),
		filepath.Join("internal", "generate", "templates", "pipecat_v1", "README.md.tmpl"),
		// The emitted modules themselves. They carried the same claim in a
		// module-level comment, and leaving it out of this list is how that one
		// survived the first pass of the rewrite: a reader in the code sees it
		// long before they open a runbook.
		filepath.Join("internal", "generate", "templates", "livekit_v1", "agent.py.tmpl"),
		filepath.Join("internal", "generate", "templates", "pipecat_v1", "bot.py.tmpl"),
		filepath.Join("examples", "salon-concierge", "README.md"),
		filepath.Join("docs-site", "optimization", "context-router.mdx"),
		filepath.Join("internal", "skill", "assets", "references", "models.md"),
	} {
		body, err := os.ReadFile(filepath.Join(root, surface))
		if err != nil {
			t.Fatalf("%s: %v", surface, err)
		}
		text := string(body)
		// The two claims this feature reversed. Both were true of every surface
		// before it, and either one left standing tells a reader the opposite of
		// what the compiler now does.
		for _, stale := range []string{
			"one value for this whole project",
			"one value for this whole package",
			"never composes it out of an agent",
			"never compose it from an agent",
		} {
			if strings.Contains(text, stale) {
				t.Errorf("%s still says %q; the scope is now composed per agent and per task", surface, stale)
			}
		}
		// And the positive half: a surface that talks about the header at all has
		// to say the scope is per site, or a reader learns only half of it.
		if !strings.Contains(text, target.SlngAgentIDHeader) && !strings.Contains(text, "agent_id") {
			continue
		}
		if !strings.Contains(text, "task.") {
			t.Errorf("%s describes the cache scope and never mentions the task. prefix, so a reader does not learn that a task has its own scope", surface)
		}
	}
}

// FR-010. The four surfaces carry the two facts this feature adds, and the skill
// is in the list because it is the surface with no other reader.
//
// A prose gate is a weak gate and this one knows it: a person still has to read
// these files, because a surface can name a thing and describe it wrongly. What
// it catches is the specific way this change half-lands. The compiler surfaces
// have their own gates and the docs do not, so a rewrite that updates the runbook
// and forgets the skill leaves every coding agent writing packages that render a
// per-call name into the prompt text, which is the exact thing this feature
// exists to stop, and nothing anywhere fails.
func TestRouterSurfacesCarryThePlaceholderAndProvenanceFacts(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, surface := range []struct {
		path  string
		facts []string
	}{
		{filepath.Join("internal", "generate", "templates", "livekit_v1", "README.md.tmpl"), []string{"slng router: ", "every request"}},
		{filepath.Join("internal", "generate", "templates", "pipecat_v1", "README.md.tmpl"), []string{"slng router: ", "refreshed whenever the call writes one"}},
		{filepath.Join("examples", "salon-concierge", "README.md"), []string{"slng router: ", "{{customer_phone}}"}},
		{filepath.Join("docs-site", "optimization", "context-router.mdx"), []string{"slng router: ", "read again for every request"}},
		// The skill, which is what a coding assistant reads before it writes a
		// package. Both facts, because it is the only surface that decides what
		// gets authored in the first place.
		{filepath.Join("internal", "skill", "assets", "references", "models.md"), []string{"slng router: ", "as a placeholder, not into the prompt"}},
	} {
		body, err := os.ReadFile(filepath.Join(root, surface.path))
		if err != nil {
			t.Fatalf("%s: %v", surface.path, err)
		}
		for _, fact := range surface.facts {
			if !strings.Contains(string(body), fact) {
				t.Errorf("%s does not carry %q; a fact only some surfaces state is a fact a reader is told the opposite of somewhere else", surface.path, fact)
			}
		}
	}
}

// FR-006 and FR-013 on the package a reader opens, plus the one thing the scope
// list above cannot see: that the constant in this file still describes the
// package rather than a version of it that has moved on.
//
// The example is where an author learns what a placeholder is for, so three
// claims have to hold together. Its prompts reference only names it declares,
// because a name the router is not given is a 422 mid-call. Its spoken per-call
// value is a placeholder, because that is the whole demonstration. And the value
// that is never spoken stays out of every prompt: putting an identifier in a
// placeholder widens what the router is asked to substitute and buys nothing.
func TestSalonConciergePlaceholdersAgreeWithItsVariables(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "salon-concierge"))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}

	// FR-013. The scope list in this file names an id; the package authors one.
	// When a prompt change bumps the id, this is what makes updating both a
	// single deliberate step instead of a silent divergence.
	authored := resolved.Models["reasoning"].AgentID
	if want := strings.TrimSuffix(salonScopes()[0], ":concierge"); authored != want {
		t.Errorf("the package authors agent_id %q and this file's scope list expects %q. Bumping the id is correct when a prompt changes, so update salonScopes() in the same commit", authored, want)
	}

	// Every referenced name is declared. ir.Validate holds this too; here it is
	// on the shipped package rather than a fixture.
	for kind, bodies := range map[string]map[string]string{
		"agent": promptBodies(resolved.Agents),
		"task":  taskBodies(resolved.Tasks),
	} {
		for name, body := range bodies {
			for _, ref := range ir.TemplateRefs(body) {
				if _, declared := resolved.Variables[ref]; !declared {
					t.Errorf("%s %q references {{%s}}, which the package does not declare: the router answers an unsupplied name with a 422 mid-call", kind, name, ref)
				}
			}
		}
	}

	// The spoken value is a placeholder somewhere, or the example demonstrates
	// nothing. The silent one is a placeholder nowhere.
	all := strings.Join(append(mapValues(promptBodies(resolved.Agents)), mapValues(taskBodies(resolved.Tasks))...), "\n")
	if !strings.Contains(all, "{{customer_phone}}") {
		t.Error("no prompt in the example uses {{customer_phone}}, so every answer that says the number is still refused by the router's number rule and the example teaches nothing about caching one")
	}
	// The format rule is the feature, not decoration. Measured against the live
	// EU router on 2026-08-24, three reads per arm on fresh throwaway scopes:
	// values written "555 070 1222" came back echoed character for character and
	// the third read was served from cache in 109ms; the same numbers written
	// "+15550707444" were reformatted by the model, so the value never appeared
	// in the answer, and none of the three reads was served. The router puts the
	// placeholder back only where the answer holds the value exactly, so a
	// description that does not pin the shape costs every number-bearing turn
	// its cache, with no error anywhere to notice.
	phone, declared := resolved.Variables["customer_phone"]
	if !declared {
		t.Fatal("the package declares no customer_phone")
	}
	for _, want := range []string{"single spaces", "character for character"} {
		if !strings.Contains(phone.Description, want) {
			t.Errorf("customer_phone description omits %q: without the format rule the value is reformatted when spoken and the turn silently stops caching", want)
		}
	}
	if strings.Contains(all, "{{customer_id}}") {
		t.Error("a prompt uses {{customer_id}}, which the package's own description says is never spoken. An identifier in a placeholder widens what the router substitutes and buys no cache hit")
	}
}

// promptBodies and taskBodies flatten the two prompt-bearing maps to name and
// body, which is all the assertions above read.
func promptBodies(agents map[string]ir.AgentDef) map[string]string {
	out := make(map[string]string, len(agents))
	for name, def := range agents {
		out[name] = def.Instructions
	}
	return out
}

func taskBodies(tasks map[string]ir.Task) map[string]string {
	out := make(map[string]string, len(tasks))
	for name, task := range tasks {
		out[name] = task.Instructions
	}
	return out
}

func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

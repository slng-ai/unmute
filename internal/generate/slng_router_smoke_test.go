//go:build smoke

package generate

import (
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
)

// L4 smoke for the SLNG Context Router's two run-time-only helpers. A golden can
// prove they were emitted and pin their text; only running them proves what they
// do, so gates.md puts both here (FR-018, FR-034f).
//
// No network: nothing here calls the router. The vertex helper reads a variable
// and the truncation helper reads a state object, which is the whole of what
// there is to check.

const slngHelpersSmokeScript = `"""Smoke check: the router's vertex credential and truncation helpers."""
import base64
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")
os.environ["REDIS_URL"] = "redis://127.0.0.1:6379/0"

import bot  # noqa: E402

# --- the vertex credential, three accepted shapes and one refusal -------------
key = {"type": "service_account", "project_id": "smoke", "private_key_id": "abc"}
raw = json.dumps(key)

os.environ["SMOKE_GCP_KEY"] = raw
assert bot._slng_vertex_credentials("SMOKE_GCP_KEY") == key, "json shape"

os.environ["SMOKE_GCP_KEY"] = base64.b64encode(raw.encode()).decode()
assert bot._slng_vertex_credentials("SMOKE_GCP_KEY") == key, "base64 shape"

with open("smoke-key.json", "w") as handle:
    handle.write(raw)
os.environ["SMOKE_GCP_KEY"] = os.path.abspath("smoke-key.json")
assert bot._slng_vertex_credentials("SMOKE_GCP_KEY") == key, "path shape"

# The order matters: a base64 value can begin with "/" and look like a path, so
# base64 is tried before the filesystem.
os.environ["SMOKE_GCP_KEY"] = "/tmp/definitely-not-a-key-file"
try:
    bot._slng_vertex_credentials("SMOKE_GCP_KEY")
except RuntimeError as failure:
    text = str(failure)
    for phrase in ("key JSON", "base64", "path to the key file"):
        assert phrase in text, (phrase, text)
else:
    raise AssertionError("a malformed credential was accepted")

# --- the 4000 character ceiling ----------------------------------------------
# An over-long value truncates and the call continues. It must never raise:
# ending a live call over a variable is the one thing FR-018 forbids.
class _State:
    pass


state = _State()
limit = bot._SLNG_VARIABLE_LIMIT
assert limit == 4000, limit

state.short = "Aurora Salon"
assert bot._slng_template_variables(state, ("short",)) == {"short": "Aurora Salon"}

state.exact = "x" * limit
assert len(bot._slng_template_variables(state, ("exact",))["exact"]) == limit

state.over = "y" * (limit + 321)
assert len(bot._slng_template_variables(state, ("over",))["over"]) == limit

# A name with no value sends the same plain wording used in prompts.
values = bot._slng_template_variables(state, ("never_set",))
assert values["never_set"] == "none recorded yet.", repr(values["never_set"])
assert bot._slng_template_variables(None, ("never_set",))["never_set"] == "none recorded yet."

# --- the provenance hook ------------------------------------------------------
# Emitted, and run. A golden proves the line was written; only calling it proves
# what the line says, and this is the one helper whose failure mode is silence.
import asyncio


class _Headers(dict):
    """Just enough of httpx's header mapping: case-insensitive get and [] ."""

    def __init__(self, pairs):
        super().__init__({k.lower(): v for k, v in pairs.items()})

    def get(self, key, default=None):
        return super().get(key.lower(), default)

    def __getitem__(self, key):
        return super().__getitem__(key.lower())


class _Request:
    def __init__(self, headers):
        self.headers = _Headers(headers)


class _Response:
    def __init__(self, request_headers, headers):
        self.request = _Request(request_headers)
        self.headers = _Headers(headers)
        # Reading either of these would consume the stream the framework is about
        # to iterate, so touching them at all is the defect.
        self.text = property(lambda self: 1 / 0)

    def read(self):
        raise AssertionError("the hook read the response body, which is not there yet")


lines = []


class _Log:
    def info(self, message):
        lines.append(message)

    def debug(self, *args, **kwargs):
        pass


real_logger, bot.logger = bot.logger, _Log()
try:
    scope = "smoke-router-v1:concierge"
    # A cache hit: layer present, model absent.
    asyncio.run(bot._slng_log_provenance(_Response(
        {"X-Slng-Agent-Id": scope},
        {"x-slng-response-source": "cache", "x-slng-cache-layer": "l2_exact",
         "x-slng-request-id": "req_smoke_1"},
    )))
    # A live answer: model present, layer absent.
    asyncio.run(bot._slng_log_provenance(_Response(
        {"X-Slng-Agent-Id": scope},
        {"x-slng-response-source": "llm", "x-slng-model": "gpt-5.6-luna",
         "x-slng-request-id": "req_smoke_2"},
    )))
    # No scope header: not a router think request, so no line and no error.
    asyncio.run(bot._slng_log_provenance(_Response({}, {"x-slng-request-id": "req_smoke_3"})))
    # And a response that would raise on any header read: the hook swallows it,
    # because a log line must never end a call.
    class _Hostile:
        request = property(lambda self: 1 / 0)

    asyncio.run(bot._slng_log_provenance(_Hostile()))
finally:
    bot.logger = real_logger

assert len(lines) == 2, lines
assert lines[0] == (
    "slng router: scope=" + scope + " source=cache layer=l2_exact request_id=req_smoke_1"
), lines[0]
assert lines[1] == (
    "slng router: scope=" + scope + " source=llm model=gpt-5.6-luna request_id=req_smoke_2"
), lines[1]

print("slng router helpers ok")
`

// TestSmokeSlngRouterHelpers compiles the router example with a vertex upstream,
// because the vertex helper is emitted only when an upstream needs it, and drives
// both helpers on the real SDK.
func TestSmokeSlngRouterHelpers(t *testing.T) {
	runPipecatSmokeScript(t, "salon-concierge", nil, func(agent *ir.Agent) {
		// Swap the openai upstream for a vertex one so the credential helper is
		// emitted. The example ships openai, which is the case a reader copies;
		// this is the case only smoke can exercise.
		for name, target := range agent.Targets {
			for profile, binding := range target.Models.Reason {
				if !binding.Router() {
					continue
				}
				binding.Upstream = &ir.Upstream{
					Provider: "vertex", CredentialsEnv: "SMOKE_GCP_KEY", Location: "europe-west4",
				}
				target.Models.Reason[profile] = binding
			}
			agent.Targets[name] = target
		}
		agent.Secrets = append(agent.Secrets, "SMOKE_GCP_KEY")
		// The entry agent's prompt references no variable of its own: the example
		// puts {{customer_name}} on the three specialists, because the concierge
		// speaks before anyone has offered a name. The snapshot helper is emitted
		// either way, and this makes the entry prompt exercise it too.
		entry := agent.Agents[agent.EntryAgent]
		entry.Instructions += "\n\nThe caller is {{customer_name}}."
		agent.Agents[agent.EntryAgent] = entry
	}, slngHelpersSmokeScript)
}

func scopedRouterFixture(agent *ir.Agent) {
	for name, tgt := range agent.Targets {
		for profile := range tgt.Models.Reason {
			tgt.Models.Reason[profile] = ir.Binding{Provider: ir.ProviderSlngRouter, AgentID: "scoped", Model: "test", Params: map[string]any{"world_part_override": "eu"}, Upstream: &ir.Upstream{Provider: "openai"}}
		}
		agent.Targets[name] = tgt
	}
	desk := agent.Agents[agent.EntryAgent]
	desk.Instructions = ir.FlattenPaths("Selected {{last_appointment.scheduled_date}}")
	agent.Agents[agent.EntryAgent] = desk
	task := agent.Tasks["book"]
	task.Instructions = "Private {{caller_reason}}"
	agent.Tasks["book"] = task
	observer := agent.Tasks["record_flags"]
	observer.Instructions = "Phone {{caller_phone}}"
	agent.Tasks["record_flags"] = observer
	task = agent.Tasks["no_output"]
	task.Instructions = "No saved values."
	agent.Tasks["no_output"] = task
}

func TestSmokeRouterActiveScopeLiveKit(t *testing.T) {
	runLiveKitSmokeScript(t, "typed_state", nil, scopedRouterFixture, scopedRouterScript("agent", "Userdata()"))
}
func TestSmokeRouterActiveScopePipecat(t *testing.T) {
	runPipecatSmokeScript(t, "typed_state", nil, scopedRouterFixture, scopedRouterScript("bot", "build_state()"))
}
func scopedRouterScript(module, state string) string {
	return `import os, json, asyncio
from types import SimpleNamespace
for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")
import ` + module + ` as generated
state = generated.` + state + `
state.last_appointment = {"scheduled_date":"2026-09-11", "scheduled_time":"PRIVATE_SIBLING", "appointment_type":"PRIVATE_SERVICE"}
state.caller_reason = ["PRIVATE_OTHER_TASK"]
scopes = generated._SLNG_TEMPLATE_PATHS
selected = next(scope for scope, refs in scopes.items() if refs == ["last_appointment__scheduled_date"])
private = next(scope for scope, refs in scopes.items() if refs == ["caller_reason"])
empty = next(scope for scope, refs in scopes.items() if not refs)
async def request(scope):
    if generated.__name__ == "bot":
        base = generated.OpenAILLMService.build_chat_completion_params
        generated.OpenAILLMService.build_chat_completion_params = lambda self, ctx: {"extra_headers":{"X-Slng-Agent-Id":scope}, "extra_body":{"template_variables":{"stale":"PRIVATE_STALE"}}}
        try:
            worker = object.__new__(generated._SlngRouterLLMService)
            worker._slng_state = state
            return worker.build_chat_completion_params({})["extra_body"]
        finally:
            generated.OpenAILLMService.build_chat_completion_params = base
    captured = {}
    class Stream:
        async def __aenter__(self): return self
        async def __aexit__(self, *args): pass
        def __aiter__(self): return self
        async def __anext__(self): raise StopAsyncIteration
    def chat(**kwargs):
        captured.update(kwargs)
        return Stream()
    session = SimpleNamespace(userdata=state, llm=SimpleNamespace(chat=chat), conn_options=SimpleNamespace(llm_conn_options=None))
    active = SimpleNamespace(session=session, llm=None, _slng_scope=scope)
    async for _ in generated._slng_llm_node(active, [], [], None): pass
    return captured["extra_kwargs"]["extra_body"]
async def check():
    for scope, expected in [(private,{"caller_reason":'["PRIVATE_OTHER_TASK"]'}), (selected,{"last_appointment__scheduled_date":"2026-09-11"}), (empty,{})]:
        body = await request(scope)
        assert body["template_variables"] == expected, body
    state.last_appointment["scheduled_date"] = "2026-09-12"
    assert (await request(selected))["template_variables"] == {"last_appointment__scheduled_date":"2026-09-12"}
    state.last_appointment = None
    assert (await request(selected))["template_variables"] == {"last_appointment__scheduled_date":"none recorded yet."}
    state.caller_phone = "+34600111222"
    confirm = next(scope for scope, site in generated._SLNG_SCOPE_SITES.items() if site == "task:confirm_number")
    observer = next(scope for scope, site in generated._SLNG_SCOPE_SITES.items() if site == "task:record_flags")
    assert (await request(confirm))["template_variables"]["caller_phone"] == "+34600111222"
    assert (await request(observer))["template_variables"] == {"caller_phone":"none recorded yet."}
    generated._save_result("confirm_number",state,{"caller_phone":"+34600111222"})
    assert (await request(observer))["template_variables"] == {"caller_phone":"+34600111222"}
asyncio.run(check())
print("active router request: selected field only; sibling, other-task and stale values absent; empty scope clears; next request refreshes")
`
}

func historyBoundaryFixture(agent *ir.Agent) {
	for name, history := range map[string]ir.History{"book": ir.HistoryReset, "record_flags": ir.HistoryMessages, "no_output": ir.HistoryFull} {
		task := agent.Tasks[name]
		task.Context.History = history
		agent.Tasks[name] = task
	}
}
func TestSmokeHistoryEntryLiveKit(t *testing.T) {
	runLiveKitSmokeScript(t, "typed_state", nil, historyBoundaryFixture, historyEntryScript("agent", "Userdata()"))
}
func TestSmokeHistoryEntryPipecat(t *testing.T) {
	runPipecatSmokeScript(t, "typed_state", nil, historyBoundaryFixture, historyEntryScript("bot", "build_state()"))
}
func historyEntryScript(module, state string) string {
	return `import os, json, asyncio
from types import SimpleNamespace
for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name,"smoke-placeholder")
import ` + module + ` as generated
state=generated.` + state + `
state.count=7
messages=[{"role":"system","content":"PRIVATE_OLD_INSTRUCTIONS"},
 {"role":"user","content":"SPOKEN_CALLER"},
 {"role":"assistant","content":"SPOKEN_ASSISTANT","tool_calls":[{"id":"old-call","type":"function","function":{"name":"old_tool","arguments":"{}"}}]},
 {"role":"tool","tool_call_id":"old-call","content":"TOOL_REPLY"}]
def assert_policy(policy,text):
    assert "PRIVATE_OLD_INSTRUCTIONS" not in text,text
    if policy=="reset": assert "SPOKEN_" not in text and "old_tool" not in text and "TOOL_REPLY" not in text,text
    else:
        assert "SPOKEN_CALLER" in text and "SPOKEN_ASSISTANT" in text,text
        assert ("old_tool" in text)==(policy=="full"),text
        assert ("TOOL_REPLY" in text)==(policy=="full"),text
async def check():
    if generated.__name__=="bot":
        for name,policy in [("book","reset"),("record_flags","messages"),("no_output","full")]:
            context=generated.LLMContext(messages=[dict(m) for m in messages])
            worker=SimpleNamespace(context=context,state=state)
            setattr(worker,"_"+name+"_finish_"+name,lambda *args:None)
            worker._bind_state=lambda handler:handler
            node=getattr(generated.DeskAgent,"_"+name+"_node_"+name)(worker)
            assert_policy(policy,json.dumps(context.get_messages()))
            assert "PRIVATE_OLD_INSTRUCTIONS" not in node["role_message"]
        return
    from livekit.agents import llm
    from livekit.agents.beta.workflows import TaskGroup
    def source():
        ctx=llm.ChatContext()
        ctx.add_message(role="system",content="PRIVATE_OLD_INSTRUCTIONS")
        ctx.add_message(role="user",content="SPOKEN_CALLER")
        ctx.add_message(role="assistant",content="SPOKEN_ASSISTANT")
        ctx.insert([llm.FunctionCall(name="old_tool",call_id="old-call",arguments="{}"),llm.FunctionCallOutput(name="old_tool",call_id="old-call",output="TOOL_REPLY",is_error=False)])
        return ctx
    for cls,policy in [(generated.Book,"reset"),(generated.RecordFlags,"messages"),(generated.NoOutput,"full")]:
        observed=[]
        class Probe(cls):
            @property
            def session(self): return SimpleNamespace(userdata=state,generate_reply=lambda **kw:None)
            def __await__(self):
                async def run():
                    await self.on_enter()
                    observed.append(json.dumps(self.chat_ctx.to_dict()))
                    return {}
                return run().__await__()
        task=Probe(chat_ctx=source())
        await task.on_enter()
        assert_policy(policy,json.dumps(task.chat_ctx.to_dict()))
        # The native group replaces its factory's initial context before entry.
        group=TaskGroup(chat_ctx=source(),summarize_chat_ctx=False)
        await group.update_chat_ctx(source(),exclude_invalid_function_calls=False)
        group.add(lambda:Probe(chat_ctx=llm.ChatContext()),id="step",description="Check policy")
        await group.on_enter()
        assert_policy(policy,observed[-1])
        isolated=Probe(chat_ctx=llm.ChatContext())
        await isolated.on_enter()
        assert "SPOKEN_" not in json.dumps(isolated.chat_ctx.to_dict())
    assert state.count==7
asyncio.run(check())
print("native history entry: reset/messages/full and group replacement obey receiver policy; state retained")
`
}

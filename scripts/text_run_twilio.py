"""Drive a compiled twilio target's app.py as ConversationRelay would, with no call.

The app runs in-process on the app's own uvicorn server, on a free local port.
A fake ConversationRelay client, on aiohttp, signs its requests with a fake auth token, the way Twilio signs them, and
speaks the WebSocket protocol:

    https://www.twilio.com/docs/voice/conversationrelay/websocket-messages

--fake scripts the model at the SDK boundary, with the SDK's own response types,
so every case is offline and deterministic. It checks signatures, setup order,
streaming and `last`, interruption, tools, limits and shutdown.

--real uses the real model and the real key from the repository's .env, and
checks a streamed reply, a tool follow-up and the next turn after an interrupt.
On Gemini it also replays a tool follow-up with the thought signatures stripped,
which the API must refuse.

    unmute compile internal/voice-agents-tests/relay-desk
    uv run --project internal/voice-agents-tests/relay-desk/build/twilio-openai \
        python scripts/text_run_twilio.py internal/voice-agents-tests/relay-desk/build/twilio-openai --fake

No real Twilio call is made, so nothing here proves carrier behaviour.
"""

from __future__ import annotations

import argparse
import asyncio
import contextlib
import copy
import json
import os
import sys
import threading
import time
from pathlib import Path
from types import SimpleNamespace
from typing import Any

ACCOUNT = "AC" + "a" * 32
TOKEN = "fake-auth-token"
ORIGIN = "https://relay.example.com"
HOST = "relay.example.com"
CALL = "CA" + "b" * 32
SESSION = "VX" + "c" * 32
REPO = Path(__file__).resolve().parent.parent


# --- script steps --------------------------------------------------------------
# A model response is a list of steps: text, a tool call, a signed empty tail
# (what Gemini 3 ends a turn with), a gate to pause on, or an error to raise.


class Call(SimpleNamespace):
    pass


def call(name: str, args: dict[str, Any], id: str = "call_1") -> Call:
    return Call(name=name, args=args, id=id)


TAIL = object()


class FakeOpenAI:
    def __init__(self, script: list[list[Any]]) -> None:
        self.script = list(script)
        self.requests: list[list[dict[str, Any]]] = []
        self.raw: list[dict[str, Any]] = []  # every other request field, as sent
        self.chat = SimpleNamespace(completions=SimpleNamespace(create=self.create))

    async def create(self, **request: Any) -> Any:
        from openai.types.chat import ChatCompletionChunk

        self.requests.append(copy.deepcopy(request["messages"]))
        self.raw.append({k: copy.deepcopy(v) for k, v in request.items() if k != "messages"})
        steps = self.script.pop(0) if self.script else []

        def chunk(delta: dict[str, Any]) -> Any:
            return ChatCompletionChunk.model_validate(
                {
                    "id": "chunk",
                    "object": "chat.completion.chunk",
                    "created": 0,
                    "model": "fake",
                    "choices": [{"index": 0, "delta": delta, "finish_reason": None}],
                }
            )

        class Stream:
            def __aiter__(self) -> Any:
                return self.generate()

            async def generate(self) -> Any:
                index = 0
                for step in steps:
                    if isinstance(step, asyncio.Event):
                        await step.wait()
                    elif isinstance(step, Exception):
                        raise step
                    elif isinstance(step, Call):
                        yield chunk(
                            {
                                "tool_calls": [
                                    {
                                        "index": index,
                                        "id": step.id,
                                        "type": "function",
                                        "function": {"name": step.name, "arguments": json.dumps(step.args)},
                                    }
                                ]
                            }
                        )
                        index += 1
                    elif step is not TAIL:
                        yield chunk({"content": step})

            async def close(self) -> None:
                pass

        return Stream()


class FakeGemini:
    def __init__(self, script: list[list[Any]]) -> None:
        self.script = list(script)
        self.requests: list[list[Any]] = []
        self.aio = SimpleNamespace(models=SimpleNamespace(generate_content_stream=self.stream))

    async def stream(self, model: str, contents: list[Any], config: Any) -> Any:
        from google.genai import types

        self.requests.append([content.model_copy(deep=True) for content in contents])
        steps = self.script.pop(0) if self.script else []

        def chunk(part: dict[str, Any]) -> Any:
            return types.GenerateContentResponse.model_validate(
                {"candidates": [{"content": {"role": "model", "parts": [part]}}]}
            )

        async def generate() -> Any:
            for step in steps:
                if isinstance(step, asyncio.Event):
                    await step.wait()
                elif isinstance(step, Exception):
                    raise step
                elif isinstance(step, Call):
                    function_call = {"name": step.name, "args": step.args}
                    if step.id:
                        function_call["id"] = step.id
                    yield chunk({"function_call": function_call, "thought_signature": b"sig-" + step.name.encode()})
                elif step is TAIL:
                    yield chunk({"text": "", "thought_signature": b"tail"})
                else:
                    yield chunk({"text": step})

        return generate()


# --- the harness ---------------------------------------------------------------


class Relay:
    """A fake ConversationRelay client on one WebSocket."""

    def __init__(self, ws: Any) -> None:
        self.ws = ws

    async def send(self, message: dict[str, Any]) -> None:
        await self.ws.send_json(message)

    async def setup(self, account: str = ACCOUNT, custom: dict[str, str] | None = None) -> None:
        await self.send(
            {"type": "setup", "sessionId": SESSION, "accountSid": account, "callSid": CALL,
             "from": "+15005550006", "to": "+15005550001", "direction": "inbound", "customParameters": custom or {}}
        )

    async def prompt(self, text: str, last: bool = True) -> None:
        await self.send({"type": "prompt", "voicePrompt": text, "lang": "en-US", "last": last})

    async def interrupt(self, heard: str) -> None:
        await self.send({"type": "interrupt", "utteranceUntilInterrupt": heard, "durationUntilInterruptMs": 400})

    async def next(self, timeout: float = 3.0) -> dict[str, Any] | None:
        from aiohttp import WSMsgType

        message = await asyncio.wait_for(self.ws.receive(), timeout)
        if message.type != WSMsgType.TEXT:
            return None
        return json.loads(message.data)

    async def reply(self, timeout: float = 3.0) -> list[dict[str, Any]]:
        """Every text message up to and including the one with last=true."""
        tokens = []
        while True:
            message = await self.next(timeout)
            if message is None:
                raise AssertionError("socket closed before last=true")
            tokens.append(message)
            if message.get("type") != "text" or message.get("last"):
                return tokens

    async def stays_open(self, secs: float = 0.3) -> bool:
        """True when the app leaves the socket open, as it must after an end."""
        try:
            await self.next(secs)
        except TimeoutError:
            return True
        return not self.ws.closed

    async def quiet(self, secs: float = 0.3) -> list[dict[str, Any]]:
        got = []
        with contextlib.suppress(TimeoutError):
            while True:
                message = await self.next(secs)
                if message is None:
                    break
                got.append(message)
        return got


def text_of(tokens: list[dict[str, Any]]) -> str:
    return "".join(t["token"] for t in tokens if t.get("type") == "text")


def check(condition: bool, message: str) -> None:
    if not condition:
        raise AssertionError(message)


async def until(predicate: Any, timeout: float = 3.0) -> None:
    deadline = time.monotonic() + timeout
    while not predicate():
        if time.monotonic() > deadline:
            raise AssertionError("timed out waiting for a condition")
        await asyncio.sleep(0.01)


class Env:
    """One app on a test server, with the model faked or real."""

    def __init__(self, app: Any, fake: Any, patches: dict[str, Any]) -> None:
        self.app = app
        self.fake = fake
        self.patches = patches
        self.calls: list[Any] = []
        self.slots: list[Any] = []
        self.server: Any = None
        self.client: Any = None

    async def __aenter__(self) -> Env:
        import aiohttp

        self.saved = {name: getattr(self.app, name) for name in self.patches}
        for name, value in self.patches.items():
            setattr(self.app, name, value)
        env = self

        class RecordingCall(self.saved.get("Call", self.app.Call)):  # type: ignore[misc]
            def __init__(self, *args: Any, **kwargs: Any) -> None:
                super().__init__(*args, **kwargs)
                env.calls.append(self)

        class RecordingSlots(self.app.Slots):  # type: ignore[misc]
            def __init__(self, *args: Any, **kwargs: Any) -> None:
                super().__init__(*args, **kwargs)
                env.slots.append(self)

        self.original = (self.app.Call, self.app.Slots)
        self.app.Call, self.app.Slots = RecordingCall, RecordingSlots
        asgi = self.app.build_app(client=self.fake)
        self.server = self.app.RelayServer(self.app.server_config(asgi, "127.0.0.1", 0), asgi.state.drain_calls)
        self.serving = asyncio.ensure_future(self.server.serve())
        await until(lambda: self.server.started or self.serving.done(), 10)
        port = self.server.servers[0].sockets[0].getsockname()[1]
        self.client = aiohttp.ClientSession(base_url=f"http://127.0.0.1:{port}")
        return self

    async def stop(self) -> None:
        """Shut the server down the way SIGTERM does: drain first."""
        self.server.should_exit = True
        await self.serving

    async def __aexit__(self, *exc: Any) -> None:
        await self.stop()
        await self.client.close()
        self.app.Call, self.app.Slots = self.original
        for name, value in self.saved.items():
            setattr(self.app, name, value)

    def sign(self, url: str, params: dict[str, Any] | None = None) -> str:
        from twilio.request_validator import RequestValidator

        return RequestValidator(TOKEN).compute_signature(url, params or {})

    async def post(self, path: str, form: dict[str, str], signed_url: str | None = None, sig: str | None = None) -> Any:
        signature = sig if sig is not None else self.sign(signed_url or ORIGIN + path, form)
        return await self.client.post(path, data=form, headers={"X-Twilio-Signature": signature})

    async def relay(self, path: str = "/conversation", signed_url: str | None = None, setup: bool = True) -> Relay:
        signature = self.sign(signed_url or f"wss://{HOST}{path}")
        ws = await self.client.ws_connect(path, headers={"X-Twilio-Signature": signature})
        relay = Relay(ws)
        if setup:
            await relay.setup()
            await until(lambda: len(self.calls) > 0)
        return relay

    def history(self) -> list[Any]:
        return self.calls[-1].brain.history

    async def next_step(self, handoff: str, status: str = "ended") -> str:
        """Ask /connect-action what comes next, as Twilio does after an end."""
        form = {"AccountSid": ACCOUNT, "CallSid": CALL, "From": "+15005550006", "To": "+15005550001",
                "Direction": "inbound", "SessionStatus": status, "HandoffData": handoff}
        response = await self.post("/connect-action", form)
        check(response.status == 200, f"/connect-action returned {response.status}")
        return await response.text()

    async def resumed(self, note: str, greeting: str = "") -> Relay:
        """The second session Twilio opens for a resume TwiML."""
        count = len(self.calls)
        relay = await self.relay(setup=False)
        await relay.setup(custom={"resume": note, **({"greeting": greeting} if greeting else {})})
        await until(lambda: len(self.calls) > count)
        return relay


def gemini(app: Any) -> bool:
    return not hasattr(app, "AsyncOpenAI")


def roles(app: Any, history: list[Any]) -> list[str]:
    if gemini(app):
        return [entry.role for entry in history]
    return [entry["role"] for entry in history]


def texts(app: Any, history: list[Any]) -> list[str]:
    """Each entry's plain text, in order."""
    if gemini(app):
        return ["".join(p.text or "" for p in entry.parts if not p.function_response) for entry in history]
    return [entry.get("content") or "" for entry in history]


def tool_results(app: Any, history: list[Any]) -> list[Any]:
    if gemini(app):
        return [p.function_response.response for e in history for p in e.parts if p.function_response]
    return [json.loads(e["content"]) for e in history if e["role"] == "tool"]


def add_tool(app: Any, name: str, run: Any, schema: dict[str, Any] | None = None) -> None:
    parameters = schema or {"type": "object", "properties": {}}
    app.TOOLS[name] = app.Tool(
        name=name,
        declaration={"name": name, "description": "test tool", "parameters": parameters},
        args=app.schema_model(name + "_args", parameters, "forbid"),
        result=None,
        run=run,
        end_call=False,
    )


# --- fake cases ----------------------------------------------------------------

CASES = []


def case(function: Any) -> Any:
    """A case that scripts the model SDK, so it runs on a model build."""
    function.kind = "model"
    CASES.append(function)
    return function


def protocol_case(function: Any) -> Any:
    """A case that never reaches the turn, so it runs on every build."""
    case(function).kind = "protocol"
    return function


def logic_case(function: Any) -> Any:
    """A case that replaces logic.respond, so it runs on a custom logic build."""
    case(function).kind = "logic"
    return function


@contextlib.contextmanager
def using_logic(app: Any, respond: Any, next_twiml: Any = None) -> Any:
    saved, app.logic.respond = app.logic.respond, respond
    saved_step = getattr(app.logic, "next_twiml", None)
    if next_twiml is not None:
        app.logic.next_twiml = next_twiml
    try:
        yield
    finally:
        app.logic.respond = saved
        if saved_step is not None:
            app.logic.next_twiml = saved_step
        elif hasattr(app.logic, "next_twiml"):
            del app.logic.next_twiml


def fake_for(app: Any, script: list[list[Any]]) -> Any:
    return FakeGemini(script) if gemini(app) else FakeOpenAI(script)


def env(app: Any, script: list[list[Any]] | None = None, **patches: Any) -> Env:
    # A short drain, so the server's shutdown does not wait out the
    # default for a call a case left open.
    patches.setdefault("DRAIN_TIMEOUT", 0.2)
    # The same for a socket the fake never closes after an end message.
    patches.setdefault("END_GRACE", 0.2)
    return Env(app, fake_for(app, script or []), patches)


@protocol_case
async def health_and_signed_voice(app: Any) -> None:
    async with env(app) as e:
        health = await e.client.get("/healthz")
        check(health.status == 200, "healthz is not 200")
        check((await health.json()).get("artifact_id") == app.ARTIFACT_ID, "healthz does not name this build")
        form = {"AccountSid": ACCOUNT, "CallSid": CALL, "From": "+15005550006"}
        response = await e.post("/voice", form)
        body = await response.text()
        check(response.status == 200, f"signed /voice returned {response.status}")
        check(f'url="wss://{HOST}/conversation"' in body, "TwiML does not point at the public wss origin")
        check(f'action="{ORIGIN}/connect-action"' in body, "TwiML action is not the public origin")
        check("__PUBLIC" not in body, "a placeholder reached the TwiML")
        check("<Hangup" in body and 'dtmfDetection="false"' in body, "TwiML lacks Hangup or has DTMF on")
        with_query = await e.post("/voice?x=1", form, signed_url=ORIGIN + "/voice?x=1")
        check(with_query.status == 200, "a signed query string was refused")


@protocol_case
async def voice_signature_rejections(app: Any) -> None:
    async with env(app) as e:
        form = {"AccountSid": ACCOUNT, "CallSid": CALL}
        for label, response in [
            ("no signature", await e.client.post("/voice", data=form)),
            ("wrong signature", await e.post("/voice", form, sig="bm90IGEgc2lnbmF0dXJl")),
            ("wrong origin", await e.post("/voice", form, signed_url="https://evil.example.com/voice")),
            ("wrong path", await e.post("/voice", form, signed_url=ORIGIN + "/other")),
            ("wrong query", await e.post("/voice?x=2", form, signed_url=ORIGIN + "/voice?x=1")),
            ("changed field", await e.client.post(
                "/voice", data={**form, "From": "+1999"}, headers={"X-Twilio-Signature": e.sign(ORIGIN + "/voice", form)}
            )),
            ("wrong account", await e.post("/voice", {"AccountSid": "AC" + "f" * 32})),
            ("unsigned action", await e.client.post("/connect-action", data=form)),
        ]:
            check(response.status == 403, f"{label}: got {response.status}, want 403")
        action = await e.post("/connect-action", {**form, "SessionStatus": "ended", "HandoffData": '{"x":1}'})
        body = await action.text()
        check(action.status == 200 and "<Hangup/>" in body and "Connect" not in body, "connect-action did not hang up")


@protocol_case
async def connect_action_picks_the_next_step(app: Any) -> None:
    seen: list[Any] = []

    def transfer(handoff: Any) -> str:
        seen.append(handoff)
        return handoff.resume("the transfer was not answered", before="<Say>One moment.</Say>")

    steps = {"transfer": transfer, "say": lambda h: "<Response><Say>Bye</Say></Response>"}
    async with env(app, NEXT_STEPS=steps) as e:
        for handoff in ["not json", '["a list"]', '{"reasonCode": "nobody_knows"}', ""]:
            body = await e.next_step(handoff)
            check("<Hangup/>" in body and "Connect" not in body, f"{handoff!r} did not hang up: {body}")
        check("<Say>Bye</Say>" in await e.next_step('{"reasonCode": "say"}'), "a built-in step was not answered")
        body = await e.next_step('{"reasonCode": "transfer", "to": "desk"}')
        check(seen and seen[0].reason == "transfer" and seen[0].data == {"to": "desk"}, f"handoff: {seen}")
        check(seen[0].call["call_sid"] == CALL and seen[0].status == "ended", f"call: {seen[0].call}")
        check('<Parameter name="resume" value="the transfer was not answered"' in body, f"no resume: {body}")
        check("welcomeGreeting=" not in body and f'url="wss://{HOST}/conversation"' in body, f"resume TwiML: {body}")
        check(body.index("<Say>One moment.</Say>") < body.index("<Connect"), f"before is not first: {body}")
        check(f'action="{ORIGIN}/connect-action"' in body, "a resume must come back to /connect-action")


@protocol_case
async def websocket_handshake_and_setup(app: Any) -> None:
    from aiohttp import WSServerHandshakeError

    async with env(app) as e:
        for label, url in [("wrong origin", "wss://evil.example.com/conversation"), ("https scheme", ORIGIN + "/conversation")]:
            try:
                await e.client.ws_connect("/conversation", headers={"X-Twilio-Signature": e.sign(url)})
            except WSServerHandshakeError as error:
                check(error.status == 403, f"{label}: status {error.status}")
            else:
                raise AssertionError(f"{label}: handshake accepted")
        slash = await e.relay(signed_url=f"wss://{HOST}/conversation/", setup=False)
        await slash.prompt("hello")  # a prompt before setup
        check(await slash.next() is None, "a prompt before setup was accepted")
        wrong = await e.relay(setup=False)
        await wrong.setup(account="AC" + "f" * 32)
        check(await wrong.next() is None, "a setup for another account was accepted")
        check(not e.calls, "call state started before a valid setup")


@case
async def streamed_reply_spacing_and_last(app: Any) -> None:
    async with env(app, [["The desk ", "opens ", "at nine.", TAIL]]) as e:
        relay = await e.relay()
        await relay.prompt("When do you", last=False)
        check(not await relay.quiet(0.2), "a partial prompt started a reply")
        check(not e.fake.requests, "a partial prompt reached the model")
        await relay.prompt("When do you open?")
        tokens = await relay.reply()
        check(text_of(tokens) == "The desk opens at nine.", f"spacing lost: {text_of(tokens)!r}")
        check([t["last"] for t in tokens] == [False, False, True], "last=true is not on the last fragment only")
        first = [(r, t) for r, t in zip(roles(app, e.fake.requests[0]), texts(app, e.fake.requests[0])) if r != "system"]
        check(first[0][1] == app.GREETING, "the greeting is not the first history entry")
        check([r for r, _ in first][1:] == ["user"], "the greeting was not recorded exactly once")


@case
async def router_request_carries_config_and_identity(app: Any) -> None:
    if not hasattr(app, "ROUTER_SCOPE"):
        return  # a direct binding: nothing router-shaped to check
    async with env(app, [["Nine."], ["Ten."]]) as e:
        relay = await e.relay()
        for text in ("When do you open?", "And on Friday?"):
            await relay.prompt(text)
            await relay.reply()
        for raw in e.fake.raw:
            headers = {"X-Slng-Agent-Id": app.ROUTER_SCOPE, "X-Slng-Session-Id": SESSION}
            check(raw.get("extra_headers") == headers, f"identity headers: {raw.get('extra_headers')}")
            tiers = raw.get("extra_body", {}).get("slng_config", {}).get("tiers", {})
            check(tiers.get("1", [{}])[0].get("model") == app.MODEL, f"slng_config does not name the model: {tiers}")
            check("api_key" not in json.dumps(raw.get("extra_headers")), "a credential rode a header")


@case
async def identical_prompts_are_two_turns(app: Any) -> None:
    gate = asyncio.Event()
    async with env(app, [[gate, "one"], ["Yes."]]) as e:
        relay = await e.relay()
        await relay.prompt("Yes")
        await until(lambda: len(e.fake.requests) == 1)
        await relay.prompt("Yes")
        gate.set()
        await relay.reply()
        users = [t for r, t in zip(roles(app, e.history()), texts(app, e.history())) if r == "user"]
        check(users == ["Yes", "Yes"], f"identical prompts were merged: {users}")


@case
async def tool_round_is_two_cycles(app: Any) -> None:
    script = [["Let me check. ", call("opening_hours", {"day": "monday"})], ["We open at nine.", TAIL]]
    async with env(app, script) as e:
        relay = await e.relay()
        await relay.prompt("When do you open on Monday?")
        first, second = await relay.reply(), await relay.reply()
        check(text_of(first) == "Let me check. " and first[-1]["last"], "the preamble is not its own cycle")
        check(text_of(second) == "We open at nine." and second[-1]["last"], "the answer is not its own cycle")
        results = tool_results(app, e.history())
        check(results and results[0].get("opens") == "09:00", f"tool result missing: {results}")
        check(tool_results(app, e.fake.requests[1]) == results, "the follow-up request lacks the tool result")
        if gemini(app):
            call_part = next(p for c in e.history() for p in c.parts if p.function_call)
            check(call_part.thought_signature == b"sig-opening_hours", "the function call lost its signature")
            check(call_part.function_call.id == "call_1", "the function call lost its id")


@case
async def gemini_call_without_an_id(app: Any) -> None:
    if not gemini(app):
        return  # Chat Completions always sends a call id
    async with env(app, [[call("opening_hours", {"day": "friday"}, id="")], ["Nine to three."]]) as e:
        relay = await e.relay()
        await relay.prompt("Friday?")
        await relay.reply()
        response = next(p.function_response for c in e.history() for p in c.parts if p.function_response)
        check(response.id is None, f"an id was invented for a call that had none: {response.id!r}")


@case
async def regression_stale_token_after_interrupt(app: Any) -> None:
    gate = asyncio.Event()
    async with env(app, [["Hello ", "there ", gate, "stale"]]) as e:
        relay = await e.relay()
        await relay.prompt("Hi")
        first = await relay.next()
        check(first and first["token"] == "Hello ", f"unexpected first token {first}")
        await relay.interrupt("Hello")
        await until(lambda: e.calls[0].generation >= 2)
        gate.set()
        late = await relay.quiet(0.3)
        check(not any("stale" in m.get("token", "") for m in late), f"a stale token was sent: {late}")
        # The send boundary itself: a fragment tagged with an old generation
        # is dropped even if it reaches the lock.
        live = e.calls[0]
        old = app.Cycle(generation=live.generation - 1)
        await live.send_token(old, "late", last=False)
        check(not old.sent and not await relay.quiet(0.2), "the send lock let an old generation through")


@case
async def regression_preamble_survives_round_two_interrupt(app: Any) -> None:
    gate = asyncio.Event()
    script = [
        ["Let me check. ", call("opening_hours", {"day": "monday"})],
        ["We ", "open ", "at ", "nine ", "in the morning.", gate],
    ]
    async with env(app, script) as e:
        relay = await e.relay()
        await relay.prompt("Monday hours?")
        await relay.reply()
        seen = []
        while len(seen) < 4:
            seen.append(await relay.next())
        await relay.interrupt("We open at nine")
        await until(lambda: e.calls[0].spoken is None and e.calls[0].interrupt is None)
        history = e.history()
        spoken = [t for r, t in zip(roles(app, history), texts(app, history)) if r in ("assistant", "model")]
        check(spoken.count("Let me check. ") == 1, f"the preamble was duplicated or lost: {spoken}")
        check(spoken[-1] == "We open at nine", f"round two was not cut to what was heard: {spoken}")
        check(len(tool_results(app, history)) == 1, "the completed tool result was lost")
        if gemini(app):
            call_part = next(p for c in history for p in c.parts if p.function_call)
            check(call_part.thought_signature == b"sig-opening_hours", "an interrupt edited a signed part")
        gate.set()


@case
async def interrupt_after_last_cuts_history(app: Any) -> None:
    script = [["The desk opens at nine. ", "It closes at five."], ["Sure."]]
    async with env(app, script) as e:
        relay = await e.relay()
        await relay.prompt("Hours?")
        await relay.reply()
        await relay.interrupt("The desk opens at nine.")
        await until(lambda: texts(app, e.history())[-1] != "The desk opens at nine. It closes at five.")
        check(texts(app, e.history())[-1] == "The desk opens at nine.", f"not cut: {texts(app, e.history())}")
        await relay.prompt("Thanks")
        check(text_of(await relay.reply()) == "Sure.", "the next turn after an interrupt failed")


@case
async def unclear_interrupt_drops_the_cycle(app: Any) -> None:
    async with env(app, [["We open at nine."]]) as e:
        relay = await e.relay()
        await relay.prompt("Hours?")
        await relay.reply()
        before = len(e.history())
        await relay.interrupt("something else entirely")
        await until(lambda: len(e.history()) == before - 1)


@case
async def interrupt_during_tool_keeps_result_and_reply(app: Any) -> None:
    release = threading.Event()
    add_tool(app, "slow", lambda: release.wait(5) and {"ok": True})
    try:
        async with env(app, [["One moment. ", call("slow", {})], ["Done."]]) as e:
            relay = await e.relay()
            await relay.prompt("Do it")
            await relay.reply()
            await until(lambda: e.calls[0].tool_future is not None)
            await relay.interrupt("One")
            release.set()
            check(text_of(await relay.reply()) == "Done.", "the post-tool reply was dropped")
            check(tool_results(app, e.history()) == [{"ok": True}], "the tool result was lost")
    finally:
        app.TOOLS.pop("slow")


@case
async def prompt_during_tool_is_kept_after_the_result(app: Any) -> None:
    release = threading.Event()
    add_tool(app, "slow", lambda: release.wait(5) and {"ok": True})
    try:
        async with env(app, [[call("slow", {})], ["Both done."]]) as e:
            relay = await e.relay()
            await relay.prompt("Do it")
            await until(lambda: e.calls[0].tool_future is not None)
            await relay.prompt("And tuesday?")
            release.set()
            check(text_of(await relay.reply()) == "Both done.", "no reply after the tool")
            order = roles(app, e.fake.requests[1])
            check(order[-2:] == (["tool", "user"]), f"the prompt is not after the tool result: {order}")
    finally:
        app.TOOLS.pop("slow")


@case
async def tool_errors_are_results(app: Any) -> None:
    script = [
        [call("nope", {}, id="a")],
        [call("opening_hours", {"day": "funday"}, id="b")],
        [call("opening_hours", {"day": "monday", "extra": 1}, id="c")],
        ["Sorry."],
    ]
    async with env(app, script) as e:
        relay = await e.relay()
        await relay.prompt("Hours?")
        check(text_of(await relay.reply()) == "Sorry.", "no reply after tool errors")
        errors = [r.get("error") for r in tool_results(app, e.history())]
        check(errors == ["unknown_tool", "invalid_arguments", "invalid_arguments"], f"errors: {errors}")


@case
async def timeout_is_unknown_and_holds_the_slot(app: Any) -> None:
    release = threading.Event()
    add_tool(app, "hang", lambda: release.wait(5) and {"ok": True})
    try:
        async with env(app, [[call("hang", {})], ["It may still finish."]], TOOL_DEADLINE=0.2, MAX_SESSIONS=1) as e:
            relay = await e.relay()
            await relay.prompt("Do it")
            await relay.reply()
            check(tool_results(app, e.history())[0].get("outcome") == "unknown", "a timeout was not reported as unknown")
            await relay.ws.close()
            await until(lambda: e.calls[0].closed)
            slots = e.slots[0]
            check(slots.taken == 1 and slots.orphaned == 1, "the slot was released while the handler still runs")
            tools = [th for th in threading.enumerate() if th.name == "tool"]
            check(tools and all(th.daemon for th in tools), "a handler thread would block process exit")
            full = await e.client.get("/healthz")
            check(full.status == 503, "healthz is 200 with every slot held")
            check((await full.json()).get("artifact_id") == app.ARTIFACT_ID, "a 503 healthz does not name this build")
            release.set()
            await until(lambda: slots.taken == 0 and slots.orphaned == 0)
            check((await e.client.get("/healthz")).status == 200, "healthz did not recover")
    finally:
        app.TOOLS.pop("hang")


@case
async def rounds_are_bounded(app: Any) -> None:
    loop = [[call("opening_hours", {"day": "monday"}, id=f"r{i}")] for i in range(3)]
    async with env(app, loop, MAX_TOOL_ROUNDS=2) as e:
        relay = await e.relay()
        await relay.prompt("Hours?")
        check(text_of(await relay.reply()) == app.FAILURE_LINE, "the round cap did not take the failure path")
        check(len(e.fake.requests) == 2, f"{len(e.fake.requests)} model requests, want 2")


@case
async def empty_reply_takes_the_failure_path(app: Any) -> None:
    async with env(app, [[TAIL]]) as e:
        relay = await e.relay()
        await relay.prompt("Hello?")
        check(text_of(await relay.reply()) == app.FAILURE_LINE, "an empty reply was not the failure line")


@case
async def model_failures_end_the_call(app: Any) -> None:
    boom = [[RuntimeError("provider down")] for _ in range(3)]
    async with env(app, boom) as e:
        relay = await e.relay()
        for _ in range(2):
            await relay.prompt("Hello?")
            check(text_of(await relay.reply()) == app.FAILURE_LINE, "a model failure was not the failure line")
        await relay.prompt("Hello?")
        end = await relay.next()
        check(end and end["type"] == "end" and "model_failures" in end["handoffData"], f"no end after 3 failures: {end}")


@case
async def end_call_ends_the_session(app: Any) -> None:
    async with env(app, [["Goodbye. ", call("end_call", {})]]) as e:
        relay = await e.relay()
        await relay.prompt("That's all")
        await relay.reply()
        end = await relay.next()
        check(end and end["type"] == "end", f"end_call did not end the session: {end}")


@case
async def end_waits_for_twilio_to_close(app: Any) -> None:
    # Twilio closes the socket once it has the end message. An app that closes
    # first can beat the end there, and the session fails as 64105: seen on a
    # real call, where the Connect callback said status=failed.
    async with env(app, [["Goodbye. ", call("end_call", {})]], END_GRACE=2.0) as e:
        relay = await e.relay()
        await relay.prompt("That's all")
        await relay.reply()
        check((await relay.next())["type"] == "end", "end_call sent no end")
        check(await relay.stays_open(), "the app closed the socket before Twilio did")
        await relay.ws.close()
        await until(lambda: e.slots[0].taken == 0)
    # A peer that never closes is closed by the app after END_GRACE.
    async with env(app, [["Goodbye. ", call("end_call", {})]], END_GRACE=0.2) as e:
        relay = await e.relay()
        await relay.prompt("That's all")
        await relay.reply()
        check((await relay.next())["type"] == "end", "end_call sent no end")
        check(await relay.next(2.0) is None, "the app never closed a socket Twilio left open")


@case
async def resume_carries_the_conversation_on(app: Any) -> None:
    script = [["Hello. "], ["Goodbye. ", call("end_call", {})], ["Sure."]]
    back = "Sorry, nobody answered. What else can I do?"
    steps = {"end_call": lambda h: h.resume("the transfer was not answered", greeting=back)}
    async with env(app, script, NEXT_STEPS=steps) as e:
        relay = await e.relay()
        await relay.prompt("Hi")
        await relay.reply()
        await relay.prompt("Put me through")
        await relay.reply()
        end = await relay.next()
        check(end and end["type"] == "end", f"no end: {end}")
        await relay.ws.close()
        body = await e.next_step(end["handoffData"])
        check('name="resume"' in body and f'welcomeGreeting="{back}"' in body, f"the step did not resume: {body}")
        again = await e.resumed("the transfer was not answered", greeting=back)
        # ConversationRelay speaks the greeting; the model is not asked to.
        check(await again.quiet(0.3) == [], "the app spoke on a resume")
        await again.prompt("Can you try again?")
        check(text_of(await again.reply()) == "Sure.", "the resumed call did not answer the caller")
        said = texts(app, e.history())
        check("Hi" in said and back in said, f"the resumed call lost the conversation or the greeting: {said}")
        check(app.GREETING is None or said.count(app.GREETING) <= 1, "the first greeting was added twice")
        await again.ws.close()


@case
async def resume_after_hangup_or_expiry_starts_fresh(app: Any) -> None:
    for patches in [{}, {"PARK_SECONDS": -1.0}]:
        # A hangup answer lets the history go; an expired park has none left.
        steps = {} if not patches else {"end_call": lambda h: h.resume("late")}
        async with env(app, [["Goodbye. ", call("end_call", {})]], NEXT_STEPS=steps, **patches) as e:
            relay = await e.relay()
            await relay.prompt("Bye")
            await relay.reply()
            end = await relay.next()
            await relay.ws.close()
            await e.next_step(end["handoffData"])
            again = await e.resumed("late")
            check(await again.quiet(0.3) == [], "a session with nothing parked spoke first")
            check("Bye" not in texts(app, e.history()), f"history survived {patches or 'a hangup'}")
            await again.ws.close()


@protocol_case
async def overload_is_refused(app: Any) -> None:
    async with env(app, MAX_SESSIONS=1, END_GRACE=2.0) as e:
        # Held, not dropped: a dropped socket is collected and closed, which
        # gives the slot back before /voice is asked.
        first = await e.relay()
        body = await (await e.post("/voice", {"AccountSid": ACCOUNT})).text()
        check("<Hangup/>" in body and "Connect" not in body, "/voice took a call past max_sessions")
        second = await e.relay(setup=False)
        await second.setup()
        end = await second.next()
        check(end and end["type"] == "end" and "busy" in end["handoffData"], f"a second session was admitted: {end}")
        check(await second.stays_open(), "the busy refusal closed the socket before Twilio did")
        await second.ws.close()
        await first.ws.close()


@protocol_case
async def shutdown_drains_then_ends(app: Any) -> None:
    async with env(app, DRAIN_TIMEOUT=0.2) as e:
        relay = await e.relay()
        closing = asyncio.ensure_future(e.stop())
        end = await relay.next()
        await closing
        check(end and end["type"] == "end" and "shutdown" in end["handoffData"], f"no shutdown end: {end}")
        check(e.slots[0].draining, "admission stayed open during shutdown")


@logic_case
async def logic_reply_streams_with_last(app: Any) -> None:
    seen: list[Any] = []

    async def respond(session: Any) -> Any:
        seen.append(session.history)
        for piece in ("The desk ", "opens ", "at nine."):
            yield piece

    with using_logic(app, respond):
        async with env(app) as e:
            relay = await e.relay()
            await relay.prompt("When do you open?")
            tokens = await relay.reply()
            check(text_of(tokens) == "The desk opens at nine.", f"reply: {text_of(tokens)!r}")
            check([t["last"] for t in tokens] == [False, False, True], "last=true is not on the last piece only")
            want = [{"role": "user", "content": "When do you open?"}]
            if app.GREETING:
                want.insert(0, {"role": "assistant", "content": app.GREETING})
            check(seen == [want], f"respond() saw {seen}")
            session = e.calls[0].brain.session
            check(session.call["call_sid"] == CALL and session.call["session_id"] == SESSION, "call ids")
            check(session.call["from"] == "+15005550006" and session.call["custom_parameters"] == {}, "setup fields")
            check(session.instructions == app.INSTRUCTIONS and session.model.model == app.MODEL, "package settings")


@logic_case
async def logic_interrupt_keeps_what_was_heard(app: Any) -> None:
    gate = asyncio.Event()
    cancelled: list[bool] = []
    seen: list[Any] = []

    async def respond(session: Any) -> Any:
        seen.append(session.history)
        if len(seen) > 1:
            yield "Sure."
            return
        try:
            yield "One two. "
            yield "Three four. "
            await gate.wait()
            yield "Never sent."
        except asyncio.CancelledError:
            cancelled.append(True)
            raise

    with using_logic(app, respond):
        async with env(app) as e:
            relay = await e.relay()
            await relay.prompt("Count for me.")
            first = await relay.next()
            check(first and first["token"] == "One two. ", f"first piece: {first}")
            await relay.interrupt("One two.")
            await until(lambda: cancelled)
            await relay.quiet(0.3)
            await relay.prompt("Stop.")
            await relay.reply()
            heard = [h for h in seen[1] if h["role"] == "assistant" and h["content"] != app.GREETING]
            check(heard == [{"role": "assistant", "content": "One two."}], f"history after the interrupt: {seen[1]}")


@logic_case
async def logic_error_takes_the_failure_path(app: Any) -> None:
    async def respond(session: Any) -> Any:
        raise RuntimeError("the agent broke")
        yield ""  # an async generator, as respond() must be

    with using_logic(app, respond):
        async with env(app) as e:
            relay = await e.relay()
            await relay.prompt("Hello?")
            check(text_of(await relay.reply()) == app.FAILURE_LINE, "no failure line after respond() raised")


@logic_case
async def logic_end_ends_the_call(app: Any) -> None:
    async def respond(session: Any) -> Any:
        session.end("caller_done")
        yield "Goodbye."

    with using_logic(app, respond):
        async with env(app) as e:
            relay = await e.relay()
            await relay.prompt("That's all.")
            check(text_of(await relay.reply()) == "Goodbye.", "the goodbye was not spoken")
            end = await relay.next()
            check(end and end["type"] == "end", f"session.end() sent no end: {end}")
            await relay.ws.close()


@logic_case
async def logic_next_twiml_hands_the_caller_back(app: Any) -> None:
    seen: list[Any] = []

    async def respond(session: Any) -> Any:
        session.state["turns"] = session.state.get("turns", 0) + 1
        seen.append((session.history[-1]["content"], session.state["turns"], session))
        if session.state["turns"] == 1:
            session.end("hold", seconds=5)
            yield "One moment."
        else:
            yield "Thanks for waiting."

    async def next_twiml(handoff: Any) -> str:
        check(handoff.reason == "hold" and handoff.data == {"seconds": 5}, f"handoff: {handoff}")
        return handoff.resume("the hold music ended", greeting="Thanks for waiting.")

    with using_logic(app, respond, next_twiml):
        async with env(app) as e:
            relay = await e.relay()
            await relay.prompt("Hold on.")
            await relay.reply()
            end = await relay.next()
            check(end and '"seconds": 5' in end["handoffData"], f"session.end data: {end}")
            await relay.ws.close()
            body = await e.next_step(end["handoffData"])
            check('name="resume"' in body, f"next_twiml was not answered: {body}")
            again = await e.resumed("the hold music ended", greeting="Thanks for waiting.")
            await again.prompt("Still there?")
            check(text_of(await again.reply()) == "Thanks for waiting.", "the resumed logic did not answer")
            check(seen[1][1] == 2 and seen[1][0] == "Still there?", f"state or turn lost: {seen}")
            session = seen[1][2]
            check(session.call["custom_parameters"].get("resume") == "the hold music ended", "the note is not on session.call")
            check({"role": "assistant", "content": "Thanks for waiting."} in session.history, "the greeting is not in history")
            # One session object for the whole call, pointed at the new socket.
            check(seen[1][2] is seen[0][2] and seen[1][2]._call is e.calls[-1], "the session was not kept and rebound")
            await again.ws.close()


@logic_case
async def logic_next_twiml_failures_hang_up(app: Any) -> None:
    async def respond(session: Any) -> Any:
        yield "Hi."

    def broken(handoff: Any) -> str:
        raise RuntimeError("the step broke")

    for hook, label in [(broken, "an exception"), (lambda h: "<Response><Say>", "bad XML"), (lambda h: 42, "not a str")]:
        with using_logic(app, respond, hook):
            async with env(app) as e:
                body = await e.next_step('{"reasonCode": "end_call"}')
                check("<Hangup/>" in body, f"{label} did not hang up: {body}")
    with using_logic(app, respond, lambda h: "<Response><Say>Bye</Say></Response>"):
        async with env(app) as e:
            check("<Say>Bye</Say>" in await e.next_step('{"reasonCode": "end_call"}'), "next_twiml TwiML was replaced")


# --- real cases ----------------------------------------------------------------


async def real_logic(app: Any) -> None:
    """The package's own logic against the real model: a tool answer, an
    interrupt, the next turn, a hold that leaves and resumes the session when
    the logic has next_twiml, and a goodbye that ends the call."""
    async with Env(app, None, {"DRAIN_TIMEOUT": 0.2}) as e:
        relay = await e.relay()
        await relay.prompt("What time do you open on Monday?")
        monday = text_of(await relay.reply(timeout=40))
        check("nine" in monday.lower() or "9" in monday, f"no Monday hours: {monday!r}")
        print(f"  tool answer: {monday!r}")

        await relay.prompt("Tell me a long story about the building's history.")
        first = await relay.next(timeout=40)
        check(first and first["type"] == "text", "no token to interrupt")
        await relay.interrupt(first["token"])
        await relay.quiet(1.0)
        await relay.prompt("Sorry, what about Friday?")
        friday = text_of(await relay.reply(timeout=40))
        check(friday.strip(), "no answer after the interrupt")
        print(f"  next turn after interrupt: {friday!r}")
        story = [h for h in e.calls[0].brain.history if h["role"] == "assistant"][-2]["content"]
        check(len(story) < 400, f"the interrupted story was kept whole: {story!r}")

        if hasattr(app.logic, "next_twiml"):
            # The package's own step after the session: a hold, then back.
            await relay.prompt("Can you hold on a second?")
            await relay.reply(timeout=40)
            end = await relay.next(timeout=10)
            check(end and end["type"] == "end", f"the hold did not end the session: {end}")
            await relay.ws.close()
            body = await e.next_step(end["handoffData"])
            check("<Say>" in body and 'name="resume"' in body, f"next_twiml did not hold and resume: {body}")
            greeting = body.split('name="greeting" value="')[1].split('"')[0]
            relay = await e.resumed("a short hold", greeting=greeting)
            await relay.prompt("What was the very first thing I asked you on this call?")
            back = text_of(await relay.reply(timeout=40))
            check("monday" in back.lower(), f"the agent forgot the call after the hold: {back!r}")
            print(f"  back after the hold: {back!r}")

        await relay.prompt("That's all, thank you. Goodbye.")
        await relay.reply(timeout=40)
        end = await relay.next(timeout=10)
        check(end and end["type"] == "end", f"the goodbye did not end the call: {end}")
        check("caller_done" in end["handoffData"], f"the goodbye ended the wrong way: {end}")
        print("  goodbye ended the call through session.end()")


def load_key(app: Any, env_file: Path, name: str) -> None:
    if os.environ.get(name):
        return
    for line in env_file.read_text().splitlines():
        key, _, value = line.partition("=")
        if key.strip() == name:
            os.environ[name] = value.strip().strip('"').strip("'")
            return
    sys.exit(f"{name} is not set and not in {env_file}")


async def real(app: Any) -> None:
    async with Env(app, None, {"DRAIN_TIMEOUT": 0.2}) as e:
        relay = await e.relay()
        await relay.prompt("What time do you open on Monday?")

        def answered() -> bool:
            history = e.history()
            return bool(
                tool_results(app, history)
                and roles(app, history)[-1] in ("assistant", "model")
                and texts(app, history)[-1].strip()
            )

        await until(answered, 40)
        tokens = [m for m in await relay.quiet(1.0) if m.get("type") == "text"]
        check(tokens and tokens[-1]["last"], "the reply did not end with last=true")
        check(tool_results(app, e.history())[0].get("opens") == "09:00", "opening_hours did not run")
        print(f"  tool follow-up: {texts(app, e.history())[-1]!r}")

        await relay.prompt("Tell me a long story about the building's history.")
        first = await relay.next(timeout=40)
        check(first and first["type"] == "text", "no token to interrupt")
        await relay.interrupt(first["token"])
        await relay.quiet(1.0)
        await relay.prompt("Sorry, what about Friday?")
        after = await relay.reply(timeout=40)
        while not text_of(after).strip() or not after[-1].get("last"):
            after = await relay.reply(timeout=40)
        print(f"  next turn after interrupt: {text_of(after)!r}")

        if gemini(app):
            await signature_replay(app, e.history())


async def signature_replay(app: Any, history: list[Any]) -> None:
    """Replay the first tool follow-up with and without its signatures."""
    from google.genai import errors

    index = next(i for i, c in enumerate(history) if any(p.function_call for p in c.parts))
    contents = history[: index + 2]  # through the tool's response
    client = app.make_client()
    brain = app.Brain(client)
    kept = await client.aio.models.generate_content(model=app.MODEL, contents=contents, config=brain.config)
    check(kept.candidates is not None, "the follow-up with signatures failed")
    stripped = [c.model_copy(deep=True) for c in contents]
    for content in stripped:
        for part in content.parts:
            if part.function_call:
                part.thought_signature = None
    try:
        await client.aio.models.generate_content(model=app.MODEL, contents=stripped, config=brain.config)
    except errors.ClientError as error:
        check(error.code == 400, f"stripped signatures returned {error.code}, want 400")
        print("  signatures: kept -> 200, stripped -> 400")
        return
    raise AssertionError("the API accepted a tool follow-up with its signatures stripped")


# --- main ----------------------------------------------------------------------


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("build", help="a compiled twilio target directory, holding app.py")
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--fake", action="store_true", help="scripted model, offline")
    mode.add_argument("--real", action="store_true", help="the real model, with the key from --env-file")
    parser.add_argument("--env-file", type=Path, default=REPO / ".env")
    parser.add_argument("--case", action="append", default=[], help="run only these fake cases")
    args = parser.parse_args()

    build = Path(args.build).resolve()
    if not (build / "app.py").exists():
        sys.exit(f"{build} holds no app.py; compile the package first")
    sys.path.insert(0, str(build))
    os.environ.update(
        {"TWILIO_ACCOUNT_SID": ACCOUNT, "TWILIO_AUTH_TOKEN": TOKEN, "TWILIO_PUBLIC_URL": ORIGIN}
    )
    import app  # noqa: E402 - the build directory is only importable now

    os.environ.setdefault(app.ENV_MODEL_KEY, "fake-model-key")
    for name in app.UPSTREAM_ENV:
        os.environ.setdefault(name, "fake-upstream-key")
    if args.real:
        for name in (app.ENV_MODEL_KEY, *app.UPSTREAM_ENV):
            os.environ.pop(name, None)
            load_key(app, args.env_file, name)
        print(f"real: {app.MODEL}")
        asyncio.run(real_logic(app) if hasattr(app, "LogicBrain") else real(app))
        print("real: pass")
        return

    ran = failed = 0
    logic_build = hasattr(app, "LogicBrain")
    for function in CASES:
        if args.case and function.__name__ not in args.case:
            continue
        if function.kind == ("logic" if not logic_build else "model"):
            continue
        ran += 1
        try:
            asyncio.run(asyncio.wait_for(function(app), 15))
            print(f"pass  {function.__name__}")
        except Exception as error:  # noqa: BLE001 - every failure is reported, then counted
            failed += 1
            print(f"FAIL  {function.__name__}: {type(error).__name__}: {error}")
    print(f"{ran - failed} passed, {failed} failed")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()

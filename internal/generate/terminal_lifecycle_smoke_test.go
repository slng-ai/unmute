//go:build smoke

package generate

import "testing"

// The one acknowledgement a completed flow gets, proven on the framework's own
// lifecycle rather than on a helper's return value: a real AgentSession, a
// scripted model and a fake voice, driving the fixture's owner into its group,
// through two steps that end on their tools, and back.
//
// A live call showed the step speaking first and the owner speaking again, one
// request and one acknowledgement more than the caller needed, because the last
// step returned a value from its terminal tool. Now it returns nothing, and the
// owner's reply is the one the framework generates when the delegate returns.
// The three journeys here are the two-step flow, the same flow with the caller
// interrupting the booking step, and the isolated sequence stopping on unserved.
func TestSmokeTerminalLifecycleLiveKit(t *testing.T) {
	runLiveKitSmokeScript(t, "terminal_step", nil, nil, terminalLifecycleScript)
}

const terminalLifecycleScript = `"""Smoke check: who speaks after a step ends on its tool, on the real session."""
# ruff: noqa: E402 - the environment has to be seeded before the module imports
import ast
import asyncio
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import agent
from livekit.agents import DEFAULT_API_CONNECT_OPTIONS, AgentSession, io, llm, tts
from livekit.agents.llm.tool_context import get_function_info, is_function_tool


def tool_names(tools):
    return {get_function_info(tool).name for tool in tools or [] if is_function_tool(tool)}


class ModelStream(llm.LLMStream):
    def __init__(self, model, *, reply, **kwargs):
        super().__init__(model, **kwargs)
        self.reply = reply

    async def _run(self):
        kind, payload = self.reply
        if kind == "text":
            delta = llm.ChoiceDelta(role="assistant", content=payload)
        else:
            name, arguments = payload
            self._llm.calls += 1
            delta = llm.ChoiceDelta(
                role="assistant",
                tool_calls=[
                    llm.FunctionToolCall(
                        name=name, arguments=json.dumps(arguments), call_id=f"call-{self._llm.calls}"
                    )
                ],
            )
        self._event_ch.send_nowait(llm.ChatChunk(id=f"request-{self._llm.calls}", delta=delta))


class Model(llm.LLM):
    """Answers by which step is asking, so the script survives retries and
    interruptions: the owner routes into the flow, verification looks the number
    up, booking books, and the owner acknowledges once it holds the status."""

    def __init__(self, *, interrupt=False, alone=False):
        super().__init__()
        self.interrupt = interrupt
        self.alone = alone
        self.requests = []
        self.book_requests = 0
        self.calls = 0

    @property
    def model(self):
        return "lifecycle-probe"

    @property
    def provider(self):
        return "fake-provider"

    def decide(self, chat_ctx, tools):
        names = tool_names(tools)
        delegate = "do_book_alone" if self.alone else "do_book"
        if delegate in names:
            returned = any(
                isinstance(item, llm.FunctionCallOutput) and item.name == delegate
                for item in chat_ctx.items
            )
            self.requests.append(("owner", returned))
            if returned:
                return ("text", "All set, your haircut is booked.")
            return ("call", (delegate, {}))
        if "look_up" in names:
            self.requests.append(("verify", False))
            if self.alone:
                return ("call", ("finish", {"unserved_request": "The caller gave a wrong number and hung back."}))
            return ("call", ("look_up", {"phone": "5550101010"}))
        if "book_it" in names:
            self.requests.append(("book", False))
            self.book_requests += 1
            if self.interrupt and self.book_requests == 1:
                return ("text", "Which service would you like?")
            return ("call", ("book_it", {"confirmed": True, "service": "haircut"}))
        self.requests.append(("other", False))
        return ("text", "Okay.")

    def chat(self, *, chat_ctx, tools=None, conn_options=DEFAULT_API_CONNECT_OPTIONS, **kwargs):
        return ModelStream(
            self, reply=self.decide(chat_ctx, tools), chat_ctx=chat_ctx, tools=tools or [], conn_options=conn_options
        )


class SpeechStream(tts.ChunkedStream):
    async def _run(self, output_emitter):
        output_emitter.initialize(
            request_id="tts-probe", sample_rate=16000, num_channels=1, mime_type="audio/pcm"
        )
        self._tts.spoken.append(self.input_text)
        hold = self._tts.holds.get(self.input_text)
        if hold is not None:
            self._tts.holding.set()
            await hold.wait()
        output_emitter.push(b"\x00\x00" * 160)


class Speaker(tts.TTS):
    def __init__(self):
        super().__init__(
            capabilities=tts.TTSCapabilities(streaming=False), sample_rate=16000, num_channels=1
        )
        self.spoken = []
        self.holds = {}
        self.holding = asyncio.Event()

    def synthesize(self, text, *, conn_options=DEFAULT_API_CONNECT_OPTIONS):
        return SpeechStream(tts=self, input_text=text, conn_options=conn_options)


class AudioOutput(io.AudioOutput):
    def __init__(self):
        super().__init__(label="lifecycle-output", capabilities=io.AudioOutputCapabilities(pause=False))
        self.position = 0.0

    async def capture_frame(self, frame):
        first = self.position == 0.0
        await super().capture_frame(frame)
        self.position += frame.duration
        if first:
            self.on_playback_started(created_at=asyncio.get_running_loop().time())

    def flush(self):
        super().flush()
        if self.position:
            position, self.position = self.position, 0.0
            self.on_playback_finished(playback_position=position, interrupted=False)

    def clear_buffer(self):
        self.position = 0.0


class QuietDesk(agent.Desk):
    """The owner without its spoken greeting, which is not what is under test."""

    async def on_enter(self):
        pass


async def until(predicate, message, timeout=10):
    async with asyncio.timeout(timeout):
        while not predicate():
            await asyncio.sleep(0.005)
    assert predicate(), message


async def journey(*, interrupt=False, alone=False):
    model, speaker = Model(interrupt=interrupt, alone=alone), Speaker()
    if interrupt:
        speaker.holds["Which service would you like?"] = asyncio.Event()
    session = AgentSession(
        userdata=agent.Userdata(),
        llm=model,
        tts=speaker,
        turn_handling={"turn_detection": "manual"},
    )
    session.output.audio = AudioOutput()
    async with session:
        await session.start(QuietDesk(initial=True))
        handle = session.generate_reply(user_input="Hi, I'd like a haircut. My number is 555 010 1010.")
        if interrupt:
            # The caller talks over the booking step's question: the step's
            # speech is interrupted and the answer is a new turn inside the step.
            await until(lambda: speaker.holding.is_set(), "the booking step never asked")
            session.interrupt()
            speaker.holds["Which service would you like?"].set()
            session.generate_reply(user_input="A haircut, and book it.")
        await asyncio.wait_for(handle, 20)
        await until(lambda: len(speaker.spoken) >= (2 if interrupt else 1), "the owner never spoke")
        await asyncio.sleep(0.2)
        owner_items = session.current_agent.chat_ctx.items
    return model, speaker, session.userdata, owner_items


def parse_output(text):
    # The framework stores a dict a tool returned as its repr, not as JSON.
    try:
        return json.loads(text)
    except ValueError:
        return ast.literal_eval(text)


def owner_output(items, delegate):
    outputs = [
        parse_output(item.output)
        for item in items
        if isinstance(item, llm.FunctionCallOutput) and item.name == delegate
    ]
    assert len(outputs) == 1, outputs
    return outputs[0]


async def check_two_steps_one_acknowledgement():
    model, speaker, userdata, items = await journey()
    # Four requests: the owner routes, verification ends on its lookup, booking
    # ends on its tool, and the owner acknowledges. Nothing asked the booking
    # step for a reply after its tool, and nothing asked verification for one.
    assert model.requests == [("owner", False), ("verify", False), ("book", False), ("owner", True)], model.requests
    assert speaker.spoken == ["All set, your haircut is booked."], speaker.spoken
    assert userdata.customer_phone == "+5550101010", userdata.customer_phone
    assert userdata.booking["reference"] == "bkg_0001", userdata.booking
    assert owner_output(items, "do_book") == {"status": "completed"}


async def check_interrupted_step_still_one_acknowledgement():
    model, speaker, userdata, items = await journey(interrupt=True)
    # The booking step spoke once, was interrupted, took the caller's answer,
    # and ended on its tool without a reply of its own. The owner spoke once.
    assert model.requests == [
        ("owner", False), ("verify", False), ("book", False), ("book", False), ("owner", True)
    ], model.requests
    assert speaker.spoken == ["Which service would you like?", "All set, your haircut is booked."], speaker.spoken
    assert userdata.booking["reference"] == "bkg_0001", userdata.booking
    assert owner_output(items, "do_book") == {"status": "completed"}


async def check_isolated_sequence_stops_on_unserved():
    model, speaker, userdata, items = await journey(alone=True)
    # Verification ended unserved, so the booking step never ran: no request
    # was made with its tools, and the owner was handed the unserved status.
    assert model.requests == [("owner", False), ("verify", False), ("owner", True)], model.requests
    assert speaker.spoken == ["All set, your haircut is booked."], speaker.spoken
    assert userdata.booking is None, userdata.booking
    assert owner_output(items, "do_book_alone") == {"status": "unserved"}


async def main():
    await check_two_steps_one_acknowledgement()
    await check_interrupted_step_still_one_acknowledgement()
    await check_isolated_sequence_stops_on_unserved()


asyncio.run(main())
print("terminal lifecycle smoke passed")
`

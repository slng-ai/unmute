"""Count the LLM round trips a caller waits through, per turn, with no network.

`read_langfuse_trace.py` reads a call that already happened. This reads the one
thing that decides how long a turn takes before the call happens: how many
sequential model requests the agent makes between the caller stopping and the
caller hearing something, and which of them a tool `announce` covers.

That number is structural. It comes from the shape of the emitted agent, not
from the provider, so it is knowable without a key, a room, or a phone. The
model, the voice and the ears are local fakes with fixed latencies declared at
the top, so two runs are comparable and the only thing that varies between them
is the agent.

Run it against a compiled LiveKit package:

    python3 scripts/probe_turn_structure.py examples/salon-concierge/build/livekit

It needs `livekit-agents` at the version the package pins, and nothing else:

    pip install "livekit-agents==1.6.10"

What it prints, per caller turn: the number of model requests, how long until
the caller hears anything, and how much of every request is the agent's own
system prompt rather than the conversation. A turn whose first sound arrives
after two or more requests is a turn with an audible hole in it.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import pathlib
import sys
import time
import types
from dataclasses import dataclass

# Latencies of the fakes, in seconds. LLM_TTFT is the one worth changing: set it
# to the p50 `llm_request` latency your own Langfuse traces show, and the
# projection at the end reports what this package's shape costs at that speed.
LLM_TTFT = 0.45
LLM_STREAM = 0.15
TTS_TTFB = 0.20
TTS_PER_CHAR = 0.055
SAMPLE_RATE = 24000


def install_stubs() -> None:
    """Park a stub for every import that wants a network, a GPU or a key.

    The agent module imports them at module scope, so they have to be in
    sys.modules before it is imported. None of them is reached: this probe
    replaces the model, the voice and the transcriber outright.
    """

    def stub(name: str, **attrs: object) -> types.ModuleType:
        module = types.ModuleType(name)
        for key, value in attrs.items():
            setattr(module, key, value)
        sys.modules[name] = module
        return module

    class _VAD:
        @staticmethod
        def load(*args: object, **kwargs: object) -> "_VAD":
            return _VAD()

    stub("dotenv", load_dotenv=lambda *a, **k: None)
    stub("knowledge", build_indexes=lambda *a, **k: None, search=lambda *a, **k: {})
    stub("tracing", setup_langfuse=lambda *a, **k: None)
    stub("dev_metrics", install_dev_metrics=lambda session: session)

    import livekit

    plugins = stub("livekit.plugins")
    plugins.__path__ = []  # type: ignore[attr-defined]
    livekit.plugins = plugins  # type: ignore[attr-defined]
    for name in ("silero", "slng", "openai"):
        setattr(plugins, name, stub(f"livekit.plugins.{name}", VAD=_VAD))


@dataclass
class Event:
    at: float
    kind: str
    detail: str
    scope: str = ""
    prompt: int = 0
    conversation: int = 0


class Probe:
    """One run's timeline. Times are seconds since the run started."""

    def __init__(self) -> None:
        self.started = time.monotonic()
        self.events: list[Event] = []

    def now(self) -> float:
        return time.monotonic() - self.started

    def record(self, kind: str, detail: str, **kw: object) -> None:
        self.events.append(Event(self.now(), kind, detail, **kw))  # type: ignore[arg-type]


def split_context(chat_ctx) -> tuple[int, int]:
    """Characters of system prompt, and characters of everything else.

    The split is the whole point of the measurement: a task exists to keep the
    prompt small, so it is worth knowing which half of a request actually grows.
    """
    prompt = conversation = 0
    for item in chat_ctx.items:
        if item.type == "message":
            size = len(item.text_content or "")
            if item.role == "system":
                prompt += size
            else:
                conversation += size
        elif item.type == "function_call":
            conversation += len(item.name) + len(item.arguments or "")
        elif item.type == "function_call_output":
            conversation += len(str(item.output or ""))
    return prompt, conversation


def build_fakes(probe: Probe, script: list[tuple]):
    """A model that answers from a script, and a voice that takes real time."""
    from livekit.agents import APIConnectOptions, llm
    from livekit.agents import tts as tts_api
    from livekit.agents.llm import ChatChunk, ChoiceDelta, FunctionToolCall

    class ScriptedStream(llm.LLMStream):
        def __init__(self, model, *, chat_ctx, tools, conn_options, reply):
            super().__init__(
                model, chat_ctx=chat_ctx, tools=tools, conn_options=conn_options
            )
            self._reply = reply

        async def _run(self) -> None:
            await asyncio.sleep(LLM_TTFT)
            text, calls = self._reply
            for name, arguments in calls:
                self._event_ch.send_nowait(
                    ChatChunk(
                        id=f"c{int(probe.now() * 1e6)}",
                        delta=ChoiceDelta(
                            role="assistant",
                            tool_calls=[
                                FunctionToolCall(
                                    type="function",
                                    name=name,
                                    arguments=json.dumps(arguments),
                                    call_id=f"call_{name}_{int(probe.now() * 1e6)}",
                                )
                            ],
                        ),
                    )
                )
            if text:
                await asyncio.sleep(LLM_STREAM)
                self._event_ch.send_nowait(
                    ChatChunk(
                        id=f"t{int(probe.now() * 1e6)}",
                        delta=ChoiceDelta(role="assistant", content=text),
                    )
                )

    class ScriptedLLM(llm.LLM):
        """Picks the first scripted reply whose scope and tool both match.

        Matching on the offered tools rather than on a turn counter is what lets
        one script survive a change to the agent: a reply that names a tool the
        current step cannot call is not this step's reply.
        """

        def __init__(self) -> None:
            super().__init__()
            self._script = list(script)

        @property
        def model(self) -> str:
            return "scripted"

        @property
        def provider(self) -> str:
            return "scripted"

        def chat(self, *, chat_ctx, tools=None, conn_options=None, extra_kwargs=None, **kw):
            tools = tools or []
            scope = ""
            if isinstance(extra_kwargs, dict):
                headers = extra_kwargs.get("extra_headers") or {}
                scope = headers.get("X-Slng-Agent-Id", "")
            offered = set(llm.ToolContext(tools).function_tools) if tools else set()
            reply = ("(unscripted)", [])
            for index, (want_scope, want_tool, text, calls) in enumerate(self._script):
                if want_scope and want_scope not in scope:
                    continue
                if want_tool and want_tool not in offered:
                    continue
                reply = (text, calls)
                self._script.pop(index)
                break
            prompt, conversation = split_context(chat_ctx)
            probe.record(
                "request",
                reply[1][0][0] if reply[1] else (reply[0] or "")[:46],
                scope=scope.split(":")[-1] or "-",
                prompt=prompt,
                conversation=conversation,
            )
            return ScriptedStream(
                self,
                chat_ctx=chat_ctx,
                tools=tools,
                conn_options=conn_options or APIConnectOptions(),
                reply=reply,
            )

    class FakeChunk(tts_api.ChunkedStream):
        async def _run(self, emitter) -> None:
            emitter.initialize(
                request_id=f"tts{int(probe.now() * 1e6)}",
                sample_rate=SAMPLE_RATE,
                num_channels=1,
                mime_type="audio/pcm",
            )
            await asyncio.sleep(TTS_TTFB)
            seconds = max(0.3, len(self._input_text) * TTS_PER_CHAR)
            emitter.push(b"\x00\x00" * int(SAMPLE_RATE * seconds))
            emitter.flush()

    class FakeTTS(tts_api.TTS):
        def __init__(self) -> None:
            super().__init__(
                capabilities=tts_api.TTSCapabilities(streaming=False),
                sample_rate=SAMPLE_RATE,
                num_channels=1,
            )

        @property
        def model(self) -> str:
            return "fake"

        @property
        def provider(self) -> str:
            return "fake"

        def synthesize(self, text: str, *, conn_options=None) -> FakeChunk:
            return FakeChunk(
                tts=self,
                input_text=text,
                conn_options=conn_options or APIConnectOptions(),
            )

    return ScriptedLLM(), FakeTTS()


async def drive(emitted, script, turns, *, seed: dict | None = None) -> Probe:
    from livekit.agents import AgentSession

    probe = Probe()
    model, voice = build_fakes(probe, script)
    session = AgentSession[emitted.Userdata](
        llm=model, tts=voice, userdata=emitted.Userdata()
    )
    session.userdata.slng_session_id = "probe"
    for name, value in (seed or {}).items():
        setattr(session.userdata, name, value)

    def on_item(ev) -> None:
        if ev.item.type != "message":
            return
        kind = "sound" if ev.item.role == "assistant" else "caller"
        probe.record(kind, (ev.item.text_content or "")[:58])

    session.on("conversation_item_added", on_item)
    await session.start(emitted.Concierge(initial=True))
    await asyncio.sleep(0.2)

    for label, text in turns:
        probe.record("turn", label)
        result = session.run(user_input=text)
        try:
            await asyncio.wait_for(result, timeout=60)
        except asyncio.TimeoutError:
            probe.record("timeout", label)
        await asyncio.sleep(0.1)

    await session.aclose()
    return probe


def summarise(probe: Probe, title: str) -> None:
    marks = [(e.at, e.detail) for e in probe.events if e.kind == "turn"]
    bounds = marks + [(float("inf"), "")]

    print(f"\n{'=' * 76}\n{title}\n{'=' * 76}")
    print(f"{'t':>6}  {'event':<8} {'scope':<26} {'prompt':>7} {'conv':>6}  detail")
    index = 0
    for event in probe.events:
        while index + 1 < len(bounds) and event.at >= bounds[index + 1][0]:
            index += 1
        print(
            f"{event.at:6.2f}  {event.kind:<8} {event.scope:<26} "
            f"{event.prompt or '':>7} {event.conversation or '':>6}  {event.detail}"
        )

    print(f"\n{'-' * 76}\nPer turn: what the caller waits through\n{'-' * 76}")
    print(f"{'caller turn':<26} {'requests':>8} {'silent':>7} {'1st sound':>10}  covered by")
    rows = []
    for i, (start, label) in enumerate(marks):
        end = bounds[i + 1][0]
        window = [e for e in probe.events if start <= e.at < end]
        requests = [e for e in window if e.kind == "request"]
        sounds = [e for e in window if e.kind == "sound"]
        first = sounds[0] if sounds else None
        silent = len([r for r in requests if not first or r.at < first.at])
        gap = f"{first.at - start:.2f}s" if first else "-"
        rows.append((label, len(requests), silent))
        print(
            f"{label:<26} {len(requests):>8} {silent:>7} {gap:>10}  "
            f"{(first.detail[:34] if first else '(silence)')}"
        )

    print(f"\n{'-' * 76}\nSilence before the first sound, by per-request model latency\n{'-' * 76}")
    speeds = (0.45, 0.80, 1.20, 1.80)
    print(f"{'caller turn':<26}" + "".join(f"{s:>9.2f}s" for s in speeds))
    for label, _, silent in rows:
        print(f"{label:<26}" + "".join(f"{silent * s:>9.2f}s" for s in speeds))

    requests = [e for e in probe.events if e.kind == "request"]
    if requests:
        conversation = [e.conversation for e in requests]
        biggest = max(requests, key=lambda e: e.prompt + e.conversation)
        share = 100 * biggest.prompt / (biggest.prompt + biggest.conversation)
        print(
            f"\nConversation grew {min(conversation)} -> {max(conversation)} characters. "
            f"In the largest request the system prompt is still {share:.0f}% of it."
        )


# The salon package's two delegated steps. Each entry is
# (scope-substring, tool-that-must-be-offered, spoken-text, [(tool, arguments)]).
VERIFY_SCRIPT = [
    ("concierge", "verify_customer", "", [("verify_customer", {})]),
    ("customer_verification", None, "Sure, what's the best number for you?", []),
    ("customer_verification", None, "Is that +34 680 830 464?", []),
    ("customer_verification", "find_or_create_customer", "",
     [("find_or_create_customer", {"phone": "+34680830464"})]),
    ("customer_verification", "finish", "",
     [("finish", {"customer_phone": "+34680830464", "status": "existing",
                  "summary": "known customer", "unserved_request": ""})]),
    ("concierge", None, "Great, you're all set. What can I get booked in?", []),
]

VERIFY_TURNS = [
    ("enter the step", "I need to sort out a booking"),
    ("give the number", "it's 6 8 0 8 3 0 4 6 4, in Spain"),
    ("confirm it", "yes that's right"),
]

BOOKING_SCRIPT = [
    ("concierge", "manage_booking", "", [("manage_booking", {})]),
    ("booking", None, "What day were you thinking?", []),
    ("booking", "get_current_date", "", [("get_current_date", {})]),
    ("booking", "check_availability", "",
     [("check_availability", {"service": "haircut", "date": "2026-09-02"})]),
    ("booking", None, "I've got 9:00 AM, 11:30, or 3:00 in the afternoon.", []),
    ("booking", None, "Tomorrow at 3:00 PM for a haircut, shall I book it?", []),
    ("booking", "create_booking", "",
     [("create_booking", {"service": "haircut", "slot_id": "SLOT", "confirmed": True})]),
    ("booking", "finish", "",
     [("finish", {"action": "create", "booking_id": "B1", "status": "confirmed",
                  "summary": "haircut booked", "unserved_request": ""})]),
    ("concierge", None, "You're all booked in. Anything else?", []),
]

BOOKING_TURNS = [
    ("enter the step", "a haircut please"),
    ("name a relative day", "tomorrow if possible"),
    ("pick a time", "3 in the afternoon works"),
    ("say yes", "yes, book it"),
]


async def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("build_dir", type=pathlib.Path,
                        help="a compiled LiveKit package, e.g. examples/<pkg>/build/livekit")
    args = parser.parse_args()

    build = args.build_dir.resolve()
    if not (build / "agent.py").exists():
        print(f"no agent.py under {build}: compile the package first", file=sys.stderr)
        return 1

    install_stubs()
    sys.path.insert(0, str(build))
    import agent as emitted

    summarise(
        await drive(emitted, VERIFY_SCRIPT, VERIFY_TURNS),
        "verification step",
    )
    summarise(
        await drive(emitted, BOOKING_SCRIPT, BOOKING_TURNS,
                    seed={"customer_phone": "+34680830464"}),
        "booking step",
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))

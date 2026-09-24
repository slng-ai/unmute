"""Drive a compiled Pipecat bot through a scripted text conversation.

The Pipecat twin of text_run_livekit.py. The emitted agents run on a real
WorkerRunner and FlowManager, with the package's real think model and its real
local tools, and no transport, STT or TTS. Each caller line goes in as a user
message, the way a finished caller turn reaches the context, and the script
prints what the agents said, every tool call and its result, and the state.

    uv run --project <package>/build/pipecat python scripts/text_run_pipecat.py \
        <package> --line "Hi, I'd like a haircut tomorrow afternoon." --line "Yes."

A caller id is seeded the way `unmute dev --source` seeds one. Pass
--from-number "" for a call that carries none, such as the web channel.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import re
import sys
import time
from pathlib import Path

from text_run_livekit import seed_unused_env, state_of


def parse() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    parser.add_argument("package", help="the package directory, compiled for pipecat")
    parser.add_argument(
        "--line", action="append", default=[], help="one caller turn; repeat in order"
    )
    parser.add_argument(
        "--from-number",
        default="+15005550006",
        help='the caller id the pre-fetch reads (E.164); "" for none',
    )
    parser.add_argument(
        "--quiet-secs",
        type=float,
        default=2.0,
        help="how long nothing may move before a turn counts as finished",
    )
    return parser.parse_args()


async def run(args: argparse.Namespace) -> None:
    package = Path(args.package).resolve()
    build = package / "build" / "pipecat"
    if not (build / "bot.py").exists():
        sys.exit(f"{build} holds no bot.py; compile the package for pipecat first")
    sys.path.insert(0, str(build))
    os.chdir(build)
    if args.from_number:
        os.environ["UNMUTE_CALL_FACTS"] = json.dumps({"from_number": args.from_number})
    else:
        os.environ.pop("UNMUTE_CALL_FACTS", None)
    seed_unused_env(package, build)

    # After sys.path and cwd are set, so the emitted module and its tools load.
    import bot  # noqa: PLC0415
    from pipecat.bus import BusBridgeProcessor  # noqa: PLC0415
    from pipecat.frames.frames import (  # noqa: PLC0415
        EndFrame,
        FunctionCallInProgressFrame,
        FunctionCallResultFrame,
        LLMFullResponseEndFrame,
        LLMFullResponseStartFrame,
        LLMMessagesAppendFrame,
        LLMTextFrame,
        TTSSpeakFrame,
    )
    from pipecat.pipeline.pipeline import Pipeline  # noqa: PLC0415
    from pipecat.pipeline.worker import PipelineWorker  # noqa: PLC0415
    from pipecat.processors.aggregators.llm_context import LLMContext  # noqa: PLC0415
    from pipecat.processors.aggregators.llm_response_universal import (  # noqa: PLC0415
        LLMContextAggregatorPair,
    )
    from pipecat.processors.frame_processor import FrameProcessor  # noqa: PLC0415
    from pipecat.workers.llm import LLMWorkerActivationArgs  # noqa: PLC0415
    from pipecat.workers.runner import WorkerRunner  # noqa: PLC0415

    moved = [time.monotonic()]
    replies = [0]
    said = [0]

    class Voice(FrameProcessor):
        """Stands in for one agent's TTS: prints what it would have spoken.

        Every frame still travels on, so the assistant aggregator records the
        reply exactly as it would behind a real voice.
        """

        def __init__(self, agent: str) -> None:
            super().__init__(name=f"{agent}::voice")
            self.agent = agent
            self.words: list[str] = []

        async def process_frame(self, frame, direction):
            await super().process_frame(frame, direction)
            moved[0] = time.monotonic()
            if isinstance(frame, TTSSpeakFrame):
                said[0] += 1
                print(f"   [{self.agent}] {frame.text!r}  (spoken line)")
            elif isinstance(frame, LLMFullResponseStartFrame):
                self.words = []
            elif isinstance(frame, LLMTextFrame):
                self.words.append(frame.text)
            elif isinstance(frame, LLMFullResponseEndFrame):
                replies[0] += 1
                if "".join(self.words).strip():
                    said[0] += 1
                    print(f"   [{self.agent}] {''.join(self.words).strip()!r}")
            elif isinstance(frame, FunctionCallInProgressFrame):
                print(f"   [call] {frame.function_name}({json.dumps(frame.arguments, ensure_ascii=False)})")
            elif isinstance(frame, FunctionCallResultFrame):
                out = json.dumps(frame.result, ensure_ascii=False, default=str)
                print(f"   [result] {frame.function_name}: {out[:300]}")
            await self.push_frame(frame, direction)

    # Only the voices are replaced. Every build_<agent>_tts the module emits
    # becomes a stand-in named after its agent.
    for name in [n for n in vars(bot) if n.startswith("build_") and n.endswith("_tts")]:
        agent = name.removeprefix("build_").removesuffix("_tts")
        setattr(bot, name, lambda agent=agent: Voice(agent))

    context = LLMContext()
    state = bot.build_state({})
    await bot._prefetch(state, {})
    print("prefetch:", state_of(state))
    agents = [
        cls(state=state, context=context, call_context={})
        for cls in vars(bot).values()
        if isinstance(cls, type)
        and issubclass(cls, bot.TracedLLMWorker)
        and cls is not bot.TracedLLMWorker
        and cls.__module__ == bot.__name__
    ]
    # The entry agent and its greeting are what _run_bot's activate_entry()
    # activates and speaks. Read from the source so this follows the emitter.
    opening = (build / "bot.py").read_text().split("async def activate_entry", 1)[1]
    first = re.search(r'main\.activate_worker\(\s*"([^"]+)"', opening)
    entry = next(a for a in agents if first and a.name == first.group(1))
    spoken = re.search(r'TTSSpeakFrame\(\s*"((?:[^"\\]|\\.)*)"', opening.split("_end_after", 1)[0])
    greeting = spoken.group(1) if spoken else None
    if greeting:
        context.add_message({"role": "assistant", "content": greeting})
        print(f"   [{entry.name}] {greeting!r}  (greeting)")

    class Clock(FrameProcessor):
        async def process_frame(self, frame, direction):
            await super().process_frame(frame, direction)
            moved[0] = time.monotonic()
            await self.push_frame(frame, direction)

    runner = WorkerRunner(handle_sigint=False)
    user, assistant = LLMContextAggregatorPair(context)
    main = PipelineWorker(
        Pipeline([user, BusBridgeProcessor(bus=runner.bus, worker_name="main"), Clock(), assistant]),
        name="main",
    )
    ready = asyncio.Event()
    started = asyncio.Event()

    @runner.event_handler("on_ready")
    async def on_ready(runner):
        ready.set()

    @main.event_handler("on_pipeline_started")
    async def on_started(worker, frame):
        await ready.wait()
        await runner.add_workers(*agents)
        await main.activate_worker(entry.name, args=LLMWorkerActivationArgs(run_llm=False))
        started.set()

    async def settle(before: int, timeout: float = 90.0) -> None:
        """Wait for at least one reply, then for nothing to move for a while."""
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            await asyncio.sleep(0.1)
            if replies[0] > before and time.monotonic() - moved[0] >= args.quiet_secs:
                return
        print("   <no reply settled within the timeout>")

    await runner.add_workers(main)
    running = asyncio.create_task(runner.run())
    try:
        await asyncio.wait_for(started.wait(), timeout=60)
        # activate_worker only sends the request over the bus. A caller line
        # that lands before the agent has taken it is added to the context and
        # never answered, which a first run showed as a 90 second silence.
        while not entry.active:
            await asyncio.sleep(0.05)
        for i, line in enumerate(args.line, 1):
            print(f"\n=== turn {i}: caller says {line!r}")
            before, spoke = replies[0], said[0]
            await main.queue_frame(
                LLMMessagesAppendFrame([{"role": "user", "content": line}], run_llm=True)
            )
            await settle(before)
            if said[0] == spoke:
                print("   <silence: the agent said nothing this turn>")
            print("   state:", state_of(state))
        print("\n=== final state")
        print(state_of(state))
    finally:
        await main.queue_frame(EndFrame())
        await runner.cancel(reason="text run complete")
        await running


if __name__ == "__main__":
    asyncio.run(run(parse()))

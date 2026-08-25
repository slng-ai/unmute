//go:build smoke

package generate

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// pipecatInlineSmokeScript proves the inline single-agent emission (F3) against
// real pinned pipecat-ai 1.7.0 + pipecat-slng. Besides construction, it invokes
// the real DirectFunctionWrapper with the malformed argument shape seen in the
// live call and forces a local handler failure before result_callback.
const pipecatInlineSmokeScript = `"""Smoke check: the inline single-agent bot imports and constructs, no bus."""
import asyncio
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import bot  # noqa: E402
from pipecat.adapters.schemas.direct_function import DirectFunctionWrapper  # noqa: E402
from pipecat.processors.aggregators.llm_context import LLMContext  # noqa: E402

src = open("bot.py").read()
assert "BusBridgeProcessor" not in src, "inline bot must not build the bus"
assert "activate_worker(" not in src, "inline bot must not activate workers"

assert type(bot.build_stt()).__name__ == "SlngSTTService"
assert type(bot.build_appointment_desk_llm()).__name__ == "OpenAILLMService"
assert type(bot.build_appointment_desk_tts()).__name__ == "SlngTTSService"

# The generated module-level tool functions are valid direct functions.
LLMContext(
    tools=[
        bot.lookup_customer,
        bot.create_customer,
        bot.check_availability,
        bot.book_appointment,
        bot.cancel_appointment,
    ]
)


class Params:
    function_name = "check_availability"

    def __init__(self, callback):
        self.result_callback = callback


async def invoke(arguments):
    results = []

    async def result_callback(result, **_kwargs):
        results.append(result)

    await DirectFunctionWrapper(bot.check_availability).invoke(
        arguments,
        Params(result_callback),
    )
    assert len(results) == 1, results
    return results[0]


async def exercise_tool_boundary():
    wrapper = DirectFunctionWrapper(bot.check_availability)
    schema = wrapper.to_function_schema()
    assert set(schema.properties) == {"date", "service"}, schema.properties

    malformed = await invoke(
        {"date": "2026-08-16", "service": "haircut", "customer_id": "cus_1042"}
    )
    assert "Unexpected arguments" in malformed["error"], malformed
    assert "customer_id" in malformed["error"], malformed

    original = bot.tools.check_availability.check_availability

    def fail_before_result(service, date):
        raise RuntimeError("private provider detail")

    bot.tools.check_availability.check_availability = fail_before_result
    try:
        failed = await invoke({"date": "2026-08-16", "service": "haircut"})
    finally:
        bot.tools.check_availability.check_availability = original
    assert "failed before completing" in failed["error"], failed
    assert "private provider detail" not in failed["error"], failed

    corrected = await invoke({"date": "2026-08-16", "service": "haircut"})
    assert len(corrected["slots"]) == 3, corrected


asyncio.run(exercise_tool_boundary())
print("inline instantiation ok")
`

const pipecatLoggingSmokeScript = `"""Smoke check: a later session keeps active Loguru sinks."""
import asyncio
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import bot  # noqa: E402


async def fake_create_transport(*_args, **_kwargs):
    return object()


async def fake_run_bot(*_args, **_kwargs):
    pass


async def main():
    bot.create_transport = fake_create_transport
    bot.run_bot = fake_run_bot
    await bot.bot(object())

    messages = []
    sink = bot.logger.add(
        lambda message: messages.append(str(message)),
        level="INFO",
        format="{message}",
    )
    try:
        await bot.bot(object())
        bot.logger.info("active sink survived")
    finally:
        bot.logger.remove(sink)

    assert any("active sink survived" in message for message in messages), messages


asyncio.run(main())
print("pipecat logging idempotence ok")
`

const pipecatIdleResumeSmokeScript = `"""Resumed speech cancels a pending idle hangup on Pipecat 1.7."""
import asyncio
import json
import os
from types import SimpleNamespace

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import bot  # noqa: E402
from loguru import logger  # noqa: E402
from pipecat.processors.frame_processor import FrameProcessor  # noqa: E402


class Transport:
    def input(self):
        return FrameProcessor()

    def output(self):
        return FrameProcessor()

    def event_handler(self, _name):
        return lambda handler: handler


captured = {}
end_started = asyncio.Event()
end_cancelled = asyncio.Event()
result = {}
real_pair = bot.LLMContextAggregatorPair


def capture_pair(*args, **kwargs):
    pair = real_pair(*args, **kwargs)
    captured["user"] = pair.user()
    return pair


async def pending_end(*_args):
    end_started.set()
    try:
        await asyncio.Event().wait()
    except asyncio.CancelledError:
        end_cancelled.set()
        raise


class Runner:
    def __init__(self, **_kwargs):
        pass

    async def add_workers(self, *workers):
        self.workers = workers

    async def run(self):
        user = captured["user"]

        async def discard(*_args, **_kwargs):
            pass

        user.push_frame = discard
        await user._call_event_handler("on_user_turn_idle")
        await end_started.wait()
        await user._call_event_handler("on_user_turn_started", object())
        try:
            await asyncio.wait_for(end_cancelled.wait(), timeout=0.25)
            result["cancelled"] = True
        except TimeoutError:
            result["cancelled"] = False


async def main():
    bot.LLMContextAggregatorPair = capture_pair
    bot.SileroVADAnalyzer = lambda: None
    bot.build_stt = FrameProcessor
    bot.build_appointment_desk_llm = FrameProcessor
    bot.build_appointment_desk_tts = FrameProcessor
    bot.WorkerRunner = Runner
    bot._end_after = pending_end

    messages = []
    sink = logger.add(lambda message: messages.append(str(message)), format="{message}")
    try:
        await bot.run_bot(Transport(), SimpleNamespace(handle_sigint=False))
    finally:
        logger.remove(sink)

    assert result["cancelled"], "a resumed user turn left the idle hangup pending"
    assert not any(
        "event handler on_user_turn_resumed not registered" in message
        for message in messages
    ), messages


asyncio.run(main())
print("pipecat idle resume smoke ok")
`

const pipecatHandoffAnnouncementSmokeScript = `"""V2: source playout finishes before receiver activation."""
import asyncio
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import bot  # noqa: E402
from pipecat.frames.frames import (  # noqa: E402
    BotStartedSpeakingFrame,
    BotStoppedSpeakingFrame,
    TTSSpeakFrame,
)
from pipecat.pipeline.worker import PipelineWorker  # noqa: E402


class Params:
    function_name = "to_appointment_manager"

    async def result_callback(self, _result, **_kwargs):
        pass


async def exercise_handoff():
    source = bot.BookingDeskAgent()
    events = []
    original_queue = PipelineWorker.queue_frame

    async def capture_queue(worker, frame, *_args, **_kwargs):
        if isinstance(frame, TTSSpeakFrame):
            events.append(("speak", frame.text))
            await worker.queue_frame(BotStartedSpeakingFrame())
            events.append(("started",))
            await worker.queue_frame(BotStoppedSpeakingFrame())
            events.append(("stopped",))
        elif isinstance(frame, (BotStartedSpeakingFrame, BotStoppedSpeakingFrame)):
            events.append(("replayed", type(frame).__name__))

    async def capture_activate(name, **kwargs):
        assert "messages" not in kwargs, kwargs
        activation = kwargs["args"]
        assert activation.metadata is None
        assert activation.run_llm is True
        assert activation.messages == [
            {
                "role": "developer",
                "content": "The caller wants to reschedule or cancel an existing appointment.",
            }
        ]
        assert name == "appointment_manager"
        source._active = False
        events.append(("activate", name))

    async def capture_flush(*_args, **_kwargs):
        return True

    PipelineWorker.queue_frame = capture_queue
    source._active = True
    source.activate_worker = capture_activate
    source.flush_pipeline = capture_flush
    try:
        registered = source.llm._functions["to_appointment_manager"].handler
        await asyncio.wait_for(registered.invoke({}, Params()), timeout=1.0)
    finally:
        PipelineWorker.queue_frame = original_queue

    assert events == [
        ("speak", "I’m connecting you with our appointment manager now."),
        ("started",),
        ("stopped",),
        ("activate", "appointment_manager"),
    ], events


asyncio.run(exercise_handoff())
print("handoff announcement smoke ok")
`

// TestSmokePipecatV1InlineInstantiates proves the inline single-agent shape (F3)
// end to end on real pipecat-ai: no bus, LLM inline, tools as direct functions
// on LLMContext. simple-prompt ships tracing, which the inline path excludes, so
// the smoke clears it.
func TestSmokePipecatV1InlineInstantiates(t *testing.T) {
	runPipecatSmokeScript(t, "simple-prompt", nil, func(agent *ir.Agent) {
		agent.Tracing = nil
	}, pipecatInlineSmokeScript)
}

func TestSmokePipecatV1LoggingIsConfiguredOnce(t *testing.T) {
	runPipecatSmokeScript(t, "simple-prompt", nil, nil, pipecatLoggingSmokeScript)
}

// TestSmokePipecatV1UserTurnCancelsIdleHangup runs the public generated worker
// against the supported SDK. Its real aggregator must accept the event and the
// resumed turn must cancel the already-started inactivity hangup.
func TestSmokePipecatV1UserTurnCancelsIdleHangup(t *testing.T) {
	runPipecatSmokeScript(t, "simple-prompt", nil, func(agent *ir.Agent) {
		agent.Tracing = nil
		agent.Conversation.Interruption.MinimumWords = 1
		agent.Conversation.Inactivity = &ir.Inactivity{NudgeAfter: "1s", EndAfter: "2s"}
	}, pipecatIdleResumeSmokeScript)
}

// TestSmokePipecatV1BuiltinEndCall proves the emitted bodyless end_call @tool
// imports and constructs in a real venv (prebuilt-tools T11).
func TestSmokePipecatV1BuiltinEndCall(t *testing.T) {
	runPipecatSmokeScript(t, "simple-prompt", nil, func(agent *ir.Agent) {
		agent.Tracing = nil
		addBuiltinEndCall(agent)
	}, pipecatInlineSmokeScript)
}

func TestSmokeV2PipecatAgentTransferAnnouncementWaitsForSourcePlayout(t *testing.T) {
	runPipecatSmokeScript(t, "subagents", nil, nil, pipecatHandoffAnnouncementSmokeScript)
}

func examplePackagePath(name string) string {
	if name == "remy" || name == "safe_core" || name == "daily_carrier" {
		return filepath.Join("..", "testdata", name)
	}
	return filepath.Join("..", "..", "examples", name)
}

// smokeCheckScript imports the emitted bot and instantiates every service
// builder with placeholder env values. Importing alone proves the imports and
// dependency set; calling the builders proves the constructor kwargs against
// the real installed services — the drift class py_compile can never see
// (driver-pipecat B6).
const smokeCheckScript = `"""Smoke check: import the generated bot and instantiate every service."""
import asyncio
import inspect
import json
import os
from pathlib import Path

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import bot  # noqa: E402

if Path("tracing.py").exists():
    import tracing
    from opentelemetry import trace
    from opentelemetry.sdk.trace import TracerProvider

    if hasattr(tracing, "setup_langfuse_tracing"):
        for name in ("LANGFUSE_SECRET_KEY", "LANGFUSE_PUBLIC_KEY", "LANGFUSE_BASE_URL"):
            os.environ.pop(name, None)
        try:
            tracing.setup_langfuse_tracing()
        except ValueError:
            pass
        else:
            raise AssertionError("configured Langfuse tracing requires all credentials")
    elif hasattr(tracing, "setup_coval_tracing"):
        # Unlike Langfuse, a missing COVAL_API_KEY is not fatal here (an
        # evaluation credential must never gate a live call), so this exercises
        # real construction and the cached-provider idempotence guard instead
        # of a raise.
        os.environ.pop("COVAL_API_KEY", None)
        provider = tracing.setup_coval_tracing()
        assert isinstance(provider, TracerProvider), provider
        assert trace.get_tracer_provider() is provider, "setup did not install the global tracer provider"
        assert tracing.setup_coval_tracing() is provider, "second call must reuse the cached provider"
    else:
        raise AssertionError(
            "tracing.py exposes neither setup_langfuse_tracing nor setup_coval_tracing: "
            + repr(sorted(n for n in vars(tracing) if not n.startswith("_")))
        )

builders = sorted(n for n in vars(bot) if n.startswith("build_") and callable(getattr(bot, n)))
assert builders, "no service builders found in bot.py"


async def _run() -> None:  # some services construct an aiohttp session
    for name in builders:
        builder = getattr(bot, name)
        # A Context Router LLM builder takes a call-scoped slng_session_id
        # (unique per call, so the cache scope never collides across calls);
        # every other builder is still zero-arg.
        kwargs = {}
        if "slng_session_id" in inspect.signature(builder).parameters:
            kwargs["slng_session_id"] = "smoke-session"
        builder(**kwargs)


asyncio.run(_run())
print("smoke ok:", ", ".join(builders))
`

const pipecatTaskRoleSmokeScript = `"""Smoke check: task role replaces, then restores, the owner role."""
import asyncio
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import bot  # noqa: E402
from pipecat.flows import FlowManager  # noqa: E402
from pipecat.frames.frames import Frame  # noqa: E402
from pipecat.processors.aggregators.llm_context import LLMContext  # noqa: E402
from pipecat.processors.aggregators.llm_response_universal import LLMContextAggregatorPair  # noqa: E402
from pipecat.processors.frame_processor import FrameDirection, FrameProcessor  # noqa: E402
from pipecat.services.llm_service import LLMService  # noqa: E402
from pipecat.services.settings import LLMSettings  # noqa: E402
from pipecat.workers.runner import WorkerRunner  # noqa: E402

OWNER_PROMPT = None
original_owner_builder = bot.build_appointment_desk_llm


class FakeLLM(LLMService):
    def __init__(self) -> None:
        super().__init__(settings=LLMSettings(model="smoke", system_instruction=OWNER_PROMPT))

    async def process_frame(self, frame: Frame, direction: FrameDirection) -> None:
        await super().process_frame(frame, direction)
        await self.push_frame(frame, direction)


class Passthrough(FrameProcessor):
    async def process_frame(self, frame: Frame, direction: FrameDirection) -> None:
        await super().process_frame(frame, direction)
        await self.push_frame(frame, direction)


for name in list(vars(bot)):
    if name.startswith("build_") and name.endswith("_llm"):
        setattr(bot, name, FakeLLM)
    elif name.startswith("build_") and name.endswith("_tts"):
        setattr(bot, name, Passthrough)


async def main() -> None:
    global OWNER_PROMPT
    OWNER_PROMPT = original_owner_builder()._settings.system_instruction
    context = LLMContext()
    owner = bot.AppointmentDeskAgent(state=None, context=context, call_context=None)
    target = bot.AftercareAgent(state=None, context=context, call_context=None)
    runner = WorkerRunner(handle_sigint=False)
    await runner.add_workers(owner, target)
    run_task = asyncio.create_task(runner.run(auto_end=False))
    await asyncio.wait_for(owner._pipeline_start_event.wait(), timeout=5)
    await asyncio.wait_for(target._pipeline_start_event.wait(), timeout=5)

    owner._active = True
    owner._manage_appointment_results = {}
    owner._manage_appointment_snapshot = (
        [dict(message) for message in context.get_messages()],
        context.tools,
    )
    flow = FlowManager(
        llm=owner.llm,
        context_aggregator=LLMContextAggregatorPair(context),
        worker=owner,
    )
    node = owner._manage_appointment_node_identify_customer()
    await flow.initialize(node)
    await owner.flush_pipeline()
    assert owner.llm._settings.system_instruction == node["role_message"]
    assert owner.llm._settings.system_instruction != OWNER_PROMPT

    await owner._manage_appointment_finish_finalize_appointment(
        {"action": "book", "status": "ok", "appointment_id": "apt-smoke"},
        flow,
    )
    assert owner.llm._settings.system_instruction == OWNER_PROMPT
    for _ in range(100):
        if target.active:
            break
        await asyncio.sleep(0.01)
    assert target.active, "then: transfer did not activate the target worker"

    await runner.cancel("task role smoke complete")
    await asyncio.wait_for(run_task, timeout=5)
    print("task role smoke ok")


asyncio.run(main())
`

const pipecatTaskTransferSmokeScript = `"""Smoke check: task transfer obeys Pipecat 1.7 Flow termination."""
import asyncio
import json
import os
import subprocess

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import bot  # noqa: E402
from pipecat.bus import AsyncQueueBus, BusActivateWorkerMessage  # noqa: E402
from pipecat.flows import FlowManager, NO_RESPONSE  # noqa: E402
from pipecat.frames.frames import LLMSetToolsFrame  # noqa: E402
from pipecat.processors.aggregators.llm_context import LLMContext  # noqa: E402
from pipecat.processors.aggregators.llm_response_universal import LLMContextAggregatorPair  # noqa: E402
from pipecat.services.llm_service import FunctionCallParams  # noqa: E402
from pipecat.utils.asyncio.task_manager import TaskManager  # noqa: E402


async def main() -> None:
    context = LLMContext()
    owner = bot.IntakeAgent(
        state=bot.State(customer_id=None, verified=False),
        context=context,
        call_context=None,
    )
    original_queue_frame = owner.queue_frame
    original_queue_frames = owner.queue_frames
    original_flush_pipeline = owner.flush_pipeline
    activation_frames = []

    async def capture_activation_frame(frame, *args, **kwargs):
        activation_frames.append(frame)

    owner.queue_frame = capture_activation_frame
    owner.state.customer_id = "prompt-smoke"
    await owner.on_activated({
        "messages": [{"role": "developer", "content": "Continue."}],
        "run_llm": True,
    })
    assert [type(frame).__name__ for frame in activation_frames] == [
        "LLMUpdateSettingsFrame", "LLMSetToolsFrame", "LLMMessagesAppendFrame",
    ], "owner prompt must be restored before activation tools and messages"
    assert activation_frames[0].delta.system_instruction.endswith(
        "Current customer: prompt-smoke."
    ), "templated owner prompt did not render through self.state"
    owner.queue_frame = original_queue_frame
    owner.state.customer_id = None
    flow = FlowManager(
        llm=owner.llm,
        context_aggregator=LLMContextAggregatorPair(context),
        worker=owner,
    )
    owner_tools = context.tools
    flow_frames = []

    async def capture_flow_frames(frames, *args, **kwargs):
        flow_frames.extend(frames)

    owner.queue_frames = capture_flow_frames
    await flow.initialize(owner._run_verify_node_verify())
    owner.queue_frames = original_queue_frames
    task_tools = next(
        frame.tools for frame in flow_frames if isinstance(frame, LLMSetToolsFrame)
    )
    registered = task_tools.standard_tools
    handlers = {function.name: function.handler for function in registered}
    owner_messages = [{"role": "user", "content": "I need help."}]
    task_messages = [
        {"role": "developer", "content": "Begin this step."},
        {"role": "user", "content": "This is really about billing."},
        {
            "role": "assistant",
            "content": None,
            "tool_calls": [{
                "id": "call-smoke",
                "type": "function",
                "function": {"name": "lookup_customer", "arguments": "{}"},
            }],
        },
        {"role": "tool", "tool_call_id": "call-smoke", "content": "found"},
        {"role": "assistant", "content": "I can connect you."},
    ]

    def reset_task_context() -> None:
        context.set_messages(owner_messages + task_messages)
        context.set_tools(task_tools)
        owner._run_verify_snapshot = ([dict(message) for message in owner_messages], owner_tools)
        owner._run_verify_results = {}
        owner._run_verify_active_step = "verify"

    reset_task_context()
    refused, next_node = await owner._run_verify_transfer_verify_to_billing({}, flow)
    assert "still need" in refused["refused"]
    assert next_node is None, "recoverable refusal must keep the task LLM active"
    assert owner._run_verify_active_step == "verify"

    announcements = []

    async def announce(text):
        announcements.append(text)

    async def fail_activation(*args, **kwargs):
        raise RuntimeError("activation failed")

    owner._announce_handoff = announce
    owner.activate_worker = fail_activation
    owner.state.customer_id = "cus-smoke"
    failed_transfer_frames = []

    async def capture_failed_transfer_frame(frame, *args, **kwargs):
        failed_transfer_frames.append(frame)

    owner.queue_frame = capture_failed_transfer_frame
    try:
        await owner._run_verify_transfer_verify_to_billing({}, flow)
    except RuntimeError as error:
        assert str(error) == "activation failed"
    else:
        raise AssertionError("activation failure was swallowed")
    assert owner._run_verify_active_step == "verify"
    assert not failed_transfer_frames, "failed task handoff changed the active task prompt"
    assert context.get_messages() == owner_messages + task_messages
    assert context.tools is task_tools, "failed transfer did not restore the active Flow tools"
    owner.queue_frame = original_queue_frame

    activations = []

    async def activate(name, *, args, deactivate_self):
        activations.append((name, args, deactivate_self))

    owner.activate_worker = activate
    callbacks = []

    async def result_callback(result, **kwargs):
        callbacks.append((result, kwargs.get("properties")))

    transition = handlers["to_billing"]
    assert transition is not None
    await transition(FunctionCallParams(
        function_name="to_billing",
        tool_call_id="transfer-smoke",
        arguments={},
        llm=owner.llm,
        pipeline_worker=owner,
        context=context,
        result_callback=result_callback,
    ))
    assert callbacks[-1][0] == {"transferred": True}
    assert callbacks[-1][1].run_llm is False, "NO_RESPONSE must suppress the source LLM"
    assert len(activations) == 1
    assert [message["role"] for message in context.get_messages()] == [
        "user", "user", "assistant", "tool", "assistant",
    ], "history: full lost task records or retained Flow developer controls"

    already, next_node = await owner._run_verify_transfer_verify_to_billing({}, flow)
    assert already == {"status": "already handled"}
    assert next_node is NO_RESPONSE
    assert len(activations) == 1, "a completed transfer ran twice"

    callbacks.clear()
    stale = handlers["finish_run_verify_verify"]
    assert stale is not None
    await stale(FunctionCallParams(
        function_name="finish_run_verify_verify",
        tool_call_id="finish-smoke",
        arguments={"verified": True},
        llm=owner.llm,
        pipeline_worker=owner,
        context=context,
        result_callback=result_callback,
    ))
    assert callbacks[-1][0] == {"status": "already handled"}
    assert callbacks[-1][1].run_llm is False
    assert "verify" not in owner._run_verify_results

    # The generated topology uses Pipecat's local AsyncQueueBus. Its activation
    # dispatch must not overtake the Flow wrapper's result callback.
    bus_events = []
    activation_dispatched = asyncio.Event()

    class BillingSubscriber:
        name = "billing"

        async def on_bus_message(self, message):
            if isinstance(message, BusActivateWorkerMessage):
                bus_events.append("activation-dispatched")
                activation_dispatched.set()

    bus = AsyncQueueBus()
    await bus.setup(TaskManager())
    await bus.subscribe(BillingSubscriber())
    await bus.start()
    try:
        reset_task_context()

        async def activate_on_bus(name, *, args, deactivate_self):
            await bus.send(BusActivateWorkerMessage(
                source=owner.name,
                target=name,
                args=args.to_dict(),
            ))
            bus_events.append("activation-enqueued")

        async def bus_result_callback(result, **kwargs):
            assert result == {"transferred": True}
            assert kwargs["properties"].run_llm is False
            bus_events.append("result-callback")

        owner.activate_worker = activate_on_bus
        await transition(FunctionCallParams(
            function_name="to_billing",
            tool_call_id="bus-order-smoke",
            arguments={},
            llm=owner.llm,
            pipeline_worker=owner,
            context=context,
            result_callback=bus_result_callback,
        ))
        bus_events.append("wrapper-returned")
        assert bus_events == [
            "activation-enqueued", "result-callback", "wrapper-returned",
        ], "local activation dispatched before the Flow result was committed"
        await asyncio.wait_for(activation_dispatched.wait(), timeout=1)
        assert bus_events[-1] == "activation-dispatched"
    finally:
        await bus.stop()

    # Reverse the terminal-call order. Finishing first owns this step and makes
    # a same-batch transfer stale; transfer-first above made finish stale.
    reset_task_context()
    owner.activate_worker = activate
    callbacks.clear()
    exact_result = {
        "verified": True,
        "label": "customer-smoke",
    }
    await handlers["finish_run_verify_verify"](FunctionCallParams(
        function_name="finish_run_verify_verify",
        tool_call_id="exact-result-smoke",
        arguments=exact_result,
        llm=owner.llm,
        pipeline_worker=owner,
        context=context,
        result_callback=result_callback,
    ))
    expected = {"status": "ok", "result": exact_result}
    assert callbacks[-1][0] == expected, "registered Flow handler changed the typed result"
    assert callbacks[-1][1].run_llm is False, "next task must own the next LLM turn"
    assert callbacks[-1][1].on_context_updated is not None
    assert owner._run_verify_results["verify"] == exact_result
    assert owner._run_verify_active_step == "complete"

    callbacks.clear()
    await transition(FunctionCallParams(
        function_name="to_billing",
        tool_call_id="stale-transfer-smoke",
        arguments={},
        llm=owner.llm,
        pipeline_worker=owner,
        context=context,
        result_callback=result_callback,
    ))
    assert callbacks[-1][0] == {"status": "already handled"}
    assert callbacks[-1][1].run_llm is False
    assert len(activations) == 1, "a stale transfer activated after finish claimed the step"

    # A failed final completion restores the task prompt and releases its claim
    # only after messages, tools, and prompt are safe for a model retry.
    prompt_restores = []
    flush_count = 0

    async def capture_prompt_restore(frame, *args, **kwargs):
        prompt_restores.append(frame.delta.system_instruction)

    async def fail_first_flush():
        nonlocal flush_count
        flush_count += 1
        if flush_count == 1:
            raise RuntimeError("role restore failed")

    owner.queue_frame = capture_prompt_restore
    owner.flush_pipeline = fail_first_flush
    before_final_messages = [dict(message) for message in context.get_messages()]
    before_final_tools = context.tools
    try:
        await owner._run_verify_finish_complete({"complete": True}, flow)
    except RuntimeError as error:
        assert str(error) == "role restore failed"
    else:
        raise AssertionError("final completion failure was swallowed")
    assert owner._run_verify_active_step == "complete"
    assert "complete" not in owner._run_verify_results
    assert context.get_messages() == before_final_messages
    assert context.tools is before_final_tools
    assert prompt_restores[0].endswith("Current customer: cus-smoke.")
    # The step's prompt continues with the compiler's finish contract.
    assert prompt_restores[1].startswith("Complete verification.")

    prompt_restores.clear()
    flush_count = 0

    async def fail_restore_and_rollback():
        nonlocal flush_count
        flush_count += 1
        if flush_count == 1:
            raise RuntimeError("owner prompt restore failed")
        raise RuntimeError("task prompt rollback failed")

    owner.flush_pipeline = fail_restore_and_rollback
    try:
        await owner._run_verify_finish_complete({"complete": True}, flow)
    except RuntimeError as error:
        assert str(error) == "owner prompt restore failed"
        assert str(error.__cause__) == "task prompt rollback failed"
    else:
        raise AssertionError("rollback failure hid the original completion error")
    assert owner._run_verify_active_step == "complete"
    assert "complete" not in owner._run_verify_results

    prompt_restores.clear()

    async def flush_ok():
        pass

    owner.flush_pipeline = flush_ok
    completed, next_node = await owner._run_verify_finish_complete({"complete": True}, flow)
    assert completed == {"status": "ok"}
    assert next_node is None
    assert owner._run_verify_active_step is None
    assert owner._run_verify_results["complete"] == {"complete": True}
    assert prompt_restores[0].endswith("Current customer: cus-smoke.")
    owner.queue_frame = original_queue_frame
    owner.flush_pipeline = original_flush_pipeline
    subprocess.run(["ruff", "check", "bot.py"], check=True)
    print("task transfer smoke ok")


asyncio.run(main())
`

const pipecatSessionIsolationSmokeScript = `"""Smoke check: two workers cannot share call-local transfer state."""
import asyncio
import json
import os
import subprocess

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import bot  # noqa: E402
from pipecat.processors.aggregators.llm_context import LLMContext  # noqa: E402


class Params:
    def __init__(self):
        self.results = []

    async def result_callback(self, result, **kwargs):
        self.results.append(result)


async def main() -> None:
    updates = []
    first_started = asyncio.Event()
    second_updated = asyncio.Event()
    release_first = asyncio.Event()

    async def update_call(call_id, twiml):
        updates.append((call_id, twiml))
        if call_id == "call-a":
            first_started.set()
            await release_first.wait()
        else:
            second_updated.set()

    bot._update_carrier_call = update_call
    first_context = {"_phone_call": {"call_id": "call-a"}}
    second_context = {"_phone_call": {"call_id": "call-b"}}
    first = bot.BillingAgent(
        state=bot.State(), context=LLMContext(), call_context=first_context,
    )
    second = bot.BillingAgent(
        state=bot.State(), context=LLMContext(), call_context=second_context,
    )

    first_attempt = Params()
    first_task = asyncio.create_task(first.to_human(first_attempt))
    await first_started.wait()

    # The claim is synchronous: a duplicate from the same session cannot start
    # a second primitive while the first one is suspended.
    in_progress = Params()
    await first.to_human(in_progress)
    assert len(updates) == 1
    assert in_progress.results == [{
        "in_progress": "A transfer is already under way; do not start another."
    }]
    assert "_transfer_result" not in second_context

    second_attempt = Params()
    second_task = asyncio.create_task(second.to_human(second_attempt))
    await second_updated.wait()
    release_first.set()
    await asyncio.gather(first_task, second_task)
    assert [call_id for call_id, _ in updates] == ["call-a", "call-b"]
    assert first_attempt.results == [{"transfer_started": True}]
    assert second_attempt.results == [{"transfer_started": True}]

    first_replay = Params()
    await first.to_human(first_replay)
    assert first_replay.results == first_attempt.results
    assert len(updates) == 2
    assert first_context["_phone_call"]["call_id"] == "call-a"
    assert second_context["_phone_call"]["call_id"] == "call-b"
    assert first.call_context is first_context
    assert second.call_context is second_context
    subprocess.run(["ruff", "check", "bot.py"], check=True)
    print("pipecat session isolation smoke ok")


asyncio.run(main())
`

const pipecatDailyTransferFailureSmokeScript = `"""Smoke check: Daily transfer failures become terminal per-call results."""
import asyncio
import json
import os
from urllib.parse import urlencode

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")
os.environ["UNMUTE_PUBLIC_URL"] = "https://voice.example"
os.environ["TWILIO_AUTH_TOKEN"] = "smoke-auth-token"

import bot  # noqa: E402
import telephony_helper as helper  # noqa: E402
from pipecat.processors.aggregators.llm_context import LLMContext  # noqa: E402
from pipecat.transports.daily.transport import DailyTransportClient  # noqa: E402
from twilio.request_validator import RequestValidator  # noqa: E402


class ProbeDailyTransport(bot.DailyTransport):
    def __init__(self, failure):
        self.failure = failure
        self.attempts = 0

    async def sip_call_transfer(self, _settings):
        self.attempts += 1
        if self.failure is not None:
            raise self.failure
        return None


class FakeLLM:
    def __init__(self, fail_on_push=None):
        self.fail_on_push = fail_on_push
        self.pushes = 0
        self.frames = []

    async def push_frame(self, frame):
        self.pushes += 1
        self.frames.append(frame)
        if self.pushes == self.fail_on_push:
            raise RuntimeError("announcement failed")


class Params:
    function_name = "send_to_billing"

    def __init__(self, llm):
        self.llm = llm
        self.results = []

    async def result_callback(self, result, **_kwargs):
        self.results.append(result)


class HelperRequest:
    def __init__(self, form, signature):
        self._body = urlencode(form).encode()
        self.headers = {"X-Twilio-Signature": signature}

    async def body(self):
        return self._body


async def main() -> None:
    assert helper._call_url() == "https://voice.example/call"
    os.environ["UNMUTE_PUBLIC_URL"] = "https://voice.example/prefix/"
    assert helper._call_url() == "https://voice.example/prefix/call"
    for invalid_url in (
        "http://voice.example",
        "https://voice.example?query=yes",
        "https://voice.example#fragment",
        "https://user:password@voice.example",
    ):
        os.environ["UNMUTE_PUBLIC_URL"] = invalid_url
        try:
            helper._call_url()
        except RuntimeError:
            pass
        else:
            raise AssertionError(f"accepted invalid public URL: {invalid_url}")
    os.environ["UNMUTE_PUBLIC_URL"] = "https://voice.example"

    # The helper authenticates every field before the platform-keyed start. An
    # invalid signature receives 403 and cannot start an agent; a valid signature
    # over the same form reaches the start exactly once.
    starts = []

    async def fake_start_agent(_request, *, caller, call_sid):
        starts.append((caller, call_sid))
        return {"sessionId": "session-smoke"}

    helper._start_agent = fake_start_agent
    form = {
        "CallSid": "CA-smoke",
        "From": "+14155550100",
        "ExtraSignedField": "must-be-validated",
    }
    rejected = await helper.inbound_call(HelperRequest(form, "invalid"))
    assert rejected.status_code == 403
    assert starts == []
    signature = RequestValidator(os.environ["TWILIO_AUTH_TOKEN"]).compute_signature(
        "https://voice.example/call", form
    )
    accepted = await helper.inbound_call(HelperRequest(form, signature))
    assert accepted.status_code == 200
    assert starts == [("+14155550100", "CA-smoke")]

    # The supported SDK itself requires an existing phone session. This exact
    # return is the premise behind rejecting browser and removed carrierless
    # transfers before the generated tool announces anything.
    client = DailyTransportClient.__new__(DailyTransportClient)
    client._dial_out_session_id = ""
    client._dial_in_session_id = ""
    settings = {"toEndPoint": "sip:+15551234567@example.com"}
    error = await DailyTransportClient.sip_call_transfer(client, settings)
    assert error == "Can't transfer SIP call if 'sessionId' is not set"
    assert "sessionId" not in settings

    # A browser session on a carrier-backed package still has a DailyTransport,
    # but no existing SIP leg. Refuse without an announcement or primitive.
    browser_transport = ProbeDailyTransport(None)
    browser_context = {"_transport": browser_transport, "_daily_sip_session": False}
    browser_agent = bot.FrontDeskAgent(
        state=None, context=LLMContext(), call_context=browser_context,
    )
    browser_params = Params(FakeLLM())
    await browser_agent.send_to_billing(browser_params)
    assert browser_params.results == [
        {"failed": "this session is not a phone call, so it cannot be transferred"}
    ]
    assert browser_params.llm.pushes == 0
    assert browser_transport.attempts == 0

    # An announcement failure happens before the carrier primitive. Release the
    # claim so a later model turn can retry the one operation that never began.
    retry_transport = ProbeDailyTransport(None)
    retry_context = {"_transport": retry_transport, "_daily_sip_session": True}
    retry_agent = bot.FrontDeskAgent(
        state=None, context=LLMContext(), call_context=retry_context,
    )
    await retry_agent.send_to_billing(Params(FakeLLM(fail_on_push=1)))
    assert "_transfer_result" not in retry_context
    assert retry_transport.attempts == 0

    retried = Params(FakeLLM())
    await retry_agent.send_to_billing(retried)
    assert retried.results == [{"transferred": True}]
    assert retry_transport.attempts == 1

    # The primitive fails, then even the failure announcement fails. The
    # claimed result must already be terminal, so replay cannot dial again.
    transport = ProbeDailyTransport(RuntimeError("daily transfer failed"))
    call_context = {"_transport": transport, "_daily_sip_session": True}
    agent = bot.FrontDeskAgent(
        state=None, context=LLMContext(), call_context=call_context,
    )
    first = Params(FakeLLM(fail_on_push=2))
    await agent.send_to_billing(first)
    terminal = {
        "failed": "The transfer could not be completed; the call is ending."
    }
    assert call_context["_transfer_result"] == terminal
    assert transport.attempts == 1
    assert type(first.llm.frames[-1]).__name__ == "EndFrame"

    replay = Params(FakeLLM())
    await agent.send_to_billing(replay)
    assert replay.results == [terminal]
    assert transport.attempts == 1, "terminal failure was retried"

    # Cancellation is an ambiguous carrier outcome. Keep the synchronous claim
    # and replay it; never start a second primitive.
    cancelled_transport = ProbeDailyTransport(asyncio.CancelledError())
    cancelled_context = {
        "_transport": cancelled_transport,
        "_daily_sip_session": True,
    }
    cancelled_agent = bot.FrontDeskAgent(
        state=None, context=LLMContext(), call_context=cancelled_context,
    )
    try:
        await cancelled_agent.send_to_billing(Params(FakeLLM()))
    except asyncio.CancelledError:
        pass
    else:
        raise AssertionError("transfer cancellation was swallowed")
    in_progress = {
        "in_progress": "A transfer is already under way; do not start another."
    }
    assert cancelled_context["_transfer_result"] == in_progress

    cancelled_replay = Params(FakeLLM())
    await cancelled_agent.send_to_billing(cancelled_replay)
    assert cancelled_replay.results == [in_progress]
    assert cancelled_transport.attempts == 1, "cancelled transfer was retried"
    print("pipecat Daily transfer failure smoke ok")


asyncio.run(main())
`

const pipecatStaticCheckScript = `"""Smoke check: the generated project passes Ruff and ty."""
import subprocess

subprocess.run(["ruff", "check", "."], check=True)
subprocess.run(["ty", "check", "."], check=True)
`

// pipecatRequestTracingSmokeScript drives the generated worker/bus topology
// through deterministic STT, LLM, and TTS services. V17 requires all three
// request spans to share the conversation trace.
const pipecatRequestTracingSmokeScript = `"""Smoke check: a real worker turn emits nested speech and LLM spans."""
import asyncio
import base64
import json
import os
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer


class OTLPReceiver(BaseHTTPRequestHandler):
    def do_POST(self) -> None:
        self.rfile.read(int(self.headers["Content-Length"]))
        self.server.requests.append((self.path, dict(self.headers)))
        self.send_response(200)
        self.end_headers()

    def log_message(self, format, *args) -> None:
        pass


receiver = HTTPServer(("127.0.0.1", 0), OTLPReceiver)
receiver.requests = []
threading.Thread(target=receiver.serve_forever, daemon=True).start()

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")
os.environ["LANGFUSE_PUBLIC_KEY"] = "pk-smoke"
os.environ["LANGFUSE_SECRET_KEY"] = "sk-smoke"
os.environ["LANGFUSE_BASE_URL"] = f"http://127.0.0.1:{receiver.server_port}"

import bot  # noqa: E402
import tracing as tracing_config  # noqa: E402
from loguru import logger  # noqa: E402
from opentelemetry import trace  # noqa: E402
from opentelemetry.sdk.trace import TracerProvider  # noqa: E402
from opentelemetry.sdk.trace.export import SimpleSpanProcessor  # noqa: E402
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter  # noqa: E402
from pipecat.bus import BusBridgeProcessor  # noqa: E402
from pipecat.frames.frames import (  # noqa: E402
    Frame,
    FunctionCallFromLLM,
    InputAudioRawFrame,
    LLMContextFrame,
    LLMFullResponseEndFrame,
    LLMFullResponseStartFrame,
    LLMTextFrame,
    TTSAudioRawFrame,
    TTSStoppedFrame,
    TranscriptionFrame,
)
from pipecat.pipeline.pipeline import Pipeline  # noqa: E402
from pipecat.pipeline.worker import PipelineParams, PipelineWorker  # noqa: E402
from pipecat.processors.aggregators.llm_context import LLMContext  # noqa: E402
from pipecat.processors.aggregators.llm_response_universal import LLMContextAggregatorPair  # noqa: E402
from pipecat.processors.frame_processor import FrameDirection, FrameProcessor  # noqa: E402
from pipecat.services.llm_service import LLMService  # noqa: E402
from pipecat.services.settings import LLMSettings, STTSettings, TTSSettings  # noqa: E402
from pipecat.services.stt_service import STTService  # noqa: E402
from pipecat.services.tts_service import TTSService  # noqa: E402
from pipecat.utils.tracing.service_decorators import traced_llm, traced_stt, traced_tts  # noqa: E402
from pipecat.workers.llm import LLMWorker, LLMWorkerActivationArgs  # noqa: E402
from pipecat.workers.runner import WorkerRunner  # noqa: E402

tool_probe = {
    "name": "lookup_customer",
    "call_id": "call-smoke",
    "arguments": {"phone": "+1555010101"},
    "result": {"customer_id": "cus_1001"},
}


class FakeLLM(LLMService):
    def __init__(self) -> None:
        super().__init__(
            settings=LLMSettings(
                model="probe-model",
                system_instruction="You are the tracing probe.",
                temperature=None,
                max_tokens=None,
                top_p=None,
                top_k=None,
                frequency_penalty=None,
                presence_penalty=None,
                seed=None,
                filter_incomplete_user_turns=False,
                user_turn_completion_config=None,
            )
        )
        self.tool_called = False

    @traced_llm
    async def _process_context(self, context: LLMContext) -> None:
        if not self.tool_called:
            self.tool_called = True
            await self.run_function_calls(
                [
                    FunctionCallFromLLM(
                        function_name=tool_probe["name"],
                        tool_call_id=tool_probe["call_id"],
                        arguments=tool_probe["arguments"],
                        context=context,
                    )
                ]
            )
        else:
            await self.push_frame(LLMTextFrame("traced."))

    async def process_frame(self, frame: Frame, direction: FrameDirection) -> None:
        await super().process_frame(frame, direction)
        if isinstance(frame, LLMContextFrame):
            await self.push_frame(LLMFullResponseStartFrame())
            await self._process_context(frame.context)
            await self.push_frame(LLMFullResponseEndFrame())
        else:
            await self.push_frame(frame, direction)


class FakeSTT(STTService):
    def __init__(self) -> None:
        super().__init__(
            audio_passthrough=False,
            sample_rate=16000,
            settings=STTSettings(model="probe-stt", language="en"),
        )

    def can_generate_metrics(self) -> bool:
        return True

    @traced_stt
    async def run_stt(self, audio: bytes):
        await self.start_ttfb_metrics()
        yield TranscriptionFrame(
            "trace this request",
            user_id="probe-user",
            timestamp="2026-07-20T00:00:00Z",
            language="en",
            finalized=True,
        )


class FakeTTS(TTSService):
    def __init__(self) -> None:
        super().__init__(
            push_start_frame=True,
            push_stop_frames=True,
            push_text_frames=False,
            sample_rate=16000,
            settings=TTSSettings(model="probe-tts", voice="probe-voice", language="en"),
        )

    def can_generate_metrics(self) -> bool:
        return True

    @traced_tts
    async def run_tts(self, text: str, context_id: str):
        yield TTSAudioRawFrame(
            audio=b"\x00\x00" * 160,
            sample_rate=16000,
            num_channels=1,
            context_id=context_id,
        )


class Passthrough(FrameProcessor):
    async def process_frame(self, frame: Frame, direction: FrameDirection) -> None:
        await super().process_frame(frame, direction)
        await self.push_frame(frame, direction)


class StopAfterSpeech(Passthrough):
    def __init__(self, runner: WorkerRunner) -> None:
        super().__init__()
        self.runner = runner

    async def process_frame(self, frame: Frame, direction: FrameDirection) -> None:
        await super().process_frame(frame, direction)
        if isinstance(frame, TTSStoppedFrame):
            asyncio.create_task(self.runner.cancel(reason="speech traced"))


class SlowProvider:
    def __init__(self, started: threading.Event, release: threading.Event) -> None:
        self.started = started
        self.release = release
        self.released_while_flushing = False

    def force_flush(self) -> None:
        self.started.set()
        self.released_while_flushing = self.release.wait(timeout=1)


async def assert_worker_start_failure_stops_runner() -> None:
    runner = WorkerRunner()
    runner_ready = asyncio.Event()
    pipeline_started = asyncio.Event()
    worker_start_error = None
    main_worker = PipelineWorker(
        Pipeline([Passthrough()]),
        name="startup-failure-main",
    )

    class FailingWorker:
        name = "startup-failure-agent"

        async def attach(self, *, registry, bus) -> None:
            raise RuntimeError("specialist startup probe")

    @runner.event_handler("on_ready")
    async def on_runner_ready(runner):
        runner_ready.set()

    @main_worker.event_handler("on_pipeline_started")
    async def on_pipeline_started(worker, frame):
        nonlocal worker_start_error
        await runner_ready.wait()
        try:
            await runner.add_workers(FailingWorker())
        except Exception as error:
            worker_start_error = error
            pipeline_started.set()
            await runner.cancel(reason="agent worker startup failed")
            return
        pipeline_started.set()

    await runner.add_workers(main_worker)
    try:
        await asyncio.wait_for(runner.run(), timeout=2)
        if worker_start_error is not None:
            raise worker_start_error
    except RuntimeError as error:
        assert str(error) == "specialist startup probe"
    else:
        raise AssertionError("specialist startup failure was swallowed")
    assert pipeline_started.is_set()


async def main() -> None:
    await assert_worker_start_failure_stops_runner()

    original_get_provider = tracing_config.trace.get_tracer_provider
    tracing_config.trace.get_tracer_provider = lambda: TracerProvider()
    try:
        tracing_config.setup_langfuse_tracing()
    except RuntimeError as exc:
        assert "OpenTelemetry already has a TracerProvider" in str(exc)
    else:
        raise AssertionError("preinstalled provider was silently replaced")
    finally:
        tracing_config.trace.get_tracer_provider = original_get_provider

    flush_started = threading.Event()
    release_flush = threading.Event()
    slow_provider = SlowProvider(flush_started, release_flush)

    async def run_alongside_flush() -> None:
        while not flush_started.is_set():
            await asyncio.sleep(0)
        release_flush.set()

    concurrent_task = asyncio.create_task(run_alongside_flush())
    await asyncio.to_thread(tracing_config.flush_tracing, slow_provider)
    await concurrent_task
    assert slow_provider.released_while_flushing

    memory = InMemorySpanExporter()
    provider = tracing_config.setup_langfuse_tracing()
    assert provider is tracing_config.setup_langfuse_tracing()
    assert provider is trace.get_tracer_provider()
    provider.add_span_processor(SimpleSpanProcessor(memory))

    agent_names = sorted(
        name.removeprefix("build_").removesuffix("_llm")
        for name in vars(bot)
        if name.startswith("build_") and name.endswith("_llm")
    )
    assert len(agent_names) == 1, agent_names
    agent_name = agent_names[0]
    setattr(bot, f"build_{agent_name}_llm", FakeLLM)
    setattr(bot, f"build_{agent_name}_tts", FakeTTS)

    runner = WorkerRunner()
    session_id = "session-smoke"
    context = LLMContext()
    user_aggregator, assistant_aggregator = LLMContextAggregatorPair(context)
    main_worker = PipelineWorker(
        Pipeline(
            [
                FakeSTT(),
                user_aggregator,
                BusBridgeProcessor(bus=runner.bus, worker_name="trace-main"),
                StopAfterSpeech(runner),
                assistant_aggregator,
            ]
        ),
        name="trace-main",
        conversation_id=session_id,
        enable_tracing=True,
        additional_span_attributes={
            "langfuse.trace.name": tracing_config.TRACE_NAME,
            "langfuse.session.id": session_id,
        },
        params=PipelineParams(enable_metrics=True, enable_usage_metrics=True),
    )
    # LLMWorker comes from pipecat, not from bot: the template drops that import
    # when a package has both tracing and function calls, because the agent class
    # extends TracedLLMWorker (itself an LLMWorker) and an unused import would
    # fail the emitted project's own ruff gate.
    agent_types = [
        value
        for value in vars(bot).values()
        if isinstance(value, type)
        and issubclass(value, LLMWorker)
        and value.__module__ == "bot"
    ]
    assert len(agent_types) == 1, agent_types
    request_agent = agent_types[0]()
    tracing_config.enable_agent_tracing(main_worker, [request_agent])
    mcp_clients = getattr(request_agent, "_mcp_clients", [])
    mcp_session = None
    mcp_exit_stack = None
    if mcp_clients:
        from mcp.types import CallToolResult, ListToolsResult, TextContent, Tool

        class FakeMCPSession:
            def __init__(self) -> None:
                self.calls = []

            async def list_tools(self):
                return ListToolsResult(
                    tools=[
                        Tool(
                            name="firecrawl_search",
                            description="Search the web.",
                            inputSchema={
                                "type": "object",
                                "properties": {"query": {"type": "string"}},
                                "required": ["query"],
                            },
                        )
                    ]
                )

            async def call_tool(self, name, arguments=None):
                self.calls.append((name, arguments))
                return CallToolResult(
                    content=[TextContent(type="text", text="fresh MCP result")]
                )

        class FakeExitStack:
            def __init__(self) -> None:
                self.closed = False

            async def aclose(self) -> None:
                self.closed = True

        assert len(mcp_clients) == 1
        mcp_session = FakeMCPSession()
        mcp_exit_stack = FakeExitStack()
        mcp_clients[0]._active_session = mcp_session
        mcp_clients[0]._exit_stack = mcp_exit_stack
        tool_probe.update(
            name="firecrawl_search",
            call_id="mcp-call-smoke",
            arguments={"query": "latest salon trends"},
            result="fresh MCP result",
        )
        await request_agent.start_mcp()

    runner_ready = asyncio.Event()
    worker_start_error = None

    @runner.event_handler("on_ready")
    async def on_runner_ready(runner):
        runner_ready.set()

    @main_worker.event_handler("on_pipeline_started")
    async def on_pipeline_started(worker, frame):
        nonlocal worker_start_error
        await runner_ready.wait()
        try:
            await runner.add_workers(request_agent)
        except Exception as error:
            worker_start_error = error
            await runner.cancel(reason="agent worker startup failed")
            return
        await main_worker.queue_frame(
            InputAudioRawFrame(
                audio=b"\x00\x00" * 160,
                sample_rate=16000,
                num_channels=1,
            )
        )
        await main_worker.activate_worker(
            request_agent.name,
            args=LLMWorkerActivationArgs(
                messages=[{"role": "user", "content": "trace this request"}],
                run_llm=True,
            ),
        )

    error_lines = []
    error_sink = logger.add(
        lambda message: error_lines.append(str(message)),
        level="ERROR",
        format="{message}",
    )
    await runner.add_workers(main_worker)
    try:
        await asyncio.wait_for(runner.run(), timeout=10)
        if worker_start_error is not None:
            raise worker_start_error
    finally:
        logger.remove(error_sink)
        if mcp_clients:
            await request_agent.close_mcp()
    assert not any("StartFrame not received yet" in line for line in error_lines), error_lines

    cancel_parent = trace.get_tracer("unmute.smoke").start_span("cancel-parent")
    cancel_parent_context = trace.set_span_in_context(cancel_parent)
    cancel_trace_id = cancel_parent.context.trace_id
    cancel_parent_id = cancel_parent.context.span_id

    class FixedTraceContext:
        def get_turn_context(self):
            return cancel_parent_context

        def get_conversation_context(self):
            return None

    async def cancelled_call(_span):
        raise asyncio.CancelledError

    original_context = request_agent._tracing_context
    request_agent._tracing_context = FixedTraceContext()
    try:
        await request_agent._trace_tool(
            "cancel_probe",
            {"reason": "caller interruption"},
            cancelled_call,
            tool_call_id="cancel-call-smoke",
        )
    except asyncio.CancelledError:
        pass
    else:
        raise AssertionError("tool cancellation was swallowed")
    finally:
        request_agent._tracing_context = original_context
        cancel_parent.end()
    tracing_config.flush_tracing(provider)

    spans = memory.get_finished_spans()
    conversation = next(span for span in spans if span.name == "conversation")
    turn = next(span for span in spans if span.name == "turn")
    tool_call = next(span for span in spans if span.name == f"tool:{tool_probe['name']}")
    cancelled_tool = next(span for span in spans if span.name == "tool:cancel_probe")
    requests = {span.name: span for span in spans if span.name in {"stt", "llm", "tts"}}
    assert requests.keys() == {"stt", "llm", "tts"}
    assert requests["stt"].attributes["gen_ai.request.model"] == "probe-stt"
    assert requests["llm"].attributes["gen_ai.request.model"] == "probe-model"
    assert requests["tts"].attributes["gen_ai.request.model"] == "probe-tts"
    assert requests["stt"].attributes["gen_ai.provider.name"] == "fakestt"
    assert requests["tts"].attributes["gen_ai.provider.name"] == "faketts"
    assert requests["stt"].attributes["transcript"] == "trace this request"
    assert requests["stt"].attributes["language"] == "en"
    assert requests["stt"].attributes["is_final"] is True
    assert requests["llm"].attributes["output"] == "traced."
    assert requests["llm"].attributes["gen_ai.system_instructions"] == "You are the tracing probe."
    llm_input = json.loads(requests["llm"].attributes["langfuse.observation.input"])
    assert llm_input[0] == {"role": "system", "content": "You are the tracing probe."}
    assert {"role": "user", "content": "trace this request"} in llm_input
    assert json.loads(requests["llm"].attributes["input"]) == llm_input
    assert requests["tts"].attributes["text"] == "traced."
    assert requests["tts"].attributes["voice_id"] == "probe-voice"
    assert requests["tts"].attributes["metrics.character_count"] == len("traced.")
    assert requests["stt"].attributes["metrics.ttfb"] >= 0
    # Pipecat 1.7.0 emits TTS TTFB as a framework metric after its native TTS
    # span has closed. Keep the native lifecycle instead of patching its queue.
    assert json.loads(requests["stt"].attributes["langfuse.observation.input"]) == "audio"
    assert json.loads(requests["stt"].attributes["langfuse.observation.output"]) == "trace this request"
    assert json.loads(requests["tts"].attributes["langfuse.observation.input"]) == "traced."
    assert json.loads(requests["tts"].attributes["langfuse.observation.output"]) == "audio"
    assert json.loads(requests["stt"].attributes["langfuse.trace.input"]) == "trace this request"
    assert json.loads(requests["tts"].attributes["langfuse.trace.output"]) == "traced."
    assert requests["stt"].attributes["langfuse.observation.metadata.ttfb_seconds"] >= 0
    assert requests["stt"].attributes["langfuse.observation.completion_start_time"]
    assert requests["tts"].attributes["langfuse.observation.metadata.character_count"] == len("traced.")
    assert json.loads(requests["tts"].attributes["langfuse.observation.usage_details"]) == {
        "characters": len("traced.")
    }
    assert conversation.attributes["langfuse.trace.name"] == tracing_config.TRACE_NAME
    assert conversation.attributes["conversation.id"] == session_id
    assert conversation.attributes["langfuse.session.id"] == session_id
    assert conversation.resource.attributes["service.name"] == tracing_config.TRACE_NAME
    assert all(span.context.trace_id == conversation.context.trace_id for span in requests.values())
    assert tool_call.context.trace_id == conversation.context.trace_id
    assert tool_call.parent.span_id == turn.context.span_id
    assert json.loads(tool_call.attributes["langfuse.observation.input"]) == tool_probe["arguments"]
    traced_result = json.loads(tool_call.attributes["langfuse.observation.output"])
    if mcp_session:
        assert traced_result == tool_probe["result"]
        assert mcp_session.calls == [(tool_probe["name"], tool_probe["arguments"])]
        assert mcp_exit_stack.closed
        assert mcp_clients[0]._active_session is None
        assert mcp_clients[0]._exit_stack is None
    else:
        assert traced_result["customer_id"] == tool_probe["result"]["customer_id"]
    assert tool_call.attributes["tool.function_name"] == tool_probe["name"]
    assert tool_call.attributes["tool.call_id"] == tool_probe["call_id"]
    assert tool_call.end_time > tool_call.start_time
    assert cancelled_tool.context.trace_id == cancel_trace_id
    assert cancelled_tool.parent.span_id == cancel_parent_id
    assert cancelled_tool.attributes["tool.function_name"] == "cancel_probe"
    assert cancelled_tool.attributes["tool.call_id"] == "cancel-call-smoke"
    assert cancelled_tool.attributes["tool.result_status"] == "cancelled"
    assert json.loads(cancelled_tool.attributes["langfuse.observation.input"]) == {
        "reason": "caller interruption"
    }
    assert "langfuse.observation.output" not in cancelled_tool.attributes
    assert cancelled_tool.end_time > cancelled_tool.start_time
    assert all(span.end_time > span.start_time for span in requests.values())
    assert receiver.requests
    path, headers = receiver.requests[0]
    headers = {name.lower(): value for name, value in headers.items()}
    auth = base64.b64encode(b"pk-smoke:sk-smoke").decode()
    assert path == "/api/public/otel/v1/traces"
    assert headers["authorization"] == f"Basic {auth}"
    assert headers["x-langfuse-ingestion-version"] == "4"
    receiver.shutdown()
    print("pipecat speech tracing smoke ok")


asyncio.run(main())
`

const pipecatDailyInboundSmokeScript = `"""Smoke check: the Daily transport params accept a real inbound call.

This is the only layer that can prove the fix. The offline tests can see that
the emitted class differs from the generic one and that its import is present;
they cannot see whether the framework can actually assign an inbound call's
details onto it. So this runs the framework's own wiring against the emitted
factory, with a real dial-in payload and with an empty one.
"""
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import bot  # noqa: E402
from pipecat.runner.types import DailyDialinRequest  # noqa: E402
from pipecat.runner.utils import _maybe_apply_daily_dialin  # noqa: E402
from pipecat.transports.base_transport import TransportParams  # noqa: E402

factory = bot.transport_params["daily"]

# 1. A real inbound call. Pipecat Cloud's managed dial-in webhook hands the
#    runner this payload; create_transport merges it onto the factory's result.
body = {
    "dialin_settings": {"call_id": "call-smoke", "call_domain": "domain-smoke"},
    "daily_api_key": "smoke-daily-key",
    "daily_api_url": "https://api.daily.co/v1",
}
params = factory()
_maybe_apply_daily_dialin(params, DailyDialinRequest.model_validate(body))
assert params.dialin_settings is not None, "inbound dial-in settings did not land"
assert params.dialin_settings.call_id == "call-smoke"
assert params.dialin_settings.call_domain == "domain-smoke"
assert params.api_key == "smoke-daily-key"
assert params.api_url == "https://api.daily.co/v1"

# The same payload against the generic class is the defect this feature fixed.
# If this ever stops raising, the offline scoping test is measuring nothing.
generic = TransportParams(audio_in_enabled=True, audio_out_enabled=True)
try:
    _maybe_apply_daily_dialin(generic, DailyDialinRequest.model_validate(body))
except Exception:
    pass
else:
    raise AssertionError(
        "the generic params class accepted inbound call fields; "
        "the Daily-specific class may no longer be needed"
    )

# 2. No call details at all: the documented no-op path, and the one every
#    browser and console session takes. It must behave exactly as before.
for empty in (None, {}, {"unrelated": "content"}):
    plain = factory()
    _maybe_apply_daily_dialin(plain, empty)
    assert plain.dialin_settings is None, f"{empty!r} invented dial-in settings"
    assert not plain.api_key, f"{empty!r} invented an api_key"
    assert plain.audio_in_enabled and plain.audio_out_enabled, "audio kwargs lost"

print("daily inbound params ok")
`

// TestSmokePipecatV1DailyInboundParamsAcceptCall is US1's proof. The emitted
// Daily factory is handed a real dial-in payload through Pipecat's own wiring
// and the fields have to land; the same payload against the generic class must
// still raise, which is what keeps the fix necessary rather than decorative.
//
// It also covers FR-006, the empty-body path: a session carrying no call details
// behaves exactly as it did before, which is every browser and console run.
func TestSmokePipecatV1DailyInboundParamsAcceptCall(t *testing.T) {
	runPipecatSmokeScript(t, "daily_carrier", nil, nil, pipecatDailyInboundSmokeScript)
}

// TestSmokePipecatV1ServicesInstantiate proves the safe_core emission end to
// end (V9, L4): uv resolves the emitted pyproject (network), bot.py imports,
// and every emitted service constructor accepts its emitted kwargs
// (deepgram Settings-style STT, slng flat-kwargs TTS, openai Settings LLM).
// Opt-in (`make smoke` / -tags smoke), never in the default suite.
func TestSmokePipecatV1ServicesInstantiate(t *testing.T) {
	runPipecatSmoke(t, "safe_core", nil, nil)
}

func TestSmokePipecatRegionalInfrastructureInstantiates(t *testing.T) {
	runPipecatSmoke(t, "salon-concierge", nil, nil)
}

// TestSmokePipecatV1TaskGroupsInstantiate runs the generated FlowManager on
// pinned Pipecat 1.7.0 and observes task-role replacement, owner-role restoration,
// and transfer activation (V28).
func TestSmokePipecatV1TaskGroupsInstantiate(t *testing.T) {
	runPipecatSmokeScript(t, "task-groups", nil, func(agent *ir.Agent) {
		aftercare := agent.Agents["appointment_desk"]
		aftercare.Instructions = "You are the aftercare agent."
		aftercare.Tools = nil
		agent.Agents["aftercare"] = aftercare
		group := agent.TaskGroups["appointment_flow"]
		group.Then = ir.GroupTransfer
		group.ThenTarget = "aftercare"
		agent.TaskGroups["appointment_flow"] = group
	}, pipecatTaskRoleSmokeScript)
}

// TestSmokePipecatV1TaskTransferStopsFlow checks the generated handler against
// the supported SDK's real NO_RESPONSE transition semantics.
func TestSmokePipecatV1TaskTransferStopsFlow(t *testing.T) {
	runPipecatSmokeScript(t, "safe_core", func(target *ir.Target) {
		target.Version = "1.7.0"
	}, func(agent *ir.Agent) {
		addPipecatTaskTransferFixture(agent)
	}, pipecatTaskTransferSmokeScript)
}

// TestSmokePipecatV1SessionStateIsIsolated runs the emitted cloud-transfer
// worker against Pipecat 1.7.0. One session replays its own result, while a
// second session still transfers its own phone call.
func TestSmokePipecatV1SessionStateIsIsolated(t *testing.T) {
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not available")
	}
	artifact := cloudWebsocketArtifact(t, cloudWebsocketOptions{
		inbound: true, transfer: true, connection: true,
	})
	runGeneratedPipecatSmokeScript(t, artifact, pipecatSessionIsolationSmokeScript)
}

// TestSmokePipecatV1DailyTransferFailureIsTerminal runs the claimed-result
// contract through the supported Pipecat SDK's real direct-function wrapper.
func TestSmokePipecatV1DailyTransferFailureIsTerminal(t *testing.T) {
	runPipecatSmokeScript(t, "daily_carrier", nil, func(agent *ir.Agent) {
		human := agent.Controls["send_to_billing"].(*ir.HumanTransfer)
		human.OnUnavailable = ir.OnUnavailableHangup
	}, pipecatDailyTransferFailureSmokeScript)
}

// TestSmokePipecatV1MultiVendorInstantiates covers the remaining official
// entries in one venv: assemblyai listen, elevenlabs + cartesia speak.
func TestSmokePipecatV1MultiVendorInstantiates(t *testing.T) {
	runPipecatSmoke(t, "safe_core", func(tgt *ir.Target) {
		tgt.Models.Listen = &ir.Binding{Provider: "assemblyai", Model: "universal-3-5-pro"}
		tgt.Models.Speak["front_desk"] = ir.Binding{
			Provider: "elevenlabs", Model: "eleven_multilingual_v2", Voice: "21m00Tcm4TlvDq8ikWAM",
		}
		tgt.Models.Speak["specialist"] = ir.Binding{
			Provider: "cartesia", Model: "sonic-3", Voice: "f786b574-daa5-4673-aa0c-cbe3e8534c02",
		}
	}, nil)
}

// TestSmokePipecatV1RestoredVendorsInstantiates covers the riskiest of the
// T13-restored entries in one venv: soniox listen, inworld + rime speak, and
// anthropic reason (the workers driver injects system_instruction into its
// Settings). Constructor kwargs are checked against the real packages.
// (speechmatics speak was smoke-rejected here 2026-07-17: its service demands
// a caller-supplied aiohttp_session, impossible at module import — T13.)
func TestSmokePipecatV1RestoredVendorsInstantiates(t *testing.T) {
	runPipecatSmoke(t, "safe_core", func(tgt *ir.Target) {
		tgt.Models.Listen = &ir.Binding{Provider: "soniox", Model: "stt-rt-v5"}
		tgt.Models.Speak["front_desk"] = ir.Binding{
			Provider: "inworld", Model: "inworld-tts-2", Voice: "Ashley",
		}
		tgt.Models.Speak["specialist"] = ir.Binding{
			Provider: "rime", Model: "mistv2", Voice: "cove",
		}
		tgt.Models.Reason["fast_reasoning"] = ir.Binding{
			Provider: "anthropic", Model: "claude-sonnet-4-6",
		}
	}, nil)
}

// TestSmokePipecatV1LocalToolInstantiates proves the local-tool lowering (T14,
// V13) against real pipecat-ai: importing bot defines the call-local worker
// classes, so the @tool wrapper class-collects and `import tools.fetch_notes`
// resolves the copied handler file inside the venv.
func TestSmokePipecatV1LocalToolInstantiates(t *testing.T) {
	runPipecatSmoke(t, "safe_core", nil, func(agent *ir.Agent) {
		agent.Tools["fetch_notes"] = ir.Tool{
			Description: "Fetch the caller's saved notes.",
			Input:       map[string]any{"type": "object", "properties": map[string]any{"topic": map[string]any{"type": "string"}}, "required": []any{"topic"}},
			Execution:   ir.ToolLocal, Handler: "tools/fetch_notes.py",
			HandlerSource: "def fetch_notes(topic):\n    return {\"notes\": []}\n",
			Interruption:  ir.ToolProviderDefault, Effect: ir.ToolReturnsData,
		}
		intake := agent.Agents["intake"]
		intake.Tools = append(intake.Tools, "fetch_notes")
		agent.Agents["intake"] = intake
	})
}

// mcpSmokeScript instantiates the emitted agent workers and inspects the MCP
// clients they built. Constructing them is the point: it runs the emitted
// MCPClient/params kwargs against the installed SDK, which is the drift
// py_compile cannot see. Nothing connects, so no server is needed.
const mcpSmokeScript = `"""Smoke check: the emitted MCP clients construct against the installed SDK."""
import inspect
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "https://mcp.example/mcp")

import bot  # noqa: E402

from pipecat.processors.aggregators.llm_context import LLMContext  # noqa: E402
from pipecat.services.mcp_service import MCPClient  # noqa: E402
from pipecat.workers.llm import LLMWorker  # noqa: E402

workers = sorted(
    name for name, obj in vars(bot).items()
    if inspect.isclass(obj) and issubclass(obj, LLMWorker) and obj.__module__ == "bot"
)
assert workers, "no agent workers found in bot.py"

clients = []
for name in workers:
    worker = getattr(bot, name)(state=None, context=LLMContext(), call_context=None)
    clients.extend(getattr(worker, "_mcp_clients", []))
assert clients, "no MCP client was constructed"
for client in clients:
    assert isinstance(client, MCPClient)
    assert callable(client.start) and callable(client.close) and callable(client.register_tools)
print("smoke ok:", len(clients), "mcp client(s)")
`

const pipecatMCPTransactionSmokeScript = `"""Exercise generated MCP collision and cleanup paths against Pipecat 1.7."""
import asyncio
import inspect
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "https://mcp.example/mcp")

import bot  # noqa: E402
from mcp.types import ListToolsResult, Tool  # noqa: E402
from pipecat.processors.aggregators.llm_context import LLMContext  # noqa: E402
from pipecat.workers.llm import LLMWorker  # noqa: E402


class FakeSession:
    def __init__(self, names=(), error=None, entered=None, block=False):
        self.names = names
        self.error = error
        self.entered = entered
        self.block = block

    async def list_tools(self):
        if self.entered:
            self.entered.set()
        if self.block:
            await asyncio.Event().wait()
        if self.error:
            raise self.error
        return ListToolsResult(
            tools=[
                Tool(
                    name=name,
                    description=f"Schema for {name}.",
                    inputSchema={"type": "object", "properties": {}},
                )
                for name in self.names
            ]
        )


class FakeExitStack:
    def __init__(self, error=None):
        self.error = error
        self.calls = 0

    async def aclose(self):
        self.calls += 1
        if self.error:
            raise self.error


worker_types = [
    value
    for value in vars(bot).values()
    if inspect.isclass(value)
    and issubclass(value, LLMWorker)
    and value.__module__ == "bot"
    and value.__name__.endswith("Agent")
]
mcp_worker_type = next(worker_type for worker_type in worker_types if "start_mcp" in vars(worker_type))


def new_worker(sessions, close_errors=None):
    close_errors = close_errors or [None] * len(sessions)
    worker = mcp_worker_type(state=None, context=LLMContext(), call_context=None)
    clients = worker._mcp_clients
    assert len(clients) == len(sessions), clients
    stacks = []
    for client, session, close_error in zip(clients, sessions, close_errors, strict=True):
        stack = FakeExitStack(close_error)
        client._active_session = session
        client._exit_stack = stack
        stacks.append(stack)
    return worker, clients, stacks


def allowed_name(client, index):
    return client._tools_filter[0] if client._tools_filter else f"source_{index}"


async def start_owned(worker):
    try:
        await worker.start_mcp()
    except BaseException:
        await bot._close_mcp((worker.close_mcp(),), suppress=True)
        raise


async def main():
    messages = []
    sink = bot.logger.add(lambda message: messages.append(str(message)), level="ERROR")
    try:
        # A failure in the second source preserves that error, closes both clients,
        # and never advertises the schema discovered from the first source.
        probe, probe_clients, _ = new_worker([FakeSession(), FakeSession()])
        allowed_names = [allowed_name(client, index) for index, client in enumerate(probe_clients)]
        await probe.close_mcp()
        first_name = allowed_names[0]
        setup_error = RuntimeError("primary setup https://secret.example/token")
        close_errors = [
            RuntimeError("first close https://secret.example/one"),
            RuntimeError("second close https://secret.example/two"),
        ]
        worker, clients, stacks = new_worker(
            [FakeSession([first_name]), FakeSession(error=setup_error)],
            close_errors,
        )
        try:
            await start_owned(worker)
        except RuntimeError as caught:
            assert caught is setup_error
        else:
            raise AssertionError("second MCP source failure was swallowed")
        assert [stack.calls for stack in stacks] == [1, 1]
        assert not worker.llm.has_function(first_name)
        assert all(client._active_session is None for client in clients)

        # Cancellation during discovery also rolls back every already-started source.
        entered = asyncio.Event()
        worker, clients, stacks = new_worker(
            [FakeSession([allowed_names[0]]), FakeSession(entered=entered, block=True)]
        )
        owner = asyncio.current_task()
        assert owner is not None

        async def cancel_owner():
            await asyncio.wait_for(entered.wait(), timeout=1)
            owner.cancel()

        canceller = asyncio.create_task(cancel_owner())
        try:
            await start_owned(worker)
        except asyncio.CancelledError:
            pass
        else:
            raise AssertionError("MCP startup cancellation was swallowed")
        await canceller
        assert [stack.calls for stack in stacks] == [1, 1]
        assert all(client._active_session is None for client in clients)

        # Local and remote names share one namespace and fail before registration.
        local_sessions = []
        for index, name in enumerate(allowed_names):
            name = name if name == "firecrawl_search" else "lookup_customer"
            local_sessions.append(FakeSession([name]))
        worker, _, stacks = new_worker(local_sessions)
        try:
            await start_owned(worker)
        except RuntimeError as caught:
            assert "MCP tool name collision for 'lookup_customer'" in str(caught)
        else:
            raise AssertionError("local-to-MCP collision was accepted")
        assert [stack.calls for stack in stacks] == [1, 1]

        # Two MCP sources exposing the same selected name also fail before registration.
        worker, _, stacks = new_worker(
            [FakeSession(["firecrawl_search"]), FakeSession(["firecrawl_search"])]
        )
        try:
            await start_owned(worker)
        except RuntimeError as caught:
            assert "MCP tool name collision for 'firecrawl_search'" in str(caught)
        else:
            raise AssertionError("MCP-to-MCP collision was accepted")
        assert [stack.calls for stack in stacks] == [1, 1]
        assert not worker.llm.has_function("firecrawl_search")

        # Flow task handlers use the same Pipecat LLM registry as MCP handlers.
        flow_sessions = [
            FakeSession([name if name == "firecrawl_search" else "get_invoice"])
            for name in allowed_names
        ]
        worker, _, stacks = new_worker(flow_sessions)
        try:
            await start_owned(worker)
        except RuntimeError as caught:
            assert "MCP tool name collision for 'get_invoice'" in str(caught)
        else:
            raise AssertionError("task-to-MCP collision was accepted")
        assert [stack.calls for stack in stacks] == [1, 1]
        assert not worker.llm.has_function("get_invoice")

        # A close error cannot stop the next client from closing; the first error wins.
        first_close = RuntimeError("first direct close https://secret.example/three")
        second_close = RuntimeError("second direct close https://secret.example/four")
        worker, _, stacks = new_worker(
            [FakeSession(), FakeSession()], [first_close, second_close]
        )
        try:
            await worker.close_mcp()
        except RuntimeError as caught:
            assert caught is first_close
        else:
            raise AssertionError("MCP close failure was swallowed")
        assert [stack.calls for stack in stacks] == [1, 1]
    finally:
        bot.logger.remove(sink)

    error_log = "".join(messages)
    assert "RuntimeError" in error_log
    assert "secret.example" not in error_log
    print("pipecat MCP transaction smoke ok")


asyncio.run(main())
`

// TestSmokePipecatV1MCPToolSourceInstantiates is the proof gate for N40 on this
// driver, and for research R3's stale-checkout caveat: the reference checkout
// is older than the pinned SDK, so the emitted MCPClient shape is only claimed
// once it constructs against pipecat-ai as pinned, with the `mcp` extra the
// generated pyproject now asks uv to resolve.
func TestSmokePipecatV1MCPToolSourceInstantiates(t *testing.T) {
	runPipecatSmokeScript(t, "safe_core", nil, func(agent *ir.Agent) {
		agent.Tools["web_search"] = ir.Tool{
			Execution: ir.ToolMCP, URLEnv: "FIRECRAWL_MCP_URL",
			MCPTransport: ir.MCPTransportStreamableHTTP, MCPTools: []string{"firecrawl_search"},
			Auth:         &ir.ToolAuth{Type: ir.ToolAuthBearer, TokenEnv: "FIRECRAWL_API_KEY"},
			Interruption: ir.ToolProviderDefault, Effect: ir.ToolReturnsData,
		}
		// A second source with no transport and no selection, so the startup
		// chooser and the argument-free client are constructed too.
		agent.Tools["notes"] = ir.Tool{
			Execution: ir.ToolMCP, URLEnv: "NOTES_MCP_URL",
			Interruption: ir.ToolProviderDefault, Effect: ir.ToolReturnsData,
		}
		intake := agent.Agents["intake"]
		intake.Tools = append(intake.Tools, "web_search", "notes")
		agent.Agents["intake"] = intake
	}, mcpSmokeScript)
}

func TestSmokePipecatV1MCPTransactions(t *testing.T) {
	runPipecatSmokeScript(t, "safe_core", nil, func(agent *ir.Agent) {
		addPipecatTaskTransferFixture(agent)
		verify := agent.Tasks["verify"]
		verify.Tools = append(verify.Tools, "get_invoice")
		agent.Tasks["verify"] = verify
		agent.Tools["web_search"] = ir.Tool{
			Execution: ir.ToolMCP, URLEnv: "FIRECRAWL_MCP_URL",
			MCPTransport: ir.MCPTransportStreamableHTTP, MCPTools: []string{"firecrawl_search"},
			Interruption: ir.ToolProviderDefault, Effect: ir.ToolReturnsData,
		}
		agent.Tools["notes"] = ir.Tool{
			Execution: ir.ToolMCP, URLEnv: "NOTES_MCP_URL",
			MCPTransport: ir.MCPTransportStreamableHTTP,
			Interruption: ir.ToolProviderDefault, Effect: ir.ToolReturnsData,
		}
		intake := agent.Agents["intake"]
		intake.Tools = append(intake.Tools, "web_search", "notes")
		agent.Agents["intake"] = intake
	}, pipecatMCPTransactionSmokeScript)
}

// TestSmokeV17PipecatSpeechTracing proves the generated OTLP setup exports the
// native STT, LLM, and TTS tree under the named conversation trace (V21).
func TestSmokeV17PipecatSpeechTracing(t *testing.T) {
	runPipecatSmokeScript(t, "simple-prompt", nil, nil, pipecatRequestTracingSmokeScript)
}

func TestSmokeV17PipecatMCPToolTracing(t *testing.T) {
	runPipecatSmokeScript(t, "simple-prompt", nil, func(agent *ir.Agent) {
		agent.Tools["web_search"] = ir.Tool{
			Execution: ir.ToolMCP, URLEnv: "FIRECRAWL_MCP_URL",
			MCPTransport: ir.MCPTransportStreamableHTTP, MCPTools: []string{"firecrawl_search"},
			Interruption: ir.ToolProviderDefault, Effect: ir.ToolReturnsData,
		}
		entry := agent.Agents[agent.EntryAgent]
		entry.Tools = append(entry.Tools, "web_search")
		agent.Agents[agent.EntryAgent] = entry
	}, pipecatRequestTracingSmokeScript)
}

func TestSmokeV24PipecatSimplePromptStaticCheck(t *testing.T) {
	runPipecatSmokeScript(t, "simple-prompt", nil, nil, pipecatStaticCheckScript)
}

func TestSmokeV24PipecatTracedMCPOnlyStaticCheck(t *testing.T) {
	runPipecatSmokeScript(t, "simple-prompt", nil, func(agent *ir.Agent) {
		agent.Tools = map[string]ir.Tool{
			"web_search": {
				Execution: ir.ToolMCP, URLEnv: "FIRECRAWL_MCP_URL",
				MCPTransport: ir.MCPTransportStreamableHTTP, MCPTools: []string{"firecrawl_search"},
				Interruption: ir.ToolProviderDefault, Effect: ir.ToolReturnsData,
			},
		}
		entry := agent.Agents[agent.EntryAgent]
		entry.Tools = []string{"web_search"}
		agent.Agents[agent.EntryAgent] = entry
	}, pipecatStaticCheckScript)
}

// The Daily route needs its own ty run, because the transfer path is the one
// place the emitted bot calls a method that is not on BaseTransport. simple-prompt
// emits no transfer, so it could never have caught this: the route shipped a
// project that failed the very lint gate its own README promises. It now narrows
// with isinstance before calling the primitive, which fixes the type error and
// turns a browser-session transfer request from an AttributeError into a named
// failure the model can act on.
func TestSmokeV24PipecatDailyTransferStaticCheck(t *testing.T) {
	runPipecatSmokeScript(t, "daily_carrier", nil, nil, pipecatStaticCheckScript)
}

// TestSmokeV24PipecatExamplesStaticCheck holds raw Pipecat output to the bar
// LiveKit has had since V26, over the same examples: `uv run ruff check .`, the
// exact command a user would run in a generated project. It closes the gap where
// a lint regression in a Pipecat template was caught on one driver and missed on
// the other, and it only became runnable once the emitted pyproject declared a
// pinned ruff of its own.
//
// ty stays on simple-prompt (TestSmokeV24PipecatSimplePromptStaticCheck) rather
// than widening here: run over multi-task and task-groups it reports real type
// errors in emitted task code (self.context is `Unknown | None` at the snapshot
// and aggregator call sites, self.state likewise where results are assigned).
// Those are driver bugs to fix in their own change, not something to widen the
// gate into and leave red.
func TestSmokeV24PipecatExamplesStaticCheck(t *testing.T) {
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not available")
	}
	for _, example := range []string{"simple-prompt", "multi-task", "task-groups", "subagents"} {
		t.Run(example, func(t *testing.T) {
			pkg, err := spec.Load(examplePackagePath(example))
			if err != nil {
				t.Fatal(err)
			}
			agent, err := ir.Build(pkg)
			if err != nil {
				t.Fatal(err)
			}
			artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
			if err != nil {
				t.Fatal(err)
			}
			// Only the emitted project lands here: no smoke script alongside, so
			// ruff sees exactly what a user would compile.
			dir := t.TempDir()
			for _, file := range artifact.Files {
				path := filepath.Join(dir, file.Path)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, file.Content, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("uv", "run", "ruff", "check", ".")
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("uv run ruff check . failed:\n%s", out)
			}
		})
	}
}

func runPipecatSmoke(t *testing.T, example string, mutate func(*ir.Target), mutateAgent func(*ir.Agent)) {
	t.Helper()
	runPipecatSmokeScript(t, example, mutate, mutateAgent, smokeCheckScript)
}

func runPipecatSmokeScript(t *testing.T, example string, mutate func(*ir.Target), mutateAgent func(*ir.Agent), script string) {
	t.Helper()
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not available")
	}
	pkg, err := spec.Load(examplePackagePath(example))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if mutateAgent != nil {
		mutateAgent(agent)
	}
	tgt := targetByProvider(t, agent, ir.ProviderPipecat)
	if mutate != nil {
		mutate(&tgt)
	}
	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatal(err)
	}
	runGeneratedPipecatSmokeScript(t, artifact, script)
}

func runGeneratedPipecatSmokeScript(t *testing.T, artifact Artifact, script string) {
	t.Helper()
	dir := t.TempDir()
	for _, file := range artifact.Files {
		out := filepath.Join(dir, file.Path)
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, file.Content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "smoke_check.py"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}

	// uv resolves the emitted pyproject into a project venv (shared uv cache,
	// so repeat runs are fast) and runs the check inside it.
	cmd := exec.Command("uv", "run", "python", "smoke_check.py")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("smoke check failed:\n%s", out)
	} else {
		t.Logf("%s", out)
	}
}

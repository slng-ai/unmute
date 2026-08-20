//go:build smoke

package generate

import "testing"

// L1 pins the decoder against a captured line. These pin the producers, which is
// the half no Go test can reach: `examples/*/build/` is gitignored, so the
// emitted Python is never linted and never imported by the default suite. Here
// it runs against the real installed SDK, which is what catches a renamed field
// or a moved import after a version bump.
//
// Each script checks the pure mapping rather than driving a whole pipeline or
// joining a room: the mapping is where a field name lives, and it is the part
// that breaks when an SDK changes underneath.

const devMetricsPipecatSmokeScript = `"""Map a real pipecat LatencyBreakdown through the emitted producer."""
import io
import json
import os
from contextlib import redirect_stdout

os.environ["UNMUTE_DEV_METRICS"] = "1"

from pipecat.observers.user_bot_latency_observer import (  # noqa: E402
    FunctionCallMetrics,
    LatencyBreakdown,
    TextAggregationBreakdownMetrics,
    TTFBBreakdownMetrics,
    UserBotLatencyObserver,
)

import dev_metrics  # noqa: E402

breakdown = LatencyBreakdown(
    ttfb=[
        TTFBBreakdownMetrics(
            processor="DeepgramSTTService", model="nova-3", start_time=1.0, duration_secs=0.11
        ),
        TTFBBreakdownMetrics(
            processor="OpenAILLMService", model="gpt-4o", start_time=1.1, duration_secs=0.284
        ),
        TTFBBreakdownMetrics(
            processor="CartesiaTTSService", model="sonic-2", start_time=1.4, duration_secs=0.361
        ),
    ],
    text_aggregation=TextAggregationBreakdownMetrics(
        processor="CartesiaTTSService", start_time=1.3, duration_secs=0.042
    ),
    user_turn_start_time=0.9,
    user_turn_secs=0.412,
    function_calls=[
        FunctionCallMetrics(
            function_name="check_availability", start_time=1.0, duration_secs=0.834
        )
    ],
)

record = dev_metrics.turn_record(breakdown, 1, 1.057)
assert record["kind"] == "turn", record
assert record["seq"] == 1, record
assert record["e2e"] == 1.057, record
assert record["user_turn"] == 0.412, record
assert record["text_aggregation"] == 0.042, record
assert [s["kind"] for s in record["stages"]] == ["stt", "llm", "tts"], record
assert all("total" not in s for s in record["stages"]), record
assert record["tools"] == [{"name": "check_availability", "seconds": 0.834}], record
assert "transcription" not in record, "pipecat does not report it, so it must be absent"

# The observer the bot passes to observers= accepts our handlers.
assert dev_metrics.install_dev_metrics(UserBotLatencyObserver()) is not None

buf = io.StringIO()
with redirect_stdout(buf):
    dev_metrics._emit(record)
line = buf.getvalue().strip()
assert line.startswith(dev_metrics.SENTINEL + " "), line
assert json.loads(line[len(dev_metrics.SENTINEL) + 1 :]) == record, line

# Switched off, the producer attaches nothing and prints nothing.
del os.environ["UNMUTE_DEV_METRICS"]
import importlib  # noqa: E402

reloaded = importlib.reload(dev_metrics)
observer = UserBotLatencyObserver()
buf = io.StringIO()
with redirect_stdout(buf):
    reloaded.install_dev_metrics(observer)
assert buf.getvalue() == "", buf.getvalue()

print("ok", line)
`

const devMetricsLiveKitSmokeScript = `"""Map a real livekit MetricsReport through the emitted producer."""
import io
import json
import os
from contextlib import redirect_stdout

os.environ["UNMUTE_DEV_METRICS"] = "1"

from livekit.agents.llm import ChatMessage, MetricsReport  # noqa: E402,F401
from livekit.agents.voice import (  # noqa: E402,F401
    AgentSession,
    ConversationItemAddedEvent,
    ToolExecutionUpdatedEvent,
)

import dev_metrics  # noqa: E402

user = {
    "end_of_turn_delay": 0.388,
    "transcription_delay": 0.18,
    "stt_metadata": {"model_name": "nova-3", "model_provider": "deepgram"},
}
assistant = {
    "e2e_latency": 1.204,
    "llm_node_ttft": 0.302,
    "tts_node_ttfb": 0.377,
    "started_speaking_at": 101.0,
    "stopped_speaking_at": 102.98,
    "llm_metadata": {"model_name": "gpt-4o", "model_provider": "openai"},
    "tts_metadata": {"model_name": "sonic-2", "model_provider": "cartesia"},
}

record = dev_metrics.turn_record(
    1, user, assistant, interrupted=True, tools=[{"name": "check_availability", "seconds": 0.9}]
)
assert record["kind"] == "turn", record
assert record["e2e"] == 1.204, record
assert record["user_turn"] == 0.388, record
assert record["transcription"] == 0.18, record
assert record["interrupted"] is True, record
kinds = [s["kind"] for s in record["stages"]]
assert kinds == ["stt", "llm", "tts"], record
assert record["stages"][0] == {"kind": "stt", "name": "deepgram", "model": "nova-3"}, record
assert record["stages"][2]["total"] == 1.98, record
assert record["tools"] == [{"name": "check_availability", "seconds": 0.9}], record
assert "text_aggregation" not in record, "livekit does not report it, so it must be absent"

# A real ChatMessage carries the two things the session handler reads.
message = ChatMessage(role="assistant", content=["hello"], metrics=assistant)
assert message.metrics["e2e_latency"] == 1.204
assert message.interrupted is False

buf = io.StringIO()
with redirect_stdout(buf):
    dev_metrics._emit(record)
line = buf.getvalue().strip()
assert line.startswith(dev_metrics.SENTINEL + " "), line
assert json.loads(line[len(dev_metrics.SENTINEL) + 1 :]) == record, line

print("ok", line)
`

func TestSmokePipecatDevMetricsMapsARealBreakdown(t *testing.T) {
	runPipecatSmokeScript(t, "salon-concierge", nil, nil, devMetricsPipecatSmokeScript)
}

func TestSmokeLiveKitDevMetricsMapsARealMetricsReport(t *testing.T) {
	runLiveKitSmokeScript(t, "salon-concierge", nil, nil, devMetricsLiveKitSmokeScript)
}

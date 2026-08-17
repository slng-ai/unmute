//go:build smoke

package generate

import (
	"os/exec"
	"testing"
)

// L4 smoke for the variables surface (variable_secrets_specs.md T12): the
// emitted render helper, the refusal gate, and the generated capture tool are
// exercised against the real SDK in a real venv. Opt-in (`make smoke`).

func TestSmokeSLNGSnapshotRefreshLimitsAndTextConversion(t *testing.T) {
	script := slngSnapshotHelperSource + `

class PipecatState:
    pass


class LiveKitUserdata:
    pass


def check_state(state):
    state.name = "Ada"
    state.count = 7
    state.confirmed = True
    state.unrelated = "must not leave the process"

    first = _slng_snapshot(state, ["name", "count", "confirmed", "task_value"])
    assert first == {
        "name": "Ada",
        "count": "7",
        "confirmed": "True",
        "task_value": "",
    }, first
    assert "unrelated" not in first, first

    state.name = "Grace"
    state.task_value = "booked"
    refreshed = _slng_snapshot(state, ["name", "task_value"])
    assert refreshed == {"name": "Grace", "task_value": "booked"}, refreshed
    assert first["name"] == "Ada" and first["task_value"] == "", first

    calls = []

    def sdk_call(snapshot):
        calls.append(snapshot)

    state.notes = "x" * 4000
    assert len(_slng_snapshot(state, ["notes"])["notes"]) == 4000

    value = "do-not-echo-" + ("x" * 4001)
    state.notes = value
    try:
        snapshot = _slng_snapshot(state, ["notes"])
        sdk_call(snapshot)
    except RuntimeError as exc:
        message = str(exc)
        assert "notes" in message, message
        assert value not in message, message
    else:
        raise AssertionError("oversized snapshot was accepted")
    assert calls == [], calls


# Exercise the same lowering against both generated targets' state shape.
check_state(PipecatState())
check_state(LiveKitUserdata())
print("slng snapshot ok")
`
	cmd := exec.Command("python3", "-c", script)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("SLNG snapshot smoke failed: %v\n%s", err, output)
	}
}

// pipecatVariablesSmokeScript imports the emitted bot, then drives the three
// pieces directly: rendering with and without a value, the refusal that keeps a
// half-formed request off the wire, and the capture tool writing State.
const pipecatVariablesSmokeScript = `"""Smoke check: templates, refusal, and capture on the emitted Pipecat bot."""
import asyncio
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")
# The emitted telephony module builds a Redis client at import, so this one env
# needs a real scheme. Nothing connects: the helpers under test never use it.
os.environ["REDIS_URL"] = "redis://127.0.0.1:6379/0"
# Input variables are required on this outbound package: supply them the way
# unmute dev --var does.
os.environ["UNMUTE_CALL_START"] = json.dumps(
    {"name": "Ada", "customer_id": "cus_1042", "appointment_time": "tomorrow at 3 pm"}
)

import bot  # noqa: E402

# A conversation variable is not a call-context field: it must never appear in
# the startup check, or every call would fail before the greeting (B3).
state = bot.build_state({"to_number": "+15551230000"})
assert state.name == "Ada", state.name
assert state.dialed_number == "+15551230000", state.dialed_number
assert state.reschedule_to is None, state.reschedule_to

# Rendering substitutes the value and leaves the literal alone.
rendered = bot._render("Hi {{name}}, see you {{appointment_time}}.", state)
assert rendered == "Hi Ada, see you tomorrow at 3 pm.", rendered

# A path renders with its values URL-encoded, separators untouched.
state.customer_id = "cus/10 42"
path = bot._render("/customers/{{customer_id}}/appointments", state, quote_values=True)
assert path == "/customers/cus%2F10%2042/appointments", path
state.customer_id = "cus_1042"

# An unset variable produces a refusal naming it, not a request.
assert state.reschedule_to is None
refusal = bot._refusal("reschedule_appointment", state, [("reschedule_to", "the new slot")])
assert "reschedule_to" in refusal and "reschedule_appointment" in refusal, refusal
# Once set, the same check passes silently.
state.reschedule_to = "Friday at 4"
assert bot._refusal("reschedule_appointment", state, [("reschedule_to", "the new slot")]) == ""

# The generated capture tool writes the state and reports what it saved.
agent = bot.ReminderAgent(state=state, context=None, call_context=None)
saved = {}


class _Params:
    async def result_callback(self, value, **kwargs):
        saved.update(value)


state.reschedule_to = None
asyncio.run(agent.update_variables(_Params(), reschedule_to="Saturday at noon"))
assert state.reschedule_to == "Saturday at noon", state.reschedule_to
assert saved == {"saved": ["reschedule_to"]}, saved

# A second dispatch payload lands on a fresh state.
os.environ["UNMUTE_CALL_START"] = json.dumps({"name": "Grace", "customer_id": "cus_7", "appointment_time": "Monday"})
dispatched = bot.build_state({"to_number": "+15551230000"})
assert dispatched.name == "Grace", dispatched.name

print("pipecat variables ok")
`

func TestSmokePipecatVariablesRenderRefuseAndCapture(t *testing.T) {
	runPipecatSmokeScript(t, "outbound-reminder", nil, nil, pipecatVariablesSmokeScript)
}

// livekitVariablesSmokeScript does the same against the emitted LiveKit agent:
// the module imports on the real SDK, and the helpers behave.
const livekitVariablesSmokeScript = `"""Smoke check: templates, refusal, and capture on the emitted LiveKit agent."""
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")
os.environ["REDIS_URL"] = "redis://127.0.0.1:6379/0"

import agent as generated  # noqa: E402

userdata = generated.Userdata()
userdata.name = "Ada"
userdata.customer_id = "cus_1042"

rendered = generated._render("Hi {{name}}!", userdata)
assert rendered == "Hi Ada!", rendered

path = generated._render("/customers/{{customer_id}}/appointments", userdata, quote_values=True)
assert path == "/customers/cus_1042/appointments", path

refusal = generated._refusal("reschedule_appointment", userdata, [("reschedule_to", "the new slot")])
assert "reschedule_to" in refusal, refusal
userdata.reschedule_to = "Friday at 4"
assert generated._refusal("reschedule_appointment", userdata, [("reschedule_to", "the new slot")]) == ""

# The dispatch stand-in is validated and applied.
os.environ["UNMUTE_CALL_START"] = json.dumps({"name": "Grace", "customer_id": "cus_7", "appointment_time": "Monday"})
values = generated._dispatched_call_start({})
fresh = generated.Userdata()
generated._hydrate_call_start(fresh, values)
assert fresh.name == "Grace", fresh.name

# The capture tool is a real function tool on the agent class.
assert hasattr(generated.Reminder, "update_variables"), "update_variables missing"

print("livekit variables ok")
`

func TestSmokeLiveKitVariablesRenderRefuseAndCapture(t *testing.T) {
	runLiveKitSmokeScript(t, "outbound-reminder", nil, nil, livekitVariablesSmokeScript)
}

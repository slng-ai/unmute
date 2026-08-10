//go:build smoke

package generate

import "testing"

// L4 smoke for the variables surface (variable_secrets_specs.md T12): the
// emitted render helper, the refusal gate, and the generated capture tool are
// exercised against the real SDK in a real venv. Opt-in (`make smoke`).

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

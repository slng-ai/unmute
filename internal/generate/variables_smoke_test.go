//go:build smoke

package generate

import "testing"

// L4 smoke for the variables surface (variable_secrets_specs.md T12): the
// emitted render helper and the refusal gate are exercised against the real SDK
// in a real venv. Opt-in (`make smoke`).
// addReminderVariables (smoke_fixture_test.go) adds the sources this suite
// drives; the salon ships one, so none of the three would be emitted without it.

// pipecatVariablesSmokeScript imports the emitted bot, then drives the two
// pieces directly: rendering with and without a value, and the refusal that
// keeps a half-formed request off the wire.
const pipecatVariablesSmokeScript = `"""Smoke check: templates and refusal on the emitted Pipecat bot."""
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")
# The fixture's dispatch variables are what the render assertions read, so they
# are supplied the way unmute dev --var does.
os.environ["UNMUTE_CALL_START"] = json.dumps(
    {"name": "Ada", "customer_phone": "+34600111222", "appointment_time": "tomorrow at 3 pm"}
)

import bot  # noqa: E402

state = bot.build_state()
assert state.name == "Ada", state.name
# Not hydrated: this pipecat target has no telephony plane. The field is still on
# State, which is what every tool reads it off.
assert state.dialed_number is None, state.dialed_number
state.dialed_number = "+15551230000"
assert state.reschedule_to is None, state.reschedule_to

# Rendering substitutes the value and leaves the literal alone.
rendered = bot._render("Hi {{name}}, see you {{appointment_time}}.", state)
assert rendered == "Hi Ada, see you tomorrow at 3 pm.", rendered

# A path renders with its values URL-encoded, separators untouched. Set directly:
# the salon types customer_phone as E.164, so this shape can only be rendered,
# never saved.
state.customer_phone = "cus/10 42"
path = bot._render(
    "/customers/{{customer_phone}}/appointments",
    state,
    quote_values=True,
    site="task:verify_customer",
)
assert path == "/customers/cus%2F10%2042/appointments", path
state.customer_phone = "+34600111222"
# verify_customer assigns both fields, and the status is a required Literal.
bot._save_result(
    "verify_customer",
    state,
    {"customer_phone": state.customer_phone, "customer_status": "existing"},
)

# An unset variable produces a refusal naming it, not a request.
assert state.reschedule_to is None
refusal = bot._refusal("reschedule_appointment", state, [("reschedule_to", "the new slot")])
assert "reschedule_to" in refusal and "reschedule_appointment" in refusal, refusal
# Once set, the same check passes silently.
state.reschedule_to = "Friday at 4"
assert bot._refusal("reschedule_appointment", state, [("reschedule_to", "the new slot")]) == ""

# A second dispatch payload lands on a fresh state.
os.environ["UNMUTE_CALL_START"] = json.dumps({"name": "Grace", "customer_phone": "+34600111333", "appointment_time": "Monday"})
dispatched = bot.build_state()
assert dispatched.name == "Grace", dispatched.name

print("pipecat variables ok")
`

func TestSmokePipecatVariablesRenderAndRefuse(t *testing.T) {
	runPipecatSmokeScript(t, "salon-concierge", nil, addReminderVariables, pipecatVariablesSmokeScript)
}

// livekitVariablesSmokeScript does the same against the emitted LiveKit agent:
// the module imports on the real SDK, and the helpers behave.
const livekitVariablesSmokeScript = `"""Smoke check: templates and refusal on the emitted LiveKit agent."""
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")
os.environ["REDIS_URL"] = "redis://127.0.0.1:6379/0"

import agent as generated  # noqa: E402

userdata = generated.Userdata()
userdata.name = "Ada"
userdata.customer_phone = "+34600111222"

rendered = generated._render("Hi {{name}}!", userdata)
assert rendered == "Hi Ada!", rendered

path = generated._render(
    "/customers/{{customer_phone}}/appointments",
    userdata,
    quote_values=True,
    site="task:verify_customer",
)
# The plus is percent-encoded too.
assert path == "/customers/%2B34600111222/appointments", path
# verify_customer assigns both fields, and the status is a required Literal.
generated._save_result(
    "verify_customer",
    userdata,
    {"customer_phone": userdata.customer_phone, "customer_status": "existing"},
)

refusal = generated._refusal("reschedule_appointment", userdata, [("reschedule_to", "the new slot")])
assert "reschedule_to" in refusal, refusal
userdata.reschedule_to = "Friday at 4"
assert generated._refusal("reschedule_appointment", userdata, [("reschedule_to", "the new slot")]) == ""

# The dispatch stand-in is validated and applied.
os.environ["UNMUTE_CALL_START"] = json.dumps({"name": "Grace", "customer_phone": "+34600111333", "appointment_time": "Monday"})
values = generated._dispatched_call_start({})
fresh = generated.Userdata()
generated._hydrate_call_start(fresh, values)
assert fresh.name == "Grace", fresh.name

print("livekit variables ok")
`

func TestSmokeLiveKitVariablesRenderAndRefuse(t *testing.T) {
	runLiveKitSmokeScript(t, "salon-concierge", nil, addReminderVariables, livekitVariablesSmokeScript)
}

//go:build smoke

package generate

import "testing"

// L4 smoke for the webhook side of the variables surface. No shipped example
// has a webhook tool, so useWebhookTools (smoke_fixture_test.go) adds two to the
// resolved salon package in memory. They then prove the full request against a
// real in-process HTTP server without making the live example depend on an API.
//
// Nothing external is contacted: the server is a stdlib handler on 127.0.0.1.

// echoServerPreamble is the captured-request server both scripts share. It sets
// the tool's url_env with a trailing slash on purpose, so the emitted
// rstrip("/") is exercised and a doubled slash would show up in the asserted path.
const echoServerPreamble = `
import json
import os
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

captured = {"count": 0}


class _Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(length) if length else b"{}"
        captured["count"] += 1
        captured["path"] = self.path
        captured["auth"] = self.headers.get("Authorization")
        captured["headers"] = {k.lower(): v for k, v in self.headers.items()}
        captured["body"] = json.loads(raw or b"{}")
        payload = json.dumps({"ok": True, "echo": captured["body"]}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, *args):
        pass


_server = ThreadingHTTPServer(("127.0.0.1", 0), _Handler)
threading.Thread(target=_server.serve_forever, daemon=True).start()

for _name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(_name, "smoke-placeholder")
os.environ["REDIS_URL"] = "redis://127.0.0.1:6379/0"
# The tool reads these at call time, so they are set after the placeholders.
os.environ["SALON_API_URL"] = "http://127.0.0.1:" + str(_server.server_address[1]) + "/"
os.environ["SALON_API_TOKEN"] = "test-token-abc123"
# A customer id with a slash and a space: proof that a rendered path segment is
# URL-encoded and cannot rewrite the route. The phone is E.164 because the salon
# types it that way and _save_result refuses anything else.
os.environ["UNMUTE_CALL_START"] = json.dumps(
    {
        "name": "Ada",
        "customer_id": "cus/10 42",
        "customer_phone": "+34600111222",
        "appointment_time": "tomorrow at 3 pm",
    }
)
`

const pipecatWebhookSmokeScript = echoServerPreamble + `
import asyncio

import bot  # noqa: E402


class _Params:
    def __init__(self):
        self.result = None

    async def result_callback(self, value, **kwargs):
        self.result = value


state = bot.build_state()
# Set here, not hydrated: this pipecat target has no telephony plane.
state.dialed_number = "+15551230000"
# verify_customer assigns both fields, and the status is a required Literal.
bot._save_result(
    "verify_customer",
    state,
    {"customer_phone": state.customer_phone, "customer_status": "existing"},
)
agent = bot.ConciergeAgent(state=state, context=None, call_context=None)

# 1. A tool whose injected values are all available: the request goes out.
params = _Params()
asyncio.run(agent.confirm_appointment(params))

assert captured["count"] == 1, captured
assert captured["path"] == "/customers/cus%2F10%2042/appointments/confirm", captured["path"]
assert captured["auth"] == "Bearer test-token-abc123", captured["auth"]
assert captured["body"] == {
    "channel": "phone",
    "customer_phone": "+34600111222",
    "dialed_number": "+15551230000",
}, captured["body"]
assert params.result["ok"] is True, params.result

# 2. A tool injecting an unset variable: refused, nothing sent.
assert state.reschedule_to is None
refused = _Params()
asyncio.run(agent.reschedule_appointment(refused))
assert captured["count"] == 1, "a refused call must not reach the network"
assert "refused" in refused.result, refused.result
assert "reschedule_to" in refused.result["refused"], refused.result

# 3. Once the value is set, the same tool sends it in the body.
state.reschedule_to = "Friday at 4"
allowed = _Params()
asyncio.run(agent.reschedule_appointment(allowed))
assert captured["count"] == 2, captured
assert captured["path"] == "/customers/cus%2F10%2042/appointments", captured["path"]
assert captured["body"] == {
    "customer_phone": "+34600111222",
    "new_time": "Friday at 4",
}, captured["body"]
assert allowed.result["ok"] is True, allowed.result

print("pipecat webhook ok:", captured["path"], captured["body"], captured["auth"])
`

func TestSmokePipecatWebhookSendsInjectedBodyPathAndToken(t *testing.T) {
	runPipecatSmokeScript(t, "salon-concierge", nil, useWebhookTools, pipecatWebhookSmokeScript)
}

const livekitWebhookSmokeScript = echoServerPreamble + `
import asyncio
from types import SimpleNamespace

import agent as generated  # noqa: E402

userdata = generated.Userdata()
generated._hydrate_call_start(userdata, generated._dispatched_call_start({}))
userdata.dialed_number = "+15551230000"
# verify_customer assigns both fields, and the status is a required Literal.
generated._save_result(
    "verify_customer",
    userdata,
    {"customer_phone": userdata.customer_phone, "customer_status": "existing"},
)
ctx = SimpleNamespace(userdata=userdata)
desk = generated.Concierge()

# 1. All injected values available: the request goes out.
result = asyncio.run(desk.confirm_appointment(ctx))

assert captured["count"] == 1, captured
assert captured["path"] == "/customers/cus%2F10%2042/appointments/confirm", captured["path"]
assert captured["auth"] == "Bearer test-token-abc123", captured["auth"]
assert captured["body"] == {
    "channel": "phone",
    "customer_phone": "+34600111222",
    "dialed_number": "+15551230000",
}, captured["body"]
assert result["ok"] is True, result

# 2. An unset variable refuses before any request is made.
assert userdata.reschedule_to is None
refused = asyncio.run(desk.reschedule_appointment(ctx))
assert captured["count"] == 1, "a refused call must not reach the network"
assert "refused" in refused, refused
assert "reschedule_to" in refused["refused"], refused

# 3. Once the value is set, the same tool sends it in the body.
userdata.reschedule_to = "Friday at 4"
allowed = asyncio.run(desk.reschedule_appointment(ctx))
assert captured["count"] == 2, captured
assert captured["body"] == {
    "customer_phone": "+34600111222",
    "new_time": "Friday at 4",
}, captured["body"]
assert allowed["ok"] is True, allowed

print("livekit webhook ok:", captured["path"], captured["body"], captured["auth"])
`

func TestSmokeLiveKitWebhookSendsInjectedBodyPathAndToken(t *testing.T) {
	runLiveKitSmokeScript(t, "salon-concierge", nil, useWebhookTools, livekitWebhookSmokeScript)
}

// The api_key scheme is the other half of SCHEMA §5.3 and no example uses it, so
// it is exercised by rewriting the resolved tool's auth before generating: the
// token goes in its own named header and no Authorization header is sent.
const pipecatAPIKeySmokeScript = echoServerPreamble + `
import asyncio

import bot  # noqa: E402


class _Params:
    def __init__(self):
        self.result = None

    async def result_callback(self, value, **kwargs):
        self.result = value


state = bot.build_state()
# Set here, not hydrated: this pipecat target has no telephony plane.
state.dialed_number = "+15551230000"
# verify_customer assigns both fields, and the status is a required Literal.
bot._save_result(
    "verify_customer",
    state,
    {"customer_phone": state.customer_phone, "customer_status": "existing"},
)
agent = bot.ConciergeAgent(state=state, context=None, call_context=None)
asyncio.run(agent.confirm_appointment(_Params()))

assert captured["count"] == 1, captured
assert captured["headers"].get("x-api-key") == "test-token-abc123", captured["headers"]
assert captured["auth"] is None, "api_key must not send an Authorization header"

# A required secret missing fails loudly, naming it (V12).
import os  # noqa: E402

saved = os.environ.pop("SALON_API_TOKEN")
try:
    bot.require_env()
except RuntimeError as exc:
    assert "SALON_API_TOKEN" in str(exc), str(exc)
else:
    raise AssertionError("a missing required secret must fail the startup check")
finally:
    os.environ["SALON_API_TOKEN"] = saved

print("pipecat api_key + startup check ok:", captured["headers"].get("x-api-key"))
`

func TestSmokePipecatAPIKeySchemeAndStartupCheck(t *testing.T) {
	runPipecatSmokeScript(t, "salon-concierge", nil, useAPIKeyAuth, pipecatAPIKeySmokeScript)
}

const livekitAPIKeySmokeScript = echoServerPreamble + `
import asyncio
from types import SimpleNamespace

import agent as generated  # noqa: E402

userdata = generated.Userdata()
generated._hydrate_call_start(userdata, generated._dispatched_call_start({}))
userdata.dialed_number = "+15551230000"
# verify_customer assigns both fields, and the status is a required Literal.
generated._save_result(
    "verify_customer",
    userdata,
    {"customer_phone": userdata.customer_phone, "customer_status": "existing"},
)
asyncio.run(generated.Concierge().confirm_appointment(SimpleNamespace(userdata=userdata)))

assert captured["count"] == 1, captured
assert captured["headers"].get("x-api-key") == "test-token-abc123", captured["headers"]
assert captured["auth"] is None, "api_key must not send an Authorization header"

import os  # noqa: E402

saved = os.environ.pop("SALON_API_TOKEN")
try:
    generated.require_env()
except RuntimeError as exc:
    assert "SALON_API_TOKEN" in str(exc), str(exc)
else:
    raise AssertionError("a missing required secret must fail the startup check")
finally:
    os.environ["SALON_API_TOKEN"] = saved

print("livekit api_key + startup check ok:", captured["headers"].get("x-api-key"))
`

func TestSmokeLiveKitAPIKeySchemeAndStartupCheck(t *testing.T) {
	runLiveKitSmokeScript(t, "salon-concierge", nil, useAPIKeyAuth, livekitAPIKeySmokeScript)
}

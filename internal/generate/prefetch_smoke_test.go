//go:build smoke

package generate

import "testing"

// The pre-fetch block, driven in the real emitted Python.
//
// Every assertion here is one a compile-time test cannot make. `asyncio.timeout`
// either bounds the block or it does not; `except Exception` either swallows or it
// does not; `ZoneInfo("Europe/Madrid")` either resolves in the base image or raises
// at import. A Go test comparing strings says nothing about any of those, and each
// one fails as a call that is never answered rather than as a build error.
//
// The four outcomes are driven in one module because that is also the claim: the
// same block, in the same process, resolves one entry, skips another, survives a
// timeout and survives an exception, and starts a session afterwards every time.
func TestSmokePrefetchOutcomes(t *testing.T) {
	runLiveKitSmokeScript(t, "salon-concierge", nil, nil, prefetchOutcomesSmokeScript)
}

func TestSmokePrefetchOutcomesPipecat(t *testing.T) {
	runPipecatSmokeScript(t, "salon-concierge", nil, nil, prefetchOutcomesPipecatSmokeScript)
}

// TestSmokePrefetchZoneResolves is separate and deliberately narrow: if a future
// base image drops its zone data, `ZoneInfo` raises at import and the module never
// starts. That is worth failing a test over rather than a call, and it is the one
// thing measurement showed both images currently do carry.
func TestSmokePrefetchZoneResolves(t *testing.T) {
	runLiveKitSmokeScript(t, "salon-concierge", nil, nil, prefetchZoneSmokeScript)
}

const prefetchOutcomesSmokeScript = `"""Smoke check: the four pre-fetch outcomes, on LiveKit."""
import asyncio
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

# No seeded facts: this is the shape a call with no caller ID has, and the shape
# the browser loop has with no --source.
os.environ.pop("UNMUTE_CALL_FACTS", None)

import agent  # noqa: E402

state = agent.Userdata()


def fresh():
    return agent.Userdata()


# 1. Resolved. The clock always reads, so this is the entry that proves the block
#    runs at all rather than being skipped wholesale.
resolved = fresh()
asyncio.run(agent._prefetch(resolved, None))
assert resolved.booking_date, "the clock entry resolved nothing"
assert len(resolved.booking_date) == 10, resolved.booking_date
assert resolved.booking_date.count("-") == 2, resolved.booking_date

# 2. Skipped, twice over: no call context, so the caller entry has nothing to read,
#    and the profile entry that reads what it would have assigned skips with it.
assert resolved.customer_phone == "", resolved.customer_phone
assert resolved.customer_name == "", resolved.customer_name
assert resolved._unconfirmed == set(), resolved._unconfirmed

# 2b. And with a call context, the caller entry resolves and is marked unconfirmed.
seeded = fresh()
asyncio.run(agent._prefetch(seeded, {"from_number": "+34600111222"}))
assert seeded.customer_phone == "+34600111222", seeded.customer_phone
assert "customer_phone" in seeded._unconfirmed, seeded._unconfirmed

# 3. Timed out. The budget is what stops a slow lookup delaying the greeting, and
#    the only way to see it work is to be slower than it.
original = agent.tools.look_up_customer.look_up_customer


async def too_slow(*_args, **_kwargs):
    await asyncio.sleep(agent._PREFETCH_BUDGET_S + 1)
    raise AssertionError("the budget did not fire")


agent.tools.look_up_customer.look_up_customer = too_slow
timed_out = fresh()
asyncio.run(agent._prefetch(timed_out, {"from_number": "+34600111222"}))
assert timed_out.customer_name == "", "a timed-out lookup wrote a value"
# The number still landed: the entry that timed out is the lookup, not the caller.
assert timed_out.customer_phone == "+34600111222", timed_out.customer_phone
assert timed_out.booking_date, "a timed-out lookup lost the clock reading too"

# 4. Raised. Anything at all, and the call still greets.
def explode(*_args, **_kwargs):
    raise RuntimeError("the customer store is down")


agent.tools.look_up_customer.look_up_customer = explode
failed = fresh()
asyncio.run(agent._prefetch(failed, {"from_number": "+34600111222"}))
assert failed.customer_name == "", "a failed lookup wrote a value"
assert failed.customer_phone == "+34600111222", failed.customer_phone
assert failed.booking_date, "a failed lookup lost the clock reading too"

agent.tools.look_up_customer.look_up_customer = original

# The block runs once per call and holds no state between calls: two runs on two
# state objects must not see each other's unconfirmed marks.
first, second = fresh(), fresh()
asyncio.run(agent._prefetch(first, {"from_number": "+34600111222"}))
asyncio.run(agent._prefetch(second, None))
assert "customer_phone" in first._unconfirmed, first._unconfirmed
assert second._unconfirmed == set(), second._unconfirmed

# And an unconfirmed value satisfies no gate, which is the whole point of marking
# it: the emitted guard is what a step consults before it starts.
assert agent._unmet_prerequisites(first, ["customer_phone"]) == ["customer_phone"]
first._unconfirmed.discard("customer_phone")
assert agent._unmet_prerequisites(first, ["customer_phone"]) == []

print("prefetch outcomes check passed")
`

const prefetchOutcomesPipecatSmokeScript = `"""Smoke check: the four pre-fetch outcomes, on Pipecat."""
import asyncio
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

os.environ.pop("UNMUTE_CALL_FACTS", None)

import bot  # noqa: E402


def fresh():
    return bot.build_state(None)


resolved = fresh()
asyncio.run(bot._prefetch(resolved, None))
assert resolved.booking_date, "the clock entry resolved nothing"
assert resolved.customer_phone == "", resolved.customer_phone
assert resolved._unconfirmed == set(), resolved._unconfirmed

seeded = fresh()
asyncio.run(bot._prefetch(seeded, {"from_number": "+34600111222"}))
assert seeded.customer_phone == "+34600111222", seeded.customer_phone
assert "customer_phone" in seeded._unconfirmed, seeded._unconfirmed

original = bot.tools.look_up_customer.look_up_customer


async def too_slow(*_args, **_kwargs):
    await asyncio.sleep(bot._PREFETCH_BUDGET_S + 1)
    raise AssertionError("the budget did not fire")


bot.tools.look_up_customer.look_up_customer = too_slow
timed_out = fresh()
asyncio.run(bot._prefetch(timed_out, {"from_number": "+34600111222"}))
assert timed_out.customer_name == "", "a timed-out lookup wrote a value"
assert timed_out.booking_date, "a timed-out lookup lost the clock reading too"


def explode(*_args, **_kwargs):
    raise RuntimeError("the customer store is down")


bot.tools.look_up_customer.look_up_customer = explode
failed = fresh()
asyncio.run(bot._prefetch(failed, {"from_number": "+34600111222"}))
assert failed.customer_name == "", "a failed lookup wrote a value"
assert failed.booking_date, "a failed lookup lost the clock reading too"

bot.tools.look_up_customer.look_up_customer = original

assert bot._unmet_prerequisites(seeded, ["customer_phone"]) == ["customer_phone"]
seeded._unconfirmed.discard("customer_phone")
assert bot._unmet_prerequisites(seeded, ["customer_phone"]) == []

print("prefetch outcomes check passed")
`

// The seed is read only where the carrier gave nothing, and this is the assertion
// that a stale value in an environment cannot reshape a real call.
const prefetchZoneSmokeScript = `"""Smoke check: the declared zone resolves, and the seed loses to the carrier."""
import asyncio
import json
import os
from datetime import datetime
from zoneinfo import ZoneInfo

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import agent  # noqa: E402

# The zone resolved at import, or this module would not have loaded. Assert it
# names the package's own zone rather than trusting that.
assert agent._PREFETCH_TZ == ZoneInfo("Europe/Madrid"), agent._PREFETCH_TZ

state = agent.Userdata()
asyncio.run(agent._prefetch(state, None))
assert state.booking_date == datetime.now(agent._PREFETCH_TZ).date().isoformat()

# The seed fills what the carrier did not.
os.environ["UNMUTE_CALL_FACTS"] = json.dumps({"from_number": "+34600111222"})
seeded = agent.Userdata()
asyncio.run(agent._prefetch(seeded, None))
assert seeded.customer_phone == "+34600111222", seeded.customer_phone

# And loses to what it did. This is the one that matters: a stale value left in a
# .env must not quietly replace a real caller's number.
carrier = agent.Userdata()
asyncio.run(agent._prefetch(carrier, {"from_number": "+34999888777"}))
assert carrier.customer_phone == "+34999888777", carrier.customer_phone

os.environ.pop("UNMUTE_CALL_FACTS", None)

print("prefetch zone check passed")
`

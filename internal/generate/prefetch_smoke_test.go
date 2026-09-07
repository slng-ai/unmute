//go:build smoke

package generate

import (
	"github.com/slng-ai/unmute/internal/ir"
	"testing"
)

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
assert resolved._unconfirmed == {"customer_phone", "customer_name", "customer_on_file"}, resolved._unconfirmed

# 2b. And with a call context, the caller entry resolves and is marked unconfirmed.
seeded = fresh()
asyncio.run(agent._prefetch(seeded, {"from_number": "+34600111222"}))
assert seeded.customer_phone == "+34600111222", seeded.customer_phone
assert "customer_phone" in seeded._unconfirmed, seeded._unconfirmed

# 2c. A withheld caller ID, in all three shapes it arrives in. This is the case a
#     Go test cannot settle, because the question is what the emitted Python does
#     with a real value.
#
#     Twilio sends the word "anonymous" when a caller withheld their number, and
#     where an upstream carrier sent a word instead, the keypad digits of it,
#     which are shaped exactly like a real number. All three mean absent.
for withheld in ("", "anonymous", "ANONYMOUS", "+266696687", "+8628245225"):
    hidden = fresh()
    asyncio.run(agent._prefetch(hidden, {"from_number": withheld}))
    assert hidden.customer_phone == "", (withheld, hidden.customer_phone)
    assert hidden._unconfirmed == {"customer_phone", "customer_name", "customer_on_file"}, (withheld, hidden._unconfirmed)

# And a real number is still a real number, including one that begins with the
# digits a placeholder spells. "unknown" spells 8656696, and +86 5669 6xxx is an
# ordinary Chinese mobile: reading it as withheld would throw away a real caller.
for real in ("+34600111222", "+865669612345"):
    kept = fresh()
    asyncio.run(agent._prefetch(kept, {"from_number": real}))
    assert kept.customer_phone == real, (real, kept.customer_phone)

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
assert second._unconfirmed == {"customer_phone", "customer_name", "customer_on_file"}, second._unconfirmed

# And an unconfirmed value refuses any tool call that would inject it, which is
# the whole point of marking it: the emitted _refusal helper is what a tool
# call consults before it runs.
assert agent._refusal("probe", first, [("customer_phone", "hint")]) != ""
first._unconfirmed.discard("customer_phone")
assert agent._refusal("probe", first, [("customer_phone", "hint")]) == ""

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
assert resolved._unconfirmed == {"customer_phone", "customer_name", "customer_on_file"}, resolved._unconfirmed

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

assert bot._refusal("probe", seeded, [("customer_phone", "hint")]) != ""
seeded._unconfirmed.discard("customer_phone")
assert bot._refusal("probe", seeded, [("customer_phone", "hint")]) == ""

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

# The zone the package declared, written out here rather than read back off the
# module. Reading the module's own constant was circular: an emitter that inlined
# the wrong zone would agree with itself and pass.
declared = ZoneInfo("Europe/Madrid")

state = agent.Userdata()
asyncio.run(agent._prefetch(state, None))
now = datetime.now(declared)
assert state.booking_date == now.date().isoformat(), state.booking_date
# The weekday names the day the date lands on. Only a real reading can show this:
# a compile-time test sees the expression, not that indexing the spelled-out tuple
# with weekday() lines up, and the tuple exists because strftime("%A") follows the
# container's locale.
assert state.booking_weekday == agent._PREFETCH_DAYS[now.weekday()], state.booking_weekday

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

func provenanceFixture(agent *ir.Agent) {
	variable := agent.Variables["customer_id"]
	variable.Confirm = "verify_caller"
	variable.ConfirmInherited = true
	agent.Variables["customer_id"] = variable
	agent.Prefetch = append(agent.Prefetch, ir.Prefetch{Name: "related_record", Tool: "lookup_customer", Inputs: []string{"caller_name"}, Args: []ir.Pair{{Key: "email", Value: "{{caller_name}}"}}, Assign: []ir.Pair{{Key: "customer_id", Value: "result.customer_id"}}, Confirm: "verify_caller"})
}
func TestSmokeConfirmationProvenanceLiveKit(t *testing.T) {
	runLiveKitSmokeScript(t, "prefetch_core", nil, provenanceFixture, provenanceScript("agent", "Userdata()"))
}
func TestSmokeConfirmationProvenancePipecat(t *testing.T) {
	runPipecatSmokeScript(t, "prefetch_core", nil, provenanceFixture, provenanceScript("bot", "build_state()"))
}
func provenanceScript(module, state string) string {
	return `import os,json
from copy import deepcopy
for name in json.load(open("compile-report.json"))["required_env"]: os.environ.setdefault(name,"smoke-placeholder")
import ` + module + ` as generated
state=generated.` + state + `
def seed():
    generated._save_batch(state,{"caller_phone":"+34600111222"},inputs=())
    generated._save_batch(state,{"caller_name":"OLD_PROFILE"},inputs=("caller_phone",))
    generated._save_batch(state,{"customer_id":"OLD_RELATED"},inputs=("caller_name",))
    generated._save_batch(state,{"booking_date":"2026-09-06"},inputs=())
seed()
assert generated._prompt_value(state,"caller_name")[1] is None
assert generated._prompt_value(state,"caller_name","task:verify_caller")[1]=="OLD_PROFILE"
generated._save_result("verify_caller",state,{"caller_phone":"+34600111222"})
assert generated._prompt_value(state,"caller_name")[1]=="OLD_PROFILE"
assert generated._prompt_value(state,"customer_id")[1]=="OLD_RELATED"
generated._save_result("verify_caller",state,{"caller_phone":"+34600999888"})
assert state.caller_name is None and state.customer_id is None
assert state.booking_date=="2026-09-06"
seed()
before=deepcopy(vars(state))
try: generated._save_batch(state,{"caller_phone":"+34600999888","verified":"invalid"},step="verify_caller")
except generated._StateRefused: pass
else: raise AssertionError("invalid batch saved")
assert vars(state)==before
# Explicit replacements in the same valid batch survive input invalidation.
generated._save_batch(state,{"caller_phone":"+34600999888","caller_name":"NEW_PROFILE"},step="verify_caller")
assert state.caller_name=="NEW_PROFILE" and state.customer_id is None
assert "caller_name" not in state._prefetch_provenance
# Another writer is not agreement, even when it repeats an agreed value.
generated._save_batch(state,{"caller_phone":"+34600999888"},step="another_task")
assert generated._prompt_value(state,"caller_phone")[1] is None
assert generated._prompt_value(state,"caller_name")[1] is None
fresh=generated.` + state + `
assert fresh._unconfirmed is not state._unconfirmed
assert not getattr(fresh,"_prefetch_provenance",{})
print("confirmation provenance: same-value agreement, changed/transitive invalidation, atomic retry, replacement, other writer, fresh call")
`
}

func deadlineFixture(agent *ir.Agent) {
	for _, entry := range agent.Prefetch {
		if entry.Tool != "" {
			entry.Name = "second_lookup"
			agent.Prefetch = append(agent.Prefetch, entry)
			break
		}
	}
}
func TestSmokePrefetchDeadlineLiveKit(t *testing.T) {
	runLiveKitSmokeScript(t, "salon-concierge", nil, deadlineFixture, deadlineScript("agent", "Userdata()"))
}
func TestSmokePrefetchDeadlinePipecat(t *testing.T) {
	runPipecatSmokeScript(t, "salon-concierge", nil, deadlineFixture, deadlineScript("bot", "build_state()"))
}
func deadlineScript(module, state string) string {
	return `import os,json,asyncio,time
for name in json.load(open("compile-report.json"))["required_env"]: os.environ.setdefault(name,"smoke-placeholder")
os.environ.pop("UNMUTE_CALL_FACTS",None)
import ` + module + ` as generated
generated._PREFETCH_BUDGET_S=0.12
async def main():
    calls=[]
    async def slow(**kwargs):
        calls.append(kwargs)
        try: await asyncio.sleep(0.08)
        except asyncio.CancelledError: await asyncio.sleep(0.1)
        return {"name":"LATE_NAME","customer_id":"LATE_ID"}
    generated.tools.look_up_customer.look_up_customer=slow
    state=generated.` + state + `
    start=time.monotonic()
    await generated._prefetch(state,{"from_number":"+34600111222"})
    elapsed=time.monotonic()-start
    assert elapsed<0.16,("fresh budget or waited for cancellation",elapsed)
    assert len(calls)==2,calls
    before=dict(vars(state))
    await asyncio.sleep(0.15)
    assert vars(state)==before,"late task mutated saved state"
    def blocking(**kwargs):
        time.sleep(0.25)
        return {"name":"BLOCKING_NAME","customer_id":"BLOCKING_ID"}
    generated.tools.look_up_customer.look_up_customer=blocking
    fresh=generated.` + state + `
    start=time.monotonic()
    await generated._prefetch(fresh,{"from_number":"+34600111222"})
    assert time.monotonic()-start<0.18,"sync handler blocked greeting"
    assert fresh.customer_name in (None,""),fresh.customer_name
    await asyncio.sleep(0.3)
    assert fresh.customer_name in (None,""),"late thread saved"
asyncio.run(main())
print("one startup deadline; sync offload; bounded cancellation; no late saves")
`
}

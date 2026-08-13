# Contract: the carrier markup

**Feature**: [../spec.md](../spec.md) | **Date**: 2026-08-13

Three pieces of TwiML exist on this route. None is an emitted file: the first is
dictated by the README for the operator to paste into a TwiML Bin, the second is
inside the README's outbound command, the third is composed by the bot at
transfer time. All three name the same service host and the same wss URL through
one template helper, so they cannot disagree (data-model §3).

## 1. The inbound Bin (dictated by the README)

```xml
<?xml version="1.0" encoding="UTF-8"?>
<Response>
  <Say>Connecting you now.</Say>
  <Connect>
    <Stream url="wss://[region.]api.pipecat.daily.co/ws/twilio">
      <Parameter name="_pipecatCloudServiceHost" value="AGENT_NAME.YOUR_ORGANIZATION"/>
      <Parameter name="from_number" value="{{From}}"/>
      <Parameter name="to_number" value="{{To}}"/>
    </Stream>
  </Connect>
</Response>
```

Line by line, because every line is a decision:

- `<Say>` before `<Connect>`: a cold start is seconds of silence otherwise; a
  caller who hears nothing hangs up (spec SC-002). The README notes the line can
  be deleted once a warm instance is kept (research D2).
- The wss URL is regional exactly when the target declares a region; the README
  renders the right one so the operator never learns regions exist unless they
  declared one (spec FR-004).
- `AGENT_NAME` is rendered by the compiler (it is the deploy manifest's name);
  `YOUR_ORGANIZATION` is the one placeholder the operator fills, from
  `pipecat cloud organizations list` (research F2).
- `{{From}}`/`{{To}}` are Twilio Bin template substitutions; they are what
  populates the parsed handshake's caller and callee fields (research D9, F3).

## 2. The outbound TwiML (inside the README's one command)

The command creates the call at Twilio; the TwiML connects the answered call's
stream to the platform and marks the session outbound:

```xml
<Response>
  <Connect>
    <Stream url="wss://[region.]api.pipecat.daily.co/ws/twilio">
      <Parameter name="_pipecatCloudServiceHost" value="AGENT_NAME.$PIPECAT_CLOUD_ORGANIZATION"/>
      <Parameter name="direction" value="outbound"/>
    </Stream>
  </Connect>
</Response>
```

- No `<Say>`: the stream starts on answer, a human is already listening, and the
  agent's greeting is the first thing they should hear.
- `direction=outbound` is how the bot knows (data-model §5); nothing else marks
  it.
- The command reads `From` from the connection's `from_number` env value and the
  organization from `PIPECAT_CLOUD_ORGANIZATION`; the operator types only the
  destination (spec FR-006). `From` is the caller identity the recipient sees, and
  it must be a voice-capable number the operator owns in that Twilio account,
  which may be the same number that receives calls (spec FR-006a, research F14).

## 3. The transfer update (composed by the bot)

One REST request updates the live call's TwiML by CallSid (research F5, D7), and
the announcement is the first verb inside it:

```xml
<Response>
  <Say>Connecting you to a colleague now.</Say>
  <Dial answerOnBridge="true" timeout="25">DESTINATION</Dial>
  <Say>Sorry, we could not reach anyone. Let me take over again.</Say>
  <Connect>
    <Stream url="wss://[region.]api.pipecat.daily.co/ws/twilio">
      <Parameter name="_pipecatCloudServiceHost" value="AGENT_NAME.ORG_FROM_ENV"/>
      <Parameter name="from_number" value="CALLER"/>
    </Stream>
  </Connect>
</Response>
```

**Amendment, 2026-08-13 (implementation).** The leading `<Say>` is new, and the
announcement moved from the agent's own voice into the markup. The original
wording ("announce first, spoken to the caller, then one REST request") describes
what the 006 route can do and this one cannot: applying the update replaces the
call's document immediately, which tears the media stream down, so a line the
agent starts speaking is cut off mid-word by its own transfer. There is no event
to wait on either: pipecat 1.5.0's workers expose no bot-stopped-speaking
handler (`pipecat/workers/base_worker.py`, `pipecat/pipeline/worker.py`, read
2026-08-13), so "speak, then update" has no reliable join. The carrier speaking
the line is deterministic: it plays after the update is applied and before the
destination rings, which is what FR-007's "announce before doing so" asks for.
The cost is one sentence in Twilio's voice rather than the agent's, the same
trade the inbound Bin's cold-start line already makes on this route.

- `DESTINATION` is read from the declared destination's environment variable at
  call time; no number literal is ever emitted (spec FR-007).
- `answerOnBridge` keeps the caller hearing ringback rather than dead air while
  the destination rings.
- The verbs after `<Dial>` are the failure path, reached sequentially: a static
  document cannot branch on the dial's outcome, and branching would need a
  hosted callback, which this route exists to not have (research D7).

**Stated limits. This is their canonical specification** (plan, research D7, the
runbook, and the emitted README all reference this section rather than restating
it):

1. The reconnected session is **fresh**. The agent that comes back does not
   remember the call. An operator who needs same-session survival of a failed
   transfer wants the 006 route, and the route comparison says exactly that.
2. The same continuation runs when a completed transfer ends by the destination
   hanging up first: the caller hears the line and meets a fresh agent, and can
   simply hang up. Named, harmless, not fixable without a callback endpoint.

## Contract tests (offline)

Rendered-output tests assert: the three markups share one wss URL and one
service host rendering; the Bin carries the `<Say>` line, both template
substitutions, and the compiled agent name; the outbound TwiML exists exactly
when outbound is declared and carries `direction=outbound`; the transfer TwiML
dials an env-read destination, never a literal, and its failure verbs follow the
`<Dial>`; no secret-looking literal appears in any of the three.

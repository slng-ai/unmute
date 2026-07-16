# 07. Phone calls

So far Acme's agent runs over a browser audio session. To take real phone calls it needs a **telephony** channel, and often a way to hand the caller to a human. This page adds both.

This is also the page where the Pipecat driver's limits matter most, so it is honest about what works today: on Pipecat, **inbound calls and cold human transfer work now; outbound calls, voicemail, and warm transfer do not yet** and fail validation with a clear message.

## A telephony channel

Add a phone channel next to the web one:

```yaml
channels:
  web: { kind: realtime_audio }
  phone:
    kind: telephony
    inbound: true
    outbound: false
    required_controls: [cold_transfer, hangup]
```

**`kind: telephony`** is a phone channel. **`inbound`** and **`outbound`** say which directions it handles; both are required on a telephony channel. **`required_controls`** lists the telephony capabilities the call needs, from a fixed vocabulary: `cold_transfer`, `warm_transfer`, `dtmf_send`, `dtmf_receive`, `hold`, `hangup`, `voicemail_detection`, `ivr_navigation`. These are resolved against the target's carrier and transport, not the provider name alone.

## Transferring to a human

A human handoff is a control, like an agent handoff, but the destination is a person on a phone:

```yaml
controls:
  to_human:
    kind: human_transfer
    destination: billing_line
    mode: cold
```

Give it to the billing agent by adding `to_human` to its `tools:` list, and add the number to the target so the symbolic name resolves:

```yaml
targets:
  pipecat-dev:
    provider: pipecat
    version: "1.5.0"
    transport: daily-sip          # cold transfer on Pipecat needs the Daily SIP transport
    # ... models ...
    destinations:
      billing_line: "+14155550123"
```

Reading the control:

- **`destination`** is a symbolic name, not a number. The real number (or SIP address) lives in each target's `destinations:` map, so the same spec dials your test line in dev and your real line in production.
- **`mode`** is `cold` or `warm`. **Cold** transfers the caller and the agent drops off. **Warm** keeps the agent on to brief the human first (with an optional `briefing`). Cold is the portable choice.

On Pipecat, a cold transfer needs `transport: daily-sip`. Unmute then adds `DAILY_API_KEY` to the required environment, and the generated tool dials the destination over SIP and leaves the call once the human answers.

## Outbound calls and voicemail (not on Pipecat yet)

The schema also describes **outbound** calling. When you set `outbound: true`, you must say what to do if a machine picks up:

```yaml
channels:
  phone:
    kind: telephony
    inbound: false
    outbound: true
    on_voicemail: leave_message    # or: hangup
```

`on_voicemail` is required whenever `outbound: true`, and it is `hangup` or `leave_message`. Outbound also requires that every `source: call_start` variable can actually be supplied.

**On Pipecat today this fails validation.** The driver does not emit outbound or voicemail yet:

```text
error: pipecat-dev: the Pipecat driver does not emit outbound calling yet
error: pipecat-dev: the Pipecat driver does not emit voicemail handling yet
```

This is the fail-loud rule doing its job: the feature is in the schema and Pipecat the platform can do it, but the driver has not shipped the lowering, so Unmute stops rather than pretend. When the driver adds it, the same spec will compile with no change. The same is true for **warm transfer**: Pipecat supports it, the driver does not emit it yet, so `mode: warm` fails today.

## What just got harder

Telephony is the most platform-specific area, so the differences are real:

- **A telephony channel** is `core` in shape, but its options gate per target.
- **Cold human transfer** works on Pipecat (via Daily SIP), LiveKit, Vapi, and ElevenLabs; it is carrier-conditional on Deepgram.
- **Warm transfer** is supported by several platforms but **not emitted by the Pipecat driver yet.**
- **Outbound and voicemail** are proven on four platforms but **not emitted by the Pipecat driver yet**, and generated with a warning on Deepgram.

For a Pipecat deployment today, build around **inbound calls plus cold transfer.** Track the [Pipecat target page](../targets/pipecat.md) for when the rest lands.

Next: [08. Going live](08-going-live.md), on running one spec across targets, capacity, and secrets.

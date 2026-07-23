# Reference: channels and capacity

`channels` says how callers reach the agent. `capacity` declares expected traffic. See the [phone-calls](../learn/07-phone-calls.md) and [going-live](../learn/08-going-live.md) learn pages.

## channels

At least one channel is required.

```yaml
channels:
  web: { kind: realtime_audio }
  phone:
    kind: telephony
    inbound: true
    outbound: false
    required_controls: [cold_transfer, hangup]
```

### kind

The channel type.

Required: yes. Values: `realtime_audio | telephony`. Default: none. Targets: all four, core. `realtime_audio` is a browser or app audio session; `telephony` is a phone line.

### inbound, outbound

Which call directions a telephony channel handles.

Required: yes, on a telephony channel. Values: bool. Default: none.

| Route | What happens | Tag |
|---|---|---|
| LiveKit SIP with Twilio, Telnyx, or Plivo | Inbound and outbound paths are emitted offline; the exact route remains provisional | provisional |
| LiveKit Twilio Connector | No adapter is emitted | gated |
| Pipecat carrier WebSocket with Twilio, Telnyx, or Plivo | Inbound and outbound paths are emitted offline; the exact route remains provisional | provisional |
| Pipecat or LiveKit with Exotel | No route is emitted | gated |

`outbound: true` needs all `source: call_start` variables to be satisfiable.
Voicemail handling is optional (see `on_voicemail` below), so an outbound
Pipecat carrier-WebSocket agent no longer has to set it. Support is resolved
against the exact route; see the
[phone-call route matrix](../learn/07-phone-calls.md#choose-a-supported-carrier-route).

### required_controls

The telephony capabilities the call needs, resolved against the target's carrier and transport, never the brand alone.

Required: no. Values: a list from `cold_transfer, warm_transfer, dtmf_send, dtmf_receive, hold, hangup, voicemail_detection, ivr_navigation`. Default: none. Tag: gated (per capability and target).

### on_voicemail

What to do when a machine answers an outbound call.

Required: no. Optional on any outbound channel, and only valid on a route that
can detect voicemail. Setting it on Pipecat is an error, because the Pipecat
driver does not emit voicemail handling. Values: `hangup | leave_message`.
Default: none (outbound proceeds without voicemail detection).

| Route | What happens | Tag |
|---|---|---|
| LiveKit SIP with Twilio, Telnyx, or Plivo | Both values are emitted through LiveKit answering-machine detection; each route remains provisional | provisional |
| LiveKit Twilio Connector | No adapter is emitted | gated |
| Pipecat carrier WebSocket with Twilio, Telnyx, or Plivo | Voicemail handling is not emitted | gated |
| Pipecat or LiveKit with Exotel | No route is emitted | gated |

## capacity

The declared half of the resource model. Required whenever `channels` has a
telephony channel or the resolved target is a code target, including LiveKit
and Pipecat.

```yaml
capacity:
  peak_sessions: 40
  max_sessions: 60
  peak_starts_per_second: 4
  avg_session_duration: 6m
```

| Field | Required | Type | Notes |
|---|---|---|---|
| `peak_sessions` | yes | int | concurrent conversations at the busy hour; must not exceed `max_sessions` |
| `max_sessions` | yes | int | hard admission limit; reject above it before agent allocation |
| `peak_starts_per_second` | telephony only | number | peak new-call rate; must be greater than zero |
| `avg_session_duration` | yes | duration | sizing and quota input |

Targets: all four, core (as a declaration). Sizing depends on concurrency, placement, and channels, not on how many agents are in the file and never on the provider brand alone. The derived numbers (workers, GPUs, quotas) are not yet printed by the CLI; see [going live](../learn/08-going-live.md).

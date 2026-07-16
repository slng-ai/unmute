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

Required: yes. Values: `realtime_audio | telephony`. Default: none. Targets: all five, core. `realtime_audio` is a browser or app audio session; `telephony` is a phone line.

### inbound, outbound

Which call directions a telephony channel handles.

Required: yes, on a telephony channel. Values: bool. Default: none.

| Target | What happens | Tag |
|---|---|---|
| LiveKit | inbound and outbound work | gated |
| Pipecat | inbound works; `outbound: true` fails (driver does not emit it yet) | gated |
| Vapi | inbound and outbound work | gated |
| ElevenLabs | inbound and outbound work | gated |
| Deepgram | `outbound: true` generated with a warning (carrier-conditional) | gated |

`outbound: true` requires `on_voicemail` and that all `source: call_start` variables are satisfiable. On Pipecat, outbound is a driver maturity gate today.

### required_controls

The telephony capabilities the call needs, resolved against the target's carrier and transport, never the brand alone.

Required: no. Values: a list from `cold_transfer, warm_transfer, dtmf_send, dtmf_receive, hold, hangup, voicemail_detection, ivr_navigation`. Default: none. Tag: gated (per capability and target).

### on_voicemail

What to do when a machine answers an outbound call.

Required: conditional (iff `outbound: true`). Values: `hangup | leave_message`. Default: none.

| Target | What happens | Tag |
|---|---|---|
| LiveKit | both values work (`AMD`) | gated |
| Pipecat | fails (driver does not emit voicemail yet) | gated |
| Vapi | both values work | gated |
| ElevenLabs | both values work | gated |
| Deepgram | generated with a warning (carrier-conditional) | gated |

## capacity

The declared half of the resource model. Required whenever `channels` has a telephony channel or the resolved target is a code target (so it is required on Pipecat).

```yaml
capacity:
  peak_sessions: 40
  max_sessions: 60
  avg_session_duration: 6m
```

| Field | Required | Type | Notes |
|---|---|---|---|
| `peak_sessions` | yes | int | concurrent conversations at the busy hour; must not exceed `max_sessions` |
| `max_sessions` | yes | int | hard admission limit; reject or queue above it |
| `avg_session_duration` | yes | duration | sizing and quota input |

Targets: all five, core (as a declaration). Sizing depends on concurrency, placement, and channels, not on how many agents are in the file and never on the provider brand alone. The derived numbers (workers, GPUs, quotas) are not yet printed by the CLI; see [going live](../learn/08-going-live.md).

# human-transfer

Putting a caller through to a person, both shapes, on LiveKit over a Twilio
SIP trunk. Transfers ride the platform's native primitives, so this example
lives on the one route where LiveKit ships both: `transport: sip`.

- **`send_to_billing`** is a **cold** transfer: `TransferSIPParticipant`, a
  SIP REFER through the trunk. The agent speaks one line, the caller's leg is
  rerouted, the agent is gone. Billing answers knowing nothing about the call.
- **`escalate_to_supervisor`** is a **warm** transfer: LiveKit's
  `WarmTransferTask`. The caller waits on hold music while the task dials the
  supervisor, briefs them with the conversation so far plus your `briefing`
  text, and connects the two when the supervisor agrees. Every way the
  supervisor does not take the call (no answer, decline, voicemail, failed
  dial) comes back as one failure, and `on_unavailable` decides what the
  caller gets.

Cold on Pipecat is its own example: [human-transfer-daily](../human-transfer-daily),
on the Daily route. Pipecat has no native warm transfer, which is why warm is
LiveKit-only; the full capability map, with sources, is in
[docs/TRANSFERS.md](../../docs/TRANSFERS.md).

## The authoring shape

The shape is a block name, never a `mode:` field, and the block carries every
setting of the transfer:

```yaml
send_to_billing:
  kind: human_transfer
  cold:
    destination: billing_line

escalate_to_supervisor:
  kind: human_transfer
  warm:
    destination: supervisor_line
    briefing: Lead with the caller's name and what they are unhappy about.
    ring_timeout: 25s
    on_unavailable: return_to_caller
```

`briefing` is plain text, and it is unwritable on a cold transfer because
there is nobody to brief. You do not ask for a summary: the call transcript is
passed to the supervisor on its own, so use `briefing` for what matters on top
of it.

`on_unavailable` covers every way the person does not take the call. See
[controls](../../docs/user/reference/controls.md#kind-human_transfer).

Destinations are symbolic names resolved in `targets.yaml`, and this example
resolves both to env var names:

```yaml
destinations:
  billing_line: BILLING_PHONE_NUMBER
  supervisor_line: SUPERVISOR_PHONE_NUMBER
```

The env var form lands in the generated `.env.example` and the required-env
list, and is read at call time. The model never sees a phone number and can
never dial an arbitrary one: it picks a symbolic name, the target resolves it.

## Set it up

The trunk side is Twilio Elastic SIP Trunking (by decision; LiveKit Phone
Numbers are inbound-only and cannot transfer). In the Twilio console under
**Elastic SIP Trunking > Manage > Trunks**, create a trunk, then:

1. Enable **Call Transfer (SIP REFER)** and tick **Enable PSTN Transfer**.
   Without it the carrier rejects the cold transfer.
2. Point the trunk's origination URI at your LiveKit SIP endpoint.
3. Copy the trunk's SIP domain, username, and password.

```sh
TWILIO_SIP_ADDRESS=your-trunk.pstn.twilio.com
TWILIO_SIP_USERNAME=...
TWILIO_SIP_PASSWORD=...
TWILIO_PHONE_NUMBER=+1...
LIVEKIT_SIP_OUTBOUND_TRUNK=ST_...
BILLING_PHONE_NUMBER=+1...
SUPERVISOR_PHONE_NUMBER=+1...
```

`LIVEKIT_SIP_OUTBOUND_TRUNK` is what the warm transfer dials out on. The warm
package pins `livekit-agents` to the minor series the prebuilt was verified
against; do not loosen that pin, the task is beta and its surface has moved
before.

## Run it

```sh
bin/unmute validate examples/human-transfer
```

```sh
bin/unmute compile examples/human-transfer
```

That writes `build/livekit/`. Deploy it with `lk agent deploy` from the build
directory, or run the worker yourself.

Testing is not local: SIP signaling and RTP do not fit a tunnel. The warm
transfer needs **no phone number at all**: open the LiveKit Agent Console,
talk to the agent, ask for a manager, and the supervisor's real phone rings.
The cold transfer needs one real inbound call through the trunk. The full
walkthrough, including the failure drills and teardown, is in
[docs/TRANSFERS.md](../../docs/TRANSFERS.md).

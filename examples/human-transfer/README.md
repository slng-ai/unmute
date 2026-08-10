# human-transfer

Putting a caller through to a person, both ways, on one Twilio number.

The salon's front desk can do two things this package cares about:

- **`send_to_billing`** is a **cold** transfer. One call to the carrier, the
  caller's leg is rerouted, the agent drops off. Billing answers knowing nothing
  about the call.
- **`escalate_to_supervisor`** is a **warm** transfer. The caller waits on hold
  music while the agent rings the supervisor, tells them who is calling and why,
  and only then connects the two. If the supervisor cannot take it, the agent
  comes back to the caller.

## The authoring shape

The shape is a block name, never a `mode:` field:

```yaml
send_to_billing:
  kind: human_transfer
  destination: billing_line
  cold: {}

escalate_to_supervisor:
  kind: human_transfer
  destination: supervisor_line
  warm:
    briefing: Lead with the caller's name and what they are unhappy about.
    ring_timeout: 25s
    on_unavailable: return_to_caller
```

`briefing` is plain text, and it is unwritable on a cold transfer because there
is nobody to brief. You do not ask for a summary: the call transcript is passed
to the supervisor on its own, so use `briefing` for what matters on top of it.

`on_unavailable` covers every way the person does not take the call: no answer
within `ring_timeout`, an explicit decline, voicemail, or a failed dial. See
[controls](../../docs/user/reference/controls.md#kind-human_transfer).

## Why one target

Warm transfer needs a route that can hold a private consultation leg while the
caller waits, and LiveKit over a SIP trunk is the only route **unmute emits**
that can today. This is a gap in our Pipecat driver, not in Pipecat: Pipecat
supports warm transfer, and the Pipecat lowering is designed (a second phone
call to the person, briefed on its own media socket while the caller hears hold
music on theirs, then bridged in software) but not generated yet. See
[docs/spec/human-transfer.md](../../docs/spec/human-transfer.md) §T7.

If you add a Pipecat target, `unmute validate` tells you exactly that rather than
quietly dropping the feature:

```
pipecat: telephony warm_transfer: telephony route (pipecat, carrier-websocket, twilio) does not support warm_transfer
```

Drop `escalate_to_supervisor` and a Pipecat carrier-WebSocket target validates
green alongside this one.

## Set it up

The destinations in `targets.yaml` are placeholders. A destination is either a
literal or the name of an env var holding one, and both forms are shown:

```yaml
destinations:
  billing_line: "+34910000001"              # fixed everywhere: keep it here
  supervisor_line: SUPERVISOR_PHONE_NUMBER  # varies per environment: name a var
```

The env var form lands in the generated `.env.example` and the required-env
list, and is read at call time, so staging can dial a test line while production
dials the real desk from the same file.

Either way the model never sees a phone number and can never dial an arbitrary
one. It picks a symbolic name, and the target resolves it.

In the Twilio console, under **Elastic SIP Trunking > Manage > Trunks**, create a
trunk and:

1. Enable **Call Transfer (SIP REFER)** and tick **Enable PSTN Transfer**.
   Without this the cold transfer is rejected by the carrier.
2. Point the trunk's origination URI at your public LiveKit SIP endpoint.
3. Copy the trunk's SIP domain, username, and password.

Then set the environment variables named in `connections/twilio_sip.yaml`, plus
the LiveKit and model-provider keys listed in the generated `.env.example`:

```sh
TWILIO_SIP_ADDRESS=your-trunk.pstn.twilio.com
TWILIO_SIP_USERNAME=...
TWILIO_SIP_PASSWORD=...
TWILIO_PHONE_NUMBER=+34...
LIVEKIT_SIP_OUTBOUND_TRUNK=ST_...
SUPERVISOR_PHONE_NUMBER=+34...
```

`LIVEKIT_SIP_OUTBOUND_TRUNK` is what the warm transfer dials out on. `unmute dev
--telephony` creates the local trunk records and supplies it for you; production
needs a real trunk ID.

## Run it

```sh
bin/unmute validate examples/human-transfer
```

```sh
bin/unmute compile examples/human-transfer
```

The route is still marked provisional: the adapter is emitted and the code is
real, but no credentialed smoke has run against Twilio yet. The compile report
says so per feature, and it is the honest state, not a warning to ignore.

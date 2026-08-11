# human-transfer

Putting a caller through to a person, both ways, on both orchestrators, from one
`agent.yaml`.

Two targets compile from the same controls:

| Target | Route | Credentials | Runs on a laptop |
|---|---|---|---|
| `pipecat` | Twilio Media Streams | the account trio | yes, with `unmute dev --telephony` |
| `livekit` | Twilio Media Streams, bridged into a LiveKit room | the account trio | yes, with `unmute dev --telephony` |

Both run on the same three credentials a Twilio account hands you, and both do
both shapes. Pick either with `--target`.

The third way to run this package is LiveKit over a SIP trunk
(`transport: sip`, `connections/twilio_sip.yaml`, kept in this folder). It does
the same two shapes with LiveKit's own machinery, and it needs a provisioned
Elastic SIP Trunk plus public SIP and RTP, so it is not laptop-testable. See
[when to move to SIP](../../docs/user/learn/twilio-walkthrough.md#when-to-move-to-the-livekit-sip-route).

The salon's front desk can do two things this package cares about:

- **`send_to_billing`** is a **cold** transfer. One REST call to Twilio, the
  caller's leg is rerouted, the agent drops off. Billing answers knowing nothing
  about the call.
- **`escalate_to_supervisor`** is a **warm** transfer. The caller waits on hold
  music while the agent rings the supervisor on a second call, tells them who is
  calling and why, and only then connects the two. If the supervisor cannot take
  it, the agent comes back to the caller.

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

`briefing` is plain text, and it is unwritable on a cold transfer because there
is nobody to brief. You do not ask for a summary: the call transcript is passed
to the supervisor on its own, so use `briefing` for what matters on top of it.

`on_unavailable` covers every way the person does not take the call: no answer
within `ring_timeout`, an explicit decline, voicemail, or a failed dial. See
[controls](../../docs/user/reference/controls.md#kind-human_transfer).

## The same warm transfer, two machines

Warm transfer needs a way to talk to the person privately while the caller
waits. Each orchestrator gets there differently, and comparing them is the point
of shipping both targets.

**Pipecat** builds it out of sockets. The supervisor is a second phone call with
its own media WebSocket, so their audio and the caller's are separate by
construction. The bot holds the caller with hold music on their socket, briefs
the supervisor on theirs, then copies audio between the two. It stays on the
call as a silent bridge, so one warm transfer is two carrier calls and one
session that lasts as long as the conversation.

**LiveKit on the connector** builds it out of rooms, but the carrier moves are
the same as Pipecat's, because it is the same Twilio product. The bridge dials
the person as a second streamed call whose leg joins the caller's room; the
agent points its ears at them with `room_io.set_participant`, briefs them, and
then the bridge copies audio between the two legs. Like Pipecat, the bridge
stays on the call.

**LiveKit on a SIP trunk** is the one that differs. `WarmTransferTask` dials the
supervisor on the outbound trunk, briefs them away from the caller, moves them
into the caller's room, and shuts the agent's session down. The two of them
carry on alone, and the generated code passes `delete_room_on_close=False` so
the room outlives the agent that made it.

The caller cannot tell the difference. Your logs and your capacity planning can.

Route gates are per exact route, so both targets refuse rather than degrade.
Warm on Pipecat is emitted for Twilio only:

```
pipecat: telephony warm_transfer: telephony route (pipecat, carrier-websocket, telnyx) does not support warm_transfer
```

and on LiveKit the two transports get there differently: `sip` uses LiveKit's
SIP machinery, `connector` uses the bridge's own, and both are gated per exact
route.

## Set it up

The destinations in `targets.yaml` are placeholders. A destination is either a
literal or the name of an env var holding one, and both forms are shown:

```yaml
destinations:
  billing_line: BILLING_PHONE_NUMBER       # read from the environment at call time
  supervisor_line: SUPERVISOR_PHONE_NUMBER  # varies per environment: name a var
```

The env var form lands in the generated `.env.example` and the required-env
list, and is read at call time, so staging can dial a test line while production
dials the real desk from the same file.

Either way the model never sees a phone number and can never dial an arbitrary
one. It picks a symbolic name, and the target resolves it.

### For the pipecat target

Everything needed is on the Twilio Console account dashboard plus one
voice-capable number:

```sh
TWILIO_ACCOUNT_SID=AC...
TWILIO_AUTH_TOKEN=...
TWILIO_PHONE_NUMBER=+34...
SUPERVISOR_PHONE_NUMBER=+34...
```

Both transfers use that same number: the cold one redirects the caller's call,
the warm one dials the supervisor from it. No SIP trunk, no public SIP or RTP
ports, nothing else to provision.

The `livekit` target uses the same three values: it is the same Twilio product,
reached through our own Media Streams bridge instead of Pipecat's transport.

### For the SIP variant

Only if you switch the livekit target to `transport: sip` and
`connection: twilio_sip`. A different Twilio product, so different values. In
the console under **Elastic SIP Trunking > Manage > Trunks**, create a trunk,
then:

1. Enable **Call Transfer (SIP REFER)** and tick **Enable PSTN Transfer**.
   Without it the carrier rejects the cold transfer.
2. Point the trunk's origination URI at your public LiveKit SIP endpoint.
3. Copy the trunk's SIP domain, username, and password.

```sh
TWILIO_SIP_ADDRESS=your-trunk.pstn.twilio.com
TWILIO_SIP_USERNAME=...
TWILIO_SIP_PASSWORD=...
TWILIO_PHONE_NUMBER=+34...
LIVEKIT_SIP_OUTBOUND_TRUNK=ST_...
SUPERVISOR_PHONE_NUMBER=+34...
```

`LIVEKIT_SIP_OUTBOUND_TRUNK` is what the warm transfer dials out on. Inbound
calls also need LiveKit SIP deployed with public SIP signalling and RTP, which
is why that variant does not run on a laptop even though it compiles on one.

## Run it

```sh
bin/unmute validate examples/human-transfer
```

```sh
bin/unmute compile examples/human-transfer
```

That writes `build/pipecat/` and `build/livekit/`, both from the same controls.

```sh
bin/unmute dev examples/human-transfer --target pipecat --telephony
```

```sh
bin/unmute dev examples/human-transfer --target livekit --telephony
```

`unmute dev --telephony` opens the tunnel and points the number's voice webhook
at it, so a real call reaches the agent on your laptop. Add the model-provider
keys from the generated `.env.example` first.

Both routes are marked provisional: the code is emitted, linted and
type-checked, and neither has been through a credentialed smoke with two real
phones. The
compile report says so per feature, and it is the honest state, not a warning to
ignore. When you do try warm transfer against real numbers, listen on the
caller's leg during the briefing: hearing hold music and nothing else is the one
thing this design exists to guarantee.

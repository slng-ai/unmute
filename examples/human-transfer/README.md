# human-transfer

Putting a caller through to a person, both ways, on one Twilio number and the
three credentials a Twilio account gives you.

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

## Why this route

Both shapes ride Twilio Media Streams, the same route as
[telephony-hello](../telephony-hello). Warm transfer needs a way to talk to the
person privately while the caller waits, and on this route that falls out of the
topology: the supervisor is a second phone call with its own media WebSocket, so
their audio and the caller's are separate by construction. The bot holds the
caller with hold music on their socket, briefs the supervisor on theirs, and
then copies audio between the two until someone hangs up.

That last part is worth knowing: on this route the bot stays on the call as a
silent bridge, so one warm transfer is two carrier calls but still one session.
On LiveKit the agent hands over and shuts down instead. The caller cannot tell
the difference; your logs and your phone bill can.

Warm transfer is emitted for Twilio only. Point this package at Telnyx or Plivo
and `unmute validate` says so rather than quietly dropping the feature:

```
pipecat: telephony warm_transfer: telephony route (pipecat, carrier-websocket, telnyx) does not support warm_transfer
```

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

## Run it

```sh
bin/unmute validate examples/human-transfer
```

```sh
bin/unmute compile examples/human-transfer
```

`unmute dev --telephony` opens the tunnel and points the number's voice webhook
at it, so a real call reaches the agent on your laptop. Add the model-provider
keys from the generated `.env.example` first.

The route is marked provisional: the code is emitted, linted and type-checked,
and it has not yet been through a credentialed smoke with two real phones. The
compile report says so per feature, and it is the honest state, not a warning to
ignore. When you do try warm transfer against real numbers, listen on the
caller's leg during the briefing: hearing hold music and nothing else is the one
thing this design exists to guarantee.

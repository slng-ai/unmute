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
  caller gets. Since 2026-08-12 the prompt the supervisor hears is Unmute's
  rather than the prebuilt's, because the prebuilt's own never briefs
  unprompted (SCHEMA N35).

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

1. Enable **Call Transfers** and tick **Enable PSTN Transfer**
   ([Twilio: Call Transfer via SIP REFER](https://www.twilio.com/docs/sip-trunking/call-transfer)).
   Without it the carrier rejects the cold transfer.
2. Point the trunk's origination URI at your LiveKit SIP endpoint.
3. Copy the trunk's SIP domain, username, and password.

```sh
SIP_TRUNK_HOSTNAME=your-trunk.pstn.twilio.com
SIP_AUTH_USERNAME=...
SIP_AUTH_PASSWORD=...
SIP_FROM_NUMBER=+1...
BILLING_PHONE_NUMBER=+1...
SUPERVISOR_PHONE_NUMBER=+1...
```

Those four `SIP_*` values are all the warm transfer needs. It dials the
supervisor by passing them inline with the call, so **no LiveKit outbound trunk
is registered** and `lk sip outbound create` is not part of this example.

### If you set this example up before 2026-08-12

Four variables were renamed and one was retired. The rename is because these are
standard SIP trunk settings rather than one carrier's, and the same emitted code
now dials through any SIP carrier with them (SCHEMA N33):

```
TWILIO_SIP_ADDRESS          ->  SIP_TRUNK_HOSTNAME
TWILIO_SIP_USERNAME         ->  SIP_AUTH_USERNAME
TWILIO_SIP_PASSWORD         ->  SIP_AUTH_PASSWORD
TWILIO_PHONE_NUMBER         ->  SIP_FROM_NUMBER

LIVEKIT_SIP_OUTBOUND_TRUNK  ->  delete it, nothing reads it
LIVEKIT_SIP_INBOUND_TRUNK   ->  delete it too, nothing reads it either
```

Both trunk-ID variables are retired. The outbound one went with inline dialling
(SCHEMA N33); the inbound one went when `telephony-setup.sh` started resolving
the trunk by phone number (SCHEMA N36), so setting it changes nothing. The stored
outbound trunk itself can be deleted with `lk sip outbound delete` whenever
convenient; leaving it costs nothing but a stale record. Keep the inbound trunk
and its dispatch rule: incoming calls still need both, and the script reuses them. If you would rather not rename anything,
edit `connections/twilio_sip.yaml` back to your own names: the compiler carries
whatever a Connection declares through verbatim.

The warm package pins `livekit-agents` to the minor series the prebuilt was
verified against; do not loosen that pin, the task is beta and its surface has
moved before.

## Run it

```sh
bin/unmute validate examples/human-transfer
```

```sh
bin/unmute compile examples/human-transfer
```

That writes `build/livekit/`. Its own `README.md` has a Deploy section printing
the exact commands for this package, including its region: on LiveKit Cloud the
first deploy and every later one are different commands, and the build directory
ships no `livekit.toml` because the platform writes that itself. Self-hosting the
worker is documented there too.

Testing is not local: SIP signaling and RTP do not fit a tunnel. The warm
transfer needs **no phone number at all**: open the LiveKit Agent Console,
talk to the agent, ask for a manager, and the supervisor's real phone rings.

**The cold transfer cannot be tested that way.** It refers the caller's *existing*
SIP leg out, and an Agent Console session has no SIP leg, so there is nothing to
act on: the tool fires, logs `cold transfer skipped: no phone caller in the room`,
and the agent carries on. Cold needs one real inbound call through the trunk, which
means the inbound trunk and the dispatch rule from the generated README must exist
and the rule must name **this** package's agent. The full
walkthrough, including the failure drills and teardown, is in
[docs/TRANSFERS.md](../../docs/TRANSFERS.md).

## What a working transfer looks and sounds like

Give the agent a name and a complaint before you ask for a manager, so the
briefing has something to say. Then check both halves.

**On the supervisor's phone**, the first sentence names the caller and what they
are unhappy about and ends with a question. Something like "Nicola called about a
colour correction Maya did on Tuesday, she is unhappy with the tone and I have
already offered a redo. Can you take the call?" No hello, no "how can I help",
and no waiting for you to ask what it is about. Say you can take it and the
caller is put through.

**In `lk agent logs`**, three lines per transfer. Warm, when it works:

```text
human transfer fired: escalate_to_supervisor (warm)
warm transfer dialling out: handing over 12 conversation messages
warm transfer merged after 34s: sip_abc123
```

The last line is `warm transfer unavailable after <n>s: <reason>` instead for
every way the transfer did not happen. Cold:

```text
human transfer fired: send_to_billing (cold)
cold transfer referring the caller out
cold transfer completed after 2s
```

The message count is the one number worth reading. **Zero or one** means the
briefing had nothing to work with, which is a different problem from a briefing
that was ignored, and they have different fixes.

One limit worth knowing before you test: `ring_timeout: 25s` bounds **ringing
only**. Once the supervisor picks up, nothing bounds the consultation, and the
caller is on hold for all of it. The agent is told to decline on the supervisor's
behalf when they go quiet or never decide, which is a mitigation rather than a
guarantee. [docs/TRANSFERS.md](../../docs/TRANSFERS.md) explains why there is no
bound and what the alternatives cost.

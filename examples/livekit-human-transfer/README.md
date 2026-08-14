# livekit-human-transfer

Putting a caller through to a person, both shapes, on LiveKit over a Twilio
SIP trunk. Transfers ride the platform's native primitives, so this example
lives on the one route where LiveKit ships both: `transport: sip`.

| | |
|---|---|
| provider | LiveKit, deployed on LiveKit Cloud (self-hosting the worker is documented too) |
| route | `transport: sip`, `carrier: twilio`: a Twilio Elastic SIP Trunk you own |
| what you host | nothing on LiveKit Cloud; the worker if you self-host |
| what it does | inbound calls, cold transfer, warm transfer |

**The name says the route because the route is the thing that differs.** The
Pipecat examples reach a person over Twilio Media Streams and TwiML markup, which
is a different mechanism with different capabilities. Nothing on this page applies
to them, and nothing on their pages applies here.

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

Cold on Pipecat has two examples of its own, and neither hosts anything:
[pipecat-human-transfer-daily](../pipecat-human-transfer-daily) on `transport: daily-sip` with a
number from Daily, and
[pipecat-human-transfer-twilio](../pipecat-human-transfer-twilio) on
`transport: cloud-websocket` with a Twilio number you already own.

**Warm is LiveKit-only, and the reason differs per Pipecat route** rather than
being one blanket fact: on the Daily routes the platform documents a warm pattern
and this project has not built it (feature 005); on `cloud-websocket` it would need
a callback endpoint you host, which is the one cost that route exists to remove;
on the carrier-websocket transports there is no transfer control at all. Each
refusal says which it means. The full capability map, with sources, is in
[docs/TRANSFERS.md](../../docs/TRANSFERS.md), and the route comparison is in
[docs/TELEPHONY.md](../../docs/TELEPHONY.md).

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
Numbers are inbound-only and cannot transfer). Compile the package first, because
the generated `build/livekit/README.md` dictates the carrier steps with your own
variable names, the console paths, and a runnable command block for each one. Its
`## Telephony setup` section is the authority; this page does not copy it.

In short, on one Elastic SIP trunk:

1. **Termination** gives you the three dial-out values: the trunk's own domain,
   ending in `pstn.twilio.com`, plus a Credential List username and password.
   The domain is one value, not two.
2. **Origination** points at your LiveKit project SIP URI with `;transport=tcp`.
   That URI comes from the **project ID**, not from `LIVEKIT_URL`: the generated
   README prints yours with one `lk` command.
3. **Your number is attached to the trunk.** This is the step people miss. A
   number that is not on the trunk never reaches LiveKit, however right the
   origination URI is.
4. **Call Transfer (SIP REFER) enabled and PSTN Transfer ticked**
   ([Twilio: Call Transfer via SIP REFER](https://www.twilio.com/docs/sip-trunking/call-transfer)).
   Without both, the carrier rejects the cold transfer and nothing else here can
   fix it.

```sh
OPENAI_API_KEY=sk-...                # the reasoning model
SLNG_API_KEY=...                     # listen and speak
SIP_TRUNK_HOSTNAME=your-trunk.pstn.twilio.com
SIP_AUTH_USERNAME=...
SIP_AUTH_PASSWORD=...
SIP_FROM_NUMBER=+1...
BILLING_PHONE_NUMBER=+1...
SUPERVISOR_PHONE_NUMBER=+1...
```

Those eight are the whole `secrets:` block in `agent.yaml`, which is the list of
everything you supply. The generated `.env.example` ends with four more under a
"supplied for you, not by you" heading — `LIVEKIT_URL`, `LIVEKIT_API_KEY`,
`LIVEKIT_API_SECRET`, and `REDIS_URL`. Set those only for a local run or a
self-hosted deployment; on LiveKit Cloud the platform injects the LIVEKIT_*
trio and its managed SIP service owns Redis.

Every name in `.env` must be a valid shell identifier: letters, digits and
underscores, never starting with a digit. LiveKit Cloud exports your secrets with
a shell, so a name like `11LABS_API_KEY` fails at export, that value is missing at
runtime, and the only trace is one `/etc/run/env: ... not a valid identifier` line
at the top of `lk agent logs`. Fix the name and re-upload with `--overwrite`;
merging leaves the bad name in place.

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

Validate and compile from the repository root:

```sh
bin/unmute validate examples/livekit-human-transfer
bin/unmute compile examples/livekit-human-transfer
```

That writes `build/livekit/`. Its own `README.md` has a Deploy section printing
the exact commands for this package, including its region: on LiveKit Cloud the
first deploy and every later one are different commands, and the build directory
ships no `livekit.toml` because the platform writes that itself. Self-hosting the
worker is documented there too.

To hear the agent before any carrier account exists, with no phone in the picture:

```sh
bin/unmute dev examples/livekit-human-transfer              # browser, needs Docker
bin/unmute dev --console examples/livekit-human-transfer    # terminal, local mic
```

Neither transfer completes in that kind of session, and the two reasons are
different. **Cold** refers the caller's existing SIP leg out, so with no SIP leg
there is nothing to act on: the tool logs `cold transfer skipped: no phone caller
in the room` and the agent carries on. **Warm** dials the supervisor through
LiveKit's SIP service, so it is exercised against the deployed project, which is
the Agent Console path below.

## Deploy to LiveKit Cloud, and prove the cold transfer

This is the sequence that passed on 2026-08-12, run from `build/livekit` with
`.env` filled in. Steps 1 and 2 are one-time; 3 to 6 are what you repeat.

**1. Attach your number to the trunk.** The step people miss, and the one that
makes an inbound call reach LiveKit at all.

```sh
NUMBER=$(sed -n 's/^SIP_FROM_NUMBER=//p' .env | head -1 | tr -d "\r\"'")
NUMBER_SID=$(twilio phone-numbers:list -o json |
  jq -r --arg n "$NUMBER" '.[] | select(.phoneNumber==$n) | .sid')
twilio api:trunking:v1:trunks:phone-numbers:create \
  --trunk-sid <your-trunk-sid> --phone-number-sid "$NUMBER_SID"

# expect your trunk SID, not "none"
twilio phone-numbers:list -o json |
  jq -r --arg n "$NUMBER" '.[] | select(.phoneNumber==$n) | "trunk: \(.trunkSid // "none")"'
```

Find the trunk SID with
`twilio api:trunking:v1:trunks:list -o json | jq -r '.[] | "\(.friendlyName)  \(.domainName)  \(.sid)"'`.

**2. Check the trunk's transfer settings**, because cold transfer has no other
setting anywhere:

```sh
twilio api:trunking:v1:trunks:list -o json | jq -r --arg s "<your-trunk-sid>" \
  '.[] | select(.sid==$s) | "transfers: \(.transferMode), caller id: \(.transferCallerId)"'
```

Expect `transfers: enable-all`. `disable-all` or `sip-only` is what makes a cold
transfer to a phone number fail, and no LiveKit-side change can compensate.

**3. Fix any secret name that is not a shell identifier**, before uploading:

```sh
sed -i '' '/^11LABS_API_KEY=/d' .env    # or rename it to ELEVENLABS_API_KEY
```

**4. Create the LiveKit records.** Run it twice; the second run must report
everything reused rather than created.

```sh
bash telephony-setup.sh
bash telephony-setup.sh
lk sip inbound list     # a trunk claiming your number
lk sip dispatch list    # a rule scoped to it, dispatching this package's agent
```

**5. Ship this build and its secrets.**

```sh
lk agent deploy
lk agent update-secrets --secrets-file .env --overwrite
lk agent status
```

**6. Call your number.** Give the agent a name and a complaint, then ask for
billing. You should be connected to `BILLING_PHONE_NUMBER` and the agent should
leave the call. Then read the logs:

```sh
lk agent logs
```

Expect the three cold lines below. If the call rings and drops with no agent, work
backwards through steps 1, 4 and 5: number on the trunk, records present, agent up.

Testing is not local: SIP signaling and RTP do not fit a tunnel. The warm
transfer needs **no phone number at all**: open the LiveKit Agent Console,
talk to the agent, ask for a manager, and the supervisor's real phone rings.

**The cold transfer cannot be tested that way.** It refers the caller's *existing*
SIP leg out, and an Agent Console session has no SIP leg, so there is nothing to
act on: the tool fires, logs `cold transfer skipped: no phone caller in the room`,
and the agent carries on. Cold needs one real inbound call through the trunk, which
means the inbound trunk and the dispatch rule must exist and the rule must name
**this** package's agent. That is what the six steps above set up, and it passed
end to end on 2026-08-12: a call to `SIP_FROM_NUMBER` was answered by the deployed
agent, the caller asked for billing, and the three cold log lines came back clean.
The full walkthrough, including the failure drills and teardown, is in
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

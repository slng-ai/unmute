# Human transfers

Putting a caller through to a person, in the two shapes the telephony world
has: **cold** (the call is rerouted, the agent is gone) and **warm** (the
caller holds while the agent briefs the person, then the two are connected).

One rule decides where transfers exist: **a transfer compiles only on a route
where the platform ships the primitive.** Unmute's generated code makes one
documented platform call and never owns the audio path. On any other route,
`unmute validate` refuses and names the routes that work.

This page answers four questions: what is available, how to write the yaml,
which secrets you need, and how to test it. It is the reference the examples
and the telephony docs point at.

## 1. What is available

Verified against the platforms' live documentation on 2026-08-11. "Native"
means the platform ships and maintains the primitive.

| driver | route | cold | warm |
|---|---|---|---|
| livekit | `sip` (trunk) | **yes**: `TransferSIPParticipant`, a SIP REFER through the trunk. The caller leaves the room and the session ends. On failure the caller stays with the agent, so `on_unavailable` applies. | **yes**: `WarmTransferTask`, LiveKit's prebuilt. Hold music, the consult call, the briefing (transcript plus your `briefing` text), and the merge are all the task's. Every failure (no answer, decline, voicemail, failed dial) comes back as one error and `on_unavailable` applies. |
| livekit | `connector` (Twilio websocket) | no | no |
| pipecat | Daily (`transport: daily-sip`) | **yes**: `transport.sip_call_transfer`. The bot announces, Daily reroutes the leg, the bot drops off. Needs dial-out enabled on the Daily domain. | **not emitted yet.** The platform supports it; this project has not built it. Feature 004. |
| pipecat | carrier websockets (twilio, telnyx, plivo, exotel) | no: the platform has no transfer control on these transports | no: same reason |

Sources: [LiveKit call forwarding](https://docs.livekit.io/telephony/features/transfers/cold.md),
[LiveKit agent-assisted transfer](https://docs.livekit.io/telephony/features/transfers/warm.md),
[WarmTransferTask](https://docs.livekit.io/agents/prebuilt/tasks/warm-transfer.md),
[Pipecat Daily PSTN](https://docs.pipecat.ai/pipecat/telephony/daily-pstn),
[Pipecat telephony overview](https://docs.pipecat.ai/pipecat/telephony/overview)
(the websocket routes have "no advanced call center features like transfers").

**Every cell above says which of two things it means**, because they are not
the same and mixing them up is how a document ends up lying. "no" means the
platform does not ship the primitive. "not emitted yet" means it does and we
have not built it. Corrected 2026-08-12: the Pipecat warm cell used to read
"no", which was wrong.

Why the websocket "no" rows are firm:

- The websocket routes (both drivers) carry media only, and Pipecat's own
  telephony overview says those transports have no call-transfer control.
  Everything this project once built on them (REST redirects, Twilio
  conferences, in-process audio bridges) meant owning the call's audio path,
  and every live test found a new lifecycle bug there. That work is deleted,
  on purpose, and the rule stands there.

Why Pipecat warm on Daily says "not emitted yet" instead:

- Daily documents two transfer patterns, cold and warm
  ([Daily PSTN](https://docs.pipecat.ai/pipecat/telephony/daily-pstn),
  verified 2026-08-12). So the platform is not the limit.
- The warm pattern does put the generated bot in charge of audio: a transfer
  coordinator, a hold-music mixer, and a gate per leg. That is the same class
  of complexity the deleted work ran into, which is a real reason to build it
  deliberately rather than by default. It is **not** a reason to write it down
  as a platform limitation.
- One thing makes Daily safer than the deleted designs: Daily's room is
  already the bridge and the bot already owns it, so the gates sit inside a
  pipeline we control instead of stitching two carrier sockets together.
- Building it is **feature 005**. Nothing in `agent.yaml` needs to change for
  it: the `warm:` block, its `destination`, `briefing`, `ring_timeout`, and
  `on_unavailable` already exist and are already what LiveKit uses.

Two platform facts worth knowing before you provision anything:

- **LiveKit Phone Numbers cannot transfer.** They are inbound-only and their
  docs state `TransferSipParticipant` is not yet supported on them. Unmute's
  documented telephony provider for the LiveKit route is Twilio Elastic SIP
  Trunking, by decision.
- **The trunk must allow REFER** for LiveKit cold. On Twilio it is a
  per-trunk setting
  ([Call Transfer via SIP REFER](https://www.twilio.com/docs/sip-trunking/call-transfer)):
  in the console under **Elastic SIP Trunking > Manage > Trunks > your
  trunk**, enable **Call Transfers** and tick **Enable PSTN Transfer**, or do
  it with the CLI:

  ```sh
  twilio api trunking v1 trunks update --sid <trunk-sid> \
    --transfer-mode enable-all --transfer-caller-id from-transferee
  ```

  Caller ID for the transfer target is that trunk setting (`Transferee` shows
  the caller's number, `Transferor` the trunk's), never per-call. Two Twilio
  restrictions to know: transfers to emergency services (911/933) are not
  supported, and the referred-to leg keeps billing per-minute trunking
  charges. Plivo has REFER on by default (destination form
  `sip:+E164@<trunk-id>.zt.plivo.com`); Telnyx is supported
  (`sip:+E164@sip.telnyx.com`).

Open-source note: the LiveKit SIP server is self-hostable, so the `sip` route
and both transfers work without LiveKit Cloud. Daily has no self-hosted
option. The only fully open-source Pipecat telephony routes are the carrier
websockets, and those cannot transfer.

## 2. How to write the yaml

The shape is a block name, never a `mode:` field, and the block carries every
parameter of the transfer (SCHEMA.md §4.7):

```yaml
controls:
  send_to_billing:
    kind: human_transfer
    when: The caller asks about an invoice, a refund, or a charge they do not recognise.
    cold:
      destination: billing_line

  escalate_to_supervisor:
    kind: human_transfer
    when: The caller is unhappy and asks for a manager.
    warm:
      destination: supervisor_line
      briefing: |
        Lead with the caller's name and what they are unhappy about.
        Ask whether they can take the call now.
      ring_timeout: 25s
      on_unavailable: return_to_caller
```

- `destination` (required in both blocks) is a symbolic name, resolved
  through the target's `destinations:` map. Use env var names in anything
  you commit; a literal number in a repository is a number nobody answers.
- `briefing` is warm-only free text. The transcript is passed to the person
  on its own; the briefing is what matters on top of it.
- `on_unavailable` is `return_to_caller` (default) or `hangup`, and covers
  every way the person does not take the call, including a failed dial.
- A package with a warm transfer must declare `channels.phone.outbound: true`
  (warm dials out, whatever the channel says).

And the target side, one per driver:

```yaml
# LiveKit: both shapes, on a SIP trunk.
targets:
  livekit:
    provider: livekit
    version: "1.6.4"
    sdk_language: python
    transport: sip
    carrier: twilio
    connection: twilio_sip
    destinations:
      billing_line: BILLING_PHONE_NUMBER
      supervisor_line: SUPERVISOR_PHONE_NUMBER
```

```yaml
# Pipecat: cold only, on Daily.
targets:
  pipecat:
    provider: pipecat
    version: "1.5.0"
    transport: daily-sip
    destinations:
      billing_line: BILLING_PHONE_NUMBER
```

The complete packages live in
[examples/human-transfer](../examples/human-transfer) (LiveKit, both shapes)
and [examples/human-transfer-daily](../examples/human-transfer-daily)
(Pipecat, cold).

## 3. Which secrets you need

Exact env names, as the generated `.env.example` lists them. Values are never
written into a package; a Connection stores env var names only.

**LiveKit rig** (`examples/human-transfer`):

| Name | What it is |
|---|---|
| `LIVEKIT_URL` | The LiveKit project URL (cloud or self-hosted). |
| `LIVEKIT_API_KEY` / `LIVEKIT_API_SECRET` | The project API key pair. |
| `LIVEKIT_SIP_INBOUND_TRUNK` | Inbound trunk ID; the cold test's call arrives through it. |
| `SIP_TRUNK_HOSTNAME` / `SIP_AUTH_USERNAME` / `SIP_AUTH_PASSWORD` | The Elastic SIP trunk's host and credential list entry. The agent dials out with these directly. |
| `SIP_FROM_NUMBER` | The number on the trunk, and the number transfers are placed from. |
| `OPENAI_API_KEY` / `SLNG_API_KEY` | The package's model providers. |
| `REDIS_URL` | The coordination store for the telephony runtime. |
| `BILLING_PHONE_NUMBER` / `SUPERVISOR_PHONE_NUMBER` | The transfer destinations, read at call time. |

On LiveKit Cloud the first three are supplied by the platform: `lk` drops them
from a secrets file and the deployed agent gets its own. Set them for local runs
and for a self-hosted server; do not try to send them as secrets.

**Pipecat rig** (`examples/human-transfer-daily`):

| Name | What it is |
|---|---|
| `DAILY_API_KEY` | The Daily domain's API key. |
| `OPENAI_API_KEY` / `SLNG_API_KEY` | The package's model providers. |
| `BILLING_PHONE_NUMBER` | The transfer destination, read at call time. |

## 4. How to test it

Nothing here runs on a laptop tunnel: SIP signaling and RTP do not fit one.
The rigs are the platforms' own clouds, and both start from the same
generated package. Real numbers and per-minute charges are involved, and you
need two phones to answer as "billing" and "supervisor".

### LiveKit rig (cold and warm)

1. **Accounts and CLI**: a LiveKit Cloud project (or self-hosted LiveKit with
   its SIP server) and a Twilio account. LiveKit Phone Numbers are not an
   option here (inbound-only, no transfer support). Authenticate the CLI with
   `lk cloud auth` and set the project as the default with
   `lk project set-default "<name>"`; without a default project every deploy
   command fails with a subdomain mismatch that does not say which side is
   wrong.
2. **Trunking, one-time**: create a Twilio Elastic SIP trunk; enable "Call
   Transfer (SIP REFER)" and "Enable PSTN Transfer"; attach a number; point
   its origination URI at your LiveKit SIP endpoint. In LiveKit, create the
   inbound trunk plus a dispatch rule for the agent. **No outbound trunk.**
   Since 2026-08-12 (SCHEMA N33) the generated agent dials out with the
   carrier's own trunk settings passed inline, from the four `SIP_*` names in
   section 3, so `lk sip outbound create` and `LIVEKIT_SIP_OUTBOUND_TRUNK` are
   no longer part of this rig. If you set one up for an earlier build, drop the
   variable and delete the trunk when convenient; nothing reads it. Inbound
   cannot work the same way, because an unsolicited call arrives with no
   request of ours for configuration to travel with, so the platform has to
   already know which project owns the number and which room the caller joins.
   Verified against [Inline trunk configuration](https://docs.livekit.io/telephony/making-calls/outbound-calls/#inline-trunk)
   and [WarmTransferTask](https://docs.livekit.io/agents/prebuilt/tasks/warm-transfer/)
   on 2026-08-12.
3. **Deploy**: `unmute compile examples/human-transfer`, then follow the Deploy
   section of the generated `build/livekit/README.md`. It prints the exact
   commands for this package, including its region: the first deploy
   (`lk agent create`, which also writes the `livekit.toml` the build directory
   does not ship) is a different command from every later one
   (`lk agent deploy`), and the section-3 env goes in as `--secrets-file .env`
   on the first deploy. That README is the authority for the commands; this
   page deliberately does not copy them.
4. **Warm test, no phone number needed**: open the Agent Console, talk to
   the agent, ask for a manager. Expect: one spoken line, hold music, the
   supervisor's phone rings, the supervisor is briefed as a colleague and
   says "connect me", and the two of you are joined.
5. **Cold test**: call the Twilio number, ask about an invoice. Expect: one
   spoken line, then the billing phone rings and the agent is gone from the
   call.
6. **Failure drills**: let the supervisor leg ring out (expect
   `ring_timeout`, then the agent back with the caller, or a goodbye when
   `on_unavailable: hangup`); answer and decline (same policy applies).

### Pipecat rig (cold)

1. **Accounts and CLI**: Pipecat Cloud (`pipecat cloud auth login`) and a Daily domain
   with a purchased phone number and **dial-out enabled** (required by
   `sip_call_transfer`; ask Daily support if the dashboard does not offer
   it).
2. **Deploy**: `unmute compile examples/human-transfer-daily`, then follow the
   Deploy section of the generated `build/pipecat/README.md`. The order matters:
   create the secret set from `.env` with `pipecat cloud secrets set` **first**,
   because the emitted `pcc-deploy.toml` already names it, then
   `pipecat cloud deploy`, which builds the image in the cloud from the emitted
   `Dockerfile`. Check `pipecat cloud agent status` reports `ready`. That README
   prints the exact commands, this page does not copy them.
3. **Dial-in**: connect the Daily number to the deployed agent with Pipecat
   Cloud's managed dial-in webhook (dashboard or REST API). No webhook
   server of yours is involved.
4. **Cold test**: call the Daily number, ask about an invoice. Expect: the
   announcement, then the billing phone rings and the bot exits.
5. **Failure drill**: point `BILLING_PHONE_NUMBER` at an undialable value
   and call again. Expect the agent to say the transfer did not work and
   keep helping (or hang up after a goodbye, per `on_unavailable`).
6. **Double-request drill**: call again and ask to be put through twice in quick
   succession. Expect **exactly one** transfer attempt. The bot keeps the first
   attempt's answer and replays it, so a second ask cannot fire a second REFER,
   and a transfer that failed cannot come back to the model as a success. Added
   2026-08-12; before that there was no guard here at all.

### Teardown

A test rig must not become a standing bill: release the Twilio number and
trunk if they were test-only, `pipecat cloud agent delete` the deployed bot,
delete the secret set if it was made for the test, and delete unused LiveKit
trunks and dispatch rules.

**The Daily number is the exception.** Daily does not allow releasing a number
until **14 days after purchase**, and releasing is permanent once it is allowed
(verified against
[Daily phone numbers](https://docs.pipecat.ai/pipecat/telephony/daily-phone-numbers),
2026-08-12). So it cannot be torn down with the rest of the rig: you own it, and
pay for it, for at least a fortnight. Note the purchase date, then release it
later with `scripts/daily-phone-number.sh release <id>`. Plan a Daily test rig
knowing this, rather than discovering it on teardown day.

### Status

A row is **verified** only once its recipe has been run as written, against real
accounts, with the result dated below. Anything else is **provisional**, however
much offline testing it has. When a run finds a wrong step, the fix lands in this
document before it lands in code.

| row | state | evidence |
|---|---|---|
| LiveKit SIP cold | provisional | no credentialed run recorded |
| LiveKit SIP warm | provisional | no credentialed run recorded |
| Pipecat Daily cold | provisional | no credentialed run recorded. Proven offline against real `pipecat-ai` 1.5.0 on 2026-08-12: the emitted transport accepts a real dial-in payload, the project passes `ruff` and `ty`, and the transfer attempts at most once. None of that is a phone call. |

Nothing here is verified yet. Saying so plainly is the point: three rows sat
under one sentence promising a run that had not happened, which reads as a record
of success to anyone skimming.

**What the Pipecat Daily row is waiting on**, specifically, is steps 1 through 6
of the rig above, which need a Pipecat Cloud account, a Daily domain with dial-out
granted, two answerable phones, and real per-minute money. Until someone does
that, the honest claim is that inbound answering and cold transfer are *built and
offline-proven*, not that they work.

One defect this row already found without a phone call: every inbound Daily call
failed while the transport was being built, because the generated bot handed the
runner a parameter object that rejects a call's own details. Fixed 2026-08-12.
That is why the row's offline evidence is worth recording even though it is not a
run.

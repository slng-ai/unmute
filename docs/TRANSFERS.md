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
| pipecat | Daily (`transport: daily-sip`) | **yes**: `transport.sip_call_transfer`. The bot announces, Daily reroutes the leg, the bot drops off. Needs dial-out enabled on the Daily domain. | no |
| pipecat | carrier websockets (twilio, telnyx, plivo, exotel) | no | no |

Sources: [LiveKit call forwarding](https://docs.livekit.io/telephony/features/transfers/cold.md),
[LiveKit agent-assisted transfer](https://docs.livekit.io/telephony/features/transfers/warm.md),
[WarmTransferTask](https://docs.livekit.io/agents/prebuilt/tasks/warm-transfer.md),
[Pipecat Daily PSTN](https://docs.pipecat.ai/pipecat/telephony/daily-pstn),
[Pipecat telephony overview](https://docs.pipecat.ai/pipecat/telephony/overview)
(the websocket routes have "no advanced call center features like transfers").

Why the two "no" rows are firm:

- The websocket routes (both drivers) carry media only. Everything this
  project once built on them (REST redirects, Twilio conferences, in-process
  audio bridges) meant owning the call's audio path, and every live test
  found a new lifecycle bug there. That work is deleted, on purpose.
- Pipecat has no warm primitive on any route. Its documented warm pattern
  makes the bot the audio coordinator (dial-out into the same room, hold
  mixer, audio gating), which is the same class of complexity. If a Pipecat
  warm transfer is ever needed, it is its own decision, not a default.

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
| `LIVEKIT_SIP_OUTBOUND_TRUNK` | Outbound trunk ID; the warm transfer dials the supervisor on it. |
| `TWILIO_SIP_ADDRESS` / `TWILIO_SIP_USERNAME` / `TWILIO_SIP_PASSWORD` | The Elastic SIP trunk's domain and credential list entry. |
| `TWILIO_PHONE_NUMBER` | The number on the trunk. |
| `OPENAI_API_KEY` / `SLNG_API_KEY` | The package's model providers. |
| `REDIS_URL` | The coordination store for the telephony runtime. |
| `BILLING_PHONE_NUMBER` / `SUPERVISOR_PHONE_NUMBER` | The transfer destinations, read at call time. |

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

1. **Accounts**: a LiveKit Cloud project (or self-hosted LiveKit with its
   SIP server) and a Twilio account. LiveKit Phone Numbers are not an
   option here (inbound-only, no transfer support).
2. **Trunking, one-time**: create a Twilio Elastic SIP trunk; enable "Call
   Transfer (SIP REFER)" and "Enable PSTN Transfer"; attach a number; point
   its origination URI at your LiveKit SIP endpoint. In LiveKit, create the
   inbound trunk plus a dispatch rule for the agent, and an outbound trunk;
   export the outbound trunk ID as `LIVEKIT_SIP_OUTBOUND_TRUNK`.
3. **Deploy**: `unmute compile examples/human-transfer`, then
   `lk agent deploy` from `build/livekit/`, with the section-3 env set.
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

1. **Accounts**: Pipecat Cloud (`pcc auth login`) and a Daily domain with a
   purchased phone number and **dial-out enabled** (required by
   `sip_call_transfer`; ask Daily support if the dashboard does not offer
   it).
2. **Deploy**: `unmute compile examples/human-transfer-daily`, then
   `pcc deploy` from `build/pipecat/` (the emitted `pcc-deploy.toml` is the
   manifest); set the section-3 secrets with `pcc secrets set`.
3. **Dial-in**: connect the Daily number to the deployed agent with Pipecat
   Cloud's managed dial-in webhook (dashboard or REST API). No webhook
   server of yours is involved.
4. **Cold test**: call the Daily number, ask about an invoice. Expect: the
   announcement, then the billing phone rings and the bot exits.
5. **Failure drill**: point `BILLING_PHONE_NUMBER` at an undialable value
   and call again. Expect the agent to say the transfer did not work and
   keep helping (or hang up after a goodbye, per `on_unavailable`).

### Teardown

A test rig must not become a standing bill: release the Twilio number and
trunk if they were test-only, release the Daily number, `pcc agent delete`
the deployed bot, and delete unused LiveKit trunks and dispatch rules.

### Status

Every transfer row above is **provisional until its recipe has been run as
written**: LiveKit SIP cold, LiveKit SIP warm, Pipecat Daily cold. When a run
finds a wrong step, the fix lands in this document first.

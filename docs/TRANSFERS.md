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

### Three mechanisms, which is why the routes differ

"Transfer to a human" is not one feature with several implementations. The
shipped routes use three different mechanisms, and each one decides what is
possible on its route before any of our code runs.

| Mechanism | Where | What actually happens | What it costs |
|---|---|---|---|
| **SIP REFER** | LiveKit `sip` | The agent asks the carrier to hand the caller's **existing SIP leg** somewhere else (`TransferSIPParticipant`). The caller leaves the room; the session ends. | The trunk must allow REFER. Nothing else. |
| **Room reroute** | Pipecat `daily-sip`, both forms | Daily moves the caller's leg out of the room it is in (`transport.sip_call_transfer`) and the bot drops off. | Dial-out granted on the Daily domain. |
| **Markup replacement** | Pipecat `cloud-websocket` | One request to the carrier **replaces the live call's instructions**, keyed on its call id: speak a line, `<Dial answerOnBridge="true">` the destination. The bot's part of the call ends there. | The session cannot survive it. Whatever happens to the dial, the caller meets a **fresh** agent, because knowing more would need a callback endpoint this route exists to avoid hosting. |

Two consequences fall straight out of that:

- **Warm exists on exactly one mechanism.** A warm handoff has to hold the
  caller, dial a person, listen to how *that* leg ended, and only then merge.
  SIP REFER and the room reroute both let a platform primitive own that
  (LiveKit ships one as `WarmTransferTask`; Daily documents the pattern and we
  have not built it). Markup replacement cannot: once the markup is replaced,
  the bot has no leg to hold and no way to be told the outcome.
- **The websocket routes have no mechanism at all**, so they have no transfer.
  Both Pipecat `carrier-websocket` and LiveKit `connector` carry media and
  nothing else. Building a transfer there means owning the audio path, which
  this project deleted on purpose (see "Why the websocket 'no' rows are firm").

The rest of this section is the same information per route, with sources.

Verified against the platforms' live documentation on 2026-08-11. "Native"
means the platform ships and maintains the primitive.

| driver | route | cold | warm |
|---|---|---|---|
| livekit | `sip` (trunk) | **yes**: `TransferSIPParticipant`, a SIP REFER through the trunk. The caller leaves the room and the session ends. On failure the caller stays with the agent, so `on_unavailable` applies. | **yes**: `WarmTransferTask`, LiveKit's prebuilt. Hold music, the consult call and the merge are the task's. The **persona** that talks to the person is Unmute's since 2026-08-12 (SCHEMA N35), because the prebuilt's own never briefs unprompted; the transcript and your `briefing` text still land in the task's template. Every failure (no answer, decline, voicemail, failed dial) comes back as one error and `on_unavailable` applies. |
| livekit | `connector` (Twilio websocket) | no | no |
| pipecat | Daily, Daily-provisioned number (`transport: daily-sip`) | **yes**: `transport.sip_call_transfer`. The bot announces, Daily reroutes the leg, the bot drops off. Needs dial-out enabled on the Daily domain. | **not emitted yet.** The platform supports it; this project has not built it. Feature 004. |
| pipecat | Daily, your own carrier (`transport: daily-sip` + `carrier:`) | **yes, same primitive**: `transport.sip_call_transfer`, with the destination composed as a SIP URI at your trunk's termination address, so the leg leaves through your own carrier (SCHEMA N37, verified against [Daily transfers](https://docs.daily.co/guides/products/dial-in-dial-out/transfers) 2026-08-12: `sipCallTransfer` works for dial-in legs, SIP-to-SIP and SIP-to-PSTN both supported). Same dial-out approval on the Daily domain. **Provisional**: documented by category rather than by this exact interconnect topology, so it stays provisional until its live run is recorded in `specs/006-pipecat-carrier-telephony/tasks.md`. | **not emitted yet**, same reason as the row above, and the carrier leg will carry warm unchanged when it lands: a carrier call joins the same room as a Daily-provisioned one, and only the supervisor leg's destination composes differently. |
| pipecat | Pipecat Cloud carrier stream (`transport: cloud-websocket` + `carrier: twilio`) | **yes**, by a different mechanism: one request replaces the live call's markup at your carrier, keyed on its call id. A spoken line, `<Dial answerOnBridge="true">` on a destination read from the environment, and the bot's part ends. **Its one limit, stated plainly**: when the dial ends, the caller hears a spoken handback line and comes back to a **fresh** agent that does not remember the call, because deciding anything else would need a callback endpoint you host and this route hosts nothing. "When the dial ends" covers both endings, the destination hanging up as well as a dial that never connected, which is why that line names neither. **Provisional** until its live run is recorded in `specs/007-pipecat-native-websocket/tasks.md`. | no, by trade: a warm handoff has to act on how the destination's leg ended, which needs that same hosted callback. The refusal names it. |
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

### What the person hears on a LiveKit warm transfer

Added 2026-08-12 (SCHEMA N35). The person who answers hears the handover in the
**first sentence**: who is on hold, what they want, what was already tried, then
one question they can answer. No hello, no "how can I help", and no waiting to
be asked what the call is about. When they say they can take it, the caller is
put through.

That is Unmute's prompt, not the platform's. The prebuilt ships its own persona
saying to give the colleague context, but its lifecycle deliberately lets the
human speak first and never briefs unprompted, so the agent's first turn is a
*reply*. A reply to "hello" is a greeting. On 2026-08-12 a live warm transfer did
exactly that: the manager answered, heard nothing, then got greeted like a
stranger and had to ask what the call was about, which is the one thing a warm
transfer exists to prevent. Verified against `livekit-agents` 1.6.9 as installed
(`beta/workflows/warm_transfer.py`), 2026-08-12.

Your `briefing` text is unchanged and still lands last, after the transcript. Use
it for what is specific to your business: which fields to lead with, what the
person needs to decide.

**When the transcript is thin**, the person is told plainly that someone is on
hold asking for a person and their details are not known yet, and asked whether
they can take the call. That is a degraded transfer. A greeting would be a broken
one.

### The limit after the call is answered

**`ring_timeout` covers ringing only.** Once the person picks up, nothing bounds
the consultation, and the caller hears hold music for the whole of it. If the
person answers and then never says yes or no, the caller can in principle hold
until somebody hangs up.

What the generated agent does about it is **ask**: the prompt tells the briefing
model to decline the transfer, with the person's reason, when they say they
cannot take it, when they go quiet, or when the conversation moves on without an
answer. Declining is the prebuilt's own exit and it is the thing that stops the
hold music and gives the caller back. So this is a mitigation and not a bound: a
prompt is probabilistic.

Why there is no bound: the platform has no post-answer timeout; the awaited
result comes back through `asyncio.shield`, so a timeout on our side would raise
in the generated code while the consultation kept running with the caller still
muted and the music still playing; and the one method that stops the music and
restores the caller is private. Read from `livekit-agents` 1.6.9 on 2026-08-12
(`beta/workflows/warm_transfer.py`, `voice/agent.py`). Two ways to get a real
bound are recorded in `specs/003-warm-transfer-briefing/plan.md`, and neither is
worth its cost until somebody has met this limit in production. Documentation
feedback asking upstream for a post-answer bound was sent on 2026-08-12.

### Reading a transfer in the logs

Added 2026-08-12 (SCHEMA N35), because the live failure above produced **no log
line at all**, which left three different causes fitting the same evidence.
`lk agent logs` now shows three `info` lines per transfer. Warm:

```text
human transfer fired: escalate_to_supervisor (warm)
warm transfer dialling out: handing over 12 conversation messages
warm transfer merged after 34s: sip_abc123
```

The third line is `warm transfer unavailable after <n>s: <reason>` instead
whenever the transfer did not happen, for every reason: no answer, declined,
voicemail, failed dial. Exactly one of the two appears. Line 2 with no line 3 is
a consultation still running, or one that never ended, and that gap is itself the
signal.

Cold:

```text
human transfer fired: send_to_billing (cold)
cold transfer referring the caller out
cold transfer completed after 2s
```

or `cold transfer failed after <n>s: <reason>`.

There is a fourth cold line, and it is the one you are most likely to meet first:

```text
human transfer fired: send_to_billing (cold)
cold transfer skipped: no phone caller in the room
```

**Cold cannot be tested from the Agent Console.** It refers the caller's *existing*
SIP leg, and a console session has no SIP leg, so there is nothing to act on. Warm
works from the console because it dials out and needs no inbound leg. This is why
the two have different test rigs above, and it is what this line says when you mix
them up. The other cause is a dispatch rule pointing at a different agent name, so
the call never reaches this worker: check `lk sip dispatch list` against the
`agentName` in the generated `sip-dispatch-rule.json`.

The message count on line 2 is what the agent handed over, not the smaller number
the prebuilt's own transcript filter keeps. A count of **zero or one** means the
briefing had nothing to work with, which is a different problem with a different
fix from a briefing that was ignored. A healthy count means material was passed,
not that the model used it.

No log line carries a destination, a credential, an environment variable value, or
the caller's words. The control name on the first line already says which
destination fired, and the identity on the merge line is the platform's own value
for the joined participant, which is not a phone number.

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

Destinations are declared once for the package, as environment variable names.
Who the agent escalates to is the same desk whichever carrier reaches it:

```yaml
# agent.yaml
destinations:
  billing_line: BILLING_PHONE_NUMBER
  supervisor_line: SUPERVISOR_PHONE_NUMBER
```

And the route side, one per driver. The target names a connection; the
connection declares the route:

```yaml
# LiveKit: both shapes, on a SIP trunk.
# connections/twilio_sip.yaml
transport: sip
carrier: twilio

# targets.yaml
targets:
  livekit:
    provider: livekit
    version: "1.6.4"
    sdk_language: python
    connection: twilio_sip
```

```yaml
# Pipecat: cold only, on a Daily-provisioned number.
# connections/daily.yaml — one line, because this route has no carrier leg
transport: daily-sip

# targets.yaml
targets:
  pipecat:
    provider: pipecat
    version: "1.5.0"
    connection: daily
```

```yaml
# The same, through your own carrier's number and trunk (SCHEMA N37). The
# transfer leg leaves through your trunk.
# connections/twilio_sip_daily.yaml
transport: daily-sip
carrier: twilio

# targets.yaml
targets:
  pipecat:
    provider: pipecat
    version: "1.5.0"
    connection: twilio_sip_daily
```

The complete packages live in
[examples/livekit-human-transfer](../examples/livekit-human-transfer) (LiveKit, both shapes),
[examples/pipecat-human-transfer-daily](../examples/pipecat-human-transfer-daily)
(Pipecat, cold, Daily-provisioned number), and
[examples/pipecat-human-transfer-twilio](../examples/pipecat-human-transfer-twilio)
(Pipecat, cold, your own carrier, nothing hosted by you).

The Daily carrier form has no public example any more. Feature 007 replaced it
with the route above, which does the same job without a server to host; the route
keeps its code, its rows here, and its guards, against the fixture
`internal/testdata/daily_carrier`. Its `targets.yaml` is the snippet above.

### Why the carrier leg keeps Daily in the call

`sip_call_transfer` is the primitive on both Daily forms, and `sip_refer` was
considered and rejected for the carrier form. REFER would take Daily out of the
media path and stop its billing, which sounds better, but it depends on the
originating SIP system honouring REFER and neither Daily nor Pipecat documents
whether a carrier's `<Dial><Sip>` leg does. So this project uses the primitive it
can stand behind, and states the cost: **after a completed transfer Daily stays in
the call path and both legs keep billing until the call ends**, and the
destination leg also bills at your carrier's rate because it left through your
trunk. Verified against
[Daily transfers](https://docs.daily.co/guides/products/dial-in-dial-out/transfers),
2026-08-12.

## 3. Which secrets you need

Exact env names, as the generated `.env.example` lists them. Values are never
written into a package; a Connection stores env var names only.

**LiveKit rig** (`examples/livekit-human-transfer`):

| Name | What it is |
|---|---|
| `LIVEKIT_URL` | The LiveKit project URL (cloud or self-hosted). |
| `LIVEKIT_API_KEY` / `LIVEKIT_API_SECRET` | The project API key pair. |
| `SIP_TRUNK_HOSTNAME` / `SIP_AUTH_USERNAME` / `SIP_AUTH_PASSWORD` | The Elastic SIP trunk's host and credential list entry. The agent dials out with these directly. |
| `SIP_FROM_NUMBER` | The number on the trunk, and the number transfers are placed from. |
| `OPENAI_API_KEY` / `SLNG_API_KEY` | The package's model providers. |
| `REDIS_URL` | The coordination store for the telephony runtime. |
| `BILLING_PHONE_NUMBER` / `SUPERVISOR_PHONE_NUMBER` | The transfer destinations, read at call time. |

No trunk ID of either direction appears here any more: the generated
`telephony-setup.sh` resolves the inbound trunk by phone number when it creates
the records (SCHEMA N36, 2026-08-12).

On LiveKit Cloud the first two rows are supplied by the platform: `lk` drops them
from a secrets file and the deployed agent gets its own. Set them for local runs
and for a self-hosted server; do not try to send them as secrets.

**Pipecat rig, Daily-provisioned number** (`examples/pipecat-human-transfer-daily`):

| Name | What it is |
|---|---|
| `DAILY_API_KEY` | The Daily domain's API key. |
| `OPENAI_API_KEY` / `SLNG_API_KEY` | The package's model providers. |
| `BILLING_PHONE_NUMBER` | The transfer destination, read at call time. |

**Pipecat rig, your own carrier, nothing hosted**
(`examples/pipecat-human-transfer-twilio`). One group, because one thing reads them:
the deployed agent. There is no second side on this route, so every name below
belongs in the platform secret set.

| Name | What it is |
|---|---|
| `TWILIO_ACCOUNT_SID` / `TWILIO_AUTH_TOKEN` | The REST credentials for the one request that hands a live call to a person, and for the outbound command. |
| `TWILIO_PHONE_NUMBER` | The caller identity the recipient sees. A voice-capable number you own in this account, or a caller ID verified on it; the same number receives calls. |
| `PIPECAT_CLOUD_ORGANIZATION` | Your organization name, from `pipecat cloud organizations list`. The transfer's reconnect markup has to name `<agent>.<organization>`, and the compiler knows only the agent name. |
| `OPENAI_API_KEY` / `SLNG_API_KEY` | The package's model providers. |
| `BILLING_PHONE_NUMBER` | The transfer destination, read at call time. |

There is no `DAILY_API_KEY` here and no `REDIS_URL`: this route touches no Daily
API and keeps no shared record. A **receive-only** package on this route needs
none of the first three either.

**Pipecat rig, the Daily carrier form** (the fixture
`internal/testdata/daily_carrier`). Two groups, because two different things read
them: the deployed agent reads the agent-side names and those are what go in the
platform secret set, while the operator-run `telephony_helper.py` reads the
helper-side names and the agent never does.

| Name | Side | What it is |
|---|---|---|
| `TWILIO_ACCOUNT_SID` / `TWILIO_AUTH_TOKEN` | agent | The REST credentials for the one request that moves a live call into the room. |
| `SIP_TRUNK_HOSTNAME` | agent and helper | The trunk's termination address. Every outbound leg, transfers included, is composed as `sip:<number>@<this>`. |
| `SIP_FROM_NUMBER` | agent | The number on the trunk. |
| `DAILY_API_KEY` | agent | The Daily domain's API key. The helper does not read it: the room is the platform's to create. |
| `OPENAI_API_KEY` / `SLNG_API_KEY` | agent | The package's model providers. |
| `BILLING_PHONE_NUMBER` | agent | The transfer destination, read at call time. |
| `PIPECAT_CLOUD_API_KEY` | helper | The public key the helper starts agent sessions with. |
| `UNMUTE_HOLD_AUDIO_URL` / `UNMUTE_DAILY_ROOM_GEO` | helper, optional | Hold audio you host, and the Daily room's geography. Unset is supported on both: the caller hears a spoken line, and Daily picks its own region. |

There is no outbound trigger token on this route, unlike the carrier-WebSocket
ones. The helper answers incoming calls and nothing else, so it has no endpoint
that places a call and therefore nothing to guard: outbound is started against the
platform with the same public key, exactly as it is on a Daily-provisioned number.

There is no `SIP_AUTH_USERNAME` and no `SIP_AUTH_PASSWORD` here, and the route
rejects both by name. Daily's dial-out accepts a SIP URI with no credential field
on any documented surface, so carrier termination authenticates Daily by IP
allow-list against its published static addresses instead
(`https://ip-info.daily.co/ips/ip-info.json`, read 2026-08-12). An operator coming
from the LiveKit rig keeps `SIP_TRUNK_HOSTNAME` and `SIP_FROM_NUMBER` unchanged and
finds their two credential lines unused.

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
2. **Trunking, one-time**: compile the package first, then follow the
   `## Telephony setup` section of the generated `build/livekit/README.md`. It
   dictates the Twilio steps for this package and its own env names: create an
   Elastic SIP trunk, set termination and its credential list, point origination
   at your LiveKit project SIP URI, attach a number, and enable "Call Transfer
   (SIP REFER)" and "Enable PSTN Transfer" for cold. The LiveKit side is one
   command, `bash telephony-setup.sh`, which creates the inbound trunk and the
   dispatch rule and finds them by phone number, so no record ID is copied
   anywhere (SCHEMA N36, 2026-08-12). **No outbound trunk.**
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
3. **Deploy**: `unmute compile examples/livekit-human-transfer`, then follow the Deploy
   section of the generated `build/livekit/README.md`. It prints the exact
   commands for this package, including its region: the first deploy
   (`lk agent create`, which also writes the `livekit.toml` the build directory
   does not ship) is a different command from every later one
   (`lk agent deploy`), and the section-3 env goes in as `--secrets-file .env`
   on the first deploy. That README is the authority for the commands; this
   page deliberately does not copy them.
4. **Warm test, no phone number needed**: open the Agent Console, talk to
   the agent, give a name and a complaint, ask for a manager. Expect: one spoken
   line, hold music, the supervisor's phone rings, and the supervisor's **first
   sentence** names the caller and the complaint and ends with a question. Say
   you can take it and the two of you are joined. Keep `lk agent logs` open: the
   three warm lines above tell you what happened even when the audio does not,
   and the message count settles whether a bad briefing had nothing to work with
   or ignored what it had.
5. **Cold test**: call the Twilio number, ask about an invoice. Expect: one
   spoken line, then the billing phone rings and the agent is gone from the
   call. The log shows the three cold lines.
6. **Failure drills**: let the supervisor leg ring out (expect
   `ring_timeout`, then the agent back with the caller, or a goodbye when
   `on_unavailable: hangup`, and `warm transfer unavailable after <n>s` with a
   duration close to `ring_timeout`); answer and decline (same policy, and the
   log carries your reason).
7. **The one drill that can fail and still ship**: answer the supervisor's phone,
   say hello, then talk about anything except taking the call. Never say yes and
   never say no. Expect the agent to ask again and then decline on your behalf,
   and the caller to come back. If instead the caller keeps hearing hold music
   and no third line appears, the mitigation did not hold. That is the limit
   above, not a regression: record it, because nobody has a number for how often
   a prompt-based exit works.

### Pipecat rig (cold)

1. **Accounts and CLI**: Pipecat Cloud (`pipecat cloud auth login`) and a Daily domain
   with a purchased phone number and **dial-out enabled** (required by
   `sip_call_transfer`; ask Daily support if the dashboard does not offer
   it).
2. **Deploy**: `unmute compile examples/pipecat-human-transfer-daily`, then follow the
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

**Two different scales share the word "verified", so read which one a page means.**
This table is about **a phone call somebody made**: the recipe run as written,
against real accounts, dated. The `provisional` / `verified` tag in
`compile-report.json` is about **a credentialed smoke running in CI**, which does not
exist yet, so every route tag stays `provisional` regardless of what is recorded
here. A row below can be verified while its code tag reads provisional, and that is
not a contradiction.

| row | state | evidence |
|---|---|---|
| LiveKit SIP cold | **verified 2026-08-12** | run as written on a real Twilio Elastic SIP Trunk and a deployed LiveKit Cloud agent: a call to `SIP_FROM_NUMBER` was answered by the agent, the caller asked for billing, the destination's phone rang, the agent left the call, and the three cold log lines came back clean. This run is also what found N33: the first attempt raised `ValueError` from the prebuilt's constructor because a build directory cannot mint a platform-assigned trunk ID, which is why dial-out now passes the carrier's settings inline. |
| LiveKit SIP warm | **verified 2026-08-12** | run as written from the LiveKit Agent Console against a deployed agent: the supervisor's real phone rang, the caller held, and the two were merged. This run is what found N35: the prebuilt's own prompt never briefs unprompted, so the supervisor heard a greeting rather than the briefing, and the persona the supervisor hears is Unmute's from that date. |
| Pipecat Daily cold, Daily-provisioned number | provisional | no credentialed run recorded. Proven offline against real `pipecat-ai` 1.5.0 on 2026-08-12: the emitted transport accepts a real dial-in payload, the project passes `ruff` and `ty`, and the transfer attempts at most once. None of that is a phone call. |
| Pipecat Daily cold, your own carrier | provisional | no credentialed run recorded. Built and offline-proven 2026-08-13: the route validates Redis-free, the destination composes as a SIP URI at the trunk's termination address, the at-most-one-attempt guard and the caller-stays-connected branches are unchanged from the row above, and the emitted Python passes `ruff`. Daily documents `sipCallTransfer` for dial-in legs by category, never for this exact interconnect topology, so the run in `specs/006-pipecat-carrier-telephony/tasks.md` is what this row is waiting on. |
| Pipecat Cloud carrier stream, inbound | **verified 2026-08-13** | run as written on a real Twilio number and a deployed Pipecat Cloud agent: a static TwiML Bin, a call to the number, the agent answered and held a conversation. Two defects came out of this run rather than out of review. **F15:** `TwilioFrameSerializer` raises when `auto_hang_up` is left on and credentials are falsy, which every receive-only build hit, found by driving a synthetic Media Streams handshake into the built container. **The organization value:** a display name where the hyphenated slug belongs is refused by the platform before the agent starts, so the agent's own log stays empty and the failure looks like ours. The runbook's wording was wrong twice before it was right. |
| Pipecat Cloud carrier stream, cold transfer | **verified 2026-08-13**, except the decline path | run as written on the same deployment: the caller asked about an invoice, the agent spoke its handoff line, the destination's phone rang, and the caller was connected. **What has not been run is the decline drill**: letting the destination ring out or reject, and confirming the caller hears the failure line and then a fresh agent rather than silence. That path is emitted and offline-proven (the markup is sequential, so the failure `<Say>` and the reconnect run only when the `<Dial>` never connects), and it is the path a caller meets on a bad day, so it is called out rather than folded into the row above. |

Two of the six rows above are still waiting on a phone call, and one is waiting on a
single path within it. Saying which, per row, is the point: these rows once sat under
one sentence promising a run that had not happened, which reads as a record of
success to anyone skimming.

**What the verified rows changed about how this document works.** Every one of the
three live runs found something no amount of offline testing had: a trunk ID that a
build directory cannot mint, a prebuilt that never briefs unprompted, and a
serializer that raises on a receive-only build. That is the argument for the rule
this page sets for itself, that a wrong step is fixed here before it is fixed in
code.

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

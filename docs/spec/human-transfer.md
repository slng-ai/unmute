# SPEC: human transfer (the `cold:` and `warm:` blocks)

Source of truth for the schema: [SCHEMA.md](../SCHEMA.md) §4.7 (controls).
When code and SCHEMA.md disagree, SCHEMA.md wins (CLAUDE.md). The route rule
and the user-facing map live in [TRANSFERS.md](../TRANSFERS.md); this spec is
the design record behind them. It plugs into:

- [compiler.md](compiler.md): `spec.Load -> ir.Build -> ir.Validate`, the IR
  types, and the capability table.
- [driver-livekit.md](driver-livekit.md) and
  [driver-pipecat.md](driver-pipecat.md): the emitters.

Status (rewritten 2026-08-11, SCHEMA N31): **native primitives only.** Cold
compiles on `(livekit, sip)` and on Pipecat's Daily route; warm compiles on
`(livekit, sip)` only. Every custom transfer transport this repository built
before N31 (the Pipecat two-socket bridge, its Twilio-conference replacement,
and the LiveKit connector bridge transfers) was live-tested, found wanting,
and deleted; see "History" at the end. Every transfer row is provisional
until its TRANSFERS.md recipe has been run as written.

## §G goal

Let a package say "put this caller through to a person" and get a working
call, in the two shapes the telephony world actually has, using only calls
the platforms ship and maintain.

**Cold transfer** (blind transfer, call forwarding): one platform call. The
carrier reroutes the caller's leg, the AI drops out, the person answers
knowing nothing about the call.

**Warm transfer** (attended, consultative): the caller holds with music, the
person is dialled and briefed privately, then the two are connected. The
person can decline, and then the AI comes back to the caller.

The generated code makes one documented platform call per shape and never
owns the audio path. Where no platform primitive exists, validation refuses
and names the routes that work.

## §C constraints

The authoring constraints (locked with SCHEMA N25-N27, unchanged by N31):

- **C1: the shape is the block name, not a field value.** A control declares
  exactly one of `cold:` or `warm:`; zero or two fail at build with file:line
  and the control named. With the block, a warm-only field on a cold transfer
  is unwritable, so no cross-field rule is needed.
- **C2: `destination` lives inside the block** (N27), required in both. A
  symbolic name resolved through the target instance's `destinations:` map to
  an E.164 number, a SIP URI, or an env var name (N26), validated by
  `ir.validDestination`. Committed examples use the env var form only (V6).
- **C3: `briefing` is free text, not an enum.** The old
  `summary | message | wait` values were Vapi vocabulary that mapped to no
  shipped lowering. LiveKit's `WarmTransferTask` always passes the transcript
  and takes free text on top: the lowering passes it as the task's
  `extra_instructions` kwarg (verified against livekit-agents 1.6.4 in the
  reference checkout, 2026-08-11; the earlier `WorkflowInstructions` name
  never existed in that release and emitting it was an ImportError, B17).
- **C4: `on_unavailable` is one concept covering every way the person does
  not take the call**: no answer within `ring_timeout`, decline, voicemail,
  or a failed dial. That is the platform's own shape: `WarmTransferTask`
  surfaces all of them as one `ToolError` with the caller already restored,
  so the warm lowering is one `except` branch and one policy. On cold, the
  same field answers a failed dial.
- **C5: three knobs stay out of v1**: hold music (both platforms default to
  their own; silence on hold reads as a dropped call), DTMF extensions
  (LiveKit has `dtmf`, Pipecat has nothing portable), and caller ID (trunk
  infrastructure; the environment owns it). Each is addable inside the
  existing blocks with no shape change.

The route constraints (new with N31):

- **C6: native primitive or no compile.** No bot-owned audio bridges, no
  REST TwiML choreography, no conferences, no hold mixers, no briefing
  pipelines of ours. The gate names the working routes when it refuses, at
  the route table for telephony targets and at the control rows otherwise.
  The emitters carry the same rule as defense in depth: Pipecat refuses any
  transfer on a carrier telephony target and any warm anywhere.
- **C7: the LiveKit lowering is the platform's own two calls.** Cold: the
  tool speaks its line (V4), then `transfer_sip_participant`; a failure
  leaves the caller in the room and `on_unavailable` applies. Warm: the tool
  speaks, then awaits `WarmTransferTask(sip_call_to=..., chat_ctx=...,
  extra_instructions=<briefing>, ringing_timeout=...)` with the trunk from
  `LIVEKIT_SIP_OUTBOUND_TRUNK`. The task is beta on Python, so a warm
  package pins the verified `livekit-agents` minor series (V10) instead of
  floating.
- **C8: the Pipecat lowering is Daily cold, nothing else.** Announce through
  the LLM (the REFER keeps the bot streaming, so the line lands; B14), then
  `transport.sip_call_transfer({"toEndPoint": dest})`. The failure is a
  return value on this transport, not an exception, and the tool reads it
  (B18): `return_to_caller` hands the model a failure string, `hangup` says
  a goodbye and pushes EndFrame. Warm has no Pipecat primitive; the
  documented upstream pattern makes the bot the audio coordinator, which C6
  forbids. If real demand appears it is its own spec, never a default.
- **C9: testing is cloud-hosted and documented before it is run.** The rigs,
  the secrets, and the walkthroughs live in TRANSFERS.md; the smoke follows
  the doc as written, and fixes land in the doc first (V11). Twilio Elastic
  SIP Trunking is the telephony provider for the LiveKit route by decision;
  LiveKit Phone Numbers are inbound-only and cannot transfer.

## §I surfaces

### I.authoring

```yaml
controls:
  send_to_billing:
    kind: human_transfer
    when: The caller asks to be put through to the billing team.
    cold:
      destination: billing_line

  escalate_to_supervisor:
    kind: human_transfer
    when: The caller is upset and asks for a manager.
    warm:
      destination: supervisor_line
      briefing: |
        Give the caller's name and the invoice they are disputing.
        Say their identity is already verified.
        Ask whether they can take the call.
      ring_timeout: 30s
      on_unavailable: return_to_caller
```

Block fields:

| Field | Values | Default | Notes |
|---|---|---|---|
| `destination` | symbolic name | **required in both blocks** | Resolved through the target's `destinations:` map (C2, N26/N27). |
| `ring_timeout` | duration, Go syntax | platform default (LiveKit 30s) | How long to wait for the person to pick up. |
| `on_unavailable` | `return_to_caller \| hangup` | `return_to_caller` | What happens on no answer, decline, voicemail, or dial failure (C4). |
| `briefing` | text | the prebuilt's own persona | **`warm:` only.** What the agent tells the person, on top of the transcript. |

A package with a warm transfer declares `channels.phone.outbound: true`
(N30): warm dials its destination whatever the channel says.

### I.gates

`internal/target/telephony.go`: the LiveKit `sip` routes grant
`cold_transfer` and `warm_transfer`; no carrier-websocket route and not the
connector grant either, and `ResolveTelephonyFeature` appends the working
routes to the refusal. `internal/target/table.go`: the Pipecat cold control
row is `daily-sip` only; the Pipecat warm row is a deny naming
`(livekit, sip)`; `FieldTransferBriefing` denies Pipecat (briefing rides the
warm row). The LiveKit control rows stay unconditional on purpose: a
transfer tool on a non-telephony LiveKit target compiles and refuses at
runtime when no SIP caller exists, which is LiveKit's own documented pattern
and what makes the Agent Console warm test possible.

### I.livekit (`templates/livekit_v1/agent.py.tmpl`)

Cold: announcement (`ctx.session.say`, V4), find the SIP participant by
`ParticipantKind`, `TransferSIPParticipantRequest` with the resolved
destination and `ring_timeout` when set, one `except` per `on_unavailable`
policy. Warm: announcement, `WarmTransferTask` per C7, one
`except ToolError` branch, `room_options=room_io.RoomOptions(
delete_room_on_close=False)` so the room outlives the agent.
`pyproject.toml` pins `livekit-agents>=1.6,<1.7` when the package has a warm
transfer (V10).

### I.pipecat (`templates/pipecat_v1/bot.py.tmpl`)

The `_TRANSPORT` module global set in `run_bot`, the announcement via
`LLMMessagesAppendFrame(run_llm=True)`, `sip_call_transfer`, and the
returned-error branch per C8. The bot's leg ends on `on_dialout_answered`
(EndFrame). `DAILY_API_KEY` joins the required env.

### I.examples + I.docs

`examples/human-transfer` (LiveKit sip, both shapes) and
`examples/human-transfer-daily` (Pipecat Daily, cold). TRANSFERS.md answers
availability, yaml, secrets, and testing in one place, pinned to the
examples' generated `.env.example` by `TestV11_TransfersDocListsEveryRequiredEnv`.
TELEPHONY.md, the phone-calls doc, the target docs, controls.md, and the
Twilio walkthrough link there instead of repeating it. SCHEMA.md records the
decision as N31.

## §V invariants

- **V1**: a transfer control compiles only on a route where the platform
  documents the primitive; everywhere else `ir.Validate` refuses and names
  the working routes. Pinned by `TestV1_TransfersCompileOnlyOnNativeRoutes`
  and `TestV1_PipecatWarmTransferFailsWithSupportedRoutesNamed`.
- **V2**: no emitted artifact contains a transfer media path we own; the
  deleted machinery's names stay grep-forbidden in generation tests.
- **V3**: the warm lowering delegates hold, dial, briefing, merge, and
  failure to `WarmTransferTask`; our emitted warm code is one task call and
  one `ToolError` branch, and it never creates a room, token, or audio track.
- **V4**: the tool owns the announcement. No instructions file sequences
  "say it, then call it"; every transfer tool speaks its own line as part of
  firing (B4).
- **V5**: warm requires `channels.phone.outbound: true` (N30, B5).
- **V6**: no committed example ships a literal transfer destination (B9).
- **V7**: a transfer-free package keeps its behavior; the only permitted
  delta of the N31 back-off was the disappearance of dead always-emitted
  transfer helpers from telephony files.
- **V8**: docs and the capability table agree; no doc claims a websocket
  route can transfer.
- **V9**: transfer rows are provisional until their TRANSFERS.md recipe has
  been run as written.
- **V10**: a warm package pins the `livekit-agents` minor series the beta
  `WarmTransferTask` import was verified against (1.6 today).
- **V11**: TRANSFERS.md lists every env name the transfer examples' emitted
  `.env.example` requires, pinned by a generation test.

## §B bugs

The pre-N31 bug ledger (B1-B16: the LiveKit warm session lifetimes, the
bridge lifecycle leaks, the announcement races, the serializer auto-hangup
fight) lives in this file's git history and in the scratch SPEC.md history on
the feature branch; those designs are gone, and their invariants died with
them. Carried and new:

id|date|cause|fix
B4|2026-08-11|instructions sequenced "announce, then transfer"; the model announced and never transferred|V4: the tool owns the announcement
B5|2026-08-11|example declared `outbound: false` while its warm transfer dials out|V5/N30: warm requires outbound, validated
B9|2026-08-11|example shipped a literal phone number; the live demo dialled a number nobody answers|V6: env var names only
B17|2026-08-11|the emitted warm briefing imported `WorkflowInstructions`, which does not exist in the pinned livekit-agents (1.6.4): every warm package died with ImportError at boot. Found by T4's verify pass, missed by ruff/ty|C3/V10: `extra_instructions` kwarg; warm packages pin the verified minor series; a generation test forbids the dead name
B18|2026-08-11|the Daily cold tool ignored `sip_call_transfer`'s returned error and always reported "transferred", leaving a failed transfer indistinguishable from success|C8: the tool reads the result and applies `on_unavailable`

## History

Three custom transfer transports were designed, built, and live-tested on
this repository before N31, all trying to make transfers run through a
laptop tunnel: the Pipecat carrier-websocket two-socket bridge (hold mixer,
per-leg sockets, in-process audio pumping), its replacement where warm became
a Twilio conference (`hold_in_conference`/`join_conference`, REST redirects),
and the LiveKit connector transfers (bridge RPC, hold loop, identity-aware
forwarding). Each one made the generated process own the call's audio path,
and every live test found a new lifecycle bug in that ownership: a briefing
pipeline leak that poisoned the next call's STT, a supervisor greeted as a
customer, an announcement cut off by its own redirect, a serializer
auto-hangup fighting the conference. The deletions are commits on the
feature branch (`e1274d9` and `c5bf2ab` built them; the T1/T2 commits of the
N31 back-off removed them); revive from history, never from redesign.

# SPEC — human transfer (the `cold:` and `warm:` blocks)

Source of truth for the schema: [SCHEMA.md](../SCHEMA.md) §4.7 (controls). When code and SCHEMA.md disagree, SCHEMA.md wins (CLAUDE.md). Route resolution and the telephony vocabulary live in [TELEPHONY.md](../TELEPHONY.md) §"Human transfers"; this spec does not restate them. It plugs into:

- [compiler.md](compiler.md) — `spec.Load → ir.Build → ir.Validate`, the IR types (I.ir.types), and the capability table (I.capability).
- [driver-livekit.md](driver-livekit.md) — the LiveKit emitter (V6, T6).
- [driver-pipecat.md](driver-pipecat.md) — the Pipecat emitter (V5, T6).

Status: LiveKit shipped; Pipecat warm shipped for the carrier-WebSocket **Twilio** route (§T7/§T8, 2026-08-11), provisional until its credentialed smoke; telnyx and plivo still fail warm by name. Written 2026-08-10 after a research pass against live provider sources (LiveKit docs plus the `livekit-agents` source for `WarmTransferTask`; the Pipecat Daily PSTN docs plus `examples/flows/warm_transfer.py`); the Pipecat design re-scoped the same day from Daily to the carrier-WebSocket bridge after B5. Every platform claim below carries its source.

## §G goal

Let a package say "put this caller through to a person" and get a working call on both shipped drivers, in the two shapes the telephony world actually has.

**Cold transfer** (blind transfer, call forwarding) is one API call. The carrier reroutes the caller's leg, the AI drops out, and the person answers knowing nothing about the call. It ships today on both drivers.

**Warm transfer** (attended, consultative) is a state machine with three parties. The caller goes on hold with music, the AI opens a second leg to the person, briefs them privately, then bridges the two and steps out of the conversation. The person can decline, and then the AI has to come back to the caller. LiveKit emitted it badly before this spec (§B B1-B3, now fixed); Pipecat does not emit it at all yet.

The authoring change is to stop spelling the choice as a `mode:` field with mode-dependent siblings, and spell it as a block named after the shape, exactly as N19 did for tool execution. `mode:` and the `briefing: summary | message | wait` enum are removed.

## §C constraints

- **C1: the shape is the block name, not a field value.** A control declares exactly one of `cold:` or `warm:`. Zero blocks and two blocks both fail at **build** with file:line and the control named. (Tools check this at load because a tool is its own file and a column-zero key scan finds its blocks; a control is nested inside `agent.yaml`, so the check lives where the control is resolved.) This is N19 applied to controls, for the same reason: with `mode: cold` plus a sibling `briefing:`, a warm-only field on a cold transfer is *writable* and needs a cross-field rule to reject it. With the block, it is unwritable and the rule disappears. There is no such thing as a block with nothing set, because `destination` lives inside it (C2, SCHEMA N25).
- **C2 (reversed 2026-08-11, SCHEMA N25): `destination` lives inside the block.** The original rule was about *sharing*: both shapes need it, so it sat above them. What that produced was `cold: {}` — a block naming a shape while the field deciding where the call goes sat outside it, which reads as a shape with nothing to configure. The rule that replaces it is about *placement*: above the block is the tool (`kind`, `when`), inside it is the transfer. `destination` is required in both blocks and keeps its contract otherwise: a symbolic name resolved through the target instance's `destinations:` map to an E.164 number, a SIP URI, or an env var name (N24), validated by `ir.validDestination`.
- **C3: `briefing` is free text, not an enum.** The removed `summary | message | wait` values are Vapi's `transferPlan.mode` vocabulary (`warm-transfer-say-summary`, `warm-transfer-say-message`, `warm-transfer-wait-for-operator-to-speak-first`) and they do not map to either shipped driver. LiveKit's `WarmTransferTask` *always* summarizes and takes free text on top, as `instructions=WorkflowInstructions(extra=...)` (source-verified in `livekit-agents/livekit/agents/beta/workflows/warm_transfer.py`: `INSTRUCTIONS_TEMPLATE` interpolates `{_conversation_history}` and `{extra}`). Pipecat has no prebuilt at all, so generated code takes whatever prompt the package writes. Neither has a "wait" mode. Free text is what both can honour without loss, and it is strictly more expressive than three fixed values. Vapi keeps its own lowering when a Vapi driver ships; the enum is not the portable surface.
- **C4: the two drivers end a warm transfer with different machinery, and the spec promises the caller's experience, never the agent's exit.** On LiveKit the agent moves the person into the caller's room and shuts its session down, so caller and person continue alone (docs: `/telephony/features/transfers/warm`, step 4). On Pipecat the bot **owns the media path** — on Daily because the room dies with its owner (the Daily PSTN docs are explicit that "the room terminates and PSTN legs drop when the bot leaves"), and on carrier-WebSocket because both phone calls terminate in WebSockets on the bot process itself — so the bot stays as a silent audio bridge for the rest of the call. No invariant may say "the agent hangs up". What both promise is: the caller stops hearing the agent and starts hearing the person.
- **C5: the briefing carries the transcript by interpolation, not by a summarizer model.** Both lowerings format the conversation into the briefing instruction as plain `Caller:` / `Assistant:` lines, which is exactly what LiveKit's `_format_conversation_history` does. Pipecat must **not** use `ContextStrategy.RESET_WITH_SUMMARY` for this: it is deprecated and removed in 2.0.0 (driver-pipecat C5), and the official `examples/flows/warm_transfer.py` uses it only because it predates that deprecation. No `summarizer:` model reference is added to the control, so warm transfer costs no extra model definition and no extra LLM round trip.
- **C6: `on_unavailable` is one concept covering every way the person does not take the call.** Four failures collapse into it: nobody answers within `ring_timeout`, the person declines, the line reaches voicemail, or the dial itself fails. This is not a simplification imposed on the platform, it is the platform's own shape: `WarmTransferTask` surfaces all four as a `ToolError` completion (`decline_transfer`, `voicemail_detected`, the ringing timeout, and the dial exception all call `_set_result` with a `ToolError`), and `_set_result` already restores the caller's audio and stops the hold music before completing. The lowering is therefore one `except ToolError` and one policy branch, not four paths.
- **C7 (re-scoped 2026-08-10, B5): route gating is per exact route, never per orchestrator.** Pipecat warm targets the **carrier-WebSocket** route, Twilio first (TELEPHONY.md Phase 3), because that route's topology is the one where the privacy problem cannot exist: every human on the call has their own media WebSocket, so the caller's audio and the supervisor's audio are separate tracks by construction (C9). Daily SIP is not the v1 warm route — its shared room gives the bot one output track for everyone, which is exactly B5 — and may come later as a second lowering with its own second-participant design. Telnyx and Plivo follow Twilio only after their own smokes, per TELEPHONY.md's one-route-at-a-time rule. Pipecat cold keeps the gates it has today. LiveKit warm needs an outbound trunk, so it keeps requiring `LIVEKIT_SIP_OUTBOUND_TRUNK` (or an inline connection) and `sdk_language: python`.
- **C9 (added 2026-08-10, B5; re-scoped same day): warm transfer's privacy problem is a property of the route's audio topology, and the carrier-WebSocket route does not have it.** The problem, found on Daily: all parties share one room and the bot has **one** output track into it; `SoundfileMixer` is wired through `audio_out_mixer` on `base_output` and mixes into that same track (source-verified in a pipecat checkout: `transports/base_output.py:486`). While the bot briefs the person, the caller either hears the briefing or, if their receive permission for the bot is revoked, loses the hold music too — the two cannot both be true on one track. Pipecat's own `examples/flows/warm_transfer.py` needs a separate hold-music participant and `canReceive.byUserId` routing to escape it.
  On the carrier-WebSocket route the geometry is different: **each human is a separate phone call terminating in its own media WebSocket on the bot**. The caller's socket and the supervisor's socket are independent output tracks. Hold music is the caller transport's own `SoundfileMixer`, toggled with `MixerEnableFrame`; the briefing conversation runs entirely on the supervisor's transport. Nothing needs routing rules because nothing is shared. The hold clip ships in the artifact as an 8 kHz mono WAV (the mixer requires the file to match the output transport's sample rate, and these routes are 8 kHz μ-law).
  Conference-first — TELEPHONY.md's earlier common shape for these routes — is **rejected** for Twilio v1: a Twilio call on bidirectional `<Connect><Stream>` cannot simultaneously be a Conference participant (TELEPHONY.md, verified constraint), so a conference design must tear down and rebuild media legs mid-call. The bridge design never moves a leg: both calls keep the WebSockets they started with, and the bot pumps decoded audio between them. The cost is honest and small: the bot bridges for the rest of the call, which on this route adds no new failure mode (the bot already owns the caller's media path — if the process dies, the call dies today too) and negligible CPU (16 kB/s per direction of 8 kHz PCM).
- **C8: three knobs stay out of v1**, each for a stated reason, and each addable later inside the existing block with no shape change:
  - **hold music.** Both drivers default to built-in music, so the only thing a field buys is turning it off, and silence on hold reads to a caller as a dropped connection. LiveKit's default is `AudioConfig(BuiltinAudioClip.HOLD_MUSIC, volume=0.8)`; the Pipecat lowering ships a bundled clip through `SoundfileMixer`. Defaulting is the decision; the knob is not.
  - **extension / DTMF.** LiveKit has `dtmf` on `WarmTransferTask`. Pipecat's is carrier-dependent and unproven. A field that works on one of two drivers is a gate nobody asked for yet.
  - **caller ID.** LiveKit's `sip_number` names *the trunk's* outbound number. That is infrastructure, and SCHEMA §3 puts infrastructure in `targets.yaml`, never in `agent.yaml`. The LiveKit lowering reads `LIVEKIT_SIP_NUMBER` from the environment, which is where it belongs.

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
| `destination` | symbolic name | **required in both blocks** | Resolved through the target's `destinations:` map (C2, N24/N25). |
| `ring_timeout` | duration, Go syntax | platform default (LiveKit 30s; Twilio's dial default 60s) | How long to wait for the person to pick up. |
| `on_unavailable` | `return_to_caller \| hangup` | `return_to_caller` | What happens on no answer, decline, voicemail, or dial failure (C6). |
| `briefing` | text | driver default persona | **`warm:` only.** What the agent tells the person before bridging. |

### I.spec.Control

`internal/spec/package.go`. Remove `Mode *string`, `Briefing *string` and (N25) `Destination *string`; add the two blocks, mirroring the `Tool` execution blocks:

```go
Cold *ColdTransfer `json:"cold,omitempty" yaml:"cold,omitempty"`
Warm *WarmTransfer `json:"warm,omitempty" yaml:"warm,omitempty"`

// ColdTransfer is the `cold:` block: hand the caller off and drop out.
type ColdTransfer struct {
	Destination   string `json:"destination" yaml:"destination"`
	RingTimeout   string `json:"ring_timeout,omitempty" yaml:"ring_timeout,omitempty"`
	OnUnavailable string `json:"on_unavailable,omitempty" yaml:"on_unavailable,omitempty"`
}

// WarmTransfer is the `warm:` block: hold the caller, brief the person, bridge.
type WarmTransfer struct {
	Destination   string `json:"destination" yaml:"destination"`
	Briefing      string `json:"briefing,omitempty" yaml:"briefing,omitempty"`
	RingTimeout   string `json:"ring_timeout,omitempty" yaml:"ring_timeout,omitempty"`
	OnUnavailable string `json:"on_unavailable,omitempty" yaml:"on_unavailable,omitempty"`
}
```

`TransferShape()` and `TransferDestination()` mirror `Tool.ExecutionKind()`, answering with the selected block's name and its destination. `unexpectedControlField` (`internal/ir/build.go`) drops `mode`/`briefing`/`destination` and gains `cold`/`warm` under `ControlHumanTransfer`, so the two blocks on an `agent_transfer` still fail by name, and a top-level `destination:` fails at its own line.

### I.ir.HumanTransfer

`internal/ir/compiler.go`. `Mode TransferMode` survives as the resolved shape (the `cold`/`warm` constants stay; they are the IR vocabulary and the capability-table key). `Briefing Briefing` and its three constants are deleted. The resolved control becomes:

```go
type HumanTransfer struct {
	Kind          ControlKind    `json:"kind" yaml:"kind"`
	When          string         `json:"when,omitempty" yaml:"when,omitempty"`
	Destination   string         `json:"destination" yaml:"destination"`
	Mode          TransferMode   `json:"mode" yaml:"mode"`
	Briefing      string         `json:"briefing,omitempty" yaml:"briefing,omitempty"`
	RingTimeout   time.Duration  `json:"ring_timeout,omitempty" yaml:"ring_timeout,omitempty"`
	OnUnavailable OnUnavailable  `json:"on_unavailable" yaml:"on_unavailable"`
}
```

`OnUnavailable` is a string enum (`return_to_caller`, `hangup`) resolved to its default at Build time so no driver reads an empty value. `internal/ir/schema.go` re-derives both schemas from the structs (compiler C2); no hand-authored JSON.

### I.capability

`internal/target/table.go` and `internal/target/telephony.go`:

- `ColdTransfer` control row: unchanged.
- `WarmTransfer` control row: Pipecat stays `controlDeny` (with the reason naming the designed lowering, C9/T7) until T7 emits it; T8 then flips it to the exact `(carrier-websocket, twilio)` route condition — carrier **and** transport, the double-condition form `ControlCapability` already supports — provisional until the Phase 3 smoke.
- `TelephonyBriefingSummary`, `TelephonyBriefingMessage`, `TelephonyBriefingWait` are **deleted** from `internal/target/telephony.go`, from the LiveKit `livekitTelephonyFeatures` map, and from the `ir.Build` feature collection at `internal/ir/build.go:815-821`. Free-text briefing rides the `WarmTransfer` control row; there is nothing left to resolve per briefing value. The LiveKit SIP route's feature list drops `TelephonyBriefingSummary`.
- No new `Field` constants. `ring_timeout` and `on_unavailable` are emitted by both drivers wherever their shape is allowed, so a field-level row would have no divergence to record (SCHEMA §1: a tag exists to describe a difference).
- `pipecatEmittedFields` and `livekitEmittedFields` each gain the human-transfer entries, so the B1/V15 class (validates green, driver silently drops it) stays impossible for the new fields.

### I.livekit.lowering

`internal/generate/livekit_v1_build.go` plus `templates/livekit_v1/agent.py.tmpl`.

**Cold** keeps its current REFER, gaining the two knobs:

```python
await job_ctx.api.sip.transfer_sip_participant(
    api.TransferSIPParticipantRequest(
        room_name=job_ctx.room.name,
        participant_identity=identity,
        transfer_to="tel:+14155550123",
        play_dialtone=True,
        ringing_timeout=<ring_timeout>,   # omitted when unset
    )
)
```

The SIP participant is found by filtering `job_ctx.room.remote_participants` on `ParticipantKind.PARTICIPANT_KIND_SIP`, not by `next(iter(...))` (§B B3). On `api.SipCallError` or `api.ServerError` the caller is still in the room, so `on_unavailable` branches: `return_to_caller` returns a refusal string to the model, `hangup` calls `session.shutdown()`.

**Warm** awaits the prebuilt with the arguments it actually needs:

```python
result = await WarmTransferTask(
    sip_call_to="+14155550987",
    chat_ctx=self.chat_ctx,                                  # B1
    instructions=WorkflowInstructions(extra=<briefing>),      # omitted when unset
    ringing_timeout=<ring_timeout>,                           # omitted when unset
)
```

`WarmTransferTask` reads `LIVEKIT_SIP_OUTBOUND_TRUNK` itself, which the emitter already registers. `instructions=WorkflowInstructions(extra=...)` is used rather than the `extra_instructions=` keyword, which the source marks deprecated. The task raises `ToolError` for every unavailable case (C6), so one `except ToolError` carries the policy. On success the agent says a short bridging line and calls `session.shutdown()`, and `session.start` must pass `room_options=room_io.RoomOptions(delete_room_on_close=False)` or the room dies with the agent and takes the bridged call with it (§B B2).

### I.pipecat.lowering

`internal/generate/pipecat_v1_build.go`, `templates/pipecat_v1/bot.py.tmpl`, `templates/pipecat_v1/telephony_shared.py.tmpl`, and `templates/pipecat_v1/telephony_twilio.py.tmpl`. Emitted since 2026-08-11 on `carrier-websocket` + `twilio` only (C7). Two details differ from the plan below and are the shipped shape: hold music is a synthesized `BaseAudioMixer` subclass rather than a bundled clip through `SoundfileMixer` (no asset to ship, no sample-rate match to get wrong, no extra dependency), and the per-call handles ride a `ContextVar` set in `run_bot` rather than the Redis transfer state, because the two sockets are in one process by construction: the leg is dialled by the process holding the caller.

**Cold** is unchanged: `transport.sip_call_transfer({"toEndPoint": ...})` on Daily SIP, carrier REST on the provisional carrier-WebSocket routes.

**Warm** is generated as a **two-WebSocket software bridge**, reusing the machinery the generated Twilio app already has. Every named surface below exists today:

1. **Dial the second leg.** The `@tool` marks the session as transferring in the Redis store (`STATE`, the existing `human_transfer` coordination reason), mints a transfer token, and creates the supervisor's call exactly the way the outbound path already does: `client.calls.create(to=<destination>, from_=_env(FROM_NUMBER_ENV), twiml=_stream_twiml(token), timeout=<ring_timeout>, ...)` (`telephony_twilio.py.tmpl:144` today). The `timeout` kwarg is the `ring_timeout` lowering; status callbacks land on the existing `/telephony/status` endpoint and are correlated by the token.
2. **Hold the caller.** The caller's transport is constructed with a `SoundfileMixer` over the bundled 8 kHz mono hold clip (rides `Artifact.Files`), disabled by default; the tool pushes a developer message telling the caller to hold, then `MixerEnableFrame(True)` on the caller transport. The bot's TTS stops routing to the caller's transport for the duration (C9: separate tracks, no permission juggling).
3. **Brief on the second socket.** When the supervisor's WebSocket connects on `/telephony/ws/{token}` and the token resolves to a pending transfer, the process attaches a second `FastAPIWebsocketTransport` (same serializer family as the caller's) and runs a small briefing pipeline on it — STT → context → the session's own LLM → TTS — whose system instruction is the `briefing:` text plus the formatted transcript (C5), with two tools: `connect_to_caller` and `decline_transfer(reason)`.
4. **Bridge.** `connect_to_caller` tears down the briefing pipeline's model stages, sends `MixerEnableFrame(False)` to stop the music, and wires the two transports into a frame bridge: caller input → supervisor output, supervisor input → caller output, raw PCM both ways. The bot's own agents stop hearing the call. Session affinity is a v1 deployment constraint, not a code path: the supervisor's WebSocket must reach the process holding the caller's WebSocket, which the generated single-process compose topology already guarantees; multi-replica routing is future work recorded in TELEPHONY.md's coordination section.
5. **Unavailable.** No answer within `timeout`, a `busy`/`failed`/`no-answer` status callback, or `decline_transfer` during the briefing: stop the music, clear the Redis transfer state, restore the agent instruction through the existing `LLMUpdateSettingsFrame` path (V11), and apply `on_unavailable`. Voicemail detection on the second leg uses Twilio async AMD (`machine_detection`), which the route already documents for outbound; it is verify-at-smoke (§9-style), not assumed.

Because the bot bridges for the rest of the call, a warm transfer is **not** `EndsCall` on Pipecat, unlike cold. The two shapes take different paths in `humanTransferTool`.

### I.docs

- SCHEMA.md §4.7 `kind: human_transfer` table replaced by the block shape; §7 feature rows and §9 driver-maturity row updated; a new decision entry records the removal of `mode:` and the briefing enum.
- `docs/user/reference/controls.md` §"kind: human_transfer" rewritten to the block shape, with the per-route table and the C4 difference stated in the user's words.
- `docs/user/learn/07-phone-calls.md` keeps its route matrix; its transfer example moves to the block spelling.
- TELEPHONY.md keeps owning route resolution. Its warm-transfer state diagram already matches the generated Pipecat coordinator and needs no change.

## §V invariants

- **V1 (amended 2026-08-11, N25)**: a `human_transfer` control declares exactly one of `cold:` / `warm:`, and that block carries a `destination:`. Zero blocks, two blocks, and a block with no destination all fail at build with file:line and the control named. The zero-block error names the `destination:` the block must carry, because a block written with an empty body decodes as absent and would otherwise fail as "no block" with no clue why (C1).
- **V2**: `briefing` is legal only inside `warm:`. It is structurally unwritable elsewhere, so there is no cross-field rule and no test for one. `cold`/`warm` on an `agent_transfer` or `delegate` fail by field name through `unexpectedControlField`.
- **V3**: `mode:` and `briefing: summary|message|wait` are gone from the authoring surface. An old file using either fails strict decode (compiler V3) with the file, the line, and the offending line quoted (`agent.yaml: [47:5] unknown field "mode"`). It does **not** yet carry a migration hint naming the block form the way retired *tool* keys do (`movedToolKeys`), because that scan reads column-zero keys of a standalone tool file and a control is nested; T14 adds the equivalent.
- **V4**: `on_unavailable` resolves to `return_to_caller` when unset, at Build time. No driver ever reads an empty value, and both branches are emitted wherever the shape compiles.
- **V5**: `ring_timeout` parses as a Go duration and is omitted from the emitted call when unset, so the platform default applies. A zero or negative duration fails validation naming the control.
- **V6 (LiveKit, amends driver-livekit V6)**: `cold:` lowers to `TransferSIPParticipantRequest` with the resolved destination, the SIP participant selected by `ParticipantKind`, and `ringing_timeout` present only when set. `warm:` lowers to `WarmTransferTask` with `chat_ctx`, with `instructions=WorkflowInstructions(extra=...)` present only when `briefing` is set, and with `ringing_timeout` present only when set. Both wrap the call in the `on_unavailable` policy.
- **V7 (LiveKit)**: a package containing any warm transfer emits `session.start(..., room_options=room_io.RoomOptions(delete_room_on_close=False))`. Guarded by a generation test, because without it the bridged call drops the moment the agent shuts down (§B B2).
- **V8 (Pipecat, amends driver-pipecat V5; re-scoped 2026-08-10)**: `warm:` lowers on the exact `(pipecat, carrier-websocket, twilio)` route, provisional until its credentialed smoke (TELEPHONY.md Phase 3) like every telephony feature on these routes; every other Pipecat route fails validation before generation, in that route's own words (C7). Daily SIP warm is a possible later lowering with its own design, not a degraded version of this one.
- **V9 (Pipecat)**: a warm transfer never pushes `EndFrame` and never ends the worker. The generated `connect_to_caller` mutes the bot and returns; the session ends only through the existing participant-left path (C4). Guarded by a generation test asserting the absence of `EndFrame` on the warm path and its presence on the cold one.
- **V10 (Pipecat)**: the briefing instruction contains the formatted transcript and the `briefing:` text, and the generated code uses neither `ContextStrategy.RESET_WITH_SUMMARY` nor a summarizer model (C5). During the briefing, TTS routes **only** to the supervisor's transport and the caller's transport carries only the mixer's hold music — briefing audio reaching the caller's socket is the failure this design exists to prevent (C9). Guarded by a golden plus the Phase 3 smoke, which must include a human listening on the caller leg during a briefing.
- **V11**: on every failure path, both drivers restore what they changed before applying `on_unavailable`: the caller's audio is ungated, hold music is stopped, and on Pipecat the agent's own system instruction is restored through `LLMUpdateSettingsFrame` before any further inference. A caller who is returned hears a working agent, not a muted one.
- **V12**: the emitted-fields agreement tests cover the new fields on both drivers, so `briefing`, `ring_timeout`, and `on_unavailable` cannot validate green while an emitter ignores them (the B1/V15 class).
- **V13**: L4 smoke instantiates a warm-transfer package on both drivers against the real frameworks. `py_compile` is not enough here: the LiveKit path imports `WorkflowInstructions` from a beta namespace and the Pipecat path constructs a `SoundfileMixer`, and a wrong import is exactly what driver-pipecat B5 proved a syntax check cannot see.
- **V14**: the safe core (SCHEMA §7) still omits human transfer while the exact routes are provisional. This spec does not move that line; it only makes the two shapes emit correctly where they are already allowed.

## §T tasks

status: `x` done, `~` partial, blank open

id|status|desc|cites
T1|x|`spec.Control` gains `Cold`/`Warm` blocks, `TransferShape()` and `TransferDestination()`; `Mode`/`Briefing`/`Destination` removed; `unexpectedControlField` updated; the no-block error names the `destination:` the block must carry|C1,C2,I.spec.Control,V1,V2,V3
T2|x|`ir.HumanTransfer` gains `Briefing string`, `RingTimeout`, `OnUnavailable`; the `Briefing` enum constants are deleted; `buildHumanTransfer` resolves the `on_unavailable` default; `checkTransferBlock` validates the duration, the enum, and warm-only `briefing`; schema re-derived|I.ir.HumanTransfer,V4,V5
T3|x|capability table: the three `TelephonyBriefing*` features deleted from `telephony.go`, from `livekitEmittedTelephonyFeatures`, and from the `ir.Build` collection (the shape block is the feature now); `FieldBriefingSummary/Message/Wait` collapse into one `FieldTransferBriefing`, denied on Pipecat (no warm lowering to hang it on) and Deepgram; LiveKit SIP route feature list updated|C3,I.capability
T4|x|LiveKit cold lowering: `TransferSIPParticipantRequest` with `ringing_timeout` set the way `livekit-agents` sets it (`FromNanoseconds`, so no `google.protobuf` import is needed), SIP participant selected by `ParticipantKind`, `on_unavailable` branching on `SipCallError`/`ServerError`|I.livekit.lowering,V6,B3
T5|x|LiveKit warm lowering: `chat_ctx`, `WorkflowInstructions(extra=...)` (not the deprecated `extra_instructions=`), `ringing_timeout`, one `except ToolError` carrying the policy; the `WorkflowInstructions` and `ToolError` imports emit only when used|I.livekit.lowering,V6,C6,B1
T6|x|LiveKit `room_options=room_io.RoomOptions(delete_room_on_close=False)` emitted at every `session.start` whenever the package contains a warm transfer; `room_io` rides the `livekit.agents` import tuple so isort order holds|V7,B2
T7|x|Pipecat warm lowering on the **carrier-WebSocket Twilio route**: the two-socket bridge of I.pipecat.lowering, landed 2026-08-11 as a synthesized `_HoldMixer` (no shipped clip, C4 of the feature SPEC), an `_AudioBridge` per leg, `start_warm_leg` on the existing create-call path, transfer-token routing in `handle_media`, and a briefing worker on the person's own transport. Emit the transfer branch of `humanTransferTool` (`EndsCall: false`), the second-leg dial through the existing `client.calls.create` + `_stream_twiml(token)` path with `timeout=<ring_timeout>`, the transfer-token resolution on `/telephony/ws/{token}`, the briefing mini-pipeline with `connect_to_caller`/`decline_transfer`, the caller-transport `SoundfileMixer` + `MixerEnableFrame` hold, the frame bridge, the Redis transfer state, and the bundled 8 kHz mono hold clip on `Artifact.Files`. Verified against surfaces that already exist: the outbound create-call site (`telephony_twilio.py.tmpl:144`), the token WebSocket endpoint, the status endpoint, `FastAPIWebsocketTransport(params.serializer, params.audio_out_mixer)`, and `MixerEnableFrame` (pipecat `frames.py:1977`)|I.pipecat.lowering,C4,C7,C9,V9,V10,V11,B5
T8|x|Pipecat gating with T7: the `WarmTransfer` control row flips from deny to the exact `(carrier-websocket, twilio)` route condition, provisional until the Phase 3 smoke; `FieldTransferBriefing`'s Pipecat deny lifts with it; every other Pipecat route keeps failing before generation in its own words|C7,V8,V9
T9|x|the emitted-fields agreement entries land for both drivers (`FieldTransferBriefing`), and `pipecatEmittedTelephonyFeaturesFor` makes the warm route feature per carrier so telnyx and plivo cannot silently claim it. Goldens are unchanged because the LiveKit golden fixture carries no human transfer; the assertions live in `TestLiveKitV1HumanTransferColdAndWarm` and the parity fixture|V12
T10||L4 smokes: warm packages instantiate against real `livekit-agents` and real `pipecat-ai` (FastAPI WebSocket transport + serializer + mixer construction). Partly covered ahead of the task by T13's example, checked by hand against the real SDK; not automated yet. The Phase 3 credentialed smoke (two real numbers, a listener on the caller leg during the briefing, V10) is TELEPHONY.md's, not this suite's|V13
T11|x|docs: SCHEMA.md N23 + §4.7 rewrite + §7/§9 rows; `docs/user/reference/controls.md` rewrite; `07-phone-calls.md`, `targets/livekit.md`, `targets/pipecat.md`, ORCHESTRATOR_SHARED_CONFIGURATION.md, and TELEPHONY.md moved off the old spelling|I.docs
T12|x|migration: `internal/testdata/safe_core` moves to the block form; every test constructing `spec.Control{Mode:…}` or an `ir.Briefing*` constant moves to the block and the free-text field|V3
T13|x|`examples/human-transfer/`: one Pipecat target on the Twilio carrier-WebSocket route showing both shapes on the Twilio account trio (moved off LiveKit SIP 2026-08-11, §B B6), placeholder `+34` destinations, and a README covering the whole setup. Registered in `TestPublicExamplePackages`, so `TestPublicExamplesValidateAndGenerate` gates both shapes end to end. Checked beyond that gate by hand: `ruff check`, `ty check`, and importing the emitted `agent.py` against real `livekit-agents`|V6,V7,B4
T14||migration hint for the retired control keys: `mode:` and an enum-valued `briefing:` inside a `human_transfer` should read as "the shape is the block name now", the way `movedToolKeys` does for retired tool keys. Needs a nested-key scan (a control lives inside `agent.yaml`, not its own file), so it is a separate task rather than a line in T1|V3

Dependency order: T1 → T2 → T3 → (T4, T5, T7) → T6 after T5; T8 with T7; T9 after T4–T8; T10 after T9; T11 with T3 (the tables must agree with the docs in one commit); T12 after T1; T13 after T6. T7 additionally after TELEPHONY.md Phase 2 (outbound proven on Twilio), because the second leg rides the outbound create-call path.

## §B bugs

id|date|cause|fix
B1|2026-08-10|The shipped LiveKit warm lowering emits `await WarmTransferTask(sip_call_to=...)` and nothing else ([agent.py.tmpl:384](../../internal/generate/templates/livekit_v1/agent.py.tmpl)). `chat_ctx` is never passed, and the `livekit-agents` source shows `_format_conversation_history` returns an empty string when it is missing, so the interpolated `{_conversation_history}` in `INSTRUCTIONS_TEMPLATE` is blank. The person hears a briefing containing no call history, which is the entire point of a warm transfer. Latent because L4 smoke instantiates but never runs a transfer, and no golden asserted the argument list. Found 2026-08-10 reading the prebuilt's source during the design pass.|T5: pass `chat_ctx=self.chat_ctx`; assert the argument in a golden so the emitted call list is pinned rather than incidental.
B2|2026-08-10|The LiveKit template calls `session.start(agent=..., room=ctx.room)` with no `room_options` anywhere in the file, so `delete_room_on_close` keeps its default. After a warm transfer completes, the agent shuts its session down, the room is deleted, and the bridged caller-to-person call drops on the spot. LiveKit's own `examples/warm-transfer` sets `delete_room_on_close=False` with a comment naming this exact reason. Same latency cause as B1.|T6: emit `room_options=room_io.RoomOptions(delete_room_on_close=False)` whenever the package contains a warm transfer; regression test asserts it.
B5|2026-08-10|C5 and the original T7 plan assumed Pipecat's warm transfer could hold the caller with a `SoundfileMixer` while briefing the person privately, taking the Daily PSTN docs' "SoundfileMixer to handle hold music, and audio gating" at face value. It cannot: `audio_out_mixer` is read in `transports/base_output.py`, so the mixer rides the bot's single output track, and the caller cannot be gated away from the briefing without also losing the music. Caught before any code shipped, by checking the mixer's wiring in a pipecat source checkout rather than trusting the docs summary. Had it not been caught, the driver would have emitted a warm transfer that plays the caller the private briefing, which is the one thing warm transfer must never do.|C9 records the real constraint and the design moved routes because of it: the carrier-WebSocket topology gives every human their own socket and output track, so the one-track conflict cannot occur there, and the mixer works per-leg exactly as documented. T7 is re-scoped to that route (the two-socket bridge); Daily warm, if it ever ships, needs the separate hold-music participant that `examples/flows/warm_transfer.py` uses. The capability row keeps its deny with the reason named until T7 lands, so nothing validates green that cannot generate (the driver-livekit B2 class).
B4|2026-08-10|The generated LiveKit **SIP** entrypoint assigned `call_start`, `phone_number`, `call_context`, and `participant` unconditionally, but each is read only under a condition (`HasVars` for the first, third, and fourth; `Outbound` for the second). A package on that route with no `variables:` and no outbound emitted four assigned-never-used locals, so `ruff check` failed F841 and the project violated driver-livekit V26. Latent because no public example used the LiveKit `sip` transport: `telephony-hello` is on `connector`, whose entrypoint is a different template branch. Found the moment `examples/human-transfer` compiled and was ruff-checked (T13).|Each assignment is gated on the condition that reads it, and `participant = ` is dropped from the two `wait_for_participant` calls when nothing consumes it. Covered from now on by the public-example gate, since `human-transfer` is the first example on the route.
B6|2026-08-11|`examples/human-transfer` was written on the LiveKit SIP route, so it asked for `sip_address`/`sip_username`/`sip_password` — right for that route, wrong for anybody who has a plain Twilio account. Worse, the route needs public SIP signalling and RTP, so it cannot run on a laptop at all, while the README called it "fully working end to end". Found by the person who tried to run it.|The example moved to `(pipecat, carrier-websocket, twilio)` and the Twilio account trio, which is what `unmute dev --telephony` can actually tunnel. Landing warm transfer on that route (T7) is what made a single example able to show both shapes there.
B7|2026-08-11|The generated Twilio adapter imported `TwilioRestException` and `_outbound_request` unconditionally, but both are used only inside the `HasOutbound` block, so an inbound-only package failed `ruff check` with two F401s. Same latent class as B4: no public example was inbound-only on this route until this one.|Both imports are gated on `.Telephony.HasOutbound`, and the public-example gate now covers an inbound-only carrier-WebSocket package.
B3|2026-08-10|The LiveKit cold lowering picks the transfer target with `next(iter(job_ctx.room.remote_participants))`, taking whichever participant the map yields first. LiveKit's own docs warn against this and filter on `ParticipantKind.PARTICIPANT_KIND_SIP`, because the identity is assigned at dispatch and a room can hold more than one participant. During a warm transfer a room holds two, so a cold transfer issued after a warm one can REFER the wrong leg. Also raised by the same design pass.|T4: select the SIP participant by kind, and return a clear refusal to the model when none is present.

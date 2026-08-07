# Unmute schema, v1 (decided)

Status: locked, v1. Post-lock adversarial review (context7 + live provider docs) applied 2026-07-15, marked inline as "review-corrected": warm transfer on Pipecat/LiveKit, model-written opening on ElevenLabs/Deepgram, Deepgram outbound voicemail. These re-word or loosen gates; none changes the schema shape.
Amended 2026-07-19 (N15): the `pipeline` and `voices` blocks are removed; models are defined once, concretely, in `agent.yaml`'s central `models:` map, grouped by kind (`think`/`speak`/`listen`/`turn`, each entry carrying `provider` + `model` as the old target bindings did), referenced by agents and by the top-level `listen`/`turn` selectors, with unreferenced entries kept as swappable alternates; `targets.yaml` shrinks to infrastructure plus optional per-target model overrides; `placement` is derived from `provider`; scaffold drops the `-dev` instance suffix. This one does change the authoring shape; old files fail strict decode loudly. (`reason` survives only as an internal role identifier — the `reason:` binding block is gone, so it is no longer user-facing; the think/speak vocabulary is the authoring surface.)
Amended 2026-07-20 (N16, N17, N18): the top-level `language` field is removed — language is per-model only, and no language kwarg is emitted when a model does not set one (N16); ElevenLabs is removed from the target set — the managed group is Vapi alone, and the `region`/`edition` instance fields plus the `unmute apply` command go with it, while ElevenLabs the model vendor stays in the Pipecat/LiveKit catalogues (N17); `deployment_region` is added to target instances (N18). N16 and N17 change the authoring shape; old files fail strict decode loudly. Dated pre-removal verification notes naming ElevenLabs are kept as history.
Date: 2026-07-15.
Source: [ORCHESTRATOR_SHARED_CONFIGURATION.md](./ORCHESTRATOR_SHARED_CONFIGURATION.md). That file holds the research and the reasons. This file holds the decisions. If the two disagree, this file wins, and the other file should be fixed.

Scope: the four primary targets. **LiveKit, Pipecat, Vapi, Deepgram.** Secondary providers come later and never change this schema. (Five originally; ElevenLabs removed 2026-07-20, N17.)

Two groups of targets, used everywhere below:

- **Code targets**: LiveKit, Pipecat, Deepgram. Unmute generates the code, so it can build missing features itself.
- **Managed targets**: Vapi. The provider runs the agent, so only what their API offers can be used. (ElevenLabs was the second managed target until N17.)

---

## 1. How to read the tags

Every field below carries one tag:

- `core`: works on all four targets. No failures.
- `warn`: works on all four, but at least one target prints a warning (stderr, exit 0).
- `gated`: fails validation on at least one target. The notes say where and why.
- `provisional`: not proven on any target yet. Using it fails validation everywhere until a driver proves it. It stays in the schema so the shape is already decided.

A failure is a clear error before anything is generated or sent. A field never silently does nothing.

A blank tag cell inherits the tag of its enclosing construct: a task field without its own tag is gated (T1) like `tasks` itself.

---

## 2. Decisions

### Adopted from the source document

- **D1. Five targets decide the schema.** A field gets in only when all five primaries can honor what it promises, natively, conditionally, or through generated code. Amended 2026-07-20 (N17): four primaries after ElevenLabs' removal; the rule is unchanged.
- **D2. The source describes what the agent should do, never provider settings.** No free-form option maps in `agent.yaml`. The one exception is `params` on bindings in `targets.yaml`: sent to the provider as-is, never checked. Amended 2026-07-19 (N15): model definitions, including their `params`, now live in `agent.yaml`; the "never checked, forwarded as-is" contract is unchanged, only the file moved. Platform and telephony settings still never ride through `params`.
- **D3. Fail loudly, never average.** If a target cannot honor a field, validation fails with a clear message in that provider's own words. No silent downgrades.
- **D4. The pattern rule.** On code targets, Unmute may generate a missing feature itself. On managed targets the same feature fails, because there is nowhere to host generated logic.
- **D5. Three tiers.** T0: one agent. T1: tasks and task groups. T2: agent handoff. A file may use any tier; validation checks it against the chosen target.
- **D6. Task groups are ordered lists, not graphs.** No branches, no going back, no routers in v1.
- **D7. Context is always explicit.** Every task and transfer says what history it carries. There is no default, because providers disagree about their own defaults.
- **D8. One name space.** Tools and controls share one set of names. What an agent or task can call is decided only by its `tools:` list, nowhere else.
- **D9. Flat typed data.** Variables and task results are flat maps of name to primitive type. That is the real common ground across the five providers.
- **D10. Abstract profiles, concrete bindings.** `agent.yaml` names model and voice profiles. `targets.yaml` binds them to real models per target. Identities and `params` are forwarded as-is and never validated; the provider API and the generated project are the real validators. Version pins replace validation on code targets. Amended 2026-07-19 (N15) to **concrete models, target overrides**: `agent.yaml` defines each model fully (`provider`, `model`, voice, generation settings) and `targets.yaml` overrides a definition only for a target that cannot run it. The forwarded-verbatim, never-validated contract is unchanged.
- **D11. Declare traffic, derive machines.** The file declares peak sessions and call length. Worker counts, GPUs, and quotas are computed per target and printed in a report. They are never stored in the package.
- **D12. Package layout** as in section 3. Secrets never appear in any package file. Only env var names and secret references.

### New decisions, made here to close gaps the source left open

- **N1.** `placement` stays `api | local`. No `edge` or `on_prem` until a target actually needs the difference.
- **N2.** `capacity` stays in `agent.yaml` for v1. It may move into target instances later if multi-environment setups need different numbers.
- **N3.** Every field has an explicit required or optional status, written in the tables below. The source doc left some of these implicit.
- **N4.** A tool's name is its file name. `tools/lookup_customer.yaml` defines `lookup_customer`. There is no `name` field inside the file.
- **N5.** `assign:` is only allowed on single-task delegates. A group delegate has no `assign` in v1; step results travel through the group's shared context. Reason: the only path grammar is `result.<field>`, and it has no way to name a step.
- **N6.** Voicemail is verified on four of five primaries (2026-07-15): LiveKit `AMD` (classifications include `machine-vm`; leave a message via `generate_reply`, then shut down), Pipecat `VoicemailDetector` (leave message, then hang up), Vapi `voicemailDetection` plus `voicemailMessage` (message set = leave it, omitted = hang up), ElevenLabs `voicemail_detection` system tool plus `voicemail_message` (same semantics). `outbound: true` is therefore no longer blocked: it requires `on_voicemail`, generated with a warning on Deepgram (review-corrected 2026-07-15: an official AMD-bridge outbound reference impl proves the carrier-conditional lowering). (`greeting.speaks_first: user` history: native on Vapi as `firstMessageMode: assistant-waits-for-user`; native on ElevenLabs, verified 2026-07-15: an empty `first_message` means "the agent waits for the user to start the discussion"; generated on LiveKit and Pipecat. On Deepgram the behavior of an omitted `agent.greeting` is undocumented: warn until the driver smoke test proves silence.)
- **N7.** Variable types are the four primitives: string, number, boolean, integer. An enum result field assigns into a string variable.
- **N8.** All names (agents, tasks, groups, tools, controls, models, voices, variables, destinations) are lowercase snake_case. Names starting with an underscore are reserved by providers and rejected.
- **N9.** `pipeline.turn` is optional in `agent.yaml`. Whether a turn binding is needed in `targets.yaml` follows the target's role table (section 6), not this block. Superseded by N15 (2026-07-19): the `pipeline` block itself is gone; the role-table rule lives on in section 6.2.
- **N10.** Tool `input` is a JSON Schema object. All five targets accept JSON Schema tool inputs, so nesting is allowed here. `output` has the same shape but is only enforced on code targets; managed targets have no place to check it.
- **N11.** `greeting` is a block, not a scalar: `speaks_first: agent | user` plus an optional `text`. With `text`, the agent opens with those exact words every call. Without it, the model writes the opening from the prompt. This replaces the scalar `greeting: agent_first | user_first` spelling in the source document, which could not express a fixed opening line.
- **N12.** Task `context` is the transfer context block without `variables`. Within one session the state store is already shared on all five primaries (LiveKit `userdata`, Pipecat flow state, Vapi Squad variables), so a task has nothing to filter; `context.variables` exists only on transfers.
- **N13.** The return path is part of the contract: when a task or a `then: return` group completes, the owner receives the typed result only; the task's conversation turns are not appended to the owner's context. This is LiveKit's native `AgentTask` behavior (verified against LiveKit docs 2026-07-15: a task starts with an empty chat context and its turns are not propagated back). `TaskGroup` is different, and `summarize_chat_ctx=False` alone does not honor this: a `TaskGroup` is itself an `AgentTask`, so when it completes LiveKit merges the group's turns into the owner's context on handoff regardless of that flag (verified against the livekit-agents source 2026-07-15: `voice/agent.py` merges `old_agent.chat_ctx` with the task context on completion; `summarize_chat_ctx` only controls a separate summarization pass, not the merge). To keep the group's turns out of the owner the driver must snapshot the owner's chat context before awaiting the group and restore it afterward, returning only the typed results; it still passes `summarize_chat_ctx=False` to skip the wasteful summarization call. Generated on Pipecat and Deepgram. Vapi is n/a while single tasks fail there.
- **N14.** `language` is the agent's primary spoken language, written as a BCP-47 tag and defaulting to `en`. It does not rewrite model routes. Pipecat and LiveKit lower it only through each catalogue entry's explicit `CallSpec.Language` slot: Pipecat official services place it in `Settings`, while SLNG and LiveKit plugins use constructor kwargs. An existing target `params.language` is an explicit per-integration override. ElevenLabs lowers it once to `conversation_config.agent.language`, which governs its integrated ASR and TTS. Vapi and Deepgram stay unavailable in `unmute init` until their generators ship; no generic parameter injection is invented for them. Verified against Pipecat, SLNG, LiveKit, and ElevenLabs provider docs 2026-07-16. Superseded by N16 (2026-07-20): the top-level field is gone; `language` is per-model only and nothing is emitted when unset.
- **N15 (amended 2026-07-19).** One definition per model, in `agent.yaml`. The old shape declared the same model in up to three places (`pipeline` role placement, an abstract profile, a target binding) and put the only real definition in `targets.yaml`; all three collapse into one unified `models:` map in `agent.yaml` where every model is defined fully and concretely (`provider`, `model`, `voice`, generation settings, `params`), with voice models and think models side by side. The `pipeline` and `voices` blocks are gone. A model's kind follows from where it is referenced: an agent's `voice:` names a speak model, an agent's or task's `model:` (or a `summarizer:`) names a think model; the referenced entry is validated against that kind's fields (section 4.3). `listen` and `turn` are small optional top-level blocks (section 4.2), because they are conversation plumbing shared by the whole package, not per-agent identity; `turn.semantic_endpointing` lives there. Each entry keeps the `provider` + `model` pairing the old target bindings used (the shape the author already liked) — `provider` names the catalogue vendor, `model` is the identity that vendor's SDK expects, and `placement` derives from `provider` (`local` runs on your machines; anything else is a hosted API; an explicit `placement:` overrides). No route-parsing: folding the provider into the `model` string was rejected during implementation because the forwarded model identity is not uniform across vendors (OpenAI wants `gpt-4.1-mini`, SLNG wants `slng/deepgram/nova:3-en`), so a parse would mangle what reaches the SDK. `targets.yaml` keeps infrastructure only (provider, version, pins, transport, carrier, destinations) plus an optional `models:` override map for a target that cannot run a defined entry (section 6.2); an override replaces the whole entry, no merge rules. Supersedes N9 and amends D2/D10. Both original jobs of declared placement survive, per target and more precise: `local` still fails loudly on managed targets, and sizing still reads placement from each target's effective models. Strict decode (compiler V3) makes old files fail with "unknown field pipeline" naming file and line. The `reason` role identifier stays internal-only (the `reason:` binding block is gone), so it is never renamed in the authoring surface — think/speak is the user vocabulary. Amended again 2026-07-19 (same day, review with the author): the map is grouped into four kind sections (`think`/`speak`/`listen`/`turn`) so kind is structural rather than inferred from reference sites; listen/turn entries live in the map like everything else and the top-level `listen:`/`turn:` fields select one by name (defaulting to a section's sole entry); unreferenced entries are legal palette alternates for fast model swapping, compiled only when selected. Per-agent listen/turn was considered and rejected: STT/VAD are call plumbing (the generated Pipecat main worker owns STT; the Deepgram bridge has one listen per session), and repeating the same reference on every agent recreates the duplication N15 removes. Scaffold names the generated target instance after the provider (`pipecat`, never `pipecat-dev`): users test exactly what they deploy, and extra instances are added only when a real second environment exists.
- **N16 (2026-07-20).** No package-level `language`. Language is a property of the model that hears or speaks it, so it lives only on model definitions (section 4.3): an optional BCP-47 tag on speak and listen entries. When set, it lowers through the catalogue entry's explicit language slot exactly as N14 established (an existing `params.language` stays an explicit per-integration override). When unset, **no language kwarg is emitted anywhere**: the provider default, or the language already encoded in the model route (`slng/deepgram/nova:3-en`), applies. No `en` is ever injected. Supersedes the top-level half of N14; old files with a top-level `language:` fail strict decode loudly (compiler V3).
- **N17 (2026-07-20).** ElevenLabs leaves the target set. The managed-target group is Vapi alone; four primaries decide the schema (D1). Everything that existed only for the ElevenLabs target goes with it: its driver spec and generator, its target catalogue, the `unmute apply` command (ElevenLabs was its only wired provider; the pattern returns with the next managed driver), and the `region`/`edition` instance fields (their only consumer was ElevenLabs key residency). ElevenLabs the **model vendor** is untouched: the `elevenlabs` STT/TTS catalogue entries inside the Pipecat and LiveKit drivers stay (B4 breadth, compiler V20/V21). Dated verification records naming ElevenLabs elsewhere in this file and in §9/§B history are retained as history.
- **N18 (2026-07-20).** `deployment_region` is a new optional instance field in `targets.yaml`: where the target platform runs the deployed agent. Free-form provider vocabulary, forwarded as declared, never validated or derived — a typo fails at the provider with its own error before anything irreversible exists. Lowering, verified against provider docs 2026-07-20: Pipecat Cloud takes it as the top-level `region` key in the emitted `pcc-deploy.toml` (line omitted when unset); LiveKit Cloud accepts a region only as the `lk agent create --region` flag (`us-east|eu-central|ap-south` today, immutable after creation, no `livekit.toml` key exists), so the generated README's deploy instructions carry the flag. A model's own service region is a different knob and intentionally stays on the model: `params` for kwarg-style pinning, `endpoint_env` for endpoint-style — an agent deployed in one region may pin a model endpoint in another.

- **N16 (added 2026-07-20).** Telephony compilation is scoped to
  LiveKit and Pipecat. A telephony target selects exactly one Connection and
  one exact `(orchestrator, transport, carrier)` route. Connections live in
  `connections/<name>.yaml`, contain environment variable names rather than
  values, and never repeat `carrier`; the target owns that choice. The first
  version permits one telephony Connection per target, but a package may
  declare any number of named targets and Connections. Each target produces a
  separate single-route artifact. Capability support is resolved per route and
  feature, never with carrier-wide booleans. See
  [TELEPHONY.md](./TELEPHONY.md).

- **N19 (2026-08-07).** The four typed generation fields (`temperature`,
  `top_p`, `top_k`, `speed`) lower through an explicit per-entry slot in the
  provider catalogue, exactly as `voice`, `model` and `language` already do. An
  entry with no slot for one is a **compile error**, and each slot carries the
  vendor's own kwarg name, per framework: `speed` reaches rime as `speed_alpha` on
  LiveKit but `speedAlpha` on Pipecat (Rime's own camelCase, inside `Settings`),
  sarvam as `pace`, inworld as `speaking_rate`. This flips `speed`, `top_p` and `top_k`
  from `warn` to `gated` in section 4.3. Reason: the `warn` tag was never
  implemented, and neither reading of it holds up. Emitting the kwarg anyway
  produces Python that passes `ruff` and raises `TypeError` on the first live
  call (real cases found while implementing: Pipecat `CartesiaTTSService` keeps
  speed inside `generation_config`, Pipecat `DeepgramTTSService` and LiveKit
  `deepgram.TTS` have no rate control at all, LiveKit `elevenlabs.TTS` keeps
  speed on a nested `VoiceSettings`). Dropping it instead would silently change
  how the agent sounds. What is checked is whether the kwarg exists, which is
  6.2 rule 5, a structural fact, never the value.

  **The gate is on the typed fields only, and `params:` is untouched.** D2 and
  D10 are unamended: an author key in `params:` is forwarded verbatim and never
  checked, *including* a key spelled the same as one of these four. Write
  `params: {speed: 1.1}` and you get a literal `speed=1.1` on any vendor, slot or
  no slot; write `speed: 1.1` and it lowers through the slot or fails. Both halves
  matter. Without the second there is no gate; without the first the escape hatch
  the error message points at would not exist, and `params: {temperature: 0.2}` on
  an OpenAI STT model, a real documented parameter of that API, would stop
  compiling. Once folded the two are indistinguishable by name, so `ir.Binding`
  carries the provenance (`Generation`).

  Two spellings of one knob is an error, not a silent winner: typed `speed` plus
  `params: {speed_alpha: ...}` on rime fails naming both. This is deliberately
  *unlike* `params.language` shadowing a typed `language` (N16), where the author
  writes the same word and can see the override; here only the compiler knows the
  two names are one setting.

  Slots verified 2026-08-07 against the pipecat-ai documented Settings tables and
  the livekit-agents plugin constructor signatures.

  **Scope.** A slot here is a constructor kwarg, so the gate covers the two targets
  that emit constructors, Pipecat and LiveKit. It does not reach Vapi or Deepgram,
  and that is a permanent property of the mechanism, not a gap waiting on a driver:
  Vapi is the managed target (N17) and has nowhere to host generated code (D4),
  while the Deepgram rows are call-less by design because its bridge driver
  forwards provider names into the `Settings` JSON. Neither will ever have a
  constructor to read a slot off.

  So on both, these params stay forwarded unvalidated, exactly as vendor selection
  already is (`CheckVendor` treats a role with no rows as unrestricted, D10).
  Nothing broken is emitted either way, because neither generates a project today.

  Covering them needs two things the catalogue does not have yet, and both are
  prerequisites, not one task. First a second kind of slot: an API body path rather
  than a kwarg, declared on the `Entry` the way `RequireModel`/`RequireVoice`
  already retain `Call`-independent facts for call-less rows. Section 9 records the
  paths that exist (Vapi `assistant.model.temperature`, Deepgram
  `agent.think.provider.temperature`) and none for `speed`, `top_p` or `top_k`,
  which is why those three should eventually fail on both.

  Second, rows to declare it on. There are no Vapi rows at all, and the Deepgram
  rows cover listen and speak only (its 6.2 role table facts). Since `temperature`,
  `top_p` and `top_k` are think fields, that documented
  `agent.think.provider.temperature` path has nowhere to live either; only `speed`,
  a speak field, could attach to the Deepgram rows that exist today. Tracked as an
  open row in section 9.

---

## 3. Package layout

```text
agent.yaml            # everything in section 4
instructions.md       # complete prompt for the entry agent
agents/               # one .md per additional agent (T2)
tasks/                # one .md per task (T1)
tools/
  lookup_customer.yaml  # contract: input, output, execution, interruption, effect
  lookup_customer.py    # handler, code targets only
connections/
  primary_phone.yaml    # external telephony account, env names only
targets.yaml          # named target instances: provider, pins, destinations, model overrides
```

Rules:

- Secrets and credentials never appear in any file. Connection files and
  `targets.yaml` carry environment variable names and secret references only,
  never values.
- Remote IDs, deployment regions, SDK language, version pins, and carriers live in `targets.yaml`, never in `agent.yaml`. Model definitions live in `agent.yaml` (N15); `targets.yaml` only overrides one for a target that cannot run it.
- Machine sizes, replica counts, and GPU counts appear in neither file. They are derived and printed in reports.

---

## 4. agent.yaml

Named maps instead of lists, so every item has a stable identity and diffs stay readable. Durations use Go syntax (`90s`, `15m`, `1h30m`).

### 4.1 Top level

| Field | Required | Type | Tag |
|---|---|---|---|
| `version` | yes | int, must be `1` | core |
| `entry_agent` | yes | name of an agent | core |
| `models` | yes (N15) | four kind sections (`think`/`speak`/`listen`/`turn`), see 4.3 | core |
| `listen` | only when `models.listen` has 2+ entries | name of a listen model, see 4.2 | gated |
| `turn` | only when `models.turn` has 2+ entries | name of a turn model, see 4.2 | warn |
| `variables` | no | map, see 4.4 | core |
| `agents` | yes, must include `entry_agent` | map, see 4.5 | core |
| `tasks` | no | map, see 4.6 | gated (T1) |
| `task_groups` | no | map, see 4.6 | gated (T1) |
| `controls` | no | map, see 4.7 | per kind |
| `tools` | no | list of plain tool names | core |
| `conversation` | no | block, see 4.8 | mixed |
| `tracing` | no | block, see 4.11 | gated |
| `channels` | yes, at least one | map, see 4.9 | core |
| `capacity` | see 4.10 | block | core |

There is no package-level `language` (N16, removed 2026-07-20). `language` is a per-model field (4.3): when a speak or listen entry sets it, the shipped Pipecat and LiveKit generators lower it through the catalogue entry's language slot; when unset, no language kwarg is emitted and the provider default or the model route's own encoding (`slng/deepgram/nova:3-en`) applies. Vapi and Deepgram remain unavailable in `unmute init` until their generators ship.

Top-level `tools` is the load manifest: only listed tool files are compiled into the package. Which agents and tasks can call a tool is decided by their own `tools:` lists (D8), never here.

### 4.2 listen, turn, and placement (amended 2026-07-19, N15)

There is no `pipeline` block. The think and speak roles ride each agent's `model:` and `voice:` references (section 4.5). Listen (speech to text) and turn (end-of-turn detection) are conversation plumbing shared by the whole package: one STT hears the call no matter which agent is active, so they are selected once, not per agent. Their models are defined in the `models.listen` and `models.turn` sections (4.3); the top-level `listen:` and `turn:` fields select one **by name**:

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `listen` | no when the section has at most one chain head (the sole head selects itself; entries named only in another entry's `fallback` list are chain members, not candidates); required with 2+ heads | name of a `models.listen` entry | gated | Swapping the STT is a one-line pointer change. Validation requires an effective listen model on every resolved target whose listen role is open (section 6.2 role table): Pipecat, LiveKit, Deepgram (Deepgram models only). |
| `turn` | same rule against `models.turn` | name of a `models.turn` entry | warn | A preference, not a promise, everywhere (previously N9). On targets where turn is integrated (Vapi, Deepgram) the effective entry carries settings only. |

`placement` says where a **model** runs, not where the agent runs, and keeps its two values (N1). It is never written in the common case: it derives from `provider`. `provider: local` runs on your own machines, next to the agent worker; any other provider is a hosted API endpoint. A model definition may state `placement:` explicitly to override the derivation (rare: a self-hosted deployment of a vendor's stack). Running the agent on a laptop and deploying it later changes nothing: a hosted provider calls the vendor in both places. (`provider` is a first-class field, exactly as the old target bindings carried it — only the file moved, section 4.3. The `model` identity is whatever that provider's SDK expects, forwarded verbatim: `gpt-4.1-mini` for OpenAI, `slng/deepgram/nova:3-en` for SLNG.)

The two jobs declared placement used to do survive unchanged, per target and more precise than the old global declaration:

- `provider: local` on listen or on a speak model fails loudly on the managed target (Vapi) and on Deepgram (no slot for an outside STT), exactly as before. `provider: local` on a think model fails on Vapi too (a documented custom LLM endpoint there is unverified). On Deepgram a custom think endpoint is fine.
- placement is the main input to sizing: hosted providers make vendor quotas the limit; `local` GPU models make GPUs dominate cost. Sizing reads each target's effective models (section 6.2).

### 4.3 models (amended 2026-07-19, N15)

One central map, grouped into four kind sections: `think` (LLM), `speak` (TTS), `listen` (STT), `turn` (VAD/end-of-turn). Every model the package can use is defined here, once, fully and concretely; there is no separate `voices:` block and no per-target binding step (a target may *override* an entry, section 6.2, but never defines one). Identities, providers, and settings are forwarded to the provider as-is and never validated (D10): the provider API and the generated project are the real validators.

Every entry carries `provider` (the catalogue vendor) and `model` (the identity that vendor's SDK expects), exactly as the old target bindings did — this is the pairing that already worked, moved into `agent.yaml`. `provider: local` marks an on-machine model (section 4.2).

A model's kind is its section, and its fields are validated against that kind. References must land in the right section:

- an agent's `voice:` names a `speak` entry; an agent's or task's `model:`, a transfer's `summarizer:`, and a `fallback` list name `think` entries; the top-level `listen:`/`turn:` pointers name entries of their sections (4.2).
- a name referenced but not defined is an error naming the reference's file:line; a reference into the wrong section is an error naming both kinds.
- names are one namespace across all four sections (N8): the same name in two sections is an error.
- the map is a **palette**: entries that nothing currently references are legal and stay in the file as swappable alternates (define `nova` and `soniox` under `listen`, flip the pointer to test). Only referenced or selected entries are compiled and forwarded; the rest are inert. Fast swapping is the point; production packages keep their alternates maintained in place.

Speak model fields:

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `provider` | yes | catalogue vendor, for example `slng` | core | `local` marks an on-machine TTS (section 4.2). |
| `model` | yes | model identity forwarded to the provider, for example `slng/deepgram/aura:2-en` | core | |
| `voice` | yes | voice id, forwarded as-is | core | |
| `speed` | no, default `1.0` | number | gated (N19) | Lowered through the catalogue entry's declared slot, under that vendor's own kwarg name and per framework (rime `speed_alpha` on LiveKit, `speedAlpha` on Pipecat; sarvam `pace`; inworld `speaking_rate`). An entry with no slot is a compile error. A `speed` key inside `params:` is a different thing: forwarded verbatim, never renamed, never checked (N19). |
| `language` | no | BCP-47 tag | gated (N16) | Lowered through the catalogue entry's language slot only when set; when unset no language kwarg is emitted (N16). |
| `params` | no | open map, forwarded verbatim | core | |
| `description` | no | text | core | For humans only. |

Think model fields:

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `provider` | yes | catalogue vendor, for example `openai` | core | `local` marks a self-hosted LLM (section 4.2). |
| `model` | yes | model identity forwarded to the provider, for example `gpt-4.1-mini` | core | |
| `temperature` | no | number | core | Verified slots on all four (section 9): Vapi `assistant.model.temperature`, Deepgram `agent.think.provider.temperature`, constructor kwargs on Pipecat and LiveKit. Every catalogued think entry declares one (N19). |
| `top_p`, `top_k` | no | number | gated (N19) | Lowered through the catalogue entry's declared slot. An entry with no slot is a compile error. `top_k` is the common gap: every Pipecat LLM service takes it, on LiveKit only `anthropic` does, and `anthropic` is in turn the only LiveKit LLM with no `top_p`. The same names inside `params:` stay forwarded verbatim and unchecked (N19). |
| `params` | no | open map, forwarded verbatim | core | Anything else the bound component accepts (`max_tokens` where a slot exists; never forwarded to Deepgram, which has no max-tokens slot). |
| `description` | no | text | core | For humans only. |
| `fallback` | no | ordered list of think model names | gated | Cycle-checked. Every model in a chain must land in the same slot kind and placement on the resolved target. All verified 2026-07-15. Deepgram: native (`agent.think` as an ordered provider array; mixed providers, per-entry params). LiveKit: native (`llm.FallbackAdapter`; STT/TTS adapters exist too). Pipecat: generated (the Pipecat driver v1 does not emit fallback yet — a maturity gate, not a platform limit; lifts when driver §T lands). Vapi: native (`model.fallbackModels`); entries are same-provider model IDs, so a **cross-provider chain fails on Vapi**; verified on OpenAI model schemas, others unverified. |

Listen model fields: `provider` (yes), `model` (yes; Deepgram models only on the Deepgram target), `language` (no; when unset no language kwarg is emitted, N16), `params` (no, forwarded verbatim), `fallback` (no; ordered list of listen model names, gated — see below), `description` (no).

Listen `fallback` (added 2026-07-19, T16): the chain stays within the listen section, is cycle-checked, and every entry must share the primary's placement. LiveKit: native `stt.FallbackAdapter` (verified in the livekit-agents source 2026-07-19; the driver emits it). Pipecat: gated, the driver does not emit listen fallback yet. Vapi: gated, no documented transcriber fallback slot. Deepgram: gated, `agent.listen` takes a single provider (unlike think's ordered array). Verify rows in section 9.

Turn model fields: `provider` (no), `model` (no, for example `silero`), `semantic_endpointing` (no: `required | preferred | off`, warn — forwarded as a preference; whether it applies depends on the listen model at runtime), `params` (no), `description` (no).

There is no `tier` field on models. Nothing would use it; Unmute never picks a model for you. `fallback` lives on think and listen models today (T16); speak stays fallback-free until a slot is verified somewhere beyond LiveKit's native TTS adapter — the sectioned shape takes it later with no new syntax.

### 4.4 variables

Typed shared state. Task results, handoff payloads, and personalization all flow through it.

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `type` | yes | `string \| number \| boolean \| integer` | core | |
| `default` | no | value of that type | core | |
| `source` | no | `call_start \| session_id \| carrier \| connection \| call_id \| stream_id \| direction \| from_number \| to_number` | gated | `call_start` is caller input. The remaining values are runtime-owned system sources and must be available on the exact route before the greeting. `stream_id` is optional only when no variable requests it. |

Notes that drivers must respect: on Deepgram, live state lives in the generated bridge, never in template variables (those are substitution-time only and visible to project members; never route secrets through them).

### 4.5 agents

| Field | Required | Values | Tag |
|---|---|---|---|
| `instructions` | yes | path to a markdown file | core |
| `model` | yes | think model name (4.3) | core |
| `voice` | yes | speak model name (4.3) | core |
| `tools` | no | list of tool and control names | core |

Per-agent voices are native on LiveKit and Pipecat, and work on all four.

### 4.6 tasks and task_groups (T1)

A `task` is delegate-and-return: control comes back to the owning agent with a typed result.

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `instructions` | yes | path | | |
| `tools` | no | list of names | | |
| `model` | no | think model name (4.3) | gated | Per-task override. **Fails on Pipecat** — a maturity gate, not a platform limit (runtime-verified 2026-07-16: an `LLMSwitcher` inside an `LLMWorker` pipeline stalls all flow frames on pipecat-ai 1.5.0, so the driver has no working lowering; driver-pipecat B7. Review-corrected 2026-07-15 the other way on docs alone — the spike overrode it). |
| `result` | yes | flat map: name to `string \| number \| boolean \| integer \| {enum: [a, b]}` | core shape | Nested schemas only when every configured target is a code target. |
| `context` | yes | transfer context block without `variables` (N12), `history` required | gated | See 4.7 and the history table. |

Tier support for `task` itself: native on LiveKit. Conditional on Pipecat (needs a cascaded pipeline; Flows ships inside core `pipecat-ai` as `pipecat.flows` since 1.5.0, and the standalone `pipecat-ai-flows` package is deprecated, never used). Pattern on Deepgram (generated, fine). **Unverified on Vapi**: it is not proven that a handoff can go back to the previous assistant, so single-task delegates fail there until verified.

A `task_group` is an ordered list. No edges, no cycles possible.

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `steps` | yes | non-empty ordered list of task names | | |
| `context_scope` | yes | `shared \| isolated` | gated | `isolated` compiles only on code targets (on LiveKit as standalone AgentTasks, not TaskGroup). The group's scope overrides the member tasks' own `context` while inside the group. In history vocabulary: `shared` means each step sees the group context as `history: full`; `isolated` means each step enters as `reset`. |
| `then` | yes | `return \| transfer \| end` | gated | **`return` fails on Vapi** (a state-preserving Squad return is undocumented — gated as unverified, review-corrected 2026-07-15). |
| `then_target` | iff `then: transfer` | agent name | | |
| `merge` | no | `results` (only value) | core | On completion the owner receives the steps' typed results and nothing else (N13). On LiveKit `summarize_chat_ctx=False` is necessary but not sufficient: the driver also snapshots and restores the owner context around the group so only the results cross back (see N13). `summary` stays out: managed targets cannot host a summarizer. |

Never lowered to Vapi Workflows (they retire 2026-08-18).

### 4.7 controls

What the model can invoke besides plain tools. Controls share the tool name space (D8).

Common fields:

| Field | Required | Values | Notes |
|---|---|---|---|
| `kind` | yes | `delegate \| agent_transfer \| human_transfer` | |
| `when` | no | text | The trigger description the LLM sees. Lowered into the tool or edge description. |

`kind: delegate`:

| Field | Required | Notes |
|---|---|---|
| `task` or `group` | exactly one | Name of a task or task group. |
| `assign` | no, task only (N5) | Map of `variable_name: result.<field>`. The field must exist in the task's `result` and match the variable's type. |

A delegate hands work off; whether control comes back is decided by the target. A single task always returns. A group returns only when its `then:` is `return`; with `then: transfer` or `then: end` the delegate never returns, and the generated tool description must say so.

`kind: agent_transfer` (T2, works on all five):

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `to` | yes | agent name | core | |
| `requires` | no | list of variable names | gated | A machine-checked guard. Generated on code targets; **fails on Vapi** (no mechanism). On a failed guard the model gets a refusal naming the unmet variables; that behavior is part of the contract. |
| `context.history` | yes | `full \| messages \| last_n \| summary \| reset` | gated | See the history table below. |
| `context.max_messages` | iff `last_n` | int | | Illegal with any other value. |
| `context.summarizer` | iff `summary` is generated | think model name (4.3) | | So the model is defined and counted by sizing. |
| `context.include_tool_calls` | no, default `true` | bool | gated | `false` works on code targets only. |
| `context.variables` | yes | `all` or a list of names | gated | `all` is the only value managed targets accept. Lists compile on code targets only. |

Transfer history support per target (Vapi column verified against provider docs 2026-07-15):

| Value | LiveKit | Pipecat | Vapi | Deepgram |
|---|---|---|---|---|
| `full` | ok | ok | ok | ok |
| `messages` | ok | ok | ok | ok |
| `last_n` | ok | ok | ok | ok |
| `summary` | ok (generated) | ok (generated) | fails | ok (generated) |
| `reset` | ok | ok | ok | ok |

`reset` never promises a literally empty context; on LiveKit a handoff marker still lands in the new context. The Pipecat driver v1 emits `history: full` only — the other values (and `context.variables` subset, `include_tool_calls: false`) are a maturity gate (§9); the workers handoff carries the running context.

Vapi lowering, literal spellings verified 2026-07-15: `contextEngineeringPlan` is one of `all` (their default; ours is no default, D7), `userAndAssistantMessages`, `lastNMessages` plus `maxMessages`, `none`; no summary mode exists; `previousAssistantMessages` stays unexposed. For tasks this table collapses: code targets support all five values (generated), and Vapi is n/a while single tasks fail there.

`kind: human_transfer`:

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `destination` | yes | symbolic name | core | Resolves through the target instance's `destinations:` map to a number or SIP URI. |
| `mode` | yes | `cold \| warm` | gated | `cold`: LiveKit native, Vapi native, and Pipecat native on Daily SIP. Pipecat's exact Twilio, Telnyx, and Plivo carrier-WebSocket adapters also emit out-of-band carrier-REST cold transfer, but remain provisional. Deepgram is carrier-conditional in the bridge. **`warm`** (review-corrected 2026-07-15): LiveKit native — stable on Node, `beta.workflows` on Python (NOT Python-only); **Pipecat ships it** (custom `TransferCoordinator` + hold music on Daily PSTN, official `warm_transfer.py`) but the driver does not emit it yet (a maturity gate, not "never implemented"); on Vapi the stable `transferPlan` path needs carrier Twilio. |
| `briefing` | no, warm only | `summary \| message \| wait` | gated | Vapi: all three. LiveKit: `summary`. Everywhere else: fails. |

### 4.8 conversation

Outcomes, not provider knobs. All lifecycle fields are gated per target.

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `greeting.speaks_first` | yes, if block present | `agent \| user` | `agent` core, `user` warn | `user` means the agent stays silent until the caller talks. Native on Vapi (`firstMessageMode: assistant-waits-for-user`), verified 2026-07-15. Generated on LiveKit and Pipecat: no opening is emitted. **Warns on Deepgram**: behavior of an omitted `agent.greeting` is undocumented; the driver smoke test must prove silence. |
| `greeting.text` | no | text | core | Exact opening line, spoken word for word, every call. Verified 2026-07-15: Vapi `firstMessage`, Deepgram `agent.greeting`. Generated on LiveKit and Pipecat. May reference `{{variables}}` available at call start. |
| `interruption.enabled` | yes, if block present | bool | core | |
| `interruption.minimum_words` | no | int | warn | Lossy on Deepgram (model halts first): warns. |
| `interruption.ignore_phrases` | no | list of text | warn | Native on Vapi. Generated on LiveKit and Pipecat. Dropped with a warning on Deepgram. |
| `inactivity.nudge_after`, `inactivity.end_after` | no | durations | warn | Range-checked per target by the driver. |
| `max_duration` | no | duration | warn | Some providers have no cap knob; the driver gates and documents it. |
| `thinking_audio` | no | `none \| subtle` | gated | Native on LiveKit and Pipecat. **Fails on Deepgram and Vapi** (no faithful lowering). The Pipecat driver v1 does not emit thinking_audio yet (a maturity gate). |

The three greeting combinations:

- `speaks_first: agent` with `text`: fixed opening, same words every call. Works on all four.
- `speaks_first: agent` without `text`: the model writes the opening from the prompt, so it varies per call. Generated on LiveKit and Pipecat; native on Vapi (`firstMessageMode: assistant-speaks-first-with-model-generated-message`, verified 2026-07-15). **Generated with a warning on Deepgram** (review-corrected 2026-07-15: inject a synthetic turn at call start via `InjectUserMessage`/`InjectAgentMessage`, documented for orchestrated openings, though not framed as a first-class greeting mode).
- `speaks_first: user`: native on Vapi (verified 2026-07-15), generated on LiveKit and Pipecat, warns on Deepgram (omission behavior undocumented).

If the `greeting` block is absent, the target's own default applies and the driver warns, because provider defaults differ.

### 4.9 channels

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `kind` | yes | `realtime_audio \| telephony` | core | |
| `inbound`, `outbound` | yes, telephony only | bool | gated | `outbound: true` requires `on_voicemail` and all `source: call_start` variables satisfiable. LiveKit SIP emits both directions offline for Twilio, Telnyx, and Plivo; each exact route remains provisional. Pipecat's carrier-WebSocket adapters emit inbound and outbound paths offline for those carriers, but outbound cannot validate until voicemail handling is emitted. Vapi supports both directions; Deepgram emits outbound with a carrier-conditional warning. |
| `required_controls` | no | list from the control vocabulary | gated | Vocabulary: `cold_transfer, warm_transfer, dtmf_send, dtmf_receive, hold, hangup, voicemail_detection, ivr_navigation`. Resolved against the target's carrier and transport, never the provider brand alone. |
| `on_voicemail` | iff `outbound: true` | `hangup \| leave_message` | gated | LiveKit SIP emits both values through answering-machine detection for Twilio, Telnyx, and Plivo, with each exact route provisional. Pipecat has a verified `VoicemailDetector` platform path, but its carrier-WebSocket adapters do not emit that lowering yet. Vapi supports both values; Deepgram emits them with a carrier-conditional warning. |

### 4.10 capacity

The declared half of the resource model. Required whenever `channels` has a telephony channel or the resolved target is a code target.

| Field | Required | Type | Notes |
|---|---|---|---|
| `peak_sessions` | yes | int | Concurrent conversations at busy hour. Must not exceed `max_sessions`. |
| `max_sessions` | yes | int | Hard admission limit. Reject above it before Agent allocation; don't queue. |
| `peak_starts_per_second` | yes for telephony | number, greater than zero | Peak call-start rate. Report separately from concurrent sessions. |
| `avg_session_duration` | yes | duration | Sizing and quota input. |

Sizing depends on concurrency, placement, and channels. It does not depend on how many agents are in the file, and never on the provider brand alone. Derived numbers (workers, GPUs, quotas) are printed in the compile or plan report with dated benchmark coefficients, marked `unbenchmarked` until measured.

### 4.11 tracing (added 2026-07-20)

Tracing is package-wide and strictly opt-in. The only v1 shape is one object,
not a list:

```yaml
tracing:
  provider: langfuse
```

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `provider` | yes | `langfuse` | gated | LiveKit and Pipecat emit the integration. Vapi and Deepgram fail validation before any artifact. |

With no `tracing` block, generated projects contain no Unmute tracing setup,
Langfuse/OpenTelemetry-only imports or dependencies, Langfuse environment
variables, tracing hooks, or tracing documentation. Environment variables do
not enable an integration the package did not request.

With `provider: langfuse`, the generated runtime requires
`LANGFUSE_PUBLIC_KEY`, `LANGFUSE_SECRET_KEY`, and `LANGFUSE_BASE_URL`. Missing
or empty values fail agent startup, including when all three are absent. The
credentials select the Langfuse project; v1 adds no project name, trace name,
sampling, endpoint, or environment-variable override fields. Drivers keep
their existing trace/session naming and span mappings.

This stays schema version 1. Existing packages that want to retain the former
automatic tracing output add the block above. `unmute init` omits it; the
checked-in packages under `examples/` include it.

---

## 5. tools/*.yaml

The file name is the tool name (N4). Four parts plus a description. Which agents see the tool is decided only by their `tools:` lists in `agent.yaml`, never here.

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `description` | yes, except `builtin` | text | core | What the LLM reads. Optional for `execution: builtin`, where the prebuilt registry supplies a default and this text is added on top (docs/spec/prebuilt-tools.md). |
| `input` | yes, except `builtin` | JSON Schema object | core | Lowers natively everywhere (N10). A `builtin` tool has no `input`: the prebuilt owns its schema. |
| `output` | no | JSON Schema object | warn | Enforced by generated code on code targets. Managed targets have no slot for it: warns there. Not legal on a `builtin` tool. |
| `execution` | yes | `local \| client \| webhook \| provider_hosted \| builtin \| mcp` | see below | |
| `builtin` | iff `execution: builtin` | prebuilt registry id (v1: `end_call`) | see below | Names a provider-shipped prebuilt tool the user selects instead of authoring a handler. Unknown id fails with file:line. |
| `instructions` | no, `builtin` only | text | core | The prebuilt's closing/goodbye message (LiveKit `end_instructions`; Pipecat developer message). Illegal on a non-builtin tool. |
| `handler` | iff `execution: local` | path, default `<name>.py` | | Code targets only. Not legal on a `builtin` tool. |
| `url_env` | iff `execution: webhook` or `mcp` | env var name | core | Reference only, never a URL value. For `mcp` it names the MCP server address (driver-livekit B3, 2026-07-16: code targets have no other slot for it; managed targets may configure the server provider-side and ignore it). Not legal on a `builtin` tool. |
| `interruption` | no, default `provider_default` | `continue \| cancel \| provider_default` | warn | Honored on Pipecat (`cancel_on_interruption`); LiveKit runs tools to completion, so non-default values warn there (2026-07-16). On managed targets only `provider_default` means anything; other values warn. |
| `effect` | no, default `returns_data` | `returns_data \| ends_conversation` | core | Fixed by the registry for a `builtin` tool (`end_call` implies `ends_conversation`); a conflicting value fails. |

Execution gating across the four:

- `webhook`: works everywhere. **This is the safe choice.**
- `local`: code targets only.
- `mcp`: **fails on Deepgram** (no runtime MCP client). On LiveKit it requires SDK language Python; code targets read the server address from `url_env` (B3, 2026-07-16).
- `builtin`: LiveKit and Pipecat host the prebuilt-tool registry (v1: `end_call`); **fails on Vapi and Deepgram** (no lowering). LiveKit lowers `end_call` to the beta `EndCallTool`; Pipecat to a bodyless end tool. See docs/spec/prebuilt-tools.md.
- `client`, `provider_hosted`: gated per driver; each driver documents what it can host. Not part of the safe core yet.

The Pipecat driver v1 emits `webhook`, `local`, and `builtin` tools (amended 2026-07-17, driver-pipecat T14: `local` lowers to the same `@tool` method, body awaiting the user handler from `tools/<name>.py`; `builtin` added 2026-07-22, prebuilt-tools T6); `mcp` stays maturity-gated there until the driver emits it.

---

## 6. targets.yaml

Named target instances: which orchestrator runs the package, and the infrastructure facts that only make sense per target (amended 2026-07-19, N15 — model definitions moved to `agent.yaml` section 4.3; here they can only be overridden).

### 6.1 Instance fields

| Field | Required | Notes |
|---|---|---|
| `provider` | yes | `livekit \| pipecat \| vapi \| deepgram` for now (N17). |
| `version` | code targets | Framework pin. The driver checks it against the range its templates support. A codegen check, not model validation. |
| `pins` | no | Independently versioned packages (LiveKit plugins) get their own entries. Pipecat Flows no longer qualifies: it ships inside `pipecat-ai` core since 1.5.0; never pin the deprecated standalone `pipecat-ai-flows`. |
| `sdk_language` | no | LiveKit: warm transfer and MCP need `python`. |
| `transport`, `carrier`, `connection` | required for LiveKit or Pipecat telephony | Driver route vocabulary and a Connection name. Telephony features resolve against the exact tuple, never the orchestrator or carrier alone. |
| `deployment_region` | no | Where the target platform runs the deployed agent (N18). Provider vocabulary, forwarded as declared, never validated or derived. Pipecat: the `region` key in the emitted `pcc-deploy.toml`. LiveKit: the `--region` flag on the `lk agent create` command in the generated README deploy instructions (create-time, immutable). A model's own service region rides its `params`/`endpoint_env` instead. Replaces the retired `region`/`edition` fields (N17). |
| `models` | no | Per-target overrides, see 6.2. |
| `destinations` | if any `human_transfer` is used | Map of symbolic name to phone number or SIP URI. |

### 6.2 Overrides (amended 2026-07-19, N15)

The instance's `models:` map is optional and holds overrides only, for a target that cannot run an entry defined in `agent.yaml` (a `local` model on the managed target). Keys are model names from any section of `agent.yaml` 4.3 — names are one namespace, so the map stays flat and the kind comes from the definition. An override **replaces the whole entry** with the same shape and the same kind; there are no field-level merge rules to reason about. The effective model for a target is the override when present, the `agent.yaml` definition otherwise; every gate and sizing input below reads effective models. Selection is package-level: a target changes *what a name means* for itself, never which name is selected.

Each role is **open** or **integrated** per target:

| Role | LiveKit | Pipecat | Vapi | Deepgram |
|---|---|---|---|---|
| `listen` | open | open | open | open, Deepgram models only |
| `turn` | open | open | integrated | integrated, rides the listen entry's `params` |
| `speak` | open | open | open | open, Deepgram plus a fixed third-party list |
| `think` (formerly `reason`) | open | open | open | open, custom endpoints allowed |

Rules:

1. Every used model, and `listen` on every open-listen target, must have an effective definition. Without one there is nothing to emit; the error names the model and the target.
2. On a target whose role is integrated, the effective entry for that role carries settings for the built-in part only, and can never name an outside model.
3. Definitions and overrides carry their settings as typed fields (sections 4.2, 4.3) plus `params:`, an open map for the bound component's own remaining settings (audio format, turn thresholds where the provider puts them). Forwarded as-is, **never validated**. They configure only the bound component; platform and telephony settings can never ride through them.
4. Placement is derived from the effective entry's `provider` (`local` means `local`, anything else `api`; explicit `placement:` overrides) and gates per section 4.2.
5. If a driver has no slot for a value (a third-party listen model on Deepgram), compilation fails: the value has nowhere to go. That is a structural fact, not a judgment about the model.
6. Every forwarded model and param is listed in the compile or plan report, so what was sent is always inspectable. Some providers keep fields that do nothing, so run the agent to be sure. That is the contract.
7. An override naming a model that `agent.yaml` does not define, or changing its kind, is an error.

Why never validated: provider model lists change faster than any shipped catalog, the valid set on code targets depends on the pinned versions, and the real validators already exist (the provider API at plan/apply, the generated project at startup). Unmute relays those errors word for word.

### 6.3 Telephony connections and routes

Telephony Connections keep account-specific environment names outside the
portable Agent and outside target route selection:

```yaml
# connections/primary_phone.yaml
kind: telephony
environment:
  account_sid: TWILIO_ACCOUNT_SID
  auth_token: TWILIO_AUTH_TOKEN
  from_number: TWILIO_PHONE_NUMBER
```

The target binds that Connection to an exact route:

```yaml
targets:
  pipecat:
    provider: pipecat
    version: "1.5.0"
    transport: carrier-websocket
    carrier: twilio
    connection: primary_phone
```

A package may add more named targets and Connections for every supported
carrier route it needs. There is no package-level route-count field. The
one-Connection rule applies to each target, not to the whole package, and each
target writes a separate `build/<target-name>/` artifact. One target never
combines carriers or their route-specific limits.

The Connection's keys use carrier route vocabulary. Its values must be valid
environment variable names. Unknown keys, a missing required key, an unknown
Connection, or a route mismatch fails before generation. The loader never
reads the named environment values.

The compiler resolves directions, controls, briefing behavior, system variable
sources, evidence, public endpoints, coordination, and admission ownership for
the exact route. Every LiveKit or Pipecat v1 telephony plan resolves shared
coordination and emits Redis. LiveKit Server and LiveKit SIP use it as platform
infrastructure. Generated Pipecat code uses it only for pending-call
correlation, callback replay protection, human-transfer state, and admission;
agent handoff, tasks, transcripts, prompts, model context, and audio stay in the
active worker. The plan and compile report name the applicable reasons from the
closed set `livekit_control_plane`, `call_correlation`,
`callback_idempotency`, `human_transfer`, and `admission`. A feature is enabled
only when its row has current official documentation and a successful route
smoke. The reason set is non-empty and every reason maps to a declared service
that consumes the Redis connection; an idle Redis sidecar is invalid. Without a
live smoke, the row remains provisional and validation fails when the Agent
requests it.

---

## 7. The safe core: write this and it runs on all four

The subset that passes validation on every primary target. The regression
fixture in [internal/testdata/safe_core/](./internal/testdata/safe_core/)
follows these rules exactly.

1. Any number of agents with `agent_transfer` between them (T0 + T2).
2. Every transfer context: `history: full`, `variables: all`.
3. Tools: `execution: webhook`, `interruption: provider_default`, `effect: returns_data`.
4. Omit human transfer while the exact LiveKit and Pipecat telephony routes are
   provisional. Their platform-level and offline-emitted capabilities remain
   visible in the matrix below, but are not part of the validation-safe core.
5. Hosted providers only for listen and speak models (no `provider: local`). `turn` is a preference anyway.
6. If the agent speaks first, give it a fixed `greeting.text`. A model-written opening is generated-with-warning on Deepgram (review-corrected 2026-07-15); a fixed line stays the zero-warning safe choice.
7. Skip for now: single `tasks` (return to owner unverified on Vapi) and `task_groups` with `then: return` (fails on Vapi). A `task_group` with `then: transfer` or `end` does pass on all four (warning on LiveKit: TaskGroup experimental). Also skip `requires`, `thinking_audio`, telephony routes, warm transfer, `mcp` and `local` tools, tracing, and any history other than `full`. `fallback` passes everywhere when the chain stays within one provider on Vapi. Pipecat's current carrier routes do not emit the required voicemail handling, and every exact telephony route remains provisional until its credentialed smoke passes.
8. Accept warnings: interruption tuning on Deepgram, turn model notes.

Feature by feature:

| Feature | LiveKit | Pipecat | Vapi | Deepgram |
|---|---|---|---|---|
| single agent (T0) | ok | ok | ok | ok |
| agent_transfer, `full` + `all` | ok | ok | ok | ok |
| fixed opening line (`greeting.text`) | ok | ok | ok | ok |
| model-written opening (no `text`) | ok | ok | ok | generated (warn) |
| user speaks first (`speaks_first: user`) | ok | ok | ok | warn |
| task | ok | ok | unverified | ok |
| task_group, `then: transfer\|end` | warn | ok | ok | ok |
| task_group, `then: return` | warn | ok | fail | ok |
| history `messages` / `last_n` / `reset` | ok | gated (v1) | ok | ok |
| history `summary` | ok | gated (v1) | fail | ok |
| `requires:` | ok | ok | fail | ok |
| `fallback:` (think) | ok | gated (v1) | conditional | ok |
| `fallback:` (listen) | ok | gated (v1) | fail | fail |
| human_transfer cold | ok | Daily SIP, or provisional carrier REST on Twilio/Telnyx/Plivo | ok | carrier-conditional |
| human_transfer warm | native (Node stable, Python Beta) | ships, not emitted yet | Twilio only (stable path) | carrier-conditional |
| `thinking_audio` | ok | gated (v1) | fail | fail |
| `provider: local` (listen/speak) | ok | ok | fail | fail |
| webhook tools | ok | ok | ok | ok |
| mcp tools | Python only | gated (v1) | ok | fail |
| outbound + `on_voicemail` | ok | gated (v1) | ok | generated (warn) |
| tracing `provider: langfuse` | ok | ok | fail | fail |

---

## 8. Not in v1

- Branches, routers, backtracking, any graph beyond linear task groups.
- Integrated speech-to-speech models (one model doing listen, think, and speak). Cascaded only in v1.
- Parallel conversational agents and supervisor logic as schema constructs.
- `merge: summary` and any automatic transfer summarization.
- Cross-session variable persistence and history retention toggles.
- Multiple tracing providers, simultaneous trace export, provider options, and
  a generic module registry.
- Force-interrupt phrase lists, pacing knobs, canned audio steps, external mid-call event injection. All target native.
- Messaging channels, batch outbound campaigns, IVR navigation as behavior.
- Machine sizes, replica counts, GPU types or counts, anywhere.
- Automatic telephony provisioning. Carriers and trunks are target configuration.

---

## 9. Verify before these harden

Open items that can loosen or tighten a gate above. None of them changes the shape of the schema; they change tags.

Resolved by the 2026-07-15 research pass (context7 plus official docs; exact fields recorded in the sections above):

- `fallback` on all five primaries: LiveKit `FallbackAdapter` (LLM, STT, TTS; Python and Node); Vapi `model.fallbackModels` (same-provider chains, verified on OpenAI model schemas); ElevenLabs `backup_llm_config` (`preference: override` with ordered `order`, `cascade_timeout_seconds`; no per-entry params); Deepgram `agent.think` ordered provider array (mixed providers, per-entry params). Pipecat stays generated.
- `speaks_first: user` on ElevenLabs: an empty `first_message` is documented as "the agent waits for the user to start the discussion". Native.
- Voicemail on LiveKit (`AMD`), Pipecat (`VoicemailDetector`), Vapi (`voicemailDetection` + `voicemailMessage`), ElevenLabs (`voicemail_detection` system tool + `voicemail_message`): both `hangup` and `leave_message`. Outbound unblocked (N6).
- Generation param slots: Vapi `assistant.model.temperature` and `maxTokens`; ElevenLabs `conversation_config.agent.prompt.temperature` (default 0) and `max_tokens`; Deepgram `agent.think.provider.temperature` (**no max-tokens slot exists**, do not forward one). Values stay forwarded verbatim (D10); **which** kwarg each slot is became per-entry catalogue data on 2026-08-07 (N19), and Pipecat and LiveKit now gate a missing one. Vapi and Deepgram are not gated, because neither has a catalogued constructor to read a slot from — see the open row below.
- Pipecat custom STT/TTS: `OpenAISTTService` and `OpenAITTSService` take a documented `base_url` override for OpenAI-compatible endpoints; service subclassing only for other protocols. (Later same day: SLNG STT/TTS ships first-class as `pipecat-slng` / `livekit-plugins-slng`, mapped by the provider catalogue, so the `base_url` path serves generic custom endpoints, not SLNG.)
- Pipecat Flows: ships inside core `pipecat-ai` (`pipecat.flows`) since 1.5.0; the standalone `pipecat-ai-flows` package (last 1.4.0) is deprecated and never used.
- Deepgram reusable agent configurations: exist and are immutable (delete and recreate to change; referenced as a UUID string in place of the `agent` object). Decision stands: compile to inline `Settings`; immutability makes reusable configs create-per-change churn with no compile-time benefit.

Still open:

| Item | Field affected | Today's stance |
|---|---|---|
| Vapi: can a handoff return to a previously active assistant? Docs silent both ways (no destination restriction documented; `previousAssistantMessages` implies prior sessions exist). Needs an empirical test, not more reading. | `tasks` on Vapi | fails |
| Model-written opening on ElevenLabs and Deepgram | `greeting` without `text` | **review-resolved 2026-07-15**: ElevenLabs conditional via Workflow `entry_behavior: generate_immediately`; Deepgram generated via `InjectUserMessage`/`InjectAgentMessage` (warn) |
| Deepgram: behavior of an omitted `agent.greeting` (silence assumed, undocumented) | `speaks_first: user` on Deepgram | warns; prove in driver smoke test |
| Deepgram voicemail: bridge plus carrier AMD lowering | `on_voicemail`, `outbound` on Deepgram | **review-resolved 2026-07-15**: Deepgram publishes an official AMD-bridge outbound reference impl (Twilio async AMD → hangup + leave-message); generated, carrier-conditional (warn) |
| In-flight tool calls on barge-in, managed target: Vapi docs silent | `interruption` | `provider_default` only |
| Vapi `fallbackModels` on non-OpenAI model schemas | `fallback` on Vapi | conditional, same-provider chains |
| Listen fallback slots beyond LiveKit: Vapi transcriber fallback, Deepgram multi-provider listen (added 2026-07-19, T16) | `fallback` on listen | gated on Pipecat/Vapi/Deepgram until a slot is doc-verified |
| Generation slots as an API body path, not a constructor kwarg. N19's gate reads a kwarg, so it can never reach Vapi (managed, nowhere to host generated code, D4) or Deepgram (rows call-less by design; the bridge forwards names into `Settings` JSON). Their real slot is a body path, `temperature` only per the resolved row above, so `speed`/`top_p`/`top_k` genuinely have nowhere to go there and should fail once paths are declared. Blocked on two prerequisites: an `Entry`-level path declaration (like `RequireModel`/`RequireVoice`), and rows to put it on — Vapi has none, and Deepgram's cover listen and speak only, so the think params have no row on either target. | `speed`, `top_p`, `top_k` on Vapi and Deepgram | forwarded unvalidated (D10), as vendor selection already is |

**Driver maturity gates (tags tightened until a driver emits the feature).** A code target may support a feature at the schema level while its first driver has not emitted the lowering yet. Like warm transfer (§4.7), these are gates on the driver, not the platform, and lift when the matching driver §T task lands:

| Driver | Gated until emitted | Where |
|---|---|---|
| Pipecat v1 | `models.fallback`, `thinking_audio`, `outbound` + `on_voicemail`, `mcp` tools, warm transfer; transfer/task context shaping beyond the safe-core defaults — `history` other than `full`, `context.variables` subset, `include_tool_calls: false` (the workers handoff carries the running context; fine-grained shaping is not emitted yet). (`local` tools lifted 2026-07-17, driver-pipecat T14.) | [docs/spec/driver-pipecat.md](docs/spec/driver-pipecat.md) §T. Emitted: single agent, `agent_transfer` (+ `requires` guard), `tasks`, `task_groups` with `context_scope` (shared/isolated), `then` return/transfer/end, `local` tools (2026-07-17). |

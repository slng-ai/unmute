# Unmute schema, v1 (decided)

Status: locked, v1. Post-lock adversarial review (context7 + live provider docs) applied 2026-07-15, marked inline as "review-corrected": warm transfer on Pipecat/LiveKit, model-written opening on ElevenLabs/Deepgram, Deepgram outbound voicemail. These re-word or loosen gates; none changes the schema shape.
Amended 2026-07-19 (N15): the `pipeline` and `voices` blocks are removed; models are defined once, concretely, in `agent.yaml`'s unified `models:` map (voice and think models side by side, each carrying `provider` + `model` as the old target bindings did) and referenced by agents; `targets.yaml` shrinks to infrastructure plus optional per-target model overrides; `placement` is derived from `provider`; scaffold drops the `-dev` instance suffix. This one does change the authoring shape; old files fail strict decode loudly. (`reason` survives only as an internal role identifier — the `reason:` binding block is gone, so it is no longer user-facing; the think/speak vocabulary is the authoring surface.)
Date: 2026-07-15.
Source: [ORCHESTRATOR_SHARED_CONFIGURATION.md](./ORCHESTRATOR_SHARED_CONFIGURATION.md). That file holds the research and the reasons. This file holds the decisions. If the two disagree, this file wins, and the other file should be fixed.

Scope: the five primary targets. **LiveKit, Pipecat, Vapi, ElevenLabs, Deepgram.** Secondary providers come later and never change this schema.

Two groups of targets, used everywhere below:

- **Code targets**: LiveKit, Pipecat, Deepgram. Unmute generates the code, so it can build missing features itself.
- **Managed targets**: Vapi, ElevenLabs. The provider runs the agent, so only what their API offers can be used.

---

## 1. How to read the tags

Every field below carries one tag:

- `core`: works on all five targets. No failures.
- `warn`: works on all five, but at least one target prints a warning (stderr, exit 0).
- `gated`: fails validation on at least one target. The notes say where and why.
- `provisional`: not proven on any target yet. Using it fails validation everywhere until a driver proves it. It stays in the schema so the shape is already decided.

A failure is a clear error before anything is generated or sent. A field never silently does nothing.

A blank tag cell inherits the tag of its enclosing construct: a task field without its own tag is gated (T1) like `tasks` itself.

---

## 2. Decisions

### Adopted from the source document

- **D1. Five targets decide the schema.** A field gets in only when all five primaries can honor what it promises, natively, conditionally, or through generated code.
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
- **N13.** The return path is part of the contract: when a task or a `then: return` group completes, the owner receives the typed result only; the task's conversation turns are not appended to the owner's context. This is LiveKit's native `AgentTask` behavior (verified against LiveKit docs 2026-07-15: a task starts with an empty chat context and its turns are not propagated back). `TaskGroup` is different, and `summarize_chat_ctx=False` alone does not honor this: a `TaskGroup` is itself an `AgentTask`, so when it completes LiveKit merges the group's turns into the owner's context on handoff regardless of that flag (verified against the livekit-agents source 2026-07-15: `voice/agent.py` merges `old_agent.chat_ctx` with the task context on completion; `summarize_chat_ctx` only controls a separate summarization pass, not the merge). To keep the group's turns out of the owner the driver must snapshot the owner's chat context before awaiting the group and restore it afterward, returning only the typed results; it still passes `summarize_chat_ctx=False` to skip the wasteful summarization call. Generated on Pipecat and Deepgram. On ElevenLabs the single running transcript makes task turns visible to the owner; the driver warns. Vapi is n/a while single tasks fail there.
- **N14.** `language` is the agent's primary spoken language, written as a BCP-47 tag and defaulting to `en`. It does not rewrite model routes. Pipecat and LiveKit lower it only through each catalogue entry's explicit `CallSpec.Language` slot: Pipecat official services place it in `Settings`, while SLNG and LiveKit plugins use constructor kwargs. An existing target `params.language` is an explicit per-integration override. ElevenLabs lowers it once to `conversation_config.agent.language`, which governs its integrated ASR and TTS. Vapi and Deepgram stay unavailable in `unmute init` until their generators ship; no generic parameter injection is invented for them. Verified against Pipecat, SLNG, LiveKit, and ElevenLabs provider docs 2026-07-16.
- **N15 (amended 2026-07-19).** One definition per model, in `agent.yaml`. The old shape declared the same model in up to three places (`pipeline` role placement, an abstract profile, a target binding) and put the only real definition in `targets.yaml`; all three collapse into one unified `models:` map in `agent.yaml` where every model is defined fully and concretely (`provider`, `model`, `voice`, generation settings, `params`), with voice models and think models side by side. The `pipeline` and `voices` blocks are gone. A model's kind follows from where it is referenced: an agent's `voice:` names a speak model, an agent's or task's `model:` (or a `summarizer:`) names a think model; the referenced entry is validated against that kind's fields (section 4.3). `listen` and `turn` are small optional top-level blocks (section 4.2), because they are conversation plumbing shared by the whole package, not per-agent identity; `turn.semantic_endpointing` lives there. Each entry keeps the `provider` + `model` pairing the old target bindings used (the shape the author already liked) — `provider` names the catalogue vendor, `model` is the identity that vendor's SDK expects, and `placement` derives from `provider` (`local` runs on your machines; anything else is a hosted API; an explicit `placement:` overrides). No route-parsing: folding the provider into the `model` string was rejected during implementation because the forwarded model identity is not uniform across vendors (OpenAI wants `gpt-4.1-mini`, SLNG wants `slng/deepgram/nova:3-en`), so a parse would mangle what reaches the SDK. `targets.yaml` keeps infrastructure only (provider, version, pins, transport, carrier, destinations) plus an optional `models:` override map for a target that cannot run a defined entry (section 6.2); an override replaces the whole entry, no merge rules. Supersedes N9 and amends D2/D10. Both original jobs of declared placement survive, per target and more precise: `local` still fails loudly on managed targets, and sizing still reads placement from each target's effective models. Strict decode (compiler V3) makes old files fail with "unknown field pipeline" naming file and line. The `reason` role identifier stays internal-only (the `reason:` binding block is gone), so it is never renamed in the authoring surface — think/speak is the user vocabulary. Scaffold names the generated target instance after the provider (`pipecat`, never `pipecat-dev`): users test exactly what they deploy, and extra instances are added only when a real second environment exists.

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
targets.yaml          # named target instances: provider, pins, destinations, model overrides
```

Rules:

- Secrets and credentials never appear in any file. `targets.yaml` carries env var names and secret references only, never values.
- Remote IDs, regions, editions, SDK language, version pins, and carriers live in `targets.yaml`, never in `agent.yaml`. Model definitions live in `agent.yaml` (N15); `targets.yaml` only overrides one for a target that cannot run it.
- Machine sizes, replica counts, and GPU counts appear in neither file. They are derived and printed in reports.

---

## 4. agent.yaml

Named maps instead of lists, so every item has a stable identity and diffs stay readable. Durations use Go syntax (`90s`, `15m`, `1h30m`).

### 4.1 Top level

| Field | Required | Type | Tag |
|---|---|---|---|
| `version` | yes | int, must be `1` | core |
| `language` | no, defaults to `en` | BCP-47 tag, for example `en` or `es-MX` | gated (N14) |
| `entry_agent` | yes | name of an agent | core |
| `models` | yes (N15) | map of model definitions, see 4.3 | core |
| `listen` | no; required when a resolved target's listen role is open | block, see 4.2 | gated |
| `turn` | no | block, see 4.2 | warn |
| `variables` | no | map, see 4.4 | core |
| `agents` | yes, must include `entry_agent` | map, see 4.5 | core |
| `tasks` | no | map, see 4.6 | gated (T1) |
| `task_groups` | no | map, see 4.6 | gated (T1) |
| `controls` | no | map, see 4.7 | per kind |
| `tools` | no | list of plain tool names | core |
| `conversation` | no | block, see 4.8 | mixed |
| `channels` | yes, at least one | map, see 4.9 | core |
| `capacity` | see 4.10 | block | core |

`language` is lowered by the shipped Pipecat, LiveKit, and ElevenLabs generators. Vapi and Deepgram remain unavailable in `unmute init` until their generators ship.

Top-level `tools` is the load manifest: only listed tool files are compiled into the package. Which agents and tasks can call a tool is decided by their own `tools:` lists (D8), never here.

### 4.2 listen, turn, and placement (amended 2026-07-19, N15)

There is no `pipeline` block. The think and speak roles ride each agent's `model:` and `voice:` references (section 4.5). What remains at the top level is the conversation plumbing shared by the whole package:

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `listen.provider` | yes, if block present | catalogue vendor, for example `slng` | gated | Names the catalogue entry. `local` runs your own STT. |
| `listen.model` | yes, if block present | model identity forwarded to the provider, for example `slng/deepgram/nova:3-en` | gated | The block itself is optional in the file, but validation requires it on every resolved target whose listen role is open (section 6.2 role table): Pipecat, LiveKit, Deepgram (Deepgram models only). On integrated-listen targets (ElevenLabs) the block, when present, carries settings the built-in ASR accepts and never an outside model. |
| `listen.params` | no | open map, forwarded verbatim | core | |
| `turn.provider` | no | catalogue vendor, for example `local` | warn | Defaults to the target's built-in turn/VAD. |
| `turn.model` | no | model identity, for example `silero` | warn | A preference, not a promise, everywhere (previously N9). On targets where turn is integrated (Vapi, ElevenLabs, Deepgram) the block carries settings only. |
| `turn.semantic_endpointing` | no | `required \| preferred \| off` | warn | Forwarded as a preference. Whether it really applies depends on the listen model at runtime. |
| `turn.params` | no | open map, forwarded verbatim | core | |

`placement` says where a **model** runs, not where the agent runs, and keeps its two values (N1). It is never written in the common case: it derives from `provider`. `provider: local` runs on your own machines, next to the agent worker; any other provider is a hosted API endpoint. A model definition may state `placement:` explicitly to override the derivation (rare: a self-hosted deployment of a vendor's stack). Running the agent on a laptop and deploying it later changes nothing: a hosted provider calls the vendor in both places. (`provider` is a first-class field, exactly as the old target bindings carried it — only the file moved, section 4.3. The `model` identity is whatever that provider's SDK expects, forwarded verbatim: `gpt-4.1-mini` for OpenAI, `slng/deepgram/nova:3-en` for SLNG.)

The two jobs declared placement used to do survive unchanged, per target and more precise than the old global declaration:

- `provider: local` on listen or on a speak model fails loudly on managed targets (Vapi, ElevenLabs) and on Deepgram (no slot for an outside STT), exactly as before. `provider: local` on a think model fails on managed targets too, with one exception: a documented custom LLM endpoint (ElevenLabs has one; Vapi unverified, fails there for now). On Deepgram a custom think endpoint is fine.
- placement is the main input to sizing: hosted providers make vendor quotas the limit; `local` GPU models make GPUs dominate cost. Sizing reads each target's effective models (section 6.2).

### 4.3 models (amended 2026-07-19, N15)

One unified map. Every model the package uses is defined here, once, fully and concretely; there is no separate `voices:` block and no per-target binding step (a target may *override* an entry, section 6.2, but never defines one). Identities, providers, and settings are forwarded to the provider as-is and never validated (D10): the provider API and the generated project are the real validators.

Every entry carries `provider` (the catalogue vendor) and `model` (the identity that vendor's SDK expects), exactly as the old target bindings did — this is the pairing that already worked, moved into `agent.yaml`. `provider: local` marks an on-machine model (section 4.2).

A model's kind follows from where it is referenced, and its fields are validated against that kind:

- a **speak model** is referenced from an agent's `voice:`.
- a **think model** is referenced from an agent's or task's `model:`, a transfer's `summarizer:`, or a fallback list.
- an entry that nothing references is an error: a declaration never silently does nothing (section 1).
- a name referenced but not defined here is an error naming the reference's file:line.

Speak model fields:

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `provider` | yes | catalogue vendor, for example `slng` | core | `local` marks an on-machine TTS (section 4.2). |
| `model` | yes | model identity forwarded to the provider, for example `slng/deepgram/aura:2-en` | core | May be omitted when the target's TTS engine is integrated (ElevenLabs) and the voice id alone selects it. |
| `voice` | yes | voice id, forwarded as-is | core | |
| `speed` | no, default `1.0` | number | warn | Lowered through the catalogue entry's documented slot; warned where the provider has none (verify per provider, section 9). |
| `language` | no, defaults to top-level `language` | BCP-47 tag | gated (N14) | Per-model override of the package language. |
| `params` | no | open map, forwarded verbatim | core | |
| `description` | no | text | core | For humans only. |

Think model fields:

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `provider` | yes | catalogue vendor, for example `openai` | core | `local` marks a self-hosted LLM (section 4.2). |
| `model` | yes | model identity forwarded to the provider, for example `gpt-4.1-mini` | core | |
| `temperature` | no | number | core | Verified slots on all five (section 9): Vapi `assistant.model.temperature`, ElevenLabs `conversation_config.agent.prompt.temperature`, Deepgram `agent.think.provider.temperature`, constructor kwargs on Pipecat and LiveKit. |
| `top_p`, `top_k` | no | number | warn | Lowered through the catalogue entry's documented slot; warned where the provider has none (verify per provider, section 9). |
| `params` | no | open map, forwarded verbatim | core | Anything else the bound component accepts (`max_tokens` where a slot exists; never forwarded to Deepgram, which has no max-tokens slot). |
| `description` | no | text | core | For humans only. |
| `fallback` | no | ordered list of think model names | gated | Cycle-checked. Every model in a chain must land in the same slot kind and placement on the resolved target. All five verified 2026-07-15. Deepgram: native (`agent.think` as an ordered provider array; mixed providers, per-entry params). LiveKit: native (`llm.FallbackAdapter`; STT/TTS adapters exist too). Pipecat: generated (the Pipecat driver v1 does not emit fallback yet — a maturity gate, not a platform limit; lifts when driver §T lands). ElevenLabs: native (`backup_llm_config.preference: override` with ordered `order`, `cascade_timeout_seconds` 2-15s); entries are model IDs only, so fallback models whose effective definitions carry settings beyond the ID warn there. Vapi: native (`model.fallbackModels`); entries are same-provider model IDs, so a **cross-provider chain fails on Vapi**; verified on OpenAI model schemas, others unverified. |

There is no `tier` field on models. Nothing would use it; Unmute never picks a model for you.

### 4.4 variables

Typed shared state. Task results, handoff payloads, and personalization all flow through it.

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `type` | yes | `string \| number \| boolean \| integer` | core | |
| `default` | no | value of that type | core | |
| `source` | no | `call_start` | core | Must be supplied when the call starts. Checked as satisfiable on outbound channels. |

Notes that drivers must respect: on ElevenLabs the only mid-call write path is a tool returning JSON. On Deepgram, live state lives in the generated bridge, never in template variables (those are substitution-time only and visible to project members; never route secrets through them).

### 4.5 agents

| Field | Required | Values | Tag |
|---|---|---|---|
| `instructions` | yes | path to a markdown file | core |
| `model` | yes | think model name (4.3) | core |
| `voice` | yes | speak model name (4.3) | core |
| `tools` | no | list of tool and control names | core |

Per-agent voices are native on LiveKit, Pipecat, and ElevenLabs, and work on all five.

### 4.6 tasks and task_groups (T1)

A `task` is delegate-and-return: control comes back to the owning agent with a typed result.

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `instructions` | yes | path | | |
| `tools` | no | list of names | | |
| `model` | no | think model name (4.3) | gated | Per-task override. **Fails on Pipecat** — a maturity gate, not a platform limit (runtime-verified 2026-07-16: an `LLMSwitcher` inside an `LLMWorker` pipeline stalls all flow frames on pipecat-ai 1.5.0, so the driver has no working lowering; driver-pipecat B7. Review-corrected 2026-07-15 the other way on docs alone — the spike overrode it). |
| `result` | yes | flat map: name to `string \| number \| boolean \| integer \| {enum: [a, b]}` | core shape | Nested schemas only when every configured target is a code target. |
| `context` | yes | transfer context block without `variables` (N12), `history` required | gated | See 4.7 and the history table. |

Tier support for `task` itself: native on LiveKit. Conditional on Pipecat (needs a cascaded pipeline; Flows ships inside core `pipecat-ai` as `pipecat.flows` since 1.5.0, and the standalone `pipecat-ai-flows` package is deprecated, never used). Conditional on ElevenLabs (a Workflow node; `assign` must route through a tool that returns JSON; only `history: full`). Pattern on Deepgram (generated, fine). **Unverified on Vapi**: it is not proven that a handoff can go back to the previous assistant, so single-task delegates fail there until verified.

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
| `requires` | no | list of variable names | gated | A machine-checked guard. Generated on code targets; **fails on Vapi and ElevenLabs** (no mechanism). On a failed guard the model gets a refusal naming the unmet variables; that behavior is part of the contract. |
| `context.history` | yes | `full \| messages \| last_n \| summary \| reset` | gated | See the history table below. |
| `context.max_messages` | iff `last_n` | int | | Illegal with any other value. |
| `context.summarizer` | iff `summary` is generated | think model name (4.3) | | So the model is defined and counted by sizing. |
| `context.include_tool_calls` | no, default `true` | bool | gated | `false` works on code targets only. |
| `context.variables` | yes | `all` or a list of names | gated | `all` is the only value managed targets accept. Lists compile on code targets only. |

Transfer history support per target (Vapi and ElevenLabs columns verified against provider docs 2026-07-15):

| Value | LiveKit | Pipecat | Vapi | ElevenLabs | Deepgram |
|---|---|---|---|---|---|
| `full` | ok | ok | ok | ok | ok |
| `messages` | ok | ok | ok | fails | ok |
| `last_n` | ok | ok | ok | fails | ok |
| `summary` | ok (generated) | ok (generated) | fails | fails | ok (generated) |
| `reset` | ok | ok | ok | fails | ok |

ElevenLabs always keeps the full transcript, for tasks too. `reset` never promises a literally empty context; on LiveKit a handoff marker still lands in the new context. The Pipecat driver v1 emits `history: full` only — the other values (and `context.variables` subset, `include_tool_calls: false`) are a maturity gate (§9); the workers handoff carries the running context.

Vapi lowering, literal spellings verified 2026-07-15: `contextEngineeringPlan` is one of `all` (their default; ours is no default, D7), `userAndAssistantMessages`, `lastNMessages` plus `maxMessages`, `none`; no summary mode exists; `previousAssistantMessages` stays unexposed. For tasks this table collapses: code targets support all five values (generated), ElevenLabs supports `full` only, and Vapi is n/a while single tasks fail there.

`kind: human_transfer`:

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `destination` | yes | symbolic name | core | Resolves through the target instance's `destinations:` map to a number or SIP URI. |
| `mode` | yes | `cold \| warm` | gated | `cold`: LiveKit native, Vapi native, ElevenLabs native, Pipecat only on Daily SIP transport, Deepgram carrier-conditional in the bridge. **`warm`** (review-corrected 2026-07-15): LiveKit native — stable on Node, `beta.workflows` on Python (NOT Python-only); **Pipecat ships it** (custom `TransferCoordinator` + hold music on Daily PSTN, official `warm_transfer.py`) but the driver does not emit it yet (a maturity gate, not "never implemented"); on Vapi the stable `transferPlan` path needs carrier Twilio. |
| `briefing` | no, warm only | `summary \| message \| wait` | gated | Vapi: all three. LiveKit: `summary`. ElevenLabs: `message`. Everywhere else: fails. |

### 4.8 conversation

Outcomes, not provider knobs. All lifecycle fields are gated per target.

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `greeting.speaks_first` | yes, if block present | `agent \| user` | `agent` core, `user` warn | `user` means the agent stays silent until the caller talks. Native on Vapi (`firstMessageMode: assistant-waits-for-user`) and ElevenLabs (empty `first_message`: "the agent waits for the user to start the discussion"), both verified 2026-07-15. Generated on LiveKit and Pipecat: no opening is emitted. **Warns on Deepgram**: behavior of an omitted `agent.greeting` is undocumented; the driver smoke test must prove silence. |
| `greeting.text` | no | text | core | Exact opening line, spoken word for word, every call. Verified 2026-07-15: Vapi `firstMessage`, ElevenLabs `conversation_config.agent.first_message`, Deepgram `agent.greeting`. Generated on LiveKit and Pipecat. May reference `{{variables}}` available at call start. |
| `interruption.enabled` | yes, if block present | bool | core | |
| `interruption.minimum_words` | no | int | warn | No word-count knob on ElevenLabs: warns. Lossy on Deepgram (model halts first): warns. |
| `interruption.ignore_phrases` | no | list of text | warn | Native on ElevenLabs and Vapi. Generated on LiveKit and Pipecat. Dropped with a warning on Deepgram. |
| `inactivity.nudge_after`, `inactivity.end_after` | no | durations | warn | Range-checked per target by the driver. |
| `max_duration` | no | duration | warn | Some providers have no cap knob; the driver gates and documents it. |
| `thinking_audio` | no | `none \| subtle` | gated | Native on LiveKit, Pipecat, ElevenLabs. **Fails on Deepgram and Vapi** (no faithful lowering). The Pipecat driver v1 does not emit thinking_audio yet (a maturity gate). |

The three greeting combinations:

- `speaks_first: agent` with `text`: fixed opening, same words every call. Works on all five.
- `speaks_first: agent` without `text`: the model writes the opening from the prompt, so it varies per call. Generated on LiveKit and Pipecat; native on Vapi (`firstMessageMode: assistant-speaks-first-with-model-generated-message`, verified 2026-07-15). **Conditional on ElevenLabs** (review-corrected 2026-07-15: a Workflow override-agent node's `entry_behavior: generate_immediately`, shipped 2026-06-15, produces a model-generated opening; requires wrapping the entry agent in a workflow node — a plain-agent greeting has no such toggle). **Generated with a warning on Deepgram** (review-corrected 2026-07-15: inject a synthetic turn at call start via `InjectUserMessage`/`InjectAgentMessage`, documented for orchestrated openings, though not framed as a first-class greeting mode).
- `speaks_first: user`: native on Vapi and ElevenLabs (verified 2026-07-15), generated on LiveKit and Pipecat, warns on Deepgram (omission behavior undocumented).

If the `greeting` block is absent, the target's own default applies and the driver warns, because provider defaults differ.

### 4.9 channels

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `kind` | yes | `realtime_audio \| telephony` | core | |
| `inbound`, `outbound` | yes, telephony only | bool | gated | `outbound: true` requires `on_voicemail` and all `source: call_start` variables satisfiable. Verified on LiveKit, Pipecat, Vapi, ElevenLabs (N6); the Pipecat driver v1 does not emit `outbound`/`on_voicemail` yet (a maturity gate); **generated with a warning on Deepgram** (review-corrected 2026-07-15: Deepgram ships an official AMD-bridge outbound reference impl — carrier AMD in the bridge — so the lowering is proven, carrier-conditional). |
| `required_controls` | no | list from the control vocabulary | gated | Vocabulary: `cold_transfer, warm_transfer, dtmf_send, dtmf_receive, hold, hangup, voicemail_detection, ivr_navigation`. Resolved against the target's carrier and transport, never the provider brand alone. |
| `on_voicemail` | iff `outbound: true` | `hangup \| leave_message` | gated | Both values verified 2026-07-15 on LiveKit (`AMD`), Pipecat (`VoicemailDetector`), Vapi (`voicemailDetection` + `voicemailMessage`), ElevenLabs (`voicemail_detection` system tool + `voicemail_message`). **Generated with a warning on Deepgram** (review-corrected 2026-07-15: official AMD-bridge reference impl covers both values, carrier-conditional). |

### 4.10 capacity

The declared half of the resource model. Required whenever `channels` has a telephony channel or the resolved target is a code target.

| Field | Required | Type | Notes |
|---|---|---|---|
| `peak_sessions` | yes | int | Concurrent conversations at busy hour. Must not exceed `max_sessions`. |
| `max_sessions` | yes | int | Hard admission limit. Reject or queue above it. |
| `avg_session_duration` | yes | duration | Sizing and quota input. |

Sizing depends on concurrency, placement, and channels. It does not depend on how many agents are in the file, and never on the provider brand alone. Derived numbers (workers, GPUs, quotas) are printed in the compile or plan report with dated benchmark coefficients, marked `unbenchmarked` until measured.

---

## 5. tools/*.yaml

The file name is the tool name (N4). Four parts plus a description. Which agents see the tool is decided only by their `tools:` lists in `agent.yaml`, never here.

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `description` | yes | text | core | What the LLM reads. |
| `input` | yes | JSON Schema object | core | Lowers natively everywhere (N10). |
| `output` | no | JSON Schema object | warn | Enforced by generated code on code targets. Managed targets have no slot for it: warns there. |
| `execution` | yes | `local \| client \| webhook \| provider_hosted \| builtin \| mcp` | see below | |
| `handler` | iff `execution: local` | path, default `<name>.py` | | Code targets only. |
| `url_env` | iff `execution: webhook` or `mcp` | env var name | core | Reference only, never a URL value. For `mcp` it names the MCP server address (driver-livekit B3, 2026-07-16: code targets have no other slot for it; managed targets may configure the server provider-side and ignore it). |
| `interruption` | no, default `provider_default` | `continue \| cancel \| provider_default` | warn | Honored on Pipecat (`cancel_on_interruption`); LiveKit runs tools to completion, so non-default values warn there (2026-07-16). On managed targets only `provider_default` means anything; other values warn. |
| `effect` | no, default `returns_data` | `returns_data \| ends_conversation` | core | |

Execution gating across the five:

- `webhook`: works everywhere. **This is the safe choice.**
- `local`: code targets only.
- `mcp`: **fails on Deepgram** (no runtime MCP client). On LiveKit it requires SDK language Python; code targets read the server address from `url_env` (B3, 2026-07-16).
- `client`, `provider_hosted`, `builtin`: gated per driver; each driver documents what it can host. Not part of the safe core yet.

The Pipecat driver v1 emits `webhook` and `local` tools (amended 2026-07-17, driver-pipecat T14: `local` lowers to the same `@tool` method, body awaiting the user handler from `tools/<name>.py`); `mcp` stays maturity-gated there until the driver emits it.

---

## 6. targets.yaml

Named target instances: which orchestrator runs the package, and the infrastructure facts that only make sense per target (amended 2026-07-19, N15 — model definitions moved to `agent.yaml` section 4.3; here they can only be overridden).

### 6.1 Instance fields

| Field | Required | Notes |
|---|---|---|
| `provider` | yes | `livekit \| pipecat \| vapi \| elevenlabs \| deepgram` for now. |
| `version` | code targets | Framework pin. The driver checks it against the range its templates support. A codegen check, not model validation. |
| `pins` | no | Independently versioned packages (LiveKit plugins) get their own entries. Pipecat Flows no longer qualifies: it ships inside `pipecat-ai` core since 1.5.0; never pin the deprecated standalone `pipecat-ai-flows`. |
| `sdk_language` | no | LiveKit: warm transfer and MCP need `python`. |
| `transport`, `carrier` | no | Driver vocabulary. Telephony controls resolve against these, never the brand alone. |
| `region`, `edition` | no | Provider vocabulary. Declared, never derived. |
| `models` | no | Per-target overrides, see 6.2. |
| `destinations` | if any `human_transfer` is used | Map of symbolic name to phone number or SIP URI. |

### 6.2 Overrides (amended 2026-07-19, N15)

The instance's `models:` map is optional and holds overrides only, for a target that cannot run an entry defined in `agent.yaml` (an SLNG provider on ElevenLabs, a `local` model on a managed target). Keys are the model names from `agent.yaml` section 4.3, plus `listen` and `turn` for the section 4.2 blocks. An override **replaces the whole entry** with the same shape and the same kind; there are no field-level merge rules to reason about. The effective model for a target is the override when present, the `agent.yaml` definition otherwise; every gate and sizing input below reads effective models.

Each role is **open** or **integrated** per target:

| Role | LiveKit | Pipecat | Vapi | ElevenLabs | Deepgram |
|---|---|---|---|---|---|
| `listen` | open | open | open | integrated (settings only, never an outside model) | open, Deepgram models only |
| `turn` | open | open | integrated | integrated | integrated, rides the listen entry's `params` |
| `speak` | open | open | open | open, ElevenLabs voices only | open, Deepgram plus a fixed third-party list |
| `think` (formerly `reason`) | open | open | open | open, supported list plus custom LLM endpoint | open, custom endpoints allowed |

Rules:

1. Every used model, and `listen` on every open-listen target, must have an effective definition. Without one there is nothing to emit; the error names the model and the target.
2. On a target whose role is integrated, the effective entry for that role carries settings for the built-in part only, and can never name an outside model.
3. Definitions and overrides carry their settings as typed fields (sections 4.2, 4.3) plus `params:`, an open map for the bound component's own remaining settings (audio format, turn thresholds where the provider puts them). Forwarded as-is, **never validated**. They configure only the bound component; platform and telephony settings can never ride through them.
4. Placement is derived from the effective entry's `provider` (`local` means `local`, anything else `api`; explicit `placement:` overrides) and gates per section 4.2.
5. If a driver has no slot for a value (a custom speak endpoint on ElevenLabs, a third-party listen model on Deepgram), compilation fails: the value has nowhere to go. That is a structural fact, not a judgment about the model.
6. Every forwarded model and param is listed in the compile or plan report, so what was sent is always inspectable. Some providers keep fields that do nothing, so run the agent to be sure. That is the contract.
7. An override naming a model that `agent.yaml` does not define, or changing its kind, is an error.

Why never validated: provider model lists change faster than any shipped catalog, the valid set on code targets depends on the pinned versions, and the real validators already exist (the provider API at plan/apply, the generated project at startup). Unmute relays those errors word for word.

---

## 7. The safe core: write this and it runs on all five

The subset that passes validation on every primary target. The example package in [examples/safe_core/](./examples/safe_core/) follows these rules exactly.

1. Any number of agents with `agent_transfer` between them (T0 + T2).
2. Every transfer context: `history: full`, `variables: all`.
3. Tools: `execution: webhook`, `interruption: provider_default`, `effect: returns_data`.
4. Human transfer: `mode: cold` only. Pipecat needs the Daily SIP transport; Deepgram needs a carrier in its target instance.
5. Hosted providers only for listen and speak models (no `provider: local`). `turn` is a preference anyway.
6. If the agent speaks first, give it a fixed `greeting.text`. A model-written opening is conditional on ElevenLabs (workflow node) and generated-with-warning on Deepgram (review-corrected 2026-07-15); a fixed line stays the zero-warning safe choice.
7. Skip for now: single `tasks` (return to owner unverified on Vapi) and `task_groups` with `then: return` (fails on Vapi). A `task_group` with `then: transfer` or `end` does pass on all five (warning on LiveKit: TaskGroup experimental). Also skip `requires`, `thinking_audio`, warm transfer, `mcp` and `local` tools, and any history other than `full`. `fallback` passes everywhere when the chain stays within one provider on Vapi and the fallback models carry no settings beyond the ID on ElevenLabs. `outbound: true` with `on_voicemail` passes everywhere; on Deepgram it is generated with a warning (review-corrected 2026-07-15), so keep it out of the zero-warning safe core if you want no warnings.
8. Accept warnings: `minimum_words` on ElevenLabs, interruption tuning on Deepgram, turn model notes.

Feature by feature:

| Feature | LiveKit | Pipecat | Vapi | ElevenLabs | Deepgram |
|---|---|---|---|---|---|
| single agent (T0) | ok | ok | ok | ok | ok |
| agent_transfer, `full` + `all` | ok | ok | ok | ok | ok |
| fixed opening line (`greeting.text`) | ok | ok | ok | ok | ok |
| model-written opening (no `text`) | ok | ok | ok | conditional (workflow node) | generated (warn) |
| user speaks first (`speaks_first: user`) | ok | ok | ok | ok | warn |
| task | ok | ok | unverified | conditional | ok |
| task_group, `then: transfer\|end` | warn | ok | ok | ok | ok |
| task_group, `then: return` | warn | ok | fail | ok | ok |
| history `messages` / `last_n` / `reset` | ok | gated (v1) | ok | fail | ok |
| history `summary` | ok | gated (v1) | fail | fail | ok |
| `requires:` | ok | ok | fail | fail | ok |
| `fallback:` | ok | gated (v1) | conditional | ok | ok |
| human_transfer cold | ok | Daily SIP only | ok | ok | carrier-conditional |
| human_transfer warm | native (Node stable, Python Beta) | ships, not emitted yet | Twilio only (stable path) | ok | carrier-conditional |
| `thinking_audio` | ok | gated (v1) | fail | ok | fail |
| `provider: local` (listen/speak) | ok | ok | fail | fail | fail |
| webhook tools | ok | ok | ok | ok | ok |
| mcp tools | Python only | gated (v1) | ok | ok | fail |
| outbound + `on_voicemail` | ok | gated (v1) | ok | ok | generated (warn) |

---

## 8. Not in v1

- Branches, routers, backtracking, any graph beyond linear task groups.
- Integrated speech-to-speech models (one model doing listen, think, and speak). Cascaded only in v1.
- Parallel conversational agents and supervisor logic as schema constructs.
- `merge: summary` and any automatic transfer summarization.
- Cross-session variable persistence and history retention toggles.
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
- Generation param slots: Vapi `assistant.model.temperature` and `maxTokens`; ElevenLabs `conversation_config.agent.prompt.temperature` (default 0) and `max_tokens`; Deepgram `agent.think.provider.temperature` (**no max-tokens slot exists**, do not forward one). Forwarded-verbatim stance unchanged.
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
| In-flight tool calls on barge-in, managed targets: Vapi and ElevenLabs docs both silent (ElevenLabs documents prevention via per-tool `interruption_mode`, not cancellation) | `interruption` | `provider_default` only |
| Vapi `fallbackModels` on non-OpenAI model schemas | `fallback` on Vapi | conditional, same-provider chains |
| Speak `speed` and think `top_p`/`top_k`: which providers document a slot (added 2026-07-19, N15; verify per catalogue entry before hardening) | `speed`, `top_p`, `top_k` | warn: lowered where the catalogue entry documents a slot, warned where none exists |

**Driver maturity gates (tags tightened until a driver emits the feature).** A code target may support a feature at the schema level while its first driver has not emitted the lowering yet. Like warm transfer (§4.7), these are gates on the driver, not the platform, and lift when the matching driver §T task lands:

| Driver | Gated until emitted | Where |
|---|---|---|
| Pipecat v1 | `models.fallback`, `thinking_audio`, `outbound` + `on_voicemail`, `mcp` tools, warm transfer; transfer/task context shaping beyond the safe-core defaults — `history` other than `full`, `context.variables` subset, `include_tool_calls: false` (the workers handoff carries the running context; fine-grained shaping is not emitted yet). (`local` tools lifted 2026-07-17, driver-pipecat T14.) | [docs/spec/driver-pipecat.md](docs/spec/driver-pipecat.md) §T. Emitted: single agent, `agent_transfer` (+ `requires` guard), `tasks`, `task_groups` with `context_scope` (shared/isolated), `then` return/transfer/end, `local` tools (2026-07-17). |

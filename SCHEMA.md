# Unmute schema, v1 (decided)

Status: locked, v1. Post-lock adversarial review (context7 + live provider docs) applied 2026-07-15, marked inline as "review-corrected": warm transfer on Pipecat/LiveKit, model-written opening on ElevenLabs/Deepgram, Deepgram outbound voicemail. These re-word or loosen gates; none changes the schema shape.
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
- **D2. The source describes what the agent should do, never provider settings.** No free-form option maps in `agent.yaml`. The one exception is `params` on bindings in `targets.yaml`: sent to the provider as-is, never checked.
- **D3. Fail loudly, never average.** If a target cannot honor a field, validation fails with a clear message in that provider's own words. No silent downgrades.
- **D4. The pattern rule.** On code targets, Unmute may generate a missing feature itself. On managed targets the same feature fails, because there is nowhere to host generated logic.
- **D5. Three tiers.** T0: one agent. T1: tasks and task groups. T2: agent handoff. A file may use any tier; validation checks it against the chosen target.
- **D6. Task groups are ordered lists, not graphs.** No branches, no going back, no routers in v1.
- **D7. Context is always explicit.** Every task and transfer says what history it carries. There is no default, because providers disagree about their own defaults.
- **D8. One name space.** Tools and controls share one set of names. What an agent or task can call is decided only by its `tools:` list, nowhere else.
- **D9. Flat typed data.** Variables and task results are flat maps of name to primitive type. That is the real common ground across the five providers.
- **D10. Abstract profiles, concrete bindings.** `agent.yaml` names model and voice profiles. `targets.yaml` binds them to real models per target. Identities and `params` are forwarded as-is and never validated; the provider API and the generated project are the real validators. Version pins replace validation on code targets.
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
- **N9.** `pipeline.turn` is optional in `agent.yaml`. Whether a turn binding is needed in `targets.yaml` follows the target's role table (section 6), not this block.
- **N10.** Tool `input` is a JSON Schema object. All five targets accept JSON Schema tool inputs, so nesting is allowed here. `output` has the same shape but is only enforced on code targets; managed targets have no place to check it.
- **N11.** `greeting` is a block, not a scalar: `speaks_first: agent | user` plus an optional `text`. With `text`, the agent opens with those exact words every call. Without it, the model writes the opening from the prompt. This replaces the scalar `greeting: agent_first | user_first` spelling in the source document, which could not express a fixed opening line.
- **N12.** Task `context` is the transfer context block without `variables`. Within one session the state store is already shared on all five primaries (LiveKit `userdata`, Pipecat flow state, Vapi Squad variables), so a task has nothing to filter; `context.variables` exists only on transfers.
- **N13.** The return path is part of the contract: when a task or a `then: return` group completes, the owner receives the typed result only; the task's conversation turns are not appended to the owner's context. This is LiveKit's native `AgentTask` behavior (verified against LiveKit docs 2026-07-15: a task starts with an empty chat context and its turns are not propagated back). `TaskGroup` is different, and `summarize_chat_ctx=False` alone does not honor this: a `TaskGroup` is itself an `AgentTask`, so when it completes LiveKit merges the group's turns into the owner's context on handoff regardless of that flag (verified against the livekit-agents source 2026-07-15: `voice/agent.py` merges `old_agent.chat_ctx` with the task context on completion; `summarize_chat_ctx` only controls a separate summarization pass, not the merge). To keep the group's turns out of the owner the driver must snapshot the owner's chat context before awaiting the group and restore it afterward, returning only the typed results; it still passes `summarize_chat_ctx=False` to skip the wasteful summarization call. Generated on Pipecat and Deepgram. On ElevenLabs the single running transcript makes task turns visible to the owner; the driver warns. Vapi is n/a while single tasks fail there.
- **N14.** `language` is the agent's primary spoken language, written as a BCP-47 tag and defaulting to `en`. It does not rewrite model routes. Pipecat and LiveKit lower it only through each catalogue entry's explicit `CallSpec.Language` slot: Pipecat official services place it in `Settings`, while SLNG and LiveKit plugins use constructor kwargs. An existing target `params.language` is an explicit per-integration override. ElevenLabs lowers it once to `conversation_config.agent.language`, which governs its integrated ASR and TTS. Vapi and Deepgram stay unavailable in `unmute init` until their generators ship; no generic parameter injection is invented for them. Verified against Pipecat, SLNG, LiveKit, and ElevenLabs provider docs 2026-07-16.

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
targets.yaml          # named target instances: provider, pins, bindings, destinations
```

Rules:

- Secrets and credentials never appear in any file. `targets.yaml` carries env var names and secret references only, never values.
- Remote IDs, regions, editions, SDK language, version pins, carriers, and concrete model names live in `targets.yaml`, never in `agent.yaml`.
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
| `pipeline` | yes | block, see 4.2 | core |
| `models` | yes, at least one | map of profiles, see 4.3 | core |
| `voices` | yes, at least one | map of profiles, see 4.3 | core |
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

### 4.2 pipeline

Roles, not services. A target may cover `listen` and `turn` with one model (Deepgram Flux, ElevenLabs built-in ASR) or with separate parts (Pipecat). The `reason` role does not appear here; its placement rides on the model profiles.

`placement` says where the **model** runs, not where the agent runs. `api` means the role calls a hosted vendor endpoint; `local` means the model runs on your own machines, next to the agent worker. Running the agent on a laptop in dev and deploying it later changes nothing here: `api` calls the vendor in both places. Dev versus prod lives in `targets.yaml` as separate target instances. The field exists for two reasons: `local` must fail loudly on managed targets (they cannot run your models), and placement is the main input to sizing (`api` means vendor quotas are the limit; `local` GPU models mean GPUs dominate cost).

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `listen.placement` | yes | `api \| local` | gated | `local` only on LiveKit and Pipecat. Fails on Vapi and ElevenLabs (managed) and on Deepgram (no slot for an outside STT). |
| `speak.placement` | yes | `api \| local` | gated | Same gating as `listen`. |
| `turn` | no | block | warn | On Vapi, ElevenLabs, and Deepgram, turn is built in, so `placement` is ignored with a warning. On LiveKit the full turn model is a Cloud feature, so it is a preference there too. Everywhere: a preference, not a promise. |
| `turn.placement` | yes, if block present | `api \| local` | warn | See above. |
| `turn.semantic_endpointing` | no | `required \| preferred \| off` | warn | Forwarded as a preference. Whether it really applies depends on the bound listen model at runtime. |

### 4.3 models and voices

Model profiles are abstract on purpose. Concrete names bind per target (section 6).

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `description` | no | text | core | For humans only. |
| `placement` | yes | `api \| local` | gated | `local` reason fails on managed targets, with one exception: a documented custom LLM endpoint (ElevenLabs has one; Vapi unverified, fails there for now). On Deepgram a custom reason endpoint is fine. |
| `fallback` | no | ordered list of profile names | gated | Cycle-checked. Every profile in a chain must land in the same slot kind and placement on the resolved target. All five verified 2026-07-15. Deepgram: native (`agent.think` as an ordered provider array; mixed providers, per-entry params). LiveKit: native (`llm.FallbackAdapter`; STT/TTS adapters exist too). Pipecat: generated (the Pipecat driver v1 does not emit fallback yet — a maturity gate, not a platform limit; lifts when driver §T lands). ElevenLabs: native (`backup_llm_config.preference: override` with ordered `order`, `cascade_timeout_seconds` 2-15s); entries are model IDs only, so fallback profiles whose bindings carry `params` warn there. Vapi: native (`model.fallbackModels`); entries are same-provider model IDs, so a **cross-provider chain fails on Vapi**; verified on OpenAI model schemas, others unverified. |

A voice profile carries only `description`. Same pattern: abstract here, bound per target as `speak.<profile>`.

There is no `tier` field on profiles. Nothing would use it; Unmute never picks a model for you.

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
| `model` | yes | model profile name | core |
| `voice` | yes | voice profile name | core |
| `tools` | no | list of tool and control names | core |

Per-agent voices are native on LiveKit, Pipecat, and ElevenLabs, and work on all five.

### 4.6 tasks and task_groups (T1)

A `task` is delegate-and-return: control comes back to the owning agent with a typed result.

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `instructions` | yes | path | | |
| `tools` | no | list of names | | |
| `model` | no | model profile name | gated | Per-task override. **Fails on Pipecat** — a maturity gate, not a platform limit (runtime-verified 2026-07-16: an `LLMSwitcher` inside an `LLMWorker` pipeline stalls all flow frames on pipecat-ai 1.5.0, so the driver has no working lowering; driver-pipecat B7. Review-corrected 2026-07-15 the other way on docs alone — the spike overrode it). |
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
| `context.summarizer` | iff `summary` is generated | model profile name | | So the model is declared, bound, and counted by sizing. |
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
| `url_env` | iff `execution: webhook` | env var name | core | Reference only, never a URL value. |
| `interruption` | no, default `provider_default` | `continue \| cancel \| provider_default` | warn | Honored on code targets. On managed targets only `provider_default` means anything; other values warn. |
| `effect` | no, default `returns_data` | `returns_data \| ends_conversation` | core | |

Execution gating across the five:

- `webhook`: works everywhere. **This is the safe choice.**
- `local`: code targets only.
- `mcp`: **fails on Deepgram** (no runtime MCP client). On LiveKit it requires SDK language Python.
- `client`, `provider_hosted`, `builtin`: gated per driver; each driver documents what it can host. Not part of the safe core yet.

The Pipecat driver v1 emits `webhook` tools only; `local` and `mcp` are maturity-gated there until the driver emits them.

---

## 6. targets.yaml

Named target instances. Everything provider-specific lives here.

### 6.1 Instance fields

| Field | Required | Notes |
|---|---|---|
| `provider` | yes | `livekit \| pipecat \| vapi \| elevenlabs \| deepgram` for now. |
| `version` | code targets | Framework pin. The driver checks it against the range its templates support. A codegen check, not model validation. |
| `pins` | no | Independently versioned packages (LiveKit plugins) get their own entries. Pipecat Flows no longer qualifies: it ships inside `pipecat-ai` core since 1.5.0; never pin the deprecated standalone `pipecat-ai-flows`. |
| `sdk_language` | no | LiveKit: warm transfer and MCP need `python`. |
| `transport`, `carrier` | no | Driver vocabulary. Telephony controls resolve against these, never the brand alone. |
| `region`, `edition` | no | Provider vocabulary. Declared, never derived. |
| `models` | yes | The binding block, see 6.2. |
| `destinations` | if any `human_transfer` is used | Map of symbolic name to phone number or SIP URI. |

### 6.2 Bindings

Grammar: a nested map, no dotted keys. `listen` and `turn` bind at role level (one block each). `reason` binds one block per model profile. `speak` binds one block per voice profile.

Each role is **open** or **integrated** per target:

| Role | LiveKit | Pipecat | Vapi | ElevenLabs | Deepgram |
|---|---|---|---|---|---|
| `listen` | open | open | open | integrated (settings only, never an outside model) | open, Deepgram models only |
| `turn` | open | open | integrated | integrated | integrated, rides the listen binding's `params` |
| `speak` | open | open | open | open, ElevenLabs voices only | open, Deepgram plus a fixed third-party list |
| `reason` | open | open | open | open, supported list plus custom LLM endpoint | open, custom endpoints allowed |

Rules:

1. Every open role in use, and every used model and voice profile, must have a binding. Without one there is nothing to emit.
2. An integrated role's binding is optional. When present, it carries settings for the built-in part only, and can never name an outside model.
3. A binding may carry `params:`, an open map for the bound component's own settings (temperature, audio format, turn thresholds where the provider puts them). Forwarded as-is, **never validated**. It configures only the bound component; platform and telephony settings can never ride through it.
4. Bindings must agree with the declared `placement`.
5. If a driver has no slot for a value (a custom `speak` endpoint on ElevenLabs, a third-party `listen` model on Deepgram), compilation fails: the value has nowhere to go. That is a structural fact, not a judgment about the model.
6. Every forwarded binding and param is listed in the compile or plan report, so what was sent is always inspectable. Some providers keep fields that do nothing, so run the agent to be sure. That is the contract.

Why never validated: provider model lists change faster than any shipped catalog, the valid set on code targets depends on the pinned versions, and the real validators already exist (the provider API at plan/apply, the generated project at startup). Unmute relays those errors word for word.

---

## 7. The safe core: write this and it runs on all five

The subset that passes validation on every primary target. The example package in [examples/safe_core/](./examples/safe_core/) follows these rules exactly.

1. Any number of agents with `agent_transfer` between them (T0 + T2).
2. Every transfer context: `history: full`, `variables: all`.
3. Tools: `execution: webhook`, `interruption: provider_default`, `effect: returns_data`.
4. Human transfer: `mode: cold` only. Pipecat needs the Daily SIP transport; Deepgram needs a carrier in its target instance.
5. Pipeline: `placement: api` for `listen` and `speak`. `turn` is a preference anyway.
6. If the agent speaks first, give it a fixed `greeting.text`. A model-written opening is conditional on ElevenLabs (workflow node) and generated-with-warning on Deepgram (review-corrected 2026-07-15); a fixed line stays the zero-warning safe choice.
7. Skip for now: single `tasks` (return to owner unverified on Vapi) and `task_groups` with `then: return` (fails on Vapi). A `task_group` with `then: transfer` or `end` does pass on all five (warning on LiveKit: TaskGroup experimental). Also skip `requires`, `thinking_audio`, warm transfer, `mcp` and `local` tools, and any history other than `full`. `fallback` passes everywhere when the chain stays within one provider on Vapi and the fallback profiles carry no extra binding params on ElevenLabs. `outbound: true` with `on_voicemail` passes everywhere; on Deepgram it is generated with a warning (review-corrected 2026-07-15), so keep it out of the zero-warning safe core if you want no warnings.
8. Accept warnings: `minimum_words` on ElevenLabs, interruption tuning on Deepgram, turn placement notes.

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
| `placement: local` (listen/speak) | ok | ok | fail | fail | fail |
| webhook tools | ok | ok | ok | ok | ok |
| mcp tools | Python only | gated (v1) | ok | ok | fail |
| outbound + `on_voicemail` | ok | gated (v1) | ok | ok | generated (warn) |

---

## 8. Not in v1

- Branches, routers, backtracking, any graph beyond linear task groups.
- Integrated speech-to-speech models (one model doing listen, reason, and speak). Cascaded only in v1.
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

**Driver maturity gates (tags tightened until a driver emits the feature).** A code target may support a feature at the schema level while its first driver has not emitted the lowering yet. Like warm transfer (§4.7), these are gates on the driver, not the platform, and lift when the matching driver §T task lands:

| Driver | Gated until emitted | Where |
|---|---|---|
| Pipecat v1 | `models.fallback`, `thinking_audio`, `outbound` + `on_voicemail`, `local` tools, `mcp` tools, warm transfer; transfer/task context shaping beyond the safe-core defaults — `history` other than `full`, `context.variables` subset, `include_tool_calls: false` (the workers handoff carries the running context; fine-grained shaping is not emitted yet) | [docs/spec/driver-pipecat.md](docs/spec/driver-pipecat.md) §T. Emitted: single agent, `agent_transfer` (+ `requires` guard), `tasks`, `task_groups` with `context_scope` (shared/isolated), per-task model, `then` return/transfer/end. |

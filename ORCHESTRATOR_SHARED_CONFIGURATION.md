# Shared configuration

Status: v2 draft. Round 1 and round 2 corrections from [ORCHESTRATOR_SHARED_CONFIGURATION_REVIEW.md](./ORCHESTRATOR_SHARED_CONFIGURATION_REVIEW.md) are incorporated. Items that still need verification are listed in section 9. 2026-07-15: corrections from the [SCHEMA.md](./SCHEMA.md) lock review applied (LiveKit TaskGroup summarization default, Vapi context-mode spellings verified, greeting became a block). SCHEMA.md holds the decisions; where the two disagree, that file wins.
Date: 2026-07-14, corrected 2026-07-15
Companion documents: [ORCHESTRATOR_CAPABILITY_PROFILES.md](./ORCHESTRATOR_CAPABILITY_PROFILES.md), [ORCHESTRATOR_CAPABILITY_ARCHITECTURE.md](./ORCHESTRATOR_CAPABILITY_ARCHITECTURE.md), [ORCHESTRATOR_COMPARISON_AND_DEPLOYMENT.md](./ORCHESTRATOR_COMPARISON_AND_DEPLOYMENT.md)

This document is the portable source contract: the shared configuration a user writes once and Unmute compiles per target. It answers three questions:

1. What is the widest common denominator that still covers single agents, tasks and task groups, and agent handoff, instead of stopping at trivial single-prompt bots?
2. Are task groups the same thing as graphs? No. See section 2.2.
3. What do deployment resources depend on, and how does the shared configuration express that without pulling infrastructure into the portable source? See section 5.

**Provider priority.** The schema is decided by five primary targets: **LiveKit, Pipecat, Vapi, ElevenLabs, Deepgram**. A field earns its place when the five primaries can honor its observable behavior, natively, conditionally, or through generated code. The four secondary providers (Cognigy, Voiceflow, Bolna, Vocode) are mapped best-effort where the same semantics hold, and they never shape the schema. Where a provider cannot honor a field, validation fails with a diagnostic; nothing is weakened to accommodate them.

**Terminology.** "Code targets" means project compilers (LiveKit, Pipecat) plus session bridges (Deepgram): everywhere Unmute owns generated code. "Managed targets" means the provider runs the runtime (Vapi, ElevenLabs, and the managed secondaries). Rules that used to say "project targets" mean code targets unless they explicitly exclude the bridge.

---

## 1. Design rules

1. **The source describes intent, never provider options.** Every field must have an observable conversational or operational meaning. No `options: map[string]any`. Concrete model and voice identities, and their generation `params`, are the deliberate exception, and they live outside this file: per-target bindings, forwarded verbatim to the provider and never validated (section 4.2).
2. **Capabilities are gated, not averaged.** A construct belongs in the shared schema when the five primary targets can honor its observable behavior. Unsupported combinations fail validation, and the diagnostic uses the provider's own vocabulary. A field that would silently do nothing on a target must fail or warn there; silent no-ops are never acceptable.
3. **The pattern rule.** A capability labeled Pattern in the research is compilable on code targets, because Unmute owns the generated code and can generate the pattern (for example a Deepgram handoff via `UpdateThink` or session replacement). On managed targets the same capability is a validation failure. The blocker is not that managed platforms cannot run customer code (Vapi Code tools, Voiceflow Function tools, and Cognigy Code Nodes all do): it is that none of them can host a persistent generated orchestrator loop, and the visual platforms have no documented API for authoring their artifacts programmatically. One exception cuts the other way: Vocode is a code target, but its runtime documents no mid-call instruction or tool mutation, so no handoff or task pattern is compilable there. Pattern requires both owning the code and a runtime that permits the mutation.
4. **Everything the LLM can invoke is declared once and referenced by name.** Tools, delegations, transfers, and human transfers share one namespace. Availability is decided solely by the `tools:` lists on agents and tasks; the tool file itself carries no scope. This mirrors how the primaries trigger controls: a Vapi Handoff tool, an ElevenLabs `transfer_to_agent` system tool, a LiveKit function tool. Two mechanical exceptions: ElevenLabs enters Workflow subagent nodes by edge transition rather than tool call, and Bolna transitions are chosen by a routing LLM after each user message. The controls still lower on both, but invocation timing is observably different there.
5. **Typed shared variables are the portability glue.** Every primary has a variable or state store (Vapi variables, ElevenLabs dynamic variables, LiveKit `userdata`, Pipecat `flow_manager.state`, and generated bridge state on Deepgram). Task results, handoff payloads, and personalization all flow through it. Two caveats the schema must respect: on ElevenLabs the only mid-call write path is a tool returning valid JSON, and Deepgram template variables are substitution-time only, so live state lives in the generated bridge, never in template variables.

---

## 2. The common denominator, decided

### 2.1 Three tiers, not one schema

| Tier | Constructs | Portability across the five primaries |
|---|---|---|
| **T0: single agent** | one agent: instructions, model roles, voices, variables, tools, knowledge refs, turn intent, channels | All five primaries and all four secondaries. Lifecycle fields (`inactivity`, `max_duration`, `greeting`) are gated per target, not universal; see section 4. |
| **T1: tasks and task groups** | delegate-and-return with a typed result; ordered task sequences with shared state and an explicit `then:` | Conditional on LiveKit (`TaskGroup` experimental), Pipecat (cascaded pipeline plus Flows), ElevenLabs (no native typed mid-call return; `assign` needs a tool-JSON write path), and Vapi (Squad chain; `then: return` impossible). Pattern on Deepgram. Fails on Vocode. |
| **T2: agent handoff** | permanent control transfer with an explicit context policy | **All five primaries support it**: native on LiveKit, Pipecat, Vapi, ElevenLabs; pattern on Deepgram via the generated bridge. Fails on Vocode; native on Cognigy and Voiceflow; conditional on Bolna. |

A source file may use any tier. Validation resolves the tier requirements against the chosen target and its conditions (edition, SDK, framework version, transport, carrier, maturity).

### 2.2 Task groups are not graphs

The tempting shortcut is to treat LiveKit `TaskGroup`, Pipecat Flows, ElevenLabs Workflows, Cognigy Flows, Voiceflow Workflows, and Bolna Graph Agents as one shared "graph" concept. The research shows they only share one observable shape: a linear sequence of focused steps with typed outputs. Everything beyond that diverges:

- edge semantics differ (Bolna has four edge kinds, ElevenLabs has forward and backward edges with retry, LiveKit `TaskGroup` is experimental as a whole, including its regression support);
- correction and backtracking exist in some providers and not in others;
- what happens after the last node differs: return to a controller, simply continue, or end;
- routers, conditions, and visual layout are provider specific.

So the shared configuration models the sequence, not the graph:

- `task` is delegate-and-return. Control comes back to the owning agent with a typed result. This is a LiveKit `AgentTask`, a Pipecat Flow node (or a `self.job()` dispatch), an ElevenLabs Workflow subagent node, a Cognigy Execute Workflow call, a Voiceflow Playbook, and a Bolna node with an extraction-bearing edge.
- `task_group` is an **ordered** list of tasks with shared state, typed results per step, and an explicit terminal action (`then:` plus `then_target:`). No conditional edges, no backtracking, no routers in v1.

Arbitrary graphs stay target native. If real use cases later prove shared semantics for branches and correction, a `graph` extension can be added. For the record, the correction evidence already spans four providers (LiveKit TaskGroup regression, ElevenLabs backward edges, Cognigy Slot Fillers, Bolna edge kinds), so that bar is closer than it first appeared; what is still missing is shared observable semantics. Never lower a portable `task_group` onto Vapi Workflows. They retire on 2026-08-18.

### 2.3 Concept to provider mapping

Support labels: **N**ative, **C**onditional, **P**attern, **No** means absent. Pattern is compilable only on code targets (rule 3). The first five columns are the primary targets. Superscripts point to the conditions list below, which is normative for validation.

| Concept | LiveKit | Pipecat | Vapi | ElevenLabs | Deepgram | Vocode | Cognigy | Voiceflow | Bolna |
|---|---|---|---|---|---|---|---|---|---|
| `agent` (T0) | N | N | N | N | N | N | N | N | N |
| `task` (T1) | N `AgentTask` | C Flow node¹ | C extraction² | C Workflow node³ | P phase session | No⁵ | N Execute Workflow | C Playbook⁶ | N node + edge⁷ |
| `task_group` (T1) | C `TaskGroup`⁰ | C linear Flow chain¹ | C Squad chain² | N linear Workflow | P | No⁵ | N Flow sequence | C | N linear graph⁷ |
| `agent_transfer` (T2) | N handoff | N worker activation | N Handoff tool² | N `transfer_to_agent`³ | P `UpdateThink` or session replacement⁴ | No⁵ | N AI Agent Handover | N Crew/Playbook | C graph transition⁷ |
| `human_transfer` | N cold / C warm⁰ | C cold, Daily SIP only; warm No¹ | N cold / C warm (Twilio)² | N (blind, conference, SIP REFER)³ | P at the bridge, carrier conditional⁴ | N cold / C warm⁵ | N cold and warm | N blind only⁶ | C undistinguished⁷ |
| `variables` | N `userdata` | N flow state¹ | N variables² | N dynamic vars³ | P bridge state⁴ | P (OSS) / C (hosted)⁵ | N Context | N variables | N `context_data`⁷ |

**Conditions (normative):**

⁰ **LiveKit.** `TaskGroup` is experimental as a whole and always shares context across its tasks, so `context_scope: isolated` cannot lower to it (it lowers to a generated sequence of standalone `AgentTask`s instead). TaskGroup also summarizes the group conversation into the owner's context by default (`summarize_chat_ctx=True`, verified 2026-07-15); the `merge: results` lowering must disable it. A standalone `AgentTask` starts with an empty chat context by default and does not propagate its turns back to the owner; the typed result is the only return. The prebuilt warm-transfer task is native — stable on Node (`workflows`), Beta on Python (`beta.workflows`) (review-corrected 2026-07-15: not Python-only; LiveKit's docs badge lags its shipped code); its consultation flow supports a spoken briefing, so `briefing: summary` lowers there. MCP is Python only. Adaptive interruption and the full turn-detector model are Cloud/Inference features; self-hosted deployments fall back to the mini model automatically, so turn `placement` is a preference, not a guarantee. The native fallback adapter is confirmed (`FallbackAdapter` for LLM, STT, and TTS, Python and Node, verified 2026-07-15). Outbound voicemail is covered by documented answering-machine detection (`AMD`, classifications include `machine-vm`; leave a message via `generate_reply`, then shut down).

¹ **Pipecat.** T1 requires Flows plus a cascaded pipeline; Flows ships inside the core `pipecat-ai` package as `pipecat.flows` since 1.5.0, and the standalone `pipecat-ai-flows` package (last release 1.4.0) is deprecated, never depend on it (rule 11). Flows does not run on integrated speech-to-speech models, which are excluded from v1 anyway (section 7). Delegate-and-return may lower to `self.job()` instead of a node round trip; that avoids synthesizing a hub edge for `then: return`. A per-task `model:` override lowers via `LLMSwitcher` (a FlowManager accepts an `LLMSwitcher` for per-node model switching, `llm_switching.py`), both standalone and inside a `task_group` step (review-corrected 2026-07-15: earlier claimed a Flow chain stays on one LLM service and gated it inside a group — false). Cold transfer exists only on Daily SIP; warm transfer ships in Pipecat (custom `TransferCoordinator` + hold music on Daily PSTN, official `warm_transfer.py`) but is not emitted by the driver yet (review-corrected 2026-07-15: "never implemented" was wrong); the WebSocket serializers (Twilio, Telnyx, Plivo) cannot transfer via the media-stream serializer (out-of-band carrier-REST transfer is possible, not emitted). `flow_manager.state` exists only under a FlowManager; for T0/T2-only sources Unmute generates a typed state store. Result validation is generated code, not a Flows primitive. Its native summary context strategy is deprecated; the generated summarizer must not use it. Custom OpenAI-compatible STT/TTS endpoints are a config forward: `OpenAISTTService` and `OpenAITTSService` accept a documented `base_url` override (verified 2026-07-15); subclassing the service base classes is only needed for other protocols. Outbound voicemail is a documented feature (`VoicemailDetector`, with leave-message and hang-up flows).

² **Vapi.** Typed extraction fires only as a side effect of a handoff, and values are flat primitives (String, Number, Boolean, Integer, plus enums). A `task_group` lowers to a Squad handoff chain with shared variables; `then: transfer` and `then: end` work, `then: return` has no mechanism and fails. Any T2 lowering must synthesize a Squad around the assistants; handoff does not exist for standalone Assistants. Warm transfer requires carrier Twilio; briefing styles are summary, custom message, or wait. `requires:` guards have no native mechanism and fail (the webhook-picked handoff destination is a possible future lowering, unverified). `fallback:` lowers natively to `model.fallbackModels` (verified 2026-07-15 on the OpenAI model schema); entries are same-provider model IDs, so cross-provider chains fail, and non-OpenAI model schemas are unverified. Voicemail is native: `voicemailDetection` (providers vapi, google, openai, twilio) plus `voicemailMessage`; message set means leave it, omitted means hang up. Context-mode spellings verified 2026-07-15 against the Handoff tool docs: `contextEngineeringPlan` is one of `all` (default), `userAndAssistantMessages`, `lastNMessages` (with `maxMessages`), `previousAssistantMessages`, `none`; no summary mode exists. A single-task delegate additionally needs a handoff back to the previously active assistant, which remains unverified; SCHEMA.md fails single tasks on Vapi until proven.

³ **ElevenLabs.** Focused steps are native Workflow nodes, entered by edge transition rather than tool call; there is no native typed mid-call result returned to an owner (Data collection is post-call), so a delegate with `assign:` is conditional on routing the result through a tool that returns JSON, the platform's only mid-call variable write path. Tasks (subagent nodes) always see the full running transcript, so task-level context policy other than `full` fails. Transfer keeps the full transcript, resets agent config (tools, voice, LLM), and hides the transfer tool calls; no general tool-call filtering exists. `requires:` guards have no native mechanism and fail. The `listen` and `turn` roles are provider-integrated: a `listen` binding lowers into the agent's `asr` settings block and can tune the built-in ASR, but can never name an outside STT model. Never lower to Procedures (Alpha, breaking changes expected). The custom LLM endpoint is the one placement exception on this managed target. Warm transfer's conference mode reads a message to the operator, so `briefing: message` is native; `summary` is not (no summarizer host). Verified 2026-07-15: LLM fallback is configurable via `backup_llm_config` (`preference: default | disabled | override`, ordered `order` list, `cascade_timeout_seconds` 2-15s); entries are model IDs with no per-entry params. An empty `first_message` is documented ("the agent waits for the user to start the discussion"), so user-speaks-first is native. A Workflow override-agent node's `entry_behavior: generate_immediately` (shipped 2026-06-15) produces a model-generated opening, so model-written-first is conditional there (review-corrected 2026-07-15: not impossible), requiring the entry agent to be wrapped in a workflow node. Voicemail is the `voicemail_detection` system tool with optional `voicemail_message` (absent means hang up immediately). Generation params live at `conversation_config.agent.prompt` (`temperature`, default 0, and `max_tokens`).

⁴ **Deepgram.** Turn detection is integrated in the listen model: the `turn` role coalesces into `listen`, and turn thresholds (`eot_threshold`, `eager_eot_threshold`, `eot_timeout_ms`) legitimately ride the **listen binding's** `params`, because that is where the provider locates them (`agent.listen.provider`). Whether semantic endpointing actually applies depends on the bound listen model at runtime (Flux integrates it; Nova falls back to silence endpointing); Unmute forwards and does not verify. The `listen` role accepts Deepgram models only; `speak` accepts Deepgram plus a fixed third-party list; a custom STT/TTS endpoint has no slot and fails structurally. Handoff lowers two ways: in-session `UpdateThink` plus `UpdatePrompt` (keeps full history natively, no summarizer needed) or session replacement with context replay. Telephony controls live in the generated bridge and resolve against the carrier, so they are carrier-conditional pattern, not absent. Template variables are substitution-time only and visible to project members; never route secrets or live state through them. Interruption tuning is lossy: the model halts natively before the bridge can count words. Verified 2026-07-15: `agent.think` accepts an ordered array of provider configurations as a native fallback chain (mixed providers, per-entry params); `agent.think.provider.temperature` exists but no max-tokens slot does; Reusable Agent Configurations exist and are immutable (delete and recreate to change), so the compile artifact stays inline `Settings`. The behavior of an omitted `agent.greeting` is undocumented: user-speaks-first warns until the driver proves silence.

⁵ **Vocode.** Needs an edition split. OSS is a code target, but the runtime documents no mid-call instruction or tool mutation, so T1/T2 fail even under the pattern rule. Warm transfer requires the hosted edition, Twilio, outbound calls only, Beta status, and a pre-provisioned steering pool. Hosted variables are a read-only template fill at call start (Beta). Actions work only with the ChatGPT-family agent class, which constrains the model profile of any agent with tools.

⁶ **Voiceflow.** `task` lowers to a Playbook (exit conditions, required variables); results are flat named variables, not a typed schema. `requires:` lowers natively to Playbook entry conditions, the one place it is native. Whether Playbooks and tools can be authored programmatically at all is unresolved (section 9); until confirmed, Voiceflow is a validate-and-report target, not a compile target. Human transfer is blind only, phone/SIP destinations only.

⁷ **Bolna.** Assessed on Bolna Cloud; whether Graph Agents exist in OSS is unverified. Transitions are picked by a routing LLM after each user message (no mid-turn transfer). Extraction lives on edges as a flat name-to-type map and materializes only on a successful transition. Lowering must leave the global `agent_information` prompt empty (it applies to every node) and must not collide with reserved underscore-prefixed variables like `_node_turns`. Human transfer takes a phone number, offers no summary handover, and never distinguishes warm from cold. DTMF is receive-only, on Plivo and Twilio.

### 2.4 Context policy

Every `task` and `agent_transfer` carries an explicit context block, and **`history` is required**; there is no schema default, because provider defaults disagree (LiveKit resets by default on both handoffs and tasks; most managed platforms carry everything). The five values mean:

- **`full`**: everything the target preserves for the conversation. Per-provider notes below define exactly what that includes, because it differs (ElevenLabs hides transfer tool calls; Voiceflow re-scopes instructions).
- **`messages`**: user and assistant turns only. No system instructions, no tool calls, no handoff events.
- **`last_n`**: the most recent `max_messages` user and assistant turns. `max_messages` is required with `last_n` and illegal with every other value.
- **`summary`**: a generated summary of the conversation so far. Requires `summarizer: <model profile>` wherever the summary is produced by generated code, so the model is declared, bound, and counted by sizing.
- **`reset`**: nothing carried except the provider's own transfer marker (on LiveKit an `AgentHandoff` item lands in the new context even on reset) and whatever `variables:` passes. Never a promise of a literally empty context.
- **`include_tool_calls`** (optional, default true): filters tool calls out of `full` or `last_n`. Honored on code targets only; fails on managed targets (ElevenLabs documents no tool-call filtering; on Vapi, `messages` already excludes tool calls natively).

Per-provider support:

| Provider | `full` | `messages` | `last_n` | `summary` | `reset` | Notes |
|---|---|---|---|---|---|---|
| LiveKit | yes | yes | yes | yes (generated, `summarizer` required) | yes | All generated via `ChatContext` construction. `reset` keeps the `AgentHandoff` marker. |
| Pipecat | yes | yes | yes | yes (generated, `summarizer` required) | yes | Generated on `LLMContext`. Native worker handoff passes history in full and injects a reason message; the generated code must account for it. |
| Vapi | yes | yes | yes | **fails** | yes | Native context-engineering modes, spellings verified 2026-07-15 (`all`, `userAndAssistantMessages`, `lastNMessages`, `none`). Vapi's `previousAssistantMessages` mode has no shared equivalent and stays unexposed. |
| ElevenLabs | yes | fails | fails | fails | fails | Transfer always keeps the full transcript. Applies to tasks too (subagent nodes see everything). |
| Deepgram | yes | yes | yes | yes (generated, `summarizer` required) | yes | Via `agent.context.messages` replay in the generated bridge, or natively-kept history with in-session `UpdateThink`. |
| Cognigy | yes | fails | fails | fails | unverified | Context object is inherited as-is on handover. |
| Voiceflow | yes | fails | fails | fails | fails | Never map `last_n` onto `vf_memory` (fixed at ten, prompt-side only). `full` carries history and variables but instructions re-scope per Playbook. |
| Bolna | yes | fails | fails | fails | fails | Nodes always receive full history. |
| Vocode | n/a | n/a | n/a | n/a | n/a | No transfer construct. |

Further context rules:

- `context.variables: all` is the only value managed targets accept; selective variable subsets across a transfer compile only on code targets.
- On task groups the field is **`context_scope: shared | isolated`** (a scalar, deliberately not named `context`). `isolated` compiles only on code targets (on LiveKit as standalone `AgentTask`s, not `TaskGroup`). The group's `context_scope` supersedes the member tasks' own `context` blocks while they run inside the group.
- `merge:` on task groups supports **`results` only**: on completion the owner receives the steps' typed results and nothing else. Correction (2026-07-15): LiveKit `TaskGroup` does summarize into the owner's context by default (`summarize_chat_ctx=True`), contrary to the earlier claim here, so the LiveKit lowering must disable it. `merge: summary` stays out anyway: it cannot be injected on any managed target and would imply an undeclared summarizer. If it ever ships, it is a generated step with a declared `summarizer` profile everywhere, native on LiveKit.

---

## 3. Package layout

A small filesystem package, inspired by Truss and Eve.

```text
agent.yaml            # this document's schema
instructions.md       # complete prompt for the entry agent
agents/               # instructions per additional agent (T2)
tasks/                # instructions per task (T1)
tools/
  lookup_customer.yaml  # schema + execution + interruption policy + effect
  lookup_customer.py    # handler, code targets only
targets.yaml          # named target instances: provider, version pins,
                      # model bindings, destinations (section 4.2)
```

Credentials and secrets never appear anywhere in the package, in any file. `targets.yaml` carries **references only**: environment variable names and secret-manager references, never values. Remote IDs, regions, editions, SDK language, framework and plugin version pins, carriers, and concrete model bindings are target concerns: they live in `targets.yaml`, never in `agent.yaml`. Machine sizes, replica counts, and GPU counts appear in neither file; they are derived and printed in reports (section 5.1).

---

## 4. The configuration schema

One annotated example first, then the blocks that need justification. Named maps instead of lists, so every item has stable identity and diffs stay readable. Duration values use Go duration syntax (`90s`, `15m`, `1h30m`).

```yaml
version: 1
entry_agent: intake

# Model roles (section 4.1). Roles, not services: a target may cover
# listen and turn with one integrated model (Deepgram Flux, ElevenLabs
# built-in ASR) or with separate components (Pipecat). The reason role
# does not appear here: its placement rides on the model profiles below.
# On integrated-turn targets, turn placement is ignored with a warning.
pipeline:
  listen: { placement: api }
  turn:   { placement: local, semantic_endpointing: preferred }  # required | preferred | off
  speak:  { placement: api }

models:                      # reasoning profiles agents and tasks reference.
                             # Abstract on purpose: concrete model names and
                             # generation params bind per target (section 4.2)
  fast_reasoning:
    description: cheap, low-latency routing and small talk   # optional, human only
    placement: api
    fallback: [careful_reasoning]   # ordered, cycle-checked; see section 4.1
  careful_reasoning:
    placement: api

voices:                      # voice profiles agents reference. Same pattern as
                             # models: abstract here, bound per target as
                             # speak.<profile> (section 4.2)
  front_desk: { description: warm, concise }
  specialist: { description: slower, more deliberate }

# Typed shared state (section 4.3)
variables:
  customer_id: { type: string }
  verified:    { type: boolean, default: false }
  order_total: { type: number }
  campaign_id: { type: string, source: call_start }   # must be supplied when
                                                      # the call starts

# Agents (T0/T2)
agents:
  intake:
    instructions: instructions.md
    model: fast_reasoning
    voice: front_desk
    tools: [lookup_customer, verify_identity, to_billing]

  billing:
    instructions: agents/billing.md
    model: careful_reasoning
    voice: specialist        # per-agent voices: native on LiveKit, Pipecat,
    tools: [get_invoice, refund_flow, to_human]   # ElevenLabs (per Agent resource)

# Tasks and task groups (T1)
tasks:
  identity_check:
    instructions: tasks/identity_check.md
    tools: [lookup_customer]
    model: fast_reasoning         # optional per-task override; forces job/
                                  # switcher lowering on Pipecat (condition 1)
    result:                       # flat name-to-type map (section 4.3);
      customer_id: string         # enums spell as {enum: [a, b]}
      verified: boolean
    context: { history: full }

  collect_refund_reason:
    instructions: tasks/refund_reason.md
    result: { reason: string }
    context: { history: full }

  confirm_refund:
    instructions: tasks/confirm_refund.md
    result: { confirmed: { enum: [yes, no, unsure] } }
    context: { history: full }

task_groups:
  refund_flow_steps:
    steps: [collect_refund_reason, confirm_refund]   # ordered, linear
    context_scope: shared      # shared | isolated (isolated: code targets only)
    then: return               # return | transfer | end; fails on Vapi (no
    # then_target: billing     # Squad return); required iff then: transfer
    merge: results             # results only (section 2.4)

# Controls: what the model can invoke besides plain tools
controls:
  verify_identity:
    kind: delegate
    task: identity_check
    assign:                    # result.<field> -> declared variable
      customer_id: result.customer_id
      verified: result.verified

  refund_flow:
    kind: delegate
    group: refund_flow_steps

  to_billing:
    kind: agent_transfer
    to: billing
    when: Caller asks about billing after identity is verified.
    # requires: [verified]     # machine-checked guard. Generated check on code
    #                          # targets; native only on Voiceflow (entry
    #                          # conditions); FAILS on Vapi and ElevenLabs.
    context:
      history: full            # REQUIRED: full | messages | last_n | summary | reset
      variables: all           # `all` is the only value on managed targets

  to_human:
    kind: human_transfer
    destination: billing_line  # symbolic; resolves via the target instance's
                               # destinations map to a number or SIP URI
    mode: cold                 # warm | cold; cold lets the target pick blind
                               # vs SIP REFER. Warm is gated (see 2.3).
    # briefing: summary        # summary | message | wait; warm only. Gating:
                               # Vapi all three, LiveKit summary, ElevenLabs
                               # message, Pipecat n/a (warm fails there).

# Tools (section 4.4). Bodies live in tools/*.yaml, referenced here.
# Availability is decided by the agents'/tasks' tools lists above.
tools: [lookup_customer, get_invoice]

# Conversation intent: outcomes, not provider knobs.
# All lifecycle fields here are gated per target (see 2.1 and section 6).
conversation:
  greeting:                    # block form per SCHEMA.md N11 (was a scalar here)
    speaks_first: agent        # agent | user; user is native on Vapi, generated
                               # on code targets, fails on ElevenLabs until proven
    text: Thanks for calling Acme! How can I help?   # optional fixed opening line
  interruption:
    enabled: true
    minimum_words: 2           # no word-count knob on ElevenLabs: warn
    ignore_phrases: [okay, right, uh-huh]   # native on ElevenLabs and Vapi;
                                            # generated on LiveKit/Pipecat;
                                            # warn-and-drop on Deepgram
  inactivity: { nudge_after: 15s, end_after: 45s }   # range-checked per target
  max_duration: 20m
  # thinking_audio: subtle     # none | subtle. Native on LiveKit, Pipecat,
                               # ElevenLabs; fails on Deepgram; no faithful
                               # Vapi lowering (backgroundSound is constant).

# Channels
channels:
  web:   { kind: realtime_audio }
  phone:
    kind: telephony
    inbound: true
    outbound: false
    # control vocabulary: cold_transfer, warm_transfer, dtmf_send,
    # dtmf_receive, hold, hangup, voicemail_detection, ivr_navigation
    required_controls: [cold_transfer, hangup]
    # when outbound: true, a voicemail policy is required:
    # on_voicemail: hangup | leave_message   (behaviors unverified; section 9)

# Capacity: the declared half of the resource model (section 5)
capacity:
  peak_sessions: 40            # concurrent conversations at busy hour
  max_sessions: 60             # hard admission limit; reject/queue above
  avg_session_duration: 6m     # sizing and quota estimation input
```

**Does this example compile on the five primaries?** The T0 and T2 core does. T1 is tier-gated by design. Honest per-primary outcome:

| Primary | Outcome for this example |
|---|---|
| LiveKit | Passes. Warnings: `TaskGroup` experimental; full turn model is a Cloud preference. |
| Pipecat | Passes on a Daily SIP transport (required by `cold_transfer`). Warnings: Flows pre-2.0; `identity_check`'s per-task model forces the job lowering. |
| Vapi | Fails on one line: `then: return` (Squads cannot return). Everything else passes. |
| ElevenLabs | T0/T2 pass. T1 is conditional: `assign:` must route through a tool-JSON return (condition 3). |
| Deepgram | Passes entirely as generated bridge. Turn thresholds ride the listen binding. |

The commented-out fields (`requires:`, `briefing:`, `thinking_audio:`) are real schema fields with per-target gating; they are commented here precisely because they do not pass everywhere.

### 4.1 Model roles, placement, and fallback

The architecture doc (section 8.1) already argues for roles (`listen`, `reason`, `speak`, `turn`) instead of hard STT/LLM/TTS blocks. Each pipeline role carries **`placement: api | local`**: the only infrastructure-adjacent fact in the portable source, because it changes conversational properties (latency, data residency) and is the main input to sizing (section 5). Which GPU, which region, which vendor account: all target concerns.

Placement rules:

1. `placement: local` fails on managed targets for **open** roles (see section 4.2 for open versus integrated), with one exception: `reason` may point at a customer endpoint where the provider documents a custom LLM (ElevenLabs Custom LLM; others unverified, section 9).
2. On **integrated** roles placement is ignored with a warning: Deepgram and ElevenLabs integrate turn detection (and ElevenLabs integrates ASR), and LiveKit's full turn model is a Cloud/Inference feature with automatic fallback to the mini model, so turn `placement` is a preference everywhere.

Fallback semantics (`fallback:` on a model profile):

- The list is ordered, flattened across chained profiles, and cycle-checked (rule 1 resolves the names).
- Fallback fires on request failure; precise trigger conditions (error classes, timeouts) are a per-driver contract, stated in the driver's docs.
- Every profile in a chain must lower into the same provider slot kind and the same placement on the resolved target. A native model and a custom-endpoint model cannot share one chain on a managed target.
- Per-primary support, all verified 2026-07-15: **Deepgram** native (`agent.think` as an ordered provider array; mixed providers, per-entry params). **ElevenLabs** native (`backup_llm_config.preference: override` with an ordered `order` list; entries carry no per-entry params, so fallback profiles with distinct binding params warn there). **Pipecat** generated (model switcher or `ParallelPipeline`). **LiveKit** native (`FallbackAdapter` for LLM, STT, TTS; Python and Node). **Vapi** native (`model.fallbackModels`; same-provider model IDs only, cross-provider chains fail).
- Any generated summarizer (`history: summary` on code targets) must also be a declared profile, so sizing and quota estimation see it.

### 4.2 Model bindings: forwarded, never validated

The portable profiles are abstract on purpose: the same source must compile to ElevenLabs (which only runs its own voices and its supported LLM list) and to Pipecat (which runs anything the pinned runtime accepts). Concrete model and voice identities live in named target instances in `targets.yaml`.

**Binding grammar.** `models:` is a nested map, no dotted keys: `listen` and `turn` bind at role level (one block each); `reason` binds one block per model profile; `speak` binds one block per voice profile. The target instance also carries `destinations:` (symbolic human-transfer destinations to numbers or SIP URIs) and version pins.

**Open versus integrated roles.** Each driver declares every role as one of:

- **open**: the binding is required and names a model (rule 12 completeness). LiveKit and Pipecat: all roles open. Vapi: listen, speak, reason open; turn integrated (endpointing is lowered from conversation intent, not bound). Deepgram: listen open but Deepgram models only, speak open within a fixed provider list, reason open including custom endpoints; turn integrated into listen.
- **integrated**: the provider supplies the component. The binding is **optional**; when present it may carry only that provider's own settings for the integrated component, and it can never name an outside model. ElevenLabs listen is the canonical case: the binding lowers into the agent's `asr` settings block. ElevenLabs and Deepgram turn are integrated likewise.

```yaml
targets:
  pipecat-dev:
    provider: pipecat
    version: "1.5.0"           # framework pin, checked against the driver's
                               # template-compatible range (a codegen check, not
                               # model validation). Flows ships in core as
                               # pipecat.flows since 1.5.0; never pin the
                               # deprecated standalone pipecat-ai-flows.
    models:
      listen: { provider: deepgram, model: nova-3 }
      turn:   { provider: local, model: smart-turn }
      speak:
        front_desk: { provider: slng, voice: nova-it, endpoint_env: SLNG_TTS_URL }
        specialist: { provider: slng, voice: marco,   endpoint_env: SLNG_TTS_URL }
      reason:
        fast_reasoning:
          provider: openai
          model: gpt-4o-mini
          params: { temperature: 0.4, top_p: 0.9 }   # forwarded verbatim
        careful_reasoning:
          provider: openai
          model: gpt-4o
    destinations:
      billing_line: "+14155550123"

  elevenlabs-prod:
    provider: elevenlabs
    models:
      # listen and turn are integrated on ElevenLabs. The optional listen
      # binding tunes the built-in ASR (lowers into the asr block); it can
      # never name an outside STT model.
      listen: { params: { user_input_audio_format: ulaw_8000 } }
      speak:
        front_desk: { voice_id: "pNIn..." }
        specialist: { voice_id: "EXAV..." }
      reason:
        fast_reasoning:
          model: gemini-2.5-flash
          params: { temperature: 0.3 }               # tuned per binding, since
        careful_reasoning:                           # the model differs per target
          model: claude-sonnet-4-5
    destinations:
      billing_line: "+14155550123"
```

A binding may carry an open `params:` map for settings of the bound component (temperature, top_p, max tokens, an ASR audio format, whatever the bound model takes). Params live on the binding, not the profile, because they tune the concrete model. Two guardrails keep `params` from becoming the escape hatch rule 1 forbids: it is scoped strictly to the bound component's own config surface (telephony and platform settings can never ride through it; turn-taking thresholds ride it exactly where the provider locates them inside the bound model's config, as with Deepgram's `eot_threshold` in the listen provider block), and it exists only in `targets.yaml`, so the ban on `map[string]any` in the portable IR stays intact. The target-config Go structs carry exactly one `map[string]any` field for it, documented as the deliberate exception. A profile itself carries only its name, an optional `description`, `placement`, and `fallback`; there is no `tier` field, because nothing would consume it (Unmute never picks a model for you).

The stance on validation is deliberate: **model identities and their `params` are forwarded verbatim and never validated.** No catalogs, no allowlists, no name or parameter checks, on any target, including the code targets. Three reasons, and one honest limit:

1. Provider model lists change faster than any shipped catalog. A stale allowlist makes the CLI wrong, not safe.
2. On the code targets the "valid" set is a function of the pinned framework and plugin versions. A name check without running that exact code is false confidence.
3. The real validators already exist. The provider API rejects a bad name at `plan`/`apply`, and the generated project fails at startup on code targets. Unmute relays those errors verbatim.
4. The honest limit: some providers accept and retain fields that do nothing (ElevenLabs documents at least one inert config field), so a forwarded param can be silently ignored rather than rejected. Unmute therefore lists every forwarded binding and param in the compile/plan report, so what was sent is always inspectable, but it never judges the values. Users run the agent to find out; that is the contract.

What the compiler still requires is completeness and structure, not validation: every open role and every used profile must have a binding in the resolved target (without one there is nothing to emit); an integrated role's binding, if present, carries settings only; the binding must agree with the declared `placement`. If a driver has no slot to forward a binding into (a custom `speak` endpoint on ElevenLabs, a third-party `listen` model on Deepgram), compilation fails because the value has nowhere to go. That is a structural fact, not a judgment about the model.

Version pinning replaces validation on code targets, with two precisions from review: independently versioned packages (LiveKit plugins; Pipecat Flows no longer qualifies, it ships in `pipecat-ai` core since 1.5.0) need their own entries in `pins:`, and the driver checks the pins against the range its templates were written for, because generated code is only valid within a framework API range (a codegen compatibility check, not model validation). The generated project pins everything in its dependency file. Deterministic output, zero opinions about model names.

**SLNG appears in both positions, with different confidence levels.** As a target, bindings name SLNG models directly and the SLNG control plane is the validator on `apply`. As a model vendor inside other targets: SLNG STT/TTS is a **first-class integration on both code targets** (corrected 2026-07-15, catalogue pilot: `pipecat-slng` in Pipecat's supported-services matrix, `livekit-plugins-slng` on LiveKit — see PROVIDER_CATALOG.md), so the OpenAI-compatible `base_url` override on `OpenAISTTService`/`OpenAITTSService` now serves generic custom endpoints, not SLNG. SLNG as a **reason** model stays the OpenAI-compatible endpoint route (documented on Deepgram Think; the Pipecat wildcard entry covers it). Deepgram's listen/speak stay locked to its own and a fixed list, and Vapi's custom-model surfaces remain undocumented.

**Managed reconcilers and forwarded fields.** A driver diffs the fields it models normally, and compares forwarded `params` opaquely (byte-equal after normalization it defines). Provider APIs that normalize or default stored config can cause perpetual plan drift on forwarded fields; each managed driver must document its comparison rule, and ElevenLabs' config branching (which branch `apply` targets) is an open item (section 9).

### 4.3 Variables are first class

Task `assign` writes results into variables using `result.<field>` paths, the only path grammar. Transfer `context.variables` decides what crosses a handoff (always `all` on managed targets). Instructions may reference variables (`{{customer_id}}`). Two shape rules keep this honest:

1. **Task `result` declarations are flat name-to-type maps in the portable core** (string, number, boolean, integer, plus enums spelled `{enum: [a, b]}`). That is the real cross-provider denominator: Vapi extraction, Bolna edge parameters, and Voiceflow Playbook variables all confirm flat primitives. Nested JSON Schema results compile only when every configured target is a code target, where Unmute generates the validation code.
2. `source: call_start` marks variables that must be supplied when a call starts (Vapi assistant overrides, ElevenLabs dynamic variables at session start, bridge-injected values on Deepgram, generated entrypoint parameters on LiveKit and Pipecat). Validation requires them satisfiable on outbound channels.

Write-path caveats from section 2.3 apply: ElevenLabs writes only via tool JSON returns; Deepgram live state lives in the generated bridge. Underscore-prefixed names are reserved (Bolna auto-populates `_node_turns` and friends). Cross-session persistence (Voiceflow per-user variables, Cognigy Contact Profile) is deliberately excluded; see section 7.

### 4.4 Tool contract

Each `tools/*.yaml` declares four parts: input and output schema, execution location (`local | client | webhook | provider_hosted | builtin | mcp`), interruption policy (`continue | cancel | provider_default`), and control effect (`returns_data | ends_conversation`). Availability scope is **not** one of them: which agents and tasks see a tool is decided solely by their `tools:` lists in `agent.yaml` (one authoritative place, rule 4). Controls with the effects `delegate`, `agent_transfer`, and `human_transfer` live in `controls:` instead. A control's `when:` is the LLM-facing trigger description, lowered into the tool or edge description on each target.

Review-driven gating:

- `execution: mcp` fails on Deepgram, Vocode, and Bolna (no runtime MCP client); on LiveKit it requires SDK language Python; on Voiceflow only remote MCP servers work.
- Authored **output** schemas are enforced by generated code on code targets; managed targets have no native slot for them (tool descriptions and input schemas lower natively, output validation does not).
- The interruption policy is honored on code targets, where generated code controls cancellation. On managed targets only `provider_default` is meaningful; other values warn. Almost no provider documents in-flight tool behavior on barge-in.
- On Cognigy a tool lowers by branch synthesis: a child Node whose logic is a generated Flow branch, not a declared function.

### 4.5 Departures from the profiles section 6.2 candidate shape

1. **Added `task_groups`** with `steps`, `then` plus `then_target`, and `merge`. `then:` exists because graph-shaped providers have no implicit return to the controller: `return` lowers to a `TaskGroup`, a Pipecat `self.job()` round trip, or a hub edge; `transfer` chains into a handoff; `end` hangs up gracefully.
2. **Added `pipeline`, `models`, and `voices` with `placement` and `fallback`.** Voice profiles mirror model profiles so multi-agent sources can give agents distinct voices, which LiveKit, Pipecat, and ElevenLabs all support natively.
3. **Added `capacity`.** Traffic facts describe the use case, not the infrastructure.
4. **Renamed `execution:` to `conversation:`**, and the task-group context field to `context_scope:` (the same name with two types was an IR hazard).
5. **Flattened task `result` to a name-to-type map** (was full JSON Schema) after review showed flat primitives are the actual managed-target denominator.
6. **Added `requires:` guards, `briefing:` on human transfer, `source: call_start` on variables, `thinking_audio`, and the voicemail policy.** Each is gated per target under rule 2; none is universal, and the flagship example comments out the ones that fail on any primary.

---

## 5. Resources: what model deployments depend on

"How much compute does this agent need" has no provider-independent answer, but it has a provider-independent dependency structure. Five axes decide everything:

| Axis | Question | Effect on resources |
|---|---|---|
| **1. Target mode** | Who runs the agent runtime? | Managed reconcilers (Vapi, ElevenLabs, and the secondaries Voiceflow, Cognigy SaaS, Bolna Cloud): customer compute is the webhook tool servers, when webhook tools are used at all. Project compilers (LiveKit, Pipecat): the customer runs agent workers, or a provider cloud (LiveKit Cloud Agents, Pipecat Cloud) runs the same compiled project as managed hosting. Session bridges (Deepgram): the customer runs the bridge. Self-hosting a provider stack itself (LiveKit server, Deepgram engines) is a separate and much larger commitment. |
| **2. Model placement per role** | Does each pipeline role call a vendor API or run locally? | `api`: the worker is I/O bound and CPU light; the scarce resources become vendor rate limits, quotas, and per-minute cost, not your CPU. `local` CPU-class models (Silero VAD, small turn detectors): a small per-session CPU cost that sets worker density. `local` GPU-class models (self-hosted Deepgram engines, any local LLM): GPUs scale with concurrent streams and become the dominant cost. |
| **3. Concurrency** | How many conversations at once? | Everything scales roughly linearly with peak concurrent sessions. Not registered agents, not total daily calls. Guidance for choosing the declared number: `peak_sessions = calls_per_hour * avg_session_duration / 3600 * peak_factor` (`calls_per_hour` and `peak_factor` are planning inputs, not schema fields). |
| **4. Media and telephony ownership** | Who terminates audio? | Self-hosted WebRTC needs an SFU plus TURN capacity plan. WebSocket telephony bridges need one long-lived connection plus codec work per call. SIP trunks need as many concurrent channels as peak phone sessions. Managed media makes all of this the provider's problem. |
| **5. Per-session audio work** | What runs on the worker per stream? | Resampling, codecs, VAD, denoising, turn models. This sets `sessions_per_worker`, the packing density, and it is measured, never assumed. |

### 5.1 Declared versus derived: the design decision

The shared configuration declares only what the operator actually knows:

- traffic: `capacity.peak_sessions`, `max_sessions`, `avg_session_duration`;
- placement: `placement: api | local` per pipeline role;
- the channels in use.

Everything else is derived per target and printed in the compile or plan report. It is never stored in the portable source, because worker shapes, GPU types, and quota mechanics are target vocabulary:

```text
sizing (pipecat, self-hosted, transport: daily-sip):
  workers:            4 replicas          = ceil(40 peak / 12 sessions_per_worker)
  worker envelope:    2 vCPU / 4 GiB      (benchmark coefficient, pipecat@1.x, 2026-07)
  gpus:               none                (all roles placement: api except turn: local cpu)
  vendor quotas:      llm  at least 40 concurrent streams, about 480 req/min at peak
                      stt  at least 40 concurrent websockets
  telephony:          Daily SIP must carry 40 concurrent channels inbound
  admission:          reject at 60 sessions (max_sessions)
```

The coefficients (`sessions_per_worker`, `streams_per_gpu`) live in a per-target benchmark table maintained with the target driver, versioned and dated. They are measurements, not schema. Until a target has real measurements, the report prints the formula with the coefficient marked `unbenchmarked` instead of inventing a number.

For managed targets the same `capacity` block still matters: it sizes the customer-side pieces that remain (webhook tool servers must handle `peak_sessions * tool_calls_per_session` requests) and should check provider concurrency limits, though no provider research yet documents those limits (section 9).

### 5.2 What sizing does not depend on (common traps)

- **The number of agents and tasks in the file.** Ten agents in a T2 handoff chain still serve one conversation per session. Only concurrency matters.
- **The provider brand alone.** "Pipecat needs X" is unanswerable. Pipecat with Daily WebRTC and all-API models is a different system from Pipecat with Twilio WebSocket and local STT.
- **Total call volume.** 10,000 short calls spread over a day can need fewer resources than 200 long overlapping ones.

### 5.3 Resource ownership per provider (condensed)

Rows marked "verify" have conflicting sources; see the review, section 4. Do not rely on those cells until re-checked against official docs.

| Provider | Customer always deploys | Customer optionally deploys | GPU question |
|---|---|---|---|
| LiveKit | agent workers, or LiveKit Cloud Agents runs them | LiveKit server, SIP, TURN | only for local models; agent workers run on CPU |
| Pipecat | bot workers, or Pipecat Cloud runs them | everything except chosen SaaS transports and models | same |
| Vapi | webhook servers only for webhook tools (Code and Integration tools run on Vapi) | enterprise on-premise reported in one source only (verify) | none (managed) |
| ElevenLabs | webhook tool endpoints; client tools live in the client app | custom LLM endpoint, remote MCP servers; private deployment (sources conflict, verify) | none in normal use |
| Deepgram | media and telephony bridge | full engine stack (self-host exists; GPU and licensing details from earlier research only, verify) | reportedly NVIDIA GPUs for self-hosted engines (verify) |
| Vocode | OSS: full Python service (Redis only for the telephony config manager). Hosted: external action endpoints only | nothing | only for local models |
| Cognigy | integrations, carrier | full platform on-premises (availability conflicts between sources, verify) | provider dependent |
| Voiceflow | nothing required (API, Function, Integration, and remote MCP tools all run on Voiceflow); customer hosts only backing APIs | nothing | none (managed) |
| Bolna | tools and integrations | OSS service; enterprise on-prem reported in earlier research only (verify) | edition dependent |

---

## 6. Validation rules

1. Every referenced name (`tools`, `controls`, `task`, `group`, `to`, `then_target`, `model`, `voice`, `fallback`, `requires`, `summarizer`, `destination`) must resolve. Names are unique across the shared tool and control namespace. Underscore-prefixed variable names are reserved. Fallback chains are flattened and cycle-checked.
2. Every `task` declares a `result` as a flat name-to-type map (primitives plus enums); every `assign` path is `result.<field>`, must exist in that map, and must target a declared variable of a compatible type. Nested result schemas validate only when every configured target is a code target. Post-call analysis features (Vapi Structured Outputs) are never a lowering for `result`.
3. A `task_group` is a non-empty ordered list. `then:` is one of `return | transfer | end`; `then_target:` is required iff `then: transfer` and must name a declared agent. `then: return` fails on Vapi (no Squad return mechanism). No cycles are possible because there are no edges.
4. Tier gating per resolved target uses the section 2.3 matrix **including its conditions list**, which is normative. Pattern-level support passes on code targets and fails on managed targets (rule 3 in section 1), with the Vocode exception (code target, no mid-call mutation, T1/T2 fail).
5. `context.history` is required on every task and transfer context block and validates against the section 2.4 table and definitions: `max_messages` only with `last_n`; `summary` requires `summarizer:` wherever it is generated; `include_tool_calls` and `variables:` subsets and `context_scope: isolated` compile only on code targets. Task context is gated, not just transfer context.
6. `requires:` guards lower natively where a guard mechanism exists (Voiceflow entry conditions), as generated checks on code targets, and **fail on managed targets without one** (Vapi, ElevenLabs). The generated-check behavior on a failed guard (the model receives a refusal result naming the unmet variables) is part of the portable contract.
7. Telephony controls resolve against the matrix for the resolved carrier and transport, never the provider alone. The control vocabulary is `cold_transfer`, `warm_transfer`, `dtmf_send`, `dtmf_receive`, `hold`, `hangup`, `voicemail_detection`, `ivr_navigation`; send and receive are independent capabilities everywhere. `human_transfer` destinations must resolve through the target instance's `destinations:` map to a phone number or SIP URI. `briefing:` values are `summary | message | wait`, gated per provider: Vapi supports all three; LiveKit supports `summary` (consultation flow); ElevenLabs supports `message` (conference mode); elsewhere briefing fails. Warm-transfer conditions per provider come from the section 2.3 conditions list. Whether Bolna's undistinguished transfer satisfies `mode: cold` is decided per target driver and documented in its diagnostics, not assumed.
8. `placement: local` fails on managed targets for open roles (exception: `reason` via a documented custom-LLM endpoint). On integrated roles, `placement` and `semantic_endpointing` are lowered as preferences: the driver forwards the relevant settings and warns that actual behavior depends on the bound model at runtime. `outbound: true` requires an `on_voicemail` policy and all `source: call_start` variables satisfiable; `on_voicemail` fails on targets without verified voicemail support (section 9).
9. `capacity.peak_sessions` must not exceed `max_sessions`. Both are required whenever `channels.phone` or a code target is configured.
10. Conversation lifecycle fields are gated and range-checked per target (Voiceflow's inactivity timeout caps at 300 seconds; several providers document no `max_duration` at all). `minimum_words` warns on ElevenLabs; `ignore_phrases` fails on targets with neither a native phrase list nor generated code; interruption fields warn as lossy on Deepgram; `thinking_audio` fails on Deepgram and Vapi (no faithful lowering) and is native on LiveKit, Pipecat, ElevenLabs.
11. Never lower to deprecated or retiring provider surfaces: Vapi Workflows (retire 2026-08-18), Cognigy AudioCodes and Generic Voice Nodes (removal around Q2 2026), Pipecat's deprecated summary context strategy, ElevenLabs Procedures (Alpha), and the standalone `pipecat-ai-flows` package (Flows lives in `pipecat-ai` core since 1.5.0).
12. Model bindings are structure-checked only: every open role and every used model/voice profile must have a binding in the resolved target; integrated roles may carry a settings-only binding or none; bindings must agree with `placement`; `params` may only configure the bound component. Identities and `params` values are **never validated**; provider API and runtime errors are relayed verbatim, and every forwarded binding appears in the compile/plan report (section 4.2). Code targets pin framework and independently versioned packages (`version:` plus `pins:`), and the driver checks the pins against its template-compatible range; that is a codegen compatibility check, not model validation.
13. Warnings (stderr, exit 0) for experimental and beta lowerings: LiveKit `TaskGroup`, LiveKit warm-transfer task (Beta, Python only), fallback profiles carrying binding `params` on ElevenLabs (no per-entry param slot), Bolna BYO SIP trunks (Beta; keyed on the resolved carrier being a BYO trunk), Vocode hosted Beta surface, and `unbenchmarked` sizing coefficients.

---

## 7. Deliberately excluded from v1

- Conditional edges, routers, backtracking, and any graph beyond linear task groups (section 2.2).
- **Integrated speech-to-speech models.** The pipeline contract is cascaded-only in v1; a model covering listen, reason, and speak together has no binding form and stays target native. (The architecture doc section 8.1 asks the IR to support integrated models eventually; that requirement is deferred, not contradicted: a coalesced-binding form is the likely future shape.)
- Parallel conversational agents. Pipecat parallel mechanisms (job groups, `ParallelPipeline`, distributed workers over the bus) stay target native, except `ParallelPipeline` reused internally as a fallback implementation.
- Supervisor logic as a schema construct. It compiles to generated code where users write it, or it does not exist.
- `merge: summary` on task groups and any automatic transfer summarization (section 2.4).
- Cross-session variable persistence (Voiceflow per-user variables, Cognigy Contact Profile) and session history retention toggles (Deepgram `settings.flags.history`).
- Force-interrupt phrase lists (Vapi `interruptionPhrases`, Bolna stopwords), conversation pacing (Vocode `speed_coefficient`), static/canned audio steps (Bolna static nodes), and external mid-call event injection (Bolna `POST /events`, Deepgram `InjectUserMessage` as an external API). All target native.
- Vapi's previous-assistant-only context mode (no shared equivalent).
- WhatsApp and other messaging channels, batch outbound campaigns (ElevenLabs), IVR navigation as a portable behavior (the control name exists for matrix resolution only).
- Machine sizes, replica counts, GPU types, and GPU counts anywhere in the package. These are derived per target (section 5.1). Regions and editions are declared target config in `targets.yaml`, not derived and not portable.
- Automatic telephony provisioning. Carriers and trunks are target configuration.

---

## 8. Open questions

1. **Deepgram compile artifact: resolved 2026-07-15.** Reusable Agent Configurations exist and are immutable (delete and recreate to change; the UUID replaces the `agent` object in Settings). Decision: emit inline `Settings` JSON; immutability makes reusable configs create-per-change churn with no compile-time benefit.
2. **Vapi single `task` lowering**: even if a Squad round trip (A to B and back to A) works, result fidelity caps at flat primitives, which the schema now assumes anyway. The remaining unknown is whether a handoff can target a previously active assistant at all; verify against the Handoff API before implementing `then: return` alternatives.
3. Should `placement` allow `edge` or `on_prem` refinements, or is `api | local` enough until a target actually distinguishes them? Lean: it's enough.
4. Where do the benchmark coefficient tables live: in the target driver package, or in a versioned data file users can override? Lean: driver package, no override until someone needs it.
5. Does `capacity` belong in `agent.yaml` or in a sibling file? Traffic is use-case knowledge, but multi-environment setups will want different numbers. Likely the first field to move into named target instances when those arrive.
6. **ElevenLabs reconciliation**: which config branch does `apply` target, and how are dashboard-authored fields outside Unmute's model preserved? Blocking for the managed driver, not for the schema.
7. **Voiceflow (secondary)**: is there any programmatic authoring path for Playbooks, Workflows, and tools? If not, Voiceflow stays validate-and-report and its matrix column is informational only.

---

## 9. Unverified items

Claims that survive in this document on one source only, or none. Verify against current official docs before the IR or a target driver encodes them. Full detail in the review, sections 4, 5, and 7.

1. Vapi context-mode enum spellings: **resolved 2026-07-15.** Verified against the Handoff tool docs: `all` (default), `userAndAssistantMessages`, `lastNMessages` (with `maxMessages`), `previousAssistantMessages`, `none`; no summary mode.
2. Vapi enterprise on-premise, ElevenLabs private deployment, Cognigy on-premises availability, Bolna enterprise on-prem: conflicting or single-source deployment claims (marked "verify" in 5.3).
3. Deepgram self-hosting details: GPU requirements, licensing tier, and whether Flux/EOT ships in the self-hosted artifact set. (The immutability of Reusable Agent Configuration bodies was confirmed 2026-07-15.)
4. `greeting.speaks_first: user` (was `user_first`): **resolved 2026-07-15** except a Deepgram warn. Native on Vapi (`firstMessageMode: assistant-waits-for-user`) and ElevenLabs (empty `first_message` waits for the user); generated on LiveKit and Pipecat; on Deepgram the omitted-greeting behavior is undocumented, warns until the driver proves silence (SCHEMA.md N6).
5. `capacity` lowering to managed-provider concurrency limits and plan tiers: plausible, zero documentation found for any provider.
6. Tool interruption policy on barge-in: still unresolved 2026-07-15. Vapi and ElevenLabs docs are silent on in-flight tool fate; ElevenLabs documents prevention via per-tool `interruption_mode` (`allow`, `disable_during_tool`, `disable_during_tool_and_turn`), which is different semantics, not cancellation. Managed-target story stays `provider_default` only.
7. Bolna OSS Graph Agents: all Bolna T1/T2 verdicts assume the cloud product.
8. **Generation-parameter slots: resolved 2026-07-15.** Vapi `assistant.model.temperature` and `maxTokens`; ElevenLabs `conversation_config.agent.prompt.temperature` (default 0) and `max_tokens`; Deepgram `agent.think.provider.temperature` (no max-tokens slot exists). The 4.2 examples are confirmed surfaces, except that a max-tokens param must never be forwarded to Deepgram.
9. **Vapi custom-model surfaces** (custom-llm provider, custom transcriber, custom voice) and Vapi's unknown-field behavior (reject versus silently ignore): both undocumented in the research; the second decides whether misconfigured params are detectable there.
10. **ElevenLabs LLM cascading: resolved 2026-07-15.** Configurable via `backup_llm_config` (`preference: default | disabled | override`, ordered `order` list, `cascade_timeout_seconds`); entries carry no per-entry params, so fallback profiles with distinct binding params warn.
11. **LiveKit native fallback adapter: resolved 2026-07-15.** `FallbackAdapter` exists for LLM, STT, and TTS in Python and Node; `fallback:` lowers natively on LiveKit.
12. **Pipecat custom STT/TTS mechanism: resolved 2026-07-15.** `OpenAISTTService` and `OpenAITTSService` accept a documented `base_url` override; OpenAI-compatible SLNG endpoints are a config forward, no generated service classes needed.
13. **Voicemail behaviors: resolved on four primaries 2026-07-15.** LiveKit `AMD`, Pipecat `VoicemailDetector`, Vapi `voicemailDetection` plus `voicemailMessage`, ElevenLabs `voicemail_detection` system tool: both hang-up and leave-message are documented on each. The Deepgram bridge lowering (carrier AMD) is also proven (review-corrected 2026-07-15: Deepgram publishes an official AMD-bridge outbound reference impl using Twilio async AMD → hangup + leave-message); outbound is generated with a warning there, carrier-conditional (SCHEMA.md N6).

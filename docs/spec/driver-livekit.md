# SPEC — LiveKit driver (generate.Artifact case)

Consumes the core: [compiler.md](compiler.md). Schema truth: [SCHEMA.md](../../SCHEMA.md). LiveKit facts: SCHEMA.md conditions ⁰ + ORCHESTRATOR footnote ⁰. SCHEMA.md wins on disagreement.

Type: **code target**. Emits a LiveKit Agents project on disk (`Artifact.Files`). No generator today. SDK language matters: `sdk_language: python` is required for warm transfer and MCP.

## §G goal
Lower a validated `ir.Agent` + a `livekit` target instance into a runnable LiveKit Agents project: entrypoint (worker/session), agents as `Agent`s with handoff, tasks as `AgentTask`, task groups as `TaskGroup` or a standalone-`AgentTask` sequence, controls, tools, conversation, dependency/deploy files, compile report. The session wires SLNG-routed `listen`/`speak` through the `livekit-plugins-slng` plugin (the Execution Layer), `reason` through LiveKit Inference, turn detection through Inference, VAD through Silero, and optional input noise cancellation through ai-coustics (C8). The `unmute init` example for LiveKit is an invented handoff + task-group agent that emits this recipe, so a first generation is a real SLNG agent, not a random plugin pick (V11, V12).

## §C constraints
- C1: Go emits the project via `text/template` (ADR-0002); embedded FS. Default SDK language `python`; `node` allowed. MCP is Python-only (V9); warm transfer works on **both** languages — stable on Node, Beta on Python (V6). (review-corrected 2026-07-15.)
- C2: all four roles are **open**: every used profile needs a binding (V9 core).
- C3: `TaskGroup` is experimental **and always shares context** → `context_scope: isolated` cannot lower to it; it lowers to a generated sequence of standalone `AgentTask`s instead. `TaskGroup` also summarizes into the owner's context by default (`summarize_chat_ctx=True`); the `merge: results` lowering MUST set it `False`.
- C4: a standalone `AgentTask` starts with an empty chat context and does not propagate its turns back — the typed result is the only return (N13). This is the default (a `preserveFunctionCallHistory` option can override it; the templates must never set it). `reset` history still lands an `AgentHandoff` marker (Node: `AgentHandoffItem`) in the new context.
- C7: version pins are **per-SDK-language** — Python (`livekit-agents`) and Node (`@livekit/agents`) version independently and their namespaces diverge (`beta.workflows` vs stable `workflows`); the template-compatible range is checked per language.
- C5: full turn-detector model is a Cloud/Inference feature with automatic fallback to the mini model → `turn.placement` is a preference; forward + warn, never promise.
- C6: version pins — LiveKit plugins are independently versioned and each needs a `pins:` entry; the driver checks pins against its template-compatible range (codegen check).
- C8: SLNG is the Execution Layer, not a `placement`. On LiveKit a `listen`/`speak` role bound to an SLNG model route lowers to `slng.STT`/`slng.TTS` from `livekit-plugins-slng`, never a per-vendor STT/TTS service; the bound route is the plugin's `model=` argument (for example `deepgram/nova:3`, and `deepgram/aura:2` with a `voice`), and `language` rides the binding. This keeps SLNG's middleware underneath the model calls on every target (CONTEXT.md). The rest of the recipe is native LiveKit: `reason` to `inference.LLM`, turn to `inference.TurnDetector`, VAD to `silero.VAD`, optional input noise cancellation to `ai_coustics`. Reference: the working `taskgroups`/`handoffs` variants at github.com/slng-ai/livekit_orchestration_examples; the plugin is pinned per C6/T7 (`livekit-plugins-slng`).

## §I surfaces
- I.emit: `GenerateLiveKit(agent *ir.Agent, target ir.Target) (Artifact, error)` — the `livekit` case of `generate.Artifact`.
- I.templates: `templates/livekit/*.tmpl` (embedded) → entrypoint, agents, tasks, tools, deploy files, compile report.

## §V invariants
- V1: `task` lowers to a native `AgentTask` (empty context, no turn propagation, typed result only, C4).
- V2: `task_group` with `context_scope: shared` → `TaskGroup` with `summarize_chat_ctx=False` (C3) and an experimental **warning on Python** (stable in Node's `workflows` namespace — no warning there); with `context_scope: isolated` → a generated standalone-`AgentTask` sequence (no TaskGroup).
- V3: `then: return`/`transfer`/`end` all compile; `agent_transfer` is native handoff; `reset` keeps the `AgentHandoff` (Node `AgentHandoffItem`) marker (C4).
- V4: `fallback` lowers natively to `FallbackAdapter` (LLM/STT/TTS) — no generated switcher.
- V5: history `full`/`messages`/`last_n`/`summary`/`reset` all compile (all via `ChatContext` construction); `summary` requires a declared+bound `summarizer` profile.
- V6: `human_transfer` — `mode: cold` native; `mode: warm` native (prebuilt `WarmTransferTask`) on **both** SDK languages — stable on Node (no warning), `beta.workflows` on Python (Beta warning); `briefing: summary` supported (consultation flow: TransferAgent summarizes to the operator); `briefing: message`/`wait` → gated error. (review-corrected 2026-07-15: LiveKit's docs badge lags its shipped code; warm is not Python-only.)
- V7: `requires:` guards are generated machine-checked code (code target); a failed guard returns the model a refusal naming the unmet variables (contract).
- V8: `greeting` — `speaks_first: user`, fixed `text`, model-written opening all generated; `on_voicemail` lowers to `AMD` (leave via `generate_reply` then shut down, or hang up); `placement: local` on listen/speak allowed.
- V9: `mcp` tools compile **only with `sdk_language: python`** → gated error on `node`.
- V10: L4 smoke proves the emitted entrypoint imports/validates; safe_core → livekit emits valid, with the expected `TaskGroup`/turn-placement warnings only.
- V11: the emitted `AgentSession` binds SLNG-routed `listen`/`speak` through `slng.STT`/`slng.TTS` (route as `model=`), never a per-vendor STT/TTS plugin, and binds `reason` through `inference.LLM`; the golden asserts the entrypoint imports `slng` from `livekit.plugins` and constructs both (C8).
- V12: the LiveKit `unmute init` example (`examples/livekit/`) is an invented persona, never the reference Casavo prompt: a greeter that hands off (agent_transfer) to two specialist agents, each running a `TaskGroup` (a journey step, then a shared confirm-contact task). It exercises handoff, task groups, and the SLNG plugin together, and emits valid (V10) with only the permitted `TaskGroup`/turn warnings. The scaffold source package is owned by compiler.md T11; V12 governs only its LiveKit lowering.

## §T tasks
id|status|desc|cites
T1|x|LiveKit capability rows in the core table: TaskGroup→warn(Python only), warm→native both langs (Beta-warn Python only), briefing summary-only, mcp→python-only, turn placement→warn; else code-target defaults|C3,C5,V6,V9
T2|x|`GenerateLiveKit` skeleton wired into the `generate.Artifact` switch|I.emit
T3|~|`templates/livekit_v1/*.tmpl` — entrypoint, agents (handoff), pipeline roles (SLNG recipe), webhook tools, greeting done; channels/telephony + deploy files (Dockerfile/pcc) not emitted yet|I.templates,C2
T4|~|task/`AgentTask` (typed result via `finish`) + task_group shared → TaskGroup(`summarize_chat_ctx=False`)+warn done; isolated → standalone-AgentTask sequence not emitted yet (guarded)|V1,V2,V3,C3,C4
T5|.|native `FallbackAdapter` fallback; `ChatContext` history for all five values + summarizer|V4,V5
T6|.|human_transfer cold/warm(python+Beta)/briefing summary; requires guards; voicemail AMD|V6,V7,V8
T7|~|framework version range check done (>=1.5,<2.0); `sdk_language` mcp/node gate + per-SDK-language plugin `pins:` range check not done yet|C1,C7,V9
T8|~|golden test done (Remy, byte-for-byte) + emitted agent.py passes `py_compile`; L4 smoke (build-tag, real import) not wired yet; safe_core→livekit needs human_transfer (T6)|V10
T9|x|entrypoint/session template wires the SLNG recipe: `slng.STT`/`slng.TTS` for SLNG-routed listen/speak (route as `model=`), `inference.LLM` for reason, `inference.TurnDetector`, `silero.VAD`, optional `ai_coustics` NC; golden asserts the `slng` import + construction|C8,V11
T10|~|`examples/remy/` source package compiles to a valid LiveKit project (handoff + task groups + SLNG); golden is the tracked output; a checked-in `examples/livekit/` dir + compiler.md T11 init scaffold still pending. invented "Remy" restaurant concierge; a greeter with agent_transfer to a reservations agent and an events agent, each a `TaskGroup` (qualify, then a shared confirm-contact task); SLNG-routed listen/speak; source package via compiler.md T11|V12,V2,V3

Dependency order: T1, T2 → T3 → T4, T5, T6 → T7 → T8. T9 with T3 (entrypoint template); T10 after T4 + T9 and after compiler.md T11 scaffolds the source package.

## §B bugs
id|date|cause|fix

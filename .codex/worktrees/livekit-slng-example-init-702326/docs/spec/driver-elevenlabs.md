# SPEC — ElevenLabs driver (generate.Artifact case)

Consumes the core: [compiler.md](compiler.md). Schema truth: [SCHEMA.md](../../SCHEMA.md). ElevenLabs facts: SCHEMA.md conditions ³ + ORCHESTRATOR footnote ³. SCHEMA.md wins on disagreement.

Type: **managed target**. Emits an agent-config API payload (`Artifact.Payload`) plus an `ApplyPlan`. No generator today; follow the SLNG managed pattern (`internal/generate/slng.go` + `internal/cli/apply.go`).

## §G goal
Lower a validated `ir.Agent` + an `elevenlabs` target instance into an ElevenLabs Conversational-AI agent config body (with linear Workflow nodes for tasks/handoff), plus the apply plan and reconcile rule. Only the provider API surface is usable (D4). The single running transcript shapes context: everything is `full`.

## §C constraints
- C1: managed target — no generated code, no `placement: local` for open roles (gated), **except** `reason` via the documented custom-LLM endpoint (the one placement exception on this target).
- C2: roles — `listen` integrated (a binding tunes the built-in `asr` block, **never names an outside STT model**), `turn` integrated, `speak` open (**ElevenLabs voices only**), `reason` open (supported list + custom LLM endpoint). Generation params: `conversation_config.agent.prompt.temperature` (default 0), `max_tokens`.
- C3: the only mid-call variable write path is a **tool returning JSON** — a task `assign:` must route through such a tool.
- C4: transfer + tasks keep the **full running transcript**; there is no *user-configurable* tool-call filtering (the platform auto-strips `transfer_to_agent` calls from the child's history, so `full` here means full-minus-transfer-tool-calls). **Never lower to Procedures** (Alpha, breaking changes) — rule 11.
- C5: reconcile — ElevenLabs agents now have a **config branch system** (`preview-merge`/`preview-rebase` endpoints); which branch `apply` targets, and how dashboard-authored fields outside Unmute's model are preserved, is an open item (ORCHESTRATOR §9 item 6). Expect this to force a **branch-aware `ApplyPlan`** (see compiler.md), not just a doc note. The driver documents its comparison rule and branch choice; inert forwarded fields (retained but ignored) are listed in the report, never judged.

## §I surfaces
- I.emit: `GenerateElevenLabs(agent *ir.Agent, target ir.Target) (Artifact, error)` — the `elevenlabs` case of `generate.Artifact`; sets `Payload` + `Apply`.
- I.apply: uses the core `ApplyPlan` in its **branch-aware** form (C5: the config-branch system means apply targets a branch, not just an endpoint+method). Env-named credential only, no secret value (C9 core).

## §V invariants
- V1: `task` is a native Workflow subagent node; `assign:` compiles **only when routed through a tool returning JSON** (C3), else gated error; task-level context other than `full` → gated error (no documented context-isolation knob for subagent nodes); the driver **warns** that on the single running transcript task turns are likely visible to the owner (inferred, not a documented node-scope guarantee, N13).
- V2: `task_group` → native linear Workflow; `then: return`/`transfer`/`end` compile.
- V3: `agent_transfer` → native `transfer_to_agent`; the destination agent supplies its own config (prompt, first message, LLM, voice, tools, KB), the parent overwrites only plumbing (client events, TTS/ASR audio format), and the transfer tool calls are auto-stripped from the child's history; `requires:` guards → gated error (no mechanism).
- V4: context — **`full` only**; `messages`/`last_n`/`summary`/`reset` all → gated error; `include_tool_calls: false` → gated error (no *user-configurable* tool-call filtering, C4).
- V5: `fallback` → native LLM fallback — `backup_llm_config` (`preference: override`, ordered `order` of model IDs, **no per-entry params**) plus a **sibling** `cascade_timeout_seconds` (default 8, range 2–15s); a fallback profile whose binding carries `params` → **warn**.
- V6: `greeting` — `speaks_first: user` native (empty `first_message`); fixed `text` native (`first_message`); **model-written opening is conditional** — it lowers to a Workflow override-agent entry node with `entry_behavior: generate_immediately` (shipped 2026-06-15); a plain (non-workflow) agent greeting has no generate-opening toggle, so wrapping the entry agent in a workflow node is required. (review-corrected 2026-07-15: was a hard gate on the false premise that no mode exists.)
- V7: `on_voicemail` → native `voicemail_detection` system tool + optional `voicemail_message` (absent = hang up); `outbound: true` compiles.
- V8: `human_transfer` — `mode: cold` native; `mode: warm` native (conference); `briefing: message` native (reads a static message to the operator) **only when the number is imported via the native Twilio integration** — SIP-based transfers do not support warm-transfer messages → gated there; `briefing: summary`/`wait` → gated error. (review-added 2026-07-15: Twilio-only condition.)
- V9: `thinking_audio` native; `interruption.minimum_words` → **warn** (no word-count knob); `placement: local` gated except `reason` custom-LLM endpoint (C1); a `listen` binding that names an outside model → structural error (C2).
- V10: apply carries no secret values (C9 core); the reconciler documents its branch + comparison rule (C5); inert forwarded fields are reported, never judged; every forwarded binding/param appears in the report (V15 core).

## §T tasks
id|status|desc|cites
T1|x|ElevenLabs capability rows in the core table: history non-full→fail, include_tool_calls:false→fail, requires→fail, model-written-opening→conditional (workflow entry node), briefing message (Twilio-only), minimum_words→warn, fallback-with-params→warn, task-turns-visible→warn|V1,V3,V4,V5,V6,V8,V9
T2|x|`GenerateElevenLabs` skeleton wired into `generate.Artifact` switch; `Payload` + `Apply`|I.emit,I.apply
T3|x|agent-config payload — integrated listen (asr settings only), speak ElevenLabs-voice-only, reason (+custom LLM), prompt temperature/max_tokens|C1,C2,V9
T4|x|Workflow nodes — task (assign via tool-JSON), task_group linear (then/merge), agent_transfer transfer_to_agent; no Procedures|C3,C4,V1,V2,V3
T5|x|fallback `backup_llm_config` + sibling `cascade_timeout_seconds` (params→warn); greeting (user/text native, model-written via workflow `entry_behavior: generate_immediately`); voicemail; human_transfer cold/warm/briefing:message (Twilio-only)|V5,V6,V7,V8
T6|x|`unmute apply` for elevenlabs — branch-aware payload push with env credential; documented reconcile branch + comparison rule; report inert fields|I.apply,C5,V10
T7|x|golden test (payload) + apply integration test (mockable); safe_core → elevenlabs payload valid (T0+T2 only)|V10

Dependency order: T1, T2 → T3 → T4 → T5 → T6 → T7.

## §B bugs
id|date|cause|fix

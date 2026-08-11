# SPEC — Vapi driver (generate.Artifact case)

Consumes the core: [compiler.md](compiler.md). Schema truth: [SCHEMA.md](../SCHEMA.md). Vapi facts: SCHEMA.md conditions ² + ORCHESTRATOR footnote ². SCHEMA.md wins on disagreement.

Type: **managed target**. Emits an assistant/Squad API payload (`Artifact.Payload`) plus an `ApplyPlan` that pushes it to the Vapi API. No generator today; the existing SLNG generator (`internal/generate/slng.go` payload + `internal/cli/apply.go` POST) is the closest managed-target pattern to follow.

## §G goal
Lower a validated `ir.Agent` + a `vapi` target instance into a Vapi assistant (or a Squad, when handoff is present) API body, plus the apply plan (endpoint + method + env-named credential) and a reconcile rule for forwarded fields. Only what Vapi's API offers is usable — no generated orchestrator loop (D4).

## §C constraints
- C1: managed target — no generated code, no `placement: local` for open roles (gated error); `reason` via a documented custom-LLM endpoint is unverified → fails there for now.
- C2: any handoff (`agent_transfer` or a `task_group` that transfers) is lowered by synthesizing a **Squad** around the assistants — the documented, reliable handoff mechanism (transfer-by-`assistantId` also exists; Squad synthesis is the safe choice, not a documented requirement).
- C3: **never lower to Vapi Workflows** (retire 2026-08-18, verified exact) — rule 11.
- C4: roles — `listen`/`speak`/`reason` open, `turn` integrated (endpointing lowered from conversation intent, not bound). Generation params: `assistant.model.temperature`, `maxTokens`.
- C5: context-mode spellings (verified 2026-07-15 letter-for-letter): `contextEngineeringPlan` ∈ `all` (their default), `userAndAssistantMessages`, `lastNMessages` (+ `maxMessages`), `none`; `previousAssistantMessages` stays unexposed; **no summary mode exists**. Because our schema has no default (D7) but Vapi defaults to `all` (full history), the driver **MUST emit an explicit `contextEngineeringPlan` on every transfer** — never omit it, or users silently get full-history forwarding (the opposite of privacy-safe).
- C6: managed reconcile — the driver diffs modeled fields normally and compares forwarded `params` opaquely (byte-equal after a documented normalization rule). The normalization must **also ignore server-injected defaults on modeled fields the driver never set** (`voicemailDetection.provider`→`vapi`, `firstMessageMode`→`assistant-speaks-first`, `backgroundSound`→`office`), or those produce the same perpetual plan drift.

## §I surfaces
- I.emit: `GenerateVapi(agent *ir.Agent, target ir.Target) (Artifact, error)` — the `vapi` case of `generate.Artifact`; sets `Payload` + `Apply`.
- I.apply: a Squad apply is a **multi-step ordered plan** — N assistant creates, then one squad create referencing them by id (id captured between steps) — so this driver uses the core `ApplyPlan`'s multi-step form (see compiler.md), not a single endpoint+method. Idempotency keyed on assistant/squad identity (create-vs-update). Env-named credential only, never a secret value (C9 core); consumed by `unmute apply`.

## §V invariants
- V1: single `tasks` → **gated error** (a handoff back to a previously active assistant is unverified, N6/open item 2).
- V2: `task_group` lowers to a Squad handoff chain with shared variables; `then: transfer`/`then: end` compile; **`then: return` → gated as unverified** (a state-preserving return to a prior assistant is undocumented — docs silent both ways, N6/open item 2; matches V1, not a proven impossibility); never a Vapi Workflow (C3). (review-corrected 2026-07-15: was worded "Squad cannot return".)
- V3: `agent_transfer` → native Handoff tool, synthesizing a Squad (C2).
- V4: context — `full`/`messages`/`last_n`/`reset` compile via C5 spellings; **`summary` → gated error** (no summary mode); `requires:` guards → gated error (no mechanism).
- V5: `fallback` → native `model.fallbackModels`; a **cross-provider chain → gated error** (same-provider model IDs only); non-OpenAI model schemas are conditional (verified on OpenAI only) → warn.
- V6: `greeting` — `speaks_first: user` native (`firstMessageMode: assistant-waits-for-user`); fixed `text` native (`firstMessage`); model-written opening native (`assistant-speaks-first-with-model-generated-message`).
- V7: `on_voicemail` → native `voicemailDetection` + `voicemailMessage` (message set = leave, omitted = hang up); `outbound: true` compiles.
- V8 (amended 2026-08-10, SCHEMA N25): `human_transfer` — the `cold:` block is native; the `warm:` block on the stable `transferPlan` path requires `carrier: twilio` (the experimental assistant-based warm transfer also works with Vapi numbers/SIP, not emitted). (review-corrected 2026-07-15: warm is not universally Twilio-only.) The `briefing` mode enum is gone: `transferPlan.mode` was where it came from, and it never mapped to the two code targets. `warm.briefing` is now free text, and this driver is the one that has to translate it — a set `briefing` lowers to `warm-transfer-with-summary` with the text as the summary plan, and an unset one to Vapi's own default. `warm-transfer-with-message` and `-wait-for-operator-…` have no portable trigger left and are not emitted in v1; if a user needs the exact wording read out verbatim, that is the case for a future `warm.briefing_style` field, decided when a real package asks. `ring_timeout` and `on_unavailable` gate here until their `transferPlan` slots are doc-verified.
- V9: `thinking_audio` → **gated error** (`backgroundSound` plays continuously, not gated to model-thinking windows — a custom URL is allowed but still not thinking-gated, so no faithful lowering); `placement: local` on open roles → gated error (C1).
- V10: the apply payload carries no secret values (env-named credential only, C9 core); the reconciler compares forwarded `params` opaquely (C6); every forwarded binding/param appears in the report (V15 core).

## §T tasks
id|status|desc|cites
T1|.|Vapi capability rows in the core table: task→fail, then:return→fail, summary→fail, requires→fail, cross-provider fallback→fail, thinking_audio→fail, local→fail, warm→twilio-only|V1,V2,V4,V5,V8,V9
T2|.|`GenerateVapi` skeleton wired into `generate.Artifact` switch; `Payload` + `Apply` output|I.emit,I.apply
T3|.|assistant payload — pipeline roles (turn integrated), tools (webhook), conversation, channels; temperature/maxTokens params|C4
T4|.|Squad synthesis for agent_transfer + task_group transfer/end; context spellings (C5); no Workflows|C2,C3,C5,V2,V3
T5|.|greeting (all three modes native), voicemail (voicemailDetection+voicemailMessage), human_transfer cold/warm(twilio)/briefing|V6,V7,V8
T6|.|`unmute apply` for vapi — multi-step Squad apply (N assistant creates → squad create with id back-refs, idempotent); reconcile diff (modeled fields + server-default normalization + opaque params)|I.apply,C6,V10
T7|.|golden test (payload) + apply integration test (mockable); safe_core → vapi payload valid (T0+T2 only)|V10

Dependency order: T1, T2 → T3 → T4 → T5 → T6 → T7.

## §B bugs
id|date|cause|fix

# SPEC — Deepgram driver (generate.Artifact case)

Consumes the core: [compiler.md](compiler.md). Schema truth: [SCHEMA.md](../../SCHEMA.md). Deepgram facts: SCHEMA.md conditions ⁴ + ORCHESTRATOR footnote ⁴. SCHEMA.md wins on disagreement.

Type: **code target — session bridge**. Emits a generated media/telephony bridge (`Artifact.Files`) plus an inline agent `Settings` JSON. No generator today. It is a code target (Unmute owns the bridge code), NOT managed.

## §G goal
Lower a validated `ir.Agent` + a `deepgram` target instance into a generated bridge that owns media/telephony and drives Deepgram's Voice Agent API with an inline `Settings` object. Handoff, tasks, and human transfer are the **Pattern** (generated in the bridge). Live state lives in the bridge, never in template variables.

## §C constraints
- C1: emit **inline `Settings` JSON**, never a Reusable Agent Configuration — the config body is immutable (to change it you create a NEW config → new UUID; the old need not be deleted), so referencing one is create-per-change churn with no compile-time benefit (SCHEMA.md §9 item 1 resolved).
- C2: `listen` accepts **Deepgram models only**; a third-party listen model has no slot → structural failure (V9 core). `speak` accepts Deepgram + a fixed third-party provider list, and an `agent.speak.endpoint` (url + headers) is allowed **within that fixed list**; an arbitrary TTS protocol still fails structurally. `reason` is open, custom endpoints allowed (`agent.think` has an `endpoint`). (Since 2026-07-15 the listen/speak lists are provider-catalogue matrix rows — `internal/target/catalog_deepgram.go`, Deepgram's `eleven_labs`/`open_ai` spellings as aliases — enforced at `validate` via `Catalog.CheckVendor`, ahead of this driver existing.)
- C3: `turn` is **integrated into `listen`**; turn thresholds (`eot_threshold`, `eager_eot_threshold`, `eot_timeout_ms`) ride the **listen binding's `params`** (that is where the provider locates them, `agent.listen.provider`). These knobs are **Flux-only** — when the bound listen model is not Flux (e.g. Nova), the driver drops them with a warning rather than emitting keys the model has no field for. `semantic_endpointing` is forwarded, not verified — whether it applies depends on the bound listen model (Flux integrates it; Nova falls back to silence endpointing). (review-corrected 2026-07-15.)
- C4: template variables are substitution-time only and visible to project members — **never route secrets or live state through them**; live state lives in the generated bridge.
- C5: generation params — `agent.think.provider.temperature` exists but **no max-tokens slot does**; the driver must never forward a max-tokens param to Deepgram (SCHEMA.md §9 item 8).
- C6: `placement: local` on `listen`/`speak` fails (no slot for an outside model, C2).

## §I surfaces
- I.emit: `GenerateDeepgram(agent *ir.Agent, target ir.Target) (Artifact, error)` — the `deepgram` case of `generate.Artifact`.
- I.templates: `templates/deepgram/*.tmpl` (embedded) → bridge code, inline `Settings` JSON, deploy files, compile report.

## §V invariants
- V1: the emitted artifact carries an inline `Settings` object; no Reusable Agent Configuration UUID is referenced (C1).
- V2: `listen` binding names a Deepgram model or fails structurally; a third-party `listen` model, or `placement: local` on listen/speak, → gated/structural error (C2, C6).
- V3: turn thresholds are forwarded on the listen binding's `params`; `turn.placement` is ignored with a warning (integrated, C3).
- V4: `agent_transfer`/`task`/`task_group` lower to the Pattern — in-session `UpdateThink` + `UpdatePrompt` (keeps full history natively) or session replacement with context replay; history `full`/`messages`/`last_n`/`reset` need no summarizer, while `summary` still requires a generated summarizer profile; all five compile (via `agent.context.messages` replay or natively-kept history).
- V5: `fallback` lowers natively to `agent.think` as an ordered provider array (mixed providers, per-entry params allowed).
- V6: `greeting.text` lowers natively to `agent.greeting`; `speaks_first: user` **warns** (omitted-greeting behavior undocumented — the driver smoke test must prove silence); a model-written opening (no `text`) lowers as a **generated Pattern** — inject a synthetic turn at call start via `InjectUserMessage`/`InjectAgentMessage` — **with a warning** (documented for orchestrated openings, but not as a first-class greeting mode). (review-corrected 2026-07-15: was a hard gate.)
- V7: `on_voicemail` and `outbound: true` lower as a **generated carrier-conditional Pattern with a warning** — Deepgram ships an official AMD-bridge outbound reference impl (Twilio async AMD in the bridge → both `hangup` and `leave_message` via Aura-2 TTS), structurally identical to the human-transfer bridge Pattern; resolves against the target `carrier`. `thinking_audio` → gated error (no faithful lowering). (review-corrected 2026-07-15: voicemail/outbound was a hard gate on a now-disproven "unproven" rationale.)
- V8: `interruption.minimum_words` and `ignore_phrases` are lossy → warn (the model halts before the bridge can count words / phrases dropped); `mcp` tools → gated error (no runtime MCP client).
- V9: `human_transfer` resolves against the target's `carrier` in the bridge (carrier-conditional), never the brand alone.
- V10: no max-tokens param is ever forwarded (C5); L4 smoke proves the bridge starts and the `Settings` JSON is accepted; safe_core → deepgram emits valid, with only the `speaks_first: user` / turn-placement / interruption-tuning warnings SCHEMA.md §7 allows.

## §T tasks
id|status|desc|cites
T1|.|Deepgram capability rows in the core table: voicemail/outbound→carrier-conditional Pattern+warn, model-written-opening→Pattern+warn, thinking_audio→fail, mcp→fail, speaks_first:user→warn, interruption tuning→warn, turn placement→warn, non-Flux eot_*→drop+warn|V6,V7,V8,C3
T2|.|`GenerateDeepgram` skeleton wired into the `generate.Artifact` switch|I.emit
T3|.|inline `Settings` JSON emission (no Reusable Config); listen Deepgram-only + speak fixed-list structural checks; drop max-tokens|C1,C2,C5,V1,V2
T4|.|turn thresholds on listen `params`; drop `eot_*` + warn on non-Flux listen model; semantic_endpointing forwarded; turn-placement warn|C3,V3
T5|.|bridge Pattern — handoff/task via `UpdateThink`+`UpdatePrompt` or session replacement; history replay for all five values (summarizer for `summary`)|V4
T6|.|native `agent.think` fallback array; greeting.text native + speaks_first:user warn + model-written opening via Inject Pattern+warn|V5,V6
T7|.|carrier-conditional human_transfer + voicemail/outbound AMD in bridge (Pattern+warn); interruption/ignore_phrases warn; mcp fail|V7,V8,V9
T8|.|golden test (bridge + Settings) + L4 smoke (bridge starts, Settings accepted, silence proven); safe_core green with allowed warnings|V10

Dependency order: T1, T2 → T3 → T4, T5, T6, T7 → T8.

## §B bugs
id|date|cause|fix

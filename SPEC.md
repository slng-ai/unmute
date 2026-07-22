# SPEC — LiveKit codegen fidelity + delegated-flow fix

Scope: close the gaps between our generated LiveKit `agent.py` and the canonical
upstream examples (`hotel_receptionist`, the restaurant-agent recipe) and the
unmute tool model, plus fix one confirmed runtime defect (F0) where a completed
delegated flow re-runs and double-fires its side effects. This is **not** a
rewrite of the handoff, task, or task-group lowering; those already track
upstream. This file is subordinate to the authoritative driver contract
[driver-livekit.md](docs/spec/driver-livekit.md); references to it are written
`dl§V1`, `dl§C3`, `dl§N13`. This file's own ids (V1, T1, B1...) are local.
Schema truth is [SCHEMA.md](SCHEMA.md); the tool model is
[tools.md](docs/user/reference/tools.md).

## §G goal
Generated LiveKit code is **schema-faithful** to the unmute tool model (one YAML
tool = one JSON-Schema input, carried across both the agent-level and task-level
`@function_tool` paths, with enums and per-property descriptions intact), **reads
like** the `hotel_receptionist` and restaurant-agent examples (no scaffolding a
single-agent bot never needs), **ends a delegated flow exactly once** (the owner
never re-executes a finished flow, so side effects fire once), and is
**ruff/ty/format-clean**. Fix F0 (confirmed defect) and F1; align F2/F3/F4;
confirm-then-record F5/F6. SLNG stays the scaffold default (dl§C8); the Remy
golden updates only where F0/F1/F2 force it, and each update is recorded.

## §C constraints
- C1: Go emits the LiveKit project **only** through embedded `text/template`
  (ADR-0002, dl§C1); no hand-written Python in the generate package. Every fix
  is a template ([agent.py.tmpl](internal/generate/templates/livekit_v1/agent.py.tmpl))
  plus build-model ([livekit_v1_build.go](internal/generate/livekit_v1_build.go),
  [livekit_v1.go](internal/generate/livekit_v1.go)) change, golden-tested
  (`-update`) and smoke-gated (`make smoke`, needs Python + uv). The CLI write
  path already runs a best-effort `ruff format` pass on emitted `.py`
  ([compile.go](internal/cli/compile.go) `formatPython`, shared with pipecat);
  goldens capture the **raw** generator output (zero-Python L1-L3 preserved).
- C2: **doc wins** (CLAUDE.md). When code and a doc disagree, fix the code.
  Every LiveKit API claim in this spec was verified against docs.livekit.io
  (`/agents/logic/tasks/`, `/agents/logic/workflows/`, `/agents/logic/agents-handoffs/`,
  `/agents/logic/tools/definition/`, the restaurant-agent recipe) and the
  livekit/agents source on 2026-07-22, never from memory.
- C3: **reconcile with [driver-livekit.md](docs/spec/driver-livekit.md)** — it is
  authoritative. No invariant here may silently contradict dl§V1-V26 or
  dl§C3/C4/C8. In particular dl§C3 (TaskGroup always shares context and merges
  back, so `merge: results` snapshots+restores the owner context, N13) and dl§C4
  (a standalone `AgentTask` starts fresh and does not propagate its turns back)
  stay intact; F0's fix does not touch them. Changing locked behavior is an
  explicit amendment to that file plus this one, never a quiet redefinition.
- C4: **unmute tool model is the fidelity north star** ([tools.md](docs/user/reference/tools.md)):
  a tool is one YAML file, filename = tool name; `input` is a JSON-Schema object
  with `type`, `enum`, per-property `description`, `required`. No new `execution`
  kinds, no new spec surface; this is faithful lowering of the model that already
  exists. A generation path must not drop schema detail the author wrote.
- C5: **clean, minimal output**. Reader is LiveKit-fluent. Lazy wins: no
  `chat_ctx` handoff plumbing on an agent that is never a handoff target; no
  restating framework defaults.
- C6: version floor is **per-SDK-language** (dl§C7): Python `livekit-agents` and
  Node `@livekit/agents` version independently. Template-compatible range
  `>=1.5 <2.0` ([livekit_v1.go:34](internal/generate/livekit_v1.go:34)); upstream
  examples are on `>=1.6.0`. Fixes stay inside this range.
- C7 (non-goals): no pipecat-target change; no provider-catalogue change (owned
  by dl§C8/V11/T17, leave `catalog_livekit.go` alone); no tracing change
  (dl§V21-V25). Don't rewrite handoff/task/task-group lowering that already
  matches upstream; only F0 and the gaps below. Keep SLNG the scaffold default
  and the Remy golden byte-identical unless F0/F1/F2 force a recorded update.

## §I surfaces
- I.tmpl: [agent.py.tmpl](internal/generate/templates/livekit_v1/agent.py.tmpl) —
  the single template emitting every scenario. F0 lives in the `finish` block
  (~426-430) and the delegate blocks (single-task ~340-357, TaskGroup ~379-412);
  F1 in the tool-signature defines (`webhook_tool` ~15, `local_tool` ~32, `finish`
  ~427); F2 in the transfer/delegate ctx copies and the `on_enter` bodies
  (~132, ~197, ~238, ~262, ~286); F3 in the agent-class `__init__` (~269).
- I.build: [livekit_v1_build.go](internal/generate/livekit_v1_build.go)
  (`buildLiveKitDelegate`, `buildLiveKitTool`, `livekitToolArgs`,
  `resultPyType`, `livekitCtxExpr`, `delegateWhen`) +
  [livekit_v1.go](internal/generate/livekit_v1.go) (`livekitArg`, `livekitDelegate`).
- I.gold: `internal/generate/testdata/golden/livekit_v1_remy_unconfigured_agent.py`
  + `livekit_v1_remy.txt` (regen with `-update`).
- I.smoke: L4 `make smoke` (`livekit_v1_smoke_test.go`, `-tags smoke`, needs
  Python + uv) — proves emitted Python imports, instantiates, and passes
  `ruff check .` + `ty check .` over every public example (dl§V26).
- I.eval: an L4 delegated-flow assertion (new, T1) running the multi-task example
  through one flow and asserting a single activity cycle with the side effect
  firing once.

## §V invariants
- V1 (F0, from B1): **a completed task or delegate returns control to the owner
  exactly once, and the owner never re-executes a finished flow.** Concretely:
  (a) a task's completion tool (`finish`) resolves the task with `self.complete(<result>)`
  **only**, typed `-> None`, with **no trailing return value**
  ([agent.py.tmpl](internal/generate/templates/livekit_v1/agent.py.tmpl) ~426-430);
  a value returned after `complete()` emits a stray function-tool output after the
  task is already closed, which is not the upstream idiom (docs `/agents/logic/tasks/`:
  every completion tool is `-> None` and calls `self.complete(...)` as the sole
  resolution). (b) A `then: return` delegate (single-task and TaskGroup-return)
  carries **finality guidance in its docstring**: the returned result is the final
  outcome to relay, and the flow must not be re-run for the same request
  (mirroring the restaurant recipe's flow-entry docstrings). Without it the owner
  LLM treats the delegate's `when` as a standing instruction and re-invokes the
  finished flow, producing a second `start_agent_activity` with no intervening
  `user_turn` and double-firing side effects (B1). (c) The single-task delegate's
  context discipline is **unchanged**: it already matches the documented supervisor
  pattern (`await Task(chat_ctx=self.chat_ctx.copy(exclude_instructions=True))`,
  the owner keeps its own context, no snapshot/restore) and dl§C4 (a standalone
  `AgentTask` does not propagate its turns back). The TaskGroup snapshot/restore
  (dl§C3/N13, ~394-401) stays because `summarize_chat_ctx=False` still merges the
  group's turns into the owner context. The single-task/group asymmetry is correct
  by design, not a defect. Guard: the Remy golden asserts `finish` has no trailing
  return (`-> None`) and every `then: return` delegate docstring carries the
  finality guidance; plus an L4 eval (I.eval) that runs one flow of the multi-task
  example and asserts a single `start_agent_activity` cycle with the tool side
  effect firing once.
- V2 (F1, extends dl§V15): **both `@function_tool` emission paths present the LLM
  the schema the tool YAML declares.** A tool's `input` enum lowers to
  `Literal[...]` and its per-property `description` to `Annotated[<type>, Field(description=...)]`
  on the method signature (LiveKit derives the JSON schema the LLM sees from the
  signature via pydantic `build_legacy_openai_schema`, which maps `Literal` to a
  JSON-schema `enum` and `Field(description=...)` to the property description; the
  tool docstring supplies the tool-level description). This holds on the agent-level
  path (`webhook_tool`/`local_tool`, ~15/~32) and the task-level path (task tools
  and the `finish` result args, ~427) alike. Non-string primitive types already map
  (`jsonPyType`), so `party_size: int` is correct today; the gap is enums and
  per-property descriptions, dropped by `livekitToolArgs`
  ([livekit_v1_build.go:847](internal/generate/livekit_v1_build.go:847), reads only
  `type`) and `resultPyType`
  ([livekit_v1_build.go:824](internal/generate/livekit_v1_build.go:824), collapses
  enum to `str`), because `livekitArg`
  ([livekit_v1.go:232](internal/generate/livekit_v1.go:232)) carries no enum or
  description field. Reconcile with dl§V15 (`livekitEmittedFields` agreement test)
  so a dropped schema detail fails a test. Guard: `TestV2LiveKitToolCarriesSchema`
  asserts a tool with an enum property and per-property descriptions reaches the
  LLM with `Literal[...]` and `Field(description=...)` on both the agent-level and
  task-level signatures; the multi-task `check_availability`
  (`service: enum [haircut, hair-color, blowout]`, `date: description`) and
  `customer_record` finish (`record_status: enum [existing, created, failed]`)
  show the enums and descriptions previously lost.
- V3 (F4, extends dl§V26): the emitted LiveKit project is **stable under
  `ruff format`** in addition to dl§V26's `ruff check .` + `ty check .`. The L4
  static-check gate (`TestSmokeV26LiveKitExamplesStaticCheck`, I.smoke) runs
  `uv run ruff format --diff .` over every public example and asserts no diff (the
  write-path `ruff format` pass, [compile.go](internal/cli/compile.go), has already
  laid out the on-disk source, so a format regression in an emitted example fails
  the gate). Byte-`ruff format`-stability of the **raw** generator output stays out
  of scope (C1 forbids formatting in the generator; runtime string length drives
  line wrapping). Guard: the `ruff format --diff` step added to the same L4 loop
  as `ruff check`/`ty check` (skips if uv absent).

*(No new §V for F2: per the recorded decision it is a plain alignment task, not a
locked-invariant amendment; dl§V3/V5 defaults stay authoritative. No new §V for
F3/F5/F6: F3 is an idiom-cleanup task, F5/F6 confirmed sound with no defect, see
§T.)*

## §T tasks
id|status|desc|cites
T1|x|F0 fix (PRIORITY, confirmed defect). (a) `finish` resolves via `self.complete(<result>)` only: drop the trailing `return "Done."` and change `-> str` to `-> None` ([agent.py.tmpl](internal/generate/templates/livekit_v1/agent.py.tmpl) ~426-430; golden `:252`,`:276`,`:289`). (b) Emit `then: return` delegate docstrings that state the returned result is FINAL (relay it and continue) and the flow must not be re-run for the same request, for both the single-task delegate (`buildLiveKitDelegate` ~609) and the TaskGroup-return delegate (~624 `GroupReturn` case, which today appends nothing while transfer/end append finality text at ~628/~630); source the finality line generically or from the flow/task description. (c) Single-task context discipline UNCHANGED (dl§C4 + supervisor pattern already match upstream; document, no code change) and TaskGroup restore UNCHANGED (dl§C3/N13). Regen Remy golden (finish loses `Done.`, `do_event`/`do_reserve` docstrings gain finality). Add the L4 eval (I.eval) asserting a single activity cycle + one side-effect fire on the multi-task flow|V1,B1,dl§C3,dl§C4,dl§N13,I.tmpl,I.build,I.eval
T2|x|F1 fix (PRIORITY). Thread the tool `input` schema through both `@function_tool` paths. Extend `livekitArg` ([livekit_v1.go:232](internal/generate/livekit_v1.go:232)) with `Enum []string` + `Description string`; `livekitToolArgs` ([livekit_v1_build.go:847](internal/generate/livekit_v1_build.go:847)) populates them from `prop["enum"]`/`prop["description"]`; `resultPyType`/finish-arg lowering ([livekit_v1_build.go:824](internal/generate/livekit_v1_build.go:824)) stops collapsing enum to `str`. Template renders `Literal[...]` for enum args and `Annotated[<type>, Field(description=...)]` for described args (import `Literal` from `typing`, `Field` from `pydantic`) on `webhook_tool`/`local_tool`/`finish`. Reconcile with dl§V15 (`livekitEmittedFields`) so a dropped detail fails a test. Add `TestV2LiveKitToolCarriesSchema`; regen goldens|V2,dl§V15,C4,I.tmpl,I.build
T3|x|F3 alignment (low priority). Minimal single-agent shape: when NO agent is ever a handoff target (single agent, no `agent_transfer`, no tasks/delegates), omit the unused `chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN` ctor param and the `super().__init__(chat_ctx=chat_ctx)` line ([agent.py.tmpl](internal/generate/templates/livekit_v1/agent.py.tmpl) ~269), and gate the now-unused `NOT_GIVEN`/`NotGivenOr`/`llm` imports (llm only when no fallback chain uses it). Scoped to the agent-class + entrypoint sections; tool and task blocks unchanged; multi-agent output byte-identical. Confirmed shape against `examples/simple-prompt` output and the docs' `Agent(instructions=...)` single-agent example. Record the trade-off (uniform template vs a conditional branch)|C5,I.tmpl,I.build
T4|x|F2 alignment (plain task, no locked-§V amendment per recorded decision). (a) Handoff/transfer context copies match the recipe idiom: add `exclude_handoff=True` and `exclude_config_update=True` (and consider `.truncate(max_items=N)`) to the transfer/delegate `chat_ctx.copy(...)` calls, keeping `exclude_instructions=True` (doc-correct) as the base (`livekitCtxExpr` [livekit_v1_build.go:773](internal/generate/livekit_v1_build.go:773); template ~138,~374,~389,~403). (b) Replace the canned `on_enter` strings ("You are now handling this. Continue in one short line; do not greet again." ~132/~197, "Begin this step." ~238/~262/~286) with a lighter, step-instruction-driven opening (the agent/task's own `instructions` drive it; consider `generate_reply(tool_choice="none")` per the recipe base class). Does not redefine dl§V3/V5 defaults. Regen Remy golden (recorded)|C2,C3,dl§V3,dl§V5,I.tmpl,I.build
T5|x|F4 fix (low priority). Add `uv run ruff format --diff .` to the L4 static-check gate (`livekit_v1_smoke_test.go` ~436, alongside `ruff check`/`ty check`) over every public example, asserting no diff (write-path already formats, C1). Raw-generator format-stability stays out of scope. Skips if uv absent|V3,dl§V26,I.smoke
T6|x|F5 investigation (VERIFY, concluded). `AgentTask[dict]` is functionally sound: the LLM sees the result schema through the `finish` tool's arg signature (not the generic), and `assign` reads `result["<field>"]` (dict indexing, [agent.py.tmpl](internal/generate/templates/livekit_v1/agent.py.tmpl) ~353) which stays correct with `dict`. DECISION: keep `AgentTask[dict]`; a generated typed result dataclass per task is scaffolding for a marginal ty-level typing gain (C5) and would force `assign` to attribute access. The one real fidelity loss (enum on `finish` result args) is folded into T2. No §B (no defect)|V2,dl§V1,I.tmpl
T7|x|F6 investigation (VERIFY, concluded). `then: end`/`effect: ends_conversation` call `self.session.shutdown()` (~377,~411,~24,~38); `AgentSession.shutdown(*, drain=True)` drains pending speech before closing (verified in `voice/agent_session.py` and docs `/agents/server/job/`), so the default already drains TTS. The voicemail leave-message path already `wait_for_playout()` then `ctx.shutdown()` (dl§V8). DECISION: no change required. Optional UX nicety (not blocking): speak a short closing line before the `then: end` shutdown. No §B (no defect)|dl§V8,I.tmpl

Dependency order: T1, T2 are priority, do first (both touch `agent.py.tmpl` and
regen the Remy golden; sequence T1 then T2 to avoid golden churn). T6 is settled
by T1+T2 (fold enum fidelity into T2). T3, T4 touch the template too; sequence
after T1/T2. T5 is independent (smoke only). T7 needs no build action. F2 is a
plain alignment task (T4), not an amendment.

## §B bugs
id|date|cause|fix
B1|2026-07-22|A completed single-task delegate re-runs: the owner LLM re-invokes the finished flow with no intervening user turn, replaying `create_customer`/`finish` and re-speaking each step, so side effects double-fire and state corrupts. Two causes, confirmed against docs.livekit.io + the trace, not the single-task context (which already matches the documented supervisor pattern + dl§C4 and needs no snapshot/restore, verified): (1) the `then: return` delegate docstring is generic (`delegateWhen` returns the control's `when` verbatim; finality text is appended only for `then: transfer`/`then: end` at [livekit_v1_build.go:628](internal/generate/livekit_v1_build.go:628)/`:630`, never for return), so the owner treats the delegate's `when` as a standing instruction and calls it again after it returns; (2) `finish` calls `self.complete({...})` AND then `return "Done."` with `-> str` ([agent.py.tmpl](internal/generate/templates/livekit_v1/agent.py.tmpl) ~426-430), emitting a stray function-tool output after the task is closed, against the upstream idiom (completion tools are `-> None`, no return). Found via a `console` repro of `examples/multi-task` (book as a new customer) + the Langfuse trace `livekit-multi-tasks.json`: two back-to-back `start_agent_activity`/`on_enter`->`on_exit` cycles with byte-identical `check_customer` results at 10:40:01 and 10:40:39, `create_customer` firing twice, and no `user_turn` between the cycles. Latent because L1-L3 goldens check text and L4 smokes only import/instantiate; a live trace surfaced it.|V1

# SPEC — Pipecat codegen fidelity + lint alignment

Scope: close the gaps between our generated Pipecat `bot.py` and the canonical
upstream examples (`examples/flows/food_ordering.py`,
`examples/multi-worker/local-handoff/local-handoff-two-agents.py`) and the
unmute tool model. **Not** a rewrite of the handoff or Flows lowering — those
already match upstream. This spec is subordinate to the authoritative driver
contract [driver-pipecat.md](docs/spec/driver-pipecat.md); references to it are
written `dp§V1`, `dp§C8`, etc. This file's own ids (V1, V2, T1…) are local.

## §G goal
Generated Pipecat code is **schema-faithful** to the unmute tool model (one
YAML tool = one JSON-Schema input, carried across both emission paths), **reads
like the upstream examples** (no scaffolding a single-agent bot never needs),
and **passes `ruff check --select F` + is stable under `ruff format`**. Fix the
gaps F1/F3/F4; confirm-then-decide F5/F6; F2's semantic change is rejected (C8) —
dp§V26's small-context task-role boundary is preserved.

## §C constraints
- C1 (amended per T3 decision): the **generator** emits Python **only** through
  embedded `text/template` (ADR-0002, dp§C1) — no hand-written Python in the
  generate package; every generator fix is a template (`bot.py.tmpl`) +
  build-model (`pipecat_v1_build.go`) change, golden-tested. dp§C1 stays intact:
  the generator is still template-only. **New:** the CLI **write path**
  (`writeArtifactFiles`, I.write) applies a best-effort `ruff format` pass to
  emitted `.py` before writing to disk (layout only, not the generator);
  `ruff` is an **optional** runtime tool — absent → warn to stderr, write the
  unformatted (still valid, F-clean) source, exit 0. Goldens capture the **raw**
  generator output (zero-Python L1–L3 preserved).
- C2: **doc wins** (CLAUDE.md). When code and a doc disagree, fix the code. F6
  is partly a doc-vs-code check, not only an upstream-idiom one.
- C3: **reconcile with [driver-pipecat.md](docs/spec/driver-pipecat.md)** — it
  is authoritative. No invariant here may silently contradict its dp§V1–V3,
  dp§V26–V28, or dp§C8. Changing locked behavior (F2) is an amendment to *that*
  file plus this one, never a quiet redefinition.
- C4: **unmute tool model is the fidelity north star**
  ([tools.md](docs/user/reference/tools.md)): a tool is one YAML file, filename
  = tool name; `input` is a JSON-Schema object with `type`, `enum`, per-property
  `description`, `required`. No new `execution` kinds, no new spec surface — this
  is faithful lowering of the model that already exists.
- C5: **clean, minimal output**. Reader is Pipecat-fluent. Lazy wins: no bus for
  a bot with one agent and no handoff; no restating framework defaults
  (`respond_immediately=True`, `@tool()`).
- C6: version floor unchanged — driver range `>=1.5 <2.0`
  ([pipecat_v1.go:33](internal/generate/pipecat_v1.go:33)), pin `==1.5.0`.
- C7 (non-goals): no LiveKit-target change; no provider-catalogue change; do not
  touch the handoff (`activate_worker`) or Flows (`FlowManager`) lowering beyond
  the gaps below.
- C8 (F2 decision — **preserve the small-context task-role boundary**): dp§V26 is
  the deliberate design (T26/B19, 2026-07-21) and the stated goal — *tasks need
  smaller context; the full agent prompt is not disclosed during a task.* A Flow
  node's `role_message` = the task's own step prompt and **replaces** the owner's
  `system_instruction`, so the task call never receives the persona
  ([how-targets-run-your-agent.md](docs/user/concepts/how-targets-run-your-agent.md)).
  **F2's upstream `food_ordering` layering is REJECTED**: putting the persona in a
  set-once `role_message` and the objective in `task_messages` would make every
  task see persona + objective — i.e. disclose the whole prompt on every task,
  the opposite of the goal and a revert of B19. The only goal-compatible residue
  F2 found — the agent prompt emitted as a **duplicated string literal** in the
  builder and each restore handler (golden `:110`,`:263`,`:319`) — is a code-DRY
  fix folded into T3 (name the prompt once, reference it), with **no change** to
  the replace-and-restore semantics or the runtime `LLMUpdateSettingsFrame`
  restore (that restore is the correct, necessary cost of the small-context
  design, not a defect).

## §I surfaces
- I.tmpl: [bot.py.tmpl](internal/generate/templates/pipecat_v1/bot.py.tmpl) —
  the single template emitting all four scenarios.
- I.build: [pipecat_v1_build.go](internal/generate/pipecat_v1_build.go)
  (`buildTool`, `inputFields`, `setImportNeeds`, `collectImportsExtras`) +
  [pipecat_v1.go](internal/generate/pipecat_v1.go) (`pipecatArg`, `pipecatTool`,
  `pyQuote`).
- I.gold: `internal/generate/testdata/golden/pipecat_v1_*.py` + `pipecat_v1.txt`
  (regen with `-update-pipecat`).
- I.write: [writeArtifactFiles](internal/cli/compile.go) — the shared CLI seam
  that writes a code-target project to disk (compile/dev). T3 adds the
  best-effort `ruff format` pass here (C1).
- I.smoke: L4 `make smoke` (build tag `smoke`, needs Python) — proves emitted
  Python is valid.

## §V invariants
- V1 (F1): **both tool-emission paths present the LLM the same schema the tool
  YAML declares.** Inside a Flow node a tool is a `FlowsFunctionSchema` with
  `properties`/`required` verbatim (enums, per-property descriptions, non-string
  types all preserved — `bot.py.tmpl` `FlowsFunctionSchema` block, golden
  `pipecat_v1_tasks_bot.py:241`). As an **agent-level `@tool`** the same tool is
  emitted as `async def name(self, params, arg: str)` (`bot.py.tmpl` `range
  .Tools` signature line ~165) whose schema is derived only from signature +
  bare `{{.Description}}` docstring — so enums (`service: [haircut, hair-color,
  blowout]`), per-property descriptions, and non-string types are lost.
  Root cause: `pipecatArg` carries only `Name`+`Required`
  ([pipecat_v1.go:139](internal/generate/pipecat_v1.go:139)); `inputFields`
  drops the rest while `buildTool` keeps the full schema only in
  `InputProps`/`InputRequired` for the Flow path
  ([pipecat_v1_build.go:507](internal/generate/pipecat_v1_build.go:507)).
  Aligned fix, cheapest first: (a) map spec `input` types → Python type hints
  (drop hardcoded `: str`); (b) emit a Google-style `Args:` docstring section so
  per-property descriptions flow into the derived schema; (c) represent enums as
  docstring prose — upstream does this because the direct-function generator
  does not map `Literal` → JSON-schema `enum` yet. Strict enums on agent-level
  tools are the author's opt-in via an explicit `FunctionSchema` bundled with the
  handler (documented escape hatch, not the default). Guard:
  `TestV1PipecatAgentToolCarriesSchema` — the same tool reaches the LLM with its
  declared type/description/enum on both the agent-level signature+docstring and
  the Flow-node `FlowsFunctionSchema`; the tasks golden shows the `Args:`
  descriptions previously lost (`lookup_customer`).
- V2 (F4, amended — the write path runs `ruff format`, C1): the emission is
  clean in two layers. (1) **Generator** (template-only): its raw output passes
  `ruff check --select F` — only used imports (`LLMWorker` gated to plain-worker
  bots — unused when agents subclass `TracedLLMWorker`; no `asdict`), same-module
  `from pipecat.frames.frames` imports merged, no other F-violations — plus the
  idiom fixes ruff cannot make: `@tool` not `@tool()`, no `respond_immediately=True`
  (the Flows default), and each agent prompt a single triple-quoted module
  constant referenced by builder + restore (dedup; C8-preserving). So the raw
  output is valid and F-clean even when ruff is absent. (2) **Write path**
  (`writeArtifactFiles`, I.write): a best-effort `ruff format` pass makes the
  on-disk `.py` byte-stable under `ruff format` (brace spacing, blank lines,
  line-wrapping — the data-dependent layout the template cannot make static).
  Byte-`ruff format`-stability of the *raw* generator output is **out of scope**
  (line-wrapping depends on runtime string length; C1 forbids formatting in the
  generator). Guard: `TestPipecatRuffCheckClean` (raw output is F-clean; skips if
  ruff absent) + `TestWriteArtifactFilesFormatsPython` (write path formats +
  stable; skips if ruff absent).
- V3 (F5, from B1): every generated agent-level `@tool` method resolves its
  function call via `await params.result_callback(...)`. Pipecat ignores a
  `@tool`'s **return value** ("Tool methods must call `params.result_callback()`
  … to avoid unresolved LLM calls" — Pipecat docs), so a handler that returns
  instead of calling the callback leaves the call unresolved and hangs the turn.
  The delegate `@tool` that spins up a `FlowManager` (`bot.py.tmpl` ~L205, golden
  `:230`,`:286`) returns `{"status": …}` and must resolve the call instead.
  Exempt: flow-internal `_flow_tool_*`/`_finish_*` handlers — they *return* per
  the Flows `ConsolidatedFunctionResult` contract and never take `params`. Guard:
  `TestV3PipecatToolsResolveCallback` — every emitted `async def <n>(self,
  params …)` body calls `params.result_callback`.
- V4 (F6, from B2): a `task_group` `then: end` lowers to the Flows built-in
  `end_conversation` post-action on the terminal node
  (`post_actions=[{"type": "end_conversation"}]`), matching dp§V2 ("then: end
  runs the end_conversation action", so C2 doc-wins) and upstream
  `food_ordering` — **not** a raw `await self.queue_frame(EndFrame())` in the
  finish handler (`bot.py.tmpl` ~L236). Every delegate `then:` branch
  (return/transfer/end) carries golden coverage, so an emitted-but-untested path
  can't drift again (the end path had none). Guard: a `then: end` golden fixture
  + assertion.
- V5 (F1, from B3): an agent-level `@tool` signature is valid Python — required
  params precede optional ones (Python forbids a defaulted arg before a
  non-defaulted one). `inputFields` orders required-first, then alphabetical
  within each group. Guard: `TestV1PipecatAgentToolCarriesSchema` uses a tool
  whose optional params sort before its required one and `py_compile`s the bot.

*(No F2 invariant — F2's semantic change is rejected, see C8. dp§V26 stays
authoritative. F2's only real residue, the duplicated prompt literal, is a
code-DRY fix inside T3/V2.)*

## §T tasks
id|status|desc|cites
T1|x|F1: thread the tool `input` schema into the agent-level `@tool` path. Extended `pipecatArg` with type/description/enum; `inputFields` populates them (required-first ordering, B3/V5); template renders typed signature + Google `Args:` docstring (descriptions carried, enums as prose). Both paths schema-equal; escape hatch (explicit `FunctionSchema`) documented in V1. Goldens regen|V1,V5,C4,I.build,I.tmpl,B3
T2|.|F3: collapse `len(agents)==1 && no agent_transfers` to the inline `food_ordering` shape — LLM inline in the pipeline, no bus / `BusBridgeProcessor` / `activate_entry` dance — scoped to the pipeline+`run_bot` section of the template only; agent class, tools, and flows blocks stay shared. Record the trade-off (one uniform shape vs. a second code path) in the PR. Reconcile with dp§V14 (activation gate applies only to the bus path). Regen `simple-prompt` golden|C5,C3,I.tmpl
T3|x|F4: two-layer clean output. Template/build: gate `LLMWorker` + drop `asdict` (F-clean), merge `frames.frames` imports, `@tool()`→`@tool`, drop `respond_immediately=True`, `json={...}` spacing, each agent prompt as one triple-quoted module constant referenced by builder + restore (dedup; C8-preserving). Write path (`writeArtifactFiles`, I.write): best-effort `ruff format` pass on emitted `.py`, warn to stderr if ruff absent (ADR-0002/C1 amended). Guards: `TestPipecatRuffCheckClean` + `TestWriteArtifactFilesFormatsPython` (both skip w/o ruff); regen goldens (raw)|V2,C8,I.tmpl,I.build,I.write
T4|x|F5 CONFIRMED defect (context7: Pipecat "tool methods must call `params.result_callback()` … to avoid unresolved LLM calls"). Fix: the delegate `@tool` resolves its call via `params.result_callback` instead of returning `{"status": …}` (`bot.py.tmpl` ~L205; golden `:230`,`:286`). Add `TestV3PipecatToolsResolveCallback`; regen tasks golden|V3,B1,I.tmpl
T5|x|F6 CONFIRMED (context7 + no test coverage): `then: end` queues `EndFrame()` (`bot.py.tmpl` ~L236), drifting from dp§V2 + upstream `food_ordering`; no example/golden exercises the path. Fix: emit `post_actions=[{"type": "end_conversation"}]` on the terminal node; add a `then: end` golden fixture + assertion. Verify end-node wiring against the pipecat-flows end pattern before finalizing|V4,B2,C2,C3,I.tmpl

Dependency order: T4, T5 are investigations, do first (cheap, may add §B rows the
fixes must respect). T1, T2, T3 all touch `bot.py.tmpl` — sequence to avoid
golden churn: T3 → T1 → T2. F2 is decided (rejected, C8); no task.

## §B bugs
id|date|cause|fix
B1|2026-07-22|delegate `@tool` (`run_collect`/`run_triage`) returns `{"status": …}` and never calls `params.result_callback`; Pipecat ignores a `@tool`'s return value, so the FlowManager delegate call is left unresolved and the LLM turn hangs (docs: tool methods *must* call `result_callback` "to avoid unresolved LLM calls"). Latent: the tasks golden only import-checks and the B7/T12 scripted-SSE spike didn't enforce tool_call/result pairing.|V3
B2|2026-07-22|`then: end` queues `EndFrame()` directly in the finish handler (`bot.py.tmpl` ~L236), contradicting dp§V2 ("end_conversation action") and upstream `food_ordering` (`post_actions=[{"type":"end_conversation"}]`). Undetected because no example/golden exercises `then: end` — the path is emitted but untested.|V4
B3|2026-07-22|`inputFields` sorted agent-level `@tool` args alphabetically, so a tool with an optional param sorting before a required one would emit `def f(opt=..., req)` — invalid Python (non-default arg after default). Latent: every example tool's input is all-required or all-optional, never mixed, so it never surfaced; F1's real type hints (`= 0`/`= ""`) make the ordering matter more than the old uniform `= ""`.|V5

# Tasks: MCP servers as tool sources

**Input**: Design documents from `/specs/008-mcp-tool-sources/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Included. The constitution makes them non-optional here: `make test`
is the merge gate, goldens pin emitted bytes, and two agreement tests
(emitter vs capability table) fail the moment the table and an emitter
disagree, so test tasks are part of each story, not an option.

**Organization**: Grouped by user story after a foundational phase. One honest
deviation from story independence, stated up front: the schema surface
(`transport`, `auth`, `tools`) is one struct and one validator, so it lands
once in Phase 2, and the two emitters land whole in US1 per
[contracts/emission.md](contracts/emission.md) — splitting the same template
lines across four stories would triple golden churn for nothing. US2/US3/US4
then verify their story's behaviour (omission rules, auth, transport) rather
than re-opening the emitters.

## Format: `[ID] [P?] [Story] Description`

---

## Phase 1: Setup

**Purpose**: Repository facts the later phases copy from.

- [X] T001 [P] Add `FIRECRAWL_MCP_URL=https://mcp.firecrawl.dev/v2/mcp` to the repo-root `.env` and `.env.example`, and document the `FIRECRAWL_API_KEY` name (never its value) in `.env.example`
- [X] T002 [P] Fix the stale comment at `internal/spec/package.go:213` — `inject` is legal on webhook and local only; the validator already rejects it on mcp

---

## Phase 2: Foundational (blocking prerequisites)

**Purpose**: The authoring surface, IR, and validation for the full `mcp:`
block per [data-model.md](data-model.md). Every story reads these shapes.
`make test` must be green at the end of this phase with Pipecat still denied.

**⚠️ CRITICAL**: No user story work before this phase completes.

- [X] T003 Grow `ToolMCP` in `internal/spec/package.go` with `Transport string`, `Auth *ToolAuth`, `Tools []string` (doc comments state: server owns per-tool contract; env names only)
- [X] T004 Add `MCPTransport string` and `MCPTools []string` to `ir.Tool` in `internal/ir/compiler.go`; extend the mcp arm of `buildTool` in `internal/ir/build.go` (~line 492) to copy `Transport`, `Tools`, and `Auth` through the existing `buildToolAuth`
- [X] T005 Two rule families, each in the layer that can report what FR-008 promises. Load shape in `internal/spec/tool_shape.go` (`checkToolShape` already scans top-level keys and knows the block kind): a top-level `description`/`input`/`output`/`inject`/`interruption`/`effect` key on an mcp file fails at load with file, line, and what to remove (FR-007). Validation in `internal/ir/validate.go`: transport ∈ {`sse`,`streamable_http`} naming both values on failure; widen the auth rule (~line 350) so `auth` is legal on webhook AND mcp and `validateToolAuth` runs for mcp; `tools` entries non-empty and unique; skip the `description is required` path for mcp (~line 366, now unreachable for mcp but the skip keeps it honest); add the `tools/<name>.yaml mcp.auth.token_env` reference site next to the existing url_env site (~line 1254)
- [X] T006 [P] L1 tests for T003-T005: round-trip the full block in `internal/spec/load_test.go`; load-shape cases in `internal/spec` tests asserting file:line for each illegal top-level field on an mcp file; table cases in `internal/ir/validate_test.go` for transport enum failure, literal secret value in `token_env` (SC-005), duplicate selection names, auth-on-mcp green path
- [X] T007 Scaffold: give `internal/scaffold/templates/tool.yaml.tmpl` an mcp branch that writes the block only (no description/input/interruption/effect, like the builtin branch); regenerate `internal/scaffold/testdata/golden/init.txt` with `-update` and read the diff
- [X] T008 TUI: keep the single `url_env` prompt (`internal/tui/tui.go:1059`); update the mcp flows in `internal/tui/tui_test.go` (~lines 736, 1686) and the console golden for the new file shape
- [X] T009 Regenerate the IR/schema goldens (`go test ./internal/ir -update`) and read the diff — the derived authoring and debug schemas must show the new fields with no hand-written JSON anywhere
- [X] T010 `docs/SCHEMA.md`: append amendment **N40** (dated, states the shape change and that old-shape mcp files fail strict decode); rewrite §5.1 (contract fields not legal on mcp), the §5.2 `mcp:` row (all four fields), and the §6.1 `sdk_language` note — same-commit with the code it describes. The pipecat-support claims (§7 skip list and matrix line, gate inventory ~line 832) move to T018 so the doc never outranks a validator that still denies

**Checkpoint**: full block loads, validates, and round-trips; Pipecat still
fails by name; `make test` green.

---

## Phase 3: User Story 1 — Connect an agent to a remote MCP server (P1) 🎯 MVP

**Goal**: Both shipped drivers emit the full tool source per
[contracts/emission.md](contracts/emission.md); a package with one mcp source
compiles and talks on both targets.

**Independent Test**: quickstart.md steps 1-3 with a minimal package: compile
both targets, `unmute dev` each over WebRTC, hear an answer built from an MCP
tool call.

### LiveKit driver

- [X] T011 [US1] Reshape `livekitMCPServer` in `internal/generate/livekit_v1.go` (~line 274) to carry `Name`, `URLEnv`, `Transport`, `Tools`, and the lowered auth expression; in `internal/generate/livekit_v1_build.go` replace the collapse-by-env logic (agents ~line 713, tasks ~line 905, `livekitMCPServers` ~line 804) with one entry per source in declaration order, feeding `AuthKinds` so the `_bearer`/`_api_key` helpers emit; add every mcp source's env names to the generated startup check (`REQUIRED_ENV`, today built from declared secrets only) so a missing `url_env` or `token_env` value is named before anything dials (FR-009)
- [X] T012 [US1] Rewrite the `mcp_mounts` define in `internal/generate/templates/livekit_v1/agent.py.tmpl` (~line 74) to emit `mcp.MCPToolset(id=..., mcp_server=mcp.MCPServerHTTP(...))` entries on the `tools` surface at both sites (~lines 515, 834), with the omission rules from contracts/emission.md (no `transport_type`/`allowed_tools`/`headers` argument when the source field is absent); keep the `mcp` import gated on `NeedsMCP`
- [X] T013 [US1] In `livekitDeps` (`internal/generate/livekit_v1_build.go` ~line 1289): when the package uses an mcp source, add the `mcp` extra and raise the floor to `>=1.6`, composing with the warm-transfer `>=1.6,<1.7` case — fixes the existing missing-extra import failure (research R2)
- [X] T014 [US1] Update `internal/generate/livekit_v1_test.go`: the exact-line assertion at ~line 1783 (now `MCPToolset`, not `mcp_servers=`), the parity fixture at ~line 1898, dependency-string assertions; regenerate goldens with `-update-livekit` and read the diff

### Pipecat driver

- [X] T015 [US1] Add an explicit `ToolMCP` branch to the pipecat builder (`internal/generate/pipecat_v1_build.go` ~line 927 and the agent/task call sites ~lines 759, 840) producing MCP-client template data — an mcp tool must never fall through to the webhook lowering (plan design note 1); ensure the source's env names land in the bot's `REQUIRED_ENV` (built from `.DevEnv`) so a missing value is named at startup (FR-009)
- [X] T016 [US1] Emit the client in `internal/generate/templates/pipecat_v1/bot.py.tmpl`: gated imports, one `MCPClient` per source with the startup transport chooser (`ponytail:` comment naming the `/mcp` rule, research R5), `await start()` during setup, `register_tools(llm)`, `standard_tools` joined into the `LLMContext` tools (agent scope), `close()` on every existing shutdown path
- [X] T017 [US1] Add the `mcp` extra to the pipecat dependency string when used (`pipecat-ai[mcp,...]==<version>`, `internal/generate/pipecat_v1_build.go` extras collection)
- [X] T018 [US1] Lift the gate in the same commit as T015/T016: remove `deny(Pipecat, ...)` from `FieldToolMCP` in `internal/target/table.go` (~line 331), add `targetcap.FieldToolMCP: true` to `pipecatEmittedFields` (`internal/generate/pipecat_v1.go` ~line 414), update the pinned-message tests `internal/ir/validate_test.go` (~line 727) and `internal/target/table_test.go` (~line 217), and update the `docs/SCHEMA.md` pipecat-support claims deferred from T010 (§7 skip list, matrix line `mcp tools`: pipecat `gated (v1)` → `ok`, gate inventory ~line 832) in this same commit
- [X] T019 [US1] Pipecat emitter tests + goldens: MCP coverage in `internal/generate/pipecat_v1_test.go` (full-block emission, never-webhook-fallback), regenerate with `-update-pipecat` and read the diff

### Both targets

- [X] T020 [US1] Emitted README templates `internal/generate/templates/livekit_v1/README.md.tmpl` and `internal/generate/templates/pipecat_v1/README.md.tmpl`: an MCP section stating the per-platform unreachable-server behaviour (LiveKit logs and starts; Pipecat start raises and exits) and the selection-name caveat — three-places rule, leg 1
- [X] T021 [US1] Extend the L4 smoke tests `internal/generate/livekit_v1_smoke_test.go` and `internal/generate/pipecat_v1_smoke_test.go` (build tag `smoke`) so a generated project with an mcp source imports and instantiates on both pinned SDKs — this is the proof gate for the stale pipecat checkout caveat (research R3)

**Checkpoint**: MVP. A one-source package compiles and speaks on both targets.

---

## Phase 4: User Story 2 — Pick which tools the agent sees (P2)

**Goal**: Selection lowers to exactly-N tool names; no selection means all.

**Independent Test**: acceptance scenarios US2-1/US2-2 via emitted bytes.

- [X] T022 [US2] Assertions in `internal/generate/livekit_v1_test.go` and `internal/generate/pipecat_v1_test.go`: with `tools: [a, b]` the emission carries exactly `allowed_tools=["a", "b"]` / `tools_filter=["a", "b"]`; with no `tools:` neither argument appears at all (SC-004, never an empty list); two sources naming the same `url_env` emit two independent mounts/clients, each with its own selection (the spec's same-address edge case — the exact case the old collapse-by-env code merged)

---

## Phase 5: User Story 3 — Authenticate to a protected server (P2)

**Goal**: Bearer and api_key headers reach every request; secrets stay env names.

**Independent Test**: acceptance scenarios US3-1/2/3 via emitted bytes + validate.

- [X] T023 [US3] Assertions in both driver test files: `headers=_bearer("...")` / `headers=_api_key("...", "...")` emitted and the helper defined exactly once per used scheme; no `headers` argument when auth is absent; grep-style assertion that no secret value string can appear in any generated file or `compile-report.json`; L2 `validate` command test that a literal token value fails before any artifact (SC-005) in `internal/cli`

---

## Phase 6: User Story 4 — Choose the transport (P3)

**Goal**: Stated transport always wins; absent transport follows the one `/mcp` rule on both targets.

**Independent Test**: acceptance scenarios US4-1/2/3 via emitted bytes + validate.

- [X] T024 [US4] Assertions in both driver test files: explicit `transport: sse` and `transport: streamable_http` each emit their exact argument/params class; absent transport emits no `transport_type` (LiveKit) and emits the startup chooser (Pipecat); validation test for a bad transport value naming both legal values

---

## Phase 7: User Story 5 — Scope MCP tools to a task or an agent (P3)

**Goal**: A source listed on a task is offered only inside that task, on both targets.

**Independent Test**: acceptance scenarios US5-1/2 via emitted bytes for a
fixture with one agent-scoped and one task-scoped source.

- [X] T025 [US5] LiveKit task scoping: confirm the per-source mounts from T011 flow through the `livekitTask` path (template site ~line 834); test in `internal/generate/livekit_v1_test.go` that a task-listed source appears on the task's tools surface and not the agent's
- [X] T026 [US5] Pipecat task scoping: **not emittable** (research R11, found while implementing). A Flows node advertises only `FlowsFunctionSchema`/direct functions and `MCPClient` exposes no public per-tool handler, so instead: new capability field `FieldToolMCPTask` denying Pipecat in `internal/target/table.go`, the walk that declares it in `internal/ir/validate.go`, a loud driver-level error in `buildTask` so an IR built in code cannot fall through, and the gate test in `internal/ir/validate_test.go`. Recorded in spec FR-005/US5, plan note 7, contracts/emission.md, SCHEMA N40 + §7 matrix, and both target pages

**Checkpoint**: all five behaviour stories hold on both targets; full gate green.

---

## Phase 8: User Story 6 — Learn from a runnable example (P2)

**Goal**: `examples/mcp-example` compiles to both targets and answers a spoken
web search through Firecrawl, verified live with root credentials.

**Independent Test**: quickstart.md end to end from a clean checkout.

- [X] T027 [US6] Create `examples/mcp-example/`: `agent.yaml` (one agent, plain instructions to answer using web search, `secrets:` declaring `FIRECRAWL_API_KEY`, tools list naming `web_search`, and model bindings copied from `examples/simple-prompt/agent.yaml` so the root `.env` credentials suffice), `instructions.md`, `targets.yaml` (livekit `1.6.4` + `sdk_language: python`, pipecat `1.5.0`), and `tools/web_search.yaml` with the full block from [contracts/authoring.md](contracts/authoring.md) (explicit `transport: streamable_http` to teach the field)
- [X] T028 [US6] Write `examples/mcp-example/README.md` (what it shows, the copy-credentials flow from repo root, both `bin/unmute dev` commands, the search demo script) and register the example: add it to the pinned directory list in `internal/generate/examples_test.go` (~line 251) and to the examples index page `examples/README.md` — same commit, the list test fails otherwise
- [X] T029 [US6] Docs sweep, three-places rule leg 3 (research R9): `docs/user/learn/02-add-a-tool.md`, `docs/user/targets/livekit.md` ("Use an MCP tool"), `docs/user/targets/pipecat.md`, `docs/user/reference/tools.md`, `docs/user/reference/safe-core.md`, `docs/user/reference/secrets.md`, `docs/user/README.md` — every MCP fact matches N40 and the example; links into `examples/mcp-example` resolve (the link test enforces this)
- [X] T030 [US6] Live verification per [quickstart.md](quickstart.md) steps 2-4: compile, copy root credentials, `bin/unmute dev examples/mcp-example --target livekit` then `--target pipecat`, speak a query that triggers `firecrawl_search`, confirm the selection shows exactly one tool and no errors are displayed (SC-003, SC-004, SC-007); note results in the PR description

**Checkpoint**: the named deliverable exists and is proven live.

---

## Phase 9: Polish & cross-cutting

- [X] T031 Full gate in order: `make fmt && make lint && make build && make test`, then `make smoke` (uv present) for the L4 MCP fixtures — quickstart.md step 1/5
- [X] T032 [P] Re-read `specs/008-mcp-tool-sources/checklists/requirements.md` against the shipped behaviour and re-run the spec's success criteria list SC-001..SC-007; fix any doc drift found (`docs/REPO_MAP.md` mention if the new example warrants it)

---

## Dependencies & execution order

- **Phase 1 → Phase 2 → Phase 3**: strictly ordered. T018 must land in the
  same commit as T015/T016 (the deny is the only guard against the webhook
  fallback — plan design note 1).
- **Phases 4, 5, 6 (US2/US3/US4)**: depend on Phase 3; independent of each
  other, all three parallelizable.
- **Phase 7 (US5)**: depends on Phase 3 (T011, T015/T016).
- **Phase 8 (US6)**: T027/T028 depend on Phase 3 (the example must compile);
  T029 can draft in parallel; T030 last, after everything it demonstrates.
- **Phase 9**: last.

Within Phase 3, the LiveKit chain (T011→T012→T013→T014) and the Pipecat chain
(T015→T016→T017→T018→T019) are independent of each other and can run in
parallel; T020/T021 close the phase.

## Parallel example: after Phase 2

```text
Stream A (LiveKit):  T011 → T012 → T013 → T014
Stream B (Pipecat):  T015 → T016 → T017 → T018 → T019
Then:                T020, T021
Then in parallel:    T022 · T023 · T024 · T025+T026 · T027+T028+T029
Finally:             T030 → T031 → T032
```

## Implementation strategy

**MVP first**: Phases 1-3 (T001-T021), then stop and validate with a minimal
package over `unmute dev` on both targets. That alone delivers US1 and most of
the feature's value.

**Incremental delivery**: each later phase is a small, independently green
increment; the example (Phase 8) is the demo gate, and T030's live run is the
last thing before the PR.

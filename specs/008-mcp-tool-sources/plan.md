# Implementation Plan: MCP servers as tool sources

**Branch**: `008-mcp-tool-sources` | **Date**: 2026-08-13 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/008-mcp-tool-sources/spec.md`

## Summary

Grow the `mcp:` tool block from one field (`url_env`) into a full tool-source
declaration (`transport`, `auth`, `tools` selection), make the file's
top-level per-tool contract illegal on mcp (the server owns tool schemas),
lift the Pipecat maturity gate by emitting a persistent `MCPClient`, move the
LiveKit emission off the deprecated `mcp_servers=` onto `MCPToolset`, and ship
`examples/mcp-example`: a WebRTC-only package that answers spoken questions
through Firecrawl's `firecrawl_search`, compiled and run on both targets with
credentials copied from the repository root. Lands as SCHEMA.md amendment N40.

Design inputs: [research.md](research.md) (all provider claims verified
2026-08-13), [data-model.md](data-model.md), [contracts/](contracts/),
[quickstart.md](quickstart.md).

## Technical Context

**Language/Version**: Go 1.24 (compiler); generated Python via `text/template` only

**Primary Dependencies**: unchanged Go set (cobra, goccy/go-yaml,
jsonschema-go, Charm stack) — **no new Go dependency**. Generated projects
gain SDK extras only: `livekit-agents[mcp,...]>=1.6` (MCPToolset verified at
1.6.4) and `pipecat-ai[mcp,...]==<pinned>` (`mcp[cli]>=1.11,<2`)

**Storage**: N/A (stateless compiler; server connections live in the generated project)

**Testing**: L1 table-driven unit, L2 in-process command tests, L3 goldens
(`-update-livekit`, `-update-pipecat`), L4 opt-in smoke (`make smoke`, proves
the new MCP constructor shapes against pinned SDKs), plus the live quickstart
run against Firecrawl

**Target Platform**: macOS/Linux CLI; generated projects run in Docker (web
dev) or uv (console)

**Project Type**: compiler CLI with generated-code targets

**Performance Goals**: N/A — compile-time feature; runtime latency belongs to
the platforms and is not wrapped

**Constraints**: `make test` stays zero-Python; no secret value in any file or
report; generated projects carry no Unmute dependency; fail loud on every
unsupported combination

**Scale/Scope**: 2 spec/IR structs, 1 capability row, 2 driver emitters + 2
templates, scaffold + TUI touch-ups, 1 new example, ~9 doc pages, N40

## Constitution Check

*GATE: evaluated before Phase 0, re-checked after Phase 1 design. Version 2.0.0.*

| Principle | Verdict | Evidence |
|---|---|---|
| I. Compile AOT, never interpret | PASS | Emission uses each platform's own MCP primitives (`MCPToolset`, `MCPClient`); no runtime layer, no Unmute dependency in artifacts. The pipecat transport chooser (R5) is 2 lines of generated stdlib Python, marked with a `ponytail:` comment. |
| II. Fail loud, never average | PASS | New validations are errors, never silent: transport enum, contract fields illegal on mcp, `token_env` UPPER_SNAKE. Deepgram deny stays verbatim. Selection names that don't exist server-side cannot be checked at compile time — documented in the emitted runbook rather than silently implied (spec edge case). Fixes an existing silent failure: LiveKit MCP projects today miss the `mcp` extra and die on import (R2). |
| III. One source of truth | PASS | Structs stay the schema source (no hand-written JSON). Capability row changes in `internal/target/table.go` only; `pipecatEmittedFields` agreement test updated in the same commit, so the emitter cannot lag the table. Auth reuses `ToolAuth` and the emitted `_bearer`/`_api_key` helpers — no second auth shape (R6). |
| IV. The document wins | PASS | N40 amendment states the shape change and the strict-decode break for old mcp files. Every SDK claim carries a 2026-08-13 verification date and source (research.md header). Pipecat checkout staleness is declared, with L4 smoke as the proof gate (R3 caveat). |
| V. Whatever compiles can be spoken to | PASS | `examples/mcp-example` is the deliverable: `unmute dev` over WebRTC on both targets, verified with root credentials (quickstart.md). No telephony surface touched. |
| Targets and providers | PASS | Pipecat/LiveKit gain generation; Vapi stays validate-only (Core row untouched); Deepgram stays denied by name. No new target, no vendor/target blur. |
| Complexity | PASS | No new Go dep, no new abstraction, no config knob. Net deletion in LiveKit build (collapse-by-env logic goes away, R7). |

**Post-design re-check (after Phase 1)**: no new violations introduced by the
contracts; Complexity Tracking stays empty.

## Project Structure

### Documentation (this feature)

```text
specs/008-mcp-tool-sources/
├── plan.md              # This file
├── research.md          # Phase 0 — decisions R1-R10, all dated
├── data-model.md        # Phase 1 — spec/IR struct changes + validation rules
├── quickstart.md        # Phase 1 — end-to-end validation guide
├── contracts/
│   ├── authoring.md     # the mcp: block contract (N40 source text)
│   └── emission.md      # per-driver generated-code contract
└── tasks.md             # Phase 2 (/speckit-tasks — not created here)
```

### Source Code (repository root)

```text
internal/spec/package.go            # ToolMCP grows Transport, Auth, Tools
internal/spec/tool_shape.go         # contract fields illegal on mcp, at load with
                                    # file:line (FR-007/FR-008)
internal/ir/compiler.go             # ir.Tool: MCPTransport, MCPTools
internal/ir/build.go                # mcp arm copies the new fields via buildToolAuth
internal/ir/validate.go             # transport enum · auth-on-mcp · tools list
                                    # checks · new token_env reference site
internal/ir/variables.go            # inject-on-mcp reason (unchanged, comment fix)
internal/target/table.go            # drop deny(Pipecat) on FieldToolMCP
internal/generate/livekit_v1.go     # livekitMCPServer → full source shape
internal/generate/livekit_v1_build.go   # per-source mounts replace collapse-by-env;
                                        # mcp extra + >=1.6 floor in livekitDeps
internal/generate/templates/livekit_v1/agent.py.tmpl   # MCPToolset emission
internal/generate/pipecat_v1.go     # pipecatEmittedFields += FieldToolMCP
internal/generate/pipecat_v1_build.go   # explicit ToolMCP branch (today it would
                                        # silently emit mcp as a webhook POST)
internal/generate/templates/pipecat_v1/bot.py.tmpl     # MCPClient emission
internal/generate/templates/*/README.md.tmpl           # runbook: MCP behaviour
internal/scaffold/templates/tool.yaml.tmpl  # mcp branch writes the block only
internal/tui/tui.go                 # url_env prompt unchanged; no illegal file
examples/mcp-example/               # agent.yaml · targets.yaml · tools/web_search.yaml · README.md
.env / .env.example (repo root)     # FIRECRAWL_MCP_URL joins FIRECRAWL_API_KEY
docs/SCHEMA.md                      # §5.1 · §5.2 · §7 · N40
docs/user/…                         # 7 pages listed in R9
```

**Structure Decision**: existing layout, no new packages, no new files outside
the example and its docs. The compile flow stays `spec.Load` → `ir.Build` →
`ir.Validate` → `generate.Generate`; every change slots into a stage that
already exists.

## Design notes the tasks phase must not lose

1. **Order of operations is safety-relevant**: the Pipecat capability deny is
   the only thing standing between an mcp tool and
   `pipecat_v1_build.go:927`'s webhook fallback, which would emit an HTTP POST
   to the MCP URL. The `ToolMCP` branch in the pipecat builder must land
   before or with the table change, never after.
2. **Existing tests that pin the old world**:
   `internal/generate/livekit_v1_test.go:1783` asserts the exact
   `mcp_servers=[...]` line; `internal/ir/validate_test.go:727` asserts the
   Pipecat deny message; `internal/target/table_test.go:217` pins the
   condition; `internal/generate/pipecat_v1_test.go:1431` is the emitter
   agreement test. All change in the same commits as the behaviour.
3. **Goldens**: no golden currently contains MCP output. The parity fixtures
   that inject `ir.Tool{Execution: ToolMCP}` get the new fields; regenerate
   with `-update-livekit` / `-update-pipecat` and read the diff.
4. **Example registration**: `examples_test.go:251` pins the example
   directory list — `mcp-example` must be added there, which automatically
   puts it under `TestPublicExamplesValidateAndGenerate` and the README
   link/transport tests.
5. **Stale comment fix rides along**: `internal/spec/package.go:213` claims
   inject is legal on mcp; the validator says otherwise. Fix the comment.
6. **Root `.env.example`** gains `FIRECRAWL_MCP_URL` (value, not secret) and
   documents `FIRECRAWL_API_KEY` (name only), so the quickstart copy step is
   real.

7. **Found during implementation (2026-08-14)**: pipecat cannot scope an MCP
   source to a task (research R11). The plan assumed a template edit; the SDK
   has no supported surface for it. Shipped instead: agent scope on both
   targets, and a new capability field `tasks.tools.execution.mcp` denying
   pipecat by name. Spec FR-005 and US5 carry the amendment; SCHEMA N40, the
   §7 matrix, both target pages, and the example README all state it.

## Complexity Tracking

No constitution violations to justify. No new dependencies, no new
abstractions, one deliberate simplification (the emitted transport chooser,
R5) marked with a `ponytail:` comment naming its rule.

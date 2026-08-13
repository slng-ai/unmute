# Implementation Plan: Pipecat carrier telephony, bring your own number to the Daily route

**Branch**: `feature/warm-cold-human-transfer` | **Date**: 2026-08-12 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/006-pipecat-carrier-telephony/spec.md`

## Summary

Give the Pipecat Daily route a carrier leg, Twilio first, so an operator's own number reaches a deployed Pipecat Cloud agent and the agent dials out and cold-transfers through the operator's own trunk. The work is: one new route row `(pipecat, daily-sip, twilio)` in the capability rulebook with the mixed Connection key set (`account_sid`, `auth_token`, `sip_address`, `from_number`); one new emitted artifact (`telephony_helper.py`, the inbound webhook and outbound trigger server); a carrier block in the emitted bot (forward-once on the ready event, SIP-URI dial legs composed at the carrier's termination address); a specs/005-style two-part runbook in the emitted README; the deliberate inversion of the guards that today keep connections and phone channels off the Daily route; a schema amendment superseding N34's channel rejection; a new example; and the doc sweep. The no-carrier Daily route stays byte-identical.

Two findings from research reshape details a reasonable implementer would otherwise guess wrong. First, the platform's start endpoint cannot mint the interconnect room usefully (its response carries no SIP endpoint), so the helper creates the room itself and starts the agent with `createDailyRoom: false`, exactly as the official example does. Second, Daily's dial-out has no SIP credential support anywhere, so carrier termination authenticates by IP allow-list and the Connection deliberately carries no `sip_username` or `sip_password`.

## Technical Context

**Language/Version**: Go 1.24 (pinned in `go.mod`); emitted artifacts are `text/template` output (Python, Markdown, TOML, env)

**Primary Dependencies**: no new Go dependency. The emitted project, only when a carrier is declared, gains the helper's server dependencies in `pyproject.toml` (FastAPI and uvicorn unless the pinned pipecat extras already carry them; task-time check against pipecat 1.5.0). No carrier SDK: the one forwarding request uses the HTTP client already in the dependency tree (research D3)

**Storage**: none. No Redis on this route; the transfer guard stays in-process as specs/004 built it

**Testing**: `go test ./...` (L1 unit, L2 in-process command, L3 golden), zero Python in the default suite; `ruff` over emitted Python via the examples lint test; `make smoke` opt-in

**Target Platform**: darwin/linux CLI; emitted project deploys to Pipecat Cloud, helper runs wherever the operator chooses (locally with a tunnel for testing)

**Project Type**: compiler CLI with template-emitted artifacts

**Performance Goals**: not applicable; compile-time and operator-time feature. The one runtime timing promise is spec SC-004 (no silence over 2 seconds on the inbound path), carried by hold audio, not by code speed

**Constraints**: Go templates only, no post-generation string surgery; no secret value in any emitted file; goldens read, never regenerated blind; every platform claim carries source and verification date (research verification log); plain wording, no em or en dashes in emitted text or docs

**Scale/Scope**: one new route row; 1 new template (`telephony_helper.py.tmpl`), 3 templates extended (`bot.py.tmpl`, `README.md.tmpl`, `pyproject.toml.tmpl`), ~8 Go files including the scaffold and the console, ~14 test files, 1 new example, SCHEMA amendment N37, 7 docs. The largest hidden cost is narrowing the ~24 sites that today read a non-nil telephony plan as "carrier-websocket"

## Constitution Check

*GATE: evaluated against constitution v2.0.0 before Phase 0; re-checked after Phase 1.*

| Principle | Check | Status |
|---|---|---|
| I. Compile ahead of time | The helper is an emitted artifact the operator runs; the compiler provisions nothing and works offline. The generated project keeps zero Unmute dependency. The helper and the carrier block in the bot are template output, no maintained Python. | PASS |
| II. Fail loud, never average | The helper refuses to start with any missing value named. The route row makes the Connection key set a validation gate (unknown or missing key fails with the route named). The route grants only capabilities with an emitted code path, so nothing can validate green and do nothing: the call sources are refused by name because their fill path belongs to the adapter this route does not emit (research D11). Forwarded-unchecked values (region, the optional room geography) appear in the compile report. Caller identity on carrier dial-out is stated as carrier-governed and unverified rather than promised. | PASS |
| III. One source of truth | The route row in `internal/target/telephony.go` is the one home for the key set, features, and manual steps; the emitter and validate read it. The runbook text lives once in `README.md.tmpl`, keyed off the carrier. The agreement test between emitter and table extends to the new row rather than being weakened. | PASS |
| IV. The document wins | SCHEMA amendment (next free number, dated, appended) supersedes N34's statement that the Daily route takes no phone channel; the old text stays as history. Every platform claim carries the research log's sources, read 2026-08-12. The tests that enforce N34 today are inverted in the same change as the amendment. | PASS |
| V. Whatever compiles can be spoken to | The carrier target compiles to a deployable project plus a runnable helper, and the README takes the operator to a live call. `unmute dev --telephony` keeps refusing on daily-sip with a message that stays true for both forms (research D6). Browser and console modes unchanged. | PASS |
| Telephony boundary | The CLI never touches the carrier: carrier steps are dictated in the runbook. The one carrier request is the generated agent moving a live call the carrier just handed it, which is call control and not provisioning, so it sits inside the boundary as written rather than needing an exception. No number purchase, no trunk creation, no webhook writing by the CLI. | PASS |
| Secrets | Env names only, everywhere: the Connection carries names, the helper reads values at start, the runbook and reports print names. The termination address is treated like the LiveKit route treats it (a name in the env file). | PASS |

**Post-design re-check (after Phase 1)**: no new violations. The two justified additions are in Complexity Tracking.

## Complexity Tracking

| Addition | Why needed | Simpler alternative rejected because |
|---|---|---|
| Emitted `telephony_helper.py` (new artifact, new emitted deps when carrier declared) | Interconnect SIP addresses are per room and rooms are per call (research R4, R5), so no static carrier forwarding target exists; something must answer the webhook, mint the room, start the agent, and hold the caller | Letting the start endpoint mint the room fails structurally: its response has no SIP endpoint, so the call could never be forwarded (research F1). Hosting the webhook inside the deployed agent contradicts how the platform starts agents (specs/004) |
| Mixed Connection vocabulary on one route (`account_sid`, `auth_token`, `sip_address`, `from_number`) | The route genuinely spans two carrier surfaces: REST to forward the inbound call, SIP for the outbound legs. Daily offers no SIP credential auth, so `sip_username`/`sip_password` have nothing to carry (research F3) | Reusing the LiveKit four-name set would declare credentials nothing can use and omit the REST pair the forwarding action cannot work without |

No new Go dependency, no new authoring field, no new abstraction in the compiler.

## Project Structure

### Documentation (this feature)

```text
specs/006-pipecat-carrier-telephony/
├── plan.md              # This file
├── research.md          # Phase 0: F1 to F4 and D1 to D10, verification log
├── data-model.md        # Phase 1: entities, template data deltas, call lifecycles
├── quickstart.md        # Phase 1: offline and live validation runs
├── contracts/
│   ├── runbook.md            # README "Telephony setup" section contract
│   ├── forwarding-helper.md  # telephony_helper.py behavior contract
│   └── environment.md        # env surface: who reads which names, before and after
└── tasks.md             # Phase 2 (/speckit-tasks)
```

### Source Code (repository root)

```text
internal/target/telephony.go            # add the (pipecat, daily-sip, twilio) row: key set, features
                                        # (provisional), helper process, manual steps; refresh the
                                        # daily_dialout prerequisite summary to cover SIP dial-out
internal/ir/build.go                    # connection+channel pairing now legal on daily-sip with a
                                        # carrier; key set validated against the new row
internal/ir/validate.go                 # telephony plan check satisfied by the helper process; no
                                        # weakening of the no-process guard
internal/generate/pipecat_v1_build.go   # buildPipecatTelephony learns the daily-sip carrier form;
                                        # humanTransferTool refusal narrows (SIP-URI toEndPoint per
                                        # research F2); helper data; env additions
internal/generate/pipecat_v1.go         # render telephony_helper.py for carrier builds; report rows;
                                        # emitted-telephony-features map for the new key; narrow the
                                        # carrier-websocket artifact branch from "Telephony != nil" to
                                        # the transport (one of ~24 such gating sites, research item 0)
internal/scaffold/scaffold.go           # the daily-sip branch: does the scaffold offer the carrier form?
internal/tui/tui.go                     # the console's transport and carrier prompts for this route
                                        # (both mandated by the constitution's compliance review, FR-002a)
internal/generate/telephony_agreement_test.go  # the switch gains the new key or the agreement test fails
internal/cli/dev.go                     # daily-sip refusal message split: carrier form names the helper
internal/generate/templates/pipecat_v1/telephony_helper.py.tmpl  # new emitted artifact (D1, D2, D4)
internal/generate/templates/pipecat_v1/bot.py.tmpl   # carrier block: forward-once on ready (D3);
                                                     # SIP-URI dial legs on carrier targets (F2)
internal/generate/templates/pipecat_v1/README.md.tmpl # "Telephony setup" runbook, two parts, counts
                                                      # stated (contracts/runbook.md)
internal/generate/templates/pipecat_v1/pyproject.toml.tmpl # helper deps, carrier-gated
examples/human-transfer-daily-twilio/   # new example: carrier target, connection, phone channels,
                                        # cold transfer, capacity (D9)
internal/spec/authoring_surface_test.go, internal/ir/build_test.go,
internal/ir/validate_test.go, internal/generate/pipecat_v1_test.go,
internal/generate/examples_test.go, internal/target/table_test.go,
internal/target/telephony_test.go, internal/cli/dev_test.go,
internal/target/user_docs_test.go       # deliberate inversions and extensions (research "must not break")
internal/generate/testdata/golden/pipecat_v1*.txt  # regenerate deliberately, read the diff
docs/SCHEMA.md (amendment superseding N34's channel rejection), docs/TRANSFERS.md,
docs/TELEPHONY.md, docs/user/targets/pipecat.md, docs/user/learn/07-phone-calls.md,
docs/user/reference/controls.md, docs/user/reference/targets-yaml.md  # doc sweep (FR-016)
```

**Structure Decision**: existing compiler layout; one new template file and one new example directory are the only new source artifacts.

## Phase Summary

- **Phase 0** ([research.md](research.md)): the spec's four open facts resolved against the official docs and the official example source, eleven design decisions, a twelve-row verification log, and the list of guards and gating sites the implementation must handle deliberately. No NEEDS CLARIFICATION remain.
- **Phase 1** ([data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)): the route row and template data deltas, the three operator-facing contracts (runbook, helper, environment), and the validation runs, offline and live.

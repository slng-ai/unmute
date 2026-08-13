# Implementation Plan: Telephony setup, one YAML block and a dictated runbook

**Branch**: `feature/warm-cold-human-transfer` | **Date**: 2026-08-12 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/005-telephony-setup/spec.md`

## Summary

Make a generated LiveKit package's telephony fully settable from its own README: a two-part runbook (dictated Twilio steps, then one emitted provisioning script for the LiveKit side), and retire `LIVEKIT_SIP_INBOUND_TRUNK` from every emitted surface by resolving the inbound trunk by phone number. No agent code changes. No authoring surface changes. The work is: one new emitted artifact (`telephony-setup.sh`), a rewritten README telephony section, the placeholder rename in `sip-dispatch-rule.json`, the removal of the dev-supplied-environment plumbing that existed only for the retired name, and the doc sweep.

Two boundaries settled during design and worth stating up front, because both are places where a reasonable implementer would guess wrong. The runbook belongs to the **SIP** branch only: the connector route carries the inbound feature but has no SIP trunk, so it neither gets the script nor the runbook. And the runbook is **Cloud-shaped**, with the single self-hosted difference (the origination target) kept as a labelled note beside that one step rather than as a second runbook.

## Technical Context

**Language/Version**: Go 1.24 (pinned in `go.mod`); emitted artifacts are `text/template` output (Markdown, JSON, POSIX shell)

**Primary Dependencies**: `spf13/cobra`, `goccy/go-yaml`, `google/jsonschema-go`, Charm TUI stack. No new Go dependency. The emitted script's operator prerequisites are `lk` (already required) and `jq` (new, dictated in the runbook)

**Storage**: none

**Testing**: `go test ./...` (L1 unit, L2 in-process command, L3 golden), zero Python in the default suite; `make smoke` opt-in

**Target Platform**: darwin/linux CLI; emitted project deploys to LiveKit Cloud or self-hosted LiveKit

**Project Type**: compiler CLI with template-emitted artifacts

**Performance Goals**: not applicable; compile-time and operator-time feature

**Constraints**: Go templates only, no post-generation string surgery; no secret value in any emitted file; goldens read, never regenerated blind; every platform claim carries source and verification date; plain wording, no em or en dashes in emitted text or docs

**Scale/Scope**: one driver (LiveKit), three SIP carrier routes share the change; 6 Go files, 1 template, 1 new template, 8 test files, 7 docs, 1 example README; SCHEMA amendment N36

## Constitution Check

*GATE: evaluated against constitution v2.0.0 before Phase 0; re-checked after Phase 1.*

| Principle | Check | Status |
|---|---|---|
| I. Compile ahead of time | The script is an emitted artifact the operator runs; the compiler still provisions nothing and works offline. The generated project keeps zero Unmute dependency (`lk` + `jq` + shell only). | PASS |
| II. Fail loud, never average | The script guards every step: missing prerequisite, missing env value, unresolvable trunk each stop with a named message before anything is created. The one forbidden silent outcome (a dispatch rule matching every trunk) is structurally impossible: `trunk_ids` is always present in the emitted JSON, and an unsubstituted placeholder scopes the rule to a nonexistent ID, which matches nothing. | PASS |
| III. One source of truth | The runbook text lives once, in `README.md.tmpl`, keyed off the carrier the route already names. The capability table stays the single telephony rulebook; its `ManualSteps` text is updated, not duplicated. The dev-supplied-environment plumbing is deleted rather than left as a second, now-empty description. | PASS |
| IV. The document wins | SCHEMA amendment N36 (dated, appended) records the retirement and the new artifact. All platform claims carry the V1 to V8 table's sources, read 2026-08-12. The stale `UNVERIFIED` note on the SIP JSON shapes (verified 2026-07-20 without MCP) is refreshed to 2026-08-12 with MCP. | PASS |
| V. Whatever compiles can be spoken to | `unmute dev --telephony` keeps provisioning local records automatically, re-keyed off the route's inbound feature evidence instead of the retired env name. The one real hazard is `dev_telephony.go:160`, which gates the two-phase startup rather than merely reporting environment: its condition moves, its behavior does not, and a test pins it. | PASS |
| Telephony boundary | Unchanged and load-bearing: the CLI never touches the carrier. The runbook dictates carrier steps; the script touches only LiveKit records, run by the operator, exactly as `lk` commands did before. | PASS |
| Secrets | The script echoes no values, reads only names the package already declares, and the phone number (not a secret, but treated carefully) appears only in `lk` arguments. `.env` stays gitignored and unread by the compiler. | PASS |

**Post-design re-check (after Phase 1)**: no new violations. The one new operator prerequisite (`jq`) is justified below rather than silently added.

## Complexity Tracking

| Addition | Why needed | Simpler alternative rejected because |
|---|---|---|
| Emitted `telephony-setup.sh` (new artifact) | FR-004 forbids transcribing an ID between commands; something has to resolve the trunk by number and feed the dispatch rule | README paste-blocks doing the same thing are the same logic without tests, and a multi-line pipeline in a README is where quoting bugs live |
| `jq` as an operator prerequisite of the emitted project | The script must parse `lk sip inbound list --json` to find the trunk by number | grep or sed on nested JSON is wrong on edge cases (nested objects, reordered keys); a python3 requirement is heavier than jq on a machine that only deploys |

No new Go dependency, no new authoring field, no new abstraction in the compiler.

## Project Structure

### Documentation (this feature)

```text
specs/005-telephony-setup/
├── plan.md              # This file
├── research.md          # Phase 0: decisions D1 to D9
├── data-model.md        # Phase 1: entity and surface deltas
├── quickstart.md        # Phase 1: validation runs
├── contracts/
│   ├── runbook.md            # README "Telephony setup" section contract
│   ├── provisioning-script.md# telephony-setup.sh behavior contract
│   └── environment.md        # env surface before and after
└── tasks.md             # Phase 2 (/speckit-tasks)
```

### Source Code (repository root)

```text
internal/target/telephony.go            # delete RuntimeEnvironment row + DevSuppliedEnvironment; rewrite ManualSteps
internal/ir/build.go                    # delete DevEnvironment copy (plumbing for the retired name)
internal/generate/telephony.go          # delete DevSuppliedEnv field from TelephonyRuntimePlan
internal/generate/livekit_v1.go         # rename dispatch placeholder; emit telephony-setup.sh; refresh UNVERIFIED note
internal/generate/livekit_v1_build.go   # drop env.add of the retired name
internal/cli/dev_livekit_sip.go         # gate on inbound feature evidence, not env name; stop injecting the ID
internal/cli/dev_compose.go             # remove the two DevSuppliedEnv uses (env classification, stale-value check)
internal/cli/dev_telephony.go           # line 160 is the two-phase startup GATE, not a display block: move its
                                        # condition to the inbound-feature check, keep infraServices and beforeApp
internal/generate/templates/livekit_v1/README.md.tmpl        # new "Telephony setup" runbook; retire old sections
internal/generate/templates/livekit_v1/telephony-setup.sh.tmpl # new emitted script
internal/generate/livekit_v1_test.go, livekit_inline_trunk_test.go,
internal/cli/dev_livekit_sip_test.go, dev_telephony_test.go, dev_test.go,
internal/ir/build_test.go, internal/target/telephony_test.go, user_docs_test.go  # follow the surface
internal/generate/testdata/golden/livekit_v1_remy.txt        # regenerate deliberately, read the diff
docs/SCHEMA.md (N36), docs/TRANSFERS.md, docs/TELEPHONY.md,
docs/user/targets/livekit.md, docs/user/learn/07-phone-calls.md,
docs/user/reference/cli.md, examples/human-transfer/README.md  # doc sweep (FR-010)
```

**Structure Decision**: existing compiler layout; one new template file is the only new source artifact.

## Phase Summary

- **Phase 0** (`research.md`): nine decisions, all resolved; no NEEDS CLARIFICATION remain.
- **Phase 1** (`data-model.md`, `contracts/`, `quickstart.md`): surface deltas, the two operator-facing contracts (runbook and script), the environment table, and the validation runs including the live SC-001 pass.

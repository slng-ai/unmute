# Implementation Plan: Pipecat native WebSocket telephony (zero hosted infrastructure)

**Branch**: `007-pipecat-native-websocket` | **Date**: 2026-08-13 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/007-pipecat-native-websocket/spec.md`

## Summary

Add a route on which an operator's own Twilio number reaches a deployed Pipecat
Cloud agent with nothing hosted by the operator. Twilio streams the call's audio
straight to Pipecat Cloud's native carrier endpoint; a static TwiML Bin in the
Twilio console names the agent; the platform starts the bot. The compiler's job
is a new route row, one new transport-params entry in the emitted bot, an
outbound command and a TwiML-update cold transfer that ride the operator's Twilio
credentials, a README that dictates the carrier console work, and the schema and
docs work that make the route declarable and honest. No helper artifact and no
operator-hosted process anywhere; the only tunnel is the local dev flow's
cloudflared, on the author's machine, restored-state and all. Clarified
2026-08-13: `unmute dev --telephony` runs the phone path locally end to end
(research D11), and the telephony example set is reorganized by use case
(research D13).

## Technical Context

**Language/Version**: Go 1.24 (compiler); emitted Python targets pipecat-ai 1.5.0 (pinned in the emitted pyproject)

**Primary Dependencies**: no new Go dependencies. Emitted project adds the `websocket` extra of pipecat-ai (brings fastapi, which the `runner` extra already carries; declared anyway so the need is stated, not inherited)

**Storage**: none. This route declares no coordination store; one platform session serves one call

**Testing**: `go test ./...` (L1 unit, L2 command, L3 golden); `-update-pipecat` for goldens; optional L4 smoke via `make smoke`; emitted Python linted by the examples ruff gate

**Target Platform**: darwin/linux CLI; emitted project deploys to Pipecat Cloud; nothing else runs anywhere

**Project Type**: compiler (single Go module, `internal/` layout)

**Performance Goals**: not performance-bound; compile-time work only

**Constraints**: zero operator-hosted infrastructure in production is the deliverable and must be provable offline; every existing route compiles byte-identical builds, and only FR-016's deliberate example changes move files; no secret values in emitted files

**Scale/Scope**: one new route row; the example reorg (one new example replacing one, one target migration, one audit); template deltas in bot.py/pyproject/pcc-deploy/README/env.example; the dev --telephony local flow; one SCHEMA amendment; five docs touched; one new test file

## Constitution Check

*GATE: evaluated before Phase 0; re-evaluated after Phase 1 design.*

- **I. Compile ahead of time**: PASS. The route removes runtime pieces rather than adding any: no helper process, no Unmute code anywhere at call time. The emitted project keeps zero Unmute dependency. `build/<target>/` stays disposable.
- **II. Fail loud, never average**: PASS. New capabilities enter `provisional` and fail on every claim until dated live runs. Declaring `sip_address`, `sip_username`, or `sip_password` on this route fails naming the field, the route, and the accepted set. A transfer or outbound declaration without a connection fails; a pure-inbound package needs none and that is stated, not silently accepted.
- **III. One source of truth**: PASS. One new row in the single capability rulebook; the emitter agreement test is extended so nothing is granted without an emitted path. No second table anywhere. The TwiML Bin content lives in one template block the README renders; the outbound command and the transfer reconnect markup derive from the same compiled values.
- **IV. The document wins**: PASS. SCHEMA.md gains a numbered, dated amendment (N38) for the new transport value and its connection key set. Every platform claim in research.md carries its source file and a 2026-08-13 verification date, read from the local pipecat-docs checkout and the installed pipecat 1.5.0 package, not from memory. One upstream docs contradiction (the `websocket_auth` default) is recorded and designed around rather than resolved by guessing.
- **V. Whatever compiles can be spoken to**: PASS, and this route strengthens it. Browser and console dev modes are untouched. `unmute dev --telephony` runs the phone path locally with one command (research D11), and the constitution's borrowed-state rule binds it: the number's voice configuration is restored on every exit path, interrupt included.

Post-design re-check (after Phase 1): still PASS. The design added one env name (`PIPECAT_CLOUD_ORGANIZATION`) rather than a hosted piece, and the cold-transfer failure path's session-continuity limitation is stated in the contract and the emitted README rather than hidden (II and IV both respected).

## Project Structure

### Documentation (this feature)

```text
specs/007-pipecat-native-websocket/
├── plan.md              # This file
├── research.md          # Phase 0: platform facts, dated; design decisions
├── data-model.md        # Phase 1: route row, env model, session shapes
├── quickstart.md        # Phase 1: offline and live validation halves
├── contracts/
│   ├── carrier-markup.md   # The TwiML the README dictates, and why each line
│   ├── environment.md      # Who reads which env name, per declaration shape
│   └── runbook.md          # The README's telephony section, action by action
└── tasks.md             # Phase 2 (/speckit-tasks), not created here
```

### Source Code (repository root)

```text
internal/target/
├── telephony.go                    # new route row (pipecat, cloud-websocket, twilio)
└── telephony_test.go               # row shape test

internal/ir/
├── build.go                        # route resolution; mutual-requirement guards
├── build_test.go
├── validate.go                     # service/coordination expectations for the route
└── validate_test.go

internal/generate/
├── pipecat_v1.go                   # no new emitted file; manifest gains websocket_auth
├── pipecat_v1_build.go             # cloud-websocket data group; connection vocabulary
├── templates/pipecat_v1/
│   ├── bot.py.tmpl                 # twilio transport params; session detection; transfer
│   ├── pyproject.toml.tmpl         # websocket extra when route selected
│   ├── pcc-deploy.toml.tmpl        # websocket_auth = "none" when route selected
│   ├── README.md.tmpl              # the dictated carrier setup, outbound, troubleshooting
│   └── env.example.tmpl            # connection names when outbound/transfer declared
├── pipecat_cloud_websocket_test.go # new: route's offline proofs
├── telephony_agreement_test.go     # extended: emitter agreement for the new row
└── testdata/golden/                # regenerated deliberately, diffs read

internal/cli/
├── dev.go                          # --telephony local flow for this route: local
│                                   # Twilio-mode run + cloudflared + webhook
│                                   # set/restore (reuses existing machinery)
└── dev_test.go

examples/
├── human-transfer-cloud-twilio/    # new: cold transfer + inbound on this route;
│                                   # REPLACES human-transfer-daily-twilio (D13)
└── telephony-hello/                # pipecat target moves to cloud-websocket;
                                    # audited end to end while touched (D13)

docs/
├── SCHEMA.md                       # amendment N38
├── TELEPHONY.md                    # route section + the three-Twilio-routes comparison
├── TRANSFERS.md                    # transfer semantics on this route
├── user/targets/pipecat.md         # route table and artifact notes
└── user/learn/07-phone-calls.md    # authoring-facing route choice
```

**Structure Decision**: single Go module, existing layout. The feature is a new
route through the existing four-stage flow (`spec.Load` → `ir.Build` →
`ir.Validate` → `generate.Generate`); no new packages, no new emitted files, and
deliberately fewer moving parts than 006 (no helper template at all).

## Complexity Tracking

No constitution violations to justify. The one point of accepted awkwardness is
the cold transfer's failure path: it reconnects the caller to a **fresh** agent
session, because branching on the dial outcome would require a callback endpoint
and this route exists to have none. The 006 route keeps the same session alive
through a failed transfer; this one cannot. Specified once in
[contracts/carrier-markup.md](contracts/carrier-markup.md) §3, which is the
canonical home for both of the route's transfer limits; the emitted README and
the route comparison carry it for readers, and nothing else restates it.

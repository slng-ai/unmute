# Tasks: Unmute User Docs on Mintlify

**Input**: Design documents from `/specs/008-mintlify-user-docs/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/navigation.md, quickstart.md

**Tests**: No TDD test suite. The feature's own quality gates are tasks: snippet validation, the providers agreement test, `mint validate`, `mint broken-links`, and the existing `internal/generate` example tests.

**Organization**: Tasks are grouped by user story. Every page task implicitly includes: verify each claim against the named source, run every YAML snippet through a scratch package with `./bin/unmute validate`, append what was checked (and any discrepancy) to `specs/008-mintlify-user-docs/verification-log.md`, use plain language with no em or en dashes, and end the page with where to go next. Two more standing rules: any Mintlify component gets its syntax confirmed against current Mintlify docs (the Mintlify index) the first time a page uses it (FR-018), and no page presents a route or feature as more proven than the compile evidence says (telephony routes are provisional: they work locally via `unmute dev --telephony` but carry no credentialed production smoke).

## Format: `[ID] [P?] [Story] Description`

## Phase 1: Setup (the Mintlify project exists)

- [X] T001 Scaffold `docs-site/`: write `docs-site/docs.json` exactly per `specs/008-mintlify-user-docs/contracts/navigation.md` (theme mint, name "Unmute by SLNG//", all eight groups) and a stub `docs-site/index.mdx`; create empty page stubs for all 35 slugs so navigation resolves.
- [X] T002 [P] Brand it: copy `images/Logo_SLNG.png` to `docs-site/logo/slng.png`; set `logo` and `favicon` in `docs-site/docs.json`; sample the primary color from the logo's yellow, check it against docs.slng.ai (fetch the live site), and set `colors.light`/`colors.dark` if the yellow is too light for link text.
- [X] T003 Prove the local loop: in `docs-site/` run `mint dev --no-open`, then `mint validate`; fix anything the scaffold broke before moving on.

---

## Phase 2: Foundational (fact oracles ready, log started)

**⚠️ CRITICAL**: every page task depends on these.

- [X] T004 Run `make build`; capture `./bin/unmute --help` plus `init|validate|compile|dev|completion --help` verbatim into `specs/008-mintlify-user-docs/help.txt` (the single source every CLI page quotes). In the same commit, add the agreement test that guards it: a Go test in `internal/cli` that renders the cobra tree's help in-process and compares it to `help.txt`, so a flag change fails the suite until the capture (and the pages quoting it) are refreshed (constitution Principle III).
- [X] T005 [P] Re-run `./bin/unmute validate` and `compile` on all ten `examples/*`; start `specs/008-mintlify-user-docs/verification-log.md` with the baseline results, warnings seen, and sections for: snippets run, discrepancies (code location + doc location), unverified claims, rule impacts.
- [X] T006 [P] Confirm the three unconfirmed anchors from `contracts/navigation.md` by reading the examples' YAML: tools page (simple-prompt vs salon-support), variables page (outbound-reminder vs multi-task), two-agents page (salon-support); record the choices in `specs/008-mintlify-user-docs/verification-log.md` and update the contract table.

**Checkpoint**: binary help captured, all ten anchors green, log open.

---

## Phase 3: User Story 1 - Newcomer understands why and gets a talking agent (Priority: P1) 🎯 MVP

**Goal**: intro that makes the why land, plus a standalone install-to-talking-agent path.

**Independent Test**: a cold reader states what Unmute is after `index`; the quickstart alone reaches a talking agent (SC-001, SC-002).

- [X] T007 [US1] Write `docs-site/index.mdx`: the problem (orchestrator code sprawls), "one spec, many runtimes", who it is for, a validated taste of `agent.yaml`; verify the story against `docs/ARCHITECTURE.md` re-checked against code; only Pipecat and LiveKit Agents named as targets.
- [X] T008 [US1] Verify how users actually install unmute (read `Makefile` install target and `go.mod` module path; test the command); write `docs-site/start/installation.mdx` with prerequisites (Go version, Docker for dev browser mode).
- [X] T009 [US1] Run `./bin/unmute init` end to end in a scratch directory, validate and compile the scaffold, run `unmute dev` far enough to confirm the documented flow; write `docs-site/start/quickstart.mdx` from that transcript (real commands, real output, SLNG scaffold defaults, needed env keys). If `dev` cannot run end to end for lack of a working SLNG key, ask the user for one; failing that, document the flow up to the credential boundary and record the untested step as an unverified claim in `verification-log.md` (SC-002 then stays unproven and says so in the report).
- [X] T010 [US1] Write `docs-site/start/how-unmute-works.mdx`: load → build → validate → generate in reader words, `build/<target>/` artifacts, the compile report; verify stage names and behavior against `internal/spec/load.go`, `internal/ir/build.go`, `internal/ir/validate.go`, `internal/generate/artifact.go`.
- [X] T011 [US1] Checkpoint: cold-read `index.mdx`; follow the quickstart start to finish touching no other page; `mint validate` in `docs-site/`.

---

## Phase 4: User Story 2 - The learning narrative, one concept per page (Priority: P2)

**Goal**: seven `build/*` pages that grow one agent, each anchored to a ready example.

**Independent Test**: read in sidebar order, no page uses a concept a later page teaches; every anchor validates, compiles, and has a README (SC-003, SC-005, SC-009).

- [X] T012 [P] [US2] Write `examples/simple-prompt/README.md` modeled on the six existing example READMEs (name every transport its targets declare; resolvable links; do not change targets).
- [X] T013 [P] [US2] Write `examples/multi-task/README.md` (same rules).
- [X] T014 [P] [US2] Write `examples/task-groups/README.md` (same rules).
- [X] T015 [P] [US2] Write `examples/subagents/README.md` (same rules).
- [X] T016 [US2] Run `go test ./internal/generate` to prove all four new READMEs pass the example tests; fix until green.
- [X] T017 [US2] Write `docs-site/build/one-agent.mdx` anchored to `examples/simple-prompt`: agent.yaml shape, entry_agent, models, secrets by env name; snippets cut from the example and validated.
- [X] T018 [US2] Write `docs-site/build/tools.mdx` using the anchor confirmed in T006; teach declaring a tool and what the agent does with it, verified against `internal/spec` tool structs and `docs/SCHEMA.md`.
- [X] T019 [US2] Write `docs-site/build/variables.mdx` using the anchor confirmed in T006; teach variables, `source: call_start`, templating, and `--var` as the local stand-in for dispatch metadata.
- [X] T020 [US2] Write `docs-site/build/two-agents.mdx` using the anchor confirmed in T006; teach the agents map and handoffs.
- [X] T021 [US2] Write `docs-site/build/tasks.mdx` anchored to `examples/multi-task`: typed steps and results.
- [X] T022 [US2] Write `docs-site/build/task-groups.mdx` anchored to `examples/task-groups`; state the "LiveKit TaskGroup is experimental" warning honestly (it prints at validate).
- [X] T023 [US2] Write `docs-site/build/subagents.mdx` anchored to `examples/subagents`.
- [X] T024 [US2] Checkpoint: forward-reference pass over `build/*` in order; confirm every snippet was validated for exactly the targets its page claims; `mint validate` and `mint broken-links`.

---

## Phase 5: User Story 3 - A practitioner looks things up (Priority: P3)

**Goal**: dev, telephony, transfers, targets, deployment, and reference pages, all code-derived.

**Independent Test**: any flag, field, or vendor picked from the code is findable and matches exactly (SC-004, SC-010).

### Run it locally

- [X] T025 [P] [US3] Write `docs-site/dev/overview.mdx` from `specs/008-mintlify-user-docs/help.txt` and `internal/cli/dev.go`: the dev loop, browser UI, `--port`, `--bot-port`, `--no-open`, `--verbose`, log file location, `--target` single-valued and required without a TTY when multiple targets exist.
- [X] T026 [P] [US3] Write `docs-site/dev/console.mdx`: `--console` terminal mode over local mic/speaker, no Docker, and which flags it ignores (`--port`, `--bot-port`, `--no-open`).

### Telephony

- [X] T027 [US3] Write `docs-site/telephony/overview.mdx` from `docs/TELEPHONY.md` re-verified against `internal/cli/dev_telephony.go` and the capability table: inbound and outbound, per-target routes, what `--telephony` automates (tunnel, webhook) and that every outward change is undone on exit.
- [X] T028 [P] [US3] Write `docs-site/telephony/first-phone-call.mdx` anchored to `examples/twilio-telephony-hello` and its README.
- [X] T029 [P] [US3] Write `docs-site/telephony/outbound-calls.mdx` anchored to `examples/outbound-reminder`: `--to` E.164 test calls, dispatch payload versus `--var`.
- [X] T030 [P] [US3] Write `docs-site/telephony/webhooks-and-tunnels.mdx`: `--public-url` (exact HTTPS origin), `--no-webhook` opt-out and pointing the number yourself; verify against `internal/cli/dev_tunnel.go` and `internal/cli/dev_twilio.go`.

### Transfers

- [X] T031 [US3] Write `docs-site/transfers/overview.mdx` from `docs/TRANSFERS.md` re-verified against code: warm versus cold transfer in plain words, which route supports what.
- [X] T032 [P] [US3] Write `docs-site/transfers/livekit.mdx` anchored to `examples/livekit-human-transfer`, presented as LiveKit only.
- [X] T033 [P] [US3] Write `docs-site/transfers/pipecat-daily.mdx` anchored to `examples/pipecat-human-transfer-daily`, Pipecat only, including the Daily dial-out prerequisite the validator prints.
- [X] T034 [P] [US3] Write `docs-site/transfers/pipecat-twilio.mdx` anchored to `examples/pipecat-human-transfer-twilio`, Pipecat only.

### Targets

- [X] T035 [US3] Write `docs-site/targets/overview.mdx`: what a target is, `targets.yaml` basics, one package compiling to both runtimes; exactly two targets presented.
- [X] T036 [P] [US3] Compile a both-target example and tour the Pipecat artifact in `docs-site/targets/pipecat.mdx` (`bot.py` project, what gets emitted, the generated README runbook).
- [X] T037 [P] [US3] Same for LiveKit in `docs-site/targets/livekit.mdx` (`agent.py` project).

### Deployment

- [X] T038 [US3] Write `docs-site/deploy/going-live.mdx` from `docs/DEPLOYMENT.md` re-verified against a real compile's `build/<target>/README.md`: the artifact is yours, `.env.example`, Docker image, what stays with the operator.

### Reference

- [X] T039 [US3] Write `docs-site/reference/cli/overview.mdx` from `specs/008-mintlify-user-docs/help.txt`: command tree, `-v/--version`, `completion`, exit codes (0 ok, 1 error), warnings to stderr with exit 0.
- [X] T040 [P] [US3] Write `docs-site/reference/cli/init.mdx` (interactive console behavior, refuses non-empty directories; verify against `internal/cli/init.go`).
- [X] T041 [P] [US3] Write `docs-site/reference/cli/validate.mdx` (`--target` repeatable, one result line per target, exit 1 if any fails; verify against `internal/cli/validate.go`).
- [X] T042 [P] [US3] Write `docs-site/reference/cli/compile.mdx` (`--target` repeatable default all, `build/<target>/`, compile report lines including `[unbenchmarked]` sizing and provisional telephony evidence; verify against `internal/cli/compile.go`).
- [X] T043 [P] [US3] Write `docs-site/reference/cli/dev.mdx`: every flag from help.txt with defaults ("8765", "7860"), requires-relationships (`--no-webhook`/`--public-url`/`--to` need `--telephony`), TTY behavior; verify against `internal/cli/dev.go`.
- [X] T044 [US3] Write `docs-site/reference/agent-yaml.mdx` from `internal/spec` structs cross-read with `docs/SCHEMA.md`, one section per top-level block; every YAML fragment validated.
- [X] T045 [P] [US3] Write `docs-site/reference/targets-yaml.mdx` the same way.
- [X] T046 [P] [US3] Write `docs-site/reference/variables.mdx` (types, sources, templating rules) from `internal/spec` and `docs/SCHEMA.md`.
- [X] T047 [P] [US3] Write `docs-site/reference/secrets.mdx` (env names only, UPPER_SNAKE, `.env.example` flow, startup check) from `internal/spec` and `docs/SCHEMA.md`.
- [X] T048 [US3] Write `docs-site/reference/providers.mdx` from `internal/target/catalog_pipecat.go` and `catalog_livekit.go`: vendors per role (STT, TTS, LLM, VAD/turn detection) per target, SLNG first everywhere, exact model names only for SLNG fetched live from https://docs.slng.ai/models and linked; other vendors' model IDs described as passed through.
- [X] T049 [US3] Add the agreement test binding `docs-site/reference/providers.mdx` vendor lists to the catalog: new file `internal/target/providers_docsite_test.go`, mirroring the existing sync tests in that package (`providers_doc_test.go`, `user_docs_test.go`); prove it fails on an edited vendor, then passes; `go test ./...` green.
- [X] T050 [US3] Checkpoint: diff every documented flag against `help.txt`; `mint validate` and `mint broken-links`.

---

## Phase 6: User Story 4 - The team trusts what shipped (Priority: P4)

**Goal**: the discrepancy report, with nothing resolved silently.

**Independent Test**: report lists every page, every code-versus-docs disagreement with both locations, every unverified claim (SC-008).

- [X] T051 [US4] Assemble `specs/008-mintlify-user-docs/report.md` from `verification-log.md`: pages table (page, anchor, verification status), discrepancies, unverified claims, and rule impacts including the "three places become four" note (research R9) proposing a `CLAUDE.md` amendment to the maintainers.
- [X] T052 [US4] Cross-check the report against `docs-site/docs.json`: every shipped page accounted for, zero silent resolutions (any discrepancy found during writing appears here, not fixed-and-forgotten).

---

## Phase 7: Polish and final gates

- [X] T053 Run the full gate set: `mint validate` and `mint broken-links` in `docs-site/`, `go test ./...` at root, `make lint`; fix everything this feature introduced.
- [X] T054 [P] Story sweep over `docs-site/`: grep for em/en dashes used as punctuation, for Vapi/Deepgram/ElevenLabs presented as targets (must be absent; ElevenLabs allowed only as a catalog vendor), and re-read `build/*` in order for forward references.
- [X] T055 [P] Write `docs-site/README.md` for contributors: how to preview (`mint dev --no-open`), how snippets get validated, the agreement tests, that docs-site pages join the places a behaviour change must update, and the go-live checklist: create a NEW Mintlify project for this site (never connect it to the existing docs.slng.ai deployment), and set site visibility to Private in the dashboard before sharing any URL.
- [X] T056 Execute `specs/008-mintlify-user-docs/quickstart.md` top to bottom; all seven sections pass; record the run at the end of `report.md`.

---

## Dependencies & Execution Order

- **Setup (P1..T003)** → **Foundational (T004..T006)** → user stories.
- **US1 (T007..T011)**: needs only Foundational. This is the MVP.
- **US2 (T012..T024)**: needs Foundational. READMEs T012..T015 are parallel; T016 gates the narrative pages; narrative pages T017..T023 go in sidebar order (each page may reference only concepts already written).
- **US3 (T025..T050)**: needs Foundational; independent of US2 except cross-links added at checkpoint time. Sections (dev, telephony, transfers, targets, deploy, reference) are mutually independent; overview pages precede their siblings (T027 before T028..T030; T031 before T032..T034; T035 before T036..T037; T039 before T040..T043). T049 needs T048.
- **US4 (T051..T052)**: needs US1..US3 done (it reports on them).
- **Polish (T053..T056)**: last; T056 is the final acceptance run.

### Parallel opportunities

- T002 alongside T001's stub work; T005 and T006 alongside each other after T004.
- T012..T015 (four READMEs, four files).
- Within US3: T025+T026; T028..T030; T032..T034; T036+T037; T040..T043; T045..T047.
- T054 and T055 during Polish.

### Parallel example: User Story 2 kickoff

```text
Task: "Write examples/simple-prompt/README.md"
Task: "Write examples/multi-task/README.md"
Task: "Write examples/task-groups/README.md"
Task: "Write examples/subagents/README.md"
```

## Implementation Strategy

MVP first: Setup + Foundational + US1, then stop and validate (cold read plus quickstart walkthrough). Ship value incrementally: US2 makes it a teachable product, US3 makes it a usable reference, US4 makes it trustworthy. Nothing deploys until the user runs `mint login` and approves the push (deployment is deliberately outside this task list). At deploy time this site becomes its own NEW Mintlify project on its own subdomain; it must never be connected to or pushed over the existing docs.slng.ai deployment (verified 2026-08-14: this repo contains no docs.json or mint.json today, so the scaffold overrides nothing), and visibility is set to Private in the dashboard before the URL is shared.

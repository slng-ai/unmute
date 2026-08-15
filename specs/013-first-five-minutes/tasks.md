---

description: "Task list for feature 013, make the first five minutes work"
---

# Tasks: Make the first five minutes work, and stop lying quietly

**Input**: Design documents from `specs/013-first-five-minutes/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md),
[research.md](./research.md), [data-model.md](./data-model.md),
[contracts/messages.md](./contracts/messages.md),
[quickstart.md](./quickstart.md), and [reproduction.md](./reproduction.md),
which holds the Wave A evidence every red test is written from.

**Tests**: required. FR-008 says every refusal this feature adds must have a test
that fails before the fix and passes after, so the test tasks are not optional
here and they come first within each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel, different files, no dependency on unfinished work
- **[Story]**: which user story the task serves

## Path conventions

Go code in `internal/`, never `pkg/`. One file per command in `internal/cli/`.
Generated Python lives only in `internal/generate/templates/`. Documentation
lives in `docs/` and `docs-site/`; the shipped coding-agent bundle lives in
`internal/skill/assets/` and is the fourth documentation surface for this
feature.

## Story order deviates from priority order once, on purpose

User Story 4, every example runs, is last in this file rather than fourth. Every
fix in User Stories 1 to 3 changes generated output, so validating eleven
examples before those land would validate output that is about to change. The
priority in the spec is unchanged; only the execution order moves.

---

## Phase 1: Setup

**Purpose**: land the branch on the right base and confirm the gate is green
before anything moves.

- [X] T001 Fast-forward the worktree branch onto `origin/main` at `16289f4`, which carries PR #78, #79, and #80, and create `013-first-five-minutes` from it
- [X] T002 Confirm the constitution is at 2.1.0 and that `specs/011-*` and `specs/012-*` exist, so `013` is the free number
- [X] T003 Run `make fmt && make lint && make build && make test` on the untouched base and record that it is green, so any later red is this feature's — confirmed at `16289f4`: 0 lint issues, all ten packages pass

**Checkpoint**: a known-green base with the skill bundle present.

---

## Phase 2: Foundational — the red tests

**Purpose**: turn Wave A's evidence into repository tests that fail for the right
reason. Every defect gets one before any fix is written.

**⚠️ No fix starts until every test in this phase is red.**

- [ ] T004 [P] Add `TestUnreachableControlIsRefused` to `internal/ir/build_test.go`, table-driven over the eight shapes in data-model.md: unattached `human_transfer` cold and warm, unattached `agent_transfer`, unattached `delegate`, unreferenced destination, unreferenced top-level tool, unreachable task, unreachable agent
- [ ] T005 [P] Add a case to the same table asserting an unreferenced `models:` entry stays legal, per `docs/SCHEMA.md:287`, so the fix cannot break the palette carve-out
- [ ] T006 [P] Add `TestColdTransferNeedsARoute` to `internal/ir/validate_test.go`: a browser-only LiveKit package with an attached `human_transfer` must fail validation, and the Pipecat variant must produce exactly one error in Pipecat's own words
- [ ] T007 [P] Add `TestSecretsCheckRunsWithNoBlock` to `internal/ir/variables_test.go`: the three shapes from reproduction.md section C, asserting the warning fires for the deleted-block package with exit 0 preserved
- [ ] T008 [P] Add a case to the same test asserting a scaffolded package's provider key names are in the reference set, so removing the guard alone does not leave the check vacuous
- [ ] T009 [P] Give `pipecatArtifact` in `internal/generate/pipecat_deploy_test.go` a fixture with one local tool, and change `TestPipecatImageMeetsThePlatformContract` to assert every emitted `.py` the entrypoint imports is reachable inside the image rather than asserting one `COPY` line's spelling
- [ ] T010 [P] Add `TestValueChecksFailAtValidate` to `internal/ir/validate_test.go`, table-driven over all eight generator-only checks listed in reproduction.md section E
- [ ] T011 [P] Add `TestScaffoldAgreesWithItsOwnBuild` to `internal/scaffold/scaffold_test.go`: compile the scaffolded package and assert the package-root and generated `.env.example` name the same set
- [ ] T012 [P] Add `TestScaffoldPromptMatchesItsChannel` to `internal/scaffold/scaffold_test.go`: with only a `web` channel, neither the greeting nor `instructions.md` may contain "call", "calling", "caller", or "phone"
- [ ] T013 [P] Add `TestNoUnmuteEnvOnTheBeginnerPath` to `internal/skill/agreement_test.go`, modelled on `TestNoSecretsInTheBundle` at line 367 and reusing `sitePages` and `Bundle.Files`, covering the site index, `docs-site/start/`, `docs-site/build/`, root `README.md`, the scaffold output, and the whole bundle
- [ ] T014 [P] Add `TestOneModelIdEverywhere` to `internal/skill/agreement_test.go`, reading one exported constant and grepping the scaffold, the eleven examples, `docs/`, `docs-site/`, root `README.md`, and the bundle
- [ ] T015 Run `go test ./...` and confirm each new test fails, and fails for the reason its name claims rather than a compile error or a typo

**Checkpoint**: twelve red tests. Fixing begins.

---

## Phase 3: User Story 1 — nothing compiles green while silently dropping what the author declared (Priority: P1) 🎯 MVP

**Goal**: every declared thing either reaches the generated project or is named
in a refusal.

**Independent Test**: run the eight reproductions from
[quickstart.md](./quickstart.md) section 1. Each refuses, naming the thing.

- [ ] T016 [US1] Implement the reachability walk in `internal/ir/build.go` beside `checkToolRefs`, using the `missing()` helper's file-and-line shape and the message contracts from contracts/messages.md, covering every row of the data-model table and skipping the `models:` palette
- [ ] T017 [US1] Confirm T004 and T005 are green and that no existing test regressed, especially `internal/ir/golden_test.go`
- [ ] T018 [US1] Widen the transfer guard in `validateHumanTransfer` at `internal/ir/validate.go:1148` to cover cold as well as warm, keeping the `len(row.Errors) == before` guard so Pipecat keeps failing in its own words, and rewrite the comment whose "cold is unaffected" reasoning is the defect
- [ ] T019 [US1] Confirm T006 is green, and that a telephony package with a real SIP connection still compiles its cold transfer unchanged
- [ ] T020 [US1] Remove the `len(agent.Secrets) == 0` guard from `undeclaredSecretWarning` at `internal/ir/validate.go:1388`, keeping the severity a warning on stderr with exit 0 per `docs/SCHEMA.md` N24
- [ ] T021 [US1] Add model provider key environment names to `referencedEnvNames` at `internal/ir/validate.go:1301`, reading the key-env field from the catalogue `Entry` in `internal/target/catalog_*.go` rather than adding a second list
- [ ] T022 [US1] Derive the generated `REQUIRED_ENV` from the environment names the compiler already knows it requires rather than from `agent.Secrets`, in `internal/generate/livekit_v1_build.go` and `internal/generate/pipecat_v1_build.go`, so a package with no block keeps the startup check `docs-site/reference/secrets.mdx:107` promises
- [ ] T023 [US1] Confirm T007 and T008 are green; if T022 moves more goldens than it is worth, stop and take the recorded fallback in research D3 instead, amending `secrets.mdx` to say the startup check is generated from what you declare
- [ ] T024 [US1] Add the conditional `COPY tools/ ./tools/` to the non-telephony branch of `internal/generate/templates/pipecat_v1/Dockerfile.tmpl`, guarded on there being local tools, keeping the named-file discipline the template comment explains
- [ ] T025 [US1] Confirm T009 is green and that `go test ./internal/generate -update` moves zero goldens, per research D4; investigate before regenerating if any move
- [ ] T026 [US1] Add a `smoke`-tagged test that builds a generated Pipecat image for a package with a local tool and imports its entrypoint, so the invariant is proven in a real container and the default suite stays Python-free
- [ ] T027 [P] [US1] Move the recognised LiveKit turn detector model set out of `livekitTurnVersion` in `internal/generate/livekit_v1_build.go:1056` into `internal/target/catalog_livekit.go`, and have the generator read it from there
- [ ] T028 [US1] Mirror the turn detector model check into `internal/ir/validate.go`, reading the set from its new home
- [ ] T029 [P] [US1] Mirror the remaining seven generator-only value checks into `internal/ir/validate.go`: `sdk_language`, three `pins` checks, two `version` checks on both drivers, and the slotless speak `voice`, each keeping its exact existing wording
- [ ] T030 [US1] Confirm T010 is green for all eight, and that each generator keeps its own error as a backstop rather than losing it
- [ ] T031 [US1] Confirm `compile-report.json` no longer claims a `cold_transfer` or `warm_transfer` capability the emitted agent does not have, per FR-002a, and that an unattached `agent_transfer` no longer leaves a dead agent class emitted
- [ ] T032 [US1] Append a dated, numbered amendment to `docs/SCHEMA.md` recording the LiveKit turn-model constraint, naming the legal identifiers, correcting the `silero` example at line 317, and stating that Pipecat still forwards the value unchecked
- [ ] T033 [P] [US1] Update `internal/skill/assets/references/transfers.md`: rewrite the section headed "A transfer needs a phone call, and a browser package will not tell you" to say the compiler refuses it and quote the refusal, and correct its claim that the control is "absent from the generated project", which described the unattached-control defect and not the browser-only one
- [ ] T034 [P] [US1] Update `internal/skill/assets/references/package.md:168-186`, which documents three of these defects as known, and any other bundle file that teaches a workaround for something now fixed
- [ ] T035 [P] [US1] Update `docs-site/transfers/*` and the emitted `README.md` templates for both drivers wherever they describe the dead-end behaviour, so all four documentation surfaces move in this story's commits

**Checkpoint**: eight red tests green, the constitution's Principle II restored,
gate green.

---

## Phase 4: User Story 2 — `unmute init` writes a package worth talking to (Priority: P2)

**Goal**: a first-time author reaches a spoken greeting on two environment
variables, and every file the scaffold wrote is true.

**Independent Test**: [quickstart.md](./quickstart.md) section 2, ending in one
real spoken exchange heard by a person.

- [ ] T036 [US2] Reorder `withDefaults` in `internal/scaffold/scaffold.go` so `Channel` is set before `Instructions`, which today makes the channel literally unreadable at the point the prompt is chosen
- [ ] T037 [US2] Write the new `DefaultGreeting` and `DefaultInstructions` for web audio using the `voice-agent-prompting` skill, read against `internal/skill/assets/references/prompting.md` and `examples/simple-prompt/instructions.md`, covering who the agent is, a voice contract, what it will not do, and a greeting that matches the channel — and mark the absence of a channel branch with a `// ponytail:` comment naming the ceiling, per research D8
- [ ] T038 [US2] Delete the unconditional `Transport = "daily-sip"` from `SetTarget("pipecat")` at `internal/scaffold/scaffold.go:269`, which is a phantom whose only observable effect anywhere is the `DAILY_API_KEY` line
- [ ] T039 [US2] Add a `secrets:` block naming `OPENAI_API_KEY` and `SLNG_API_KEY` to `internal/scaffold/templates/agent.yaml.tmpl`, so a fresh package demonstrates the rule from T020
- [ ] T040 [US2] Fix `internal/scaffold/templates/env.example.tmpl:2`, which tells the reader to run `unmute dev <name>` from inside the directory it names, resolving to `<name>/<name>`
- [ ] T041 [US2] Fix the `targets.yaml` comment claiming "Secrets and URLs are env var names only" in a file that would not hold them, and audit every other scaffolded comment against what its file contains
- [ ] T042 [US2] Introduce one exported constant for the default model identifier in `internal/scaffold`, set to `gpt-4.1-mini` per research D10, and have `scaffold.go:277` read it
- [ ] T043 [P] [US2] Change the eleven `examples/*/agent.yaml` and the `docs-site/build/`, `docs-site/models/`, `docs-site/reference/` pages, `docs/ORCHESTRATOR_SHARED_CONFIGURATION.md`, and `internal/skill/assets/references/models.md:97` to the single identifier
- [ ] T044 [US2] Confirm T014 is green, and fix `internal/skill/assets/references/models.md:103`, which claims only SLNG identifiers appear in this repository's documentation three lines from two OpenAI ones
- [ ] T045 [US2] Change `TestWrite_golden` in `internal/scaffold/scaffold_test.go` to pass `scaffold.DefaultTools()` as `internal/cli/init.go:27` does, so the golden covers the two `tools:` blocks and `tools/end_call.yaml` that every real `unmute init` writes and nothing currently tests
- [ ] T046 [US2] Regenerate `internal/scaffold/testdata/golden/init.txt` with `-update` and review the diff line by line
- [ ] T047 [P] [US2] Remove the dead `OPENAI_MODEL` from the repository-root `.env.example`, read by nothing, and correct the model identifier in the root `README.md`
- [ ] T048 [P] [US2] Fix `docs-site/start/quickstart.mdx`, which prints "Hi, thanks for calling." as the expected browser greeting and explains `DAILY_API_KEY` away as needed later for a phone number
- [ ] T049 [US2] Confirm T011 and T012 are green
- [ ] T050 [US2] Run `unmute init` then `unmute dev` with only `OPENAI_API_KEY` and `SLNG_API_KEY` set, reach a spoken greeting, and complete one real spoken exchange after it. A probe reaching a socket does not close this task
- [ ] T051 [P] [US2] Update `docs-site/build/` pages and any bundle reference that shows scaffold output, so all four surfaces move with the behaviour

**Checkpoint**: the first five minutes work, and nothing in them contradicts
itself.

---

## Phase 5: User Story 3 — no Unmute-branded name looks like a credential (Priority: P3)

**Goal**: the generated files stop contradicting the page that already gets this
right.

**Independent Test**: [quickstart.md](./quickstart.md) section 3.

- [ ] T052 [US3] Move `UNMUTE_PUBLIC_URL` and `UNMUTE_OUTBOUND_TOKEN` out of the fill-in list and into a labelled supplied-for-you block in `internal/generate/templates/pipecat_v1/env.example.tmpl` and `internal/generate/templates/livekit_v1/env.example.tmpl`, in the shape the LiveKit platform-env section already uses
- [ ] T053 [US3] Drop both names from the emitted README templates' "set these before running" list for both drivers
- [ ] T054 [P] [US3] Correct `docs/TELEPHONY.md:338`, which tells the reader to generate a secret that `unmute dev` mints at `internal/cli/dev_telephony.go:87-92`
- [ ] T055 [P] [US3] Delete or reword the speculative `UNMUTE_ICE_SERVERS` line at `docs/PRODUCTION_ROADMAP.md:192`, the only fictional name in the repository
- [ ] T056 [P] [US3] Document `UNMUTE_TELEPHONY_BRIDGE_PORT` and `UNMUTE_AGENT_HEALTH_PORT` beside the other port knobs in the emitted LiveKit README, or delete them; each is one template line named by no Go code, no document, no test, and no golden
- [ ] T057 [US3] Confirm T013 is green, and record in results.md that it locks a state which already held at baseline rather than fixing a violation
- [ ] T058 [US3] Record in results.md that no `UNMUTE_*` rename was made, with the priced table and the reason from research D11

**Checkpoint**: an author reading a generated `.env.example` is never told to
obtain an Unmute credential.

---

## Phase 6: User Story 5 — every rule validation enforces is findable in the docs (Priority: P5)

**Goal**: an author who hits a validation error can find the rule next to the
field, without reading Go. This phase runs before User Story 4 because it changes
no behaviour and the examples work depends on the behaviour being final.

**Independent Test**: [quickstart.md](./quickstart.md) section 5.

- [ ] T059 [P] [US5] State in `docs-site/telephony/*` and the channel reference page that `capacity.peak_starts_per_second` must be positive and is required the instant any channel is `telephony`, per `internal/ir/validate.go:1450`
- [ ] T060 [P] [US5] State in the same places that a warm transfer requires `outbound: true` on its channel because it places a call, per `internal/ir/validate.go:1501`, so an author writing an inbound line is not led to `outbound: false`
- [ ] T061 [US5] Check whether `docs/` and `docs-site/` received the fix PR #80 applied only to the skill bundle, and fold both rules into `docs/` too
- [ ] T062 [US5] Extract every error string `ir.Validate` can produce and look each one up in `docs/` and `docs-site/` from the field it names; produce the audit table
- [ ] T063 [US5] Document the ones that are enforced and undocumented, or list them in results.md with a reason each
- [ ] T064 [P] [US5] Mirror both rules into `internal/skill/assets/` wherever the bundle describes the same fields, keeping the fourth surface true

**Checkpoint**: the two causes of PR #80's three telephony failures are
documented on the site, not only in the bundle.

---

## Phase 7: User Story 4 — every example is meaningful and every example runs (Priority: P4)

**Goal**: eleven examples that validate, compile, run, and teach what the table
says they teach.

**Independent Test**: [quickstart.md](./quickstart.md) section 4.

- [ ] T065 [US4] Validate and compile all eleven examples for every target each declares, and record the raw count
- [ ] T066 [US4] Start every browser-only example with `unmute dev` and reach a greeting; build and import the container for every telephony example. The five examples reproduction.md section D found broken on Pipecat are the ones to check first
- [ ] T067 [US4] Remove `tracing:` from every example that is not the tracing example, so no first-run example needs a Langfuse account, per research D12
- [ ] T068 [US4] Name the one tracing example in the `examples/README.md` table and correct the paragraph claiming all three `LANGFUSE_*` secrets are required because the public examples configure Langfuse
- [ ] T069 [US4] Make `examples/README.md`, `docs-site/build/your-first-agent.mdx`, and `internal/skill/assets/references/examples.md` all name `salon-support` as the starting example, per research D13
- [ ] T070 [US4] Fix `docs-site/build/your-first-agent.mdx`, which shows a trimmed `agent.yaml` and claims it is the real file: show the real file or say plainly it is reduced and what was removed
- [ ] T071 [US4] Read every example `README.md` and correct anything the fixes made untrue; confirm each names every `transport` its targets declare and that every link out resolves, and that the two tests in `internal/generate/examples_test.go` still hold
- [ ] T072 [US4] Confirm each example still teaches the one thing `examples/README.md` says it teaches, and delete any that teaches nothing, recording which and why
- [ ] T073 [US4] Confirm the five structural examples now run on Pipecat, which they have not since 2026-08-13

**Checkpoint**: eleven examples, all runnable, all honestly described.

---

## Phase 8: Verification — Waves B and C

**Purpose**: prove the fixes independently, and re-measure the bar PR #80 missed.
Every agent gets its own scratch directory named after the agent.

- [ ] T074 Wave B: one agent per fix, each given the fix commit and nothing else, re-running its Wave A reproduction. An agent that wrote a fix does not verify it
- [ ] T075 Wave B: one agent whose only job is the six bundle agreement tests plus the bundle golden, reporting which this feature turned red and whether the bundle or the code was the thing that was wrong
- [ ] T076 [P] Wave C: five first-run agents, each `unmute init` to a working greeting using only the CLI and the local `docs-site/` tree, never a published URL. Bar is 5 of 5 with no file edited outside the scaffold
- [ ] T077 [P] Wave C: ten authoring agents on the same ten briefs PR #80 used — salon, hotel, vet, gym, bank, restaurant, utility, dental, pizza, helpdesk — each installing the bundle with `unmute skill install`. Bar is 8 of 10 validating clean on first attempt. This is the SC-002 re-run PR #80 did not do
- [ ] T078 [P] Wave C: two adversarial agents, each trying to produce a package that compiles green and does nothing. Every one they find is a User Story 1 defect that got missed and must be fixed
- [ ] T079 Write `specs/013-first-five-minutes/results.md` with the raw counts, a per-brief table for T077 with a stated cause for every failure, and no rounding

---

## Phase 9: Polish and cross-cutting

- [ ] T080 Run `make smoke` and get it genuinely green. If it fails, run `docker ps` first and name the blocking port and what held it before blaming the branch
- [ ] T081 [P] Mark every deliberate simplification with a `// ponytail:` comment naming the ceiling and the upgrade path
- [ ] T082 [P] Mark each of the five defects in `specs/011-coding-agent-skill/tasks.md` closed with the commit that closed it, or explicitly deferred with a reason
- [ ] T083 Confirm no new dependency was added, and that `go.mod` is unchanged
- [ ] T084 Run the full [quickstart.md](./quickstart.md) end to end as a person
- [ ] T085 Final gate: `make fmt && make lint && make build && make test`, zero lint issues, on the final commit

---

## Dependencies and execution order

### Phase dependencies

- **Phase 1 Setup**: no dependencies.
- **Phase 2 Foundational**: depends on Phase 1. **Blocks every fix.** No task in Phase 3 onward starts until Phase 2's tests are red.
- **Phase 3 (US1)**: depends on Phase 2.
- **Phase 4 (US2)**: depends on Phase 2. Independent of Phase 3 except T039, which demonstrates the rule T020 fixes, so T039 lands after T020.
- **Phase 5 (US3)**: depends on Phase 2. Independent of Phases 3 and 4.
- **Phase 6 (US5)**: documentation only. Depends on Phase 3 for the wording of any rule Phase 3 changed.
- **Phase 7 (US4)**: **depends on Phases 3, 4, and 5**, because all three change generated output. T073 specifically depends on T024.
- **Phase 8**: depends on every fix being committed.
- **Phase 9**: last.

### Within each story

Tests first and failing, then the fix, then the confirmation task, then the four
documentation surfaces in the same commit.

### Parallel opportunities

- All of Phase 2 is parallel: twelve tests in six files, no shared state.
- Within Phase 3: T027 is independent of T016 to T026. T033, T034, and T035 are documentation in three trees.
- Within Phase 4: T043, T047, T048, and T051 touch different trees.
- Within Phase 5: T054, T055, and T056 are three different documents.
- Phase 8's T076, T077, and T078 run as seventeen agents in one message, each with its own scratch directory.

---

## Implementation strategy

### MVP

Phase 1, Phase 2, Phase 3. At that point the constitutional violation is fixed:
nothing compiles green while silently dropping what the author declared. That is
shippable on its own and is the whole reason this feature exists.

### Incremental delivery

1. Setup and red tests → a base that proves the defects.
2. User Story 1 → **stop and validate** → the silent drops are gone.
3. User Story 2 → the first five minutes work.
4. User Story 3 → nothing looks like a credential it is not.
5. User Story 5 → the rules are findable.
6. User Story 4 → every example runs on the finished behaviour.
7. Waves B and C → the numbers, reported raw.

### Notes

- One commit per defect in Phase 3, so Wave B can be given a single commit.
- The four-surface rule applies inside every commit, not at the end.
- The gate is green on every commit, not only the last.
- A bar that is missed is reported as the number it is. A 7 is a 7.

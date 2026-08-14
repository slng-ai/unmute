# Tasks: Mintlify Docs Extension

**Input**: Design documents from `/specs/009-mintlify-docs-extension/`

**Prerequisites**: plan.md, spec.md (7 user stories after two clarification rounds), research.md (R11-R26), data-model.md, contracts/navigation.md (49-page tree), contracts/update-map.md, quickstart.md

**Tests**: Two test tasks exist because the spec demands them: T042 adds the execution-blocks agreement test (FR-037, research R17) and T047 retargets the catalog agreement test (FR-040, research R21). No TDD scaffolding beyond that.

**Organization**: Grouped by user story. Phases run in execution order, which is priority order with one exception: US4 (the addenda, P4) runs last because addenda describe finished work. The spec's P4 reflects maintainer value, not sequence.

## Format: `[ID] [P?] [Story] Description`

## Phase 1: Setup (commit and merge)

**Purpose**: Take the new code before writing a word (FR-027).

- [ ] T001 Commit the untracked docs work: `git add -A && git commit -m "docs(site): the Mintlify user docs, first pass"` from the worktree root
- [ ] T002 Merge the new work: `git fetch origin && git merge origin/pre-release-v1`; resolve any conflict favoring the incoming product code and keeping the docs-site work, noting conflicts for the report addendum
- [ ] T003 Rebuild and inventory the damage: `make build`, `go test ./...`, `make lint`, `gofmt -l internal/`; list every failure with the doc or page it implicates in a scratch note (failures are signals, not blockers)

---

## Phase 2: Foundational (read the change, baseline the tree)

**Purpose**: Ground truth for every story. No page is edited until these are done.

- [ ] T004 Read the landed contracts before touching any page: `specs/008-mcp-tool-sources/contracts/{authoring,emission}.md`; `specs/008-simplify-telephony-connections/contracts/{authoring,errors,environment}.md`; merged `docs/SCHEMA.md` N40 and N41; skim merged `docs/TELEPHONY.md`, `docs/TRANSFERS.md`, `docs/DEPLOYMENT.md`, `docs/REPO_MAP.md`, `docs/user/reference/connections.md`, `docs/user/learn/twilio-walkthrough.md`, `docs/user/learn/08-going-live.md`
- [ ] T005 [P] Re-grep the site for the moved fields on the merged tree (`transport:`, `carrier:`, `destinations:`, `kind:` in `docs-site/`); diff against the baseline in contracts/update-map.md and make the fresh list the working checklist (FR-030)
- [ ] T006 [P] Validate and compile all eleven examples with the merged `./bin/unmute` (including `examples/mcp-example`), confirm each has a README, run `go test ./internal/generate`; capture outputs for the verification log
- [ ] T007 Append a new dated phase skeleton to `specs/008-mintlify-user-docs/tasks.md` mirroring this task list (unchecked), tracked the way the first 56 tasks were (FR-038); keep it in step as tasks finish
- [ ] T008 Check the merged behavior of `kind:` written in a connection file (`internal/spec/load.go`, `internal/ir/validate.go`): if code and the landed contract disagree, record it as a new discrepancy for the report addendum, do not settle it (research R15)

**Checkpoint**: Merged tree understood, baselines captured, tracking in place.

---

## Phase 3: User Story 1 - The site describes the merged product (Priority: P1) 🎯 MVP

**Goal**: No page shows the pre-merge product: connection owns the route, `destinations:` and the new `secrets:` rule in agent.yaml, fresh quickstart transcript, a connections reference page, and no provider-branded env-name examples.

**Independent Test**: quickstart.md sections 2 and 4: fresh greps judged clean, every changed snippet validates, every quoted refusal reproduces, transcript matches a fresh run, `grep -rn "11LABS" docs-site` empty.

- [ ] T009 [US1] Re-run `unmute init` with the quickstart's inputs against the merged scaffold and re-capture the transcript and every scaffolded file shown in `docs-site/start/quickstart.mdx` (FR-032); sweep other pages for scaffolded `targets.yaml` and update from the same run
- [ ] T010 [US1] Write `docs-site/reference/connections-yaml.mdx`: the three valid shapes each backed by a validated scratch package or example, `kind:` not written, one target names one connection, the outbound-reminder split as the worked example (FR-029); register in `docs.json` under Reference after `reference/targets-yaml`
- [ ] T011 [US1] Update `docs-site/reference/targets-yaml.mdx`: the merged field list (`provider`, `version`, `pins`, `sdk_language`, `connection`, `deployment_region`, `models`); trigger and paste the refusal for each moved field naming its new home; link to `reference/connections-yaml`
- [ ] T012 [US1] Update `docs-site/reference/agent-yaml.mdx`: top-level `destinations:` (env var names only, literal-value refusal triggered and pasted), the new `secrets:` rule summarized, channel `kind: telephony` left intact
- [ ] T013 [US1] Update `docs-site/reference/secrets.mdx` to the merged rule: every author-written env name is listed, platform-supplied names are not (name them); verify both lists against `specs/008-simplify-telephony-connections/contracts/environment.md` and a real build's `.env.example`
- [ ] T014 [P] [US1] Update `docs-site/targets/overview.mdx`: the target names a connection and declares nothing else about the route; scratch-validate shown snippets
- [ ] T015 [P] [US1] Update `docs-site/telephony/overview.mdx` for N41: the connection owns the route, three shapes named and linked to `reference/connections-yaml`, matrix re-verified, routes stay `provisional` (the local-run walkthrough leaves this page later, in T050)
- [ ] T016 [P] [US1] Update `docs-site/telephony/outbound-calls.mdx`: `destinations:` in agent.yaml, `examples/outbound-reminder`'s two connection files by their new names (`twilio_websocket.yaml`, `twilio_connector.yaml`), outputs re-run and re-captured
- [ ] T017 [P] [US1] Update `docs-site/transfers/overview.mdx`: moved fields gone; trigger and paste the merged gated-transfer refusal that names the connection and its transport (`specs/008-simplify-telephony-connections/contracts/errors.md`)
- [ ] T018 [P] [US1] Update `docs-site/transfers/livekit.mdx` against the merged `examples/livekit-human-transfer`: connection shape in snippets, captured outputs re-run
- [ ] T019 [P] [US1] Update `docs-site/transfers/pipecat-daily.mdx`: the carrier-less `daily-sip` connection shape, example re-validated
- [ ] T020 [P] [US1] Update `docs-site/transfers/pipecat-twilio.mdx`: full-route connection shape, refusals re-triggered, example re-validated
- [ ] T021 [P] [US1] Update `docs-site/reference/cli/compile.mdx` and `docs-site/telephony/first-phone-call.mdx`: any shown `targets.yaml`, compile output, or example file list matches the merged tree (re-run and re-capture)
- [ ] T022 [US1] Replace the branded invalid env-name example on `docs-site/deploy/going-live.mdx`, `docs-site/telephony/first-phone-call.mdx`, and `docs-site/reference/secrets.mdx` with a neutral name that still starts with a digit; trigger the validator's real refusal for it and quote that (FR-044, research R26); sweep the site for other provider-branded placeholder names
- [ ] T023 [US1] US1 checkpoint: re-run the moved-field greps (judged clean per quickstart.md section 2), `grep -rn "11LABS" docs-site` empty, `cd docs-site && mint validate && mint broken-links`, and re-validate every snippet changed in this phase in scratch packages; log everything for the verification addendum

**Checkpoint**: Every existing page tells the truth about the merged product. This is the MVP: a correct site, still in the old structure.

---

## Phase 4: User Story 2 - An author learns MCP tool sources (Priority: P2)

**Goal**: A dedicated MCP page anchored on `examples/mcp-example`, teaching the block shape and the no-contract rule.

**Independent Test**: The page exists and is reachable, the example validates and compiles, the illegal-field refusal reproduces word for word, capability claims match the merged table.

- [ ] T024 [US2] Gather the MCP evidence: validate and compile `examples/mcp-example`; capture `.env.example`, the startup-check names, and the compile-report reference sites for the block's env names (`specs/008-mcp-tool-sources/contracts/emission.md` as the checklist)
- [ ] T025 [P] [US2] Trigger the no-contract refusals in a scratch package: an `mcp:` tool file declaring `description` and one declaring `input`, each refused individually with file and line; capture verbatim
- [ ] T026 [P] [US2] Confirm the merged capability rows in `internal/target/table.go`: `tools.execution.mcp` per target, the LiveKit `sdk_language: python` requirement, the Pipecat task-scope denial (`tasks.tools.execution.mcp`); quote the table's own words
- [ ] T027 [US2] Write `docs-site/build/tools/mcp.mdx` per contracts/update-map.md; because `build/tools/overview.mdx` does not exist yet, the page defines its own terms and leans on no overview vocabulary until T030 lands; add the nested Tools group to `docs.json` inside "Build the agent" with this page as its first member; `mint validate && mint broken-links`

**Checkpoint**: MCP is fully taught and verifiable on its own.

---

## Phase 5: User Story 3 - The Build section teaches concepts (Priority: P3)

**Goal**: The Build group per contracts/navigation.md: `your-first-agent`, nested Tools (overview, webhook, python, mcp, prebuilt), `variables`, nested Orchestration (overview, handoffs, tasks, task-groups, choosing-a-structure).

**Independent Test**: Nav matches disk for the group, no forward references reading in order, every tools page maps to a real execution block, the new agreement test passes and fails when the overview lies.

- [ ] T028 [US3] Read the LiveKit inspiration pages via the livekit-docs MCP (agents-handoffs, tasks, workflows, supervisor-pattern, tools, tools/mcp, tools/definition); take shape only: overview that routes, deep sub pages, a decision aid; no LiveKit-only concept becomes a page (FR-036)
- [ ] T029 [US3] Move `docs-site/build/one-agent.mdx` to `docs-site/build/your-first-agent.mdx` (`git mv`), update the title if needed and every inbound link, and update `docs.json`
- [ ] T030 [US3] Write `docs-site/build/tools/overview.mdx`: what a tool is, the six execution blocks by name with `client` and `provider_hosted` marked gated per the merged table, the routing table to sub pages, `interruption` and `effect` taught once, assignment scoping; re-check T027's mcp page now that the shared vocabulary exists
- [ ] T031 [US3] Write `docs-site/build/tools/webhook.mdx` from the verified content of `docs-site/build/tools.mdx` plus the anchor picked while writing (`examples/salon-support` or `examples/outbound-reminder`, choice recorded in the verification log): `url_env`, `path` rendering, `auth`, `input`/`output`, `inject`; re-validate every snippet; then retire `docs-site/build/tools.mdx` (`git rm`)
- [ ] T032 [P] [US3] Write `docs-site/build/tools/python.mdx` anchored on `examples/outbound-reminder`'s local tool: handler location in the generated project, what it receives and returns, `os.environ` as the credential seam; run any shown Python and check it with `ruff` and `ty` (CLAUDE.md)
- [ ] T033 [P] [US3] Write `docs-site/build/tools/prebuilt.mdx` from the merged `internal/target/prebuilt.go`: closed registry, exactly its current entries (expected: `end_call`, effect `ends_conversation`, default description), no implied catalog
- [ ] T034 [P] [US3] Write `docs-site/build/orchestration/overview.mdx`: names handoffs, tasks, and task groups and routes onward; names, does not teach
- [ ] T035 [US3] Move `docs-site/build/two-agents.mdx` to `docs-site/build/orchestration/handoffs.mdx` and reframe as a concept: one-way handoff, `context` carries history and variables, tool lists as the guardrail; re-validate snippets
- [ ] T036 [P] [US3] Move `docs-site/build/tasks.mdx` to `docs-site/build/orchestration/tasks.mdx`: delegation, typed results, `assign`, what the agent cannot do while delegated; re-check against merged code
- [ ] T037 [P] [US3] Move `docs-site/build/task-groups.mdx` to `docs-site/build/orchestration/task-groups.mdx`; re-run and keep the captured LiveKit warning
- [ ] T038 [US3] Move `docs-site/build/choosing-a-structure.mdx` to `docs-site/build/orchestration/choosing-a-structure.mdx` and enhance into a decision aid: symptom paired with the fixing shape, cost of each shape (handoff is one way, task returns, group shares context), "more tools before a second agent", delegate versus transfer
- [ ] T039 [US3] Finalize the Build group in `docs-site/docs.json` to exactly the contracts/navigation.md tree; sweep every inbound link to a moved page (grep old slugs: `build/one-agent`, `build/tools`, `build/two-agents`, `build/tasks`, `build/task-groups`, `build/choosing-a-structure`); `mint validate && mint broken-links`
- [ ] T040 [P] [US3] Re-check `docs-site/build/variables.mdx` against the merged code (unchanged shape expected; the check is what ships)
- [ ] T041 [US3] Confirm the flag mapping in `internal/cli/help_capture_test.go` still points at existing pages (CLI pages did not move); update the mapping without weakening the test if any moved path is referenced; run `go test ./internal/cli -run TestHelpCapture`
- [ ] T042 [US3] Add the execution-blocks agreement test `internal/spec/tools_docsite_test.go`: the block set stated on `docs-site/build/tools/overview.mdx` equals the `Tool` struct's execution blocks (research R17); prove it fails when the page lies, restore, `gofmt`, `go test ./internal/spec`

**Checkpoint**: Concept-first Build group, agreement tests green.

---

## Phase 6: User Story 5 - Models and what makes them fast (Priority: P5)

**Goal**: A top-level Models group (stt, tts, llm, turn-detection, optimization) from the merged catalogs and the SLNG execution-layer docs; `reference/providers` retired with its test retargeted; Context Router absent.

**Independent Test**: Retargeted agreement test passes and fails when a list lies; `grep -rin "context router\|context-router" docs-site` empty; every optimization claim attributed and dated; the scaffold page names SLNG models by design.

- [ ] T043 [US5] Derive the vendor lists per role per target from the merged `internal/target/catalog_pipecat.go` and `catalog_livekit.go` (Listen, Speak, Reason, and the turn/VAD reality per target), plus the SLNG model ids proven in this repo; capture for the verification log (research R21)
- [ ] T044 [P] [US5] Re-fetch the SLNG execution-layer docs with dates (`/execution-layer`, `/how-it-works`, `/adaptive`, `/stt-performance-layer`, `/tts-path-optimization`); note the STT layer's Private Beta status and the exact claims to attribute (research R22)
- [ ] T045 [US5] Write the four role pages `docs-site/models/{stt,tts,llm,turn-detection}.mdx` from T043's lists: SLNG first, model names only for SLNG linked to https://docs.slng.ai/models, pass-through stated for other vendors, per-target differences explicit (FR-040)
- [ ] T046 [US5] Write `docs-site/models/optimization.mdx` from T044's fetches: SLNG models by design, the STT Performance Layer (SLNG's own Private Beta status stated) and TTS Path Optimization only, every claim attributed and linked, zero Context Router mentions (FR-041); add the "SLNG models by design" line where the scaffold is first shown in `docs-site/start/quickstart.mdx`, linking to the Models group (FR-042)
- [ ] T047 [US5] Retire `docs-site/reference/providers.mdx` (`git rm`), retarget `internal/target/providers_docsite_test.go` to the four role pages (SLNG-first rule kept, updated and never weakened), re-point every inbound link, add the Models group to `docs.json` after Targets; prove the test fails when a role page lies, restore, `go test ./internal/target`; `mint validate && mint broken-links`

**Checkpoint**: Models group live, catalog facts under test, providers page gone.

---

## Phase 7: User Story 6 - A developer lives in the dev loop (Priority: P6)

**Goal**: The Dev group becomes "Development lifecycle": overview (loop + per-mode logs), console, telephony (the local run, moved from telephony/overview), webhooks-and-tunnels (moved in).

**Independent Test**: The group holds four pages; log-location claims match real runs per mode; `grep -rn "dev --telephony" docs-site/telephony` shows a link at most.

- [ ] T048 [US6] Run each documented dev mode on the merged tree and capture where logs land: web (compose log path), `--console` (no log file expected, `dev.go` C6 comment), `--telephony` (`telephony.log` in the build output directory); verify the `--verbose` default wording (research R23)
- [ ] T049 [US6] Update `docs-site/dev/overview.mdx`: the loop (change, `unmute dev`, talk, read the logs, iterate) with T048's per-mode log locations, each verified, and `--verbose` explained
- [ ] T050 [US6] Write `docs-site/dev/telephony.mdx` from the local-run material leaving `docs-site/telephony/overview.mdx`: what `dev --telephony` does and how it happens (tunnel, automatic webhook, local process, undo on exit), plus `--no-webhook`, `--public-url`, `--to`; slim `telephony/overview.mdx` to concepts and routes with a link here (FR-043)
- [ ] T051 [US6] Move `docs-site/telephony/webhooks-and-tunnels.mdx` to `docs-site/dev/webhooks-and-tunnels.mdx` (`git mv`), re-check against merged code, sweep inbound links
- [ ] T052 [US6] Update `docs.json`: rename the group label to "Development lifecycle" with pages overview, console, telephony, webhooks-and-tunnels; remove webhooks-and-tunnels from the Telephony group; `mint validate && mint broken-links`

**Checkpoint**: The local loop has one home and it matches reality.

---

## Phase 8: User Story 7 - Go live and wire the phone number (Priority: P7)

**Goal**: Per-platform CLI go-live guides grounded in the emitted runbooks, and a Twilio provider page mapping console values to connection env names.

**Independent Test**: Every command on the guides was run or carries a dated attribution plus an unverified-list entry; the Twilio page's env mapping matches the telephony examples' connection files exactly.

- [ ] T053 [US7] Compile a package per target and extract the generated README deploy sections as the command truth: LiveKit (`lk cloud auth`, `lk agent create --secrets-file .env`, `lk agent deploy`) and Pipecat ("Deploy to Pipecat Cloud": `pipecat cloud secrets set <set> --file .env`, `pipecat cloud deploy`, region and `--min-agents` variants); capture for the verification log (research R24)
- [ ] T054 [P] [US7] Fetch the current LiveKit Cloud and Pipecat Cloud CLI docs with dates; note where they and the emitted runbooks agree, and mark every step that cannot be executed here (needs a real cloud account) for the unverified list
- [ ] T055 [US7] Write `docs-site/deploy/livekit-cloud.mdx`: the generated project to LiveKit Cloud via the platform CLI, from T053's runbook extract, re-checked against T054; the reader's generated README stays the runbook of record (FR-045)
- [ ] T056 [US7] Write `docs-site/deploy/pipecat-cloud.mdx`: same shape for Pipecat Cloud (FR-045)
- [ ] T057 [US7] Write `docs-site/telephony/twilio.mdx` per research R25: buy a number, find the account SID and auth token, which connection env name each value fills, separate sections for the Pipecat routes and the LiveKit route as this repo's code defines them (our LiveKit dev flow creates local trunks automatically; the pasted LiveKit sample is shape inspiration only), dated Twilio doc fetches, no carrier-provisioning claims (FR-046); register the three new pages in `docs.json` (Deployment: livekit-cloud, pipecat-cloud, going-live; Telephony gains twilio); `mint validate && mint broken-links`

**Checkpoint**: The last mile is documented for both platforms, honestly.

---

## Phase 9: User Story 4 - The maintainers trust what changed (Priority: P4, runs last by dependency)

**Goal**: Dated, append-only addenda in the 008 artifacts; D1 to D5 re-checked with verdicts; every unverifiable claim stated.

**Independent Test**: quickstart.md section 9.

- [ ] T058 [US4] Re-check D1 to D5 from `specs/008-mintlify-user-docs/report.md` against the merged tree, each getting a verdict (stands, stale, changed) with code and doc locations re-read: D1 subagents naming, D2 root README target coverage, D3 module path, D4 the `Inject` comment versus the validator (N40 likely changed it: read `internal/spec/package.go` and `internal/ir/variables.go`), D5 DEPLOYMENT.md telephony claim; change no product code or doc
- [ ] T059 [P] [US4] Append the dated navigation amendment to `specs/008-mintlify-user-docs/contracts/navigation.md`: the 49-page tree, the moves and retirements, the group rename, and why (maintainer feedback plus N40/N41), in the file's existing amendment style
- [ ] T060 [P] [US4] Append the dated addendum to `specs/008-mintlify-user-docs/verification-log.md`: every scratch package, command, refusal capture, re-captured transcript, dev-mode log run, and external fetch date (SLNG, LiveKit Cloud, Pipecat Cloud, Twilio) from T009 to T057
- [ ] T061 [US4] Append the dated addendum to `specs/008-mintlify-user-docs/report.md`: the 49-page table with anchors and verification status, the D1 to D5 verdicts from T058, any new discrepancies (including T008's `kind:` finding if real), the note that `reference/connections-yaml` relies on scratch-validated shapes rather than an agreement test (the accepted 008 precedent), and the unverified claims: the standing three plus any unexecuted cloud-deploy steps from T054
- [ ] T062 [P] [US4] Update `docs-site/README.md`: the new structure (9 groups, 49 pages), the four agreement tests, the models rules (SLNG first, attributed execution-layer claims, no Context Router), and any other rule this work changed; leave the go-live checklist exactly where and as it is
- [ ] T063 [US4] True up the 008 `tasks.md` phase from T007: every item marked to match what actually happened

**Checkpoint**: The honesty trail is complete and nothing was settled silently.

---

## Phase 10: Polish and final gates

**Purpose**: Prove done per quickstart.md, end to end, in one sitting.

- [ ] T064 Run the full gate table: `go test ./...`, `make lint`, `gofmt -l internal/` empty, `cd docs-site && mint validate && mint broken-links`, `mint dev --no-open` and probe one moved and one new page, all eleven examples validate and compile with READMEs
- [ ] T065 [P] Run the sweeps: `grep -rn "—\|–" docs-site` empty; `grep -rin "vapi" docs-site` nothing as a target; `grep -rin "context router\|context-router" docs-site` empty; `grep -rn "11LABS" docs-site` empty; moved-field greps judged clean; `.mdx` count equals `docs.json` page count equals 49; `reference/providers` gone with zero inbound links
- [ ] T066 Final story read-through of the Build and Development lifecycle groups in sidebar order: no concept used before taught, every page says why the reader is there and where to go next; fix in place, then re-run any gate a fix touched
- [ ] T067 Commit the finished work in reviewable commits (merge already committed in T002); do not push, do not deploy the docs site, create no Mintlify project

---

## Dependencies & Execution Order

- **Setup (Phase 1)** → **Foundational (Phase 2)** → user stories. Nothing may precede the merge (FR-027).
- **US1 (Phase 3)** blocks US3, US6, and US7 in practice: they move or link pages US1 corrects, and US7 links `reference/connections-yaml`.
- **US2 (Phase 4)** depends on Foundational only; it seeds the Tools group US3 fills out (T027's page defines its own terms until T030 exists).
- **US3 (Phase 5)** depends on US1 and US2.
- **US5 (Phase 6)** depends on Foundational (catalog facts) and touches `start/quickstart.mdx`, so it runs after T009; independent of US3 and US6 otherwise.
- **US6 (Phase 7)** depends on US1 (T015 updated `telephony/overview` content that T050 then splits).
- **US7 (Phase 8)** depends on US1 (connections reference, going-live fix) and US6 (dev/telephony exists for links).
- **US4 (Phase 9)** depends on everything before it: addenda describe finished work. T058 (D re-check) can start any time after Phase 2.
- **Polish (Phase 10)** last.

Within US1: T009 to T013 first, then T014 to T021 in parallel, then T022, then T023. Within US3: T028 first; T039, T041, T042 after the pages they touch exist. Within US5: T043 and T044 before the writing tasks; T047 last. `docs.json` is edited only in T010, T027, T029, T039, T047, T052, T057: never in two parallel tasks.

## Parallel Example: User Story 1

After T009 to T013, these touch different files and share no capture:

```text
T014 targets/overview.mdx
T015 telephony/overview.mdx
T016 telephony/outbound-calls.mdx
T017 transfers/overview.mdx
T018 transfers/livekit.mdx
T019 transfers/pipecat-daily.mdx
T020 transfers/pipecat-twilio.mdx
T021 reference/cli/compile.mdx + telephony/first-phone-call.mdx
```

## Implementation Strategy

MVP is US1: after Phase 3 the site is correct about the merged product in the
old structure, with the env-name fix in, and that alone is shippable. US2 adds
the one genuinely new authoring page. US3 is the Build restructure as one
reviewable unit. US5 (Models), US6 (Development lifecycle), and US7 (go live
and Twilio) each land as their own reviewable unit with their own gates. US4
and Polish close the trail. Stop at any checkpoint: each leaves the site valid
(`mint validate` and `broken-links` pass at every checkpoint by construction).

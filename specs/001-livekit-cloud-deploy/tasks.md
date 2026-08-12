---

description: "Task list for cloud-deployable builds, region aware, on both shipped targets"
---

# Tasks: Cloud-deployable builds, region aware, on both shipped targets

**Input**: Design documents from `specs/001-livekit-cloud-deploy/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/](./contracts/)

**Tests**: included, and not optional here. The constitution requires that non-trivial logic leaves one runnable check behind, and FR-026 requires every test that describes the old artifact to be updated in the same change.

**Organization**: grouped by user story. Two stories are P1 because both clouds are broken and each unblocks a different T8 row.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: US1 to US6, matching [spec.md](./spec.md)
- Every task names its file

## Path conventions

Go source under `internal/`, templates under `internal/generate/templates/<driver>/`, goldens under `internal/generate/testdata/golden/`, documents under `docs/`, examples under `examples/`. No `src/` or `tests/` tree in this repository: tests live beside the code they cover.

---

## Phase 1: Setup

**Purpose**: establish a known-good baseline before touching anything, because this change rewrites goldens on both drivers and a pre-existing failure would be indistinguishable from a new one.

- [ ] T001 Run the gate on a clean tree and record that it passes, using the targets in `Makefile`: `make fmt && make lint && make build && make test`
- [ ] T002 [P] Read `docs/REPO_MAP.md`'s closing note before touching any document: `docs/spec/` was retired in commit `063289c`, so citations like `driver-livekit V27` or `compiler V35` are history only (`git show 959af97:docs/spec/driver-livekit.md`). The live homes for this feature's facts are `docs/SCHEMA.md`, `docs/user/`, `specs/001-livekit-cloud-deploy/contracts/`, and the tests

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: put the one-or-many region type through the pipeline and land the rule that keeps it honest. Every task here is **behaviour-neutral for a single region**, which is what lets the suite stay green while the emitters are still untouched. The multi-region refusal ships in this phase rather than later, so there is never a window in which a two-region Pipecat package silently drops one.

**⚠️ CRITICAL**: no user story work begins until this phase is complete and green.

- [ ] T003 [P] Create `internal/spec/regions.go` with `type Regions []string` implementing goccy's `InterfaceUnmarshaler` (`UnmarshalYAML(func(any) error) error`): try `[]string` first, fall back to `string`, return the list attempt's error if both fail, so the message keeps goccy's position; add `MarshalYAML` emitting a bare scalar when there is exactly one region
- [ ] T004 Change `Target.DeploymentRegion` to type `Regions` in `internal/spec/package.go:347`, keeping the `deployment_region` yaml and json keys unchanged
- [ ] T005 Add a `jsonschema.ForOptions{TypeSchemas: ...}` override for `Regions` in `internal/spec/schema.go` producing `oneOf: [string, array of string]`, following the same hook `internal/ir/schema.go` already uses; do not hand-author any `.json` schema
- [ ] T006 [P] Add a table test to `internal/spec/load_test.go`: a scalar decodes to one region; a list decodes in declared order; a mapping value fails with `targets.yaml` plus a line and column in the message; a single region re-serialises as a bare scalar
- [ ] T007 Rename `Target.DeploymentRegion string` to `DeploymentRegions []string` with json key `deployment_regions` in `internal/ir/compiler.go:461`
- [ ] T008 Copy the declared regions in order into the IR at `internal/ir/build.go:797`, with no deduplication and no default region invented
- [ ] T009 Add two errors to `validateTarget` in `internal/ir/validate.go:475`: an empty string entry in the list, and the same region declared twice
- [ ] T010 Add `FieldDeploymentMultiRegion Field = "deployment_region.multiple"` to `internal/target/table.go` with `field(deny("pipecat", ...), deny("vapi", ...), deny("deepgram", ...))`, the Pipecat note quoting that platform's own reason: agent names are globally unique across regions, so a second region needs a differently named agent
- [ ] T011 Apply that capability in `internal/ir/validate.go` through the existing `applyCapability`, **only when more than one region is declared**, so a single-region package on any provider stays green
- [ ] T012 [P] Add validation tests to `internal/ir/validate_test.go`: two regions on Pipecat is a gated error naming the target and quoting its reason; two on LiveKit is clean; one on Pipecat is clean; a duplicate is an error; an empty entry is an error
- [ ] T013 Thread `DeploymentRegions []string` into `livekitData` in `internal/generate/livekit_v1.go:307` and `internal/generate/livekit_v1_build.go:42`, leaving single-region output byte-identical for now
- [ ] T014 Thread the single region into `pipecatData` in `internal/generate/pipecat_v1.go:234` and `internal/generate/pipecat_v1_build.go:23`, reading element zero because validation now guarantees at most one on this target
- [ ] T015 Add `FieldDeploymentMultiRegion: true` to `livekitEmittedFields` in `internal/generate/livekit_v1.go:406` so the emitter-against-table agreement test stays honest, and leave it out of `pipecatEmittedFields`
- [ ] T016 Change `Data.DeploymentRegion` to `DeploymentRegions []string` in `internal/scaffold/scaffold.go:50` and its reset at `:253`
- [ ] T017 Render a bare scalar for one region and a list for several in `internal/scaffold/templates/targets.yaml.tmpl:12`
- [ ] T018 Make the deployment region prompt at `internal/tui/tui.go:2657` a comma-separated field (split and trim on save, join for display) and carry the list through `packageData` in `internal/tui/maintain.go:133`, so opening the maintain flow on a multi-region package cannot silently drop a region
- [ ] T019 [P] Add a TUI test through the accessible renderer proving a two-region package survives a maintain round-trip with both regions intact

**Checkpoint**: a single-region package compiles to byte-identical output, a list is accepted, and a list of more than one is refused everywhere except LiveKit. Goldens must be unchanged at this point; if any moved, something in T013 or T014 was not behaviour-neutral.

---

## Phase 3: User Story 1 - A compiled LiveKit package deploys to LiveKit Cloud (Priority: P1) 🎯 MVP

**Goal**: `unmute compile` then the commands the generated README prints put the agent in LiveKit Cloud, with no edits to generated files.

**Independent Test**: compile `examples/human-transfer`, follow only `build/livekit/README.md`, and see the agent healthy in the Agents dashboard.

- [ ] T020 [P] [US1] Delete `internal/generate/templates/livekit_v1/livekit.toml.tmpl`
- [ ] T021 [US1] Remove the `{"livekit.toml", "livekit.toml"}` entry from the outputs list in `renderLiveKitFiles` at `internal/generate/livekit_v1.go:562`
- [ ] T022 [US1] Invert the assertion at `internal/generate/livekit_v1_test.go:1647` so it fails if any `livekit*.toml` is emitted, replacing the current check that one exists with `id = "livekit"`
- [ ] T023 [US1] Build the per-region deploy view in `internal/generate/livekit_v1_build.go`: one row per declared region carrying the region and a config file name that is empty for a single region and `livekit.<region>.toml` when several are declared, per [data-model.md](./data-model.md#the-livekit-per-region-view)
- [ ] T024 [US1] Rewrite the Deploy section of `internal/generate/templates/livekit_v1/README.md.tmpl:41-57`: the first-deploy command per region carrying `--region`, the redeploy command per region carrying no region flag, both labelled; self-hosted kept and labelled; the "not the supported path" sentence deleted; a line saying region is fixed at the first deploy with the platform's move procedure; and when no region is declared, a line saying the platform will ask which one to use
- [ ] T025 [P] [US1] Add an unprivileged user to `internal/generate/templates/livekit_v1/Dockerfile.tmpl` and switch to it after the dependency install, before the existing `CMD`
- [ ] T026 [P] [US1] Widen the emitted `.dockerignore` at `internal/generate/livekit_v1.go` from `.env` to `.env`, `.env.*`, `.venv/`, `__pycache__/`
- [ ] T027 [US1] Add `deployment_regions` (omitempty) to `livekitReportJSON` and populate it in `livekitReport` at `internal/generate/livekit_v1.go:718`
- [ ] T028 [US1] Add README assertions to `internal/generate/livekit_v1_test.go` from [contracts/artifacts.md](./contracts/artifacts.md): both commands present and distinguishable, the region flag on the first-deploy command only, per-region file names when several regions are declared and the default name when one, and no sentence calling either path unsupported
- [ ] T029 [US1] Correct the LiveKit part of `docs/user/learn/08-going-live.md`: it must stop saying LiveKit adds a `livekit.toml` (line 102 area), name the first-deploy versus redeploy distinction, and point at the generated README as the home for the commands
- [ ] T030 [US1] Regenerate `internal/generate/testdata/golden/livekit_v1_remy.txt` with its `-update` flag and **read the diff**: expect exactly one file removed from the list, the rewritten Deploy section, the Dockerfile and `.dockerignore` changes, and `deployment_regions`; anything else is a bug in the change

**Checkpoint**: US1 is independently demonstrable on a real LiveKit Cloud project.

---

## Phase 4: User Story 2 - A compiled Pipecat package deploys to Pipecat Cloud (Priority: P1)

**Goal**: `unmute compile` then the command the generated README prints reaches a `ready` agent on Pipecat Cloud, built in the cloud from the emitted Dockerfile.

**Independent Test**: compile `examples/human-transfer-daily`, follow only `build/pipecat/README.md`, and see `pipecat cloud agent status` report `ready`.

- [ ] T031 [US2] Remove the unconditional `image = "{{.Project}}:latest"` line from `internal/generate/templates/pipecat_v1/pcc-deploy.toml.tmpl:2`, since that key switches off the cloud build the documented deploy depends on
- [ ] T032 [US2] Add a Deploy section to `internal/generate/templates/pipecat_v1/README.md.tmpl` using the current `pipecat cloud` CLI name: the deploy command, the status command, a line stating that deploying the same agent name again updates the existing agent while running sessions finish on the old image, a line stating that a failed deploy leaves the previous ready version serving, and what happens to the region when none is declared
- [ ] T033 [US2] Add `deployment_regions` (omitempty) to `pipecatReportJSON` at `internal/generate/pipecat_v1.go:473` and populate it in `pipecatReport`
- [ ] T034 [P] [US2] Add tests to `internal/generate/pipecat_v1_test.go`: the emitted manifest has no `image` key; the README has a Deploy section; no emitted file or document in the artifact uses the retired `pcc` CLI name
- [ ] T035 [US2] Correct the Pipecat part of `docs/user/learn/08-going-live.md`: the current `pipecat cloud` CLI name, that the image is built in the cloud from the emitted Dockerfile, and that the manifest no longer names an image (same file as T029, so sequential, not parallel)
- [ ] T036 [US2] Regenerate the Pipecat goldens (`internal/generate/testdata/golden/pipecat_v1.txt` and any pcc-deploy golden) with their `-update` flags and **read the diff**: expect the `image` line gone, the Deploy section added, and `deployment_regions`

**Checkpoint**: both clouds now accept a freshly compiled package. Both T8 rows have a deployable artifact.

---

## Phase 5: User Story 3 - Secrets reach the deployed agent (Priority: P2)

**Goal**: each README tells the operator exactly how to get the package's declared environment into the deployed agent, in that platform's own way, with no secret value written anywhere.

**Independent Test**: deploy each example following its README's secrets step, then list the deployed secret names and compare against the package's required-env list.

- [ ] T037 [US3] Derive the secret set name `<project>-secrets` in `internal/generate/pipecat_v1_build.go` when `pipecatData.Secrets` is non-empty, and leave it unset otherwise
- [ ] T038 [US3] Emit `secret_set = "<name>"` in `internal/generate/templates/pipecat_v1/pcc-deploy.toml.tmpl` when that name is set, so the manifest describes where its environment comes from and a deploy that skipped the secrets step fails at deploy time
- [ ] T039 [US3] Add the secrets step to the Pipecat README Deploy section in `internal/generate/templates/pipecat_v1/README.md.tmpl`, **before** the deploy command: `pipecat cloud secrets set <name> --file .env` carrying `--region <declared>` whenever a region is declared, plus the verified asymmetry when none is declared (a secret set defaults to `us-west` while the agent goes to the organisation default, so both need an explicit region if that default was changed), plus how to update the set later
- [ ] T040 [US3] Add the secrets step to the LiveKit README Deploy section in `internal/generate/templates/livekit_v1/README.md.tmpl`: the env file passed to the first-deploy command, a line stating that `LIVEKIT_URL`, `LIVEKIT_API_KEY`, and `LIVEKIT_API_SECRET` are injected by the platform and must not be sent, and the update-secrets command with whether it merges or replaces
- [ ] T041 [P] [US3] Add tests to both driver test files: `secret_set` present exactly when the package declares secrets; the region flag on the Pipecat secrets command when a region is declared; the three injected LiveKit values named in the LiveKit README; and no secret value in any emitted file
- [ ] T042 [US3] Regenerate both drivers' goldens and read the diffs

**Checkpoint**: a deployed agent on either platform has every environment name it needs, and no artifact carries a value.

---

## Phase 6: User Story 4 - The declared region is where the agent lands (Priority: P2)

**Goal**: one region or several, forwarded to the right command on each platform, refused where the platform cannot do it, and inspectable afterwards.

**Independent Test**: declare one region, compile, deploy, confirm the region matches with no prompt; then declare two on the LiveKit instance and confirm the README carries one first-deploy command per region with per-region config file names.

**Note**: this story's emitter behaviour arrives with T023 and T024 in US1, deliberately, so the most delicate template is written once rather than twice. What remains here is the visible authoring surface, its documentation, and its proof.

- [ ] T043 [US4] Declare a `deployment_region` on the LiveKit instance in `examples/human-transfer/targets.yaml` so the T8 rig runs without an interactive prompt
- [ ] T044 [US4] Add amendment **N32** to `docs/SCHEMA.md`: `deployment_region` accepts one region or a list; the scalar form stays valid so existing packages still decode; a list of more than one is LiveKit only, gated elsewhere with the platform's reason; region codes stay unvalidated and no code list is printed; dated 2026-08-12 with both platform sources
- [ ] T045 [US4] Assert the derived authoring schema in a test in `internal/spec` (extend `load_test.go` or add `schema_test.go`) that `deployment_region` renders as a `oneOf` of a string and an array of strings, mirroring how `internal/ir/schema_test.go` pins its unions, so the published contract cannot silently lose a shape
- [ ] T046 [US4] Update the `deployment_region` row in `docs/user/reference/targets-yaml.md`: both shapes, what an unset region means on each platform, that a multi-region list is LiveKit only, and the one-instance-per-region alternative for Pipecat
- [ ] T047 [US4] Add the second-region instructions to the Pipecat README in `internal/generate/templates/pipecat_v1/README.md.tmpl`: its own secret set in that region, then a deploy under a differently named agent, because agent names are globally unique across regions
- [ ] T048 [P] [US4] Add tests: a one-element list produces output identical to the scalar form including file names; two regions on LiveKit produce two first-deploy and two redeploy commands with per-region file names; `deployment_regions` appears in both drivers' reports and is absent when no region is declared
- [ ] T049 [US4] Regenerate any golden the example's new region moves, and read the diff

**Checkpoint**: region is authored once and lands correctly on both platforms, or is refused with a reason.

---

## Phase 7: User Story 5 - Redeploy without duplicating or stranding (Priority: P2)

**Goal**: a recompile never costs the operator their deployed agent, and never invites a second billable one.

**Independent Test**: deploy, recompile, deploy again on both platforms; the agent count does not grow on either.

- [ ] T050 [US5] Generalise the preservation in `writeArtifactFiles` at `internal/cli/compile.go:147-178` from the single `.env` path to a matched set of `.env` plus `livekit*.toml`, keeping the existing read-before, restore-after shape and its error wrapping
- [ ] T051 [P] [US5] Add an L2 test to `internal/cli/compile_test.go`: a fake `livekit.toml` written into a build directory survives a recompile byte for byte, including a per-region name like `livekit.us-east.toml`, and `.env` still survives
- [ ] T052 [US5] Add the two recovery notes to the LiveKit README Deploy section in `internal/generate/templates/livekit_v1/README.md.tmpl`: the command that rebuilds a lost config file from an agent ID, and that a config file from another project produces the subdomain mismatch error along with how to resolve it; plus the status and logs commands, noting that `Sleeping` is healthy on plans that scale to zero
- [ ] T053 [US5] Extend the doc comment above `writeArtifactFiles` in `internal/cli/compile.go` to record why `build/` gains a second preserved file, naming the constitution's disposable-artifact rule it deviates from and the duplicate-agent failure it prevents, so the exception reads as intent rather than oversight
- [ ] T054 [US5] Regenerate the LiveKit golden and read the diff

**Checkpoint**: the deploy, edit, redeploy loop is safe on both platforms.

---

## Phase 8: User Story 6 - The rig walkthroughs read as runnable sequences (Priority: P3)

**Goal**: every document that describes deploying matches the generated READMEs, in order, with current CLI names, and none becomes a fourth copy of the command list.

**Independent Test**: follow `docs/TRANSFERS.md` section 4, both rigs, from step 1, and confirm every command runs.

- [ ] T055 [US6] Rewrite section 4 of `docs/TRANSFERS.md`: the LiveKit rig's deploy step points at the generated README for the commands and states the CLI prerequisites (authenticate, set a default project) before the step that needs them; the Pipecat rig uses the current `pipecat cloud` CLI name throughout, orders the secret set before the deploy, and its teardown names the current delete command
- [ ] T056 [P] [US6] Update the deploy step in `examples/human-transfer/README.md` to point at the generated README rather than naming a command that fails, and keep the trunk setup as it is
- [ ] T057 [P] [US6] Update `examples/human-transfer-daily/README.md` to the current CLI name and the secret-set-before-deploy order, pointing at the generated README for the commands
- [ ] T058 [P] [US6] Sweep the rest of `docs/user/` for statements this change falsifies (`grep -rn "livekit.toml\|pcc \|agent create" docs/user/`) and fix anything T029 and T035 did not already cover, including `docs/user/reference/secrets.md` if it describes deploy-time secrets
- [ ] T059 [US6] Read both generated READMEs against `docs/TRANSFERS.md`, `examples/human-transfer/README.md`, `examples/human-transfer-daily/README.md`, and `docs/user/learn/08-going-live.md` side by side, and confirm they name the same commands in the same order per platform with no fourth copy of a command list

**Checkpoint**: T8 has a walkthrough that runs end to end on both platforms.

---

## Phase 9: Polish and cross-cutting

- [ ] T060 Run the full gate from `Makefile`: `make fmt && make lint && make build && make test`
- [ ] T061 [P] Run `make smoke` (needs `uv`) to prove the emitted Python still installs, imports, and instantiates after the Dockerfile and manifest changes
- [ ] T062 [P] Prove SC-009 by searching `docs/`, `examples/`, and `internal/generate/templates/`: no document or template names the retired `pcc` CLI form, and none claims a `livekit.toml` is emitted
- [ ] T063 Walk the offline half of [quickstart.md](./quickstart.md), including the region shape table and the recompile-preservation check
- [ ] T064 Re-read the full golden diff for both drivers one last time before committing, against the expected changes listed in [contracts/artifacts.md](./contracts/artifacts.md)

---

## Dependencies and execution order

### Phase dependencies

- **Setup (Phase 1)**: no dependencies
- **Foundational (Phase 2)**: after Setup. **Blocks every user story**, because every emitter task reads the resolved region list
- **US1 (Phase 3)** and **US2 (Phase 4)**: after Foundational. Independent of each other: different drivers, different templates, different goldens. Either can be worked first, and either alone is a shippable increment
- **US3 (Phase 5)**: after US1 and US2, because it edits the Deploy section each of them creates
- **US4 (Phase 6)**: after US1 (its LiveKit fan-out arrives in T023 and T024) and after US2 for the Pipecat second-region text
- **US5 (Phase 7)**: `internal/cli/compile.go` work (T050, T051) is independent of everything and can be done any time after Foundational; the README notes (T052) need US1
- **US6 (Phase 8)**: after US1 and US2, since it points at their READMEs
- **Polish (Phase 9)**: last

### File conflicts to respect

Three tasks edit `internal/generate/templates/livekit_v1/README.md.tmpl` in different phases (T024, T040, T052) and three edit the Pipecat README (T032, T039, T047). Two edit `docs/user/learn/08-going-live.md` (T029, T035). None of these are marked `[P]`: same file, sequential.

### Parallel opportunities

- Foundational: T003 with T006; T012 with T010's aftermath; T019 alongside T016 to T018
- US1: T020, T025, T026 are three different files and can go together; T028 alongside T029
- US2: T034 alongside T035
- US6: T056, T057, T058 are three different documents
- Across stories: once Foundational is green, one person can take the LiveKit driver (US1, then US5's compile work) while another takes the Pipecat driver (US2), with no shared file until US3

### Parallel example: User Story 1

```bash
# Three different files, no shared state:
Task: "Delete internal/generate/templates/livekit_v1/livekit.toml.tmpl"
Task: "Add an unprivileged user to internal/generate/templates/livekit_v1/Dockerfile.tmpl"
Task: "Widen the emitted .dockerignore in internal/generate/livekit_v1.go"
```

---

## Implementation strategy

### MVP

Phase 1, Phase 2, Phase 3. That is a LiveKit package that deploys to LiveKit Cloud, which unblocks the warm transfer in the Agent Console: the single most valuable thing here, because it is the test T8 has never been able to run. Stop and validate against a real project before going further.

### Incremental delivery

1. Setup and Foundational: the region type is accepted and the refusal is honest, with output unchanged
2. US1: LiveKit deploys. Ship, then run the warm transfer in the Console
3. US2: Pipecat deploys. Ship, then run the Daily cold transfer
4. US3: secrets documented on both. Now a deploy is actually usable rather than merely successful
5. US4: region authoring surface and its documents, plus SCHEMA N32
6. US5: the redeploy loop stops being a foot-gun
7. US6: the walkthroughs match

Each step leaves the suite green and the artifacts better than before.

### Notes

- Goldens are read, never regenerated blind. Every phase that touches an emitter ends with a diff review, and an unexpected line in that diff is a bug in the change.
- Document changes land with the code they describe: the going-live guide in US1 and US2, `docs/SCHEMA.md` N32 and `docs/user/reference/targets-yaml.md` in US4, the rigs in US6. That is Principle IV, not paperwork. `docs/spec/` is retired (commit `063289c`), so the per-driver invariants that used to live there are now carried by this feature's [contracts/](./contracts/) plus the tests that encode them.
- No new dependency is needed at any point. If one seems necessary, stop: something has gone wrong.

# Phase 0 research

All platform claims verified 2026-08-12 against live documentation, per Constitution principle IV. The two contract tables live in [spec.md](./spec.md#verified-platform-contract-source-of-truth-for-this-feature) and are not repeated here. This file records what was found in *our* code and the mechanism decisions that follow.

## What the code actually does today

| Finding | Evidence |
|---|---|
| The emitted `livekit.toml` carries only `[agent] id = "<agent_name>"`. Two of its three problems are visible in the template itself; the third is that the file exists at all, which makes `lk agent create` refuse. | `internal/generate/templates/livekit_v1/livekit.toml.tmpl`, emitted at `internal/generate/livekit_v1.go:562` |
| The LiveKit README's Deploy section documents self-hosted workers and calls the working Cloud command "not the supported path". | `internal/generate/templates/livekit_v1/README.md.tmpl:41-57` |
| The Pipecat manifest sets `image = "<project>:latest"` unconditionally. The platform documents that key as switching cloud builds **off**, so the deploy skips the emitted Dockerfile and tries to pull a tag that exists nowhere. | `internal/generate/templates/pipecat_v1/pcc-deploy.toml.tmpl:2`; [CLI deploy reference](https://docs.pipecat.ai/api-reference/cli/cloud/deploy) |
| The Pipecat manifest names no `secret_set`, and the Pipecat README has no Deploy section at all. | same template; `internal/generate/templates/pipecat_v1/README.md.tmpl` (no match for "Deploy") |
| `deployment_region` reaches both drivers but only lands in the LiveKit README's disclaimed footnote and in the Pipecat manifest the platform cannot deploy. | `internal/spec/package.go:347` → `internal/ir/build.go:797` → `internal/generate/{livekit,pipecat}_v1_build.go` |
| `deployment_region` appears in **neither** driver's compile report, though the constitution names it as a forwarded value that must be inspectable there. | no match for the key in `internal/generate/testdata/golden/*.txt`; `livekitReport` at `internal/generate/livekit_v1.go:718` |
| `writeArtifactFiles` does `os.RemoveAll(outDir)` and preserves exactly one path, `.env`. Anything the platform writes into a build directory is destroyed by the next compile. | `internal/cli/compile.go:147-178` |
| Repository docs still use the retired `pcc` CLI name in six places. | `docs/TRANSFERS.md:204,209,210,223`; `examples/human-transfer-daily/README.md:42,44` |
| `docs/user/learn/08-going-live.md:102` tells readers that LiveKit "adds `livekit.toml`", which stops being true. | that line |

## D1: how one-or-many decodes, keeps its position, and stays derived

**Decision.** A named type in `internal/spec`, `type Regions []string`, implementing goccy's `InterfaceUnmarshaler` (`UnmarshalYAML(func(any) error) error`): try `[]string` first, fall back to `string`, and if both fail return the error from the list attempt. Pair it with a `MarshalYAML` that writes a bare scalar when there is exactly one region, so a TUI round-trip cannot rewrite someone's file shape. In `internal/spec/schema.go`, pass `jsonschema.ForOptions{TypeSchemas: {reflect.TypeFor[Regions](): oneOf{string, array-of-string}}}`.

**Rationale.** The loader decodes with `yaml.Strict()` (`internal/spec/load.go:146`), and the repository picked goccy specifically for line and column on errors. Of goccy's unmarshaler interfaces, `BytesUnmarshaler` returns our error verbatim with no position attached (`decode.go:791-800`), while `InterfaceUnmarshaler` hands back a closure that calls the decoder's own `decodeValue` (`decode.go:817-831`), so a genuinely wrong value (a map, a nested list) fails with goccy's positioned error rather than a bare sentence. The schema override is not a hand-authored schema file: it is the documented `ForOptions` hook for a type reflection cannot express, and `internal/ir/schema.go` already uses exactly this hook for its unions and enums, so this follows precedent instead of setting one.

**Alternatives considered.** `BytesUnmarshaler` (loses position, which is the one thing this codebase pays a dependency for). A list-only field with a "the schema moved" error for the scalar form (breaks every existing package for no benefit to it). A second plural field (two homes for one fact, forbidden by Principle III, and it was rejected in the spec's clarifications for that reason).

**Check to leave behind.** A table test in `internal/spec` proving: scalar decodes to one region; list decodes in order; a mapping value fails with the file name plus a line and column in the message.

## D2: where the "Pipecat takes one region" rule lives

**Decision.** One new row in `internal/target/table.go`: `FieldDeploymentMultiRegion Field = "deployment_region.multiple"`, built with `field(deny("pipecat", ...), deny("vapi", ...), deny("deepgram", ...))` so LiveKit is `Core` and the other three are `Gated`. `ir.Validate` applies it through the existing `applyCapability` **only when more than one region is declared**, and `livekitEmittedFields` gains the row.

**Rationale.** `internal/target` is the single capability rulebook and validation must read it rather than keep a second description (Principle III). The `deny` note is where Pipecat's own vocabulary goes, which is what Principle II demands of a gated error: agent names are globally unique across regions, so deploy uniquely named agents per region. Denying it on Vapi and Deepgram too is the honest default: neither has a driver, so nothing has proven a list means anything there, and an unproven claim stays gated. Applying it conditionally is what keeps a single-region Pipecat package green.

**Alternatives considered.** A hard-coded check in the Pipecat driver (a second copy of a capability fact, and `validate` would pass while `compile` failed). Silently taking the first region (a silent downgrade, forbidden). Allowing it on Vapi and Deepgram because "they forward everything" (a support claim nothing verifies).

**Check to leave behind.** A validation test that two regions on Pipecat produces a gated error naming the target, and that one region does not.

## D3: how the platform-written config survives a recompile

**Decision.** Generalise the existing `.env` preservation in `writeArtifactFiles` to a small set: `.env` plus anything matching `livekit*.toml`. Same read-before, restore-after shape the function already has, extended from one path to a matched set.

**Rationale.** The precedent is already in the file and for the same reason: a file that an external tool wrote into a disposable directory, whose loss costs the user real state. Here the cost is sharper than `.env`'s, because losing it makes `lk agent deploy` fail and makes `lk agent create` register a **second billable agent** with dispatch split between versions. The glob covers both the default name and the platform's own per-region naming (`livekit.us-east.toml`). It is safe precisely because FR-008 forbids the emitter from ever producing a file matching it.

**Alternatives considered.** Docs-only guidance ("run `lk agent config --id` after every compile"): rejected in the spec's clarifications because the foot-gun stays armed. Warning on delete without preserving: fires on every recompile of a deployed package and trains people to ignore it. Recording the emitted region alongside the file so we can warn only on change: adds state to a directory the constitution calls disposable.

**Check to leave behind.** An L2 test in `internal/cli` that writes a fake `livekit.toml` into a build directory, recompiles, and asserts the bytes are unchanged.

## D4: the Pipecat `image` key is the Pipecat bug

**Decision.** Stop emitting `image` unless the package is asking for a pre-built registry image, which nothing in the authoring surface does today, so in practice: stop emitting it.

**Rationale.** The key is documented as "Required when not using cloud builds", and specifying it makes the platform deploy that image instead of building the Dockerfile. The emitted value, `<project>:latest`, is not a resolvable image URL, so the documented one-command deploy cannot work. Removing the line is what makes the emitted Dockerfile the thing that gets built, which is what every other part of this repository already assumes.

**Alternatives considered.** Keeping `image` and documenting a local `docker build` plus a push to a registry: that is a different product decision, it needs a registry the user may not have, and no repository document currently asks for it. Emitting `image` only when some future authoring field asks for a registry image: correct, and it is exactly what the requirement leaves room for, but nothing declares it today so there is nothing to lower.

**Check to leave behind.** A test asserting the emitted manifest has no `image` key, plus the golden diff.

## D5: the Pipecat secret set is named in the manifest

**Decision.** When the package declares secrets, emit `secret_set = "<project>-secrets"` and have the README order the set's creation before the deploy.

**Rationale.** Chosen in the spec's clarifications: a deploy that skipped the secrets step then fails at deploy time in the platform's words, instead of starting an agent that only fails on a live call. The name follows the platform's own `my-agent-secrets` example. Secret-set names are globally unique and region-scoped, which is why the second-region instructions must name a second set rather than reuse the first.

**Alternatives considered.** `--secrets <name>` on the command only (the manifest stops describing where its environment comes from, and forgetting the flag is silent).

## D6 and D7: LiveKit emits no config file, and names one only when it must

**Decision.** Emit no `livekit.toml`. When one region (scalar or a one-element list) is declared, the README's commands use the default file name, so nothing about the single-region flow changes shape. When several are declared, each command names `livekit.<region>.toml`, which is the platform's own convention.

**Rationale.** Both values in the file are platform-assigned, so emitting it is guessing; and `create` refuses when a file of that name exists, so emitting it actively blocks the fix. Keeping the default name for the common case avoids teaching every single-region user a file name they never need to type.

**Alternatives considered.** Emitting a `livekit.toml.example` (the CLI does not read it, so it is decoration that invites a copy into the real name). Emitting the file with a placeholder subdomain (still refuses, and now with a confusing value in it). Always using per-region names (churn for the 99% case).

## D8: the Dockerfile gets a non-root user with somewhere to write, and nothing else

**Decision.** Add an unprivileged user, give it ownership of the working directory (or a writable cache directory), and switch to it after the dependency install; widen `.dockerignore` from `.env` to also exclude `.env.*`, `.venv/`, and `__pycache__/`. Do not adopt the platform's two-stage template.

**Rationale.** Non-root is a documented requirement, so leaving it is a known deviation. Ownership is not separable from it: `COPY . .` runs as root today, so `/app` is root-owned, and a user whose home is `/app` has nowhere to write when the agent fetches model files on first run. The platform's own published template does `COPY --from=build --chown=appuser:appuser` for that exact reason. This also matters locally, not only on deploy: `unmute dev` in the browser runs this same image through the generated Compose file, which mounts nothing and overrides no user, so a wrong ownership decision breaks local development too. `internal/cli/dev_web_smoke_test.go` (build tag `smoke`) is the check. The ignore list is what keeps the uploaded build context under the 1 GB cap after somebody has run the project locally in that directory. The two-stage rewrite (uv base image, `uv sync --locked`, separate build stage) buys cold-start and image size, which nothing in this feature is blocked on, and it would need a lockfile the generated project does not have.

**Alternatives considered.** Full template parity with the platform's published Dockerfile: more diff, more to keep in sync with an upstream that changes, no observed failure fixed. Leaving the Dockerfile alone: leaves a documented requirement unmet for the sake of four lines.

## D11: the Pipecat manifest stops declaring a replica count

**Decision.** Drop the hardcoded `[scaling] min_agents = 1` from the emitted manifest. The platform default (nothing kept warm) applies, and the README names `--min-agents` as the operator's knob.

**Rationale.** The constitution is explicit that machine sizes and replica counts are derived from declared `capacity` and printed in the report, never written as literals, and `1` is a literal nobody declared. It is not free either: it holds a warm instance and bills for it, which is a surprising default to inherit from a compiler. Removing it is the same discipline the region field already follows, where an unset value invents no default. Deriving a real `min_agents` from `capacity` is a separate feature, not this one.

**Alternatives considered.** Leaving it and documenting why (keeps a cost the author never asked for). Deriving it from `ir.Sizing` now (correct eventually, but it needs a benchmarked coefficient, and the constitution says an underived number stays marked `unbenchmarked` rather than being guessed into a manifest).

**Check to leave behind.** An assertion that the emitted manifest carries no `min_agents`, plus the golden diff.

## D9: the TUI keeps a list without becoming a list editor

**Decision.** The target form's region prompt stays one text field, holding regions separated by commas, split and trimmed on save and joined for display. `scaffold.Data` carries `[]string`, and the `targets.yaml` template writes a bare scalar for one region and a list for several.

**Rationale.** `internal/tui/maintain.go:133` rebuilds `targets.yaml` from `scaffold.Data`, so a field that cannot hold a list would silently drop regions from a multi-region package the moment someone opened the maintain flow to edit something unrelated. Silent loss of a user's declaration is the worst outcome available here, and a comma-separated field prevents it in about six lines. Rendering a scalar for the single case keeps the file the author wrote.

**Alternatives considered.** A repeatable list widget (the right answer at four or more regions, noted as the upgrade path in the plan's Complexity Tracking). Showing only the first region (silent data loss).

## D10: the resolved IR holds a list, and says so

**Decision.** `ir.Target.DeploymentRegion string` becomes `DeploymentRegions []string` with the JSON key `deployment_regions`. Five call sites move with it.

**Rationale.** The IR is the *resolved* form, so the one-or-many ambiguity should not survive into it: every consumer then handles exactly one shape. A singular name holding a list is the kind of small lie that costs someone an afternoon later. The debug schema is derived, so it follows the rename for free.

**Alternatives considered.** Keeping the singular name with a list type (less diff, worse reading). Keeping both a scalar and a list in the IR (two shapes for consumers to handle, which is the ambiguity the resolve step exists to remove).

## Open question, deliberately not closed

Neither platform documents what happens if an existing agent is redeployed into a different region. LiveKit states region is immutable; Pipecat says nothing either way. Nothing in this feature promises an in-place move on either target, and compile does not try to detect a change, because it is offline and cannot know where a live agent runs. If the Pipecat behaviour is ever established, it belongs in [contracts/deploy-commands.md](./contracts/deploy-commands.md) with its date and source, not in an assumption here.

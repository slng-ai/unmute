# Phase 1 data model

Two things move in this feature: one field grows a second shape, and two artifact file sets change. Nothing else in the IR is touched.

## The region value, stage by stage

| Stage | Shape | Notes |
|---|---|---|
| `targets.yaml` (authoring) | `deployment_region: us-east` **or** `deployment_region: [us-east, eu-central]` | Optional. Field name unchanged, so packages already written keep loading. |
| `internal/spec` | `Target.DeploymentRegion Regions`, where `type Regions []string` | Decodes either shape. Re-serialises a single region as a bare scalar so a TUI round-trip does not rewrite the author's file. |
| `internal/ir` | `Target.DeploymentRegions []string`, JSON key `deployment_regions` | Resolved form: always a list, possibly empty. One shape for every consumer. |
| `internal/generate` (LiveKit) | one deploy view per region | See the per-region view below. A build directory is still one directory. |
| `internal/generate` (Pipecat) | exactly one region, or none | More than one never reaches generate: validation gates it first. |
| Compile report | `deployment_regions: ["us-east", ...]`, omitted when empty | Both drivers. Required because Unmute forwards the value without checking it. |

### The LiveKit per-region view

One row per declared region, built in `livekit_v1_build.go` and consumed by the README template only:

| Field | Value when one region is declared | Value when several are declared |
|---|---|---|
| `Region` | the declared region | that row's region |
| `ConfigFile` | empty, so commands use the platform default name | `livekit.<region>.toml` |

The two flags are built in the template from those fields (` --region` on the first-deploy command only, ` --config` only when `ConfigFile` is set) rather than being pre-rendered in Go: assembling shell fragments in the emitter reads worse than the template does, and the observable contract is the same.

With no region declared there is exactly **one** row, with both fields empty: the commands are identical minus the flag, and the README states separately that the platform prompts for a region. That keeps the template one `range` rather than two branches. A one-element list behaves exactly like the scalar form: default file name, no per-region naming.

## Validation rules

Applied in `ir.Validate`, which runs before any artifact exists.

| Rule | Trigger | Result |
|---|---|---|
| Empty entry | a list containing `""` | error naming the field |
| Duplicate region | the same region twice in one list | error; never silently deduplicated, because two `create` runs against one config file name is a confusing outcome to debug |
| More than one region off LiveKit | `len(regions) > 1` on a `pipecat`, `vapi`, or `deepgram` instance | **gated** error via the capability table, quoting that platform's own reason. Pipecat's is that agent names are globally unique across regions, so a second region needs a differently named agent |
| Unknown region code | never checked | forwarded as declared, per SCHEMA N18. The platform CLI is the validator, and no region list is kept in this repository |

## The capability row

| Field | LiveKit | Pipecat | Vapi | Deepgram |
|---|---|---|---|---|
| `deployment_region.multiple` | `core` | `gated` | `gated` | `gated` |

Read by `ir.Validate` and by the LiveKit emitted-fields map, so the existing emitter-against-table agreement test covers it. Gated off LiveKit because no other driver has proven what a list means there, and an unproven claim stays gated.

## Emitted artifact file sets

### LiveKit (`build/livekit/`)

| File | Before | After |
|---|---|---|
| `livekit.toml` | emitted, with a single `[agent] id` holding the worker's dispatch name | **not emitted**; the platform writes it on the first deploy and `unmute compile` preserves it thereafter |
| `README.md` | Deploy section documents self-hosted only, Cloud called "not the supported path" | Deploy section documents LiveKit Cloud first-class and self-hosted, both labelled, with per-region commands, the secrets step, region immutability, and the two recovery notes |
| `Dockerfile` | runs as root | unprivileged user, switched to after the dependency install |
| `.dockerignore` | `.env` | `.env`, `.env.*`, `.venv/`, `__pycache__/` |
| `compile-report.json` | no region | `deployment_regions` when declared |
| everything else | unchanged | unchanged |

### Pipecat (`build/pipecat/`)

| File | Before | After |
|---|---|---|
| `pcc-deploy.toml` | `agent_name`, `image = "<project>:latest"`, `region` when set, `[scaling] min_agents = 1` | `agent_name`, `region` when set, `secret_set` when the package declares secrets. **No `image`**, because that key switches off the cloud build the documented deploy depends on, and **no `min_agents`**, because it is a replica count the package never declared and it bills for a warm instance |
| `README.md` | no Deploy section at all | Deploy section: create the secret set from the env file, deploy, check status, what a redeploy of the same name does, the default region, and the one extra command for a second region |
| `compile-report.json` | no region | `deployment_regions` when declared (a single-element list on this target) |
| everything else | unchanged | unchanged |

## Preserved across a recompile

`build/<target>/` stays disposable, with these exceptions in `writeArtifactFiles`:

| Pattern | Why | Who writes it |
|---|---|---|
| `.env` | existing behaviour; holds the operator's real values | the operator |
| `livekit*.toml` | losing it breaks redeploy and invites a duplicate billable agent | LiveKit Cloud, on the first deploy, one per region |

Pipecat needs no equivalent: its agent is a mutable manifest keyed by name, with no platform-written local state.

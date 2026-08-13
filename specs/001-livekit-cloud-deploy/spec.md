# Feature Specification: Cloud-deployable builds, region aware, on both shipped targets

**Feature Branch**: `feature/warm-cold-human-transfer`

**Created**: 2026-08-12

**Status**: Draft

**Input**: User description: "Make the generated LiveKit build deployable on LiveKit Cloud end to end. The documented verification path for the human-transfer work is `unmute compile examples/human-transfer` then `lk agent deploy` from `build/livekit/`, and it fails with `project does not match agent subdomain []`. Figure out the real `lk` CLI contract from live docs, fix the emitter, make the docs match. Then: you are not working only with LiveKit but with Pipecat as well, and Unmute should manage all of them, so regional deployment has to work on both. In the yaml I want either a single region or several, as a list. For LiveKit that means several deployments; Pipecat should not accept several regions but you can deploy one by one, so it is slightly different. And it is very important that we guide users on the right way to inject secrets into deployments, for example LiveKit Cloud takes `--secrets-file` with the `.env`. All of that belongs in the docs."

*(The feature directory is named `001-livekit-cloud-deploy` from the first draft, before the scope grew to both targets. The name is left alone so existing references keep working.)*

## Why this exists

Task T8 of the human-transfer work is "verify the transfers live". Both transfers only exist on a real platform: SIP signalling and RTP do not fit a laptop tunnel, so the rigs are the two platforms' own clouds. Both rigs start with a deploy. **Both deploys are broken, in the same way, for the same reason**: nobody ever ran them, so the emitted manifest was never in the shape the platform accepts. Every transfer row in `docs/TRANSFERS.md` is still marked provisional because of it.

- **LiveKit.** The emitted `livekit.toml` has no `[project] subdomain`, which is literally the error the CLI prints; its `id` holds the worker's dispatch name instead of the Cloud-assigned agent ID; and the file existing at all makes `lk agent create` refuse, so the fallback that would have worked is blocked too. The generated README then calls the working command "not the supported path".
- **Pipecat.** The emitted `pcc-deploy.toml` sets `image = "<project>:latest"` unconditionally, and that key's documented effect is to **prevent cloud builds**, so the platform skips the Dockerfile and tries to pull an image tag that exists nowhere. The manifest also names no secret set, so nothing the package declares reaches the agent. The generated Pipecat README has no Deploy section at all.

Region is part of the same gap. `deployment_region` has been a `targets.yaml` field since SCHEMA N18 and reaches both drivers, but on LiveKit the driver only prints it inside the footnote that disclaims LiveKit Cloud, and on Pipecat it lands in a manifest the platform cannot deploy. So a declared region reaches no command anyone runs, on either target. Secrets are the same story: the one thing a deployed voice agent cannot do without is exactly what neither README explains how to inject.

Making the two deploys work, making region reach them, and telling people how to pass secrets are one job.

## Clarifications

### Session 2026-08-12

- Q: After the first LiveKit deploy, the platform writes the project subdomain and assigned agent ID into `build/livekit/`, which the next compile deletes. How should that be handled? → A: Preserve the platform-written config file across a rewrite, exactly as `.env` is preserved today. Chosen over docs-only guidance because the failure it prevents is a silently duplicated billable agent.
- Q: Should `deployment_region` become required for a LiveKit target, since an unset region makes the platform prompt interactively? → A: Stay optional. The generated README states that omitting a region means the CLI prompts for one, and `examples/human-transfer` declares a region so the T8 rig is non-interactive.
- Q: A LiveKit region cannot be changed after the agent is created. What happens when someone edits the region and redeploys? → A: State it in the docs, with the platform's own move procedure. No compile-time detection, because compile is offline and cannot know where the live agent runs.
- Q: Should `deployment_region` accept a list, so one instance covers several regions? → A: **Yes.** One region or several, in one field. This reverses an earlier answer in this session that had kept the field scalar and pushed multi-region onto multiple target instances; that earlier reasoning is superseded and the text it produced has been replaced.
- Q: Which name and shape should the list land on, given `deployment_region` is a locked v1 scalar and the request wrote it as `region`? → A: Keep the name `deployment_region` and widen the type, so `deployment_region: us-east` stays valid and `deployment_region: [us-east, eu-central]` becomes valid. No existing package breaks, the fact keeps one home, and the change lands as the next dated `docs/SCHEMA.md` amendment (N32).
- Q: A Pipecat agent name is globally unique across regions, so how does one package reach two Pipecat regions if a list of several regions is refused there? → A: A Pipecat instance takes exactly one region and a list of several fails validation quoting Pipecat's own rule. The generated README shows the second region as one extra command, `pipecat cloud deploy <name>-<region> --region <region>`, using the agent-name argument that overrides the manifest, plus its own region-scoped secret set.
- Q: Should the generated Pipecat manifest name its secret set, or should the set live only in the deploy command? → A: Name it in the manifest when the package declares secrets, with the README ordering the secret-set creation before the deploy. A deploy that skips the secrets step then fails at deploy time in the platform's words, rather than producing an agent that only fails once it is on a call.

## Verified platform contract *(source of truth for this feature)*

Verified 2026-08-12 against each platform's live documentation. Every row carries its source, per Constitution principle IV. Where the two platforms differ, they differ on purpose and the difference is the thing to document, not to smooth over.

### LiveKit Cloud

| Fact | Source |
|---|---|
| The deployment config file holds **two** values, both assigned by the platform: the project subdomain and the Cloud agent ID (`CA_...`). Shape: `[project] subdomain = "..."` and `[agent] id = "..."`. | [Deployment management](https://docs.livekit.io/deploy/agents/managing-deployments/) |
| **`lk agent create` is the first deploy.** It registers the agent, writes the config file itself, and **refuses to run when a config file of that name already exists**. Options include `--region`, `--secrets`, `--secrets-file`, `--secret-mount`, `--config`, `--silent`. | [Agent commands](https://docs.livekit.io/reference/developer-tools/livekit-cli/agent/) |
| **`lk agent deploy` is a redeploy.** It ships a new version of an agent that already exists, and requires a config file **and** a Dockerfile in the working directory. It has no `--region`. | [Agent commands](https://docs.livekit.io/reference/developer-tools/livekit-cli/agent/) |
| **`lk agent config --id <AGENT_ID>`** regenerates the config file for an agent that already exists. This is the recovery path when the file is lost. | [Agent commands](https://docs.livekit.io/reference/developer-tools/livekit-cli/agent/) |
| **Region is chosen at the first deploy and is immutable.** Each deployment is isolated to one region. Passing no `--region` to `create` makes the CLI **prompt the operator** to select one. | [Regions: agent deployment](https://docs.livekit.io/deploy/admin/regions/agent-deployment/), [Agent commands](https://docs.livekit.io/reference/developer-tools/livekit-cli/agent/) |
| **Multi-region is `create` once per region**, each with its own config file, the platform's own example being `livekit.us-east.toml` and `livekit.eu-central.toml`. Moving a region is the same thing plus deleting the old agent. The **same dispatch name in every region is fine**: callers reach the nearest deployment by default, and pinning callers to a region is what needs a name per region. | [Regions: agent deployment](https://docs.livekit.io/deploy/admin/regions/agent-deployment/) |
| **Secrets go in at create time** as `--secrets-file <file>` or repeated `--secrets KEY=VALUE`, and change later with `lk agent update-secrets` (`--overwrite` to replace rather than merge). With no argument the CLI offers to load `.env`, `.env.local`, or `.env.production`. Values are injected as environment variables. | [Secrets management](https://docs.livekit.io/deploy/agents/secrets/) |
| **`LIVEKIT_URL`, `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET` are injected by the platform and cannot be set as secrets.** The CLI copies every value from a secrets file **except** those three. A Dockerfile must not set them either. | [Secrets management](https://docs.livekit.io/deploy/agents/secrets/), [Builds and Dockerfiles](https://docs.livekit.io/deploy/agents/builds/) |
| Container requirements: glibc `-slim` base (no Alpine), explicit `WORKDIR`, **non-root user**, fixed `CMD`/`ENTRYPOINT` launching the worker's `start` command with no wrapper, no `.env*` copied in. Build context excludes `.env.*` plus `.dockerignore`/`.gitignore` matches; 1 GB and 10 minute limits apply. | [Builds and Dockerfiles](https://docs.livekit.io/deploy/agents/builds/) |
| A deployed agent appears in the project's Agents dashboard and the Agent Console launches from it. Full Console function needs the Python Agents SDK at 1.5.2 or later. | [Agent Console](https://docs.livekit.io/agents/start/console/) |

### Pipecat Cloud

| Fact | Source |
|---|---|
| **The CLI is `pipecat cloud ...`**. Everything in this repository that says `pcc deploy`, `pcc secrets set`, `pcc agent delete`, or `pcc auth login` is out of date. | [Deployments](https://docs.pipecat.ai/pipecat-cloud/fundamentals/deploy) |
| **One command does both first deploy and redeploy.** `pipecat cloud deploy` builds the image in the cloud from the project's Dockerfile and deploys it. An agent is a **mutable manifest**: deploying the same name again updates it, so there is no create-versus-update split and no local state to preserve. Running sessions finish on the old image. | [Deployments](https://docs.pipecat.ai/pipecat-cloud/fundamentals/deploy) |
| **Setting `image` prevents cloud builds.** The key is documented as "Required when not using cloud builds", and specifying it makes the platform deploy that image instead of building the Dockerfile. It must be a valid Docker image URL. | [CLI deploy reference](https://docs.pipecat.ai/api-reference/cli/cloud/deploy) |
| `pcc-deploy.toml` keys include `agent_name` (required), `region`, `secret_set`, `image`, `image_credentials`, `agent_profile`, `max_session_duration`, and `[scaling]`/`[build]` tables. CLI flags override the file. | [CLI deploy reference](https://docs.pipecat.ai/api-reference/cli/cloud/deploy) |
| **Region is `--region` on deploy, or `region` in the manifest.** With neither, the agent goes to the organisation's default region (`us-west` unless changed). `pipecat cloud regions list` prints the current set. | [Deployments](https://docs.pipecat.ai/pipecat-cloud/fundamentals/deploy), [Regions](https://docs.pipecat.ai/pipecat-cloud/guides/regions) |
| **Agent names are globally unique across regions.** Multi-region means deploying uniquely named agents per region, the platform's own example being `my-agent-us-west` and `my-agent-us-east`. The deploy command takes the agent name as an argument, which overrides the manifest. | [Deployments](https://docs.pipecat.ai/pipecat-cloud/fundamentals/deploy), [CLI deploy reference](https://docs.pipecat.ai/api-reference/cli/cloud/deploy) |
| **Secrets are a named set, created first and referenced after**: `pipecat cloud secrets set <set-name> --file .env`, then either `secret_set` in the manifest or `--secrets <set-name>` on deploy. The platform injects them as environment variables. A referenced set must already exist and be ready. | [Secrets](https://docs.pipecat.ai/pipecat-cloud/fundamentals/secrets), [CLI deploy reference](https://docs.pipecat.ai/api-reference/cli/cloud/deploy) |
| **Secret sets are region-scoped and their names are globally unique.** A secret set must be in the same region as the agent that uses it, so a second region needs its own set under a different name. | [Regions](https://docs.pipecat.ai/pipecat-cloud/guides/regions) |

### Notes on the sources themselves

- LiveKit's command reference does not list `--config` among `deploy`'s options while its regions page shows `lk agent deploy --config <file>`. Treat the config file name as settable on both, and prefer the default name where one region is involved so the discrepancy never matters.
- Neither platform documents what happens if you redeploy an existing agent into a different region. LiveKit says outright that region is immutable; Pipecat says nothing either way. Nothing in this feature may promise an in-place region move on either target, and the Pipecat side stays an open question rather than an assumption.
- Valid region codes live with the platforms and change without notice. Per SCHEMA N18 a declared value is forwarded as written and never validated by Unmute; the CLIs are the validators. No repository document may print a list of codes as though it were fixed.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A compiled LiveKit package deploys to LiveKit Cloud (Priority: P1)

Someone with a LiveKit Cloud project linked in `lk` compiles a package, reads the Deploy section of the generated README, runs the commands it prints, and ends up with their agent running. They edit no generated file and invent no command.

**Why this priority**: T8's warm and cold LiveKit rows both need it, and nothing downstream of the deploy has ever been run.

**Independent Test**: Compile `examples/human-transfer`, follow only the generated README, confirm the agent appears in the Agents dashboard with a healthy status.

**Acceptance Scenarios**:

1. **Given** a clean checkout and a LiveKit Cloud project set as default in `lk`, **When** the user compiles and runs the first-deploy command from the generated README in `build/livekit/`, **Then** the agent is registered, built, and deployed, and the config file naming the project and assigned agent ID now exists in the build directory.
2. **Given** a freshly compiled `build/livekit/`, **When** the user inspects it, **Then** it contains no deployment config file, because both values in one belong to the platform and cannot be known at compile time.
3. **Given** the generated README, **When** a reader looks for how to deploy, **Then** LiveKit Cloud and self-hosted workers are both documented and labelled, and neither is called unsupported.

---

### User Story 2 - A compiled Pipecat package deploys to Pipecat Cloud (Priority: P1)

The same, on the other platform: compile, read the generated README, run what it prints, get a `ready` agent.

**Why this priority**: T8's cold-on-Daily row needs it, and the Pipecat artifact is broken independently of LiveKit's. It also has no Deploy section at all today, so there is nothing to follow even once the manifest is right.

**Independent Test**: Compile `examples/human-transfer-daily`, follow only the generated README, confirm `pipecat cloud agent status` reports `ready`.

**Acceptance Scenarios**:

1. **Given** a freshly compiled `build/pipecat/`, **When** the user runs the deploy command from the generated README, **Then** the platform builds the image in the cloud from the emitted Dockerfile and the agent reaches `ready`.
2. **Given** a package that does not ask for a pre-built image from a registry, **When** the emitted manifest is read, **Then** it does not name an image, because naming one turns cloud builds off.
3. **Given** the generated Pipecat README, **When** a reader looks for how to deploy, **Then** there is a Deploy section, it uses the current CLI name, and it says that deploying the same agent name again updates the existing agent rather than making a second one.

---

### User Story 3 - Secrets reach the deployed agent, values never reach the artifact (Priority: P2)

Both READMEs say exactly how to get the package's declared environment into the deployed agent, in that platform's own way. No secret value is ever written into a generated file.

**Why this priority**: The warm transfer cannot dial anybody without `SUPERVISOR_PHONE_NUMBER` and the outbound trunk ID. A deploy with no secrets looks healthy and fails on the call, which is the exact failure Principle II exists to prevent. It is also the thing the request called out as very important.

**Independent Test**: Deploy each example with the documented secret step, then list the deployed secrets and compare against the package's required-env list.

**Acceptance Scenarios**:

1. **Given** a compiled LiveKit package, **When** the user follows its README, **Then** the secrets step passes the generated env file to the first-deploy command, and every name the package requires (minus the three the platform injects) exists on the deployed agent.
2. **Given** a compiled Pipecat package that declares secrets, **When** the user follows its README, **Then** they create the named secret set from the env file first and deploy second, and the manifest already names that set.
3. **Given** a compiled LiveKit package, **When** the README's secrets step is read, **Then** it says the three LiveKit connection values are supplied by the platform and must not be sent, so their absence is not read as a mistake.
4. **Given** a deployed agent on either platform, **When** the user needs to change one secret later, **Then** the README names the command that updates secrets without a full redeploy, and says whether it merges or replaces.
5. **Given** any generated file in either build directory, **When** it is inspected, **Then** it contains environment variable names only, no values, and no platform-assigned project, agent, or secret-set identity beyond the name the docs told the user to create.

---

### User Story 4 - The declared region is where the agent lands, one region or several (Priority: P2)

`deployment_region` takes one region or a list of them. On LiveKit a list fans out to one deployment per region. On Pipecat a list of more than one is refused, with the platform's reason, and the README shows the one extra command per extra region instead.

**Why this priority**: Region decides where the compute runs and therefore the latency every caller hears, and on LiveKit it cannot be changed afterwards. A region that is declared but not passed is worse than one never declared: the deploy succeeds in the wrong place and looks fine.

**Independent Test**: Declare one region, compile, deploy, confirm the agent's region matches with no prompt. Then declare two on a LiveKit instance and confirm the README carries one first-deploy command per region with per-region config file names.

**Acceptance Scenarios**:

1. **Given** a LiveKit instance declaring one region, **When** the README is read, **Then** the region flag carries that value on the first-deploy command and appears on no other command.
2. **Given** a LiveKit instance declaring several regions, **When** the README is read, **Then** there is one first-deploy command per region, each naming its own config file, and one redeploy command per region naming the same file, and the dispatch name is the same in all of them.
3. **Given** a Pipecat instance declaring one region, **When** the emitted manifest is read, **Then** it carries that region.
4. **Given** a Pipecat instance declaring more than one region, **When** the package is validated, **Then** it fails by name, quoting Pipecat's rule that agent names are globally unique across regions, and no artifact is written.
5. **Given** a Pipecat package already deployed in one region, **When** the README is read, **Then** it shows the single extra command that puts a differently named agent in a second region, and says that second region needs its own secret set.
6. **Given** an instance with no region at all, **When** the README is read, **Then** it says what the platform does by default: LiveKit prompts for one, Pipecat uses the organisation's default region.
7. **Given** an agent that already exists, **When** the region is changed and it is redeployed, **Then** the docs already said region is fixed at the first deploy, and named LiveKit's move procedure, so nothing is promised that the platforms do not do.
8. **Given** any compiled package that declares a region, **When** its compile report is read, **Then** every declared region is in it, because Unmute forwards the value without checking it.

---

### User Story 5 - Redeploy after a change, without duplicating or stranding (Priority: P2)

The user changes the source package, recompiles, and ships a new version to the same agent on either platform. They do not accidentally register a second one.

**Why this priority**: This is the loop live verification actually runs in. On LiveKit, getting it wrong makes a second billable agent and splits dispatch so half the calls reach the wrong version. On Pipecat the platform's own model prevents it, and the docs should say so rather than leaving people to guess.

**Independent Test**: Deploy, recompile, deploy again on both platforms, then list agents on each and confirm the count did not grow.

**Acceptance Scenarios**:

1. **Given** a LiveKit build directory holding a config file the platform wrote, **When** the user recompiles, **Then** the file is still there afterwards exactly as written, and the redeploy command works with no extra step. This holds for every per-region config file when several regions are declared.
2. **Given** a LiveKit build directory that was deleted outright, **When** the user recompiles and reads the docs, **Then** they are told the one command that rebuilds the config file from the existing agent's ID, so they redeploy instead of registering a duplicate.
3. **Given** a LiveKit config file written for a different project than the one now default in `lk`, **When** a deploy is attempted, **Then** the docs name that mismatch as a known cause and say how to resolve it, because the platform's error does not say which side is wrong.
4. **Given** a Pipecat agent already deployed, **When** the user recompiles and deploys again with the same name, **Then** the same agent is updated, and the README said so, so nobody invents a second name to be safe.

---

### User Story 6 - The rig walkthroughs read as runnable sequences (Priority: P3)

`docs/TRANSFERS.md` and both example READMEs give steps that work, in order, with the same commands the generated READMEs print.

**Why this priority**: The documents already promise rigs; the defect is that they promise steps that fail, on both platforms, with a CLI name that no longer exists. Fixing the emitters without fixing them leaves the contradiction that started this feature. Lower only because it delivers nothing on its own.

**Independent Test**: Follow `docs/TRANSFERS.md` section 4, both rigs, from step 1, and confirm every command runs.

**Acceptance Scenarios**:

1. **Given** either rig walkthrough, **When** a reader works top to bottom, **Then** account and CLI prerequisites appear before the first command that needs them, and no step fails.
2. **Given** every place that describes deploying (both generated READMEs, `docs/TRANSFERS.md`, both example READMEs), **When** they are compared, **Then** they name the same commands in the same order for each platform, and nothing calls a working command unsupported.
3. **Given** any repository document that names a platform CLI, **When** it is read, **Then** the CLI name is the current one.

### Edge Cases

- **No default project linked in `lk`.** Deploying with none produces the same confusing subdomain error. The prerequisite must appear before the deploy step.
- **LiveKit build directory wiped by `git clean` or by hand.** The Cloud-assigned identity goes with it. Recovery is regenerating the config file from the agent ID, and the docs must name it; guessing leads to a duplicate agent.
- **A region the platform does not recognise.** Forwarded as declared and rejected by the CLI in its own words, per SCHEMA N18. Unmute keeps no list of region codes to check against.
- **Region changed on an agent that already exists.** LiveKit will not move it, and Pipecat does not say. Documented on the LiveKit side, left unpromised on the Pipecat side, and detected on neither, because compile is offline.
- **A LiveKit list with one region.** Behaves exactly like the scalar form, one deployment, one config file with the default name. A one-element list must not produce per-region file names that the single-region docs do not mention.
- **The same region twice in one list.** Duplicates are an authoring mistake with a confusing outcome (two creates against one file name), so validation rejects them rather than deduplicating silently.
- **Several LiveKit regions sharing one dispatch name.** Callers reach the nearest deployment, which is the platform's default and usually what is wanted. Pinning particular callers to a region needs a name per region and explicit dispatch, which this feature does not add.
- **Pipecat second region without its own secret set.** Secret sets are region-scoped, so reusing the first region's set cannot work. The docs must say a second region needs its own.
- **Agent shows as `Sleeping`** on LiveKit plans that scale to zero. Normal, not a failed deploy, and a reader verifying "it shows up ready" needs to know.
- **Build fails rather than the deploy.** Both platforms build in the cloud from the emitted Dockerfile, and LiveKit caps the context at 1 GB and the build at 10 minutes. A build directory holding a `.venv` or caches from a local run inflates that context; the emitted ignore list is what keeps it small.
- **A package that declares no secrets at all.** Both deploys must still work, with no secret set and no secrets flag. The secrets step is skippable, not mandatory.

## Requirements *(mandatory)*

### Both targets

- **FR-001**: A freshly compiled package MUST contain everything its platform's deploy command requires and nothing that makes that command refuse or misbehave. Whatever the platform assigns MUST NOT be pre-emptively emitted, and whatever the platform reads MUST be emitted in the documented shape.
- **FR-002**: No generated file MUST carry a platform-assigned identity: no LiveKit project subdomain or agent ID, and no value a deploy hands back. A build directory MUST stay portable between projects, organisations, and accounts.
- **FR-003**: Each generated README MUST carry a Deploy section written for that platform's real flow: for LiveKit, first deploy then redeploy, labelled; for Pipecat, one deploy command with the fact that redeploying the same name updates the existing agent.
- **FR-004**: Each generated README MUST document how to inject the package's declared environment on that platform, using the env file the package already generates: for LiveKit, the env file passed to the first deploy plus the update-secrets command and whether it merges or replaces; for Pipecat, creating the named secret set from the env file before the deploy, plus how to update it later.
- **FR-005**: The LiveKit README MUST state that `LIVEKIT_URL`, `LIVEKIT_API_KEY`, and `LIVEKIT_API_SECRET` are supplied by the platform and must not be sent as secrets. This is the one part of FR-004 that is called out separately, because their absence from a secret list reads as an omission unless the artifact says otherwise.
- **FR-006**: Every claim about either platform's deploy contract MUST cite the page it came from and carry the 2026-08-12 verification date, in whichever repository document states it. Nothing may promise an in-place region move on Pipecat, which no source confirms.
- **FR-007**: Every repository document and template MUST use each platform's current CLI name. The `pcc ...` form is retired and MUST NOT appear.

### LiveKit specifics

- **FR-008**: The deployment config file, whose two values the platform assigns, MUST NOT be emitted at compile time.
- **FR-009**: `unmute compile` MUST preserve a platform-written deployment config file found in the build directory, the same way it already preserves `.env`, so a redeploy after a recompile needs no extra step. This MUST cover every per-region config file when several regions are declared. The documents MUST also name the command that rebuilds such a file from an agent ID, for a build directory that was deleted outright.
- **FR-010**: The generated README MUST NOT describe LiveKit Cloud deployment as unsupported. It MUST document both LiveKit Cloud and self-hosted workers, labelling each.
- **FR-011**: The generated container recipe MUST satisfy the platform's documented build requirements: glibc slim base, explicit working directory, non-root run user, fixed start command with no wrapper, no LiveKit connection variables set, no environment files copied in. The emitted ignore list MUST keep the upload context free of local run leftovers.

### Pipecat specifics

- **FR-012**: The emitted manifest MUST NOT name an image unless the package is asking for a pre-built one from a registry, because naming an image turns off the cloud build that the documented deploy depends on.
- **FR-013**: When the package needs any environment at all, the emitted manifest MUST name the secret set the README tells the user to create, so the manifest is self-describing and a deploy that skipped the secrets step fails at deploy time rather than on a call. *(Corrected 2026-08-12 during implementation: this first read "when the package declares secrets", meaning the `secrets:` block. Most packages declare no such block yet still require provider keys, and gating on it would deploy those with no environment at all, which looks healthy and fails on the first call. The gate is the generated required-env list, which is what `.env.example` already holds. A package requiring no environment still deploys with no set.)*
- **FR-014**: The generated README MUST show how to reach a second region: one deploy command with a differently named agent and its own region, plus that region's own secret set. It MUST also say what to do when a secret-set name is already taken, since those names are globally unique and the deploy command can override the manifest.
- **FR-027**: The emitted manifest MUST NOT declare a replica count the package never declared. The hardcoded `[scaling] min_agents = 1` goes: machine sizes and replica counts are derived from declared `capacity` and printed in the report, never written as a literal, and this one also bills for a warm instance the platform would not keep by default. A warm pool stays available to the operator as a deploy-time flag, named in the README. *(Numbered out of sequence on purpose: added by the 2026-08-12 analysis pass, so existing references stay stable.)*

### Region

- **FR-015**: `deployment_region` MUST accept either one region or a list of regions. The existing scalar form MUST stay valid, and the field MUST keep its name, so no package already written fails to load. The widened type MUST land as a new numbered, dated `docs/SCHEMA.md` amendment (N32) that states the authoring shape changed and that existing packages still decode, with the derived authoring schema regenerated from the Go structs rather than hand-edited.
- **FR-016**: On a LiveKit instance, each declared region MUST produce its own first-deploy command carrying that region and its own config file name, and its own redeploy command naming the same file. One region, whether written as a scalar or a one-element list, MUST use the default config file name and MUST NOT introduce per-region naming.
- **FR-017**: On a Pipecat instance, more than one declared region MUST fail validation by name before any artifact exists, quoting Pipecat's rule that agent names are globally unique across regions. Exactly one region MUST be written into the manifest.
- **FR-018**: Where an instance declares no region, each README MUST state what that platform does: LiveKit prompts the operator to choose, Pipecat uses the organisation's default region. `deployment_region` stays optional on both.
- **FR-019**: The LiveKit README MUST state that region is fixed at the first deploy and name the platform's procedure for moving one. Unmute MUST NOT try to detect a changed region at compile time and MUST NOT keep its own list of valid region codes.
- **FR-020**: Every declared region MUST appear in the compile report, because it is a value Unmute forwards without checking and every such value MUST be inspectable there. It appears in neither driver's report today, so both gain it.
- **FR-021**: A list that repeats a region MUST fail validation rather than being silently deduplicated.

### Documentation and discipline

- **FR-022**: `examples/human-transfer` MUST declare a region on its LiveKit instance so the T8 rig runs without an interactive prompt, and the LiveKit example or reference docs MUST show the list form somewhere an author will meet it.
- **FR-023**: `docs/TRANSFERS.md` and both example READMEs MUST match the generated READMEs' commands, and MUST list account and CLI prerequisites before the step that needs them. None MUST become an independent copy of the command list: each MUST point at the generated README as the authority for the commands themselves.
- **FR-024**: `docs/user/reference/targets-yaml.md` MUST document the one-or-many shape of `deployment_region`, what an unset region means on each platform, and that a multi-region list is LiveKit only.
- **FR-025**: Transfer behaviour MUST NOT change: not which transfers compile on which route, not the authoring shape. Packages with no transfers MUST keep working; these fixes are emitter-wide and their artifacts change shape with everything else. *(Amended 2026-08-12, after the first live deploy: this originally also forbade any change to the emitted transfer code. Two defects were found by running the artifact rather than reading it, and both are fixed here under FR-028 and FR-029, because a transfer that cannot execute is exactly what this feature exists to unblock. Nothing about which transfers compile where, or what an author writes, has changed.)*
- **FR-028**: The emitted cold transfer MUST send its destination as a **URI**. `transfer_to` becomes the `Refer-To` of a SIP REFER: a bare phone number appears in no platform example, the platform's own sample writes `tel:` explicitly, and at least one supported carrier documents the `sip:<number>@<trunk-host>` form as mandatory. A destination already written as a `sip:`/`tel:` URI MUST pass through untouched, since double prefixing breaks it. The warm path's dial argument MUST NOT gain a scheme: it takes a number. *(Added 2026-08-12 from the first live deploy; verified against the call-forwarding guide and the WarmTransferTask reference.)*
- **FR-029**: The emitted environment file MUST separate the names the **operator** supplies from the names something else supplies. A LiveKit Cloud deployment is injected its own connection values, and the managed SIP service owns Redis, which no emitted Python reads on this driver. Listing those beside real provider keys made a deployed agent appear to require infrastructure it can never use, and sent a meaningless secret to the platform. The complete required-env list MUST stay complete in the compile report: what the package requires has not changed, only who supplies each name. *(Added 2026-08-12. The capability table already carried this distinction as the route's locally-supplied set; the driver simply did not read it.)*
- **FR-026**: Anything in the repository that asserts the old artifact shape MUST be updated in the same change: the test asserting the removed LiveKit config file, the pinned emitted-file lists, the compile-report goldens, and the Pipecat manifest goldens. No test may pass by describing an artifact that could not deploy.

### Key artefacts

- **Declared region**: an instance field in `targets.yaml`, one region or several, forwarded as written. Fans out to one deployment per region on LiveKit, capped at one on Pipecat, readable afterwards in the compile report.
- **LiveKit deployment config**: names the project and assigned agent for a build directory, one per region. Written by the platform, preserved by `unmute compile`, never authored and never emitted.
- **Pipecat manifest**: the emitted deployment configuration the platform reads, naming the agent, its region, and its secret set, and deliberately not naming an image so the cloud build runs.
- **Environment template**: the names, no values, that the package requires. Feeds the local `.env`, LiveKit's secrets file, and Pipecat's secret set.
- **Container recipe**: how each platform's build service turns the compiled package into the image it runs.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: On **both** platforms, a person following only the generated README, from a clean checkout with that platform's CLI already authenticated, reaches a running deployed agent with **zero** edits to generated files and **zero** commands not printed in the docs they were given.
- **SC-002**: **Every** step of **both** rig walkthroughs in `docs/TRANSFERS.md` runs as written. Today the LiveKit rig fails at step 3 of 6 and the Pipecat rig at step 2 of 5, which is what blocks everything after them.
- **SC-003**: A package that declares a region deploys to that region on the first try, on both platforms, with **zero** interactive prompts and **zero** flags typed by hand.
- **SC-004**: A LiveKit instance declaring N regions yields N deployments from one build directory, and a Pipecat instance declaring more than one region fails validation with **zero** artifacts written.
- **SC-005**: Compile, deploy, recompile, deploy again leaves **exactly one** agent per declared region on each platform.
- **SC-006**: **All** environment names the compiled package requires, other than the three LiveKit injects itself, are present on the deployed agent after the documented secret step, on both platforms.
- **SC-007**: **Zero** generated files contain a secret value or a platform-assigned identity.
- **SC-008**: **Every** value Unmute forwards without checking, each declared region included, can be read back from the compile report without opening the source package.
- **SC-009**: **Zero** occurrences of a retired CLI name remain in the repository's documents and templates.
- **SC-010**: T8 is unblocked on both rows: the warm transfer can be attempted in the LiveKit Agent Console straight after its deploy with no phone number, and the Daily cold transfer has a deployed bot to call.
- **SC-011**: **Zero** generated files declare a machine size or replica count that the package did not declare.
- **SC-012**: **Zero** generated files ask the operator to supply a value the platform supplies itself, and the compile report still lists **every** name the package requires.
- **SC-013**: A cold transfer's destination reaches the platform as a URI in **every** shape a package can declare it: a bare number, an authored `sip:` URI, and an environment variable resolved on the call.

## Assumptions

- The operator has each platform's current CLI installed and authenticated, and for LiveKit has a default project set. Unmute never authenticates to either platform, never runs their CLIs, and never provisions anything there. The CLIs stay the validators of region codes, secret-set names, and everything else the platforms own.
- Values the platforms assign are theirs: no LiveKit subdomain, agent ID, or Pipecat-side identity becomes an authoring field. The only authoring change in this feature is widening `deployment_region` to accept a list, landing as SCHEMA N32.
- Region correctness is the platforms' to enforce. Unmute forwards the declared strings and a wrong one fails at the CLI before anything is created. Nothing here reads or writes the region of an agent that already exists.
- On LiveKit, all regions of one instance share the package's single dispatch name, which gives the platform's default nearest-region routing. Pinning callers to a region is out of scope.
- On Pipecat, the second region's agent name is the user's to choose at the command line, following the platform's own `<name>-<region>` convention. Unmute does not invent names, and the emitted manifest keeps naming exactly one agent.
- Recompile safety on LiveKit comes from preserving the platform-written config files, mirroring the existing `.env` preservation. Pipecat needs no equivalent because its agent is a mutable manifest keyed by name with no local state.
- Each generated README stays the single home for its platform's deploy commands. Repository documents point at it rather than restating it, so a command cannot drift in one place and not the other.
- These fixes are emitter-wide on both drivers, so every compiled package changes shape and the goldens change with them. That diff gets read before it is committed; it is not a mechanical regeneration.
- Out of scope: pinning particular callers to a particular region, pre-built or private-registry images beyond not breaking the cloud build, Pipecat agent profiles, deriving a replica count from declared `capacity` (removing the hardcoded one is in scope per FR-027; deriving a real one is a separate feature), LiveKit self-hosted guidance beyond keeping it documented and labelled, and running the live transfer tests themselves, which is T8's own work once this unblocks it.
- The examples' pinned Agents SDK series is at or above the version the LiveKit Agent Console needs, so the Console half of the rig works without loosening a pin that exists because the warm transfer task is beta.

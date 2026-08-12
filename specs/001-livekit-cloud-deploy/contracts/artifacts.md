# Contract: what each artifact must contain

The generated project is the deliverable. These are the assertions a test should make about it, target by target. Everything here is checkable offline, with no platform account.

## LiveKit (`build/livekit/`)

**Must not contain**

- Any `livekit*.toml`. Both of its values are platform-assigned, and its presence makes the platform's first-deploy command refuse.
- Any LiveKit Cloud project subdomain or agent ID, in any file.
- Any secret value.

**`README.md` must contain**

| Assertion | Why |
|---|---|
| A Deploy section naming the first-deploy command and, separately, the redeploy command | They are different commands with different effects, and getting them the wrong way round either refuses or duplicates |
| ` --region <declared>` on the first-deploy command, once per declared region, and on no other command | Region is fixed at creation; the redeploy command has no such flag |
| No region flag anywhere when none is declared, plus a line saying the platform will ask | Otherwise its absence reads as an omission |
| `livekit.<region>.toml` in the per-region commands when several regions are declared, and no such file name when one is | The platform's own convention, and the single-region flow should not teach a file name nobody needs |
| The secrets step, passing the generated env file to the first deploy | The one thing a voice agent cannot run without |
| A line stating that `LIVEKIT_URL`, `LIVEKIT_API_KEY`, and `LIVEKIT_API_SECRET` come from the platform and must not be sent | Their absence from the secret list is correct, and looks like a mistake otherwise |
| The command that updates secrets later, and whether it merges or replaces | Changing one key should not mean redeploying blind |
| A line stating region is fixed at the first deploy, and the platform's move procedure | The platform will not move it, and silence would imply it does |
| The recovery command that rebuilds the config file from an agent ID | For a build directory deleted outright |
| A note that a config file from another project produces a subdomain mismatch, and how to resolve it | The platform's error does not say which side is wrong |
| Both LiveKit Cloud and self-hosted, each labelled | Both are real paths |
| No sentence calling either path unsupported | A working command must not be disclaimed |

**`Dockerfile` must contain**: an unprivileged user and a `USER` switch before the start command, with the working directory owned by that user or a writable cache directory given to it, because the agent fetches model files at first run and a root-owned `/app` would leave it nowhere to write; no `LIVEKIT_URL`, `LIVEKIT_API_KEY`, or `LIVEKIT_API_SECRET`; the existing fixed `start` command and explicit `WORKDIR`. The image `unmute dev` runs in the browser is this same image, so a mistake here breaks local development as well as the deploy.

**`.dockerignore` must exclude**: `.env`, `.env.*`, `.venv/`, `__pycache__/`.

**`compile-report.json` must contain**: `deployment_regions` listing every declared region, and the key absent when none is declared.

## Pipecat (`build/pipecat/`)

**`pcc-deploy.toml` must contain**

| Assertion | Why |
|---|---|
| No `image` key | The key is documented as switching cloud builds off, and the emitted value is not a resolvable image URL, so the documented deploy cannot work while it is there |
| `region = "<declared>"` when a region is declared, exactly one, and no region line otherwise | Unchanged from V29, except that "exactly one" is now enforced upstream by validation |
| `secret_set = "<project>-secrets"` when the package declares secrets, and no such key otherwise | So the manifest describes where its environment comes from, and a deploy that skipped the secrets step fails at deploy time rather than on a call |
| `agent_name` as today | Unchanged |
| No `min_agents`, and no `[scaling]` table if that leaves it empty | A replica count the package never declared is neither derived nor free: it bills for a warm instance the platform would not keep by default. A warm pool stays available as a deploy-time flag |

**`README.md` must contain**

| Assertion | Why |
|---|---|
| A Deploy section at all | It has none today |
| The current CLI name (`pipecat cloud ...`) | The `pcc` form is retired |
| The secret-set command before the deploy command, reading the generated env file | The platform requires the set to exist and be ready before a deploy can reference it |
| `--region <declared>` on the secret-set command whenever a region is declared | A secret set is region-scoped and defaults to `us-west`; a set in the wrong region cannot be used by the agent |
| When no region is declared: that the agent goes to the organisation's default region while a secret set defaults to `us-west`, so both need a region if that default was changed | Verified asymmetry, and a silent mismatch otherwise |
| A line stating that deploying the same agent name again updates the existing agent | So nobody invents a second name to be safe |
| The one extra command pair for a second region: its own secret set in that region, and a deploy under a differently named agent | Agent names are globally unique across regions |
| The status command | "Deployed" is not the same as `ready` |
| What to do when the secret-set name is already taken: those names are globally unique, and `--secrets <other-name>` on the deploy command overrides the manifest | Otherwise a name collision is a dead end with no documented escape |
| That a warm instance pool is opt-in with `--min-agents`, because the package declares no replica count | The manifest no longer keeps one warm, so the operator should know the knob exists |

**`compile-report.json` must contain**: `deployment_regions` with the single declared region, absent when none is declared.

## Both

- No emitted file contains a secret value, on either target.
- A package with no transfers, no region, and no secrets still compiles and still deploys. These fixes are emitter-wide, so such a package's artifact changes shape; what must not change is whether it works.
- Transfer output is byte-identical apart from the file-set and README changes listed above.

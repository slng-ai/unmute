# Contract: the command sequences a generated README must print

Verified 2026-08-12 against each platform's live documentation. `<project>` is the package's project name, `<region>` a declared region. These are the sequences the emitter must produce; the repository's own documents point here rather than restating them.

## LiveKit Cloud

Prerequisites the README must state before the first command: an authenticated CLI (`lk cloud auth`) and a default project (`lk project list`, `lk project set-default "<name>"`). Without the default, the deploy fails with the same subdomain error that started this feature.

**One region declared** (`deployment_region: us-east`), run in `build/livekit/`:

```sh
cp .env.example .env            # then fill in the values
lk agent create --region us-east --secrets-file .env
lk agent deploy                 # every later version
```

**No region declared**: identical, without `--region`. The CLI asks which region to use, so an unattended run needs the field set.

**Several regions declared** (`[us-east, eu-central]`):

```sh
lk agent create --region us-east    --config livekit.us-east.toml    --secrets-file .env
lk agent create --region eu-central --config livekit.eu-central.toml --secrets-file .env

lk agent deploy --config livekit.us-east.toml
lk agent deploy --config livekit.eu-central.toml
```

Callers reach the nearest deployment by default. All regions keep the package's single dispatch name; pinning callers to a region is out of scope for this feature.

**Also printed**

```sh
lk agent update-secrets --secrets-file .env              # merges into existing keys
lk agent update-secrets --secrets-file .env --overwrite  # replaces the whole set
lk agent config --id CA_xxx        # rebuild a lost config file, then deploy again
lk agent status                    # Sleeping is normal on plans that scale to zero
lk agent logs
```

**Facts the README must state, not just imply**

- `LIVEKIT_URL`, `LIVEKIT_API_KEY`, and `LIVEKIT_API_SECRET` are injected by the platform. The CLI drops them from a secrets file, and they must not be set in the image.
- Region is fixed at the first deploy. Moving one means creating in the new region and deleting the old agent.
- A config file belonging to another project produces the subdomain mismatch error; check the default project.

## Pipecat Cloud

Prerequisites: an authenticated `pipecat cloud` CLI. Run in `build/pipecat/`.

**One region declared** (`deployment_region: eu-west`):

```sh
cp .env.example .env            # then fill in the values
pipecat cloud secrets set <project>-secrets --file .env --region eu-west
pipecat cloud deploy
pipecat cloud agent status <project>
```

The `--region` on the secret set is not optional decoration: a set is region-scoped, defaults to `us-west`, and an agent can only use a set in its own region.

**No region declared**:

```sh
pipecat cloud secrets set <project>-secrets --file .env
pipecat cloud deploy
```

The README must say what happens here: the agent goes to the organisation's default region, while a secret set defaults to `us-west`. If the organisation default was changed, both commands need an explicit `--region`.

**A second region** (the same package, a differently named agent, because agent names are globally unique across regions):

```sh
pipecat cloud secrets set <project>-<region>-secrets --file .env --region <region>
pipecat cloud deploy <project>-<region> --region <region> --secrets <project>-<region>-secrets
```

**Facts the README must state**

- Deploying the same agent name again updates the existing agent. Running sessions finish on the old image.
- A failed deploy leaves the previous ready version serving traffic.
- `pipecat cloud regions list` prints the current region set.

## What the repository documents may do

`docs/TRANSFERS.md`, `examples/human-transfer/README.md`, and `examples/human-transfer-daily/README.md` may name the step ("create the secret set, then deploy, following the generated README") and must not become a fourth copy of these sequences. The generated README is the single home; a copy that drifts is worse than a pointer.

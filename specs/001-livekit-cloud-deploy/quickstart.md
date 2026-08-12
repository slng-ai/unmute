# Validation guide

Two halves. The first proves the artifacts offline with no cloud account and belongs in the suite. The second is the live rig, which is T8's own work and cannot run in CI: real numbers, real per-minute charges, two phones.

## Offline, no accounts needed

```sh
make fmt && make lint && make build && make test
```

Then look at the things a green suite does not prove on its own:

```sh
bin/unmute compile examples/human-transfer
ls examples/human-transfer/build/livekit/          # no livekit.toml
grep -n "region\|secrets-file\|agent create\|agent deploy" \
  examples/human-transfer/build/livekit/README.md
grep -n "deployment_regions" \
  examples/human-transfer/build/livekit/compile-report.json

bin/unmute compile examples/human-transfer-daily
cat examples/human-transfer-daily/build/pipecat/pcc-deploy.toml   # no image key, secret_set present
grep -n "secrets set\|cloud deploy\|--region" \
  examples/human-transfer-daily/build/pipecat/README.md
```

Region shapes, by hand on a scratch copy of a package:

| Edit `targets.yaml` to | Expect |
|---|---|
| `deployment_region: us-east` | compiles; one first-deploy command carrying the region; default config file name |
| `deployment_region: [us-east]` | identical output to the scalar form |
| `deployment_region: [us-east, eu-central]` on the LiveKit instance | two first-deploy commands, two redeploy commands, per-region file names |
| `deployment_region: [us-east, us-east]` | validation error, no artifact |
| `deployment_region: [us-east, eu-central]` on a Pipecat instance | gated error quoting Pipecat's globally-unique-agent-name rule, no artifact |
| `deployment_region: {region: us-east}` | load error with the file name, line, and column |

Recompile safety:

```sh
printf '[project]\n  subdomain = "fake"\n\n[agent]\n  id = "CA_fake"\n' \
  > examples/human-transfer/build/livekit/livekit.toml
bin/unmute compile examples/human-transfer
cat examples/human-transfer/build/livekit/livekit.toml    # byte-identical
```

Goldens: regenerate with each package's own `-update` flag and **read the diff** before committing. Expect exactly the changes in [contracts/artifacts.md](./contracts/artifacts.md): a file removed on the LiveKit side, a key removed and a key added on the Pipecat side, two rewritten README sections, and `deployment_regions` in both reports. Anything else in that diff is a bug in the change, not a golden that needs updating.

Optional, needs `uv`:

```sh
make smoke
```

## On LiveKit Cloud

Prerequisites: an authenticated `lk` CLI with a default project set, and for the transfer half the Twilio trunk from [docs/TRANSFERS.md](../../docs/TRANSFERS.md) section 3.

1. `bin/unmute compile examples/human-transfer`
2. In `build/livekit/`, copy `.env.example` to `.env` and fill it in.
3. Run the first-deploy command exactly as the generated README prints it. Expect: the agent registers, builds, deploys, and a `livekit.toml` naming the project and a `CA_...` agent ID now exists.
4. `lk agent status` reports a healthy state (`Sleeping` is healthy on plans that scale to zero).
5. `lk agent secrets` lists every name from the package's required-env list except the three the platform injects.
6. The agent appears in the project's Agents dashboard, and **Launch Console** opens on it. This is the point T8 has never reached.
7. Recompile, then run the redeploy command. Expect a new version and still exactly one agent in `lk agent list`.
8. Warm transfer, no phone number needed: talk to the agent in the Console and ask for a manager. Expect one spoken line, hold music, the supervisor's real phone ringing, a briefing, then the two legs joined.
9. Cold transfer: call the Twilio number and ask about an invoice. Expect one spoken line, then the billing phone rings and the agent leaves.

If a region was declared, confirm in step 3 that no prompt appeared and that the deployed agent's region matches the declared one.

## On Pipecat Cloud

Prerequisites: an authenticated `pipecat cloud` CLI, and a Daily domain with a number and dial-out enabled.

1. `bin/unmute compile examples/human-transfer-daily`
2. In `build/pipecat/`, copy `.env.example` to `.env` and fill it in.
3. Create the secret set as the generated README prints it, including its `--region` when a region is declared.
4. `pipecat cloud deploy`. Expect a cloud build from the emitted Dockerfile, not an attempt to pull an image.
5. `pipecat cloud agent status <project>` reports `ready`.
6. Connect the Daily number to the deployed agent, then call it and ask about an invoice. Expect the announcement, then the billing phone rings and the bot exits.
7. Recompile and deploy again. Expect the same agent updated, not a second one.

## Teardown

A test rig must not become a standing bill. Release test-only Twilio numbers and trunks, release the Daily number, delete the deployed agents on both platforms, and remove unused LiveKit trunks and dispatch rules.

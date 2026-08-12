# Quickstart: proving the inline dial-out works

**Feature**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md) | **Date**: 2026-08-12

Three layers, cheapest first. Layers 1 and 2 run offline with no credentials and
are what the suite enforces. Layer 3 is the one that actually proves the feature,
because the failure this fixes only appeared on a live call.

**There is no laptop path to a transfer, and that is deliberate.** A transfer uses
the platform's own primitive, and those exist only where telephony runs in the
cloud: LiveKit's on the SIP route, Pipecat's on Daily. The routes that run on a
laptop carry plain audio with no transfer control, and this repository will not
substitute its own machinery there. So layer 3 needs a real deployment, and
layers 1 and 2 have to carry every shape layer 3 will not reach.

Details of what the artifact must contain live in
[contracts/emitted-dial-out.md](./contracts/emitted-dial-out.md) and
[contracts/environment.md](./contracts/environment.md). This file is how to run
the checks, not a second copy of them.

## Prerequisites

| Layer | Needs |
|---|---|
| 1. Offline suite | Go 1.24. Nothing else. |
| 2. Emitted Python | `uv`, for `make smoke`. Opt in, never in the pull request gate. |
| 3. Live transfer | A LiveKit Cloud project, `lk` authenticated with that project set as default, a Twilio Elastic SIP Trunking trunk with Call Transfer (SIP REFER) and PSTN Transfer enabled, and a phone you can answer. **No `lk sip outbound create`.** That is the point. |

## Layer 1: the offline gate

```sh
make fmt
make lint
make build
make test
```

`make test` must pass with zero Python. The checks that matter for this feature:

**The trunk name is gone.** From the repository root, after building:

```sh
./bin/unmute compile examples/human-transfer
grep -rn "LIVEKIT_SIP_OUTBOUND_TRUNK" build/livekit/   # expect no matches
ls build/livekit/sip-outbound-trunk.json               # expect: No such file
```

**The inline config is there, once, called three times.** For a package with an
outbound channel as well as the transfers:

```sh
grep -n "_sip_trunk\|sip_connection\|trunk=\|sip_number\|sip_trunk_id" build/livekit/agent.py
```

Expect one `def _sip_trunk`, one `sip_connection=_sip_trunk()`, two
`trunk=_sip_trunk()`, three `sip_number=`, and **zero** `sip_trunk_id`.

**Inbound is untouched.** The two inbound files must be byte for byte what they
were before this feature:

```sh
git stash && ./bin/unmute compile examples/human-transfer \
  && cp build/livekit/sip-*.json /tmp/before/ && git stash pop
./bin/unmute compile examples/human-transfer
diff /tmp/before/sip-inbound-trunk.json build/livekit/sip-inbound-trunk.json
diff /tmp/before/sip-dispatch-rule.json build/livekit/sip-dispatch-rule.json
```

**The compiler knows none of the four names.**

```sh
grep -rn "SIP_TRUNK_HOSTNAME\|SIP_AUTH_USERNAME\|SIP_AUTH_PASSWORD\|SIP_FROM_NUMBER" \
  --include="*.go" internal/ | grep -v _test.go   # expect no matches
```

Test files may name them, because a fixture has to author something. Non-test Go
code must not, which is what FR-015 requires and SC-009 measures.

**The old names still work.** A fixture whose Connection declares
`TWILIO_SIP_ADDRESS` and friends must compile and produce the same shape with
those names substituted. Most existing fixtures do this already and are left
alone on purpose (SC-010).

**The warm-only shape.** This is the case with no fixture today and the one most
likely to break. A package with one warm transfer, no outbound channel and no
cold transfer must emit `from livekit import api`, or the transfer raises
`NameError` on the first call. Assert the import line, not just the config.

**The leftover trunk variable is inert.** A test must set
`LIVEKIT_SIP_OUTBOUND_TRUNK` to a value that could not work and still expect the
inline dial, because upstream's precedence is what makes that safe and upstream
could change its mind.

**Goldens.** One golden moves:

```sh
go test ./internal/generate -run TestGolden -update
git diff internal/generate/testdata/golden/livekit_v1_telephony_compose.yaml
```

Read that diff. The only expected change is the disappearance of
`LIVEKIT_SIP_OUTBOUND_TRUNK`. Anything else in it is a bug in the change, not in
the golden.

## Layer 2: the emitted Python is real

```sh
make smoke
```

This installs the pinned upstream packages, imports the emitted module and
instantiates what it can, which is the only layer that catches a constructor
whose signature moved. Two things it is worth confirming it covers here:

- `api.SIPOutboundConfig` resolves. Verified by hand on 2026-08-12 against
  `livekit-agents 1.6.9`, where it exists on both `livekit.api` and
  `livekit.protocol.sip`, but a pin bump could move it.
- The warm-only fixture imports. That is the `NameError` case, and an import is
  enough to catch it.

## Layer 3: the live warm transfer

This is the acceptance test from the spec, and the exact scenario that failed on
2026-08-12 with
``ValueError: `LIVEKIT_SIP_OUTBOUND_TRUNK` environment variable, `sip_trunk_id`, or `sip_connection` must be set``.

```sh
./bin/unmute compile examples/human-transfer
cd build/livekit
cp .env.example .env
```

Fill `.env`. The four carrier values come from the Twilio console under Elastic
SIP Trunking, and `SUPERVISOR_PHONE_NUMBER` is the phone you will answer. Leave
the labelled platform section blank: LiveKit Cloud injects `LIVEKIT_URL` and the
key pair itself, and `REDIS_URL` is self-hosted only.

**Do not run `lk sip outbound create`.** If a trunk already exists in the
project, leave it; it must make no difference. If you want to prove that, delete
it with `lk sip outbound delete`.

Then, following the generated README:

```sh
lk agent create --region eu-central --secrets-file .env   # first deploy
# or, for an agent that already exists:
lk agent update-secrets --secrets-file .env && lk agent deploy
lk agent status                                           # Sleeping is healthy
```

Open the Agent Console for the project, start a session, talk to the agent, then
ask for a manager.

**Expected**: the agent says one line about bringing a colleague on, hold music
starts, and the supervisor's phone rings. Answering it puts you on a briefing
with the agent, and `connect_to_caller` merges the calls.

**Expected in `lk agent logs`**: `human transfer fired: escalate_to_supervisor
(warm)` followed by no traceback. The absence of the `ValueError` is the whole
result.

If it fails, the message tells you which layer:

| Message | Meaning |
|---|---|
| `ValueError: ... must be set` | `sip_connection` did not reach the call. The template change did not land. |
| `NameError: name 'api' is not defined` | The import condition was not widened. C4. |
| A SIP error about the `From` number | `sip_number` is missing or empty. FR-003. |
| A SIP 401 or 403 from the carrier | The credentials are wrong, or the carrier requires an IP allowlist rather than username and password. Carrier side, not ours. |
| The phone never rings and no error appears | Check the carrier's own call logs. The INVITE left LiveKit and the carrier rejected or dropped it. |

## What this quickstart deliberately does not cover

- **Cold transfer**, which is unchanged and needs no trunk. Prove it separately by
  asking for billing on the same call, and it must keep working with no SIP trunk
  configured at all (SC-007).
- **Inbound**, which is unchanged and still needs an inbound trunk and a dispatch
  rule. Those two cannot go the way the outbound trunk did, because an
  unsolicited call has no request for configuration to travel with.
- **Region pinning**, which is out of scope with the reason recorded in the spec.

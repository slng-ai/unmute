# Data Model: Telephony setup

No authoring surface change and no new IR entity. This feature moves facts between existing surfaces and deletes one dead field chain. Deltas only.

## Capability table (`internal/target/telephony.go`, the single telephony rulebook)

| Field (livekit/sip/* routes) | Before | After |
|---|---|---|
| `RuntimeEnvironment` | `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET`, `LIVEKIT_SIP_INBOUND_TRUNK` (inbound), `LIVEKIT_URL`, `REDIS_URL` | the inbound-trunk row is deleted; nothing else changes |
| `DevSuppliedEnvironment` | `["LIVEKIT_SIP_INBOUND_TRUNK"]` | field deleted from the struct (it had no other member on any route) |
| `ManualSteps` | step 4 says to copy the returned trunk ID into the reported environment variable | step 4 says to run `bash telephony-setup.sh` from the build directory (or that `unmute dev --telephony` creates local records itself) |

## Plumbing chain (deleted end to end)

| Site | Before | After |
|---|---|---|
| `internal/target/telephony.go:44-47` | `DevSuppliedEnvironment []string` on the route struct | gone |
| `internal/ir/build.go:939-941` | copies route field into the IR plan's `DevEnvironment` | gone (IR debug schema regenerates by reflection) |
| `internal/generate/telephony.go:18-20` | `DevSuppliedEnv []string` on `TelephonyRuntimePlan`, serialized as `dev_supplied_environment` in the compile report | gone from struct and report |
| `internal/cli/dev_compose.go:57-62` | `externalTelephonyEnv` seeds `supplied` from `DevSuppliedEnv` so those names are not demanded of the user | loop removed; no behavior change, because the name also leaves `RequiredEnv` |
| `internal/cli/dev_compose.go:84-88` | `rejectLocalTopologyConflicts` errors when a dev-supplied name is set locally | loop removed; a stale retired value is now ignored rather than flagged (accepted, research D5) |
| `internal/cli/dev_telephony.go:160` | **not a display block.** `if len(plan.DevSuppliedEnv) > 0` gates the two-phase startup: it sets `run.infraServices` and installs the `beforeApp` hook that creates the trunk and dispatch records | condition becomes the inbound-feature check; `infraServices` and `beforeApp` stay; only the env-injection loop inside the hook goes (T018, and T020 pins it) |
| `internal/cli/dev_livekit_sip.go:66-92` | gates on `needs("LIVEKIT_SIP_INBOUND_TRUNK")`, injects the ID into env | gates on the plan's inbound feature evidence (`"inbound"`, confirmed present in a real compile report on 2026-08-12); creates the same records; injects nothing (the local agent never read the ID) |

## Emitted artifacts (build directory, inbound routes)

| Artifact | Before | After |
|---|---|---|
| `sip-inbound-trunk.json` | `trunk.name`, `trunk.numbers: ["${<FromNumberEnv>}"]` | unchanged |
| `sip-dispatch-rule.json` | `trunk_ids: ["${LIVEKIT_SIP_INBOUND_TRUNK}"]` | `trunk_ids: ["${UNMUTE_SIP_TRUNK_ID}"]`; key always present, never empty (contract) |
| `telephony-setup.sh` | does not exist | new, emitted inside the existing `if !connector` guard so the connector route never receives it; behavior in [contracts/provisioning-script.md](contracts/provisioning-script.md) |
| `README.md` | `## Configure self-hosted LiveKit SIP` holding the instructions, plus envsubst, two `lk` commands, and a manual ID copy | heading becomes `## Telephony setup` and holds the two-part runbook; contract in [contracts/runbook.md](contracts/runbook.md) |
| `.env.example` | lists `LIVEKIT_SIP_INBOUND_TRUNK` for inbound routes | does not |
| `compile-report.json` | required env includes the retired name; plan carries `dev_supplied_environment` | neither |

## Environment surface

See [contracts/environment.md](contracts/environment.md). Summary: one name removed, zero names added. `${UNMUTE_SIP_TRUNK_ID}` is a substitution token inside one JSON file, not an environment name.

## Validation rules carried by tests

- Emitted build for an inbound SIP route contains `telephony-setup.sh`; a route without inbound does not; the connector route does not, even though it carries the inbound feature.
- The connector README has no `## Telephony setup` section.
- No emitted file contains `LIVEKIT_SIP_INBOUND_TRUNK`, except the README's single retirement sentence.
- `sip-dispatch-rule.json` has a non-empty `trunk_ids` array.
- The script contains the empty-ID guard line, no `set -x`, and no `source` of the env file.
- Dev flow: a plan whose route has inbound still triggers record creation and the infra-first startup; one without does not.

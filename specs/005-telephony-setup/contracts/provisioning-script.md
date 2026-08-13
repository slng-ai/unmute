# Contract: telephony-setup.sh

Emitted into the build root only when the route is a SIP route that accepts inbound calls, inside the generator's existing `if !connector` guard. POSIX-ish bash, `set -euo pipefail`, never `set -x`. The operator runs it once per project; running it again is safe and says what it reused.

## Inputs

- The from-number env (`SIP_FROM_NUMBER` in the example) is the only variable the script reads. If it is already exported, that value is used. Otherwise the script reads **that one assignment** out of `./.env` textually, for example `sed -n 's/^SIP_FROM_NUMBER=//p' ./.env | head -1`.
- **The script MUST NOT `source` the env file.** Sourcing would read every secret in it, and one line whose name is not a valid shell identifier would abort the script under `set -e`. That exact failure has already been observed on LiveKit Cloud, where `/etc/run/env` rejected a digit-leading name and the secret went silently missing. Reading one assignment textually cannot execute anything and cannot be broken by an unrelated line.
- `./sip-inbound-trunk.json` and `./sip-dispatch-rule.json`: the emitted inputs; the script substitutes `${<FromNumberEnv>}` and `${UNMUTE_SIP_TRUNK_ID}` itself. No `envsubst`.
- `lk` authentication: whatever the operator already has (`lk cloud auth` for Cloud, or `LIVEKIT_URL` plus API key pair for self-hosted). The script does not manage auth; it checks reachability and stops with the `lk` error if not.

## Steps and guarantees

| # | Step | Guarantee |
|---|---|---|
| 1 | Preflight | `lk` and `jq` on PATH, from-number value non-empty after the read above; each missing item is named and the script exits non-zero before any create |
| 2 | Resolve | `lk sip inbound list --json`, match the trunk whose `numbers` contains the number |
| 3 | Create if absent | create from the substituted `sip-inbound-trunk.json`, then resolve again |
| 4 | Guard | if the trunk ID is still empty, exit non-zero with a message naming the number; nothing else runs. A dispatch rule can never be created without a resolved trunk ID |
| 5 | Dispatch | if a dispatch rule already targets that trunk, report it and skip; otherwise create from the substituted `sip-dispatch-rule.json` |
| 6 | Report | print one line per record: created or reused, with the record kind and (for the trunk) the number; never a credential |

## Failure modes (each named in output)

- `lk` missing, `jq` missing, from-number env empty or unset.
- `lk` unauthenticated or unreachable (surfaced as the underlying `lk` error).
- Trunk create rejected (typically the number is claimed elsewhere): the script explains that a trunk this project cannot see already claims the number.
- Unresolvable trunk after create: guard message with the number.

## Non-negotiables

- The wildcard ban: the dispatch JSON's `trunk_ids` is populated before create, or nothing is created (spec FR-005, research D4).
- No secret in output, no secret read: the script's only env read is the phone number name, and it never sources the env file.
- Idempotent by content, mirroring the dev flow's ensureRecord semantics (list, match, reuse).
- Works unchanged for Cloud and self-hosted, because `lk` carries the difference.

## Tests

- Template test pins: the guard line, the preflight checks, absence of `set -x`, absence of any `source` of the env file, and absence of any env name other than the from-number env and `UNMUTE_SIP_TRUNK_ID`.
- Structural test: emitted for an inbound SIP route, not for an outbound-only route, and not for the connector route (which carries the inbound feature but no trunk); `sip-dispatch-rule.json` carries a non-empty `trunk_ids`.
- L4 smoke (opt-in, credentialed): a real run against a disposable project, asserted by `lk sip inbound list` and `lk sip dispatch list` afterward.

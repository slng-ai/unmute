# Contract: the environment surface

**Feature**: [../spec.md](../spec.md) | **Date**: 2026-08-12

Who reads which name, before and after. Packages carry env var names only; values live in the operator's `.env` and in the platform's secret set. No name below ever appears with a value in any emitted file.

## Before (the no-carrier Daily build, unchanged)

Agent secrets: the model keys the package declares, `DAILY_API_KEY` when a cold transfer is declared, and the transfer destination names. `.env.example` lists exactly these. Nothing else. This build must stay byte-identical (spec FR-003, SC-007).

## After (a carrier build adds two groups)

### Group one: the Connection's names (author-chosen; example names shown)

| Key | Example name | Read by agent | Read by helper |
|---|---|---|---|
| `account_sid` | `TWILIO_ACCOUNT_SID` | yes (forward the inbound call) | no |
| `auth_token` | `TWILIO_AUTH_TOKEN` | yes (same request) | no |
| `sip_address` | `SIP_TRUNK_HOSTNAME` | yes (compose transfer leg) | yes (compose outbound leg) |
| `from_number` | `SIP_FROM_NUMBER` | no | yes (webhook text, logs by name) |

The example names deliberately reuse the specs/005 example's names where the meaning is identical (`SIP_TRUNK_HOSTNAME`, `SIP_FROM_NUMBER`), so an operator holding that `.env` reuses two lines unchanged and adds five (four without outbound), which is what spec SC-002 counts. Their `SIP_AUTH_USERNAME` and `SIP_AUTH_PASSWORD` lines go unused here. `sip_username` and `sip_password` are rejected on this route by the key-set gate, with the route named (research F3).

### Group two: the helper's own names (fixed by the route)

| Name | Read by | Purpose |
|---|---|---|
| `DAILY_API_KEY` | helper (and agent, when a transfer is declared, as today) | mint the per-call rooms |
| `PIPECAT_CLOUD_API_KEY` | helper | bearer for the start endpoint; the public key, not an account key |
| `UNMUTE_OUTBOUND_TOKEN` | helper | bearer the outbound trigger requires; rendered only when outbound is declared (name shared with the carrier-websocket routes on purpose: same meaning, one vocabulary) |
| `UNMUTE_HOLD_AUDIO_URL` | helper | optional; when set, the caller hears this audio on a loop. When unset the caller hears a looped spoken line instead, so the default depends on no external asset (research D4) |
| `UNMUTE_DAILY_ROOM_GEO` | helper | optional; Daily room geography, absent means Daily's default (research D7) |

The agent name is not an env value: the compiler knows it and bakes it into the helper.

## Surfaces each name reaches

- `.env.example`: every name above, grouped and commented, carrier build only.
- The platform secret set instructions: only the names the deployed agent reads (group one's first three plus today's set). Helper-only names never enter the secret set.
- The helper's startup check: every non-optional helper name, failing by name.
- The agent's startup check: its names, failing by name, as specs/004 built it.
- The README required-configuration table and the compile report: names only, marked agent-side or helper-side.

## Contract test

An L1 test asserts the split: no helper-only name appears in the secret set instructions, no agent-only name appears in the helper's startup check, the optional names are marked optional, and the no-carrier build's `.env.example` is byte-identical to today's golden.

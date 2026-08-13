# Phase 1 Data Model: Pipecat carrier telephony

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-08-12

No storage exists anywhere in this feature. The "data" is the route row, the Connection key set, the template data the generator hands its templates, the body the helper passes the agent, and the lifecycles of the three call flows.

## 1. The route row

One new entry in the single telephony route map, keyed `(pipecat, daily-sip, twilio)`.

| Field | Value | Notes |
|---|---|---|
| RequiredEnvironment | `account_sid`, `auth_token`, `sip_address`, `from_number` | Connection keys, not env names. First route to mix the REST and SIP vocabularies (research F3). |
| Features | route selection, inbound, outbound, cold transfer, hangup, and **no** call sources | Every feature `provisional` with the research log's docs URL and date, until its dated live run (spec FR-004, FR-020). Hangup is granted on verified evidence: the bot ends the call on any transport (research R13). The call sources are refused on purpose: nothing on this route fills them, so granting them would validate green and deliver nothing (research D11, R14), and the refusal names where they do work. |
| Processes | the helper, run from the build directory | Truthfully the one process this route runs; satisfies the plan validation without weakening it (research D5). |
| PublicEndpoints | the helper's `POST /call`, `POST /outbound`, `GET /healthz` | **A deliberate reinterpretation of the field.** On every other route these are the deployed application's endpoints; here they belong to the operator-run helper, and the deployed agent still exposes none of its own. The route row's comment and the compile report both have to say whose they are, or a reader will believe the agent serves them. |
| Services | `application` only, meaning the helper | Same reinterpretation, same reason. No `redis`: this route keeps no shared control record (specs/004 FR-027). |
| ManualSteps | the carrier runbook facts, summarized | The README runbook is the full text; these rows feed the capability report as on every route. |

The existing `daily_dialout` prerequisite rule already matches any carrier on this transport; its summary is refreshed to say it covers SIP dial-out too (research F2). The no-carrier key `(pipecat, daily-sip)` deliberately stays absent from the route map, exactly as today.

## 2. The Connection

Existing `kind: telephony` object; this route gives it a new key set. Values are env var names, never values.

| Key | Read by | Used for |
|---|---|---|
| `account_sid` | deployed agent | the one forwarding request that moves the inbound call to the room (research D3) |
| `auth_token` | deployed agent | same request, basic auth |
| `sip_address` | deployed agent and helper | composing every outbound SIP URI: `sip:{E164}@{sip_address}` (research F2) |
| `from_number` | helper, runbook | the package's number; webhook identity checks and operator-facing text |

`sip_username` and `sip_password` are deliberately not accepted on this route: Daily's dial-out carries no credential auth on any documented surface, so a value here would be a promise nothing keeps (research F3). Validation rejects them with the route named, through the same key-set gate every route already has.

**Pairing rules** (all existing mechanisms, newly legal on this route):

- `carrier: twilio` + `connection:` + `channels.phone` are valid together on a `daily-sip` target and mutually required, same as on every carrier route.
- A `daily-sip` target with none of the three keeps its exact current meaning. A `daily-sip` target with some but not all of the three fails with the existing pairing errors.
- A telephony channel still forces `capacity` with positive `peak_starts_per_second`.

## 3. Template data deltas

The generator's data for a carrier build gains one group, absent on the no-carrier build so the templates cannot render carrier content by accident:

| Field | Content | Consumed by |
|---|---|---|
| Carrier identity | `twilio` | README carrier part keying, bot forwarding block keying |
| The four env names | resolved from the Connection | bot carrier block, helper, README required-config table, `.env.example`, secret set instructions |
| HasInbound / HasOutbound | from the phone channel | which helper endpoints and README parts render |
| HasColdTransfer | existing field | SIP-URI destination composition in the transfer block (research F2) |
| Helper artifact flag | true only for carrier builds | file emission list, compile report |

The transfer block's destination changes shape on carrier targets only: `toEndPoint` becomes the composed SIP URI instead of the bare E.164. Daily-only targets keep today's bytes.

## 4. The body contract (helper to agent)

The helper starts the agent with `createDailyRoom: false` and a `body` the bot validates before building anything (malformed body fails loud, naming the field). One shape, one optional branch per direction:

```json
{
  "room_url": "https://<domain>.daily.co/<room>",
  "token": "<meeting token>",
  "direction": "inbound" | "outbound",
  "call_sid": "<carrier call id>",        // inbound only
  "sip_uri": "sip:<room>.0@<domain>.sip-us.daily.co",  // inbound only: the forward target
  "dialout": { "sip_uri": "sip:+15551230000@<sip_address>" }  // outbound only
}
```

- Inbound: the bot joins the room, waits for the ready event, performs the carrier forward once (research D3), then runs the normal pipeline.
- Outbound: the bot joins the room and calls `start_dialout` with the composed SIP URI and `provider: "daily"` (research R6), then runs the normal pipeline on answer.
- A body with no `direction` is not a phone session; the bot behaves exactly as today (spec FR-003, US1 scenario 5).

The helper composes `dialout.sip_uri` because the outbound trigger's caller supplies only a destination number; the bot composes the transfer `toEndPoint` at call time because the destination is resolved from the declared `destinations` map, as today.

## 5. Call lifecycles

**Inbound**: carrier webhook received → helper validates env and payload → room created with the interconnect provider and the SIP endpoint captured → agent started with the body → helper answers the webhook with looped hold audio → bot ready event fires (possibly more than once) → exactly one forwarding request updates the live call to dial the room's SIP address → caller and agent connected. Failure at any step before the forward: the helper answers with a spoken failure rather than eternal hold, and logs the named cause.

**Outbound**: trigger received with a bearer token → helper validates → room created with dial-out enabled → agent started with the composed SIP URI → bot dials out through the carrier trunk → recipient answers and the pipeline runs. Failure surfaces through the dial-out warning and stopped events, named in logs.

**Cold transfer** (unchanged shape, new leg): caller asks → guard claims the attempt → spoken handoff → `sip_call_transfer` with the composed SIP URI → on answer the agent's session ends; on failure the caller stays connected and is told, per the declared behavior. Daily stays in the media path after a completed transfer and both legs keep billing until the call ends (research F4); the runbook states this cost.

## 6. Report and environment surfaces

- The compile report gains the helper in `generated_files`, the route row's provisional evidence, and the same prerequisite row it prints today.
- `.env.example` for a carrier build adds the helper's names and the Connection's names; the no-carrier build's file is byte-identical to today's. The full split of who reads which name lives in [contracts/environment.md](contracts/environment.md).

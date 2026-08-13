# Contract: environment, by declaration shape

**Feature**: [../spec.md](../spec.md) | **Date**: 2026-08-13

The route's defining property is that the environment shrinks with the
declaration. There is no helper side at all; every name below is read by the
deployed agent (or by the operator's own shell, for the outbound command), so
the platform secret set is the only place values live.

## By shape

| Declaration | Required beyond model keys | Why |
|---|---|---|
| phone inbound only | **nothing** | the platform receives the call without credentials (research F4). The emitted bot builds the carrier transport itself on this shape, with automatic hangup off, because the framework's factory refuses to build one without credentials for REST hangup (research F15). Socket close still ends the call |
| \+ outbound | `account_sid`, `auth_token`, `from_number` envs; `PIPECAT_CLOUD_ORGANIZATION` | the call is created at Twilio (F6) and the TwiML must name the service host (D5, D6) |
| \+ cold transfer | same four | the update request needs the account pair (F5); the reconnect markup needs the host (D7); the destination env (e.g. `BILLING_PHONE_NUMBER`) comes from the declared destination as on every route |

## Rules

- The per-call check runs at the start of a **phone** session, after the
  handshake identifies it as one, and names every missing value. Browser and
  console sessions on the same package never read, and are never asked for, any
  name in this file beyond the model keys (spec FR-008; data-model §5).
- `TWILIO_ACCOUNT_SID` and `TWILIO_AUTH_TOKEN` (or whatever names the operator's
  connection maps) are agent-side platform secrets here, unlike 006 where the
  forward ran in the agent too: same side, same reasoning.
- `PIPECAT_CLOUD_ORGANIZATION` holds the organization name, which is not a
  secret but is also not knowable at compile time; it lives in the same secret
  set for the agent and in the operator's shell for the outbound command.
- The three SIP keys (`sip_address`, `sip_username`, `sip_password`) are refused
  on this route by name at compile time. Nothing on this route speaks SIP.
- No secret value ever appears in an emitted file; names only (spec FR-011).
- `.env.example` renders exactly the names the declaration shape requires,
  grouped and commented; a pure-inbound package's file gains nothing.

## Contract tests (offline)

- A pure-inbound package compiles with no connection and its compile report
  lists no carrier environment.
- A transfer-declaring package's report lists exactly the four names plus the
  destination, and the per-call check in the emitted bot names the same set (one
  source, asserted equal).
- A SIP key on this route fails naming the key, the route, and the accepted set.
- The secret-set guidance in the README names no value that the agent does not
  read (there is no helper side to exclude on this route, and the test asserts
  that no such language appears).

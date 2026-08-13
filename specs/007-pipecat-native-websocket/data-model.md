# Data Model: Pipecat native WebSocket telephony

**Feature**: [spec.md](spec.md) | **Date**: 2026-08-13

Entities in compiler order: what the author declares, what the rulebook says
about it, what the build carries, and what a session sees at call time. No new
authoring fields exist; the feature is a new value in an existing field plus a
narrower connection key set.

## 1. Route row (internal/target)

One new row in the single capability rulebook.

| Field | Value |
|---|---|
| Key | `(pipecat, "cloud-websocket", "twilio")` |
| Docs | `https://docs.pipecat.ai/pipecat-cloud/guides/telephony/twilio-websocket` |
| Features | route selected, inbound, outbound, cold transfer, hangup, all `provisional`, note "built and offline-proven; no call has been placed through this endpoint yet" |
| Refused | warm transfer (message names the callback endpoint it would take); `source.*` call sources (message names where they work); `sip_address` / `sip_username` / `sip_password` connection keys |
| RequiredEnvironment | `account_sid`, `auth_token`, `from_number`, required only when outbound or cold transfer is declared (D4) |
| Processes | **none** (this is the row's defining difference from 006's) |
| PublicEndpoints | **none** |
| RuntimeEnvironment | `PIPECAT_CLOUD_ORGANIZATION` when outbound or cold transfer is declared (D5) |

`Processes` and the IR plan's services answer different questions, and the pair
reads like a contradiction unless it is said out loud. `Processes` is empty
because **the operator hosts nothing**: there is no artifact in the build for them
to run. The IR telephony plan still carries one service, `application`, because
the agent is an application; the platform hosts it in production, and
`unmute dev --telephony` runs that same application locally. A route with an
empty `Processes` and one `application` service is exactly the shape this feature
exists to produce.

The emitter agreement test extends to this row: every granted feature must have
an emitted code path, and a row with no processes must produce a build with no
process artifact.

`hangup` is granted whether or not carrier credentials are present, and it means
two different mechanisms (spec FR-005): the carrier's call control when they are,
and closing the stream when they are not. The route row's note records that, so a
reader of the rulebook is not left inferring it.

The second mechanism costs one emitted branch, and research F15 is why. The
framework's transport factory always asks the carrier serializer for REST-based
hangup, and that serializer refuses to be built without credentials for it. So on
the **no-connection shape only**, the emitted bot builds the transport itself with
automatic hangup off. Every other shape uses the framework's path unchanged. The
branch is compile-time, keyed on whether a connection was declared, so no build
carries a path it cannot take.

## 2. Authoring surface (internal/spec, unchanged shapes)

- `targets.yaml`: `transport: cloud-websocket`, `carrier: twilio`,
  `connection: <name>` (only when the package places or redirects calls).
- `connections/<name>.yaml`: keys `account_sid`, `auth_token`, `from_number`,
  each mapping to an environment variable name. The three SIP keys are refused
  on this route by name. The examples reuse `telephony-hello`'s `twilio_voice`
  mapping verbatim (`TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`,
  `TWILIO_PHONE_NUMBER`), so one `.env` drives every Twilio example; future
  carriers follow `<CARRIER>_*` (spec FR-017, research D13).
- `agent.yaml`: the existing `channels.phone` (inbound/outbound) and
  `controls.<name>.cold.destination` carry the whole feature. No new field.

What `from_number` must hold (spec FR-006a): a voice-capable phone number the
operator owns in the Twilio account those credentials belong to. It is the caller
identity the recipient sees, it may be the same number that receives calls (which
is why the examples share one value), and a number carries no separate "outbound"
capability to enable. What can still refuse a call lives on the account, not the
number: destination-country permissions, and any restriction on calling
unverified destinations. Those two are troubleshooting rows, and their exact
rules stay out of emitted text until dated verification lands (research F14).

Mutual-requirement guards (ir.Build):

| Declaration | Result |
|---|---|
| phone inbound only, no connection | valid: the platform receives the call without credentials (D4) |
| phone outbound or cold transfer, no connection | error naming the three keys and why each is needed |
| connection declared, no phone channel and no transfer | error: dead weight, names what to remove or add |
| connection carrying a SIP key | error naming the key, the route, and the accepted set |

## 3. Build-time data group (internal/generate)

A third, separate data field beside `Telephony` (carrier-websocket) and
`DailyCarrier` (006), for the same reason 006's was separate: sites that read
one group must not half-match another.

```go
type pipecatCloudWebsocket struct {
    Carrier        string   // "twilio"
    Connection     string   // connection name, "" on pure inbound
    AccountSIDEnv  string   // "" on pure inbound
    AuthTokenEnv   string
    FromNumberEnv  string
    OrganizationEnv string  // "PIPECAT_CLOUD_ORGANIZATION", set when placing/redirecting
    HasInbound     bool
    HasOutbound    bool
    Region         string   // deployment region or "", picks the wss host in emitted text
    CallEnv        []string // names checked at the start of a phone session only
}
```

Derived, never stored twice:

- **Service host** = `<agent name>.<organization>`: agent name compiled in,
  organization read from `OrganizationEnv` at call time (bot) or pasted by the
  operator (Bin). The README renders the Bin with the compiled agent name
  already in place, so the operator substitutes exactly one value.
- **wss URL** = `wss://api.pipecat.daily.co/ws/twilio` or
  `wss://<region>.api.pipecat.daily.co/ws/twilio` when `Region` is set; one
  template helper, used by the Bin, the outbound command, and the transfer
  reconnect markup so the three can never disagree.

## 4. Manifest and project deltas

| File | Delta when this route is selected |
|---|---|
| `pcc-deploy.toml` | `websocket_auth = "none"` line (D3) |
| `pyproject.toml` | `websocket` added to the pipecat-ai extras (D12) |
| `bot.py` | `"twilio"` entry in `transport_params`; phone-session detection off `runner_args.call_data`; per-call env check; outbound greeting note; cold transfer tool body |
| `README.md` | the route's Telephony setup section (contracts/runbook.md) |
| `.env.example` | connection names + `PIPECAT_CLOUD_ORGANIZATION`, only when declared |
| emitted file list | **unchanged**: no new file, which is itself asserted by test |

## 5. Call-time session shapes (emitted bot)

What a session is, decided once, right after the transport exists:

| Session | Detected by | Environment checked |
|---|---|---|
| Browser / WebRTC | `transport_type` absent or not `"twilio"` | model keys only (existing `REQUIRED_ENV`) |
| Console | local transport, unchanged path | model keys only |
| Phone, inbound | `transport_type == "twilio"`, `call_data.body` has no `direction` | `CallEnv` if the package declares outbound/transfer (the transfer must work on an inbound call), else nothing extra |
| Phone, outbound | `call_data.body["direction"] == "outbound"` | `CallEnv` |

`call_data` fields used and their sources (research F3, F4):

- `call_id`: Twilio CallSid, from the stream start message; the handle every
  REST call-control request uses.
- `body`: the TwiML custom parameters: `direction` (outbound marker, D6),
  `from_number` / `to_number` (Bin templating, D9).

## 6. Cold transfer state

Reuses 006's module-scope `_TRANSFER_RESULT` discipline unchanged: one process
serves one call, the result of an attempt is recorded before the model sees it,
and a second request replays the recorded outcome rather than re-firing. The
transfer's TwiML (announce → update → sequential failure continuation) is
specified in contracts/carrier-markup.md; its one state transition worth naming:

```
announced → twiml_updated → (stream closes; session ends)
                             └─ on dial failure, carrier-side: caller hears the
                                failure line and reconnects as a NEW session
```

The new session is a fresh inbound phone session with no memory; that limit is
part of the route's contract, not an accident (plan, Complexity Tracking).

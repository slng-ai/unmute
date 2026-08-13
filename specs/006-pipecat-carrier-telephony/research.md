# Phase 0 Research: Pipecat carrier telephony

**Feature**: [spec.md](spec.md) | **Date**: 2026-08-12

Every platform claim below was verified against the official documentation or the official example source on 2026-08-12. The verification log at the end carries the exact sources. Nothing here rests on model memory.

## The four open facts from the spec, resolved

### F1. Can the platform's start endpoint create the interconnect room? No, and it would not help if it could.

The start endpoint (`POST /v1/public/{agentName}/start`) documents `dailyRoomProperties` as a passthrough to Daily's room config, and Daily's room config documents `sip.provider` with the single enum value `"daily"`. So the combination is plausible on paper. But no Pipecat document or official example ever expresses the interconnect through the start endpoint, and the start response carries no SIP endpoint field (`dailyRoom` and `dailyToken` only). The inbound flow needs the room's SIP address to forward the carrier's live call, so a room minted by the start endpoint leaves the webhook server unable to complete the call.

**Decision**: the emitted helper creates the room itself, using the documented Pipecat helper (`pipecat.runner.daily.configure(session, sip_caller_phone=..., sip_provider="daily", enable_dialout=...)`), which returns the room, a token, and the SIP endpoint. It then calls the start endpoint with `createDailyRoom: false` and passes `room_url`, `token`, `call_sid`, and `sip_uri` in `body`, exactly as the official `daily-twilio-sip-dial-in` example does. The agent reads them from `runner_args.body`.

**Consequence**: the helper needs a Daily API key and a Pipecat Cloud public API key; the deployed agent needs the carrier REST credentials (to forward the call) but no Daily API key beyond what it has today.

### F2. Which side carries the outbound and transfer dial legs? The carrier trunk carries the media; Daily's dial-out feature still gates the action.

Dial-out to a SIP URI sits inside Daily's approved dial-out flow (paid account, request form, `enable_dialout` per domain and per room); the checklist's only SIP concession is "Purchase a phone number (skip if dialing a SIP URI)". So the existing `daily_dialout` account prerequisite stays, for outbound and for cold transfer alike, and no Daily number purchase is needed on carrier targets.

**Decision**: on a carrier target, every outbound leg is a SIP URI composed at the carrier's termination address: the agent's outbound call and the cold transfer destination both become `sip:{E164}@{sip_address}`. Daily-only targets keep dialing E.164 through Daily PSTN exactly as today. This keeps one dial path per target shape, sends the billing and the caller identity to the side the operator already controls, and avoids requiring a purchased Daily number.

**Caller identity**: Daily documents `callerId` for PSTN dial-out only; what the SIP `From` header carries when dialing a SIP URI is not documented anywhere. On the Twilio side, caller ID for termination traffic is governed by the trunk and the numbers the account owns. The emitted instructions state that the recipient's display is governed at the carrier, and the live run must confirm what actually shows. Until then the claim stays provisional.

### F3. How does the outbound SIP leg authenticate to the carrier? IP allow-list only. This fixes the Connection key set.

All three documented dial-out surfaces (daily-js, REST, daily-python) carry no SIP username or password field, and the REST schema formally rejects extra fields. Daily's own guidance is to allow-list its published static addresses (`https://ip-info.daily.co/ips/ip-info.json`, the `sip.hosts` entries) on the carrier side. So Twilio termination must authenticate by IP access control list, not by credential list.

**Decision**: the route's Connection keys are `account_sid`, `auth_token`, `sip_address`, `from_number`.

- `account_sid` and `auth_token`: the carrier REST credentials the forwarding action needs (both names already exist in the vocabulary, on the carrier-websocket and connector routes).
- `sip_address`: the trunk termination address every outbound leg dials through (already exists on the LiveKit sip routes).
- `from_number`: the package's number (exists everywhere).
- `sip_username` and `sip_password` are deliberately absent: there is nothing on the Daily side that could carry them. This is the first route to mix the REST and SIP vocabularies, and it does so because the route genuinely spans both surfaces: REST to forward the inbound call, SIP to carry the outbound legs.

**Security note the runbook must carry**: Daily's egress addresses are shared by all Daily customers, so an IP allow-list alone means any Daily-originated call that knows the termination address could dial through the trunk. The runbook tells the operator to treat the termination prefix as non-guessable and points at the carrier's own hardening options.

**Operational note**: the IP list is dynamic; changes are published three days ahead in the same file. The runbook names the list URL and tells the operator to re-check it if termination starts rejecting calls. No polling automation in this feature.

### F4. Does the existing cold transfer primitive cover the carrier leg? Yes, by documented category.

The emitted bot calls `sip_call_transfer({"toEndPoint": ...})`. Daily documents `sipCallTransfer` as working "for both dial-in and dial-out connections" with "SIP-to-SIP, PSTN-to-PSTN, SIP-to-PSTN, and PSTN-to-SIP transfers all supported". A caller bridged in from Twilio by `<Dial><Sip>` is a SIP dial-in leg on Daily's side, so the existing primitive covers it by category. The word "interconnect" appears nowhere in the transfers guide, so the exact topology still needs its live run before the capability row stops being provisional.

**Decision**: keep `sip_call_transfer`, with `toEndPoint` composed per F2 on carrier targets. Reject `sip_refer` for this feature: it would take Daily out of the media path and stop its billing, but it depends on the originating SIP system honoring REFER, and whether Twilio's `<Dial><Sip>` leg does is documented on neither side. The runbook states the cost consequence: after a completed transfer, Daily stays in the call path and both legs keep billing until the call ends.

## Design decisions

### D1. The helper is one small emitted server with two jobs

**Decision**: one emitted Python artifact, `telephony_helper.py`, part of the build when the target declares a carrier. It serves the inbound webhook (`POST /call`: create the interconnect room, start the agent with `createDailyRoom: false`, answer the carrier with hold audio TwiML) and the outbound trigger (`POST /outbound`: create a dial-out enabled room, start the agent with the destination in `body`), plus a health path. It refuses to start with any needed environment value missing, naming it.
**Rationale**: mirrors the two official examples (dial-in and dial-out servers) collapsed into one file; one file is one thing to run and one URL to tunnel.
**Alternatives rejected**: two servers (twice the runbook); putting the webhook inside the deployed agent (the platform starts agents through its own entry point and hosts no arbitrary HTTP endpoints, specs/004).

### D2. Room creation uses the documented Pipecat helper, not hand-rolled REST

**Decision**: `pipecat.runner.daily.configure(..., sip_provider="daily", enable_dialout=...)`, exactly as the official example.
**Rationale**: it is the documented way to request the interconnect, it returns the SIP endpoint the flow needs, and the generated project already pins pipecat.
**Alternatives rejected**: direct REST against Daily's rooms API (more code to own, and the `sip.provider` default-when-omitted is undocumented, so composing it by hand invites a silent wrong room).

### D3. The bot forwards the inbound call, once, and that is the whole carrier seam

**Decision**: the bot's `on_dialin_ready` handler performs the carrier's call-update (Twilio: one HTTPS call updating the call by `call_sid` with `<Response><Dial><Sip>{sip_uri}</Sip></Dial></Response>`), guarded by a forward-once flag because the ready event can fire more than once. The HTTPS call uses the HTTP client already in the project's dependency tree, with basic auth from the Connection's REST credential names. No carrier SDK dependency.
**Rationale**: the ready event is the documented signal and it fires in the bot; the update is one HTTP request, which does not justify a vendor SDK (the official example's use of the Twilio SDK is convenience, not necessity).
**Alternatives rejected**: forwarding from the helper (it has no documented signal for SIP readiness and would have to poll); the Twilio Python SDK (a blocking client and a new dependency for one request).

### D4. Hold audio is an optional environment-named URL, and the default plays no third-party asset

**Decision**: the helper answers the webhook with TwiML that plays a looped audio URL when the optional environment value is set. When it is unset, the default is a looped spoken line ("one moment while I connect you"), not an audio file. A bare pause is forbidden: the platform documents it as too short for room setup.
**Rationale**: silence reads as a dead call (spec SC-004), but the official example's hardcoded third-party asset URL is a dependency a generated project should never carry: the day that host goes away, every generated project plays silence and nobody knows why. A spoken line needs no external host, cannot rot, and says something more useful than music. Real hold music stays one environment value away.
**Alternatives rejected**: pinning the example's asset URL (rots outside our control); requiring the value with no default (friction against the zero-friction goal, for a caller experience a sentence already solves).

### D5. The route row and its runtime process

**Decision**: register `(pipecat, daily-sip, twilio)` in the single telephony route map with the F3 key set, features route selection, inbound, outbound, cold transfer, and hangup, all provisional until their dated live runs. The call sources are deliberately excluded; D11 owns that decision and its reason. Hangup is included on evidence, not assumption: the emitted bot pushes an end frame both when a tool declares it ends the call and on the transfer's hangup branch, and neither site is gated on the transport or on the telephony plan (R13). The row carries the helper as its one runtime process, because that is truthfully the process this route runs.
**Rationale**: the route map is the one home for route facts; a row with no process would need the plan validation weakened instead.
**Alternatives rejected**: relaxing the "telephony plan has no runtime process" validation (weakens a guard that protects every other route).

### D6. `unmute dev --telephony` keeps refusing on this route, with an accurate message

**Decision**: the refusal stays for every daily-sip target. For carrier targets the message changes: it names the helper, says the local test path is running the helper beside a tunnel as the README dictates, and stops claiming there is no local telephony topology. The no-carrier message stays as it is.
**Rationale**: spec puts the local dev flow out of scope but requires the message to stay accurate; the current wording becomes false the moment a carrier target exists.

### D7. Region stays split and honest

**Decision**: the deployment region keeps meaning the platform region, exactly as specs/004 wired it. The Daily room's geography is a separate optional helper environment value passed to room creation when set, absent by default.
**Rationale**: the two vocabularies (platform regions, Daily room geographies) have no documented mapping; inventing one is a silent-downgrade risk the constitution forbids. The official example sets a room geography by hand for the same reason.

### D8. The schema amendment supersedes N34's channel rejection

**Decision**: one numbered dated amendment records: the Daily route gains carrier legs; on a daily-sip target, `carrier:`, `connection:`, and `channels.phone` are now valid together and mutually required, with the F3 key set; the no-carrier form keeps its exact meaning and stays connection-free. N34's statement that the Daily route takes no phone channel is superseded, dated, with the old text left as history.
**Rationale**: Principle IV; the tests that enforce N34 today are inverted deliberately in the same change.

### D9. The example

**Decision**: a new example `examples/human-transfer-daily-twilio`: one Pipecat target on `(pipecat, daily-sip, twilio)`, a telephony Connection with the four F3 names, `channels.phone` inbound and outbound, one cold transfer, capacity declared. The existing `human-transfer-daily` example stays untouched as the no-carrier form.
**Rationale**: spec FR-019; the example directory list and the route-scoping marker test both pin examples exactly, so a new directory is the honest change.

### D11. The route grants no telephony call sources, because nothing fills them

**Decision**: the route row grants route selection, inbound, outbound, cold transfer, and hangup, and **not** the `source.*` call-source features. A package declaring a variable sourced from the caller number, called number, call identifier, direction, or their siblings fails by name on this route.
**Rationale**: found by reading the code rather than the docs. The generated bot reads system-source values out of a context table (`bot.py.tmpl`, the `SystemSources` loop), and that table is filled only by the carrier-websocket adapters (`telephony_shared.py`, `telephony_twilio.py`). This route emits no adapter, so granting the features would let a package validate green and receive empty values on a live call, which is precisely what Principle II forbids and what the compliance review asks reviewers to catch. The values themselves are all available in the helper's webhook payload, so lifting this refusal later is cheap; doing it now widens the feature past the four flows the requester asked to test.
**Alternatives rejected**: granting them and filling the context in the bot's carrier block (real work, no requester need yet, and it would ship untested against a live call); granting them and hoping the values arrive (the silent-downgrade failure this repository has already been burned by).

### D10. Warm transfer compatibility costs nothing here

**Decision**: no warm code, no warm shape. The carrier leg joins the same room a Daily-provisioned call joins, as a SIP dial-in participant; the future warm feature (bot as bridge, second dial-out leg) sees no difference except that its supervisor leg composes a SIP URI per F2 on carrier targets.
**Rationale**: spec FR-006.

## What implementation must not break (from the code map)

The full surface map lives with the plan. Five load-bearing changes, listed here because each is currently enforced by code or a test that must be changed deliberately, never deleted silently. The first is not a guard at all, which is exactly why it is easy to miss:

1. **Roughly twenty-four gating sites read `.Telephony != nil` as "carrier-websocket"**: nine in the Pipecat README template, eleven in the bot template, one each in the Dockerfile and pyproject templates, plus four in the driver's Go (`pipecat_v1.go:439`, `pipecat_v1_build.go:158`, `:306`, `:784`). Giving this route a telephony plan switches every one of them on. They must be narrowed to the transport before the helper is emitted, or a carrier build silently gains the whole carrier-websocket artifact set and loses its deploy manifest.
2. `ir/build.go` requires connection and phone channel together; today the Daily route has neither. The carrier form now needs both; the no-carrier form still refuses both.
3. `pipecat_v1_build.go` hard-refuses any Pipecat telephony plan outside carrier-websocket, twice (route adapter and transfer tool). Both refusals learn the daily-sip carrier form.
4. `TestUS2_DailyProjectDeclaresNoServiceOrEndpoint` pins that a Daily build has no service and no endpoint. That stays true for the no-carrier form; the carrier form emits the helper and the test splits in two.
5. `TestDailyRouteNeedsNoConnectionOrChannel` and the specs/004 comment block behind FR-002 pin the old authoring stance; both follow the amendment.

## Verification log

| # | Fact | Source | Verified |
|---|------|--------|----------|
| R1 | Start endpoint: `POST /v1/public/{agentName}/start`, Bearer public key, `createDailyRoom`, `dailyRoomProperties` passthrough to Daily room config, `body` up to 1 MB reaching the agent as `runner_args.body`. Response carries `dailyRoom`, `dailyToken`, `sessionId`, and no SIP endpoint. | docs.pipecat.ai/api-reference/pipecat-cloud/rest-reference/endpoint/start.md | 2026-08-12 |
| R2 | Agent receives `session_id`, `room_url`, `token`, `body` (Daily runner arguments). | docs.pipecat.ai/api-reference/pipecat-cloud/sdk-reference/session-arguments.md | 2026-08-12 |
| R3 | The official interconnect example creates the room server-side via `configure(..., sip_provider="daily", enable_dialout=True)`, starts the agent with `createDailyRoom: false` and `body` carrying `room_url`, `token`, `call_sid`, `sip_uri`; the bot forwards on `on_dialin_ready` by updating the Twilio call; the server answers the webhook with looped hold audio, never a pause. | github.com/pipecat-ai/pipecat-examples phone-chatbot/daily-twilio-sip-dial-in (server.py, server_utils.py, bot.py), docs.pipecat.ai/pipecat/telephony/twilio-daily-sip.md | 2026-08-12 |
| R4 | Daily room `sip` config: `display_name` and `sip_mode: dial-in` required, `provider` enum holds only `daily`, `num_endpoints`, codecs; `sip_uri` is read-only on the response; `enable_dialout` is a separate room property. Default when `provider` is omitted is undocumented. | docs.daily.co/reference/rest-api/rooms/create-room | 2026-08-12 |
| R5 | Interconnect SIP addresses are per room: `sip:$roomName.$index@$domainName.sip-us.daily.co`. | docs.pipecat.ai/pipecat/telephony/daily-sip.md | 2026-08-12 |
| R6 | Dial-out options across daily-js, REST, daily-python: `sipUri` or `phoneNumber`, `displayName`, `userId`, `callerId` (PSTN only in every prose description), codecs, permissions, `provider` (daily-python and Pipecat only). No SIP auth credential field on any surface; REST rejects unknown fields. | docs.daily.co/reference/daily-js/instance-methods/start-dial-out, docs.daily.co/reference/rest-api/rooms/dial-out/start, reference-python.daily.co/types.html | 2026-08-12 |
| R7 | Dial-out requires a paid account and per-domain approval on request; the checklist covers the SIP URI case ("Purchase a phone number (skip if dialing a SIP URI)"); rooms need `enable_dialout`. | docs.daily.co/guides/products/dial-in-dial-out | 2026-08-12 |
| R8 | `sipCallTransfer`: works for dial-in and dial-out legs, SIP-to-SIP and SIP-to-PSTN supported, Daily stays anchored and both legs keep billing; `callerId` on it is PSTN-only. `sipRefer`: removes Daily from the path, requires the originating SIP system to honor REFER, billed per call. | docs.daily.co/guides/products/dial-in-dial-out/transfers, docs.daily.co/reference/daily-js/instance-methods/sip-call-transfer, sip-refer | 2026-08-12 |
| R9 | The emitted bot's transfer primitive is `sip_call_transfer({"toEndPoint": ...})` behind an `isinstance(_TRANSPORT, DailyTransport)` narrow, with the module-global at-most-once guard. | internal/generate/templates/pipecat_v1/bot.py.tmpl (transfer block) | 2026-08-12 |
| R10 | Daily's allow-list source is `https://ip-info.daily.co/ips/ip-info.json`; `sip.hosts` are the SIP servers; the list is dynamic with changes published three days ahead. | docs.daily.co/guides/privacy-and-security/corporate-firewalls-nats-allowed-ip-list | 2026-08-12 |
| R11 | The ready event can fire more than once; the documented practice is a forward-once guard. | docs.pipecat.ai/pipecat/telephony/daily-sip.md | 2026-08-12 |
| R12 | The twilio-daily-sip guide's cloud deployment snippet contradicts the example repo (`createDailyRoom` true beside a passed room); the example repo is authoritative. Do not copy the guide's snippet. | both sources above, compared | 2026-08-12 |
| R13 | Hangup has an emitted path independent of transport: the bot pushes an end frame when a tool declares it ends the call, and again on the transfer's hangup-on-unavailable branch. Neither site is gated on the transport or on the telephony plan, so the route may grant hangup on evidence. | internal/generate/templates/pipecat_v1/bot.py.tmpl lines 238 and 338 | 2026-08-12 |
| R14 | Telephony call sources have **no** emitted path on this route: the bot reads them from a context table (the `SystemSources` loop) that only the carrier-websocket adapters fill, and this route emits no adapter. This is the evidence behind D11. | internal/generate/templates/pipecat_v1/bot.py.tmpl line 139, telephony_shared.py.tmpl, telephony_twilio.py.tmpl | 2026-08-12 |

## Claims that stay provisional until the live runs

- What the recipient of a carrier-trunk outbound call sees as caller identity (undocumented on the Daily side, governed at the carrier, F2).
- `sip_call_transfer` behavior on the exact interconnect topology (documented by category, not by name, F4).
- The full inbound path timing (webhook answer to agent speech) against spec SC-004.

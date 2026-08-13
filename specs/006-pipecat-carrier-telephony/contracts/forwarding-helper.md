# Contract: the call-forwarding helper

**Feature**: [../spec.md](../spec.md) | **Date**: 2026-08-12

> **Amended 2026-08-13, at the requester's decision: `POST /outbound` is removed
> from this contract, and with it `UNMUTE_OUTBOUND_TOKEN`.**
>
> The reason the helper exists is that an *incoming* call needs a room that does
> not exist yet: Daily makes one room per call and its SIP address arrives with it,
> so a carrier has no static address to be pointed at. Dialling out has no such
> problem. The room only needs dial-out enabled, which the platform's own start
> endpoint does, so outbound is one command against the platform, exactly as it
> already is on a Daily-provisioned number.
>
> The token existed to guard that endpoint, and the guard was real: the helper is
> reachable from the internet by necessity, so an unauthenticated call-placing
> endpoint would let anyone who found the URL dial through the operator's trunk.
> Removing the endpoint removes the exposure and the value the operator had to
> invent and keep. The helper now has exactly two endpoints, `POST /call` and
> `GET /healthz`, neither of which spends money, so it needs no authentication at
> all. The sections below are superseded where they disagree with this note.

> **Amended 2026-08-13, second time, by what a real deploy proved: the helper no
> longer creates the room. It asks the platform to create it.**
>
> The first live attempt never reached the agent at all, and the two reasons were
> both structural.
>
> The emitted image was built on `python:3.12-slim` and ran the pipecat dev
> runner. Pipecat Cloud starts a session by POSTing `/bot` to the container and
> gates the deployment on readiness probes it serves itself; both live in
> `dailyco/pipecat-base`. Nothing in our image answered either, so the deployment
> never reached ready and every start was refused with `PCC-1001` while the
> container's own log showed a clean boot. The Dockerfile for the Pipecat Cloud
> shape is now built on the base image, copies named files rather than the whole
> directory (the base image owns `/app`), installs dependencies rather than the
> project, and declares no `CMD`. Verified by building and running the emitted
> image: `GET /readyz` answers 200 and `POST /bot` reaches the emitted `bot()`.
>
> With that fixed, `createDailyRoom: false` would still have failed. A start
> request with no room reaches the bot as `PipecatSessionArguments`, which carries
> no room and which pipecat's transport factory refuses by name — observed in the
> running container. So the helper now sends `createDailyRoom: true` with the SIP
> interconnect asked for in `dailyRoomProperties`, which is the shape Pipecat
> Cloud's own Twilio-SIP guide uses. The platform hands the room to the agent the
> ordinary way, `create_transport` builds the transport, and the agent learns the
> room's SIP address from the `on_dialin_ready` event, which is both the address
> and the signal that it is usable.
>
> Consequences: the helper reads no `DAILY_API_KEY` (one required value, not two);
> an inbound body carries `direction` and `call_sid` and nothing else; and the bot
> builds no transport by hand. Proven end to end on 2026-08-13 without a phone, by
> driving `POST /bot` at the running container with a real SIP-enabled room: the
> bot joined, `dial-in ready` fired with the room's address, and the forward went
> out to Twilio and was refused only because the call id was synthetic.

`telephony_helper.py` is an emitted artifact, present in the build exactly when the target declares a carrier. It is the operator-run bridge between the carrier's webhook and the platform's start API. It is carrier-neutral in structure; the only carrier-specific piece in the whole feature is the forwarding action in the bot (research D3), not here.

## Runtime shape

- Run from the build directory: `uv run telephony_helper.py`. One process, one port.
- Startup: read every needed env name; if any value is missing, print the missing names, one line each, and exit non-zero. Never start half-configured (spec FR-007).
- No secret value is ever logged or echoed; log lines name env vars by name only.

## Environment it reads

Exact names and the split with the agent live in [environment.md](environment.md). By role, after both amendments above: the platform's public API key, and nothing else required. Two optional knobs: the hold audio URL and the Daily room geography (research D4, D7). No Daily API key, because the room is the platform's to create. The agent name is baked in at compile time; it is not an env value.

## Endpoints

### POST /call (the carrier webhook, inbound)

1. Parse the carrier's form payload; the call identifier is required, a missing one is answered with a spoken failure TwiML and a log line naming the field.
2. Compose the room the platform should make: `sip.provider="daily"`, `sip_mode="dial-in"`, one endpoint, display name from the caller, video off, dial-out enabled only when the package declares outbound or a cold transfer, optional geography when the knob is set.
3. Start the agent: `POST /v1/public/{agent}/start` with the public key as bearer, `createDailyRoom: true`, those room properties in `dailyRoomProperties`, and a body of `direction: "inbound"` and `call_sid`. The room and its SIP address are not in the body: the platform hands the agent the room, and Daily tells the agent the address when it goes live.
4. Answer the webhook with TwiML that keeps the caller hearing something: the hold audio URL on a loop when that value is set, otherwise a looped spoken line (research D4). Never a bare pause (research R3), never silence, and never a third-party asset URL baked into the default.
5. On any failure in 2 or 3: answer with TwiML that speaks a short failure line and hangs up, and log the named cause. The caller must never be parked on hold for a call no agent will join.

The helper never forwards the call itself: the forward happens in the bot on the ready event, exactly once (research D3, R11).

### POST /outbound (the trigger)

1. Require the bearer token; refuse without it.
2. Body: `{"to": "+E164"}`. Validate shape; name the field on refusal.
3. Create a dial-out enabled room (no SIP dial-in config needed), start the agent with `direction: "outbound"` and the composed `dialout.sip_uri` (`sip:{to}@{sip_address}`).
4. Respond with the session id and room URL as JSON. Failures return the platform's error text, never a silent 200.

Rendered only when the package declares outbound; a package without outbound gets a helper without this endpoint.

### GET /healthz

Returns 200 when the process is up. Exists so the runbook's troubleshooting can separate "helper down" from "helper up, flow broken".

## What the helper must never do

- Touch carrier configuration (webhooks, trunks, numbers). The one carrier interaction in this feature is the bot moving a live call the carrier just delivered, which is call control rather than provisioning and so sits inside the telephony boundary as written.
- Persist anything. No state survives a request; the forward-once guard lives in the bot process, whose life is the call.
- Read the model or transfer destination env values; those belong to the agent.

## Contract test

An L1 test over the rendered helper asserts: emitted exactly when a carrier is declared; the startup check names every required env; the outbound endpoint renders only with outbound declared; the TwiML answer loops audio or speech rather than pausing, and carries no third-party asset URL; no secret-looking literal appears; and the file lands in the compile report's generated files. The Python itself is covered by the examples ruff lint like every other emitted Python file.

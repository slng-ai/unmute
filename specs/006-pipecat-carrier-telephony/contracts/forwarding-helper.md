# Contract: the call-forwarding helper

**Feature**: [../spec.md](../spec.md) | **Date**: 2026-08-12

`telephony_helper.py` is an emitted artifact, present in the build exactly when the target declares a carrier. It is the operator-run bridge between the carrier's webhook and the platform's start API. It is carrier-neutral in structure; the only carrier-specific piece in the whole feature is the forwarding action in the bot (research D3), not here.

## Runtime shape

- Run from the build directory: `uv run telephony_helper.py`. One process, one port.
- Startup: read every needed env name; if any value is missing, print the missing names, one line each, and exit non-zero. Never start half-configured (spec FR-007).
- No secret value is ever logged or echoed; log lines name env vars by name only.

## Environment it reads

Exact names and the split with the agent live in [environment.md](environment.md). By role: the Daily API key (mint rooms), the platform's public API key (start the agent), the outbound trigger token, the `sip_address` value (compose outbound SIP URIs), and two optional knobs: the hold audio URL and the Daily room geography (research D4, D7). The agent name is baked in at compile time; it is not an env value.

## Endpoints

### POST /call (the carrier webhook, inbound)

1. Parse the carrier's form payload; the call identifier is required, a missing one is answered with a spoken failure TwiML and a log line naming the field.
2. Create the interconnect room: `sip_provider="daily"`, display name from the caller, dial-out enabled only when the package declares outbound or a cold transfer, room expiry set so an abandoned room dies on its own, optional geography when the knob is set. Capture the room URL, token, and SIP endpoint (research F1, D2).
3. Start the agent: `POST /v1/public/{agent}/start` with the public key as bearer, `createDailyRoom: false`, and the body from [../data-model.md](../data-model.md) section 4 (`direction: "inbound"`, `call_sid`, `sip_uri`).
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

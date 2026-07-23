# SPEC — Pipecat Twilio dial-out (outbound) at the core

Source brief: "implement the dial-out that got excluded" for the Pipecat Twilio
carrier-websocket route, following the Pipecat Twilio WebSocket dial-out docs
(https://docs.pipecat.ai/pipecat/telephony/twilio-websockets#dial-out).
Schema truth: [SCHEMA.md](docs/SCHEMA.md). Telephony design: [docs/TELEPHONY.md](docs/TELEPHONY.md).

Ground truth found while scoping (do not re-litigate): the generated outbound
path already exists. `internal/target/telephony.go` gives the Pipecat
carrier-websocket route the `outbound` feature, the `/telephony/outbound`
endpoint rule, and the `UNMUTE_OUTBOUND_TOKEN` runtime env. The template
`telephony_twilio.py.tmpl` emits `POST /telephony/outbound` (gated on
`.Telephony.HasOutbound`) that Bearer-auths on `UNMUTE_OUTBOUND_TOKEN`, calls
`_remember("outbound", ...)`, and runs `client.calls.create(to, from_,
twiml=<Connect><Stream wss://…/telephony/ws/{token}>>, status_callback=…)` —
the exact docs pattern. The media WebSocket reuses dial-in `handle_media`. This
spec adds no second WS path and no serializer.

## §G goal

A Pipecat carrier-websocket agent whose `channels.phone` direction includes
outbound compiles a working dial-out path, and `unmute dev --telephony --to
<E.164>` places a real outbound call once the container is healthy. Direction
drives it: inbound-only is unchanged, outbound places a call, both do both.

## §C constraints

- C1: outbound is a direction, not a new transport. Reuse the emitted
  `POST /telephony/outbound`, `_outbound_request`, `_remember`, and shared
  `handle_media`. No new endpoint, WS path, or serializer in generated Python.
- C2: voicemail detection is optional, never a precondition of outbound. A
  carrier-websocket agent may set `outbound: true` with no `on_voicemail`.
  `on_voicemail` stays route-gated: Pipecat still errors when it is set,
  because the Pipecat driver denies voicemail (unchanged deny).
- C3: `UNMUTE_OUTBOUND_TOKEN` is a dev-supplied secret for local runs, like
  `UNMUTE_PUBLIC_URL`: `execDevTelephony` mints a random token, injects it into
  the container env before `up`, and reuses it to authorize the trigger. It is
  never demanded from `.env` and never printed. Production supplies its own.
- C4: the CLI places the call over loopback to the container's published bot
  port, not through the tunnel. The tunnel only carries the media WebSocket
  Twilio opens back to the bot.
- C5: the fail-closed gate is unchanged. Outbound is provisional like inbound;
  `UNMUTE_DEV_UNSAFE_TELEPHONY` lifts it for local dev. No new bypass, no
  promotion of the route.
- C6: no new Go dependency (stdlib `net/http` for the trigger POST) and no new
  Python dependency (the endpoint already exists).
- C7: L1–L3 need zero Python, zero network, zero Docker. A real placed call is
  L4 only, behind the dev flag and real credentials.

## §I surfaces

- I.schema: `channels.phone` with `outbound: true` and `on_voicemail` omitted is
  valid on a Pipecat carrier-websocket target. Setting `on_voicemail` on a
  Pipecat target stays a validation error (route denies voicemail). Routes that
  support voicemail (LiveKit SIP) keep accepting `on_voicemail` as before.
- I.flag: `unmute dev --telephony --to <E.164>` places one outbound call to
  `<E.164>` after `up --wait` reports healthy. `--to` requires `--telephony`
  and a resolved direction that includes outbound; the value is validated as
  E.164 before generate. On an inbound-only target `--to` is a clear error.
  Without `--to`, an outbound-capable target prints one line saying dial-out is
  available and how to place a call, and places nothing.
- I.trigger: the outbound call is placed from the existing `onReady` hook in
  `execDevTelephony`, after health and after the webhook/inbound line. The CLI
  POSTs `{"to":"<E.164>"}` with `Authorization: Bearer <UNMUTE_OUTBOUND_TOKEN>`
  to `http://127.0.0.1:<bot-port>/telephony/outbound`, prints the returned
  `call_id`, then follows logs as usual. A non-2xx or transport error tears the
  project down on the normal failure path.
- I.env: `execDevTelephony` mints `UNMUTE_OUTBOUND_TOKEN` (crypto-random) when
  the resolved direction includes outbound, injects it with the same
  `setChildEnv` used for `UNMUTE_PUBLIC_URL`, and removes it from the
  missing-credentials check (special-cased exactly like `UNMUTE_PUBLIC_URL`).
  It is NOT added to `DevSuppliedEnvironment`: that field drives the LiveKit SIP
  infra-first path and must stay LiveKit-only.
- I.endpoint: the generated `POST /telephony/outbound` (telephony_twilio.py) is
  unchanged by this spec. It is the dial-out server the docs describe.

## §V invariants

- V1: a Pipecat carrier-websocket target with `phone.inbound: true,
  outbound: true` and no `on_voicemail` passes validation and generation, and
  the artifact contains `@app.post("/telephony/outbound")`.
- V2: a Pipecat target with `on_voicemail` set still fails validation with the
  route-denies-voicemail error. Decoupling outbound from voicemail never
  enables voicemail on Pipecat.
- V3: `--to` requires `--telephony`; `--to` on a resolved direction without
  outbound errors before any child process; a `--to` value that is not E.164
  errors before generate.
- V4: when the resolved direction includes outbound and `--to` is set, the CLI
  supplies `UNMUTE_OUTBOUND_TOKEN` itself, never lists it in the
  missing-credentials error, and never writes it to stdout, stderr, or any
  emitted file.
- V5: the outbound trigger POST fires only after `up --wait` succeeds, targets
  `127.0.0.1:<bot-port>/telephony/outbound`, carries the Bearer token and a body
  of only `{"to": "<E.164>"}`, and on failure returns through the same teardown
  as any startup failure (project-scoped `down`, no `--volumes`).
- V6: inbound behavior is unchanged. An inbound-only target rejects `--to`,
  prints its dial-in number, and places no call.
- V7: `--telephony` stdout, stderr, and emitted artifacts for existing targets
  are byte-for-byte identical when `--to` is absent (regression guard on the
  compose command sequence and the printed lines).

## §T tasks

id|status|desc|cites
T1|x|validate: outbound no longer requires on_voicemail; on_voicemail still validated + route-gated when present. L1: pipecat outbound-no-voicemail validates; pipecat + on_voicemail still errors; livekit-sip outbound+voicemail still valid|I.schema,V1,V2
T2|x|dev env: mint UNMUTE_OUTBOUND_TOKEN in execDevTelephony when direction includes outbound, inject before up, special-case it out of the missing-credentials check like UNMUTE_PUBLIC_URL; do not touch DevSuppliedEnvironment. L2: token absent from .env still runs; token never printed|I.env,V4
T3|.|`--to <E.164>` flag on newDevCmd: E.164 validation, requires --telephony, requires outbound-capable resolved direction, rejects inbound-only with a clear message. L2 flag-guard tests|I.flag,V3,V6
T4|.|place the call from onReady: POST Bearer-authed {"to":…} to 127.0.0.1:<bot-port>/telephony/outbound, print call_id, teardown on failure. L2 with a fake bot HTTP server asserting method+path+Authorization+body; assert POST fires after readiness|I.trigger,V5
T5|.|regression: inbound-only --telephony output unchanged without --to; outbound-capable target without --to prints the availability line and places nothing. L2 golden-ish assertions on printed lines|V6,V7
T6|.|docs: outbound is a direction; `--to` local dial-out; voicemail optional; the CLI→loopback→Twilio→media-WS flow; update docs/user telephony + cli reference. No em dashes|I.flag,I.schema
T7|.|verify no golden drift for pipecat outbound generation (endpoint already emitted); regenerate only if env-name ordering changes; add L4 real outbound-call smoke behind the dev flag|V1,V7

## §B bugs

id|date|cause|fix

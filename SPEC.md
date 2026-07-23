# SPEC: LiveKit Twilio connector route (local telephony parity with Pipecat)

Source brief: "I want to test locally both pipecat and livekit in the same
way. No LiveKit Cloud dependency: only Docker and open source, and it must
also scale to production." Telephony design: [docs/TELEPHONY.md](docs/TELEPHONY.md).

Ground truth found while scoping (do not re-litigate):

- Cloudflared quick tunnels carry HTTPS/WSS to one local port, nothing else.
  LiveKit SIP needs SIP 5060 (UDP/TCP) and RTP UDP into the laptop, so no
  tunnel can ever make SIP inbound work locally. That route stays as the
  production trunk option; it is not the local-testing answer.
- LiveKit ships an official "Twilio Connector" (Twilio Media Streams over
  WebSocket instead of SIP), but the serving side is LiveKit Cloud only
  (BETA). Verified: `ConnectTwilioCall` exists in livekit/protocol, and no
  OSS service implements it (livekit/livekit pkg/service has no connector;
  there is no livekit/connector repo). We therefore build our own bridge,
  same mechanism, fully open source.
- The route key is already reserved in `internal/target/telephony.go`:
  `{LiveKit, "connector", "twilio"}`, today gated with no adapter, required
  env already `account_sid, auth_token, from_number` (pipecat-shaped).
- The Pipecat template already implements the Twilio side end to end
  (webhook signature validation, TwiML `<Connect><Stream>`, Media Streams
  WS protocol, outbound `calls.create`, status callback). The bridge reuses
  those exact protocol shapes.
- Verified in livekit/python-sdks: `rtc.AudioSource(sample_rate=8000,
  num_channels=1)` publishes 8 kHz audio and `rtc.AudioStream(track,
  sample_rate=8000, num_channels=1)` resamples received audio to 8 kHz. The
  SDK does the resampling; we only need mu-law encode/decode (pure Python
  lookup tables, ~30 lines; stdlib audioop is removed in Python 3.13, so no
  audioop).
- The CLI web-mode token minting (`mintLiveKitToken` + `lkRoomConfig`
  agents) already shows how a joining participant's token dispatches the
  agent. The bridge uses the same trick: no SIP trunks, no dispatch rules,
  no Redis.
- SIP dial-out API verified 2026-07-23 against docs.livekit.io
  /reference/agents/agent-dispatch-service-api: `POST
  <server>/twirp/livekit.AgentDispatchService/CreateDispatch` with JSON
  `{"agent_name","room","metadata"}`, Bearer JWT with video grant
  `{"roomAdmin":true,"room":<room>}`; the room is auto-created. Same Twirp
  shape as the existing `sipAdminClient`. Outbound SIP works from a laptop
  because the local stack initiates every connection; only inbound needs
  public SIP/RTP reachability.
- `UNMUTE_OUTBOUND_TOKEN` mint currently triggers on the outbound feature;
  the connector route lists the token like Pipecat, so making the mint
  condition data-driven (plan RequiredEnv contains it) serves both.

## §G goal

`unmute dev <pkg> --telephony --target <livekit-connector-instance>` works
exactly like the Pipecat run: same three Twilio env vars, same managed
cloudflared tunnel, same auto webhook, same `--to` dial-out. All local, all
open source, and the same generated container deploys to production behind
any public HTTPS origin with a self-hosted LiveKit server.

## §C constraints

- C1: no LiveKit Cloud API, SDK call, or URL anywhere in generated
  artifacts. The bridge speaks only Twilio Media Streams and livekit-rtc
  against the configured LIVEKIT_URL.
- C2: no new Python dependency beyond what the routes already use:
  aiohttp and livekit-rtc ship with livekit-agents; `twilio` is added for
  this route only (the Pipecat route already depends on it for the same
  jobs: outbound calls and webhook signatures).
- C3: no new Go dependency.
- C4: the Pipecat route is behaviorally unchanged. The LiveKit SIP route
  changes in exactly two ways: `LIVEKIT_SIP_URI` (consumed by nothing) is
  removed, and `--to` places a real call via agent dispatch instead of
  404ing against an endpoint that route never emits.
- C5: L1-L3 need zero Python, zero network, zero Docker. Bridge behavior is
  covered by golden files; real calls are manual/L4.
- C6: Twilio webhook signature validation is mandatory in the bridge, same
  as Pipecat (trust boundary, never simplified away). The outbound endpoint
  Bearer-auths on UNMUTE_OUTBOUND_TOKEN.
- C7: secrets (auth token, outbound token) never reach stdout, stderr, or
  emitted files.

## §I surfaces

- I.connection: a connection YAML with `transport: connector`,
  `carrier: twilio` and env keys `account_sid`, `auth_token`, `from_number`
  resolves to the connector route. Features: route_selected, inbound,
  outbound, hangup, and the non-stream variable sources. Voicemail and
  transfers stay unsupported (gated) on this route for now.
- I.env: user-supplied env is exactly the Pipecat set: TWILIO_ACCOUNT_SID,
  TWILIO_AUTH_TOKEN, TWILIO_PHONE_NUMBER, plus model keys. LIVEKIT_URL,
  LIVEKIT_API_KEY, LIVEKIT_API_SECRET are locally supplied by the generated
  Compose (devkey pair) and real env in production. UNMUTE_PUBLIC_URL and
  UNMUTE_OUTBOUND_TOKEN are dev-supplied like Pipecat.
- I.bridge: generated `telephony_bridge.py` (aiohttp, in the application
  container next to agent.py):
  - `POST /telephony/inbound`: validates the Twilio signature, returns TwiML
    `<Connect><Stream url="wss://<public>/telephony/ws/<token>">`.
  - `WS /telephony/ws/<token>`: speaks Twilio Media Streams (start, media,
    stop, mark, clear). Joins room `call-<CallSid>` via livekit-rtc with a
    token whose room config dispatches the agent (agent_name = target name,
    metadata carries direction, from/to numbers, call_start). Publishes
    caller audio (base64 mu-law 8k -> AudioSource(8000,1)) and returns agent
    audio (AudioStream(track, 8000, 1) -> mu-law -> base64 media frames).
  - `POST /telephony/outbound`: Bearer token, body `{"to", "call_start"}`,
    `client.calls.create` with TwiML pointing back at the same WS path,
    returns `call_id`. Same contract as Pipecat.
  - `POST /telephony/status/<token>`: status callback, logs call end.
  - `GET /`: health, port 8081 (unchanged health contract).
- I.agent: on the connector route agent.py reads call facts (direction,
  numbers, call_start) from the dispatch metadata the bridge wrote. It never
  calls create_sip_participant; the bridge places outbound calls.
- I.compose: `compose.telephony.yaml` for the connector route runs exactly
  two services: application (bridge + worker) and livekit_server (`--dev`,
  single node, no Redis, no SIP container).
- I.dev: `dev --telephony` on this route reuses the Pipecat machinery
  unchanged and data-driven: managed tunnel (PublicEndpoints non-empty),
  auto Twilio webhook (AutoWebhookEndpoint), call line, `--to` places the
  call via loopback POST with the minted token.
- I.sipdial: on a LiveKit SIP plan with `--to`, after the graph is healthy
  the CLI mints a roomAdmin JWT for the local devkey pair and POSTs
  `CreateDispatch` to `http://127.0.0.1:<UNMUTE_LIVEKIT_PORT|7880>` with
  agent_name = target name, room `call-<random>`, and metadata
  `{"direction":"outbound","phone_number":"<E.164>","call_start":{}}`. The
  worker then dials through the stored outbound trunk. Prints
  `calling <E.164> (room <room>, dispatch <id>)`. Failure returns through
  the normal teardown path.
- I.prod: same container behind any public HTTPS origin; set
  UNMUTE_PUBLIC_URL, UNMUTE_OUTBOUND_TOKEN, LIVEKIT_URL and key pair to the
  self-hosted server; point the Twilio number webhook at
  `/telephony/inbound`. README documents this.

## §V invariants

- V1: generated connector artifacts contain no `livekit.cloud`, no
  `ConnectTwilioCall`, and no SIP trunk or dispatch-rule setup. Grep-clean.
- V2: the bridge rejects webhook requests with a missing or wrong Twilio
  signature (403) before doing any work, byte-identical logic to the
  Pipecat template's validation.
- V3: `POST /telephony/outbound` without the exact Bearer token is 401; with
  it, exactly one `calls.create` fires and its TwiML `<Stream>` URL is the
  public WS endpoint. Empty `call_start` never errors (B1 class from the
  Pipecat spec: required-set arithmetic must hold when empty).
- V4: the dev run on a connector plan demands exactly TWILIO_ACCOUNT_SID,
  TWILIO_AUTH_TOKEN, TWILIO_PHONE_NUMBER plus model keys; never LIVEKIT_URL,
  the key pair, a trunk ID, or LIVEKIT_SIP_URI.
- V5: UNMUTE_OUTBOUND_TOKEN is minted exactly when the plan's RequiredEnv
  lists it (Pipecat outbound and connector outbound); a SIP plan run injects
  no token.
- V6: no artifact, route rule, doc, or test references LIVEKIT_SIP_URI.
- V7: `--to` on a LiveKit SIP plan sends exactly one CreateDispatch to the
  local server: path `/twirp/livekit.AgentDispatchService/CreateDispatch`,
  Bearer JWT signed by the devkey pair with a roomAdmin grant, agent_name =
  target name, metadata JSON with direction "outbound" and phone_number =
  the `--to` value. No POST to `/telephony/outbound` happens on a SIP plan,
  and SIP auth values appear in no printed line.
- V8: the mu-law tables round-trip: encode(decode(b)) == b for all 256 byte
  values (asserted in the emitted code's self-check and the L4 smoke).
- V9: Pipecat telephony stdout/stderr and artifacts are byte-for-byte
  unchanged (regression guard).
- V10: the generated local LiveKit stack uses the documented `livekit-server
  --dev` key pair `devkey`/`secret` consistently across the server, the app
  worker, LiveKit SIP, and every hand-minted admin/dispatch token. No
  artifact, compose file, or Go constant uses a secret the `--dev` server
  will not accept. (grep: no `devsecret-local-only`).

## §T tasks

id|status|desc|cites
T1|x|route table: un-gate {livekit,connector,twilio} with features (route_selected, inbound, outbound, hangup, non-stream sources), endpoints (inbound, ws, outbound, status), processes, runtime env (UNMUTE_PUBLIC_URL, UNMUTE_OUTBOUND_TOKEN, LIVEKIT_URL, LIVEKIT_API_KEY, LIVEKIT_API_SECRET with local-supplied set), AutoWebhookEndpoint. L1 route resolution + validate tests (voicemail/transfer on connector still errors)|I.connection,I.env,V4
T2|x|generate: connector flavor in livekit_v1: telephony_bridge.py.tmpl (signature validation, TwiML, Media Streams WS, mu-law tables, livekit-rtc room join with dispatching token, outbound calls.create, status, health), agent.py connector branch (metadata call facts, no create_sip_participant), pyproject adds twilio for this route, Dockerfile/entry runs bridge + worker, compose.telephony.yaml with application + livekit_server --dev only. Goldens for all|I.bridge,I.agent,I.compose,V1,V2,V3,V8
T3|.|dev wiring: confirm tunnel, auto webhook, call line, --to all fire data-driven on the connector plan; make the token mint condition RequiredEnv-driven. L2: connector plan run demands only the pipecat-shaped env; --to POSTs loopback with the token; pipecat run unchanged|I.dev,V4,V5,V9
T4|x|SIP route: remove LIVEKIT_SIP_URI everywhere (route rule, generate env list, templates, goldens, docs, tests); --to on a SIP plan places the call via CreateDispatch (mintLiveKitRoomAdminToken helper + placeLiveKitDispatch on the sipAdminClient Twirp pattern, branch in onReady by route transport, print room + dispatch id). L2 httptest: method, path, auth grant, agent_name, metadata, one call only|C4,I.sipdial,V6,V7
T5|.|telephony-hello: switch the livekit target to a twilio connector connection so one .env drives both targets; README update|I.connection,I.env
T6|.|docs: twilio-walkthrough (connector route section, SIP kept as the production trunk path), cli.md, TELEPHONY.md, tags-and-gating (connector un-gated), targets/livekit.md. Simple words, no em dashes|I.prod,V6
T7|.|full suite green; manual E2E with real creds: connector inbound call, connector outbound --to call (both through the tunnel), SIP outbound --to call (needs the Twilio SIP trunk values)|V1-V9

## §B bugs

id|date|cause|fix
B1|2026-07-23|`--to` on a LiveKit SIP target POSTed /telephony/outbound on the bot port, an endpoint that route never emits, so dial-out 404ed; dev also demanded LIVEKIT_SIP_URI which nothing consumes|V5,V6,V7
B2|2026-07-23|the generated LiveKit SIP telephony Compose set the app/SIP/admin-token secret to `devsecret-local-only`, but `livekit-server --dev` only accepts `devkey`/`secret` (docs.livekit.io/transport/self-hosting/local), so worker registration and every livekit.SIP Twirp admin call would 401. Never observed earlier because the phantom LIVEKIT_SIP_URI env error (B1) blocked startup before containers ran|V10

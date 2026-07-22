# SPEC — zero-step local telephony (managed tunnel, Twilio webhook, LiveKit trunks)

Scope: make `unmute dev ./agent --telephony` need zero manual steps once a
route is promoted. The user sets credentials in `.env` once; the CLI starts a
managed cloudflared tunnel (carrier WebSocket routes), configures the Twilio
voice webhook automatically (Pipecat Twilio), creates idempotent LiveKit
trunk and dispatch records against the local self-hosted stack (LiveKit SIP),
and prints "call +1XXXXXXXXXX, ctrl-c to stop". Everything stays open source:
no LiveKit Cloud, no Pipecat Cloud, no Daily. Nothing here promotes a route;
the fail-closed validation gate is byte-for-byte unchanged. Authoritative
design is the amended [TELEPHONY.md](docs/TELEPHONY.md) (this work amends it
in T1; doc wins). Schema truth is [SCHEMA.md](docs/SCHEMA.md) (N16, §4.9,
§6.3); compiler locks are [compiler.md](docs/spec/compiler.md) C13, V27-V31
(none are weakened). External API facts were verified 2026-07-22 against
cloudflare/cloudflared source, twilio.com API docs, and docs.livekit.io +
livekit/protocol (see §C.C2).

## §G goal
For a promoted route, `unmute dev <dir> --telephony` runs with no
`--public-url` and no manual carrier console step per run: it validates and
generates behind the unchanged gate, checks user credentials, starts
cloudflared as a managed child when the plan has public endpoints and no
`--public-url` (parsing the `https://<random>.trycloudflare.com` origin and
injecting it as `UNMUTE_PUBLIC_URL`), brings up the Compose graph (LiveKit
SIP: infra services first, then trunk/dispatch records created idempotently
over the local Twirp SIP API with the generated dev key pair, IDs injected as
`LIVEKIT_SIP_INBOUND_TRUNK`/`LIVEKIT_SIP_OUTBOUND_TRUNK`, then the
application), configures the Twilio number voice webhook via the REST API
(printing the previous URL), prints the call line, streams logs, and tears
down the tunnel and the project-scoped stack on every exit path. User docs
show every route end to end with real-schema YAML.

## §C constraints
- C1: **no new Go dependency.** Child processes (cloudflared, docker compose)
  plus stdlib HTTP/JSON/HMAC only. The LiveKit access token is a hand-rolled
  HS256 JWT (stdlib `crypto/hmac` + `encoding/base64` + `encoding/json`),
  claims `iss`=API key, `exp`, `nbf`, top-level `"sip": {"admin": true}`
  (verified against livekit/protocol `auth/grants.go` ClaimGrants/SIPGrant).
  Reuse the existing token minting in
  [livekit_token.go](internal/cli/livekit_token.go) if it fits; otherwise a
  local helper, never a dep.
- C2: **verified external facts only** (all checked 2026-07-22, recorded in
  the code where used):
  (a) cloudflared: `cloudflared tunnel --url http://127.0.0.1:<port>` runs a
  quick tunnel; the URL is printed to its log output (stderr) in a table line
  containing `https://<random>.trycloudflare.com` (source:
  cloudflare/cloudflared `cmd/cloudflared/tunnel/quick_tunnel.go`, LogTable
  prepends `https://` when missing). Parse by scanning child output lines for
  `https://[a-z0-9-]+\.trycloudflare\.com`.
  (b) Twilio: lookup
  `GET /2010-04-01/Accounts/{AccountSid}/IncomingPhoneNumbers.json?PhoneNumber=<E.164>`
  (exact match, Basic auth AccountSid:AuthToken; response array
  `incoming_phone_numbers` with `sid`, `voice_url`); update
  `POST /2010-04-01/Accounts/{AccountSid}/IncomingPhoneNumbers/{PNSid}.json`
  form-encoded `VoiceUrl` + `VoiceMethod=POST` (source:
  twilio.com/docs/phone-numbers/api/incomingphonenumber-resource).
  (c) LiveKit SIP Twirp: `POST <server>/twirp/livekit.SIP/<Method>`, JSON
  body, `Authorization: Bearer <JWT>`. Request wrappers from
  livekit/protocol `protobufs/livekit_sip.proto` (authoritative over the
  flattened docs example): CreateSIPInboundTrunk `{"trunk":{...}}`,
  CreateSIPOutboundTrunk `{"trunk":{...}}`, CreateSIPDispatchRule
  `{"dispatch_rule":{...}}` (field 10, non-deprecated), List* `{}` returning
  `{"items":[...]}`. Responses may use camelCase or snake_case field names;
  the parser accepts both. `destination_country` is Cloud region routing,
  not required self-hosted.
- C3: **the promotion gate is untouched.** `TelephonyRoutes()` evidence stays
  `Provisional`; `ir.Validate`
  ([validate.go:1011](internal/ir/validate.go:1011)) still rejects every
  telephony route with "route has not passed its credentialed smoke" before
  artifacts or Docker. The gate L2 test
  (`TestDevTelephonyReportsProvisionalRouteBeforeConfiguration`,
  [dev_test.go:321](internal/cli/dev_test.go:321)) passes unchanged. New
  machinery activates only after the gate, so it is exercised in L2 by
  driving the post-gate core with literal plans and seams, never by
  weakening validation.
- C4: **cloudflared is the only supported tunnel client** (Apache 2.0, no
  account, one parser, one failure mode). `--public-url` is the
  bring-your-own-tunnel escape hatch (ngrok included) and skips all tunnel
  management. Tunnel applies only to routes with non-empty
  `PublicEndpoints` (Pipecat carrier-websocket); LiveKit SIP needs public
  SIP/RTP, which an HTTPS tunnel cannot provide (document, do not fight).
- C5: **webhook auto-config is a carrier-definition fact, data not
  framework.** Only the Pipecat carrier-websocket Twilio route carries it in
  v1; Telnyx/Plivo keep printed manual steps until someone adds their fact
  plus implementation. Never buy numbers, never create carrier-side trunks.
- C6: **secrets never in generated files, logs, or key names.** Trunk auth
  (`sip_username`/`sip_password` values) goes only into the Twirp request
  body, never into emitted JSON, compose files, or printed output. Signature
  validation and webhook URLs derive only from `UNMUTE_PUBLIC_URL` plus plan
  endpoint paths.
- C7: **repo rules.** Go 1.24, `CGO_ENABLED=0`; cobra `RunE`, output via
  `cmd.OutOrStdout()`/`cmd.ErrOrStderr()`, errors wrapped `%w`, no
  `os.Exit` outside main. `go test ./...` green with zero Python and zero
  network; `make lint fmt test` before finishing. Simple language in docs,
  no em dashes.
- C8 (non-goals): no ngrok integration, no named/authenticated Cloudflare
  tunnels, no carrier number provisioning, no Telnyx/Plivo webhook
  automation, no production trunk automation (the emitted `sip-*.json` +
  `lk` manual path stays for production), no change to the Pipecat/LiveKit
  Python emitters' call behavior.

## §I surfaces
- I.dev: [dev.go:169](internal/cli/dev.go:169) `runDevTelephony`: keeps the
  gate (loadPackage → `generate.Generate`) and env checks, then delegates to
  a post-gate core function that L2 tests can drive with a literal
  `*generate.TelephonyRuntimePlan` + artifact dir + seams.
- I.compose: [dev_compose.go](internal/cli/dev_compose.go): existing seams
  `composeLookPath`/`composeCommand`/`composePreflight`;
  `runTelephonyCompose` gains an infra-first phase (up `--wait` the plan's
  services minus the application, run a between-phase hook that may extend
  env, then full up) used only when the plan asks for trunk automation.
- I.tunnel: new `internal/cli/dev_tunnel.go`: `tunnelLookPath`/`tunnelCommand`
  package seams (mirror compose seams), quick-tunnel child (`Setpgid`,
  `killGroup`/stop on every exit path), line scanner for the
  trycloudflare.com origin with a startup timeout.
- I.twilio: new `internal/cli/dev_twilio.go`: `twilioAPIBase` package seam
  (httptest in tests), lookup + update + previous-URL capture.
- I.lksip: new `internal/cli/dev_livekit_sip.go`: Twirp client (stdlib),
  HS256 token, idempotent ensure-functions for inbound trunk, outbound
  trunk, individual dispatch rule (`roomPrefix: call-`, agent name from the
  plan); returns IDs.
- I.fact: [telephony.go](internal/target/telephony.go): `TelephonyRoute`
  gains the auto-webhook fact (endpoint name; set only on
  pipecat/carrier-websocket/twilio) and a dev-supplied environment list
  (`LIVEKIT_SIP_INBOUND_TRUNK`/`LIVEKIT_SIP_OUTBOUND_TRUNK` on sip routes);
  [ir build.go:799](internal/ir/build.go:799) projects both, plus the
  Connection vocabulary→env-name map, into `ir.TelephonyPlan` and
  [generate/telephony.go](internal/generate/telephony.go)
  `TelephonyRuntimePlanFor` copies them into the runtime plan.
- I.doc: [TELEPHONY.md](docs/TELEPHONY.md) (amend: decision, non-goals,
  credentials table, 12-step local development), user docs
  [learn/07-phone-calls.md](docs/user/learn/07-phone-calls.md),
  [reference/targets-yaml.md](docs/user/reference/targets-yaml.md),
  [reference/providers.md](docs/user/reference/providers.md),
  [reference/channels-and-capacity.md](docs/user/reference/channels-and-capacity.md),
  [targets/pipecat.md](docs/user/targets/pipecat.md),
  [targets/livekit.md](docs/user/targets/livekit.md),
  [reference/cli.md](docs/user/reference/cli.md), example package
  [examples/telephony-multi-task](examples/telephony-multi-task).
- I.tests: L1 (tunnel URL parse, webhook URL derivation, trunk content
  match), L2 (missing cloudflared error, `--public-url` bypass, env
  injection, orchestration order via seams, gate unchanged), L3 (goldens for
  any generated-file change), L4 smoke (real cloudflared/Twilio/Docker,
  build tag `smoke`, opt-in).

## §V invariants
- V1: when the plan has public endpoints and `--public-url` is absent, the
  dev command spawns exactly one `cloudflared tunnel --url
  http://127.0.0.1:<port>` child, parses the first
  `https://<sub>.trycloudflare.com` origin from its output within a startup
  timeout, injects it as `UNMUTE_PUBLIC_URL` before Compose up, and kills
  the child's process group on every exit path (startup failure, compose
  failure, interrupt, normal stop). When `--public-url` is set, no tunnel
  process is ever spawned and the given origin is used unchanged. Guard: L2
  tests with a fake cloudflared script assert spawn/parse/inject and
  kill-on-every-path; a `--public-url` L2 test asserts the tunnel seam is
  never called.
- V2: a missing cloudflared binary fails before Compose with an error that
  names the install commands (macOS `brew install cloudflared`; Linux
  package or the cloudflare/cloudflared releases page) and offers
  `--public-url` as the bring-your-own-tunnel alternative. Guard: L2 test
  asserts all three substrings.
- V3: the Twilio voice webhook update runs on every start after the app is
  healthy, targets the `IncomingPhoneNumber` matched exactly by the
  configured number env value, sets `VoiceUrl` to `UNMUTE_PUBLIC_URL` + the
  plan's inbound endpoint path (no other URL source), and prints the
  previous `VoiceUrl` so the user can restore it. It never creates or buys
  carrier resources. Routes whose carrier definition lacks the fact keep
  printed manual steps. Guard: L1 URL-derivation test; L2 httptest asserting
  request method/path/params and previous-URL output; agreement test: a
  route carrying the fact must have a CLI implementation for its carrier.
- V4: LiveKit trunk/dispatch creation is idempotent and local-only: after
  the infra services are healthy, the dev command lists existing inbound
  trunks, outbound trunks, and dispatch rules on the local server (generated
  dev key pair) and reuses a content-identical record instead of creating a
  duplicate; otherwise it creates (inbound: configured number or wildcard;
  outbound: address=`sip_address` env value, numbers=`from_number` value,
  auth in request body only; dispatch: individual rule `roomPrefix: call-`
  dispatching the generated agent name, bound to the inbound trunk).
  Returned IDs are injected as `LIVEKIT_SIP_INBOUND_TRUNK` and
  `LIVEKIT_SIP_OUTBOUND_TRUNK` into the application environment before the
  application service starts; the user never supplies these two values for
  local dev (missing-env excludes them; a user-set non-empty value is
  rejected like other locally supplied names). Guard: L1 content-match
  tests (same record reused, changed record recreated); L2 httptest Twirp
  server asserting list-before-create, auth only in body, env injection,
  and second-run reuse.
- V5: fail-closed validation output for gated and provisional routes is
  byte-for-byte unchanged, and no tunnel, carrier, or Twirp call happens
  before the gate passes. Guard: the existing gate L2 test passes without
  modification; new orchestration is reachable only after
  `generate.Generate` succeeds.
- V6: no secret value appears in generated files, logs, printed output, or
  Redis/record key names; trunk auth is request-body only; the printed call
  line shows only the phone number. Env names come from the plan's
  Connection vocabulary map, never hardcoded per carrier in the CLI beyond
  the carrier fact itself. Guard: L2 output assertions (no token/password
  substrings); golden diffs show `${VAR}` placeholders only.
- V7: `go.mod` is unchanged by this feature (C1). Guard: review + CI.

## §T tasks
id|status|desc|cites
T1|x|Amend [TELEPHONY.md](docs/TELEPHONY.md): decision bullets (managed cloudflared quick tunnel for carrier WebSocket routes; automatic Twilio webhook config as a carrier fact; automatic local LiveKit trunk/dispatch records), non-goals rewrite (drop "no built-in tunnel client", keep no-provisioning scoped to carrier-side/production resources), credentials table (`LIVEKIT_SIP_INBOUND_TRUNK`/`LIVEKIT_SIP_OUTBOUND_TRUNK` become CLI-supplied for local dev; `UNMUTE_PUBLIC_URL` CLI-supplied when the managed tunnel runs), and the "Local development" section rewritten to the 12-step promoted-route order: 1 validate+generate, 2 resolve+print plan, 3 validate user env, 4 compose preflight, 5 start managed tunnel or take --public-url, 6 print exact public endpoint URLs, 7 up infra services (LiveKit SIP) or full graph, 8 create/reuse LiveKit trunk+dispatch records and inject IDs, 9 wait full-graph health and app readiness, 10 auto-configure carrier webhook where the fact allows (print previous URL), 11 print call line and stream logs, 12 teardown: compose down project-scoped (volumes preserved) and tunnel killed. Honest limits kept (SIP physics, Docker Desktop NAT)|C4,C5,C8,I.doc
T2|x|Carrier facts + plan plumbing: add auto-webhook fact and dev-supplied env list to `TelephonyRoute` ([telephony.go](internal/target/telephony.go)); set the fact on pipecat/carrier-websocket/twilio (endpoint `inbound`), dev-supplied `LIVEKIT_SIP_INBOUND_TRUNK`/`LIVEKIT_SIP_OUTBOUND_TRUNK` on the three sip routes; project fact + dev-supplied list + Connection vocabulary→env map through `ir.TelephonyPlan` ([build.go:799](internal/ir/build.go:799)) into `generate.TelephonyRuntimePlan`; `externalTelephonyEnv` subtracts dev-supplied names; `rejectLocalTopologyConflicts` covers them; compose passthrough for the two trunk vars stays. Update sip-route ManualSteps item 4 and twilio webhook manual step to say dev does this automatically and manual steps remain for production. L1 tests + regenerate affected goldens/compile-report fixtures|I.fact,V4,V6
T3|x|Managed tunnel: `internal/cli/dev_tunnel.go` with `tunnelLookPath`/`tunnelCommand` seams, missing-binary error (V2), quick-tunnel child (Setpgid, output to telephony log, line-scan regex `https://[a-z0-9-]+\.trycloudflare\.com`, startup timeout, kill on every exit), returning origin + stop func. L1 parse tests; L2 fake-script tests for V1/V2|I.tunnel,V1,V2,C2,C4
T4|x|Post-gate core restructure: extract the orchestration after the gate in `runDevTelephony` into a core that takes plan/artifact/env/seams; wire order per T1 steps 3-12; `runTelephonyCompose` infra-first phase + between-phase hook (services = plan services minus application); print call line from the `from_number` env value via the vocabulary map. Gate test unchanged (V5); L2 order/injection tests with fake compose+tunnel|I.dev,I.compose,V1,V5
T5|x|Twilio webhook auto-config: `internal/cli/dev_twilio.go` (lookup by exact number, update VoiceUrl+VoiceMethod=POST, capture+print previous URL, Basic auth from mapped env names, `twilioAPIBase` seam, short timeouts); called only when the plan carries the fact and endpoints are public; failure is a clear error (not silent). L1 URL derivation; L2 httptest; agreement test fact⇒implementation|I.twilio,V3,C2,C5
T6|x|LiveKit trunk automation: `internal/cli/dev_livekit_sip.go` (HS256 sip-admin JWT, Twirp POST helper, list/ensure inbound trunk, outbound trunk, dispatch rule with content-identity matching per V4, camelCase+snake_case tolerant parsing); admin URL from `UNMUTE_LIVEKIT_PORT` default 7880 against 127.0.0.1 with the generated dev key pair; IDs injected via the T4 between-phase hook. L1 match tests; L2 httptest Twirp server incl. second-run reuse|I.lksip,V4,C1,C2,C6
T7|x|User docs + example: rewrite [learn/07-phone-calls.md](docs/user/learn/07-phone-calls.md) zero-step walkthrough (what dev does, cloudflared install line, one-time Twilio console setup per model, printed output meaning, honest limits); per-route full YAML in reference/targets pages (agent.yaml telephony channel inbound/outbound/both, call_start variables, transfer destinations; connections per carrier vocabulary for both models; targets.yaml per route; .env example per route with CLI-supplied values noted); [reference/providers.md](docs/user/reference/providers.md) pointer stays carrier-free; make [examples/telephony-multi-task](examples/telephony-multi-task) a real telephony package (channel+connection+target+.env.example+README) that fails closed with today's gate note; sidebar entry check. Derive every field from SCHEMA.md; simple language, no em dashes|I.doc,C7,C8
T8|.|Gates + hygiene: verify fail-closed byte-identical (run gate tests), `go test ./...` zero Python/network, `make lint fmt test`, goldens regenerated where files changed, no go.mod diff (V7). L4 smoke additions only where real cloudflared/Twilio/Docker are needed (build tag smoke)|V5,V7,C3,C7

Dependency order: T1 first (doc wins; the 12-step order drives T4). T2 before
T4/T5/T6 (plan fields). T3 parallel to T2. T4 after T2+T3. T5/T6 after T4.
T7 after T1 (and after T2 for exact env names). T8 last.

## §B bugs
id|date|cause|fix
B1|2026-07-22|Pre-existing at branch start: commit 5e21577 added `examples/telephony-multi-task` (a copy of multi-task with no telephony channel, connection, or route) without registering it in `TestPublicExamplePackages` (internal/generate/examples_test.go:113), so the default suite was red before this work began. No new invariant: that registry test is the guard and it fired. Fix folded into T7, which makes the example a real telephony package and registers it|T7

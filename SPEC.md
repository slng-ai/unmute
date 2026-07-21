# SPEC — provider-neutral telephony

Source plan: [TELEPHONY.md](TELEPHONY.md). Schema truth:
[SCHEMA.md](SCHEMA.md). Compiler core:
[docs/spec/compiler.md](docs/spec/compiler.md). This spec includes the
pressure-test corrections required before implementation. Orchestrator scope:
LiveKit and Pipecat only. Carrier scope remains Twilio, Telnyx, Plivo, and
Exotel.

## §G goal

Compile portable telephony intent once into a validated route plan, then emit
only the selected LiveKit or Pipecat route and carrier code. Add a carrier or
transport without changing Agent intent, shared pipeline logic, or CLI
dispatch.

## §C constraints

These constraints keep the common seam small and make unsupported combinations
fail before generation.

- C1: Go is the maintained implementation. Python exists only in embedded
  `text/template` files. No maintained Python package.
- C2: Unmute owns intent, resolution, validation, and generated adapters.
  Pipecat serializers and LiveKit SIP/Connector own media. No universal audio
  gateway, codec layer, or serializer rewrite.
- C3: `SCHEMA.md` must adopt Connections, target binding, and explicit system
  variable sources before runtime work. Docs win over code.
- C4: Telephony support resolves by exact `(orchestrator, transport,
  carrier, feature)`. Provider-only, carrier-only, and broad fallback matches
  are invalid.
- C5: Inbound, outbound, each control, each briefing mode, and each call-context
  source are separate features. Missing, unknown, provisional, or unproved
  tuple -> validation error before generation.
- C6: One static telephony Connection per target in v1. All telephony channels
  on that target share it. Multiple accounts, dynamic tenant trunks, and
  per-channel bindings stay gated until a real package needs them.
- C7: Target `carrier` remains the only carrier declaration. Connection files
  contain environment-variable names, not duplicate carrier identity or secret
  values. Route + carrier validate required and unknown environment keys.
- C8: `internal/target` remains the only capability rulebook. Route facts,
  carrier facts, evidence, validation, and emitter agreement cannot drift into
  separate matrices.
- C9: Add Twilio directly. Extract shared carrier rendering only after Telnyx
  proves duplication. Use generator `switch` statements, not runtime plugin
  registries, base classes, or single-implementation interfaces.
- C10: Generated application files are byte-identical locally and in
  deployment. Route-derived local infrastructure helpers may be emitted beside
  them, but never change application behavior. Pipecat carrier WebSockets add
  an explicit public HTTPS base URL and external tunnel. LiveKit SIP adds
  public SIP and RTP reachability. Unmute installs neither and provisions no
  carrier resources.
- C11: Every public carrier request and WebSocket upgrade is authenticated
  before call state or media is accepted. Signature validation uses the
  configured public URL and unmodified request data. Never trust forwarded host
  headers to choose the signed URL.
- C12: Outbound triggers use separate application auth. Phone/SIP destinations
  validate at ingress; model-invoked transfers resolve authored symbolic
  destinations only. Secrets, raw webhook bodies, phone numbers, and call-start
  values stay out of normal logs and URL query strings.
- C13: Every v1 LiveKit or Pipecat telephony route resolves coordination
  `shared` and includes Redis. Every emitted plan names at least one closed
  coordination reason whose defined consumer is a declared service; Redis is
  never an idle sidecar. LiveKit Server and LiveKit SIP use it as platform
  infrastructure. Generated Pipecat code uses it only for telephony correlation,
  callback idempotency, human-transfer state, and admission. Agent handoff,
  tasks, transcripts, prompts, model context, and audio remain call-local.
  Sticky routing is not a correctness mechanism.
- C14: One Pipecat carrier call owns one long-lived WebSocket; one LiveKit call
  owns one SIP/Connector participant and one dispatched job. Scale on active
  sessions and peak call-start rate; drain active sessions during shutdown.
- C15: Official current docs plus a route smoke are required before enabling a
  feature. A carrier catalogue row without emitted behavior remains gated.
- C16: L1-L3 use Go only and no network. Credentialed carrier and self-hosted
  infrastructure smokes are L4, opt-in, and outside the default PR gate.
- C17: `max_sessions` is a runtime admission limit, not report-only metadata.
  No unbounded internal queue. At capacity, reject through the route's native
  busy/error path before allocating an Agent session.
- C18: Outbound start is one call per request. Campaign scheduling, consent
  records, number provisioning, and jurisdiction-specific calling policy remain
  outside Unmute v1.
- C19: Docker Compose is the required local executor for
  `unmute dev --telephony`, not a production deployment model. Every telephony
  graph contains version-pinned Redis. This is Unmute control infrastructure on
  Pipecat and LiveKit platform infrastructure on SIP, not a claim that the
  Pipecat framework itself requires Redis. Non-telephony `unmute dev` behavior
  is unchanged.

## §I surfaces

These are the only new authoring, compiler, artifact, and runtime surfaces.

- I.connection: `connections/<name>.yaml` strictly decodes to
  `Connection{Kind:"telephony", Environment map[string]string}`. Map keys are
  route vocabulary; values are environment-variable names. No carrier field,
  URL credentials, or secret values.
- I.target: a telephony target requires `transport`, `carrier`, and
  `connection`. Example route identities are
  `pipecat/carrier-websocket/twilio`, `livekit/sip/twilio`, and
  `livekit/twilio-connector/twilio`. Other orchestrators are out of scope.
- I.variable: `Variable.source` adds explicit system sources:
  `session_id`, `carrier`, `connection`, `call_id`, `stream_id`, `direction`,
  `from_number`, and `to_number`. `call_start` remains caller/dispatcher input.
  Variable names never imply their source.
- I.capacity: telephony adds required positive
  `capacity.peak_starts_per_second`. Reports combine it with peak sessions and
  measured startup time to size warm workers and surface carrier/framework
  concurrency or call-rate ceilings. Unknown ceilings remain `unbenchmarked`.
- I.route: `internal/target` resolves
  `TelephonyKey{Provider, Transport, Carrier, Feature}` to
  `core|warn|gated|provisional`, note, docs URL, verification date, and smoke
  status. `Feature` includes `inbound`, `outbound`, controls, briefing modes,
  and system sources.
- I.plan: `ir.Build` produces one `TelephonyPlan` per selected target with
  channel directions, connection, exact route, required features, resolved
  destinations, required environment names, system-source mappings, public
  endpoints, processes, manual steps, Compose application/dependency services,
  admission owner, `coordination: shared`, and closed coordination reasons:
  `livekit_control_plane`, `call_correlation`, `callback_idempotency`,
  `human_transfer`, and `admission`. Validation and generation consume this
  value; neither re-derives it. Each reason maps deterministically to a declared
  consuming service in the exact route graph.
- I.context: generated adapters normalize `session_id`, `carrier`,
  `connection`, `call_id`, optional `stream_id`, `direction`, `from_number`, and
  `to_number`. `session_id` is Unmute-stable across transfer legs; `call_id` is
  the route-native call-leg ID. Provider-private leg/conference IDs remain
  internal.
- I.outbound: generated code exposes an authenticated start-call operation with
  destination plus `call_start` variables. It returns `session_id`, provider
  call ID when available, and accepted status. Context crosses carrier hops by
  opaque one-use correlation token; raw values never ride in stream URLs.
- I.artifact: `Artifact.Telephony` carries provider-neutral processes,
  commands, health/readiness addresses, public endpoint paths, required
  environment names, manual steps, Compose application/dependency services,
  `coordination: shared`, and applicable closed coordination reasons. CLI code
  does not branch on carrier names.
- I.report: `compile-report.json` includes route, features, environment names,
  endpoints, manual steps, Compose service names, evidence dates,
  `coordination: shared`, and applicable coordination reasons with their
  consuming service names. It contains no environment values or raw phone
  numbers.
- I.dev: `unmute dev <agent-dir> --target <name> --telephony` compiles,
  validates environment names, starts the artifact plan, waits for readiness,
  prints exact carrier URLs/manual steps, streams logs under existing
  `--verbose`, and stops the process group on interruption. `--public-url` is
  required only for routes with public HTTP or WSS endpoints.
- I.local: `unmute dev <agent-dir> --target <name> --telephony` always runs the
  emitted `compose.telephony.yaml`, which contains the generated application
  service plus the exact route's dependencies. It builds or updates the stack,
  waits for every declared service health check, streams Compose logs, and
  stops its project-scoped stack without removing data volumes. There is no
  local-infrastructure flag or native-process telephony dev path in v1.
- I.pipecat: carrier WebSocket routes reuse Pipecat's runner parser,
  transport, and selected serializer. Generated code owns verified webhooks,
  outbound control, normalization, and out-of-band call control only. Compose
  always runs the generated Pipecat application and Redis. Redis backs only the
  bounded telephony control state named by V27.
- I.livekit: `sip` emits/documents self-hosted LiveKit Server, Redis, LiveKit
  SIP, trunks/dispatch inputs, and Agent worker requirements. Its Compose file
  runs the generated Agent, Redis, LiveKit Server, and LiveKit SIP. The Beta
  Twilio Connector is a distinct route and cannot inherit SIP capabilities or
  this service graph without exact route proof.

## §V invariants

These invariants are the executable pressure-test result.

- V1: strict load discovers only declared Connection files, rejects path
  escape/unknown fields, and reports file:line:col. A target reference to a
  missing Connection fails in Build.
- V2: a telephony target missing transport, carrier, or Connection fails. A
  target with multiple telephony channels passes only when one Connection and
  one route satisfy every channel.
- V3: each Connection environment value matches the environment-name grammar.
  Missing/unknown keys are checked against the exact route; secret values are
  never read during Load, Build, Validate, or Generate.
- V4: capability lookup requires an exact tuple. `twilio` support on one
  transport never enables another. Both mismatched carrier and mismatched
  transport fail; no boolean condition can pass when either differs.
- V5: `inbound: true` and `outbound: true` each require enabled features.
  Control support never implies direction support. `dtmf_receive` never implies
  `dtmf_send`.
- V6: validation-green telephony features equal emitted telephony features for
  every route. Agreement tests fail on missing code, extra code, missing docs
  URL/date, or missing smoke status.
- V7: output contains the selected carrier adapter and dependency only. No
  unselected carrier code, SDK, environment name, endpoint, or manual step.
- V8: adapters never parse or emit carrier audio frames themselves. Pipecat
  serializers and LiveKit media paths remain the only media implementations.
- V9: every requested system source is available before greeting/first model
  turn or the call ends with a stable error. Optional `stream_id` is unavailable
  on routes that cannot provide it and therefore fails validation when used.
- V10: custom `call_start` variables are required fields on outbound start-call
  input unless they have defaults. Inbound custom inputs require defaults in v1;
  carrier metadata cannot silently populate arbitrary Agent variables.
- V11: carrier HTTP signatures and WebSocket authentication are tested with
  valid, invalid, stale, replayed, and proxy-terminated requests. Validation
  uses exact configured public URL plus raw body/form data required by the
  carrier.
- V12: outbound auth is independent of carrier credentials. Opaque correlation
  tokens are unguessable, expire, are single-use, and disclose no call-start
  values.
- V13: every v1 LiveKit or Pipecat telephony plan reports coordination `shared`,
  includes Redis, and fails readiness when Redis is unavailable. No selected
  feature, carrier, single-replica topology, or sticky-routing promise removes
  that requirement. Reasons are non-empty; each maps to a declared service that
  receives the Redis connection. An unconsumed Redis service is invalid.
- V14: callback processing and carrier-control requests are idempotent by
  carrier event or call-leg identity. Retry only documented-safe operations;
  duplicate callbacks do not repeat transfer or hangup side effects.
- V15: warm transfer follows connected -> consulting -> briefing -> joined ->
  transferred. Decline, no-answer, timeout, and carrier failure restore the
  caller or end cleanly per proven route behavior. No route inherits another
  route's transfer result.
- V16: readiness turns false before shutdown; new calls are rejected; active
  calls drain up to configured maximum duration; forced termination is
  reported. WebSocket ingress timeouts exceed the maximum call duration.
- V17: local and deployed artifacts differ only by environment values and
  infrastructure. The same generated application handles the same route.
- V18: LiveKit and Pipecat have explicit route rows. Unsupported routes fail
  before generation. Adding a carrier or transport changes route data,
  selected rendering, tests, and docs only; Agent/channel/Connection schema
  stays unchanged.
- V19: compile report and normal logs redact secret values, caller numbers, raw
  webhook bodies, call-start values, and provider-private transfer IDs. Logs
  correlate on `session_id`; protected debug logging is explicit opt-in.
- V20: L1 tests cover route, environment, source, and coordination resolution;
  L2 covers CLI planning and shutdown; L3 locks each enabled route's selected
  files/report; L4 proves real inbound/outbound/hangup/transfer/auth behavior.
- V21: telephony capacity requires positive `peak_starts_per_second`. Reports
  show session concurrency and call-start rate separately; neither may be
  inferred from average duration or silently replaced by the other.
- V22: each route names the component that owns admission. `max_sessions`
  rejects excess work before Agent allocation through a route-native response;
  no route silently queues, overcommits, or treats the report as enforcement.
- V23: the Compose graph is resolved by exact route, never by an orchestrator
  or carrier boolean. Every telephony route resolves its generated application
  and Redis. `livekit/sip/*` additionally resolves LiveKit Server and LiveKit
  SIP. Carrier choice never removes Redis.
- V24: `unmute dev --telephony` fails before application readiness when Docker
  Compose is missing, a declared service is unhealthy, or local topology
  environment conflicts with explicit external LiveKit settings. Every
  successful invocation runs Compose; no flag enables or disables it.
- V25: the LiveKit SIP Compose file uses explicit image versions, a conspicuous
  non-production API key pair, one shared Redis, and the documented SIP and RTP
  ports. It builds the generated Agent without baking carrier or model
  credentials into the image. `REDIS_URL` configures LiveKit Server and LiveKit
  SIP; the Agent is not claimed to use Redis.
- V26: a credential-free L4 topology smoke proves Compose startup, Redis
  failure propagation for both orchestrators, LiveKit Server readiness, LiveKit
  SIP readiness, and clean restart. It does not promote a carrier route.
  Promotion still requires carrier credentials, public SIP/RTP or HTTP/WSS
  ingress as appropriate, and a real call smoke.
- V27: Pipecat Redis values are limited to opaque pending-call correlation with
  the minimum normalized call-start context, callback replay/idempotency
  markers, human-transfer state and short-lived locks, and admission counters.
  Every record has a bounded TTL; phone numbers and call-start values never
  appear in Redis key names or logs. Redis never stores credentials, raw webhook
  bodies, audio, transcripts, prompts, model context, task state, or
  agent-handoff state. Agent handoff stays inside the active Pipecat worker.
- V28: every generated carrier control request (answer, outbound create,
  hangup, transfer, status) sets an explicit short timeout, and inbound webhook
  handlers respond inside the carrier's documented retry window, deferring slow
  control work off the request path. Template tests assert the timeout exists.
- V29: on routes whose carrier signatures carry no timestamp, replay markers
  outlive every artifact a replayed request can mint (correlation tokens,
  admission slots, media upgrades). A captured inbound request replayed after
  any TTL never yields a usable media connection.
- V30: admission is exact. A route accepts exactly `max_sessions` concurrent
  calls, boundary-tested at 1 and at the limit. An admitted slot stays held
  while its call is alive (liveness refresh or a TTL that bounds real call
  duration), and worker load thresholds never shrink effective capacity below
  the declared limit.

## §T tasks

Tasks prove the shared seam before copying carrier code.

id|status|desc|cites
T1|x|amend `SCHEMA.md`, `CONTEXT.md`, `TELEPHONY.md`, compiler spec: Connection ownership, target binding, explicit system sources, telephony start rate, exact route tuple, coordination rule|C3,C7,C13,I.connection,I.variable,I.capacity
T2|x|strict Connection load + derived authoring/IR schemas + target reference resolution|V1,V2,V3
T3|x|resolved `TelephonyPlan`; exact route capability/evidence table; gate inbound, outbound, controls, briefing, sources; fix tuple-condition tests|I.route,I.plan,V4,V5,V6,V18
T4|x|provider-neutral artifact/runtime plan + compile report; CLI consumes plan without carrier branches|I.artifact,I.report,V7,V19
T5|~|Pipecat + Twilio inbound/outbound, normalized context, hangup, cold transfer, auth, one-use outbound correlation; goldens + smokes|I.pipecat,V7-V14,V20
T6|~|`unmute dev --telephony --public-url`; initial readiness, exact setup output, and process-group shutdown exist; Compose execution remains T15|I.dev,V11,V16,V17
T7|~|Twilio warm transfer state machine on the shared Redis control store; duplicate and failure-path smokes|V13-V15,V20,V27
T8|~|Telnyx route as second carrier; extract only demonstrated shared definition/rendering code; same contract smokes|C9,V4-V7,V18,V20
T9|~|Plivo route; keep Exotel gated until carrier-authenticated WebSocket ingress is proven, in addition to its custom-input/transfer limits|C11,V4-V7,V10,V18,V20
T10|~|LiveKit self-hosted SIP route: topology inputs, dispatch, context, inbound/outbound/voicemail/transfers; route smokes|I.livekit,V4-V7,V13-V18,V20
T11|x|LiveKit Twilio Connector spike as separate Beta route; enable only smoke-proven features; otherwise keep SIP alternative|I.livekit,V4-V6,V15,V18,V20
T13|x|production hardening: admission, shared coordination readiness, callback replay/idempotency, drain, capacity/call-arrival reporting, redaction tests|C11-C17,V11-V16,V19-V22
T14|x|extend the resolved/runtime telephony plan and report with closed coordination reasons and consumers; emit deterministic `compose.telephony.yaml`: every route gets its generated application + Redis, LiveKit SIP also gets LiveKit Server + LiveKit SIP; reject orphan Redis; L1-L3 agreement, TTL/redaction tests, goldens|C10,C13,C19,I.plan,I.artifact,I.report,I.local,V13,V17,V23,V25,V27
T15|x|make `unmute dev --telephony` always execute Compose; preflight, build/update, Redis/application health, log streaming, project-scoped lifecycle, conflict checks, credential-free Pipecat and LiveKit Redis-failure L4, and telephony docs|I.dev,I.local,V24-V27
T16|x|honor `HumanTransfer.Mode` end to end (warm never lowers to a cold operation); implement the V6 agreement test over `TelephonyRoutes` features x generators; reconcile `briefing.summary` route rows vs the Fields deny; make the connector row generate-consistent or drop it|V6,B1,B2
T17|x|one rulebook: a telephony channel requires a Connection (kill the legacy `Table.Controls` fallback path), carrier env vocabulary resolves only from `internal/target`, drop the phantom LiveKit `UNMUTE_OUTBOUND_TOKEN`, enforce `peak_starts_per_second` unconditionally|V2,V7,V21,C8,B3,B4,B13
T18|x|edge hardening: explicit timeouts on every carrier control call, Telnyx answer moved off the webhook request path, replay-proof no-timestamp carriers, cold-transfer failure preservation or a documented limitation in generated docs|V11,V14,V28,V29,B5,B6
T19|x|capacity exactness: fix LiveKit `load_threshold` off-by-one, admission slot liveness refresh for calls without `max_duration`, boundary tests at `max_sessions` 1 and N|V22,V30,B7,B8
T20|x|lifecycle: real drain (readiness flips before stop, Compose `stop_grace_period`, forced-termination report), emitted log level and redaction configuration, clean ctrl-c exit codes, configurable host ports so two local stacks coexist|V16,V19,V24,B9,B10
T21|x|move runtime-plan facts (endpoints, manual steps, processes, local env defaults) onto the ir plan or carrier definition so generate and the CLI stop re-deriving them; extract the triplicated adapter template helpers per C9; deepen telephony L4 beyond `py_compile`|C8,C9,I.plan,V20,B11
T22|x|truth pass: align TELEPHONY.md current-state and user docs with the fail-closed reality, run the credential preflight after validation, document or bootstrap the LiveKit SIP local trunk-ID ordering, and track this spec in git (untracked today while tests cite V11/V17/V24-V26)|C15,V17,B12

## §B bugs

Recorded 2026-07-21 from the three-track PR #30 review (compiler, emitted
Python, CLI/docs). Every row cites the invariant it violates and the task that
fixes it.

id|date|cause|fix
B1|2026-07-21|pipecat generator ignores `HumanTransfer.Mode`; an authored warm transfer lowers to an emitted cold transfer, masked only by the provisional gate|V6,T16
B2|2026-07-21|the V6 agreement test was never implemented: generator tests iterate legacy `table.Fields` only, so route features (pipecat warm transfer, `briefing.summary`, livekit connector) validate green with no emitter behind them|V6,T16
B3|2026-07-21|a telephony channel without `connection:` bypasses `TelephonyPlan` entirely and resolves via legacy `Table.Controls`; it compiles green with zero telephony runtime emitted, so two rulebooks answer the same question differently|V2,T17
B4|2026-07-21|LiveKit SIP runtime plans demand `UNMUTE_OUTBOUND_TOKEN`, which no LiveKit template reads; a promoted route would refuse to start over an unused credential|V7,T17
B5|2026-07-21|carrier control calls in all three pipecat adapters run without timeouts (twilio client unbounded in `to_thread`, telnyx aiohttp default 300s, plivo requests none)|V28,T18
B6|2026-07-21|Plivo V3 signatures carry no timestamp and the nonce marker expires at 300s, so a captured inbound request replays later and mints a fresh media-WS token, burning admission slots and model spend|V11,V29,T18
B7|2026-07-21|LiveKit `load_threshold=(M-1)/M` marks the worker full at M-1 jobs; `max_sessions: 1` compiles but dispatches zero calls|V30,T19
B8|2026-07-21|the admission zset slot expires mid-call when `conversation.max_duration` is unset (fixed 360s TTL, never refreshed), so long calls overcommit the cap and their later release is a no-op|V22,V30,T19
B9|2026-07-21|the drain flag is set in a uvicorn lifespan shutdown hook no request can ever observe, no Compose `stop_grace_period` exists, and forced termination is unreported; LiveKit drains correctly, pipecat only nominally|V16,T20
B10|2026-07-21|pipecat's runner logs caller numbers at DEBUG via loguru and the generated app configures no log level or redaction, defeating the redaction rule end to end|V19,T20
B11|2026-07-21|endpoints, manual steps, and required env are re-derived per carrier in `generate.TelephonyRuntimePlanFor` instead of riding the ir plan; the CLI infers local topology env from the `livekit_server` service name; both generators re-hardcode the env vocabulary beside the rulebook|I.plan,C8,T21
B12|2026-07-21|TELEPHONY.md "current repository state" and three user-doc how-tos present a runnable `dev --telephony` flow while every route is provisional, so no public command can emit or run any telephony artifact; the documented "run the emitted Compose file directly" escape hatch is unobtainable|C15,T22
B13|2026-07-21|`peak_starts_per_second` is enforced only when a Connection is bound, narrower than SCHEMA §4.10's unconditional telephony requirement|V21,T17

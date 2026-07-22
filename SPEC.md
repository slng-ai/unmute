# SPEC — containerized `unmute dev` entry points + one web UI

Source brief: the feature request "Two containerized entry points plus console".
Schema truth: [SCHEMA.md](docs/SCHEMA.md). Telephony spec (untouched here):
[docs/SPEC.md](docs/SPEC.md). LiveKit containerized facts verified 2026-07-22
against LiveKit self-hosting docs and `config-sample.yaml` (see §I.livekit).

## §G goal

`unmute dev <dir>` runs the same Docker image production deploys, locally,
through one SLNG-branded web UI over WebRTC, identically for Pipecat and
LiveKit targets. Local (Docker) mode is the new default. `--telephony` is
byte-for-byte unchanged. `--console` stays native on the host mic and speaker.
The old host-uv web path is deleted, not hidden behind a flag: local testing
always runs the deployable container.

## §C constraints

- C1: exactly three modes behind one command. Default = local Docker mode.
  `--telephony` = phone calls, already containerized, unchanged. `--console` =
  terminal over host audio, stays native uv. `--console --telephony` is
  rejected before any work.
- C2: WebRTC stays the browser transport for both targets. No new WebSocket
  browser audio path is invented; both clients already use WebRTC.
- C3: one embedded web client in `internal/web`. One host bootstrap endpoint
  returns the transport kind and its parameters. The page's only per-target
  code is the transport adapter behind that contract.
- C4: no new Go dependency (child processes + stdlib) and no new JS dependency
  in `internal/web`. Reuse the compose exec seams, do not add new ones without
  need.
- C5: `compose.dev.yaml` uses pinned images with explicit versions, env passed
  by name from the host (same `.env` handling and `devChildEnv`), a healthcheck
  per service, and no secret values in the file.
- C6: Pipecat dev compose has one `application` service built from the
  generated `Dockerfile`, command overridden to the web entry
  `python bot.py --host 0.0.0.0 --port 7860`, host port from `--bot-port`. No
  Valkey sidecar: the web path does not use the coordination store and the
  no-idle-sidecar rule applies.
- C7: LiveKit dev compose has `livekit_server` (pinned `livekit/livekit-server`
  in `--dev` single-node mode, no external store) plus the `application` worker
  built from the `Dockerfile` with command `python agent.py dev` and
  `LIVEKIT_URL=ws://livekit_server:7880` inside the network. No Valkey.
- C8: the breaking change is loud. `unmute dev` now requires Docker. A missing
  Docker or Compose plugin fails with an install message as helpful as the
  cloudflared one, naming Docker Desktop or Docker Engine plus the Compose
  plugin.
- C9: the telephony fail-closed validation, `execDevTelephony`,
  `runTelephonyCompose`, and the route gate are untouched. Local mode never
  touches telephony plans, carrier code, or the tunnel.
- C10: L1 to L3 need zero Python, zero network, zero Docker. Real Docker and a
  real browser are L4 only (`make smoke`, build tag `smoke`), outside the PR
  gate.

## §I surfaces

- I.compose: `generate.Generate` emits `compose.dev.yaml` for both code targets
  beside the existing project files. Deterministic, golden-locked. Pipecat: one
  `application` service. LiveKit: `livekit_server` + `application`. Built from
  the generated `Dockerfile` via `build:`, images pinned, env by name only,
  healthcheck per service.
- I.dev: `unmute dev <dir>` with no mode flag runs the default local runner:
  compose preflight (reuse `composePreflight` / `composeLookPath`), generate,
  `docker compose up --build --detach --wait` under a project-scoped name
  (reuse `composeProjectName`), then start the host dev server against the
  containerized services. Logs to `build/<target>/dev.log`, `--verbose`
  follows. Project-scoped `down` (no `--volumes`) on every exit path.
- I.session: `GET /api/session` on the host dev server returns the transport
  contract. Pipecat: `{"kind":"webrtc-offer","offerUrl":"/api/offer"}` (the
  offer is reverse-proxied to the containerized bot at `--bot-port`). LiveKit:
  `{"kind":"livekit","url":"ws://localhost:7880","token":"<jwt>"}` (token minted
  by the existing `livekit_token.go`). The handler is a plain `http.Handler` so
  L2 tests hit it with `httptest`.
- I.web: one SLNG-branded page in `internal/web` (embedded FS, `//go:embed`).
  Same layout, controls, and connection-status UX for both targets. One
  transport-adapter module switches on the `kind` from `/api/session`: Pipecat
  posts a WebRTC offer, LiveKit joins with the LiveKit JS SDK. No new JS
  framework, same asset approach.
- I.console: `--console` keeps the native uv path and host audio, unchanged
  except that it is now selected by an explicit flag rather than being a branch
  of the web path.
- I.livekit: containerized LiveKit dev facts (verified 2026-07-22). Single node
  needs no external store; Redis only enables distributed mode. `--dev` injects
  the `devkey`/`secret` placeholder keys. The browser needs three published
  ports: `7880/tcp` (API + WebSocket signaling), `7881/tcp` (ICE/TCP fallback),
  `7882/udp` (ICE/UDP mux, a single port; the default 50000-60000 range is
  impractical through Docker). Inside a container the server must bind
  `0.0.0.0`, and `--node-ip 127.0.0.1` is required so the SFU advertises a
  host-reachable ICE candidate instead of the container's internal IP. Command:
  `livekit-server --dev --bind 0.0.0.0 --node-ip 127.0.0.1 --udp-port 7882`.
  All four flags exist in the server CLI. Exact loopback-candidate behavior is
  proven at L4; L1 to L3 only lock the emitted compose shape.

## §V invariants

- V1: `unmute dev <dir>` with no flags runs Compose (build, up, wait) and then
  the host dev server. It never runs the host-uv bot path and never spawns
  `livekit-server` on the host. The removed host-uv web path and the
  server-spawn branch of `runLiveKitWeb` no longer exist in the tree.
- V2: `compose.dev.yaml` is emitted for both code targets, is byte-deterministic
  (golden), pins every image to an explicit version, passes env by name with no
  secret values in the file, gives every service a healthcheck, and the runner
  starts it with `--wait`.
- V3: the Pipecat `compose.dev.yaml` has exactly one `application` service built
  from the generated `Dockerfile`, command
  `python bot.py --host 0.0.0.0 --port 7860`, host port equal to `--bot-port`,
  and no Valkey or other coordination service.
- V4: the LiveKit `compose.dev.yaml` has `livekit_server` (pinned
  `livekit/livekit-server`, `--dev`, no external store) and an `application`
  worker (`python agent.py dev`, `LIVEKIT_URL=ws://livekit_server:7880`),
  publishes `7880/tcp`, `7881/tcp`, and `7882/udp`, sets a host-reachable
  `--node-ip`, and has no Valkey service.
- V5: default-mode teardown runs a project-scoped `docker compose down` on every
  exit path including interrupt, and never passes `--volumes`, so data volumes
  survive.
- V6: `GET /api/session` returns the webrtc-offer contract for Pipecat and the
  livekit contract for LiveKit, and the one page selects its transport adapter
  from `kind` with no other per-target branch.
- V7: `--console` runs the native uv path with host audio; `--console` combined
  with `--telephony` is rejected before generate or any child process.
- V8: a missing Docker binary or Compose plugin fails in preflight, before
  generate and before any container starts, with an error naming Docker Desktop
  or Docker Engine and the Compose plugin.
- V9: env passthrough reuses the existing `.env` load and `devChildEnv`.
  `compose.dev.yaml` lists env var names only; values come from the host
  environment at run time and are never written into the file.
- V10: `execDevTelephony`, `runTelephonyCompose`, the route gate, and the L2
  telephony gate test are unchanged. `--telephony` stdout, stderr, and emitted
  artifacts are byte-for-byte identical to before this work.
- V11: `go.mod` is unchanged and `internal/web` gains no JS dependency.

## §T tasks

id|status|desc|cites
T1|x|emit `compose.dev.yaml` for both code targets: pipecat `application` template, livekit `livekit_server`+`application` template, wire into `generate.Generate`; L1/L3 goldens with `-update`|I.compose,V2,V3,V4,V11
T2|x|make local Docker mode the default runner: preflight, generate, `up --build --detach --wait`, project-scoped `down` on every exit, logs to `build/<target>/dev.log`, `--verbose` follows; delete the host-uv web path and the `runLiveKitWeb` server-spawn branch, migrate token mint + readiness into the runner; missing-Docker install message|I.dev,I.console,V1,V5,V8,V9,V10
T3|x|standardize the web UI: `GET /api/session` bootstrap handler (httptest-able) + one SLNG-branded page whose only per-target code is the transport adapter; keep the pipecat offer reverse-proxy and livekit token mint|I.session,I.web,V6
T4|x|console routing: `--console` keeps native uv + host audio; `--console --telephony` stays rejected|I.console,V7
T5|x|gate preservation: telephony fail-closed path and its L2 gate test untouched; local mode isolated from telephony code|V10
T6|x|tests: L2 default-mode routing, docker-missing text, command sequences (`up --build --detach --wait`, logs, project-scoped `down`, no `--volumes`), env passthrough, session-endpoint contract, teardown on interrupt; L4 smoke (build tag) for real Docker/browser; full suite green with zero Python/network/Docker|V1-V11
T7|.|docs: `docs/user/reference/cli.md`, the learn-flow dev-mode pages, `docs/user/targets/{pipecat,livekit}.md`, and a going-live "local Compose to production" checklist per route; local mode is the deployable-image test step, Kubernetes is the same image with different manifests|I.dev,I.compose

## §B bugs

id|date|cause|fix

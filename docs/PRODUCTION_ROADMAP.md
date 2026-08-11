# Production roadmap: from prototype to state of the art deployment

Status: research and recommendations, August 11, 2026. Nothing in this
document is implemented yet. It records what Unmute should add so that a
deployed agent is a state of the art deployment on every platform we target,
and where we deliberately stop. Before any code, the chosen parts become a
kept spec under `docs/spec/`.

The bar: an agent deployed from an Unmute package should match or beat the
official production guidance of its platform (self-hosted LiveKit, self-hosted
Pipecat, and stay compatible with LiveKit Cloud and Pipecat Cloud), without
the author writing any infrastructure by hand.

Related documents: [DEPLOYMENT.md](DEPLOYMENT.md) records today's manual
self-hosted path. [ARCHITECTURE.md](ARCHITECTURE.md) holds the invariants this
roadmap must respect. [TELEPHONY.md](TELEPHONY.md) owns telephony details.

## What "state of the art" means here

Drawn from the production article in the sources and the official LiveKit and
Pipecat guidance. A deployed voice agent needs all of these:

- **Session-aware scaling.** Scale on active sessions, never on CPU alone.
  Voice workers are I/O bound: CPU stays low while the worker is full.
  Aggressive scale-up, slow scale-down, because draining takes as long as the
  longest call.
- **Drain on every shutdown.** Rollouts and scale-downs must let calls finish.
  Kubernetes defaults to a 30 second grace period; a voice agent needs the
  maximum call duration, often 10 minutes or more.
- **Ingress that survives long calls.** Load balancers kill idle WebSockets
  (AWS ALB defaults to 60 seconds). Timeouts must exceed the longest allowed
  call, with WebSocket upgrade support.
- **Admission control.** Reject sessions above declared capacity before any
  resources are allocated. Never queue a phone call.
- **Health split into liveness and readiness.** Readiness gates new sessions,
  liveness restarts the process. They are different questions.
- **Secrets from the environment, names declared, values never in artifacts.**
- **Observability from day one.** Per-session tracing, per-component latency
  (P95, not mean), worker load metrics a scaler can read, one session ID in
  every log line.
- **Graceful degradation.** Fallback providers for STT, LLM, and TTS.
- **Hardened images.** Non-root user, pinned versions, no secrets baked in.
- **Separate environments.** Staging and production never share a server.

## Where Unmute stands today

The lifecycle is `init → validate → compile → dev` and ends at a Docker image
plus dev Compose files whose own header says they are not a production model.
[DEPLOYMENT.md](DEPLOYMENT.md) is a manual for a human operator; none of it is
generated or checked by the tool.

The runtime half of the list above already exists in the generated code:

- Pipecat telephony has `/healthz` and `/readyz`, SIGTERM drain with a forced
  termination deadline, and Redis admission counters enforcing `max_sessions`
  per replica.
- The LiveKit worker has a health port (8081), a drain timeout derived from
  call duration, and on telephony a `load_fnc = active_jobs / max_sessions`,
  which is exactly the "scale on sessions, not CPU" rule.
- The schema declares `capacity` (peak and max sessions, starts per second,
  average duration), `secrets` (names only), `tracing` (Langfuse), and
  `fallback` adapters on models.
- The compile report already prints derived sizing (workers, quotas), marked
  `unbenchmarked`.

The gap is narrow and well shaped: the compiler computes the right numbers and
emits the right process behavior, then throws the numbers away instead of
emitting the manifests that would use them. There is no `deploy`, `plan`, or
`package` command; the Dockerfiles run as root with no `EXPOSE`; the generated
Pipecat `bot.py` configures no ICE servers (a known gap in DEPLOYMENT.md); and
nothing turns the `capacity` block into Kubernetes objects.

## The article's production problems, mapped

| Production problem | Status in Unmute | What to add |
|---|---|---|
| Autoscale on session count, not CPU; use KEDA | Metric exists (LiveKit `load_fnc`, Pipecat Redis admission counter) | Emit the HPA or KEDA objects that read it |
| Sticky sessions, one call stays on one replica | Documented rule, enforced by admission counters | Nothing extra; a WebSocket stays on its pod. The risk is scale-down, solved by drain plus a `preStop` hook |
| Load balancer idle timeouts kill long calls | Named as an operator duty only | Emit Ingress manifests with WebSocket upgrade and timeouts above the longest call |
| Graceful shutdown for long calls | Fully generated (drain, stop grace period) | Carry the same number into `terminationGracePeriodSeconds` |
| Graceful degradation (fallback providers) | In the schema | Nothing for v1 |
| Observability, per-session tracing | Langfuse opt-in, metrics events wired | Expose Prometheus on the LiveKit worker; a Grafana dashboard pack later |
| Templates over code, voice prompts, VAD tuning | Unmute's whole thesis | Nothing |
| Eager pre-connect, speculative tool calls, comfort noise | Runtime patterns inside the frameworks | Out of scope for deployment; candidate schema features much later |

## What the platform docs say (verified August 2026)

### LiveKit

Load balancing across agent workers is built into LiveKit server. Workers dial
out to the server, jobs are dispatched round-robin with automatic reassignment
if a worker does not accept, and a worker above `load_threshold` reports
itself full. You never put a load balancer in front of workers.

- Official sizing: 4 cores and 8 GB per worker handles roughly 10 to 25
  concurrent sessions; their load test ran 30 voice agents on that box.
- There is a reference Kubernetes manifest (plain Deployment,
  `terminationGracePeriodSeconds: 600`, requests 4 CPU / 8 Gi) but no Helm
  chart for agents, no probes, and no HPA in it.
- The Python SDK ships Prometheus metrics behind a `prometheus_port` option
  (`lk_agents_worker_load`, `lk_agents_active_job_count`). Verified in code,
  not yet documented.
- Official autoscaling advice: scale on the same metric as the `load_fnc`, at
  a lower threshold (0.5 against the 0.7 default), with a long scale-down
  stabilization window.
- The built-in health endpoint on 8081 returns 200 when connected and healthy,
  503 otherwise. Default drain timeout is one hour.
- LiveKit Cloud hosted agents are GA: `lk agent deploy` builds from your
  Dockerfile, health-gated rolling deploys, drain up to one hour, managed
  autoscaling and secrets. Our artifacts stay compatible with it.
- Self-hosting LiveKit server on Kubernetes needs host networking, one pod per
  node, public UDP ranges, and Redis; the official Helm chart covers it.
  LiveKit SIP has no Helm chart and no Kubernetes docs at all; Docker on a VM
  is the documented path.

### Pipecat

Pipecat has no official Kubernetes story. The docs name three production
patterns: VM per session, warm pool with subprocess workers, and managed
runtime (Pipecat Cloud or AWS AgentCore). On Kubernetes they say, in their own
words, that KEDA on a custom active-sessions metric fits better than HPA on
CPU, that the default 30 second grace period will drop calls, and that you
need a `preStop` hook that stops accepting sessions.

- Pipecat Cloud runs one session per instance at 0.5 vCPU / 1 GB (agent-1x),
  a useful sizing anchor for self-hosted replicas.
- The development runner (`pipecat.runner.run`) is explicitly unsupported in
  production: unauthenticated `/start`, no rate limiting, no lifecycle
  management.
- Telephony in production inverts dispatch: the carrier webhooks you, webhook
  signature verification becomes mandatory, TLS is required, and cold-start
  heavy patterns are ruled out because a caller expects sound within a second
  or two.
- OpenTelemetry support is spans only (conversation → turn → stt/llm/tts),
  with metrics riding as span attributes. Any OTLP backend works.
- The maintainers' own position on self-hosted production WebRTC is to move to
  a hosted solution, which confirms our ICE/TURN gap as the real blocker for
  the self-hosted Pipecat browser path.

## Recommendation: make deploy a compile target

### 1. An opt-in `deploy` block in `targets.yaml`, not in `agent.yaml`

Something like `deploy: {flavor: kubernetes, namespace: ..., ingress_host:
...}` per target instance. Everything sizeable stays derived from `capacity`.
This respects the SCHEMA.md §8 ban: the package still never states replicas,
machine sizes, or GPU counts. Environment identity belongs in `targets.yaml`,
which is already the per-instance file.

### 2. Emit a kustomize base, not a Helm chart

Plain YAML manifests plus a `kustomization.yaml`, so operators patch with
overlays instead of Unmute growing knobs. This matches the compiler philosophy
(emit readable native artifacts, no runtime interpreter) and avoids owning a
templating surface.

Per target, the emitted `deploy/k8s/` base:

- **LiveKit worker.** Deployment (no Service; workers dial out), resources
  4 CPU / 8 Gi, `replicas = ceil(peak_sessions / sessions_per_worker)` using
  the same coefficients as the compile report, liveness and readiness probes
  on the existing 8081 endpoint, `terminationGracePeriodSeconds` set to the
  already-derived drain timeout, `envFrom` a Secret named from the `secrets`
  block, a PodDisruptionBudget, and an HPA (CPU at 0.5 against the 0.7
  threshold) or a KEDA ScaledObject on `lk_agents_worker_load` once
  Prometheus is exposed.
- **Pipecat.** Deployment, Service, Ingress with WebSocket upgrade annotations
  and proxy timeouts set above the longest allowed call (this kills the silent
  load-balancer timeout failure), `preStop` hook plus the existing drain
  behavior, and a KEDA ScaledObject reading active sessions from the Redis
  admission counter that already exists. That counter is the load-bearing
  detail: Unmute already maintains the exact metric the Pipecat docs say you
  need.
- **Shared.** A Valkey manifest for the telephony routes (staging grade, with
  a comment that production should bring managed Redis), and namespace-scoped
  everything so staging and production are two overlays of the same base.

### 3. Promote the sizing report to `unmute plan`

The unbenchmarked numbers already computed at validation time become a first
class command: given the package, print sessions per replica, replica counts
at peak, provider concurrency quota checks, and what the Kubernetes output
will contain. Later, an opt-in benchmark harness (L4 style, needs a live
stack) replaces the placeholder "1 session per worker" coefficients with
measured ones, which removes the `unbenchmarked` label honestly.

### 4. Quick wins that need no new surface

- Harden the Dockerfiles: non-root `USER`, `EXPOSE`. Kubernetes ignores
  `HEALTHCHECK`, so keep health at the Compose and manifest level.
- Close the documented ICE gap: the generated `bot.py` reads ICE servers from
  an environment variable (for example `UNMUTE_ICE_SERVERS`). Environment
  config, not package config, so no schema change.
- Set `prometheus_port` on the generated LiveKit `AgentServer`, and use the
  session-based `load_fnc` on non-telephony workers too. Today they get a bare
  `AgentServer`, meaning CPU-based load, while `max_sessions` is already
  declared and is the better signal for I/O bound voice work.
- Remove the dangling `unmute apply` message in `internal/cli/compile.go`;
  that command no longer exists.

### 5. The "easy until we can't" boundary

Unmute generates the agent workload manifests and stops there.

- For LiveKit server: emit a `values.yaml` for the official Helm chart plus a
  checklist, never the media server manifests themselves. Host networking,
  TURN certificates, and public UDP are the operator's world, and LiveKit
  already maintains that chart.
- For LiveKit SIP: document the VM/Compose path as the supported one, since
  not even LiveKit has a Kubernetes recipe for it.
- No Terraform, no cluster provisioning, no cloud accounts.

This boundary gives [DEPLOYMENT.md](DEPLOYMENT.md) a new job: instead of a
manual for everything, it becomes the contract for what the emitted `deploy/`
covers and what remains the operator's.

## Suggested order

1. Quick wins (Dockerfile, ICE env, Prometheus port, `load_fnc`, dangling
   message). Days of work, no schema change.
2. `deploy/k8s` kustomize emission for the two browser paths, gated by the
   `targets.yaml` `deploy` block, with grace periods and probes carried over
   from what is already derived.
3. Scaling wiring: KEDA or HPA objects, PodDisruptionBudget, `preStop`, plus
   `unmute plan`.
4. Telephony deploy pack: Valkey manifest, Pipecat telephony Ingress, LiveKit
   server Helm values, SIP documented as VM based.
5. Later, only if wanted: an observability pack (Grafana dashboard JSON, alert
   rules on P95 time-to-first-byte and drain events), and eventually an eval
   loop with synthetic callers in CI as a separate feature.

## Open point against the current architecture stance

[ARCHITECTURE.md](ARCHITECTURE.md) currently assigns replica management to the
operator and calls the Compose files "development topologies, not production
recipes". Emitting derived manifests with operator overlays is an amendment to
that stance, not a violation of it, but it needs a deliberate edit to the
architecture document and a kept spec under `docs/spec/` before any code. By
the repo's own definition this is a complex feature.

## Sources

Verified August 11, 2026.

Article that framed the problem set:

- [Building a Production AI Voice Agent: Architecture, Patterns, and the Hard Problems](https://medium.com/data-science-collective/building-a-production-ai-voice-agent-architecture-patterns-and-the-hard-problems-48c880870527)
  (Santosh Shinde, Data Science Collective)

LiveKit:

- [Self-hosted agent deployments](https://docs.livekit.io/deploy/custom/deployments.md)
  (networking, load balancing, sizing, autoscaling, rollout)
- [Agent server options](https://docs.livekit.io/agents/server/options.md)
  (`load_fnc`, `load_threshold`, health endpoint, permissions)
- [Startup modes](https://docs.livekit.io/agents/server/startup-modes.md)
  (drain timeout, SIGTERM behavior)
- [Server lifecycle](https://docs.livekit.io/agents/server/lifecycle.md)
  (registration, availability exchange)
- [Agent deployment on LiveKit Cloud](https://docs.livekit.io/deploy/agents.md)
  and [managing deployments](https://docs.livekit.io/deploy/agents/managing-deployments.md)
- [Quotas and limits](https://docs.livekit.io/deploy/admin/quotas-and-limits.md)
- [Observability](https://docs.livekit.io/deploy/observability.md),
  [data hooks](https://docs.livekit.io/deploy/observability/data.md), and
  [trace export](https://docs.livekit.io/deploy/observability/tracing.md)
- [Kubernetes (server self-hosting)](https://docs.livekit.io/transport/self-hosting/kubernetes.md),
  [ports and firewall](https://docs.livekit.io/transport/self-hosting/ports-firewall.md),
  [distributed setup](https://docs.livekit.io/transport/self-hosting/distributed.md),
  [SIP server](https://docs.livekit.io/transport/self-hosting/sip-server.md)
- [livekit-helm charts](https://github.com/livekit/livekit-helm),
  [agent-deployment reference manifests](https://github.com/livekit-examples/agent-deployment),
  [livekit/sip](https://github.com/livekit/sip),
  [agents releases](https://github.com/livekit/agents/releases)
  (Prometheus metrics verified in `livekit-agents` worker and telemetry code)

Pipecat:

- [Deployment overview](https://docs.pipecat.ai/pipecat/deployment/overview)
  and [running bots in production](https://docs.pipecat.ai/pipecat/deployment/running-bots-in-production)
  (dispatcher patterns, KEDA and grace period quotes, monitoring guidance)
- Patterns: [VM per session](https://docs.pipecat.ai/pipecat/deployment/patterns/vm-per-session),
  [warm pool with subprocess workers](https://docs.pipecat.ai/pipecat/deployment/patterns/warm-pool-subprocess),
  [managed runtime](https://docs.pipecat.ai/pipecat/deployment/patterns/managed-runtime)
- [Telephony in production](https://docs.pipecat.ai/pipecat/deployment/telephony-in-production)
- [Running bots locally](https://docs.pipecat.ai/pipecat/deployment/running-bots-locally)
  (development runner warning)
- Pipecat Cloud: [scaling](https://docs.pipecat.ai/pipecat-cloud/fundamentals/scaling),
  [deploy](https://docs.pipecat.ai/pipecat-cloud/fundamentals/deploy),
  [agent images](https://docs.pipecat.ai/pipecat-cloud/fundamentals/agent-images),
  [health checks](https://docs.pipecat.ai/pipecat-cloud/fundamentals/health-checks),
  [capacity planning](https://docs.pipecat.ai/pipecat-cloud/guides/capacity-planning),
  [pricing](https://www.daily.co/pricing/pipecat-cloud/),
  [GA announcement](https://www.daily.co/blog/pipecat-cloud-is-now-generally-available/)
- [OpenTelemetry tracing](https://docs.pipecat.ai/api-reference/server/utilities/opentelemetry)
  and [pipeline metrics](https://docs.pipecat.ai/pipecat/fundamentals/metrics)
- [Issue #3987: self-hosted deployment blueprints](https://github.com/pipecat-ai/pipecat/issues/3987)
  (maintainer position on self-hosted production WebRTC)

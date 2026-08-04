# Self-hosted deployment

Status: Adopted stance, July 23, 2026. For now, Unmute deployments do not use
LiveKit Cloud or Pipecat Cloud. Everything below runs on infrastructure you
control.

The generated artifacts stay compatible with the managed clouds, but the
supported path is self-hosted. This document records what you need to take the
emitted Docker image and run a full, production-ready voice AI application
without either cloud.

[ARCHITECTURE.md](ARCHITECTURE.md) explains the runtime shapes this document
deploys. [TELEPHONY.md](TELEPHONY.md) owns the telephony details and its
release gate.

## What you get, what you provide

The compiler emits everything that belongs to the application. You provide
everything that belongs to the environment.

| The compiler emits | You provide |
|---|---|
| The Python project and a `Dockerfile` (`python:3.12-slim` base) | A place to run containers (VM, Kubernetes, or similar) |
| An `.env.example` naming every required variable | The secret values, from your secret store |
| Local Compose files for development | Production networking, TLS, DNS, and ingress |
| Deployment metadata and a compile report | Redis, scaling policy, and monitoring |

The same image runs locally and in production. Only the startup command and
the environment change.

## LiveKit: what to run

A self-hosted LiveKit deployment is three services, plus one more for phones:

```text
Internet
   |  wss:// (443, TLS)
   v
Reverse proxy / load balancer
   |
   v
LiveKit Server <----> Redis
   ^
   |  outbound WebSocket (no inbound ports)
Agent worker containers (the emitted image)
```

### LiveKit Server

The media server is the hard part of self-hosting, because WebRTC needs UDP
ports and a known public IP. Requirements from the official deployment guide:

- A domain you own, with a TLS certificate from a trusted authority.
  Self-signed certificates do not work. Clients connect to
  `wss://livekit.yourhost.com`.
- TLS termination at a reverse proxy or load balancer in front of the signal
  port.
- Redis, recommended for any production deploy and required the moment you run
  more than one server node or add SIP.
- A YAML config file holding the API key pair, RTC ports, and
  `use_external_ip: true` on cloud VMs so the server discovers and advertises
  its public IP over STUN.
- TURN for callers behind strict firewalls. LiveKit ships an embedded TURN
  server; TURN/TLS needs its own domain and certificate.

Open these ports (from the official VM guide):

| Port | Protocol | Purpose |
|---|---|---|
| 443 | TCP | Primary HTTPS and TURN/TLS |
| 80 | TCP | TLS certificate issuance |
| 7881 | TCP | WebRTC over TCP |
| 3478 | UDP | TURN/UDP |
| 50000-60000 | UDP | WebRTC media |

The fastest correct path is LiveKit's own generator. It produces a Docker
Compose file, a Caddy config that provisions certificates automatically, the
server config, a Redis config, and a startup script that installs everything
as a systemd service:

```bash
docker run --rm -it -v$PWD:/output livekit/generate
```

Run compute-optimized instances, and use host networking for the Docker
container. Metrics are available by setting `prometheus_port` in the config.

### Agent worker

The emitted image starts `python agent.py start`. Deploying the worker is much
simpler than the server:

- **No inbound ports.** The worker dials out to LiveKit Server over WebSocket
  and receives jobs on that connection. It needs no public exposure at all. An
  optional health check endpoint listens on port 8081 for your monitoring.
- **Environment variables.** `LIVEKIT_URL`, `LIVEKIT_API_KEY`,
  `LIVEKIT_API_SECRET`, plus the model provider keys named in the emitted
  `.env.example`. Inject them from your secret store, never bake them into the
  image.
- **Sizing.** LiveKit recommends starting at 4 cores and 8 GB per worker,
  which handles roughly 10 to 25 concurrent conversations depending on the
  components in use. Around 10 GB of ephemeral disk is plenty.
- **Graceful shutdown.** On SIGTERM the worker stops accepting jobs and lets
  running conversations finish. Give it a long grace period, 10 minutes or
  more, so calls are not cut off. On Kubernetes this is
  `terminationGracePeriodSeconds`.
- **Load balancing is built in.** LiveKit Server distributes jobs round-robin
  across registered workers and reassigns a job if a worker does not accept it
  in time. You do not put a load balancer in front of workers.
- **Autoscaling.** The worker reports itself full through its load function
  (CPU by default, threshold 0.7). Scale up on the same metric at a lower
  threshold, around 0.5, and use a longer cool-down when scaling down because
  draining workers take time to finish their calls.

Run separate LiveKit Server deployments for production and staging, and a
local dev server for development, so agent work never touches real traffic.

### Phone calls

The SIP route adds LiveKit SIP next to the server. It shares the server's
Redis and needs public SIP signaling and RTP ports; an HTTPS tunnel is not
enough. [TELEPHONY.md](TELEPHONY.md) owns the details. The route runs today:
`validate`, `compile`, and `dev --telephony` all accept it. Its provisional
status is internal maturity tracking, recorded in `compile-report.json` and
never a block.

## Pipecat: what to run

Pipecat is one container. The emitted image is the whole application: front
door, pipeline, models, and voices in a single process.

```text
Internet
   |  https:// + WebRTC media, or wss:// for phones
   v
TLS ingress
   |
   v
bot.py container(s) <----> Redis (telephony only)
```

### Browser sessions

The non-telephony image starts `python bot.py`, which serves the WebRTC
signaling endpoint and the media itself on the bot port (7860).

- **Ingress.** Public HTTPS to the signaling endpoint.
- **Media reachability.** The generated bot uses Pipecat's SmallWebRTC
  transport, which is a direct peer connection. Per Pipecat's docs, STUN is
  usually needed when the caller is on a different network, and TURN is
  recommended for production and often required behind strict NAT. The
  container therefore needs UDP reachability or a TURN relay.
- **Known gap.** The generated `bot.py` does not configure ICE servers today.
  Cross-network production use needs that configuration added; treat this as
  open work, not a settled path.

### Phone calls

The telephony image starts the generated FastAPI application with uvicorn on
port 7860. Production must provide:

- TLS ingress on 443 forwarding to 7860, with WebSocket upgrade support and
  idle timeouts longer than the longest allowed call.
- A shared Redis deployment, reachable through `REDIS_URL`. It holds only
  bounded control records; audio and conversation state never enter it.
- Carrier credentials and `UNMUTE_PUBLIC_URL` set to the exact public HTTPS
  origin.
- A stop grace period at least as long as the maximum call: on SIGTERM the
  application refuses new calls and drains active ones.

This route runs today; its provisional status is internal maturity tracking,
not a block. [TELEPHONY.md](TELEPHONY.md) has the full requirements,
credentials, and security rules.

### Scaling Pipecat

One call is one long-lived connection on one replica, for the life of the
call.

- Scale by active sessions, not request rate.
- Keep each WebSocket on the replica that accepted it. Redis cannot move or
  resume a call on another replica.
- Declared capacity is enforced per replica through Redis admission counters
  on the telephony route.

## Shared production concerns

These apply to both targets.

- **Secrets.** Generated files and reports contain environment variable
  names, never values. Supply values from your platform's secret store.
- **Redis.** Required for LiveKit SIP (used by LiveKit Server and SIP) and
  for Pipecat telephony (used by the generated application). Not needed for
  plain browser deployments. The pinned image is Valkey, which speaks the
  Redis protocol.
- **Separate environments.** Distinct deployments for production and staging.
  Never point a dev worker at the production server.
- **Monitoring.** LiveKit Server exposes Prometheus metrics when configured.
  The workers and the Pipecat application log to stdout; ship those logs.
  Keep `INFO` as the log level: debug logging can expose phone numbers.
- **Model providers.** Self-hosting the orchestrator does not self-host the
  models. STT, LLM, and TTS calls still go to the configured providers unless
  you point the package at self-hosted model endpoints.

## Checklists

LiveKit, browser sessions:

1. Domain plus DNS records, and a TURN subdomain if you enable TURN/TLS.
2. LiveKit Server, Redis, and Caddy (or your own proxy) on a VM or cluster,
   ideally from `livekit/generate` output.
3. Firewall open: 443, 80, 7881/tcp, 3478/udp, 50000-60000/udp.
4. Build the emitted image; run workers with `LIVEKIT_URL`, the API key pair,
   and model keys from secrets.
5. Set a 10+ minute termination grace period and an autoscaling rule below
   the worker's load threshold.
6. Confirm a browser client can join through `wss://` and a worker picks up
   the job.

Pipecat, browser sessions:

1. Build the emitted image; run it with model keys from secrets.
2. Public HTTPS to the bot port.
3. STUN/TURN plan for cross-network media (see the known gap above).
4. Scale and drain by active sessions.

Telephony, either target:

1. Declare a route with a generated adapter: any Pipecat carrier WebSocket
   route, the LiveKit Twilio connector, or any LiveKit SIP route. Only Exotel,
   which has no adapter, is rejected by `validate` and `compile`.
2. Follow [TELEPHONY.md](TELEPHONY.md) for credentials, networking, and the
   security rules.
3. Confirm the exact route on real calls yourself before production. Every
   route is still provisional, which means it has an adapter but no
   credentialed end-to-end smoke test in CI.

## Sources

Verified July 23, 2026 against the official documentation:

- [LiveKit: Deploying LiveKit](https://docs.livekit.io/transport/self-hosting/deployment/)
  (TLS, config, TURN, ports)
- [LiveKit: Virtual machines](https://docs.livekit.io/transport/self-hosting/vm/)
  (generator, Caddy, firewall list)
- [LiveKit: Self-hosted agent deployments](https://docs.livekit.io/deploy/custom/deployments/)
  (worker networking, sizing, drain, autoscaling)
- [Pipecat: SmallWebRTC transport](https://docs.pipecat.ai/api-reference/server/services/transport/small-webrtc)
  (STUN/TURN guidance)
- [Pipecat: Twilio WebSockets](https://docs.pipecat.ai/pipecat/telephony/twilio-websockets)
  (self-hosted telephony ingress requirements)

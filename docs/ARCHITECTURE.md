# Architecture

Unmute is a Go compiler for portable voice-agent packages. It reads one
declarative package, resolves it against a target, and writes artifacts that
the selected orchestrator runs natively. Unmute is not part of the generated
agent's production process.

This document defines the system boundaries, compiler flow, and runtime
topologies. [SCHEMA.md](SCHEMA.md) defines the authoring contract,
[CONTEXT.md](../CONTEXT.md) defines the shared vocabulary, and
[TELEPHONY.md](TELEPHONY.md) contains the detailed telephony design and
verification state.

## System boundary

The repository owns the compiler and its templates. The generated artifact is
an output, not a second maintained application.

```text
Agent package
    |
    v
spec.Load -> ir.Build -> ir.Validate -> generate.Generate
                                              |
                                              v
                                  Target-native artifact
                                              |
                                              v
                              LiveKit, Pipecat, or a
                                managed provider API
```

The ownership boundary has four rules:

- Maintained runtime code in this repository is Go.
- Python exists only in templates, copied local tool handlers, and generated
  artifacts.
- Generated projects contain no Unmute runtime dependency.
- Authors change the source package and recompile instead of editing generated
  files.

## Compiler flow

Validation and generation use the same compiler stages so they cannot
interpret a package differently.

1. `internal/spec.Load` reads `agent.yaml`, `targets.yaml`, declared tools,
   connections, prompts, and local handlers. It uses strict YAML decoding and
   rejects unknown fields.
2. `internal/ir.Build` resolves names, model selections, controls, connections,
   target overrides, and telephony routes into target-independent IR.
3. `internal/ir.Validate` checks the IR against the selected target capability
   table. Unsupported behavior fails before generation; target differences
   that preserve the contract produce warnings.
4. `internal/generate.Generate` validates again and dispatches to one target
   driver. A driver emits files for a code target or an apply plan for a
   managed target.

`internal/target` is the shared rulebook for target capabilities and provider
catalogs. Validation and generators must not maintain separate descriptions of
what a target supports.

## Target boundary

Unmute supports code targets and managed targets without introducing a common
runtime abstraction between them.

- A **code target** emits a runnable project. LiveKit emits `agent.py`, and
  Pipecat emits `bot.py`. You can run these projects locally or deploy them
  without Unmute.
- A **managed target** emits an apply plan for a provider API. There is no
  generated worker because the provider owns the conversation runtime.

The source package is durable. Generated artifacts are target-specific and may
be replaced completely on each compile.

## Runtime vocabulary

The word "worker" has different meanings across the two code targets. Keep the
deployment boundary separate from in-process conversation objects.

| Term | Meaning |
|---|---|
| Server | Network and control-plane service that accepts participants, routes media, or dispatches work. |
| Deployment worker | Process or container that runs agent conversations. |
| Conversation worker | In-process framework object that owns part of one conversation. |
| Redis | Shared control-plane coordination. It is never an audio or transcript store. |

LiveKit has a separate server and deployment worker. Pipecat's generated
application combines the network endpoint and conversation runtime in one
process; its `PipelineWorker` and `LLMWorker` values are in-process objects, not
separate deployments.

## Worker, server, and telephony in plain terms

Before the deep dive, here is the short version of the three moving parts and
how they differ between the two code targets.

### The worker

The worker is the process that actually runs a conversation. It holds the
speech-to-text, the language models, the voices, the tools, and the live audio
for one call.

- **LiveKit.** The worker is the generated `agent.py`, and it is its own
  process. It starts up, registers with a LiveKit Server, and waits. When a call
  arrives, the server hands it a job, the worker joins that room, and it runs the
  conversation. Inside the code this process is a LiveKit `AgentServer` object.
  The name is misleading: it is the worker side, not the media server.
- **Pipecat.** There is no separate worker process. The generated `bot.py` is
  one process that is both the entry point and the conversation runtime. Inside
  it, a `PipelineWorker` owns the transport and speech-to-text, and each authored
  agent is an `LLMWorker` with its own model and voice. Only one `LLMWorker` is
  active at a time; a handoff just switches which one is active. These "workers"
  are objects inside the single process, not separate deployments.

### The server

The server is the network front door. It accepts the caller, moves the audio,
and decides which worker handles the call.

- **LiveKit.** The server is a real, separate service: the LiveKit Server (plus
  LiveKit SIP for phone calls). It owns rooms, participants, signaling, media
  routing, and job dispatch. You scale it separately from the workers.
- **Pipecat.** There is no separate server. The same `bot.py` process is its own
  front door: it accepts the browser WebRTC connection, or the carrier
  WebSocket, and then runs the pipeline for that call.

The one-line difference: **LiveKit splits server and worker into two services;
Pipecat combines them into one process.**

### Telephony

Telephony adds a phone bridge in front of whatever already exists. Most routes
also add Redis for coordination. The one exception is the LiveKit Twilio
connector, which needs no Redis.

- **LiveKit.** LiveKit has two phone routes. The **Twilio connector** is the
  simplest to run on a laptop: a generated bridge answers Twilio and joins a
  local LiveKit room as a participant, so the worker handles the call like any
  other browser call. It needs a LiveKit Server but no Redis and no SIP. The
  **SIP** route is the multi-carrier path: a LiveKit SIP service turns the
  carrier call into a normal LiveKit participant, and LiveKit Server and LiveKit
  SIP share Redis to coordinate. The worker itself never touches Redis on either
  route.
- **Pipecat.** The same `bot.py` process gains a FastAPI front door for carrier
  webhooks and the media WebSocket. Redis sits beside it and holds only small
  control records (call correlation, idempotency, transfer state, admission
  counters). The live audio and conversation stay in the process.

Phone calls run in the public CLI today for every route with a real adapter
(see [Telephony runtime state](#telephony-runtime-state)). Only routes with no
adapter, currently Exotel, fail closed.

## LiveKit architecture

LiveKit separates room infrastructure from agent execution. That split is the
key to everything below.

### Server and worker

```text
Browser
   |
   v
LiveKit Server <----> generated Agent worker
   |                         agent.py
   v
LiveKit room
```

The **LiveKit Server** owns rooms, participants, signaling, media routing, and
job dispatch. The generated **Agent worker** (`agent.py`) registers with the
server and waits for a job. When it accepts one, it joins the assigned room and
runs the `AgentSession` containing the authored agents, tasks, tools, and model
calls.

The generated code creates a LiveKit `AgentServer`. Despite its class name, this
is the worker-side host for registration, jobs, draining, and health checks. It
is not the separate `livekit-server` media service.

All authored agents live inside one dispatched session. An agent handoff changes
the active LiveKit `Agent`; it does not dispatch the conversation to a different
container.

### Local

`unmute dev` recompiles the target and starts the smallest topology the chosen
mode needs.

| Mode | What runs |
|---|---|
| Console | `agent.py console` only. No LiveKit Server, no Redis. |
| Browser | A local or configured LiveKit Server, a separate `agent.py dev` worker, and a local UI/token helper. |
| Telephony (Twilio connector) | The generated bridge and Agent in one application container, plus a local `livekit-server --dev`. No Redis, no SIP. |
| Telephony (SIP) | Agent, Redis, LiveKit Server, and LiveKit SIP together in Docker Compose. |

In browser mode, `unmute dev` starts or reuses `livekit-server --dev` when no
`LIVEKIT_URL` is configured. The browser joins that server directly, and the
separate Agent worker joins the same room after dispatch.

### Deployment

Deployment runs the same `agent.py`, started with `python agent.py start`. For
now we do not deploy on LiveKit Cloud: the worker connects to a self-hosted
LiveKit Server. Worker replicas and LiveKit Server replicas are separate
scaling units, so you size them independently.

A self-hosted **SIP** phone route also needs LiveKit SIP, shared Redis, public
SIP signaling, and public RTP ports. The **Twilio connector** route instead
needs public HTTPS and WSS ingress for the bridge and a LiveKit Server it can
reach; it needs no SIP, no RTP ports, and no Redis.
[DEPLOYMENT.md](DEPLOYMENT.md) records the full self-hosted production
requirements.

### Telephony

LiveKit has two telephony routes with different topologies. The Twilio
connector is the primary route for local development, and SIP is the
multi-carrier path with the full feature set.

#### Twilio connector

The connector route is a generated bridge that speaks Twilio Media Streams and
joins a self-hosted LiveKit room. It uses no SIP and no Redis.

```text
Phone carrier (Twilio)
    | Media Streams (HTTPS webhook + WSS audio)
    v
telephony_bridge.py ----> LiveKit Server <----> Agent worker
  (joins the room as a participant)
```

Twilio reaches the generated `telephony_bridge.py` over a public HTTPS webhook
and a media WebSocket. The bridge answers with TwiML, terminates the Media
Streams audio itself, and joins a local LiveKit room as an ordinary participant
carrying the caller's audio. LiveKit Server then dispatches the Agent worker
into that room, and the worker reads the call facts from the job metadata. Audio
is G.711 mu-law at 8 kHz; the LiveKit SDK resamples to the agent's rate.

This is Unmute's own implementation of the Media Streams mechanism. It does not
call LiveKit's `ConnectTwilioCall` API, so it runs against a stock
`livekit-server --dev` with no connector service. Locally, Docker Compose runs
just two services: the application container (bridge plus worker) and
`livekit-server --dev`. There is no Redis and no LiveKit SIP.

#### Self-hosted SIP

The SIP route adds a SIP bridge while preserving the server-worker split. It is
the multi-carrier path for Twilio, Telnyx, and Plivo, and it carries the full
telephony feature set including transfers and voicemail detection.

```text
Phone carrier
    | SIP/RTP
    v
LiveKit SIP ----> LiveKit Server <----> Agent worker
      |                 |
      +------ Redis ----+
```

LiveKit SIP turns the carrier call into a LiveKit participant. From there the
call behaves like any other: LiveKit Server places the participant in a room and
dispatches the Agent worker. LiveKit Server and LiveKit SIP share Redis for
distributed room state, routing, and service coordination. The generated Agent
worker does not consume Redis.

Locally, Docker Compose runs all four services together (Agent, Redis, LiveKit
Server, LiveKit SIP). `unmute dev --telephony` also creates the local trunk and
dispatch records for you. SIP needs carrier-reachable public SIP signaling and
an RTP port range, which an HTTPS tunnel cannot provide.

## Pipecat architecture

Pipecat's generated application owns both its transport endpoint and its
conversation pipeline. There is no separate LiveKit-style room server in the
generated topology.

### One process

```text
Browser or carrier
        |
        v
Generated Pipecat application (bot.py)
  |- network transport
  |- speech-to-text
  |- main PipelineWorker
  |- agent LLMWorkers
  |- tools and task Flows
  `- text-to-speech
```

One main `PipelineWorker` owns the transport and speech-to-text path. Each
authored agent becomes an `LLMWorker` with its own reasoning model and voice.
Only one agent worker is active at a time, and a handoff activates another worker
inside the same process. Tasks run as Pipecat Flows on the delegating agent;
they do not create another deployment worker.

For browser sessions, the Pipecat runner accepts the WebRTC connection and
starts the conversation pipeline. For telephony, the generated FastAPI
application accepts carrier webhooks and the media WebSocket, then starts the
same pipeline for that call.

### Local

| Mode | What runs |
|---|---|
| Console | `bot.py console` only. No network server, no Redis. |
| Browser | The `bot.py` WebRTC runtime plus a local UI/reverse proxy. No Redis. |
| Telephony | The Pipecat telephony application and Redis together in Docker Compose. |

In browser mode, `bot.py` runs on the bot port and the local Unmute UI proxies
its WebRTC API requests to that process. The proxy is development tooling, not
part of the production artifact.

### Deployment

The non-telephony image starts `python bot.py`. For now we do not deploy on
Pipecat Cloud: the image runs on any infrastructure that can host the
generated Python project. The telephony image starts the generated FastAPI
application, which requires TLS ingress with long-lived WebSocket support and
a shared Redis deployment. [DEPLOYMENT.md](DEPLOYMENT.md) records the full
self-hosted production requirements.

### Telephony

Pipecat telephony adds Redis beside the generated application.

```text
Phone carrier
    | HTTPS/WSS
    v
Pipecat telephony application <----> Redis
```

The application uses Redis only for bounded control records:

- pending-call and media-stream correlation;
- callback replay and idempotency markers;
- human-transfer state and short-lived locks; and
- active-session admission counters.

The active media stream, transcript, prompts, model context, task state, and
agent handoffs remain in the application process. Redis cannot move or resume
an active call on another replica.

## Docker and Compose

Docker appears in two places: the image the compiler emits, and the local
telephony stacks.

- **Emitted image.** Every code target gets a `Dockerfile`. The same image runs
  locally and in production; only the startup command and the environment
  change.
- **Local telephony Compose.** `unmute dev --telephony` runs a route-derived
  Docker Compose graph, written as `compose.telephony.yaml` in the generated
  project. Pipecat gets its application plus Redis. The LiveKit Twilio connector
  gets its application plus a local `livekit-server --dev` and no Redis (this
  variant comes from the `compose.telephony.connector.yaml` template). LiveKit
  SIP gets its application plus Redis, LiveKit Server, and LiveKit SIP. These
  files use pinned image versions, health checks, local credentials where
  needed, and a named Redis volume where Redis is present so a normal stop and
  restart keeps local state. They are development topologies, not production
  recipes.

The compiler emits Dockerfiles and target-native deployment metadata. It does
not provision production networking, Redis, carrier resources, secrets, or
replica management. Those stay with the operator.

## Redis boundary

Redis is required by most generated telephony designs, but its consumer differs
by target, and one route uses no Redis at all.

| Target | Redis consumer | Purpose |
|---|---|---|
| LiveKit Twilio connector | None | The connector uses no Redis; the bridge keeps its own in-process call state. |
| LiveKit SIP | LiveKit Server and LiveKit SIP | Distributed room state, routing, and SIP coordination. |
| Pipecat telephony | Generated telephony application | Call correlation, idempotency, transfer locks, and admission. |

The pinned image is Valkey, an open-source server that speaks the Redis
protocol. The service name and `REDIS_URL` keep the Redis name for familiarity.

Redis never stores credentials, raw webhook bodies, audio, transcripts,
prompts, model context, task state, or agent-handoff state. Normal console and
browser development does not require Redis.

## Telephony runtime state

Public telephony execution is no longer fail-closed. Every route with a real
adapter runs in the public CLI: `validate`, `compile`, and `dev --telephony`
emit it and run it with no warning and no error. This covers the Pipecat
carrier-WebSocket routes (Twilio, Telnyx, Plivo), the LiveKit SIP routes
(Twilio, Telnyx, Plivo), and the LiveKit Twilio connector.

Two things still fail closed, and both are correct:

- **Exotel**, on either framework, has no generated adapter, so it is gated and
  rejected before an artifact is written.
- **Unproven controls** on a route that cannot perform them. Voicemail
  detection and transfers are not emitted on the connector route, so a channel
  that requests them fails and points at the SIP route. Warm transfer is not
  yet proven on any route.

A running route can still be *provisional*, which is internal maturity tracking,
not a runtime block. Provisional means the route has a real adapter but no
automated end-to-end smoke in CI yet. The status lives in the generated
`compile-report.json` and is never printed at runtime. The Twilio connector and
the Pipecat Twilio route were both confirmed on real inbound and outbound calls
by the author. [TELEPHONY.md](TELEPHONY.md) records the exact route matrix and
what each route has proven.

## Architectural invariants

Changes must preserve these boundaries:

- Compile ahead of time; do not interpret the package in production.
- Keep portable behavior in the source package and target mechanics in drivers.
- Reject unsupported behavior before generation; never drop it silently.
- Derive authoring and resolved schemas from Go structs; do not hand-author
  schema files.
- Keep secret values out of source packages and generated reports.
- Emit one carrier route per telephony target and one artifact directory per
  target instance.
- Keep media and conversation state in the active process, never in Redis.
- Scale infrastructure from declared capacity and measured runtime behavior,
  not from the number of authored agents.

## Next steps

Use these documents for details that this architecture deliberately does not
repeat:

- [SCHEMA.md](SCHEMA.md) for package fields, validation rules, and target
  capability contracts.
- [DEPLOYMENT.md](DEPLOYMENT.md) for self-hosted production deployment
  without LiveKit Cloud or Pipecat Cloud.
- [CONTEXT.md](../CONTEXT.md) for domain terms and the compile-versus-runtime
  vocabulary.
- [TELEPHONY.md](TELEPHONY.md) for carrier routes, credentials, ports,
  generated services, and verification requirements.
- [LiveKit target guide](user/targets/livekit.md) and
  [Pipecat target guide](user/targets/pipecat.md) for user-facing runtime
  instructions.

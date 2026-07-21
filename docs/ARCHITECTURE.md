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

## LiveKit runtime

LiveKit separates room infrastructure from agent execution. The generated
`agent.py` process registers with a LiveKit Server and waits for dispatched
jobs.

```text
Browser
   |
   v
LiveKit Server <----> generated Agent worker
   |                         agent.py
   v
LiveKit room
```

The LiveKit Server owns rooms, participants, signaling, media routing, and job
dispatch. The Agent worker accepts a job, joins the assigned room, and runs the
`AgentSession` containing the authored agents, tasks, tools, and model calls.

The generated code creates a LiveKit `AgentServer`. Despite its class name,
this is the worker-side host for registration, jobs, draining, and health
checks. It is not the separate `livekit-server` media service.

Authored agents also remain inside one dispatched session. An agent handoff
changes the active LiveKit `Agent`; it does not dispatch the conversation to a
different container.

### LiveKit telephony

LiveKit telephony adds a SIP bridge while preserving the server-worker split.

```text
Phone carrier
    | SIP/RTP
    v
LiveKit SIP ----> LiveKit Server <----> Agent worker
      |                 |
      +------ Redis ----+
```

LiveKit SIP turns the carrier call into a LiveKit participant. LiveKit Server
places that participant in a room and dispatches the Agent worker. LiveKit
Server and LiveKit SIP share Redis for distributed room state, routing, and
service coordination. The generated Agent worker does not consume Redis.

## Pipecat runtime

Pipecat's generated application owns both its transport endpoint and its
conversation pipeline. There is no separate LiveKit-style room server in the
generated topology.

```text
Browser or carrier
        |
        v
Generated Pipecat application
  |- network transport
  |- speech-to-text
  |- main PipelineWorker
  |- agent LLMWorkers
  |- tools and task Flows
  `- text-to-speech
```

One main `PipelineWorker` owns the transport and speech-to-text path. Each
authored agent becomes an `LLMWorker` with its own reasoning model and voice.
Only one agent worker is active at a time, and a handoff activates another
worker inside the same process. Tasks run as Pipecat Flows on the delegating
agent; they do not create another deployment worker.

For browser sessions, the Pipecat runner accepts the WebRTC connection and
starts the conversation pipeline. For telephony, the generated FastAPI
application accepts carrier webhooks and the media WebSocket, then starts the
same pipeline for that call.

### Pipecat telephony

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

## Local runtime

`unmute dev` recompiles the selected target and starts the smallest local
topology needed by the chosen mode.

| Mode | LiveKit | Pipecat |
|---|---|---|
| Console | `agent.py console`; no LiveKit Server or Redis. | `bot.py console`; no network server or Redis. |
| Browser | Local or configured LiveKit Server, separate `agent.py dev` worker, and a local UI/token helper. | `bot.py` WebRTC runtime and a local UI/reverse proxy; no Redis. |
| Telephony | Agent, Redis, LiveKit Server, and LiveKit SIP in Compose. | Pipecat telephony application and Redis in Compose. |

LiveKit browser mode starts or reuses `livekit-server --dev` when no explicit
`LIVEKIT_URL` is configured. The browser joins that server directly, and the
separate Agent worker joins the same room after dispatch.

Pipecat browser mode runs `bot.py` on the bot port. The local Unmute UI proxies
its WebRTC API requests to that process. The proxy is development tooling, not
part of the production artifact.

The telephony Compose files are development topologies. They use pinned images,
health checks, local credentials where needed, and a named Redis volume. They
are not production deployment recipes.

## Deployed runtime

Deployment runs the same generated application code with production startup
modes and infrastructure.

For LiveKit, the generated image starts `python agent.py start`. The worker
connects to LiveKit Cloud or a self-hosted LiveKit Server. Worker replicas and
LiveKit Server replicas are separate scaling units. A self-hosted SIP route
also requires LiveKit SIP, shared Redis, public SIP signaling, and public RTP
ports.

For Pipecat, the non-telephony image starts `python bot.py` and can run on
Pipecat Cloud or infrastructure that can host the generated Python project.
The telephony image starts the generated FastAPI application. Production must
provide TLS ingress with long-lived WebSocket support and a shared Redis
deployment.

The compiler emits Dockerfiles and target-native deployment metadata, but it
does not provision production networking, Redis, carrier resources, secrets,
or replica management.

## Redis boundary

Redis is required by the generated telephony designs, but its consumer differs
by target.

| Target | Redis consumer | Purpose |
|---|---|---|
| LiveKit SIP | LiveKit Server and LiveKit SIP | Distributed room state, routing, and SIP coordination. |
| Pipecat telephony | Generated telephony application | Call correlation, idempotency, transfer locks, and admission. |

Redis never stores credentials, raw webhook bodies, audio, transcripts,
prompts, model context, task state, or agent-handoff state. Normal console and
browser development does not require Redis.

## Telephony release boundary

The telephony architecture and offline emitters exist, but public telephony
execution is fail-closed as of July 21, 2026. No selectable LiveKit or Pipecat
telephony route has passed its exact credentialed smoke test. Therefore,
`validate`, `compile`, and `dev --telephony` reject those routes before writing
an artifact or invoking Docker.

Treat the Compose topologies in this document as the intended post-promotion
runtime, not an available workaround. [TELEPHONY.md](TELEPHONY.md) records
the exact route matrix and promotion requirements.

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
- [CONTEXT.md](../CONTEXT.md) for domain terms and the compile-versus-runtime
  vocabulary.
- [TELEPHONY.md](TELEPHONY.md) for carrier routes, credentials, ports,
  generated services, and verification requirements.
- [LiveKit target guide](user/targets/livekit.md) and
  [Pipecat target guide](user/targets/pipecat.md) for user-facing runtime
  instructions.

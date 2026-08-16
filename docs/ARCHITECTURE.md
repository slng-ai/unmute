# Architecture

Unmute is a Go compiler for portable voice-agent packages. It reads one
declarative package, resolves it for a target, and writes a project that the
selected orchestrator runs natively. Unmute is not part of the generated
agent's production process.

This is the only internal design document. It explains system boundaries,
compiler flow, runtime topology, and the files a contributor needs first.
Public usage lives in [`docs-site/`](../docs-site/README.md).

## Sources of truth

Each surface has one owner:

| Surface | Owner |
|---|---|
| Authoring fields and unresolved schema | Go structs in `internal/spec` |
| Resolved and debug schema | Go structs in `internal/ir` |
| Target capabilities, routes, and model providers | `internal/target` |
| Commands, flags, usage, and exits | `internal/cli` |
| System boundaries and compiler flow | This document |
| Public user guidance | `docs-site/` |
| Contributor workflow and gates | `CLAUDE.md` |
| Coding assistants that build Unmute packages | `internal/skill/assets/` |

Feature specs under `specs/` are ignored local work. They help plan a change,
but they do not outrank shipped code or tracked documentation.

## System boundary

The repository owns the compiler and its templates. A generated artifact is
output, not a second maintained application.

```text
Agent package
    |
    v
spec.Load -> ir.Build -> ir.Validate -> generate.Generate
                                              |
                                              v
                                  Target-native project
                                              |
                                              v
                                      LiveKit or Pipecat
```

Four rules hold the boundary:

- Maintained runtime code in this repository is Go.
- Python exists in templates, copied local handlers, examples, and generated
  output.
- Generated projects contain no Unmute runtime dependency.
- Authors edit the source package and compile again instead of editing
  `build/<target>/`.

## Compiler flow

Validation and generation use the same stages, so they cannot interpret a
package differently.

1. `internal/spec.Load` reads `agent.yaml`, `targets.yaml`, prompts, tools,
   connections, and local handlers. Strict decoding rejects unknown fields.
2. `internal/ir.Build` resolves names, model bindings, controls, connections,
   overrides, and routes into target-independent IR.
3. `internal/ir.Validate` checks the IR against the selected target's
   capability table. Unsupported behavior fails before generation. Safe
   target differences can produce warnings.
4. `internal/generate.Generate` validates again and dispatches to one target
   driver, which writes the native project.

`internal/target` is the shared rulebook. Validation, the console, and the
generators must not keep separate capability tables.

## Target boundary

The source package describes durable behavior. A target driver owns how that
behavior is expressed in one framework.

- **LiveKit** emits `agent.py` and uses a separate LiveKit Server for media,
  rooms, and job dispatch.
- **Pipecat** emits `bot.py`. The generated process owns both its network
  endpoint and conversation pipeline.

Both outputs are normal Python projects with pinned dependencies, a
Dockerfile, a compile report, and a generated runbook. They can run without
Unmute after compilation.

## Runtime topology

### LiveKit

```text
Browser or phone bridge
         |
         v
LiveKit Server <----> generated Agent worker
                            agent.py
```

The server owns rooms, media, participants, signaling, and job dispatch. The
generated worker registers with it and runs one dispatched conversation. An
agent handoff changes the active agent inside that session; it does not move
the call to another container.

LiveKit telephony has two shapes:

- The Twilio connector is an HTTPS/WebSocket bridge that joins a LiveKit room.
  It needs no SIP service and no Redis.
- The SIP route adds LiveKit SIP. LiveKit Server and LiveKit SIP share Redis
  for service coordination. The generated agent worker does not use Redis.

### Pipecat

```text
Browser or carrier
        |
        v
Generated Pipecat application
  |- network transport
  |- speech-to-text
  |- conversation pipeline
  |- agents, tasks, and tools
  `- text-to-speech
```

There is no separate room server. `PipelineWorker` and `LLMWorker` are objects
inside the generated process, not deployment workers. Telephony adds an HTTPS
and WebSocket front door. Routes that need shared call coordination use Redis
for bounded records such as call correlation, idempotency, transfers, and
admission counters.

## State and deployment boundaries

Redis never stores credentials, raw webhook bodies, audio, transcripts,
prompts, model context, task state, or agent-handoff state. Active conversation
state stays in the process handling the call.

The compiler emits images, local Compose files, and target-native deployment
metadata. It does not provision production networking, secret storage,
carrier numbers, carrier applications, SIP trunks, Redis, or replicas. Those
belong to the operator.

Public run instructions are kept with the behavior they explain:

- [local development](../docs-site/dev/overview.mdx)
- [telephony development](../docs-site/dev/telephony.mdx)
- [targets](../docs-site/targets/overview.mdx)
- [deployment](../docs-site/deploy/going-live.mdx)

## Repository map

Start with these files rather than scanning the whole tree.

| Area | Files |
|---|---|
| Program entry | `main.go`, `internal/cli/root.go` |
| Package loading and authoring schema | `internal/spec/` |
| Resolution and validation | `internal/ir/` |
| Capabilities and model provider catalogue | `internal/target/` |
| Driver dispatch and emitted artifacts | `internal/generate/artifact.go` |
| LiveKit driver | `internal/generate/livekit_v1*.go`, `internal/generate/templates/livekit_v1/` |
| Pipecat driver | `internal/generate/pipecat_v1*.go`, `internal/generate/templates/pipecat_v1/` |
| Interactive console and styles | `internal/tui/`, `internal/style/` |
| Package scaffolding | `internal/scaffold/` |
| Shipped coding-agent skill | `internal/skill/assets/` |
| Public packages | `examples/` |
| Public documentation | `docs-site/` |

Provider integrations are typed entries in
`internal/target/catalog_{pipecat,livekit}.go`. `service_call.go` lowers a
catalogue entry and model binding into a constructor for the templates. Add a
provider there, then update the catalogue golden and the matching Models page
under `docs-site/models/`.

## Test layers

- **L1 unit**: pure table-driven logic.
- **L2 command**: the real Cobra tree runs in process with captured output.
- **L3 golden**: generated files and catalogue resolution are byte-pinned.
- **L4 smoke**: opt-in tests install provider SDKs and import generated Python.

`make test` runs L1 through L3 with the race detector and needs no Python.
`make smoke` runs L4. Repository agreement tests bind repeated facts to their
code owners, including schemas, capabilities, provider lists, docs-site CLI
help, the shipped skill, and repository layout.

For real conversations, use the
[end-to-end example harness](../examples/E2E_HARNESS.md). It covers the part
offline and SDK smoke tests cannot: a human speaks to the generated agent and
checks the expected tool and handoff behavior.

## Architectural invariants

- Compile ahead of time. Never interpret the package in production.
- Keep portable behavior in the source package and target mechanics in the
  drivers.
- Reject unsupported behavior before writing an artifact.
- Derive schemas from Go structs. Do not hand-author schema JSON.
- Keep one capability rulebook in `internal/target`.
- Keep secret values out of source packages and generated reports.
- Emit one artifact directory per target instance and one carrier route per
  telephony target.
- Keep media and conversation state in the active process, never in Redis.
- Scale from declared capacity and measured behavior, not authored agent count.

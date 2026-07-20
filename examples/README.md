# Examples

These source packages isolate each supported orchestration shape so you can
compare the generated LiveKit and Pipecat traces without unrelated behavior.
Generated Python belongs in each package's ignored `build/` directory.

| Package | Behavior under test | LiveKit trace | Pipecat trace |
|---|---|---|---|
| [`simple-prompt`](simple-prompt/) | One agent with one prompt | `welcome_agent-livekit` | `welcome_agent-pipecat` |
| [`single-task`](single-task/) | One delegate-and-return task | `triage_agent-livekit` | `triage_agent-pipecat` |
| [`task-groups`](task-groups/) | Two ordered tasks with shared context | `event_planner-livekit` | `event_planner-pipecat` |
| [`subagents`](subagents/) | Router handoff to two specialist agents | `front_desk-livekit` | `front_desk-pipecat` |
| [`remy`](remy/) | Combined handoffs and task groups | `greeter-livekit` | Not declared |
| [`safe_core`](safe_core/) | Portable multi-target support agent | `intake-livekit` | `intake-pipecat` |

## Compile an example

Build the CLI, then validate and compile both code targets for a package.

```sh
make build
bin/unmute validate examples/simple-prompt
bin/unmute compile examples/simple-prompt
```

The generated projects are in `examples/simple-prompt/build/livekit/` and
`examples/simple-prompt/build/pipecat/`.

## Review Langfuse traces

Keep credentials in the ignored repository-root `.env.local`. Copy the file
without printing it, then run one target through `unmute dev`.

```sh
cp .env.local examples/simple-prompt/.env
bin/unmute dev examples/simple-prompt --target pipecat
```

Set `LANGFUSE_SECRET_KEY`, `LANGFUSE_PUBLIC_KEY`, and `LANGFUSE_BASE_URL`
together. Leaving all three unset disables tracing. The example also needs the
model-provider keys listed in its generated `.env.example`.

LiveKit creates one trace for the room and uses the room name as the Langfuse
session ID. Pipecat creates one trace for the full conversation.

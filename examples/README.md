# Examples

These packages apply the same Sage and Stone Salon appointment workflow
to progressively stronger orchestration. Compare them to see when one large
prompt stops being enough and independent tasks, task groups, or agent handoffs
help.

Each package contains the same five deterministic local Python tools for
customer lookup and creation, availability, booking, and cancellation. The
tools use no network or durable storage; they are fixtures for local LiveKit
and Pipecat runs.

| Package | Structure | Responsibility split |
|---|---|---|
| [`simple-prompt`](simple-prompt/) | One agent and one large prompt | One agent owns every workflow and tool. |
| [`multi-task`](multi-task/) | One agent and two independent tasks | One task owns customer records; another owns appointments. |
| [`task-groups`](task-groups/) | One agent and three ordered tasks | Shared context moves through customer identification, slot selection, and finalization. |
| [`subagents`](subagents/) | Two agents with handoffs | One agent books new visits; the other reschedules and cancels. |
| [`telephony-multi-task`](telephony-multi-task/) | The multi-task agent with a phone channel | Inbound and outbound calls over Twilio on both routes (Pipecat carrier-websocket and LiveKit SIP), with a cold transfer to a person. Fails closed until its routes are promoted. |

## Test the tools

Run the shared behavior and drift check directly with Python:

```sh
python3 examples/test_tools.py
```

The check executes every handler in every package, including invalid input and
not-found cases. The default Go suite still needs zero Python.

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

Keep credentials in the ignored repository-root `.env`. Copy the file
without printing it, then run one target through `unmute dev`.

```sh
cp .env examples/simple-prompt/.env
bin/unmute dev examples/simple-prompt --target pipecat
```

Set `LANGFUSE_SECRET_KEY`, `LANGFUSE_PUBLIC_KEY`, and `LANGFUSE_BASE_URL`
together; all three are required because the public examples configure
`tracing.provider: langfuse`. The example also needs the model-provider keys
listed in its generated `.env.example`.

LiveKit creates one trace for the room and uses the room name as the Langfuse
session ID. Pipecat creates one trace for the full conversation.

Starting a worker or exporting a synthetic span only proves that credentials
and transport work. Complete at least one user turn before reviewing Langfuse.
LiveKit then records `llm_node` and `llm_request` generation observations;
Pipecat records `llm` and `tts` generation observations under its conversation
and turn spans.

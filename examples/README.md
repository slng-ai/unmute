# Examples

These packages apply the same Sage and Stone Salon appointment workflow
to progressively stronger orchestration. Compare them to see when one large
prompt stops being enough and independent tasks, task groups, or agent handoffs
help.

The four structural packages contain the same five deterministic local Python
tools for customer lookup and creation, availability, booking, and cancellation.
The tools use no network or durable storage; they are fixtures for local LiveKit
and Pipecat runs. `salon-support`, `outbound-reminder`, and the telephony
packages carry their own smaller tool sets.

The telephony examples are **one per use case**: `telephony-hello` for inbound and
outbound, `human-transfer-cloud-twilio` for a cold transfer with nothing hosted,
`human-transfer` for a warm transfer, and `human-transfer-daily` for a
Daily-provisioned number. Which Twilio route to pick is answered in
[docs/TELEPHONY.md](../docs/TELEPHONY.md).

| Package | Structure | Responsibility split |
|---|---|---|
| [`simple-prompt`](simple-prompt/) | One agent and one large prompt | One agent owns every workflow and tool. |
| [`multi-task`](multi-task/) | One agent and two independent tasks | One task owns customer records; another owns appointments. |
| [`task-groups`](task-groups/) | One agent and three ordered tasks | Shared context moves through customer identification, slot selection, and finalization. |
| [`subagents`](subagents/) | Two agents with handoffs | One agent books new visits; the other reschedules and cancels. |
| [`salon-support`](salon-support/) | One agent, variables, browser only | **Start here.** The one you can run in a minute: web audio, local tools, no Twilio and no API to stand up. Shows a personalized greeting, a hidden tool parameter, and the model saving what the caller says. |
| [`telephony-hello`](telephony-hello/) | A minimal inbound and outbound phone agent | Real Twilio calls over two routes from one `.env`: Pipecat on the platform's own carrier stream (`cloud-websocket`, nothing hosted by you) and the LiveKit Twilio connector (which you host). The smallest way to confirm your Twilio setup before wiring a real agent. |
| [`human-transfer`](human-transfer/) | One phone agent, two ways to reach a person | Cold transfer hands the caller off and the agent drops out; warm transfer holds the caller, briefs a supervisor, then bridges the two. **LiveKit only**, over a Twilio SIP trunk: warm transfer compiles on no other route today. |
| [`human-transfer-cloud-twilio`](human-transfer-cloud-twilio/) | Cold transfer and inbound, with nothing hosted | The same salon on Pipecat Cloud, reached through your own Twilio number. Your number points at a small piece of static markup in the Twilio console; no server of yours is in the path. A failed transfer brings back a fresh agent, which is the trade for hosting nothing. |
| [`human-transfer-daily`](human-transfer-daily/) | Cold transfer on a Daily-provisioned number | The same salon on Pipecat over Daily's own number, so there is no carrier account to set up at all. |
| [`outbound-reminder`](outbound-reminder/) | One outbound agent using variables and secrets | **The secrets example.** Input variables from the dispatch, a system variable from the route, a conversation variable the model saves mid call, and both ways a secret reaches a tool: `url_env`/`token_env` on two webhook tools, and `os.environ` inside one local handler. Design in [SCHEMA.md](../docs/SCHEMA.md) sections 4.4 and 4.12. |

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

Keep credentials in the ignored repository-root `.env`, then run one target
from the repository root. A package-level `.env` can override shared values.

```sh
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

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

The telephony examples are **one per use case**: `twilio-telephony-hello` for inbound and
outbound, `pipecat-human-transfer-twilio` for a cold transfer with nothing hosted,
`livekit-human-transfer` for a warm transfer, and `pipecat-human-transfer-daily` for a
Daily-provisioned number. Which Twilio route to pick is answered in
[docs/TELEPHONY.md](../docs/TELEPHONY.md).

**Where a name starts with a provider, the provider is the point.** Putting a
caller through to a person works differently on each platform (LiveKit does it over
a SIP trunk with a native primitive for both cold and warm; Pipecat does it over
Twilio Media Streams, cold only), so those packages are not interchangeable and
their names say which one you are reading. `twilio-telephony-hello` is named after
its **carrier** instead, because it carries one target per provider and the point is
comparing how one carrier reaches each platform. `outbound-reminder` is named after
neither, because it is about variables and secrets and happens to compile for both.

| Package | Structure | Responsibility split |
|---|---|---|
| [`simple-prompt`](simple-prompt/) | One agent and one large prompt | One agent owns every workflow and tool. **The tracing example**: the only package that sets `tracing.provider: langfuse`, so it is the only one that needs the three `LANGFUSE_*` values. |
| [`multi-task`](multi-task/) | One agent and two independent tasks | One task owns customer records; another owns appointments. |
| [`task-groups`](task-groups/) | One agent and three ordered tasks | Shared context moves through customer identification, slot selection, and finalization. |
| [`subagents`](subagents/) | Two agents with handoffs | One agent books new visits; the other reschedules and cancels. |
| [`salon-support`](salon-support/) | One agent, variables, browser only | **Start here.** The one you can run in a minute: web audio, local tools, no Twilio and no API to stand up. Shows a personalized greeting, a hidden tool parameter, and the model saving what the caller says. |
| [`twilio-telephony-hello`](twilio-telephony-hello/) | A minimal inbound and outbound phone agent | Real Twilio calls on the route each platform recommends: Pipecat on the platform's own carrier stream (`cloud-websocket`, Media Streams, nothing hosted by you) and LiveKit on a Twilio Elastic SIP Trunk (`sip`, the route with transfers and voicemail). Two mechanisms side by side, and between them you can hear both call directions on a laptop. |
| [`livekit-human-transfer`](livekit-human-transfer/) | One phone agent, two ways to reach a person | Cold transfer hands the caller off and the agent drops out; warm transfer holds the caller, briefs a supervisor, then bridges the two. **LiveKit only**, over a Twilio SIP trunk: warm transfer compiles on no other route today. |
| [`pipecat-human-transfer-twilio`](pipecat-human-transfer-twilio/) | Cold transfer and inbound, with nothing hosted | The same salon on Pipecat Cloud, reached through your own Twilio number. Your number points at a small piece of static markup in the Twilio console; no server of yours is in the path. However the transfer ends, the caller comes back to a fresh agent, which is the trade for hosting nothing. |
| [`pipecat-human-transfer-daily`](pipecat-human-transfer-daily/) | Cold transfer on a Daily-provisioned number | The same salon on Pipecat over Daily's own number, so there is no carrier account to set up at all. |
| [`mcp-example`](mcp-example/) | One agent, one remote MCP server | **The MCP example.** A single tool file declares Firecrawl's MCP server, its transport, its bearer token, and the one tool of its own the agent may use; ask a question that needs current information and the agent searches the web. Browser only, both code targets, no telephony. Design in [SCHEMA.md](../docs/SCHEMA.md) N40. |
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
together; all three are required, and only by `simple-prompt`, which is the one
package that configures `tracing.provider: langfuse`. Every other example runs
on the model-provider keys alone, so no example you meet first asks you to sign
up for a third service. Add `tracing:` to any package that wants traces; the
block is two lines and the section below explains what you get.

LiveKit creates one trace for the room and uses the room name as the Langfuse
session ID. Pipecat creates one trace for the full conversation.

Starting a worker or exporting a synthetic span only proves that credentials
and transport work. Complete at least one user turn before reviewing Langfuse.
LiveKit then records `llm_node` and `llm_request` generation observations;
Pipecat records `llm` and `tts` generation observations under its conversation
and turn spans.

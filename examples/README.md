# Examples

These packages apply the same Sage and Stone Salon appointment workflow
to progressively stronger orchestration. Compare them to see when one large
prompt stops being enough and independent tasks, task groups, or agent handoffs
help.

The four structural packages contain the same five deterministic local Python
tools for customer lookup and creation, availability, booking, and cancellation.
Those tools use no network or durable storage. `salon-concierge` is the full
integration example: it combines the structures with handoffs, tracing, and
inbound telephony. The other packages keep smaller tool sets on purpose.

Use the [end-to-end example harness](../docs/HARNESS_TEST.md) when a change needs a real
provider request and a human conversation, not only the automated checks.

| Package | Structure | Responsibility split |
|---|---|---|
| [`simple-prompt`](simple-prompt/) | One agent and one large prompt | **Start here.** One agent owns every workflow and tool. **Compact tracing example:** it sets `tracing.provider: langfuse` and needs the three `LANGFUSE_*` values. |
| [`multi-task`](multi-task/) | One agent and two independent tasks | One task owns customer records; another owns appointments. |
| [`task-groups`](task-groups/) | One agent and three ordered tasks | Shared context moves through customer identification, slot selection, and finalization. |
| [`subagents`](subagents/) | Two agents with handoffs | One agent books new visits; the other reschedules and cancels. |
| [`salon-concierge`](salon-concierge/) | Four agents, tasks, a task group, handoffs, local SQLite, tracing, and inbound phone routes | **Release-readiness example.** Verify once, manage stored bookings, answer or escalate complaints, cold-transfer to a manager, and inspect Coval traces. Every tool is local Python, so nothing remote has to be up before the greeting. Browser and inbound phone on three targets, one per local telephony plane, no outbound. |
| [`slng-support`](slng-support/) | One agent, one builtin tool, hosted by SLNG | **The hosted target, smallest form.** Produces no runnable project: `unmute deploy` compiles a deployment body and pushes it. Builtins only, so the push creates nothing — SLNG already owns every capability it names. No `unmute dev`. |
| [`slng-orders`](slng-orders/) | One agent and one `local:` tool, hosted by SLNG | **The hosted target with a tool of its own.** The handler is uploaded and becomes a versioned object in SLNG, so the push creates it, runs it once to prove it works, publishes it, and attaches it. Needs `--run-samples`. |

`salon-concierge` is the only package with a telephony route. The
[telephony overview](../docs-site/telephony/overview.mdx) explains the routes
each platform offers and which one to pick.

## Compile an example

Build the CLI, then validate and compile both code targets for a package.

```sh
make build
bin/unmute validate examples/simple-prompt
bin/unmute compile examples/simple-prompt
```

The generated projects are in `examples/simple-prompt/build/livekit/` and
`examples/simple-prompt/build/pipecat/`.

## Review traces

Keep credentials in the ignored repository-root `.env`, then run one target
from the repository root. A package-level `.env` can override shared values.

```sh
bin/unmute dev examples/simple-prompt --target pipecat
```

`simple-prompt` sets `tracing.provider: langfuse` and needs
`LANGFUSE_SECRET_KEY`, `LANGFUSE_PUBLIC_KEY`, and `LANGFUSE_BASE_URL` together.
`salon-concierge` sets `tracing.provider: coval` and needs `COVAL_API_KEY`. The
smaller starter examples set neither, so the first run still needs only
model-provider keys. Add `tracing:` to any other package that wants traces; the
block is two lines and the section below explains what you get.

LiveKit creates one trace for the room and uses the room name as the session ID.
Pipecat creates one trace for the full conversation.

Starting a worker or exporting a synthetic span only proves that credentials
and transport work. Complete at least one user turn before reviewing traces.
LiveKit then records `llm_node` and `llm_request` generation observations;
Pipecat records `llm` and `tts` generation observations under its conversation
and turn spans.

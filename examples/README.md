# Examples

Four packages. `salon-concierge` is the full Sage and Stone Salon project and
the one to read when you want to see every path working together.
`salon-concierge-v2` is that salon with each step scoped to the context and the
variables it actually needs, and `salon-concierge-single-prompt` is the same
salon with the structural optimizations removed, so the three read as a ladder.
`slng-support` is the hosted target, which emits no runnable project and today
publishes only agents whose tools are already built.

If you want a package of your own to start from rather than one to read, run
`unmute init my-agent`. The scaffold writes the smallest package that does
something: one agent, browser audio, one built-in tool, no phone number and no
third-party account. [Your first
agent](../docs-site/build/your-first-agent.mdx) walks through what it contains.

Use the [end-to-end example harness](../docs/HARNESS_TEST.md) when a change needs a real
provider request and a human conversation, not only the automated checks.

| Package | Structure | Responsibility split |
|---|---|---|
| [`salon-concierge`](salon-concierge/) | Two agents, two tasks (one shared by both agents from a single definition), handoffs, a guarded task, a cold manager transfer, tracing, and inbound phone routes | **Release-readiness example.** Verify once, manage stored bookings, answer or escalate complaints, cold-transfer to a manager, and inspect Langfuse traces. Every tool is local Python, so nothing remote has to be up before the greeting. Browser and inbound phone on two targets, one per telephony plane, no outbound. |
| [`salon-concierge-v2`](salon-concierge-v2/) | The same two agents and two tasks, with every `context.history` chosen per step and one more value carried as a variable | **The scoped version.** `verify_customer` runs on `reset`, so it gets its prompt and its two values and no conversation at all; `manage_booking` and both handoffs run on `messages`, so no tool record crosses a seam. `customer_status` is assigned by the verification step and declared by the booking step, which is what makes the booking prompt allowed to read it. Same targets, same routes, same models as `salon-concierge`. |
| [`salon-concierge-single-prompt`](salon-concierge-single-prompt/) | One agent, one prompt holding everything, every tool on every turn, framework-default turn taking, the model's own endpoint | **The baseline, not a template.** The same salon as above with no tasks, no handoffs, no variables and no pre-fetch: the caller is asked for a number the carrier already supplied, the model calls a tool to find out what day it is, and it retypes the phone number into every tool call. Validates, compiles and runs on the same two targets, because a baseline that did not would prove nothing. |
| [`slng-support`](slng-support/) | One agent, one builtin tool, hosted by SLNG | **The hosted target, smallest form.** Produces no runnable project: `unmute deploy` compiles a deployment body and pushes it. Builtins only, so the push creates nothing — SLNG already owns every capability it names. No `unmute dev`. |

The three salon packages are the ones with a telephony route, and they carry the
same pair: a Twilio Elastic SIP Trunk on their LiveKit target and Pipecat Cloud's
Twilio websocket on their Pipecat target. The
[telephony overview](../docs-site/telephony/overview.mdx) explains the routes
each platform offers and which one to pick.

Every package here states a `name:`, which is required, and every deployment is
named after it: `salon-concierge` on a target called `livekit` registers a worker
called `salon-concierge-livekit`. Copy a package and you are copying its name, so
rename it before you deploy, or your deploy lands on top of the one already
there. [The `name` reference](../docs-site/reference/agent-yaml.mdx) has the
whole rule.

## Compile an example

Validate and compile both code targets for a package.

```sh
unmute validate examples/salon-concierge
unmute compile examples/salon-concierge
```

The generated projects are in `examples/salon-concierge/build/livekit/` and
`examples/salon-concierge/build/pipecat/`.

## Review traces

Keep credentials in the ignored repository-root `.env`, then run one target
from the repository root. A package-level `.env` can override shared values.

```sh
unmute dev examples/salon-concierge --target pipecat
```

All three salon packages set `tracing.provider: langfuse` and need
`LANGFUSE_SECRET_KEY`, `LANGFUSE_PUBLIC_KEY`, and `LANGFUSE_BASE_URL` together.
A package that wants Coval instead sets `tracing.provider: coval` and needs
`COVAL_API_KEY`.
A scaffolded package sets neither, so the first run needs only model-provider
keys. Add `tracing:` to any package that wants traces; the block is two lines
and the section below explains what you get.

LiveKit creates one trace for the room and uses the room name as the session ID.
Pipecat creates one trace for the full conversation.

Starting a worker or exporting a synthetic span only proves that credentials
and transport work. Complete at least one user turn before reviewing traces.
LiveKit then records `llm_node` and `llm_request` generation observations;
Pipecat records `llm` and `tts` generation observations under its conversation
and turn spans.

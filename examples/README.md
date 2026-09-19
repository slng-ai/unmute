# Examples

`customer-intake` is the small one: a single agent that collects
a caller's details, saves each under a declared type, and hands them to a tool
the model cannot type over. Start there if the question is about types, saved
values or injection. `salon-concierge` is the full Sage and Stone Salon project
and the one to read when you want to see every path working together.
`salon-concierge-single-prompt` is that same salon with the structural
optimizations removed, so the optimized package can be read against something.
`hotel-concierge` is the hosted target, which emits no runnable project and
publishes only agents whose tools SLNG already holds; it uses everything that
target accepts. `takeaway-orders` is the speech to speech one: a single live
model hears the caller and speaks back, and a backend model runs the tools and
searches the shop's documents behind it. `pharmacy-refills` is the other speech
to speech one: one realtime model does everything itself, and the package is
there to show who decides the caller has finished talking.

If you want a package of your own to start from rather than one to read, run
`unmute init my-agent`. The scaffold writes the smallest package that does
something: one agent, browser audio, one built-in tool, no phone number and no
third-party account. [Your first
agent](../docs-site/build/your-first-agent.mdx) walks through what it contains.

| Package | Structure | Responsibility split |
|---|---|---|
| [`customer-intake`](customer-intake/) | One agent, three tasks, one local tool, one declared shape, browser audio on both code targets | **The typed-state example.** Take a caller's number, name, email address, enquiry and callback time, save each under its own type, and open a customer record. Every declared type appears once. The tool takes one argument from the model and reads four values straight out of state through `inject:`, so the model cannot retype a number it already heard. The caller's number carries `confirm:`, so the tool refuses itself until the caller has agreed the number is theirs. No phone route and no second agent. It traces every call, so it needs the three `LANGFUSE_*` names as well as the two model keys. |
| [`salon-concierge`](salon-concierge/) | Two agents, two tasks, both steps of a task group `book` that runs verification then booking and skips verification with `skip_when_confirmed` once the caller's number is confirmed, both ending on their own tool with `finish:`, one read tool and one write tool for the whole diary, `announce:` lines covering the waits, handoffs in both directions, a cold manager transfer, tracing, and inbound phone routes on two targets | **Release-readiness example.** Verify once, manage stored bookings, answer or escalate complaints, cold-transfer to a manager, and inspect Langfuse traces. A task is spent only where something has to happen in an order, because entering one costs a model request that speaks no words. Every tool is local Python, so nothing remote has to be up before the greeting. Browser and inbound phone on two targets, one per telephony plane, no outbound. |
| [`salon-concierge-single-prompt`](salon-concierge-single-prompt/) | One agent, one prompt holding everything, every tool on every turn | **The baseline, not a template.** The same salon as above with no tasks, no handoffs, no variables and no pre-fetch: the caller is asked for a number the carrier already supplied, the model calls a tool to find out what day it is, and it retypes the phone number into every tool call. Model, transport and turn taking are held identical to the package above, so a difference you hear is a difference the structure made. Validates, compiles and runs on the same two targets, because a baseline that did not would prove nothing. |
| [`hotel-concierge`](hotel-concierge/) | One agent, four tool references, five template variables, a model fallback, hosted by SLNG | **The hosted target's showcase.** Produces no runnable project: `unmute deploy` compiles a deployment body and pushes it. Two `slng:` tools by name, two named tools from one `mcp:` server and one `builtin:`, so the push creates nothing and SLNG already owns every capability it names. Template variables with defaults reach the greeting and the prompt, an `inject:` pins the hotel's identifier so the model never asks for it, and a tool `announce:` covers the wait. No `unmute dev`: a web session or an attached phone number talks to it. |
| [`takeaway-orders`](takeaway-orders/) | One agent on `architecture: live`, one live model with a think backend, two local tools, one knowledge base of three documents, browser audio on both code targets | **The speech to speech example.** Take phone orders for a busy takeaway. One OpenAI live model hears the caller, decides when they have finished and speaks in its own voice, so a caller who talks over the agent is answered rather than queued. Tools and the harder reasoning run on the `backend:` think entry while the live model keeps talking: the menu lookup and the order both go there, and so does the search over the shop's opening hours, allergens and offers. One agent and nothing else, because a live session fixes its instructions when it starts. No phone route, and one key, `OPENAI_API_KEY`. |
| [`pharmacy-refills`](pharmacy-refills/) | One agent on `architecture: realtime`, one realtime model doing all four jobs, two local tools, one knowledge base of three documents, browser audio on both code targets | **The turn taking example.** Reorder a repeat prescription at a pharmacy. One OpenAI realtime model hears the caller and answers in its own voice, with no transcriber and no synthesizer in the path. It is here for one key, `turn_detection:`, which says whether a silence window, the model, or this project's own detector decides the caller has finished. Callers read a seven character prescription reference off a box and pause in the middle of it, so a silence window answers half a number. A knowledge base of three markdown documents answers questions about how the shop works. No phone route, and one key, `OPENAI_API_KEY`. |

The two salon packages are the ones with a telephony route, and they carry the
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

Both salon packages set `tracing.provider: langfuse` and need
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

## Contribute one

A change to Unmute ships with a package that uses it, so a maintainer can
compile it, dial it, and hear the difference. The new key has to appear in the
package's authored files and the code path has to run on a real call, or there
is nothing to review. Extending one of the packages above counts.

[CONTRIBUTING.md](../CONTRIBUTING.md) says what a public example has to pass,
where a package goes when it is meant to be dialled rather than read, and what
else a pull request carries: the issue you opened first, the video, the README
and the docs page.

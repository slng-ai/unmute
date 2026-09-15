# takeaway-orders

One agent on `architecture: live`: a single OpenAI model hears the caller, decides
when they have finished and speaks back in its own voice, while a backend model
searches the menu, prices the order and answers questions from the shop's own
documents.

It is the package for one question: **when is speech to speech worth it?** A busy
takeaway is the answer. The caller talks over the agent, changes their mind
halfway through a sentence and reads a list of dishes out at speed, and none of
that waits on a transcriber, a turn detector and a synthesizer taking their turns.

No phone route and no carrier account. Browser audio on both code targets. It
needs one value, `OPENAI_API_KEY`, which covers the live model, the backend model
and the embeddings the knowledge index is built from.

On this page:

- [Quickstart](#quickstart) - validate, run, talk to it
- [What it shows](#what-it-shows) - the four things worth reading
- [The live model and its backend](#the-live-model-and-its-backend) - who does what
- [Knowledge](#knowledge) - documents, not prompt text
- [Files](#files) - what each file holds
- [What a live package cannot carry](#what-a-live-package-cannot-carry) - and why
- [Troubleshooting](#troubleshooting) - when something goes wrong
- [Where to go next](#where-to-go-next) - packages and pages

## Quickstart

```sh
unmute validate examples/takeaway-orders
unmute compile examples/takeaway-orders
cp examples/takeaway-orders/build/pipecat/.env.example .env   # then fill it in
unmute dev examples/takeaway-orders --target pipecat
```

Swap `--target livekit` for the other one. Both are browser audio over WebRTC,
so `unmute dev` opens a page you talk to and no carrier is involved.

Try this, in this order. It is the shortest route through everything the package
does:

1. Ask for the crispy duck. It is off tonight, and the agent finds that out from
   the tool rather than from its prompt.
2. Order salt and pepper chicken and egg fried rice for collection.
3. While it is reading the order back, talk over it and ask whether there are
   nuts in the kung pao. That question goes to the knowledge documents.
4. Ask what time it closes on a Monday.

The two local tools are one Python file, `tools/takeaway.py`. Run its own check
without compiling anything:

```sh
python3 examples/takeaway-orders/tools/takeaway.py
```

## What it shows

**A live model doing three jobs.** `architecture: live` plus a `models.live`
entry replaces the transcriber, the model and the synthesizer with one service.
The emitted project builds no speech recognition and no speech synthesis at all.

**A backend that does the careful work.** `backend:` names a `models.think` entry
at OpenAI. The live model hands it anything needing a tool or real thought and
keeps talking meanwhile.

**Knowledge on speech to speech.** A `knowledge:` section and a tool file with
`knowledge: base:` give the agent three documents to search. Allergens and
opening hours are facts that change and that a caller will hold you to, so they
are documents rather than sentences in a prompt.

**A greeting the model paraphrases.** The greeting under `conversation:` reaches
the live session as its opening instruction, not as a line to read. The model
says something with the sense of it and different words each call. That is
working as intended, not a bug in your run.

## The live model and its backend

The live model is quick and loose. It is very good at knowing when a caller has
stopped, at being talked over, and at sounding like a person answering a phone in
a busy shop. It is not good at adding a bill up or remembering which dish costs
what.

So the package splits the work:

| Who | Does |
|---|---|
| `counter_voice`, the live model | hears the caller, decides the turn, speaks |
| `kitchen`, the backend think model | runs the three tools, and the reasoning around them |

The handover happens inside the live session. The caller hears the live model go
on talking while a lookup runs. This is the reason the backend has to be an
OpenAI think entry with no `endpoint_env`, and the reason an agent that holds
tools and names no `backend:` is refused at validate on both targets.

One thing to know before you copy this: a `params:` block on the think entry
reaches the emitted live service nowhere. Only the model name crosses over. It
is written up in `compile-report.json` either way, so read that rather than
assuming a setting arrived.

## Knowledge

`knowledge:` in `agent.yaml` declares one base, `kitchen`, pointing at
`knowledge/kitchen/`. Every `.md`, `.txt` and `.pdf` in that folder is read,
chunked and indexed when the worker starts.

`tools/look_up_kitchen_info.yaml` is the search tool. A knowledge tool owns both
sides of its contract, so it declares no `input:` and no `output:`: it takes a
query and hands passages back.

The documents are deliberately written the way a real shop would write them, with
the hedges kept in. The allergen document says the kitchen is not allergen free
and says what to do when the answer is not there, because that is the honest
answer and the agent will read it out.

Add a document to the folder and recompile. Nothing else changes.

## Files

| File | Holds |
|---|---|
| `agent.yaml` | the architecture, the two models, the knowledge base, the greeting and the idle timer |
| `targets.yaml` | both code targets, no connection on either, so browser only |
| `instructions.md` | the counter's prompt: how to take an order and when to look something up |
| `tools/look_up_dish.yaml` | menu lookup, one spoken name in, real name and price out |
| `tools/place_order.yaml` | places the order and returns the number, the total and the wait |
| `tools/look_up_kitchen_info.yaml` | the knowledge search over the `kitchen` base |
| `tools/takeaway.py` | both local handlers and the in process demo store |
| `knowledge/kitchen/*.md` | opening hours and delivery, allergens, set meals and offers |

## What a live package cannot carry

A live session fixes its instructions, its model and its voice when it starts.
That is the vendor's rule, not this compiler's, and it is why this package is one
agent and nothing else. Tasks, task groups, handoffs, a second agent, variables,
prefetch, tracing, an mcp tool, escalations, a telephony connection,
`listen:`, `speak:`, `turn:` and `conversation.interruption` are each refused at
validate with a sentence saying what to write instead.

If you need any of them, the shape is `architecture: cascade`.
[`customer-intake`](../customer-intake/) is the small cascaded package, and
[`salon-concierge`](../salon-concierge/) is the full one.

## Troubleshooting

**The agent says something other than the greeting.** Expected. The live model
paraphrases the greeting rather than reading it.

**It answers an allergen question from nowhere.** Check that
`knowledge/kitchen/` still holds the three documents and that the worker logged
the index build at startup. An empty folder is refused at validate, so a package
that compiles has documents.

**A tool call never happens.** An agent with tools needs `backend:`. Without it
validate refuses the package, so if you have edited `models.live` and it will not
compile, that is the line to look at.

**`OPENAI_API_KEY` is set and the call still fails on the first word.** The live
model and the backend are separate model ids on the same key. A key without
access to the live model fails at session start, before any tool runs.

**The order total looks wrong.** `tools/takeaway.py` is a demo store with about
ten dishes. Run it on its own to see what it holds.

**Nothing is said at all after the page connects.** The idle nudge is at twenty
seconds and the call ends at sixty. Both are in `conversation.inactivity`.

## Where to go next

- [`customer-intake`](../customer-intake/) - the same browser only shape on a
  cascaded pipeline, with typed state and a confirming step.
- [`salon-concierge`](../salon-concierge/) - two agents, five tasks, knowledge,
  tracing and two phone routes.
- [Live model](../../docs-site/models/live.mdx) - every key a live entry takes and
  every shape a live package is refused.

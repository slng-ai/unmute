# pharmacy-refills

A repeat prescription line, built on `architecture: realtime`. One model hears
the caller and answers in its own voice. There is no transcriber and no
synthesizer in the path.

It is the small package for one question: **who decides the caller has
finished?** On this line that question is the whole product. Callers read a
seven character prescription reference off a box, and they stop in the middle of
it to check the label. A pipeline that ends the turn on silence talks over them
every time.

No phone route and no carrier account. Browser audio on both code targets. It
needs one credential, `OPENAI_API_KEY`, which serves the realtime model and the
knowledge base together.

On this page:

- [Quickstart](#quickstart) - validate, compile, run
- [Why the turn setting is the package](#why-the-turn-setting-is-the-package) - the one decision
- [What it knows](#what-it-knows) - the documents it answers from
- [Files](#files) - what each file holds
- [What it cannot have](#what-it-cannot-have) - and what to write instead
- [Advanced](#advanced) - the other two turn settings, the half cascade
- [Troubleshooting](#troubleshooting) - when something goes wrong
- [Where to go next](#where-to-go-next) - packages and pages

## Quickstart

```sh
unmute validate examples/pharmacy-refills
unmute compile examples/pharmacy-refills
cp examples/pharmacy-refills/build/pipecat/.env.example .env   # then fill it in
unmute dev examples/pharmacy-refills --target pipecat
```

`unmute dev` is browser audio. That is the only transport this package declares:
`targets.yaml` names no `connection:`, so there is no phone leg, no tunnel and
nothing to set up with a carrier.

Swap the target to hear the same package on the other framework.

```sh
unmute dev examples/pharmacy-refills --target livekit
```

The two local tools live in one Python file, `tools/pharmacy.py`. Run its own
check without compiling anything:

```sh
python3 examples/pharmacy-refills/tools/pharmacy.py
```

Ask for a refill on reference R X four eight two one B, read slowly, with a
pause in the middle. That is the call this package exists for.

## Why the turn setting is the package

`turn_detection:` is written out in `agent.yaml`, and it is set to `semantic`.
There were three answers and the reference number picked one.

| Value | What decides | Why not here |
|---|---|---|
| `server_vad` | a silence window at the vendor | A caller pausing to turn the box over has been silent long enough, so the model answers half a reference. |
| `semantic` | the model, on whether the caller finished a thought | **This one.** Half a reference is not a finished thought. |
| `local` | this project's own turn detector, the one a cascaded package uses | The local detector hears audio, not words, so it makes the same mistake on a paused number, with another service in the path. |

Write `local` when you want `pace:` and `endpointing_delay` to apply, because
those settings belong to this project's detector and reach nothing while the
vendor decides. On LiveKit `local` lowers to emitting no turn argument at all,
which is what lets the framework take over.

Leaving the key out is not the same as writing one. An omitted value sends
nothing and the vendor's own default applies, which is semantic detection on
today's release and may not be on the next one. Writing it is how a package pins
the behaviour it was tested with.

## What it knows

The agent answers questions about the pharmacy from documents, not from its
prompt. Three short markdown files under `knowledge/policies/`: repeat
prescriptions, collection and delivery, and controlled medicines.

Two authored pieces make that work. The package declares the base:

```yaml
knowledge:
  policies:
    documents: knowledge/policies
```

and a tool reads it:

```yaml
knowledge:
  base: policies
```

That tool file carries no `input:` and no `output:`. A knowledge tool owns both
sides of its own contract: it takes the caller's question and returns passages
with their sources, so there is nothing for an author to declare.

The documents are read, split and embedded once when the process starts, and
held in memory. Content is fixed until the next compile, so editing a file here
changes nothing until you run `unmute compile` again.

Everything the retrieval settings could hold is left at the default: `hybrid`
search over passages of 90 tokens, three returned per lookup, no score cutoff.
This corpus is three short pages, and none of those numbers changes an answer on
it. [Knowledge bases](../../docs-site/build/tools/knowledge.mdx) says what each
one does and when to move it.

## Files

| File | What is in it |
|---|---|
| `agent.yaml` | the architecture key, the realtime model, `turn_detection:`, the knowledge base, the greeting and the nudge |
| `targets.yaml` | both code targets, no `connection:`, which is what makes it browser only |
| `instructions.md` | the agent's own prompt, including how to wait for a reference and how to read one back |
| `tools/look_up_prescription.yaml` | one argument, six fields back, so a miss has the same shape as a hit |
| `tools/request_refill.yaml` | the write, with four named outcomes the model reports rather than invents |
| `tools/pharmacy.py` | both handlers, a four record demo store, and a `_demo()` self-check |
| `tools/look_up_pharmacy_policy.yaml` | the knowledge lookup: a `base:` and a description, nothing else |
| `tools/end_call.yaml` | the built-in that hangs up |
| `knowledge/policies/*.md` | the three documents the agent answers policy questions from |

## What it cannot have

A realtime package is one agent and one model, and this release emits nothing
else for it. Each of these is refused at `unmute validate`, with its own sentence
saying what to write instead:

- tasks, task groups, handoffs, and any control that hands the call on
- a second agent
- `variables:` and `prefetch:`, because this shape carries no call state yet
- `tracing:`, because there is no traced worker for it yet
- an `mcp:` tool, because there is nowhere to start and close the connection
- an escalation and a `connection:`, because both need the carrier leg this
  shape does not carry yet

For any of those, write `architecture: cascade`, which is what every other
example here runs on. [`salon-concierge`](../salon-concierge/) has all of them
working together.

The cascade sections are refused too, and for a different reason: the model
replaces them. There is no `models.listen`, no `models.turn` and no `think:`,
because this one model does all three jobs.

## Advanced

<details>
<summary>Hear the other two turn settings</summary>

Change one line in `agent.yaml` and run the same call. Read a reference slowly
and stop in the middle of it on each value.

```yaml
turn_detection: server_vad   # then semantic, then local
```

`local` also wants a decision about what takes over. On LiveKit the framework's
own detector does; on Pipecat the service goes into manual mode and this
project's turn settings decide. Once you are on `local`, a `turn:` binding and
`pace:` become legal and start to matter.

</details>

<details>
<summary>Let a synthesizer speak instead of the model</summary>

A realtime model can be asked for text and let a voice of your choosing speak
it. That is the half cascade: the model still listens and thinks, and a
`speak:` binding on the agent renders the words.

Remove `voice:` from the realtime entry and bind a `models.speak` entry on the
agent instead. Asking for both is refused, because the frameworks read one and
ignore the other, so which voice the caller hears would be decided by something
the package never says.

</details>

## Troubleshooting

### It stops at startup and says an environment variable is missing

`OPENAI_API_KEY` has to hold a value before the first turn. It serves the
realtime model and the embedding calls that build the knowledge index, so a
missing one is a session that never starts.

**Fix:** fill in the generated `.env.example`.

```sh
unmute compile examples/pharmacy-refills
cp examples/pharmacy-refills/build/pipecat/.env.example .env
```

### The agent answers before the caller has finished the reference

That is the turn decision, and it is the one thing this package is about. Check
which value `agent.yaml` carries.

**Fix:** put it back to `semantic`, which asks the model whether the caller
finished a thought rather than only went quiet.

```yaml
turn_detection: semantic
```

If it is already `semantic` and the model still cuts in, the caller's pause is
long enough that the model believes the sentence ended. Say in the prompt that a
reference is read in groups and that a pause in the middle is normal, which is
what the "Let the caller finish" section of `instructions.md` does.

### The agent waits too long after the caller has clearly stopped

The other side of the same setting. `semantic` costs a beat over a silence
window, because a judgement is made about what was said.

**Fix:** if your callers say short things and never spell anything out,
`server_vad` is cheaper and faster and is the right answer for that line.

### A policy answer is vague, or it says it has nothing written down

The lookup ran and the documents did not cover the question, or the question did
not match the words the documents use.

**Fix:** the documents are the content. Edit or add a file under
`knowledge/policies/` and compile again. Nothing changes until you do: the index
is built from what was compiled in.

```sh
unmute compile examples/pharmacy-refills
```

### I added tasks and validate refused the package

A realtime package serves one agent in this version. The refusal names the
capability row it read, so it is telling you where the limit lives rather than
restating it.

**Fix:** write `architecture: cascade`, which carries tasks, or keep the flow in
the one prompt.

### I added a phone connection and validate refused it

Realtime compiles for the browser route in this version. The carrier leg is not
in this shape yet.

**Fix:** keep the package on browser audio, or move the phone work to a cascade
package. [`salon-concierge`](../salon-concierge/) has the two shipped phone
routes.

## Where to go next

- [Realtime architecture](../../docs-site/build/architecture/realtime.mdx) - every key a realtime package takes
- [Turn detection](../../docs-site/models/turn-detection.mdx) - who ends the turn, on every architecture
- [Knowledge bases](../../docs-site/build/tools/knowledge.mdx) - documents, chunking and retrieval
- [`salon-concierge`](../salon-concierge/) - the full cascade package, with knowledge and two phone routes
- [`customer-intake`](../customer-intake/) - one agent, typed saved values, browser only
- [All the examples](../README.md) - what each one is for

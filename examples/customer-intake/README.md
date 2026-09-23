# customer-intake

One agent that takes a caller's details, saves each one under a declared type,
and hands them to a tool the model cannot type over.

It is the small package for one question: **how do I collect typed information
from a caller and use it?** [`salon-concierge`](../salon-concierge/) does all of
this too, but spread across two agents, five tasks, tracing, knowledge documents
and two phone routes. This one does nothing else, so the typed part is the only
part there is to read.

No phone route and no carrier account. Browser audio on both code targets. It
needs `OPENAI_API_KEY` and `SLNG_API_KEY` for the models, and the three
`LANGFUSE_*` names together, because the package traces every call.

On this page:

- [Quickstart](#quickstart) - validate, run, check
- [What it collects](#what-it-collects) - every declared type once
- [The three things it shows](#the-three-things-it-shows) - the ideas behind it
- [Files](#files) - what each file holds
- [What it does not do](#what-it-does-not-do) - and where to look
- [Advanced](#advanced) - scripted run, other gateways
- [Troubleshooting](#troubleshooting) - when something goes wrong
- [Where to go next](#where-to-go-next) - packages and pages

## Quickstart

```sh
unmute validate examples/customer-intake
unmute compile examples/customer-intake
cp examples/customer-intake/build/pipecat/.env.example .env   # then fill it in
unmute dev examples/customer-intake --target pipecat
```

The generated `.env.example` names every value the run needs. All five are
required and the agent refuses to start while one is empty.

`unmute dev` is browser audio, so there is no carrier and no caller ID. Seed one
the way a route would:

```sh
unmute dev examples/customer-intake --target pipecat --source from_number=+34600111222
```

Leave `--source` off and the pre-fetch entry finds nothing, skips, and
`verify_contact` asks for a number instead. That is the same path a withheld
caller ID takes on a real phone route.

The package's one tool is a local Python file, `tools/intake.py`.
Run the handler's own check on its own, without compiling anything:

```sh
python3 examples/customer-intake/tools/intake.py
```

## What it collects

Every type in the authoring grammar appears once, and each one is there because
that value really has that shape.

| Value | Type | Where it comes from |
|---|---|---|
| `caller_phone` | `Phone` | the carrier's caller ID, then a step that hears the caller agree |
| `contact` | `NameEmail` | one answer, held as a name and an address |
| `caller_email` | `EmailStr` | picked off `contact` with a dotted assign, not asked for twice |
| `enquiry` | `Literal[...]` | one of four words; a fifth is refused where it enters |
| `callback_time` | `Time \| None` | the caller says "half four", the model writes 16:30, or leaves it out |
| `notes` | `list[str]` | appended, so a second remark does not replace the first |
| `record` | a declared shape | the tool's return, relayed by the model into `finish` |
| `record_id` | `Id` | picked off `record` with a dotted assign |
| `today_date` | `Date` | read once from the clock before the greeting |

## The three things it shows

**A value is checked where it enters, not where it is used.** Each type above
lowers to `str` in the schema the model is sent, and to a validator in the
generated Python. An address the model heard wrong is refused with the format,
in a message the model can correct itself from, and the previous value survives.
Nothing downstream re-checks anything.

**An injected value is not a parameter.** `create_customer_record` takes exactly
one argument from the model, a one-sentence summary. The number, the address,
the name and the enquiry word come out of saved state through `inject:`, so they
are never advertised to the model and it cannot retype them. The compiler
refuses an `inject:` key that is also an `input:` property, which is what stops
a parameter quietly having two sources.

**A pre-fetched value is a proposal, not a fact.** `caller_phone` carries
`confirm: verify_contact`. Until that step has heard the caller agree, the number
renders in no prompt but that step's own, and the tool refuses itself to the
model:

```
cannot call create_customer_record yet: caller_phone not set. run verify_contact first.
```

Somebody may be ringing from a friend's phone. Without the mark, a record would
be opened against somebody else's number and nothing would say so.

## Files

| File | What is in it |
|---|---|
| `agent.yaml` | one agent, three tasks, the shape, the variables and the pre-fetch |
| `targets.yaml` | both code targets, no `connection:`, which is what makes it browser only |
| `instructions.md` | the agent's own prompt; deliberately holds no phone number |
| `tasks/verify-contact.md` | the confirming step, and the only prompt that holds the number |
| `tasks/take-details.md` | name, address and enquiry in one pass |
| `tasks/open-record.md` | calls the tool and relays the record back |
| `tools/create_customer_record.yaml` | one model argument, four injected values |
| `tools/intake.py` | the local handler, an in-process store, and a `_demo()` self-check |

## What it does not do

No phone number, no transfer, no handoff, no second agent and no knowledge
base. For any of those, read [`salon-concierge`](../salon-concierge/).
For a package with none of the structure at all, to read the optimized one
against, read
[`salon-concierge-single-prompt`](../salon-concierge-single-prompt/).

The declared types are refused on the `slng` target, which runs no code from
your package and so has nowhere to check one. That is why this package names
only the two code targets.

## Advanced

<details>
<summary>Let Coval call it on your laptop</summary>

This package has no phone route, but its Pipecat build still answers the
runner's Twilio route. So Coval can place simulated calls to it, on both
targets, with nothing deployed:

```sh
uv run --project utils/coval_sim coval-sim add customer-intake   # once
make sim TEST_SET=<your test set id>
```

`add` creates the Coval agents `unmute-customer-intake-livekit` and
`unmute-customer-intake-pipecat`. Attach a test set and metrics to both in Coval first.
[The coval-sim guide](../../utils/coval_sim/README.md) has the rest.

</details>

<details>
<summary>Drive it through a scripted conversation, with no audio</summary>

To watch the values land without talking to anything, drive the compiled LiveKit
agent through a scripted conversation. It uses the real model and the real local
tool, no audio, and prints the declared state after every turn:

```sh
unmute compile examples/customer-intake
uv run --python 3.12 --project examples/customer-intake/build/livekit \
  python scripts/text_run_livekit.py examples/customer-intake \
  --line "Hi, I'd like to get on your books." \
  --line "Yeah that's right." \
  --line "It's Robin Vega, and my email is robin dot vega at gmail dot com." \
  --line "That's right. I'm a new customer. Any time after half four suits me."
```

That script loads the package's own `.env` and nothing else, so copy the
repository's into `examples/customer-intake/.env` first. It is ignored by git.
`unmute dev` does not need this: it reads the repository root's `.env` on its
own.

</details>

<details>
<summary>Send the speech models through another gateway</summary>

**Speech gateway.** Both targets send STT and TTS through `eu-north.api.slng.ai`.
Change `params.world_part` on each speech model to choose another
[SLNG gateway](../../docs-site/optimization/regional-infrastructure.mdx).

</details>

## Troubleshooting

### It stops at startup and says an environment variable is missing

Every name under `secrets:` has to hold a value before the first turn, and this
package declares five: the two model keys and the three `LANGFUSE_*` names that
tracing needs. The check runs before the agent answers rather than at the first
tool call, so a missing one is a session that never starts.

**Fix:** fill in every line of the generated `.env.example`.

```sh
unmute compile examples/customer-intake
cp examples/customer-intake/build/pipecat/.env.example .env
```

### The agent asks for my number instead of reading one back

Nothing supplied a caller ID, so the `caller` pre-fetch entry skipped and
`verify_contact` fell back to asking. On browser audio that is the normal case.

**Fix:** seed the number the way a phone route would.

```sh
unmute dev examples/customer-intake --target pipecat --source from_number=YOUR_NUMBER
```

### `create_customer_record` refuses itself and names `caller_phone`

The number is still a proposal. It carries `confirm: verify_contact`, so every
tool that injects it refuses until that step has heard the caller agree.

**Fix:** run `verify_contact` first and let the caller agree the number is
theirs. In a scripted run that is a turn that says yes.

```
--line "Yeah that's right."
```

### A value the model sent was refused and the turn went round again

That is the declared type doing its job. `enquiry` takes one of the four words
in the table above, `callback_time` takes a time on the 24 hour clock or
nothing at all, and the address, the number, the date and the reference number
each have a format.

**Fix:** read the refusal. It names the field and what was allowed, and the
model corrects itself on the next turn. To change what is accepted, change the
`type:` on that variable in `agent.yaml`. Rewording the description does not
work: no wording reliably stops a model sending nothing when there is nothing.

### The scripted run cannot find a key

`scripts/text_run_livekit.py` reads the package's own `.env` and no other one.

**Fix:** copy the repository's file into the package first.

```sh
cp .env examples/customer-intake/.env
```

### I added a `slng` target and the declared types are refused

That target is hosted. The check for a declared type is Python this compiler
emits, and a hosted target emits no project to run it in.

**Fix:** keep this package on the two targets `targets.yaml` names, `livekit`
and `pipecat`.

```sh
unmute compile examples/customer-intake --target livekit
```

## Where to go next

- [`salon-concierge`](../salon-concierge/) - the same types, in a full package
- [`salon-concierge-single-prompt`](../salon-concierge-single-prompt/) - none of the structure, to read against
- [All four examples](../README.md) - what each one is for
- [Variables](../../docs-site/build/variables.mdx) - every key a variable takes
- [Pre-fetch](../../docs-site/build/prefetch.mdx) - what runs before the greeting
- [Python tools](../../docs-site/build/tools/python.mdx) - the `local:` block this package uses

# salon-support

The support desk for Sage and Stone Salon: the smallest package that uses
variables and secrets, and the one to run first. You can talk to it in a browser
in about a minute, with nothing but two model keys.

Nothing external is needed. The channel is web audio, so there is no phone
number, no Twilio, and no tunnel. The tools are the same deterministic Python
fixtures the other salon examples use, so there is no API to stand up. Tracing is
off, so there are no Langfuse keys to find.

## Run it

You need `OPENAI_API_KEY` and `SLNG_API_KEY`, and Docker running.

```sh
bin/unmute validate examples/salon-support
```

```sh
bin/unmute compile examples/salon-support
```

```sh
cp examples/salon-support/build/pipecat/.env.example examples/salon-support/.env
```

Fill in the two values, then talk to it:

```sh
bin/unmute dev examples/salon-support --target pipecat --var customer_name=Ada
```

That opens a browser page with a mic button. Swap `--target pipecat` for
`--target livekit` to run the identical agent on the other driver; the LiveKit
run also starts a local LiveKit server and takes a little longer the first time.

## What to try on the call

**The greeting is personalized.** It opens with "Hi Ada" because of
`--var customer_name=Ada`. Restart without the flag and it says "Hi there": the
variable declares `default: there`, so the greeting always has something to say.

**The model never invents a customer id.** Ask to book something. When
`book_appointment` runs, the agent supplies only the slot; the customer id
arrives from the `customer_id` variable through the tool's `inject:` block, and
the model never sees it.

**Watch the model save what you tell it.** Say "I'd like a haircut". The agent
calls the generated `update_variables` tool and stores `requested_service`. You
never declared that tool: declaring a `source: conversation` variable is what
creates it.

**Try to break the order.** Ask it to book a slot *before* saying what service
you want. `book_appointment` injects `requested_service`, which has no default,
so the call is refused before any request is made and the agent asks you what
service you want instead. That is the guard, and it is the most interesting
thing here to watch.

## Where each piece lives

| Piece | File |
|---|---|
| The three variables and the two secrets | [agent.yaml](agent.yaml) |
| The personalized greeting | `conversation.greeting.text` in [agent.yaml](agent.yaml) |
| The templated prompt | [instructions.md](instructions.md) |
| The injected tool | [tools/book_appointment.yaml](tools/book_appointment.yaml) |

The reference pages are [variables](../../docs/user/reference/variables.md) and
[secrets](../../docs/user/reference/secrets.md); the design is in
[docs/spec/variable_secrets_specs.md](../../docs/spec/variable_secrets_specs.md).
For the same surface on a real outbound phone call, see
[outbound-reminder](../outbound-reminder/), which needs Twilio and a booking API.

# salon-support

The support desk for Sage and Stone Salon: the smallest package that uses
variables and secrets, and the one to run first. You can talk to it in a browser
in about a minute, with nothing but the values in `.env.example`.

Nothing external is needed. The channel is web audio, so there is no phone
number, no Twilio, and no tunnel. The tools are the same deterministic Python
fixtures the other salon examples use, so there is no API to stand up.

## Run it

You need Docker running and five values in `.env`: `OPENAI_API_KEY` and
`SLNG_API_KEY` for the models, plus `LANGFUSE_BASE_URL`, `LANGFUSE_PUBLIC_KEY`
and `LANGFUSE_SECRET_KEY`, because this package sets `tracing: langfuse`. The
generated `.env.example` lists all five.

```sh
bin/unmute validate examples/salon-support
```

```sh
bin/unmute compile examples/salon-support
```

```sh
cp examples/salon-support/build/pipecat/.env.example examples/salon-support/.env
```

Fill in the blanks, then talk to it:

```sh
bin/unmute dev examples/salon-support --target pipecat \
  --var customer_name=Ada \
  --var customer_id=cus_2002
```

That opens a browser page with a mic button. Swap `--target pipecat` for
`--target livekit` to run the identical agent on the other driver; the LiveKit
run also starts a local LiveKit server and takes a little longer the first time.

## Which variables you can seed

`--var` is the local stand-in for the dispatch payload a real deployment sends,
so it seeds the `source: call_start` variables only:

| Variable | Seedable | Where the value comes from |
|---|---|---|
| `customer_name` | yes | `--var`, else the declared `default: there` |
| `customer_id` | yes | `--var`, else the declared `default: cus_1001` |
| `requested_service` | no | the model saves it mid-call via `update_variables` |

Every seedable variable has a default, so you can leave both flags out and still
have a working call:

```sh
bin/unmute dev examples/salon-support --target pipecat
```

Two things worth knowing before you run it:

- Nothing seeds a `source: conversation` variable. `--var requested_service=...`
  is refused before the container starts, with an error pointing you at
  `update_variables`, so a seeded value can never be dropped in silence.
- One dev run per package at a time. The Compose project name comes from the
  package path, so a second `bin/unmute dev` on this example does not run beside
  the first: it recreates the same container on the new port and takes it over.
  If a run fails with `port is already allocated`, a container from an earlier
  session that was killed rather than stopped with ctrl-c is still holding the
  port; `docker rm -f` it and run again.

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

## How `inject:` puts a variable into a tool

The model never passes the customer id or the service. It cannot: neither name
appears in the tool's `input:` schema, and that schema is the whole of what the
model sees. [tools/book_appointment.yaml](tools/book_appointment.yaml) splits the
three arguments into one the model fills and two that ride along:

```yaml
input:
  type: object
  properties:
    slot_id:
      type: string
      description: ID returned by check_availability
  required:
    - slot_id

inject:
  customer_id: "{{customer_id}}"
  service: "{{requested_service}}"
```

So the model is offered a one-parameter function, `book_appointment(slot_id)`.
The `inject:` keys are handler arguments, not model parameters, which is why a
key that also names an `input:` property is a compile error: the model could
overwrite the value you were hiding from it.

Your handler in [tools/book_appointment.py](tools/book_appointment.py) takes all
three as ordinary arguments and never mentions a variable, a template or the call
state:

```python
def book_appointment(customer_id, service, slot_id):
```

The generated bot is what joins them. It renders each `{{token}}` from the live
call state at the moment of the call, then calls your function by keyword:

```python
result = tools.book_appointment.book_appointment(
    slot_id=slot_id,                  # from the model
    customer_id=state.customer_id,    # rendered from {{customer_id}}
    service=state.requested_service,  # rendered from {{requested_service}}
)
```

**The timing is the part worth internalizing.** A greeting or a prompt renders
once, at session start, so it may only name a variable that already has a value.
`inject:` renders on every call instead, which is exactly why
`{{requested_service}}` is legal here even though nobody has said it yet when the
call begins. It is also why a value the model saves through `update_variables` at
minute two is visible to a tool at minute three, with no prompt rewriting.

When an injected variable is still unset at call time, the tool refuses instead
of sending a half-formed request:

```python
refusal = _refusal("book_appointment", state, [("requested_service", "...")])
if refusal:
    await params.result_callback({"refused": refusal})
    return
```

The model receives `cannot call book_appointment yet: requested_service not set.
Ask the caller first.` and asks. `customer_id` never trips this, because it
declares `default: cus_1001` and so always has something to send.

## How secrets reach the call

Secrets do not interpolate at all. `{{...}}` is for variables only, and naming a
secret in a template is a compile error rather than a value that leaks at
runtime. Both of these fail `unmute validate`, with the file and line:

```
agent.yaml:74: conversation.greeting.text references {{OPENAI_API_KEY}}, but secrets
never flow through templates; a secret reaches a tool through its own *_env field
```

```
tools/book_appointment.yaml:22: tool "book_appointment" inject "customer_id" references
{{SLNG_API_KEY}}, but secrets never flow through templates; a secret reaches a tool
through its own *_env field
```

The reasoning is the destination. A template renders into the greeting, a
prompt, a tool argument, or a URL, so its value is spoken, logged, or traced.
That is right for a customer name and wrong for a token. Secrets travel by
environment variable instead, through exactly these seams:

| Seam | Used for |
|---|---|
| a tool's `webhook.url_env`, `webhook.auth.token_env`, `mcp.url_env` | calling an authenticated API |
| a model's `endpoint_env` | pointing a model at your own gateway |
| `os.environ` inside a `local:` handler | a handler that builds its own request |

For a worked version of the first and the last, running side by side, see
[examples/outbound-reminder](../outbound-reminder/README.md). The full
reference is [docs/user/reference/secrets.md](../../docs/user/reference/secrets.md).

This package deliberately uses none of the seams. Its tools are local fixtures
that need no credential, so `book_appointment.py` reads no environment at all,
and its five secrets are consumed by the runtime itself: two model keys and three
Langfuse values for tracing. Declaring them by name still buys you the startup
check, generated into `bot.py`:

```python
REQUIRED_ENV = [
    "LANGFUSE_BASE_URL",
    "LANGFUSE_PUBLIC_KEY",
    "LANGFUSE_SECRET_KEY",
    "OPENAI_API_KEY",
    "SLNG_API_KEY",
]
```

A missing value stops the container at startup and names what is missing, rather
than surfacing as a confusing failure on the first tool call or the first spoken
word.

## Where each piece lives

| Piece | File |
|---|---|
| The three variables and the two secrets | [agent.yaml](agent.yaml) |
| The personalized greeting | `conversation.greeting.text` in [agent.yaml](agent.yaml) |
| The templated prompt | [instructions.md](instructions.md) |
| The injected tool | [tools/book_appointment.yaml](tools/book_appointment.yaml) |

The reference pages are [variables](../../docs/user/reference/variables.md) and
[secrets](../../docs/user/reference/secrets.md); the design is in
[SCHEMA.md](../../docs/SCHEMA.md) sections 4.4 and 4.12.
For the same surface on a real outbound phone call, see
[outbound-reminder](../outbound-reminder/), which needs Twilio and a booking API.

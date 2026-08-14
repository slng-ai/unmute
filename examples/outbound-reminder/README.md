# outbound-reminder

An outbound reminder call for the Sage and Stone Salon, built to show every
runtime-value kind in one small package. It compiles and runs on both shipped
code targets, Pipecat and LiveKit, from the same source. The design is in
[SCHEMA.md](../../docs/SCHEMA.md) sections 4.4 (variables) and 4.12 (secrets).

Both targets here are **self-hosted** routes, chosen because this example is about
runtime values rather than about telephony: Pipecat on `transport:
carrier-websocket` (the container answers the carrier itself) and LiveKit on
`transport: connector` (the generated bridge puts the call in a local LiveKit
room). Its `dialed_number` variable is why: `source: to_number` is filled by the
carrier adapter, and those are the routes that emit one. The Pipecat routes that
host nothing refuse that source by name, so a package needing it belongs here. The
route comparison is in [docs/TELEPHONY.md](../../docs/TELEPHONY.md).

## The four kinds, and where each shows up

**Input variables** arrive with the dispatch, before the call rings:
`customer_id`, `name`, and `appointment_time` in `agent.yaml` carry
`source: call_start`. The greeting says "Hi {{name}}", the prompt in
[instructions.md](instructions.md) is personalized the same way, and every
booking tool injects `customer_id` into its request, so the model never sees or
invents it.

**System variables** are owned by the runtime: `dialed_number` carries
`source: to_number` and is filled by the telephony route, not by you.

**Conversation variables** are saved by the model mid call: `reschedule_to`
carries `source: conversation`. Because the package declares it, the agent gets
one generated tool, `update_variables`, whose schema is built from the
variable's type and description. The prompt tells the model to save the slot
the customer names, and `reschedule_appointment` injects the saved value as
`new_time`. If the model calls that tool before saving a slot, the call is
refused with a message telling it to ask the caller first, and nothing is sent.

**Secrets** are env names declared in the `secrets:` block, values never appear
in any file. `SALON_API_URL` is the webhook base URL, `SALON_API_TOKEN` rides
the bearer auth of the two webhook tools, `SALON_API_SIGNING_KEY` is read by the
one local handler, and the model keys are listed so the generated `.env.example`
and the startup check cover everything the runtime needs.

## Both ways a secret reaches a tool

The three tools are here to show the two shapes side by side. Full reference in
[secrets](../../docs-site/reference/secrets.mdx).

**Named in YAML**, for a webhook tool. `confirm_appointment` and
`reschedule_appointment` name their base URL and their token, and unmute writes
the request:

```yaml
webhook:
  url_env: SALON_API_URL
  path: /customers/{{customer_id}}/appointments/confirm
  auth:
    type: bearer
    token_env: SALON_API_TOKEN
```

**Read in Python**, for a local handler. `cancel_appointment` needs a signed
request, and `webhook.auth` only speaks bearer and api_key, so the handler
builds the signature itself and reads the key the ordinary way:

```python
key = os.environ["SALON_API_SIGNING_KEY"].encode()
```

There is no credential field on the `local:` block and nothing is passed into
the function. `unmute validate` reads the handler, finds the name, and warns if
`agent.yaml` never declared it:

```
Warnings:
  pipecat: environment variables referenced but not declared in secrets:
    SALON_API_SIGNING_KEY (tools/cancel_appointment.py os.environ)
```

Neither shape puts a value in a file, and neither is reachable from `{{...}}`:
a template renders into the greeting, a prompt, a tool argument, or a URL, and
all four are spoken, logged, or traced.

## Two connections, one Twilio account

This is the package that shows why a connection file is per **route** rather
than per account. Both targets use the same Twilio account, but they reach it
by different mechanisms — Pipecat over `carrier-websocket`, LiveKit over
`connector` — and a connection declares its own transport. So there are two
files, `connections/twilio_websocket.yaml` and `connections/twilio_connector.yaml`,
holding the same three environment names. Each target names one of them and says
nothing else about telephony.

## What you set, and what is set for you

The browser is the default way to test, and it needs none of the phone values
below: `bin/unmute dev examples/outbound-reminder --target pipecat` opens a
session with nothing but the model keys set. Add `--telephony` only when you
want a real call.

You set these:

| Variable | What it is |
|---|---|
| `OPENAI_API_KEY` | reasoning model |
| `SLNG_API_KEY` | listen and speak models |
| `SALON_API_URL` | the booking service the tools call |
| `SALON_API_TOKEN` | bearer token for that service |
| `SALON_API_SIGNING_KEY` | signs the webhook payloads |
| `TWILIO_ACCOUNT_SID` | Twilio REST account, named by both connections |
| `TWILIO_AUTH_TOKEN` | Twilio REST auth token |
| `TWILIO_PHONE_NUMBER` | the caller identity the recipient sees |

These reach the build without you:

| Variable | Who supplies it |
|---|---|
| `UNMUTE_PUBLIC_URL` | `unmute dev --telephony` sets it from the tunnel; the operator sets it at deploy time |
| `UNMUTE_OUTBOUND_TOKEN` | the outbound trigger token, generated for a local run |
| `REDIS_URL` | the generated Compose graph, which ships Valkey |
| `LIVEKIT_URL`, `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET` | the local Compose graph, or your LiveKit Cloud project (livekit target only) |

## Run it

Compile both targets, then copy either generated env template and fill it in.
The template lists every declared secret, then the names the target and the
connection need, grouped under one label:

```sh
bin/unmute compile examples/outbound-reminder
cp examples/outbound-reminder/build/pipecat/.env.example examples/outbound-reminder/.env
```

For a local run, input variables come from repeatable `--var` flags. Swap
`--target pipecat` for `--target livekit` to run the same agent on the other
driver:

```sh
bin/unmute dev examples/outbound-reminder --target pipecat --var customer_id=cus_1042 --var name=Ada --var "appointment_time=tomorrow at 3 pm"
```

Values are checked against their declared type, and a name the package never
declares is refused rather than quietly ignored.

In production the same three values ride the target's own dispatch payload as
one flat JSON object; each build's own README prints the exact spelling for its
driver.

## Deploy it

Both routes here host something of yours, which is what makes deploying them the
same job twice rather than two different jobs: a process that answers your carrier,
behind a public HTTPS origin, with the number's voice webhook pointed at it.
`build/pipecat/README.md` and `build/livekit/README.md` each carry their own
platform commands, required variables, and carrier steps. If you want a phone route
with nothing to host, that is a different transport, and
[docs/TELEPHONY.md](../../docs/TELEPHONY.md) compares them.

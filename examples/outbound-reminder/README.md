# outbound-reminder

An outbound reminder call for the Sage and Stone Salon, built to show every
runtime-variable source in one small package. It compiles and runs on both shipped
code targets, Pipecat and LiveKit, from the same source. Its three appointment
tools are deterministic local Python fixtures, so the call needs no booking API.
See the [variables reference](../../docs-site/reference/variables.mdx).

Both targets here are **self-hosted** routes that can place the outbound call
locally: Pipecat on `transport: carrier-websocket` and LiveKit on `transport:
connector`. Their carrier adapters also fill the `dialed_number` system
variable. The [telephony overview](../../docs-site/telephony/overview.mdx)
compares the routes.

## The three value sources

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
refused with a message telling it to ask the caller first, and the handler is
not called.

## Local appointment fixtures

`confirm_appointment`, `reschedule_appointment`, and `cancel_appointment` are
plain Python functions copied into both generated projects. They return a
successful deterministic result and make no network request. This keeps the
live acceptance test on its subject: placing one outbound call and carrying
dispatch, route, and conversation values through it.

The hidden `inject:` values still exercise the same compiler path. For example,
the reschedule fixture receives the dispatched customer id and the slot saved
mid-call, while the model sees neither as a tool argument:

```yaml
inject:
  customer_id: "{{customer_id}}"
  new_time: "{{reschedule_to}}"

local:
  handler: tools/reschedule_appointment.py
```

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
| `TWILIO_ACCOUNT_SID` | Twilio REST account, named by both connections |
| `TWILIO_AUTH_TOKEN` | Twilio REST auth token |
| `TWILIO_PHONE_NUMBER` | the caller identity the recipient sees |

The following values reach the build without you, and the generated `.env.example` does not
list them for exactly that reason. `compile-report.json` does, under
`required_env`, and the emitted `README.md` says where each one comes from:

| Variable | Who supplies it |
|---|---|
| `UNMUTE_PUBLIC_URL` | `unmute dev --telephony` sets it from the tunnel; the operator sets it at deploy time |
| `UNMUTE_OUTBOUND_TOKEN` | the outbound trigger token, generated for a local run |
| `REDIS_URL` | the generated Compose graph, which ships Valkey |
| `LIVEKIT_URL`, `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET` | the local Compose graph, or your LiveKit Cloud project (livekit target only) |

## Run it

Compile both targets, then copy either generated env template. Browser testing
needs the two model keys; a real outbound call also needs the three Twilio
values:

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

The real acceptance is an outbound phone call. Add the destination and run one
target at a time:

```sh
bin/unmute dev examples/outbound-reminder --telephony --target pipecat --to +15551234567 --var customer_id=cus_1042 --var name=Ada --var "appointment_time=tomorrow at 3 pm"
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
required variables and carrier steps. Only the LiveKit one offers a managed
platform: the Pipecat `carrier-websocket` route has no Pipecat Cloud path, so its
runbook has a **Deploy it yourself** section instead and the build emits no
`pcc-deploy.toml`. If you want a phone route
with nothing to host, that is a different transport; compare them in the
[telephony overview](../../docs-site/telephony/overview.mdx).

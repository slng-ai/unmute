# outbound-reminder

An outbound reminder call for the Sage and Stone Salon, built to show every
runtime-value kind in one small package. It compiles and runs on both shipped
code targets, Pipecat and LiveKit, from the same source. The design is in
[docs/spec/variable_secrets_specs.md](../../docs/spec/variable_secrets_specs.md).

## The four kinds, and where each shows up

**Input variables** arrive with the dispatch, before the call rings:
`customer_id`, `name`, and `appointment_time` in `agent.yaml` carry
`source: call_start`. The greeting says "Hi {{name}}", the prompt in
[instructions.md](instructions.md) is personalized the same way, and the two
booking tools inject `customer_id` into every request, so the model never sees
or invents it.

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
the bearer auth of both tools, and the model keys are listed so the generated
`.env.example` and the startup check cover everything the runtime needs.

## Run it

Compile both targets, then copy either generated env template and fill it in.
The template lists each declared secret with its description above it:

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

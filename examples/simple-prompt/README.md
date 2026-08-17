# simple-prompt

One agent, one prompt, five tools. This is the baseline the other three
structural examples are measured against: the same Sage and Stone Salon
workflow with nothing split out.

The whole workflow lives in [instructions.md](instructions.md), and the single
agent in [agent.yaml](agent.yaml) owns every tool:

```yaml
agents:
  appointment_desk:
    instructions: instructions.md
    model: reasoning
    voice: voice
    tools:
      - lookup_customer
      - create_customer
      - check_availability
      - book_appointment
      - cancel_appointment
```

Both code targets are declared, browser audio only. There is no phone number,
no carrier, and no connection: `channels:` is `web: realtime_audio`.

## Run it

You need Docker running and the keys the generated `.env.example` lists:
`OPENAI_API_KEY` and `SLNG_API_KEY` for the models, plus `LANGFUSE_BASE_URL`,
`LANGFUSE_PUBLIC_KEY` and `LANGFUSE_SECRET_KEY`, because this package sets
`tracing: langfuse`.

```sh
bin/unmute validate examples/simple-prompt
```

```sh
bin/unmute compile examples/simple-prompt
```

```sh
cp examples/simple-prompt/build/pipecat/.env.example examples/simple-prompt/.env
```

Fill in the blanks, then talk to it:

```sh
bin/unmute dev examples/simple-prompt --target pipecat
```

Swap `--target pipecat` for `--target livekit` to run the identical agent on
the other driver.

## Optimize reasoning with the Context Router

The checked-in package uses OpenAI directly for reasoning. Keep that default
for the shortest first run. To route the same upstream model through SLNG,
replace only the `models.think.reasoning` entry with:

```yaml
    reasoning:
      provider: slng
      model: slng/auto
      slng:
        region: eu
        agent_id: sage-stone-v1
        upstream:
          name: luna
          provider: openai-responses
          url: https://api.openai.com/v1
          api_key_env: OPENAI_API_KEY
          model_id: gpt-5.6-luna
```

`SLNG_API_KEY` authenticates the router and `OPENAI_API_KEY` authenticates your
upstream model. Both names are already in this example's `secrets` list. Choose
`india`, `eu`, `us`, or `indonesia`; Unmute builds the router URL from that
region. Use `model: luna` only when you want to target the inline entry instead
of router selection.

SLNG is the optimization layer, not the model provider. Unmute generates one
cache-enabled inline tier, stable agent identity, and one session UUID per
call. If `instructions.md` contains declared `{{name}}` variables, the raw
placeholders reach the router and the matching values travel separately in
`template_variables`. Generated applications do not send warm-up requests, and
only an SLNG cache marker proves a hit.

Both checked-in target versions meet the Context Router minimums. See the
[complete Context Router guide](../../docs-site/models/context-router.mdx) for
the `openai-compat` form, prompt scopes, cache rules, and limitations. For a
package that already uses all of those features, run the dedicated
[SLNG Context Router example](../slng-context-router/README.md).

## What to look at

**One prompt carries the whole workflow.** The customer gate, the booking
workflow, the rescheduling workflow, and the cancellation workflow are all
sections of one Markdown file. That works, and it is the honest starting point
for most agents.

**The tools are plain.** Each tool is a pair of files: a YAML contract the
model sees and a Python function that runs.
[tools/check_availability.yaml](tools/check_availability.yaml) declares two
parameters and an `enum` that stops the model inventing a service:

```yaml
input:
  type: object
  properties:
    service:
      type: string
      enum:
        - haircut
        - hair-color
        - blowout
    date:
      type: string
      description: Preferred date in YYYY-MM-DD form
  required:
    - service
    - date

local:
  handler: tools/check_availability.py
```

The handler in [tools/check_availability.py](tools/check_availability.py) is an
ordinary function with those two arguments. It needs no credential and no
network: the five tools here are deterministic fixtures.

**Where it stops scaling.** Everything the model can do is offered on every
turn, and every rule is in one prompt. When the workflow grows, the prompt is
the thing that gets fragile. The three examples below are the three ways out.

## Compare with

| Package | What changes |
|---|---|
| [multi-task](../multi-task/README.md) | The same tools, split into two tasks the agent delegates to. |
| [task-groups](../task-groups/README.md) | Three ordered tasks with shared context. |
| [subagents](../subagents/README.md) | Two agents that hand the caller back and forth. |

Reference pages: [agent.yaml](../../docs-site/reference/agent-yaml.mdx),
[tools](../../docs-site/build/tools/overview.mdx).

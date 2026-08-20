# optimized-salon-concierge

The [salon-concierge](../salon-concierge/README.md) project with its thinking
behind the SLNG Context Router. Everything else is the same package: the same
prompts, the same verification task, the same booking task group, the same
specialist handoffs, the same cold manager transfer, the same local SQLite tools,
the same browser audio, and the same inbound-only phone routes on both code
targets.

The two exist as a matched pair. Run the same conversation through both and the
difference you measure is the router's.

## What the router does

It sits in front of your own model. On every turn it decides whether it has
answered this turn before. If it has, it replies from its cache in around a tenth
of the model's time. If it has not, it calls your model and answers as normal.

Three things follow from that, and they are the whole reason this package is
shaped the way it is:

- **The prompt has to be the same every call.** So the system prompt travels to
  the router with its `{{customer_id}}` placeholders intact, and the values for
  this call travel separately. Nothing renders the prompt locally any more. Look
  in the generated `bot.py` or `agent.py` and you will see the prompt constant
  still holding its braces, next to a `template_variables` map.
- **The cache is scoped by `agent_id`.** One stable value for this whole package,
  written by hand. Bump the version suffix when you change a prompt enough that
  you want the old answers gone. Nothing else bumps it for you, and nothing else
  should: a derived id would throw the cache away on a typo fix.
- **A first turn never caches**, and the router decides which later turns are
  repeatable. A repeated turn served by the model is expected, not a fault.

## What changed from the baseline

Eight lines in `agent.yaml`, in the think block:

```yaml
  think:
    reasoning:
      provider: slng                              # was openai
      model: gpt-5.6-luna                         # named directly
      agent_id: optimized-salon-concierge-v1      # yours, stable, versioned
      upstream:
        provider: openai                          # who actually serves the model
      params:
        world_part_override: eu                   # the router region
        reasoning_effort: "none"                  # keep this: the agent has tools
```

And one deletion in `targets.yaml`: the baseline pins its LiveKit target to
OpenAI's Responses API, and the router speaks Chat Completions, so that override
goes. If your package has no such override, the think block is the entire edit.

`upstream` is required and has no default, because your provider credentials
travel to SLNG in the body of every think request. That is the price of
configuring the model inline, and it should be a decision you make rather than
one you discover by reading generated Python. The accepted upstreams are
`openai`, `openai-compat`, `azure`, `vertex` and `bedrock`. The OpenAI-compatible
kind, which covers the first two, was validated against the live router on
2026-08-19; the other three come from the router team's published field list and
have not been run here. See
[the router page](../../docs-site/models/context-router.mdx) for each one's
fields.

`reasoning_effort: "none"` is not optional in practice. This agent has tools, and
without it every tool turn on the reasoning models in the GPT-5 family comes back
as a 400. The compiler warns when it is missing.

## What you need

Common values:

| Name | Purpose |
|---|---|
| `SLNG_API_KEY` | the router, and the speech and transcription models |
| `OPENAI_API_KEY` | the upstream that serves the model, and what pays for the tokens |
| `LANGFUSE_BASE_URL` | Langfuse API base URL |
| `LANGFUSE_PUBLIC_KEY` | Langfuse project public key |
| `LANGFUSE_SECRET_KEY` | Langfuse project secret key |

One SLNG key serves all three SLNG roles here: listen, speak, and now think.
`OPENAI_API_KEY` needs a value but no `secrets:` line, because the compiler
supplied the name. A credential name you write yourself always needs the line.

`MANAGER_PHONE_NUMBER` is the cold-transfer destination in E.164 form. It is
needed only for inbound phone manager transfers, may stay unset for browser
sessions, and is checked before a phone caller hears the greeting.

The Pipecat target uses the `cloud-websocket` transport and also needs
`TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, and `TWILIO_PHONE_NUMBER` for a real
inbound call and transfer. `PIPECAT_CLOUD_ORGANIZATION` is supplied by the route
when deployed, not declared by the package. This route needs no `DAILY_API_KEY`.

The LiveKit target uses the `sip` transport and also needs
`SIP_TRUNK_HOSTNAME`, `SIP_AUTH_USERNAME`, `SIP_AUTH_PASSWORD`, and
`SIP_FROM_NUMBER`. Local development supplies `LIVEKIT_URL`, `LIVEKIT_API_KEY`,
`LIVEKIT_API_SECRET`, and `REDIS_URL`; LiveKit Cloud or your operator supplies
them after deployment.

Secrets stay in `.env`. No credential or phone number belongs in this package.
Keep `UNMUTE_LOG_LEVEL=INFO` for normal runs.

## Validate and compile

```sh
unmute validate examples/optimized-salon-concierge
unmute compile examples/optimized-salon-concierge
```

The compile report names both things the compiler consumed rather than forwarded:

```
pipecat: binding reason.reasoning provider=slng model=gpt-5.6-luna (SLNG Context Router; world_part_override is consumed into base_url=https://eu.context-router.slng.ai/v1)
pipecat:   param world_part_override=eu (consumed as the router base URL)
pipecat:   upstream openai url=https://api.openai.com/v1 api_key=OPENAI_API_KEY (env) (sent inline in the request body)
```

The upstream line names the variable, never its value. No generated file holds a
credential either.

## Talk in the browser

```sh
unmute dev examples/optimized-salon-concierge --target pipecat
unmute dev examples/optimized-salon-concierge --target livekit
```

## Measuring against the baseline

Hold the same conversation on both packages, on both targets, and include a turn
you repeat later in the same call. A first turn never caches, so a script with no
repeat measures nothing.

Per turn, record:

| What | Why |
|---|---|
| time from the end of your speech to the agent starting to speak | the number you came for |
| the `x-slng-response-source` response header, `llm` or `cache` | which path answered |
| `x-slng-cache-layer`, when present | which cache layer |
| `x-slng-model`, when the source is `llm` | which model answered |
| whether the answer was right | speed is not the only thing that changed |

Expect the first turn of every call on the model path at full latency. Expect a
repeated turn the router judges cacheable to come back in roughly a tenth of the
time. Expect some repeated turns never to cache, indefinitely: that is the router
deciding, and it is not a fault. Tool turns always take the model path, both the
request turn and the result turn.

Streamed responses report no usage on either path, so token savings cannot be
read off the stream.

## Everything else

The call flow, the local data, the booking confirmation boundary, the empty
LiveKit task response behaviour, the phone runtimes, and the release conversation
script are all unchanged. They are documented once, in
[salon-concierge/README.md](../salon-concierge/README.md).

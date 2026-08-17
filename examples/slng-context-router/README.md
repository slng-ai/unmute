# SLNG Context Router

This is the runnable package used by Unmute's Context Router regression tests.
It uses SLNG as the optimization layer around an inline OpenAI Responses model
and compiles unchanged for Pipecat and LiveKit.

The package covers the prompt scopes that matter for cache isolation:

- the `front_desk` entry agent;
- the `specialist` handoff agent;
- the `standalone` delegated task;
- the `group_step` task, reused by two task groups.

Each scope keeps its raw `{{variable}}` prompt and sends only its referenced
values through `template_variables`. The generated request uses a stable derived
agent ID and one fresh session UUID per call.

## Configure it

Keep the two values in the ignored repository-root `.env`:

```dotenv
SLNG_API_KEY=...
OPENAI_API_KEY=...
```

The example defaults to the EU router and `model: slng/auto`. Edit
`slng.region` to use `india`, `us`, or `indonesia`. Set `model: luna` to target
the inline entry directly.

## Validate and compile

```sh
make build
bin/unmute validate examples/slng-context-router
bin/unmute compile examples/slng-context-router
```

The generated projects appear under `build/livekit` and `build/pipecat`.

## Run Pipecat

```sh
bin/unmute dev examples/slng-context-router \
  --target pipecat \
  --var caller_name=Nico \
  --var request_topic=router-caching \
  --var account_tier=gold \
  --verbose
```

## Run LiveKit

Stop Pipecat first, then run the same package on LiveKit:

```sh
bin/unmute dev examples/slng-context-router \
  --target livekit \
  --var caller_name=Nico \
  --var request_topic=router-caching \
  --var account_tier=gold \
  --verbose
```

Both targets use browser `realtime_audio`. Try these requests in order:

1. “Give me a short standalone summary.”
2. “Transfer me to the specialist.”
3. “Run the first grouped review.”
4. “Run the second grouped review.”
5. “Return me to the front desk.”

For a cache probe, stop the call, start a fresh call with the same variables,
and repeat the same wording. A hit is proven only by `cached: true`, a nonempty
`cache_layer`, or a nonempty `X-Cache-Layer` response header. OpenAI
`cached_tokens` alone is upstream prompt caching, not an SLNG Context Router
hit. Standard generated logs may not expose router response headers, so absence
of a marker in those logs is “not proven,” not proof that caching is disabled.

See the [Context Router guide](../../docs-site/models/context-router.mdx) for
regions, identity, BYOK, cache rules, and limitations.

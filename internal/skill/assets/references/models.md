# Models

Which vendor listens, speaks, and thinks, and which ones each target accepts.

## The defaults

Unless the user asks for something else:

| Role | Provider | Notes |
|---|---|---|
| listen | `slng` | speech to text |
| speak | `slng` | text to speech |
| think | `openai` | the reasoning model |
| turn | `local` with `silero` on Pipecat, `livekit` with its own detector on LiveKit | each target runs the one it is best at |

```yaml agent.yaml
models:
  think:
    reasoning:
      provider: openai
      model: gpt-5.6-terra
      params:
        reasoning_effort: "none"
  speak:
    voice:
      provider: slng
      model: "slng/deepgram/aura:2-en"
      voice: "aura-2-thalia-en"
  listen:
    transcriber:
      provider: slng
      model: "slng/deepgram/nova:3-en"
  turn:
    detector:
      provider: local
      model: silero
```

On a LiveKit target, override the turn entry rather than changing the agent.

**The override is keyed on the entry name in your own package, not on the name
below.** Entry names are yours to choose, and `unmute init` chooses its own: it
writes `assistant_model`, `assistant_voice`, `transcriber`, and one turn entry
named after the target it scaffolded. A scaffolded livekit package calls it
`detector` and already binds LiveKit's own detector to it, so it needs no
override at all. A pipecat package usually calls it `vad`. Overriding a name the
package does not define fails, cleanly but needlessly:

```
targets.yaml:11: target "livekit" overrides "detector", which is not a defined model
```

Read the `models:` block you actually have before you write the override.

```yaml targets.yaml
targets:
  livekit:
    provider: livekit
    version: "1.6.10"
    sdk_language: python
    models:
      detector:
        provider: livekit
        model: turn-detector-mini
```

If the user names their own vendor, use it. Check the table below first, and
say what you bound.

## Fields by model section

| Field | Legal section |
|---|---|
| `provider`, `model`, `endpoint_env`, `placement`, `params`, `description` | `think`, `speak`, `listen`, `turn` |
| `voice`, `speed` | `speak` |
| `language` | `speak`, `listen` |
| `temperature`, `top_p`, `top_k` | `think` |
| `semantic_endpointing` | `turn`: `required`, `preferred`, or `off` |
| `fallback` | `think`, `listen` |

A target and vendor may narrow this further. For example, validation rejects
`language` when that integration has no language slot.

## The default OpenAI think model needs `reasoning_effort`

Keep it on the scaffold's `gpt-5.6-terra` entry:

```yaml
      params:
        reasoning_effort: "none"
```

`gpt-5.6-terra` is a reasoning model, and OpenAI rejects a chat completions
request that carries function tools unless the request also sets
`reasoning_effort`. Leaving it out is not the same as leaving it alone: the
server applies its own default and every turn comes back as HTTP 400. Nearly
every agent has tools. This is an OpenAI model setting, not a field to copy to
other providers.

This model takes `none`, `low`, `medium`, `high`, and `xhigh`. It rejects
`minimal`. Use `none` unless the user asks for more thinking.

You write the same line in a shared profile for both targets. On LiveKit it becomes a constructor
argument to `openai.LLM`. On Pipecat the settings class has no field for it, so
the compiler puts it in the service's `extra` field, which Pipecat merges into
the request body as written. That is the normal rule for `params:`: a name the
target's settings object has no field for rides the target's overflow field
instead of being dropped. The LiveKit Responses mode below is the narrow
compiler-owned exception.

On LiveKit there is a second reason to be explicit. `livekit-plugins-openai`
1.6.10 injects `reasoning_effort="minimal"` by itself for several older ids in
the same GPT-5 family, and that is the same 400 once the agent has tools.

## The SLNG Context Router as the think provider

`provider: slng` on a `think` entry puts the SLNG Context Router in front of the
user's own model. It caches the turns the agent has answered before and serves
them without calling the model, so a repeated turn returns in roughly a tenth of
the time. The user keeps their model, their provider, and their bill for the turns
that reach the model.

```yaml agent.yaml
secrets:
  - SLNG_API_KEY

models:
  think:
    reasoning:
      provider: slng
      model: gpt-5.6-luna
      agent_id: salon-concierge-v1
      upstream:
        provider: openai
      params:
        world_part_override: eu
        reasoning_effort: "none"
```

Four things are required and none has a default:

- `model`, named directly. There is no auto-select spelling to write here.
- `agent_id`, which scopes the router's cache. One stable value per package,
  written by a human, carrying a version suffix they bump when a prompt change
  should make old answers wrong. Never derive it, never hash prompts into it,
  never compose it from an agent or task name: a split id splits the cache and
  nothing fails, the agent is simply never fast. Two think profiles disagreeing
  about it is a compile error. It must be printable ASCII with no whitespace,
  because it becomes an HTTP header value.
- `upstream`, saying who actually serves the model.
- `params.world_part_override`, from the router's own region set: `eu`, `us`,
  `india`, `indonesia`. **Not the speech world parts** `na`, `eu`, `ap`: the same
  key, a different accepted set per role. The compiler consumes this into the
  base URL and names the substitution in the compile report.

`params.reasoning_effort: "none"` is not optional once the agent has tools, for
the same reason as a direct OpenAI binding above. The compiler warns rather than
refusing, because it cannot know the upstream model family for certain.

`endpoint_env` has no slot on a router binding: the region owns the router URL and
`upstream` owns the upstream one.

### The upstream block

| `provider` | required | optional |
|---|---|---|
| `openai` | nothing else | `url`, `key_env` |
| `openai-compat` | `url`, `key_env` | `auth_header` |
| `azure` | `url`, `key_env`, `deployment`, `api_version` | nothing |
| `vertex` | `credentials_env`, `location` | `project` |
| `bedrock` | `access_key_id_env`, `secret_access_key_env`, `region`, `model_id` | `session_token_env` |

Five spellings over four kinds of upstream. The OpenAI-compatible kind was
validated against the live router on 2026-08-19, covering `openai` and
`openai-compat`; `azure`, `vertex` and `bedrock` come from the router team's
published field list and have not been run. Say so if the user asks.

Three rules keep a package free of secrets, and they are gates rather than
advice:

- A credential field is always named `*_env` and holds an **environment variable
  name**. Writing a value there is a refusal, and the refusal does not echo the
  value back.
- Every name the author writes must appear in `secrets:`, or the build fails. A
  name the compiler supplied, like `OPENAI_API_KEY` on `provider: openai`, needs
  no line and is never demanded.
- No field mixes a literal with an environment value, which is why `auth_header`
  carries only a header name and the key still comes from `key_env`.

A key the table does not expect for that provider is a refusal, not a
pass-through, because the router answers an unknown endpoint field with a 400 on
every think request.

`vertex`'s `credentials_env` may hold the key JSON, that JSON base64 encoded, or a
path to the key file. The generated agent decides which at startup and fails at
boot on a malformed value.

**Tell the user where their credentials go.** The model configuration travels
inline in the body of every think request, so the upstream credentials are sent to
SLNG on every turn. No package, generated file, or compile report holds a value,
and every name joins the generated startup check, but the inline path is a trust
decision the author is making rather than an implementation detail.

### What the router does and does not promise

- A first turn never caches. There is no preceding pair yet.
- The cache key is the pair (previous assistant reply, current user message),
  scoped by the agent id.
- The router decides which turns are repeatable, and some repeats never cache. A
  repeat served by the model is expected, not a fault. Do not promise the user
  that every repeat is fast.
- Tool turns always take the model path, both the request turn and the result
  turn.
- A router-bound system prompt keeps its `{{name}}` placeholders and the values
  travel beside it, which is what makes a personalised prompt cacheable.
  Greetings, tool arguments, injected values and webhook paths keep rendering
  locally.
- Streamed responses carry no usage on either path, so token savings cannot be
  read off the stream.
- Three response headers are the only way to check which path answered:
  `x-slng-response-source`, `x-slng-cache-layer`, `x-slng-model`.

Full page: [Context Router](https://docs.slng.ai/context-router/).
`examples/salon-concierge` is the shipped package behind it. That package also
sets `slng_pure_proxy: true` in the think params: the cache key is the last
(assistant, user) exchange with no system prompt in it, so a package with
several agents behind one `agent_id` can be served another agent's answer.
Pure proxy keeps the cache writes and suppresses the serving. Author it on any
multi-agent package until each emitted agent sends its own id; a single-agent
package does not need it.

## Keep SLNG speech models in region

Put regional routing in the SLNG model's `params:`. The same YAML works for
listen and speak models on both generated targets:

```yaml agent.yaml
models:
  listen:
    transcriber:
      provider: slng
      model: "slng/deepgram/nova:3-multi"
      params:
        world_part_override: eu
        region_override: eu-north-1
  speak:
    voice:
      provider: slng
      model: "slng/deepgram/aura:2-en"
      voice: "aura-2-thalia-en"
      params:
        world_part_override: eu
        region_override: eu-north-1
```

`world_part_override` chooses a broad geography. `region_override` pins an
exact SLNG model region. If both are present,
`region_override` takes precedence over `world_part_override`. A world part is
not a country boundary, so use the exact region when hard data isolation
matters. Use a value from the
[SLNG region reference](https://docs.slng.ai/region-override); Unmute forwards
these provider params as written and does not validate them.

The generated runtime also has to support these params:

- LiveKit needs `livekit-plugins-slng` 1.6.7 or newer. See the
  [SLNG LiveKit plugin guide](https://docs.slng.ai/agents/livekit-plugin).
- Pipecat needs `pipecat-slng` 0.4.0 or newer. See the
  [SLNG Pipecat plugin guide](https://docs.slng.ai/agents/pipecat-plugin).

These settings choose where SLNG runs STT and TTS. They do not choose where the
agent worker runs; set that separately with `deployment_region` in
`targets.yaml`. See `package.md` for the deployment rules.

### LiveKit Responses API

When a package needs OpenAI's Responses API on LiveKit, override the shared
model inside that target:

```yaml
models:
  reasoning:
    provider: openai
    model: gpt-5.6-terra
    params:
      api: responses
      reasoning_effort: none
      use_websocket: true
```

This emits `openai.responses.LLM` and, when present, maps `reasoning_effort` to
the nested reasoning setting. Use that field instead of a raw `reasoning` map.
Keep the override target-local so Pipecat retains its normal OpenAI request
shape. The salon concierge is the working source example.

`use_websocket: true` belongs in every voice package that reaches OpenAI. With
HTTP each model call in a turn opens its own TLS connection, and a turn that
calls a tool makes at least two, so the handshake lands in the silence the
caller is sitting through.

## The vendors, per target per role

SLNG leads every list it appears in. These are the built-in `provider:` values
the catalogue holds.

| Target | Role | Vendors |
|---|---|---|
| pipecat | listen | `slng`, `assemblyai`, `cartesia`, `deepgram`, `elevenlabs`, `gradium`, `openai`, `soniox`, `speechmatics` |
| pipecat | speak | `slng`, `cartesia`, `deepgram`, `elevenlabs`, `gradium`, `inworld`, `openai`, `rime`, `sarvam`, `soniox` |
| pipecat | think | `slng`, `anthropic`, `deepseek`, `google`, `groq`, `mistral`, `openai`, `openrouter`, `qwen` |
| livekit | listen | `slng`, `assemblyai`, `cartesia`, `deepgram`, `elevenlabs`, `gradium`, `sarvam`, `soniox`, `speechmatics` |
| livekit | speak | `slng`, `cartesia`, `deepgram`, `elevenlabs`, `gemini`, `gradium`, `inworld`, `rime`, `sarvam`, `soniox` |
| livekit | think | `slng`, `anthropic`, `aws`, `azure`, `groq`, `mistralai`, `openai`, `openrouter`, `sarvam` |

Read that table carefully rather than from memory. The two targets do not hold
the same set, and the same company can appear under a different name: LiveKit
takes `mistralai` where Pipecat takes `mistral`.

The table is not a closed list where a target has a wildcard integration:

- Pipecat accepts an unlisted listen, speak, or think provider only with
  `endpoint_env`, through its OpenAI-compatible integration.
- LiveKit accepts an unlisted think provider through LiveKit Inference. Its
  listen and speak rows remain closed to the values above.

A wildcard is a route, not proof that a name exists. On Pipecat, use it only
when the user supplies an OpenAI-compatible endpoint. On LiveKit, use it only
for a provider and model the user supplied or LiveKit Inference documents.
Never invent a provider name.

**Turn detection has no vendor list.** Neither target has catalogue entries for
the `turn` role, because turn detection is a mechanism each target ships rather
than a vendor you bind. Pipecat runs Silero locally; LiveKit runs its own turn
detector. Use `provider: local` or `provider: livekit`. What each one does, and
what to listen for when it is wrong, is in `conversation.md`.

If a user asks for an unlisted vendor and no wildcard applies, say plainly that
it is not available for that role on that target, and name what is. Do not guess
at a spelling or bind it anyway and hope.

## Model ids are forwarded, not checked

```yaml
      provider: openai
      model: gpt-5.6-terra
```

Unmute keeps no allowlist of model ids, with exactly one exception: a LiveKit
`turn` model must be `turn-detector-mini` or `turn-detector`, because those are
loaded by name rather than sent to a provider. Everywhere else `model:` and
`voice:` go to the provider exactly as written. Most `params:` do too. The
narrow exception is the LiveKit OpenAI `api: responses` directive above, which
selects the Responses client and maps `reasoning_effort` to nested reasoning. A
model typo becomes a provider error on the first call, not a compile error, and
the compile report says so:

```
pipecat: binding reason.reasoning provider=openai model=gpt-5.6-terra (forwarded as-is, not validated)
```

So when you pick a model id, pick one the user named or one from that vendor's
own documentation. Do not invent an id that looks plausible.

Do not guess model ids, voice ids, or params.

Two families appear in this repository's own documentation, and only two: the
SLNG listen and speak ids, and `gpt-5.6-terra` for think. They are the ones
proven here. Any other id is the user's to name.

## Alternates and fallbacks

Entries you do not reference are legal alternates. Declaring more than you use
costs nothing and makes a swap one line:

```yaml
models:
  speak:
    voice:
      provider: slng
      model: "slng/deepgram/aura:2-en"
      voice: "aura-2-thalia-en"
    backup_voice:
      provider: elevenlabs
      model: eleven_turbo_v2_5
```

`fallback:` on an entry names other entry names to fall back to. It is not
supported everywhere: `unmute validate` tells you per target, and a warning
there is worth reading rather than skipping.

With two or more `listen` or `turn` entries, a top level `listen:` or `turn:`
line has to name the one in use. With one entry each, nothing needs selecting.

## Where the keys live

Remote providers need credentials, listed as environment variable names in
`secrets:`, never values. Local turn detection needs no credential:

```yaml
secrets:
  - OPENAI_API_KEY
  - SLNG_API_KEY
```

`endpoint_env` holds the name of a variable with a custom endpoint, for a
self-hosted or regional deployment. Same rule: a name, never a URL.

## Speed, cost, and quality

When a user says the agent feels slow, do not reach for a different model
first. The answer is usually turn detection or the greeting, not the LLM, and
`conversation.md` covers both. Change one thing, listen to it, and say what you
changed.

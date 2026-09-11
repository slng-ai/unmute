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
      model: "deepgram/aura:2"
      voice: "aura-2-thalia-en"
  listen:
    transcriber:
      provider: slng
      model: "deepgram/nova:3"
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
| `pace` | `turn`: `snappy`, `balanced`, or `patient`. Defaults to `balanced`. No per-target override |
| `endpointing_delay` | `turn`: a positive duration. The floor, and only the floor |
| `eager` | `turn`, Pipecat only, with `provider: listen`: answer the transcriber's predicted end of turn before it is confirmed. Off unless set |
| `fallback` | `think`, `listen` |
| `name`, `provider`, `model`, `voice`, `think`, `description` | `realtime`, Pipecat only, and nothing else: every other field is refused on a live entry by name |

A target and vendor may narrow this further. For example, validation rejects
`language` when that integration has no language slot.

### One live model that listens, thinks and speaks (Pipecat)

`models.realtime` binds one model that does the work of `listen`, `think` and
`speak` together: it hears the caller's audio directly, decides what to say and
when, and speaks in its own voice. It is a **list** of entries carrying `name:`,
and an agent names one with `realtime:` in place of `think:` and `speak:`.
`openai` is the one vendor, and `gpt-live-1` its live model.

```yaml
models:
  realtime:
    - name: live
      provider: openai
      model: gpt-live-1
      voice: marin
      think: fast
  think:
    fast:
      provider: openai
      model: gpt-5.6-terra

agents:
  desk:
    instructions: instructions.md
    realtime: live
    tools:
      - lookup_customer
```

`think:` on the realtime entry names a `models.think` entry with
`provider: openai` and no `endpoint_env`; the live model hands tools and hard
reasoning to it on OpenAI's Responses API, inside the same live session, and
keeps talking while it works. An agent with tools and no `think:` is refused.
That backend must be at OpenAI for the same reason: the handover happens inside
the session the live model already holds. The greeting reaches the
session as its opening instruction and the model paraphrases it, so tell the
user the sense of the line is kept and not its letters.

Write a live package only when the user asks for speech to speech, and say what
it cannot carry in this version, because each is refused at validate:

- a live model **serves one agent**, with no `tasks`, no `task_groups`,
  no `handoffs` and no `escalations`: the session fixes its instructions when it
  starts, so nothing may change them mid-call;
- no `listen`, `speak` or `turn` sections, and no `conversation.interruption`:
  the model listens, speaks and decides the turn itself;
- no `variables` and no `prefetch`: the live shape carries no call state yet;
- no `tracing`: the live shape has no traced worker yet;
- no `mcp` tool: nothing in the live shape can start and close a server
  connection yet;
- no telephony connection: a live model compiles for the browser route in this
  version;
- no `temperature`, `language`, `speed`, `params`, `pace` or `endpoint_env` on
  the entry, and no per-target override of it;
- the `think` backend must be at OpenAI, and the target must be Pipecat, because
  LiveKit and slng refuse the binding by name.

`conversation.inactivity` still works: the nudge is put to the model in its own
words and `end_after` ends the call.

### Let the transcriber decide the turn (Pipecat)

A `turn` entry's `provider` is `local` (the on-device pair) or, on Pipecat,
`listen`: the listening model's own turn detection ends the turn and no local
analyzer is built. Only two listeners can take it, and both can predict a turn
before it is final: Deepgram with a `flux-` model (`flux-general-en`) and
Cartesia with an `ink-` model (`ink-2`, not `ink-whisper`). Any other listening
vendor is refused naming these two.

```yaml
models:
  listen:
    transcriber:
      provider: deepgram
      model: flux-general-en
      language: en
  turn:
    detector:
      provider: listen
      eager: true
      pace: snappy
```

`pace` still applies: the ceiling becomes the transcriber's own end-of-turn
timeout in milliseconds (`eot_timeout_ms` on Flux, `turn_end_timeout_ms` on
Turns; snappy 1200, balanced 1600, patient 3000). There is no floor, so
`endpointing_delay`, `semantic_endpointing` and `interruption.minimum_words` are
refused; `interruption.protect` still works. `eager: true` costs one model
request per prediction, including the ones the transcriber withdraws, so leave
it off unless the user wants the faster reply. `eager` beside `provider: local`
is refused, and `provider: listen` is refused on LiveKit and slng.

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
  should make old answers wrong. Never derive it and never hash prompts into it.
  Two think profiles disagreeing about it is a compile error. It must be
  printable ASCII with no whitespace, because it becomes an HTTP header value.

  **You write one value; the compiler sends one scope per prompt.** Each agent
  and each task reaches the router as `<agent_id>:<its own name>`, and a task as
  `<agent_id>:task.<its own name>`. Do not write those yourself and do not try to
  give an agent its own `agent_id`: the compiler composes them from the package's
  names, identically on both targets. The reason is measured. The router's cache
  key is the last exchange and carries no system prompt, so two agents under one
  scope get served each other's lines: a booking specialist's opening turn came
  back as the concierge's "what phone number should I use", from cache, with no
  model call.

  Two things to tell an author. Two agents whose instructions are identical still
  get two scopes and stop sharing warmth. And an existing single-agent package
  goes cold once on upgrade, because a bare `agent_id` and `agent_id:name` are
  different scopes.

  The 128-character bound is checked on the finished scope, not on `agent_id`
  alone, so a long id plus a long agent name can be refused where the id by
  itself passed. The refusal names the agent or task that produced the long value.
- `upstream`, saying who actually serves the model.
- `params.world_part_override`, from the router's own region set: `eu`, `us`,
  `india`, `indonesia`. Speech gateways use a different set under `params.world_part`,
  such as `in` for India. The compiler consumes this into the base URL and names
  the substitution in the compile report.

`params.reasoning_effort: "none"` is not optional once the agent has tools **when
the upstream serves OpenAI's own models**, for the same reason as a direct OpenAI
binding above. The compiler warns rather than refusing, and only on the `openai`
and `azure` upstreams. Do not add it on an `openai-compat` upstream, and do not add
it defensively: there it ranges from useless to fatal depending on the host. Measured
on `qwen/qwen3-32b`, 2026-08-27: Nebius answers a request carrying it with a 400,
Groq accepts it with a 200 and ignores it. Same model, same param, one loud failure
and one silent one.

`endpoint_env` has no slot on a router binding: the region owns the router URL and
`upstream` owns the upstream one.

### Everything else under `params:` rides the request body

The compiler consumes two names: `world_part_override` becomes the router's base
URL and `slng_pure_proxy` is the router's shadow-trial switch. It forwards the rest
in the request body, on both targets. The router passes a key it does not
recognise to the upstream.

So `params:` is how a package reaches a provider-specific option Unmute has never
heard of, and nothing checks it: a wrong value comes back as the upstream's own
error. Nested values are fine, because the body is JSON:

```yaml
      params:
        world_part_override: eu
        provider:                     # forwarded to the router, and on to OpenRouter
          only: ["groq"]
```

Two things to tell a user about a host pin like that one, because both bite:

- **It pins one provider and gives up fall-back.** With `only` set, an unavailable
  host is a 404 with the message intact rather than a quiet landing somewhere that
  behaves differently. That is usually what you want on a phone call, and it is a
  trade to name out loud.
- **The context ceiling becomes the pinned host's**, not the figure the model card
  advertises for the model as a whole. Same model, `qwen/qwen3-32b`: 131,072 on
  Groq, 40,960 on Nebius.

**Tell the user to measure the host, and to measure latency and tool-correctness
separately.** Neither predicts the other, and a provider's published figures are
aggregated over short prompts with no tools, so they will not match a voice agent.
On `qwen/qwen3-32b`, measured 2026-08-27 with a real 1.8 KB prompt and four tool
schemas: p50 spanned 564 ms to 1261 ms, p90 spanned 739 ms to 7.3 s, one host
skipped the tool entirely and another spoke a fragment of its own tool-call
template into the reply on every single turn. One sample per host is not enough to
choose: it is how a usable host gets excluded and a broken one gets shipped.

### `prompt_suffix`, when no parameter reaches the model

Some models take instructions that only work as prompt text. `prompt_suffix` on a
**think** entry is literal text the compiler appends to every system prompt that
binding sends: each agent's, each task's on that binding's profile, and the
summarizer's where one is emitted.

```yaml
  think:
    reasoning:
      provider: slng
      model: qwen/qwen3-32b
      agent_id: salon-concierge-v1
      prompt_suffix: "/no_think"
      upstream:
        provider: openai-compat
        url: https://openrouter.ai/api/v1
        key_env: OPENROUTER_API_KEY
```

**Do not reach for a `reasoning` parameter to turn a model's thinking off before
checking that model actually honours one.** On 2026-08-27 three spellings were
sent to three hosts of `qwen/qwen3-32b`, nine requests: `reasoning: {enabled:
false}`, `reasoning: {effort: "none"}`, `reasoning_effort: "none"`, and
`chat_template_kwargs: {enable_thinking: false}`. Every one was accepted, and every
one was ignored: hundreds of reasoning tokens each time. A parameter that is
accepted and ignored looks exactly like one that worked, which is the trap. Qwen3's
own `/no_think` directive in the prompt was the only thing that worked.

Rules for the field:

- **Think entries only.** It appends to a system prompt, so a `speak`, `listen` or
  `turn` entry is refused.
- **Literal text, no placeholders.** A `{{...}}` in it is refused: the router
  substitutes placeholders from a snapshot of the names the *prompts* reference, so
  one from a suffix would be sent with no value and come back 422 mid-call.
- **512 characters.** It is a directive, not a second prompt.
- **One value per package.** A per-target override cannot name a different one,
  because it is appended to instructions files every target shares.
- **It moves no cache scope.** Scopes come from names, never prompt content.
  Retiring a cache is still only ever the version suffix on `agent_id`.
- **The compiler attaches no meaning to it.** `/no_think` is just a string. Write
  whatever directive the user's model documents.

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
- **Write a per-call value the agent speaks as a placeholder, not into the prompt
  text.** A caller's name, the company name, an appointment detail read back: put
  `{{customer_name}}` in the prompt, declare the variable, and let the router
  substitute. The stored answer then holds the placeholder rather than the name,
  so it can be cached at all and it is shared across callers, each hearing their
  own value. Measured 2026-08-24: an answer stored as "Absolutely, Rajesh" came
  back for the next caller as "Absolutely, Sarah", from cache, in 182ms against
  1216ms. Rendered into the prompt text yourself, that same answer is withheld
  from the cache as personal data.
- **A placeholder's value must be in the exact form the agent speaks it.** The
  router puts the placeholder back only where the answer holds the value
  character for character, so a value the model reformats while saying it leaves
  the real thing in the stored copy and the turn silently stops caching. Measured
  2026-08-24, three reads per arm on fresh scopes: a phone number supplied as
  digit groups separated by single spaces was echoed verbatim and the third read
  was served from cache in 109ms, while the same digits supplied with a leading
  country-code plus and no spaces were regrouped by the model, so the value never
  appeared in the answer and none of the three reads was served. Tell an author to write the
  format into the variable's `description` and to have whatever produces the
  value normalise to it. There is no error and no warning; the only symptom is a
  hit rate of zero.
- **This is what makes a number-bearing answer cacheable.** The router otherwise
  refuses to store any answer containing a number, so an agent that reads a phone
  number, a booking reference or a time back aloud pays the model for every one of
  those turns. Supplied as a placeholder, in the form it is spoken, that same
  answer caches like any other. Do not tell an author numbers can never cache.
- **A value that steers the answer is not a placeholder.** The reply language is
  the clearest case: two callers who chose different languages would share one
  cached answer and one would hear the wrong one. Write a steering value into the
  prompt text, and if two variants must never receive each other's answers give
  each its own `agent_id`. The compiler does not check this and cannot: it would
  be judging a value from its name.
- The values are read again for every request, so a value the call learns partway
  through is in the prompt from the next turn. A referenced name with no value yet
  is still sent, as an empty string; an unsupplied name is a 422 mid-call.
- Streamed responses carry no usage on either path, so token savings cannot be
  read off the stream.
- Three response headers say which path answered:
  `x-slng-response-source`, `x-slng-cache-layer`, `x-slng-model`.
- **An emitted agent reports where each answer came from.** Every think request
  logs one line at info level, in every run, not only locally:
  `slng router: scope=<id>:<site> source=cache layer=l2_exact request_id=...`, or
  `source=llm model=<model>` on a live answer. Tell an author to read that line
  rather than to guess from latency: a hit has been measured as slow as 396ms and
  a generation as fast as a hit. Nothing aggregates the lines, so a hit rate is a
  count of them.

Full page: [Context Router](https://docs.slng.ai/context-router/). No shipped
example binds to it today: both salon packages reach OpenAI directly, so an
author who wants to see what the router is worth compiles one of them twice,
once as it ships and once with the think binding pointed at the router.

## Keep models close to callers

Choose locations for STT, TTS and LLM services separately from the worker's
deployment region. A nearby worker still waits on a distant model. Direct
providers have their own regional endpoints and settings; use `endpoint_env`
where the integration has an endpoint slot, or a supported provider parameter
under `params`. Check the selected target's plugin before writing either.
There is no shared region vocabulary across providers. Measure the full turn
from the caller's location, including fallback models and tools.

### Choose an SLNG speech gateway

Put `world_part` in the SLNG model's `params:`. The same YAML works for
listen and speak models on LiveKit and Pipecat:

```yaml agent.yaml
models:
  listen:
    transcriber:
      provider: slng
      model: "slng/deepgram/nova:3-multi"
      params:
        world_part: eu-north
  speak:
    voice:
      provider: slng
      model: "deepgram/aura:2"
      voice: "aura-2-thalia-en"
      params:
        world_part: eu-north
```

The accepted values are `us-east`, `us-west`, `br`, `eu-west`, `eu-north`, `gb`,
`za`, `il`, `jp`, `sg`, `id`, `in`, and `au`. Unmute consumes the key and emits
the host `{world_part}.api.slng.ai`: `slng_base_url="eu-north.api.slng.ai"` on
LiveKit and `base_url="eu-north.api.slng.ai"` on Pipecat. It sends no scheme or
path and does not forward `world_part` to the plugin.

Omitting `world_part` keeps the existing default URL. An empty,
non-string, or unknown value is refused. The old speech values `na`, `eu`, and
`ap` are refused too: choose one of the new values explicitly. There is no
automatic mapping from the old broad areas. `params.base_url` and
`params.slng_base_url` cannot be combined with `params.world_part`;
remove the explicit URL or the world part.

A gateway choice alone is not a data residency guarantee.
The generated dependencies already support these host parameters; see the
[SLNG LiveKit plugin guide](https://docs.slng.ai/agents/livekit-plugin) and the
[SLNG Pipecat plugin guide](https://docs.slng.ai/agents/pipecat-plugin).

This only changes generated SLNG speech services on LiveKit and Pipecat. The
SLNG hosted target and the Context Router's think values stay unchanged. Set
the agent worker's `deployment_region` separately in `targets.yaml`;
`package.md` has the deployment rules.

### LiveKit Responses API

Author it once, on the shared think binding in `agent.yaml`:

```yaml agent.yaml
models:
  think:
    reasoning:
      provider: openai
      model: gpt-5.6-terra
      params:
        api: responses
        reasoning_effort: none
        use_websocket: true
```

On LiveKit this emits `openai.responses.LLM` and maps `reasoning_effort` to the
nested reasoning setting when it is present. Use that field instead of a raw
`reasoning` map.

`api` and `use_websocket` are the two params only LiveKit can act on: one picks
the class, the other is a kwarg that class has and the chat completions one does
not. **Do not put them in a target override.** A per-target `models:` entry
replaces the base entry rather than merging into it, so an override has to
repeat `provider`, `model` and `reasoning_effort` verbatim to keep them, and a
duplicated binding is one somebody edits on one side only. Pipecat drops both
and builds `OpenAILLMService` either way; `unmute validate` warns per param,
naming the target, so the drop is reported rather than silent.
`salon-concierge-single-prompt` shows this binding; `salon-concierge` uses Chat
Completions with `reasoning_effort: "none"`.

`use_websocket: true` keeps a WebSocket connection for Responses requests.
HTTP clients can also reuse connections. Compare several calls with the same
prompts and tools before choosing an API or transport for latency.

## The vendors, per target per role

SLNG leads every list it appears in. These are the built-in `provider:` values
the catalogue holds.

| Target | Role | Vendors |
|---|---|---|
| pipecat | listen | `slng`, `assemblyai`, `cartesia`, `deepgram`, `elevenlabs`, `gradium`, `openai`, `soniox`, `speechmatics` |
| pipecat | speak | `slng`, `cartesia`, `deepgram`, `elevenlabs`, `gradium`, `inworld`, `openai`, `rime`, `sarvam`, `soniox` |
| pipecat | think | `slng`, `anthropic`, `deepseek`, `google`, `groq`, `mistral`, `openai`, `openrouter`, `qwen` |
| pipecat | realtime | `openai` |
| livekit | listen | `slng`, `assemblyai`, `cartesia`, `deepgram`, `elevenlabs`, `gradium`, `sarvam`, `soniox`, `speechmatics` |
| livekit | speak | `slng`, `cartesia`, `deepgram`, `elevenlabs`, `gemini`, `gradium`, `inworld`, `rime`, `sarvam`, `soniox` |
| livekit | think | `slng`, `anthropic`, `aws`, `azure`, `groq`, `mistralai`, `openai`, `openrouter`, `sarvam` |

Some vendor facts on Pipecat change what an author writes. Each setting below is
one `params:` line, which reaches the service's settings by name:

- `deepgram` listen: since Pipecat 1.9.0 the profanity filter is off unless
  asked for, because it rewrites the words it matches and a false positive
  silently changes a transcript. `params: {profanity_filter: true}` turns it on.
  `params: {version: "2021-03-17.0"}` pins a model version. A Flux model also
  takes `params: {redact: ...}` to mask numbers.
- `openai` listen: OpenAI retires its previous transcription model on
  2027-02-26; `gpt-transcribe` is the current one. The transcriber pads each
  speech segment with half a second of silence so the last word is not cut,
  and the padding counts toward usage.
- `elevenlabs` listen takes `params: {no_verbatim: true}` to drop filler words;
  `speechmatics` listen takes `params: {include_results: true}` for word-level
  results.
- `cartesia` listen under a listening decider takes `turn_start_threshold`,
  `turn_eager_end_threshold` and `turn_end_threshold`, which say how sure Turns
  has to be. Leave them out and Cartesia's own defaults apply.
- `assemblyai` listen: `universal-3-5-pro` is the default and
  `universal-3-6-pro` is the same model upgraded, with the same features.
- `deepgram` speak takes `params: {speed: 1.1}`, Aura's speech rate, 0.7 to 1.5;
  `soniox` speak takes `params: {reduce_silence: true}` to shorten the pauses
  between words.
- Two speaking defaults moved in 1.9.0: `cartesia` now speaks with `sonic-3.6`
  and `sarvam` with `bulbul:v3`. Sarvam's API no longer serves `bulbul:v2`, so a
  package naming it cannot speak at all.

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
`build/<target>/compile-report.json` records the exact binding that will be
forwarded, unvalidated, at runtime: provider, model, and every param, under the
`bindings` key.

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
      model: "deepgram/aura:2"
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

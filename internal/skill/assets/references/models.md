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
      model: gpt-5.6-luna
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

## The think model needs `reasoning_effort`

Write it on the think entry of every package you create, and keep it on one you
edit:

```yaml
      params:
        reasoning_effort: "none"
```

`gpt-5.6-luna` is a reasoning model, and OpenAI rejects a chat completions
request that carries function tools unless the request also sets
`reasoning_effort`. Leaving it out is not the same as leaving it alone: the
server applies its own default and every turn comes back as HTTP 400. Nearly
every agent has tools, so treat the line as part of the block rather than as
tuning somebody asked for.

This model takes `none`, `low`, `medium`, `high`, and `xhigh`. It rejects
`minimal`. Use `none` unless the user asks for more thinking.

You write the same line for both targets. On LiveKit it becomes a constructor
argument to `openai.LLM`. On Pipecat the settings class has no field for it, so
the compiler puts it in the service's `extra` field, which Pipecat merges into
the request body as written. That is the general rule for `params:`: a name the
target's settings object has no field for rides the target's overflow field
instead of being dropped.

On LiveKit there is a second reason to be explicit. `livekit-plugins-openai`
1.6.10 injects `reasoning_effort="minimal"` by itself for several older ids in
the same GPT-5 family, and that is the same 400 once the agent has tools.

## The vendors, per target per role

SLNG leads every list it appears in. These are the built-in `provider:` values
the catalogue holds.

| Target | Role | Vendors |
|---|---|---|
| pipecat | listen | `slng`, `assemblyai`, `cartesia`, `deepgram`, `elevenlabs`, `gradium`, `openai`, `soniox`, `speechmatics` |
| pipecat | speak | `slng`, `cartesia`, `deepgram`, `elevenlabs`, `gradium`, `inworld`, `openai`, `rime`, `sarvam`, `soniox` |
| pipecat | think | `anthropic`, `deepseek`, `google`, `groq`, `mistral`, `openai`, `openrouter`, `qwen` |
| livekit | listen | `slng`, `assemblyai`, `cartesia`, `deepgram`, `elevenlabs`, `gradium`, `sarvam`, `soniox`, `speechmatics` |
| livekit | speak | `slng`, `cartesia`, `deepgram`, `elevenlabs`, `gemini`, `gradium`, `inworld`, `rime`, `sarvam`, `soniox` |
| livekit | think | `anthropic`, `aws`, `azure`, `groq`, `mistralai`, `openai`, `openrouter`, `sarvam` |

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
      model: gpt-5.6-luna
```

Unmute keeps no allowlist of model ids, with exactly one exception: a LiveKit
`turn` model must be `turn-detector-mini` or `turn-detector`, because those are
loaded by name rather than sent to a provider. Everywhere else `model:`,
`voice:`, and `params:` go to the provider exactly as written. A typo becomes a
provider error on the first call, not a compile error, and the compile report
says so:

```
pipecat: binding reason.reasoning provider=openai model=gpt-5.6-luna (forwarded as-is, not validated)
```

So when you pick a model id, pick one the user named or one from that vendor's
own documentation. Do not invent an id that looks plausible.

Do not guess model ids, voice ids, or params.

Two families appear in this repository's own documentation, and only two: the
SLNG listen and speak ids, and `gpt-5.6-luna` for think. They are the ones
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

Every provider needs a credential, and it is an environment variable name in
`secrets:`, never a value:

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

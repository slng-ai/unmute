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
below.** Entry names are yours to choose, and `unmute init` chooses different
ones from this file: it writes `assistant_model`, `assistant_voice`,
`transcriber`, and `vad`. Copying this block onto a scaffolded package without
changing `detector` to `vad` fails, cleanly but needlessly:

```
targets.yaml:11: target "livekit" overrides "detector", which is not a defined model
```

Read the `models:` block you actually have before you write the override.

```yaml targets.yaml
targets:
  livekit:
    provider: livekit
    version: "1.5.2"
    sdk_language: python
    models:
      detector:
        provider: livekit
        model: turn-detector-mini
```

If the user names their own vendor, use it. Check the table below first, and
say what you bound.

## The vendors, per target per role

SLNG leads every list it appears in. These are the `provider:` values the
catalogue holds, and nothing else is legal.

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

**Turn detection has no vendor list.** Neither target has catalogue entries for
the `turn` role, because turn detection is a mechanism each target ships rather
than a vendor you bind. Pipecat runs Silero locally; LiveKit runs its own turn
detector. Use `provider: local` or `provider: livekit`. What each one does, and
what to listen for when it is wrong, is in `conversation.md`.

If a user asks for a vendor that is not in the row above, say plainly that it is
not available for that role on that target, and name what is. Do not invent a
provider value, do not guess at a spelling, and do not bind it anyway and hope.
A provider the catalogue does not hold is refused at validation with the target
named, so inventing one only moves the failure later.

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

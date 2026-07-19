# Reference: providers

This page lists the STT, TTS, and LLM providers each target accepts, what to
write in [`agent.yaml`](agent-yaml.md), and what the compiler emits. The tables
come from the provider catalogue (`internal/target/catalog_*.go`), which both
validation and generation use.

Two rules frame everything:

1. **The provider picks the code slot; the model stays yours.** `provider:` decides which integration is emitted (import, dependency, constructor, key env). `model:`, `voice:`, and `params:` are forwarded to that integration verbatim and never validated (SCHEMA.md D10). A typo in a provider name fails at `validate`; a typo in a model name fails at the provider, with its error relayed.
2. **Unknown providers fail closed, with the matrix quoted.** On Pipecat, an
   unknown provider is allowed only as an OpenAI-compatible custom endpoint
   with `endpoint_env`.

Changing a model's `provider` is a one-line edit. For example, changing a
LiveKit speak model from SLNG to ElevenLabs swaps one constructor, one import,
one dependency, and one environment variable in the emitted project.

## Pipecat

All four model kinds are open. Official Pipecat services install as extras on
the `pipecat-ai` pin and take model, voice, and params nested in
`Class.Settings(...)`. The SLNG plugin is a separate package with flat keyword
arguments.

| Kind | `provider:` | Model fields | Key env | Installed as |
|---|---|---|---|---|
| listen | `assemblyai` | `model` | `ASSEMBLYAI_API_KEY` | `pipecat-ai[assemblyai]` |
| listen | `cartesia` | `model` | `CARTESIA_API_KEY` | `pipecat-ai[cartesia]` |
| listen | `deepgram` | `model` | `DEEPGRAM_API_KEY` | `pipecat-ai[deepgram]` |
| listen | `elevenlabs` (also `eleven_labs`) | `model` | `ELEVENLABS_API_KEY` | `pipecat-ai[elevenlabs]` |
| listen | `gradium` | `model` | `GRADIUM_API_KEY` | `pipecat-ai[gradium]` |
| listen | `openai` | `model`, optional `endpoint_env` | `OPENAI_API_KEY` | `pipecat-ai[openai]` |
| listen | `slng` | `model` (the `slng/<vendor>/<model>` route) | `SLNG_API_KEY` | `pipecat-slng>=0.4.0` |
| listen | `soniox` | `model` | `SONIOX_API_KEY` | `pipecat-ai[soniox]` |
| listen | `speechmatics` | `model` | `SPEECHMATICS_API_KEY` | `pipecat-ai[speechmatics]` |
| speak | `cartesia` | `voice`, optional `model` | `CARTESIA_API_KEY` | `pipecat-ai[cartesia]` |
| speak | `deepgram` | `voice` (the aura voice id), optional `model` | `DEEPGRAM_API_KEY` | `pipecat-ai[deepgram]` |
| speak | `elevenlabs` (also `eleven_labs`) | `voice`, optional `model` | `ELEVENLABS_API_KEY` | `pipecat-ai[elevenlabs]` |
| speak | `gradium` | `voice`, optional `model` | `GRADIUM_API_KEY` | `pipecat-ai[gradium]` |
| speak | `inworld` | `voice`, optional `model` | `INWORLD_API_KEY` | `pipecat-ai[inworld]` |
| speak | `openai` | `voice`, `model`, optional `endpoint_env` | `OPENAI_API_KEY` | `pipecat-ai[openai]` |
| speak | `rime` | `voice`, optional `model` | `RIME_API_KEY` | `pipecat-ai[rime]` |
| speak | `sarvam` | `voice`, optional `model` | `SARVAM_API_KEY` | `pipecat-ai[sarvam]` |
| speak | `slng` | `model` (route), `voice` | `SLNG_API_KEY` | `pipecat-slng>=0.4.0` |
| speak | `soniox` | `voice`, optional `model` | `SONIOX_API_KEY` | `pipecat-ai[soniox]` |
| think | `anthropic` | `model` | `ANTHROPIC_API_KEY` | `pipecat-ai[anthropic]` |
| think | `deepseek` | `model`, optional `endpoint_env` | `DEEPSEEK_API_KEY` | `pipecat-ai[deepseek]` |
| think | `google` | `model` (Gemini via GenAI) | `GOOGLE_API_KEY` | `pipecat-ai[google]` |
| think | `groq` | `model`, optional `endpoint_env` | `GROQ_API_KEY` | `pipecat-ai[groq]` |
| think | `mistral` | `model`, optional `endpoint_env` | `MISTRAL_API_KEY` | `pipecat-ai[mistral]` |
| think | `openai` (default when omitted) | `model`, optional `endpoint_env` | `OPENAI_API_KEY` | `pipecat-ai[openai]` |
| think | `openrouter` | `model`, optional `endpoint_env` | `OPENROUTER_API_KEY` | `pipecat-ai[openrouter]` |
| think | `qwen` | `model`, optional `endpoint_env` | `QWEN_API_KEY` | `pipecat-ai[qwen]` |
| any of the three | *any other name* + `endpoint_env` | OpenAI-compatible custom endpoint | `<NAME>_API_KEY` | `pipecat-ai[openai]` |

This example defines listen and speak models and shows the emitted services:

```yaml
models:
  listen:
    transcriber: { provider: deepgram, model: nova-3 }
  speak:
    front_desk: { provider: slng, model: "slng/deepgram/aura:2-en", voice: "aura-2-thalia-en" }
```

```python
from pipecat.services.deepgram.stt import DeepgramSTTService
from pipecat_slng import SlngTTSService

DeepgramSTTService(
    api_key=os.environ["DEEPGRAM_API_KEY"],
    settings=DeepgramSTTService.Settings(model="nova-3"),
)
SlngTTSService(
    api_key=os.environ["SLNG_API_KEY"],
    voice="aura-2-thalia-en",
    model="slng/deepgram/aura:2-en",
)
```

**Custom OpenAI-compatible endpoints.** Any provider name outside the table is
legal only with `endpoint_env`, and lowers to the OpenAI service with a
`base_url` override. The key environment variable follows the name:
`provider: fireworks, endpoint_env: FIREWORKS_URL` reads
`FIREWORKS_API_KEY`. This is also the path for SLNG as a think model. Without
`endpoint_env`, the unknown name is an error.

**SLNG notes.** The model keeps its full `slng/<vendor>/<model>` route on
Pipecat. `endpoint_env` has no SLNG slot because routing uses the API key and
region parameters, so validation rejects it.

## LiveKit

Listen and speak models resolve to plugins. Think models resolve to native
vendor plugins where available and to LiveKit Inference otherwise. SLNG is the
`unmute init` default, not a constraint.

| Kind | `provider:` | Model fields | Key env | Installed as |
|---|---|---|---|---|
| listen | `assemblyai` | `model` | `ASSEMBLYAI_API_KEY` | `livekit-agents[assemblyai]` |
| listen | `cartesia` | `model` | `CARTESIA_API_KEY` | `livekit-agents[cartesia]` |
| listen | `deepgram` | `model` | `DEEPGRAM_API_KEY` | `livekit-agents[deepgram]` |
| listen | `elevenlabs` (also `eleven_labs`) | `model` (emitted as `model_id`) | `ELEVEN_API_KEY` | `livekit-agents[elevenlabs]` |
| listen | `gradium` | `model` (emitted as `model_name`) | `GRADIUM_API_KEY` | `livekit-agents[gradium]` |
| listen | `sarvam` | `model` | `SARVAM_API_KEY` | `livekit-agents[sarvam]` |
| listen | `slng` | `model` (route, passed verbatim) | `SLNG_API_KEY` | `livekit-plugins-slng>=1.6.1` |
| listen | `soniox` | `model` (nested in `params=soniox.STTOptions(...)`; language auto-identified) | `SONIOX_API_KEY` | `livekit-agents[soniox]` |
| listen | `speechmatics` | no `model` slot (accuracy via `operating_point` param) | `SPEECHMATICS_API_KEY` | `livekit-agents[speechmatics]` |
| speak | `cartesia` | `voice`, optional `model` | `CARTESIA_API_KEY` | `livekit-agents[cartesia]` |
| speak | `deepgram` | `model` only (the aura id carries voice + language) | `DEEPGRAM_API_KEY` | `livekit-agents[deepgram]` |
| speak | `elevenlabs` (also `eleven_labs`) | `voice` (emitted as `voice_id`), optional `model` | `ELEVEN_API_KEY` | `livekit-agents[elevenlabs]` |
| speak | `gemini` | `voice` (emitted as `voice_name`), optional `model` | `GOOGLE_API_KEY` | `livekit-agents[google]` |
| speak | `gradium` | `voice` (emitted as `voice_id`), optional `model` (`model_name`) | `GRADIUM_API_KEY` | `livekit-agents[gradium]` |
| speak | `inworld` | `voice`, optional `model` | `INWORLD_API_KEY` | `livekit-agents[inworld]` |
| speak | `rime` | `voice` (emitted as `speaker`), optional `model` | `RIME_API_KEY` | `livekit-agents[rime]` |
| speak | `sarvam` | `voice` (emitted as `speaker`), optional `model` | `SARVAM_API_KEY` | `livekit-agents[sarvam]` |
| speak | `slng` | `model` (route), `voice` | `SLNG_API_KEY` | `livekit-plugins-slng>=1.6.1` |
| speak | `soniox` | `voice`, optional `model` | `SONIOX_API_KEY` | `livekit-agents[soniox]` |
| think | `anthropic` | `model`, optional `endpoint_env` | `ANTHROPIC_API_KEY` | `livekit-agents[anthropic]` |
| think | `aws` | `model` (Bedrock; region via params or `AWS_REGION`) | `AWS_ACCESS_KEY_ID` + `AWS_SECRET_ACCESS_KEY` | `livekit-agents[aws]` |
| think | `azure` | `model`, `endpoint_env` (emitted as `azure_endpoint`) | `AZURE_OPENAI_API_KEY` | `livekit-agents[openai]` |
| think | `groq` | `model`, optional `endpoint_env` | `GROQ_API_KEY` | `livekit-agents[groq]` |
| think | `mistralai` (also `mistral`) | `model` | `MISTRAL_API_KEY` | `livekit-agents[mistralai]` |
| think | `openai` | `model`, optional `endpoint_env` | `OPENAI_API_KEY` | `livekit-agents[openai]` |
| think | `openrouter` | `model`, optional `endpoint_env` | `OPENROUTER_API_KEY` | `livekit-agents[openai]` |
| think | `sarvam` | `model`, optional `endpoint_env` | `SARVAM_API_KEY` | `livekit-agents[sarvam]` |
| think | any other (LiveKit Inference) | `model`; emitted as `"<provider>/<model>"` | none (LiveKit Cloud credentials) | ships with `livekit-agents` |

This example defines LiveKit models backed by vendor plugins:

```yaml
models:
  listen:
    transcriber: { provider: deepgram, model: nova-3 }
  speak:
    front_desk: { provider: elevenlabs, voice: cgSgspJ2msm6clMCkdW9 }
```

Watch the env names: the LiveKit ElevenLabs plugin reads `ELEVEN_API_KEY`, not `ELEVENLABS_API_KEY`. The emitted `.env.example` and compile report always list exactly what the project reads.

A catalogued think provider uses its native plugin and key. For example,
`{ provider: openai, model: gpt-4o-mini }` emits `openai.LLM(...)` and reads
`OPENAI_API_KEY`, so a local console run needs no LiveKit Cloud credentials.
Uncatalogued think providers use LiveKit Inference. The explicit
`{ provider: livekit, model: "openai/gpt-4o-mini" }` form also selects
Inference. LiveKit listen and speak models have no custom-endpoint wildcard.

## Managed targets

No code is injected; the provider name is forwarded into the platform's own config, and the matrix only guards what each platform can actually run:

- **ElevenLabs**: Speak accepts ElevenLabs voices only, usually with just a
  `voice` field. Listen and turn are integrated, so their target overrides can
  contain settings only. Think accepts the platform's model list or a custom
  LLM endpoint.
- **Deepgram**: Listen accepts Deepgram only. Speak accepts `deepgram`,
  `elevenlabs`, `cartesia`, `openai`, and `aws_polly`; the aliases
  `eleven_labs` and `open_ai` also work. Think is open and accepts custom
  endpoints.
- **Vapi**: unrestricted at this layer; Vapi's API is the validator.

## Errors you will see

Provider errors name the selected target and model kind, then list the accepted
providers or the unsupported field:

```
livekit listen binding provider "acme" has no slot; listen providers on livekit: assemblyai, cartesia, deepgram, elevenlabs, gradium, sarvam, slng, soniox, speechmatics

pipecat speak binding provider "slng": endpoint_env has no slot here; drop it or use an OpenAI-compatible provider
```

These fire at `unmute validate`, and the same rulebook backs generation, so a spec that validates green never fails provider selection at `compile`.

## Adding a provider

If a framework already integrates a provider that is missing here, adding it is one catalogue entry plus tests; see the recipe in [PROVIDER_CATALOG.md](../../../PROVIDER_CATALOG.md). Every entry carries a verification date and a docs URL, and the resolution golden (`internal/generate/testdata/golden/catalog_resolution.txt`) shows the exact emitted call for every entry.

# Reference: providers

Which STT, TTS, and LLM providers each target accepts, what to write in [targets.yaml](targets-yaml.md), and what the compiler emits for each choice. The tables below are generated knowledge from the provider catalogue (`internal/target/catalog_*.go`), the single source both validation and code generation read, so what validates green here is exactly what compiles.

Two rules frame everything:

1. **The provider picks the code slot; the model stays yours.** `provider:` decides which integration is emitted (import, dependency, constructor, key env). `model:`, `voice:`, and `params:` are forwarded to that integration verbatim and never validated (SCHEMA.md D10). A typo in a provider name fails at `validate`; a typo in a model name fails at the provider, with its error relayed.
2. **Unknown providers fail closed, with the matrix quoted.** For example: `livekit listen binding provider "acme" has no slot; listen providers on livekit: deepgram, slng`. On Pipecat, an unknown provider is allowed in exactly one case: as a genuinely OpenAI-compatible custom endpoint (see below).

Swapping providers is a one-line change per binding. Rebinding LiveKit speak from SLNG to ElevenLabs swaps one constructor, one import, one dependency, and one env var in the emitted project; nothing else moves.

## Pipecat

All four roles are open. Official Pipecat services install as extras on the `pipecat-ai` pin and take model/voice/params nested in `Class.Settings(...)` (the current Pipecat API; flat forms are deprecated upstream). The SLNG plugin is its own package with flat kwargs.

| Role | `provider:` | Binding fields | Key env | Installed as |
|---|---|---|---|---|
| listen | `assemblyai` | `model` | `ASSEMBLYAI_API_KEY` | `pipecat-ai[assemblyai]` |
| listen | `deepgram` | `model` | `DEEPGRAM_API_KEY` | `pipecat-ai[deepgram]` |
| listen | `openai` | `model`, optional `endpoint_env` | `OPENAI_API_KEY` | `pipecat-ai[openai]` |
| listen | `slng` | `model` (the `slng/<vendor>/<model>` route) | `SLNG_API_KEY` | `pipecat-slng>=0.4.0` |
| speak | `cartesia` | `voice`, optional `model` | `CARTESIA_API_KEY` | `pipecat-ai[cartesia]` |
| speak | `elevenlabs` (also `eleven_labs`) | `voice`, optional `model` | `ELEVENLABS_API_KEY` | `pipecat-ai[elevenlabs]` |
| speak | `openai` | `voice`, `model`, optional `endpoint_env` | `OPENAI_API_KEY` | `pipecat-ai[openai]` |
| speak | `slng` | `model` (route), `voice` | `SLNG_API_KEY` | `pipecat-slng>=0.4.0` |
| reason | `openai` (default when omitted) | `model`, optional `endpoint_env` | `OPENAI_API_KEY` | `pipecat-ai[openai]` |
| any of the three | *any other name* + `endpoint_env` | OpenAI-compatible custom endpoint | `<NAME>_API_KEY` | `pipecat-ai[openai]` |

Example bindings and what they emit:

```yaml
models:
  listen: { provider: deepgram, model: nova-3 }
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

**Custom OpenAI-compatible endpoints.** Any provider name outside the table is legal only with `endpoint_env`, and lowers to the OpenAI service with a `base_url` override. The key env follows the name: `provider: fireworks, endpoint_env: FIREWORKS_URL` reads `FIREWORKS_API_KEY`. This is also the documented path for SLNG as a `reason` model. Without `endpoint_env`, the unknown name is an error, never a silent substitution.

**SLNG notes.** The model keeps its full `slng/<vendor>/<model>` route on Pipecat. `endpoint_env` has no slot on SLNG bindings (routing is by api key and region params) and is rejected rather than ignored.

## LiveKit

`listen` and `speak` resolve to plugins; `reason` goes through LiveKit Inference. SLNG is what `unmute init` scaffolds, not a constraint.

| Role | `provider:` | Binding fields | Key env | Installed as |
|---|---|---|---|---|
| listen | `deepgram` | `model` | `DEEPGRAM_API_KEY` | `livekit-agents[deepgram]` |
| listen | `slng` | `model` (route; the `slng/` prefix is stripped for the plugin) | `SLNG_API_KEY` | `livekit-plugins-slng>=1.6.1` |
| speak | `cartesia` | `voice`, optional `model` | `CARTESIA_API_KEY` | `livekit-agents[cartesia]` |
| speak | `elevenlabs` (also `eleven_labs`) | `voice` (emitted as `voice_id`), optional `model` | `ELEVEN_API_KEY` | `livekit-agents[elevenlabs]` |
| speak | `slng` | `model` (route), `voice` | `SLNG_API_KEY` | `livekit-plugins-slng>=1.6.1` |
| reason | any (LiveKit Inference) | `model`; emitted as `"<provider>/<model>"` | none (LiveKit Cloud credentials) | ships with `livekit-agents` |

Example: the same two roles bound to per-vendor plugins, then to SLNG:

```yaml
listen: { provider: deepgram, model: nova-3 }         # deepgram.STT(model="nova-3")
speak:
  front_desk: { provider: elevenlabs, voice: cgSgspJ2msm6clMCkdW9 }
                                                      # elevenlabs.TTS(voice_id="cgSg...")
# or the SLNG execution layer:
listen: { provider: slng, model: "slng/deepgram/nova:3" }   # slng.STT(model="deepgram/nova:3")
```

Watch the env names: the LiveKit ElevenLabs plugin reads `ELEVEN_API_KEY`, not `ELEVENLABS_API_KEY`. The emitted `.env.example` and compile report always list exactly what the project reads.

Reason bindings need no provider key: `{ provider: openai, model: gpt-4o-mini }` emits `inference.LLM(model="openai/gpt-4o-mini")`, billed through LiveKit Cloud; `params` ride `extra_kwargs`. There is no custom-endpoint wildcard on LiveKit listen/speak: unknown vendors fail.

## Managed targets

No code is injected; the provider name is forwarded into the platform's own config, and the matrix only guards what each platform can actually run:

- **ElevenLabs**: `speak` accepts ElevenLabs voices only (usually written with no `provider:` at all, just `voice_id:`). `listen` and `turn` are integrated (settings-only bindings). `reason` takes the platform's supported model list or a custom LLM endpoint; names are forwarded, not validated.
- **Deepgram**: `listen` accepts Deepgram only. `speak` accepts `deepgram`, `elevenlabs`, `cartesia`, `openai`, `aws_polly` (Deepgram's own spellings `eleven_labs` and `open_ai` are accepted too). `reason` is open, custom endpoints allowed.
- **Vapi**: unrestricted at this layer; Vapi's API is the validator.

## Errors you will see

```
pipecat reason binding provider "slng" has no slot; reason providers on pipecat: openai;
  an unlisted provider needs endpoint_env (an OpenAI-compatible endpoint) ("slng" provides listen, speak on pipecat)

livekit listen binding provider "acme" has no slot; listen providers on livekit: deepgram, slng

pipecat speak binding provider "slng": endpoint_env has no slot here; drop it or use an OpenAI-compatible provider
```

These fire at `unmute validate`, and the same rulebook backs generation, so a spec that validates green never fails provider selection at `compile`.

## Adding a provider

If a framework already integrates a provider that is missing here, adding it is one catalogue entry plus tests; see the recipe in [PROVIDER_CATALOG.md](../../../PROVIDER_CATALOG.md). Every entry carries a verification date and a docs URL, and the resolution golden (`internal/generate/testdata/golden/catalog_resolution.txt`) shows the exact emitted call for every entry.

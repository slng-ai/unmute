# nemotron-voice-agent

NVIDIA's [Nemotron Voice Agent blueprint](https://github.com/NVIDIA-AI-Blueprints/nemotron-voice-agent),
expressed as an Unmute definition.

One package. Two frameworks. Nothing rewritten between them.

```sh
unmute validate
unmute compile --target pipecat     # writes a Pipecat project
unmute compile --target livekit     # writes a LiveKit Agents project
unmute dev                          # talk to it locally
```

## Run it

```sh
NVIDIA_API_KEY=nvapi-...
NVIDIA_NIM_BASE_URL=https://integrate.api.nvidia.com/v1
SLNG_API_KEY=...
```

## Point it at your own models

```sh
NVIDIA_NIM_BASE_URL=https://integrate.api.nvidia.com/v1     # NVIDIA-hosted
NVIDIA_NIM_BASE_URL=http://your-workstation:8000/v1         # your own NIM
NVIDIA_NIM_BASE_URL=http://jetson.local:8000/v1             # edge
```

Same package every time. `endpoint_env` resolves at run time, so moving between
NVCF, a DGX box and a Jetson is an environment variable rather than a fork.

**On the Pipecat target.** LiveKit resolves an unlisted think provider through
LiveKit Inference, which has no slot for a custom endpoint, so the livekit target
redeclares the entry without it and runs Nemotron NVIDIA-hosted.

That difference is worth knowing in its own right: **today you can self-host
Nemotron behind a Pipecat agent and you cannot behind a LiveKit one.**

## The two axes

The blueprint's deployment profiles combine two decisions that are actually
independent:

|                          |                                                                 |
| ------------------------ | --------------------------------------------------------------- |
| **Where the models run** | `endpoint_env` — NVCF, your workstation, DGX Spark, Jetson Thor |
| **Where the agent runs** | `--target` — Pipecat Cloud, LiveKit Cloud, your own cloud       |

Nemotron on your own Jetson with a Pipecat agent on any cloud is a legal
combination, and so is NVIDIA-hosted Nemotron with the agent anywhere else. A
matrix rather than one choice.

## Using NVIDIA speech endpoints

This example uses SLNG for `listen` and `speak` — that's what `unmute init` writes
by default. To use NVIDIA speech endpoints instead, edit `agent.yaml`:

**For ASR (Parakeet):** NVIDIA's ASR NIM serves `/v1/audio/transcriptions`, the
same OpenAI-compatible route that STT providers use. Add `endpoint_env` to point
at your NIM:

```yaml
listen:
  transcriber:
    provider: nvidia
    model: "parakeet-ctc-1.1b"
    endpoint_env: NVIDIA_NIM_BASE_URL
```

**For TTS (Chatterbox):** NVIDIA's TTS NIM serves `/v1/audio/synthesize` instead
of OpenAI's `/v1/audio/speech`. Support for this endpoint is in progress.

**For LLM (Nemotron):** Already configured with `endpoint_env: NVIDIA_NIM_BASE_URL`
in the example. NIM LLM endpoints are OpenAI-compatible, so they work on both
Pipecat and LiveKit targets.

## What targets.yaml is doing

The whole difference between the two frameworks is one `models:` override block:

| Entry      | Why it is overridden on LiveKit                                                                                                    |
| ---------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| `nemotron` | no custom-endpoint slot, so the entry is redeclared without `endpoint_env`                                                         |
| `detector` | `silero` is a voice activity detector, not a turn detector. Pipecat forwards the identity unchecked; LiveKit checks it and refuses |

Everything else — the prompt, the greeting, the models, the tool, the channel —
is written once.

## Files

```
agent.yaml            the agent: models, prompt, greeting, channel, tool
targets.yaml          the two frameworks, and the overrides LiveKit needs
instructions.md       the prompt
tools/end_call.yaml   lets the agent hang up
```

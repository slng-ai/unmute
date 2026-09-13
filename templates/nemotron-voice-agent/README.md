# nemotron-voice-agent

NVIDIA's [Nemotron Voice Agent blueprint](https://github.com/NVIDIA-AI-Blueprints/nemotron-voice-agent),
expressed as an Unmute definition.

**Write once. Compile to Pipecat or LiveKit. Deploy anywhere.**

```sh
unmute validate
unmute compile --target pipecat     # writes a Pipecat project
unmute compile --target livekit     # writes a LiveKit Agents project
unmute dev                          # talk to it locally
```

One `agent.yaml` file defines your voice agent. Point it at any NVIDIA NIM endpoint —
build.nvidia.com, your workstation, a DGX box, or a Jetson — by changing environment
variables, not code.

## Run it

```sh
NVIDIA_API_KEY=nvapi-...
NVIDIA_NIM_BASE_URL=https://integrate.api.nvidia.com/v1
```

## Point it at your endpoints

```sh
NVIDIA_NIM_BASE_URL=https://integrate.api.nvidia.com/v1     # NVIDIA-hosted
NVIDIA_NIM_BASE_URL=http://your-workstation:8000/v1         # your own NIM
NVIDIA_NIM_BASE_URL=http://jetson.local:8000/v1             # edge device
```

Same `agent.yaml` every time. Change where your models run without changing your code.

## Customize for your NVIDIA endpoints

This example currently uses placeholder speech providers. To use your NVIDIA NIM speech
endpoints, edit the `listen` and `speak` sections in `agent.yaml`:

**ASR (Parakeet)** — NVIDIA's ASR NIM serves `/v1/audio/transcriptions`:

```yaml
listen:
  transcriber:
    provider: nvidia
    model: "parakeet-ctc-1.1b"
    endpoint_env: NVIDIA_NIM_BASE_URL
```

**TTS (Chatterbox)** — NVIDIA's TTS NIM serves `/v1/audio/synthesize`:

```yaml
speak:
  voice:
    provider: nvidia
    model: "chatterbox-tts"
    endpoint_env: NVIDIA_NIM_BASE_URL
```

**LLM (Nemotron)** — Already configured:

```yaml
think:
  nemotron:
    provider: nvidia
    model: "nemotron-3.5-lightning-30b-a3b"
    endpoint_env: NVIDIA_NIM_BASE_URL  # ← points at your endpoint
```

All three models use the same `NVIDIA_NIM_BASE_URL` environment variable. Change
the URL, and all three models instantly point at your new endpoint — no code changes.

## Files

```
agent.yaml            the agent: models, prompt, greeting, channel, tool
targets.yaml          the two frameworks, and the overrides LiveKit needs
instructions.md       the prompt
tools/end_call.yaml   lets the agent hang up
```

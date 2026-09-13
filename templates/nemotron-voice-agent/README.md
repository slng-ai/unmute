# nemotron-voice-agent

NVIDIA's [Nemotron Voice Agent blueprint](https://github.com/NVIDIA-AI-Blueprints/nemotron-voice-agent),
expressed as an Unmute definition.

One package. Two frameworks. Nothing rewritten between them.

```bash
unmute validate
unmute compile --target pipecat     # writes a Pipecat project
unmute compile --target livekit     # writes a LiveKit Agents project
```

## Point it at your own models

```bash
NVIDIA_NIM_BASE_URL=https://integrate.api.nvidia.com/v1     # NVIDIA-hosted
NVIDIA_NIM_BASE_URL=http://your-workstation:8000/v1         # your own NIM
NVIDIA_NIM_BASE_URL=http://jetson.local:8000/v1             # edge
```

Same package every time. `endpoint_env` is resolved at run time, so moving
between NVCF, a DGX box and a Jetson is an environment variable, not a fork.

## The two axes

The blueprint's deployment profiles combine two decisions that are actually
independent:

| | |
| --- | --- |
| **Where the models run** | `endpoint_env` — NVCF, your workstation, DGX Spark, Jetson Thor |
| **Where the agent runs** | `--target` — Pipecat Cloud, LiveKit Cloud, your own cloud |

So Nemotron on your own Jetson with the agent on Pipecat Cloud is a legal
combination, and so is Nemotron on build.nvidia.com with the agent running
on LiveKit Cloud. It is a matrix rather than one choice.

## What binds today, and what does not

**`think` — Nemotron — binds on both code targets with no new integration.**
NIM LLM endpoints are OpenAI-compatible, which is exactly the route both
compilers keep open for an unlisted provider: Pipecat accepts one named with
`endpoint_env`, LiveKit accepts one through LiveKit Inference.

**`listen` and `speak` are bound to SLNG speech here**, not to Parakeet and
Chatterbox. NIM speech services are not OpenAI-compatible, so they do not
travel the same wildcard route, and Unmute will not let you invent a provider
name to pretend otherwise.

Getting NVIDIA speech models into the catalogue is a small, well-defined piece
of work rather than a blocker — and it is the one thing that would make this
package bind the full NVIDIA stack end to end.

## Files

```
agent.yaml       the agent: models, prompt, greeting, channel
targets.yaml     where it runs. The LiveKit turn-detector override is the
                 only line that differs between targets
instructions.md  the prompt
```

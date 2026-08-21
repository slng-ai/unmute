# regional-infrastructure

A small browser voice agent that shows the two regional controls in an Unmute
package: where the agent worker runs, and where SLNG processes speech. Keeping
them explicit helps you meet latency and data-location requirements.

## Regional settings

[agent.yaml](agent.yaml) routes each speech model:

```yaml
models:
  listen:
    transcriber:
      provider: slng
      model: "slng/deepgram/nova:3-multi"
      params:
        world_part_override: eu
  speak:
    voice:
      provider: slng
      model: "slng/deepgram/aura:2-en"
      voice: "aura-2-thalia-en"
      params:
        region_override: eu-north-1
        world_part_override: eu
```

`world_part_override: eu` lets SLNG choose an available STT region in Europe.
`region_override: eu-north-1` pins TTS to that exact region. Use the world part
when any region in a geographic area is acceptable. Use the exact region when
the workload should use one location. If you set both, the exact region wins
and the world part is ignored; it is not a fallback.

[targets.yaml](targets.yaml) places the agent workers:

```yaml
targets:
  livekit:
    provider: livekit
    pins:
      livekit-plugins-slng: "1.6.7"
    deployment_region: eu-central

  pipecat:
    provider: pipecat
    deployment_region: eu-central
```

Both runnable targets place the worker in Europe. These deployment values belong
to the hosting platform; they are not SLNG model-region names. The LiveKit pin
also makes sure the generated project installs the plugin version that supports
both regional model parameters.

LiveKit also accepts a list of deployment regions and creates one deployment per
entry. Pipecat Cloud accepts only one region per target. A LiveKit region list is
useful for availability, but it is not strict regional isolation: LiveKit can
send a caller to another deployment when the nearest one is at capacity. For
strict locality, create separate single-region targets with separate agent
names, then use explicit dispatch to select the right agent.

## Run it

You need Docker and the two keys listed in the generated `.env.example`:
`OPENAI_API_KEY` and `SLNG_API_KEY`.

```sh
bin/unmute validate examples/regional-infrastructure
bin/unmute compile examples/regional-infrastructure
cp examples/regional-infrastructure/build/pipecat/.env.example examples/regional-infrastructure/.env
bin/unmute dev examples/regional-infrastructure --target pipecat
```

Use `--target livekit` to run the same browser agent with the LiveKit driver.
If another local LiveKit server already uses ports `7880`–`7882`, move all
three browser WebRTC ports:

```sh
LIVEKIT_HOST_PORT=7890 LIVEKIT_TCP_HOST_PORT=7891 LIVEKIT_UDP_HOST_PORT=7892 \
  bin/unmute dev examples/regional-infrastructure --target livekit --port 8766
```

The generated README in each build directory contains the deployment commands
for that platform.

Read [regional infrastructure](../../docs-site/optimization/regional-infrastructure.mdx)
for media placement and the full residency checklist. Reference pages:
[agent.yaml](../../docs-site/reference/agent-yaml.mdx),
[targets.yaml](../../docs-site/reference/targets-yaml.mdx),
[STT models](../../docs-site/models/stt.mdx), and
[TTS models](../../docs-site/models/tts.mdx).

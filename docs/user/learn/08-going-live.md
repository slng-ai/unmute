# 08. Going live

The agent works in `dev`. This page covers the last mile: running one spec against more than one target, declaring capacity, and keeping secrets out of the files.

## One spec, many targets

`targets.yaml` can hold several target instances. They all compile from the
same `agent.yaml`: model definitions live there, while each instance carries
its infrastructure and any by-name overrides for models that platform cannot
run as defined. Start with one instance per platform you are evaluating:

```yaml
targets:
  pipecat:
    provider: pipecat
    version: "1.5.0"
  livekit:
    provider: livekit
    version: "1.5.2"
    sdk_language: python
    models:
      # LiveKit swaps the VAD entry for its own turn model
      vad: { provider: livekit, model: turn-detector-mini }
```

Pick one with `--target`, or compile every declared target by leaving it off:

```sh
unmute compile acme --target livekit          # one target
unmute compile acme                           # every declared target
```

Add another instance of the same framework when you have a real separate route,
region, or account. Telephony is the common case: `pipecat_twilio` and
`pipecat_telnyx` are separate single-carrier projects with separate Connections
and route limits. A package may declare any number of supported routes, but one
target never combines them. See
[configure multiple carriers](07-phone-calls.md#configure-multiple-carriers).

## Check portability before you commit

Here is the useful move before choosing a platform: **`validate` runs against
all five targets, including providers without a generator.** Validation uses
the schema's capability rules, so it can tell you what passes, warns, or fails
before generation:

```sh
unmute validate acme
```

```text
✓ elevenlabs (elevenlabs)
✓ livekit (livekit)
✓ pipecat (pipecat)
✗ vapi (vapi)
```

Warnings and errors print per target. LiveKit and Pipecat have shipped code
drivers, so `compile` writes both native projects. ElevenLabs has a shipped
managed driver and uses `unmute apply`. Vapi and Deepgram still validate, but
generation fails with `driver is not implemented` until their drivers ship.

## Capacity

Every package declares expected traffic in `capacity`:

```yaml
capacity:
  peak_sessions: 40          # concurrent calls at the busy hour
  max_sessions: 60           # hard limit; reject above this
  peak_starts_per_second: 4  # required for telephony
  avg_session_duration: 6m
```

It is **required** when the package has a code target, including LiveKit or
Pipecat, or a telephony channel. `peak_sessions` must not exceed
`max_sessions`. A telephony package also declares a positive
`peak_starts_per_second`; starting calls and keeping calls active are separate
capacity constraints. Capacity depends on concurrency, model placement, and
channels, not the number of agents or carrier targets in the file.

Capacity feeds Unmute's worker, GPU, and provider quota sizing. `compile`
prints each derived sizing line with its status and basis;
`compile-report.json` records the same contract for the generated target.

## Secrets stay out of the files

None of your spec files ever hold a secret. This is a rule, not a convention:

- Tool endpoints are **environment variable names** (`url_env: LOOKUP_CUSTOMER_URL`), never URLs.
- Provider keys are referenced by name (`OPENAI_API_KEY`, `SLNG_API_KEY`) and set in the environment, never written in `targets.yaml`.
- Phone numbers in `destinations:` are configuration, not secrets, and may live in the target.

For local runs, put values in a `.env` at the package root; `unmute dev` reads it. When you `compile`, Unmute writes a `build/<target>/.env.example` listing the exact variables that target needs, so you always know what to provide. The `compile-report.json` lists the required environment too.

For deployment, both generated projects include a `Dockerfile`. Pipecat adds
`pcc-deploy.toml` for Pipecat Cloud; LiveKit adds `livekit.toml`. Both are
ordinary Python projects, so you can supply the listed environment variables
through any host and run them without Unmute present.

## You have gone the whole way

From [one agent](01-one-agent.md) to a complex, multi-agent, tool-using,
task-delegating voice agent, the package is described once, checked against all
five platforms, and compiled to native LiveKit and Pipecat projects.

To go deeper, use the [YAML reference](../reference/agent-yaml.md),
[LiveKit guide](../targets/livekit.md), or
[Pipecat guide](../targets/pipecat.md).

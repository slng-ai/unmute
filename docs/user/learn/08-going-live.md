# 08. Going live

The agent works in `dev`. This page covers the last mile: running one spec against more than one target, declaring capacity, and keeping secrets out of the files.

## One spec, many targets

`targets.yaml` can hold several target instances, each named after its provider. They all compile from the same `agent.yaml` — the model definitions live there; each instance only carries its infrastructure, its `listen`/`turn` role slots, and any overrides for models that platform cannot run as defined. One instance per platform you are evaluating:

```yaml
targets:
  pipecat:
    provider: pipecat
    version: "1.5.0"
    models:
      listen: { provider: slng, model: "slng/deepgram/nova:3-en" }
      turn:   { provider: local, model: silero }
  livekit:
    provider: livekit
    version: "1.5.2"
    sdk_language: python
    models:
      listen: { provider: slng, model: "slng/deepgram/nova:3-en" }
      turn:   { provider: livekit, model: turn-detector-mini }
```

Pick one with `--target`, or compile every declared target by leaving it off:

```sh
unmute compile acme --target livekit          # one target
unmute compile acme                           # every declared target
```

Add a second instance of the *same* provider only when you have a real second environment (a separate region or account, say); the default is one instance per provider, so what you test is what you deploy.

## Check portability before you commit

Here is the useful move before choosing a platform: **`validate` runs against all five targets even though only the Pipecat driver ships today.** Validation uses the schema's capability rules, not a driver, so it can tell you, per target, what would pass, warn, or fail:

```sh
unmute validate acme
```

```text
TARGET            PROVIDER    RESULT
elevenlabs   elevenlabs  pass
pipecat       pipecat     pass
vapi         vapi        fail
```

with the warnings and errors printed per target. So you can declare a target for each of the five, run `validate`, and see exactly which features cost you a warning or a failure on each platform, before writing a line of platform code. Then `compile` the one whose driver is ready (Pipecat today). The other drivers error on `compile` until they ship; `validate` still tells you where you stand.

## Capacity

Every package declares expected traffic in `capacity`:

```yaml
capacity:
  peak_sessions: 40          # concurrent calls at the busy hour
  max_sessions: 60           # hard limit; reject or queue above this
  avg_session_duration: 6m
```

It is **required** on Pipecat, because a code target is something you host, and required whenever you have a telephony channel. `peak_sessions` must not exceed `max_sessions`. Capacity does not depend on how many agents are in the file, only on concurrency, model placement, and channels.

Capacity feeds Unmute's sizing (how many workers, how much GPU, which quotas). Be aware of the current limit: **the sizing numbers are derived internally but are not yet printed by the CLI.** Declaring capacity is validated and correct; a user-facing sizing report is not surfaced yet. Do not expect worker or GPU counts in the output today.

## Secrets stay out of the files

None of your spec files ever hold a secret. This is a rule, not a convention:

- Tool endpoints are **environment variable names** (`url_env: LOOKUP_CUSTOMER_URL`), never URLs.
- Provider keys are referenced by name (`OPENAI_API_KEY`, `SLNG_API_KEY`) and set in the environment, never written in `targets.yaml`.
- Phone numbers in `destinations:` are configuration, not secrets, and may live in the target.

For local runs, put values in a `.env` at the package root; `unmute dev` reads it. When you `compile`, Unmute writes a `build/<target>/.env.example` listing the exact variables that target needs, so you always know what to provide. The `compile-report.json` lists the required environment too.

For deployment, the generated Pipecat project ships a `Dockerfile` and a `pcc-deploy.toml` (Pipecat Cloud). Because the output is an ordinary Python project, you supply those same environment variables however your host does secrets, and run it.

## You have gone the whole way

From [one agent](01-one-agent.md) to a complex, multi-agent, tool-using, task-delegating voice agent, described once and compiled to a real Pipecat project, checked for portability across all five platforms, and ready to host.

To go deeper on any single field, the [reference](../reference/agent-yaml.md) pages give the exact contract, and the [Pipecat target page](../targets/pipecat.md) is your deployment companion.

# The package

What an author writes, file by file. This is the surface you work in.

## The files

| File | Required | What it holds |
|---|---|---|
| `agent.yaml` | yes | the agent: models, prompts, tools, conversation, channels |
| an instructions file | yes | the prompt, in Markdown, named by each agent |
| `targets.yaml` | yes | where it runs, and the framework version pinned |
| `tools/<name>.yaml` | no | one file per tool |
| `tools/<name>.py` | no | a Python handler, beside its tool file |
| `connections/<name>.yaml` | phone agents only | one phone route |
| `build/<target>/` | generated | never edit, never hand-write |

Nothing in `agent.yaml` is specific to a runtime. That lives in `targets.yaml`.
Keeping them apart is what makes one package compile to two orchestrators.

For parallel local LiveKit packages, give each stack a different three-port
set with `LIVEKIT_HOST_PORT`, `LIVEKIT_TCP_HOST_PORT`, and
`LIVEKIT_UDP_HOST_PORT`. Moving only the signaling port is not enough for
browser WebRTC.

YAML decoding is strict. An unknown field is an error with the file and the
line, not a shrug. A field you half remember is worth checking rather than
writing.

## The smallest agent that works

```yaml agent.yaml
version: 1
entry_agent: appointment_desk

secrets:
  - OPENAI_API_KEY
  - SLNG_API_KEY

models:
  think:
    reasoning:
      provider: openai
      model: gpt-5.6-terra
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

agents:
  appointment_desk:
    instructions: instructions.md
    model: reasoning
    voice: voice

conversation:
  greeting:
    speaks_first: agent
    text: "Hi, this is Sage and Stone Salon. How can I help with your appointment?"
  interruption:
    enabled: true

channels:
  web:
    kind: realtime_audio

capacity:
  peak_sessions: 5
  max_sessions: 10
  avg_session_duration: 5m
```

That is `examples/simple-prompt`, and it runs in a browser.

## Every top-level key

| Key | Required | What it is |
|---|---|---|
| `version` | yes | the schema version, `1` |
| `entry_agent` | yes | which agent answers |
| `models` | yes | the model palette, grouped by kind |
| `listen` | when `models.listen` has two or more entries | which listen entry to use |
| `turn` | when `models.turn` has two or more entries | which turn entry to use |
| `variables` | no | per call values |
| `secrets` | no | environment names the generated project reads |
| `destinations` | when a `human_transfer` is used | symbol to the environment variable holding a number |
| `agents` | yes | one or more agents |
| `tasks` | no | delegated steps |
| `task_groups` | no | ordered sequences of tasks |
| `controls` | no | delegate, agent transfer, human transfer |
| `tools` | no | which tool files to load |
| `conversation` | no | greeting, interruption, inactivity, limits |
| `tracing` | no | tracing provider |
| `channels` | yes | how people reach the agent |
| `capacity` | for telephony or code targets | your traffic estimate |

## models

Four sections, and the section an entry sits in decides its kind.

| Section | Job | Common name |
|---|---|---|
| `think` | decides what to say and which tool to call | LLM |
| `speak` | turns text into audio | TTS |
| `listen` | turns audio into text | STT |
| `turn` | decides when the caller has finished speaking | turn detection or VAD |

Entry names are yours and share one namespace across sections. An agent points
at an entry by name. Entries you never reference are legal alternates, so
swapping a voice is a one line change.

Each section allows these fields:

| Field | Legal section |
|---|---|
| `provider`, `model`, `endpoint_env`, `placement`, `params`, `description` | `think`, `speak`, `listen`, `turn` |
| `voice`, `speed` | `speak` |
| `language` | `speak`, `listen` |
| `temperature`, `top_p`, `top_k` | `think` |
| `semantic_endpointing` | `turn`: `required`, `preferred`, or `off` |
| `endpointing_delay` | `turn`: a positive duration, the window of silence before the caller counts as finished. The floor on every turn. LiveKit refuses under `250ms`; defaults differ per target (LiveKit `550ms`, Pipecat `200ms`) |
| `fallback` | `think`, `listen` |

Unmute keeps no list of valid model ids. `model:` and `voice:` are forwarded to
the provider exactly as written, so a typo is a provider error at run time, not
a compile error. The compile report says so out loud. Which `provider:` values
are legal per role per target is in `models.md`.

`params:` is normally the same passthrough. The narrow exception is
`api: responses` on a LiveKit OpenAI reasoning binding: Unmute checks the
directive, selects the Responses client, and maps `reasoning_effort` to nested
reasoning. The scaffold's default OpenAI reasoning model needs
`reasoning_effort: "none"` when it carries function tools. Do not copy that
provider-specific value onto every think model. `models.md` has the rule.

Do not guess model ids, voice ids, or params. Use values the user supplied or
values verified in the provider's own documentation.

## agents

```yaml
agents:
  appointment_desk:
    instructions: instructions.md
    model: reasoning
    voice: voice
    tools:
      - check_availability
```

| Field | What it is |
|---|---|
| `instructions` | path to a Markdown prompt in the package |
| `model` | a `models.think` entry name |
| `voice` | a `models.speak` entry name |
| `tools` | names this agent may call: tool files and controls |

The prompt lives in its own file so it reviews like prose rather than like YAML.

## secrets

```yaml
secrets:
  - OPENAI_API_KEY
  - SLNG_API_KEY
```

A list of `UPPER_SNAKE` environment variable names. Never values, and never
usable in a `{{template}}`.

Declare every environment name the generated project reads: provider and
tracing keys inferred from the package, tool `*_env` names, connection
`environment:` values, destinations, and names read by local handlers. Names
the driver or platform supplies, such as `REDIS_URL` or `DAILY_API_KEY`, are not
yours to declare and are left out.

Every name must be a valid shell identifier: letters, digits, and underscores,
never starting with a digit. A platform exports secrets through a shell, so a
name starting with a digit would go missing at run time with no error of its
own. The compiler refuses it first.

**`unmute validate` checks that the list is complete**, and warns at exit 0
naming every environment name the package references and this block does not
declare, with the file and field that named it. It warns whether or not the
block exists, so deleting it does not buy silence — a package that declares
nothing and references eight names is the case most worth reporting.

The generated agent's own startup check is derived the same way: from what the
compiler knows the package requires, not from what you remembered to declare. So
an undeclared name still stops the container rather than failing on the first
call.

Write the list as you go anyway: every time you add an `*_env` field, a
connection `environment:` name, or a `destinations:` entry, add the name here in
the same edit. `unmute init` scaffolds the block for you with the model provider
keys already in it.

The package-root `.env.example` is a one-time snapshot from `unmute init`; it
does not update when you add a secret. The list that stays true is
`build/<target>/.env.example`, regenerated on every compile, and it names only
what you fill in.

## channels

```yaml
channels:
  web:
    kind: realtime_audio
  phone:
    kind: telephony
    inbound: true
    outbound: true
```

| Field | Values |
|---|---|
| `kind` | `realtime_audio` or `telephony` |
| `inbound`, `outbound` | `true` or `false`; at least one must be `true` |
| `required_controls` | `cold_transfer`, `warm_transfer`, `dtmf_send`, `dtmf_receive`, `hold`, `hangup`, `voicemail_detection`, or `ivr_navigation` |
| `on_voicemail` | `hangup` or `leave_message`, requires `outbound: true` |

`web: realtime_audio` is browser audio, which is what `unmute dev` serves.
It takes only `kind`. A `telephony` channel requires both `inbound` and
`outbound`, at least one set to `true`, and it needs a connection file too. See
`telephony.md`.

## capacity and tracing

```yaml
capacity:
  peak_sessions: 5
  max_sessions: 10
  peak_starts_per_second: 2
  avg_session_duration: 5m

tracing:
  provider: langfuse
```

`capacity` is the user's traffic estimate, not a measurement. The compiler turns
it into worker and quota numbers in the generated project and marks them
`[unbenchmarked]`, because they come from a conservative assumption of one
session per worker. Say that when you write one.

**`peak_starts_per_second` is optional on a browser-only package and required
the moment any channel is `telephony`.** Leave it out of a phone package and
validation fails:

```
pipecat: capacity.peak_starts_per_second must be positive for telephony
```

Write it whenever you write a `telephony` channel, in the same edit. It has to
be positive, and `1` is a fine starting number for a package nobody has measured
yet. `peak_sessions` and `max_sessions` must also be positive,
`peak_sessions` cannot exceed `max_sessions`, and `avg_session_duration` must
be a positive Go duration such as `5m`.

`provider` takes one of two values, `langfuse` or `coval`. Tracing works on the
`pipecat` and `livekit` targets and is refused on `vapi` and `deepgram`.

| provider | secrets it needs | use it for |
|---|---|---|
| `langfuse` | `LANGFUSE_BASE_URL`, `LANGFUSE_PUBLIC_KEY`, `LANGFUSE_SECRET_KEY` | reading one live conversation |
| `coval` | `COVAL_API_KEY` | scoring simulated calls in Coval |

Those names go in `secrets:` like any others.

LiveKit uses the room name as the Langfuse session ID. Pipecat uses the runner
session ID as both its conversation ID and the Langfuse session ID.
Pipecat tracing owns the process OpenTelemetry provider and startup fails if another SDK provider is installed first.

With `coval`, each trace is attached to the Coval simulation that placed the
call. The agent finds that simulation ID on the call itself, so the package
author writes no correlation code. The route differs per target: LiveKit reads
the SIP participant attribute `coval.simulation_id` (the emitted
`sip-inbound-trunk.json` maps the `X-Coval-Simulation-Id` SIP header onto it) or
its own dispatch metadata; Pipecat reads the WebSocket upgrade header, the
Pipecat Cloud dial-in SIP headers, or a carrier stream parameter. Both fall back
to `COVAL_SIMULATION_ID` in the environment. A call that no simulation owns is
traced too: when it ends the agent submits what was said to Coval as a
conversation and exports the same spans against the returned conversation ID,
which is what puts a local `unmute dev` run in Coval's Trace Search.
`coval.correlation.method` says which of the two routes a trace took. Without
`COVAL_API_KEY`, or with nothing said on the call, nothing is sent and the call
still runs.

Coval reads a call as `conversation` > `turn` > `stt` | `vad` | `llm` | `tts`,
with `stt.provider.<name>` under `stt` and `llm_tool_call` under `llm`. Each
`llm` span carries the prompt that round ran on: `gen_ai.system_instructions`,
the message history as `input`, the offered tools as `tools` and `tool_count`,
the answering agent as `agent.label`, and the reply as `output`. Those are
Pipecat's own attribute names, used on both targets so one metric or judge
prompt reads either. All of it is size-bounded. Pipecat
already emits that tree, so its own spans are what Coval sees. LiveKit's spans
are shaped differently, so the emitted module builds the tree from LiveKit's
session events and leaves LiveKit's own OpenTelemetry off; the numbers are still
LiveKit's own measurements. A missing `COVAL_API_KEY`
warns and disables Coval tracing instead of failing startup, unlike Langfuse,
which fails when its keys are missing. Coval trace metrics read these spans
directly: percentiles over `metrics.ttfb` on `llm` and `tts` (and `vad` on
LiveKit) find the bottleneck, averages over `gen_ai.usage.*` on `llm` track
cost, and an LLM-judged metric created with traces included can check the
agent's answers against its real prompt and tool results.

Traces can contain caller speech, model input and output, and tool arguments and results.
Use only fake identities and fake customer data for release tests. Use a separate
project on the tracing provider for those tests, and do not send real customer
data until its access and retention rules are approved.

## targets.yaml

```yaml targets.yaml
targets:
  pipecat:
    provider: pipecat
    version: "1.7.0"

  livekit:
    provider: livekit
    version: "1.6.10"
    sdk_language: python
    models:
      detector:
        provider: livekit
        model: turn-detector-mini
```

The map key is the **target instance name**. It is what `--target` takes and
what names the output directory, `build/<name>/`. Two targets may use the same
provider with different settings, for example `pipecat_twilio` and
`pipecat_telnyx`.

| Field | What it is |
|---|---|
| `provider` | `livekit`, `pipecat`, `vapi`, or `deepgram`; only the first two generate and run |
| `version` | required exact `x.y.z` framework version for code targets |
| `pins` | LiveKit-only known package pins, name to semantic version |
| `sdk_language` | `python` when written |
| `connection` | required for LiveKit or Pipecat telephony; illegal with no phone use |
| `deployment_region` | one non-empty region, or a duplicate-free list; multiple regions are LiveKit-only |
| `models` | per target overrides of named `models` entries |

That is the whole list. A `models` override is keyed by the entry name from
`agent.yaml` and takes the same fields. Use it when a target cannot run an entry
as defined, or runs its own better. Override the entry, never the agent.

Transport, carrier, and destinations used to live here and no longer do. Writing
any of them on a target is refused, and the refusal names the new home.

## Deployment regions and model regions

`deployment_region` chooses where the agent worker runs. It does not choose
where SLNG runs STT or TTS. Those model regions belong in
`params.world_part_override` or `params.region_override` on the SLNG listen and
speak entries. Set both layers when the worker and speech data must stay in the
same geography; `models.md` has the model YAML.

A LiveKit target accepts one deployment region or a duplicate-free list.
Pipecat accepts exactly one. Every deployment from one LiveKit target uses the
same generated model params, so a multi-region deployment does not move STT or
TTS with each worker.

For hard regional isolation, use one target instance per geography. Give each
target one deployment region and complete per-target listen and speak model
overrides that pin the matching SLNG region. A target model override replaces
the entry instead of merging it, so repeat every field that entry needs.

## Which targets do what

| Provider | Validates | Generates and runs |
|---|---|---|
| `pipecat` | yes | yes |
| `livekit` | yes | yes |
| `vapi` | yes | no |
| `deepgram` | yes | no |

Validation is deliberately wider than generation. A package can be checked
against Vapi or Deepgram without a driver existing for them, and `unmute
compile` fails by name for a provider with no driver. When you tell a user a
target is supported, say which of the two you mean.

LiveKit and Pipecat versions are exact three-part versions in this release's
supported window. Unmute never widens the pin. `pins` accepts only packages the
LiveKit driver knows; other providers do not consume it. A target's `models`
map is keyed by an existing model entry name and takes the same model fields.

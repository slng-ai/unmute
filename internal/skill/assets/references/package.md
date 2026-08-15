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
      model: gpt-4.1-mini
      temperature: 0.2
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
| `secrets` | no | every environment variable name this package writes |
| `destinations` | when a `human_transfer` is used | symbol to the environment variable holding a number |
| `agents` | yes | one or more agents |
| `tasks` | no | delegated steps |
| `task_groups` | no | ordered sequences of tasks |
| `controls` | no | delegate, agent transfer, human transfer |
| `tools` | no | which tool files to load |
| `conversation` | no | greeting, interruption, inactivity, limits |
| `tracing` | no | tracing provider |
| `channels` | yes | how people reach the agent |
| `capacity` | no | your traffic estimate |

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

Fields on an entry: `provider`, `model`, `voice`, `speed`, `language`,
`temperature`, `top_p`, `top_k`, `endpoint_env`, `placement`,
`semantic_endpointing`, `params`, `fallback`, `description`.

Unmute keeps no list of valid model ids. `model:` and `voice:` are forwarded to
the provider exactly as written, so a typo is a provider error at run time, not
a compile error. The compile report says so out loud. Which `provider:` values
are legal per role per target is in `models.md`.

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

The rule is: every environment name you wrote anywhere in this package goes
here. Model and tool credentials, the names a connection's `environment:` maps
to, and the numbers `destinations:` points at. Names the driver or the platform
supplies for you, such as `REDIS_URL` or `DAILY_API_KEY`, are not yours to
declare and are left out.

Every name must be a valid shell identifier: letters, digits, and underscores,
never starting with a digit. A platform exports secrets through a shell, so a
name starting with a digit would go missing at run time with no error of its
own. The compiler refuses it first.

**Nothing checks that the list is complete.** Verified 2026-08-15: deleting the
whole `secrets:` block from a package that genuinely uses `OPENAI_API_KEY` and
`SLNG_API_KEY` still validates clean, exit 0, with no warning. `unmute init`
does not scaffold a `secrets:` block at all. So writing this list is on you, and
an incomplete one fails much later, when the deployed container cannot find a
key. Write it as you go: every time you add an `*_env` field, a connection
`environment:` name, or a `destinations:` entry, add the name here in the same
edit.

Two more things `unmute init` leaves you to fix, so check them rather than
trusting the scaffold:

- The scaffolded `instructions.md` says "This is a phone call" while the
  scaffolded `agent.yaml` declares only `web: realtime_audio`. Rewrite the
  prompt to match the channel the package actually has.
- The scaffolded root `.env.example` names `DAILY_API_KEY`, which a browser
  package never uses, and it does not update when you add secrets. It is a
  one-time snapshot. The list that stays true is
  `build/<target>/.env.example`, regenerated on every compile.

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
| `inbound`, `outbound` | `true` or `false` |
| `required_controls` | controls this channel requires |
| `on_voicemail` | `hangup` or `leave_message`, requires `outbound: true` |

`web: realtime_audio` is browser audio, which is what `unmute dev` serves.
Phones are `telephony`, and they need a connection file too. See
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
yet.

`langfuse` is the only tracing provider today. It needs `LANGFUSE_BASE_URL`,
`LANGFUSE_PUBLIC_KEY`, and `LANGFUSE_SECRET_KEY`, and those names go in
`secrets:` like any others.

## targets.yaml

```yaml targets.yaml
targets:
  pipecat:
    provider: pipecat
    version: "1.5.0"

  livekit:
    provider: livekit
    version: "1.5.2"
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
| `provider` | `pipecat` or `livekit` |
| `version` | the framework version pinned in the generated project |
| `pins` | extra package pins, name to version constraint |
| `sdk_language` | `python` |
| `connection` | the connection file that declares this target's phone route |
| `deployment_region` | one region as a string, or several as a list |
| `models` | per target overrides of named `models` entries |

That is the whole list. A `models` override is keyed by the entry name from
`agent.yaml` and takes the same fields. Use it when a target cannot run an entry
as defined, or runs its own better. Override the entry, never the agent.

Transport, carrier, and destinations used to live here and no longer do. Writing
any of them on a target is refused, and the refusal names the new home.

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

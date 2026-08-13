# Pipecat

Pipecat is one of Unmute's two shipped Python code targets. It compiles the
same portable YAML as LiveKit, but lowers agents to workers and tasks to Flows.
This page covers its models, emitted features, local runtime, and deployment.

## What kind of target

Pipecat is a **code target.** Unmute writes a real Python project, you host it. This is the opposite of a managed target like Vapi, where the provider runs the agent and you only get an API.

Being a code target is why Pipecat can do so much. When a feature is not a built-in Pipecat setting, like a handoff guard or a delegated task, Unmute writes the Python for it. There is a project to put the code in. This is [the pattern rule](../concepts/our-take-on-orchestrators.md) working in your favor: on Pipecat, nearly the whole schema is available.

What it means for you:

- You get a folder of code you can read, run, and deploy without Unmute present.
- You host it yourself (locally with `unmute dev`, or on your own infrastructure or Pipecat Cloud).
- You need the keys for the providers your selected models use.
- Because you host it, `capacity` in `agent.yaml` is required: Unmute uses it to size the deployment.

## The agency model: workers

Everything Unmute generates for Pipecat is built on Pipecat's **workers** model. Knowing the shape helps you read the generated `bot.py`:

- A single **main worker** owns the transport and the listen (speech-to-text) step. It hears the caller.
- **Each agent is its own worker**, with its own reasoning model and its own voice. Only one agent is active at a time.
- **A handoff** (`agent_transfer`) is one worker activating another and stepping aside.
- **A task or task group** runs as a Pipecat Flow on the agent that delegated
  it. With `history: full`, the Flow keeps the agent's running context and
  replaces its system instruction with the current task prompt. It replaces
  the tools for the step, then restores the owner's system instruction,
  messages, and tools when the step finishes. No extra worker is involved.

So a two-agent, one-task spec becomes a main worker plus two agent workers, wired together, with the task living inside its delegating agent. You never edit this; you change the spec and recompile. (For how the same yaml lowers on LiveKit, see [how targets run your agent](../concepts/how-targets-run-your-agent.md).)

The one exception is the simplest case. A **single agent with no handoffs and no
tasks** skips the workers machinery entirely: its model runs inline in one
pipeline and its tools are plain functions on the LLM context, so the generated
`bot.py` has no bus or worker scaffolding to read. Turning on tracing, adding
`variables`, or using telephony keeps the workers shape even for one agent.

## What gets generated

`unmute compile acme --target pipecat` writes a complete project to `acme/build/pipecat/`:

| File | What it is |
|---|---|
| `bot.py` | The whole agent: the pipeline, every agent worker, every task flow, tools, handoffs. |
| `pyproject.toml` | Pinned dependencies. Only the services your spec uses are included. |
| `Dockerfile` | A container image for deployment. On a target that deploys to Pipecat Cloud it is built on `dailyco/pipecat-base`, because the platform starts a session by calling `/bot` on the container and that route is the base image's. A plain Python base serves nothing the platform calls, so the deployment never reaches ready. Telephony targets, which are self-hosted, keep the plain base and their own server command. |
| `compose.dev.yaml` | The local dev stack `unmute dev` runs: one `application` service built from the `Dockerfile`, no coordination store. |
| `pcc-deploy.toml` | The manifest `pipecat cloud deploy` reads, for non-telephony targets: the agent name, its region, and the secret set holding its environment. It deliberately names no image, so the platform builds the emitted `Dockerfile` in the cloud. |
| `compose.telephony.yaml` | The generated application plus version-pinned Redis for telephony targets. |
| `README.md` | A quickstart for the generated project. |
| `.env.example` | The exact environment variables this spec needs, ready to copy to `.env`. |
| `compile-report.json` | A machine-readable record of what was compiled: target, version, agents, required env, and any warnings. |

That command works for telephony and non-telephony Pipecat targets alike.
Provisional telephony routes compile and write their files, including
`compose.telephony.yaml`, cleanly and with no warning. Only the gated no-adapter
route (Exotel) stops validation, because there is nothing to generate for it.

The output folder is rewritten from scratch on every compile, so never edit it by hand. `bot.py` carries only the imports and code your spec actually exercises: no tools means no HTTP client import, no tasks means no task machinery. The emitted pipeline stays clean.

As it writes the project, Unmute runs `ruff format` over the emitted Python so
the code is consistently formatted out of the box. `ruff` is optional: if it is
not on your `PATH`, Unmute writes the (still valid) code unformatted and prints
a one-line warning to stderr.

The `compile-report.json` is worth reading after a compile. It lists every
required environment variable and every forwarded model, so you can see what
was sent to the platform. Provider model IDs and parameters are
[forwarded without validation](../concepts/profiles-and-bindings.md).

## Models on Pipecat

All four model kinds are **open** on Pipecat: you choose listen, speak, and
think models freely, and turn runs on your machine. Every model is defined in
`agent.yaml`'s kind sections; the Pipecat target carries infrastructure and
optional by-name overrides. The accepted providers, required keys, installed
packages, and emitted services are in the
[providers reference](../reference/providers.md#pipecat).

```yaml
# agent.yaml — models defined once, by kind
models:
  think:
    fast_reasoning:
      provider: openai
      model: gpt-4o-mini
      temperature: 0.4
    careful_reasoning:
      provider: openai
      model: gpt-4o
  speak:
    front_desk:
      provider: slng
      model: "slng/deepgram/aura:2-en"
      voice: "aura-2-thalia-en"
    specialist:
      provider: slng
      model: "slng/deepgram/aura:2-en"
      voice: "aura-2-orion-en"
  listen:
    transcriber:
      provider: deepgram
      model: nova-3
  turn:
    vad:
      provider: local
      model: silero

# targets.yaml — infrastructure only
targets:
  pipecat:
    provider: pipecat
    version: "1.5.0"
```

### Map providers to services

The `provider` on each model selects a Pipecat service. Listen and speak support
the catalogued vendors for those kinds. Think models have native entries for
OpenAI, Anthropic, DeepSeek, Google, Groq, Mistral, OpenRouter, and Qwen. An
unlisted provider is legal only with `endpoint_env`, which selects the
OpenAI-compatible service and passes that environment variable as `base_url`.

See the [providers reference](../reference/providers.md#pipecat) for the full
matrix, installed extras, and environment variable names. The generated
`.env.example` and compile report list the exact keys your package needs.

### The turn role and Silero

The turn model is `{ provider: local, model: silero }`. End-of-turn detection
runs on-device with a Silero voice-activity detector: no key and no network
hop. The selected turn model is **advisory** on Pipecat; the generated runtime
uses the local VAD. Semantic endpointing is also advisory.

### Transport

- **WebRTC** is the default and is what `unmute dev` uses to serve a browser test client. You do not configure it.
- **`transport: cloud-websocket`** is the one route where **you host nothing**.
  Pipecat Cloud terminates your carrier's media stream itself, on its own
  endpoint; a small piece of static markup in the carrier's console (a TwiML Bin)
  names your agent, and the platform starts it. It requires `carrier: twilio`.
  `connection:` is required only when the package **places or redirects** calls:
  receiving one needs no carrier credentials, because the platform receives it
  without them.

  ```yaml
  targets:
    pipecat:
      provider: pipecat
      version: "1.5.0"
      transport: cloud-websocket
      carrier: twilio
      connection: twilio_voice     # omit on a receive-only package
  ```

  Three Connection keys: `account_sid`, `auth_token`, `from_number`. The first two
  authenticate the one request that hands a live call to a person and the outbound
  command; `from_number` is the caller identity the recipient sees, and it may be
  the same number that receives calls. **There is no `sip_address`, no
  `sip_username`, and no `sip_password`**, and the route refuses all three by
  name, naming the accepted set: nothing on this route speaks SIP.

  `deployment_region` is the only place a region is written: the deploy manifest,
  the secret set's region, and the `wss://` host in every piece of markup are all
  derived from it, because the platform routes a regional stream endpoint only to
  agents deployed in that region.

  The build emits **no new file**: the file list is exactly a plain Pipecat Cloud
  build's. The generated README dictates the carrier console work in four steps,
  three of them clicks and one a `pipecat cloud organizations list`. Cold transfer
  works here, by replacing the live call's markup; the limit is that **any** ending
  brings back a **fresh** agent that does not remember the call, a dial nobody
  answered and a person hanging up alike, which is what the Daily carrier form
  buys with its helper. The route comparison is in
  [TELEPHONY.md](../../TELEPHONY.md).
- **`transport: carrier-websocket`** selects direct Twilio, Telnyx, or Plivo
  media streaming for telephony. It also requires `carrier` and `connection`;
  support is resolved for that exact tuple. The Exotel value is recognized but
  gated until authenticated WebSocket ingress is proven. Pipecat does not use
  a SIP trunk for these routes; see the
  [orchestrator comparison](../learn/07-phone-calls.md#configure-telephony-by-orchestrator).
- **`transport: daily-sip`** is the one Pipecat route with a transfer primitive,
  and it is the managed route: the call arrives in a Daily room and your agent runs
  on Pipecat Cloud. It comes in **two forms**, and which one you get depends on
  whose number it is (SCHEMA N37).

  **Daily's number (no `carrier`).** You buy the number from Daily and point it at
  your deployed agent. It takes **no `carrier` and no `connection`**, and you
  declare **no phone channel**: there is no carrier to configure, so the compiler
  derives what the route needs from the transport. Nothing of yours is in the call
  path: no server, no public port. This is the least infrastructure of any
  telephony route in this project.

  **Your own number (`carrier:` plus `connection:` plus `channels.phone`).** Your
  carrier owns the number and forwards the call over SIP into the same Daily room.
  All three fields are required together, and a partial declaration fails naming
  the one you left out. Choose this form when you already hold a voice-capable
  number, or when you cannot provision Daily numbers.

  ```yaml
  targets:
    pipecat:
      provider: pipecat
      version: "1.5.0"
      transport: daily-sip
      carrier: twilio
      connection: twilio_sip_daily
  ```

  Four Connection keys: `account_sid`, `auth_token`, `sip_address`, `from_number`.
  The first two move the inbound call into the room; `sip_address` is your trunk's
  termination address, and every outbound leg, transfers included, leaves through
  it. **There is no `sip_username` and no `sip_password`**, and the route rejects
  both by name. Daily's dial-out accepts a SIP address with no credential field on
  any documented surface, so a key here would promise authentication nothing
  performs. Your trunk allows Daily by IP address list instead, and the generated
  README dictates that step with the address list's URL.

  This form emits one extra file, `telephony_helper.py`, and **you run it**. Daily
  makes one room per call and its SIP addresses are per room, so your carrier has
  no static address to forward to. The helper answers your carrier, asks the
  platform to start your agent on a fresh room with a SIP address on it, and keeps
  the caller hearing something while the agent boots; the agent then moves the live
  call into the room once Daily says the address is live. The deployed
  agent still exposes no endpoint of its own. The generated README's "Telephony
  setup" section is the whole runbook: four actions at your carrier, two commands
  here.

  Things that are true of both forms, and that the emitted `README.md` spells out:

  - **Dial-out has to be enabled on the Daily domain.** It is a paid feature
    granted on request, and international dial-out is granted separately. Cold
    transfer needs it, because it dials the destination, and so does outbound. It
    covers dialling a SIP address as well as a phone number, so the carrier form
    needs the same approval and needs **no purchased Daily number**. `unmute
    validate` names this before you spend anything, and still exits 0: the package
    is correct, the account may not be provisioned yet.
  - **`unmute dev --telephony` refuses**, on both forms, with a different message
    for each. On Daily's number there is no local topology at all. On your own
    number there is one, the helper, but the CLI cannot put it somewhere your
    carrier can reach; the README's two commands are the local path. Either way,
    talk to the agent in the browser or with `--console` right now, and get a real
    phone call by deploying.
  - **Caller-number variables are refused**, on both forms, by name. A variable
    sourced from the caller's number, the called number, the call identifier, or
    the direction has no fill path on this route: the code that puts those values
    where the agent reads them is part of the carrier-WebSocket adapter, which this
    route does not emit. The refusal names the routes where they do work.

### Telephony carrier integrations

Pipecat uses a separate generated adapter for each carrier. A target emits only
the selected adapter, serializer, dependency, and Connection vocabulary.

| Carrier | Connection keys | Generated integration | Status |
|---|---|---|---|
| Twilio | `account_sid`, `auth_token`, `from_number` | Signed webhooks and WebSocket, Twilio serializer, REST call control | Offline-tested; provisional |
| Telnyx | `api_key`, `public_key`, `connection_id`, `from_number` | Signed webhooks, one-use WebSocket, Telnyx serializer, Call Control API | Offline-tested; provisional |
| Plivo | `auth_id`, `auth_token`, `from_number` | Signed webhooks, one-use WebSocket, Plivo serializer, Calls API | Offline-tested; provisional |
| Exotel | `api_key`, `api_token`, `account_sid`, `subdomain`, `from_number`, `app_id` | None | Gated pending authenticated WebSocket ingress |

Use `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, and `TWILIO_PHONE_NUMBER` for
the Twilio Connection; `TELNYX_API_KEY`, `TELNYX_PUBLIC_KEY`,
`TELNYX_CONNECTION_ID`, and `TELNYX_PHONE_NUMBER` for Telnyx; and
`PLIVO_AUTH_ID`, `PLIVO_AUTH_TOKEN`, and `PLIVO_PHONE_NUMBER` for Plivo. The
Connection stores these names, while `.env` stores their values.

Every generated row runs now and is tagged provisional: validation and
compilation emit it cleanly, with no warning. The adapters contain inbound,
outbound, and hangup paths; they carry no transfers, because the websocket
transports have no transfer primitive ([TRANSFERS.md](../../TRANSFERS.md)).
An outbound channel runs without a voicemail policy; voicemail detection
stays gated on Pipecat, so setting `on_voicemail` still fails validation on
this route.

These routes use Twilio Programmable Voice and Media Streams, not SIP trunking,
which is why they want the account SID and auth token rather than trunk
credentials, and why they reach a laptop through the managed tunnel. See
[why the same carrier asks for different credentials](../learn/07-phone-calls.md#why-the-same-carrier-asks-for-different-credentials).

To configure several carriers, declare several Pipecat target instances, such
as `pipecat_twilio` and `pipecat_telnyx`, and bind each to its own Connection.
Each compiles to a separate project, and each provisional route runs cleanly,
with no warning. See
[phone calls](../learn/07-phone-calls.md#configure-multiple-carriers) for a full
Pipecat and LiveKit example.

### Version pin

`version` is required and must be in the range the driver's templates support: **`>= 1.5.0` and `< 2.0.0`.** Version 1.5.0 is the first release with the workers model and with Pipecat Flows shipped inside the core package. A version outside the range fails the compile with a clear message. This is a code check against the templates, not a guess about the platform.

### The SLNG plugin

Models with `provider: slng` use `pipecat-slng`, which routes speech-to-text
and voice through SLNG's hosted models with one `SLNG_API_KEY`. Unmute adds it
to `pyproject.toml` automatically. Model IDs look like
`slng/deepgram/nova:3-en`.

## Feature support on Pipecat

This is Pipecat's column from the Unmute schema. `ok` means it works, with no failures.

| Feature | Pipecat |
|---|---|
| single agent (T0) | ok |
| agent handoff, `full` history + `all` variables | ok |
| fixed opening line (`greeting.text`) | ok |
| model-written opening (no `text`) | ok (generated) |
| user speaks first | ok |
| webhook tools | ok |
| webhook `auth` (bearer/api_key) | ok |
| tool `output` schema, `interruption`, `effect` | ok |
| task (delegate and return) | ok |
| per-task `model` | not yet (driver gate) |
| task group, any `then` (return / transfer / end) | ok |
| task group `context_scope` (shared / isolated) | ok |
| handoff guard (`requires`) | ok |
| interruption `minimum_words` and `ignore_phrases` | ok |
| `inactivity` nudge and end | ok |
| `max_duration` | ok |
| `provider: local` for listen and speak | ok |
| Pipecat Cloud carrier stream (`transport: cloud-websocket`) | provisional until its credentialed run is recorded (SCHEMA N38). Twilio only for now; the platform terminates other carriers' streams and each needs its own dictated markup. **Nothing is hosted by you**, so the build emits no process and no endpoint |
| carrier WebSocket telephony | provisional for generated Twilio, Telnyx, and Plivo adapters; Exotel is gated pending authenticated WebSocket ingress |
| Daily telephony, Daily's number (`transport: daily-sip`) | provisional until its credentialed run is recorded; needs dial-out enabled on the Daily domain, which `validate` names |
| Daily telephony, your own number (`transport: daily-sip` + `carrier:`) | provisional until its credentialed run is recorded (SCHEMA N37); same dial-out approval, no Daily number needed. Twilio only for now: a second carrier is one forwarding action and one block of instruction text, and the structure for it already ships |
| cold human transfer (`cold:`) | ok on `transport: daily-sip` (both forms) and on `transport: cloud-websocket`; the carrier WebSocket routes have no transfer primitive. The two differ in what happens when the dial ends: Daily keeps the same session and returns to it only on a failed dial, `cloud-websocket` speaks a handback line and brings back a fresh agent whichever way the dial ended |
| telephony call-source variables (caller number, called number, call id, direction) | ok on the carrier WebSocket routes; **refused by name** on `transport: daily-sip` and `transport: cloud-websocket`, because the code that fills them is part of the adapter neither route emits |
| warm human transfer (`warm:`) | not emitted on any Pipecat route, and the refusals say why per route: Daily documents the pattern and this project has not built it (feature 005); the carrier WebSocket transports have no transfer control at all; `cloud-websocket` would need a callback endpoint you host, which is the cost that route exists to remove |

Everything in the [learn pages](../learn/01-one-agent.md), including the guarded handoff, the task, and the task group, runs here. The one hard `fail` is the per-task `model:` override; it sits with the driver gates below.

## What the driver does not emit yet

Some features are in the schema and Pipecat itself supports them, but this first version of the driver does not write them yet. These are **maturity gates on the driver, not limits of Pipecat.** Using one fails the compile today, and the gate lifts when the driver adds it. Right now these are not emitted:

- **Model fallback** (`fallback` on a think model).
- **Per-task `model:`.** Pipecat's mechanism for switching models mid-call stalls the conversation in the current release, so there is nothing safe to emit yet. Drop the override and the task runs on the delegating agent's model.
- **`thinking_audio`.**
- **Voicemail detection** (`on_voicemail`) on carrier WebSocket routes.
- **`mcp` tools.** Use `webhook` or `local` Python-handler tools, which are
  emitted.
- **Warm human transfer** (the `warm:` block), on every route, for two different
  reasons (checked 2026-08-12). On the **carrier WebSocket** routes the platform
  has no transfer control at all, so there is nothing to build against. On the
  **Daily** route Daily does document a warm pattern, and this project has not
  built it yet: the pattern needs the generated bot to own the call's audio,
  through a transfer coordinator, a hold-music mixer, and a gate per leg. That is
  deliberate work rather than a default, tracked as feature 005. Either way warm
  compiles on `(livekit, sip)` today and validation says so by name. The `warm:`
  block you would write already exists and will not change when it lands.
  On **`cloud-websocket`** there is a third reason, and it is a trade rather than
  a gap: acting on how the destination's leg ended needs a callback endpoint you
  host, and hosting nothing is what that route is for. Cold compiles on the Daily
  route (`transport: daily-sip`), where `sip_call_transfer` is the platform's own
  primitive ([TRANSFERS.md](../../TRANSFERS.md)), on both of that route's forms,
  and on `cloud-websocket` by replacing the live call's markup. The Daily carrier
  form will carry warm unchanged when it lands: a carrier call joins the same room
  a Daily-provisioned one joins, so only the supervisor leg's destination composes
  differently.
- **Handoff and task context shaping beyond the defaults:** any `history` other
  than `full`, a subset `context.variables` list rather than `all`, and
  `include_tool_calls: false`. The handoff carries the running context; finer
  shaping is not written yet. The
  [context strategy map](../concepts/how-targets-run-your-agent.md#compare-context-strategies)
  shows the exact task, group, and transfer boundaries.

If you stay within the feature table above, you will not hit any of these. When you do use one, the failure names it, so you are never surprised.

## Warnings and notes

Two different things surface, and it helps to tell them apart.

**Validation warnings** are printed to standard error during `validate` and `compile`. They pass (exit 0) but flag a real behavior difference. For an agent that uses turn preferences, inactivity, and a max duration, Pipecat prints:

```text
warning: pipecat: Pipecat semantic endpointing depends on the bound model
warning: pipecat: Pipecat driver must range-check inactivity durations
warning: pipecat: Pipecat driver must verify a max-duration cap
```

**Notes** are recorded in `compile-report.json` under `notes`, not printed as warnings. They describe how the driver lowered something. For example:

```json
"notes": [
  "interruption ignore_phrases emitted as IGNORE_PHRASES; short phrases are also suppressed by the min-words turn-start strategy",
  "turn role lowers to on-device VAD (Silero); its binding is advisory"
]
```

Read both, then test the exact behavior they point at.

## Running and deploying

**Local, while iterating:**

```sh
unmute dev acme             # talk in the browser
unmute dev acme --console   # talk in the terminal, over your mic and speaker
unmute dev acme --target pipecat --telephony   # answer real phone calls
```

The command selects the only target automatically; with multiple targets, it
prompts in a terminal or requires `--target <name>` in a noninteractive shell.
Browser mode (the default) builds the deployable image and runs it via the
emitted `compose.dev.yaml`, then serves a WebRTC web client against the
container. It needs Docker (Docker Desktop or Docker Engine with the Compose
plugin); this is the same image you deploy, so what you test is what ships.
`--console` runs `uv run --extra console bot.py console` over your local
microphone and speaker, no Docker; on macOS install PortAudio first with
`brew install portaudio`. All modes read shared keys from the current
directory's `.env`, then apply package-root `.env` overrides. Browser logs go
to `build/<target>/dev.log`; add `--verbose` to follow the container logs.
Console mode streams directly to the terminal.

The telephony command runs the provisional route now, cleanly and with no
warning. It builds and runs the emitted Docker Compose graph, waits for the
Pipecat application and Redis health checks, and passes `--bot-port` to Compose
as `UNMUTE_TELEPHONY_PORT`. Redis stores only bounded telephony control records;
audio, transcripts, prompts, task state, and worker handoffs stay in the active
process.

Public ingress is managed for you: without `--public-url`, the command runs a
cloudflared quick tunnel as a child process (install it once; macOS:
`brew install cloudflared`) and supplies `UNMUTE_PUBLIC_URL` itself. On the
Twilio route it then sets the number's voice webhook to the printed inbound
endpoint on every start, prints the previous value, and restores it when you
stop (`--no-webhook` leaves the number untouched). Pass
`--public-url https://your-tunnel.example` to bring your own tunnel instead;
that tunnel must remain running. Telephony logs will go to
`build/<target>/telephony.log`; `--verbose` will follow them in the terminal.

**Compile only, to inspect or deploy a non-telephony project:**

```sh
unmute compile acme --target pipecat
```

For a non-telephony target, `acme/build/pipecat/README.md` shows the quickstart
the project supports directly. A telephony target compiles to the same
directory, but you run it through `unmute dev --telephony` rather than the
`uv run bot.py` quickstart below.

```sh
cp .env.example .env    # fill in your keys
uv run bot.py           # open the URL it prints to talk to the agent
```

For hosting, the project ships a `Dockerfile` and a `pcc-deploy.toml` for Pipecat Cloud, and its own `README.md` has a Deploy section with the exact commands: create the secret set from `.env`, then `pipecat cloud deploy`, which builds that Dockerfile in the cloud. Because the output is an ordinary Python project, you can also run it anywhere you run Python.

## Where to go next

- Build the agent step by step: the [learn pages](../learn/01-one-agent.md), 01 through 06.
- Understand the model split:
  [models and overrides](../concepts/profiles-and-bindings.md).
- Understand why features are gated: [tags and gating](../concepts/tags-and-gating.md).
- The exact per-target contract for every field: `SCHEMA.md` in the repository.

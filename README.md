<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="images/Logo_UNMUTE_wb.svg">
    <img src="images/Logo_UNMUTE.svg" alt="Unmute" height="80">
  </picture>
</p>

<p align="center"><b>One voice agent spec. Compiled to the runtime you pick.</b></p>

<p align="center">
  <a href="https://github.com/slng-ai/unmute/releases"><img src="https://img.shields.io/github/v/release/slng-ai/unmute?label=release" alt="Release"></a>
  <a href="https://github.com/slng-ai/unmute/actions/workflows/ci.yml"><img src="https://github.com/slng-ai/unmute/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/slng-ai/unmute" alt="License"></a>
  <a href="https://unmute.ai"><img src="https://img.shields.io/badge/docs-unmute.ai-8A7300" alt="Documentation"></a>
  <a href="https://discord.gg/kxZactmWj"><img src="https://img.shields.io/badge/Discord-join-5865F2?logo=discord&logoColor=white" alt="Discord"></a>
</p>

Unmute is a command line compiler for voice agents. You write a small package of
YAML and Markdown that says who the agent is, which models it uses, which tools
it can call, what it saves as the call goes on, and how the work is broken into
steps. Unmute turns that into a real Python project for the orchestrator you
picked, and refuses to compile the mistakes you would otherwise find on a live
call.

The project it writes is yours. Pinned dependencies, a Dockerfile, a runbook, and
no dependency on Unmute at runtime. Unmute compiles ahead of time and gets out of
the way. It is never in the call path.

> [!TIP]
> The [Quickstart](https://unmute.ai/start/quickstart) goes from nothing
> to a voice you can talk to in your browser. The full guide lives at
> [unmute.ai](https://unmute.ai).

## Quickstart

```sh
brew install slng-ai/tap/unmute              # macOS
go install github.com/slng-ai/unmute@latest  # anywhere with Go 1.26+
```

```sh
unmute init my-agent      # agent.yaml, instructions.md, targets.yaml, tools/, .env.example
cd my-agent
cp .env.example .env      # then fill in OPENAI_API_KEY and SLNG_API_KEY
unmute validate
unmute dev                # compile, run the container, open the browser
```

Allow the microphone and say hello. The agent speaks first, because `agent.yaml`
says so.

Windows uses Scoop, Linux takes the archive from the
[releases page](https://github.com/slng-ai/unmute/releases), and every release
also carries a signed `checksums.txt` and one SBOM per archive.
[Installation](https://unmute.ai/start/installation) covers all four
ways, plus the one thing running an agent needs: Docker with Compose for the
LiveKit browser loop, or [uv](https://docs.astral.sh/uv/) for Pipecat.

## An agent is a file

This is a whole agent.

```yaml
# agent.yaml
version: 1
name: my-agent
entry_agent: assistant

agents:
  assistant:
    instructions: instructions.md
    think: assistant_model
    speak: assistant_voice
    tools:
      - end_call

secrets:
  - OPENAI_API_KEY
  - SLNG_API_KEY

models:
  think:
    assistant_model:
      provider: openai
      model: gpt-5.6-terra
      params:
        # A reasoning model answering with function tools on chat completions
        # needs this exact value, or OpenAI returns 400 on every turn.
        reasoning_effort: "none"
  speak:
    assistant_voice:
      provider: slng
      model: "deepgram/aura:2"
      voice: "aura-2-thalia-en"
      params:
        # Speech goes through the SLNG gateway nearest your callers.
        world_part: eu-north
  listen:
    transcriber:
      provider: slng
      model: "deepgram/nova:3"
      params:
        world_part: eu-north
  turn:
    detector:
      provider: local
      model: silero

tools:
  - end_call

conversation:
  greeting:
    speaks_first: agent
    text: "Hi, how can I help?"

channels:
  web:
    kind: realtime_audio

capacity:
  peak_sessions: 10
  max_sessions: 20
  avg_session_duration: 5m
```

Three things to notice:

- The prompt is a Markdown file next to it, not a quoted string buried in YAML.
- Every model is named once and then referenced by name. Point `assistant_model`
  at a different model and every agent using it follows.
- Nothing here mentions Pipecat or LiveKit. That choice lives in its own file.

```yaml
# targets.yaml
targets:
  pipecat:
    provider: pipecat
    version: "1.8.0"

  livekit:
    provider: livekit
    version: "1.6.10"
    sdk_language: python
    models:
      detector:
        provider: livekit
        model: turn-detector-mini
```

One agent, two targets, and `unmute compile` writes both projects. Where a
runtime cannot run a model as defined, it overrides that single entry by name,
the way LiveKit does with the turn detector above. The agent itself does not
change.

## Three targets

| Target | Kind | What you get |
|---|---|---|
| [LiveKit Agents](https://unmute.ai/targets/livekit) | code | `build/livekit/agent.py`, a Python project you host and run |
| [Pipecat](https://unmute.ai/targets/pipecat) | code | `build/pipecat/bot.py`, plus a `pcc-deploy.toml` for Pipecat Cloud |
| [SLNG](https://unmute.ai/targets/slng) | hosted | `unmute deploy` pushes a deployment body and SLNG runs the agent |

`pipecat`, `livekit` and `slng` are the only values `provider` accepts. A code
target gives you something to host. The hosted target has nothing to host, and
no `unmute dev`.

## What compile writes

One directory per target, under `build/`.

```
build/livekit/
├── agent.py             # the agent
├── tools/               # your local Python handlers, copied
├── dev_metrics.py       # per-turn timings, read by unmute dev
├── pyproject.toml       # pinned dependencies
├── Dockerfile
├── .dockerignore
├── compose.dev.yaml
├── .env.example         # exactly the variables you supply
├── README.md            # the runbook for this build
└── compile-report.json
```

Pipecat gets the same shape with `bot.py` as its entry point. Neither project
imports Unmute.

Treat `build/` as output. Edit the package and compile again. Anything you change
inside `build/` is overwritten on the next compile.

## What else the file holds

The agent at the top of this page is the floor. Everything below is written in
the same `agent.yaml`, next to what you have already read, and every piece of it
is checked before a call happens rather than during one. The snippets are from
[`customer-intake`](examples/customer-intake/) and
[`salon-concierge`](examples/salon-concierge/).

### Values with a type

A variable says what it holds. The model is told the format, and the value is
checked where it enters, not where it is used.

```yaml
shapes:
  - name: CustomerRecord
    description: The record the intake desk opened for this caller.
    fields:
      - record_id: Id
      - opened_on: Date
      - enquiry: Literal["new_customer", "existing_customer", "complaint", "other"]

variables:
  contact:
    type: NameEmail
    description: >-
      Who the caller is and where their confirmation goes, the name and the
      email address held as two separate parts.

  caller_email:
    type: EmailStr
    default: ""
    description: The caller's email address on its own, with no name around it.

  callback_time:
    type: Time | None
    description: >-
      A good time of day to ring the caller back, on the 24 hour clock. Leave it
      out when the caller has not named one.

  notes:
    type: list[str]
    description: >-
      Anything the caller added that no other field holds, one short entry per
      thing they said.
```

`Phone`, `Date`, `Time`, `Id` and `EmailStr` are text with a checked shape.
`NameEmail` holds a name and an address as two separate parts, so a prompt can
use the name without reading the address out loud. You also have `str`, `int`,
`float`, `bool`, `Literal[...]`, `list[...]`, `| None`, and any shape you
declare yourself.

An address the model misheard is refused with the format, inside the same turn,
so the model fixes it rather than saving something wrong. The email check never
asks DNS: this runs while the caller is on the line, and a slow resolver would
hold them in silence.

A step takes one part of a result with a dotted path, and `+` appends instead of
replacing.

```yaml
assign:
  - contact: result.contact
  - caller_email: result.contact.email
  - notes+: result.note
```

### A step that ends on its own tool

One task out of an agent's `tasks:` list.

```yaml
- name: manage_booking
  instructions: tasks/booking.md
  tools:
    - create_booking
    - modify_booking
    - cancel_booking
  finish:
    - tool: create_booking
      success:
        - status: booked
    - tool: modify_booking
      success:
        - status: modified
    - tool: cancel_booking
      success:
        - status: cancelled
  assign:
    - appointment: result.appointment
  context:
    history: messages
```

`finish:` names the tool calls that end the step, and the result value that
counts as success. The booking goes through and the step is over. No extra model
call to announce it, no second sentence, and none of those tools runs again in
that step, because a second booking that ran and was then refused is still a
second booking. The caller hears the owning agent reply once.

The success value has to be one the tool's own output declares in an `enum:`. A
field that cannot hold it is refused when you compile, with the file and the
line.

### A group that skips work already done

```yaml
task_groups:
  book:
    when: >-
      The caller wants to create, move or cancel a booking. Includes a change to
      an appointment just made.
    steps:
      - task: verify_customer
        skip_when_confirmed: customer_phone
      - manage_booking
    context_scope: shared
    then: return
```

Verification runs the first time and is skipped once the number is agreed.
`skip_when_confirmed:` has to name a value that this same step confirms, so the
two cannot drift apart. Entering the step again withdraws what it confirmed,
which is what a caller correcting their number needs.

### Facts in the prompt before the caller speaks

```yaml
prefetch:
  - name: today
    clock: now
    timezone: Europe/Madrid
    assign:
      - today_date: result.date

  - name: caller
    source: from_number
    assign:
      - caller_phone: result.value
```

Each entry runs before the greeting, on its own budget, and cannot raise. When
one skips, the variable keeps its default and the step that asks runs instead.
The zone sits on the entry, because a container's clock is UTC and would name
the wrong day for everybody who is not on it.

### A value the model never retypes

```yaml
# tools/create_customer_record.yaml
input:
  type: object
  properties:
    summary:
      type: string
      description: One sentence in the caller's own words saying what they rang about.
  required:
    - summary

inject:
  - phone: "{{caller_phone}}"
  - email: "{{caller_email}}"
  - name: "{{contact.name}}"
```

The model fills one argument. The other three come straight out of saved state
and are hidden from it, so a digit cannot change between the turn it was agreed
on and the turn it was written down.

A variable can also carry `confirm: verify_contact`. Until that step hears a
yes, the value renders in no prompt but that step's own, and any tool injecting
it refuses itself and names the step to run first.

### Speech through the gateway nearest your callers

```yaml
models:
  speak:
    voice:
      provider: slng
      model: "deepgram/aura:2"
      voice: "aura-2-thalia-en"
      params:
        world_part: eu-north
  listen:
    transcriber:
      provider: slng
      model: "deepgram/nova:3"
      params:
        world_part: eu-north
```

`world_part` picks one of 13 SLNG speech gateways, on each `listen` and `speak`
model separately. It becomes the host `eu-north.api.slng.ai` in the generated
project: `slng_base_url=` on LiveKit, `base_url=` on Pipecat.

The 13 are `us-east`, `us-west`, `br`, `eu-west`, `eu-north`, `gb`, `za`, `il`,
`jp`, `sg`, `id`, `in` and `au`. Leave `world_part` out and the existing default
URL stands. Reasoning through the SLNG Context Router picks its own region with
`params.world_part_override`, which has a smaller set of its own, so the two
settings are never confused for one another. Where the worker itself runs is a
third choice, `deployment_region` in `targets.yaml`, and none of the three has
to match the others: `salon-concierge` deploys to `eu-central` and speaks
through `eu-north`.

### Turn taking you can hear

```yaml
models:
  turn:
    detector:
      provider: local
      model: silero
      pace: patient
```

`pace:` is `snappy`, `balanced` or `patient`. It sets how long the agent waits
after the caller stops talking. A desk that asks people to spell an email
address out loud wants `patient`, because they pause between the characters.

## What validate catches

`unmute validate` reads the package the way the compiler does, and names the
file, the line and often the column when something is wrong. It is offline and
needs no account. A few of the things it refuses:

- a `finish:` whose success value the tool's own output cannot hold
- a `skip_when_confirmed:` naming a value that step does not confirm
- a pre-fetch entry reading a value only a later entry assigns, naming both and
  which one to move
- a `type:` outside the grammar, with the column inside the expression
- a prompt placeholder naming a value the package does not declare, or a greeting
  naming one that is not settled before the first word
- a `world_part` that is not a gateway, listing the ones that are
- a feature the chosen target cannot run, naming what to write instead

Warnings are the other half. A credential a package uses but never declares in
`secrets:`, an agent holding every tool of its own step so the step's `assign:`
never runs, a turn field that reaches nothing: each one names the thing to
change, goes to standard error, and still exits 0.

## What goes in a package

Each of these has a page in the guide.

| | Where it is taught |
|---|---|
| **Tools** that call a webhook, run local Python, reach an MCP server, use one the runtime already has, or search your own documents | [Tools](https://unmute.ai/build/tools/overview) |
| **Tasks, task groups and handoffs**, for when one prompt stops being enough, and the steps that end on their own tool | [Orchestration](https://unmute.ai/build/orchestration/overview) |
| **Typed values**, declared once and checked where they enter: phone numbers, dates, email addresses, a closed set of words, lists, and shapes of your own | [Variables](https://unmute.ai/build/variables) |
| **Escalation to a person**, cold or warm depending on the phone route | [Transfers](https://unmute.ai/transfers/overview) |
| **Phone calls**, inbound and outbound, through Twilio, SIP trunks or a carrier stream | [Phone calls](https://unmute.ai/telephony/overview) |
| **Pre-fetch**, so a known fact is in the prompt before the caller finishes the first sentence | [Pre-fetch](https://unmute.ai/build/prefetch) |
| **Tracing** to Langfuse or Coval, with per-turn latency and tool calls | [Tracing](https://unmute.ai/tracing/overview) |
| **Turn taking, the context router, and regional infrastructure** | [Optimization](https://unmute.ai/optimization/overview) |
| **Secrets**, so a credential never sits in a package file | [Secrets](https://unmute.ai/reference/secrets) |
| **What to declare, and what to leave out**, once a package has more than a handful of values | [State design](https://unmute.ai/best-practices/state-design) |

Every key of every package file is listed under
[configuration](https://unmute.ai/reference/agent-yaml).

## Commands

| Command | What it does |
|---|---|
| [`init`](https://unmute.ai/reference/cli/init) | scaffold a new package, or open an interactive console with no name |
| [`validate`](https://unmute.ai/reference/cli/validate) | check a package against its targets, naming the file and the line when a field is wrong |
| [`compile`](https://unmute.ai/reference/cli/compile) | write the generated project for every target |
| [`dev`](https://unmute.ai/reference/cli/dev) | compile, run locally, and talk to the agent in your browser |
| [`deploy`](https://unmute.ai/reference/cli/deploy) | validate, compile and push a package to SLNG |
| [`pull`](https://unmute.ai/reference/cli/pull) | fetch each SLNG-hosted tool's definition into the package |
| [`resources`](https://unmute.ai/reference/cli/resources) | list the tools, MCP servers and phone numbers your SLNG organisation offers |
| [`skill`](https://unmute.ai/reference/cli/skill) | install the Unmute skill so a coding assistant can build packages |

`validate`, `compile`, `dev`, `deploy` and `pull` take the package directory as
an optional argument. From inside the package you run them bare; from anywhere
else you pass the path, as in `unmute dev my-agent`.

`validate` and `compile` work offline. SLNG deployment resolves hosted tools
by name with `unmute deploy`: no hash, mirror or `pull` step is needed. Only
LiveKit and Pipecat need `unmute pull` to fetch the hosted tools they run
themselves. `deploy`, `pull` and `resources` use your SLNG account.

Warnings go to standard error and still exit 0. Errors exit 1.

## Coding agents

`unmute skill install` writes the Unmute skill into your repository, so Claude
Code, Cursor, Codex and the rest know how to author a package. The skill ships
inside the binary, so nothing is downloaded, and the files travel with the repo
so a team shares one skill. The docs are also readable as
[`llms.txt`](https://unmute.ai/llms.txt) and
[`llms-full.txt`](https://unmute.ai/llms-full.txt), or one page at a
time by adding `.md` to its URL. See
[Coding agents](https://unmute.ai/start/coding-agents).

## Examples

[`examples/`](examples/) holds four packages.

- [`customer-intake`](examples/customer-intake/) is the small one: one agent,
  three tasks, one local tool. It takes a caller's number, name, email address,
  enquiry and callback time, saves each under its own type, and hands them to a
  tool the model cannot type over. Every type in the grammar appears once. Start
  here if the question is about types, saved values or injection.
- [`salon-concierge`](examples/salon-concierge/) is the big one: two agents, four
  tasks, a task group that skips verification once the number is agreed, three
  steps that end on their own tool with `finish:`, handoffs in both directions, a
  cold manager transfer, tracing, and inbound phone on both code targets. Every
  tool is local Python, so nothing remote has to be up before the greeting.
- [`salon-concierge-single-prompt`](examples/salon-concierge-single-prompt/) is
  the same salon with the structural features taken back out, so the one above
  can be read against something. Model, transport and turn taking are held
  identical, so a difference you hear is a difference the structure made.
- [`hotel-concierge`](examples/hotel-concierge/) is the hosted target's
  showcase: a concierge line whose tools are all references SLNG already holds,
  with template variables, an injected argument, a tool announcement, named MCP
  tools and a fallback. It produces no runnable project:
  `unmute deploy` compiles a deployment body and pushes it.

If you want a package to start from rather than one to read, run
`unmute init my-agent`.

## Develop

```sh
make install  # builds and puts unmute on your PATH
make test     # go test -race ./... , pure Go, no Python needed
make lint
make fmt
```

`make smoke` proves the emitted Python is valid and needs Python installed, so it
is opt-in and never the pull request gate. `make contracts` re-fetches the
published SLNG conformance fixtures and needs network.

## Contributing

Contributions are welcome, from anyone. Unmute is MIT licensed and every part of
it is open: the compiler, the three targets, the examples, the skill and the
docs site.

A pull request needs five things:

1. **An issue, opened before you write the code.** Search the
   [open issues](https://github.com/slng-ai/unmute/issues) first, then open a
   bug report, an improvement or a feature request. Link it from the pull
   request.
2. **An example package that uses your feature.** Shaped like the ones in
   [`examples/`](examples/), with your new key in its authored files and the
   code path running on a real call, so we can compile it, dial it and hear it.
3. **A video** of that example working.
4. **A README** for that example, saying which part of it is your feature.
5. **Updated docs** under [`docs-site/`](docs-site/) that explain how the
   feature works, not only what the key is called.

[CONTRIBUTING.md](CONTRIBUTING.md) is the whole thing: what the example has to
exercise, where the package goes, what a public example has to pass, and every
check CI runs with its local command.

Say hello on [Discord](https://discord.gg/kxZactmWj) before you start something
large. It is the fastest way to find out whether somebody is already on it.

## Resources

- [Documentation](https://unmute.ai) covers everything above in order.
- [How Unmute works](https://unmute.ai/start/how-unmute-works) is the
  four compiler stages between your package and the generated project.
- [Configuration reference](https://unmute.ai/reference/agent-yaml) is
  every key of every package file.
- [Changelog](https://unmute.ai/changelog) says what changed in each
  release.
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) explains the design and points
  at the load-bearing code.
- [Issues](https://github.com/slng-ai/unmute/issues) for bugs and requests.
- [Discord](https://discord.gg/kxZactmWj) for questions, and for showing what
  you built.
- [Contributing](CONTRIBUTING.md) for how to send a change.

Unmute is MIT licensed. See [LICENSE](LICENSE).

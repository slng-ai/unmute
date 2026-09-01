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
  <a href="https://unmute.mintlify.app"><img src="https://img.shields.io/badge/docs-unmute.mintlify.app-8A7300" alt="Documentation"></a>
</p>

Unmute is a command line compiler for voice agents. You write a small package of
YAML and Markdown that says who the agent is, which models it uses, and which
tools it can call. Unmute turns that into a real Python project for the
orchestrator you picked.

The project it writes is yours. Pinned dependencies, a Dockerfile, a runbook, and
no dependency on Unmute at runtime. Unmute compiles ahead of time and gets out of
the way. It is never in the call path.

> [!TIP]
> The [Quickstart](https://unmute.mintlify.app/start/quickstart) goes from nothing
> to a voice you can talk to in your browser. The full guide lives at
> [unmute.mintlify.app](https://unmute.mintlify.app).

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
[Installation](https://unmute.mintlify.app/start/installation) covers all four
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
    model: assistant_model
    voice: assistant_voice
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
  listen:
    transcriber:
      provider: slng
      model: "deepgram/nova:3"
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
| [LiveKit Agents](https://unmute.mintlify.app/targets/livekit) | code | `build/livekit/agent.py`, a Python project you host and run |
| [Pipecat](https://unmute.mintlify.app/targets/pipecat) | code | `build/pipecat/bot.py`, plus a `pcc-deploy.toml` for Pipecat Cloud |
| [SLNG](https://unmute.mintlify.app/targets/slng) | hosted | `unmute deploy` pushes a deployment body and SLNG runs the agent |

`pipecat`, `livekit` and `slng` are the only values `provider` accepts. A code
target gives you something to host. The hosted target has nothing to host, and
no `unmute dev`.

## What compile writes

One directory per target, under `build/`.

```
build/livekit/
├── agent.py             # the agent
├── tools/               # your local Python handlers, copied
├── pyproject.toml       # pinned dependencies
├── Dockerfile
├── compose.dev.yaml
├── .env.example         # exactly the variables you supply
├── README.md            # the runbook for this build
└── compile-report.json
```

Pipecat gets the same shape with `bot.py` as its entry point. Neither project
imports Unmute.

Treat `build/` as output. Edit the package and compile again. Anything you change
inside `build/` is overwritten on the next compile.

## What goes in a package

| | Where it is taught |
|---|---|
| **Tools** that call a webhook, run local Python, reach an MCP server, use one the runtime already has, or search your own documents | [Tools](https://unmute.mintlify.app/build/tools/overview) |
| **Handoffs, tasks and task groups**, for when one prompt stops being enough | [Orchestration](https://unmute.mintlify.app/build/orchestration/overview) |
| **Escalation to a person**, cold or warm depending on the phone route | [Transfers](https://unmute.mintlify.app/transfers/overview) |
| **Phone calls**, inbound and outbound, through Twilio, SIP trunks or a carrier stream | [Phone calls](https://unmute.mintlify.app/telephony/overview) |
| **Pre-fetch**, so a known fact is in the prompt before the caller finishes the first sentence | [Pre-fetch](https://unmute.mintlify.app/build/prefetch) |
| **Tracing** to Langfuse or Coval, with per-turn latency and tool calls | [Tracing](https://unmute.mintlify.app/tracing/overview) |
| **Turn taking, the context router, and regional compute** | [Optimization](https://unmute.mintlify.app/optimization/overview) |
| **Variables and secrets**, so a value never sits in a package file | [Variables](https://unmute.mintlify.app/reference/variables) |

Every key of every package file is listed under
[configuration](https://unmute.mintlify.app/reference/agent-yaml).

## Commands

| Command | What it does |
|---|---|
| [`init`](https://unmute.mintlify.app/reference/cli/init) | scaffold a new package, or open an interactive console with no name |
| [`validate`](https://unmute.mintlify.app/reference/cli/validate) | check a package against its targets, naming the file and the line when a field is wrong |
| [`compile`](https://unmute.mintlify.app/reference/cli/compile) | write the generated project for every target |
| [`dev`](https://unmute.mintlify.app/reference/cli/dev) | compile, run locally, and talk to the agent in your browser |
| [`deploy`](https://unmute.mintlify.app/reference/cli/deploy) | validate, compile and push a package to SLNG |
| [`resources`](https://unmute.mintlify.app/reference/cli/resources) | list the tools, MCP servers and phone numbers your SLNG organisation offers |
| [`skill`](https://unmute.mintlify.app/reference/cli/skill) | install the Unmute skill so a coding assistant can build packages |

`validate`, `compile`, `dev` and `deploy` take the package directory as an
optional argument. From inside the package you run them bare; from anywhere else
you pass the path, as in `unmute dev my-agent`.

Warnings go to standard error and still exit 0. Errors exit 1.

## Coding agents

`unmute skill install` writes the Unmute skill into your repository, so Claude
Code, Cursor, Codex and the rest know how to author a package. The skill ships
inside the binary, so nothing is downloaded, and the files travel with the repo
so a team shares one skill. The docs are also readable as
[`llms.txt`](https://unmute.mintlify.app/llms.txt) and
[`llms-full.txt`](https://unmute.mintlify.app/llms-full.txt), or one page at a
time by adding `.md` to its URL. See
[Coding agents](https://unmute.mintlify.app/start/coding-agents).

## Examples

[`examples/`](examples/) holds two packages.

- [`salon-concierge`](examples/salon-concierge/) is the one to read: two agents,
  two tasks, handoffs, a guarded delegate, a cold manager transfer, tracing, and
  inbound phone on both code targets. Every tool is local Python, so nothing
  remote has to be up before the greeting.
- [`slng-support`](examples/slng-support/) is the hosted target in its smallest
  form. It produces no runnable project: `unmute deploy` compiles a deployment
  body and pushes it.

If you want a package to start from rather than one to read, run
`unmute init my-agent`.

## Develop

```sh
make build    # writes bin/unmute
make test     # go test -race ./... , pure Go, no Python needed
make lint
make fmt
```

`make smoke` proves the emitted Python is valid and needs Python installed, so it
is opt-in and never the pull request gate. `make contracts` re-fetches the
published SLNG conformance fixtures and needs network.

## Resources

- [Documentation](https://unmute.mintlify.app) covers everything above in order.
- [How Unmute works](https://unmute.mintlify.app/start/how-unmute-works) is the
  four compiler stages between your package and the generated project.
- [Configuration reference](https://unmute.mintlify.app/reference/agent-yaml) is
  every key of every package file.
- [Changelog](https://unmute.mintlify.app/changelog) says what changed in each
  release.
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) explains the design and points
  at the load-bearing code.
- [Issues](https://github.com/slng-ai/unmute/issues) for bugs and requests.

Unmute is MIT licensed. See [LICENSE](LICENSE).

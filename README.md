<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="images/Logo_UNMUTE_wb.svg">
    <img src="images/Logo_UNMUTE.svg" alt="Unmute" height="80">
  </picture>
</p>

Unmute is a command line compiler for voice agents. You write a small package of
YAML and Markdown that says who the agent is, which models it uses, and which
tools it can call. Unmute turns that into a real Python project for the
orchestrator you picked.

The project it writes is yours. Pinned dependencies, a Dockerfile, a runbook, and
no dependency on Unmute at runtime. Unmute compiles ahead of time and gets out of
the way. It is never in the call path.

## An agent is a file

This is a whole agent.

```yaml
# agent.yaml
version: 1
entry_agent: assistant

models:
  think:
    brain:
      provider: openai
      model: gpt-5.6-terra
      params:
        # A reasoning model answering with function tools on chat completions
        # needs this exact value, or OpenAI returns 400 on every turn.
        reasoning_effort: "none"
  speak:
    voice:
      provider: slng
      model: "deepgram/aura:2"
      voice: "aura-2-thalia-en"
  listen:
    ears:
      provider: slng
      model: "deepgram/nova:3"
  turn:
    detector:
      provider: local
      model: silero

agents:
  assistant:
    instructions: instructions.md
    model: brain
    voice: voice

conversation:
  greeting:
    speaks_first: agent
    text: "Hi, how can I help?"

channels:
  web:
    kind: realtime_audio

capacity:
  peak_sessions: 2
  max_sessions: 4
  avg_session_duration: 3m
```

Three things to notice:

- The prompt is a Markdown file next to it, not a quoted string buried in YAML.
- Every model is named once and then referenced by name. Point `brain` at a
  different model and every agent using it follows.
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

## Install

Unmute is one static binary. Four ways to get it, in order of how little you
have to know:

```sh
brew install slng-ai/tap/unmute              # macOS
go install github.com/slng-ai/unmute@latest  # anywhere with Go 1.26+
```

On Windows, Scoop is the way that works today. Add the bucket once, then
install:

```powershell
scoop bucket add slng-ai https://github.com/slng-ai/scoop-bucket
scoop install slng-ai/unmute
```

Or take the archive for your platform from the
[releases page](https://github.com/slng-ai/unmute/releases): darwin, linux and
windows, on amd64 and arm64. Each archive holds the binary, the LICENSE and this
README, and the release also carries a signed `checksums.txt` and one SBOM per
archive.

Homebrew is macOS only, because Homebrew on Linux has no casks. On Linux, use
`go install` or the archive.

`brew`, Scoop, `go install` and the archive all work today. `winget` is coming
soon: every release submits its manifest to Microsoft's package repository, and
`winget install slng.unmute` starts finding the package once Microsoft merges
the first submission. Until then, Windows users take Scoop.

Building from a clone is the contributor path, and needs Go 1.26 or newer:

```sh
git clone https://github.com/slng-ai/unmute.git
cd unmute
make build       # writes bin/unmute
make install     # puts unmute on your Go bin path
```

Either way, the binary says what it is:

```sh
unmute --version
# unmute version 1.2.0 (3a9f2c1 2026-08-14T10:11:12Z)
```

Running an agent also needs Docker with Compose for the browser, or
[uv](https://docs.astral.sh/uv/) for the terminal.

## Use it

```sh
unmute init my-agent      # agent.yaml, instructions.md, targets.yaml, tools/, .env.example
cd my-agent
cp .env.example .env      # then fill in OPENAI_API_KEY and SLNG_API_KEY
unmute validate
unmute dev
```

`validate`, `compile` and `dev` take the package directory as an optional
argument. From inside the package you run them bare; from anywhere else you pass
the path, as in `unmute dev my-agent`.

The scaffold targets LiveKit, but `.env.example` lists only the values you
supply: `OPENAI_API_KEY` and `SLNG_API_KEY`. `unmute dev` supplies
`LIVEKIT_URL`, `LIVEKIT_API_KEY`, and `LIVEKIT_API_SECRET` for a local run;
LiveKit Cloud or a self-hosted operator supplies them at deploy time.
`.env.local` is also supported for local development and overrides `.env` in
the same directory.

`unmute dev` compiles the package, builds the container a deployment would run,
starts it, and opens a browser page. Allow the microphone and say hello. The
agent speaks first, because `agent.yaml` says so.

| Command | What it does |
|---|---|
| `init` | scaffold a new package |
| `validate` | check a package against its targets, naming the file and the line when a field is wrong |
| `compile` | write the generated project for every target |
| `dev` | compile, run locally, and talk to the agent |

Add `--telephony` to `dev` and a real phone call reaches the same agent. Both
modes run the agent in Docker Compose, so local development needs Docker.

## What compile writes

One directory per target, under `build/`.

```
build/livekit/
├── agent.py             # the agent
├── pyproject.toml       # pinned dependencies
├── Dockerfile
├── compose.dev.yaml
├── .env.example         # exactly the variables you supply
├── README.md            # the runbook for this build
└── compile-report.json
```

That is what the scaffold above compiles to. Pipecat gets the same shape with
`bot.py` as its entry point, plus a `pcc-deploy.toml` for Pipecat Cloud. Local
Python tool handlers are copied in beside the entry point on both.

Treat `build/` as output. Edit the package and compile again. Anything you change
inside `build/` is overwritten on the next compile.

## Examples

[`examples/`](examples/) holds runnable packages, and each one compiles for both
targets. [`salon-support`](examples/salon-support/) is the quickest to try, since
it needs nothing but a browser and two keys.
[`twilio-telephony-hello`](examples/twilio-telephony-hello/) places a real phone
call.

## Develop

```sh
make test     # go test -race ./... , pure Go, no Python needed
make lint
make fmt
```

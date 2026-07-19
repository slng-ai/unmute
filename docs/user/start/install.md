# Install

Unmute is a single command-line tool called `unmute`. It is one small binary with no runtime dependencies. This page gets it onto your machine and checks that it works.

## Build the binary

The tool is written in Go, so you build it from the source with the project's `Makefile`:

```sh
make build
```

That writes the binary to `bin/unmute`. Check it:

```sh
bin/unmute --version
bin/unmute --help
```

`--help` lists every command:

```text
init      Scaffold a new v1 agent package.
validate  Validate a v1 agent package against its targets.
compile   Compile a v1 agent package to its resolved target artifacts.
apply     Apply a v1 package to its managed (config-plane) targets.
dev       Compile, run the agent locally, and talk in a browser or terminal.
```

To put `unmute` on your `PATH` so you can run it from anywhere:

```sh
make install
```

If you prefer to build directly without the Makefile, this is the exact command it runs:

```sh
CGO_ENABLED=0 go build -o bin/unmute .
```

## Install tools for local runs

Building and compiling need only `unmute`. The `unmute dev` command runs the
generated Python project with **uv**, a Python package manager.

Install uv by following the instructions at [docs.astral.sh/uv](https://docs.astral.sh/uv/). Then check it:

```sh
uv --version
```

If `uv` isn't installed, `unmute dev` stops with an installation link.
Everything else works without uv.

Browser and console modes have these additional requirements:

- LiveKit browser mode needs a LiveKit server. Set `LIVEKIT_URL`,
  `LIVEKIT_API_KEY`, and `LIVEKIT_API_SECRET` for an existing server, or install
  `livekit-server` so `unmute dev` can start it locally.
- Pipecat console mode needs PortAudio. On macOS, install it with
  `brew install portaudio`; uv installs the Python `pyaudio` package through
  the generated project's `console` extra.

## What you do not need

You don't install Pipecat, Python packages, or platform SDKs by hand. When you
run your agent, `unmute` generates the Python project, and uv installs its
pinned dependencies on the first run.

## Next

You are ready. Build [your first agent](first-agent.md).

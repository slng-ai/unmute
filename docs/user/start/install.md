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

Building and compiling need only `unmute`.

The default `unmute dev` (browser) and `unmute dev --telephony` run the
generated project in containers, so they need **Docker**: Docker Desktop or
Docker Engine with the Compose plugin. `unmute dev` builds the same image you
would deploy and runs it locally. If Docker or the Compose plugin is missing,
`unmute dev` stops with an install message before doing any work.

`unmute dev --console` is the no-Docker path. It runs the generated Python
project with **uv**, a Python package manager, over your terminal's mic and
speaker. Install uv from [docs.astral.sh/uv](https://docs.astral.sh/uv/), then
check it:

```sh
uv --version
```

Compiling a code target (Pipecat, LiveKit) also runs `ruff format` over the
generated Python so it lands cleanly formatted. `ruff` is optional: without it,
`unmute` writes the (still valid) code unformatted and prints a one-line
warning. Install it from [docs.astral.sh/ruff](https://docs.astral.sh/ruff/).

Console mode has one extra requirement:

- Pipecat console mode needs PortAudio. On macOS, install it with
  `brew install portaudio`; uv installs the Python `pyaudio` package through
  the generated project's `console` extra.

## What you do not need

You don't install Pipecat, Python packages, or platform SDKs by hand. When you
run your agent, `unmute` generates the Python project; the container build
installs its pinned dependencies for browser mode, and uv does the same for
`--console`.

## Next

You are ready. Build [your first agent](first-agent.md).

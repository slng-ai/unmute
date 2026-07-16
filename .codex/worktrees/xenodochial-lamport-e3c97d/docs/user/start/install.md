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
dev       Compile, run the agent locally, and talk to it in the browser.
```

To put `unmute` on your `PATH` so you can run it from anywhere:

```sh
make install
```

If you prefer to build directly without the Makefile, this is the exact command it runs:

```sh
CGO_ENABLED=0 go build -o bin/unmute .
```

## One extra tool for running agents locally

Building and compiling need only `unmute`. But `unmute dev`, the command that runs your agent so you can talk to it, starts a Python project under the hood. To run that project it uses **uv**, a fast Python package manager.

Install uv by following the instructions at [docs.astral.sh/uv](https://docs.astral.sh/uv/). Then check it:

```sh
uv --version
```

If `uv` is not installed, `unmute dev` stops with a clear message telling you to install it. Everything else works without uv.

## What you do not need

You do not install Pipecat, Python packages, or any platform SDK by hand. When you run your agent, `unmute` generates the Python project and uv installs the exact pinned dependencies on first run. You never manage those yourself.

## Next

You are ready. Build [your first agent](first-agent.md).

# Voice agent test packages

Whole packages we compile, deploy and talk to. Not shipped, not examples, and
nobody is pointed at one as a starting shape.

Three places in this tree hold a package, and they are for different things:

| Directory | What it is for | Held to |
|---|---|---|
| `examples/` | packages a reader is sent to, and the shapes the docs describe | every example gate: a README naming its transports, resolving links, the model and framework pins |
| `internal/testdata/` | the smallest package that makes one unit assertion possible | validates and builds |
| `internal/voice-agents-tests/` | a whole agent we run against real providers, to find what only a real call finds | validates clean and generates on every target it declares |

The bar for this directory is in `internal/generate/examples_test.go`:
`TestVoiceAgentTestPackagesValidateAndGenerate` loads every subdirectory holding
an `agent.yaml`, validates it against every target it declares with zero errors,
and generates each one. A package here that stops compiling fails the default
suite, because a test agent that does not run is not a test.

Everything else about a package here is deliberately unheld. The prompts, the
tools, the models and the routes are free to differ from anything in
`examples/`, because the point of a package here is to be the thing worth
finding out about.

## What is here

| Package | What it is for |
|---|---|
| [`salon-concierge-v2`](salon-concierge-v2/) | the same salon as `examples/salon-concierge` with each step's context chosen per step and one more value carried as a declared variable, so the two can be run against each other on a real call |

## Running one

Same commands as any package. `build/` under here is ignored, like every other
compiled project.

```sh
unmute validate internal/voice-agents-tests/salon-concierge-v2
unmute compile internal/voice-agents-tests/salon-concierge-v2
unmute dev internal/voice-agents-tests/salon-concierge-v2 --target pipecat
```

Each package's own `README.md` says what it is for and what it needs.

# Voice agent test packages

Whole packages we compile, deploy and talk to. Not shipped, not examples, and
nobody is pointed at one as a starting shape.

Three places in this tree hold a package, and they are for different things.
`examples/` holds the packages a reader is sent to and the shapes the docs
describe, so each one carries every example gate: a README naming its
transports, resolving links, and the model and framework pins.
`internal/testdata/` holds the smallest package that makes one unit assertion
possible, and it only has to validate and build. This directory holds a whole
agent we run against real providers, to find what only a real call finds.

The bar here is in `internal/generate/examples_test.go`:
`TestVoiceAgentTestPackagesValidateAndGenerate` loads every subdirectory holding
an `agent.yaml`, validates it against every target it declares with zero errors,
and generates each one. A package here that stops compiling fails the default
suite, because a test agent that does not run is not a test.

Everything else about a package here is deliberately unheld. The prompts, the
tools, the models and the routes are free to differ from anything in
`examples/`, because the point of a package here is to be the thing worth
finding out about.

## What is here

[`salon-concierge-v2`](salon-concierge-v2/) is the same salon as
`examples/salon-concierge` with each step's context chosen per step and one more
value carried as a declared variable, so the two can be run against each other
on a real call.

## Running one

Same commands as any package. `build/` under here is ignored, like every other
compiled project.

```sh
unmute validate internal/voice-agents-tests/salon-concierge-v2
unmute compile internal/voice-agents-tests/salon-concierge-v2
unmute dev internal/voice-agents-tests/salon-concierge-v2 --target pipecat
```

Each package's own `README.md` says what it is for and what it needs.

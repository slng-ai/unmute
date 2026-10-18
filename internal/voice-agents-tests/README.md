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

[`slng-tasks`](slng-tasks/) is the slng target's task acceptance package:
two tasks, a group with a skipped step, typed values, and a hosted code tool
only a task can call, with that tool's Python source beside it. Its README is
the test plan for tasks on SLNG managed agents.

[`slng-live`](slng-live/) is the slng target's live package: Python tools
deploy publishes as code tools, an API request tool, `end_call`, a group, and a
step that one tool ends with a `then: done` outcome. `make e2e-slng` deploys it and calls it.

[`relay-desk`](relay-desk/) is the Twilio ConversationRelay acceptance
package: one agent, one inbound phone channel, one read-only tool and
`end_call`, compiled twice, once thinking on OpenAI and once on Gemini through
Vertex AI in the EU.

## Running one

Same commands as any package. `build/` under here is ignored, like every other
compiled project.

```sh
unmute validate internal/voice-agents-tests/kestrel
unmute compile internal/voice-agents-tests/kestrel
make e2e PKG=kestrel FW=pipecat
```

Each package's own `README.md` says what it is for and what it needs.

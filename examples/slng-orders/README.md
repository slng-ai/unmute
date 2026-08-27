# slng-orders

An order-status agent hosted by SLNG, with one tool of its own.

This is the second `slng` example, and the difference from
[`slng-support`](../slng-support) is the whole reason it exists. `slng-support`
references only builtins, so its push creates nothing: SLNG already owns every
capability it names. This package ships a `local:` tool, so the push has real
work to do.

```bash
export SLNG_API_KEY=...
bin/unmute deploy examples/slng-orders --run-samples
```

```text
✓ slng (slng)
slng: credential from SLNG_API_KEY
slng: compiled examples/slng-orders/build/slng (3 files)
slng: organisation Your Workspace (550fffde-…)
slng: tool check_order created, sample succeeded, published v1
slng: agent created 428006b1-19cf-499f-be00-e12b8dbfad14
slng: version 1 labelled "slng-orders @ 2026-08-27T15:08:39.184Z"
slng: deployed. Talk to it: voiceai agents web-sessions create 428006b1-… --file session.json
```

`--run-samples` is not optional here, and the next section is why.

The walkthrough with every step and every failure message is
[Deploy to SLNG](https://unmute.mintlify.app/deploy/slng). This file is the
package's own notes.

## The tool lives in SLNG, not in this repository

On pipecat or livekit a `local:` tool is a Python function inside the project you
host, and it runs in your process. Here there is no process of yours: the handler
in `tools/check_order.py` is **uploaded**, and it becomes an org-level object in
SLNG with a version number. This package names it; SLNG owns it.

That is not just the necessary shape, it is the fast one. A tool sits on the
critical path of a turn — the caller stops speaking, the model calls the tool, and
nobody speaks until it returns. A handler inside SLNG's sandbox, in the region
serving the call, answers in one hop. The same logic behind a webhook of your own
is a round trip out of SLNG's network and back, on every call.

The consequences show up in this package:

- **No network access at all.** `check_order` answers from a table for exactly
  that reason. A handler that has to reach a service is a `webhook:` tool
  instead, and `unmute validate` refuses the import rather than letting it fail
  in the sandbox.
- **No secret.** Deliberate: it keeps this example deployable with nothing set up
  in a dashboard. A tool that authenticates names its credential and you create a
  vault entry of that name before the push. Unmute never sees a value.
- **A publish gate.** A `code` or `api_request` tool cannot be attached to a live
  agent until one successful run proves it. So it needs a sample.

## The sample

`unmute deploy` without `--run-samples` refuses, and says so before touching your
organisation:

```text
Cannot deploy slng. 1 problem:

  sample missing (1)
    check_order (code)
    a code or api_request tool cannot publish until one successful run proves it. …
    samples for this target belong in examples/slng-orders/build/slng/samples, and `unmute deploy --run-samples` runs them.

  nothing was created or changed.
```

One JSON object, one set of arguments, named after the tool:

```bash
mkdir -p examples/slng-orders/build/slng/samples
cat > examples/slng-orders/build/slng/samples/check_order.json <<'JSON'
{"order_number": "A-1001"}
JSON
```

It lives in the build directory and not in this package on purpose. A sample is
**operator input**, like `.env`: what counts as a safe set of arguments depends on
your environment and your real dependencies, not on the agent definition, and two
organisations deploying this package would want different ones. Those two are the
only things a recompile preserves inside `build/`; everything else there is
regenerated.

`A-1001`, `A-1002` and `A-1003` are the order numbers `tools/check_order.py`
knows about.

## What compiling writes

```bash
bin/unmute validate examples/slng-orders
bin/unmute compile examples/slng-orders
```

```text
examples/slng-orders/build/slng/
├── agent.json                the agent create body
├── tools/check_order.json    the tool create body, with the handler in code_src
└── README.md                 the runbook
```

`code_src` is your handler file byte for byte, followed by a generated block:
unmute derives an `Input` and `Output` model from the tool's `input`/`output`
schemas and a `handler()` that calls your function. That is the rule that lets one
handler work on all three targets — the file you wrote is never rewritten.

SLNG introspects the uploaded source to derive the schemas the model sees, so the
handler's type hints have to agree with the YAML. A mismatch is rejected at push.

## What this package leaves out, and why

Same list as `slng-support`, for the same reason: SLNG owns the layer below the
agent, so there is nothing here to configure and each of these is refused at
validate rather than dropped in silence.

| Left out | What owns it instead |
|---|---|
| a `turn:` section | SLNG's own turn taking |
| tasks, task groups, agent transfers | SLNG's agent carries one prompt |
| inactivity, max_duration, interruption thresholds | no field on the create body holds them |
| tracing, capacity | unmute instruments and sizes no process here |
| `version`, `pins`, `sdk_language`, `connection` in targets.yaml | all four describe a generated project, and there is none |

## Talk to it

There is no `unmute dev` for this target: `dev` runs a generated project locally
and this one generates none.

```bash
cat > session.json <<'JSON'
{"arguments": {"customer_name": "Nicola"}, "participant_name": "you"}
JSON
voiceai agents web-sessions create <agent_id> --file session.json
```

A `livekit_dispatch_id` in the response means a worker joined the room, which is
what tells you the agent is running rather than merely created.

The agent id is positional and required. `--file` is required too, even though
every field in it is optional, because the endpoint declares a request body and
the CLI sends none without it.

Then say "I have order A-1001".

## A note on the agent's name

Both slng examples name their target instance `slng`, and today an agent's name on
SLNG is its target instance name. So deploying both writes the same agent: the
second replaces the first, and `unmute deploy` warns when it is about to. Deploy
one, rename a target instance, or pass `--agent-id`.

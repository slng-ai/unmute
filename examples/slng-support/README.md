# slng-support

A support agent hosted by SLNG. Nothing to run, nothing to keep alive.

This is the only example that targets `slng`, and the only one that produces no
runnable project. `unmute compile` writes a deployment body and a runbook, and
the `voiceai` CLI pushes them. Unmute opens no connection to SLNG at any point.

```bash
bin/unmute validate examples/slng-support
bin/unmute compile examples/slng-support
```

That writes:

```text
examples/slng-support/build/slng/
├── agent.json    the agent create body
└── README.md     the runbook: what to create, what to run, what to watch for
```

No `tools/` directory, because every tool here is a builtin.

## Why every tool is a builtin

`end_call` names a capability SLNG already owns, so nothing has to be *created*
before this agent can exist. That is the smallest push there is.

It is not a zero-step push. Read the emitted runbook before you run anything:
SLNG's `tool_refs` entries require `attachment_id`, `tool_id` and `version`, and
unmute writes a name where the `tool_id` goes, because no compiler can invent an
id a server assigns. A curated capability has an id too. So even here, the name
has to be resolved before the body is accepted.

```bash
export VOICEAI_API_KEY=...
voiceai agents create --file examples/slng-support/build/slng/agent.json --json
```

`voiceai login` stores the key instead, if you prefer.

The `voiceai` CLI has no `tools` command yet, so nothing resolves that name for
you today: fill the id in from the SLNG dashboard, or create the agent with no
tools and attach `end_call` there.

A package with a `local:` or `webhook:` tool compiles too, and its bodies land
under `tools/`. Those need each tool created first *and* its new id filled in,
which is strictly more work than this example asks for.

A `local:` tool may also pin what its handler imports, with exact
`name==version` entries under `local.dependencies`. The sandbox runs Python 3.14
and has `pydantic`, so a stdlib-only handler pins nothing.

The runbook says which case a given package is in, and says it in the emitted
file rather than expecting you to remember.

## There is no `unmute dev` here

`dev` runs the generated project locally. This target generates no project:
SLNG runs the agent. To talk to it, open a web session:

```bash
voiceai agents web-sessions create <agent_id> --file session.json
```

The id is the one `agents create` returned. It is required: the command creates
a session for one named agent.

The `--file` is required even though every field in it is optional, because the
endpoint declares a request body and the CLI sends none without it. A minimal
`session.json` is `{"arguments": {}}`; `arguments` is where the package's
declared variables get their value for one session.

For this example that means `{"arguments": {"customer_name": "Nicola"}}` greets
the caller by name, and `{"arguments": {}}` falls back to the declared default.

## Writing model names

SLNG names a model with the vendor and the model joined by a slash. A package
writes them as two fields, the same as on every other target, and the slng
driver joins them when it writes the body.

A model name that already carries a slash is passed through whole instead of
joined twice, which is how a Context Router model such as
`slng/deepgram/nova:3-en` reaches the body unchanged. Both shapes are in this
example's `agent.yaml`.

SLNG owns its model list, so unmute checks no vendor and no model name here. The
compile report says so for every binding it forwards.

## What this package deliberately leaves out

Each of these is refused by `unmute validate --target slng`, by name, with what
to do instead. None is dropped silently.

| Left out | Why |
|---|---|
| a `turn:` section | SLNG owns its own turn taking; a bound turn model reaches nothing |
| tasks, task groups, agent transfers | SLNG's agent carries one prompt and one greeting |
| `conversation.inactivity` | SLNG's idle nudges need three spoken texts a package does not carry |
| `conversation.max_duration`, `thinking_audio`, interruption thresholds | no field on the create body holds them |
| `tracing:` | unmute instruments no process here, so it can install no exporter |
| `version:`, `pins:`, `sdk_language:`, `connection:` | all four describe a generated project, and there is none |

## The Vault

This package needs no SLNG Vault entries, and the runbook says so rather than
printing an empty list. A package that authenticates a webhook tool, or writes a
`{{$NAME}}` Vault token into a prompt, gets a table of the names it needs and
where each one came from. Unmute lists names and never values: no secret value
reaches any emitted file or any command in the runbook.

## Regions

`deployment_region` takes exactly one of `any`, `us-east`, `eu-central` or
`ap-south`. This example uses `any`, which lets SLNG route each call itself.

That is deliberate, and it was learned the hard way. Not every model is served in
every region: pinning `eu-central` here failed at push with

```text
STT model 'slng/deepgram/nova:3-en' is not allowed for region 'eu-central' and language 'en'
```

Unmute cannot check that. The region-by-language-by-model matrix lives in SLNG
and is per organisation. `any` sidesteps it; pin a region when you need the
agent in one place and are willing to check the models exist there.

`deployment_region` is the only region *value* check in the compiler, because
slng is the only target whose platform publishes a closed set of names. Which
models each of those names serves is a different question, and one only SLNG can
answer.

## Model names are per organisation

The model strings here are the ones this repository uses in its other examples.
Whether your organisation has them enabled for agents, and in which regions, is
something only SLNG knows. A string it will not accept fails at push with
`AGENT_MODEL_UNAVAILABLE` naming the field, so you find out in one command
rather than on a call.

# slng-support

A support agent hosted by SLNG. Nothing to run, nothing to keep alive.

This is the only example that targets `slng`, and the only one that produces no
runnable project. One command validates it, compiles it, and pushes it:

```bash
export SLNG_API_KEY=...
bin/unmute deploy examples/slng-support
```

`--dry-run` reports what would happen and changes nothing. `unmute deploy` reads
`SLNG_API_KEY` first, then `VOICEAI_API_KEY`, then whatever profile
`voiceai login` stored: one SLNG key serves every SLNG role, so either name
works and both hold the same token.

Unmute's compiler opens no connection to SLNG at any point. `unmute deploy`
compiles the package and hands the artifacts to the `voiceai` CLI, which owns
your account and the push, so `voiceai` has to be on your PATH:

```bash
brew install slng-ai/tap/voiceai
```

Compiling on its own still works, and writes the same files:

```bash
bin/unmute validate examples/slng-support
bin/unmute compile examples/slng-support
```

That writes:

```text
examples/slng-support/build/slng/
├── agent.json              the agent create body
├── tools/
│   ├── check_order.json    the code tool's body
│   └── refund.json         the webhook tool's body
├── samples/
│   └── check_order.json    one call's arguments, so the code tool can publish
└── README.md               the runbook: what to create, what to run, what to watch for
```

`end_call` gets no file: a builtin is referenced, not created.

## The agent's name

`agent.yaml` here says `name: acme-support`, and the target in `targets.yaml` is
called `slng`, so this pushes an agent called **acme-support-slng**.

SLNG has no project to deploy into. The agent's name *is* its identity: names
are unique across an organisation, and a push resolves which agent to write by
matching one. So the name has to say which agent this package is, and neither
name unmute could have inferred does. The target is called `slng`, which is what
every package on this target calls it, so it names the platform. The folder is
called `slng-support` here and something else in your checkout, because a folder
is named by whoever cloned the repository.

Unmute wrote the target name into the body until it was fixed. Two packages in
one organisation both compiled to an agent called `slng`, and pushing the second
one replaced the first: same live agent, new prompt, new models, tool
attachments detached.

Pick a name no other package in your organisation uses, and check before the
first push:

```bash
voiceai agents list
```

Renaming later does not move a deployed agent. It leaves the old one running and
creates a second, so rename early or clean up after.

## Three kinds of tool, and why the difference matters

This package has one of each, because they behave differently at deploy time.

| File | Kind | Who creates it | Catch |
|---|---|---|---|
| `tools/end_call.yaml` | builtin | nobody: SLNG already owns it | must ALREADY EXIST in your org, and is referenced by the **file's** name |
| `tools/check_order.yaml` | code | the push | no internet access; cannot publish without a sample run |
| `tools/refund.yaml` | webhook | the push | the only one that may reach the network; carries the Vault secret |

**The builtin trap.** The emitted reference carries the tool *file's* name, not
the `builtin: id` inside it. Name the file `hang_up.yaml` while selecting
`builtin: end_call` and the body asks SLNG for a tool called `hang_up`, which
does not exist. Nothing refuses that at validate; `unmute deploy` catches it and
tells you to rename the file.

**The code trap.** Custom code on SLNG runs in a sandbox with **no internet
access at all**. `requests`, `httpx` and `urllib` are refused at validate rather
than failing on a live call. Anything that needs the network is a webhook tool.

**The publish trap.** A `code` or `api_request` tool cannot be published until
one successful run proves it. That needs a sample, one JSON object of arguments
at `build/slng/samples/<tool>.json`, run with `unmute deploy --run-samples`.

A builtin-only package would be the smallest push there is. Even that is not a
body you can post by hand. SLNG's `tool_refs` entries require
`attachment_id`, `tool_id` and `version`, and unmute writes a name where the
`tool_id` goes, because no compiler can invent an id a server assigns. A curated
capability has an id too.

Resolving those names is what the push step does, which is why
`voiceai agents create --file build/slng/agent.json` is the wrong command: it
posts the body verbatim, names included, and the API refuses it. Use
`unmute deploy`, or the command it runs underneath:

```bash
export VOICEAI_API_KEY=...
voiceai agents push examples/slng-support/build/slng
```

`voiceai login` stores the key instead, if you prefer.

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

The id is the one `unmute deploy` printed. It is required: the command creates a
session for one named agent.

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

SLNG owns its model list, so unmute checks no vendor and no model name here.
`build/slng/compile-report.json` records every binding it forwards, unchecked,
under `bindings`.

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

`refund` authenticates with a bearer token, so this package needs one Vault
entry: `REFUND_API_TOKEN`. The emitted runbook prints it in a table with the line
of the package that asked for it. Unmute lists names and never values: no secret
value reaches any emitted file or any command in the runbook.

`agent.yaml` also declares `REFUND_URL` under `secrets:`, and that one does *not*
become a Vault entry. The livekit and pipecat targets read the whole URL from the
environment at run time; SLNG stores a literal `base_url` in the tool body
instead, because its URL validator rejects a token in the host. Same package,
two mechanisms, and only the token is a secret on SLNG.

`unmute deploy` checks that same list against your organisation before it
compiles anything, and offers to create whatever is missing. It does not prompt
for the value itself: it runs `voiceai secret create`, which masks its own
input, so the value never passes through unmute at all.

## What deploy checks before it writes anything

The push refuses a body whose references do not resolve, and it refuses them all
at once. `unmute deploy` asks your organisation the same questions first, before
it compiles, so a refusal costs nothing and names the line of the package that
caused it:

- **`builtin:` tools.** This package's `end_call` has to exist in your
  organisation, and it does: SLNG lists its curated capabilities as ordinary
  tools. The name checked is **the tool file's own name**, because that is what
  the emitted reference carries. `tools/hang_up.yaml` selecting
  `builtin: end_call` emits a reference to `hang_up`, which resolves nowhere.
- **MCP servers and their tools.** Checked by name, against the server's last
  stored capability probe. That probe is not a live call, so a healthy server can
  still be unreachable.
- **Vault secrets and variables.** Checked for existing *and* for holding a
  value. An entry created without one is reported differently from a missing
  name, because the fix is different.

`check_order` and `refund` are never reported as missing. The push creates them,
so their absence is the expected state of a first deploy.

Run [`unmute resources`](https://unmute.mintlify.app/reference/cli/resources) to
see what your organisation offers before you write a name.

## Giving it a phone number

This package declares no `connection:`, and the slng target refuses one. Carrier
state is not a package field here: the compiled body is portable, and which
number answers is an operator's choice about one deployment.

After a successful push, `unmute deploy` says which number reaches the agent. If
none does and an inbound trunk is free, it offers to attach one, and does so only
if you pick a number at a terminal. Buying the number and configuring the trunk
stay in the SLNG dashboard; this chooses among trunks that already exist.

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

# Tools

A tool is two things: a contract the model sees, and something that runs when
the model calls it. Both live in one file, `tools/<name>.yaml`.

## The shape of a tool file

```yaml tools/find_slots.yaml
description: >-
  List Sage and Stone slots for one service and date, and the caller's own
  bookings. Call this before offering a time and before changing a booking.

local:
  handler: tools/salon.py:find_slots
```

The top is the contract. For a local tool, the typed signature of `find_slots` in
`tools/salon.py` is the input, and its Pydantic return model is the output, so the
file has no `input:` or `output:`. A webhook tool writes both as JSON Schema. Write
the description as an instruction rather than a label, and let the types do real
work: a `Literal["haircut", "hair-color", "blowout"]` on `service` means the model
cannot ask for a service the salon does not offer.

The signature is the complete argument list. Keep workflow prerequisites in the
agent or task prompt; do not make their names look like extra tool inputs in the
description. Generated Pipecat direct tools return a corrective result when a
provider adds an undeclared argument, or when a handler fails before returning,
so the model can retry instead of leaving the call stuck in progress.

The block near the bottom says how the tool runs. **Every tool file has exactly
one execution block.** Two is an error, and none is an error whose message is
also the list of what you could have written.

## The eight execution blocks

| Block | The tool is | Reach for it when |
|---|---|---|
| `webhook:` | an HTTP call to a URL named by an environment variable | the user already has an API. This is the everyday case |
| `local:` | a Python function in the package | the call needs code of your own: a signature, a transform, a fixture |
| `mcp:` | a remote MCP server that offers its own tools | the user names a server and wants what it exposes |
| `builtin:` | a tool the runtime already has, selected by id | you want `end_call`, or `send_sms` on the `slng` target |
| `slng:` | a tool the SLNG platform already hosts | the user names a tool that exists in their SLNG organisation. See below |
| `client:` | a tool the caller's own application fulfils | never yet. Gated, see below |
| `provider_hosted:` | a tool the model provider runs itself | never yet. Gated, see below |
| `knowledge:` | a search over a folder of the user's own documents | the user has policies, price lists, or manuals the agent should quote instead of guess |

### Which fields each block allows

| Field | Required | Legal on |
|---|---|---|
| `description` | yes, except on `builtin:` and `mcp:` | everywhere else |
| `input` | on `webhook:` only; a `local:` tool's handler signature is its input | `webhook:` and version 1 `local:` |
| `output` | no | `webhook:` and version 1 `local:`, but see below. A version 2 `local:` return model is its output |
| `with` | no | `webhook:`, `local:` and `slng:`, and `builtin:` for a capability's own settings |
| `precheck` | no | `webhook:` and `local:` |
| `interruption` | no | everywhere except `mcp:` |
| `effect` | no | everywhere except `mcp:` and `knowledge:` |

The tool file keeps what the tool is. What it does in a conversation (a spoken
line while it runs, what it saves, what is said after it, whether the model
sees it) is written on the tool's entry in `agent.yaml`: see "The tool entry in
agent.yaml" below.

An `mcp:` file is the block and nothing else, because the server owns each
tool's contract. A `builtin:` file needs no `description` or `input`, because
the registry supplies both. A `knowledge:` file takes no `input` or `output`
either, because the tool owns both: it asks for one string and returns passages.
An `slng:` file takes no `input` or `output` either, and for the same reason:
the platform published the schema, so a second copy here could disagree with it.

**`output:` is author-side documentation, not a contract with the model.** The
compiler checks that it is a JSON Schema object, but no generator sends it to
the model, puts it in the compile report, or enforces it at run time. The
generated wrapper returns whatever the endpoint or handler returned, unshaped.

### The two gated blocks

`client:` and `provider_hosted:` exist in the schema and no target emits them.
Writing one fails with the target named:

```
livekit: LiveKit client tools are not proven by its driver
```

Do not write one, and do not offer one as an option. They are listed here so
that a refusal a user meets reads as a decision rather than a bug.
If you need to show the refusal, YAML requires `client: {}` or
`provider_hosted: {}`; a bare empty block is itself invalid.

## SLNG-hosted tools

The user says a tool already exists in their SLNG organisation. Write one
line naming it, not a copy of it:

```yaml
# tools/check_order.yaml
slng: check_order
```

`slng: check_order` is the whole reference. `check_order` is the tool's exact
name **on the platform**. Write no `description:` and no `input:`: both are
inherited from the published tool. A `description:` you do write overrides
the platform's for this one attachment, which is the right move only when the
user deliberately wants different words spoken than the platform's own
description. Delete it later and inheritance comes back.

Two names, and keeping them straight is what a coding agent gets wrong first:

1. **The tool file's own name is the package reference**, the one an agent's
   `tools:` list attaches and every local diagnostic uses. It does not have
   to match the hosted name: `tools/order_status.yaml` may hold
   `slng: check_order`, so the agent attaches `order_status` and deployment
   resolves `check_order`. This renames nothing on the
   platform; use it when the user wants the file and the prompt to read
   naturally without matching the platform's own naming.
2. **The `slng:` scalar is the hosted name**, checked against the
   organisation only when the user deploys. Get it wrong and the deploy
   refuses, naming the organisation and what it does have; nothing checks it
   offline, because only the organisation knows.

`with:` and the tool's entry in `agent.yaml` work exactly as they do on any
other tool: see "Hidden values the model cannot see" and "The tool entry in
agent.yaml" below. Pinned values keep their exact type, including `false` and `0`; the tool call
remains model-invoked either way, never automatic.

**No hash, no mirror, and no `unmute pull` for a package that deploys to
SLNG only.** Never write `slng: {}` and tell the user to run `unmute pull`:
that was the old shape, superseded by naming the tool directly.

**`slng:` compiles on the slng target only for now.** Version 2 livekit and
pipecat refuse it. When the package also targets one of them, write the tool
as a typed `local:` handler or a `webhook:` there instead.

The `.slng.` files a pull writes are the platform's copy, mirrored, never
authored. Never edit one: the change reaches nothing, and the next compile of
a code target refuses because the pin no longer matches. Change the tool on
the platform and pull again.

The legacy block, `slng:` with `hash:` under it, still loads and still
resolves by the file's own name. It is what an older package carries. Do not
write it into a new tool file; write the scalar name instead.

### Why this block exists

On an slng target a typed `local:` tool works: `unmute deploy` publishes it to
SLNG as a code tool (see the slng rules in [package.md](package.md)). A
`webhook:` block is refused there. A new request tool starts in the SLNG
dashboard, and `slng:` is how a package reaches it once it exists.

```sh
voiceai tool create hosted/check_availability.py --description "..."   # Input, Output, handler
voiceai tool run check_availability --input sample.json --confirm-side-effects
voiceai tool publish check_availability    # a push attaches only a published version
```

`voiceai tool update <name> --file <path>` changes it later.

It costs no portability, for a package that wants livekit or pipecat too. A
pulled mirror carries the platform's own introspected schema and, for a code
tool, its module, so the same package compiles to livekit and pipecat and
runs the same tool inside the generated project.

One limit, and state it rather than letting the user discover it: a hosted
tool that declares Python dependencies compiles to slng, which installs a
per-tool environment, and is **refused** on livekit and pipecat, which build
one dependency list for the whole project. A hosted tool with no dependencies
works on all three. On a slng-only package this limit is invisible until
deploy, because there is no mirror to read it from ahead of time.

**Say plainly what an offline `validate` or `compile` cannot tell the user,**
for a `slng:` reference: whether the organisation still holds that name,
whether a `with:` argument fits its published parameters, and which Vault
entries it needs. `compile-report.json` names these under `deferred_checks`;
`unmute deploy` completes all three before it changes the agent, and reports
what it found. A clean `validate` or `compile` is not proof the reference
exists on the platform.

`unmute pull` needs an SLNG credential and is only needed when a code target
runs a hosted tool itself. `validate` and `compile` work offline; a slng-only
package needs no mirror at all. `deploy` and `resources` also need an account
credential to reach the organisation.

## Knowledge bases

The user has documents and wants the agent to answer from them instead of
guessing. Two parts: a `knowledge:` section in `agent.yaml` naming a folder, and
one tool per base.

```yaml
# agent.yaml
knowledge:
  refunds:
    documents: knowledge/refunds
  services:
    documents: knowledge/services
    embed: openai              # optional; openai is the default
```

```yaml
# tools/look_up_refund_policy.yaml
description: >-
  Look up the company's refund and complaints policy. Use this before you state
  any refund, replacement, timescale, or goodwill offer, so you quote the policy
  instead of guessing it.
knowledge:
  base: refunds
```

To cover the lookup with a spoken line, give the tool a `start:` entry in
`agent.yaml`.

| Field | Required | Default | Rule |
|---|---|---|---|
| map key in `knowledge:` | yes | none | 3 to 64 characters of `[a-z0-9_]` |
| `documents` | yes | none | folder path relative to the package root, holding `.txt`, `.md` or `.pdf` files |
| `embed` | no | `openai` | one of the supported services below |
| `mode` | no | `hybrid` | `meaning`, `keyword`, or `hybrid` |
| `chunk_size` | no | `90` | passage size in **tokens**, 1 to 2048 |
| `chunk_overlap` | no | `20` | tokens two neighbouring passages share; never larger than `chunk_size` |
| `top_k` | no | `3` | passages a lookup returns, 1 to 20 |
| `min_score` | no | none | drop results below this score, 0 to 1. See the warning below |
| `base` on the tool | yes | none | names a base declared in `knowledge:` |

### Which mode to write

Default to leaving `mode` out, which gives `hybrid`. Choose deliberately when the
user's situation matches a column:

| `mode` | Searches by | Needs a key | When to write it |
|---|---|---|---|
| `meaning` | what the question means | yes | callers paraphrase, and the documents use different words than they do |
| `keyword` | the words themselves (BM25) | **no** | codes, names, prices, part numbers; or the user cannot send documents to a third party; or they want no per-lookup latency |
| `hybrid` | both, interleaved | yes | the default, and the right answer when unsure |

`meaning` wins paraphrase, which is the only thing it is for. `keyword` wins exact
terms, and holds up better as a corpus grows. `hybrid` takes both, which is why it
is the default.

**`keyword` is the one to remember.** It needs no embedding service, no credential
in `secrets:`, and makes no network call, so a lookup is local memory access rather
than a round trip. If the user is nervous about sending documents to a third party,
this is the answer. It produces no scores, so `min_score` is refused with it. Do
not sell it on image size: the difference is small, because no mode installs a
vector store.

### The index is built at startup

The version 2 `Dockerfile` has no step that bakes the index into the image, so
every worker process embeds the corpus at startup, and that is paid again on
every scale-up. Say so when a package declares `knowledge:` with a mode that
embeds. It does not apply to `mode: keyword`, which embeds nothing. A lookup
embeds the caller's question either way, so the run time needs the credential in
`secrets:`.

### A startup error that names neither uv nor knowledge

```
PermissionError: Security Violation [pathsec.open]: refusing multiply-linked file
```

This means the `.venv` was installed with hard links out of uv's cache, the
default on Linux. NLTK refuses such a file, and the knowledge index reads its
stopword list through NLTK. The generated `pyproject.toml` sets
`link-mode = "copy"` under `[tool.uv]` and the Dockerfile passes
`--link-mode=copy`, so do not remove either. A `.venv` built before that setting
existed still holds the links, and so does one built with `UV_LINK_MODE=hardlink`
exported. Recompiling deletes `build/<target>/`, `.venv` with it, so recompile or
delete `.venv` and run again.

### When to set the retrieval fields

**Leave them alone unless the user's documents give you a reason.** The defaults
suit prose, and send about 200 tokens of retrieved text per lookup.

Set them when the shape of the document calls for it:

| The user's documents | What to write |
|---|---|
| Prose: policies, manuals, FAQs | nothing; the defaults |
| Lists: prices, opening hours, specifications | `chunk_size: 220`, `chunk_overlap: 40` |
| A caller will quote a long passage back | wider `chunk_size`, and `top_k: 5` |

The list case is the one that bites. At the default 90 tokens a table of prices
splits mid row, so a service name lands in one passage and its price in the next,
and a question about the price ranks something else above it.

**`top_k` times `chunk_size` is what reaches the model on every lookup**, during
a phone call. Above about 1500 tokens the compiler warns. Do not raise both.

**Do not set `min_score` unless the user asks for it, and push back if they name
a high value.** These are similarity scores, not probabilities: in practice they
land well below 1, so `0.9` reads like "high confidence" and in fact returns
nothing. The gap between a genuine answer and an off-topic question is far smaller
than the 0 to 1 range suggests.

**It only works on `mode: meaning`.** On `hybrid` the keyword half returns
unscored passages that survive every cutoff, so a cutoff there removes real
answers and silences nothing. If a user wants one, suggest `0.25` and `mode:
meaning`, tell them the band depends on their own documents, and tell them to
check it before shipping. Above `0.25` the compiler warns.

| Embedding service | Credential to declare in `secrets:` |
|---|---|
| `openai` *(default)* | `OPENAI_API_KEY` |
| `gemini` | `GEMINI_API_KEY` |
| `huggingface` | `HF_TOKEN` |
| `bedrock` | the AWS credential chain, so nothing to declare |

Use `openai` unless the user asks for something else. `embed:` is per base, so two
bases in one package can use different services, and the emitted project installs a
client only for the services actually named. `huggingface` is the hosted Inference
API, not a local model. `bedrock` declares no variable in `secrets:`, because it
authenticates through the AWS credential chain.

### What to write, and what not to

- **Write a real `description`.** It is the only thing that tells the model when
  to look something up rather than answer from memory. Say what is in the folder
  and when to check it, as the example above does.
- **Write a `start:` line.** A lookup takes a moment, and silence sounds like a
  dropped call.
- **Give each agent only the bases it should see.** An agent gets a base by
  being given its tool, so a refunds tool on the concierge means the concierge
  can quote refund policy. That is the whole access model.
- **Do not set `mode`, `chunk_size`, `chunk_overlap`, `top_k` or `min_score`
  without a reason from the user's own documents.** All five exist, and all five
  default to something sensible. Reach for them when the shape of the document
  calls for it, per the two sections above, not by habit.
- **Do not put `input:` or `output:` on the tool.** Refused, with the line number.

### Two things to tell the user

- **Content is fixed until the next compile.** The documents are read, split and
  embedded once when the agent starts. Editing a PDF changes nothing until they
  compile and deploy again.
- **A scanned PDF fails at startup, not at compile.** Deciding whether a PDF
  yields text needs a parser the compiler does not have, so a document with no
  text layer is named and skipped at startup, and a base where nothing yields
  text stops the deployment. If their PDF is a photo of a page, it needs OCR
  first.

## Webhook tools

```yaml tools/confirm_appointment.yaml
description: Confirm that the existing appointment stays as booked. Call it when the customer says the time works.

input:
  type: object
  properties: {}

with:
  - customer_id: state.customer_id
  - channel: phone

webhook:
  url_env: SALON_API_URL
  path: /customers/{{state.customer_id}}/appointments/confirm
  auth:
    type: bearer
    token_env: SALON_API_TOKEN

effect: returns_data
interruption: provider_default
```

| Field | Required | What it is |
|---|---|---|
| `url_env` | yes | the `UPPER_SNAKE` name of a variable holding the base URL |
| `path` | no | starts with `/`, is appended to that base URL, and may carry `{{state.variable}}` tokens |
| `auth` | no | `bearer` or `api_key`; an API key may name a custom `header` |

`webhook:` runs on LiveKit and Pipecat. For SLNG, create the request tool in
the dashboard and reference it with `slng:`.

`url_env` holds a **name**, never a URL. Writing the address there is refused.
That is what lets staging and production run the same package against different
APIs.

Both names (the `url_env` and any `auth.token_env`) also go in the package's
top-level `secrets:` list. That is a separate file from this one, and forgetting
it is a warning at exit 0 rather than an error, so it is easy to miss. See
`package.md`.

`path` renders per call and the rendered value is URL encoded for you. Because
it renders per call rather than at session start, a variable that only gets
its value once a task assigns it partway through the call is fine here. A
token naming nothing at all fails at compile time.

**`path` templates a declared variable, never an `input` property.** These are
two different things and mixing them up is the most common webhook mistake:

```yaml
input:
  type: object
  properties:
    tracking_number:
      type: string

webhook:
  url_env: COURIER_API_URL
  path: /tracking/{{state.tracking_number}}   # WRONG: that is an input property
```

```
tools/track_parcel.yaml:19: tool "track_parcel" webhook.path references {{tracking_number}},
  which is not a declared variable
```

**Every `input` property is sent as the JSON request body already.** So the
usual fix is to delete the template and keep a fixed path:

```yaml
webhook:
  url_env: COURIER_API_URL
  path: /tracking
```

The API then receives `{"tracking_number": "..."}` in the body. Only put a
`{{state.name}}` in the path when `name` is in the package's top-level `variables:`
block, and say to the user which shape the request ended up with, because they
may need to change their endpoint to match.

Every `with` value must be a scalar: a string, number, boolean, or null. Maps
and lists are refused.

### Authentication

| Field | What it is |
|---|---|
| `type` | `bearer` or `api_key` |
| `token_env` | environment variable holding the token |
| `header` | header name, `api_key` only, defaults to `X-API-Key` |

Those two schemes are the whole list in this version. If the user's API needs a
signed request, an OAuth exchange, or mutual TLS, a webhook tool cannot do it.
Say so and write a Python handler instead.

## Python tools

Two files that go together: the tool file and the typed handler it names. The
handler's signature is the tool's input and its Pydantic return model is the
output, so the tool file has no `input:` or `output:`. LiveKit and Pipecat compile
`version: 2` only, and this is the version 2 form.

```yaml tools/cancel_appointment.yaml
description: Cancel the appointment outright. Call it only when the customer says plainly that they want to cancel, never when they want a different time.

with:
  - customer_id: state.customer_id

local:
  handler: tools/cancel_appointment.py:cancel_appointment
```

```python tools/cancel_appointment.py
from pydantic import BaseModel


class Cancellation(BaseModel):
    cancelled: bool
    customer_id: str


def cancel_appointment(customer_id: str) -> Cancellation:
    return Cancellation(cancelled=True, customer_id=customer_id)
```

This is a self-contained fixture that runs without a booking API.

The rules the function follows:

| Rule | Why |
|---|---|
| every parameter has a type | the type becomes the model's input schema |
| the return type is a Pydantic model | its fields are the tool's output, which a `save:` and an outcome's `if:` and `say:` read as `result.x` |
| a `Field(description=...)` on a parameter says what it means | the model reads that text |
| a parameter named by `with:` is not the model's | it takes its value from State, so the model never sees it |
| it may be `async def`, except on slng | livekit and pipecat await an awaitable result, and SLNG calls a code tool synchronously |

`handler:` is written `file.py:function`. The function name need not match the
tool name, and one file can hold many tools. SLNG refuses a version 1
handler, so write the typed function and a `tools/<name>.sample.json` beside it.
Twilio runs the typed handlers too.

A handler reaches a credential through `os.environ`, and the variable name goes
in `secrets:` like any other. Literal lookups are also inferred for generated
environment instructions and startup checks.

`unmute compile` copies the file into the generated project.

## MCP servers

**`mcp:` compiles on the slng target only for now.** Version 2 livekit and
pipecat refuse it. The `url_env`, `transport` and `auth` fields below belonged
to the version 1 livekit and pipecat output.

One block, and nothing else in the file.

```yaml tools/web_search.yaml
mcp:
  server: firecrawl-mcp-2
  url_env: FIRECRAWL_MCP_URL
  transport: streamable_http
  auth:
    type: bearer
    token_env: FIRECRAWL_API_KEY
  tools:
    - firecrawl_search
```

| Field | Required | What it is |
|---|---|---|
| `server` | no | the server's name on the platform, when it is not this tool's own file name. A platform name often carries a dash, which no tool file name can |
| `url_env` | on livekit and pipecat; not read by slng | the `UPPER_SNAKE` name of the variable holding the server address |
| `transport` | no | `sse` or `streamable_http` |
| `auth` | no; never read by slng | `bearer` with `token_env`, or `api_key` with `token_env` and an optional `header` |
| `tools` | on slng | non-empty, unique server tool names to offer; absent means all of them on the code targets, and is refused on slng |

**On a package that only targets slng, write `server:` (when it differs from
the file name) and `tools:`, and nothing else.** SLNG already has the server
registered, with its own address and credential; `url_env`, `transport` and
`auth` exist for livekit and pipecat, which dial the server themselves, and
selecting one of those still requires them.

`url_env` is a name, never an address. `transport` is optional because both
platforms guess it from the URL: a path ending in `/mcp` is streamable HTTP,
anything else is SSE. Write it when you want the choice visible rather than
inferred. Any other value is refused with both legal ones named.

Listing specific `tools:` is usually right. A whole server dropped into an
agent's tool list is a large, unreviewed surface, and the model will use all of
it.

## Prebuilt tools

```yaml tools/end_call.yaml
description: "End the call when the caller is finished or says goodbye."
builtin:
  id: end_call
  instructions: Thank the caller briefly, then end the call.
```

**The registry is closed and has four rows.**

Builtin ids: `current_datetime`, `end_call`, `send_sms`, `user_phone_number`.
`end_call` compiles on every target. The other three are capabilities SLNG
curates and compile on the `slng` target only.
`builtin.instructions` is optional and tells the model what to do as the
prebuilt runs without changing its fixed behavior.

| id | Effect | Settings it takes | Default description |
|---|---|---|---|
| `end_call` | `ends_conversation` | none | End the call when the caller is finished or says goodbye. |
| `send_sms` | `returns_data` | `from_number` | Send a text message to a phone number the caller has given and confirmed. |
| `current_datetime` | `returns_data` | `timezone` | Read the current date and time. |
| `user_phone_number` | `returns_data` | none | Read the number the caller is calling from. |

**The file has to be named after the id it selects.** `slng` attaches a builtin
by the tool file's own name, so `tools/hang_up.yaml` declaring
`builtin: {id: end_call}` asks the platform for a tool called `hang_up`, which
nobody has. Validate refuses it and says which file to rename.

A capability's **settings** are written with `with:`, which on a builtin is
the tool's own configuration and not an argument. A curated capability
publishes no argument schema, so there is nowhere else for a pinned value to go,
and the model never sees one either way.

`send_sms` takes one `from_number`, a literal number in international format
starting with a plus sign, and nothing else; the model supplies the recipient
and the body, and SLNG reads `TWILIO_ACCOUNT_SID` and `TWILIO_AUTH_TOKEN` from
the vault by itself, which `unmute deploy` checks. Pair it with a
`source: conversation` variable so the confirmed number is recorded on the call.

`current_datetime` takes one `timezone`, an IANA zone name. **Write it.** The
curated tool defaults to UTC and belongs to no organisation, so it cannot be
changed on the platform, and an agent that is not in UTC is told the wrong date
for part of every day:

```yaml tools/current_datetime.yaml
description: Read the clock. Call it once at the start of the call, before any day or time is discussed.
builtin:
  id: current_datetime
with:
  - timezone: "America/Los_Angeles"
effect: returns_data
```

A zone survives daylight saving. An offset written into the prompt does not.

There is no plugin seam, and you cannot add to it from a package. Do not invent
a builtin id: an unknown one is refused by name.

If what the user wants is not in that table, it is usually a webhook, a Python
handler, or an MCP server. **One thing it is not is a tool at all:** handing the
caller to a person is an entry under an agent's `transfers:` in
`agent.yaml`, not a file in `tools/`. See `transfers.md`, and check there first,
because on a browser-only package a transfer is not possible at all.

The registry decides the effect and the parameters for you. Writing an `effect`
that disagrees fails rather than being ignored, and a `builtin:` file takes no
`input`, `output`, `handler`, or `url_env`. Leave `description` out and the
registry default is used.

Add `end_call` to every agent that answers a phone. `unmute init` scaffolds it
for exactly that reason.

On `slng`, `end_call` says its own goodbye before it hangs up, "Thanks for
calling. Goodbye!" by default. Set your own with `start:` and `say:` on its entry
in `agent.yaml` (see "The tool entry in agent.yaml" below), and do not tell the
model to say goodbye too.

## Hidden values the model cannot see

```yaml
with:
  - customer_id: state.customer_id
  - channel: phone
```

`with` is an ordered list of one-key items merged into the call and never
advertised to the model, so the model can neither see the value nor overwrite
it. A value written `state.x` is read from State on every call; anything else
is a literal or a template. A `with` key that also names an `input` property is
a compile error, for that reason.

Legal on `webhook:`, `local:` and `slng:`. An MCP server owns its own call
shape, so there is nothing to merge into.

When a pinned variable has no value at call time, the tool refuses instead of
sending a half formed request, and the model is told what to ask for.

Use `with` for anything the caller should not be able to change: the customer
id, the channel, a tenant. A parameter in `input` is a parameter the model can
invent.

## The behaviour fields

```yaml
interruption: provider_default
effect: returns_data
```

| Field | Values | Default | Meaning |
|---|---|---|---|
| `interruption` | `provider_default`, `continue`, `cancel` | `provider_default` | what happens to the call if the caller speaks while the tool runs |
| `effect` | `returns_data`, `ends_conversation` | `returns_data` | whether the conversation continues after the tool |

A tool run by `prefetch:` carries no field of its own for this. The pre-fetch
entry that runs it declares `writes: true` or `writes: false`, because the
question is about that one use of the tool, not about the tool itself. See
`variables.md`, "`writes:` is a promise, not a guarantee".

Both are honoured differently per target. Pipecat maps `interruption` onto its
own cancel-on-interruption setting. LiveKit runs tools to completion, so a
non-default value warns there. Read the warning to the user rather than
dropping it.

## The tool entry in agent.yaml

Every file in `tools/` is a tool, read in name order, so the top-level `tools:`
list does not repeat them. It keeps only entries with behaviour, `save:`,
`outcomes:`, `hidden:` or `start:`. A bare name there is refused. The block says
what the tool does in a conversation, so the tool file stays the contract.

A **top-level** block holds the same for every agent and step:

```yaml agent.yaml
tools:
  - name: find_slots
    start:
      say:
        - Let me check the calendar.
        - Let me see what is free.
    save:
      - last_offer: result.offer
    outcomes:
      - if:
          - status: closed
        say:
          - We are closed that day. Which other day suits you?
      - else:
        say:
          - "{{result.offer}}"
  - name: look_up_staff
    hidden: true
```

| Key | Meaning |
|---|---|
| `start` | `say:` is a fixed line, or a list of alternatives, spoken as the tool starts. `wait: true` holds the call until the line has played |
| `save` | what the tool saves on every result, one `- variable: result.path` pair per item |
| `outcomes` | what is said after the result (see below) |
| `hidden` | with `true`, the model never sees the tool; only code calls it |

A **step** block is the tool inside one step. It holds `confirm:` and
`outcomes:`, and only that step uses them:

```yaml agent.yaml
agents:
  - name: concierge
    prompt: instructions.md
    tasks:
      - name: manage_booking
        prompt: tasks/booking.md
        tools:
          - name: book_appointment
            confirm:
              - Shall I book your {{input.service}} for {{input.date}} at {{input.time}}?
            outcomes:
              - if:
                  - status: booked
                save:
                  - appointment: result.appointment
                say:
                  - Lovely, you are booked for {{result.appointment.date}}.
                then: done
```

### Outcomes

Outcomes run top to bottom, and the first match wins.

- Each starts with `if:` or `else:`. `if:` is a typed match on the result:
  `- status: booked`, or `- status: [a, b]` for either. `else:` takes every
  result no `if:` above matched. It comes last, and there is at most one.
- `then: done` ends the step as completed. `then: failed` ends it as failed,
  and a flow stops unless the step is non-blocking. No `then:` means the step
  goes on. Only a step's outcomes may have `then:`.
- Write outcomes with `then:` first, then those without, then `else:`. An
  `else:` cannot say `then: done`. An `else:` with `then: failed` cannot follow
  an outcome without `then:`.
- An outcome without `then:` needs `say:`. Its `save:` reads `result.` only.
- An outcome with `then: done` and no `save:` saves the step's own `save:`
  rows from the tool's result. A step cannot mix `done` outcomes with and
  without `save:`.

### Confirm

`confirm:` is a line, or a list of alternatives, read back before the tool
runs. The tool runs on the caller's yes. It needs at least one `then: done`
outcome on that tool.

### Lines

| Where | What it can read |
|---|---|
| any line | `{{state.x}}` |
| a `confirm:` line | `{{input.x}}`, the arguments about to be sent |
| an outcome `say:` | `{{result.x}}`, the tool's result |
| a line with no tool (a greeting, a nudge) | State only |
| a tool's `start:` `say:` | fixed text, no template |

### When to write `start:` on a tool

Write it on a tool that keeps the caller waiting: a webhook to a slow service,
a handler that reads a calendar or a database. Do not write it on a fast tool,
and do not write one on every tool. Two agents talking over each other is worse
than a short pause.

The sentence is fixed, so it is spoken word for word every time that tool runs.
Write what the agent is **doing**, never what it expects to find, and keep it
shorter than the wait it covers:

| Write this | Not this |
|---|---|
| `Let me check the calendar.` | `Let me find you some great times!` |
| `One moment while I look that up.` | `I'm querying the availability API.` |

**Write a list when the tool can fire twice in one call.** One sentence is the
same sentence every time, and a caller who books and then moves the booking
hears it twice inside a minute, which is what a recording sounds like. Give
`say:` a list and the caller hears one of them per firing, never the one that
tool used last.

The `slng` target takes one line, because an attachment carries a single
pre-action message. A list there is refused by `unmute validate`, with the
line, rather than quietly narrowed to the first entry.

**On `slng`, a `builtin:` takes one too.** A curated capability is an
attachment like any other, so `current_datetime` can speak before it reads the
clock. On `end_call` the line is the goodbye: SLNG's `end_call` always says one
before it hangs up, "Thanks for calling. Goodbye!" by default, and `start:`
replaces it. Do not also tell the model to say goodbye before it calls
`end_call`, or the caller hears two. A code target refuses it by name: it
builds the prebuilt from its own SDK and has no seam in front of it.

If the package instructions already tell the agent to say it is checking
something, remove that instruction when you add `start:`. Otherwise the model
speaks its own version and the tool speaks the fixed one, and the caller hears
both. This is the most common way to get it wrong.

The second most common way is quieter, because no instruction asks for it. The
model opens its **next** turn with an acknowledgment of its own, so the caller
hears "Okay, one sec." and then "A haircut, lovely. What day suits you?". The
fix is in the prompt, not here: see `prompting.md`, "Do not say the same thing
twice".

Rules that will fail the compile if you break them:

- Legal on `webhook:`, `local:`, `knowledge:`, `slng:` and `builtin:` tools.
  An `mcp:` file has no body to speak before.
- A fixed sentence. `{{variables}}` are refused, because a rendered line would
  need a round trip, which is the delay the field exists to hide.
- A blank value reads as absent. Nothing is spoken and nothing is emitted.
- A target whose driver has no lowering for the field fails validation with
  that driver's own reason. Read the error to the user rather than dropping the
  field.

Nothing waits for the line to finish playing unless `wait: true` is set. The
tool's own work starts straight away, and the tool's `interruption:` value
still decides what happens if the caller speaks over it.

## Define once, attach by name

**Define each tool once.** The full definition exists only in
`tools/<name>.yaml`: `description`, `input`, optional `output` and `with`, and
one execution block. A local handler lives beside it in `tools/<handler>.py`.
Do not put any of those fields in `agent.yaml`.

The tools/ folder is the list of tools. An agent's own `tools:` list still names
the tools that agent may call, and a step's `tools:` the tools that step may
call. Each is a string name, unless a step's entry needs `confirm:` or
`outcomes:`:

- `tools/<name>.yaml` loads the tool; the top-level `tools:` list only adds
  behaviour to one (see above),
- an agent's `tools:` grants that agent access, and
- a task's `tools:` grants that task access.

```yaml agent.yaml
agents:
  - name: appointment_desk
    prompt: instructions.md
    think: reasoning
    speak: voice
    tools:
      - check_slots
      - end_call
```

For a task-scoped tool, attach the same name to the task instead, where the
task is nested inside its agent:

```yaml agent.yaml
agents:
  - name: appointment_desk
    prompt: instructions.md
    tasks:
      - name: find_slot
        prompt: tasks/find-slot.md
        tools:
          - check_slots
```

The agent and task lists are visibility scopes. Attach a tool only where it is
called; do not grant it to both unless both really call it. Never replace a
name with an inline mapping of `description`, `input`, `output`, `local`, or
`webhook`.

**A step's `save:` and a tool's `output:` are different contracts.** A tool's optional
`output:` describes one tool call and stays in `tools/<name>.yaml`. A step's
finish fields come from the destination variables in its `save:` list. A task
that saves nothing needs neither a `save:` nor an invented summary.

A file in `tools/` that no agent or step lists is refused as declared but
unreached. When a tool is never offered, check the agent's `tools:` list first.

Splitting tool lists is how you make a wrong action impossible rather than
discouraged. In `examples/salon-concierge`, only the booking task holds
`save_booking`, so no caller can talk the entry agent into booking, moving, or
cancelling an appointment without going through the step that checks who they
are.

## Choosing a kind, from a plain English ask

| The user says | Write |
|---|---|
| "call our booking API" | `webhook:` with `url_env` and `auth` |
| "our API needs a signed request" | `local:`, because webhook auth is bearer and api_key only |
| "look something up in this spreadsheet of ours" | `local:`, and say the handler is a fixture unless they wire it up |
| "use the Firecrawl MCP server" | `mcp:` with `tools:` naming what it may use |
| "this tool already exists in our SLNG organisation" | `slng:` naming its exact platform name; code targets also need a pulled mirror |
| "let it hang up" | `builtin:` with `id: end_call` |
| "let the caller's app do it" | nothing yet. `client:` is gated on every target. Say so |

When it could be a webhook or a handler, ask one question: is there an HTTP
endpoint already? If yes, webhook. If no, a handler, and say plainly that the
handler you wrote is a stub the user has to fill in.

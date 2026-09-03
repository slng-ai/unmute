# Tools

A tool is two things: a contract the model sees, and something that runs when
the model calls it. Both live in one file, `tools/<name>.yaml`.

## The shape of a tool file

```yaml tools/check_availability.yaml
description: >-
  List Sage and Stone slots for one service and date. Call only after customer
  identification succeeds. This tool accepts only service and date.

input:
  type: object
  properties:
    service:
      type: string
      enum:
        - haircut
        - hair-color
        - blowout
    date:
      type: string
      description: Preferred date in YYYY-MM-DD form
  required:
    - service
    - date

local:
  handler: tools/check_availability.py
```

The top is the contract. `description` and `input` are everything the model
knows about this tool, so write the description as an instruction rather than a
label, and let the schema do real work. The `enum` above means the model cannot
ask for a service the salon does not offer.

The schema is the complete argument list. Keep workflow prerequisites in the
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
| `builtin:` | a tool the runtime already has, selected by id | you want `end_call`, which is the only one |
| `slng:` | a tool the SLNG platform already hosts | the user names a tool that exists in their SLNG organisation. See below |
| `client:` | a tool the caller's own application fulfils | never yet. Gated, see below |
| `provider_hosted:` | a tool the model provider runs itself | never yet. Gated, see below |
| `knowledge:` | a search over a folder of the user's own documents | the user has policies, price lists, or manuals the agent should quote instead of guess |

### Which fields each block allows

| Field | Required | Legal on |
|---|---|---|
| `description` | yes, except on `builtin:` and `mcp:` | everywhere else |
| `input` | yes, except on `builtin:`, `mcp:` and `knowledge:` | everywhere else |
| `output` | no | everywhere except `builtin:`, `mcp:` and `knowledge:` — but see below |
| `inject` | no | `webhook:` and `local:` only |
| `interruption` | no | everywhere except `mcp:` |
| `effect` | no | everywhere except `mcp:` and `knowledge:` |
| `announce` | no | `webhook:`, `local:`, `knowledge:` and `slng:` only |
| `read_only` | no | `webhook:` and `local:` only, and only useful with `prefetch:` |

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

The user says a tool already exists in their SLNG organisation. Write a
reference to it, not a copy of it.

```yaml
# tools/check_order.yaml
description: Look up an order by its number and return its status and delivery date.

slng:
  hash: 336a66b9a564f472...
```

Two rules, and both are things you can get wrong silently:

1. **The file's name is the tool's name.** `tools/check_order.yaml` binds to a
   tool called `check_order` in the organisation. There is no name field. This
   is the same rule `builtin:` follows.
2. **You do not type the hash.** `unmute pull` writes it, along with two files
   beside the tool file: `tools/<name>.slng.json` and, for a code tool,
   `tools/<name>.slng.py`. Write `slng: {}` and tell the user to run
   `unmute pull`, then commit everything it writes.

The `.slng.` files are the platform's copy, mirrored. Never edit one: the change
reaches nothing, and the next compile refuses because the hash no longer
matches. Change the tool in the SLNG dashboard and pull again.

### Why this block exists

The SLNG platform owns a tool's code, version and gate pipeline. So on an slng
target `local:` and `webhook:` are refused: unmute creates no tool there, and a
brand new tool starts in the SLNG dashboard. `slng:` is how a package reaches
one that is already there.

It costs no portability. The committed mirror carries the platform's own
introspected schema and, for a code tool, its module, so the same package
compiles to livekit and pipecat and runs the same tool inside the generated
project.

One limit, and state it rather than discovering it: a hosted tool that declares
Python dependencies compiles to slng, which installs a per-tool environment, and
is **refused** on livekit and pipecat, which build one dependency list for the
whole project. A hosted tool with no dependencies works on all three.

`unmute pull` is the only command that needs an SLNG credential. `validate` and
`compile` read the committed mirror and work offline.

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
announce: "Let me check the policy."
knowledge:
  base: refunds
```

| Field | Required | Default | Rule |
|---|---|---|---|
| map key in `knowledge:` | yes | — | 3 to 64 characters of `[a-z0-9_]` |
| `documents` | yes | — | folder path relative to the package root, holding `.txt`, `.md` or `.pdf` files |
| `embed` | no | `openai` | one of the supported services below |
| `mode` | no | `hybrid` | `meaning`, `keyword`, or `hybrid` |
| `chunk_size` | no | `90` | passage size in **tokens**, 1 to 2048 |
| `chunk_overlap` | no | `20` | tokens two neighbouring passages share; never larger than `chunk_size` |
| `top_k` | no | `3` | passages a lookup returns, 1 to 20 |
| `min_score` | no | none | drop results below this score, 0 to 1. See the warning below |
| `base` on the tool | yes | — | names a base declared in `knowledge:` |

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

### Tell the user to bake the index into the image

Every worker process embeds the corpus at startup otherwise, and that is paid again
on every scale-up. One extra build flag removes it:

```sh
docker build --build-arg KNOWLEDGE_BAKE=1 \
  --secret id=OPENAI_API_KEY,env=OPENAI_API_KEY .
```

Startup becomes a disk read, with the same answers. Use the credential the chosen
embedding service needs, and the generated `README.md` prints the exact command. Both flags are required, and the credential must be a `--secret` rather
than a `--build-arg` so it never lands in a layer.

Mention it whenever a package declares `knowledge:` with a mode that embeds. It does
not apply to `mode: keyword`, which embeds nothing, though baking still saves the
splitting work. A lookup embeds the caller's question either way, so the run time
still needs the credential in `secrets:`.

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
- **Write an `announce`.** A lookup takes a moment, and silence sounds like a
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

inject:
  customer_id: "{{customer_id}}"
  channel: phone

webhook:
  url_env: SALON_API_URL
  path: /customers/{{customer_id}}/appointments/confirm
  auth:
    type: bearer
    token_env: SALON_API_TOKEN

effect: returns_data
interruption: provider_default
```

| Field | Required | What it is |
|---|---|---|
| `url_env` | yes | the `UPPER_SNAKE` name of a variable holding the base URL |
| `base_url` | on slng | the literal `https://` host; SLNG stores the URL in the tool body and refuses a tool that names only `url_env` |
| `path` | no | starts with `/`, is appended to that base URL, and may carry `{{variable}}` tokens |
| `auth` | no | how the request authenticates; on slng an `api_key` with a custom `header` is sent as `bearer`, the header name is dropped |

`url_env` holds a **name**, never a URL. Writing the address there is refused.
That is what lets staging and production run the same package against different
APIs.

Both names — the `url_env` and any `auth.token_env` — also go in the package's
top-level `secrets:` list. That is a separate file from this one, and forgetting
it is a warning at exit 0 rather than an error, so it is easy to miss. See
`package.md`.

`path` renders per call and the rendered value is URL encoded for you. Because
it renders per call rather than at session start, a variable the conversation
itself filled in is fine here. A token naming nothing at all fails at compile
time.

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
  path: /tracking/{{tracking_number}}   # WRONG: that is an input property
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
`{{name}}` in the path when `name` is in the package's top-level `variables:`
block, and say to the user which shape the request ended up with, because they
may need to change their endpoint to match.

Every `inject` value must be a scalar: a string, number, boolean, or null. Maps
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

Two files that go together: the tool file and the handler beside it.

```yaml tools/cancel_appointment.yaml
description: Cancel the appointment outright. Call it only when the customer says plainly that they want to cancel, never when they want a different time.

input:
  type: object
  properties: {}

inject:
  customer_id: "{{customer_id}}"

output:
  type: object
  properties:
    cancelled:
      type: boolean
    customer_id:
      type: string
  required:
    - cancelled
    - customer_id

local: {}
```

```python tools/cancel_appointment.py
def cancel_appointment(customer_id):
    return {"cancelled": True, "customer_id": customer_id}
```

This is a self-contained fixture that runs without a booking API.

The rules the function follows:

| Rule | Why |
|---|---|
| the function name matches the tool name | that is how the generated code finds it |
| its parameters match the `input` properties plus the `inject` keys | the call is built from both |
| it returns the value your description and prompt expect | the result goes back to the model; the code targets do not enforce `output`, SLNG turns it into a pydantic `Output` class and validates the return value against it |
| it may be `async def` on the code targets | the generated code awaits an awaitable result; SLNG calls the handler synchronously and refuses an `async def`, and refuses any import of `requests`, `httpx`, `urllib`, `urllib3`, `aiohttp`, `http.client` or `socket`, because custom code there has no network |
| it imports nothing from Unmute | the generated project does not depend on Unmute at run time |

**An optional `input` property is always passed, as an empty string.** The
generated call is by keyword every time, so a Python default in your handler is
dead code — it receives `""`, not `None` and not your default. Write
`def check(date, part_of_day="")` and treat `""` as "not given". A handler that
tests `if part_of_day is None:` compiles clean and misbehaves on the first call,
and neither `validate` nor `compile` will say a word about it.

A handler reaches a credential through `os.environ`, and the variable name goes
in `secrets:` like any other. Literal lookups are also inferred for generated
environment instructions and startup checks.

`unmute compile` copies the file into the generated project and imports it as a
plain module.

The `local.handler` field is optional. When it is absent, Unmute uses
`tools/<tool-name>.py`, so the example above resolves to
`tools/cancel_appointment.py`.

## MCP servers

One block, and nothing else in the file.

```yaml tools/web_search.yaml
mcp:
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
| `url_env` | yes | the `UPPER_SNAKE` name of the variable holding the server address |
| `transport` | no | `sse` or `streamable_http` |
| `auth` | no | `bearer` with `token_env`, or `api_key` with `token_env` and an optional `header` |
| `tools` | on slng | non-empty, unique server tool names to offer; absent means all of them on the code targets, and is refused on slng |

`url_env` is a name, never an address. `transport` is optional because both
platforms guess it from the URL: a path ending in `/mcp` is streamable HTTP,
anything else is SSE. Write it when you want the choice visible rather than
inferred. Any other value is refused with both legal ones named.

Listing specific `tools:` is usually right. A whole server dropped into an
agent's tool list is a large, unreviewed surface, and the model will use all of
it.

MCP sources are required at runtime. Pipecat connects them during bot setup;
LiveKit probes every source before `AgentSession.start`, always attempts to close
every created probe, and mounts a fresh client on the agent or task. A connection
or tool-list error stops the session before it greets the caller on either
target. A LiveKit probe close error also stops startup. Pipecat cleanup errors
surface during teardown after every close has been attempted.

With Langfuse tracing enabled, Pipecat MCP calls emit finite `tool:<name>` spans with tool arguments and, when completed, the result.
With Coval tracing enabled, the same calls emit `llm_tool_call` spans carrying `function.name`, `tool_call_id`, `function.arguments`, the bounded result as `tool.result`, `tool.latency_ms`, and a numeric `tool.error`, with an error status when the tool failed.
Pipecat refuses to start when an agent tool, task function, or MCP source on the same agent exposes the same name.

## Prebuilt tools

```yaml tools/end_call.yaml
description: "End the call when the caller is finished or says goodbye."
builtin:
  id: end_call
  instructions: Thank the caller briefly, then end the call.
```

**The registry is closed and has one row.**

Builtin ids: `end_call`. `builtin.instructions` is optional and tells the model
what to do as the prebuilt runs without changing its fixed behavior.

| id | Effect | Default description |
|---|---|---|
| `end_call` | `ends_conversation` | End the call when the caller is finished or says goodbye. |

There is no plugin seam, and you cannot add to it from a package. Do not invent
a builtin id: an unknown one is refused by name.

If what the user wants is not `end_call`, it is usually a webhook, a Python
handler, or an MCP server. **One thing it is not is a tool at all:** handing the
caller to a person is an entry under `escalations:` at the top level of
`agent.yaml`, not a file in `tools/`. See `transfers.md`, and check there first,
because on a browser-only package a transfer is not possible at all.

The registry decides the effect and the parameters for you. Writing an `effect`
that disagrees fails rather than being ignored, and a `builtin:` file takes no
`input`, `output`, `handler`, or `url_env`. Leave `description` out and the
registry default is used.

Add `end_call` to every agent that answers a phone. `unmute init` scaffolds it
for exactly that reason.

## Hidden values the model cannot see

```yaml
inject:
  customer_id: "{{customer_id}}"
  channel: phone
```

`inject` is a flat map merged into the call and never advertised to the model,
so the model can neither see the value nor overwrite it. An `inject` key that
also names an `input` property is a compile error, for that reason.

Legal on `webhook:` and `local:` only. An MCP server owns its own call shape, so
there is nothing to merge into.

When an injected variable has no value at call time, the tool refuses instead of
sending a half formed request, and the model is told what to ask for.

Use `inject` for anything the caller should not be able to change: the customer
id, the channel, a tenant. A parameter in `input` is a parameter the model can
invent.

## The behaviour fields

```yaml
interruption: provider_default
effect: returns_data
announce: Let me check the calendar.
read_only: true
```

| Field | Values | Default | Meaning |
|---|---|---|---|
| `interruption` | `provider_default`, `continue`, `cancel` | `provider_default` | what happens to the call if the caller speaks while the tool runs |
| `effect` | `returns_data`, `ends_conversation` | `returns_data` | whether the conversation continues after the tool |
| `announce` | any one sentence | absent, nothing is spoken | a fixed line the agent speaks as the tool starts, so a slow call is not silence |
| `read_only` | `true` | absent, which reads as false | your promise that this tool writes nothing, which a `prefetch:` entry requires before it may run the tool |

### `read_only:` is a promise, not a guarantee

**The compiler cannot check it.** It checks that you made it. Nothing reads your
handler or your endpoint to see whether it writes; `read_only: true` is a claim
about a tool, and a wrong claim compiles.

It is required before `prefetch:` may run a tool, because a pre-fetch runs unasked
on every call: a tool that writes would write on every call, wrong numbers
included. So a lookup that creates a record when it finds none is exactly the tool
this field must not be put on, however convenient. Write a reading tool beside it
and pre-fetch that one.

Declaring it on a tool no `prefetch:` names is a warning: the declaration reaches
nothing.

All three are honoured differently per target. Pipecat maps `interruption` onto
its own cancel-on-interruption setting. LiveKit runs tools to completion, so a
non-default value warns there. Read the warning to the user rather than dropping
it.

### When to write `announce:`

Write it on a tool that keeps the caller waiting: a webhook to a slow service, a
handler that reads a calendar or a database. Do not write it on a fast tool, and
do not write one on every tool. Two agents talking over each other is worse than
a short pause.

The sentence is fixed, so it is spoken word for word every time that tool runs.
Write what the agent is **doing**, never what it expects to find, and keep it
shorter than the wait it covers:

| Write this | Not this |
|---|---|
| `Let me check the calendar.` | `Let me find you some great times!` |
| `One moment while I look that up.` | `I'm querying the availability API.` |

If the package instructions already tell the agent to say it is checking
something, remove that instruction when you add `announce:`. Otherwise the model
speaks its own version and the tool speaks the fixed one, and the caller hears
both. This is the most common way to get it wrong.

The second most common way is quieter, because no instruction asks for it. The
model opens its **next** turn with an acknowledgment of its own, so the caller
hears "Okay, one sec." and then "A haircut, lovely. What day suits you?". The fix
is in the prompt, not here: see `prompting.md`, "Do not say the same thing twice".

Rules that will fail the compile if you break them:

- Legal on `webhook:` and `local:` only. Every other kind has no body to speak
  before. An `mcp:` file is refused at load with the line number.
- A fixed sentence. `{{variables}}` are refused, because a rendered line would
  need a round trip, which is the delay the field exists to hide.
- A blank value reads as absent. Nothing is spoken and nothing is emitted.
- Legal on a tool listed on an agent **or** on a task, on both code drivers.
  LiveKit lowers both through one path and emits the same `session.say`. Pipecat
  emits an agent tool as a decorated function and a task tool as a flows handler,
  and queues the frame through `FlowManager.worker` in the second case. This rule
  used to say a Pipecat task tool was refused by name; that was wrong, and the
  compiler was refusing a feature that worked.
- A target whose driver has no lowering for the field fails validation with that
  driver's own reason. Read the error to the user rather than dropping the field.

Nothing waits for the line to finish playing, on either driver. The tool's own
work starts straight away, and the tool's `interruption:` value still decides
what happens if the caller speaks over it.

## Define once, attach by name

**Define each tool once.** The full definition exists only in
`tools/<name>.yaml`: `description`, `input`, optional `output` and `inject`, and
one execution block. A local handler lives beside it in `tools/<handler>.py`.
Do not put any of those fields in `agent.yaml`.

Every `tools:` entry in `agent.yaml` is a string name:

- the top-level list loads `tools/<name>.yaml`,
- `agents.<name>.tools` grants an agent access, and
- `tasks.<name>.tools` grants a task access.

```yaml agent.yaml
tools:
  - check_availability
  - end_call

agents:
  appointment_desk:
    instructions: instructions.md
    think: reasoning
    speak: voice
    tools:
      - check_availability
      - end_call
```

For a task-scoped tool, attach the same loaded name to the task instead,
where the task is nested inside its agent:

```yaml agent.yaml
agents:
  appointment_desk:
    tasks:
      - name: find_slot
        instructions: tasks/find-slot.md
        tools:
          - check_availability
        result:
          summary: string
        context:
          history: full
```

The agent and task lists are visibility scopes. Attach a tool only where it is
called; do not grant it to both unless both really call it. Never replace a
name with an inline mapping of `description`, `input`, `output`, `local`, or
`webhook`.

**Task `result:` and tool `output:` are different contracts.** A tool's optional
`output:` describes one tool call and stays in `tools/<name>.yaml`. A task's
required `result:` describes what the whole task returns to its caller after
any tool calls. It may select or combine tool data, so design it for what the
caller needs instead of copying a tool output schema by default.

A file in `tools/` that the package level list does not name is not loaded at
all, and nothing complains. When a tool is never offered, check that list first.

Splitting tool lists is how you make a wrong action impossible rather than
discouraged. In `examples/salon-concierge`, only the booking task holds
`cancel_booking`, so no caller can talk the entry agent into a cancellation
without going through the step that checks who they are.

## Choosing a kind, from a plain English ask

| The user says | Write |
|---|---|
| "call our booking API" | `webhook:` with `url_env` and `auth` |
| "our API needs a signed request" | `local:`, because webhook auth is bearer and api_key only |
| "look something up in this spreadsheet of ours" | `local:`, and say the handler is a fixture unless they wire it up |
| "use the Firecrawl MCP server" | `mcp:` with `tools:` naming what it may use |
| "let it hang up" | `builtin:` with `id: end_call` |
| "let the caller's app do it" | nothing yet. `client:` is gated on every target. Say so |

When it could be a webhook or a handler, ask one question: is there an HTTP
endpoint already? If yes, webhook. If no, a handler, and say plainly that the
handler you wrote is a stub the user has to fill in.

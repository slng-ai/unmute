# The SLNG push

Everything `unmute deploy` checks, every way it refuses, and the parts of a push
no package can express. `SKILL.md` beside this file has the command loop and what
a push replaces.

## What is checked before anything is written

`unmute deploy` asks the organisation what it already has, resolves each hosted
reference to a published tool, checks it, compares the result with what the
package needs, and reports every gap in one pass **before** it pushes the agent.
A dry run changes no live state. A real deploy may first refresh MCP discovery or
fill a Vault entry with consent; the report names those changes even if a later
check fails. Build and report files are disposable.

Six things are checked, and knowing which is which is the difference between a
useful package and one that fails at the push:

| Checked | Not checked |
|---|---|
| every `builtin:` tool, by the **tool file's own name** | a control, because the compiled body carries no reference to one |
| every `slng:` reference, against the organisation's tools and the latest published version | a `local:` or `webhook:` tool: refused before this step is reached, because unmute creates no tool on this target at all |
| every `inject:` argument on a `slng:` reference, against that version's published parameters | |
| every MCP server named by an `mcp:` tool, and every tool listed under `mcp.tools` | |
| every vault secret and variable, including one a hosted tool or MCP server needs that nothing in the package declares | |
| two tool files resolving to the same hosted tool, refused before anything is staged and naming both | |

**Two severities for a hosted reference, and telling them apart matters.** A name
the organisation does not have at all stops the run: unmute creates no tool, so
there is nothing else to try. A committed mirror pinned to a version the
organisation has since moved past only warns, because the agent calls the
organisation's latest published version either way, checked fresh on every run;
a package with no mirror has nothing to compare and gets no warning either way.
An injected argument that does not fit the published parameters stops the run,
naming the tool, the argument, the version checked and the file that supplied it.

**Three things stop an `inject:` value, and none of them is "the value is
wrong".** The parameter is not declared, so an attachment has nowhere to bind it.
The parameter's type is not settled to a single scalar, so the platform will not
pin it: a parameter that could be a string or an integer, that declares no type,
or that holds an object or a list. Or a constraint sits at the root of the
published schema, where checking one supplied value against a rule written for
the whole call would reject the arguments the model fills in. Tell the user to
leave that argument to the model, and never tell them the destination has to be
text: SLNG stores a variable's value as text and converts it into the parameter's
declared type when the call starts, so an integer parameter takes a variable
whose stored value reads as a number.

"Settled to a single scalar" is read the way SLNG reads it, which is by narrowing
rather than by first answer: a parameter declaring two types and then constraining
itself to one of them under `allOf` is settled, and one declaring no type at all
but constrained to an integer is settled too. A parameter whose declared type and
its own constraints have no type in common is refused for having nothing to pin,
which reads differently from having too much.

**A dry run previews, and says what it cannot prove.** `--dry-run` names the old
and proposed published versions and whether the description or parameters
changed, and it names what a replacement would remove: an attachment setting only
the dashboard could have added, such as a description, a `call_start` trigger,
the arguments a system-invoked attachment carries, or a setting of a curated
capability that the package does not pin. A setting the package **does** pin is
compared rather than called lost, so pinning a timezone never reads as deleting
one. The sentence the agent speaks before a
tool runs is **not** on that list, because a package declares it with `announce:`:
the preview compares the two and says nothing when they match, so never tell a
user that deploying will delete an announcement their own tool file declares.
Never tell a user that a schema comparison proves a tool's behaviour is
unchanged: it proves only that the contract is, or is not, the same shape. A dry
run changes no live state, no authored file and no mirror.

## A curated capability attaches from the package

SLNG lists its curated capabilities as ordinary tools, so a package reaches one
by name and the push attaches it like any other. Both spellings resolve:

```yaml tools/current_datetime.yaml
builtin:
  id: current_datetime
```

```yaml tools/current_datetime.yaml
slng: current_datetime
```

The names, read 2026-08-31: `end_call`, `transfer_call`, `voicemail_detection`,
`current_datetime`, `user_phone_number`, `send_sms`. Four of them are in the
`builtin:` registry today; `.agents/skills/unmute/references/tools.md` has the
table, with the settings each one takes.

**Use `builtin:` when the capability takes a setting.** A setting is written with
`inject:` and reaches the attachment's configuration only through that block.
Under `slng:` an `inject:` becomes an *argument*, and a curated capability
publishes an empty argument schema, so validate refuses that combination and
names the `builtin:` spelling to use instead.

**Never tell a user to attach one in the dashboard instead.** A push replaces
rather than merges, so an attachment made by hand is detached again by the next
deploy. Attaching from the package is the only version that survives. A refusal
here is a filename or a manifest bucket, both below, and never the platform
declining.

### The builtin name rule

A `builtin:` tool's emitted reference carries the **file's** name, not the id it
selects. So `tools/hang_up.yaml` declaring `builtin: {id: end_call}` asks the
organisation for a tool called `hang_up`, which nobody has.

`unmute validate` refuses it on the slng target and names the rename, so it no
longer reaches a push:

```text
slng target: tool "hang_up" selects builtin "end_call", and this target attaches a builtin
  by the tool file's own name: rename tools/hang_up.yaml to tools/end_call.yaml
```

**Name the file after the capability.** A code target lowers the builtin to a
function and does not care what the file is called, which is why this refusal is
slng's alone.

### One name, held twice

SLNG publishes a capability to everybody and an organisation can hold its own
copy of the same name. `unmute deploy` prefers the organisation's, which is what
the dashboard attaches, and names both so the choice is visible:

```text
note: slng: this organisation holds "end_call" more than once: fd25f5c5-… was attached,
  and 952eb6b1-… at global scope was not. Rename one of them in the SLNG dashboard if the
  wrong one is running
```

A bare `voiceai agents push` prefers the other one, which is how a single package
has attached `end_call` v3 through `unmute deploy` and v1 through a direct push.
Read the note out when it appears.

### A governed model writes its provider

A manifest matches its model rules by provider, so an entry that declares none
matches no rule and every model it names is refused:

```text
models.reasoning.model: "gemini/google/gemini-3.1-flash-lite:latest" declares no provider:,
  and manifest "acme" matches its models.think rules by provider: write the provider this
  model is served by
```

`provider:` is optional in general, and a model id carrying its own vendor reads
fine without it. Under a manifest it is load-bearing. Write it on every governed
entry, fallbacks included.

## A manifest bucket follows the keyword, not the tool's nature

A curated capability is a real tool on the platform, so putting `current_datetime`
in `tools.builtin.allow` reads as the reasonable choice. It is refused, because
the bucket follows the keyword in the tool file:

- `slng:` → `tools.slng.allow`
- `builtin:` → `tools.builtin.allow`
- every tool, whichever keyword → `tools.names.allow`

A misfiled name produces a refusal that looks like a permissions problem, so read
the tool file before you argue with the manifest. The rest of the contract is in
the `unmute` skill, under `.agents/skills/unmute/references/manifests.md`.

## What the package cannot say, and says nothing about

`capacity:` and `channels:` are accepted by the schema and never reach the
compiled body. Nothing warns. They are code-target fields, so on slng they are
simply absent from the agent that deploys.

```yaml
channels:
  web:
    kind: realtime_audio
capacity:
  peak_sessions: 5
```

Read `build/slng/compile-report.json` → `notes` after every compile. It states
what the target did with the package, and silence there about a block somebody
wrote means the block did nothing.

## Credentials and the push tool

Unmute's compiler opens no connection to SLNG at any point. `unmute deploy` hands
the files to the `voiceai` CLI, which must be on PATH
(`brew install slng-ai/tap/voiceai`).

The key is read from `SLNG_API_KEY`, then `VOICEAI_API_KEY`, then whatever
profile `voiceai login` stored. Those are two names for one token: a single SLNG
key serves every SLNG role, including the Context Router key a generated livekit
or pipecat project reads at run time. `VOICEAI_API_KEY` is the name the push tool
itself reads.

A real push needs a `voiceai` release that supports a checked, resolved
attachment, verified with 0.1.18. `unmute deploy` checks for that support first:
an older `voiceai` is refused with upgrade guidance naming the install command,
rather than falling back to a push that resolves and attaches whatever is newest
without having checked it. Tell the user this if their run refuses early, naming
`--require-resolved`. `unmute validate` and `unmute compile` are unaffected
either way; neither reads the account at all.

**`unmute deploy` already pushes in guarded mode.** It passes
`--require-resolved`, so only the exact `tool_id` and version the staged package
carries is attached rather than re-resolving names at push time, and
`--expect-org`, which confirms the organisation before any write. Add both
yourself when you run `voiceai agents push` by hand, especially against a shared
organisation: without them the same tool name can resolve to a different version
depending on which command pushed.

```sh
voiceai agents push build/slng --require-resolved --expect-org <org-id> --dry-run
```

## What the refusals mean

A refusal blocks the agent push. A real deploy may already have refreshed MCP
discovery or filled a Vault entry with consent; `deploy-report.json` records
those changes. Report the problem and its fix: `vault missing`, a hosted tool the
organisation does not have, an argument that does not fit a published tool's
parameters, `agent ambiguous`, or an incompatible `voiceai`.

All of these come from `unmute deploy` reading the account or resolving a
reference, not from `validate`. `unmute deploy` compiles only the slng target,
and slng reads no mirror, so `no mirror of it is committed` and `does not match
the hash` never happen here. Those two belong to a `livekit` or `pipecat` compile
of the same package.

A model string SLNG does not have enabled for agents is rejected at push with
`AGENT_MODEL_UNAVAILABLE`, naming the field. Unmute cannot check this: the list
is per organisation.

### The 422 that names no field

```text
HTTP 422 · Voice agent config is invalid. Fix the highlighted fields and try again.
  · AGENT_VALIDATION_FAILED
```

No field is named, in the CLI output or in `deploy-report.json`, and there is no
verbose flag. The cause, every time it has been seen: a `{{placeholder}}` in the
prompt or the greeting naming a variable declared `source: conversation`.

`unmute validate` refuses it now, with the file and the line, so it should not
reach a push:

```text
agent.yaml:41: conversation.greeting.text references {{caller_email}}, a value the model
  records during the call, which no prompt receives: describe the value in the variable's
  description: and name it in prose here instead
```

A value the model records mid-call is emitted as a runtime variable and lands in
neither `template_defaults` nor `template_variable_options`, so a prompt naming
one references a template variable that does not exist. A variable with a
`default:` is a template variable and belongs in a prompt or greeting freely.
`.agents/skills/unmute/references/variables.md` has both.

If a 422 arrives anyway, diff the emitted `agent.json` against one that deploys.
Do not bisect by deleting parts of the package: isolating this once took sixteen
pushes.

### A refused push is not a no-op

The agent body is validated after tool attachment has begun, so a 422 can leave
attachments the agent never received:

```text
this push had already started writing, so some tools above exist on SLNG.
```

`deploy-report.json` records `"outcome": "partial"`. Say so, rather than implying
nothing happened.

**Never suggest creating a tool, an MCP server or a trunk from the CLI.** There is
no command for any of them. They are created in the SLNG dashboard, and
`unmute deploy` says so when one is missing. The one resource unmute writes is a
vault entry, and it offers that during a deploy.

**Never suggest passing a secret value on a command line.** `voiceai secret
create` has no `--value` flag on purpose: argv lands in shell history and is
visible in `ps`. The value is prompted for with the input masked, or piped on
stdin.

`unmute resources` lists the tools, MCP servers and phone numbers an organisation
offers, in the exact spelling a package must use. Suggest it before writing a
`builtin:`, `slng:` or `mcp:` name from memory.

## No pull, no samples

A `slng:` reference needs no hash, no mirror and no pull: `unmute deploy`
resolves it directly against the organisation. `unmute pull` matters only for a
package that also targets `livekit` or `pipecat`, which build and run the tool
themselves and so need a real copy of it; a slng-only package never runs it.

Do not pass `--run-samples`. A sample proves a tool before the platform publishes
it, and this push creates no tool: every tool the agent gets was published on
SLNG before the package named it. To exercise a hosted tool by hand, write an
input JSON file matching its published parameters, then run
`voiceai tool run <tool> --input samples/<tool>.json --confirm-side-effects`.

An MCP reference resolves by name at push time: the push looks up the server's
`server_id` and copies each tool's `observed_schema_hash` out of the platform's
own stored capability snapshot. A real deploy can refresh an unusable snapshot
once through `voiceai mcp run <server>` and recheck it. A dry run never refreshes
discovery; neither flow executes business tools as a check.

## Why the body cannot be posted directly

The emitted body carries a name where the API wants an id. SLNG's `tool_refs`
entries require `attachment_id`, `tool_id` and `version`; unmute writes a name
where the `tool_id` goes, because no compiler can invent an id a server assigns.
That is true of a curated builtin too.

The push step resolves those names, which is why `voiceai agents create --file
build/slng/agent.json` is the wrong command: it posts the body verbatim and the
API refuses it. `unmute deploy` resolves and checks references, then gives a
temporary resolved body to a guarded `voiceai` push. A direct push skips unmute's
binding checks.

Check the deployed name is free with `voiceai agents list` before the first push:
an SLNG name is unique across an organisation, and a push replaces the agent it
matches.

## Talking to the deployed agent

```sh
export VOICEAI_API_KEY="$SLNG_API_KEY"
cat > session.json <<'JSON'
{"arguments": {}}
JSON
voiceai agents web-sessions create <agent_id> --file session.json
```

The command takes the agent id `unmute deploy` printed, and a `--file` holding at
least `{"arguments": {}}`: every field in that body is optional in the API schema,
but a required input without a default must be supplied in `arguments`. It returns
LiveKit connection details, not a browser call. For a microphone test, open the
deployed agent in the dashboard and choose **Test**, then **Web session**.

## After a deploy, on a phone

For SLNG inbound phone calls, a required injected session input needs a valid
default: inbound dispatch has no web-session `arguments` payload. Otherwise SLNG
refuses trunk attachment with `AGENT_RUNTIME_COMPILATION_FAILED`. Leave tool
arguments that the caller supplies to the model instead. The
`examples/hotel-concierge` package requires no session inputs.

Telephony is verified on a deployed agent against a real carrier. There is no
local stand-in, and `unmute dev` is the browser loop only.

`unmute deploy` reports attached numbers after a successful push; it does not
verify carrier routing. Configure the number to send inbound calls to SLNG using
[SLNG Telephony setup](https://docs.slng.ai/dashboard/telephony). A number still
pointing at another webhook will not reach the agent. If no new SLNG call
appears, inspect carrier logs and SIP delivery first. When none does and an
inbound trunk is free, `unmute deploy` offers to attach one, which is a
single-field PATCH on the agent and happens only when an operator picks a number
at a terminal. A run with no terminal never attaches.

Do not put a trunk, a number or a `connection:` in a package for the slng target:
the compiler refuses it, the package stays portable, and attaching is an
operator's choice at deploy time. Creating a trunk and buying a number stay in
the SLNG dashboard.

`unmute deploy --call <e164>` places one outbound call from the agent, which
rings a real phone and costs a real call, so it happens only when it is asked for.

## Changing a deployed agent

Edit the package, compile again, deploy again. **Never edit `build/`**: the next
compile replaces generated files, preserving only `.env` and `samples/*.json`.

```sh
unmute validate ./my-agent
unmute deploy ./my-agent --dry-run
unmute deploy ./my-agent
```

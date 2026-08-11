# SPEC: variables and secrets

Source of truth for the schema: [SCHEMA.md](../SCHEMA.md) §4.4 (variables), §5 (tools), D12 (secrets never in files). When code and SCHEMA.md disagree, SCHEMA.md wins (CLAUDE.md). This spec plugs into the compiler core and the two shipped drivers; it does not restate them:

- [compiler.md](compiler.md): `spec.Load → ir.Build → ir.Validate`, the IR types, the capability table.
- [driver-livekit.md](driver-livekit.md): the LiveKit emitter.
- [driver-pipecat.md](driver-pipecat.md): the Pipecat emitter.

Worked examples: [examples/salon-support/](../../examples/salon-support/) is the runnable one (web audio, local tools, two model keys — `validate`, `compile`, `dev`, talk to it), and [examples/outbound-reminder/](../../examples/outbound-reminder/) is the same surface on a real outbound phone call, which needs Twilio and a booking API.

## §G goal

A voice agent runs on four kinds of runtime values, and today only half of them have a full path from `agent.yaml` to a running call:

1. **Input variables**: given by the caller of the system when the call starts. An outbound reminder is dispatched with `customer_id` and `name`; the greeting says "Hi {{name}}", the prompt says "you are calling {{name}}", and a booking tool sends `customer_id` without the model ever seeing or inventing it.
2. **System variables**: owned by the runtime. The dialed number, the call id, the carrier. Already in the schema as `source:` values (SCHEMA 4.4).
3. **Conversation variables**: learned by the model during the call. An inbound agent asks who is calling, saves `name` and `callback_number`, and later tools and transfers use them like any other variable.
4. **Secrets**: runtime environment values. An API bearer token, a webhook base URL, a model key. Declared by name, never by value (D12), and consumed by tool auth, webhook URLs, and local Python handlers.

The goal is one small authoring surface for all four: a `variables:` block (already exists, extended) and a `secrets:` block (new), plus one template syntax `{{name}}` that works in the greeting, in prompts, in hidden tool parameters, and in webhook URL paths. A variable is declared with a description and an optional default, a secret with nothing but its name, so `unmute` can validate references at compile time, generate a `.env.example`, check required env at startup, and give the model a typed way to save what it learns.

**Naming decision.** The request proposed "system variables and LLM variables" as two blocks. This spec keeps **one `variables:` block** and puts the origin in the existing `source:` field instead, because origin is a property of a variable, not a different kind of thing: every consumer (`{{name}}` templates, `assign:`, `requires:`, tool injection) works on any variable no matter where its value came from, and a variable that changes origin (a `name` that is dispatched on outbound but asked for on inbound) should not have to move between blocks. The prose vocabulary is: **input variables** (`source: call_start`), **system variables** (the runtime-owned sources, already the schema's term), **conversation variables** (`source: conversation`, new). "LLM variables" is not schema vocabulary: every `source:` value names where the value comes from (the call start, the carrier, the conversation), not which component writes it. Secrets stay a separate block because they are a different thing entirely: env names with no type, no default, no template access, and a hard rule that values never appear in any file.

## §C constraints

- C1: **One `variables:` block.** No second variables block, no new top-level kind. `source: conversation` is one new enum value on the existing field (SCHEMA 4.4). Old files keep loading unchanged.
- C2: **Flat typed data stands** (D9, N7). Conversation variables use the same four primitives (`string`, `number`, `boolean`, `integer`). No nested capture in v1.
- C3: **Secrets are declared, never valued** (D12 unchanged). The `secrets:` map key **is** the env var name, `UPPER_SNAKE`, same rule as every `*_env` field (N19). No `env:` indirection field and no `value:` field exists to misuse. A `default:` or `example:` field is deliberately absent: anywhere a value could be written, one day a real one will be.
- C4: **Secrets never flow through templates.** `{{...}}` resolves against variables only. SCHEMA 4.4 already says this for Deepgram ("never route secrets through template variables"); this spec makes it a package-wide rule on every target. Secrets reach the call only through the existing `*_env` slots and the generated auth helpers, and through `os.environ` in local Python handlers.
- C5: **One template syntax, four sites, two render times.** Syntax: `{{name}}`, optional inner spaces, `name` is a declared variable. Sites: `conversation.greeting.text`, agent and task instructions markdown, tool `inject:` values, `webhook.path`. Greeting and instructions render once at session start; `inject` and `path` render at each tool call. No other file takes templates (models, targets, connections stay literal). No escape syntax in v1; a `{{` that does not name a declared variable is a compile error, which also catches typos loudly (D3).
- C6: **Capture is one generated tool.** When a package declares any `source: conversation` variable, drivers emit one tool named `update_variables` whose input schema is exactly the conversation variables (name, type, `description`), all optional, and attach it to every agent and task. It writes the shared state both drivers already have (Pipecat `State`, LiveKit `userdata`). This is a documented exception to D8 (an agent's callable set is its `tools:` list): the tool is generated plumbing, like the `requires` guard, not a package tool. The name `update_variables` is reserved; a user tool or control with that name fails with both names cited.
- C7: **Additive only.** Existing packages compile with identical output. The env cross-check (V10) is a warning, never an error, because no existing package has a `secrets:` block. Warnings go to stderr with exit 0 (repo rule).
- C8: **Go only in this repo; drivers emit Python** (compiler C4). Emitted Python passes ruff and ty like all generated code. The template renderer in emitted code is a small regex substitution, no Python templating dependency.
- C9: **Managed and unwritten targets gate, never degrade** (D3, D4). Vapi has no place for generated merge or capture logic until its native equivalents are doc-verified; Deepgram's driver is unwritten. New capability fields fail there with the provider named. Verify rows in §9-style table below lift them later.
- C10: **No TUI editing in v1.** The create wizard does not gain variable or secret editors; `unmute init` scaffolds neither block. Authoring is by hand against SCHEMA.md and the example package. A TUI editor is a follow-up once the shape has survived real use.
- C11: **Values captured mid-call do not re-render prompts.** Greeting and instructions are rendered once, at session start, with the values known then (input variables, system variables, and any variable's `default`). A conversation variable referenced there must carry a `default` so there is something to say. Live values reach the model through tool results and its own context, and reach tools through call-time rendering.

## §I surfaces

- I.authoring.variables: `agent.yaml`, existing block, two additions (`description`, `source: conversation`):

  ```yaml
  variables:
    customer_id:
      type: string
      source: call_start          # input variable: arrives with the dispatch
      description: CRM id of the customer this call is about.
    dialed_number:
      type: string
      source: to_number           # system variable: runtime owned (existing)
    reschedule_to:
      type: string
      source: conversation        # conversation variable: the model saves it
      description: New slot the customer asks for, in spoken form.
  ```

  `description` is legal on every variable. It feeds the generated capture tool schema, the dispatch section of the generated README, and the compile report. Any variable, whatever its source, may still be an `assign:` target and a `requires:` guard exactly as today.

- I.authoring.secrets: `agent.yaml`, new top-level block. A list of env var names:

  ```yaml
  secrets:
    - SALON_API_URL
    - SALON_API_TOKEN
    - OPENAI_API_KEY
  ```

  No fields. The name is the whole declaration, every listed name is required, and a repeat is an error.

- I.authoring.tool: `tools/<name>.yaml` gains one top-level field and one webhook field:

  ```yaml
  description: Move the appointment to the slot the customer asked for.
  input:
    type: object
    properties: {}                # the model has nothing to fill in

  inject:                        # merged into the call, invisible to the model
    customer_id: "{{customer_id}}"
    new_time: "{{reschedule_to}}"
    channel: phone               # literals are legal too

  webhook:
    url_env: SALON_API_URL
    path: /customers/{{customer_id}}/appointments   # appended to the env base URL
    auth:
      type: bearer
      token_env: SALON_API_TOKEN
  ```

  `inject` is a flat map of request key to a scalar. A string value may contain `{{name}}` tokens. A value that is exactly one token keeps the variable's declared type (an integer variable injects a JSON number); anything else renders to a string. Injected keys must not appear in `input.properties`, so the model can never see or override them. `inject` is legal on `webhook` (merged into the POST body) and `local` (merged into the handler's kwargs); it is an error on `builtin`, `client`, `provider_hosted`, and **`mcp`** (corrected during implementation: an MCP tool's arguments are assembled by the MCP client from the server's own schema, and neither driver has a hook to add a hidden argument — LiveKit mounts one `MCPServerHTTP` per URL with `allowed_tools`, nothing per call. Allowing it would have meant silently dropping the value, so it gates until an SDK hook exists; see the verify table). `path` is webhook-only, must start with `/`, is appended to the trimmed env base URL, and rendered variable values are URL-encoded.

- I.templates.render-times: greeting and instructions render at session start from input variables, system variables, and defaults (C11). `inject` and `path` render at each call from the live state, so conversation variables work there with no default. A call-time render that hits an unset variable makes the generated tool **refuse instead of send**: it returns an error result to the model naming the variable and its description ("cannot call reschedule_appointment yet: reschedule_to is not set. Ask the caller for it first.") and no request leaves the machine. Same contract as the `requires` guard refusal.

- I.dispatch: how input variables get their values. The wire format is one flat JSON object of variable name to value, checked against declared types at call start (a mismatch fails the call setup with the variable and expected type named).
  - LiveKit: the job dispatch metadata (`ctx.job.metadata`) parsed by the generated worker.
  - Pipecat: the runner's call-start payload, next to where `normalized_context` already reads telephony facts.
  - Dev: `unmute dev --var name=value` (repeatable) seeds the session for web, console, and telephony dev runs; the value is parsed per the declared type.
  - Unknown keys in a dispatch payload are logged and ignored, never fatal mid-dispatch. Missing input variables follow the existing satisfiability rules (inbound `call_start` needs a `default`; outbound is satisfied by the dispatch payload).
  - The generated README documents the exact dispatch spelling per target with the package's own variable names.

- I.spec.types (`internal/spec/package.go`): `Variable` gains `Description string`. `AgentFile` gains `Secrets map[string]Secret`; new `type Secret struct { Description string; Required *bool }`. `Tool` gains `Inject map[string]any`; `ToolWebhook` gains `Path string`. jsonschema-go re-derives the authoring schema from the structs (compiler C2), no hand-authored JSON.

- I.ir.types (`internal/ir/compiler.go`): mirror the spec fields; add `VariableSourceConversation VariableSource = "conversation"`. `ir.Build` scans all four template sites and rejects a bad reference there. The parse itself lives in one exported place, `internal/ir/template.go` (`ParseTemplate`, `TemplateRefs`, `HasTemplate`, `TemplateVar`), which Build and both generators call. **Changed from the first draft:** the plan was to store each site's resolved reference list on the IR so drivers never re-parse. That was dropped for the shared parser, because a driver does not want a reference *list* — it wants the ordered literal-and-variable segments, to decide per value whether it reads a state attribute directly (keeping its declared type) or renders through a helper. Storing a second, lossier view of the same strings next to the strings would be two things to keep in step; one exported parser cannot disagree with itself.

- I.capability (`internal/target/table.go`): four new fields, core on LiveKit and Pipecat, deny on Vapi and Deepgram (C9):
  `FieldVariableConversation "variables.source.conversation"`, `FieldToolInject "tools.inject"`, `FieldWebhookPath "tools.webhook.path"`, `FieldTemplates "templates.session_start"` (greeting and instructions templates). The `secrets:` block itself has no capability field: declaring env names is core everywhere, only lowerings differ.

- I.validate (`internal/ir/validate.go`): the rules in §V, all with file:line via `Package.Location`, provider gates in the provider's own words (compiler C6).

- I.capture: the generated `update_variables` tool (C6). Description: "Save details the caller gives you, as soon as you learn them." plus one line per variable from its `description`. Input schema: one optional property per conversation variable, `additionalProperties: false`. Returns the saved names so the model gets confirmation. Unset means no value was ever provided; a declared `default` counts as a value; in emitted Python an unset conversation variable is `None` (fields are `Optional`), which is also what the `requires` guard and call-time refusal test.

- I.pipecat.lowering: conversation variables become `State` fields defaulting to `None` (or the declared default); `update_variables` is one function-tool writing `self.state`; `render(text, state)` is a module-level helper (regex `{{\s*([a-z_][a-z0-9_]*)\s*}}`) applied to the greeting `TTSSpeakFrame` text and to each agent's and task's instructions at session start; the webhook helper merges rendered `inject` pairs into the body and appends the rendered, URL-encoded `path` to the base URL; the existing missing-env startup check (`bot.py` already raises on missing required env) extends to every declared secret.

- I.livekit.lowering: same shape on LiveKit vocabulary: conversation variables are `userdata` fields, `update_variables` is an `@function_tool` on every agent and task, `render` applies to `session.say`/greeting emission and instructions at construction, webhook helper and startup check as on Pipecat, dispatch metadata parsed into `userdata` at job start.

- I.artifacts: every code-target build writes `build/<target>/.env.example`: each declared secret as a bare `NAME=` line, sorted; then one labeled group holding the resolved Connection's env names and any referenced-but-undeclared ones (V10). The compile report gains a `variables` section (name, type, source, has-default, description) and a `secrets` section (name, referenced-by).

- I.schema.doc: SCHEMA.md amendments, landing with the feature (repo rule: doc wins):
  - New decision **N23** (variables): `description` field, `source: conversation`, the template contract (sites, render times, refusal), the generated `update_variables` tool and its D8 exception, C11's no-re-render rule.
  - New decision **N24** (secrets): the block, key-is-env-name, never-in-templates, `.env.example`, startup check, the cross-check warning.
  - §4.1 top-level table: `secrets` row (no, map, core). §4.4: new fields and source value. New §4.12: the secrets block table. §5.1: `inject` row. §5.2: webhook `path`. §7 matrix rows: conversation variables, templates, inject, path (ok on LiveKit and Pipecat, fail on Vapi and Deepgram).

## §V invariants

- V1: every `{{token}}` in any template site names a declared variable; anything else fails with file:line. If the token matches a declared secret or is `UPPER_SNAKE`, the error says secrets never flow through templates (C4).
- V2: a template in `greeting.text` or in instructions only references variables with a session-start value: `source: call_start`, a system source, or any variable with a `default`. A conversation variable without a default referenced there fails with file:line (C11).
- V3: `inject` keys do not collide with the tool's `input.properties`; a collision fails naming the tool, the key, and both sites. `inject` on a `builtin`, `client`, `provider_hosted`, or `mcp` tool fails, naming the kind. `webhook.path` starts with `/` and is only legal inside `webhook:` (strict decode gives the second half for free).
- V4: a call-time render (inject or path) that hits an unset variable refuses: the model gets an error naming the variable and its description, and no request is sent. Goldens pin the refusal text on both drivers. The check covers only variables the model or the dispatch can actually supply — a **system** variable is excluded (B2), because a refusal is an instruction to the model to go and ask, and no caller can be asked for the dialed number. A system source missing on a route that owns it already fails loudly at session start, so it cannot vanish quietly either way.
- V5: `source: conversation` resolved onto a target whose `FieldVariableConversation` is deny fails in that provider's own words; same for `FieldToolInject`, `FieldWebhookPath`, and `FieldTemplates` (C9).
- V6: when at least one conversation variable exists, both shipped drivers emit `update_variables` attached to every agent and task, its schema listing exactly the conversation variables with their types and descriptions, `additionalProperties: false`. With zero conversation variables the tool does not exist anywhere in the output.
- V7: `update_variables` is reserved: a user tool or control with that name fails with both names cited (C6). The generated tool lives in the one tool/control namespace (compiler C10).
- V8: secret entries are valid `UPPER_SNAKE` env names; anything else fails with file:line naming the entry, as does the same name listed twice. `secrets:` is a list of names and carries no fields (strict decode).
- V9: no package file ever contains a secret value: the `Secret` struct has no value-shaped field to write (C3), and `*_env` fields keep rejecting non-env-name values (N19).
- V10: an env name referenced by the package (tool `url_env`, `token_env`, mcp `url_env`, model `endpoint_env`, the tracing trio when `tracing:` is present) but not declared in `secrets:` produces one warning listing each name and the file that references it, stderr exit 0, never an error (C7). Connection env names are exempt (declared in their own file). Declared-but-unreferenced secrets are silent (a local handler may read them in Python).
- V11: `build/<target>/.env.example` lists every declared secret by name, then the resolved Connection's env names and any referenced-but-undeclared ones, labeled once as a group; output is deterministic (sorted).
- V12: generated runtimes fail startup when a declared secret is missing or empty, naming it, on both shipped drivers (same contract as the tracing env check, SCHEMA 4.11).
- V13: a dispatch value whose type does not match the declared variable type fails call setup naming the variable and the expected type; unknown dispatch keys are logged and ignored. `unmute dev --var` parses values by declared type and rejects undeclared names before starting.
- V14: an injected value that is exactly one `{{token}}` keeps the variable's declared type in the emitted request; any other string renders to a string; path segments are URL-encoded. Goldens pin all three.
- V15: templates render once at session start for greeting and instructions, per call for inject and path; no re-render machinery exists in the output (C11).
- V16: a package that declares no secrets and writes no template emits no secret block, no `.env.example` descriptions, no render helper, no refusal helper, and no capture tool: those all stay behind their own conditions (C7). One thing does change for a package that merely has `variables:` — it gains the dispatch path (`_dispatched_call_start`, and the LiveKit metadata reader), because `unmute dev --var` has to reach a variable whose `source:` is left unset, and accepting the flag then ignoring the value is the silent no-op the repo forbids. The path is inert: it reads one optional environment variable and changes nothing when that is absent. Byte-identical output is therefore promised for a package with no `variables:` block at all, and the existing goldens record the added path for the rest.
- V17 (added 2026-08-10, B1): the four-target `safe_core` fixture uses none of this feature — no template in any prompt or greeting, no `inject`, no `webhook.path`, no `source: conversation` variable — because each is gated on Vapi and Deepgram. Its job is to validate clean on all four (compiler V14), so it cannot carry a feature two targets refuse. A test asserts the absence, so the syntax cannot creep back in and quietly narrow the fixture's reach.

## §T tasks

id|status|desc|cites
T1|x|`spec`/`ir` types: `Variable.Description`, `source: conversation`, `Secrets` block, `Tool.Inject`, `ToolWebhook.Path`; schema re-derivation; strict decode still rejects unknown fields|I.spec.types,I.ir.types,V8,V9
T2|x|template parser in `internal/ir/template.go` (shared by Build and both drivers) plus the Build-time scan of all four sites; unknown-token and secret-in-template errors|I.ir.types,V1
T3|x|`ir.Validate`: session-start legality (V2), inject/path shape rules (V3), reserved `update_variables` name (V7), secret key shape (V8)|V2,V3,V7,V8
T4|x|capability fields `variables.source.conversation`, `tools.inject`, `tools.webhook.path`, `templates.session_start`: core LiveKit+Pipecat, deny Vapi+Deepgram; table test rows|I.capability,V5
T5|x|env cross-check warning + compile-report `variables`/`secrets` sections|V10,I.artifacts
T6|x|Pipecat lowering: `render` helper, `Optional` state fields, `update_variables` tool, inject/path merge + refusal, startup secret check; goldens|I.pipecat.lowering,V4,V6,V12,V14,V15
T7|x|LiveKit lowering: same six pieces on `userdata`/`@function_tool`; goldens|I.livekit.lowering,V4,V6,V12,V14,V15
T8|x|dispatch: `unmute dev --var` (typed parse, undeclared rejected), LiveKit metadata parse, Pipecat call-start payload parse, generated README dispatch section|I.dispatch,V13
T9|x|`.env.example` emission per code target, deterministic, connection names included|I.artifacts,V11
T10|x|SCHEMA.md amendments N23+N24: §4.1 row, §4.4, new §4.12, §5.1 `inject`, §5.2 `path`, §7 matrix rows|I.schema.doc
T11|x|lift the `outbound-reminder` skip in `examples_test.go` (the example ships spec-first, skipped by name in `TestPublicExamplesValidateAndGenerate`) and wire it into the compile matrix once T1..T9 land; byte-identical check for pre-existing examples|V16
T12|x|L4 smoke: emitted `update_variables` + a templated webhook call round-trip on both drivers (opt-in, `make smoke`)|V4,V6

## §B bugs

id|date|cause|fix
B3|2026-08-10|The Pipecat telephony builder classified every variable with a non-empty `source` as a runtime-owned system source, so a `source: conversation` variable joined the emitted `build_state` system-source loop and the bot raised `Missing call context fields: conversation` at session start — every telephony call would have failed before the greeting. `ir.buildTelephonyPlan` had already been narrowed with `IsSystemSource`, but the driver kept its own copy of that predicate, so the fix reached the IR and not the emitted Python. Found by the L4 smoke (T12) on the first run, invisible to every L1–L3 test because none asserted the contents of that loop|V5, V6; the driver calls `ir.IsSystemSource` instead of testing `source != ""`, and the smoke asserts a conversation variable never appears in the emitted call-context check
B2|2026-08-10|V4's refusal check covered every referenced variable without a default, including system ones. The worked example injects `dialed_number` (`source: to_number`), which no web session has, so `confirm_appointment` refused every call in the browser with "ask the caller first: dialed_number" — asking the caller to supply the number they dialed. Root cause: the refusal was specified against "unset" when the useful predicate is "unset and obtainable"; a runtime-owned value is neither the model's nor the caller's to provide|V4 narrowed to variables the model or dispatch can supply; `neededVars` skips system sources, and a test pins that a system-source injection does not gate a tool call
B1|2026-08-10|`internal/testdata/safe_core/agents/billing.md` carried `{{customer_id}}` in an agent prompt, written as pseudo-syntax back when `{{...}}` meant nothing and was passed to the model as literal characters. Giving the syntax meaning turned that line into a real template on the one fixture that must validate clean on all four targets, and it failed twice over: `customer_id` there has no source and no default, so it has no session-start value (V2), and templates are gated on Vapi and Deepgram (V5). Root cause: a fixture used a syntax the schema had described but never interpreted, so nothing tied prompt text to the feature's own gates|V17; safe_core drops the template syntax (the line names the tool, not a placeholder), and V17 keeps every gated field out of the four-target fixture

## Verify before these harden

Same contract as SCHEMA §9: these change tags, never the shape. Each row lifts a deny gate when doc-verified (context7 or official docs, dated), per the provider-docs rule.

| Item | Field affected | Today's stance |
|---|---|---|
| Vapi: call-start variable injection and template spelling in `firstMessage` and the system prompt (their dynamic-variable mechanism, exact key names unverified here) | `FieldTemplates`, input variables on Vapi | fails |
| Vapi: mid-call variable capture and server-side tool parameter injection (whether a native equivalent of `inject` exists) | `FieldVariableConversation`, `FieldToolInject` on Vapi | fails |
| Deepgram: bridge-held state for capture and injection; SCHEMA 4.4 already warns its template variables are substitution-time and member-visible | all four fields on Deepgram | fails until the driver ships (driver-deepgram §T) |
| `inject` on an `mcp` tool: whether either SDK exposes a per-call argument hook on a mounted MCP server (LiveKit `MCPServerHTTP`, Pipecat's MCP client). Nothing found in either, so the value would be dropped | `inject` on mcp, every target | fails; a tool needing hidden parameters uses `webhook:` or `local:` |

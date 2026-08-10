# SPEC — prebuilt tools (the `builtin:` block)

Source of truth for the schema: [SCHEMA.md](../SCHEMA.md) §4 (tools). When code and SCHEMA.md disagree, SCHEMA.md wins (CLAUDE.md). This spec plugs into the compiler core and the two code drivers; it does not restate them:

- [compiler.md](compiler.md) — `spec.Load → ir.Build → ir.Validate`, the IR types (I.ir.types), and the capability table (I.capability).
- [driver-livekit.md](driver-livekit.md) — the LiveKit emitter.
- [driver-pipecat.md](driver-pipecat.md) — the Pipecat emitter.

## §G goal

Let a user pick a provider's prebuilt tool by name instead of hand-authoring a handler for it. Today the only way to end a call from a tool is to write a real `webhook` or `local` tool and flag `effect: ends_conversation` (compiler `ToolEndsConversation`). Both code drivers already ship a ready-made end-call primitive: LiveKit's beta `EndCallTool`, and Pipecat's `end_conversation` function pattern. A user should be able to select one and, at most, customize its wording.

The mechanism is the already-reserved builtin execution kind (a top-level `execution: builtin` until N19 made it the `builtin:` block, 2026-08-10) (SCHEMA.md §4, gate note: "gated per driver; each driver documents what it can host"). This spec is that documentation. It defines one closed **registry** of known prebuilt ids. v1 ships exactly one id, `end_call`, proven on LiveKit and Pipecat. Adding a second prebuilt later is one registry row plus a per-driver lowering, with no new authoring surface. That is the scalable shape the request asked for.

## §C constraints

- C1: Reuse the builtin execution kind (the `builtin:` block since N19). It already exists in the `ToolExecution` enum (`internal/ir/compiler.go`), has a capability field `FieldToolBuiltin` (`internal/target/table.go`), and is documented but `deny`'d on every provider today ("not proven by its driver"). No new top-level authoring key, no new execution kind.
- C2: Go only in this repo; drivers emit Python. The registry is Go data in the leaf package `internal/target` (imported by both `ir` for validation and `generate` for lowering, same rule as the capability table, compiler C4). The Python strings live in each driver, keyed by prebuilt id.
- C3: No new dependency. LiveKit's prebuilt is `livekit.agents.beta.tools.EndCallTool` (BETA). The LiveKit driver already imports `livekit.agents`; this is a submodule, not a new package. driver-livekit.md flags the beta status and pins the `livekit-agents` version where its plugins are pinned.
- C4: A prebuilt tool owns its own schema. A `builtin` tool carries no `input`, `output`, `handler`, or `url_env`. Strict decode (compiler C3) rejects any of those on a `builtin` tool with file:line.
- C5: The registry is the single source for each id's default description and implied `effect`. Provider gating rides the existing `FieldToolBuiltin` capability field (flipped to allow on LiveKit and Pipecat). The accepted param set is a closed set of typed struct fields (`description`, `instructions`), so strict decode (compiler C3) rejects any other key as "unknown field" for free, no registry param-list check needed. For v1 the sole id's provider support equals `FieldToolBuiltin`, so the field is the provider gate. A future prebuilt whose support diverges from the coarse field (a LiveKit-only id) is when per-id registry-driven provider gating gets added, not before (YAGNI).
- C6: v1 registry has one row. `end_call` accepts two portable params, `description` and `instructions`, both optional, both map cleanly to LiveKit and Pipecat. LiveKit-only knobs (`delete_room`) are deferred, not exposed (decision 2026-07-22).
- C7: Naming. `tools/<name>.yaml` file name is the user's tool name as always (I.package, C10 shared namespace). The `builtin:` field names the registry id, decoupled from the file name, so a user may call the tool `hang_up` while selecting `end_call`.
- C8: `effect` for a builtin tool is fixed by the registry, not the user. `end_call` implies `ends_conversation`. Omitting `effect` is correct; stating the matching value is allowed; stating a conflicting value is an error. The resolved IR effect is what the existing shutdown machinery reads, so no downstream change is needed for the "ends the call" behavior.
- C9: `end_call` is scaffolded by default. Both `unmute init <name>` (non-interactive, always the default pipecat target) and the TUI create wizard build a `scaffold.Data` that flows through `scaffold.Write`, so the default lives in the scaffold layer as one function, `scaffold.DefaultTools()`, seeded explicitly at both entry points. Seeding at the entry points (not inside `withDefaults`) is deliberate: the TUI user must be able to edit or remove it, and a removal must stick rather than be re-injected at write time. Non-interactive `init` has no target flag, so its package always validates green with the default present. The TUI create/maintain target picker calls `dropUnsupportedBuiltins` after `SetTarget`: switching to a target that denies `builtin` (Vapi, Deepgram) drops the seeded default rather than leaving a guaranteed validation failure. This lives in the wizard target-change handler, not in `SetTarget` (which many unit paths call directly), and it does not re-add on switching back (the user re-adds it if wanted). It removes prebuilt tools only, never user webhook/local tools.

## §I surfaces

- I.authoring: `tools/<name>.yaml`:
  ```yaml
  # tools/hang_up.yaml   (name = file name; referenced from tools: like any tool)
  builtin:
    id: end_call
  description: End the call once the caller's issue is resolved.   # optional, extra when-to-end guidance
  instructions: Thank the caller and say goodbye warmly.           # optional, goodbye message
  ```
  Referenced from `agents.<a>.tools` / `tasks.<t>.tools` exactly like a webhook tool.
- I.spec.Tool / I.ir.Tool: add `Builtin string` (the id) and `Instructions string` (goodbye) to both `spec.Tool` (`internal/spec/package.go`) and `ir.Tool` (`internal/ir/compiler.go`). `Description` stays, becomes optional for builtin. jsonschema-go re-derives the authoring + debug schema from the structs (compiler C2); no hand-authored JSON.
- I.registry: `internal/target/prebuilt.go` — `Prebuilt` table `id → {DefaultDescription, Effect}` with `LookupPrebuilt(id string) (Prebuilt, bool)`. Read by `ir` (validate/build) and `generate` (lowering). v1: `end_call → {Effect: "ends_conversation", DefaultDescription: "End the call when the caller is finished or says goodbye."}`. Params are typed struct fields, not a registry list (C5).
- I.capability: flip `FieldToolBuiltin` from `deny` to `core` for LiveKit and Pipecat in `internal/target/table.go`; keep `deny` on Vapi, Deepgram (unchanged). This is the provider gate (C5). `validateTools` keeps applying it for `ToolBuiltin`.
- I.livekit.lowering: `builtin: {id: end_call}` lowers to `from livekit.agents.beta.tools import EndCallTool`, an `EndCallTool(extra_description=<description>, end_instructions=<instructions>)` constructed with only the set params, and its `.tools` spread into the agent's (or task's) `tools=[...]` list next to any `@function_tool` methods. It does not touch the webhook/local path or emit a handler.
- I.pipecat.lowering: `builtin: {id: end_call}` lowers to a bodyless end tool over the existing `EndsCall` path (`push_frame(EndFrame())`, `bot.py.tmpl:218`, already proven by cold human-transfer which emits `URLEnv: "", Args: nil, EndsCall: true`). The tool docstring is `description`; `instructions` is the developer/goodbye message, matching the referenced `end_conversation` example. No `url_env`, no handler.
- I.scaffold.default: `scaffold.DefaultTools()` returns the seeded defaults, v1 = one `end_call` builtin tool (`Name: end_call`, `Execution: builtin`, `Builtin: end_call`, a default `Description`), attached to the entry agent (the default `AttachTo` of the assistant). `scaffold.Tool` gains `Builtin` and `Instructions` fields; `tool.yaml.tmpl` branches on the builtin kind to render the `builtin:` block (`id:` plus an optional `instructions:`) with an optional top-level `description` and no `input`/`interruption`/`effect` (block shape since N19, 2026-08-10).
- I.init.default: `cli.newInitCmd` (non-interactive `init <name>`) seeds `scaffold.Data{..., Tools: scaffold.DefaultTools()}`. Always the default pipecat target, so the emitted package validates green.
- I.tui: the create wizard (`tui.runCreate`) seeds its initial `scaffold.Data.Tools` with `scaffold.DefaultTools()` so `end_call` shows in the Tools list and is editable/removable via the existing `editTools` flow. `editTools` gains handling for a builtin tool: pick a registry id, edit the optional `description`/`instructions`, no `input`/`url_env`/`handler` prompts. The "Add prebuilt tool" path lists registry ids.
- I.schema.doc: SCHEMA.md §5 documents the `builtin:` block (`id`, optional `instructions`) and states `description` is optional for a builtin tool. SCHEMA.md wins over code, so this row lands with the feature (C4 of compiler).

## §V invariants

- V1: a `builtin` tool's `builtin:` value is set and is a known registry id. Missing or unknown id fails with file:line naming the tool.
- V2: `builtin:` and `instructions:` are legal only on a `builtin` tool. Either present on a non-builtin tool fails with file:line.
- V3: a `builtin` tool carries none of `input`, `output`, `handler`, `url_env`. Any present fails with file:line (C4). `description` is optional (registry default fills it).
- V4: `FieldToolBuiltin` is `core` on LiveKit and Pipecat and `deny` on Vapi, Deepgram. A builtin tool resolved onto a denying target is a gated error in that provider's own words (compiler C6). This is the provider gate (C5).
- V5: the resolved `ir.Tool.Effect` of a builtin tool equals the registry `Effect` for its id (`end_call → ends_conversation`). A user `effect:` that conflicts fails with file:line; a matching or absent one resolves to the registry value. The existing shutdown path (`EndsConversation` on LiveKit, `EndsCall` on Pipecat) consumes this unchanged.
- V6: the accepted param set is the closed set of typed fields (`description`, `instructions`). Any other tool key on a builtin tool fails strict decode as "unknown field" with file:line (compiler C3), no registry param list needed.
- V7: goldens pin both lowerings. LiveKit golden shows the exact `from livekit.agents.beta.tools import EndCallTool` import, the constructor with only the set params, and `.tools` spread into the tools list. Pipecat golden shows a bodyless end tool with no `url_env`/handler and the `EndFrame()` end. A builtin tool with no params still lowers on both.
- V8: a `builtin` tool lives in the one tool/control namespace (compiler C10). It is referenced, name-uniqueness-checked, and reserved-underscore-checked exactly like any tool. An `agent`/`task` referencing an undefined builtin tool fails as any missing reference does.
- V9: `scaffold.DefaultTools()` yields an `end_call` builtin tool attached to the entry agent. A non-interactive `unmute init <name>` writes `tools/end_call.yaml` with a `builtin:` block whose `id` is `end_call`, wires `end_call` into the entry agent's `tools:`, and the emitted package validates green on the default (pipecat) target.
- V10: the seeded default is editable and removable in the TUI create flow. The default is seeded once at wizard start, never re-injected at write, so a user who removes it and creates the agent gets a package with no `end_call` tool.

## §T tasks

id|st|task|cites
T1|x|`spec.Tool` + `ir.Tool` gain `Builtin` and `Instructions`; `Description`/`Input` optional (omitempty) for builtin; strict decode still rejects unknown fields (compiler V3)|I.spec.Tool,V3
T2|x|`internal/target/prebuilt.go` `Prebuilt` registry: `end_call` row (default description, effect); `LookupPrebuilt`|I.registry,C5
T3|x|`internal/target/table.go`: `FieldToolBuiltin` → core on LiveKit+Pipecat, deny elsewhere; table_test updated|I.capability,V4
T4|x|`ir.Build`/`ir.Validate`: resolve `builtin` id against registry (V1), builtin/instructions builtin-only (V2), forbid input/output/handler/url_env (V3), fix effect from registry + reject conflict (V5); param set closed by struct fields + strict decode (V6)|V1,V2,V3,V5,V6
T5|x|LiveKit lowering: `builtin: end_call` → beta `EndCallTool` import + constructor + `.tools` spread into super().__init__ (agents + tasks); golden test + emitted-fields flag|I.livekit.lowering,V7,C3
T6|x|Pipecat lowering: `builtin: end_call` → bodyless end tool over the `EndsCall`/`EndFrame()` path (agent @tool + flows handler); golden test + emitted-fields flag|I.pipecat.lowering,V7
T7|x|SCHEMA.md §5: the `builtin:` block fields, `description`/`input` optional for a builtin tool, execution gating note updated|I.schema.doc,C1
T8|x|`scaffold.Tool` gains `Builtin`+`Instructions`; `tool.yaml.tmpl` branches on the builtin kind; `scaffold.DefaultTools()` returns the `end_call` default; scaffold_test added, bare-Data goldens unchanged|I.scaffold.default,V9
T9|x|Seed `DefaultTools()` at both entry points: `cli.newInitCmd` non-interactive path and `tui.runCreate`; init_test asserts `tools/end_call.yaml` + entry-agent wiring + green compile|I.init.default,I.tui,V9,V10
T10|x|TUI: `builtin` added to the execution picker; `editTool` builtin branch (description/goodbye/prebuilt id, no input/url/handler); `dropUnsupportedBuiltins` on target change; reconciliation test green|I.tui,V10,C9
T11|x|L4 smoke: LiveKit + Pipecat `TestSmoke...BuiltinEndCall` import + instantiate the emitted end_call (opt-in, `make smoke`; compiles under `-tags smoke`)|V7

## §B bugs

id|date|cause|fix

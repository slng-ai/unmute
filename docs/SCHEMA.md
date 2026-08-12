# Unmute schema, v1 (decided)

Status: locked, v1. Post-lock adversarial review (context7 + live provider docs) applied 2026-07-15, marked inline as "review-corrected": warm transfer on Pipecat/LiveKit, model-written opening on ElevenLabs/Deepgram, Deepgram outbound voicemail. These re-word or loosen gates; none changes the schema shape.
Amended 2026-07-19 (N15): the `pipeline` and `voices` blocks are removed; models are defined once, concretely, in `agent.yaml`'s central `models:` map, grouped by kind (`think`/`speak`/`listen`/`turn`, each entry carrying `provider` + `model` as the old target bindings did), referenced by agents and by the top-level `listen`/`turn` selectors, with unreferenced entries kept as swappable alternates; `targets.yaml` shrinks to infrastructure plus optional per-target model overrides; `placement` is derived from `provider`; scaffold drops the `-dev` instance suffix. This one does change the authoring shape; old files fail strict decode loudly. (`reason` survives only as an internal role identifier — the `reason:` binding block is gone, so it is no longer user-facing; the think/speak vocabulary is the authoring surface.)
Amended 2026-07-20 (N16, N17, N18): the top-level `language` field is removed — language is per-model only, and no language kwarg is emitted when a model does not set one (N16); ElevenLabs is removed from the target set — the managed group is Vapi alone, and the `region`/`edition` instance fields plus the `unmute apply` command go with it, while ElevenLabs the model vendor stays in the Pipecat/LiveKit catalogues (N17); `deployment_region` is added to target instances (N18). N16 and N17 change the authoring shape; old files fail strict decode loudly. Dated pre-removal verification notes naming ElevenLabs are kept as history.
Amended 2026-08-10 (N23, N24): variables gain a `description` and a third origin, `source: conversation`, saved mid-call by a generated `update_variables` tool; `{{variable}}` substitution becomes real in four named places; and a new top-level `secrets:` block declares runtime environment values by name, driving each build's `.env.example` and a startup check. Both are additive — every existing package keeps loading and compiling unchanged. Detail in sections 4.4 (variables) and 4.12 (secrets).
Amended 2026-08-12 (N32): `deployment_region` accepts one region or a list of them — the scalar form stays valid, a list of more than one is LiveKit only and gated elsewhere with each platform's own reason, and every declared region reaches the compile report. Additive: no existing package fails decode.
Date: 2026-07-15.
Source: [ORCHESTRATOR_SHARED_CONFIGURATION.md](./ORCHESTRATOR_SHARED_CONFIGURATION.md). That file holds the research and the reasons. This file holds the decisions. If the two disagree, this file wins, and the other file should be fixed.

Scope: the four primary targets. **LiveKit, Pipecat, Vapi, Deepgram.** Secondary providers come later and never change this schema. (Five originally; ElevenLabs removed 2026-07-20, N17.)

Two groups of targets, used everywhere below:

- **Code targets**: LiveKit, Pipecat, Deepgram. Unmute generates the code, so it can build missing features itself.
- **Managed targets**: Vapi. The provider runs the agent, so only what their API offers can be used. (ElevenLabs was the second managed target until N17.)

---

## 1. How to read the tags

Every field below carries one tag:

- `core`: works on all four targets. No failures.
- `warn`: works on all four, but at least one target prints a warning (stderr, exit 0).
- `gated`: fails validation on at least one target. The notes say where and why.
- `provisional`: not proven on any target yet. Using it fails validation everywhere until a driver proves it. It stays in the schema so the shape is already decided.

A failure is a clear error before anything is generated or sent. A field never silently does nothing.

A blank tag cell inherits the tag of its enclosing construct: a task field without its own tag is gated (T1) like `tasks` itself.

---

## 2. Decisions

### Adopted from the source document

- **D1. Five targets decide the schema.** A field gets in only when all five primaries can honor what it promises, natively, conditionally, or through generated code. Amended 2026-07-20 (N17): four primaries after ElevenLabs' removal; the rule is unchanged.
- **D2. The source describes what the agent should do, never provider settings.** No free-form option maps in `agent.yaml`. The one exception is `params` on bindings in `targets.yaml`: sent to the provider as-is, never checked. Amended 2026-07-19 (N15): model definitions, including their `params`, now live in `agent.yaml`; the "never checked, forwarded as-is" contract is unchanged, only the file moved. Platform and telephony settings still never ride through `params`.
- **D3. Fail loudly, never average.** If a target cannot honor a field, validation fails with a clear message in that provider's own words. No silent downgrades.
- **D4. The pattern rule.** On code targets, Unmute may generate a missing feature itself. On managed targets the same feature fails, because there is nowhere to host generated logic.
- **D5. Three tiers.** T0: one agent. T1: tasks and task groups. T2: agent handoff. A file may use any tier; validation checks it against the chosen target.
- **D6. Task groups are ordered lists, not graphs.** No branches, no going back, no routers in v1.
- **D7. Context is always explicit.** Every task and transfer says what history it carries. There is no default, because providers disagree about their own defaults.
- **D8. One name space.** Tools and controls share one set of names. What an agent or task can call is decided only by its `tools:` list, nowhere else.
- **D9. Flat typed data.** Variables and task results are flat maps of name to primitive type. That is the real common ground across the five providers.
- **D10. Abstract profiles, concrete bindings.** `agent.yaml` names model and voice profiles. `targets.yaml` binds them to real models per target. Identities and `params` are forwarded as-is and never validated; the provider API and the generated project are the real validators. Version pins replace validation on code targets. Amended 2026-07-19 (N15) to **concrete models, target overrides**: `agent.yaml` defines each model fully (`provider`, `model`, voice, generation settings) and `targets.yaml` overrides a definition only for a target that cannot run it. The forwarded-verbatim, never-validated contract is unchanged.
- **D11. Declare traffic, derive machines.** The file declares peak sessions and call length. Worker counts, GPUs, and quotas are computed per target and printed in a report. They are never stored in the package.
- **D12. Package layout** as in section 3. Secrets never appear in any package file. Only env var names and secret references.

### New decisions, made here to close gaps the source left open

- **N1.** `placement` stays `api | local`. No `edge` or `on_prem` until a target actually needs the difference.
- **N2.** `capacity` stays in `agent.yaml` for v1. It may move into target instances later if multi-environment setups need different numbers.
- **N3.** Every field has an explicit required or optional status, written in the tables below. The source doc left some of these implicit.
- **N4.** A tool's name is its file name. `tools/lookup_customer.yaml` defines `lookup_customer`. There is no `name` field inside the file.
- **N5.** `assign:` is only allowed on single-task delegates. A group delegate has no `assign` in v1; step results travel through the group's shared context. Reason: the only path grammar is `result.<field>`, and it has no way to name a step.
- **N6.** Voicemail is verified on four of five primaries (2026-07-15): LiveKit `AMD` (classifications include `machine-vm`; leave a message via `generate_reply`, then shut down), Pipecat `VoicemailDetector` (leave message, then hang up), Vapi `voicemailDetection` plus `voicemailMessage` (message set = leave it, omitted = hang up), ElevenLabs `voicemail_detection` system tool plus `voicemail_message` (same semantics). `outbound: true` is therefore no longer blocked. (It required `on_voicemail` until **N29** made that field optional; voicemail handling is still generated with a warning on Deepgram (review-corrected 2026-07-15: an official AMD-bridge outbound reference impl proves the carrier-conditional lowering). (`greeting.speaks_first: user` history: native on Vapi as `firstMessageMode: assistant-waits-for-user`; native on ElevenLabs, verified 2026-07-15: an empty `first_message` means "the agent waits for the user to start the discussion"; generated on LiveKit and Pipecat. On Deepgram the behavior of an omitted `agent.greeting` is undocumented: warn until the driver smoke test proves silence.)
- **N7.** Variable types are the four primitives: string, number, boolean, integer. An enum result field assigns into a string variable.
- **N8.** All names (agents, tasks, groups, tools, controls, models, voices, variables, destinations) are lowercase snake_case. Names starting with an underscore are reserved by providers and rejected.
- **N9.** `pipeline.turn` is optional in `agent.yaml`. Whether a turn binding is needed in `targets.yaml` follows the target's role table (section 6), not this block. Superseded by N15 (2026-07-19): the `pipeline` block itself is gone; the role-table rule lives on in section 6.2.
- **N10.** Tool `input` is a JSON Schema object. All five targets accept JSON Schema tool inputs, so nesting is allowed here. `output` has the same shape; it was specified as enforced on code targets, but no driver implements that and it is unenforced everywhere today (N22).
- **N11.** `greeting` is a block, not a scalar: `speaks_first: agent | user` plus an optional `text`. With `text`, the agent opens with those exact words every call. Without it, the model writes the opening from the prompt. This replaces the scalar `greeting: agent_first | user_first` spelling in the source document, which could not express a fixed opening line.
- **N12.** Task `context` is the transfer context block without `variables`. Within one session the state store is already shared on all five primaries (LiveKit `userdata`, Pipecat flow state, Vapi Squad variables), so a task has nothing to filter; `context.variables` exists only on transfers.
- **N13.** The return path is part of the contract: when a task or a `then: return` group completes, the owner receives the typed result only; the task's conversation turns are not appended to the owner's context. This is LiveKit's native `AgentTask` behavior (verified against LiveKit docs 2026-07-15: a task starts with an empty chat context and its turns are not propagated back). `TaskGroup` is different, and `summarize_chat_ctx=False` alone does not honor this: a `TaskGroup` is itself an `AgentTask`, so when it completes LiveKit merges the group's turns into the owner's context on handoff regardless of that flag (verified against the livekit-agents source 2026-07-15: `voice/agent.py` merges `old_agent.chat_ctx` with the task context on completion; `summarize_chat_ctx` only controls a separate summarization pass, not the merge). To keep the group's turns out of the owner the driver must snapshot the owner's chat context before awaiting the group and restore it afterward, returning only the typed results; it still passes `summarize_chat_ctx=False` to skip the wasteful summarization call. Generated on Pipecat and Deepgram. Vapi is n/a while single tasks fail there.
- **N14.** `language` is the agent's primary spoken language, written as a BCP-47 tag and defaulting to `en`. It does not rewrite model routes. Pipecat and LiveKit lower it only through each catalogue entry's explicit `CallSpec.Language` slot: Pipecat official services place it in `Settings`, while SLNG and LiveKit plugins use constructor kwargs. An existing target `params.language` is an explicit per-integration override. ElevenLabs lowers it once to `conversation_config.agent.language`, which governs its integrated ASR and TTS. Vapi and Deepgram stay unavailable in `unmute init` until their generators ship; no generic parameter injection is invented for them. Verified against Pipecat, SLNG, LiveKit, and ElevenLabs provider docs 2026-07-16. Superseded by N16 (2026-07-20): the top-level field is gone; `language` is per-model only and nothing is emitted when unset.
- **N15 (amended 2026-07-19).** One definition per model, in `agent.yaml`. The old shape declared the same model in up to three places (`pipeline` role placement, an abstract profile, a target binding) and put the only real definition in `targets.yaml`; all three collapse into one unified `models:` map in `agent.yaml` where every model is defined fully and concretely (`provider`, `model`, `voice`, generation settings, `params`), with voice models and think models side by side. The `pipeline` and `voices` blocks are gone. A model's kind follows from where it is referenced: an agent's `voice:` names a speak model, an agent's or task's `model:` (or a `summarizer:`) names a think model; the referenced entry is validated against that kind's fields (section 4.3). `listen` and `turn` are small optional top-level blocks (section 4.2), because they are conversation plumbing shared by the whole package, not per-agent identity; `turn.semantic_endpointing` lives there. Each entry keeps the `provider` + `model` pairing the old target bindings used (the shape the author already liked) — `provider` names the catalogue vendor, `model` is the identity that vendor's SDK expects, and `placement` derives from `provider` (`local` runs on your machines; anything else is a hosted API; an explicit `placement:` overrides). No route-parsing: folding the provider into the `model` string was rejected during implementation because the forwarded model identity is not uniform across vendors (OpenAI wants `gpt-4.1-mini`, SLNG wants `slng/deepgram/nova:3-en`), so a parse would mangle what reaches the SDK. `targets.yaml` keeps infrastructure only (provider, version, pins, transport, carrier, destinations) plus an optional `models:` override map for a target that cannot run a defined entry (section 6.2); an override replaces the whole entry, no merge rules. Supersedes N9 and amends D2/D10. Both original jobs of declared placement survive, per target and more precise: `local` still fails loudly on managed targets, and sizing still reads placement from each target's effective models. Strict decode (compiler V3) makes old files fail with "unknown field pipeline" naming file and line. The `reason` role identifier stays internal-only (the `reason:` binding block is gone), so it is never renamed in the authoring surface — think/speak is the user vocabulary. Amended again 2026-07-19 (same day, review with the author): the map is grouped into four kind sections (`think`/`speak`/`listen`/`turn`) so kind is structural rather than inferred from reference sites; listen/turn entries live in the map like everything else and the top-level `listen:`/`turn:` fields select one by name (defaulting to a section's sole entry); unreferenced entries are legal palette alternates for fast model swapping, compiled only when selected. Per-agent listen/turn was considered and rejected: STT/VAD are call plumbing (the generated Pipecat main worker owns STT; the Deepgram bridge has one listen per session), and repeating the same reference on every agent recreates the duplication N15 removes. Scaffold names the generated target instance after the provider (`pipecat`, never `pipecat-dev`): users test exactly what they deploy, and extra instances are added only when a real second environment exists.
- **N16 (2026-07-20).** No package-level `language`. Language is a property of the model that hears or speaks it, so it lives only on model definitions (section 4.3): an optional BCP-47 tag on speak and listen entries. When set, it lowers through the catalogue entry's explicit language slot exactly as N14 established (an existing `params.language` stays an explicit per-integration override). When unset, **no language kwarg is emitted anywhere**: the provider default, or the language already encoded in the model route (`slng/deepgram/nova:3-en`), applies. No `en` is ever injected. Supersedes the top-level half of N14; old files with a top-level `language:` fail strict decode loudly (compiler V3).
- **N17 (2026-07-20).** ElevenLabs leaves the target set. The managed-target group is Vapi alone; four primaries decide the schema (D1). Everything that existed only for the ElevenLabs target goes with it: its driver spec and generator, its target catalogue, the `unmute apply` command (ElevenLabs was its only wired provider; the pattern returns with the next managed driver), and the `region`/`edition` instance fields (their only consumer was ElevenLabs key residency). ElevenLabs the **model vendor** is untouched: the `elevenlabs` STT/TTS catalogue entries inside the Pipecat and LiveKit drivers stay (B4 breadth, compiler V20/V21). Dated verification records naming ElevenLabs elsewhere in this file and in §9/§B history are retained as history.
- **N18 (2026-07-20).** `deployment_region` is a new optional instance field in `targets.yaml`: where the target platform runs the deployed agent. Free-form provider vocabulary, forwarded as declared, never validated or derived — a typo fails at the provider with its own error before anything irreversible exists. Lowering, verified against provider docs 2026-07-20: Pipecat Cloud takes it as the top-level `region` key in the emitted `pcc-deploy.toml` (line omitted when unset); LiveKit Cloud accepts a region only as the `lk agent create --region` flag (`us-east|eu-central|ap-south` today, immutable after creation, no `livekit.toml` key exists), so the generated README's deploy instructions carry the flag. A model's own service region is a different knob and intentionally stays on the model: `params` for kwarg-style pinning, `endpoint_env` for endpoint-style — an agent deployed in one region may pin a model endpoint in another.
- **N19 (2026-08-10).** A tool's execution kind is a **block name**, not a field. `tools/*.yaml` keeps the model contract (`description`, `input`, `output`) and the two conversation scalars (`interruption`, `effect`) at the top level, and everything about how the tool runs moves inside exactly one block named after the kind: `webhook:`, `local:`, `mcp:`, `builtin:`, `client: {}`, `provider_hosted: {}` (section 5.2). The top-level `execution:`, `url_env:`, `handler:`, `builtin:` scalar, and `instructions:` keys are gone. The reason is that the flat shape let a field belong to no kind in particular: `handler` on a webhook tool, `url_env` on a builtin, `token_env` with no `auth` — each needed its own cross-field rule, and each rule was a place the schema could disagree with itself. With the kind as the block, a foreign field is unwritable and the rules disappear. `webhook:` also gains the optional `auth:` block (section 5.3, `bearer` and `api_key`), and `mcp:` is the slot a future MCP `auth:` lands in with no further shape change. Old files fail at load with the block form named and the line quoted, never a bare "unknown field" (compiler V36). Every `*_env` name is `UPPER_SNAKE`, so a pasted URL or secret fails validation instead of becoming a lookup that fails at call time.
- **N20 (2026-08-08).** An absent `greeting` block has one default on every target whose driver actually emits an opening: the agent speaks first with a model-written opening, lowered exactly as `speaks_first: agent` without `text`. That is LiveKit and Pipecat today, the two shipped generators; the scope is deliberately "drivers that exist", not the code-target group, because Deepgram is a code target whose driver is unwritten and whose behavior for an omitted `agent.greeting` is undocumented (N6). This closes a real divergence: LiveKit already generated the opening, while Pipecat activated silently and waited, so the same source produced opposite first impressions on a call. Section 4.8 previously declined to name a default ("the target's own default applies"), which is what let the two drivers drift. Silence is not eligible to be the default, because a caller has no visual signal that the line is live and reads it as a dropped connection. Agent-speaks-first is also the established shape of a new package: `unmute init` scaffolds `speaks_first: agent`, though with a fixed `text` line rather than a model-written one, so what this amendment settles is only what happens once that block is deleted. `greeting.absent` stays **warn** rather than becoming core, and the warn now covers two different situations rather than one: Vapi is managed and applies its own native default, while Deepgram is simply unproven. When the Deepgram driver ships it adopts this default and its cell can go core; until then the warn is the honest answer. Declaring `speaks_first: user` is now the only way to get silence, which is the point: it has to be asked for. Amends 4.8; no authoring shape changes, so no file fails decode.
- **N21 (2026-08-08).** Tool `input` and `output` keys are inspected and **reported, never rejected**. A key unmute does not recognise produces a warning on stderr with exit 0 naming the tool and the exact path, and a valueless key containing whitespace gets a sharper one that names the cause (`tool "check_availability" has an empty schema key "e.g. 2026-08-14" at input.properties.date; an unquoted comma in a YAML flow mapping splits the entry, so quote the value if that text belongs to it`).
- **N22 (2026-08-08).** Tool `output` is **not enforced on any target**, and this entry exists because the schema said otherwise. N10 and the field table claimed it was "enforced by generated code on code targets", but no driver has ever read it — `.Output` appears nowhere in `internal/generate`. The documentation is corrected here and in the field table; the capability table is deliberately **unchanged**. Widening its Vapi-only warning to all four was tried and reverted: `warn` is defined in section 1 as "works on all four", and every other use of it in the table means works-with-a-caveat (placement ignored, endpointing advisory, minimum words lossy). "Declared and completely inert" is not that. The tag that does mean "not proven on any target yet" is `provisional`, and it fails validation everywhere, which would reject every package that declares an output today, including `safe_core` and every example. Choosing between implementing enforcement and taking that break is a maintainer decision, and quietly redefining `warn` to avoid making it would put the table at odds with its own vocabulary. Note this also stands against section 1's promise that "a field never silently does nothing": `output` does exactly that today, which is the strongest argument for closing it properly. Declaring it is still useful: it documents the tool contract and rides into `compile-report.json`. Enforcement needs a design call this document cannot make alone — what a generated agent should do when a tool returns a value that does not match, mid-conversation, after the call has already happened.
  Nothing here fails a build, and that is a decision rather than an omission. JSON Schema requires an implementation to ignore keywords it does not know, whatever their value, so `{"e.g. 2026-08-14": null}` is itself a *valid* schema. N10 inherits that openness by calling tool `input` "a JSON Schema object" without restricting the vocabulary, and D10 says forwarded values are never validated because the provider is the real validator. No key-shape rule can catch this accident without also refusing something those three permit, and a closed allow-list would rot with each new draft or vendor keyword besides. So unmute reports rather than refusing; the win is turning silence into a named, path-precise message.
  Why it matters: both fields decode into `map[string]any`, so the strict decode that rejects unknown fields everywhere else (compiler V3) stops at that boundary and anything typed inside can travel into the emitted tool contract: on the Pipecat Flow-node path the whole `properties` map is serialised verbatim into a `FlowsFunctionSchema`, so a bad key there reaches the model as part of the tool's advertised arguments. On every other path it is dropped instead, which is why the bug stayed invisible; the measured matrix is below. The bug that motivated this shipped in a fixture and was invisible for weeks: a comma inside an unquoted YAML flow scalar split `description: The requested date, e.g. 2026-08-14` into a truncated description plus a null-valued key `e.g. 2026-08-14`.
  Inspection recurses through every subschema position across draft-07 to 2020-12 — the name-keyed containers (`properties`, `patternProperties`, `$defs`, `definitions`, `dependencies`, `dependentSchemas`) and the direct and list positions (`items`, `contains`, `propertyNames`, `if`/`then`/`else`, `not`, `allOf`/`anyOf`/`oneOf`, `prefixItems`, the `additional*`/`unevaluated*` pair, `contentSchema`) — and deliberately does **not** descend into keywords carrying author data (`default`, `const`, `enum`, `examples`), because an object-valued default would otherwise be read as a schema. Vendor extensions (`x-`, `$`-prefixed) are silent. The warning does **not** promise the key reaches the provider, because measured behaviour is not uniform: LiveKit reads five named keys per property (`type`, `enum`, `description`, plus `properties` and `required` above them) and drops everything else; Pipecat drops them too, except on the Flow-node path where `buildTool` hands the whole `properties` map to `pyLiteral` and it lands in `bot.py` verbatim. A key at the top level of `input` is dropped by both. So an unrecognised key survives in exactly one case out of four, and the answer needs three axes (driver, agent-tool versus task-tool, depth) rather than one. The warning therefore says only `unmute does not read it` and makes no survival claim at all: "depends on the driver" would be wrong often enough to mislead, and this table is the place with room to be exact. Task `result:` field schemas reach the same generator path (`ResultField.Schema`) and are covered by the same walk, reported against the field rather than a tool (`task "collect" result "details" has unrecognised schema key ... at schema.properties.city`).

- **N23 (2026-08-10).** Variables gain a `description` and a third origin, and templates become real. `variables` keeps being **one block**: origin is a property of a variable, not a separate kind of thing, so it stays in `source:` rather than splitting the block in two. The vocabulary is **input variables** (`source: call_start`), **system variables** (the runtime-owned sources that already existed), and **conversation variables** (`source: conversation`, new — the model saves the value mid-call). `description` is legal on every variable and feeds the generated capture tool, the compile report, and the generated README.
  `{{variable}}` now substitutes, in exactly four places: `conversation.greeting.text`, agent and task instructions, tool `inject:` values, and `webhook.path`. The first two render **once at session start**, so they may only name a variable that has a value by then — an input variable, a system variable, or any variable with a `default`; a conversation variable without a default is an error there, because prompts are never re-rendered mid-call. The last two render **per tool call**, so a conversation variable is fine. A token that is not a declared variable fails with file:line, and one naming a secret fails saying secrets never flow through templates.
  Declaring any conversation variable makes the drivers emit one tool, **`update_variables`**, whose parameters are exactly those variables with their types and descriptions, all optional, attached to every agent and task. The name is reserved. This is a deliberate exception to D8: it is generated plumbing like the `requires` guard, not a package tool.
  Input variables arrive as one flat JSON object of name to value: the job dispatch metadata on LiveKit, the runner's call-start payload on Pipecat, and `unmute dev --var name=value` locally (which parses each value against its declared type and refuses an undeclared name). Full detail, including the per-target gates, in section 4.4.
- **N24 (2026-08-10, amended 2026-08-11).** A new top-level `secrets:` block declares the runtime environment values a package needs, as a **list of environment variable names** (`UPPER_SNAKE`). A secret has **no fields at all**. The block began as a map with `description` and `required`, and both are gone: a description restated the name it sat above and then had to be kept true in a second place, and `required: false` bought an optional-credential case no package ever wrote. Every listed name is required, a repeat is an error, and there is deliberately no `default:` or `example:` field, because anywhere a value could be written one day a real one will be, and D12 says values never appear in any package file. Secrets are **never** reachable from a template (that is what N23's error says); they reach the call only through the existing `*_env` slots, the generated auth helpers, and `os.environ` in a local Python handler.
  Two things follow from declaring them. Each code target's build writes a `build/<target>/.env.example` listing every declared name, then the env names the route needs that the package never declared, labeled once as a group. And a generated runtime refuses to start when a declared secret is missing or empty, naming it — the same contract tracing already had (4.11). An env name the package references but does not declare is a **warning on stderr, exit 0**, never an error: declaring secrets is opt-in, and a package written before this block existed still compiles unchanged.
- **N25 (2026-08-10).** A human transfer's **shape is a block name**, and the briefing is **free text**. `mode: cold | warm` and `briefing: summary | message | wait` are both removed; a `human_transfer` control now declares `destination` plus exactly one of `cold:` or `warm:` (section 4.7). Two separate corrections, made together because they touch the same three lines.
  The first is N19 applied to controls. With `mode:` as a field, `briefing:` was a legal sibling of `mode: cold`, where it means nothing, so the schema needed a cross-field rule to reject it. That is the exact class of rule N19 removed from tools by making the execution kind a block, and the argument transfers unchanged: with the shape as the block, a warm-only field on a cold transfer is *unwritable*, and the rule disappears rather than being enforced. Cold and warm are also genuinely different machines, not two settings of one (cold is a single carrier redirect that ends the session; warm is a three-party state machine with hold, a private consultation leg, and a decline path), so a name each is honest about what the file is choosing.
  The second is that `summary | message | wait` was never portable vocabulary. It is Vapi's `transferPlan.mode` (`warm-transfer-say-summary`, `warm-transfer-say-message`, `warm-transfer-wait-for-operator-to-speak-first`), and neither shipped driver has those three modes. LiveKit's prebuilt `WarmTransferTask` *always* summarizes and takes free text on top, verified in the `livekit-agents` source 2026-08-10: `INSTRUCTIONS_TEMPLATE` interpolates the formatted transcript and an `extra` string, and `extra` reaches it as `instructions=WorkflowInstructions(extra=...)`. Pipecat has no prebuilt at all, so a generated briefing takes whatever prompt the package writes. There is no "wait" mode on either. Free text is what both honour without loss and is strictly more expressive than three fixed values; a Vapi driver maps free text into whichever `transferPlan.mode` fits when it ships. The three `briefing.*` telephony features are deleted with the enum.
  Two knobs are **added** inside both blocks, because they are the decisions a caller actually feels: `ring_timeout` (how long the person's phone rings) and `on_unavailable` (`return_to_caller | hangup`, default `return_to_caller`). `on_unavailable` is deliberately one field covering four failures (no answer, declined, voicemail, dial failure) because that is the platform's own shape rather than a simplification: `WarmTransferTask` completes with a single `ToolError` for all four, having already restored the caller's audio and stopped the hold music.
  Three knobs are deliberately **not** added: hold music (both drivers default to a built-in clip, and silence on hold reads to a caller as a dropped connection, so defaulting is the decision and the knob is not), extension/DTMF (LiveKit has `dtmf`, Pipecat's is carrier-dependent and unproven, so a field would work on one driver of two), and caller ID (LiveKit's `sip_number` names the trunk's outbound number, which section 3 puts in `targets.yaml`, never in `agent.yaml`). Each lands inside the existing block later with no shape change, which is the point of the block.
  This changes the authoring shape; old files fail strict decode loudly, with the block form named and the offending line quoted rather than a bare "unknown field" (compiler V3, the N19 error contract). Full design, the per-route support matrix, and how to test each shape are in [TRANSFERS.md](TRANSFERS.md).

- **N26 (2026-08-10).** A `destinations:` value may be an **environment variable name** instead of a literal. The three forms are unambiguous by shape, so no new key, suffix, or wrapper is needed: an E.164 literal starts with `+`, a SIP URI with `sip:`/`sips:`, and an UPPER_SNAKE token can only be an env var name. Drivers emit the literal verbatim or an `os.environ` lookup, and a named variable is registered in the generated `.env.example` and required-env list like every other one.
- **N27 (2026-08-11).** The shape block carries **every parameter of the transfer**, `destination` included. N27 left `destination` above the block because both shapes need it, which produced `cold: {}`: a block naming a shape while the field that decides where the call goes sat outside it. The placement rule replaces the sharing rule — above the block is the tool (`kind`, `when`), inside it is the transfer — so a shape block always has a body and the empty-brace spelling leaves the surface. Old files fail strict decode at the `destination:` line.
  Literals-only was the previous rule and it does not hold up. Section 3 says `targets.yaml` carries "environment variable names and secret references only, never values", and `destinations` was the one field contradicting it. In practice that meant real phone numbers committed to git, and no way for staging to dial a test line while production dials the real desk without editing the file per environment. A phone number is not a secret, which is why literals stay legal and are still the right choice for a number that never changes; but "not a secret" is not the same as "never varies".
  What does **not** change: the model never sees a number and can never dial one. It picks a symbolic name, the target resolves it, and an unresolvable name fails before anything is generated. Deferring the value to the environment moves *when* the number is known, never *who* chooses it.

- **N29 (2026-08-11).** `on_voicemail` is **optional** on an outbound channel, not required by it. The original rule read "iff `outbound: true`" on the theory that dialling a human means dialling their voicemail, but it turned out to describe no shipped route: the Twilio Media Streams dial-out flow never needs answering-machine detection to place or run a call, and the compiler decoupled the two when the Pipecat carrier-WebSocket route landed. Three shipped examples set `outbound: true` with no `on_voicemail` and validate green, so the table was describing a gate the code had already dropped. Setting `on_voicemail` on a route that cannot detect voicemail still errors, which is the gate that was actually doing the work. This loosens a requirement and adds nothing; the field, its values, and every route tag are unchanged.

- **N30 (2026-08-11).** A **warm** human transfer requires `channels.phone.outbound: true`. Warm holds the caller and then *dials* the destination, so the agent places calls whatever the channel declares. A package that says `outbound: false` while emitting `calls.create` is describing a shape it does not have, and the cost is not theoretical: the `human-transfer` example shipped that way, validated green, and then refused `--to` with "no outbound direction", which reads as a driver regression rather than as the package's own declaration. Cold is unaffected; it redirects the caller's existing call and originates nothing. This tightens one cross-field rule and changes no field, no value, and no route tag.

- **N31 (2026-08-11).** Human transfers are **native-route only**, and this supersedes N28. A transfer compiles only on a route where the platform ships the primitive: cold on `(livekit, sip)` (`TransferSIPParticipant`, a SIP REFER the trunk must allow) and on Pipecat's Daily route (`transport.sip_call_transfer`); warm on `(livekit, sip)` only (`WarmTransferTask`). The connector and carrier-websocket transfers N28 described were built, live-tested, and then deleted: every custom design made the generated process own the call's audio path, and each live test found a new lifecycle bug in that ownership (briefing-pipeline leaks, serializer auto-hangup fights, announcement races). Validation now refuses a transfer on any other route and names the routes that work. Nothing in `agent.yaml` changes: the `cold:`/`warm:` blocks, their fields, and their defaults are untouched; N30 (warm requires `outbound: true`) stands. The capability map, the secrets, and the cloud test walkthroughs live in [TRANSFERS.md](TRANSFERS.md); the deleted designs remain in git history.

- **N32 (2026-08-12).** `deployment_region` accepts **one region or a list of them**. The field keeps its name and stays optional, and the scalar form N18 shipped stays valid, so no package already written needs an edit: `deployment_region: us-east` and `deployment_region: [us-east, eu-central]` are both legal, and a one-element list behaves exactly like the scalar. The plural is a **LiveKit-only** shape. On LiveKit each declared region is its own deployment from one build directory: the generated README prints one `lk agent create --region <region> --config livekit.<region>.toml` per region and one `lk agent deploy --config <same file>` per region, and every deployment keeps the package's single dispatch name, which gives the platform's nearest-region routing (verified against [Regions: agent deployment](https://docs.livekit.io/deploy/admin/regions/agent-deployment/), 2026-08-12). A single region keeps the platform's default config file name, so nobody has to learn a file name they never type. On **Pipecat, Vapi and Deepgram** a list of more than one is a **gated** validation error before any artifact exists, each quoting its own reason; Pipecat's is that agent names are globally unique across regions, so a second region is a differently named agent (`pipecat cloud deploy <name>-<region> --region <region>`) with its own region-scoped secret set, which the generated README prints (verified against [Deployments](https://docs.pipecat.ai/pipecat-cloud/fundamentals/deploy) and [Regions](https://docs.pipecat.ai/pipecat-cloud/guides/regions), 2026-08-12). A repeated region in one list is an **error**, never silently deduplicated, because two first deploys against one config file name is a confusing thing to debug; an empty entry is an error too. Region codes stay **unvalidated and never enumerated** (N18 unchanged): they are forwarded exactly as written, the platform CLI is the validator, and no list of codes is kept in this repository, because both platforms change theirs without notice. N18's parenthetical list of LiveKit codes is history for that reason. Every declared region now also appears in the compile report on both drivers as `deployment_regions`, since it is a value Unmute forwards without checking. The authoring change is additive, so nothing fails strict decode; the derived authoring schema shows the field as a `oneOf` of a string and an array of strings, regenerated from the Go structs rather than hand-edited.

- **N33 (2026-08-12).** A generated LiveKit project **dials out with the carrier's own SIP credentials, passed inline**, and uses no stored LiveKit outbound trunk. Both dial-out paths change together: the warm transfer passes `sip_connection` and the outbound call passes `trunk`, each an `api.SIPOutboundConfig` built from one emitted helper, plus the from-number that inline configuration makes mandatory because it carries no number list for the platform to default from (verified against [WarmTransferTask](https://docs.livekit.io/agents/prebuilt/tasks/warm-transfer/), [CreateSIPParticipant](https://docs.livekit.io/reference/telephony/sip-api/#createsipparticipant) and [Inline trunk configuration](https://docs.livekit.io/telephony/making-calls/outbound-calls/#inline-trunk), and against `livekit-agents 1.6.9` as installed, on 2026-08-12). What this fixes was found on a live deploy, not in review: a compiled `examples/human-transfer` registered, held a full conversation, fired the transfer, and raised ``ValueError: `LIVEKIT_SIP_OUTBOUND_TRUNK` environment variable, `sip_trunk_id`, or `sip_connection` must be set`` from inside the prebuilt's constructor, because a build directory that looks complete cannot mint a platform-assigned trunk ID. So `LIVEKIT_SIP_OUTBOUND_TRUNK` leaves the emitted required-environment list, the compile report and the local development path; the emitted `sip-outbound-trunk.json` and the `lk sip outbound create` step go with it; and a deployment that still sets that name is unaffected, because the prebuilt ignores it once `sip_connection` is passed, by its own documented precedence. **Inbound is untouched.** An unsolicited call arrives with no request of ours for configuration to travel with, so the platform must already know which project owns the number (the inbound trunk) and which room and agent the caller joins (the dispatch rule); dialling out is our own code starting a call, which is exactly why the settings can ride along with it. **The authoring surface does not change**: the Connection's four keys (`sip_address`, `sip_username`, `sip_password`, `from_number`) keep their names and nothing fails strict decode. What moved is the **environment names** the shipped example, the scaffold and the documents use, from `TWILIO_SIP_ADDRESS` and friends to `SIP_TRUNK_HOSTNAME`, `SIP_AUTH_USERNAME`, `SIP_AUTH_PASSWORD` and `SIP_FROM_NUMBER`, because these are standard SIP trunk settings rather than one carrier's and the same emitted code now dials through any SIP carrier with them. Those names are the author's to choose and the compiler knows none of them, so a package already using carrier-prefixed names keeps compiling and dialling unchanged; an operator adopting the updated example renames four lines in `.env` once. One thing tightens: a **warm transfer on a target with no telephony Connection is now a gated error** naming the four values it needs, because inline dialling has nothing to dial with there. It used to validate green and then read a trunk ID the package never mentioned, which is the same defect in a quieter form. Region pinning and transport stay **out of scope**: both are optional on the platform, the transport is auto-detected, and outbound calls already originate from the region the agent runs in, which `deployment_region` sets. Nothing about cold transfer changes: it acts on the caller's existing leg through SIP REFER and needs no trunk of either kind. This also retires one sentence of **N28**, which explained the connector route by saying `WarmTransferTask` "acts on a SIP participant reached through an outbound trunk": a SIP participant is still needed, a stored trunk is not. N28's conclusion was already superseded by **N31**, which stands unchanged.
- **N34 (2026-08-12).** **Pipecat telephony is the Daily route.** `transport: daily-sip` is the only Pipecat transport that carries a phone call end to end, and it is a managed route: Daily's own infrastructure delivers the call to an agent deployed on Pipecat Cloud, through the platform's managed dial-in webhook. **No authoring field changes.** Nothing in `agent.yaml` or `targets.yaml` is added, removed, or renamed by this amendment; it records what the code already supports and narrows three statements elsewhere in this document that describe more than it does. A hosting-model field was considered and rejected: there is one hosting model per driver, so an author has nothing to select. A `channels.phone` entry on the Daily route was considered and rejected: the route has no carrier connection, the compiler derives what it needs from the transport, and a telephony channel would drag `capacity` in with it (§4.10). Both rejections are enforced by tests, so re-adding either fails the suite rather than arriving quietly.

  Three corrections this makes, each verified against the code and the provider documentation on 2026-08-12:

  1. **Pipecat carrier-WebSocket cold transfer does not exist.** §4.9 and §9 described Twilio, Telnyx, and Plivo carrier-WebSocket adapters emitting out-of-band carrier-REST cold transfer "but remain provisional". Those were built, live-tested, and deleted (N31). Pipecat's own documentation is that its websocket transports carry no call-transfer control. Cold on Pipecat is the Daily route and nowhere else.
  2. **The Pipecat carrier-WebSocket warm bridge does not exist either.** The two-socket software bridge described on the exact `(carrier-websocket, twilio)` route was deleted with the rest. Warm compiles on `(livekit, sip)` only, today.
  3. **Daily documents warm transfer.** This document said Daily SIP "is deliberately **not** the Pipecat warm route" because a shared room gives the bot one output track. The engineering judgement holds, and warm on Daily does need the generated bot to own audio control. The framing was wrong: `docs.pipecat.ai/pipecat/telephony/daily-pstn` states Daily supports two transfer patterns, cold and warm. So this is a thing **this project does not emit yet**, not a thing the platform cannot do. Emitting it is a planned follow-up feature. A statement of support must say which of the two it means; see also [TRANSFERS.md](TRANSFERS.md).

  **N30's scope is narrowed.** "A warm human transfer requires `channels.phone.outbound: true`" applies only to routes that have a phone channel. The Daily route has none and cannot declare one, so on it the compiler derives the fact instead and names the account prerequisite. N30 is otherwise unchanged, and still holds wherever a phone channel exists.

  **New in the report, not in the authoring surface.** A route may now carry **account prerequisites**: platform features the provider grants on request rather than by default, which the route cannot work without. The Daily route has one, `daily_dialout`, needed by cold transfer and by outbound calling, because both dial a destination. It is deliberately **not** a capability and carries none of the four tags: unmute compiles the package correctly, and whether an author's Daily account has the feature is unknowable at compile time. Gating it would refuse correct packages; warning would print on every Daily compile forever. So `validate` reports it at exit 0, the emitted README carries it, and `compile-report.json` records it as `route_prerequisites`. The facts live in `internal/target` only (verified against [Daily dial-out](https://docs.pipecat.ai/pipecat-cloud/guides/telephony/daily-dial-out), 2026-08-12: dial-out is a paid Daily feature granted on request, and international dial-out is enabled separately per domain).

  **`unmute dev --telephony` refuses on this route**, by name, and points at the browser and console modes that do work plus the deploy path for a real phone call. Daily delivers calls to a deployed agent, so there is no local topology to run. A silent no-op would be a flag that does nothing, and a message saying telephony is unsupported here would be false: this is the only Pipecat telephony route there is.

  **Independent of N33**, which lands in the same release and covers LiveKit outbound dialling. The two touch nothing in common: N33 is about how a LiveKit project reaches a SIP carrier, this is about the one Pipecat route that carries a call at all. Both were numbered N33 in parallel branches; this one renumbered.

- **N35 (2026-08-12).** On a LiveKit warm transfer the **emitter owns the manager-facing persona**, and the consultation after the call is answered has **no hard bound**. **No authoring field changes**: `warm.briefing` keeps its name and its meaning, no field is added, removed, or renamed, and a package written before this compiles with no edit. What changes is the emitted Python. Three things, each answering something observed on a live call on 2026-08-12 rather than in review, when a manager was dialled, heard silence, and was then greeted like a stranger by an agent that never mentioned the caller.

  **The supported instructions hook replaces the deprecated one.** The emitted call passes `instructions=WorkflowInstructions(persona=..., extra=...)` in place of `extra_instructions=`, which the prebuilt warns about and ignores whenever `instructions` is given. The persona is a module-level constant, emitted once per package that has any warm transfer and referenced by every warm transfer in it, so several transfers cannot drift apart. The authored `briefing` lands in `extra`, which is the same last-section slot it landed in before, and the argument is **absent** rather than empty when a package declares no briefing, because a no-op argument in emitted code is a thing a reader has to check. It is only cosmetic: `extra` defaults to `""` and the platform holds no default text for that slot, so omitting the argument and passing an empty string resolve to byte-identical instructions (measured, not read, on 2026-08-12). The `persona` slot is not like that: it has a `NOT_GIVEN` sentinel and a platform default behind it, so passing `""` there really would delete the identity section. Verified against `livekit-agents 1.6.9` as installed, on 2026-08-12: the class is **`WorkflowInstructions`**, renamed from `InstructionParts` with no alias between two patch releases, so the retired name would have shipped a project that fails at import. A test keeps it out of every template and every emitted file.

  **Why the emitter owns the persona at all.** The prebuilt's own persona says to give the colleague context, and its template says to open with a summary, but its `on_enter` deliberately lets the human speak first and never briefs unprompted. So the agent's first turn is a *reply*, and a reply to "hello" is a greeting unless the prompt says otherwise. The emitted persona says four things the platform's does not: open with the handover and never greet; tell the colleague the caller goes through as soon as they say they are ready, because joining the calls is that agent's own tool call and nothing else triggers it; say plainly that someone is on hold and their details are not known yet when the transcript carries no caller words, because a model asked to summarise nothing improvises a greeting; and decline out loud, with the colleague's reason, rather than leave the question open. Only the persona section is replaced. The platform still supplies the paragraph saying who is who, the caller transcript, the sentence naming the joining tool, and the instruction to open with a summary.

  **The cost, stated as principle II requires.** After the dialled call is answered, **nothing bounds the consultation**, and the caller hears hold music for all of it. `ringing_timeout` covers ringing only. There is no post-answer timeout on the platform, the awaited result is returned through `asyncio.shield`, so a timeout on our side would raise here while the consultation kept running with the caller still muted and the music still playing, and the one method that stops the music and restores the caller is private. So the exit is the prebuilt's own decline tool, reached by asking the briefing model to use it, which is a mitigation and not a bound: a caller can in principle hold until the manager hangs up. Two ways to get a real bound were considered and rejected for now, both recorded in `specs/003-warm-transfer-briefing/plan.md`; neither is taken without being asked for. Upstream documentation feedback asking for a post-answer bound was sent on 2026-08-12, because the flow's own documentation promises the caller comes back when a transfer does not happen and today that promise holds for ringing only.

  **Every transfer now leaves a log.** Three info lines per warm transfer (fired, dialling out with the count of conversation items handed over, then merged or unavailable with elapsed seconds) and three per cold transfer (fired, referring out, then completed or failed with elapsed seconds). No line carries a destination, a credential, an environment variable value, or the caller's words, and a test walks every emitted `logger.` call to keep it that way. This exists because the live failure produced **no log line at all**, which left three separate causes fitting the same evidence. The count is of items handed over, not of the smaller number the prebuilt's own transcript filter keeps, so a count of zero or one means the briefing had nothing, and a healthy count means material was passed rather than used. **Cold transfer gains logging and nothing else**: its request, its destination handling and its failure branches are unchanged. **Pipecat is untouched.**

- **N36 (2026-08-12).** A generated LiveKit SIP project's telephony is **set up from its own README**: the carrier steps are dictated in it, and the LiveKit side is one emitted command. **No authoring field changes**: nothing in `agent.yaml`, `targets.yaml`, or a Connection is added, removed, or renamed, and a package written before this compiles byte-identically apart from the surfaces named below.

  **`LIVEKIT_SIP_INBOUND_TRUNK` is retired.** Inbound still needs its two platform records, because an unsolicited call arrives with no request of ours for configuration to travel with, so the platform must already know which project owns the number (the inbound trunk) and which room and agent the caller joins (the dispatch rule). What changes is how the dispatch rule finds its trunk: the emitted `telephony-setup.sh` resolves the trunk **by the phone number the operator already has**, from `lk sip inbound list --json`, and substitutes it into `sip-dispatch-rule.json` itself. So the name leaves the required-environment list, `.env.example`, the compile report, the emitted Compose file, and the dev command's plumbing, and the README carries exactly one sentence telling an operator of an earlier build to delete it. Nothing has to be transcribed between two commands, which is what the retired name was: a record ID copied out of one command's output into an environment variable. A deployment that still sets it is unaffected, because nothing reads it. The `${UNMUTE_SIP_TRUNK_ID}` token now in the dispatch input is not an environment name; it is substituted by the script before `lk` ever sees the file.

  **The script is the provisioning artifact, and the compiler still provisions nothing.** It is emitted for SIP routes that accept inbound calls, never for the connector route, which carries the inbound feature but has no SIP trunk at all. It needs `lk` and `jq`, checks both before creating anything, is idempotent by listing first, and it never sources the env file: it reads the one phone-number assignment textually, because sourcing would read every secret in the file and would abort on a single line whose name is not a shell identifier. The one silent failure that matters is closed by construction: a dispatch rule with an empty trunk list matches **every** trunk in the project, so the emitted JSON always carries a populated `trunk_ids` and the script refuses to create a rule while the resolved ID is empty.

  **The runbook is written for LiveKit Cloud**, because that is what the deploy section of the same README uses, and the section that used to be headed "Configure self-hosted LiveKit SIP" told a Cloud operator to skip the only part they needed. Exactly one step differs when LiveKit is self-hosted, the origination target, and that difference is a labelled note beside that step rather than a second runbook. Twilio's carrier steps are dictated with console paths and CLI equivalents, verified 2026-08-12 against [SIP trunk setup](https://docs.livekit.io/telephony/start/sip-trunk-setup/) and [Twilio trunk configuration](https://docs.livekit.io/sip/quickstarts/configuring-twilio-trunk/): the termination domain ending in `pstn.twilio.com` is **one value**, the hostname env, not two; origination is the project SIP URI with `;transport=tcp`, found on the project settings page or by dropping the `p_` prefix from `lk project list --json`; and cold transfer's only setting anywhere is the trunk's Call Transfer and PSTN transfer toggles. A carrier without a dictated block gets the same obligations without console paths, pointing at its own provider guide, which is what makes a second SIP carrier a change of words rather than of shapes. **Automatic provisioning stays out of scope** (§8): unmute buys no numbers and creates no carrier trunks.

- **N28 (2026-08-11).** Human transfer works on the **LiveKit Twilio connector** route, lowered by our own bridge rather than LiveKit's SIP machinery. `transfer_sip_participant` and `WarmTransferTask` both act on a SIP participant reached through an outbound trunk, and the connector has neither: its caller is audio the generated `telephony_bridge.py` published into a room. What the bridge does have is the Twilio call itself, which is the same thing the Pipecat carrier-WebSocket route builds its transfers out of, so the two routes make the identical carrier moves: cold redirects the caller's call over the REST API, warm creates a second streamed call whose media socket lands on the same bridge. An agreement test pins that sameness, because one Twilio product behaving two ways would be a bug.
  The agent and the bridge talk over **LiveKit RPC**: the bridge is already a participant in the agent's room, so there is no Redis on this route (there never was) and no side channel. Privacy on a warm transfer is a property of the topology, as on Pipecat: each phone leg has its own WebSocket and its own downlink, a held leg is sent a synthesized hold loop instead of room audio, and no leg is ever forwarded its own audio.
  What this changes for authors: nothing in `agent.yaml`. The same `cold:` and `warm:` blocks now compile on a route that runs on a laptop with three Twilio credentials, which the SIP route cannot do at all (a carrier opens SIP signalling to a public `sip:` URI and sends RTP over UDP; no HTTPS tunnel carries either). The two LiveKit routes are a trade-off, not a hierarchy: SIP buys LiveKit-maintained machinery, RTP media, cheaper minutes and any carrier; the connector buys a laptop.

- **N16 (added 2026-07-20).** Telephony compilation is scoped to
  LiveKit and Pipecat. A telephony target selects exactly one Connection and
  one exact `(orchestrator, transport, carrier)` route. Connections live in
  `connections/<name>.yaml`, contain environment variable names rather than
  values, and never repeat `carrier`; the target owns that choice. The first
  version permits one telephony Connection per target, but a package may
  declare any number of named targets and Connections. Each target produces a
  separate single-route artifact. Capability support is resolved per route and
  feature, never with carrier-wide booleans. See
  [TELEPHONY.md](./TELEPHONY.md).

---

## 3. Package layout

```text
agent.yaml            # everything in section 4
instructions.md       # complete prompt for the entry agent
agents/               # one .md per additional agent (T2)
tasks/                # one .md per task (T1)
tools/
  lookup_customer.yaml  # contract: input, output, execution, interruption, effect
  lookup_customer.py    # handler, code targets only
connections/
  primary_phone.yaml    # external telephony account, env names only
targets.yaml          # named target instances: provider, pins, destinations, model overrides
```

Rules:

- Secrets and credentials never appear in any file. Connection files and
  `targets.yaml` carry environment variable names and secret references only,
  never values.
- Remote IDs, deployment regions, SDK language, version pins, and carriers live in `targets.yaml`, never in `agent.yaml`. Model definitions live in `agent.yaml` (N15); `targets.yaml` only overrides one for a target that cannot run it.
- Machine sizes, replica counts, and GPU counts appear in neither file. They are derived and printed in reports.

---

## 4. agent.yaml

Named maps instead of lists, so every item has a stable identity and diffs stay readable. Durations use Go syntax (`90s`, `15m`, `1h30m`).

### 4.1 Top level

| Field | Required | Type | Tag |
|---|---|---|---|
| `version` | yes | int, must be `1` | core |
| `entry_agent` | yes | name of an agent | core |
| `models` | yes (N15) | four kind sections (`think`/`speak`/`listen`/`turn`), see 4.3 | core |
| `listen` | only when `models.listen` has 2+ entries | name of a listen model, see 4.2 | gated |
| `turn` | only when `models.turn` has 2+ entries | name of a turn model, see 4.2 | warn |
| `variables` | no | map, see 4.4 | core |
| `secrets` | no | list of env var names, see 4.12 | core |
| `agents` | yes, must include `entry_agent` | map, see 4.5 | core |
| `tasks` | no | map, see 4.6 | gated (T1) |
| `task_groups` | no | map, see 4.6 | gated (T1) |
| `controls` | no | map, see 4.7 | per kind |
| `tools` | no | list of plain tool names | core |
| `conversation` | no | block, see 4.8 | mixed |
| `tracing` | no | block, see 4.11 | gated |
| `channels` | yes, at least one | map, see 4.9 | core |
| `capacity` | see 4.10 | block | core |

There is no package-level `language` (N16, removed 2026-07-20). `language` is a per-model field (4.3): when a speak or listen entry sets it, the shipped Pipecat and LiveKit generators lower it through the catalogue entry's language slot; when unset, no language kwarg is emitted and the provider default or the model route's own encoding (`slng/deepgram/nova:3-en`) applies. Vapi and Deepgram remain unavailable in `unmute init` until their generators ship.

Top-level `tools` is the load manifest: only listed tool files are compiled into the package. Which agents and tasks can call a tool is decided by their own `tools:` lists (D8), never here.

### 4.2 listen, turn, and placement (amended 2026-07-19, N15)

There is no `pipeline` block. The think and speak roles ride each agent's `model:` and `voice:` references (section 4.5). Listen (speech to text) and turn (end-of-turn detection) are conversation plumbing shared by the whole package: one STT hears the call no matter which agent is active, so they are selected once, not per agent. Their models are defined in the `models.listen` and `models.turn` sections (4.3); the top-level `listen:` and `turn:` fields select one **by name**:

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `listen` | no when the section has at most one chain head (the sole head selects itself; entries named only in another entry's `fallback` list are chain members, not candidates); required with 2+ heads | name of a `models.listen` entry | gated | Swapping the STT is a one-line pointer change. Validation requires an effective listen model on every resolved target whose listen role is open (section 6.2 role table): Pipecat, LiveKit, Deepgram (Deepgram models only). |
| `turn` | same rule against `models.turn` | name of a `models.turn` entry | warn | A preference, not a promise, everywhere (previously N9). On targets where turn is integrated (Vapi, Deepgram) the effective entry carries settings only. |

`placement` says where a **model** runs, not where the agent runs, and keeps its two values (N1). It is never written in the common case: it derives from `provider`. `provider: local` runs on your own machines, next to the agent worker; any other provider is a hosted API endpoint. A model definition may state `placement:` explicitly to override the derivation (rare: a self-hosted deployment of a vendor's stack). Running the agent on a laptop and deploying it later changes nothing: a hosted provider calls the vendor in both places. (`provider` is a first-class field, exactly as the old target bindings carried it — only the file moved, section 4.3. The `model` identity is whatever that provider's SDK expects, forwarded verbatim: `gpt-4.1-mini` for OpenAI, `slng/deepgram/nova:3-en` for SLNG.)

The two jobs declared placement used to do survive unchanged, per target and more precise than the old global declaration:

- `provider: local` on listen or on a speak model fails loudly on the managed target (Vapi) and on Deepgram (no slot for an outside STT), exactly as before. `provider: local` on a think model fails on Vapi too (a documented custom LLM endpoint there is unverified). On Deepgram a custom think endpoint is fine.
- placement is the main input to sizing: hosted providers make vendor quotas the limit; `local` GPU models make GPUs dominate cost. Sizing reads each target's effective models (section 6.2).

### 4.3 models (amended 2026-07-19, N15)

One central map, grouped into four kind sections: `think` (LLM), `speak` (TTS), `listen` (STT), `turn` (VAD/end-of-turn). Every model the package can use is defined here, once, fully and concretely; there is no separate `voices:` block and no per-target binding step (a target may *override* an entry, section 6.2, but never defines one). Identities, providers, and settings are forwarded to the provider as-is and never validated (D10): the provider API and the generated project are the real validators.

Every entry carries `provider` (the catalogue vendor) and `model` (the identity that vendor's SDK expects), exactly as the old target bindings did — this is the pairing that already worked, moved into `agent.yaml`. `provider: local` marks an on-machine model (section 4.2).

A model's kind is its section, and its fields are validated against that kind. References must land in the right section:

- an agent's `voice:` names a `speak` entry; an agent's or task's `model:`, a transfer's `summarizer:`, and a `fallback` list name `think` entries; the top-level `listen:`/`turn:` pointers name entries of their sections (4.2).
- a name referenced but not defined is an error naming the reference's file:line; a reference into the wrong section is an error naming both kinds.
- names are one namespace across all four sections (N8): the same name in two sections is an error.
- the map is a **palette**: entries that nothing currently references are legal and stay in the file as swappable alternates (define `nova` and `soniox` under `listen`, flip the pointer to test). Only referenced or selected entries are compiled and forwarded; the rest are inert. Fast swapping is the point; production packages keep their alternates maintained in place.

Speak model fields:

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `provider` | yes | catalogue vendor, for example `slng` | core | `local` marks an on-machine TTS (section 4.2). |
| `model` | yes | model identity forwarded to the provider, for example `slng/deepgram/aura:2-en` | core | |
| `voice` | yes | voice id, forwarded as-is | core | |
| `speed` | no, default `1.0` | number | warn | Lowered through the catalogue entry's documented slot; warned where the provider has none (verify per provider, section 9). |
| `language` | no | BCP-47 tag | gated (N16) | Lowered through the catalogue entry's language slot only when set; when unset no language kwarg is emitted (N16). |
| `params` | no | open map, forwarded verbatim | core | |
| `description` | no | text | core | For humans only. |

Think model fields:

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `provider` | yes | catalogue vendor, for example `openai` | core | `local` marks a self-hosted LLM (section 4.2). |
| `model` | yes | model identity forwarded to the provider, for example `gpt-4.1-mini` | core | |
| `temperature` | no | number | core | Verified slots on all four (section 9): Vapi `assistant.model.temperature`, Deepgram `agent.think.provider.temperature`, constructor kwargs on Pipecat and LiveKit. |
| `top_p`, `top_k` | no | number | warn | Lowered through the catalogue entry's documented slot; warned where the provider has none (verify per provider, section 9). |
| `params` | no | open map, forwarded verbatim | core | Anything else the bound component accepts (`max_tokens` where a slot exists; never forwarded to Deepgram, which has no max-tokens slot). |
| `description` | no | text | core | For humans only. |
| `fallback` | no | ordered list of think model names | gated | Cycle-checked. Every model in a chain must land in the same slot kind and placement on the resolved target. All verified 2026-07-15. Deepgram: native (`agent.think` as an ordered provider array; mixed providers, per-entry params). LiveKit: native (`llm.FallbackAdapter`; STT/TTS adapters exist too). Pipecat: generated (the Pipecat driver v1 does not emit fallback yet — a maturity gate, not a platform limit; lifts when the driver emits it). Vapi: native (`model.fallbackModels`); entries are same-provider model IDs, so a **cross-provider chain fails on Vapi**; verified on OpenAI model schemas, others unverified. |

Listen model fields: `provider` (yes), `model` (yes; Deepgram models only on the Deepgram target), `language` (no; when unset no language kwarg is emitted, N16), `params` (no, forwarded verbatim), `fallback` (no; ordered list of listen model names, gated — see below), `description` (no).

Listen `fallback` (added 2026-07-19, T16): the chain stays within the listen section, is cycle-checked, and every entry must share the primary's placement. LiveKit: native `stt.FallbackAdapter` (verified in the livekit-agents source 2026-07-19; the driver emits it). Pipecat: gated, the driver does not emit listen fallback yet. Vapi: gated, no documented transcriber fallback slot. Deepgram: gated, `agent.listen` takes a single provider (unlike think's ordered array). Verify rows in section 9.

Turn model fields: `provider` (no), `model` (no, for example `silero`), `semantic_endpointing` (no: `required | preferred | off`, warn — forwarded as a preference; whether it applies depends on the listen model at runtime), `params` (no), `description` (no).

There is no `tier` field on models. Nothing would use it; Unmute never picks a model for you. `fallback` lives on think and listen models today (T16); speak stays fallback-free until a slot is verified somewhere beyond LiveKit's native TTS adapter — the sectioned shape takes it later with no new syntax.

### 4.4 variables

Typed shared state. Task results, handoff payloads, and personalization all flow through it.

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `type` | yes | `string \| number \| boolean \| integer` | core | |
| `default` | no | value of that type | core | |
| `source` | no | `call_start \| conversation \| session_id \| carrier \| connection \| call_id \| stream_id \| direction \| from_number \| to_number` | gated | `call_start` is caller input, dispatched with the call. `conversation` (N23) is saved by the model mid-call through the generated `update_variables` tool; it fails on Vapi and Deepgram. The remaining values are runtime-owned system sources and must be available on the exact route before the greeting. `stream_id` is optional only when no variable requests it. |
| `description` | no | text | core | What the value is. Feeds the generated capture tool's schema, the compile report, and the generated README (N23). |

A variable is referenced as `{{name}}` in four places (N23): `conversation.greeting.text`, agent and task instructions, tool `inject:` values, and `webhook.path`. The first two render once at session start and so may only name a variable that has a value by then; the last two render per tool call. A call-time render that hits an unset variable makes the tool refuse and tell the model what to ask for, rather than sending a half-formed request — except for a system variable, which no caller can be asked for.

Notes that drivers must respect: on Deepgram, live state lives in the generated bridge, never in template variables (those are substitution-time only and visible to project members; never route secrets through them).

### 4.5 agents

| Field | Required | Values | Tag |
|---|---|---|---|
| `instructions` | yes | path to a markdown file | core |
| `model` | yes | think model name (4.3) | core |
| `voice` | yes | speak model name (4.3) | core |
| `tools` | no | list of tool and control names | core |

Per-agent voices are native on LiveKit and Pipecat, and work on all four.

### 4.6 tasks and task_groups (T1)

A `task` is delegate-and-return: control comes back to the owning agent with a typed result.

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `instructions` | yes | path | | |
| `tools` | no | list of names | | |
| `model` | no | think model name (4.3) | gated | Per-task override. **Fails on Pipecat** — a maturity gate, not a platform limit (runtime-verified 2026-07-16: an `LLMSwitcher` inside an `LLMWorker` pipeline stalls all flow frames on pipecat-ai 1.5.0, so the driver has no working lowering; driver-pipecat B7. Review-corrected 2026-07-15 the other way on docs alone — the spike overrode it). |
| `result` | yes | flat map: name to `string \| number \| boolean \| integer \| {enum: [a, b]}` | core shape | Nested schemas only when every configured target is a code target. |
| `context` | yes | transfer context block without `variables` (N12), `history` required | gated | See 4.7 and the history table. |

Tier support for `task` itself: native on LiveKit. Conditional on Pipecat (needs a cascaded pipeline; Flows ships inside core `pipecat-ai` as `pipecat.flows` since 1.5.0, and the standalone `pipecat-ai-flows` package is deprecated, never used). Pattern on Deepgram (generated, fine). **Unverified on Vapi**: it is not proven that a handoff can go back to the previous assistant, so single-task delegates fail there until verified.

A `task_group` is an ordered list. No edges, no cycles possible.

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `steps` | yes | non-empty ordered list of task names | | |
| `context_scope` | yes | `shared \| isolated` | gated | `isolated` compiles only on code targets (on LiveKit as standalone AgentTasks, not TaskGroup). The group's scope overrides the member tasks' own `context` while inside the group. In history vocabulary: `shared` means each step sees the group context as `history: full`; `isolated` means each step enters as `reset`. |
| `then` | yes | `return \| transfer \| end` | gated | **`return` fails on Vapi** (a state-preserving Squad return is undocumented — gated as unverified, review-corrected 2026-07-15). |
| `then_target` | iff `then: transfer` | agent name | | |
| `merge` | no | `results` (only value) | core | On completion the owner receives the steps' typed results and nothing else (N13). On LiveKit `summarize_chat_ctx=False` is necessary but not sufficient: the driver also snapshots and restores the owner context around the group so only the results cross back (see N13). `summary` stays out: managed targets cannot host a summarizer. |

Never lowered to Vapi Workflows (they retire 2026-08-18).

### 4.7 controls

What the model can invoke besides plain tools. Controls share the tool name space (D8).

Common fields:

| Field | Required | Values | Notes |
|---|---|---|---|
| `kind` | yes | `delegate \| agent_transfer \| human_transfer` | |
| `when` | no | text | The trigger description the LLM sees. Lowered into the tool or edge description. |

`kind: delegate`:

| Field | Required | Notes |
|---|---|---|
| `task` or `group` | exactly one | Name of a task or task group. |
| `assign` | no, task only (N5) | Map of `variable_name: result.<field>`. The field must exist in the task's `result` and match the variable's type. |

A delegate hands work off; whether control comes back is decided by the target. A single task always returns. A group returns only when its `then:` is `return`; with `then: transfer` or `then: end` the delegate never returns, and the generated tool description must say so.

`kind: agent_transfer` (T2, works on all five):

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `to` | yes | agent name | core | |
| `requires` | no | list of variable names | gated | A machine-checked guard. Generated on code targets; **fails on Vapi** (no mechanism). On a failed guard the model gets a refusal naming the unmet variables; that behavior is part of the contract. |
| `context.history` | yes | `full \| messages \| last_n \| summary \| reset` | gated | See the history table below. |
| `context.max_messages` | iff `last_n` | int | | Illegal with any other value. |
| `context.summarizer` | iff `summary` is generated | think model name (4.3) | | So the model is defined and counted by sizing. |
| `context.include_tool_calls` | no, default `true` | bool | gated | `false` works on code targets only. |
| `context.variables` | yes | `all` or a list of names | gated | `all` is the only value managed targets accept. Lists compile on code targets only. |

Transfer history support per target (Vapi column verified against provider docs 2026-07-15):

| Value | LiveKit | Pipecat | Vapi | Deepgram |
|---|---|---|---|---|
| `full` | ok | ok | ok | ok |
| `messages` | ok | ok | ok | ok |
| `last_n` | ok | ok | ok | ok |
| `summary` | ok (generated) | ok (generated) | fails | ok (generated) |
| `reset` | ok | ok | ok | ok |

`reset` never promises a literally empty context; on LiveKit a handoff marker still lands in the new context. The Pipecat driver v1 emits `history: full` only — the other values (and `context.variables` subset, `include_tool_calls: false`) are a maturity gate (§9); the workers handoff carries the running context.

Vapi lowering, literal spellings verified 2026-07-15: `contextEngineeringPlan` is one of `all` (their default; ours is no default, D7), `userAndAssistantMessages`, `lastNMessages` plus `maxMessages`, `none`; no summary mode exists; `previousAssistantMessages` stays unexposed. For tasks this table collapses: code targets support all five values (generated), and Vapi is n/a while single tasks fail there.

`kind: human_transfer` (rewritten 2026-08-10, N25; `destination` moved into the block 2026-08-11, N27; full design in [TRANSFERS.md](TRANSFERS.md)):

`kind` and `when`, then **exactly one block named after the transfer shape**, carrying every parameter of the transfer. There is no `mode:` field: a warm-only field on a cold transfer is unwritable rather than rejected by a rule, exactly as N19 decided for tool execution.

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| one shape block | yes, exactly one | `cold: \| warm:` | gated | Zero blocks and two blocks both fail at load with file:line. A block always has a body, because `destination` lives in it. |

Fields inside either block:

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `destination` | yes | symbolic name | core | Resolves through the target instance's `destinations:` map to a number or SIP URI. |
| `ring_timeout` | no | duration | gated | How long to wait for the person to pick up. Omitted from the emitted call when unset, so the platform default applies (LiveKit: 30s; the Pipecat Twilio route: Twilio's own 60s dial timeout). |
| `on_unavailable` | no, default `return_to_caller` | `return_to_caller \| hangup` | gated | One concept covering every way the person does not take the call: no answer, declined, voicemail, dial failure. LiveKit surfaces all four as one `ToolError`, so the lowering is one branch, not four. |

Fields inside `warm:` only:

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `briefing` | no | text | gated | What the agent tells the person before bridging. **Free text, not an enum** (N25). Lowers to LiveKit's `instructions=WorkflowInstructions(extra=...)` and to the generated Pipecat briefing instruction; the conversation transcript is interpolated alongside it by both drivers, so no summarizer model is involved. |

Shape support per target (corrected 2026-08-12, N34): **`cold`** is LiveKit native on `(livekit, sip)`, Vapi native, and Pipecat native on Daily SIP. Deepgram is carrier-conditional in the bridge. Pipecat's carrier-WebSocket routes emit **no** cold transfer: the carrier-REST version this paragraph used to describe was built, live-tested, and deleted (N31), and Pipecat's own documentation is that its websocket transports carry no call-transfer control. **`warm`** (review-corrected 2026-07-15; Pipecat route re-scoped 2026-08-10; corrected again 2026-08-12) is LiveKit native on `(livekit, sip)`, stable on Node and `beta.workflows` on Python (NOT Python-only). On Vapi the stable `transferPlan` path needs carrier Twilio.

On Pipecat, warm is **documented by the platform on the Daily route and not emitted by this project yet**. Two claims that need keeping apart. Daily supports both transfer patterns, cold and warm (`docs.pipecat.ai/pipecat/telephony/daily-pstn`, verified 2026-08-12), so the platform is not the limit. What is true is that the warm pattern requires the generated bot to own audio control, through a transfer coordinator, a hold-music mixer, and per-leg audio gates, and this project has not built that yet. It is a planned follow-up feature. The two-socket carrier-WebSocket bridge previously described here was deleted with the rest and is not coming back on those routes, where the audio-path rule was bought with real live-test failures ([TRANSFERS.md](TRANSFERS.md) section 1).

A warm transfer promises the caller's experience, not the agent's exit, which is why the two drivers can finish it with different machinery. On LiveKit the agent moves the person into the caller's room and shuts down, so caller and person continue alone. On Daily, whenever this project emits warm there, the bot stays in the room as the audio bridge, because the room is the bridge and the bot owns it. What both promise is that the caller stops hearing the agent and starts hearing the person. (The Pipecat half of this paragraph described the deleted carrier-WebSocket bridge until 2026-08-12; see N34.)

### 4.8 conversation

Outcomes, not provider knobs. All lifecycle fields are gated per target.

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `greeting.speaks_first` | yes, if block present | `agent \| user` | `agent` core, `user` warn | `user` means the agent stays silent until the caller talks. Native on Vapi (`firstMessageMode: assistant-waits-for-user`), verified 2026-07-15. Generated on LiveKit and Pipecat: no opening is emitted. **Warns on Deepgram**: behavior of an omitted `agent.greeting` is undocumented; the driver smoke test must prove silence. |
| `greeting.text` | no | text | core | Exact opening line, spoken word for word, every call. Verified 2026-07-15: Vapi `firstMessage`, Deepgram `agent.greeting`. Generated on LiveKit and Pipecat. May reference `{{variables}}` available at call start. |
| `interruption.enabled` | yes, if block present | bool | core | |
| `interruption.minimum_words` | no | int | warn | Lossy on Deepgram (model halts first): warns. |
| `interruption.ignore_phrases` | no | list of text | warn | Native on Vapi. Generated on LiveKit and Pipecat. Dropped with a warning on Deepgram. |
| `inactivity.nudge_after`, `inactivity.end_after` | no | durations | warn | Range-checked per target by the driver. |
| `max_duration` | no | duration | warn | Some providers have no cap knob; the driver gates and documents it. |
| `thinking_audio` | no | `none \| subtle` | gated | Native on LiveKit and Pipecat. **Fails on Deepgram and Vapi** (no faithful lowering). The Pipecat driver v1 does not emit thinking_audio yet (a maturity gate). |

The three greeting combinations:

- `speaks_first: agent` with `text`: fixed opening, same words every call. Works on all four.
- `speaks_first: agent` without `text`: the model writes the opening from the prompt, so it varies per call. Generated on LiveKit and Pipecat; native on Vapi (`firstMessageMode: assistant-speaks-first-with-model-generated-message`, verified 2026-07-15). **Generated with a warning on Deepgram** (review-corrected 2026-07-15: inject a synthetic turn at call start via `InjectUserMessage`/`InjectAgentMessage`, documented for orchestrated openings, though not framed as a first-class greeting mode).
- `speaks_first: user`: native on Vapi (verified 2026-07-15), generated on LiveKit and Pipecat, warns on Deepgram (omission behavior undocumented).

If the `greeting` block is absent, the agent speaks first with a model-written opening: the two drivers that ship a generator (LiveKit, Pipecat) lower an absent block exactly like `speaks_first: agent` without `text`, so one source cannot open a call differently across them (N20). It still warns, because the remaining two targets do not share that default: Vapi is managed and applies its own native default, and Deepgram's behavior for an omitted `agent.greeting` is undocumented (N6) and its driver is unwritten.

### 4.9 channels

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `kind` | yes | `realtime_audio \| telephony` | core | |
| `inbound`, `outbound` | yes, telephony only | bool | gated | `outbound: true` requires all `source: call_start` variables satisfiable, and does **not** require `on_voicemail` (N29). A **warm** human transfer requires `outbound: true`, because warm dials its destination and a package must not claim it never places calls while emitting code that does (N30). LiveKit SIP emits both directions offline for Twilio, Telnyx, and Plivo; each exact route remains provisional. Pipecat's carrier-WebSocket adapters and the LiveKit Twilio connector emit inbound and outbound paths offline, and both directions validate (N29 removed the voicemail precondition). Vapi supports both directions; Deepgram emits outbound with a carrier-conditional warning. |
| `required_controls` | no | list from the control vocabulary | gated | Vocabulary: `cold_transfer, warm_transfer, dtmf_send, dtmf_receive, hold, hangup, voicemail_detection, ivr_navigation`. Resolved against the target's carrier and transport, never the provider brand alone. |
| `on_voicemail` | no (N29) | `hangup \| leave_message` | gated | LiveKit SIP emits both values through answering-machine detection for Twilio, Telnyx, and Plivo, with each exact route provisional. Pipecat has a verified `VoicemailDetector` platform path, but its carrier-WebSocket adapters do not emit that lowering yet. Vapi supports both values; Deepgram emits them with a carrier-conditional warning. |

### 4.10 capacity

The declared half of the resource model. Required whenever `channels` has a telephony channel or the resolved target is a code target.

| Field | Required | Type | Notes |
|---|---|---|---|
| `peak_sessions` | yes | int | Concurrent conversations at busy hour. Must not exceed `max_sessions`. |
| `max_sessions` | yes | int | Hard admission limit. Reject above it before Agent allocation; don't queue. |
| `peak_starts_per_second` | yes for telephony | number, greater than zero | Peak call-start rate. Report separately from concurrent sessions. |
| `avg_session_duration` | yes | duration | Sizing and quota input. |

Sizing depends on concurrency, placement, and channels. It does not depend on how many agents are in the file, and never on the provider brand alone. Derived numbers (workers, GPUs, quotas) are printed in the compile or plan report with dated benchmark coefficients, marked `unbenchmarked` until measured.

### 4.11 tracing (added 2026-07-20)

Tracing is package-wide and strictly opt-in. The only v1 shape is one object,
not a list:

```yaml
tracing:
  provider: langfuse
```

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `provider` | yes | `langfuse` | gated | LiveKit and Pipecat emit the integration. Vapi and Deepgram fail validation before any artifact. |

With no `tracing` block, generated projects contain no Unmute tracing setup,
Langfuse/OpenTelemetry-only imports or dependencies, Langfuse environment
variables, tracing hooks, or tracing documentation. Environment variables do
not enable an integration the package did not request.

With `provider: langfuse`, the generated runtime requires
`LANGFUSE_PUBLIC_KEY`, `LANGFUSE_SECRET_KEY`, and `LANGFUSE_BASE_URL`. Missing
or empty values fail agent startup, including when all three are absent. The
credentials select the Langfuse project; v1 adds no project name, trace name,
sampling, endpoint, or environment-variable override fields. Drivers keep
their existing trace/session naming and span mappings.

This stays schema version 1. Existing packages that want to retain the former
automatic tracing output add the block above. `unmute init` omits it; the
checked-in packages under `examples/` include it.

### 4.12 secrets (added 2026-08-10, N24)

The runtime environment values the package needs, as a **list of environment
variable names**. A secret has no fields, so there is nothing a value could be
written into (D12).

```yaml
secrets:
  - SALON_API_URL
  - SALON_API_TOKEN
  - OPENAI_API_KEY
```

Every listed name is required: a generated runtime refuses to start without it,
naming it. A name listed twice is an error, because a repeat is a typo.

Each name must be `UPPER_SNAKE`, the same rule every `*_env` field follows (N19),
so a pasted URL or secret fails validation instead of becoming a lookup that
fails at call time. Secrets never flow through `{{...}}` templates (N23): they
reach the call through `*_env` fields, the generated auth helpers, and
`os.environ` in a local handler. Each code target's build writes a
`.env.example` from this block. An env name the package references but never
declares is a warning on stderr, exit 0.

The cross-check reads the **body of every local handler** as well as the YAML
sites, matching `os.environ["X"]`, `os.environ.get("X")`, and `os.getenv("X")`
for `UPPER_SNAKE` names (amended 2026-08-11). A handler owns its own request, so
its credential is named in Python rather than in a `*_env` field, and leaving
that path unchecked was the one way a declared-secrets package could still fail
on its first tool call. The scan is a text match, not a Python parse, so a name
in a comment counts: the check over-reports rather than under-reports, since a
spurious line costs one extra declaration and a miss costs a live call.

---

## 5. tools/*.yaml

The file name is the tool name (N4). The top level is the contract with the
model plus the two conversation scalars; **how the tool runs lives in exactly
one execution-keyed block** (rewritten 2026-08-10, N19, compiler T23). The block name
is the execution kind, so a field belonging to another kind is unwritable rather
than merely rejected. Which agents see the tool is decided only by their
`tools:` lists in `agent.yaml`, never here.

```yaml
description: Search for a place by name, type, or area.

input:
  type: object
  properties:
    query:
      type: string
      description: 'e.g. "tapas bar in Madrid"'
  required:
    - query

webhook:
  url_env: LOOKUP_PLACES_URL
  auth:
    type: bearer
    token_env: LOOKUP_PLACES_TOKEN

effect: returns_data
interruption: provider_default
```

### 5.1 Top level

| Field | Required | Values | Tag | Notes |
|---|---|---|---|---|
| `description` | yes, except `builtin` | text | core | What the LLM reads. Optional for a `builtin` tool, where the prebuilt registry supplies a default and this text is added on top. |
| `input` | yes, except `builtin` | JSON Schema object | core | The parameters the model fills in; lowers natively everywhere (N10). A `builtin` tool has no `input`: the prebuilt owns its schema. |
| `output` | no | JSON Schema object | warn | Declared and carried into the compile report, but **not enforced anywhere yet** (N22): no driver reads it. Only Vapi warns, and the tag is `warn` on that basis alone. Not legal on a `builtin` tool. |
| one execution block | yes, exactly one | `webhook \| local \| mcp \| builtin \| client \| provider_hosted` | see 5.2 | Zero blocks and two-or-more blocks both fail at load with file:line. |
| `inject` | no | flat map of request key to scalar | gated (N23) | Values merged into the call and **never shown to the model**, so it can neither see nor overwrite them: the place a user id or a captured slot rides along. A string value may hold `{{variable}}` tokens; a value that is exactly one token keeps the variable's declared type. A key here may not also appear in `input.properties`. Legal on `webhook` and `local` only — the two kinds whose request unmute builds itself. Fails on Vapi and Deepgram. |
| `interruption` | no, default `provider_default` | `continue \| cancel \| provider_default` | warn | Honored on Pipecat (`cancel_on_interruption`); LiveKit runs tools to completion, so non-default values warn there (2026-07-16). On managed targets only `provider_default` means anything; other values warn. |
| `effect` | no, default `returns_data` | `returns_data \| ends_conversation` | core | Fixed by the registry for a `builtin` tool (`end_call` implies `ends_conversation`); a conflicting value fails. |

### 5.2 Execution blocks

| Block | Fields | Gating |
|---|---|---|
| `webhook:` | `url_env` (required), `path` (optional), `auth` (optional, 5.3) | works everywhere. **The safe choice.** `path` is appended to the env base URL, must start with `/`, may hold `{{variable}}` tokens whose rendered values are URL-encoded, and fails on Vapi and Deepgram (N23). |
| `local:` | `handler` (path, default `tools/<name>.py`) | code targets only |
| `mcp:` | `url_env` (the MCP server address) | **fails on Deepgram** (no runtime MCP client); on LiveKit needs `sdk_language: python` (driver-livekit B3, 2026-07-16). The block where MCP auth would land later. |
| `builtin:` | `id` (prebuilt registry id, v1 `end_call`), `instructions` (optional closing line) | LiveKit and Pipecat host the registry; **fails on Vapi and Deepgram**. LiveKit lowers `end_call` to the beta `EndCallTool`, Pipecat to a bodyless end tool. Unknown id fails with file:line. |
| `client: {}` / `provider_hosted: {}` | none | gated per driver; not part of the safe core |

`url_env`, `handler`, and every `*_env` are **names, never values**: an env var
name is `UPPER_SNAKE`, so a pasted URL or secret fails validation rather than
landing in the spec.

The Pipecat driver v1 emits the `webhook`, `local`, and `builtin` blocks
(amended 2026-07-17, driver-pipecat T14: `local` lowers to the same `@tool`
method, body awaiting the user handler from `tools/<name>.py`; `builtin` added
2026-07-22, prebuilt-tools T6); `mcp` stays maturity-gated there until the
driver emits it.

### 5.3 Webhook auth (added 2026-08-10, compiler T23; hmac removed 2026-08-10)

`webhook.auth` authenticates the POST. `type` selects the scheme, and every
other field belongs to exactly one scheme — a field from the other scheme is an
error, never silently ignored. The token is always an env var name.

| Scheme | Fields | Sends |
|---|---|---|
| `bearer` | `token_env` | `Authorization: Bearer <token>` |
| `api_key` | `token_env`, `header` (default `X-API-Key`) | `<header>: <token>` |

```yaml
webhook:
  url_env: LOOKUP_PLACES_URL
  auth:
    type: api_key
    token_env: LOOKUP_PLACES_API_KEY
    header: X-API-Key
```

Rules and lowering:

- Request signing (HMAC) and OAuth2 are **not in v1**, and neither is `basic`. A
  tool that must sign its request uses a `local:` Python handler until a scheme
  is specified.
- Both code targets emit one helper per scheme actually declared, so a project
  with no auth carries no auth code. A managed target configures its tool auth
  provider-side, so `tools.auth` is **ok on LiveKit and Pipecat, fail on Vapi
  and Deepgram**.

---

## 6. targets.yaml

Named target instances: which orchestrator runs the package, and the infrastructure facts that only make sense per target (amended 2026-07-19, N15 — model definitions moved to `agent.yaml` section 4.3; here they can only be overridden).

### 6.1 Instance fields

| Field | Required | Notes |
|---|---|---|
| `provider` | yes | `livekit \| pipecat \| vapi \| deepgram` for now (N17). |
| `version` | code targets | Framework pin. The driver checks it against the range its templates support. A codegen check, not model validation. |
| `pins` | no | Independently versioned packages (LiveKit plugins) get their own entries. Pipecat Flows no longer qualifies: it ships inside `pipecat-ai` core since 1.5.0; never pin the deprecated standalone `pipecat-ai-flows`. |
| `sdk_language` | no | LiveKit: warm transfer and MCP need `python`. |
| `transport`, `carrier`, `connection` | required for LiveKit or Pipecat telephony | Driver route vocabulary and a Connection name. Telephony features resolve against the exact tuple, never the orchestrator or carrier alone. |
| `deployment_region` | no | Where the target platform runs the deployed agent: one region, or a list of them (N18, widened by N32). Provider vocabulary, forwarded as declared, never validated or derived. Pipecat: the `region` key in the emitted `pcc-deploy.toml`, exactly one; a list of more than one is a gated error, and a second region is a differently named agent. LiveKit: the `--region` flag on the `lk agent create` command in the generated README, one deployment per declared region (create-time, immutable). A model's own service region rides its `params`/`endpoint_env` instead. Replaces the retired `region`/`edition` fields (N17). |
| `models` | no | Per-target overrides, see 6.2. |
| `destinations` | if any `human_transfer` is used | Map of symbolic name to an E.164 number, a `sip:`/`sips:` URI, or the UPPER_SNAKE name of an environment variable holding one of those (N24). The three are told apart by shape. A named variable rides into `.env.example` and the required-env list; the model still only ever picks the symbolic name. |

### 6.2 Overrides (amended 2026-07-19, N15)

The instance's `models:` map is optional and holds overrides only, for a target that cannot run an entry defined in `agent.yaml` (a `local` model on the managed target). Keys are model names from any section of `agent.yaml` 4.3 — names are one namespace, so the map stays flat and the kind comes from the definition. An override **replaces the whole entry** with the same shape and the same kind; there are no field-level merge rules to reason about. The effective model for a target is the override when present, the `agent.yaml` definition otherwise; every gate and sizing input below reads effective models. Selection is package-level: a target changes *what a name means* for itself, never which name is selected.

Each role is **open** or **integrated** per target:

| Role | LiveKit | Pipecat | Vapi | Deepgram |
|---|---|---|---|---|
| `listen` | open | open | open | open, Deepgram models only |
| `turn` | open | open | integrated | integrated, rides the listen entry's `params` |
| `speak` | open | open | open | open, Deepgram plus a fixed third-party list |
| `think` (formerly `reason`) | open | open | open | open, custom endpoints allowed |

Rules:

1. Every used model, and `listen` on every open-listen target, must have an effective definition. Without one there is nothing to emit; the error names the model and the target.
2. On a target whose role is integrated, the effective entry for that role carries settings for the built-in part only, and can never name an outside model.
3. Definitions and overrides carry their settings as typed fields (sections 4.2, 4.3) plus `params:`, an open map for the bound component's own remaining settings (audio format, turn thresholds where the provider puts them). Forwarded as-is, **never validated**. They configure only the bound component; platform and telephony settings can never ride through them.
4. Placement is derived from the effective entry's `provider` (`local` means `local`, anything else `api`; explicit `placement:` overrides) and gates per section 4.2.
5. If a driver has no slot for a value (a third-party listen model on Deepgram), compilation fails: the value has nowhere to go. That is a structural fact, not a judgment about the model.
6. Every forwarded model and param is listed in the compile or plan report, so what was sent is always inspectable. Some providers keep fields that do nothing, so run the agent to be sure. That is the contract.
7. An override naming a model that `agent.yaml` does not define, or changing its kind, is an error.

Why never validated: provider model lists change faster than any shipped catalog, the valid set on code targets depends on the pinned versions, and the real validators already exist (the provider API at plan/apply, the generated project at startup). Unmute relays those errors word for word.

### 6.3 Telephony connections and routes

Telephony Connections keep account-specific environment names outside the
portable Agent and outside target route selection:

```yaml
# connections/primary_phone.yaml
kind: telephony
environment:
  account_sid: TWILIO_ACCOUNT_SID
  auth_token: TWILIO_AUTH_TOKEN
  from_number: TWILIO_PHONE_NUMBER
```

The target binds that Connection to an exact route:

```yaml
targets:
  pipecat:
    provider: pipecat
    version: "1.5.0"
    transport: carrier-websocket
    carrier: twilio
    connection: primary_phone
```

A package may add more named targets and Connections for every supported
carrier route it needs. There is no package-level route-count field. The
one-Connection rule applies to each target, not to the whole package, and each
target writes a separate `build/<target-name>/` artifact. One target never
combines carriers or their route-specific limits.

The Connection's keys use carrier route vocabulary. Its values must be valid
environment variable names. Unknown keys, a missing required key, an unknown
Connection, or a route mismatch fails before generation. The loader never
reads the named environment values.

The compiler resolves directions, controls, briefing behavior, system variable
sources, evidence, public endpoints, coordination, and admission ownership for
the exact route. Every LiveKit or Pipecat v1 telephony plan resolves shared
coordination and emits Redis. LiveKit Server and LiveKit SIP use it as platform
infrastructure. Generated Pipecat code uses it only for pending-call
correlation, callback replay protection, human-transfer state, and admission;
agent handoff, tasks, transcripts, prompts, model context, and audio stay in the
active worker. The plan and compile report name the applicable reasons from the
closed set `livekit_control_plane`, `call_correlation`,
`callback_idempotency`, `human_transfer`, and `admission`. A feature is enabled
only when its row has current official documentation and a successful route
smoke. The reason set is non-empty and every reason maps to a declared service
that consumes the Redis connection; an idle Redis sidecar is invalid. Without a
live smoke, the row remains provisional and validation fails when the Agent
requests it.

---

## 7. The safe core: write this and it runs on all four

The subset that passes validation on every primary target. The regression
fixture in [internal/testdata/safe_core/](./internal/testdata/safe_core/)
follows these rules exactly.

1. Any number of agents with `agent_transfer` between them (T0 + T2).
2. Every transfer context: `history: full`, `variables: all`.
3. Tools: a `webhook:` block, `interruption: provider_default`, `effect: returns_data`.
4. Omit human transfer while the exact LiveKit and Pipecat telephony routes are
   provisional. Their platform-level and offline-emitted capabilities remain
   visible in the matrix below, but are not part of the validation-safe core.
5. Hosted providers only for listen and speak models (no `provider: local`). `turn` is a preference anyway.
6. If the agent speaks first, give it a fixed `greeting.text`. A model-written opening is generated-with-warning on Deepgram (review-corrected 2026-07-15); a fixed line stays the zero-warning safe choice.
7. Skip for now: single `tasks` (return to owner unverified on Vapi) and `task_groups` with `then: return` (fails on Vapi). A `task_group` with `then: transfer` or `end` does pass on all four (warning on LiveKit: TaskGroup experimental). Also skip `requires`, `thinking_audio`, telephony routes, warm transfer, `mcp` and `local` tools, tracing, and any history other than `full`. `fallback` passes everywhere when the chain stays within one provider on Vapi. Pipecat's current carrier routes do not emit the required voicemail handling, and every exact telephony route remains provisional until its credentialed smoke passes.
8. Accept warnings: interruption tuning on Deepgram, turn model notes.
9. No `{{variable}}` template in any prompt or greeting, no tool `inject:`, no
   `webhook.path`, and no `source: conversation` variable: each fails on Vapi and
   Deepgram (N23/N24). The four-target fixture cannot carry a feature two targets
   refuse; a package targeting only LiveKit and Pipecat uses them freely.

Feature by feature:

| Feature | LiveKit | Pipecat | Vapi | Deepgram |
|---|---|---|---|---|
| single agent (T0) | ok | ok | ok | ok |
| agent_transfer, `full` + `all` | ok | ok | ok | ok |
| fixed opening line (`greeting.text`) | ok | ok | ok | ok |
| model-written opening (no `text`) | ok | ok | ok | generated (warn) |
| user speaks first (`speaks_first: user`) | ok | ok | ok | warn |
| task | ok | ok | unverified | ok |
| task_group, `then: transfer\|end` | warn | ok | ok | ok |
| task_group, `then: return` | warn | ok | fail | ok |
| history `messages` / `last_n` / `reset` | ok | gated (v1) | ok | ok |
| history `summary` | ok | gated (v1) | fail | ok |
| `requires:` | ok | ok | fail | ok |
| `fallback:` (think) | ok | gated (v1) | conditional | ok |
| `fallback:` (listen) | ok | gated (v1) | fail | fail |
| human_transfer cold (`cold:`) | ok on `(livekit, sip)` | Daily SIP only; the carrier-WebSocket routes emit none (N34) | ok | carrier-conditional |
| human_transfer warm (`warm:`) | native (Node stable, Python Beta) on `(livekit, sip)` | not emitted yet. Daily documents warm and this project has not built it (a planned follow-up feature); the carrier-WebSocket routes have no transfer control at all (N34, [TRANSFERS.md](TRANSFERS.md)) | Twilio only (stable path) | carrier-conditional |
| `thinking_audio` | ok | gated (v1) | fail | fail |
| `provider: local` (listen/speak) | ok | ok | fail | fail |
| webhook tools | ok | ok | ok | ok |
| webhook `auth` (bearer/api_key) | ok | ok | fail | fail |
| `{{variable}}` in prompts/greeting | ok | ok | fail | fail |
| tool `inject:` | ok | ok | fail | fail |
| `webhook.path` | ok | ok | fail | fail |
| `source: conversation` (+ generated `update_variables`) | ok | ok | fail | fail |
| `secrets:` block (`.env.example`, startup check) | ok | ok | n/a | n/a |
| mcp tools | Python only | gated (v1) | ok | fail |
| outbound + `on_voicemail` | ok | gated (v1) | ok | generated (warn) |
| tracing `provider: langfuse` | ok | ok | fail | fail |

---

## 8. Not in v1

- Branches, routers, backtracking, any graph beyond linear task groups.
- Integrated speech-to-speech models (one model doing listen, think, and speak). Cascaded only in v1.
- Parallel conversational agents and supervisor logic as schema constructs.
- `merge: summary` and any automatic transfer summarization.
- Cross-session variable persistence and history retention toggles.
- Multiple tracing providers, simultaneous trace export, provider options, and
  a generic module registry.
- Force-interrupt phrase lists, pacing knobs, canned audio steps, external mid-call event injection. All target native.
- Messaging channels, batch outbound campaigns, IVR navigation as behavior.
- Machine sizes, replica counts, GPU types or counts, anywhere.
- Automatic telephony provisioning. Carriers and trunks are target configuration.

---

## 9. Verify before these harden

Open items that can loosen or tighten a gate above. None of them changes the shape of the schema; they change tags.

Resolved by the 2026-07-15 research pass (context7 plus official docs; exact fields recorded in the sections above):

- `fallback` on all five primaries: LiveKit `FallbackAdapter` (LLM, STT, TTS; Python and Node); Vapi `model.fallbackModels` (same-provider chains, verified on OpenAI model schemas); ElevenLabs `backup_llm_config` (`preference: override` with ordered `order`, `cascade_timeout_seconds`; no per-entry params); Deepgram `agent.think` ordered provider array (mixed providers, per-entry params). Pipecat stays generated.
- `speaks_first: user` on ElevenLabs: an empty `first_message` is documented as "the agent waits for the user to start the discussion". Native.
- Voicemail on LiveKit (`AMD`), Pipecat (`VoicemailDetector`), Vapi (`voicemailDetection` + `voicemailMessage`), ElevenLabs (`voicemail_detection` system tool + `voicemail_message`): both `hangup` and `leave_message`. Outbound unblocked (N6).
- Generation param slots: Vapi `assistant.model.temperature` and `maxTokens`; ElevenLabs `conversation_config.agent.prompt.temperature` (default 0) and `max_tokens`; Deepgram `agent.think.provider.temperature` (**no max-tokens slot exists**, do not forward one). Forwarded-verbatim stance unchanged.
- Pipecat custom STT/TTS: `OpenAISTTService` and `OpenAITTSService` take a documented `base_url` override for OpenAI-compatible endpoints; service subclassing only for other protocols. (Later same day: SLNG STT/TTS ships first-class as `pipecat-slng` / `livekit-plugins-slng`, mapped by the provider catalogue, so the `base_url` path serves generic custom endpoints, not SLNG.)
- Pipecat Flows: ships inside core `pipecat-ai` (`pipecat.flows`) since 1.5.0; the standalone `pipecat-ai-flows` package (last 1.4.0) is deprecated and never used.
- Deepgram reusable agent configurations: exist and are immutable (delete and recreate to change; referenced as a UUID string in place of the `agent` object). Decision stands: compile to inline `Settings`; immutability makes reusable configs create-per-change churn with no compile-time benefit.

Still open:

| Item | Field affected | Today's stance |
|---|---|---|
| Vapi: can a handoff return to a previously active assistant? Docs silent both ways (no destination restriction documented; `previousAssistantMessages` implies prior sessions exist). Needs an empirical test, not more reading. | `tasks` on Vapi | fails |
| Model-written opening on ElevenLabs and Deepgram | `greeting` without `text` | **review-resolved 2026-07-15**: ElevenLabs conditional via Workflow `entry_behavior: generate_immediately`; Deepgram generated via `InjectUserMessage`/`InjectAgentMessage` (warn) |
| Deepgram: behavior of an omitted `agent.greeting` (silence assumed, undocumented) | `speaks_first: user` on Deepgram | warns; prove in driver smoke test |
| Deepgram voicemail: bridge plus carrier AMD lowering | `on_voicemail`, `outbound` on Deepgram | **review-resolved 2026-07-15**: Deepgram publishes an official AMD-bridge outbound reference impl (Twilio async AMD → hangup + leave-message); generated, carrier-conditional (warn) |
| In-flight tool calls on barge-in, managed target: Vapi docs silent | `interruption` | `provider_default` only |
| Vapi `fallbackModels` on non-OpenAI model schemas | `fallback` on Vapi | conditional, same-provider chains |
| Speak `speed` and think `top_p`/`top_k`: which providers document a slot (added 2026-07-19, N15; verify per catalogue entry before hardening) | `speed`, `top_p`, `top_k` | warn: lowered where the catalogue entry documents a slot, warned where none exists |
| Listen fallback slots beyond LiveKit: Vapi transcriber fallback, Deepgram multi-provider listen (added 2026-07-19, T16) | `fallback` on listen | gated on Pipecat/Vapi/Deepgram until a slot is doc-verified |

**Driver maturity gates (tags tightened until a driver emits the feature).** A code target may support a feature at the schema level while its first driver has not emitted the lowering yet. Like warm transfer (§4.7), these are gates on the driver, not the platform, and lift when the driver emits the lowering:

| Driver | Gated until emitted | Where |
|---|---|---|
| Pipecat v1 | `models.fallback`, `thinking_audio`, `outbound` + `on_voicemail`, `mcp` tools, warm transfer on every route (Daily documents it, this project has not built it, a planned follow-up feature; the carrier-WebSocket routes have no transfer control at all) and cold transfer off the Daily route ([TRANSFERS.md](TRANSFERS.md), N34); transfer/task context shaping beyond the safe-core defaults — `history` other than `full`, `context.variables` subset, `include_tool_calls: false` (the workers handoff carries the running context; fine-grained shaping is not emitted yet). (`local` tools lifted 2026-07-17.) | [internal/generate/pipecat_v1.go](../internal/generate/pipecat_v1.go). Emitted: single agent, `agent_transfer` (+ `requires` guard), `tasks`, `task_groups` with `context_scope` (shared/isolated), `then` return/transfer/end, `local` tools (2026-07-17). |

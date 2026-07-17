# SPEC — Unmute console (create + maintain)

Consumes the core: [compiler.md](compiler.md). Driver lowering stays in [driver-pipecat.md](driver-pipecat.md), [driver-livekit.md](driver-livekit.md), [driver-deepgram.md](driver-deepgram.md), [driver-vapi.md](driver-vapi.md), and [driver-elevenlabs.md](driver-elevenlabs.md). Provider facts live in [PROVIDER_CATALOG.md](../../PROVIDER_CATALOG.md) + `internal/target/catalog_*.go`; schema truth remains [SCHEMA.md](../../SCHEMA.md). This spec owns the interactive console in `internal/tui` + its `internal/cli` entry/actions, not schema or driver lowering.

## §G goal
Make the TUI the Unmute console: bare `unmute` on a TTY opens **Create a new agent | Open an existing agent | Quit**. Create edits `scaffold.Data` in memory and writes only after confirmation. Open discovers an existing v1 package, loads it through `spec.Load`, exposes the same section editors, and adds Validate / Compile / Save without leaving the console.

## §C constraints
- C1: entry — bare `unmute` with no args launches the console iff stdin AND stdout are character devices. Non-TTY/scripted bare invocation prints Cobra help with the existing exit codes. `unmute init [name]` remains; interactive `unmute init` enters Create directly. Cobra rules hold: fresh `newRootCmd()`, `RunE`, `cmd.OutOrStdout()` / `cmd.ErrOrStderr()`, no `os.Exit` outside `main.go`, `SilenceUsage` + `SilenceErrors`.
- C2: maintenance policy is **regenerate (a)**. A changed Save regenerates package files from `internal/scaffold` templates; hand edits to rewritten generated files are overwritten only after an explicit confirmation naming every affected file. Open reports every field the editor cannot represent, by source file + field path, before any write; Save repeats that loss report. Never silently clobber. An unchanged Save still validates, then performs no write, preserving every byte (V32). `build/` is outside Save; Compile owns it.
- C3: Save runs the real pipeline before touching the destination: Create uses `scaffold.Preflight`; Maintain uses `spec.Load` → `ir.Build` → `ir.Validate` on the rendered candidate. Failure opens the existing dedicated repair screens; no partial package write.
- C4: UI stack — keep `huh` v1.0.0. Amend `CLAUDE.md` with exactly one direct Bubble Tea exception: one persistent `tea.Program`, with alt-screen, owns the whole console session and hosts the huh forms. No other direct Bubble Tea import; no direct Lip Gloss import. The shell replaces the per-form `tea.WithAltScreen()` stopgap (V27).
- C5: accessible mode (`input != os.Stdin`) is the L2 seam. Every screen, including Home, Open, Save, Validate, and Compile, is drivable by numbered input with zero TTY and zero Python. L1–L3 stay inside `go test ./...`.
- C6: all provider facts — brands, distributors, routes, model/voice arity, and language slot — come only from `target.DefaultCatalog()`, never TUI literals. Full doc-verified STT/TTS/LLM breadth from PRs #9/#10 is catalogue work in `internal/target/catalog_*.go`; this spec does not restate entries. A new catalogue entry needs zero TUI code (V31).
- C7: model and voice identities are free text forwarded without an allowlist (SCHEMA.md D10). Provider params remain forwarded JSON.
- C8: webhook literal-URL plumbing (`url` in spec/IR/generate) belongs to [compiler.md](compiler.md) + the five driver specs. This console owns only the single-field URL-or-env interaction and reads its literal-URL gate from `target.FieldToolWebhookURL`; it does not redefine the compiler field or driver lowering (V28).
- C9: discovery is local and bounded: Open lists immediate child directories of the working directory that contain `agent.yaml`, sorted deterministically, plus **Enter a path manually**. No recursive scan or package index.

## §I surfaces
- I.root: `internal/cli.newRootCmd()` — a no-arg `RunE` dispatches to the console only under C1; otherwise it prints root help. Existing subcommands and exit codes do not change.
- I.init: `unmute init [name]` — named use stays noninteractive; interactive no-name use enters the console's Create flow directly, bypassing Home.
- I.console: `internal/tui` — one persistent program owns Home, Create, Open, section editors, repair screens, notices, confirmation, and Back navigation. The SLNG wordmark is Home content, never pre-program terminal output.
- I.open: immediate-child package discovery + manual path; selected paths load through `spec.Load`. The internal package→`scaffold.Data` adapter also returns the exact unrepresentable field/file loss report used by C2; no new public package or one-implementation interface.
- I.save: Create returns confirmed `scaffold.Data` for the existing `scaffold.Write` path. Maintain renders a candidate from the same scaffold templates, validates it, diffs it against package files, and atomically replaces only the confirmed file set; unchanged state is a no-op.
- I.actions: `internal/cli` supplies one console action handler that calls the same in-process paths as `runValidate` and `runCompile`; `internal/tui` streams that handler's stdout + stderr into a scrollable notice, avoiding a `tui` → `cli` import cycle. Compile may update `build/<target>/` exactly as the Cobra command does; either action returns to the maintain menu.
- I.catalog: `target.DefaultCatalog()` is the sole source for provider and distributor options, labels, arity hints, and language-slot hints (C6).

## §V invariants
- V18: saved variables, tools, and agents remain visible and editable when their sections reopen (`TestV18VariablesMenuShowsSavedItems`, `TestV18ToolsMenuUsesNeutralNameAndShowsExecution`, `TestV18AgentMenuShowsAndEditsSavedAgent`).
- V19: reference editors select compatible saved state instead of asking users to retype names/assignment JSON: handoffs expose agents + variables, tasks expose tools, and task-result fields map to saved variables (`TestV19HandoffShowsExistingVariablesAsChoices`, `TestV19TaskShowsExistingToolsAsChoices`, `TestV19TaskAssignmentPicksSavedVariableAndResultField`).
- V20: Back preserves all prior in-memory edits in Create and Maintain (`TestRunBackPreservesPriorEdits`; retrofit name `TestV20BackPreservesPriorEdits`).
- V21: a preflight failure opens a dedicated repair screen containing the exact compiler failure and relevant editors, then returns through Back; it is never printed above the main editor (`TestV21PreflightFailureUsesDedicatedScreen`).
- V22: a new task result is prefilled as `{"result":"string"}` and the screen explains that each key is one returned field (`TestV22TaskResultExplainsPrefilledShape`).
- V23: the SLNG wordmark carries foreground color only, never a background color (`TestV23WordmarkHasNoBackgroundColor`).
- V24: provider labels are brand-deduplicated; selection is provider brand first, then distributor when more than one route exists (`TestV24ModelsLabelDeduplicatesProviderBrands`, `TestV24ProviderThenDistributorFlow`, `TestV24ProviderBrandsAreUniqueAndExposeDistributors`).
- V25: every saved resource has an explicit Delete action; deletion removes or safely resets dependent references; invalid saved resources remain reachable for edit/delete even when their Add gate is closed (`TestV25SavedResourcesOfferDelete`, `TestV25DeleteResourceCleansReferences`, `TestV25InvalidSavedResourcesRemainAvailableForRepair`).
- V26: the SLNG wordmark renders inside TUI output as the Home title; `NO_COLOR` omits it. It is never printed before `tea.Program` starts, so alt-screen cannot hide it on the normal buffer (B6; `TestV26WordmarkRendersInsideHome`, `TestV26NoColorOmitsWordmark`).
- V27: selecting any menu item redraws in place and never stacks a new frame into scrollback. One persistent alt-screen program spans the entire session; no between-step flash from per-form programs (`TestV27StepsRedrawInPersistentAltScreen`).
- V28: a webhook tool has one endpoint input accepting either a pasted `http://`/`https://` URL or an env-var name, auto-detected; exactly one of `url` / `url_env` is set. Literal URL is selectable only when `target.FieldToolWebhookURL` permits it (Pipecat + LiveKit); every other target stays env-only and shows the gate's own message. Compiler/driver plumbing is C8 (`TestV28WebhookEndpointDetectsURLOrEnv`, `TestV28WebhookLiteralURLUsesTargetGate`).
- V29: no unavailable choice dead-ends: the notice names the exact gate in the selected target's vocabulary and offers Back (`TestV29UnavailableChoiceNamesGateAndOffersBack`).
- V30: bare `unmute` launches Home only when stdin + stdout are TTYs; non-TTY bare use prints help and keeps current exit codes; interactive `unmute init` enters Create directly (`TestV30BareRootTTYLaunchesConsole`, `TestV30BareRootNonTTYPrintsHelp`, `TestV30InitEntersCreate`).
- V31: for every `(framework, role)`, console options mirror `target.DefaultCatalog()` exactly: every non-wildcard entry or distributed brand is selectable and nothing else appears; per-entry arity/language hints come from the same entry. The coverage test iterates the catalogue, so adding an entry changes the TUI with zero TUI edits (extend `TestProviderOptionsMirrorCatalog`; add `TestV31ProviderOptionsMirrorCatalog`).
- V32: Open → edit nothing → Save validates and leaves package output byte-identical. Any changed Save that would rewrite bytes lists every affected file and every unrepresentable field, then requires explicit confirmation (`TestV32MaintainNoOpSaveIsByteIdentical`, `TestV32DestructiveSaveNamesFilesAndConfirms`).
- V33: Maintain Validate and Compile run in-process through the same code paths as their Cobra commands, stream stdout + stderr into a scrollable notice, and return to the console (`TestV33ValidateStaysInConsole`, `TestV33CompileStaysInConsole`).

## §T tasks
id|status|desc|cites
T1|x|one persistent `tea.Program` shell + alt-screen; Home; wordmark as Home content; bare-root TTY dispatch + direct-Create init path; add the single Bubble Tea exception to `CLAUDE.md`|C1,C4,I.root,I.init,I.console,V26,V27,V30
T2|x|immediate-child package discovery + manual path; `spec.Load` adapter into editor state + loss report; Maintain menu; validated no-op Save or candidate render/diff/explicit rewrite confirmation|C2,C3,C9,I.open,I.save,V32
T3|x|Maintain Validate / Compile actions reuse the in-process Cobra paths; combined stdout+stderr scrollable notice; return to menu|I.actions,V33
T4|.|catalogue-driven binding editor: all `(framework, role)` entries/brands, provider grouping, distributor route, model/voice arity + language hints; extend the coverage test to iterate `DefaultCatalog()`|C6,C7,I.catalog,V24,V31
T5|.|single webhook endpoint input auto-detects literal http(s) URL vs env name; gate literal URLs through `FieldToolWebhookURL`; land only after compiler.md + driver-spec `url` plumbing|C8,V28
T6|.|regroup the ~17-item agent menu into identity / models / behavior / integrations / lifecycle; every gated notice uses target vocabulary + Back|G,V29
T7|.|retrofit the existing V18–V25 tests to cite this spec and rename the unnumbered Back test to `TestV20BackPreservesPriorEdits`; keep all create-flow repair behavior green under the persistent shell|V18,V19,V20,V21,V22,V23,V24,V25

Dependency order: T1 → T2 → T3; T4 after T1; T5 after its compiler/driver cross-spec dependency; T6 after T2; T7 last.

## §B bugs
id|date|cause|fix
B1|2026-07-16|the init wizard's append-only section menus hid saved variables/tools/agents on re-entry and offered no edit path, so in-memory state looked lost (commit 721dfba)|V18,V20
B2|2026-07-16|reference-bearing flows required retyping names/assignment JSON, task result shape was opaque, and a failed preflight had no focused in-flow repair screen (commit 721dfba)|V19,V21,V22
B3|2026-07-16|the SLNG wordmark ANSI sequence painted a background color, leaking a terminal-sized color block (commit c77787e)|V23
B4|2026-07-16|catalogue vendor choices conflated provider brands with distributor routes, producing duplicate labels and no provider-then-distributor flow (commit c77787e)|V24
B5|2026-07-16|saved resources had no delete/cascade cleanup, while Add gates hid already-invalid saved resources and trapped repair (commit c77787e)|V25
B6|2026-07-16|the SLNG wordmark was printed before the TUI program; once alt-screen landed it remained on the hidden normal buffer instead of the console|V26

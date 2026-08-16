# Implementation Plan: Work Inside The Agent Folder, LiveKit By Default

**Branch**: `feat/unmute-cli-workdir-livekit-53d1c2` | **Date**: 2026-08-16 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/015-cwd-livekit-defaults/spec.md`

## Summary

Two small behaviour changes with a wide documentation blast radius.

1. **Work in the folder.** `validate`, `compile`, and `dev` take
   `cobra.ExactArgs(1)` today, so `unmute validate` from inside a package dies
   on `accepts 1 arg(s), received 0` before any Unmute code runs. Relax all
   three to `MaximumNArgs(1)` and resolve the directory in one shared helper
   that defaults to the current directory and fails with real guidance when the
   current directory is not a package.

2. **LiveKit by default.** `scaffold.DefaultTarget` is `"pipecat"`. Flip it to
   `"livekit"`. For the plain `unmute init <name>` path this really is a
   one-constant change, and it was measured rather than assumed: a patched
   binary in an isolated export scaffolds a package that validates
   `✓ livekit (livekit)` and compiles a full LiveKit project untouched
   (research.md D6).

   Two things the constant does **not** carry:
   - **The console's target menu.** It preselects the first option positionally
     and never reads `data.Target`, so the create flow would highlight Pipecat
     while building a LiveKit package. The maintain flow needs the *opposite*
     fix — preselect the package's own target — and ordering it by the default
     would itself be a regression (research.md D12, FR-011).
   - **A test that selects menus by ordinal.** `TestRunSelectTarget` sends `2`
     for LiveKit, so reordering the create menu breaks it
     (`tui_test.go:528-542`).

   And one thing that looked like a third and is not. The wizard's telephony
   branch sets a transport for Pipecat only (`scaffold.go:383`, `tui.go:2170`).
   Two successive drafts of this plan predicted what an author would see there
   and **both were wrong**; the third pass ran it. The path stays gated on both
   targets, LiveKit's message is already specific, and no code change is owed —
   only `TestRunTelephonyCreateGatedOnConnection`'s target-coupled message
   assertion moves (research.md D11, measured).

The code diff stays small. The real work is the repository's five-places rule:
the scaffold templates, the emitted runbooks, `docs/`, `docs-site/`, and the
skill bundle all teach these commands, and one of those surfaces is currently
ungated and would rot silently.

## Technical Context

**Language/Version**: Go 1.26 (the `go` directive in `go.mod`; note the
constitution's "Go 1.24" line is stale, see research.md "Out of scope")

**Primary Dependencies**: `spf13/cobra` for the command tree; `goccy/go-yaml`;
`google/jsonschema-go`; the Charm stack (`bubbletea`, `bubbles`, `lipgloss`,
with `huh` confined to the accessible renderer). No new dependency.

**Storage**: N/A. The resolved package directory lives for one command
invocation and is never persisted.

**Testing**: `go test -race ./...` via `make test`, layers L1–L3, zero Python.
L4 smoke (`make smoke`, `uv`) stays opt-in and out of the pull request gate.
New tests use `t.Chdir`, already used at 15+ sites in `internal/tui`.

**Target Platform**: macOS, Linux, and Windows CLI; `CGO_ENABLED=0` static
binaries across the six release platforms.

**Project Type**: Single Go CLI (a compiler, per the constitution).

**Performance Goals**: N/A. No hot path is touched; the added work is one
`os.Stat` per invocation on the zero-argument path.

**Constraints**: No change to the authoring surface, so no `docs/SCHEMA.md`
amendment and no constitution amendment (research.md D10). Exit codes stay 0
and 1. Warnings stay on stderr at exit 0.

**Scale/Scope**: 3 command files, 1 constant, 2 new helpers in one file, 2 scaffold
templates, 2 regenerated golden files, 2 new gates, and a documentation sweep
across `docs/`, `docs-site/`, `examples/`, and `internal/skill/assets/`.

## Constitution Check

*GATE: checked before Phase 0 and re-checked after Phase 1 design.*

| Principle | Verdict | Reasoning |
|---|---|---|
| I. Compile ahead of time | **PASS** | Nothing moves to runtime. No maintained Python. The four-stage flow `spec.Load → ir.Build → ir.Validate → generate.Generate` is untouched; only the directory handed to `spec.Load` changes. |
| II. Fail loud, never average | **PASS**, and it is the point | FR-002 replaces a useless cobra arity error with a message naming the file, the directory, and both usage forms. The fresh-scaffold warning `LiveKit turn placement is a preference` is deliberately **not** hidden to make first-run look tidier (research.md D7) — that would be exactly the silent downgrade this principle forbids. |
| III. One source of truth, derived not copied | **PASS**, with two required constraints | (a) The cwd rule must live in one helper called by three commands, never copied into three `RunE` bodies. (b) The scaffold default is **not** as single-homed as it looks. `DefaultTarget` has three production readers (`scaffold.go:80`, `scaffold.go:354`, `tui.go:120`), but the console's target menu preselects `options[0]` positionally and never consults `data.Target` (research.md D12). Ordering the menu by the constant creates a second statement of the same fact, so an agreement test is mandatory here, not optional. |
| IV. The document wins | **PASS** | No authoring-surface change, so no `SCHEMA.md` amendment is owed (research.md D10). The five-places rule does apply and is planned as first-class work below, not as a follow-up. |
| V. Whatever compiles can be spoken to | **PASS**, and it is served | This shortens the path from nothing to a voice, which is what the principle protects. `unmute init` keeps its contract (still takes a name, still refuses a non-empty directory). `unmute dev` on LiveKit needs no LiveKit Cloud credentials locally (research.md D8), so the new default does not add a step before first sound. |
| Gate: a rule with no gate is a wish | **ACTION REQUIRED** | `TestDocsSiteCLIPagesQuoteHelp` filters to lines starting with `-`, so it checks flags and never the `Usage:` line. The three `docs-site/reference/cli/*.mdx` pages quote a usage string nothing verifies. This change would rot them silently, so extending that test is part of the work, not optional. |

**Post-Phase-1 re-check**: unchanged. The design adds two small unexported
functions in one file, no interface, no dependency, no config knob. Complexity
Tracking is empty.

## Project Structure

### Documentation (this feature)

```text
specs/015-cwd-livekit-defaults/
├── spec.md              # /speckit-specify output
├── plan.md              # This file
├── research.md          # Phase 0: D1-D14, three measured findings
├── data-model.md        # Phase 1
├── quickstart.md        # Phase 1
├── contracts/
│   └── cli-surface.md   # Phase 1: the command argument contract, C1-C10
├── checklists/
│   └── requirements.md  # spec quality checklist
└── tasks.md             # /speckit-tasks output, NOT created here
```

### Source Code (repository root)

```text
internal/
├── cli/                       # one file per command
│   ├── validate.go            # Args + Use + resolve   (:17-21)
│   ├── compile.go             # Args + Use + resolve   (:24-28)
│   ├── dev.go                 # Args + Use + resolve   (:35-39)
│   ├── package_dir.go         # NEW: the single cwd-resolution helper
│   ├── package_dir_test.go    # NEW: L1/L2 over C1-C10
│   └── help_capture_test.go   # extend :76 to gate the Usage line
├── scaffold/
│   ├── scaffold.go            # DefaultTarget :80 -> "livekit" (:383 unchanged)
│   ├── templates/
│   │   ├── agent.yaml.tmpl    # :1-2 drop the `.` argument
│   │   └── env.example.tmpl   # :2-3 drop the `.` argument
│   └── testdata/golden/init.txt        # regenerate (LiveKit + dropped dots)
├── tui/
│   ├── tui.go                 # create-menu order :231-233 (:2170 unchanged)
│   ├── maintain.go            # preselect data.Target :554-563
│   └── tui_test.go            # :528-542 ordinal; :1019-1043 message half
└── skill/assets/              # SKILL.md + references/, the <dir> restatements

docs/                          # TESTING.md carries the command grammar
docs-site/reference/cli/       # validate.mdx compile.mdx dev.mdx Usage lines
examples/*/README.md           # runbooks using the explicit-path form
```

**Structure Decision**: unchanged single-project layout. `internal/`, not
`pkg/`; one file per command in `internal/cli/`; the new helper is one small
file in the same package, which keeps the rule beside the three commands that
share it and keeps `internal/target` a leaf.

## Design

### The helper

```go
// packageDir resolves the package directory from an optional positional arg.
// No argument means the current directory, so an author can cd into a package
// and work there. The current directory must be the package: no parent search,
// or `unmute compile` from build/<target>/ would rewrite what you stand in.
// cmd supplies the command name for the failure message.
func packageDir(cmd *cobra.Command, args []string) (string, error)
```

The `cmd` parameter is load-bearing, not decoration. The C4 message names the
command twice, so a helper holding only `args` could not produce it. An earlier
draft of this plan omitted it (research.md D2, corrected).

Explicit argument → returned verbatim, no extra checks, so today's error text
survives byte-for-byte (contract C9). No argument → `.`, guarded by an
`os.Stat` of `agent.yaml` that produces the guidance message (contract C4),
with the directory reported as an absolute path via `filepath.Abs` — an `Abs`
failure is wrapped with `%w`, never silently downgraded to a relative path.

The same file carries a second small function: the D5 display helper that keeps
the header from reading `validate .`. It is pure, so it gets a direct L1 table
test rather than riding on `printHeader`, which is TTY-gated and therefore
invisible to every captured-output test.

Each command becomes `Args: cobra.MaximumNArgs(1)` with `Use` switching from
`<package-dir>` to `[package-dir]`, matching `init [name]`'s existing
convention, **and** gains a `Long:` field stating that omitting the argument
uses the current directory. Brackets alone say the argument is optional; they
never say what the default is, which spec.md's help-text edge case requires.

### Where the explicit form must survive untouched

The emitted `build/<target>/README.md` keeps `<source-dir>`. Its reader is
standing in `build/<target>/`, not in the package, so the cwd default does not
apply and dropping the argument would make the instructions wrong. This also
keeps the generate goldens and the hard-coded assertion at
`internal/generate/pipecat_carrier_telephony_test.go:228` still, which has no
`-update` path and would otherwise be a hand edit.

The scaffold templates are the opposite case: they live **inside** the package
and already teach the `.` workaround (`agent.yaml.tmpl:1-2`,
`env.example.tmpl:2-3`). Those dots come out.

## Work Breakdown

Detailed task ordering belongs to `/speckit-tasks`; this is the shape.

**A. The cwd default** — new helper plus three call sites; `Use` and `Args`
edits; new tests covering C1–C10; regenerate the help golden.

**B. The LiveKit default** — flip `DefaultTarget`; order the console **create**
menu by the constant (`tui.go:231-233`) and add the agreement test that
Principle III requires for that second statement of the fact; fix the
**maintain** menu (`maintain.go:554-563`) to preselect `data.Target` instead,
which is a pre-existing wart the flip would otherwise make visible. Keep the
telephony gate as it is and improve only its LiveKit guidance (FR-010).
Regenerate `internal/scaffold/testdata/golden/init.txt` only. Hand-edit the
tests that pin strings: `internal/cli/init_test.go:61` (`✓ pipecat (pipecat)`)
and `internal/tui/tui_test.go:530-532`, whose positional wizard input selects
the target menu by ordinal and breaks when the menu is reordered. No
`SetTarget` or template work: those LiveKit branches already exist and were
proven to work.

**C. Close the gate gap** — extend `TestDocsSiteCLIPagesQuoteHelp` to assert
the `Usage:` line, then fix the three `docs-site/reference/cli/*.mdx` pages the
newly-strict test flags.

**D. The five-places sweep** — scaffold templates (drop the dots); `docs/`
(notably the command grammar restated in `TESTING.md:463-465`); `docs-site/`
(reference pages plus the tutorial pages that show invocations);
`examples/*/README.md`; and the skill bundle (`SKILL.md:46,51` and
`references/workflow.md:10-12` restate `unmute validate <dir>`). Note the skill
bundle's arity claims are effectively ungated — `TestSkillBundleNamesRealCommands`
checks that commands and flags exist, never their arity — so this surface needs
reading, not just a test run.

**FR-009 lives here too**, and it is the sharpest edit in the sweep: ten
documented invocations pin `--target pipecat` (or the already-stale
`pipecat-dev`) against a *scaffolded* package, which after the flip declares
only a `livekit` instance. Six are in the skill bundle, so a coding agent
following it hits `target instance "pipecat" is not declared` on step 4. The
fix is to delete the flag, not retarget it: a single-target package needs none
(`dev.go:422`), and an omitted flag cannot go stale again.

The LiveKit default adds its own documentation edits, all currently ungated:
`docs-site/start/quickstart.mdx` (the file list, the `✓ pipecat (pipecat)`
line, `build/pipecat`, and the "only two keys" sentence that D14 makes false),
`docs-site/reference/cli/init.mdx:20-30`, and `README.md:152`. If the skill
bundle or docs assert a default target anywhere, they move too.

**E. Verify and ship** — `make fmt && make lint && make build && make test`,
the manual walkthrough in quickstart.md, then the pull request to `main`.

## Risks

| Risk | Handling |
|---|---|
| **Wizard + phone channel on the new default** | Measured, after two wrong predictions: the path stays gated on both targets and LiveKit's message is already specific (`connection "phone" declares no transport`). No code change. The one consequence is that `TestRunTelephonyCreateGatedOnConnection` fails on its target-coupled message assertion while its blocking assertion still passes; update the message half only (research.md D11, measured). |
| **Reordering a menu breaks positional wizard tests** | The accessible renderer takes numeric input, so tests select menu entries by ordinal. `TestRunSelectTarget` (`tui_test.go:530-532`) sends `2` for LiveKit and asserts on it. Any `huh.Option` reorder invalidates every numeric path through that menu — grep the test inputs before reordering. |
| **The console highlights Pipecat while building LiveKit** | Create menu ordered by `scaffold.DefaultTarget` with an agreement test on the first option (research.md D12). The maintain menu must **not** follow the constant; it preselects the package's own target (FR-011). |
| A user hand-switching a target edits only `targets.yaml` and hits `turn model "silero" is not recognized` | Pre-existing correct behaviour, not introduced here. Called out in data-model.md and quickstart.md so docs and fixtures move both files together. |
| An empirical probe proves less than it appears to | D6's probe covered only the browser-only default package, which is exactly why it missed D11. Findings now state what they did **not** cover. |
| Documentation rot on ungated surfaces (`docs/`, `examples/`, skill arity) | Work item C converts the highest-value one (the `Usage:` lines) from ungated to gated. The rest is a read-through; the constitution's own note that prose can still rot applies. |
| A chdir test breaks a fixture path | Existing `internal/cli` tests reach fixtures by relative path (`validate_test.go:12`). New tests resolve to absolute before chdir, or copy into `t.TempDir()` as several tests already do (research.md D9). |
| Every new package now shows a warning on first validate | Deliberate. Hiding a true warning is forbidden by Principle II (research.md D7). |

## Complexity Tracking

No constitution violations. No new dependency, no new abstraction, no interface
with one implementation, no config knob. The change adds two small unexported
function and flips one constant.

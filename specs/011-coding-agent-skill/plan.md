# Implementation Plan: Coding-agent skill for building Unmute voice agents

**Branch**: `011-coding-agent-skill` | **Date**: 2026-08-15 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/011-coding-agent-skill/spec.md`

## Summary

Ship an Agent Skills bundle inside the CLI binary and a fifth command,
`unmute skill install`, that writes it into a project. The bundle teaches any
major coding assistant to author Unmute packages, covering the whole documented
surface and pointing at the documentation page that owns each fact.

The research turned one assumption over. Agent Skills is no longer a Claude
Code feature; it is an open standard read by all four assistants the spec
names. So there is one format, not four, and the work splits cleanly: a small
Go command that unpacks embedded files and reports honestly about what it
found, and a body of written content held true by five agreement tests.

## Technical Context

**Language/Version**: Go 1.24, pinned in `go.mod`.

**Primary Dependencies**: none new. `spf13/cobra` for the command, already a
direct dependency. Everything else is standard library: `embed`, `crypto/sha256`,
`encoding/json`, `os`, `path/filepath`, `io/fs`.

**Storage**: files on disk in the user's project. `embed.FS` in the binary for
the source bundle, matching `internal/scaffold`, `internal/web`, and both
generator drivers.

**Testing**: the existing four layers. L1 for hashing, manifest comparison, and
path resolution. L2 for the command through the real cobra tree with output
captured. L3 goldens for the installed file tree. Five agreement tests hold the
bundle's factual lists against the Go sources. No Python, so `make test` still
runs with none.

**Target Platform**: macOS, Linux, Windows. Path separators matter, because the
command writes nested directories; use `filepath.Join` throughout and never a
literal slash in a filesystem path.

**Project Type**: CLI.

**Performance Goals**: install completes in well under a second and makes no
network call. This is file copying, so the only real budget is that it must
never feel like it is doing something remote.

**Constraints**: offline by construction. No secret ever appears in the bundle.
No file outside the two skill directories is created or edited. Every outward
change is one the user can read, diff, and delete.

**Scale/Scope**: one command with three flags, one embedded bundle of roughly
thirteen files, two install destinations, five agreement tests, one constitution
amendment, one `CLAUDE.md` amendment.

## Constitution Check

*GATE: checked before Phase 0, re-checked after Phase 1 design.*

| Principle | Verdict | Why |
|---|---|---|
| I. Compile ahead of time, never interpret at runtime | Pass | The bundle is static text embedded at build time and written once at install. Nothing sits between a generated agent and its platform. No maintained Python enters the repository; the bundle's Python examples are illustrative text inside markdown, and any that is executable gets the `ty` and `ruff` treatment the constitution requires. |
| II. Fail loud, never average | Pass | An unknown `--agent` fails and lists what is supported. A locally modified file refuses to be overwritten and is named. A version mismatch is reported, never silently reconciled. No partial write is left behind on failure. |
| III. One source of truth per surface, derived not copied | Pass, with tests | The embedded bundle is the single source; both installed directories derive from it. The bundle restates facts owned by `internal/spec`, `internal/target`, and the catalogue, so five agreement tests hold each list. This is the principle most at risk here and it is where the test budget goes. |
| IV. The document wins | Pass | Every claim points at the documentation page that owns it (R4). The bundle states in its own text that the documentation wins on disagreement. Every assistant path in this plan was read from the vendor's own documentation on 2026-08-15 and is dated in `research.md`. |
| V. Whatever compiles can be spoken to | **Violation** | "The command surface is four commands." This adds a fifth. See Complexity Tracking. |
| Secrets | Pass | The bundle teaches environment variable names only and contains no value. The command reads no environment value. |
| Naming and types | Pass | The skill directory is `unmute`, lowercase, no leading underscore. |
| Command rules | Pass | `newRootCmd()` gains one child. `RunE`, no `os.Exit`, output through `cmd.OutOrStdout()` and `cmd.ErrOrStderr()`, errors wrapped with `%w`, exit 0 or 1. |
| Dependencies | Pass | No new dependency. Standard library does all of it. |
| Layout | Pass | `internal/skill/` for the bundle and its logic, `internal/cli/skill.go` for the command, one file per command as the rule requires. |
| Voice | Pass | Plain wording, no dashes as punctuation, in the bundle and in every message the command prints. |

**Post-Phase 1 re-check**: unchanged. The design added no dependency, no
abstraction with one implementation, and no configuration knob. The single
violation is the command count, and it is resolved by amendment rather than by
working around it.

## Project Structure

### Documentation (this feature)

```text
specs/011-coding-agent-skill/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
│   ├── cli-skill-command.md
│   └── skill-bundle.md
├── checklists/
│   └── requirements.md
├── spec.md
└── tasks.md             # /speckit-tasks output, not created here
```

### Source code (repository root)

```text
internal/skill/
├── skill.go                    # embed.FS, Bundle type, Install, plan/apply
├── manifest.go                 # version + per-file SHA-256, read, write, compare
├── skill_test.go               # L1: hashing, comparison, path resolution
├── agreement_test.go           # the five drift checks
└── assets/                     # the bundle, embedded
    ├── SKILL.md                # entry, under 500 lines, the decision layer
    ├── pointer/SKILL.md        # the Claude Code pointer body
    └── references/
        ├── package.md          # layout, agent.yaml, targets.yaml, instructions
        ├── models.md           # SLNG listen and speak, OpenAI think, vendors per role
        ├── orchestration.md    # the ladder and every context decision
        ├── tools.md            # webhook, prebuilt, mcp, python
        ├── prompting.md        # voice prompting, per prompt surface
        ├── variables.md        # variables, sources, secrets
        ├── conversation.md     # greeting, interruption, inactivity, turn detection
        ├── telephony.md        # routes, carriers, directions, the boundary
        ├── transfers.md        # cold and warm, per route
        ├── deploy.md           # what the operator owns
        ├── workflow.md         # the build loop and what to state out loud
        └── examples.md         # need to shipped example, with pointers

internal/cli/
├── skill.go                    # newSkillCmd, install subcommand
├── skill_test.go               # L2: in-process command tests
└── root.go                     # one line: AddCommand(newSkillCmd())

internal/generate/testdata/golden/
└── skill_install.txt           # L3: the installed tree, pinned
```

**Structure Decision**: `internal/skill` is a new leaf package holding the
embedded bundle and the install logic. It imports `internal/spec`,
`internal/target`, and `internal/ir` in tests only, for the agreement checks, so
it stays free of the compiler pipeline at runtime. `internal/cli/skill.go` is
the cobra surface and holds no logic beyond flag parsing and printing, matching
how `init`, `validate`, and `compile` are already split.

## What gets built, in order

**Stage 1, the command with a stub bundle.** `unmute skill install` exists,
writes both destinations from `embed.FS`, writes the manifest, refuses to
overwrite a modified file without `--force`, reports a version mismatch, and
fails by name on an unknown `--agent`. The bundle at this point is `SKILL.md`
plus one reference, enough to prove the machinery. L1, L2, and the L3 golden
land here.

**Stage 2, the bundle content.** The twelve reference files, written against
the documentation and the code, each carrying its pointers. This is the bulk of
the work and it is writing, not engineering.

**Stage 3, the five agreement tests.** Written last on purpose: they assert
against content that has to exist first. Each one fails loudly with the offending
file named.

**Stage 4, the amendments.** The constitution's Principle V, and `CLAUDE.md`'s
"three places" rule. Both are small and both are required for the feature to be
honest about what it changed.

## Existing tests this feature must feed

A new command is not a private addition here. Three existing agreement tests
already hold the command surface against the documentation, and all three go red
the moment `newRootCmd` gains a child. This is the repository working as
designed, and the work is real, so it is listed rather than discovered during
implementation.

| What breaks | Why | What has to land with it |
|---|---|---|
| `TestHelpCaptureMatchesBinary` in `internal/cli/help_capture_test.go` | root help gains a command, so the capture no longer matches | add `{"skill"}` and `{"skill", "install"}` to `helpCommands`, then re-capture with `go test ./internal/cli -run TestHelpCaptureMatchesBinary -update` and read the diff |
| `TestDocsSiteCLIPagesQuoteHelp` in the same file | every flag in the capture must appear on the page documenting that command, and `skill` has no page | write `docs-site/reference/cli/skill.mdx` quoting `--agent`, `--dir`, and `--force`, and add `skill` to the test's `pages` map |
| the documentation site's page-count invariant | a new `.mdx` needs a navigation entry | add `reference/cli/skill` to the CLI group in `docs-site/docs.json` |

This is also the point where `docs/` and the emitted README rule apply. The
command is new user-facing behaviour, so `docs-site/reference/cli/overview.mdx`
names it alongside the other commands, and `docs/REPO_MAP.md` gains the new
package in the pipeline table.

## Complexity Tracking

| Violation | Why needed | Simpler alternative rejected because |
|---|---|---|
| A fifth command, against Principle V's "the command surface is four commands" | The skill has to reach a user's project somehow, and the CLI is the only thing the user already has. | Three alternatives were weighed. Documenting a copy-paste of files from the repository puts the burden on the reader and breaks the offline, version-matched guarantee in FR-002. Publishing the bundle to a package registry adds a release surface, a network dependency at install, and a second place the version can drift. Folding it into `unmute init` welds a one-time setup step onto a command that scaffolds packages, and gives no way to update the skill later without scaffolding again. The amendment states that `skill` is not part of the path from nothing to a spoken agent and touches no package, which is what Principle V's sentence is actually protecting. |

## Deliberate simplifications

Recorded here so they read as intent, per the constitution's rule on marking
shortcuts.

- **No assistant detection.** FR-004 asks the install to detect which assistants
  a project uses. With two destinations, both small, writing both is always
  correct and detection is a heuristic that is wrong for anyone who has not yet
  created their assistant's directory. `--agent` covers the person who wants
  only one. Full reasoning in research R8.
- **No `AGENTS.md` write.** All four assistants in FR-003 read skills, so the
  file adds nothing for them, and editing a file the user owns needs a reason
  there is not yet. Research R9.
- **One pointer file, not a second copy of the bundle.** Research R2.
- **The prompting reference is exempt from the pointer test**, because no
  documentation page owns that content yet. The exemption is named in the test
  rather than passed over. Research R10.

## Risks

- **The vendor paths move.** Four assistants, four independent release
  schedules, and `.agents/skills/` is new enough that adoption is still
  spreading. The mitigation is that every path is recorded with its
  verification date in `research.md`, so a stale one is visible rather than
  mysterious, and the install writes locations rather than reading them, so a
  wrong path fails visibly for the user on the first prompt.
- **Full coverage is a lot of prose.** Twelve reference files can rot in ways a
  list test cannot see. The mitigation is that the tests hold the lists, the
  pointers keep the reader one hop from the authority, and FR-034 puts the skill
  in the same commit as any behaviour change.
- **`SKILL.md` grows past its budget.** Under 500 lines is the documented
  guidance and it is the whole point of the layering. The mitigation is that the
  entry file is a decision layer, not a summary: it routes to a reference and
  states what the assistant must say out loud.

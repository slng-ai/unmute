# Phase 1 data model: coding-agent skill

Entities are the spec's Key Entities made concrete. Nothing here is persisted by
Unmute at run time; everything is either embedded in the binary or written once
into the user's project.

## Bundle

The embedded source of truth. One per binary.

| Field | Type | Notes |
|---|---|---|
| `FS` | `embed.FS` | `//go:embed assets`, the only copy in the process |
| `Version` | string | the CLI version, stamped at link time, never hardcoded |

Rules:

- The bundle is read-only. Nothing at run time writes into it.
- Every file under `assets/references/` must be reachable from `assets/SKILL.md`.
  A reference no document points at is dead weight and fails the pointer test.
- `assets/SKILL.md` must be under 500 lines. Held by a test, because it is the
  budget that makes progressive disclosure work.

## Destination

Where a bundle lands. Two of them, fixed.

| Name | Path | Contents | Serves |
|---|---|---|---|
| canonical | `.agents/skills/unmute/` | `SKILL.md`, `references/*.md`, `.unmute-manifest.json` | Codex, Cursor, GitHub Copilot |
| pointer | `.claude/skills/unmute/` | `SKILL.md`, `.unmute-manifest.json` | Claude Code |

Rules:

- Paths are built with `filepath.Join`, never a literal separator, because
  Windows is a supported platform.
- The pointer's `SKILL.md` carries the same `name` and `description` frontmatter
  as the canonical one, because that string is what decides activation, and a
  body that says where the real bundle is and to read it first.
- A destination is written whole or not at all. A failure part way through
  removes what it wrote, so a broken install never looks like a good one.

## Assistant

The user-facing name for a destination, used by `--agent`.

| Name | Writes | Why |
|---|---|---|
| `claude` | pointer | Claude Code reads only `.claude/skills/` |
| `codex` | canonical | reads `.agents/skills/` |
| `cursor` | canonical | reads `.agents/skills/` and `.cursor/skills/` |
| `copilot` | canonical | reads `.agents/skills/`, `.github/skills`, `.claude/skills` |
| `all` | both | the default |

Rules:

- Several names can map to one destination. Naming two that share a destination
  writes it once, not twice.
- An unknown name is an error that lists every supported name. It is never a
  warning, and it never falls back to `all`.
- Verified against each vendor's documentation on 2026-08-15. The table in
  `research.md` R2 is the source; this one is its user-facing projection.

## Manifest

Written into each destination as `.unmute-manifest.json`. This is what makes a
second install honest.

| Field | Type | Notes |
|---|---|---|
| `version` | string | the CLI version that wrote this destination |
| `files` | map of relative path to string | SHA-256, lowercase hex, of each file as written |

Rules:

- Relative paths use forward slashes in the file, so a manifest written on
  Windows and read on macOS still matches. Conversion happens at the filesystem
  boundary.
- The manifest lists every file the install wrote, and never itself.
- A destination with no manifest is treated as not installed by Unmute. It is
  never overwritten without `--force`, because it is someone else's directory.

## Install decision

Not stored. Computed per file on every run, and it is the whole behaviour of the
command.

| On disk | Manifest | Embedded | Decision |
|---|---|---|---|
| absent | absent | present | write, report as new |
| present, hash matches manifest | version equals CLI | same bytes | leave, report as current |
| present, hash matches manifest | version equals CLI | different bytes | write, report as updated |
| present, hash matches manifest | version differs | any | write, report as upgraded from that version |
| present, hash differs from manifest | any | any | refuse and name the file, unless `--force` |
| present | absent | present | refuse and name the file, unless `--force` |
| absent | present | present | write, report as restored |

Rules:

- The whole decision set is computed before any file is written, so a refusal
  names every offending file at once rather than stopping at the first.
- `--force` overwrites and rewrites the manifest. It never merges.
- Every outcome is printed. A file that was left alone is reported as left
  alone, because a silent no-op and a silent overwrite look identical to a user.

## Reference

One file under `assets/references/`. The unit of progressive disclosure.

| Field | Notes |
|---|---|
| name | the file name, which is also how `SKILL.md` refers to it |
| subject | the one area it covers |
| pointers | at least one documentation page path, except `prompting.md` |

Rules:

- A pointer is a site path such as `build/tools/overview`, not a URL and not a
  repository path. Reasoning in `research.md` R4.
- Every pointer must resolve to a real page under `docs-site/`. Held by a test.
- `prompting.md` carries no pointer because no page owns that content yet. The
  test names this exemption rather than skipping quietly.

## Held facts

The lists inside the bundle that a test checks against the code. These are not
data structures; they are the reason the drift tests exist, listed here so the
tasks phase has them in one place.

| Held fact | Source of truth |
|---|---|
| tool execution kinds | the `Tool` struct in `internal/spec` |
| model vendors per role per target | `internal/target/catalog_*.go` |
| target providers, and which generate versus only validate | `ir.Provider` and `internal/target` |
| commands and flags | the cobra tree in `internal/cli` |
| documentation pointers | the pages under `docs-site/` |

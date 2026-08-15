# Phase 0 research: coding-agent skill

**Date**: 2026-08-15. Every outside claim below was read from the vendor's own
documentation on that date, per constitution Principle IV.

## R1. Agent Skills is an open standard, not a Claude Code feature

**Decision**: ship the bundle as an Agent Skills skill: a folder holding
`SKILL.md` with YAML frontmatter, plus a `references/` directory of files
loaded on demand.

**Rationale**: the format was released by Anthropic as an open standard at
[agentskills.io](https://agentskills.io) and is now read by roughly fifty
agents, including all four this feature must support. One format covers all of
them. Its progressive-disclosure model is exactly what FR-031 asks for:
discovery loads only `name` and `description`, activation loads `SKILL.md`,
and referenced files load only when a task needs them.

**Alternatives rejected**:

- Per-assistant formats (`AGENTS.md` for Codex, `.github/copilot-instructions.md`
  for Copilot, `.cursor/rules/*.mdc` for Cursor). This was the first draft's
  assumption and it is now wrong. It would mean four bodies of text to keep in
  step for no gain, and it throws away progressive disclosure everywhere except
  Claude Code.
- `AGENTS.md` alone. It is read by more tools, but it is a single flat file
  loaded in full every session. Full coverage (spec option B) in one always-on
  file is exactly the shape FR-031 forbids.

## R2. Two install locations cover all four assistants

**Decision**: install to `.agents/skills/unmute/` as the canonical bundle, and
write a short pointer skill at `.claude/skills/unmute/SKILL.md`.

**Rationale**: the discovery paths do not fully overlap. Read from each
vendor's documentation on 2026-08-15:

| Assistant | Project skill paths it reads |
|---|---|
| Claude Code | `.claude/skills/<name>/SKILL.md` |
| Codex | `.agents/skills/` at repo root, cwd, and parents; also `$HOME/.agents/skills`, `/etc/codex/skills` |
| Cursor | `.agents/skills/`, `.cursor/skills/`; back-compat `.claude/skills/`, `.codex/skills/` |
| GitHub Copilot | `.github/skills`, `.claude/skills`, `.agents/skills` |

`.agents/skills/` is the vendor-neutral path and three of the four read it.
Claude Code reads only `.claude/skills/`, and its documentation does not
mention `.agents/skills/`. So one directory cannot serve all four, and two can.

The pointer keeps one body of text in the user's repository. Its frontmatter
carries the same `description`, because that string is what decides activation,
and its body says where the real bundle is and to read that first.

**Alternatives rejected**:

- Write the full bundle twice. No second source of truth, since both copies come
  from the same embedded files, but it doubles what a reader has to diff and a
  hand edit to one copy diverges silently.
- Symlink `.claude/skills/unmute` at the canonical bundle. Breaks on Windows and
  travels badly through git.
- `.claude/skills/` only, leaning on Cursor and Copilot back-compat. Building on
  another vendor's compatibility shim for a path we could name correctly is a
  bug waiting for a deprecation note.

## R3. Restrict frontmatter to the standard's fields

**Decision**: use only `name`, `description`, and `metadata`.

**Rationale**: Claude Code documents six fields as the portable set (`name`,
`description`, `license`, `compatibility`, `metadata`, `allowed-tools`) and
warns that anything outside it errors on some distribution paths. Cursor reads
`name`, `description`, `paths`, `disable-model-invocation`, and `metadata`.
The intersection that every one of the four accepts is `name`, `description`,
and `metadata`. Nothing this feature needs sits outside it.

**Alternatives rejected**: Claude Code extensions such as `allowed-tools`. They
would buy a small permission convenience on one assistant at the cost of a
frontmatter that is no longer portable.

## R4. Documentation pointers are page paths, not URLs

**Decision**: every claim points at a documentation page by its site path, for
example `build/tools/overview`, rendered as a readable name plus that path. The
skill states the site's base address once, in one place.

**Rationale**: this resolves the tension found while specifying feature 012.
The documentation site starts private, so a URL printed in an installed skill
would fail for the reader. A repository-relative path like
`docs-site/build/tools/overview.mdx` is meaningless once the skill is installed
in someone else's project. A site path is meaningful to a person, resolves to a
real file for the drift test, and becomes a working link the day the site goes
public without editing the bundle.

**Alternatives rejected**: full URLs (dead today), repository paths (meaningless
to a user), no pointers at all (fails FR-038).

## R5. Drift is caught by agreement tests, not by generating the skill

**Decision**: hand-write the bundle and hold its factual lists with agreement
tests against the Go sources, following the pattern this repository already
uses for the documentation site.

**Rationale**: `internal/target/providers_docsite_test.go` and
`internal/spec/tools_docsite_test.go` already do exactly this for `docs-site/`,
and they work. Generating prose from Go structs would produce text that reads
like a table dump, which is the opposite of what a skill is for. The facts that
actually rot are lists, and lists are what a test can hold.

Five checks, all in the default suite:

| Check | Holds |
|---|---|
| execution blocks | the tool kinds the skill names equal the `Tool` struct's blocks |
| vendors | the vendors the skill names per role per target equal the catalogue's |
| providers | the target providers the skill names equal `ir.Provider`, with the generate-versus-validate split stated |
| commands | the commands and flags the skill names exist in the cobra tree |
| pointers | every documentation pointer resolves to a real page, and every reference file carries at least one |

**Alternatives rejected**: generating reference files from the catalogue and
pinning them as goldens. More machinery, worse prose, and the golden would
still not catch a wrong sentence next to a right table.

## R6. The bundle ships in the binary through `embed.FS`

**Decision**: `//go:embed assets` in `internal/skill`, matching
`internal/scaffold`, `internal/web`, and both generator drivers.

**Rationale**: already the established pattern here, stdlib only, and it is what
makes FR-002 true: install makes no network call and the skill always matches
the CLI that wrote it. No new dependency.

## R7. Local edits are detected through a manifest inside the bundle

**Decision**: write `.unmute-manifest.json` inside each installed skill
directory, holding the CLI version and a SHA-256 per installed file.

**Rationale**: FR-006 requires naming the files that differ, and FR-007 requires
reporting a version mismatch. Hashing the directory as a whole answers neither.
Comparing installed bytes against the embedded bytes cannot tell a user edit
from an older shipped version. Recording both version and per-file hash at
install time answers both questions with about forty lines of stdlib.

**Alternatives rejected**: putting hashes in `SKILL.md` frontmatter `metadata`
(pollutes a document a human reads), and no record at all (turns every re-install
into either a silent overwrite or a blanket refusal).

## R8. Install writes both locations by default

**Decision**: with no flags, install writes both the canonical bundle and the
pointer. `--agent` narrows it. An unknown name fails and lists the supported
ones.

**Rationale**: FR-004 asks for detection. With only two destinations, both of
them small, detection buys nothing and costs a heuristic that is wrong for a
user who has not yet created their assistant's directory. Writing both is never
wrong and is less code. This is a deliberate simplification of FR-004 and is
recorded as such in the plan.

**Alternatives rejected**: probing for `.claude/`, `.cursor/`, `.codex/`,
`.github/`, and `AGENTS.md`. A reasonable design when there were four formats;
pointless now that there are two paths.

## R9. No `AGENTS.md` write

**Decision**: install does not create or edit `AGENTS.md`.

**Rationale**: all four assistants named in FR-003 read skills, so `AGENTS.md`
adds nothing for them. Editing a file the user owns, and that other tooling also
writes, is the kind of outward change that needs a reason. There is no reason
yet.

**Revisit when**: a user asks for an assistant that reads `AGENTS.md` but not
skills. Then the answer is a pointer block, never a copy of the bundle.

## R10. The prompting content has no documentation page to point at

**Decision**: vendor it into `references/prompting.md`, and exempt that one file
from the pointer test with a stated reason rather than a silent skip.

**Rationale**: FR-038 says every claim names the page that owns it. Voice
prompting guidance is the one part of the bundle this project originates, as
the spec's Assumptions already record. An exemption that a test knows about and
names is honest. A quiet pass is not.

**Follow-up, not in this feature**: give prompting a documentation page, then
remove the exemption.

## Open items carried into the plan

- The constitution fixes the command surface at four commands. This feature adds
  a fifth. The amendment is a task, not a footnote. See Complexity Tracking in
  the plan.
- `CLAUDE.md` says three places document a change. With the documentation site
  and now the skill, it is five. `docs-site/README.md` already flags the fourth
  and calls the amendment a maintainer decision. FR-034 makes it this feature's
  job.

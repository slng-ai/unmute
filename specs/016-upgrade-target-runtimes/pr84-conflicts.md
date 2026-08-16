# Conflicts with PR #84

**Checked**: 2026-08-16 against `feat/unmute-cli-workdir-livekit-53d1c2` at `4ab8299`

PR #84, "Work inside the agent folder, scaffold LiveKit by default", is open
against `main` and touches 51 files. This feature touches 93. **17 files were
common to both.**

**Status: resolved, 2026-08-16.** This branch is rebased onto PR #84's head and
stacked on it, so the two merge in order: #84 first, this second. Every
resolution below was applied during that rebase, exactly as written, and the
full gate is green on the combined result. The sections stay as the record of
what was decided and why.

## Already fixed on this branch

Two collisions were real defects, not just merge friction, and are corrected
here rather than left for the merge.

| Was | Now |
|---|---|
| Both features numbered their spec directory `015` | This feature moved to `specs/016-upgrade-target-runtimes`. PR #84 keeps `specs/015-cwd-livekit-defaults`. |
| This feature's schema amendments were written as N42 and N43, which **already exist on main** (and PR #84 adds N45) | Renumbered to **N46** (exact version pin) and **N47** (run modes), and moved after N44 so the list stays in order. N45 is left free for PR #84. |

The duplicate amendment numbers were a genuine bug: main already carried N42,
N43, and N44, so this branch had two of each.

## The conflicts and their resolutions (applied)

### 1. `internal/cli/dev.go` — the only code conflict

Both edit the same hunk in `newDevCmd`.

PR #84 changes `Use` to `dev [agent-dir]`, `Args` to `cobra.MaximumNArgs(1)`,
adds a `Long`, and replaces `root := args[0]` with `packageDir(cmd, args)`.
This feature changes the `Short` line and removes the `--console` dispatch.

**Resolution: take both.** Keep PR #84's `Use`, `Args`, `Long`, and
`packageDir` call. Keep this branch's `Short`
(`Compile, run the agent locally, and talk to it in the browser.`) and the
`--console` rejection. **PR #84's `Short` and `Long` both still say
`or terminal (--console)`; that text has to go**, because the flag does not
exist after this feature. The `--console` error message reads `root`, which
after the merge is whatever `packageDir` returned, and still works.

### 2. `docs-site/dev/console.mdx` — delete versus modify

This feature deletes the page; PR #84 adds four lines to it explaining that the
package argument is optional.

**Resolution: keep the deletion.** The page documents a flag that no longer
exists, and PR #84's addition describes how to invoke it. Also confirm
`docs-site/docs.json` has no `dev/console` nav entry left; this branch removed
it.

### 3. Regenerated goldens: `specs/008-mintlify-user-docs/help.txt`, `internal/scaffold/testdata/golden/init.txt`

Both branches regenerate both files.

**Resolution: do not hand-merge.** Take either side, then rerun:

```bash
go test ./internal/cli -run TestHelpCaptureMatchesBinary -update && go test ./internal/scaffold -update
```

### 4. Prose overlaps

`README.md`, `docs/TESTING.md`, `docs/TELEPHONY.md`, `docs-site/dev/overview.mdx`,
`docs-site/reference/cli/dev.mdx`, `docs-site/reference/cli/overview.mdx`,
`docs-site/start/quickstart.mdx`, `docs-site/targets/overview.mdx`,
`internal/skill/assets/references/workflow.md`,
`internal/skill/assets/references/models.md`.

Both sides rewrite overlapping regions: PR #84 documents the optional directory
argument and the LiveKit-by-default scaffold, this feature removes console mode
and updates framework versions. The edits are compatible in meaning. Wherever
PR #84's text mentions `--console`, terminal mode, `agent.py dev`, or
`agent.py console`, that half is superseded.

### 5. `internal/scaffold/scaffold.go` — no conflict expected

PR #84 changes `DefaultTarget` (line ~80); this feature changes `SetTarget`'s
version defaults (line ~325). Different regions, and compatible: after both,
`unmute init` scaffolds a LiveKit target pinned to the verified ceiling.

## The semantic break the drift gate caught (fixed)

**PR #84 adds `version: "1.5.2"` in three places this feature's new drift test
sweeps.** After merging both, `make test` fails until they are bumped to
`1.6.10`, because 1.5.2 is no longer any framework's ceiling:

| File | Fix |
|---|---|
| `docs-site/reference/cli/init.mdx` | `version: "1.5.2"` becomes `version: "1.6.10"` |
| `internal/scaffold/testdata/golden/init.txt` | regenerate (see item 3); the scaffold now writes the ceiling |
| `docs/SCHEMA.md` | PR #84's N45 text quotes `version: "1.5.2"`; bump the quoted value |

`specs/015-cwd-livekit-defaults/*` also carries `1.5.2`, and needs no change:
`specs/` is a documented carve-out of the sweep, because specs record history.

This is the drift gate doing its job. The failure names the file and the
expected value.

## Merge order

Stacked: PR #84 is the bottom, this branch's PR sits on it. Merge #84, then
this one. The stack view on GitHub shows both in order.

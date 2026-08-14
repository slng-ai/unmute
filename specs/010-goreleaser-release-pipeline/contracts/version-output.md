# Contract: version output and stamping agreement

**Date**: 2026-08-14 | Implements D9 and FR-005/FR-006.

## The one agreement

Three link-time variables, GoReleaser's own default names, stamped
identically by both build paths:

| Var | GoReleaser (`.goreleaser.yaml`) | Makefile (`LDFLAGS`) |
|---|---|---|
| `main.version` | `{{.Version}}` (tag without `v`) | `git describe --tags --always` (unchanged) |
| `main.commit` | `{{.ShortCommit}}` | `git rev-parse --short HEAD` |
| `main.date` | `{{.CommitDate}}` | `git log -1 --format=%cI` |

Correction, 2026-08-14 (during implementation): this table first said
`{{.Commit}}`, which is the full 40-character SHA. That contradicted the output
table below, which shows a short SHA, and it made the two build paths print
different shapes for the same commit. `{{.ShortCommit}}` is the correct
template and is what `.goreleaser.yaml` uses.

This is the one fact that lives in two places (Constitution III). The
mitigation is this contract plus execution from both sides: `make build`
exercises the Makefile path daily, and the PR snapshot assertion
([release-workflow.md](release-workflow.md), step 6) exercises the
GoReleaser path on every PR. If either side renames a var, one of the two
`--version` checks stops matching.

## `main.go`

```go
var (
    version = "dev" // stamped at link time (see Makefile / .goreleaser.yaml)
    commit  = ""
    date    = ""
)
```

`main` composes one string and passes it to the existing
`cli.Execute(version string)` — no change to `internal/cli`:

- commit empty (a bare `go build`): pass `version` as is.
- otherwise: `version (commit date)`, for example
  `1.2.0 (3a9f2c1 2026-08-14T10:11:12+02:00)`.

## Output shape (cobra prints `unmute version <string>`)

| Build | `unmute --version` prints |
|---|---|
| Released `v1.2.0` | `unmute version 1.2.0 (3a9f2c1 <commit date>)` |
| Snapshot / `make release-dry` | `unmute version 1.2.0-SNAPSHOT-3a9f2c1 (3a9f2c1 <commit date>)` |
| `make build` | `unmute version <git describe> (<short sha> <commit date>)` |
| Bare `go build` | `unmute version dev` |

## Assertions

1. Reproducibility: none of the three values may derive from wall-clock
   time; `date` is the commit date on both paths (FR-006).
2. The released value of `main.version` equals the tag without the leading
   `v` (FR-005); the snapshot value contains `SNAPSHOT` (used by the CI
   assertion).
3. No version string is hardcoded anywhere (existing repo rule); the only
   defaults in source are `"dev"` and empty strings.
4. Docs that show `--version` output (README, installation.mdx) show the
   new shape.

# Data Model: GoReleaser Release Pipeline

**Date**: 2026-08-14 | **Feature**: [spec.md](spec.md) | **Sources**: [research.md](research.md)

No database and no runtime data. The "data" of this feature is the artifact
set a tag produces, the channels that consume it, and the phase gates that
decide what publishes. Names below are the concrete defaults verified in
research; they are the contract the docs and runbook quote.

## Version tag

| Field | Rule |
|---|---|
| Shape | `vX.Y.Z`, optional prerelease suffix (`v1.2.0-rc.1`) |
| Who may create | admins / named maintainer team (GitHub tag ruleset, external prerequisite 5) |
| Effect | triggers `release.yml`; `{{.Version}}` = tag without `v` |
| Prerelease | GitHub Release marked prerelease (`prerelease: auto`); cask and winget skip (`skip_upload: "auto"` once enabled) |
| Snapshot (no tag) | version becomes `<version>-SNAPSHOT-<shortsha>`; nothing publishes |

## Artifact set (one per release, all under `dist/` then the GitHub Release)

| Artifact | Name | Count |
|---|---|---|
| Binary | `unmute` (`unmute.exe` on Windows), static, CGO off | 6 (3 OS x 2 arch) |
| Archive | `unmute_<version>_<os>_<arch>.tar.gz` (`.zip` for windows) | 6 |
| Archive contents | binary + `LICENSE` + `README.md` (GoReleaser default `files`) | per archive |
| Checksums | `unmute_<version>_checksums.txt` (sha256, covers all archives) | 1 |
| Signature | `unmute_<version>_checksums.txt.sigstore.json` (cosign keyless bundle: cert + sig in one file) | 1 |
| SBOM | `<archive-name>.sbom.json` (SPDX JSON, syft) | 6 (one per archive) |
| Release notes | groups: Features, Bug fixes, Others; excludes docs/style/test/chore | 1 |

Validation rules:
- Every binary must print `<version> (<commit> <commit-date>)` from
  `--version` (see [contracts/version-output.md](contracts/version-output.md)).
- Same tag built twice = byte-identical binaries (trimpath, commit
  timestamp, commit date; no wall clock).
- Phase 2 additionally: `go version -m` on a released binary shows module
  `github.com/slng-ai/unmute` and the release version (gomod proxy mode).

## Channels

| Channel | Consumes | Platform truth | Written where | Gate |
|---|---|---|---|---|
| GitHub Release | all artifacts | all 6 pairs | `slng-ai/unmute` releases | Phase 1 (private release ok) |
| Homebrew cask | darwin archives | macOS only, never Linux | `slng-ai/homebrew-tap` `Casks/unmute.rb`, direct commit | Phase 2 (`skip_upload: true` → `"auto"`) |
| winget | windows zip archives | Windows | fork branch `unmute-<version>` → PR to `microsoft/winget-pkgs` master, manifests at `manifests/s/slng/unmute/<version>` | Phase 3 (`skip_upload: true` → `"auto"`) |
| Direct download / `go install` | archives / source | everyone; the only Linux channel | release page / module proxy | Phase 1 / Phase 2 (public module) |

## Phase gates (each flip is one line, per FR-015)

| Phase | Precondition (external) | Flips in-repo |
|---|---|---|
| 1 (now, private) | tag ruleset | none: ships with `skip_upload: true` on both publishers, `gomod.proxy` commented out |
| 2 (public) | repo public, `GH_PAT` secret set | cask `skip_upload` → `"auto"`; uncomment `gomod: {proxy: true}`; delete the installation.mdx module-path note |
| 3 (stable public release) | winget fork exists, PAT covers it | winget `skip_upload` → `"auto"`; fill `repository.owner` with the fork owner |

## Identities and secrets

| Item | Value | Notes |
|---|---|---|
| Module path | `github.com/slng-ai/unmute` | task 1; must equal the repo URL |
| Cask name / binary | `unmute` | equal by default, no override needed |
| winget identity | publisher `slng`, identifier `slng.unmute` | license `MIT` (required field) |
| Publisher URLs | `https://slng.ai`, support: repo issues page | winget optional fields, set explicitly |
| `GITHUB_TOKEN` | default Actions token | creates the release only; cannot reach other repos |
| `GH_PAT` | fine-grained PAT, maintainer's account | contents-write on tap + fork; the only added credential; referenced only as `{{ .Env.GH_PAT }}` |

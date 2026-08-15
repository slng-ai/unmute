# Data Model: Windows Release Channels (winget + Scoop)

No application data changes. The "model" of this feature is channels,
credentials, external repos, and doc states. Feature 010's data-model.md
stays the record for the pre-existing channels; this file holds the delta.

## Channels after this feature

| Channel | Input | Store | Live when | Prerelease |
| --- | --- | --- | --- | --- |
| Scoop (new) | Windows zip archives (existing) | `slng-ai/scoop-bucket`, manifest `unmute.json` in root | the release run finishes | skipped (`"auto"`) |
| winget (opened) | Windows zip archives (existing) | fork branch `unmute-<version>` → PR to `microsoft/winget-pkgs`, manifests at `manifests/s/slng/unmute/<version>` | Microsoft merges, index refreshes | skipped (`"auto"`) |
| Homebrew, `go install`, direct download | unchanged | unchanged | unchanged | unchanged |

## Credentials

| Secret | Kind | Reaches | Change in this feature |
| --- | --- | --- | --- |
| `GH_PAT` | fine-grained, maintainer-owned, contents read/write | `slng-ai/homebrew-tap` + `slng-ai/scoop-bucket` | bucket repo added to its repo list (console edit, no re-mint) |
| `GH_PAT_WINGET` | classic, `public_repo` only, maintainer-owned | maintainer's `winget-pkgs` fork; opens the upstream PR | new secret; new `env` line in `release.yml` |
| `GITHUB_TOKEN` | Actions-provided | this repo's release | unchanged |

Validation rules (from FR-006): every secret has a real expiry plus a
calendar reminder; values exist only in the Actions secret store; only the
names appear in any file.

## External repos

| Repo | Owner | Created by | Written by |
| --- | --- | --- | --- |
| `scoop-bucket` | `slng-ai` org | hand, once, public, with README | release runs only |
| `winget-pkgs` fork | `slng-ai` org (maintainer is admin) | `gh repo fork --org slng-ai --clone=false --default-branch-only`, once | release runs only (GoReleaser syncs it with upstream master before each push) |

## Doc states (lifecycle)

| State | Trigger | `installation.mdx` + `README.md` say |
| --- | --- | --- |
| Current | — | winget listed but marked not working yet |
| A | the flip PR merges | Scoop: guided Windows path, two commands. winget: "coming soon", submitted on release, waiting on Microsoft review |
| B | Microsoft merges the first manifest PR | winget: primary Windows path. Scoop: kept, described as the fastest on every release. No "coming soon" left |

State transitions are one-way and each is one PR. A doc must never describe
a command that cannot work at the time of reading (spec edge case "Docs
promise winget too early").

## Config identity (unchanged facts both manifests share)

| Fact | Value | Source |
| --- | --- | --- |
| Binary and command | `unmute` (`unmute.exe` in the zip) | `builds` block, feature 010 |
| Description | "Compile a declarative voice-agent package to Pipecat or LiveKit" | same string as cask and winget blocks |
| License | MIT | required by winget, set explicitly on Scoop too |
| Homepage | `https://github.com/slng-ai/unmute` | same as cask |
| Version output | `unmute version X.Y.Z (<commit> <date>)` | FR-008, ldflags, feature 010 |

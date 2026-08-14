# Quickstart: validating the release pipeline

**Date**: 2026-08-14 | **Feature**: [spec.md](spec.md)

Runnable checks that prove the feature works, from a laptop to Phase 3.
Artifact names come from [data-model.md](data-model.md); config and workflow
shapes from [contracts/](contracts/).

## Prerequisites (local)

```sh
brew install goreleaser syft        # cosign not needed locally (dry runs skip sign)
goreleaser healthcheck              # confirms required tools are on PATH
```

## 1. Config is valid (SC-008)

```sh
goreleaser check
```

Expected: exit 0. Exit 2 means a deprecated key crept in; exit 1 means
invalid. Both are failures.

## 2. Local dry run (SC-002, FR-013, FR-014)

```sh
make release-dry
```

Expected under `dist/`: 6 binaries in 6 archives
(`unmute_<ver>_darwin_amd64.tar.gz`, `..._darwin_arm64.tar.gz`,
`..._linux_amd64.tar.gz`, `..._linux_arm64.tar.gz`, `..._windows_amd64.zip`,
`..._windows_arm64.zip`), one `unmute_<ver>_checksums.txt`, six
`*.sbom.json`, plus the unpublished cask and winget manifests. Nothing is
published anywhere. Then:

```sh
tar -xOf dist/unmute_*_darwin_arm64.tar.gz unmute > /tmp/u && chmod +x /tmp/u
/tmp/u --version    # prints: unmute version <ver>-SNAPSHOT-<sha> (<sha> <commit date>)
```

(On Linux use the linux archive; this is what the CI assertion does.)

## 3. PR gate (SC-003)

Open a PR that breaks `.goreleaser.yaml` (for example, misspell a key). The
`release-config` job must fail on `goreleaser check`. Revert; the job must
pass and its assertion step must report the 6 archives and the snapshot
version.

## 4. Phase 1: a private test release (User Story 1)

```sh
git tag v0.1.0 && git push origin v0.1.0
```

Expected: one workflow run; a GitHub Release on the private repo with 6
archives, checksums, `checksums.txt.sigstore.json`, 6 SBOMs, and grouped
notes (docs/style/test/chore commits absent). A downloaded binary prints
`0.1.0 (<sha> <date>)`. The run must NOT touch the tap (skip_upload) and
must need no manual step after the push (SC-001). Clean up a test tag by
deleting the release and the tag.

Also verify the signature from the release page alone (SC-007):

```sh
cosign verify-blob --bundle unmute_0.1.0_checksums.txt.sigstore.json unmute_0.1.0_checksums.txt
shasum -a 256 -c unmute_0.1.0_checksums.txt --ignore-missing
```

## 5. Phase 2 checks (after: repo public, GH_PAT set, cask flip, gomod flip)

```sh
brew install slng-ai/tap/unmute   # clean macOS machine, both arches
unmute --version                   # no Gatekeeper dialog, no xattr by hand (SC-004)
go install github.com/slng-ai/unmute@latest   # may lag minutes on first fetch (proxy cache)
go version -m $(which unmute) | head -5        # shows module path + version (SC-005)
```

Order matters on release day: tag the first public release, verify brew,
then announce; the install docs describe Phase 2 reality and go live with
the flip.

## 6. Phase 3 checks (after: fork, winget flip, FORK_OWNER filled)

Tag a stable release, then:

- A PR exists on `microsoft/winget-pkgs` from the fork, manifests under
  `manifests/s/slng/unmute/<version>`, PR body from upstream's template.
- Locally: `winget validate --manifest <dir>` passes (SC-006).
- First PR only: the maintainer answers the Microsoft CLA bot once.
- After merge, on Windows: `winget install slng.unmute` then
  `unmute --version`.

## Regression guardrails that stay on after this feature

- `release-config` CI job: config validity + full snapshot + artifact
  assertion, every PR.
- `goreleaser check` exit-2 behavior: a deprecation in any used key turns
  CI red the day GoReleaser announces it.
- The version-output agreement is exercised from both build paths
  ([contracts/version-output.md](contracts/version-output.md)).

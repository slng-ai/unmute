# Releasing unmute

One command releases. Everything else in this file is context for when it
misbehaves or when a channel is ready to open.

```sh
git tag v1.2.0 && git push origin v1.2.0
```

The push starts `.github/workflows/release.yml`, which runs
`goreleaser release --clean` against `.goreleaser.yaml`. There is no second
step, no button to press, and no artifact to upload by hand.

## Who can release

Tag creation matching `v*` is restricted by a GitHub tag ruleset on
`slng-ai/unmute` to admins or the named maintainer team. If your push is
rejected, you are not on that list; that is the ruleset working.

Tag shape is `vX.Y.Z`, with an optional prerelease suffix (`v1.2.0-rc.1`).
GoReleaser strips the `v`, so `v1.2.0` ships as version `1.2.0`. A prerelease
tag is marked prerelease on GitHub and, once the channels open, is skipped by
both package managers.

## What one tag produces

| Artifact | Name | Count |
|---|---|---|
| Binaries | `unmute` (`unmute.exe` on Windows), static, CGO off | 6 (darwin/linux/windows x amd64/arm64) |
| Archives | `unmute_<version>_<os>_<arch>.tar.gz`, `.zip` on Windows | 6 |
| Archive contents | the binary plus `LICENSE` and `README.md` | per archive |
| Checksums | `unmute_<version>_checksums.txt` (sha256) | 1 |
| Signature | `unmute_<version>_checksums.txt.sigstore.json` | 1 |
| SBOMs | `<archive>.sbom.json` (SPDX, from syft) | 6 |
| Release notes | Features / Bug fixes / Others; docs, style, test and chore commits excluded | 1 |

Two builds of the same tag are byte-identical: `-trimpath`, the commit
timestamp, and the commit date, with no wall clock anywhere.

Every binary knows where it came from:

```
unmute version 1.2.0 (3a9f2c1 2026-08-14T10:11:12Z)
```

## The rollout phases

The pipeline ships complete but with both package managers switched off, because
each one needs something that does not exist yet. Opening a channel is one line.

| Phase | Needs (outside this repo) | The flip, in `.goreleaser.yaml` |
|---|---|---|
| 1 (now) | the tag ruleset | nothing. Both publishers say `skip_upload: true`, `gomod` stays commented out |
| 2 | repo public, `GH_PAT` secret set | cask `skip_upload: true` → `"auto"`; uncomment the `gomod: proxy: true` block; delete the `go install` Note in `docs-site/start/installation.mdx` |
| 3 | a fork of `microsoft/winget-pkgs`, PAT covering it | winget `skip_upload: true` → `"auto"`; replace `owner: FORK_OWNER` with the fork's owner |

`"auto"` rather than `false`: it uploads stable releases and skips prereleases,
which is what you want for both channels.

Order on the day Phase 2 opens: make the repo public, add the secret, flip the
cask, tag, watch the run, verify `brew install` on a clean Mac, then announce.

## The one secret

`GH_PAT` is a fine-grained personal access token owned by the maintainer, with
contents-write on `slng-ai/homebrew-tap` and (from Phase 3) on the winget fork.
It is stored as an Actions secret on `slng-ai/unmute`. Only its name appears in
any file in this repo.

It exists because `GITHUB_TOKEN`, the token Actions provides, cannot write to any
other repository. It creates the release and nothing more.

The token's owning account is the one that authors the tap commits and the winget
PRs, and it is the account that answers Microsoft's CLA bot the first time.

A snapshot run needs no token at all: `GH_PAT` can be unset and
`make release-dry` still succeeds, because snapshot mode never publishes and so
never evaluates the `{{ .Env.GH_PAT }}` template (verified 2026-08-14).

## Rehearsing before you tag

```sh
brew install goreleaser syft   # cosign is CI-only; dry runs skip signing
goreleaser check               # exit 0 required; 2 means a deprecated key
make release-dry               # the whole pipeline into dist/, publishing nothing
```

`make release-dry` is the exact command the `release-config` CI job runs, and
that job runs on every pull request. If it is green there, the config is valid,
all 6 platforms build, and the version stamps survived.

## Verifying a release

From the release page alone, with nothing but the file names:

```sh
cosign verify-blob \
  --bundle unmute_1.2.0_checksums.txt.sigstore.json \
  unmute_1.2.0_checksums.txt
shasum -a 256 -c unmute_1.2.0_checksums.txt --ignore-missing
```

`cosign verify-blob` will also want the signing identity, which for a keyless
GitHub Actions signature is the workflow's own URL:

```sh
cosign verify-blob \
  --bundle unmute_1.2.0_checksums.txt.sigstore.json \
  --certificate-identity-regexp 'https://github.com/slng-ai/unmute/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  unmute_1.2.0_checksums.txt
```

The checksums file covers every archive, so verifying it plus one `shasum -c`
covers the artifact you actually downloaded. The SBOMs are plain SPDX JSON; any
SBOM tool reads them.

From Phase 2 a released binary also carries its own provenance:

```sh
go version -m $(which unmute) | head -5
```

## When it goes wrong

**A run failed halfway.** Re-run it. `--clean` wipes `dist/` first and the
default release mode does not duplicate assets, so a re-run of the same tag is
safe. If the tag itself was wrong, delete the release and the tag, then tag
again.

**`GH_PAT` is missing or expired** (Phase 2 onward). The cask step fails loudly
and the run goes red. The GitHub Release itself is already published by then, so
mint a new token and re-run.

**Microsoft rejects or flags the winget PR** (Phase 3). GoReleaser logs it and
the release still succeeds. This is deliberate: an upstream review must never
dirty our release. Follow up on the PR by hand.

**The first winget PR sits there.** Microsoft's CLA bot wants an answer, once,
from the account that opened the PR.

**`go install` cannot find the module** just after going public. The module
proxy caches on first fetch; it can lag a few minutes. Wait, then retry.

**Antivirus flags a Windows binary.** Unsigned Go binaries draw false positives.
The checksums and the signature bundle are the answer; there is no code signing
certificate in this pipeline.

**Gatekeeper blocks a macOS binary.** Through `brew` it will not: the cask's
post-install hook strips the quarantine attribute. A binary downloaded straight
from the release page is quarantined by the browser, and
`xattr -dr com.apple.quarantine ./unmute` clears it.

## Where the design lives

`specs/010-goreleaser-release-pipeline/` holds the spec, the plan, and the
contracts for the config, the workflow, and the `--version` output. When this
file and one of those disagree, the contract wins.

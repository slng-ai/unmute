# Feature Specification: GoReleaser Release Pipeline

**Feature Branch**: `010-goreleaser-release-pipeline`

**Created**: 2026-08-14

**Status**: Draft

**Input**: User description: "GoReleaser release pipeline for the unmute CLI: GitHub Releases plus Homebrew cask and winget channels. A tag-driven pipeline: pushing vX.Y.Z produces cross-platform binaries, archives, checksums, SBOMs, a signed GitHub Release with a grouped changelog, an updated Homebrew cask in our own tap, and (once public) a winget manifest PR. GoReleaser v2 open source features only. One config file, validated in CI on every PR, zero manual release steps after the tag push."

## The story

Today there is no way to ship unmute to a user. The Makefile builds one binary
for the machine it runs on, the version comes from `git describe`, and nothing
leaves the repo. A user who wants the CLI has to clone and build it.

This feature makes a version tag the one and only release action. A maintainer
runs `git push origin vX.Y.Z` and walks away. One automated run then builds the
binary for every platform we serve, packages and checksums everything, signs
the checksums, attaches software bills of materials, writes release notes
grouped from our conventional commits, publishes the GitHub Release, updates
the Homebrew cask in our own tap, and, once the repo is public and stable,
opens a manifest PR to Microsoft's winget repository. No step in that list is
manual. No step uses a paid GoReleaser feature.

The repo is private today and has no LICENSE file. It was renamed to
`github.com/slng-ai/unmute` on 2026-08-14, but the module path in `go.mod`
still says `github.com/slng/unmute` and must change to match. So the rollout
is phased: the pipeline lands now, fully configured but with external
publishing switched off, and each channel turns on with a one-line change when
its preconditions are met.

## Clarifications

### Session 2026-08-14

- Q: Which license, MIT or Apache-2.0 (D2)? → A: MIT.
- All other open decisions (D1 to D12) were resolved by adopting the
  maintainer's recommendations given in the feature brief. The full
  resolutions are in the Decisions section below.
- Update (maintainer): the repo has been renamed to `slng-ai/unmute`.
  Verified 2026-08-14: the renamed repo exists and is still private, the tap
  repo `slng-ai/homebrew-tap` already exists and is public, the module path
  in `go.mod` is still `github.com/slng/unmute`, no winget fork exists, and
  the `GH_PAT` secret is not set. Prerequisite statuses below reflect this.
- Q: Should the module path change to `github.com/slng-ai/unmute` be part of
  this feature or land separately? → A: Part of this feature, as its first
  task: one mechanical commit (go.mod plus import prefix rewrite), proven by
  the full test gate, before any release config is written.
- Q: Who should be able to trigger a release (push a `v*` tag)? → A: Protect
  the tags: a GitHub tag ruleset on the repo restricts `v*` tag creation to
  admins or a named maintainer team. Contributors keep normal push access.
- Q: Which GitHub identity owns the `GH_PAT` that writes to the tap and
  opens winget PRs? → A: The maintainer's own account (nicola). Tap commits
  and winget PRs carry that name, and that account signs the Microsoft CLA
  once. A dedicated bot account is a possible later swap, not built now.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - One tag push produces the whole release (Priority: P1)

A maintainer decides the code on `main` is ready. They create and push a
version tag. Minutes later, a GitHub Release exists for that tag containing:
six binaries (macOS, Linux, Windows, each on amd64 and arm64), one archive per
platform, a checksums file, a signature for the checksums file, one SBOM per
archive, and release notes grouped into Features, Fixes, and Others. The
maintainer did nothing after the push.

**Why this priority**: This is the product. Every channel downstream (brew,
winget, direct download) consumes what this run produces. Without it, nothing
else in this spec can exist.

**Independent Test**: Push a test tag on the private repo (Phase 1). Confirm
the run completes, the release exists with every artifact listed above, and a
downloaded binary prints the tag version from `unmute --version`.

**Acceptance Scenarios**:

1. **Given** a repo with the pipeline landed, **When** a maintainer pushes tag `vX.Y.Z`, **Then** one automated run produces a GitHub Release for that tag with 6 binaries in 6 archives, a checksums file, its signature, and one SBOM per archive, with no human action after the push.
2. **Given** the published release, **When** a user downloads the archive for their platform and runs `unmute --version`, **Then** the output shows the tag version, the commit, and the commit date.
3. **Given** commits since the previous tag that follow conventional commits, **When** the release notes are generated, **Then** features and fixes appear in their own groups and docs, style, test, and chore commits do not appear at all.
4. **Given** the same tag built twice, **When** the two runs are compared, **Then** the binaries are byte-identical (reproducible builds).

---

### User Story 2 - A broken release setup cannot reach main (Priority: P2)

A contributor edits the release config, or touches something the release
depends on (build flags, main package, version variables). Their PR runs the
same checks every PR runs: the release config is validated and a full local
snapshot build runs end to end, publishing nothing. If the release would
break, the PR goes red before merge, not on release day.

**Why this priority**: A release pipeline that is only exercised on tag day
breaks on tag day. Validation on every PR is what makes "zero manual steps"
safe to promise.

**Independent Test**: Open a PR that introduces a config error. CI must fail.
Revert it, CI must pass, and the snapshot job must produce all artifacts
without publishing anything anywhere.

**Acceptance Scenarios**:

1. **Given** a PR with an invalid release config, **When** CI runs, **Then** the config check fails and the PR cannot merge.
2. **Given** a PR with a valid config, **When** CI runs, **Then** a snapshot build produces 6 binaries, archives, checksums, and SBOMs, and publishes nothing: no release, no tap commit, no winget PR.
3. **Given** a maintainer's machine, **When** they run the dry-run make target, **Then** they get the same snapshot artifacts locally under the build output directory, and the built binary prints a snapshot version.

---

### User Story 3 - A macOS user installs with one brew command (Priority: P3)

Once the repo is public (Phase 2), a macOS user runs
`brew install slng-ai/tap/unmute`. Homebrew picks the right binary for their
chip, installs it, and `unmute --version` runs immediately, with no Gatekeeper
"app is damaged" dialog and no manual quarantine workaround.

**Why this priority**: brew is the main distribution channel for a developer
CLI on macOS, and macOS is where the Gatekeeper trap lives. It is third only
because it needs the public repo and the tap to exist first.

**Independent Test**: On a clean macOS machine (or fresh user account) with
Homebrew installed, run the one install command and then `unmute --version`.
No other setup allowed.

**Acceptance Scenarios**:

1. **Given** a published release and the updated tap, **When** a macOS user on Apple silicon or Intel runs the install command, **Then** the correct binary for their architecture is installed.
2. **Given** the fresh install, **When** the user runs `unmute --version`, **Then** it prints the version with no Gatekeeper error and no manual `xattr` step.
3. **Given** a new release tag, **When** the release run finishes, **Then** the cask in the tap already points at the new version, with no human commit to the tap.

---

### User Story 4 - Anyone can verify what they downloaded (Priority: P4)

A security-conscious user (or a company's supply-chain policy) wants proof
that a downloaded artifact is what the project built. From the release page
alone they can: check their archive against the checksums file, verify the
checksums file's signature against the public transparency log, and read the
SBOM to see what is inside. Once the repo is public, they can additionally
prove the binary's origin: the binary itself reports the public module path
and version, verifiable against the public Go module ecosystem.

**Why this priority**: Cheap to provide (all free features), increasingly
expected, and impossible to retrofit onto already-published releases.

**Independent Test**: Download an archive, the checksums file, and the
signature from a release. Verify the checksum matches and the signature
verifies. Run the standard Go tooling against the extracted binary and read
the embedded module path and version.

**Acceptance Scenarios**:

1. **Given** a release, **When** a user recomputes their archive's SHA256, **Then** it matches the checksums file.
2. **Given** the checksums file and its signature, **When** the user verifies keyless against the public log, **Then** verification succeeds and names this repo's release workflow as the signer.
3. **Given** a Phase 2 release binary, **When** the user inspects it with standard Go tooling, **Then** it reports the public module path and the release version.

---

### User Story 5 - A Windows user installs with winget (Priority: P5)

After the first stable public release (Phase 3), the release run pushes a
manifest branch to our fork of Microsoft's winget repository and opens a PR
upstream. Microsoft's automated validation downloads the installers, checks
hashes, scans, and test-installs. Once merged, a Windows user runs
`winget install slng.unmute` and gets the CLI.

**Why this priority**: Real value, but it has the most preconditions: public
repo, public downloads that Microsoft's pipeline can fetch without auth, a
stable release, and an org fork with a PAT. It cannot come earlier.

**Independent Test**: Tag a release in Phase 3. Confirm the PR opens against
`microsoft/winget-pkgs` with a manifest that passes `winget validate`
locally.

**Acceptance Scenarios**:

1. **Given** a Phase 3 release, **When** the run finishes, **Then** a PR exists on `microsoft/winget-pkgs` from our fork containing a manifest with publisher, license, and short description.
2. **Given** that manifest, **When** `winget validate` runs against it, **Then** it passes.
3. **Given** the merged manifest, **When** a Windows user runs the winget install command, **Then** the CLI installs and `unmute --version` prints the release version.

---

### Edge Cases

- **Prerelease tags** (`v1.2.3-rc.1`): the GitHub Release is marked as a
  prerelease, and neither the cask nor the winget manifest updates. Only
  stable tags reach the channels.
- **A tag while the repo is private** (Phase 1): the run completes and
  attaches everything to a private GitHub Release; all external publishing
  stays off. Nothing fails for being private.
- **The run fails midway** (network, a rejected tap push): re-running the
  workflow for the same tag is safe; the run starts clean and overwrites its
  own partial output rather than appending to it.
- **The publishing token is missing or expired**: the run fails loudly naming
  the missing secret. It must not silently skip a channel.
- **Microsoft rejects the winget PR** (validation failure, new policy): the
  release itself is unaffected; the runbook documents the manual follow-up.
  A rejected winget PR never blocks or dirties the GitHub Release.
- **Homebrew on Linux**: casks do not exist there. No document may imply
  `brew install` works for Linux; Linux users get the tarball or `go install`.
- **Shallow checkout**: changelog grouping and `git describe`-style versioning
  need full history; the release and snapshot jobs must fetch full history or
  the notes silently truncate.
- **Windows antivirus false positives**: unsigned Go binaries occasionally
  trip scanners, including Microsoft's winget sandbox. The runbook names this
  as a known risk with the re-run/appeal path.
- **First `go install` after going public**: the public module proxy caches;
  the first fetch of a new tag can lag minutes. Expected, documented, not a
  defect.

## Requirements *(mandatory)*

### Functional Requirements

**Trigger and artifacts**

- **FR-001**: Pushing a tag matching `v*` MUST start exactly one automated
  run that carries the release to completion with zero manual steps after the
  push.
- **FR-002**: Each release MUST contain binaries for six platform pairs:
  macOS, Linux, and Windows, each on amd64 and arm64, packaged one archive
  per pair (tar.gz, zip on Windows), all built as static binaries per the
  existing repo rule.
- **FR-003**: Each release MUST include one checksums file covering every
  archive, a keyless cryptographic signature of that checksums file
  verifiable against the public transparency log, and one SBOM per archive.
- **FR-004**: Release notes MUST be generated from commit history, grouped as
  Features, Fixes, and Others by conventional-commit prefix, with docs,
  style, test, and chore commits excluded.

**Version identity**

- **FR-005**: The released binary MUST print the tag version, the commit, and
  the commit date from `unmute --version`. All three are stamped at link
  time; none is hardcoded (existing repo rule, extended from version-only to
  all three).
- **FR-006**: Builds MUST be reproducible: building the same tag twice yields
  byte-identical binaries. No wall-clock time may enter the build.
- **FR-007** (Phase 2): Released binaries MUST be verifiable: standard Go
  tooling run against a downloaded binary reports the public module path and
  the release version, provable through the public module ecosystem.

**Channels**

- **FR-008** (Phase 2): The release run MUST update a Homebrew cask in the
  org's own tap (`slng-ai/homebrew-tap`, cask files under `Casks/`) by direct
  commit, so `brew install slng-ai/tap/unmute` serves macOS on both
  architectures. No PR-and-review flow on the tap.
- **FR-009**: The installed cask binary MUST run on first use with no
  Gatekeeper "damaged" error: the install removes the quarantine attribute.
  Code signing and notarization are documented as future work only.
- **FR-010** (Phase 3): The release run MUST push a manifest branch to the
  org's fork of `microsoft/winget-pkgs` and open a PR upstream. The manifest
  MUST carry publisher, license, and short description, and pass
  `winget validate`. Package identity: publisher `slng`, identifier
  `slng.unmute`.
- **FR-011**: Linux is served by the release tarball and `go install` only.
  Every install document MUST state this coverage honestly; no document may
  claim brew serves Linux.

**Validation and dry runs**

- **FR-012**: Every PR MUST validate the release setup: a config check plus a
  full snapshot build that exercises the entire pipeline locally.
- **FR-013**: A snapshot build MUST publish nothing: no GitHub Release, no
  tap commit, no winget PR, no external write of any kind.
- **FR-014**: A maintainer MUST be able to run the same snapshot build
  locally through one make target, alongside the existing build targets.

**Phasing and cost boundary**

- **FR-015**: The pipeline MUST land fully configured while the repo is
  private, with each external channel switched off by an explicit per-channel
  flag, so enabling a channel later is a one-line change and never a
  structural one.
- **FR-016**: The configuration MUST use only free, open-source GoReleaser v2
  features. No paid-tier key may appear anywhere in it, and the config MUST
  validate under the free distribution.

**Legal and documentation**

- **FR-017**: The repo MUST carry an MIT LICENSE file before any artifact is
  distributed, and the license name MUST appear in the winget manifest,
  where it is a required field. Corrected during planning (2026-08-14):
  Homebrew casks have no license field at all, so the cask carries none;
  brew users get the license through the LICENSE file, which every archive
  includes automatically.
- **FR-018**: The README install section and the user docs install page MUST
  list every install path (brew, winget, `go install`, direct download) with
  the honest platform coverage from FR-011, landing in the same change as the
  pipeline, per the repo's documentation rule.
- **FR-019**: A releasing runbook MUST exist covering: how to cut a release,
  who can cut one (the `v*` tag ruleset), exactly what a tag triggers, the
  three phases and the one-line flips between them, the required secrets and
  where they live, and the known failure modes from the edge cases above.

  **Unmet as of 2026-08-14, by maintainer decision.** `RELEASING.md` was
  written to satisfy this and then deleted before the feature merged. The
  facts it held now live only in `contracts/` and in this spec; nothing at the
  repository root tells a releaser what to do. Reopen this requirement if the
  runbook is wanted somewhere else, for example a `docs-site` page.

**Prerequisites and hygiene**

- **FR-020**: The module path MUST change to `github.com/slng-ai/unmute` to
  match the renamed repo, as the first task of this feature: `go.mod` plus
  the import prefix rewrite across the repo, in one mechanical commit, with
  the full test gate proving it. Every path in the release setup is written
  against the new module path. The repo rename itself is done (2026-08-14).
  Name collisions were checked 2026-08-14: no `unmute` cask or formula in
  homebrew-core, no `unmute` winget identifier.
- **FR-021**: Publishing to the tap and to the winget fork MUST authenticate
  with a fine-grained personal access token stored as a repo secret
  (`GH_PAT`), because the default workflow token cannot write to other
  repos. The workflow MUST grant itself only the permissions it needs.
- **FR-022**: The release build output directory MUST be ignored by git.

### Key Entities

- **Version tag**: the single release trigger, `vX.Y.Z`, optionally with a
  prerelease suffix. Owns the version string every artifact carries.
- **Release**: one GitHub Release per tag: archives, checksums file,
  signature, SBOMs, grouped notes.
- **Channel**: a downstream consumer of the release. Three exist: the
  Homebrew tap (macOS), winget (Windows), and direct download plus
  `go install` (everyone, and the only channel for Linux).
- **Phase**: a rollout state gating which channels are live. Phase 1 private
  (no external publishing), Phase 2 public (tap, verifiable builds), Phase 3
  stable (winget).
- **Publishing secret**: the fine-grained token that lets the release run
  write to the tap and the fork. The only credential this feature adds.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Cutting a release requires exactly one human action, the tag
  push. Zero follow-up actions, zero manual uploads, zero hand edits to any
  channel.
- **SC-002**: A local dry run yields exactly 6 binaries in 6 archives, one
  checksums file, and one SBOM per archive, and the built binary prints the
  snapshot version, on any maintainer machine with the toolchain installed.
- **SC-003**: A change that would break the release cannot merge: the PR
  gate turns red on an invalid release setup with no tag needed to find out.
- **SC-004** (Phase 2): A macOS user on a clean machine goes from nothing to
  a working `unmute --version` with one install command and no security
  dialog, in under two minutes on a normal connection.
- **SC-005** (Phase 2): `go install github.com/slng-ai/unmute@latest` works
  from a clean machine, and a downloaded release binary proves its own origin
  (public module path and version readable from the binary itself).
- **SC-006** (Phase 3): The winget PR passes Microsoft's validation, and a
  Windows user installs with one winget command.
- **SC-007**: Every artifact on a release page is verifiable by an outsider
  in under a minute using only the release page contents: checksum match plus
  one signature verification.
- **SC-008**: The release configuration contains no paid-tier feature, proven
  by validating under the free distribution in CI on every PR.

## Decisions

All twelve open decisions from the feature brief, resolved:

| # | Decision | Resolution |
|---|----------|------------|
| D1 | Public identity | Repo renamed to `slng-ai/unmute` (done 2026-08-14, still private). Module path becomes `github.com/slng-ai/unmute` to match; both must match before the first public release. Collisions checked 2026-08-14: none in homebrew-core or winget. |
| D2 | License | MIT (maintainer's choice, this session). |
| D3 | Gatekeeper | Quarantine-strip on install now. Apple Developer signing plus notarization is documented future work, not built. |
| D4 | macOS artifact shape | Per-arch binaries (amd64 + arm64). The cask selects the arch natively. No universal binary. |
| D5 | Linux channel | Tarball + `go install` only in v1. No deprecated brew formulas, no deb/rpm yet. Docs state the coverage honestly. |
| D6 | Tap publishing | Direct commit to `slng-ai/homebrew-tap`. No PR flow on our own tap. |
| D7 | winget identity | Publisher `slng`, identifier `slng.unmute`, publisher URL `https://slng.ai`, support URL the repo's issues page. |
| D8 | Changelog | Conventional-commit grouping: Features, Fixes, Others. Exclude docs, style, test, chore. |
| D9 | Version stamping | `unmute --version` shows version, commit, and date. Three link-time variables, matching Go vars added alongside the existing one. |
| D10 | Rollout phasing | Phase 1 now (private, publishing off, CI validates). Phase 2 at repo-public (tap on, verifiable builds on, first public tag). Phase 3 after a stable public release (winget on). |
| D11 | Supply chain | All three: checksums, keyless signature of the checksums file, per-archive SBOMs. All free features. |
| D12 | Makefile parity | New `release-dry` target wrapping the snapshot build. `make build` unchanged. |

## Scope

### In scope

- The module path change to `github.com/slng-ai/unmute` (go.mod plus the
  import prefix rewrite), landed as this feature's first task (FR-020).
- One release configuration file covering: 6-platform builds, archives,
  checksums, SBOMs, signing, grouped changelog, GitHub Release, the Homebrew
  cask channel, and the winget channel.
- One release workflow triggered on `v*` tags.
- PR CI validation of the release setup (config check + snapshot build).
- The MIT LICENSE file.
- Install documentation: README install section and the user docs install
  page, honest per D5, same change as the pipeline.
- The releasing runbook (FR-019).
- The `release-dry` make target and the git-ignore entry for build output.

### Out of scope (do not design for these)

Scoop, deb/rpm/nfpm, AUR, Nix, Docker images, nightly builds, MSI/NSIS
installers, a `curl | sh` install script, macOS notarization implementation
(documented as future work only), and every GoReleaser paid-tier feature.

## External prerequisites (human tasks, outside this repo)

These are not deliverables of this feature. They gate the phases. Statuses
verified 2026-08-14:

1. ~~Rename the repo to `slng-ai/unmute`~~ **Done 2026-08-14.** Still to do:
   make the repo public (timing per D10, gates Phase 2). The module path
   change is tracked in FR-020, not here.
2. ~~Create the public repo `slng-ai/homebrew-tap`~~ **Done, exists and is
   public.** Casks live under `Casks/`.
3. Fork `microsoft/winget-pkgs`. Simplest under the maintainer's own
   account, since that account owns the PAT (below); under the org also
   works if the org allows fine-grained PATs on it. **Pending.** Gates
   Phase 3.
4. The maintainer (nicola) mints a fine-grained PAT from their own account
   with contents-write on the tap and the fork; store it as the `GH_PAT`
   secret on the main repo. That account authors the tap commits and winget
   PRs and signs the Microsoft CLA once. **Pending** (the repo has no
   secrets today). Gates Phases 2 and 3.
5. Add a GitHub tag ruleset on `slng-ai/unmute` restricting `v*` tag
   creation to admins or a named maintainer team. **Pending.** Should exist
   before the first real tag; cheap to add now.

   *Correction, 2026-08-14:* "to admins" cannot work here. All 13
   collaborators hold the admin role, so a ruleset that exempts admins
   exempts everyone. Restrict to named people or a narrower team, or exempt
   nobody. The same finding shaped the `main` branch ruleset added that day
   (**main: pull requests only**, no bypass actors), which is a separate
   thing from this item: it governs branches, not tags.

## Verified facts carried into planning

Checked against goreleaser.com raw docs and microsoft/winget-pkgs on
2026-08-14. Field-level details MUST be re-verified against live docs during
planning (context7 or WebFetch); GoReleaser moves fast.

1. Formula-based brew publishing is deprecated; the current channel is the
   cask block (v2.10+). Casks are macOS only; Homebrew on Linux has no casks.
2. Unsigned macOS cask binaries hit Gatekeeper; the documented free fix is a
   post-install hook stripping the quarantine attribute.
3. winget flow: push branch to our fork, PR to upstream master. Required
   manifest fields: publisher, license, short description. Microsoft's
   pipeline downloads installers unauthenticated, so winget needs public
   downloads.
4. Paid/free boundaries that shape the config: the global metadata block is
   paid (set description/homepage/license inside each publisher block
   instead); global before-hooks are free only as plain strings; includes,
   template files, MSI/NSIS, cask app/DMG mode, and alternative names are all
   paid. None may be used.
5. Verifiable builds: proxy-mode builds verify through the public module
   proxy and checksum database. Needs: the main build target is a package,
   the module path publicly fetchable, tag releases only. VCS info is not
   embedded in the binary in this mode.
6. Reproducible builds: trim paths, commit timestamp as mod time, commit
   date in link flags, never a wall-clock template function.
7. CI action: the official GoReleaser action v7, free distribution, v2
   version range, full-history checkout, Go version from `go.mod`.
   Permissions: contents write, plus id-token write for keyless signing. The
   default token cannot write to other repos (hence FR-021).
8. Keyless signing and SBOM generation are free features, but the CI action
   installs neither tool; both need explicit installer steps.
9. Template variables (version, tag, project name, commit timestamp,
   snapshot flag, env) are available broadly, but git-derived fields are not
   available in the root env section.
10. Build output defaults to `./dist`; project name `unmute`.

Reference docs (all re-checkable): the GoReleaser customization pages for
homebrew_casks, winget, the Go builder, verifiable builds, hooks, project,
metadata, dist, templates, env, general hooks, git, and GitHub Actions, plus
`microsoft/winget-pkgs` `doc/Validation.md`.

## Assumptions

- The existing CI workflow stays; release validation is added to it or beside
  it rather than replacing it.
- The current `main.go` version variable pattern is kept and extended with
  commit and date variables (D9); the Makefile's local stamping stays as the
  dev-build path.
- Phase transitions are deliberate maintainer actions (edit a flag, push),
  not automatic detection of repo visibility.
- Test tags on the private repo (Phase 1) are allowed and cheap; they
  exercise User Story 1 before anything is public.
- The tap repo is org-owned and long-lived. The `GH_PAT` is a personal
  fine-grained token minted by the maintainer (clarified 2026-08-14); it is
  the only credential this feature adds, and swapping it for a bot account's
  token later changes a secret value, not the design.
- MIT copyright holder line reads the org (slng.ai); exact legal entity name
  is confirmed by the maintainer when the LICENSE lands.
- "Signed GitHub Release" means the keyless signature on the checksums file
  (D11); individual binaries are not signed in v1 (no Apple/Windows code
  signing, per D3 and out-of-scope).

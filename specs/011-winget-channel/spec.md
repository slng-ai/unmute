# Feature Specification: Windows Release Channels (winget + Scoop)

**Feature Branch**: `011-winget-channel`

**Created**: 2026-08-15

**Status**: Draft

**Input**: User description: "Release unmute to all Windows users via GoReleaser: evaluate npm publishing (GoReleaser Pro) vs winget vs scoop vs chocolatey, pick the safe/secure/stable channel set, and open the chosen channel(s) in the release pipeline"

## The story

Today a macOS or Linux user installs unmute with one brew command. A Windows
user has to find the release page, download a zip, unpack it, and put the
binary on their PATH by hand. That is not a channel, it is a chore, and it is
the last gap in "anyone can install this".

The question that started this feature was: is npm the easiest way to reach
everyone, instead of winget? The evaluation below answers it: no. npm only
exists on machines that have Node.js, and it is a paid GoReleaser feature.
The only install tool that ships preinstalled on Windows 10 and 11 is winget.

winget has one catch: Microsoft reviews every manifest by hand, so between
our release run and their merge, a Windows user reading our docs has nothing
to type. Scoop closes that gap. It works exactly like our Homebrew tap: a
manifest in a bucket repository we own, pushed by the release run, installable
the moment the run finishes, with nobody's approval between us and the user.

So this feature opens two channels: **Scoop**, documented as the Windows
install path that works today, and **winget**, opened in the same release but
documented as "coming soon" until Microsoft merges the first manifest PR.
The pipeline stays on the free tier. Direct zip download and `go install`
remain the fallbacks for everyone else.

## Clarifications

### Session 2026-08-15

- Q: Open npm as a second channel alongside winget? → A: No. npm is
  deferred: it is Pro-only, the bare name `unmute` is already taken on the
  registry, it would put a paid license key in the critical path of every
  release, and it can never satisfy "all Windows users" by itself because it
  requires Node.js. The evaluation stays recorded below so reopening the
  question later starts from facts, not from scratch.
- Q: Which Windows route ships, winget alone or with a second channel? → A:
  Both winget and Scoop, opened in the same release. Scoop is the install
  path the docs actively guide Windows users to, because it is live the
  moment the release run finishes. winget is documented as "coming soon"
  until Microsoft merges the first manifest PR, then documented as the
  primary path since its client is preinstalled on Windows.
- Q: Where do the Scoop bucket and the winget fork live? → A: Both under
  the `slng-ai` org, with the maintainer as admin on both. The org fork
  does not change the token facts: winget still needs a classic token
  (opening the upstream PR is out of reach for any fine-grained token), and
  the pre-flight on that token now also proves the org allows classic
  tokens at all.

## Channel evaluation

All claims verified against goreleaser.com and registry.npmjs.org on
2026-08-15. This section exists so nobody redoes this research.

| Channel | Who it reaches | GoReleaser tier | Publish gate | Verdict |
| --- | --- | --- | --- | --- |
| winget | Every Windows 10/11 user; the client is preinstalled | Open source (already configured, dormant) | Microsoft reviews each manifest PR by hand; hours to days; one-time CLA | **Open it.** The only channel that satisfies "all Windows users" |
| Scoop | Windows users who installed Scoop, and anyone willing to run its one-line installer | Open source | None: own bucket repo, exactly like our Homebrew tap | **Open it.** Live the instant the run finishes, fully under our control. Covers the gap while winget sits in review, and stays the fastest Windows path after |
| npm | Anyone with Node.js, on all three OSes | **Pro only** (paid license) | None; live the moment the run finishes | **Defer.** Reaches only Node.js users, the bare name `unmute` is taken (would need a scoped name), and adopting Pro puts a paid license key in the critical path of every release. Revisit on user demand |
| Chocolatey | Windows users who installed choco | Open source (archive format) | Human moderation per version, plus the choco client must exist on the CI runner, plus its own API key | **Skip.** Most friction, slowest review, and its audience is covered by winget and Scoop |
| MSI / NSIS installer | Users who want a GUI installer | Pro only | None | **Skip.** Wrong shape for a CLI; winget serves the same users better |
| Direct zip download | Everyone | Already shipping | None | **Keep.** The fallback when a user has no package manager at all (locked-down machines, older Windows) |

Facts that shaped the verdicts:

- winget's procedure was already specced and configured (dormant) in
  `specs/010-goreleaser-release-pipeline/` as Rollout Phase 3. Opening it is
  a one-line flip plus its preconditions, not new pipeline design.
- One correction learned since feature 010 was written: the winget PR is
  opened on `microsoft/winget-pkgs`, which no fine-grained token of ours can
  ever reach. A fine-grained token only reaches repositories its resource
  owner owns, and nobody here owns Microsoft's repo. So winget needs its own
  classic token with public-repo scope, stored as its own secret, instead of
  sharing the tap's fine-grained token. The 010 contract documents still say
  one token covers both channels and must be corrected as part of this
  feature.
- Scoop follows the Homebrew tap pattern one for one: a public bucket
  repository owned by the org, a manifest written and pushed by the release
  run, credentialed by a fine-grained token that reaches only org-owned
  repositories. No third party sits between a tag push and an installable
  package. Microsoft's review delay applies to every winget version update
  (the first, brand-new-package PR takes the slow path; later version bumps
  are usually faster), so Scoop is permanently the fastest Windows channel,
  not just a launch stopgap.
- On npm, for the record: publishing is Pro-only, all six of our platform
  pairs are supported, the install runs a script at install time (an install
  with `--ignore-scripts` is broken by design upstream), and the name
  `unmute` is owned by an unrelated web audio package, so ours would ship
  scoped. Pro pricing as of 2026-08-15: Personal $165/yr, Startup $247/yr.
  None of this is needed to reach Windows users.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A Windows user installs with the tool already on their machine (Priority: P1)

A Windows user hears about unmute. They open a terminal and run
`winget install slng.unmute`. No account, no extra package manager, no zip.
`unmute --version` prints the released version.

**Why this priority**: This is the goal of the feature. winget is the one
install tool present on a stock Windows 10/11 machine, so it is the only
channel that reaches all Windows users. Every other Windows channel assumes
the user installed something else first.

**Independent Test**: After the first stable tag with the channel open and
Microsoft's review merged, run the winget install on any Windows machine (or
confirm the published manifests exist upstream) and check the version output.

**Acceptance Scenarios**:

1. **Given** a stable release tag has been pushed and its run finished, **When** the run's log is read, **Then** it shows a manifest branch pushed and a pull request opened against Microsoft's package repository, with no manual step taken.
2. **Given** Microsoft has merged that pull request and the index has refreshed, **When** a Windows user runs the standard winget install command, **Then** unmute installs and `unmute --version` prints the tag's version, commit, and date.
3. **Given** a prerelease tag (for example `v0.2.0-rc.1`), **When** its run finishes, **Then** no winget manifest was pushed anywhere.

---

### User Story 2 - A Windows user installs via Scoop the moment we release (Priority: P2)

A Windows user reads our install docs on release day. The docs guide them to
Scoop: add our bucket, install unmute. It works immediately, because nothing
between our release run and their terminal needs anyone's approval. Users who
do not have Scoop get its standard one-line install first.

**Why this priority**: This is the only Windows path that is guaranteed
available the moment a release ships, and it stays the fastest one on every
later release, because winget versions always wait on Microsoft's review. It
is second only because it requires the user to have or install Scoop.

**Independent Test**: Push one stable tag with the channel open, then on any
Windows machine add the bucket and install; check the version output. No
waiting on any third party.

**Acceptance Scenarios**:

1. **Given** a stable release tag has been pushed and its run finished, **When** a Windows user adds our bucket and runs the Scoop install command from the docs, **Then** unmute installs immediately and `unmute --version` prints the tag's version, commit, and date.
2. **Given** the bucket repository, **When** the release run finishes, **Then** it contains exactly one updated manifest for the new version, committed by the run, and installable with no manual step by anyone.
3. **Given** a prerelease tag, **When** its run finishes, **Then** the bucket received nothing.

---

### User Story 3 - The maintainer releases exactly as before (Priority: P3)

The maintainer writes notes, tags, pushes. One run publishes everything:
GitHub release, Homebrew cask, Scoop manifest, winget PR. Nothing about the
existing channels changes, rehearsal still works before tagging, and a
problem in any new channel never damages an old one.

**Why this priority**: The pipeline's value is that a release is one action.
Two new channels must not erode that, and must not make releases more
fragile than they are today.

**Independent Test**: Rehearse with a dry run before the tag, then push one
stable tag and confirm the GitHub release, the cask, the Scoop manifest, and
the winget PR all came from the single run, with brew install behaving
exactly as the previous release.

**Acceptance Scenarios**:

1. **Given** the channel flips have landed on main, **When** the maintainer pushes one stable tag, **Then** one automated run produces the GitHub release, the cask update, the Scoop manifest, and the winget PR, with no human action after the push.
2. **Given** the winget or Scoop step fails for any reason (bad token, upstream hiccup), **When** the run finishes, **Then** the GitHub release, checksums, signature, SBOMs, and Homebrew cask still shipped, unharmed.
3. **Given** the release configuration changed in a pull request, **When** CI runs on that PR, **Then** the config is validated and all six platform builds are rehearsed with no secret needed, exactly as today.
4. **Given** the same tag is built twice, **Then** the binaries are byte-identical, exactly as today.

---

### Edge Cases

- **The winget step fails silently green.** The winget publisher never turns
  a release run red, by design, so a green run proves nothing about winget.
  The run log must be read after each release until the channel has a track
  record; the runbook says how.
- **Microsoft rejects or sits on a manifest PR.** The release is already out
  on Scoop and every other channel, so no Windows user is blocked. Follow-up
  happens by hand on the PR.
- **The first winget PR triggers the CLA bot.** One-time: the account that
  owns the winget token replies with the command the bot's comment shows.
  Nothing moves upstream until then.
- **The wrong kind of token for winget.** A fine-grained token cannot open
  the upstream PR no matter how it is scoped. The failure text names the
  resource, not the cause, so the runbook and the contract docs must both
  say: classic token, public-repo scope, nothing else.
- **The Scoop bucket does not exist or the token cannot write to it.** The
  same failure shape the tap had on v0.1.2: a correctly scoped token still
  collapses to read-only if the owning account has no push access to the
  bucket. The pre-flight check from the runbook applies to the bucket too,
  before the first tag.
- **A channel token lapses.** Must not block any other channel (scenario
  3.2). Like the tap token, every channel credential gets a real expiry and
  a calendar reminder.
- **Docs promise winget too early.** The docs say "coming soon" for winget
  until Microsoft merges the first manifest PR, and only then flip to
  presenting winget as the primary Windows path. Scoop is the guided path in
  the meantime. A user must never be given a command that cannot work yet.
- **Version skew between Windows channels.** Scoop has the new version
  minutes after the tag; winget lags by Microsoft's review on every release.
  The docs must not claim the two are in step.
- **An rc tag.** Becomes a GitHub prerelease; Scoop and winget both skip it,
  matching the existing Homebrew behavior.
- **Windows antivirus flags the binary.** Known false-positive pattern for
  unsigned Go binaries; the checksums and signature bundle are the answer.
  Code signing certificates stay out of scope.
- **A user on Windows with no package manager at all** (locked-down machine,
  older Windows). The zip download stays documented as the fallback.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: On every stable release tag, the pipeline MUST publish winget
  manifests and open the upstream pull request automatically, with no manual
  step in our pipeline. Microsoft's review is outside our pipeline and gates
  only the winget listing, nothing else.
- **FR-002**: On every stable release tag, the pipeline MUST write the
  updated Scoop manifest to the org-owned bucket repository automatically,
  so the new version is installable the moment the run finishes.
- **FR-003**: Prerelease tags MUST NOT publish to winget or Scoop, matching
  the existing Homebrew behavior.
- **FR-004**: A failure in any one publish channel MUST NOT prevent or
  damage the GitHub release, its checksums, its signature, its SBOMs, or any
  other channel.
- **FR-005**: The pipeline MUST stay on the free GoReleaser tier. No paid
  feature enters the config as part of this feature.
- **FR-006**: Every channel credential MUST exist only as a named CI secret
  with a real expiry and a calendar reminder. The winget credential is its
  own secret, separate from the credential that writes the tap and the
  bucket. No value ever appears in a file, a log, or a chat. Only names
  appear in the repository.
- **FR-007**: Release rehearsal MUST keep working before every tag with no
  secret present, and config validation MUST keep running on every pull
  request, exactly as today.
- **FR-008**: The version output contract is unchanged: an install from any
  channel prints `unmute version X.Y.Z (<commit> <date>)`.
- **FR-009**: The installation docs and the README MUST, in the same change
  that opens the channels: guide Windows users to the Scoop install as the
  path that works now, and mark winget as "coming soon". After Microsoft
  merges the first manifest PR, a separate small change MUST flip the docs
  to present winget as the primary Windows path, keeping Scoop documented as
  the fastest one.
- **FR-010**: The 010 contract documents that still say one token covers
  both the tap and winget MUST be corrected to the real credential layout in
  the same change that opens the channels.
- **FR-011**: The channel evaluation and its verification dates (this spec)
  MUST stay the recorded reason npm, chocolatey, and installer formats were
  skipped, so the research is not redone.

### Key Entities

- **Release channel**: a way a user installs unmute. After this feature:
  GitHub release download (all), Homebrew tap (macOS), `go install` (Go
  users), Scoop (Windows, instant), winget (Windows, after review).
- **Scoop bucket**: a public org-owned repository holding the Scoop
  manifest, written only by release runs. The Windows twin of the Homebrew
  tap. Default name: `slng-ai/scoop-bucket`.
- **winget token**: classic token with public-repo scope, owned by the
  maintainer, stored as its own CI secret. Pushes manifest branches to the
  org's fork of Microsoft's package repository and opens the upstream PR.
  Deliberately separate from the fine-grained token that writes the tap and
  the bucket.
- **Manifest fork**: the org's fork of Microsoft's package repository, with
  the maintainer as admin. Never maintained by hand; the pipeline syncs it
  before each push.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: On release day, a Windows user following the docs installs
  unmute with the guided commands and sees the released version from
  `unmute --version`, with zero waiting on any third party.
- **SC-002**: Once the first winget manifest is merged, a user on a stock
  Windows 10 or 11 machine, with no developer tooling installed, installs
  unmute with one command.
- **SC-003**: Releasing is still one action. The maintainer pushes one tag
  and takes zero further steps inside our pipeline; every open channel
  publishes from that single run.
- **SC-004**: The first stable release after the change reaches five install
  paths from one tag push (direct download, Homebrew, `go install`, Scoop,
  and winget once Microsoft merges), and the pre-existing channels behave
  identically to the previous release.
- **SC-005**: A prerelease tag reaches zero package managers while still
  producing a marked prerelease on the release page.
- **SC-006**: Two runs of the same tag produce byte-identical binaries, and
  every published archive remains covered by the signed checksums file.
- **SC-007**: The release pipeline's running cost stays zero: no paid
  license, no new paid service.

## Assumptions

- The Scoop bucket repository is created under the org before the flip, and
  the account minting its credential can push to it, verified with the same
  pre-flight the runbook already prescribes for the tap. Whether the bucket
  is covered by extending the existing tap token or by a sibling token is a
  plan-level choice; either way it is fine-grained and org-repo-only.
- winget preconditions, amended from Rollout Phase 3 by the clarification
  above: a fork of Microsoft's package repository under the `slng-ai` org
  with the maintainer as admin, a classic token with public-repo scope
  stored as its own secret (pre-flighted against the org fork, which also
  proves the org's classic-token policy allows it), and a one-time CLA
  answer from the token's account on the first PR.
- Both channels flip in the same change and ship on the same tag; Scoop is
  simply usable first because nothing external gates it.
- Re-run safety (`replace_existing_artifacts`) is already merged to main
  (PR #77, verified in git history 2026-08-15), so it rides the next tag.
- The repo is public and the Homebrew channel is open and verified (Phase 2,
  confirmed 2026-08-15), so the "real, public, stable release" precondition
  is met for both new channels.
- The direct zip download remains the documented fallback for Windows users
  without any package manager and for offline or locked-down environments.
- Nothing about the six built platforms changes; both new channels consume
  the Windows zip archives the pipeline already produces.
- Microsoft's review timeline (days for the first, brand-new-package PR;
  usually faster for later version bumps) is accepted; nothing on our side
  waits for it.

## Out of scope

- npm publishing. Deferred, not rejected forever. Reopening it later means:
  a GoReleaser Pro license (a paid key in every release run), a scoped
  package name because `unmute` is taken, and an npm publish credential. The
  evaluation above is the starting point.
- Chocolatey and MSI/NSIS installers (rejected above, with reasons).
- Windows or macOS code signing certificates.
- Any change to what is built: platforms, archive formats, checksums,
  signing, and SBOMs all stay exactly as shipped by feature 010.

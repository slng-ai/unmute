# Research: Windows Release Channels (winget + Scoop)

All upstream claims verified against goreleaser.com on 2026-08-15 unless
another date is given. The channel-set evaluation itself (why Scoop and
winget, why not npm, chocolatey, or installers) lives in
[spec.md](spec.md) under "Channel evaluation" and is not repeated here.

## D1: Scoop config shape

**Decision**: one entry under the v2 `scoops:` key, mirroring the cask:

- `name: unmute`, `skip_upload: "auto"`, `homepage`, `description`, and
  `license: MIT` set explicitly, matching the values the winget block
  already carries.
- `repository`: `slng-ai/scoop-bucket`, token `{{ .Env.GH_PAT }}`, direct
  push (no `pull_request` block).
- Everything else on defaults: `use: archive` picks up our Windows zips
  (Scoop requires zip, which we already ship for winget), the manifest lands
  in the bucket root (required: `scoop bucket list` shows 0 manifests if
  they are not in root), the download URL template defaults to our GitHub
  release assets, and both Windows architectures (amd64, arm64) come from
  the existing archives.

**Rationale**: the smallest block that works; every non-default value is one
we already publish elsewhere. `"auto"` skips prereleases (FR-003), same as
the cask.

**Alternatives considered**: PR mode into the bucket (pointless ceremony on
a repo only release runs write to, and publish errors in PR mode log instead
of failing, which hides breakage); a subdirectory for manifests (breaks
`scoop bucket list`).

## D2: The bucket repo

**Decision**: `slng-ai/scoop-bucket`, public, created once by hand with a
README (`gh repo create slng-ai/scoop-bucket --public --add-readme`).
Nothing but release runs ever writes a manifest to it.

**Rationale**: exact twin of `slng-ai/homebrew-tap`, which is proven. Public
because an install is a raw download. The README seeds the default branch so
the first automated push has something to commit onto.

**Alternatives considered**: hosting the manifest in the main repo (couples
release history to source history and makes the bucket URL ugly); a personal
bucket (org assets belong to the org; the tap set the precedent).

## D3: Scoop credential

**Decision**: extend the existing `GH_PAT` fine-grained token by adding
`slng-ai/scoop-bucket` to its "Only select repositories" list. No new
secret. The scoops block references `{{ .Env.GH_PAT }}`.

**Rationale**: identical trust shape to the tap: maintainer-owned,
fine-grained, contents read/write, org repos only. The repo list of a
fine-grained token is editable in place (confirmed on the real token,
2026-08-14, per the release runbook), so this is a console edit plus the
standard pre-flight, not a re-mint. One token, one expiry to track.

**Alternatives considered**: a sibling fine-grained token just for the
bucket (a second expiry and secret for zero extra isolation: same owner,
same permission, same org); reusing `GITHUB_TOKEN` (cannot write to another
repo, the reason `GH_PAT` exists at all).

**Trap to pre-flight**: a fine-grained token is the intersection of its
grants and its owner's access. The token's owner must have push on the
bucket before the token works, exactly the failure the tap hit on v0.1.2.
Quickstart step 1 carries the check.

## D4: winget flip values

**Decision**: exactly the three edits feature 010 staged, with the
correction learned since: `skip_upload: true` becomes `"auto"`;
`repository.owner: FORK_OWNER` becomes `slng-ai`; `repository.token`
becomes `{{ .Env.GH_PAT_WINGET }}`. The fork is created with
`gh repo fork microsoft/winget-pkgs --org slng-ai --clone=false
--default-branch-only`, per the maintainer's decision (clarification,
2026-08-15) that org assets live in the org, with the maintainer as admin.

**Rationale**: the PR is opened on `microsoft/winget-pkgs`, which no
fine-grained token of ours can ever reach, because a fine-grained token only
reaches repos its resource owner owns. GitHub's answer is a classic token,
and a classic token does not care whether the fork is personal or org-owned.
The release runbook's default was a personal fork, to dodge two org-side
risks; both are handled instead of dodged: the grant-intersection trap is
closed because the maintainer is admin on the org fork, and the org's
classic-token policy (an org can block classic tokens outright) is proven by
the token pre-flight in D5 before anything ships.

**Alternatives considered**: widening `GH_PAT` (impossible, see above); a
personal fork (the runbook default; rejected by the maintainer so the fork
lives with the org's other release assets); waiting for winget until Scoop
has a track record (rejected: the spec ships both in one change, and
winget's review delay is exactly why shipping it early matters).

## D5: The new secret

**Decision**: `GH_PAT_WINGET`, a classic token with the `public_repo` scope
only, a real expiration date, and a calendar reminder. Stored with
`gh secret set GH_PAT_WINGET --repo slng-ai/unmute`. Added to the goreleaser
step's `env` in `release.yml` next to `GH_PAT`.

**Rationale**: `public_repo` is the minimum classic scope that can push the
manifest branch and open the upstream PR. It is broad (it reaches every
public repo the account can push to), which is exactly why it is a separate
secret instead of replacing the tightly scoped `GH_PAT`.

**Pre-flight**: `GH_TOKEN=$WPAT gh api repos/slng-ai/winget-pkgs --jq
'.permissions.push'` must print `true`. This one check proves two things:
the token can push the manifest branch, and the org's policy allows classic
tokens to reach org repos at all (if it prints `false` or 404s, an org owner
must allow classic tokens under Organization Settings, Third-party Access,
Personal access tokens). The PR-opening half cannot be tested without
opening a real PR; that is as far as a pre-flight goes.

## D6: Docs states

**Decision**: two docs carry the install story
(`docs-site/start/installation.mdx` and `README.md`), and they move through
two states:

- **State A (ships with the flip)**: Scoop added as the guided Windows path
  with its two commands (add the bucket, install). The winget row stays but
  is marked "coming soon", and the existing Note explains why: manifests are
  submitted on release, Microsoft reviews them, the command works after
  their merge. A user is never shown a command that cannot work yet.
- **State B (separate small PR, after Microsoft merges the first manifest
  PR)**: winget presented as the primary Windows path (client preinstalled),
  Scoop kept and described as the fastest (no review lag on any release),
  "coming soon" language deleted.

**Rationale**: FR-009 verbatim. Matches the Phase 2 precedent: the
private-repo note was deleted only when it became false.

## D7: Failure and verification posture

**Decision**: no new mechanism; adopt and document the existing ones.

- The GitHub release, checksums, signature, and SBOMs publish before the
  channel publishers run, so a channel failure loses nothing (FR-004).
- Scoop direct push fails loudly on a bad token, like the cask did on
  v0.1.2 (the log-only caveat in the Scoop docs applies to PR mode, which D1
  rejects partly for that reason).
- winget never turns the run red, by upstream design. Compensating control:
  grep the run log for `winget` after every tag until the channel has a
  track record (quickstart step 5); the first PR additionally needs the
  one-time CLA reply.
- Re-run safety: `replace_existing_artifacts: true` is on `main` (PR #77,
  merged, verified in git history 2026-08-15), so a failed run re-runs
  cleanly on the next tag's config.

## D8: Version and sequencing

**Decision**: the flip PR carries config, workflow env, docs state A, and
the 010 contract corrections in one squash-merged change. The next tag is
`v0.1.3` (pipeline-only change bumps patch, per the release policy).
External preconditions (bucket repo, token edit, fork, new secret,
pre-flights) happen before the PR merges so the tag can follow immediately.
The gitignored release runbook is updated to match after the tag proves the
channels (it is a working note, not a tracked artifact; the tracked truth is
this spec and the corrected 010 contracts).

**Rationale**: one tag proves everything (SC-004); nothing in the PR is
usable until the preconditions exist, so they go first.

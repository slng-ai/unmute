# Contract: Windows Channel Configuration

What the flip PR must contain, exactly. When code and this contract
disagree, this contract wins (constitution IV). It extends, and in two
places corrects, feature 010's contracts.

## 1. `.goreleaser.yaml`

### New `scoops:` block (free tier)

```yaml
scoops:
  - name: unmute
    # Same meaning as the cask: stable releases publish, prereleases skip.
    skip_upload: "auto"
    repository:
      owner: slng-ai
      name: scoop-bucket # manifest in the root, or `scoop bucket list` shows nothing
      token: "{{ .Env.GH_PAT }}"
    homepage: https://github.com/slng-ai/unmute
    description: Compile a declarative voice-agent package to Pipecat or LiveKit
    license: MIT
```

Everything else stays on defaults on purpose: `use: archive` consumes the
existing Windows zips, the URL template points at our release assets, and
both Windows architectures come from the existing archives. No other block
in the file changes for Scoop.

### winget block: exactly three line edits

| Line today | Becomes |
| --- | --- |
| `skip_upload: true # Rollout Phase 3: change to "auto"` | `skip_upload: "auto"` |
| `owner: FORK_OWNER # Rollout Phase 3: ...` | `owner: slng-ai` (the org owns the fork; the maintainer is admin on it) |
| `token: "{{ .Env.GH_PAT }}"` (winget block only) | `token: "{{ .Env.GH_PAT_WINGET }}"` |

Nothing else in the winget block changes. The cask's token line is not
touched.

### Invariants

- No key from a paid tier appears anywhere in the file (`goreleaser check`
  with the OSS binary must still exit 0).
- `builds`, `archives`, `checksum`, `sboms`, `signs`, `release`, and
  `homebrew_casks` are byte-identical to before this PR.

## 2. `.github/workflows/release.yml`

One added line in the goreleaser step's `env`, next to `GH_PAT`:

```yaml
GH_PAT_WINGET: ${{ secrets.GH_PAT_WINGET }}
```

Nothing else changes. Snapshot and dry runs still need no secret: snapshot
mode never evaluates `{{ .Env.* }}` templates (FR-007).

## 3. Docs, state A (same PR)

Both `docs-site/start/installation.mdx` and `README.md`:

- Add Scoop with its two commands:

  ```
  scoop bucket add slng-ai https://github.com/slng-ai/scoop-bucket
  scoop install slng-ai/unmute
  ```

- Keep the winget row, marked "coming soon": manifests are submitted
  automatically on each release and the command works once Microsoft merges
  the first one. The current "channel is still closed" wording is replaced;
  a reader must be able to tell Scoop works now and winget does not yet.

Two comment corrections ride along because they become false in this PR:
`docs/REPO_MAP.md`'s pipeline output list gains Scoop, and the two
`release.yml` comments that say the run writes "the winget fork" with
`GH_PAT` are corrected to the two-token reality.

State B (winget primary, "coming soon" deleted) is a separate PR after the
first upstream merge and is out of this contract.

## 4. Corrections to feature 010's contract docs (same PR)

These currently state that one token covers the tap and the winget fork.
That is false (a fine-grained token cannot reach `microsoft/winget-pkgs`),
so:

- `specs/010-goreleaser-release-pipeline/contracts/release-workflow.md`:
  the Env row lists `GH_PAT` (tap and Scoop bucket) and `GH_PAT_WINGET`
  (winget fork and upstream PR); runbook item 4 describes both tokens.
- `specs/010-goreleaser-release-pipeline/data-model.md`: the `GH_PAT` row
  drops "tap + fork" and "the only added credential"; a `GH_PAT_WINGET` row
  is added; the Phase 3 row names the fork owner and the new secret.
- `specs/010-goreleaser-release-pipeline/quickstart.md`: the Phase 3
  preconditions add `GH_PAT_WINGET` set and pre-flighted.

## 5. Preconditions (exist before the PR merges; nothing in the repo)

| Precondition | Proof |
| --- | --- |
| `slng-ai/scoop-bucket` exists, public, non-empty | `gh api repos/slng-ai/scoop-bucket --jq .visibility` → `public` |
| Token owner can push to the bucket | `gh api repos/slng-ai/scoop-bucket --jq .permissions.push` → `true` (own credentials) |
| `GH_PAT` repo list includes the bucket | `GH_TOKEN=$PAT gh api repos/slng-ai/scoop-bucket --jq .permissions.push` → `true` |
| Fork of `microsoft/winget-pkgs` under the org | `gh api repos/slng-ai/winget-pkgs --jq .fork` → `true` |
| Maintainer is admin on the fork | `gh api repos/slng-ai/winget-pkgs --jq .permissions.admin` → `true` (own credentials) |
| `GH_PAT_WINGET` minted (classic, `public_repo`) and stored | `gh secret list --repo slng-ai/unmute` shows both names |
| New token can push to the fork, and the org allows classic tokens | `GH_TOKEN=$WPAT gh api repos/slng-ai/winget-pkgs --jq .permissions.push` → `true` |

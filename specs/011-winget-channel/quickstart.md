# Quickstart: prove the Windows channels work

Runnable checks, in order, from before the PR to after Microsoft merges.
Config shapes live in [contracts/channels.md](contracts/channels.md); values
in [data-model.md](data-model.md).

## 1. Pre-flight (before the flip PR merges)

Run the six proofs in the contract's Preconditions table. All must pass.
The two token checks matter most: a fine-grained token silently collapses to
read-only when its owner lacks push access, which is what broke the cask on
v0.1.2.

## 2. Rehearse the config (on the PR branch)

```bash
goreleaser check        # exit 0, still on the OSS binary
make release-dry        # all 6 platforms build, no secret needed
```

Then read what GoReleaser resolved, not what the YAML looks like:

```bash
python3 - <<'EOF'
import json
arts = json.load(open("dist/artifacts.json"))
for a in arts:
    t = a.get("type", "")
    if "scoop" in t.lower() or "winget" in t.lower():
        cfg = next((v for v in a.get("extra", {}).values()
                    if isinstance(v, dict) and "skip_upload" in v), {})
        print(t, "->", cfg.get("skip_upload"))
EOF
# every printed line must end in 'auto'. Read dist/ to see the generated
# unmute.json (scoop) and winget manifests before anything publishes.
```

The `release-config` CI job runs the same dry run on the PR with no secrets,
proving FR-007.

## 3. Prove the flips are on main, before tagging

The v0.1.1 lesson: a green merge is not evidence the flip landed.

```bash
git show origin/main:.goreleaser.yaml | grep -A3 '^scoops:' | grep skip_upload   # "auto"
git show origin/main:.goreleaser.yaml | sed -n '/^winget:/,$p' | grep -E 'skip_upload|owner:|GH_PAT_WINGET'
# expect: skip_upload: "auto", the real fork owner, the GH_PAT_WINGET template
git show origin/main:.github/workflows/release.yml | grep GH_PAT_WINGET          # the env line
```

## 4. Tag and release

Normal release loop, notes and all. Pipeline-only change, so the next patch
version:

```bash
git tag -a v0.1.3 --cleanup=verbatim -F /tmp/v0.1.3.md
git tag -l --format='%(contents)' v0.1.3
git push origin v0.1.3
gh run watch "$(gh run list --workflow=Release --limit 1 --json databaseId --jq '.[0].databaseId')"
```

## 5. Read the log even if the run is green

winget never turns a run red; Scoop direct push does, but read both:

```bash
gh run view "$(gh run list --workflow=Release --limit 1 --json databaseId --jq '.[0].databaseId')" \
  --log | grep -iE "scoop|winget"
# scoop: a commit pushed to slng-ai/scoop-bucket, no error after it
# winget: a manifest branch pushed and a PR URL on microsoft/winget-pkgs
# "skip_upload is set" on either = the flip is not on the tagged commit
# "Resource not accessible" on winget = wrong token kind (must be classic)
```

## 6. Verify Scoop like a stranger (SC-001, same day)

```bash
gh api repos/slng-ai/scoop-bucket/contents/unmute.json --jq '.name, .size'
```

On any Windows machine:

```powershell
scoop bucket add slng-ai https://github.com/slng-ai/scoop-bucket
scoop install slng-ai/unmute
unmute --version    # unmute version 0.1.3 (<sha> <date>)
```

No Windows machine handy: the bucket file check above plus a download of the
URL inside `unmute.json` is the remote equivalent.

## 7. The Microsoft side (SC-002, days later)

- First PR only: answer the CLA bot from the token's account.
- Watch the PR until merged; fix validation flags by hand on the PR.
- After merge and index refresh, on Windows:

```powershell
winget source update
winget install slng.unmute
unmute --version
```

No Windows machine: confirm manifests exist at
`https://github.com/microsoft/winget-pkgs/tree/master/manifests/s/slng/unmute`.

## 8. Docs state B (separate small PR, only now)

Flip the docs: winget becomes the primary Windows path, Scoop stays as the
fastest, every "coming soon" is deleted. Also update the gitignored release
runbook to match reality (channels open, PR #77 note gone).

## 9. Prerelease guard (SC-005, first rc that comes up)

On the next `-rc` tag: the release page shows a prerelease, the bucket has
no new commit, the fork has no new branch, the tap has no new commit.

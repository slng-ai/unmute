# Research: GoReleaser Release Pipeline

**Date**: 2026-08-14 | **Feature**: [spec.md](spec.md)

Every claim below was re-verified against the live official docs on
2026-08-14 by five parallel research passes (goreleaser.com pages, the
goreleaser-action README, and, where the docs site had gaps, the GoReleaser
source on `main`). The old `www/docs/...` raw paths are dead; the site now
serves from `www/content/...`. Where a finding changed something the spec
assumed, it is called out as **Correction**.

## R1. Config file shape

- **Decision**: one `.goreleaser.yaml` at repo root, starting with
  `version: 2` and `project_name: unmute`.
- **Rationale**: `version: 2` is required (missing header is a warning,
  wrong version is a hard error). `project_name` is optional but pins the
  name instead of inferring it from the release.
- **Alternatives**: `.config/goreleaser.yaml` (higher precedence, no benefit
  here).

## R2. Build block

- **Decision**: one build entry: `env: [CGO_ENABLED=0]`,
  `goos: [darwin, linux, windows]`, `goarch: [amd64, arm64]`, `main: .`,
  `binary: unmute`, `flags: [-trimpath]`,
  `mod_timestamp: "{{ .CommitTimestamp }}"`, and
  `ldflags: -s -w -X main.version={{.Version}} -X main.commit={{.Commit}} -X main.date={{.CommitDate}}`.
- **Rationale**: the default `goarch` includes `386`, so it MUST be pinned to
  get exactly the 6 pairs. The ldflags follow GoReleaser's own default
  variable names (`main.version`, `main.commit`, `main.date`), swapping
  `{{.Date}}` for `{{.CommitDate}}` per the documented reproducible-builds
  guidance (commit date, commit timestamp as mod time, `-trimpath`, no
  `time` template function anywhere).
- **Alternatives**: keeping only `main.version` (rejected by D9: the spec
  wants commit and date in `--version`); GoReleaser's default ldflags
  as-is (rejected: `{{.Date}}` breaks reproducibility, `builtBy` is noise).

## R3. Version wiring in Go and Makefile

- **Decision**: `main.go` gains `commit` and `date` vars next to the
  existing `version` var and composes one string passed to `cli.Execute`
  (bare version when commit is empty, so dev builds stay clean). The
  Makefile stamps all three the same way (`git rev-parse --short HEAD`,
  commit date) so local builds and released builds agree.
- **Rationale**: `cli.Execute(version string)` already exists; composing in
  `main.go` means zero change to `internal/cli`. Using GoReleaser's default
  var names keeps the config minimal.
- **Duplication note (Constitution III)**: the stamping recipe now lives in
  two places, Makefile and `.goreleaser.yaml`. Both use the identical
  `-X main.*` names; the version-output contract
  ([contracts/version-output.md](contracts/version-output.md)) states the
  agreement, and the quickstart checks both paths print the same shape.

## R4. Verifiable builds (Phase 2)

- **Decision**: `gomod: {proxy: true}` ships in the config **commented
  out**, flipped on at Phase 2.
- **Rationale**: proxy mode builds through proxy.golang.org and verifies
  against sum.golang.org, which fails while the module is private. Docs
  confirm: tag builds only, snapshots ignore the setting, `build.main` must
  be a package (ours is `.`), and VCS info is not embedded (our own
  `-X main.commit` still works, it is passed at link time).
- **Alternatives**: `proxy: true` from day one (rejected: every Phase 1
  test tag would fail on the private module).

## R5. Archives, checksums, snapshot

- **Decision**: `archives` uses the **current key names**: `formats:
  [tar.gz]` with `format_overrides: [{goos: windows, formats: [zip]}]`.
  Default archive name template and default `files` (which auto-includes
  `LICENSE*` and `README*` in every archive). Checksums and snapshot
  version stay on defaults.
- **Rationale**: two v2.x renames matter for a fresh config: archive
  `format` → `formats` (v2.6) and `builds` → `ids` (v2.8). The default
  checksum file is `unmute_<version>_checksums.txt` (sha256). The snapshot
  key is `snapshot.version_template` (not `name_template`); the default
  `{{ .Version }}-SNAPSHOT-{{.ShortCommit}}` is fine, so the block is
  omitted.
- **Alternatives**: custom name templates (no need, defaults are the
  well-known shape).

## R6. Release block

- **Decision**: explicit `release: {github: {owner: slng-ai, name: unmute},
  prerelease: auto}`.
- **Rationale**: explicit owner/name avoids inference from the git remote
  (local remotes still point at the pre-rename URL). `prerelease: auto`
  marks rc/beta style tags as GitHub prereleases, which the spec's edge
  case requires.
- **Alternatives**: inferred repo (fragile during the rename window).

## R7. Changelog

- **Decision**: `changelog` with `use: git` (default), `sort: asc`, groups
  Features / Bug fixes / Others, and `filters.exclude` for docs, style,
  test, chore.
- **Rationale**: all `use` values are free. `use: git` works identically in
  snapshot, local, and tag runs and needs no token. With `use: git` each
  line is `<short-sha> <subject>`, so every regexp needs the documented
  `^.*?` prefix. The docs' own grouping example uses `bug` where
  conventional commits say `fix`; ours uses `fix`. Group regexes:
  `'^.*?feat(\([[:word:]]+\))??!?:.+$'` (order 0),
  `'^.*?fix(\([[:word:]]+\))??!?:.+$'` (order 1), catch-all Others
  (order 999). Excludes use the same shape for `docs|style|test|chore`.
- **Pro fence**: `paths`, `title`, nested subgroups, `divider`, and the
  whole `ai` section are Pro. Not used.

## R8. Signing (cosign keyless)

- **Decision**: the current documented bundle flow, verbatim shape:

  ```yaml
  signs:
    - cmd: cosign
      signature: "${artifact}.sigstore.json"
      args: ["sign-blob", "--bundle=${signature}", "${artifact}", "--yes"]
      artifacts: checksum
  ```

- **Correction**: the docs no longer use `COSIGN_EXPERIMENTAL` or separate
  certificate + signature files. One `checksums.txt.sigstore.json` bundle
  is produced and uploaded. Verify with
  `cosign verify-blob --bundle <file>.sigstore.json <file>`.
- **CI needs**: cosign is NOT installed by the GoReleaser action; add
  `sigstore/cosign-installer@v3`. Keyless needs `id-token: write` in the
  workflow (this OIDC requirement is documented by cosign, not on the
  GoReleaser sign page).

## R9. SBOMs (syft)

- **Decision**: `sboms: [{artifacts: archive}]`, all other keys default.
- **Rationale**: defaults produce one `<archive>.sbom.json` (SPDX JSON) per
  archive. Syft is NOT installed by the action; current GoReleaser docs
  bless no installer step, so we choose Anchore's own
  `anchore/sbom-action/download-syft@v0` (their official action; the
  original brief's suggestion still stands, it is just no longer quoted in
  GoReleaser's docs).

## R10. Homebrew cask

- **Decision**:

  ```yaml
  homebrew_casks:
    - name: unmute
      repository:
        owner: slng-ai
        name: homebrew-tap
        token: "{{ .Env.GH_PAT }}"
      homepage: https://github.com/slng-ai/unmute
      description: <one line>
      skip_upload: true   # Phase 2: change to "auto"
      hooks:
        post:
          install: |
            if OS.mac?
              system_command "/usr/bin/xattr", args: ["-dr", "com.apple.quarantine", "#{staged_path}/unmute"]
            end
  ```

- **Facts verified**: `homebrew_casks` exists since v2.10; the `hooks`
  field (the quarantine strip) **requires v2.13+**. Old `brews` is
  deprecated (soft v2.10, hard v2.16). Direct commit to the tap branch is
  the default (no `pull_request` block). `skip_upload: "auto"` skips
  prerelease tags. `Casks` is the default directory. Arch selection
  (amd64/arm64) is automatic from the archives; no config surface for it.
  `binaries` defaults to the cask name, which equals our binary name.
- **Correction (spec FR-017)**: casks have **no `license` field** at all.
  The license claim lives in the winget manifest and the LICENSE file
  (which the archives auto-include). FR-017 amended accordingly.
- **Pro fence**: `alternative_names`, `app`/DMG mode,
  `repository.token_type`, `pull_request.check_boxes`, versioned casks.
  Not used. `pull_request.token` is v2.18-unreleased; not used.
- **Tap note**: no formula existed before, so no `tap_migrations.json` is
  needed.

## R11. winget

- **Decision**:

  ```yaml
  winget:
    - publisher: slng
      package_identifier: slng.unmute
      short_description: <one line>
      license: MIT
      publisher_url: https://slng.ai
      publisher_support_url: https://github.com/slng-ai/unmute/issues
      homepage: https://github.com/slng-ai/unmute
      skip_upload: true   # Phase 3: change to "auto"
      repository:
        owner: FORK_OWNER   # decided at Phase 3 (maintainer account or org)
        name: winget-pkgs
        token: "{{ .Env.GH_PAT }}"
        pull_request:
          enabled: true
          base: {owner: microsoft, name: winget-pkgs, branch: master}
  ```

- **Facts verified**: `publisher`, `short_description`, `license` are the
  three required fields. The cross-repo PR flow is documented exactly as
  planned: GoReleaser syncs the fork with upstream, pushes a branch
  (default `{{ .ProjectName }}-{{ .Version }}` since v2.17), opens the PR
  against the base. Manifests land at `manifests/s/slng/unmute/<version>`.
  Default installer type covers archives; for winget the Windows archive
  must be zip (ours is). GoReleaser auto-uses upstream's
  `PULL_REQUEST_TEMPLATE.md` as the PR body.
- **Behavior note**: a failed PR "will log it to the release output, but
  will not fail the pipeline". That matches the spec's edge case (a winget
  rejection never blocks the release) and is a documented deviation from
  fail-loud, accepted in the spec.
- **Pro fence**: `use: msi`, `use: nsis`, `product_code`,
  `repository.token_type`, `pull_request.check_boxes`. Not used.

## R12. GitHub Actions

- **Decision**: `release.yml` on `push: tags: [v*]` with permissions
  `contents: write` + `id-token: write`, steps: checkout (`fetch-depth: 0`,
  required for the changelog), setup-go (`go-version-file: go.mod`),
  `sigstore/cosign-installer@v3`, `anchore/sbom-action/download-syft@v0`,
  then `goreleaser/goreleaser-action@v7` with `distribution: goreleaser`,
  `version: "~> v2"`, `args: release --clean`, env
  `GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}` and
  `GH_PAT: ${{ secrets.GH_PAT }}`.
- **Facts verified**: action major is v7; inputs are distribution /
  version / args / workdir / install-only. The action installs no other
  tools by design. The docs confirm the default token cannot write to
  other repos; a PAT secret is the documented pattern for taps and forks.
  The official examples pin checkout@v6/setup-go@v6 (README) vs @v7 (docs
  site); we follow the repo's existing pins and dependabot-style bumps.
- **PR validation job** (added to `ci.yml`): checkout with full history,
  setup-go, download-syft, action with `install-only: true`, then
  `goreleaser check` and `goreleaser release --snapshot --clean
  --skip=sign`, plus a shell assertion that `dist/` holds the 6 archives
  and that the built linux binary prints the snapshot version.
- **Rationale for `--skip=sign` in PRs and local dry runs**: `--snapshot`
  already implies `--skip=announce,publish,validate` (verified from
  source; `/cmd/` reference pages are gone from the site). Signing is
  tag-release-only anyway, and keyless signing in PRs would need OIDC and
  an interactive browser locally. `--skip=sign` is a valid OSS skip value
  (verified in `internal/skips/skips.go`).
- **`goreleaser check`** exits 1 on invalid config and **exits 2 on valid
  config that uses deprecated properties**, so CI goes red the day a field
  we use gets deprecated. `goreleaser healthcheck` exists and checks that
  needed tools are on PATH (goes in the runbook for local setup).

## R13. What is NOT used anywhere (Pro fence, full list)

Global `metadata` block, extended/global `after` hooks, `includes`,
`template_files`/`templated_files`, changelog `paths`/`title`/subgroups/
`divider`/`ai`, MSI/NSIS, cask `app`/DMG/`alternative_names`/versioned
casks, `token_type`, `pull_request.check_boxes`, release
`header.from_url`/`from_file`, `prebuilt` builder, archive hooks,
`templated_extra_files`, sign/sbom `installer`+`diskimage` artifact types.
The config must pass `goreleaser check` under the free distribution
(SC-008); exit code 2 (deprecated-but-valid) also fails CI.

## R14. Repo-side facts (verified in-tree 2026-08-14)

- Module rename touches `go.mod` plus 68 Go files importing
  `github.com/slng/unmute`.
- Live docs that reference the old clone URL and must change with FR-018
  (re-counted after `main` retired `docs/user/`, commit `fda1b61`): four
  files, seven lines. `README.md` (clone URL + `cd`),
  `docs-site/start/installation.mdx` (clone URL + `cd`, and it carries an
  explicit note that `go install ...@latest` does not work because of the
  module path mismatch; this feature deletes that note at Rollout Phase 2),
  `docs-site/README.md` (two Mintlify repository-access lines),
  `docs/REPO_MAP.md` (the old repo name in its opening line). Historical
  spec artifacts (`specs/008-*/report.md`, `verification-log.md`) are
  records of their time and stay untouched.
- CI today: `ci.yml` with test / lint / format jobs, `permissions:
  contents: read`, concurrency group per ref. The release-validation job
  slots in beside them.
- Makefile: single-line targets; `LDFLAGS` currently stamps only
  `main.version` from `git describe`.
- `.gitignore` has no `dist/` entry yet (FR-022).

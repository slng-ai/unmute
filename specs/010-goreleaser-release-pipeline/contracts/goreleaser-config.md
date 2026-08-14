# Contract: `.goreleaser.yaml`

**Date**: 2026-08-14 | Verified against live GoReleaser docs ([research.md](../research.md)).

The file the implementation must produce, in full. Every key below is
GoReleaser v2 free tier; `goreleaser check` must pass with exit 0 (exit 2 =
deprecated key = contract breach). Comments marked `Rollout Phase N:` are
the only lines that change between rollout phases (FR-015); the "Rollout"
prefix is deliberate, so these never collide with task or plan phase
numbers.

```yaml
version: 2

project_name: unmute

# Rollout Phase 2: uncomment to build through proxy.golang.org and verify
# against sum.golang.org (verifiable builds). Requires the module to be
# publicly fetchable. Tag builds only; snapshots ignore it either way.
# gomod:
#   proxy: true

builds:
  - env:
      - CGO_ENABLED=0
    goos: [darwin, linux, windows]
    goarch: [amd64, arm64]          # default includes 386; pin to the 6 pairs
    main: .
    binary: unmute
    flags:
      - -trimpath
    mod_timestamp: "{{ .CommitTimestamp }}"
    ldflags:
      - -s -w
        -X main.version={{.Version}}
        -X main.commit={{.ShortCommit}}
        -X main.date={{.CommitDate}}

archives:
  - formats: [tar.gz]               # "formats" plural since v2.6
    format_overrides:
      - goos: windows
        formats: [zip]              # winget needs zip; default files add LICENSE+README

checksum: {}                        # defaults: unmute_<version>_checksums.txt, sha256

sboms:
  - artifacts: archive              # one <archive>.sbom.json (SPDX) per archive, via syft

signs:
  - cmd: cosign                     # keyless; needs id-token: write + cosign on PATH
    signature: "${artifact}.sigstore.json"
    args: ["sign-blob", "--bundle=${signature}", "${artifact}", "--yes"]
    artifacts: checksum

changelog:
  use: git                          # works in local, snapshot, and tag runs; no token
  sort: asc
  filters:
    exclude:                        # lines are "<sha> <subject>", hence ^.*?
      - '^.*?docs(\([[:word:]]+\))??!?:'
      - '^.*?style(\([[:word:]]+\))??!?:'
      - '^.*?test(\([[:word:]]+\))??!?:'
      - '^.*?chore(\([[:word:]]+\))??!?:'
  groups:
    - title: Features
      regexp: '^.*?feat(\([[:word:]]+\))??!?:.+$'
      order: 0
    - title: Bug fixes
      regexp: '^.*?fix(\([[:word:]]+\))??!?:.+$'
      order: 1
    - title: Others
      order: 999

release:
  github:
    owner: slng-ai
    name: unmute                    # explicit: local remotes may still say unmute_cli
  prerelease: auto                  # rc/beta tags become GitHub prereleases

homebrew_casks:                     # since v2.10; hooks since v2.13
  - name: unmute
    skip_upload: true               # Rollout Phase 2: change to "auto" (auto skips prereleases)
    repository:
      owner: slng-ai
      name: homebrew-tap            # cask lands in Casks/ (the default)
      token: "{{ .Env.GH_PAT }}"    # default token cannot write to other repos
    homepage: https://github.com/slng-ai/unmute
    description: Compile a declarative voice-agent package to Pipecat or LiveKit
    # No license field exists on casks (FR-017): the archives carry LICENSE.
    hooks:
      post:
        install: |
          if OS.mac?
            system_command "/usr/bin/xattr", args: ["-dr", "com.apple.quarantine", "#{staged_path}/unmute"]
          end

winget:
  - publisher: slng                 # required
    package_identifier: slng.unmute
    short_description: Compile a declarative voice-agent package to Pipecat or LiveKit   # required
    license: MIT                    # required
    publisher_url: https://slng.ai
    publisher_support_url: https://github.com/slng-ai/unmute/issues
    homepage: https://github.com/slng-ai/unmute
    skip_upload: true               # Rollout Phase 3: change to "auto"
    repository:
      owner: FORK_OWNER             # Rollout Phase 3: the account that owns the winget-pkgs fork
      name: winget-pkgs
      token: "{{ .Env.GH_PAT }}"
      pull_request:
        enabled: true               # branch defaults to unmute-<version> (v2.17+)
        base:
          owner: microsoft
          name: winget-pkgs
          branch: master
```

## Contract assertions

1. `goreleaser check .goreleaser.yaml` exits 0 (not 2) with the free
   distribution.
2. No key from the Pro fence (research R13) appears in the file.
3. `grep -c 'skip_upload: true'` is 2 in Rollout Phase 1, 1 in Rollout
   Phase 2, 0 in Rollout Phase 3; those flips and the `gomod` uncomment are
   the only diffs between rollout phases.
4. The two description strings above are placeholders in wording only; the
   implementation may sharpen them, but both stay one line and identical in
   meaning to the README's first sentence.
5. Snapshot runs (`--snapshot`) publish nothing (implied
   `--skip=announce,publish,validate`) and ignore `gomod.proxy`.

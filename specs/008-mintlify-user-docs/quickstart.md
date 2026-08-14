# Quickstart: validating the docs feature end to end

Run these from the repo root. They prove the feature works without reading the
implementation.

## Prerequisites

- Go 1.24 (repo toolchain), Node v20.17+ and the `mint` CLI (verified present: Node v24.16.0, mint 4.2.614).
- No credentials needed for any check below.

## 1. The verification tooling works

```bash
make build                      # produces ./bin/unmute
./bin/unmute --help             # matches reference/cli pages
```

## 2. Every anchor is ready (FR-022, SC-005, SC-009)

```bash
# each of the ten must pass both, and each must have a README
./bin/unmute validate examples/simple-prompt
./bin/unmute compile  examples/simple-prompt
ls examples/simple-prompt/README.md
# ...repeat for the other nine
go test ./internal/generate -run TestExamples   # README/transport + link rules
```

Expected: exit 0 everywhere; warnings (stderr) are fine; ten READMEs exist.

## 3. Every snippet on the site validates (FR-002)

Each YAML block in `docs-site/` was run through a scratch package during
writing. Spot-check any page: copy its snippet into
`$(mktemp -d)/agent.yaml` alongside the page's stated `targets.yaml`, then
`./bin/unmute validate <dir>`. Expected: exit 0 for the targets the page
claims.

## 4. Provider lists match the catalog (FR-023, SC-010)

```bash
go test ./... -run ProvidersDocsSite   # the new agreement test from research R7
```

Expected: pass. Edit a vendor list in `docs-site/reference/providers.mdx` and
the test must fail.

## 5. The site itself is sound (FR-019, SC-006)

```bash
cd docs-site
mint validate          # docs.json + frontmatter
mint broken-links      # internal links
mint dev --no-open     # manual look: story order, one concept per page
```

Expected: zero errors from `mint validate` and `mint broken-links`.

## 6. The story holds (SC-001..SC-003)

Manual pass, in this order: read `index` cold (can you say what Unmute is and
why?); follow `start/quickstart` on a clean machine to a talking agent using
no other page; read `build/*` in sidebar order checking no page uses a concept
a later page teaches; grep the site for "vapi", "deepgram" as targets (must be
absent) and for em/en dashes used as punctuation (must be absent).

## 7. The report exists (FR-020, SC-008)

`specs/008-mintlify-user-docs/report.md` lists every page, every
code-versus-docs discrepancy, every unverified claim, and the "three places
become four" rule note for maintainers.

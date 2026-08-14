# Implementation Plan: Unmute User Docs on Mintlify

**Branch**: `008-mintlify-user-docs` | **Date**: 2026-08-14 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/008-mintlify-user-docs/spec.md`

## Summary

Build the public Unmute user docs as a Mintlify site in a new top-level
`docs-site/` directory of this repo: a story-driven site (why → quickstart →
one-concept-per-page narrative → topical sections → reference) where every
fact is verified against the code, every YAML snippet passes
`./bin/unmute validate`, every anchor example compiles and has a README (four
get written), provider lists derive from the Go catalog with an agreement
test, and the whole thing passes `mint validate` and `mint broken-links`.
Deliverables: ~35 MDX pages plus `docs.json`, four example READMEs, one Go
agreement test, and a final discrepancy report.

## Technical Context

**Language/Version**: MDX + `docs.json` (Mintlify, docs.json schema); verification tooling is the repo's Go 1.24 binary and Go tests.

**Primary Dependencies**: `mint` CLI 4.2.614 on Node v24.16.0 (both verified installed); the built `./bin/unmute` (commit ae9f8cc) as the fact oracle; LiveKit Agents docs and docs.slng.ai as structural/tonal references; https://docs.slng.ai/models as the SLNG model source.

**Storage**: Files only. New `docs-site/` top-level directory; four new `examples/*/README.md`; one report in this feature directory.

**Testing**: `mint validate`, `mint broken-links` (site); `./bin/unmute validate`/`compile` on every snippet and anchor (facts); existing `internal/generate` example tests (README/transport and link rules); one new Go agreement test binding `docs-site/reference/providers.mdx` to the catalog.

**Target Platform**: Mintlify-hosted web docs; local preview via `mint dev --no-open`.

**Project Type**: Documentation site plus small repo hygiene additions (READMEs, one test).

**Performance Goals**: n/a (static docs). The meaningful "performance" bars are SC-002 (talking agent in under 15 minutes from the quickstart) and zero broken links.

**Constraints**: Plain language, no em or en dashes as punctuation; only Pipecat and LiveKit Agents presented as targets; SLNG first in provider lists, only SLNG gets model names; each page claims exactly its anchor's declared targets; no example gains or loses a target; product Go code is out of scope (report only); nothing published without user approval.

**Scale/Scope**: 35 pages (see contracts/navigation.md), 10 anchor examples, 5 CLI surfaces, 2 targets, 1 provider reference with agreement test.

## Constitution Check

*GATE: evaluated against Unmute Constitution v2.0.0. Re-checked after Phase 1 design: PASS.*

| Principle | Check |
|---|---|
| I. Compile ahead of time | PASS. No runtime code, no Python, no new Go packages. The one new Go file is a test. |
| II. Fail loud, never average | PASS. Docs describe warn/gated behavior truthfully (warnings to stderr, exit 0; gated errors name the target). No support claim without saying what it means. |
| III. One source of truth | PASS with two obligations, both planned as tests: the providers page states catalog facts a second time (agreement test, research R7, quickstart §4), and CLI pages quote captured help output (agreement test in `internal/cli` comparing the in-process cobra tree's help to the captured `help.txt`, tasks T004). No hand-authored schema files. |
| IV. The document wins | PASS. Any code-versus-doc disagreement stops and goes to the report (FR-004). External claims (Mintlify, SLNG models) verified against live docs with dates. |
| V. Whatever compiles can be spoken to | PASS. The docs teach exactly the shipped command surface, verified from the binary (research R3). `apply` is never mentioned. |
| Targets and providers boundary | PASS. Site presents only Pipecat and LiveKit as targets; omitting Vapi/Deepgram makes no false claim (spec Assumptions). Vendor-versus-target wording kept apart. |
| Voice | PASS. Plain wording enforced per page (data-model Page rules). |

Violations to justify: none. Complexity Tracking left empty.

## Project Structure

### Documentation (this feature)

```text
specs/008-mintlify-user-docs/
├── plan.md              # This file
├── research.md          # Phase 0: verified facts and decisions R1-R10
├── data-model.md        # Phase 1: page/anchor/CLI/provider/report entities
├── quickstart.md        # Phase 1: end-to-end validation guide
├── contracts/
│   └── navigation.md    # Phase 1: docs.json draft + 35-page inventory
├── report.md            # Written at the END of implementation (FR-020)
└── tasks.md             # /speckit-tasks output (not created here)
```

### Source Code (repository root)

```text
docs-site/                      # NEW: the Mintlify project
├── docs.json                   # navigation per contracts/navigation.md
├── index.mdx                   # introduction (the why)
├── start/                      # installation, quickstart, how-unmute-works
├── build/                      # the 7-page learning narrative
├── dev/                        # overview, console
├── telephony/                  # overview, first-phone-call, outbound-calls, webhooks-and-tunnels
├── transfers/                  # overview, livekit, pipecat-daily, pipecat-twilio
├── targets/                    # overview, pipecat, livekit
├── deploy/                     # going-live
└── reference/                  # cli/{overview,init,validate,compile,dev}, agent-yaml,
                                # targets-yaml, providers, variables, secrets

examples/
├── simple-prompt/README.md     # NEW (must pass internal/generate example tests)
├── multi-task/README.md        # NEW
├── task-groups/README.md       # NEW
└── subagents/README.md         # NEW

internal/target/
└── providers_docsite_test.go   # NEW: agreement test, catalog ↔ docs-site/reference/providers.mdx
                                # (sits beside the existing providers_doc_test.go / user_docs_test.go)

internal/cli/
└── help_capture_test.go        # NEW: agreement test, cobra tree help ↔ captured help.txt
                                # that the reference/cli pages quote verbatim
```

**Structure Decision**: Docs live in-repo at `docs-site/` (user decision,
research R1) so snippet checks run the locally built binary and docs version
with the code. `docs/user/` stays untouched (spec Assumptions). Everything
else the feature touches is additive.

## Implementation notes for /speckit-tasks

Work in story order, verifying as you write (spec FR-001..FR-005):

1. **Foundations**: scaffold `docs-site/` with docs.json from the approved contract; brand it "Unmute by SLNG//" by copying `images/Logo_SLNG.png` to `docs-site/logo/slng.png` (navbar logo + favicon) and tuning accent colors from the logo's yellow and docs.slng.ai; confirm `mint dev --no-open` serves a stub index.
2. **Anchor readiness first** (FR-022): write the four missing example READMEs, modeled on the six existing ones, and make `go test ./internal/generate` pass; re-run validate/compile on all ten (baseline already green, research R4).
3. **Pages in sidebar order**: Get started → Build the agent → Run it locally → Telephony → Transfers → Targets → Deployment → Reference. Each page: read its anchor and named sources, verify claims against code, run every snippet through a scratch package, then write.
4. **Reference from the binary and structs**: capture fresh `--help` per command; derive agent-yaml/targets-yaml pages from `internal/spec` structs cross-read with `docs/SCHEMA.md`; build reference/providers from the catalogs and add the agreement test.
5. **Gates and report**: `mint validate`, `mint broken-links`, the manual story pass (quickstart §6), then write `report.md` (pages, discrepancies, unverified claims, the "three places become four" note).

Out of scope, restated: product Go code changes (report only), retiring
`docs/user/`, deployment (user runs `mint login` and approves the push;
domain/branch decided then).

## Complexity Tracking

No constitution violations to justify.

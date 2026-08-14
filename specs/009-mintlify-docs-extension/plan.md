# Implementation Plan: Mintlify Docs Extension

**Branch**: `worktree-user-facing-docs` (spec directory `009-mintlify-docs-extension`) | **Date**: 2026-08-14 (updated same day after two clarification rounds) | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/009-mintlify-docs-extension/spec.md`

## Summary

Extend the docs site built under 008, in this order. First, commit the
untracked docs work and merge `origin/pre-release-v1` (MCP tool sources N40,
the breaking connection change N41), then update every page the merge makes
wrong. Second, restructure "Build the agent" into a concept-first shape with
nested Tools and Orchestration groups, plus a new `reference/connections-yaml`
page. Third, the two clarified expansions: a top-level Models group (STT, TTS,
LLM, VAD/turn detection, optimization on the SLNG Execution Layer, Context
Router excluded) absorbing `reference/providers`, and a Development lifecycle
group that owns the whole local loop (logs, console, the local phone call,
tunnels), with per-platform go-live CLI guides and a Twilio provider page.
The site grows from 35 to 49 pages. Fourth, pass every gate and write dated
addenda into the 008 artifacts, including a D1 to D5 re-check. Two small new
agreement tests hold twice-stated facts: the execution-block set (research
R17) and the retargeted catalog test that follows the vendor lists onto the
Models role pages (research R21).

## Technical Context

**Language/Version**: MDX + `docs.json` (Mintlify). Verification tooling is the repo's Go 1.24 binary, rebuilt from the merged tree, and the Go test suite.

**Primary Dependencies**: `mint` CLI 4.2.614 (verified installed, research R12); the merged `./bin/unmute` as the fact oracle; the two landed feature specs as the change statement; merged `docs/SCHEMA.md` (N40, N41); the emitted runbooks (`internal/generate/templates/*/README.md.tmpl`) as the go-live command truth (research R24); external doc sources fetched and dated during writing: docs.slng.ai (models, execution layer), LiveKit Cloud and Pipecat Cloud CLI docs, Twilio console docs.

**Storage**: Files only. Changed and new `.mdx` under `docs-site/`, `docs.json`, two new Go test changes (one new file, one retargeted), dated addenda in `specs/008-mintlify-user-docs/`.

**Testing**: `go test ./...` (agreement tests: help capture, catalog retargeted to Models pages, examples, execution blocks), `make lint`, `gofmt -l internal/`, `mint validate`, `mint broken-links`, `mint dev --no-open`, `./bin/unmute validate` and `compile` on all eleven examples, grep gates (no em or en dashes, no Vapi as target, moved fields gone, zero Context Router mentions, no provider-branded env names as invalid examples), nav-matches-disk count at 49.

**Target Platform**: Mintlify-hosted web docs, local preview only. The docs site deploys nowhere (FR-039); the go-live pages teach the reader to deploy their own agent.

**Project Type**: Documentation site extension plus small repo hygiene additions (tests, addenda).

**Performance Goals**: n/a (static docs). The bars are the gates and the honesty trail.

**Constraints**: Every 008 constraint carries over (FR-026). Merge before writing (FR-027). A page ships only if the code has the concept (FR-033). Only SLNG model names, SLNG first, catalog-derived vendor lists (FR-040). Context Router excluded (FR-041). External claims attributed and dated, never asserted as measured here (FR-041, FR-045, FR-046). No provider-branded env names as invalid examples (FR-044). No product code or product doc fixes in passing. Plain language, no em or en dashes as punctuation.

**Scale/Scope**: 49 pages (35 today: minus 7 flat build, plus 12 build, plus 1 connections reference, plus 5 Models with providers retired, plus dev/telephony, telephony/twilio, and 2 deploy guides; webhooks-and-tunnels moves without changing the count). 11 examples (one new). 4 agreement tests after this work. 5 addenda files. 9 sidebar groups.

## Constitution Check

*GATE: evaluated against Unmute Constitution v2.0.0. Re-checked after Phase 1 design: PASS.*

| Principle | Check |
|---|---|
| I. Compile ahead of time | PASS. No runtime code, no maintained Python. New Go is test code only. Python shown on the tools/python page is example handler code, run and linted per CLAUDE.md. |
| II. Fail loud, never average | PASS. New refusals quoted from real runs only (moved fields naming their new home, contract fields illegal on `mcp:` files, gated transfer naming its connection, the invalid env-name refusal with a neutral example). Telephony stays `provisional` on every page. |
| III. One source of truth | PASS with three obligations, all planned as tests or sweeps: the tools overview states the execution-block set (new test, research R17); the Models role pages restate the catalog's vendor lists (the existing catalog agreement test retargets to them in the same change that retires `reference/providers`, research R21); the telephony/twilio env-name mapping restates the examples' connection files (held by re-validating those examples and cross-reading the page, no new test: the names are few and already covered by example validation). Execution-layer facts have one home, SLNG's docs, and the site only attributes and links. |
| IV. The document wins | PASS. Merged SCHEMA.md and the landed feature-spec contracts are normative; disagreements go to the report addendum with verdicts (D1 to D5 re-checked the same way). External provider and platform claims (SLNG execution layer, LiveKit Cloud, Pipecat Cloud, Twilio console) are fetched from official docs during writing and carry dates; anything unexecutable ships attributed, listed as unverified. The deployment stance is the adopted one (2026-08-12): managed clouds for remote deploys, DEPLOYMENT.md's self-hosted path stays what it is (research R24). |
| V. Whatever compiles can be spoken to | PASS. CLI command sources did not change on the merge (research R13); the scaffold did, so the quickstart transcript is re-captured. The dev pages document the real dev surface including its log files, verified per mode (research R23). `apply` is never mentioned. |
| Targets and providers boundary | PASS. Two targets everywhere. Models role pages keep vendor-versus-target language strict: vendors are catalog entries, never runtimes. The SLNG STT Performance Layer's "Private Beta" status is presented as SLNG's own status (research R22). |
| Telephony boundary | PASS. The telephony/twilio page reads console values and configures the reader's number; it never claims Unmute buys numbers or creates carrier resources, and stays consistent with what `dev --telephony` automates restorably. |
| Voice | PASS. Plain wording, short sentences, per page. |

Violations to justify: none. Complexity Tracking left empty.

## Project Structure

### Documentation (this feature)

```text
specs/009-mintlify-docs-extension/
├── plan.md              # This file
├── research.md          # Phase 0: verified facts and decisions R11-R26
├── data-model.md        # Phase 1: entities this extension adds or changes
├── quickstart.md        # Phase 1: end-to-end validation guide
├── contracts/
│   ├── navigation.md    # Phase 1: target docs.json tree + 49-page inventory + moves
│   └── update-map.md    # Phase 1: per-page blast radius and new-page content contracts
├── checklists/requirements.md
└── tasks.md             # /speckit-tasks output (regenerate after this update)
```

The deliverable addenda land in `specs/008-mintlify-user-docs/` (navigation
amendment, verification-log addendum, report addendum, tasks phase), which
stays the home of the site's living artifacts.

### Source Code (repository root)

```text
docs-site/
├── docs.json                          # 9 groups, 49 pages: see contracts/navigation.md
├── build/                             # restructured: your-first-agent, Tools/, variables, Orchestration/
├── dev/                               # group renamed "Development lifecycle"
│   ├── overview.mdx                   # UPDATED: the loop + where each mode writes its logs
│   ├── console.mdx                    # stays
│   ├── telephony.mdx                  # NEW: the local phone-call run, moved from telephony/overview
│   └── webhooks-and-tunnels.mdx       # MOVED from telephony/webhooks-and-tunnels.mdx
├── telephony/
│   ├── overview.mdx                   # UPDATED: concepts and routes only; links to dev/telephony
│   ├── first-phone-call.mdx           # UPDATED: N41 + neutral env example
│   ├── outbound-calls.mdx             # UPDATED: N41, outbound-reminder's two connections
│   └── twilio.mdx                     # NEW: get your details from the Twilio console, per route
├── models/                            # NEW top-level group (after Targets)
│   ├── stt.mdx                        # vendor lists per target from the catalog, SLNG first
│   ├── tts.mdx
│   ├── llm.mdx
│   ├── turn-detection.mdx             # VAD and turn detection, per-target reality
│   └── optimization.mdx               # SLNG Execution Layer: STT + TTS stages only, attributed
├── deploy/
│   ├── livekit-cloud.mdx              # NEW: lk cloud auth, lk agent create/deploy, from the emitted runbook
│   ├── pipecat-cloud.mdx              # NEW: pipecat cloud secrets set + deploy, from the emitted runbook
│   └── going-live.mdx                 # UPDATED: shared checklist, neutral env-name example
├── reference/
│   ├── connections-yaml.mdx           # NEW (N41)
│   └── (providers.mdx RETIRED into models/)
└── (all other N40/N41-touched pages per contracts/update-map.md)

internal/spec/
└── tools_docsite_test.go              # NEW: execution blocks ↔ build/tools/overview.mdx

internal/target/
└── providers_docsite_test.go          # RETARGETED: catalog ↔ models/{stt,tts,llm,turn-detection}.mdx

specs/008-mintlify-user-docs/          # dated addenda: navigation, verification log, report, tasks phase
```

**Structure Decision**: Everything stays in-repo where 008 put it. Sidebar
group order: Get started, Build the agent, Development lifecycle, Telephony,
Transfers, Targets, Models, Deployment, Reference. Models sits after Targets
because its lists are per-target facts. No redirects (research R18): the site
has never been deployed, and `mint broken-links` guards internal links after
the moves (Build pages, webhooks-and-tunnels, providers).

## Implementation notes for /speckit-tasks

Strict order, because each phase invalidates work done before it:

1. **Commit, merge, rebuild** (FR-027), then read the landed contracts and the merged SCHEMA.md before touching any page.
2. **Merge-accuracy pass** over the existing pages (FR-028 to FR-032): fresh moved-field grep, ten-plus page updates with triggered refusals, re-captured quickstart transcript, new `reference/connections-yaml`, updated secrets rule. Fix the branded env-name example on all three pages here too (FR-044): it is a correctness fix, not a restructure.
3. **Restructure Build** (FR-033 to FR-036): moves, five tools pages (mcp anchored on `examples/mcp-example`), orchestration group, the execution-blocks test. Read the LiveKit inspiration pages first, shape only.
4. **Development lifecycle** (FR-043): rename the group, write `dev/telephony` from the material leaving `telephony/overview`, move webhooks-and-tunnels, add the per-mode log-file facts to the overview, verified by running each mode.
5. **Models group** (FR-040 to FR-042): five pages from the merged catalogs and the fetched execution-layer docs, retire `reference/providers`, retarget its agreement test in the same change, re-point inbound links, add the "SLNG models by design" line where the scaffold is first shown.
6. **Go live and Twilio** (FR-045, FR-046): the two platform guides grounded in real compiles of the emitted runbooks plus dated platform docs; the telephony/twilio page grounded in the examples' connection env names plus dated Twilio docs (`docs/user/learn/twilio-walkthrough.md` and `08-going-live.md` are starting points, never copy to paste).
7. **Gates, then addenda** (FR-037 to FR-039): the full quickstart.md table, then the dated addenda with D1 to D5 verdicts and the unverified list (which now also carries any unexecuted cloud-deploy steps).

Out of scope, restated: deploying the docs site, deploying any agent from this
work without credentials on hand, product code fixes, example target changes,
retiring `docs/user/`, redirects.

Retiring `docs/user/` happened separately on 2026-08-14: the tree was deleted
and `make docs` now previews `docs-site/`. Paths under `docs/user/` in this
folder are history.

## Complexity Tracking

No constitution violations to justify.

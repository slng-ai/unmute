# Data Model: Mintlify Docs Extension

The 008 data model still holds (Page, Navigation group, Example anchor, CLI
surface, Provider capability row, Discrepancy report). This extension adds or
changes the entities below. All file-shaped.

## Nested navigation group

A `{"group": ..., "pages": [...]}` object inside another group's `pages`
array, the pattern Reference already uses for CLI. This feature adds two:
Tools and Orchestration, both inside "Build the agent". The group order stays
the story arc; inside a nested group, the overview comes first and is the only
page allowed to name a concept before its page teaches it (it names, it does
not use).

## Tools page

One page per way a tool can run. The set of pages is bound to the execution
blocks of the `Tool` struct in `internal/spec`:

| Page | Block | Status rule |
|---|---|---|
| `build/tools/webhook` | `webhook` | taught in full |
| `build/tools/python` | `local` | taught in full |
| `build/tools/mcp` | `mcp` | taught in full (N40) |
| `build/tools/prebuilt` | `builtin` | taught as a closed registry, currently exactly `end_call` |
| `build/tools/overview` | all six | teaches shared vocabulary; `client` and `provider_hosted` appear here only, as gated, per the capability table |

Rule: a block gets a page only if the code has it, and the overview's stated
set is held by the new agreement test (research R17). Behaviour fields
(`interruption`, `effect`) and assignment scoping are taught once, on the
overview.

## Connection

A new documented authoring surface: `connections/<name>.yaml`, owner of one
whole phone route (SCHEMA N41). Documented on `reference/connections-yaml.mdx`
with exactly three valid shapes, each backed by a validated package:

| Shape | What it declares |
|---|---|
| Full route | `transport`, `carrier`, `environment` names |
| Receive-only | no credentials; valid on the one route the contract names |
| Carrier-less | `transport: daily-sip`; the platform provisions the number |

Relationships: one target names at most one connection; two targets whose
transports differ need two connection files (the `outbound-reminder` teaching
moment). `kind:` is not written in a connection file; `destinations:` lives in
`agent.yaml` and maps symbolic names to env var names only.

## Updated page

An existing page the merge made wrong. Tracked in `contracts/update-map.md`
with: the page, what it currently shows, what the merged truth is, and how the
fix is verified (fresh grep, re-run refusal, re-captured transcript, or
re-validated snippet). States: stale → corrected → verified. The pre-merge
baseline is ten pages; the post-merge grep is authoritative.

## Addendum

A dated, append-only section in a 008 artifact. Four are mandatory
(navigation amendment, verification-log addendum, report addendum, tasks
phase) plus `docs-site/README.md` if rules changed. An addendum never rewrites
what it extends and follows the receiving file's existing form (research R20).

## Models role page

One page per catalog role (`models/stt`, `models/tts`, `models/llm`,
`models/turn-detection`), carrying the per-target vendor lists from
`catalog_pipecat.go` and `catalog_livekit.go`. Rules: SLNG first; exact model
names only for SLNG (ids proven in this repo), linked to
https://docs.slng.ai/models; other vendors' ids pass through unchecked.
Guarded by the retargeted catalog agreement test, which follows the lists
here when `reference/providers` retires.

## Execution Layer (external entity)

SLNG's optimization system. On this site only two stages exist: STT
Performance Layer (Private Beta, SLNG's own status) and TTS Path
Optimization. The Context Router is excluded by maintainer decision
(2026-08-14). Facts are external: fetched, dated, attributed, linked, never
asserted as measured here. Lives on `models/optimization` plus one
"SLNG models by design" line where the scaffold is first shown.

## Development lifecycle group

The renamed Dev group (label only, slugs stay `dev/*`): overview (the loop
and per-mode log files), console, telephony (the local phone-call run),
webhooks-and-tunnels (moved in). Log-location claims are per mode and
verified by running that mode.

## Platform go-live guide

One page per platform (`deploy/livekit-cloud`, `deploy/pipecat-cloud`)
walking the generated project to production with the platform's own CLI.
Source of truth: a real compile's generated README, cross-checked against
the platform's current docs with fetch dates. Commands not executed here are
attributed and listed as unverified in the report addendum.

## Provider telephony guide

The `telephony/twilio` page: where each needed value lives in the Twilio
console and which connection env name holds it, per route, per target,
matching the telephony examples' connection files exactly. Never claims
Unmute provisions carrier resources.

## Discrepancy verdict

One per D1 to D5 in the report addendum: **stands** (still true on the merged
tree), **stale** (the merge resolved it), or **changed** (still a
disagreement, but different), each with the code and doc locations re-read on
the merged tree as evidence. New disagreements found during this work get new
numbers (D6 onward) in the same format.

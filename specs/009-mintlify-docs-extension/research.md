# Research: Mintlify Docs Extension

**Date**: 2026-08-14. Everything below was verified by running commands in this
worktree, mostly by reading `origin/pre-release-v1` through git without merging
it (the merge is implementation step one, FR-027). Numbering continues from
008's research (R1 to R10), which still stands except where noted.

## R11. What actually landed on origin/pre-release-v1

- **Decision**: Treat `specs/008-mcp-tool-sources/` and `specs/008-simplify-telephony-connections/` as the authoritative change statement, with merged `docs/SCHEMA.md` amendments N40 and N41 (both dated 2026-08-14) as the contract of record.
- **Facts captured** (git log `ae9f8cc..origin/pre-release-v1`, 11 commits): `80b26bb` lands MCP tool sources (N40), `39d2198` moves the route into the connection (N41, breaking), `6405ec8` makes `secrets:` list every author-written env name (breaking), plus validation refusal commits (`4588161`, `26866ba`), doc and example alignment commits, and smoke fixture moves. 129 files, +7502/-792.
- **Rationale**: The brief's section 3 is a map, not a source of truth. The specs and SCHEMA.md are.

## R12. Tooling unchanged since 008

- **Decision**: Same loop as 008: `mint` CLI 4.2.614 (re-verified installed today), `mint validate`, `mint broken-links`, `mint dev --no-open`. Components: `<Columns cols={2}>` for card groups, Lucide icon names (already set in docs.json).
- **Rationale**: `mint --version` run in this worktree. The two Mintlify details are stated in the brief as lessons already learned; re-check against the Mintlify index MCP only if a new component is needed.

## R13. The CLI help surface did not change on the merge

- **Decision**: Do not re-capture `help.txt`. The help-capture agreement test should pass on the merged tree as is. If it fails, that is a finding to investigate, not a prompt to re-capture.
- **Facts captured**: `git diff --stat ae9f8cc origin/pre-release-v1 -- internal/cli` touches only test files (`compile_test.go`, `dev_compose_smoke_test.go`, `dev_test.go`, `validate_test.go`). No command source file changed, so the cobra tree and its flags are identical.
- **What did change**: `internal/scaffold/templates/` (no `transport:` line in `targets.yaml.tmpl`, new `connections/phone.yaml.tmpl`), so the `unmute init` output changed and the quickstart transcript must be re-captured (FR-032). Scaffold output is not help output; the two are separate captures.

## R14. MCP capability facts (read from origin/pre-release-v1 internal/target/table.go)

- **Decision**: The mcp page teaches these rows as the capability truth, re-confirmed after the merge:
  - `tools.execution.mcp`: denied only for Deepgram ("Deepgram has no runtime MCP client"). Pipecat and LiveKit are ok; the old Pipecat gate is lifted (N40).
  - `tools.execution.mcp` carries a LiveKit requirement note: `sdk_language: python` ("LiveKit MCP tools require sdk_language: python").
  - `tasks.tools.execution.mcp`: denied for Pipecat ("the Pipecat driver cannot scope an MCP tool source to a task: list it on the agent instead"). So scope is a real per-target difference the page must teach.
  - `tools.execution.client` and `tools.execution.provider_hosted`: denied on all four providers ("not proven by its driver"). These stay gated and are mentioned as such, not taught.
  - `tools.execution.builtin`: denied for Vapi and Deepgram only, so ok on both code targets.
- **Rationale**: FR-028 says the capability table wins over feature-spec prose. These are the pre-merge readings; the implementation re-reads them from the merged working tree before printing them.

## R15. The authoring shapes this extension documents (read from origin/pre-release-v1 internal/spec/package.go)

- **Facts captured**:
  - `Target` accepts exactly: `provider`, `version`, `pins`, `sdk_language`, `connection`, `deployment_region`, `models`. `transport`, `carrier`, and `destinations` remain on the decode struct with `json:"-"` so a package written the old way is refused with a message naming the new home, and they stay out of the derived schema (held by `authoring_surface_test.go`).
  - `Destinations map[string]string` now lives on `AgentFile` (top level of agent.yaml), values are env var names only.
  - `Connection` is `transport`, `carrier` (empty on the Daily-provisioned route), `kind`, `environment`. The struct still decodes `kind`, while the landed contract says `kind:` is no longer written in a connection file: how the loader or validator treats a written `kind:` must be checked in the merged tree and, if code and contract disagree, reported as a new discrepancy, not settled.
  - `ToolMCP` is `url_env` (required), `transport`, `auth`, `tools`. The six execution blocks are `webhook`, `local`, `mcp`, `builtin`, `client`, `provider_hosted`.
  - The prebuilt registry (`internal/target/prebuilt.go`) is closed and holds exactly one entry: `end_call`, effect fixed to `ends_conversation`, with a default description the author's text is added on top of.
- **Rationale**: FR-029, FR-034. These are the struct-level facts; refusal wording is captured live during implementation (FR-030, SC-019).

## R16. Blast radius on the current site (grep run today, pre-merge)

- **Facts captured**: ten pages mention `transport`, `destinations`, or `kind: telephony`: `reference/agent-yaml`, `reference/cli/compile`, `reference/targets-yaml`, `targets/overview`, `telephony/outbound-calls`, `telephony/overview`, `transfers/livekit`, `transfers/overview`, `transfers/pipecat-daily`, `transfers/pipecat-twilio`. Hit counts: `transport:` 5, `destinations:` 5, `kind: telephony` 8.
- **Decision**: contracts/update-map.md records this as the baseline checklist, and FR-030 requires a fresh grep after the merge; the fresh grep wins.
- **Trap noted**: `kind: telephony` under `agent.yaml` channels is correct and stays. Only connection-file `kind:` went away. Each hit is judged by which file the snippet shows.

## R17. A third docs agreement test: yes, one small one

- **Decision**: Add `internal/spec/tools_docsite_test.go`: the set of execution block names stated on `build/tools/overview.mdx` must equal the block fields of the `Tool` struct in `internal/spec`. Same pattern and size as the providers test from 008.
- **Rationale**: The tools overview states the execution-block set a second time, and Constitution Principle III makes an agreement test mandatory where a fact is stated twice. The brief invites exactly this test. It is ~30 lines and fails the day `internal/spec` gains or loses a block.
- **Alternatives considered**: No test (violates Principle III once the overview prints the set); generating the page from the struct (more machinery than one test justifies).

## R18. No redirects for the old build paths

- **Decision**: Skip the `redirects` array. Move the pages, fix every internal link, and let `mint broken-links` hold the result.
- **Rationale**: The site has never been deployed, so no external link exists (spec edge case; the brief calls redirects a nice to have). Redirects would be config for URLs nobody has ever seen.
- **Alternatives considered**: Adding redirects anyway (harmless but dead weight; anyone wanting them later adds an array to docs.json in one commit).

## R19. Page inventory math and the nesting pattern

- **Decision**: 41 pages. Today's 35, minus the 7 flat build pages, plus 12 build pages (`your-first-agent`, 5 tools, `variables`, 5 orchestration), plus `reference/connections-yaml`. Nested groups use the exact `{"group": ..., "pages": [...]}` object-in-pages-array pattern the Reference group already uses for CLI (verified by parsing the current docs.json).
- **Content mapping**: `one-agent` moves to `your-first-agent` (same content, better name); `tools.mdx` retires, its verified content seeding `tools/overview` and `tools/webhook`; `two-agents` moves to `orchestration/handoffs` reframed as a concept; `tasks`, `task-groups`, `choosing-a-structure` move under `orchestration/` (the first two mostly as they are, the capstone enhanced into a decision aid); `orchestration/overview`, `tools/python`, `tools/mcp`, `tools/prebuilt` are new writing.
- **Anchors for new pages**: mcp on `examples/mcp-example` (verified on origin/pre-release-v1: declares both targets, LiveKit pinned to 1.6.4 with `sdk_language: python`, Pipecat 1.5.0; has README, agent.yaml, instructions.md, targets.yaml, tools/web_search.yaml). python on `examples/outbound-reminder` (has a `local:` tool with `tools/cancel_appointment.py`). webhook on `examples/salon-support` or `examples/outbound-reminder` (chosen while writing, whichever reads cleaner). prebuilt from the registry itself plus whichever example carries a `builtin:` tool, if any (checked during writing).

## R20. Addenda style (read from the 008 artifacts today)

- **Decision**: Each addendum is appended, dated 2026-08-14 (or the date written), in the receiving file's own voice: navigation.md already carries one dated amendment note (the D1 slot change), so the new amendment follows that form; verification-log.md gets a new numbered section continuing its structure; report.md gets an "Addendum" section with the new page table, a D1 to D5 verdict list, new discrepancies, and the unverified claims restated; tasks.md gets a new phase with checkboxes continuing after the first 56 tasks.
- **D re-check leads**: D4 (the `Inject` comment claiming mcp is legal) is likely resolved or changed by N40, and D2 (root README target coverage) may have changed with example churn; both are verified against the merged tree, never assumed. D1, D3, D5 are re-checked the same way.
- **The standing unverified claims**: no spoken turn completed (SC-002 unmeasured), SLNG model ids proven only from this repo, no live phone call made. Restated in the addendum unless this work proves otherwise.

## R20a. Second pass note

R21 to R26 were added after the two clarification rounds of 2026-08-14
(Models and Execution Layer; Development lifecycle, go-live guides, Twilio
guide, env-name examples). Same method: verified in this worktree, mostly by
reading `origin/pre-release-v1` through git without merging.

## R21. Models pages come from the catalog, and the agreement test follows them

- **Decision**: The Models role pages (stt, tts, llm, turn-detection) are written from `catalog_pipecat.go` and `catalog_livekit.go`, and `internal/target/providers_docsite_test.go` is retargeted from `reference/providers.mdx` to the role pages in the same change that retires that page. SLNG first; model names only for SLNG (the ids proven in this repo); other vendors pass model ids through unchecked.
- **Facts captured** (origin/pre-release-v1 catalogs, entry counts by role): Pipecat holds 10 Listen, 9 Reason, 11 Speak entries; LiveKit holds 9 Listen, 9 Reason, 10 Speak. Vendor sets overlap but differ (for example `openai` has 3 Pipecat entries and 1 LiveKit entry; LiveKit has `azure`, `aws`, `gemini`, `mistralai` where Pipecat has `google`, `mistral`, `qwen`, `deepseek`). Turn/VAD entries are catalog facts to re-read on the merged tree (the mcp example pins LiveKit `turn-detector-mini`; Pipecat VAD reality is read from the catalog and the emitted project during writing).
- **Rationale**: Constitution Principle III: the vendor lists are stated twice, so the existing agreement test must follow the content. Retiring providers without moving the test would delete coverage.

## R22. Execution Layer facts (fetched from docs.slng.ai on 2026-08-14)

- **Facts captured**: The Execution Layer names three stages: STT Performance Layer (marked Private Beta), Context Router (a drop-in OpenAI-compatible LLM endpoint), TTS Path Optimization (reuses generated audio for common phrases). SLNG's own headline numbers: 39% latency reduction per turn, 53% pipeline cost reduction, and "a 16-turn voice call makes 48 model calls". All stages run on a global edge network with lowest-latency routing by default.
- **Decision**: The optimization page covers only the STT and TTS stages (maintainer decision, Clarifications 2026-08-14). The Context Router is never mentioned. Every number is attributed to SLNG's docs with a link; the Private Beta status of the STT layer is stated as SLNG's own status. Re-fetch the pages during writing so the shipped text matches the then-current docs, with the fetch date in the verification log.

## R23. Where `unmute dev` writes logs (read from internal/cli today)

- **Facts captured**: `--verbose` help text: "follow container/agent logs on stderr (default: write to the log file only)". Telephony and cloud-websocket runs create `telephony.log` in the build output directory (`dev_telephony.go`, `dev_cloud_websocket.go`); the compose path tracks its own `logPath` (`dev_compose.go`); the console path is documented as "no log file" (`dev.go` comment, C6).
- **Decision**: The Development lifecycle overview states log locations per mode, each verified by actually running that mode on the merged tree and looking at what appears. No blanket "logs are in the folder" sentence.

## R24. Go-live command truth is the emitted runbook

- **Facts captured** (origin/pre-release-v1 templates): the LiveKit runbook teaches `lk cloud auth`, then `lk agent create --secrets-file .env` (first deploy) and `lk agent deploy` (updates). The Pipecat runbook has a "Deploy to Pipecat Cloud" section: `pipecat cloud secrets set <set> --file .env` then `pipecat cloud deploy`, with region and `--min-agents` variants. `docs/DEPLOYMENT.md` (adopted stance 2026-08-12) says remote deployment uses the managed clouds and keeps the self-hosted path as a documented alternative.
- **Decision**: `deploy/livekit-cloud` and `deploy/pipecat-cloud` are written from a real compile's generated README for each target (code-verified by construction), summarizing the path and sending the reader to their own generated README as the runbook. Platform CLI claims are re-checked against the platforms' current official docs with fetch dates. Steps that need a real cloud account and were not executed ship attributed and go on the unverified list.
- **Alternatives considered**: writing free-standing platform tutorials (rejected: they would duplicate the emitted runbook, which is the single source the compiler already maintains).

## R25. The Twilio page maps console values to connection env names

- **Facts captured**: the telephony examples' connection files on the merged branch declare the env names per route (outbound-reminder: `twilio_websocket.yaml` on `carrier-websocket` and `twilio_connector.yaml` on `connector`; the other telephony examples' files are read during writing). `docs/user/learn/twilio-walkthrough.md` exists on the merged branch as a starting point.
- **Decision**: `telephony/twilio` is one page, sectioned by route (Pipecat routes, LiveKit route), teaching: buy a number, find the account SID and auth token in the console, and which connection env name each value fills, exactly as the examples declare them. Console navigation steps are verified against current Twilio docs with a fetch date. The LiveKit sample the maintainer pasted is shape and tone inspiration only: our LiveKit dev flow creates local trunks automatically, so TwiML Bins and hand-written trunk JSON appear only if our code's route actually uses them.

## R26. Renames, moves, and the count is 49

- **Decision**: The Dev group is renamed "Development lifecycle" (label only; slugs stay `dev/*`, so no page moves for the rename itself). `telephony/webhooks-and-tunnels.mdx` moves to `dev/webhooks-and-tunnels.mdx` (a real move with a link sweep). New pages: `dev/telephony`, `telephony/twilio`, `deploy/livekit-cloud`, `deploy/pipecat-cloud`, `models/{stt,tts,llm,turn-detection,optimization}`. Retired: `reference/providers`. Count: 35 - 7 + 12 + 1 (connections) + 5 - 1 (models/providers) + 4 = 49, group order per plan.md.
- **Env-name example fix**: `11LABS_API_KEY` appears as the invalid-name example on three pages (going-live, first-phone-call, secrets; grep run today). The replacement is a neutral name that still starts with a digit so the rule stays illustrated, and the exact string is checked against the validator's real refusal during implementation.

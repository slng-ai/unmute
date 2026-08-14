# Research: Unmute User Docs on Mintlify

**Date**: 2026-08-14. Everything below was verified by running commands in this
worktree or fetching current vendor docs. Nothing is from memory.

## R1. Where the docs live

- **Decision**: New top-level `docs-site/` directory in this repo.
- **Rationale**: User's explicit choice (Clarifications, 2026-08-14). `docs/` is taken by internal docs. In-repo means snippet checks can run the locally built `./bin/unmute` and docs version with the code.
- **Alternatives considered**: docs.slng.ai repo (rejected: docs would drift from code), standalone repo (rejected: same drift problem, extra deploy).

## R2. Tooling versions (verified on this machine)

- **Decision**: Use the installed `mint` CLI 4.2.614 on Node v24.16.0. No installs needed.
- **Rationale**: Ran `node --version` (v24.16.0, above the required v20.17) and `mint --version` (4.2.614). `mint` resolves at `~/.nvm/versions/node/v24.16.0/bin/mint`.
- **Alternatives considered**: `npm i -g mint` (not needed), `mint update` (only if a command misbehaves during implementation).

## R3. CLI surface (verified from the built binary, commit ae9f8cc)

- **Decision**: Document exactly this surface, captured from `./bin/unmute ... --help` after `make build`:
  - Root: `unmute` with `-h/--help`, `-v/--version`. Short text: "Author-once, portable voice agents." Commands: `init`, `validate`, `compile`, `dev`, plus cobra's built-in `completion` and `help`.
  - `unmute init [name]`: no flags beyond help.
  - `unmute validate <package-dir>`: `--target strings` (repeatable).
  - `unmute compile <package-dir>`: `--target strings` (repeatable; default: all).
  - `unmute dev <agent-dir>`: `--bot-port string` (default "7860"), `--console`, `--no-open`, `--no-webhook` (requires `--telephony`), `--port string` (default "8765"), `--public-url string` (requires `--telephony`), `--target string` (single value, "required without a TTY when multiple exist"), `--telephony`, `--to string` (E.164, requires `--telephony`), `--var stringArray` (repeatable), `--verbose`.
- **Rationale**: The mission's flag list matches the binary exactly, with three additions found in the binary that must also be documented: the `completion` command, root `-v/--version`, and the nuance that dev's `--target` is single-valued while validate/compile take a repeatable list.
- **Alternatives considered**: Trusting the mission's list alone (rejected: FR-003 requires the binary as truth).

## R4. Example anchor readiness (verified by running validate and compile on all ten)

- **Decision**: All ten examples validate and compile today. The gap is only the four missing READMEs (simple-prompt, multi-task, task-groups, subagents).
- **Facts captured**:
  - `./bin/unmute validate` passes for all ten. Recurring warning on LiveKit: "LiveKit turn placement is a preference"; task-groups adds "LiveKit TaskGroup is experimental". pipecat-human-transfer-daily prints a "Setup prerequisites" block about Daily dial-out.
  - `./bin/unmute compile` succeeds for all ten and prints report lines worth explaining in the docs: sizing lines marked `[unbenchmarked]`, and telephony evidence lines marked `provisional` with vendor doc URLs and verification dates.
  - Target sets: seven examples declare both targets; livekit-human-transfer is LiveKit only; pipecat-human-transfer-daily and pipecat-human-transfer-twilio are Pipecat only. `examples/*/build/` is gitignored, so compiling locally leaves the tree clean.
  - Feature usage (grep, to be confirmed per page while writing): tools appear in nine examples (all but twilio-telephony-hello); `variables:` in multi-task, outbound-reminder, subagents, salon-support; salon-support has a multi-entry `agents:` map; handoff wording appears in pipecat-human-transfer-daily.
- **Rationale**: FR-022 needs a ground-truth baseline. This is it.

## R5. Structure to copy (LiveKit Agents docs, fetched via the LiveKit docs MCP)

- **Decision**: Mirror the LiveKit shape at our scale: Get started (intro, quickstart, prompt-adjacent pages) → a core "logic and structure" narrative (sessions → tasks → workflows → tools → turns) → topical sections (telephony, deployment) → reference. Our narrative: one agent → tools → variables → two agents and handoffs → tasks → task groups → subagents.
- **Rationale**: Their Build Agents section is exactly the requested effect: a Get Started group, then "Logic & Structure" that grows one concept per page, then topical groups, then reference. Verified against the live overview on 2026-08-14.
- **Alternatives considered**: Reference-first structure like classic CLI docs (rejected: the mission demands story first).

## R6. Mintlify configuration and components (fetched from mintlify.com/docs via the Mintlify index MCP)

- **Decision**: One `docs.json` with `$schema: https://mintlify.com/docs.json`, required fields `name`, `theme`, `colors.primary`, `navigation`. Use root-level `navigation.groups` (no tabs: the site is small, one sidebar reads as one story). Components available and current: Steps, Tabs, CodeGroup, Cards, Accordions, Callouts, Icons, Frames, Tree. Author pages as `.mdx` with `title` and `description` frontmatter.
- **Rationale**: Verified the minimal valid docs.json and the component list against current Mintlify docs (2026-08-14). `mint dev --no-open`, `mint validate`, `mint broken-links` are the local loop.
- **Alternatives considered**: Tabs at the root (rejected for v1: ~28 pages fit one sidebar; tabs can come later without moving files).

## R7. Provider lists without a second copy of the catalog

- **Decision**: Write the providers reference page from `internal/target/catalog_pipecat.go` and `catalog_livekit.go`, and add one small Go agreement test that fails when the MDX page's vendor lists drift from `Catalog` (same pattern as the existing `docs/user/reference/providers.md` sync test). SLNG first in every list; exact model names only for SLNG, linking https://docs.slng.ai/models.
- **Rationale**: Constitution Principle III makes an agreement test mandatory wherever a fact is stated twice. The repo already has this exact test shape for the old providers page; mirroring it is the smallest compliant move.
- **Alternatives considered**: Generating the MDX from the catalog (more machinery than one test justifies for one page); no test (violates the constitution).

## R8. What guards the rest of the prose

- **Decision**: Snippets are verified by running them: every YAML block gets dropped into a scratch package and run through `./bin/unmute validate` during writing (recorded in the final report). Site integrity comes from `mint validate` and `mint broken-links`. Links from docs-site into `examples/` are few; they are checked manually and listed in the report rather than adding a new test (the existing `examples_test.go` scans `examples/` and `docs/`, not `docs-site/`).
- **Rationale**: Matches FR-002, FR-005, FR-019, FR-020 with the least new machinery.
- **Note for the future**: if docs-site grows links into `examples/`, extending `examples_test.go` to scan `docs-site/` is the one-line upgrade.

## R9. The "three places" rule grows a fourth place

- **Decision**: Flag in the final report (and in the docs-site README) that once this site ships, a change to emitted behaviour has a fourth place to update: the emitted README template, the example README, `docs/`, and now the docs-site page. Amending `CLAUDE.md` is proposed to the maintainers, not done silently.
- **Rationale**: FR-004 says disagreements and rule impacts go to the maintainers rather than being settled in passing.

## R10. Deployment: a new, private Mintlify project

- **Decision**: Build and verify locally with the mint CLI. At deploy time the site becomes a NEW Mintlify project on its own subdomain, never connected to the existing docs.slng.ai deployment. The site launches private: password authentication (dashboard: Authentication → visibility Private → Password) if the SLNG Mintlify plan is Pro or Enterprise; otherwise Mintlify private authentication (org members only, available on all plans) until the plan allows a password. The user runs `mint login`, connects GitHub, and approves the push; site name, domain, and production branch are decided at that step.
- **Rationale**: FR-021 plus two user directives on 2026-08-14: do not override an existing project (verified: this repo contains no docs.json or mint.json, so the scaffold creates a fresh project; the dashboard-side guard is "create new project" at connect time), and the site must be private at first. Auth facts verified against the current Mintlify authentication docs (2026-08-14): password auth needs Pro or Enterprise; private auth (org members) is on all plans; OAuth and JWT need Enterprise; authentication works only on a custom domain or `*.mintlify.site` subdomain, not on a custom subpath, which independently rules out hosting under docs.slng.ai as a subpath.
- **Alternatives considered**: OAuth or JWT (Enterprise only, needless machinery for "private at first"); hosting under the docs.slng.ai project (rejected twice over: the user wants a separate project, and a subpath cannot be authenticated).

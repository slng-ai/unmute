# Feature Specification: Unmute User Docs on Mintlify

**Feature Branch**: `008-mintlify-user-docs`

**Created**: 2026-08-14

**Status**: Draft

**Input**: User description: "Build outstanding, story-driven user docs for Unmute on Mintlify. The docs must explain what Unmute is, why you need it to maintain a voice agent in a simple way, and then teach the product by adding complexity slowly, one page at a time, following the LiveKit Agents docs structure. Every factual claim must be verified against the code in this repo; every YAML snippet must pass validation and every referenced example must compile. Only Pipecat and LiveKit Agents targets exist in the docs. The CLI is a first-class part of the docs. Inspiration: docs.slng.ai for tone and docs.livekit.io/agents for structure."

## The story

Unmute has no public user documentation. The repo holds strong internal design
docs and a rough draft under `docs/user/`, but nothing a stranger can read.
A person who hears "one spec, many runtimes" today has no page that proves it.

This feature ships a complete user documentation site on Mintlify. The site
tells one story in one order: why Unmute exists, how to get a talking agent in
minutes, how to grow that agent one concept at a time, and where to look things
up later. Every fact on the site is checked against the code in this repo
before it is written. Nothing ships from memory or from the old draft alone.

## Clarifications

### Session 2026-08-14

- Q: Where does the Mintlify docs project live? → A: In this repo, in a new top-level directory.
- Q: Must every anchor example validate and have its own README? → A: Yes. The user flagged that some examples lack a README (confirmed: simple-prompt, multi-task, task-groups, subagents; all telephony and transfer examples have one).
- Q: How deep do example fixes go in this feature? → A: Write the missing example READMEs and fix example YAML so every anchor validates and compiles. If a failure traces to product Go code, flag it in the report instead of fixing it here.
- Q: What should the per-target model support pages list for STT, TTS, LLM, and VAD or turn detection? → A: Exact vendor integrations per role per target, straight from the catalog code. Exact model names only for SLNG, linked to https://docs.slng.ai/models, with SLNG listed first everywhere because the examples are built on SLNG models. Note that other vendors' model IDs pass through to that vendor.
- Q: Must the site be private at first, and is that feasible on Mintlify? → A: Yes, and yes. The site launches private. Preferred method: password authentication, set in the Mintlify dashboard (Authentication → visibility Private → Password), which needs a Pro or Enterprise plan. Fallback if the plan does not allow it: Mintlify private authentication (viewers log in as members of the Mintlify organization), available on all plans. Both are dashboard settings, not repo changes. Constraint from the Mintlify docs (2026-08-14): authentication works only on a custom domain or a `*.mintlify.site` subdomain, never on a custom subpath, so the site gets its own subdomain. This is also a NEW Mintlify project; it must never be connected to or pushed over the existing docs.slng.ai deployment (verified: this repo contains no docs.json or mint.json today).
- Q: How is the site branded? → A: As "Unmute by SLNG//", using the SLNG logo at `images/Logo_SLNG.png` (yellow rounded badge, black SLNG// wordmark). The logo is copied into the docs project for the navbar and favicon, and accent colors are matched to the logo and docs.slng.ai.
- Q: May example fixes change an example's declared target set? → A: No. Each example's declared targets are authoritative. Some examples are general (both targets) and some are specific to one (verified: livekit-human-transfer is LiveKit only; pipecat-human-transfer-daily and pipecat-human-transfer-twilio are Pipecat only; the other seven declare both). Fixes and docs pages must never add or remove a target, and each page claims exactly the targets its example declares.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A newcomer understands why and gets a talking agent (Priority: P1)

A developer who has never seen Unmute lands on the introduction. In one page
they learn what Unmute is, the pain it removes (hand-written orchestrator code
that sprawls and locks you in), and what a spec looks like. They follow the
get-started path: install, `unmute init`, and an agent they can talk to in the
browser, without opening any other page.

**Why this priority**: This is the front door. If the why does not land and
the first agent does not talk, no other page matters.

**Independent Test**: Give the intro and get-started pages to someone with no
Unmute context. They can say back what Unmute is and why it exists, and they
reach a talking agent using only those pages.

**Acceptance Scenarios**:

1. **Given** a reader with no prior context, **When** they finish the intro page, **Then** they can state the problem Unmute solves and who it is for.
2. **Given** a machine with the prerequisites named on the page, **When** the reader follows the get-started steps exactly as written, **Then** they are talking to an agent in the browser and every command on the page ran as shown.
3. **Given** the reader finished get-started, **Then** the page tells them where to go next.

---

### User Story 2 - A learner grows one agent, one concept per page (Priority: P2)

A developer with a talking agent walks the core narrative: one agent with a
prompt, then a tool, then variables, then a second agent and handoffs, then
tasks, task groups, and subagents. Each page adds exactly one idea, assumes
only what earlier pages taught, and anchors to a working example in
`examples/`.

**Why this priority**: This narrative is the heart of the docs and the reason
to copy the LiveKit Agents structure. It turns a demo into a product a team
can maintain.

**Independent Test**: Read the narrative pages in order. No page uses a term
or feature before the page that teaches it. Every page names a runnable
example that compiles.

**Acceptance Scenarios**:

1. **Given** the narrative read in order, **When** any page introduces a concept, **Then** no earlier page relied on that concept.
2. **Given** any narrative page, **When** the reader opens the example it references, **Then** that example exists in `examples/`, compiles, and has its own README.
3. **Given** any YAML snippet on a narrative page, **When** it is run through validation, **Then** it passes.

---

### User Story 3 - A practitioner looks things up (Priority: P3)

A developer already using Unmute needs a specific answer: a telephony route, a
transfer pattern, a deployment step, a CLI flag, a YAML field, a supported
provider. Topical sections (tools, telephony, targets, transfers, deployment)
and a reference section (CLI, agent YAML, targets YAML, providers, variables,
secrets) answer it without making them re-read the narrative.

**Why this priority**: Lookup traffic is most of a docs site's life after the
first week, but it only matters once the narrative exists.

**Independent Test**: Pick any CLI flag, YAML field, or provider from the
code. It is findable in the reference and its documented name, default, and
meaning match the code.

**Acceptance Scenarios**:

1. **Given** any flag documented on a CLI page, **When** it is compared with the built binary's help output, **Then** name, default, and meaning match.
2. **Given** the provider list in the docs, **When** it is compared with the catalog code, **Then** the lists match exactly.
3. **Given** the telephony pages, **When** a reader follows an inbound or outbound flow, **Then** every step matches the current behavior of `unmute dev --telephony`.

---

### User Story 4 - The team trusts what shipped (Priority: P4)

The maintainers receive a final report: every page written, every place where
code and internal docs disagreed, and every claim that could not be verified.
Nothing was silently resolved.

**Why this priority**: The repo rule is that docs win over code, so a
disagreement is a decision for the maintainers, not for the docs writer.

**Independent Test**: The report exists, lists pages, discrepancies, and
unverified claims, and every discrepancy names the code location and the doc
location that disagree.

**Acceptance Scenarios**:

1. **Given** a fact where an internal doc and the code disagree, **When** the docs are written, **Then** the fact is flagged in the report and not shipped as settled.
2. **Given** the finished site, **When** the report is read, **Then** it lists every page and every unverified claim.

---

### Edge Cases

- An internal doc and the code disagree on a fact. The docs writer stops on that fact, flags it in the final report, and does not pick a side silently.
- An anchor example fails to validate or compile. Its YAML is fixed as part of this feature until it passes. If the failure traces to product Go code, it goes in the report and the example is not used as an anchor until fixed.
- An anchor example has no README. This feature writes one, and it must satisfy the existing repo tests (every example README names every transport its targets declare, and links resolve).
- A CLI flag from the requested list no longer exists in the code. It is not documented, and the removal goes in the report.
- A flag exists in the code but not in the requested list. It is documented anyway.
- A YAML snippet passes validation for one target but not the other. The page says so explicitly or the snippet is changed until it passes for the targets the page claims.
- An anchor example declares only one target. The page presents it as single-target and does not add the other target to the example or imply it works there. General examples (both targets) and specific examples (one target) each keep their declared set.
- A Mintlify component is remembered wrongly. Component syntax is checked against the current Mintlify docs index, not memory.
- The docs must never present Vapi, Deepgram, or ElevenLabs as a target or runtime, even though the codebase validates some of them. ElevenLabs may appear only as a model vendor where the catalog lists it.
- The first deploy would expose the site publicly before authentication is switched on. Rule: visibility is set to Private in the dashboard at (or immediately after) the first deploy, before the URL is shared with anyone.
- The Mintlify plan turns out not to support password authentication. The fallback is Mintlify private authentication (organization members), which every plan has; the password upgrade is noted in the report as pending a plan decision.

## Requirements *(mandatory)*

### Functional Requirements

**Verification (the non-negotiable rule)**

- **FR-001**: Every factual claim on the site MUST be verified against the code in this repo before it is written. The Go structs in `internal/spec` and `internal/ir` are the schema truth, `internal/cli` is the command truth, and `internal/target/catalog_pipecat.go` plus `internal/target/catalog_livekit.go` are the provider truth.
- **FR-002**: Every YAML snippet on the site MUST pass `./unmute validate` (run in a scratch package), and every full example the site references MUST exist in `examples/` and compile with `./unmute compile`.
- **FR-003**: CLI pages MUST match the real help output of a freshly built binary, including flag names, defaults, and argument shapes.
- **FR-004**: When an internal doc and the code disagree, the writer MUST stop on that fact and flag it in the final report instead of choosing silently.
- **FR-005**: Every code block on the site MUST be something that was actually run or validated during writing.

**Story and structure**

- **FR-006**: The navigation MUST follow this arc: Introduction (the why), Get started, a core learning narrative that adds one concept per page (one agent with a prompt, tool, variables, second agent and handoffs, tasks, task groups, subagents), then topical sections (tools, telephony, targets, transfers, deployment), then reference.
- **FR-007**: The introduction MUST make the problem land before any how-to: maintaining voice agents is painful, orchestrator code sprawls, one spec serves many runtimes.
- **FR-008**: The get-started path MUST take a new user from install to a talking agent in the browser without requiring any other page.
- **FR-009**: The learning narrative MUST never use a concept before the page that teaches it, and each narrative page MUST anchor to a real example in `examples/`.
- **FR-010**: Each page MUST say why the reader is there and where to go next.

**Content scope**

- **FR-011**: The site MUST present exactly two targets: Pipecat and LiveKit Agents. Vapi, Deepgram, and ElevenLabs MUST NOT appear as targets or runtimes anywhere on the site. ElevenLabs MAY appear as a model vendor only where the catalog lists it.
- **FR-012**: The CLI section MUST document `unmute init`, `unmute validate`, `unmute compile`, and `unmute dev` with every real flag, real defaults, and what the user sees, including: dev behavior with and without a TTY, the browser UI versus `--console`, `--var` as the local stand-in for production dispatch metadata, the full `--telephony` flow (tunnel, automatic webhook configuration, `--no-webhook` opt out, `--public-url`, outbound test calls with `--to`), exit codes, and that warnings go to stderr with exit 0.
- **FR-013**: The reference section MUST cover the CLI, the agent YAML surface, the targets YAML surface, providers, variables, and secrets, each derived from code, using the old draft only as a topic checklist.
- **FR-014**: Telephony, transfers, and deployment pages MUST be grounded in the internal docs (`docs/TELEPHONY.md`, `docs/TRANSFERS.md`, `docs/DEPLOYMENT.md`) and re-verified against code and the telephony and transfer examples.
- **FR-022**: Every example the docs anchor to MUST validate, compile, and have its own README before its page ships. This feature writes the missing READMEs (today: simple-prompt, multi-task, task-groups, subagents) and fixes example YAML where needed. New and changed READMEs MUST pass the existing repo tests (name every transport their targets declare, resolvable links). A failure that traces to product Go code goes in the report and is not fixed here. An example's declared target set is authoritative: fixes MUST NOT add or remove a target, and each docs page claims exactly the targets its anchor example declares.
- **FR-023**: The provider reference MUST state, per target and per role (STT, TTS, LLM, and VAD or turn detection), exactly which vendor integrations are available, derived from the catalog code, and MUST make the Pipecat and LiveKit differences explicit. SLNG is listed first in every list. Exact model names appear only for SLNG, linked to https://docs.slng.ai/models. For other vendors the docs say that model IDs are passed through to the vendor unchecked.

**Style**

- **FR-015**: All pages MUST use simple language and short sentences, explain each term the first time it appears, and never use em dashes or en dashes as punctuation.
- **FR-016**: Pages MUST prefer a runnable snippet plus one sentence over a paragraph of theory.
- **FR-017**: The site MUST feel like part of the same family as docs.slng.ai in tone and look, and MUST take its structural cues from the LiveKit Agents docs.
- **FR-024**: The site MUST be branded "Unmute by SLNG//" using the SLNG logo from `images/Logo_SLNG.png`: the logo appears in the navbar and as the favicon source, and the site's accent colors are taken from the logo's yellow and from docs.slng.ai. The logo file is copied into the docs project (the site must be self-contained).

**Platform and quality gates**

- **FR-018**: The site MUST be authored as MDX pages with a `docs.json` navigation that encodes the story arc, using Mintlify components where they help, with component syntax checked against current Mintlify documentation.
- **FR-019**: The finished site MUST pass `mint validate` and `mint broken-links` with zero errors introduced by this work.
- **FR-020**: The work MUST end with a report listing every page written, every code-versus-docs discrepancy found, and every claim that could not be verified.
- **FR-021**: Any authenticated Mintlify step MUST be requested from the user (`mint login`), never faked, and deployment happens only after the user approves.
- **FR-025**: The site MUST launch private. It deploys as a NEW Mintlify project on its own subdomain (authentication does not work on subpaths, so it cannot live under docs.slng.ai as a subpath), and it MUST never be connected to the existing docs.slng.ai deployment. Site visibility is set to Private in the Mintlify dashboard before any URL is shared: password authentication if the plan is Pro or Enterprise, otherwise Mintlify private authentication (organization members) until a password is possible. Going public later is a deliberate user decision, not a default.

### Key Entities

- **Docs site**: The Mintlify project: MDX pages plus a `docs.json` navigation that encodes the story arc.
- **Page**: One MDX file with one job in the story. Narrative pages teach one concept each; reference pages answer lookups.
- **Learning narrative**: The ordered core section. Its order is a contract: no page may depend on a later page.
- **Example**: A working package in `examples/` (ten exist today). The raw material every narrative page anchors to. To qualify as an anchor it must validate, compile, and carry its own README. Each example declares its own target set, general (both targets) or specific (one target), and that set is authoritative for its docs page.
- **CLI surface**: The four commands and their flags as defined in `internal/cli`, confirmed by real help output.
- **Provider catalog**: The typed entries in `internal/target/catalog_*.go`, one per framework, role, and vendor. The only legitimate source for any provider list on the site. It carries vendors, not model names; the only model names on the site are SLNG's, via the linked SLNG models page.
- **Discrepancy report**: The final deliverable to maintainers: pages written, code-versus-docs disagreements, unverifiable claims.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A reader with no prior Unmute exposure can state what Unmute is and why it exists after the introduction page alone.
- **SC-002**: A new user reaches a talking agent in the browser using only the get-started path, in under 15 minutes on a machine with the named prerequisites.
- **SC-003**: Read in order, the learning narrative introduces every concept before any page uses it, with zero forward references.
- **SC-004**: 100% of CLI flags on the site exist in the code with the same name, default, and meaning, and no documented flag is absent from the code.
- **SC-005**: 100% of YAML snippets on the site pass validation, and 100% of referenced examples compile.
- **SC-006**: The site link check and configuration check both pass with zero errors from this work.
- **SC-007**: Zero pages present anything other than Pipecat and LiveKit Agents as a target.
- **SC-008**: The final report exists and accounts for every page, every discrepancy, and every unverified claim; zero disagreements were resolved silently.
- **SC-009**: 100% of examples the docs anchor to have their own README that passes the existing repo tests, including the four that lack one today.
- **SC-010**: Every provider list on the site matches the catalog code exactly per target and role, SLNG appears first in every such list, and the only exact model names on the site are SLNG's, linked to the SLNG models page.
- **SC-011**: From first deploy onward, the site is not publicly readable: opening any page without the password (or an organization login) is refused, and the existing docs.slng.ai deployment is untouched.

## Assumptions

- The old draft under `docs/user/` stays untouched by this feature. It serves as a topic checklist only. Retiring or replacing it is a separate decision for the maintainers once the new site is live.
- Omitting Vapi and Deepgram from the user docs is a deliberate scope choice by the product owner. The project constitution permits (but does not require) validation-only tables for them; leaving them out makes no false claim, so there is no conflict.
- The site is English only, unversioned, and single-branded for v1.
- The ten examples in `examples/` are the complete anchor set for the learning narrative and topical pages; no new examples are written for the docs unless a narrative step has no anchor, in which case the gap goes in the report. Bringing existing anchors up to standard (READMEs, YAML fixes) is in scope per FR-022; product Go code changes are not.
- The examples are built on SLNG models, and SLNG is the vendor the docs lead with. The SLNG models page (https://docs.slng.ai/models) is the external source of truth for SLNG model names and may be linked from the site.
- The user can run `mint login` and connect GitHub when asked; publishing is a push to the production branch after their approval. At that step the site is created as a new Mintlify project, and the exact authentication method (password versus organization members) is picked based on what the dashboard shows the plan supports.
- Screenshots and recordings are out of scope for v1 unless a page is unclear without one.
- The docs project lives in this repo, in a new top-level directory (confirmed by the user on 2026-08-14). Docs version together with the code they describe, and snippet validation can run the locally built binary directly.

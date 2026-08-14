# Feature Specification: Mintlify Docs Extension

**Feature Branch**: `009-mintlify-docs-extension`

**Created**: 2026-08-14

**Status**: Draft

**Input**: User description: "Extend the existing Mintlify user docs site (specs/008-mintlify-user-docs) in three parts: (1) merge origin/pre-release-v1 and update all pages for two landed features, MCP tool sources (SCHEMA N40) and the breaking telephony-connection change; (2) restructure the Build the agent nav group to teach concepts instead of counting agents, with nested Tools and Orchestration groups; (3) keep all gates green and produce dated addenda to the 008 spec artifacts. All original spec constraints carry over."

## The story

The docs site built under `specs/008-mintlify-user-docs` describes the product
as of commit `ae9f8cc`. Two features have landed on `origin/pre-release-v1`
since. One adds MCP servers as a first class tool source. The other is
breaking: the connection file now owns the whole phone route, and
`targets.yaml` no longer accepts `transport`, `carrier`, or `destinations`.
A site that still shows the old shape teaches a product that no longer exists.

At the same time, the maintainer reviewed the "Build the agent" section and
asked for a change: keep the story, drop the page names that count agents
("one agent", "two agents"), and give tools and orchestration real
sub sections that go deep on one mechanism each.

This feature is an extension of 008, not a replacement. Everything in
`specs/008-mintlify-user-docs/` still binds: `spec.md` (FR-001 to FR-025),
`plan.md`, `research.md`, `data-model.md`, `contracts/`, `quickstart.md`,
`verification-log.md`, `report.md`, and the contributor rules in
`docs-site/README.md`. The deliverable addenda of this feature amend those
008 artifacts in place, dated, in the style each file already uses.

## Clarifications

### Session 2026-08-14

- Directive (maintainer): the site must make clear that a scaffolded project uses SLNG models by design, and that those models are optimized by the SLNG Execution Layer (https://docs.slng.ai/execution-layer). The execution-layer docs are the context for explaining that voice agents can be far more optimized. A Models section is added, broken into STT, TTS, LLM, and VAD/turn detection, with supported models per target inferred from the code, plus the optimization story.
- Q: How should the docs treat the SLNG Context Router (the LLM optimization stage of the Execution Layer)? → A: Exclude it entirely. The optimization story covers only the STT Performance Layer and TTS Path Optimization; the Context Router is not mentioned anywhere on the site for now.
- Q: Where does the Models section live, and what happens to the existing reference/providers page? → A: A new top-level nav group "Models" with pages for STT, TTS, LLM, VAD/turn detection, and optimization. reference/providers retires; its per-target vendor lists move into the role pages and the catalog agreement test retargets to them.
- Directive (maintainer, second round): running telephony locally must be extremely well explained, and the `dev --telephony` walkthrough moves out of the Telephony group into the Dev section. The Dev docs experience is the focus and the group gets a lifecycle framing. The going-live page must not use provider-branded env var names as examples (11LABS is provider related). Add: the dev loop mentions the log files `unmute dev` writes; go-live gains command-line guides for LiveKit Cloud and Pipecat Cloud; and a guide teaches readers how to get their telephony details from Twilio, in the spirit of LiveKit's Twilio Voice integration page, distinguishing what Pipecat and LiveKit each need.
- Q: How should the local telephony content move into the Dev section? → A: The Dev group grows its own pages: overview (the loop, logs), console, a new dev/telephony page (the local phone-call run, moved from telephony/overview), and telephony/webhooks-and-tunnels moves in as dev/webhooks-and-tunnels. The Telephony group keeps concepts, routes, and provider setup.
- Q: How should the go-live guides for LiveKit Cloud and Pipecat Cloud be structured? → A: One page per platform: deploy/livekit-cloud and deploy/pipecat-cloud, each a command-line walkthrough for deploying the generated project, with deploy/going-live staying as the shared pre-launch checklist.
- Q: How should the guide for getting telephony details from the Twilio console be shaped? → A: One provider page, telephony/twilio, with separate sections for the Pipecat routes and the LiveKit route, mapping each console value to the connection env name that holds it, verified against current Twilio and platform docs with dates.
- Q: What should the Dev navigation group be called? → A: "Development lifecycle". Page slugs stay `dev/*`.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - The site describes the merged product (Priority: P1)

A reader lands on any page after the merge of `origin/pre-release-v1`. What
they read matches the binary they just built. No page shows `transport`,
`carrier`, or `destinations` as fields of a target. The connection file is
documented as the whole phone route, with its three valid shapes. The
`secrets:` page states the new rule: it lists every environment name the
author wrote, and not the names the driver or platform supplies. The
quickstart transcript matches a freshly run `unmute init` against the merged
scaffold.

**Why this priority**: A wrong page is worse than a missing page. Writing or
restructuring anything before the merge would document a product that is
about to stop existing.

**Independent Test**: Build the merged binary, then grep the site for the
moved fields and read each telephony, transfer, target, and reference page
against the merged code and the two feature specs that landed with it.

**Acceptance Scenarios**:

1. **Given** the merged code, **When** the site is grepped for `transport:`, `carrier:`, or `destinations:` shown inside a `targets.yaml` snippet, **Then** there are zero hits.
2. **Given** the quickstart page, **When** `unmute init` is re-run with the same inputs, **Then** the transcript on the page matches the fresh run.
3. **Given** the `secrets:` reference page, **When** it is compared with the merged environment contract, **Then** the page states which names the author lists and which names the platform supplies, and both lists match the contract.
4. **Given** any refusal message quoted on a new or changed page, **When** the input that provokes it is run, **Then** the message on the page matches the real output.
5. **Given** a page that mentions `kind: telephony` in `agent.yaml` channels, **When** it is read against the merged schema, **Then** that usage is intact, because only connection files dropped `kind:`.

---

### User Story 2 - An author learns MCP tool sources (Priority: P2)

An author who wants their agent to call tools from an MCP server finds a
dedicated page. It teaches the `mcp:` block shape, anchored on
`examples/mcp-example`. It explains the rule authors get wrong: an MCP tool
file carries no tool contract, because the server announces its own tools at
run time. It shows that `tools:` is a selection filter whose names are not
checked against a live server, that two files may name the same `url_env` as
independent sources, and that assignment scopes the source exactly like any
other tool.

**Why this priority**: This is the larger of the two new features on the
authoring surface, and the only one with a brand new example to anchor on.
It depends on User Story 1 only for the merge itself.

**Independent Test**: Validate and compile `examples/mcp-example`, then read
the page against `specs/008-mcp-tool-sources/contracts/` and the merged
capability table. Trigger one contract-field refusal and compare it with the
page.

**Acceptance Scenarios**:

1. **Given** `examples/mcp-example`, **When** it is validated and compiled with the merged binary, **Then** both pass and the page cites only what those runs show.
2. **Given** an `mcp:` tool file that also declares `description` or `input`, **When** it is validated, **Then** the refusal names the field, the file, and the line, and the page quotes a real capture of it.
3. **Given** the capability claim on the page, **When** it is compared with the merged capability table, **Then** the page claims exactly what the table says for each target.
4. **Given** the env names in the example's `mcp:` block, **When** the example is compiled, **Then** those names appear in `.env.example` and the compile report as the page describes.

---

### User Story 3 - The Build section teaches concepts (Priority: P3)

A learner opens "Build the agent" and sees concepts, not a headcount. The
first page is `build/your-first-agent`. A nested Tools group holds an
overview plus one page per way a tool can run: webhook, python, mcp,
prebuilt. `build/variables` stays where the story needs it. A nested
Orchestration group holds an overview, handoffs (the old two-agents content
reframed), tasks, task-groups, and choosing-a-structure as a genuine
decision aid: symptom in one column, the shape that fixes it in the other,
with the cost of each shape stated.

**Why this priority**: The restructure is the maintainer's explicit ask, but
doing it before the merge would restructure pages that are about to change
content.

**Independent Test**: Read the group in order. No page uses a concept before
the page that teaches it. Every tools page maps to an execution block that
exists in the merged authoring schema, and every orchestration page maps to
a construct that exists in the code. The navigation on disk matches the
navigation file page for page.

**Acceptance Scenarios**:

1. **Given** the restructured navigation, **When** its pages are compared with the files on disk, **Then** the counts and paths match exactly, and the Tools and Orchestration groups are nested the same way the existing Reference group nests CLI.
2. **Given** each tools sub page, **When** its subject is checked against the authoring schema, **Then** the execution block it teaches exists in the code, and no page teaches a block the code does not have.
3. **Given** the prebuilt tools page, **When** it is compared with the registry, **Then** the page says plainly that the registry is closed and names exactly its current entries, with no implied catalog.
4. **Given** the choosing-a-structure page, **When** a reader arrives with a symptom (one prompt doing too many jobs, a result that must come back, context that must be shared), **Then** the page names the shape that fixes it and what that shape costs.
5. **Given** the old page paths, **When** the site is checked for links to them, **Then** no internal link points at a path that no longer exists.

---

### User Story 4 - The maintainers trust what changed (Priority: P4)

The maintainers receive dated addenda to the 008 artifacts: a navigation
amendment saying what moved and why, a verification log addendum listing
every scratch package and captured run, a report addendum with the new page
list, a re-check of discrepancies D1 to D5 against the merged code, any new
disagreement found, and every claim that could not be verified. A new dated
phase in `tasks.md` tracks the work the way the first 56 tasks were tracked.
Nothing was resolved silently, and nothing was deployed.

**Why this priority**: Same reason as 008's User Story 4. The honesty trail
is what makes the site maintainable, but it can only be written after the
work it describes.

**Independent Test**: The addenda exist in the named files, follow each
file's existing style, and every D1 to D5 entry carries a verdict: stands,
stale, or changed, with the evidence named.

**Acceptance Scenarios**:

1. **Given** the report addendum, **When** D1 to D5 are read, **Then** each has been re-checked against the merged code and carries a verdict with evidence, and no product code or product doc was changed to settle one.
2. **Given** the verification log addendum, **When** any claim on a new or changed page is questioned, **Then** the log names the scratch package or command run that backs it.
3. **Given** the finished work, **When** the repository and any Mintlify account are inspected, **Then** nothing was deployed, no project was created, and no URL exists.

---

### User Story 5 - A reader sees the models and what makes them fast (Priority: P5)

A reader wants to know which models each target supports. A top-level Models
group answers by role: STT, TTS, LLM, and VAD/turn detection, each page
listing the vendor integrations per target straight from the code, SLNG
first, with exact model names only for SLNG. An optimization page explains
that a scaffolded project uses SLNG models by design, and that those models
run on the SLNG Execution Layer: the STT Performance Layer and TTS Path
Optimization, with every optimization claim attributed to the SLNG docs and
linked. The Context Router is not mentioned anywhere.

**Why this priority**: The positioning matters to the maintainer, but the
pages restate facts other pages already hold behind an agreement test, so
they come after the merge accuracy and the restructure are safe.

**Independent Test**: Compare each role page's vendor lists with the merged
catalog per target per role; the retargeted agreement test passes and fails
when a list is edited to lie. Grep the site for the Context Router: zero
hits. Every optimization claim on the site names SLNG's docs as its source.

**Acceptance Scenarios**:

1. **Given** any Models role page, **When** its vendor lists are compared with the merged catalog for that role and target, **Then** they match exactly, SLNG first, and only SLNG entries carry model names.
2. **Given** the optimization page, **When** its claims are checked, **Then** each is attributed to the SLNG execution-layer docs with a link, none is presented as measured by this work, and only the STT and TTS stages appear.
3. **Given** the finished site, **When** it is grepped for the Context Router, **Then** there are zero mentions, and `reference/providers` no longer exists as a page while the agreement test guards the role pages instead.
4. **Given** the quickstart or the page that shows the scaffold, **When** a reader finishes it, **Then** they have been told the scaffolded project uses SLNG models by design and where the optimization story lives.

---

### User Story 6 - A developer lives in the dev loop (Priority: P6)

A developer building an agent works from one group: Development lifecycle.
The overview teaches the loop: change the package, `unmute dev`, talk to the
agent, and read the log files the run writes to see how the agent behaved.
The console page stays. A dev/telephony page, moved from the Telephony
overview, explains extremely well how a real phone call runs locally and how
that happens under the hood: the tunnel, the automatic webhook, the local
run. The webhooks-and-tunnels page moves into the group, because it is local
dev mechanics. The Telephony group keeps concepts, routes, and provider
setup, and links to the Development lifecycle for the local run.

**Why this priority**: The maintainer's second-round focus, but it moves
pages US1 corrects, so it lands with the restructure work, after merge
accuracy.

**Independent Test**: Read the Development lifecycle group top to bottom: the
loop, the logs (locations matching what the code writes, verified per dev
mode), the console, the local phone call, the tunnel mechanics. Grep the
Telephony group for the local-run walkthrough: it is gone, replaced by a
link.

**Acceptance Scenarios**:

1. **Given** the sidebar, **When** the reader opens Development lifecycle, **Then** it holds overview, console, telephony, and webhooks-and-tunnels, and telephony/overview no longer walks through `dev --telephony`.
2. **Given** the overview's statement about logs, **When** `unmute dev` runs in each documented mode, **Then** the log file locations and names on the page match what the run actually writes.
3. **Given** the dev/telephony page, **When** a reader follows it, **Then** they can run a real local phone call and can say how it happened (tunnel, webhook, local process), with every step matching the merged code.

---

### User Story 7 - An operator goes live and wires the phone number (Priority: P7)

An operator with a working local agent opens Deployment and finds a
command-line walkthrough for their platform: deploy/livekit-cloud for the
LiveKit Agents target and deploy/pipecat-cloud for the Pipecat target, each
taking the generated project to that platform's cloud using the platform's
own CLI, clearly separated so the two paths never blur. The going-live
checklist remains the shared capstone, and its environment-variable naming
rule no longer uses a provider-branded name as its example. A telephony/twilio
page teaches how to get the needed details from the Twilio console (buying a
number, account SID, auth token) and which connection env name each value
fills, per route, per target.

**Why this priority**: Going live is the last step of the story arc, and
these pages depend on the connection reference (US1) and the dev loop pages
(US6) existing first.

**Independent Test**: Follow each platform guide against the generated
project for that target; every command shown was run, or is attributed to the
platform's current docs with a fetch date and listed as unverified in the
report. Grep the site for provider-branded env names used as invalid
examples: zero. The Twilio page names, for each route, exactly the env names
the connection files declare.

**Acceptance Scenarios**:

1. **Given** either platform guide, **When** its commands are checked, **Then** each was actually run, or carries an attribution to the platform's current docs with a date, and the report addendum lists it as unverified.
2. **Given** the going-live checklist and every other page, **When** the site is grepped, **Then** no provider-branded env var name (such as one starting with a vendor's name) is used as the example of an invalid or placeholder name; the shell-identifier rule itself stays, with a neutral example.
3. **Given** the telephony/twilio page, **When** its env name mapping is compared with the connection files of the telephony examples, **Then** every name matches, per route, per target, and the page never claims Unmute buys numbers or creates carrier resources.

---

### Edge Cases

- The merge brings test failures or stale goldens. They are signals, not blockers: each one names a doc or page that now lies, and is fixed as part of this work. Product code is not changed to make a doc true.
- A proposed page names a concept the code does not have (the 008 lesson from D1, `build/subagents`). The page is not written. The gap and the reasoning go in the report addendum, exactly as D1 was handled.
- `kind:` was removed from connection files but not from `agent.yaml` channels. A careless sweep would delete the wrong one. Every `kind:` mention is checked against which file the snippet shows.
- `examples/outbound-reminder` split one connection into two because one connection cannot serve two targets whose transports differ. The docs use this as a teaching moment, and its page must match the new file names.
- The restructure moves CLI or build pages that the help-capture agreement test maps flags onto. The test's mapping moves with the pages. The test is updated, never weakened.
- The gated-transfer and unused-route refusals are new or changed on the merged branch. Any of them quoted on a page is triggered first, never paraphrased from the contract.
- The old build page paths have never been deployed, so no external link can break. Redirects in the navigation config are a nice to have, not a requirement.
- The merged capability table may say something different from the feature spec prose (for example the exact `mcp` gate status per target). The table wins, and a mismatch goes in the report addendum as a new discrepancy.
- The catalog carries vendors, never model names (constitution rule). So the Models role pages list vendor integrations per target, and the only exact model names printed are the SLNG ids already proven in this repository, with https://docs.slng.ai/models linked for the rest. "Infer the models from the code" means the vendor lists and the proven SLNG ids, not a scraped model catalog.
- The execution-layer numbers (SLNG's own latency and cost figures) are marketing claims this work cannot measure. They appear only as attributed, linked statements from the SLNG docs, fetched and dated during writing, never as facts this site asserts itself.
- VAD and turn detection are not the same mechanism on both targets. The role page says what each target actually uses, from the catalog, instead of blending them.
- Retiring `reference/providers` breaks its inbound links and orphans its agreement test. Every page linking to it is re-pointed, and the test is retargeted to the role pages in the same change, updated and never weakened.
- The log files `unmute dev` writes differ by mode (the code shows a `telephony.log` in the build directory for phone routes, a compose log path, and a `--verbose` flag whose default is "write to the log file only"). The dev pages state only the locations each mode actually writes, verified per mode, never one blanket claim.
- The branded name `11LABS_API_KEY` is currently the invalid-name example on three pages (going-live, first-phone-call, secrets), not one. The sweep fixes every occurrence with a neutral invalid name, and the example must still be a name the validator really rejects.
- The pasted LiveKit Twilio integration page is shape and tone inspiration only. Unmute's LiveKit route may not use TwiML Bins or manual trunk JSON at all (the dev flow creates local trunks automatically); the telephony/twilio page documents the routes as this repo's code and TELEPHONY.md define them, not LiveKit's generic path.
- The platform go-live walkthroughs may include steps that cannot be executed here (a real cloud deploy needs an account and would publish an agent). Any step not actually run is attributed to the platform's current docs with a fetch date and listed in the report addendum as unverified, never presented as proven.
- Moving `telephony/webhooks-and-tunnels` to `dev/webhooks-and-tunnels` breaks inbound links the same way the Build moves do; the same link sweep rule applies.

## Requirements *(mandatory)*

Numbering continues from 008 (FR-001 to FR-025), because those requirements
still bind this work.

### Functional Requirements

**Carry over**

- **FR-026**: Every constraint of `specs/008-mintlify-user-docs/spec.md` MUST hold for every page this feature adds or changes: claims verified against code, snippets validated in scratch packages, command outputs actually run, exactly two targets, plain language with no em or en dashes as punctuation, provider lists derived from the catalog with SLNG first, example target sets never changed, telephony routes presented as `provisional`, disagreements reported and never settled in passing, and nothing deployed.

**Merge first**

- **FR-027**: The work MUST start by committing the current untracked docs work, then merging `origin/pre-release-v1`, rebuilding, and running the test and lint gates. Every page written after that point MUST describe the merged product. The two feature specs that landed with the merge (`specs/008-mcp-tool-sources/` and `specs/008-simplify-telephony-connections/`) are the authoritative statement of what changed, and their contracts MUST be read before any page is edited.

**MCP tool sources**

- **FR-028**: The site MUST teach MCP tool sources on a dedicated page anchored on `examples/mcp-example`, which MUST validate and compile before it is cited. The page MUST cover: the `mcp:` block shape (`url_env` required as an UPPER_SNAKE env name, optional `transport`, `auth`, and `tools`), the rule that an `mcp:` file carries no tool contract and why (the server announces its own tools at run time), that each contract field fails at load individually with file and line, that `tools:` is an unchecked selection filter, that two files may share a `url_env` as independent sources, and that assignment scopes the source like any other tool. The capability status per target MUST be read from the merged capability table, not from the feature spec prose. The page MUST show, from a real compile, that every env name in the block reaches `.env.example`, the startup check, and the compile report.

**Connections own the route**

- **FR-029**: The site MUST document the connection file as the whole phone route. The target reference MUST list exactly the fields the merged schema accepts and MUST state that `transport`, `carrier`, and `destinations` are rejected there, with the refusal naming each field's new home. A new reference page for the connection file MUST exist as a sibling of the target reference, covering the three valid shapes (full route, receive-only without credentials, and carrier-less via the transport that provisions its own number), that `kind:` is no longer written in a connection file, and how one target names one connection. The merged internal reference doc is a starting point for it, never copy to paste.
- **FR-030**: Every page that showed `transport`, `carrier`, or `destinations` on a target MUST be updated, found by a fresh grep after the merge rather than by the pre-merge checklist alone. `destinations:` MUST be documented in its new home in `agent.yaml`, accepting only the UPPER_SNAKE name of an environment variable, with the literal-value refusal shown from a real run. Mentions of `kind: telephony` under `agent.yaml` channels MUST be left intact.
- **FR-031**: The `secrets:` reference MUST state the merged rule: the block lists every environment name the author wrote (connection values, destination values, model and tool credentials) and does not list names the driver or platform supplies. The two lists MUST match the merged environment contract, verified against a real build.
- **FR-032**: The quickstart transcript and every scaffolded file shown on the site MUST be re-captured from a fresh `unmute init` run against the merged scaffold.

**Build section restructure**

- **FR-033**: The "Build the agent" group MUST be restructured to: `build/your-first-agent`, a nested Tools group (`overview`, `webhook`, `python`, `mcp`, `prebuilt`), `build/variables`, and a nested Orchestration group (`overview`, `handoffs`, `tasks`, `task-groups`, `choosing-a-structure`), using the same nesting pattern the Reference group already uses. This shape is a proposal: if the code says otherwise for any page, the shape is adjusted and the adjustment and its reason go in the report addendum. A page ships only if the code has the concept.
- **FR-034**: The Tools overview MUST teach the shared vocabulary once: what a tool is, the ways one can run as named by the authoring schema's execution blocks, a table routing the reader to the right sub page, the two behaviour fields, and how assignment scopes a tool. The webhook page MUST cover the everyday case (`url_env`, `path` rendering, `auth`, `input` and `output`, `inject` and why the model cannot see or overwrite it), anchored on a real example. The python page MUST cover the local block and its handler: where the handler lives in the generated project, what it receives and returns, and the environment as the credential seam, with any shown handler code actually run and checked with the repository's Python linters. The mcp page is FR-028. The prebuilt page MUST say plainly that the registry is closed and name exactly its current entries and their fixed behaviour, and the gated execution blocks MUST be mentioned wherever the capability table currently places them.
- **FR-035**: The Orchestration overview MUST name the shapes and route onward without teaching, so nothing is used before it is taught. Handoffs MUST reframe the two-agents content as a concept: the handoff does not come back, context carries history and variables, tool lists are the guardrail. Tasks MUST cover delegation, typed results, assignment into a variable, and what the agent cannot do while delegated. Task-groups MUST keep the real captured warning the current page holds. Choosing-a-structure MUST become a genuine decision aid: symptom paired with the shape that fixes it, the cost of each shape, the guidance to grow tools before adding a second agent, and the delegate versus transfer distinction.
- **FR-036**: The restructure takes only the shape of the named external inspiration docs (an overview that orients, sub pages that go deep, a decision aid that names symptoms), never their content. A concept that exists there but not in this code MUST NOT become a page.

**Models and optimization**

- **FR-040**: The site MUST have a top-level Models group with one page per role: STT, TTS, LLM, and VAD/turn detection, plus an optimization page. Each role page lists the vendor integrations per target derived from the merged catalog, SLNG first, exact model names only for SLNG (the ids proven in this repository), linked to https://docs.slng.ai/models, and states that other vendors' model ids pass through unchecked. `reference/providers` retires into these pages: its inbound links are re-pointed and the catalog agreement test is retargeted to the role pages in the same change, updated and never weakened.
- **FR-041**: The optimization page MUST explain, from the SLNG execution-layer docs (https://docs.slng.ai/execution-layer and its how-it-works, adaptive, stt-performance-layer, and tts-path-optimization pages, fetched and dated during writing), that SLNG models run on the SLNG Execution Layer and that this is how voice agents get faster and cheaper. Only the STT Performance Layer and TTS Path Optimization are covered. The Context Router MUST NOT be mentioned anywhere on the site. Every optimization claim is attributed to SLNG's docs with a link; none is presented as measured by this work.
- **FR-042**: The site MUST tell the reader, where the scaffold is first shown, that a project created by `unmute init` uses SLNG models by design and that those models are optimized by the SLNG Execution Layer, with a link onward to the Models group. The statement MUST stay truthful to the scaffold: it describes the default, never a constraint.

**Development lifecycle and go live**

- **FR-043**: The Dev group MUST be renamed "Development lifecycle" (slugs stay `dev/*`) and grow to four pages: overview, console, telephony, and webhooks-and-tunnels. The overview MUST teach the loop and state where each dev mode writes its log files, with every location verified against a real run of that mode. The local phone-call walkthrough MUST move from `telephony/overview` to `dev/telephony` and explain how the local run happens (tunnel, automatic webhook, local process), and `telephony/webhooks-and-tunnels` MUST move to `dev/webhooks-and-tunnels`. The Telephony group keeps concepts, routes, and provider setup, linking to the Development lifecycle for the local run.
- **FR-044**: No page on the site may use a provider-branded environment variable name as an example of an invalid or placeholder name. The shell-identifier rule stays, illustrated with a neutral name the validator really rejects. The known occurrences (going-live, first-phone-call, secrets) are fixed and a site-wide sweep confirms no others.
- **FR-045**: The Deployment group MUST gain one command-line go-live guide per platform: `deploy/livekit-cloud` and `deploy/pipecat-cloud`, each taking that target's generated project to its cloud using the platform's own CLI, grounded in the generated project's README, `docs/DEPLOYMENT.md`, and the platform's current official docs (fetched and dated). Every command shown was run, or is attributed with a date and listed as unverified in the report addendum. `deploy/going-live` stays as the shared pre-launch checklist. The guides deploy the reader's agent; this feature still deploys nothing itself and creates no accounts.
- **FR-046**: A `telephony/twilio` page MUST teach how to get telephony details from the Twilio console: buying a number, finding the account SID and auth token, and which connection env name each value fills, in separate sections for the Pipecat routes and the LiveKit route as this repo's code defines them. Console steps are verified against current Twilio docs with a fetch date. The page MUST NOT claim Unmute buys numbers or creates carrier resources, and MUST stay consistent with what `dev --telephony` automates restorably.

**Gates and honesty**

- **FR-037**: All gates MUST pass before done is claimed: the Go test suite, lint, formatting, the site configuration check, the site link check, the site serving locally, every example validating and compiling (including the new one), every example having a README, zero em or en dashes in the site, no Vapi presented as a target, and the navigation matching the files on disk. The three agreement tests MUST stay green: help capture (its flag mapping moving with any moved page, updated and never weakened), the catalog test retargeted to the Models role pages (FR-040), and example READMEs against declared transports as redefined on the merged branch.
- **FR-038**: The work MUST end with dated addenda in the 008 artifacts, each in the style its file already uses: a navigation amendment recording the new structure and why, a verification log addendum listing scratch packages, commands, and captures, a report addendum with the new page list and count, a re-check of D1 to D5 each carrying a verdict with evidence, any new code-versus-doc disagreement, and every claim that could not be verified, plus a new dated phase in the 008 task list tracked item by item. The contributor rulebook MUST be updated if the rules or the test list changed.
- **FR-039**: The feature MUST NOT: deploy anything or create any docs project or share any URL, add or remove a target from an example, document a concept the code does not have, fix product code or product docs in passing, paste an output that was not run, or use an em or en dash as punctuation.

### Key Entities

- **Connection**: The authoring file that owns one whole phone route. Three valid shapes. Named by a target; the target no longer carries route fields.
- **MCP tool source**: A tool file whose execution block points at an MCP server through an environment name. Carries no tool contract; may filter which server tools are offered.
- **Execution block**: The authoring schema's answer to "how does this tool run". The tools sub pages map onto these blocks one to one, and only blocks that exist get pages.
- **Nested navigation group**: A group inside a group in the site navigation, already precedented by Reference containing CLI. Tools and Orchestration become the second and third.
- **Addendum**: A dated, append-only extension to an existing 008 artifact. Never rewrites what it extends.
- **Discrepancy verdict**: The re-check outcome for each of D1 to D5: stands, stale, or changed, with the evidence named.
- **Models role page**: One page per catalog role (STT, TTS, LLM, VAD/turn detection) in the Models group, carrying the per-target vendor lists the catalog holds, guarded by the retargeted agreement test.
- **Execution Layer**: SLNG's optimization system, documented at docs.slng.ai. On this site only two of its stages exist: the STT Performance Layer and TTS Path Optimization. Its facts are external: fetched, dated, attributed, and linked, never asserted as this site's own.
- **Development lifecycle group**: The renamed Dev group, home of the whole local loop: overview (loop and log files), console, telephony (the local phone-call run), webhooks-and-tunnels.
- **Platform go-live guide**: One page per platform (LiveKit Cloud, Pipecat Cloud) walking the generated project to production through that platform's own CLI, with unexecutable steps attributed and dated.
- **Provider telephony guide**: The telephony/twilio page: where each needed value lives in the Twilio console and which connection env name holds it, per route, per target.

## Success Criteria *(mandatory)*

Numbering continues from 008 (SC-001 to SC-011).

### Measurable Outcomes

- **SC-012**: Zero pages show `transport`, `carrier`, or `destinations` as fields of a target after the merge, proven by a fresh grep of the site.
- **SC-013**: The MCP page exists, anchored on an example that validates and compiles, and states the no-contract rule with a real captured refusal.
- **SC-014**: The connection reference page exists, documents all three valid shapes, and each shape shown is backed by a validated scratch package or example.
- **SC-015**: The quickstart transcript matches a freshly run scaffold byte for byte where the page quotes it.
- **SC-016**: The Build group contains nested Tools and Orchestration groups, every page in the navigation exists on disk and the counts match, and each concept from the old flat pages still has exactly one home.
- **SC-017**: 100% of the gates pass, and the three agreement tests are green on the merged code.
- **SC-018**: The report addendum re-checks all five discrepancies with a verdict and evidence for each, and lists every unverifiable claim; zero disagreements were settled silently.
- **SC-019**: 100% of refusals quoted on new or changed pages were triggered, and 100% of new snippets pass validation.
- **SC-020**: Nothing was deployed: no docs project exists, no URL was created or shared.
- **SC-021**: Every Models role page matches the merged catalog exactly per target and role, SLNG first, only SLNG model names, and the retargeted agreement test proves it (passes, and fails when a list is edited to lie).
- **SC-022**: Zero mentions of the Context Router anywhere on the site, `reference/providers` no longer exists as a page, and no internal link points at it.
- **SC-023**: 100% of execution-layer claims on the site are attributed to the SLNG docs with a link and a fetch date in the verification log; zero are presented as measured by this work.
- **SC-024**: The Development lifecycle group holds the four pages, every log-location claim matches what a real run of that mode wrote, and the Telephony group contains no local-run walkthrough, only a link.
- **SC-025**: Zero provider-branded env var names are used as invalid or placeholder examples anywhere on the site, proven by a sweep, and the shell-identifier rule still appears with a neutral example that really fails validation.
- **SC-026**: Both platform go-live guides exist, and 100% of their commands were either run or carry a dated attribution plus an entry in the report addendum's unverified list.
- **SC-027**: The telephony/twilio page's env name mapping matches the telephony examples' connection files exactly, per route, per target, and makes no carrier-provisioning claim.

## Assumptions

- This extension's spec lives in its own feature directory (`specs/009-mintlify-docs-extension/`), while its deliverable addenda amend the 008 artifacts in place, as the brief directs. The 008 directory stays the home of the site's living artifacts (report, verification log, task list, navigation contract).
- The merge from `origin/pre-release-v1` is expected clean because this branch has no committed changes beyond `ae9f8cc`; failures after it are expected and are part of the work, not a reason to stop.
- The proposed Build group shape is adjustable where the code disagrees, with the adjustment recorded in the report addendum. The maintainer's proposal is the default.
- Navigation redirects for the old page paths are optional, because the site has never been deployed and no external link exists.
- The three standing unverified claims from 008 (no spoken turn completed, SLNG model ids proven from this repository only, no live phone call made) are assumed to still stand and are restated in the report addendum unless this work proves otherwise.
- A third agreement test holding the set of execution blocks against the tools pages is worth considering if those pages end up asserting that set; it is a judgment call during implementation, not a requirement.
- The external inspiration pages are shape references only. Their names for concepts never override this product's names.
- The SLNG execution-layer docs (fetched 2026-08-14) name three stages: STT Performance Layer, Context Router, TTS Path Optimization. The maintainer excluded the Context Router from this site on 2026-08-14; including it later is a deliberate future decision, not a gap.
- The Models group's exact sidebar slot and page slugs are decided in the navigation contract update, defaulting to a topical group near Targets; the spec fixes the set of pages (four roles plus optimization), not the slugs.
- The plan artifacts predate the two clarification rounds. The page inventory is now 49: the original 41, plus the Models group (five new, `reference/providers` retired), plus `dev/telephony`, `telephony/twilio`, `deploy/livekit-cloud`, and `deploy/pipecat-cloud` (`webhooks-and-tunnels` moves without changing the count). The plan, contracts, and tasks are updated to match before implementation.
- The go-live guides describe how the reader deploys their own agent. Whether a real cloud deploy can be executed during writing depends on credentials being available in this environment; if not, those steps ship attributed and dated, and the report addendum says so. The docs site itself still deploys nowhere.

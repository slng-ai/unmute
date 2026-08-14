# Feature Specification: MCP servers as tool sources

**Feature Branch**: `008-mcp-tool-sources`

**Created**: 2026-08-13

**Status**: Draft

**Input**: User description: "MCP (Model Context Protocol) servers as a tool source in the YAML spec, compiling to both pipecat and livekit targets. Support SSE and Streamable HTTP transports (remote MCP servers), with authentication headers, and optional tool filtering (user picks specific tools; default is all tools). MCP tool sources can be assigned to specific tasks or entire agents, same as regular tools. Deliverable includes an examples/mcp-example that compiles to both pipecat and livekit and runs over WebRTC via unmute dev (no telephony), verified by copying credentials from repo root into the compiled target and running it with no errors."

## Why this feature exists

The schema already reserves an `mcp:` block on a tool file, but the block holds
one field: the server address. That is not enough to use a real MCP server.
Real servers sit behind authentication. They speak one of two remote
transports, SSE or streamable HTTP, and the right one is not always guessable.
And they expose many tools at once, often far more than one voice agent should
carry into a conversation, so the author needs to pick.

Today the two shipped targets are also uneven. LiveKit mounts an MCP server
address; Pipecat is maturity-gated and emits nothing. A package that leans on
MCP is therefore not portable, which is the whole promise of the tool.

This feature closes both gaps. An MCP server becomes a first-class tool
source: declared once in the package with its address, transport,
authentication, and tool selection, assigned to agents or tasks exactly like
any other tool, and emitted by both shipped drivers.

One shape change rides along. An MCP tool file today inherits the top-level
`description` and `input` contract that webhook and local tools carry, but no
driver reads them: the server itself owns each tool's name, description, and
parameters, and it announces them at runtime. A declared contract nobody reads
is a silent lie, so this feature removes it from MCP files rather than keeping
it green.

## Clarifications

### Session 2026-08-13

- Q: Which MCP server does the example use, and what does the demo do? → A:
  The Firecrawl MCP server over streamable HTTP at
  `https://mcp.firecrawl.dev/v2/mcp`, authenticated with a bearer token from
  `FIRECRAWL_API_KEY`. The demo interaction is a spoken web search. The
  operator provides the API key, and it lives with the other credentials at
  the repository root so the copy-and-run flow stays one step.
- Q: Should the example expose only the search tool or all Firecrawl tools? →
  A: Search only. The example selects the search tool by name, so the example
  also demonstrates tool selection.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Connect an agent to a remote MCP server (Priority: P1)

An author writes one tool file that names a remote MCP server by an
environment variable, lists that tool source in an agent's `tools:` list,
compiles the package, and the running agent can call the server's tools during
a live voice conversation on either shipped target.

**Why this priority**: This is the core value. Without a working end-to-end
path on both targets, nothing else in this feature matters.

**Independent Test**: Author a minimal package with one agent and one MCP tool
source pointing at a public MCP server. Compile to each target, run each over
WebRTC, and speak a request that only an MCP tool can answer.

**Acceptance Scenarios**:

1. **Given** a package with an MCP tool source assigned to an agent,
   **When** it is compiled to the pipecat target, **Then** the artifact is
   generated, starts without errors, and the agent answers a spoken request by
   calling a tool from the MCP server.
2. **Given** the same package, **When** it is compiled to the livekit target,
   **Then** the same conversation works with no package edits.
3. **Given** either compiled target, **When** the author runs it in local dev
   mode over WebRTC, **Then** the agent greets, hears the author, calls an MCP
   tool, and speaks an answer built from the tool's result.

---

### User Story 2 - Pick which tools the agent sees (Priority: P2)

An MCP server exposes many tools. The author lists the names of the tools the
agent should see. When no list is given, every tool the server exposes is
available.

**Why this priority**: Voice agents degrade when the model carries a large
tool set. Selection is what makes a big server usable in a voice call, and it
was asked for explicitly.

**Independent Test**: Point a package at a server that exposes several tools,
select a subset by name, and confirm the model is offered exactly that subset.
Remove the selection and confirm every tool is offered.

**Acceptance Scenarios**:

1. **Given** an MCP tool source that selects two named tools from a server
   exposing more, **When** the package runs, **Then** the model is offered
   exactly those two tools from that server.
2. **Given** an MCP tool source with no selection, **When** the package runs,
   **Then** every tool the server exposes is offered.

---

### User Story 3 - Authenticate to a protected server (Priority: P2)

Most useful MCP servers need a credential. The author declares the
authentication scheme on the MCP tool source, with the secret named by an
environment variable, never pasted as a value.

**Why this priority**: Without authentication the feature only works against
open demo servers. It was asked for explicitly.

**Independent Test**: Declare an MCP tool source with bearer authentication.
Confirm the generated project sends the credential from the named environment
variable, that the package and the compile report never show a secret value,
and that a pasted literal value fails validation.

**Acceptance Scenarios**:

1. **Given** an MCP tool source with an authentication block naming a token
   environment variable, **When** the package compiles, **Then** the generated
   project reads the token from that variable at run time and sends it on
   every request to the server.
2. **Given** a package whose authentication field holds a literal secret value
   instead of an `UPPER_SNAKE` environment variable name, **When** it is
   validated, **Then** validation fails before any artifact is written.
3. **Given** a compiled target whose named environment variable is missing at
   startup, **When** the project starts, **Then** the startup check names the
   missing variable.

---

### User Story 4 - Choose the transport (Priority: P3)

Remote MCP servers speak SSE or streamable HTTP. The author can state which
one, and when nothing is stated the transport is picked from the server URL
the way each platform expects, so the common case needs no extra field.

**Why this priority**: Both transports were asked for. Most servers work with
the default, so the explicit override matters less often.

**Independent Test**: Compile one package with an explicit SSE transport and
one with an explicit streamable HTTP transport, and confirm each generated
project connects with the declared transport on both targets.

**Acceptance Scenarios**:

1. **Given** an MCP tool source with no transport stated, **When** the package
   compiles, **Then** the generated project uses each platform's own default
   for the server URL.
2. **Given** an MCP tool source that states a transport, **When** the package
   compiles, **Then** the generated project uses exactly that transport on
   both targets.
3. **Given** a transport value outside the supported set, **When** the package
   loads, **Then** loading fails with the file, line, and the allowed values.

---

### User Story 5 - Scope MCP tools to a task or an agent (Priority: P3)

MCP tool sources ride the same assignment surface as every other tool: an
agent's `tools:` list or a task's `tools:` list. Listing the source name
attaches its selected tools to that scope and nothing else.

**Why this priority**: This is expected consistency rather than new surface.
It must work, but it reuses an existing mechanism.

**Independent Test**: Assign an MCP tool source to one task inside an agent.
Confirm the model is offered the MCP tools only while that task runs, and
never in scopes that do not list the source.

**Acceptance Scenarios**:

1. **Given** an MCP tool source listed on a task, **When** the conversation is
   inside that task, **Then** the MCP tools are offered; **When** it is
   outside, **Then** they are not. On pipecat this combination is refused at
   validation instead, naming the target and the reason (see FR-005).
2. **Given** an MCP tool source listed on an agent, **When** that agent is
   active, **Then** the MCP tools are offered alongside the agent's other
   tools.

---

### User Story 6 - Learn from a runnable example (Priority: P2)

A new author opens `examples/mcp-example`, reads its README, compiles it to
both targets, copies the credentials that already live at the repository root
(the voice-stack keys plus `FIRECRAWL_API_KEY`), runs local dev mode over
WebRTC, and asks the agent to search the web. The agent answers with fresh
results fetched through the Firecrawl MCP server. No telephony is involved.

The example exercises the feature's whole surface in one small package:
streamable HTTP transport, bearer authentication from an environment variable,
and a tool selection that names only the search tool.

**Why this priority**: The example is the named deliverable and the proof that
the feature works end to end on both targets.

**Independent Test**: Follow the example README from a clean checkout with the
root credentials present, on each target in turn, and reach a working
conversation with no errors displayed.

**Acceptance Scenarios**:

1. **Given** the example package, **When** it is compiled, **Then** both
   targets generate without errors.
2. **Given** a compiled target with credentials copied from the repository
   root, **When** local dev mode runs over WebRTC and the author speaks a
   question that needs current information, **Then** the agent performs a web
   search through the Firecrawl MCP server, answers from the results, and no
   errors are displayed.
3. **Given** the example's tool selection, **When** the package runs, **Then**
   the model is offered only the search tool from the Firecrawl server.
4. **Given** the example's documentation set, **Then** the example README, the
   emitted runbook, and the docs pages describe the same behaviour, and every
   link between them resolves.

---

### Edge Cases

- The MCP server is unreachable when the agent starts or during a call: the
  generated project follows its platform's native behaviour, the failure is
  visible in the project's output, tools from other sources keep working, and
  the emitted runbook states what to expect on each platform.
- A selected tool name is not among the tools the server exposes: the server's
  tool list only exists at run time, so the compiler cannot check names. The
  selection is passed through, the missing name is simply never offered, and
  the runbook says so.
- The environment variable named for the address or the credential is missing
  at startup: the startup check names the missing variable, matching the
  existing secrets behaviour.
- A target that cannot run MCP tools is declared: validation fails loudly and
  names the target with its own vocabulary, matching the existing capability
  gate for MCP on targets without a runtime MCP client.
- Two tool files name the same server address: each file is its own tool
  source with its own selection and assignment; declaring both is legal.
- An existing MCP tool file written against the old shape, with top-level
  `description` and `input`: strict decode fails with the file and line and
  says what to remove, because no driver has ever read those fields on an MCP
  tool. The schema amendment records this break.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: A package MUST be able to declare a remote MCP server as a named
  tool source in a single tool file, holding the server address as an
  environment variable name, an optional transport, an optional authentication
  block, and an optional list of selected tool names.
- **FR-002**: Both remote transports, SSE and streamable HTTP, MUST work on
  both shipped targets. When no transport is stated, the generated project
  uses the platform's own default for the given URL; a stated transport always
  wins.
- **FR-003**: The authentication block MUST reuse the same scheme surface as
  webhook authentication (bearer token and API key header), with every secret
  named by an `UPPER_SNAKE` environment variable. A literal value in a secret
  position MUST fail validation before any artifact is written.
- **FR-004**: Tool selection MUST be an optional list of tool names. Absent,
  all tools the server exposes are offered. Present, exactly the named tools
  are offered, enforced through each platform's native filtering.
- **FR-005**: An MCP tool source MUST be assignable wherever other tools are
  assignable today: an agent's `tools:` list and a task's `tools:` list, with
  the same scoping behaviour. **Amended during implementation (2026-08-14):**
  task scope holds on livekit; on pipecat it fails validation by name, because
  a Flows node advertises only its own function schemas and the SDK's MCP
  client exposes no per-tool handler to build one from (research R11). Agent
  scope works on both. The failure is loud and says to list the source on the
  agent, rather than the source being silently offered everywhere or nowhere.
- **FR-006**: The pipecat driver MUST emit MCP tool sources, lifting its
  maturity gate. The livekit driver MUST keep working and reach parity on
  transport, authentication, and selection. Targets with no runtime MCP
  client MUST keep failing by name. Driver claims MUST be verified against
  current official platform documentation, with the verification date
  recorded.
- **FR-007**: An MCP tool file MUST NOT carry a top-level model contract
  (`description`, `input`, `output`): the server owns each tool's name,
  description, and parameters. A file that still declares them MUST fail at
  load with the file, line, and what to remove.
- **FR-008**: Nothing fails silently, and each failure reports what it can
  know: unknown fields, a second execution block, and a top-level contract
  field on an mcp file fail at load with the file and line; a bad transport
  value and a non-`UPPER_SNAKE` environment name fail validation naming the
  tool, the field, and the legal values.
- **FR-009**: Every environment variable a tool source names MUST reach the
  generated project's environment example file and its startup check, so a
  missing value is named before the first call.
- **FR-010**: The repository MUST gain `examples/mcp-example`: one package,
  both shipped targets, WebRTC local dev only, no telephony. It MUST run
  against the Firecrawl MCP server over streamable HTTP
  (`https://mcp.firecrawl.dev/v2/mcp`), authenticate with a bearer token named
  `FIRECRAWL_API_KEY`, and select only the search tool, so a spoken question
  is answered from a live web search. The credentials required are the
  voice-stack ones already at the repository root plus `FIRECRAWL_API_KEY`,
  provided by the operator in the same place.
- **FR-011**: The authoring surface change MUST land as a numbered, dated
  amendment in the schema document, stating the shape change and that old MCP
  tool files fail strict decode. The emitted runbook, the example README, and
  the docs pages MUST be updated in the same change.

### Key Entities

- **MCP tool source**: A named declaration of one remote MCP server: address
  (environment variable name), transport, authentication, tool selection. The
  name is the file name, and it is what agents and tasks list.
- **Transport**: How the generated project talks to the server. One of SSE or
  streamable HTTP. Optional; platform default applies when absent.
- **Authentication block**: Scheme plus secret environment variable name,
  mirroring webhook authentication. Never a secret value.
- **Tool selection**: An optional list of tool names from the server. Absent
  means all.
- **Assignment**: The existing `tools:` lists on agents and tasks. Listing a
  source name attaches its selected tools to that scope.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An author connects an agent to a remote MCP server by writing
  one tool file of fewer than fifteen lines and adding one entry to a `tools:`
  list.
- **SC-002**: The same package compiles to both shipped targets with zero
  package edits between them.
- **SC-003**: On each shipped target, a live WebRTC conversation reaches a
  spoken answer built from MCP tool results, with no errors displayed from
  compile through conversation end.
- **SC-004**: With a selection of N tool names declared, the model is offered
  exactly N tools from that server; with no selection, it is offered every
  tool the server exposes.
- **SC-005**: A package holding a pasted secret value instead of an
  environment variable name fails validation before any artifact exists.
- **SC-006**: A package declaring a target that cannot run MCP tools fails
  validation with a message naming that target and the reason.
- **SC-007**: The example is verified end to end on both targets by copying
  the repository-root credentials into each compiled target and running local
  dev mode, with no errors displayed.

## Assumptions

- Only remote MCP servers are in scope. Local process servers over stdio are
  not part of this feature; the block can grow that later without a shape
  change.
- The authentication surface is the existing webhook scheme set (bearer and
  API key header). OAuth flows and request signing stay out, matching the
  webhook decision already recorded in the schema.
- The example's server is the Firecrawl MCP server, and the operator provides
  the `FIRECRAWL_API_KEY` value at the repository root beside the voice-stack
  credentials. The example is verified against the live server when it is
  built.
- Runtime failure behaviour for an unreachable server follows each platform's
  native behaviour rather than adding a portability layer, in line with the
  compile-ahead-of-time principle. The documentation states the behaviour per
  platform.
- The existing capability rule that MCP on the livekit target requires the
  Python SDK stays as is.
- Task scope on pipecat is a platform limit, not a maturity gate: it lifts only
  if pipecat's Flows nodes accept a plain tool schema or its MCP client exposes
  a per-tool handler. Recorded in the capability table, SCHEMA N40, and the two
  target pages.
- Validation-only targets keep their current standing: the new fields validate
  against the capability table, and nothing in this feature is a claim that
  they compile.
- Existing packages in the wild are assumed not to use the current MCP block,
  since no shipped driver ran it on Pipecat and the repository's own examples
  do not use it. The strict-decode break is therefore acceptable and is
  recorded in the amendment.

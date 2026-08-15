# Feature Specification: Coding-agent skill for building Unmute voice agents

**Feature Branch**: `011-coding-agent-skill`

**Created**: 2026-08-15

**Status**: Draft

**Input**: User description: "I want to create a skill, that user will be able to download via the CLI (unmute skill --install) or similar. The skill, should be able to tell any coding assistant (we have to support the majors such as CLAUDE code, codex, github copilot, etc) how to create a voice agent project with unmute, going into different levels of details. By default, we will always use SLNG models for STT and TTS and Openai for the thinking model, but we can ask users to provide their own preferred models. In summary, the skill should be able to create agents with different complexity levels: starting from a single agent, agent handoff (2 or more agents and how the context is shared), tasks and taskgroups. It should cover all the nuances that makes an agent GREAT. Especially context sharing across tasks/task groups etc. Tools are another important step: you need to make sure that the coding agent is able to: add webhook tools, add pre-configured tools, add MCP tools, add python tools with what the user is asking. For voice agents, prompting is crucial. So make sure that you include in the skill the prompting guidelines /voice-agent-prompting. It is very important how each task, task group, agent etc are defined. Additionally, you need to make sure that in the CLAUDE.md we add the fact that, once a feature is added, not only should add to the docs but also to the skill so that coding agents know how to use it."

## Why this exists

Most people meet Unmute through a coding assistant, not through the docs. The
assistant writes the `agent.yaml`, the `targets.yaml`, the `tools/*.yaml`, and
the instruction markdown. Today it does that from guesswork: it invents fields
the schema does not have, it picks a context-sharing mode at random, and it
writes text prompts that sound wrong when a TTS engine reads them out loud.

This feature ships a skill: a bundle of instructions that any major coding
assistant can read, installed into a project with one command, that turns
"build me a voice agent that books salon appointments" into a package that
validates on the first try and sounds good on a real call.

The skill is a navigation layer over the documentation, never a second
opinion. It covers everything the documentation covers, and every claim it
makes names the page that owns that claim. The documentation stays the source
of truth, so an assistant that needs more than the skill gives it knows exactly
where to go, and a reader who suspects the skill is stale can check in one
click.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Install the skill into a project (Priority: P1)

A developer has the Unmute CLI. They run one command inside their project, and
from that point their coding assistant knows what Unmute is, what a package
looks like, and how to build one. The command detects which assistants the
project already uses and writes the right entry file for each; if it detects
none, it asks or takes a named assistant.

**Why this priority**: nothing else in this feature can be tested or delivered
until the skill reaches the assistant. This is the whole distribution path.

**Independent Test**: run the install command in an empty directory and in a
directory that already has an assistant configured; confirm the expected files
appear, that a second run does not silently destroy edits, and that the whole
thing works with no network connection.

**Acceptance Scenarios**:

1. **Given** a project with no assistant configuration, **When** the developer
   runs the install command and names an assistant, **Then** the skill files
   for that assistant are written and the command prints where they landed and
   what to do next.
2. **Given** a project that already uses one of the supported assistants,
   **When** the developer runs the install command with no arguments, **Then**
   the tool detects that assistant and installs for it.
3. **Given** a project where the skill is already installed at the same
   version, **When** the developer runs install again, **Then** the command
   reports that it is already current and changes nothing.
4. **Given** an installed skill whose files were edited by hand, **When** the
   developer runs install again with a newer CLI, **Then** the command refuses
   to overwrite silently and says exactly which files differ and how to force
   the update.
5. **Given** a machine with no internet access, **When** the developer runs
   install, **Then** it succeeds, because the skill travels with the CLI
   rather than being fetched.

---

### User Story 2 - One brief, one working single agent (Priority: P1)

A developer describes an agent in a paragraph of plain English. The assistant,
following the skill, produces a complete package: `agent.yaml`, `targets.yaml`,
an instructions file, and any secrets placeholders. It uses SLNG for listening
and speaking and OpenAI for thinking unless the developer named other vendors.
It then checks its own work and fixes what failed before handing back.

**Why this priority**: this is the payload. A skill that installs but does not
produce a working agent has delivered nothing.

**Independent Test**: give an assistant with the skill installed a short brief
and no other help; confirm the resulting package passes Unmute's validation
with zero errors and can be run locally and spoken to.

**Acceptance Scenarios**:

1. **Given** a brief with no vendors named, **When** the assistant builds the
   package, **Then** the listening and speaking models are SLNG and the
   thinking model is OpenAI, and the assistant says so rather than leaving it
   implicit.
2. **Given** a brief that names preferred vendors, **When** those vendors exist
   for the chosen target, **Then** the assistant uses them; **When** a named
   vendor does not exist for that target, **Then** the assistant says so and
   offers the vendors that do, instead of writing a package that fails later.
3. **Given** the package is written, **When** the assistant runs validation,
   **Then** validation passes; **And When** it does not pass, **Then** the
   assistant reads the file, line, and column from the error and fixes the
   package rather than guessing.
4. **Given** the developer has not chosen a target, **When** the assistant
   builds the package, **Then** it picks one, names it, and explains the
   consequence in one line.
5. **Given** the package needs a secret, **When** it is written, **Then** the
   package carries the environment variable name only, never a pasted key.

---

### User Story 3 - Prompts that sound right out loud (Priority: P2)

Every instruction file the assistant writes, for an agent, a task, or a task
group, follows voice prompting rules rather than chat prompting habits. Short
spoken turns. No markdown, no bullet lists, no raw URLs, no unspoken digits.
An identity, output rules, tool rules, goals, and guardrails. Enough natural
speech shaping that the agent does not sound like a form letter.

**Why this priority**: an agent that validates and compiles but reads bullet
points out loud is a failed agent. This is the difference between working and
good, and the user named it as crucial.

**Independent Test**: ask an assistant with the skill to write instructions for
a given role, then check the result against the prompt structure and the voice
formatting rules; also give it a chat-style prompt and ask it to make it
voice-ready.

**Acceptance Scenarios**:

1. **Given** a request for an agent's instructions, **When** the assistant
   writes them, **Then** the file carries the expected sections in order and
   contains no markdown formatting, tables, or raw URLs meant to be spoken.
2. **Given** a task's instructions, **When** the assistant writes them, **Then**
   they are scoped to that one job and its typed result, and they do not repeat
   the whole agent's personality.
3. **Given** a fixed greeting is wanted, **When** the assistant writes it,
   **Then** it is written as a spoken line and placed where a fixed greeting
   belongs, not buried in the instructions.
4. **Given** an existing chat-shaped prompt, **When** the developer asks the
   assistant to make it voice-ready, **Then** the assistant rewrites it and
   names what it changed and why.
5. **Given** a tool exists, **When** the assistant writes its description,
   **Then** the description tells the model when to call it, in the words the
   caller would use.

---

### User Story 4 - Tools of every kind (Priority: P2)

The developer asks for a capability: "look up availability from our API",
"let it hang up", "give it web search", "run this Python against our
database". The assistant picks the right kind of tool for the ask, writes the
file correctly, and wires it to the agents or tasks that should see it.

**Why this priority**: an agent with no tools can only talk. Tools are where
most real requests land, and each of the four kinds has a different file shape
with rules that are easy to get wrong.

**Independent Test**: make four separate requests, one per kind, and confirm
each produces a valid tool file, wired to the right agents, that validates on
the chosen target.

**Acceptance Scenarios**:

1. **Given** a request that hits an HTTP endpoint, **When** the assistant
   writes the tool, **Then** it uses the webhook kind, declares the input
   schema, refers to the URL and any token by environment variable name, and
   picks the right auth scheme.
2. **Given** a request for behaviour the prebuilt registry already covers,
   **When** the assistant writes the tool, **Then** it uses the prebuilt kind
   with the registry id and does not hand-roll an equivalent.
3. **Given** a request for tools hosted on a remote MCP server, **When** the
   assistant writes the tool, **Then** the file carries only what an MCP tool
   is allowed to carry, and the assistant states the target and scope
   restrictions before writing rather than after validation fails.
4. **Given** a request for custom Python, **When** the assistant writes the
   tool, **Then** it writes both the tool file and the handler, the handler
   reads its secrets from the environment, and the assistant states that this
   kind only works on code targets.
5. **Given** any tool is written, **When** the assistant finishes, **Then**
   every agent or task that should be able to call it lists it, and no other
   one does.
6. **Given** a value must ride along with the request but must stay hidden from
   the model, **When** the assistant writes the tool, **Then** it uses the
   hidden-value mechanism rather than adding it to the model-visible input.

---

### User Story 5 - Climbing the complexity ladder (Priority: P3)

The developer outgrows one agent. They ask for a second agent to take over, or
for a side job that returns an answer, or for a fixed sequence of steps. The
assistant knows which of the three shapes fits, and, above all, decides context
sharing on purpose: what the next agent remembers, what a delegated job is
allowed to see, and what comes back when it finishes.

**Why this priority**: this is the part assistants get wrong most often and the
part the user singled out. It is P3 only because a single agent is a complete
product without it.

**Independent Test**: give three separate briefs, one that clearly wants a
handoff, one that clearly wants a delegated job with a typed answer, and one
that clearly wants an ordered sequence; confirm the assistant picks the right
shape and states the context decision in each case.

**Acceptance Scenarios**:

1. **Given** a brief where a second specialist should take over the call,
   **When** the assistant builds it, **Then** it writes a handoff, and it
   states in plain words how much conversation history and which variables
   cross over, rather than accepting a default it never mentioned.
2. **Given** a brief where a job should run and hand back a structured answer,
   **When** the assistant builds it, **Then** it writes a delegated job with a
   typed result, and maps result fields into variables where the caller needs
   them later.
3. **Given** a brief with an ordered sequence of steps, **When** the assistant
   builds it, **Then** it writes a group, decides whether the steps share
   context or each start clean, decides what happens when the group ends, and
   explains both choices.
4. **Given** a chosen target cannot honour a requested context option, **When**
   the assistant is about to write it, **Then** it says which target and which
   option, and offers the shapes that do work, before writing files.
5. **Given** a handoff should only happen once certain facts are known,
   **When** the assistant builds it, **Then** it adds the guard on those
   variables rather than trusting the prompt to enforce it.

---

### User Story 6 - Onto a phone line, then into production (Priority: P3)

The agent works in the browser. Now the developer wants to call it on a real
number, hand a caller to a human when the agent cannot help, and put the whole
thing live. The assistant knows the routes, knows which one supports what,
knows that Unmute never buys a number or creates a carrier trunk, and knows
what the operator still owns after the artifact is generated.

**Why this priority**: this is where a demo becomes a product, and it is the
area with the most target-specific rules, so it is the area where an assistant
guessing does the most damage. It is P3 because a browser agent is a complete
product without it.

**Independent Test**: give three separate briefs, one for an inbound phone
agent on a named carrier, one that adds a handover to a human, and one that
asks how to go live; confirm the assistant picks a supported route, names the
route's limits before writing, and separates what Unmute generates from what
the operator must do by hand.

**Acceptance Scenarios**:

1. **Given** a brief for a phone agent, **When** the assistant builds it,
   **Then** it picks a route that actually ships, names the carrier and the
   direction, and states what that route can and cannot do.
2. **Given** a requested route has no generated adapter, **When** the assistant
   is about to write it, **Then** it says so and offers a route that does,
   rather than writing something that fails closed later.
3. **Given** a brief that asks for a handover to a person, **When** the
   assistant builds it, **Then** it chooses the transfer shape the route
   supports, states what the caller experiences, and refuses a shape that route
   cannot emit instead of writing it anyway.
4. **Given** a phone package is written, **When** the assistant explains the
   next step, **Then** it separates what Unmute does from what the developer
   does at the carrier, because Unmute never buys numbers or creates carrier
   trunks.
5. **Given** a request to go live, **When** the assistant answers, **Then** it
   names what the generated project needs from the operator, including public
   ingress, secrets, and scaling, and does not present a local dev run as a
   deployment.

---

### User Story 7 - The skill stays true (Priority: P3)

Whoever adds a feature to Unmute updates the skill in the same change, the same
way they already update the emitted runbook, the example page, and the docs. An
automated check fails when the skill states something the contract no longer
says.

**Why this priority**: a skill that drifts is worse than no skill, because the
assistant states stale facts with full confidence. It is P3 because the first
version of the skill is correct by construction; this protects version two
onward.

**Independent Test**: change a contract fact that the skill states, run the
repository's test suite, and confirm it goes red with a message naming the
skill file.

**Acceptance Scenarios**:

1. **Given** the contributor rules, **When** a contributor reads them, **Then**
   the skill is named alongside the emitted runbook, the example page, and the
   docs as a place a behaviour change must land.
2. **Given** a contract fact the skill states is changed in code, **When** the
   test suite runs, **Then** it fails and names the skill file that is now
   wrong.
3. **Given** the skill links to a docs page or an example, **When** the test
   suite runs, **Then** every such link is checked to resolve.

---

### Edge Cases

- The developer names an assistant the skill does not support. The command
  lists what it does support and exits with an error, rather than writing a
  best-guess file somewhere.
- The install runs outside any project, or in a directory the process cannot
  write to. It fails with a clear reason and leaves nothing behind.
- Two supported assistants are configured in the same project. Both get an
  entry file, and both point at one shared body of instructions rather than two
  copies that can disagree.
- The installed skill is older or newer than the CLI on the machine. The skill
  records the version that wrote it, and a mismatch is reported.
- The assistant has no shell access, so it cannot run validation itself. The
  skill tells it to hand the exact command to the developer and to wait for the
  result before claiming the package works.
- The developer asks for something Unmute does not support at all, such as a
  feature reserved for a target with no driver. The assistant names the limit
  and stops, rather than emitting something that validates green and does
  nothing.
- The developer asks for a model vendor that has no catalogue entry. The
  assistant does not invent one.
- The assistant produces a field name that does not exist. Strict decoding
  fails with file, line, and column, and the skill's recovery loop is what turns
  that into a fix instead of a retry storm.
- A brief mixes shapes, for example a handoff inside an ordered sequence. The
  skill gives a rule for choosing, so the assistant does not stall or pick at
  random.
- A documentation page the skill points at is renamed or moved. The pointer
  check goes red in the same change, so the link is fixed by whoever moved the
  page rather than found broken by a user months later.
- The assistant needs a detail the skill does not carry. The skill has already
  told it which page owns the subject, so the next step is reading that page,
  not inventing an answer.
- The developer installs the skill in a project that has no Unmute package yet,
  and in one that already has a package to extend. Both are supported paths.

## Requirements *(mandatory)*

### Functional Requirements

**Distribution and install**

- **FR-001**: The CLI MUST provide a command that installs the skill into the
  current project.
- **FR-002**: The skill contents MUST travel with the CLI, so install makes no
  network call and the skill a developer gets is the one that matches the CLI
  they are running.
- **FR-003**: The install MUST support the major coding assistants, at minimum
  Claude Code, OpenAI Codex, GitHub Copilot, and Cursor, and MUST fail by name
  for an assistant it does not support.
- **FR-004**: The install MUST cover every supported assistant by default, and
  MUST allow the developer to name one or more explicitly instead.

  *Amended during implementation, 2026-08-15.* This originally required the
  install to detect which assistants a project already used and default to
  those. Research (R8) and the plan's "Deliberate simplifications" both dropped
  detection: there are only two small destinations, writing both is always
  correct, and detection is a heuristic that is wrong for anyone who has not yet
  created their assistant's directory. `--agent` covers the developer who wants
  only one. The requirement is reworded to match what shipped rather than left
  as a MUST the plan contradicts.
- **FR-005**: The install MUST write one shared body of instructions plus a
  thin per-assistant entry file, so two assistants in one project cannot be
  told different things.
- **FR-006**: The install MUST NOT overwrite locally modified skill files
  without an explicit force, and MUST name the files that differ.
- **FR-007**: The install MUST record the CLI version that produced the
  installed skill, and MUST report a mismatch on a later run.
- **FR-008**: The installed files MUST be plain text that a developer can read,
  diff, and commit to their repository.

**What the skill teaches: the package**

- **FR-009**: The skill MUST describe the package layout and the purpose of
  each file an author writes.
- **FR-010**: The skill MUST state the default model choice: SLNG for listening
  and speaking, OpenAI for thinking, and MUST require the assistant to say
  which vendors it used.
- **FR-011**: The skill MUST tell the assistant to accept developer-named
  vendors, to check that a named vendor exists for the chosen target, and to
  refuse rather than invent one that does not.
- **FR-012**: The skill MUST tell the assistant that secrets are declared as
  environment variable names and never as values, in any file.
- **FR-013**: The skill MUST teach the build loop: write the package, run
  validation, read the file, line, and column of any error, fix, repeat, and
  only then report success. It MUST cover the case where the assistant cannot
  run commands itself.
- **FR-014**: The skill MUST teach how to run the agent locally so the
  developer can speak to it, and MUST make that, not a green validation, the
  definition of done.

**What the skill teaches: complexity levels**

- **FR-015**: The skill MUST cover, as a graded ladder, the single agent, the
  two-or-more agent handoff, the delegated job with a typed result, and the
  ordered group of steps, and MUST give a rule for choosing between them.
- **FR-016**: The skill MUST make context sharing an explicit decision at every
  boundary: how much history crosses a handoff, which variables cross, what a
  delegated job sees when it starts, whether the steps of a group share context
  or each start clean, and what comes back to the caller when a group ends.
- **FR-017**: The skill MUST require the assistant to state the context
  decision it made, in plain words, whenever it writes one.
- **FR-018**: The skill MUST cover machine-checked guards on a handoff, so a
  precondition is enforced by the artifact rather than by the prompt.
- **FR-019**: The skill MUST cover typed results and mapping result fields into
  variables the rest of the call can use.
- **FR-020**: The skill MUST list the places where a target refuses a shape or
  an option, so the assistant raises the limit before writing files rather than
  after validation fails.

**What the skill teaches: tools**

- **FR-021**: The skill MUST cover all four tool kinds a developer will ask
  for: webhook, prebuilt, MCP, and Python. For each it MUST give the file
  shape, what is legal and what is not, and which targets accept it.
- **FR-022**: The skill MUST cover webhook authentication schemes and which
  authentication needs are out of scope in this version.
- **FR-023**: The skill MUST cover writing the Python handler alongside the
  Python tool file, including how the handler reads its own secrets.
- **FR-024**: The skill MUST cover attaching values to a request that the model
  can neither see nor overwrite.
- **FR-025**: The skill MUST cover which agents and jobs see a tool, and make
  clear that visibility is decided in one place.
- **FR-026**: The skill MUST tell the assistant to write a tool description
  aimed at the model deciding when to call it.

**What the skill teaches: prompting**

- **FR-027**: The skill MUST carry voice prompting guidance covering prompt
  structure, output rules for spoken text, tool-use rules, goals, guardrails,
  and speech realism.
- **FR-028**: The prompting guidance MUST be applied separately to each prompt
  surface: an agent's instructions, a delegated job's instructions, a group's
  steps, a greeting, and a tool description, with what changes at each.
- **FR-029**: The skill MUST tell the assistant how to turn a chat-shaped
  prompt into a voice-ready one and to report what it changed.
- **FR-030**: The skill MUST give a way to test a prompt, so "it sounds fine"
  is not the only check.

**Shape and quality of the skill itself**

- **FR-031**: The skill MUST be layered so an assistant reads a short entry
  document first and loads deeper reference material only when the task needs
  it. The entry document MUST stay small enough to be read on every task no
  matter how much reference material sits behind it, and MUST make clear which
  reference to open for which kind of request.
- **FR-032**: The skill MUST carry complete, working examples for each rung of
  the complexity ladder and each tool kind, drawn from packages that this
  repository already builds and tests.
- **FR-033**: The skill MUST state clearly which target providers can be
  generated and run and which can only be validated, and MUST NOT let a reader
  mistake one for the other.

**Keeping it true**

- **FR-034**: The repository's contributor rules MUST name the skill, alongside
  the emitted runbook, the example page, and the docs, as a place a change to
  authoring or emitted behaviour must land in the same commit.
- **FR-035**: An automated check that runs in the default test suite MUST fail
  when the skill states a fact the contract no longer supports, and MUST name
  the offending skill file.
- **FR-036**: Every link from the skill into the repository's docs or examples
  MUST be checked to resolve.

**Coverage and provenance**

- **FR-037**: The skill MUST cover every area the user documentation covers, in
  enough depth that an assistant can act on it without leaving the skill for
  the common cases. Nothing the product supports is left as a bare mention.
- **FR-038**: The skill MUST NOT name a documentation page a reader cannot
  open. *Amended during implementation, 2026-08-15.* This requirement used to
  say the opposite: every claim named the page that owned it, and the skill was
  a navigation layer rather than a second source. The site is not published, so
  in practice every one of those pointers resolved to nothing outside this
  repository, and an assistant meeting a dead pointer could not tell a missing
  page from its own mistake. The pointers were removed. When the site is public
  they should come back as absolute URLs, and this requirement should return to
  its original form.
- **FR-039**: The skill MUST state, in its own text, what wins when it is
  wrong. *Amended during implementation, 2026-08-15, alongside FR-038.* The
  authority is `unmute validate`, which the reader can run, rather than a
  documentation page they cannot reach. The skill MUST tell the assistant to
  read the refusal, quote it, and change the package, rather than working
  around it or preferring what it remembers about Unmute.
- **FR-040**: The skill MUST cover telephony: choosing a route from the
  orchestrator, transport, and carrier together rather than from a brand name,
  what each route can and cannot do, inbound and outbound, testing over a real
  phone locally, and the fact that Unmute never buys numbers and never creates
  carrier applications or carrier trunks.
- **FR-041**: The skill MUST cover handing a caller to a person: the transfer
  shapes, which routes support which shape, what the caller experiences in
  each, and how the destination is named without putting a number in the
  package.
- **FR-042**: The skill MUST cover going live: what the generated project needs
  from the operator, what stays the operator's job, and why a local dev run is
  not a deployment.
- **FR-043**: The skill MUST cover the rest of the authoring surface the
  documentation covers, including variables and where their values come from,
  secrets, greetings and the conversation lifecycle, turn detection, tracing,
  and capacity.

### Key Entities

- **Skill bundle**: the shared body of instructions, references, and examples
  that teaches an assistant to build Unmute packages. One bundle, versioned
  with the CLI.
- **Assistant entry file**: the small per-assistant file that makes the bundle
  discoverable by that assistant, in the location and format that assistant
  reads.
- **Install record**: the note of which CLI version wrote the installed skill,
  used to report drift on a later run.
- **Complexity rung**: one level of the ladder (single agent, handoff,
  delegated job, ordered group), each with its own rule for when to use it, its
  own context decisions, and its own worked example.
- **Tool recipe**: one of the four tool kinds, with its file shape, its legal
  and illegal fields, its target support, and a worked example.
- **Prompt surface**: a place a prompt is written (agent, job, group step,
  greeting, tool description), each with its own rules.
- **Model default**: the vendor bound for listening, speaking, and thinking
  when the developer names none.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A developer installs the skill with one command in under a
  minute, with no network connection and no configuration.
- **SC-002**: Given a one-paragraph brief, an assistant with the skill produces
  a package that passes validation with zero errors on the first attempt in at
  least 8 of 10 briefs, and in all 10 after one self-correction round.
- **SC-003**: Across the whole acceptance set, assistant-written packages
  contain zero invented field names. Every field used exists in the authoring
  contract.
- **SC-004**: Each of the four tool kinds can be added from a plain-English
  request and validates on the chosen target, in 4 out of 4 attempts.
- **SC-005**: In every case where a multi-agent, delegated job, or group shape
  is produced, the assistant states the context decision it made. Silent
  defaults occur zero times.
- **SC-006**: Every instruction file produced carries the expected prompt
  sections and contains no markdown formatting, tables, or raw URLs intended
  to be spoken.
- **SC-007**: When a brief asks for something the chosen target refuses, the
  assistant names the limit before writing files, in 100% of such cases.
- **SC-008**: The skill installs correctly for all four named assistants, and a
  project with two of them installed shares one body of instructions.
- **SC-009**: Changing a contract fact the skill states makes the default test
  suite fail, every time, with the skill file named.
- **SC-010**: A developer who has never used Unmute goes from install to
  speaking with their own agent, through their assistant, in under 15 minutes.
- **SC-011**: Every reference document in the skill names the documentation
  page that owns its subject, and every one of those pointers resolves. Zero
  orphan claims, zero broken pointers.
- **SC-012**: Given a brief for a phone agent on a named carrier, the assistant
  picks a route that ships, names that route's limits before writing files, and
  separates the work Unmute does from the work the developer does at the
  carrier, in 100% of such briefs.
- **SC-013**: The entry document is short enough that an assistant reads it on
  every task, and a request that needs deeper material causes the right
  reference to be opened rather than the whole bundle.

## Assumptions

- The command shape is a `skill` subcommand with an `install` action. The user
  wrote `unmute skill --install`; the exact spelling is settled in planning.
  Either way this adds a fifth command to the CLI, which the constitution
  currently fixes at four. See Dependencies.
- The skill installs into the project, not into a global assistant
  configuration directory, so a team shares one skill through their repository.
  A global install is out of scope for this version.
- The bundle is written once for all assistants, and per-assistant differences
  are limited to the entry file's location and format. No assistant gets
  different facts.
- The skill covers the whole documented surface, decided 2026-08-15. It goes
  deep everywhere the documentation goes, including telephony, human transfers,
  and deployment. Depth is bought with layering rather than with a shorter
  scope: the entry document stays small and the references carry the weight.
- The documentation is the source of truth, and the skill points at it from
  every claim. The skill's own value is deciding and doing: which shape fits
  this brief, which route ships, what to check before writing. Facts belong to
  the pages that own them.
- The prompting guidance is the one part of the bundle the skill originates,
  because no documentation page owns it today. It is vendored into this
  repository, adapted to Unmute's prompt surfaces, and maintained here. Whether
  it also gets a documentation page is a planning question, not a blocker.
- The skill teaches authoring Unmute packages. It does not teach writing
  LiveKit or Pipecat Python by hand, and it does not teach editing generated
  output.
- The default vendors match what the scaffold already binds today: SLNG for
  listening and speaking, OpenAI for thinking.
- Assistants are assumed to be able to read files in the project and write
  files. Running shell commands is treated as likely but not guaranteed, and
  the skill covers both.
- Examples in the skill are drawn from packages this repository already tests,
  rather than written fresh, so they cannot rot independently.

## Dependencies

- **Constitution amendment.** Principle V fixes the command surface at four
  commands. Adding a `skill` command requires an amendment to that principle in
  the same change, stating that the skill command is not part of the path from
  nothing to a spoken agent and does not touch a package.
- **The docs site and the schema are the source of the facts.** The skill
  restates facts that live in `docs/SCHEMA.md`, `internal/target`, the provider
  catalogue, `docs-site/`, and, for the areas this version adds,
  `docs/TELEPHONY.md`, `docs/TRANSFERS.md`, and `docs/DEPLOYMENT.md`. Those
  stay authoritative; the skill is a derived, checked surface that points home
  from every claim, never a second opinion.
- **Full coverage raises the drift stakes.** Telephony has the most
  target-specific rules in the product, so it is where a stale claim does the
  most damage. The pointer rule and the automated checks in FR-035, FR-036, and
  FR-038 are what make full coverage affordable, and they are not optional
  extras to this scope.
- **The existing examples.** The complexity ladder and the tool recipes lean on
  `examples/`, which already covers a single agent, delegated jobs, a group,
  handoffs, and MCP.
- **The existing prompting skill.** The voice prompting content comes from the
  author's `voice-agent-prompting` skill and is adapted, not linked, because
  the installed skill has to work on a machine that has never seen it.

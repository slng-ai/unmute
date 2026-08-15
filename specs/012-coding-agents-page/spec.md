# Feature Specification: The "Coding agents" page

**Feature Branch**: `012-coding-agents-page`

**Created**: 2026-08-15

**Status**: Draft

**Input**: User description: "once the skill is available I want you to create a page similar to what livekit has for coding agents, explaining how to easily get started with coding agents: https://docs.livekit.io/intro/coding-agents/" Narrowed 2026-08-15: "I just want a page for coding agents telling a story on how to use this skill properly."

## Why this exists

Feature 011 ships a skill that teaches any major coding assistant to build
Unmute packages. Nothing tells anyone it exists, and nothing shows what using
it well looks like.

This is that page. One page, telling one story: you install the skill, you ask
for an agent in a sentence, you check what came back, you ask for more, and you
end up talking to something you built. A reader should finish it knowing not
just that the skill exists, but how a good session with it actually goes.

LiveKit's page is the reference for shape and tone. Theirs is a setup page with
a practices list on the end. Ours is narrower on purpose: it is about using the
skill properly, not about every way an assistant can reach documentation.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Set it up and know it took (Priority: P1)

A developer is already working inside a coding assistant. They find this page,
run one command, and their assistant knows Unmute. Then the page gives them a
prompt to paste and tells them what a good answer looks like, so they find out
in a minute whether it applied.

**Why this priority**: setup with no proof step produces silent failures, and a
silent failure here looks exactly like the assistant being bad at Unmute. The
two halves ship together or the page is not finished.

**Independent Test**: hand the page to someone who has never used Unmute, on
each supported assistant. Then deliberately break the setup and confirm the
page's own check catches it.

**Acceptance Scenarios**:

1. **Given** a reader on any supported assistant, **When** they follow the
   page, **Then** they reach a working setup without opening another page.
2. **Given** setup is complete, **When** the reader runs the page's first
   prompt, **Then** the page describes a correct response specifically enough
   to tell it from a confident guess.
3. **Given** setup silently did not apply, **When** the reader runs the check,
   **Then** the difference is visible to them.
4. **Given** a reader who has not installed the CLI, **When** they reach the
   setup step, **Then** the page says so and links to the one page that covers
   it, rather than assuming or restating it.
5. **Given** a reader whose assistant is not listed, **When** they read the
   page, **Then** it says what they can still do, rather than implying Unmute
   needs a particular tool.

---

### User Story 2 - Follow one build from a sentence to a voice (Priority: P1)

The page walks through a single real build, start to finish. What the reader
types. What the assistant does with it. What the reader looks at before saying
yes. What they run to hear it. Not a feature tour, one story with a beginning
and an end.

**Why this priority**: this is the difference between a setup page and the page
the user asked for. A reader who has seen one session go well knows how to
start their own; a reader given a command and a list does not.

**Independent Test**: follow the page's own story literally, typing what it
says to type, and confirm you end up talking to the agent it promised.

**Acceptance Scenarios**:

1. **Given** the story, **When** a reader follows it exactly, **Then** they
   reach a running agent they can speak to.
2. **Given** each step in the story, **When** the reader reads it, **Then**
   they can see what the assistant did and what they are meant to check before
   moving on.
3. **Given** the story, **When** a reader compares it to their own different
   idea, **Then** the shape transfers, because the page shows the moves rather
   than only the outcome.
4. **Given** the story's build, **When** the page presents it, **Then** it is a
   build this project can actually run, not an illustration.

---

### User Story 3 - Ask for more, and know what good looks like (Priority: P2)

The reader wants a tool, a second agent, a phone number. The page shows how to
ask so the assistant does the right thing, and what the assistant should be
telling them back: which target it chose, which models it bound, what context
crosses a handoff, what it checked before claiming it worked.

**Why this priority**: this is the "properly" in the request. It is P2 because
the first build works without it, but every build after it is worse without it.

**Independent Test**: take each kind of follow-up the page covers, ask for it
the way the page suggests, and confirm the assistant's response includes what
the page says to expect.

**Acceptance Scenarios**:

1. **Given** a reader wanting a capability, **When** they follow the page's
   advice on asking, **Then** the request carries the detail the assistant
   needs, so it does not guess.
2. **Given** any assistant response, **When** the reader checks it against the
   page, **Then** they know which decisions the assistant should have stated
   out loud, and can notice a silent one.
3. **Given** the page's habits, **When** a reader reads them, **Then** each one
   states the habit and the failure it prevents, in one line, and each one that
   says avoid something says what to do instead.
4. **Given** the reader asks for something Unmute does not do, **When** the
   assistant answers, **Then** the page has already told the reader to expect a
   plain refusal rather than an invention, so they can tell the two apart.

---

### User Story 4 - The page cannot quietly go stale (Priority: P3)

The list of assistants on the page and the list the CLI actually supports are
the same list, held by a test rather than by memory. Every link resolves.

**Why this priority**: this page names things that change: assistants,
commands, file locations. A stale setup page is worse than none, because the
reader follows it and blames themselves. It is P3 because the page is correct
on the day it ships.

**Independent Test**: add or remove a supported assistant in the CLI, run the
test suite, and confirm it fails naming this page.

**Acceptance Scenarios**:

1. **Given** the CLI's set of supported assistants changes, **When** the test
   suite runs, **Then** it fails and names this page.
2. **Given** the page's links, **When** the link check runs, **Then** every one
   resolves.
3. **Given** the site's rule that pages and navigation entries agree in number,
   **When** this page is added, **Then** the rule still holds.

---

### Edge Cases

- The reader's assistant is not one the skill supports. The page says which
  ones are, and what a reader on another assistant can still do.
- The reader has no CLI installed. The page treats that as a prerequisite and
  links to it rather than restating it.
- The reader's assistant cannot run shell commands. Both the setup step and the
  proof step have to survive that, because one of the supported assistants
  often cannot.
- The reader is offline. The skill install works offline by design, so the page
  says which parts of the story need a network and which do not.
- The reader's installed skill is older than their CLI. The page says how that
  shows up and what to run.
- The reader arrives from a search engine, straight into the middle of the
  page. Each section still has to make sense on its own, even though the page
  is written to be read in order.
- Two assistants are set up in the same project. The page says this is fine and
  that they share one body of instructions.
- The story's build stops working because something upstream changed. The page
  names a build this project already runs and tests, so that failure shows up
  in the test suite rather than in a reader's terminal.

## Requirements *(mandatory)*

### Functional Requirements

**The page**

- **FR-001**: The documentation site MUST carry one page about building with a
  coding assistant, placed where a new reader meets it early rather than after
  they have already started by hand.
- **FR-002**: The page MUST be reachable from the site navigation, and the
  site's existing rule that pages and navigation entries agree in number MUST
  still hold.
- **FR-003**: The page MUST be written as one story read in order, not as a
  reference list, while still leaving each section usable on its own for a
  reader who lands mid-page.
- **FR-004**: The page MUST open by saying what the reader gets and roughly
  what it costs them in time, so they can decide to continue in one paragraph.
- **FR-005**: The page MUST NOT imply that a coding assistant is required to
  use Unmute, or that an unsupported assistant rules it out.

**Setup and proof**

- **FR-006**: The page MUST name every coding assistant the skill supports and
  give each one a setup path that stands alone.
- **FR-007**: The page MUST give the single command that installs the skill,
  and MUST say what appears in the project as a result and whether it belongs
  in version control.
- **FR-008**: The page MUST state its prerequisites and link to them rather
  than repeating them.
- **FR-009**: The page MUST cover the case where the reader's assistant cannot
  run commands, so setup and the proof step are both still possible.
- **FR-010**: The page MUST give a first prompt the reader can paste, and MUST
  describe a correct result in enough detail to tell it from a confident guess.
- **FR-011**: The page MUST give a way to tell that setup did not apply, so a
  silent failure is visible.

**The story**

- **FR-012**: The page MUST carry one worked build, followed from a plain
  sentence through to an agent the reader can speak to, showing at each step
  what the reader says, what the assistant does, and what the reader checks.
- **FR-013**: The worked build MUST be one this project already runs and tests,
  so it cannot rot on its own.
- **FR-014**: The page MUST show how to ask for the common next steps, at
  minimum a tool, a second agent, and a phone number, and MUST make clear what
  detail a request needs so the assistant does not guess.
- **FR-015**: The page MUST tell the reader which decisions a good assistant
  states out loud, at minimum the target it chose, the models it bound, the
  context that crosses a handoff, and what it checked before claiming success,
  so a silent decision is noticeable.
- **FR-016**: The page MUST carry a short list of habits, each stating the
  habit and the failure it prevents in one line, and each one that says avoid
  something MUST say what to do instead. At minimum: let the assistant check
  its own work and read the error rather than guess; never edit generated
  output, because it is rewritten on every build; name the target rather than
  letting it be chosen silently; treat the documentation as the authority over
  anything the assistant remembers; and listen to the agent, because a green
  check is not a good call.
- **FR-017**: The habits section MUST be short enough to finish in one sitting,
  because a list nobody finishes prevents nothing.

**Staying true**

- **FR-018**: An automated check in the default test suite MUST fail when the
  assistants the page names and the assistants the CLI supports stop matching,
  and MUST name this page.
- **FR-019**: Every link on the page MUST be checked to resolve.
- **FR-020**: The page MUST NOT document a command, address, or file that does
  not exist or does not resolve on the day it is published.
- **FR-021**: The page MUST obey the rules the rest of the documentation site
  is written under, including plain language with no dashes used as
  punctuation, only the two targets that ship presented as targets, and no
  claim of maturity beyond what the project can show.

### Key Entities

- **The page**: one page in the user documentation, for a reader who builds
  through an assistant rather than by hand.
- **Assistant setup path**: the per-assistant instructions for getting the
  skill in place, one per supported assistant.
- **First-run prompt**: the paste-and-see check that proves setup applied.
- **The worked build**: the single example the story follows end to end.
- **Habit**: one practice, with the failure it prevents.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A developer who has never used Unmute goes from landing on this
  page to speaking with an agent they built through their assistant, in under
  15 minutes.
- **SC-002**: A reader following the page opens no other page before their
  assistant is set up and proven.
- **SC-003**: The set of assistants the page names matches the set the CLI
  supports exactly, and a change to either fails the test suite.
- **SC-004**: Every command, address, and file the page names exists and works
  on the day it is published. Zero broken instructions.
- **SC-005**: Every link on the page resolves.
- **SC-006**: A reader who deliberately breaks their setup finds out from the
  page's own check, not from a confusing result an hour later.
- **SC-007**: The habits section can be read end to end in under two minutes.
- **SC-008**: Every claim on the page holds for a reader outside this project,
  with no fact that depends on repository access or special permissions.
- **SC-009**: The site's page and navigation counts stay equal after this page
  is added.

## Assumptions

- One page, not a group. LiveKit's equivalent is one page and this content
  fits.
- It sits in the site's first navigation group, near installation. A reader who
  finds it after the hand-authoring pages has already taken the slower path.
- The supported assistants are whichever ones feature 011 supports. This page
  never widens or narrows that set on its own.
- The install command is the one feature 011 defines. This page documents it
  and does not invent a second entry point.
- The page links within the documentation site and to the repository only, so
  it reads correctly whether or not the site is publicly reachable.
- The worked build is drawn from an example this repository already ships and
  tests, rather than written fresh for the page.
- The reader has the CLI or can install it. The page does not restate
  installation.

## Out of scope

- **Other ways to feed an assistant the documentation.** LiveKit's page also
  covers a documentation subcommand in their CLI, a search endpoint, and a
  machine-readable index. Those are separate capabilities, and two of the three
  behave differently while a documentation site is not publicly reachable. This
  feature is the page. Worth noting for later, not built here.
- **Any change to the skill itself.** The page documents what feature 011
  ships. If writing the page shows something the skill should do differently,
  that is a change to 011, not a widening of this one.

## Dependencies

- **Feature 011 ships first.** This page documents the skill, its install
  command, its supported assistants, and its file locations. None of that can
  be written until those are settled, which is what "once the skill is
  available" means.
- **An example this repository already tests.** FR-013 leans on `examples/`,
  which already covers a single agent, tools, handoffs, and telephony.
- **The site's existing rules and tests.** The page inherits the rules the site
  is written under and the invariant that pages and navigation entries agree in
  number.

# Feature Specification: Complexity Cleanup

**Feature Branch**: `011-complexity-cleanup`

**Created**: 2026-08-15

**Status**: Draft

**Input**: User description: "let's create the specs to fix ALL the above." (the repository-wide over-engineering audit run immediately before this command)

## Context

A repository-wide audit looked for code that does not need to exist: unreachable
functions, the same fact written twice, hand-written versions of things the
standard library ships, and parameters that only ever take one value.

The audit produced nineteen findings. Three of them were wrong, because the
constitution mandates the thing the audit proposed cutting. Those three are
recorded in Out Of Scope below with the rule that protects them, so that nobody
re-proposes them later. The rest are real, and this feature removes them.

Every remaining finding is something the constitution already asks for.
Principle III wants one home per fact. Governance says an abstraction with one
implementation, or a knob for a value that never changes, needs a stated reason
or the answer is no. None of the code in scope has that reason.

**This feature changes no behavior.** It is a pure internal cleanup. What the
tool prints, what it writes to `build/<target>/`, and what exit code it returns
are all byte-for-byte identical before and after. That property is what makes
the work safe to do in one pass, and it is the first thing the acceptance
criteria check.

## Clarifications

### Session 2026-08-15

- Q: Which `unmute dev` modes should the acceptance run cover before this refactor is considered verified? → A: Browser mode (`unmute dev`) on the seven examples that need no carrier credentials, both targets each. Telephony examples are compile-only.
- Q: What has to happen in a browser session for it to count as passing? → A: The session establishes and the agent speaks its greeting. No human speech is required, so the check can be scripted.
- Q: How often should the thirteen-session acceptance run happen across the five user stories? → A: The full sweep runs after every user story, so a regression is attributed to the story that caused it. Sixty-five sessions in total.
- Q: What should the sweep do about secrets that an example declares but the person running it does not have? → A: Every declared secret must be present and real; any missing key fails the sweep. Real values come from the untracked `.env` at the repository root.

## User Scenarios & Testing *(mandatory)*

The user in every story below is a contributor to this repository: someone
reading the code to change it, review it, or find a bug in it.

### User Story 1 - Nothing in the tree is unreachable (Priority: P1)

A contributor searching for where a function is used finds real callers, not a
dead definition. A contributor running a repository script finds it works and
points at a path that exists. Nothing in the tree is kept alive only by the fact
that nobody checked whether it runs.

**Why this priority**: Dead code is the cheapest thing to remove and the most
misleading thing to leave. A reader cannot tell an unused helper from a helper
they have not yet found the caller for, so every dead definition costs every
future reader the search. It is also the only story with zero risk of changing
behavior, so it can ship first and alone.

**Independent Test**: Run reachability analysis over the whole module and
confirm it reports nothing unreachable. Search the tree for any reference to the
removed script and confirm there is none outside historical spec records.
Delivers a tree where "no callers" reliably means "does not exist".

**Acceptance Scenarios**:

1. **Given** the repository at the current commit, **When** a contributor runs
   whole-module reachability analysis including tests, **Then** it reports zero
   unreachable functions.
2. **Given** a contributor looking for repository maintenance scripts, **When**
   they list the scripts that the build, the test suite, the CI workflows, or
   any current document reference, **Then** every script present in the tree
   appears in that list.
3. **Given** the cleanup is complete, **When** the full gate runs, **Then**
   formatting, linting, build, and tests all pass unchanged.

---

### User Story 2 - Each repeated fact has one home (Priority: P2)

A contributor fixing a bug in a shared behavior fixes it once. Today several
behaviors exist as two or more near-identical copies: two ways to ask whether a
writer is a terminal, three copies of the token-signing sequence, two copies of
the framework version check, two copies of the environment-lookup rendering, two
copies of the service-call POST, and five separate maps from the same four
primitive types to their target-language spelling. Each pair is a place where
a fix can land in one copy and miss the other.

**Why this priority**: This is the story the constitution's third principle is
written about, and the repository has already been burned by exactly this shape
of bug. It is higher value than the standard-library swaps below because a
divergent copy is a correctness risk, not only a reading cost.

**Independent Test**: For each duplicated behavior, confirm one definition
remains and every former call site reaches it. Confirm generated artifacts are
byte-identical to the goldens recorded before the change, with no golden file
regenerated.

**Acceptance Scenarios**:

1. **Given** two functions that answer the same question about the same input,
   **When** the cleanup is applied, **Then** one definition remains and both
   original call sites use it.
2. **Given** the three places that build a signed token, **When** the cleanup is
   applied, **Then** the marshal, header, signing, and encoding sequence exists
   once, and each caller supplies only its own claim shape.
3. **Given** the two framework version checks that differ only by a name and
   three constants, **When** the cleanup is applied, **Then** one check remains
   and each target passes its own name and constants.
4. **Given** any package with golden files, **When** the full test suite runs
   after the cleanup, **Then** every golden matches without being regenerated.

---

### User Story 3 - The standard library does the standard-library work (Priority: P3)

A contributor reading a helper sees either a well-known standard call or logic
specific to this project, never a hand-written re-creation of something the
language already ships. Today the tree hand-writes membership tests, element
removal, first-non-empty selection, sorted map keys, and version-tuple
comparison, in some cases directly next to code that already calls the standard
equivalent for the same job.

**Why this priority**: Lower risk and lower value than the duplication above,
because a hand-written copy that is correct today stays correct. It still earns
its place: the version in this tree that removes elements in place sits one line
away from a call to the standard removal function, and that inconsistency is the
kind of thing a reader has to stop and reason about.

**Independent Test**: Confirm each named helper is gone and its call sites use
the standard equivalent. Confirm the standard calls used are available at the
language version pinned in the module file, which the existing vet gate checks.

**Acceptance Scenarios**:

1. **Given** a hand-written membership test over a string list, **When** the
   cleanup is applied, **Then** call sites use the standard membership function
   and the hand-written one is gone.
2. **Given** two separately-written first-non-empty helpers in two packages,
   **When** the cleanup is applied, **Then** both are gone and roughly sixty
   call sites use the one standard equivalent.
3. **Given** the cleanup uses standard calls introduced after the language
   baseline, **When** the vet gate runs, **Then** it reports no use of an API
   newer than the module's declared language version.

---

### User Story 4 - No knob that only ever has one setting (Priority: P4)

A contributor reading a function signature learns something from every
parameter. Today one central resolution function takes a catalogue argument that
is the same value at every call site, and a rendering-function argument whose
two possible values produce identical output. A comment on the first says it
exists for a loader that has not been written. One small confirmation helper
forwards to another with an unchanged signature.

**Why this priority**: Small in lines, but it is the exact shape Governance
names as needing a stated reason or being refused. Lower priority than the
duplication work because a redundant parameter misleads a reader without risking
a wrong answer.

**Independent Test**: Confirm each named parameter and wrapper is gone and the
callers compile and behave identically. Confirm generated output is unchanged.

**Acceptance Scenarios**:

1. **Given** a parameter that receives the same value at every call site,
   **When** the cleanup is applied, **Then** the parameter is gone and the value
   is referenced directly inside the function.
2. **Given** a parameter whose distinct arguments produce identical output,
   **When** the cleanup is applied, **Then** the parameter is gone and the
   behavior is inlined.
3. **Given** a helper that forwards to another with an unchanged signature,
   **When** the cleanup is applied, **Then** the forwarding helper is gone and
   its callers reach the target directly.
4. **Given** a speculative extension point is removed, **When** a future change
   genuinely needs it, **Then** reintroducing it is a normal change, because
   nothing about the removal blocks it.

---

### User Story 5 - The console's repeated list screens share one flow (Priority: P5)

A contributor changing how a list screen behaves in the interactive console
changes it once, and every list screen picks it up. Today five list screens each
hand-write the same sequence: build one option per saved item, append an add
option and a back option, dispatch on a prefixed choice, and run a create path.
The five differ only in what they are listing and what its label reads.

**Why this priority**: The largest single reduction in this feature, but it
touches the interactive surface and so carries the most risk of a behavior
change that tests do not catch. It is last so the four safer stories can ship
without waiting for it, and so it can be dropped without affecting them.

**Independent Test**: Drive each list screen through the accessible renderer and
confirm the same screens, options, order, and labels appear as before. The
console is already testable this way with no terminal attached.

**Acceptance Scenarios**:

1. **Given** any of the five list screens, **When** a contributor opens it after
   the cleanup, **Then** its options, their order, and their labels are
   unchanged.
2. **Given** the shared list flow, **When** a contributor adds an item, edits it,
   and deletes it on any of the five screens, **Then** each step behaves as it
   did before.
3. **Given** the console's existing golden output, **When** the test suite runs,
   **Then** it matches without being regenerated.
4. **Given** the interactive path's dependency boundary, **When** the shared flow
   is introduced, **Then** the interactive path still imports no accessible-only
   form library.

---

### Edge Cases

- **A function that looks unreachable but is bound by name in a template.**
  Several helpers are registered into template function maps as bare names, so a
  search for a call with parentheses reports them as having no callers. Any
  removal must confirm the name is absent from template sources too, not only
  from code.
- **A function that is reachable only from tests.** Some helpers exist so a test
  can assert against them. These are not dead and must not be removed on
  reachability grounds alone; the distinguishing question is whether a test
  genuinely consumes them or merely re-asserts their own definition.
- **A colour token left with no reader.** Removing the two unreachable colour
  helpers leaves one token in the table with no consumer. The token table is the
  single source of colour and an unused token is not the same thing as dead
  code, so tokens stay and only the helpers go.
- **A golden file that changes.** Any difference in generated bytes means the
  cleanup changed behavior and is wrong. The correct response is to fix the
  change, never to regenerate the golden.
- **A standard-library call newer than the language baseline.** The vet gate
  fails on this before merge, so a replacement must use a call available at the
  pinned version.
- **Removing a helper whose only remaining caller is in the same commit.** The
  work must land so that the tree builds and the gate passes at each committed
  step, not only at the end.
- **A live session that starts and then dies on the first user turn.** The
  greeting criterion would call that a pass. It is accepted: identical generated
  bytes mean conversational behavior cannot have changed, and the smoke layer
  proves the emitted Python still imports and instantiates.
- **A secret present in `.env` but stale or revoked.** The pre-check confirms a
  name is present, not that the value still works. A revoked key surfaces as a
  failed session rather than a failed pre-check, so a failure has to be read
  before it is blamed on the refactor.
- **Docker unavailable or a container image that will not build.** Browser mode
  needs Docker. This blocks the sweep rather than failing the refactor, and it
  must be reported as blocked rather than recorded as a pass.
- **A sweep artifact that would carry a secret.** Reports name missing keys
  only. No log, transcript, or result file may contain a value, which is the
  same rule the packages themselves follow.

## Requirements *(mandatory)*

### Functional Requirements

**Behavior preservation (applies to every requirement below)**

- **FR-001**: The cleanup MUST NOT change what any command prints, what bytes it
  writes to a generated artifact directory, or what exit code it returns.
- **FR-002**: No golden file MAY be regenerated as part of this work. Every
  golden MUST match unchanged.
- **FR-003**: The full gate — formatting, linting, build, and tests — MUST pass
  at every committed step, not only at the end.
- **FR-004**: The opt-in smoke layer MUST still pass where it can run, proving
  emitted Python is unchanged and still valid.

**Live verification against the examples**

Golden files prove the generated bytes did not move. They do not prove the
tool's own runtime paths still work, because several of those paths have no
generated output to pin: terminal detection, the development server, container
orchestration, and the interactive console. The examples are the only place
those paths get exercised end to end, so they are the acceptance evidence.

- **FR-029**: Every example that needs no carrier credentials MUST be run
  through the browser development mode, on each target it declares. Seven
  examples qualify, declaring thirteen targets between them; the four that need
  a real carrier are excluded by FR-031.
- **FR-030**: A live run counts as passing when the development server starts,
  the browser session establishes, and the agent speaks its greeting. Spoken
  input is not required.
- **FR-030a**: Because no person has to speak, the acceptance run MUST be
  scripted rather than performed by hand, so that it is repeatable and so that
  a later refactor can reuse it unchanged.
- **FR-030b**: The greeting criterion deliberately does not cover what happens
  on the first user turn. That gap is accepted because FR-001 and FR-002 prove
  the generated agent code is byte-identical, so any conversational behavior
  that worked before still works, and the opt-in smoke layer independently
  proves the emitted Python imports and instantiates.
- **FR-031**: The four examples that declare a telephony channel MUST be
  verified by compiling only, and their generated bytes checked against FR-001
  and FR-002. They are not run live. Governance forbids credentialed carrier
  runs in the pull request gate, and these four need a real carrier account and
  a purchased number.
- **FR-032**: The live runs MUST NOT become part of the automated gate. They are
  a recorded pre-merge step, consistent with the existing rule that credentialed
  and container-dependent checks stay opt in.
- **FR-033**: The full thirteen-session sweep MUST run after every user story,
  not once at the end, so that a regression is attributed to the story that
  caused it rather than found after five have landed. Five stories give
  sixty-five sessions; four give fifty-two if User Story 5 is dropped.
- **FR-034**: A sweep MUST record which story it followed and its per-example,
  per-target result, so the run that first showed a regression can be
  identified afterwards.
- **FR-035**: Every secret an example declares MUST be present and hold a real
  value for that example's sessions to count. Nothing is skipped and no
  placeholder is accepted: a missing or fake value fails the sweep.
- **FR-036**: Real values come from the untracked `.env` at the repository root,
  which the tool's existing environment loader already reads when a run starts
  there. No file is copied. That `.env` MUST stay untracked, and no sweep
  report, log, or spec artifact may contain a secret value. Reports name missing
  keys only.
- **FR-037**: The sweep MUST check every required name is present before it
  builds the first container, so a missing key is reported in seconds rather
  than after a long series of failed sessions.
- **FR-038**: `FIRECRAWL_MCP_URL` is declared by the MCP example and is absent
  from the root `.env` today. It MUST be supplied before the sweep can pass,
  because under FR-035 its absence fails both of that example's sessions. This
  is a precondition to resolve, not a reason to weaken FR-035.

**Unreachable code (User Story 1)**

- **FR-005**: Every function unreachable from both the command entry point and
  the test suite MUST be removed.
- **FR-006**: Before removing any function, its name MUST be confirmed absent
  from template sources as well as from code, because template function maps
  bind helpers by bare name.
- **FR-007**: The orphaned repository maintenance script MUST be removed. It is
  referenced by no build target, no workflow, and no current document, and its
  default argument names a directory that was renamed.

**Single home per fact (User Story 2)**

- **FR-008**: The two terminal-detection helpers in the command package MUST
  collapse to one.
- **FR-009**: The token-signing sequence, currently written three times, MUST
  exist once, with each caller supplying only its own claim shape.
- **FR-010**: The two framework version checks MUST collapse to one that takes
  the target name and its version constants.
- **FR-011**: The two environment-lookup rendering helpers, which return
  identical output, MUST collapse to one.
- **FR-012**: The two service-call POST helpers, which differ only by one path
  segment, MUST collapse to one that takes the segment.
- **FR-013**: The five separate mappings over the four primitive types MUST be
  driven from one table keyed by primitive type, carrying the JSON name, the
  target-language name, and the runtime check expression. These are three
  different outputs over one key set, not one mapping written five times, so the
  accessors remain; the single source is the table behind them. Merging the
  outputs themselves would be wrong.
- **FR-014**: The two sorted-map-keys helpers in different packages MUST agree,
  using the standard approach the existing correct one already uses.

**Standard library (User Story 3)**

- **FR-015**: Hand-written list membership testing MUST be replaced by the
  standard equivalent.
- **FR-016**: Hand-written list element removal MUST be replaced by the standard
  equivalent, which neighbouring code already uses for the same job.
- **FR-017**: Both hand-written first-non-empty helpers MUST be replaced by the
  standard equivalent at every call site.
- **FR-018**: Hand-written version-tuple comparison MUST be replaced by the
  standard sequence comparison.
- **FR-019**: Every standard call introduced MUST be available at the language
  version declared in the module file.

**Single-value parameters and wrappers (User Story 4)**

- **FR-020**: The catalogue parameter that receives the same value at every call
  site MUST be removed, along with the comment describing the unbuilt loader it
  was reserved for.
- **FR-021**: The environment-rendering function parameter, whose arguments
  produce identical output, MUST be removed.
- **FR-022**: The confirmation helper that forwards with an unchanged signature
  MUST be removed and its callers pointed at the target.
- **FR-023**: The named empty-interface type MUST be replaced by the language's
  built-in equivalent.

**Shared console list flow (User Story 5)**

- **FR-024**: The five repeated list screens MUST share one flow, parameterised
  by what is listed and how each entry is labelled.
- **FR-025**: The shared flow MUST produce the same screens, options, option
  order, and labels as the five it replaces.
- **FR-026**: The interactive path MUST continue to import no accessible-only
  form library.

**Recording the work**

- **FR-027**: Any simplification whose reasoning is not obvious from the
  resulting code MUST carry a short comment stating the intent, so that simple
  reads as deliberate rather than as an oversight.
- **FR-028**: Because no authoring surface changes, this work MUST NOT add a
  schema amendment, a capability row, or a scaffold template change. If any
  proposed step would require one, that step is out of scope by definition and
  MUST be dropped.

### Key Entities

- **Finding**: One audited item. Carries the location, what is removed, what
  replaces it, and which user story it belongs to.
- **Behavior baseline**: The recorded output of the tool before the change —
  command output, generated artifact bytes, exit codes. Every finding is checked
  against it.
- **Reachability report**: The list of functions no entry point can reach, used
  to open and to close User Story 1.
- **Sweep**: One full pass of the live acceptance run. Carries the user story it
  followed and one result per example and target: passed, failed, or blocked.
  Thirteen sessions, and five sweeps across the feature.
- **Session**: One example compiled and run on one target in browser mode.
  Passes when it establishes and the agent speaks its greeting.
- **Required secret set**: The union of the names the seven runnable examples
  declare. Names only, never values, checked for presence before any container
  builds.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Generated artifacts are byte-identical before and after. Zero
  golden files are regenerated.
- **SC-002**: Reachability analysis over the whole module, including tests,
  reports zero unreachable functions. It reports two today.
- **SC-003**: Maintained non-test Go shrinks by at least 150 lines without User
  Story 5, and by at least 300 lines with it, from a starting point of 20,901,
  with no behavior change. User Story 5 is roughly half the total on its own.
  Separately, one 64-line shell script is deleted; it is not Go and does not
  count toward these figures.
- **SC-004**: Every behavior that is currently written more than once in the
  eighteen locations named in FR-008 through FR-014 is written exactly once.
- **SC-005**: The gate passes at every committed step, and the opt-in smoke
  layer passes where it can run.
- **SC-006**: Every user story can be reverted on its own without touching
  another, confirming the five are genuinely independent.
- **SC-007**: No file under the documentation trees, the examples, or the
  authoring schema changes, confirming the work stayed internal.
- **SC-008**: All seven credential-free examples establish a browser session and
  speak their greeting on every target they declare: thirteen passing live
  sessions, six examples declaring two targets and one declaring one. The four
  carrier examples compile with unchanged bytes.
- **SC-009**: The acceptance run is a single scripted command that reports pass
  or fail per example and target, with no human speaking into a microphone.
- **SC-010**: Every user story is followed by a full passing sweep, giving
  sixty-five green sessions across the feature. Any regression names the story
  that introduced it, with no bisecting.
- **SC-011**: Every sweep runs with a complete set of real secrets. Zero
  sessions are skipped for a missing credential, and no sweep artifact contains
  a secret value.

## Out Of Scope

Three audit findings are refused here, each because a governing rule requires
the thing the audit proposed removing. They are recorded so the same proposal is
not made again.

- **The `vapi` and `deepgram` capability columns and the driverless catalogue
  stay.** The audit read them as speculative because neither provider has a
  generator. That is the wrong reading. Validation is deliberately wider than
  generation: all four providers validate so that portability is checkable
  before an author commits to a platform, and `validate` is stated to be the one
  command whose reach is all four. A capability row for a driverless target is
  named as normal and correct, and the driverless catalogue is named as exactly
  what a catalogue for such a target should look like. The provider set is also
  fixed at four. Removing any of this would need a constitutional amendment, not
  a cleanup.
- **The derived-schema machinery and its dependency stay.** The audit noted the
  schema derivation is reachable only from tests and proposed dropping the
  dependency. Deriving both schemas by reflection from the Go structs is a stated
  principle, hand-authoring a schema file is forbidden, and the dependency is
  named directly in the allowed set with instructions to pin and bump it
  deliberately. Test-only reachability is not grounds to remove a mandated
  derivation.
- **The overlap between the two documentation trees stays.** The audit measured
  real duplication across the normative documents and the published site. That
  duplication is required: the normative documents outrank everything on what a
  field means, and a surface change must update both in the same commit. This
  feature touches no documentation at all.

Two further items are deferred rather than refused:

- **The vendored browser client** adds roughly 540KB to the binary. Moving it to
  a hosted copy is a real reduction and adds no new class of failure, since the
  development UI already requires network access to work. It is a product
  decision about offline behavior and version drift, not a cleanup, so it needs
  its own spec.
- **Stale document references.** Several source comments cite paths under a
  documentation subdirectory that does not exist. Worth fixing, but it is a
  documentation correction and this feature deliberately changes no documents.

## Assumptions

- The audience for this work is repository contributors. There is no end-user
  visible change, and that is the point rather than a limitation.
- The behavior baseline is trustworthy: the test suite passes at the current
  commit, and the golden files genuinely pin the generated output. Both were
  confirmed before this spec was written.
- Language-version safety is already enforced. The existing vet gate fails on a
  standard call newer than the declared baseline, so replacements do not need a
  separate check.
- Colour tokens are kept even where a token loses its last reader, because the
  token table is the single source of colour and an unused token is not dead
  code. Only the two unreachable helpers are removed.
- User Story 5 is the one story that may be dropped on risk grounds without
  affecting the others. SC-003 therefore carries two figures rather than one,
  because User Story 5 is roughly half the total reduction; a single bar would
  either be unreachable without it or trivially met with it.
- No new dependency is added. Every replacement named here is either the
  standard library or existing code.
- The work lands as a sequence of small changes, one story at a time, rather
  than a single large change, so that each step is separately reviewable and
  separately revertible.
- The person running the sweep has Docker available and holds every credential
  the seven examples declare. The untracked `.env` at the repository root
  supplies twenty-four of the twenty-five names needed; only
  `FIRECRAWL_MCP_URL` has to be added, per FR-038.
- Sixty-five sessions is affordable only because the pass criterion needs no
  person at a microphone. If the criterion were ever deepened to require spoken
  input, the per-story cadence would have to be revisited with it.
- The four carrier examples stay compile-only by choice, not for want of
  credentials. The root `.env` does hold Twilio, SIP, and Daily values, so
  extending the sweep to a live telephony route later is possible without new
  accounts.
- Tracing credentials are treated as ordinary required secrets. Although this
  refactor touches no tracing code, the sweep supplies real Langfuse values
  rather than placeholders, so a session never fails for a reason unrelated to
  the change under test.

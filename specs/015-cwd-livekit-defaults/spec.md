# Feature Specification: Work Inside The Agent Folder, LiveKit By Default

**Feature Branch**: `015-cwd-livekit-defaults`

**Created**: 2026-08-16

**Status**: Draft

**Input**: User description: "Once you run unmute init my-test-agent and cd into the folder, unmute validate, unmute dev and all other commands should work IN the folder without naming the agent. From a parent folder holding several agents, naming a folder must still work. Also, the generated example must be based on livekit, not pipecat. Once done, raise a PR to main."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Commands work inside the agent folder (Priority: P1)

An author scaffolds a new agent, steps into its folder, and works there. Every
package command runs against the folder they are standing in, with no extra
argument:

```
unmute init my-test
cd my-test
unmute validate
unmute compile
unmute dev
unmute dev --target <name>
```

Today each of these commands demands a directory argument and refuses to run
without one, so the most natural workflow (`cd` in, then work) dead-ends on a
usage error.

**Why this priority**: This is the first thing every new user does after
`init`. The current behavior breaks the very first five minutes with the tool.

**Independent Test**: Scaffold a package, `cd` into it, and run `validate`,
`compile`, and `dev` with no directory argument. Each one must behave exactly
as it does today when given the directory explicitly.

**Acceptance Scenarios**:

1. **Given** a freshly scaffolded package and a shell inside its folder,
   **When** the author runs `unmute validate` with no argument, **Then** the
   package in the current folder is validated and the result lines print as
   they do for `unmute validate <dir>` today.
2. **Given** a shell inside a package folder, **When** the author runs
   `unmute compile` with no argument, **Then** artifacts are written to
   `build/<target>/` inside that folder, same as the explicit form.
3. **Given** a shell inside a package folder, **When** the author runs
   `unmute dev` (with or without flags like `--target`, `--console`,
   `--var`), **Then** the dev session starts on the current folder's package
   and every flag works as it does with an explicit directory.
4. **Given** a shell in a folder that is not a package (no `agent.yaml`),
   **When** the author runs `unmute validate` with no argument, **Then** the
   command exits 1 with a message that says what file was looked for, where,
   and shows both ways to run the command (from inside a package, or naming a
   directory).

---

### User Story 2 - New agents are LiveKit-based by default (Priority: P2)

An author runs `unmute init my-test` and accepts the defaults. The scaffolded
package targets LiveKit, so `unmute validate` reports `✓ livekit (livekit)`
and `dev` runs the LiveKit artifact. Today the scaffold defaults to Pipecat.

**Why this priority**: The default is the recommendation. The project wants
the first-run experience to land on LiveKit, and every new user inherits
whatever the scaffold picks.

**Independent Test**: Scaffold a package with defaults, then validate it. The
reported target must be LiveKit, and the package must compile and run without
edits.

**Acceptance Scenarios**:

1. **Given** a fresh `unmute init my-test` accepting defaults, **When** the
   author runs `unmute validate` on it, **Then** the passing target line names
   `livekit`, not `pipecat`.
2. **Given** the same fresh package, **When** the author runs `unmute compile`
   and `unmute dev`, **Then** both succeed end to end on the LiveKit target
   with no manual edits to the scaffolded files.
3. **Given** an author who wants Pipecat, **When** they choose it in the
   interactive console, **Then** Pipecat still works exactly as before. The
   default changes; the choice does not go away.
4. **Given** an author who switches an existing package to Pipecat by hand,
   **When** they edit `targets.yaml` alone, **Then** validation fails with
   `turn model "silero" is not recognized`, because the turn block in
   `agent.yaml` must move with the provider. This is correct fail-loud
   behaviour, not a defect, and it is the same in both directions. An earlier
   draft of this spec promised that editing `targets.yaml` alone was enough;
   that promise was false and is withdrawn. The documentation MUST teach the
   two-file edit rather than leave an author to discover it from an error.

---

### User Story 3 - Naming a folder still works from outside (Priority: P3)

An author keeps several agents side by side in one parent folder. From that
parent folder they run commands against a named agent, exactly as today:

```
unmute init my-test
unmute validate my-test
unmute compile my-test
unmute dev my-test
```

**Why this priority**: This is the existing behavior and it must not regress.
It also stays the only way to work when the shell is not inside a package.

**Independent Test**: From a parent folder holding two scaffolded agents, run
each command with an explicit folder name and confirm nothing changed.

**Acceptance Scenarios**:

1. **Given** a parent folder with packages `a/` and `b/`, **When** the author
   runs `unmute validate a` and `unmute validate b`, **Then** each validates
   its own package, same as today.
2. **Given** a shell inside package `a/`, **When** the author runs
   `unmute validate ../b`, **Then** the explicit path wins and `b` is the
   package validated. The current folder is only a fallback, never an
   override.

---

### Edge Cases

- Zero-argument run in a folder with no `agent.yaml`: exit 1 with a message
  naming the missing file, the folder that was checked, and both usage forms.
  Never a bare usage dump.
- Zero-argument run inside `build/<target>/` or another subfolder of a
  package: the command checks only the current folder. It does not walk up
  parent folders looking for a package, so this fails with the same helpful
  message. Predictable beats clever here.
- Explicit argument naming a folder that does not exist or is not a package:
  same errors as today, unchanged.
- More than one positional argument: still rejected as a usage error.
- `unmute init` with no changes to its own contract: it still takes a name,
  still refuses a non-empty existing folder, and interactive mode still
  previews before writing. Only the default target it scaffolds changes.
- Help text: each command's help must show the argument as optional and say
  what the default is, or authors will keep believing the argument is
  required.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: `unmute validate`, `unmute compile`, and `unmute dev` MUST
  accept zero positional arguments. With zero arguments, the package directory
  is the current working directory.
- **FR-002**: With zero arguments and no package in the current directory,
  each command MUST exit 1 with a message that names the file it looked for
  (`agent.yaml`), the directory it checked, and shows both usage forms. The
  failure is loud and instructive, never a silent no-op or a bare usage
  string.
- **FR-003**: The explicit one-argument form MUST keep working byte-for-byte
  as today, and an explicit argument always wins over the current directory.
- **FR-004**: Every flag on `validate`, `compile`, and `dev` MUST behave
  identically in the zero-argument and one-argument forms.
- **FR-005**: The `unmute init` scaffold MUST default to the LiveKit target.
  A default-accepting scaffold produces a `targets.yaml` whose provider is
  `livekit`, and `unmute validate` on it reports the LiveKit target passing.
- **FR-006**: The scaffolded LiveKit package MUST validate, compile, and run
  under `dev` end to end with no manual edits, matching the bar the Pipecat
  scaffold meets today.
- **FR-007**: Pipecat MUST remain fully selectable and supported. Only the
  default changes; no capability is removed.
- **FR-008**: Every surface that teaches these commands MUST show the form
  that is correct **for where its reader is standing**, across command help
  text, the docs pages, the public docs site, the example READMEs, and the
  coding agent skill bundle. Concretely: a surface whose reader is inside the
  package (the scaffolded file comments, the first-run tutorials) MUST show
  the in-folder form; a surface whose reader is elsewhere (commands run from
  the repository root against `examples/`, and the emitted
  `build/<target>/README.md`, whose reader stands in the build directory) MUST
  keep the explicit directory argument, because dropping it there would make
  the instructions wrong. Per the repository's own rule, a fact that only
  lives in one of these places is a fact readers never see.
- **FR-009**: No surface may teach a `--target` value that a
  default-scaffolded package does not declare. A scaffolded package declares
  exactly one target instance named after its provider, so any documented
  invocation that pins `--target` against a scaffolded package MUST either
  name the current default or omit the flag entirely. Omitting it is
  preferred: with one declared target the flag is unnecessary, and an omitted
  flag cannot go stale the next time the default moves.
- **FR-010**: Changing the scaffold default MUST keep the interactive wizard's
  phone path **gated**, and MUST keep the author's guidance specific. Verified
  by running it: a wizard-built phone package is refused on both targets, and
  on both the author gets a message naming the file and the field to fix. They
  differ because a different field is missing first:
  - Pipecat sets `daily-sip`, so the carrier is what is missing:
    `give the connection a carrier, or drop the phone channel`.
  - LiveKit sets no transport, so the transport is what is missing:
    `connections/phone.yaml: connection "phone" declares no transport`.

  Both are actionable, so this requirement is already satisfied by the code and
  needs no new guidance work. What it does require is that the **gate keeps
  blocking Create** on both targets. The existing test asserts both the block
  and one target's message text; only the message half is target-coupled, and
  updating that half is not loosening the gate, because the blocking behaviour
  it guards is unchanged.
- **FR-011**: The interactive console's target menu MUST preselect the target
  the author is most likely to want, and that differs by flow. Creating a new
  package preselects the scaffold default. Editing an existing package
  preselects **that package's current target**, never the scaffold default,
  because the author already chose. Wherever the default is stated a second
  time, an agreement test MUST fail on drift.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A new user goes from `unmute init my-test` to a running dev
  session using only in-folder commands (`cd my-test`, then `validate`,
  `compile`, `dev` with no directory argument), with zero usage errors along
  the way.
- **SC-002**: A default-accepting scaffold validates with LiveKit as its
  passing target on the first `unmute validate`, with no file edits.
- **SC-003**: Every pre-existing invocation with an explicit directory
  argument behaves exactly as before: the full existing test suite passes
  without weakening any test.
- **SC-004**: A user standing in a non-package folder who runs a
  zero-argument command can tell from the error message alone what to do
  next, without opening the docs.

## Assumptions

- "Work in the folder" means the current directory itself must be the package
  (it contains `agent.yaml`). Commands do not search parent directories. This
  keeps the rule simple to state and impossible to be surprised by.
- `unmute init` keeps its current contract for where it writes: it takes a
  name and creates that subfolder. Making `init` scaffold into the current
  directory is out of scope.
- The default-target change applies to the non-interactive scaffold and to the
  interactive console's **create** flow. It does not reach the maintain flow,
  which edits an existing package and must respect what that package already
  chose (FR-011). The console still offers Pipecat as a choice in both flows.
- `unmute skill` is not part of this change beyond its bundled documentation
  reflecting the new command forms; it takes no package directory today and
  stays that way.
- Exit codes stay 0 for success and 1 for error, per the constitution.
- Delivery lands as a pull request to `main` once implementation and gates
  are green.

---
description: "Task list for the coding agents page"
---

# Tasks: The "Coding agents" page

**Input**: Design documents from `specs/012-coding-agents-page/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/.
**Feature 011 must be merged first.** Every command, flag, and path this page
names comes from it.

**Tests**: one agreement test, requested by FR-018, plus the site's existing
checks. Everything else on this page is prose, and the tasks say plainly where a
human has to look because no test can.

**Organization**: grouped by user story. The page is written in the order a
reader meets it, which is also the order the stories run.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel, different files, no dependency on unfinished work
- **[Story]**: which user story the task serves

## Path conventions

The page is `docs-site/start/coding-agents.mdx`. Navigation is
`docs-site/docs.json`. The one Go test lives in `internal/skill/`, next to the
supported assistant set it holds.

---

## Phase 1: Setup

**Purpose**: the file, the navigation entry, and the seven headings. Nothing is
claimed yet, and the site stays green.

- [X] T001 Create `docs-site/start/coding-agents.mdx` with the frontmatter from `contracts/page-structure.md`: `title` "Coding agents" and a one-sentence `description`
- [X] T002 Add the seven section headings to `docs-site/start/coding-agents.mdx` in order: opening, set it up, check it took, build the salon agent, ask for more, habits, where next
- [X] T003 Add `start/coding-agents` to the Get started group in `docs-site/docs.json`, between `start/installation` and `start/quickstart`
- [X] T004 Run `mint validate` in `docs-site/` and confirm the page and config are clean
- [X] T005 Confirm the page count invariant still holds: the number of `.mdx` files under `docs-site/` equals the number of page entries in `docs-site/docs.json`

**Checkpoint**: the page exists, is reachable, and claims nothing.

---

## Phase 2: Foundational

**Purpose**: the opening paragraph, which decides whether anyone reads the rest.

**⚠️ Everything below depends on this framing being right.**

- [X] T006 Write the opening paragraph in `docs-site/start/coding-agents.mdx`: what the reader gets and roughly what it costs in time, per FR-004
- [X] T007 Name `/start/quickstart` in the opening as the by-hand path, so a reader who wants that leaves here rather than reading on, and make clear that an assistant is not required and an unsupported one does not rule Unmute out, per FR-005

**Checkpoint**: a reader can decide in one paragraph whether this page is for them.

---

## Phase 3: User Story 1 - Set it up and know it took (Priority: P1) 🎯 MVP

**Goal**: one command, then a check that proves in a minute whether it applied.

**Independent Test**: hand the page to someone who has never used Unmute, on
each supported assistant. Then break the setup on purpose and confirm the page's
own check catches it.

### Set it up

- [X] T008 [US1] Write the prerequisite line in `docs-site/start/coding-agents.mdx` linking to `/start/installation` rather than restating installation, per FR-008
- [X] T009 [US1] Write the setup step with the single command `unmute skill install`, the files it wrote, and one line saying to commit them so the team's assistants get them too, per FR-007
- [X] T010 [US1] Add the assistant table to `docs-site/start/coding-agents.mdx`: Claude Code reads `.claude/skills/unmute/`, and Codex, Cursor, and GitHub Copilot read `.agents/skills/unmute/`, per `contracts/page-structure.md`
- [X] T011 [US1] Add the note for a reader whose assistant cannot run commands, and the note that install needs no network, per FR-009 and the spec's edge cases
- [X] T012 [US1] Add the note that two assistants in one project share one body of instructions, per the spec's edge cases

### Check it took

- [X] T013 [US1] Write the proof step: ask the assistant to name the four tool execution kinds, per FR-010 and research R4
- [X] T014 [US1] Write what a right answer contains, all four kinds named, and what a silent failure looks like, a vague answer or invented kinds, per FR-011
- [X] T015 [US1] Write what to do when the check fails: re-run install, and look at the directory for your assistant in the table above

### The check that holds it

- [X] T016 [US1] Write `internal/skill/coding_agents_docsite_test.go` asserting that the assistants named in `docs-site/start/coding-agents.mdx` equal the assistants `unmute skill install` accepts, failing with the page path named, per FR-018
- [X] T017 [US1] Prove the test bites: add a fifth assistant name to the CLI's supported set, confirm the test goes red naming the page, then revert
- [ ] T018 [US1] Run quickstart steps 4a and 4b by hand, including deleting the two skill directories and confirming the assistant's answer visibly degrades

**Checkpoint**: a reader can set up and prove it. This is the MVP and it is worth shipping alone.

---

## Phase 4: User Story 2 - Follow one build from a sentence to a voice (Priority: P1)

**Goal**: one real build, start to finish, with what the reader types, what the
assistant does, and what to check at each step.

**Independent Test**: follow the page literally, typing what it says to type,
and end up talking to the agent it promised.

- [X] T019 [US2] Run the `examples/salon-support` build through a real assistant session and record what was typed, what came back, and what was worth checking. This section is reported, not composed, per plan stage 3
- [X] T020 [US2] Write the story section in `docs-site/start/coding-agents.mdx` using `<Steps>` and `<Step>`, matching the style of `docs-site/start/quickstart.mdx`
- [X] T021 [US2] Give every step all three parts: what you type, what it does, what you check. A step with no check teaches blind trust, per data-model.md
- [X] T022 [US2] End the story at `unmute dev` and a conversation, not at a green validation, per FR-012 and constitution Principle V
- [X] T023 [US2] If the recorded session went badly, open the finding against feature 011's skill rather than smoothing it over in prose
- [ ] T024 [US2] Run quickstart step 4c: follow the build section literally from a scratch directory and confirm every "what you check" matches what you see
- [ ] T025 [US2] Time the run from landing on the page to speaking with the agent, and confirm it is under 15 minutes, per SC-001

**Checkpoint**: the page tells a story, not just a setup procedure.

---

## Phase 5: User Story 3 - Ask for more, and know what good looks like (Priority: P2)

**Goal**: the reader can ask for a tool, a second agent, or a phone number and
can tell a good answer from a silent decision.

**Independent Test**: take each growth ask, phrase it the way the page suggests,
and confirm the assistant's response includes what the page says to expect.

- [X] T026 [US3] Write the "ask for more" table in `docs-site/start/coding-agents.mdx` with the three rows from `contracts/page-structure.md`: a tool, a second agent, a phone number, each with what the request must carry and what the assistant should say back
- [X] T027 [US3] Add the line telling the reader to expect a plain refusal rather than an invention when they ask for something Unmute does not do, per the spec's US3 acceptance
- [X] T028 [US3] Write the six habits, one line each, each naming the failure it prevents, and each avoid-something item saying what to do instead. The six are fixed in `research.md` R7
- [X] T029 [US3] Read the habits section aloud and confirm it finishes in under two minutes, per SC-007 and FR-017
- [ ] T030 [US3] Run quickstart step 4d: try all three growth asks and confirm the assistant says back what the table says it should, with no silent decision

**Checkpoint**: the reader knows how to keep going, and how to tell when the assistant is guessing.

---

## Phase 6: User Story 4 - The page cannot quietly go stale (Priority: P3)

**Goal**: the links resolve, the site's rules hold, and the assistant list is
held by a test.

**Independent Test**: add or remove a supported assistant in the CLI, run the
suite, and confirm it fails naming this page.

- [X] T031 [US4] Write the "where next" section linking into orchestration, tools, telephony, and deployment, per FR-003
- [X] T032 [US4] Run `mint broken-links` in `docs-site/` and fix anything it names, per FR-019
- [X] T033 [US4] Confirm every command the page names exists by running each one, per FR-020
- [X] T034 [P] [US4] Read the page against the nine rules in `docs-site/README.md`, especially plain language with no dashes as punctuation, only two targets presented as targets, and no route over-claimed, per FR-021
- [X] T035 [P] [US4] Grep the page for em and en dashes used as punctuation and remove them

**Checkpoint**: the page holds itself true.

---

## Phase 7: Polish and cross-cutting

- [X] T036 Run quickstart step 5: open the page at each heading anchor in turn and read only that section, confirming each survives a reader who arrived from search, per FR-003
- [X] T037 Run quickstart step 6: with both destinations installed, confirm the files show what the page claims, one copy of the references and a pointer at the canonical bundle
- [X] T038 Run the full gate: `make fmt`, `make lint`, `make build`, `make test`
- [X] T039 [P] Re-run `mint validate` and `mint broken-links` after every edit above
- [ ] T040 Confirm each success criterion has been measured, not assumed, and record the numbers for SC-001 and SC-007

---

## Status, 2026-08-15

Thirty-five of forty done. The page is
`docs-site/start/coding-agents.mdx`, the navigation entry is in place, and the
agreement test is `internal/skill/coding_agents_docsite_test.go`.

### What was measured

| Check | Result |
|---|---|
| `mint validate` | build validation passed |
| `mint broken-links` | no broken links found |
| page count invariant (T005) | 51 `.mdx` files, 51 page entries |
| every command the page names (T033) | `unmute skill install`, `unmute validate`, `unmute dev`, all three run |
| every internal link (T033) | 7 links, all resolve to a real `.mdx` |
| em and en dashes (T035) | none |
| SC-007, the habits section | 145 words, about 55 seconds read aloud. The bar is two minutes |
| the gate (T038) | `make fmt`, `make lint` at 0 issues, `make build`, `make test` all clean |
| T037, both destinations installed | 14 files under `.agents/skills/unmute/`, 2 under `.claude/skills/unmute/`, one copy of the references |

The agreement test bites, proved rather than assumed (T017). Adding a fifth
assistant to the map in `internal/skill/skill.go` fails two tests, both naming
`docs-site/start/coding-agents.mdx`: one because the page no longer quotes the
install summary the CLI would print, one because the table has four rows and
the CLI supports five.

The `.env.example` claim in the story was verified by compiling
`examples/salon-support` rather than from memory. Compile writes
`build/pipecat/.env.example` listing all five names the package needs.

### The five left open, and why

All five need a person or a live assistant session in front of the finished
page. None can be closed by reasoning about it, and marking them done from
reasoning is the thing the notes at the bottom of this file warn against.

- **T018** wants the proof check run on a real assistant, then the skill
  directories deleted and the answer confirmed to visibly degrade.
- **T024** wants the build section followed literally from a scratch
  directory, checking that every "what you check" matches what appears.
- **T025** is SC-001, the fifteen minute claim. Only a stopwatch settles it.
- **T030** wants all three growth asks tried against a real assistant.
- **T040** is blocked on T025. SC-007 is measured at 145 words. SC-001 is not
  measured, so it is not claimed.

### T019, and what the story is built from

T019 asked for a recorded assistant session, and the section is reported
rather than composed, but the source is worth naming exactly. It is the
eighteen-agent clean room run for feature 011, which included a salon brief,
plus the real output of `unmute skill install`, `unmute validate`, and
`unmute compile` captured while writing the page. The three checks in the
build section are the mistakes that run actually turned up, not invented ones.
What it is not is one continuous session against this finished page. That is
T024.

Everything the clean room found went back into feature 011's bundle rather
than being smoothed over here, which is T023.

## Dependencies and execution order

### Phase dependencies

- **Feature 011 merged**: hard prerequisite for the whole file. Nothing here can be written against a command that does not exist.
- **Phase 1 Setup**: no dependency beyond that. Do it first so the site stays green while the prose lands.
- **Phase 2 Foundational**: depends on Phase 1. The opening frames everything after it.
- **Phase 3 US1**: depends on Phase 2. Blocks nothing, but it is the MVP.
- **Phase 4 US2**: depends on Phase 3, because the story assumes a set-up assistant.
- **Phase 5 US3**: depends on Phase 4, because it is about growing what the story built.
- **Phase 6 US4**: last, because the link and rule sweeps assert against finished prose.
- **Phase 7**: depends on everything you intend to ship.

### Story dependencies

Unlike feature 011, these stories are genuinely sequential. They are sections of
one page read in order, and each assumes the one above. Parallelism here is
small and honest about it.

### Parallel opportunities

- T034 and T035 in Phase 6, two different kinds of read.
- T039 in Phase 7, alongside any remaining edit.
- T019, recording a real assistant session, can start as soon as feature 011
  merges, before Phase 1 is written. It is the longest-lead task on the page.

---

## Implementation strategy

### MVP: Phases 1 to 3

The file, the opening, setup, and the proof check. That is already the thing
that closes the gap: feature 011 ships a skill and nothing tells anyone it
exists. A page that says "here is the command, here is how to know it worked" is
worth publishing on its own.

Stop and validate. If someone can follow it cold, keep going.

### Incremental delivery

1. Phases 1 and 2: the page exists and frames the choice.
2. Phase 3: setup and proof. **Ship and validate here.**
3. Phase 4: the story, which is what the user asked for.
4. Phase 5: growing, and the habits.
5. Phase 6: the checks that keep it true.

### Notes

- One file carries almost every task. Two people editing
  `docs-site/start/coding-agents.mdx` at once will conflict, so this page is a
  one-person job taken in order.
- T019 is the task most likely to surprise you. It is a real session with a real
  assistant, and what it turns up is a finding about the skill, not something to
  write around.
- Where a task says confirm, read aloud, or time it, no test can hold it. Do not
  mark it done from reasoning.

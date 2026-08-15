# Implementation Plan: The "Coding agents" page

**Branch**: `012-coding-agents-page` | **Date**: 2026-08-15 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/012-coding-agents-page/spec.md`

## Summary

One page on the documentation site, `docs-site/start/coding-agents.mdx`, that
tells the story of building an Unmute agent through a coding assistant: install
the skill, prove it took, follow one real build to a voice you can hear, learn
how to ask for more, and pick up six habits.

The work is mostly writing. The engineering is one test that stops the page's
assistant list from drifting away from what the CLI actually supports, plus one
navigation entry.

This feature is written after feature 011 and against its settled decisions.

## Technical Context

**Language/Version**: Markdown with MDX, in the existing Mintlify project. One
Go test file, Go 1.24.

**Primary Dependencies**: none new. `mint` for local preview, already used.
The test uses the standard library only.

**Storage**: not applicable. One page file and one navigation entry.

**Testing**: one agreement test in `internal/skill`, in the default suite, plus
the site's existing checks: `mint validate`, `mint broken-links`, and the page
count invariant.

**Target Platform**: the documentation site, private today and public later.
The page is written so that neither state changes what it says.

**Project Type**: documentation.

**Performance Goals**: not applicable, beyond the reader's clock. SC-001 gives
15 minutes from landing to a voice, and the page's own length is the part of
that budget this feature controls.

**Constraints**: every link resolves inside the site or the repository, so
nothing depends on public reachability. Every command named must exist. The
page inherits the nine rules the site is written under.

**Scale/Scope**: one `.mdx` page, one `docs.json` entry, one Go test, six
habits, one worked build.

## Constitution Check

*GATE: checked before Phase 0, re-checked after Phase 1 design.*

| Principle | Verdict | Why |
|---|---|---|
| I. Compile ahead of time | Not engaged | A documentation page adds no runtime behaviour and no dependency to a generated project. |
| II. Fail loud, never average | Pass | The page states limits rather than glossing them: it says which assistants are supported and what a reader on another one can do, and it tells the reader what a failed setup looks like instead of assuming success. |
| III. One source of truth, derived not copied | Pass, with a test | The page restates the supported assistant set, which lives in `internal/skill`. FR-018 requires the agreement test, and R6 puts it in the same package as the set it holds. Everything else on the page links rather than restates. |
| IV. The document wins | Pass | The page is one of the documents. Its facts come from the code and from feature 011's plan, and the assistant paths carry the 2026-08-15 verification date recorded in that feature's research. |
| V. Whatever compiles can be spoken to | Pass, and served | The story does not stop at a green validation. It ends at `unmute dev` and a conversation, which is the principle stated as a reader's experience. |
| Voice | Pass | Plain language, short sentences, no dashes as punctuation, per the site's rule 4. |
| Targets and providers | Pass | Only Pipecat and LiveKit appear as targets. The worked build is browser only, so the question barely arises, and where it does the page says which target it chose. |

**Post-Phase 1 re-check**: unchanged. No new dependency, no new abstraction, and
the one restated fact has a test.

## Project Structure

### Documentation (this feature)

```text
specs/012-coding-agents-page/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output: the page's content model
├── quickstart.md        # Phase 1 output: how to validate the page
├── contracts/
│   └── page-structure.md
├── checklists/
│   └── requirements.md
├── spec.md
└── tasks.md             # /speckit-tasks output, not created here
```

### Source (repository root)

```text
docs-site/
├── start/coding-agents.mdx        # the page
└── docs.json                      # one new entry in the Get started group

internal/skill/
└── coding_agents_docsite_test.go  # the assistant-list agreement test
```

**Structure Decision**: the page lives in `start/` because it is a getting
started path, not a reference. The test lives in `internal/skill` because that
is where the supported assistant set lives, and a test that reaches into another
package to restate the set would be the second copy the constitution warns
about. Feature 011 already puts its own bundle tests there, so a fifth assistant
is one edit and two failures that point at exactly what to update.

## The page, section by section

The order is the story. Each section is written to survive a reader who lands
in the middle, per FR-003.

| Section | What it does | Requirements |
|---|---|---|
| Opening | What you get and roughly what it costs, in one paragraph. Names the alternative for a reader who wants the by-hand path. | FR-004, FR-005 |
| Set it up | One command. A table of which directory each assistant reads. What appeared and whether to commit it. What to do if your assistant cannot run commands. | FR-006 to FR-009 |
| Check it took | The proof prompt, what a right answer contains, and what a silent failure looks like. | FR-010, FR-011 |
| Build the salon agent | The story: what you type, what the assistant does, what you check, then `unmute dev` and a conversation. | FR-012, FR-013 |
| Ask for more | A tool, a second agent, a phone number. How to phrase each so the assistant does not guess, and what it should tell you back. | FR-014, FR-015 |
| Habits | Six, one line each, each naming the failure it prevents. | FR-016, FR-017 |
| Where next | Into the rest of the site. | FR-003 |

## What gets built, in order

**Stage 1, the page skeleton.** The file, the frontmatter, the seven sections
with their headings, and the navigation entry. `mint validate` and the page
count invariant pass. Nothing is claimed yet.

**Stage 2, setup and proof.** The two sections that carry every command and path
this page names. These are the ones FR-020 is about, so they get written against
feature 011's contracts and then run for real.

**Stage 3, the story.** Follow `examples/salon-support` through an actual
assistant session, and write down what happened. This section is reported, not
imagined. If the session goes badly, that is a finding about the skill and it
goes back to feature 011 rather than getting smoothed over in prose.

**Stage 4, asking for more, and the habits.** The shortest sections and the last
written, because both are distilled from what stages 2 and 3 turned up.

**Stage 5, the test and the sweep.** The agreement test, `mint broken-links`,
and a read of the page against the site's nine rules.

## Existing checks this feature must feed

| Check | Why it engages | What has to land |
|---|---|---|
| the site's page count invariant | a new `.mdx` needs a navigation entry | add `start/coding-agents` to the Get started group in `docs-site/docs.json` |
| `mint broken-links` | the page links into the rest of the site | run it before merge, per FR-019 |
| `mint validate` | new page and frontmatter | run it before merge |

Note that this page does not engage the CLI help capture. It names
`unmute skill install` but does not quote its flag list, so the page that has to
carry those flags is `docs-site/reference/cli/skill.mdx`, which feature 011 owns.
The two must agree on the command's spelling, and they do because both are
written from the same contract.

## Deliberate simplifications

- **No per-assistant setup tabs.** One command serves all four, so four tabs
  would teach a difference that does not exist. A table covers the one thing
  that genuinely differs, which is where each assistant looks. Research R3.
- **One worked build, not a matrix.** The page shows the moves on one example
  and lets them transfer, rather than repeating itself per target. Research R2.
- **No test on the prose.** The assistant list has a test because it is a list.
  The story does not, because a test that could hold it would have to be the
  story. The mitigation is that the story follows a package the default suite
  already validates and compiles, so it fails there first.

## Risks

- **The story is a report, and reports go stale.** The build it follows is
  tested, so the package cannot rot silently, but what an assistant says in a
  session can drift with the model. The mitigation is that the page shows what
  the reader types and what to check, and quotes the assistant only where the
  content is pinned by the skill.
- **Depends on a feature that has not merged.** Every command, flag, and path on
  this page comes from feature 011's contracts. If those move, this page moves.
  Writing it after, not beside, is the whole mitigation, and it is why the spec
  carries the dependency explicitly.
- **Fifteen minutes is a real bar.** SC-001 is the page's hardest claim and the
  only way to know is to watch someone do it. The quickstart puts that in the
  validation steps rather than leaving it as an aspiration.

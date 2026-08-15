# Phase 1 content model: the coding agents page

A documentation page has no runtime data. What it has is a content model: the
parts, what each must contain, and the rules that make the page true. This is
the spec's Key Entities made concrete enough to write against.

## Page

One file, `docs-site/start/coding-agents.mdx`.

| Field | Value |
|---|---|
| `title` | "Coding agents" |
| `description` | one sentence, what the reader gets |
| navigation slot | Get started group, between `start/installation` and `start/quickstart` |

Rules:

- Written to be read in order, and every section still makes sense alone, per
  FR-003. A reader arriving from search lands mid-page and must not be lost.
- Every link resolves inside the site or the repository. Nothing depends on the
  site being publicly reachable.
- Inherits the site's nine rules, including plain language and no dashes used as
  punctuation.

## Assistant

One row in the setup table. The set is not the page's to choose; it comes from
what `unmute skill install` accepts.

| Field | Notes |
|---|---|
| name | as typed in `--agent`: `claude`, `codex`, `cursor`, `copilot` |
| reads | the directory that assistant looks in |

Rules:

- The set on the page equals the set the CLI accepts. Held by the agreement test
  in `internal/skill/coding_agents_docsite_test.go`.
- The page never widens or narrows the set on its own. Adding an assistant is a
  change to feature 011 that this page follows.
- Paths shown were read from each vendor's own documentation on 2026-08-15, and
  are recorded with that date in feature 011's research.

## Proof check

The thing a reader runs to know setup applied.

| Field | Value |
|---|---|
| prompt | ask the assistant to name the four tool execution kinds |
| right answer | names all four: webhook, python, MCP, prebuilt |
| failure signal | a vague answer, a wrong count, or invented kinds |

Rules:

- The right answer must be specific enough that a confident guess fails it. A
  closed four-item list from this project's own schema meets that bar.
- The kinds named here are the same set the skill's `tools.md` names and the
  `Tool` struct defines, so the existing execution-kind agreement test covers
  this page's claim as a side effect.
- The failure signal must be described, not implied. FR-011 exists because a
  silent failure looks exactly like a bad assistant.

## Story step

One beat in the worked build. The build is `examples/salon-support`.

| Field | Notes |
|---|---|
| what you type | the reader's actual words, quotable |
| what it does | what the assistant produces |
| what you check | the one thing to look at before continuing |

Rules:

- Every step has all three. A step with no check is a step where a reader learns
  to trust output blindly, which is the habit this page exists to prevent.
- The story ends at a conversation, not a green validation.
- Reported from a real session, not composed. If the session went badly, that is
  a finding for feature 011.

## Follow-up ask

One of the three growth paths in FR-014.

| Ask | What the request must carry | What the assistant should say back |
|---|---|---|
| a tool | what it does, where the data comes from, whether it hits an HTTP endpoint or runs locally | which tool kind it chose and why |
| a second agent | what the second one owns, and when control moves | what context crosses the handoff |
| a phone number | inbound or outbound, and the carrier | which route it picked and what that route cannot do |

Rules:

- Each row states both halves. Half of this is asking well and half is knowing
  what a good answer contains, per FR-015.
- The "says back" column is the page's mechanism for making a silent decision
  noticeable.

## Habit

One line in the habits section. Six of them.

| Field | Notes |
|---|---|
| habit | the practice, imperative |
| prevents | the failure it stops, named |
| instead | what to do, required whenever the habit says avoid something |

Rules:

- Six, capped. SC-007 gives the section two minutes and FR-017 says a list
  nobody finishes prevents nothing.
- Every avoid-something habit carries an instead. FR-018.
- The six are listed in research R7.

## Held facts

The one thing on this page that a test holds, and the ones held elsewhere that
the page leans on.

| Fact | Held by |
|---|---|
| the assistant list | `internal/skill/coding_agents_docsite_test.go`, this feature |
| the four tool execution kinds | the execution-kind agreement test, feature 011 |
| the command and its flags | the CLI help capture, feature 011 |
| the worked build still compiles | the default suite, which validates and compiles every example |
| every link resolves | `mint broken-links` |

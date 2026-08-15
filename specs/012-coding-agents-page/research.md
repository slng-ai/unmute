# Phase 0 research: the coding agents page

**Date**: 2026-08-15.

Feature 011's plan settled everything this page depends on, so most of this
phase was reading that plan rather than the web. What is new here is the page's
own shape.

## R1. Where the page goes

**Decision**: `docs-site/start/coding-agents.mdx`, in the "Get started" group,
between `start/installation` and `start/quickstart`.

**Rationale**: FR-001 asks for a slot a new reader meets early. `quickstart` is
the by-hand path, and this page is its sibling, not its sequel. A reader who
lands on `installation` should see both routes and pick. Putting it after
`quickstart` would mean everyone who follows the sidebar in order takes the
slower path first and finds out afterwards that a faster one existed.

**Alternatives rejected**:

- After `how-unmute-works`, at the end of the group. Reads as an afterthought,
  and by then the reader has already started typing YAML.
- Its own top-level navigation group. One page is not a group, and it would sit
  outside the arc the sidebar tells.
- Under `Reference`. This is a story, not a lookup.

## R2. The worked build is `examples/salon-support`

**Decision**: the story in FR-012 follows `examples/salon-support`.

**Rationale**: FR-013 requires a build this repository already runs and tests,
and the examples README names this one "Start here. The one you can run in a
minute: web audio, local tools, no Twilio and no API to stand up." That is
exactly the profile a first story needs: nothing to provision, a real
conversation at the end, and it already shows a personalized greeting, a hidden
tool parameter, and the model saving what the caller says. Enough substance that
the reader sees the moves, not enough setup that they stop.

**Alternatives rejected**:

- `simple-prompt`. One agent and one large prompt. It compiles, but the story
  ends with nothing worth showing, and it teaches the shape the rest of the
  documentation exists to talk people out of.
- `subagents` or `task-groups`. Better material, wrong place. A first story that
  opens with two agents and a context decision is a tutorial about
  orchestration, and FR-014 already covers growing into that.
- A build written fresh for the page. Fails FR-013 and rots the moment anything
  upstream moves.

## R3. Setup is one command plus a table of what it wrote

**Decision**: one command for everyone, `unmute skill install`, followed by a
table naming each assistant and the directory it reads.

**Rationale**: feature 011 R8 settled that install writes both destinations by
default, so there is genuinely one command and no per-assistant variation in
what the reader types. But when it does not work, the first question is "did my
tool look in the right place", and that is a per-assistant fact. The table
answers it without turning setup into four procedures.

Read from each vendor's documentation on 2026-08-15, per feature 011 research
R2: Claude Code reads `.claude/skills/`; Codex, Cursor, and GitHub Copilot all
read `.agents/skills/`.

**Alternatives rejected**: four tabbed setup blocks. Three of them would carry
the same command, which teaches the reader that the assistants differ more than
they do.

## R4. The proof prompt asks for something unguessable

**Decision**: the check in FR-010 is asking the assistant to name the four tool
execution kinds. A correct answer names all four: webhook, python, MCP, and
prebuilt.

**Rationale**: FR-010 requires a check specific enough to tell success from a
confident guess. Most obvious prompts fail that bar. "What is Unmute" gets a
plausible paragraph from any model that has seen the word. "Build me an agent"
takes minutes and mixes the setup question with the quality question. A closed
four-item list drawn from this project's own schema is not guessable and not
inferable, so a right answer is proof the skill loaded and a wrong one is proof
it did not.

**Alternatives rejected**: asking which models are bound by default. Nearly as
specific, but a model that has seen SLNG marketing could land it by luck.
Asking it to build something. Slow, and it confuses two questions.

## R5. Links stay inside the site and the repository

**Decision**: every link on the page points at another page on this site or at a
file in the repository. No link depends on the site being publicly reachable.

**Rationale**: this is what dissolved the open question at specification time.
The site starts private, and FR-020 forbids publishing an instruction that does
not work. Internal links resolve in preview, in a private deployment, and in a
public one. Feature 011 R4 made the same call for the skill's own pointers, and
the two now agree, which was the point of deciding it once.

## R6. The assistant list is held by one test, shared with feature 011

**Decision**: `internal/skill/coding_agents_docsite_test.go` asserts that the
assistants named on the page equal the assistants `unmute skill install`
accepts.

**Rationale**: FR-018 asks for exactly this, and the repository already has the
pattern three times over: `providers_docsite_test.go`, `tools_docsite_test.go`,
and the CLI help capture. Putting it in `internal/skill` keeps the supported-set
and both things that quote it in one package, so a fifth assistant is one edit
and two failing tests that tell you where to go.

**Alternatives rejected**: a test under `internal/cli`. The set lives in
`internal/skill`, and a test that reaches across packages to restate it is the
second copy the constitution warns about.

## R7. The habits list is capped at six

**Decision**: six habits, one line each.

**Rationale**: SC-007 says the section reads end to end in under two minutes,
and FR-017 says a list nobody finishes prevents nothing. Six one-line items is
about forty seconds. LiveKit's equivalent runs to seven and is the longest part
of their page, which is the failure mode to avoid.

The six, each naming the failure it prevents, drawn from FR-016 plus the two
that FR-015 implies:

1. Let it validate its own work and read the error, rather than guess.
2. Never edit anything under `build/`, because the next compile overwrites it.
   Change the source package instead.
3. Name the target, or it picks one silently.
4. Treat the documentation as the authority over anything the assistant
   remembers.
5. Ask what it decided: target, models, context across a handoff.
6. Listen to the agent. A green check is not a good call.

## R8. Mintlify components already in use, nothing new

**Decision**: the page uses `<Steps>`, `<Step>`, `<Tip>`, and a plain markdown
table, matching `start/quickstart.mdx`.

**Rationale**: the site has a voice and this page is not the place to invent a
second one. Everything the story needs is already on the quickstart page.

## Carried in from feature 011

Not decisions this page makes, but facts it is written against:

- The command is `unmute skill install`, with `--agent`, `--dir`, and `--force`.
- It writes `.agents/skills/unmute/` and `.claude/skills/unmute/`.
- It makes no network call and touches no file the user owns.
- Re-running is honest about what changed, and refuses to overwrite hand edits.

If any of those move before 011 merges, this page moves with them. That is what
the dependency in the spec means, and it is why the page is written after the
command rather than beside it.

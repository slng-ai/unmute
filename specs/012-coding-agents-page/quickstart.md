# Quickstart: validating the coding agents page

How to prove the page is true. A documentation page fails differently from code:
the tests catch the lists, and a person has to catch the rest.

## Prerequisites

- Feature 011 merged, so `unmute skill install` exists
- Node 20 or newer, for `mint`
- `make build` produces `./bin/unmute`
- One of the four assistants, for the walkthrough

## 1. The automated gate

```sh
make fmt
make lint
make build
make test
```

The page's one test is in the default suite:

```sh
go test ./internal/skill/ -run TestCodingAgentsPage -v
```

It fails if the assistants named on the page and the assistants
`unmute skill install` accepts stop matching. Prove it bites rather than
assuming:

```sh
# add a fifth assistant name to the CLI's supported set, then:
go test ./internal/skill/
```

Expected: red, naming `docs-site/start/coding-agents.mdx`. Revert.

## 2. The site's own checks

```sh
cd docs-site
mint validate         # config and page checks
mint broken-links     # every internal link resolves
```

Both must be clean. `broken-links` is what holds FR-019.

Then the page count invariant:

```sh
find docs-site -name '*.mdx' | wc -l
```

That number must equal the count of page entries in `docs-site/docs.json`. It
went up by one with this page, and by one more with feature 011's
`reference/cli/skill.mdx`.

## 3. Read it against the site's rules

Open `docs-site/README.md` and read the page against its nine rules. The three
that bite hardest here:

- **Rule 4, plain language, no dashes as punctuation.** Grep for them.
- **Rule 3, only two targets.** The page must not present Vapi or Deepgram as
  something you can run.
- **Rule 5, no route presented as more proven than it is.** The phone number
  row in "Ask for more" is where this can slip.

## 4. Follow the page literally

The real test. Open the page and do exactly what it says, in a scratch
directory, with no other knowledge.

```sh
cd "$(mktemp -d)"
```

Then work through it start to finish. Check each of these as you go:

**4a. Setup lands.** The command runs, the files appear where the table says,
and you did not need to open another page to get here. That is FR-006 to FR-009
and SC-002.

**4b. The proof check works.** Ask the assistant the page's prompt. A correct
answer names all four tool kinds. Now break it on purpose:

```sh
rm -rf .agents/skills/unmute .claude/skills/unmute
```

Ask the same question again in a fresh session. The answer should visibly
degrade. If it does not, the check is not a check, and FR-011 is unmet.
Reinstall before continuing.

**4c. The story runs.** Follow the build section literally, typing what it says
to type. You should end at `unmute dev` and a conversation with the salon agent.
If any step's "what you check" does not match what you see, the page is wrong,
not you.

**4d. The follow-ups behave.** Try each of the three growth asks the way the
page phrases them. In each case the assistant should tell you back what the
table says it should: the tool kind it chose, the context crossing the handoff,
the route and its limits. A silent decision here is a finding, and it goes to
feature 011's skill, not to this page's prose.

**4e. Time it.** SC-001 gives 15 minutes from landing on the page to speaking
with an agent. Time the run. If it is over, the fix is usually the story being
too long, not the reader being slow.

## 5. Land in the middle

FR-003 says each section survives a reader who arrives from search. Open the
page at each heading anchor in turn and read only that section. Any section that
depends on one above it needs a sentence of its own context.

## 6. The two-assistant check

With both destinations installed, confirm the page is honest about sharing:

```sh
ls .agents/skills/unmute/references/   # the twelve references, one copy
cat .claude/skills/unmute/SKILL.md     # a pointer at the canonical bundle
```

The page says two assistants share one body of instructions. Confirm that is
what the files show.

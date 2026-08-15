# Contract: `docs-site/start/coding-agents.mdx`

What the page must contain. Behaviour not written here is not promised.

## Frontmatter

```yaml
---
title: "Coding agents"
description: "<one sentence: install the skill, then build a voice agent by asking for one>"
---
```

Matches the pattern every other page on the site uses.

## Navigation

One entry in `docs-site/docs.json`, in the Get started group:

```json
"pages": [
  "index",
  "start/installation",
  "start/coding-agents",
  "start/quickstart",
  "start/how-unmute-works"
]
```

The site's invariant that the count of `.mdx` files equals the count of page
entries must still hold.

## Sections, in order

### 1. Opening

- One paragraph: what the reader gets and roughly what it costs in time.
- Names `quickstart` as the by-hand path, so a reader who wants that goes there
  instead of reading on.
- Does not imply an assistant is required, or that an unsupported one rules
  Unmute out.

### 2. Set it up

- The prerequisite, `unmute` installed, linked to `/start/installation` rather
  than restated.
- One command, `unmute skill install`.
- The list of files it wrote, and one line saying to commit them so the team's
  assistants get them too.
- A table: assistant, and the directory it reads.

  | Assistant | Reads |
  |---|---|
  | Claude Code | `.claude/skills/unmute/` |
  | Codex | `.agents/skills/unmute/` |
  | Cursor | `.agents/skills/unmute/` |
  | GitHub Copilot | `.agents/skills/unmute/` |

- A note for a reader whose assistant cannot run commands: the command is safe
  to run in a plain terminal, and the files it writes are the whole result.
- A note that the install needs no network.

### 3. Check it took

- The prompt: ask the assistant to name the four tool execution kinds.
- What a right answer contains: all four, webhook, python, MCP, prebuilt.
- What a silent failure looks like: a vague answer, or invented kinds.
- What to do about it: re-run install, and check the directory for your
  assistant from the table above.

### 4. Build the salon agent

The story, following `examples/salon-support`. Every step carries three things:
what the reader types, what the assistant does, and what the reader checks.

- Ends with `unmute dev` and a conversation, not with a green validation.
- Uses `<Steps>` and `<Step>`, matching `start/quickstart.mdx`.

### 5. Ask for more

Three growth paths, each with what the request must carry and what the assistant
should say back:

| Ask | Request carries | Assistant says back |
|---|---|---|
| a tool | what it does, where the data comes from, HTTP endpoint or local | which tool kind it chose |
| a second agent | what it owns, when control moves | what context crosses the handoff |
| a phone number | inbound or outbound, and the carrier | which route, and what that route cannot do |

### 6. Habits

Six items, one line each, each naming the failure it prevents, and each
avoid-something item saying what to do instead. The six are fixed in
`research.md` R7.

### 7. Where next

Links into the rest of the site: orchestration, tools, telephony, deployment.

## Invariants

| Invariant | Held by |
|---|---|
| the assistants named equal the assistants `unmute skill install` accepts | `internal/skill/coding_agents_docsite_test.go` |
| every link resolves | `mint broken-links` |
| frontmatter and page config are valid | `mint validate` |
| pages and navigation entries agree in number | the site's existing invariant |
| every command named exists | the CLI help capture, feature 011 |

## Content rules

Inherited from `docs-site/README.md`:

- Plain language, short sentences, no dashes used as punctuation.
- Only Pipecat and LiveKit presented as targets.
- No route presented as more proven than the compile report says.
- Every model or vendor claim comes from the catalogue.
- Every fact checked against the code before it is written.

## What this contract does not cover

- The skill's contents. That is feature 011's `contracts/skill-bundle.md`.
- The command's behaviour. That is feature 011's `contracts/cli-skill-command.md`.
- `docs-site/reference/cli/skill.mdx`, the reference page that quotes the
  command's flags. Feature 011 owns it, because the help capture test requires
  it the moment the command exists.

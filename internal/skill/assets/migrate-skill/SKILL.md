---
name: unmute-migrate
description: Migrates an Unmute package written for unmute 0.8 or older to the current agent.yaml contract. Use when unmute says "version: is gone", when validate refuses a key as renamed or moved, when an agent.yaml has a version: line, or when asked to upgrade, migrate or port an Unmute package.
metadata:
  unmute_version: "{{unmute_version}}"
---

# Migrate a package to the current contract

unmute reads one agent.yaml contract. A file written for unmute 0.8 or older
has a `version:` line, and unmute refuses it before anything else. This skill
rewrites the package once, so it validates and compiles again.

`references/old-to-new.md` maps every old key to its new form. Read it before
you edit.

## Find out which old shape it is

Read the `version:` line, then delete it. There is no version key any more.

- `version: 1`: its local tools are not typed, and its value types may still
  sit in `agent.yaml`. Types move to `state.py` and to each tool's Python
  function. Do "From version 1" in the reference first, then "From unmute 0.8".
- `version: 2`: do "From unmute 0.8" only.

## The loop

```sh
unmute validate .
```

1. Run `unmute validate .`. Read the first error. It names the file, the line
   and, for a retired key, the new spelling. Errors come one at a time.
2. Fix that one thing, using the reference. Fix every other place in the
   package that has the same old key, not only the one on that line.
3. Run it again. Stop when it prints no error.

Do the big moves first, because they change where everything else sits:

1. `agents:` becomes a list with `- name:`.
2. `handoffs:`, `transfers:` and `flows:` move inside the agent that uses them.
3. Step keys move into `tools:` entries with `outcomes:`, and into `start:`.
4. Then the renames.

## Rules while you rewrite

- Keep the behaviour. A migration changes keys, not what the agent says or
  does. Copy every line, prompt path and `when:` text as it is.
- Keep comments. Move a comment with the key it explains.
- Every new field is a list. A name goes in `- name:`. A mapping from one name
  to one value is a list of one-key items, like `- phone: result.value`.
- `channels:` and `capacity:` belong in `targets.yaml`, not `agent.yaml`.
- Do not invent a key. If the reference has no row for a key and validate
  refuses it, ask the user.

## When validate is clean

```sh
unmute compile .
```

1. Compile every target in `targets.yaml`. A target can refuse a key the
   others accept. The error says to remove it or compile to livekit or pipecat.
2. If the package has a `scenarios/` folder, run one whole call with
   `unmute dev` or the scenarios, and listen for the same lines as before.
3. Tell the user what moved, in a short list, and anything you could not
   carry over.

The build skill, `unmute`, describes the current contract in full:
`.agents/skills/unmute/references/package.md`.

# Contract: the skill bundle

What the installed files must contain and satisfy. The command contract covers
how they get there; this covers what they are.

## Format

The [Agent Skills](https://agentskills.io) open standard, verified 2026-08-15.
A folder holding `SKILL.md` with YAML frontmatter, plus supporting files loaded
on demand.

Frontmatter carries three fields and no others:

```yaml
---
name: unmute
description: <one string, the activation trigger>
metadata:
  unmute_version: <the CLI version that wrote this>
---
```

`name`, `description`, and `metadata` are the intersection every one of the four
assistants accepts. Anything outside that set errors on at least one of them.

## Layout

```text
.agents/skills/unmute/
├── SKILL.md
├── references/
│   ├── package.md
│   ├── models.md
│   ├── orchestration.md
│   ├── tools.md
│   ├── prompting.md
│   ├── variables.md
│   ├── conversation.md
│   ├── telephony.md
│   ├── transfers.md
│   ├── deploy.md
│   ├── workflow.md
│   └── examples.md
└── .unmute-manifest.json

.claude/skills/unmute/
├── SKILL.md
└── .unmute-manifest.json
```

## `SKILL.md`, the entry document

Under 500 lines, held by a test. It is a decision layer, not a summary. It must:

1. Say what Unmute is in two sentences, and that the assistant authors a package
   rather than writing framework Python.
2. Route the task to a reference. One line per reference saying what it covers
   and when to open it. This is the part that makes the other twelve files free
   until needed.
3. State the build loop: write the package, run `unmute validate`, read the
   file, line, and column of any error, fix, repeat, then run it and listen.
   Include what to do when the assistant cannot run commands itself.
4. State the default models: SLNG to listen and speak, OpenAI to think, and that
   the assistant must say which vendors it used.
5. State the decisions the assistant must say out loud: the target it chose, the
   models it bound, the context that crosses any handoff, and what it checked
   before claiming success.
6. State that the documentation wins over anything the assistant remembers, and
   that a pointer is where to go when a claim looks stale.
7. State that generated output under `build/` is rewritten on every compile and
   must never be edited.

## Reference files

Each covers one area, opens with what it is for, and carries at least one
documentation pointer.

| File | Covers | Held by |
|---|---|---|
| `package.md` | the files an author writes and what each is for | pointer test |
| `models.md` | default vendors, and which vendors exist per role per target | vendor agreement test |
| `orchestration.md` | single agent, handoff, delegated task, task group, and every context decision at each boundary | pointer test |
| `tools.md` | webhook, prebuilt, mcp, python: file shape, what is legal, target support | execution-kind agreement test |
| `prompting.md` | voice prompting per surface: agent, task, group step, greeting, tool description | exempt from the pointer test, by name |
| `variables.md` | variables, where values come from, secrets as environment variable names only | pointer test |
| `conversation.md` | greeting, interruption, inactivity, turn detection | pointer test |
| `telephony.md` | routes as orchestrator plus transport plus carrier, directions, the boundary Unmute does not cross | pointer test |
| `transfers.md` | cold and warm, which routes support which, what the caller experiences | pointer test |
| `deploy.md` | what the operator owns after generation | pointer test |
| `workflow.md` | the build loop, reading errors, what to state out loud | command agreement test |
| `examples.md` | need to shipped example, with pointers | pointer test |

## Invariants

Each is a test in the default suite. Each fails naming the offending file.

1. **Entry budget.** `SKILL.md` is under 500 lines.
2. **No orphans.** Every file under `references/` is referenced from `SKILL.md`,
   and every reference named in `SKILL.md` exists.
3. **Execution kinds.** The tool kinds `tools.md` names equal the execution
   blocks on the `Tool` struct in `internal/spec`. No more, no fewer.
4. **Vendors.** The vendors `models.md` names, per role per target, equal what
   `internal/target/catalog_*.go` holds, with SLNG first in every list it
   appears in, matching the rule the documentation site is written under.
5. **Providers.** The target providers the bundle names equal `ir.Provider`, and
   wherever support is claimed the bundle says whether it means validation or
   generation. Pipecat and LiveKit generate; Vapi and Deepgram validate only.
6. **Commands.** Every command and flag the bundle names exists in the cobra
   tree, reusing the capture pattern in `internal/cli/help_capture_test.go`.
7. **Pointers.** Every documentation pointer resolves to a real page under
   `docs-site/`, and every reference file except `prompting.md` carries at least
   one. The exemption is named in the test.
8. **Frontmatter.** Both `SKILL.md` files carry exactly `name`, `description`,
   and `metadata`, and the pointer's `name` and `description` match the
   canonical one's.
9. **No secrets.** No file in the bundle contains anything shaped like a
   credential. Environment variable names only.

## Content rules inherited from the documentation site

The bundle is read by people as well as agents, and it restates the site's
facts, so it is written under the same rules:

- Plain language, short sentences, no dashes used as punctuation.
- Only Pipecat and LiveKit are presented as targets that run. Vapi and Deepgram
  appear only where validation is the claim, and the difference is always
  stated.
- ElevenLabs and Deepgram appear as model vendors where the catalogue lists
  them, never as targets.
- No route is presented as more proven than the compile report says.
- Every vendor and model claim comes from the catalogue.

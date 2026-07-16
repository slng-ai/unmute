# Unmute docs

You describe a voice agent once, in a few small YAML files. Unmute compiles that description into a real project for the platform you pick. Today that means a runnable Python project for **Pipecat** or **LiveKit** (code targets, both drivers complete), and provider config for ElevenLabs (managed target). If a platform cannot do something you asked for, Unmute tells you before anything runs, in that platform's own words. It never quietly drops a feature.

## Start here

New to Unmute? Three short pages, in order:

1. [What is Unmute](start/what-is-unmute.md): the idea in plain words.
2. [Install](start/install.md): one binary, plus `uv` for running agents locally.
3. [Your first agent](start/first-agent.md): `init`, `validate`, `dev`. An agent talking in your browser in a few minutes, then the same agent compiled for LiveKit.

Then grow it one feature at a time with the [learn pages](learn/01-one-agent.md): a tool, shared state, a second agent, tasks, task groups, phone calls, going live.

## How your code gets generated

The heart of Unmute is the compile step: `agent.yaml` in, a readable project out. Two pages explain it end to end, one per target:

- [Pipecat](targets/pipecat.md): a bus of workers. Each agent is a worker with its own model and voice; tasks re-program the active agent step by step.
- [LiveKit](targets/livekit.md): one session. Agents are classes that take turns holding the conversation; tasks are awaited objects with typed results.

Same YAML, same behavior for the caller, genuinely different machinery. [How targets run your agent](concepts/how-targets-run-your-agent.md) puts the two side by side with the actual generated code.

## The map

```text
docs/user/
├── start/        # zero to a running agent, fast
├── learn/        # one skill per page, simple to complex, in order
├── concepts/     # the ideas behind the fields, read when curious or confused
├── reference/    # every field, every allowed value, every target outcome
└── targets/      # one page per provider: how your YAML lands there
```

Common questions and where they are answered:

- "Why did validate fail?": [tags and gating](concepts/tags-and-gating.md), then the failing field's reference page.
- "Does X work on Y?": the feature table in targets/<y>.md, or the field's reference table.
- "What exactly gets sent to the provider?": [profiles and bindings](concepts/profiles-and-bindings.md), then `compile-report.json` after a compile.
- Migrating between providers: [safe core](reference/safe-core.md) first, then both target pages.

## Rules for writing these docs

Source of truth for facts is [SCHEMA.md](../../SCHEMA.md) and the driver specs; docs never contradict them, and when they do, the doc is the bug. Every page follows the same rules:

1. Simple words. Short sentences. One idea per sentence.
2. Explain a term the first time it appears, or link to the concepts page that does.
3. Show YAML before prose. An example first, then the explanation.
4. Every provider claim traces back to SCHEMA.md. No new provider facts invented in docs.
5. Never hide a limitation. If a field fails on a target, the docs say so as loudly as the CLI does.
6. No em or en dashes as punctuation. Use commas, colons, or separate sentences.

Reference pages use one fixed template per field: meaning, required, values, default, then a five-target table of what happens on each platform, then a short YAML example. A reference or target page ships only for behavior the compiler actually has; unshipped drivers get a one-line "driver in progress" note, never speculative tables.

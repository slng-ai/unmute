# Unmute docs, structure and plan

This folder is the user-facing documentation for the Unmute CLI. This file is the map: what pages exist, what each one is for, and the rules every page follows. Source of truth for facts stays [SCHEMA.md](../../SCHEMA.md) and [ORCHESTRATOR_SHARED_CONFIGURATION.md](../../ORCHESTRATOR_SHARED_CONFIGURATION.md). Docs never contradict them; when they do, fix the doc.

## Who we write for

Someone who has never seen Unmute. They may have never used LiveKit, Pipecat, Vapi, ElevenLabs, or Deepgram either. They want to build a voice agent, not learn five platforms. Every page assumes zero prior knowledge unless it links to a page that covers it.

## Writing rules (every page)

1. Simple words. Short sentences. One idea per sentence. If a 12-year-old cannot follow the sentence, rewrite it.
2. Explain a term the first time it appears, or link to the concepts page that does.
3. Show YAML before prose. An example first, then the explanation.
4. Every claim about a provider must trace back to SCHEMA.md or the shared configuration doc. No new provider facts invented in docs.
5. Never hide a limitation. If a field fails on a target, the docs say so as loudly as the CLI does. Fail-loud is the product; the docs follow it.
6. No em or en dashes as punctuation. Use commas, colons, or separate sentences.

## The four sections

Classic split: learn by doing (start, learn), understand (concepts), look up (reference, targets).

```text
docs/user/
├── README.md                    # this file: the map and the rules
│
├── start/                       # zero to a running agent, fast
│   ├── what-is-unmute.md        # the pitch and our take on orchestrators
│   ├── install.md               # get the binary, check it works
│   └── first-agent.md           # init, validate, run: one agent talking in minutes
│
├── learn/                       # one skill per page, simple to complex, in order
│   ├── 01-one-agent.md          # anatomy of the package: agent.yaml, instructions.md, targets.yaml
│   ├── 02-add-a-tool.md         # webhook tool: declare, describe, give to the agent
│   ├── 03-variables.md          # typed shared state, defaults, call_start
│   ├── 04-two-agents.md         # agent_transfer, context history, the handoff
│   ├── 05-tasks.md              # delegate and return with a typed result
│   ├── 06-task-groups.md        # ordered steps, then: return | transfer | end
│   ├── 07-phone-calls.md        # telephony channel, human transfer, outbound and voicemail
│   └── 08-going-live.md         # multiple targets, capacity, the sizing report, secrets
│
├── concepts/                    # the ideas behind the fields, read when curious or confused
│   ├── how-unmute-works.md      # compile, don't interpret: package -> validate -> generate
│   ├── our-take-on-orchestrators.md  # why one source, code vs managed targets, the pattern rule
│   ├── tags-and-gating.md       # core / warn / gated / provisional, fail loud, never average
│   ├── profiles-and-bindings.md # abstract in agent.yaml, concrete in targets.yaml, forwarded verbatim
│   └── tiers.md                 # T0 one agent, T1 tasks, T2 handoff, what each costs in portability
│
├── reference/                   # every field, every allowed value, every target outcome
│   ├── agent-yaml.md            # top level: version, entry_agent, and links to each block
│   ├── pipeline.md              # listen / turn / speak, placement, semantic_endpointing
│   ├── models-and-voices.md     # profiles, placement, fallback chains
│   ├── variables.md             # type, default, source
│   ├── agents.md                # instructions, model, voice, tools
│   ├── tasks.md                 # tasks and task_groups, result maps, context_scope, then
│   ├── controls.md              # delegate, agent_transfer, human_transfer, context blocks
│   ├── conversation.md          # greeting, interruption, inactivity, max_duration, thinking_audio
│   ├── channels-and-capacity.md # realtime_audio, telephony, on_voicemail, peak/max sessions
│   ├── tools.md                 # tools/*.yaml: input, output, execution, interruption, effect
│   ├── targets-yaml.md          # instances, pins, bindings, params, destinations
│   ├── safe-core.md             # the subset that runs on all five targets, verbatim from SCHEMA §7
│   └── cli.md                   # every command: validate, compile, apply, dev, init
│
└── targets/                     # one page per provider: how your YAML lands there
    ├── livekit.md
    ├── pipecat.md
    ├── vapi.md
    ├── elevenlabs.md
    └── deepgram.md
```

## What each section must do

### start/

Goal: a working agent before the reader understands everything. `what-is-unmute.md` carries the core message in plain words: you describe what the agent should do, once. Unmute compiles it for the platform you pick. If a platform cannot do something you asked for, Unmute tells you before anything runs, in that platform's own words. It never quietly drops a feature.

### learn/

A single running example (a small customer service agent) grows one page at a time. Page 01 is the safe core with one agent. Every page after it adds exactly one construct and shows the full diff of `agent.yaml`. Each page ends with "what just got harder": which targets now warn or fail, and why. This is where simple-to-complex lives, and where gating stops being abstract.

### concepts/

The reasoning. `our-take-on-orchestrators.md` is the flagship page: five platforms, one description; code targets (LiveKit, Pipecat, Deepgram) where Unmute writes the code and can build missing features; managed targets (Vapi, ElevenLabs) where only the provider API surface exists; the pattern rule that decides what compiles where. `tags-and-gating.md` explains core / warn / gated / provisional once, so reference pages can just use the words.

### reference/

The contract pages. One page per `agent.yaml` block, plus tools, targets.yaml, safe core, and the CLI. Every field gets the same fixed template, no exceptions:

```markdown
### field_name

What it means, in one or two plain sentences.

Required: yes / no / conditional (say the condition)
Values: the exact allowed values or type
Default: value, or "none: you must choose" (D7 fields have no default on purpose)

| Target | What happens | Tag |
|---|---|---|
| LiveKit | how it translates there (native API name if useful) | core |
| Pipecat | ... | core |
| Vapi | ... | gated: fails, provider-vocabulary reason |
| ElevenLabs | ... | warn: what the warning says |
| Deepgram | ... | core |

One short YAML example.
```

The five-target table on every single field is the promise the user asked for: for each option, the allowed values and how it translates in every framework. The rows come from SCHEMA.md sections 4 to 7; the reference never invents a row.

### targets/

The same information pivoted by provider, for the reader who thinks "I deploy on Vapi, what do I get?". Each page: what kind of target it is (code or managed), what that means for you, the full feature table for this provider (its column from SCHEMA §7), what its bindings look like in `targets.yaml` with a real example, its known conditions (carrier, SDK language, version pins), and its warnings explained.

## Reading paths

- Never seen it: start/ in order, then learn/01 to 04.
- "Why did validate fail?": concepts/tags-and-gating.md, then the reference page of the failing field.
- "Does X work on Y?": targets/<y>.md feature table, or the field's reference table.
- Migrating between providers: reference/safe-core.md first, then both target pages.

## Build order (not all at once)

1. start/ (all three) and concepts/our-take-on-orchestrators.md: the front door.
2. reference/ pages for the blocks the compiler already validates today, template above.
3. learn/01 to 04 (the T0 + T2 safe-core arc).
4. targets/pipecat.md first (driver in progress), then the other four as drivers land.
5. learn/05 to 08 and the remaining concepts pages.

A reference or target page ships only for behavior the compiler actually has. Docs for unshipped drivers get a one-line "driver in progress" page, never speculative tables.

---
name: unmute
description: Build voice agents with the Unmute CLI. Use when the user asks for a voice agent, a phone agent, a voice assistant, a call bot, or mentions Unmute, agent.yaml, targets.yaml, unmute validate, or unmute dev.
metadata:
  unmute_version: "{{unmute_version}}"
---

# Building voice agents with Unmute

Unmute is a Go command line tool that compiles one declarative package into a
Python project that LiveKit Agents or Pipecat runs natively. The author writes
what the agent should do, once, and Unmute writes the orchestrator project that
does it.

**You author a package. You do not write framework Python.** If you find
yourself writing `livekit.agents` or `pipecat.pipeline` imports by hand, stop:
that code is generated. Your job is `agent.yaml`, a prompt in Markdown, and a
few small YAML files beside them.

## Read this first, then open one reference

This file is a router. Everything below is a pointer to one reference file that
covers its area properly. Open the reference you need, not all of them.

| Reference | Open it when |
|---|---|
| `references/package.md` | you are writing or reading `agent.yaml`, `targets.yaml`, or the files around them |
| `references/workflow.md` | you are about to run a command, or an error came back and you need to read it |
| `references/models.md` | you are choosing which vendor listens, speaks, or thinks |
| `references/prompting.md` | you are writing any instructions file, greeting, task prompt, or tool description |
| `references/tools.md` | after choosing the structure, a real action has to call an API, run Python, use an MCP server, or hang up |
| `references/orchestration.md` | the brief has phases, required order, separate roles, permissions, or next-step behavior |
| `references/variables.md` | after choosing the structure, a runtime value has to cross a boundary or reach a tool |
| `references/conversation.md` | greeting, interruption, inactivity, turn detection, call limits |
| `references/telephony.md` | the agent has to answer or place a real phone call |
| `references/transfers.md` | the caller has to reach a person **on a phone call**. On a browser agent, see the note in that file and use a tool instead |
| `references/deploy.md` | the package works and now it has to run somewhere |
| `references/examples.md` | you want a working package that already does this |

## Choose the structure before files

Read the whole brief before you scaffold. If it names **required order**,
**separate roles** or permissions, or a server's **next step**, open
`references/orchestration.md` first. Choose the smallest native shape from the
brief and tell the user what you chose. Do not ask them to know the words task,
task group, or handoff.

Do this before tools and variables. They carry real actions and values; they do
not replace Unmute's task state, group order, or handoff state.

**Define each tool once.** Its contract and execution block live only in
`tools/<name>.yaml`, with local code in `tools/<handler>.py` when needed. Every
`tools:` list in `agent.yaml` contains names only: the top-level list loads tool
files, and an agent or task list grants access to them. Never copy a tool's
`description`, `input`, `output`, or execution block into `agent.yaml`.

**Task `result:` and tool `output:` are different contracts.** The task result
describes what the whole delegated task returns after any tool calls, so shape
it for the calling agent instead of copying an attached tool's output schema by
default.

## The build loop

Every change goes through the same four steps. Do not skip step two.

1. **Write the package.** `agent.yaml`, the prompt file, `targets.yaml`, and any
   tool files.
2. **Run `unmute validate`.** From inside the package the directory argument is
   optional, so `cd my-agent` once and run it bare. It prints one line per
   target and exits 1 if any fails. An error names the file, the line, and often
   the column.
3. **Read the error and fix the package.** The message says what is wrong and
   usually what to write instead. Do not guess, and do not work around a
   refusal: a refusal is a decision, and the message names the fix.
4. **Run `unmute dev`** and talk to it. A package that validates is not yet an
   agent that sounds right.

Repeat two and three until validate is clean. Never claim a package works when
you have only written it.

### Two different things you might not be able to do

**You cannot run commands at all.** Say so plainly, write the package, and give
the user the exact commands:

```sh
unmute validate ./my-agent
unmute dev ./my-agent
```

Write the path here, because you do not know which directory they are in.
Then ask them to paste the output back. Do not claim the package validates when
nothing has validated it.

**You can run commands but you cannot hear.** This is the common one. You can
run `unmute validate` and `unmute compile` yourself and you should. You cannot
do step four, because it needs ears. Do not skip it silently and do not pretend
it passed. Finish with the exact command and what to listen for:

> Validate is clean on the livekit target and compile succeeded. I have not
> heard it. Run `unmute dev ./my-agent` and listen for the greeting, the pause
> before the first reply, and whether it stops when you talk over it.

`prompting.md` has the five scenarios worth trying out loud.

## The default models

Unless the user says otherwise, bind these:

| Role | Provider | Why |
|---|---|---|
| listen (STT) | `slng` | the default in this repository, and the vendor its examples use |
| speak (TTS) | `slng` | same |
| think (LLM) | `openai` | the default reasoning provider |
| turn detection | `local` with `silero` on Pipecat, LiveKit's own detector on LiveKit | each target runs the one it is best at |

If the user names their own vendor, use it. Check it exists for that role on
that target first: `references/models.md` has the lists. Never invent a vendor
name, and never bind one you have not checked. If the vendor they asked for is
not there, say so and name what is.

## Say these four things out loud

When you finish a package, tell the user, in plain words:

1. **The target you chose**, and why. Pipecat and LiveKit both generate and run.
   Vapi and Deepgram validate only, which means a package can be checked against
   them but no project is generated.
2. **The models you bound**, by vendor and role, including any the user did not
   ask for.
3. **The context that crosses every handoff, delegation, and group step.** This
   is the decision that gets made silently and goes wrong later. Name it.
4. **What you actually checked.** "Validate is clean on the livekit target" is a
   claim. "I wrote the package and did not run it" is also fine, and it is what
   you say when it is true.

Never make a decision the user did not ask for without naming it.

## Two rules that are not negotiable

**The CLI wins.** These references are written against the code and held to it
by tests, but they are still prose and the code moves. If a reference and
`unmute validate` disagree, validate is right. Read its error out loud, quote
it to the user, and change the package. Never work around a refusal you have
not read, and never prefer what you remember about Unmute to what these
references say.

**Never edit `build/`.** `unmute compile` writes `build/<target>/` and rewrites
it every single time. Work you do there is lost on the next compile. Change the
package and compile again.

## Secrets never appear in a package

A package carries environment variable **names** in `UPPER_SNAKE`, never values.
There is no field anywhere that takes a key, a token, or a phone number as a
literal, and the compiler refuses one. A secret reaches a tool through its own
`*_env` field, or through `os.environ` inside a Python handler.

Secrets never flow through `{{...}}` templates, because every template site
renders into something spoken, prompted, traced, or logged.

If a user pastes a key into the conversation, do not write it into any file.
Put its name in `secrets:` and tell them to set the value in their environment.

## Where a package's files live

```
my-agent/
├── agent.yaml            the agent: models, agents, tools, conversation, channels
├── instructions.md       the prompt, in Markdown
├── targets.yaml          where it runs: pipecat, livekit, or both
├── connections/          one file per phone route, only for phone agents
│   └── twilio_sip.yaml
├── tools/                one file per tool, plus any Python handlers
│   ├── check_availability.yaml
│   └── check_availability.py
└── build/                generated. Never edit. Never commit by hand.
```

`unmute init <name>` scaffolds this. Start there rather than from an empty
directory, then `cd` into it and edit what it wrote. The scaffold declares one
target, `livekit`, so there is nothing for `--target` to choose between until
you add a second one by hand.

## Getting started, in order

Given a one-sentence brief from the user, do this:

1. Read the whole brief and choose its native structure. Open
   `references/orchestration.md` first when it carries phases or boundaries.
2. Scaffold with `unmute init <name>` and `cd` into it, or write the four files
   by hand using `references/package.md`.
3. Bind the default models. Write the prompt with `references/prompting.md` open,
   because a prompt written for chat sounds wrong out loud.
4. Add `end_call` so the agent can hang up. It is the one prebuilt tool that
   exists.
5. Validate. Fix. Validate again.
6. Run it and tell the user how to talk to it.

Use one agent when the brief has no boundary that buys a task, task group, or
handoff. When the repository examples are available, compare a structured brief
with `examples/multi-task` or `examples/task-groups`; `references/examples.md`
explains what to do when they are not there.

# The build loop

Write, validate, read the error, fix, repeat. Then run it and listen.

## The commands

| Command | What it does |
|---|---|
| `unmute init <name>` | scaffold a new package |
| `unmute validate [dir]` | load, build, and check against every declared target |
| `unmute compile [dir]` | validate, then write `build/<target>/` for each code target |
| `unmute dev [dir]` | compile, run locally, and let you talk to the agent |
| `unmute skill install` | write this skill into a project |

`[dir]` is optional on those three. With no directory they use the current one,
so you can `cd` into a package and run them with no argument. Passing a
directory still works and still wins, and it is the right form when you are not
inside the package.

Four commands take an author from nothing to a voice. `skill` is off that path:
it writes this bundle into a project and does nothing else.

```sh
unmute skill install
unmute skill install --agent claude
unmute skill install --force
```

`--agent` narrows which assistants to write for and takes `all`, `claude`,
`codex`, `copilot`, or `cursor`. `--dir` installs somewhere other than the
current directory. `--force` overwrites files that changed after they were
installed, which is what the command otherwise refuses to do.

## Start with init

```sh
unmute init my-agent
cd my-agent
```

It writes a working package: `agent.yaml`, `instructions.md`, `targets.yaml`,
and a `tools/end_call.yaml` so the agent can hang up from its first run. Edit
what it wrote rather than starting from an empty directory.

The scaffold declares **one** target, `livekit`, named after its provider. One
target means what you test is what you deploy, and it means you do not pass
`--target` on a scaffolded package: there is nothing to choose between. Add a
second target later, by hand, when the package needs one.

`unmute init` refuses to write into a directory that already exists and is not
empty. That is deliberate, not a bug to route around.

## Validate, every time

From inside the package, with no argument:

```sh
unmute validate
```

```
✓ livekit (livekit)

Warnings:
  livekit: LiveKit turn placement is a preference
```

One line per target: the target instance name, then its provider in brackets. A
scaffolded package prints one line, and a package that declares two prints two.
With no `--target`, every declared target is checked. `--target` is repeatable
and results come back in the order asked for. Naming a target the package does
not declare is refused:

```
target instance "pipecat" is not declared
```

Exit code 1 if any selected target fails.

### Warnings are not failures

Warnings go to standard error and the command still exits 0. They are real
differences worth reading and worth repeating to the user:

```
  livekit: LiveKit TaskGroup is experimental
```

Do not silence a warning and do not skip past one. A warning names what the
package is standing on.

Some routes also print a setup prerequisite block, which is work the user must
do outside Unmute before a real call. Pass those on.

## Read the error

An error names the file and the line:

```
unmute: validate examples/salon-support: build: agent.yaml:70: conversation.greeting.text
  references {{OPENAI_API_KEY}}, but secrets never flow through templates; a secret
  reaches a tool through its own *_env field
```

Three things to notice, and they are true of most Unmute errors:

1. **The location is exact.** File, line, often column. Go there.
2. **The message says what is wrong**, in a sentence, not a code.
3. **The message often says what to write instead.** Do that, rather than
   inventing a third thing.

A refusal is a decision. When validate says a target does not emit a shape, the
answer is to change the shape or change the target, not to look for a flag that
turns the check off. There is no such flag.

## Compile, then run

```sh
unmute compile
```

Writes one directory per code target: `build/livekit/`, `build/pipecat/`. Each
holds a Python project, a `Dockerfile`, an `.env.example`, Compose files, a
deploy manifest, a compile report, and a `README.md` runbook written for that
build.

**Never edit `build/`.** It is rewritten on every compile. Change the package.

Generation reaches only providers with a shipped driver. Pipecat and LiveKit
generate. Vapi and Deepgram validate and then fail by name at compile, which is
the honest behaviour, not a bug.

## Talk to it

```sh
unmute dev
```

Opens the browser and runs the same image production would deploy. Local
development runs in Docker, so Docker is required. Other flags:

| Flag | What it does |
|---|---|
| none | browser audio, Docker, the deployable image |
| `--telephony` | the generated Compose graph, so a real phone call can reach it |
| `--var name=value` | seed a `call_start` variable, repeatable |

`--var` is the local stand-in for the dispatch payload production sends. Each
value is parsed against the declared type, and an undeclared name is refused
rather than accepted and dropped.

A dev run puts back every outward change it made when it exits, including on
`ctrl-c`.

## What to state when you finish

Say what you actually did, in this order:

1. What you wrote, by file.
2. What you ran, and its real output. Quote the result line.
3. What you did not run, and why. "I cannot run commands here" is a complete
   answer, and a better one than silence.
4. The decisions you made that the user did not ask for.

A package that validates has not been heard. Say "validate is clean on every
declared target" if that is what happened, and name the targets, because a
scaffolded package has one. Do not say "it works" until someone has talked to
it.

## If you cannot run commands

Write the package, then hand the user the exact commands and ask for the output:

```sh
unmute validate ./my-agent
unmute dev ./my-agent
```

Write the path, because you do not know which directory they are sitting in.
Read what they paste back the same way you would read your own output. The
error message is the same message.

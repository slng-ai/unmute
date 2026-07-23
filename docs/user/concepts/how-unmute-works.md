# How Unmute works

Unmute is a **compiler**. You give it a description of an agent, it produces something a platform can run. It does not run your agent itself, and it is not a library your agent imports. This one choice, compile instead of interpret, shapes everything else.

## Compile, do not interpret

There are two ways a tool could support many platforms.

The first is to be a **layer that runs at call time**. Your agent would import Unmute, and Unmute would sit between your code and each platform, translating on the fly. Every platform difference becomes a runtime branch, and you carry Unmute's code into production forever.

Unmute does the second thing. It **generates the platform's own project ahead
of time**, then gets out of the way. LiveKit gets an `agent.py` project built
on LiveKit Agents. Pipecat gets a `bot.py` project built on workers and Flows.
Both are real Python you can read, run, and deploy without Unmute present.
Unmute's job is done when the files are written.

This matters for a practical reason: because Unmute writes each code target's
project, it can generate a handoff guard or task adapter around the framework's
native primitives. A runtime translation layer could only pass along what the
platform already exposes. This difference is the heart of
[our take on orchestrators](our-take-on-orchestrators.md).

## The four steps

Every `unmute` command that touches a package runs the same pipeline:

```text
load  ->  build  ->  validate  ->  generate
```

1. **Load.** Read the package files: `agent.yaml`, the prompt files, each `tools/*.yaml`, and `targets.yaml`. Parsing is strict, so a typo or an unknown field is an error here, with the file and line.
2. **Build.** Resolve the package into one internal model: link each agent to its prompt, model, and voice; connect controls to the tasks and agents they name; resolve each target's effective models (an override, or the agent.yaml definition). Anything that does not connect (an agent pointing at a model that does not exist) fails here.
3. **Validate.** Check the built agent against the chosen target. Every feature you used is measured against what that target can do. If the target cannot honor a feature, validation fails with a clear message in that platform's own words. This is where [tags and gating](tags-and-gating.md) live.
4. **Generate.** Only a valid agent reaches this step. The target's driver
   turns the model into native artifacts: a LiveKit Agents project, a Pipecat
   project, or a managed-provider plan.

`unmute validate` runs the first three steps and stops. `unmute compile` runs all four and writes the files. `unmute dev` compiles and then runs the result. The steps are always in this order, so you never generate broken output: validation is a gate, not a warning you can skip.

## Fail loud, never average

The rule that makes all of this trustworthy: **when a target cannot do something you asked for, Unmute stops and says so.** It never silently drops the feature, and it never quietly swaps in a weaker version to make things fit.

Consider a feature that works on four platforms and not the fifth. A tool that "averages" would remove the feature everywhere, so your agent is the same but worse on all four. A tool that "silently downgrades" would keep the feature on four and pretend on the fifth, so your agent looks fine but behaves differently where you least expect. Unmute does neither. It keeps the feature where it works and **fails validation** where it does not, telling you exactly which platform and why.

The payoff: what passes validation is real. If `unmute validate` says a target passes, every feature in your spec genuinely works on that target. Nothing was dropped behind your back.

## The package is the source of truth

Your package is the durable thing. The target is a swappable argument. Every
generated project under `build/<target>/` is disposable: you never edit it,
and every compile rewrites it from scratch. Change your target block and
recompile; the agent description doesn't move.

Read next: [our take on orchestrators](our-take-on-orchestrators.md), the reasoning behind why some features work on some platforms and not others.

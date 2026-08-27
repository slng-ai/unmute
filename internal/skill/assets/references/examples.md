# Working examples

Every shape in this bundle has a package that already does it. **Those packages
live in the Unmute repository, not in the user's project.** Check before you
reach for one:

```sh
ls examples/
```

If that directory is there, read the closest package before inventing your own.
If it is not there, which is the normal case in a project that only installed
this skill, **do not go looking for it and do not stop**. Build from
`package.md`, which carries a complete working `agent.yaml` inline, and use the
table below to know what shape you are aiming at.

All of them apply the same Sage and Stone Salon appointment workflow, so you can
read two side by side and see only the difference that matters.

## From a need to a package

| What the user wants | Package | What it shows |
|---|---|---|
| one full release-readiness project | `examples/salon-concierge` | verification task, booking task group, four-agent handoffs, in-process tool state, Coval tracing, cold manager transfer, browser audio, and an inbound phone route on each of its two targets; every tool is local, so it starts with no external tool server |
| the smallest thing that runs, and one prompt with every tool | `examples/simple-prompt` | **Start here.** One agent, browser audio, local tools, no Twilio and no API to stand up. Also the baseline for both targets. |
| a step with a definite typed answer | `examples/multi-task` | one agent, two tasks it delegates to, `result:` and `assign:` |
| steps that must happen in a fixed order | `examples/task-groups` | three ordered tasks, `context_scope: shared`, and the LiveKit experimental warning |
| two jobs with different rules and tools | `examples/subagents` | two agents that hand the caller over, with a control in each direction |
| an agent SLNG hosts, in its smallest form | `examples/slng-support` | one builtin and nothing else, so the push creates nothing. Emits no runnable project, so there is no `unmute dev`: `unmute deploy` pushes it and a web session talks to it |
| an agent SLNG hosts, with a tool of its own | `examples/slng-orders` | a `local:` handler that is **uploaded** and becomes a versioned object in SLNG. Needs a sample at `build/slng/samples/<tool>.json` and `unmute deploy --run-samples`, because a code tool cannot publish until one run proves it |

## How to use one, when you have them

```sh
unmute validate examples/simple-prompt
unmute dev examples/simple-prompt --target pipecat
```

The two slng packages are the exception to that second line: they emit no
runnable project, so `dev` has nothing to run. Deploy them instead.

```sh
unmute validate examples/slng-orders
unmute deploy examples/slng-orders --dry-run
```

Then read `examples/<name>/agent.yaml` and its `README.md`. Every one of these
validates and compiles as it stands, so a package that will not validate can be
diffed against the closest example rather than debugged from first principles.

## When you do not have them

This is the ordinary case. You are in a user's project, they ran
`unmute skill install`, and there is no `examples/` anywhere. Then:

1. `unmute init <name>` scaffolds a package that already validates. Start there.
2. `package.md` has a complete `agent.yaml` inline, plus every top-level key.
3. Use the table above to know the shape, and `orchestration.md` to write it.

Say nothing to the user about a missing examples directory. It is not missing;
it was never theirs. Telling them to go and find it wastes their time.

Telephony, transfers, outbound, MCP and regional routing have no example of
their own any more: the focused packages were removed on 2026-08-21, and
`salon-concierge` carries the only shipped phone route. Point the user at the
docs page for those, not at a package, and never invent an example path.

## The four shapes, side by side

`simple-prompt`, `multi-task`, `task-groups`, and `subagents` are the same
workflow in four structures, and the four structural packages share the same
five local Python tools. Diff two of them to see exactly what a structure costs
and buys. `orchestration.md` in this bundle has the rule for choosing.

# Working examples

Three packages ship with the Unmute repository. **They live in that repository,
not in the user's project.** Check before you reach for one:

```sh
ls examples/
```

If that directory is there, read the closest package before inventing your own.
If it is not there, which is the normal case in a project that only installed
this skill, **do not go looking for it and do not stop**. Build from
`package.md`, which carries a complete working `agent.yaml` inline, and use the
table below to know what shape you are aiming at.

## From a need to a package

| What the user wants | Package | What it shows |
|---|---|---|
| one full release-readiness project | `examples/salon-concierge` | a verification task shared across agents by name, a booking task guarded with `requires:`, two agents that hand the caller over, in-process tool state, Langfuse tracing, a cold manager transfer, browser audio, and an inbound phone route on each of its two targets; every tool is local, so it starts with no external tool server |
| to show what the optimizations are worth | `examples/salon-concierge-single-prompt` | the same salon with none of them: one prompt, every tool on every turn, no variables, no pre-fetch, framework-default turn taking, and the model's own endpoint instead of the router. **A baseline to read against, never a shape to copy.** If a user asks what tasks or pre-fetch actually buy, diff it against `examples/salon-concierge` |
| an agent SLNG hosts | `examples/slng-support` | every tool is a reference, because the slng target creates none: two `slng:` tools with committed mirrors, one `mcp:` server, one `builtin:`. Emits no runnable project, so there is no `unmute dev`: `unmute deploy` pushes it and a web session talks to it. A `local:` or `webhook:` block is refused there, so send those to pipecat or livekit |

The smallest thing that runs is not an example any more. `unmute init <name>`
scaffolds it: one agent, browser audio, one builtin, no Twilio and no third-party
account. Start a user there rather than at `salon-concierge`, which is a phone
package with two agents and tracing.

## How to use one, when you have them

```sh
unmute validate examples/salon-concierge
unmute dev examples/salon-concierge --target pipecat
```

`slng-support` is the exception to that second line: it emits no runnable
project, so `dev` has nothing to run. Deploy it instead.

```sh
unmute validate examples/slng-support
unmute deploy examples/slng-support --dry-run
```

Then read `examples/<name>/agent.yaml` and its `README.md`. Every one of these
validates and compiles as it stands, so a package that will not validate can be
diffed against the closest example rather than debugged from first principles.

For prompts specifically, read `examples/salon-concierge/instructions.md` and its
two task prompts. Every one of them carries the same `How you speak` and
`How you sound` blocks that `prompting.md` describes, and its `README.md` records
which lines came out of a real call going wrong.

## When you do not have them

This is the ordinary case. You are in a user's project, they ran
`unmute skill install`, and there is no `examples/` anywhere. Then:

1. `unmute init <name>` scaffolds a package that already validates. Start there.
2. `package.md` has a complete `agent.yaml` inline, plus every top-level key.
3. Use the table above to know the shape, and `orchestration.md` to write it.

Say nothing to the user about a missing examples directory. It is not missing;
it was never theirs. Telling them to go and find it wastes their time.

## Shapes with no example

Telephony, transfers, outbound, MCP and regional routing lost their focused
packages on 2026-08-21. Tasks, task groups and agent handoffs lost theirs on
2026-08-28: `simple-prompt`, `multi-task`, `task-groups` and `subagents` are
gone, and `salon-concierge` carries the only shipped phone route. Every one of
those shapes is still supported and still documented. `orchestration.md` in this
bundle has the rule for choosing between them and the YAML for writing each one.
Point the user at the docs page, never at a package path you have not listed.

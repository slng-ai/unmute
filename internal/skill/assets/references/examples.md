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
| the smallest thing that runs | `examples/salon-support` | **Start here.** One agent, browser audio, local tools, no Twilio and no API to stand up. A personalized greeting, a hidden tool parameter, and the model saving what the caller says. |
| one full release-readiness project | `examples/salon-concierge` | verification task, booking task group, four-agent handoffs, SQLite tools, Langfuse tracing, cold manager transfer, browser audio, and inbound-only phone routes on both targets |
| one agent, one prompt, every tool | `examples/simple-prompt` | the baseline for both targets |
| a step with a definite typed answer | `examples/multi-task` | one agent, two tasks it delegates to, `result:` and `assign:` |
| steps that must happen in a fixed order | `examples/task-groups` | three ordered tasks, `context_scope: shared`, and the LiveKit experimental warning |
| two jobs with different rules and tools | `examples/subagents` | two agents that hand the caller over, with a control in each direction |
| a real phone number, inbound and outbound | `examples/twilio-telephony-hello` | one carrier reaching each platform two different ways, so you can hear both call directions from a laptop |
| a cold transfer with nothing hosted | `examples/pipecat-human-transfer-twilio` | Pipecat Cloud reached through a Twilio number, no server of yours in the path |
| a warm transfer | `examples/livekit-human-transfer` | LiveKit over a Twilio SIP trunk. Warm transfer compiles on no other route today |
| a remote MCP server | `examples/mcp-example` | one tool file declares the server, its transport, its bearer token, and the one tool the agent may use |
| an outbound workflow with runtime values | `examples/outbound-reminder` | input variables from the dispatch, a system variable from the route, a conversation variable the model saves, and local Python appointment fixtures |
| keep the worker, STT, and TTS in a chosen geography | `examples/regional-infrastructure` | both runnable targets stay in Europe; its guide also explains LiveKit multi-region and Pipecat single-region deployment rules |

## How to use one, when you have them

```sh
unmute validate examples/salon-support
unmute dev examples/salon-support --target pipecat
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

Where a package name starts with a provider, the provider is the point. Putting
a caller through to a person works differently on each platform, so
`livekit-human-transfer` and `pipecat-human-transfer-twilio` are not
interchangeable and their names say which one you are reading.

## The four shapes, side by side

`simple-prompt`, `multi-task`, `task-groups`, and `subagents` are the same
workflow in four structures, and the four structural packages share the same
five local Python tools. Diff two of them to see exactly what a structure costs
and buys. `orchestration.md` in this bundle has the rule for choosing.

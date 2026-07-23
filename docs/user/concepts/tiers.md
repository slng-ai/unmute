# Tiers

Agents come in three levels of ambition. Unmute calls them **tiers**. A tier is not a setting you write; it is just a name for how much your agent does. Knowing the tiers helps you predict portability: the higher the tier, the more the four platforms disagree.

## The three tiers

**T0: one agent.** A single agent that talks, uses tools, and holds typed state. No handoffs, no delegated work. Everything at T0 is `core`: it runs on all four platforms, clean. This is where the [learn pages](../learn/01-one-agent.md) start, and where you should start.

**T1: tasks and task groups.** The agent can **delegate** a piece of work to a helper that runs, produces a typed result, and hands control back. A task group runs several such steps in a fixed order. This adds real structure, and it is where platforms begin to differ.

**T2: agent handoff.** Two or more agents, each with its own prompt, model, and voice, that **transfer** the conversation between them. A caller can move from an intake agent to a billing specialist. Handoff itself works on all four platforms; the fine control over what history carries across does not.

You can mix tiers freely in one package. A file with two agents (T2) and a delegated task (T1) is normal. Validation checks whatever you used against your chosen target.

## What each tier costs in portability

This table is the short version of "what works where". `ok` means it works, `warn` means it works with a warning, `fail` means validation fails. The rows come straight from the Unmute schema.

| Feature | Tier | LiveKit | Pipecat | Vapi | Deepgram |
|---|---|---|---|---|---|
| single agent | T0 | ok | ok | ok | ok |
| tools (webhook) | T0 | ok | ok | ok | ok |
| agent handoff, `full` history | T2 | ok | ok | ok | ok |
| task (delegate and return) | T1 | ok | ok | fail | ok |
| task group, then transfer or end | T1 | warn | ok | ok | ok |
| task group, then return | T1 | warn | ok | fail | ok |
| handoff history other than `full` | T2 | ok | gated | ok | ok |
| handoff guard (`requires`) | T2 | ok | ok | fail | ok |

Read the columns, not just the rows. **Pipecat has no `fail` in this table.** Every tier, up to and including delegated tasks with a guarded handoff, works on Pipecat. That is why these docs take Pipecat all the way to a complex agent: nothing you will build gets blocked.

The managed target (Vapi) is where the fails cluster, exactly as [our take on orchestrators](our-take-on-orchestrators.md) predicts: the richer the tier, the more a managed target has no place to host it.

## One shape rule for T1

Task groups are **ordered lists, not graphs.** Steps run one after another, start to finish. There are no branches, no loops, no going back, no routers. If you need "do A, then either B or C depending on the result", that is not a task group in this version; it is two agents with a handoff between them (T2). Keeping groups linear is what lets them compile cleanly across targets.

## Where to go from here

- Building up through the tiers in order: the [learn pages](../learn/01-one-agent.md), 01 through 06.
- Deploying the result on Pipecat: the [Pipecat target page](../targets/pipecat.md).
- The exact per-feature outcomes: `SCHEMA.md` section 7 in the repository, the full safe-core table.

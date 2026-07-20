# Our take on orchestrators

An **orchestrator** (also called a target or a platform) is the engine that runs a voice agent: it takes in audio, runs the listen-think-speak loop, and calls out to tools. Unmute supports five of them: **LiveKit, Pipecat, Vapi, ElevenLabs, and Deepgram.**

The interesting question is not "how do we support five platforms". It is "what do we do when the five disagree". This page is the answer.

## Two kinds of target

The five split into two groups, and the split explains almost every difference you will meet.

**Code targets: LiveKit, Pipecat, Deepgram.** This category gives Unmute a
project in which to generate missing logic. LiveKit and Pipecat have shipped
Python generators today. Deepgram validates against the same schema, but its
generator isn't implemented yet.

**Managed targets: Vapi, ElevenLabs.** The provider runs the agent for you. You do not get a project to host; you get an API with a fixed set of settings. Unmute can only use what that API already exposes. There is nowhere to put extra logic.

Say that difference out loud, because it is the whole game: **on a code target Unmute can build a missing feature; on a managed target it cannot, because there is nowhere for the built code to live.**

## The pattern rule

From that split comes the rule that decides what compiles where:

> If a feature is missing on a platform, Unmute may generate it on a code target. On a managed target the same feature fails.

For example, a **guard** on a handoff only transfers once the caller is
verified. LiveKit and Pipecat don't need to share a built-in setting: each
driver writes the check around its native handoff. Vapi and ElevenLabs have no
place to run that check, so the same field **fails validation** there. Same
spec, honest and different outcomes.

This is why the shipped code targets can support rich agents while keeping
their runtimes native. LiveKit uses Agents, `AgentTask`, and `TaskGroup`;
Pipecat uses workers and Flows. The YAML contract stays the same.

## One description, five outcomes

You still describe the agent only once. You do not write five versions. What changes per platform is not your description; it is the report Unmute gives you about that description.

- On a target where every feature works, validation passes clean.
- On a target where a feature needs a warning (it works, but behaves a little differently), validation passes and prints the warning.
- On a target where a feature cannot work, validation fails and names it.

Three rules hold this together, and they are worth stating plainly:

1. **The five targets decide the schema.** A feature is only in Unmute's vocabulary if all five can honor what it promises, whether natively, conditionally, or through generated code. Nothing is in the schema that no platform can do.
2. **You describe behavior, never platform infrastructure.** Model entries in
   `agent.yaml` name their provider and can carry `params`, an open map passed
   straight to that model's provider and never checked. Platform versions,
   transports, carriers, and destinations stay in `targets.yaml`. See
   [models and overrides](profiles-and-bindings.md).
3. **Fail loud, never average.** Covered in [how Unmute works](how-unmute-works.md). No silent drops, no silent downgrades.

## What this means for you

Pick your platform based on where you want to deploy, not on what you are allowed to ask for. Write the agent you actually want. Then let validation tell you, per platform, exactly what you get.

If you use LiveKit or Pipecat, read its target page for the remaining driver
gates. On a managed target, expect richer features such as guards,
fine-grained history, and delegated tasks to be limited, with validation
naming each difference.

The exact per-feature outcomes live in [tiers](tiers.md) and on each feature's page. The vocabulary for those outcomes (core, warn, gated, provisional) is the next thing to learn: [tags and gating](tags-and-gating.md).

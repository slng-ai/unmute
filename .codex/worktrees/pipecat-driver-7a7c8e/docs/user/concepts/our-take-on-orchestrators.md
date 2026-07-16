# Our take on orchestrators

An **orchestrator** (also called a target or a platform) is the engine that runs a voice agent: it takes in audio, runs the listen-think-speak loop, and calls out to tools. Unmute supports five of them: **LiveKit, Pipecat, Vapi, ElevenLabs, and Deepgram.**

The interesting question is not "how do we support five platforms". It is "what do we do when the five disagree". This page is the answer.

## Two kinds of target

The five split into two groups, and the split explains almost every difference you will meet.

**Code targets: LiveKit, Pipecat, Deepgram.** Unmute writes the code that runs on these. For Pipecat, that is a Python project. Because Unmute owns the code, it can add logic the platform does not offer directly.

**Managed targets: Vapi, ElevenLabs.** The provider runs the agent for you. You do not get a project to host; you get an API with a fixed set of settings. Unmute can only use what that API already exposes. There is nowhere to put extra logic.

Say that difference out loud, because it is the whole game: **on a code target Unmute can build a missing feature; on a managed target it cannot, because there is nowhere for the built code to live.**

## The pattern rule

From that split comes the rule that decides what compiles where:

> If a feature is missing on a platform, Unmute may generate it on a code target. On a managed target the same feature fails.

An example. A **guard** on a handoff (only hand off once the caller is verified) is not a built-in setting on any of the five. On Pipecat, a code target, Unmute writes the check into the generated Python, so the guard works. On Vapi and ElevenLabs, managed targets, there is no place to run that check, so using a guard **fails validation** there. Same spec, honest and different outcomes, each explained.

This is why Pipecat, the focus of these docs, can do so much: it is a code target, so nearly every feature in the schema is available, either because Pipecat has it natively or because Unmute writes it.

## One description, five outcomes

You still describe the agent only once. You do not write five versions. What changes per platform is not your description; it is the report Unmute gives you about that description.

- On a target where every feature works, validation passes clean.
- On a target where a feature needs a warning (it works, but behaves a little differently), validation passes and prints the warning.
- On a target where a feature cannot work, validation fails and names it.

Three rules hold this together, and they are worth stating plainly:

1. **The five targets decide the schema.** A feature is only in Unmute's vocabulary if all five can honor what it promises, whether natively, conditionally, or through generated code. Nothing is in the schema that no platform can do.
2. **You describe behavior, never provider settings.** `agent.yaml` never contains a platform's option names. The one exception is `params` inside a target binding, which is passed straight to the provider and never checked. See [profiles and bindings](profiles-and-bindings.md).
3. **Fail loud, never average.** Covered in [how Unmute works](how-unmute-works.md). No silent drops, no silent downgrades.

## What this means for you

Pick your platform based on where you want to deploy, not on what you are allowed to ask for. Write the agent you actually want. Then let validation tell you, per platform, exactly what you get.

If you are on a code target like Pipecat, expect almost everything to work. If you are on a managed target, expect the richer features (guards, fine-grained history, delegated tasks) to be limited, and expect Unmute to tell you which ones and why.

The exact per-feature outcomes live in [tiers](tiers.md) and on each feature's page. The vocabulary for those outcomes (core, warn, gated, provisional) is the next thing to learn: [tags and gating](tags-and-gating.md).

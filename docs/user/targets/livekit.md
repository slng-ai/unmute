# LiveKit

**Code target, driver shipped.** `unmute compile` emits a runnable LiveKit Agents project (Python, `livekit-agents>=1.5,<2.0`): each agent is a LiveKit `Agent` with native handoff via `@function_tool`, task groups lower to `beta.workflows.TaskGroup` (with `summarize_chat_ctx=False` for `merge: results`), webhook tools ride tasks, and the `AgentSession` is wired from your bindings with Silero VAD and the Inference turn detector.

## Providers

`unmute init` scaffolds SLNG for `listen`/`speak` (the `slng/<vendor>/<model>` route form), and any provider in the [providers reference](../reference/providers.md) binds instead: Deepgram for listen, ElevenLabs and Cartesia for speak today. `reason` runs on LiveKit Inference (any `provider`/`model` pair, no provider API key, billed through LiveKit Cloud). The emitted `pyproject.toml` pins exactly the plugins your bindings use, as `livekit-agents[...]` extras or the standalone SLNG plugin, and `.env.example` lists exactly the keys the project reads.

## Not emitted yet

These fail loud at compile rather than silently dropping behavior (each is a driver maturity gate tracked in `docs/spec/driver-livekit.md`): interruption shaping (`minimum_words`, `ignore_phrases`), model `fallback`, `human_transfer`, outbound calling and voicemail, transfer `requires` guards, agent-level plain tools (tools attach to tasks for now), single-task delegates, `context_scope: isolated`, transfer history other than `full`, `thinking_audio`, and nested task result schemas. `inactivity` and `max_duration` compile with a not-emitted-yet note in the report.

## Fields

For LiveKit's per-feature contract (native vs generated vs gated), see `SCHEMA.md`; for binding syntax, [targets.yaml](../reference/targets-yaml.md) and the [providers reference](../reference/providers.md).

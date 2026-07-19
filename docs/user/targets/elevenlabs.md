# ElevenLabs

**Managed target, driver shipped.** The provider runs the agent; `unmute compile` builds an ApplyPlan instead of code: one Conversational-AI Agent resource per Unmute agent (`conversation_config`), wired together with the `transfer_to_agent` system tool and created in dependency order so transfer targets exist before the agents that reference them. `unmute apply` executes the plan with `ELEVENLABS_API_KEY`.

- Being a managed target, ElevenLabs keeps a single running transcript and a
  fixed API surface. The schema records the consequences: only `history: full`,
  no minimum-word interruption knob, tasks only as a workflow node, and no
  handoff guard. Listen and turn are integrated, so their selected models or
  target overrides can carry settings only, never an outside speech model.
  Speak accepts ElevenLabs voices only. See the
  [providers reference](../reference/providers.md) and `SCHEMA.md` for the
  exact rules.

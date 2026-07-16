# ElevenLabs

**Managed target, driver shipped.** The provider runs the agent; `unmute compile` builds an ApplyPlan instead of code: one Conversational-AI Agent resource per Unmute agent (`conversation_config`), wired together with the `transfer_to_agent` system tool and created in dependency order so transfer targets exist before the agents that reference them. `unmute apply` executes the plan with `ELEVENLABS_API_KEY`.

- Being a managed target, ElevenLabs keeps a single running transcript and a fixed API surface. The schema records the consequences: only `history: full`, no minimum-word interruption knob (warns), tasks only as a workflow node, and no handoff guard. Its listen and turn roles are integrated, so you bind only settings, never an outside speech model; speak takes ElevenLabs voices only (see the [providers reference](../reference/providers.md)). See `SCHEMA.md` for the exact rows.

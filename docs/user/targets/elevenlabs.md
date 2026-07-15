# ElevenLabs

**Driver in progress.** ElevenLabs is a planned **managed target** (the provider runs the agent; you reconcile its settings through an API with `unmute apply`). Its driver is not implemented yet, so compiling or applying to an ElevenLabs instance currently fails with `elevenlabs driver is not implemented`.

This page will fill in when the driver ships. Until then:

- For a working target today, use [Pipecat](pipecat.md).
- Being a managed target, ElevenLabs keeps a single running transcript and a fixed API surface. The schema records the consequences: only `history: full`, no minimum-word interruption knob (warns), tasks only as a workflow node, and no handoff guard. Its listen and turn roles are integrated, so you bind only settings, never an outside speech model. See `SCHEMA.md` for the exact rows.

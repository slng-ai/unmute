# Vapi

**Driver in progress.** Vapi is a planned **managed target** (the provider runs the agent; you reconcile its settings through an API with `unmute apply`). Its driver is not implemented yet, so compiling or applying to a Vapi instance currently fails with `vapi driver is not implemented`.

This page will fill in when the driver ships. Until then:

- For a working target today, use [Pipecat](pipecat.md).
- Being a managed target, Vapi will support fewer of the richer features. The schema already records where: single-task delegates, `then: return` groups, handoff guards (`requires`), and non-`full` history are the known gaps. See [our take on orchestrators](../concepts/our-take-on-orchestrators.md) for why, and `SCHEMA.md` for the exact rows.

# Deepgram

**Driver in progress.** Deepgram is a planned **code target**. Its driver is not implemented yet, so `unmute compile` and `unmute validate --target <deepgram-instance>` currently fail with `deepgram driver is not implemented`.

This page will fill in when the driver ships. Until then:

- For a working code target today, use [Pipecat](pipecat.md).
- Deepgram integrates listen and turn into one model, so turn thresholds ride the listen binding's `params`, and it accepts Deepgram speech models plus a fixed third-party voice list. A few behaviors warn (interruption tuning is lossy, ignore-phrases are dropped) and its telephony features are carrier-conditional. See `SCHEMA.md` for the exact rows once you need them.

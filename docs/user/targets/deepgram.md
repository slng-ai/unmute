# Deepgram

Deepgram is a planned **code target**. You can validate a Deepgram target
today, including its capability and provider rules. Its generator is not
implemented, so `unmute compile` fails with
`deepgram driver is not implemented`.

This page will fill in when the driver ships. Until then:

- For a working code target today, use [Pipecat](pipecat.md).
- Deepgram integrates listen and turn into one model, so turn thresholds ride
  the selected listen model's `params`. It accepts Deepgram speech models plus
  a fixed third-party voice list. A few behaviors warn: interruption tuning is
  lossy, ignore phrases are dropped, and telephony features depend on the
  carrier. See `SCHEMA.md` for the exact rows when you need them.

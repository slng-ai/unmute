# parcel-desk

A parcel delivery company's phone desk, ported from unmute-core's `examples/parcel-desk`. The agent is defined as data. Every model request goes through the SLNG Context Router.

Try these tracking ids: `PD-1042` (in transit) and `PD-2077` (held at the depot).

## What it proves

- **State with a shape.** `state.py` holds a date, `Phone`, a `Parcel` model (an `Id` and a `Literal` status) and plain strings. Core builds the same State from type text.
- **Python tools.** `tools/depot.py` holds module-level functions over an in-memory depot. Types come from the signatures.
- **`start: say:`.** `find_parcel` and `reschedule_delivery` say a line as they start.
- **`with:`.** `reschedule_delivery` reads the tracking id from `{{state.parcel.tracking_id}}`. `save_callback_note` reads `{{call.session_id}}`. The model sends neither.
- **`effect: ends_conversation`.** `end_call` ends the call.
- **Finish, confirm and say.** `change_delivery` asks "Shall I move the delivery to ...?" before the write and says a receipt after it.
- **A flow that ends the call.** `request_callback` has `then: end`.
- **Prefetch clock.** Today on the Europe/London clock, before the greeting.
- **The SLNG Context Router as the model.** `provider: slng`, `model: slng/auto`, `agent_id: parcel-desk`, EU north.

## Scenarios

None. Core has no scenarios for this example, so there is no `scenarios/` folder. The e2e suite skips a package without one.

## Gaps

- **A `day` value in State.** The step saves `day: input.day` in its `save:`. Core has no `day` in its State, so the final state has one more value.
- **Receipt line reads `{{result.parcel.delivery_day}}`.** It reads `{{state.parcel.delivery_day}}`, because the step saves `parcel: result.parcel` first.
- **`then` on `move_delivery`.** Core leaves it unset. v2 needs one, so it is `then: return`, which is core's default.
- **Router upstream and model.** Core asks for `slng/auto` and leaves the rest to the router config stored with the key. Unmute sends the upstream with every request, so it names one: `upstream: provider: openai`, with `model: gpt-4.1-mini`. Change both to match your config. Validate also warns that no `reasoning_effort` is set. I left it out, as core does.
- **The move step's prompt.** It names today, as the main prompt does, and tells the model
  the application reads the day back. Core's prompt does neither: with them missing, a live
  model guessed a past year and read the day back twice. `find_parcel` also matches an id
  read aloud, like "P D 1042", which core's exact lookup misses.
- **Task `when` and `task_only_tools`.** `when` on `track_parcel` is kept. v2 has no `task_only_tools`: the tools sit under their tasks, and `end_call` is the agent's only tool of its own.
- **Turn pace.** Core's `balanced` is `pace: balanced` on both targets. `targets.yaml` sets the LiveKit turn model to `turn-detector-mini` with the same pace.
- **Core's own pipecat/livekit code.** Core's example uses `SLNG_ROUTER_URL` and `SLNG_MODEL` overrides from the environment. Unmute reads the router from `models.think`.

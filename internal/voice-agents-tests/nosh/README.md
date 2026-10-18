# nosh

An outbound restaurant and bar finder, ported from unmute-core's `examples/nosh`.
Nosh rings someone who asked for help, reads back the area they mean, lists places
there, and says a picked place's details. It changes nothing, so nothing needs a yes.

It is one agent with no tasks. Each tool saves what it returns and says code's next
line, so every caller turn costs one model request.

`scenarios/*.json` are unmute-core's, unchanged. The compiled agent must give the same
lines, `max_requests` and final state.

## What it proves

- An agent with only `tools:` and no tasks or flows.
- `save:` on a tool's entry in agent.yaml saves typed values from every result (`spot`,
  `offer`, `details`).
- `outcomes:` (`if` / `say`) is the line code says for each result.
- `with:` in the tool file is core's `locked=`: `state.spot` goes to `find_places` and
  `state.offer` to `place_details`, never from the model.
- `ToolError`: `place_details` refuses an id that was never offered.
- `hidden:` on the tool's entry keeps `listed` and `spoken` out of what the model reads.
- `start: say:` on three tools' entries, and an end tool with `effect: ends_conversation`.
- `variables:` with `call_start: required` for `customer_name`. A dispatch without a name
  is refused (`scenarios/no-name.json`).
- Tool handlers are module-level functions in `tools/finder.py`, with their own
  in-memory demo places.

## Place service

Without `TOOLS_API_KEY`, `tools/finder.py` answers from a few demo Barcelona places.
`TOOLS_API_KEY` and `TOOLS_URL` are listed in `secrets:` only so validate does not warn
about reading them. They are optional for the demo.

## Gaps

| Core behaviour | What is written instead |
|---|---|
| `AgentSpec(timezone="Europe/Madrid")` | No clock prefetch, so the agent has no timezone. No line names a date. `tools/finder.py` reads the Madrid clock itself for "today's hours" |
| Replies read `{{result.formatted_address}}`, `{{result.listed}}`, `{{result.spoken}}`, `{{result.name}}` | Saved to state with the entry's `save:` and read as `{{state.x}}`, so later turns can read them too. New state fields `listed`, `spoken`, `place_name`; `spot.formatted_address` is read from the saved spot |
| `hidden=["listed"]` and `["spoken"]` hide result fields only | The same `hidden:`. The saved `listed` and `spoken` state values are not hidden from the prompt, as nothing in the prompt reads them |
| No inactivity nudge (`idle=None`) | No `inactivity:` block |
| `Greeting` placeholder `{{customer_name}}` | `{{state.customer_name}}` |
| Pace `snappy` is one value for both targets | `pace: snappy` on `models.turn` and in the livekit `targets.yaml` detector (a per-target pace is refused) |

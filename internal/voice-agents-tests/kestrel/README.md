# kestrel

An inbound motor claim on the Kestrel Insurance line, ported from unmute-core's `examples/kestrel`. The caller gives a policy number and a date of birth, then reports what happened, and the agent files the claim.

Code says every line of the flow. The model only turns what the caller said into typed tool calls. The scenarios in `scenarios/` are unmute-core's, unchanged: the same lines, the same model requests per turn, and the same final state must hold when Unmute compiles this package.

## What it proves

- A flow, `claim`, takes `policy_number` and `birth_date` as `fills:` from the main agent.
- `find_policy` opens on `lookup_policy` with those values, so the read-back plays with no model request.
- `confirm:` reads the number and the date of birth back before the lookup runs. They are saved from the tool's input (`- policy_number: input.policy_number`) once it succeeds.
- `precheck:` refuses a number that is not KP and six digits, and the tool's own `outcomes:` ask again.
- `note_facts` saves the report, the read-back and the next question (its entry's `save:`), and its `outcomes:` say the next question or refuse a future day. The step opens on it, and the outcome that matches says the first question.
- `file_claim` reads the whole claim back, then files it on a yes. The tool file's `with:` passes the report, the policy number and `{{call.session_id}}`.
- `state.py` uses unmute-core's `Spelled` and `BirthDate`.

## Run it

```bash
make e2e PKG=kestrel
```

It needs `GOOGLE_API_KEY` and `SLNG_API_KEY` in `.env`.

## Gaps

| unmute-core does | This package does instead |
|---|---|
| `AgentSpec(language="en-GB")`: lines say dates as "5 June" | The agent's language is its voice's, `en`: lines say dates as "June 5". The timezone is the clock prefetch's, `Europe/London`, as in core |
| A second idle nudge, 20 s after the first | One nudge at 15 s, then the goodbye |

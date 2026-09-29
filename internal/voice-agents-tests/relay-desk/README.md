# relay-desk

The Twilio ConversationRelay acceptance package. It holds the whole
first-release surface of the `twilio` target and nothing that target refuses:
one agent, one inbound phone channel, a read-only `opening_hours` tool and
`end_call`.

It compiles to two target instances with one agent definition:

| Target | Thinks with | Key it reads |
|---|---|---|
| `twilio-openai` | OpenAI `gpt-5.6-luna`, Chat Completions | `OPENAI_API_KEY` |
| `twilio-gemini` | Gemini `gemini-3.1-flash-lite`, Vertex AI `eu` with an API key | `GOOGLE_API_KEY` |

## Check it without a call

```sh
unmute compile internal/voice-agents-tests/relay-desk
B=internal/voice-agents-tests/relay-desk/build/twilio-openai
uv run --project $B python scripts/text_run_twilio.py $B --fake   # offline protocol cases
uv run --project $B python scripts/text_run_twilio.py $B --real   # the real model, key from .env
```

Swap `twilio-openai` for `twilio-gemini` to check the other build. `--real` on
Gemini also replays a tool follow-up with its thought signatures stripped, which
the API must refuse.

## Call it

Follow `build/<target>/README.md`: host the app at a public HTTPS origin, set
`TWILIO_PUBLIC_URL`, and point a Twilio number's voice webhook at `/voice`.
Nothing here has been called through a real number yet.

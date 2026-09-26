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

1. Host one build at a public HTTPS origin, as `build/<target>/README.md` says,
   with `TWILIO_PUBLIC_URL` set to that origin on the host.
2. Put `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, `TWILIO_PHONE_NUMBER_SID` and
   `TWILIO_PUBLIC_URL` in this package's `.env`. No model key is needed here.
3. Check, then point the number:

   ```sh
   unmute deploy internal/voice-agents-tests/relay-desk --target twilio-openai --dry-run
   unmute deploy internal/voice-agents-tests/relay-desk --target twilio-openai
   ```

   It refuses when the host runs a different build, so recompile and rehost
   after any change here. The old route is saved first; the runbook says how
   to put it back.
4. Call the number.

The deploy path is tested against fake Twilio and fake host services only.
Nothing here has been routed on a real account or called through a real
number yet.

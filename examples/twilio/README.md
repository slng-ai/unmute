# twilio

A front desk on a Twilio number, using the `conversation-relay` transport.
Twilio handles speech; the FastAPI app you host runs `opening_hours`, `end_call`,
and LLM requests through the SLNG Context Router with OpenAI upstream.

On this page:

- [Quickstart](#quickstart)
- [How it connects](#how-it-connects)
- [SLNG Context Router](#slng-context-router)
- [Advanced](#advanced)
- [Files](#files)
- [Troubleshooting](#troubleshooting)

## Quickstart

From the repository root:

```sh
unmute validate examples/twilio
unmute compile examples/twilio
cp examples/twilio/build/twilio/.env.example examples/twilio/.env
```

Follow the [first-call guide](../../docs-site/telephony/twilio-conversation-relay.mdx)
to create a Twilio account, buy a voice-capable number, and enable
ConversationRelay. Fill in `.env` with `SLNG_API_KEY`, `OPENAI_API_KEY`,
`TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, and your host's public HTTPS origin
as `TWILIO_PUBLIC_URL`. Uncomment and fill in `TWILIO_PHONE_NUMBER_SID` for
deploy. Never commit this file.

Compilation creates local files; it does not publish a running service. Upload
`examples/twilio/build/twilio/` to your host, excluding `.env`, or push the
generated project to a deployment repository connected to a container service.
The [hosting walkthrough](../../docs-site/telephony/twilio-conversation-relay.mdx#4-host-the-application)
shows the steps, including a Render example and a VM alternative.

Host the generated app on a service of your choice with public HTTPS, secure
WebSockets, one process/instance, and at least 35 seconds of shutdown grace.
Keep it available when calls arrive; a sleeping service can miss calls.
For a local run from the build folder, use Python 3.12 and `uv`:

```sh
cd examples/twilio/build/twilio
uv run --env-file ../../.env python app.py
```

Or build and run its Docker image from that same folder:

```sh
docker build -t twilio-agent .
docker run --env-file ../../.env -e PORT=8080 -p 8080:8080 --stop-timeout 35 twilio-agent
```

These commands run wherever you execute them; running them locally does not
publish the app. The app uses port 8080 by default (`PORT` changes it). Supply those environment
values on your host and configure its proxy to pass WebSocket upgrades and
keep connections open for the duration of calls. `TWILIO_PUBLIC_URL` must match
the externally visible origin exactly, with no path. Put the same origin in
your local `examples/twilio/.env` so deploy routes the number to that host.
For a laptop test, expose
port 8080 through an HTTPS/WebSocket tunnel and use its origin.

Once the public app answers, return to the repository root:

```sh
curl https://YOUR-HOST/healthz
unmute deploy examples/twilio --target twilio --dry-run
unmute deploy examples/twilio --target twilio
```

Call the number. Check the greeting, ask “Are you open on Friday?” (nine to
three), interrupt an answer, then say goodbye and check that the call ends.
Read the host logs and Twilio's call inspector if a step fails.

For a new package, `unmute init my-desk --target twilio` uses the same SLNG
default, with a starter prompt and `end_call`.

## How it connects

The number's webhook sends `POST https://YOUR-HOST/voice`. The generated app
returns TwiML directing ConversationRelay to `wss://YOUR-HOST/conversation`.
Twilio sends caller text; the app runs tools and streams response text back
for Twilio to speak. The app validates Twilio signatures on both connections.

`unmute deploy --target twilio` checks the number, the hosted build's
`/healthz`, and a signed `/voice` response. It saves the previous route locally
and updates the number's voice webhook. It uploads no application and places
no call. Twilio requires a reachable secure WebSocket service; FastAPI is
Unmute's implementation choice. See the [target reference](../../docs-site/targets/twilio.mdx)
for supported configuration and rollback.

## SLNG Context Router

`agent.yaml` defaults to SLNG in `eu-west`, with OpenAI upstream. The router
can reuse cached responses for requests it judges repeatable and sends
other requests to the upstream LLM. The app authenticates to SLNG with
`SLNG_API_KEY` and forwards `OPENAI_API_KEY` for upstream requests, so both
keys are required. Twilio still provides all speech recognition and synthesis.

Keep `agent_id: twilio-v1` stable to reuse the agent's cache across calls.
Change it when changing instructions, tools, or model behavior makes previous
cached responses inappropriate. See [Context Router](../../docs-site/optimization/context-router.mdx)
for its configuration.

## Advanced

<details>
<summary>Direct OpenAI</summary>

Replace the `reasoning` binding under `models.think` with this complete binding:

```yaml
models:
  think:
    reasoning:
      provider: openai
      model: gpt-5.6-luna
      params:
        reasoning_effort: none
        parallel_tool_calls: false
```

This uses `OPENAI_API_KEY` without SLNG optimization.

</details>

<details>
<summary>Direct Gemini</summary>

Replace the `reasoning` binding under `models.think` with:

```yaml
models:
  think:
    reasoning:
      provider: google
      model: gemini-3.1-flash-lite
      params:
        vertexai: true
        location: eu
        thinking_config:
          thinking_level: MINIMAL
```

This uses `GOOGLE_API_KEY` with a Vertex AI API key and EU location.
Omit `vertexai` and `location` to use the Gemini Developer API instead.

</details>

After either change, recompile, supply the selected provider's key, replace the
hosted build, and deploy again. Only the selected binding's keys are required.
The target reference also covers custom agent logic, post-session TwiML,
regional configuration, and restoring the previous number route.

## Files

| File | Purpose |
|---|---|
| `agent.yaml` | Agent, models, phone channel, greeting, and call capacity |
| `instructions.md` | Agent instructions |
| `tools/opening_hours.yaml` | Tool input and output schemas |
| `handlers/opening_hours.py` | Opening-hours handler |
| `tools/end_call.yaml` | Builtin hangup tool |
| `targets.yaml` | Twilio target and connection |
| `connections/twilio.yaml` | ConversationRelay connection and environment names |

## Troubleshooting

| Symptom | Check |
|---|---|
| App exits naming a variable | Fill in that variable on the host |
| Deploy reports a different build | Recompile and replace the hosted app |
| Signed `/voice` is refused | Match the account, Auth Token, and public origin on the host and deployment machine |
| Number has a TwiML App, SIP trunk, or fallback | Resolve that routing in Twilio Console before deploying |
| Application error during a call | Check public `/healthz`, WebSocket support, the AI/ML addendum, host logs, and Twilio call inspector |
| Calls hang up immediately | Check whether `capacity.max_sessions` is full |
| Agent hears itself | Use a handset or headset |

The [first-call guide](../../docs-site/telephony/twilio-conversation-relay.mdx)
contains account setup and Twilio examples; the
[target reference](../../docs-site/targets/twilio.mdx) describes current limits.

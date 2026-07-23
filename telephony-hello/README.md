# telephony-hello

A minimal Twilio phone agent for testing real calls locally. It only greets and
chats, so it exercises the phone path without any tools, tasks, or transfers.
Use it to confirm your Twilio setup works before pointing the same flow at a
real agent.

It has one Pipecat target (Twilio Programmable Voice over a media WebSocket) and
one LiveKit target (Twilio SIP). The Pipecat target is the one you can fully
test on a laptop; LiveKit SIP needs public SIP and RTP reachability that a home
or office network usually cannot provide.

## What you need

- Docker Desktop (or Docker Engine) with the Compose plugin, running.
- `cloudflared` on your PATH for the automatic tunnel:
  ```bash
  brew install cloudflared
  ```
  On Linux, install the distribution package or a binary from the
  cloudflare/cloudflared releases page. No Cloudflare account is needed. To use
  your own tunnel instead, pass `--public-url https://your-tunnel.example`.
- A Twilio account with a Voice-capable phone number.
- Model provider keys for the agent to think, hear, and speak.

## Credentials

Put these in `telephony-hello/.env` (gitignored). Values are examples; use your
own.

```bash
TWILIO_ACCOUNT_SID=ACxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
TWILIO_AUTH_TOKEN=your_auth_token
TWILIO_PHONE_NUMBER=+15551234567     # your Voice-capable number, E.164
OPENAI_API_KEY=sk-...                # the LLM
SLNG_API_KEY=...                     # SLNG speech-to-text and text-to-speech
```

You do not set `UNMUTE_OUTBOUND_TOKEN`, `UNMUTE_PUBLIC_URL`, or `REDIS_URL`. The
dev command supplies those itself.

You do not configure the Twilio number's webhook by hand. The dev command sets
it on every run and prints the previous value so you can restore it.

## Test an inbound call

From the repository root:

```bash
unmute dev telephony-hello --telephony --target pipecat
```

Watch the output for the managed tunnel URL, the webhook update, and the line
`call +1...`. Call that number from your phone. The agent answers and greets
you. Speak, and it replies. `ctrl-c` stops everything and restores the tunnel.

## Test an outbound call

Add `--to` with a number you can answer:

```bash
unmute dev telephony-hello --telephony --target pipecat --to +15551234567
```

Once the container is healthy, the CLI places the call and prints
`calling +1..., call <id>`. Your phone rings, and the agent talks when you
answer. The CLI mints the dial-out token itself, so no extra setup is needed.

If Twilio refuses the call, the CLI prints the reason (for example geo
permissions for the destination country, or a trial account that can only call
verified numbers). Fix it in the Twilio Console and run again.

## A note on the warning

Validation prints `telephony ... is unverified`. That is expected: these routes
have real adapters but have not passed an automated credentialed smoke, so the
CLI flags them and lets you run them. Test the behavior you rely on yourself
before you depend on it in production.

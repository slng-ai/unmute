# telephony-hello

A minimal Twilio phone agent for testing real calls locally. It only greets and
chats, so it exercises the phone path without any tools, tasks, or transfers.
Use it to confirm your Twilio setup works before pointing the same flow at a
real agent.

It has two targets, both driven by the same `.env`:

- **pipecat**: Twilio Programmable Voice over a media WebSocket.
- **livekit**: the LiveKit Twilio connector, which also uses Twilio Media
  Streams over a WebSocket and bridges the call into a local LiveKit room where
  a LiveKit worker handles it.

Both test fully on a laptop through the managed tunnel, inbound and outbound.
Neither needs a Twilio SIP trunk or public SIP/RTP. Swap `--target pipecat` for
`--target livekit` in any command below to test the other stack.

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

Put these in `examples/telephony-hello/.env` (gitignored). Values are examples; use your
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
unmute dev examples/telephony-hello --telephony --target pipecat
```

Watch the output for the managed tunnel URL, the webhook update, and the line
`call +1...`. Call that number from your phone. The agent answers and greets
you. Speak, and it replies. `ctrl-c` stops everything and restores the tunnel.

The LiveKit connector works the same way; it just runs a local LiveKit Server
alongside the bridge:

```bash
unmute dev examples/telephony-hello --telephony --target livekit
```

## Test an outbound call

Add `--to` with a number you can answer:

```bash
unmute dev examples/telephony-hello --telephony --target pipecat --to +15551234567
```

Once the container is healthy, the CLI places the call and prints
`calling +1..., call <id>`. Your phone rings, and the agent talks when you
answer. The CLI mints the dial-out token itself, so no extra setup is needed.
`--to` works the same on `--target livekit`.

If Twilio refuses the call, the CLI prints the reason (for example geo
permissions for the destination country, or a trial account that can only call
verified numbers). Fix it in the Twilio Console and run again.

## A note on route maturity

These routes have real adapters, so `unmute validate`, `unmute compile`, and
`unmute dev --telephony` run them with no warning and no error. The
provisional-versus-verified status is internal maturity tracking, recorded in
the generated `compile-report.json`, not something the CLI prints at runtime.
Test the behavior you rely on yourself before you depend on it in production.

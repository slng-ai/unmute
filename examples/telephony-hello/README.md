# telephony-hello

A minimal Twilio phone agent for testing real calls. It only greets and chats, so
it exercises the phone path without any tools, tasks, or transfers. Use it to
confirm your Twilio setup works before pointing the same flow at a real agent.

It has two targets, both driven by the same `.env`:

- **pipecat** (`transport: cloud-websocket`): Pipecat Cloud terminates the Twilio
  Media Stream itself. In production nothing is hosted by you: your number points
  at a small piece of static markup in the Twilio console, and the platform starts
  the agent.
- **livekit** (`transport: connector`): the LiveKit Twilio connector, which also
  uses Twilio Media Streams over a WebSocket and bridges the call into a local
  LiveKit room where a LiveKit worker handles it. This one you host.

That difference is the point of having both here, and it changes what each can do
locally. See **Which target does what locally** below.

## What you need

- A Twilio account with a Voice-capable phone number.
- Model provider keys for the agent to think, hear, and speak.
- `cloudflared` on your PATH, for the automatic tunnel both targets use locally:
  ```bash
  brew install cloudflared
  ```
  On Linux, install the distribution package or a binary from the
  cloudflare/cloudflared releases page. No Cloudflare account is needed. To use
  your own tunnel instead, pass `--public-url https://your-tunnel.example`.
- `uv` on your PATH, for the **pipecat** target's local run.
- Docker Desktop (or Docker Engine) with the Compose plugin, for the **livekit**
  target's local run. The pipecat target needs no Docker locally.

## Credentials

Put these in `examples/telephony-hello/.env` (gitignored). Values are examples;
use your own.

```bash
TWILIO_ACCOUNT_SID=ACxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
TWILIO_AUTH_TOKEN=your_auth_token
TWILIO_PHONE_NUMBER=+15551234567     # your Voice-capable number, E.164
OPENAI_API_KEY=sk-...                # the LLM
SLNG_API_KEY=...                     # SLNG speech-to-text and text-to-speech
PIPECAT_CLOUD_ORGANIZATION=your-org  # pipecat target only, and only for outbound
```

`PIPECAT_CLOUD_ORGANIZATION` comes from `pipecat cloud organizations list`. It is
needed because an outbound call's markup has to name the deployed agent, and the
compiler knows the agent name but not your organization.

You do not set `UNMUTE_OUTBOUND_TOKEN`, `UNMUTE_PUBLIC_URL`, or `REDIS_URL`. The
dev command supplies what it needs itself, and the pipecat target on this route
uses none of the three.

You do not configure the Twilio number's webhook by hand for a local session. The
dev command sets it on every run, prints the previous value, and restores it when
the session ends.

## Which target does what locally

| | pipecat (Pipecat Cloud) | livekit (connector) |
|---|---|---|
| local inbound call | yes, `dev --telephony` | yes, `dev --telephony` |
| local outbound call | no: see below | yes, `--to` |
| runs locally as | `uv run bot.py`, no Docker | a Docker Compose graph |
| hosted by you in production | nothing | the bridge and the worker |

**Why local outbound differs.** On the pipecat target an outbound call is created
at Twilio with markup naming the **deployed** agent, so the call reaches Pipecat
Cloud rather than your laptop. There is nothing for a local session to answer.
`--to` therefore does nothing on this target and says so; the outbound command
lives in the generated `build/pipecat/README.md` and runs against the deployed
agent.

## Test an inbound call

From the repository root:

```bash
unmute dev examples/telephony-hello --telephony --target pipecat
```

Watch the output for the tunnel URL, the webhook update, and the line
`call +1...`. Call that number from your phone. The agent answers and greets you.
Speak, and it replies. `ctrl-c` stops everything and puts the number's previous
voice configuration back.

The LiveKit connector works the same way; it just runs a local LiveKit Server
alongside the bridge:

```bash
unmute dev examples/telephony-hello --telephony --target livekit
```

## Test an outbound call

On **livekit**, add `--to` with a number you can answer:

```bash
unmute dev examples/telephony-hello --telephony --target livekit --to +15551234567
```

Once the container is healthy, the CLI places the call and prints
`calling +1..., call <id>`. Your phone rings, and the agent talks when you
answer. The CLI mints the dial-out token itself, so no extra setup is needed.

On **pipecat**, outbound runs against the deployed agent:

```bash
unmute compile examples/telephony-hello
cd examples/telephony-hello/build/pipecat
# fill .env, push the secret set, deploy, then run the outbound command from
# this directory's README
```

If Twilio refuses the call, the reason is printed in Twilio's own words (for
example geographic permissions for the destination country, or a trial account
that can only call verified numbers). Fix it in the Twilio Console and run again.

## Regions

This package declares **no** `deployment_region`, so the Pipecat target goes to
your organisation's default region and the generated markup uses the platform's
default stream endpoint, `wss://api.pipecat.daily.co/ws/twilio`, which routes to
`us-west`.

To pin a region, add one line to the `pipecat` target in `targets.yaml`:

```yaml
    deployment_region: eu-central
```

Recompile, and three things move together, because all three are rendered from
that one line: the `region` in `pcc-deploy.toml`, the `--region` on the
`secrets set` command in the generated README, and the `wss://` host in the markup
you paste into Twilio. They have to agree, because a regional stream endpoint
routes **only** to agents deployed in that region and an agent can only read a
secret set from its own region. `pipecat cloud regions list` prints what is
available to you, and
[docs/TELEPHONY.md](../../docs/TELEPHONY.md) explains the chain.

The LiveKit target has its own regional story on its own platform; nothing here is
shared between the two.

## A note on route maturity

Both routes have real adapters, so `unmute validate`, `unmute compile`, and
`unmute dev --telephony` run them with no warning and no error. The
provisional-versus-verified status is internal maturity tracking, recorded in the
generated `compile-report.json`, not something the CLI prints at runtime. Test the
behavior you rely on yourself before you depend on it in production.

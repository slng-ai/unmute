# Twilio walkthrough, step by step

This guide walks you through testing real phone calls with the
[telephony-hello example](../../../examples/telephony-hello/README.md), inbound
and outbound, on both laptop-testable routes:

- **Pipecat** with Twilio Programmable Voice (`carrier-websocket`)
- **LiveKit** with the Twilio connector (`connector`)

Both routes use the same three Twilio credentials, the same managed cloudflared
tunnel, and the same `--to` dial-out. Neither needs a SIP trunk or public
SIP/RTP: Twilio reaches the generated app over HTTPS and WSS. It tells you
exactly where each value lives in the Twilio Console. If you want the concepts
behind these steps, read [07. Phone calls](07-phone-calls.md) first.

> These telephony routes have real adapters, so validation, compilation, and
> `unmute dev --telephony` run them cleanly, with no warning. The commands below
> work today. Test the behavior you depend on yourself before you rely on it in
> production.

## What you need

- A Twilio account with some balance (calls and numbers cost money).
- Docker with the Compose plugin.
- The Unmute CLI built (`make build`).
- `cloudflared` on PATH, or your own tunnel.
- A real phone to call from and to.
- Model provider keys (`OPENAI_API_KEY`, `SLNG_API_KEY`) so the agent can
  think, hear, and speak.

Create the environment file the example reads:

```bash
cat > examples/telephony-hello/.env <<'EOF'
TWILIO_ACCOUNT_SID=
TWILIO_AUTH_TOKEN=
TWILIO_PHONE_NUMBER=
OPENAI_API_KEY=
SLNG_API_KEY=
EOF
```

Everything below fills in that `.env`. The dev command supplies
`UNMUTE_PUBLIC_URL`, `UNMUTE_OUTBOUND_TOKEN`, `LIVEKIT_URL`,
`LIVEKIT_API_KEY`, and `LIVEKIT_API_SECRET` by itself. Leave those out.

## Part 1: the Pipecat route (Programmable Voice)

Pipecat talks to Twilio over HTTPS webhooks and a media WebSocket. No SIP
trunk is involved.

### Step 1: get the account credentials

1. Sign in at [console.twilio.com](https://console.twilio.com).
2. On the Console home page, find the **Account Info** card.
3. Copy **Account SID** (starts with `AC`) into `TWILIO_ACCOUNT_SID`.
4. Click **Show** next to **Auth Token** and copy it into
   `TWILIO_AUTH_TOKEN`.

The Auth Token signs and verifies webhooks, so the generated app needs it
even if you use API keys elsewhere.

### Step 2: get a Voice-capable number

1. In the Console, go to **Phone Numbers → Manage → Buy a number**.
2. Filter by **Voice** capability and buy one, or pick an existing number
   under **Phone Numbers → Manage → Active numbers**.
3. Copy the number in E.164 form (for example `+15550001111`) into
   `TWILIO_PHONE_NUMBER`.

You do not need to configure the number's webhook by hand. The dev command
sets it on every start and prints the previous value so you can restore it.

### Step 3: the outbound token is automatic

The generated dial-out endpoint is protected by an application token. For local
development you do not set it: `unmute dev --telephony` mints a random
`UNMUTE_OUTBOUND_TOKEN`, injects it into the container, and reuses it to place
the call. In production you supply your own value in the deployment
environment.

### Step 4: install cloudflared

Twilio must reach your machine over public HTTPS. The dev command manages a
[cloudflared quick tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/do-more-with-tunnels/trycloudflare/)
for you; it only needs the binary on PATH:

```bash
brew install cloudflared
```

On Linux, install the distribution package or download a binary from the
cloudflare/cloudflared releases page. No Cloudflare account is needed. If
you prefer ngrok or another tunnel, skip this step and pass
`--public-url https://your-tunnel.example` later instead.

### Step 5: run it

```bash
unmute dev examples/telephony-hello --target pipecat --telephony
```

Read the output top to bottom:

1. The resolved route, setup notes, and services.
2. `managed tunnel https://<random>.trycloudflare.com` (rotates per run).
3. The exact public endpoints (inbound webhook, media WebSocket, outbound
   trigger, status callback).
4. The ready banner with the Compose project name and log path.
5. `Twilio voice webhook for +1... set to ... (was: ...)`. Note the `was`
   value if you want to restore it later.
6. `call +1XXXXXXXXXX, ctrl-c to stop`.

### Step 6: test inbound

Call the printed number from your phone. Twilio requests the webhook, the
generated app validates Twilio's signature and answers with a Media Streams
connection, and the agent greets you.

### Step 7: test outbound

The agent's phone channel allows it (`outbound: true`). Re-run with `--to`
set to a number you can answer:

```bash
unmute dev examples/telephony-hello --target pipecat --telephony --to +15551234567
```

Once the container is healthy the CLI places the call for you and prints
`calling +1..., call <id>`. Your phone rings, and the agent starts talking when
you answer. No token or curl needed: the CLI mints the token and triggers the
dial-out over loopback.

To drive dial-out from your own application instead, POST to the generated
endpoint with your production token:

```bash
curl -X POST "https://<your-host>/telephony/outbound" \
  -H "Authorization: Bearer $UNMUTE_OUTBOUND_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"to": "+15551234567", "call_start": {}}'
```

`call_start` carries values for any `source: call_start` variables the agent
declares; this example declares none, so it stays empty.

## Part 2: the LiveKit Twilio connector

The connector uses Twilio Media Streams too, but bridges the call into a local
LiveKit room where a LiveKit worker handles it. It needs the same three Twilio
credentials as Part 1 and no extra Twilio setup. This is our own open-source
bridge; LiveKit's hosted connector is Cloud only.

### Step 1: reuse the same credentials

The connector reads the same `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, and
`TWILIO_PHONE_NUMBER` you already set. There is no SIP trunk and no Redis. The
dev command supplies `LIVEKIT_URL` and the local key pair itself and runs a
local `livekit-server --dev` container next to the app.

### Step 2: run it

```bash
unmute dev examples/telephony-hello --target livekit --telephony
```

The output is the same shape as Part 1: the managed tunnel, the public
endpoints, the auto webhook update, and the call line. The only difference is
an extra `livekit_server` service in the Compose graph.

### Step 3: test inbound and outbound

Call the printed number: Twilio opens the media WebSocket to the bridge, the
bridge joins a fresh `call-*` room and dispatches the worker, and the agent
greets you. For outbound, add `--to`:

```bash
unmute dev examples/telephony-hello --target livekit --telephony --to +15551234567
```

The CLI places the call and the bridge connects it into the room, exactly like
the Pipecat route.

## Production: the LiveKit SIP route

For production, LiveKit's stable multi-carrier path is a self-hosted SIP bridge
(`transport: sip`) fed by a carrier Elastic SIP Trunk. It is not laptop-testable
for inbound, because Twilio talks SIP and RTP straight to your machine and an
HTTPS tunnel cannot carry that. Set it up when you deploy on a host with public
SIP and RTP reachability. The SIP route also supports call transfers and
voicemail detection, which the connector does not. See
[07. Phone calls](07-phone-calls.md#configure-self-hosted-livekit-sip) and
[Configure LiveKit in YAML](../targets/livekit.md) for the trunk fields and
the environment it needs.

## Troubleshooting

- **`cloudflared not found on PATH`**: install it (Part 1, Step 4) or pass
  `--public-url` with your own tunnel.
- **`missing telephony credentials/configuration: ...`**: the named `.env`
  values are empty. The error lists exactly which ones.
- **"an application error occurred" on inbound**: run with `--verbose` and read
  the `application` container lines; the bridge logs each step (webhook,
  media WS, room join) and any error.
- **`phone number ... was not found on this Twilio account`**: the number in
  `TWILIO_PHONE_NUMBER` does not exist on this account or is not E.164.
  Check **Phone Numbers → Manage → Active numbers**.
- **Outbound call rings then drops, or never rings**: on a trial account Twilio
  only calls verified numbers, and international destinations need
  **Voice → Settings → Geo Permissions** enabled for that country.
- **Outbound curl returns 401**: the `Authorization` header does not match
  `UNMUTE_OUTBOUND_TOKEN`.

## Restore Twilio when you are done

- Set the number's voice webhook back to the `was:` value the command
  printed, under **Phone Numbers → Manage → Active numbers → your number →
  Voice Configuration**.
- Release test numbers you no longer need; they bill monthly.

Next: [08. Going live](08-going-live.md) for production ingress, secrets,
and capacity.

# Twilio walkthrough, step by step

This guide walks you through testing real phone calls with the
[telephony-multi-task example](../../../examples/telephony-multi-task/README.md),
inbound and outbound, on both routes:

- **Pipecat** with Twilio Programmable Voice (`carrier-websocket`)
- **LiveKit** with Twilio Elastic SIP Trunking (`sip`)

It tells you exactly where each value lives in the Twilio Console. If you
want the concepts behind these steps, read
[07. Phone calls](07-phone-calls.md) first.

> Every telephony route is provisional: it has a real adapter but has not
> passed an automated credentialed smoke. Validation prints an `unverified`
> warning and lets you run it, so the commands below work today. Test the
> behavior you depend on yourself before you rely on it in production.

> For the quickest Pipecat test with no extra agent setup, use the
> [telephony-hello example](../../../telephony-hello/README.md): a minimal
> inbound and outbound Twilio agent. This walkthrough uses the richer
> multi-task example. Note that its Pipecat target sets `on_voicemail`, which
> Pipecat cannot do, so use `telephony-hello` (or this example's LiveKit
> target) when testing Pipecat.

## What you need

- A Twilio account with some balance (calls and numbers cost money).
- Docker with the Compose plugin.
- The Unmute CLI built (`make build`).
- For the Pipecat route: `cloudflared` on PATH, or your own tunnel.
- A real phone to call from and to.

Start from the example:

```bash
cd examples/telephony-multi-task
cp .env.example .env
```

Everything below fills in that `.env`. The dev command supplies
`UNMUTE_PUBLIC_URL`, `REDIS_URL`, `LIVEKIT_URL`, `LIVEKIT_API_KEY`,
`LIVEKIT_API_SECRET`, `LIVEKIT_SIP_INBOUND_TRUNK`, and
`LIVEKIT_SIP_OUTBOUND_TRUNK` by itself. Leave those out of `.env`.

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
unmute dev . --target pipecat --telephony
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
connection, and the agent greets you: "Hi, this is Sage and Stone Salon."

### Step 7: test outbound

Your agent's phone channel must allow it (`outbound: true`). Re-run with `--to`
set to a number you can answer:

```bash
unmute dev . --target pipecat --telephony --to +15551234567
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

## Part 2: the LiveKit route (Elastic SIP Trunking)

LiveKit runs a self-hosted SIP bridge on your machine. Twilio talks SIP and
RTP to it directly, so this route needs real network reachability, not an
HTTPS tunnel.

### Step 1: create the Elastic SIP Trunk

1. In the Twilio Console, go to **Elastic SIP Trunking → Trunks** and
   create a trunk.
2. Open the trunk's **Termination** page. Set a unique termination SIP URI;
   Twilio shows it as `<your-name>.pstn.twilio.com`. Copy it into
   `TWILIO_SIP_ADDRESS`.
3. Still under Termination, create a **Credential List** (a username and a
   strong password) and attach it to the trunk. Copy the values into
   `TWILIO_SIP_USERNAME` and `TWILIO_SIP_PASSWORD`.
4. Open the trunk's **Origination** page and add an origination URI that
   points at your machine's public SIP endpoint, in the form
   `sip:<your-public-host-or-ip>;transport=tcp`.
5. Open the trunk's **Numbers** page and attach your Twilio phone number.
   Reuse `TWILIO_PHONE_NUMBER` from Part 1 or buy a separate number.

Termination is the direction LiveKit dials out through Twilio; origination
is Twilio sending inbound calls to you. You need both for inbound and
outbound.

### Step 2: check your network reality

Twilio must reach SIP port `5060` (TCP and UDP) and the RTP range
`10000-10100/udp` on your machine. That usually means a public IP or router
port forwarding. Docker Desktop's NAT on macOS and Windows often breaks
carrier-reachable SIP even when every local health check is green. If
inbound calls never arrive, this is the first thing to suspect; a Linux
host with plain Docker or a small cloud VM behaves better.

### Step 3: run it

```bash
unmute dev . --target livekit --telephony
```

What happens, in order:

1. Redis, LiveKit Server, and LiveKit SIP start first and become healthy.
2. The command creates or reuses the local records and prints each one:
   inbound trunk, dispatch rule, outbound trunk (`created`, `reused`, or
   `updated`). They live in the local Redis volume and survive restarts.
3. Both trunk IDs are injected and the agent starts.
4. The ready banner and the call line print.

No tunnel and no `--public-url` here: SIP does not ride HTTPS.

### Step 4: test inbound

Call the number attached to the trunk. Twilio sends the call to your
origination URI, LiveKit SIP accepts it, and the dispatch rule creates a
fresh `call-*` room and dispatches the agent into it. You hear the same
salon greeting.

### Step 5: test outbound

Dispatch the agent to a new room with outbound metadata. Point `lk` at the
local server with the generated local-only key pair first:

```bash
export LIVEKIT_URL=http://127.0.0.1:7880
export LIVEKIT_API_KEY=devkey
export LIVEKIT_API_SECRET=devsecret-local-only
lk dispatch create --new-room --agent-name livekit \
  --metadata '{"direction": "outbound", "phone_number": "+15551234567", "call_start": {}}'
```

The worker validates the destination, dials through the stored outbound
trunk with your Credential List auth, and your phone rings. The `devkey`
pair only exists inside the generated local stack; never reuse it anywhere
else.

## Troubleshooting

- **`cloudflared not found on PATH`**: install it (Step 4 above) or pass
  `--public-url` with your own tunnel.
- **`missing telephony credentials/configuration: ...`**: the named `.env`
  values are empty. The error lists exactly which ones.
- **`LIVEKIT_URL conflicts with the generated local LiveKit SIP topology`**
  (or `REDIS_URL`, the API key pair, a trunk ID): remove that value from
  `.env`; the local stack supplies its own.
- **`phone number ... was not found on this Twilio account`**: the number in
  `TWILIO_PHONE_NUMBER` does not exist on this account or is not E.164.
  Check **Phone Numbers → Manage → Active numbers**.
- **Outbound curl returns 401**: the `Authorization` header does not match
  `UNMUTE_OUTBOUND_TOKEN`.
- **LiveKit inbound never rings**: almost always SIP/RTP reachability.
  Recheck Step 2 and your origination URI.
- **Stale records**: stopping preserves the Redis volume on purpose. To
  start clean, run `docker compose -f build/livekit/compose.telephony.yaml
  -p <project-name> down --volumes` with the project name from the ready
  banner.

## Restore Twilio when you are done

- Set the number's voice webhook back to the `was:` value the command
  printed, under **Phone Numbers → Manage → Active numbers → your number →
  Voice Configuration**.
- Release test numbers you no longer need; they bill monthly.

Next: [08. Going live](08-going-live.md) for production ingress, secrets,
and capacity.

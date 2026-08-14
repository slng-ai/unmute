# twilio-telephony-hello

A minimal Twilio phone agent for testing real calls, on **the route each platform
actually recommends for Twilio**. It only greets and chats, so it exercises the
phone path with no tools, tasks, or transfers in the way. Use it to prove your
Twilio wiring before pointing a real agent at the same setup.

Both directions are declared, `inbound: true` and `outbound: true`, so one package
tests a call coming in and a call going out.

## The two routes, which is the point of this package

| Target | Route | How a call gets there | Who hosts it |
|---|---|---|---|
| **pipecat** | `transport: cloud-websocket`, `carrier: twilio` | your number points at a **static TwiML Bin**, whose `<Connect><Stream>` streams the audio to Pipecat Cloud | nobody: no server of yours is in the path, in production or ever |
| **livekit** | `transport: sip`, `carrier: twilio` | your number is attached to a **Twilio Elastic SIP Trunk**, whose origination points at your LiveKit project's SIP URI | the worker; LiveKit Cloud can run it for you |

These are two genuinely different mechanisms, not two spellings of one thing:

- **Media Streams over a WebSocket** (pipecat). Twilio sends raw audio frames to a
  socket. The carrier's view of the call is a stream; call control happens over
  Twilio's REST API.
- **SIP** (livekit). Twilio hands the call over as a SIP session with its own
  signalling and RTP media. The carrier's view is a call leg, which is why this is
  the only route where a transfer can move that leg somewhere else.

That difference decides what each can do. SIP is the route with cold transfer, warm
transfer, and voicemail detection; it is also the documented default multi-carrier
LiveKit route. The `cloud-websocket` route can do cold transfer by a different
mechanism (it replaces the live call's markup) and cannot do warm at all.
[docs/TELEPHONY.md](../../docs/TELEPHONY.md) has the route comparison and
[docs/TRANSFERS.md](../../docs/TRANSFERS.md) has the transfer mechanisms.

**A previous version of this package used `transport: connector` for LiveKit**, our
own Twilio Media Streams bridge. It is easier to test on a laptop and carries no
transfers, so it taught a route you have to leave as soon as you need one. That
route still ships and is exercised by [outbound-reminder](../outbound-reminder).

## What you need

- A Twilio account with a **voice-capable phone number**.
- For the **livekit** target, a **Twilio Elastic SIP Trunk**: termination (which
  gives you the dial-out host and a credential list), origination pointed at your
  LiveKit project SIP URI, and **your number attached to the trunk**. The generated
  `build/livekit/README.md` dictates all of it with your own values; its
  `## Configure` section is the authority and this page does not copy it.
- Model provider keys for the agent to think, hear, and speak.
- `uv` on your PATH, for the **pipecat** target's local run.
- Docker Desktop (or Docker Engine) with the Compose plugin, for the **livekit**
  target's local run. The pipecat target needs no Docker.
- `cloudflared` on your PATH, for the tunnel the **pipecat** local run uses:
  ```bash
  brew install cloudflared
  ```
  On Linux, install the distribution package or a binary from the
  cloudflare/cloudflared releases page. No Cloudflare account is needed. To use
  your own tunnel instead, pass `--public-url https://your-tunnel.example`. The
  livekit target needs no tunnel, because a tunnel cannot carry SIP.

**One number serves one target at a time.** A number attached to a SIP trunk
ignores its voice configuration, so it cannot also point at a TwiML Bin. Take it
off the trunk to test the pipecat target, and put it back to test the livekit one.

## Credentials

Two groups, because the two routes ask Twilio for different things. Put them in
`examples/twilio-telephony-hello/.env` (gitignored). Values are examples; use your
own.

```bash
# Both targets: the agent's own model providers
OPENAI_API_KEY=sk-...                # the LLM
SLNG_API_KEY=...                     # speech-to-text and text-to-speech

# pipecat target: Twilio's REST account credentials
TWILIO_ACCOUNT_SID=ACxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
TWILIO_AUTH_TOKEN=your_auth_token
TWILIO_PHONE_NUMBER=+15551234567     # your voice-capable number, E.164
PIPECAT_CLOUD_ORGANIZATION=zonal-bison-orange-168

# livekit target: the SIP trunk's own dial-out settings
SIP_TRUNK_HOSTNAME=your-trunk.pstn.twilio.com
SIP_AUTH_USERNAME=...
SIP_AUTH_PASSWORD=...
SIP_FROM_NUMBER=+15551234567         # the same number is fine
```

`PIPECAT_CLOUD_ORGANIZATION` comes from `pipecat cloud organizations list`. It is
the hyphenated machine **slug**, not your display name; the CLI's heading for that
column has changed between versions. It is needed because the markup a call arrives
on has to name the deployed agent, and the compiler knows the agent name but not
your organization.

The livekit build's `.env.example` ends with four more names under a "supplied
for you, not by you" heading:

| Variable | Who supplies it |
|---|---|
| `LIVEKIT_URL` | LiveKit Cloud injects it into the deployed agent; the local Compose graph sets it for a local run |
| `LIVEKIT_API_KEY` | the same, as a pair with the secret |
| `LIVEKIT_API_SECRET` | the same |
| `REDIS_URL` | LiveKit Cloud's managed SIP service owns it; the generated Compose graph ships Valkey locally |

Set those four only for a local run or a self-hosted deployment. On LiveKit
Cloud the platform provides all four and drops them from any secrets file.

Every name here must be a valid shell identifier: letters, digits, underscores,
never starting with a digit. LiveKit Cloud exports secrets through a shell, so a
name like `11LABS_API_KEY` fails at export and the value is simply missing at
runtime, with one `/etc/run/env: ... not a valid identifier` line as the only clue.

## Which target does what locally

This is the honest table, and the asymmetry is the transport's, not the CLI's.

| | pipecat (`cloud-websocket`) | livekit (`sip`) |
|---|---|---|
| local inbound call | **yes**, `dev --telephony` | **no**: see below |
| local outbound call | no: see below | **yes**, `--to` |
| runs locally as | `uv run bot.py`, no Docker | a Docker Compose graph: agent, Redis, LiveKit Server, LiveKit SIP |
| tunnel | managed cloudflared | none, and none would help |
| hosted by you in production | nothing | the worker |

**Why local inbound differs.** Twilio reaches the pipecat route over HTTPS and WSS,
which a tunnel carries, so the dev command borrows your number's voice
configuration and a real call lands on your laptop. SIP is not HTTP: an inbound call
needs the carrier to reach SIP signalling and an RTP media range at a routable
address, and an HTTPS tunnel is neither required nor sufficient for that. Everything
in the local SIP graph can be healthy and the call still never arrives. So inbound
on the livekit target is tested against the deploy.

**Why local outbound differs.** On the pipecat route an outbound call is created at
Twilio with markup naming the **deployed** agent, so the call reaches Pipecat Cloud
rather than your laptop; there is nothing for a local session to answer, `--to` says
so, and the outbound command lives in the generated `build/pipecat/README.md`. On
the livekit route the call starts from your side, dialling out through the trunk
with the four `SIP_*` values passed inline, so `--to` works locally.

Between the two, you can hear both directions on a laptop before deploying
anything. Just not both on the same target.

## Test an inbound call

On **pipecat**, from the repository root:

```sh
unmute dev examples/twilio-telephony-hello --telephony --target pipecat
```

Watch for the tunnel URL, the webhook update, and the line `call +1...`. Call that
number. The agent answers and greets you; speak and it replies. `ctrl-c` stops
everything and puts the number's previous voice configuration back. Your TwiML Bin
is never touched: the local runner answers Twilio's webhook itself.

On **livekit**, inbound is a deploy exercise. See **Deploy it**.

## Test an outbound call

On **livekit**, add `--to` with a number you can answer:

```sh
unmute dev examples/twilio-telephony-hello --telephony --target livekit --to +15551234567
```

Once the Compose graph is healthy the CLI dispatches the agent and the call goes out
through your trunk. Your phone rings and the agent talks when you answer. If the
call connects but you hear nothing, that is the RTP path back to the container
rather than anything in the agent; the local SIP section of
[docs/TELEPHONY.md](../../docs/TELEPHONY.md) covers it.

On **pipecat**, outbound runs against the deployed agent:

```sh
unmute compile examples/twilio-telephony-hello
cd examples/twilio-telephony-hello/build/pipecat
# fill .env, push the secret set, deploy, then run the outbound command from
# this directory's README
```

If Twilio refuses either call, the reason is printed in Twilio's own words (for
example geographic permissions for the destination country, or a trial account that
can only call verified numbers). Fix it in the Twilio Console and run again.

## Deploy it

```sh
bin/unmute compile examples/twilio-telephony-hello
```

**pipecat**, to Pipecat Cloud. Nothing of yours runs anywhere, so the deploy is the
agent and its secret set, and the carrier side is markup you paste once:

```sh
cd examples/twilio-telephony-hello/build/pipecat
cp .env.example .env                        # then fill in the values
pipecat cloud secrets set <set-name> --file .env --region eu-central
pipecat cloud deploy
pipecat cloud agent status <agent-name>
```

The secret set comes first: the emitted `pcc-deploy.toml` already names it and a
deploy cannot start without it, and it carries the same region as the deployment.
Wait for `status` to say `ready`. Then follow **Telephony setup** in
`build/pipecat/README.md`, which dictates the TwiML Bin with your own values and
says where to paste it.

**livekit**, to LiveKit Cloud or a worker you host:

```sh
cd examples/twilio-telephony-hello/build/livekit
cp .env.example .env                        # then fill in the values
lk agent create --region eu-central --secrets-file .env   # first deploy only
bash telephony-setup.sh                     # inbound trunk + dispatch rule
lk agent status
```

`lk agent create` is the first deploy and writes the `livekit.toml` this directory
deliberately does not ship, because the platform assigns both of its values. Every
later version is `lk agent deploy`. `telephony-setup.sh` creates the inbound trunk
and the dispatch rule, resolving the trunk by the phone number you already have, so
no record ID is ever copied by hand; run it twice and the second run should report
everything reused.

Then the step people miss: **attach your number to the trunk**. A number that is
not on the trunk never reaches LiveKit, however correct the origination URI is.
`build/livekit/README.md` has the commands, and
[livekit-human-transfer](../livekit-human-transfer) walks the whole sequence with
the checks at each step.

## Regions

Both targets pin `deployment_region: eu-central`, so the first deploy of either one
never stops to ask. The pin means different things on the two platforms.

On **pipecat** the one line drives three things at once, and the platform requires
them to agree: the `region` in the emitted `pcc-deploy.toml`, the `--region` on the
emitted `secrets set` command, and the `wss://eu-central.api.pipecat.daily.co/...`
host in the markup you paste into Twilio. A regional stream endpoint routes **only**
to agents in that region, and an agent can only read a secret set from its own
region. Change the line, recompile, and re-paste the address.

On **livekit** it is where LiveKit Cloud runs the agent. It is chosen at the first
deploy and is **immutable**: moving one means creating the agent in the new region
and deleting the old one, because the platform will not move it in place.

Both codes are forwarded exactly as written and never checked here, so a typo fails
the platform CLI rather than the compile (SCHEMA N32). `pipecat cloud regions list`
prints the Pipecat ones; the LiveKit Cloud project settings list theirs.

## A note on route maturity

Both routes have real adapters, so `unmute validate`, `unmute compile`, and
`unmute dev --telephony` run them with no warning and no error. The
provisional-versus-verified status is internal maturity tracking, recorded in the
generated `compile-report.json`, and it tracks whether a **credentialed smoke runs
in CI**, which is a different question from whether anyone has made a phone call.
Live-call evidence, dated, is in the Status table of
[docs/TRANSFERS.md](../../docs/TRANSFERS.md). Test the behaviour you rely on
yourself before you depend on it in production.

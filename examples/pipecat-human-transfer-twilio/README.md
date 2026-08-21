# pipecat-human-transfer-twilio

A salon agent reached through **your own Twilio number**, with **nothing hosted
by you in production**. Inbound calls, outbound calls, and a cold transfer to a
person.

Twilio streams the call's audio straight to Pipecat Cloud, which starts the
deployed agent. There is no server of yours in the production path: the whole
carrier side is a small piece of static markup that lives in the Twilio console.

Three existing fields select the route (`transport: cloud-websocket`, `carrier`,
`connection`) and no new authoring field exists.

---

## How a call gets here

```
caller → your Twilio number → a TwiML Bin in the Twilio console
                                        │
                                        │  <Connect><Stream> to the platform
                                        ▼
                          Pipecat Cloud starts this agent
                          and receives the audio directly
```

That is the whole diagram. Compare it with the other Twilio route on Pipecat,
`transport: carrier-websocket` (it is what [outbound-reminder](../outbound-reminder)
declares): there, the thing Twilio talks to is a webhook and a media socket of
**yours**, which has to keep running wherever calls should land, forever.

## What you need

One account and three values, plus one lookup:

| Value | Where it comes from |
|---|---|
| `OPENAI_API_KEY` | the reasoning model |
| `SLNG_API_KEY` | the listen and speak models |
| `TWILIO_ACCOUNT_SID` + `TWILIO_AUTH_TOKEN` | your Twilio account dashboard |
| `TWILIO_PHONE_NUMBER` | a voice-capable number you own in that account |
| `BILLING_PHONE_NUMBER` | wherever the transfer should land; your own mobile works for a test |
| `PIPECAT_CLOUD_ORGANIZATION` | `pipecat cloud organizations list`, pasted into the markup once. It is the hyphenated machine slug (`zonal-bison-orange-168`), not your display name; the CLI's heading for that column has changed between versions. The route supplies it at runtime, so it is not in `secrets:` |

The same three `TWILIO_*` names
[twilio-telephony-hello](../twilio-telephony-hello)'s Pipecat target uses, so one
`.env` drives every Pipecat Twilio example here. (Its LiveKit target reads four
`SIP_*` names instead, because a SIP trunk's dial-out settings are not the account's
REST credentials.) Values never go in this package; the package carries environment
variable **names** only.

**No Daily key, and no Daily account.** This route touches no Daily API, so
`DAILY_API_KEY` is not required and is not asked for.

## Run it

The generated `build/pipecat/README.md` is the runbook, written to be followed
with nothing else open. This file is the why; that file is the how.

```sh
bin/unmute validate examples/pipecat-human-transfer-twilio
bin/unmute compile examples/pipecat-human-transfer-twilio
```

Look at what compiling did **not** emit: no Redis, no media websocket, no
`telephony.py`, and no server of any kind. The file list is exactly what a
Pipecat Cloud build with no telephony emits, and a test asserts that rather than
trusting it. `build/` is disposable and gitignored.

To hear the agent with no phone in the picture, and no accounts at all:

```sh
bin/unmute dev examples/pipecat-human-transfer-twilio              # browser, needs uv
```

To take a call and run the transfer **before** deploying anything, and without a
Twilio account:

```sh
bin/unmute dev --telephony examples/pipecat-human-transfer-twilio
```

That runs the generated agent on your machine with `uv` and makes the CLI the
carrier: it speaks Twilio's media-stream protocol over loopback, places the call
itself, and connects your microphone to it. **No account, no number, no tunnel,
and nothing leaves your machine.** Credentials are minted for the run and
override any real ones in your environment.

Ask for the handoff and the transfer runs: one request replaces the live call's
markup at the carrier, which here is the CLI. The caller's leg leaves the agent,
the destination leg is recorded separately, and the run prints how far it got. A
local run proves the handoff and never proves that a person answered, which is
the one part no local run can witness.

The production shape is the same one: the transfer stays at Twilio, and after the
destination leg ends Twilio ends the original call. It never needs a deployed
copy of the agent for a handback.

To take a **real** call on your own number, add `--carrier`. That opens a
temporary Cloudflare HTTPS/WSS tunnel and borrows the declared number's voice
configuration for the session, answering Twilio at `POST /` and streaming the
audio to `wss://<tunnel-host>/ws`; the previous configuration is restored when
you stop it, and your TwiML Bin is never touched. Within that mode,
`--public-url https://your-tunnel.example` brings your own tunnel and
`--no-webhook` leaves the number alone so you can point its voice webhook at the
printed origin yourself. Both change the local test only; the production TwiML
Bin path above stays the same.

Pipecat `cloud-websocket` requires explicit `on_unavailable: hangup`; it cannot
reconnect the original media stream. Omitting the field resolves to
`return_to_caller`, so validation refuses it on this route. A successful Twilio
REST update means the transfer has started, not that the destination answered;
the tool result is `transfer_started`.

## Deploy to Pipecat Cloud

Authenticate once with `pipecat cloud auth login`, then four commands, in this
order. The generated runbook prints them with this package's own names already
filled in:

```sh
cd examples/pipecat-human-transfer-twilio/build/pipecat
cp .env.example .env             # then fill in the values
pipecat cloud secrets set pipecat-secrets --file .env --region eu-central
pipecat cloud deploy
pipecat cloud agent status pipecat
```

The secret set comes first because the emitted `pcc-deploy.toml` already names it
and a deploy cannot start without it, and it carries the same `--region` as the
deployment for the reason under **Regions, in one line** below. `deploy` builds the image in the
cloud from the emitted `Dockerfile`, which is why the manifest names no image.
Wait for `status` to report **`ready`**: that, not a successful deploy command, is
the deploy being usable.

Then the carrier side, which is the whole of what makes this route different: open
`build/pipecat/README.md` and follow **Telephony setup**. Four steps, three of them
in the Twilio console, and nothing to run here. You paste one small piece of static
markup into a TwiML Bin, point your number at it, and the phone path is deployed.

## Regions, in one line

`targets.yaml` declares `deployment_region: eu-central`, and that is the only place
a region is written. Three things are rendered from it, and the platform requires
them to agree:

| What | Where it lands |
|---|---|
| where the agent runs | `region` in the emitted `pcc-deploy.toml` |
| where its secrets live | the `--region` on the emitted `secrets set` command |
| where Twilio streams to | the `wss://eu-central.api.pipecat.daily.co/...` host in the markup you paste and in the outbound command |

A regional stream endpoint routes **only** to agents deployed in that region, and
an agent can only read a secret set from its own region. So change that one line to
your own region, recompile, and re-paste the address into your TwiML Bin.
`pipecat cloud regions list` prints what is available.

Two things bite when moving an agent that already exists: agent names are globally
unique **across** regions, and a secret set is region-scoped with a globally unique
name. The generated README's **One region, three places** section spells out both.
Region codes are forwarded exactly as written and never checked here, so a typo
fails `pipecat cloud deploy` rather than compiling.

## Why outbound is declared, on a package about receiving calls

Because it is how you test a transfer without waiting for somebody to call you.
Place a call to your own mobile with the one command in the generated runbook,
answer it, and ask the agent about an invoice. The transfer path is the same one
an inbound caller takes.

## What is not here

**Warm transfer. Unmute does not support it on any Pipecat target.** The refusal
on this route names what it would take: acting on the destination leg while the
caller waits needs call control this route does not have. Warm compiles on
LiveKit SIP today, in
[livekit-human-transfer](../livekit-human-transfer). The
[transfer overview](../../docs-site/transfers/overview.mdx) has the capability
map and sources.

**Session survival through a transfer.** There is no handback. When the person
hangs up, or when the destination declines or never answers, Twilio ends the
original call. This avoids starting a fresh agent with no conversation context.
The [telephony overview](../../docs-site/telephony/overview.mdx) compares the
routes.

**Caller-number variables.** A variable sourced from the caller's number, the
called number, the call identifier, or the direction is refused on this route, by
name. The Bin does pass the caller and callee numbers, and the agent reads them
for the transfer, but the code that binds them to spec variables belongs to the
carrier-websocket adapter this route does not emit. Granting them would validate
green and hand the agent empty strings on a live call.

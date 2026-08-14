# pipecat-human-transfer-twilio

The same salon agent as [pipecat-human-transfer-daily](../pipecat-human-transfer-daily), reached
through **your own Twilio number**, with **nothing hosted by you**. Inbound calls,
outbound calls, and a cold transfer to a person.

Twilio streams the call's audio straight to Pipecat Cloud, which starts the
deployed agent. There is no server of yours in the path, in production or ever:
the whole carrier side is a small piece of static markup that lives in the Twilio
console.

Three existing fields select the route (`transport: cloud-websocket`, `carrier`,
`connection`) and no new authoring field exists (SCHEMA N38).

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

**No Daily key, and no Daily anything.** This route touches no Daily API, so
`DAILY_API_KEY` is not required and is not asked for. That is the visible
difference from [pipecat-human-transfer-daily](../pipecat-human-transfer-daily).

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
bin/unmute dev examples/pipecat-human-transfer-twilio              # browser
bin/unmute dev --console examples/pipecat-human-transfer-twilio    # terminal
```

To take a real call on your own number **before** deploying anything:

```sh
bin/unmute dev --telephony examples/pipecat-human-transfer-twilio
```

That runs this agent on your machine behind a cloudflared tunnel and borrows the
declared number's voice configuration for the length of the session, putting the
previous one back when you stop it. Your TwiML Bin is never touched, because the
local runner answers Twilio's webhook itself. One limit worth knowing: the
transfer's own markup names the **deployed** stream address, so a transfer during
a local session hands the caller back to the deployed agent rather than to your
laptop. `PIPECAT_CLOUD_ORGANIZATION` therefore has to be the organization
**slug** even for a local run, or that handback reaches nothing and the call
simply drops.

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
| where Twilio streams to | the `wss://eu-central.api.pipecat.daily.co/...` host in the markup you paste, in the outbound command, and in the transfer's reconnect |

A regional stream endpoint routes **only** to agents deployed in that region, and
an agent can only read a secret set from its own region. So change that one line to
your own region, recompile, and re-paste the address into your TwiML Bin.
`pipecat cloud regions list` prints what is available.

Two things bite when moving an agent that already exists: agent names are globally
unique **across** regions, and a secret set is region-scoped with a globally unique
name. The generated README's **One region, three places** section spells out both.
Region codes are forwarded exactly as written and never checked here, so a typo
fails `pipecat cloud deploy` rather than compiling (SCHEMA N32).

## Why outbound is declared, on a package about receiving calls

Because it is how you test a transfer without waiting for somebody to call you.
Place a call to your own mobile with the one command in the generated runbook,
answer it, and ask the agent about an invoice. The transfer path is the same one
an inbound caller takes.

## What is not here

**Warm transfer.** No Pipecat route offers it today, on any transport. The
refusal on this route names what it would take: acting on how the destination's
leg ended needs a callback endpoint you host, which is the one cost this route
exists to remove. Warm compiles on LiveKit SIP today, in
[livekit-human-transfer](../livekit-human-transfer). The capability map with sources is
[docs/TRANSFERS.md](../../docs/TRANSFERS.md).

**Session survival through a transfer.** However the dial ends, the caller hears
one spoken line and meets a **fresh** agent that does not remember the call. That
is both endings: a dial that never connected, and a completed transfer the person
ended by hanging up first. Nothing in the markup can tell them apart without a
callback endpoint, so the line names neither ("Putting you back to the
assistant."). Both endings are written into the generated runbook, and the Daily
carrier route is the one that keeps the session. The route comparison is in
[docs/TELEPHONY.md](../../docs/TELEPHONY.md).

**Caller-number variables.** A variable sourced from the caller's number, the
called number, the call identifier, or the direction is refused on this route, by
name. The Bin does pass the caller and callee numbers, and the agent reads them
for the transfer, but the code that binds them to spec variables belongs to the
carrier-websocket adapter this route does not emit. Granting them would validate
green and hand the agent empty strings on a live call.

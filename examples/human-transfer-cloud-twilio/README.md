# human-transfer-cloud-twilio

The same salon agent as [human-transfer-daily](../human-transfer-daily), reached
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

That is the whole diagram. Compare it with
[human-transfer-daily-twilio's](../../specs/006-pipecat-carrier-telephony/) route,
which needs a small webhook server running wherever calls should land, forever.

## The whole runbook

The generated `build/pipecat/README.md` is the runbook, written to be followed
with nothing else open. This file is the why; that file is the how.

```sh
bin/unmute validate examples/human-transfer-cloud-twilio
bin/unmute compile examples/human-transfer-cloud-twilio
```

Then open `build/pipecat/README.md` and follow **Telephony setup**: four steps,
three of them in the Twilio console, and nothing to run here.

Look at what compiling did **not** emit: no Redis, no media websocket, no
`telephony.py`, and no server of any kind. The file list is exactly what a
Pipecat Cloud build with no telephony emits, and a test asserts that rather than
trusting it. `build/` is disposable and gitignored.

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

## What you need

One account and three values, plus one lookup:

| Value | Where it comes from |
|---|---|
| `TWILIO_ACCOUNT_SID` + `TWILIO_AUTH_TOKEN` | your Twilio account dashboard |
| `TWILIO_PHONE_NUMBER` | a voice-capable number you own in that account |
| `BILLING_PHONE_NUMBER` | wherever the transfer should land; your own mobile works for a test |
| your organization name | `pipecat cloud organizations list`, pasted into the markup once |

The same three `TWILIO_*` names [telephony-hello](../telephony-hello) uses, so one
`.env` drives every Twilio example here. Values never go in this package; the
package carries environment variable **names** only.

**No Daily key, and no Daily anything.** This route touches no Daily API, so
`DAILY_API_KEY` is not required and is not asked for. That is the visible
difference from both `human-transfer-daily` examples.

## Why outbound is declared

Because it is how you test a transfer without waiting for somebody to call you.
Place a call to your own mobile with the one command in the generated runbook,
answer it, and ask the agent about an invoice. The transfer path is the same one
an inbound caller takes.

## What is not here

**Warm transfer.** No Pipecat route offers it today, on any transport. The
refusal on this route names what it would take: acting on how the destination's
leg ended needs a callback endpoint you host, which is the one cost this route
exists to remove. Warm compiles on LiveKit SIP today, in
[human-transfer](../human-transfer). The capability map with sources is
[docs/TRANSFERS.md](../../docs/TRANSFERS.md).

**Session survival through a failed transfer.** If the dial does not connect, the
caller hears a spoken line and meets a **fresh** agent that does not remember the
call. Same when a completed transfer ends because the other side hangs up first.
Both are limits of having no callback endpoint, both are written into the
generated runbook, and the Daily carrier route is the one that keeps the session.
The route comparison is in [docs/TELEPHONY.md](../../docs/TELEPHONY.md).

**Caller-number variables.** A variable sourced from the caller's number, the
called number, the call identifier, or the direction is refused on this route, by
name. The Bin does pass the caller and callee numbers, and the agent reads them
for the transfer, but the code that binds them to spec variables belongs to the
carrier-websocket adapter this route does not emit. Granting them would validate
green and hand the agent empty strings on a live call.

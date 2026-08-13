# human-transfer-daily-twilio

The same salon agent as [human-transfer-daily](../human-transfer-daily), reached
through **your own Twilio number** instead of a number bought from Daily. Inbound
calls, outbound calls, and a cold transfer, all carried by your own trunk.

Why you might want this one: buying a Daily number needs Daily provisioning you
may not have, and it locks the number up for two weeks. If you already hold a
voice-capable number at a carrier, this route uses it.

What changes from the no-carrier form is only the way in and the way out. The
agent, the room, and the transfer are the same code. Three existing fields select
it (`transport`, `carrier`, `connection`) and no new authoring field exists
(SCHEMA N37).

---

## How a call gets here

Daily's SIP addresses are per room and rooms are made per call, so your carrier
has nowhere static to forward to. One small emitted server closes that gap:

```
caller → your Twilio number → telephony_helper.py  → creates a Daily room
                                    │                  with a SIP address
                                    │
                                    ├→ starts the deployed agent on that room
                                    └→ answers the carrier with hold audio
                                                    │
                       the agent's room SIP leg goes ready
                                                    │
                       the agent moves the live call into the room, once
```

`telephony_helper.py` is compiled into `build/pipecat/` and **you run it**. The
deployed agent still exposes no public endpoint of its own.

## The whole runbook

The generated `build/pipecat/README.md` is the runbook, and it is written to be
followed with nothing else open. This file is the why; that file is the how.

Two accounts, three keys:

| Key | Whose | What it does here |
|---|---|---|
| `TWILIO_ACCOUNT_SID` + `TWILIO_AUTH_TOKEN` | your Twilio account | the one request that moves a live call into the room |
| `DAILY_API_KEY` | your Daily developer account | creates the per-call room, and the transfer |
| `PIPECAT_CLOUD_API_KEY` | your Pipecat Cloud org | starts agent sessions, which is how every call begins |

Plus your trunk's termination address (`SIP_TRUNK_HOSTNAME`), your number
(`SIP_FROM_NUMBER`), and the transfer destination (`BILLING_PHONE_NUMBER`). Nine
values, and there is nothing to invent: every one of them is something you already
have or something a dashboard gives you.

Values never go in this package. The package carries env var **names** only.

### Ask Daily to enable dial-out first

Do this before anything else, because a person at Daily approves it and that
takes time. Dial-out is a paid feature granted on request, per domain, and
international is granted separately. Outbound calls and the cold transfer both
need it, and `unmute validate` says so.

It covers dialling a **SIP address** as well as a phone number, which is what
this route does, so **you do not need to buy a Daily number.**

### Then

```sh
bin/unmute validate examples/human-transfer-daily-twilio
bin/unmute compile examples/human-transfer-daily-twilio
```

Then open `build/pipecat/README.md` and follow its "Telephony setup" section: four
actions in the Twilio Console, two commands here.

Look at what compiling did **not** emit: no Redis, no media websocket, no
`telephony.py`. The carrier hands the call to Daily over SIP, so there is no
per-carrier protocol in the agent and no shared control store anywhere. `build/`
is disposable and gitignored.

## If you already did the LiveKit setup

[human-transfer](../human-transfer) sets up the same Twilio account for LiveKit
SIP. This target reuses that account, that trunk, and that number. Two lines of
your `.env` carry over unchanged (`SIP_TRUNK_HOSTNAME`, `SIP_FROM_NUMBER`), and
two go unused: this route has no `sip_username` and no `sip_password`, because
Daily's outbound SIP carries no credentials on any documented surface. Termination
authenticates Daily by IP allow-list instead, which the generated runbook dictates.

A number serves one target at a time. Moving it between the two is one change at
the carrier, in either direction, and the generated runbook says which.

## What is not here

**Warm transfer.** Daily documents a warm pattern; this project has not built it
on any Pipecat route yet, because it needs the generated bot to own the call's
audio. Nothing about this carrier leg blocks it: the carrier call joins the same
room a Daily-provisioned call joins. Warm compiles on LiveKit SIP today, in
[human-transfer](../human-transfer). The capability map with sources is
[docs/TRANSFERS.md](../../docs/TRANSFERS.md).

**Caller-number variables.** A variable sourced from the caller's number, the
called number, the call identifier, or the direction is refused on this route, by
name. The values exist in the helper's payload, but the code that puts them where
the agent reads them belongs to the carrier-websocket adapter this route does not
emit. Granting them would validate green and hand the agent empty strings on a
live call, so the refusal is deliberate and it names the routes where those
sources do work.

**Local `unmute dev --telephony`.** Still refused on this route, and the message
says what to run instead. Testing means running the helper beside a tunnel, which
the generated runbook dictates.

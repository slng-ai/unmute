# telephony-multi-task

The multi-task salon agent with a phone channel, inbound and outbound. One
agent delegates customer records and appointment work to tasks, answers
calls, places calls, and can cold-transfer the caller to a person.

It declares two Twilio routes for the same agent:

- `pipecat`: Twilio Programmable Voice over `carrier-websocket`.
- `livekit`: Twilio Elastic SIP Trunking into the self-hosted LiveKit SIP
  bridge over `sip`.

For the full setup, including where every value lives in the Twilio Console,
follow the step-by-step
[Twilio walkthrough](../../docs/user/learn/twilio-walkthrough.md).

> Honest status: every telephony route is still provisional, so `unmute
> validate`, `unmute compile`, and `unmute dev --telephony` fail closed on
> this package today. The configuration and the test steps below describe
> the promoted-route behavior; they run once the exact route passes its
> credentialed call smokes.

## Set up once

From the repo root:

1. Build the CLI and put it on your PATH: `make install` (or use
   `bin/unmute` after `make build` and adjust the commands below to
   `bin/unmute ... examples/telephony-multi-task`).
2. `cd examples/telephony-multi-task`
3. Copy `.env.example` to `.env` and fill in the values. The walkthrough
   explains where each one comes from.
4. For the Pipecat route, install cloudflared (macOS:
   `brew install cloudflared`), or plan to pass `--public-url` with your own
   tunnel.
5. For the LiveKit route, create the Twilio Elastic SIP Trunk (termination
   URI, Credential List, origination URI, attached number).

The dev command supplies `UNMUTE_PUBLIC_URL`, `REDIS_URL`, the local LiveKit
key pair, and both LiveKit trunk IDs itself. Never set those in `.env`. The
`REDIS_URL` name is kept for compatibility; the container behind it is
Valkey (BSD-3-Clause), not Redis, so the whole local stack stays open source.

## Test the Pipecat route

Start it:

```bash
unmute dev . --target pipecat --telephony
```

Leave this terminal running and watch its output top to bottom: the
resolved route, the tunnel origin (`managed tunnel https://<random>.trycloudflare.com`),
the exact public endpoints, the ready banner, the Twilio webhook update
(with the previous value so you can restore it), and finally
`call +1XXXXXXXXXX, ctrl-c to stop`.

**Inbound:** call the printed number from your phone. You should hear:
"Hi, this is Sage and Stone Salon. How can I help with your appointment?"

**Outbound:** open a second terminal in this directory. Copy the tunnel
origin from the first terminal's output and export it there, then trigger a
call to your own phone:

```bash
export UNMUTE_PUBLIC_URL=https://<the-origin-you-just-saw>
source .env   # loads UNMUTE_OUTBOUND_TOKEN into this shell

curl -X POST "$UNMUTE_PUBLIC_URL/telephony/outbound" \
  -H "Authorization: Bearer $UNMUTE_OUTBOUND_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"to": "+15551234567", "call_start": {}}'
```

A `{"session_id": ..., "call_id": ..., "status": "accepted"}` reply means
Twilio is dialing. Your phone rings, and the same agent answers. `UNMUTE_PUBLIC_URL`
is only set inside the running container, never in your shell, so it must be
exported by hand in this second terminal; the tunnel origin also changes on
every run, so copy it fresh each time.

## Test the LiveKit route

Start it:

```bash
unmute dev . --target livekit --telephony
```

In order, the command starts Redis-protocol Valkey, LiveKit Server, and
LiveKit SIP; creates or reuses the inbound trunk, then the `call-` dispatch
rule, then the outbound trunk on the local server; injects both trunk IDs;
and starts the agent. Your machine must be reachable from Twilio on SIP port
5060 and UDP ports 10000-10100; Docker Desktop NAT often blocks this even
when every health check is green (see the walkthrough for the limits).

**Inbound:** call the number attached to the Twilio trunk. Twilio sends the
call to your origination URI, LiveKit SIP answers, and the dispatch rule
puts the caller in a fresh `call-*` room with the agent.

**Outbound:** dispatch the agent to a new room with outbound job metadata.
Point `lk` at the local server first, in a second terminal:

```bash
export LIVEKIT_URL=http://127.0.0.1:7880
export LIVEKIT_API_KEY=devkey
export LIVEKIT_API_SECRET=secret
lk dispatch create --new-room --agent-name livekit \
  --metadata '{"direction": "outbound", "phone_number": "+15551234567", "call_start": {}}'
```

The worker validates the number, dials through the stored outbound trunk,
and your phone rings. The `devkey` pair is the generated local-only pair
from `compose.telephony.yaml`; it is not a secret and must never be used
outside this local stack.

## Stop and clean up

`ctrl-c` stops this package's Compose project, kills the managed tunnel, and
keeps the Valkey data volume, so the LiveKit records are reused on the next
run. To wipe the local records too, remove the project's volumes using the
project name from the ready banner (`compose project: unmute-...`):

```bash
docker compose -f build/livekit/compose.telephony.yaml -p <project-name> down --volumes
```

Restore the previous Twilio voice webhook from the value the command printed
if you are done testing the Pipecat route.

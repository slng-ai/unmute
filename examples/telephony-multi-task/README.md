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

1. Copy `.env.example` to `.env` and fill in the values. The walkthrough
   explains where each one comes from.
2. For the Pipecat route, install cloudflared (macOS:
   `brew install cloudflared`), or plan to pass `--public-url` with your own
   tunnel.
3. For the LiveKit route, create the Twilio Elastic SIP Trunk (termination
   URI, Credential List, origination URI, attached number).

The dev command supplies `UNMUTE_PUBLIC_URL`, `REDIS_URL`, the local LiveKit
key pair, and both LiveKit trunk IDs itself. Never set those in `.env`.

## Test the Pipecat route

Start it:

```bash
unmute dev . --target pipecat --telephony
```

Wait for the ready banner. The command prints the tunnel origin, sets the
Twilio voice webhook automatically (and prints the previous webhook so you
can restore it), and ends with `call +1XXXXXXXXXX, ctrl-c to stop`.

**Inbound:** call the printed number from your phone. You should hear:
"Hi, this is Sage and Stone Salon. How can I help with your appointment?"

**Outbound:** trigger a call to your own phone through the generated
endpoint. Use the tunnel origin the command printed and your
`UNMUTE_OUTBOUND_TOKEN` value:

```bash
curl -X POST "$UNMUTE_PUBLIC_URL/telephony/outbound" \
  -H "Authorization: Bearer $UNMUTE_OUTBOUND_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"to": "+15551234567", "call_start": {}}'
```

A `{"session_id": ..., "call_id": ..., "status": "accepted"}` reply means
Twilio is dialing. Your phone rings, and the same agent answers.

## Test the LiveKit route

Start it:

```bash
unmute dev . --target livekit --telephony
```

The command starts Redis, LiveKit Server, and LiveKit SIP, creates or reuses
the inbound trunk, outbound trunk, and `call-` dispatch rule on the local
server, injects both trunk IDs, and then starts the agent. Your machine must
be reachable from Twilio on SIP port 5060 and UDP ports 10000-10100; Docker
Desktop NAT often blocks this even when every health check is green (see the
walkthrough for the limits).

**Inbound:** call the number attached to the Twilio trunk. Twilio sends the
call to your origination URI, LiveKit SIP answers, and the dispatch rule
puts the caller in a fresh `call-*` room with the agent.

**Outbound:** dispatch the agent to a new room with outbound job metadata.
Point `lk` at the local server first:

```bash
export LIVEKIT_URL=http://127.0.0.1:7880
export LIVEKIT_API_KEY=devkey
export LIVEKIT_API_SECRET=devsecret-local-only
lk dispatch create --new-room --agent-name livekit \
  --metadata '{"direction": "outbound", "phone_number": "+15551234567", "call_start": {}}'
```

The worker validates the number, dials through the stored outbound trunk,
and your phone rings. The `devkey` pair is the generated local-only pair
from `compose.telephony.yaml`; it is not a secret and must never be used
outside this local stack.

## Stop and clean up

`ctrl-c` stops this package's Compose project, kills the managed tunnel, and
keeps the Redis data volume, so the LiveKit records are reused on the next
run. To wipe the local records too, remove the project's volumes using the
project name from the ready banner (`compose project: unmute-...`):

```bash
docker compose -f build/livekit/compose.telephony.yaml -p <project-name> down --volumes
```

Restore the previous Twilio voice webhook from the value the command printed
if you are done testing the Pipecat route.

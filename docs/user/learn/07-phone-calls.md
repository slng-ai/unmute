# 07. Phone calls

Unmute compiles phone-call intent for two orchestrators: **Pipecat** and
**LiveKit**. The Agent says what the call needs, a Connection names the secret
environment variables, and the target selects one exact media route. Unmute
does not buy a number, create a trunk, or copy credentials into generated code.

All carrier routes are currently **provisional**. The compiler and generated
Pipecat Twilio, Telnyx, and Plivo adapters have credential-free tests, but
validation continues to fail closed until each exact route passes real inbound,
outbound, authentication, hangup, and transfer smokes.

## Declare the phone channel

```yaml
# agent.yaml
channels:
  phone:
    kind: telephony
    inbound: true
    outbound: false
    required_controls: [cold_transfer, hangup]
```

`inbound` and `outbound` are required booleans. `required_controls` names only
behavior the Agent actually needs. Each direction and control is checked
against the exact `(orchestrator, transport, carrier)` route; support on
LiveKit SIP never enables the LiveKit Connector, and Twilio support never
enables another carrier.

## Add a Connection

Connection files contain environment-variable **names**, not their values:

```yaml
# connections/primary_phone.yaml
kind: telephony
environment:
  account_sid: TWILIO_ACCOUNT_SID
  auth_token: TWILIO_AUTH_TOKEN
  from_number: TWILIO_PHONE_NUMBER
```

Bind that Connection to a route in `targets.yaml`:

```yaml
targets:
  pipecat:
    provider: pipecat
    version: "1.5.0"
    transport: carrier-websocket
    carrier: twilio
    connection: primary_phone
    destinations:
      billing_line: "+14155550123"
```

Pipecat uses one WebSocket per carrier call and delegates media framing to the
selected Pipecat carrier serializer. The generated `telephony.py` owns signed
webhooks, one-use outbound context, normalized call metadata, and selected
carrier call control. It does not parse or emit audio frames. Twilio, Telnyx,
and Plivo use separate generated adapters because their signatures and call
control APIs differ; selecting one never emits another carrier's SDK or
credentials.

LiveKit uses either `transport: sip` or the distinct Beta
`transport: connector` route. The Connector is Twilio-only and cannot inherit
SIP transfer behavior. The SIP route uses this Connection vocabulary for
Twilio, Telnyx, and Plivo:

```yaml
# connections/primary_phone.yaml
kind: telephony
environment:
  sip_address: TWILIO_SIP_ADDRESS
  sip_username: TWILIO_SIP_USERNAME
  sip_password: TWILIO_SIP_PASSWORD
  from_number: TWILIO_PHONE_NUMBER
```

Use equivalent environment names for the selected carrier. Self-hosted
LiveKit SIP also needs Redis because the LiveKit server and SIP service use it
as a shared datastore and message bus. Pipecat also uses Redis, but only for
opaque pending-call correlation, callback idempotency, human-transfer locks,
and admission counters. Audio, transcripts, prompts, task state, and agent
handoff remain inside the active call worker.

## Configure credentials

Keep values in the package's ignored `.env` for development or in the
deployment secret store. Obtain them here:

| Route | Required values | Where to find them |
|---|---|---|
| Twilio | `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, `TWILIO_PHONE_NUMBER` | Twilio Console → Account dashboard and Phone Numbers. The Auth Token also validates webhook and WebSocket signatures. |
| Telnyx | `TELNYX_API_KEY`, `TELNYX_PUBLIC_KEY`, `TELNYX_CONNECTION_ID`, `TELNYX_PHONE_NUMBER` | Telnyx Mission Control → API Keys, Public Key, Voice API Application, and Numbers. |
| Plivo | `PLIVO_AUTH_ID`, `PLIVO_AUTH_TOKEN`, `PLIVO_PHONE_NUMBER` | Plivo Console → API Keys and Phone Numbers. |
| Exotel | `EXOTEL_API_KEY`, `EXOTEL_API_TOKEN`, `EXOTEL_ACCOUNT_SID`, `EXOTEL_SUBDOMAIN`, `EXOTEL_PHONE_NUMBER`, `EXOTEL_APP_ID` | Exotel Dashboard → API Settings, ExoPhones, and the Voice call-flow application. |
| LiveKit Cloud or Connector | `LIVEKIT_URL`, `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET` | LiveKit Cloud project settings, or `lk app env -w`. Add the selected carrier values above. |
| Self-hosted LiveKit SIP topology | `LIVEKIT_URL`, `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET`, `REDIS_URL`, `LIVEKIT_SIP_URI` | Create the key pair in the LiveKit Server `keys` configuration. Use the same server, key pair, and Redis deployment for LiveKit SIP. `LIVEKIT_SIP_URI` is the public DNS name or SIP URI of that deployment. |
| LiveKit SIP with Twilio | `TWILIO_SIP_ADDRESS`, `TWILIO_SIP_USERNAME`, `TWILIO_SIP_PASSWORD`, `TWILIO_PHONE_NUMBER` | Twilio Console → Elastic SIP Trunking. Use the termination URI, a Credential List username and password, and an associated number. |
| LiveKit SIP with Telnyx | `TELNYX_SIP_ADDRESS`, `TELNYX_SIP_USERNAME`, `TELNYX_SIP_PASSWORD`, `TELNYX_PHONE_NUMBER` | Telnyx Mission Control → SIP Trunking. Use the SIP connection address and credentials, and its assigned number. |
| LiveKit SIP with Plivo | `PLIVO_SIP_ADDRESS`, `PLIVO_SIP_USERNAME`, `PLIVO_SIP_PASSWORD`, `PLIVO_PHONE_NUMBER` | Plivo Console → Zentrunk. Use the termination domain, outbound credential, and linked number. |
| LiveKit SIP resource IDs | `LIVEKIT_SIP_INBOUND_TRUNK`, `LIVEKIT_SIP_OUTBOUND_TRUNK` | Copy the `SIPTrunkID` values printed by the generated `lk sip ... create` setup commands. Only the directions and controls you request are required. |

For local development, generated Compose supplies `REDIS_URL` on Pipecat and
the local `LIVEKIT_URL`, API key pair, and Redis connection on LiveKit SIP. Do
not copy the generated `devkey`/`devsecret-local-only` pair into deployment.
Production still needs the self-hosted values in the table. Carrier credentials,
model-provider keys, trunk IDs, and public ingress remain yours in both places.

Carrier WebSocket deployments also set `UNMUTE_PUBLIC_URL` to the exact public
HTTPS origin used in signature validation. It is configuration, not a secret.
Outbound HTTP starts require a separate secret, `UNMUTE_OUTBOUND_TOKEN`, which
you generate yourself. It is never a carrier credential.

The complete credential links and self-hosted topology are in
[TELEPHONY.md](../../../TELEPHONY.md#credentials).

For Telnyx, configure the Voice API Application for API version 2 and point its
webhook URL at the inbound endpoint printed by `unmute dev --telephony`. Assign
the phone number to that application. Telnyx signs HTTP events with the public
key; the generated WebSocket URL carries a short-lived, one-use opaque token.

For Plivo, create a Voice XML Application with its Answer URL set to the
reported inbound endpoint using POST, assign the number, and set the
Application Hangup URL to the reported status endpoint. Plivo V3 signs those
HTTP callbacks; the returned XML embeds a short-lived, one-use WebSocket token.

Exotel is not enabled yet. Its documented App Bazaar Voicebot flow uses a
static WebSocket URL, while its Pipecat guide warns that custom URL data may be
stripped. Until a carrier-authenticated upgrade or another replay-safe ingress
pattern is proven, Unmute rejects the route before generation. The Exotel
credentials in the table are the values a future outbound adapter needs, not a
claim that they make the current route usable. LiveKit SIP with Exotel is also
gated until an official provider setup and credentialed route smoke prove it.

## Configure self-hosted LiveKit SIP

The generated LiveKit project contains only the selected directions and
carrier. It emits `sip-inbound-trunk.json`, `sip-outbound-trunk.json`, and
`sip-dispatch-rule.json` as needed. These files contain environment
placeholders, not credentials.

For local development, run:

```sh
unmute dev ./agent --target livekit --telephony
```

This always builds and starts the generated Agent, Redis, LiveKit Server, and
LiveKit SIP with Docker Compose. It rejects non-empty `LIVEKIT_URL`,
`LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET`, or `REDIS_URL` from the host because
those values would conflict with the generated local topology. `ctrl-c` stops
only this package's Compose project and preserves its Redis data volume.

Complete real carrier setup in this order:

1. Deploy LiveKit Server and LiveKit SIP against the same Redis deployment.
2. Expose SIP signaling and RTP to the carrier. The defaults are SIP port
   `5060` and UDP RTP ports `10000-20000`.
3. Configure the carrier trunk to send inbound calls to `LIVEKIT_SIP_URI`.
4. Materialize the generated JSON files with `envsubst` and run the documented
   `lk sip inbound create`, `lk sip outbound create`, and
   `lk sip dispatch create` commands.
5. Copy the returned trunk IDs into the generated `.env.example` names.
6. Start the local stack with `unmute dev --telephony`, or deploy the generated
   Agent against the production topology.

`unmute dev --telephony` doesn't require `--public-url` for this route because
there are no carrier HTTP callbacks. It still requires the public SIP/RTP
deployment and the listed configuration. An HTTPS tunnel can't expose that
media topology.

## Run Pipecat telephony locally

Pipecat carrier WebSocket routes need Docker Compose and an externally visible
HTTPS origin:

```sh
unmute dev ./agent --target pipecat --telephony \
  --public-url https://agent-test.example-tunnel.dev
```

The command builds the same generated application used in deployment, starts
it with Redis, waits for both health checks, and prints the exact HTTP/WSS
carrier endpoints. `--bot-port` selects the host port; it does not change the
container's internal port. Add `--verbose` to follow Compose logs in the
terminal; otherwise they remain in `build/<target>/telephony.log`.

Docker does not provide public ingress. Keep your tunnel running for Pipecat.
For LiveKit SIP, expose SIP `5060` and UDP RTP `10000-20000` through networking
that the carrier can actually reach. A healthy local stack proves service
wiring, not a real call.

## Transfer to a person

Author a symbolic destination in the portable control:

```yaml
# agent.yaml
controls:
  to_human:
    kind: human_transfer
    destination: billing_line
    mode: cold
```

The target resolves `billing_line` to an E.164 number or SIP URI. The generated
runtime never accepts a model-supplied arbitrary transfer destination. Warm
transfer is a separate route feature and stays gated until that exact carrier
and transport pass their state-machine smoke. In particular, Twilio's
bidirectional Media Stream leg cannot also be a Conference participant, so the
Pipecat route must prove a separate conference media leg before it can claim a
warm transfer.

## Start outbound calls

An outbound channel must declare voicemail policy:

```yaml
channels:
  phone:
    kind: telephony
    inbound: false
    outbound: true
    on_voicemail: hangup

variables:
  campaign_id: { type: string, source: call_start }
  provider_call_id: { type: string, source: call_id }
```

The generated authenticated start operation requires every non-defaulted
`source: call_start` field. It returns an Unmute `session_id`, the carrier call
ID when available, and accepted status. Inbound calls can use a `call_start`
variable only when it has a default.

System sources are explicit: `session_id`, `carrier`, `connection`, `call_id`,
`stream_id`, `direction`, `from_number`, and `to_number`. A source that the
selected route cannot provide fails validation before generation.

Next: [08. Going live](08-going-live.md), on capacity, deployment, and secrets.

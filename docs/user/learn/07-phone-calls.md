# 07. Phone calls

Unmute compiles phone-call intent for two orchestrators: **Pipecat** and
**LiveKit**. The Agent says what the call needs, a Connection names the secret
environment variables, and the target selects one exact media route. Unmute
does not buy a number, create a trunk, or copy credentials into generated code.

All emitted carrier routes are currently **provisional**, and the remaining
recognized routes are gated. The generated Pipecat Twilio, Telnyx, and Plivo
adapters and the LiveKit SIP routes have credential-free tests, but validation
continues to fail closed until each exact route passes real inbound, outbound,
authentication, hangup, and transfer smokes.

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

## Choose a supported carrier route

Telephony support belongs to an exact framework, transport, and carrier tuple.
The Connection keys also belong to that tuple; a target never loads credentials
for an unselected carrier.

| Framework | Target route | Carrier | Required Connection keys | Status |
|---|---|---|---|---|
| Pipecat | `carrier-websocket` | Twilio | `account_sid`, `auth_token`, `from_number` | Generated offline; provisional |
| Pipecat | `carrier-websocket` | Telnyx | `api_key`, `public_key`, `connection_id`, `from_number` | Generated offline; provisional |
| Pipecat | `carrier-websocket` | Plivo | `auth_id`, `auth_token`, `from_number` | Generated offline; provisional |
| Pipecat | `carrier-websocket` | Exotel | `api_key`, `api_token`, `account_sid`, `subdomain`, `from_number`, `app_id` | Gated; no emitted adapter |
| LiveKit | `sip` | Twilio | `sip_address`, `sip_username`, `sip_password`, `from_number` | Generated offline; provisional |
| LiveKit | `sip` | Telnyx | `sip_address`, `sip_username`, `sip_password`, `from_number` | Generated offline; provisional |
| LiveKit | `sip` | Plivo | `sip_address`, `sip_username`, `sip_password`, `from_number` | Generated offline; provisional |
| LiveKit | `sip` | Exotel | `sip_address`, `sip_username`, `sip_password`, `from_number` | Gated; no emitted setup |
| LiveKit | `connector` | Twilio | `account_sid`, `auth_token`, `from_number` | Recognized Beta route; no emitted adapter |

"Generated offline" means the emitter and credential-free checks exist. It
does not promote the route: public validation, compilation, and telephony
development still fail closed until the exact credentialed smoke passes. The
Pipecat emitters contain inbound, outbound, hangup, and cold-transfer paths;
voicemail and warm transfer remain gated. The LiveKit SIP emitter contains
inbound, outbound, voicemail, hangup, cold-transfer, and warm-transfer paths.

## Configure multiple carriers

A package can declare any number of supported carrier routes. Give each route a
named target and bind it to one Connection. Each target produces a separate,
single-carrier project; Unmute never bundles several carrier SDKs or credential
sets into one runtime.

```yaml
# targets.yaml
targets:
  pipecat_twilio:
    provider: pipecat
    version: "1.5.0"
    transport: carrier-websocket
    carrier: twilio
    connection: twilio_api

  pipecat_telnyx:
    provider: pipecat
    version: "1.5.0"
    transport: carrier-websocket
    carrier: telnyx
    connection: telnyx_api

  livekit_twilio:
    provider: livekit
    version: "1.5.2"
    sdk_language: python
    transport: sip
    carrier: twilio
    connection: twilio_sip

  livekit_plivo:
    provider: livekit
    version: "1.5.2"
    sdk_language: python
    transport: sip
    carrier: plivo
    connection: plivo_sip
```

Create `connections/twilio_api.yaml`, `connections/telnyx_api.yaml`,
`connections/twilio_sip.yaml`, and `connections/plivo_sip.yaml` with the keys
from the matrix. The package can contain all of them, but every telephony
target currently fails closed because its exact route is provisional or gated.
After promotion, `unmute compile` will process every declared target when you
omit `--target`, while `unmute dev --telephony` will run one selected target at
a time. Adding another supported carrier is another target and Connection, not
a schema change.

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

Do not buy a number or export these values merely to get past the current CLI
gate. Validation and generation run first and report that the exact route has
not passed its credentialed smoke. The credentials become necessary only
after that route is promoted; the table records the names and sources so the
future setup is explicit.

After route promotion, generated Compose will supply `REDIS_URL` on Pipecat and
the local `LIVEKIT_URL`, API key pair, and Redis connection on LiveKit SIP. Do
not copy its `devkey`/`devsecret-local-only` pair into deployment. Production
will still need the self-hosted values in the table. Carrier credentials,
model-provider keys, trunk IDs, and public ingress remain yours in both places.

Carrier WebSocket deployments also set `UNMUTE_PUBLIC_URL` to the exact public
HTTPS origin used in signature validation. It is configuration, not a secret.
Outbound HTTP starts require a separate secret, `UNMUTE_OUTBOUND_TOKEN`, which
you generate yourself. It is never a carrier credential.

The complete credential links and self-hosted topology are in
[TELEPHONY.md](../../../TELEPHONY.md#credentials).

After the Telnyx route is promoted, configure the Voice API Application for API
version 2 and point its webhook URL at the inbound endpoint printed by `unmute
dev --telephony`. Assign the phone number to that application. Telnyx signs
HTTP events with the public key; the generated WebSocket URL carries a
short-lived, one-use opaque token.

After the Plivo route is promoted, create a Voice XML Application with its
Answer URL set to the reported inbound endpoint using POST, assign the number,
and set the Application Hangup URL to the reported status endpoint. Plivo V3
signs those HTTP callbacks; the returned XML embeds a short-lived, one-use
WebSocket token.

Exotel is not enabled yet. Its documented App Bazaar Voicebot flow uses a
static WebSocket URL, while its Pipecat guide warns that custom URL data may be
stripped. Until a carrier-authenticated upgrade or another replay-safe ingress
pattern is proven, Unmute rejects the route before generation. The Exotel
credentials in the table are the values a future outbound adapter needs, not a
claim that they make the current route usable. LiveKit SIP with Exotel is also
gated until an official provider setup and credentialed route smoke prove it.

## Configure self-hosted LiveKit SIP

The offline LiveKit emitter is designed to include only the selected
directions and carrier. Its tests render `sip-inbound-trunk.json`,
`sip-outbound-trunk.json`, and `sip-dispatch-rule.json` as needed, with
environment placeholders rather than credentials. No public compile can emit
those files yet because every LiveKit SIP route is provisional.

This is the intended command after an exact route is promoted:

```sh
unmute dev ./agent --target livekit --telephony
```

Today it fails during route validation, before trunk IDs, credentials, or
Docker are checked. After promotion it will build and start the generated
Agent, Redis, LiveKit Server, and LiveKit SIP with Docker Compose. It will
reject non-empty `LIVEKIT_URL`, `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET`, or
`REDIS_URL` from the host because those values conflict with the generated
local topology. `ctrl-c` will stop only this package's Compose project and
preserve its Redis data volume.

Complete real carrier setup in this order:

1. Deploy LiveKit Server and LiveKit SIP against the same Redis deployment.
2. Expose SIP signaling and RTP to the carrier. The defaults are SIP port
   `5060` and the local UDP RTP range `10000-10100`; size a production range
   for its expected traffic.
3. Configure the carrier trunk to send inbound calls to `LIVEKIT_SIP_URI`.
4. Materialize the generated JSON files with `envsubst` and run the documented
   `lk sip inbound create`, `lk sip outbound create`, and
   `lk sip dispatch create` commands.
5. Copy the returned trunk IDs into the generated `.env.example` names.
6. Start the local stack with `unmute dev --telephony`, or deploy the generated
   Agent against the production topology.

For the future all-local topology, trunk IDs must be created against the local
server before the full Agent can start. From the emitted project, first run:

```sh
docker compose -f compose.telephony.yaml up -d redis livekit_server livekit_sip
```

Point `lk` at that server with the generated local development key pair, create
the trunk and dispatch resources, export the returned trunk IDs, and then run
the full `unmute dev --telephony` command. This bootstrap is not available
today because the provisional route prevents the CLI from emitting the Compose
file.

After promotion, `unmute dev --telephony` will not require `--public-url` for
this route because there are no carrier HTTP callbacks. It will still require
the public SIP/RTP deployment and listed configuration. An HTTPS tunnel cannot
expose that media topology.

## Run Pipecat telephony locally

After promotion, Pipecat carrier WebSocket routes will need Docker Compose and
an externally visible HTTPS origin:

```sh
unmute dev ./agent --target pipecat --telephony \
  --public-url https://agent-test.example-tunnel.dev
```

Today the command reports the provisional route before checking
`--public-url`, carrier credentials, or Docker, and it emits no project. Once
the route is promoted, the command will build the same generated application
used in deployment, start it with Redis, wait for both health checks, and print
the exact HTTP/WSS carrier endpoints. `--bot-port` becomes
`UNMUTE_TELEPHONY_PORT` for Compose and selects the host port; it does not
change the container's internal port. Add `--verbose` to follow Compose logs in
the terminal; otherwise they remain in `build/<target>/telephony.log`.

Docker does not provide public ingress. Keep your tunnel running for Pipecat.
For LiveKit SIP, expose SIP `5060` and the configured UDP RTP range (local
default `10000-10100`) through networking that the carrier can actually reach.
A healthy local stack proves service wiring, not a real call.

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

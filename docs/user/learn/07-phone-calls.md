# 07. Phone calls

Unmute compiles phone-call intent for two orchestrators: **Pipecat** and
**LiveKit**. The Agent says what the call needs, a Connection names the secret
environment variables, and the target selects one exact media route. Unmute
never buys a number, never creates carrier-side trunks, and never copies
credentials into generated code. For local development,
`unmute dev --telephony` automates the rest: it manages a cloudflared tunnel,
points the Twilio number's voice webhook at it, and creates the local LiveKit
trunk records itself (see "Local development with zero steps" below).

The Pipecat Twilio, Telnyx, and Plivo routes and the LiveKit SIP routes have
real adapters and are usable now. They are tagged **provisional**: the adapter
exists but has not passed its automated credentialed smoke yet, so
`unmute validate`, `unmute compile`, and `unmute dev --telephony` run them and
print a per-feature warning that the route is unverified. The remaining
recognized routes (Exotel on any framework, and the LiveKit Twilio Connector)
are gated with no adapter, so they still fail closed.

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

The three direction shapes:

```yaml
# Inbound only: the agent answers calls.
channels:
  phone:
    kind: telephony
    inbound: true
    outbound: false
    required_controls: [cold_transfer, hangup]
```

```yaml
# Outbound only: the agent places calls and must declare voicemail policy.
channels:
  phone:
    kind: telephony
    inbound: false
    outbound: true
    on_voicemail: hangup
```

```yaml
# Both directions: answer and place calls with one channel.
channels:
  phone:
    kind: telephony
    inbound: true
    outbound: true
    on_voicemail: leave_message
    required_controls: [cold_transfer, hangup]
```

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

Each carrier route has its own key vocabulary. The Pipecat equivalents for
Telnyx and Plivo:

```yaml
# connections/telnyx_api.yaml
kind: telephony
environment:
  api_key: TELNYX_API_KEY
  public_key: TELNYX_PUBLIC_KEY
  connection_id: TELNYX_CONNECTION_ID
  from_number: TELNYX_PHONE_NUMBER
```

```yaml
# connections/plivo_api.yaml
kind: telephony
environment:
  auth_id: PLIVO_AUTH_ID
  auth_token: PLIVO_AUTH_TOKEN
  from_number: PLIVO_PHONE_NUMBER
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

Use equivalent environment names for the selected carrier
(`TELNYX_SIP_ADDRESS`, `TELNYX_SIP_USERNAME`, `TELNYX_SIP_PASSWORD`,
`TELNYX_PHONE_NUMBER` for Telnyx; `PLIVO_SIP_ADDRESS`, `PLIVO_SIP_USERNAME`,
`PLIVO_SIP_PASSWORD`, `PLIVO_PHONE_NUMBER` for Plivo). Self-hosted
LiveKit SIP also needs Redis because the LiveKit server and SIP service use it
as a shared datastore and message bus. Pipecat also uses Redis, but only for
opaque pending-call correlation, callback idempotency, human-transfer locks,
and admission counters. The generated Compose graphs ship Valkey
(BSD-3-Clause) as that store, so the whole local stack stays open source;
it speaks the Redis protocol and the service keeps the Redis name. Audio, transcripts, prompts, task state, and agent
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

"Generated offline" means the emitter and credential-free checks exist. The
route is usable now: public validation, compilation, and telephony development
run it and print a warning that the route is unverified until its exact
credentialed smoke passes. The Pipecat emitters contain inbound, outbound,
hangup, and cold-transfer paths; voicemail and warm transfer remain gated. The
LiveKit SIP emitter contains inbound, outbound, voicemail, hangup,
cold-transfer, and warm-transfer paths.

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

  pipecat_plivo:
    provider: pipecat
    version: "1.5.0"
    transport: carrier-websocket
    carrier: plivo
    connection: plivo_api

  livekit_twilio:
    provider: livekit
    version: "1.5.2"
    sdk_language: python
    transport: sip
    carrier: twilio
    connection: twilio_sip

  livekit_telnyx:
    provider: livekit
    version: "1.5.2"
    sdk_language: python
    transport: sip
    carrier: telnyx
    connection: telnyx_sip

  livekit_plivo:
    provider: livekit
    version: "1.5.2"
    sdk_language: python
    transport: sip
    carrier: plivo
    connection: plivo_sip
```

Create `connections/twilio_api.yaml`, `connections/telnyx_api.yaml`,
`connections/plivo_api.yaml`, `connections/twilio_sip.yaml`,
`connections/telnyx_sip.yaml`, and `connections/plivo_sip.yaml` with the keys
from the matrix. The package can contain all of them. `unmute compile` processes every declared
target when you omit `--target`, while `unmute dev --telephony` runs one
selected target at a time. Provisional routes run with an unverified warning;
only the gated no-adapter routes (Exotel, the LiveKit Twilio Connector) fail.
Adding another supported carrier is another target and Connection, not a schema
change.

## Local development with zero steps

Local development needs one command and no manual setup per run:

```sh
unmute dev ./agent --target pipecat --telephony
```

You set credentials in `.env` once (see the `.env` examples below and the
one-time carrier setup for each model). For a hands-on version of this
section with the exact Twilio Console clicks, follow the
[Twilio walkthrough](twilio-walkthrough.md). The command then does the rest,
in this order:

1. Validates and generates the selected target (gated routes still fail
   closed here).
2. Prints the resolved runtime plan and checks your credentials. Values the
   command supplies itself are never demanded from you.
3. Checks Docker Compose.
4. Starts a managed tunnel for carrier webhook routes, or takes your
   `--public-url`.
5. Prints the exact public webhook, WebSocket, outbound, and status URLs.
6. Starts the generated Compose graph. For LiveKit SIP it starts Redis,
   LiveKit Server, and LiveKit SIP first, creates or reuses the inbound
   trunk, outbound trunk, and `call-` dispatch rule on your local server,
   and injects the returned IDs before the application starts.
7. Configures the carrier voice webhook where the carrier supports it
   (Twilio today), printing the previous webhook URL so you can restore it.
8. Prints `call +1XXXXXXXXXX, ctrl-c to stop` and streams logs.
9. On ctrl-c, stops only this package's Compose project, keeps its data
   volumes, and kills the tunnel.

### The managed tunnel (carrier webhook routes)

Pipecat carrier-websocket routes need a public HTTPS/WSS origin. Without
`--public-url`, the dev command runs a
[cloudflared quick tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/do-more-with-tunnels/trycloudflare/)
as a child process and uses its `https://<random>.trycloudflare.com` origin
as `UNMUTE_PUBLIC_URL`. cloudflared must be on PATH:

```sh
brew install cloudflared
```

On Linux, install the distribution package or a binary from the
cloudflare/cloudflared releases page. Quick tunnels need no Cloudflare
account. Their URL changes on every run, which is why the Twilio webhook is
reconfigured on every start. To use ngrok or any other tunnel instead, pass
`--public-url https://your-tunnel.example` and the command manages no tunnel
at all.

### What gets configured on Twilio automatically

For the Pipecat Twilio route, after the app is healthy the command looks up
your `TWILIO_PHONE_NUMBER`, sets its voice webhook to the printed inbound
endpoint, and prints the previous webhook value:

```text
phone: Twilio voice webhook for +15550001111 set to https://<random>.trycloudflare.com/telephony/inbound (was: https://old.example/hook)
```

It never buys numbers and never creates carrier trunks. Telnyx and Plivo
keep printed manual steps for now.

### What gets created on your local LiveKit stack automatically

For the LiveKit SIP route, the command creates these records against your
local LiveKit server with the generated development key pair:

- an inbound trunk for your number,
- an outbound trunk (address and auth from your `*_SIP_*` values; the
  password goes only into the API request, never into files or logs),
- an individual-room dispatch rule (`call-` room prefix) that dispatches the
  generated agent.

Creation is idempotent. The local Redis volume keeps records across
restarts, so a second run reuses them instead of duplicating. The returned
IDs are injected as `LIVEKIT_SIP_INBOUND_TRUNK` and
`LIVEKIT_SIP_OUTBOUND_TRUNK`; never set these two for local runs.

### One-time carrier setup per model

Pipecat with Twilio (Programmable Voice):

1. Buy or pick a Voice-capable number in the Twilio Console.
2. Copy the Account SID and Auth Token from the account dashboard.
3. Put `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, and
   `TWILIO_PHONE_NUMBER` in `.env`.

```dotenv
# .env for pipecat + carrier-websocket + twilio
TWILIO_ACCOUNT_SID=
TWILIO_AUTH_TOKEN=
TWILIO_PHONE_NUMBER=
# Only for outbound channels; generate it yourself:
UNMUTE_OUTBOUND_TOKEN=
# Supplied by the command itself: UNMUTE_PUBLIC_URL, REDIS_URL.
```

LiveKit with Twilio (Elastic SIP Trunking):

1. Create an Elastic SIP Trunk in the Twilio Console.
2. Copy the termination URI (`something.pstn.twilio.com`).
3. Create a Credential List and attach it to the trunk.
4. Set the trunk's origination URI to your reachable SIP endpoint.
5. Attach the phone number to the trunk.
6. Put the four values in `.env`.

```dotenv
# .env for livekit + sip + twilio
TWILIO_SIP_ADDRESS=
TWILIO_SIP_USERNAME=
TWILIO_SIP_PASSWORD=
TWILIO_PHONE_NUMBER=
# Supplied by the command itself: REDIS_URL, LIVEKIT_URL, LIVEKIT_API_KEY,
# LIVEKIT_API_SECRET, LIVEKIT_SIP_INBOUND_TRUNK, LIVEKIT_SIP_OUTBOUND_TRUNK.
```

The Telnyx and Plivo `.env` files use the same shapes with their own names
(`TELNYX_*`, `PLIVO_*`; the Pipecat routes use the API vocabulary from the
route matrix instead of the `*_SIP_*` names).

### The honest limits

- LiveKit SIP needs carrier-reachable SIP signaling and RTP. An HTTPS
  tunnel cannot carry it, and Docker Desktop NAT may block it even when
  every local health check passes. The managed tunnel applies only to
  carrier webhook routes.
- Provisional routes run now with an unverified warning. Only the gated
  no-adapter routes (Exotel, the LiveKit Twilio Connector) fail closed,
  because there is nothing to run for them.

## Configure telephony by orchestrator

Choose the orchestrator before you configure the carrier. In Unmute,
**LiveKit** uses SIP trunks, while **Pipecat** connects directly to the
carrier's HTTP and WebSocket APIs. The same carrier therefore needs different
credentials and a different `transport` on each orchestrator.

| Orchestrator | Carrier | `transport` | Do you configure a SIP trunk? | Status |
|---|---|---|---|---|
| LiveKit | Twilio, Telnyx, or Plivo | `sip` | Yes | Offline-tested; provisional |
| LiveKit | Exotel | `sip` | Not yet | Gated; no emitted setup |
| LiveKit | Twilio | `connector` | No | Gated; no emitted adapter |
| Pipecat | Twilio, Telnyx, or Plivo | `carrier-websocket` | No | Offline-tested; provisional |
| Pipecat | Exotel | `carrier-websocket` | No | Gated; no emitted adapter |

<!-- prettier-ignore -->
> [!IMPORTANT]
> The provisional routes in this table run now. Validation, compilation, and
> `unmute dev --telephony` generate and run them, and print a warning that the
> route is unverified until it passes a credentialed call smoke. Only the gated
> Exotel and Twilio Connector rows fail, because there is no adapter to run.

### Configure a Pipecat carrier WebSocket

Pipecat does not use your carrier's SIP trunk. Create a direct carrier
Connection, select `transport: carrier-websocket`, and expose the generated
HTTP and WebSocket endpoints over HTTPS.

| Carrier | Connection keys | Put these values in `.env` |
|---|---|---|
| Twilio | `account_sid`, `auth_token`, `from_number` | `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, `TWILIO_PHONE_NUMBER` |
| Telnyx | `api_key`, `public_key`, `connection_id`, `from_number` | `TELNYX_API_KEY`, `TELNYX_PUBLIC_KEY`, `TELNYX_CONNECTION_ID`, `TELNYX_PHONE_NUMBER` |
| Plivo | `auth_id`, `auth_token`, `from_number` | `PLIVO_AUTH_ID`, `PLIVO_AUTH_TOKEN`, `PLIVO_PHONE_NUMBER` |
| Exotel | `api_key`, `api_token`, `account_sid`, `subdomain`, `from_number`, `app_id` | Gated; these values do not enable the route |

Run it locally with one command; the managed tunnel supplies the public origin:

```sh
unmute dev ./agent --target pipecat_twilio --telephony
```

Pass `--public-url https://your-tunnel.example` instead to bring your own
tunnel. In deployment, set `UNMUTE_PUBLIC_URL` to the exact public HTTPS
origin yourself. If the channel is outbound, generate a separate
`UNMUTE_OUTBOUND_TOKEN`; it is application auth, not a carrier credential.

Carrier webhook setup by carrier:

- For Twilio, `unmute dev --telephony` sets the number's voice webhook
  automatically on every start and prints the previous value. In
  deployment, set the voice webhook and call-status callback to the printed
  endpoints yourself.
- For Telnyx, use a version 2 Voice API Application, set its webhook URL to
  the printed inbound endpoint, and assign the phone number to it.
- For Plivo, create a Voice XML Application, set its Answer and Hangup URLs
  to the printed endpoints, and assign the phone number to it.
- For Exotel, wait for an authenticated WebSocket route. Its static Voicebot
  URL does not satisfy Unmute's ingress policy.

### Configure self-hosted LiveKit SIP

LiveKit is the only Unmute orchestrator that uses SIP trunks. The target
chooses the carrier, the Connection maps four route keys to environment
variable names, and the generated JSON creates the matching LiveKit inbound
trunk, outbound trunk, and dispatch rule.

#### Understand the two sides of the trunk

LiveKit and the carrier use different identifiers. Keep these values separate
when you fill in `.env`.

| Value | Owned by | Meaning |
|---|---|---|
| `LIVEKIT_SIP_URI` | Your LiveKit SIP deployment | Public SIP endpoint where the carrier sends inbound calls |
| `*_SIP_ADDRESS` | Carrier | Carrier termination address that LiveKit calls for outbound calls |
| `*_SIP_USERNAME`, `*_SIP_PASSWORD` | Carrier | Credentials that LiveKit uses for outbound SIP authentication |
| `*_PHONE_NUMBER` | Carrier | E.164 number associated with the carrier trunk |
| `LIVEKIT_SIP_INBOUND_TRUNK` | LiveKit | ID returned by `lk sip inbound create` |
| `LIVEKIT_SIP_OUTBOUND_TRUNK` | LiveKit | ID returned by `lk sip outbound create` |

The Connection stores only the environment variable names. Use the same four
keys for Twilio, Telnyx, and Plivo:

```yaml
# connections/twilio_sip.yaml
kind: telephony
environment:
  sip_address: TWILIO_SIP_ADDRESS
  sip_username: TWILIO_SIP_USERNAME
  sip_password: TWILIO_SIP_PASSWORD
  from_number: TWILIO_PHONE_NUMBER
```

Bind the Connection to one exact LiveKit route:

```yaml
# targets.yaml
targets:
  livekit_twilio:
    provider: livekit
    version: "1.5.2"
    sdk_language: python
    transport: sip
    carrier: twilio
    connection: twilio_sip
```

Change both `carrier` and the Connection for another carrier. Do not reuse a
Pipecat API Connection: `account_sid`, `api_key`, and `auth_id` are not valid
keys on a LiveKit SIP route.

#### Configure the LiveKit SIP deployment

Production needs a self-hosted LiveKit Server and LiveKit SIP deployment that
share one Redis instance. Configure these values in `.env` or your deployment
secret store:

```dotenv
LIVEKIT_URL=wss://livekit.example.com
LIVEKIT_API_KEY=replace-with-server-key
LIVEKIT_API_SECRET=replace-with-server-secret
REDIS_URL=redis://redis.example.com:6379
LIVEKIT_SIP_URI=sip.example.com
```

Expose SIP signaling and RTP directly to the carrier. Generated local Compose
defaults to SIP port `5060` and UDP RTP ports `10000-10100`; production needs a
range sized for its call traffic. An HTTPS tunnel cannot expose this media
path.

#### Configure Twilio SIP

Twilio uses an Elastic SIP Trunk, a termination URI, and a Credential List.
Use the
[LiveKit Twilio SIP guide](https://docs.livekit.io/telephony/start/providers/twilio/)
for the carrier-side fields.

1. Create an Elastic SIP Trunk in the Twilio Console.
2. For inbound calls, set its origination URI to
   `sip:<LIVEKIT_SIP_URI>;transport=tcp`.
3. For outbound calls, create a Credential List and attach it to the trunk.
4. Associate the Twilio phone number with the trunk.
5. Put the termination SIP URI, Credential List username and password, and
   phone number in `.env`:

```dotenv
TWILIO_SIP_ADDRESS=your-trunk.pstn.twilio.com
TWILIO_SIP_USERNAME=replace-with-credential-list-user
TWILIO_SIP_PASSWORD=replace-with-credential-list-password
TWILIO_PHONE_NUMBER=+14155550123
```

#### Configure Telnyx SIP

Telnyx uses an FQDN SIP Connection, outbound credentials, and an outbound voice
profile. Use the
[LiveKit Telnyx SIP guide](https://docs.livekit.io/telephony/start/providers/telnyx/)
for the carrier-side fields.

1. Create an FQDN SIP Connection in the Telnyx Portal.
2. Add `LIVEKIT_SIP_URI` as the connection's FQDN for inbound calls.
3. Set the origination and destination number formats to `+E.164`.
4. For outbound calls, set a username and password and select an outbound
   voice profile.
5. Assign the Telnyx phone number to the SIP Connection.
6. Put the carrier SIP address, username and password, and phone number in
   `.env`:

```dotenv
TELNYX_SIP_ADDRESS=sip.telnyx.com
TELNYX_SIP_USERNAME=replace-with-sip-user
TELNYX_SIP_PASSWORD=replace-with-sip-password
TELNYX_PHONE_NUMBER=+14155550123
```

Telnyx also requires its SIP username on the first outbound `INVITE`. The
route remains provisional while Unmute verifies that provider-specific trunk
input and the complete call flow.

#### Configure Plivo SIP

Plivo calls SIP trunking **Zentrunk** and uses separate inbound and outbound
trunks. Use the
[LiveKit Plivo SIP guide](https://docs.livekit.io/telephony/start/providers/plivo/)
for the carrier-side fields.

1. Create an inbound Zentrunk whose primary URI is
   `<LIVEKIT_SIP_URI>;transport=tcp`, and link the Plivo phone number.
2. Create an outbound credential with a username and strong password.
3. Create an outbound Zentrunk with that credential.
4. Copy its termination SIP domain from the `trunk_domain` field.
5. Put the termination domain, outbound username and password, and phone
   number in `.env`:

```dotenv
PLIVO_SIP_ADDRESS=12345678901234.zt.plivo.com
PLIVO_SIP_USERNAME=replace-with-zentrunk-user
PLIVO_SIP_PASSWORD=replace-with-zentrunk-password
PLIVO_PHONE_NUMBER=+14155550123
```

#### Handle Exotel and the LiveKit Twilio Connector

Neither route has a runnable setup. LiveKit SIP with Exotel is gated until an
official provider setup and credentialed smoke prove the route. The Beta
LiveKit Twilio Connector is a separate `transport: connector` route; Unmute
recognizes its credential vocabulary but emits no adapter and never inherits
SIP capabilities.

#### Create the LiveKit resources

Compile the target and materialize the generated JSON. The committed inputs
contain environment variable placeholders; `envsubst` resolves them from your
exported environment.

```sh
envsubst < sip-inbound-trunk.json > /tmp/unmute-sip-inbound-trunk.json
lk sip inbound create /tmp/unmute-sip-inbound-trunk.json
# Set LIVEKIT_SIP_INBOUND_TRUNK to the returned SIPTrunkID.

envsubst < sip-dispatch-rule.json > /tmp/unmute-sip-dispatch-rule.json
lk sip dispatch create /tmp/unmute-sip-dispatch-rule.json

envsubst < sip-outbound-trunk.json > /tmp/unmute-sip-outbound-trunk.json
lk sip outbound create /tmp/unmute-sip-outbound-trunk.json \
  --auth-user "$SIP_USERNAME" \
  --auth-pass "$SIP_PASSWORD"
# Set LIVEKIT_SIP_OUTBOUND_TRUNK to the returned SIPTrunkID.
```

Replace `SIP_USERNAME` and `SIP_PASSWORD` with the selected carrier's variable
names. Only create the resources required by the channel directions and
controls. The generated README contains the exact commands for that target.
These manual commands are the production path.

For local development, none of that is needed:

```sh
unmute dev ./agent --target livekit_twilio --telephony
```

The command starts Redis, LiveKit Server, and LiveKit SIP, creates or reuses
the trunks and dispatch rule on that local server itself, injects
`LIVEKIT_SIP_INBOUND_TRUNK` and `LIVEKIT_SIP_OUTBOUND_TRUNK`, and then starts
the Agent. It rejects external `LIVEKIT_URL`, `LIVEKIT_API_KEY`,
`LIVEKIT_API_SECRET`, `REDIS_URL`, and user-set trunk IDs because they
conflict with the local topology. `ctrl-c` stops this package's Compose
project and preserves its Redis data volume, so the created records survive
restarts and are reused, not duplicated.

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

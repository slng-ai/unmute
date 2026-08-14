# 07. Phone calls

Unmute compiles phone-call intent for two orchestrators: **Pipecat** and
**LiveKit**. The Agent says what the call needs, a Connection names the secret
environment variables, and the target selects one exact media route. Unmute
never buys a number, never creates carrier-side trunks, and never copies
credentials into generated code. For local development,
`unmute dev --telephony` automates the rest: it manages a cloudflared tunnel,
points the Twilio number's voice webhook at it, and creates the local LiveKit
trunk records itself (see "Local development with zero steps" below).

The Pipecat Twilio, Telnyx, and Plivo routes, the LiveKit Twilio connector, and
the LiveKit SIP routes have real adapters and are usable now. They are tagged
**provisional**, but that is internal maturity tracking recorded in
`compile-report.json`, not a runtime warning: `unmute validate`,
`unmute compile`, and `unmute dev --telephony` run them cleanly, with no warning.
The remaining recognized routes (Exotel on any framework) are gated with no
adapter, so they still fail closed.

## Declare the phone channel

```yaml
# agent.yaml
channels:
  phone:
    kind: telephony
    inbound: true
    outbound: false
    required_controls:
      - hangup
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
    required_controls:
      - hangup
```

```yaml
# Outbound only: the agent places calls. on_voicemail is optional; set it when
# you want a policy for reaching an answering machine, and only on a route that
# can detect one.
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
    required_controls:
      - hangup
```

## Add a connection

**A target names one connection. The connection is the whole route.** The
connection file says which mechanism carries the call, which carrier hands it
over, and which environment variables hold that account's credentials. The
values are variable **names**, never the secrets themselves:

```yaml
# connections/primary_phone.yaml
transport: carrier-websocket
carrier: twilio
environment:
  account_sid: TWILIO_ACCOUNT_SID
  auth_token: TWILIO_AUTH_TOKEN
  from_number: TWILIO_PHONE_NUMBER
```

Each carrier route has its own key vocabulary. The Pipecat equivalents for
Telnyx and Plivo:

```yaml
# connections/telnyx_api.yaml
transport: carrier-websocket
carrier: telnyx
environment:
  api_key: TELNYX_API_KEY
  public_key: TELNYX_PUBLIC_KEY
  connection_id: TELNYX_CONNECTION_ID
  from_number: TELNYX_PHONE_NUMBER
```

```yaml
# connections/plivo_api.yaml
transport: carrier-websocket
carrier: plivo
environment:
  auth_id: PLIVO_AUTH_ID
  auth_token: PLIVO_AUTH_TOKEN
  from_number: PLIVO_PHONE_NUMBER
```

The target names it and says nothing else about telephony:

```yaml
targets:
  pipecat:
    provider: pipecat
    version: "1.5.0"
    connection: primary_phone
```

Two targets that ride different mechanisms need one connection file each, even
when they share a carrier account: a connection declares its own transport, so
one file cannot serve both. `examples/outbound-reminder` is that shape, with
`twilio_websocket.yaml` and `twilio_connector.yaml` holding the same three
names.

There is no `kind:` line. Every transport in the catalog is telephony, so it
said nothing the first line does not.

Full field-by-field reference:
[reference/connections.md](../reference/connections.md).

## Say who the agent escalates to

A transfer dials a person. Which person is a package-wide fact, so it lives at
the top level of `agent.yaml` rather than on a target: the same desk answers
whichever carrier reaches it.

```yaml
# agent.yaml
destinations:
  billing_line: BILLING_PHONE_NUMBER
  supervisor_line: SUPERVISOR_PHONE_NUMBER
```

A value is the `UPPER_SNAKE` name of an environment variable holding the
number, read at call time. A literal number or `sip:` URI is refused:
`agent.yaml` is the portable half of a package, and a number is a deployment
fact. The model never sees a number and can never dial an arbitrary one.

## Declare every name you set

`secrets:` lists every environment variable **you** supply: model keys, the
values your connections name, and the numbers your destinations point at.

```yaml
# agent.yaml
secrets:
  - OPENAI_API_KEY
  - SLNG_API_KEY
  - TWILIO_ACCOUNT_SID
  - TWILIO_AUTH_TOKEN
  - TWILIO_PHONE_NUMBER
  - BILLING_PHONE_NUMBER
```

A name missing from this list is a warning on stderr, not an error, and the
build still succeeds. That is the same rule every environment name has always
had. The cost is real and worth knowing: a package missing a name compiles
green and fails on its first phone call.

Names the runtime supplies for you are not listed there, because you do not set
them. Where each value comes from:

| Value | Who supplies it |
|---|---|
| Model and tool keys, connection credentials, destination numbers | you, through `secrets:` |
| `REDIS_URL`, `UNMUTE_PUBLIC_URL`, `UNMUTE_OUTBOUND_TOKEN` | `unmute dev` locally; the operator at deploy time on routes that read them |
| `LIVEKIT_URL`, `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET` | the local Compose graph, or your LiveKit Cloud project |
| `DAILY_API_KEY`, `PIPECAT_CLOUD_ORGANIZATION` | the route's own runtime environment |

`DAILY_API_KEY` is the one that surprises people. The Daily route reads it at
runtime and it never belongs in `secrets:`, because the route's environment
supplies it rather than you.

## Test in a browser before you test on a phone

Telephony is opt-in at run time. `unmute dev` opens a browser session by
default, on any package, including one that declares only `channels.phone`:

```sh
unmute dev ./agent --target pipecat
```

No carrier credentials are needed and none are checked. Talk to the agent,
get the prompt and the models right, and only then add `--telephony`, which is
where the route variables start mattering. This is the shortest loop the
project has, and it is the one to stay in while the agent is still changing.

Pipecat uses one WebSocket per carrier call and delegates media framing to the
selected Pipecat carrier serializer. The generated `telephony.py` owns signed
webhooks, one-use outbound context, normalized call metadata, and selected
carrier call control. It does not parse or emit audio frames. Twilio, Telnyx,
and Plivo use separate generated adapters because their signatures and call
control APIs differ; selecting one never emits another carrier's SDK or
credentials.

LiveKit uses either `transport: sip` or the distinct `transport: connector`
route. The connector is Twilio-only and carries no transfers; transfers live
on the SIP route ([TRANSFERS.md](../../TRANSFERS.md)).
The SIP route uses this Connection vocabulary for Twilio, Telnyx, and Plivo:

```yaml
# connections/primary_phone.yaml
transport: sip
carrier: twilio
environment:
  sip_address: SIP_TRUNK_HOSTNAME
  sip_username: SIP_AUTH_USERNAME
  sip_password: SIP_AUTH_PASSWORD
  from_number: SIP_FROM_NUMBER
```

The same four names serve Twilio, Telnyx and Plivo: they are standard SIP
trunk settings, not one carrier's, and the deployed agent dials out with them
directly. They are yours to choose, because the compiler carries whatever a
Connection declares through verbatim, so a package already using
`TWILIO_SIP_ADDRESS` and friends keeps working unchanged. Self-hosted
LiveKit SIP also needs Redis because the LiveKit server and SIP service use it
as a shared datastore and message bus. Pipecat also uses Redis, but only for
opaque pending-call correlation, callback idempotency, and admission
counters. The generated Compose graphs ship Valkey
(BSD-3-Clause) as that store, so the whole local stack stays open source;
it speaks the Redis protocol and the service keeps the Redis name. Audio, transcripts, prompts, task state, and agent
handoff remain inside the active call worker.

## Choose a supported carrier route

Telephony support belongs to an exact framework, transport, and carrier tuple.
The Connection keys also belong to that tuple; a target never loads credentials
for an unselected carrier.

| Framework | Target route | Carrier | Required Connection keys | Status |
|---|---|---|---|---|
| Pipecat | `cloud-websocket` | Twilio | `account_sid`, `auth_token`, `from_number`, and **none at all** on a receive-only package | Generated offline; provisional |
| Pipecat | `carrier-websocket` | Twilio | `account_sid`, `auth_token`, `from_number` | Generated offline; provisional |
| Pipecat | `carrier-websocket` | Telnyx | `api_key`, `public_key`, `connection_id`, `from_number` | Generated offline; provisional |
| Pipecat | `carrier-websocket` | Plivo | `auth_id`, `auth_token`, `from_number` | Generated offline; provisional |
| Pipecat | `carrier-websocket` | Exotel | `api_key`, `api_token`, `account_sid`, `subdomain`, `from_number`, `app_id` | Gated; no emitted adapter |
| LiveKit | `sip` | Twilio | `sip_address`, `sip_username`, `sip_password`, `from_number` | Generated offline; provisional |
| LiveKit | `sip` | Telnyx | `sip_address`, `sip_username`, `sip_password`, `from_number` | Generated offline; provisional |
| LiveKit | `sip` | Plivo | `sip_address`, `sip_username`, `sip_password`, `from_number` | Generated offline; provisional |
| LiveKit | `sip` | Exotel | `sip_address`, `sip_username`, `sip_password`, `from_number` | Gated; no emitted setup |
| LiveKit | `connector` | Twilio | `account_sid`, `auth_token`, `from_number` | Generated offline; provisional |

"Generated offline" means the emitter and credential-free checks exist. The
route is usable now: public validation, compilation, and telephony development
run it cleanly, with no warning. The Pipecat carrier emitters contain inbound,
outbound, and hangup paths; they carry no transfers, and neither does the
LiveKit connector, because transfers compile only where the platform ships
the primitive ([TRANSFERS.md](../../TRANSFERS.md)). The LiveKit SIP emitter
contains inbound, outbound, voicemail, hangup, cold-transfer, and
warm-transfer paths, all on LiveKit's native machinery. Voicemail detection
stays on the LiveKit SIP route.

### Why the same carrier asks for different credentials

Look down the Twilio rows above and you see two different credential sets. That
is not our naming: Twilio sells two products here, and each route uses one of
them.

| | Programmable Voice + Media Streams | Elastic SIP Trunking |
|---|---|---|
| What you get | REST API, TwiML, audio over a WebSocket | A SIP trunk: termination URI, credential list, origination URI |
| Credentials | `account_sid`, `auth_token`, a voice number | `sip_address`, `sip_username`, `sip_password`, a number on the trunk |
| Setup | Buy a voice-capable number | Create the trunk, point its origination URI at your SIP server |
| Network | HTTPS and WSS only | SIP signalling plus RTP media, on public ports |
| Used by | Pipecat `carrier-websocket`, LiveKit `connector` | LiveKit `sip` |

Each orchestrator asks for the product its telephony stack speaks. Pipecat's
telephony transport is a WebSocket that talks the Media Streams protocol, so it
needs the first. LiveKit's telephony is LiveKit SIP, a server that terminates
SIP from a carrier and bridges it into a room, so it needs the second.

The practical consequence is the network row. HTTPS and WSS go through the
managed tunnel, so the two Media-Streams routes run on a laptop. SIP and RTP do
not tunnel, so the LiveKit SIP route needs a deployed, publicly reachable SIP
endpoint even for a first call. That, and not the credentials themselves, is
why the same carrier feels so different depending on the route.

Telnyx and Plivo split the same way: their own API products feed the Pipecat
routes, their own SIP trunking feeds LiveKit.

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
    connection: twilio_api

  pipecat_telnyx:
    provider: pipecat
    version: "1.5.0"
    connection: telnyx_api

  pipecat_plivo:
    provider: pipecat
    version: "1.5.0"
    connection: plivo_api

  livekit_twilio:
    provider: livekit
    version: "1.5.2"
    sdk_language: python
    connection: twilio_sip

  livekit_telnyx:
    provider: livekit
    version: "1.5.2"
    sdk_language: python
    connection: telnyx_sip

  livekit_plivo:
    provider: livekit
    version: "1.5.2"
    sdk_language: python
    connection: plivo_sip
```

Create `connections/twilio_api.yaml`, `connections/telnyx_api.yaml`,
`connections/plivo_api.yaml`, `connections/twilio_sip.yaml`,
`connections/telnyx_sip.yaml`, and `connections/plivo_sip.yaml` with the keys
from the matrix. The package can contain all of them. `unmute compile` processes every declared
target when you omit `--target`, while `unmute dev --telephony` runs one
selected target at a time. Provisional routes run cleanly, with no warning;
only the gated no-adapter routes (Exotel) fail. Adding another supported carrier
is another target and Connection, not a schema change.

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

On both Twilio Media Streams routes, the Pipecat carrier WebSocket and the
LiveKit connector, the command looks up your `TWILIO_PHONE_NUMBER` once the app
is healthy, sets its voice webhook to the printed inbound endpoint, and prints
the previous value:

```text
phone: Twilio voice webhook for +15550001111 set to https://<random>.trycloudflare.com/telephony/inbound (was: https://old.example/hook)
```

It never buys numbers and never creates carrier trunks. Telnyx and Plivo
keep printed manual steps for now.

**Your number is borrowed, not taken.** When you stop the run, the old value
goes back:

```text
phone: Twilio voice webhook for +15550001111 restored to https://old.example/hook
```

This matters more than it sounds. The tunnel hostname is random per run and
dies with the process, so a run that rewrote your webhook and walked away
would leave a real phone line pointing at a URL that no longer resolves. The
next person to call would hear "an application error has occurred", and
nothing in your own logs would show it, because the call never reaches you.
The restore runs on every exit path, `ctrl-c` included.

**To keep our hands off the number entirely**, pass `--no-webhook`. Nothing is
written to Twilio and the public URL is printed for you to configure yourself:

```sh
unmute dev examples/twilio-telephony-hello --target pipecat --telephony --no-webhook
```

Use it for a number that is shared with someone else, serving production
traffic, or managed by something other than this CLI.

**If you are past experimenting**, stop the rewriting instead of relying on it.
Give `cloudflared` a named tunnel with a fixed hostname, set the webhook to
that hostname once in the Twilio console, and use `--no-webhook` from then on.
The automatic rewrite exists only because quick tunnel hostnames rotate.

One wrinkle worth knowing: restoring means putting back whatever was there. If
a previous run left a dead tunnel URL on the number, the next run will read
that as the old value and faithfully restore it. Clear it once by hand, then
every run afterwards hands back something real.

### What gets created on your local LiveKit stack automatically

For the LiveKit SIP route, the command creates these records against your
local LiveKit server with the generated development key pair:

- an inbound trunk for your number,
- an individual-room dispatch rule (`call-` room prefix) that dispatches the
  generated agent.

Creation is idempotent. The local Redis volume keeps records across
restarts, so a second run reuses them instead of duplicating. Nothing is
injected into the application's environment: the records are platform state the
local LiveKit SIP service reads for itself, and no environment name carries their
IDs (SCHEMA N36, 2026-08-12). No outbound trunk is created, locally or in a
deployment: since 2026-08-12 (SCHEMA N33) the agent dials out with the carrier's
trunk settings passed inline, so local and deployed use the same mechanism.

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
SIP_TRUNK_HOSTNAME=
SIP_AUTH_USERNAME=
SIP_AUTH_PASSWORD=
SIP_FROM_NUMBER=
# Supplied by the command itself: REDIS_URL, LIVEKIT_URL, LIVEKIT_API_KEY,
# LIVEKIT_API_SECRET.
```

The Telnyx and Plivo `.env` files use the same shapes with their own names
(`TELNYX_*`, `PLIVO_*`; the Pipecat routes use the API vocabulary from the
route matrix instead of the `*_SIP_*` names).

### The honest limits

- LiveKit SIP needs carrier-reachable SIP signaling and RTP. An HTTPS
  tunnel cannot carry it, and Docker Desktop NAT may block it even when
  every local health check passes. The managed tunnel applies only to
  carrier webhook routes.
- That limit is about **inbound**, and the two directions are not the same
  problem. Inbound cannot work locally: the carrier opens the connection, so it
  needs a public `sip:` URI to send the INVITE to and an address in your SDP it
  can send RTP to, and a laptop behind NAT has neither. **Outbound** is your
  side opening the connection, and Twilio replies to the address the RTP
  actually came from rather than the one advertised in the SDP, so an outbound
  call may well work from a laptop. The generated
  Compose already publishes 5060 and the RTP range, which is what it would
  need. We have not run it, so this is a plausible-but-untested note rather
  than a supported path: if you try it, the thing to watch is whether Docker
  Desktop's NAT rewrites the RTP source port. Use the connector route if you
  want a laptop-testable LiveKit path today.
- Provisional routes run now, cleanly and with no warning. Only the gated
  no-adapter routes (Exotel) fail closed, because there is nothing to run for
  them.

## Configure telephony by orchestrator

Choose the orchestrator before you configure the carrier. In Unmute,
**LiveKit** uses SIP trunks, while **Pipecat** connects directly to the
carrier's HTTP and WebSocket APIs. The same carrier therefore needs different
credentials and a different `transport` on each orchestrator.

| Orchestrator | Carrier | `transport` | Do you configure a SIP trunk? | Status |
|---|---|---|---|---|
| LiveKit | Twilio, Telnyx, or Plivo | `sip` | Yes | Offline-tested; provisional |
| LiveKit | Exotel | `sip` | Not yet | Gated; no emitted setup |
| LiveKit | Twilio | `connector` | No | Offline-tested; provisional |
| Pipecat | Twilio | `cloud-websocket` | No | Offline-tested; provisional |
| Pipecat | Twilio, Telnyx, or Plivo | `carrier-websocket` | No | Offline-tested; provisional |
| Pipecat | Exotel | `carrier-websocket` | No | Gated; no emitted adapter |
| Pipecat | none, Daily owns the number | `daily-sip` | No | Offline-tested; provisional |
| Pipecat | Twilio | `daily-sip` | Yes, termination only | Offline-tested; provisional |

<!-- prettier-ignore -->
> [!IMPORTANT]
> The provisional routes in this table run now. Validation, compilation, and
> `unmute dev --telephony` generate and run them cleanly, with no warning. Only
> the gated Exotel rows fail, because there is no adapter to run.

### Which Pipecat route, if your carrier is Twilio

Three routes reach a Pipecat target from a Twilio number, and one question
separates them: **what do you want to be running?**

- **Nothing.** `transport: cloud-websocket`. Pipecat Cloud terminates the call's
  audio itself; your number points at a small piece of static markup in the Twilio
  console, and the generated README dictates it in four steps. This is the
  recommendation for the common case. Cold transfer works; a **failed** transfer
  brings back a fresh agent that does not remember the call.
- **A small webhook server, so a failed transfer keeps the same agent.**
  `transport: daily-sip` with `carrier: twilio`. The build emits
  `telephony_helper.py` and you host it wherever calls should land.
- **The whole application, on your own infrastructure.**
  `transport: carrier-websocket`. Also the only Pipecat route that binds
  `source.*` call-source variables.

The full comparison, including what each needs from the carrier account, is in
[TELEPHONY.md](../../TELEPHONY.md).

### One region, declared once

On `transport: cloud-websocket`, `deployment_region` on the target is the only
place a region is written, and three things are rendered from it:

```yaml
targets:
  pipecat:
    provider: pipecat
    version: "1.5.0"
    transport: cloud-websocket
    carrier: twilio
    deployment_region: eu-central     # the only place a region appears
```

| What it sets | Where you see it |
|---|---|
| where the agent deploys | `region` in the generated `pcc-deploy.toml` |
| where its secrets live | the `--region` on the generated `secrets set` command |
| where the carrier streams to | the `wss://eu-central.api.pipecat.daily.co/...` host in the call markup the generated README dictates |

The platform requires all three to agree: a regional stream endpoint routes
**only** to agents deployed in that region, and an agent can only read a secret
set from its own region. Declaring no region is also fine, and then all three use
the platform's defaults.

To move region, change that one line, recompile, and re-paste the address into
your carrier's markup. Two platform rules bite when the agent already exists:
agent names are globally unique **across** regions, and a secret set is
region-scoped with a globally unique name, so plan on either deleting the old pair
or naming the new one differently. Region codes are forwarded exactly as written
and never checked by the compiler, so a typo fails the platform's own deploy
command rather than compiling into something unreachable.

The shipped examples are one per use case:

| Example | Use case | Targets |
|---|---|---|
| `examples/twilio-telephony-hello` | inbound and outbound, nothing else | Pipecat (`cloud-websocket`) and LiveKit (`sip`), the route each platform recommends for Twilio |
| `examples/pipecat-human-transfer-twilio` | cold transfer and inbound, hosting nothing | Pipecat (`cloud-websocket`) |
| `examples/livekit-human-transfer` | warm transfer and inbound | LiveKit (`sip`) |
| `examples/pipecat-human-transfer-daily` | cold transfer on a Daily-provisioned number | Pipecat (`daily-sip`, no carrier) |

The transfer examples name their provider first because putting a caller through
to a person is not one feature with two implementations. LiveKit does it over a SIP
trunk, with a native primitive for cold **and** warm; Pipecat does it over Twilio
Media Streams, cold only. Those packages are not interchangeable, so the name says
which platform you are reading about. `twilio-telephony-hello` is named after the
**carrier** instead, because it carries one target per provider: the point there is
comparing how the same carrier reaches each platform, over Media Streams on one and
over a SIP trunk on the other.

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
  automatically on every start, prints the previous value, and restores it on
  exit; `--no-webhook` skips the change entirely. In deployment, set the voice
  webhook and call-status callback to the printed endpoints yourself.
- For Telnyx, use a version 2 Voice API Application, set its webhook URL to
  the printed inbound endpoint, and assign the phone number to it.
- For Plivo, create a Voice XML Application, set its Answer and Hangup URLs
  to the printed endpoints, and assign the phone number to it.
- For Exotel, wait for an authenticated WebSocket route. Its static Voicebot
  URL does not satisfy Unmute's ingress policy.

### Configure the Pipecat Daily route

Two forms, and the difference is whose number it is.

**Daily's number.** Set `transport: daily-sip` and nothing else: no `carrier`, no
`connection`, and no phone channel. Buy the number from Daily, point it at your
deployed agent from the Pipecat Cloud dashboard, and you are done. Nothing of
yours is in the call path.

**Your own number.** Set all three of `carrier`, `connection`, and a
`channels.phone` entry. They are required together on this transport; leaving one
out fails naming it (SCHEMA N37).

| Carrier | Connection keys | Put these values in `.env` |
|---|---|---|
| Twilio | `account_sid`, `auth_token`, `sip_address`, `from_number` | `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, `SIP_TRUNK_HOSTNAME`, `SIP_FROM_NUMBER` |

Your carrier forwards the call over SIP into the same Daily room the other form
uses, so the agent is identical either way. Two things about this that surprise
people:

- **There is no SIP username or password here**, and the route rejects both by
  name. Daily's outbound SIP carries no credential on any documented surface, so
  your trunk allows Daily by IP address list instead. The generated README dictates
  that step and names the list's URL.
- **The build emits `telephony_helper.py`, and you run it.** Daily makes one room
  per call, and its SIP addresses are per room, so your carrier has no static
  address to forward to. The helper answers your carrier, asks the platform to
  start your agent on a fresh room with a SIP address, and keeps the caller
  hearing something meanwhile. The agent hands the call over itself.
  Locally, run it and put a tunnel in front of it; the generated README's
  "Telephony setup" section is two copy-paste commands plus four actions in your
  carrier's console.

`unmute dev --telephony` refuses on both forms, and its message says which one you
are on and what to do instead. Browser and `--console` work as always.

If you already set up the LiveKit SIP route with the same carrier, you reuse that
account, that trunk, and that number: `SIP_TRUNK_HOSTNAME` and `SIP_FROM_NUMBER`
carry over unchanged, your two credential lines go unused, and moving the number
between the two targets is one change at the carrier in either direction.

### Configure LiveKit SIP

LiveKit is the only Unmute orchestrator that uses SIP trunks. The target chooses
the carrier, the Connection maps four route keys to environment variable names,
and the build carries the two JSON inputs an incoming call needs plus the
`telephony-setup.sh` that creates them. This works the same on LiveKit Cloud and
on a self-hosted LiveKit; only the origination target differs, and the generated
README says which is which.

#### Understand the two directions

Carriers name the two directions from their own point of view, which is the single
biggest source of confusion here:

- **Termination** is calls leaving the carrier towards the phone network. That is
  your agent dialling out: an outbound call, or the second leg of a warm transfer.
  It needs an address and credentials, which are three of your four values.
- **Origination** is calls that started on the phone network and have to be handed
  onward to your infrastructure. That is an incoming call. It needs one thing: the
  address of your LiveKit SIP endpoint.

Two consequences worth knowing before you debug anything. **A number must be
attached to the trunk**, or incoming calls never enter it and the origination URI
is never consulted, however correct it is. And **on LiveKit Cloud the origination
address comes from the project ID, not from `LIVEKIT_URL`**: the project URL
subdomain and the SIP subdomain are unrelated strings, so the obvious guess gives
an address that rings nowhere. The generated README prints yours with one command.

#### Understand the two sides of the trunk

LiveKit and the carrier use different identifiers. Keep these values separate
when you fill in `.env`.

| Value | Owned by | Meaning |
|---|---|---|
| `SIP_TRUNK_HOSTNAME` | Carrier | Carrier termination host the INVITE is sent to. Not a URI: no `sip:` prefix |
| `SIP_AUTH_USERNAME`, `SIP_AUTH_PASSWORD` | Carrier | Credentials LiveKit authenticates the outbound call with |
| `SIP_FROM_NUMBER` | Carrier | E.164 number on the trunk, and the number calls are placed from |

The Connection stores only the environment variable names. Use the same four
keys for Twilio, Telnyx, and Plivo:

```yaml
# connections/twilio_sip.yaml
kind: telephony
environment:
  sip_address: SIP_TRUNK_HOSTNAME
  sip_username: SIP_AUTH_USERNAME
  sip_password: SIP_AUTH_PASSWORD
  from_number: SIP_FROM_NUMBER
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
```

Note the public SIP endpoint you deploy LiveKit SIP on (for example
`sip.example.com`); the carrier's origination URI points at it. Nothing reads
it as an environment variable. Expose SIP signaling and RTP directly to the
carrier. Generated local Compose
defaults to SIP port `5060` and UDP RTP ports `10000-10100`; production needs a
range sized for its call traffic. An HTTPS tunnel cannot expose this media
path.

#### Configure Twilio SIP

Twilio uses an Elastic SIP Trunk, a termination URI, and a Credential List.
Use the
[LiveKit Twilio SIP guide](https://docs.livekit.io/telephony/start/providers/twilio/)
for the carrier-side fields.

1. Create an Elastic SIP Trunk in the Twilio Console.
2. For incoming calls, set its origination URI to your LiveKit SIP endpoint with
   `;transport=tcp` on the end. On LiveKit Cloud that is
   `sip:<project ID without the p_ prefix>.sip.livekit.cloud`; self-hosted, it is
   the public SIP signalling address you deployed.
3. For outgoing calls, create a Credential List and attach it to the trunk. Its
   username and password are two of your four values, and the trunk's own domain,
   ending in `pstn.twilio.com`, is the third. That domain is one value, not two:
   there is no separate termination address to hunt for.
4. **Attach your phone number to the trunk.** Without this the other three steps
   have no effect on incoming calls.
5. For a **cold** transfer, enable Call Transfer (SIP REFER) and tick Enable PSTN
   Transfer on the trunk. A cold transfer is a SIP REFER on the caller's existing
   leg, so the carrier is the only thing that can allow or refuse it: there is no
   LiveKit-side setting that compensates, and a trunk left at `disable-all` or
   `sip-only` fails every cold transfer to a phone number.

The generated `build/livekit/README.md` has all of this as console paths **and** as
a runnable command block for the Twilio CLI, with your own variable names filled
in, plus three checks that print the states this side fails in.
4. Associate the Twilio phone number with the trunk.
5. Put the termination SIP URI, Credential List username and password, and
   phone number in `.env`:

```dotenv
SIP_TRUNK_HOSTNAME=your-trunk.pstn.twilio.com
SIP_AUTH_USERNAME=replace-with-credential-list-user
SIP_AUTH_PASSWORD=replace-with-credential-list-password
SIP_FROM_NUMBER=+14155550123
```

#### Configure Telnyx SIP

Telnyx uses an FQDN SIP Connection, outbound credentials, and an outbound voice
profile. Use the
[LiveKit Telnyx SIP guide](https://docs.livekit.io/telephony/start/providers/telnyx/)
for the carrier-side fields.

1. Create an FQDN SIP Connection in the Telnyx Portal.
2. Add your public LiveKit SIP endpoint as the connection's FQDN for inbound
   calls.
3. Set the origination and destination number formats to `+E.164`.
4. For outbound calls, set a username and password and select an outbound
   voice profile.
5. Assign the Telnyx phone number to the SIP Connection.
6. Put the carrier SIP address, username and password, and phone number in
   `.env`:

```dotenv
SIP_TRUNK_HOSTNAME=sip.telnyx.com
SIP_AUTH_USERNAME=replace-with-sip-user
SIP_AUTH_PASSWORD=replace-with-sip-password
SIP_FROM_NUMBER=+14155550123
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
   `<your-livekit-sip-endpoint>;transport=tcp`, and link the Plivo phone number.
2. Create an outbound credential with a username and strong password.
3. Create an outbound Zentrunk with that credential.
4. Copy its termination SIP domain from the `trunk_domain` field.
5. Put the termination domain, outbound username and password, and phone
   number in `.env`:

```dotenv
SIP_TRUNK_HOSTNAME=12345678901234.zt.plivo.com
SIP_AUTH_USERNAME=replace-with-zentrunk-user
SIP_AUTH_PASSWORD=replace-with-zentrunk-password
SIP_FROM_NUMBER=+14155550123
```

#### Handle Exotel

LiveKit SIP with Exotel is gated until an official provider setup and
credentialed smoke prove the route. Unmute recognizes its credential vocabulary
but emits no setup for it, so it fails closed.

#### Use the LiveKit Twilio connector

The `transport: connector` route is the easy, laptop-testable LiveKit option. It
is Twilio-only and uses the same three Twilio credentials as the Pipecat carrier
route: `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, and `TWILIO_PHONE_NUMBER`. No
SIP trunk and no Redis. The generated `telephony_bridge.py` speaks the Twilio
Media Streams protocol and bridges the call into a local, self-hosted LiveKit
room, so it needs no LiveKit Cloud. Local Compose runs the application container
and a local `livekit-server --dev`.

Run it like the Pipecat route. `unmute dev --telephony` starts a managed
cloudflared tunnel, sets the Twilio voice webhook automatically, and places an
outbound call with `--to`:

```sh
unmute dev ./agent --target livekit --telephony --to +15551234567
```

Twilio reaches the bridge over HTTPS and WSS, so both inbound and outbound
work fully on a laptop. The connector supports inbound, outbound, and hangup.
It carries no transfers: LiveKit's transfer machinery acts on a SIP
participant reached through a trunk, and this route has neither — the caller
is audio the bridge published into a room. Transfers and voicemail detection
live on the LiveKit SIP route ([TRANSFERS.md](../../TRANSFERS.md)).

#### Create the LiveKit resources

Compile the target, then run the emitted script from the build directory:

```sh
bash telephony-setup.sh
```

It reads your phone number, finds the inbound trunk that claims it, creates the
trunk and the dispatch rule when they do not exist yet, and reuses them when they
do. Everything is found by the number, so no record ID is copied between commands
and no environment name holds one (SCHEMA N36, 2026-08-12). It needs `lk`,
authenticated against the project, and `jq`.

Two things it does on purpose. It **never sources your `.env`**, because that would
read every secret in the file and would abort on a single line whose name is not a
shell identifier; it reads the one phone-number assignment as text instead. And it
**refuses to create the dispatch rule while the trunk ID is empty**, because a rule
with an empty trunk list matches every trunk in the project, which in a shared
project would capture other people's calls.

Inbound only, and both records are needed. An unsolicited call arrives with no
request of yours for configuration to travel with, so the platform has to
already know which project owns the number (the inbound trunk) and which room
and agent the caller joins (the dispatch rule).

**There is no outbound trunk to create.** Since 2026-08-12 (SCHEMA N33) the
generated agent dials out by passing the carrier's own trunk settings inline
with each call, from the four `SIP_*` names above, so `lk sip outbound create`
and `LIVEKIT_SIP_OUTBOUND_TRUNK` are gone. Dialling out is your own code
starting a call, so the settings can ride along with it; nothing has to be
registered first.

The generated README's `## Telephony setup` section is the authority for this
package: it dictates the carrier steps too, in the order they have to happen, and
ends with the sequence that takes a package live: carrier, then these records, then
deploy the agent, then call your own number and read `lk agent logs`.

One failure worth knowing before it costs you an evening. Every name in the `.env`
you upload as secrets must be a valid shell identifier: letters, digits and
underscores, never starting with a digit. LiveKit Cloud exports your secrets with a
shell, so a name like `11LABS_API_KEY` fails at export, that value is missing at
runtime, and the agent dies later somewhere unrelated. The only trace is one line
at the very top of `lk agent logs`:

```text
/etc/run/env: line 2: export: `11LABS_API_KEY=...': not a valid identifier
```

Rename or delete it, then re-upload with
`lk agent update-secrets --secrets-file .env --overwrite`. A merge leaves the bad
name in place.

For local development, none of that is needed:

```sh
unmute dev ./agent --target livekit_twilio --telephony
```

The command starts Redis, LiveKit Server, and LiveKit SIP, creates or reuses
the inbound trunk and dispatch rule on that local server itself, and then starts
the Agent. It creates no outbound trunk, because the agent dials out inline,
exactly as a deployment does. It rejects external `LIVEKIT_URL`,
`LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET`, and `REDIS_URL` because they conflict
with the local topology. `ctrl-c` stops this package's Compose
project and preserves its Redis data volume, so the created records survive
restarts and are reused, not duplicated.

## Transfer to a person

Author a symbolic destination in the portable control:

```yaml
# agent.yaml
controls:
  to_human:
    kind: human_transfer
    cold:
      destination: billing_line
```

The shape is a block, and the block holds the settings: `cold:` hands the caller
off and drops out. Swap it for a `warm:` block to keep the agent on the line and
brief the person first. See
[controls](../reference/controls.md#kind-human_transfer) for what goes inside
each block.

**A `warm:` transfer needs `outbound: true` on the phone channel.** Warm holds
the caller and then rings the person, and ringing someone is placing a call. So
a package with a warm transfer places calls, whatever the channel says, and
declaring `outbound: false` next to one is rejected:

```text
channel "phone" needs outbound: true; a warm transfer places a call to its destination
```

Cold is unaffected. It reroutes the call the caller already made and originates
nothing, so it works on an inbound-only agent.

The target resolves `billing_line` to an E.164 number or SIP URI. The
generated runtime never accepts a model-supplied arbitrary transfer
destination. Testing a transfer is a cloud exercise (SIP does not fit a
tunnel); the walkthrough, including a warm test that needs no phone number at
all, is in [TRANSFERS.md](../../TRANSFERS.md).

### Which routes can transfer

One rule: a transfer compiles only on a route where the platform ships the
primitive. Everywhere else, validation refuses and names the routes that
work. The map with sources lives in [TRANSFERS.md](../../TRANSFERS.md).

| Route | `cold:` | `warm:` |
|---|---|---|
| LiveKit `sip` + Twilio, Telnyx, or Plivo | yes | yes |
| Pipecat Daily (`transport: daily-sip`), Daily's number | yes | not yet |
| Pipecat Daily (`transport: daily-sip`), your own number | yes | not yet |
| Pipecat `cloud-websocket` + Twilio | yes | no, by trade |
| Pipecat `carrier-websocket` (any carrier) | no | no |
| LiveKit `connector` + Twilio | no | no |

The three answers in the `warm:` column mean three different things (checked
2026-08-13). **"no"** means the platform has no transfer control on that
transport at all, so there is nothing to build against. **"not yet"** means it
does and we have not built it: Daily documents a warm pattern, but it puts the
generated bot in charge of the call's audio, so it is deliberate work rather than
a default. Tracked as feature 005. **"no, by trade"** means it is buyable and the
route declines to buy it: a warm handoff has to act on how the destination's leg
ended, which on `cloud-websocket` needs a callback endpoint you host, and hosting
nothing is that route's whole reason to exist. The refusal message says so.

Either way, a Pipecat warm package fails validation today and points you at
`(livekit, sip)`, where LiveKit's `WarmTransferTask` prebuilt does the job.

### What actually happens on the call

On **LiveKit + SIP**, cold is a SIP REFER, so the carrier moves the caller's
leg and the agent drops out. That needs Call Transfer (SIP REFER) enabled on
the Twilio trunk with PSTN transfer ticked, or the carrier rejects it. Warm
is `WarmTransferTask`: the caller holds with music while the task dials the
person on the outbound trunk, briefs them away from the caller, moves them
into the caller's room and shuts the agent's session down, so the two of
them carry on alone.

On **Pipecat + Daily**, cold is `sip_call_transfer`: the bot announces the
handoff, Daily reroutes the caller's leg, and the bot drops off once the
person answers. A failed transfer comes back as a result the tool reads, and
`on_unavailable` decides whether the agent keeps helping or says goodbye.

On **Pipecat + `cloud-websocket`**, cold is neither of those. There is no SIP leg
to refer and no room to reroute, so the agent instead sends one request to your
carrier that **replaces the live call's instructions**: speak a line, dial the
destination, then speak a second line and hand the caller back to a new session.
The bot's part of the call ends when the markup is replaced. That second line
plays **whenever the dial ends**, and that includes the person hanging up after a
perfectly good conversation, not only a dial that never connected. A static
document cannot tell the two apart, so the line says only what is true of both
and never apologises for a failure that may not have happened. That is also why
this route cannot keep the session across a transfer and cannot do warm at all:
it has no leg left to hold and nothing tells it how the other side's leg ended.

Three mechanisms, then, not one feature: refer the existing leg, reroute the room,
or replace the instructions. Which one your route has is what decides whether cold
keeps the session and whether warm exists at all.

Every transfer route stays provisional until its recipe in
[TRANSFERS.md](../../TRANSFERS.md) has been run as written. See
[controls](../reference/controls.md#kind-human_transfer) for the fields,
[examples/livekit-human-transfer](https://github.com/slng-ai/unmute_cli/tree/main/examples/livekit-human-transfer)
for both shapes on LiveKit SIP, and
[examples/pipecat-human-transfer-daily](https://github.com/slng-ai/unmute_cli/tree/main/examples/pipecat-human-transfer-daily)
for cold on Pipecat over Daily.

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
  campaign_id:
    type: string
    source: call_start
  provider_call_id:
    type: string
    source: call_id
```

The generated authenticated start operation requires every non-defaulted
`source: call_start` field. It returns an Unmute `session_id`, the carrier call
ID when available, and accepted status. Inbound calls can use a `call_start`
variable only when it has a default.

System sources are explicit: `session_id`, `carrier`, `connection`, `call_id`,
`stream_id`, `direction`, `from_number`, and `to_number`. A source that the
selected route cannot provide fails validation before generation.

Next: [08. Going live](08-going-live.md), on capacity, deployment, and secrets.

# Telephony

A phone call reaches the agent over a route: a target, a transport, and a
carrier, together. Pick the route from all three, never from a brand name.

## "Put it on Twilio" is not a route

Twilio reaches both targets, three different ways, and they are not
interchangeable. Before writing anything, get three answers:

1. **Which target**, Pipecat or LiveKit?
2. **Which transport**, which is the mechanism that carries the call?
3. **Which carrier**?

If the user only said "Twilio", ask which of the routes below they want, or pick
one and say plainly which you picked and what it cannot do.

## The routes

| Target | Transport | Carrier | How the call arrives |
|---|---|---|---|
| Pipecat | `cloud-websocket` | Twilio | Pipecat Cloud terminates the carrier's media stream itself. Nothing of yours is hosted |
| Pipecat | `carrier-websocket` | Twilio, Telnyx, Plivo | a generated adapter of yours terminates the carrier's media stream |
| Pipecat | `carrier-websocket` | Exotel | no adapter, so this route is refused at validation |
| Pipecat | `daily-sip` | Twilio | your carrier forwards into the same Daily room through a helper you run |
| LiveKit Agents | `sip` | Twilio, Telnyx, Plivo | a SIP trunk carries the call into LiveKit SIP |
| LiveKit Agents | `sip` | Exotel | no adapter, so this route is refused at validation |
| LiveKit Agents | `connector` | Twilio | a generated bridge turns Twilio Media Streams into a LiveKit room |

The emitted Pipecat Daily helper is a public ingress, not an open start API. Set
the exact public HTTPS base URL named by its generated runbook. The helper
validates Twilio's signature over the complete `/call` form before using the
Pipecat Cloud key; a missing or invalid signature returns 403 and starts no
session.

The compile report marks each phone route and prints the vendor document and
the date it was last checked. Read that report before describing the route.

`carrier-websocket` is the one Pipecat route with no managed-platform path. That
build emits no `pcc-deploy.toml`, its `Dockerfile` starts from plain Python, and
its README says **Deploy it yourself**, not `pipecat cloud deploy`. Tell the user
they need public HTTPS and WSS on port 443, the public origin the generated
runbook names, WebSocket timeouts longer than the longest call, and one Redis
shared by every replica. Every other Pipecat route deploys to Pipecat Cloud.

## What the transport decides

The transport is not a detail. It decides what the agent can do on a call.

- **SIP** hands over a call leg with its own signalling, so the leg can be
  moved. That is why cold transfer, warm transfer, and voicemail detection live
  on the LiveKit `sip` route.
- **A media stream over a websocket** hands over audio frames. Call control
  happens over the carrier's REST API instead, so a transfer is either a
  different mechanism or not possible at all.

So if the user wants a warm transfer, the transport decision is already made for
them. See `transfers.md` before choosing a route, not after.

## Writing it

Three pieces, in three files.

### The channel

```yaml agent.yaml
channels:
  web:
    kind: realtime_audio
  phone:
    kind: telephony
    inbound: true
    outbound: true
```

`inbound` and `outbound` are separate, because most routes support them
differently. Set only what the user actually wants.

**A `telephony` channel makes `capacity.peak_starts_per_second` required.** Add
it in the same edit, or validation fails on a field the user never mentioned:

```
pipecat: capacity.peak_starts_per_second must be positive for telephony
```

```yaml agent.yaml
capacity:
  peak_sessions: 5
  max_sessions: 10
  peak_starts_per_second: 1
  avg_session_duration: 5m
```

### The connection

One file is the whole route: the mechanism, the carrier, and the account
settings as environment variable **names**, never values.

```yaml connections/twilio_sip.yaml
transport: sip
carrier: twilio
environment:
  sip_address: SIP_TRUNK_HOSTNAME
  sip_username: SIP_AUTH_USERNAME
  sip_password: SIP_AUTH_PASSWORD
  from_number: SIP_FROM_NUMBER
```

The setting names on the left are fixed by the route. The names on the right are
yours and go in `secrets:`. The compiler never reads a value, so a package with
connections validates and compiles with no credentials present anywhere.

Two shapes exist:

| Shape | When | Looks like |
|---|---|---|
| full route | most connections | `transport`, `carrier`, and an `environment` map |
| no credentials | receive only on Pipecat `cloud-websocket` | `transport` and `carrier`, nothing else |

The receive-only shape has an edge worth knowing: the moment the package places
a call or hands one to a person, that same route needs `account_sid`,
`auth_token`, and `from_number`, because both of those speak to Twilio's API in
your name. The refusal says which behaviour asked for them.

### The environment keys, per route

A key from another route is refused, and the refusal carries the accepted set.

| Target | `transport` | `carrier` | `environment` keys |
|---|---|---|---|
| Pipecat | `cloud-websocket` | `twilio` | `account_sid`, `auth_token`, `from_number`, and only when the package places or redirects a call |
| Pipecat | `carrier-websocket` | `twilio` | `account_sid`, `auth_token`, `from_number` |
| Pipecat | `carrier-websocket` | `telnyx` | `api_key`, `public_key`, `connection_id`, `from_number` |
| Pipecat | `carrier-websocket` | `plivo` | `auth_id`, `auth_token`, `from_number` |
| Pipecat | `daily-sip` | `twilio` | `account_sid`, `auth_token`, `sip_address`, `from_number` |
| LiveKit Agents | `sip` | `twilio`, `telnyx`, `plivo` | `sip_address`, `sip_username`, `sip_password`, `from_number` |
| LiveKit Agents | `connector` | `twilio` | `account_sid`, `auth_token`, `from_number` |

The SIP route uses standard SIP names rather than one vendor's, because the same
generated code dials through any SIP carrier with them.

### The target

```yaml targets.yaml
targets:
  livekit:
    provider: livekit
    version: "1.6.10"
    sdk_language: python
    connection: twilio_sip
```

A target names at most one connection, and a connection declares one transport.
Two carriers, or two mechanisms, means two targets with a connection file each,
and each compiles to its own `build/<target>/`.

Transport, carrier, and destinations are refused on a target and the refusal
names the new home.

## Testing over a real phone, locally

```sh
unmute dev examples/twilio-telephony-hello --telephony --target pipecat
```

This runs the route the connection declares on the user's own machine, with **no
carrier account and nothing leaving the machine**. It compiles, prints the
resolved route and the plane it chose, starts the local runtime, and then does
one of two things depending on the plane:

- On the `sip` plane it prints an address and a per-run credential and waits for
  the user to dial from a softphone.
- On the `media-websocket` plane there is nothing to dial: the CLI **is** the
  carrier, so it places the call itself and connects the machine's microphone to
  it. Without `sox` on the PATH it plays a recorded fixture and says which.

The runtime is Compose on every route except Pipecat `cloud-websocket`, which
runs the generated `bot.py` under `uv` on the host.

Every route is exercised on a **local plane**, which is a route fact rather than
a choice. The plane presents the agent the same call mechanism the route's
carrier uses in production, so a route never gets a more convenient one:

| Plane | Which routes | What it is |
|---|---|---|
| `sip` | LiveKit `sip`, Pipecat `sip` | a real SIP trunk in containers, with endpoints for the caller and for each declared transfer destination |
| `media-websocket` | LiveKit `connector`, Pipecat `carrier-websocket`, Pipecat `cloud-websocket` | the CLI speaks the carrier's media-streaming protocol to the agent over loopback |
| `none` | Pipecat `daily-sip` | no carrier-free loop exists, and the command keeps refusing rather than pretending |

**Pipecat `sip` is the self-hosted route**, and the only one with no managed
platform anywhere in it. It runs the same four containers locally and in
production (the agent, LiveKit Server, LiveKit SIP, and a coordination store),
emits no deployment manifest, and builds on a plain Python image. Its trade is
warm transfer, which this project has not built on Pipecat. Reach for it when the
user says they want to host everything themselves; reach for `cloud-websocket`
when they want to host nothing.

**Tell the user this one before they deploy it themselves.** LiveKit SIP answers
an inbound call only once something joins the caller's room and publishes audio.
On LiveKit `sip` the dispatch rule puts the agent in the room, because that agent
is a LiveKit agent worker. On Pipecat `sip` it cannot: a dispatch rule dispatches
only workers, and this agent is a Pipecat bot. So the route emits a
`telephony.py` that answers a LiveKit room webhook at `/telephony/livekit` and
starts a session, and the dispatch rule it creates names no agent at all. A local
run configures the server's `webhook` block for the user. A deployment of their
own has to, and the emitted `README.md` carries the block to paste. Without it
every inbound call rings for three minutes and is cut off, with nothing in any
log that looks like an error.

Things worth telling the user in advance:

- **A default run touches no carrier and opens no tunnel.** This changed: it
  used to do both. An author who wants a real call passes `--carrier`.
- **`--carrier` requires `--telephony`** and places one real call through the
  user's own carrier, including the webhook rewrite and its restore. So do
  `--public-url` and `--no-webhook`, which exist only to manage a public origin
  and now name `--carrier` in their refusals.
- **The delay heard under `--carrier` is not the product's delay.** It is the
  public network reaching the user's machine. The run says so itself.
- **The SIP plane's code is the same locally and deployed.** Unlike the WebSocket
  routes there is no local-only transport branch, because the plane *is* the
  stack: the only difference is where `LIVEKIT_URL` points. Nothing on a `sip`
  route reads the plane selector, and a package that did would contradict the
  route's own argument.
- **On the SIP plane the run prints a dial address and a per-run credential.**
  The plane is not a registrar: a softphone that insists on registering before
  it will place a call cannot be used. `baresip` works and is BSD licensed.
- **Transfers land on the plane's own endpoints**, one per destination the
  package declares, and each leg is recorded to `calls/<run>/<name>.wav`.
- **`--to` on the `media-websocket` plane never dials the number.** The stand-in
  is both the carrier and the far end, so the number is carried in the request
  and echoed back. It proves the agent can ask for a call and talk on it, and
  nothing about which destination a number reaches. Twilio only on that plane:
  the other two carriers' call-creation dialects are redirected to the stand-in
  so nothing leaves the machine, and refused there with a message saying so.
  `cloud-websocket` has no local outbound at all, because its agent publishes no
  endpoint to ask a call from.
- **A healthy local run is not proof of reachability.** Going live needs the
  route's dictated carrier steps, on every route, and a default run prints them
  every time for exactly this reason.
- **Warm and cold transfers are not equally testable locally, and the reason is
  worth telling the user.** On the `sip` plane a warm transfer is dialled by the
  plane, so every leg is between containers and a softphone can exercise the
  whole thing including the briefing and the merge. A cold transfer there is a
  SIP REFER: the plane hands the *caller* the destination's address and steps
  out, and a softphone on the user's machine cannot route the plane's container
  network. So cold is proven up to the REFER being sent and accepted, which is
  where the product's responsibility ends (in production the carrier routes it),
  and the headless profile completes the last leg.
- **A cold transfer runs on the `media-websocket` plane too**, on the one route
  there that emits one (`cloud-websocket`). It works by replacing the live call's
  markup at the carrier, so the stand-in serves the carrier's own call-control
  endpoint on loopback and carries the document out: it cuts the agent's stream,
  bridges the caller to a destination sink, records that leg separately, and
  honours the final hangup. Without this the agent would post to
  `api.twilio.com`, which is a write leaving the machine on a run meant to touch
  nothing.
- **No local plane can prove a person answered.** Both planes stop at the
  handoff and say so on the way out. A run that claimed a completed transfer
  would be the false completion the unattended check exists to catch.
- **The run reports transfer progress as distinct outcomes**, and two of them
  never print locally on purpose: `destination reached` on a cold transfer,
  because nothing observable says the caller arrived, and `not acted on by the
  caller`, which is indistinguishable from success from the agent's side.
- **Recordings are per leg, at `build/<target>/calls/<run>/<name>.wav`.** The
  destination's file is what the destination *heard*, so it is where to check
  whether a warm briefing carried. A file of exactly 44 bytes is a header with
  no audio, which is a different failure from silence.

`docs-site/dev/local-telephony.mdx` is the user-facing version of all of this,
including the softphone commands. Point users there rather than restating it.

Every outward change the run makes is undone when it exits, including on
`ctrl-c`.

A healthy local stack proves wiring, not reachability. A real call also needs
carrier credentials, a voice capable number, and the route's public ingress.
For SIP, network address translation on a laptop can block a call even when
every health check passes. Say that rather than letting a user conclude the
package is broken.

## The boundary Unmute does not cross

Be explicit about this, because it is the most common wrong expectation.

**Unmute never buys a phone number.** It never creates a carrier application, a
carrier trunk, or a carrier subaccount. It does not sign anyone up to Twilio,
Telnyx, Plivo, or Daily.

What Unmute does is generate the code for the route and, on `unmute dev
--telephony`, point an existing number at a local tunnel for the length of the
run and put it back afterwards.

What the operator does by hand:

| Their job |
|---|
| buy or port a voice capable number |
| create the carrier application, trunk, or credential set the route needs |
| put the values in the environment variables the connection names |
| enable any account permission the route needs, for example dial-out on a Daily domain |
| paste the markup, attach the number, or configure the trunk, as the generated runbook says |

The generated `build/<target>/README.md` carries the exact carrier steps for
that route and that carrier. Point the user at it rather than repeating a
half-remembered version.

## Outbound

Outbound needs `outbound: true` on the channel and, on most routes, a
`from_number` in the connection's environment. To place one locally, run
`unmute dev --telephony --to <E.164 number>`.

**Pipecat `sip` receives calls only.** It emits no dial-out path at all, so
`outbound: true` on that route is refused at compile and `--to` has nothing to
place. Its LiveKit sibling on the same plane does dial out. If the user wants a
self-hosted route *and* outbound, that is the one to reach for.

`on_voicemail` on the channel takes `hangup` or `leave_message` and requires
`outbound: true`. Voicemail detection belongs to the LiveKit `sip` route, so check
the route before promising it.

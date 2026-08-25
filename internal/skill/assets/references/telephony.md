# Telephony

A phone call reaches the agent over a route: a target, a transport, and a
carrier, together. Pick the route from all three, never from a brand name.

## "Put it on Twilio" is not a route

Twilio reaches both targets, four different ways, and they are not
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
| Pipecat | `daily-sip` | Twilio | your carrier forwards into the same Daily room through a helper you run |
| LiveKit Agents | `sip` | Twilio, Telnyx, Plivo | a SIP trunk carries the call into LiveKit SIP |
| LiveKit Agents | `sip` | Exotel | no adapter, so this route is refused at validation |
| LiveKit Agents | `connector` | Twilio | a generated bridge turns Twilio Media Streams into a LiveKit room |

Four of those five rows are routes an author can pick; the Exotel row is listed
so its refusal is not a surprise. Pipecat has no self-hosted `sip` route and no
`carrier-websocket` route for any carrier. Both were removed. Never offer
either, and never offer Telnyx or Plivo on a Pipecat target: those two carriers
reach a package only through LiveKit `sip`.

The emitted Pipecat Daily helper is a public ingress, not an open start API. Set
the exact public HTTPS base URL named by its generated runbook. The helper
validates Twilio's signature over the complete `/call` form before using the
Pipecat Cloud key; a missing or invalid signature returns 403 and starts no
session.

The compile report marks each phone route and prints the vendor document and
the date it was last checked. Read that report before describing the route.

Every Pipecat route here deploys to Pipecat Cloud, with `pipecat cloud deploy`.
Every LiveKit route here deploys to LiveKit Cloud, or to a LiveKit Server the
user runs themselves. There is no route left with no managed platform under
it.

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

## There is no local phone rehearsal

`unmute dev` gives a browser session, nothing more. It covers the prompt, the
tools, and the models, and it stops exactly where the phone leg would start.
There is no carrier account, no softphone, no local SIP trunk, and no stand-in
for a carrier's media stream anywhere in it. Never offer one.

A phone call reaches an agent that is **deployed**. Telephony, cold transfer,
and warm transfer are all verified the same way: compile, deploy, and place a
real call through the carrier. Say this plainly before a user assumes a working
`unmute dev` session means a working phone line:

```sh
unmute compile examples/salon-concierge --target pipecat
```

Deploy the emitted project, then follow the Telephony setup section of its
`README.md` for the exact carrier steps that route and that carrier need. See
`docs-site/telephony/overview.mdx` and `docs-site/transfers/overview.mdx` for
the user-facing version of the routes and the transfer shapes, and `deploy.md`
in this bundle for going live.

## The boundary Unmute does not cross

Be explicit about this, because it is the most common wrong expectation.

**Unmute never buys a phone number.** It never creates a carrier application, a
carrier trunk, or a carrier subaccount. It does not sign anyone up to Twilio,
Telnyx, Plivo, or Daily.

What Unmute does is generate the code for the route. Everything that reaches an
existing number, a trunk, or a carrier console is a step in the generated
runbook, done by hand, after a deploy.

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
`from_number` in the connection's environment. Placing a call is verified after
deploy, against the real carrier; there is no local stand-in for it.

`on_voicemail` on the channel takes `hangup` or `leave_message` and requires
`outbound: true`. Voicemail detection belongs to the LiveKit `sip` route, so check
the route before promising it.

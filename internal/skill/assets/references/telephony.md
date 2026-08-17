# Telephony

A phone call reaches the agent over a route: a target, a transport, and a
carrier, together. Pick the route from all three, never from a brand name.

## "Put it on Twilio" is not a route

Twilio reaches both targets, three different ways, and they are not
interchangeable. Before writing anything, get three answers:

1. **Which target**, Pipecat or LiveKit?
2. **Which transport**, which is the mechanism that carries the call?
3. **Which carrier**, or none at all?

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

The compile report marks each phone route and prints the vendor document and
the date it was last checked. Read that report before describing the route.

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

Three shapes exist:

| Shape | When | Looks like |
|---|---|---|
| full route | most connections | `transport`, `carrier`, and an `environment` map |
| no credentials | receive only on Pipecat `cloud-websocket` | `transport` and `carrier`, nothing else |
| no carrier | outbound dial-out through Daily | `transport: daily-sip`, and that is the whole file |

The receive-only shape has an edge worth knowing: the moment the package places
a call or hands one to a person, that same route needs `account_sid`,
`auth_token`, and `from_number`, because both of those speak to Twilio's API in
your name. The refusal says which behaviour asked for them.

The no-carrier Daily shape is outbound-only. It can back a control that dials a
person, but it cannot receive calls and is refused if the package declares a
telephony channel.

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

This runs the route the connection declares, on the user's laptop, and a real
call can reach it. In order it compiles and prints the resolved route, checks
the credentials that are genuinely theirs, opens a `cloudflared` tunnel on the
routes that need a public callback origin, and brings up the generated Compose
stack.

Two things worth telling the user in advance:

- **Quick tunnel URLs rotate on every run**, which is why the carrier webhook is
  rewritten on every start rather than once. `--public-url` skips the tunnel and
  uses their own origin.
- **The LiveKit SIP route needs no tunnel**, because SIP does not call back over
  HTTP.
- **A real LiveKit SIP call still needs a reachable telephony deployment**:
  either a LiveKit Cloud project, or a public self-hosted LiveKit Server and SIP
  service. The agent worker may run locally against either one. Starting the
  laptop Compose graph alone does not make its SIP and RTP ports public.

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

`on_voicemail` on the channel takes `hangup` or `leave_message` and requires
`outbound: true`. Voicemail detection is a SIP-route capability, so check the
route before promising it.

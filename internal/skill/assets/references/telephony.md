# Telephony

A phone call reaches the agent over a route: a target, a transport, and a
carrier, together. Pick the route from all three, never from a brand name.

## "Put it on Twilio" is not a route

Twilio reaches three targets, five different ways, and they are not
interchangeable. Before writing anything, get three answers:

1. **Which target**, Pipecat, LiveKit or Twilio? The twilio target is one
   inbound agent with one prompt; anything more needs Pipecat or LiveKit.
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
| Twilio | `conversation-relay` | Twilio | Twilio ConversationRelay does the speech and calls a small app the user hosts |

Five of those six rows are routes an author can pick; the Exotel row is listed
so its refusal is not a surprise. Pipecat has no self-hosted `sip` route and no
`carrier-websocket` route for any carrier. Both were removed. Never offer
either, and never offer Telnyx or Plivo on a Pipecat target: those two carriers
reach a package only through LiveKit `sip`.

The emitted Pipecat Daily helper is a public ingress, not an open start API. Set
the exact public HTTPS base URL named by its generated runbook. The helper
validates Twilio's signature over the complete `/call` form before using the
Pipecat Cloud key; a missing or invalid signature returns 403 and starts no
session.

`build/<target>/compile-report.json` marks each phone route and records the
vendor document and the date it was last checked. Read that report before
describing the route.

Every Pipecat route here deploys to Pipecat Cloud, with `pipecat cloud deploy`.
Every LiveKit route here deploys to LiveKit Cloud, or to a LiveKit Server the
user runs themselves. The Twilio `conversation-relay` route runs on the
user's own host, behind a public HTTPS origin, with Twilio doing the speech.

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
| Twilio | `conversation-relay` | `twilio` | `account_sid`, `auth_token`, `phone_number_sid`, `public_url` |

The Twilio route is the `provider: twilio` target; it is described in
`references/package.md` under "The twilio target". Its app is hosted by the
user, not by a managed platform, and `phone_number_sid` is read only by
`unmute deploy --target <name>`, which points that number at the hosted app.

The SIP route uses standard SIP names rather than one vendor's, because the same
generated code dials through any SIP carrier with them.

### The target

```yaml targets.yaml
targets:
  livekit:
    provider: livekit
    version: "1.8.1"
    sdk_language: python
    connection: twilio_sip
```

A target names at most one connection, and a connection declares one transport.
Two carriers, or two mechanisms, means two targets with a connection file each,
and each compiles to its own `build/<target>/`.

Transport, carrier, and destinations are refused on a target and the refusal
names the new home.

## Inbound on LiveKit SIP, step by step

This is the route people get stuck on, so write it out rather than summarising
it. Four pieces, and only the first is Unmute's:

| # | Piece | Lives in | If it is missing |
|---|---|---|---|
| 1 | the phone channel and the connection | the package | a browser-only agent, nothing to route a call to |
| 2 | the deployed agent | LiveKit Cloud | the rule names an agent that never joins, so the phone rings forever |
| 3 | the origination URL on the carrier trunk | the carrier | the call stops at the carrier |
| 4 | the inbound trunk and the dispatch rule | the LiveKit project | LiveKit refuses the call, or opens a room with nobody in it |

**Pass `--project` on every `lk` command.** `lk` has a default project, marked
with `*` in `lk project list`, and it is often not the one the agent deploys to.
A command without `--project` writes to that default and reports success. This is
the single most common way to end up with correct records in the wrong account.

### 1 and 2: compile, then deploy

```sh
unmute validate <pkg> --target livekit
unmute compile <pkg> --target livekit
cd <pkg>/build/livekit
lk --project "<project>" agent create --region "<region>" --secrets-file .env
```

Later changes use `lk --project "<project>" agent deploy .`, which updates the
agent in `livekit.toml` rather than creating a second one. Pass
`--secrets-file .env` whenever the declared name set changes: the generated agent
checks its whole list the moment a SIP caller arrives, so a missing name fails
the call and not the build.

### 3: the carrier

The origination URL is the forwarding address that hands the call to LiveKit.
Read the project ID off `lk project list`, drop the `p_` prefix, and put the rest
in front of `.sip.livekit.cloud`. So `p_abc123def` gives
`sip:abc123def.sip.livekit.cloud;transport=tcp`. It is **not** the `LIVEKIT_URL`
subdomain; the two strings are unrelated.

Give them the address to paste, never a pipeline that derives it:

```sh
twilio api:trunking:v1:trunks:origination-urls:create \
  --trunk-sid "<TK...>" --friendly-name "LiveKit SIP" \
  --sip-url "sip:abc123def.sip.livekit.cloud;transport=tcp" \
  --weight 1 --priority 1 --enabled
```

**The number has to be attached to that trunk, and it is attached from inside the
trunk.** In the console: open the trunk, go to its **Numbers** tab, click **Add a
Number** (some accounts say Add an Existing Number), tick the number, save. Not
from the number's own page, which is where people look first. Check it with
`twilio api:trunking:v1:trunks:phone-numbers:list --trunk-sid "<TK...>"`.

A number attached to a SIP trunk ignores its own voice configuration, silently,
so it cannot also serve a webhook or a TwiML Bin. One route per number.

*Self-hosted LiveKit:* there is no project SIP URI. Point origination at the
public SIP signalling address of the LiveKit SIP service they deployed.

### 4: the two LiveKit records

`unmute compile` writes `sip-inbound-trunk.json` and `sip-dispatch-rule.json`
into `build/livekit/`. Those are the inputs, and they carry fields the `lk` flags
cannot express. Never edit `build/`: change the package and compile again.

Each file holds exactly one `${...}` placeholder: the phone number in the trunk
input, the trunk ID in the rule. Both commands take the file as an argument, so
tell the author to edit the placeholder and run the command. Do not hand them a
`sed` pipeline.

1. In `sip-inbound-trunk.json`, replace the placeholder with the number in E.164
   form, then `lk --project "<project>" sip inbound create sip-inbound-trunk.json`.
2. Copy the `ST_` ID it prints into `sip-dispatch-rule.json` in place of its
   placeholder, then
   `lk --project "<project>" sip dispatch create sip-dispatch-rule.json`.

Stop if the first command prints no ID: a rule with the placeholder still in it
matches every trunk in the project. Compiling again rewrites both files.

**Never offer the `lk` flags instead of the JSON.** Two flag sets look
equivalent and are not:

- `sip dispatch create --individual` has no flag for `roomConfig.agents`, so it
  makes a rule with an empty Agents column. The room opens, nothing joins it, and
  the caller hears ringing forever.
- `sip inbound create --auth-user/--auth-pass` makes a trunk that challenges
  incoming INVITEs for a SIP password. Carrier origination sends none, because it
  identifies itself by source IP, so every call is rejected. The generated JSON
  sets no authentication, which is correct here. To restrict inbound, use
  `allowedAddresses` with the carrier's signalling IP ranges, never digest auth.

Check both records:

```sh
lk --project "<project>" sip inbound list      # Authentication column empty
lk --project "<project>" sip dispatch list     # Agents column names the agent
```

There is no setup script. Earlier builds emitted a `telephony-setup.sh`; it
called bare `lk`, which takes no project flag, so it could create both records in
the wrong account and report success. Never tell anyone to run it.

### Which parts to redo, when

| What changed | Redo |
|---|---|
| prompt, tools or model | 1, 2 |
| a new phone number | `SIP_FROM_NUMBER` in `.env`, then 1, 2, 3, 4 |
| a new LiveKit project | all four: SIP URI, trunk and rule are all per project |
| the agent's name | 1, 2, then the dispatch rule, which names the agent as plain text |

### When they say it does not work

The symptom is rarely near the cause.

| What they see | What it is | Fix |
|---|---|---|
| carrier call log says failed, 0 seconds, 0 cost | the inbound trunk has `authUsername` set | recreate the trunk from the generated JSON, which sets no auth |
| records in an account nobody expected | a bare `lk` command used the default project | always pass `--project`, delete the strays, create them again |
| the phone rings forever, nobody answers | the rule's Agents column is empty, or the named agent is not deployed | recreate the rule from the JSON, check `lk agent list` |
| nothing in the carrier call log at all | no origination URL, or the number is not on the trunk | part 3, especially the Numbers tab |
| everything looks right, still nothing | the number is on a SIP trunk, so its voice configuration is ignored | one route per number |
| the deploy worked, behaviour is the old one | deployed from a branch without the SIP changes | deploy from the branch that has them |

Taking it down: delete the dispatch rule before the trunk, because the rule
points at the trunk. Removing the carrier's origination URL alone stops calls
reaching the agent and leaves the number and trunk intact.

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
the user-facing version of the routes and the transfer shapes,
`docs-site/telephony/pipecat-twilio.mdx` for the Pipecat `cloud-websocket` route
end to end, and `deploy.md` in this bundle for going live. Never invent carrier
markup: the emitted runbook dictates it, and that page explains what each part
of it is for.

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
| on LiveKit `sip`, create the inbound trunk and the dispatch rule with `lk`, from the JSON files compile wrote |

**Unmute never creates a LiveKit SIP record either.** It writes the two JSON
inputs and nothing else runs `lk`. There is no setup script to point anyone at.

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

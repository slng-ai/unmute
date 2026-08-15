# Transfers to a person

Sooner or later a caller needs a human. One authoring shape, `kind:
human_transfer`, two forms, and the phone route decides which you can have.

## Cold and warm

| Form | What the caller experiences |
|---|---|
| **cold** | the agent says it is putting them through, the call moves to the person, and the agent drops out |
| **warm** | the caller waits while the agent rings the person, tells them what the call is about, and only then connects the two |

Warm is what people picture when they say "transfer me to a manager". It is also
the harder one, and it compiles on exactly one route.

## How you write it

The shape you write is the shape you get. There is no `mode:` field, so a warm
only setting cannot be written on a cold transfer.

```yaml agent.yaml
controls:
  send_to_billing:
    kind: human_transfer
    when: The caller asks about an invoice, a refund, or a charge they do not recognise.
    cold:
      destination: billing_line

  escalate_to_supervisor:
    kind: human_transfer
    when: The caller is unhappy with how something was handled and asks for a manager.
    warm:
      destination: supervisor_line
      briefing: |
        Lead with the caller's name and which stylist they saw.
        Say what they are unhappy about and what you already offered them.
        Ask whether they can take the call now.
      ring_timeout: 25s
      on_unavailable: return_to_caller
```

| Block | Fields |
|---|---|
| `cold` | `destination`, `ring_timeout`, `on_unavailable` |
| `warm` | `destination`, `briefing`, `ring_timeout`, `on_unavailable` |

`on_unavailable` is `return_to_caller` or `hangup`. The control's name goes in
the agent's tool list, like any other control.

### A warm transfer needs `outbound: true`, even on an inbound-only line

This is the trap. A warm transfer rings the destination, and ringing someone is
an outbound call, so the channel has to allow one:

```yaml agent.yaml
channels:
  phone:
    kind: telephony
    inbound: true
    outbound: true    # required by the warm transfer, not by the brief
```

Leave it out and validation fails on something the user never asked for:

```
livekit: channel "phone" needs outbound: true; a warm transfer places a call to its destination
```

The shape this bites is common: an inbound-only line that sometimes has to reach
a person. Reading the channel documentation alone, `outbound: false` looks
obviously right. Set it to `true` whenever the package has a warm transfer, and
tell the user why, because it is not a decision they asked for.

A cold transfer moves the caller's existing leg rather than placing a call, so
it does not need this.

`briefing` is what the agent says to the person picking up, before the two are
connected. Write it as spoken instructions, in the order they should be said.
The rules in `prompting.md` apply.

## Destinations name a variable, never a number

```yaml agent.yaml
destinations:
  billing_line: BILLING_PHONE_NUMBER
  supervisor_line: SUPERVISOR_PHONE_NUMBER
```

A destination is only the `UPPER_SNAKE` name of an environment variable holding
an E.164 number or a `sip:` URI, read at call time. A number written there is
refused:

```
agent.yaml:60: destination "billing_line" is "+14155550123", a literal. agent.yaml is
  the portable half of a package, so a destination names an environment variable holding
  the number: billing_line: BILLING_PHONE_NUMBER
```

The model never sees a number and cannot dial one that is not listed. If the
user gives you a real phone number, put its **name** in `destinations:` and its
name in `secrets:`, and tell them to set the value in their environment.

Destinations sit at the top level of `agent.yaml` rather than on the target,
because who this agent escalates to is the same desk whichever carrier reaches
it.

## What each route can do

Transfers ride the platform's own primitive, so the answer differs per route.
Check this table **before** promising a shape.

| Route | Cold | Warm | Mechanism |
|---|---|---|---|
| LiveKit `sip` | yes | **yes, the only one** | SIP REFER on the caller's existing leg, and LiveKit's warm transfer task for the held and briefed shape |
| Pipecat `daily-sip` | yes | not built yet | Daily moves the caller's leg out of the room and the bot drops off |
| Pipecat `cloud-websocket` | yes, differently | no, by trade | one request replaces the live call's markup at the carrier |
| Pipecat `carrier-websocket` | no | no | the transport carries media only, with no transfer control |
| LiveKit `connector` | no | no | same |

Two of those cells mean different things:

- **no** means the platform does not ship the primitive.
- **not built yet** means the platform ships it and Unmute has not emitted it.

Do not blur those two when you explain a limit.

A transfer a route cannot do is refused at validation, naming the connection and
the transport it declares:

```
pipecat: telephony warm_transfer: telephony route (pipecat, cloud-websocket, twilio) does
  not emit warm transfer: a warm handoff has to act on how the destination's leg ended,
  which on this route needs a callback endpoint you host, and hosting nothing is what
  this route is for; warm transfer compiles on (livekit, sip) trunks today. Connection
  "twilio_voice" declares transport: cloud-websocket
```

That last sentence is the fix. To get a warm transfer, change the connection,
not the control.

### A transfer needs a route, and the compiler refuses one without

A transfer moves a real phone leg. A cold transfer hands the caller's own leg to
the destination, so a target that names no route has nothing to hand over.
`unmute validate` refuses it:

```
livekit: cold transfer needs a telephony Connection: it hands the caller's own
phone leg to the destination, and a session that did not arrive by phone has no
leg to hand over
```

Pipecat refuses the same package in its own words, and only its own:

```
pipecat: Pipecat cold transfer requires Daily SIP transport
```

| The target names | A transfer to a person |
|---|---|
| a connection | possible, subject to the route table above |
| no connection | **refused at validate**, before anything is written |

A route is not the same as a phone channel. `examples/pipecat-human-transfer-daily`
has only `web: realtime_audio` and transfers fine, because Daily's dial-out
brings the person into the room the browser session is already in. What it does
have is a connection.

So when a user asks a browser agent with no route to reach a person, offer what
actually works instead: a tool that books a callback, raises a ticket, or emails
a desk. You will not have to guess, because writing the control anyway fails at
validate rather than compiling into nothing.

## So: a user asks for a warm transfer

Say this, in this order:

1. Warm transfer compiles on the LiveKit `sip` route and no other route today.
2. That means a LiveKit target and a SIP trunk with a carrier, which means the
   four SIP environment names in `telephony.md`.
3. If they are already on Pipecat and cannot move, cold transfer works there and
   here is what the caller will experience instead.
4. `examples/livekit-human-transfer` is the working package. Start from it.

Do not write a warm block against a Pipecat target and let validate deliver the
news.

## When the person does not pick up

```yaml
      ring_timeout: 25s
      on_unavailable: return_to_caller
```

On the LiveKit `sip` route, no answer, a decline, voicemail, and a failed dial
all come back as one failure, and `on_unavailable` decides what happens next.

On the Pipecat `cloud-websocket` route there is one honest limit worth naming
before anyone ships: when the dial ends, however it ends, the caller hears a
handback line and returns to a **fresh** agent that does not remember the call.
Knowing more would need a callback endpoint the user hosts, and that route
exists to host nothing.

## What a target refuses

| Option | Refused on | Why |
|---|---|---|
| `briefing` | Pipecat, Deepgram | Pipecat has no warm transfer, so there is no briefing to lower |
| `requires:` on a transfer | Vapi | Vapi has no machine-checked transfer guard |
| `include_tool_calls: false` on transfer context | Pipecat, Vapi | the Pipecat driver does not shape transfer context yet |
| a variables subset on transfer context | Pipecat, Vapi | Pipecat accepts context, not a subset. Vapi accepts `variables: all` only |

## The working packages

| Package | Route | Shape |
|---|---|---|
| `examples/livekit-human-transfer` | LiveKit over a Twilio SIP trunk | cold and warm, side by side |
| `examples/pipecat-human-transfer-twilio` | Pipecat Cloud, reached through a Twilio number | cold, with nothing hosted by the user |
| `examples/pipecat-human-transfer-daily` | Pipecat over a Daily-provisioned number | cold, with no carrier account at all |

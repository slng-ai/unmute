# Transfers to a person

Sooner or later a caller needs a human. One authoring shape, an entry under an
agent's `transfers:`, two forms, and the phone route decides which you can have.

## Cold and warm

| Form | What the caller experiences |
|---|---|
| **cold** | the agent says it is putting them through, the call moves to the person, and the agent drops out |
| **warm** | the caller waits while the agent rings the person, tells them what the call is about, and only then connects the two |

A transfer is cold. Writing `brief:` makes it warm: the agent briefs the person
first.

Warm is what people picture when they say "transfer me to a manager". It is also
the harder one, and it compiles on exactly one route.

## How you write it

The shape you write is the shape you get. There is no `mode:` field: a transfer
with a `brief:` is warm and one without is cold.

```yaml agent.yaml
agents:
  - name: front_desk
    prompt: instructions.md
    think: reasoning
    speak: voice
    transfers:
      - name: send_to_billing
        to: BILLING_PHONE_NUMBER
        when: The caller asks about an invoice, a refund, or a charge they do not recognise.

      - name: escalate_to_supervisor
        to: SUPERVISOR_PHONE_NUMBER
        when: The caller is unhappy with how something was handled and asks for a manager.
        brief: |
          Lead with the caller's name and which stylist they saw.
          Say what they are unhappy about and what you already offered them.
          Ask whether they can take the call now.
        ring_timeout: 25s
        on_unavailable: return_to_caller
```

| Field | Meaning |
|---|---|
| `name` | the transfer's name, one name across the package |
| `to` | the environment variable holding the number |
| `when` | the situation in which the model takes it |
| `brief` | what the agent says to the person first; present makes it warm |
| `ring_timeout` | how long to ring |
| `on_unavailable` | `return_to_caller` or `hangup` |

### Who defines it and who can use it

A transfer is defined once in the package, by the agent that writes its block.
Any other agent names it with a bare name under its own `transfers:`. A bare
name nobody defines is refused, and so are two blocks with one name. A transfer
is not something a task can hold: a task has `tools:` and `handoffs:` and no
other list, so there is no key to put one in.

A transfer no reachable agent lists is a build error, not dead config. The
refusal names the file, the line, and the agents you could attach it to:

```
agent.yaml:47: transfer "send_to_billing" is declared but no agent reaches it; add it to the
  transfers: of one of these agents: front_desk, billing
```

### A warm transfer needs `outbound: true`, even on an inbound-only line

This is the trap. A warm transfer rings the destination, and ringing someone is
an outbound call, so the channel has to allow one:

```yaml targets.yaml
channels:
  - name: phone
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

`brief` is what the agent says to the person picking up, before the two are
connected. Write it as spoken instructions, in the order they should be said.
The rules in `prompting.md` apply.

## `to:` names a variable, never a number

A transfer's `to:` is only the `UPPER_SNAKE` name of an environment variable
holding an E.164 number or a `sip:` URI, read at call time. Declare that
variable in `secrets:`. A number written there is refused:

```
agent.yaml:60: transfer "send_to_billing" has a literal to:. agent.yaml is the portable half of a package, so to: names an environment variable holding the number, for example to: BILLING_PHONE_NUMBER
```

The model never sees a number and cannot dial one that is not listed. If the
user gives you a real phone number, put its **name** in `to:` and in
`secrets:`, and tell them to set the value in their environment.

The destination sits on the transfer in `agent.yaml` rather than on the target,
because who this agent transfers to is the same desk whichever carrier reaches
it.

## What each route can do

Transfers ride the platform's own primitive, so the answer differs per route.
Check this table **before** promising a shape.

| Route | Cold | Warm | Mechanism |
|---|---|---|---|
| LiveKit `sip` | yes | **yes, the only one** | SIP REFER on the caller's existing leg, and LiveKit's warm transfer task for the held and briefed shape |
| Pipecat `daily-sip` + Twilio | yes, on a phone call | not built yet | Daily transfers the existing SIP phone leg |
| Pipecat `cloud-websocket` | yes, differently | no, by trade | one request replaces the live call's markup at the carrier |
| LiveKit `connector` | no | no | no platform transfer primitive on this route |

Two of those cells mean different things:

- **no** means the platform does not ship the primitive.
- **not built yet** means the platform ships it and Unmute has not emitted it.

## Verifying a transfer

There is no local stand-in for a carrier, so a transfer is verified the same way
as any other phone behaviour: compile, deploy, and place a real call. `unmute
dev` never runs a transfer, cold or warm, on any route; it stops at the browser
session. Say that plainly, so an author does not assume a working `unmute dev`
session proves a transfer works.

One more thing worth saying out loud, because it wastes real time: **a model will
announce a transfer the package never declared.** If an agent says it is putting
somebody through and nothing happens, check for an entry under `transfers:`
before looking anywhere else.

Do not blur those two when you explain a limit.

Unmute does not support warm transfer on any Pipecat target. Warm transfer
currently requires LiveKit `sip`.

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

A Pipecat `daily-sip` transfer needs a Twilio connection and an active phone
channel. A browser session has no SIP leg and must fail before announcing a
transfer.

On LiveKit, a cold destination environment name is call-only: it is checked
after the worker identifies a real SIP job and before the greeting, so it does
not block WebRTC startup. Warm transfer is different because it can dial from a
browser session. Its destination plus the selected SIP connection's address,
username, password, and caller number stay in browser `REQUIRED_ENV` and
`compose.dev.yaml`.

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
4. `examples/salon-concierge` is the working package for the cold half. No
   shipped example declares a warm block; write it from the fields here.

Do not write a `brief:` against a Pipecat target and let validate deliver the
news.

## When the person does not pick up

```yaml
      ring_timeout: 25s
      on_unavailable: return_to_caller
```

On the LiveKit `sip` route, no answer, a decline, voicemail, and a failed dial
all come back as one failure, and `on_unavailable` decides what happens next.

On the Pipecat `cloud-websocket` route, when the destination leg ends, Twilio
ends the original call. A decline or no answer does the same after the dial
timeout. No fresh agent starts without the previous conversation context.
Pipecat `cloud-websocket` requires explicit `on_unavailable: hangup`; it cannot
reconnect the original media stream. Omitting the field resolves to
`return_to_caller`, so validation refuses it on this route. A successful Twilio
REST update means the transfer has started, not that the destination answered;
the tool result is `transfer_started`.

## What a target refuses

| Option | Refused on | Why |
|---|---|---|
| `brief` | Pipecat, Deepgram | Pipecat has no warm transfer, so there is no briefing to lower |
| `include_tool_calls: false` on transfer context | Pipecat | the Pipecat driver does not shape transfer context yet |

## The working packages

| Package | Route | Shape |
|---|---|---|
| `examples/salon-concierge`, `livekit` target | LiveKit over a Twilio SIP trunk | cold to a manager |
| `examples/salon-concierge`, `pipecat` target | Pipecat Cloud, reached through a Twilio number | cold, with nothing hosted by the user |

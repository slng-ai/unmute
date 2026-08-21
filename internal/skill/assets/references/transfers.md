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

`on_unavailable` is `return_to_caller` or `hangup`.

### The control's name goes in an agent's tool list, and that half is enforced

Declaring a human transfer is half the job. Until a reachable agent lists its
name in `tools:`, no agent can reach it, and the build refuses with the file,
the line, and the agents you could attach it to. Human transfers are not task
controls; validation rejects `human_transfer` in a task's list.

```
agent.yaml:47: control "send_to_billing" is declared but no agent reaches it; add it to the
  tools: of one of these agents: front_desk, billing
```

The same refusal covers anything else the entry agent cannot reach: a
`destinations:` entry no control resolves to, a top-level `tools:` entry no agent
lists, a task or task group nothing delegates to, an agent no `agent_transfer`
points at. An unreferenced `models:` entry is the one exception and stays legal,
because that map is a palette.

Write the declaration and the attachment in the same edit. Before this was
enforced, a forgotten attachment compiled at exit 0 and left the control out of
the generated project, while its destination's environment name still reached
`.env.example` and the startup check.

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
| Pipecat `daily-sip` + Twilio | yes, on a phone call | not built yet | Daily transfers the existing SIP phone leg |
| Pipecat `cloud-websocket` | yes, differently | no, by trade | one request replaces the live call's markup at the carrier |
| Pipecat `sip` | yes | not built yet | the platform's own SIP participant transfer, on a room the agent is already in |
| Pipecat `carrier-websocket` | no | no | the transport carries media only, with no transfer control |
| LiveKit `connector` | no | no | same |

Two of those cells mean different things:

- **no** means the platform does not ship the primitive.
- **not built yet** means the platform ships it and Unmute has not emitted it.

## Testing a transfer without a carrier

`unmute dev --telephony` runs transfers locally, with no carrier account. What it
can show differs by route, and the difference is worth stating before an author
goes looking for a bug that is a route limit:

| Route | Locally |
|---|---|
| LiveKit `sip` | warm end to end, including the briefing and the merge. Cold up to the REFER being accepted |
| Pipecat `cloud-websocket` | cold up to the handoff: the caller's leg leaves the agent, the destination leg is recorded, the final hangup is honoured |
| Pipecat `sip` | cold up to the REFER: same plane and same primitive as its LiveKit sibling, but the run prints only the request and any failure, so a completed handoff shows as the caller's leg leaving the room |
| Pipecat `daily-sip` | nothing: this route has no local plane and the command refuses it |
| Pipecat `carrier-websocket`, LiveKit `connector` | nothing to test: neither emits a transfer |

**No local run proves that a person answered.** Every one of them stops at the
handoff and prints that it did. If an author asks whether the transfer "worked",
that distinction is the answer.

One more thing worth saying out loud, because it wastes real time: **a model will
announce a transfer the package never declared.** If an agent says it is putting
somebody through and nothing happens, check for a `human_transfer` control with a
`cold:` or `warm:` block before looking anywhere else.

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
| `briefing` | Pipecat, Deepgram | Pipecat has no warm transfer, so there is no briefing to lower |
| `requires:` on a transfer | Vapi | Vapi has no machine-checked transfer guard |
| `include_tool_calls: false` on transfer context | Pipecat, Vapi | the Pipecat driver does not shape transfer context yet |
| a variables subset on transfer context | Pipecat, Vapi | Pipecat accepts context, not a subset. Vapi accepts `variables: all` only |

## The working packages

| Package | Route | Shape |
|---|---|---|
| `examples/livekit-human-transfer` | LiveKit over a Twilio SIP trunk | cold and warm, side by side |
| `examples/pipecat-human-transfer-twilio` | Pipecat Cloud, reached through a Twilio number | cold, with nothing hosted by the user |

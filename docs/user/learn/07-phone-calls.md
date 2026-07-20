# 07. Phone calls

Unmute compiles phone-call intent for two orchestrators: **Pipecat** and
**LiveKit**. The Agent says what the call needs, a Connection names the secret
environment variables, and the target selects one exact media route. Unmute
does not buy a number, create a trunk, or copy credentials into generated code.

All carrier routes are currently **provisional**. The compiler and generated
Pipecat Twilio and Telnyx adapters have credential-free tests, but validation
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
carrier call control. It does not parse or emit audio frames. The Twilio and
Telnyx files are separate generated adapters because their signatures and call
control APIs differ; selecting one never emits the other's SDK or credentials.

LiveKit uses either `transport: sip` or the distinct Beta
`transport: connector` route. The Connector is Twilio-only and cannot inherit
SIP transfer behavior. Self-hosted LiveKit SIP also needs Redis because the
LiveKit server and SIP service use it as a shared datastore and message bus;
Pipecat's single-process adapter can keep one call's pending context local.

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
| Self-hosted LiveKit SIP | `LIVEKIT_URL`, `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET`, `REDIS_URL` | Create the key pair in the LiveKit Server configuration; use the same server and Redis deployment for LiveKit SIP. Add the selected carrier trunk credentials. |

Carrier WebSocket deployments also set `UNMUTE_PUBLIC_URL` to the exact public
HTTPS origin used in signature validation. It is configuration, not a secret.
Outbound HTTP starts require a separate secret, `UNMUTE_OUTBOUND_TOKEN`, which
you generate yourself. It is never a carrier credential.

The complete credential links and self-hosted topology are in
[TELEPHONY.md](../../../TELEPHONY.md#credentials).

For Telnyx, configure the Voice API Application for API version 2 and point its
webhook URL at the inbound endpoint printed by `unmute dev --telephony`. Assign
the phone number to that application. Telnyx signs HTTP events with the public
key; the generated WebSocket URL carries a short-lived, one-use opaque token.

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

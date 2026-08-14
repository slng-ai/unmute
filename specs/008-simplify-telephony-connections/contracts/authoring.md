# Contract: the authoring shape

What a package author writes. This is the user-facing interface of the feature —
for a compiler CLI, the file format *is* the API.

---

## The rule, in one line

**A target names one connection. The connection is the whole route.**

---

## `targets.yaml`

```yaml
targets:
  livekit:
    provider: livekit
    version: "1.6.4"
    sdk_language: python
    connection: twilio_sip
    deployment_region: eu-central
    models:
      detector:
        provider: livekit
        model: turn-detector-mini
```

Accepted keys: `provider`, `version`, `pins`, `sdk_language`, `connection`,
`deployment_region`, `models`.

Rejected, with the message naming the new home: `transport`, `carrier`,
`destinations`.

### Providers with no driver keep no exception

`vapi` and `deepgram` validate but never compile. They have no route, no transport
vocabulary, and no connection — and they lose `carrier` like every other target:

```yaml
targets:
  vapi:
    provider: vapi
    models:
      front_desk:
        provider: elevenlabs
        voice: cgSgspJ2msm6clMCkdW9
```

Four capability rows condition on their `carrier` today. Since no author can write
one after this change, those rows lose the condition rather than becoming
impossible to satisfy (FR-001a). The Twilio requirement each records moves into a
comment for whoever builds that driver.

---

## `connections/<name>.yaml`

Three valid shapes.

**Full** — a route with credentials:

```yaml
transport: sip
carrier: twilio
environment:
  sip_address: SIP_TRUNK_HOSTNAME
  sip_username: SIP_AUTH_USERNAME
  sip_password: SIP_AUTH_PASSWORD
  from_number: SIP_FROM_NUMBER
```

**No credentials** — receive-only on `(pipecat, cloud-websocket, twilio)`, where
the platform terminates the carrier's stream itself:

```yaml
transport: cloud-websocket
carrier: twilio
```

**No carrier** — a Daily-provisioned number, which carries its own calls:

```yaml
transport: daily-sip
```

`kind:` is no longer written. Every transport in the catalog is telephony, so the
line said nothing the first line does not.

Values in `environment` are environment variable **names**, never secrets. The
loader never reads the named values.

---

## `agent.yaml`

```yaml
secrets:
  - OPENAI_API_KEY
  - SLNG_API_KEY
  - SIP_TRUNK_HOSTNAME
  - SIP_AUTH_USERNAME
  - SIP_AUTH_PASSWORD
  - SIP_FROM_NUMBER
  - BILLING_PHONE_NUMBER
  - SUPERVISOR_PHONE_NUMBER

destinations:
  billing_line: BILLING_PHONE_NUMBER
  supervisor_line: SUPERVISOR_PHONE_NUMBER

channels:
  phone:
    kind: telephony
    inbound: true
    outbound: true
```

`destinations` accepts only the `UPPER_SNAKE` name of an environment variable. A
literal number or `sip:` URI is rejected: `agent.yaml` is the portable half of a
package.

`secrets` lists every environment name **the author wrote** — connection
environment values, destination values, model and tool credentials. It does not
list names the driver or platform supplies.

---

## What the author does not write

| Value | Who supplies it |
|---|---|
| `REDIS_URL`, `UNMUTE_PUBLIC_URL`, `UNMUTE_OUTBOUND_TOKEN` | `unmute dev` locally; the operator at deploy time on routes that read them |
| `LIVEKIT_URL`, `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET` | the local Compose graph, or the LiveKit Cloud project |
| `DAILY_API_KEY`, `PIPECAT_CLOUD_ORGANIZATION` | the route's own runtime environment; required at runtime, never declared in the package |

These are exempt from `secrets:` (FR-005c) and are still documented wherever they
reach a generated `.env.example` (FR-005f).

---

## Migration, by example

`examples/twilio-telephony-hello`, before:

```yaml
# targets.yaml
  livekit:
    provider: livekit
    transport: sip
    carrier: twilio
    connection: twilio_sip

# connections/twilio_sip.yaml
kind: telephony
environment:
  sip_address: SIP_TRUNK_HOSTNAME
  ...
```

After:

```yaml
# targets.yaml
  livekit:
    provider: livekit
    connection: twilio_sip

# connections/twilio_sip.yaml
transport: sip
carrier: twilio
environment:
  sip_address: SIP_TRUNK_HOSTNAME
  ...
```

Three route lines on the target become one. Across the five telephony examples,
route lines in `targets.yaml` go from 19 to 7.

---

## The one package that gains a file

`examples/outbound-reminder` names a single `twilio_voice` connection from two
targets whose transports differ — `carrier-websocket` on Pipecat,
`connector` on LiveKit. Once a connection declares its own transport, one file
cannot serve both, so it becomes two:

- `connections/twilio_websocket.yaml` — `transport: carrier-websocket`
- `connections/twilio_connector.yaml` — `transport: connector`

Both hold the same three environment names. Six lines each. No naming convention
is mandated: the file states its transport on line one, so its name does not have
to.

---

## Authoring surface that did not grow

No new field is added anywhere. This feature moves three fields and deletes one.
`internal/spec/authoring_surface_test.go` exists to hold that line and its two
route-shape tests move to the new form rather than being deleted.

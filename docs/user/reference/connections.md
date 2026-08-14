# Reference: connections/*.yaml

A connection is one phone route. It says which mechanism carries the call, which
carrier hands it over, and which environment variables hold that account's
credentials.

**A target names one connection, and the connection is the whole route.** A
target declares nothing else about how a call reaches it, so this file is the
one place to look.

```yaml
# connections/twilio_sip.yaml
transport: sip
carrier: twilio
environment:
  sip_address: SIP_TRUNK_HOSTNAME
  sip_username: SIP_AUTH_USERNAME
  sip_password: SIP_AUTH_PASSWORD
  from_number: SIP_FROM_NUMBER
```

```yaml
# targets.yaml
targets:
  livekit:
    provider: livekit
    version: "1.6.4"
    sdk_language: python
    connection: twilio_sip
```

## Fields

| Field | Required | What it is |
|---|---|---|
| `transport` | yes | the mechanism that carries the call |
| `carrier` | on every route that has a carrier leg | who hands the call over |
| `environment` | when the route needs account values | role name → environment variable **name** |

The file name is the connection's name: `connections/twilio_sip.yaml` is named
by `connection: twilio_sip`. No naming convention is imposed, because the file
states its own transport on the first line.

There is no `kind:` field. Every transport in the catalog is telephony, so it
said nothing `transport:` does not.

## Three valid shapes

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

The carrier-less form dials out and cannot receive, so it never serves a
`channels.phone` entry. A package reaches it through a control that dials.

## Environment keys, per route

The keys a route accepts are fixed by the route, both ways: a missing required
key and an unaccepted key each fail at compile time, naming the route and the
accepted set.

| Route | Keys |
|---|---|
| `pipecat` + `carrier-websocket` + `twilio` | `account_sid`, `auth_token`, `from_number` |
| `pipecat` + `carrier-websocket` + `telnyx` | `api_key`, `public_key`, `connection_id`, `from_number` |
| `pipecat` + `carrier-websocket` + `plivo` | `auth_id`, `auth_token`, `from_number` |
| `pipecat` + `cloud-websocket` + `twilio` | `account_sid`, `auth_token`, `from_number` — required only when the package places or redirects calls |
| `pipecat` + `daily-sip` + `twilio` | `account_sid`, `auth_token`, `sip_address`, `from_number` |
| `pipecat` + `daily-sip` (no carrier) | none |
| `livekit` + `sip` + any carrier | `sip_address`, `sip_username`, `sip_password`, `from_number` |
| `livekit` + `connector` + `twilio` | `account_sid`, `auth_token`, `from_number` |

The four SIP names serve every carrier on the LiveKit SIP route: they are
standard trunk settings, not one carrier's. They are yours to choose, because
the compiler carries whatever you write through verbatim.

Values must be valid shell identifiers — letters, digits and underscores, never
starting with a digit. A deployment platform exports secrets through a shell, so
a name like `11LABS_API_KEY` would be missing at runtime with no error of its
own. The compiler refuses it instead.

## Which transfers each route supports

Cold transfer reroutes the caller and drops the agent. Warm transfer dials the
destination itself, briefs them, then connects the two.

| Route | Cold | Warm |
|---|---|---|
| `livekit` + `sip` | yes, a SIP REFER through the trunk | yes |
| `livekit` + `connector` | no | no |
| `pipecat` + `daily-sip` | yes, `sip_call_transfer` | no |
| `pipecat` + `carrier-websocket` | no | no |
| `pipecat` + `cloud-websocket` | yes, by replacing the live call's markup | no |

A transfer the route cannot emit is refused at compile time, naming the
connection, the transport it declares, and the transport the transfer needs.
Full detail: [TRANSFERS.md](../../TRANSFERS.md).

## Two connections, one account

Two connections may declare the same account. They are two accounts, or one
account reached two ways. `examples/outbound-reminder` is the second case: its
Pipecat target rides `carrier-websocket` and its LiveKit target rides
`connector`, so it holds `twilio_websocket.yaml` and `twilio_connector.yaml`
with the same three names. A connection declares its own transport, so one file
cannot serve both.

## Where each value goes

| Value | Who supplies it |
|---|---|
| Connection `environment` values | you, and each name goes in `secrets:` |
| Destination numbers | you, named in `agent.yaml` `destinations` and listed in `secrets:` |
| `REDIS_URL`, `UNMUTE_PUBLIC_URL`, `UNMUTE_OUTBOUND_TOKEN` | `unmute dev` locally; the operator at deploy time |
| `LIVEKIT_URL`, `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET` | the local Compose graph, or your LiveKit Cloud project |
| `DAILY_API_KEY`, `PIPECAT_CLOUD_ORGANIZATION` | the route's own runtime environment |

The last three rows are not declared in `secrets:`, because you do not write
them into the package. They still have to be set wherever the agent runs.

## Refusals

| What you wrote | What happens |
|---|---|
| `transport` or `carrier` on a target | refused, naming the connection file it belongs in |
| `destinations` on a target | refused, naming `agent.yaml` |
| `kind:` in a connection | refused |
| a connection with no `transport` | refused: a connection is a phone route |
| a `(transport, carrier)` the provider has no route for | refused, listing the routes it does have |
| a missing or unaccepted environment key | refused, naming the route and the accepted set |
| a target with a telephony channel and no connection | refused |
| a connection nothing in the package uses | refused |
| a connection no target names | warning on stderr, exit 0 |

## See also

- [Phone calls](../learn/07-phone-calls.md) — the end-to-end path
- [targets.yaml](targets-yaml.md) — what a target still declares
- [agent.yaml](agent-yaml.md) — `destinations` and `secrets`
- [secrets](secrets.md) — what belongs in the list

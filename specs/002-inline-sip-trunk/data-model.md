# Phase 1 Data Model: Dial out with the carrier's own SIP credentials

**Feature**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md) | **Date**: 2026-08-12

This is a compiler, so the data model is the chain a Connection's four values
travel down, from a YAML file to a line of generated Python. Nothing new is
added to the chain. What changes is that the chain now reaches the dial, and one
link (a platform-assigned trunk id) is cut out of it.

## The chain, before and after

```
connections/<name>.yaml          author writes four env NAMES
        │
        ▼   spec.Load
target.TelephonyRoute            route says WHICH keys are required
        │
        ▼   ir.Build
ir.TelephonyPlan                 keys resolved to names, per target
        │
        ▼   generate (livekit driver)
livekitTelephony                 names carried into the template model
        │
        ▼   text/template
agent.py                         os.environ[name] read at call time
```

Before: the last hop dropped the four names on the dial-out path and read a
fifth name instead, one the platform had to mint. After: the four names reach the
dial and there is no fifth.

## Entities

### Connection (telephony)

An authored YAML file. One per carrier account. Declares environment variable
**names**, never values.

| Key | Meaning | Env name before | Env name after |
|---|---|---|---|
| `sip_address` | Host the SIP INVITE is sent to. Not a URI, no `sip:` prefix. | `TWILIO_SIP_ADDRESS` | `SIP_TRUNK_HOSTNAME` |
| `sip_username` | SIP auth username | `TWILIO_SIP_USERNAME` | `SIP_AUTH_USERNAME` |
| `sip_password` | SIP auth password | `TWILIO_SIP_PASSWORD` | `SIP_AUTH_PASSWORD` |
| `from_number` | The number calls are placed from, E.164 | `TWILIO_PHONE_NUMBER` | `SIP_FROM_NUMBER` |

**Four names for one value, and they are all live at once.** The SIP host is
called `sip_address` as a Connection key, `SIP_TRUNK_HOSTNAME` as an environment
name, `hostname` on the wire, and "the SIP host" in prose. That is not drift: the
key cannot be renamed without breaking a written package, and the environment name
follows the platform's own spelling. When reading any document in this feature,
those four refer to the same string.

**The keys do not change.** Renaming a key would break a written package, which
FR-010 forbids. Only the right-hand side moves, and only in the shipped example,
the scaffold and the documents. Any name an author already wrote keeps working,
because nothing in the compiler knows these names (FR-015). That property is what
SC-010 asserts.

**Validation rules** (all existing, none added):

- Every value must match `^[A-Z][A-Z0-9_]*$` (`internal/ir/validate.go`).
- All four keys are required on the `livekit/sip/*` route
  (`internal/target/telephony.go`, `RequiredEnvironment`). A Connection missing
  any of them fails before any artifact is written, which is what FR-006 asks
  for and what SC-008 measures. R7 carries a task to assert it rather than
  assume it.
- No value may look like a secret. A pasted token fails decode rather than
  becoming a lookup that fails on a live call.

### TelephonyRoute (`internal/target/telephony.go`)

The single capability rulebook entry for `(livekit, sip, <carrier>)`. Three of
its lists move.

| Field | Before | After |
|---|---|---|
| `RequiredEnvironment` | `sip_address`, `sip_username`, `sip_password`, `from_number` | unchanged |
| `RuntimeEnvironment` | `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET`, `LIVEKIT_SIP_INBOUND_TRUNK` (inbound), **`LIVEKIT_SIP_OUTBOUND_TRUNK`** (outbound or warm), `LIVEKIT_URL`, `REDIS_URL` | the outbound trunk row is **deleted** |
| `DevSuppliedEnvironment` | `LIVEKIT_SIP_INBOUND_TRUNK`, **`LIVEKIT_SIP_OUTBOUND_TRUNK`** | the outbound trunk is **deleted** |
| `LocallySuppliedEnvironment` | `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET`, `LIVEKIT_URL`, `REDIS_URL` | unchanged |
| `ManualSteps` | four steps, the last of which creates both trunks and the dispatch rule and copies both ids back | the outbound trunk and its id copy leave; inbound trunk and dispatch rule stay |

Deleting the `DevSuppliedEnvironment` entry is what disables the local
outbound-trunk creation, because `internal/cli/dev_livekit_sip.go` gates that
block on `needs("LIVEKIT_SIP_OUTBOUND_TRUNK")`. The block then has no caller and
is deleted with it.

### ir.TelephonyPlan

The resolved, per-target record. Carries `Environment` (key to name),
`RequiredEnvironment` (the resolved names, which drive `.env.example` and the
startup check), `DevSuppliedEnv` and `LocalEnvironment`.

Not changed by this feature. It changes **shape** only as a consequence: one
name leaves `RequiredEnvironment` because the route no longer asks for it.

### livekitTelephony (template model, `internal/generate/livekit_v1.go`)

Already carries every value the inline form needs:

```go
SIPAddressEnv  string   // becomes hostname
SIPUsernameEnv string   // becomes auth_username
SIPPasswordEnv string   // becomes auth_password
FromNumberEnv  string   // becomes sip_number
HasInbound     bool
HasOutbound    bool
HasWarm        bool
```

**No field is added.** This is the point of the feature: the model already knew
everything, and the template was throwing it away on the dial-out path. The four
fields are populated in `livekit_v1_build.go:549-550` from
`plan.Environment[...]`.

### livekitData (`internal/generate/livekit_v1.go`)

The root template model. Two things about it move.

- `RequiredEnv` loses `LIVEKIT_SIP_OUTBOUND_TRUNK`, because the three
  `env.add("LIVEKIT_SIP_OUTBOUND_TRUNK")` calls in `livekit_v1_build.go` (lines
  296, 558, 790) are deleted. That name therefore leaves `.env.example`, the
  generated startup check, and the compile report.
- One flag is needed that does not exist cleanly today: whether **any** agent has
  a warm transfer, so the `from livekit import api` line can be emitted for a
  warm-only package. `livekitTelephony.HasWarm` holds it, but `.Telephony` can be
  nil in the template, and the import condition sits above the telephony guard.
  A `HasWarmTransfer bool` on `livekitData`, set beside the existing
  `HasColdTransfer` in the same loop, is the shape that matches what is already
  there.

### livekitHumanTransfer (template model)

```go
Warm     bool
ToExpr   string   // cold: the REFER URI
DialExpr string   // warm: the number to dial
```

Unchanged. `DialExpr` already resolves to either a quoted literal or an
`os.environ[...]` read, and it is what `sip_call_to` receives. The inline
configuration is a **separate** concern from the destination, which is why it
belongs in a shared fragment rather than in this struct: the config is the same
for every transfer in the project, and the destination is per transfer.

### Emitted file set

| File | Condition before | Condition after |
|---|---|---|
| `sip-inbound-trunk.json` | inbound | unchanged |
| `sip-dispatch-rule.json` | inbound | unchanged |
| `sip-outbound-trunk.json` | outbound **or** warm | **never emitted** |

`livekitSIPFiles` in `internal/generate/livekit_v1.go` loses its third block
entirely. The two inbound files are byte for byte unchanged, which SC-011
asserts.

### The inline configuration itself

Not a Go entity. It is the Python value the template renders, and it exists once
per project as a shared template fragment so all three dial sites read the same
names. Its full contract is in
[contracts/emitted-dial-out.md](./contracts/emitted-dial-out.md).

Wire shape, from `SIPOutboundConfig`: `hostname`, `destination_country`,
`transport`, `auth_username`, `auth_password`, `headers_to_attributes`,
`attributes_to_headers`, `from_host`. **Three are emitted**: `hostname`,
`auth_username`, `auth_password`. The from-number is not part of this object; it
is a sibling argument on the call, which is why it is easy to forget and why
FR-003 names it separately.

## State transitions

There is one, and it belongs to the operator rather than to any object.

```
before this change        after this change
─────────────────        ─────────────────
lk sip outbound create   (nothing)
copy id into .env        (nothing)
set 4 carrier values     set 4 carrier values, under new names
deploy                   deploy
dial: reads trunk id     dial: reads the 4 values
```

A deployment caught mid-migration has the old trunk id still set and the four
values under old or new names. That state is safe in one direction and not the
other:

- **Old trunk id still set**: harmless. The prebuilt ignores it once
  `sip_connection` is passed, by upstream design (see
  [research.md R1](./research.md#r1-what-the-warm-transfer-prebuilt-actually-accepts)).
- **Values still under old names while the example uses new ones**: the agent
  fails at startup naming the missing values, because `.env.example` and the
  generated startup check both come from the same resolved list. That is the
  loud failure Principle II wants, and User Story 3 scenario 4 requires the
  migration note to make it a mechanical fix.

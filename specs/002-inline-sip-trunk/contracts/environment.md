# Contract: the environment names

**Feature**: [../spec.md](../spec.md) | **Plan**: [../plan.md](../plan.md) | **Date**: 2026-08-12

The `livekit/sip/<carrier>` route's environment surface, before and after, plus
the migration table an operator needs. This is the contract the goldens, the
compile report and the generated `.env.example` are checked against.

## E1. The route's environment, before and after

| Name | Before | After | Who supplies it |
|---|---|---|---|
| `SIP_TRUNK_HOSTNAME` (was `TWILIO_SIP_ADDRESS`) | required | required | operator, from the carrier |
| `SIP_AUTH_USERNAME` (was `TWILIO_SIP_USERNAME`) | required | required | operator, from the carrier |
| `SIP_AUTH_PASSWORD` (was `TWILIO_SIP_PASSWORD`) | required | required | operator, from the carrier |
| `SIP_FROM_NUMBER` (was `TWILIO_PHONE_NUMBER`) | required | required | operator, from the carrier |
| `LIVEKIT_SIP_INBOUND_TRUNK` | required, inbound only | unchanged | platform, on `lk sip inbound create` |
| **`LIVEKIT_SIP_OUTBOUND_TRUNK`** | **required, outbound or warm** | **gone** | was the platform, on `lk sip outbound create` |
| `LIVEKIT_URL`, `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET` | required, platform-supplied on Cloud | unchanged | LiveKit Cloud injects them; self-hosted operator sets them |
| `REDIS_URL` | required, self-hosted only | unchanged | operator, self-hosted only. Never read on Cloud, which is why feature 001 moved it into the labelled section of `.env.example` |
| `LIVEKIT_SIP_NUMBER` | never emitted | never emitted | n/a. It is the prebuilt's own fallback for the from-number, and this repository has zero occurrences of it. The number is passed explicitly instead. |

The four carrier values do not become more or fewer. They change name, and they
change **role**: today they exist so an operator can hand them to
`lk sip outbound create`, and afterwards they are read by the agent itself at
dial time.

## E2. The names are the author's, not the compiler's

The compiler MUST contain none of the four names. A Connection declares them:

```yaml
kind: telephony
environment:
  sip_address: SIP_TRUNK_HOSTNAME
  sip_username: SIP_AUTH_USERNAME
  sip_password: SIP_AUTH_PASSWORD
  from_number: SIP_FROM_NUMBER
```

The **keys** on the left are fixed vocabulary and do not change, because renaming
one would break a package already written (FR-010). The **names** on the right are
the author's choice, and the four above are what the shipped example, the
scaffold and the documents now use.

Two consequences a test must pin:

- A Connection still declaring `TWILIO_SIP_ADDRESS` and friends compiles and
  dials exactly as before (SC-010). Most existing test fixtures deliberately keep
  the old names for this reason.
- At least one fixture uses the new names, so both spellings are covered.

## E3. The three carriers collapse onto one set of names

Today the documents show `TWILIO_SIP_ADDRESS`, `TELNYX_SIP_ADDRESS` and
`PLIVO_SIP_ADDRESS` as three separate things, and
`internal/target/user_docs_test.go` asserts all three appear. Afterwards all
three carriers use `SIP_TRUNK_HOSTNAME`, because they are one route with one
shape and the values are standard SIP trunk settings rather than one vendor's.

That test must follow the rename. It is also the thing that catches a partial
rename, since it reads the documents rather than the code.

Carrier-specific credentials on **other** routes keep their carrier names, and
that is not an inconsistency: `TWILIO_ACCOUNT_SID` and `TWILIO_AUTH_TOKEN` are
Twilio's REST API credentials and mean nothing to another carrier, while a SIP
trunk host is a SIP trunk host (FR-016).

## E4. The migration table the operator needs

This belongs in the generated README and in `examples/human-transfer/README.md`,
because User Story 3 scenario 4 requires the `.env` edit to be mechanical.

```
rename these four:
  TWILIO_SIP_ADDRESS      ->  SIP_TRUNK_HOSTNAME
  TWILIO_SIP_USERNAME     ->  SIP_AUTH_USERNAME
  TWILIO_SIP_PASSWORD     ->  SIP_AUTH_PASSWORD
  TWILIO_PHONE_NUMBER     ->  SIP_FROM_NUMBER

delete this one, nothing reads it now:
  LIVEKIT_SIP_OUTBOUND_TRUNK

keep this one if you accept inbound calls:
  LIVEKIT_SIP_INBOUND_TRUNK
```

Note the first column is the **shipped example's** old names. An operator who
chose their own names renames nothing: they update their Connection file if they
want the new convention, or leave it alone and keep working.

The stored trunk itself can be deleted at the platform with
`lk sip outbound delete`, and leaving it costs nothing but a stale record.

## E5. Failure modes, and which message each produces

| State | What happens | Where it fails |
|---|---|---|
| A Connection missing any of the four keys | gated compile error naming the missing key | `ir.Validate`, before any artifact is written (FR-006, SC-008) |
| A deployment missing one of the four values | startup check names the missing variable | the generated `require_env()` at worker start |
| A deployment with the old names set and the example's new names expected | same as above: the startup check names the new variable | worker start, not mid-call |
| A deployment that still sets `LIVEKIT_SIP_OUTBOUND_TRUNK` | nothing. The prebuilt ignores it once `sip_connection` is passed | never (FR-004, and see [emitted-dial-out.md C7](./emitted-dial-out.md#c7-behaviour-a-test-must-pin-rather-than-trust)) |
| A credential rotated at the carrier but not in the deployment | both dial-out paths fail identically, in the platform's own words | on the dial. An improvement on today, where a stored trunk id keeps working while the credentials behind it have changed |
| No from-number reaching the dial | the SIP service rejects an empty `From` | on the dial, which is why FR-003 makes it explicit rather than relying on a fallback that ends at `""` |

## E6. Local development must use the same names and the same mechanism

`unmute dev --telephony` currently creates a local outbound trunk and injects its
id. After this change it MUST NOT, so that local and deployed dial by the same
mechanism (SC-012). Removing `LIVEKIT_SIP_OUTBOUND_TRUNK` from the route's
`DevSuppliedEnvironment` is what switches that off, since the creation block is
gated on it; the block then has no caller and is deleted.

Local development still creates the **inbound** trunk and the dispatch rule,
still injects `LIVEKIT_SIP_INBOUND_TRUNK`, and still undoes what it created on
every exit path. None of that changes.

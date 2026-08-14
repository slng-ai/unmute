# Contract: every refusal

Principle II requires a failure to name the fix, and requires a **moved** field to
report its new form rather than a bare "unknown field". These are the messages
that contract commits us to. Prefix format is `Location`'s existing `file:line`.

Wording may be edited during implementation; the **facts each message carries**
may not be dropped, because each one is what turns a refusal into an edit.

---

## 1. Moved fields (FR-007)

Each must name: the file, the line, the key, its value, and the new home.

```
targets.yaml:17: target "livekit" declares transport: sip, which now belongs in
  connections/twilio_sip.yaml. A target names one connection and the connection
  declares the route.

targets.yaml:18: target "livekit" declares carrier: twilio, which now belongs in
  connections/twilio_sip.yaml alongside its transport.

targets.yaml:38: target "livekit" declares destinations, which now belong at the
  top level of agent.yaml. A destination is who this agent escalates to, which is
  the same desk whichever carrier reaches it.
```

Naming the connection file in the first two requires the target to have a
`connection:`. Where it does not, the message says
`in the connection file this target should name`.

**Keyed on the field, not the provider.** `carrier` is rejected on every target,
`vapi` and `deepgram` included. The four capability rows that condition on their
carrier lose the condition in the same change (FR-001a), so nothing is left
demanding a value an author can no longer write.

There is no deprecation path. The repository is pre-release with no external
packages: one shape loads, one shape is tested, one shape is documented.

---

## 2. Removed field (FR-006)

```
connections/twilio_sip.yaml:1: kind is no longer written in a connection. Every
  transport in the catalog is telephony, so transport: sip already says it.
```

---

## 3. Unknown key (FR-010)

Strict decode already reports file, line, and the key. No change beyond making
sure a moved or removed key is caught by §1 and §2 first, so an author never gets
the bare form for a key we moved ourselves.

---

## 4. Unsupported route (FR-011, FR-011a)

Must name: the pair, the provider, and the routes that provider actually supports.
The list contains **only selectable routes** — rows with a `route` feature — so a
suggestion never leads to a second refusal.

```
connections/twilio_sip.yaml:1: transport "sip" with carrier "twilio" is not a
  route for provider pipecat. pipecat supports: carrier-websocket with twilio,
  telnyx, or plivo; cloud-websocket with twilio; daily-sip with twilio, or with
  no carrier for a Daily-provisioned number.
```

**The check is not a flat triple lookup.** A connection with no `carrier` is valid
only where the transport has a carrier-less form, which today is `daily-sip`
alone — and that form has no row in the capability table at all (research R10).
So:

1. carrier present → the triple must resolve to a selectable route;
2. carrier absent → the transport must be one with a documented carrier-less form.

Implementing this as a plain lookup makes `pipecat-human-transfer-daily` fail with
the message above, which reads like a broken example rather than a missing branch.

---

## 5. Environment keys (FR-012)

Both directions already exist and already carry the accepted set. The addition is
conditional requiredness: where a key is required only because the package does
something, the message says which behaviour.

```
connections/twilio_voice.yaml:3: connection "twilio_voice" requires environment
  key "account_sid" for route (pipecat, cloud-websocket, twilio), because this
  package places or redirects calls. A package that only receives calls on this
  route needs no connection environment at all.

connections/twilio_voice.yaml:5: connection "twilio_voice" environment key
  "sip_address" is not accepted by route (pipecat, cloud-websocket, twilio); it
  accepts account_sid, auth_token, from_number.
```

---

## 6. Environment value shape (FR-013)

```
connections/twilio_voice.yaml:4: connection "twilio_voice" environment value
  "11LABS_API_KEY" is not a valid environment variable name: use letters, digits,
  and underscores, and do not start with a digit. A deployment platform exports
  secrets through a shell, so this name would be missing at runtime with no error
  of its own.
```

The trailing sentence is why this is a compile error rather than a lint: the
failure it prevents is silent, and appears only on a live call.

---

## 7. Connection resolution (FR-014, FR-015, FR-016)

```
targets.yaml:14: target "livekit" names connection "twilio_slp", which this
  package does not define. It defines: twilio_sip, twilio_voice.

targets.yaml:14: target "livekit" has a telephony channel and names no
  connection. Add connection: <name> and a connections/<name>.yaml declaring the
  route.

targets.yaml:14: target "pipecat" names connection "twilio_voice", but nothing in
  this package uses a phone route: declare a channels.phone entry, or a control
  that dials a person.
```

And the one warning in this file, which does not stop the build:

```
warning: connections/twilio_sip.yaml declares a route no target names. Nothing
  uses it.
```

---

## 8. Transfers (FR-016a)

Must name: the connection, the transport it declares, and the transport the
transfer needs. Today the message can only say a connection is missing, because
the transport it needed was written in a different file.

```
agent.yaml:43: control "escalate" is a warm transfer, which dials the destination
  itself and needs a connection with transport: sip. Target "pipecat" names
  connection "twilio_voice", which declares transport: cloud-websocket. Warm
  transfer compiles on (livekit, sip) trunks today.
```

The existing route-capability refusals in
`internal/target/telephony.go:478-526` keep their wording. This message is the
one that gained information from the new shape.

---

## 9. Destinations (FR-004d)

```
agent.yaml:12: destination "billing_line" is "+14155550123", a literal number.
  agent.yaml is the portable half of a package, so a destination names an
  environment variable holding the number: billing_line: BILLING_PHONE_NUMBER.
```

---

## 10. Secrets cross-check (FR-005e)

A warning, at the severity `docs/SCHEMA.md` §4.12 already sets for every
environment name. It reuses the existing site-labelled report.

```
warning: agent.yaml secrets does not declare SIP_TRUNK_HOSTNAME, referenced by
  connections/twilio_sip.yaml environment sip_address.

warning: agent.yaml secrets does not declare BILLING_PHONE_NUMBER, referenced by
  agent.yaml destinations billing_line.
```

Exit code stays 0. The consequence is accepted and stated in the docs: a package
missing a name compiles green and fails on its first phone call. Raising this
check to an error, for every name rather than telephony names alone, is deferred.

---

## Severity summary

| Class | Severity |
|---|---|
| §1 moved field, §2 removed field, §3 unknown key | error |
| §4 unsupported route | error |
| §5 environment keys, §6 value shape | error |
| §7 missing connection, unused-by-package connection | error |
| §8 transfer the route cannot emit | error |
| §9 literal destination | error |
| §7 connection no target names | **warning, exit 0** |
| §10 secrets cross-check | **warning, exit 0** |

Every error is raised before any artifact is written. No error may be downgraded
to a partial compile.

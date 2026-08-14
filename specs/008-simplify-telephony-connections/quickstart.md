# Quickstart: proving this feature works

Runnable checks, in the order that fails fastest. Every one of them runs with no
credentials except the two that say otherwise.

Run from the repository root.

---

## Prerequisites

- Go 1.24 (`go.mod` pins it)
- `make` for the shortcuts below
- Nothing else. No Python, no Docker, no carrier account, for everything except
  the two live checks at the end.

---

## 1. The suite, which is the main gate

```sh
make test          # go test ./... — L1 unit, L2 command, L3 golden
make lint          # golangci-lint
make fmt           # gofmt
```

Expected: green, with **exactly two golden files changed** across the whole
feature — the `.env.example` for `livekit-human-transfer` and
`pipecat-human-transfer-twilio`, which switch to the labelled two-list form
because those packages gain a `secrets:` block (research R5). Any third golden
that moves is a bug: the resolved IR shape is unchanged on purpose, so no
generator output should follow the authoring change.

---

## 2. Every example still compiles to the same thing

```sh
go run . validate examples/twilio-telephony-hello
go run . validate examples/livekit-human-transfer
go run . validate examples/pipecat-human-transfer-twilio
go run . validate examples/outbound-reminder
go run . validate examples/pipecat-human-transfer-daily
```

Expected: one result line per target, exit 0, no warnings. Then:

```sh
go run . compile examples/twilio-telephony-hello
```

Expected: `build/pipecat/` and `build/livekit/` written, the same file set as
before the change, and `compile-report.json` still naming
`(provider, transport, carrier)` on the target — the resolved surface did not move
(FR-003a).

**Grep proof of the authoring change**, all five expected to print nothing:

```sh
grep -rn "transport:\|carrier:\|destinations:" examples/*/targets.yaml
grep -rn "^kind:" examples/*/connections/*.yaml
```

---

## 3. The browser still works with no phone credentials

This is User Story 2, and it is the check most likely to be broken by accident.

```sh
env -u TWILIO_ACCOUNT_SID -u TWILIO_AUTH_TOKEN -u TWILIO_PHONE_NUMBER \
    -u SIP_TRUNK_HOSTNAME -u SIP_AUTH_USERNAME -u SIP_AUTH_PASSWORD \
    -u SIP_FROM_NUMBER \
    go run . dev examples/twilio-telephony-hello
```

Expected: the browser session starts. No carrier variable is required, and none is
reported missing.

Then the phone-only package, which declares **no `channels.web`** (FR-018a):

```sh
go run . dev examples/livekit-human-transfer
```

Expected: the browser session starts anyway. A `channels.web` entry is not a
precondition for local browser testing.

Contrast, to prove the credentials are still required where they matter:

```sh
go run . dev examples/twilio-telephony-hello --telephony --target livekit
```

Expected: every missing variable reported **by name**, before anything starts.

---

## 4. Each refusal, from a hand-written package

Copy an example into a scratch directory and break one thing at a time.
`contracts/errors.md` has the full expected text; this is the checklist.

| Break | Expect |
|---|---|
| put `transport: sip` back on a target | names the key and the connection file it belongs in |
| put `destinations:` back on a target | names the key and `agent.yaml` |
| add `kind: telephony` to a connection | says the transport line already carries it |
| add a nonsense key to a connection | file, line, key |
| set `transport: sip` on a `pipecat` target's connection | names the pair, the provider, and the routes that provider supports — **and no Exotel row** (FR-011a) |
| drop `sip_username` from a SIP connection | names the key and the accepted set |
| set a value to `11LABS_API_KEY` | names the value and why a leading digit breaks at export |
| point `connection:` at a name that does not exist | lists the connections the package defines |
| delete `channels.phone` while keeping a connection | says a channel or a dialing control is missing |
| add a second connection file nothing names | **warning on stderr, exit 0**, build still succeeds |
| ask for a warm transfer on a `cloud-websocket` connection | names the connection, its transport, and the transport warm needs |
| write `billing_line: "+14155550123"` in `agent.yaml` | names the literal and shows the env-name form |
| remove a name from `secrets:` that a connection uses | **warning on stderr, exit 0** |

Two rows are warnings that exit 0 — the unnamed connection file, and the name
missing from `secrets:`. Everything else exits 1 with nothing written.

---

## 5. Documentation agreement

```sh
go test ./internal/generate/ -run 'TestExample|TestV11|TestV16'
```

Expected green, and specifically:

- the rewritten transport check reads transports from **connections**, not from
  targets, so it still fails when a README and a package disagree (FR-027). Prove
  it is not passing vacuously: change a transport in one connection file without
  touching its README and confirm the test fails.
- the new check (FR-027a) fails when a name in a generated `.env.example` appears
  in neither the example's README nor
  `docs/user/learn/07-phone-calls.md`. Prove it by deleting one name from a README.

Manual read, because prose rots and no test holds it:

- `docs/user/reference/connections.md` exists, is in `docs/user/_sidebar.md`, and
  answers the where-does-each-value-go question in one table.
- `docs/user/learn/07-phone-calls.md` runs route choice → connection → secrets →
  destinations → transfers → browser before phone.
- No README names a variable the reader never sets. In particular the
  `UNMUTE_OUTBOUND_TOKEN` / `UNMUTE_PUBLIC_URL` / `REDIS_URL` paragraph is gone
  from `twilio-telephony-hello/README.md` (FR-005h).

---

## 6. The emitted Python still runs

```sh
make smoke         # build tag smoke, needs Python
```

Opt-in, never in the default suite or the PR gate. It should be unaffected — the
resolved IR did not change, so no template input changed — which is exactly why
running it is worth the minute.

---

## 7. Live calls, credentials required

Not a gate, and not automatable here. Do it before claiming a route works.

```sh
go run . dev examples/twilio-telephony-hello --telephony --target pipecat
# call the printed number

go run . dev examples/twilio-telephony-hello --telephony --target livekit --to +15551234567
# answer the phone
```

This feature changes no route behaviour, so a failure here is either a
pre-existing route issue or a mistake in the migration — and the second is worth
finding before merge. Dated live-call evidence belongs in `docs/TRANSFERS.md`, and
no `provisional` tag moves for this feature.

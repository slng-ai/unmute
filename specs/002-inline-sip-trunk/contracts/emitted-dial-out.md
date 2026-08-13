# Contract: the emitted dial-out code

**Feature**: [../spec.md](../spec.md) | **Plan**: [../plan.md](../plan.md) | **Date**: 2026-08-12

What `unmute compile` must produce in `build/<target>/agent.py` for a
`livekit/sip/<carrier>` target. This is the contract the tests assert against.
Every LiveKit surface named here was verified on 2026-08-12 against
`livekit-agents 1.6.9` as installed, not against documentation alone; see
[../research.md](../research.md).

## C1. One helper, three callers

The project MUST emit exactly one place where the carrier's trunk settings are
read, and all three dial-out sites MUST call it. This is what makes FR-002
structural instead of a promise: a credential rotation cannot fix one path and
break another when there is one path to fix.

Emitted at module level, beside the existing `_refer_uri` helper that cold
transfer uses:

```python
def _sip_trunk() -> api.SIPOutboundConfig:
    """The carrier's own trunk settings, sent with each dial.

    No stored outbound trunk is used: LiveKit accepts the trunk inline, and
    these four values are what the Connection already declares. Verified
    against livekit-agents 1.6.9 on 2026-08-12.
    """
    return api.SIPOutboundConfig(
        hostname=os.environ["SIP_TRUNK_HOSTNAME"],
        auth_username=os.environ["SIP_AUTH_USERNAME"],
        auth_password=os.environ["SIP_AUTH_PASSWORD"],
    )
```

The three names come from `livekitTelephony.SIPAddressEnv`, `SIPUsernameEnv` and
`SIPPasswordEnv`, so a package that authored carrier-prefixed names gets those
instead. The compiler MUST NOT contain any of the three literals.

**Emitted when**: `.Telephony` is present, its transport is `sip`, and it has
outbound or warm. Never on the connector route (FR-008), which has no transfer
features and whose outbound is placed by the bridge.

**Not emitted when**: the target has no telephony, or the route is the
connector, or the package has neither an outbound channel nor a warm transfer. A
cold-transfer-only package needs no trunk of any kind and MUST NOT get this
helper.

## C2. The warm transfer

```python
result = await WarmTransferTask(
    sip_call_to=os.environ["SUPERVISOR_PHONE_NUMBER"],
    sip_connection=_sip_trunk(),
    sip_number=os.environ["SIP_FROM_NUMBER"],
    chat_ctx=self.chat_ctx,
    extra_instructions="...",   # when briefing is authored
    ringing_timeout=25,         # when ring_timeout is authored
)
```

MUST hold:

- `sip_connection=_sip_trunk()` is present.
- `sip_number` is present and reads the Connection's number. It is **not**
  optional: with inline configuration the prebuilt's fallback chain ends at `""`,
  which the SIP service rejects, so omitting it turns a clear failure into a
  confusing one (FR-003).
- **No** `sip_trunk_id` argument appears anywhere in the file.
- `sip_call_to` is unchanged from today: `DialExpr`, which is a quoted literal or
  an `os.environ[...]` read depending on how the destination was authored.

## C3. The outbound call, at both sites

`agent.py.tmpl` places this call twice, once in the plain outbound path and once
in the variant that carries call-start variables. Both MUST change, identically.

```python
await ctx.api.sip.create_sip_participant(
    api.CreateSIPParticipantRequest(
        room_name=ctx.room.name,
        trunk=_sip_trunk(),
        sip_number=os.environ["SIP_FROM_NUMBER"],
        sip_call_to=phone_number,
        participant_identity="phone_user",
        wait_until_answered=True,
    )
)
```

MUST hold:

- The field is `trunk`, not `sip_trunk_id`. `CreateSIPParticipant` documents
  `trunk` as the inline configuration and says "when using this parameter,
  `sip_number` must also be set".
- `sip_number` is present at both sites, reading the same name as C1 and C2.
- `os.environ["LIVEKIT_SIP_OUTBOUND_TRUNK"]` appears **nowhere** in the emitted
  project.

A test MUST assert both sites, not one. They are duplicated in the template
today, and a change applied to one is exactly the drift FR-002 exists to prevent.

## C4. The import that must be in scope

`agent.py.tmpl:90` currently emits `from livekit import api` only for an outbound
channel or a cold transfer:

```gotemplate
{{if or .Outbound .HasColdTransfer}}from livekit import api{{if .HasColdTransfer}}, rtc{{end}}
```

The condition MUST widen so that a **warm transfer alone** also emits it.
Otherwise a package with one warm transfer, no outbound channel and no cold
transfer raises `NameError: name 'api' is not defined` on its first transfer.

`api.SIPOutboundConfig` is the correct spelling: it resolves on `livekit.api`,
and it is the form the prebuilt's own signature annotation uses
(`sip_connection: NotGivenOr[api.SIPOutboundConfig]`). No new import line is
added.

**This case has no fixture today.** `examples/human-transfer` has both a cold and
a warm transfer, so it never exercises it. A warm-only fixture is required, and
it is the same class of gap that let the missing `httpx` dependency reach a live
deploy in feature 001.

## C5. What is no longer emitted

| Artifact | Rule |
|---|---|
| `sip-outbound-trunk.json` | MUST NOT be written, for any feature combination |
| `LIVEKIT_SIP_OUTBOUND_TRUNK` | MUST NOT appear in `.env.example`, the generated startup check, the compile report, the generated README, or the emitted Compose file |
| `lk sip outbound create` | MUST NOT appear in the generated README |

## C6. What must not change

| Artifact | Rule |
|---|---|
| `sip-inbound-trunk.json` | byte for byte identical |
| `sip-dispatch-rule.json` | byte for byte identical |
| `LIVEKIT_SIP_INBOUND_TRUNK` | still required for an inbound route, still dev-supplied |
| Cold transfer | `TransferSIPParticipant` with a `tel:`/`sip:` URI, and the `_refer_uri` helper, both unchanged. A cold-transfer-only package still needs no trunk of any kind. |
| The connector route | no change of any kind |
| The Pipecat driver | no change of any kind |

## C7. Behaviour a test must pin rather than trust

Passing `sip_connection` makes the prebuilt ignore
`LIVEKIT_SIP_OUTBOUND_TRUNK`. This is upstream's own behaviour, with a comment
saying so:

```python
elif self._sip_connection is not None:
    # explicit sip_connection: don't override with the env var trunk
    self._sip_trunk_id = None
```

So FR-004's "a deployment that still sets that name MUST be unaffected by it"
and User Story 3 scenario 2 need **no code**. They need a test that sets the
variable to a value that could not possibly work and still expects an inline
dial, so that an upstream change of mind fails here rather than on a live call.

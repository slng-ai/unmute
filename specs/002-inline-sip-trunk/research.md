# Phase 0 Research: Dial out with the carrier's own SIP credentials

**Feature**: [spec.md](./spec.md) | **Date**: 2026-08-12

Every platform fact below was checked on 2026-08-12, either against live
documentation or against the installed package inside the image this repository
actually builds (`unmute-lk-fixed:latest`, `livekit-agents 1.6.9`). Reading the
installed source rather than the docs matters here: two of the findings are
things the documentation does not state.

The spec's [Verified platform contract](./spec.md#verified-platform-contract-source-of-truth-for-this-feature)
holds the same facts in one table and stays the source of truth. This file holds
the decisions those facts produced, plus three things found while planning that
the spec did not know about.

## R1. What the warm-transfer prebuilt actually accepts

**Decision**: pass `sip_connection` and `sip_number` to the prebuilt. Pass no
trunk id, and register no trunk environment name.

**Rationale**: read from
`livekit/agents/beta/workflows/warm_transfer.py` in the installed package.
The constructor signature carries `sip_call_to`, `sip_trunk_id`,
`sip_connection`, `sip_number`, `sip_headers`, `dtmf`, `ringing_timeout`,
`hold_audio`, `instructions`, `chat_ctx`, `extra_instructions` and more, all
defaulting to `NOT_GIVEN`. Its resolution order is explicit:

```python
if is_given(sip_trunk_id):
    self._sip_trunk_id = sip_trunk_id
elif self._sip_connection is not None:
    # explicit sip_connection: don't override with the env var trunk
    self._sip_trunk_id = None
else:
    self._sip_trunk_id = os.getenv("LIVEKIT_SIP_OUTBOUND_TRUNK", None)
if self._sip_trunk_id is None and self._sip_connection is None:
    raise ValueError(
        "`LIVEKIT_SIP_OUTBOUND_TRUNK` environment variable, `sip_trunk_id`,"
        " or `sip_connection` must be set"
    )
```

**This is the finding that matters most for the plan.** Passing
`sip_connection` explicitly makes the prebuilt ignore
`LIVEKIT_SIP_OUTBOUND_TRUNK` **by upstream design**, with a comment saying so.
So FR-004's "a deployment that still sets that name MUST be unaffected by it"
and User Story 3's leftover-name scenario need **no code at all**. They need a
test that pins the behaviour, and the test can set the variable to a nonsense
value and still expect an inline dial.

The same file shows how it dials, which confirms the rules of the underlying
API reach the prebuilt unchanged:

```python
if self._sip_connection is not None:
    sip_request.trunk.CopyFrom(self._sip_connection)
await job_ctx.api.sip.create_sip_participant(sip_request)
```

**Alternatives considered**: keeping `sip_trunk_id` as a documented override,
rejected in the spec's Clarifications because it buys a branch in emitted code
and two ways for one package to dial. Note that the upstream order would have
made that override work with no code, since an explicit id wins over an explicit
connection. It was rejected for being a second shape, not for being expensive.

## R2. Where `SIPOutboundConfig` comes from, and the import trap

**Decision**: write `api.SIPOutboundConfig(...)`, reusing the existing
`from livekit import api`. Add no import line. **Widen the condition that emits
that import** so a warm transfer alone is enough to trigger it.

**Rationale**: checked all three plausible module paths in the installed image.

| Module | Exports `SIPOutboundConfig` |
|---|---|
| `livekit.api` | yes |
| `livekit.protocol.sip` | yes |
| `livekit.agents` | no |

The prebuilt's own type annotation is
`sip_connection: NotGivenOr[api.SIPOutboundConfig]`, so `api.SIPOutboundConfig`
is the form upstream itself uses. The generated `agent.py` already imports `api`
for `api.CreateSIPParticipantRequest`, so the value is in scope for free.

**The trap**: it is in scope only sometimes.
`templates/livekit_v1/agent.py.tmpl:90` reads

```gotemplate
{{if or .Outbound .HasColdTransfer}}from livekit import api{{if .HasColdTransfer}}, rtc{{end}}
```

A package with a **warm transfer and nothing else** (no outbound channel, no
cold transfer) does not import `api` today, because it never needed it: it
passed no trunk argument at all. Give that package an
`api.SIPOutboundConfig(...)` and it raises `NameError` on the first transfer,
which is the same failure the operator just reported, wearing a different hat.

`examples/human-transfer` hides this, because it has a cold transfer as well as
a warm one, so `HasColdTransfer` is true and the import is emitted. This is the
same blind spot that let the `httpx` break through in feature 001: only a
minimal shape is affected and no golden covers that shape. It needs a fixture,
not just a fix.

**Alternatives considered**: importing `SIPOutboundConfig` from
`livekit.protocol.sip` on its own line, which would sidestep the gating
question. Rejected: it adds an import for a symbol already reachable, and it
diverges from the name upstream uses in its own signature, which is the form a
reader of the generated file will recognise.

## R3. The from-number is not optional with inline configuration

**Decision**: always pass `sip_number` from the Connection's number, on all
three dial-out sites.

**Rationale**: two sources agree, and the installed source explains the failure
mode. `CreateSIPParticipant` documents `sip_number` as "Required if using an
inline trunk configuration. If `sip_trunk_id` is set, this defaults to the
number associated with the stored trunk", and the inline section says why:
"inline trunk configuration has no `numbers[]` field to pick a default from".

The prebuilt's own fallback chain is
`sip_number` argument, then `LIVEKIT_SIP_NUMBER`, then `""`. So a warm transfer
with inline configuration and no number passes an **empty** From number to the
SIP service and fails there rather than in the constructor. Passing the
Connection's number explicitly is what turns that into a case that cannot
happen.

This closes the item the spec's requirements checklist flagged as "worth
confirming before implementation rather than after".

**Alternatives considered**: relying on `LIVEKIT_SIP_NUMBER`. Rejected: it is a
name nothing in this repository emits (verified, zero occurrences), and it would
mean a second name for a number the Connection already declares.

## R4. Region pinning and transport are reachable inline, and stay out of scope

**Decision**: emit exactly four values. No destination country, no transport.

**Rationale**: `SIPOutboundConfig` carries `hostname`, `destination_country`,
`transport`, `auth_username`, `auth_password`, two header maps and a `from_host`.
The region-pinning page states destination country "works with both inline trunk
configuration and stored outbound trunks", and that if the code "doesn't match
any supported region, the parameter has no effect and calls are routed using
default behavior". The same page states outbound calls otherwise "originate from
the same region where the `CreateSIPParticipant` API call is made", which for a
deployed agent is the region `deployment_region` already chose.

So the parameter is optional, and for our shape it is close to redundant. This
corrected a wrong claim in the spec, which had named region pinning as the one
capability only a stored trunk could express. That claim was the stated reason
for keeping the stored path, and removing it is what made dropping the path
answerable.

**Alternatives considered**: a new optional Connection key for the country.
Rejected in the spec's Clarifications: it would be a new authoring field, which
FR-005 forbids, and a compiler-known environment name, which FR-015 forbids.
Reopening it needs a real compliance requirement, which is recorded as the
trigger.

## R5. What an operator actually gives up

**Decision**: state the loss plainly in `docs/TRANSFERS.md` and the generated
README rather than implying nothing changed.

**Rationale**: comparing `CreateSIPOutboundTrunk` with `SIPOutboundConfig`, a
stored trunk holds three things inline configuration does not: a **list** of
caller-ID numbers the platform picks from per call, trunk **metadata** copied
onto every SIP participant it creates, and a single place to change the host or
credentials without touching a deployment. None of the three is used by anything
this repository emits today, which is why the trade is acceptable, and all three
are worth naming so the trade is visible.

One thing deliberately **not** claimed either way: the documentation warns that
trunks are cached and reused and that creating one per call "can degrade
reliability at scale". It does not say whether inline configuration takes part
in that caching. Inline configuration is not a trunk object, so the warning does
not obviously apply, but the honest position is that this is unknown, and no
repository document may assert either side of it.

## R6. Blast radius, and which fixtures keep the old names on purpose

**Decision**: rename in the shipped example, the scaffold, and the documents.
Leave most **test fixtures** on the carrier-prefixed names, deliberately.

**Rationale**: `LIVEKIT_SIP_OUTBOUND_TRUNK` appears in 18 non-spec files;
`TWILIO_SIP_ADDRESS` and friends appear in 15. The two sets overlap heavily.
Grouped by what each site is:

| Group | Files | Change |
|---|---|---|
| Capability table | `internal/target/telephony.go` | trunk name leaves runtime and dev-supplied env; manual steps drop the outbound trunk |
| Emitter | `internal/generate/livekit_v1.go`, `livekit_v1_build.go` | stop emitting the trunk input file; delete three `env.add` sites |
| Template | `templates/livekit_v1/agent.py.tmpl`, `README.md.tmpl` | inline config at three dial sites; import condition widened; README steps and migration note |
| Local dev | `internal/cli/dev_livekit_sip.go` | the outbound-trunk block goes dead and is deleted |
| Golden | `testdata/golden/livekit_v1_telephony_compose.yaml` | regenerate and read |
| Shipped example | `examples/human-transfer/connections/twilio_sip.yaml`, its `README.md`, `targets.yaml` header comment | renamed to the neutral names |
| Documents | `docs/TRANSFERS.md`, `docs/TELEPHONY.md`, `docs/SCHEMA.md`, `docs/user/learn/07-phone-calls.md`, `docs/user/targets/livekit.md`, `README.md` | renamed, trunk step removed, N33 amendment |
| Docs sync test | `internal/target/user_docs_test.go` | expects `TWILIO_SIP_ADDRESS`, `TELNYX_SIP_ADDRESS`, `PLIVO_SIP_ADDRESS` in documents; must follow the rename |
| Test fixtures | `internal/ir/build_test.go`, `internal/cli/dev_test.go`, `dev_livekit_sip_test.go`, `dev_compose_smoke_test.go`, `internal/generate/livekit_v1_test.go`, `livekit_deploy_test.go` | **mostly unchanged, on purpose** |

Leaving the test fixtures on `TWILIO_SIP_ADDRESS` is not laziness, it is the
evidence for SC-010: the compiler must keep working with whatever names a
Connection declares, and a suite that renamed everything would no longer prove
it. At least one fixture must use the neutral names too, so both spellings are
covered.

The **three carriers collapse onto one set of names**, which is a visible
consequence: today the documents show `TWILIO_SIP_ADDRESS`,
`TELNYX_SIP_ADDRESS` and `PLIVO_SIP_ADDRESS` as three different things, and
afterwards all three carriers use `SIP_TRUNK_HOSTNAME`. That is correct, because
they are one route with one shape, and it is exactly what the docs sync test
will catch if it is not done everywhere.

**Alternatives considered**: renaming the Connection **keys** as well
(`sip_address` to `sip_hostname`). Rejected: FR-010 forbids breaking a written
package, and a key rename does exactly that for no gain, since the key is
internal vocabulary and the name is what an operator types.

## R7. One risk left open, to be closed by a test not by reasoning

**Question**: can a package declare a warm transfer with **no** telephony route
at all? If it can, that package has no Connection, so it has no SIP values, and
inline configuration is impossible.

**What the code suggests**: `internal/ir/validate.go:1043-1047` resolves the
warm-transfer capability against `resolved.Transport` and `resolved.Carrier`,
which are empty without a telephony plan, so the capability lookup should find
no route and deny. `validateChannels` separately requires a resolved Connection
plan for any telephony channel. So the shape looks impossible already.

**Why it is still listed**: "looks impossible" is how the phantom `REDIS_URL`
requirement survived a whole test suite in feature 001. The plan carries a task
to assert it, and if the assertion fails, the fix is a gated validation error
naming the missing SIP values, which is what FR-006 asks for anyway.

## R8. A finding I got wrong, and the document that already had it right

**Corrected 2026-08-12, during implementation.** Everything below replaces the
earlier version of this section, which reported a document-versus-code conflict
that does not exist.

**What I claimed**: `docs/SCHEMA.md` N28 says human transfer works on the LiveKit
Twilio connector route and that an agreement test pins it, while
`internal/target/telephony.go` denies both transfer features there, so the
document and the code disagreed and N28 needed amending.

**What is actually true**: **N31 (2026-08-11) already supersedes N28**, says so in
its first sentence, and agrees with the capability table exactly. I read N28 and
did not read N31, which sits two entries above it in the same list. There was no
conflict and no Constitution IV violation.

N31 also corrects the *substance* of my finding. I concluded from the missing
bridge code, the missing agreement test and the "no transfers **yet**" comment
that the connector transfer path had been specified and never built. N31 records
what happened:

> The connector and carrier-websocket transfers N28 described **were built,
> live-tested, and then deleted**: every custom design made the generated process
> own the call's audio path, and each live test found a new lifecycle bug in that
> ownership (briefing-pipeline leaks, serializer auto-hangup fights, announcement
> races).

So the absence of code was the result of a deliberate deletion after live
testing, not of unfinished work, and the deleted designs are in git history. The
evidence I gathered was consistent with both readings and I picked the wrong one.

**What this feature still owes the document**: one sentence. N28 explains the
connector route by saying `WarmTransferTask` "acts on a SIP participant reached
through an outbound trunk". After this change a SIP participant is still needed
and a stored trunk is not, so N33 retires that half. N28's conclusion needs
nothing, because N31 already superseded it, and superseded amendments stay as
history by design.

**What it does not owe**: no capability row changes, no test asserting that
documents agree with the route table, and no amendment recording a rejection that
N31 recorded five weeks earlier and in more detail than I could have.

**The lesson worth keeping**: an amendment list is read forwards. Quoting one
entry without checking whether a later entry supersedes it is how a
non-contradiction becomes a finding, and it cost a requirement (FR-019) and a
success criterion (SC-013) that both had to be withdrawn.

## Summary of decisions

| # | Decision |
|---|---|
| R1 | Pass `sip_connection` plus `sip_number`; the leftover trunk variable is ignored upstream, so pin it with a test and write no code |
| R2 | Use `api.SIPOutboundConfig`; widen the `from livekit import api` condition to cover a warm-only package |
| R3 | Always pass the Connection's number as `sip_number` on all three dial sites |
| R4 | Four values only; region and transport stay out of scope with the reason recorded |
| R5 | Name the three things a stored trunk kept; claim nothing about inline and trunk caching |
| R6 | Rename example, scaffold and documents; keep most test fixtures on the old names as SC-010 evidence |
| R7 | Assert that a warm transfer cannot exist without a resolved SIP route, rather than assuming it |
| R8 | **Withdrawn.** N31 already supersedes N28 and agrees with the capability table, and it records that the connector transfers were built, live-tested and deleted rather than never built. N33 retires one sentence of N28's trunk wording; nothing else is owed. |

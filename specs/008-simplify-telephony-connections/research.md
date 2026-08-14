# Phase 0 research: what the code says this change costs

Every decision below was reached by reading the code that would have to change,
not by reasoning about the spec alone. Line references are to the tree at
2026-08-14.

---

## R1. Keep the resolved IR shape identical, and the change stays small

**Decision**: `ir.Target` keeps `Transport`, `Carrier`, and `Destinations`.
`ir.buildTarget` fills them from their new homes instead of from `spec.Target`.

**Rationale**: `Transport` and `Carrier` are read in roughly twenty places outside
`internal/spec` — `internal/ir/validate.go` (7 sites), both generators
(`pipecat_v1_build.go`, `livekit_v1.go`, `livekit_v1_build.go`),
`internal/cli/dev.go`, `dev_telephony.go`, `dev_cloud_websocket.go`, and
`internal/target/telephony.go`. `Destinations` is read by both generators
(`pipecat_v1_build.go:980`, `livekit_v1_build.go:772`), `ir/build.go:613`, and
`internal/tui/maintain.go:263`. If the resolved struct changed shape, every one of
those is an edit and every golden file moves with them.

Holding `ir.Target` still reduces the whole feature to two authoring structs plus
one builder function. FR-003a already mandates this for `transport` and `carrier`.

**Extension the spec did not spell out**: the same rule is applied to
`destinations`. FR-004a moves it in *authoring*; the resolved target keeps a
`Destinations` map, copied from the agent by `buildTarget`. The spec's reasoning
for FR-003a — "working the route out is what the compiler did" — applies word for
word to resolving a destination map onto a target, and the alternative buys
nothing but churn.

**Alternatives considered**: mirroring the authoring shape in the IR (rejected in
FR-003a for the golden-file cost); putting `Destinations` on `ir.Agent` only
(rejected: four call sites already reach it through the target, and a target is
where a generator has its context).

---

## R2. `ir.buildTarget` is where the guards live, and three of them change

**Decision**: all route-shape validation stays in `buildTarget`
(`internal/ir/build.go:770-860`). No new validation site is created.

**What happens to each guard**:

| Guard today | After |
|---|---|
| `connection` names a connection the package defines (build.go:782) | **Unchanged.** Still the first check, still `missing(...)`. |
| Telephony channel but no connection, with the cloud-websocket carve-out for receive-only packages (build.go:792-807) | **Kept and re-pointed.** The route now comes from the connection, so "which route am I on" is answered before the check rather than beside it. The cloud-websocket message keeps its three-key explanation. |
| `connection` set but no telephony channel (build.go:809) | **Widens.** FR-016 makes the test "used by a telephony channel *or* a control that dials", because the Daily-provisioned route is dial-out only and now carries a connection. |
| `daily-sip` + telephony + no carrier (build.go:823-828) | **Collapses.** Unreachable: the carrier is declared in the connection, so a target cannot name a route and omit its carrier. |
| `daily-sip` + carrier + no telephony channel (build.go:829-835) | **Collapses.** Same reason, and this was the silent-downgrade guard that made `carrier` mean something. The condition it protected against cannot be expressed any more, which is the better fix. |

**Rationale**: two of the five guards exist only because the route was split across
two files. Deleting them is the measurable part of "the authoring surface got
simpler" — the invariant survives by construction rather than by a check.

**Risk noted for tasks**: deleting a guard deletes its test. Each deleted guard
needs its test replaced by one proving the condition is now unrepresentable, not
merely removed.

---

## R3. `validateTelephonyEnvironment` needs no structural change

**Decision**: keep `internal/ir/build.go:885-915` as-is in shape. It already
resolves `targetcap.TelephonyEnvironment(key)` and checks the connection's key set
both ways, with the accepted list in the message (FR-012 is already satisfied).

**Rationale**: the function takes a resolved `*TelephonyPlan`, and the plan's
`Key` is built the same way after the change. Its `path` is already
`connections/<name>.yaml`, so its errors already point at the right file.

**One addition**: FR-012's conditional wording. The message must say *which
behaviour* makes a key required on the cloud-websocket route. That conditionality
lives in `buildTarget` (`packagePlacesCalls`, build.go:875) and only the message
needs it, not the logic.

**Noted duplication, out of scope**: `internal/generate/pipecat_v1_build.go:253`
(`pipecatConnectionVocabulary`) performs the same connection-key check a second
time, against the same table. Two homes for one check is a Principle III smell,
but it predates this feature and merging them is its own change. Recorded so the
next reader does not think this feature introduced it.

---

## R4. The `secrets:` cross-check already exists and already warns

**Decision**: FR-005a and FR-005e are implemented by deleting one exemption and
adding two loops to `referencedEnvNames` (`internal/ir/validate.go:1241`).

**Evidence**: the function's own comment reads *"Connection env names are exempt:
they are declared in their own file."* That sentence is the exemption FR-005a
removes. The function already collects `url_env`, `token_env`, `endpoint_env`,
tracing names, and `os.environ[...]` reads from local handler source. Adding
connection `environment:` values and `destinations` values makes them ordinary
members of the same set.

The severity FR-005e asks for is what this check already does: `docs/SCHEMA.md`
§4.12 states *"An env name the package references but never declares is a warning
on stderr, exit 0."* So FR-005e is not new behaviour — it is the existing rule
applied to names that used to be excluded from it.

**Rationale**: this is the smallest possible implementation of the author's
decision, and it inherits the handler-source scan, the site labels, and the
report wiring for free.

**Alternatives considered**: a separate telephony-specific cross-check with error
severity (rejected by FR-005e: two behaviours, no principle between them).

---

## R5. Two example golden files change, and the reason is not the route move

**Finding**: `secretEnvDocs` (`internal/generate/templates_lower.go:263`) returns
`nil, nil` when `agent.Secrets` is empty, which keeps the plain name list a
package with no `secrets:` block has always had. When `secrets:` is non-empty it
switches to the two-list form: declared names, then route-required names the
package never declared, labelled once as a group.

`livekit-human-transfer` and `pipecat-human-transfer-twilio` have **no `secrets:`
block today** and gain one under FR-005d. Their generated `.env.example` therefore
changes form — plain list to labelled two-list — even though nothing about their
route changed.

**Decision**: accept it, and reconcile SC-003 explicitly. SC-003 promises the same
route, the same required environment, and the same emitted *file set*. All three
hold: no file appears or disappears, and the required-name set is identical. The
*content* of two `.env.example` files changes, caused by FR-005d, not by the route
move. FR-003a's "no golden file may change shape for this reason" is about the
resolved-schema reason and is unaffected.

**Action for tasks**: regenerate those two goldens deliberately, in their own
commit, with the diff reviewed rather than `-update`-ed in passing. Any *other*
golden that moves is a bug in this feature.

---

## R6. Telling a real route from a placeholder

**Decision**: add one helper in `internal/target` that returns the routes an
author can select, and use it for both FR-011a consumers — the "routes this
provider supports" list in the refusal, and the TUI picker.

**Rationale**: `TelephonyRoutes()` contains rows with required environment and an
**empty `Features` map** — both Exotel entries (`telephony.go:158` and `:220`).
`ResolveTelephonyFeature` refuses anything whose route lacks
`TelephonyRouteSelected`, so those rows are undeclarable. Listing one in a "did
you mean" message sends an author into a second, different refusal.

The test is `Features[TelephonyRouteSelected]` being present. That is one
predicate, in the package that already owns the table, so Principle III's "single
capability rulebook" is respected and no second list is created.

**Alternatives considered**: deleting the Exotel rows (rejected — they carry a
real environment vocabulary and deleting them is a catalog decision, not a
authoring-shape one); hard-coding the selectable list at each call site (rejected
— that is the second table the constitution forbids).

---

## R7. How a moved field produces the right error instead of "unknown field"

**Decision**: keep the moved fields on the decode structs, tagged `json:"-"` so
schema derivation skips them, and reject them in a post-decode check that reports
through `pkg.Location`.

**Rationale**: two constraints pull against each other.

- Principle II: *"A field the schema moved MUST report the new form and quote the
  offending line, never a bare unknown field."* The loader already decodes with
  `yaml.UnmarshalWithOptions(content, out, yaml.Strict())` (`load.go:146`), and
  strict decode of a removed field produces exactly the bare message the
  constitution forbids.
- FR-001 and FR-006: the derived authoring schema must not carry the removed
  properties, and `internal/spec/authoring_surface_test.go` asserts absence by
  searching the marshalled schema.

`json:"-"` satisfies both: `Schema()` derives via `jsonschema.For[Package]`
(`schema.go:11`), which follows `encoding/json` conventions, so a `json:"-"` field
is absent from the derived schema while `yaml:"transport,omitempty"` still decodes
it. The pattern already exists in the same file — `Package.Root`, `Markdown`, and
`Handlers` are all `json:"-"` (`package.go:15-17`), and `ir.Tool.HandlerSource`
does the same.

**Verify during implementation**: that `jsonschema-go` honours `json:"-"` for
exclusion rather than emitting a property named `-`. The existing `json:"-"`
fields on `Package` are paired with `yaml:"-"`, so they prove the tag is accepted
but not that a decoded-but-hidden field behaves. The authoring-surface test is the
check: it fails loudly if the property survives into the schema.

**Alternatives considered**: scanning the raw YAML text for moved keys before
decode (rejected — a key in a comment or a nested map produces a false positive,
and this repository's whole argument for `goccy/go-yaml` is real positions from a
real parse); wrapping the strict-decode error and pattern-matching the field name
out of its message (rejected — parsing a dependency's error text is exactly the
fragile coupling that breaks on a library bump).

---

## R10. The carrier-less Daily connection has no row in the route table

**The finding**, and the one thing in this plan most likely to bite during
implementation. FR-009 gives the Daily-provisioned route a connection file
holding `transport: daily-sip` and nothing else. But `TelephonyRoutes()` has no
`(pipecat, daily-sip, "")` row — the only Daily row is
`add(Pipecat, "daily-sip", "twilio", ...)` at `telephony.go:242`, which is the
*carrier leg*, a different thing. The carrier-less form has never had a row, which
is exactly why `RouteAccountPrerequisites` was given "its own door"
(`telephony.go:449-456`, whose comment says the Daily route "has neither a
connection nor a telephony channel and so never gets a plan").

**Decision**: do not add a row, and do not let the new connection create a plan.

**Rationale**: the plan is guarded by `telephony && (raw.Connection != "" ||
cloudWebsocket)` where `telephony` is `hasTelephonyChannel(agent)`
(`build.go:846`). `pipecat-human-transfer-daily` declares `channels.web` only, so
`telephony` is false and **a connection alone does not produce a plan**. That is
already the behaviour we want, and it survives FR-009 untouched. The spec's
assumption that the route catalog is unchanged therefore holds.

**What this does change**: FR-011's route check cannot be a flat triple lookup for
every connection. A connection with no `carrier` is valid only where the transport
has a carrier-less form, which today is `daily-sip` and nothing else. The check
becomes:

1. carrier present → the triple must resolve to a **selectable** route (R6);
2. carrier absent → the transport must be one with a documented carrier-less form.

**Risk if missed**: implementing FR-011 as a plain triple lookup makes
`pipecat-human-transfer-daily` fail to validate with "not a route for provider
pipecat", which looks like a broken example rather than a missing branch. That
failure mode is why this is written down rather than left to be discovered.

**Alternatives considered**: adding a `(pipecat, daily-sip, "")` row to the
capability table (rejected — the row would need features, evidence, and a
`Verified` date, which is a catalog decision with live-call consequences and has
nothing to do with moving a field between files); requiring the Daily package to
declare a phone channel so it gets a plan (rejected — it receives no calls, and
the channel would drag `capacity` in per SCHEMA §4.10, which
`authoring_surface_test.go` explicitly rejected for this route).

---

## R11. `carrier` on driverless targets, measured rather than argued

**Superseded once, on the same day.** The first version of this section kept
`carrier` on `vapi` and `deepgram` targets because four capability rows condition
on it. Then the actual cost was measured, and the exception was not worth it. Both
versions are below, because the measurement is the useful part.

### What the experiment showed

Copy `internal/testdata/safe_core`, strip every `carrier: twilio` line, validate:

```
✗ deepgram (deepgram)   Deepgram transfer requires carrier Twilio in the generated bridge
✓ livekit  ✓ pipecat  ✓ vapi
```

**One** failure, on **one** provider, from **one** row. Vapi passes without a
carrier: `safe_core`'s `human_transfer` resolves to cold transfer, and Vapi's cold
transfer is unconditional. The other three carrier-conditioned rows — Vapi warm
transfer, Deepgram warm transfer, Deepgram voicemail detection — are not reachable
from any shipped package or fixture.

So the exception would have bought exactly one gated error, on a provider that
ships no driver, whose message names "the generated bridge" that does not exist.

**Decision**: remove `carrier` from every target (FR-001), and remove the
condition from all four rows (FR-001a), moving each Twilio requirement into a
comment for whoever builds that driver.

**Rationale**: after FR-001 no author can write a carrier anywhere those rows can
see it, so the condition can only ever produce a refusal naming an impossible fix.
Principle II wants a failure to name the fix; a fix that cannot be performed is
worse than no condition at all. Keeping the field alive for two unimplemented
providers would have made `carrier` mean two different things and given the
feature's headline rule an exception, for one error nobody can hit.

**Cost, stated rather than hidden**: `unmute validate` will report those four
controls as supported on `vapi` and `deepgram` without qualification. FR-001c
requires the SCHEMA amendment to say so.

**Verified unchanged**: `routedControls` (`table.go:474`) conditions on `carrier`
**and** `transport` for all four providers, covering `dtmf_send`, `dtmf_receive`,
`hold`, `ivr_navigation`. A driverless target has no transport today either, so
those already gate and removing `carrier` changes nothing for them.

### The withdrawn alternative, kept for the record

Keeping `carrier` legal on driverless targets. Rejected on the measurement above.
Also rejected, and worse: giving those targets a connection that declares
`carrier` and no `transport`. That looks tidiest and **actively regresses** —
`buildTarget` builds a telephony plan when `telephony && raw.Connection != ""`, so
a Vapi target with a connection resolves against key `(vapi, "", "twilio")`, which
has no row in `TelephonyRoutes()`, and `ResolveTelephonyFeature` returns
`Gated: unsupported telephony route`. The tidy shape breaks a package that works
today.

---

## R11-appendix. Where the four conditions live

`Table.Control` (`internal/target/table.go:169-187`) reads `carrier` straight off
the resolved target. Four rows condition on it for providers with no route:

| Control | Provider | Condition | Reachable today? |
|---|---|---|---|
| `cold_transfer` | Deepgram | `carrier == twilio` (`table.go:411`) | **yes** — `safe_core` hits it |
| `warm_transfer` | Vapi | `carrier == twilio` (`table.go:420`) | no |
| `warm_transfer` | Deepgram | `carrier == twilio` (`table.go:421`) | no |
| `voicemail_detection` | Deepgram | `carrier == twilio` (`table.go:427`) | no |

All four lose the condition under FR-001a; each keeps its Twilio requirement as a
comment on the row, so the fact survives for whoever builds the driver.

`internal/testdata/safe_core/targets.yaml` is the only package in the repository
with a `vapi` or `deepgram` target carrying a `carrier`, and it has no transport,
no connection, and no telephony channel on either. No shipped example has one,
which is why the whole feature was specified without noticing.

---

## R8. Documentation surfaces, counted

**Decision**: eight existing pages change and one is created.

| Page | Change | Requirement |
|---|---|---|
| `docs/SCHEMA.md` | numbered dated amendment superseding four clauses (§6.1 route fields, §6.1 destinations row, §6.3 example, §4.12 exemption) | FR-021 |
| `docs/user/reference/targets-yaml.md` | target field set loses three fields | FR-022 |
| `docs/user/reference/connections.md` | **new page** — file shape, routes, per-route env keys, per-route transfer support, the where-does-each-value-go table | FR-023, FR-023a, FR-023b |
| `docs/user/reference/agent-yaml.md` | `destinations` documented in its new home | FR-024b |
| `docs/user/reference/secrets.md` | telephony names now belong in `secrets:`; a missing one warns | FR-024b |
| `docs/user/learn/07-phone-calls.md` | becomes the end-to-end path; no sixth page | FR-024a |
| `docs/user/learn/twilio-walkthrough.md` | new shape everywhere it shows a route | FR-024 |
| `docs/TELEPHONY.md` | new shape everywhere it shows a route | FR-024 |
| `docs/user/_sidebar.md` | the new reference page needs a sidebar entry or nobody finds it | FR-023, implied |

Five example `README.md` files change under FR-008b, and the emitted
`build/<target>/README.md` template under FR-025.

**Rationale**: `docs/user/_sidebar.md` is not named in the spec and a new page
that is not in it is a page with no door. Recorded here so it lands in tasks.

---

## R9. What is deliberately not touched

Recorded so a reviewer can tell a gap from a decision.

- **The capability table.** `internal/target/telephony.go` gains one helper
  (R6) and changes no row, no feature, no tag, and no `Verified` date.
- **Both generators' readers.** Guaranteed by R1. If a generator needs an edit,
  R1 was implemented wrong.
- **`unmute dev` behaviour.** FR-018 and FR-018a describe what already works;
  they become regression tests, not new code.
- **The channel and capacity contract.** Untouched, per the spec's assumptions.
- **Non-telephony examples.** Out of scope by FR-005f0. `salon-support` would
  pass FR-005f today; the four without a README are a separate change.
- **The `secrets:` warn-versus-error severity for names generally.** Deferred by
  FR-005e.

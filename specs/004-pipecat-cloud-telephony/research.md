# Phase 0 Research: Pipecat Cloud telephony on the Daily route

Every provider claim below was verified against upstream source or official documentation on 2026-08-12, with the source named. Nothing here comes from memory, per constitution Principle IV.

## D1: The parameter class for the Daily inbound fix

**Decision.** Emit `DailyParams` for the `"daily"` entry in the generated `transport_params` map, on the Daily route only.

**Rationale.** Pipecat's `create_transport` has this Daily branch, verified in the pinned version at `src/pipecat/runner/utils.py` on tag `v1.5.0`:

```python
if isinstance(runner_args, DailyRunnerArguments):
    params = _get_transport_params("daily", transport_params)
    # Transparently wire PSTN dial-in (no-op when body has none).
    _maybe_apply_daily_dialin(params, runner_args.body)
    return DailyTransport(runner_args.room_url, runner_args.token, "Pipecat Bot", params=params)
```

`_get_transport_params` returns the factory result with no type check. `_maybe_apply_daily_dialin` then assigns onto it directly:

```python
params.dialin_settings = DailyDialinSettings(...)
params.api_key = request.daily_api_key
params.api_url = request.daily_api_url
```

`TransportParams`, verified at `src/pipecat/transports/base_transport.py` on `v1.5.0`, is a Pydantic `BaseModel` with `model_config = ConfigDict(arbitrary_types_allowed=True)`. It sets no `extra="allow"` and declares none of those three fields, so Pydantic v2 raises on assignment to an undeclared field.

`DailyParams`, verified at `src/pipecat/transports/daily/transport.py` on `v1.5.0`, is declared `class DailyParams(TransportParams)` and does declare all three:

```python
api_url: str = "https://api.daily.co/v1"
api_key: str = ""
dialin_settings: DailyDialinSettings | None = None
```

Because it subclasses `TransportParams`, everything the generated code already passes (`audio_in_enabled`, `audio_out_enabled`) keeps working unchanged.

**Alternatives considered.**

- *Parse the dial-in request in the generated bot and build the transport by hand*, which is what every official example shows. Rejected: it duplicates work the framework already does, and it would mean abandoning `create_transport`, which the generated bot uses for all four of its transports. More generated code for no gain.
- *Set `extra="allow"` on the params we construct.* Rejected: it would silence the error without giving the transport the fields it needs, so dial-in would appear to work and then behave wrongly. That is a silent downgrade, which Principle II forbids.
- *Always emit `DailyParams`, on every route.* Rejected under D2.

**Resolved 2026-08-12 (this was the one open item).** The import is `from pipecat.transports.daily.transport import DailyParams`. The docs show two spellings — the short `from pipecat.transports.daily import DailyParams` in the telephony guides, and the `.transport` form in the API reference — and only the API reference's is correct on the pinned version. Settled by importing both paths from the installed package rather than by reading more docs:

```text
pipecat.transports.daily            NO DailyParams
pipecat.transports.daily.transport  DailyParams
```

The telephony guides' spelling therefore raises `ImportError` on 1.5.0. The same run confirmed the behavioural claim the decision rests on, which no offline test can see:

```text
TransportParams  REJECTS: ValueError
DailyParams      ACCEPTS inbound call fields
DailyParams subclasses TransportParams: True
```

Both facts are now held by `TestSmokePipecatV1DailyInboundParamsAcceptCall`, which runs the framework's own `_maybe_apply_daily_dialin` against the emitted factory with a real dial-in payload, and asserts the generic class *still* refuses it. That second assertion is what stops the fix becoming decorative if upstream ever widens `TransportParams`.

## D2: Keeping the fix scoped

**Decision.** The `"daily"` entry becomes `DailyParams` only when the target's transport is the Daily route. The `"webrtc"` entry, the console path, and the carrier websocket entry are untouched.

**Rationale.** Two reasons, one correctness and one hygiene.

The generated bot registers several transports in one map and the dev runner picks one at runtime. A non-Daily package that happens to include the `"daily"` key does not need Daily's fields, and emitting a Daily-specific import into every project would break invariant V12 in the Pipecat driver's own spec history, which requires that `bot.py` import only what the package exercises. That rule exists because dead imports made the simple example unreadable and would fail `ruff`.

The hygiene reason is golden churn. Every existing Pipecat golden contains the `transport_params` map. An unconditional change rewrites all of them, which buries the one line that matters in a diff nobody will read carefully. Scoping the change means only the Daily golden moves.

**Alternatives considered.** *Emit `DailyParams` unconditionally and accept the churn.* Rejected for both reasons above; the constitution's golden-file rule is to read the diff before committing, which an unnecessary whole-suite rewrite defeats.

## D3: Where an account prerequisite lives

**Decision.** The prerequisite is **route data** in `internal/target`, surfaced three ways: a line in the `validate` report, a section in the emitted README, and a field in `compile-report.json`. It is **not** given a capability tag.

**Rationale.** Cold transfer on the Daily route dials the destination, so it needs Daily's dial-out feature, which Daily grants on request as a paid feature and enables per domain. Verified at `docs.pipecat.ai/pipecat-cloud/guides/telephony/daily-dial-out`, which states dial-out is a paid Daily feature requiring a pay-as-you-go key, and that international dial-out needs separate enablement per domain.

The tag question matters. The four tags in Principle II describe whether *Unmute* can honour a field: `core` never fails, `warn` prints and exits 0, `gated` is a hard error, `provisional` fails until proven. An account permission is none of those. Unmute can compile the package perfectly; whether the author's Daily account has the feature is unknowable at compile time and unrelated to the package's correctness. Forcing it into `gated` would refuse valid packages. Forcing it into `warn` would print on every single Daily compile forever, which trains authors to ignore stderr and is the noise failure mode the constitution's maturity rule warns about when it says provisional status must never be printed as a runtime warning.

So it is reported, not tagged. The report states a fact about the route; it does not claim a failure.

**Alternatives considered.**

- *A `warn` tag.* Rejected above: unconditional noise, and it would make `validate` print a warning for a package that is entirely correct.
- *Only in the emitted README.* Rejected: `validate` is the command an author runs before compiling, and Principle V makes it the one command that works for every provider. A prerequisite discovered only after compiling is discovered too late.
- *Only in `docs/`.* Rejected: it creates a second copy of a route fact with nothing tying it to the rulebook, which is the exact duplication Principle III forbids and the exact bug shape its rationale describes.

## D4: What the region story still needs

**Decision.** Add a refusal for an unhonourable region and carry the region facts into `validate`. Do not rewrite the README wording, which is already correct.

**Rationale.** Reading the current template, most of this is already done. `internal/generate/templates/pipecat_v1/README.md.tmpl` already emits:

```sh
pipecat cloud secrets set {{.SecretSet}} --file .env{{if .DeploymentRegion}} --region {{.DeploymentRegion}}{{end}}
```

and already carries the globally-unique-name caveat and a region section explaining the default. So FR-015 is substantially satisfied and the honest scope here is smaller than the spec's framing implies.

What is genuinely missing is on the validation side. `internal/ir/validate.go` checks region syntax and gates a list of more than one region, but nothing connects the region to anything else, and `validate` prints nothing about it. Verified at `docs.pipecat.ai/pipecat-cloud/guides/regions`: the four regions are `us-west` (default), `us-east`, `eu-central`, `ap-south`; credential stores are per-region with globally unique names; and an agent must be deployed in the same region as the endpoint serving it.

Region values stay forwarded-as-declared rather than checked against that list, per the existing N18 decision and because Principle II requires forwarded values to appear in the compile report, which they do. The platform's region list changes outside our release cycle, so hardcoding it would age badly.

**Alternatives considered.** *Validate the region against the four known values.* Rejected: it contradicts the existing amendment N18, and a new region on the platform would then fail a package that is correct. The `pipecat cloud regions list` command stays the authority and the report names it.

## D5: Correcting the two documents without breaking a true claim

**Decision.** Split the claim rather than delete it. `docs/DEPLOYMENT.md` gets a new dated stance line. `docs/TELEPHONY.md` keeps its cloud-free sentence but scoped explicitly to local runs.

**Rationale.** The current text, quoted exactly:

- `docs/DEPLOYMENT.md`: "Status: Adopted stance, July 23, 2026. For now, Unmute deployments do not use LiveKit Cloud or Pipecat Cloud. Everything below runs on infrastructure you control." and "the supported path is self-hosted."
- `docs/TELEPHONY.md`: the design "supports local and self-hosted deployments for Pipecat and LiveKit without requiring Pipecat Cloud or LiveKit Cloud."

The requester's correction draws a line the current text does not: local runs need no cloud account, and remote deployment is where the managed clouds are the supported path. Both halves are true, and the existing sentences fail by conflating them. Deleting the cloud-free claim would make the documents wrong in the other direction, because `unmute dev` genuinely runs with no cloud account and that is a real property worth keeping.

Per the amendment procedure, the new stance carries its own date and the superseded stance stays visible as history rather than being rewritten away.

**Alternatives considered.** *Delete the cloud-free sentences.* Rejected: it would remove a true and useful claim and would misdescribe `unmute dev`.

## D6: Whether a SCHEMA.md amendment is required

**Decision.** Yes. Add a numbered, dated amendment recording that Pipecat telephony is the Daily route.

**Rationale.** No authoring *field* changes, so the strict reading of the governance rule ("a change to the authoring surface") could be argued either way. Two things settle it toward amending.

First, `docs/SCHEMA.md` §4.9 currently describes what Pipecat's carrier-WebSocket adapters emit for inbound and outbound, and §9 carries route maturity rows. Those statements are now narrower than the text implies, because the base branch deleted the carrier transfers. A reader following §4.9 today would reach a different conclusion about Pipecat telephony than the code supports. Under Principle IV the document wins, which means a document that is out of step is a defect to fix, not a harmless lag.

Second, FR-019 requires the authoring contract to state the Daily-only fact so a reader learns it without opening a generated project. That statement belongs in the locked contract, and the contract's own rule is that changes to it are appended as numbered dated amendments, never edited in place.

**Alternatives considered.** *Record it only in `docs/TRANSFERS.md`.* Rejected: `TRANSFERS.md` is the user-facing reference and yields to `SCHEMA.md` on what a field means and where it works. Leaving `SCHEMA.md` stale would leave the outranking document wrong.

## Verification log

| Claim | Source | Date |
|---|---|---|
| `create_transport` Daily branch calls `_maybe_apply_daily_dialin` | `pipecat` source, tag `v1.5.0`, `src/pipecat/runner/utils.py` | 2026-08-12 |
| `_maybe_apply_daily_dialin` assigns `dialin_settings`, `api_key`, `api_url` | same file | 2026-08-12 |
| `TransportParams` is a Pydantic model with no extras and none of those fields | `pipecat` source, `v1.5.0`, `src/pipecat/transports/base_transport.py` | 2026-08-12 |
| `DailyParams(TransportParams)` declares all three fields | `pipecat` source, `v1.5.0`, `src/pipecat/transports/daily/transport.py` | 2026-08-12 |
| Dial-out is a paid Daily feature, granted on request, per domain, international separately | `docs.pipecat.ai/pipecat-cloud/guides/telephony/daily-dial-out` | 2026-08-12 |
| Four regions; `us-west` default; credential stores per-region with globally unique names; agent and endpoint must share a region | `docs.pipecat.ai/pipecat-cloud/guides/regions` | 2026-08-12 |
| Managed dial-in webhook is provided by the platform, no webhook server of the author's own | `docs.pipecat.ai/pipecat-cloud/guides/telephony/daily-dial-in` | 2026-08-12 |
| Pipecat's websocket transports have no call-transfer control | `docs.pipecat.ai/pipecat/telephony/overview` | 2026-08-12 |
| Daily cold transfer is `sip_call_transfer`, and the bot leaves on `on_dialout_answered` | `docs.pipecat.ai/pipecat/telephony/daily-pstn` | 2026-08-12 |

**Unresolved, deliberately deferred to implementation.** The import path for `DailyParams` (see D1). It is resolved by importing the real package in the L4 smoke rather than by further reading, because the docs disagree with themselves and the installed package is the authority.

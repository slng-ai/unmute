# Contract: the emitted project, per route

What a compiled project must and must not contain. The "must not" column is what keeps the two route shapes from leaking into each other, which is the failure the spec's original hosting-model story was trying to prevent and which now has to be held by tests instead of by an authoring field.

## Pipecat Daily route (`transport: daily-sip`)

**Must contain.**

| Item | Detail |
|---|---|
| `bot.py` | A transport parameter object for the Daily key that accepts inbound call fields, with its import present. |
| `bot.py` | The cold transfer tool, announcing to the caller, calling the platform transfer primitive, reading the error the primitive returns, and handing the model a failure result on failure. Already emitted; unchanged by this feature. |
| `bot.py` | An in-process guard so a second transfer request in the same call produces no second attempt. **This route cannot use the shared control store** the carrier routes use for it, because this contract forbids that service here and the constitution forbids an idle one. The guard therefore lives in the call's own process and lasts the life of the call, which is sufficient because the call is served by exactly one process. Spec FR-008. |
| `pcc-deploy.toml` | The agent name, the credential-set name, and the region line when a region is declared. |
| `README.md` | The deploy sequence in the order that works: credentials before deploy. The account prerequisite. How to attach a phone number. The region facts, including that credential stores are per-region with globally unique names. |
| `.env.example` | Every required env name, including the transfer destination. No values. |
| `compile-report.json` | The forwarded region, the route's prerequisites, and the route's provisional status. |

**Must not contain.**

| Item | Why |
|---|---|
| A Redis service | Nothing on this route coordinates through it. The route has no telephony plan, so no coordination reason exists to justify a sidecar, and the constitution forbids an idle one. |
| A public HTTP endpoint of its own for call ingress | The platform's managed dial-in webhook is the ingress. An emitted `/telephony/inbound` would be an endpoint nothing calls. |
| A media websocket endpoint of its own | Same reason. Daily delivers media over its own infrastructure. |
| Credentials that only a self-hosted media path needs | An author must not be asked for a value that nothing reads. |
| A Daily-specific import on a package that is not on this route | Invariant: `bot.py` imports only what the package exercises. |

## Pipecat carrier websocket routes (`transport: carrier-websocket`)

**Unchanged by this feature.** Listed so the contract is complete and so a test can assert the shapes stay distinct.

Must contain: its own runtime process, its own signed carrier ingress, its own media endpoint, its own status callback endpoint, Redis with at least one coordination reason, and the carrier credentials.

Must not contain: any transfer tool. The base branch deleted those deliberately, because the framework's websocket transports have no call-transfer control and every attempt to own the audio path there produced a lifecycle bug.

## Cross-route invariants

These are the assertions that have to exist, because each is a fact that would otherwise be true only by accident.

1. **A Daily project declares no service and no public endpoint of its own.** Asserted against the emitted artifact's runtime description, not by reading the README prose.
2. **A carrier websocket project keeps its current services and endpoints.** A regression guard: this feature must not shrink a route it is not touching.
3. **Neither shape's credentials appear in the other.** Asserted by set comparison, so a new credential added later to one route cannot quietly appear in both.
4. **No transfer tool is emitted on a carrier websocket route.** Guards the base branch's deletion against being undone by a later change to shared emitter code.
5. **A route that can receive a phone call emits a parameter class that accepts inbound call fields.** Stated as a property rather than a class name, so an upstream rename fails honestly instead of passing on a stale string match.
6. **Every emitted region reference resolves from the one declared value.** No second authored region.
7. **The emitted README, the capability rulebook, and `docs/user/` agree on what the route supports**, including its prerequisites. Extends the existing emitter-versus-rulebook agreement test rather than adding a second one.
8. **A second transfer request produces no second attempt**, on every route that emits a transfer tool. The mechanism differs per route (shared store on the carrier routes, in-process on Daily) but the observable property is the same, so the assertion is written against the property.

Invariants 1 through 4 are spec FR-027. Invariant 5 is FR-005. Invariant 6 is FR-013. Invariant 7 is FR-024. Invariant 8 is FR-008. None of them is a contract-only rule with no requirement behind it, which was true of the first four until 2026-08-12.

## Goldens

- The Daily example's golden moves, because its `bot.py` gains the corrected parameter class and its README gains the prerequisite section.
- Every other Pipecat golden must stay byte-identical. That is the check that the fix was scoped correctly, per research D2. A diff outside the Daily golden means the change was made unconditionally and needs narrowing.
- Regenerated with `-update-pipecat`, then the diff is read before committing.

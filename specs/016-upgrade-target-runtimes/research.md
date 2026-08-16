# Research: Upgrade Target Runtimes and Make Version Support Scalable

**Feature**: 016-upgrade-target-runtimes | **Date**: 2026-08-16

Every claim below was checked against upstream source, an installed package, or
this repository, as Principle IV requires. Where a fact came from running code,
it says so.

## R1. Ceiling versions

**Decision.** pipecat-ai **1.7.0**, livekit-agents **1.6.10**. Floor stays
**1.5.0** for both.

**Rationale.** Both are the newest releases on PyPI (pipecat 1.7.0 on
2026-08-01, livekit 1.6.10 on 2026-08-13). Neither framework has a 2.0, so the
existing `<2.0` cap never binds.

**Compatibility, checked not assumed.** Pipecat 1.5.0 → 1.6.0 → 1.7.0 renames,
moves, or changes the signature of nothing this generator emits: `Pipeline`,
`PipelineRunner`, `PipelineTask`, `PipelineParams`, `LLMContext`,
`LLMContextAggregatorPair`, `LLMUserAggregatorParams`, `LLMRunFrame`,
`LLMSwitcher`, `SileroVADAnalyzer`, `pipecat.runner.*`, the transports, the
Twilio serializer, and the service classes are all unchanged. LiveKit 1.6.9 →
1.6.10 is one patch release with no API change at all. So one template set per
framework spans the whole window, which is what makes the range-plus-exact-pin
design honest rather than hopeful.

**Alternatives considered.** Per-version template sets or version-conditional
emitters: rejected, nothing in either range needs them, and they would multiply
the goldens by the number of supported versions.

## R2. Small upstream items that do touch us

| Item | Effect here |
|---|---|
| livekit openai plugin now requires `openai<3` | No action. `openai` is an extra of `livekit-agents`, never a standalone package in the catalogue, so we emit no `openai` constraint and `pins:` cannot name one. The framework enforces it. FR-009 is satisfied structurally. |
| livekit-agents 1.6.10 requires Python `>=3.10,<3.15` | No action. See R10. |
| pipecat `runner` extra now pulls `pipecat-ai-prebuilt>=1.0.5` | No action, transitive. |
| pipecat `webrtc` extra deprecates its OpenCV dep for video | No action, our pipelines are audio-only and 1.6.0 made `cv2` lazy. |
| pipecat 1.7.0 fixed `LLMSwitcher` settings delivery | Out of scope. It is the machinery behind the per-task model gate (B7). Worth re-verifying on its own; not a rider on this feature. |
| RTVI `dtmf` client message shape changed in 1.6.0 | Client-side only. `RTVIProcessor` behavior downstream is unchanged. |

## R3. LiveKit must emit an exact pin

**Decision.** `livekitDeps()` stops deriving a constraint and emits
`livekit-agents[extras]==<declared version>`. The constraint ladder at
[livekit_v1_build.go:1296-1311](internal/generate/livekit_v1_build.go:1296) is
deleted; `extras["mcp"] = true` stays, because which extra to install is a
different fact from which version to install.

**Rationale.** Today `data.Version` is read only for the compile report. The
author's declared version has zero effect on what installs, so a LiveKit project
floats to the newest 1.x. That float is why the repository's own records
disagree: examples declare 1.6.4 while tests say "verified against 1.6.9".

**Consequence, and it is the important one.** Feature floors currently work by
*overwriting* the author's version at emission time. With an exact pin that
mechanism cannot exist, so each floor becomes a validation rule instead. See R4.

## R4. Feature floors become validation, and the upper bound disappears

**Decision.** Warm transfer and MCP each declare a minimum framework version
(1.6.0, both LiveKit). Validation fails when a package uses the feature and
declares a lower version, naming the feature, the floor, and the declared
version. The `<1.7` upper bound is dropped.

**Rationale for the floors.** Silently rewriting a declared version is exactly
the silent downgrade Principle II forbids, and it hides a real incompatibility:
a package declaring 1.5.2 with a warm transfer validates and compiles green
today only because the emitter quietly installs `>=1.6,<1.7` behind the author's
back. Under an exact pin the same package would install 1.5.2, which has no
`WarmTransferTask`, and fail at import. Turning the floor into a gate converts a
runtime import error into a validation error that names the cause.

**Rationale for dropping `<1.7`.** The bound is a verified-window marker, not a
known incompatibility. Its own comment says a beta API "is allowed to move"
between minors, so a warm package pins the series it was verified against. With
a per-framework verified ceiling, that job is now done centrally and for every
feature at once, and the ceiling is re-verified by a live call each release.

**One home for the predicate.** "Does this package use a warm transfer / an MCP
source" is currently derived inside the LiveKit builder only. Validation now
needs the same answer, so the predicate gets one home that both read, or an
agreement test that fails when they diverge. Two independent definitions would
recreate the bug class this feature exists to remove (Principle III).

## R5. Version comparison needs the patch, and no new dependency

**Decision.** Extend `CheckVersion` to compare full triples against floor and
ceiling, reusing the existing `ParseVersion` + `slices.Compare` already used by
`CheckPins`.

**Rationale.** Today's check compares major and minor only
([driver.go:53-56](internal/target/driver.go:53)); the patch is parsed and
discarded, so `1.99.0` passes. A ceiling of `1.6.10` is meaningless without
patch comparison. String comparison would be actively wrong here: `"1.6.10"` is
lexically *less* than `"1.6.4"`, so the ceiling bump we are shipping is exactly
the case a naive compare gets backwards.

**Alternatives considered.** `golang.org/x/mod/semver`: rejected, the repo
already has a parser and a comparison for this, and a new dependency for a
ten-line function is against the dependency rule.

## R6. Declared versions must carry all three parts

**Decision.** Require `X.Y.Z`. Reject `1.5` and `1` with a message naming the
supported range.

**Rationale.** The version is now an exact install pin, and a ceiling comparison
against a partial version is ambiguous to a reader even where PEP 440 resolves
it. Failing loud on a half-written pin is cheaper than explaining what `==1.5`
resolved to.

**Cost.** One row flips in an existing test table, and the schema amendment
notes it. No shipped example, fixture, or scaffold default uses a partial
version, so nothing in the repository changes behavior.

## R7. The LiveKit run-mode migration is bigger than swapping `dev` for `start`

**Decision.** Generated projects move to the thin, non-deprecated CLI:
`python -m livekit.agents start agent.py`. The emitted
`if __name__ == "__main__": cli.run_app(server)` block and its `cli` import are
removed.

**Rationale, verified by installing livekit-agents 1.6.10 and running it.** The
deprecation is wider than the 1.6.8 release note implies. It is not only the
`dev` and `console` subcommands: `cli.run_app` itself is documented as "Run the
agent via the (deprecated) rich Python CLI", delegates to a `_legacy` module,
and fires an umbrella `DeprecationWarning` covering **every** subcommand,
`start` included, saying the built-in Python CLI "will be removed in a future
release". Its own docstring names the replacements: the `lk agent` CLI and "the
thin interface in `livekit.agents.__main__`".

So `python agent.py start` is a deprecated wrapper that happens to print nothing
(the warning is attributed to a library frame, which Python's default filters
suppress). Since FR-011 forbids emitting what the ceiling deprecates, and since
a future removal would break every generated project at once, the fix is to stop
going through the wrapper at all.

**Verified against our own emitted shape.** The thin CLI discovers a
module-level `AgentServer` named `app`, `server`, or `agent`. Our template
already emits `server = AgentServer(...)`
([agent.py.tmpl:909](internal/generate/templates/livekit_v1/agent.py.tmpl:909)),
so it qualifies. Running `python -m livekit.agents start agent.py
--log-format colored` against a module of exactly our shape started the worker,
bound the health port, initialized processes, honored the colored format, and
printed **no deprecation warning**. (A `WorkerOptions`-based agent fails this
discovery with `Could not find AgentServer`; ours is not that shape, which is
why the migration is available to us today.)

**Alternatives considered.**

- *Keep `agent.py dev`*: rejected, it prints two visible deprecation warnings on
  every dev run at 1.6.10.
- *Swap to `agent.py start` and stop there*: rejected as the endpoint, though it
  is the minimal change. It leaves generated projects on a wrapper slated for
  removal, and the user's instruction was to drop deprecated-mode use.
- *Adopt the `lk agent` CLI*: rejected. It is a separate Go binary that would
  have to be installed into the image, it wants a project manifest, and its
  `console` needs host mic and speakers. It is a developer tool, not a container
  entrypoint.

## R8. What changes when the container runs `start` instead of `dev`

Verified by reading 1.6.10 and by probe runs.

| Behavior | `dev` | `start` (and the thin CLI) | Verdict |
|---|---|---|---|
| Job dispatch | identical | identical | **Safe.** Dispatch is decided by whether `agent_name` is set, not by the mode. Our browser dev path already uses explicit token dispatch (`roomConfig.agents[].agentName` in [livekit_token.go:62](internal/cli/livekit_token.go:62)), which is unaffected. |
| Worker registration | registers | registers | **Safe.** Both run with `unregistered=False` and reach the same `"registered worker"` log line, which is the marker `readyWatcher` waits for before opening the browser. Re-confirm in the live call. |
| Hot reload | **already removed in 1.6.10** | never had it | **No loss.** 1.6.10's `dev` logs that in-process auto-reload is gone. Our compose has no bind mount either, so reload was doubly dead. |
| Log format | colored, DEBUG | JSON, INFO | Dev compose passes `--log-format colored` so a human watching `unmute dev` still gets readable logs. |
| `load_threshold` | `inf` | `0.7` | Watch item for the live call. A CPU-starved container could stop accepting jobs. Our compose sets no CPU limit, so the risk is low; the knob is `ServerOptions.load_threshold` if it bites. |
| `drain_timeout` | n/a | 3600s | Not addressed, deliberately. Draining finishes immediately when no job is active, and `compose down` already caps the wait at 30s. If ctrl-c during a live call feels slow in verification, the knob exists. |
| Health port | random | 8081, container-internal | Non-issue, nothing publishes it. The telephony branch already binds it on purpose. |

## R9. Console removal

**Decision.** Delete `--console`, its uv runner, and the emitted console
scaffolding (the Pipecat `console` extra, `console_main()` and its `sys.argv`
branch in `bot.py`, and the console lines in emitted READMEs). `unmute dev`
becomes Docker and compose only, on both targets.

**Rationale.** The user's decision. It is also where upstream is going on the
LiveKit side: at 1.6.10 `agent.py console` prints a deprecation banner, and the
replacement `console` in the thin CLI is not a local mic session at all but a
TCP console that attaches to a running worker.

**Cost, stated plainly.** Docker becomes a hard prerequisite for hearing an
agent locally. That narrows Principle V, which today names `--console` as the
"uv, no Docker" path, so this feature carries a constitution amendment. See the
plan's Complexity Tracking.

**Note.** Removing console does not remove all `uv` use: the Pipecat
cloud-websocket telephony route still shells out to `uv run bot.py`
([dev_cloud_websocket.go:154](internal/cli/dev_cloud_websocket.go:154)).

## R10. The Python upper bound is not mirrored

**Decision.** Emitted `requires-python` stays `>=3.10` (LiveKit) and `>=3.11`
(Pipecat). We do not copy livekit-agents' `<3.15`.

**Rationale.** The framework declares its own bound and the resolver reports it.
Copying it into every generated project creates a second home for a fact
upstream owns, which is the thing Principle III exists to prevent, and it would
go stale the moment upstream widens it.

## R11. The registry shape follows an existing convention

**Decision.** A per-provider record of floor, ceiling, and verification date in
`internal/target`, replacing `driverVersions`.

**Rationale.** `internal/target` is the leaf package both `internal/ir` and
`internal/generate` already import, so one home is reachable from validation,
both drivers, and the scaffold without a cycle. A dated verification field is
already the convention here: catalogue entries carry `Verified`
([catalog.go:99](internal/target/catalog.go:99)) and surface it in the compile
report through `serviceNotes`, which is a ready-made path for FR-007.

## R12. The drift check imitates an existing test

**Decision.** Model the agreement test on `TestOneModelIdEverywhere`
([internal/skill/agreement_test.go:496](internal/skill/agreement_test.go:496)),
which already solves this exact problem for the model identifier across roughly
two dozen author-facing files.

**Four properties worth copying.** A guard that fails when the recorded home is
empty, so the test cannot pass vacuously. Matching by *shape* (a regex over
version-looking strings next to `livekit-agents` / `pipecat-ai`) rather than a
list someone must remember to extend. An explicit, curated set of author-facing
surfaces. And a documented carve-out, because goldens, fixtures, and the specs
that record history deliberately contain stale versions and must not fight the
test.

## R13. Smoke stops floating on its own

**Finding, no separate work.** The smoke layer installs whatever the emitted
`pyproject.toml` resolves to. Pipecat already installs the declared version;
LiveKit floats because its pin is open-ended. Once R3 lands, both install the
declared version and FR-006 is satisfied by the pin change itself.

## R14. Where the human verification is recorded

**Decision.** `specs/016-upgrade-target-runtimes/results.md`, matching the
convention spec 013 set with its own `results.md`.

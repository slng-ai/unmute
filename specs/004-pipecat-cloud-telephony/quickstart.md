# Quickstart: proving Pipecat Cloud telephony on the Daily route

Two halves. The first needs nothing but the repository and proves everything that can be proven offline. The second needs real accounts and money, and is what closes the Status section in `docs/TRANSFERS.md`.

## Part 1: offline, no accounts

Run from the repository root.

### The gate

```sh
make fmt
make lint
make build
make test
```

`make test` is the required gate and must pass with zero Python. Expect every package green.

**Known pre-existing defect, unrelated to telephony.** If `internal/generate` fails with `public example directories = [...]`, the cause is an example directory holding only a gitignored `.env` and no package files. `internal/generate/examples_test.go` enumerates directories on disk, which violates the constitution's rule that a repository hygiene check must be written against `git` rather than the working tree, precisely so that compiling an example locally cannot turn the suite red. Recorded in the spec's Out of Scope section. Work around it by removing the stray directory; do not treat it as caused by this feature.

### The example still validates and compiles

```sh
go build -o /tmp/unmute .
/tmp/unmute validate examples/human-transfer-daily
```

Expected before this feature: `✓ pipecat` and nothing else.

Expected after: the same result line, plus the Daily dial-out prerequisite named on stderr, because the package uses a cold transfer. Exit code stays 0 either way, because a prerequisite is a fact about the route, not a failure.

```sh
/tmp/unmute compile examples/human-transfer-daily
```

Then read the emitted project:

```sh
ls examples/human-transfer-daily/build/pipecat/
grep -n 'daily' examples/human-transfer-daily/build/pipecat/bot.py
grep -n -A3 'Prerequisite' examples/human-transfer-daily/build/pipecat/README.md
cat examples/human-transfer-daily/build/pipecat/pcc-deploy.toml
```

What to look for:

- the Daily transport parameter object accepts inbound call fields, and its import is present
- the README names the dial-out prerequisite
- the manifest carries the agent name and credential set, and a region line only if the package declares one
- no Redis service, no `/telephony/inbound`, no media websocket endpoint

Clean up afterwards, since `build/` is disposable and gitignored:

```sh
rm -rf examples/human-transfer-daily/build
```

### The prerequisite appears only when it is needed

Compile a Daily package with no transfer and confirm no prerequisite text appears anywhere. This is the half of the rule that is easy to get wrong: a prerequisite that always prints is a banner, not a warning.

### The fix is scoped

```sh
go test ./internal/generate/
```

Every Pipecat golden except the Daily one must be byte-identical. A diff in another golden means the parameter class change was made unconditionally and needs narrowing. See research D2.

### The `--telephony` refusal

```sh
/tmp/unmute dev --telephony examples/human-transfer-daily
```

Expected: exit 1, with a message naming the route, naming the browser and console modes that do work on it, and pointing at the deploy path for a real phone call. It must not say telephony is unsupported on the route, which would be false.

### The real package, opt in

```sh
make smoke
```

Needs `uv`. Skips rather than fails when `uv` is absent, and never runs in the pull request gate. This is the only layer that catches a wrong import, which matters here because the official documentation shows two different import spellings for the Daily parameter class and the installed package is the authority. See research D1.

## Part 2: the live run

This is the recipe in `docs/TRANSFERS.md` §4, Pipecat rig. It is reproduced here only as a checklist; that document is the authority and any correction lands there first.

### Prerequisites

- A Pipecat Cloud account, authenticated, and its public API key.
- A Daily developer account and its API key. **Two different keys**, and confusing them is the most common way to lose an hour here: `DAILY_API_KEY` buys and manages numbers, the Pipecat Cloud `pk_...` key starts sessions.
- **Dial-out enabled on the Daily account.** A paid feature granted on request, and international dial-out is granted separately per domain. Without it the cold transfer cannot dial the destination. Ask first: a person approves it.
- A purchased Daily phone number. `scripts/daily-phone-number.sh` wraps the four REST calls. Note the returned `id`, which is what dial-out wants as its `callerId`.
- Two phones you can answer: one to call from, one to be the billing destination.
- Real money, and a **14-day floor on the number**. Per-minute charges apply on both legs, and the number cannot be released for two weeks.

The full step-by-step runbook, with every command and the failure each step can produce, is `examples/human-transfer-daily/README.md`. This section is the checklist; that file is the walkthrough.

### Steps

1. Compile the example.
2. Follow the Deploy section of the emitted `build/pipecat/README.md`. The order matters: create the credential set first, because the manifest already names it, then deploy. Confirm the agent reports ready.
3. Attach the Daily number to the deployed agent using the platform's managed dial-in webhook, from the dashboard or the REST API. No webhook server of yours is involved.
4. **The answer test.** Call the number. The agent should answer and hold a conversation. This is the step that fails today, before this feature, because the transport is built with a parameter object that rejects the inbound call details.
5. **The cold transfer test.** Ask about an invoice. Expect one spoken handoff line within about two seconds, then the billing phone rings and the agent leaves the call.
6. **The failure drill.** Point the destination env var at an undialable number and call again. Expect the agent to say the transfer did not work and keep helping, or hang up after a goodbye, according to what the package declared.
7. **The double-request drill.** Call again and ask to be transferred twice in quick succession. Expect exactly one transfer attempt. Before this feature there was no guard at all on this route, so a second request would fire a second platform call.

### Recording the result

The run is not finished until `docs/TRANSFERS.md` carries the dated result. If any step above was wrong, the correction lands in that document before the code, per its own rule. A row stays provisional until its recipe has been run as written.

### Teardown

A test rig must not become a standing bill. Delete the deployed agent and remove the credential set if it was created for the test.

**The number cannot be released with the rest of it.** Daily refuses to release a number until 14 days after purchase, and releasing is permanent after that (verified 2026-08-12). So a Daily test rig has a two-week minimum cost that no teardown step can shorten. Note the purchase date and release it later. `scripts/daily-phone-number.sh release <id>` does it when the time comes. This is worth knowing before step 2, not after step 7.

## What the implementation run found (2026-08-12)

Recorded here because several tasks were written expecting either a production
change or a green test, and the honest answer differed.

**The baseline gate was green** before any change: `make fmt`, `make lint`,
`make build`, `make test` all passed. The pre-existing `internal/generate`
failure described above did not occur, because no stray example directory was
present. The defect in `examples_test.go` is still there and still out of scope.

**Three groups of regression guards passed with no production change**, which is
what they were for:

- The two route shapes were already distinct. A Daily project already declared no
  service, no public endpoint, and none of the self-hosted credentials, and the
  carrier websocket routes already kept theirs. No transfer tool was emitted on
  any carrier route. Tasks T019 through T022 needed no fix, so T025 is recorded
  as passed unchanged.
- The region story was already correct in the emitted project, as research D4
  predicted. One declared region already reached the manifest and the
  credential-store command, and the no-region case already stated the default for
  both sides. T045 changed no wording.

**Two tasks could not be done as written**, both because they contradict
something the repository already decided:

- **T042 and T044, the "unhonourable region" refusal.** `docs/SCHEMA.md` N18 and
  N32 state that region codes are forwarded exactly as written, that the platform
  CLI is the validator, and that no list of codes lives in this repository because
  both platforms change theirs without notice. Research D4 considered validating
  against the four known regions and rejected it for the same reason. Adding a
  refusal would have contradicted the locked contract, which Principle IV says
  wins. So no new refusal was added. Instead the three refusals that *are*
  knowable without a region list are now covered by tests (empty entry, the same
  region twice, more than one region on Pipecat), and a further test pins that an
  unrecognised region code still compiles, so nobody adds an allow-list later.
- **T046, validating a Daily package that declares outbound calling.** There is no
  way to declare it. A telephony channel requires a resolved connection plan and
  the Daily route has none, so `channels.phone` on it fails at build time with
  `target "pipecat" requires connection for telephony`. Adding a path for it would
  be new authoring surface, which FR-002 rules out and T007 now tests against. The
  half of User Story 5 that needs no authoring surface **is** done: the emitted
  README describes how the platform starts an outbound call against the deployed
  agent, and states that the recipient sees a provider-chosen number, because the
  package cannot pick one. Declaring outbound on this route is its own feature.

**One defect was found and fixed that no task asked for.** The emitted Daily
project failed the `ty` check its own README promises: the transfer called
`sip_call_transfer` on a `BaseTransport`, where that method does not exist. The
static-check smoke only covered `simple-prompt`, which emits no transfer, so this
could never have been caught. The tool now narrows to the Daily transport first,
which fixes the type error and turns a transfer requested in a browser session
from an `AttributeError` into a named failure the model can act on. Covered by
`TestSmokeV24PipecatDailyTransferStaticCheck`.

**T052 was implemented as a scoping property rather than a stored baseline.** A
byte-for-byte copy of every example's old output would need a golden per example
per file and would go stale on the first unrelated template edit, at which point
the question it exists to answer gets buried. The property is the same and is
checked directly: none of this feature's additions may appear on any target that
is not the Daily route.

## Success signals

Mapped to the spec's measurable outcomes.

| Signal | Spec |
|---|---|
| An author reaches an answered call using only the emitted instructions | SC-001 |
| Cold transfer completes on 9 of 10 attempts | SC-002 |
| Every failure path leaves the caller connected and informed, 10 of 10 | SC-003 |
| The dial-out prerequisite was named before any money was spent | SC-004 |
| A region change is one line and every reference follows | SC-005 |
| The Daily route and its support are legible from the authoring contract alone | SC-006 |
| No document states a stance the project does not hold | SC-007 |
| Existing packages behave identically or fail with a named fix | SC-008 |
| The default suite passes with zero failures | SC-009 |
| Every claimed capability has a dated live record | SC-010 |
| The caller hears something within 2 seconds of asking for a person | SC-011 |
| A second transfer request never produces a second attempt, 10 of 10 | SC-012 |

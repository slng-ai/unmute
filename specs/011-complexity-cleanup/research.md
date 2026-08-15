# Phase 0 Research: Complexity Cleanup

**Date**: 2026-08-15 | **Plan**: [plan.md](./plan.md)

The cleanup itself needs no research; it is mechanical Go editing against a
finished audit. Everything below is about the verification half, where the
questions were real. Findings come from reading the current tree, not from
assumption.

---

## R1. Where the acceptance run has to live

**Decision**: A Go test behind `//go:build sweep`, run as
`go test -tags sweep ./internal/cli/... -run TestSweep`. Not a new `unmute`
subcommand, and not a shell script.

**Rationale**:

- The constitution fixes the command surface at four commands and says what each
  one is for. A fifth command for an internal QA tool would add user-facing
  surface to remove complexity, which is self-defeating.
- The L4 smoke layer already establishes the exact pattern this needs: a build
  tag, excluded from the default suite and the PR gate, skipping rather than
  failing when its tooling is absent. FR-032 restates that rule, so inheriting
  the mechanism is free.
- Go subtests give per-example, per-target reporting for nothing. `t.Run` names
  map one-to-one onto FR-034's requirement to record a result per example and
  target, and `-run` lets a failing pair be re-run alone.
- `internal/cli` already holds `dev_web_smoke_test.go` and
  `dev_compose_smoke_test.go`, so the file has an obvious home and obvious
  neighbours to copy structure from.

**Alternatives considered**:

- *A fifth cobra command (`unmute verify`)*: rejected. Adds permanent surface,
  needs help text, docs-site pages, and its own tests, for a tool only
  maintainers run.
- *A shell script like the old `preflight.sh`*: rejected, and pointedly — this
  feature deletes `preflight.sh` precisely because an unreferenced script rots.
  A script would repeat that mistake in the same commit that fixes it.
- *A CI workflow*: rejected. FR-032 keeps container and credential work out of
  the automated gate.

---

## R2. Proving the agent actually spoke

**Decision**: Drive real headless Chromium against the real dev page with
Playwright, invoked by `exec` from the Go test. Pass when an inbound audio track
reports `bytesReceived > 0` via `getStats()` within a timeout.

**Rationale**:

- The pass criterion is media arriving at a client. The only thing that can
  observe that is a client. `internal/cli/dev_web_smoke_test.go:115` already
  concedes this in a comment: a full call "needs a mic and a browser, so a full
  call stays a manual smoke". This feature automates exactly that manual smoke.
- One driver covers both targets. Pipecat POSTs a WebRTC offer to `/api/offer`;
  LiveKit joins a room with a minted token. Both end as WebRTC inbound audio in
  the same page, so `getStats()` is transport-agnostic in the same way the page
  is. No per-target branch in the driver.
- It exercises the code this feature actually changes. `/api/session` calls
  `mintLiveKitToken` (`dev_web.go:275`), which is one of the three functions
  FR-009 collapses. A browser session is the only check that proves the merged
  signing still produces a token a real LiveKit server accepts.
- Chromium runs unattended with `--use-fake-device-for-media-stream`,
  `--use-fake-ui-for-media-stream`, and `--autoplay-policy=no-user-gesture-required`.
  No microphone, no human — which is what makes the sixty-five-session cadence
  affordable.
- Playwright is external tooling invoked by `exec`, exactly like `ruff`,
  `docker`, and `uv` already are. It is not a Go module dependency and not a
  maintained Python package, so it clears both constitutional constraints.

**Alternatives considered**:

- *`pion/webrtc` as a Go dependency*: rejected. A new direct dependency, and it
  would test a hand-built client rather than the page users actually load. The
  repository hand-rolls its LiveKit JWTs specifically to avoid pulling in an SDK;
  pulling in a WebRTC stack for a test would contradict that.
- *A Python client with `aiortc` or the LiveKit Python SDK*: rejected. The
  constitution forbids a maintained Python package in this repository.
- *`chromedp` or `playwright-go`*: rejected. Both are Go module dependencies.
  Invoking the Playwright CLI keeps the dependency count at zero.
- *Watching container logs for a TTS marker*: rejected as the primary signal. It
  proves the agent believes it spoke, not that audio reached a client, and it
  couples the test to log strings the driver may reword. Kept as a **secondary
  diagnostic** on failure, because knowing whether the agent never spoke or the
  media never arrived is the difference between a two-minute and a two-hour
  debug.
- *Stopping at `/api/session` returning a valid contract*: rejected. It proves
  the dev server and token minting but not the session, which is less than the
  clarified pass criterion asks for.

---

## R3. Making the byte-identity check trustworthy

**Decision**: Compile all eleven examples to a baseline tree before the first
edit, recompile after each story, and compare with the build directory's two
preserved patterns excluded. Pin the `ruff` version across both sides.

**Rationale**:

- `internal/generate` contains no `time.Now()` and no build-stamp field. The
  `Version` fields carry the target's declared framework version from
  `targets.yaml`, not a timestamp. Generation is therefore deterministic and a
  byte-diff is meaningful.
- **`ruff` is the one real nondeterminism.** `formatPython`
  (`internal/cli/compile.go:297`) formats emitted Python only when `ruff` is on
  `PATH`, and formatting differs between `ruff` versions. A baseline captured
  with one version and compared against another would show diffs everywhere and
  hide a genuine regression. Both sides must run the same version, or neither
  side may have `ruff` at all.
- **Two files must be excluded from the diff.** `preservedPatterns`
  (`internal/cli/compile.go:155`) is `[".env", "livekit*.toml"]`, and a rewrite
  deliberately restores them. `.env` in particular is written by the sweep, so
  comparing it would compare the sweep against itself.
- Comparing whole trees rather than only the goldens covers the four
  telephony examples, which FR-031 verifies by compiling alone and which no
  golden covers end to end.

**Alternatives considered**:

- *Trusting the goldens alone*: rejected. Goldens pin a fixture package, not the
  eleven real examples, and FR-031 makes the carrier examples' bytes their only
  evidence.
- *Hashing instead of diffing*: rejected. A hash says something changed; a diff
  says what, which is what a person needs at the moment it fails.

---

## R4. Where the sweep gets its credentials

**Decision**: Run the sweep with the working directory at the repository root
and let the existing loader find `.env`. Add an explicit pre-check that every
required name is present before the first container builds.

**Rationale**:

- **No copying is needed.** `devChildEnv` (`internal/cli/dev.go:447`) already
  reads `$CWD/.env` and then `<package>/.env`, layering both over the process
  environment. Running from the repository root means `unmute dev` picks up the
  root `.env` unchanged. FR-036's "copied into each build's environment" is
  satisfied by behavior that already exists, so the harness implements nothing.
- The pre-check exists because FR-037 asks for one and because the failure it
  prevents is expensive: without it, a missing key surfaces as a container that
  starts and never speaks, after a build, once per affected session.
- The pre-check reads **names only**. `parseDotenv` returns values, so the
  harness must compare presence and never log, print, or record a value —
  FR-036 and the constitution's secrets rule.

**Confirmed gap**: the root `.env` holds twenty-four of the twenty-five names
these examples declare. `FIRECRAWL_MCP_URL` is declared by `mcp-example` and is
absent. Under FR-035 that fails both of its sessions, so it is pre-work, per
FR-038.

**Alternatives considered**:

- *Copying `.env` into each build directory*: rejected as redundant given the
  loader, and it would multiply copies of a secrets file across eleven
  directories.
- *Reading credentials from the environment only*: rejected. It would make the
  sweep depend on shell state that differs per person, where a file is the same
  for everyone.

---

## R5. Two examples need seeded variables

**Decision**: The harness carries a small per-example table of `--var` flags.
`salon-support` gets `customer_name` and `customer_id`; `multi-task` is checked
at implementation time and given the same treatment if its variables are
`call_start`.

**Rationale**:

- `salon-support` declares `customer_name` and `customer_id` with
  `source: call_start`, and its greeting is `Hi {{customer_name}}, thanks for
  calling…`. A `call_start` variable is filled by dispatch metadata in
  production and by `--var` locally; that equivalence is a stated principle.
  Without the flag the greeting is at best malformed, which would fail the
  session for a reason that has nothing to do with this refactor.
- `unmute dev` validates each `--var` against the declared name and type before
  anything compiles, so a wrong entry in the table fails fast and loudly.
- All seven runnable examples declare `speaks_first: agent`, so the greeting
  criterion is reachable for all thirteen sessions with no other intervention.
  This was checked rather than assumed; a `speaks_first: user` example would
  have hung forever waiting for audio.

**Alternatives considered**:

- *Leaving variables unset*: rejected. It fails a session for an unrelated
  reason and would train the runner to ignore a red result.
- *Editing the examples to drop the templated greeting*: rejected outright.
  SC-007 requires that no example file changes.

---

## R6. What the sweep costs, and why it is affordable

**Decision**: Run sessions serially on one port. Accept a slow first sweep and
fast subsequent ones.

**Rationale**:

- Sweeps two through five rebuild no images. FR-001 guarantees generated bytes
  never change, so the Docker build context is identical and every layer is
  cached. Only the `unmute` binary differs between sweeps — which is precisely
  what needs testing, since every finding in scope lives in the tool rather than
  in its output.
- Serial execution needs no port allocation, no container name collisions, and
  no interleaved logs. Parallelism would trade real complexity for wall-clock in
  a job that already runs unattended.

**Alternatives considered**:

- *Running examples in parallel*: rejected for now. If the first sweep proves
  painfully slow, the cheap fix is a port offset per worker, and nothing in this
  design blocks adding it later.
- *Sampling a subset per sweep*: rejected. The clarification chose the full
  sweep after every story, and a sampled sweep cannot deliver SC-010.

---

## Open items carried into tasks

| Item | Why it is not resolved here | Where it lands |
|---|---|---|
| `FIRECRAWL_MCP_URL` value | Needs a real Firecrawl URL, which only the maintainer can supply | Pre-work task, FR-038 |
| `multi-task` variable sources | One file read at implementation time; does not change the design | US-agnostic pre-work, R5 |
| Exact `ruff` version to pin | Read from the runner's environment when the baseline is captured | Pre-work task, R3 |

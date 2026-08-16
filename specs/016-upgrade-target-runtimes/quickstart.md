# Quickstart: Validating the Runtime Upgrade

**Feature**: 016-upgrade-target-runtimes | **Date**: 2026-08-16

How to prove this feature works, in the order that fails fastest. The automated
layers come first because they need no credentials; the human live call is last
because it is the one that actually closes FR-012.

## Prerequisites

- Go 1.26 and a built binary: `make build`
- Docker running (the only local run path after this change)
- `uv` for the smoke layer
- Provider credentials in `.env` for the live calls (`SLNG_API_KEY`,
  `OPENAI_API_KEY`, and per-example telephony credentials)

## 1. The gate, zero Python

```bash
make fmt && make lint && make build && make test
```

Expect green. `make test` now also runs the version agreement sweep, which fails
if any author-facing surface names a framework version that disagrees with the
recorded window.

## 2. The pin is real on both targets

The heart of P1. Compile a package per target and read the emitted dependency.

```bash
bin/unmute compile examples/simple-prompt
```

Check `build/<target>/pyproject.toml`:

- The LiveKit target declares `livekit-agents[...]==1.6.10`, not `>=1.5`.
- The Pipecat target declares `pipecat-ai[...]==1.7.0`.
- Neither carries a `console` optional-dependency group.

## 3. Out-of-range versions fail loudly

Edit a target's `version:` and confirm each message names the supported range.
All four must fail before any artifact is written.

```bash
bin/unmute validate examples/simple-prompt
```

| Declared version | Expected |
|---|---|
| `1.4.9` | below the floor, names `>=1.5.0, <=1.7.0` |
| `1.9.0` (pipecat) | above the ceiling, and says a newer unmute may support it |
| `1.6` | must name all three parts |
| `1.5.2` on a package with a warm transfer | names the warm transfer and its `>=1.6.0` floor |

The last row is the regression this feature exists to prevent: before the
change, that package validates green and silently installs a different version
than it declares.

## 4. The window is discoverable from the tool

```bash
bin/unmute --version
```

States both frameworks' ranges and the verification date. The same three facts
appear per code target in `build/<target>/compile-report.json`.

## 5. No deprecated run mode survives

```bash
grep -rn "agent.py dev\|agent.py console\|cli.run_app" build/ examples/ docs/ docs-site/ internal/generate/templates/
```

Expect no hits outside history records. Then confirm the removed flag explains
itself:

```bash
bin/unmute dev --console examples/simple-prompt
```

Expect an error that points at browser dev mode, not a bare unknown-flag
message.

## 6. Smoke: the emitted Python installs and imports at the ceilings

```bash
make smoke
```

This is the layer that catches constructor drift in a vendor SDK. It resolves
whatever the emitted `pyproject.toml` declares, so after this change both
frameworks install the declared version rather than LiveKit floating to whatever
is newest.

## 7. The human live call (FR-012)

The release gate. For **each** of the eleven examples: run it, hold a real
conversation, and exercise what that example exists to prove.

```bash
bin/unmute dev examples/<name>
```

For telephony examples, use the telephony path instead:

```bash
bin/unmute dev --telephony examples/<name>
```

What to check on every run:

1. The agent greets and holds a back-and-forth conversation.
2. The example's distinguishing behavior actually fires (a transfer completes, a
   handoff switches agents, a tool call returns, an outbound call connects).
3. For LiveKit: the browser opens on its own, which proves the readiness marker
   still appears under the new run command, and the container logs are readable
   colored output rather than JSON.
4. No deprecation warning appears in the container logs.
5. Ctrl-c tears the compose project down without hanging.

Record every result in [results.md](results.md): example, target, versions,
behavior exercised, pass or fail, date, and who ran it. **A release ships only
when every row reads pass.** A fail is a blocker, not a footnote.

## Watch items during verification

These come from research R8 and are cheap to notice while you are already on a
call:

- A CPU-starved container could stop accepting jobs under the new run mode's
  `load_threshold` of 0.7. Symptom: the browser connects but no agent joins.
- Ctrl-c during a live call could wait on draining. Symptom: teardown pauses for
  up to the compose timeout.

Neither is expected. Both have a named knob if they appear.

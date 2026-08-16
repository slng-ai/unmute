# Quickstart: Validating This Feature

**Feature**: 015-cwd-livekit-defaults

How to prove the change works, by hand and by test. Contract IDs (C1–C10) refer
to [contracts/cli-surface.md](contracts/cli-surface.md).

## Prerequisites

- Go 1.26 (the `go` directive in `go.mod`)
- Nothing else for C1–C10 and the automated gate. Docker only if you want to
  take `unmute dev` all the way to a spoken call.

## Build

```bash
make build
```

That writes `bin/unmute` with the version stamped at link time.

## The bug, before and after

This is the reproduction from the original report. Run it against `bin/unmute`
before the change and it fails on the third line:

```bash
cd "$(mktemp -d)" && "$OLDPWD/bin/unmute" init my-test && cd my-test && "$OLDPWD/../bin/unmute" validate
```

Before: `unmute: accepts 1 arg(s), received 0`.
After: a normal validate run against the current directory.

## Manual walkthrough

Use an absolute path to the binary so the working directory can move freely.

```bash
export UNMUTE="$PWD/bin/unmute"
cd "$(mktemp -d)"
```

**The in-folder path (C1, C6, C7)** — the workflow the user asked for:

```bash
$UNMUTE init my-test
cd my-test
$UNMUTE validate
$UNMUTE compile
```

Expect `✓ livekit (livekit)` from validate, a `LiveKit turn placement is a
preference` warning on stderr with exit 0, and `build/livekit/` written under
`my-test` by compile.

**The named-folder path (C2)** — must be unchanged:

```bash
cd .. && $UNMUTE validate my-test && $UNMUTE compile my-test
```

**Explicit argument wins (C3)**:

```bash
$UNMUTE init other && cd my-test && $UNMUTE validate ../other
```

Expect `other` to be the package validated, not `my-test`.

**The helpful failure (C4)**:

```bash
cd "$(mktemp -d)" && $UNMUTE validate
```

Expect exit 1, and a message that names `agent.yaml`, names this directory, and
shows both usage forms. Specifically: not `accepts 1 arg(s), received 0`.

**Explicit failure is unchanged (C9)**:

```bash
$UNMUTE validate /definitely-not-a-package
```

Expect today's wording: `validate /definitely-not-a-package: load: agent.yaml:
open ...: no such file or directory`.

**Every flag still works on `dev` (C8)**:

```bash
cd my-test && $UNMUTE dev --console --target livekit --var caller_name=Ada
```

**...and on `validate` and `compile` too (C8a)** — FR-004 covers all three
commands, not just `dev`:

```bash
cd my-test && $UNMUTE validate --target livekit && $UNMUTE compile --target livekit
```

**No parent search (C10)** — the safety case. From inside a build directory,
the command must fail rather than quietly recompile the package above it:

```bash
cd my-test/build/livekit && $UNMUTE compile
```

Expect exit 1 with the same C4 guidance, and confirm `my-test/build/livekit`
was not rewritten. An upward search here would have the tool overwrite the
directory you are standing in.

## LiveKit default

```bash
cd "$(mktemp -d)" && $UNMUTE init fresh && cd fresh
grep -A 3 '^targets:' targets.yaml
grep -A 4 '  turn:' agent.yaml
cat .env.example
```

Expect `provider: livekit` with `version: "1.5.2"`, a `turn.detector` block
naming `turn-detector-mini`, and `LIVEKIT_API_KEY` / `LIVEKIT_API_SECRET` /
`LIVEKIT_URL` alongside `OPENAI_API_KEY` and `SLNG_API_KEY`.

Pipecat must stay reachable. Switching a package by hand means editing **both**
`targets.yaml` and the turn block in `agent.yaml`; editing only `targets.yaml`
correctly fails with `turn model "silero" is not recognized`.

## Talking to it

```bash
cd my-test && $UNMUTE dev
```

Needs Docker. The dev LiveKit server runs in `--dev` mode with its placeholder
`devkey`/`secret` pair and `unmute dev` mints its own access token, so no
LiveKit Cloud project is needed to hear the agent (research.md D8). Reasoning
and speech still need real `OPENAI_API_KEY` and `SLNG_API_KEY` values in `.env`.

## The automated gate

Run in this order. All four must pass before the pull request:

```bash
make fmt && make lint && make build && make test
```

`make test` is `go test -race ./...` and needs zero Python. `make smoke` is the
constitution's fifth target but is opt-in, needs `uv`, and is never the pull
request gate.

## Goldens that must be regenerated

Regenerate deliberately, then read the diff before committing. Expect these to
move:

```bash
go test ./internal/cli -run TestHelpCaptureMatchesBinary -update
go test ./internal/scaffold -update
```

Those two, and no others. The scaffold golden changes because the default
package is now LiveKit; the help golden changes because the three `Use:`
strings now show an optional argument.

Verified as **not** affected, so do not regenerate them to "be safe":
`internal/tui/testdata/golden/console_models_80x24.txt` (its `target:
"pipecat"` is a literal fixture, not derived), and the `internal/generate` and
`internal/ir` goldens (sourced from `internal/testdata/safe_core` and the
catalogue, never from the scaffold). `-update-pipecat` and `-update-catalog`
are not needed (research.md D13).

Two assertions are hardcoded strings and need hand edits, not `-update`:

- `internal/cli/init_test.go:61` expects `✓ pipecat (pipecat)` from a fresh
  scaffold.
- `internal/tui/tui_test.go:528-542` (`TestRunSelectTarget`) selects the target
  menu **by ordinal** — its input sends `2` for LiveKit — so reordering that
  menu breaks it.

A third test, `TestRunTelephonyCreateGatedOnConnection`
(`internal/tui/tui_test.go:1019-1043`), also needs a hand edit, but only to its
**message** assertion — see the telephony section below. Its blocking
assertion must not be touched.

## Checking the wizard's telephony path

The default scaffold is browser-only, so it never exercises this. A
wizard-built phone package is refused on **both** targets, and that gate stays:

```bash
go test ./internal/tui -run TestRunTelephonyCreateGatedOnConnection
```

Measured on 2026-08-16: with the LiveKit default this test **fails**, and the
failure is expected and narrow. Its blocking assertion (`got.Confirmed` is
false) still passes on both targets — the gate works. Only its message
assertion breaks, because it pins Pipecat's wording while a different guard
fires first:

- Pipecat sets `daily-sip`, so the carrier is missing: `give the connection a
  carrier, or drop the phone channel`.
- LiveKit sets no transport, so the transport is missing:
  `connections/phone.yaml: connection "phone" declares no transport`.

Both name the file and the field to fix, so no guidance work is owed. Update
the message assertion to hold for both, leave the blocking assertion alone.

## Checking the console menus

Two different rules, easy to conflate:

```bash
go test ./internal/tui -run 'TestRunSelectTarget|TestDefaultTarget'
```

- A fresh `unmute init` with no name must highlight **LiveKit** (the create
  flow follows the scaffold default).
- Opening an existing **LiveKit** package to edit its target must highlight
  **LiveKit**. Use a LiveKit package for this check, not a Pipecat one:
  Pipecat sits at `options[0]` in `maintain.go:556`, so a Pipecat package looks
  right by coincidence today and proves nothing. The LiveKit case is the one
  that is actually broken before the fix (FR-011).

## The check that has no `-update`

`internal/generate/pipecat_carrier_telephony_test.go:228` asserts the literal
string `unmute compile <source-dir>` in the emitted runbook. If the emitted
README templates change, that assertion is a hand edit. The plan keeps
`<source-dir>` in emitted runbooks precisely so this stays still: their reader
is inside `build/<target>/`, not inside the package.

## Coverage gap this feature should close

`TestDocsSiteCLIPagesQuoteHelp` (`internal/cli/help_capture_test.go:76`) checks
only lines beginning with `-`, so it verifies flags and never the `Usage:`
line. The three `docs-site/reference/cli/*.mdx` pages therefore quote a usage
string nothing gates, and they would silently go stale on exactly this change.
Extending that test to assert the `Usage:` line is part of the work, per the
repository's own rule that a rule with no gate is a wish.

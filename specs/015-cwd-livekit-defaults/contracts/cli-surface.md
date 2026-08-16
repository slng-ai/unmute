# Contract: Command Argument Surface

**Feature**: 015-cwd-livekit-defaults

For a CLI the public contract is the command surface. This file states the
argument contract each command must honour after the change. Every row is
observable from a terminal, so every row is testable.

## The rule, stated once

> A package command takes at most one positional argument: the package
> directory. When it is omitted, the package directory is the current working
> directory. The current directory must itself contain `agent.yaml`; no parent
> directory is searched.

This sentence has one implementation (`packageDir` in `internal/cli`) and three
callers. It must not be restated in a second code path.

## Per-command contract

| Command | Args before | Args after | `Use` before | `Use` after |
|---|---|---|---|---|
| `validate` | `ExactArgs(1)` | `MaximumNArgs(1)` | `validate <package-dir>` | `validate [package-dir]` |
| `compile` | `ExactArgs(1)` | `MaximumNArgs(1)` | `compile <package-dir>` | `compile [package-dir]` |
| `dev` | `ExactArgs(1)` | `MaximumNArgs(1)` | `dev <agent-dir>` | `dev [agent-dir]` |
| `init` | `MaximumNArgs(1)` | unchanged | `init [name]` | unchanged |
| `skill` | `NoArgs` | unchanged | `skill` | unchanged |

Square brackets are cobra's and this repo's existing convention for an optional
argument (`init [name]`, `internal/cli/init.go:16`).

Brackets alone are **not** sufficient. They say the argument may be omitted;
they do not say what happens when it is, and spec.md's help-text edge case
requires the help to state the default. None of the three commands has a
`Long:` field today, so each gains one naming the current directory as the
default. An earlier draft of this contract claimed brackets stated optionality
"without extra prose"; that contradicted the spec and is withdrawn.

## Behavioural contract

| # | Invocation | Cwd | Expected |
|---|---|---|---|
| C1 | `unmute validate` | a package | validates the cwd package, exit 0 |
| C2 | `unmute validate my-test` | parent of `my-test` | validates `my-test`, exit 0, output unchanged from today |
| C3 | `unmute validate ../b` | package `a` | validates `b`; the explicit argument always wins |
| C4 | `unmute validate` | not a package | exit 1, message names `agent.yaml`, names the directory, shows both usage forms |
| C5 | `unmute validate a b` | any | exit 1, cobra usage error (unchanged) |
| C6 | `unmute compile` | a package | writes `build/<target>/` inside the cwd, exit 0 |
| C7 | `unmute dev` | a package | dev session on the cwd package |
| C8 | `unmute dev --target x --console --var k=v` | a package | every flag behaves as with an explicit directory |
| C8a | `unmute validate --target x` / `unmute compile --target x` | a package | the `--target` flag behaves identically with no positional argument (FR-004 covers all three commands, not just `dev`) |
| C9 | `unmute validate /missing` | any | exit 1 with today's existing error text, unchanged |
| C10 | `unmute compile` | inside `build/<target>/` of a real package | exit 1 with the C4 guidance message. The parent is **not** searched and **not** recompiled |

C10 is the safety case, not a curiosity. It is the one where an upward search
would silently rewrite the directory the author is standing in, so it is the
rule most worth a test rather than a code comment.

C9 is deliberate: the explicit path keeps its current wording. Only the
zero-argument path gains the new guidance, because only the zero-argument path
has to explain what directory it chose and why.

## Error text contract

Zero-argument failure (C4) must contain, in one message:

1. the file that was looked for: `agent.yaml`
2. the directory that was checked, as an absolute path
3. both ways to run the command

It must not be cobra's `accepts 1 arg(s), received 0`, which is what the
command prints today and what the reported bug is about.

## Scaffold default contract

| Fact | Before | After |
|---|---|---|
| `scaffold.DefaultTarget` | `"pipecat"` | `"livekit"` |
| `targets.yaml` provider written by `unmute init <name>` | `pipecat` | `livekit` |
| `targets.yaml` version | `"1.5.0"` | `"1.5.2"` (already set by `SetTarget`) |
| `agent.yaml` turn block | `turn.vad` / `silero` | `turn.detector` / `turn-detector-mini` (already templated) |
| `.env.example` keys | `OPENAI_API_KEY`, `SLNG_API_KEY` | adds `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET`, `LIVEKIT_URL` |
| First `unmute validate` on a fresh package | `✓ pipecat (pipecat)` | `✓ livekit (livekit)` |
| Pipecat still selectable | yes | yes, unchanged |
| Console **create** menu preselect | Pipecat | LiveKit (ordered by the constant) |
| Console **maintain** menu preselect | Pipecat, regardless of the package | the package's own current target (FR-011) |
| Wizard phone-route transport | `daily-sip` on Pipecat, and gated on carrier | unchanged; gated on carrier on both targets, with guidance of equal quality (FR-010) |

All "after" values in this table were observed by running a patched binary in
an isolated export, not predicted. See research.md D6.

## What does not change

- Exit codes: 0 success, 1 error.
- Warnings go to stderr and still exit 0. A fresh LiveKit scaffold emits
  `LiveKit turn placement is a preference`; that stays (research.md D7).
- `unmute init` still takes a name and still creates that subdirectory. It does
  not scaffold into the current directory.
- The emitted `build/<target>/README.md` keeps its explicit `<source-dir>`
  argument. Its reader is standing in `build/<target>/`, not in the package, so
  the cwd default does not apply there and dropping the argument would make
  those instructions wrong.

# Data Model: Upgrade Target Runtimes and Make Version Support Scalable

**Feature**: 016-upgrade-target-runtimes | **Date**: 2026-08-16

This feature adds no authoring fields and no runtime state. Everything below is
compile-time data: one table the binary carries, one value the author already
writes, and one record a human fills in before release.

## 1. Support window (new, one per framework)

The single recorded home FR-003 asks for. It lives in `internal/target`, the
leaf package both `internal/ir` (validate) and `internal/generate` (both
drivers) already import, replacing `driverVersions` in
[driver.go:29-35](internal/target/driver.go:29).

| Field | Type | Meaning |
|---|---|---|
| `Floor` | version triple | Oldest version this release's templates compile against. Stays `1.5.0` for both frameworks. |
| `Ceiling` | version triple | Newest version a human has verified end to end. `1.7.0` for Pipecat, `1.6.10` for LiveKit. |
| `VerifiedOn` | date | When the ceiling was verified. Same convention as `Entry.Verified` in the catalogue ([catalog.go:99](internal/target/catalog.go:99)). |

Rules:

- One row per code target provider. `vapi` and `deepgram` have no driver, so
  they have no row and `CheckVersion` keeps returning nil for them, exactly as
  today.
- `Floor <= Ceiling` always. A row that breaks this is a programming error and
  the agreement test fails on it, rather than shipping a window nothing can
  satisfy.
- The ceiling is a claim about verification, not about upstream. A newer
  upstream release simply is not supported until a human verifies it and a new
  unmute ships. This is what ties the range to the unmute version (FR-007).

**Why the shape changes.** Today's `[2]int{major, minMinor}` cannot express a
ceiling and has no date. It also compares only the minor, so `1.99.0` passes and
the patch is parsed and thrown away. A ceiling of `1.6.10` is meaningless
without patch comparison, so the type must widen.

## 2. Feature floor (new, derived table)

The facts that today live only as an emission side effect in
[livekit_v1_build.go:1300-1307](internal/generate/livekit_v1_build.go:1300),
where using a feature silently rewrites the author's declared version.

| Field | Type | Meaning |
|---|---|---|
| `Feature` | enum | What the package uses. Two entries today: warm transfer, MCP tool source. |
| `Provider` | provider | Which driver the floor applies to. Both entries are LiveKit; Pipecat has none. |
| `Minimum` | version triple | Oldest framework version where the emitted code for that feature is verified. `1.6.0` for both. |

Rules:

- A feature floor only ever raises the **required** version. It never changes
  what gets installed. That is the whole correction: the author's declared
  version is the pin, and a floor it does not meet is an error, not a silent
  rewrite.
- A package using a feature whose floor exceeds the declared version fails
  validation, naming the feature, the floor, and the declared version (FR-004).
- The upper bound disappears. Today warm transfer emits `>=1.6,<1.7`; research
  R4 establishes that `<1.7` is a verified-window marker, not a known
  incompatibility, and the support window's ceiling now carries that meaning for
  every feature at once.

## 3. Declared version (existing, unchanged shape, new force)

The `version:` field on a code target in `targets.yaml`
([package.go:365-368](internal/spec/package.go:365)). Its name, place, and
format do not change (FR-010). What changes is that it now decides the install
on both targets instead of one.

| Rule | Today | After |
|---|---|---|
| Required on code targets | yes | yes, unchanged |
| Must parse as a version | yes, partial versions allowed (`1.5`) | yes, all three parts required (see research R6) |
| Must sit inside the support window | floor only | floor and ceiling |
| Must satisfy every used feature's floor | not checked | checked, named error |
| Pipecat install | exact `==` pin | unchanged |
| LiveKit install | ignored, emits `>=1.5` | exact `==` pin |

## 4. Verification record (new, one per release)

What FR-012 requires a human to fill in: proof each example holds a real
conversation at the ceiling versions. Recorded in
`specs/016-upgrade-target-runtimes/results.md`, matching the convention spec 013
set with its own `results.md`.

| Field | Meaning |
|---|---|
| Example | The package under `examples/`. |
| Target(s) | Which code target(s) the run exercised. |
| Versions | The framework versions installed for the run. |
| Distinguishing behavior | What this example exists to prove (transfer, handoff, tool call, outbound call, telephony route). |
| Result | Pass or fail, with a note on anything surprising. |
| Date and person | Who ran it and when. |

A release ships only when every row reads pass. A fail is a blocker, not a
footnote.

## Relationships

```text
Support window (per framework)
  ├── bounds ──> Declared version (per code target)
  │                   └── becomes ──> the exact install pin in the generated project
  ├── seeds  ──> Scaffold default (unmute init writes the ceiling)
  ├── printed in ──> compile report + version output (FR-007)
  └── proven by ──> Verification record (per example, per release)

Feature floor (per feature, per provider)
  └── raises the minimum for ──> Declared version, when the package uses that feature
```

## What this model deliberately does not add

- No per-version template sets, no version-conditional emitters. One template
  set per framework covers the whole window, which research R1 and R2 verified
  rather than assumed.
- No new authoring field. Authors keep writing one `version:` string.
- No network lookup and no runtime version check. The window is data compiled
  into the binary, which is what "tied to the unmute version" means here.
- No mirrored Python upper bound. livekit-agents declares `<3.15` itself;
  copying it into emitted projects would create a second home for a fact
  upstream owns (research R10).

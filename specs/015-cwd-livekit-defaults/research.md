# Phase 0 Research: Work Inside The Agent Folder, LiveKit By Default

**Feature**: 015-cwd-livekit-defaults
**Date**: 2026-08-16

All findings below were checked against the code in this worktree, and the two
behavioural claims were proven by running the binary. Nothing here is from
memory.

## D1: What the current commands actually do

Measured, not assumed:

```
$ unmute validate
unmute: accepts 1 arg(s), received 0

$ unmute validate /tmp/definitely-not-a-package
unmute: validate /tmp/definitely-not-a-package: load: agent.yaml: open /tmp/definitely-not-a-package: no such file or directory
```

The first message is the whole bug the user reported. Cobra rejects the call
before any Unmute code runs, so there is nowhere today to say anything useful.

Current argument declarations:

| Command | File | `Args` | `Use` |
|---|---|---|---|
| `validate` | `internal/cli/validate.go:19` | `cobra.ExactArgs(1)` | `validate <package-dir>` |
| `compile` | `internal/cli/compile.go:26` | `cobra.ExactArgs(1)` | `compile <package-dir>` |
| `dev` | `internal/cli/dev.go:37` | `cobra.ExactArgs(1)` | `dev <agent-dir>` |
| `init` | `internal/cli/init.go:18` | `cobra.MaximumNArgs(1)` | `init [name]` |

`init` is already optional-arg and is not changing.

**Decision**: change the three package commands to `cobra.MaximumNArgs(1)` and
resolve the directory in one shared helper.

**Rationale**: `MaximumNArgs(1)` keeps the "no more than one positional"
guarantee, so a two-argument typo is still a usage error. Nothing else in the
tree depends on `ExactArgs`.

**Alternatives rejected**: a `--dir` flag (adds a second way to say the same
thing, and the user asked for the positional form); `cobra.ArbitraryArgs` plus
a manual count check (reimplements what cobra already does).

## D2: Where the default-to-cwd rule lives

**Decision**: one unexported helper in `internal/cli`, called by all three
commands:

```go
// packageDir resolves the package directory from an optional positional arg.
// No argument means the current directory, so an author can cd into a package
// and work there. cmd supplies the command name for the failure message.
func packageDir(cmd *cobra.Command, args []string) (string, error)
```

**Signature corrected 2026-08-16.** An earlier draft took only `args`. That
cannot work: the D4 message names the command twice (`validate: no
agent.yaml...` and `unmute validate <package-dir>`), and a helper holding only
`args` has no idea which of the three commands called it. Passing `cmd` (whose
`Name()` and `UseLine()` supply both) is what makes FR-002 and contract C4
reachable. Caught in audit before implementation.

**Also settle**: the message quotes the directory as an absolute path, so the
helper calls `filepath.Abs`. That can fail, and the constitution requires
errors wrapped with `%w` and matched with `errors.Is`/`As`. On an `Abs`
failure, wrap and return rather than falling back to a relative path in a
message that promises an absolute one.

**Rationale**: Constitution Principle III forbids a second copy of a fact. The
rule "no argument means the current directory, and here is what to say when
that directory is not a package" is one fact. Copying it into three `RunE`
bodies is exactly the duplication that principle exists to stop, and it is how
the three commands would drift apart later.

**Alternatives rejected**: resolving inside `spec.Load` (it is a library
function with no opinion about a CLI's cwd, and it would change the error text
for the explicit form, breaking D4); a cobra `PersistentPreRunE` on the root
(it would run for `init` and `skill`, which take no package).

## D3: Whether to walk up to a parent directory

**Decision**: no. The current directory must itself contain `agent.yaml`.

**Rationale**: git-style upward search is the obvious alternative and it is
worse here. `compile` writes to `build/<target>/` inside the package, so an
upward search would let a user standing in `build/livekit/` silently recompile
the parent and rewrite the directory they are standing in. A rule you can
state in one sentence ("the folder you are in is the package") has no
surprising cases.

**Alternatives rejected**: search upward until `agent.yaml` is found (surprising
writes, as above); search upward but stop at a git root (same problem, more
rules to explain).

## D4: The failure message when there is no package

**Decision**: the friendly, instructive message applies to the zero-argument
path only. The explicit-argument path keeps today's error text unchanged.

The zero-argument failure must name the file, the directory, and both usage
forms, per FR-002. Target shape:

```
unmute: validate: no agent.yaml in the current directory (/Users/x/work)
  run this from inside an agent package, or name one: unmute validate <package-dir>
```

**Rationale**: the two cases need different help. If you typed a directory, you
know what you meant and the existing message already names the path. If you
typed nothing, you need to be told what the default was, which is precisely the
information the current `accepts 1 arg(s), received 0` withholds. Keeping the
explicit path byte-identical also satisfies FR-003 at zero test cost.

**Alternatives rejected**: one unified message for both paths (changes existing
error text, so it would need edits to tests that assert today's wording for no
user benefit); letting `spec.Load`'s raw `no such file or directory` stand for
the cwd case (this is the fail-loud-and-usefully rule in Principle II, and a
bare fs error on the most common first-run mistake fails it).

## D5: The header line when the argument is omitted

`printHeader` (`internal/cli/ui.go:43`) prints `validate <dir>`. With no
argument, `dir` is `.`, so the banner would read `validate .`.

**Decision**: print the base name of the resolved absolute path instead of `.`.

**Rationale**: this feature exists entirely to make the first five minutes feel
right, and `SLNG// validate .` reads like something went wrong. The cost is one
small helper. Note `printHeader` only writes when the writer is a TTY
(`ui.go:44`), so no golden file or captured-output test sees this string. The
change is free of test churn.

**Alternatives rejected**: leave `.` (honest, unambiguous, and slightly ugly on
the exact screen this feature is meant to fix); print the full absolute path
(noisy).

## D6: Does a LiveKit-default scaffold actually work?

This was the one real risk in the feature, so it was measured rather than
reasoned about. In an isolated `git archive` export of HEAD (the working tree
was never modified), `DefaultTarget` was flipped to `livekit` and a package
scaffolded with plain `unmute init probe`:

```
=== validate (untouched scaffold) ===
✓ livekit (livekit)
Warnings:
  livekit: LiveKit turn placement is a preference

=== compile (untouched scaffold) ===
generated probe/build/livekit/agent.py
generated probe/build/livekit/Dockerfile
generated probe/build/livekit/compose.dev.yaml
generated probe/build/livekit/pyproject.toml
...
```

**Finding**: the change is genuinely close to a one-constant flip. The scaffold
is already target-aware everywhere it needs to be:

- `Data.SetTarget` (`internal/scaffold/scaffold.go:312`) already has a
  `case "livekit"` that sets `TargetVersion = "1.5.2"` and
  `SDKLanguage = "python"`, and both targets share the same SLNG/OpenAI starter
  bindings (`scaffold.go:330-349`).
- `agent.yaml.tmpl` already branches: pipecat gets `turn.vad` with `silero`,
  livekit gets `turn.detector` with `turn-detector-mini`. Both are valid for
  their target.
- `.env.example` correctly switched to `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET`,
  `LIVEKIT_URL` alongside `OPENAI_API_KEY` and `SLNG_API_KEY`.

**Decision**: flip `DefaultTarget` to `"livekit"`. Do not restructure the
scaffold.

**Scope limit on this finding**: the probe covers the *default* scaffold, which
is browser-only. It does **not** cover the interactive wizard's telephony path,
which has a real gap — see D11. "One constant flip" is true for
`unmute init <name>`, not for the whole feature.

**A trap worth recording**: a first attempt at this probe edited only
`targets.yaml` to say `livekit` and left `agent.yaml` alone. That fails
validation with `turn model "silero" is not recognized`. This is the tool
behaving correctly, not a defect, but it means a user who hand-edits only
`targets.yaml` to switch targets hits a real error. It also means any test
fixture that flips a provider must flip the turn block with it.

## D7: The fresh-scaffold warning

A LiveKit scaffold validates with `LiveKit turn placement is a preference` on
stderr, exit 0. Every new user will see it on their first `validate`.

**Decision**: leave it. Do not suppress or reword it as part of this feature.

**Rationale**: Principle II says a warning is never a silent downgrade and must
not be promoted to a pass by hiding it. Making the first run look tidier by
hiding a true warning is exactly the trade that principle forbids. Recorded
here so the next reader knows it was considered rather than missed.

## D8: `unmute dev` on LiveKit needs no extra credentials locally

`internal/cli/dev_web.go:28` documents that the containerized dev
`livekit-server` runs with `--dev` and its placeholder `devkey`/`secret` pair,
and `internal/cli/livekit_token.go` mints access tokens in-process with no
LiveKit SDK. So `unmute dev` on a fresh LiveKit scaffold works without the
author first provisioning a LiveKit Cloud project.

**Consequence**: FR-006 ("scaffolded package runs under dev with no manual
edits") is satisfiable for LiveKit, and the LiveKit default does not push a
credential requirement onto the first run.

## D9: Test strategy

**Decision**: L2 in-process command tests using `t.Chdir(dir)`, plus one
negative test for the no-package message.

**Rationale**: `t.Chdir` is already used in this repo
(`internal/tui/tui_test.go`, 15+ call sites), restores the directory
automatically, and works under `go test -race`. Go forbids `t.Chdir` in a
parallel test, which is fine: none of the affected tests call `t.Parallel`.

Care needed: existing `internal/cli` tests reach fixtures by relative path such
as `filepath.Join("..", "testdata", "safe_core")` (`validate_test.go:12`). A
test that chdirs must resolve its fixture path to an absolute path *before*
chdir, or copy the fixture into `t.TempDir()` first, which several tests
already do.

**Confirmed safe**: no existing test asserts the current zero-argument
rejection. A grep for `ExactArgs`, `accepts 1 arg`, and
`arg(s), received 0` across `internal/cli/*_test.go` returns nothing, so
relaxing the arity breaks no assertion.

## D10: Scope check against the constitution

- **No `docs/SCHEMA.md` amendment is required.** Neither change touches the
  authoring surface: no field is added, removed, renamed, or retyped, and no
  existing package changes its strict-decode result. `targets.yaml` still
  carries the same `provider:` field; only the value the scaffold pre-fills
  changes.
- **No constitution amendment is required.** Principle V describes what the
  four commands do, not how their arguments are shaped, and the LiveKit default
  is compatible with the "SLNG is the vendor the scaffold binds by default"
  rule, which already names `livekit-plugins-slng`.
- **The five-places rule does apply**, because emitted and scaffolded text
  teaches these commands. Confirmed hits inside the scaffold templates alone:
  `templates/agent.yaml.tmpl:1-2` teaches `unmute validate .` and
  `unmute compile .`; `templates/env.example.tmpl:2-3` teaches `unmute dev .`
  and `unmute compile .`. Those trailing dots are the workaround this feature
  removes, so they become argument-free.

## D11: The telephony path — twice mis-analysed, finally measured

`internal/scaffold/scaffold.go:383`:

```go
if d.Transport == "" && d.Target == "pipecat" && d.UsesPhoneRoute() {
    d.Transport = "daily-sip"
}
```

There is no LiveKit arm. The same shape is repeated in the wizard at
`internal/tui/tui.go:2170-2172`. The surrounding comment explains why the
default is conditional: set unconditionally in `SetTarget`, its only observable
effect on a browser-only package was a `DAILY_API_KEY` line in `.env.example`
asking a first-time author for a credential nothing in their package reads.

**CORRECTED 2026-08-16.** The first version of D11 was wrong on both halves of
its premise, and the fix it proposed would have made things worse. An
independent audit caught it and the correction below is verified in code.

**What is actually true:**

1. **The wizard + phone path does not work today either.** Every registered
   route carries a non-empty carrier (`internal/target/telephony.go:133, 145,
   154, 165, 181, 218, 227, 248, 311, 360`), and `ResolveTelephonyFeature`
   (`:508-513`) is an exact map lookup, so no carrier-less key resolves on any
   target. The existing test says so in its own comment
   (`internal/tui/tui_test.go:1022-1026`): "Create is correctly gated until
   someone chooses a carrier."
2. **`Transport = "sip"` would not have fixed it.** `SetTarget` resets
   `Carrier = ""` (`scaffold.go:315`), so the key would be
   `(livekit, sip, "")`, which still misses.
3. **It would actively degrade the error.** `internal/ir/build.go:881` fires a
   helpful, specific message before the capability lookup, but only for
   transports on the carrier-less list, and that list
   (`internal/ir/build.go:956-958`) contains exactly one entry: `daily-sip`.
   So Pipecat gets "give the connection a carrier, or drop the phone channel",
   while `sip` would fall through to the bare
   `unsupported telephony route (livekit, sip, )`. The comment at
   `build.go:875-879` exists precisely to prevent that class of unhelpful
   error.

**MEASURED 2026-08-16, third pass.** Reasoning about this area produced a wrong
answer twice, so it was finally run. In an isolated export with only
`DefaultTarget` flipped:

```
--- FAIL: TestRunTelephonyCreateGatedOnConnection
    tui_test.go:1041: wizard did not surface the telephony route gate:
    connections/phone.yaml: connection "phone" declares no transport. A
    connection is a phone route, and the transport is the mechanism that
    carries the call
```

Two things this settles, both contrary to earlier drafts:

- **The author never sees `unsupported telephony route (livekit, , )`.** An
  earlier guard fires first (`internal/ir/build.go:101`), because with no
  transport at all the code stops before any capability lookup. So the second
  correction, which promised that bare note, was also wrong.
- **LiveKit's message is already good.** It names the file, the connection, and
  the missing field, and explains what a transport is. There is no guidance to
  improve, so no guidance work is owed.

**Final decision**: add no arm and write no new message. The one real
consequence is that `TestRunTelephonyCreateGatedOnConnection` fails, because
its second assertion pins one target's wording (`"cannot receive them"`).

**Is updating it loosening a gate?** No, and the distinction matters. The test
makes two assertions: that Create is blocked, and that a specific string
appears. The **blocking assertion still passes** — the gate does its job on
both targets. Only the message assertion is target-coupled, and it is coupled
to a guard that no longer fires first. Rewriting that half to be target-aware,
while keeping the blocking assertion untouched, tightens the test rather than
weakening it. Deleting or relaxing the blocking assertion would be the
forbidden move; this is not that.

**Why the D6 probe missed it**: the default scaffold is browser-only, so
`UsesPhoneRoute()` is false and this branch never runs. The probe was right
about what it tested and silent about what it did not. The lesson is recorded
in the plan's risk table: an empirical probe proves only what it exercised.

## D12: The console preselect is positional, not derived (corrects an earlier claim)

An earlier draft of this plan asserted that flipping `DefaultTarget` also flips
the interactive console, because `internal/tui/tui.go:120` seeds the wizard
with `data.SetTarget(scaffold.DefaultTarget)`. **That is wrong**, and it was
checked rather than assumed:

- The target menu builds its options with Pipecat first and LiveKit second
  (`internal/tui/tui.go:231-233`).
- `selectOne` preselects `options[0].Value` unconditionally, on both the
  interactive path (`tui.go:3223`, `initial: options[0].Value`) and the
  accessible path (`tui.go:3232`, `choice := options[0].Value`). It never
  consults `data.Target`.

So after flipping the constant, the console would highlight Pipecat while
building a LiveKit package. The same ordering exists in the maintain flow
(`internal/tui/maintain.go:554-559`).

**Decision**, and the two flows need opposite fixes — do not apply one rule to
both:

- **Create flow** (`tui.go:231-233`): order the target options so
  `scaffold.DefaultTarget` comes first, and add an agreement test that fails
  when the menu's first option stops matching the constant.
- **Maintain flow** (`maintain.go:554-563`): do **not** order it by the
  constant. It edits an existing package, so it preselects `data.Target`, which
  `editTarget` already holds (it compares against it on line 560). Ordering
  this one by the default would make editing a Pipecat package highlight
  LiveKit — the regression FR-011 exists to forbid.

Note what today's code hides: with Pipecat at `options[0]`, the maintain flow
looks correct for a Pipecat package purely by coincidence. The case that is
already broken today is opening a **LiveKit** package, which highlights
Pipecat. So a test for this must open a LiveKit package; one that opens a
Pipecat package passes before and after the fix and gates nothing.

**Rationale**: reordering alone is the two-line fix, but it creates a second
home for "which target is the default" (the constant, and the menu order), and
Principle III is explicit that where one fact is stated twice an agreement test
is mandatory. The test is what keeps this from silently drifting the next time
either side changes.

**Alternatives rejected**: threading a preselect value through `selectOne`
(it is shared by many menus, so this is a wide change for one caller); leaving
the order alone and accepting a menu that contradicts the package being built
(the exact class of user-visible inconsistency this feature exists to remove).

## D13: Which goldens actually move

Checked individually rather than regenerating everything and reading the diff:

| Golden | Moves? | Why |
|---|---|---|
| `internal/scaffold/testdata/golden/init.txt` | **Yes** | Contains the whole scaffolded package: turn block, `targets.yaml`, `.env.example`. Update with `go test ./internal/scaffold -update`. |
| `specs/008-mintlify-user-docs/help.txt` | **Yes**, for the other change | The three `Use:` strings appear in it. Update with `go test ./internal/cli -run TestHelpCaptureMatchesBinary -update`. Unaffected by the LiveKit flip, since `init` has no `--target` flag. |
| `internal/tui/testdata/golden/console_models_80x24.txt` | **No** | Its `target: "pipecat"` is a hand-written literal in the `fieldReq` fixture (`console_golden_test.go:20`), not derived from `DefaultTarget`. An earlier draft wrongly listed this for regeneration. |
| `internal/generate/testdata/golden/pipecat_v1.txt`, `catalog_resolution.txt`, `internal/ir/testdata/golden/compiler.txt` | **No** | Sourced from `internal/testdata/safe_core` or the catalogue, never from the scaffold. `-update-pipecat` and `-update-catalog` are not needed. |

Two tests break on hardcoded strings and need hand edits, not `-update`:

- `internal/cli/init_test.go:61` asserts `"✓ pipecat (pipecat)"` on a freshly
  scaffolded package.
- `internal/tui/tui_test.go:528-542` (`TestRunSelectTarget`) drives the
  accessible wizard with the positional input `"1\nagent\n1\n1\n2\n7\n\n"`,
  where the `2` selects the target menu **by ordinal**, and asserts the result
  is `livekit`. Reordering that menu (D12) makes `2` select Pipecat.

**Corrected 2026-08-16.** An earlier draft named
`TestRunTelephonyCreateGatedOnConnection` here instead, on the premise that
D11's LiveKit arm would change its expectation. There is no such arm (D11,
corrected), and that test is a working gate on a genuinely unsupported route:
its expectation must **not** be edited. If its message moves, the code gets
fixed. Its real location is `internal/tui/tui_test.go:1019-1043`, not
`1008-1041`.

Tests that use `scaffold.DefaultTarget` symbolically follow the flip on their
own and need no edit (`internal/scaffold/scaffold_test.go:134`,
`internal/cli/init_test.go:156`, and ten sites in `internal/tui/tui_test.go`).

## D14: A cosmetic inconsistency the flip creates

`PlatformEnv()` (`scaffold.go:572-577`) is deliberately excluded from
`DeclaredSecrets()`, so a LiveKit scaffold's `.env.example` grows to five keys
while `agent.yaml`'s `secrets:` block still lists two. That is intended, but
`docs-site/start/quickstart.mdx:47-50` currently tells the reader these are
"the same two `agent.yaml` lists under `secrets:`", which stops being true.

**Decision**: fix the sentence in `docs-site/`. Do not change the code. The
separation is deliberate and documented at `scaffold.go:567-577`.

## Out of scope, noted so it is not lost

`.specify/memory/constitution.md` states "Go 1.24, pinned in `go.mod`", but
`go.mod` says `go 1.26` and `CLAUDE.md` says Go 1.26. That is pre-existing
document drift, unrelated to this feature, and fixing it here would bury a
constitution edit inside a CLI bugfix. Flagging only.

# Results: the real numbers

Written at the end, from what was run rather than from what was planned. Raw
counts. A bar that was missed is reported as the number it is.

---

## What shipped

Two commits on `013-first-five-minutes`, both green on
`make fmt && make lint && make build && make test`:

| Commit | Content |
|---|---|
| `1f4a6d5` | User Stories 1, 2, and 3: the silent drops, the scaffold, the environment names |
| `9763f61` | User Story 5 and User Story 4: the rules stated in `docs/` and `docs-site/`, tracing off the first-run path, the examples |

### One deviation from the task plan, and why

`tasks.md` asked for **one commit per defect group** in Phase 3, so Wave B could
be given a single commit each. That was not done, and the reason is worth
recording rather than hiding.

The groups do not separate at file granularity. `internal/generate/livekit_v1.go`
carries both group E's value-check delegation and User Story 3's `AuthorEnv`
field; `internal/ir/validate_test.go` carries the red tests for groups B and E;
`internal/testdata/safe_core/` had to gain a SIP connection for group B, which
moved goldens that groups C and E also move. Splitting would have produced
commits that do not pass the gate, and "the gate is green on every commit" is the
stronger of the two rules.

Wave B loses nothing by this. What matters for an independent verifier is that
the agent did not write the fix it is checking, not that the commit is minimal;
each Wave B agent was given the reproduction and the tree, and reproduced from
scratch.

---

## Phase 2: the red tests

**Sixteen test tasks produced eleven test functions. Ten were red. One was green
from birth.**

| Test | File | State when written |
|---|---|---|
| `TestUnreachableControlIsRefused` | `internal/ir/build_test.go` | red, 9 of 10 rows |
| `TestColdTransferNeedsARoute` | `internal/ir/validate_test.go` | red on LiveKit; the Pipecat half passed already, which is the point of it |
| `TestSecretsCheckRunsWithNoBlock` | `internal/ir/variables_test.go` | red, 2 of 4 subtests |
| `TestValueChecksFailAtValidate` | `internal/ir/validate_test.go` | red, 8 of 8 |
| `TestPipecatImageMeetsThePlatformContract` | `internal/generate/pipecat_deploy_test.go` | red |
| `TestEnvExampleListsOnlyAuthorNames` | `internal/generate/env_test.go` | red, 3 of 5 subtests |
| `TestScaffoldAgreesWithItsOwnBuild` | `internal/scaffold/scaffold_test.go` | red |
| `TestScaffoldPromptMatchesItsChannel` | `internal/scaffold/scaffold_test.go` | red |
| `TestOneModelIdEverywhere` | `internal/skill/agreement_test.go` | red on all three of the things it must catch |
| `TestNoVendorVariableWearsTheUnmutePrefix` | `internal/target/table_test.go` | red |
| `TestNoUnmuteEnvOnTheBeginnerPath` | `internal/skill/agreement_test.go` | **green** |

The eleventh being green is not a failure of the test. Reproduction G measured
the beginner path and found zero `UNMUTE_*` hits across the site index, all of
`docs-site/start/`, all of `docs-site/build/`, the root `README.md`, the
scaffold, and every byte of the bundle. The test locks a state that already
held, and nothing was holding it there before. It is reported as green rather
than dressed up as a fix.

`TestColdTransferNeedsARoute` replaced a test named
`TestColdTransferWithoutAConnectionStillValidates`, which asserted the defect.
That is the only test this feature inverted rather than added.

---

## What each group cost, measured

| Group | The fix | Goldens moved |
|---|---|---|
| A, unattached declarations | one function in `internal/ir/build.go` | 0 |
| B, cold transfer with no route | six lines in `validateHumanTransfer`, plus a SIP connection for the `safe_core` fixture | 0 functional; the fixture change moved none |
| C, the secrets check | the guard removed, provider keys added to the reference set, `REQUIRED_ENV` derived | `compiler.txt` (the new warning), 2 LiveKit goldens (the startup check a package with no block now gets) |
| D, the container | one conditional `COPY` line | 0, exactly as research D4 predicted |
| E, the eight value checks | facts moved to `internal/target`, mirrored in `ir.Validate` | 0 |

Group D's prediction is worth keeping: research D4 said a conditional copy would
move zero goldens because the golden's fixture has no local tools, and an
unconditional one would move one. That is what happened.

---

## The model identifier sweep

The census in research D10 measured 80 occurrences of two identifiers across 42
tracked files, of which 24 were author-facing.

**22 author-facing files were swept.** The other two the census counted were
`internal/scaffold/scaffold.go` and its golden, which are covered by the
constant and by `-update` rather than by the sweep. Zero author-facing
occurrences of `gpt-4o-mini` or `gpt-4.1-mini` remain, held by
`TestOneModelIdEverywhere`, which fails on three things and not one: a stale
identifier, the combined `openai/gpt-5.6-luna` form, and a `temperature` on any
think model.

### T043c: what else the examples forward

Audited across all eleven `agent.yaml` files and their `targets.yaml` overrides:

| Parameter | Occurrences after the sweep |
|---|---|
| `temperature` | 0 |
| `top_p` | 0 |
| `top_k` | 0 |
| `max_tokens` | 0 |
| `reasoning_effort` | 9, all `minimal`, all on a think model |

Nothing unverified survives on a think model. The `params:` blocks that remain
elsewhere are `speed` on a speak model and `eot_threshold` on a Deepgram listen
override, both of which are that vendor's own documented settings.

---

## Phase 7: the examples

**11 of 11 validate. 11 of 11 compile, for every target each declares.**

```
livekit-human-transfer        validate=ok compile=ok
mcp-example                   validate=ok compile=ok
multi-task                    validate=ok compile=ok
outbound-reminder             validate=ok compile=ok
pipecat-human-transfer-daily  validate=ok compile=ok
pipecat-human-transfer-twilio validate=ok compile=ok
salon-support                 validate=ok compile=ok
simple-prompt                 validate=ok compile=ok
subagents                     validate=ok compile=ok
task-groups                   validate=ok compile=ok
twilio-telephony-hello        validate=ok compile=ok
```

### T073: the container regression, proven in a container

Not by reading the emitted Dockerfile. `examples/salon-support/build/pipecat`
was built with `docker build` and run:

```
$ docker run --rm <image> sh -c "ls /app && python -c 'import tools.book_appointment, tools.check_availability'"
... tools ...
tools import OK

$ docker run --rm <image> python -c "import bot"
bot import OK
```

`import bot` is the exact line that raised `ModuleNotFoundError: No module named
'tools'` in reproduction.md section D. All five structural examples emit the
`COPY tools/ ./tools/` line; a package with no local tools does not, which is
what makes it conditional.

### T072: the row-uniqueness bar

FR-027 says an example whose table row makes no claim another row does not
already make is deleted. Read across all eleven rows:

**Zero deletions.** The closest three are the transfer examples, and each names a
distinct mechanism: `livekit-human-transfer` is warm on a SIP trunk (the only
route that compiles warm), `pipecat-human-transfer-twilio` is cold over Media
Streams with nothing hosted, `pipecat-human-transfer-daily` is cold on a
Daily-provisioned number with no carrier account at all. `salon-support` and
`outbound-reminder` both touch variables, and their rows claim different halves:
variables without secrets, and both ways a secret reaches a tool.

### T067: tracing

Was on 4 of the 11 examples. Now on 1: `simple-prompt`, named in the table as
the tracing example. The other three lost the block **and** the three orphaned
`LANGFUSE_*` declarations in their `secrets:` lists, which is the half that
mattered: a declared secret reaches `REQUIRED_ENV`, so those packages would have
refused to start without three values they no longer used.

`examples/salon-support`, the one marked "Start here", now needs two keys.

---

## T050: `unmute init` to a running container

Run end to end from a clean scratch directory.

```
$ unmute init hello-agent
created hello-agent/agent.yaml
created hello-agent/.env.example
created hello-agent/instructions.md
created hello-agent/targets.yaml
created hello-agent/tools/end_call.yaml

$ unmute validate hello-agent
✓ pipecat (pipecat)

$ cat hello-agent/.env.example          # names only
OPENAI_API_KEY=
SLNG_API_KEY=

$ cat hello-agent/build/pipecat/.env.example   # names only
OPENAI_API_KEY=
SLNG_API_KEY=

$ docker run --rm -e OPENAI_API_KEY=probe -e SLNG_API_KEY=probe <image> \
    python -c "import bot; bot.require_env()"
two variables are enough: require_env passed

$ docker run --rm -e OPENAI_API_KEY=probe <image> python -c "import bot; bot.require_env()"
RuntimeError: Missing required environment variables: SLNG_API_KEY
```

Two variables are enough, the two files agree, and the startup check still fails
loud naming exactly what is missing.

**What is NOT verified here, stated plainly:** nobody has spoken to it. T050
asks for one real spoken exchange heard by a person, and SC-003 asks for
`reasoning_effort: minimal` to be judged by ear. Neither was done. A container
that starts and a check that passes is not a greeting anybody heard. That is the
open item this document is most explicit about, and it is the one that decides
whether `minimal` stays or drops to `none`.

---

## T062 and T063: the validator's error strings

Every error-returning site in `internal/ir/validate.go` was enumerated
statically: **36 literal strings and roughly 110 formatted ones**, of which
about 15 formatted entries are site labels rather than errors (`agent.yaml
destinations %s`, `tools/%s.yaml webhook.url_env`). Call it **130 error strings**.

They fall into four classes, and only one of them was a real documentation gap:

| Class | Count, approximate | Documented? |
|---|---|---|
| Field-shape rules (tool blocks, model kinds, task and group fields, variable types) | ~80 | Yes, as field tables in `docs-site/reference/agent-yaml.mdx` and `docs/SCHEMA.md` §4–§5. The rule is documented; the exact string is not quoted, which is the right trade for 80 strings |
| Capability gates (`applyCapability`) | ~20 | Yes. Each carries the provider's own note from `internal/target/table.go`, which is the documented rulebook |
| Telephony plan invariants (`telephony coordination must be shared`, `telephony services must be non-empty and unique`, and the rest) | ~20 | **No, and deliberately.** These assert the compiler's own route data against itself. No authored package can reach them; a failure means a bug in `internal/target/telephony.go`, not a mistake by an author. Documenting them would tell authors about states they cannot produce. This is the same judgement reproduction.md section E made about the thirteen generator sites it found already gated |
| The rules this feature added or PR #80 left undocumented | 4 | **Yes, now.** `capacity.peak_starts_per_second`, the warm-transfer `outbound: true` rule, the cold-transfer route rule, and the unattached-declaration refusal |

**The method's limit, stated rather than hidden.** Wave A proved reachability on
the generator side by mutating a package per candidate and observing the output,
which is what caught that 13 of 90 generator sites were already gated and could
never fire. That method was **not** run for all 130 validator strings here. The
static enumeration is complete; the per-string reachability probe is not. The
telephony-invariant class above is asserted unreachable by reading rather than
by mutation, and a future session that wants the count exact should probe it.

---

## T083: dependencies

`go.mod` and `go.sum` are byte-identical to `16289f4`. No dependency was added.

---

## Open, and owned by a person

1. **Speak to it.** T050's spoken exchange and SC-003's judgement of
   `reasoning_effort: minimal` by ear. If the agent pauses before each reply,
   the recorded fallback is `none`, and that is a one-line change to
   `internal/scaffold/scaffold.go`.
2. **`make smoke`.** T080.
3. **Rotate the ElevenLabs key.** The private `.env` files carried it under a
   name no shell can export (`11LABS_API_KEY`); both were renamed to
   `ELEVENLABS_API_KEY` on 2026-08-15, and the value was echoed into a shell
   session before that, so it should be rotated on the ElevenLabs dashboard.
   Nothing in this repository reads it today.

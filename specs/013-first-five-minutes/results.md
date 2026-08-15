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
| `126e996` | What Wave B and `make smoke` found, acted on |
| the last one | What Wave C's seventeen agents found, acted on |

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

| Parameter | After the sweep | After `make smoke` |
|---|---|---|
| `temperature` | 0 | 0 |
| `top_p` | 0 | 0 |
| `top_k` | 0 | 0 |
| `max_tokens` | 0 | 0 |
| `reasoning_effort` | 9, all `minimal`, all on a think model | **0** — see the `make smoke` section |

**No think model in this repository forwards any generation parameter.** The
`params:` blocks that remain are `speed` on a speak model and `eot_threshold` on
a Deepgram listen override, both of which are that vendor's own documented
settings.

That is a stronger result than the audit was aiming for, and it is not a
tidiness win: `reasoning_effort` was there for a reason, and it came out because
a container proved the driver cannot carry it.

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

## Wave B: three independent verifiers

Each was given the tree and its own scratch directory, told to build packages
itself and report what it observed. None of them wrote the code it checked.

### Agent 1 — the reachability check and the cold-transfer route

**9 of 9** claimed cases behaved as claimed, including the control case: an
unreferenced `models:` entry still exits 0. **4 of 4** cold-transfer cases held.

It then tried to break the check, which is the part worth reading:

- **Zero false positives.** Every legitimately-reachable shape it could
  construct exits 0: an agent reached only through a two-hop `agent_transfer`
  chain, an agent reached only through a task group's `then_target`, a control
  listed only in a task's `tools:`, a task reached only through a reachable
  group's `steps:`.
- **Zero false negatives** inside the nine categories. Four probes that a
  "mentioned anywhere" implementation would have passed were all caught: two
  agents that transfer to each other but neither is reachable from the entry
  agent; an agent that transfers to itself; a control listed only in an
  unreachable agent's tools; a control listed only in an unreachable task's
  tools.

Three observations it made that are true and are being kept rather than fixed:

1. **Only one unreachable declaration is reported per run.** N of them take N
   runs. Marked with a `// ponytail:` comment naming the upgrade path.
2. **The reported line can land on a preceding comment**, because `Location`
   finds the first line containing the token. Cosmetic; the same is true of
   every other tier-1 message in the repository.
3. **Two of the eight messages name no fix** (the destination case and the
   unreachable-agent case). That matches the message contract, which specifies
   the fix clause for controls and tools only.
4. It also noted that unused `variables:` and unused `secrets:` entries are not
   caught. True, and out of scope: neither costs the author anything at
   runtime, unlike an unreferenced destination, which reached the startup check.

### Agent 2 — the secrets check, the container, and the eight value checks

- **8 of 8** value checks now fail at `validate` with exit 1 and the message.
  `compile` still fails too, so the generators' backstops are intact.
- **4 of 4** secrets variants behaved as claimed, all at exit 0.
- The container claim **held**, proven by building the image and running
  `import bot` inside it, and by checking that the two packages with no local
  tools emit no `COPY tools/` line.

**And it found a real defect the claim had missed.** It pasted a
credential-shaped string into seven slots that take an environment variable
name. Four refused it without repeating it. **Three printed it back verbatim**:
a `secrets:` entry, a connection's `environment:` value, and a `destinations:`
value — which is a phone number. All three were `ir.Build` messages, outside the
`ir.Validate` path the original claim was about.

That is a real leak: the refusal put a credential in a terminal, a CI log, and
any bug report pasted from either. Fixed in `126e996`, with three tests holding
it. **The narrow claim held; the broad one did not, and the broad one was the
one that mattered.**

### Agent 3 — the bundle

**15 of 15** agreement tests pass, including `TestBundleNamesNoSitePage`, which
it confirmed is not passing vacuously: `sitePages` fatals on an empty walk, and
it scans 51 site pages against 12 bundle reference files, finding 0 hits. It
independently grepped for `Documentation:` lines and `docs.slng.ai`: 0 each.

**0 stale claims** in the three bundle files this feature changed, verified
against the code rather than against the diff — including reproducing the
secrets warning and a fresh `unmute init` to check the two claims about them.

**One gap it found:** no bundle file mentioned the new
unattached-declaration refusal, so an assistant would have met it without
warning. Closed in `126e996`.

---

## T080: `make smoke`

**Failed on the first run, and that is the most useful thing in this document.**

Nine tests failed on one cause and one on another.

**Nine: `reasoning_effort: minimal` cannot be forwarded on Pipecat.** The driver
lowers a think model's `params:` into `OpenAILLMService.Settings(...)`, which is
a fixed field set:

```
TypeError: OpenAILLMSettings.__init__() got an unexpected keyword argument 'reasoning_effort'
```

and `ty` refuses the emitted file for the same reason:

```
error[unknown-argument]: Argument `reasoning_effort` does not match any known parameter
   --> bot.py:183:13
```

Research D10 chose the parameter to stop a reasoning model pausing before it
speaks, and reasoned carefully about whether OpenAI accepts it. The driver never
gets as far as OpenAI. It works on LiveKit, whose reason row forwards params as
extra kwargs, and that is not enough for a package that compiles for both — and
Pipecat is the scaffold's default target.

**Removed from the scaffold and all eleven examples.** This is D10's recorded
fallback, taken for a different reason than the one it anticipated. The
mitigation it was there to provide is gone with it, which raises the stakes on
the listening test that has not been done: if the agent pauses audibly, the fix
is now a driver change rather than a config line.

**One: two smoke fixtures encoded the defects this feature fixed.** One dropped a
transfer control and left its destination behind. The other derived its
container environment from `.env.example`, which by design no longer lists the
names the route supplies, so `/readyz` demanded `UNMUTE_PUBLIC_URL` and nothing
set it. That second one is exactly the failure research D14 predicted for a
human operator, arriving first in a test — which is where it should arrive. The
fixture now reads the telephony plan's complete required-env, the same list
`compile-report.json` carries.

**After both fixes: `make smoke` green, all ten packages.** Docker images built,
containers started and waited on.

---

## Wave C: seventeen clean-room agents

Five first-run, ten authoring, two adversarial. Each had its own scratch
directory, none could edit the repository, and each was told to report what it
observed rather than what it expected.

### SC-005, the authoring bar: **10 of 10**

The bar is 8 of 10 validating clean on **first attempt**. PR #80 measured 7 of
10 and shipped without re-running it. This is the re-run, on the same ten briefs
— salon, hotel, vet, gym, bank, restaurant, utility, dental, pizza, helpdesk —
each agent installing the bundle with `unmute skill install` and reading nothing
else.

| Brief | First attempt | Attempts to clean | Compile |
|---|---|---|---|
| salon | PASS | 1 | ok |
| hotel (telephony, warm transfer) | PASS | 1 | ok |
| vet (webhook + auth) | PASS | 1 | ok |
| gym (conversation variables) | PASS | 1 | ok |
| bank (two agents, handoff) | PASS | 1 | ok |
| restaurant (task group) | PASS | 1 | ok |
| utility (outbound telephony) | PASS | 1 | ok |
| dental (local Python tool) | PASS | 1 | ok |
| pizza (conversation tuning) | PASS | 1 | ok |
| helpdesk (MCP) | PASS | 1 | ok |

**10 of 10, every one on the first attempt, with zero iterations.** Several
agents went further and probed the rules the bundle claims, confirming each
refusal string matched the binary character for character.

None of them has heard its agent. Every one said so unprompted, which is the
answer the bundle's own build loop teaches.

### What the ten found wrong in the bundle

Ten passes is not ten clean bills of health. They found eleven inaccuracies, and
the four that would have cost someone real time are fixed:

- **`conversation.md` described the LiveKit turn-placement warning as conditional
  on a `placement:` field.** It fires on every LiveKit target with a turn entry.
  Two agents independently reported hunting for a field they had never written.
- **`conversation.md` said a greeting naming a `conversation` variable is
  refused.** It is not, when the variable has a `default` — what decides it is
  the value, not the source. The page would have talked an author out of a legal
  package.
- **`unmute init` and the reference examples disagree on model entry names.**
  The scaffold writes `vad`; every override example in the bundle is keyed
  `detector`. Copying the documented LiveKit override onto a scaffolded package
  fails. Three agents hit this. `models.md` now says the key is your package's
  name and that `init` picks a different one.
- **No bundle file said an unattached control is a build error.** Added to
  `transfers.md` and `orchestration.md`, with the refusal quoted.

Also fixed: `tools.md` now says that an optional `input` property is always
passed as `""` (a handler testing `is None` misbehaves at run time and nothing
catches it), that `output:` is a contract with the model that nothing enforces,
and that a webhook's `url_env` and `auth.token_env` also belong in `secrets:`.

### The five first-run agents

Between them they tested 53 factual claims made by the files `unmute init`
writes, plus every step of three documentation pages, plus ten deliberate
beginner mistakes.

**The good result first: zero files had to be edited outside what `unmute init`
wrote.** One agent verified that with a `diff -r` against a pristine scaffold.
Two environment variables, consistent across all five places that name them, and
the emitted image built and imported.

**What they found, and what was done:**

| Finding | Action |
|---|---|
| `agent.yaml` and `.env.example` both promise `.env` is gitignored; `unmute init` wrote no `.gitignore`, so following the instructions and running `git add -A` staged the key | **Fixed**: `init` writes one, and a test holds the promise |
| The scaffolded `instructions.md` ended with a line addressed to the author, which is the system prompt, so the model was told to replace a file it cannot see — directly under a rule saying never to read its instructions out | **Fixed**: that advice moved into an `agent.yaml` comment, and a test refuses author-facing text in the prompt |
| `docs-site/index.mdx` showed an agent with `model: silero` and a `targets.yaml` with both targets, and said "It validates for both targets". It does not | **Fixed**: the page shows the per-target override and explains why it is there |
| The turn-model refusal printed `livekit: livekit turn model ...` — the target name twice, because the message predates the check moving to validate | **Fixed**: the message no longer carries its own prefix |
| `quickstart.mdx` says a container with missing keys "stops with the names it did not find". It did not: `require_env()` ran per session, so the container reported healthy, the browser got a valid answer, and the failure landed in a background task | **Fixed**: the check runs at import, so the container refuses to start |
| Three documentation pages quoted error strings this feature changed | **Fixed** |
| `your-first-agent.mdx` said "three things taken out" and had taken out four | **Fixed** |
| `SLNG_API_KEY` appears in 22 places on the site and no page says where to get one | **Open** — a real gap, and not this feature's to close |
| `unmute compile` prints its caveats to stdout, interleaved with the file list | **Open** — a pre-existing violation of the warnings-to-stderr rule |
| `compile-report.json` names one referrer for a key used twice | **Open**, cosmetic |
| Internal spec ids (`N15`, `V10/B3`, `B15`) leak into files handed to a stranger | **Open** — worth a pass of its own |

### The two adversarial agents: **9 defects, all fixed**

Both were told to produce a package that compiles green and does nothing. Both
succeeded, which means User Story 1 was not finished when the first commits
landed. The spec says every one they find must be fixed, and every one is.

**Agent 1 — five fields declared and dropped, all Pipecat:**

1. `conversation.interruption.enabled: false` was computed and rendered nowhere.
   The author says "the caller cannot barge in" and gets a fully interruptible
   agent, while the LiveKit build from the same source honours it. Now lowers to
   Pipecat 1.5's `AlwaysUserMuteStrategy` — **verified by introspecting the class
   inside a built image before emitting it**, which is the lesson
   `reasoning_effort` taught earlier the same day.
2. `conversation.inactivity.end_after` was computed and rendered nowhere. An idle
   call was nudged forever and never hung up; on a phone line that is a billed
   open call.
3. Every Pipecat `agent_transfer` and `delegate` without a `when:` emitted an
   empty docstring. The docstring is the only thing the model reads when it
   decides to call the tool, so the control was present in the file and dead at
   run time. The default was computed and the template rendered the raw field.
4. `ring_timeout` was ignored and 25 seconds hardcoded. `ring_timeout: 7s`
   produced output byte-identical to declaring nothing.
5. A whitespace-only `when:` defeated the default on **both** drivers, so writing
   spaces was strictly worse than writing nothing.

Agent 1's root-cause note is the durable part: the `pipecatEmittedFields`
agreement test guarantees a field is never validate-green-but-dropped, **per
capability `Field` constant** — and all three of its finds live where no `Field`
constant exists (`interruption.enabled` is tagged core, `inactivity.end_after`
hides under a coarser row, `ring_timeout` has none). The test is sound; its
coverage is the gap.

**Agent 2 — four contradictions between the generated project and itself**, all
one root cause: env names were classified by string-matching against every
connection in the package rather than by what this target's code reads.

6. A two-target package's Pipecat build demanded the **LiveKit** target's four
   SIP trunk values before it would accept a phone call — names its own emitted
   code never reads once. It survived `--target`. Reproduces on the shipped
   `examples/twilio-telephony-hello`.
7. The LiveKit build's Compose file injected the Pipecat target's Twilio REST
   credentials.
8. A token used by both a connection and a webhook tool was stripped from the
   startup check and from `compose.dev.yaml`, so the emitted code raised
   `KeyError` on the first tool call — after a check that passed and a comment
   promising "every address and token a tool names".
9. A model provider key mapped by a connection was dropped from
   `compose.dev.yaml`, so `unmute dev` started a container that could never
   construct the LLM.

Fixed by recording provenance rather than re-deriving it: the env set now knows
which names the emitted code reads on every session, and those are never treated
as route names; and a target no longer inherits another target's connection
names from the package-wide `secrets:` block.

Both agents also listed what they probed and could **not** break, which is worth
as much as the finds. Between them that covers unreferenced variables, unused
task results, tool input schemas, `required_controls`, tracing on a target
without it, fallback chains, history shaping, `thinking_audio`, two agents
sharing a prompt file, every README filename and port, and `generated_files`
against the files on disk.

### One thing a prompt of mine got wrong

The utility-brief agent was asked to confirm that "an outbound telephony channel
needs an `on_voicemail` policy". It checked, found no such rule, and said so:
the real dependency runs the other way (`on_voicemail` requires `outbound:
true`). The prompt was wrong and the agent was right. Recorded because a
verification wave that only ever confirms the asker is not a verification wave.

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

1. **Speak to it.** T050's spoken exchange and SC-003's judgement by ear. This
   is now the largest open item, and `make smoke` made it larger: the scaffold
   ships with no reasoning-effort setting at all, because Pipecat cannot carry
   one, so if a reasoning model does pause before each reply there is no config
   line to change. The fix would be a driver change — teaching the Pipecat
   reason row to forward unknown params outside `Settings(...)` — and nobody
   should do that work until somebody has heard the pause.
2. **Rotate the ElevenLabs key.** The private `.env` files carried it under a
   name no shell can export (`11LABS_API_KEY`); both were renamed to
   `ELEVENLABS_API_KEY` on 2026-08-15, and the value was echoed into a shell
   session before that, so it should be rotated on the ElevenLabs dashboard.
   Nothing in this repository reads it today.
3. **The per-string reachability probe for the validator**, described in T062
   above. The enumeration is complete; the probe is not.

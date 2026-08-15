# Phase 0: research and decisions

Every decision below was taken against the measured evidence in
[reproduction.md](./reproduction.md), not against the brief's description of it.
Where the two disagree, the evidence wins and the disagreement is stated.

---

## D1. The unreachable-declaration check goes in `ir.Build`, not `ir.Validate`

**Decision.** Add the reverse of `checkToolRefs` to `internal/ir/build.go`.

**Rationale.** FR-004 proposed `ir.Validate` so the check fires once for both
targets rather than twice in two drivers. `ir.Build` is strictly better on that
same argument and on one more:

- It fires **once for the package**, not once per target. Whether a declaration
  is reachable is a property of the package graph alone; no target can change
  the answer.
- It is the only stage that can satisfy FR-001's requirement to name the file
  and line. The repository has three error tiers, and only `spec.Load` and
  `ir.Build` carry a position (see D7). An `ir.Validate` error prints as
  `livekit: <message>` with no location.
- The forward check already lives there. `internal/ir/build.go:1325`,
  `checkToolRefs`, walks exactly this graph in the opposite direction and
  already has the `missing()` helper that formats
  `<file>:<line>: <kind> %q does not resolve`. The new check is its mirror
  image, in the same file, using the same helper shape.

**Alternatives rejected.**

- *`ir.Validate`, as FR-004 said.* Loses the file and line for no gain, and
  re-answers a target-independent question once per target.
- *A per-driver check in each generator.* This is what produces the defect: the
  generators are where the control is silently skipped. Two copies of one rule
  is what Principle III forbids.

**Carve-out that must not break.** An unreferenced `models:` entry stays legal.
`docs/SCHEMA.md:287` calls the models map "a palette: entries that nothing
currently references are legal". That wording is scoped to `models:` and to
nothing else; controls, destinations, and top-level tools get no such status.

**Severity: error, not warning.** No finished package has a reason to declare a
control nothing reaches, and the cost of the current silence is concrete: an
unreferenced destination's environment name still reaches `.env.example` and the
generated `REQUIRED_ENV`, so the agent refuses to start over a phantom secret it
will never use.

---

## D2. The browser-only transfer refusal extends an existing guard, and corrects its comment

**Decision.** Widen the existing warm-transfer guard in `validateHumanTransfer`
(`internal/ir/validate.go:1148`) to cover cold, with mode-specific wording.

**Rationale.** The guard is already there, ten lines from where the defect sits,
and its own comment contains the reasoning error:

```go
// Only when the provider itself allows warm, so a provider that denies it
// keeps failing in its own words (principle II). Cold is unaffected: it acts
// on the caller's existing leg and dials nobody.
if control.Mode == TransferWarm && len(row.Errors) == before {
    row.Errors = add(row.Errors, "warm transfer needs a telephony Connection: ...")
}
```

"Cold is unaffected: it acts on the caller's existing leg and dials nobody" is
half right. The author reasoned about **credentials** — warm dials, so it needs
SIP credentials; cold does not dial, so it needs none — and drew the correct
conclusion from that premise. What it misses is that cold needs a **leg**, and a
session that never arrived by phone has none. The generated code already knows
this: its `caller is None` branch names "a browser session" as the usual cause.

So the fix is roughly six lines in one function: drop the `Mode == TransferWarm`
condition, pick the message by mode, and rewrite the comment to say what is
actually true of each mode.

The `len(row.Errors) == before` guard is kept exactly as it is. That is what
makes Pipecat keep failing in its own words — Pipecat's table row has already
added `Pipecat cold transfer requires Daily SIP transport`, so the generic
message never appends on top of it. Principle II's "quote that provider's own
vocabulary" survives untouched.

**Alternatives rejected.**

- *Make the LiveKit `ColdTransfer` capability row `controlTransport("sip", ...)`.*
  This works — `controlTransport` takes a free-text note, and with no connection
  the transport is empty so the condition fails and the row gates. It was
  rejected because the table row is not wrong: LiveKit cold transfer genuinely
  is supported on `(livekit, sip)`, and the table describes route support. The
  incomplete thing is the guard for packages that have no route at all. Editing
  the row to express "and also you need a route to exist" overloads the rulebook
  with a fact that is not about routes.
- *A new check elsewhere in the validator.* A second place that knows this rule,
  ten lines from the first. Principle III.

---

## D3. The secrets cross-check needs three changes, not one

**Decision.** Remove the guard, widen the reference set, and derive the startup
check from what the compiler requires.

1. **Remove the guard.** `undeclaredSecretWarning`
   (`internal/ir/validate.go:1388`) returns early on `len(agent.Secrets) == 0`.
   The guard tests the **declaration** list, so the package with the most to
   report — declares nothing, references seven names — takes the same early
   return as the package with nothing to report. Its sibling,
   `unusedConnectionWarning` at `:1365`, guards on the **subject** set and is
   correct; the shape to copy is already in the file.

2. **Widen the reference set.** Removing the guard alone does not make a fresh
   `unmute init` package see the check work, which is the whole point of
   FR-014. `referencedEnvNames` (`validate.go:1301-1352`) collects only `*_env`
   fields, Langfuse keys, handler `os.environ` reads, connection `environment:`
   values, and `destinations:`. A model provider's API key is in none of those,
   so a scaffolded package's reference set is **empty** and the check stays
   vacuous. The provider key environment name already has one home — the
   catalogue `Entry`'s key-env field in `internal/target/catalog_*.go` — so the
   fix is to read it from there, not to add a second list.

3. **Derive the startup check.** `docs-site/reference/secrets.mdx:107` promises
   "the generated agent refuses to start without them", and today the generated
   `REQUIRED_ENV` and `require_env()` vanish entirely when no block is declared,
   and a name left undeclared drops out of the check that is supposed to catch
   it. Build `REQUIRED_ENV` from the names the compiler already knows it
   requires — the same set that populates `required_env` in the compile report —
   rather than from the author's declaration list.

**Severity stays a warning with exit 0**, exactly as `docs/SCHEMA.md` N24
fixes it. No package that compiles today starts failing.

**Doc tension resolved in the document's favour.** N24 calls declaring secrets
"opt-in" and `SCHEMA.md:244` lists the block as not required. Neither says the
**warning** is opt-in — "opt-in" is given as the reason the severity is a
warning rather than an error. The code read it as "no block, no check". Under
Principle IV the document wins, and no amendment is needed because the document
already says the right thing.

**Fallback if item 3 proves expensive.** If deriving `REQUIRED_ENV` moves more
goldens than the value justifies, the honest alternative is to amend
`secrets.mdx` to say the startup check is generated from what you declare. That
is a worse product but a true document. It is not the default.

---

## D4. The container copy is conditional, and the fixture that hid it gets a tool

**Decision.** `{{if .LocalTools}}COPY tools/ ./tools/{{end}}` in the
non-telephony branch of `internal/generate/templates/pipecat_v1/Dockerfile.tmpl`,
keeping the named-file discipline the template's own comment explains.

**Two corrections to the brief.**

- This is a **regression**, not a permanent gap. The template was one unbranched
  definition using `COPY . .` until `6a5e77f` on 2026-08-13 split it in two.
  All three published tags carry the break. It matters because it changes what
  "prove it works" means: the target is the behaviour that existed before, not a
  new capability.
- It regenerates **zero** goldens, not "every Pipecat golden". Only one golden
  contains Dockerfile content, and its fixture has no local tools, so a
  conditional copy leaves it untouched. An unconditional copy would move one
  file. The conditional form is both more correct and cheaper.

**The fixture is the actual defect.**
`internal/generate/pipecat_deploy_test.go:66-102` asserts `COPY bot.py ./` is
present and `COPY . .` is absent — against `pipecatArtifact(t, nil)`, a package
with no local tools. It tests the letter of the container contract and never the
thing the contract is for. The fixture gains a local tool, and the assertion
becomes "every emitted `.py` the entrypoint imports is reachable inside the
image", which is the invariant rather than a spelling.

**Blast radius**: five of the ten examples that declare a Pipecat target,
including `salon-support`, which the table marks "Start here". Because
`compose.dev.yaml` has no bind mount, `unmute dev --target pipecat` runs the
same broken image, so the browser demo path is affected and not only cloud
deploys.

---

## D5. Eight generator-only value checks move to `ir.Validate`

**Decision.** Mirror all eight, and move the one fact that would otherwise exist
twice.

The reported turn-model defect is one member of a confirmed class of eight, each
of which lets a package pass `validate` at exit 0 and fail `compile`: the
LiveKit turn detector model, `sdk_language`, three `pins` checks, two target
`version` checks on both drivers, and a speak entry whose vendor has no voice
slot. The list and the probing method are in
[reproduction.md](./reproduction.md) section E.

**The constitution does not license the gap.** Principle V's sentences about
`validate` reach versus `compile` reach are about the **provider set**, not
about depth: "It MUST work for every declared provider whether or not that
provider's driver ships"; "This is the only command whose reach is all four
providers"; "Generation reaches only the providers with a shipped driver". The
Technology section repeats it as "Validation is deliberately wider than
generation. All four providers validate, because portability has to be checkable
before an author commits to a platform." The 1.1.0 sync note that introduced the
sentence describes it as provider reach. Nothing permits skipping a value check
for a provider validate fully serves.

**The repository already decided this**, for the sibling field, at
`internal/ir/validate.go:954-957`: "The generator errors on a slotless entry;
mirror it here so a slotless language fails validate, not just generate (C6:
gate before any artifact)." Two more `service_call.go` checks are mirrored the
same way. These eight were omissions, not a boundary.

**One home for the recognised turn models.** The set
`{turn-detector-mini, turn-detector}` currently exists only inside
`livekitTurnVersion` (`internal/generate/livekit_v1_build.go:1056`). Mirroring
the check without moving the set would create the second copy Principle III
forbids, so the set moves to `internal/target/catalog_livekit.go` — the
capability rulebook — and both validate and generate read it from there. The
generator keeps its error as a defence-in-depth backstop; it just stops owning
the fact.

**Sharpest member.** Number 8 is not a typo check: `docs/SCHEMA.md` §4.3 lists
`voice` as **required** on every speak entry, so authoring a `deepgram` speak
model necessarily produces a package that validates green and cannot compile.

---

## D6. `docs/SCHEMA.md` needs a deliberate amendment for the turn model

**Decision.** Append a dated, numbered amendment rather than change the code to
match the document or the document to match the code silently.

`docs/SCHEMA.md:317` specifies the field as `model` (no, **for example
`silero`**) — the schema's own example is the value that fails. §4.3 states
identities are "forwarded to the provider as-is and **never validated** (D10)".
The LiveKit driver contradicts both. Meanwhile `turn-detector-mini` and
`turn-detector` appear in no `docs/` or `docs-site/` page at all; they exist only
in four examples' `targets.yaml` override blocks and in one Go error string.
Delete those overrides and those four examples break.

Under Principle IV the document wins, and Principle IV also says an amendment
must be appended with a date and a number and must state whether existing
packages fail strict decode. So the amendment records: LiveKit constrains this
one field against a known set; the set is named; `silero` is not in it and the
example is corrected; Pipecat still forwards the value unchecked, so the same
`agent.yaml` is legal on one target and not the other, and the refusal names the
target.

**Alternative rejected.** *Make LiveKit accept `silero`.* It is not a turn
detector on that platform — it is a VAD. Accepting it would compile something
that cannot work, which is the class of defect this whole feature removes.

---

## D7. Three error tiers, and what "move the check" actually buys

Recorded because the spec's first draft claimed something false, and any future
change to these checks will hit the same trap.

| Tier | Stage | What the author sees |
|---|---|---|
| 1 | `spec.Load`, `ir.Build` | `connections/twilio_voice.yaml:21: "banana_key" is not accepted by route (pipecat, cloud-websocket, twilio); it accepts account_sid, auth_token, from_number` |
| 2 | `ir.Validate` | `livekit: <message>` — target-prefixed, **no position** |
| 3 | `generate` | bare message, no prefix, no position, after validate already said the package was fine |

The locator is `spec.Package.Location(file, token)` at
`internal/spec/package.go:22`, and `Generate` never receives a `*spec.Package`:

```go
func Generate(agent *ir.Agent, resolved ir.Target, caps target.Table) (Artifact, error)
```

So moving a check from tier 3 to tier 2 buys "fails before any artifact is
written". It does **not** buy a file and line. Only a check that can be
expressed against the authored document, before target resolution, reaches
tier 1 — which is why D1 puts the reachability check in `ir.Build` and D5 leaves
the eight in `ir.Validate`.

---

## D8. The scaffold gets no channel branch, because it has only one channel

**Decision.** Write the prompt and greeting for web audio, and delete the phone
framing. Do not add a channel switch.

`internal/scaffold/scaffold.go:422`, `AllChannels()`, hardcodes
`Kind: "realtime_audio"`, and `DefaultChannel` is `"web"`. There is no scaffold
path that produces a telephony package, so a `switch channel` would have one
arm reachable and one dead. The mechanism defect is real but smaller than it
looks: `withDefaults` sets `Instructions` at `:291` **before** it sets `Channel`
at `:294`, so the channel is not merely ignored, it is unreadable at that point.

Marked with a `// ponytail:` comment naming the ceiling: add the branch when a
scaffold path can emit a telephony channel, and the reorder is the prerequisite
that makes it possible.

**`Transport = "daily-sip"` is deleted outright.** It is a phantom: it is never
rendered into any scaffolded file — `targets.yaml` carries no `transport:` line,
and no `connections/` file is written because the package uses no phone route.
Its only observable effect anywhere is the `DAILY_API_KEY` line in the root
`.env.example`.

---

## D9. The two `.env.example` files are reconciled by a test, not a refactor

**Decision.** Keep both files, keep both templates, and add one test that
compiles the scaffolded package and asserts the two agree.

They share no code today: the root file renders
`internal/scaffold/templates/env.example.tmpl` from `scaffold.Data.RequiredEnv()`
(pre-spec, wizard struct); the generated one renders the per-driver template
from the resolved `ir.Agent`. The root file reads `Data.Transport`, a field the
IR never sees because the scaffold never writes it to disk. That is structurally
why they can disagree.

Unifying them means either teaching the scaffold to run the full load-build-
generate path at `init` time, or teaching the generator to emit into the package
root. Both are real refactors for a file that is three lines long. Once D8
removes the phantom transport, the two sets are identical by construction, and a
test is what keeps them that way — the shortest thing that fails if the bug
returns.

Deleting the root file was considered and rejected: `unmute dev` reads a `.env`
at the package root, so a new author needs something to copy there.

---

## D10. `gpt-5.6-luna`, set by the author, and what follows from it being a reasoning model

**Decision.** One identifier, `gpt-5.6-luna`, with one Go home and one test.
Authored as two fields. `reasoning_effort: minimal` in `params:`. No
`temperature`.

**Superseded.** This decision originally read "`gpt-4.1-mini`, against the raw
count", chosen on document rank because every front door already said it. The
author set `gpt-5.6-luna` in the 2026-08-15 clarification session, which makes
the front-door argument moot: neither incumbent survives. The census below is
kept because it is what sizes the sweep, not because it still decides anything.

**Verified 2026-08-15 against OpenAI's own API documentation.** `gpt-5.6-luna`
is in the Chat Completions supported-model list. The family has three tiers:
`gpt-5.6-sol` for frontier capability, `gpt-5.6-terra` for a balance of
intelligence and cost, `gpt-5.6-luna` for "efficient, high-volume workloads",
described elsewhere as "optimized for faster, more cost-effective responses"
with "lower cost and reduced latency". The bare `gpt-5.6` alias defaults to
`sol`, so the tier is named explicitly everywhere.

**Two fields, never one string.** `docs/SCHEMA.md` N15 rejected folding the
provider into the model string by name, because "the forwarded model identity is
not uniform across vendors (OpenAI wants `gpt-4.1-mini`, SLNG wants
`slng/deepgram/nova:3-en`), so a parse would mangle what reaches the SDK". A
`model:` of `openai/gpt-5.6-luna` is forwarded verbatim and fails at the
provider. The agreement test catches the combined form as well as a stale
identifier.

**`reasoning_effort: minimal`.** This is a reasoning family, and OpenAI's own
migration guidance is to hold the reasoning effort and then "evaluate success
metrics including latency". A voice turn that pauses to think before speaking is
the thing this feature exists to avoid shipping. `reasoning_effort` is a
documented Chat Completions parameter taking
`none | minimal | low | medium | high | xhigh | max`, and Unmute already
forwards a think model's `params:` verbatim, so no schema change is needed.
`minimal` rather than `none` keeps some of the accuracy that motivated moving to
this family, particularly for the five-tool structural examples. It is confirmed
by ear in SC-003; if it sounds slow, `none` is the recorded fallback.

**No `temperature`.** OpenAI's reference states that "parameter support varies
by model, with specific constraints applying to newer reasoning models", and
does not say whether this model accepts `temperature`. Three documentation
fetches did not resolve it. `docs/SCHEMA.md:307` has the field as optional, so
removing it costs nothing and deletes a line from twelve files; keeping it would
put an unverified parameter on a live call, which is the defect class this
feature exists to remove. CLAUDE.md and Principle IV both say an unverified
provider claim stays gated. Re-adding it later, if verified, is a deliberate
separate change.

**The sweep is both incumbents, not one.** Measured on the branch: 80
occurrences across 42 tracked files, of which 24 are author-facing and get
changed. The other 18 stay put: test fixtures and goldens under
`internal/testdata/` and `internal/generate/testdata/`, a comment in
`internal/target/catalog_livekit.go`, `specs/011-coding-agent-skill/tasks.md`
which records the drift as history, and this feature's own spec files, which
quote both identifiers for the same reason. Sweeping both incumbents at once
removes the risk a partial migration leaves a third identifier in the tree. The
split that used to decide this was by **surface type**:

| Says `gpt-4.1-mini` | Says `gpt-4o-mini` |
|---|---|
| the scaffold and its golden | all eleven examples |
| root `README.md` | `docs-site/build/your-first-agent.mdx` |
| `docs-site/index.mdx` | `docs-site/models/llm.mdx` |
| `docs/SCHEMA.md`, four places | `docs-site/reference/agent-yaml.mdx` |
| the skill bundle, three places | `docs/ORCHESTRATOR_SHARED_CONFIGURATION.md` |

Every front door says one thing and every example says the other. The doc site
contradicts itself between its homepage and its "your first agent" page, and
`references/models.md` contradicts itself inside one file, teaching one
identifier in two blocks and printing the other in its sample output.

Raw counts: `gpt-4o-mini` 38 occurrences in 28 files, `gpt-4.1-mini` 15 in 11.
Both go. The counts are recorded so the sweep can be verified as complete rather
than assumed complete, and so a later bump is one constant and one `sed`.

**Not extending the existing agreement test.**
`TestModelsReferenceMatchesCatalog` (`internal/skill/agreement_test.go:103-159`)
parses markdown rows for **vendor** names and asserts them against
`target.DefaultCatalog().Vendors(fw, role)`. It never looks at a model
identifier, and the identifiers in that file sit in fenced blocks its row regex
cannot match. Extending it would need a new regex over fenced blocks, a new
import edge (`internal/skill` does not import `internal/scaffold`), and would
leave the test's name describing something it no longer only does. A separate
test reading one exported constant is smaller and says what it means.

---

## D11. No `UNMUTE_*` renames, and the guard test locks a state that already holds

**Decision.** Fix the presentation, skip the renames, and say so.

The audit moved this part's target. The beginner path is **already clean**: zero
hits across the site index, all of `docs-site/start/` and `docs-site/build/`, the
root `README.md`, the scaffold, and every byte of the skill bundle. A live
`unmute init` plus `unmute compile` surfaces exactly one `UNMUTE_` name, inside
a Compose port mapping. `docs-site/reference/secrets.mdx` is already right: its
user-set table names none of them, and the two that appear sit in the following
"supplied by the route, the platform, or `unmute dev`" table.

**The generated files are what contradict the page.** A compiled outbound
example prints `UNMUTE_OUTBOUND_TOKEN=` and `UNMUTE_PUBLIC_URL=` as bare blanks
under "required by the target or a connection", and the emitted README lists
both under "Set these before running" alphabetically beside `TWILIO_AUTH_TOKEN`.
`docs/TELEPHONY.md:338` tells the reader to "generate this secret yourself with
a cryptographically secure password generator" for a token `unmute dev` mints.
That is the fix: move them into the labelled supplied-for-you block the LiveKit
platform-env section already uses, and correct the sentence.

**No name is dead.** `UNMUTE_OUTBOUND_TOKEN` still has live readers on the
Pipecat carrier-websocket and LiveKit connector routes; `specs/006` removed the
outbound endpoint only from the Daily-carrier helper.
`UNMUTE_HOLD_AUDIO_URL` and `UNMUTE_DAILY_ROOM_GEO` are live and already
presented correctly as commented-out optional lines. The only fictional name is
`UNMUTE_ICE_SERVERS`, one line of speculative prose, which is deleted.

**Renames are skipped.** FR-021 is a SHOULD. Renaming does not fix the one name
that actually reads as a credential — `UNMUTE_OUTBOUND_TOKEN` genuinely is a
bearer token, so any name reads that way, and presentation is the defect. The
eleven cheap renames move goldens and generated Python for a cosmetic gain, and
`UNMUTE_CALL_START` alone touches 23 golden lines across five goldens. The
priced table is kept in the spec so a later decision is cheap.

**Two live names are undocumented anywhere**:
`UNMUTE_TELEPHONY_BRIDGE_PORT` and `UNMUTE_AGENT_HEALTH_PORT`, one template line
each, named by no Go code, no document, no test, no golden. They get a line each
in the emitted README beside the other port knobs.

---

## D12. Tracing moves out of the first-run path

**Decision.** Remove `tracing:` from every example except one, and name that one
in the table as the tracing example.

Today `examples/README.md` says three `LANGFUSE_*` secrets are required "because
the public examples configure `tracing.provider: langfuse`", so the first
example in the table cannot be run without signing up for a third service. Which
example keeps the demonstration is a Phase 7 decision made against what the
examples actually contain; the requirement is that it is exactly one and that
the table says so.

---

## D13. `salon-support` is the starting example

**Decision.** Every entry point names `salon-support`.

The table already marks it "Start here", and it is the only structural example
that needs no carrier and no third-party account: web audio, local tools, no
Twilio. `docs-site/build/your-first-agent.mdx` and the bundle's `examples.md`
move to match it, rather than the other way round.

This decision has a dependency the brief did not know about:
`salon-support` is one of the five examples whose Pipecat container cannot start
(D4). The starting example must actually start, so D4 lands before this is
claimed.

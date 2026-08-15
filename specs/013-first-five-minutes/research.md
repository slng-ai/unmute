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

- *`ir.Validate`, as FR-004 first said; the spec has since been amended to record this split.* Loses the file and line for no gain, and
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

FR-011 originally forbade this, requiring the root file to "derive from the same
source" and calling two hand-synchronised files unacceptable. That reading was
too narrow: the objection is to **drift**, not to two templates, and a test that
fails on drift removes the drift without the refactor. FR-011 now names all
three acceptable resolutions and points here for which one was taken.

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

## D11. Vendor names take the vendor's prefix, and non-author names are hidden entirely

**Decision.** Two rules, both set by the author on 2026-08-15.

1. **Naming.** A variable that configures a service is named after that service:
   `TWILIO_*`, `OPENAI_*`, `SLNG_*`, `DAILY_*`, `LIVEKIT_*`. Five names take a
   vendor prefix they should always have had. Variables belonging to the
   generated agent itself, owned by no vendor, keep `UNMUTE_*`.
2. **Visibility.** `build/<target>/.env.example` and the emitted README's
   set-these list contain **only names the author supplies**. Everything else is
   absent, not relabelled.

**This adds no new concept.** An adversarial pass over the real compiled files
found that `internal/target/telephony.go` already carries
`LocallySuppliedEnvironment` per route, cloned into the IR as
`LocalEnvironment`, and it is already right about four names. The work is
fixing three things that ignore it, not inventing a classification:

- The LiveKit env template reads it and renders a labelled block; the Pipecat
  template ignores it entirely. So the same locally-supplied `REDIS_URL` is
  explained on one target and silently demanded on the other, from one piece of
  data. Principle III.
- `UNMUTE_PUBLIC_URL` and `UNMUTE_OUTBOUND_TOKEN` are in `RuntimeEnvironment`
  but missing from `LocallySuppliedEnvironment`, though `unmute dev` mints both.
  Three lines of data, and it is why they render as blanks.
- LiveKit's file prints `REDIS_URL=` under a comment that ends "which this agent
  never reads", and the comment is correct: the only Python reading it is
  `templates/pipecat_v1/telephony_shared.py.tmpl:35`, and
  `templates/livekit_v1/compose.telephony.yaml.tmpl:14` excludes it from the
  agent service. This is Part 2.2's "asks for a key the build does not use",
  alive on the telephony path.

**The author-set set is already computed too.** Verified against the compiled
LiveKit example, the eight genuine names come from five sources: model provider
keys, connection `environment:` values, `destinations:`, tool `url_env` and
`token_env`, and handler `os.environ`. That is `referencedEnvNames`
(`internal/ir/validate.go:1301`) plus the provider keys D3 adds. So one
definition serves three consumers: the secrets warning, the env file, and the
README list. A second list of "what the author fills in" would be the duplicate
this repository has been burned by before.

**Where the hidden names go.** Nowhere in an env file, and no second generated
file. `route.ManualSteps` already carries better text than a blank line, because
it names the source rather than the variable: "get `LIVEKIT_URL` and the API key
pair from the LiveKit Cloud project settings, or from a self-hosted LiveKit
Server configuration". `required_env` in `compile-report.json` stays complete as
the machine-readable form. A `deploy.env.example` was considered and rejected:
a second generated file to keep true, for information the README already carries
in prose that is strictly more useful.

**Superseded.** This decision originally read "No `UNMUTE_*` renames, and the
guard test locks a state that already holds", on the argument that renames are
cosmetic once presentation is fixed. That was wrong on the vendor-owned names.
`UNMUTE_DAILY_ROOM_GEO` configures a Daily room and claims two owners in one
name; `UNMUTE_HOLD_AUDIO_URL` is its sibling; the three `UNMUTE_LIVEKIT_*` host
mappings exist only because a LiveKit container is running. Those are not
cosmetic, and the audit had already priced all five as cheap.

**The stronger argument, which the first pass missed.** Principle I says a
generated project "MUST carry no Unmute dependency" and "MUST be readable,
runnable, and deployable with Unmute absent". A variable named `UNMUTE_` inside
a project Unmute is not part of is the dependency-shaped thing that principle
forbids, whether or not any reader mistakes it for a secret. That is what makes
the vendor renames a MUST rather than a SHOULD.

**Collision check, done rather than assumed.** The repository has no existing
bare `LIVEKIT_PORT`, `LIVEKIT_SIP_PORT`, `LIVEKIT_RTP_PORT_RANGE`, or
`DAILY_ROOM_GEO`; a grep appears to find them only because they are substrings
of the `UNMUTE_*` names being replaced. The three LiveKit renames keep `HOST` in
them (`LIVEKIT_HOST_PORT`, `LIVEKIT_SIP_HOST_PORT`,
`LIVEKIT_RTP_HOST_PORT_RANGE`) because they are Docker Compose host-side
mappings, not configuration the LiveKit server reads inside its container, and
the name should not suggest otherwise.

**Why the vendor-less group keeps `UNMUTE_`.** Once FR-018 removes them from
every author-facing file, the prefix stops being something a reader meets. The
renames those would need are the expensive half of the audit's table, roughly 44
to 53 sites each for `UNMUTE_PUBLIC_URL` and `UNMUTE_OUTBOUND_TOKEN`, and 23
golden lines across five goldens for `UNMUTE_CALL_START`. Paying that to change
a string nobody sees is the kind of work this project's ceiling comment exists
to refuse.

**The cost of hiding, named rather than hidden.** The strictest visibility
option removes the one document that told a self-hosted operator what to supply.
That information is not lost: `compile-report.json` already carries a complete
`required_env`, and the Compose file keeps its interpolation defaults. So the
deploy story moves from prose to the compile report, which is a developer
artifact rather than an author one, and that is the right place for it. The
author's own rule carries the exemption that keeps this honest: a genuinely
useful developer note, such as what to do when host port 5060 is taken, stays,
in a troubleshooting section rather than a to-do list.

**One name is not an environment variable at all.** `UNMUTE_SIP_TRUNK_ID` is a
`sed` substitution token inside one generated JSON file, and `specs/005` says so
in two places. It must stop being shaped like an environment variable so that
nobody tries to set it.

### The audit facts these rules were applied to

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

**Two live names are undocumented anywhere**:
`UNMUTE_TELEPHONY_BRIDGE_PORT` and `UNMUTE_AGENT_HEALTH_PORT`, one template line
each, named by no Go code, no document, no test, no golden. They get a line each
in the emitted README beside the other port knobs.

---

## D14. A hidden name still fails loud, and the failure says where the value comes from

**Decision.** The generated `REQUIRED_ENV` startup check keeps every name it
needs, including the ones FR-018 hides from `.env.example`. What changes is the
**message**: a missing locally-supplied name says where the value comes from,
not only that it is absent.

**The contradiction this resolves.** FR-018 removes `UNMUTE_PUBLIC_URL`,
`UNMUTE_OUTBOUND_TOKEN`, `REDIS_URL`, and the three `LIVEKIT_*` values from
`build/<target>/.env.example`, while FR-005b derives `REQUIRED_ENV` from the
names the compiler knows it requires, which includes all of them. So an operator
who copies `.env.example` to `.env` and starts the container without
`unmute dev` gets a refusal naming a variable that file never mentioned.

Pipecat makes it sharpest, because there the agent genuinely reads the value:
`templates/pipecat_v1/telephony_state.py.tmpl:36` raises
`RuntimeError("Missing required environment variable: REDIS_URL")`. The Compose
graph supplies it at `compose.telephony.yaml.tmpl:8`; a hand deploy does not.

**Why the other two answers are worse.**

- *Drop the locally-supplied names from `REQUIRED_ENV`.* The agent then starts
  and fails later, at the moment a call arrives, on a live phone line. That is
  the exact trade Principle II names as the worst one: "a loud failure at
  validate for a quiet one in production".
- *Put them back in `.env.example`.* Rejected twice by the author, and rightly:
  a to-do list whose entries are not the reader's to do is not a to-do list.

**So the failure stays loud and stops being unhelpful.** The generated check
already knows which names are in `LocalEnvironment`, because that is the same
set FR-018 uses to exclude them. A name in that set gets a message naming its
source:

```
Missing required environment variable: REDIS_URL
This one is supplied for you: `unmute dev` sets it locally, and your platform
or operator sets it at deploy time. See "Carrier setup" in README.md.
```

Names the author does supply keep the message they have today, because for those
the current wording is already right.

This is a message change, not a mechanism change. One template, one branch on a
set the generator already carries.

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

# Wave A: reproduction before fixing

Every defect below was reproduced by an isolated agent against commit
`16289f4` (main with PR #78, #79, and #80 merged), each in its own scratch
directory, before any fix was written. No agent edited a repository file.

Status legend: **REPRODUCED** / **NOT REPRODUCED** / **PARTIAL** / *pending*.

| # | Defect | Status |
|---|---|---|
| A | An unattached control vanishes completely | **REPRODUCED** (wider than claimed) |
| B | A browser-only package emits a transfer that cannot work | **REPRODUCED** |
| C | The secrets completeness rule is silent when no block is declared | **REPRODUCED** |
| D | A Pipecat container with local tools cannot import them | **REPRODUCED** (a regression, not a permanent gap) |
| E | A turn detector model is checked at compile, not validate | **REPRODUCED** (one of eight, not one) |
| F | `unmute init` contradicts itself | **REPRODUCED** (5 of 5 claims, plus 8 more) |
| G | `UNMUTE_*` names appear as credentials | **PARTIAL** — the beginner path is already clean; the generated files are not |

Two premises in the brief did not survive reproduction, and the spec was
corrected rather than the evidence:

- The Pipecat container defect is **not** "never worked". It is a regression
  introduced on 2026-08-13 by `6a5e77f`, and it ships in all three published
  tags.
- The `UNMUTE_*` beginner path is **already clean**, and
  `docs-site/reference/secrets.mdx` already gets it right. The contradiction is
  downstream, in the generated `.env.example` and the emitted README.

A third premise was wrong in a way that changed a decision: the model
identifier is not "scaffold versus docs". Every front door says `gpt-4.1-mini`
and every example says `gpt-4o-mini`, and the doc site disagrees with itself.

---

## A. An unattached control vanishes completely

**REPRODUCED.** All five claimed rows hold, and the defect is wider than
reported.

Package: a **telephony** LiveKit package with a valid Twilio SIP connection, a
`human_transfer` declared under `controls:` with an env-var destination, and the
agent's `tools:` left empty.

| Step | Observed |
|---|---|
| `unmute validate .` | exit 0. `✓ livekit (livekit)` plus the unrelated turn-placement warning |
| `unmute compile .` | exit 0. The control name never appears |
| `grep -rn reach_a_person build/` | zero hits. `agent.py` contains **no `@function_tool` at all** |
| `compile-report.json` | zero hits — absent even in the *attached* build |
| `build/livekit/.env.example` | `FRONT_DESK_PHONE_NUMBER=` at line 4, and it also reaches `agent.py:66` (`REQUIRED_ENV`, so the agent refuses to start without it), `compile-report.json`, `compose.telephony.yaml`, and `compose.dev.yaml` |

Adding the control to `tools:` and recompiling makes it appear at
`agent.py:153`. The entire delta between the two builds is the ~90-line tool
plus three README sections, including the carrier setup step that tells the
operator to **enable SIP REFER / PSTN transfer**. `compile-report.json` and
`.env.example` are byte-identical between the two, so the report never
distinguishes a package that can transfer from one that cannot.

### Mechanism

`internal/generate/livekit_v1_build.go:700`, `buildLiveKitAgent`. The loop at
`:727` is `for _, ref := range def.Tools`, the control lookup is at `:744`
(`control, ok := agent.Controls[ref]`), and the `*ir.HumanTransfer` case is at
`:783`. **This is the only read of `agent.Controls` anywhere on the LiveKit
generate side.** There is no second pass, so the control is never visited at
all — it is not filtered out, it is simply unreachable.

Pipecat has the same shape: `internal/generate/pipecat_v1_build.go:767`, loop at
`:785`, lookup at `:797`.

Nothing checks the reverse direction. `internal/ir/build.go:1325`,
`checkToolRefs`, only proves every name in an agent's `tools:` resolves.
Precedent for the opposite check already exists —
`internal/ir/validate.go:1365`, `unusedConnectionWarning`, warns about a
`connections/*.yaml` no target names — but there is no equivalent for controls,
destinations, or tools.

### A contradiction inside the compiler

`internal/ir/validate.go:655`, `routeCapabilitiesUsed`, walks `agent.Controls`
**directly** at `:662` with no tool-list filter, so an unattached control still
produces a `cold_transfer` evidence row. Confirmed by deleting the control:
evidence drops to inbound / outbound / route only. So `compile-report.json`
asserts the package does cold transfer while the emitted agent cannot.

The same holds for warm (`internal/ir/validate.go:1461`, `hasWarmTransfer`): an
unattached `warm:` block compiles at exit 0, emits no tool, and reports
`warm_transfer=provisional`. And `internal/ir/build.go:629` validates the
control's `destination` against every target even when unattached. The machinery
inspects unattached controls for **validation** and ignores them for
**codegen**.

Knock-on: `internal/generate/livekit_v1_build.go:318-323` derives
`HasColdTransfer` / `HasWarmTransfer` from the already-emitted transfers, so the
README gate at `templates/livekit_v1/README.md.tmpl:414` goes silent too and the
runbook loses the carrier transfer-toggle step.

### Which other shapes share it

Every row below exits 0 from both commands with no warning.

| Shape | Result |
|---|---|
| unattached `human_transfer` cold, LiveKit | silent drop |
| unattached `human_transfer` warm, LiveKit | silent drop, **plus a false `warm_transfer` evidence row** |
| unattached `human_transfer` cold, Pipecat | silent drop — Pipecat is not honest here either |
| unattached `agent_transfer` | silent drop, **and the unreachable target agent is still emitted** as `class Specialist(Agent)` dead code |
| unattached `delegate` | silent drop, and its task class disappears too |
| unreferenced `destinations:` entry | silent, **and it costs you**: the env name reaches `.env.example`, `REQUIRED_ENV`, the compile report, and both compose files. A phantom required secret whose absence blocks agent startup |
| unreferenced top-level `tools:` entry | silent drop of the tool, but its `webhook.url_env` still lands in `.env.example` and the compile report. Same phantom-secret shape |
| unreferenced `models:` entry | **legal by design.** `docs/SCHEMA.md:287` calls the models map "a palette: entries that nothing currently references are legal". That wording is scoped to `models:` only |
| unreferenced `connections/*.yaml` | **already caught** — the only honest one in the group |

The `models:` palette rule is the one carve-out this feature must not break.

---

## B. A browser-only package emits a transfer that cannot work

**REPRODUCED.** All four claimed rows held exactly.

Package: one agent, one channel `web: realtime_audio`, a LiveKit target, no
connection, a `human_transfer` declared under `controls:` **and** attached to
the agent's `tools:`, with an env-var destination.

| Step | Observed |
|---|---|
| `unmute validate .` | exit 0. Only warning: `livekit: LiveKit turn placement is a preference` |
| `unmute compile .` | exit 0, all eight files emitted |
| `grep -c send_to_human build/livekit/agent.py` | **2** — `agent.py:85` (the `@function_tool` at `:84`) and `agent.py:97` |
| `build/livekit/.env.example` | lists `FRONT_DESK_PHONE_NUMBER=` |
| `build/livekit/compile-report.json` | `required_env` includes it; `secrets` records `referenced_by: ["agent.yaml destinations front_desk_line"]` |

The generated `caller is None` branch at `build/livekit/agent.py:110-128` names
the failing case in its own comment: *"The usual cause is a session that never
arrived by phone: an Agent Console run, a browser session, or a dispatch rule
pointing at a different agent."* The compiler emitted a transfer for a package
whose only channel **is** a browser session, then explained in a generated
comment why it cannot work. The emitted `build/livekit/README.md` goes further
and ships a runbook section documenting the dead end.

### Mechanism

`internal/ir/validate.go:1148`, `validateHumanTransfer`. The gate at
`validate.go:1155-1157`:

```go
if resolved.Telephony != nil {
    return
}
```

With a telephony connection the route table has already resolved the control, so
this returns early. With **no** connection it falls through to
`applyResolvedCapability(caps.Control(required, provider, resolved.Transport, resolved.Carrier), ...)`
at `validate.go:1163` with both transport and carrier empty.

The static control table, `internal/target/table.go:414-427`:

- **LiveKit** `Controls[ColdTransfer]` is a bare `control()`, an unconditional
  `Capability{Tag: Core}` with no transport or carrier condition. This is why it
  compiles.
- **Pipecat** (`table.go:417`) is
  `controlTransport("daily-sip", "Pipecat cold transfer requires Daily SIP transport")`.
  `Table.Control` (`table.go:170-188`) sees a required transport and an empty
  one, returns `Gated`, and `applyCapabilityValue` (`validate.go:1738-1739`)
  turns that into a row error. This is why Pipecat refuses.

The route-naming half of N31 lives in `internal/target/telephony.go:496`,
`ResolveTelephonyFeature`, with the messages
`unsupported telephony route (%s, %s, %s)` at `:501` and
`telephony route (%s, %s, %s) does not support %s` at `:506`. **None of it is
reachable here**, because a package with no connection has no `TelephonyKey` to
resolve, so `ResolveTelephonyFeature` is never called. The refusal N31 promises
exists; it just never runs on the browser-only route.

`internal/target/table_test.go:237`, `TestV1_TransfersCompileOnlyOnNativeRoutes`,
covers `TelephonyKey`-bearing routes and the Pipecat control rows. It does not
cover a LiveKit target with no route at all.

`docs/SCHEMA.md:102` (N31) and `docs/SCHEMA.md:441` both say cold transfer is
"LiveKit native on `(livekit, sip)`". `(livekit, no route)` is not that, so the
emitted output contradicts the document. Under Principle IV the document wins.

### The wording the fix should match

Pipecat's refusal on the same package, `unmute validate .` exit 1:

```
✗ pipecat (pipecat)

Errors:
  pipecat: Pipecat cold transfer requires Daily SIP transport
```

Closer still, the **same LiveKit browser-only package with `cold:` changed to
`warm:`** already refuses today:

```
Errors:
  livekit: warm transfer needs a telephony Connection: it dials the destination
  itself, using the connection's sip_address, sip_username, sip_password and
  from_number
```

So on one target and one control kind, the warm shape refuses and the cold shape
compiles a dead tool. The cold refusal should read as that message's sibling.

### B is not A

The same package with the control **removed** from the agent's `tools:` but
still declared under `controls:`: validate exit 0, compile exit 0,
`grep -c send_to_human agent.py` = **0**, and no diagnostic of any kind. The
`.env.example` still carries `FRONT_DESK_PHONE_NUMBER=` for a control no
generated code reads.

Cross-check: the same unattached package on the pipecat target still refuses
with `Pipecat cold transfer requires Daily SIP transport`, so the capability
check runs off the **declaration**, not off attachment. The two defects are in
different code paths. Fixing B (make the LiveKit cold row route-aware) does not
fix A. Fixing A (diagnose an unattached control) does not fix B, because with
the control attached B still compiles a dead transfer.

---

## C. The secrets completeness rule is silent when no block is declared

**REPRODUCED**, and the consequence is larger than the report.

### The guard

`internal/ir/validate.go:1388-1391`:

```go
func undeclaredSecretWarning(agent *Agent) string {
	if len(agent.Secrets) == 0 {
		return ""
	}
```

Called once, at `validate.go:71`. The guard tests the **declared** list, so the
maximally incomplete package — declares nothing, references seven names — takes
the same early return as a package with nothing to report.

The existing warning string, `validate.go:1402`, which the fix should reuse
verbatim:

```
environment variables referenced but not declared in secrets: <name> (<site>), ...
```

Sites come from `referencedEnvNames`, `validate.go:1301-1352`.

### The three shapes

All three copied `examples/livekit-human-transfer`; only `agent.yaml` differs.
Every command exited **0**.

| Package | `secrets:` | stderr |
|---|---|---|
| a | all seven declared | only the unrelated turn-placement warning |
| b | present, three missing | names all three with file and key |
| c | block deleted, otherwise identical to b | **nothing about secrets** |

Package b's warning, verbatim, showing the file-and-key detail the fix must
keep:

```
livekit: environment variables referenced but not declared in secrets:
BILLING_PHONE_NUMBER (agent.yaml destinations billing_line),
SIP_AUTH_USERNAME (connections/twilio_sip.yaml environment sip_username),
SIP_TRUNK_HOSTNAME (connections/twilio_sip.yaml environment sip_address)
```

Package c's own stdout in the same run lists every name it needs
(`livekit: telephony required env BILLING_PHONE_NUMBER`, and so on). The
compiler knows, and says nothing.

### Second consequence: the startup check disappears too

`docs-site/reference/secrets.mdx:107` promises "the generated agent refuses to
start without them."

- Package a's `build/livekit/agent.py`: four hits for `REQUIRED_ENV` /
  `require_env`.
- Package c's `build/livekit/agent.py`: **zero**. No `REQUIRED_ENV`, no
  `require_env()`.
- Package b's `REQUIRED_ENV` holds only `OPENAI_API_KEY` and `SLNG_API_KEY`.
  `BILLING_PHONE_NUMBER` drops out of the startup check *because* it was left
  undeclared — the warning is the only thing between that and a failed transfer.

So a package with no block loses both documented benefits, with no diagnostic.

### Third finding: removing the guard alone is not enough

A copy of the fresh scaffold with `secrets:` set to one unrelated placeholder is
**still** silent about `OPENAI_API_KEY` and `SLNG_API_KEY`.
`referencedEnvNames` (`validate.go:1301`) collects only `*_env` fields, langfuse
tracing keys, handler `os.environ` reads, connection `environment:` values, and
`destinations:`. A model provider's API key is not among them, so it is never in
the reference set. A fresh `unmute init` package has an **empty** reference set,
which means the check would be vacuous there even after the guard is removed.
This is why FR-005a exists.

### The scaffold, confirmed

`unmute init myagent` exits 0 and writes `agent.yaml`, `.env.example`,
`instructions.md`, `targets.yaml`, `tools/end_call.yaml`. Grepping the whole
scaffold for `secrets` returns no match. Its `.env.example` names
`DAILY_API_KEY=`, `OPENAI_API_KEY=`, `SLNG_API_KEY=`. `unmute validate .` on the
untouched scaffold: exit 0, `✓ pipecat (pipecat)`, **stderr completely empty**.

### Doc tension to resolve

`docs/SCHEMA.md:244` lists `secrets` as not required, and N24 calls declaring
secrets "opt-in". Neither document says the **warning** is opt-in — "opt-in" is
given as the reason the severity is a warning rather than an error. The code
reads "opt-in" as "no block, no check". Under Principle IV the document wins.

### Other checks with this shape

The validator has exactly two global warning functions.

- `undeclaredSecretWarning` (`validate.go:1388`) guards on the **declaration**
  list. This is the defect: an empty declaration list is the case most worth
  reporting.
- `unusedConnectionWarning` (`validate.go:1365`) guards on the **subject** set
  (`len(agent.Connections) == 0`). Correct and harmless.

No other completeness check in the validator guards on the declaring side.

### The test that knew (C)

`internal/ir/variables_test.go:277`,
`TestTelephonyExamplesDeclareEveryNameTheyWrite`, contains:

```go
if len(agent.Secrets) == 0 { t.Fatal("this example declares no secrets, so the check below is vacuous") }
```

The authors knew the check goes vacuous on an empty list and guarded the test
against it, without guarding the product.

---

## D. A Pipecat container with local tools cannot import them

**REPRODUCED**, with one correction: this is a **regression**, not a permanent
gap. The template was a single unbranched definition using `COPY . .` until
commit `6a5e77f` on 2026-08-13 ("fix(pipecat): build the image the platform can
actually start") split it into two branches. All three published tags
(`v0.1.0`, `v0.1.1`, `v0.1.2`) carry the break.

### The template

`internal/generate/templates/pipecat_v1/Dockerfile.tmpl`.

Telephony branch (`FROM python:3.12-slim`), fine because it copies everything:

```
 7: COPY pyproject.toml ./
24: COPY . .
```

Non-telephony branch (`FROM dailyco/pipecat-base:0.1.27-py3.12`), the defect:

```
39: COPY pyproject.toml ./
58: COPY bot.py ./
59: {{if .Tracing}}COPY tracing.py ./
60: {{end}}
```

The comment that explains the discipline, lines 56-57:

```
# Named files, never `COPY . .`: /app is the base image's own directory, and
# copying over it replaces the server that answers /bot.
```

LiveKit is unaffected: `templates/livekit_v1/Dockerfile.tmpl:14` is
`COPY --chown=appuser:appuser . .`.

`internal/generate/pipecat_v1.go:608` writes `tools/__init__.py` and `:610`
writes `tools/<name>.py`; `templates/pipecat_v1/bot.py.tmpl:47` emits
`import tools.<name>`. The string `COPY tools` appears nowhere in the
repository. `.dockerignore` does not exclude the directory — the Dockerfile
simply never asks for it.

### Real container output

An image built earlier the same day by a real `unmute dev` run of
`salon-support`, whose `/app/bot.py` is byte-identical to what the compiler
emits now, and whose `docker history` shows exactly the three COPY lines above:

```
=== ls /app ===
.venv  app.py  bot.py  feature_manager.py  pcc_observers.py
pcc_structured_logs.py  pipecatcloud_system.py  pyproject.toml
tracing.py  waiting_server.py          <-- no tools/

=== python bot.py --host 0.0.0.0 --port 7860 ===   (the compose.dev.yaml command)
Traceback (most recent call last):
  File "/app/bot.py", line 22, in <module>
    import tools.book_appointment
ModuleNotFoundError: No module named 'tools'
```

`compose.dev.yaml` has no bind mount, so `unmute dev --target pipecat` builds
and runs this same broken image. The browser demo path is broken, not only cloud
deploys.

### Blast radius: 5 of the 10 examples that declare a Pipecat target

| Example | Branch | Local tools | Status |
|---|---|---|---|
| `multi-task` | non-telephony | 5 | **broken** |
| `salon-support` | non-telephony | 2 | **broken** — and the table marks it "Start here" |
| `simple-prompt` | non-telephony | 5 | **broken** |
| `subagents` | non-telephony | 5 | **broken** |
| `task-groups` | non-telephony | 5 | **broken** |
| `outbound-reminder` | telephony (`COPY . .`) | 1 | ok |
| `mcp-example`, `pipecat-human-transfer-daily`, `pipecat-human-transfer-twilio`, `twilio-telephony-hello` | non-telephony | 0 | ok |

### Golden impact, and why this shipped

Exactly one golden contains Dockerfile content,
`internal/generate/testdata/golden/pipecat_v1.txt`. Its fixture has **no local
tools**, so a conditional `{{if .LocalTools}}` copy changes **zero** goldens and
an unconditional one changes **one**.

That empty fixture is also the cause.
`internal/generate/pipecat_deploy_test.go:66-102`,
`TestPipecatImageMeetsThePlatformContract`, asserts `COPY bot.py ./` is present
and `COPY . .` is absent — against `pipecatArtifact(t, nil)`. Nothing ever
checked that every emitted `.py` is reachable inside the image.

---

## E. A turn detector model is checked at compile, not validate

**REPRODUCED**, character for character, and it is one member of a class of
eight.

```
$ unmute validate .
✓ livekit (livekit)

Warnings:
  livekit: LiveKit turn placement is a preference
exit 0

$ unmute compile .
unmute: compile .: generate livekit livekit: livekit turn model "silero" is not recognized; use turn-detector-mini (local) or turn-detector (LiveKit Cloud)
exit 1
```

The check lives at `internal/generate/livekit_v1_build.go:1056`, in
`livekitTurnVersion`. `internal/ir/validate.go` has no value check on the turn
model at all.

The same authored value compiles clean on Pipecat (`turn: {model: banana}` →
exit 0), so one `agent.yaml` is legal or illegal depending on a target the
author may add later.

### The constitution does not authorise a depth gap

Principle V's three relevant sentences are all about the **provider set**:
validate "MUST work for every declared provider whether or not that provider's
driver ships"; "This is the only command whose reach is all four providers";
"Generation reaches only the providers with a shipped driver". The Technology
section repeats it: "Validation is deliberately wider than generation. All four
providers validate, because portability has to be checkable before an author
commits to a platform." The 1.1.0 sync note that introduced the sentence
describes it as provider reach.

Nothing says validate may skip a value-level check for a provider it fully
serves.

### The repository already decided this, for the sibling field

`internal/ir/validate.go:954-957`:

```go
// A per-model language must have a slot on the resolved target's integration
// (N16). The generator errors on a slotless entry; mirror it here so a
// slotless language fails validate, not just generate (C6: gate before any
// artifact).
```

`validate.go:1022` and `:1025` mirror two more `service_call.go` checks the same
way. The turn model, the pins, the version range, `sdk_language`, and "voice has
no slot" were never mirrored. That is an omission, not a boundary.

### The class: eight generator-only value checks

Confirmed by execution — validate exits 0, compile exits 1 — after enumerating
all 90 `fmt.Errorf` sites in the eight non-test files of `internal/generate/`
and probing 26 mutated packages.

| # | Site | Field | Message |
|---|---|---|---|
| 1 | `livekit_v1_build.go:1056` | `models.turn.<n>.model` | `livekit turn model "silero" is not recognized; use turn-detector-mini (local) or turn-detector (LiveKit Cloud)` |
| 2 | `livekit_v1_build.go:24` | `targets.<n>.sdk_language` | `livekit driver emits python projects only; sdk_language "node" has no templates yet` |
| 3 | `livekit_v1.go:538` | `pins` key | `livekit pin "..." is not a pinnable package; known: ...` |
| 4 | `livekit_v1.go:542` | `pins` value | `livekit pin ...: "banana" is not a semantic version` |
| 5 | `livekit_v1.go:548` | `pins` value | `livekit pin ... "0.0.1" is below the catalogue floor >=1.6.1` |
| 6 | `artifact.go:146` (both drivers) | `targets.<n>.version` | `livekit version "banana" is not a semantic version` |
| 7 | `artifact.go:151` (both drivers) | `targets.<n>.version` | `... is outside the driver's template-compatible range (>=1.5, <2.0)` |
| 8 | `service_call.go:75` | speak entry `voice` on a slotless vendor | `... livekit speak binding provider "deepgram": voice has no slot here` |

Number 8 is the sharpest: `docs/SCHEMA.md` §4.3 lists `voice` as **required** on
every speak entry, so authoring a `deepgram` speak model necessarily produces a
package that validates green and cannot compile.

A further thirteen generator error sites were probed and found already gated
earlier, so the class is eight and not ninety. The telephony-route sites could
not be reached by any constructed package and are recorded as probably-gated,
unconfirmed.

### Three error tiers, and what moving a check actually buys

The locator is `spec.Package.Location(file, token)` at
`internal/spec/package.go:22`, and `Generate` never receives a `*spec.Package`:

```go
func Generate(agent *ir.Agent, resolved ir.Target, caps target.Table) (Artifact, error)
```

| Tier | Where | What the author sees |
|---|---|---|
| 1 | `spec.Load` / `ir.Build` | `connections/twilio_voice.yaml:21: "banana_key" is not accepted by route (pipecat, cloud-websocket, twilio); it accepts account_sid, auth_token, from_number` |
| 2 | `ir.Validate` | `livekit: <message>` — target-prefixed, **no position** |
| 3 | `generate` | bare message, no prefix, no position, after validate already said the package was fine |

So moving a check from tier 3 to tier 2 buys "before any artifact is written",
not a file and line. The spec was corrected to say so.

### The schema contradicts the driver

`docs/SCHEMA.md:317` is the only specification of the field:

> Turn model fields: `provider` (no), `model` (**no, for example `silero`**), ...

The schema's own example value is the one that fails. §4.3 reinforces it:
identities are "forwarded to the provider as-is and **never validated** (D10)".
`turn-detector-mini` and `turn-detector` appear in no `docs/` or `docs-site/`
page — only in four examples' `targets.yaml` override blocks and in the Go error
string. Delete those four-line overrides and those examples break.

### Validate says nothing about what it did not check

Verbatim, on a clean package:

```
✓ livekit (livekit)
✓ pipecat (pipecat)

Warnings:
  livekit: LiveKit turn placement is a preference
```

`unmute validate --help` carries no caveat. A green tick per target is the
entire contract the author sees.

---

## F. `unmute init` contradicts itself

**REPRODUCED**, all five claims, with the line numbers confirmed exact except
one off-by-one. Eight further contradictions were found.

| # | Claim | Verdict |
|---|---|---|
| 1 | prompt and greeting describe a phone call | **REPRODUCED**. `scaffold.go:33-34`, applied unconditionally at `:288` and `:291` |
| 2 | root `.env.example` asks for `DAILY_API_KEY` | **REPRODUCED**. `scaffold.go:269` sets `Transport = "daily-sip"`; `:480-481` turns it into the name |
| 3 | model identifier disagrees with the docs | **REPRODUCED**, but the claim understates it — see below |
| 4 | the prompt is a single flat sentence | **REPRODUCED**. One line, 106 characters |
| 5 | no `secrets:` block | **REPRODUCED** |

The channel is not merely ignored, it is **unreadable** at that point:
`withDefaults` sets `Instructions` at `:291` before it sets `Channel` at `:294`.

`Transport = "daily-sip"` is a **phantom**. It is never rendered into any
scaffolded file — `targets.yaml` carries no `transport:` line, and no
`connections/` file is written because the package uses no phone route. Its only
observable effect in the entire package is the `DAILY_API_KEY` line.

The env diff, in both directions:

| Name | package root | `build/pipecat/` |
|---|---|---|
| `DAILY_API_KEY` | **present** | absent |
| `OPENAI_API_KEY` | present | present |
| `SLNG_API_KEY` | present | present |

One name in root and not in the build; nothing in the build and not in root.
`unmute validate .` on the untouched scaffold: exit 0, stderr empty.

The two files share no code. The root one renders
`internal/scaffold/templates/env.example.tmpl` from `scaffold.Data.RequiredEnv()`
(`scaffold.go:471-530`), pre-spec. The generated one renders
`internal/generate/templates/pipecat_v1/env.example.tmpl` from the resolved
`ir.Agent`. The root file reads `Data.Transport`, a field the IR never sees
because the scaffold never writes it to disk. That is structurally why the two
can disagree and why nothing catches it.

### The model census, which corrects the brief

Tracked files, excluding compiled output:

- **`gpt-4o-mini`: 38 occurrences in 28 files**
- **`gpt-4.1-mini`: 15 occurrences in 11 files**

But the split is by surface type, not by code versus docs.

| Says `gpt-4.1-mini` | Says `gpt-4o-mini` |
|---|---|
| `internal/scaffold/scaffold.go:277` and its golden | all eleven `examples/*/agent.yaml` |
| root `README.md:29` | `docs-site/build/your-first-agent.mdx:29,99,109` |
| `docs-site/index.mdx:61` | `docs-site/models/llm.mdx:13` |
| `docs/SCHEMA.md:68 (x2), 269, 306` | `docs-site/reference/agent-yaml.mdx:45` |
| `internal/skill/assets/references/models.md:21,89` | `docs-site/reference/cli/compile.mdx:55` |
| `internal/skill/assets/references/package.md:38` | `docs/ORCHESTRATOR_SHARED_CONFIGURATION.md:420` |
| root `.env.example:3` (`OPENAI_MODEL`, dead) | `internal/skill/assets/references/models.md:97` (sample output) |

Every front door says one thing; every example says the other. The doc site
contradicts itself between its homepage and its "your first agent" page, and
`references/models.md` contradicts itself inside one file. That is why the spec
chose `gpt-4.1-mini` rather than the majority string.

### The models agreement test does not hold a model identifier

`internal/skill/agreement_test.go:103-159`,
`TestModelsReferenceMatchesCatalog`, parses only the markdown rows matching
`^\| (pipecat|livekit) \| (listen|speak|think) \| (.*) \|$` out of
`references/models.md`, pulls backticked **vendor** names out of the third cell,
and asserts three things about `target.DefaultCatalog().Vendors(fw, role)`. It
never looks at a model identifier; the identifiers in that file sit in fenced
blocks the row regex cannot match.

Extending it would mean a new regex over fenced code blocks, a new import edge
(`internal/skill` does not import `internal/scaffold`), and a test whose name
stops describing what it does. A separate test reading a new
`scaffold.DefaultReasonModel` constant is the right shape.

Also false in its own file: `models.md:103` says "Only SLNG model ids appear in
this repository's own documentation, because those are the ones proven here",
three lines from two OpenAI identifiers.

### The scaffold golden does not cover the CLI

`TestWrite_golden` (`scaffold_test.go:44-69`) calls `Write` with **no tools**,
while `internal/cli/init.go:27` passes `scaffold.DefaultTools()`. Diffing a real
`unmute init` against the golden:

```
45a46,47
>     tools:
>       - end_call
46a49,51
> tools:
>   - end_call
>
75a81,86
> === tools/end_call.yaml ===
> description: "End the call when the caller is finished or says goodbye."
> builtin:
>   id: end_call
```

`tools/end_call.yaml` has no golden coverage at all. A change to the default
tool template is invisible to `go test ./...`.

### Eight further contradictions

1. **The `.env.example` tells you to run a command that cannot work from where
   the file is.** Line 2 says "Copy to `.env`, fill in, then run
   `unmute dev hello-agent`" from **inside** `hello-agent/`. Following it
   literally resolves to `hello-agent/hello-agent`:
   `unmute: validate hello-agent: load: agent.yaml: open .../hello-agent/hello-agent: no such file or directory`
2. `references/models.md` teaches `gpt-4.1-mini` at lines 21 and 89 and prints
   `gpt-4o-mini` in its sample output at line 97.
3. The doc site disagrees with itself between two entry pages.
4. The golden test does not test the CLI's output.
5. `targets.yaml:6` says "Secrets and URLs are env var names only" in a package
   that names zero secrets, in the file that would not hold them anyway.
6. `OPENAI_MODEL` in the repository-root `.env.example:3` is read by nothing —
   `git grep` returns only its own definition.
7. `docs-site/start/quickstart.mdx:80-89` prints "Hi, thanks for calling." as
   the expected greeting after the browser opens and you allow the microphone.
8. `quickstart.mdx:46-48` rationalises the `DAILY_API_KEY` defect instead of
   fixing it: "only needed if you later run this agent on a phone number; leave
   it empty for now." Not true — the generated `.env.example` omits the name,
   and adding a telephony channel later needs a `connections/` file the scaffold
   never wrote either.

Not a defect, recorded so nobody "fixes" it: `end_call` appearing in both the
top-level `tools:` list and the agent's `tools:` is the schema's declare-then-
attach shape.

Three of the five claims are **already documented as known defects inside the
shipped skill bundle**, at `internal/skill/assets/references/package.md:168-186`,
verified 2026-08-15. They were found, written down, and shipped as documentation
of themselves.

---

## G. `UNMUTE_*` names appear as credentials

**PARTIAL.** The brief aimed at the wrong surface.

27 distinct names exist: 14 on the product surface, 1 fictional, 12 in test
harnesses only.

### The beginner path is already clean

Zero `UNMUTE_` hits in `docs-site/index.mdx`, all of `docs-site/start/**`
(including `coding-agents.mdx`), all of `docs-site/build/**`, root `README.md`,
`internal/scaffold/**`, and every byte under `internal/skill/assets/**`. The
guard test would lock in a state that already holds.

A live `unmute init hello` plus `unmute compile .` produces exactly **one**
`UNMUTE_` occurrence in the whole output:

```
build/pipecat/compose.dev.yaml:14:      - "${UNMUTE_DEV_PORT:-7860}:7860"
```

Inside a Compose port mapping, where it cannot read as a credential.

### `secrets.mdx` is already right

Its **user-set** table (lines 54-58) contains no `UNMUTE_*` name. The two that
appear sit in the *following* table (lines 60-71), introduced by "These names
are supplied by the route, the platform, or `unmute dev`, so no package declares
them", with `unmute dev` named as the supplier.

### The generated files contradict the page

From a compiled `examples/outbound-reminder`:

```
# required by the target or a connection
REDIS_URL=
UNMUTE_OUTBOUND_TOKEN=
UNMUTE_PUBLIC_URL=
```

and the emitted README lists both under "Set these before running (values are
never written into the project):", alphabetically among `OPENAI_API_KEY`,
`SLNG_API_KEY`, and `TWILIO_AUTH_TOKEN`. `docs/TELEPHONY.md:338` goes further:
"**Generate this secret yourself** with a cryptographically secure password
generator" — for a token `unmute dev` mints at `dev_telephony.go:87-92`.

### Nothing on the product surface is dead

`UNMUTE_OUTBOUND_TOKEN` is live: `specs/006` removed the outbound endpoint only
from the Daily-carrier helper, and the Pipecat carrier-websocket and LiveKit
connector routes still serve `POST /telephony/outbound` and read the token.
`UNMUTE_HOLD_AUDIO_URL` and `UNMUTE_DAILY_ROOM_GEO` are live and already
presented correctly, as commented-out lines under "helper side, optional: unset
is fine". Only `UNMUTE_ICE_SERVERS` is fictional, one line of speculative prose
in `docs/PRODUCTION_ROADMAP.md:192`.

Two names are live but orphaned in documentation:
`UNMUTE_TELEPHONY_BRIDGE_PORT` (`templates/livekit_v1/telephony_bridge.py.tmpl:39`)
and `UNMUTE_AGENT_HEALTH_PORT` (`templates/livekit_v1/agent.py.tmpl:870`). One
template line each, named by no Go code, no document, no test, no golden.

### Where the guard test belongs

`internal/skill/agreement_test.go`. It is the only test file that already
reaches both `docs-site/` and the shipped bundle, and it already contains the
precedent: `TestNoSecretsInTheBundle` at line 367, a regex-over-a-whole-tree
prohibition test. Available helpers: `bundleFile` (`:25`), `referenceNames`
(`:39`), `sitePages` (`:210`, a `WalkDir` over `../../docs-site` with a
"found no pages, so this test would pass for the wrong reason" guard at `:231`),
and `Bundle.Files(Canonical|Pointer)`. It does not currently reach root
`README.md` or the scaffold; both are one-liners to add.

`internal/generate/examples_test.go` is second choice: its `WalkDir` is inline,
it walks `docs/` rather than `docs-site/`, and package `generate` cannot import
`internal/skill` without a new dependency edge.

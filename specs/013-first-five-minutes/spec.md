# Feature Specification: Make the first five minutes work, and stop lying quietly

**Feature Branch**: `013-first-five-minutes`

**Created**: 2026-08-15

**Status**: Draft

**Input**: User description: "013 — Make the first five minutes work, and stop lying quietly. Six parts: (1) fix silent drops in validation/codegen (unattached controls vanish; browser-only packages emit unreachable human_transfer; secrets completeness check only runs when a secrets block exists; Pipecat Dockerfile never copies tools/; turn model name validated at compile not validate). (2) unmute init writes a web-audio agent worth talking to: prompt matches the channel, no unused DAILY_API_KEY, one model id everywhere, real voice prompt, secrets block. (3) no UNMUTE_* env name on any beginner path or in the skill bundle, guarded by a test. (4) every example validates, compiles, and runs; examples/README.md true; Langfuse not required for first-run. (5) docs state the rules ir.Validate enforces (telephony capacity.peak_starts_per_second, warm transfer needs outbound: true) in docs-site/ and docs/, plus an audit of every validator error string. (6) subagent verification waves: reproduce, verify, clean room (5 first-run, 10 authoring at 8/10 bar, 2 adversarial)."

## Why this feature exists

Unmute promises, out loud, in `docs-site/transfers/overview.mdx`, that a package
"never compiles into something that quietly does nothing." Constitution
Principle II says the same thing in normative terms: a field MUST NEVER silently
do nothing.

Eight defects break that promise in the same shape: **the CLI exits 0 while the
thing the author asked for does not exist in the generated project.** Five were
found by the eighteen-agent clean room in PR #80 and recorded in
`specs/011-coding-agent-skill/tasks.md`. One more came from the complexity
cleanup. Two more were found while writing this brief.

Separately, the on-ramp contradicts itself. `unmute init` writes a prompt that
describes a phone call for a package with no phone channel, and an
`.env.example` asking for a key the build does not use. A first-time reader who
reads carefully finds a lie in the first file they open.

**The goal: a person runs `unmute init`, then `unmute dev`, and talks to a
genuinely good voice agent. Nothing in between contradicts itself.**

## This feature lands on top of the coding-agent skill

PR #80 shipped `unmute skill install`, a bundle of `SKILL.md` plus twelve
reference files under `internal/skill/assets/`, and six agreement tests plus a
golden that hold the bundle to the code. The bundle **restates facts Go code
owns**: validation behaviour, the scaffold's output, the default model id,
environment names.

Two consequences bind this feature:

1. **The bundle is the fifth documentation surface, and CLAUDE.md already
   says so.** Since PR #80 landed, CLAUDE.md's rule is "five places document a
   change, not one": the emitted `build/<target>/README.md` template, the
   source example's own `README.md`, the page in `docs/`, the page in
   `docs-site/`, and the skill under `internal/skill/assets/`. For every
   change in this feature that touches emitted behaviour, validation
   behaviour, the scaffold, or an environment name, **all five move in the
   same commit.** A change that turns an agreement test red is a change that
   would otherwise have left the bundle teaching something untrue.

2. **One bundle section exists only because of a defect this feature fixes.**
   `internal/skill/assets/references/transfers.md`, the section headed *"A
   transfer needs a phone call, and a browser package will not tell you"*,
   currently teaches coding assistants to enforce a rule themselves "because the
   compiler will not", and tells them not to write a `human_transfer` into a
   browser package because "it will look like it worked, and it will do
   nothing." Once the compiler refuses, that section must be rewritten to say
   the product refuses and to quote the refusal. Leaving a workaround in place
   for a fixed bug is the same class of lie this feature exists to remove.

`docs.slng.ai` is not published: PR #80 measured `200` on the root and `404` on
every deep link, stripped every site URL out of the bundle, and made
`unmute validate` the authority instead. `TestBundleNamesNoSitePage` keeps them
out. Site URLs MUST NOT be reintroduced into the bundle, and where this
specification says "the docs", it means the local `docs-site/` and `docs/`
trees.

## Clarifications

### Session 2026-08-15

- Q: Which single model identifier should every surface use? → A: `gpt-5.6-luna`, authored as `provider: openai` plus `model: gpt-5.6-luna`
- Q: What reasoning effort should the scaffold and examples set on a reasoning-family model used for speech? → A: `minimal`, via the existing `params:` pass-through
- Q: `gpt-5.6-luna`'s support for `temperature` is not stated in OpenAI's documentation. What happens to the `temperature: 0.2` the examples carry? → A: drop it from the scaffold and all eleven examples
- Q: An environment variable that configures a vendor's own component should carry that vendor's prefix, not Unmute's. What about the ones no vendor owns? → A: vendor-owned names take the vendor prefix; the generated agent's own runtime names keep `UNMUTE_*`
- Q: What is the bar for showing an environment variable at all, so a test can enforce it? → A: only author-set names are shown; everything else is hidden
- Q: Once `.env.example` holds only the names the author supplies, where does a person deploying for real find the platform connection values? → A: in the emitted README's carrier setup steps, which already say where to get each one; no env file lists them

**The classification already exists in the code.** An adversarial pass over the
real generated files found that `internal/target/telephony.go` already carries
`LocallySuppliedEnvironment` per route, cloned into the IR as
`LocalEnvironment`. It correctly marks `REDIS_URL` on the Pipecat carrier route,
and `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET`, `LIVEKIT_URL`, `REDIS_URL` on the
LiveKit SIP route. So this feature adds no new concept. It fixes three things
that ignore the one that is there.

**Three findings, each verified against a compiled example.**

1. **One fact, two implementations.** The LiveKit env template reads
   `LocalEnvironment` and prints a labelled "supplied for you, not by you"
   block. The Pipecat template ignores it, so `REDIS_URL` lands alphabetically
   in the fill-in list between `OPENAI_API_KEY` and `SLNG_API_KEY`, with no
   label at all, on a route where `telephony.go:122` has already marked it
   locally supplied. Principle III forbids exactly this.
2. **Two names are missing from the field.** `UNMUTE_PUBLIC_URL` and
   `UNMUTE_OUTBOUND_TOKEN` sit in `RuntimeEnvironment` but not in
   `LocallySuppliedEnvironment`, even though `unmute dev` mints both at
   `internal/cli/dev_telephony.go:87-92` and `:153`. That omission is why they
   render as blanks the author is told to fill.
3. **LiveKit asks for a variable its own comment says nothing reads.** The
   generated file prints `REDIS_URL=` under a comment ending "which this agent
   never reads". Confirmed: the only Python that reads it is
   `templates/pipecat_v1/telephony_shared.py.tmpl:35`, and
   `templates/livekit_v1/compose.telephony.yaml.tmpl:14` explicitly excludes it
   from the agent service. On LiveKit it is consumed by the local Compose
   graph's own containers, which `unmute dev` runs. This is the Part 2.2 defect,
   "asks for a key the build does not use", surviving on the telephony path.

**The author-set set has one definition, and it is already computed.** Verified
against the compiled LiveKit example, the eight names the author genuinely
supplies come from exactly five sources: model provider key names, connection
`environment:` values, `destinations:` values, tool `url_env` and `token_env`,
and handler `os.environ` reads. That is `referencedEnvNames` in
`internal/ir/validate.go:1301` plus the provider keys FR-005a adds. **The same
set that drives the secrets completeness warning drives the env file and the
README list.** One definition, three consumers, no new abstraction.

**The naming rule, stated once.** If a variable configures a service, it is
named after that service. `TWILIO_*`, `OPENAI_*`, `SLNG_*`, `DAILY_*`,
`LIVEKIT_*`. Unmute is not a service the generated project talks to, so an
`UNMUTE_` prefix is only correct where the variable belongs to the generated
agent itself and to no vendor.

`UNMUTE_DAILY_ROOM_GEO` is the clearest case for the rule: it is a Daily room
setting whose name claims Daily and Unmute at once. `UNMUTE_HOLD_AUDIO_URL` is
the same knob's sibling. The three LiveKit host-port mappings are the same
mistake in the other direction, an `UNMUTE_` prefix on ports that exist only
because a LiveKit container is running.

**The visibility rule, stated once.** `build/<target>/.env.example` and the
emitted README's set-these list contain **only names the author supplies**. Not
a labelled block of things they do not set, not a commented-out section: absent.
Everything else stays discoverable where a developer would look for it —
`compile-report.json` already carries `required_env`, and the Compose file
carries its own interpolation defaults — and is documented in prose only where
it is genuinely useful, which the author's own rule allows ("unless something is
necessary or helpful for developers"). A port-conflict escape hatch qualifies. A
list of variables `unmute dev` sets for you does not.

**Why this is stronger than "it reads like a credential".** Principle I says a
generated project "MUST carry no Unmute dependency" and "MUST be readable,
runnable, and deployable with Unmute absent". A variable named `UNMUTE_` inside
a project Unmute is not part of is the dependency-shaped thing that principle
forbids, independent of whether a reader mistakes it for a secret. That makes
the vendor renames a MUST rather than the SHOULD this spec first drafted, and it
is why the earlier decision to skip the renames entirely was wrong.

**Verification, 2026-08-15, against OpenAI's own API documentation.**
`gpt-5.6-luna` appears in the Chat Completions supported-model list. The family
is three tiers: `gpt-5.6-sol` for frontier capability, `gpt-5.6-terra` for a
balance of intelligence and cost, and `gpt-5.6-luna` for "efficient,
high-volume workloads", described elsewhere as "optimized for faster, more
cost-effective responses" and offering "lower cost and reduced latency". The
bare `gpt-5.6` alias defaults to `sol`, so the tier must be named explicitly.

**On the authored shape.** The identifier is written as two fields, not one
string. `docs/SCHEMA.md` N15 rejected folding the provider into the model
string by name: "the forwarded model identity is not uniform across vendors
(OpenAI wants `gpt-4.1-mini`, SLNG wants `slng/deepgram/nova:3-en`), so a parse
would mangle what reaches the SDK". A `model:` value of `openai/gpt-5.6-luna`
would be forwarded verbatim to the SDK and fail. Every surface writes
`provider: openai` and `model: gpt-5.6-luna`.

**On reasoning effort.** GPT-5.6 is a reasoning family, and OpenAI's migration
guidance is to "maintain the existing workload role and reasoning effort
initially, then evaluate success metrics including latency". A voice turn that
pauses to think before speaking is the difference between a good agent and a
broken-sounding one, and this feature's whole goal is a spoken exchange that
works. `reasoning_effort` is a documented Chat Completions parameter taking
`none | minimal | low | medium | high | xhigh | max`, and Unmute already
forwards `params:` verbatim on a think model, so `minimal` needs no schema
change. It is confirmed by ear in SC-003, not by assumption.

**On temperature.** OpenAI's reference states that "parameter support varies by
model, with specific constraints applying to newer reasoning models", and does
not say whether this model accepts `temperature`. `docs/SCHEMA.md:307` has
`temperature` as optional, so dropping it costs nothing and removes a line from
twelve files. Keeping an unverified parameter would put the failure on a live
call, which is the exact defect class this feature exists to remove, and
CLAUDE.md requires provider claims to be verified against official
documentation rather than assumed. Under Principle IV an unverified claim stays
gated.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Nothing compiles green while silently dropping what the author declared (Priority: P1)

An author declares something in the package: a control, a destination, a tool, a
secret, a local Python tool, a turn detector model. Every one of those either
reaches the generated project, or the CLI says out loud that it did not, naming
the thing, the file, and the line. There is no third outcome.

**Why this priority**: This is the constitutional violation. A validation error
the author never sees is worse than no validation, because they stop reading.
Everything else in this feature is quality of life; this is correctness. It is
also the only part whose absence causes a live phone call to fail in front of a
real customer.

**Independent Test**: For each of the six defects below, author the smallest
package that triggers it, observe the current silent-green behaviour, apply the
fix, and observe a named refusal or a named warning. Every one is testable
without Python and without a network.

**Acceptance Scenarios**:

1. **Given** a telephony package with a valid SIP connection, a `human_transfer`
   declared under `controls:`, and no agent listing it under `tools:`,
   **When** the author runs `unmute validate`,
   **Then** validation fails naming the control, its source file and line, and
   the agents it could attach to; and the run does not exit 0.

2. **Given** the same shape for any other control kind — `agent_transfer`,
   `delegate` — or a `destinations:` entry nothing resolves to, or a top-level
   `tools:` entry no agent uses, or a task or task group nothing reaches,
   **When** the author runs `unmute validate`,
   **Then** the same class of refusal fires, naming the same three things.

3. **Given** a package whose only channel is `web: realtime_audio`, with a
   LiveKit target, no connection, and a `human_transfer` attached to the agent,
   **When** the author runs `unmute validate`,
   **Then** it is refused under the existing native-route rule, in the same
   shape of message the unsupported-route refusal already uses, and the LiveKit
   refusal matches the Pipecat one that already fires today.

4. **Given** a package that writes environment names and has **no** `secrets:`
   block at all,
   **When** the author runs `unmute validate`,
   **Then** the completeness cross-check runs anyway and warns on stderr, naming
   every missing name with its source file and key, and exits 0.

5. **Given** a Pipecat package with at least one local Python tool,
   **When** the generated container image is built and started,
   **Then** it imports its tools successfully instead of dying with
   `ModuleNotFoundError: No module named 'tools'`.

6. **Given** a LiveKit package whose turn detector names a model the LiveKit
   driver does not recognise,
   **When** the author runs `unmute validate`,
   **Then** the refusal fires at validate, carrying the source file and line,
   rather than surfacing for the first time from `compile` without a location.

---

### User Story 2 - `unmute init` writes a package worth talking to, and it agrees with itself (Priority: P2)

A first-time author runs `unmute init`, sets `OPENAI_API_KEY` and
`SLNG_API_KEY`, runs `unmute dev`, and speaks to a good voice agent. Every file
the scaffold wrote is true about the package it wrote.

**Why this priority**: This is the first five minutes. It is second only because
a silent drop can break a production call, while a self-contradicting scaffold
only wastes an evening. But it is the highest-frequency defect: every new user
hits it.

**Independent Test**: Run `unmute init`, read every file it wrote as a
first-time user, and find no statement contradicted by another file in the same
package or by the package's own generated build. Then run `unmute dev` with
exactly two environment variables set and hear a greeting.

**Acceptance Scenarios**:

1. **Given** a freshly scaffolded package whose only channel is
   `web: realtime_audio`,
   **When** the author reads `instructions.md` and the greeting,
   **Then** neither says the session is a phone call, and both match the channel
   the package actually declares.

2. **Given** a freshly scaffolded package,
   **When** the author compares the package-root `.env.example` with the
   generated `build/<target>/.env.example`,
   **Then** the two agree on every name, because both derive from the same
   source rather than one being hand-written.

3. **Given** a freshly scaffolded package,
   **When** the author runs `unmute dev` with only `OPENAI_API_KEY` and
   `SLNG_API_KEY` set,
   **Then** it reaches a spoken greeting with no other credential.

4. **Given** the scaffold, all eleven examples, every page under `docs/` and
   `docs-site/`, the root `README.md`, and the skill bundle,
   **When** each is grepped for a model identifier,
   **Then** exactly one identifier appears across all of them, and a test fails
   if any surface drifts from it.

5. **Given** a freshly scaffolded package,
   **When** the author runs `unmute validate`,
   **Then** the package declares a `secrets:` block listing the names it writes,
   so the rule from User Story 1 scenario 4 is demonstrated rather than dodged.

6. **Given** the scaffolded `instructions.md`,
   **When** a reader who has never written a voice prompt reads it,
   **Then** it shows the shape of a good voice prompt — who the agent is, a
   voice contract, what it will not do, and a greeting that matches the channel
   — short enough to rewrite in one sitting.

---

### User Story 3 - No Unmute-branded environment name appears to be a credential (Priority: P3)

Unmute is a compiler and is never in the call path. It has no runtime credential
of its own. A reader never sees an `UNMUTE_*` name presented as something they
must obtain or generate.

**Why this priority**: It costs a new user real time and real confusion — they
go looking for an Unmute account that does not exist — but it never breaks a
call. It is also the cheapest of the three to hold with a test.

**The audit moved this story's target.** The beginner path is **already clean**:
zero `UNMUTE_` hits in the site index, all of `docs-site/start/` including the
coding-agents page, all of `docs-site/build/`, the root `README.md`, everything
`unmute init` writes, and every byte under `internal/skill/assets/`.
`docs-site/reference/secrets.mdx` is also already right — its **user-set** table
contains no `UNMUTE_*` name at all, and the two that appear sit in the following
"not yours to declare" table under the heading "supplied by the route, the
platform, or `unmute dev`". The default beginner journey surfaces exactly one
`UNMUTE_` name, `UNMUTE_DEV_PORT`, inside a Compose port mapping, where it
cannot read as a credential.

Nothing in the product surface is dead either. `UNMUTE_OUTBOUND_TOKEN` still has
live readers on the Pipecat carrier-websocket and LiveKit connector routes;
`specs/006` removed the outbound endpoint only from the Daily-carrier helper.
`UNMUTE_HOLD_AUDIO_URL` and `UNMUTE_DAILY_ROOM_GEO` are live and already
presented correctly, as commented-out optional lines under "helper side,
optional: unset is fine". The only fictional name in the repository is
`UNMUTE_ICE_SERVERS`, one line of speculative prose.

**So the real defect is downstream of the page that gets it right.** The
generated `build/<target>/.env.example` and the emitted README contradict
`secrets.mdx` by printing `UNMUTE_PUBLIC_URL=` and `UNMUTE_OUTBOUND_TOKEN=` as
bare fill-in blanks under "required by the target or a connection" and "Set
these before running", alongside real credentials like `TWILIO_AUTH_TOKEN`. And
`docs/TELEPHONY.md` tells the reader to "**Generate this secret yourself** with
a cryptographically secure password generator" for a token `unmute dev` already
mints.

**And two names claim a vendor's territory while wearing Unmute's prefix.**
`UNMUTE_DAILY_ROOM_GEO` and `UNMUTE_HOLD_AUDIO_URL` configure a Daily room. The
three `UNMUTE_LIVEKIT_*` port mappings exist only because a LiveKit container is
running. Under the naming rule in the Clarifications section, those five take
their vendor's prefix.

**Independent Test**: Compile an example with outbound telephony and read the
generated `.env.example` and README as an author. The env file lists only names
the author supplies, and the README's set-these list matches it exactly. Then
grep the beginner path and the bundle: still zero, and a test now keeps it that
way.

**Acceptance Scenarios**:

1. **Given** an outbound telephony package,
   **When** the author reads `build/<target>/.env.example`,
   **Then** it contains only names the author supplies. `UNMUTE_PUBLIC_URL` and
   `UNMUTE_OUTBOUND_TOKEN` do not appear at all, in any form, including
   commented out.

2. **Given** the same package,
   **When** the author reads the emitted README's "set these before running"
   list,
   **Then** it names exactly the same set as the env file and nothing more.

3. **Given** `docs/TELEPHONY.md`,
   **When** it describes the outbound token,
   **Then** it does not instruct the reader to generate a secret that
   `unmute dev` mints for them.

4. **Given** a variable that configures a vendor's own component,
   **When** it is named,
   **Then** it carries that vendor's prefix. Only variables belonging to the
   generated agent itself, owned by no vendor, keep `UNMUTE_*`.

5. **Given** an operator deploying the generated project by hand,
   **When** they need the full set of names the runtime requires,
   **Then** `compile-report.json` still carries `required_env`, so hiding a
   name from the author-facing files does not make it unrecoverable.

6. **Given** the site index, everything under `docs-site/start/` including the
   coding-agents page, everything under `docs-site/build/`, the root
   `README.md`, everything `unmute init` writes, and everything under
   `internal/skill/assets/`,
   **When** they are grepped for `UNMUTE_`,
   **Then** there are zero hits — as there already are — and a test fails on the
   first one added back.

---

### User Story 4 - Every example is meaningful, and every example runs (Priority: P4)

An author picks an example from the table in `examples/README.md`, and it does
what the table says, validates, compiles, and starts — without signing up for a
third service first.

**Why this priority**: Examples are how people learn this tool after the
scaffold. They are downstream of User Stories 1 to 3 because those change
generated output, so this work has to follow them.

**Independent Test**: For each of the eleven examples, run validate and compile
for every target it declares, start the generated project (browser examples to a
greeting; telephony examples to a container that builds and imports), and read
its page against what the table claims.

**Acceptance Scenarios**:

1. **Given** any of the eleven examples,
   **When** validate and compile are run for every target it declares,
   **Then** both are clean.

2. **Given** a browser-only example,
   **When** `unmute dev` runs it,
   **Then** it reaches a greeting. **Given** a telephony example, the generated
   container builds and imports its modules.

3. **Given** the first example a beginner is pointed at,
   **When** they try to run it,
   **Then** no third-party observability account is required. Tracing is
   demonstrated in exactly one example whose stated purpose is tracing, and the
   table says so.

4. **Given** `examples/README.md`, `docs-site/build/your-first-agent.mdx`, and
   `internal/skill/assets/references/examples.md`,
   **When** a reader asks "which example do I start with",
   **Then** all three name the same one.

5. **Given** `docs-site/build/your-first-agent.mdx`,
   **When** it shows a package file,
   **Then** either it shows the real file, or it says plainly that it is
   reduced and names what was removed.

6. **Given** any example's `README.md`,
   **When** it is read after this feature's fixes,
   **Then** it is true, it names every `transport` its targets declare, and
   every link out of it resolves.

---

### User Story 5 - Every rule validation enforces is findable in the docs (Priority: P5)

An author who hits a validation error can find the rule that produced it in the
documentation, next to the field it constrains, without reading Go.

**Why this priority**: The clean room in PR #80 failed three of ten briefs, all
telephony, and both root causes were documentation rather than code: the rules
existed and validation enforced them correctly, but nothing next to the field
said so. It is last in priority order only because it changes no behaviour;
it is the highest-leverage item for the authoring bar.

**Independent Test**: Extract every error string `ir.Validate` can produce, and
for each one, find the rule stated in `docs/` or `docs-site/` on the page that
owns the field the error names.

**Acceptance Scenarios**:

1. **Given** a package with any channel set to `telephony`,
   **When** an author reads the channel documentation,
   **Then** it states that `capacity.peak_starts_per_second` must be positive
   and is required the instant any channel is telephony.

2. **Given** a warm transfer,
   **When** an author reads the channel documentation,
   **Then** it states that a warm transfer requires `outbound: true` on its
   channel, because it places a call — so an author writing an inbound line is
   not led to `outbound: false`.

3. **Given** the complete set of `ir.Validate` error strings,
   **When** each is looked up in `docs/` and `docs-site/`,
   **Then** every one is documented next to its field, or is listed in this
   feature's results file as knowingly undocumented with a reason.

---

### Edge Cases

- **A control is declared but deliberately unattached, mid-authoring.** The
  refusal fires on a package that is not finished. This is validation's job:
  `unmute validate` is the command that says a package is not ready. There is no
  legitimate finished package that declares a control nothing reaches, so this
  is an error, not a warning. If reproduction proves a legitimate case exists,
  the severity drops to a warning with identical wording and the reason is
  recorded here.
- **A `secrets:` block that lists a name the package does not write.** Out of
  scope: this feature only closes the missing-name direction, which is the one
  documented in N24.
- **An old package with no `secrets:` block that compiles today.** It must keep
  compiling. The completeness cross-check keeps its documented severity of
  warning with exit 0 precisely so no existing package breaks.
- **A Pipecat package with no local tools.** The container must not gain a copy
  of a directory that does not exist; the copy is conditional.
- **The telephony branch of the Pipecat container.** It is a separate branch of
  the same template and may or may not have the same gap; both branches are in
  scope.
- **A model identifier a provider later retires.** One home plus one agreement
  test makes the bump a one-line change; this feature does not attempt to track
  provider retirements.
- **The new model rejects `reasoning_effort` or the value `minimal` on a target
  the examples declare.** The parameter is forwarded verbatim through `params:`,
  so a rejection surfaces at runtime rather than at compile. SC-003's spoken
  exchange is where this is caught, and the fallback is to drop the parameter
  and record the latency that results.
- **The new model turns out to reject something else the examples forward.**
  `temperature` is removed under FR-012c for exactly this reason, but `top_p`,
  `max_tokens` in `params:`, and per-example generation settings carry the same
  unverified risk. Any that survive the sweep are listed in the results file
  rather than assumed safe.
- **An `UNMUTE_*` name embedded in generated Python or in a golden.** Renaming
  it is not free. Where the rename is not cheap, the name stays and the reason
  is recorded, but it still may not appear on a beginner path.

## Requirements *(mandatory)*

### Functional Requirements

**Silent drops (User Story 1)**

- **FR-001**: A control declared under `controls:` that no agent, task, or task
  group reaches MUST be refused by validation. The message MUST name the
  control, its source file and line, and the agents it could attach to. Every
  control kind MUST be checked, not only `human_transfer`.
- **FR-001a**: An unreferenced `models:` entry MUST stay legal.
  `docs/SCHEMA.md:287` calls the models map "a palette: entries that nothing
  currently references are legal", scoped to `models:` alone. This is the one
  carve-out FR-001 must not break, and nothing in the schema extends the same
  status to controls, destinations, or tools.
- **FR-002**: A `destinations:` entry nothing resolves to, and a top-level
  `tools:` entry no agent uses, MUST be refused by the same rule with the same
  message shape. These are not merely dead: an unreferenced destination's env
  name still reaches `.env.example`, the generated `REQUIRED_ENV` startup check,
  the compile report, and both compose files, so it becomes a **phantom required
  secret whose absence blocks the agent from starting**. An unreferenced tool
  leaks its `webhook.url_env` the same way.
- **FR-002a**: `compile-report.json` MUST NOT claim a capability the emitted
  agent does not have. `internal/ir/validate.go:655`, `routeCapabilitiesUsed`,
  walks `agent.Controls` with no tool-list filter, so an unattached control
  still produces a `cold_transfer` or `warm_transfer` evidence row while the
  generated agent has no such tool. Fixing FR-001 removes the package that
  causes this; the report MUST be checked to confirm it no longer disagrees with
  the artifact beside it.
- **FR-002b**: An unattached `agent_transfer` MUST NOT leave its unreachable
  target agent emitted as dead code, which is what happens today.
- **FR-003**: A `human_transfer` on a package with no telephony channel and no
  connection MUST be refused under the existing native-route rule, on **every**
  target. LiveKit MUST match the refusal Pipecat already produces, in the same
  shape as the existing unsupported-route message. The nearest existing wording
  is the one the **warm** shape already emits on this exact package — `warm
  transfer needs a telephony Connection: it dials the destination itself, using
  the connection's sip_address, sip_username, sip_password and from_number` —
  so the cold refusal should read as its sibling, not as a new invention. The
  defect is precisely that on one target, one control kind, the warm shape
  refuses and the cold shape compiles a dead tool.
- **FR-004**: FR-001 through FR-003 MUST fire before any artifact is written,
  at the earliest stage that can express each check. The reachability checks
  (FR-001, FR-002) are properties of the package graph alone, so they land in
  `ir.Build`, the only stage that carries the file and line FR-001 requires,
  and fire once for the package rather than once per target. The
  browser-transfer refusal (FR-003) is a property of the resolved target, so
  it lands in `ir.Validate`. This requirement first named `ir.Validate` for
  all three, which would have lost the position for no gain; research D1
  records the correction.
- **FR-005**: The `secrets:` completeness cross-check MUST run whether or not
  the package declares a `secrets:` block. Its severity MUST stay as documented
  in `docs/SCHEMA.md` N24: a warning on stderr with exit 0.
- **FR-005a**: The set of environment names the cross-check compares against
  MUST include the model provider key names the compiler already knows it
  requires. Today that set is built only from `*_env` fields, tracing keys,
  handler `os.environ` reads, connection `environment:` values, and
  `destinations:`, so a provider API key is never in it. Without this, a
  scaffolded package's reference set is empty and FR-005 stays vacuous exactly
  where FR-014 needs it to fire.
- **FR-005b**: A package with no `secrets:` block MUST NOT silently lose the
  generated startup check that `docs-site/reference/secrets.mdx` promises ("the
  generated agent refuses to start without them"). Today the emitted
  `REQUIRED_ENV` list and `require_env()` call disappear entirely when the block
  is absent, and when a name is left undeclared it drops out of the startup
  check too. Either the check is generated from the names the compiler knows it
  requires, or the documentation is amended to say the startup check is opt-in.
- **FR-005c**: `REQUIRED_ENV` MUST keep every name the runtime needs, including
  the ones FR-018 hides from `build/<target>/.env.example`. Dropping them would
  move the failure from container start to the moment a call arrives, which is
  the trade Principle II names as the worst one. Instead, the **message**
  changes: a missing name that is in `LocalEnvironment` MUST say where the value
  comes from, not only that it is absent, so a reader who never saw it in the
  env file is not left guessing. Names the author does supply keep their current
  wording. Without this, FR-018 and FR-005b contradict each other, and Pipecat
  proves it: `templates/pipecat_v1/telephony_state.py.tmpl:36` raises on a
  missing `REDIS_URL` that the Compose graph supplies and a hand deploy does
  not.
- **FR-006**: A generated Pipecat container MUST include the local tool modules
  the generated `bot.py` imports, in every branch of the container definition
  that can carry local tools, and MUST NOT include them when the package has
  none. This is a **regression**, not a permanent gap: the template was a single
  unbranched definition using `COPY . .` until 2026-08-13, and all three
  published tags carry the break. It affects five of the ten examples that
  declare a Pipecat target, including the one the table marks "Start here", and
  it breaks `unmute dev --target pipecat` as well as cloud deploys, because the
  generated compose file has no bind mount and runs the same image.
- **FR-006a**: A test MUST assert that every generated Python module the
  generated entrypoint imports is reachable inside the built image. The existing
  container-contract test asserts the presence of one `COPY` line and the
  absence of another against a fixture with **no local tools**, which is why
  this shipped. A fixture with a local tool is the minimum fix.
- **FR-007**: A turn detector model identifier the LiveKit driver does not
  recognise MUST be refused by `unmute validate`, before any artifact is
  written. The constitution's `validate`-versus-`compile` distinction is about
  which **providers** each command reaches, not about how deeply either checks:
  Principle V's sentences are all about the provider set, and the 1.1.0 sync
  note that introduced them says so. Nothing licenses a depth gap for a provider
  validate fully serves.

  The repository has already decided this question for the sibling field.
  `internal/ir/validate.go:954-957` mirrors a generator check into the validator
  with the reason written in the comment: "The generator errors on a slotless
  entry; mirror it here so a slotless language fails validate, not just generate
  (C6: gate before any artifact)." Two more `service_call.go` checks are
  mirrored the same way. The turn model was simply never mirrored. This is an
  omission, not a boundary.
- **FR-007a**: FR-007 is one member of a class of **eight** confirmed
  generator-only value checks, each of which lets a package pass `validate` at
  exit 0 and then fail `compile`: the turn detector model, `sdk_language`, three
  distinct `pins` checks, the target `version` semver and range checks (both
  drivers), and a speak entry whose vendor has no voice slot. All eight MUST be
  mirrored into the validator, or each one left in place MUST be recorded in the
  results file with its reason.
- **FR-007b**: **Moving a check into the validator does not buy a file and line,
  and the spec must not claim it does.** The repository has three error tiers:
  `spec.Load` and `ir.Build` errors carry `file:line`; `ir.Validate` errors are
  per-target rows printed as `<target>: <message>` with no position;
  `generate` errors have neither prefix nor position. FR-007's requirement is
  tier two — fail before any artifact is written — not tier one. A check that
  can be expressed against the authored document rather than the resolved
  target SHOULD go to tier one instead, and the plan says which tier each of the
  eight lands in.
- **FR-007c**: The legal turn detector model identifiers MUST appear in the
  documentation. `turn-detector-mini` and `turn-detector` appear in no `docs/`
  or `docs-site/` page today — only inside four examples' `targets.yaml`
  override blocks and in the Go error string. Worse, `docs/SCHEMA.md:317` offers
  **`silero` as its own example value** for this field, and §4.3 states
  identities are "forwarded to the provider as-is and never validated (D10)".
  The driver contradicts the locked schema twice. Under Principle IV the
  document wins, so `docs/SCHEMA.md` MUST be amended deliberately as a dated,
  numbered amendment that states the LiveKit exception and corrects the example,
  rather than the code being left to disagree with it quietly.
- **FR-007d**: The same authored `turn.detector.model` value compiles cleanly on
  Pipecat and fails on LiveKit, so one `agent.yaml` is legal or illegal
  depending on a target the author may add later. The refusal MUST name the
  target, and the documentation from FR-007c MUST say the constraint is
  LiveKit's.
- **FR-008**: Every refusal added by FR-001 through FR-007 MUST have a test that
  fails before the fix and passes after it. Tests that need Python MUST live
  under the `smoke` build tag so `go test ./...` stays Python-free.

**The scaffold (User Story 2)**

- **FR-009**: `unmute init` MUST write a package whose prompt and greeting match
  the channel it declares. A web-audio scaffold MUST NOT describe a phone call.
- **FR-010**: `unmute init` MUST NOT declare a transport the package's channels
  do not need, and MUST NOT list a credential the package's own generated build
  does not require. The scaffolded package MUST compile and run with no
  credential beyond `OPENAI_API_KEY` and `SLNG_API_KEY`.
- **FR-011**: The package-root `.env.example` MUST NOT be kept in sync with
  `build/<target>/.env.example` by hand. Three resolutions are acceptable:
  derive it from the same source, delete it, or **hold the two equal with a
  test that fails on drift**. The third is what research D9 chooses, because the
  two files have no shared code today and unifying them means either running the
  full load-build-generate path at `init` time or emitting into the package
  root, which are real refactors for a three-line file. Once FR-010 removes the
  phantom transport the two sets are identical by construction, and the test is
  the shortest thing that fails if that stops being true.
- **FR-012**: Exactly one model identifier MUST appear across the scaffold, all
  eleven examples, `docs/`, `docs-site/`, the root `README.md`, and
  `internal/skill/assets/`. A test MUST fail when any surface drifts. The
  identifier MUST have one Go home that the test reads, rather than being a
  string literal the test also hard-codes. The existing bundle agreement test
  holds **vendor names** parsed out of a markdown table and never looks at a
  model identifier at all, so extending it would mean a new regex over fenced
  code blocks and a new import edge; a separate test is the right shape, and the
  spec records that as a deliberate choice rather than a duplicate.
- **FR-012a**: The identifier MUST be authored as two fields, `provider: openai`
  and `model: gpt-5.6-luna`. No surface may write `openai/gpt-5.6-luna` as a
  single `model:` string: `docs/SCHEMA.md` N15 rejected folding the provider into
  the model string by name, and the value is forwarded to the SDK verbatim, so a
  combined string fails at the provider. The test from FR-012 MUST catch a
  combined form, not only a stale identifier.
- **FR-012b**: The scaffold and every example MUST set
  `params: {reasoning_effort: minimal}` on the think model. `gpt-5.6-luna` is a
  reasoning-family model, and an unbounded reasoning budget before each spoken
  turn is a bad voice agent regardless of how good the answer eventually is.
  `reasoning_effort` is a documented Chat Completions parameter and `params:` is
  already forwarded verbatim, so this needs no schema change. The value MUST be
  confirmed by ear under SC-003, not assumed.
- **FR-012c**: `temperature` MUST be removed from the scaffold and all eleven
  examples. OpenAI's reference states that parameter support varies by model
  with specific constraints on newer reasoning models, and does not say whether
  this model accepts it. `docs/SCHEMA.md:307` has the field as optional, so
  removing it costs nothing, while keeping an unverified parameter would put the
  failure on a live call. If a later verification proves the model accepts it,
  re-adding it is a separate, deliberate change.
- **FR-013**: The scaffolded `instructions.md` MUST be a small, genuinely good
  voice prompt: who the agent is; a voice contract covering plain text only, one
  or two sentences per turn, one question at a time, no stalling, and numbers
  read digit by digit; what the agent will not do; and a greeting matching the
  channel. It MUST agree with
  `internal/skill/assets/references/prompting.md`.
- **FR-014**: `unmute init` MUST write a `secrets:` block listing the names the
  package writes, so a fresh package demonstrates FR-005 instead of dodging it.
- **FR-015**: The scaffolded `agent.yaml` MUST keep its explanatory comments,
  and every comment MUST be true of the package it ships in.
- **FR-016**: The prebuilt `end_call` tool MUST stay in the scaffold as the
  smallest real demonstration that tools exist.
- **FR-016a**: Every instruction the scaffold prints or writes MUST work when
  followed literally from where the reader is standing. The scaffolded
  `.env.example` currently says "Copy to `.env`, fill in, then run
  `unmute dev <name>`" **inside** the directory it names, so following it
  resolves to `<name>/<name>` and fails.
- **FR-016b**: Every comment in a scaffolded file MUST describe something that
  file actually contains. `targets.yaml` currently says "Secrets and URLs are
  env var names only" in a package that declares no secrets, in the file that
  would not hold them anyway, sending a first-time reader looking for where a
  key goes to the wrong file.
- **FR-016c**: The scaffold golden MUST cover what the CLI actually emits. It
  is currently generated from a call with **no tools**, so it is missing the two
  `tools:` blocks and the whole of `tools/end_call.yaml` that every real
  `unmute init` writes — a change to the default tool template is invisible to
  `go test ./...`.
- **FR-016d**: `docs-site/start/quickstart.mdx` MUST stop teaching the defects
  this feature removes. It currently prints "Hi, thanks for calling." as the
  expected browser greeting, and explains `DAILY_API_KEY` away as "only needed
  if you later run this agent on a phone number", which is not true of a
  package whose own generated build omits the name.
- **FR-016e**: The repository-root `.env.example` MUST be true. Today it fails
  three ways: `OPENAI_MODEL` is dead (nothing reads it; model identifiers come
  from `agent.yaml`, never the environment, and it is set to the identifier
  half the repository disagrees with), the header claims it holds "env vars
  for agent to run" when no package lives at the repository root, and its
  Langfuse comment says the keys are needed "for the examples" when FR-024
  reduces that to exactly one example. The dead variable is removed, the
  header states what the file is for (names to copy into an example's own
  `.env`), and the Langfuse comment points at the `examples/README.md` row
  that names the one tracing example. If the rewrite shows the file duplicates
  the per-example env files entirely, deleting it is an acceptable outcome,
  recorded with the reason.

**Environment names (User Story 3)**

- **FR-017**: Every `UNMUTE_*` name has been traced to a writer and a reader.
  The audit found **no dead name on the product surface**; the only fictional
  one is `UNMUTE_ICE_SERVERS`, a single line of speculative prose in
  `docs/PRODUCTION_ROADMAP.md`, which MUST be deleted or reworded.
  `UNMUTE_OUTBOUND_TOKEN` is **not** dead — `specs/006` removed the outbound
  endpoint only from the Daily-carrier helper, and the Pipecat
  carrier-websocket and LiveKit connector routes still serve it and still read
  the token.
- **FR-018**: `build/<target>/.env.example` MUST contain **only names the author
  supplies**. A name the author does not set MUST NOT appear in it at all, in
  any form, including commented out or inside a labelled block. Measured on a
  compiled example, that means four names leave the LiveKit file
  (`LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET`, `LIVEKIT_URL`, `REDIS_URL`) and one
  leaves the Pipecat file (`REDIS_URL`), plus `UNMUTE_PUBLIC_URL` and
  `UNMUTE_OUTBOUND_TOKEN` on outbound routes.
- **FR-018a**: The author-set set MUST have **one** definition, the one that
  already drives the secrets completeness check: model provider key names,
  connection `environment:` values, `destinations:` values, tool `url_env` and
  `token_env`, and handler `os.environ` reads. `build/<target>/.env.example`,
  the emitted README's set-these list, and the secrets warning are three
  consumers of that one set. A second list of "what the author fills in" is
  forbidden.
- **FR-018b**: The two env templates MUST agree. Today the LiveKit template
  reads `LocalEnvironment` and renders a labelled block while the Pipecat
  template ignores it entirely, so the same locally-supplied `REDIS_URL` is
  labelled on one target and rendered as a bare fill-in on the other. After this
  change both simply exclude that set.
- **FR-018c**: `UNMUTE_PUBLIC_URL` and `UNMUTE_OUTBOUND_TOKEN` MUST be added to
  `LocallySuppliedEnvironment` in `internal/target/telephony.go`. `unmute dev`
  mints both at `internal/cli/dev_telephony.go:87-92` and `:153`, so their
  absence from that field is a data error, and it is the reason they render as
  blanks today.
- **FR-018d**: `REDIS_URL` MUST NOT be presented to a LiveKit author at all. The
  generated comment already says "which this agent never reads", and that is
  true: the only Python reading it is
  `templates/pipecat_v1/telephony_shared.py.tmpl:35`, and
  `templates/livekit_v1/compose.telephony.yaml.tmpl:14` excludes it from the
  agent service. On LiveKit it belongs to the local Compose graph's own
  containers.
- **FR-018e**: Hiding a name MUST NOT make it unrecoverable. The platform
  connection values MUST stay findable in the emitted README's carrier setup
  steps, which already say where each comes from rather than only naming it:
  "get `LIVEKIT_URL` and the API key pair from the LiveKit Cloud project
  settings, or from a self-hosted LiveKit Server configuration". `required_env`
  in `compile-report.json` stays complete as the machine-readable form. No
  second generated env file is created.
- **FR-018f**: Prose documentation of a non-author name is allowed only where it
  is genuinely useful to a developer, which is the exemption the naming rule
  itself carries. A port-conflict escape hatch qualifies and stays, in a
  troubleshooting section rather than a to-do list.
  `UNMUTE_TELEPHONY_BRIDGE_PORT` and `UNMUTE_AGENT_HEALTH_PORT`, each read by
  exactly one generated template line and named by no Go code, document, test,
  or golden, are documented there or deleted.
- **FR-018g**: `docs/TELEPHONY.md` MUST stop instructing the reader to
  "generate this secret yourself with a cryptographically secure password
  generator" for the outbound token, which `unmute dev` already mints.
- **FR-019**: Zero `UNMUTE_*` occurrences MUST remain in: the site index,
  everything under `docs-site/start/`, everything under `docs-site/build/`, the
  root `README.md`, everything `unmute init` writes, and everything under
  `internal/skill/assets/`.
- **FR-020**: A test MUST enforce FR-019 and fail on the first reintroduction.
- **FR-021**: A variable that configures a vendor's own component MUST carry
  that vendor's prefix. Five names claim Unmute's prefix for something a vendor
  owns, and all five are cheap: no Go, no golden, or a single literal.

  | Today | Becomes | Why |
  |---|---|---|
  | `UNMUTE_DAILY_ROOM_GEO` | `DAILY_ROOM_GEO` | a Daily room setting whose name already says Daily |
  | `UNMUTE_HOLD_AUDIO_URL` | `DAILY_HOLD_AUDIO_URL` | the same knob's sibling on the Daily helper |
  | `UNMUTE_LIVEKIT_PORT` | `LIVEKIT_HOST_PORT` | a host mapping that exists only because a LiveKit container runs |
  | `UNMUTE_LIVEKIT_SIP_PORT` | `LIVEKIT_SIP_HOST_PORT` | as above |
  | `UNMUTE_LIVEKIT_RTP_PORT_RANGE` | `LIVEKIT_RTP_HOST_PORT_RANGE` | as above |

  The three LiveKit names keep `HOST` in them so they cannot be confused with
  anything the LiveKit server or SDK reads inside the container. The repository
  has no existing bare `LIVEKIT_PORT`, `LIVEKIT_SIP_PORT`, or
  `LIVEKIT_RTP_PORT_RANGE`; the apparent hits are substrings of the `UNMUTE_*`
  names being replaced.
- **FR-021a**: Variables belonging to the generated agent itself, owned by no
  vendor, **keep `UNMUTE_*`**: `UNMUTE_PUBLIC_URL`, `UNMUTE_OUTBOUND_TOKEN`,
  `UNMUTE_LOG_LEVEL`, `UNMUTE_AGENT_HEALTH_PORT`,
  `UNMUTE_TELEPHONY_BRIDGE_PORT`, `UNMUTE_DEV_PORT`, `UNMUTE_TELEPHONY_PORT`,
  `UNMUTE_CALL_START`. FR-018 hides all of them from author-facing output, so
  the prefix stops being something a reader meets, and the wide renames those
  would need — roughly 44 to 53 sites each for the two medium ones, and 23
  golden lines across five goldens for `UNMUTE_CALL_START` — buy nothing once
  nobody sees them.
- **FR-021b**: `UNMUTE_SIP_TRUNK_ID` is **not an environment variable**. It is a
  substitution token inside one generated JSON file, replaced by `sed` in the
  setup script, and `specs/005` says so explicitly. It MUST stop being shaped
  like an environment variable so no reader tries to set it.
- **FR-021c**: The twelve test-harness names (`UNMUTE_SWEEP_*`, `UNMUTE_TEST_*`,
  `UNMUTE_SMOKE_*`) never leave the test binaries and never reach an author.
  They are out of scope.

**Examples (User Story 4)**

- **FR-022**: Every example MUST validate and compile cleanly for every target
  it declares.
- **FR-023**: Every browser-only example MUST reach a greeting under
  `unmute dev`. Every telephony example's generated container MUST build and
  import its modules.
- **FR-024**: No example a beginner is pointed at may require a third-party
  observability account to run. Tracing MUST be demonstrated in exactly one
  example whose stated purpose is tracing, and `examples/README.md` MUST say so.
- **FR-025**: `examples/README.md`, `docs-site/build/your-first-agent.mdx`, and
  `internal/skill/assets/references/examples.md` MUST name the same starting
  example.
- **FR-026**: Where a documentation page shows a package file, it MUST show the
  real file or state plainly that it is reduced and what was removed.
- **FR-027**: Every example's `README.md` MUST be true after this feature's
  fixes, MUST name every `transport` its targets declare, and every link out of
  it MUST resolve. An example whose row in `examples/README.md` makes no claim
  another row does not already make MUST be deleted rather than maintained, and
  the deletion recorded with its reason. That row-uniqueness test is the bar,
  because it is the one a reader actually applies when choosing an example, and
  it is checkable without judgement.

**Documented rules (User Story 5)**

- **FR-028**: The requirement that `capacity.peak_starts_per_second` be positive
  whenever any channel is `telephony` MUST be stated in `docs-site/` and `docs/`
  next to the field it constrains.
- **FR-029**: The requirement that a warm transfer needs `outbound: true` on its
  channel MUST be stated in `docs-site/` and `docs/` next to the field it
  constrains, with the reason (it places a call).
- **FR-030**: Every error string `ir.Validate` can produce MUST be findable in
  `docs/` or `docs-site/` from the field it names. Any that are not MUST be
  listed in this feature's results file.

**Cross-cutting**

- **FR-031**: Every change to emitted behaviour, validation behaviour, the
  scaffold, or an environment name MUST update all **five** surfaces in the
  same commit, per CLAUDE.md: the emitted `build/<target>/README.md` template,
  the source example's own `README.md`, the page in `docs/`, the page in
  `docs-site/`, and the affected file under `internal/skill/assets/`.
- **FR-032**: The section of `internal/skill/assets/references/transfers.md`
  that teaches assistants to enforce the browser-transfer rule themselves MUST
  be rewritten to state that the product refuses it and to quote the refusal.
  Its claim that the control is "absent from the generated project" MUST be
  corrected: that describes the unattached-control defect, not the browser-only
  one.
- **FR-033**: The six bundle agreement tests and the bundle golden MUST pass. A
  test this feature turns red MUST be resolved by deciding which of the bundle
  or the code was wrong, and the decision recorded.
- **FR-034**: No site URL may be reintroduced into the skill bundle.
- **FR-035**: The five defects recorded in `specs/011-coding-agent-skill/tasks.md`
  MUST each be marked closed with the commit that closed them, or explicitly
  deferred with a reason.
- **FR-036**: No new dependency. Any addition MUST be justified.
- **FR-037**: Deliberate simplifications MUST be marked with a `// ponytail:`
  comment naming the ceiling.

### Key Entities

- **Declared thing**: anything an author writes into the package that is
  expected to reach the generated project — a control, a destination, a tool, a
  task, a task group, a secret name, a local Python tool, a model identifier.
  The invariant of this feature is that every declared thing either arrives or
  is named in a refusal.
- **Documentation surface**: one of the five places a fact about emitted
  behaviour must be true: the emitted README template, the example README, the
  page in `docs/`, the page in `docs-site/`, and the skill bundle under
  `internal/skill/assets/`.
- **Beginner path**: the set of files a first-time reader walks before they have
  a working agent — the site index, `docs-site/start/`, `docs-site/build/`, the
  root `README.md`, and everything `unmute init` writes. It is the surface on
  which `UNMUTE_*` must not appear.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Each of the **seven reproduced defect groups**, lettered A to G in
  [reproduction.md](./reproduction.md), has at least one test that fails on the
  commit before its fix and passes on the commit after. Reported as 7 of 7, or
  the number actually achieved. The groups are not one defect each: group A
  covers eight declaration shapes and group E covers eight generator-only value
  checks, which is why Phase 2 carries sixteen test tasks producing eleven new
  or changed test functions. Counting tests rather than groups is the honest
  measure, so both numbers are reported.
- **SC-002**: Zero packages exist that exit 0 from both `validate` and `compile`
  while a declared control, destination, tool, task, or task group is absent
  from the generated project. Measured by two adversarial agents whose only job
  is to construct one; every one they construct is a miss and must be fixed.
- **SC-003**: A person runs `unmute init`, sets exactly two environment
  variables, runs `unmute dev`, hears a spoken greeting, and completes one real
  spoken exchange after it. Heard by a person, not by a probe.
- **SC-004**: Five of five independent first-run agents reach a working greeting
  using only the CLI and the local documentation tree, with no file edited
  outside the scaffold. Each reports every point of confusion and every
  contradiction.
- **SC-005**: Eight of ten independent authoring agents, each given one of the
  same ten briefs used in PR #80 (salon, hotel, vet, gym, bank, restaurant,
  utility, dental, pizza, helpdesk), install the skill bundle and produce a
  package that validates clean **on the first attempt**. PR #80 measured 7 of 10
  against this same bar and did not re-measure after fixing the two causes; this
  is that re-measurement. The result is reported as a raw count with a per-brief
  table and a stated cause for every failure. A 7 is reported as a 7.
- **SC-006**: Grepping the beginner path and the skill bundle for `UNMUTE_`
  returns zero hits, and a test in the repository fails on the first
  reintroduction. **This already holds at baseline** — the audit measured zero
  across all six surfaces before any change — so the deliverable here is the
  test that locks it, not a cleanup. Reported as such rather than as a fix.
  The measurable change is SC-006a.
- **SC-006a**: Compiling a telephony example produces a
  `build/<target>/.env.example` containing **only** names the author supplies,
  and an emitted README whose set-these list names exactly the same set.
  Measured on `examples/livekit-human-transfer`: the file goes from twelve names
  to **eight**, losing `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET`, `LIVEKIT_URL`,
  and `REDIS_URL`, and losing the four-line comment block that explained them.
  On the Pipecat side `REDIS_URL` leaves an eight-name list, and on outbound
  routes `UNMUTE_PUBLIC_URL` and `UNMUTE_OUTBOUND_TOKEN` leave too.
  `required_env` in `compile-report.json` still names every one of them.
- **SC-006b**: Zero variables that configure a vendor's own component carry an
  `UNMUTE_` prefix. Measured as five renames: two Daily knobs and three LiveKit
  host-port mappings.
- **SC-006c**: Zero names appear in one target's env file that the equivalent
  package's other target treats differently. Today `REDIS_URL` is labelled
  "supplied for you" on LiveKit and rendered as a bare fill-in on Pipecat, from
  the same `LocallySuppliedEnvironment` data.
- **SC-006d**: A container started from a hand-written `.env` copied out of
  `build/<target>/.env.example` either runs, or refuses with a message naming
  where the missing value comes from. It never refuses naming a variable the env
  file did not mention and the message does not explain.
- **SC-007**: All eleven examples validate and compile cleanly for every target
  they declare, and every browser-only one reaches a greeting. Reported as a
  count out of eleven.
- **SC-008**: Exactly one model identifier, `gpt-5.6-luna`, appears across the
  scaffold, the examples, `docs/`, `docs-site/`, the root `README.md`, and the
  skill bundle. Zero occurrences of `gpt-4o-mini` or `gpt-4.1-mini` remain on
  any author-facing surface, zero surfaces write the combined
  `openai/gpt-5.6-luna` form, and zero write `temperature` on a think model.
  Baseline measured on the branch: 80 occurrences of the two outgoing
  identifiers across 42 tracked files, 24 of them author-facing.
- **SC-009**: `make fmt`, `make lint` at zero issues, `make build`, and
  `make test` are green on every commit.
- **SC-010**: `make smoke` is green. If it is not, the blocking cause is named
  explicitly, including which port was held and by what.
- **SC-011**: The six bundle agreement tests and the bundle golden pass, and
  `internal/skill/assets/references/transfers.md` no longer teaches a workaround
  for a fixed defect.

## Verification method

Verification is by isolated subagents, in three waves. **Every agent gets its
own scratch directory named after the agent.** PR #80's clean room gave eighteen
agents one shared scratchpad; two picked the same filename, one overwrote the
other's file, and a guard fired at an agent that had behaved correctly. That is
not repeated.

- **Wave A, reproduce before fixing.** One agent per defect, own scratch
  directory each, run in parallel. Each writes the smallest package that
  triggers its defect, records the exact observed output, and returns a failing
  test. The two silent-drop defects get separate agents, because the entire
  point is that they have different mechanisms. **No fix starts until every
  defect has a red test.**
- **Wave B, verify each fix independently.** One agent per fix, given the fix
  commit and nothing else, re-running the Wave A reproduction. **An agent that
  wrote a fix does not verify it.** One additional agent owns the six bundle
  agreement tests and the bundle golden, and reports which the changes turned
  red and whether the bundle or the code was the thing that was wrong.
- **Wave C, clean room.** Fresh agents with no repository context beyond the
  built binary and the local `docs-site/` tree — not a published site, which
  returns 404 on every deep link. Five first-run agents against SC-004, ten
  authoring agents against SC-005, and two adversarial agents against SC-002.

Raw counts are reported. A 7 is not rounded to an 8.

## Assumptions

- **The unattached-control refusal is an error, not a warning.** No finished
  package has a legitimate reason to declare a control nothing reaches. If Wave
  A finds one, the severity drops to a warning with identical wording and the
  reason is recorded in the results file.
- **The single model identifier is `gpt-5.6-luna`**, set by the author in the
  2026-08-15 clarification session and verified against OpenAI's own API
  documentation the same day. See the Clarifications section for the
  verification, the two-field authored shape, and the reasoning-effort and
  temperature decisions that follow from it being a reasoning-family model.

  This makes the brief's premise moot rather than merely wrong, but the census
  is recorded because it is what makes the change safe to sweep. The repository
  is split by **surface type**, not by code-versus-docs: every front door says
  `gpt-4.1-mini` (the scaffold, the root `README.md`, `docs-site/index.mdx`,
  `docs/SCHEMA.md` in four places, and the skill bundle's `models.md` and
  `package.md`), while all eleven examples and the deeper `docs-site/build/` and
  reference pages say `gpt-4o-mini`. The doc site contradicts itself: its
  homepage and its "your first agent" page disagree. So does one bundle file:
  `references/models.md` teaches one identifier in two blocks and prints the
  other in its sample output. Raw counts across tracked files: `gpt-4o-mini` 38
  occurrences in 28 files, `gpt-4.1-mini` 15 in 11.

  So the sweep is **both** sets, not one of them. Neither incumbent survives,
  which removes the risk of a partial migration leaving a third identifier in
  the tree. Measured on the branch: 80 occurrences across 42 tracked files, of
  which **24 files are author-facing** and are what this feature changes.
  The other 18 stay: test fixtures and goldens under `internal/testdata/` and
  `internal/generate/testdata/`, a comment in `internal/target/catalog_livekit.go`,
  `specs/011-coding-agent-skill/tasks.md` which records the drift as history,
  and this feature's own spec files, which quote both identifiers for the same
  reason. FR-012's test scopes itself to the author-facing set. It is what makes
  the next bump a one-line change.
- **The starting example is `salon-support`.** The table already marks it "Start
  here", it is the only structural example that needs no carrier and no
  third-party account, and it runs in a browser. `docs-site/build/your-first-agent.mdx`
  and the bundle's `examples.md` change to match it rather than the other way
  round.
- **The package-root `.env.example` survives, derived.** `unmute dev` reads a
  `.env` at the package root, so a new author needs something to copy there.
  The defect is that it is hand-written; deriving it from the same source as the
  generated one removes the defect without removing the file. Deleting it stays
  an acceptable outcome if the plan finds nothing reads it.
- **`validate` reach versus `compile` reach is about providers, not depth.**
  Constitution Principle V says `validate` "is the only command whose reach is
  all four providers" and that generation "reaches only the providers with a
  shipped driver". Neither sentence licenses a generator-only value check, so
  FR-007 moves the check without amending the constitution.
- **Existing packages keep compiling.** Only the secrets cross-check changes
  severity-free; every new refusal in FR-001 to FR-003 and FR-007 rejects a
  package that was already broken, so no working package starts failing.
- **This feature lands rebased on PR #78, PR #79, and PR #80**, all merged. The
  Pipecat container fix regenerates every Pipecat golden, which PR #79 forbade
  inside its own scope; landing after it removes the conflict.
- **`make smoke` needs Python and is opt-in.** It has never passed in the skill
  worktree, blocked by a port held by a stale container from a concurrent
  session rather than by any branch. Stale `unmute dev` containers are ephemeral
  and recreated on the next run.

## Out of scope

- The `secrets:` block listing a name the package does **not** write. Only the
  missing-name direction, the one N24 documents, is in scope.
- Tracking provider model retirements. One home and one agreement test is the
  whole mechanism.
- Any new orchestrator target. Pipecat and LiveKit remain the only two with
  drivers.
- Renaming an `UNMUTE_*` name whose rename is not cheap. It stays, with the
  reason recorded; only its appearance on the beginner path is in scope.

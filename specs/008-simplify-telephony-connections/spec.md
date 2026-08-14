# Feature Specification: The connection owns the phone route

**Feature Branch**: `008-simplify-telephony-connections`

**Created**: 2026-08-13

**Status**: Draft

**Input**: User description: "Simplify the telephony transport/connection authoring contract. Today `examples/twilio-telephony-hello` declares `transport` + `carrier` + `connection` on each target, plus two connection files under `connections/`. Move all mechanism detail (transport, carrier, env vars) into the connection YAML so targets only reference a connection by name. WebRTC testing must keep working even when telephony is enabled. Validate strictly and update `docs/` and `docs/user/`."

## The problem, in the author's words

Opening `examples/twilio-telephony-hello` today, the phone route is spread across
two places and reads as three unrelated fields:

```yaml
# targets.yaml
targets:
  livekit:
    provider: livekit
    transport: sip        # mechanism
    carrier: twilio       # mechanism
    connection: twilio_sip  # pointer to more mechanism
```

```yaml
# connections/twilio_sip.yaml
kind: telephony
environment:
  sip_address: SIP_TRUNK_HOSTNAME
  ...
```

Three problems follow from the split:

1. **Nothing tells the reader these three lines are one decision.** `transport`,
   `carrier`, and `connection` sit beside `provider`, `version`, and
   `deployment_region` as if they were peers. They are not: the first three are
   one route, chosen once.
2. **The connection file cannot be read on its own.** `connections/twilio_sip.yaml`
   names four SIP environment variables and never says it is a SIP trunk, so the
   only way to know what those names are for is to find the target that points at
   it.
3. **The same fact is checked in two files.** The environment keys a connection may
   declare depend entirely on the route the *target* selected, so the compiler
   validates one file against another, and the author gets an error about a file
   they were not editing.

The fix the author wants: **a target names one connection, and the connection is
the whole route.**

```yaml
# targets.yaml
targets:
  livekit:
    provider: livekit
    version: "1.6.4"
    sdk_language: python
    connection: twilio_sip
    deployment_region: eu-central
```

```yaml
# connections/twilio_sip.yaml
transport: sip
carrier: twilio
environment:
  sip_address: SIP_TRUNK_HOSTNAME
  sip_username: SIP_AUTH_USERNAME
  sip_password: SIP_AUTH_PASSWORD
  from_number: SIP_FROM_NUMBER
```

## Clarifications

### Session 2026-08-13

- Q: A `connections/*.yaml` file that no target names — error, warning, or ignored? → A: Warning on stderr, exit 0
- Q: Browser dev on a package that declares only `channels.phone` — should it work? → A: Yes, no `channels.web` needed
- Q: Should the resolved/compiled output keep `transport` and `carrier` on the target, or mirror the new authoring shape? → A: Keep them on the resolved target
- Q: What should `outbound-reminder`'s two connection files be called, and does the spec mandate a naming rule? → A: `twilio_websocket.yaml` and `twilio_connector.yaml`; no naming rule

### Session 2026-08-13 (transfers, credentials, and numbers)

- Q: Transfer destinations (the numbers dialed for a handoff) — stay on the target, or move into the connection? → A: Move into `agent.yaml`
- Q: In `agent.yaml`, should destinations accept literal numbers, or only environment variable names? → A: Environment variable names only
- Q: Carrier credentials and destination numbers — also listed in `secrets:`, or declared only in their own files? → A: Require them in `secrets:` too
- Q: A connection or destination variable missing from `secrets:` — fail, or warn and exit 0? → A: Warn, matching the existing rule
- Q: Where should the end-to-end telephony walkthrough live? → A: Grow `docs/user/learn/07-phone-calls.md` into it

### Session 2026-08-14

- Q: Which environment variable names must the docs and example README account for? → A: Every name in the generated `.env.example`, including the ones the reader must not set
- Q: Which documents must account for every environment variable name? → A: The example's own README and `docs/user/learn/07-phone-calls.md`; `docs/TRANSFERS.md` keeps its existing check unchanged
- Q: Should the every-variable-documented rule cover all examples, or only the telephony ones? → A: The five telephony examples only (an initial "all examples" answer was withdrawn once it emerged that four non-telephony examples ship no README at all)
- Q: Should a variable the reader never sets be mentioned at all? → A: No. What the docs cover equals `.env.example`: nothing missing, nothing extra. The "nothing missing" half is checked; the "nothing extra" half is an editorial rule

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Read a package's phone route from one file (Priority: P1)

An author opens a package they did not write and wants to answer one question:
how does a phone call reach this agent? They open `targets.yaml`, see
`connection: twilio_sip`, open `connections/twilio_sip.yaml`, and the file says
`transport: sip`, `carrier: twilio`, and the four environment names the trunk
needs. Two files, one hop, no cross-referencing.

**Why this priority**: This is the whole feature. Every other story is a
consequence of it. The route becomes a named thing an author can point at,
compare, copy, and delete.

**Independent Test**: Take `examples/twilio-telephony-hello`, rewrite both
targets and both connection files in the new shape, and confirm `unmute compile`
produces the same route, the same required environment, and the same emitted
files as before. Nothing about what the package *does* may change.

**Acceptance Scenarios**:

1. **Given** a package whose target names a connection and nothing else about the
   route, **When** the author runs `unmute validate`, **Then** it resolves the
   `(provider, transport, carrier)` route from the target's `provider` and the
   connection's `transport` and `carrier`, and reports the same route it reports
   today.
2. **Given** a target that still declares `transport:` or `carrier:` directly,
   **When** the author runs any command that loads the package, **Then** it fails
   with one message naming the file, the offending key, and the connection file
   the two values belong in.
3. **Given** a connection file, **When** the author reads it alone, **Then** it
   names its transport, its carrier, and its environment names, with no reference
   to any target.
4. **Given** two targets in one package on two different carriers, **When** the
   author compiles, **Then** each target names its own connection and each writes
   its own `build/<target>/`, exactly as today.

---

### User Story 2 - Test in the browser with no phone credentials (Priority: P1)

An author is building a phone agent but is iterating on its prompt, its tools,
and its handoffs. They want a browser session, not a phone call. They run
`unmute dev <package>` with no carrier credentials in their environment at all,
and get the WebRTC console. The phone route is declared and idle.

**Why this priority**: This is the common case, not the fallback. Most sessions on
a phone-enabled package are browser sessions, and a phone route that made the
default path require Twilio credentials would tax every one of them. It is P1
alongside story 1 because moving route facts around is exactly the change that
could break it by accident.

**Independent Test**: Unset every carrier environment variable, run
`unmute dev examples/twilio-telephony-hello`, and hold a conversation in the
browser.

**Acceptance Scenarios**:

1. **Given** a package with a telephony channel and a declared connection, and an
   environment with none of the connection's variables set, **When** the author
   runs `unmute dev <package>`, **Then** the browser session starts and the
   missing carrier variables are neither required nor reported as an error.
1a. **Given** a package that declares `channels.phone` and no `channels.web`,
   **When** the author runs `unmute dev <package>`, **Then** the browser session
   starts, because the browser path is a development tool rather than a declared
   production surface.
2. **Given** the same package, **When** the author runs `unmute dev <package>
   --telephony`, **Then** the missing carrier variables are reported by name
   before anything starts, because that run does need them.
3. **Given** a package with both a `web` channel and a `phone` channel, **When**
   the author reads the example's `README.md` and the `docs/user` phone pages,
   **Then** each says plainly that the browser is the default way to test and the
   phone route is opt-in.

---

### User Story 3 - Get told exactly what is wrong with a route (Priority: P2)

An author writes a connection by hand and gets one thing wrong: a carrier the
transport does not serve, an environment key from a different route, a variable
name that is not a legal shell identifier. Each mistake produces one message that
names the file, the key, and the accepted values.

**Why this priority**: The new shape puts every route fact in one file, which is
what makes a precise message possible. Without it the simplification would just
move the confusion.

**Independent Test**: Write one broken connection file per case below and confirm
each fails with a message naming the file, the key, and the fix.

**Acceptance Scenarios**:

1. **Given** a connection declaring a `(transport, carrier)` pair no route
   supports, **When** the author validates, **Then** the failure names the pair,
   the target's provider, and the routes that provider does support.
2. **Given** a connection declaring an environment key the route does not accept,
   **When** the author validates, **Then** the failure names the key and lists the
   keys the route accepts.
3. **Given** a connection whose environment value is not a legal shell identifier
   (`11LABS_API_KEY`), **When** the author validates, **Then** it fails at compile
   with that name quoted, rather than exporting an unusable name into a deployed
   secret set.
4. **Given** a connection file that no target names, **When** the author
   validates, **Then** a warning naming the file goes to stderr and the command
   still succeeds, because a leftover route file makes nothing wrong.
5. **Given** an unknown key in a connection file, **When** the author validates,
   **Then** it fails naming the key and the line.

---

### User Story 4 - Scaffold and console produce the new shape (Priority: P3)

An author who reaches telephony through `unmute init` or the interactive console
gets the new shape written for them: a connection file that names its route, and a
target that names the connection.

**Why this priority**: The generators are the second-largest source of packages
after the examples. Leaving them on the old shape would ship a tool that writes
files its own validator rejects.

**Independent Test**: Run the scaffold for each telephony route it offers and
confirm the output loads and validates.

**Acceptance Scenarios**:

1. **Given** the interactive console's phone questions, **When** the author picks
   a route, **Then** they pick one route (a carrier and a mechanism together), not
   a transport and a carrier as two unrelated free-text fields.
2. **Given** a scaffolded telephony package, **When** the author runs
   `unmute validate` on it unchanged, **Then** it passes.

---

### User Story 5 - Know where every phone value goes (Priority: P2)

An author wiring up a transfer asks one question: where do the credentials go, and
where does the number I dial go? One table answers it. Carrier credentials go in
the connection. Dialled numbers go in `agent.yaml` under `destinations`, as
environment variable names. Every one of those names is repeated in `secrets:`,
which is the list of what this package needs to run. The route's own variables are
supplied by the driver and never written by hand.

**Why this priority**: Getting this wrong is not a compile error today — it is a
call that connects and then fails. The facts exist but are spread over four files
and no page states them together, which is why the question gets asked.

**Independent Test**: Hand the phone-calls page to somebody who has not written a
phone package and ask them to place every value for a warm transfer without
opening another page.

**Acceptance Scenarios**:

1. **Given** a package with a warm transfer whose target names a connection with
   `transport: cloud-websocket`, **When** the author validates, **Then** the
   failure names the connection, the transport it declares, and the transport a
   warm transfer needs, rather than saying only that a connection is missing.
2. **Given** a connection or destination variable that is not listed in
   `secrets:`, **When** the author validates, **Then** a warning names it and the
   command exits 0, the same as every other undeclared environment name.
3. **Given** a destination written as a literal number in `agent.yaml`, **When**
   the author validates, **Then** it fails, quoting the value and telling the
   author to name a variable holding it.
4. **Given** any of the five telephony examples, **When** a reader opens its
   `agent.yaml`, **Then** `secrets:` lists every environment name that package's
   author wrote, and none of the driver-supplied ones.
5. **Given** any of the five telephony examples, **When** its README and the
   phone-calls page are checked against its generated `.env.example`, **Then**
   every name in that file appears in both, each with what it is and where to get
   it, and no variable the reader never sets is named anywhere in the credentials
   section.
6. **Given** a route that gains a new required environment variable, **When** the
   suite runs, **Then** it fails naming the variable and the pages that do not
   mention it, rather than passing and surfacing on a live call.

---

### Edge Cases

- **Same credentials, two routes.** `examples/outbound-reminder` names one
  connection (`twilio_voice`) from two targets whose transports differ
  (`carrier-websocket` on Pipecat, `connector` on LiveKit). Once a connection
  names its own transport, one file cannot serve both, so that package needs two
  connection files holding the same three environment names:
  `twilio_websocket.yaml` and `twilio_connector.yaml`. The duplication is the
  honest cost of a self-describing file, and each file is six lines.
- **A route that needs no credentials.** A receive-only package on
  `(pipecat, cloud-websocket, twilio)` needs no carrier credentials at all
  (SCHEMA N38). Its connection still exists, because that is where the route is
  declared, and it declares no `environment`. The requiredness of the three
  Twilio keys stays conditional on whether the package places or redirects calls.
- **A route with no carrier.** `transport: daily-sip` with no carrier means a
  Daily-provisioned number, which today is declared on the target with no
  connection and no phone channel (`examples/pipecat-human-transfer-daily`). It
  gets a one-line connection file so the rule has no exceptions, and that file is
  the smallest connection the schema allows: a transport and nothing else.
- **A telephony channel and no connection.** Still an error, still naming the
  connection the target has to add.
- **A connection nothing uses.** Still an error, but the test widens: a connection
  is used by a telephony channel *or* by a control that dials a human. The Daily
  cold-transfer package is the second case, and it has no phone channel at all.
- **Two connections declaring the same route.** Allowed. They are two accounts, or
  one account reached two ways, and nothing about the compile depends on their
  being distinct.
- **A `deployment_region` that disagrees with nothing.** Region stays on the
  target. It is where the platform runs the agent, not how a call arrives.
- **Two targets that should escalate differently.** No longer expressible. The
  per-target destination override is removed with FR-004a and nothing replaces it.
  No shipped example uses one, and the SCHEMA amendment records it as a removed
  capability so a future package that needs it finds the reason rather than a gap.
- **A cold transfer on a route with no credentials.** `pipecat-human-transfer-daily`
  dials its destination through Daily's own dial-out, so its connection declares a
  transport and no environment at all, while its destination still needs a
  variable in `secrets:`. Credentials and dialled numbers are separate questions,
  and this package answers them differently.
- **A target for a provider with no driver.** A `vapi` or `deepgram` target has no
  route, no connection, and no transport vocabulary, and today its `carrier` gates
  four control capabilities directly. It loses `carrier` like every other target,
  and those four rows lose the condition (FR-001a). No shipped example has such a
  target; `internal/testdata/safe_core` does, which is why this was invisible from
  `examples/` alone and why the fixture migration under FR-008d is where it gets
  caught.
- **A `pipecat` target with a transport, no carrier, and no phone channel.**
  `internal/testdata/safe_core` has exactly this: `transport: daily-sip` present
  only so Pipecat cold transfer resolves. It gains a one-line connection file
  under FR-009 and keeps producing no telephony plan, because it declares no
  telephony channel.
- **A driver-supplied variable an author tries to declare.** Listing `REDIS_URL`
  or `LIVEKIT_URL` in `secrets:` is not an error — `secrets:` has always accepted
  any name — but FR-005c means it is never required and FR-005f means the docs do
  not mention it at all. A variable the reader never sets earns no line, not even
  one telling them to ignore it.
- **A variable that is driver-supplied locally and operator-supplied in
  production.** `REDIS_URL` on the carrier-websocket and connector routes is
  injected by `unmute dev` for a local run and filled in by the operator for a
  deploy, so it reaches `.env.example` and is documented. On the cloud-websocket
  and SIP routes no route reads it, it never reaches `.env.example`, and it is not
  mentioned. Membership of `.env.example` is the whole test, and it is per-route.
- **A required variable nobody writes.** `DAILY_API_KEY` is exempt from `secrets:`
  because no author writes it, and still required at runtime. Exempt from the
  declaration and *not* exempt from the documentation is the whole point of
  FR-005f: the two rules answer different questions.
- **A non-telephony example.** Out of scope for FR-005f by FR-005f0, and four of
  them have no README to check anyway. `salon-support` would pass today.

## Requirements *(mandatory)*

### Functional Requirements

**The authoring shape**

- **FR-001**: A target MUST declare its phone route by naming exactly one
  connection, and MUST NOT declare `transport` or `carrier`. The connection name
  is the only route field on a target. This holds for **every** provider,
  including the two that ship no driver.
- **FR-001a**: Four capability rows condition on `carrier` for providers that have
  no route and no connection to put one in, and MUST lose that condition, because
  after FR-001 no author can satisfy it: Deepgram cold transfer, Vapi warm
  transfer, Deepgram warm transfer, and Deepgram voicemail detection
  (`internal/target/table.go:411,420,421,427`). The Twilio requirement each one
  records is not deleted — it moves into a comment on the row, addressed to
  whoever builds that driver. A condition an author cannot meet is a refusal that
  names an impossible fix, which is worse than no condition (Principle II).
- **FR-001b**: Only one of those four is reachable today, and it is the reason
  this matters: with `carrier` removed and the rows unchanged,
  `internal/testdata/safe_core` fails with `Deepgram transfer requires carrier
  Twilio in the generated bridge`. There is no generated bridge — Deepgram ships
  no driver — so the note describes an implementation that does not exist. The
  other three rows are unreachable in any shipped package and become permanently
  unsatisfiable rather than merely unused.
- **FR-001c**: This widens what `unmute validate` reports as supported on `vapi`
  and `deepgram`, and that MUST be stated in the amendment under FR-021 rather
  than left as a silent side effect. The alternative — keeping `carrier` on
  driverless targets — was specified and then withdrawn on 2026-08-14: it bought
  one gated error on one provider nobody compiles, at the price of `carrier`
  meaning two different things and the headline rule gaining an exception.
  `routedControls` (`dtmf_send`, `dtmf_receive`, `hold`, `ivr_navigation`)
  conditions on `carrier` **and** `transport` for all four providers and needs no
  change: the transport half already gates it on driverless targets today, so
  removing `carrier` changes nothing there.
- **FR-002**: A connection file MUST declare its own `transport` and, when the
  route has one, its `carrier`, alongside the `environment` map it already
  declares. A connection file MUST be readable on its own: no route fact may live
  only in a target.
- **FR-003**: The route the compiler resolves MUST remain the triple
  `(provider, transport, carrier)`, with `provider` read from the target and the
  other two from the named connection. No route in the shipped catalog may gain or
  lose a capability from this change.
- **FR-003a**: Only the authoring surface changes. The resolved surface — the
  compiled plan, `compile-report.json`, and `validate --json` — MUST keep
  `transport` and `carrier` on the resolved target, because working the route out
  is what the compiler did. The two schemas disagree on purpose: one is what an
  author writes, the other is what was resolved. No golden file may change shape
  for this reason, and no existing reader of the report may break. This is the
  load-bearing decision of the whole design, so it MUST be asserted directly:
  `internal/ir/testdata/golden/compiler.txt` stays byte-identical through the
  migration, which only proves anything once the fixtures under FR-008d are
  migrated to the new shape.
- **FR-004**: `provider`, `version`, `pins`, `sdk_language`, `deployment_region`,
  and `models` MUST stay on the target. They describe the orchestrator and the
  deployment, not how a call arrives.
- **FR-004a**: `destinations` MUST move from the target to the top level of
  `agent.yaml`. A destination is who this agent escalates to, and the billing desk
  is the same desk whichever carrier reaches it, so it is an agent fact rather
  than a per-target one. After the move a target declares no phone number of any
  kind: the number a call comes *from* is the connection's `from_number`, and the
  numbers a call goes *to* are the agent's `destinations`.
- **FR-004b**: The per-target destination override is removed with the move, and
  nothing replaces it. No shipped example uses one — all three packages with
  destinations declare a single target — but this is a real capability being
  dropped, not an unused field being tidied, and the SCHEMA amendment under
  FR-021 MUST record it as such. A package that needs two targets to escalate
  differently has no way to express it after this change.
- **FR-004c**: A `human_transfer` control MUST resolve its symbolic destination
  name against the agent-level map. The model still only ever sees the symbolic
  name, never a number, exactly as today.
- **FR-004d**: A destination value in `agent.yaml` MUST be the `UPPER_SNAKE` name
  of an environment variable, and nothing else. The literal E.164 and `sip:` URI
  forms that the target accepted are rejected, because `agent.yaml` is the
  portable half of a package and a literal number is a deployment fact — the same
  rule that put credentials in `connections/`. A literal MUST fail with a message
  that quotes the value and tells the author to name a variable holding it. All
  three examples with destinations already use the name form, so none of them
  changes on this axis.
- **FR-005**: The one-connection-per-target rule MUST hold unchanged: a target
  never combines carriers, and each target writes its own `build/<target>/`.
- **FR-006**: The `kind:` field MUST be removed from the connection file. Once a
  connection declares `transport: sip`, `kind: telephony` says nothing new: every
  transport value in the catalog is telephony. A `kind:` key MUST fail as an
  unknown key under FR-010, with a message saying it is no longer written. The
  field comes back the day a second connection kind exists, not before.

**Existing packages**

- **FR-007**: A target declaring `transport`, `carrier`, or `destinations` MUST
  fail to load, with one message naming the file, the line, the key, its value,
  and where the value belongs now — the connection file for the first two,
  `agent.yaml` for the third. The rule is keyed on the field, not on the provider. There is no compatibility path and no deprecation window: the
  repository is pre-release with no external packages, so one shape is loaded, one
  shape is tested, and one shape is documented.
- **FR-008**: All five shipped examples that declare a phone route MUST be
  rewritten in the new shape in the same change, and each MUST keep its current
  route, required environment, and emitted files. No example may be left on the
  old shape, and none may be deleted or merged to avoid the work:

  | Example | Targets and routes today | After |
  |---|---|---|
  | `twilio-telephony-hello` | `pipecat` cloud-websocket/twilio → `twilio_voice`; `livekit` sip/twilio → `twilio_sip` | both targets name a connection only; both connection files gain `transport` and `carrier`, both lose `kind` |
  | `livekit-human-transfer` | `livekit` sip/twilio → `twilio_sip` | one connection line; `twilio_sip.yaml` gains `transport: sip`, `carrier: twilio` |
  | `pipecat-human-transfer-twilio` | `pipecat` cloud-websocket/twilio → `twilio_voice` | one connection line; `twilio_voice.yaml` gains `transport: cloud-websocket`, `carrier: twilio` |
  | `outbound-reminder` | `pipecat` carrier-websocket/twilio and `livekit` connector/twilio, **both naming one `twilio_voice`** | the shared file splits into `twilio_websocket.yaml` and `twilio_connector.yaml`, same three environment names in each, one named per target |
  | `pipecat-human-transfer-daily` | `pipecat` daily-sip, **no connection, no `connections/` directory** | gains `connections/daily.yaml` holding `transport: daily-sip` and nothing else; the target names it |

- **FR-008a**: No connection-file naming convention is mandated. The file declares
  its own `transport` on its first line, so a rule would be documentation to
  maintain for a fact the file already states. The names in FR-008 are chosen for
  the examples, and `docs/` teaches the pattern by showing them rather than by
  stating a rule.
- **FR-008b**: Each example's own `README.md` MUST be updated in the same commit
  as its `targets.yaml`, per CLAUDE.md. Four of the five READMEs quote a
  `transport` value in prose or in a table, and
  `pipecat-human-transfer-daily/README.md` has no `connections/` section at all
  and needs one.
- **FR-008c**: The same five examples MUST also change in `agent.yaml`, which the
  FR-008 table does not cover:

  | Example | `destinations` move | `secrets:` |
  |---|---|---|
  | `livekit-human-transfer` | two, target → agent | **has no block**; gains one with 4 SIP names + 2 destination names |
  | `pipecat-human-transfer-twilio` | one, target → agent | **has no block**; gains one with 3 Twilio names + 1 destination name |
  | `pipecat-human-transfer-daily` | one, target → agent | **has no block**; gains one with its destination name (its connection declares no environment at all) |
  | `twilio-telephony-hello` | none | existing block grows by the 3 Twilio and 4 SIP names |
  | `outbound-reminder` | none | existing block grows by the 3 Twilio names |

**Every route lives in exactly one connection file**

- **FR-008d**: The **fixture packages** under `internal/testdata/` MUST be migrated
  in the same change as the examples. `safe_core` and `daily_carrier` both declare
  the old shape, and `safe_core` additionally exercises two things no example does:
  a `carrier` on providers with no route, which is the only thing exercising the
  four capability rows FR-001a changes, and four targets whose
  `destinations` maps are identical, which is what lets FR-004a's merge into one
  agent-level map be lossless. Leaving them behind turns the whole suite red with
  nothing pointing at the cause.
- **FR-008e**: `internal/testdata/safe_core` declares its destination as the
  literal `"+14155550123"` on four targets, which FR-004d rejects. It MUST move to
  an environment-variable name. Because this fixture backs the safe-core claim in
  `docs/SCHEMA.md` §7 and `docs/user/reference/safe-core.md`, both MUST be checked
  for any statement about literal destinations that the narrowing makes false.
- **FR-009**: The two shapes that declare a route with no connection today MUST
  each get one, so that a target's route is always exactly one connection
  reference with no exceptions:
  - a receive-only `(pipecat, cloud-websocket, twilio)` package gets a connection
    declaring `transport` and `carrier` and no `environment`;
  - a Daily-provisioned target gets a connection declaring `transport: daily-sip`
    and nothing else — no carrier, no environment.
- **FR-009a**: A connection MUST therefore be legal with an empty or absent
  `environment`, and MUST be legal on a package with no telephony channel, because
  the Daily-provisioned route is used to dial a human during a cold transfer
  rather than to receive calls.

**One list of what the runtime needs**

- **FR-005a**: The `secrets:` exemption for telephony names is removed. Every
  environment variable name the **package author writes** MUST appear in
  `agent.yaml`'s `secrets:` list: the values of a connection's `environment:` map,
  the values of `destinations:`, and the model and tool credentials already listed
  there. One block then answers "what does this package need to run".
- **FR-005b**: The name stays declared in its own file as well. A connection keeps
  saying `account_sid: TWILIO_ACCOUNT_SID`, because that line says which *role*
  the variable plays on the route, which `secrets:` cannot express. The two are
  not redundant: one names the role, the other declares the requirement.
- **FR-005c**: Variables the author does not write MUST NOT be required in
  `secrets:`. The route's own runtime environment is supplied by the driver, the
  platform, or `unmute dev` — `REDIS_URL`, `LIVEKIT_URL`, the LiveKit key pair,
  `UNMUTE_PUBLIC_URL`, `UNMUTE_OUTBOUND_TOKEN`, `PIPECAT_CLOUD_ORGANIZATION` —
  and demanding them in a hand-written list would make every phone package carry
  boilerplate it did not choose. This boundary MUST be held by a test: nothing may
  start requiring `REDIS_URL` or `LIVEKIT_URL` in `secrets:` without a failure.
- **FR-005d**: Two of the five telephony examples (`livekit-human-transfer`,
  `pipecat-human-transfer-twilio`) have **no `secrets:` block at all** today and
  MUST gain one. The other three MUST grow their existing block to cover their
  connection and destination names.
- **FR-005f**: What the docs say about environment variables MUST equal the
  generated `.env.example`: nothing missing, and nothing extra. That file is the
  set of names the operator fills in for a deploy, so it is both the source of
  truth and the boundary.
  - **Nothing missing**: every name in `.env.example` is documented with what it
    is and where to get it. This half is checked (FR-027a).
  - **Nothing extra**: a variable the reader never sets is not mentioned. Naming
    one only to say "you do not set this" gives a reader something to worry about
    and nothing to do with it. This half is an editorial rule, not a check,
    because a check strict enough to catch it would also fail deliberate teaching
    lines such as `twilio-telephony-hello`'s example of `11LABS_API_KEY` as a name
    that cannot be exported.
- **FR-005f0**: The rule covers the **five telephony examples only**. The check is
  written generically, so pointing it at the rest of `examples/` is a one-line
  change, but four non-telephony examples (`multi-task`, `subagents`,
  `task-groups`, `simple-prompt`) ship no `README.md` at all, and writing four
  pages about tasks and subagents does not belong in a telephony feature.
  `salon-support` is the only non-telephony example with a README and it already
  documents its five variables correctly, so widening the rule later starts from a
  passing baseline.
- **FR-005f1**: Two surfaces carry FR-005f, and the checked half covers both: the
  example's own `README.md`, and `docs/user/learn/07-phone-calls.md` for the
  routes it teaches. They are the two pages a reader actually opens before the generated
  runbook. `docs/TRANSFERS.md` keeps its existing check over its two examples
  unchanged — transfers are its subject, it already passes, and a route with no
  transfers has no business being listed there.
- **FR-005g**: `DAILY_API_KEY` is the case that requires FR-005f to be scoped this
  way. It is required at runtime, it is exempt from `secrets:` under FR-005c
  because the author never writes it, and a rule scoped to author-written names
  would therefore miss it entirely.
  `examples/pipecat-human-transfer-daily/README.md` already documents it in a
  table with what it is for, and that table is the model the other four follow.
- **FR-005h**: The "nothing extra" rule has one known casualty to remove now:
  `examples/twilio-telephony-hello/README.md` currently names
  `UNMUTE_OUTBOUND_TOKEN`, `UNMUTE_PUBLIC_URL`, and `REDIS_URL` to say the reader
  does not set them. Neither route in that package reads any of them and none of
  them reaches its `.env.example`, so the paragraph goes. Any equivalent
  paragraph found in the other four goes with it.
- **FR-005i**: Writing one environment **name** in two files (FR-005b) is a
  deliberate departure from Principle III, which requires one home per fact or an
  agreement test that **fails** on drift. The agreement here is the existing
  cross-check, which warns. Deriving `secrets:` from every site was possible and
  was rejected. That departure MUST therefore be recorded in `docs/SCHEMA.md` as a
  dated, numbered exception **stating the cost**, in the same shape as the tool
  `output` exception (N22). A deviation recorded only in a feature's plan is a
  deviation the next reader never finds.
- **FR-005e**: A name written in a connection or a destination and missing from
  `secrets:` MUST warn on stderr and exit 0. It joins the cross-check that already
  exists for every other env name, at the severity that check already has, so no
  env name behaves differently from any other. The consequence is accepted and
  MUST be stated in the docs: a package missing a name compiles green and fails on
  its first phone call. Raising that check to an error, for every name rather than
  telephony names alone, is a separate decision and out of scope here.

**Strict validation**

- **FR-010**: An unknown key in a connection file MUST fail with the file, the
  key, and the line.
- **FR-011**: A `(transport, carrier)` pair that no route supports for the
  target's provider MUST fail, naming the pair and the routes that provider does
  support.
- **FR-011a**: The list of supported routes in that message, and the route list
  the interactive console offers, MUST contain only routes an author can actually
  select. The catalog holds placeholder rows that declare required environment and
  no capabilities at all (the Exotel entries); naming one in a "did you mean"
  list sends the author to a second, different refusal.
- **FR-012**: A missing required environment key, or a key the route does not
  accept, MUST fail naming the key and listing the keys the route accepts. Where
  requiredness depends on what the package does (placing or redirecting calls on
  the cloud-websocket route), the message MUST say which behaviour makes the key
  required.
- **FR-013**: An environment *value* that is not a legal shell identifier
  (letters, digits, underscores, not starting with a digit) MUST fail at compile,
  naming the value. A name that cannot be exported by a deployment platform is a
  runtime hole a compile can close.
- **FR-014**: A target naming a connection that does not exist MUST fail, naming
  the connection and listing the connections the package defines.
- **FR-015**: A connection file that no target names MUST print a warning to
  stderr naming the file, and MUST exit 0. It is the one report here that is not
  a failure: the build is correct and the file is merely dead, and blocking it
  would stop an author who writes the connection before the target that uses it.
  The check runs across every target the package declares, never only the ones
  `--target` selected, so which target is being compiled cannot change the
  warning.
- **FR-016**: A package with a telephony channel whose target names no connection
  MUST fail, naming the connection field and what the route needs. In the other
  direction, a target naming a connection that nothing in the package uses MUST
  fail: a connection is used by a telephony channel, or by a control that dials a
  human. The Daily-provisioned route is the second case, and the message MUST say
  which of the two is missing rather than only that the connection is unused.
- **FR-016a**: The refusal an author sees when a transfer does not compile on
  their route MUST name the connection. A warm transfer dials the destination
  itself, so it needs a connection whose `transport` is `sip` and whose four SIP
  values are present; today the message can only say "warm transfer needs a
  telephony Connection", because the transport it needs was written on a different
  file. After this change the message MUST name the connection the target
  declares, the transport that connection declares, and the transport a warm
  transfer requires. The same applies to cold transfer on a route that does not
  emit it.
- **FR-017**: Every check above except FR-015 MUST be a failure, never a warning
  that downgrades to a partial compile. FR-015 is a warning precisely because
  nothing is downgraded: the compiled output is complete and correct. FR-005e is
  the second warning and is governed by the existing schema rule rather than by
  this list.

**Browser testing stays first-class**

- **FR-018**: `unmute dev <package>` without `--telephony` MUST start a browser
  session on a package with a declared phone route, and MUST NOT require or report
  any of the connection's environment variables.
- **FR-018a**: The browser session MUST NOT require a `channels.web` entry. It is
  a development tool, not a declared production surface, so a package that
  declares only `channels.phone` is testable in a browser the moment it is
  written. `channels.web` keeps its existing meaning: this agent serves browser
  callers in production. Two of the five telephony examples
  (`livekit-human-transfer`, `pipecat-human-transfer-twilio`) declare only a phone
  channel today, and neither may gain a `web` channel to satisfy this feature.
- **FR-019**: `unmute dev <package> --telephony` MUST report every missing
  variable the resolved route needs, by name, before starting anything.
- **FR-020**: The distinction in FR-018 and FR-019 MUST be stated in the example
  README, the emitted `build/<target>/README.md`, and the `docs/user` phone pages,
  so an author does not learn it from a failure.

**Documentation**

- **FR-021**: `docs/SCHEMA.md` MUST carry a numbered, dated amendment stating the
  new shape. It supersedes four things: the §6.1 clauses that put `transport` and
  `carrier` on the target, the §6.1 `destinations` row (moved to §4 and narrowed
  to environment names only, FR-004a and FR-004d), the §6.3 connection example
  (which still shows `kind:` and no route), and the §4.12 sentence exempting
  connection names from `secrets:` (FR-005a). The amendment MUST state the dropped
  per-target destination override as a removed capability, not as tidying.
- **FR-022**: `docs/user/reference/targets-yaml.md` MUST describe the target field
  set without `transport`, `carrier`, or `destinations`, and MUST point at the
  connection reference for the route and at `agent.yaml` for the destinations.
- **FR-023**: `docs/user/reference/` MUST gain a connections page that is the one
  place a reader learns the connection file shape, the supported routes, the
  environment keys each route takes, and which keys are conditional on placing
  calls. Today no such page exists and the facts are scattered across
  `docs/SCHEMA.md`, `docs/TELEPHONY.md`, and example READMEs.
- **FR-023a**: That page MUST state, per route, which transfer shapes compile —
  cold, warm, or neither — and what each needs from the connection. A warm
  transfer dials the destination itself, so it compiles only where the connection
  is a SIP one; a reader choosing a route needs that before they write the file,
  not after the refusal.
- **FR-023b**: The docs MUST answer "where does each phone value go" in one table:
  carrier credentials in the connection, dialled numbers in `agent.yaml`'s
  `destinations`, every one of those names repeated in `secrets:`, and the route's
  own runtime variables supplied by the driver and never written by hand. This
  question is what sent a reader looking through four files, and no page answers
  it today.
- **FR-024**: `docs/TELEPHONY.md`, `docs/user/learn/07-phone-calls.md`, and
  `docs/user/learn/twilio-walkthrough.md` MUST show the new shape everywhere they
  show a route.
- **FR-024a**: `docs/user/learn/07-phone-calls.md` MUST become the end-to-end path
  a reader follows: pick a route, write the connection, declare the names in
  `secrets:`, set the destinations, learn which transfers the route supports, and
  test in a browser before testing on a phone. It links out to the connections
  reference (FR-023) for field detail rather than repeating it. No sixth page is
  added: the facts this feature is untangling are spread across too many pages
  already, and the fix is fewer places, not one more.
- **FR-024b**: `docs/user/reference/agent-yaml.md` MUST document `destinations` in
  its new home, and `docs/user/reference/secrets.md` MUST state that connection
  and destination names now belong in `secrets:` and that a missing one warns
  rather than fails.
- **FR-025**: The emitted `build/<target>/README.md` template MUST match the
  example pages updated under FR-008b and the `docs/` pages updated under FR-021
  to FR-024. Per CLAUDE.md three places document a change, not one: the emitted
  template, the example page, and `docs/`. A fact that is only true in generated
  output is a fact the reader never sees.
- **FR-026**: The scaffold (`unmute init`) and the interactive console MUST write
  the new shape, and MUST offer the route as one choice rather than two free-text
  fields.
- **FR-027a**: The "nothing missing" half of FR-005f MUST be held by a check, not
  by memory. One already exists in the same shape — it reads a generated
  `.env.example` and asserts every name appears in `docs/TRANSFERS.md`, for two
  examples. The new check reuses that shape over the five telephony examples and
  the two surfaces in FR-005f1, so a route that grows a required variable fails in
  CI rather than on somebody's live rig. It is deliberately one-way: it never
  fails on a name the page mentions and `.env.example` does not, because prose has
  legitimate reasons to name a variable it is not documenting.
- **FR-027**: The existing example-agreement check MUST move with the field. It
  asserts today that every example README names every `transport` its **targets**
  declare; after this change a target declares none, so the same rule has to read
  the transports the example's **connections** declare or it passes vacuously and
  stops protecting anything.

### Key Entities

- **Target**: a named orchestrator instance in `targets.yaml`. Owns the
  orchestrator (`provider`, `version`, `pins`, `sdk_language`), the deployment
  (`deployment_region`), and the model overrides. Owns exactly one pointer to a
  Connection and no other route fact. After this feature a target holds no phone
  number of any kind.
- **Destination**: a symbolic name in `agent.yaml` mapped to the `UPPER_SNAKE`
  name of an environment variable holding an E.164 number or a `sip:` URI. Who
  this agent escalates to, which is the same desk whichever carrier reaches it.
  The model only ever sees the symbolic name.
- **Connection**: a named file under `connections/`. Owns the whole route: the
  mechanism (`transport`), the carrier when the route has one, and the account
  environment names. Reusable by more than one target when the route resolves for
  each target's provider. May declare no environment when the route needs no
  credentials.
- **Route**: the triple `(provider, transport, carrier)` the compiler resolves
  capabilities, required environment, endpoints, and evidence against. Unchanged
  by this feature; only where its three parts are written changes.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A phone-capable target names its route in one line instead of
  three. Across the five shipped telephony examples, route lines in `targets.yaml`
  drop by at least 60%.
- **SC-002**: Every route fact an author writes appears in exactly one file. A
  reader can name a package's mechanism, carrier, and credentials by opening one
  connection file, with no second file to consult. An environment *name* is the
  one deliberate exception: it appears in the file that says what role it plays
  and again in `secrets:`, which says it is required (FR-005b).
- **SC-003**: All five shipped telephony examples — `twilio-telephony-hello`,
  `livekit-human-transfer`, `pipecat-human-transfer-twilio`, `outbound-reminder`,
  and `pipecat-human-transfer-daily` — compile to the same route, the same
  required environment, and the same emitted file set as before the change,
  proven by the existing golden files. Zero of the five still declare `transport`,
  `carrier`, or `destinations` on a target, and zero still declare `kind` in a
  connection.
- **SC-004**: A phone-enabled example starts a browser session with zero carrier
  environment variables set.
- **SC-005**: Each of the seven strict-validation checks (FR-010 to FR-016) is
  reachable from a hand-written package and produces exactly one message that
  names the file, the key, and the accepted values or the fix. Six fail the
  command; FR-015 warns and exits 0.
- **SC-006**: A package written in the old shape produces one message that tells
  the author exactly which lines to move and where, with no reading of `docs/`
  required to act on it.
- **SC-007**: Every field of the new shape appears in `docs/user/reference/`, and
  every link between the example pages and `docs/` still resolves, proven by the
  existing example-agreement checks — rewritten per FR-027 so they read
  transports from connections rather than from targets, and therefore still fail
  when a README and a package disagree.
- **SC-008**: A reader can answer "where does each phone value go" from a single
  table, and `secrets:` lists every environment name the author wrote. Across the
  five examples, the count of author-written environment names missing from
  `secrets:` is zero — asserted by a test, not by inspection, since the underlying
  check only warns and a warning is easy to stop reading.
- **SC-009**: A reader who wants their agent to answer the phone can follow one
  page from route choice to first call, without opening a second page to find out
  which transfers their route supports or where their credentials go.
- **SC-010**: Across the five telephony examples, the count of environment
  variable names in a generated `.env.example` that appear in neither the
  example's README nor the phone-calls page is zero, and a check fails when that
  count is not zero. In the other direction, no README's credentials section names
  a variable absent from that package's `.env.example` — held by review rather
  than by a check.

## Assumptions

- **Pre-release, so the shape may change.** The repository is on `pre-release-v1`
  and there are no external packages to migrate. The migration path for the old
  shape is a good error message, not a compatibility layer (FR-007).
- **The route catalog is unchanged.** This feature moves where a route is written.
  It adds no transport, no carrier, and no capability, and it lifts no
  `provisional` tag. Live-call evidence in `docs/TRANSFERS.md` is untouched. Which
  transfers compile on which route is unchanged; only the refusal message improves
  (FR-016a).
- **The scope grew on 2026-08-13, deliberately.** The feature began as "move
  `transport` and `carrier` into the connection" and now also moves
  `destinations` into `agent.yaml`, narrows it to environment names, and removes
  the `secrets:` exemption. All three came from one question — where does a phone
  value go — and answering it in one place is the point. The cost is that this is
  now an `agent.yaml` change as well as a `targets.yaml` one, so the SCHEMA
  amendment touches §4.12, §4 (new destinations home), §6.1, and §6.3.
- **`provider` stays on the target.** It selects the orchestrator, so it cannot
  move into a file that is meant to be shareable between orchestrators. This is
  why a connection is effectively bound to one provider in practice: transport
  values do not overlap between providers.
- **Redis, coordination, and admission are unaffected.** They are resolved from
  the route, and the route is unchanged.
- **No new dependency.** This is a change to how existing structs are shaped and
  validated. The derived schemas keep coming from the Go structs, never
  hand-authored JSON (CLAUDE.md).
- **The channel contract is unchanged.** `channels.phone` and the `capacity` block
  it forces stay exactly as they are. This feature does not revisit whether a
  phone channel should be required.
- **Region behaviour is unchanged.** `deployment_region` keeps its two meanings
  per platform and its forwarded-as-written rule (SCHEMA N32).

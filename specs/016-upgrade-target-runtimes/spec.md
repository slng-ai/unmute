# Feature Specification: Upgrade Target Runtimes and Make Version Support Scalable

**Feature Branch**: `feat/livekit-pipecat-upgrades-1969ee`

**Created**: 2026-08-16

**Status**: Draft

**Input**: User description: "Upgrade unmute's supported livekit-agents and pipecat-ai target versions to the latest releases, and make version support scalable: support a range of versions (multi-version) tied to the unmute CLI version, while each generated target file pins exactly one version. Investigate what changed between our currently supported versions and the latest releases, what must be updated in emitted code/templates, and how to keep future upgrades cheap. All examples must be tested and verified by a human through a live call."

## What the investigation found

This section records the research this spec is built on, so the plan does not have to redo it. All findings verified 2026-08-16 against PyPI, the upstream changelogs, and this repository.

**Upstream state.** The latest pipecat-ai is **1.7.0** (2026-08-01; we support 1.5.0 today). The latest livekit-agents is **1.6.10** (2026-08-13; our examples declare 1.6.4, our docs say "verified against 1.6.9"). Neither framework has a 2.0. For the code unmute emits, both upgrades are compatible: no class, module path, or constructor that our generated projects use changed in either range. The items worth acting on are small: the livekit openai plugin now requires `openai<3`; livekit-agents 1.6.10 requires Python `>=3.10,<3.15`; pipecat's `runner` extra now pulls `pipecat-ai-prebuilt>=1.0.5` on its own; livekit's `dev` and `console` run modes are deprecated (not removed) since 1.6.8; this release drops all use of them (see Clarifications and FR-011). Pipecat 1.7.0 also fixed LLMSwitcher settings delivery, which is the machinery behind our per-task model gate (B7); re-verifying that gate is a separate task, not part of this feature.

**Repository state.** There is no single home for framework versions today. The facts live in at least five unsynchronized places: a validation window, per-driver emission constants, a plugin floor written twice as bare literals, about fifty hand-written version strings across examples, docs, docs-site, and the skill, and the golden files. The two drivers also disagree in kind: a Pipecat target's authored `version:` becomes an exact install pin, while a LiveKit target's authored `version:` is validated and then thrown away, and the generated project floats to whatever the newest 1.x release is. That float is why our own records disagree with each other. The examples are internally inconsistent too (Pipecat targets carry 1.5.0 and 1.5.2; LiveKit targets carry 1.5.2 and 1.6.4, with no rule for the split).

## Clarifications

### Session 2026-08-16

- Q: Does the default model-vendor binding change with this upgrade? → A: No. SLNG stays the default on both targets, exactly as today (`pipecat-slng` on Pipecat, `livekit-plugins-slng` on LiveKit). The upgrade must keep those defaults working at the new ceiling versions.
- Q: LiveKit deprecated its local `dev` and `console` run modes in 1.6.8 (still working, no removal date). Keep them or drop them? → A: Drop all deprecated-mode use this release. The browser dev path moves off `agent.py dev` to a non-deprecated worker mode, and emitted run instructions stop recommending deprecated modes.
- Q: Does the terminal path survive on Pipecat? → A: No. `--console` is dropped for every target. `unmute dev` runs local development through Docker containers and compose on both frameworks, in the browser by default and over a phone where the package declares telephony. Generated projects stop carrying terminal-mode run scaffolding and instructions.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - The generated project installs exactly the version the author declared (Priority: P1)

An author writes `version:` on a code target, compiles, and the generated project installs exactly that framework version, on both LiveKit and Pipecat. Today this promise holds on Pipecat and silently breaks on LiveKit. The newest supported versions become pipecat-ai 1.7.0 and livekit-agents 1.6.10.

**Why this priority**: A declared version that has no effect is a silent downgrade of the author's intent, and it is the root cause of our version records drifting apart. Everything else in this feature builds on the pin being real.

**Independent Test**: Compile one package per target with a declared version, inspect the generated dependency file, and install it. The installed framework version equals the declared version on both targets.

**Acceptance Scenarios**:

1. **Given** a LiveKit target declaring `version: "1.6.10"`, **When** the author compiles, **Then** the generated project declares livekit-agents at exactly 1.6.10 and installing it yields 1.6.10, not a newer release.
2. **Given** a Pipecat target declaring `version: "1.7.0"`, **When** the author compiles, **Then** the generated project declares pipecat-ai at exactly 1.7.0, as it already does for 1.5.0 today.
3. **Given** an existing package declaring any version inside the supported range, **When** the author recompiles with the new unmute, **Then** validation still passes and no authoring file needs to change.

---

### User Story 2 - One recorded home for what each unmute release supports (Priority: P2)

Each unmute release supports a stated version range per framework: a floor and a verified ceiling, where the ceiling is the newest release a human has verified end to end. The range lives in exactly one recorded home. Validation, generation, the scaffold default, and every printed or documented range statement derive from that home. An author who declares a version outside the range gets a loud error naming the supported range, including a version newer than the ceiling, because unverified is unsupported.

**Why this priority**: This is what makes future upgrades cheap. When the next framework release lands, the upgrade becomes: raise the ceiling in one place, regenerate derived outputs, verify, release. It also ties the supported range to the unmute release itself, since the range a binary enforces is the range it shipped with.

**Independent Test**: Change the recorded ceiling and confirm validation, the scaffold default, and generation all follow without any other edit. Declare a version above the ceiling and below the floor and confirm both fail with the range named.

**Acceptance Scenarios**:

1. **Given** a target declaring a version above the verified ceiling, **When** the author validates, **Then** validation fails, names the target, and states the supported range for that framework.
2. **Given** a target declaring a version below the floor, **When** the author validates, **Then** validation fails the same way it does today, with the range named.
3. **Given** a new scaffolded package, **When** the author inspects it, **Then** the pre-filled version is the verified ceiling for the chosen target.
4. **Given** an author who wants to know what their installed unmute supports, **When** they look at the compile report or the version output, **Then** the supported range and verified ceiling per framework are stated there.

---

### User Story 3 - Every example is current, consistent, and proven on a live call (Priority: P3)

All examples declare the verified ceiling versions, every document and the coding-agent skill state the same versions, and a human verifies each example by talking to it on a live call before the release ships.

**Why this priority**: The examples are the fixtures the smoke layer installs and the first thing a reader copies. Inconsistent or unverified examples turn the upgrade into a claim instead of a fact. This story depends on the first two, so it lands last.

**Independent Test**: A human runs each example, holds a real conversation, exercises the example's distinguishing behavior (transfer, handoff, tool call, outbound call), and records pass or fail. A consistency check finds no version string that disagrees with the recorded home.

**Acceptance Scenarios**:

1. **Given** the upgraded examples, **When** a human runs each one and speaks to it on a live call, **Then** every example holds a conversation and performs its distinguishing behavior, and the result is recorded per example.
2. **Given** the shipped documentation, examples, and skill, **When** their version statements are compared against the recorded home, **Then** none disagrees.
3. **Given** the opt-in smoke layer, **When** it runs, **Then** it installs exactly the versions the examples declare, on both frameworks, instead of floating.

---

### Edge Cases

- An authored version is inside the range but older than the verified ceiling: it validates and compiles, and the pin is honored exactly. The range is a compatibility promise, not a nudge.
- An authored version is newer than the ceiling (for example the day after upstream releases): validation fails loudly with the range named. The fix is upgrading unmute, and the error should say so.
- A framework ships a 2.0: it is outside every range this feature defines, so it fails validation by the existing major-version rule until a future unmute release raises the ceiling deliberately.
- A `pins:` entry conflicts with an upstream constraint that the ceiling introduces (for example an `openai` pin at or above 3 next to the LiveKit openai plugin): the generated project must not be able to express an install that upstream forbids without the compile saying so.
- The two frameworks have different Python requirements (pipecat needs 3.11 or newer, livekit-agents accepts 3.10 up to but not including 3.15): the environments the generated projects declare and run in must satisfy both today and stay inside livekit's new upper bound.
- A target omits `version:`: already a hard error for code targets today; that stays.
- An author runs `unmute dev --console` on any target: a clear error explains the terminal path was removed and points at browser dev mode. It must not start a session and must not fail with an unexplained flag error.
- An author has no Docker available: with the terminal path gone, there is no Docker-free run path left, so the existing Docker-missing error is the only answer and must stay clear about what to install.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The verified ceiling versions become pipecat-ai **1.7.0** and livekit-agents **1.6.10**. The floor stays at 1.5 for both frameworks, since the emitted code surface is compatible across the whole range.
- **FR-002**: Every code target's generated project MUST install exactly the framework version the author declared. This closes the LiveKit gap where the declared version is validated and then discarded.
- **FR-003**: Each framework's supported range (floor, verified ceiling, and the date the ceiling was verified) MUST live in exactly one recorded home. Validation, generation, and the scaffold default MUST derive from it, and an agreement check MUST fail if any derived surface drifts from it. The same rule applies to any companion version floor stated in more than one place today (the known case: the silero plugin floor, written twice as unlinked literals): one home, or an agreement check that fails on drift.
- **FR-004**: A declared version outside the supported range, above or below, MUST fail validation before any artifact is written, naming the target and the supported range. A version above the ceiling MUST additionally state that a newer unmute may support it.
- **FR-005**: New scaffolded packages MUST default to the verified ceiling of the chosen target's framework.
- **FR-006**: All shipped examples MUST declare the verified ceiling versions, and the opt-in smoke layer MUST install exactly what the examples declare on both frameworks.
- **FR-007**: The supported range per framework MUST be discoverable from the tool itself (in the compile report, and in the version output), so the range is visibly tied to the unmute release the author runs.
- **FR-008**: Every document that states a framework version (docs, public docs site, example pages, the coding-agent skill, the scaffold README) MUST state the same versions as the recorded home, in the same change. Where a check can hold this, it must; prose that cannot be checked is reviewed by hand.
- **FR-009**: Generated projects MUST NOT be able to declare dependency combinations the ceiling versions forbid upstream (the known case today: `openai` must stay below 3 next to the LiveKit openai plugin).
- **FR-010**: Existing packages that validate today with an in-range version MUST keep validating and compiling with no authoring change. The `version:` field's name, place, and format do not change.
- **FR-011**: Nothing the tool emits, runs, or documents may use a run mode the ceiling version deprecates. For the LiveKit ceiling this means two things: the browser dev path runs a non-deprecated worker mode instead of `agent.py dev`, and emitted run instructions recommend only non-deprecated modes. The reworked browser dev path is covered by the live-call verification in FR-012.
- **FR-012**: Before release, a human MUST verify every shipped example through a live call at the ceiling versions, exercising each example's distinguishing behavior, and the per-example results MUST be recorded in the repository. Because the scaffold and the examples bind SLNG models by default, this verification also proves the SLNG plugins working at the ceiling versions; a ceiling the SLNG plugins cannot serve does not ship.
- **FR-013**: The terminal path is removed for every target: `unmute dev` no longer accepts `--console`, and local development runs through Docker containers and compose on both frameworks, in the browser by default and over a phone where the package declares telephony. Passing the removed flag MUST fail with a clear error pointing at browser dev mode, never with a bare unknown-flag message. Generated projects MUST stop carrying terminal-mode run scaffolding and instructions (the known cases today: the Pipecat console extra and the console lines in emitted run instructions).

### Key Entities

- **Supported range**: per framework, a floor version, a verified ceiling version, and a verification date. Owned by one recorded home per release. The ceiling is the only version a release claims as proven.
- **Declared version**: the single exact version an author writes on a code target. Must fall inside the supported range. Becomes the generated project's exact install pin.
- **Verification record**: the per-example, per-release record of the human live-call check: example name, versions, date, result.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For 100% of compiled code targets, the framework version installed by the generated project equals the version declared in the package, on both frameworks.
- **SC-002**: A future ceiling bump that needs no template change (the common case, as this one proved for both frameworks) touches exactly one recorded home plus regenerated derived outputs, and takes under an hour of work before verification starts.
- **SC-003**: All eleven shipped examples pass a human live-call verification at pipecat-ai 1.7.0 and livekit-agents 1.6.10, with results recorded per example.
- **SC-004**: An author declaring an out-of-range version learns the supported range from the first error message, with no other lookup needed.
- **SC-005**: Zero contradictory framework-version statements remain across examples, docs, the public docs site, and the skill, and the consistency check that holds this stays green.
- **SC-006**: The supported range is visible to an author from the tool itself without opening any document.

## Assumptions

- "Multi-version support" means one template set per framework that is compatible across the whole supported range, with the exact installed version chosen by the author inside that range. It does not mean per-version template sets or per-version emitters. Both upstream ranges were verified compatible with the emitted code surface, so the single template set is real, not hoped for. If a future ceiling ever needs incompatible emitted code, that release narrows the floor instead of forking templates, and only forks if narrowing is impossible. This is the deliberate answer to "is this too complex": the range-plus-exact-pin shape is the simple version; per-version emitters would be the complex one, and nothing today needs it.
- Tying support to the unmute version means each release binary carries and enforces its own range, and the release notes state it. It does not mean a lookup service or a runtime check against the network.
- The per-task model gate on Pipecat (B7) stays gated. Pipecat 1.7.0 fixed the machinery the gate is about, so a re-verification is worth doing, but it is its own runtime-verified change, not a rider on this one.
- The Pipecat Cloud base image and the turn-detector version map are separate version axes and stay out of scope, except that whatever the upgraded examples exercise gets covered by the live-call verification.
- Changing what a LiveKit compile emits for the same package is a behavior change to the generated artifact, so it lands with the required dated schema amendment and regenerated goldens. The authoring shape itself does not change.
- Human live-call verification runs every example through browser dev mode, or telephony where the example is telephonic, since the terminal path is removed (FR-013). No new verification tooling is in scope.
- Moving the LiveKit browser dev path off `agent.py dev` changes an emitted artifact, so it lands with the same dated schema amendment and regenerated goldens as the pin change, and it is proven by a live call, not by reading upstream notes.
- Removing `--console` changes the dev command's documented surface, which the project constitution describes by name. This feature therefore carries the matching constitution amendment alongside the schema amendment, and the usual documentation surfaces (docs, public docs site, example pages, the skill) drop their terminal-mode instructions in the same change.
- The default model-vendor binding does not change: SLNG remains the default the scaffold writes and the examples use, on both targets, as today. Nothing in this feature adds, removes, or re-ranks vendors.

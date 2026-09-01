package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// prefetchFixture is the package carrying all three prefetch sources plus one
// value the caller has to confirm.
func prefetchFixture(t *testing.T) *ir.Agent {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "prefetch_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

func prefetchEmitted(t *testing.T, provider ir.Provider, file string) string {
	t.Helper()
	agent := prefetchFixture(t)
	artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return artifactFile(t, artifact, file)
}

// The whole feature rests on where the block runs: after the call's facts have
// landed, so there is something to read, and before the session starts, so every
// prompt the agent renders already sees the values. One line out of place and the
// values arrive after the greeting has already asked for them.
func TestPrefetchRunsAtTheSeamOnLiveKit(t *testing.T) {
	py := prefetchEmitted(t, ir.ProviderLiveKit, "agent.py")

	if got := strings.Count(py, "async def _prefetch("); got != 1 {
		t.Fatalf("_prefetch is defined %d times, want exactly 1", got)
	}
	calls := strings.Count(py, "await _prefetch(")
	if calls == 0 {
		t.Fatal("_prefetch is defined and never called")
	}
	// Every seam that starts a session runs it: this driver starts one from four
	// different places and a seam that forgets is a route where the feature
	// silently does nothing.
	if starts := strings.Count(py, "await session.start(agent="); calls > starts {
		t.Errorf("_prefetch called %d times but only %d session starts", calls, starts)
	}
	assertBefore(t, py, "await _prefetch(", "await session.start(agent=")
	assertBefore(t, py, "async def _prefetch(", "await _prefetch(")
}

func TestPrefetchRunsAtTheSeamOnPipecat(t *testing.T) {
	py := prefetchEmitted(t, ir.ProviderPipecat, "bot.py")

	if got := strings.Count(py, "async def _prefetch("); got != 1 {
		t.Fatalf("_prefetch is defined %d times, want exactly 1", got)
	}
	if !strings.Contains(py, "await _prefetch(state, call_context)") {
		t.Error("_prefetch is not called with the state and the call context")
	}
	// After the state exists so there is somewhere to put the values, and before
	// the agents are built so every prompt they render already sees them.
	assertBefore(t, py, "state = build_state(call_context)", "await _prefetch(")
	assertBefore(t, py, "await _prefetch(", "agents = [")
}

// FR-008, the promise that lets this ship: a package declaring none of the three
// new fields compiles byte-for-byte what it compiled before. The salon package is
// the subject because it is the one real package in the tree, and the assertion
// is on the whole emitted file rather than on a grep, because a stray blank line
// from a template partial is exactly the failure this catches (and did).
func TestPrefetchEmitsNothingForAPackageThatDeclaresNone(t *testing.T) {
	for _, tc := range []struct {
		provider ir.Provider
		file     string
	}{
		{ir.ProviderLiveKit, "agent.py"},
		{ir.ProviderPipecat, "bot.py"},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			agent := salonAgent(t)
			for name, variable := range agent.Variables {
				if variable.Confirm != "" {
					t.Skipf("the salon package now declares confirm: on %s, so it is no longer the control case", name)
				}
			}
			if len(agent.Prefetch) > 0 {
				t.Skip("the salon package now declares prefetch:, so it is no longer the control case")
			}
			artifact, err := Generate(agent, targetByProvider(t, agent, tc.provider), target.Default())
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			py := artifactFile(t, artifact, tc.file)
			for _, absent := range []string{"_prefetch", "_PREFETCH_", "_unconfirmed", "ZoneInfo"} {
				if strings.Contains(py, absent) {
					t.Errorf("%s carries %q for a package that declares no prefetch:", tc.file, absent)
				}
			}
		})
	}
}

// FR-006. Reading a trace back is how this feature is verified, so every outcome
// has to be visible in one, and each line has to say which entry it is about: a
// log that says "prefetch skipped" with three entries in the block is a log that
// sends you to the wrong one.
func TestPrefetchLogsEveryOutcomeByName(t *testing.T) {
	py := prefetchEmitted(t, ir.ProviderLiveKit, "agent.py")
	for _, want := range []string{
		// resolved, one per source kind
		`logger.info(f"prefetch today: resolved booking_date={state.booking_date}")`,
		`logger.info("prefetch caller: resolved caller_phone, awaiting confirmation")`,
		`logger.info("prefetch profile: resolved caller_name, awaiting confirmation")`,
		// skipped, for both reasons an entry can skip
		`logger.info("prefetch caller: skipped, the call carries no from_number")`,
		`logger.info("prefetch profile: skipped, caller_phone is empty")`,
		// timed out and failed
		`f"prefetch profile: gave up after {_PREFETCH_BUDGET_S}s; caller_name keeps its default"`,
		`logger.exception("prefetch profile: failed; caller_name keeps its default")`,
	} {
		if !strings.Contains(py, want) {
			t.Errorf("the emitted block is missing the log line %q", want)
		}
	}
}

// One generator, two logging libraries. LiveKit's emitted module logs through
// stdlib `logging`, which interpolates `%s`; Pipecat's logs through loguru, which
// interpolates `{}`. A shared block cannot use either, because whichever it picks
// prints the placeholder literally on the other target.
//
// This is a found bug, not a hypothetical: a real Pipecat smoke run logged
// `prefetch today: resolved booking_date=%s`, and no Go test could have seen it
// because the string was correct — for the other driver.
func TestPrefetchLogsCarryNoLibrarySpecificPlaceholder(t *testing.T) {
	for _, tc := range []struct {
		provider ir.Provider
		file     string
	}{
		{ir.ProviderLiveKit, "agent.py"},
		{ir.ProviderPipecat, "bot.py"},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			block := prefetchBlockOf(t, prefetchEmitted(t, tc.provider, tc.file))
			for _, banned := range []string{"%s", "%d", ".format("} {
				if strings.Contains(block, banned) {
					t.Errorf("the block uses %q, which only one of the two logging libraries "+
						"interpolates; format the value into the string with an f-string instead", banned)
				}
			}
			// And the value really is interpolated, so this cannot pass by the block
			// simply having stopped logging values.
			if !strings.Contains(block, "{state.booking_date}") {
				t.Error("the resolved log line no longer carries the value it resolved")
			}
		})
	}
}

// FR-005. The block cannot delay the greeting past its budget and cannot fail a
// call. Both except arms swallow, and that is the point: a lookup that dies is a
// call that greets on time with the values at their defaults, which is exactly
// what a route supplying no caller ID already does.
func TestPrefetchNeitherBlocksNorRaises(t *testing.T) {
	py := prefetchEmitted(t, ir.ProviderLiveKit, "agent.py")
	if !strings.Contains(py, "_PREFETCH_BUDGET_S = 2.0") {
		t.Error("the budget is not emitted as a named constant")
	}
	if !strings.Contains(py, "async with asyncio.timeout(_PREFETCH_BUDGET_S):") {
		t.Error("the lookup is not bounded by the budget")
	}
	for _, arm := range []string{"except TimeoutError:", "except Exception:"} {
		if !strings.Contains(py, arm) {
			t.Errorf("the block is missing %q, so a slow or broken lookup fails the call", arm)
		}
	}
	// Neither arm re-raises. Checked line by line rather than by substring:
	// `raise_for_status()` is a legitimate call inside the block and would match a
	// naive search for "raise".
	for _, line := range strings.Split(prefetchBlockOf(t, py), "\n") {
		if statement := strings.TrimSpace(line); statement == "raise" || strings.HasPrefix(statement, "raise ") {
			t.Errorf("the block re-raises (%q), so a failed pre-fetch can fail a call", statement)
		}
	}
}

// FR-018c. The emitted order is the authored order, and this is the gate that
// stops somebody "tidying" the block by sorting it. Sorted would be caller,
// profile, today; authored is today, caller, profile.
func TestPrefetchRunsEntriesInAuthoredOrder(t *testing.T) {
	for _, tc := range []struct {
		provider ir.Provider
		file     string
	}{
		{ir.ProviderLiveKit, "agent.py"},
		{ir.ProviderPipecat, "bot.py"},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			block := prefetchBlockOf(t, prefetchEmitted(t, tc.provider, tc.file))
			assertBefore(t, block, "# today:", "# caller:")
			assertBefore(t, block, "# caller:", "# profile:")
			if !strings.Contains(block, "Entries run in the order agent.yaml lists them") {
				t.Error("the block does not say its order is the authored one, so the next reader may sort it")
			}
		})
	}
}

// FR-003. A tool that exists only to be pre-fetched is never offered to any
// model: the whole saving is that nothing spends a round trip deciding to call
// it. It still has to reach the artifact, which is the other half of this.
func TestPrefetchToolReachesNoModelButDoesReachTheArtifact(t *testing.T) {
	agent := prefetchFixture(t)
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	py := artifactFile(t, artifact, "agent.py")
	// No @function_tool method, so it is in no agent's tool list.
	if strings.Contains(py, "async def lookup_customer(self, ctx: RunContext") {
		t.Error("the pre-fetched tool is emitted as a model-callable method, so the model can spend a turn calling it")
	}
	// The request is still built, from inside the block.
	if !strings.Contains(prefetchBlockOf(t, py), `os.environ["LOOKUP_CUSTOMER_URL"]`) {
		t.Error("the pre-fetched webhook's URL is not built in the block")
	}
	// And its env name still joins the startup check, so a missing URL is named
	// before anything dials rather than swallowed by the block's except arm.
	if !strings.Contains(requiredEnvBlock(t, py), `"LOOKUP_CUSTOMER_URL"`) {
		t.Error("the pre-fetched webhook's env name is missing from REQUIRED_ENV")
	}
}

// FR-022. The same tool may also be callable mid-call. The pre-fetch runs once,
// at the seam, and the mid-call path is untouched: two call sites for one tool
// is fine, two pre-fetches of it is not.
func TestPrefetchRunsOncePerCall(t *testing.T) {
	block := prefetchBlockOf(t, prefetchEmitted(t, ir.ProviderLiveKit, "agent.py"))
	if got := strings.Count(block, "lookup_customer"); got != 1 {
		t.Errorf("the block references lookup_customer %d times, want 1", got)
	}
}

// FR-017a. The size is only knowable while the call runs, so this cannot be a
// refusal. What it must not be is unbounded: a value that quietly outgrows what
// the router accepts takes the prompt caching with it.
func TestPrefetchBoundsARouterValueAndSaysSo(t *testing.T) {
	py := prefetchEmitted(t, ir.ProviderLiveKit, "agent.py")
	if !strings.Contains(py, "_PREFETCH_VALUE_MAX = 512") {
		t.Error("the bound is not emitted as a named constant")
	}
	if !strings.Contains(py, "def _prefetch_bounded(name, value):") {
		t.Error("no bound is applied to a pre-fetched value")
	}
	if !strings.Contains(py, "stops the") || !strings.Contains(py, "prompt being cached") {
		t.Error("shortening a value is not logged with what it costs")
	}
	// Every assigned value goes through it, so a new source kind cannot skip the
	// bound by accident. The clock is the exception and is exempt on purpose: an
	// ISO date is ten characters.
	block := prefetchBlockOf(t, py)
	if !strings.Contains(block, `state.caller_phone = _prefetch_bounded("caller_phone", _value)`) {
		t.Error("the call fact is written unbounded")
	}
	// The lookup's write is line-wrapped by the formatter, so this asserts the
	// call and its argument rather than one exact line.
	if !strings.Contains(block, `state.caller_name = _prefetch_bounded(`) || !strings.Contains(block, `"caller_name"`) {
		t.Error("the lookup result is written unbounded")
	}
}

// FR-024 and FR-030, the two halves that make confirmation worth having. A
// settled value satisfies the gate with no change to the guard at all, which is
// what makes a known caller skip the identification step outright. An unconfirmed
// one does not, which is what stops the agent acting on a number nobody agreed to.
func TestPrefetchedValueMeetsTheGateOnlyOnceConfirmed(t *testing.T) {
	py := prefetchEmitted(t, ir.ProviderLiveKit, "agent.py")
	// The helper itself is unchanged apart from consulting the set: a filled
	// variable passes `getattr(state, name, None) in (None, "")` exactly as it did
	// before this feature, so no new guard code was needed for the settled case.
	if !strings.Contains(py, `if getattr(userdata, name, None) in (None, "")`) &&
		!strings.Contains(py, `if getattr(state, name, None) in (None, "")`) {
		t.Error("the guard no longer reads a filled variable as satisfied")
	}
	if !strings.Contains(py, `or name in getattr(state, "_unconfirmed", ())`) {
		t.Error("the guard does not consult the unconfirmed set, so a proposed value satisfies a step")
	}
	if !strings.Contains(py, `state._unconfirmed.add("caller_phone")`) {
		t.Error("the pre-fetched number is never marked as awaiting confirmation")
	}
	// FR-029: the confirming step's own assign clears the mark. Read through
	// getattr, because this step is reachable on a path where the pre-fetch never
	// ran: a bare attribute read there is an AttributeError inside a finish
	// handler, which a real Pipecat smoke run hit before this was defended.
	if !strings.Contains(py, `"_unconfirmed", set()).discard("caller_phone")`) {
		t.Error("the confirming step's assign does not clear the mark, so the caller can never get past it")
	}
	if strings.Contains(py, `userdata._unconfirmed.discard(`) || strings.Contains(py, `state._unconfirmed.discard(`) {
		t.Error("the mark is cleared with a bare attribute read, which raises when the pre-fetch has not run")
	}
}

// FR-012. A seeded fact goes through the pre-fetch exactly as a real one does, so
// the local run marks it unconfirmed and reads it back. And it loses to a carrier
// value, because a seed stands in for a fact the route could not supply and never
// overrides one it did: a stale value in a .env must not reshape a real call.
func TestPrefetchSeedFillsOnlyWhatTheCarrierDidNot(t *testing.T) {
	for _, tc := range []struct {
		provider ir.Provider
		file     string
	}{
		{ir.ProviderLiveKit, "agent.py"},
		{ir.ProviderPipecat, "bot.py"},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			py := prefetchEmitted(t, tc.provider, tc.file)
			if !strings.Contains(py, `os.environ.get("`+LocalCallFactsEnv+`")`) {
				t.Errorf("%s does not read the seed, so the browser loop cannot exercise the path", tc.file)
			}
			// The carrier wins: the seed is only written where the fact is empty.
			if !strings.Contains(py, "if not facts.get(name):") {
				t.Error("the seed can overwrite a carrier fact")
			}
			// And it flows through the pre-fetch rather than around it, which is
			// what makes a seeded run mark the value unconfirmed.
			assertBefore(t, py, "def _prefetch_call_facts(", "async def _prefetch(")
			if !strings.Contains(py, "call_context = _prefetch_call_facts(call_context)") {
				t.Error("the pre-fetch does not merge the seed, so a seeded run skips the whole path")
			}
		})
	}
}

// The seed has to reach the container, not just this process. The worker runs in
// Compose, so `os.Setenv` in the CLI is not enough, and the first version of this
// read a driver flag that was still false at the point the list was built: the
// flag was set ten lines later, so the seed silently never arrived and the whole
// browser path did nothing.
func TestPrefetchSeedReachesTheContainer(t *testing.T) {
	for _, tc := range []struct {
		provider ir.Provider
	}{{ir.ProviderLiveKit}, {ir.ProviderPipecat}} {
		t.Run(string(tc.provider), func(t *testing.T) {
			compose := prefetchEmitted(t, tc.provider, "compose.dev.yaml")
			if !strings.Contains(compose, LocalCallFactsEnv) {
				t.Errorf("compose.dev.yaml does not pass %s through, so --source reaches the CLI and stops there",
					LocalCallFactsEnv)
			}
		})
	}
}

// And a package that reads no call fact gains nothing, so the compose file of
// every existing package stays where it is.
func TestPrefetchSeedIsAbsentWithoutACallFact(t *testing.T) {
	agent := prefetchFixture(t)
	if !PrefetchNeedsSeed(agent) {
		t.Fatal("the fixture stopped reading a call fact")
	}
	agent.Prefetch = agent.Prefetch[:1] // the clock entry alone
	if PrefetchNeedsSeed(agent) {
		t.Error("a package that reads no call fact still asks for the seed env")
	}
}

// US1's Independent Test, second half: all three sources in one block, on both
// targets, every value reaching the prompt it is permitted to reach and no tool
// advertised for any of them.
func TestPrefetchResolvesAllThreeSourcesOnBothTargets(t *testing.T) {
	for _, tc := range []struct {
		provider ir.Provider
		file     string
	}{
		{ir.ProviderLiveKit, "agent.py"},
		{ir.ProviderPipecat, "bot.py"},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			block := prefetchBlockOf(t, prefetchEmitted(t, tc.provider, tc.file))
			for _, want := range []string{
				// clock
				`state.booking_date = datetime.now(_PREFETCH_TZ).date().isoformat()`,
				`_PREFETCH_TZ`,
				// call fact
				`(call_context or {}).get("from_number")`,
				// lookup
				`os.environ["LOOKUP_CUSTOMER_URL"]`,
				`{"phone": state.caller_phone}`,
			} {
				if !strings.Contains(block, want) {
					t.Errorf("the block is missing %q:\n%s", want, block)
				}
			}
		})
	}
}

// SC-007. Every entry can skip, so every prompt naming a pre-fetched value has to
// read as a whole sentence when that value renders as nothing.
//
// This is the failure mode a compile check cannot see and a happy-path call never
// shows: the entry resolves on the developer's seeded run and on the first real
// call, and the sentence with a hole in it only appears for the caller who
// withheld their number.
func TestPrefetchedPromptsReadWholeWithEveryValueEmpty(t *testing.T) {
	agent := salonAgent(t)
	if len(agent.Prefetch) == 0 {
		t.Skip("the salon package declares no prefetch:, so there is nothing to render empty")
	}
	// Exactly the state a call with no carrier facts and no seed produces: every
	// pre-fetch-assigned variable at its declared default.
	var assigned []string
	for _, entry := range agent.Prefetch {
		for _, pair := range entry.Assign {
			assigned = append(assigned, pair.Key)
		}
	}
	if len(assigned) == 0 {
		t.Fatal("the prefetch block assigns nothing")
	}

	for _, prompt := range append(mapValues(promptBodies(agent.Agents)), mapValues(taskBodies(agent.Tasks))...) {
		rendered := prompt
		for _, name := range assigned {
			rendered = strings.ReplaceAll(rendered, "{{"+name+"}}", "")
		}
		if rendered == prompt {
			continue // this prompt names none of them
		}
		for _, line := range strings.Split(rendered, "\n") {
			// The three shapes an emptied placeholder leaves behind. `None` is the
			// one that actually reaches a caller as speech.
			if strings.Contains(line, "None") {
				t.Errorf("a prompt renders the word None where a value was empty: %q", line)
			}
			if strings.Contains(line, "  ") && strings.TrimSpace(line) != "" && !strings.HasPrefix(line, " ") {
				t.Errorf("a prompt has a doubled space where a value was empty: %q", line)
			}
			for _, dangling := range []string{" is .", " on .", " for .", " at .", " to .", " is ?", " on ?"} {
				if strings.Contains(line, dangling) {
					t.Errorf("a prompt has a dangling %q where a value was empty: %q", strings.TrimSpace(dangling), line)
				}
			}
		}
	}
}

// FR-016. The runbook is the file somebody reads at three in the morning, and the
// two questions it has to answer are which zone the clock is read in and what
// happens to a call that crosses midnight.
func TestPrefetchRunbookNamesTheZoneAndTheMidnightAnswer(t *testing.T) {
	for _, tc := range []struct {
		provider ir.Provider
	}{{ir.ProviderLiveKit}, {ir.ProviderPipecat}} {
		t.Run(string(tc.provider), func(t *testing.T) {
			readme := prefetchEmitted(t, tc.provider, "README.md")
			for _, want := range []string{
				"Europe/Madrid", "once", "session start", "midnight", "2.0 seconds", "_PREFETCH_BUDGET_S",
			} {
				if !strings.Contains(readme, want) {
					t.Errorf("the emitted runbook does not mention %q", want)
				}
			}
		})
	}
}

// prefetchBlockOf returns just the generated pre-fetch block, so an assertion
// about it cannot pass on text from elsewhere in a two-thousand-line module.
func prefetchBlockOf(t *testing.T, py string) string {
	t.Helper()
	const start = "# Pre-fetch, generated by internal/generate/prefetch.go."
	from := strings.Index(py, start)
	if from < 0 {
		t.Fatal("no pre-fetch block in the emitted module")
	}
	rest := py[from:]
	// The block ends where the next top-level definition or comment banner starts
	// after _prefetch's own body.
	body := strings.Index(rest, "async def _prefetch(")
	if body < 0 {
		t.Fatal("the block carries no _prefetch definition")
	}
	tail := rest[body:]
	if end := strings.Index(tail, "\n# ---"); end > 0 {
		return rest[:body+end]
	}
	if end := strings.Index(tail, "\n\n\n"); end > 0 {
		return rest[:body+end]
	}
	return rest
}

// assertBefore is the ordering assertion these tests keep needing: emitted order
// is behaviour here, not style.
func assertBefore(t *testing.T, text, first, second string) {
	t.Helper()
	i, j := strings.Index(text, first), strings.Index(text, second)
	if i < 0 {
		t.Fatalf("%q is not in the emitted output", first)
	}
	if j < 0 {
		t.Fatalf("%q is not in the emitted output", second)
	}
	if i > j {
		t.Errorf("%q must come before %q, and it comes after", first, second)
	}
}

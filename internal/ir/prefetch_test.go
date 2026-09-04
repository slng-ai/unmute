package ir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	packagespec "github.com/slng-ai/unmute/internal/spec"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// loadPrefetchCore loads the fixture that carries all three prefetch sources
// plus one value the caller has to confirm. It exists because no other fixture
// has all four pieces a prefetch needs: a variable to land in, a read-only tool
// to run, a task an agent runs so confirm: has a step to name, and a timezone.
func loadPrefetchCore(t *testing.T) *packagespec.Package {
	t.Helper()
	pkg, err := packagespec.Load(filepath.Join("..", "testdata", "prefetch_core"))
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func prefetchAgent(t *testing.T) *Agent {
	t.Helper()
	agent, err := Build(loadPrefetchCore(t))
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

// The order in the slice is the order in the file, and it is the order the
// emitted block resolves them in. Nothing sorts this: a sorted block would put
// back exactly what the list shape exists to remove, an order the reader cannot
// see (FR-018c, R9).
func TestBuildPrefetchKeepsTheAuthoredOrder(t *testing.T) {
	agent := prefetchAgent(t)
	var names []string
	for _, entry := range agent.Prefetch {
		names = append(names, entry.Name)
	}
	want := []string{"today", "caller", "profile"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("prefetch order = %v, want %v (the order agent.yaml lists them)", names, want)
	}
	// Sorted would be caller, profile, today. If this ever passes by accident,
	// the fixture's own order stopped distinguishing the two.
	if strings.Join(names, ",") == "caller,profile,today" {
		t.Error("the fixture's authored order matches sorted order, so it proves nothing")
	}
}

// The resolved entry carries what the emitted block and the per-target warning
// both read, so neither has to re-parse the templates.
func TestBuildPrefetchResolvesEachSource(t *testing.T) {
	agent := prefetchAgent(t)
	byName := map[string]Prefetch{}
	for _, entry := range agent.Prefetch {
		byName[entry.Name] = entry
	}

	if got := byName["today"]; got.Clock != PrefetchClockNow || got.Source != "" || got.Tool != "" {
		t.Errorf("clock entry = %+v", got)
	}
	// The zone rides the entry, not the package. Three assignments, one reading.
	if got := byName["today"]; got.Timezone != "Europe/Madrid" {
		t.Errorf("clock entry timezone = %q, want the zone the entry itself names", got.Timezone)
	}
	if got := len(byName["today"].Assign); got != 3 {
		t.Errorf("the clock entry assigns %d variables, want the 3 the fixture writes: a "+
			"one-assignment fixture cannot show that one reading fills several", got)
	}
	if got := byName["caller"]; got.Source != VariableSourceFromNumber || got.Clock != "" {
		t.Errorf("source entry = %+v", got)
	}
	profile := byName["profile"]
	if profile.Tool != "lookup_customer" {
		t.Errorf("tool entry = %+v", profile)
	}
	if strings.Join(profile.Inputs, ",") != "caller_phone" {
		t.Errorf("inputs = %v, want [caller_phone] parsed out of the args template", profile.Inputs)
	}
	// A source or tool entry carries no zone at all: only a clock reads one.
	if got := byName["caller"].Timezone; got != "" {
		t.Errorf("the call-fact entry carries a timezone (%q), and only a clock reads one", got)
	}
}

// FR-005. Two entries, two zones, both resolved. This is the thing one
// package-level key could not express, and the reason the zone moved onto the
// entry rather than merely being renamed.
func TestBuildPrefetchReadsTwoZones(t *testing.T) {
	dir := writePatchedPrefetchCore(t,
		"  - name: caller\n    source: from_number\n",
		"  - name: opening\n    clock: now\n    timezone: America/New_York\n    assign:\n"+
			"      - customer_id: result.date\n\n  - name: caller\n    source: from_number\n")
	pkg, err := packagespec.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatalf("two entries reading two zones were refused: %v", err)
	}
	zones := map[string]string{}
	for _, entry := range agent.Prefetch {
		if entry.Clock != "" {
			zones[entry.Name] = entry.Timezone
		}
	}
	if zones["today"] != "Europe/Madrid" || zones["opening"] != "America/New_York" {
		t.Errorf("zones = %v, want each entry to keep the one it named", zones)
	}
}

// FR-009. Both answers compile. `writes: true` is a declaration, not a request
// for permission: the compiler cannot check either answer, so what it can do is
// make the author state one, and then say which entries said yes.
func TestBuildPrefetchTakesEitherAnswerToWrites(t *testing.T) {
	for _, tc := range []struct {
		name string
		to   string
		want bool
	}{
		// The fixture already declares false, so that case is the fixture itself:
		// patching a value to the value it already has replaces nothing.
		{name: "writes: false", to: "    writes: false\n", want: false},
		{name: "writes: true", to: "    writes: true\n", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join("..", "testdata", "prefetch_core")
			if tc.want {
				dir = writePatchedPrefetchCore(t, "    writes: false\n", tc.to)
			}
			pkg, err := packagespec.Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			agent, err := Build(pkg)
			if err != nil {
				t.Fatalf("%s was refused: %v", tc.name, err)
			}
			for _, entry := range agent.Prefetch {
				if entry.Name != "profile" {
					// Nothing that runs no tool ever carries the answer.
					if entry.Writes {
						t.Errorf("prefetch %q runs no tool and carries writes: true", entry.Name)
					}
					continue
				}
				if entry.Writes != tc.want {
					t.Errorf("profile writes = %v, want %v", entry.Writes, tc.want)
				}
			}
		})
	}
}

// FR-026. A name looked up from an unconfirmed number is exactly as unconfirmed
// as the number was, and the author writes confirm: once rather than on every
// value downstream of it.
func TestBuildPrefetchInheritsConfirmation(t *testing.T) {
	agent := prefetchAgent(t)
	if got := agent.Variables["caller_phone"].Confirm; got != "verify_caller" {
		t.Errorf("the authored confirm: was lost: %q", got)
	}
	if got := agent.Variables["caller_name"].Confirm; got != "verify_caller" {
		t.Errorf("caller_name confirm = %q, want verify_caller inherited from its input", got)
	}
	if got := agent.Variables["booking_date"].Confirm; got != "" {
		t.Errorf("booking_date inherited a confirming step from nowhere: %q", got)
	}
}

// A prefetch-assigned variable holds a value before the first spoken word, which
// is what lets it render in a session-start prompt at all (FR-013).
func TestBuildPrefetchGivesAVariableASessionStartValue(t *testing.T) {
	agent := prefetchAgent(t)
	if !prefetchAssigns(agent, "booking_date") {
		t.Error("booking_date is assigned by a prefetch entry and was not recognised as such")
	}
	if prefetchAssigns(agent, "customer_id") {
		t.Error("customer_id is assigned by nothing and was reported as pre-fetched")
	}
}

// patchPrefetchCore edits the fixture's agent.yaml and returns Build's error, so
// each refusal is asserted against the same package an author would be editing.
func patchPrefetchCore(t *testing.T, from, to string) error {
	t.Helper()
	dir := writePatchedPrefetchCore(t, from, to)
	pkg, err := packagespec.Load(dir)
	if err != nil {
		return err
	}
	_, err = Build(pkg)
	return err
}

// copyPrefetchCore copies the fixture to a temp directory so a test can edit any
// file in it, not just agent.yaml.
func copyPrefetchCore(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "pkg")
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "testdata", "prefetch_core"))); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writePatchedPrefetchCore(t *testing.T, from, to string) string {
	t.Helper()
	dir := copyPrefetchCore(t)
	path := filepath.Join(dir, "agent.yaml")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	patched := strings.Replace(string(source), from, to, 1)
	if patched == string(source) {
		t.Fatalf("anchor %q is not in the fixture any more", from)
	}
	if err := os.WriteFile(path, []byte(patched), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// The shape refusals, rules 1 to 6 and D2. Each case asserts the location, so a
// message that names the problem but not the line still fails: a compiler that
// cannot say where is a compiler an author argues with.
func TestBuildPrefetchRefusesTheShape(t *testing.T) {
	for _, tc := range []struct {
		name string
		from string
		to   string
		want []string
	}{
		{
			name: "rule 1: no source key",
			from: "    clock: now\n",
			to:   "",
			want: []string{"agent.yaml:", `prefetch "today" names none of clock:, source: and tool:`, "clock: now for the date and time"},
		},
		{
			name: "rule 1: two source keys",
			from: "    clock: now\n",
			to:   "    clock: now\n    tool: lookup_customer\n",
			want: []string{`names both clock: and tool:`, "split it"},
		},
		{
			name: "rule 2: args on a clock entry",
			from: "    clock: now\n",
			to:   "    clock: now\n    args:\n      - phone: \"x\"\n",
			want: []string{"reads the clock, which takes no arguments", "args: belongs to a tool: entry"},
		},
		{
			name: "rule 2: args on a source entry",
			from: "  - name: caller\n    source: from_number\n",
			to:   "  - name: caller\n    source: from_number\n    args:\n      - phone: \"x\"\n",
			want: []string{"reads a fact the call carries, which takes no arguments"},
		},
		{
			name: "rule 3: empty assign",
			from: "    assign:\n      - booking_date: result.date\n      - booking_weekday: result.day_of_week\n      - booking_year: result.year\n",
			to:   "",
			want: []string{"assigns nothing", "nowhere to put it"},
		},
		{
			name: "rule 4: assign names an undeclared variable",
			from: "      - booking_date: result.date",
			to:   "      - bookng_date: result.date",
			want: []string{"assigns bookng_date", "not a declared variable", "variables: block"},
		},
		{
			name: "rule 5: assign names a field the source does not produce",
			from: "      - caller_name: result.name",
			to:   "      - caller_name: result.nmae",
			want: []string{"result.nmae", `lookup_customer's output declares no field "nmae"`, "customer_id, name"},
		},
		{
			// The six fields are one reading of one clock, so the refusal lists all
			// six rather than sending the author to a doc page to find the names.
			name: "rule 5: assign names a field the clock has no",
			from: "      - booking_date: result.date",
			to:   "      - booking_date: result.dat",
			want: []string{`the clock declares no field "dat"`,
				"date, time, datetime, day_of_week, year, timezone"},
		},
		{
			name: "rule 5: a right-hand side that is not result.<field>",
			from: "      - booking_date: result.date",
			to:   "      - booking_date: today",
			want: []string{"an assign value names one field of the result", "result.date"},
		},
		{
			name: "rule 6: two entries assign one variable",
			from: "  - name: caller\n    source: from_number\n    assign:\n      - caller_phone: result.value\n",
			to:   "  - name: caller\n    source: from_number\n    assign:\n      - booking_date: result.value\n",
			want: []string{"both assign booking_date", "One value has one source"},
		},
		{
			// One entry naming one variable twice is the same mistake as two
			// entries doing it, but the message for two entries reads "prefetch
			// today and prefetch today both assign booking_date", which sends the
			// reader looking for a second entry that is not there.
			name: "rule 6: one entry assigns one variable twice",
			from: "      - booking_date: result.date\n",
			to:   "      - booking_date: result.date\n      - booking_date: result.date\n",
			want: []string{`prefetch "today" assigns booking_date twice`, "One value has one source", "drop one of the two lines"},
		},
		{
			name: "D2: an entry with no name",
			from: "  - name: caller\n    source: from_number\n",
			to:   "  - source: from_number\n",
			want: []string{"a prefetch entry has no name:", "which entry they mean"},
		},
		{
			name: "D2: two entries share a name",
			from: "  - name: caller\n    source: from_number\n",
			to:   "  - name: today\n    source: from_number\n",
			want: []string{`two prefetch entries are both named "today"`, "cannot repeat"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := patchPrefetchCore(t, tc.from, tc.to)
			if err == nil {
				t.Fatal("the illegal shape was accepted")
			}
			assertRefusal(t, err, tc.want...)
		})
	}
}

// The source refusals, rules 7 to 12 and rule 9's four messages. Rule 9 is the
// one an author reaches by guessing a name, so each wrong guess gets its own
// answer rather than one list to search.
func TestBuildPrefetchRefusesTheSource(t *testing.T) {
	for _, tc := range []struct {
		name string
		from string
		to   string
		want []string
	}{
		{
			name: "rule 7: a clock entry with no timezone",
			from: "    timezone: Europe/Madrid\n",
			to:   "",
			want: []string{`prefetch "today" reads the clock and declares no timezone:`, "container clock is UTC",
				"wrong day", "Add timezone: to this entry", "timezone: Europe/Madrid"},
		},
		{
			name: "rule 8: a zone that is not a zone",
			from: "    timezone: Europe/Madrid",
			to:   "    timezone: Europe/Barcelona",
			want: []string{"sets timezone: Europe/Barcelona", "not an IANA zone name",
				"Europe/Madrid or America/New_York"},
		},
		{
			// The zone belongs to the reading. On any other entry it reaches
			// nothing, and an author who wrote one meant something by it.
			name: "rule 8: a timezone on an entry that reads no clock",
			from: "  - name: caller\n    source: from_number\n",
			to:   "  - name: caller\n    source: from_number\n    timezone: Europe/Madrid\n",
			want: []string{`prefetch "caller" sets timezone:`, "only a clock entry reads a zone",
				"clock: now", "drop the zone"},
		},
		{
			name: "rule 9: a clock value that is not now",
			from: "    clock: now",
			to:   "    clock: date",
			want: []string{"reads clock: date", "the clock reads now", "Use clock: now",
				"date, time, datetime, day_of_week, year, timezone"},
		},
		{
			// The package-level key is retired. Refused with a sentence naming
			// where it moved, rather than by the decoder saying "unknown field".
			name: "the retired package-level timezone",
			from: "name: prefetch-fixture\n",
			to:   "name: prefetch-fixture\n\ntimezone: Europe/Madrid\n",
			want: []string{"timezone: is no longer a package-level key", "belongs on the clock entry that reads it",
				"clock: now", "timezone: Europe/Madrid"},
		},
		{
			name: "rule 9: a source that is not a call fact",
			from: "    source: from_number",
			to:   "    source: caller_name",
			want: []string{"reads source: caller_name", "not a fact a call carries",
				"call_id, carrier, connection, direction, from_number, session_id, stream_id, to_number"},
		},
		{
			name: "rule 9: conversation gets its own reason",
			from: "    source: from_number",
			to:   "    source: conversation",
			want: []string{"source: conversation", "the model saves mid-call", "nothing holds it before the greeting"},
		},
		{
			name: "rule 9: call_start gets its own reason",
			from: "    source: from_number",
			to:   "    source: call_start",
			want: []string{"source: call_start", "arrives with the dispatch", "declare it as source: call_start on the variable"},
		},
		{
			name: "rule 10: a tool the package does not declare",
			from: "    tool: lookup_customer",
			to:   "    tool: lookup_custommer",
			want: []string{`names tool "lookup_custommer"`, "does not declare", "get_invoice, lookup_customer"},
		},
		{
			// The question is about this use of the tool, not about the tool, so
			// the refusal names the entry and asks the author to answer it there.
			name: "rule 11: a tool entry that does not say whether it writes",
			from: "    writes: false\n",
			to:   "",
			want: []string{`prefetch "profile" runs tool "lookup_customer" and does not say whether it writes`,
				"runs unasked on every call, wrong numbers and hang-ups included",
				"Read tools/lookup_customer.yaml", "add writes: false", "writes: true if it does"},
		},
		{
			name: "rule 11: writes: on a clock entry",
			from: "    clock: now\n",
			to:   "    clock: now\n    writes: false\n",
			want: []string{`prefetch "today" reads the clock, which runs no tool`,
				"writes: answers nothing here", "writes: belongs to a tool: entry"},
		},
		{
			name: "rule 11: writes: on a source entry",
			from: "    source: from_number\n",
			to:   "    source: from_number\n    writes: true\n",
			want: []string{`prefetch "caller" reads a fact the call carries, which runs no tool`,
				"writes: answers nothing here"},
		},
		{
			name: "rule 13: args name an undeclared variable",
			from: `      - phone: "{{caller_phone}}"`,
			to:   `      - phone: "{{callr_phone}}"`,
			want: []string{"reads {{callr_phone}}", "not a declared variable"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := patchPrefetchCore(t, tc.from, tc.to)
			if err == nil {
				t.Fatal("the illegal source was accepted")
			}
			assertRefusal(t, err, tc.want...)
		})
	}
}

// Rule 14, both directions. Reading a value an earlier entry assigned is the
// intended shape and is the whole reason the block is ordered; reading one a
// later entry assigns is the same intent written upside down, and the refusal
// names the line to move rather than reordering on the author's behalf.
func TestBuildPrefetchRefusesABackwardsOrder(t *testing.T) {
	// The correct order is what the fixture ships, and every other test here
	// depends on it building, so this asserts it explicitly rather than by
	// implication.
	t.Run("the authored order is accepted", func(t *testing.T) {
		agent := prefetchAgent(t)
		if len(agent.Prefetch) != 3 {
			t.Fatalf("the fixture stopped carrying three entries: %d", len(agent.Prefetch))
		}
	})

	t.Run("caller below profile is refused", func(t *testing.T) {
		caller := "  - name: caller\n    source: from_number\n    assign:\n      - caller_phone: result.value\n\n"
		profile := "  - name: profile\n    tool: lookup_customer\n    # This lookup reads. The key is required either way: the compiler cannot\n    # check either answer, so it makes the author state one.\n    writes: false\n    args:\n      - phone: \"{{caller_phone}}\"\n    assign:\n      - caller_name: result.name\n"
		err := patchPrefetchCore(t, caller+profile, profile+"\n"+caller)
		if err == nil {
			t.Fatal("a backwards list was accepted, so the file's visible order is not the agent's")
		}
		assertRefusal(t, err,
			`prefetch "profile" reads {{caller_phone}}`,
			`prefetch "caller" assigns further down the list`,
			"Entries resolve in the order you wrote them",
			`move "caller" above "profile"`)
	})
}

// Refusals 15 to 17: confirmation's own rules.
func TestBuildPrefetchRefusesConfirmation(t *testing.T) {
	t.Run("rule 15: confirm names a step that does not run", func(t *testing.T) {
		err := patchPrefetchCore(t, "    confirm: verify_caller", "    confirm: verify_custome")
		if err == nil {
			t.Fatal("a confirm: naming nothing was accepted")
		}
		assertRefusal(t, err, `names confirm: verify_custome`, "no step by that name runs",
			"Name a task an agent runs", "verify_caller")
	})

	// The greeting is the worst case and the clearest one: a stranger ringing from
	// a number the salon has on file would be greeted with somebody else's
	// details before anyone had agreed to anything (SC-006b).
	t.Run("rule 16: an unconfirmed value in the greeting", func(t *testing.T) {
		err := patchPrefetchCore(t,
			`    text: "Hi, you have reached Acme Support. How can I help you today?"`,
			`    text: "Hi, calling from {{caller_phone}}?"`)
		if err == nil {
			t.Fatal("an unconfirmed value rendered outside its confirming step")
		}
		// The message names all three things FR-027 asks for: the value, the site,
		// and the step that confirms it.
		assertRefusal(t, err, "{{caller_phone}}", "the caller has not confirmed yet",
			"conversation.greeting.text", `task "verify_caller"`, "the step that confirms it")
	})

	// An agent prompt is the same class of site, and it is the one an author
	// reaches for first, so it gets its own case rather than riding the greeting's.
	t.Run("rule 16: an unconfirmed value in an agent prompt", func(t *testing.T) {
		dir := copyPrefetchCore(t)
		path := filepath.Join(dir, "instructions.md")
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(body, []byte("\n\nThe caller is on {{caller_phone}}.\n")...), 0o600); err != nil {
			t.Fatal(err)
		}
		pkg, err := packagespec.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Build(pkg); err == nil {
			t.Fatal("an unconfirmed value rendered into an agent prompt")
		} else {
			// The location here is the prompt file, not agent.yaml, which is what
			// every existing template refusal already reports for a prompt site.
			got := err.Error()
			for _, want := range []string{
				"instructions.md", "{{caller_phone}}", "the caller has not confirmed yet",
				`agent "intake" instructions`,
			} {
				if !strings.Contains(got, want) {
					t.Errorf("the refusal is missing %q:\n%s", want, got)
				}
			}
		}
	})

	t.Run("rule 17: two inputs confirmed by different steps", func(t *testing.T) {
		dir := writePatchedPrefetchCore(t,
			"      - phone: \"{{caller_phone}}\"",
			"      - phone: \"{{caller_phone}}\"\n      - account: \"{{customer_id}}\"")
		pkg, err := packagespec.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		// A second confirming step, reached from intake's own tasks: list, so rule
		// 15 passes and rule 17 is the one that fires.
		variable := pkg.Agent.Variables["customer_id"]
		variable.Confirm = "verify_account"
		pkg.Agent.Variables["customer_id"] = variable
		task := pkg.Tasks["verify_caller"]
		task.Name, task.When = "verify_account", "Confirm the account."
		pkg.Tasks["verify_account"] = task
		pkg.Callables["verify_account"] = packagespec.Callable{Task: "verify_account", When: task.When}
		intake := pkg.Agent.Agents["intake"]
		intake.Tasks = append(intake.Tasks, packagespec.TaskItem{Task: &task})
		pkg.Agent.Agents["intake"] = intake

		if _, err := Build(pkg); err == nil {
			t.Fatal("one entry inherited two different confirming steps")
		} else {
			assertRefusal(t, err, "confirmed by two different steps", "Split the entry",
				"each result carries one confirming step")
		}
	})
}

// The confirming step's own prompt is the one place an unconfirmed value renders,
// and it has to keep working: without it there is nowhere to read the number back
// and the whole feature has no user-visible half (FR-025, FR-028).
func TestBuildPrefetchAllowsTheConfirmingStepsOwnPrompt(t *testing.T) {
	agent := prefetchAgent(t)
	if agent.Variables["caller_phone"].Confirm == "" {
		t.Fatal("the fixture stopped carrying a confirmed value")
	}
	// The fixture's verify_caller prompt names {{caller_phone}}, and Build
	// accepted it. Asserting the text rather than only the build keeps this from
	// passing if the fixture's prompt loses the reference.
	if !strings.Contains(agent.Tasks["verify_caller"].Instructions, "{{caller_phone}}") {
		t.Error("the confirming step's prompt no longer reads the number back, so this proves nothing")
	}
}

// W1: skipping is the specified behaviour on a route that supplies no caller ID
// (FR-004), so this is a warning and never a refusal. What would be wrong is
// silence, so the message names the target, the route, the entry, and the later
// entry that reads what this one would have assigned.
func TestValidatePrefetchWarnsOnARouteThatSuppliesNoCallFact(t *testing.T) {
	// session_id, not from_number: the Pipecat Twilio routes supply a caller
	// number now, and this test is about a fact a route supplies in no direction
	// at all. session_id is a compile-time literal on LiveKit and is deliberately
	// not granted on either Pipecat route, so it is the honest subject today.
	dir := writePatchedPrefetchCore(t, "    source: from_number\n", "    source: session_id\n")
	pkg, err := packagespec.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	enableTelephony(pkg)
	// Both targets need a route once the package has a phone channel. The pipecat
	// one is the subject; the livekit one exists so Build gets that far.
	setTargetField(pkg, "pipecat", func(target *packagespec.Target) { target.Connection = "primary_phone" })
	setTargetField(pkg, "livekit", func(target *packagespec.Target) { target.Connection = "twilio_sip" })
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Validate(agent, []Target{agent.Targets["pipecat"]}, targetcap.Default())
	if err != nil {
		t.Fatalf("a route that cannot supply a call fact must warn, not refuse: %v\n%v", err, report.PerTarget[0].Errors)
	}
	joined := strings.Join(report.PerTarget[0].Warnings, "\n")
	for _, want := range []string{
		`prefetch "caller" reads source: session_id`,
		"does not supply",
		"skipped on every call there",
		"caller_phone holds its default",
		`prefetch "profile" reads it, so that entry is skipped too`,
		// The routes that do supply it, read off the table rather than from a
		// hand-written list. The hand-written one said the LiveKit routes and
		// would have gone on saying so after two Pipecat routes joined.
		"source.session_id is supplied on (livekit, connector) and (livekit, sip)",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("W1 is missing %q, got:\n%s", want, joined)
		}
	}
}

// The direction half of W1, which is new. A route may supply a fact one way
// only, and on the Pipecat Twilio routes the caller's number is exactly that: a
// TwiML Bin is attached to one number, so the number being called is a constant
// there and only the caller's is worth carrying.
//
// Three outcomes, and two of them are silence. A grant with no direction limit
// says nothing, as it always did. A grant limited to a direction the package
// declares says nothing, because the fact arrives. A grant limited to the other
// direction warns, because there the grant is real and still reaches no call
// this package will ever take.
func TestValidatePrefetchWarnsWhenTheFactIsSuppliedTheOtherWay(t *testing.T) {
	for _, tc := range []struct {
		name     string
		source   string
		outbound bool
		want     []string
	}{
		{
			name:   "inbound package, fact supplied inbound: silent",
			source: "from_number",
		},
		{
			name:     "outbound package, fact supplied inbound only: warns",
			source:   "from_number",
			outbound: true,
			want: []string{
				`prefetch "caller" reads source: from_number`,
				"supplies on an inbound call only",
				"this package declares outbound",
				"caller_phone holds its default",
				`prefetch "profile" reads it, so that entry is skipped too`,
			},
		},
		{
			name:   "inbound package, fact granted with no direction limit: silent",
			source: "call_id",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The fixture already reads from_number, and patching a value to the
			// value it already has replaces nothing.
			dir := filepath.Join("..", "testdata", "prefetch_core")
			if tc.source != "from_number" {
				dir = writePatchedPrefetchCore(t, "    source: from_number\n", "    source: "+tc.source+"\n")
			}
			pkg, err := packagespec.Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			inbound, outbound := !tc.outbound, tc.outbound
			pkg.Agent.Channels["phone"] = packagespec.Channel{
				Kind: "telephony", Inbound: &inbound, Outbound: &outbound,
				RequiredControls: []string{"cold_transfer", "hangup"},
			}
			setTargetField(pkg, "pipecat", func(target *packagespec.Target) { target.Connection = "primary_phone" })
			setTargetField(pkg, "livekit", func(target *packagespec.Target) { target.Connection = "twilio_sip" })
			agent, err := Build(pkg)
			if err != nil {
				t.Fatal(err)
			}
			report, err := Validate(agent, []Target{agent.Targets["pipecat"]}, targetcap.Default())
			if err != nil {
				t.Fatalf("a one-way fact must warn, not refuse: %v\n%v", err, report.PerTarget[0].Errors)
			}
			joined := strings.Join(report.PerTarget[0].Warnings, "\n")
			if len(tc.want) == 0 {
				if strings.Contains(joined, "prefetch \"caller\"") {
					t.Errorf("the fact arrives on every call this package takes and it warned anyway:\n%s", joined)
				}
				return
			}
			for _, want := range tc.want {
				if !strings.Contains(joined, want) {
					t.Errorf("the direction warning is missing %q, got:\n%s", want, joined)
				}
			}
		})
	}
}

// The existing refusal for a variable naming a source its route cannot supply is
// deliberately untouched (R4). This is the assertion that it was not softened:
// the fixture declares no such variable, so the refusal never fires, and the
// package validates green on both code targets.
func TestValidatePrefetchLeavesTheSystemSourceRefusalAlone(t *testing.T) {
	pkg := loadPrefetchCore(t)
	for name, variable := range pkg.Agent.Variables {
		if variable.Source != "" {
			t.Errorf("variable %q carries source: %q; the prefetch reads the fact, so no variable has to",
				name, variable.Source)
		}
	}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Validate(agent, allTargets(agent), targetcap.Default())
	if err != nil {
		t.Fatalf("the fixture must validate on both code targets: %v", err)
	}
	for _, row := range report.PerTarget {
		if len(row.Errors) > 0 {
			t.Errorf("target %s refused the package: %v", row.Name, row.Errors)
		}
	}
}

// The slng target owns session start, so it has no seam of ours to resolve a
// fact in. The refusal says what to do instead rather than only saying no.
func TestValidatePrefetchRefusedOnSlng(t *testing.T) {
	for _, tc := range []struct {
		field targetcap.Field
		want  []string
	}{
		{targetcap.FieldPrefetch, []string{"no seam to run in", "compile to livekit or pipecat"}},
		{targetcap.FieldVariableConfirm, []string{"nowhere to go", "compile to livekit or pipecat"}},
		{targetcap.FieldDelegateAnnounce, []string{"nowhere to go", "compile to livekit or pipecat"}},
	} {
		t.Run(string(tc.field), func(t *testing.T) {
			capability := targetcap.Default().Capability(tc.field, targetcap.Slng)
			if capability.Tag != targetcap.Gated {
				t.Fatalf("%s on slng = %q, want gated", tc.field, capability.Tag)
			}
			for _, want := range tc.want {
				if !strings.Contains(capability.Note, want) {
					t.Errorf("the refusal for %s does not say %q: %s", tc.field, want, capability.Note)
				}
			}
		})
	}
}

// assertRefusal checks a refusal carries its location and every phrase the
// contract promises. The location check is separate because a message that says
// what is wrong but not where is a message an author argues with.
func assertRefusal(t *testing.T, err error, want ...string) {
	t.Helper()
	got := err.Error()
	if !strings.Contains(got, "agent.yaml:") {
		t.Errorf("the refusal carries no file and line: %s", got)
	}
	for _, phrase := range want {
		if !strings.Contains(got, phrase) {
			t.Errorf("the refusal is missing %q:\n%s", phrase, got)
		}
	}
}

// A pre-fetch resolves one plain value before the greeting: the clock formats a
// reading, a call fact arrives off the wire as text, and a tool hands back one
// field. Typed state is the other kind, and the two meet here because a step's
// `assign:` and a pre-fetch's write into the same variables.
//
// Letting a pre-fetch fill a list compiled, and the failure was not cosmetic:
// the emitted step calls `_append_entry` on what the pre-fetch left there, so a
// declared list holding a string dies with an AttributeError mid-sentence, on
// the call rather than at compile time.
func TestBuildPrefetchRefusesAValueWithFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		from string
		to   string
		want []string
	}{
		{
			// The crash. `caller_name` is what the tool entry assigns, so this
			// is the whole path: a tool result field into a declared list.
			name: "a list is filled by the step that produces it",
			from: "  caller_name:\n    type: string\n    default: \"\"\n",
			to:   "  caller_name:\n    type: list[string]\n",
			want: []string{`prefetch "profile" assigns caller_name`, "declared list[str]",
				"resolves one plain value before the greeting", "from the step that produces it"},
		},
		{
			// The same branch reached through a declared shape rather than a
			// list, because a shape is the other thing a step hands back whole.
			name: "a shape is filled by the step that produces it",
			from: "  caller_name:\n    type: string\n    default: \"\"\n",
			to: "  caller_name:\n    type: Customer | None\n\n" +
				"shapes:\n  - name: Customer\n    description: Who the caller is.\n" +
				"    fields:\n      - phone_number: Phone\n",
			want: []string{"declared Customer | None", "resolves one plain value before the greeting"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := patchPrefetchCore(t, tc.from, tc.to)
			if err == nil {
				t.Fatal("a pre-fetch filled a variable declared with fields")
			}
			assertRefusal(t, err, tc.want...)
		})
	}
}

// The other half, and the one that matters more: the fix must not refuse what a
// pre-fetch legitimately fills. Shaped text is checked where the value enters
// the state rather than in the schema the model is sent, so plain text assigns
// into it, and salon-concierge-v2 declares `customer_phone` exactly this way.
func TestBuildPrefetchFillsShapedText(t *testing.T) {
	for _, tc := range []struct {
		name string
		from string
		to   string
	}{
		{
			name: "a call fact fills a Phone",
			from: "  caller_phone:\n    type: string\n    default: \"\"\n",
			to:   "  caller_phone:\n    type: Phone\n    default: \"\"\n",
		},
		{
			name: "the clock fills a Date",
			from: "  booking_date:\n    type: string\n    default: \"\"\n",
			to:   "  booking_date:\n    type: Date\n    default: \"\"\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := patchPrefetchCore(t, tc.from, tc.to); err != nil {
				t.Fatalf("a pre-fetch was refused a variable it can fill: %v", err)
			}
		})
	}
}

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

	if got := byName["today"]; got.Clock != PrefetchClockDate || got.Source != "" || got.Tool != "" {
		t.Errorf("clock entry = %+v", got)
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
	if agent.Timezone != "Europe/Madrid" {
		t.Errorf("timezone = %q", agent.Timezone)
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
			from: "  - name: today\n    clock: date\n",
			to:   "  - name: today\n",
			want: []string{"agent.yaml:", `prefetch "today" names none of clock:, source: and tool:`, "clock: date for the current date"},
		},
		{
			name: "rule 1: two source keys",
			from: "  - name: today\n    clock: date\n",
			to:   "  - name: today\n    clock: date\n    tool: lookup_customer\n",
			want: []string{`names both clock: and tool:`, "split it"},
		},
		{
			name: "rule 2: args on a clock entry",
			from: "  - name: today\n    clock: date\n",
			to:   "  - name: today\n    clock: date\n    args:\n      - phone: \"x\"\n",
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
			from: "  - name: today\n    clock: date\n    assign:\n      - booking_date: result.date\n",
			to:   "  - name: today\n    clock: date\n",
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
			name: "D2: an entry with no name",
			from: "  - name: today\n    clock: date\n",
			to:   "  - clock: date\n",
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
			name: "rule 7: a clock with no timezone",
			from: "timezone: Europe/Madrid\n",
			to:   "",
			want: []string{"reads the clock", "declares no timezone:", "container clock is UTC", "wrong day", "timezone: Europe/Madrid"},
		},
		{
			name: "rule 8: a zone that is not a zone",
			from: "timezone: Europe/Madrid",
			to:   "timezone: Europe/Barcelona",
			want: []string{`timezone "Europe/Barcelona" is not an IANA zone name`, "Europe/Madrid or America/New_York"},
		},
		{
			name: "rule 9: a clock value that is not date",
			from: "    clock: date",
			to:   "    clock: time",
			want: []string{"reads clock: time", "the clock reads date", "Use clock: date"},
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
			name: "rule 11: a tool that has not declared read_only",
			from: "    tool: lookup_customer",
			to:   "    tool: get_invoice",
			want: []string{`names tool "get_invoice"`, "has not declared read_only: true",
				"would write on every call, wrong numbers included", "tools/get_invoice.yaml"},
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
		profile := "  - name: profile\n    tool: lookup_customer\n    args:\n      - phone: \"{{caller_phone}}\"\n    assign:\n      - caller_name: result.name\n"
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
	pkg := loadPrefetchCore(t)
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
		`prefetch "caller" reads source: from_number`,
		"does not supply",
		"skipped on every call there",
		"caller_phone holds its default",
		`prefetch "profile" reads it, so that entry is skipped too`,
		"Call sources compile on (livekit, sip) trunks and on (livekit, connector)",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("W1 is missing %q, got:\n%s", want, joined)
		}
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

// W3. The author has told the truth about the tool, they have just not pointed a
// prefetch at it yet, so this warns rather than refusing. Same shape as the
// existing warning for a turn binding carrying a field no target reads.
func TestValidateWarnsOnAReadOnlyToolNoPrefetchNames(t *testing.T) {
	pkg := loadPrefetchCore(t)
	// get_invoice is attached to two agents and named by no prefetch, so the
	// declaration reaches nothing while the tool itself stays perfectly reachable.
	// Dropping the entry that names lookup_customer would not do: a prefetch is
	// the only thing reaching that tool, so it would be refused as unreachable
	// before any warning fired.
	tool := pkg.Tools["get_invoice"]
	tool.ReadOnly = true
	pkg.Tools["get_invoice"] = tool
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Validate(agent, allTargets(agent), targetcap.Default())
	if err != nil {
		t.Fatalf("an unused read_only: must warn, not refuse: %v", err)
	}
	joined := strings.Join(report.PerTarget[0].Warnings, "\n")
	for _, want := range []string{`tool "get_invoice" declares read_only: true`, "no prefetch names it", "reaches nothing"} {
		if !strings.Contains(joined, want) {
			t.Errorf("W3 is missing %q, got:\n%s", want, joined)
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

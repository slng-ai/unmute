package ir

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	// tzdata embeds the IANA zone database in the binary, 413,008 bytes measured
	// at Go 1.26. It buys the one thing worth that much: `timezone: Europe/Madrid`
	// is accepted or refused by the compiler, identically on every machine. Without
	// it time.LoadLocation reads the host's own zoneinfo, so a package compiles on a
	// developer's Mac and is refused in a container that ships no zone data, which is
	// a refusal that depends on where you stood rather than on what you wrote.
	_ "time/tzdata"

	packagespec "github.com/slng-ai/unmute/internal/spec"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// PrefetchClockNow is the one clock reading there is. The entry reads `now` and
// the fields below are what that one reading yields, which is why an entry
// naming three of them cannot straddle a second or a midnight.
const PrefetchClockNow = "now"

// PrefetchClockFields is the clock's result grammar, in the order a refusal
// lists them: the two most people want first, then the rest.
//
// Exported because the emitter has to render one expression per field, and a
// field here with no expression there would emit an entry that assigns nothing.
// `TestPrefetchEmitsEveryClockField` is what holds the two lists together.
var PrefetchClockFields = []string{"date", "time", "datetime", "day_of_week", "year", "timezone"}

// prefetchResultValue is the single field a call fact produces. A call fact is
// one string, so `{value}` is its whole shape.
const prefetchResultValue = "value"

// buildPrefetch resolves `prefetch:` into agent.Prefetch, in the order the author
// wrote it, and refuses every shape a prefetch cannot have.
//
// Runs after variables, tools and controls are resolved, because every refusal
// here reads one of the three. Runs before checkTemplates, because a
// prefetch-assigned variable is one that has a session-start value and the
// template check has to know that.
func buildPrefetch(pkg *packagespec.Package, agent *Agent) error {
	if err := refuseRetiredTimezone(pkg); err != nil {
		return err
	}
	if len(pkg.Agent.Prefetch) == 0 {
		return nil
	}

	// assigned maps a variable to the entry that assigns it, and grows as the walk
	// proceeds. That growth is the whole of rule 14: a name in here is one an
	// earlier entry supplied, a name absent from here but assigned somewhere is
	// one a later entry supplies.
	assigned := map[string]string{}
	assignedBy := func(name string) (string, bool) {
		entry, ok := assigned[name]
		return entry, ok
	}
	later := laterAssignments(pkg.Agent.Prefetch)

	names := map[string]bool{}
	for _, raw := range pkg.Agent.Prefetch {
		where := prefetchLocation(pkg, raw)
		if err := checkPrefetchName(pkg, raw, names); err != nil {
			return err
		}
		names[raw.Name] = true

		entry, err := buildPrefetchEntry(pkg, agent, raw, where)
		if err != nil {
			return err
		}
		if err := checkPrefetchArgs(pkg, agent, raw, &entry, where, assignedBy, later); err != nil {
			return err
		}
		if err := checkPrefetchAssign(pkg, agent, raw, &entry, where, assigned); err != nil {
			return err
		}
		if err := deriveConfirmation(pkg, agent, &entry, where); err != nil {
			return err
		}
		agent.Prefetch = append(agent.Prefetch, entry)
	}
	return checkConfirmSteps(pkg, agent)
}

// refuseRetiredTimezone catches a package still carrying the old package-level
// key. The field survives on the spec struct only for this: a strict decoder
// would otherwise refuse it with "unknown field", which tells an author their
// file is wrong but not what replaced it.
func refuseRetiredTimezone(pkg *packagespec.Package) error {
	if strings.TrimSpace(pkg.Agent.Timezone) == "" {
		return nil
	}
	return fmt.Errorf("%s: timezone: is no longer a package-level key. The zone belongs on the clock entry that "+
		"reads it, so two entries can read two zones and so the key sits beside the reading it governs. Move it "+
		"under the prefetch entry: - name: today / clock: %s / timezone: %s",
		pkg.Location("agent.yaml", "timezone:"), PrefetchClockNow, strings.TrimSpace(pkg.Agent.Timezone))
}

// checkPrefetchName is refusal D2. A list has no duplicate-key protection of its
// own, so both halves of what a map gave for free are checked here.
func checkPrefetchName(pkg *packagespec.Package, raw packagespec.Prefetch, seen map[string]bool) error {
	if strings.TrimSpace(raw.Name) == "" {
		return fmt.Errorf("%s: a prefetch entry has no name:. Every entry in the list carries one, so a refusal "+
			"and a log line can say which entry they mean", prefetchLocation(pkg, raw))
	}
	if seen[raw.Name] {
		return fmt.Errorf("%s: two prefetch entries are both named %q. Names are how assign and every message refer "+
			"to an entry, so they cannot repeat", prefetchLocation(pkg, raw), raw.Name)
	}
	return nil
}

// buildPrefetchEntry resolves the one source key and refuses rules 1, 2, 7, 9,
// 10, 11 and 12.
func buildPrefetchEntry(pkg *packagespec.Package, agent *Agent, raw packagespec.Prefetch, where string) (Prefetch, error) {
	entry := Prefetch{Name: raw.Name}
	var present []string
	for _, key := range []struct {
		name  string
		value string
	}{{"clock", raw.Clock}, {"source", raw.Source}, {"tool", raw.Tool}} {
		if key.value != "" {
			present = append(present, key.name)
		}
	}
	switch len(present) {
	case 1:
	case 0:
		return entry, fmt.Errorf("%s: prefetch %q names none of clock:, source: and tool:. An entry reads exactly one: "+
			"clock: "+PrefetchClockNow+" for the date and time, source: <name> for a fact the call carries, or "+
			"tool: <name> for a lookup", where, raw.Name)
	default:
		return entry, fmt.Errorf("%s: prefetch %q names both %s: and %s:. An entry reads exactly one source; split it "+
			"into two entries", where, raw.Name, present[0], present[1])
	}

	if len(raw.Args) > 0 && raw.Tool == "" {
		reads := "reads the clock, which takes no arguments"
		if raw.Source != "" {
			reads = "reads a fact the call carries, which takes no arguments"
		}
		return entry, fmt.Errorf("%s: prefetch %q %s, so args: reaches nothing here. args: belongs to a tool: entry",
			where, raw.Name, reads)
	}

	// writes: answers "does running this unasked change anything". A clock and a
	// call fact run nothing, so the question has no meaning there and an author
	// who wrote it has misunderstood which entry they are looking at.
	if raw.Writes != nil && raw.Tool == "" {
		reads := "reads the clock"
		if raw.Source != "" {
			reads = "reads a fact the call carries"
		}
		return entry, fmt.Errorf("%s: prefetch %q %s, which runs no tool, so writes: answers nothing here. "+
			"writes: belongs to a tool: entry", where, raw.Name, reads)
	}

	// A zone on an entry that reads no clock reaches nothing, and an author who
	// wrote one meant it, so this is a refusal rather than a warning.
	zone := strings.TrimSpace(raw.Timezone)
	if zone != "" && raw.Clock == "" {
		return entry, fmt.Errorf("%s: prefetch %q sets timezone:, and only a clock entry reads a zone. Either give "+
			"this entry a clock: %s, or drop the zone", where, raw.Name, PrefetchClockNow)
	}

	switch {
	case raw.Clock != "":
		if raw.Clock != PrefetchClockNow {
			return entry, fmt.Errorf("%s: prefetch %q reads clock: %s, and the clock reads %s. Use clock: %s, and "+
				"name the part you want in assign:, one of: %s",
				where, raw.Name, raw.Clock, PrefetchClockNow, PrefetchClockNow, strings.Join(PrefetchClockFields, ", "))
		}
		if zone == "" {
			return entry, fmt.Errorf("%s: prefetch %q reads the clock and declares no timezone:. A container clock "+
				"is UTC, so the agent would name the wrong day for anybody who is not on it. Add timezone: to this "+
				"entry, for example timezone: Europe/Madrid", where, raw.Name)
		}
		if _, err := time.LoadLocation(zone); err != nil {
			return entry, fmt.Errorf("%s: prefetch %q sets timezone: %s, which is not an IANA zone name. Use the "+
				"zone for the place the agent books in, for example Europe/Madrid or America/New_York",
				where, raw.Name, zone)
		}
		entry.Clock = raw.Clock
		entry.Timezone = zone
	case raw.Source != "":
		source := VariableSource(raw.Source)
		switch {
		case IsSystemSource(source):
			entry.Source = source
		case source == VariableSourceConversation:
			return entry, fmt.Errorf("%s: prefetch %q reads source: conversation, and a conversation value is one the "+
				"model saves mid-call, so nothing holds it before the greeting", where, raw.Name)
		case source == VariableSourceCallStart:
			return entry, fmt.Errorf("%s: prefetch %q reads source: call_start, which arrives with the dispatch, so "+
				"declare it as source: call_start on the variable and it is already there", where, raw.Name)
		default:
			return entry, fmt.Errorf("%s: prefetch %q reads source: %s, which is not a fact a call carries. The facts "+
				"are: %s", where, raw.Name, raw.Source, strings.Join(callFactNames(), ", "))
		}
	default:
		tool, ok := agent.Tools[raw.Tool]
		if !ok {
			return entry, fmt.Errorf("%s: prefetch %q names tool %q, which this package does not declare. Declared "+
				"tools are: %s", where, raw.Name, raw.Tool, strings.Join(sortedKeys(agent.Tools), ", "))
		}
		if raw.Writes == nil {
			return entry, fmt.Errorf("%s: prefetch %q runs tool %q and does not say whether it writes. A prefetch "+
				"runs unasked on every call, wrong numbers and hang-ups included, so running it before the greeting "+
				"is a decision somebody has to make on purpose. Read %s, then add writes: false to this entry if it "+
				"changes nothing, or writes: true if it does",
				where, raw.Name, raw.Tool, filepath.ToSlash(filepath.Join("tools", raw.Tool+".yaml")))
		}
		entry.Writes = *raw.Writes
		switch tool.Execution {
		case ToolWebhook, ToolLocal:
		default:
			return entry, fmt.Errorf("%s: prefetch %q names tool %q, which is a %s tool. A prefetch runs webhook and "+
				"local tools, the two kinds whose request unmute builds itself", where, raw.Name, raw.Tool, tool.Execution)
		}
		entry.Tool = raw.Tool
	}
	return entry, nil
}

// checkPrefetchArgs resolves `args:` into Inputs and refuses rules 13 and 14.
func checkPrefetchArgs(pkg *packagespec.Package, agent *Agent, raw packagespec.Prefetch, entry *Prefetch, where string,
	assignedBy func(string) (string, bool), later map[string]string) error {
	for _, pair := range raw.Args {
		entry.Args = append(entry.Args, Pair{Key: pair.Key, Value: pair.Value})
		text, ok := pair.Value.(string)
		if !ok {
			continue
		}
		for _, ref := range TemplateRefs(text) {
			if _, declared := agent.Variables[ref]; !declared {
				return fmt.Errorf("%s: prefetch %q reads {{%s}}, which is not a declared variable. Declare it in the "+
					"variables: block of agent.yaml", where, raw.Name, ref)
			}
			// Rule 14. Reading a value an *earlier* entry assigned is the intended
			// shape and is why the block is ordered at all. Reading one a *later*
			// entry assigns is the same author's same intent written upside down,
			// so the refusal says which line to move rather than reordering for them.
			if _, earlier := assignedBy(ref); !earlier {
				if supplier, ok := later[ref]; ok {
					return fmt.Errorf("%s: prefetch %q reads {{%s}}, which prefetch %q assigns further down the list. "+
						"Entries resolve in the order you wrote them: move %q above %q",
						where, raw.Name, ref, supplier, supplier, raw.Name)
				}
			}
			if !slices.Contains(entry.Inputs, ref) {
				entry.Inputs = append(entry.Inputs, ref)
			}
		}
	}
	slices.Sort(entry.Inputs)
	return nil
}

// checkPrefetchAssign resolves `assign:` and refuses rules 3, 4, 5 and 6.
func checkPrefetchAssign(pkg *packagespec.Package, agent *Agent, raw packagespec.Prefetch, entry *Prefetch,
	where string, assigned map[string]string) error {
	if len(raw.Assign) == 0 {
		return fmt.Errorf("%s: prefetch %q assigns nothing, so it would resolve a value with nowhere to put it. Name "+
			"at least one variable in an assign: list under it", where, raw.Name)
	}
	fields := prefetchResultFields(agent, *entry)
	for _, pair := range raw.Assign {
		if _, declared := agent.Variables[pair.Key]; !declared {
			return fmt.Errorf("%s: prefetch %q assigns %s, which is not a declared variable. Declare it in the "+
				"variables: block of agent.yaml", where, raw.Name, pair.Key)
		}
		// Written inside the walk rather than after it, so one entry naming one
		// variable on two lines is caught too. Written after it, the map only
		// grew between entries, and the message for two entries reads "prefetch
		// today and prefetch today both assign booking_date", which sends the
		// reader looking for a second entry that does not exist.
		if owner, taken := assigned[pair.Key]; taken {
			if owner == raw.Name {
				return fmt.Errorf("%s: prefetch %q assigns %s twice. One value has one source: drop one of the two "+
					"lines", where, raw.Name, pair.Key)
			}
			return fmt.Errorf("%s: prefetch %q and prefetch %q both assign %s. One value has one source: drop it from "+
				"one of them", where, raw.Name, owner, pair.Key)
		}
		assigned[pair.Key] = raw.Name
		text, _ := pair.Value.(string)
		field, ok := strings.CutPrefix(text, "result.")
		if !ok || field == "" {
			return fmt.Errorf("%s: prefetch %q assigns %s from %q, and an assign value names one field of the "+
				"result. Write it as result.%s", where, raw.Name, pair.Key, text, firstOr(fields, "field"))
		}
		if !slices.Contains(fields, field) {
			return fmt.Errorf("%s: prefetch %q assigns %s from result.%s, and %s declares no field %q. Its fields "+
				"are: %s", where, raw.Name, pair.Key, field, prefetchSourceLabel(*entry), field, strings.Join(fields, ", "))
		}
		// A pre-fetch resolves one plain value: the clock formats its reading, a
		// call fact arrives off the wire as text, and a tool hands back one
		// field. A list or a shape is filled by the step that produces it, and
		// letting a pre-fetch write one compiled: the emitted step then called
		// _append_entry on a str and the call died mid-sentence. Named here
		// rather than left to assignableInto because its advice is to declare
		// the result field, and a pre-fetch has no step declaring one.
		want := agent.Variables[pair.Key]
		if want.Shape != nil && want.Shape.Shaped == "" && len(want.Shape.Literal) == 0 {
			return fmt.Errorf("%s: prefetch %q assigns %s, which is declared %s, and a pre-fetch resolves one plain "+
				"value before the greeting. Assign %s from the step that produces it, and leave the pre-fetch the "+
				"values a call already carries", where, raw.Name, pair.Key, want.Shape.String(), pair.Key)
		}
		// Everything a pre-fetch can fill is held to the same predicate a step's
		// assign: is held to, because the two write into the same variables and
		// a second predicate would drift from this one.
		if err := assignableInto(want.Shape, want.Type, prefetchResultField(agent, *entry, field)); err != nil {
			return fmt.Errorf("%s: prefetch %q assigns %s from result.%s, and %w",
				where, raw.Name, pair.Key, field, err)
		}
		entry.Assign = append(entry.Assign, Pair{Key: pair.Key, Value: text})
	}
	return nil
}

// deriveConfirmation is FR-026: a value looked up from an unconfirmed value is
// exactly as unconfirmed as that value was, so every name this entry assigns
// inherits the confirming step its inputs carry. Refusal 17 is two inputs
// carrying different steps, which no single inherited value could represent.
func deriveConfirmation(pkg *packagespec.Package, agent *Agent, entry *Prefetch, where string) error {
	var carriers, steps []string
	for _, name := range entry.Inputs {
		if step := agent.Variables[name].Confirm; step != "" {
			carriers = append(carriers, "{{"+name+"}}")
			if !slices.Contains(steps, step) {
				steps = append(steps, step)
			}
		}
	}
	switch len(steps) {
	case 0:
		// A directly-assigned variable still carries its own confirm:, which the
		// emitted block reads off the variable rather than off the entry.
	case 1:
		entry.Confirm = steps[0]
	default:
		return fmt.Errorf("%s: prefetch %q reads %s, which are confirmed by two different steps (%s). Split the "+
			"entry, so each result carries one confirming step",
			where, entry.Name, strings.Join(carriers, " and "), strings.Join(steps, " and "))
	}
	if entry.Confirm == "" {
		return nil
	}
	// The inherited step lands on the variable, because that is where every reader
	// of confirmation looks: the render restriction, the guard, and the emitted
	// block all read Variable.Confirm.
	for _, pair := range entry.Assign {
		variable := agent.Variables[pair.Key]
		if variable.Confirm == "" {
			variable.Confirm = entry.Confirm
			agent.Variables[pair.Key] = variable
		}
	}
	return nil
}

// checkConfirmSteps is refusal 15: a confirm: naming a step that does not run.
// Checked after the whole list, so an inherited step is checked too rather than
// only an authored one.
func checkConfirmSteps(pkg *packagespec.Package, agent *Agent) error {
	for _, name := range sortedKeys(agent.Variables) {
		step := agent.Variables[name].Confirm
		if step == "" {
			continue
		}
		if !runnableTask(agent, step) {
			return fmt.Errorf("%s: variable %q names confirm: %s, and no step by that name runs. Name a task an "+
				"agent runs, such as %s", pkg.Location("agent.yaml", name), name, step,
				firstOr(runnableTasks(agent), "verify_customer"))
		}
	}
	return nil
}

// prefetchSkipWarnings is warning W1: an entry reading a call fact the target's
// own route does not supply will skip on every call there, so the values it
// assigns keep their defaults.
//
// A warning and not a refusal, because skipping *is* the specified behaviour
// (FR-004): a package that reads a caller ID has to keep working on a route that
// supplies none, which is every Pipecat route today and the browser loop on both.
// What would be wrong is silence, so this names the target, the route, the entry,
// and every later entry that reads what this one would have assigned. Reading
// "the caller lookup does nothing here" off two separate warnings is worse than
// reading it off one.
func prefetchSkipWarnings(agent *Agent, resolved Target, row *TargetValidation) {
	if len(agent.Prefetch) == 0 || resolved.Transport == "" {
		return
	}
	key := targetcap.TelephonyKey{
		Provider: targetcap.Provider(resolved.Provider), Transport: resolved.Transport, Carrier: resolved.Carrier,
	}
	declared := packageDirections(resolved)
	for _, entry := range agent.Prefetch {
		if entry.Source == "" {
			continue
		}
		feature := targetcap.TelephonyFeature(targetcap.TelephonySourcePrefix + string(entry.Source))
		evidence := targetcap.ResolveTelephonyFeature(key, feature)
		assigned := make([]string, 0, len(entry.Assign))
		for _, pair := range entry.Assign {
			assigned = append(assigned, pair.Key)
		}

		// Gated is the one tag that means the route does not supply this at all:
		// ResolveTelephonyFeature returns it for a feature absent from the route's
		// map. Every feature a route *does* grant starts Provisional, pending a
		// credentialed smoke, so testing for Core here warned on every route
		// including the ones that supply the fact perfectly well.
		if evidence.Tag == targetcap.Gated {
			warning := fmt.Sprintf("prefetch %q reads source: %s, which route (%s, %s) does not supply, so this "+
				"entry is skipped on every call there and %s holds its default",
				entry.Name, entry.Source, resolved.Provider, resolved.Transport, strings.Join(assigned, ", "))
			row.Warnings = add(row.Warnings, warning+prefetchKnockOn(agent, assigned)+prefetchElsewhere(feature))
			continue
		}

		// Granted, and granted in every direction, which is what an empty
		// Directions has always meant. Nothing to say.
		if len(evidence.Directions) == 0 {
			continue
		}
		// Granted one way only. Silence when the package declares that way, or
		// declares both; a warning when it declares only the other, because there
		// the grant is real and still reaches no call this package will ever take.
		var unmet []string
		for _, direction := range declared {
			if !slices.Contains(evidence.Directions, targetcap.TelephonyFeature(direction)) {
				unmet = append(unmet, direction)
			}
		}
		if len(unmet) == 0 || len(unmet) < len(declared) {
			continue
		}
		supplies := make([]string, 0, len(evidence.Directions))
		for _, direction := range evidence.Directions {
			supplies = append(supplies, string(direction))
		}
		warning := fmt.Sprintf("prefetch %q reads source: %s, which route (%s, %s) supplies on an %s call only, "+
			"and this package declares %s. The entry is skipped on every call there and %s holds its default",
			entry.Name, entry.Source, resolved.Provider, resolved.Transport,
			strings.Join(supplies, " or "), strings.Join(unmet, " and "), strings.Join(assigned, ", "))
		row.Warnings = add(row.Warnings, warning+prefetchKnockOn(agent, assigned)+prefetchElsewhere(feature))
	}
}

// packageDirections names the call directions this package declares, read off
// the resolved plan's own evidence rows the way both emitters read them.
func packageDirections(resolved Target) []string {
	if resolved.Telephony == nil {
		return nil
	}
	var directions []string
	for _, evidence := range resolved.Telephony.Evidence {
		if evidence.Feature == "inbound" || evidence.Feature == "outbound" {
			directions = append(directions, evidence.Feature)
		}
	}
	slices.Sort(directions)
	return directions
}

// prefetchKnockOn is the second half of one warning: reading "the caller lookup
// does nothing here" off two separate warnings is worse than reading it off one.
func prefetchKnockOn(agent *Agent, assigned []string) string {
	knock := prefetchReaders(agent, assigned)
	if len(knock) == 0 {
		return ""
	}
	return fmt.Sprintf(". prefetch %s reads it, so that entry is skipped too", strings.Join(knock, " and "))
}

// prefetchElsewhere names the routes that do supply this fact, so an author has
// somewhere to go rather than only a no.
//
// Read off the table rather than written here. It was written here once, said
// call sources compile on the LiveKit routes, and would have gone on saying so
// after two Pipecat routes started supplying them.
func prefetchElsewhere(feature targetcap.TelephonyFeature) string {
	granting := targetcap.RoutesGranting(feature)
	if len(granting) == 0 {
		return ". No route supplies it today"
	}
	return fmt.Sprintf(". %s is supplied on %s", feature, strings.Join(granting, " and "))
}

// prefetchReaders names the entries whose inputs include any of these variables,
// which is what makes the skip cascade legible in one warning.
func prefetchReaders(agent *Agent, assigned []string) []string {
	var names []string
	for _, entry := range agent.Prefetch {
		for _, input := range entry.Inputs {
			if slices.Contains(assigned, input) {
				names = append(names, fmt.Sprintf("%q", entry.Name))
				break
			}
		}
	}
	return names
}

// prefetchResultFields is the result grammar: what each source produces, which
// is what an assign: right-hand side may name.
func prefetchResultFields(agent *Agent, entry Prefetch) []string {
	switch {
	case entry.Clock != "":
		return PrefetchClockFields
	case entry.Source != "":
		return []string{prefetchResultValue}
	default:
		properties, _ := agent.Tools[entry.Tool].Output["properties"].(map[string]any)
		return sortedKeys(properties)
	}
}

// prefetchResultField types one field of what an entry resolves, so the assign
// check can hold a pre-fetch to the same predicate a step's result is held to.
// Every pre-fetch source is plain text: the clock formats its reading, a call
// fact arrives off the wire as a string, and a tool's output: is JSON Schema,
// read here for the one property being assigned rather than passed whole (a
// whole schema is what assignableInto refuses as having no declared shape).
func prefetchResultField(agent *Agent, entry Prefetch, field string) ResultField {
	if entry.Tool == "" {
		return ResultField{Type: PrimitiveString}
	}
	properties, _ := agent.Tools[entry.Tool].Output["properties"].(map[string]any)
	property, _ := properties[field].(map[string]any)
	out := ResultField{Type: PrimitiveString}
	if word, ok := property["type"].(string); ok {
		switch PrimitiveType(word) {
		case PrimitiveInteger, PrimitiveNumber, PrimitiveBoolean:
			out.Type = PrimitiveType(word)
		}
	}
	// A closed set on the tool side assigns into a Literal declaring the same
	// set, which is the one structured type a pre-fetch can honestly fill.
	if values, err := stringSlice(property["enum"]); err == nil {
		out.Enum = values
	}
	return out
}

// prefetchSourceLabel names the source in a refusal the way the author wrote it,
// so "look_up_customer declares no field" points at the tool file and "the clock
// declares no field" does not send anybody looking for one.
func prefetchSourceLabel(entry Prefetch) string {
	switch {
	case entry.Clock != "":
		return "the clock"
	case entry.Source != "":
		return "a call fact"
	default:
		return entry.Tool + "'s output"
	}
}

// laterAssignments maps each assigned variable to the last entry assigning it,
// so rule 14 can name the supplier without a second walk. Built from the raw
// list because it has to see entries the walk has not reached yet.
func laterAssignments(entries []packagespec.Prefetch) map[string]string {
	out := map[string]string{}
	for _, entry := range entries {
		for _, pair := range entry.Assign {
			out[pair.Key] = entry.Name
		}
	}
	return out
}

// callFactNames lists the eight runtime-owned sources, sorted, for the refusal
// that names what a call actually carries.
func callFactNames() []string {
	names := make([]string, 0, len(systemSources))
	for _, source := range systemSources {
		names = append(names, string(source))
	}
	slices.Sort(names)
	return names
}

// runnableTask reports whether a name is a task some agent runs, which is what
// makes it a step that can confirm anything.
func runnableTask(agent *Agent, name string) bool {
	return slices.Contains(runnableTasks(agent), name)
}

func runnableTasks(agent *Agent) []string {
	var names []string
	for _, control := range agent.Controls {
		if delegate, ok := control.(*Delegate); ok && delegate.Task != "" {
			names = append(names, delegate.Task)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// prefetchAssigns reports whether any entry assigns this variable, which is the
// third way a variable can hold a value before the first spoken word.
func prefetchAssigns(agent *Agent, name string) bool {
	for _, entry := range agent.Prefetch {
		for _, pair := range entry.Assign {
			if pair.Key == name {
				return true
			}
		}
	}
	return false
}

// prefetchLocation finds the entry's own line. A list item has no key to search
// for, so the name is the token, and an entry with no name falls back to the
// block itself, which is the only honest answer available.
func prefetchLocation(pkg *packagespec.Package, raw packagespec.Prefetch) string {
	if raw.Name != "" {
		return pkg.Location("agent.yaml", "name: "+raw.Name)
	}
	return pkg.Location("agent.yaml", "prefetch:")
}

func firstOr(values []string, fallback string) string {
	if len(values) > 0 {
		return values[0]
	}
	return fallback
}

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

// PrefetchClockDate is the one clock reading there is. A time-of-day value has
// no reader anywhere in the spec, so adding one now would be a field nobody
// fills (plan.md, Complexity Tracking).
const PrefetchClockDate = "date"

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
	if err := buildTimezone(pkg, agent); err != nil {
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
		for _, pair := range entry.Assign {
			assigned[pair.Key] = entry.Name
		}
		agent.Prefetch = append(agent.Prefetch, entry)
	}
	return checkConfirmSteps(pkg, agent)
}

// buildTimezone validates `timezone:` (rule 8). Validated whether or not a clock
// is pre-fetched: a zone that is not a zone is a typo either way, and refusing it
// only when something reads it would let one sit in a file until the day somebody
// adds a clock entry.
func buildTimezone(pkg *packagespec.Package, agent *Agent) error {
	zone := strings.TrimSpace(pkg.Agent.Timezone)
	if zone == "" {
		return nil
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return fmt.Errorf("%s: timezone %q is not an IANA zone name. Use the zone for the place the agent books in, "+
			"for example Europe/Madrid or America/New_York", pkg.Location("agent.yaml", "timezone:"), zone)
	}
	agent.Timezone = zone
	return nil
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
			"clock: date for the current date, source: <name> for a fact the call carries, or tool: <name> for a "+
			"read-only lookup", where, raw.Name)
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

	switch {
	case raw.Clock != "":
		if raw.Clock != PrefetchClockDate {
			return entry, fmt.Errorf("%s: prefetch %q reads clock: %s, and the clock reads %s. Use clock: %s",
				where, raw.Name, raw.Clock, PrefetchClockDate, PrefetchClockDate)
		}
		if agent.Timezone == "" {
			return entry, fmt.Errorf("%s: prefetch %q reads the clock, and this package declares no timezone:. A "+
				"container clock is UTC, so the agent would name the wrong day for anybody who is not on it. Add "+
				"timezone: to agent.yaml, for example timezone: Europe/Madrid", where, raw.Name)
		}
		entry.Clock = raw.Clock
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
		if !tool.ReadOnly {
			return entry, fmt.Errorf("%s: prefetch %q names tool %q, which has not declared read_only: true. A "+
				"prefetch runs unasked on every call, so a tool that writes would write on every call, wrong numbers "+
				"included. Add read_only: true to %s if it writes nothing, or point this entry at a tool that reads",
				where, raw.Name, raw.Tool, filepath.ToSlash(filepath.Join("tools", raw.Tool+".yaml")))
		}
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
		if owner, taken := assigned[pair.Key]; taken {
			return fmt.Errorf("%s: prefetch %q and prefetch %q both assign %s. One value has one source: drop it from "+
				"one of them", where, raw.Name, owner, pair.Key)
		}
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

// orphanReadOnlyWarnings is warning W3. A declaration that reaches nothing is
// the existing pattern for a turn binding carrying a field no target reads, so
// this warns rather than refusing: the author has told the truth about the tool,
// they have just not pointed a prefetch at it yet.
//
// Lives here rather than in Build because Build has no warning channel: it
// returns an agent or an error, and a warning is neither.
func orphanReadOnlyWarnings(agent *Agent) []string {
	used := map[string]bool{}
	for _, entry := range agent.Prefetch {
		if entry.Tool != "" {
			used[entry.Tool] = true
		}
	}
	var warnings []string
	for _, name := range sortedKeys(agent.Tools) {
		if agent.Tools[name].ReadOnly && !used[name] {
			warnings = append(warnings, fmt.Sprintf(
				"tool %q declares read_only: true, and no prefetch names it, so the declaration reaches nothing", name))
		}
	}
	return warnings
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
	for _, entry := range agent.Prefetch {
		if entry.Source == "" {
			continue
		}
		feature := targetcap.TelephonyFeature(targetcap.TelephonySourcePrefix + string(entry.Source))
		// Gated is the one tag that means the route does not supply this at all:
		// ResolveTelephonyFeature returns it for a feature absent from the route's
		// map. Every feature a route *does* grant starts Provisional, pending a
		// credentialed smoke, so testing for Core here warned on every route
		// including the ones that supply the fact perfectly well.
		if targetcap.ResolveTelephonyFeature(key, feature).Tag != targetcap.Gated {
			continue
		}
		assigned := make([]string, 0, len(entry.Assign))
		for _, pair := range entry.Assign {
			assigned = append(assigned, pair.Key)
		}
		warning := fmt.Sprintf("prefetch %q reads source: %s, which route (%s, %s) does not supply, so this entry is "+
			"skipped on every call there and %s holds its default",
			entry.Name, entry.Source, resolved.Provider, resolved.Transport, strings.Join(assigned, ", "))
		if knock := prefetchReaders(agent, assigned); len(knock) > 0 {
			warning += fmt.Sprintf(". prefetch %s reads it, so that entry is skipped too", strings.Join(knock, " and "))
		}
		warning += ". Call sources compile on (livekit, sip) trunks and on (livekit, connector)"
		row.Warnings = add(row.Warnings, warning)
	}
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
		return []string{PrefetchClockDate}
	case entry.Source != "":
		return []string{prefetchResultValue}
	default:
		properties, _ := agent.Tools[entry.Tool].Output["properties"].(map[string]any)
		return sortedKeys(properties)
	}
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

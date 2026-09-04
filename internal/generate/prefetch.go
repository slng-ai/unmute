package generate

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/slng-ai/unmute/internal/ir"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// The pre-fetch block, and the one place its words are written.
//
// Facts knowable before the greeting are resolved here, once per call, so the
// model is never asked to discover them. Following guard.go: one file owns the
// emitted text, both drivers render what it produces, and the two targets agree
// by construction rather than by a test that has to notice they stopped
// agreeing. The test still exists, because construction can be undone.

// PrefetchBudget bounds the whole block, in seconds.
//
// Not a knob, and not per entry. Nobody has a second number to write, and a
// lookup slower than this is broken rather than slow: two seconds is already
// longer than a caller will wait for a greeting. It bounds the block rather than
// each entry so the ceiling is the thing the author can reason about, which is
// how long the greeting can be delayed in the worst case.
const PrefetchBudget = 2.0

// PrefetchRouterValueMax bounds one pre-fetched value where it is sent to the
// SLNG Context Router, in characters.
//
// This cannot be a compile-time refusal: the length is only known while the call
// runs. What it must not be is unbounded. A value that quietly outgrows what the
// router accepts takes the prompt caching with it, and the whole reason a
// pre-fetched value travels beside the prompt rather than inside it is to leave
// that caching alone. Shortening one is logged with its name, because a silent
// truncation of a customer's name is a worse outcome than a long log line.
const PrefetchRouterValueMax = 512

// PrefetchRequest is how one entry's tool call is made. The driver fills it from
// the same helpers its mid-call tool path uses (urlExpr, requestBody,
// callKwargs, loweredAuth) with its own state expression, so a pre-fetched
// webhook and a mid-call webhook cannot drift in the parts that actually drift:
// URL assembly, auth headers and the injected values.
//
// ponytail: the emitted request lines themselves live below rather than in a
// template partial shared with the tool method. Both drivers already spell a
// webhook the same way (httpx.AsyncClient().post) and a local handler the same
// way (tools.<name>.<name>), so there are six lines here and they are asserted.
// Factor them into a shared partial when a third execution kind becomes
// pre-fetchable and the shapes stop being identical.
type PrefetchRequest struct {
	Local      bool
	Name       string // the tool name, which is also its module and function name
	URLExpr    string
	AuthExpr   string
	BodyExpr   string
	CallKwargs string
}

// PrefetchBlock is what a driver renders: the Python, plus what the template
// needs to know without re-deriving it.
type PrefetchBlock struct {
	Source      string
	NeedsClock  bool
	NeedsAsync  bool
	NeedsLocal  bool
	NeedsSeed   bool
	NeedsNumber bool
	Unconfirmed bool
}

// PrefetchWrite is one entry whose author said that running it changes
// something. Reported, never warned about.
//
// A warning would fire forever: `writes:` is required on every tool entry, so a
// package that legitimately writes would print the same line on every compile
// until somebody stopped reading the warnings. A new line on stdout has to name
// something the reader did or has to do, and telling an author about a key they
// just typed names neither. So it goes where the rest of what the compiler acted
// on goes: compile-report.json, and the runbook beside it.
type PrefetchWrite struct {
	Entry string `json:"entry"`
	Tool  string `json:"tool"`
}

// PrefetchWrites names them, in authored order.
func PrefetchWrites(agent *ir.Agent) []PrefetchWrite {
	var writes []PrefetchWrite
	for _, entry := range agent.Prefetch {
		if entry.Writes {
			writes = append(writes, PrefetchWrite{Entry: entry.Name, Tool: entry.Tool})
		}
	}
	return writes
}

// LocalCallFactsEnv carries the facts `unmute dev --source` seeds, as a JSON
// object, so the browser loop exercises the same code a phone call does.
//
// A second name beside UNMUTE_CALL_START, not a widening of it, and that is the
// whole point: the dispatch payload and the carrier's own facts are different
// things arriving through different doors, and seeding a variable directly would
// skip the pre-fetch, mark nothing as awaiting confirmation, and let a local run
// pass a path a real call fails.
const LocalCallFactsEnv = "UNMUTE_CALL_FACTS"

// Prefetch returns the block for a package, and whether the package needs one.
//
// A package that declares no `prefetch:` gets no block and no behaviour change:
// its emitted output is byte-for-byte what it was before this existed. That is
// why this returns a flag rather than always emitting a helper nothing calls,
// exactly as PrerequisiteGuard does.
func Prefetch(agent *ir.Agent, stateExpr string, request func(entry ir.Prefetch) PrefetchRequest) (PrefetchBlock, bool) {
	if len(agent.Prefetch) == 0 {
		return PrefetchBlock{}, false
	}
	block := PrefetchBlock{}
	var b strings.Builder
	b.WriteString(`# Pre-fetch, generated by internal/generate/prefetch.go.
#
# Facts that are knowable before the greeting are resolved here, once per call,
# so the model is never asked to discover them. An entry whose inputs are empty
# is skipped and the values keep their declared defaults, which is what makes a
# package that pre-fetches a carrier fact still work on a route that supplies
# none. Nothing here can fail a call.
_PREFETCH_BUDGET_S = ` + strconv.FormatFloat(PrefetchBudget, 'f', 1, 64) + `
_PREFETCH_VALUE_MAX = ` + strconv.Itoa(PrefetchRouterValueMax) + `
`)
	for _, entry := range agent.Prefetch {
		if entry.Clock != "" {
			block.NeedsClock = true
		}
		if entry.Tool != "" {
			block.NeedsAsync = true
			if agent.Tools[entry.Tool].Execution == ir.ToolLocal {
				block.NeedsLocal = true
			}
		}
		if entry.Source != "" {
			block.NeedsSeed = true
			if prefetchSourceIsANumber(entry.Source) {
				block.NeedsNumber = true
			}
		}
		if prefetchConfirms(agent, entry) {
			block.Unconfirmed = true
		}
	}
	if block.NeedsClock {
		// The weekday name, spelled out rather than read from strftime("%A"), so
		// the word cannot change with the container's locale. Module level because
		// every clock entry shares it.
		b.WriteString("_PREFETCH_DAYS = (\n")
		for _, day := range []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"} {
			b.WriteString("    " + pyQuote(day) + ",\n")
		}
		b.WriteString(")\n")
	}
	if block.NeedsNumber {
		b.WriteString("# Keypad spellings of the words a carrier substitutes when a number is\n" +
			"# withheld. They pass the shape check below because they are the right shape:\n" +
			"# digits, and about as many as a phone number has.\n")
		b.WriteString("_PREFETCH_WITHHELD = (\n")
		for _, word := range prefetchWithheldWords {
			b.WriteString("    " + pyQuote(prefetchWithheldDigits(word)) + ",  # " + word + "\n")
		}
		b.WriteString(")\n")
		b.WriteString(prefetchNumberHelper)
	}
	if block.NeedsSeed {
		b.WriteString(`

def _prefetch_call_facts(call_context):
    """The call's own facts, with the local seed filling only what the carrier did not.

    ` + LocalCallFactsEnv + ` is what ` + "`unmute dev --source name=value`" + ` sets. The
    carrier wins wherever it supplied a value: a seed stands in for a fact it
    could not supply, and never overrides one it did, so a stale value in a .env
    cannot quietly reshape a real call.

    Merging here rather than at each call site is deliberate. This driver starts a
    session from four different places, and a merge written four times is a merge
    that disagrees with itself in one of them.
    """
    facts = dict(call_context or {})
    seeded = os.environ.get(` + pyQuote(LocalCallFactsEnv) + `)
    if not seeded:
        return facts
    try:
        values = json.loads(seeded)
        for name in values:
            if not facts.get(name):
                facts[name] = values[name]
    except (ValueError, TypeError):
        logger.warning(
            ` + pyQuote(LocalCallFactsEnv+" is not a JSON object of call facts; ignoring it") + `
        )
    return facts
`)
	}
	b.WriteString(`

def _prefetch_bounded(name, value):
    """Bound one pre-fetched value, and say so when it is shortened.

    The length is only knowable here, at run time, so this cannot be a
    compile-time refusal. Silence is the one thing it must not be.
    """
    text = "" if value is None else str(value)
    if len(text) <= _PREFETCH_VALUE_MAX:
        return text
    logger.warning(
        f"prefetch: {name} was {len(text)} characters and is cut to "
        f"{_PREFETCH_VALUE_MAX}; a value this long stops the prompt being cached"
    )
    return text[:_PREFETCH_VALUE_MAX]


async def _prefetch(state, call_context) -> None:
`)
	if block.NeedsSeed {
		b.WriteString("    call_context = _prefetch_call_facts(call_context)\n")
	}
	if block.Unconfirmed {
		b.WriteString(`    # Every value awaiting the caller's agreement. The prerequisite guard reads
    # this set, so an unconfirmed value satisfies no step, and each generated
    # assign write discards its own name as the caller settles it.
    state._unconfirmed = set()
`)
	}
	b.WriteString(`    # Entries run in the order agent.yaml lists them. Nothing here is sorted or
    # reordered: an entry reading a value a later entry assigns was refused at
    # compile time, so by here the order is known good.
`)
	for _, entry := range agent.Prefetch {
		b.WriteString("\n")
		switch {
		case entry.Clock != "":
			writePrefetchClock(&b, agent, entry)
		case entry.Source != "":
			writePrefetchSource(&b, agent, entry)
		default:
			writePrefetchTool(&b, agent, entry, stateExpr, request(entry))
		}
	}
	block.Source = b.String()
	return block, true
}

// writePrefetchClock emits the clock branch. No skip arm: a clock always reads,
// so there is no empty input that could make this entry do nothing, and no
// try/except either, because reading a validated zone raises nothing.
//
// One reading into `_now`, and every field of this entry derived from it. Read
// per field instead, an entry assigning date and time could straddle a second,
// and one assigning date and day_of_week could straddle a midnight and disagree
// with itself about which day it is.
func writePrefetchClock(b *strings.Builder, agent *ir.Agent, entry ir.Prefetch) {
	fmt.Fprintf(b, "    # %s: clock %s in %s -> %s\n",
		entry.Name, entry.Clock, entry.Timezone, prefetchAssignedNames(entry))
	fmt.Fprintf(b, "    _now = datetime.now(ZoneInfo(%s))\n", pyQuote(entry.Timezone))
	var reported []string
	for _, pair := range entry.Assign {
		// Always a string, and comma-ok rather than a bare assertion because a
		// compiler that panics on a package is worse than one that emits Python
		// which fails at import: checkPrefetchAssign builds every Assign value
		// from the authored `result.<field>` text and refuses anything else.
		text, _ := pair.Value.(string)
		field := strings.TrimPrefix(text, "result.")
		fmt.Fprintf(b, "    state.%s = _prefetch_bounded(%s, %s)\n",
			pair.Key, pyQuote(pair.Key), prefetchClockExpr(field, entry.Timezone))
		writeUnconfirmed(b, agent, entry, pair.Key)
		reported = append(reported, fmt.Sprintf("%s={state.%s}", pair.Key, pair.Key))
	}
	// An f-string, not a %s placeholder and not .format(), because this block is
	// rendered into both drivers and they log through different libraries: LiveKit
	// gets stdlib logging, which interpolates %s, and Pipecat gets loguru, which
	// interpolates {}. Passing either style to the other prints the placeholder
	// literally, which is exactly what a real Pipecat smoke run showed. A string
	// already formatted by Python is the one form both accept.
	//
	// Every variable the entry assigned, not just the first. A three-field entry
	// logging one of them is a trace that cannot answer whether the other two
	// landed.
	fmt.Fprintf(b, "    logger.info(f\"prefetch %s: resolved %s\")\n", entry.Name, strings.Join(reported, ", "))
}

// prefetchClockExpr renders one clock field off the entry's single `_now`.
//
// Every name in ir.PrefetchClockFields has an arm here, and a name with none
// would emit `state.x = _prefetch_bounded("x", )`, which is a syntax error
// rather than a silent wrong answer. TestPrefetchEmitsEveryClockField is what
// holds the two lists together.
func prefetchClockExpr(field, zone string) string {
	switch field {
	case "date":
		return "_now.date().isoformat()"
	case "time":
		return `_now.strftime("%H:%M")`
	case "datetime":
		return `_now.isoformat(timespec="minutes")`
	case "day_of_week":
		// A module-level tuple, not strftime("%A"), because strftime reads the
		// container's locale: the same image on a differently-configured host
		// would say "viernes" into a prompt written in English.
		return "_PREFETCH_DAYS[_now.weekday()]"
	case "year":
		return "str(_now.year)"
	case "timezone":
		// What the author wrote, not _now.tzname(). tzname() gives the abbreviation
		// in force at that instant, so the same package would answer CET in January
		// and CEST in July, and neither is the thing the author named.
		return pyQuote(zone)
	}
	return ""
}

// writePrefetchSource emits the call-fact branch: read the named fact out of the
// call context, skip with a log line when the route supplied none.
func writePrefetchSource(b *strings.Builder, agent *ir.Agent, entry ir.Prefetch) {
	fmt.Fprintf(b, "    # %s: source %s -> %s\n", entry.Name, entry.Source, prefetchAssignedNames(entry))
	fmt.Fprintf(b, "    _value = (call_context or {}).get(%s) or \"\"\n", pyQuote(string(entry.Source)))
	if prefetchSourceIsANumber(entry.Source) {
		fmt.Fprintf(b, "    _value = _prefetch_number(%s, _value)\n", pyQuote(string(entry.Source)))
	}
	b.WriteString("    if not _value:\n")
	fmt.Fprintf(b, "        logger.info(\"prefetch %s: skipped, the call carries no %s\")\n", entry.Name, entry.Source)
	b.WriteString("    else:\n")
	for _, pair := range entry.Assign {
		fmt.Fprintf(b, "        state.%s = _prefetch_bounded(%s, _value)\n", pair.Key, pyQuote(pair.Key))
		writeUnconfirmedIndented(b, agent, entry, pair.Key, "        ")
	}
	fmt.Fprintf(b, "        logger.info(\"prefetch %s: resolved %s%s\")\n",
		entry.Name, prefetchAssignedNames(entry), prefetchConfirmSuffix(agent, entry))
}

// prefetchSourceIsANumber reports whether a call fact is a phone number, and so
// whether a withheld-caller-ID placeholder could be hiding in it.
//
// Only these two. A `call_id` of "unknown" is a real call id, and refusing it
// would throw away a fact the call genuinely carries.
func prefetchSourceIsANumber(source ir.VariableSource) bool {
	return source == ir.VariableSourceFromNumber || source == ir.VariableSourceToNumber
}

// prefetchNumberHelper is the emitted shape check. Written once, module level,
// because both numbers use it.
//
// A withheld caller ID does not arrive as nothing. Twilio's own policy is that a
// call whose caller ID was withheld arrives with `From` set to the word
// `anonymous`; where an upstream carrier sends a word such as ANONYMOUS or
// RESTRICTED, Twilio converts it to keypad digits, which arrive looking exactly
// like a real number. And some calls simply arrive empty. Treating only the
// empty case as absent means the other two reach a prompt, and the agent reads
// "anonymous" back to the caller as their phone number.
//
// So the rule is the shape, not a list: a value is a number when it is a number.
// The short list below is the digit form the shape check cannot see, and it is
// deliberately short: every entry is a placeholder a carrier substitutes, not a
// number anybody was ever assigned.
const prefetchNumberHelper = `

def _prefetch_number(name, value):
    """A phone number, or "" when the carrier sent a placeholder instead.

    A withheld caller ID arrives three ways: empty, as the word "anonymous", or
    as the keypad digits of a word an upstream carrier sent, which is shaped like
    a real number. All three mean the same thing and all three must skip the
    entry, because a value that reaches a prompt is a value the agent reads back
    to the caller as their own number.
    """
    text = ("" if value is None else str(value)).strip()
    # E.164: a plus, then 8 to 15 digits. Anything else was not a number, which
    # covers "anonymous", "unavailable", "restricted" and an empty string alike.
    #
    # Spelled out rather than a regex so this block needs no import of its own,
    # and isascii() beside isdigit() because isdigit() is true of digits in other
    # scripts, which no carrier sends and no downstream tool would accept.
    digits = text[1:] if text.startswith("+") else text
    if not (text.startswith("+") and digits.isascii() and digits.isdigit() and 8 <= len(digits) <= 15):
        if text:
            logger.info(f"prefetch: {name} was not a phone number, treating it as absent")
        return ""
    # The digit form. Equal, or the start of a longer spelling, which is what a
    # word passed on truncated looks like.
    #
    # Only that direction. Matching a value that *begins* with a spelling would
    # throw away real numbers: "unknown" spells 8656696, and +86 5669 6xxx is an
    # ordinary Chinese mobile. A word shorter than eight digits therefore never
    # matches here, and does not need to: the shape check above already refused
    # it for being too short to be a number.
    for spelled in _PREFETCH_WITHHELD:
        if spelled.startswith(digits):
            logger.info(f"prefetch: {name} is a withheld-caller-ID placeholder, treating it as absent")
            return ""
    return text
`

// prefetchWithheldWords are the words a carrier sends in place of a number it is
// not passing on.
//
// Kept as words and keypad-mapped in the emitted module rather than written here
// as digit strings, for one reason: nobody has verified what digits Twilio
// actually sends. Twilio documents the behaviour (an upstream carrier passes a
// word, Twilio converts it to digits and uses those as `From`) and documents no
// table of results. A hardcoded list of digits would look verified and would
// have been guessed. The keypad map is a fact, and mapping the words through it
// in the open lets a reader check the arithmetic.
var prefetchWithheldWords = []string{"anonymous", "restricted", "unknown", "unavailable", "private"}

// prefetchKeypad is the ITU E.161 letter-to-digit map every telephone keypad
// carries. Not a guess: it is printed on the handset.
const prefetchKeypad = "22233344455566677778889999"

// prefetchWithheldDigits spells one word the way a keypad would.
func prefetchWithheldDigits(word string) string {
	var b strings.Builder
	for _, letter := range word {
		if letter < 'a' || letter > 'z' {
			continue
		}
		b.WriteByte(prefetchKeypad[letter-'a'])
	}
	return b.String()
}

// writePrefetchTool emits the lookup branch: skip when any input is empty, run
// inside the budget, and swallow both failure modes. Neither except arm
// re-raises: a pre-fetch that fails is a call that greets on time with the
// values at their defaults, which is exactly what a route supplying no caller ID
// already does.
func writePrefetchTool(b *strings.Builder, agent *ir.Agent, entry ir.Prefetch, stateExpr string, request PrefetchRequest) {
	fmt.Fprintf(b, "    # %s: %s(%s) -> %s\n", entry.Name, entry.Tool,
		strings.Join(prefetchArgSummary(entry), ", "), prefetchAssignedNames(entry))
	// An entry with no inputs cannot skip, so it gets no skip arm and no `else`:
	// the body sits at the entry's own indentation. An entry with inputs is
	// skipped when any of them is empty, which is the whole degrade path.
	indent, body := "    ", "        "
	if len(entry.Inputs) > 0 {
		guards := make([]string, 0, len(entry.Inputs))
		for _, input := range entry.Inputs {
			guards = append(guards, "not state."+input)
		}
		fmt.Fprintf(b, "    if %s:\n", strings.Join(guards, " or "))
		fmt.Fprintf(b, "        logger.info(\"prefetch %s: skipped, %s is empty\")\n",
			entry.Name, strings.Join(entry.Inputs, " or "))
		b.WriteString("    else:\n")
		indent, body = "        ", "            "
	}
	fmt.Fprintf(b, "%stry:\n", indent)
	fmt.Fprintf(b, "%sasync with asyncio.timeout(_PREFETCH_BUDGET_S):\n", body)
	if request.Local {
		fmt.Fprintf(b, "%s    result = tools.%s.%s(%s)\n", body, request.Name, request.Name, request.CallKwargs)
		fmt.Fprintf(b, "%s    if inspect.isawaitable(result):\n", body)
		fmt.Fprintf(b, "%s        result = await result\n", body)
	} else {
		fmt.Fprintf(b, "%s    async with httpx.AsyncClient() as client:\n", body)
		auth := ""
		if request.AuthExpr != "" {
			auth = "headers=" + request.AuthExpr + ", "
		}
		fmt.Fprintf(b, "%s        response = await client.post(%s, %sjson=%s, timeout=_PREFETCH_BUDGET_S)\n",
			body, request.URLExpr, auth, request.BodyExpr)
		fmt.Fprintf(b, "%s        response.raise_for_status()\n", body)
		fmt.Fprintf(b, "%s        result = response.json()\n", body)
	}
	for _, pair := range entry.Assign {
		field, _ := strings.CutPrefix(pair.Value.(string), "result.")
		fmt.Fprintf(b, "%sstate.%s = _prefetch_bounded(%s, (result or {}).get(%s))\n",
			body, pair.Key, pyQuote(pair.Key), pyQuote(field))
		writeUnconfirmedIndented(b, agent, entry, pair.Key, body)
	}
	fmt.Fprintf(b, "%slogger.info(\"prefetch %s: resolved %s%s\")\n",
		body, entry.Name, prefetchAssignedNames(entry), prefetchConfirmSuffix(agent, entry))
	fmt.Fprintf(b, "%sexcept TimeoutError:\n", indent)
	fmt.Fprintf(b, "%slogger.warning(\n", body)
	fmt.Fprintf(b, "%s    f\"prefetch %s: gave up after {_PREFETCH_BUDGET_S}s; %s\"\n",
		body, entry.Name, prefetchKeepsDefault(entry))
	fmt.Fprintf(b, "%s)\n", body)
	fmt.Fprintf(b, "%sexcept Exception:\n", indent)
	fmt.Fprintf(b, "%slogger.exception(\"prefetch %s: failed; %s\")\n",
		body, entry.Name, prefetchKeepsDefault(entry))
	_ = stateExpr
}

// writeUnconfirmed marks one value as awaiting the caller's agreement.
func writeUnconfirmed(b *strings.Builder, agent *ir.Agent, entry ir.Prefetch, name string) {
	writeUnconfirmedIndented(b, agent, entry, name, "    ")
}

func writeUnconfirmedIndented(b *strings.Builder, agent *ir.Agent, entry ir.Prefetch, name, indent string) {
	if agent.Variables[name].Confirm == "" {
		return
	}
	fmt.Fprintf(b, "%sstate._unconfirmed.add(%s)\n", indent, pyQuote(name))
	_ = entry
}

// prefetchConfirms reports whether any value this entry assigns needs the
// caller's agreement, which is what decides whether the block emits the set.
func prefetchConfirms(agent *ir.Agent, entry ir.Prefetch) bool {
	for _, pair := range entry.Assign {
		if agent.Variables[pair.Key].Confirm != "" {
			return true
		}
	}
	return false
}

// prefetchConfirmSuffix is the log line's tail. A value the caller still has to
// agree to is a different outcome from one that is settled, and the log says so:
// reading a trace back is how this feature is verified.
func prefetchConfirmSuffix(agent *ir.Agent, entry ir.Prefetch) string {
	if prefetchConfirms(agent, entry) {
		return ", awaiting confirmation"
	}
	return ""
}

// prefetchAssignedNames lists what an entry lands, for a comment and a log line.
func prefetchAssignedNames(entry ir.Prefetch) string {
	names := make([]string, 0, len(entry.Assign))
	for _, pair := range entry.Assign {
		names = append(names, pair.Key)
	}
	return strings.Join(names, ", ")
}

// prefetchArgSummary is the entry's arguments as the author wrote them, for the
// comment above the call. The author's own text, so a reader can match the
// emitted line to the line in agent.yaml.
func prefetchArgSummary(entry ir.Prefetch) []string {
	out := make([]string, 0, len(entry.Args))
	for _, pair := range entry.Args {
		out = append(out, fmt.Sprintf("%s=%v", pair.Key, pair.Value))
	}
	return out
}

// PrefetchRunbook is the emitted README section, written once so both targets'
// runbooks say the same thing.
//
// The runbook is the file somebody reads at three in the morning, and the
// questions it has to answer are the ones the code cannot: which zone the clock
// was read in, how long the greeting can be delayed, what happens on a route that
// carries no caller ID, and which day a call that crosses midnight keeps using.
// That last one is the trap: nothing about the emitted code hints that the answer
// is "the day it started on".
func PrefetchRunbook(agent *ir.Agent, resolved ir.Target) (string, bool) {
	if len(agent.Prefetch) == 0 {
		return "", false
	}
	var b strings.Builder
	b.WriteString("\n## Facts resolved before the greeting\n\n")
	b.WriteString("This agent resolves some values once per call, before it speaks, so the model\n" +
		"never spends a turn discovering them. Each entry below runs in the order\n" +
		"`agent.yaml` lists it.\n\n")
	b.WriteString("| Entry | Reads | Lands in |\n|---|---|---|\n")
	for _, entry := range agent.Prefetch {
		reads := ""
		switch {
		case entry.Clock != "":
			reads = "the clock, in `" + entry.Timezone + "`"
		case entry.Source != "":
			reads = "the call's own `" + string(entry.Source) + "`" + prefetchDirectionNote(resolved, entry.Source)
		default:
			reads = "`" + entry.Tool + "`, a lookup that writes nothing"
			if entry.Writes {
				reads = "`" + entry.Tool + "`, **which writes**"
			}
		}
		fmt.Fprintf(&b, "| `%s` | %s | `%s` |\n", entry.Name, reads, prefetchAssignedNames(entry))
	}
	b.WriteString("\n**The budget.** The whole block has " +
		strconv.FormatFloat(PrefetchBudget, 'f', 1, 64) +
		" seconds, which you will find in the\ncode as `_PREFETCH_BUDGET_S`. " +
		"Past that it gives up, the\nvalues keep their declared defaults, and the call greets on time. " +
		"Nothing here can\nfail a call: a lookup that times out or raises is logged and stepped over.\n")
	b.WriteString("\n**An entry can do nothing, and that is normal.** An entry whose inputs are empty\n" +
		"is skipped. A call that carries no caller ID skips every entry that reads one,\n" +
		"and every entry downstream of those, so the agent behaves exactly as it would\n" +
		"have without this block at all. Compiling for a target whose route supplies no\n" +
		"call facts prints a warning naming each entry it will skip there.\n")
	if numbers := prefetchNumberEntries(agent); len(numbers) > 0 {
		b.WriteString("\n**A caller's number is best effort, even where the route supplies one.** " +
			strings.Join(numbers, ", ") + " can arrive empty on a\nperfectly ordinary call. " +
			"A caller withholding their number is ordinary rather than\nexceptional, and it does not arrive as " +
			"nothing: it arrives as the word `anonymous`,\nor as digits that look like a number and are not one. " +
			"All three are treated as\nabsent, the entry is skipped, and the log says so.\n\n" +
			"The other way it arrives empty is configuration. On a route where the number\n" +
			"rides markup you pasted into your carrier's console, a console object written\n" +
			"before this agent needed the number simply does not carry it. Nothing here\n" +
			"checks that for you: reading your console back would need carrier credentials\n" +
			"this build does not ask for. The telephony section of this file says exactly\n" +
			"what to paste.\n")
	}
	if writes := PrefetchWrites(agent); len(writes) > 0 {
		b.WriteString("\n**Something here writes on every call.** ")
		names := make([]string, 0, len(writes))
		for _, write := range writes {
			names = append(names, "`"+write.Entry+"` runs `"+write.Tool+"`")
		}
		b.WriteString(strings.Join(names, ", ") + ", and the\npackage declares `writes: true` on that entry. " +
			"A pre-fetch runs unasked, before anybody\nhas said anything, so this runs for every wrong number, " +
			"every hang-up and every\nsilent call as well as every real one. That was a deliberate choice rather " +
			"than an\noversight, and this is where it is written down.\n")
	}
	if zones := prefetchClockZones(agent); len(zones) > 0 {
		b.WriteString("\n**The clock is read once, at session start, in `" + strings.Join(zones, "` and `") + "`.** " +
			"Not the\ncontainer's clock, which is UTC and would name the wrong day for anybody who is\n" +
			"not on it. Each entry above names its own zone.\n\n" +
			"**A call that crosses midnight keeps the day it started on.** " +
			"The value is read\nonce and never refreshed, so a call beginning at 23:55 still says that day at\n" +
			"00:10. This is deliberate: a value that changed underneath a conversation would\n" +
			"be worse, because the caller and the agent would stop agreeing about what\n" +
			"\"tomorrow\" means halfway through. If a call really can outlast midnight and the\n" +
			"date has to follow, read it in a tool instead of pre-fetching it.\n")
	}
	if PrefetchUnconfirmed(agent) {
		b.WriteString("\n**A pre-fetched value can need confirming.** ")
		names := make([]string, 0, len(agent.Variables))
		for _, name := range sortedKeys(agent.Variables) {
			if agent.Variables[name].Confirm != "" {
				names = append(names, "`"+name+"` (confirmed by `"+agent.Variables[name].Confirm+"`)")
			}
		}
		b.WriteString(strings.Join(names, ", ") + " arrive\nfilled but not yet agreed to. Until the naming step has heard the caller agree,\n" +
			"such a value satisfies no `requires:` guard and appears in no prompt except that\n" +
			"step's own. So the agent reads the value back and asks for a yes rather than\n" +
			"acting on it, which is what stops somebody ringing from a friend's phone being\n" +
			"treated as the account holder.\n")
	}
	b.WriteString("\n**What you see in the log.** One line per entry, naming the entry and the values:\n\n```text\n")
	b.WriteString("prefetch " + agent.Prefetch[0].Name + ": resolved " + prefetchAssignedNames(agent.Prefetch[0]) + "\n")
	b.WriteString("prefetch " + agent.Prefetch[len(agent.Prefetch)-1].Name + ": skipped, ...\n")
	b.WriteString("```\n\nA value is never written to the log, only its name, which matters when it is\n" +
		"something like a phone number.\n")
	return b.String(), true
}

// prefetchDirectionNote says which way a call has to be going for this fact to
// arrive, on a route that supplies it one way only.
//
// Empty on every other route, because empty Directions has always meant both and
// a runbook that said "on an inbound or outbound call" everywhere would be noise
// on the one line where the limit matters.
func prefetchDirectionNote(resolved ir.Target, source ir.VariableSource) string {
	if resolved.Telephony == nil {
		return ""
	}
	key := targetcap.TelephonyKey{
		Provider:  targetcap.Provider(resolved.Provider),
		Transport: resolved.Transport,
		Carrier:   resolved.Carrier,
	}
	feature := targetcap.TelephonyFeature(targetcap.TelephonySourcePrefix + string(source))
	evidence := targetcap.ResolveTelephonyFeature(key, feature)
	if evidence.Tag == targetcap.Gated || len(evidence.Directions) == 0 {
		return ""
	}
	ways := make([]string, 0, len(evidence.Directions))
	for _, direction := range evidence.Directions {
		ways = append(ways, string(direction))
	}
	return ", on an " + strings.Join(ways, " or ") + " call only"
}

// prefetchNumberEntries names the entries reading a phone number, which are the
// entries the best-effort warning is about.
func prefetchNumberEntries(agent *ir.Agent) []string {
	var names []string
	for _, entry := range agent.Prefetch {
		if prefetchSourceIsANumber(entry.Source) {
			names = append(names, "`"+entry.Name+"`")
		}
	}
	return names
}

// prefetchKeepsDefault names what an entry failed to fill, and agrees with
// itself about number. An entry may assign several variables, and "customer_name,
// customer_on_file keeps its default" is the sort of line that makes a reader at
// three in the morning wonder which one it means.
func prefetchKeepsDefault(entry ir.Prefetch) string {
	names := prefetchAssignedNames(entry)
	if len(entry.Assign) == 1 {
		return names + " keeps its default"
	}
	return names + " keep their defaults"
}

// prefetchClockZones lists the distinct zones this package reads a clock in, in
// authored order. Empty when nothing reads a clock, which is what makes the
// timezone and the midnight answer worth a runbook section at all.
func prefetchClockZones(agent *ir.Agent) []string {
	var zones []string
	for _, entry := range agent.Prefetch {
		if entry.Clock != "" && !slices.Contains(zones, entry.Timezone) {
			zones = append(zones, entry.Timezone)
		}
	}
	return zones
}

// prefetchRequestFor builds one entry's request from the same helpers the
// mid-call tool path uses: urlExpr for the URL, injectExpr for every value,
// requestBody and callKwargs for the shape, loweredAuth for the headers.
//
// The entry's own `args:` are lowered exactly as `inject:` is, which is the
// promise the contract makes ("same value grammar as inject:, written as a
// list"), and the tool's own `inject:` values are merged in on top, because a
// pre-fetched call is still that tool's call and its hidden values still belong
// in it.
//
// Both drivers call this. That is the answer to "reuse the request building
// rather than writing a second one": the parts that drift, URL assembly and auth
// and injected values, have one owner, and it is this function.
func prefetchRequestFor(agent *ir.Agent, entry ir.Prefetch) PrefetchRequest {
	tool := agent.Tools[entry.Tool]
	values := make([]injectedValue, 0, len(entry.Args)+len(tool.Inject))
	for _, pair := range entry.Args {
		values = append(values, injectedValue{Key: pair.Key, Expr: injectExpr(pair.Value, prefetchStateExpr)})
	}
	injected, _ := loweredInject(tool, agent.Variables, prefetchStateExpr)
	values = append(values, injected...)
	request := PrefetchRequest{Name: entry.Tool, Local: tool.Execution == ir.ToolLocal}
	if request.Local {
		request.CallKwargs = callKwargs(nil, values)
		return request
	}
	request.URLExpr = urlExpr(tool, prefetchStateExpr)
	request.BodyExpr = requestBody(nil, values)
	if auth := loweredAuth(tool.Auth); auth != nil {
		request.AuthExpr = auth.Expr
	}
	return request
}

// PrefetchNeedsSeed reports whether any entry reads a call fact, which is what
// makes the local seed worth passing into the container.
//
// Read off the agent rather than off a driver's own flag, because both drivers set
// their DevOptionalEnv list before they lower the block: reading the flag there
// silently answered "no" every time, and the seed never reached the container.
func PrefetchNeedsSeed(agent *ir.Agent) bool {
	for _, entry := range agent.Prefetch {
		if entry.Source != "" {
			return true
		}
	}
	return false
}

// prefetchNeedsHTTPX reports whether any pre-fetched tool is a webhook, which is
// what pulls the httpx import into a package whose mid-call tools are all local.
func prefetchNeedsHTTPX(agent *ir.Agent) bool {
	for _, entry := range agent.Prefetch {
		if entry.Tool != "" && agent.Tools[entry.Tool].Execution == ir.ToolWebhook {
			return true
		}
	}
	return false
}

// prefetchStateExpr is how the emitted block reaches the call state. It differs
// from each driver's mid-call expression on purpose: a pre-fetch runs before any
// RunContext or FunctionCallParams exists, so it holds the state object directly
// and every value expression it builds has to say `state` rather than
// `ctx.userdata` or `state` off a params object.
const prefetchStateExpr = "state"

// PrefetchUnconfirmed reports whether a package has any value awaiting the
// caller's agreement, which is what decides whether the guard consults the set.
func PrefetchUnconfirmed(agent *ir.Agent) bool {
	for _, name := range sortedKeys(agent.Variables) {
		if agent.Variables[name].Confirm != "" {
			return true
		}
	}
	return false
}

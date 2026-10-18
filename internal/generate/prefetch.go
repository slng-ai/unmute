package generate

// The pre-fetch block, and the one place its words are written.
//
// Facts knowable before the greeting are resolved here, once per call, so the
// model is never asked to discover them. One file owns the emitted text, both
// drivers render what it produces, and the two targets agree by construction
// rather than by a test that has to notice they stopped agreeing. The test
// still exists, because construction can be undone.

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

// LocalCallFactsEnv carries the facts `unmute dev --source` seeds, as a JSON
// object, so the browser loop exercises the same code a phone call does.
//
// A second name beside UNMUTE_CALL_START, not a widening of it, and that is the
// whole point: the dispatch payload and the carrier's own facts are different
// things arriving through different doors, and seeding a variable directly would
// skip the pre-fetch, mark nothing as awaiting confirmation, and let a local run
// pass a path a real call fails.
const LocalCallFactsEnv = "UNMUTE_CALL_FACTS"

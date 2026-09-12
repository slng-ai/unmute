package target

import "fmt"

// RetiredParam is a `params:` key a vendor's service no longer has.
//
// It exists because of how `params:` is forwarded: every key an author writes is
// passed to the service constructor by name, with no whitelist. That is what
// makes the surface open to a vendor's whole settings object without this
// compiler tracking each one. The cost is that a key the vendor removes compiles
// clean and raises at worker startup, in a deployed container, with a traceback
// nobody authored. A refusal here moves that to compile time, with a line.
//
// Only removals go in this table. A setting that still exists and merely behaves
// differently is documentation, not a refusal.
type RetiredParam struct {
	// Framework, Vendor and Role are all part of the key.
	//
	// Vendor and Role, because `model:` is a removed Speechmatics listening
	// setting and an ordinary field on every think binding in the tree, so a
	// table keyed by name alone would refuse half the packages here.
	//
	// Framework, because each driver reaches a vendor through its own SDK. The
	// LiveKit Speechmatics plugin is not the Pipecat one and did not lose these
	// settings, so refusing them there would break a working package to report
	// somebody else's removal.
	Framework Provider
	Vendor    string
	Role      Role
	Key       string
	// Replacement is the key that took this one's place, empty where nothing
	// did. A refusal that cannot say which of the two it is has nothing useful
	// to tell the author, so both cases are spelled out below.
	Replacement string
	// Since is the framework version that removed or deprecated it, named in the
	// refusal so the author knows whether their own pin is affected.
	Since string
	// Deprecated marks a key the vendor still accepts today and removes in its
	// next major. It changes the sentence, and the sentence is the point: a key
	// that is gone and a key that warns are two different situations, and an
	// author told the wrong one will go looking for a failure that is not there.
	// Both are refused, because a warning in a container log is a warning nobody
	// reads and the fix is one word either way.
	Deprecated bool
}

// retiredParams is the one recorded home for these facts (Principle III). The
// refusal reads it, and so does the docs-site gate that refuses a page still
// teaching one of these keys.
//
// Every entry below was produced by diffing the two Settings dataclasses field
// by field between the pinned wheels, not by reading the changelog. The eleven
// removals and the one deprecation are spec 022 research R4.
var retiredParams = []RetiredParam{
	// pipecat-ai 1.10.0 moved Speechmatics onto Agent STT. The service's own
	// migration table names a replacement for exactly one of these.
	{Framework: Pipecat, Vendor: "speechmatics", Role: Listen, Key: "include_partials", Replacement: "enable_partials", Since: "1.10.0"},
	{Framework: Pipecat, Vendor: "speechmatics", Role: Listen, Key: "include_results", Since: "1.10.0"},
	{Framework: Pipecat, Vendor: "speechmatics", Role: Listen, Key: "split_sentences", Since: "1.10.0"},
	{Framework: Pipecat, Vendor: "speechmatics", Role: Listen, Key: "max_delay", Since: "1.10.0"},
	{Framework: Pipecat, Vendor: "speechmatics", Role: Listen, Key: "end_of_utterance_silence_trigger", Since: "1.10.0"},
	{Framework: Pipecat, Vendor: "speechmatics", Role: Listen, Key: "end_of_utterance_max_delay", Since: "1.10.0"},
	{Framework: Pipecat, Vendor: "speechmatics", Role: Listen, Key: "focus_speakers", Since: "1.10.0"},
	{Framework: Pipecat, Vendor: "speechmatics", Role: Listen, Key: "ignore_speakers", Since: "1.10.0"},
	{Framework: Pipecat, Vendor: "speechmatics", Role: Listen, Key: "focus_mode", Since: "1.10.0"},
	{Framework: Pipecat, Vendor: "speechmatics", Role: Listen, Key: "speaker_passive_format", Since: "1.10.0"},
	{Framework: Pipecat, Vendor: "speechmatics", Role: Listen, Key: "extra_params", Since: "1.10.0"},
	// Not removed, deprecated: it warns on every run today and goes in 2.0.0.
	// Refused rather than forwarded because a warning in a container log is a
	// warning nobody reads, and the fix is one word.
	{Framework: Pipecat, Vendor: "speechmatics", Role: Listen, Key: "operating_point", Replacement: "model", Since: "1.10.0", Deprecated: true},
}

// LookupRetiredParam returns the record for a `params:` key a vendor's service
// no longer has on a framework, and false for every key that still reaches
// something there.
func LookupRetiredParam(framework Provider, vendor string, role Role, key string) (RetiredParam, bool) {
	for _, param := range retiredParams {
		if param.Framework == framework && param.Vendor == vendor && param.Role == role && param.Key == key {
			return param, true
		}
	}
	return RetiredParam{}, false
}

// RetiredParams returns every record, for the tests and the docs-site gate.
func RetiredParams() []RetiredParam {
	out := make([]RetiredParam, len(retiredParams))
	copy(out, retiredParams)
	return out
}

// Fate is what happened to the key, as the middle of the refusal's sentence.
// "was removed in pipecat-ai 1.10.0" and "is deprecated since pipecat-ai 1.10.0
// and goes in the next major" send an author to two different places, so the
// record says which rather than the refusal guessing from whether a replacement
// exists. An earlier version guessed, and told an author a key that still works
// was gone.
func (p RetiredParam) Fate() string {
	if p.Deprecated {
		return fmt.Sprintf("is deprecated since %s %s and goes in the next major", FrameworkPackage(p.Framework), p.Since)
	}
	return fmt.Sprintf("was removed in %s %s", FrameworkPackage(p.Framework), p.Since)
}

// Advice is the sentence a refusal prints after naming the key: either the key
// to write instead, or that nothing replaced it. One of the two, never neither,
// which is what makes this worth refusing rather than warning.
func (p RetiredParam) Advice() string {
	if p.Replacement != "" {
		return fmt.Sprintf("write %s instead", p.Replacement)
	}
	return "nothing replaced it, so remove it"
}

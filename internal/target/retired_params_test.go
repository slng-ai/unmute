package target

import (
	"strings"
	"testing"
)

// TestEveryRetiredParamSaysWhatToDo: the whole value of refusing a removed key
// instead of forwarding it is that the author is told what to write. An entry
// that can say neither "write this instead" nor "nothing replaced it" would
// produce a refusal no better than the TypeError it replaces.
func TestEveryRetiredParamSaysWhatToDo(t *testing.T) {
	params := RetiredParams()
	if len(params) == 0 {
		t.Fatal("no retired params, so the refusal reaches nothing")
	}
	seen := map[string]bool{}
	for _, param := range params {
		key := string(param.Framework) + "/" + param.Vendor + "/" + string(param.Role) + "/" + param.Key
		if seen[key] {
			t.Errorf("%s is listed twice, so one refusal would hide the other", key)
		}
		seen[key] = true
		for name, value := range map[string]string{
			"Framework": string(param.Framework), "Vendor": param.Vendor,
			"Key": param.Key, "Since": param.Since,
		} {
			if value == "" {
				t.Errorf("%s: %s is empty", key, name)
			}
		}
		if param.Role == "" {
			t.Errorf("%s: no role, so this would refuse the key on every binding", key)
		}
		advice := param.Advice()
		if advice == "" {
			t.Errorf("%s: no advice", key)
		}
		if param.Replacement != "" && !strings.Contains(advice, param.Replacement) {
			t.Errorf("%s: advice %q does not name the replacement %q", key, advice, param.Replacement)
		}
		if param.Replacement == "" && !strings.Contains(advice, "remove") {
			t.Errorf("%s: nothing replaced it and the advice %q does not say to remove it", key, advice)
		}
		// A key cannot be its own replacement, which is the shape a copy-paste
		// produces and which would tell the author to write what they wrote.
		if param.Replacement == param.Key {
			t.Errorf("%s: replacement is the same key", key)
		}
		// The vendor has to be one the catalogue lists for that role, or the
		// refusal fires on a binding this compiler cannot emit anyway.
		if _, ok := DefaultCatalog().Lookup(param.Framework, param.Role, param.Vendor); !ok {
			t.Errorf("%s: no %s %s entry for this vendor", key, param.Framework, param.Role)
		}
	}
}

// TestRetiredParamIsKeyedByFrameworkVendorAndRole is the one that stops this
// table becoming a liability. `model` is a removed Speechmatics listening
// setting and an ordinary field on every think binding in the tree, and the
// LiveKit plugin for the same vendor lost none of these settings: a lookup by
// key alone would refuse most of the packages here.
func TestRetiredParamIsKeyedByFrameworkVendorAndRole(t *testing.T) {
	for _, tc := range []struct {
		framework Provider
		vendor    string
		role      Role
		key       string
		want      bool
	}{
		{Pipecat, "speechmatics", Listen, "include_results", true},
		{Pipecat, "speechmatics", Listen, "operating_point", true},
		// Same key, same vendor, DIFFERENT FRAMEWORK. The LiveKit plugin is a
		// different SDK and lost nothing; on that target operating_point is how
		// accuracy is chosen, so refusing it would break a working package.
		{LiveKit, "speechmatics", Listen, "operating_point", false},
		{LiveKit, "speechmatics", Listen, "include_results", false},
		// Same key, different vendor: assemblyai forwards whatever it forwards.
		{Pipecat, "assemblyai", Listen, "include_results", false},
		// Same key, different role.
		{Pipecat, "speechmatics", Reason, "operating_point", false},
		// Keys the service still has.
		{Pipecat, "speechmatics", Listen, "enable_partials", false},
		{Pipecat, "speechmatics", Listen, "enable_diarization", false},
		{Pipecat, "speechmatics", Listen, "model", false},
	} {
		_, got := LookupRetiredParam(tc.framework, tc.vendor, tc.role, tc.key)
		if got != tc.want {
			t.Errorf("lookup(%s, %s, %s, %s) = %v, want %v", tc.framework, tc.vendor, tc.role, tc.key, got, tc.want)
		}
	}
}

// TestRetiredParamsAreNotReturnedByReference: the table is read by a refusal and
// by a docs gate, and a caller that could append to the returned slice would be
// editing the compiler's own record of what a vendor removed.
func TestRetiredParamsAreNotReturnedByReference(t *testing.T) {
	first := RetiredParams()
	before := len(first)
	first[0].Key = "clobbered"
	second := RetiredParams()
	if len(second) != before {
		t.Fatalf("length changed from %d to %d", before, len(second))
	}
	if second[0].Key == "clobbered" {
		t.Error("a caller edited the table through the slice it was handed")
	}
}

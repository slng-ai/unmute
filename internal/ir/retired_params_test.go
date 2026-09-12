package ir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	packagespec "github.com/slng-ai/unmute/internal/spec"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// A `params:` key is forwarded to the service constructor by name, with no
// whitelist. That is what lets an author reach a vendor's whole settings object
// without this compiler tracking each one, and it is why a key the vendor
// REMOVES compiles clean here and raises TypeError when the worker starts, in a
// deployed container, on the first call, with a traceback naming a dataclass
// nobody here wrote.
//
// pipecat 1.10.0 removed eleven Speechmatics settings and deprecated a twelfth.
// These hold the refusal that moves that failure to compile time, with a line.

// withListenParams copies a fixture, writes one `params:` entry onto its listen
// binding, and builds it. The copy is on disk rather than in memory because the
// refusal's whole value is the line number, and a line number needs a file.
func withListenParams(t *testing.T, fixture, key, value string) error {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join("..", "testdata", fixture)
	var copyTree func(from, to string)
	copyTree = func(from, to string) {
		items, err := os.ReadDir(from)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(to, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if item.Name() == "build" {
				continue
			}
			if item.IsDir() {
				copyTree(filepath.Join(from, item.Name()), filepath.Join(to, item.Name()))
				continue
			}
			raw, err := os.ReadFile(filepath.Join(from, item.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(to, item.Name()), raw, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	copyTree(src, root)

	path := filepath.Join(root, "agent.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The listening binding's language line is the anchor: writing after it puts
	// the params block inside the binding at the right indent.
	const anchor = "      language: en\n"
	body := string(raw)
	if !strings.Contains(body, anchor) {
		t.Fatalf("fixture %s has no listening language line to anchor on", fixture)
	}
	body = strings.Replace(body, anchor, anchor+"      params:\n        "+key+": "+value+"\n", 1)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	pkg, err := packagespec.Load(root)
	if err != nil {
		t.Fatalf("the fixture itself does not load: %v", err)
	}
	_, err = Build(pkg)
	return err
}

// TestBuildRefusesEveryRetiredVendorParam walks the table rather than a copy of
// it, so a key added there is refused with no second list to remember.
func TestBuildRefusesEveryRetiredVendorParam(t *testing.T) {
	params := targetcap.RetiredParams()
	if len(params) == 0 {
		t.Fatal("no retired params, so this test would pass for the wrong reason")
	}
	checked := 0
	for _, param := range params {
		if param.Framework != targetcap.Pipecat || param.Vendor != "speechmatics" || param.Role != targetcap.Listen {
			// The fixture binds speechmatics listening on pipecat. A row for
			// another vendor, role or framework needs its own fixture, and the
			// count below says how many this one covered.
			continue
		}
		checked++
		err := withListenParams(t, "speechmatics_local", param.Key, "true")
		if err == nil {
			t.Errorf("params.%s was accepted; it is gone in pipecat %s and the worker would raise on the first call", param.Key, param.Since)
			continue
		}
		message := err.Error()
		// The line, which is the whole reason this is refused at build.
		if !strings.Contains(message, "agent.yaml:") {
			t.Errorf("params.%s: the refusal names no line: %s", param.Key, message)
		}
		for _, want := range []string{param.Key, "speechmatics", param.Since} {
			if !strings.Contains(message, want) {
				t.Errorf("params.%s: the refusal does not name %q: %s", param.Key, want, message)
			}
		}
		// Removed and deprecated are two different situations and send an
		// author to two different places. An earlier version guessed which
		// from whether a replacement existed, and told an author a key that
		// still works today was gone.
		if param.Deprecated {
			if !strings.Contains(message, "is deprecated since") {
				t.Errorf("params.%s still works today and the refusal calls it removed: %s", param.Key, message)
			}
		} else if !strings.Contains(message, "was removed in") {
			t.Errorf("params.%s is gone and the refusal does not say so: %s", param.Key, message)
		}
		// And what to do, which is the difference between this and the
		// TypeError it replaces.
		if param.Replacement != "" {
			if !strings.Contains(message, "Write "+param.Replacement) {
				t.Errorf("params.%s: the refusal does not say to write %q: %s", param.Key, param.Replacement, message)
			}
		} else if !strings.Contains(message, "Nothing replaced it") {
			t.Errorf("params.%s: nothing replaced it and the refusal does not say so: %s", param.Key, message)
		}
	}
	if checked < 12 {
		t.Errorf("covered %d rows, want every speechmatics listen row; a row added to the table is a row this test has to reach", checked)
	}
}

// TestBuildKeepsEveryParamTheServiceStillHas is the half that keeps the table
// from becoming a liability. Refusing too widely breaks a working package, which
// is worse than the failure being refused: the author did nothing wrong.
func TestBuildKeepsEveryParamTheServiceStillHas(t *testing.T) {
	for _, key := range []string{
		// Settings the 1.10.0 service still declares.
		"enable_partials", "enable_diarization", "prefer_current_speaker", "domain",
		// And a key that is only retired on ANOTHER vendor's binding.
		"profanity_filter",
	} {
		if err := withListenParams(t, "speechmatics_local", key, "true"); err != nil {
			t.Errorf("params.%s was refused, and the service still has it: %v", key, err)
		}
	}
}

// TestBuildRefusesARetiredParamOnTheListeningBindingToo: the refusal reads the
// resolved binding, so it fires whichever turn decider the package wrote. A
// check wired to one path only would leave the other silently forwarding.
func TestBuildRefusesARetiredParamOnTheListeningBinding(t *testing.T) {
	err := withListenParams(t, "speechmatics_listen", "include_results", "true")
	if err == nil {
		t.Fatal("params.include_results was accepted on a package that hands the turn to the transcriber")
	}
	if !strings.Contains(err.Error(), "include_results") {
		t.Errorf("the refusal does not name the key: %v", err)
	}
}

// TestBuildRefusesTheDeprecatedAliasByName: operating_point is not removed, it
// warns on every run and goes in 2.0.0. Refused anyway, because a warning in a
// container log is a warning nobody reads and the fix is one word.
func TestBuildRefusesTheDeprecatedAlias(t *testing.T) {
	err := withListenParams(t, "speechmatics_local", "operating_point", "enhanced")
	if err == nil {
		t.Fatal("params.operating_point was accepted")
	}
	if !strings.Contains(err.Error(), "Write model") {
		t.Errorf("the refusal does not name the field that replaced it: %v", err)
	}
}

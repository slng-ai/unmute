package ir

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	packagespec "github.com/slng-ai/unmute/internal/spec"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// TestHostedDigestAlgorithmIsPinned pins the digest of empty content.
//
// Same reason internal/skill/skill_test.go pins Hash(nil): a change of
// algorithm or of encoding would silently invalidate every pin already
// committed, and the symptom would be every hosted package refusing to compile
// with a message about a mirror nobody edited. Failing here instead says what
// actually happened.
func TestHostedDigestAlgorithmIsPinned(t *testing.T) {
	// SHA-256 of the empty string, lowercase hex.
	const want = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got := MirrorDigest(nil); got != want {
		t.Errorf("MirrorDigest(nil) = %q, want %q: the algorithm or its encoding changed, which invalidates every pin already committed", got, want)
	}
	if got := MirrorDigest([]byte{}); got != want {
		t.Errorf("MirrorDigest of empty bytes = %q, want %q", got, want)
	}
}

// hostedFixture loads the slng-only hosted fixture and returns the agent, so
// each case below starts from a package that really validates.
func hostedFixture(t *testing.T) (*Agent, []Target) {
	t.Helper()
	pkg, err := packagespec.Load(filepath.Join("..", "testdata", "slng_hosted"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	var targets []Target
	for _, name := range sortedKeys(agent.Targets) {
		targets = append(targets, agent.Targets[name])
	}
	return agent, targets
}

// TestMirrorPinIsCheckedOffline is the check that makes an offline build
// trustworthy, which is the requirement the whole design rests on: nothing in
// CI has an SLNG credential, so a build that cannot verify its own inputs with
// no network cannot be verified at all.
//
// It refuses rather than warns. A stale mirror on a code target is the wrong
// code running, not a stale note.
func TestMirrorPinIsCheckedOffline(t *testing.T) {
	// The fixture as committed has to pass, or nothing below means anything.
	agent, targets := hostedFixture(t)
	if _, err := Validate(agent, targets, targetcap.Default()); err != nil {
		t.Fatalf("the committed fixture does not validate, so this gate proves nothing: %v", err)
	}

	for _, tc := range []struct {
		name string
		edit func(tool *Tool)
		want []string
	}{
		{
			name: "a hand-edited mirror",
			edit: func(tool *Tool) { tool.MirrorBytes = append(tool.MirrorBytes, " # tweaked"...) },
			want: []string{
				"does not match the hash",
				"tools/check_order.yaml",
				"tools/check_order.slng.py",
				// Both recoveries, because one of the two files is wrong and the
				// author is the only one who knows which.
				"unmute pull",
				"git checkout",
			},
		},
		{
			name: "no mirror committed",
			edit: func(tool *Tool) { tool.Mirror, tool.MirrorBytes = nil, nil },
			want: []string{"no mirror of it is committed", "run `unmute pull`", "commit what it writes"},
		},
		{
			name: "a mirror with no pin, which is `slng: {}` after a pull that was not committed",
			edit: func(tool *Tool) { tool.MirrorPin = "" },
			want: []string{"pins no hash", "nothing proves the two belong together", "unmute pull"},
		},
		{
			name: "a curated capability committed as a mirror",
			edit: func(tool *Tool) { tool.Mirror.Source = "curated" },
			want: []string{"capability SLNG curates", "builtin:"},
		},
		{
			name: "a tool_type no target can run",
			edit: func(tool *Tool) { tool.Mirror.ToolType = "current_datetime" },
			want: []string{"is a current_datetime tool", "`code` or an `api_request`", "builtin:"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent, targets := hostedFixture(t)
			tool := agent.Tools["check_order"]
			// A copy of the mirror per case, so one case cannot leak into
			// another through the shared pointer.
			mirror := *tool.Mirror
			tool.Mirror = &mirror
			tc.edit(&tool)
			agent.Tools["check_order"] = tool

			report, err := Validate(agent, targets, targetcap.Default())
			if err == nil {
				t.Fatal("validation passed")
			}
			joined := strings.Join(report.PerTarget[0].Errors, "\n")
			for _, want := range tc.want {
				if !strings.Contains(joined, want) {
					t.Errorf("the refusal does not say %q:\n%s", want, joined)
				}
			}
		})
	}
}

// TestHostedMirrorIsNotPartOfTheDebugSchema: a mirrored module is content, not
// resolved authoring.
//
// The debug schema describes what somebody wrote plus what the compiler
// decided. A mirrored module inlined into it would bury both under another
// system's source, which is the same reason HandlerSource carries `json:"-"`.
func TestHostedMirrorIsNotPartOfTheDebugSchema(t *testing.T) {
	agent, _ := hostedFixture(t)
	encoded, err := json.Marshal(agent.Tools["check_order"])
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"code_src", "content_hash", "ORDERS", "def handler"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("the resolved tool serialises %q, so the mirror leaked into the debug schema", forbidden)
		}
	}
	// The half that has to be there: the platform's schema became the tool's
	// input, which is what makes both code drivers work unchanged.
	if !strings.Contains(string(encoded), "order_number") {
		t.Error("the mirror's arg_schema never reached Tool.Input, so no driver can build a signature")
	}
}

// TestHostedExecutionKindIsInTheDerivedSchema. The enum listed six of seven
// kinds for as long as ToolKnowledge existed, so the derived schema called a
// value the compiler produces illegal. Adding an eighth beside a missing
// seventh would have read as deliberate.
func TestHostedExecutionKindIsInTheDerivedSchema(t *testing.T) {
	schema, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []ToolExecution{
		ToolLocal, ToolClient, ToolWebhook, ToolProviderHosted,
		ToolBuiltin, ToolMCP, ToolKnowledge, ToolSlngHosted,
	} {
		if !strings.Contains(string(encoded), `"`+string(kind)+`"`) {
			t.Errorf("the derived schema's execution enum omits %q, so it calls a value the compiler produces illegal", kind)
		}
	}
}

// TestHostedFixtureMirrorsAreWhatThePullWrote guards the fixtures themselves,
// because every other test in this file trusts them.
//
// A mirror is the platform's copy. If somebody edits one and re-pins it to keep
// the suite green, the offline check above still passes and proves nothing. So
// this asserts the two properties an edit would break: the module carries the
// lint header the pull writes, and the pin is the digest of the bytes on disk.
func TestHostedFixtureMirrorsAreWhatThePullWrote(t *testing.T) {
	for _, fixture := range []string{"slng_hosted", "slng_hosted_code"} {
		t.Run(fixture, func(t *testing.T) {
			root := filepath.Join("..", "testdata", fixture)
			module, err := os.ReadFile(filepath.Join(root, "tools", "check_order.slng.py"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(module), packagespec.MirrorHeaderLines) {
				t.Error("the mirrored module does not start with the header the pull writes, which is what keeps CI's lint green over another system's code")
			}
			sidecar, err := os.ReadFile(filepath.Join(root, "tools", "check_order.slng.json"))
			if err != nil {
				t.Fatal(err)
			}
			pkg, err := packagespec.Load(root)
			if err != nil {
				t.Fatal(err)
			}
			want := MirrorDigest(append(append([]byte{}, sidecar...), module...))
			if got := pkg.Tools["check_order"].Slng.Hash; got != want {
				t.Errorf("the fixture's pin is %q and the bytes on disk digest to %q: re-pin with `unmute pull`, and if a mirror was hand-edited, undo that instead", got, want)
			}
		})
	}
}

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

// hostedFixture loads a hosted fixture and returns the agent, so each case
// below starts from a package that really validates. `slng_hosted` is the
// slng-only one and `slng_hosted_code` carries the same two tools on livekit and
// pipecat, and which one a case uses is the whole point of these tests now: the
// committed mirror is what a code target builds a tool out of, and what the
// slng target reads nowhere.
func hostedFixture(t *testing.T, fixture string) (*Agent, []Target) {
	t.Helper()
	pkg, err := packagespec.Load(filepath.Join("..", "testdata", fixture))
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
//
// It runs on the code fixture, and that is the change spec 007 made: a mirror
// is what livekit and pipecat build a tool out of, so they are the targets that
// require one and check it. TestSlngNeedsNoMirror below is the other half, and
// the two have to be read together: this test alone would pass if the check had
// simply been deleted.
func TestMirrorPinIsCheckedOffline(t *testing.T) {
	// The fixture as committed has to pass, or nothing below means anything.
	agent, targets := hostedFixture(t, "slng_hosted_code")
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
			want: []string{"builds a tool out of its committed mirror", "run `unmute pull`", "commit what it writes",
				// And the way out that is not a pull, because a package that
				// only deploys to slng never needed the mirror at all.
				"compile this package to slng"},
		},
		{
			// A mirror that is on disk and cannot be read is its own state.
			// Reporting it as absent would send the author to `unmute pull` for
			// a file they already have, and hide the edit that broke it.
			name: "a mirror on disk that will not decode",
			edit: func(tool *Tool) {
				tool.MirrorFailure = "tools/check_order.slng.json: unexpected end of JSON input: it is written by `unmute pull` and not by hand, so run the pull again"
			},
			want: []string{"unexpected end of JSON input", "not by hand"},
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
			agent, targets := hostedFixture(t, "slng_hosted_code")
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
	agent, _ := hostedFixture(t, "slng_hosted")
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

// TestSlngNeedsNoMirror is the inverse of the gate above, and it exists because
// deleting that gate would have made the gate pass.
//
// A SLNG-only package references a tool the organisation publishes. The
// platform owns its description, its parameters and its code, so there is
// nothing local for a mirror to supply and no pull to run before a compile. A
// package that carries no mirror files, and one whose mirror is corrupt, both
// have to validate clean on slng: the first is the shape spec 007 exists to
// allow, and the second proves the requirement really moved rather than being
// reordered behind a nil check.
func TestSlngNeedsNoMirror(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(tool *Tool)
	}{
		{"a scalar reference with no mirror at all", func(tool *Tool) {
			tool.Mirror, tool.MirrorBytes, tool.MirrorPin = nil, nil, ""
			tool.Input, tool.Dependencies, tool.Description = nil, nil, ""
		}},
		{"a mirror that will not decode", func(tool *Tool) {
			tool.Mirror, tool.MirrorBytes = nil, nil
			tool.MirrorFailure = "tools/check_order.slng.json: unexpected end of JSON input"
		}},
		{"a mirror whose pin no longer matches", func(tool *Tool) {
			tool.MirrorBytes = append(tool.MirrorBytes, " # edited"...)
		}},
		{"a mirror of a curated capability", func(tool *Tool) { tool.Mirror.Source = "curated" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent, targets := hostedFixture(t, "slng_hosted")
			tool := agent.Tools["check_order"]
			mirror := *tool.Mirror
			tool.Mirror = &mirror
			tc.edit(&tool)
			agent.Tools["check_order"] = tool

			report, err := Validate(agent, targets, targetcap.Default())
			if err != nil {
				t.Fatalf("the slng target refused a hosted reference over its mirror: %v\n%s",
					err, strings.Join(report.PerTarget[0].Errors, "\n"))
			}
			for _, warning := range report.PerTarget[0].Warnings {
				if strings.Contains(warning, "mirror") || strings.Contains(warning, "unmute pull") {
					t.Errorf("the slng target warns about a mirror it reads nowhere: %s", warning)
				}
			}
		})
	}
}

// TestSelectingTargetsInEitherOrderGivesTheSameResult: the mirror requirement is
// per target, so the two rows must not be able to influence each other. A check
// that wrote its finding onto the shared IR rather than onto its own row would
// pass in one order and fail in the other, and this is the cheapest way to catch
// that.
func TestSelectingTargetsInEitherOrderGivesTheSameResult(t *testing.T) {
	read := func(order []string) map[string][]string {
		agent, _ := hostedFixture(t, "slng_hosted_code")
		// A hosted tool with no mirror: refused on both code targets, and the
		// state a shared-IR mutation would smear across the rows.
		tool := agent.Tools["check_order"]
		tool.Mirror, tool.MirrorBytes = nil, nil
		agent.Tools["check_order"] = tool

		var targets []Target
		for _, name := range order {
			targets = append(targets, agent.Targets[name])
		}
		report, _ := Validate(agent, targets, targetcap.Default())
		out := map[string][]string{}
		for _, row := range report.PerTarget {
			out[row.Name] = row.Errors
		}
		return out
	}
	forward, reverse := read([]string{"livekit", "pipecat"}), read([]string{"pipecat", "livekit"})
	for name, errs := range forward {
		if strings.Join(errs, "\n") != strings.Join(reverse[name], "\n") {
			t.Errorf("target %q reports different errors depending on the order the targets were selected in:\n  forward: %v\n  reverse: %v",
				name, errs, reverse[name])
		}
	}
}

// TestScalarMirrorPinComesFromGeneratedMetadata is TestMirrorPinIsCheckedOffline's
// scalar counterpart, over slng_hosted_code_scalar rather than
// slng_hosted_code: same two mirrors, referenced with `slng: check_order`
// instead of a legacy `hash:` block, so the pin these cases check comes from
// generated tools/check_order.slng.meta.json rather than from the tool file.
//
// slng_hosted_code stays exactly as it is (TestHostedFixtureMirrorsAreWhatThePullWrote
// still reads it), which is why this is a second fixture and not an edit of
// the first.
func TestScalarMirrorPinComesFromGeneratedMetadata(t *testing.T) {
	agent, targets := hostedFixture(t, "slng_hosted_code_scalar")
	if _, err := Validate(agent, targets, targetcap.Default()); err != nil {
		t.Fatalf("the committed scalar fixture does not validate, so this gate proves nothing: %v", err)
	}
	if !agent.Tools["check_order"].MirrorScalar {
		t.Fatal("a tool referenced with `slng: check_order` did not record itself as a scalar reference")
	}

	for _, tc := range []struct {
		name string
		edit func(tool *Tool)
		want []string
	}{
		{
			name: "no metadata committed, which is a scalar reference before its first pull",
			edit: func(tool *Tool) { tool.MirrorPin = "" },
			want: []string{
				"check_order.slng.meta.json", "no hash is recorded", "nothing proves the two belong together", "unmute pull",
				// And never the legacy wording, which would send the author to
				// edit a file `unmute pull` never touches for this form.
			},
		},
		{
			name: "a hand-edited mirror",
			edit: func(tool *Tool) { tool.MirrorBytes = append(tool.MirrorBytes, " # tweaked"...) },
			want: []string{
				"does not match the hash", "tools/check_order.slng.py",
				// The pin named here is the metadata file, not the tool's own YAML.
				"check_order.slng.meta.json", "unmute pull", "git checkout",
			},
		},
		{
			name: "the scalar changed since the last pull, so the committed mirror belongs to a different tool now",
			edit: func(tool *Tool) { tool.HostedName = "renamed_tool" },
			want: []string{
				"mirrors \"check_order\"", `now means "renamed_tool"`,
				"changed since the last `unmute pull`", "unmute pull",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent, targets := hostedFixture(t, "slng_hosted_code_scalar")
			tool := agent.Tools["check_order"]
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

// TestScalarMissingPinNamesMetadataNotTheToolFile is the negative half of the
// case above: the message a legacy reference gets must not leak into a scalar
// one's, because "tools/check_order.yaml pins no hash" sends a scalar author
// to edit a file `unmute pull` never writes for that form.
func TestScalarMissingPinNamesMetadataNotTheToolFile(t *testing.T) {
	agent, targets := hostedFixture(t, "slng_hosted_code_scalar")
	tool := agent.Tools["check_order"]
	tool.MirrorPin = ""
	agent.Tools["check_order"] = tool

	report, err := Validate(agent, targets, targetcap.Default())
	if err == nil {
		t.Fatal("validation passed")
	}
	joined := strings.Join(report.PerTarget[0].Errors, "\n")
	if strings.Contains(joined, "tools/check_order.yaml pins no hash") {
		t.Errorf("a scalar reference's missing pin was described in the tool file's own words:\n%s", joined)
	}
}

// TestSlngNeedsNoScalarMetadataEither is TestSlngNeedsNoMirror's scalar
// counterpart: a scalar reference with no metadata, a corrupt one, or one that
// no longer matches must all still validate clean on slng, because the
// published version supplies the tool there and no metadata is read at all.
func TestSlngNeedsNoScalarMetadataEither(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(tool *Tool)
	}{
		{"no metadata at all", func(tool *Tool) {
			tool.Mirror, tool.MirrorBytes, tool.MirrorPin = nil, nil, ""
			tool.Input, tool.Dependencies, tool.Description = nil, nil, ""
		}},
		{"metadata that will not decode", func(tool *Tool) {
			tool.Mirror, tool.MirrorBytes = nil, nil
			tool.MirrorFailure = "tools/check_order.slng.meta.json: unexpected end of JSON input"
		}},
		{"a pin that no longer matches", func(tool *Tool) {
			tool.MirrorBytes = append(tool.MirrorBytes, " # edited"...)
		}},
		{"the hosted name changed since the last pull", func(tool *Tool) {
			tool.HostedName = "renamed_tool"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent, _ := hostedFixture(t, "slng_hosted")
			tool := agent.Tools["check_order"]
			mirror := *tool.Mirror
			tool.Mirror = &mirror
			tool.MirrorScalar = true
			tc.edit(&tool)
			agent.Tools["check_order"] = tool

			var targets []Target
			targets = append(targets, agent.Targets["slng"])
			report, err := Validate(agent, targets, targetcap.Default())
			if err != nil {
				t.Fatalf("the slng target refused a hosted reference over its metadata: %v\n%s",
					err, strings.Join(report.PerTarget[0].Errors, "\n"))
			}
			for _, warning := range report.PerTarget[0].Warnings {
				if strings.Contains(warning, "mirror") || strings.Contains(warning, "metadata") || strings.Contains(warning, "unmute pull") {
					t.Errorf("the slng target warns about metadata it reads nowhere: %s", warning)
				}
			}
		})
	}
}

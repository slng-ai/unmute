package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateCommandPrintsEveryTargetAndWarnings(t *testing.T) { // V16, V18
	stdout, stderr, err := runValidateCommand(t, filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"✓ livekit (livekit)", "✓ deepgram (deepgram)"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stderr, "Warnings:\n") || strings.Contains(stderr, "Errors:\n") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestValidateCommandFiltersTargetInstances(t *testing.T) { // V18
	stdout, _, err := runValidateCommand(t, "--target", "vapi", filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "✓ vapi (vapi)") || strings.Contains(stdout, "livekit") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestValidateCommandReturnsErrorForGatedTarget(t *testing.T) { // V16
	dir := filepath.Join(t.TempDir(), "agent")
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "testdata", "safe_core"))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agent.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content = bytes.Replace(content, []byte("  max_duration: 20m"), []byte("  max_duration: 20m\n  thinking_audio: subtle"), 1)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runValidateCommand(t, "--target", "vapi", dir)
	if err == nil || !strings.Contains(stdout, "✗ vapi (vapi)") || !strings.Contains(stderr, "Errors:\n") || !strings.Contains(stderr, "Vapi has no faithful thinking-audio lowering") {
		t.Fatalf("err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
}

// US3: an account feature the provider grants on request must be named before an
// author spends money proving it is missing. Cold transfer on the Daily route
// dials the destination, which needs Daily dial-out enabled on the domain.
//
// Exit code stays 0. A prerequisite is a fact about the route, not a defect in
// the package: unmute compiles it perfectly and cannot know what the author's
// Daily account is allowed to do. Failing here would refuse correct packages.
func TestValidateNamesTheDailyDialOutPrerequisite(t *testing.T) {
	stdout, stderr, err := runValidateCommand(t, "--target", "pipecat",
		filepath.Join("..", "..", "examples", "human-transfer-daily"))
	if err != nil {
		t.Fatalf("a prerequisite must not fail validation: %v\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "✓ pipecat (pipecat)") {
		t.Errorf("stdout = %q, want the target still passing", stdout)
	}
	for _, want := range []string{"dial-out", "daily_dialout"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q, so the author learns about it from a failed call:\n%s", want, stderr)
		}
	}
	// The author has to be able to act on it, so the page it came from is named.
	if !strings.Contains(stderr, "https://docs.pipecat.ai/") {
		t.Errorf("stderr names no documentation for the prerequisite:\n%s", stderr)
	}
}

// The other half of the rule, and the easy half to get wrong. A prerequisite
// that prints on every Daily compile is a banner, and a banner trains authors to
// ignore stderr.
func TestValidateOmitsPrerequisiteWithoutTheCapabilityThatNeedsIt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agent")
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "..", "examples", "human-transfer-daily"))); err != nil {
		t.Fatal(err)
	}
	// Same Daily route, no transfer and no outbound: nothing dials out.
	path := filepath.Join(dir, "agent.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	trimmed := content[:bytes.Index(content, []byte("controls:"))]
	trimmed = append(trimmed, content[bytes.Index(content, []byte("conversation:")):]...)
	trimmed = bytes.Replace(trimmed, []byte("      - send_to_billing\n"), nil, 1)
	trimmed = bytes.Replace(trimmed, []byte("    tools:\n"), nil, 1)
	if err := os.WriteFile(path, trimmed, 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := runValidateCommand(t, "--target", "pipecat", dir)
	if err != nil {
		t.Fatalf("validate: %v\n%s", err, stderr)
	}
	for _, forbidden := range []string{"dial-out", "daily_dialout"} {
		if strings.Contains(stderr, forbidden) {
			t.Errorf("stderr names %q for a package that never dials out:\n%s", forbidden, stderr)
		}
	}
}

// US4: a region unmute cannot honour fails before any artifact exists, naming the
// problem and the fix.
//
// Note what is deliberately absent. A region *code* is never checked against a
// list: docs/SCHEMA.md N18 and N32 say codes are forwarded exactly as written,
// the platform CLI is the validator, and no list of codes lives in this
// repository, because both platforms change theirs without notice. So the
// refusals here are the three that are knowable without one: an empty entry, the
// same region twice, and more than one region on a platform whose agent names are
// globally unique across regions.
func TestValidateRefusesARegionItCannotHonour(t *testing.T) {
	for _, tc := range []struct {
		name    string
		region  string
		wantAny []string
	}{
		{"several regions", "[us-east, eu-central]", []string{"globally unique across regions"}},
		{"same region twice", "[us-east, us-east]", []string{`lists "us-east" twice`}},
		{"empty entry", `[""]`, []string{"empty entry"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeDailyPackage(t, tc.region)
			_, stderr, err := runValidateCommand(t, "--target", "pipecat", dir)
			if err == nil {
				t.Fatalf("a region unmute cannot honour must fail, got exit 0\n%s", stderr)
			}
			for _, want := range tc.wantAny {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr missing %q:\n%s", want, stderr)
				}
			}
		})
	}
}

// A region code unmute does not recognise still compiles, because the platform is
// the validator. Pinned as a test so nobody "helpfully" adds a region allow-list:
// the platforms add regions outside our release cycle, and a list here would
// refuse a package that is correct.
func TestValidateForwardsAnUnknownRegionCode(t *testing.T) {
	dir := writeDailyPackage(t, "moon-base-1")
	_, stderr, err := runValidateCommand(t, "--target", "pipecat", dir)
	if err != nil {
		t.Fatalf("an unrecognised region code must be forwarded, not refused: %v\n%s", err, stderr)
	}
}

// writeDailyPackage copies the Daily example and rewrites its declared region.
func writeDailyPackage(t *testing.T, region string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "agent")
	source := filepath.Join("..", "..", "examples", "human-transfer-daily")
	if err := os.CopyFS(dir, os.DirFS(source)); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "targets.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content = bytes.Replace(content, []byte("    transport: daily-sip\n"),
		[]byte("    transport: daily-sip\n    deployment_region: "+region+"\n"), 1)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func runValidateCommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := newRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(append([]string{"validate"}, args...))
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

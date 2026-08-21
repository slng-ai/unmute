package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The plane's own behaviour: what it says, when it says it, and where it puts
// what it recorded.

// Gate P4: the dial instruction prints before the run blocks. A run that says
// how to call it only on the way out is a run nobody can call, which is the
// defect this whole feature exists to fix.
//
// What holds it: the instruction has to appear before the teardown does. The
// teardown only runs once the wait is over, so an instruction printed after the
// wait, which is the regression, lands after it and fails here.
func TestPlaneDialInstructionPrintsBeforeTheRunBlocks(t *testing.T) {
	newFakeSIPAdmin(t)
	root, _ := fakeTelephonyRoot(t, strings.Join(sipTestEnv(), "\n"))
	fakeDocker(t, root)
	refuseTunnel(t)
	cmd, out := telephonyTestCommand(t)
	if err := execDevTelephony(cmd, root, "phone", livekitSIPPlan(), composeFiles,
		devTelephonyOptions{botPort: "8083"}); err != nil {
		t.Fatalf("plane run: %v\n%s", err, out.String())
	}
	printed := out.String()
	dial := strings.Index(printed, "dial sip:")
	teardown := strings.Index(printed, "stopping...")
	switch {
	case dial < 0:
		t.Fatalf("the run never printed an address to dial:\n%s", printed)
	case teardown < 0:
		t.Fatalf("the run never tore down, so this proves nothing about ordering:\n%s", printed)
	case dial > teardown:
		t.Errorf("the dial instruction printed after the run had already stopped:\n%s", printed)
	}
	// Everything a caller needs is on the instruction, because the alternative
	// is reading a generated Compose file to find the port.
	for _, want := range []string{"user dev-", "pass ", "registration turned off", "transfers to billing_line", "recordings:"} {
		if !strings.Contains(printed, want) {
			t.Errorf("the dial instruction is missing %q:\n%s", want, printed)
		}
	}
}

// Gate P7, the first half: the recordings go somewhere derived per run, and the
// run says where before it waits. The other half, that they are non-silent when
// audio flowed, needs real audio and belongs to the unattended check.
func TestRecordingPathsAreDerivedPerRunAndPrinted(t *testing.T) {
	// Per run, not per second. Two runs started in the same second sharing a
	// directory would have the second overwrite the first's recordings, and the
	// timestamp alone is only accurate to the second.
	first, second := planeRunID(), planeRunID()
	if first == second {
		t.Errorf("two runs would share the call directory %q", first)
	}

	newFakeSIPAdmin(t)
	root, _ := fakeTelephonyRoot(t, strings.Join(sipTestEnv(), "\n"))
	fakeDocker(t, root)
	refuseTunnel(t)
	var report runReport
	cmd, out := telephonyTestCommand(t)
	if err := execDevTelephony(cmd, root, "phone", livekitSIPPlan(), composeFiles,
		devTelephonyOptions{botPort: "8083", report: &report}); err != nil {
		t.Fatalf("plane run: %v\n%s", err, out.String())
	}
	callDir := filepath.Join(root, "build", "phone", "calls")
	entries, err := os.ReadDir(callDir)
	if err != nil {
		t.Fatalf("the run made no call directory: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("the run made %d call directories, want one for itself", len(entries))
	}
	runDir := filepath.Join(callDir, entries[0].Name())
	if !strings.Contains(out.String(), runDir) {
		t.Errorf("the run never printed where its recordings go (%s):\n%s", runDir, out.String())
	}
	// The directory is made rather than left to Compose. A bind mount whose
	// source is missing is created by the daemon and owned by root, and the
	// endpoint then cannot write into it.
	info, err := os.Stat(runDir)
	if err != nil || !info.IsDir() {
		t.Errorf("this run's call directory is not there for the endpoint to write into: %v", err)
	}
}

// printPlaneRecordings tells three states apart, and the middle one is the
// reason it exists: a file of exactly a header is a leg that recorded nothing,
// and it used to look the same as a leg that recorded silence.
func TestRecordingsPrintTellsEmptyFromRecorded(t *testing.T) {
	dir := t.TempDir()
	plane := &planeRun{callDir: dir}
	plan := livekitSIPPlan()

	// caller.wav: a header and no audio. billing_line.wav: real samples.
	if err := writeWAV(filepath.Join(dir, "caller.wav"), nil, callAudioRate); err != nil {
		t.Fatal(err)
	}
	samples := make([]int16, callAudioRate)
	for i := range samples {
		samples[i] = callFixtureSample(i)
	}
	if err := writeWAV(filepath.Join(dir, "billing_line.wav"), samples, callAudioRate); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	var report runReport
	printPlaneRecordings(&out, "phone", plan, plane, &report)
	printed := out.String()
	if !strings.Contains(printed, "caller.wav is empty") {
		t.Errorf("a recording with no audio in it was reported as a recording:\n%s", printed)
	}
	if !strings.Contains(printed, "recording "+filepath.Join(dir, "billing_line.wav")) {
		t.Errorf("the recording that has audio was not named:\n%s", printed)
	}
	if len(report.Recordings) != 2 {
		t.Errorf("the report holds %d recordings, want both legs", len(report.Recordings))
	}
}

// Gate P8 through the only source a developer's run has: the log stream. The
// seven outcomes are distinguished, and the two that a log cannot support are
// deliberately not claimed.
func TestTransferOutcomesComeFromTheLogStream(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		lines    []string
		outcomes []transferOutcome
		says     []string
		refuses  []string
	}{
		{
			name: "warm reaches the destination and merges",
			lines: []string{
				"INFO agent human transfer fired: to_human (warm)",
				"INFO agent warm transfer dialling out: handing over 12 conversation messages",
				"INFO agent warm transfer merged after 9s: sip_human_1",
			},
			outcomes: []transferOutcome{transferAccepted, transferDestinationReached, transferMerged},
			says:     []string{"transfer requested  shape=warm", "destination reached", "transfer merged"},
		},
		{
			name: "cold ends at accepted, and says so",
			lines: []string{
				"INFO agent human transfer fired: to_human (cold)",
				"INFO agent cold transfer referring the caller out",
				"INFO agent cold transfer completed after 2s",
			},
			outcomes: []transferOutcome{transferAccepted},
			says:     []string{"transfer requested  shape=cold", "referring the caller out", "not acted on by the caller"},
			// A cold transfer must not claim the destination was reached: the
			// caller leaves through the carrier and nothing here can see them
			// arrive. This is research R4, and claiming it is how a silently
			// failed transfer used to read as a success.
			refuses: []string{"destination reached", "transfer merged"},
		},
		{
			name: "nobody took it",
			lines: []string{
				"INFO agent human transfer fired: to_human (warm)",
				"INFO agent warm transfer unavailable after 21s: no answer",
			},
			outcomes: []transferOutcome{transferUnavailableReturn},
			// The cause, verbatim. On a live call "unavailable" alone covered
			// both a destination that never answered and a request refused
			// before anything rang, and telling them apart took a log dive.
			says:    []string{"destination unavailable: no answer"},
			refuses: []string{"transfer merged"},
		},
		{
			name: "a cold transfer the caller could not route",
			lines: []string{
				"INFO agent human transfer fired: to_human (cold)",
				"INFO agent cold transfer referring the caller out",
				`INFO agent cold transfer failed after 30s: ServerError(code=deadline_exceeded)",`,
			},
			outcomes: []transferOutcome{transferUnavailableReturn},
			says: []string{
				"destination unavailable: ServerError(code=deadline_exceeded)",
				"routed by the caller",
				"make rig",
			},
		},
		{
			name: "there was no phone caller to transfer",
			lines: []string{
				"INFO agent human transfer fired: to_human (cold)",
				"INFO agent cold transfer skipped: no phone caller in the room",
			},
			says:    []string{"no phone caller in the room"},
			refuses: []string{"transfer accepted", "destination unavailable"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var out strings.Builder
			var report runReport
			watcher := &transferWatcher{out: &out, targetName: "phone", report: &report}
			// Fed as one write with the lines joined, and then the tail without
			// a newline, because that is how a log pipe actually arrives.
			if _, err := watcher.Write([]byte(strings.Join(testCase.lines, "\n") + "\n")); err != nil {
				t.Fatal(err)
			}
			printed := out.String()
			for _, want := range testCase.says {
				if !strings.Contains(printed, want) {
					t.Errorf("the run never said %q:\n%s", want, printed)
				}
			}
			for _, forbidden := range testCase.refuses {
				if strings.Contains(printed, forbidden) {
					t.Errorf("the run claimed %q, which the log does not support:\n%s", forbidden, printed)
				}
			}
			var got []transferOutcome
			for _, record := range report.Transfers {
				got = append(got, record.Outcome)
			}
			if len(got) != len(testCase.outcomes) {
				t.Fatalf("recorded outcomes %v, want %v", got, testCase.outcomes)
			}
			for i, want := range testCase.outcomes {
				if got[i] != want {
					t.Errorf("outcome %d is %q, want %q", i, got[i], want)
				}
			}
		})
	}
}

// A log line split across two writes still matches. The stream arrives in
// whatever chunks the pipe hands over, and a transfer outcome lost to a chunk
// boundary would be a report that is wrong only sometimes.
func TestTransferWatcherReadsAcrossWriteBoundaries(t *testing.T) {
	var out strings.Builder
	watcher := &transferWatcher{out: &out, targetName: "phone"}
	for _, chunk := range []string{"INFO human transfer fi", "red: to_human (warm)\nINFO warm transfer mer", "ged after 4s: x\n"} {
		if _, err := watcher.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []string{"transfer requested  shape=warm", "transfer merged"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("a line split across writes was missed (%q):\n%s", want, out.String())
		}
	}
}

// The address the plane advertises has to be reachable from both sides of the
// call: a softphone on this machine, and a container on the plane's network.
// Loopback is reachable from only one of them, and the plane's own subnet is
// skipped because its gateway appears as a local interface once the network is
// up.
func TestPlaneAdvertiseAddressIsNeitherLoopbackNorThePlanesOwnSubnet(t *testing.T) {
	address, err := planeAdvertiseAddress("10.185.61.0/24")
	if err != nil {
		t.Skipf("this machine has no private address on any interface: %v", err)
	}
	if strings.HasPrefix(address, "127.") {
		t.Errorf("the plane would advertise %q, which a container cannot route to", address)
	}
	if strings.HasPrefix(address, "10.185.61.") {
		t.Errorf("the plane would advertise %q, which is its own subnet", address)
	}
}

// The plane's values win over the author's. A developer with a real carrier
// configured in .env is the normal case, not the exception, and a default run
// that picked those up would dial a real trunk while claiming to touch nothing.
// So this is SC-004 in its strongest form: not "no carrier value is required"
// but "no carrier value is used".
func TestPlaneOverridesRealCarrierValuesAlreadyInTheEnvironment(t *testing.T) {
	// This test rolls its own fake docker rather than calling fakeDocker, so it
	// has to turn the host-port probe off itself.
	allowHeldPorts(t)
	newFakeSIPAdmin(t)
	// Exactly what an author who has been through the runbook has in .env.
	carrierEnv := strings.Join([]string{
		"TWILIO_SIP_ADDRESS=real.pstn.twilio.com",
		"TWILIO_SIP_USERNAME=real-user",
		"TWILIO_SIP_PASSWORD=real-password-9",
		"TWILIO_PHONE_NUMBER=+34600111222",
		"BILLING_PHONE_NUMBER=+34600333444",
	}, "\n")
	root, trace := fakeTelephonyRoot(t, carrierEnv)
	// A fake docker that echoes the values the containers were handed, which is
	// the only way to read what the run actually passed on rather than what it
	// meant to.
	script := filepath.Join(root, "docker")
	body := "#!/bin/sh\nprintf 'ENV %s %s %s %s\\n' \"$TWILIO_SIP_ADDRESS\" \"$TWILIO_SIP_PASSWORD\" " +
		"\"$TWILIO_PHONE_NUMBER\" \"$BILLING_PHONE_NUMBER\" >> \"$TRACE_FILE\"\n" +
		"case \"$*\" in *' logs '*) kill -INT $$;; esac\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	restoreCmd, restorePreflight := composeCommand, composePreflight
	composeCommand = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, script, args...)
	}
	composePreflight = func(context.Context, []string) error { return nil }
	t.Cleanup(func() { composeCommand, composePreflight = restoreCmd, restorePreflight })
	refuseTunnel(t)

	cmd, out := telephonyTestCommand(t)
	if err := execDevTelephony(cmd, root, "phone", livekitSIPPlan(), composeFiles,
		devTelephonyOptions{botPort: "8083"}); err != nil {
		t.Fatalf("plane run: %v\n%s", err, out.String())
	}
	raw, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	handed := string(raw)
	if !strings.Contains(handed, "ENV ") {
		t.Fatalf("the fake recorded no environment, so this proves nothing:\n%s", handed)
	}
	for _, carrier := range []string{"real.pstn.twilio.com", "real-password-9", "+34600111222", "+34600333444"} {
		if strings.Contains(handed+out.String(), carrier) {
			t.Errorf("a default run handed on the author's real carrier value %q:\n%s", carrier, handed)
		}
	}
	// And the plane's own values are what went in their place.
	if !strings.Contains(handed, "10.185.61.21") {
		t.Errorf("the containers were not pointed at the plane's endpoint:\n%s", handed)
	}
	if !strings.Contains(handed, planeLocalNumber) {
		t.Errorf("the containers were not given the plane's own number:\n%s", handed)
	}
}

//go:build rig

// The rig on the media websocket plane: the seven steps of
// specs/015-local-telephony-rigs/contracts/local-planes.md, against the real
// emitted agent, with no accounts of any kind.
//
// Two decisions make it possible, and the first had to be corrected by running
// it.
//
// **The agent needs its model names present, and nothing behind them.** The
// contract's step 3 assumed an agent with no model credentials would start and
// simply be unable to speak. It does not: the emitted `bot.py` calls
// `require_env()` at import, so with an empty environment the container exits 1
// with "Missing required environment variables: OPENAI_API_KEY", measured
// 2026-08-20. So the rig package carries **placeholders**, and its provider base
// URL points at a closed loopback port: the names satisfy the import, the values
// authorise nothing, and the agent's provider calls fail immediately and
// locally. Credential-free still means what it says, nobody needs an account,
// and nothing here asserts conversation, which belongs to `make smoke`.
//
// **The transfer is driven through the plane rather than through the agent. The
// stand-in *is* the carrier, so the rig posts the carrier document the agent
// would have posted, to the stand-in's own call-control endpoint. That takes the
// model out of the loop entirely, which is step 4's stated purpose. It also means
// the rig exercises the real bridge, the real recordings and the real outcome
// reporting, none of which depend on anything deciding to transfer.
package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slng-ai/unmute/internal/generate"
)

// rigPackage is the smallest package that can carry a phone call, checked in
// beside this test so a rig run depends on no example staying the way it is.
const rigPackage = "testdata/rig"

// rigBotPort is off the default, because the rig must not fight a developer's
// own `unmute dev` for a port. The host-port probe would refuse either way; this
// means it does not have to.
const rigBotPort = "7899"

// rigRun is one plane brought up for the length of one check.
type rigRun struct {
	outDir  string
	control *mediaControl
	standIn mediaCarrierRun
	project string
	callDir string
}

// TestRigMediaWebsocketPlane is steps 1 to 7, in order, as one test. One test
// rather than seven, because the steps share a live call: splitting them would
// mean bringing the plane up seven times or sharing state between tests, and
// both are worse than a long function with the steps named in it.
func TestRigMediaWebsocketPlane(t *testing.T) {
	requireContainerRuntime(t)
	rigRequiresNoCredentials(t)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	// --- step 1: bring the plane up ------------------------------------------
	run := rigBringUp(ctx, t)

	// --- step 2: the stand-in calls the agent, and the call is accepted -------
	// Placed as a background call so the transfer in step 4 lands mid-call. The
	// stand-in is the carrier here, so "the caller endpoint dials the plane" and
	// "the plane dials the agent" are the same act.
	type outcome struct {
		result mediaCallResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := run.standIn.call(ctx, planeLocalNumber, planeLocalNumber, 0)
		done <- outcome{result, err}
	}()

	// --- step 3: wait for the call, and assert arrival after it ---------------
	// Arrival and session mechanics, not conversation: with no model credentials
	// the agent cannot speak, and asserting that it does would make this check
	// need an account.
	callID := rigWaitForCall(t, run)

	// --- step 4: drive a transfer through the plane's own interface -----------
	rigPostTransfer(ctx, t, run, callID)

	// --- step 5, 6: the destination answered, the legs ended, nothing was written
	var got outcome
	select {
	case got = <-done:
	case <-ctx.Done():
		t.Fatal("the call never ended; the transfer did not tear the stream down")
	}
	if got.err != nil {
		t.Fatalf("the call failed: %v", got.err)
	}
	rigAssertArrival(t, got.result)
	rigAssertTransfer(t, got.result)
	rigAssertRecordings(t, got.result)

	// --- step 7: tear down ---------------------------------------------------
	// Deferred inside bringUp, so this asserts the result of it rather than doing
	// it: a teardown that only happens when the test passes is not a teardown.
	rigTearDown(ctx, t, run)
}

// rigRequiresNoCredentials is the check's own precondition, and the reason it
// exists at all (SC-007): this must pass on a machine with no accounts.
//
// It cannot assert an empty environment, because the agent will not start without
// its model names. What it asserts instead is that every value is a placeholder,
// which is the property that actually matters: a real key creeping in here would
// make a green rig mean nothing on anybody else's machine, and would do it
// silently.
func rigRequiresNoCredentials(t *testing.T) {
	t.Helper()
	envFile := filepath.Join(rigPackage, ".env")
	contents, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("the rig package has no %s, and the agent will not start without one: %v", envFile, err)
	}
	for _, line := range strings.Split(string(contents), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Errorf("%s: %q is not a name=value line", envFile, line)
			continue
		}
		// A phone number and a loopback URL are not credentials. Everything else
		// has to say in its own value that it is not one.
		switch {
		case strings.HasPrefix(value, "+"), strings.HasPrefix(value, "http://127.0.0.1"):
		case strings.Contains(value, "placeholder"):
		default:
			t.Errorf("%s: %s carries something not marked a placeholder. The rig must need no "+
				"account: a real credential here makes a green run mean nothing anywhere else", envFile, name)
		}
	}
}

// rigBringUp is step 1: compile, start the plane, start the agent. Teardown is
// registered here so it runs however the test ends.
func rigBringUp(ctx context.Context, t *testing.T) *rigRun {
	t.Helper()
	outDir := filepath.Join(rigPackage, "build", "pipecat")
	if err := exec.CommandContext(ctx, rigBinary(t), "compile", rigPackage).Run(); err != nil {
		t.Fatalf("compile the rig package: %v", err)
	}

	// The plane, built the way the command builds it: a real control server on
	// loopback, real minted credentials, and the container flag set because this
	// route's agent runs in one.
	plan := rigRuntimePlan(t, outDir)
	planeRun, err := startMediaPlaneRun(plan, rigBotPort, true)
	if err != nil {
		t.Fatalf("start the plane: %v", err)
	}
	t.Cleanup(planeRun.stop)
	if _, err := planeRun.prepare(outDir); err != nil {
		t.Fatalf("prepare the call directory: %v", err)
	}

	project := composeProjectName(rigPackage, "pipecat")
	// The same environment the command builds, which is the package's own .env on
	// top of the host's. Built by hand at first, from os.Environ() alone, and the
	// container exited 1 on the model name the .env carries: the rig has to use
	// the product's own assembly or it tests a run nobody performs.
	env := planeRun.apply(append(devChildEnv(rigPackage, io.Discard),
		"UNMUTE_TELEPHONY_PORT="+rigBotPort))
	compose := exec.CommandContext(ctx, "docker",
		composeArgs(filepath.Join(outDir, "compose.telephony.yaml"), project, "up", "--build", "--wait")...)
	compose.Env = env
	if output, err := compose.CombinedOutput(); err != nil {
		// The agent's own reason, which is in the container's log rather than in
		// Compose's exit status. Fetched here because a rig that says only "exit
		// status 1" makes the reader go and do this by hand.
		logs := exec.Command("docker", composeArgs(
			filepath.Join(outDir, "compose.telephony.yaml"), project, "logs", "--tail", "25", "application")...)
		logs.Env = env
		reason, _ := logs.CombinedOutput()
		t.Fatalf("bring the plane up: %v\n%s\n--- the agent said ---\n%s", err, output, reason)
	}
	t.Cleanup(func() {
		// Not the test's teardown assertion, which is step 7: this is the safety
		// net for a test that failed before reaching it.
		down := exec.Command("docker", composeArgs(
			filepath.Join(outDir, "compose.telephony.yaml"), project, "down", "--volumes")...)
		down.Env = env
		_ = down.Run()
	})

	standIn := planeRun.standIn()
	// No microphone: there is nobody here to talk, and a rig that opened an audio
	// device would pass or fail on what the room sounds like.
	standIn.forceFixture = true
	return &rigRun{
		outDir: outDir, control: planeRun.control,
		standIn: standIn, project: project, callDir: planeRun.callDir,
	}
}

// rigWaitForCall waits until the plane has a call to transfer, and returns its
// id. It does **not** prove the agent joined: the stand-in registers a call
// before it places one, so this only says the call exists.
//
// The arrival assertion is rigAssertArrival, made after the call, from the bytes
// that actually reached the agent. That is the only evidence available here, and
// it is real evidence: bytes cannot reach an agent that never took the stream.
func rigWaitForCall(t *testing.T, run *rigRun) string {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		run.control.mu.Lock()
		var found string
		for id := range run.control.live {
			found = id
		}
		run.control.mu.Unlock()
		if found != "" {
			return found
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("no call reached the plane within 90s; the agent never took the stream")
	return ""
}

// rigPostTransfer is step 4: the carrier document, posted by the rig rather than
// by the agent, so the check needs no model.
func rigPostTransfer(ctx context.Context, t *testing.T, run *rigRun, callID string) {
	t.Helper()
	// Give the call a moment of audio first, so the destination bridge has
	// something to carry and step 5's assertion means something.
	time.Sleep(3 * time.Second)

	form := url.Values{"Twiml": {emittedColdTransferDocument}}
	// loopback, not base: base carries the name Docker resolves to the host for
	// the container's benefit, and that name does not resolve on the host. Found
	// by running it, with a "no such host" on the very address the agent uses
	// successfully.
	endpoint := rigControlAddress(run.control) + carrierCallPath + run.control.accountSID + "/Calls/" + callID + ".json"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.SetBasicAuth(run.control.accountSID, run.control.authToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("the plane's call control is unreachable: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("the plane refused the transfer document with %s", response.Status)
	}
}

// rigAssertArrival is step 3's real assertion: the agent took the stream and read
// what was sent to it. Bytes to the agent are the evidence, because a webhook that
// answered and a socket that opened are both things a dead agent can do; frames
// being consumed off it are not.
func rigAssertArrival(t *testing.T, result mediaCallResult) {
	t.Helper()
	if result.BytesToAgent == 0 {
		t.Fatal("no audio reached the agent, so the stream never carried anything: " +
			"the webhook may have answered and the socket may have opened, and neither is arrival")
	}
	// And the agent said nothing, which is expected and worth stating: this run
	// has no model credentials. A rig that started passing because the agent
	// spoke would be a rig that had quietly acquired an account.
	if result.BytesFromAgent > 0 {
		t.Logf("the agent sent %d bytes of audio, which means this machine has model credentials "+
			"in the container's environment. The rig is meant to run without any; check .env and "+
			"anything exporting keys into it", result.BytesFromAgent)
	}
}

// rigAssertTransfer is gate S9: requested, accepted, and the caller's leg left
// the agent. Deliberately **not** that the caller reached the destination, which
// in production is the carrier's job and here is nobody's.
func rigAssertTransfer(t *testing.T, result mediaCallResult) {
	t.Helper()
	if result.Transfer == nil {
		t.Fatal("the plane accepted a transfer document and reports no transfer")
	}
	transfer := result.Transfer
	if transfer.Shape != coldTransfer {
		t.Errorf("the transfer is recorded as %s", transfer.Shape)
	}
	if transfer.Outcome != transferDestinationReached {
		t.Errorf("the transfer reached %q, and a carried-out cold transfer reaches %q",
			transfer.Outcome, transferDestinationReached)
	}
	// Checked, but not gated here, and the difference is worth stating. The rig
	// cannot see whether the socket really closed: the bridge ends on its own
	// timer either way, so a StreamCut hardcoded true passes this file. Mutation
	// testing showed exactly that.
	//
	// The gate for it is TestMediaCarrierCarriesOutAColdTransfer in
	// dev_media_control_test.go, which measures when the agent's stream actually
	// ended against when the call returned. This assertion still earns its place
	// as the cheap half: a run that reports StreamCut false has definitely failed.
	if !transfer.StreamCut {
		t.Error("the caller's leg never left the agent, so this was an announcement and not a transfer")
	}
	if !transfer.Ended {
		t.Error("the document's final hangup was not honoured")
	}
	// FR-019's third negative: a transfer reported complete without the
	// destination having been called. The plane is the destination here, so what
	// stands for "was it called" is whether its leg carried audio.
	if transfer.BytesToDestination == 0 {
		t.Error("the destination leg carried nothing, and the run still reported the destination reached")
	}
}

// rigAssertRecordings is gate P7 and the FR-019 negatives about audio: the legs
// exist, the caller's leg is not silent, and a missing file fails rather than
// being skipped.
func rigAssertRecordings(t *testing.T, result mediaCallResult) {
	t.Helper()
	// The caller's own leg has to carry audio: the rig sent it, so silence here
	// means the plane lost it. The agent's leg is expected to be empty, because
	// with no model credentials it cannot speak, and asserting otherwise would
	// make this check need an account.
	samples, rate, err := readWAV(result.CallerSpoke)
	if err != nil {
		t.Fatalf("the caller's leg was not recorded: %v", err)
	}
	if rate != callAudioRate {
		t.Errorf("the caller's recording is %d Hz, and a carrier stream is %d Hz", rate, callAudioRate)
	}
	loud := 0
	for _, sample := range samples {
		if sample > 512 || sample < -512 {
			loud++
		}
	}
	if loud*4 < len(samples) {
		t.Errorf("the caller's recording is mostly silent: %d of %d samples carry any level. "+
			"Silence fails, because a silent leg makes every other assertion about audio meaningless",
			loud, len(samples))
	}
	if result.Transfer != nil {
		if _, _, err := readWAV(result.Transfer.DestinationHeard); err != nil {
			t.Errorf("the destination's leg was not recorded: %v", err)
		}
	}
}

// rigTearDown is step 7: nothing survives. Both halves matter, and the port half
// is the one a stopped-but-not-removed stack passes.
func rigTearDown(ctx context.Context, t *testing.T, run *rigRun) {
	t.Helper()
	env := append(devChildEnv(rigPackage, io.Discard), "UNMUTE_TELEPHONY_PORT="+rigBotPort)
	down := exec.CommandContext(ctx, "docker", composeArgs(
		filepath.Join(run.outDir, "compose.telephony.yaml"), run.project, "down", "--volumes")...)
	down.Env = env
	if output, err := down.CombinedOutput(); err != nil {
		t.Fatalf("tear the plane down: %v\n%s", err, output)
	}

	list := exec.CommandContext(ctx, "docker", "ps", "--format", "{{.Names}}")
	output, err := list.Output()
	if err != nil {
		t.Fatalf("list containers: %v", err)
	}
	for _, name := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if strings.HasPrefix(name, run.project) {
			t.Errorf("container %s survived the run", name)
		}
	}

	// And the port is free again, which is what says the stack was removed rather
	// than merely stopped.
	if err := rejectOccupiedHostPorts([]hostPort{
		{Port: rigBotPort, What: "the rig's agent", MovedBy: "rigBotPort"},
	}); err != nil {
		t.Errorf("a published port survived the run: %v", err)
	}

	// The plane made no request to any carrier, which is the whole claim. Nothing
	// in this run can have made one: the only carrier address the agent was given
	// is the stand-in's, on loopback. Asserted anyway, because it is the property
	// somebody will change by accident.
	if !strings.HasPrefix(run.control.base, "http://") ||
		!strings.Contains(run.control.base, containerHostName) {
		t.Errorf("the agent was told the carrier is at %q, which is not this machine", run.control.base)
	}
}

// loopback is the control endpoint's address as seen from *this* machine, which
// is not what the agent was told. A containerised agent is given a name Docker
// resolves to the host, and that name does not resolve on the host itself: the
// rig hit "no such host" on the very address the agent was using successfully.
//
// It lives here rather than on mediaControl because the rig is its only caller.
// Nothing in the product needs it: the agent uses the name it was given, and the
// stand-in dials the agent rather than itself.
func rigControlAddress(control *mediaControl) string {
	return "http://127.0.0.1:" + control.port
}

// rigBinary is the compiler under test. Built by `make rig` before the tests run,
// so a stale binary cannot make a green rig meaningless.
func rigBinary(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "bin", "unmute"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("no binary at %s; `make rig` builds it first: %v", path, err)
	}
	return path
}

// rigRuntimePlan reads the plan out of the compiled package's own report, rather
// than being written here. A plan written by hand would drift from the compiler
// and the rig would stop testing what ships.
func rigRuntimePlan(t *testing.T, outDir string) *generate.TelephonyRuntimePlan {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(outDir, "compile-report.json"))
	if err != nil {
		t.Fatalf("read the compile report: %v", err)
	}
	var report struct {
		Telephony *generate.TelephonyRuntimePlan `json:"telephony"`
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("parse the compile report: %v", err)
	}
	if report.Telephony == nil {
		t.Fatal("the compiled package carries no telephony plan")
	}
	return report.Telephony
}

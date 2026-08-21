package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The carrier's call-control endpoint, served on loopback (T067).
//
// The document these tests parse is the one the emitted agent builds in
// _transfer_twiml, in internal/generate/templates/pipecat_v1/bot.py.tmpl. That
// template's own gate is TestColdTransferDocumentStaysWhatThePlaneCanRead in
// internal/generate: it holds the emitted shape, and these hold the reading of
// it. Either one alone would let the two drift apart silently.

// The credentials the stand-in mints for a run. The tests present them the way
// every emitted agent does, with basic auth, because the endpoint refuses
// anything else.
const (
	testControlSID   = "ACstandin"
	testControlToken = "tok-standin"
)

// authenticate signs a request the way the emitted agents do: the SDK from its
// own constructor, the raw call-control request from an explicit BasicAuth.
func authenticate(r *http.Request) *http.Request {
	r.SetBasicAuth(testControlSID, testControlToken)
	return r
}

// emittedColdTransferDocument is what the emitted agent posts for a cold
// transfer. Copied from the template, not paraphrased.
const emittedColdTransferDocument = `<Response>` +
	`<Say>Connecting you to a colleague now.</Say>` +
	`<Dial answerOnBridge="true" timeout="30">+15551234567</Dial>` +
	`<Hangup/>` +
	`</Response>`

func TestTransferDocumentReadsWhatTheAgentSends(t *testing.T) {
	instruction, err := parseTransferDocument(emittedColdTransferDocument)
	if err != nil {
		t.Fatalf("the plane cannot read the document the agent sends: %v", err)
	}
	if instruction.Destination != "+15551234567" {
		t.Errorf("destination is %q, so the transfer would dial the wrong place", instruction.Destination)
	}
	if instruction.Say != "Connecting you to a colleague now." {
		t.Errorf("announcement is %q, and the caller hears this one rather than the agent's own voice", instruction.Say)
	}
	if instruction.RingTimeout != 30*time.Second {
		t.Errorf("ring timeout is %s, and the package asked for 30s", instruction.RingTimeout)
	}
	if !instruction.Hangup {
		t.Error("the document ends the caller's leg and the plane did not see it, so the call would run on after the transfer")
	}
}

// A SIP destination is the other form <Dial> takes, and the daily-sip route
// builds exactly that. Read here so the reason it is not served is the route's
// refusal and not the parser quietly failing.
func TestTransferDocumentReadsASIPDestination(t *testing.T) {
	instruction, err := parseTransferDocument(
		`<Response><Dial><Sip>sip:room@example.invalid</Sip></Dial></Response>`)
	if err != nil {
		t.Fatalf("a SIP <Dial> is unreadable: %v", err)
	}
	if instruction.Destination != "sip:room@example.invalid" {
		t.Errorf("destination is %q", instruction.Destination)
	}
	if instruction.Hangup {
		t.Error("a document with no <Hangup/> was read as ending the call, which would hang up a caller nobody asked to hang up")
	}
}

func TestTransferDocumentRefusesWhatItCannotCarryOut(t *testing.T) {
	for name, document := range map[string]string{
		"not xml":        `Connecting you now`,
		"no dial":        `<Response><Say>Hello</Say><Hangup/></Response>`,
		"empty dial":     `<Response><Dial timeout="30"></Dial></Response>`,
		"dial no number": `<Response><Dial><Sip>  </Sip></Dial></Response>`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseTransferDocument(document); err == nil {
				t.Error("this document was accepted, so the plane would report a transfer it cannot make")
			}
		})
	}
}

// postTransfer is the request the emitted agent makes, in its own shape.
func postTransfer(t *testing.T, control *mediaControl, accountSID, callID, document string) *http.Response {
	t.Helper()
	form := url.Values{"Twiml": {document}}
	target := control.base + carrierCallPath + accountSID + "/Calls/" + callID + ".json"
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		target, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := http.DefaultClient.Do(authenticate(request))
	if err != nil {
		t.Fatalf("the agent could not reach the stand-in's call control: %v", err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func TestCallControlDeliversTheDocumentToTheCallItIsAbout(t *testing.T) {
	control, err := startMediaControl(testControlSID, testControlToken, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = control.close() })

	instructions, stop := control.watch("CAlive")
	defer stop()

	if got := postTransfer(t, control, "ACtest", "CAlive", emittedColdTransferDocument); got.StatusCode != http.StatusOK {
		t.Fatalf("the stand-in answered %s, and the agent reports any non-2xx to the caller as a failed transfer", got.Status)
	}
	select {
	case instruction := <-instructions:
		if instruction.Destination != "+15551234567" {
			t.Errorf("the call was handed a transfer to %q", instruction.Destination)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the document was accepted and never reached the call, so the caller waits forever")
	}
}

// A document for a call nobody is on has to fail, not be swallowed. The agent
// reads the status and tells the caller, and a 200 here is the exact shape of
// the bug the user hit: "it says it will transfer me and nothing happens".
func TestCallControlRefusesADocumentForACallThatIsNotLive(t *testing.T) {
	control, err := startMediaControl(testControlSID, testControlToken, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = control.close() })

	got := postTransfer(t, control, "ACtest", "CAghost", emittedColdTransferDocument)
	if got.StatusCode != http.StatusNotFound {
		t.Errorf("a transfer for an unknown call answered %s; the agent would announce a transfer that cannot happen", got.Status)
	}
}

func TestCallControlServesOnlyTheCarriersCallEndpoint(t *testing.T) {
	for path, want := range map[string]bool{
		"/2010-04-01/Accounts/ACx/Calls/CAy.json": true,
		"/2010-04-01/Accounts/ACx/Calls/.json":    false,
		"/2010-04-01/Accounts/ACx/Messages.json":  false,
		"/2010-04-01/Accounts/ACx/Calls/CAy":      false,
		"/":                                       false,
		// Provisioning, which this project never does and the plane must
		// therefore never appear to offer.
		"/2010-04-01/Accounts/ACx/IncomingPhoneNumbers.json": false,
	} {
		if _, ok := carrierCallID(path); ok != want {
			t.Errorf("%s: served=%v, want %v", path, ok, want)
		}
	}
}

// Gate P2 on the transfer path: the emitted agent must be able to reach the
// stand-in instead of the network. This asserts the plane hands it an address it
// can reach, on loopback, and nothing else.
func TestPlaneTellsTheAgentTheCarrierIsOnLoopback(t *testing.T) {
	control, err := startMediaControl(testControlSID, testControlToken, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = control.close() })
	if !strings.HasPrefix(control.base, "http://127.0.0.1:") {
		t.Errorf("the stand-in's address is %q, and a carrier's API on anything but loopback is a "+
			"write leaving this machine (gate P2)", control.base)
	}
	// A live listener, not just a string: the run tells the agent this is where
	// the carrier is, and an address nothing answers on turns every transfer
	// into a connection error the caller hears as silence.
	probe, err := http.NewRequestWithContext(context.Background(), http.MethodGet, control.base+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(authenticate(probe))
	if err != nil {
		t.Fatalf("nothing is listening at the address the agent is given: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("the stand-in answered %s at /, and it serves the two call endpoints", response.Status)
	}
}

// --- the whole transfer, end to end ----------------------------------------

// M3 extended, gate S9 for this plane: a call that gets transferred leaves the
// agent, reaches a destination, and is reported as exactly that far and no
// further.
func TestMediaCarrierCarriesOutAColdTransfer(t *testing.T) {
	control, err := startMediaControl(testControlSID, testControlToken, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = control.close() })

	// The agent asks for the transfer after it has heard the caller, which is
	// the order a real one happens in: the model decides, then it posts.
	agent := newFakeAgent(t, &fakeAgent{token: "tok", replyFrames: 40, transferAfter: 5, control: control})
	run := agent.standIn(t)
	run.control = control
	// Enough caller audio to outlast the call and the whole bridge after it. The
	// default two seconds ran out mid-bridge, which shortened the bridge and made
	// the timing assertion below fail on working code.
	run.fixtureSeconds = 20

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := run.call(ctx, planeLocalNumber, "+15550000001", 8*time.Second)
	returned := time.Now()
	if err != nil {
		t.Fatalf("the call failed: %v", err)
	}
	if result.Transfer == nil {
		t.Fatal("the agent asked for a transfer and the run reports none")
	}
	transfer := result.Transfer
	if transfer.Shape != coldTransfer {
		t.Errorf("the transfer is recorded as %s, and this route emits cold only", transfer.Shape)
	}
	if transfer.Outcome != transferDestinationReached {
		t.Errorf("the transfer got as far as %q, and a carried-out cold transfer reaches %q",
			transfer.Outcome, transferDestinationReached)
	}
	if !transfer.StreamCut {
		t.Error("the caller's leg never left the agent, so this was an announcement and not a transfer")
	}
	// And the agent's stream really ended, at the transfer rather than at the end
	// of the run. StreamCut alone is a claim the plane makes about itself: an
	// earlier version of it was hardcoded true and every assertion here still
	// passed, which is why this asks the agent instead.
	//
	// The bridge runs for mediaPlaneBridgeSeconds after the cut, so a stream that
	// only closed when the call returned is seconds late. Half the bridge is the
	// margin, which is generous in both directions.
	select {
	case ended := <-agent.streamEnded:
		if slack := returned.Sub(ended); slack < mediaPlaneBridgeSeconds*time.Second/2 {
			t.Errorf("the agent's stream ended %s before the call returned, and a transfer cuts it "+
				"a whole bridge earlier: the caller was still on the agent while it was supposedly "+
				"talking to the destination", slack)
		}
	case <-time.After(2 * time.Second):
		t.Error("the agent's stream never ended, so the caller's leg is still on the agent after the transfer")
	}
	if !transfer.Ended {
		t.Error("the document's <Hangup/> was not honoured, so the caller's original leg would run on")
	}
	if transfer.BytesToDestination == 0 {
		t.Error("the destination heard nothing, so the caller was cut off rather than handed over")
	}
	// The third recording, which is the only evidence of the leg that exists
	// after the run ends.
	info, err := os.Stat(transfer.DestinationHeard)
	if err != nil {
		t.Fatalf("the destination's leg was not recorded: %v", err)
	}
	if info.Size() <= wavHeaderSize {
		t.Error("the destination recording is a header and no audio")
	}
	// And the caller kept hearing something after the handoff, which is what
	// tells a bridge from a call that simply ended.
	heard, _, err := readWAV(result.CallerHeard)
	if err != nil {
		t.Fatal(err)
	}
	if len(heard) == 0 {
		t.Error("the caller heard nothing at all")
	}
}

// A cold transfer cannot merge, and the shared record is what enforces it. This
// is the guard against this plane over-reporting: the plane is the carrier and
// the destination, so it could claim a completed handoff, and a run that claimed
// one would be the false completion FR-019 exists to catch.
func TestColdTransferCannotReportAMerge(t *testing.T) {
	transfer := &mediaTransfer{transferRecord: transferRecord{
		Shape: coldTransfer, Destination: "+15551234567", Outcome: transferRequested,
	}}
	if err := transfer.advance(transferAccepted); err != nil {
		t.Fatal(err)
	}
	if err := transfer.advance(transferDestinationReached); err != nil {
		t.Fatal(err)
	}
	if err := transfer.advance(transferMerged); err == nil {
		t.Error("a cold transfer reported a merge, which is a completion no local run can witness")
	}
}

// The other half of the same rule, and the one that makes "destination reached"
// mean something: a transfer whose bridge carried no audio must not report a
// destination that was reached. Without this the plane could report the same
// outcome for a working handoff and for a caller who was cut off, which is the
// false completion FR-019 exists to catch.
func TestATransferThatCarriedNothingDoesNotClaimTheDestination(t *testing.T) {
	control, err := startMediaControl(testControlSID, testControlToken, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = control.close() })

	// The caller runs out of audio at the moment of the transfer, so the bridge
	// opens with nothing to carry. One second of fixture is fifty frames, and the
	// agent posts on the fiftieth.
	agent := newFakeAgent(t, &fakeAgent{token: "tok", replyFrames: 4, transferAfter: 50, control: control})
	run := agent.standIn(t)
	run.control = control
	run.fixtureSeconds = 1

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := run.call(ctx, planeLocalNumber, "+15550000001", 8*time.Second)
	if err != nil {
		t.Fatalf("the call failed: %v", err)
	}
	if result.Transfer == nil {
		t.Fatal("the agent asked for a transfer and the run reports none")
	}
	if result.Transfer.BytesToDestination != 0 {
		t.Fatalf("this case is meant to have an empty bridge and carried %d bytes; the fixture length "+
			"and the transfer point have drifted apart", result.Transfer.BytesToDestination)
	}
	if result.Transfer.Outcome == transferDestinationReached {
		t.Error("a transfer whose destination heard nothing reports the destination as reached")
	}
	if result.Transfer.Outcome != transferAccepted {
		t.Errorf("the transfer is recorded as %q; it was requested and accepted and got no further",
			result.Transfer.Outcome)
	}
}

// --- outbound on the plane (T068) -------------------------------------------

// The agent asks the carrier to place a call, and the stand-in is the far end of
// it. The number is echoed and never dialled, which is exactly what
// printMediaPlaneLocalDial tells the reader.
func TestCallControlPlacesTheCallTheAgentAsksFor(t *testing.T) {
	control, err := startMediaControl(testControlSID, testControlToken, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = control.close() })

	// The document the emitted agent hands the carrier: its own stream address,
	// built from the public URL the plane gave it.
	form := url.Values{
		"To":    {"+15559990000"},
		"From":  {planeLocalNumber},
		"Twiml": {`<Response><Connect><Stream url="wss://127.0.0.1:7860/telephony/ws/tok" /></Connect></Response>`},
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		control.base+carrierCallPath+"ACtest/Calls.json", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := http.DefaultClient.Do(authenticate(request))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("placing a call answered %s, and the agent turns any failure into a 502 for its caller", response.Status)
	}
	// The agent reads sid off this and holds it for the rest of the call, so a
	// transfer later has to be addressable by it.
	var created map[string]any
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if sid, _ := created["sid"].(string); sid == "" {
		t.Error("the created call carries no sid, and the agent reads that field")
	}
	select {
	case placed := <-control.outbound:
		if placed.To != "+15559990000" {
			t.Errorf("the call to place is addressed to %q", placed.To)
		}
		if placed.Address != "wss://127.0.0.1:7860/telephony/ws/tok" {
			t.Errorf("the stream address read out of the document is %q", placed.Address)
		}
		if placed.CallID != created["sid"] {
			t.Errorf("the call the plane will place is %q and the agent was told %q; a transfer "+
				"on this call would be unaddressable", placed.CallID, created["sid"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the call was accepted and never reached the plane, so nothing would dial")
	}
}

// The other two carriers' call creation is redirected here so nothing leaves the
// machine, and this is where they are told the plane cannot place their calls.
// A silent hang is the failure the user actually hit; this is the opposite.
func TestCallControlSaysWhatItCannotPlace(t *testing.T) {
	control, err := startMediaControl(testControlSID, testControlToken, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = control.close() })

	for name, path := range map[string]string{
		// Telnyx call control, redirected.
		"telnyx": "/v2/calls",
		// Plivo builds {base}/{auth_id}/Call/.
		"plivo": "/MAtest/Call/",
	} {
		t.Run(name, func(t *testing.T) {
			request, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
				control.base+path, strings.NewReader("{}"))
			if err != nil {
				t.Fatal(err)
			}
			response, err := http.DefaultClient.Do(authenticate(request))
			if err != nil {
				t.Fatalf("the request did not even reach the stand-in, so it may have left the machine: %v", err)
			}
			defer func() { _ = response.Body.Close() }()
			if response.StatusCode == http.StatusOK || response.StatusCode == http.StatusCreated {
				t.Fatal("the stand-in claimed to have placed a call it cannot place")
			}
			body, err := io.ReadAll(io.LimitReader(response.Body, 1<<16))
			if err != nil {
				t.Fatal(err)
			}
			// The message is the whole point: it is what a reader finds in the
			// agent's log instead of a timeout.
			for _, want := range []string{"is implemented on the local plane yet", "rather than leaving your machine"} {
				if !strings.Contains(string(body), want) {
					t.Errorf("the refusal does not say %q; it said: %s", want, body)
				}
			}
		})
	}
}

// The dial line has to say the number is not dialled. Without it a run reads as
// a real call to a real number, which on this plane it never is.
func TestTheDialLineSaysTheNumberIsNotDialled(t *testing.T) {
	var out strings.Builder
	printMediaPlaneLocalDial(&out, "pipecat", "+15559990000")
	for _, want := range []string{
		"+15559990000", "not the public network", "never", "dialled",
		"proves nothing about which destination",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the dial line does not say %q:\n%s", want, out.String())
		}
	}
}

// --- reachability and authentication ----------------------------------------

// A container's own 127.0.0.1 is the container. The carrier-websocket agent runs
// in one, so on that route the stand-in has to advertise a name Docker resolves
// to the host and to listen where the container can reach it.
//
// Found by reading the emitted Compose file rather than by running it, which is
// the only reason it is not a fifth live bug: the earlier version handed a
// containerised agent a loopback address and every transfer would have failed
// with a connection error the caller heard as silence.
func TestAContainerisedAgentIsGivenAnAddressItCanReach(t *testing.T) {
	loopback, err := startMediaControl(testControlSID, testControlToken, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loopback.close() })
	if !strings.HasPrefix(loopback.base, "http://127.0.0.1:") {
		t.Errorf("a host-process agent is told the carrier is at %q, and loopback is where it should be "+
			"so nothing outside this machine can reach it", loopback.base)
	}

	container, err := startMediaControl(testControlSID, testControlToken, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = container.close() })
	if !strings.HasPrefix(container.base, "http://"+containerHostName+":") {
		t.Errorf("a containerised agent is told the carrier is at %q, which a container cannot reach: "+
			"its own 127.0.0.1 is itself", container.base)
	}
	// And the emitted Compose file has to be what makes that name resolve, or
	// the address is a name nothing answers to.
	compose, err := os.ReadFile(filepath.Join("..", "generate", "templates", "pipecat_v1", "compose.telephony.yaml.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(compose), containerHostName+":host-gateway") {
		t.Errorf("the emitted Compose file does not map %s to the host gateway, so the address the "+
			"stand-in advertises does not resolve inside the container", containerHostName)
	}
}

// The containerised listener binds every interface, so the endpoint that can cut
// a live call must not be open. Every request presents this run's own minted
// credentials, which is both safer and more faithful: the real API authenticates.
func TestCallControlRefusesAnUnauthenticatedRequest(t *testing.T) {
	control, err := startMediaControl(testControlSID, testControlToken, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = control.close() })

	for name, sign := range map[string]func(*http.Request){
		"no credentials": func(*http.Request) {},
		"wrong token":    func(r *http.Request) { r.SetBasicAuth(testControlSID, "guess") },
		"wrong account":  func(r *http.Request) { r.SetBasicAuth("ACsomeoneelse", testControlToken) },
		"empty both":     func(r *http.Request) { r.SetBasicAuth("", "") },
	} {
		t.Run(name, func(t *testing.T) {
			instructions, stop := control.watch("CAlive")
			defer stop()
			form := url.Values{"Twiml": {emittedColdTransferDocument}}
			request, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
				control.base+carrierCallPath+testControlSID+"/Calls/CAlive.json", strings.NewReader(form.Encode()))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			sign(request)
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = response.Body.Close() }()
			if response.StatusCode != http.StatusUnauthorized {
				t.Errorf("answered %s, and this endpoint can end somebody's live call", response.Status)
			}
			select {
			case <-instructions:
				t.Error("an unauthenticated request cut a live call")
			default:
			}
		})
	}
}

// withAudioTools makes this test describe a machine that can carry a person's
// voice, whichever machine it runs on.
func withAudioTools(t *testing.T) {
	t.Helper()
	restore := audioToolLookPath
	audioToolLookPath = func(tool string) (string, error) { return "/usr/bin/" + tool, nil }
	t.Cleanup(func() { audioToolLookPath = restore })
}

// A live call must say it is live. On a machine with audio tools an outbound
// call has no length decided in advance and holds the microphone until it is
// stopped, and the run that printed nothing here was a run you sat in front of
// with your microphone open, wondering.
func TestALiveOutboundCallSaysWhatIsHappening(t *testing.T) {
	request := outboundRequest{CallID: "CAlive", To: "+15559990000", From: planeLocalNumber}

	// A machine with audio tools, stated rather than inherited. Read from the
	// real PATH this asserted the talking banner wherever sox happened to be
	// installed and the recording banner everywhere else, so it passed on a
	// laptop and failed on a runner for being a runner.
	withAudioTools(t)

	var person strings.Builder
	printMediaPlaneAnswered(&person, "pipecat", request, mediaCarrierRun{})
	for _, want := range []string{"+15559990000", "CAlive", "you are the far end", "headphones", "until you stop it"} {
		if !strings.Contains(person.String(), want) {
			t.Errorf("a call a person is on does not say %q:\n%s", want, person.String())
		}
	}

	var fixture strings.Builder
	printMediaPlaneAnswered(&fixture, "pipecat", request, mediaCarrierRun{forceFixture: true})
	for _, want := range []string{"recorded fixture", "runs for"} {
		if !strings.Contains(fixture.String(), want) {
			t.Errorf("a fixture call does not say %q:\n%s", want, fixture.String())
		}
	}
	if strings.Contains(fixture.String(), "you are the far end") {
		t.Error("a fixture call tells the reader to speak into their microphone")
	}
}

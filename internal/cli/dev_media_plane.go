package cli

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"time"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/target"
)

// The media websocket plane's run: what a default `unmute dev --telephony`
// does on the three routes whose carrier streams call audio over a WebSocket.
//
// The shape mirrors planeRun in dev_local_sip.go on purpose, because the two
// planes answer to the same shared obligations (P1 to P8) and a reader who has
// understood one should not have to learn a second vocabulary for the other.
//
// The one real difference is who calls. On the SIP plane a person dials from a
// softphone and talks. Here the carrier *is* the stand-in, so the run places
// the call itself and plays a fixture: there is no microphone in this path.
// printMediaPlaneReady says so, because a plane that looked like it were
// waiting for you to dial would be a plane you waited at forever.

// mediaPlaneCallSeconds is how long the placed call runs. Long enough for a
// greeting and a reply on a cold model, short enough that a default run is not
// something you wait through.
const mediaPlaneCallSeconds = 12

// mediaPlaneFixtureSeconds is how much caller audio exists. Longer than the
// call, so a call never ends because the caller ran out of things to say.
const mediaPlaneFixtureSeconds = 30

// mediaPlaneRun is the plane's decisions for one run.
type mediaPlaneRun struct {
	// inboundPath is the route's own incoming-call path, threaded through
	// because the two routes on this plane do not agree on it.
	inboundPath string
	// token is the shared secret the stand-in signs with and the agent
	// validates. It is a real secret for the length of this run and authorises
	// exactly one thing: talking to a port on this machine.
	token   string
	carrier string
	botPort string
	callDir string
	// supplied is every environment name the plane sets in a carrier's place,
	// so the caller can stop demanding them from the author.
	supplied []string
	env      map[string]string
	// control is the carrier's REST API, served on loopback. A cold transfer
	// arrives there rather than on the media stream, so without it the agent
	// posts to api.twilio.com: a write leaving the machine on a run that is
	// meant to touch nothing, and a transfer the caller was promised and never
	// gets. Found by running it, on examples/salon-concierge.
	control *mediaControl
}

func planeIsMediaWebsocket(plan *generate.TelephonyRuntimePlan) bool {
	return plan != nil && plan.LocalPlane == string(target.LocalPlaneMediaWebsocket)
}

// startMediaPlaneRun mints the run's secret and works out the environment the
// plane supplies so the author needs no carrier account (SC-004).
func startMediaPlaneRun(plan *generate.TelephonyRuntimePlan, botPort string, fromContainer bool) (*mediaPlaneRun, error) {
	token, err := randomCredentialText(24)
	if err != nil {
		return nil, fmt.Errorf("mint the plane's signing token: %w", err)
	}
	run := &mediaPlaneRun{
		// The path most routes on this plane use. The cloud-websocket path
		// overrides it, because its local runner answers at "/".
		inboundPath: "/telephony/inbound",
		token:       token,
		carrier:     plan.Route.Carrier,
		botPort:     botPort,
		env:         map[string]string{},
	}
	set := func(key, value string) {
		if name := plan.Environment[key]; name != "" && value != "" {
			run.env[name] = value
		}
	}
	// The agent reads all of these by name and fails startup on a missing one,
	// so every one is set. They are the plane's own values, not placeholders:
	// the auth token is what both sides sign with, and an account identifier
	// nothing authenticates against may as well say what it is.
	set("account_sid", "AC"+token[:min(len(token), 32)])
	set("auth_token", token)
	set("auth_id", "MA"+token[:min(len(token), 18)])
	set("api_key", token)
	set("public_key", token)
	set("connection_id", "unmute-local-plane")
	set("from_number", planeLocalNumber)
	// Every credential the route requires has to be one the plane replaced. A
	// name left alone is the author's own value reaching the agent, which is
	// two failures at once: the run is no longer carrier-free, and the agent
	// would validate signatures with a token the stand-in is not signing with,
	// so every call is refused with a 403 that names nothing.
	//
	// Refusing here rather than proceeding, because the alternative is a run
	// that looks local and is not.
	// Read from the plan's own connection vocabulary rather than from the route
	// table, so a connection naming a key this plane has never heard of is
	// refused rather than quietly passed through.
	for _, key := range slices.Sorted(maps.Keys(plan.Environment)) {
		name := plan.Environment[key]
		if name == "" {
			continue
		}
		if _, replaced := run.env[name]; !replaced {
			return nil, fmt.Errorf("the plane cannot supply %s, the carrier's %q, so a local run would "+
				"reach the agent with your own carrier credentials instead. The %s %s route needs a "+
				"plane value for it before it can run carrier-free",
				name, key, plan.Route.Provider, plan.Route.Transport)
		}
	}
	// The agent signs over the address it believes is its own, and the stand-in
	// has to sign over the same string. Loopback, because that is where the
	// stand-in dials and there is no tunnel in this plane at all (gate P3).
	//
	// https, and nothing serves it. The emitted agent refuses a public URL that
	// is not an HTTPS origin, which is right for a carrier callback and has no
	// meaning on a plane with no public network. The value is only ever used to
	// build two strings: the URL the agent signs over, and the wss:// stream
	// address it hands back. The stand-in signs over the same strings and dials
	// plain ws on loopback, so nothing connects to this and no TLS exists to get
	// wrong. Found by running it: with http:// the agent never becomes healthy
	// and the run tears itself down.
	run.env["UNMUTE_PUBLIC_URL"] = "https://127.0.0.1:" + botPort
	// Started here rather than at the top, because it authenticates against the
	// same account identifier and token the agent was just handed: the stand-in
	// and the agent have to agree, and deriving them twice is how they stop
	// agreeing.
	control, err := startMediaControl("AC"+token[:min(len(token), 32)], token, fromContainer)
	if err != nil {
		return nil, err
	}
	run.control = control
	// Where the carrier is, which on this run is a listener on loopback. Its
	// presence tells the agent to build its carrier transport itself, because
	// the framework's path cannot be built without real carrier credentials and
	// writes to the carrier's REST API when a call ends. Its value is where call
	// control goes, so a cold transfer replaces the call's document here instead
	// of at api.twilio.com. Both readings are gate P2 (research R7 addendum).
	run.env[target.LocalPlaneEnvName] = control.base
	for name := range run.env {
		run.supplied = append(run.supplied, name)
	}
	slices.Sort(run.supplied)
	return run, nil
}

// prepare makes this run's call directory, beside the package it came from, the
// same place and shape the SIP plane uses.
func (run *mediaPlaneRun) prepare(outDir string) (string, error) {
	runID := planeRunID()
	run.callDir = planeCallDir(outDir, runID)
	if err := os.MkdirAll(run.callDir, 0o755); err != nil {
		return "", fmt.Errorf("make this run's call directory: %w", err)
	}
	return runID, nil
}

// apply puts the plane's environment into the child environment.
func (run *mediaPlaneRun) apply(env []string) []string {
	for _, name := range run.supplied {
		env = setChildEnv(env, name, run.env[name])
	}
	return env
}

// standIn is the carrier stand-in for this run.
func (run *mediaPlaneRun) standIn() mediaCarrierRun {
	return mediaCarrierRun{
		carrier:        run.carrier,
		publicURL:      run.env["UNMUTE_PUBLIC_URL"],
		dial:           "http://127.0.0.1:" + run.botPort,
		authToken:      run.token,
		callDir:        run.callDir,
		fixtureSeconds: mediaPlaneFixtureSeconds,
		inboundPath:    run.inboundPath,
		control:        run.control,
	}
}

// stop releases what the run holds. There is nothing to be done about a
// listener that will not close on the way out of a process that is ending.
func (run *mediaPlaneRun) stop() {
	if run.control != nil {
		_ = run.control.close()
	}
}

// printMediaPlaneReady says what this plane is and, just as importantly, what
// it is not: nothing is waiting for you to dial, because the caller is a
// fixture the run plays.
func printMediaPlaneReady(out io.Writer, targetName string, run *mediaPlaneRun) {
	fmt.Fprint(out, "\n  \033[1;32m▸\033[0m placing a local call through the stand-in\n")
	fmt.Fprintf(out, "    the carrier is this process, on loopback: no tunnel, no number, no account.\n")
	if standIn := run.standIn(); standIn.fixtureOnly() {
		// Said plainly, with the reason, because a run that silently played a
		// fixture at somebody expecting to talk is a confusing run.
		fmt.Fprintf(out, "    the caller is a recorded fixture: %s\n", standIn.fixtureReason())
		fmt.Fprintf(out, "    install it and run this again to talk to the agent yourself.\n")
	} else {
		fmt.Fprintf(out, "    you are the caller: speak into your microphone and listen on your speaker.\n")
		fmt.Fprintf(out, "    use headphones, or the agent will hear itself and interrupt itself.\n")
		fmt.Fprintf(out, "    the call lasts until you stop it.\n")
	}
	fmt.Fprintf(out, "    recordings: %s\n", run.callDir)
	fmt.Fprintf(out, "    %s\n\n", "ctrl-c to stop")
}

// placeMediaPlaneCall runs one call and reports it.
//
// A call that fails is reported and the run stays up, rather than being
// returned as an error that tears the environment down. This is a development
// loop: the reason a call failed is in the log the run is still writing, and
// exiting would take that away at exactly the moment it became interesting.
// The message goes to stderr so it cannot be mistaken for the call having
// worked, and names the log to read.
func placeMediaPlaneCall(ctx context.Context, out, errOut io.Writer, targetName, logPath string, run *mediaPlaneRun) {
	standIn := run.standIn()
	// A person's call runs until they stop it. A fixture's call is as long as the
	// fixture, because nobody is listening and a longer one only costs time.
	length := time.Duration(0)
	if standIn.fixtureOnly() {
		length = mediaPlaneCallSeconds * time.Second
	}
	reportMediaPlaneCall(ctx, out, errOut, targetName, logPath, standIn, planeLocalNumber, planeLocalNumber, length)
}

// reportMediaPlaneCall runs one call and prints it. Shared by the inbound and
// outbound shapes so a run cannot report the same call two different ways.
func reportMediaPlaneCall(ctx context.Context, out, errOut io.Writer, targetName, logPath string,
	standIn mediaCarrierRun, from, to string, length time.Duration,
) {
	result, err := standIn.call(ctx, from, to, length)
	if err != nil {
		fmt.Fprintf(errOut, "%s: the local call did not complete: %v\n", targetName, err)
		fmt.Fprintf(errOut, "%s: the run is still up. What the agent said is in %s\n", targetName, logPath)
		return
	}
	// Seconds, because that is the number a person can judge. "600 frames" is
	// arithmetic; "12.0s out, 3.3s back" says the agent answered.
	seconds := func(bytes int) float64 { return float64(bytes) / float64(callAudioRate) }
	fmt.Fprintf(out, "%s: call complete  ·  %.1fs of caller audio out, %.1fs of agent audio back",
		targetName, seconds(result.BytesToAgent), seconds(result.BytesFromAgent))
	if result.Clears > 0 {
		// A barge-in is the agent cutting its own audio because the caller
		// spoke. Worth printing: it is the one number here that says turn-taking
		// happened at all.
		fmt.Fprintf(out, ", %d barge-in(s)", result.Clears)
	}
	fmt.Fprintln(out)
	printMediaPlaneTransfer(out, targetName, result)
	printMediaPlaneRecordings(out, targetName, result)
}

// printMediaPlaneTransfer says how far a transfer got and, just as importantly,
// where it stopped on purpose. A cold transfer ends at the destination being
// reached: whether a person then picked up their phone is the one part of a
// transfer no local run can prove, and a line claiming otherwise would be the
// false completion FR-019 exists to catch.
func printMediaPlaneTransfer(out io.Writer, targetName string, result mediaCallResult) {
	transfer := result.Transfer
	if transfer == nil {
		return
	}
	fmt.Fprintf(out, "%s: %s transfer to %s  ·  %s\n",
		targetName, transfer.Shape, transfer.Destination, transfer.Outcome)
	if transfer.Announcement != "" {
		// Spoken by the carrier rather than by the agent on this route, which
		// surprises people: the agent's own words come out of the stand-in.
		fmt.Fprintf(out, "%s: the carrier announced %q to the caller\n", targetName, transfer.Announcement)
	}
	if !transfer.StreamCut {
		fmt.Fprintf(out, "%s: the caller's leg never left the agent, so this transfer was announced and not made\n", targetName)
	}
	if transfer.Ended {
		fmt.Fprintf(out, "%s: the document ended the caller's original leg after the dial\n", targetName)
	}
	if transfer.DestinationHeard != "" {
		fmt.Fprintf(out, "%s: recording %s  ·  %.1fs the destination heard\n", targetName,
			transfer.DestinationHeard, float64(transfer.BytesToDestination)/float64(callAudioRate))
	}
	fmt.Fprintf(out, "%s: a local run proves the handoff, never that a person answered\n", targetName)
}

// mediaPlaneOutboundWait is how long the stand-in waits for the agent to ask
// for the call. The agent has to reach its model and its own state store first,
// so this is generous: the failure it guards is an agent that never asks, and
// waiting a few extra seconds costs nothing next to reporting that wrongly.
const mediaPlaneOutboundWait = 30 * time.Second

// printMediaPlaneLocalDial says where a --to number goes on this plane, in the
// same words the SIP plane uses for the same reason: a run that did not say so
// would read as a real call having been placed to a real number.
//
// It is a stronger statement here than on the SIP plane. There, a local endpoint
// answers. Here the stand-in is both the carrier and the far end, so the number
// is not dialled anywhere at all: it is carried in the request, echoed back, and
// recorded. That proves the agent can place a call and that audio flows, and
// nothing whatever about which destination a number reaches.
func printMediaPlaneLocalDial(out io.Writer, targetName, to string) {
	fmt.Fprintf(out, "%s: --to %s addresses an endpoint on this machine, not the public network.\n", targetName, to)
	fmt.Fprintf(out, "%s: the stand-in is both the carrier and the far end, so the number is never\n", targetName)
	fmt.Fprintf(out, "%s: dialled. This proves the agent can place a call and that audio flows in\n", targetName)
	fmt.Fprintf(out, "%s: both directions. It proves nothing about which destination a number reaches.\n", targetName)
}

// placeMediaPlaneOutboundCall runs the outbound shape: trigger the agent, wait
// for it to ask the carrier for a call, then be the far end of that call.
//
// trigger is what pokes the agent, which differs per route, and it runs in the
// background because it does not return until the agent's own handler does, and
// the agent's handler does not return until this stand-in has answered the
// request to place the call. Waiting for it here would deadlock the two.
func placeMediaPlaneOutboundCall(ctx context.Context, out, errOut io.Writer,
	targetName, logPath, to string, run *mediaPlaneRun, trigger func() error,
) {
	printMediaPlaneLocalDial(out, targetName, to)
	triggered := make(chan error, 1)
	go func() { triggered <- trigger() }()

	place := func(request outboundRequest) {
		standIn := run.standIn()
		standIn.outbound = &request
		// Said before the call starts, because on a machine with audio tools
		// this call has no end decided in advance and holds the microphone
		// until you stop it. A run that went quiet here with the microphone
		// open would be a run you sat in front of wondering.
		printMediaPlaneAnswered(out, targetName, request, standIn)
		length := time.Duration(0)
		if standIn.fixtureOnly() {
			length = mediaPlaneCallSeconds * time.Second
		}
		reportMediaPlaneCall(ctx, out, errOut, targetName, logPath, standIn, request.From, request.To, length)
	}

	select {
	case request := <-run.control.outbound:
		place(request)
	case err := <-triggered:
		// Reaching here means the agent finished without ever asking for a call,
		// and it cannot mean the call was missed. The stand-in is handed the call
		// before it answers the agent, so this select is already parked when the
		// call arrives and wakes on the case above.
		//
		// A guard against both cases being ready at once stood here briefly. It
		// was removed: the ordering it defended is one the scheduler does not
		// produce, and the test written for it could not be made to fail even
		// with the guard deleted, which is what a wish looks like.
		//
		// The agent refused before asking for a call. Its own reason is the
		// useful one, so it is printed rather than summarised.
		if err != nil {
			fmt.Fprintf(errOut, "%s: the agent would not place the call: %v\n", targetName, err)
		} else {
			fmt.Fprintf(errOut, "%s: the agent accepted the request and never asked the carrier to "+
				"place a call. What it did instead is in %s\n", targetName, logPath)
		}
	case <-time.After(mediaPlaneOutboundWait):
		fmt.Fprintf(errOut, "%s: no call was asked for within %s. What the agent did is in %s\n",
			targetName, mediaPlaneOutboundWait, logPath)
	case <-ctx.Done():
	}
}

// printMediaPlaneAnswered says the outbound call is up and who is on it, the way
// printMediaPlaneReady does for an inbound one. Both planes owe the reader the
// same thing: what is happening now, and when it will stop.
func printMediaPlaneAnswered(out io.Writer, targetName string, request outboundRequest, standIn mediaCarrierRun) {
	fmt.Fprintf(out, "\n  \033[1;32m▸\033[0m %s answered on the stand-in  (call %s)\n",
		request.To, request.CallID)
	if standIn.fixtureOnly() {
		fmt.Fprintf(out, "    the far end is a recorded fixture: %s\n", standIn.fixtureReason())
		fmt.Fprintf(out, "    install it and run this again to talk to the agent yourself.\n")
		fmt.Fprintf(out, "    the call runs for %ds.\n", mediaPlaneCallSeconds)
	} else {
		fmt.Fprintf(out, "    you are the far end: the agent is calling you. Speak into your\n")
		fmt.Fprintf(out, "    microphone and listen on your speaker, and use headphones or the\n")
		fmt.Fprintf(out, "    agent will hear itself and interrupt itself.\n")
		fmt.Fprintf(out, "    the call lasts until you stop it.\n")
	}
	fmt.Fprintf(out, "    %s\n\n", "ctrl-c to stop")
}

// printMediaPlaneRecordings names both legs and tells an empty one from a
// recorded one, the way the SIP plane's own printer does (gate P7).
func printMediaPlaneRecordings(out io.Writer, targetName string, result mediaCallResult) {
	for _, path := range []string{result.CallerSpoke, result.CallerHeard} {
		info, err := os.Stat(path)
		switch {
		case err != nil:
			continue
		case info.Size() <= wavHeaderSize:
			// A header and no audio. Named as its own outcome because "the leg
			// connected and carried nothing" is a different fault from "the leg
			// never connected", and the file size is the only thing that tells
			// them apart.
			fmt.Fprintf(out, "%s: recording %s is empty, so no audio reached it\n", targetName, path)
		default:
			fmt.Fprintf(out, "%s: recording %s\n", targetName, path)
		}
	}
}

//go:build rig

// The rig on the SIP plane: a real SIP stack in containers, a real call from a
// container endpoint, and a transfer driven through the plane's own interface.
//
// Four things had to be established by running them, and three changed what this
// file does.
//
// **The whole plane comes up healthy with no credentials.** Measured 2026-08-21:
// the agent, LiveKit Server, LiveKit SIP, the coordination store and both
// endpoints reach healthy with an empty environment. Unlike the Pipecat agent,
// this one does not gate startup on its model names.
//
// **A call with no inbound trunk is rejected as a flood.** LiveKit SIP answered
// 486 with reason "flood" on the first INVITE, because the trunk and dispatch
// records are created by `unmute dev --telephony` and not by Compose. So this
// creates them with the product's own function.
//
// **The headless caller endpoint could not place a call at all.** It ran baresip
// with an answer-only account and nothing ever told it to dial, on the profile
// whose whole point is that every leg is a container. Fixed in the plane's
// Compose template with baresip's `-e` startup command.
//
// **An inbound call cannot be established without an answering agent, so the
// transfer is driven on a call the plane places.** The plane holds an inbound
// call at 180 Ringing until the agent joins the room, and rejects it with 486
// when nothing ever does. The emitted agent calls require_env() before it
// connects, so with no model credentials it is dispatched for the room and then
// exits, and the call rings out. Placeholder keys would not fix it either: the
// SLNG plugin takes its host as a constructor argument with no environment
// override, so a fake key reaches api.slng.ai rather than nothing.
//
// So the inbound leg proves what it can prove without an agent, which is that
// the trunk accepted an unsolicited call and the dispatch rule matched it, and
// the established call the transfer needs is one the plane places itself through
// its own outbound API. That needs no agent and no credentials.
//
// **Gate S10 is not achievable here, and this file does not pretend otherwise.**
// A warm transfer's content is the agent holding two legs, briefing one, and
// merging them: that is the agent's own conduct, and driving it "through the
// plane" would move a participant and call it a warm transfer. S10 needs a
// model, so it belongs to `make smoke`. What this covers is S9 and P7.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/ir"
)

// rigSIPPackage is the checked-in example this runs, chosen because it declares
// a transfer and so brings up the plane's destination endpoints.
const rigSIPPackage = "../../examples/livekit-human-transfer"

func TestRigSIPPlane(t *testing.T) {
	requireContainerRuntime(t)

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	outDir := filepath.Join(rigSIPPackage, "build", "livekit")
	if err := exec.CommandContext(ctx, rigBinary(t), "compile", rigSIPPackage).Run(); err != nil {
		t.Fatalf("compile the package: %v", err)
	}
	// Last run's recordings, gone before this one starts. The run directory is
	// named "current" unless UNMUTE_RUN_ID says otherwise, so a leftover
	// destination recording would satisfy step 6 on its own and the check would
	// pass without a transfer having happened at all.
	if err := os.RemoveAll(filepath.Join(outDir, "calls")); err != nil {
		t.Fatal(err)
	}

	// The endpoints' audio, from the product's own function. `compile` does not
	// write it, a run does, and without it Docker's bind mount creates a
	// directory where the file should be: the endpoint then answers the call and
	// fails to start its source, which is the plane looking broken because of a
	// missing file. Found by running this.
	if err := ensurePlaneFixture(outDir, planeFixtureSeconds); err != nil {
		t.Fatal(err)
	}
	plan := rigRuntimePlan(t, outDir)
	project := "unmute-rig-sip"
	compose := filepath.Join(outDir, "compose.telephony.yaml")
	// A port of its own, so a developer's own run does not collide with this.
	env := append(os.Environ(),
		"COMPOSE_PROFILES="+planeProfileHeadless,
		"LIVEKIT_HOST_PORT="+rigSIPControlPort,
		"UNMUTE_TELEPHONY_PORT=8097",
	)
	// The plane's own environment, from the product's own function rather than a
	// copy of its decisions. It carries the number the trunk claims and the
	// caller dials, which the plane has to have: LiveKit refuses a trunk scoped
	// by nothing at all, in those words: "for security, one of the fields must be
	// set: AuthUsername+AuthPassword, AllowedAddresses or Numbers". A real run
	// scopes it with the run's dial credential; the headless caller has no way to
	// present one, so on this profile it is scoped by the number instead. Nothing
	// is weakened: the SIP port is not published here, so the only thing that can
	// reach it is a container on the plane's own network.
	credential, err := newDialCredential()
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range planeEnvironment(plan, credential) {
		env = append(env, name+"="+value)
	}

	// --- step 1: bring the plane up ------------------------------------------
	up := exec.CommandContext(ctx, "docker", composeArgs(compose, project, "up", "--build", "--wait")...)
	up.Env = env
	if output, err := up.CombinedOutput(); err != nil {
		logs := exec.Command("docker", composeArgs(compose, project, "logs", "--tail", "30")...)
		logs.Env = env
		reason, _ := logs.CombinedOutput()
		t.Fatalf("bring the plane up: %v\n%s\n--- the plane said ---\n%s", err, output, reason)
	}
	t.Cleanup(func() {
		down := exec.Command("docker", composeArgs(compose, project, "down", "--volumes")...)
		down.Env = env
		_ = down.Run()
	})

	// --- step 2: the records an unsolicited call needs ------------------------
	// The product's own function, not a copy: a rig that created these itself
	// would stop testing the thing that creates them.
	//
	// No dial credential, deliberately. On this profile the caller is a container
	// on the plane's own network and the SIP port is not published, so there is
	// nobody to keep out; a credential here would make the endpoint's own
	// registration-free account be refused. Gate S3 is about the softphone
	// profile, where the port is published on a real interface.
	if err := ensureLiveKitSIPRecords(ctx, io.Discard, "livekit", plan, env, dialCredential{}); err != nil {
		t.Fatalf("create the plane's trunk and dispatch records: %v", err)
	}

	// The caller has to dial *after* the records exist, and Compose gives it no
	// way to wait: it dials once at startup, and an INVITE arriving before the
	// trunk is rejected as a flood with a 486. Restarting it is the dial.
	//
	// The softphone profile has no such race, because there a person dials after
	// the run has printed the address, which is after the records exist.
	restart := exec.CommandContext(ctx, "docker",
		composeArgs(compose, project, "restart", "telephony_caller")...)
	restart.Env = env
	if output, err := restart.CombinedOutput(); err != nil {
		t.Fatalf("make the caller dial: %v\n%s", err, output)
	}

	// --- step 3: the plane accepted an unsolicited inbound call --------------
	t.Logf("inbound call in room %s", rigSIPWaitForInbound(ctx, t, env, compose, project))

	// --- step 4: an established call, placed by the plane itself -------------
	dial, destination := rigSIPLegs(t, plan)
	room, callee := rigSIPPlaceCall(ctx, t, env, compose, project, plan, credential, dial)
	t.Logf("established call in room %s, callee %s", room, callee)

	// --- step 5: drive the transfer through the plane's own interface ---------
	// Gate S9's cold shape: requested, accepted, and the called leg leaving the
	// room. Deliberately not third-leg completion, which in production is the
	// carrier's job.
	if !t.Run("transfer", func(t *testing.T) {
		rigSIPTransfer(ctx, t, env, room, callee, destination)
	}) {
		rigSIPExplain(t, env, compose, project)
		t.FailNow()
	}

	// --- step 6: the destination answered the leg the transfer created -------
	if !rigSIPWaitForDestination(t, rigSIPCallsDir(t, outDir), destination.Recording) {
		rigSIPExplain(t, env, compose, project)
	}

	// --- step 7: tear down ---------------------------------------------------
	rigSIPTearDown(ctx, t, compose, project, rigSIPControlPort, env)
}

// rigSIPControlPort is LiveKit Server's published control port for the rig. The
// headless profile publishes this one and no others: it carries no media and the
// plane's own API is behind it, which is how this test reaches the plane at all.
const rigSIPControlPort = "7897"

// rigSIPAdmin calls the plane's SIP API over loopback with the product's own
// client, so the request shape is the one that ships. The token is the rig's
// own: see rigSIPToken.
func rigSIPAdmin(ctx context.Context, t *testing.T, env []string, method, room string, payload, result any) error {
	t.Helper()
	client := &sipAdminClient{base: liveKitSIPAdminBase(env), token: rigSIPToken(t, room)}
	if err := client.call(ctx, method, payload, result); err != nil {
		return fmt.Errorf("%s on the plane: %w", method, err)
	}
	return nil
}

// rigSIPRoom calls the plane's room API. Its own helper because the product has
// no room-service caller: nothing it does needs to read a room, and adding one
// for a test's benefit would be a product function with no product caller.
func rigSIPRoom(ctx context.Context, t *testing.T, env []string, method string, payload, result any) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	url := liveKitSIPAdminBase(env) + "/twirp/livekit.RoomService/" + method
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	// The room this call is about, because roomAdmin is granted per room: a token
	// with roomAdmin and no room named is refused with "permissions denied".
	room, _ := payload.(map[string]any)["room"].(string)
	request.Header.Set("Authorization", "Bearer "+rigSIPToken(t, room))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("%s on the plane: %v", method, err)
	}
	defer func() { _ = response.Body.Close() }()
	answer, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("%s on the plane: %s: %s", method, response.Status, answer)
	}
	if err := json.Unmarshal(answer, result); err != nil {
		t.Fatalf("%s answered something unreadable: %v\n%s", method, err, answer)
	}
}

// rigSIPToken mints the token the rig needs, which is wider than any token the
// product mints and deliberately so. `unmute dev` only creates trunks and
// dispatch rules, so it mints sip.admin and nothing else; reading a room and
// moving a call are things the *agent* does with a server SDK, never the CLI.
// Same signer, three more grants, nothing else:
//
//   - roomList to find the call, roomAdmin to read who is on it. roomAdmin is
//     granted per room: a token with roomAdmin and no room named is refused,
//     which is why room is a parameter. Empty for the calls that name no room.
//   - sip.call, which is what TransferSIPParticipant requires. sip.admin does
//     not cover it, and passing an admin token gets "permissions denied".
func rigSIPToken(t *testing.T, room string) string {
	t.Helper()
	claims := struct {
		Iss   string `json:"iss"`
		Sub   string `json:"sub"`
		Nbf   int64  `json:"nbf"`
		Exp   int64  `json:"exp"`
		Video struct {
			RoomAdmin bool   `json:"roomAdmin"`
			RoomList  bool   `json:"roomList"`
			Room      string `json:"room,omitempty"`
		} `json:"video"`
		SIP struct {
			Admin bool `json:"admin"`
			Call  bool `json:"call"`
		} `json:"sip"`
	}{
		Iss: liveKitSIPComposeKey, Sub: "unmute-rig",
		Nbf: time.Now().Add(-time.Minute).Unix(), Exp: time.Now().Add(15 * time.Minute).Unix(),
	}
	claims.Video.RoomAdmin, claims.Video.RoomList = true, true
	claims.Video.Room = room
	claims.SIP.Admin, claims.SIP.Call = true, true
	token, err := signJWT(liveKitSIPComposeSecret, claims)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// rigSIPWaitForInbound is step 3: the caller endpoint dialled and the plane
// took the call. Returns the room.
//
// The evidence is a room named with the dispatch rule's own prefix, holding a
// SIP participant. That is the plane's record of the whole inbound path: the
// trunk accepted an unsolicited call from an endpoint presenting no credential,
// and the dispatch rule matched it and decided this room's name. A rejected call
// produces no room at all, which is what a missing inbound trunk looked like
// (answered 486 with reason "flood").
//
// Deliberately not "and the call is established". The plane holds an inbound
// call ringing until the agent answers, and the agent needs a model, so on this
// leg established is out of reach: see the note at the top of this file. The
// established call the transfer runs on is placed in step 4.
func rigSIPWaitForInbound(ctx context.Context, t *testing.T, env []string, compose, project string) string {
	t.Helper()
	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		var rooms struct {
			Rooms []struct{ Name string } `json:"rooms"`
		}
		rigSIPRoom(ctx, t, env, "ListRooms", map[string]any{}, &rooms)
		for _, entry := range rooms.Rooms {
			if !strings.HasPrefix(entry.Name, sipDispatchRoomPrefix) {
				continue
			}
			var people struct {
				Participants []struct {
					Identity   string            `json:"identity"`
					Kind       string            `json:"kind"`
					Attributes map[string]string `json:"attributes"`
				} `json:"participants"`
			}
			rigSIPRoom(ctx, t, env, "ListParticipants", map[string]any{"room": entry.Name}, &people)
			for _, person := range people.Participants {
				if strings.HasPrefix(person.Identity, "sip_") || person.Kind == "SIP" {
					return entry.Name
				}
			}
		}
		time.Sleep(3 * time.Second)
	}
	t.Errorf("no %s room held a SIP caller within four minutes: the endpoint's call never reached "+
		"the plane", sipDispatchRoomPrefix)
	rigSIPExplain(t, env, compose, project)
	t.FailNow()
	return ""
}

// rigSIPLegs picks the two endpoints the transfer runs between: the plane calls
// the first and the transfer sends it to the second. Both are destination
// endpoints, because they are the ones that answer unattended, and which one is
// which is only a matter of what the plane is told to dial.
func rigSIPLegs(t *testing.T, plan *generate.TelephonyRuntimePlan) (dial, destination ir.TelephonyLocalEndpoint) {
	t.Helper()
	ends := planeEndpoints(plan, ir.TelephonyRoleDestination)
	if len(ends) < 2 {
		t.Fatalf("this needs a package declaring two transfer destinations, one to call and one to "+
			"transfer to, and %s declares %d", rigSIPPackage, len(ends))
	}
	return ends[0], ends[1]
}

// rigSIPPlaceCall is step 4: the plane places a call of its own and an endpoint
// answers it, which is the only way to get an established call here without a
// model. It is also the outbound half of the route, through the plane's own API
// and the plane's own trunk values.
//
// Established is read from the participant's sip.callStatus attribute, which
// LiveKit sets to "active" once the call has connected. Waiting for it is not
// belt and braces: a transfer requested while the call is still ringing is
// refused with "can't transfer non established call", measured 2026-08-21.
func rigSIPPlaceCall(ctx context.Context, t *testing.T, env []string, compose, project string,
	plan *generate.TelephonyRuntimePlan, credential dialCredential, dial ir.TelephonyLocalEndpoint,
) (room, identity string) {
	t.Helper()
	room, identity = "rig-outbound", "rig_callee"
	// The trunk the product configured for this plane, not a second opinion:
	// hostname from planeTrunkHost, credential from the run. The endpoint checks
	// no credential, which is why the same call works with the trunk record
	// scoped by number.
	if err := rigSIPAdmin(ctx, t, env, "CreateSIPParticipant", room, map[string]any{
		"room_name":            room,
		"trunk":                map[string]any{"hostname": planeTrunkHost(plan), "auth_username": credential.Username, "auth_password": credential.Password},
		"sip_number":           planeLocalNumber,
		"sip_call_to":          dial.Name,
		"participant_identity": identity,
	}, &map[string]any{}); err != nil {
		t.Fatalf("ask the plane to place the call: %v", err)
	}

	deadline := time.Now().Add(90 * time.Second)
	status := "no participant at all"
	for time.Now().Before(deadline) {
		var people struct {
			Participants []struct {
				Identity   string            `json:"identity"`
				Attributes map[string]string `json:"attributes"`
			} `json:"participants"`
		}
		rigSIPRoom(ctx, t, env, "ListParticipants", map[string]any{"room": room}, &people)
		for _, person := range people.Participants {
			if person.Identity != identity {
				continue
			}
			if person.Attributes["sip.callStatus"] == "active" {
				return room, identity
			}
			status = fmt.Sprintf("%v", person.Attributes)
		}
		time.Sleep(2 * time.Second)
	}
	t.Errorf("the plane placed a call to %s and it never became established, so nothing answered "+
		"it. the participant said: %s", planeEndpointURI(dial), status)
	rigSIPExplain(t, env, compose, project)
	t.FailNow()
	return "", ""
}

// rigSIPExplain puts the containers' own account of the run in the failure. The
// reason is always in there, so it is fetched rather than pointed at: a check
// that says only "it did not work" costs whoever reads it the same hour twice.
// Causes seen so far: a missing inbound trunk (answered 486 flood), a caller
// endpoint with nothing telling it to dial, and a transfer requested before the
// call was established.
func rigSIPExplain(t *testing.T, env []string, compose, project string) {
	t.Helper()
	for _, service := range []string{
		"telephony_caller", "telephony_destinations", "livekit_sip_headless", "application",
	} {
		logs := exec.Command("docker", composeArgs(compose, project, "logs", "--tail", "25", service)...)
		logs.Env = env
		output, _ := logs.CombinedOutput()
		t.Logf("--- %s ---\n%s", service, output)
	}
}

// rigSIPTransfer is step 4, gate S9: the transfer goes out through the plane's
// own interface rather than through the agent's language model, which is what
// lets this run with no credentials.
func rigSIPTransfer(ctx context.Context, t *testing.T, env []string,
	room, caller string, destination ir.TelephonyLocalEndpoint,
) {
	t.Helper()
	var moved map[string]any
	// The request's own status is not the gate, its effects are. LiveKit answers
	// this with 408 "transaction failed to complete (0 intermediate responses)"
	// on some runs and 200 on others, for a REFER the endpoint demonstrably acts
	// on: measured 2026-08-21, with the endpoint's log showing it dial the target
	// and the target answer in the same run the API reported a timeout. A real
	// carrier answers a REFER the way LiveKit expects, so treating the timeout as
	// a failure here would fail the check on the endpoint's manners.
	if err := rigSIPAdmin(ctx, t, env, "TransferSIPParticipant", room, map[string]any{
		"room_name": room, "participant_identity": caller,
		"transfer_to": planeEndpointURI(destination), "play_dialtone": true,
	}, &moved); err != nil {
		t.Logf("the plane reported %v on the transfer request: its effects are checked below", err)
	}

	// And the caller's leg left the agent, which is the half that a transfer
	// which was only announced would fail. A bounded wait, because the move is
	// not instant.
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		var people struct {
			Participants []struct {
				Identity string `json:"identity"`
			} `json:"participants"`
		}
		rigSIPRoom(ctx, t, env, "ListParticipants", map[string]any{"room": room}, &people)
		stillThere := false
		for _, person := range people.Participants {
			if person.Identity == caller {
				stillThere = true
			}
		}
		if !stillThere {
			return
		}
		time.Sleep(3 * time.Second)
	}
	t.Error("the caller is still in the agent's room 90s after the transfer, so the transfer was " +
		"accepted and never carried out")
}

// rigSIPCallsDir is the directory this run's recordings are in: the newest one
// under calls/, which is what the run's own directory name orders by.
func rigSIPCallsDir(t *testing.T, outDir string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(outDir, "calls"))
	if err != nil {
		t.Fatalf("no recordings directory: %v", err)
	}
	var newest string
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() > newest {
			newest = entry.Name()
		}
	}
	if newest == "" {
		t.Fatal("no run directory under calls/, so no leg was recorded at all")
	}
	return filepath.Join(outDir, "calls", newest)
}

// rigSIPWaitForDestination is gate P7's reachable half on this plane: the
// transfer produced a real second leg and the destination answered it. The
// endpoint opens its recording only when a call reaches it, so the file
// appearing is the endpoint's own record of having answered, and it appears
// nowhere in a run where the transfer went out and nothing happened.
//
// Deliberately not "and that leg carried audio". Measured 2026-08-21: baresip
// puts the call it places from a REFER on hold, the destination sees the hold as
// "re-INVITE (SDP Offer) audio-video: recvonly-inactive", and over a minute both
// endpoints' recordings stay flat zero in both directions. Holding is the
// endpoint's own conduct rather than anything the plane did, and taking it off
// hold would mean driving baresip and then checking what baresip does. The audio
// half of P7 is covered where a leg does carry audio without a model: the
// carrier stand-in on the media websocket plane, in rig_media_test.go.
func rigSIPWaitForDestination(t *testing.T, dir, recording string) bool {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		if info, err := os.Stat(filepath.Join(dir, recording)); err == nil && info.Size() >= wavHeaderSize {
			return true
		}
		if time.Now().After(deadline) {
			files, _ := os.ReadDir(dir)
			names := make([]string, 0, len(files))
			for _, file := range files {
				names = append(names, file.Name())
			}
			t.Errorf("%s never appeared in a minute, so nothing answered the transfer. the run "+
				"recorded %s", recording, strings.Join(names, " "))
			return false
		}
		time.Sleep(2 * time.Second)
	}
}

// rigSIPTearDown is step 7: nothing survives, and the port is free again, which
// is what tells a removed stack from a merely stopped one.
func rigSIPTearDown(ctx context.Context, t *testing.T, compose, project, controlPort string, env []string) {
	t.Helper()
	down := exec.CommandContext(ctx, "docker", composeArgs(compose, project, "down", "--volumes")...)
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
		if strings.HasPrefix(name, project) {
			t.Errorf("container %s survived the run", name)
		}
	}
	if err := rejectOccupiedHostPorts([]hostPort{
		{Port: controlPort, What: "the rig's LiveKit Server", MovedBy: "this case's control-port constant"},
	}); err != nil {
		t.Errorf("a published port survived the run: %v", err)
	}
}

// rigPipecatSIPPackage is the same plane with a Pipecat bot on it instead of a
// LiveKit worker, which is the route T086 made work. Its own fixture rather than
// an example, because there is no example package on this route.
const rigPipecatSIPPackage = "testdata/rig-sip"

// TestRigPipecatSIPInbound is the one thing the LiveKit route's own inbound leg
// cannot establish: a call that is actually answered, with no accounts.
//
// It can be established here for a reason worth stating, because it is what makes
// this check exist. An inbound call is answered when something in the room
// publishes audio the SIP service can subscribe to (livekit/sip
// pkg/sip/inbound.go waits in waitSubscribe while it keeps ringing), and Pipecat's
// LiveKit transport publishes its microphone track the moment it connects. The
// emitted bot joins the room *before* it constructs a single model service, so the
// call is answered on placeholder credentials and only then does the session fail
// on its provider. This asserts the answer and nothing about the conversation.
//
// Which means the whole inbound path is under test in one assertion: the trunk
// accepted an unsolicited call, the dispatch rule matched it and named a room, the
// server announced that room, the emitted application verified the announcement
// and started a session, and the session joined and answered. Before T086 the
// route could do none of it: there was no application to announce to, so every
// call rang for three minutes and was cut off.
func TestRigPipecatSIPInbound(t *testing.T) {
	requireContainerRuntime(t)

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	outDir := filepath.Join(rigPipecatSIPPackage, "build", "pipecat")
	if err := exec.CommandContext(ctx, rigBinary(t), "compile", rigPipecatSIPPackage).Run(); err != nil {
		t.Fatalf("compile the package: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(outDir, "calls")); err != nil {
		t.Fatal(err)
	}
	if err := ensurePlaneFixture(outDir, planeFixtureSeconds); err != nil {
		t.Fatal(err)
	}
	plan := rigRuntimePlan(t, outDir)
	project := "unmute-rig-pipecat-sip"
	compose := filepath.Join(outDir, "compose.telephony.yaml")
	// Ports of its own, so this and the LiveKit case can both be run.
	env := append(devChildEnv(rigPipecatSIPPackage, io.Discard),
		"COMPOSE_PROFILES="+planeProfileHeadless,
		"LIVEKIT_HOST_PORT="+rigPipecatSIPControlPort,
		"UNMUTE_TELEPHONY_PORT=8098",
	)
	credential, err := newDialCredential()
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range planeEnvironment(plan, credential) {
		env = append(env, name+"="+value)
	}

	// --- the plane, including this route's own application ---------------------
	up := exec.CommandContext(ctx, "docker", composeArgs(compose, project, "up", "--build", "--wait")...)
	up.Env = env
	if output, err := up.CombinedOutput(); err != nil {
		logs := exec.Command("docker", composeArgs(compose, project, "logs", "--tail", "40")...)
		logs.Env = env
		reason, _ := logs.CombinedOutput()
		t.Fatalf("bring the plane up: %v\n%s\n--- the plane said ---\n%s", err, output, reason)
	}
	t.Cleanup(func() {
		down := exec.Command("docker", composeArgs(compose, project, "down", "--volumes")...)
		down.Env = env
		_ = down.Run()
	})

	// --- the records an unsolicited call needs, from the product --------------
	// No dial credential, for the reason the LiveKit case gives: on this profile
	// the caller is a container on a private network and the SIP port is not
	// published, so the trunk is scoped by number instead.
	if err := ensureLiveKitSIPRecords(ctx, io.Discard, "pipecat", plan, env, dialCredential{}); err != nil {
		t.Fatalf("create the plane's trunk and dispatch records: %v", err)
	}
	// The caller dials once at startup, so it has to be restarted now the records
	// exist: an INVITE arriving before the trunk is rejected as a flood.
	restart := exec.CommandContext(ctx, "docker",
		composeArgs(compose, project, "restart", "telephony_caller")...)
	restart.Env = env
	if output, err := restart.CombinedOutput(); err != nil {
		t.Fatalf("make the caller dial: %v\n%s", err, output)
	}

	// --- the call was answered ------------------------------------------------
	room := rigPipecatSIPWaitForActive(ctx, t, env, compose, project)
	t.Logf("inbound call established in room %s", room)

	// --- and it was this route's own webhook that did it ----------------------
	// Established alone would not say which mechanism established it. This names
	// the one under test: the application's own log line for the announcement it
	// verified and acted on.
	logs := exec.CommandContext(ctx, "docker", composeArgs(compose, project, "logs", "application")...)
	logs.Env = env
	said, _ := logs.CombinedOutput()
	if !strings.Contains(string(said), "call arrived in room "+room) {
		t.Errorf("the call was established but the application never reported the room "+
			"announcement that starts a session, so something else answered it:\n%s", said)
	}

	rigSIPTearDown(ctx, t, compose, project, rigPipecatSIPControlPort, env)
}

// rigPipecatSIPControlPort is this case's own published control port.
const rigPipecatSIPControlPort = "7898"

// rigPipecatSIPWaitForActive waits for an inbound SIP participant whose call is
// established, and returns its room.
//
// "Established", not "present": a room holding a ringing caller is what this
// route produced before T086 and for three minutes it looks identical. The
// sip.callStatus attribute is the difference, and it is the whole point.
func rigPipecatSIPWaitForActive(ctx context.Context, t *testing.T, env []string, compose, project string) string {
	t.Helper()
	deadline := time.Now().Add(4 * time.Minute)
	status := "no SIP participant at all"
	for time.Now().Before(deadline) {
		var rooms struct {
			Rooms []struct{ Name string } `json:"rooms"`
		}
		rigSIPRoom(ctx, t, env, "ListRooms", map[string]any{}, &rooms)
		for _, entry := range rooms.Rooms {
			if !strings.HasPrefix(entry.Name, sipDispatchRoomPrefix) {
				continue
			}
			var people struct {
				Participants []struct {
					Identity   string            `json:"identity"`
					Kind       string            `json:"kind"`
					Attributes map[string]string `json:"attributes"`
				} `json:"participants"`
			}
			rigSIPRoom(ctx, t, env, "ListParticipants", map[string]any{"room": entry.Name}, &people)
			for _, person := range people.Participants {
				if !strings.HasPrefix(person.Identity, "sip_") && person.Kind != "SIP" {
					continue
				}
				if person.Attributes["sip.callStatus"] == "active" {
					return entry.Name
				}
				status = fmt.Sprintf("%s in %s: %v", person.Identity, entry.Name, person.Attributes)
			}
		}
		time.Sleep(2 * time.Second)
	}
	t.Errorf("no inbound call was established within four minutes, so nothing joined the "+
		"caller's room and answered them. the participant said: %s", status)
	rigSIPExplain(t, env, compose, project)
	t.FailNow()
	return ""
}

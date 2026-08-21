package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// The self-hosted trunk route (US3): Pipecat on the same SIP plane the LiveKit
// target uses, with no managed platform anywhere in it.

// pipecatSIPRoutes is every row of the new route, read from the table so a new
// carrier is covered without editing this file.
func pipecatSIPRoutes(t *testing.T) []target.TelephonyRoute {
	t.Helper()
	var routes []target.TelephonyRoute
	for _, route := range target.SelectableTelephonyRoutes() {
		if route.Key.Provider == target.Pipecat && route.Key.Transport == "sip" {
			routes = append(routes, route)
		}
	}
	if len(routes) == 0 {
		t.Fatal("no (pipecat, sip) route in the table")
	}
	return routes
}

// T077: the artifact set, which on this route is defined as much by what is
// absent as by what is present. No deployment manifest and no carrier adapter,
// because there is no managed platform and no carrier ever calls us: the trunk
// terminates at the plane and the agent joins a room.
func TestPipecatSIPEmitsTheSelfHostedArtifactSet(t *testing.T) {
	for _, route := range pipecatSIPRoutes(t) {
		name := route.Key.Transport + "_" + route.Key.Carrier
		t.Run(name, func(t *testing.T) {
			artifact, err := telephonyRouteArtifact(t, route.Key)
			if err != nil {
				t.Fatalf("%v", err)
			}
			present := map[string]bool{}
			for _, file := range artifact.Files {
				present[file.Path] = true
			}
			for _, want := range []string{
				"bot.py", "pyproject.toml", "Dockerfile", "README.md", ".env.example",
				// The plane it runs on, and the plane's own endpoints.
				"compose.telephony.yaml", "endpoint.Dockerfile", "baresip.conf",
				// The application the container's own command starts. This was
				// absent for as long as the route existed, and the route could
				// not take a call: the Dockerfile's CMD is `uvicorn
				// telephony:app`, so the container exited on a missing module and
				// the readiness probe the Compose healthcheck waits on could never
				// pass. It used to be asserted absent, which is how it stayed that
				// way.
				"telephony.py",
				// The two records an inbound call needs in production, which the
				// emitted setup script feeds to `lk` by these exact paths.
				"telephony-setup.sh", "sip-inbound-trunk.json", "sip-dispatch-rule.json",
			} {
				if !present[want] {
					t.Errorf("the route emits no %s", want)
				}
			}
			for _, forbidden := range []string{
				// Gate C2: no managed-platform deployment manifest. Its absence is
				// what the route is for.
				"pcc-deploy.toml",
				// No carrier adapter. telephony.py above is not one: it answers a
				// room announcement from the platform this agent already talks to,
				// and these three are the carrier-route machinery (a media
				// WebSocket, its shared helpers, its admission store) that nothing
				// on this route has any use for.
				"telephony_shared.py", "telephony_state.py", "telephony_helper.py",
			} {
				if present[forbidden] {
					t.Errorf("the route emits %s, which belongs to a route that hosts something of "+
						"the carrier's or deploys to a managed platform", forbidden)
				}
			}
			// The command and the module have to agree, and they are written in
			// different files by different code. Reading the route's own process
			// command rather than restating it is the point: a change to either
			// side fails here.
			var command []string
			for _, process := range route.Processes {
				if process.Name == "agent" {
					command = process.Command
				}
			}
			module := ""
			for _, argument := range command {
				if name, _, found := strings.Cut(argument, ":"); found {
					module = name
				}
			}
			if module == "" {
				t.Fatalf("the route's agent process names no <module>:<app> to import: %v", command)
			}
			if !present[module+".py"] {
				t.Errorf("the container command imports %q and the route emits no %s.py", module, module)
			}
		})
	}
}

// pipecatSIPArtifact builds a package on the new route, optionally declaring the
// cold transfer. The route-table fixture declares none, and the transfer is half
// of what this route is, so it needs its own package.
func pipecatSIPArtifact(t *testing.T, carrier string, transfer bool) Artifact {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	controls := []string{"hangup"}
	if transfer {
		controls = append(controls, "cold_transfer")
		// The declared transfer, which is what actually emits one. The channel's
		// required_controls only says the route must support the shape; it does
		// not ask for a transfer, and a fixture that set only that emitted no
		// transfer at all.
		if pkg.Agent.Destinations == nil {
			pkg.Agent.Destinations = map[string]string{}
		}
		pkg.Agent.Destinations["manager_line"] = "MANAGER_PHONE_NUMBER"
		pkg.Agent.Secrets = append(pkg.Agent.Secrets, "MANAGER_PHONE_NUMBER")
		if pkg.Agent.Controls == nil {
			pkg.Agent.Controls = map[string]spec.Control{}
		}
		pkg.Agent.Controls["to_manager"] = spec.Control{
			Kind: "human_transfer", When: "The caller asks for a person.",
			Cold: &spec.ColdTransfer{
				Destination: "manager_line", RingTimeout: "30s", OnUnavailable: "hangup",
			},
		}
		for name, agent := range pkg.Agent.Agents {
			agent.Tools = append(agent.Tools, "to_manager")
			pkg.Agent.Agents[name] = agent
			break
		}
	}
	// Inbound only: this route emits no dial-out path, so its row grants no
	// outbound and a package asking for one is refused.
	inbound, outbound := true, false
	pkg.Agent.Channels["phone"] = spec.Channel{
		Kind: "telephony", Inbound: &inbound, Outbound: &outbound,
		RequiredControls: controls,
	}
	// One target, because this package's other one is a LiveKit target that would
	// need its own connection and has nothing to do with what is under test.
	configured := pkg.Targets["pipecat"]
	configured.Connection = "trunk"
	pkg.Targets = map[string]spec.Target{"pipecat": configured}
	pkg.Connections = map[string]spec.Connection{"trunk": {
		Transport: "sip", Carrier: carrier,
		Environment: map[string]string{
			"sip_address":  "SIP_TRUNK_HOSTNAME",
			"sip_username": "SIP_AUTH_USERNAME",
			"sip_password": "SIP_AUTH_PASSWORD",
			"from_number":  "SIP_FROM_NUMBER",
		},
	}}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, agent.Targets["pipecat"], target.Default())
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

// The emitted agent joins a room through Pipecat's own LiveKit transport, and
// moves the caller's leg through the platform's transfer API. Both were checked
// against the pinned versions: LiveKitTransport(url, token, room_name, params) in
// pipecat-ai 1.7.0, and TransferSIPParticipantRequest(participant_identity,
// room_name, transfer_to, ringing_timeout) in livekit-api.
func TestPipecatSIPJoinsARoomAndTransfersThroughThePlatform(t *testing.T) {
	for _, route := range pipecatSIPRoutes(t) {
		t.Run(route.Key.Carrier, func(t *testing.T) {
			bot := artifactFile(t, pipecatSIPArtifact(t, route.Key.Carrier, true), "bot.py")
			for _, want := range []string{
				// The transport, and the import that makes it resolve.
				"from pipecat.transports.livekit.transport import LiveKitParams, LiveKitTransport",
				"return LiveKitTransport(",
				// One transport, no selector: the plane is the same stack as
				// production, so there is nothing to switch on.
				"transport = await _sip_transport(runner_args)",
				// The caller's identity, which a transfer needs and which is
				// asked of the room rather than taken from an event.
				"listed = await platform.room.list_participants(ListParticipantsRequest(room=room))",
				"p.kind == ParticipantInfo.Kind.SIP",
			} {
				if !strings.Contains(bot, want) {
					t.Errorf("the emitted agent is missing %q", want)
				}
			}
			// A transport event's participant id is a sid (PA_...) and a transfer
			// names an identity (sip_...). Passing one for the other is a 404
			// "participant does not exist" from the platform, which reads like a
			// vanished caller rather than the wrong argument: measured against a
			// real call 2026-08-21. So the sid must not be kept as the caller.
			if strings.Contains(bot, `call_context["_sip_caller"]`) {
				t.Error("the emitted agent keeps a transport event's participant id as the caller " +
					"to transfer, but that is a sid and the transfer names an identity")
			}
			// And it must not read the media plane's selector: this route's plane
			// is a real SIP stack, so local and deployed are the same code. A
			// selector here would contradict the route's own argument.
			if strings.Contains(bot, target.LocalPlaneEnvName) {
				t.Errorf("the emitted agent reads %s, which nothing on this route sets: its plane is "+
					"the same stack it deploys on", target.LocalPlaneEnvName)
			}
		})
	}
}

// The extra that makes those imports resolve. Without it the emitted project
// installs no LiveKit anything and the container exits on the first import,
// which is a failure nobody would trace back to a dependency list.
func TestPipecatSIPDeclaresTheExtraItsImportsNeed(t *testing.T) {
	for _, route := range pipecatSIPRoutes(t) {
		t.Run(route.Key.Carrier, func(t *testing.T) {
			project, ok := emittedFileFor(t, route, "pyproject.toml")
			if !ok {
				t.Fatal("this route emits no pyproject.toml")
			}
			if !strings.Contains(project, "livekit,") && !strings.Contains(project, ",livekit]") {
				t.Errorf("pyproject.toml does not declare the livekit extra:\n%s", project)
			}
		})
	}
}

// T074: a warm transfer on this route is refused, and the wording matters as much
// as the refusal. The stack underneath *can* do a warm handoff, so a message
// implying a platform limit would be false and would send an author looking for a
// workaround that does not exist. It has to say this project has not built it,
// and name where warm does compile.
func TestPipecatSIPRefusesWarmTransferWithoutBlamingThePlatform(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	inbound, outbound := true, true
	pkg.Agent.Channels["phone"] = spec.Channel{
		Kind: "telephony", Inbound: &inbound, Outbound: &outbound,
		RequiredControls: []string{"hangup"},
	}
	// Added, never replaced: this package's own controls are referenced by its
	// agents, and swapping the map out breaks a reference that has nothing to do
	// with the transfer under test.
	if pkg.Agent.Destinations == nil {
		pkg.Agent.Destinations = map[string]string{}
	}
	pkg.Agent.Destinations["manager_line"] = "MANAGER_PHONE_NUMBER"
	pkg.Agent.Secrets = append(pkg.Agent.Secrets, "MANAGER_PHONE_NUMBER")
	if pkg.Agent.Controls == nil {
		pkg.Agent.Controls = map[string]spec.Control{}
	}
	pkg.Agent.Controls["to_manager"] = spec.Control{
		Kind: "human_transfer", When: "The caller asks for a person.",
		Warm: &spec.WarmTransfer{
			Destination: "manager_line", Briefing: "Who is holding and what they want.",
			RingTimeout: "30s",
		},
	}
	for name, agent := range pkg.Agent.Agents {
		agent.Tools = append(agent.Tools, "to_manager")
		pkg.Agent.Agents[name] = agent
		break
	}
	configured := pkg.Targets["pipecat"]
	configured.Connection = "trunk"
	pkg.Targets = map[string]spec.Target{"pipecat": configured}
	pkg.Connections = map[string]spec.Connection{"trunk": {
		Transport: "sip", Carrier: "twilio",
		Environment: map[string]string{
			"sip_address":  "SIP_TRUNK_HOSTNAME",
			"sip_username": "SIP_AUTH_USERNAME",
			"sip_password": "SIP_AUTH_PASSWORD",
			"from_number":  "SIP_FROM_NUMBER",
		},
	}}

	agent, err := ir.Build(pkg)
	if err != nil {
		// Refused before generation is fine, as long as it says the same things.
		assertWarmRefusalReads(t, err.Error())
		return
	}
	if _, err = Generate(agent, agent.Targets["pipecat"], target.Default()); err == nil {
		t.Fatal("a warm transfer compiled on a route that does not emit one")
	}
	assertWarmRefusalReads(t, err.Error())
}

func assertWarmRefusalReads(t *testing.T, message string) {
	t.Helper()
	for _, want := range []string{
		// Ours, not the platform's.
		"this project has not built warm transfer",
		"The stack this route runs on can do it",
		// And where it does work, so the refusal is a redirection.
		"(livekit, sip)",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("the refusal does not say %q:\n%s", want, message)
		}
	}
	for _, forbidden := range []string{"Pipecat has no warm transfer", "not supported by"} {
		if strings.Contains(message, forbidden) {
			t.Errorf("the refusal blames the platform (%q):\n%s", forbidden, message)
		}
	}
}

// T086: the room webhook. The plane's Compose topology is shared with the
// LiveKit SIP route (one owner), and the two drivers need opposite things from
// it: this driver's agent has to be told a call arrived, and the LiveKit
// driver's agent is put in the room by the dispatch rule and would answer a POST
// with 404. So the block renders here and nowhere else.
//
// Both halves are asserted, because either one alone is satisfiable by doing
// nothing: dropping the block passes the LiveKit half, and rendering it always
// passes this one.
func TestPipecatSIPComposePointsTheServerAtTheRoomWebhook(t *testing.T) {
	for _, route := range pipecatSIPRoutes(t) {
		t.Run(route.Key.Transport+"_"+route.Key.Carrier, func(t *testing.T) {
			artifact, err := telephonyRouteArtifact(t, route.Key)
			if err != nil {
				t.Fatalf("%v", err)
			}
			compose := artifactFile(t, artifact, "compose.telephony.yaml")
			for _, want := range []string{
				"LIVEKIT_CONFIG:",
				"webhook:",
				"api_key: devkey",
				// The address has to be the application's own service and the
				// path the emitted app actually serves.
				"- http://application:7860" + target.SIPRoomWebhookPath,
			} {
				if !strings.Contains(compose, want) {
					t.Errorf("the plane's server is not pointed at the room webhook (%q missing):\n%s", want, compose)
				}
			}
			// And the app serves that path, which is the other end of the same
			// wire and is written in a different file.
			app := artifactFile(t, artifact, "telephony.py")
			if !strings.Contains(app, `@app.post("`+target.SIPRoomWebhookPath+`")`) {
				t.Errorf("the emitted app does not serve %s:\n%s", target.SIPRoomWebhookPath, app)
			}
			// The room prefix both ends agree on, and the dispatch rule writes.
			if !strings.Contains(app, `CALL_ROOM_PREFIX = "`+target.SIPCallRoomPrefix+`"`) {
				t.Errorf("the emitted app does not read the dispatch rule's room prefix:\n%s", app)
			}
		})
	}
}

// The other half: the LiveKit SIP route's own Compose file carries no webhook at
// all. Its golden pins the whole file, so this says which property the golden is
// protecting rather than leaving it to be rediscovered.
func TestLiveKitSIPComposeCarriesNoRoomWebhook(t *testing.T) {
	for _, route := range target.SelectableTelephonyRoutes() {
		if route.Key.Provider != target.LiveKit || route.Key.Transport != "sip" {
			continue
		}
		t.Run(route.Key.Carrier, func(t *testing.T) {
			artifact, err := telephonyRouteArtifact(t, route.Key)
			if err != nil {
				t.Fatalf("%v", err)
			}
			compose := artifactFile(t, artifact, "compose.telephony.yaml")
			for _, forbidden := range []string{"LIVEKIT_CONFIG", "webhook:"} {
				if strings.Contains(compose, forbidden) {
					t.Errorf("this route's agent is dispatched into the room, so a webhook to it "+
						"would 404 on every call, and %q is in its Compose file", forbidden)
				}
			}
		})
	}
}

// T086: the emitted bot greets the caller, and stops when they hang up.
//
// Both halves were wrong and neither was visible offline. The greeting was gated
// on `main.rtvi.event_handler("on_client_ready")`, which no phone call ever
// fires, so the route answered a call and then said nothing for the length of the
// call. The end was gated on `on_client_disconnected`, which Pipecat's LiveKit
// transport does not have: it warns and carries on, so a caller hanging up left
// the session running until the session cap.
//
// The named events are asserted because a wrong one is not an error at runtime.
// `add_event_handler` logs "event handler X not registered" and continues, so a
// handler for an event this transport does not have is dead code that looks
// installed. Reading the log of a real call is what found it.
func TestPipecatSIPBotGreetsAndEndsOnTheEventsItsTransportHas(t *testing.T) {
	for _, route := range pipecatSIPRoutes(t) {
		t.Run(route.Key.Transport+"_"+route.Key.Carrier, func(t *testing.T) {
			artifact, err := telephonyRouteArtifact(t, route.Key)
			if err != nil {
				t.Fatalf("%v", err)
			}
			bot := artifactFile(t, artifact, "bot.py")
			// Events pipecat's LiveKit transport registers (checked against
			// pipecat-ai 1.7.0 transports/livekit/transport.py). A handler for
			// anything else never runs.
			for _, absent := range []string{
				"main.rtvi.event_handler",
				`event_handler("on_client_ready")`,
				`event_handler("on_client_connected")`,
				`event_handler("on_client_disconnected")`,
			} {
				if strings.Contains(bot, absent) {
					t.Errorf("the bot waits on %s, which never arrives on this transport", absent)
				}
			}
			// The greeting hangs off the caller arriving, which is the one event
			// that says there is somebody to greet.
			greeting := strings.Index(bot, `event_handler("on_first_participant_joined")`)
			activate := strings.Index(bot, "await activate_entry()")
			if greeting < 0 || activate < 0 || activate < greeting {
				t.Errorf("nothing reachable on this route activates the entry agent, so the "+
					"call is answered and the agent never speaks (joined=%d activate=%d)", greeting, activate)
			}
			if !strings.Contains(bot, `event_handler("on_participant_disconnected")`) {
				t.Error("nothing ends the run when the caller hangs up, so the session outlives the call")
			}
		})
	}
}

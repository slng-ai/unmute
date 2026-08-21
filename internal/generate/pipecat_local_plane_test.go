package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/target"
)

// The emitted half of the media websocket plane.
//
// Gate P2 says a default run performs no write outside the machine on any exit
// path. Its stated gate reads the run report's carrier-write field, which is
// filled by the CLI's own carrier-configuration function, so it can only see
// writes *this* process makes. It cannot see the one that actually threatened
// P2: the emitted agent's own POST to the carrier's REST API when a call ends.
//
// Measured against pipecat-ai 1.7.0 on 2026-08-20: the framework's transport
// path builds every carrier serializer with `auto_hang_up` left at its default
// of True, which (a) refuses to be constructed without carrier credentials and
// (b) POSTs Status=completed to the carrier when the call ends. Only Twilio's
// serializer accepts a base_url that could redirect that; Telnyx and Plivo
// hardcode api.telnyx.com and api.plivo.com.
//
// So the emitted agent builds the transport itself on a local-plane run, with
// automatic hangup off, and the code that writes outside the machine is never
// constructed. This file is the gate for that, because it is the only place the
// property is visible.

// mediaWebsocketPipecatRoutes is every Pipecat route on the media websocket
// plane, read from the table so a new one is covered without editing this file.
func mediaWebsocketPipecatRoutes(t *testing.T) []target.TelephonyRoute {
	t.Helper()
	var routes []target.TelephonyRoute
	for _, route := range target.SelectableTelephonyRoutes() {
		if route.Key.Provider != target.Pipecat || route.LocalPlane != target.LocalPlaneMediaWebsocket {
			continue
		}
		routes = append(routes, route)
	}
	if len(routes) == 0 {
		t.Fatal("no Pipecat route is on the media websocket plane, so this gate is asserting nothing")
	}
	return routes
}

// emittedFileFor returns one emitted file's content for a route, using the same
// emission path the cloud isolation gates use so the two cannot disagree about
// what a route produces.
func emittedFileFor(t *testing.T, route target.TelephonyRoute, path string) (string, bool) {
	t.Helper()
	artifact, err := telephonyRouteArtifact(t, route.Key)
	if err != nil {
		t.Fatalf("%v", err)
	}
	for _, file := range artifact.Files {
		if file.Path == path {
			return string(file.Content), true
		}
	}
	return "", false
}

func TestLocalPlaneAgentTurnsCarrierHangupOff(t *testing.T) {
	for _, route := range mediaWebsocketPipecatRoutes(t) {
		name := route.Key.Transport + "_" + route.Key.Carrier
		t.Run(name, func(t *testing.T) {
			bot, ok := emittedFileFor(t, route, "bot.py")
			if !ok {
				t.Fatal("this route emits no bot.py")
			}
			// The carrier-websocket routes build the transport themselves on a
			// local run. The cloud-websocket route already did so for its own
			// reason, which the emitted comment records, so either builder
			// satisfies this: what must be true is that some emitted path turns
			// the hangup off, and that no emitted path leaves it on.
			if !strings.Contains(bot, "auto_hang_up=False") {
				t.Errorf("the emitted agent never turns the carrier's automatic hangup off. "+
					"On a local-plane run that means the serializer refuses to be built without "+
					"carrier credentials, and POSTs to the carrier's API when the call ends, which "+
					"is a write leaving the machine and breaks gate P2. Route %s", name)
			}
			if strings.Contains(bot, "auto_hang_up=True") {
				t.Errorf("the emitted agent turns the carrier's automatic hangup on explicitly, "+
					"which no local-plane run can survive. Route %s", name)
			}
		})
	}
}

// The selector is read with os.getenv and compared to "1". If either half drifts
// the emitted agent silently takes the framework's path on a local run, which
// fails as a missing credential rather than as anything that names the cause.
func TestLocalPlaneAgentReadsThePlaneSelector(t *testing.T) {
	for _, route := range mediaWebsocketPipecatRoutes(t) {
		if route.Key.Transport != "carrier-websocket" {
			// The cloud-websocket route reaches its own builder by the shape of
			// the session rather than by this name, which is the behaviour it
			// already had and which this feature did not touch.
			continue
		}
		name := route.Key.Transport + "_" + route.Key.Carrier
		t.Run(name, func(t *testing.T) {
			bot, ok := emittedFileFor(t, route, "bot.py")
			if !ok {
				t.Fatal("this route emits no bot.py")
			}
			if !strings.Contains(bot, `os.getenv("`+target.LocalPlaneEnvName+`")`) {
				t.Errorf("the emitted agent does not read %s, so a local-plane run would take "+
					"the framework's transport path and fail on a credential it should never need. Route %s",
					target.LocalPlaneEnvName, name)
			}
			if !strings.Contains(bot, "_local_plane_transport") {
				t.Errorf("the emitted agent has no local-plane transport builder to take. Route %s", name)
			}
		})
	}
}

// Gate C4: nothing the plane sets may reach the author's environment file or a
// deployment manifest. The author does not set it, and a name in .env.example is
// a name somebody will try to fill in.
func TestLocalPlaneSelectorStaysOutOfTheAuthorsEnvironment(t *testing.T) {
	for _, route := range mediaWebsocketPipecatRoutes(t) {
		name := route.Key.Transport + "_" + route.Key.Carrier
		t.Run(name, func(t *testing.T) {
			for _, file := range []string{".env.example", "pcc-deploy.toml"} {
				content, ok := emittedFileFor(t, route, file)
				if !ok {
					continue
				}
				if strings.Contains(content, target.LocalPlaneEnvName) {
					t.Errorf("%s carries %s, which is the plane's to set and never the author's (gate C4). Route %s",
						file, target.LocalPlaneEnvName, name)
				}
			}
		})
	}
}

package generate

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/slng-ai/unmute/internal/target"
)

// The SIP plane: what the emitted Compose file has to say for a call to work on
// a machine with no carrier account. Every rule here comes from
// contracts/local-planes.md, and every one of them was measured before it was
// written down, because each is a claim about what a real SIP stack does.

// planeCompose is the narrow view of the generated Compose file these tests
// read. Service environments are two shapes in one file (a list for the
// application, a map for the SIP services), so the field stays untyped and the
// helpers below take the shape they need.
type planeCompose struct {
	Services map[string]struct {
		Profiles    []string `yaml:"profiles"`
		Ports       []string `yaml:"ports"`
		Environment any      `yaml:"environment"`
		Command     []string `yaml:"command"`
		WorkingDir  string   `yaml:"working_dir"`
		Networks    map[string]struct {
			IPv4Address string `yaml:"ipv4_address"`
		} `yaml:"networks"`
	} `yaml:"services"`
	Networks map[string]struct {
		IPAM struct {
			Config []struct {
				Subnet string `yaml:"subnet"`
			} `yaml:"config"`
		} `yaml:"ipam"`
	} `yaml:"networks"`
}

func readPlaneCompose(t *testing.T, artifact Artifact) planeCompose {
	t.Helper()
	var parsed planeCompose
	if err := yaml.Unmarshal([]byte(artifactFile(t, artifact, "compose.telephony.yaml")), &parsed); err != nil {
		t.Fatalf("parse compose.telephony.yaml: %v", err)
	}
	return parsed
}

// serviceEnv reads one environment value from a service whose environment is a
// map. A service that uses the list form has none to read.
func serviceEnv(t *testing.T, compose planeCompose, service, name string) string {
	t.Helper()
	entry, ok := compose.Services[service]
	if !ok {
		t.Fatalf("compose has no %s service; it has %v", service, serviceNames(compose))
	}
	values, ok := entry.Environment.(map[string]any)
	if !ok {
		t.Fatalf("%s environment is %T, not a map", service, entry.Environment)
	}
	text, ok := values[name].(string)
	if !ok {
		t.Fatalf("%s environment has no %s", service, name)
	}
	return text
}

func serviceNames(compose planeCompose) []string {
	names := make([]string, 0, len(compose.Services))
	for name := range compose.Services {
		names = append(names, name)
	}
	return names
}

// Gates S1, S2 and S6. The advertised signalling and media addresses have to be
// reachable from whoever is calling, and who that is differs by profile: a
// softphone on the host cannot reach the plane's container network, and a
// container cannot reach the host's loopback. Measured in both directions on
// 2026-08-20, which is why there are two profiles rather than one setting.
func TestPlaneProfilesAdvertiseAnAddressTheCallerCanReach(t *testing.T) {
	compose := readPlaneCompose(t, generateSIPFixture(t, "twilio", true, true, true))

	softphone := serviceEnv(t, compose, "livekit_sip", "SIP_CONFIG_BODY")
	headless := serviceEnv(t, compose, "livekit_sip_headless", "SIP_CONFIG_BODY")

	// S1: all four settings, in both profiles. Three is the version that does
	// not work: fixing media alone leaves the Contact header unreachable and
	// the call dies waiting for an ACK that went to the container's own address.
	for _, body := range []struct {
		profile, config, advertise string
	}{
		{"softphone", softphone, "${UNMUTE_PLANE_ADVERTISE_IP:-127.0.0.1}"},
		{"headless", headless, "10.185.61.10"},
	} {
		for _, want := range []string{
			"use_external_ip: false",
			"nat_1_to_1_ip: " + body.advertise,
			"media_nat_1_to_1_ip: " + body.advertise,
			// S2: symmetric media, so the plane replies to where audio actually
			// came from rather than to what the SDP claimed.
			"symmetric_rtp: true",
		} {
			if !strings.Contains(body.config, want) {
				t.Errorf("the %s profile's SIP config is missing %q:\n%s", body.profile, want, body.config)
			}
		}
	}

	// The two configs are one config with one difference. Anything else that
	// drifts between them is a setting somebody fixed in the profile they were
	// testing and nowhere else, which is the whole failure mode this catches.
	placeholder := "<advertised>"
	normalise := func(config, advertise string) string {
		return strings.ReplaceAll(config, advertise, placeholder)
	}
	got := normalise(softphone, "${UNMUTE_PLANE_ADVERTISE_IP:-127.0.0.1}")
	want := normalise(headless, "10.185.61.10")
	if got != want {
		t.Errorf("the two profiles' SIP configs differ by more than the advertised address:\nsoftphone:\n%s\nheadless:\n%s", got, want)
	}

	// The profiles are what select them, so exactly one runs per run.
	if profiles := compose.Services["livekit_sip"].Profiles; len(profiles) != 1 || profiles[0] != "softphone" {
		t.Errorf("livekit_sip profiles are %v, want exactly [softphone]", profiles)
	}
	if profiles := compose.Services["livekit_sip_headless"].Profiles; len(profiles) != 1 || profiles[0] != "headless" {
		t.Errorf("livekit_sip_headless profiles are %v, want exactly [headless]", profiles)
	}

	// S6: the headless profile publishes no SIP port and no media range. This
	// is what makes the unattended check safe to run in continuous integration
	// and free of the host-port collisions a published range of 101 UDP ports
	// guarantees. The control ports the dev command has to reach are published
	// in both, and bound to one interface.
	if ports := compose.Services["livekit_sip_headless"].Ports; len(ports) != 0 {
		t.Errorf("the headless SIP service publishes %v; it must publish nothing", ports)
	}
	for _, want := range []string{
		"${UNMUTE_PLANE_ADVERTISE_IP:-127.0.0.1}:${LIVEKIT_SIP_HOST_PORT:-5060}:5060/udp",
		"${UNMUTE_PLANE_ADVERTISE_IP:-127.0.0.1}:${LIVEKIT_SIP_HOST_PORT:-5060}:5060/tcp",
	} {
		if !containsString(compose.Services["livekit_sip"].Ports, want) {
			t.Errorf("the softphone SIP service does not publish %q; it publishes %v", want, compose.Services["livekit_sip"].Ports)
		}
	}
	// Nothing anywhere in the file binds the wildcard. A development stack
	// carrying a published key pair and an open SIP port has no business
	// listening on every interface of a laptop.
	for name, service := range compose.Services {
		for _, port := range service.Ports {
			if strings.Count(port, ":") < 2 {
				t.Errorf("%s publishes %q with no host address, so it binds every interface", name, port)
			}
		}
	}
}

// The endpoint image and its configuration are emitted for a route with a SIP
// plane and for no other, so a route with no carrier-free loop gains no files
// it cannot use (P6).
func TestEndpointFilesFollowThePlaneAndNotTheProvider(t *testing.T) {
	sip := generateSIPFixture(t, "twilio", true, true, true)
	for _, path := range []string{"endpoint.Dockerfile", "baresip.conf"} {
		if !artifactHasFile(sip, path) {
			t.Errorf("the SIP route emits no %s, so its plane has no endpoint to run", path)
		}
	}
	// Every module the configuration loads was checked against the packaged
	// baresip. Two names must stay out: one cannot run at a carrier's sample
	// rate, and the other names its own recordings.
	config := artifactFile(t, sip, "baresip.conf")
	for _, want := range []string{"module\t\t\tg711.so", "module\t\t\taufile.so", "module\t\t\taubridge.so", "ctrl_tcp_listen"} {
		if !strings.Contains(config, want) {
			t.Errorf("baresip.conf is missing %q", want)
		}
	}
	for _, forbidden := range []string{"ausine.so", "sndfile.so"} {
		if strings.Contains(config, "module\t\t\t"+forbidden) {
			t.Errorf("baresip.conf loads %s; the comment above it says why it cannot", forbidden)
		}
	}

	agent, resolved := configuredLiveKitConnector(t)
	connector, err := GenerateLiveKit(agent, resolved, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"endpoint.Dockerfile", "baresip.conf"} {
		if artifactHasFile(connector, path) {
			t.Errorf("the connector route emits %s; its plane is the media websocket and it runs no SIP endpoint", path)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}

// The warm path dials a number or a SIP user, and the cold path refers to a
// full URI. One destination value feeds both, so the two have to disagree about
// its shape on purpose rather than by accident.
//
// Found on a live call, 2026-08-20: warm sent the whole URI and
// CreateSIPParticipant answered 400 "SipCallTo should be a phone number or SIP
// user, not a full SIP URI" before dialling anything. The plane surfaced it, but
// it was never only the plane's problem: `sip:+E164@<host>` is a documented
// destination form, mandatory for one carrier's Refer-To, so the same 400 was
// waiting for anyone using it in production.
func TestWarmDialsAUserAndColdRefersToAURI(t *testing.T) {
	for _, testCase := range []struct{ destination, want string }{
		{"+15551234567", "+15551234567"},
		{"sip:+15551234567@trunk.example.com", "+15551234567"},
		{"sip:supervisor_line@10.185.61.21:5060", "supervisor_line"},
		{"sips:+15551234567@trunk.example.com", "+15551234567"},
		{"tel:+15551234567", "+15551234567"},
	} {
		if got := sipUser(testCase.destination); got != testCase.want {
			t.Errorf("sipUser(%q) = %q, want %q", testCase.destination, got, testCase.want)
		}
	}

	// Both helpers reach the emitted agent, and each dial site uses the right
	// one. A package with only one transfer shape gets only the helper it needs.
	warm := artifactFile(t, generateSIPFixture(t, "twilio", true, true, false), "agent.py")
	for _, want := range []string{"def _sip_user(", "sip_call_to=_sip_user(os.environ["} {
		if !strings.Contains(warm, want) {
			t.Errorf("the warm agent is missing %q", want)
		}
	}
	if strings.Contains(warm, "def _refer_uri(") {
		t.Error("a warm-only package carries the cold transfer's URI helper")
	}
	cold := artifactFile(t, generateSIPFixture(t, "twilio", true, true, true), "agent.py")
	for _, want := range []string{"def _refer_uri(", "transfer_to=_refer_uri(os.environ["} {
		if !strings.Contains(cold, want) {
			t.Errorf("the cold agent is missing %q", want)
		}
	}
	if strings.Contains(cold, "def _sip_user(") {
		t.Error("a cold-only package carries the warm transfer's user helper")
	}
}

// Gate P6, the SIP half of FR-007: the plane supplies nothing the route does not
// declare. A capability a route lacks is refused at compile time, and the plane
// must not quietly make it work anyway.
//
// The risk is specific and easy to create by accident. The SIP plane brings up
// destination endpoints, one per declared destination, and those endpoints will
// answer anything that dials them. If a plane file were emitted for a route or a
// package that declares no transfer, the endpoint would be sitting there ready,
// and a reader who found it would reasonably conclude the capability exists.
func TestThePlaneEmitsNothingForACapabilityTheRouteLacks(t *testing.T) {
	for _, route := range target.SelectableTelephonyRoutes() {
		if route.Key.Provider != target.LiveKit || route.LocalPlane != target.LocalPlaneSIP {
			continue
		}
		name := route.Key.Transport + "_" + route.Key.Carrier
		t.Run(name, func(t *testing.T) {
			artifact, err := telephonyRouteArtifact(t, route.Key)
			if err != nil {
				t.Fatalf("%v", err)
			}
			// The fixture this builds declares no transfer, so no destination
			// endpoint may exist for one. The endpoint files are the plane's only
			// per-capability artifacts, which is why they are what this reads.
			for _, file := range artifact.Files {
				if !strings.Contains(file.Path, "endpoint") && !strings.Contains(file.Path, "baresip") {
					continue
				}
				for _, control := range []target.TelephonyControl{target.ColdTransfer, target.WarmTransfer} {
					if _, declared := route.Features[target.TelephonyFeature(control)]; declared {
						continue
					}
					if strings.Contains(string(file.Content), string(control)) {
						t.Errorf("%s mentions %s, which the %s route does not declare, so the plane "+
							"offers a capability the compiler refuses", file.Path, control, name)
					}
				}
			}
		})
	}
}

// And the other direction, which is the one a reader actually meets: a package
// asking for a shape its route lacks is refused, and the refusal names where the
// shape does work rather than leaving a dead end.
func TestARouteWithoutAShapeRefusesItAndSaysWhereItWorks(t *testing.T) {
	for _, route := range target.SelectableTelephonyRoutes() {
		if route.LocalPlane == target.LocalPlaneNone {
			continue
		}
		for _, control := range []target.TelephonyControl{target.ColdTransfer, target.WarmTransfer} {
			if _, declared := route.Features[target.TelephonyFeature(control)]; declared {
				continue
			}
			name := string(route.Key.Provider) + "_" + route.Key.Transport + "_" + route.Key.Carrier + "_" + string(control)
			t.Run(name, func(t *testing.T) {
				evidence := target.ResolveTelephonyFeature(route.Key, target.TelephonyFeature(control))
				if evidence.Tag != target.Gated {
					t.Fatalf("a shape this route does not declare resolved as %s rather than being refused",
						evidence.Tag)
				}
				// The sentence that turns a refusal into a redirection. Every one
				// of these messages already carries it; this is what keeps it.
				if !strings.Contains(evidence.Note, "compiles on") {
					t.Errorf("the refusal does not say where %s does work: %s", control, evidence.Note)
				}
			})
		}
	}
}

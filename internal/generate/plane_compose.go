package generate

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	targetcap "github.com/slng-ai/unmute/internal/target"
)

// The local SIP plane's Compose topology, owned once.
//
// Two drivers run their agent on this plane, and everything except the agent's
// own service is identical: the two SIP profiles, the endpoints, the network with
// its explicit subnet, the coordination store. Every one of those carries a
// measured decision in a comment, and a second copy of them is the copy that
// drifts. So the plane is one template and each driver supplies only its own
// application service.

// planeService is one endpoint container on the SIP plane. One serves the caller, one
// serves every transfer destination: an endpoint process routes an incoming
// call to the account matching the request URI's user part, so several
// destinations share an address and stay individually reachable and
// individually recorded.
type planeService struct {
	Name     string
	Address  string
	Purpose  string
	Headless bool
	// Dial is what this endpoint calls at startup, and empty on an endpoint that
	// only answers. Set on the caller: nothing else in the graph would ever tell
	// it to place a call, and on the headless profile there is no person to.
	Dial     string
	Accounts []planeAccount
}

// planeAccount is one endpoint: a SIP identity, what it plays, and where
// what it hears is written.
type planeAccount struct {
	Name      string
	Role      string
	Address   string
	Recording string
	// Line is the endpoint's baresip account line. Built here rather than in
	// the template because every parameter on it was measured, and a comment
	// explaining why belongs next to the code that writes it.
	Line string
}

// sipPlaneCompose is what the plane template reads. Explicit fields rather than
// an embedded driver struct, because Go templates do not promote fields through
// an embedded interface: the first attempt did that and every render failed on
// the first plane field it reached.
type sipPlaneCompose struct {
	// ApplicationService is the driver's own rendered service block.
	ApplicationService string
	PlaneSubnet        string
	PlaneServices      []planeService
	// WebhookURL is where this plane's LiveKit Server announces rooms, and empty
	// on a driver that needs no announcement. Set by the driver rather than here,
	// because whether the agent has to be told about a call is a fact about the
	// agent: a LiveKit worker is dispatched to the room and a Pipecat bot is not.
	//
	// Empty renders nothing at all, which is what keeps the LiveKit driver's
	// emitted Compose file byte-identical to what it emitted before this existed.
	WebhookURL string
}

//go:embed templates/plane/*.tmpl
var planeTemplates embed.FS

// sipPlaneSetup is what the plane's provisioning script reads: two generic
// names and the one that differs per route.
type sipPlaneSetup struct {
	Project       string
	AgentName     string
	Carrier       string
	FromNumberEnv string
	// DispatchesWorker says whether this route's agent is a LiveKit agent worker,
	// which is the only kind of agent a dispatch rule can dispatch. False leaves
	// roomConfig out of the emitted rule, for the same reason the dev command
	// leaves it out of the record it creates: naming a worker that never
	// registers describes something that never happens.
	DispatchesWorker bool
	// TracingProvider is the package's tracing provider, or "". The trunk is the
	// only place a provider can ask for a SIP header to be carried into the room,
	// so the provisioning input reads it; nothing else here does.
	TracingProvider string
}

// planeArtifacts are the plane's files that are the same whichever driver runs
// on it, in emit order. The endpoint image and its configuration take no data at
// all; the setup script takes sipPlaneSetup.
var planeArtifacts = []struct{ tmpl, path string }{
	{"endpoint.Dockerfile", "endpoint.Dockerfile"},
	{"baresip.conf", "baresip.conf"},
}

// renderPlaneArtifact renders one plane file.
func renderPlaneArtifact(name string, data any) ([]byte, error) {
	return renderPlane(name+".tmpl", data)
}

// renderSIPPlaneCompose wraps a driver's own application service in the shared
// plane. applicationService is already-rendered YAML, indented as a member of
// `services:` and ending with a newline.
func renderSIPPlaneCompose(data sipPlaneCompose) ([]byte, error) {
	return renderPlane("sip_compose.yaml.tmpl", data)
}

// renderPlane renders any of the plane's own templates.
func renderPlane(name string, data any) ([]byte, error) {
	raw, err := planeTemplates.ReadFile("templates/plane/" + name)
	if err != nil {
		return nil, err
	}
	// The same helpers both drivers give their own templates, so the plane can
	// use any of them without a driver having to remember to pass them.
	parsed, err := template.New(name).Funcs(template.FuncMap{
		"pyq":        pyQuote,
		"join":       strings.Join,
		"triple":     pyTriple,
		"mcpTimeout": func() int { return mcpTimeoutSeconds },
	}).Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("plane template %s: %w", name, err)
	}
	var out bytes.Buffer
	if err := parsed.Execute(&out, data); err != nil {
		return nil, fmt.Errorf("plane template %s: %w", name, err)
	}
	return out.Bytes(), nil
}

// sipPlaneFiles is every plane file that does not depend on the driver: the
// endpoint image, its configuration, and, for a route that receives calls, the
// provisioning script. Emitted from one place so the two drivers that run this
// plane cannot ship different endpoints.
//
// hasInbound gates the script alone. It creates the two records an unsolicited
// call needs, and an outbound-only package needs neither: emitting it there
// hands the operator a command whose output nothing reads.
func sipPlaneFiles(setup sipPlaneSetup, hasInbound bool) ([]File, error) {
	var files []File
	for _, artifact := range planeArtifacts {
		content, err := renderPlaneArtifact(artifact.tmpl, nil)
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: artifact.path, Content: content})
	}
	if !hasInbound {
		return files, nil
	}
	script, err := renderPlaneArtifact("telephony-setup.sh", setup)
	if err != nil {
		return nil, err
	}
	files = append(files, File{Path: "telephony-setup.sh", Content: script})
	inputs, err := sipProvisioningInputs(setup)
	if err != nil {
		return nil, err
	}
	return append(files, inputs...), nil
}

// sipProvisioningInputs are the two records an incoming call needs in
// production, as JSON for the emitted setup script to feed to `lk`.
//
// Emitted here rather than by a driver because the script that reads them is
// emitted here, and it names them by path. That was not true before: the script
// went out on both SIP routes and these files on only one, so on `(pipecat, sip)`
// the operator's one documented command failed on its first `sed` with no such
// file.
//
// Shapes re-verified 2026-08-12 with the LiveKit docs
// (docs.livekit.io/telephony/start/sip-trunk-setup): an inbound trunk is a name
// plus its numbers, and a dispatch rule is a name plus a rule, with
// dispatchRuleIndividual and roomPrefix for one room per caller. A rule with no
// trunk list matches every trunk in the project, which is why trunk_ids is always
// written and the script refuses to create a rule without a resolved ID.
func sipProvisioningInputs(setup sipPlaneSetup) ([]File, error) {
	encode := func(path string, value any) (File, error) {
		content, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return File{}, fmt.Errorf("encode %s: %w", path, err)
		}
		return File{Path: path, Content: append(content, '\n')}, nil
	}
	inbound := map[string]any{
		"name":    setup.Project + " " + setup.Carrier + " inbound",
		"numbers": []string{"${" + setup.FromNumberEnv + "}"},
	}
	if setup.TracingProvider == "coval" {
		// Coval marks the call with a SIP header, and LiveKit only surfaces a
		// header it was told about in advance. Map it explicitly rather than
		// turning on blanket X- mapping: this names the one attribute the
		// agent reads, in the one file the operator registers.
		inbound["headers_to_attributes"] = map[string]string{
			"X-Coval-Simulation-Id": "coval.simulation_id",
		}
	}
	trunk, err := encode("sip-inbound-trunk.json", map[string]any{"trunk": inbound})
	if err != nil {
		return nil, err
	}
	dispatch := map[string]any{
		"name": setup.Project + " inbound",
		// Substituted by telephony-setup.sh, not by the environment: no variable
		// of this name is ever set or read anywhere.
		"trunk_ids": []string{"${UNMUTE_SIP_TRUNK_ID}"},
		"rule": map[string]any{
			"dispatchRuleIndividual": map[string]any{"roomPrefix": targetcap.SIPCallRoomPrefix},
		},
	}
	if setup.DispatchesWorker {
		dispatch["roomConfig"] = map[string]any{
			"agents": []map[string]any{{
				"agentName": setup.AgentName,
				"metadata":  `{"direction":"inbound"}`,
			}},
		}
	}
	rule, err := encode("sip-dispatch-rule.json", map[string]any{"dispatch_rule": dispatch})
	if err != nil {
		return nil, err
	}
	return []File{trunk, rule}, nil
}

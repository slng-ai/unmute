package generate

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"text/template"

	"github.com/slng-ai/unmute/internal/ir"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// The twilio driver writes a standalone Python web app that Twilio
// ConversationRelay calls. ConversationRelay listens and speaks; the app thinks
// with one model SDK and runs the package's local tools. The emitted project
// holds no unmute dependency and no YAML: every decision is made here and
// written into the app's modules and conversation-relay.xml.tmpl as literals.

//go:embed templates/twilio_v1/*.tmpl
var twilioTemplates embed.FS

// Placeholders the XML template carries for the two values only the running app
// knows. app.py sets both attributes through ElementTree at startup and refuses
// to start if either survives.
const (
	twilioOriginPlaceholder    = "__PUBLIC_ORIGIN__"
	twilioWSSOriginPlaceholder = "__PUBLIC_WSS_ORIGIN__"
)

// TwilioRelayTemplate is the emitted TwiML template's path. `unmute deploy`
// reads it back out of the artifact to know what a hosted /voice must answer.
const TwilioRelayTemplate = "conversation-relay.xml.tmpl"

type twilioData struct {
	Target       string
	Project      string
	Python       string
	OpenAI       bool
	Model        string
	ParamsJSON   string // think params forwarded verbatim, as JSON
	Vertex       bool
	Location     string
	ModelKeyEnv  string
	Prompts      []pyPrompt // the entry agent's instructions, as prompts/instructions.md
	Greeting     string
	MaxSessions  int
	MaxRounds    int
	ToolDeadline int
	ToolsJSON    string
	LocalTools   []twilioTool
	Deps         []string
	Env          twilioEnv
	Region       string // the Twilio Region that handles the calls
	RESTEdge     string // the edge the app's REST client uses, empty in US1
	DeployEnv    []string
	Docs         targetcap.TwilioDocLinks
	ManualSteps  []string
	VertexHelper string
	ArtifactID   string
	Router       *twilioRouter // set when think is the SLNG Context Router
	Logic        *ir.Logic     // set when the author's own respond() runs the turn
}

// twilioRouter is what the OpenAI brain adds to reach the SLNG Context Router:
// the regional endpoint, the inline configuration, and the two identity
// headers. The values come from the helpers every router target shares
// (slng_router.go), so the twilio app sends what livekit and pipecat send.
type twilioRouter struct {
	BaseURL       string
	Config        string   // the slng_config dict literal
	Body          string   // the extra_body dict literal
	Scope         string   // the cache scope sent as the agent id header
	AgentHeader   string   // header names, owned by internal/target
	SessionHeader string   //
	UpstreamEnv   []string // the upstream credentials slng_config reads
}

type twilioTool struct {
	Name   string
	Source string
}

type twilioEnv struct {
	AccountSID string
	AuthToken  string
	PublicURL  string
}

// twilioEmittedTelephonyFeatures is what this driver's app.py actually does on
// its one route: it is the route, it answers inbound calls, and end_call hangs
// up. TestTelephonyRouteEmitterAgreement holds it to the route table.
var twilioEmittedTelephonyFeatures = map[targetcap.TelephonyFeature]bool{
	targetcap.TelephonyRouteSelected:             true,
	targetcap.TelephonyInbound:                   true,
	targetcap.TelephonyFeature(targetcap.Hangup): true,
}

// twilioToolSpec is what app.py reads to declare, validate and run one tool.
type twilioToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Input       map[string]any `json:"input"`
	Output      map[string]any `json:"output"`
	EndCall     bool           `json:"end_call"`
}

// GenerateTwilio renders the twilio project for one target instance.
func GenerateTwilio(agent *ir.Agent, resolved ir.Target, bindings []ir.ForwardedBinding, sizing []ir.Sizing) (Artifact, error) {
	data, err := buildTwilioData(agent, resolved)
	if err != nil {
		return Artifact{}, err
	}
	files, err := renderTwilioFiles(agent, resolved, data)
	if err != nil {
		return Artifact{}, err
	}
	// Rendered twice: once with no id to hash, once to carry it. The hash never
	// covers its own value, and the second render changes nothing else.
	data.ArtifactID = twilioArtifactID(files)
	files, err = renderTwilioFiles(agent, resolved, data)
	if err != nil {
		return Artifact{}, err
	}
	report, err := twilioReport(data, files, bindings, sizing, resolved)
	if err != nil {
		return Artifact{}, err
	}
	files = append(files, File{Path: "compile-report.json", Content: report})
	return Artifact{Kind: CodeTarget, Files: files, ArtifactID: data.ArtifactID}, nil
}

// twilioModules are the app's own Python modules, each rendered from its
// template and given its imports by linkPython. app.py stays the entry, because
// the image runs `python app.py`.
var twilioModules = []struct{ tmpl, path string }{
	{"settings.py", "settings.py"},
	{"prompts.py", "prompts/__init__.py"},
	{"tool_runner.py", "tool_runner.py"},
	{"brain.py", "brain.py"},
	{"handoff.py", "handoff.py"},
	{"call.py", "call.py"},
	{"app.py", "app.py"},
}

func renderTwilioFiles(agent *ir.Agent, resolved ir.Target, data twilioData) ([]File, error) {
	files, err := renderPythonModules(twilioTemplates, "templates/twilio_v1/", twilioFuncs(), data, twilioModules)
	if err != nil {
		return nil, fmt.Errorf("twilio modules: %w", err)
	}
	files = append(files, writePromptFiles(data.Prompts)...)
	for _, file := range []struct{ tmpl, path string }{
		{"pyproject.toml.tmpl", "pyproject.toml"},
		{"Dockerfile.tmpl", "Dockerfile"},
		{"dockerignore.tmpl", ".dockerignore"},
		{"env.example.tmpl", ".env.example"},
		{"README.md.tmpl", "README.md"},
	} {
		content, err := renderTwilio(file.tmpl, data)
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: file.path, Content: content})
	}
	xmlTemplate, err := twilioRelayXML(agent, resolved)
	if err != nil {
		return nil, err
	}
	files = append(files, File{Path: TwilioRelayTemplate, Content: xmlTemplate})
	for _, tool := range data.LocalTools {
		files = append(files, File{Path: filepath.ToSlash(filepath.Join("tools", tool.Name+".py")), Content: []byte(tool.Source)})
	}
	if data.Logic != nil {
		for _, name := range slices.Sorted(maps.Keys(data.Logic.Files)) {
			files = append(files, File{Path: "logic/" + filepath.ToSlash(name), Content: []byte(data.Logic.Files[name]), Verbatim: true})
		}
	}
	return files, nil
}

// twilioArtifactID names what a host runs, so `unmute deploy` can refuse to
// route a number at a different build. It is an Unmute drift check, not a
// Twilio API and not an attestation: a host can report any string it likes.
//
// It hashes every file that decides behaviour (the app's modules, its prompt,
// its TwiML, its pins, its image, the tool handlers and any custom logic),
// sorted by path. The README, .env.example and the compile report are left
// out, so a docs change needs no rehost. No hashed file holds a credential or
// the public origin: both are environment names here, and values only on the
// host.
func twilioArtifactID(files []File) string {
	var hashed []File
	for _, file := range files {
		switch file.Path {
		case "README.md", ".env.example", "compile-report.json":
		default:
			hashed = append(hashed, file)
		}
	}
	slices.SortFunc(hashed, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	sum := sha256.New()
	for _, file := range hashed {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(file.Content)))
		sum.Write([]byte(file.Path))
		sum.Write([]byte{0})
		sum.Write(size[:])
		sum.Write(file.Content)
	}
	return "sha256:" + hex.EncodeToString(sum.Sum(nil))
}

func twilioFuncs() template.FuncMap {
	return template.FuncMap{"pyq": pyQuote, "join": strings.Join, "checkers": pythonCheckers}
}

func renderTwilio(name string, data twilioData) ([]byte, error) {
	raw, err := twilioTemplates.ReadFile("templates/twilio_v1/" + name)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	tmpl, err := template.New(name).Funcs(twilioFuncs()).Option("missingkey=error").Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return nil, fmt.Errorf("render %s: %w", name, err)
	}
	return out.Bytes(), nil
}

func buildTwilioData(agent *ir.Agent, resolved ir.Target) (twilioData, error) {
	entry := agent.Agents[agent.EntryAgent]
	think, ok := resolved.Models.Reason[entry.Model]
	if !ok {
		return twilioData{}, fmt.Errorf("twilio: entry agent %q has no think binding", agent.EntryAgent)
	}
	catalog := targetcap.DefaultCatalog()
	thinkEntry, ok := catalog.Lookup(targetcap.Twilio, targetcap.Reason, think.Provider)
	if !ok || thinkEntry.Call == nil {
		return twilioData{}, fmt.Errorf("twilio: think provider %q has no catalogue row", think.Provider)
	}
	data := twilioData{
		Target: resolved.Name, Project: cmp.Or(agent.Name, resolved.Name), Python: targetcap.TwilioPython,
		OpenAI: thinkEntry.Vendor == "openai", Model: think.Model, ModelKeyEnv: thinkEntry.Call.APIKeyEnv,
		Prompts:   []pyPrompt{{Const: "INSTRUCTIONS", File: "instructions", Text: entry.Instructions}},
		MaxRounds: targetcap.TwilioMaxToolRounds, ToolDeadline: targetcap.TwilioToolDeadlineSeconds,
		Docs: targetcap.TwilioDocs,
	}
	if agent.Capacity != nil {
		data.MaxSessions = agent.Capacity.MaxSessions
	}
	if greeting := agent.Conversation.Greeting; greeting != nil && greeting.SpeaksFirst == ir.SpeaksFirstAgent {
		data.Greeting = greeting.Text
	}
	params := maps.Clone(think.Params)
	if params == nil {
		params = map[string]any{}
	}
	if think.Router() {
		router, err := twilioRouterFor(agent, think)
		if err != nil {
			return twilioData{}, err
		}
		data.Router = router
		data.OpenAI = true // the router speaks Chat Completions
		// Only the folded fields stay request kwargs. world_part became the
		// base URL, and everything else rides the body, the router's rule on
		// every target.
		params, _ = splitParams(slngConsumedParams(params), targetcap.SlngRequestBodyArg)
		if params == nil {
			params = map[string]any{}
		}
	}
	// Dropped for the reason the shared warning gives: they configure a
	// Responses client, and a Chat Completions request does not define them.
	for _, name := range ir.ResponsesOnlyParams {
		delete(params, name)
	}
	if !data.OpenAI && params["vertexai"] == true {
		// Client settings, not request fields: they choose the endpoint and
		// never reach GenerateContentConfig.
		data.Vertex = true
		data.Location, _ = params["location"].(string)
		data.VertexHelper = googleVertexClient
		delete(params, "vertexai")
		delete(params, "location")
	}
	delete(params, "vertexai") // false or absent means the Developer API
	encoded, err := json.Marshal(params)
	if err != nil {
		return twilioData{}, fmt.Errorf("twilio: think params: %w", err)
	}
	data.ParamsJSON = string(encoded)

	var specs []twilioToolSpec
	var deps []string
	for _, name := range entry.Tools {
		tool, ok := agent.Tools[name]
		if !ok {
			continue
		}
		spec := twilioToolSpec{Name: name, Description: tool.Description, Input: tool.Input, Output: tool.Output}
		switch tool.Execution {
		case ir.ToolBuiltin:
			spec.EndCall = true
		case ir.ToolLocal:
			data.LocalTools = append(data.LocalTools, twilioTool{Name: name, Source: tool.HandlerSource})
			deps = append(deps, tool.Dependencies...)
		default:
			return twilioData{}, fmt.Errorf("twilio: tool %q has execution %s, which validation refuses", name, tool.Execution)
		}
		specs = append(specs, spec)
	}
	encoded, err = json.Marshal(specs)
	if err != nil {
		return twilioData{}, fmt.Errorf("twilio: tools: %w", err)
	}
	data.ToolsJSON = string(encoded)

	for _, name := range append(slices.Clone(targetcap.TwilioRuntimeDeps), targetcap.TwilioModelSDK(thinkEntry.Vendor)) {
		data.Deps = append(data.Deps, name+"=="+targetcap.TwilioPins[name])
	}
	slices.Sort(data.Deps)
	slices.Sort(deps)
	data.Deps = append(data.Deps, slices.Compact(deps)...)
	// The logic's own requirements come last and as written: they are the
	// author's, and a clash with a pin above is for the installer to report.
	if resolved.Logic != nil {
		data.Logic = resolved.Logic
		data.Deps = append(data.Deps, resolved.Logic.Requirements()...)
	}

	if plan := resolved.Telephony; plan != nil {
		data.Env = twilioEnv{
			AccountSID: plan.Environment["account_sid"],
			AuthToken:  plan.Environment["auth_token"],
			PublicURL:  plan.Environment["public_url"],
		}
		data.Region = plan.Region
		data.RESTEdge = targetcap.TwilioRegionEdges[plan.Region]
		data.DeployEnv = plan.DeployEnvironment
		data.ManualSteps = plan.ManualSteps
	}
	if data.Env.AccountSID == "" || data.Env.AuthToken == "" || data.Env.PublicURL == "" {
		return twilioData{}, fmt.Errorf("twilio: connection %q must name account_sid, auth_token and public_url", resolved.Connection)
	}
	return data, nil
}

func twilioRouterFor(agent *ir.Agent, think ir.Binding) (*twilioRouter, error) {
	base, ok := targetcap.SlngRouterBaseURL(slngWorldPart(think))
	if !ok {
		return nil, fmt.Errorf("twilio: think params.world_part %q is not a router world part", slngWorldPart(think))
	}
	config, err := slngConfigBody(think)
	if err != nil {
		return nil, fmt.Errorf("twilio: think: %w", err)
	}
	scope := targetcap.SlngScope(think.AgentID, targetcap.SlngSite{Kind: targetcap.SlngSiteAgent, Name: agent.EntryAgent})
	site := slngSite{ConfigFunc: "_slng_config", Scope: scope}
	return &twilioRouter{
		BaseURL: base, Config: config, Body: pyLiteral(slngRequestBody(site, think)), Scope: scope,
		AgentHeader: targetcap.SlngAgentIDHeader, SessionHeader: targetcap.SlngSessionIDHeader,
		UpstreamEnv: slngBindingCredentialEnvs(think),
	}, nil
}

// twilioRelayXML writes the TwiML the app answers /voice with, as a template:
// the two URLs carry placeholders the app replaces at startup, and every other
// attribute is final. encoding/xml escapes each value, so a greeting holding
// quotes or an ampersand arrives intact.
func twilioRelayXML(agent *ir.Agent, resolved ir.Target) ([]byte, error) {
	attrs := []xml.Attr{{Name: xml.Name{Local: "url"}, Value: twilioWSSOriginPlaceholder + "/conversation"}}
	set := func(name, value string) {
		if value != "" {
			attrs = append(attrs, xml.Attr{Name: xml.Name{Local: name}, Value: value})
		}
	}
	entry := agent.Agents[agent.EntryAgent]
	params := func(role targetcap.Role, values map[string]any) {
		for _, param := range targetcap.TwilioParams[role] {
			if value, ok := values[param.Name]; ok {
				set(param.Name, targetcap.TwilioAttr(value))
			}
		}
	}
	if listen := resolved.Models.Listen; listen != nil {
		set("transcriptionProvider", targetcap.TwilioSpeechProviders[targetcap.Listen][listen.Provider])
		set("speechModel", listen.Model)
		set("transcriptionLanguage", listen.Language)
		params(targetcap.Listen, listen.Params)
	}
	if speak, ok := resolved.Models.Speak[entry.Voice]; ok {
		voice := speak.Voice
		if voice == "" {
			voice = speak.VoiceID
		}
		set("ttsProvider", targetcap.TwilioSpeechProviders[targetcap.Speak][speak.Provider])
		set("voice", voice+"-"+speak.Model)
		set("ttsLanguage", speak.Language)
		params(targetcap.Speak, speak.Params)
	}
	interruptible := "speech"
	conversation := agent.Conversation
	if conversation != nil && conversation.Interruption != nil && conversation.Interruption.Enabled != nil && !*conversation.Interruption.Enabled {
		interruptible = "none"
	}
	greetingInterruptible := interruptible
	if conversation != nil && conversation.Interruption != nil && slices.Contains(conversation.Interruption.Protect, ir.ProtectGreeting) {
		greetingInterruptible = "none"
	}
	if conversation != nil && conversation.Greeting != nil && conversation.Greeting.SpeaksFirst == ir.SpeaksFirstAgent {
		set("welcomeGreeting", conversation.Greeting.Text)
		set("welcomeGreetingInterruptible", greetingInterruptible)
	}
	set("interruptible", interruptible)
	set("reportInputDuringAgentSpeech", interruptible)
	set("preemptible", "false")
	set("dtmfDetection", "false")
	if turn := resolved.Models.Turn; turn != nil {
		params(targetcap.Turn, turn.Params)
	}
	type relay struct {
		XMLName xml.Name
		Attrs   []xml.Attr `xml:",any,attr"`
	}
	type connect struct {
		XMLName xml.Name `xml:"Connect"`
		Action  string   `xml:"action,attr"`
		Method  string   `xml:"method,attr"`
		Relay   relay
	}
	type response struct {
		XMLName xml.Name `xml:"Response"`
		Connect connect
		Hangup  struct{} `xml:"Hangup"`
	}
	doc := response{Connect: connect{
		Action: twilioOriginPlaceholder + "/connect-action", Method: "POST",
		Relay: relay{XMLName: xml.Name{Local: "ConversationRelay"}, Attrs: attrs},
	}}
	out, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("twilio: render TwiML: %w", err)
	}
	header := "<!-- ConversationRelay TwiML for " + resolved.Name + ", generated by unmute. Not final TwiML:\n" +
		"     app.py replaces " + twilioOriginPlaceholder + " and " + twilioWSSOriginPlaceholder + " with the public origin at startup. -->\n"
	return append([]byte(header), append(out, '\n')...), nil
}

type twilioReportJSON struct {
	Target      string                `json:"target"`
	Provider    string                `json:"provider"`
	ArtifactID  string                `json:"artifact_id"`
	Runtime     twilioRuntimeReport   `json:"runtime"`
	Files       []string              `json:"files"`
	RequiredEnv []string              `json:"required_env"`
	DeployEnv   []string              `json:"deploy_env,omitempty"`
	Bindings    []ir.ForwardedBinding `json:"bindings"`
	Sizing      []ir.Sizing           `json:"sizing"`
	Evidence    []string              `json:"evidence"`
}

type twilioRuntimeReport struct {
	Python        string            `json:"python"`
	Pins          map[string]string `json:"pins"`
	ThinkAPI      string            `json:"think_api"`
	Processes     int               `json:"processes"`
	CallSlots     int               `json:"call_slots"`
	MaxToolRounds int               `json:"max_tool_rounds"`
	ToolDeadline  string            `json:"tool_deadline"`
	Verified      string            `json:"verified"`
}

// twilioReport is the compile report: what was resolved and what it rests on.
// Deterministic by construction, with no timestamp and no value, so two
// compiles of one package write the same bytes.
func twilioReport(data twilioData, files []File, bindings []ir.ForwardedBinding, sizing []ir.Sizing, resolved ir.Target) ([]byte, error) {
	generated := []string{"compile-report.json"}
	for _, file := range files {
		generated = append(generated, file.Path)
	}
	slices.Sort(generated)
	required := []string{data.Env.AccountSID, data.Env.AuthToken, data.Env.PublicURL, data.ModelKeyEnv}
	if data.Router != nil {
		required = append(required, data.Router.UpstreamEnv...)
	}
	slices.Sort(required)
	required = slices.Compact(required)
	pins := map[string]string{}
	for _, dep := range data.Deps {
		if name, version, ok := strings.Cut(dep, "=="); ok {
			pins[name] = version
		}
	}
	api := "OpenAI Chat Completions (POST /v1/chat/completions, streamed)"
	if data.Router != nil {
		api = "SLNG Context Router, OpenAI Chat Completions at " + data.Router.BaseURL + " (streamed)"
	}
	if !data.OpenAI {
		api = "Gemini generateContent (streamGenerateContent), Gemini Developer API"
		if data.Vertex {
			api = "Gemini generateContent (streamGenerateContent), Vertex AI " + data.Location + " with an API key, v1beta1"
		}
	}
	vendor, path := "openai", "chat"
	switch {
	case data.Router != nil:
		vendor, path = "slng", "router"
	case !data.OpenAI:
		vendor, path = "google", cmp.Or(data.Location, "developer")
	}
	think := fmt.Sprintf("think %s: no real-model evidence for this model and path; run scripts/text_run_twilio.py --real", data.Model)
	for _, run := range targetcap.TwilioVerifiedThink {
		if run.Model == data.Model && run.Path == path && run.Vendor == vendor {
			think = fmt.Sprintf("think %s: streamed reply, tool follow-up and next turn after an interrupt passed against the real API on %s", data.Model, run.Date)
		}
	}
	evidence := []string{
		think,
		"ConversationRelay protocol: offline, against a signed fake client (scripts/text_run_twilio.py --fake)",
		"real Twilio call: one, on a Render host with custom logic: the signed WebSocket handshake behind the proxy, speech both ways, the end message and the action callback; interrupt playback and the built-in turn on a real call are unverified",
	}
	out, err := json.MarshalIndent(twilioReportJSON{
		Target: resolved.Name, Provider: string(resolved.Provider), ArtifactID: data.ArtifactID,
		Runtime: twilioRuntimeReport{
			Python: data.Python, Pins: pins, ThinkAPI: api, Processes: 1, CallSlots: data.MaxSessions,
			MaxToolRounds: data.MaxRounds, ToolDeadline: fmt.Sprintf("%ds", data.ToolDeadline),
			Verified: targetcap.TwilioTargetVerified,
		},
		Files: generated, RequiredEnv: required, DeployEnv: data.DeployEnv,
		Bindings: bindings, Sizing: sizing, Evidence: evidence,
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

package generate

import (
	"maps"
	"slices"
	"strings"

	"github.com/slng-ai/unmute/internal/ir"
)

// The package-to-agent-body mapping. Field detail and the file each fact was
// read from live in specs/020-slng-compilation-target/contracts/agent-body.md;
// what is here is the mapping, with a comment wherever the choice is not
// obvious from the field name.
//
// Two shapes exist on the SLNG side and they are not interchangeable.
// VoiceAgentCreate is what you POST; AgentConfigV2 is what the dashboard
// exports. They differ in ways that make a downloaded document unsafe to post
// back, so unmute writes the create body and the runbook says so in both
// directions.

// slngSchemaVersion and slngToolMode are written on every body. Version 2 with
// no tool_mode is rejected, and shared mode is the only mode that takes
// tool_refs, so neither is ever a choice.
const (
	slngSchemaVersion = 2
	slngToolMode      = "shared"
	// slngInvocation is the only invocation unmute writes. The alternative,
	// "system", requires a system block naming the event that fires the tool, and
	// nothing in a package describes one.
	slngInvocation = "model"
)

type slngBody struct {
	SchemaVersion int         `json:"schema_version"`
	Name          string      `json:"name"`
	SystemPrompt  string      `json:"system_prompt"`
	Greeting      string      `json:"greeting"`
	Language      string      `json:"language,omitempty"`
	Region        string      `json:"region"`
	Models        slngModels  `json:"models"`
	Interruptions bool        `json:"enable_interruptions"`
	ToolMode      string      `json:"tool_mode"`
	ToolRefs      []slngRef   `json:"tool_refs"`
	MCPRefs       []slngMCP   `json:"mcp_refs"`
	RuntimeVars   []string    `json:"runtime_variables"`
	Defaults      slngStrings `json:"template_defaults"`
	Variables     slngOptions `json:"template_variable_options"`
}

// slngStrings and slngOptions are named map types so a nil map still encodes as
// {} rather than null. A package with no variables must produce two present,
// empty maps: SLNG's resolver takes the declared set from the union of their
// keys, and null there is a different statement from empty.
type (
	slngStrings map[string]string
	slngOptions map[string]slngVariableOption
)

func (s slngStrings) MarshalJSON() ([]byte, error) { return marshalSortedMap(s) }
func (o slngOptions) MarshalJSON() ([]byte, error) { return marshalSortedMap(o) }

type slngVariableOption struct {
	// Required is the only field TemplateVariableOption has, and it is
	// extra: forbid, so nothing else may be written per variable.
	Required bool `json:"required"`
}

type slngModels struct {
	STT       string         `json:"stt"`
	LLM       string         `json:"llm"`
	TTS       string         `json:"tts"`
	TTSVoice  string         `json:"tts_voice"`
	STTKwargs map[string]any `json:"stt_kwargs"`
	LLMKwargs map[string]any `json:"llm_kwargs"`
	TTSKwargs map[string]any `json:"tts_kwargs"`
	Fallbacks *slngFallbacks `json:"fallbacks,omitempty"`
}

type slngFallbacks struct {
	STT []string           `json:"stt,omitempty"`
	LLM []string           `json:"llm,omitempty"`
	TTS []slngVoiceFalback `json:"tts,omitempty"`
}

type slngVoiceFalback struct {
	Model string `json:"model"`
	Voice string `json:"voice"`
}

type slngRef struct {
	// Tool is a name where the platform expects an identifier. No compiler can
	// invent an id the server assigns, so the push step resolves it.
	//
	// There is deliberately no version field, and there used to be: unmute wrote
	// the string "latest" as a marker meaning "whatever this organisation has
	// published". ToolAttachment.version is `int >= 1`
	// (shared_tool_contract.py:484), so a string there is refused — and refused
	// *after* a push step has dutifully filled attachment_id and tool_id, which
	// makes it the worst possible place for a marker. The builtin path stripped
	// the field, so the first successful live push never exercised it.
	//
	// attachment_id, tool_id and version are the three slots the platform owns.
	// All three are now omitted, which says the same thing consistently and
	// leaves the reference invalid in one obvious way instead of two.
	Tool        string `json:"tool"`
	Description string `json:"description,omitempty"`
	Invocation  string `json:"invocation,omitempty"`
	// Policy carries the announce sentence. Absent rather than null when there is
	// nothing to say, because an empty policy object is a claim about the tool.
	Policy *slngPolicy `json:"execution_policy,omitempty"`
	// Arguments are the package's inject: values, which the model never sees.
	// Always written, empty when there are none, because that is the shape a real
	// SLNG body carries and "absent" versus "empty" is a question nobody should
	// have to answer while reading a diff.
	Arguments slngArguments `json:"argument_overrides"`
	// Config is a per-agent override, and a code tool never has one:
	// ToolConfigOverrides is a seven-member union with no code member, so a code
	// tool cannot be tuned per agent through config at all.
	Config map[string]any `json:"config_overrides,omitempty"`
}

// slngArguments is a named map so a nil one encodes as {} rather than null,
// the same reason the two variable maps are named types.
type slngArguments map[string]any

func (a slngArguments) MarshalJSON() ([]byte, error) { return marshalSortedMap(a) }

// slngPolicy carries the announce sentence, and its shape is not the obvious
// one. pre_action_message is not a string: it is an object with an enabled flag,
// a segmented text, and a wait flag, which is how a message can interleave
// literal text with call-context values. Unmute writes one literal segment,
// because a package's announce: is one fixed sentence.
//
// The published positive conformance fixture is what says so
// (contracts/shared_tools/v1/positive/agent_config_v2.json), and it is why that
// fixture is a test rather than a reference.
type slngPolicy struct {
	PreActionMessage *slngPreAction `json:"pre_action_message,omitempty"`
}

type slngPreAction struct {
	Enabled bool         `json:"enabled"`
	Text    slngSegments `json:"text"`
	// Wait false means the agent keeps going while the tool runs, which is what a
	// package's announce: describes: a sentence so a slow call is not silence.
	Wait bool `json:"wait"`
}

type slngSegments struct {
	Segments []slngSegment `json:"segments"`
}

type slngSegment struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// slngMCP is deliberately incomplete, and not only in its identifiers. SLNG also
// wants observed_schema_hash, a sha256 over schemas read from the live server's
// own tools/list response, which no offline compiler can produce. The runbook
// says the push step connects to the server rather than looking an id up.
//
// `server` is a name standing where server_id goes, and is spelled differently
// from the platform's field on purpose: a reader who sees `server_id` expects a
// UUID, and this is not one. `tool_name` keeps the platform's own spelling,
// because that field really does hold a name.
type slngMCP struct {
	Server     string `json:"server"`
	Tool       string `json:"tool_name"`
	Invocation string `json:"invocation,omitempty"`
}

// slngArtifacts is everything the driver needs to write: the body, one tool body
// per tool that needs one, and the facts the runbook prints.
type slngArtifacts struct {
	Body      slngBody
	ToolFiles []slngToolFile
	Runbook   slngRunbook
	Notes     []string
	Warnings  []string
	// Requires is what the account must already hold. Derived after the body is
	// built, because two of its five sources exist only after lowering: the vault
	// tokens in the resolved prompt and greeting, and the references the body
	// carries.
	Requires Requirements
}

func buildSlng(agent *ir.Agent, tgt ir.Target) (slngArtifacts, error) {
	entry := agent.Agents[agent.EntryAgent]
	built := slngArtifacts{Body: slngBody{
		SchemaVersion: slngSchemaVersion,
		// The package's name joined to the target instance, never the target
		// instance alone. Every package that follows the docs calls its slng target
		// `slng`, and an SLNG name is unique per organisation, so naming the agent
		// after the target made every package in an organisation claim one live
		// agent. ir.Build refuses a package with no name, so this is never bare.
		Name:         agent.DeployName(tgt),
		SystemPrompt: entry.Instructions,
		Region:       slngTargetRegion(tgt),
		ToolMode:     slngToolMode,
		// Not omitempty and not nil: SLNG's create default for runtime_variables
		// differs from the document's, and nothing in a package maps to it, so an
		// explicit empty list says "none" rather than "unspecified".
		RuntimeVars: []string{},
		Defaults:    slngStrings{},
		Variables:   slngOptions{},
	}}
	if agent.Conversation != nil && agent.Conversation.Greeting != nil {
		built.Body.Greeting = agent.Conversation.Greeting.Text
	}
	// llm_router_enabled is deliberately absent from the body above.
	//
	// It used to be written as false unless the think binding selected the Context
	// Router, and on this target that binding is refused by the catalogue, so the
	// field was always false and no author could change it. That is worse than it
	// sounds: SLNG's own create default is true, and a BYOK model — a per-org
	// client_model with no catalogue row — is only accepted when the router is on
	// (surface_resolver.py:426). So unmute was overriding SLNG's default to the one
	// value that refuses a whole class of the organisation's own models, and doing
	// it on behalf of an author who never asked.
	//
	// Measured 2026-08-25: the same body with llm_router_enabled true saved, and
	// with it false was rejected AGENT_MODEL_UNAVAILABLE on models.llm.
	//
	// Nothing in a package describes SLNG's server-side routing, so unmute states
	// nothing about it, exactly as it leaves idle_nudges out. The platform applies
	// its own default and the runbook says so.
	built.Body.Interruptions = true
	if agent.Conversation != nil && agent.Conversation.Interruption != nil &&
		agent.Conversation.Interruption.Enabled != nil {
		built.Body.Interruptions = *agent.Conversation.Interruption.Enabled
	}
	built.Body.Models = slngModelsFor(agent, tgt, entry)
	built.Body.Language = slngLanguage(tgt)

	refs, mcp, files, err := slngTools(agent, tgt, entry)
	if err != nil {
		return slngArtifacts{}, err
	}
	built.Body.ToolRefs, built.Body.MCPRefs, built.ToolFiles = refs, mcp, files

	// One rule, no branch: every declared variable is a declaration, and every
	// one that also has a default is additionally a default. SLNG takes the
	// declared set from the union of the two maps' keys, so a variable missing
	// from template_variable_options would be rejected at dispatch even with a
	// default present.
	for _, name := range slices.Sorted(maps.Keys(agent.Variables)) {
		variable := agent.Variables[name]
		built.Body.Variables[name] = slngVariableOption{Required: true}
		if text, ok := variable.Default.(string); ok {
			built.Body.Defaults[name] = text
		}
	}
	// Before the runbook, which renders half of it. Deriving it here rather than
	// inside slngRunbookFor keeps the runbook a renderer: what a package needs
	// from the account is a fact about the body, not about the document that
	// describes it, and `unmute deploy` reads the same value without rendering
	// anything.
	built.Requires = slngRequirements(agent, built)
	built.Runbook = slngRunbookFor(agent, tgt, built)
	built.Notes = append(built.Notes, slngNotes(built)...)
	return built, nil
}

// slngTargetRegion returns the one region SLNG is given. Validation has already
// refused an empty list, more than one, and a value outside the four, so this
// reads the first entry rather than re-deciding.
//
// Named for the target because slngRegion next door belongs to the model vendor:
// it reads a world_part_override off a Context Router binding, which is a
// different region set answering a different question.
func slngTargetRegion(tgt ir.Target) string {
	if len(tgt.DeploymentRegions) == 0 {
		return ""
	}
	return tgt.DeploymentRegions[0]
}

// slngLanguage takes the language off the listen binding, which is where a
// package states it. SLNG caps the field at 10 characters and takes a BCP-47
// tag, which ir.Validate has already shape-checked.
func slngLanguage(tgt ir.Target) string {
	if tgt.Models.Listen != nil {
		return tgt.Models.Listen.Language
	}
	return ""
}

// slngModelsFor renders each binding into the one string SLNG names a model by.
//
// SLNG writes a model as vendor and model in one string: "openai/gpt-5.6-luna",
// "cartesia/sonic:3". A package writes them as two fields, and it has to: this
// repository's N15 rule forbids a folded identifier in a package because a
// folded string reaches the OpenAI SDK verbatim on the other two targets and is
// a bug there. The same package may name a livekit target and a slng one.
//
// So the fold happens here, in the driver, which is where a target's own
// spelling belongs. The condition is the whole rule: fold when the model carries
// no slash, and forward it untouched when it does. A model that already carries
// one has been spelled the platform's way already — "slng/deepgram/nova:3-en" is
// three segments, not two — and folding it again would produce
// "slng/slng/deepgram/nova:3-en".
//
// Nothing else is checked. SLNG owns its model list, which is why this target
// needs no catalogue rows, so an unknown vendor passes through and the compile
// report says it was forwarded.
func slngModelsFor(agent *ir.Agent, tgt ir.Target, entry ir.AgentDef) slngModels {
	models := slngModels{
		STTKwargs: map[string]any{},
		LLMKwargs: map[string]any{},
		TTSKwargs: map[string]any{},
	}
	if listen := tgt.Models.Listen; listen != nil {
		models.STT = slngModelName(*listen)
		models.STTKwargs = slngKwargs(listen.Params)
	}
	if reason := tgt.Models.Reason[entry.Model]; reason.Model != "" {
		models.LLM = slngModelName(reason)
		models.LLMKwargs = slngKwargs(reason.Params)
	}
	if speak, ok := tgt.Models.Speak[entry.Voice]; ok {
		models.TTS = slngModelName(speak)
		models.TTSVoice = firstNonEmpty(speak.VoiceID, speak.Voice)
		models.TTSKwargs = slngKwargs(speak.Params)
	}
	models.Fallbacks = slngFallbacksFor(agent, tgt, entry)
	return models
}

// slngFallbacksFor builds the three fallback lists. The listen chain is already
// flattened and ordered by Build; the think chain is the entry model's own
// fallback names, resolved through the same per-target binding map the primary
// came from. Speak has no authored chain today, so the tts list stays absent
// rather than being invented from the speak overrides.
func slngFallbacksFor(agent *ir.Agent, tgt ir.Target, entry ir.AgentDef) *slngFallbacks {
	fallbacks := slngFallbacks{}
	for _, fallback := range tgt.Models.ListenFallbacks {
		if fallback.Binding.Model != "" {
			fallbacks.STT = append(fallbacks.STT, slngModelName(fallback.Binding))
		}
	}
	for _, name := range agent.Models[entry.Model].Fallback {
		if binding := tgt.Models.Reason[name]; binding.Model != "" {
			fallbacks.LLM = append(fallbacks.LLM, slngModelName(binding))
		}
	}
	if len(fallbacks.STT) == 0 && len(fallbacks.LLM) == 0 && len(fallbacks.TTS) == 0 {
		return nil
	}
	return &fallbacks
}

// slngModelName is the fold rule described above, in one place so the primary
// models and every fallback entry cannot disagree about it.
//
// There is deliberately no Context Router arm. `provider: slng` on a think model
// selects unmute's *client-side* router support, which builds a regional base
// URL, two identity headers and an inline configuration onto every request from
// generated Python. This target emits no client, so the catalogue refuses that
// binding here by name. SLNG's own server-side routing is a different mechanism
// and reaches the body through llm_router_enabled instead.
func slngModelName(binding ir.Binding) string {
	if binding.Provider == "" || strings.Contains(binding.Model, "/") {
		return binding.Model
	}
	return binding.Provider + "/" + binding.Model
}

// slngKwargs forwards a binding's params as written. It exists to turn a nil map
// into an empty one, because the reference export carries {} for all three and an
// absent key is a different statement from an empty object.
func slngKwargs(params map[string]any) map[string]any {
	if len(params) == 0 {
		return map[string]any{}
	}
	return params
}

// slngTools walks the entry agent's tools in the order the package lists them,
// so the emitted body's order follows the package rather than a map iteration.
func slngTools(agent *ir.Agent, tgt ir.Target, entry ir.AgentDef) ([]slngRef, []slngMCP, []slngToolFile, error) {
	refs := []slngRef{}
	mcpRefs := []slngMCP{}
	var files []slngToolFile
	for _, name := range entry.Tools {
		tool, ok := agent.Tools[name]
		if !ok {
			// A control, not a tool. Controls reach SLNG as curated capabilities
			// attached in the dashboard, which the capability table already says.
			continue
		}
		if tool.Execution == ir.ToolMCP {
			for _, exposed := range tool.MCPTools {
				mcpRefs = append(mcpRefs, slngMCP{Server: name, Tool: exposed, Invocation: slngInvocation})
			}
			continue
		}
		ref := slngRef{
			Tool:        name,
			Description: tool.Description,
			Invocation:  slngInvocation,
		}
		if tool.Announce != "" {
			ref.Policy = &slngPolicy{PreActionMessage: &slngPreAction{
				Enabled: true,
				Text:    slngSegments{Segments: []slngSegment{{Type: "literal", Value: tool.Announce}}},
			}}
		}
		ref.Arguments = slngArguments(tool.Inject)
		file, config, err := slngToolBody(name, tool)
		if err != nil {
			return nil, nil, nil, err
		}
		ref.Config = config
		if file != nil {
			files = append(files, *file)
		}
		refs = append(refs, ref)
	}
	slices.SortStableFunc(files, func(a, b slngToolFile) int { return strings.Compare(a.Name, b.Name) })
	return refs, mcpRefs, files, nil
}

// slngNotes are the forwarded-without-validation facts the compile report must
// carry, because a value unmute passes through untouched is a value it cannot
// promise anything about (Principle II).
func slngNotes(built slngArtifacts) []string {
	notes := []string{
		"slng target: model strings are forwarded to SLNG exactly as written; SLNG owns its own model list, so unmute checks no vendor or model name here",
	}
	if len(built.Body.MCPRefs) > 0 {
		notes = append(notes, "slng target: each MCP reference is written by name and is incomplete on purpose; the push step must connect to the server to obtain the schema hash SLNG requires")
	}
	if len(built.ToolFiles) > 0 {
		notes = append(notes, "slng target: tool bodies are written with names where the platform assigns identifiers; the push step creates each tool and resolves its name")
	}
	notes = append(notes, "slng target: region "+built.Body.Region+" is written as declared")
	return notes
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

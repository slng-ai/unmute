package generate

import (
	"cmp"
	"fmt"

	"github.com/slng-ai/unmute/internal/ir"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// defaultCatalog is the built-in provider map, and the only one there is.
var defaultCatalog = targetcap.DefaultCatalog()

// mcpTimeoutSeconds is how long an MCP tool call may take on either target.
// Both drivers write it out rather than letting each SDK pick: LiveKit's
// MCPServerHTTP defaults to 5 seconds, which no web search answers inside, and
// Pipecat's MCP client defaults to 30, so the same package would wait six times
// longer on one target than the other. 30 is Pipecat's existing default, so
// closing the gap changes one target's behaviour and invents no new number.
//
// ponytail: one constant, no knob. A tool that needs longer wants `timeout:` on
// the mcp block, which is a schema change (SCHEMA.md N40) and waits for a real
// second user.
const mcpTimeoutSeconds = 30

// ServiceCall is a resolved constructor, ready for a template: the class and
// its ordered kwargs, each value already a Python expression. SettingsArgs is
// non-empty only for ParamsSettings entries; SettingsArg/SettingsClass name
// its wrapper (default settings=Class.Settings(...), soniox params=STTOptions).
type ServiceCall struct {
	Class         string
	Args          []pyKV
	SettingsArgs  []pyKV
	SettingsArg   string
	SettingsClass string
}

// resolveService looks a binding's vendor up in the catalogue and builds its
// call. extraSettings are driver-supplied nested args (the workers-model
// system_instruction), inserted after model/voice. Every env var the call reads
// registers on env. site carries the two expressions a SLNG Context Router
// construction needs and is the zero value everywhere else.
func resolveService(fw targetcap.Provider, role targetcap.Role,
	binding ir.Binding, env *envSet, site slngSite, extraSettings ...pyKV) (ServiceCall, targetcap.Entry, error) {

	// A router think binding consumes params.world_part_override into the base
	// URL, so it must not also reach the client as a kwarg the SDK never heard
	// of (D2).
	//
	// The filtered map is a local rather than a write back onto binding.Params,
	// because slngRequestBody reads the pure-proxy switch and the forwardable
	// params off the binding itself. Handing it a binding whose params had
	// already been consumed here is how the switch would silently stop being
	// sent.
	router := role == targetcap.Reason && binding.Router()
	region := slngRegion(binding)
	params := binding.Params
	if router {
		params = slngConsumedParams(params)
	}

	vendor := binding.Provider
	if vendor == "" {
		vendor = "openai" // the schema's OpenAI-compatible default spelling
	}
	// Same rulebook as ir.Validate (Catalog.CheckVendor): vendor known,
	// wildcard-needs-endpoint, endpoint-has-a-slot.
	if err := defaultCatalog.CheckVendor(fw, role, vendor, binding.EndpointEnv != ""); err != nil {
		return ServiceCall{}, targetcap.Entry{}, err
	}
	entry, ok := defaultCatalog.Lookup(fw, role, vendor)
	if !ok {
		return ServiceCall{}, entry, fmt.Errorf("%s %s binding provider %q has no slot; no providers are catalogued for this role", fw, role, vendor)
	}
	spec := entry.Call

	responsesAPI := false
	if fw == targetcap.LiveKit && role == targetcap.Reason && vendor == "openai" {
		if api, ok := binding.Params["api"]; ok {
			if api != "responses" {
				return ServiceCall{}, entry, fmt.Errorf("livekit openai reason binding has unsupported api %v; want %q", api, "responses")
			}
			responsesAPI = true
		}
		if _, reasoning := binding.Params["reasoning"]; responsesAPI && reasoning {
			return ServiceCall{}, entry, fmt.Errorf("livekit openai responses binding does not accept raw reasoning; use reasoning_effort")
		}
	}

	call := ServiceCall{Class: spec.Class}
	if responsesAPI {
		call.Class = "openai.responses.LLM"
	}
	flat := func(kv pyKV) { call.Args = append(call.Args, kv) }
	nested := flat
	if spec.Params == targetcap.ParamsSettings {
		nested = func(kv pyKV) { call.SettingsArgs = append(call.SettingsArgs, kv) }
		call.SettingsArg = cmp.Or(spec.SettingsArg, "settings")
		call.SettingsClass = cmp.Or(spec.SettingsClass, spec.Class+".Settings")
	}

	if spec.APIKeyArg != "" {
		keyEnv := spec.APIKeyEnv
		if keyEnv == "" {
			keyEnv = apiKeyEnv(vendor)
		}
		env.addRead(keyEnv)
		flat(pyKV{Key: spec.APIKeyArg, Value: envRef(keyEnv)})
	}
	for _, name := range spec.ExtraEnvs {
		env.addRead(name) // read implicitly by the constructor (AWS SDK creds)
	}
	if binding.EndpointEnv != "" { // slotting already checked by CheckVendor
		env.addRead(binding.EndpointEnv)
		flat(pyKV{Key: spec.Endpoint.Arg, Value: envRef(binding.EndpointEnv)})
	}
	if router {
		// The region is the whole endpoint story here: one owner for the URL
		// form, and a compile-time literal rather than a variable the operator
		// could set to something else (D2).
		url, ok := targetcap.SlngRouterBaseURL(region)
		if !ok {
			return ServiceCall{}, entry, fmt.Errorf("%s reason binding provider %q: %q is not a router region", fw, vendor, region)
		}
		flat(pyKV{Key: spec.Endpoint.Arg, Value: pyQuote(url)})
		// Every upstream credential the request body carries joins the startup
		// check, so a missing value fails at boot rather than on the first turn
		// of a live call (FR-034b).
		for _, name := range slngBindingCredentialEnvs(binding) {
			env.addRead(name)
		}
	}
	voice := cmp.Or(binding.Voice, binding.VoiceID)
	if voice != "" {
		if spec.Voice.Arg == "" {
			return ServiceCall{}, entry, fmt.Errorf("%s %s binding provider %q: voice has no slot here", fw, role, vendor)
		}
		nested(pyKV{Key: spec.Voice.Arg, Value: pyQuote(voice)})
	} else if spec.Voice.Required {
		return ServiceCall{}, entry, fmt.Errorf("%s %s binding provider %q is missing a voice", fw, role, vendor)
	}
	if binding.Model != "" {
		nested(pyKV{Key: spec.Model.Arg, Value: pyQuote(formModel(spec.Model.Form, binding))})
	} else if spec.Model.Required {
		return ServiceCall{}, entry, fmt.Errorf("%s %s binding is missing a model", fw, role)
	}
	// Language is per-model (N16): emitted only when the model sets one. Unset
	// means no language kwarg — the provider default or the model route's own
	// encoding (slng/deepgram/nova:3-en) wins. A language on an entry with no
	// slot is a gated error, never a silent drop.
	if binding.Language != "" && (role == targetcap.Listen || role == targetcap.Speak) {
		if spec.NoLanguage || spec.Language.Arg == "" {
			return ServiceCall{}, entry, fmt.Errorf("%s %s binding provider %q has no language slot", fw, role, vendor)
		}
		// A target-specific param is an explicit integration override; avoid
		// emitting the same Python kwarg twice.
		if _, overridden := params[spec.Language.Arg]; !overridden {
			nested(pyKV{Key: spec.Language.Arg, Value: pyQuote(binding.Language)})
		}
	}
	for _, kv := range extraSettings {
		nested(kv)
	}
	switch spec.Params {
	case targetcap.ParamsExtraKwargs:
		if len(params) > 0 {
			flat(pyKV{Key: "extra_kwargs", Value: pyLiteral(params)})
		}
	default: // kwargs and settings: one kwarg per param, sorted
		// A router binding's forwardable params ride the request body
		// (target.SlngRequestBodyArg), so on the router they split out of the
		// construction whether or not this entry has a Settings overflow field of
		// its own. slngRequestBody is what picks them up, which is why the split
		// half is dropped here rather than sent a second time.
		overflowArg := spec.SettingsOverflow
		if router {
			overflowArg = targetcap.SlngRequestBodyArg
		}
		fields, overflow := splitParams(params, overflowArg)
		if router {
			// Pipecat merges Settings.extra into the request params, so the two
			// dicts ride there; LiveKit takes them as constructor kwargs. Either
			// way they reach the same place in the request.
			overflow = slngRequestExtras(site, binding)
			if spec.SettingsOverflow == "" {
				for _, kv := range forwardParams(overflow) {
					flat(kv)
				}
				overflow = nil
			}
		}
		for _, kv := range forwardParams(fields) {
			if responsesAPI {
				switch kv.Key {
				case "api":
					continue
				case "reasoning_effort":
					kv.Key = "reasoning"
					kv.Value = "openai_types.Reasoning(effort=" + kv.Value + ")"
				}
			}
			nested(kv)
		}
		if len(overflow) > 0 {
			nested(pyKV{Key: spec.SettingsOverflow, Value: pyLiteral(overflow)})
		}
	}
	return call, entry, nil
}

// foldedFields are the param names unmute writes itself, from the typed model
// fields in ir/build.go foldParams. They name real Settings fields on every
// entry that has them, so they stay where they are. Anything else under
// `params:` is the author's provider-specific passthrough, which is what an
// overflow field is for.
var foldedFields = map[string]bool{"temperature": true, "top_p": true, "top_k": true, "speed": true}

// splitParams separates the params that stay constructor/Settings arguments
// from the ones that ride the entry's overflow field. With no overflow field
// declared every param stays where it was, which is every entry but Pipecat's
// two OpenAI reason rows.
func splitParams(params map[string]any, overflowArg string) (fields, overflow map[string]any) {
	if overflowArg == "" || len(params) == 0 {
		return params, nil
	}
	fields = make(map[string]any, len(params))
	for k, v := range params {
		if foldedFields[k] {
			fields[k] = v
			continue
		}
		if overflow == nil {
			overflow = make(map[string]any, len(params))
		}
		overflow[k] = v
	}
	return fields, overflow
}

// formModel applies the entry's named model transform.
func formModel(form targetcap.ModelForm, binding ir.Binding) string {
	switch form {
	case targetcap.FormProviderSlashModel:
		// provider: livekit is the deliberate Inference spelling — the model
		// already carries the provider/model route verbatim (driver-livekit V19).
		if binding.Provider != "" && binding.Provider != "livekit" {
			return binding.Provider + "/" + binding.Model
		}
		return binding.Model
	default:
		return binding.Model
	}
}

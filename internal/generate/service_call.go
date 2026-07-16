package generate

import (
	"fmt"
	"strings"

	"github.com/slng/unmute/internal/ir"
	targetcap "github.com/slng/unmute/internal/target"
)

// defaultCatalog is the built-in provider map. It becomes a parameter when
// the providers.yaml overlay loader lands (PROVIDER_CATALOG.md, step 6).
var defaultCatalog = targetcap.DefaultCatalog()

// ServiceCall is a resolved constructor, ready for a template: the class and
// its ordered kwargs, each value already a Python expression. SettingsArgs is
// non-empty only for ParamsSettings entries (Pipecat's Class.Settings(...)).
type ServiceCall struct {
	Class        string
	Args         []pyKV
	SettingsArgs []pyKV
}

// resolveService looks a binding's vendor up in the catalogue and builds its
// call. envRef renders the driver's environment-lookup idiom; extraSettings
// are driver-supplied nested args (the workers-model system_instruction),
// inserted after model/voice. Every env var the call reads registers on env.
func resolveService(cat targetcap.Catalog, fw targetcap.Provider, role targetcap.Role,
	binding ir.Binding, envRef func(string) string, env *envSet, extraSettings ...pyKV) (ServiceCall, targetcap.Entry, error) {

	vendor := binding.Provider
	if vendor == "" {
		vendor = "openai" // the schema's OpenAI-compatible default spelling
	}
	// Same rulebook as ir.Validate (Catalog.CheckVendor): vendor known,
	// wildcard-needs-endpoint, endpoint-has-a-slot.
	if err := cat.CheckVendor(fw, role, vendor, binding.EndpointEnv != ""); err != nil {
		return ServiceCall{}, targetcap.Entry{}, err
	}
	entry, ok := cat.Lookup(fw, role, vendor)
	if !ok {
		return ServiceCall{}, entry, fmt.Errorf("%s %s binding provider %q has no slot; no providers are catalogued for this role", fw, role, vendor)
	}
	spec := entry.Call

	call := ServiceCall{Class: spec.Class}
	flat := func(kv pyKV) { call.Args = append(call.Args, kv) }
	nested := flat
	if spec.Params == targetcap.ParamsSettings {
		nested = func(kv pyKV) { call.SettingsArgs = append(call.SettingsArgs, kv) }
	}

	if spec.APIKeyArg != "" {
		keyEnv := spec.APIKeyEnv
		if keyEnv == "" {
			keyEnv = apiKeyEnv(vendor)
		}
		env.add(keyEnv)
		flat(pyKV{Key: spec.APIKeyArg, Value: envRef(keyEnv)})
	}
	if binding.EndpointEnv != "" { // slotting already checked by CheckVendor
		env.add(binding.EndpointEnv)
		flat(pyKV{Key: spec.Endpoint.Arg, Value: envRef(binding.EndpointEnv)})
	}
	voice := firstNonEmpty(binding.Voice, binding.VoiceID)
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
	for _, kv := range extraSettings {
		nested(kv)
	}
	switch spec.Params {
	case targetcap.ParamsExtraKwargs:
		if len(binding.Params) > 0 {
			flat(pyKV{Key: "extra_kwargs", Value: pyLiteral(binding.Params)})
		}
	default: // kwargs and settings: one kwarg per param, sorted
		for _, kv := range forwardParams(binding.Params) {
			nested(kv)
		}
	}
	return call, entry, nil
}

// formModel applies the entry's named model transform.
func formModel(form targetcap.ModelForm, binding ir.Binding) string {
	switch form {
	case targetcap.FormSlngRoute:
		return strings.TrimPrefix(binding.Model, "slng/")
	case targetcap.FormProviderSlashModel:
		if binding.Provider != "" {
			return binding.Provider + "/" + binding.Model
		}
		return binding.Model
	default:
		return binding.Model
	}
}


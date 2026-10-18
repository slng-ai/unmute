package generate

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/slng-ai/unmute/internal/ir"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// coreSpeech is the speech a version 2 project builds from version 1's
// catalogue: every listen and speak binding that is not SLNG's. SLNG speech
// stays in the entrypoint, which hands it the call's tracking ids.
type coreSpeech struct {
	Source   string   // speech.py
	Extras   []string // the framework's own extras the catalogue rows install
	Packages []string // other packages, with their version floor
	Env      []string // the variables the constructors read
}

var pythonName = regexp.MustCompile(`[^A-Za-z0-9_]`)

// buildCoreSpeech renders speech.py: LISTEN, the speech-to-text model when it
// is not SLNG's, and SPEAK, each voice that is not SLNG's by its name in
// agent.yaml. Each constructor is v1's, from the same catalogue row.
func buildCoreSpeech(resolved ir.Target) (coreSpeech, error) {
	fw := targetcap.Provider(resolved.Provider)
	env := newEnvSet()
	imports := []string{}
	out := coreSpeech{}
	render := func(role targetcap.Role, binding ir.Binding) (string, error) {
		call, entry, err := resolveService(fw, role, binding, env, slngSite{})
		if err != nil {
			return "", err
		}
		if entry.Import != "" && !slices.Contains(imports, entry.Import) {
			imports = append(imports, entry.Import)
		}
		if extra := entry.Install.Extra; extra != "" && !slices.Contains(out.Extras, extra) {
			out.Extras = append(out.Extras, extra)
		}
		if pkg := entry.Install.Package; pkg != "" && !slices.Contains(out.Packages, pkg+entry.Install.Constraint) {
			out.Packages = append(out.Packages, pkg+entry.Install.Constraint)
		}
		return renderCall(call), nil
	}
	var body strings.Builder
	listen := "None"
	if binding := resolved.Models.Listen; binding != nil && binding.Provider != "slng" {
		call, err := render(targetcap.Listen, *binding)
		if err != nil {
			return out, fmt.Errorf("listen: %w", err)
		}
		fmt.Fprintf(&body, "\n\ndef listen() -> Any:\n    \"\"\"Return the speech-to-text model agent.yaml names.\"\"\"\n    return %s\n", call)
		listen = "listen"
	}
	var voices []string
	for _, name := range sortedKeys(resolved.Models.Speak) {
		binding := resolved.Models.Speak[name]
		if binding.Provider == "slng" {
			continue
		}
		call, err := render(targetcap.Speak, binding)
		if err != nil {
			return out, fmt.Errorf("speak %q: %w", name, err)
		}
		function := "speak_" + pythonName.ReplaceAllString(name, "_")
		fmt.Fprintf(&body, "\n\ndef %s() -> Any:\n    \"\"\"Return the voice %s.\"\"\"\n    return %s\n", function, pyQuote(name), call)
		voices = append(voices, fmt.Sprintf("%s: %s", pyQuote(name), function))
	}
	var realtime []string
	for _, name := range sortedKeys(resolved.Models.Realtime) {
		call, extraImports, err := coreRealtimeCall(fw, resolved.Models.Realtime[name], env, render)
		if err != nil {
			return out, fmt.Errorf("realtime %q: %w", name, err)
		}
		for _, line := range extraImports {
			if !slices.Contains(imports, line) {
				imports = append(imports, line)
			}
		}
		function := "realtime_" + pythonName.ReplaceAllString(name, "_")
		fmt.Fprintf(&body, "\n\ndef %s() -> Any:\n    \"\"\"Return the realtime model %s: it listens, thinks and speaks.\"\"\"\n    return %s\n", function, pyQuote(name), call)
		realtime = append(realtime, fmt.Sprintf("%s: %s", pyQuote(name), function))
	}
	for _, name := range sortedKeys(resolved.Models.Live) {
		call, extraImport, err := coreLiveCall(fw, resolved.Models.Live[name], resolved.Models.Reason, env, render)
		if err != nil {
			return out, fmt.Errorf("live %q: %w", name, err)
		}
		if extraImport != "" && !slices.Contains(imports, extraImport) {
			imports = append(imports, extraImport)
		}
		function := "realtime_" + pythonName.ReplaceAllString(name, "_")
		fmt.Fprintf(&body, "\n\ndef %s() -> Any:\n    \"\"\"Return the live model %s: it listens and speaks, and hands careful work to its backend.\"\"\"\n    return %s\n", function, pyQuote(name), call)
		realtime = append(realtime, fmt.Sprintf("%s: %s", pyQuote(name), function))
	}
	slices.Sort(imports)
	out.Source = "\"\"\"This agent's speech models that are not SLNG's. Written by `unmute compile`.\n\n" +
		"Each constructor comes from the same catalogue row version 1 uses. SLNG speech is built in\n" +
		"the entrypoint, with the call's tracking ids.\n\"\"\"\n\n" +
		"import os  # noqa: F401 - a constructor may read a key from the environment\n" +
		"from collections.abc import Callable\nfrom typing import Any\n\n" +
		strings.Join(imports, "\n") + "\n" + body.String() +
		fmt.Sprintf("\n\nLISTEN: Callable[[], Any] | None = %s\n\"\"\"The speech-to-text model when it is not SLNG's, else None.\"\"\"\n", listen) +
		fmt.Sprintf("SPEAK: dict[str, Callable[[], Any]] = {%s}\n\"\"\"Each voice that is not SLNG's, by its name in agent.yaml.\"\"\"\n", strings.Join(voices, ", ")) +
		fmt.Sprintf("REALTIME: dict[str, Callable[[], Any]] = {%s}\n\"\"\"Each realtime model, by its name in agent.yaml.\"\"\"\n", strings.Join(realtime, ", "))
	out.Env = env.sorted()
	return out, nil
}

// renderCall writes one ServiceCall as a Python expression, the way version 1's
// "svc" template does.
func renderCall(call ServiceCall) string {
	args := make([]string, 0, len(call.Args)+1)
	for _, kv := range call.Args {
		args = append(args, kv.Key+"="+kv.Value)
	}
	if len(call.SettingsArgs) > 0 {
		inner := make([]string, 0, len(call.SettingsArgs))
		for _, kv := range call.SettingsArgs {
			inner = append(inner, kv.Key+"="+kv.Value)
		}
		args = append(args, call.SettingsArg+"="+call.SettingsClass+"("+strings.Join(inner, ", ")+")")
	}
	return call.Class + "(" + strings.Join(args, ", ") + ")"
}

// coreRealtimeCall renders one realtime model the way version 1's drivers do.
// LiveKit takes the voice as an argument and the turn detection as a kwarg.
// Pipecat takes neither as an argument: both live in the service's session
// properties, which version 1's own function writes.
func coreRealtimeCall(fw targetcap.Provider, binding ir.Binding, env *envSet,
	render func(targetcap.Role, ir.Binding) (string, error)) (string, []string, error) {
	if fw == targetcap.LiveKit {
		call, _, err := resolveService(fw, targetcap.Realtime, binding, env, slngSite{})
		if err != nil {
			return "", nil, err
		}
		var imports []string
		if binding.TurnDetection != "" {
			profile, err := targetcap.ResolveTurnDetection(targetcap.LiveKit, binding.TurnDetection)
			if err != nil {
				return "", nil, err
			}
			if !profile.Omit {
				call.Args = append(call.Args, pyKV{Key: "turn_detection", Value: profile.Expr})
				imports = profile.Imports
			}
		}
		if _, err := render(targetcap.Realtime, binding); err != nil { // the row's import and install
			return "", nil, err
		}
		return renderCall(call), imports, nil
	}
	voice := cmp.Or(binding.Voice, binding.VoiceID)
	binding.Voice, binding.VoiceID = "", ""
	call, _, err := resolveService(fw, targetcap.Realtime, binding, env, slngSite{})
	if err != nil {
		return "", nil, err
	}
	properties, err := pipecatRealtimeSessionProperties(binding.TurnDetection, voice, false)
	if err != nil {
		return "", nil, err
	}
	if call.SettingsArg == "" {
		call.SettingsArg, call.SettingsClass = "settings", call.Class+".Settings"
	}
	call.SettingsArgs = append(call.SettingsArgs, pyKV{Key: "session_properties", Value: properties})
	if _, err := render(targetcap.Realtime, binding); err != nil { // the row's import and install
		return "", nil, err
	}
	return renderCall(call), []string{"from pipecat.services.openai.realtime import events as realtime_events"}, nil
}

// coreLiveCall renders one live model the way version 1's drivers do. The
// backend think model it names runs the tools and the careful work, through
// OpenAI's Responses delegation, which both frameworks' live models take as a
// constructor argument. With no backend, LiveKit hands that work to the client,
// which has no tool channel: validation has refused tools in that case.
func coreLiveCall(fw targetcap.Provider, binding ir.Binding, reason map[string]ir.Binding, env *envSet,
	render func(targetcap.Role, ir.Binding) (string, error)) (string, string, error) {
	call, _, err := resolveService(fw, targetcap.Live, binding, env, slngSite{})
	if err != nil {
		return "", "", err
	}
	if _, err := render(targetcap.Live, binding); err != nil { // the row's import and install
		return "", "", err
	}
	backend, hasBackend := reason[binding.Backend]
	switch {
	case fw == targetcap.LiveKit && !hasBackend:
		call.Args = append(call.Args, pyKV{Key: "delegation", Value: pyQuote("client")})
	case fw == targetcap.LiveKit:
		call.Args = append(call.Args,
			pyKV{Key: "delegation", Value: pyQuote("responses")},
			pyKV{Key: "responses_options", Value: "{\"model\": " + pyQuote(backend.Model) + "}"})
	case hasBackend:
		call.Args = append(call.Args, pyKV{
			Key:   "delegation",
			Value: "OpenAILiveLLMService.ResponsesDelegation(settings=OpenAIResponsesLLMService.Settings(model=" + pyQuote(backend.Model) + "))",
		})
		return renderCall(call), "from pipecat.services.openai.responses.llm import OpenAIResponsesLLMService", nil
	}
	return renderCall(call), "", nil
}

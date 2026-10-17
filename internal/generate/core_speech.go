package generate

import (
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
	slices.Sort(imports)
	out.Source = "\"\"\"This agent's speech models that are not SLNG's. Written by `unmute compile`.\n\n" +
		"Each constructor comes from the same catalogue row version 1 uses. SLNG speech is built in\n" +
		"the entrypoint, with the call's tracking ids.\n\"\"\"\n\n" +
		"import os  # noqa: F401 - a constructor may read a key from the environment\n" +
		"from collections.abc import Callable\nfrom typing import Any\n\n" +
		strings.Join(imports, "\n") + "\n" + body.String() +
		fmt.Sprintf("\n\nLISTEN: Callable[[], Any] | None = %s\n\"\"\"The speech-to-text model when it is not SLNG's, else None.\"\"\"\n", listen) +
		fmt.Sprintf("SPEAK: dict[str, Callable[[], Any]] = {%s}\n\"\"\"Each voice that is not SLNG's, by its name in agent.yaml.\"\"\"\n", strings.Join(voices, ", "))
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

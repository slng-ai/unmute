package generate

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"text/template"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

//go:embed templates/core/spec.py
var coreSpecPy string

//go:embed templates/core/livekit/agent.py
var coreLiveKitAgentPy string

//go:embed templates/core/livekit/pyproject.toml
var coreLiveKitPyproject string

//go:embed templates/core/pipecat/bot.py
var corePipecatBotPy string

//go:embed templates/core/pipecat/pyproject.toml
var corePipecatPyproject string

//go:embed templates/core/livekit/compose.dev.yaml
var coreLiveKitCompose string

//go:embed templates/core/pipecat/compose.dev.yaml
var corePipecatCompose string

//go:embed templates/core/Dockerfile
var coreDockerfile string

//go:embed templates/core/dockerignore
var coreDockerignore string

// coreDev is what `unmute dev` runs each framework's project with: its compose
// file, and the command the image starts.
var coreDev = map[ir.Provider]struct{ compose, command string }{
	ir.ProviderLiveKit: {coreLiveKitCompose, `["python", "agent.py", "start"]`},
	ir.ProviderPipecat: {corePipecatCompose, `["python", "bot.py", "-t", "webrtc", "--host", "0.0.0.0", "--port", "7860"]`},
}

// coreEntrypoints are the two files each framework's project differs by: its
// entrypoint module and its pyproject. Everything else is shared.
var coreEntrypoints = map[ir.Provider][2]struct{ path, source string }{
	ir.ProviderLiveKit: {{"agent.py", coreLiveKitAgentPy}, {"pyproject.toml", coreLiveKitPyproject}},
	ir.ProviderPipecat: {{"bot.py", corePipecatBotPy}, {"pyproject.toml", corePipecatPyproject}},
}

// coreReport is compile-report.json for a project on unmute-core: what the
// project needs set before it runs.
type coreReport struct {
	Target      string   `json:"target"`
	Runtime     string   `json:"runtime"`
	RequiredEnv []string `json:"required_env"`
}

// GenerateCore emits a package as a LiveKit or Pipecat project on
// unmute-core: agent.json holds the package's data, and spec.py and the
// framework's entrypoint are the same in every project. The author's state.py
// and tool handlers are copied as written.
func GenerateCore(agent *ir.Agent, resolved ir.Target) ([]File, error) {
	entrypoints, ok := coreEntrypoints[resolved.Provider]
	if !ok {
		return nil, fmt.Errorf("unmute-core compiles for livekit and pipecat, not %s", resolved.Provider)
	}
	spec, err := coreSpec(agent)
	if err != nil {
		return nil, err
	}
	if spec["models"], err = coreModels(agent, resolved); err != nil {
		return nil, err
	}
	transfers, err := coreTransfers(agent, resolved)
	if err != nil {
		return nil, err
	}
	spec["transfers"] = transfers
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return nil, err
	}
	speech, err := buildCoreSpeech(agent, resolved)
	if err != nil {
		return nil, err
	}
	fill := map[string]string{"CorePin": target.CorePin, "Name": agent.Name, "Extras": "", "Deps": "", "FrameworkExtras": ""}
	for _, extra := range speech.Extras {
		fill["FrameworkExtras"] += "," + extra
	}
	for _, pkg := range speech.Packages {
		fill["Deps"] += fmt.Sprintf("\n    %q,", pkg)
	}
	needs := slices.Clone(speech.Env) // variables the speech, knowledge and webhooks read
	if len(agent.Knowledge) > 0 {
		fill["Extras"] = ",knowledge"
		var deps []string
		for _, name := range sortedKeys(agent.Knowledge) {
			embed, _ := target.LookupEmbeddingService(agent.Knowledge[name].Embed)
			if !slices.Contains(deps, embed.PythonDep) {
				deps = append(deps, embed.PythonDep)
				fill["Deps"] += fmt.Sprintf("\n    %q,", embed.PythonDep)
			}
			if embed.CredentialEnv != "" {
				needs = append(needs, embed.CredentialEnv)
			}
		}
	}
	// .python-version holds uv to the Python unmute-core is developed and tested on.
	files := []File{
		{Path: "agent.json", Content: append(data, '\n')},
		{Path: ".python-version", Content: []byte("3.12\n")},
		{Path: "speech.py", Content: []byte(speech.Source)},
	}
	for _, item := range append([]struct{ path, source string }{{"spec.py", coreSpecPy}}, entrypoints[:]...) {
		content, err := fillTemplate(item.source, fill)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", item.path, err)
		}
		files = append(files, File{Path: item.path, Content: content})
	}
	if agent.StateSource != "" {
		files = append(files, File{Path: "state.py", Content: []byte(agent.StateSource), Verbatim: true})
	}
	handlers := map[string]string{}
	for _, name := range sortedKeys(agent.Tools) {
		tool := agent.Tools[name]
		if tool.Execution != ir.ToolLocal {
			continue // a knowledge or builtin tool has no handler of its own
		}
		handlers[tool.Handler] = tool.HandlerSource
		if file, _, _ := strings.Cut(tool.Precheck, ":"); file != "" && file != tool.Handler {
			return nil, fmt.Errorf("tool %q: its precheck is in %s and its handler in %s; keep both in one file", name, file, tool.Handler)
		}
	}
	for _, path := range sortedKeys(handlers) {
		files = append(files, File{Path: path, Content: []byte(handlers[path]), Verbatim: true})
	}
	for _, document := range knowledgeFiles(agent) {
		document.Verbatim = true
		files = append(files, document)
	}
	env := browserSecrets(agent, resolved)
	for _, binding := range resolved.Models.Reason {
		needs = append(needs, slngBindingCredentialEnvs(binding)...)
	}
	for _, name := range sortedKeys(agent.Tools) {
		if tool := agent.Tools[name]; tool.Execution == ir.ToolWebhook {
			needs = append(needs, tool.URLEnv)
			if tool.Auth != nil && tool.Auth.TokenEnv != "" {
				needs = append(needs, tool.Auth.TokenEnv)
			}
		}
	}
	for _, name := range needs {
		if !slices.Contains(env, name) {
			env = append(env, name)
		}
	}
	slices.Sort(env)
	var example strings.Builder
	for _, name := range env {
		fmt.Fprintf(&example, "%s=\n", name)
	}
	phone := slices.DeleteFunc(slices.Clone(agent.Secrets), func(name string) bool { return slices.Contains(env, name) })
	for _, name := range resolved.Destinations {
		if !slices.Contains(phone, name) {
			phone = append(phone, name)
		}
	}
	if len(phone) > 0 {
		slices.Sort(phone)
		example.WriteString("\n# Phone calls only. A browser session starts without these.\n")
		for _, name := range phone {
			fmt.Fprintf(&example, "%s=\n", name)
		}
	}
	files = append(files, File{Path: ".env.example", Content: []byte(example.String())})
	dev := coreDev[resolved.Provider]
	fill["AgentName"], fill["Command"], fill["BuildTools"] = agent.DeployName(resolved), dev.command, ""
	if len(agent.Knowledge) > 0 {
		// The knowledge extra's stemmer builds from source where no wheel fits.
		fill["BuildTools"] = "RUN apt-get update && apt-get install -y --no-install-recommends gcc g++ \\\n && rm -rf /var/lib/apt/lists/*\n"
	}
	for _, name := range env {
		fill["DevEnv"] += "\n      - " + name
	}
	for _, item := range []struct{ path, source string }{{"compose.dev.yaml", dev.compose}, {"Dockerfile", coreDockerfile}} {
		content, err := fillTemplate(item.source, fill)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", item.path, err)
		}
		files = append(files, File{Path: item.path, Content: content})
	}
	files = append(files, File{Path: ".dockerignore", Content: []byte(coreDockerignore)})
	report, err := json.MarshalIndent(coreReport{Target: resolved.Name, Runtime: "unmute-core==" + target.CorePin, RequiredEnv: env}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(files, File{Path: "compile-report.json", Content: append(report, '\n')}), nil
}

func fillTemplate(source string, fill map[string]string) ([]byte, error) {
	parsed, err := template.New("").Option("missingkey=error").Parse(source)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := parsed.Execute(&out, fill); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// browserSecrets is the package's secrets a browser session needs: every one
// but the phone route's. A connection's credentials, the target's telephony
// variables and a transfer's number are read on a phone call only, so a
// browser session starts without them.
func browserSecrets(agent *ir.Agent, resolved ir.Target) []string {
	route := map[string]bool{}
	for _, connection := range agent.Connections {
		for _, name := range connection.Environment {
			route[name] = true
		}
	}
	if plan := resolved.Telephony; plan != nil {
		for _, name := range plan.Environment {
			route[name] = true
		}
		for _, name := range slices.Concat(plan.RequiredEnvironment, plan.LocalEnvironment) {
			route[name] = true
		}
	}
	for _, name := range resolved.Destinations {
		route[name] = true
	}
	return slices.DeleteFunc(slices.Clone(agent.Secrets), func(name string) bool { return route[name] })
}

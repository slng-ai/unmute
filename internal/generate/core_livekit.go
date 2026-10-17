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

// coreLiveKitReport is compile-report.json for a project on unmute-core: what
// the project needs set before it runs.
type coreLiveKitReport struct {
	Target      string   `json:"target"`
	Runtime     string   `json:"runtime"`
	RequiredEnv []string `json:"required_env"`
}

// GenerateCoreLiveKit emits a version 2 package as a LiveKit project on
// unmute-core: agent.json holds the package's data, and spec.py and agent.py
// are the same in every project. The author's state.py and tool handlers are
// copied as written.
func GenerateCoreLiveKit(agent *ir.Agent, resolved ir.Target) ([]File, error) {
	spec, err := coreSpec(agent)
	if err != nil {
		return nil, err
	}
	if spec["models"], err = coreModels(resolved); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return nil, err
	}
	fill := map[string]string{"CorePin": target.CorePin, "Name": agent.Name}
	files := []File{{Path: "agent.json", Content: append(data, '\n')}}
	for _, item := range []struct{ path, source string }{
		{"spec.py", coreSpecPy}, {"agent.py", coreLiveKitAgentPy}, {"pyproject.toml", coreLiveKitPyproject},
	} {
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
		handlers[tool.Handler] = tool.HandlerSource
		if file, _, _ := strings.Cut(tool.Precheck, ":"); file != "" && file != tool.Handler {
			return nil, fmt.Errorf("tool %q: its precheck is in %s and its handler in %s; keep both in one file", name, file, tool.Handler)
		}
	}
	for _, path := range sortedKeys(handlers) {
		files = append(files, File{Path: path, Content: []byte(handlers[path]), Verbatim: true})
	}
	env := slices.Clone(agent.Secrets)
	slices.Sort(env)
	var example strings.Builder
	for _, name := range env {
		fmt.Fprintf(&example, "%s=\n", name)
	}
	files = append(files, File{Path: ".env.example", Content: []byte(example.String())})
	report, err := json.MarshalIndent(coreLiveKitReport{Target: resolved.Name, Runtime: "unmute-core==" + target.CorePin, RequiredEnv: env}, "", "  ")
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

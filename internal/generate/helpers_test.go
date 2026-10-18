package generate

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// agentSource names, for artifactFile, the agent's own Python modules joined in
// the order they are emitted, then its prompt files. A test that asserts on what
// the agent does reads this. One that asserts on a particular file names the
// file. The join is not one valid Python file, so a test that runs Python writes
// the modules out with agentModules.
const agentSource = "<agent source>"

// tracingSource names the two modules a traced call runs through, read as one:
// utils/tracing.py shapes the spans and utils/telemetry.py exports them.
const tracingSource = "<tracing source>"

// agentModules are the agent's own Python modules, in the order they are
// emitted.
func agentModules(t *testing.T, artifact Artifact) []File {
	t.Helper()
	var modules []File
	for _, want := range twilioModules {
		for _, file := range artifact.Files {
			if file.Path == want.path {
				modules = append(modules, file)
			}
		}
	}
	if len(modules) == 0 {
		t.Fatal("no agent module emitted")
	}
	return modules
}

func artifactFile(t *testing.T, artifact Artifact, path string) string {
	t.Helper()
	if path == agentSource {
		var joined strings.Builder
		for _, file := range agentModules(t, artifact) {
			joined.Write(file.Content)
		}
		for _, file := range artifact.Files {
			if strings.HasPrefix(file.Path, "prompts/") && strings.HasSuffix(file.Path, ".md") {
				joined.Write(file.Content)
				joined.WriteString("\n")
			}
		}
		return joined.String()
	}
	if path == tracingSource {
		return artifactFile(t, artifact, "utils/tracing.py") + "\n" + artifactFile(t, artifact, "utils/telemetry.py")
	}
	for _, file := range artifact.Files {
		if file.Path == path {
			return string(file.Content)
		}
	}
	t.Fatalf("%s not emitted", path)
	return ""
}

// targetByProvider returns the first target on a provider, by name, because a
// package may declare two targets on one provider.
func targetByProvider(t *testing.T, agent *ir.Agent, provider ir.Provider) ir.Target {
	t.Helper()
	for _, name := range slices.Sorted(maps.Keys(agent.Targets)) {
		if resolved := agent.Targets[name]; resolved.Provider == provider {
			return resolved
		}
	}
	t.Fatalf("no target for provider %q", provider)
	return ir.Target{}
}

// dockerCopies reports whether a COPY line of the Dockerfile puts path, a file
// of the build directory, into the image: named as itself, matched by a glob,
// or inside a directory the line copies whole.
func dockerCopies(dockerfile, path string) bool {
	for _, line := range strings.Split(dockerfile, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "COPY" {
			continue
		}
		for _, source := range fields[1 : len(fields)-1] {
			if strings.HasPrefix(source, "--") {
				continue
			}
			if matched, _ := filepath.Match(source, path); matched || strings.HasSuffix(source, "/") && strings.HasPrefix(path, source) {
				return true
			}
		}
	}
	return false
}

// installLabel is how a catalogue entry gets installed, for the catalogue golden.
func installLabel(entry targetcap.Entry) string {
	host := "pipecat-ai"
	if entry.Framework == targetcap.LiveKit {
		host = "livekit-agents"
	}
	switch {
	case entry.Install.Extra != "":
		return host + "[" + entry.Install.Extra + "]"
	case entry.Install.Package != "":
		return entry.Install.Package + entry.Install.Constraint
	default:
		return "ships with the framework"
	}
}

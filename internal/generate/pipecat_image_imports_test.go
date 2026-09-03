package generate

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// botImport matches a module-scope import in the emitted bot.py, capturing the
// top-level module name: `import dev_metrics`, `from dev_metrics import x`,
// `import tools.fetch_notes` all yield the first dotted segment.
var botImport = regexp.MustCompile(`(?m)^(?:from|import) ([A-Za-z_][A-Za-z0-9_]*)`)

// The Pipecat image copies named files, never `COPY . .`, because /app is the
// base image's own directory and copying over it replaces the server that
// answers /bot. Named files rot: a newly emitted module that bot.py imports is a
// module the image does not carry, and the failure is a container that starts,
// raises ModuleNotFoundError before it answers /bot, and never reaches ready
// while its own log looks healthy. `unmute dev` does not catch it either, since
// compose.dev.yaml bind-mounts the directory over the image's copy.
//
// That has now shipped twice: once as tools/ (v0.1.0 through v0.1.2), and again
// as dev_metrics.py. So the gate reads the emitted bot.py's own imports and
// requires a COPY for every emitted module it names. There is no list to keep in
// step by hand.
func TestPipecatImageCopiesEveryModuleBotImports(t *testing.T) {
	for _, tc := range []struct {
		name  string
		agent func(*testing.T) *ir.Agent
	}{
		{"nothing configured", plainPipecatAgent},
		{"tracing, knowledge and a local tool", loadedPipecatAgent},
		// A mirrored SLNG module is another system's source copied into the
		// project, so it reaches the container through the same conditional
		// COPY line, and a missed flag is the same ModuleNotFoundError.
		{"a mirrored SLNG module", func(t *testing.T) *ir.Agent { return buildHosted(t, hostedCodeFixture) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := tc.agent(t)
			artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
			if err != nil {
				t.Fatalf("generate: %v", err)
			}

			// Every emitted module bot.py could import: a root-level .py file, or
			// the tools package. Mapped to the path a COPY line would name.
			emitted := map[string]string{}
			for _, f := range artifact.Files {
				switch {
				case f.Path == "bot.py":
				case strings.HasPrefix(f.Path, "tools/"):
					emitted["tools"] = "tools/"
				case strings.HasSuffix(f.Path, ".py") && !strings.Contains(f.Path, "/"):
					emitted[strings.TrimSuffix(f.Path, ".py")] = f.Path
				}
			}

			// The blind spot, stated rather than discovered: botImport matches
			// module-scope imports only, so an import written inside a function
			// is invisible to this loop. Every emitted import is module-scope
			// today; a driver that starts writing one inside a handler needs
			// this parser widened rather than a note.
			bot := artifactFile(t, artifact, "bot.py")
			dockerfile := artifactFile(t, artifact, "Dockerfile")
			imported := 0
			for _, m := range botImport.FindAllStringSubmatch(bot, -1) {
				path, local := emitted[m[1]]
				if !local {
					continue // pipecat, stdlib, a third-party package
				}
				imported++
				if !regexp.MustCompile(`(?m)^COPY .*` + regexp.QuoteMeta(path)).MatchString(dockerfile) {
					t.Errorf("bot.py imports %s but the Dockerfile never copies %s: the container will raise ModuleNotFoundError before it answers /bot", m[1], path)
				}
			}
			// A regex that matched nothing would pass this test forever.
			if imported == 0 {
				t.Fatal("found no local imports in bot.py, so this test proves nothing")
			}

		})
	}
}

func plainPipecatAgent(t *testing.T) *ir.Agent {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

// loadedPipecatAgent turns on every feature that adds a module to the image, so
// the conditional COPY lines are held too, not just the unconditional ones.
func loadedPipecatAgent(t *testing.T) *ir.Agent {
	t.Helper()
	agent := knowledgeAgent(t)
	enableLangfuse(agent)
	agent.Tools["fetch_notes"] = ir.Tool{
		Description:   "Fetch the caller's saved notes.",
		Input:         map[string]any{"type": "object", "properties": map[string]any{"topic": map[string]any{"type": "string"}}, "required": []any{"topic"}},
		Execution:     ir.ToolLocal,
		Handler:       "tools/fetch_notes.py",
		HandlerSource: "def fetch_notes(topic):\n    return {\"notes\": []}\n",
		Interruption:  ir.ToolProviderDefault,
		Effect:        ir.ToolReturnsData,
	}
	intake := agent.Agents["intake"]
	intake.Tools = append(intake.Tools, "fetch_notes")
	agent.Agents["intake"] = intake
	return agent
}

// TestPipecatHTTPXImportMatchesItsUseAndItsDependency. Three things have to agree
// about httpx, and they were decided in three different places: `import httpx` in
// bot.py, `"httpx>=0.27"` in pyproject.toml, and whether any emitted body writes
// `httpx.`.
//
// They disagreed. The dependency was gated on a webhook tool and the import was
// gated on a tool not being local, so a knowledge tool — which answers from
// knowledge.py and posts nothing — set the import without setting the dependency.
// A package whose only non-local tools are knowledge lookups then emitted a
// project that fails its own ruff gate (F401) and, had ruff not caught it, would
// have shipped an image importing a distribution it never declared.
//
// Written as an agreement rather than against one example, because the same three
// decisions have to hold for every package: two of them are made while lowering
// tools and the third is made in a template.
func TestPipecatHTTPXImportMatchesItsUseAndItsDependency(t *testing.T) {
	// One package with no httpx anywhere, one with it everywhere, so a build that
	// simply stopped emitting the import could not pass this.
	checkedWith, checkedWithout := false, false
	for _, name := range []string{"salon-concierge", "salon-concierge-single-prompt"} {
		t.Run(name, func(t *testing.T) {
			agent := loadExample(t, name)
			artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			// The blind spot, stated rather than discovered: botImport matches
			// module-scope imports only, so an import written inside a function
			// is invisible to this loop. Every emitted import is module-scope
			// today; a driver that starts writing one inside a handler needs
			// this parser widened rather than a note.
			bot := artifactFile(t, artifact, "bot.py")
			pyproject := artifactFile(t, artifact, "pyproject.toml")

			imported := strings.Contains(bot, "\nimport httpx\n")
			// The import line itself is a `httpx` occurrence, so strip it before
			// asking whether anything uses the module.
			used := strings.Contains(strings.Replace(bot, "\nimport httpx\n", "\n", 1), "httpx.")
			declared := strings.Contains(pyproject, `"httpx`)

			if imported != used {
				t.Errorf("bot.py imports httpx = %v but uses it = %v: an unused import fails the emitted project's ruff gate, and a use with no import is a NameError", imported, used)
			}
			if imported != declared {
				t.Errorf("bot.py imports httpx = %v but pyproject declares it = %v: the image would import a distribution it never asked for", imported, declared)
			}
			checkedWith = checkedWith || imported
			checkedWithout = checkedWithout || !imported
		})
	}
	if !checkedWith || !checkedWithout {
		t.Errorf("covered a package that imports httpx = %v and one that does not = %v; this agreement needs both", checkedWith, checkedWithout)
	}
}

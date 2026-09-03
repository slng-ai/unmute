package generate

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// The hosted-tool fixtures. Two packages rather than one, and the reason is
// worth knowing before reading anything below: a code target requires a `turn:`
// binding and the slng target refuses one, because SLNG owns its own turn
// taking and its create body has no turn section. That is a pre-existing
// cross-target gap, not something hosted tools introduce, so the fixtures work
// around it rather than pretending it is not there.
//
// Both name the same two tools SLNG hosts and commit a mirror of each. Every
// test here compiles a real package rather than a hand-built value, because the
// mirror is read off disk and a hand-built one would prove the reading works by
// not doing it.
const (
	hostedSlngFixture = "slng_hosted"
	hostedCodeFixture = "slng_hosted_code"
)

func buildHosted(t *testing.T, fixture string) *ir.Agent {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

// emitHosted compiles one hosted fixture to one provider and returns its files
// by path.
func emitHosted(t *testing.T, fixture string, provider ir.Provider) map[string]string {
	t.Helper()
	agent := buildHosted(t, fixture)
	artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
	if err != nil {
		t.Fatalf("generate %s: %v", provider, err)
	}
	files := map[string]string{}
	for _, file := range artifact.Files {
		files[file.Path] = string(file.Content)
	}
	return files
}

// TestSlngHostedToolEmitsARefAndNoBody: a hosted reference names a tool the
// platform owns, so the body carries the name and unmute creates nothing.
//
// The negative half is the one that matters. A body would try to create the
// tool, and the push would then own a definition the platform already has, at
// whatever version the package happened to mirror.
func TestSlngHostedToolEmitsARefAndNoBody(t *testing.T) {
	files := emitHosted(t, hostedSlngFixture, ir.ProviderSlng)

	var body struct {
		ToolRefs []struct {
			Tool        string `json:"tool"`
			Description string `json:"description"`
		} `json:"tool_refs"`
	}
	if err := json.Unmarshal([]byte(files["agent.json"]), &body); err != nil {
		t.Fatalf("agent.json is not JSON: %v", err)
	}
	want := map[string]string{
		"check_order": "Look up an order by its number and return its status and delivery date.",
		// No description of its own, so the platform's reaches the model. That
		// is the precedent `builtin:` sets, and it is why the mirror carries a
		// description at all.
		"search_places_text": "Call this tool to search for places via a simple text query using the places api from google.",
	}
	seen := map[string]bool{}
	for _, ref := range body.ToolRefs {
		if expected, ok := want[ref.Tool]; ok {
			seen[ref.Tool] = true
			if ref.Description != expected {
				t.Errorf("%s reference description = %q, want %q", ref.Tool, ref.Description, expected)
			}
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("agent.json carries no reference to hosted tool %q", name)
		}
	}

	for path := range files {
		if strings.HasPrefix(path, "tools/") {
			t.Errorf("the slng target wrote %s for a hosted tool: a body would create a tool the platform already owns", path)
		}
	}
}

// TestHostedToolLowersOnBothCodeTargets: the portability claim. One mirror is
// enough to build a working tool in a generated project, offline.
func TestHostedToolLowersOnBothCodeTargets(t *testing.T) {
	for _, tc := range []struct {
		provider ir.Provider
		module   string
	}{
		{ir.ProviderLiveKit, "agent.py"},
		{ir.ProviderPipecat, "bot.py"},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			files := emitHosted(t, hostedCodeFixture, tc.provider)

			// The module, under the name a Python import can spell. The package
			// writes tools/check_order.slng.py; a dot is not legal in a module
			// name, so the build directory drops the infix.
			mirrored, ok := files["tools/check_order.py"]
			if !ok {
				t.Fatalf("no tools/check_order.py: the mirrored module never reached the project")
			}
			if !strings.HasPrefix(mirrored, "# ruff: noqa") {
				t.Error("the mirrored module lost its header, which is what keeps CI's lint green over another system's code")
			}
			if !strings.Contains(mirrored, "def handler(") {
				t.Error("the mirrored module has no handler, so the emitted call has nothing to reach")
			}

			body := files[tc.module]
			// The call goes through SLNG's own contract, not through a guess at
			// the file's shape. A bare tools.check_order.check_order(...) would
			// be the local: lowering and would work by luck on this one module.
			if !strings.Contains(body, "tools.check_order.handler(") {
				t.Error("the emitted call does not go through the platform's handler contract")
			}
			if !strings.Contains(body, "tools.check_order.Input(") {
				t.Error("the emitted call does not build the platform's Input model")
			}
			// The signature came from the mirror's schema, which the platform
			// introspected. Nothing here parses Python.
			if !strings.Contains(body, "order_number") {
				t.Error("the emitted signature does not carry the parameter the mirror's arg_schema declares")
			}
			if !strings.Contains(body, "Query to search for") {
				t.Error("the request tool's parameter description did not survive from the mirror's schema")
			}

			// And the hole the builder guard does NOT cover, which is worth
			// spelling out because it was found by trying it.
			//
			// TestUnhandledExecutionKindIsRefusedNotPosted holds a kind with no
			// *builder* arm. A kind with a builder arm and no *template* arm is
			// a different hole and the guard cannot see it: the builder returns
			// a perfectly good value, and the template's dispatch, whose final
			// else is webhook_tool, renders an HTTP POST for it.
			//
			// Removing the hosted arm today happens to produce invalid Python,
			// because a hosted code tool carries no URL and the rendered call
			// becomes `client.post(,`. That is luck rather than design: fill in
			// a URL and it would render a working request to the wrong place.
			// So the property is asserted directly.
			method := hostedMethodBody(t, body, "check_order")
			for _, forbidden := range []string{"httpx", "client.post", "os.environ"} {
				if strings.Contains(method, forbidden) {
					t.Errorf("the hosted code tool's emitted body contains %q, so the template dispatch fell through to the webhook arm:\n%s", forbidden, method)
				}
			}
		})
	}
}

// hostedMethodBody is one emitted tool method, from its `def` to the next blank
// line followed by a decorator or another def.
//
// A crude cut on purpose: it only has to be tight enough that an assertion
// about one method does not read another method's body, and both drivers emit
// one tool per method.
func hostedMethodBody(t *testing.T, source, tool string) string {
	t.Helper()
	start := strings.Index(source, "def "+tool+"(")
	if start < 0 {
		t.Fatalf("the emitted module has no %s method", tool)
	}
	rest := source[start:]
	for _, boundary := range []string{"\n    @", "\n@", "\ndef ", "\n    def "} {
		if end := strings.Index(rest, boundary); end > 0 {
			rest = rest[:end]
		}
	}
	return rest
}

// TestHostedRequestToolCarriesNoSecretValue: the emitted call reads its
// credential from the environment by name.
//
// This is the highest-risk line in the whole feature. The pull reads a tool
// that declares secrets and writes into the package, so "secret values appear
// in no package, generated file, or report" needs a gate rather than care.
func TestHostedRequestToolCarriesNoSecretValue(t *testing.T) {
	for _, tc := range []struct {
		provider ir.Provider
		module   string
	}{
		{ir.ProviderLiveKit, "agent.py"},
		{ir.ProviderPipecat, "bot.py"},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			files := emitHosted(t, hostedCodeFixture, tc.provider)
			body := files[tc.module]
			if !strings.Contains(body, `_bearer("SLNG_TOOL_RENDER")`) {
				t.Error("the emitted request does not read its bearer token from the environment by name")
			}
			if !strings.Contains(body, `"https://tools.example.com/tools/search-text"`) {
				t.Error("the emitted request does not post to the url the mirror records")
			}
			// The name has to reach the startup check and the example env too,
			// or the container starts and the first call 401s.
			if env := files[".env.example"]; !strings.Contains(env, "SLNG_TOOL_RENDER") {
				t.Error(".env.example never names the credential the hosted tool reads")
			}
			for path, content := range files {
				if strings.Contains(content, "sk-") || strings.Contains(content, "Bearer ey") {
					t.Errorf("%s carries something shaped like a credential value", path)
				}
			}
		})
	}
}

// TestHostedToolWithDependenciesIsRefusedOnCodeTargets: the honest version of
// "runs everywhere".
//
// The refusal is deliberately the message FieldToolDependencies already gives
// an authored per-tool pin, unchanged. A mirrored dependency and an authored one
// reach nothing on a code target for exactly the same reason, so inventing a
// second sentence for it would be two ways of saying one thing.
func TestHostedToolWithDependenciesIsRefusedOnCodeTargets(t *testing.T) {
	const dependency = "orjson==3.11.4"

	// slng first: it installs a per-tool environment, so this compiles.
	slngAgent := buildHosted(t, hostedSlngFixture)
	withDeps := slngAgent.Tools["check_order"]
	withDeps.Dependencies = []string{dependency}
	slngAgent.Tools["check_order"] = withDeps
	if _, err := Generate(slngAgent, targetByProvider(t, slngAgent, ir.ProviderSlng), target.Default()); err != nil {
		t.Fatalf("slng refused a mirrored dependency, which is the one target that installs them: %v", err)
	}

	codeAgent := buildHosted(t, hostedCodeFixture)
	withDeps = codeAgent.Tools["check_order"]
	withDeps.Dependencies = []string{dependency}
	codeAgent.Tools["check_order"] = withDeps
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		_, err := Generate(codeAgent, targetByProvider(t, codeAgent, provider), target.Default())
		if err == nil {
			t.Fatalf("%s accepted a mirrored dependency it cannot install", provider)
		}
		// The words of the existing row, not a new sentence.
		if !strings.Contains(err.Error(), "reads no per-tool pins") {
			t.Errorf("%s refusal is not the message FieldToolDependencies already gives: %v", provider, err)
		}
		if !strings.Contains(err.Error(), "compile to slng") {
			t.Errorf("%s refusal does not name the target that can install them: %v", provider, err)
		}
	}
}

// TestUnhandledExecutionKindIsRefusedNotPosted is the guard for the failure mode
// that made this feature's Fail Loud story load-bearing, and it is not
// hypothetical on either code target.
//
// LiveKit's emitted tool dispatch ends in `{{else}}{{template "webhook_tool"
// .}}`, and Pipecat's agent method template branches the same way. So a
// lowering that returns a zero value for a kind it does not know does NOT
// render to nothing: it renders an HTTP POST to an environment name nobody
// registered. That is worse than an absent tool, because the emitted project
// imports, starts, answers a call and only fails when the model reaches for the
// tool, by which time the log says KeyError on a variable no reader can trace
// back to a missing template arm.
//
// Both builders therefore refuse the kind instead, and this test holds them to
// it by name.
// It deliberately uses a package with no hosted tool in it, and a kind that is
// not one of the real constants, so it holds the *default* rather than any one
// arm: a kind added to ir without a lowering fails here.
func TestUnhandledExecutionKindIsRefusedNotPosted(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	unknown := agent.Tools["lookup_customer"]
	unknown.Execution = ir.ToolExecution("some_future_kind")
	agent.Tools["lookup_customer"] = unknown

	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
		if err == nil {
			// Not just "it did not error": prove the thing we are afraid of.
			for _, file := range artifact.Files {
				if !strings.HasSuffix(file.Path, ".py") {
					continue
				}
				if strings.Contains(string(file.Content), "lookup_customer") {
					t.Fatalf("%s emitted %s naming lookup_customer for an unlowerable kind: the dispatch fell through, which on this target means an HTTP POST", provider, file.Path)
				}
			}
			t.Fatalf("%s generated an artifact for an execution kind it cannot lower", provider)
		}
		if !strings.Contains(err.Error(), "some_future_kind") {
			t.Errorf("%s refusal does not name the kind it could not lower: %v", provider, err)
		}
		if !strings.Contains(err.Error(), "lookup_customer") {
			t.Errorf("%s refusal does not name the tool: %v", provider, err)
		}
	}

	// The second half, and the one this test exists for. Above, validation
	// refused first, which is the right layering and is what an author meets.
	// But validation is not the thing standing between an unhandled kind and an
	// emitted HTTP POST: the builder is, for an IR built in code or a kind whose
	// validation arm was written and whose template arm was not. So both
	// builders are called directly, with no validation in front of them.
	t.Run("the builders refuse it themselves", func(t *testing.T) {
		env := newEnvSet()
		if _, err := buildLiveKitTool("lookup_customer", unknown, agent.Variables, env); err == nil {
			t.Error("buildLiveKitTool built a tool for a kind it cannot lower, which renders as a webhook POST")
		} else if !strings.Contains(err.Error(), "ir.Validate") {
			t.Errorf("the livekit builder's refusal does not say validation should have caught it: %v", err)
		}
		if _, err := buildTool("lookup_customer", unknown, agent.Variables, env); err == nil {
			t.Error("the pipecat buildTool built a tool for a kind it cannot lower, which renders as a webhook POST")
		} else if !strings.Contains(err.Error(), "ir.Validate") {
			t.Errorf("the pipecat builder's refusal does not say validation should have caught it: %v", err)
		}
	})
}

package generate

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// TestSlngRequirementsCoversEveryKind. The slng_tools fixture is the one package
// that exercises all five: a builtin, an MCP server with two exposed tools, a
// bearer token, and a vault variable in a webhook URL.
func TestSlngRequirementsCoversEveryKind(t *testing.T) {
	artifact, _ := compileSlng(t, "slng_tools")
	requires := artifact.Requires

	if requires.Empty() {
		t.Fatal("a package with a builtin, an MCP server and a credential needs nothing from the account")
	}

	if got := names(requires.Builtins); !slices.Equal(got, []string{"end_call"}) {
		t.Errorf("builtins are %v, want [end_call]", got)
	}
	if got := names(requires.MCPServers); !slices.Equal(got, []string{"internal_docs"}) {
		t.Errorf("MCP servers are %v, want [internal_docs]", got)
	}
	if got := names(requires.MCPTools); !slices.Equal(got, []string{"search_docs", "read_doc"}) {
		t.Errorf("MCP tools are %v, want [search_docs read_doc]", got)
	}
	for _, tool := range requires.MCPTools {
		if tool.Server != "internal_docs" {
			t.Errorf("MCP tool %q names server %q, so a finding could not say which server to look in", tool.Name, tool.Server)
		}
	}
	if got := names(requires.Secrets); !slices.Contains(got, "REFUND_API_TOKEN") {
		t.Errorf("secrets are %v, want the bearer token refund authenticates with", got)
	}

	// Every requirement traces back to a line of the package. A finding without
	// one tells an author to create something and not why.
	for _, requirement := range all(requires) {
		if strings.TrimSpace(requirement.Where) == "" {
			t.Errorf("requirement %q carries no origin, so a finding about it is not actionable", requirement.Name)
		}
	}
}

// TestSlngRequirementsNamesTheToolFileNotTheBuiltinID.
//
// The emitted reference carries the *package tool's* name, so that is the name
// the account has to hold. A package with tools/hang_up.yaml declaring
// `builtin: {id: end_call}` emits {"tool": "hang_up"}, which nothing refuses at
// validate and which the account cannot resolve, because its curated tool is
// called end_call. Checking the builtin id instead would find end_call on the
// account, report everything fine, and let the push fail anyway.
func TestSlngRequirementsNamesTheToolFileNotTheBuiltinID(t *testing.T) {
	artifact, files := compileSlng(t, "slng_tools")
	if len(artifact.Requires.Builtins) != 1 {
		t.Fatalf("expected one builtin, got %v", names(artifact.Requires.Builtins))
	}
	name := artifact.Requires.Builtins[0].Name

	// Whatever is checked has to be what the body actually asks SLNG to resolve.
	body := files["agent.json"]
	if !strings.Contains(body, `"tool": "`+name+`"`) {
		t.Errorf("the requirement checks %q, which is not a name agent.json references", name)
	}
	if !strings.Contains(artifact.Requires.Builtins[0].Where, "tools/"+name+".yaml") {
		t.Errorf("the origin %q does not point at the file whose name is the problem",
			artifact.Requires.Builtins[0].Where)
	}
}

// TestSlngRequirementsExcludesWhatThePushCreates. A code or webhook tool does
// not exist on the account until the push writes it, so reporting one as missing
// would fire on every first deploy. Noise on a first deploy is how a report
// teaches authors to stop reading it.
func TestSlngRequirementsExcludesWhatThePushCreates(t *testing.T) {
	artifact, _ := compileSlng(t, "slng_tools")
	for _, created := range []string{"check_order", "refund"} {
		if slices.Contains(names(artifact.Requires.Builtins), created) {
			t.Errorf("%q is written by the push, so the account is not expected to have it already", created)
		}
	}
}

// TestSlngRequirementsIsWhatTheRunbookPrints is the agreement gate.
//
// The emitted runbook tells an author which vault entries to create, and
// `unmute deploy` checks the same names against the account before it pushes.
// Those are two readers of one fact. If the runbook ever derived its own list,
// a name could be checked and not printed, or printed and not checked, and an
// author following the runbook would still be refused by the push.
func TestSlngRequirementsIsWhatTheRunbookPrints(t *testing.T) {
	artifact, files := compileSlng(t, "slng_tools")
	runbook := files["README.md"]

	printed := 0
	for _, requirement := range append(slices.Clone(artifact.Requires.Secrets), artifact.Requires.Variables...) {
		if !strings.Contains(runbook, requirement.Name) {
			t.Errorf("the preflight checks for %q and the runbook never names it", requirement.Name)
			continue
		}
		if !strings.Contains(runbook, requirement.Where) {
			t.Errorf("the runbook names %q without saying which line asked for it", requirement.Name)
		}
		printed++
	}
	if printed == 0 {
		t.Fatal("this package needs no vault entry, so the gate proved nothing; the fixture has to keep one")
	}

	// And the other direction: the runbook's vault table may not carry a name the
	// preflight would skip, because that is a chore an author would do for
	// nothing.
	checked := append(names(artifact.Requires.Secrets), names(artifact.Requires.Variables)...)
	for _, line := range strings.Split(runbook, "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(strings.Split(line, "|")[1], " `"), "` ")
		if strings.ToUpper(name) == name && name != "" && !slices.Contains(checked, name) {
			t.Errorf("the runbook's vault table names %q, which the preflight does not check", name)
		}
	}
}

func names(requirements []Requirement) []string {
	out := make([]string, 0, len(requirements))
	for _, requirement := range requirements {
		out = append(out, requirement.Name)
	}
	return out
}

func all(r Requirements) []Requirement {
	out := slices.Clone(r.Builtins)
	out = append(out, r.MCPServers...)
	out = append(out, r.MCPTools...)
	out = append(out, r.Hosted...)
	out = append(out, r.Secrets...)
	return append(out, r.Variables...)
}

// TestSlngHostedRequirementsReachTheRunbookAndThePreflight is the same
// agreement gate over the source this feature added, and it exists because
// getting it wrong would have been invisible.
//
// The slng driver reads the package's `secrets:` list nowhere: it derives its
// own vault list from tool authentication blocks and {{$NAME}} tokens. So a
// hosted tool's declared secret written only into `secrets:` would be visible
// in the author's diff, printed by nothing and checked by nothing. This holds
// both readers to the one list.
func TestSlngHostedRequirementsReachTheRunbookAndThePreflight(t *testing.T) {
	artifact, files := compileSlng(t, "slng_hosted")
	runbook := files["README.md"]

	if len(artifact.Requires.Hosted) != 2 {
		t.Fatalf("want two hosted requirements, got %d: %+v", len(artifact.Requires.Hosted), artifact.Requires.Hosted)
	}
	for _, requirement := range artifact.Requires.Hosted {
		if requirement.Version == 0 {
			t.Errorf("hosted requirement %q carries no version, so a drift report has nothing to name", requirement.Name)
		}
		if requirement.ContentHash == "" {
			t.Errorf("hosted requirement %q carries no content hash", requirement.Name)
		}
	}

	// The credential the hosted request tool reads. The platform leaves
	// declared_secrets empty on such a tool and names it in
	// config.auth.secret_name, so this is the assertion that would have failed
	// if the mirror read only the obvious field.
	var found bool
	for _, requirement := range artifact.Requires.Secrets {
		if requirement.Name != "SLNG_TOOL_RENDER" {
			continue
		}
		found = true
		if !strings.Contains(runbook, requirement.Name) {
			t.Errorf("the preflight checks for %q and the runbook never names it", requirement.Name)
		}
		if !strings.Contains(runbook, requirement.Where) {
			t.Errorf("the runbook names %q without saying which line asked for it", requirement.Name)
		}
		if !strings.Contains(requirement.Where, ".slng.json") {
			t.Errorf("the requirement for %q points at %q, which does not name the mirror it came from", requirement.Name, requirement.Where)
		}
	}
	if !found {
		t.Error("a hosted request tool's credential never reached the vault requirements, so the runbook and the preflight both miss it")
	}

	// No value, anywhere. This is the command chain most able to break the rule
	// that a secret value appears in no package, generated file or report.
	for path, content := range files {
		if strings.Contains(content, "sk-") || strings.Contains(content, "Bearer ey") {
			t.Errorf("%s carries something shaped like a credential value", path)
		}
	}
}

// TestSlngMCPServerNameCanDifferFromTheToolName.
//
// A package tool name is lowercase snake_case, checked at build. A platform's
// MCP server name is whatever somebody typed in a dashboard, and real ones carry
// dashes: `firecrawl-mcp` is the common case. Before `mcp.server` existed, the
// emitted reference used the tool's own name, so no legal package could spell
// that server and it was unreachable. This holds the override, and that the
// finding still points at a file that exists.
func TestSlngMCPServerNameCanDifferFromTheToolName(t *testing.T) {
	artifact, files := compileSlng(t, "slng_mcp_server")
	requires := artifact.Requires

	if got := names(requires.MCPServers); !slices.Equal(got, []string{"firecrawl-mcp"}) {
		t.Fatalf("MCP servers are %v, want [firecrawl-mcp]", got)
	}
	// What is checked has to be what the body asks SLNG to resolve.
	if !strings.Contains(files["agent.json"], `"server": "firecrawl-mcp"`) {
		t.Error("the emitted reference does not carry the server name the requirement checks")
	}
	// And the origin has to name a file that exists, which is the whole reason
	// this is a lookup rather than "tools/<server>.yaml".
	for _, requirement := range append(slices.Clone(requires.MCPServers), requires.MCPTools...) {
		if !strings.Contains(requirement.Where, "tools/web_search.yaml") {
			t.Errorf("origin %q does not name the file that asked; tools/firecrawl-mcp.yaml does not exist", requirement.Where)
		}
	}
}

// TestSlngMCPRequirementsCarryNoCodeTargetConnectionSetting.
//
// A hosted MCP server's address, transport and credential are SLNG's. So a
// package that deploys only there writes none of them, and the requirements the
// runbook prints and the deploy preflight checks must not name them either:
// `url_env` and `auth.token_env` on an `mcp:` block belong to livekit and
// pipecat, which dial the server themselves.
//
// This is the half that would go wrong silently. Harvesting every env name on
// the tool would put a code target's environment variable into the list of
// things the SLNG vault has to hold, and an author would create an entry the
// platform reads nowhere.
func TestSlngMCPRequirementsCarryNoCodeTargetConnectionSetting(t *testing.T) {
	agent, resolved := loadSlngRequirementsFixture(t, "slng_mcp_server")
	artifact, err := Generate(agent, resolved, target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// The server and its tools are required: those are names SLNG must hold.
	if len(artifact.Requires.MCPServers) == 0 {
		t.Fatal("the MCP server is not required, so the deploy would not check it exists")
	}
	if len(artifact.Requires.MCPTools) == 0 {
		t.Fatal("no selected MCP tool is required, so the deploy would attach an unchecked selection")
	}
	for _, requirement := range artifact.Requires.MCPTools {
		if requirement.Server == "" {
			t.Errorf("selected tool %q names no server, so a finding could not say which one has to offer it", requirement.Name)
		}
	}

	// And the local connection settings are not. Every env name the mcp block
	// carries is read from the package by the code targets and by nothing here.
	local := map[string]bool{}
	for _, tool := range agent.Tools {
		if tool.URLEnv != "" {
			local[tool.URLEnv] = true
		}
		if tool.Auth != nil && tool.Auth.TokenEnv != "" {
			local[tool.Auth.TokenEnv] = true
		}
	}
	if len(local) == 0 {
		t.Skip("the fixture declares no local MCP connection settings, so this proves nothing")
	}
	for _, requirement := range append(artifact.Requires.Secrets, artifact.Requires.Variables...) {
		if local[requirement.Name] {
			t.Errorf("%s is an MCP connection setting the code targets read, and the slng requirements name it: %s",
				requirement.Name, requirement.Where)
		}
	}
}

// loadSlngRequirementsFixture builds one fixture and returns its slng target.
func loadSlngRequirementsFixture(t *testing.T, fixture string) (*ir.Agent, ir.Target) {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	for name, resolved := range agent.Targets {
		if resolved.Provider == ir.ProviderSlng {
			return agent, resolved
		}
		_ = name
	}
	t.Fatalf("fixture %s declares no slng target", fixture)
	return nil, ir.Target{}
}

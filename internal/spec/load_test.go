package spec

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSafeCore(t *testing.T) { // V3, V14
	pkg, err := Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Agent.EntryAgent != "intake" || len(pkg.Targets) != 2 {
		t.Fatalf("unexpected package: entry=%q targets=%d", pkg.Agent.EntryAgent, len(pkg.Targets))
	}
	if _, ok := pkg.Tools["lookup_customer"]; !ok {
		t.Fatal("tool filename was not used as its name")
	}
	if got := pkg.Connections["primary_phone"].Environment["auth_token"]; got != "TWILIO_AUTH_TOKEN" {
		t.Fatalf("connection auth_token = %q", got)
	}
	if !strings.Contains(pkg.Markdown["instructions.md"], "front desk") {
		t.Fatal("instructions.md was not loaded by path")
	}
}

func TestLoadConnectionsStrictAndPathSafe(t *testing.T) { // telephony V1, V3
	newPackage := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "connections"), 0o700); err != nil {
			t.Fatal(err)
		}
		for name, content := range map[string]string{
			"agent.yaml":   "version: 1\nentry_agent: intake\nmodels: {}\nagents: {}\nchannels: {}\n",
			"targets.yaml": "targets: {}\n",
		} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}

	t.Run("strict fields", func(t *testing.T) {
		dir := newPackage(t)
		path := filepath.Join(dir, "connections", "primary.yaml")
		if err := os.WriteFile(path, []byte("kind: telephony\nprovider: twilio\nenvironment: {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Load(dir)
		if err == nil || !strings.Contains(err.Error(), "connections/primary.yaml") || !strings.Contains(err.Error(), "provider") {
			t.Fatalf("want positioned unknown-field error, got %v", err)
		}
	})

	t.Run("symlink escape", func(t *testing.T) {
		dir := newPackage(t)
		outside := filepath.Join(t.TempDir(), "outside.yaml")
		if err := os.WriteFile(outside, []byte("kind: telephony\nenvironment: {token: TOKEN}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(dir, "connections", "escape.yaml")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "escape.yaml") {
			t.Fatalf("want path escape error, got %v", err)
		}
	})
}

func TestLoadRejectsUnknownFieldWithPosition(t *testing.T) { // V3
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte("version: 1\nentry_agent: intake\nmisspelled: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "agent.yaml") || !strings.Contains(err.Error(), "3:1") {
		t.Fatalf("want filename and line:col, got %v", err)
	}
}

// TestLoadNeverPrintsAValueBack holds the repository's hardest rule at the one
// place that used to break it. The decoder's source excerpt is genuinely the
// best error in the tool — line, neighbours, and a caret under the column — and
// it printed the author's file back at them verbatim.
//
// The mistake that produces a YAML error is very often exactly the mistake of
// writing a value where a name belongs: `secrets:` as a map is the natural
// beginner shape, and it dumped every key in the block to stderr (Wave C,
// 2026-08-15).
func TestLoadNeverPrintsAValueBack(t *testing.T) {
	const pasted = "sk-live-pretendkey123456"
	dir := t.TempDir()
	// A map where a sequence belongs, with a credential-shaped value.
	body := "version: 1\nentry_agent: intake\n\nsecrets:\n  OPENAI_API_KEY: " + pasted + "\n"
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	if err == nil {
		t.Fatal("a map where a sequence belongs must be refused")
	}
	if strings.Contains(err.Error(), pasted) {
		t.Errorf("the refusal prints the value back, into a terminal and a CI log:\n%v", err)
	}
	// The excerpt still has to be worth having: the position, the key, and the
	// structure are what a shape error is about.
	for _, want := range []string{"agent.yaml", "secrets:", "OPENAI_API_KEY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal lost %q, so redaction cost more than it bought:\n%v", want, err)
		}
	}
}

func TestLoadRejectsUnknownTracingField(t *testing.T) { // V25
	dir := t.TempDir()
	yaml := "version: 1\nentry_agent: intake\ntracing:\n  provider: langfuse\n  sample_rate: 1\n"
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "sample_rate") || !strings.Contains(err.Error(), "5:3") {
		t.Fatalf("want unknown tracing field with position, got %v", err)
	}
}

func TestLoadRejectsRetiredPipelineBlock(t *testing.T) { // V3, V22 (N15)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte("version: 1\nentry_agent: intake\npipeline:\n  listen: { placement: api }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "agent.yaml") || !strings.Contains(err.Error(), "pipeline") {
		t.Fatalf("want old pipeline block rejected with position, got %v", err)
	}
}

// TestV24_AgentRefsAreThinkAndSpeakOnly freezes the agent shape (B5): listen
// and turn are per-call plumbing selected at package level, never per agent.
// If AgentDef ever grows such a field, this strict-decode rejection vanishes
// and the B5 decision must be consciously revisited in SCHEMA.md first.
func TestV24_AgentRefsAreThinkAndSpeakOnly(t *testing.T) {
	dir := t.TempDir()
	yaml := "version: 1\nentry_agent: a\nagents:\n  a:\n    instructions: i.md\n    think: m\n    speak: v\n    listen: transcriber\n"
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "agent.yaml") || !strings.Contains(err.Error(), "listen") {
		t.Fatalf("want agent-level listen rejected with position, got %v", err)
	}
}

func TestSchemaIsDerivedFromPackage(t *testing.T) {
	schema, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	if schema.Properties["agent"] == nil || schema.Properties["connections"] == nil || schema.Properties["targets"] == nil {
		t.Fatal("derived authoring schema is missing package files")
	}
	content, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `"tracing"`) {
		t.Fatal("derived authoring schema is missing tracing")
	}
}

// writeToolPackage lays down the smallest package whose only tool is the given
// file body, so a tool-shape error is the only thing Load can report.
func writeToolPackage(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"agent.yaml": "version: 1\nentry_agent: intake\n" +
			"models:\n  think:\n    m: { provider: openai, model: gpt-4o-mini }\n" +
			"  speak:\n    v: { provider: slng, model: \"slng/deepgram/aura:2-en\", voice: aura-2-thalia-en }\n" +
			"agents:\n  intake:\n    instructions: instructions.md\n    think: m\n    speak: v\n    tools: [probe]\n" +
			"tools: [probe]\nchannels:\n  web: { kind: realtime_audio }\n",
		"instructions.md":  "Be brief.\n",
		"targets.yaml":     "targets:\n  livekit:\n    provider: livekit\n    version: \"1.5.2\"\n    sdk_language: python\n",
		"tools/probe.yaml": body,
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestLoadToolShape covers the execution-block shape checks (SCHEMA §5.2): the
// retired flat keys read as migration instructions, an empty or duplicated block
// is named with its line, and every legal single-block form loads.
func TestLoadToolShape(t *testing.T) {
	const head = "description: Probe.\ninput: { type: object }\n"
	for _, tc := range []struct {
		name, body, want string
	}{
		{"flat execution", head + "\nexecution: webhook\nurl_env: PROBE_URL\n", "no longer a top-level field"},
		{"flat url_env", head + "\nwebhook:\n  url_env: PROBE_URL\nurl_env: PROBE_URL\n", "move url_env inside"},
		{"flat token_env", head + "\nwebhook:\n  url_env: PROBE_URL\ntoken_env: PROBE_TOKEN\n", "move token_env under"},
		// read_only retired the other way round from the rest: it did not move
		// inside a block, it moved onto the caller. The refusal has to say that,
		// because "unknown field" sends the author looking for a typo.
		{"retired read_only", head + "\nwebhook:\n  url_env: PROBE_URL\nread_only: true\n", "`writes: true` or `writes: false` on the prefetch entry that runs this one"},
		{"scalar builtin", "description: Probe.\n\nbuiltin: end_call\n", "`builtin:` is a block now"},
		{"no block", head, "no execution block"},
		{"two blocks", head + "\nwebhook:\n  url_env: PROBE_URL\nlocal:\n  handler: tools/probe.py\n", "two execution blocks"},
		{"empty webhook block", head + "\nwebhook:\n", "block is empty"},
		{"bare client block", head + "\nclient:\n", "needs an explicit empty body"},
		// N40: an mcp file is the block and nothing else. Each contract field
		// is named with its own line, so one edit pass removes them all.
		{"mcp with description", "description: Probe.\n\nmcp:\n  url_env: PROBE_MCP_URL\n", "tools/probe.yaml:1: remove `description`"},
		{"mcp with input", head + "\nmcp:\n  url_env: PROBE_MCP_URL\n", "tools/probe.yaml:2: remove `input`"},
		{"mcp with effect", "mcp:\n  url_env: PROBE_MCP_URL\neffect: returns_data\n", "tools/probe.yaml:3: remove `effect`"},
		{"mcp with inject", "mcp:\n  url_env: PROBE_MCP_URL\ninject:\n  caller: \"1\"\n", "tools/probe.yaml:3: remove `inject`"},
		{"mcp with interruption", "mcp:\n  url_env: PROBE_MCP_URL\ninterruption: cancel\n", "tools/probe.yaml:3: remove `interruption`"},
		// A knowledge tool owns both sides of its contract, so input, output,
		// inject and effect have nowhere to go. Unlike mcp, description,
		// announce and interruption stay legal, which the table below proves.
		{"knowledge with input", head + "\nknowledge:\n  base: refunds\n", "tools/probe.yaml:2: remove `input`"},
		{"knowledge with output", "description: Probe.\nknowledge:\n  base: refunds\noutput: { type: object }\n", "tools/probe.yaml:4: remove `output`"},
		{"knowledge with inject", "description: Probe.\nknowledge:\n  base: refunds\ninject:\n  caller: \"1\"\n", "tools/probe.yaml:4: remove `inject`"},
		{"knowledge with effect", "description: Probe.\nknowledge:\n  base: refunds\neffect: returns_data\n", "tools/probe.yaml:4: remove `effect`"},
		{"knowledge beside webhook", "description: Probe.\ninput: { type: object }\nwebhook:\n  url_env: PROBE_URL\nknowledge:\n  base: refunds\n", "two execution blocks"},
		{"empty knowledge block", "description: Probe.\nknowledge:\n", "block is empty"},
		// A hosted tool's definition is the platform's, so a second copy written
		// here could disagree with it and the author would have no way to tell
		// which one the agent used. Only input and output can be written beside
		// the block at all: handler, url_env and auth read as migrations, and
		// base_url, path and dependencies live inside a block the
		// exactly-one-block rule already refuses beside this one.
		{"slng with input", head + "\nslng:\n  hash: abc\n", "tools/probe.yaml:2: remove `input`"},
		{"slng with output", "description: Probe.\nslng:\n  hash: abc\noutput: { type: object }\n", "tools/probe.yaml:4: remove `output`"},
		{"slng beside webhook", "description: Probe.\nslng:\n  hash: abc\nwebhook:\n  url_env: PROBE_URL\n", "two execution blocks"},
		{"slng beside local", "description: Probe.\nslng:\n  hash: abc\nlocal:\n  handler: tools/probe.py\n", "two execution blocks"},
		{"bare slng block", "description: Probe.\nslng:\n", "needs an explicit empty body"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeToolPackage(t, tc.body))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
			if !strings.Contains(err.Error(), "tools/probe.yaml") {
				t.Errorf("error must name the file: %v", err)
			}
		})
	}

	for _, tc := range []struct {
		name, body string
		kind       string
	}{
		{"webhook", head + "\nwebhook:\n  url_env: PROBE_URL\n", "webhook"},
		{"webhook with auth", head + "\nwebhook:\n  url_env: PROBE_URL\n  auth:\n    type: bearer\n    token_env: PROBE_TOKEN\n", "webhook"},
		{"quoted block key", head + "\n\"webhook\":\n  url_env: PROBE_URL\n", "webhook"},
		{"inline builtin block", "description: Probe.\n\nbuiltin: { id: end_call }\n", "builtin"},
		{"explicit empty client", head + "\nclient: {}\n", "client"},
		{"mcp", "mcp:\n  url_env: PROBE_MCP_URL\n", "mcp"},
		{"knowledge", "description: Probe.\nknowledge:\n  base: refunds\n", "knowledge"},
		{"knowledge with announce", "description: Probe.\nannounce: Let me check.\ninterruption: cancel\nknowledge:\n  base: refunds\n", "knowledge"},
		// `slng: {}` is what an author writes before the first pull, so it has
		// to load. ir.Validate is what refuses it, naming the pull, because that
		// is the message the author can act on.
		{"explicit empty slng", "description: Probe.\nslng: {}\n", "slng"},
		{"slng with a pin", "description: Probe.\nslng:\n  hash: abc\n", "slng"},
		{"slng keeps announce and effect", "description: Probe.\nannounce: One moment.\neffect: returns_data\nslng:\n  hash: abc\n", "slng"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg, err := Load(writeToolPackage(t, tc.body))
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if got := pkg.Tools["probe"].ExecutionKind(); got != tc.kind {
				t.Errorf("execution kind = %q, want %q", got, tc.kind)
			}
		})
	}
}

// TestLoadMCPToolSource round-trips the whole `mcp:` block (SCHEMA N40): the
// address, the transport, the auth block, and the tool selection all survive
// the strict decode, and no secret value is ever written.
func TestLoadMCPToolSource(t *testing.T) {
	body := "mcp:\n" +
		"  url_env: FIRECRAWL_MCP_URL\n" +
		"  transport: streamable_http\n" +
		"  auth:\n    type: bearer\n    token_env: FIRECRAWL_API_KEY\n" +
		"  tools:\n    - firecrawl_search\n"
	pkg, err := Load(writeToolPackage(t, body))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	mcp := pkg.Tools["probe"].MCP
	if mcp == nil {
		t.Fatal("the mcp block did not decode")
	}
	if mcp.URLEnv != "FIRECRAWL_MCP_URL" || mcp.Transport != "streamable_http" {
		t.Errorf("url_env/transport = %q/%q", mcp.URLEnv, mcp.Transport)
	}
	if mcp.Auth == nil || mcp.Auth.Type != "bearer" || mcp.Auth.TokenEnv != "FIRECRAWL_API_KEY" {
		t.Errorf("auth = %+v", mcp.Auth)
	}
	if len(mcp.Tools) != 1 || mcp.Tools[0] != "firecrawl_search" {
		t.Errorf("tools = %v", mcp.Tools)
	}
}

// An unknown field inside the block still fails loud, so a typo in one of the
// new field names is never silently dropped (FR-008).
func TestLoadMCPUnknownField(t *testing.T) {
	_, err := Load(writeToolPackage(t, "mcp:\n  url_env: PROBE_MCP_URL\n  transports: sse\n"))
	if err == nil || !strings.Contains(err.Error(), "transports") {
		t.Fatalf("want an unknown-field error naming `transports`, got %v", err)
	}
}

// TestLoadToolAnnounceRefusedOnMCPFile: an mcp file cannot carry a tool
// announcement, because the server owns each of its tools and there is no
// per-tool body here to speak before (N40). It is refused at load, with the
// file, the line, and the reason, like every other contract key.
//
// It has its own function rather than a row in TestLoadToolShape's table so that
// `go test -run Announce` selects it: -run matches the top-level name, and a
// filter that matches nothing still exits 0.
func TestLoadToolAnnounceRefusedOnMCPFile(t *testing.T) {
	_, err := Load(writeToolPackage(t, "mcp:\n  url_env: PROBE_MCP_URL\nannounce: Let me check.\n"))
	if err == nil {
		t.Fatal("an mcp file carrying announce must be refused")
	}
	for _, want := range []string{"tools/probe.yaml:3", "remove `announce`", "the server owns each tool's speech"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must contain %q: %v", want, err)
		}
	}
}

// TestLoadKnowledgeDocuments: the compiler reads document bytes and nothing else.
// A `.png` beside the documents is not an input, so it is neither read nor copied;
// a `.pdf` survives byte-for-byte, which is the whole point of []byte over string.
//
// A missing folder is deliberately NOT an error here. ir.Validate owns that
// message (FR-009) because it can name the base as well as the folder, and Load
// stopping first would put an authoring rule in the wrong package.
func TestLoadKnowledgeDocuments(t *testing.T) {
	dir := t.TempDir()
	binary := []byte("%PDF-1.7\n\x00\x01\x02 binary \xff\xfe trailer\n")
	files := map[string][]byte{
		"agent.yaml": []byte("version: 1\nentry_agent: intake\n" +
			"models:\n  think:\n    m: { provider: openai, model: gpt-4o-mini }\n" +
			"  speak:\n    v: { provider: slng, model: \"slng/deepgram/aura:2-en\", voice: aura-2-thalia-en }\n" +
			"knowledge:\n  refunds:\n    documents: kb/refunds\n  absent:\n    documents: kb/nowhere\n" +
			"agents:\n  intake:\n    instructions: instructions.md\n    think: m\n    speak: v\n    tools: []\n" +
			"channels:\n  web: { kind: realtime_audio }\n"),
		"instructions.md":        []byte("Be brief.\n"),
		"targets.yaml":           []byte("targets:\n  livekit:\n    provider: livekit\n    version: \"1.5.2\"\n    sdk_language: python\n"),
		"kb/refunds/policy.pdf":  binary,
		"kb/refunds/addendum.md": []byte("# Addendum\n"),
		"kb/refunds/logo.png":    []byte("not an input"),
		"kb/refunds/.DS_Store":   []byte("not an input either"),
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pkg, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if pkg.Agent.Knowledge["refunds"].Documents != "kb/refunds" {
		t.Errorf("documents = %q", pkg.Agent.Knowledge["refunds"].Documents)
	}
	if got := pkg.Agent.Knowledge["refunds"].Embed; got != "" {
		t.Errorf("embed = %q, want empty: the default is applied at Build, not Load", got)
	}
	want := map[string]bool{
		"knowledge/refunds/addendum.md": true,
		"knowledge/refunds/policy.pdf":  true,
	}
	for key := range pkg.Documents {
		if !want[key] {
			t.Errorf("read %q, which is not a supported document", key)
		}
	}
	for key := range want {
		if _, ok := pkg.Documents[key]; !ok {
			t.Errorf("did not read %q", key)
		}
	}
	if got := pkg.Documents["knowledge/refunds/policy.pdf"]; !bytes.Equal(got, binary) {
		t.Errorf("pdf bytes changed in transit: got %q", got)
	}
}

// TestLoadRefusesATaskNameDefinedByTwoAgents holds a task name to being one name
// across the whole package. Two agents each defining "verify_customer" leaves no
// way to tell which one owns it, so flattenTasks refuses the second definition
// it reaches and the message names both agents, not only the one it stopped on.
func TestLoadRefusesATaskNameDefinedByTwoAgents(t *testing.T) {
	dir := t.TempDir()
	yaml := "version: 1\nentry_agent: front_desk\n" +
		"agents:\n" +
		"  front_desk:\n    instructions: instructions.md\n    think: m\n    speak: v\n" +
		"    tasks:\n      - name: verify_customer\n        when: The caller has not been identified.\n        instructions: instructions.md\n" +
		"  back_office:\n    instructions: instructions.md\n    think: m\n    speak: v\n" +
		"    tasks:\n      - name: verify_customer\n        when: A back-office check is needed.\n        instructions: instructions.md\n"
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "targets.yaml"), []byte("targets: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	want := `task "verify_customer" is defined by agent "back_office" and again by agent "front_desk". ` +
		`A task name is one name across the package: keep one definition and let the other agent name it, "- verify_customer"`
	if err == nil || !strings.Contains(err.Error(), "agent.yaml:9") || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain \"agent.yaml:9\" and %q", err, want)
	}
}

// TestLoadRejectsRetiredFieldSpellings holds the strict decoder to the renamed
// surface: the retired top-level delegates: catalog and the retired per-agent
// model:/voice: fields are all unknown fields now, and each is refused with its
// own file, line and column, the same way any other unknown field is.
func TestLoadRejectsRetiredFieldSpellings(t *testing.T) {
	for _, tc := range []struct {
		name, yaml, field, position string
	}{
		{
			name:     "top-level delegates",
			yaml:     "version: 1\nentry_agent: intake\ndelegates: {}\n",
			field:    "delegates",
			position: "3:1",
		},
		{
			name:     "agent-level model",
			yaml:     "version: 1\nentry_agent: intake\nagents:\n  intake:\n    instructions: i.md\n    model: m\n",
			field:    "model",
			position: "6:5",
		},
		{
			name:     "agent-level voice",
			yaml:     "version: 1\nentry_agent: intake\nagents:\n  intake:\n    instructions: i.md\n    voice: v\n",
			field:    "voice",
			position: "6:5",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(tc.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(dir)
			want := "agent.yaml: [" + tc.position + `] unknown field "` + tc.field + `"`
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("error = %v, want to contain %q", err, want)
			}
		})
	}
}

// TestLoadTaskItemAcceptsBareNameAndMapping proves TaskItem.UnmarshalYAML runs
// through the real decode path rather than only in isolation: a bare string
// under an agent's tasks: is a reference to a task another agent defines, and a
// mapping defines the task right there.
func TestLoadTaskItemAcceptsBareNameAndMapping(t *testing.T) {
	dir := t.TempDir()
	yaml := "version: 1\nentry_agent: intake\n" +
		"agents:\n  intake:\n    instructions: instructions.md\n    think: m\n    speak: v\n" +
		"    tasks:\n      - look_up_order\n      - name: verify_customer\n        when: The caller has not been identified.\n        instructions: instructions.md\n"
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "targets.yaml"), []byte("targets: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "instructions.md"), []byte("Be brief.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	tasks := pkg.Agent.Agents["intake"].Tasks
	if len(tasks) != 2 {
		t.Fatalf("got %d task items, want 2: %+v", len(tasks), tasks)
	}
	if tasks[0].Ref != "look_up_order" || tasks[0].Task != nil {
		t.Errorf("bare item = %+v, want Ref %q and no Task", tasks[0], "look_up_order")
	}
	if tasks[1].Ref != "" || tasks[1].Task == nil || tasks[1].Task.Name != "verify_customer" {
		t.Errorf("mapping item = %+v, want Task.Name %q and no Ref", tasks[1], "verify_customer")
	}
}

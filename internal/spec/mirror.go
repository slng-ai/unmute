package spec

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Mirror is one SLNG-hosted tool's definition, as the platform published it.
//
// It is not authored. `unmute pull` fetches it, writes it beside the tool file
// as tools/<name>.slng.json, and every later compile reads that committed copy
// with no network. The same struct is the decode target for
// `voiceai tool get --json` and the shape of the file on disk, so there is one
// owner for the field names and no second place for them to drift.
//
// The fields the platform returns and this deliberately does not keep are
// `gate_status`, `is_current_hash_green`, `is_current_version`, `schema_stale`,
// `last_run_status`, `argument_defaults` and `organisation_id`. Each describes
// the platform's opinion of the tool at one moment rather than the tool, and
// recording a moment in a committed file makes a diff that means nothing.
type Mirror struct {
	Name        string `json:"name"`
	ToolType    string `json:"tool_type"`
	Source      string `json:"source"`
	Description string `json:"description,omitempty"`

	// ArgSchema is the tool's parameters, which the platform derived by
	// introspecting the tool itself. It becomes ir.Tool.Input, which is what
	// both code drivers build a signature from, so nothing here parses Python.
	ArgSchema map[string]any `json:"arg_schema,omitempty"`

	// Dependencies are the platform's canonical `name==version` pins. They reach
	// ir.Tool.Dependencies and inherit the verdicts that field already has:
	// allowed on slng, which installs a per-tool environment, and refused on the
	// code targets, which build one project dependency list and read no per-tool
	// pins.
	Dependencies []string `json:"dependencies,omitempty"`

	// DeclaredSecrets are names, never values, and are empty more often than
	// they should be: an api_request tool names its credential in
	// Config["auth"]["secret_name"] and leaves this list empty. Secrets() reads
	// both, which is why nothing should read this field directly.
	DeclaredSecrets []string `json:"declared_secrets,omitempty"`

	// Config is the platform's own configuration block, kept as it arrived.
	// Untyped on purpose: it is provider passthrough, and a typed copy would
	// silently drop a field the platform adds later. Request() gives the typed
	// view a code target needs and names anything it could not map, so an
	// unmapped field becomes a refusal instead of a default.
	Config map[string]any `json:"config,omitempty"`

	// Code is the mirrored module, for a `code` tool. Held here when the mirror
	// is fetched and cleared before the sidecar is written, because the module
	// is committed as its own .py file: a diff of an escaped one-line JSON
	// string is not reviewable, and reviewability is why the mirror is committed
	// at all.
	Code string `json:"code_src,omitempty"`

	// ContentHash is SLNG's own hash, whose algorithm is not ours. Recorded and
	// compared, never recomputed. It answers "does the organisation still hold
	// what was fetched", which is a different question from the pin in the tool
	// file, and needs the network.
	ContentHash string `json:"content_hash,omitempty"`
	Version     int    `json:"latest_version,omitempty"`

	// Fetched is the date the pull ran, so a reader can see how old the mirror
	// is without asking the platform.
	Fetched string `json:"fetched,omitempty"`
}

// Secrets returns every credential name this tool reads, from both places the
// platform puts them, sorted and deduplicated. Names only: a value never
// reaches a package file, a generated file, a report or an output stream.
func (m Mirror) Secrets() []string {
	seen := map[string]bool{}
	for _, name := range m.DeclaredSecrets {
		if name != "" {
			seen[name] = true
		}
	}
	// The one an api_request tool actually uses. Captured live: a tool with a
	// bearer token had declared_secrets [] and config.auth.secret_name set.
	if auth, ok := m.Config["auth"].(map[string]any); ok {
		if name, ok := auth["secret_name"].(string); ok && name != "" {
			seen[name] = true
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// MirrorRequest is the part of an api_request tool's configuration a code
// target can actually emit.
type MirrorRequest struct {
	URL     string
	Method  string
	Headers []MirrorHeader
	// SecretName is the environment variable the emitted call reads its bearer
	// token from. A name, never a value.
	SecretName string
	AuthType   string
}

// MirrorHeader is one fixed request header the platform stores.
type MirrorHeader struct {
	Name  string
	Value string
}

// mappedRequestKeys are the api_request configuration keys a code lowering
// accounts for, each with why it needs no emitted equivalent. A key outside
// this set is refused rather than ignored, because a silently dropped field is
// a tool that behaves differently on two targets and says nothing about it.
var mappedRequestKeys = map[string]string{
	"type":              "names the configuration union arm, which the tool_type already said",
	"url":               "emitted",
	"http_method":       "emitted",
	"auth":              "emitted as an environment read, by name",
	"headers":           "emitted",
	"parameters":        "the same schema as arg_schema, which is where the signature comes from",
	"strict":            "constrains the schema the platform advertises to its own model; a code target emits a typed signature instead",
	"webhook_format":    "checked: only `raw` has a code-target shape",
	"timeout_seconds":   "no package field carries a tool timeout on any target, so nothing authored is being dropped",
	"wait_for_response": "checked: a code target awaits every tool call it emits",
	"response":          "checked: `show_to_llm` false has no code-target shape",
	"egress":            "the platform's own network policy for a sandboxed tool, which does not apply to a tool running inside a generated project",
	"import_probes":     "the platform's own static-analysis record",
}

// Request returns the typed view of an api_request tool's configuration, plus
// every reason a code target cannot express it. Both are returned together so
// one call answers "can this be lowered" and "with what".
func (m Mirror) Request() (MirrorRequest, []string) {
	var req MirrorRequest
	var refusals []string

	var keys []string
	for key := range m.Config {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, mapped := mappedRequestKeys[key]; !mapped {
			refusals = append(refusals, fmt.Sprintf("the mirrored tool sets %s, which no code target maps", key))
		}
	}

	req.URL, _ = m.Config["url"].(string)
	req.Method, _ = m.Config["http_method"].(string)
	if req.Method == "" {
		req.Method = "POST"
	}
	if auth, ok := m.Config["auth"].(map[string]any); ok {
		req.AuthType, _ = auth["type"].(string)
		req.SecretName, _ = auth["secret_name"].(string)
		if req.AuthType != "" && req.AuthType != "bearer" {
			refusals = append(refusals, fmt.Sprintf("the mirrored tool authenticates with %q, and a code target emits a bearer header only", req.AuthType))
		}
	}
	for _, raw := range asSlice(m.Config["headers"]) {
		header, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := header["name"].(string)
		value, _ := header["value"].(string)
		req.Headers = append(req.Headers, MirrorHeader{Name: name, Value: value})
	}

	if format, ok := m.Config["webhook_format"].(string); ok && format != "" && format != "raw" {
		refusals = append(refusals, fmt.Sprintf("the mirrored tool sets webhook_format %q, and a code target sends the call arguments as the request body", format))
	}
	if wait, ok := m.Config["wait_for_response"].(bool); ok && !wait {
		refusals = append(refusals, "the mirrored tool sets wait_for_response false, and no code driver has a fire-and-forget tool shape: every tool call it emits awaits its result")
	}
	if response, ok := m.Config["response"].(map[string]any); ok {
		if show, ok := response["show_to_llm"].(bool); ok && !show {
			refusals = append(refusals, "the mirrored tool sets response.show_to_llm false, and a code target returns every tool result to the model")
		}
	}
	if req.URL == "" {
		refusals = append(refusals, "the mirrored tool records no url, so there is nothing for a code target to call")
	}
	return req, refusals
}

// asSlice reads a JSON array out of an untyped value, tolerating absence.
func asSlice(value any) []any {
	list, _ := value.([]any)
	return list
}

// MirrorJSON is the sidecar's bytes: the mirror without its module, indented so
// a diff is readable, which is the reason the mirror is committed at all.
func (m Mirror) MirrorJSON() ([]byte, error) {
	m.Code = ""
	content, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("mirror %s: %w", m.Name, err)
	}
	return append(content, '\n'), nil
}

// MirrorHeaderLines is the header `unmute pull` writes above a mirrored module.
//
// The first line is load-bearing and measured rather than defensive: the
// platform's own code fails this repository's lint. The hosted check_order
// module reports E402 at the ruff version CI pins, and the offending import
// sits inside a shim unmute itself generated on an earlier push. CI lints every
// tracked .py file, so without this the first pull turns the build red.
//
// It is the only lint suppression in this tree, and it says why in its own
// second and third lines, which is more honest than a configuration file that
// lowers the bar for every other file at the same time.
const MirrorHeaderLines = `# ruff: noqa
# Mirrored from SLNG. Not first-party source: the platform gated this
# module and owns it. Edit it in the SLNG dashboard, not here.
`

// MirrorPaths are the two files a pull writes beside tools/<name>.yaml. The
// `.slng.` infix is what lets one glance answer "may I edit this", and the
// answer is no.
func MirrorPaths(name string) (sidecar, module string) {
	return "tools/" + name + ".slng.json", "tools/" + name + ".slng.py"
}

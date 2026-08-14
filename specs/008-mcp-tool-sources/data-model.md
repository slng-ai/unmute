# Data Model: MCP servers as tool sources

Feature: `008-mcp-tool-sources` · Date: 2026-08-13

Two structs change, one in each schema surface. Go structs are the schema
source (constitution III), so this file describes the structs and the derived
authoring shape follows from them. No hand-written `.json` schema anywhere.

## 1. Authoring surface: `internal/spec`

### 1.1 `ToolMCP` (grows; today it holds `URLEnv` only, `internal/spec/package.go:242`)

```go
// ToolMCP is the `mcp:` block: one remote MCP server used as a tool source.
// The server owns each tool's name, description, and parameters; the file
// declares how to reach it and which tools to expose.
type ToolMCP struct {
    // URLEnv names the env var holding the server address. Required.
    URLEnv string `json:"url_env" yaml:"url_env"`
    // Transport is `sse` or `streamable_http`. Optional; empty means the
    // platform default for the URL.
    Transport string `json:"transport,omitempty" yaml:"transport,omitempty"`
    // Auth reuses the webhook auth shape (bearer, api_key). Optional.
    Auth *ToolAuth `json:"auth,omitempty" yaml:"auth,omitempty"`
    // Tools selects server tool names to expose. Optional; empty means all.
    Tools []string `json:"tools,omitempty" yaml:"tools,omitempty"`
}
```

`ToolAuth` is reused as is (`type`, `token_env`, `header`); no new auth type.

### 1.2 Authoring shape (derived)

```yaml
# tools/web_search.yaml — the file name is the tool-source name
mcp:
  url_env: FIRECRAWL_MCP_URL
  transport: streamable_http   # optional: sse | streamable_http
  auth:                        # optional
    type: bearer
    token_env: FIRECRAWL_API_KEY
  tools:                       # optional; absent = all server tools
    - firecrawl_search
```

### 1.3 Top-level contract rule (the shape change, SCHEMA amendment N40)

An `mcp` tool file is the `mcp:` block and nothing else. Every top-level field
that describes a single tool (`description`, `input`, `output`, `inject`,
`interruption`, `effect`) is illegal on an `mcp` file, because the server owns
the per-tool contract and no driver ever read those fields on an MCP tool.
Declared anyway, they fail validation by name (fail loud, never silent).

Old-shape files (with `description` + `input`) therefore fail. N40 records the
break. The scaffold template (`internal/scaffold/templates/tool.yaml.tmpl`)
gains an mcp branch that writes the block only, like the `builtin` branch.

## 2. Resolved surface: `internal/ir`

### 2.1 `ir.Tool` (flat struct, `internal/ir/compiler.go:258`)

MCP today reuses `URLEnv`. Two fields are added, `Auth` is reused:

```go
type Tool struct {
    // ...existing fields...
    URLEnv       string        // webhook base URL env, or MCP server env
    Auth         *ToolAuth     // webhook or mcp auth
    MCPTransport string        // "", "sse", "streamable_http"
    MCPTools     []string      // selected server tool names; empty = all
}
```

`ir.Build` lowering (`internal/ir/build.go:492`): the mcp arm copies
`URLEnv`, `Transport`, `Auth` (through the existing `buildToolAuth`, which
applies the `X-API-Key` default), and `Tools` verbatim.

### 2.2 Validation rules (`internal/ir/validate.go`)

| Rule | Behaviour |
|---|---|
| `url_env` required, UPPER_SNAKE | exists today (`:399-401`), unchanged |
| `transport` ∈ {`sse`, `streamable_http`} | new; anything else is an error naming the two values |
| `auth` legal on webhook **and** mcp | today `:350-356` rejects auth off webhook; the rule widens, and `validateToolAuth` (`:840`) runs for mcp too (scheme names, UPPER_SNAKE `token_env`) |
| `tools` entries non-empty strings, no duplicates | new; names are **not** checked against the live server (they exist only at run time; runbook says so) |
| top-level contract fields illegal on mcp | new, enforced **at load** in `internal/spec/tool_shape.go` (`checkToolShape` scans top-level keys and knows the block kind), so the error carries file and line per FR-007/FR-008; the validator's `description is required` path (`:366-371`) gains an mcp skip |
| `inject` illegal on mcp | exists today (`variables.go:127-141`), unchanged; the stale comment at `spec/package.go:213` is corrected |
| env reference sites | `mcp.url_env` site exists (`:1254`); a new site string `tools/<name>.yaml mcp.auth.token_env` joins it so the compile report shows both |

### 2.3 Capability table (`internal/target/table.go`)

- `FieldToolMCP` row (`:330`): the `deny(Pipecat, ...)` line is removed.
  Deepgram's deny stays. Vapi stays Core (validation-only target).
- The LiveKit `sdk_language: python` condition (`:430`) stays as is.
- Agreement tests move in the same commit: `pipecatEmittedFields` gains
  `FieldToolMCP: true` (`internal/generate/pipecat_v1.go:414`), and the pinned
  message test `internal/ir/validate_test.go:727` is updated.

## 3. Entities → spec mapping

| Spec entity | Where it lives |
|---|---|
| MCP tool source | one `tools/<name>.yaml` with an `mcp:` block; name = file name |
| Transport | `ToolMCP.Transport` → `ir.Tool.MCPTransport` |
| Authentication block | `ToolMCP.Auth` (shared `ToolAuth`) → `ir.Tool.Auth` |
| Tool selection | `ToolMCP.Tools` → `ir.Tool.MCPTools` |
| Assignment | unchanged: `agents.<n>.tools` and task `tools` name lists (`ir.AgentDef.Tools`, `ir.Task.Tools`) |

## 4. State and lifecycle

None. Tool sources have no state in the compiler; server connection lifecycle
belongs to the generated project and follows each platform's SDK (researched
in `research.md`). Two files naming the same `url_env` are two independent
sources: the LiveKit driver's current collapse-by-env
(`livekit_v1_build.go:713`) is replaced by one mount per source, because the
selection now lives on the source, not on the file-per-server-tool convention
it used to encode.

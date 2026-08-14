# Research: MCP servers as tool sources

Feature: `008-mcp-tool-sources` · Date: 2026-08-13

Sources used, per constitution IV (verified against official documentation or
upstream source, dated):

- LiveKit MCP docs, rendered 2026-08-13: `docs.livekit.io/agents/logic/tools/mcp.md`
- Pipecat `MCPClient` docs, fetched 2026-08-13: `docs.pipecat.ai` (MCPClient page)
- livekit-agents reference checkout at `livekit-agents@1.6.4` (`~/Documents/GitHub/livekit_agent`)
- pipecat reference checkout at `v1.2.1+232` (`~/Documents/GitHub/pipecat_agent`) — **stale, no 1.5.x tags; caveat in R3**
- Firecrawl MCP docs, fetched 2026-08-13: `docs.firecrawl.dev/mcp-server/tools`

## R1 — LiveKit emission shape: `MCPToolset` in `tools=[...]`, not `mcp_servers=`

**Decision**: The LiveKit driver stops emitting `mcp_servers=[mcp.MCPServerHTTP(...)]`
and emits toolset entries inside the agent's or task's `tools` surface:

```python
tools=[mcp.MCPToolset(id="web_search", mcp_server=mcp.MCPServerHTTP(
    url=os.environ["FIRECRAWL_MCP_URL"],
    allowed_tools=["firecrawl_search"],   # omitted when no selection
    headers=_bearer("FIRECRAWL_API_KEY"), # omitted when no auth
    # transport_type="sse" | "streamable_http" — only when the author stated it
))]
```

**Rationale**: `mcp_servers` on `Agent`/`AgentSession` is deprecated and logs a
warning on every start since livekit-agents 1.5.11 (checkout:
`voice/agent.py:120-124`, `voice/agent_session.py:434-439`). `MCPToolset`
(`llm/mcp.py:434`, keyword-only `id` + `mcp_server`) exists since 1.5.1 and is
the documented path. `MCPServerHTTP.__init__` (`llm/mcp.py:248`) carries
exactly the knobs the schema grows: `transport_type`, `allowed_tools`,
`headers`. The toolset `id` is the tool-source name (file name), which the
docs recommend keeping stable across sessions.
**Alternatives considered**: keep `mcp_servers` (rejected: a generated project
that warns on every start is a silent quality downgrade, and the param is
scheduled for removal); emit `MCPToolset` with per-tool `tool_options`
(rejected: no such parameter exists in the SDK — the pasted docs mention it,
the 1.6.4 source does not, so it stays out until the source shows it).

## R2 — LiveKit dependency: add the `mcp` extra, floor `>=1.6` when MCP is used

**Decision**: When a package uses an MCP tool source, the generated pyproject
dependency becomes `livekit-agents[mcp,<other extras>]>=1.6` (the warm-transfer
special case `>=1.6,<1.7` already exists and composes).

**Rationale**: `mcp` is an optional extra (`pyproject.toml:64`,
`mcp = ["mcp>=1.24.0, <2"]`), not a core dependency; `llm/mcp.py:30-34` raises
ImportError without it. **The current driver never adds the extra, so today's
MCP emission fails at import in a generated project — an existing defect this
feature fixes.** The `>=1.6` floor is where every emitted parameter
(`transport_type`, `allowed_tools`, `headers`) is verified in source (checkout
1.6.4); claiming 1.5.x compatibility per parameter is not verifiable from what
we hold, so we do not claim it.
**Alternatives considered**: keep `>=1.5` (rejected: unverifiable parameter
surface); pin exact (rejected: the LiveKit driver deliberately emits floors,
`livekit_v1_build.go:1289-1347`).

## R3 — Pipecat emission shape: persistent `MCPClient`, registered with the LLM

**Decision**: The Pipecat driver emits one `MCPClient` per tool source:

```python
web_search_mcp = MCPClient(
    server_params=StreamableHttpParameters(
        url=os.environ["FIRECRAWL_MCP_URL"],
        headers=_bearer("FIRECRAWL_API_KEY"),
    ),
    tools_filter=["firecrawl_search"],    # omitted when no selection
)
await web_search_mcp.start()
mcp_tools = await web_search_mcp.register_tools(llm)
# agent-scope: mcp_tools.standard_tools join the LLMContext tools
# task-scope: they join that flow node's functions instead
await web_search_mcp.close()              # on pipeline shutdown
```

**Rationale**: `MCPClient` (`pipecat/services/mcp_service.py:54`) takes
`server_params` (`StdioServerParameters | SseServerParameters |
StreamableHttpParameters`), `tools_filter`, and supports `start()`/`close()`
alongside the context-manager form; the generated bot owns the lifecycle, so
explicit start on setup and close on shutdown fits the existing bot structure
better than wrapping the whole pipeline in `async with`. `register_tools(llm)`
returns a `ToolsSchema` whose `standard_tools` merge into the context (docs
show exactly this merge for multiple servers). Scoping: agent-level tools ride
`LLMContext(tools=...)`; task-level tools join the flow node's function list,
which is how the driver already scopes per-task webhook tools.
**Verification caveat**: the reference checkout has no 1.5.x tags; every API
above exists identically at `v1.2.1` (diff to fork HEAD is one line) and in
the official docs fetched today, so the surface is stable across the range —
but the L4 smoke run against pinned `pipecat-ai==1.5.0` is the proof gate, and
implementation must not skip it.
**Alternatives considered**: `async with` context manager per call (rejected:
re-connects per session and fights the bot's lifecycle);
`get_tools_schema()` + `register_tools_schema()` two-step (not needed: the
driver's LLM services exist before context creation; adopt only if a
construction-time-tools LLM enters the catalogue).

## R4 — Pipecat dependency: `mcp` extra on the existing exact pin

**Decision**: `pipecat-ai[mcp,<other extras>]==<target version>` (the example
pins `1.5.0`). The extra resolves to `mcp[cli]>=1.11.0,<2`
(`pyproject.toml:89`).

## R5 — Transport default: one rule on both targets, `/mcp` means streamable HTTP

**Decision**: When the author states `transport:`, both drivers emit exactly
that. When absent: LiveKit omits `transport_type` and the SDK auto-detects
(streamable HTTP only when the URL path ends `/mcp`, else SSE —
`llm/mcp.py:312-321`). Pipecat has no auto-detect, and the URL is an env value
unknown at compile time, so the emitted bot chooses at startup with the same
rule LiveKit uses:

```python
# ponytail: mirrors livekit-agents' auto-detect so one URL behaves the same on both targets
params_cls = StreamableHttpParameters if url.rstrip("/").endswith("/mcp") else SseServerParameters
```

**Rationale**: keeps FR-002's "platform default" story coherent: one sentence
("ends in /mcp → streamable HTTP, otherwise SSE, unless you say otherwise")
is true on both targets.
**Alternatives considered**: require `transport` on pipecat targets (rejected:
the common case would fail portably for no reason); default streamable HTTP
everywhere (rejected: silently wrong for `/sse` URLs).

## R6 — Auth lowering: reuse the existing `_bearer` / `_api_key` helpers verbatim

**Decision**: Both templates already emit `_bearer(env) -> dict` and
`_api_key(header, env) -> dict` when any tool declares auth
(`agent.py.tmpl:195-211`, `bot.py.tmpl:420-431`). MCP auth feeds the same
`AuthKinds` set and passes the same lowered expression as the `headers=`
argument. Zero new auth code, and secrets stay env-read-at-call-time.

## R7 — Selection semantics replace the collapse-by-env convention

**Decision**: One tool source = one server mount. The LiveKit driver's current
collapse of mcp tools sharing a `url_env` into one `MCPServerHTTP` with
`allowed_tools=[<file names>]` (`livekit_v1_build.go:713-800`,
`livekitMCPServers` at `:804`) is removed. `allowed_tools` /`tools_filter` now
carry the file's `tools:` selection, or are omitted entirely when the author
selected nothing (= all tools). Two files naming the same env are two mounts.

**Rationale**: the old convention quietly required each mcp yaml's file name
to equal a server-side tool name — undocumented, unverifiable, and the reason
the old shape needed `description`/`input` it never used. The new shape is
what the spec promises (FR-001/FR-004).

## R8 — Firecrawl example facts (verified 2026-08-13)

- Server: `https://mcp.firecrawl.dev/v2/mcp` (streamable HTTP; URL ends `/mcp`,
  so both targets' default transport rule picks streamable HTTP with no
  `transport:` line — the example still writes it explicitly to teach the field).
- Auth: `Authorization: Bearer <FIRECRAWL_API_KEY>`; key already present in the
  root `.env`. `FIRECRAWL_MCP_URL` is added to root `.env` and `.env.example`.
- Search tool name: `firecrawl_search` (keyless tier also exposes
  `firecrawl_scrape`, `firecrawl_parse`; the selection hides them).
- Example targets: livekit `1.6.4` (matches R2's floor), pipecat `1.5.0`
  (matches every other example).

## R9 — Amendment number and doc surface

Next free amendment is **N40** (N39, 2026-08-13, is the current highest).
Pages that state MCP facts today and change in the same commit:
`docs/SCHEMA.md` (§5.1, §5.2 mcp row, §6.1 `sdk_language` note, §7 skip list
and matrix line 779, Pipecat gate inventory line 832), `docs/user/learn/02-add-a-tool.md`,
`docs/user/targets/livekit.md` ("Use an MCP tool"), `docs/user/targets/pipecat.md`,
`docs/user/reference/tools.md`, `docs/user/reference/safe-core.md`,
`docs/user/reference/secrets.md`, both emitted README templates, and the new
example's README (three-places rule).

## R10 — TUI stays minimal but stops emitting an illegal file

The console's mcp flow (kind picker + one `url_env` prompt,
`internal/tui/tui.go:931-1063`) keeps its single prompt: `transport`, `auth`,
and `tools` are optional, so the minimal file it writes stays valid. What must
change: the scaffold template's generic branch writes `description` + `input`
+ `interruption` + `effect` into every non-builtin tool file
(`internal/scaffold/templates/tool.yaml.tmpl`), which becomes illegal on mcp —
it gains an mcp branch that writes the block only. The picker's known
`sdk_language` blind spot (`toolExecutionGate` uses `Capability`, not
`CapabilityForValue`, `tui.go:936-943`) predates this feature and matters less
once Pipecat is allowed; noted, not fixed here.

## R11 — Pipecat cannot scope an MCP source to a task (found during implementation, 2026-08-14)

**Finding**: R3 assumed task-scoped MCP tools could join a Flows node's
function list. They cannot. `FlowManager._set_node` accepts only
`FlowsFunctionSchema` objects and callables and raises `InvalidFunctionError`
on anything else (`pipecat/flows/manager.py:660-671`, pipecat-ai 1.5.0), and a
`FlowsFunctionSchema` requires a `handler`. `MCPClient`'s only executor is the
private `_tool_wrapper`, registered on an LLM service by `register_tools`; the
public surface is `start`, `close`, `register_tools`, `get_tools_schema`,
`register_tools_schema`. There is no supported way to build a node function
that runs an MCP tool.

**Decision**: emit agent scope on Pipecat, and deny task scope by name through
a new capability field `tasks.tools.execution.mcp` (`FieldToolMCPTask`,
`deny(Pipecat, ...)`). The Pipecat builder also fails loud if an IR built in
code reaches it, so the webhook fallback can never catch an mcp source. LiveKit
keeps both scopes (`AgentTask` takes the same `tools=` surface).

**Alternatives considered**: wrap `MCPClient._tool_wrapper` in a
`FlowsFunctionSchema` handler (rejected: generated code against a private API
that pipecat may rename, and the constitution's "verified against official
documentation" rule has nothing to verify it with); require a second MCP
connection per node (rejected: same private-API problem, plus a reconnect per
node). Revisit if pipecat exposes a per-tool call or accepts a plain
`FunctionSchema` in a node's `functions`.

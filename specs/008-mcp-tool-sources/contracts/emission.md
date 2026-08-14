# Contract: what each driver emits for an MCP tool source

Feature: `008-mcp-tool-sources` · Grounded in [research.md](../research.md)
R1-R7. These are the shapes the golden files pin (L3) and the smoke layer
instantiates (L4). Source tool file for the examples below:

```yaml
# tools/web_search.yaml
mcp:
  url_env: FIRECRAWL_MCP_URL
  transport: streamable_http
  auth: {type: bearer, token_env: FIRECRAWL_API_KEY}
  tools: [firecrawl_search]
```

## LiveKit (`agent.py`)

Inside the owning agent's or task's `super().__init__(...)`, on its `tools`
surface, one entry per source:

```python
mcp.MCPToolset(
    id="web_search",
    mcp_server=mcp.MCPServerHTTP(
        url=os.environ["FIRECRAWL_MCP_URL"],
        transport_type="streamable_http",
        allowed_tools=["firecrawl_search"],
        headers=_bearer("FIRECRAWL_API_KEY"),
    ),
)
```

Omission rules (never emit a lie):

| Source field absent | Emitted call |
|---|---|
| `transport` | no `transport_type` argument (SDK auto-detects: path ends `/mcp` → streamable HTTP, else SSE) |
| `tools` | no `allowed_tools` argument (all server tools) |
| `auth` | no `headers` argument |

Also: `from livekit.agents import mcp` import gated on use; dependency becomes
`livekit-agents[mcp,...]>=1.6`; the deprecated `mcp_servers=` parameter never
appears; `id` is the tool-source name.

## Pipecat (`bot.py`)

One client per source, started during setup, closed on shutdown:

```python
web_search_mcp = MCPClient(
    server_params=StreamableHttpParameters(
        url=os.environ["FIRECRAWL_MCP_URL"],
        headers=_bearer("FIRECRAWL_API_KEY"),
    ),
    tools_filter=["firecrawl_search"],
)
await web_search_mcp.start()
web_search_tools = await web_search_mcp.register_tools(llm)
```

- Agent scope: `web_search_tools.standard_tools` join the `LLMContext` tools
  (inline shape), or the owning `LLMWorker`'s `build_tools()` (bus shape), so a
  source is advertised only while its agent is active.
- Task scope: **not emitted, and refused by name.** A Pipecat Flows node builds
  its advertised tool set from `FlowsFunctionSchema` objects (or direct
  functions) alone — `manager.py` raises `InvalidFunctionError` on anything
  else — and `MCPClient` exposes no public per-tool handler to put in one
  (`_tool_wrapper` is private; verified against pipecat-ai 1.5.0, 2026-08-14).
  So `FieldToolMCPTask` denies Pipecat and the message says to list the source
  on the agent. LiveKit keeps both scopes.
- `transport` absent: the bot picks the params class at startup with the same
  rule LiveKit auto-detects by (`url.rstrip("/").endswith("/mcp")` →
  `StreamableHttpParameters`, else `SseServerParameters`).
- `tools` absent: `tools_filter` omitted. `auth` absent: `headers` omitted.
- Imports: `from pipecat.services.mcp_service import MCPClient` and the params
  classes `from mcp.client.session_group import ...`, gated on use.
- Dependency: `pipecat-ai[mcp,...]==<pinned version>`.
- Every client's `close()` runs on every shutdown path the bot already has.

## Both targets

- `FIRECRAWL_MCP_URL` and `FIRECRAWL_API_KEY` appear in `.env.example`, the
  startup check, and `compile-report.json` reference sites
  (`tools/web_search.yaml mcp.url_env` / `... mcp.auth.token_env`).
- The `_bearer` / `_api_key` helpers are the existing webhook ones; MCP adds
  no new auth code.
- An unreachable server at startup follows the platform: LiveKit logs the
  failed toolset and the agent still starts; Pipecat's `start()` raises and
  the bot exits loudly. The emitted README states both.

# mcp-example

One agent whose only tool comes from a remote MCP server. Ask it a question
that needs current information and it runs a web search through Firecrawl's
MCP server, then answers from what came back.

This is the MCP example: a single tool file declares the server, its transport,
its authentication, and which of its tools the agent may use. Both code targets
compile the same package, and both talk over WebRTC in the browser. No
telephony, no tunnel, no API of your own to stand up.

## What the tool file says

[`tools/web_search.yaml`](tools/web_search.yaml) is the whole surface:

```yaml
mcp:
  url_env: FIRECRAWL_MCP_URL
  transport: streamable_http
  auth:
    type: bearer
    token_env: FIRECRAWL_API_KEY
  tools:
    - firecrawl_search
```

Four things worth noticing:

- **The file is the block and nothing else.** No `description`, no `input`.
  The server describes its own tools when the connection opens, so writing a
  contract here would be a claim nobody reads. The
  [MCP tools reference](../../docs-site/build/tools/mcp.mdx) describes this
  contract, and an old-shape MCP file now fails at load saying which lines to
  delete.
- **The address and the token are environment variable names.** Never values.
  Both reach the generated `.env.example` and the generated startup check, so a
  missing one is named before the agent dials.
- **`transport` is optional here.** This URL ends in `/mcp`, so both targets
  would pick streamable HTTP on their own. The line is written out to show
  where the choice lives; delete it and the example still works.
- **`tools:` is the selection.** Firecrawl also exposes scraping and crawling.
  A voice agent carrying every one of them answers slower and picks worse, so
  this package takes the one tool it needs. Drop the list and the agent gets
  all of them.

The source name (`web_search`, from the file name) is what
[`agent.yaml`](agent.yaml) lists under the agent's `tools:`, exactly like any
other tool. A task's `tools:` list works the same on the LiveKit target;
Pipecat cannot scope an MCP source to a task and says so at validation.

## Run it

You need Docker running, a Firecrawl API key from
[firecrawl.dev](https://firecrawl.dev), and the two model keys the other
examples use. The repository-root `.env.example` carries all four names:

```text
OPENAI_API_KEY=...
SLNG_API_KEY=...
FIRECRAWL_MCP_URL=https://mcp.firecrawl.dev/v2/mcp
FIRECRAWL_API_KEY=...
```

```sh
make build
bin/unmute validate examples/mcp-example
bin/unmute compile examples/mcp-example
```

Copy the credentials into the package and fill in the blanks. The generated
template lists exactly what this package needs:

```sh
cp examples/mcp-example/build/pipecat/.env.example examples/mcp-example/.env
```

Then talk to it:

```sh
bin/unmute dev examples/mcp-example --target pipecat
```

```sh
bin/unmute dev examples/mcp-example --target livekit
```

Both open a browser page with a mic button. The LiveKit run also starts a local
LiveKit server, so it takes a little longer the first time.

## What to say

Ask for something that changed recently, so the model cannot answer from
memory:

- "What is the latest version of Python?"
- "Who won the last Formula One race?"
- "What is in the news about the European Central Bank today?"

You should hear a short "let me look that up", a pause of a second or two while
the search runs, then an answer built from the results. Ask a follow-up like
"where did that come from?" and the agent will tell you.

## What the two targets do differently

Same package, same conversation, two mechanisms:

| | LiveKit | Pipecat |
|---|---|---|
| Emission | one `mcp.MCPToolset` on the agent's `tools=` | one `MCPClient`, started at setup |
| Selection | `allowed_tools=["firecrawl_search"]` | `tools_filter=["firecrawl_search"]` |
| Unreachable server | logged; the agent still starts, without those tools | `start()` raises and the bot exits saying why |
| Task scope | works | not supported: list the source on the agent |

Both generated projects declare the SDK extra that carries the MCP client
(`livekit-agents[mcp,...]`, `pipecat-ai[mcp,...]`), so `uv sync` in either
`build/` directory installs what the emitted imports need.

## If a tool never gets called

The server's tool list only exists once the connection is open, so nothing can
check the names in `tools:` at compile time. A name Firecrawl does not expose
is simply never offered to the model. If searches never happen, check the
spelling against Firecrawl's own tool list first, then the worker log: an MCP
server that failed to connect says so there.

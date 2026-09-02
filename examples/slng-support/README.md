# slng-support

A customer support agent for a fictional company, Acme. It looks up an
order's status and delivery date, can search nearby places and the web, and
ends the call when the caller says goodbye.

It is the only example that targets `slng`, and the only one that produces no
runnable project. SLNG hosts the agent itself, so this package compiles to a
deployment body, not a project you start and keep running.

## Structure

- **Target.** `targets.yaml` names one target, `slng`, with
  `deployment_region: any`. A push deploys an agent named
  `acme-support-slng`: the package's name joined to the target's name.
- **Agent.** `agent.yaml` defines one agent, `support`, whose prompt is in
  `instructions.md`. It reasons with a Gemini model, speaks with an SLNG
  voice, and transcribes with Deepgram. It declares one variable,
  `customer_name`, used in the greeting.
- **Tools**, one of each kind SLNG supports:
  - `check_order` (local, handler `tools/check_order.py`): looks up an order.
  - `search_places_text` (webhook): calls a places-search API.
  - `web_search` (MCP): reaches the `firecrawl-mcp-2` server for
    `firecrawl_scrape` and `firecrawl_search`.
  - `end_call` (builtin): already provided by SLNG, so nothing is created
    for it.
- **Secrets.** One Vault entry, `SLNG_TOOL_RENDER`, the webhook tool's
  bearer token. `unmute deploy` checks for it and offers to create it.
- **Compiled output.** `build/slng/`: `agent.json`, `tools/check_order.json`,
  `tools/search_places_text.json`, and `README.md`. The builtin and the MCP
  tool get no file of their own.

## How to run it

Install the push tool once, so it is on your PATH:

```bash
brew install slng-ai/tap/voiceai
```

Validate, compile, and push in one command:

```bash
export SLNG_API_KEY=...
unmute deploy examples/slng-support
```

`--dry-run` reports what would happen and changes nothing. Or push an
already-compiled package yourself:

```bash
export VOICEAI_API_KEY=...
voiceai agents push examples/slng-support/build/slng
```

`voiceai login` stores the key instead, if you prefer.

`check_order` and `search_places_text` each need a sample before they
publish: add `build/slng/samples/check_order.json` and
`build/slng/samples/search_places_text.json`, one JSON object of arguments
in each, then deploy with `--run-samples`.

There is no `unmute dev` for this target. Talk to the deployed agent with a
web session instead:

```bash
voiceai agents web-sessions create <agent_id> --file session.json
```

`unmute deploy` prints this command with the agent id already filled in. A
minimal `session.json` is `{"arguments": {}}`.

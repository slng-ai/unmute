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
- **Tools**, one of each kind SLNG supports. Every one is a reference: this
  target creates no tool.
  - `check_order` (`slng:`): a tool SLNG hosts, holding Python. Its mirror is
    committed as `tools/check_order.slng.json` and
    `tools/check_order.slng.py`.
  - `search_places_text` (`slng:`): a tool SLNG hosts, holding a request
    configuration. Its mirror is `tools/search_places_text.slng.json`; there
    is no module, because a request tool has no code.
  - `web_search` (MCP): reaches the `firecrawl-mcp-2` server for
    `firecrawl_scrape` and `firecrawl_search`.
  - `end_call` (builtin): a capability SLNG curates, reached by name.
- **Secrets.** One Vault entry, `SLNG_TOOL_RENDER`, the hosted request tool's
  bearer token. The platform names it in the mirror, so it reaches the vault
  table and the deploy check from there. `unmute deploy` looks for it and
  offers to create it.
- **Compiled output.** `build/slng/`: `agent.json` and `README.md`. That is
  all of it. Every tool is named rather than created, so there is no tool body
  to write beside the agent body.

## How to run it

Install the push tool once, so it is on your PATH:

```bash
brew install slng-ai/tap/voiceai
```

The two hosted tools already exist in the SLNG organisation this example was
written against. Their mirrors are committed, so `validate` and `compile` work
with no credential. Fetch them again if the tools change on the platform:

```bash
export SLNG_API_KEY=...
unmute pull examples/slng-support
```

Commit whatever it writes. The mirror is what would let these tools also run on
a livekit or pipecat target, and the `hash:` in each tool file is how a later
compile knows the mirror is still the right one.

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

Both hosted tools are already published on the platform, so nothing here has to
be introspected or run before the push. To try one on its own:

```bash
voiceai tool run check_order --input samples/check_order.json --confirm-side-effects
```

That runs the copy SLNG already has, against its real dependencies.

There is no `unmute dev` for this target. Talk to the deployed agent with a
web session instead:

```bash
voiceai agents web-sessions create <agent_id> --file session.json
```

`unmute deploy` prints this command with the agent id already filled in. A
minimal `session.json` is `{"arguments": {}}`.

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
  voice, and transcribes with Deepgram. The greeting names nobody, so it
  reads the same whether or not a session supplies a name.
- **Tools**, one of each kind SLNG supports. Every one is a reference: this
  target creates no tool, and no tool file carries a mirror.
  - `check_order` (`slng: check_order`): a tool SLNG already hosts, holding
    Python. The one line names it; description and parameters are inherited
    from the published tool.
  - `search_places_text` (`slng: search_places_text`): a tool SLNG already
    hosts, holding a request configuration. Its `inject:` fills `query` from
    `{{customer_name}}`, so the model never has to ask the caller to repeat a
    name it already gave when the session started.
  - `web_search` (MCP): reaches the `firecrawl-mcp-2` server for
    `firecrawl_scrape` and `firecrawl_search`, naming only the server and the
    two tools; the server's own address and credential are SLNG's.
  - `end_call` (builtin): a capability SLNG curates, reached by name.
- **A declared variable.** `customer_name`, `source: call_start`: the caller's
  name, supplied when a session is dispatched. It has no `secrets:` entry and
  no mirror to come from; `search_places_text` reads it straight off the
  declared variable through `inject:`.
- **Secrets.** No `secrets:` block. The hosted request tool's own credential
  is SLNG's, not this package's: `unmute deploy` discovers it from the
  published tool and reports or offers to create it, rather than this file
  repeating a name that could go out of step with the platform.
- **Compiled output.** `build/slng/`: `agent.json`, `README.md` and
  `compile-report.json`. That is all of it. Every tool is named rather than
  created, so there is no tool body to write beside the agent body, and the
  report names what the compile could not check offline, which `unmute
  deploy` completes.

## How to run it

Run these commands from the repository root. To test the current branch, run
`make build` and use `./bin/unmute` in place of `unmute` below.

Install the push tool once, so it is on your PATH:

```bash
brew install slng-ai/tap/voiceai
```

Both hosted tools already exist in the SLNG organisation this example was
written against, so `validate` and `compile` work with no credential and no
mirror to fetch first:

```bash
unmute validate examples/slng-support
unmute compile examples/slng-support --target slng
```

`unmute pull` would only matter if this package also compiled to `livekit` or
`pipecat`, which build and run a hosted tool themselves and so need a real
copy of it. It does not: this example targets slng alone.

Validate, compile, and push in one command. A real push needs a `voiceai`
release that supports a checked, resolved attachment; an older one is refused
with upgrade guidance before anything is written. This flow has been verified
with `voiceai 0.1.18`:

```bash
export SLNG_API_KEY=...
unmute deploy examples/slng-support --target slng --dry-run
unmute deploy examples/slng-support --target slng
```

`--dry-run` previews the checked tool versions and attachment changes without
changing SLNG. Both commands write `build/slng/deploy-report.json`. Each real
deployment resolves the latest published tools again and attaches the versions
it checked. Rename the package's `name:` before deploying a separate test agent;
otherwise this command replaces `acme-support-slng` if it already exists.

For the standalone `voiceai` commands below, use the same account:

```bash
export VOICEAI_API_KEY="$SLNG_API_KEY"
```

`voiceai login` stores the key instead, if you prefer.

Both hosted tools are already published on the platform, so nothing here has to
be introspected or run before the push. To try one on its own:

```bash
voiceai tool run check_order --input - --confirm-side-effects <<'JSON'
{"order_number":"A-1001"}
JSON
```

That runs the copy SLNG already has, against its real dependencies.

There is no `unmute dev` for this target. Talk to the deployed agent with a
web session instead:

```bash
cat > session.json <<'JSON'
{"arguments":{"customer_name":"Nicola"},"participant_name":"you"}
JSON
voiceai agents web-sessions create <agent_id> --file session.json
```

`unmute deploy` prints this command with the agent id already filled in. A
session's `arguments` supplies the required `customer_name`. That is what
`search_places_text` injects as `query`.

This command returns `livekit_url` and `livekit_token`; it does not open a
browser or microphone. Connect a LiveKit client with those details to talk.
A dispatch id alone does not prove the worker joined or a conversation happened.

Ask “What is the status of order A-1001?” to exercise `check_order` and its
announcement, then say goodbye to exercise `end_call`. To check injection,
ask for a places search and inspect the tool call's `query`: it should be
`Nicola`, the session value. Ask for a web search to exercise the MCP selection.

After the conversation, use the returned `call_id` to read the actual result:

```bash
voiceai agents calls list <agent_id> --json
voiceai agents calls get <agent_id> <call_id> --json
```

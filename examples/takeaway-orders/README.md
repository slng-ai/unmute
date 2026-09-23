# takeaway-orders

Take a takeaway order with an OpenAI live voice model and tools that look up dishes and calculate the total.
The live model handles the conversation. Its OpenAI backend runs local tools and searches the shop documents.

On this page:

- [Quickstart](#quickstart) - start the browser call
- [1. Try the workflow](#1-try-the-workflow) - check tools and answers
- [2. Read the configuration](#2-read-the-configuration) - models and defaults
- [3. Change the knowledge](#3-change-the-knowledge) - edit the source documents
- [Files](#files) - find the package parts
- [Advanced](#advanced) - customize or switch architecture
- [Troubleshooting](#troubleshooting) - symptoms and fixes
- [Where to go next](#where-to-go-next) - guides and examples

## Quickstart

This is a complete package. From the repository root, set `OPENAI_API_KEY` in your shell or `examples/takeaway-orders/.env`.
Keep any existing secrets when editing `.env`.

```sh
# Terminal, from the repository root
unmute validate examples/takeaway-orders
unmute compile examples/takeaway-orders
unmute dev examples/takeaway-orders --target pipecat
```

Pipecat runs locally with `uv`. Open the dev page, allow microphone access, and connect.
The package supports **browser audio** on Pipecat and LiveKit. It declares no phone connection.

To test LiveKit, stop the first run, start Docker, and run:

```sh
# Terminal, from the repository root
unmute dev examples/takeaway-orders --target livekit
```

The same OpenAI key serves the voice model, backend, and knowledge embeddings.
Your account must have access to the configured models.

## 1. Try the workflow

1. Ask for crispy duck. The menu tool should report that it is unavailable.
2. Order salt and pepper chicken and egg fried rice for collection.
3. Ask about nuts in kung pao chicken. The answer should come from the kitchen documents.
4. Confirm the order and check the tool result for its total and order number.
5. Interrupt the reply with a question about Monday opening hours.

Watch the dev page's tool rows and compare their results with the spoken answer.
These handlers use a demo store; they do not send orders to a restaurant.
Run their local checks separately:

```sh
# Terminal, from the repository root
python3 examples/takeaway-orders/tools/takeaway.py
```

## 2. Read the configuration

The following is a **reference fragment** from `agent.yaml`, not a replacement file.
Keep the existing agent instructions and tool attachments.

```yaml
# examples/takeaway-orders/agent.yaml
architecture: live
models:
  live:
    - name: counter_voice
      provider: openai
      model: gpt-live-1
      voice: marin
      backend: kitchen
  think:
    kitchen:
      provider: openai
      model: gpt-5.6-terra
```

The `counter` agent binds `live: counter_voice`.
The live model controls speech and interruptions; it has no separate transcriber or synthesizer.
The backend is required because this agent has tools, including knowledge lookup.
Only the backend model name reaches the live service; its `params:` would not apply.

`voice:` is explicit here. If omitted, the provider chooses its default.
The greeting is an opening instruction, so the live model can paraphrase it.

## 3. Change the knowledge

Edit the documents under `knowledge/kitchen/`.
The package declares that folder as a knowledge base, and `tools/look_up_kitchen_info.yaml` exposes its search tool.
Default retrieval settings are used; see [Knowledge bases](../../docs-site/build/tools/knowledge.mdx) for tuning.

Recompile and restart the dev run after changing documents. The worker builds its index at startup from the compiled content.

```sh
# Terminal, from the repository root
unmute compile examples/takeaway-orders
unmute dev examples/takeaway-orders --target pipecat
```

## Files

| File | Purpose |
|---|---|
| `agent.yaml` | Architecture, models, tool attachments, knowledge, and conversation settings. |
| `targets.yaml` | Pipecat and LiveKit targets, both with browser audio. |
| `instructions.md` | Conversation instructions and when to call tools. |
| `tools/*.yaml` | Local tool contracts, knowledge lookup. |
| `tools/takeaway.py` | Local handlers, demo records, and runnable checks. |
| `knowledge/kitchen/*.md` | Menu policies, allergens, and opening hours. |

## Advanced

<details>
<summary>Let Coval call it on your laptop</summary>

This package has no phone route, but its Pipecat build still answers the
runner's Twilio route. So Coval can place simulated calls to it, on both
targets, with nothing deployed:

```sh
uv run --project utils/coval_sim coval-sim add takeaway-orders   # once
make sim TEST_SET=<your test set id>
```

`add` creates the Coval agents `unmute-takeaway-orders-livekit` and
`unmute-takeaway-orders-pipecat`. Attach a test set and metrics to both in Coval first.
[The coval-sim guide](../../utils/coval_sim/README.md) has the rest.

</details>

<details>
<summary>Change the live model or backend</summary>

Edit `models.live` and its referenced `models.think` entry in `agent.yaml`.
The backend must use OpenAI and cannot set `endpoint_env`.
Keep `backend: kitchen` while tools are attached. Validate and restart after each change.
See [Live model fields](../../docs-site/models/live.mdx) for supported settings.

</details>

<details>
<summary>Switch architecture or add a larger workflow</summary>

Both S2S architectures currently support one agent and browser audio.
They do not support tasks, task groups, handoffs, variables, pre-fetch, tracing, MCP tools, escalations, or phone connections.
Live also refuses separate listen, speak, and turn sections and `conversation.interruption`.

For those features, follow the [architecture switching guide](../../docs-site/build/architecture/overview.mdx) and use cascade.
Omitting `architecture` defaults to cascade, but you must also replace the model palette and agent bindings.

</details>

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| Startup reports a missing key | The worker cannot read `OPENAI_API_KEY`. | Set it in the shell or package `.env`, preserving existing secrets, then restart. |
| The first turn fails with a provider error | The model or voice is unavailable to the key. | Read the dev logs and check access to each configured model. |
| The model speaks but a tool does not run | The tool is unattached, its arguments are wrong, or its handler refused the request. | Check tool rows and logs; compare attachments and arguments with `tools/*.yaml`. |
| A knowledge answer is missing | The documents do not cover the question, or the compiled content is stale. | Update the source document, compile, and restart. |
| Validation asks for a backend | The agent has tools but its live entry has no backend. | Restore `backend: kitchen` and its OpenAI think entry. |
| The greeting uses different words | The live model paraphrases its opening instruction. | Write the intended meaning; exact wording is not guaranteed. |
| The order total seems wrong | The demo menu or quantities differ from the request. | Inspect `place_order` arguments and run the Python demo check. |

## Where to go next

- [Live architecture](../../docs-site/build/architecture/live.mdx) - build a small agent from scratch.
- [Switch architecture](../../docs-site/build/architecture/overview.mdx) - compare defaults, bindings, and limits.
- [Knowledge bases](../../docs-site/build/tools/knowledge.mdx) - configure document search.
- [pharmacy-refills](../pharmacy-refills/) - try the other S2S architecture.
- [salon-concierge](../salon-concierge/) - a cascade workflow with tasks and phone routes.

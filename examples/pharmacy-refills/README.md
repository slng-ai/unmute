# pharmacy-refills

Try a prescription refill conversation with an OpenAI realtime model that hears and speaks directly.
The realtime model runs the tools itself. Its explicit `turn_detection: semantic` setting asks the provider to judge when the caller has finished.

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

This is a complete package. From the repository root, set `OPENAI_API_KEY` in your shell or `examples/pharmacy-refills/.env`.
Keep any existing secrets when editing `.env`.

```sh
# Terminal, from the repository root
unmute validate examples/pharmacy-refills
unmute compile examples/pharmacy-refills
unmute dev examples/pharmacy-refills --target pipecat
```

Pipecat runs locally with `uv`. Open the dev page, allow microphone access, and connect.
The package supports **browser audio** on Pipecat and LiveKit. It declares no phone connection.

To test LiveKit, stop the first run, start Docker, and run:

```sh
# Terminal, from the repository root
unmute dev examples/pharmacy-refills --target livekit
```

The same OpenAI key serves the voice model and knowledge embeddings.
Your account must have access to the configured models.

## 1. Try the workflow

1. Ask for a refill on reference R X four eight two one B, with a pause in the middle.
2. Check that the agent collects the full reference before calling `look_up_prescription`.
3. Follow the verification questions and confirm the refill request.
4. Check the `request_refill` result and the agent's explanation of that result.
5. Ask about collection or delivery. The answer should come from the policy documents.

Watch the dev page's tool rows and compare their results with the spoken answer.
These handlers use a demo store; they do not submit prescriptions to a pharmacy.
Run their local checks separately:

```sh
# Terminal, from the repository root
python3 examples/pharmacy-refills/tools/pharmacy.py
```

## 2. Read the configuration

The following is a **reference fragment** from `agent.yaml`, not a replacement file.
Keep the existing agent instructions and tool attachments.

```yaml
# examples/pharmacy-refills/agent.yaml
architecture: realtime
models:
  realtime:
    - name: counter
      provider: openai
      model: gpt-realtime
      voice: marin
      turn_detection: semantic
```

The `refills` agent binds `realtime: counter`. There is no separate backend model.
A provider voice is required unless the agent binds a separate synthesizer.

| Turn setting | Who ends the turn | What to try |
|---|---|---|
| `semantic` | Provider judges whether the thought is complete | Read a reference with pauses, as this package expects. |
| `server_vad` | Provider detects a silence window | Compare short replies with a reference read in groups. |
| `local` | Framework controls turn completion | Compare framework behavior on each target. |

Omitting `turn_detection` leaves the integration default: Pipecat uses provider server VAD, while LiveKit currently defaults to semantic detection.
Set it explicitly to keep the intended behavior across targets. No setting guarantees that every pause is handled correctly.

## 3. Change the knowledge

Edit the documents under `knowledge/policies/`.
The package declares that folder as a knowledge base, and `tools/look_up_pharmacy_policy.yaml` exposes its search tool.
Default retrieval settings are used; see [Knowledge bases](../../docs-site/build/tools/knowledge.mdx) for tuning.

Recompile and restart the dev run after changing documents. The worker builds its index at startup from the compiled content.

```sh
# Terminal, from the repository root
unmute compile examples/pharmacy-refills
unmute dev examples/pharmacy-refills --target pipecat
```

## Files

| File | Purpose |
|---|---|
| `agent.yaml` | Architecture, models, tool attachments, knowledge, and conversation settings. |
| `targets.yaml` | Pipecat and LiveKit targets, both with browser audio. |
| `instructions.md` | Conversation instructions and when to call tools. |
| `tools/*.yaml` | Local tool contracts, knowledge lookup, and end-call tool. |
| `tools/pharmacy.py` | Local handlers, demo records, and runnable checks. |
| `knowledge/policies/*.md` | Refill, collection, delivery, and controlled-medicine policies. |

## Advanced

<details>
<summary>Let Coval call it on your laptop</summary>

This package has no phone route, but its Pipecat build still answers the
runner's Twilio route. So Coval can place simulated calls to it, on both
targets, with nothing deployed:

```sh
uv run --project utils/coval_sim coval-sim add pharmacy-refills   # once
make sim TEST_SET=<your test set id>
```

`add` creates the Coval agents `unmute-pharmacy-refills-livekit` and
`unmute-pharmacy-refills-pipecat`. Attach a test set and metrics to both in Coval first.
[The coval-sim guide](../../utils/coval_sim/README.md) has the rest.

</details>

<details>
<summary>Compare turn detection or use a separate voice</summary>

Change `turn_detection` on the existing realtime entry, validate, and repeat the same reference-reading test.
With `local`, the framework controls completion, but `models.turn` and package-level `turn` or `listen` bindings remain unsupported.
See [Turn detection](../../docs-site/models/turn-detection.mdx) for target behavior.

For a separate synthesizer, remove `voice` from the realtime entry and add an agent `speak` binding.
Follow the [complete customization steps](../../docs-site/build/architecture/realtime.mdx#advanced); setting both voices is refused.

</details>

<details>
<summary>Switch architecture or add a larger workflow</summary>

Both S2S architectures currently support one agent and browser audio.
They do not support tasks, task groups, handoffs, variables, pre-fetch, tracing, MCP tools, escalations, or phone connections.
Realtime refuses separate listen and turn model sections and an agent `think:` binding.

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
| The agent interrupts a reference | The turn detector judged the pause as completion. | Restore `semantic` and the prompt instruction to wait for the full reference; repeat the same call. |
| Replies wait too long | Turn detection waits longer than this conversation needs. | Compare `server_vad` with `semantic` using the same requests. |

## Where to go next

- [Realtime architecture](../../docs-site/build/architecture/realtime.mdx) - build a small agent from scratch.
- [Switch architecture](../../docs-site/build/architecture/overview.mdx) - compare defaults, bindings, and limits.
- [Knowledge bases](../../docs-site/build/tools/knowledge.mdx) - configure document search.
- [takeaway-orders](../takeaway-orders/) - try the other S2S architecture.
- [salon-concierge](../salon-concierge/) - a cascade workflow with tasks and phone routes.

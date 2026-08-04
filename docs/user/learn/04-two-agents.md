# 04. Two agents

Real customer service has specialists. The front desk should hand a billing question to someone who only does billing, with their own prompt, their own voice, and their own reasoning model. This is **tier T2: agent handoff.**

We add a second agent, `billing`, and a **control** that transfers the call to it.

## A control is a named action

So far the model could call tools. A **control** is the other thing the model can invoke: an action that changes the conversation itself, like transferring to another agent. Controls live in a `controls:` block, and they share one name space with tools. An agent gets a control the same way it gets a tool: by listing its name in the agent's `tools:` list.

## The changes to agent.yaml

Add a think model, a speak model, the second agent, and the transfer control:

```yaml
models:
  think:
    fast_reasoning:
      description: cheap and quick, for greeting and routing
      provider: openai
      model: gpt-4o-mini
    careful_reasoning:               # new: billing gets a stronger model
      description: slower and careful, for billing work
      provider: openai
      model: gpt-4o
  speak:
    front_desk:
      description: "warm, concise"
      provider: slng
      model: "slng/deepgram/aura:2-en"
      voice: "aura-2-thalia-en"
    specialist:                      # new
      description: "slower, more deliberate"
      provider: slng
      model: "slng/deepgram/aura:2-en"
      voice: "aura-2-orion-en"

agents:
  intake:
    instructions: instructions.md
    model: fast_reasoning
    voice: front_desk
    tools: [lookup_customer, to_billing]   # intake can now transfer

  billing:                                 # new agent
    instructions: agents/billing.md
    model: careful_reasoning
    voice: specialist
    tools: [get_invoice]

controls:
  to_billing:
    kind: agent_transfer
    to: billing
    when: Caller asks about billing, an invoice, or a refund.
    context:
      history: full
      variables: all
```

The billing agent gets its own prompt file, `agents/billing.md`:

```markdown
You are the billing specialist for Acme Support. Keep answers to one or two
short sentences.

- The caller was handed to you with a billing question. The conversation so
  far is in your context.
- Use `get_invoice` to look up invoices for customer `{{customer_id}}`.
- Explain charges calmly, one item at a time.
```

> `{{customer_id}}` is the authoring syntax, but **substitution into prompt text is not implemented yet**: the braces reach the generated project as literal characters, so the model reads `{{customer_id}}` rather than the value. See [03. Variables](03-variables.md).

Two housekeeping notes: additional agents keep their prompts in an `agents/` folder, and the new `get_invoice` tool is added the same way as [lookup_customer](02-add-a-tool.md) (its own file, listed in the top-level `tools:` manifest and in billing's `tools:` list).

## Reading the transfer control

**`kind: agent_transfer`** is the handoff action.

**`to`** names the agent that takes over. When the model calls this control, `billing` becomes the active agent and `intake` steps aside.

**`when`** is the plain-language trigger the model reads to decide when to transfer. It becomes part of the tool description the model sees.

**`context`** is the important part. A handoff has to decide **what the new agent knows.** There is no default, because the platforms disagree about their own defaults, so you must say it:

- **`history`** is how much of the conversation carries over. `full` carries the whole transcript, so billing can see what the caller already said. Other values (`messages`, `last_n`, `summary`, `reset`) trim it in different ways.
- **`variables`** is which shared variables carry over. `all` carries everything in your `variables` block, so `customer_id` and `verified` come along.

`history: full` with `variables: all` is the portable choice: it works on all four platforms and it is what the Pipecat driver emits today.

## Guarding a handoff (optional)

You often want a rule like "do not transfer to billing until the caller is verified". Add `requires`:

```yaml
controls:
  to_billing:
    kind: agent_transfer
    to: billing
    when: Caller asks about billing, an invoice, or a refund.
    requires: [verified]        # refuse the handoff unless `verified` is set
    context:
      history: full
      variables: all
```

`requires` is a machine-checked guard: the transfer is refused unless every listed variable is set. When the model tries to transfer too early, it gets a refusal that names the missing variable, and the guard being enforced is part of the contract, not a suggestion to the model.

## What Pipecat generates

Each agent is its own worker with its own model and voice. The transfer becomes a method that activates the target worker and steps the current one aside (LiveKit does the same thing with different machinery; see [how targets run your agent](../concepts/how-targets-run-your-agent.md)):

```python
@tool(cancel_on_interruption=False)
async def to_billing(self, params: FunctionCallParams):
    """Caller asks about billing, an invoice, or a refund."""
    await self.activate_worker(
        "billing",
        args=LLMWorkerActivationArgs(
            messages=[{"role": "developer", "content": "Caller asks about billing, an invoice, or a refund."}],
            run_llm=True,
        ),
        deactivate_self=True,
        result_callback=params.result_callback,
    )
```

With `requires`, a guard is generated in front of that, checking the state and refusing with a message if a variable is missing. Because both agents share the one `STATE` object from [03. Variables](03-variables.md), billing already has `customer_id`.

## What just got harder

Handoff is where the platforms start to diverge, so read this:

- **The basic handoff is `core`.** `agent_transfer` with `history: full` and `variables: all` works on all four platforms. Any number of agents can transfer between each other this way.
- **`requires` is `gated`.** It works on Pipecat, LiveKit, and Deepgram (code targets can generate the check). It **fails on Vapi**, which has no mechanism for it.
- **History other than `full` is limited.** On Pipecat, the driver emits `full` only today; the other history values are a driver maturity gate, not a platform limit (see the [Pipecat target page](../targets/pipecat.md)). For now, keep `history: full`.

On Pipecat specifically, everything on this page works, including the `requires` guard.

Next: [05. Tasks](05-tasks.md), where the agent delegates a piece of work and gets a typed answer back.

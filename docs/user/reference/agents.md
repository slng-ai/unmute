# Reference: agents

`agents` is a named map of the agents in your package. The entry agent (named by top-level `entry_agent`) must be one of them. Additional agents keep their prompts in an `agents/` folder. Transfers between agents are tier T2; see [controls](controls.md) and the [two-agents learn page](../learn/04-two-agents.md).

```yaml
agents:
  intake:
    instructions: instructions.md
    model: fast_reasoning
    voice: front_desk
    tools: [lookup_customer, to_billing]

  billing:
    instructions: agents/billing.md
    model: careful_reasoning
    voice: specialist
    tools: [get_invoice, to_human]
```

## Fields

### instructions

Path to the Markdown file holding this agent's full prompt. The path is relative to the package root.

Required: yes. Values: a path to a `.md` file. Default: none. Targets: all four, core.

### model

The reasoning model this agent uses. It must name an entry in the top-level
`models.think` section.

Required: yes. Values: a
[think model name](models-and-voices.md). Default: none. Targets: all four,
core.

### voice

The voice this agent speaks with. It must name an entry in the top-level
`models.speak` section.

Required: yes. Values: a
[speak model name](models-and-voices.md). Default: none. Targets: all four,
core. Per-agent voices are native on LiveKit and Pipecat, and work
on all four.

### tools

The tools and controls this agent may invoke. Names come from the shared tool-and-control name space: plain [tools](tools.md) and [controls](controls.md) are listed the same way. A tool an agent does not list is invisible to that agent, even if it is compiled into the package by the top-level `tools:` manifest.

Required: no. Values: a list of tool and control names. Default: none (the agent can invoke nothing). Targets: all four, core.

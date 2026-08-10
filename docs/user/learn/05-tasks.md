# 05. Tasks

Sometimes an agent needs a small, focused piece of work done: read the conversation so far and pull out a structured answer, without the caller noticing a change of speaker. A **task** does exactly that. The agent **delegates** the work, the task runs, produces a **typed result**, and hands control straight back to the agent. This is **tier T1**.

A task is different from a handoff. A handoff ([04](04-two-agents.md)) moves the conversation to another agent and stays there. A task runs quietly and returns; the same agent keeps talking to the caller.

## Declare the task

Tasks are declared in a `tasks:` block in `agent.yaml`, with their prompts in a `tasks/` folder:

```yaml
tasks:
  collect:
    instructions: tasks/collect.md
    result:
      verified_flag: boolean
      tier:
        enum:
          - free
          - pro
    context:
      history: full
```

And the prompt, `tasks/collect.md`:

```markdown
Read the conversation so far. Decide whether the caller is verified, and which
account tier they are on (free or pro). Return only the structured result.
```

## Delegate to it

A task does not run on its own. An agent reaches it through a `delegate` control:

```yaml
controls:
  run_collect:
    kind: delegate
    task: collect
    when: Collect the caller's account details.
    assign:
      verified: result.verified_flag
```

Then add `run_collect` to the intake agent's `tools:` list, exactly like any other control. Now the model can call it.

## Reading the pieces

**`instructions`** is the task's own prompt. LiveKit and Pipecat use it instead
of the calling agent's instructions while the task runs. Pipecat emits it as
the Flow node's `role_message`. The
[context strategy map](../concepts/how-targets-run-your-agent.md#compare-context-strategies)
shows every supported value.

**`model`** is optional and not shown here. Leave it out and the task runs on the delegating agent's model. Set it to run the task on a specific model, for example a stronger one for a careful extraction. Know that this override currently **fails on Pipecat** (the compiler tells you; see the [tasks reference](../reference/tasks.md)), which is why this page leaves it out.

**`result`** is the typed answer the task must return. It is a flat map of name to type. Each field is one of the four primitives (`string`, `number`, `boolean`, `integer`) or an enum written `{ enum: [a, b] }`. Here the task must return a boolean `verified_flag` and a `tier` that is either `"free"` or `"pro"`. The result is the whole point of a task: structured data, not a chat reply.

**`context`** says what the task can see. `history: full` gives it the whole conversation so far. (A task uses the shared call state automatically, so unlike a transfer it has no `variables` field to set.)

**`when`** on the delegate is the trigger the model reads, same as on a transfer.

**`assign`** maps result fields into your variables: `verified: result.verified_flag` writes the task's `verified_flag` into the shared `verified` variable. This is the main way a variable changes mid-call. The field must exist in the task's `result` and its type must match the variable. (An enum field assigns into a `string` variable.)

## What Pipecat generates

The task runs as a **Pipecat Flow** on the agent that delegated it. Nothing new
starts talking. The Flow keeps the running context, replaces the current system
instruction with the task prompt, and replaces the agent's tools with the
task's tools plus one extra `finish` function whose arguments are exactly your
`result` map:

```python
def _run_collect_node_collect(self) -> NodeConfig:
    return NodeConfig(
        name="collect",
        role_message="Read the conversation so far...",
        task_messages=[{"role": "developer", "content": "Begin this step."}],
        functions=[FlowsFunctionSchema(
            name="finish_run_collect_collect",
            description="Record the result of this step and finish.",
            properties={"tier": {"enum": ["free", "pro"], "type": "string"},
                        "verified_flag": {"type": "boolean"}},
            required=["tier", "verified_flag"],
            handler=self._run_collect_finish_collect,
        )],
        respond_immediately=True,
    )
```

Your `result` map became the `finish` function's strict schema, so the answer really is typed. The delegate method saves the agent's context, then starts the flow:

```python
@tool()
async def run_collect(self, params: FunctionCallParams):
    """Collect the caller's account details."""
    self._run_collect_results = {}
    self._run_collect_snapshot = (
        [dict(m) for m in self.context.get_messages()], self.context.tools
    )
    flow = FlowManager(
        llm=self.llm,
        context_aggregator=LLMContextAggregatorPair(self.context),
        worker=self,
    )
    await flow.initialize(self._run_collect_node_collect())
    return {"status": "running the collect task"}
```

And when the model calls `finish`, the handler records the result, runs your `assign`, and restores the agent's own prompt and tools, handing back only the typed results:

```python
async def _run_collect_finish_collect(self, args, flow_manager):
    self._run_collect_results["collect"] = dict(args)
    self.state.verified = self._run_collect_results["collect"]["verified_flag"]
    messages, tools = self._run_collect_snapshot
    await self.queue_frame(LLMUpdateSettingsFrame(
        delta=LLMSettings(system_instruction="You are the intake agent."),
    ))
    self.context.set_messages(messages + [{
        "role": "developer",
        "content": "Task results: " + json.dumps(self._run_collect_results)
        + " Continue with the caller in one short line.",
    }])
    self.context.set_tools(tools)
    return {"status": "ok"}, None
```

The `self.state.verified = ...` line is your `assign` in action. Pipecat's
snapshot-restore means the agent's memory gains the results, not the task's
inner back-and-forth. LiveKit isolates the task's entry prompt, but its current
standalone-task completion path merges user and assistant task turns back into
the owner. See [how targets run your agent](../concepts/how-targets-run-your-agent.md)
for the complete boundary map.

## What just got harder

Tasks are the first tier where a whole platform drops out:

- **A task fails on Vapi.** It is not proven that a Vapi handoff can return to the calling assistant, so single-task delegates are blocked there.
- **A task works on LiveKit, Deepgram, and Pipecat.** On Pipecat everything on this page is emitted: the flow step, the typed result, and the assign.
- **The per-task `model:` override fails on Pipecat.** The compile error names it; drop the override and the same spec passes. It works on LiveKit.

So a task is portable across the code targets and one managed target, but not Vapi.

Not sure a task is the right tool? [Why tasks and task groups](../concepts/why-tasks-and-task-groups.md) covers when to reach for one, versus a single agent or a full handoff.

Next: [06. Task groups](06-task-groups.md), which run several tasks in order and decide what happens when they finish.

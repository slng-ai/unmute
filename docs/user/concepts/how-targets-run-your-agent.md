# How targets run your agent

The same `agent.yaml` compiles to genuinely different machinery on each code target. You never have to know this to use Unmute. But if you read the generated project, or you want to trust that "portable" is real and not marketing, this page shows what agents, handoffs, tasks, and task groups actually become on LiveKit and on Pipecat.

The two frameworks think about multi-agent work differently. In one line each:

- **LiveKit**: one session, and the compiler swaps *who is talking*. Agents and tasks are objects that take turns holding the mic.
- **Pipecat**: a bus of workers, and for tasks nobody is swapped. The same agent gets *re-programmed* step by step.

The caller cannot tell the two bots apart. That is the point.

## Agents and handoff

| | LiveKit | Pipecat |
|---|---|---|
| What an agent is | An `Agent` class inside one `AgentSession` | An `LLMWorker` with its own reasoning model and voice, sitting on a message bus. A main worker owns the microphone and speech-to-text and routes audio to whichever agent is active |
| How a handoff happens | The transfer tool **returns the new agent object**. The session sees the return value and swaps speakers | The transfer tool calls **`activate_worker(...)`**. The bus deactivates one worker and activates another |
| How context carries | The chat context is copied into the new agent | The activation carries the running history plus a reason message |

On LiveKit a handoff looks like this:

```python
@function_tool
async def to_billing(self, ctx: RunContext):
    """Caller asks about billing."""
    return Billing(chat_ctx=self.chat_ctx.copy(exclude_instructions=True))
```

On Pipecat, like this:

```python
@tool(cancel_on_interruption=False)
async def to_billing(self, params: FunctionCallParams):
    """Caller asks about billing."""
    await self.activate_worker(
        "billing",
        args=LLMWorkerActivationArgs(messages=[...], run_llm=True),
        deactivate_self=True,
    )
```

Same yaml, same behavior: the caller keeps talking and a different specialist answers. Structurally, LiveKit swaps objects inside one brain-holder, and Pipecat switches between brain-holders.

## Tasks

A task ([learn page 05](../learn/05-tasks.md)) is a focused step with its own prompt, its own tools, and a typed result. Here the two targets diverge the most.

**On LiveKit a task is a real object.** The delegate tool awaits an `AgentTask`. The task takes over the conversation, works with its own tools, and calls `complete(result)` when done. The `await` returns the typed result to the agent that delegated.

**On Pipecat a task is not an object at all.** The delegating agent stays active the whole time. A `FlowManager` rewrites that same agent's prompt and tools: for the duration of the step, the agent only sees the task's instructions, the task's tools, and one extra `finish` function whose arguments are exactly your `result` map. When the model calls `finish`, the step is done and the agent's own prompt and tools come back.

So during a task, LiveKit has handed the mic to a different object, while Pipecat has re-programmed the same one. The caller hears the same voice either way.

## Task groups

A group ([learn page 06](../learn/06-task-groups.md)) is an ordered list of tasks.

- **LiveKit**: a `TaskGroup` container. The generated code adds each step and awaits the whole group; the results come back together.
- **Pipecat**: a chain of re-programmings. Each step's `finish` function returns the *next* step's configuration, so the agent walks the chain one node at a time until the last `finish`.

`context_scope` works on both, with one structural twist. On Pipecat, `shared` means each step keeps seeing the growing conversation, and `isolated` resets the context per node. On LiveKit, `TaskGroup` always shares context, so `isolated` compiles to something else entirely: a plain sequence of standalone `AgentTask`s, each starting fresh, with the results collected into the same shape a group would return. Same yaml, same contract, different container.

## The part that is identical on purpose

Both frameworks would naturally leak the task's back-and-forth ("what date works? the 14th. how many people? four...") into the delegating agent's memory. Your spec says `merge: results`: the owner gets **only** the typed results. So on both targets the generated code plays the same trick: snapshot the agent's context before the flow, restore it after, and inject a single line like `Task results: {"date": "2026-08-14", ...}`. Different machinery, one contract. This is invariant N13 in the schema.

## Where the abstraction leaks

One knob currently works on LiveKit but not Pipecat: the per-task `model:` override ([reference](../reference/tasks.md)). Pipecat's mechanism for swapping models mid-call stalls the conversation in the current release, so the compiler refuses it there with a clear error instead of emitting a bot that freezes. Everything else on this page compiles from the identical yaml.

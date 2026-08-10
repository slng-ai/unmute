# 06. Task groups

One task does one thing. A **task group** runs several tasks in a fixed order, then decides what happens when they finish. It is still tier T1, and it is the most structure Unmute gives you in this version.

The one rule to keep in mind, from [tiers](../concepts/tiers.md): a group is an **ordered list, not a graph.** Steps run one after another, front to back. No branches, no loops, no going back. If you need a decision between paths, that is a handoff to another agent, not a group.

## Declare a group

A group lists the tasks it runs as steps, and says what to do at the end:

```yaml
task_groups:
  triage:
    steps:
      - collect
    context_scope: isolated
    then: return
    merge: results
```

You reach a group through a `delegate` control, the same way you reach a single task, but with `group` instead of `task`:

```yaml
controls:
  run_triage:
    kind: delegate
    group: triage
    when: Run the triage steps before helping the caller.
```

Add `run_triage` to the agent's `tools:` list and the model can call it.

## Reading the group

**`steps`** is the ordered list of task names to run. Each name is a task you declared in your `tasks:` block ([05](05-tasks.md)). They run in the order written.

**`context_scope`** decides what each step sees. This is the field people get wrong, so here it is plainly:

- **`shared`**: each step sees the results of the steps before it. The group accumulates context as it goes. Use this when step 2 builds on step 1.
- **`isolated`**: each step starts fresh, seeing only the call state, not the earlier steps. Use this when the steps are independent.

**`then`** is what happens when the last step finishes:

- **`return`**: control comes back to the agent that called the group, with the results. The conversation continues.
- **`transfer`**: hand off to another agent (you then add `then_target: <agent>`). The group does not come back.
- **`end`**: end the call.

**`merge: results`** means the calling agent receives the steps' typed results and nothing else. The tasks' own conversation turns are not poured back into the agent's context; only the structured data comes back. It is the only value, and it keeps the handback clean.

One difference from a single task: a group delegate has no `assign`. Single tasks assign their result into a variable; a group's step results travel through the group's own context instead.

## What Pipecat generates

The group runs as a **Pipecat Flow** on the agent that delegated it, the same way a single task does ([05](05-tasks.md)), just with more than one step. The delegate method saves the agent's context and starts the flow at the first step:

```python
@tool()
async def run_triage(self, params: FunctionCallParams):
    """Run the triage steps before helping the caller."""
    self._run_triage_results = {}
    self._run_triage_snapshot = (
        [dict(m) for m in self.context.get_messages()], self.context.tools
    )
    flow = FlowManager(
        llm=self.llm,
        context_aggregator=LLMContextAggregatorPair(self.context),
        worker=self,
    )
    await flow.initialize(self._run_triage_node_collect())
    return {"status": "running the triage flow"}
```

The ordering lives in the `finish` functions: each step's handler records its result, then returns the *next* step's configuration, so the agent walks the chain one step at a time:

```python
async def _run_triage_finish_collect(self, args, flow_manager):
    self._run_triage_results["collect"] = dict(args)
    return {"status": "ok"}, self._run_triage_node_classify()   # step 2 comes next
```

`context_scope` shows up on each step's configuration. Every step sets its task
instructions as the current `role_message`. A `shared` group lets every step
see the conversation so far, including the earlier steps' turns. An `isolated`
group starts each step from a clean context:

```python
role_message=STEP_PROMPT,
task_messages=[{"role": "developer", "content": "Begin this step."}],
context_strategy=ContextStrategyConfig(strategy=ContextStrategy.RESET),   # isolated only
```

The last step's `finish` does the `then`. For `then: return` it restores the
agent's saved system instruction, messages, and tools, then hands back only the
typed results. For `then: transfer` it restores the owner before activating the
target worker. For `then: end` it ends the call. You never write any of this;
it follows from the four fields above. LiveKit compiles the same YAML to
different machinery with the same behavior; see
[how targets run your agent](../concepts/how-targets-run-your-agent.md).

## What just got harder

Groups are where the last portability edges show up:

- **`then: transfer` and `then: end` pass on all four platforms** (LiveKit prints a warning that task groups are experimental).
- **`then: return` fails on Vapi.** A state-preserving return into a Vapi squad is not proven. It works on the others, and warns on LiveKit.
- **`context_scope: isolated` is code-targets only.** It compiles on LiveKit, Pipecat, and Deepgram. Managed targets cannot express it.
- **On Pipecat, everything on this page is emitted:** ordered steps, both scopes, and all three `then` outcomes.

## You now have a complex agent

Step back and look at what the package does now. Two agents with their own models and voices, a handoff between them with a guard, webhook tools, typed shared state, a delegated task with a typed result, and an ordered task group. That is a genuinely complex voice agent, described in a handful of readable files, and every piece of it runs on Pipecat.

For when to choose a task group over a single task, a handoff, or just one agent, see [why tasks and task groups](../concepts/why-tasks-and-task-groups.md).

To see exactly what all of it compiles into, and how to deploy it, read the [Pipecat target page](../targets/pipecat.md).

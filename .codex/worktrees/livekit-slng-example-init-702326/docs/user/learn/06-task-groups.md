# 06. Task groups

One task does one thing. A **task group** runs several tasks in a fixed order, then decides what happens when they finish. It is still tier T1, and it is the most structure Unmute gives you in this version.

The one rule to keep in mind, from [tiers](../concepts/tiers.md): a group is an **ordered list, not a graph.** Steps run one after another, front to back. No branches, no loops, no going back. If you need a decision between paths, that is a handoff to another agent, not a group.

## Declare a group

A group lists the tasks it runs as steps, and says what to do at the end:

```yaml
task_groups:
  triage:
    steps: [collect]
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

The group becomes a method that runs each step's job in order, then does the `then`. For our `triage` group (one step, isolated, then return):

```python
@tool()
async def run_triage(self, params: FunctionCallParams):
    """Run the triage steps before helping the caller."""
    results: dict = {}
    payload = dict(asdict(STATE))
    async with self.job("collect", name="collect", payload=payload) as job_ctx:
        results["collect"] = job_ctx.response
    await params.result_callback(results)
```

With more than one step, the ordering and the `context_scope` show up directly. A `shared` group threads each step's result into the next step's input:

```python
# shared: the payload grows as steps complete
payload = dict(asdict(STATE))
async with self.job("collect", ...) as job_ctx:
    results["collect"] = job_ctx.response
payload = {**payload, "collect": results["collect"]}   # step 2 sees step 1
async with self.job("classify", ...) as job_ctx:
    results["classify"] = job_ctx.response
await params.result_callback(results)
```

An `isolated` group omits that middle line, so every step gets the same starting `payload`. And `then: transfer` or `then: end` replace the final `result_callback` with a worker activation or an end-of-call frame. You never write any of this; it follows from the four fields above.

## What just got harder

Groups are where the last portability edges show up:

- **`then: transfer` and `then: end` pass on all five platforms** (LiveKit prints a warning that task groups are experimental).
- **`then: return` fails on Vapi.** A state-preserving return into a Vapi squad is not proven. It works on the others, and warns on LiveKit.
- **`context_scope: isolated` is code-targets only.** It compiles on LiveKit, Pipecat, and Deepgram. Managed targets cannot express it.
- **On Pipecat, everything on this page is emitted:** ordered steps, both scopes, and all three `then` outcomes.

## You now have a complex agent

Step back and look at what the package does now. Two agents with their own models and voices, a handoff between them with a guard, webhook tools, typed shared state, a delegated task with a typed result, and an ordered task group. That is a genuinely complex voice agent, described in a handful of readable files, and every piece of it runs on Pipecat.

To see exactly what all of it compiles into, and how to deploy it, read the [Pipecat target page](../targets/pipecat.md).

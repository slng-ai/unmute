# 05. Tasks

Sometimes an agent needs a small, focused piece of work done: read the conversation so far and pull out a structured answer, without the caller noticing a change of speaker. A **task** does exactly that. The agent **delegates** the work, the task runs, produces a **typed result**, and hands control straight back to the agent. This is **tier T1**.

A task is different from a handoff. A handoff ([04](04-two-agents.md)) moves the conversation to another agent and stays there. A task runs quietly and returns; the same agent keeps talking to the caller.

## Declare the task

Tasks are declared in a `tasks:` block in `agent.yaml`, with their prompts in a `tasks/` folder:

```yaml
tasks:
  collect:
    instructions: tasks/collect.md
    model: careful_reasoning        # optional: run this task on a specific model
    result:
      verified_flag: boolean
      tier: { enum: [free, pro] }
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

**`instructions`** is the task's own prompt. The task is a fresh worker with this prompt, not the calling agent.

**`model`** is optional. Leave it out and the task uses the entry agent's model. Set it (here `careful_reasoning`) to run the task on a specific model, for example a stronger one for a careful extraction. This is a per-task override.

**`result`** is the typed answer the task must return. It is a flat map of name to type. Each field is one of the four primitives (`string`, `number`, `boolean`, `integer`) or an enum written `{ enum: [a, b] }`. Here the task must return a boolean `verified_flag` and a `tier` that is either `"free"` or `"pro"`. The result is the whole point of a task: structured data, not a chat reply.

**`context`** says what the task can see. `history: full` gives it the whole conversation so far. (A task uses the shared call state automatically, so unlike a transfer it has no `variables` field to set.)

**`when`** on the delegate is the trigger the model reads, same as on a transfer.

**`assign`** maps result fields into your variables: `verified: result.verified_flag` writes the task's `verified_flag` into the shared `verified` variable. This is the main way a variable changes mid-call. The field must exist in the task's `result` and its type must match the variable. (An enum field assigns into a `string` variable.)

## What Pipecat generates

The task becomes a separate worker that runs one structured completion and returns the typed result:

```python
class CollectTask(BaseWorker):
    @job(name="collect")
    async def run(self, message: BusJobRequestMessage):
        client = AsyncOpenAI(api_key=os.environ["OPENAI_API_KEY"])
        completion = await client.chat.completions.create(
            model="gpt-4o",
            messages=[
                {"role": "system", "content": "Read the conversation so far..."},
                {"role": "user", "content": json.dumps(message.payload or {})},
            ],
            response_format={"json_schema": {"name": "result", "strict": True, "schema": {
                "type": "object",
                "properties": {"tier": {"enum": ["free", "pro"], "type": "string"},
                               "verified_flag": {"type": "boolean"}},
                "required": ["tier", "verified_flag"], "additionalProperties": False}}},
        )
        result = json.loads(completion.choices[0].message.content)
        await self.send_job_response(message.job_id, result)
```

Your `result` map became a strict JSON schema the model must fill, so the answer really is typed. The delegate method dispatches this job with the current state as its input, then writes the result back:

```python
@tool()
async def run_collect(self, params: FunctionCallParams):
    """Collect the caller's account details."""
    async with self.job("collect", name="collect", payload=asdict(STATE)) as job_ctx:
        result = job_ctx.response
    STATE.verified = result["verified_flag"]
    await params.result_callback(result)
```

The `STATE.verified = result["verified_flag"]` line is your `assign` in action.

## What just got harder

Tasks are the first tier where a whole platform drops out:

- **A task fails on Vapi.** It is not proven that a Vapi handoff can return to the calling assistant, so single-task delegates are blocked there.
- **A task is conditional on ElevenLabs.** It works only as a workflow node, only with `history: full`, and the `assign` must route through a tool.
- **A task works on LiveKit, Deepgram, and Pipecat.** On Pipecat everything on this page is emitted: the task worker, the per-task model, the typed result, and the assign.

So a task is portable across the code targets and one managed target, but not Vapi. On Pipecat, no limits here.

Next: [06. Task groups](06-task-groups.md), which run several tasks in order and decide what happens when they finish.

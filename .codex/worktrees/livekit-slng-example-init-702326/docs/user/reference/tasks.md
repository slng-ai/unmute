# Reference: tasks and task_groups

Tasks and task groups are **tier T1**: delegated work that runs, produces a typed result, and hands control back. They are declared in `agent.yaml` and reached through a `delegate` [control](controls.md). See the learn pages for [tasks](../learn/05-tasks.md) and [task groups](../learn/06-task-groups.md).

Task groups are never lowered to Vapi Workflows (those retire, so the schema does not depend on them).

## tasks

A `task` is delegate-and-return: control comes back to the owning agent with a typed result.

```yaml
tasks:
  collect:
    instructions: tasks/collect.md
    model: careful_reasoning
    result:
      verified_flag: boolean
      tier: { enum: [free, pro] }
    context:
      history: full
```

Tier support for a single task across the five:

| Target | What happens | Tag |
|---|---|---|
| LiveKit | native (`AgentTask`) | gated (T1) |
| Pipecat | works (a task worker running a structured completion) | gated (T1) |
| Vapi | fails: returning to the previous assistant is unverified | gated (T1) |
| ElevenLabs | conditional: a workflow node, `assign` via a tool, `history: full` only | gated (T1) |
| Deepgram | generated, works | gated (T1) |

### instructions

Path to the task's own prompt. The task runs as a fresh worker with this prompt, not the calling agent.

Required: yes. Values: a path. Default: none.

### tools

Tools the task may call.

Required: no. Values: a list of names. Default: none.

### model

A per-task model override. Omit it and the task uses the entry agent's model.

Required: no. Values: a model profile name. Default: the entry agent's model. Tag: gated. On Pipecat it lowers through per-node model switching, both standalone and inside a task group step.

### result

The typed answer the task must return. A flat map of name to type.

Required: yes. Values: a flat map, each field one of `string | number | boolean | integer | { enum: [a, b] }`. Default: none. Tag: core shape. Nested schemas are allowed only when every configured target is a code target.

### context

What the task can see. It is the transfer context block **without** `variables` (a task shares the call state automatically), with `history` required.

Required: yes. Values: see [controls](controls.md) history values. Default: none. Tag: gated.

## task_groups

A `task_group` is an ordered list of steps. No edges, no cycles.

```yaml
task_groups:
  triage:
    steps: [collect]
    context_scope: isolated
    then: return
    merge: results
```

### steps

The ordered task names to run, front to back.

Required: yes. Values: a non-empty ordered list of task names. Default: none.

### context_scope

What each step sees.

Required: yes. Values: `shared | isolated`. Default: none.

| Target | What happens | Tag |
|---|---|---|
| LiveKit | `isolated` compiles (as standalone AgentTasks, not TaskGroup) | gated |
| Pipecat | both `shared` and `isolated` are emitted | gated |
| Vapi | `isolated` cannot be expressed | gated |
| ElevenLabs | `isolated` cannot be expressed | gated |
| Deepgram | both compile (code target) | gated |

`shared` means each step sees the group context as `history: full`; `isolated` means each step enters reset. The group's scope overrides the member tasks' own `context` while inside the group.

### then

What happens when the last step finishes.

Required: yes. Values: `return | transfer | end`. Default: none.

| Target | What happens | Tag |
|---|---|---|
| LiveKit | works; TaskGroup is flagged experimental (warning) | gated |
| Pipecat | all three emitted | gated |
| Vapi | `return` fails (state-preserving return unverified); `transfer`/`end` work | gated |
| ElevenLabs | works | gated |
| Deepgram | works | gated |

### then_target

The agent to transfer to. Required only when `then: transfer`.

Required: conditional (iff `then: transfer`). Values: an agent name. Default: none.

### merge

On completion the owner receives the steps' typed results and nothing else; the tasks' conversation turns are not appended to the owner's context.

Required: no. Values: `results` (the only value). Default: none. Tag: core. A group delegate has no `assign`; step results travel through the group's shared context instead.

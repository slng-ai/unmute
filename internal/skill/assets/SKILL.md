---
name: unmute
description: Creates, maintains, validates, compiles, and runs voice-agent packages with the Unmute CLI. Use for voice or phone agents, existing Unmute packages, agent.yaml, targets.yaml, connections/*.yaml, tools/*.yaml, or unmute init, validate, compile, and dev.
metadata:
  unmute_version: "{{unmute_version}}"
---

# Build voice agents with Unmute

Unmute compiles one declarative package into native LiveKit Agents or Pipecat
projects. Author the package; do not hand-write framework Python or edit
generated `build/` files.

## Start with one reference

Open the first reference that matches the task. Load another only when the work
reaches it.

| Reference | Open it when |
|---|---|
| `references/package.md` | writing `agent.yaml`, `targets.yaml`, connections, or package files |
| `references/workflow.md` | running a command or fixing its output |
| `references/models.md` | choosing listening, speaking, reasoning, or turn models |
| `references/prompting.md` | writing prompts, greetings, tasks, or tool descriptions |
| `references/tools.md` | calling an API, Python, MCP, or a builtin |
| `references/orchestration.md` | the brief has phases, order, roles, permissions, or a next step |
| `references/variables.md` | values, secrets, destinations, templates, or task assignment |
| `references/conversation.md` | greeting, interruption, inactivity, turn taking, or call limits |
| `references/latency.md` | the brief is make it faster, it feels slow, or optimize the agent |
| `references/telephony.md` | answering or placing a phone call |
| `references/transfers.md` | sending a phone caller to a person |
| `references/deploy.md` | moving a checked package into production |
| `references/examples.md` | starting from the closest working package |

## Choose the structure before files

Read the whole brief first. If it names **required order**, **separate roles**
or permissions, or a server's **next step**, open
`references/orchestration.md`. Choose the smallest native shape and tell the
user what you chose.

**Define each tool once.** Its contract and execution block live only in
`tools/<name>.yaml`; every `tools:` list in `agent.yaml` contains names only.
An agent or task list may also name a control. Tasks may use only
`agent_transfer`; validation rejects `delegate` and `human_transfer` there.

**Task `result:` and tool `output:` are different contracts.** Shape a task
result for its caller instead of copying a tool output.

Every task, including a task inside a group, needs a non-empty `result:` and `context.history`.

Use block-style YAML sequences in assistant-authored packages. Do not use anchors or aliases.

## When the package already exists

Run these steps in order:

1. **Inspect the existing package.** Read `agent.yaml`, `targets.yaml`, named
   connections, loaded tool YAML and local handlers, and every used prompt.
2. **Run `unmute validate` before editing.** Record errors and warnings.
3. **Fix invalid definitions.** Make the current package legal first.
4. **Simplify.** Keep the smallest shape that still meets the brief.
5. **Run `unmute validate` again.** Fix errors and report warnings.
6. **Run `unmute compile`.** Regenerate `build/` from the package.

## The build loop

For a new package, start with `unmute init <name>` and edit what it creates.
For every change:

1. Write the package.
2. Run `unmute validate`; read the exact error and fix the package, not the refusal.
3. Run `unmute compile` when validation is clean.
4. Run `unmute dev` and talk to the agent.

Repeat validation until clean. If commands cannot run, give the exact package
path and commands and ask for their output. If audio cannot be heard, run the
other checks and say that the agent still needs a listening test. Never claim a
package works because files were written.

## Hard rules

- The CLI wins when a reference and validation disagree.
- Never edit `build/`; change the package and compile again.
- Never write a key, token, URL credential, or phone number value into the
  package. Write UPPER_SNAKE environment names and tell the user where to set
  values. Secrets never use `{{templates}}`.

## Finish clearly

Tell the user:

1. the target and smallest structure chosen;
2. every model bound by role;
3. what context crosses each task, group, or handoff;
4. what was validated, compiled, heard, or left for them to check.

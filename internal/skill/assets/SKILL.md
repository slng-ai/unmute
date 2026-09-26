---
name: unmute
description: Creates, maintains, validates, compiles, and runs voice-agent packages with the Unmute CLI. Use for voice or phone agents, existing Unmute packages, agent.yaml, targets.yaml, connections/*.yaml, tools/*.yaml, or unmute init, validate, compile, and dev.
metadata:
  unmute_version: "{{unmute_version}}"
---

# Build voice agents with Unmute

Unmute compiles one declarative package into native LiveKit Agents or Pipecat projects. Author the package; do not hand-write framework Python or edit generated `build/` files.

## Start with one reference

Open the first matching reference; load another only when needed.

| Reference | Open it when |
|---|---|
| `references/package.md` | writing `agent.yaml`, `targets.yaml`, connections, or package files |
| `references/workflow.md` | running a command or fixing its output |
| `references/manifests.md` | creating a saved manifest, choosing its default, or following an agent's contract |
| `references/models.md` | choosing listening, speaking, reasoning, or turn models |
| `references/prompting.md` | writing prompts, greetings, tasks, or tool descriptions |
| `references/tools.md` | calling an API, Python, MCP, or a builtin |
| `references/orchestration.md` | the brief has phases, order, roles, permissions, or a next step |
| `references/variables.md` | values, secrets, destinations, templates, or task assignment |
| `references/conversation.md` | greeting, interruption, inactivity, turn taking, or call limits |
| `references/latency.md` | the brief is make it faster, it feels slow, or optimize the agent |
| `references/telephony.md` | answering or placing a phone call |
| `references/transfers.md` | sending a phone caller to a person |
| `references/deploy.md` | hosting a compiled livekit or pipecat project; an slng push is the `unmute-deploy` skill; a twilio app is "The twilio target" in `references/package.md` |
| `references/examples.md` | starting from the closest working package |

## Choose the structure before files

Read the whole brief first. If it names **required order**, **separate roles** or permissions, or a server's **next step**, open `references/orchestration.md`.
Choose the smallest native shape and tell the user what you chose.

**Two steps in a row are one task group, not two tasks with a `when:` each.** Smallest is not fewest keys: every task an agent can choose between costs a model request to choose it, and that request speaks nothing and does nothing. A group is entered once and the steps after the first cost no request to reach.

**Keep state small.** Keep only values needed across a task or handoff, by a later tool, or as prompt facts. Prefer one timestamp to separate date and time values; `references/variables.md` has the example.

**Every agent-level list attaches something already declared, except `tasks:`, written where it runs.** Five kinds: `tools:`, `tasks:` and `task_groups:` come back, `handoffs:` and `escalations:` do not. No `kind:` field, and all five share one namespace. `references/orchestration.md` has the table.
**Define each tool once.** Its contract lives in `tools/<name>.yaml`; `tools:` lists hold names only.

**Task `assign:` and tool `output:` are different contracts.** Task finish fields come from destination variables; do not repeat their types.
**A step that ends on a tool result should say so.** `finish:` names the tools that end it and what a successful result looks like, so it saves and moves on with no model request in between. `skip_when_confirmed:` skips a group step whose confirmation holds; `opening: listen` speaks one fixed question and waits. Code targets only; `references/orchestration.md` has the rules.

Omitted task and handoff history means `messages`: spoken turns without tool records. With `reset`, name each needed saved value in the receiver's prompt as `{{variable}}`.

Use block-style YAML sequences in assistant-authored packages. Do not use anchors or aliases.

## When the package already exists

Run these steps in order:

1. **Inspect the existing package.** Read `agent.yaml`, its linked `manifest.yaml`, `targets.yaml`, named connections, loaded tool YAML and local handlers, and every used prompt.
2. **Run `unmute validate` before editing.** Record errors and warnings.
3. **Fix invalid definitions.** Make the current package legal first.
4. **Simplify.** Keep the smallest shape that still meets the brief.
5. **Run `unmute validate` again.** Fix errors and report warnings.
6. **Run `unmute compile`.** Regenerate `build/` from the package.

## The build loop

`unmute init <agent>` writes the starter package with no questions. A company contract reaches a package only when a person asks for one with `unmute init <agent> --from-manifest`, which is an interactive picker: you cannot create a governed package yourself, so ask the user to run it. Asked to write the company rules themselves rather than an agent, use the `unmute-manifest` skill.
A package holding a `manifest.yaml` file is company-governed. Read that file before choosing any bindings, and follow `references/manifests.md`.
Choose exact approved model IDs when listed; provider-wide approval does not establish target or provider support. SLNG stays provider `slng` even when IDs name other model makers.
Preserve the contract and its link. Explain conflicts instead of weakening company rules.
Refresh this workflow with `unmute skill install` after updating the CLI; review local edits before using `--force`.

For every change:

1. Write the package.
2. Run `unmute validate`; read the exact error and fix the package, not the refusal.
3. Run `unmute compile` when validation is clean.
4. Run `unmute dev` and talk to the agent.
5. For a slng target only, the `unmute-deploy` skill pushes it: a push replaces the live agent rather than merging with it.

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

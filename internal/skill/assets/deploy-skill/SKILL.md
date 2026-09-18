---
name: unmute-deploy
description: Pushes a finished Unmute package to SLNG and reads what landed. Use when asked to deploy, push, ship, release or go live with an agent, when a push was refused, when somebody asks what a push will overwrite or detach, or when they say the deploy did not land.
metadata:
  unmute_version: "{{unmute_version}}"
---

# Push a package to SLNG

SLNG hosts the agent, so there is no container and no platform step: the push
replaces a live agent with what the package declares. This skill is that push.
`references/slng-push.md` holds the mechanics, the refusals and what each one
means.

**A livekit or pipecat target is a different job.** Those compile to a Python
project you host yourself, and nothing here applies. They are in the `unmute`
skill, under `.agents/skills/unmute/references/deploy.md`.

## The loop

Four commands, in this order.

```sh
unmute validate .                          # the package, against its manifest and the schema
unmute deploy . --dry-run                  # the account: hosted names, injected arguments, vault entries
unmute compile .                           # writes build/slng/agent.json
voiceai agents push build/slng --dry-run   # tool ids, versions, and the attachment diff
voiceai agents push build/slng
voiceai agents get <id> --json             # what actually landed
```

`unmute deploy .` does compile and push in one command, and it is the right
command for most packages. Go the long way when `unmute deploy --dry-run`
refuses a curated capability your organisation really has: that refusal is the
builtin name rule in `references/slng-push.md`, and naming the tool file after
the capability fixes it properly.

Run `unmute deploy --dry-run` either way. It is the only check that resolves
vault entries and holds each injected argument against the published tool's
parameters, so skipping it lets a bad `inject:` key reach the platform.

## The two dry runs check different things

|  | `unmute deploy --dry-run` | `voiceai agents push --dry-run` |
|---|---|---|
| manifest rules | yes | no |
| hosted tool exists | yes | yes |
| an injected argument is a real parameter | yes | no |
| vault entries the tool needs | yes | no |
| resolves `tool_id` and version | no | yes |
| attachment diff, new against reused | no | yes |
| **what a replace will detach** | no | **yes** |
| what a replace will overwrite | no | yes |

Neither is a superset of the other. Run both.

## Read the detach list

`voiceai agents push --dry-run` prints the attachments it would create or reuse,
then what a replace would remove. That removal list is the only place an author
learns what the live agent has that the package does not: an attachment somebody
added in the dashboard by hand, or one an earlier version of the package
declared. Read it out to the user before the real push. Nothing else in either
tool reports it.

## What a push replaces, and what it leaves alone

A push replaces rather than merges, but only over what a package can express.
These fourteen fields are the whole compiled body, so every one of them is
overwritten on every push:

```text
enable_interruptions, greeting, language, mcp_refs, models, name, region,
runtime_variables, schema_version, system_prompt, template_defaults,
template_variable_options, tool_mode, tool_refs
```

Everything else on the live agent is not expressible in a package and survives
untouched. Observed across four pushes on 2026-09-17:

```text
idle_nudges, noise_cancellation_enabled, tts_cache_enabled, inbound_greeting,
outbound_greeting, sip_inbound_trunk_id, sip_outbound_trunk_id, orchestrator,
livekit_deployment
```

So the honest answer to "will I lose my dashboard settings" is: dashboard edits
to the prompt, greeting, models, region, interruptions or tools are lost on the
next push; idle nudges, noise cancellation, TTS cache and trunk bindings are
not. A package being refused a setting at validate does not mean the agent runs
without it. It runs on SLNG's default, and the dashboard is the only place to
change that.

## The deployed name is not the package name

`name: acme-support` in `agent.yaml` deploys as `acme-support-slng`: the package
name joined to the target name. `voiceai agents list` and every `--agent-id`
lookup use the joined name. Say the deployed name when you report a push, or the
user searches the dashboard for a name that is not there.

## When somebody says the push did not land

Check in this order, before touching the package.

1. `voiceai agents get <id> --json`, and read `updated_at`. Newer than the push
   means it landed and the dashboard is showing a cached page.
2. Search the live `system_prompt` for a string that exists only in the new
   version. This is the only proof that content changed rather than a version
   row.
3. Only then read the push output again.

A push prints a version and says nothing about content, so "version 7, pushed"
is not evidence and must never be reported as if it were.

## Finish clearly

Tell the user the deployed name and id, what the push detached or overwrote,
anything the dry run warned about, and what still needs doing by hand in the
dashboard.

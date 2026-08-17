---
name: unmute
description: Creates, maintains, validates, compiles, and runs voice-agent packages with the Unmute CLI. Use for voice or phone agents, existing Unmute packages, agent.yaml, targets.yaml, connections/*.yaml, tools/*.yaml, or unmute init, validate, compile, and dev.
metadata:
  unmute_version: "{{unmute_version}}"
---

# Building voice agents with Unmute

**The instructions live in `.agents/skills/unmute/`. Read
`.agents/skills/unmute/SKILL.md` first, before doing anything else.**

That file is the entry document: what Unmute is, which reference to open for the
task in front of you, the build loop, and the decisions you have to say out
loud. Its `references/` directory holds one file per area.

There is one copy on purpose. Several assistants read this project and they all
read the same text, so nothing can drift between them. This file exists only
because Claude Code reads `.claude/skills/` and the others read
`.agents/skills/`.

If `.agents/skills/unmute/SKILL.md` is missing, the install was partial. Run:

```sh
unmute skill install
```

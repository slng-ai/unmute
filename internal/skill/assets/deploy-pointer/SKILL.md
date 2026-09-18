---
name: unmute-deploy
description: Pushes a finished Unmute package to SLNG and reads what landed. Use when asked to deploy, push, ship, release or go live with an agent, when a push was refused, when somebody asks what a push will overwrite or detach, or when they say the deploy did not land.
metadata:
  unmute_version: "{{unmute_version}}"
---

# Deploying an Unmute package

**The instructions live in `.agents/skills/unmute-deploy/`. Read
`.agents/skills/unmute-deploy/SKILL.md` first, before doing anything else.**

That file is the entry document: the command loop, what the two dry runs each
check, what a push replaces and what it leaves alone, and how to tell whether a
push landed. `references/slng-push.md` beside it holds the mechanics.

There is one copy on purpose. Several assistants read this project and they all
read the same text, so nothing can drift between them. This file exists only
because Claude Code reads `.claude/skills/` and the others read
`.agents/skills/`.

If `.agents/skills/unmute-deploy/SKILL.md` is missing, the install was partial.
Run:

```sh
unmute skill install
```

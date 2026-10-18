---
name: unmute-migrate
description: Migrates an Unmute package written for unmute 0.8 or older to the current agent.yaml contract. Use when unmute says "version: is gone", when validate refuses a key as renamed or moved, when an agent.yaml has a version: line, or when asked to upgrade, migrate or port an Unmute package.
metadata:
  unmute_version: "{{unmute_version}}"
---

# Migrating an Unmute package

**The instructions live in `.agents/skills/unmute-migrate/`. Read
`.agents/skills/unmute-migrate/SKILL.md` first, before doing anything else.**

That file is the entry document: the loop, and what to check when it is done.
`references/old-to-new.md` beside it maps every old key to its new form.

There is one copy on purpose. Several assistants read this project and they all
read the same text, so nothing can drift between them. This file exists only
because Claude Code reads `.claude/skills/` and the others read
`.agents/skills/`.

If `.agents/skills/unmute-migrate/SKILL.md` is missing, the install was partial.
Run:

```sh
unmute skill install
```

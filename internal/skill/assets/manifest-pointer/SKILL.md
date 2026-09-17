---
name: unmute-manifest
description: Interviews somebody about their company's approved voice-agent choices and saves the result as an Unmute manifest. Use when asked to create, draft, write or set up a manifest, organization contract, company rules, allowed models, approved providers, or when somebody says new agents must stay within company policy.
metadata:
  unmute_version: "{{unmute_version}}"
---

# Write a company manifest

**The instructions live in `.agents/skills/unmute-manifest/`. Read
`.agents/skills/unmute-manifest/SKILL.md` first, before doing anything else.**

That file is the whole skill: the questions to ask, the file to write, and the
command that validates and saves it.

There is one copy on purpose. Several assistants read this project and they all
read the same text, so nothing can drift between them. This file exists only
because Claude Code reads `.claude/skills/` and the others read
`.agents/skills/`.

If `.agents/skills/unmute-manifest/SKILL.md` is missing, the install was
partial. Run:

```sh
unmute skill install
```

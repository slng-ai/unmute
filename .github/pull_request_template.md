<!--
Thanks for contributing. CONTRIBUTING.md has the long version of everything
below: https://github.com/slng-ai/unmute/blob/main/CONTRIBUTING.md

A pull request needs five things: the issue it answers, an example that uses
the feature, a video of that example working, a README for it, and a docs page
explaining the logic.

Stuck on any of them? Ask on Discord: https://discord.gg/kxZactmWj
-->

## What's in

<!-- The shape an author writes, in a fence, and one paragraph on what it does.
     What was wrong before, if it takes a line. -->

## How it works

<!-- Five to ten lines. The decisions somebody would otherwise ask about. -->

## The issue

Closes #

## The example that uses it

<!-- Name the package in this branch that uses the feature, and the commands
     to run it. Say where the new key appears and where the code path runs. If
     the change has no example, say why here. -->

```sh
unmute validate examples/<package>
unmute compile examples/<package>
```

Where the feature fires:

What to listen for:

## Video

<!-- Drag the recording in. Say which target and which route, and drive the
     agent to the moment the feature fires. -->

## Docs

<!-- Which pages you wrote or changed, and where a reader now learns what this
     does, when to use it, what it compiles to on each target, and what
     happens if they leave it out. -->

## Before you ask for review

- [ ] An issue exists for this, and it is linked above
- [ ] I searched the open issues first, and this is not a duplicate
- [ ] An example in this branch **uses** the feature: the new key is in its authored files and the code path runs on a call
- [ ] That example validates and compiles on every target the feature claims
- [ ] A video of it working is attached
- [ ] That example has a README saying which part of it is the feature, and what to listen for
- [ ] The `docs-site/` page explains the logic, not only the key name
- [ ] `make fmt`, `make test`, `make lint` and `ruff check .` are clean
- [ ] Nothing pasted here holds an API key, a real phone number or customer data
- [ ] If this changes what the compiler emits: the runbook template, the example's README, the `docs-site/` page and the skill are updated in the same commit

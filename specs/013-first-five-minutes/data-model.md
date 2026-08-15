# Phase 1: the reachability graph

This feature adds one concept. Everything else is a fix to an existing rule, so
this document covers only the new thing: **what "reaches" means**, and which
declarations it applies to.

## The rule in one sentence

A package declares things and attaches things. Anything declared and never
attached is either an error, or an explicitly documented palette entry. There is
no third case.

## The graph `ir.Build` already walks

`checkToolRefs` (`internal/ir/build.go:1325`) walks it forwards: for every name
in an agent's `tools:`, prove it resolves to a tool or a control. The new check
walks it backwards.

```
entry_agent ─┬─> agents[*].tools[*] ─┬─> tools[name]        (declared in tools/ or top-level tools:)
             │                       └─> controls[name] ────> destinations[name]
             ├─> agents[*].tasks / task groups ──> (same tools and controls)
             └─> controls[kind: agent_transfer].target ──> agents[name]
```

Roots: the entry agent. Reachable: anything on a path from a root.

## What the check covers

| Declaration | Reachable when | If unreachable |
|---|---|---|
| `controls.<name>` — every kind, not only `human_transfer` | some agent, task, or task group lists it in `tools:` | **error** |
| `destinations.<name>` | some reachable control resolves to it | **error** |
| top-level `tools:` entry | some agent, task, or task group lists it | **error** |
| a task | its agent or group reaches it | **error** |
| a task group | its agent reaches it | **error** |
| an agent | the entry agent reaches it, directly or through an `agent_transfer` chain | **error** |
| `models.<section>.<name>` | anything references it | **legal.** `docs/SCHEMA.md:287` calls the map "a palette: entries that nothing currently references are legal" |
| `connections/<name>.yaml` | some target binds it | **warning**, already implemented at `internal/ir/validate.go:1365` |

The `models:` row is the one carve-out, and its wording is scoped to `models:`
alone. The `connections/` row is the precedent: the repository already made this
exact judgement once, for the one declaration kind where a warning was the right
severity because a package legitimately carries connections for environments it
is not currently building.

## Why unreachable is not merely dead

Three of the rows above cost the author something concrete today, which is why
the severity is an error rather than a warning:

- **An unreferenced destination** puts its environment name into
  `build/<target>/.env.example`, into the generated `REQUIRED_ENV` startup
  check, into the compile report, and into both compose files. The agent then
  refuses to start over a phantom secret nothing will ever read.
- **An unreferenced top-level tool** leaks its `webhook.url_env` the same way.
- **An unattached `agent_transfer`** leaves its unreachable target agent emitted
  as a dead `class` nothing can reach.

## What the check must not do

- It must not fire on an unreferenced `models:` entry.
- It must not fire twice for a package with two targets. This is a property of
  the package, so it belongs in `ir.Build`, which runs once (research D1).
- It must not change the compile report's evidence rows on its own.
  `routeCapabilitiesUsed` (`internal/ir/validate.go:655`) walks `agent.Controls`
  with no attachment filter, so today an unattached control produces a
  `cold_transfer` evidence row while the emitted agent has no such tool. The new
  error removes the package that causes that disagreement; FR-002a is the check
  that it is really gone, not a second fix.

## Error shape

Tier 1 (research D7), so the message carries file and line, formatted by the
existing `missing()` helper's sibling. The contract is in
[contracts/messages.md](./contracts/messages.md).

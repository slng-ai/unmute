---
name: unmute-manifest
description: Interviews somebody about their company's approved voice-agent choices and saves the result as an Unmute manifest. Use when asked to create, draft, write or set up a manifest, organization contract, company rules, allowed models, approved providers, or when somebody says new agents must stay within company policy.
metadata:
  unmute_version: "{{unmute_version}}"
---

# Write a company manifest

A manifest is one YAML file listing what new agents are allowed to use:
providers and models per role, languages, regions, deployment targets, tools and
tracing. `unmute validate` and `unmute compile` enforce it, so a rule written
here is a rule the compiler keeps.

This skill only creates one. Editing a saved manifest is `unmute manifest edit
<name>`, which has a real editor for it.

## Interview first, file second

Ask the questions below in order, in one message each where you can. Never
invent a rule, and never widen one to make a package compile: an omitted rule
allows everything, which is a decision the user makes out loud.

1. **Name.** The local name on this computer, such as `acme-corp`, and the
   company name to write inside the file. They can differ.
2. **Deployment targets.** LiveKit, Pipecat, SLNG, or any of them.
3. **Models, per role.** Listening, reasoning and speaking. For each: which
   providers, and either an exact list of model IDs or every model that provider
   offers. Read `../unmute/references/models.md` for what each target and
   provider actually supports, and offer the user real IDs rather than asking
   them to recall one. Models served through SLNG keep the provider `slng`, even
   when the ID names Deepgram or Cartesia.
4. **Languages**, if they restrict them.
5. **Regions**, if data has to stay somewhere: per model role and provider, and
   per deployment target.
6. **Tools.** Which execution kinds, which tool names, and which builtins.
   `../unmute/references/manifests.md` lists the kinds a contract can allow;
   offer those words rather than inventing one.
7. **Tracing.** Langfuse, Coval, both, or none.

Ask what they want restricted before offering the full list of a section. A
manifest that allows everything in every section is the same as no manifest,
so say that rather than writing one.

## Write it, show it, save it

Write the YAML to a file in the project, such as `manifest-draft.yaml`. Follow
`../unmute/references/manifests.md`: it documents every key, its allowed values
and what an empty `allow` list means. Only `manifest` and `version` are
required.

Show the file and get a yes before saving. This is company policy, so the user
approves the exact text.

```sh
unmute manifest create acme-corp --file manifest-draft.yaml
```

The command validates before it saves. It refuses an unknown key or a bad value
with the line and column, and it writes nothing when it refuses, so fix the
draft and run it again. The first saved manifest becomes the default; a later
one leaves the default alone, and `unmute manifest use <name>` changes it.

Delete the draft file once the command reports `created`. The saved copy is the
one every later command reads, and a draft left in the project is a second copy
of company policy that nothing keeps in step with it. Keep it only if the user
asks, and then say where it is.

## What happens next

A manifest does nothing until somebody creates an agent under it:

```sh
unmute init hotel-agent --from-manifest
```

That picker is interactive, so the user runs it. It copies the contract into
the package as `manifest` and guides the choices the contract allows. From
there the `unmute` skill takes over: it reads the copied file and builds the
agent inside those rules.

Tell the user, in one line each: the local name, which sections you restricted,
and that the contract only reaches a package created with `--from-manifest`.

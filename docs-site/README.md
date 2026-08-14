# docs-site

The public Unmute user documentation, as a Mintlify project. Written for
readers who have never seen this repository.

This file is for contributors. It is not part of the site: `docs.json` decides
what ships, and this file is not in it.

## Preview it

```sh
cd docs-site
mint dev --no-open      # serves http://localhost:3000
mint validate           # configuration and page checks
mint broken-links       # every internal link resolves
```

`mint` needs Node 20 or newer. Pages live as `.mdx` files; the sidebar order is
`navigation.groups` in `docs.json`, and that order is the story the site tells.

## The rules this site is written under

1. **Every fact is checked against the code before it is written.** The Go
   structs in `internal/spec` and `internal/ir` are the schema truth,
   `internal/cli` is the command truth, and
   `internal/target/catalog_*.go` is the provider truth.
2. **Every YAML snippet was run through `./bin/unmute validate`** in a scratch
   package, and every example the site names validates and compiles.
3. **Only two targets exist here**: Pipecat and LiveKit Agents. Vapi and
   Deepgram are never presented as targets or runtimes. ElevenLabs and Deepgram
   appear only as model vendors, where the catalog lists them.
4. **Plain language, short sentences, no em or en dashes as punctuation.**
5. **No page presents a route as more proven than the compile report says.**
   Telephony routes are `provisional`: implemented and compiling, with no
   credentialed test in CI.
6. **When code and an internal doc disagree**, the disagreement goes to the
   maintainers rather than being settled in passing. The disagreements found
   while writing this site are listed in
   `specs/008-mintlify-user-docs/report.md`.
7. **A page ships only if the code has the concept.** A tools page exists because
   the `Tool` struct has that execution block; the Models role pages exist because
   the catalog has those roles. When the code lacks something the plan assumed, the
   page is dropped and the reason goes in the report.
8. **A model or vendor claim comes from the catalog, and an outside claim is
   attributed.** SLNG leads every vendor list it appears in, and only SLNG model
   ids are printed, because those are the ones proven in this repository. Facts
   about the SLNG Execution Layer are SLNG's: quoted, linked, and dated, never
   asserted as measured here. Upstream documents three stages of that layer and
   this site covers two of them: the routing stage between them is deliberately
   not mentioned anywhere here, including in this file, and the sweep that keeps
   it that way greps the whole directory (maintainer decision, 2026-08-14).
9. **No provider-branded environment variable name is used as an invalid
   example.** A name that starts with a digit is the point; whose product it looks
   like is not. The neutral `2FACTOR_*` names are what the site uses.

## The structure

Nine groups, 49 pages: Get started, Build the agent (nested Tools and
Orchestration), Development lifecycle, Telephony, Transfers, Targets, Models,
Deployment, Reference (nested CLI). A nested group is the object-in-pages-array
form. The count of `.mdx` files under `docs-site/` must equal the count of page
entries in `docs.json`.

## Five tests hold the parts that can be held

Prose rots. These facts cannot:

| Test | What it holds |
|---|---|
| `internal/cli/help_capture_test.go` | `specs/008-mintlify-user-docs/help.txt` still matches the cobra tree, and every flag in it appears on the CLI page that documents that command (two tests) |
| `internal/target/providers_docsite_test.go` | `models/{stt,tts,llm}.mdx` list exactly the catalog's vendors per target per role, with SLNG first; and `models/turn-detection.mdx` carries no vendor list, because the `turn` role has no catalog entries (two tests) |
| `internal/spec/tools_docsite_test.go` | `build/tools/overview.mdx` names exactly the execution blocks the `Tool` struct has |

`reference/connections-yaml` deliberately has no test: its three shapes are backed
by two shipped examples and one scratch package, and every telephony example is
validated and compiled by the default suite, so a route change fails there first.

Re-capture the help after an intentional CLI change:

```sh
go test ./internal/cli -run TestHelpCaptureMatchesBinary -update
```

Then update the pages that quote it, or the mapping test fails.

## This site is a fourth place to update

`CLAUDE.md` says a change to emitted behaviour updates three places in the same
commit: the emitted README template, the source example's own `README.md`, and
the relevant page in `docs/`. Once this site is live, it is a fourth. A reader
who lands here is reading the public answer, and it going stale is the same
failure as the other three going stale.

Proposing that amendment to `CLAUDE.md` is a maintainer decision, not something
this feature did on its own.

## Go live checklist

Nothing deploys without the maintainer running `mint login` and approving.

1. **Create a NEW Mintlify project for this site.** Never connect it to, or
   push it over, the existing docs.slng.ai deployment.
2. Give it its own subdomain. Authentication works on a custom domain or a
   `*.mintlify.site` subdomain, never on a custom subpath.
3. **Set visibility to Private in the dashboard before sharing any URL.** Use
   password authentication if the plan is Pro or Enterprise; otherwise use
   Mintlify private authentication, which every plan has, until a password is
   possible.
4. Only then share the link.

Going public later is a deliberate decision, not a default.

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

## Two tests hold the parts that can be held

Prose rots. These two facts cannot:

| Test | What it holds |
|---|---|
| `internal/cli/help_capture_test.go` | `specs/008-mintlify-user-docs/help.txt` still matches the cobra tree, and every flag in it appears on the CLI page that documents that command |
| `internal/target/providers_docsite_test.go` | `reference/providers.mdx` lists exactly the catalog's vendors per target per role, with SLNG first |

Re-capture the help after an intentional CLI change:

```sh
go test ./internal/cli -run TestHelpCaptureMatchesBinary -update
```

Then update the pages that quote it, or the second test fails.

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

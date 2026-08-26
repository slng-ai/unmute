# docs-site

The public Unmute user documentation, as a Mintlify project. Written for
readers who have never seen this repository.

This file is for contributors. It is not part of the site: `docs.json` decides
what ships, and this file is not in it.

## Preview it

```sh
cd docs-site
mint dev --no-open                                      # serves http://localhost:3000
mint validate                                           # configuration and page checks
mint broken-links --check-anchors --check-redirects    # internal links and redirects
mint broken-links --check-external                     # external links, then verify fetch failures
mint a11y                                               # contrast and media alternatives
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
3. **There are three targets**: Pipecat and LiveKit Agents, which generate a
   Python project you run, and SLNG, which is hosted and generates a deployment
   body instead. Those are the only values `provider` accepts. Vapi and Deepgram
   were retired as targets on 2026-08-24; do not reintroduce them. Deepgram and
   ElevenLabs still appear as *model vendors* where the catalog lists them, which
   is a different thing from a target and must not be written as one. `slng` is
   both a target and a model vendor, which is exactly why that distinction
   matters.
4. **Plain language, short sentences, no em or en dashes as punctuation.**
5. **Pages state what the product does and what the CLI prints.** Notes about
   how an author checked the documentation belong in the pull request, not the
   user guide.
6. **This is the only public documentation tree.** `docs/ARCHITECTURE.md`
   explains system boundaries for contributors; it is not a second user guide.
   Public package, target, telephony, transfer, deployment, and CLI guidance
   belongs here.
7. **A page ships only if the code has the concept.** A tools page exists because
   the `Tool` struct has that execution block; the Models role pages exist because
   the catalog has those roles. If the code lacks a concept, do not document it.
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
10. **The version this site describes is stated once, in
    `snippets/unmute-version.mdx`, and never typed into a page.** A page that
    names a version imports `unmuteVersion` and renders it. The release
    automation rewrites that one line when a tag is cut, so a typed literal goes
    stale the moment the next release ships and nothing tells you.
11. **`changelog.mdx` is derived, not written.** Its one source of truth is the
    GitHub Release that GoReleaser publishes, and
    `scripts/render_changelog.py` turns that into an entry. Edit the release on
    GitHub and re-run the script; do not edit an entry by hand, because the next
    render will not know you did. The page's lead paragraph is the exception:
    the script never touches anything above the `{/* changelog:entries */}`
    marker.

## The structure

Eleven top-level groups, 56 pages: Get started, Build the agent, Configuration
files, Develop and test, Tracing, Phone calls, Targets and models, Optimization,
Deploy, CLI, and Releases. Build the agent nests Tools and Orchestration; Phone
calls nests Transfers; Targets and models nests Targets and Models. Releases
holds the single `changelog` page.

Top-level groups are section headers, not clickable roots. Their overview pages
appear first in the group. Nested groups use clickable roots for Tools,
Orchestration, Transfers, Targets, and CLI. The site does not add automatic
directory listings because those roots already have useful hand-written cards
or tables. Every `.mdx` file must appear exactly once as either a page entry or
a nested group root in `docs.json`. `snippets/` is the one exception: Mintlify
never renders a file there as a standalone page, so a snippet has no navigation
entry and the structure test skips that directory.

## Tests hold the parts that can be held

Prose rots. These facts cannot:

| Test | What it holds |
|---|---|
| `internal/docsite/structure_test.go` | top-level groups stay visually separate; every page appears once; every page has unique frontmatter and a valid heading hierarchy |
| `internal/cli/help_capture_test.go` | `internal/cli/testdata/help.txt` still matches the cobra tree, and every flag in it appears on the CLI page that documents that command (two tests) |
| `internal/target/providers_docsite_test.go` | `models/{stt,tts,llm}.mdx` list exactly the catalog's vendors per target per role, with SLNG first; and `models/turn-detection.mdx` carries no vendor list, because the `turn` role has no catalog entries (two tests) |
| `internal/spec/tools_docsite_test.go` | `build/tools/overview.mdx` names exactly the execution blocks the `Tool` struct has |
| `internal/docsite/version_test.go` | `snippets/unmute-version.mdx` holds one valid version, the installation and quickstart pages import it, and no other page hardcodes a version literal (three tests) |
| `internal/docsite/changelog_test.go` | `changelog.mdx` runs newest first, every entry has a label, a version and a link to its own release, the newest entry matches the version snippet, the insert marker survives, and no entry keeps a heading, an em or en dash, or a commit hash (five tests) |
| `internal/skill/markdown_surface_test.go` | `docs.json` declares the contextual menu in order and states the two facts agents get wrong, and `start/coding-agents.mdx` names all three Markdown endpoints and the suffix rule (three tests) |

`reference/connections-yaml` deliberately has no test: its three shapes are backed
by two shipped examples and one scratch package, and every telephony example is
validated and compiled by the default suite, so a route change fails there first.

Re-capture the help after an intentional CLI change:

```sh
go test ./internal/cli -run TestHelpCaptureMatchesBinary -update
```

Then update the pages that quote it, or the mapping test fails.

## This site is one of four maintained product surfaces

A change to emitted behaviour updates the generated README template, the source
example's `README.md`, the relevant page here, and the shipped coding-agent
skill. Do not copy the same guidance into `docs/`.

## Go live checklist

Every step here is done by the maintainer in the Mintlify dashboard. A
deployment is one repository, one content directory, and one branch, so a
second deployment cannot reach the first one's site.

1. **Create a NEW deployment in the existing organization.** Never repoint the
   docs.slng.ai deployment at this repository, and never save this repository
   in its Git Settings. On Pro, each deployment carries its own subscription,
   so check the billing page before creating it.
2. **Point it at this directory.** Git Settings: repository `slng-ai/unmute`,
   branch `main`, then turn on **docs.json is in a subdirectory** and enter
   `/docs-site` with no trailing slash. Saving starts the first build. The
   Mintlify GitHub App must have `unmute` in its repository access list,
   otherwise nothing deploys on push.
3. **Keep the site public.** On the Authentication page, set site visibility
   to Public and save the change. `docs.json` also marks every top-level group
   as public, so accidentally enabling authentication does not hide the docs.
4. **Add the custom domain in this deployment's own Custom domain page.** Add
   the two verification `TXT` records first and wait for both to show a green
   check, then switch the `CNAME`. Switching the `CNAME` early breaks HTTPS
   until the certificate finishes. Use the exact values the dashboard prints,
   not values copied from a doc page.
5. Set `seo.metatags.canonical` in `docs.json` to the live domain, so the
   custom domain and the `*.mintlify.site` URL do not compete in search.
6. Only then share the link.

If any CLI command is ever run against this site, run
`mint config set subdomain <unmute-subdomain>` first. The CLI defaults to the
first subdomain on the account, and `mint add-domain` uses whatever is
configured, which is how a command meant for this site lands on docs.slng.ai.

After step 2 the site is not gated behind a human any more: a merge into `main`
that touches `docs-site/` deploys on its own. Pull requests get a Mintlify
status check and, on Pro, a preview deployment, so a broken `docs.json` shows
up before the merge rather than on the live site.

Public access is deliberate: do not enable authentication for this deployment.

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
3. **Only two targets exist here**: Pipecat and LiveKit Agents. Vapi and
   Deepgram are never presented as targets or runtimes. ElevenLabs and Deepgram
   appear only as model vendors, where the catalog lists them.
4. **Plain language, short sentences, no em or en dashes as punctuation.**
5. **No page presents a route as more proven than the compile report says.**
   Telephony routes are `provisional`: implemented and compiling, with no
   credentialed test in CI.
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
   asserted as measured here. The Context Router page documents the separate
   reasoning optimization path and makes clear that the upstream provider and
   model remain the user's choice.
9. **No provider-branded environment variable name is used as an invalid
   example.** A name that starts with a digit is the point; whose product it looks
   like is not. The neutral `2FACTOR_*` names are what the site uses.

## The structure

Seven top-level groups, 51 pages: Get started, Build the agent, Develop and
test, Phone calls, Runtimes and models, Deploy, and Reference. Build the agent
nests Tools and Orchestration; Phone calls nests Transfers; Runtimes and models
nests Runtimes and Models; Reference nests CLI.

Existing overview pages are clickable group roots for Get started, Tools,
Orchestration, Develop and test, Phone calls, Transfers, Runtimes, and CLI. The
site does not add automatic directory listings because those roots already have
useful hand-written cards or tables. Every `.mdx` file must appear exactly once
as either a page entry or a group root in `docs.json`.

## Tests hold the parts that can be held

Prose rots. These facts cannot:

| Test | What it holds |
|---|---|
| `internal/cli/help_capture_test.go` | `internal/cli/testdata/help.txt` still matches the cobra tree, and every flag in it appears on the CLI page that documents that command (two tests) |
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
3. **Set visibility to Private before sharing any URL.** Authentication page,
   visibility Private, then either Authenticated (Mintlify organization login,
   on every plan) or password (Pro and Enterprise). Authentication works on a
   custom domain or a `*.mintlify.site` subdomain, never on a custom subpath.
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

Going public later is a deliberate decision, not a default.

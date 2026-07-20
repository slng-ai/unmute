# SPEC — docs site (localhost guide viewer)

Scope: browse the user-facing guides under `docs/user/**` as a rendered site on localhost. Quick and dirty, open source, zero build step. Tool: [docsify v5](https://docsify.js.org) — renders raw markdown in the browser, no generated output, no site pipeline (verified against docsify docs 2026-07-20).

Internal engineering specs (`docs/spec/*.md`) are **out of the site** — docsify serves only `docs/user/`, so the specs are never injected. They stay where they are; no move, no broken cross-links.

## §G goal
One static `docs/user/index.html` + one `docs/user/_sidebar.md` + one `make docs` target. `make docs` serves `docs/user/` on `http://localhost:3000`; docsify renders the markdown in place with grouped sidebar nav and full-text search. `docs/user/README.md` is the homepage. No Go code changes, no copies of any doc, nothing generated, no docs moved.

## §C constraints
- C1: zero new deps in `go.mod`; zero vendored JS. docsify loads from the jsdelivr CDN at page load. The repo owns exactly two static files (`docs/user/index.html`, `docs/user/_sidebar.md`) and one Make target.
- C2: CLAUDE.md one rule intact — no Python or Node maintained here. `make docs` shells out to `npx docsify-cli serve docs/user` the same way `unmute dev` shells out to Python: a tool we invoke, never code we own.
- C3: markdown stays where it lives. docsify renders files in place — no `site/` output dir, no frontmatter added, no doc moves. Relative links inside `docs/user/` keep working under docsify hash routing. (Links that already point outside `docs/user/`, e.g. `../../SCHEMA.md`, resolve outside the served root and are not fixed — those targets are deliberately not in the site.)
- C4: localhost only. No deploy, no GitHub Pages, no `.nojekyll` — out of scope until someone asks.
- C5: guides = every `*.md` under `docs/user/` (start/, learn/, concepts/, reference/, targets/). Internal specs `docs/spec/*.md` and root `*.md` (README, SCHEMA, …) are NOT in the site — the serve root is `docs/user/`, above which docsify does not reach. Sidebar is hand-curated (grouped by the five sections), not auto-generated.

## §I surfaces
- I.index: `/docs/user/index.html` — `window.$docsify = { name: 'Unmute CLI', loadSidebar: true, auto2top: true, subMaxLevel: 2 }`; docsify v5 core CSS + JS + search plugin from `cdn.jsdelivr.net/npm/docsify@5`.
- I.sidebar: `/docs/user/_sidebar.md` — curated link tree grouped Start / Learn / Concepts / Reference / Targets; `README.md` is the docsify default homepage.
- I.make: `make docs` → `npx --yes docsify-cli serve docs/user --port 3000` (live reload included).
- I.url: `http://localhost:3000`.

## §V invariants
V1: `go test ./...`, `make build`, `make lint` unaffected — the site files are inert data under `docs/user/`, invisible to the Go toolchain.
V2: `index.html` pins docsify major version 5 via CDN URL; no minified JS committed to the repo.
V3: every path linked in `_sidebar.md` exists under `docs/user/` — no dead sidebar entries.
V4: every `*.md` under `docs/user/` (except `_sidebar.md`) appears in `_sidebar.md` — a guide missing from the sidebar is a spec violation, not a judgment call.
V5: the site never mutates the tree — serving is read-only; `git status` clean before and after `make docs`. `docs/spec/*.md` is never served.

## §T tasks
id|status|desc|cites
T1|x|add `/docs/user/index.html` — docsify v5 CDN bundle, `$docsify` config per I.index, search plugin|C1,C3,I.index,V2
T2|x|add `/docs/user/_sidebar.md` — curated tree over every `docs/user/**/*.md`, grouped by the five sections|C5,I.sidebar,V3,V4
T3|x|add `make docs` target (`npx --yes docsify-cli serve docs/user --port 3000`); one line in README how to run|C2,I.make,V1,V5

## §B bugs
id|date|cause|fix

# Data Model: Unmute User Docs on Mintlify

The "data" of a docs feature is its pages and the facts they carry. Five
entities, all file-shaped.

## Page

One `.mdx` file under `docs-site/`.

| Field | Rule |
|---|---|
| `title` (frontmatter) | Short, plain words. Required. |
| `description` (frontmatter) | One sentence, shown in search and nav. Required. |
| Body | Simple language, short sentences, no em or en dashes as punctuation. Every term explained on first use. Ends with where to go next. |
| Code blocks | Each one was actually run or validated (FR-005). YAML blocks pass `./bin/unmute validate` in a scratch package. |
| Target claims | A page claims exactly the targets its anchor example declares. |

States: planned → drafted → verified (all claims checked, snippets run) → shipped (passes mint validate and broken-links). Only verified pages enter the final report as done.

## Navigation group

One entry in `docs.json` `navigation.groups`. Ordered; the order is the story
arc and is a contract: no page may assume a concept taught by a later page.
See `contracts/navigation.md` for the full tree.

## Example anchor

A package under `examples/`. Relationships: each learning-narrative page and
each telephony/transfer page anchors to exactly one example; an example may
anchor several pages.

| Field | Source of truth |
|---|---|
| Declared target set | The example's `targets.yaml`. Authoritative; never changed by this feature. |
| Validates / compiles | `./bin/unmute validate` and `compile`, re-run during writing. Baseline 2026-08-14: all ten pass. |
| README | Required before the page ships. Missing today: simple-prompt, multi-task, task-groups, subagents. |

## CLI surface

The four commands plus `completion`, with flags, defaults, and argument
shapes. Source of truth: `internal/cli/*.go` confirmed by `./bin/unmute <cmd>
--help` output captured in research.md R3. Each documented flag must match
name, default, and meaning exactly (SC-004).

## Provider capability row

One row per (target, role, vendor), where role is STT (listen), TTS (speak),
LLM (reason), or VAD / turn detection. Source of truth:
`internal/target/catalog_pipecat.go` and `catalog_livekit.go`. Rules: SLNG
first in every list; exact model names only for SLNG via
https://docs.slng.ai/models; other vendors' model IDs are described as passed
through. Guarded by a new Go agreement test (research.md R7).

## Discrepancy report

The final deliverable to maintainers, one markdown file in this feature
directory (`report.md`, written at the end of implementation).

| Section | Contents |
|---|---|
| Pages | Every page written, with its anchor example and verification status. |
| Discrepancies | Every code-versus-docs disagreement: code location, doc location, what each says. None resolved silently. |
| Unverified claims | Any claim that could not be checked, and why. |
| Rule impacts | The "three places become four" note (research.md R9) and anything else the maintainers must decide. |

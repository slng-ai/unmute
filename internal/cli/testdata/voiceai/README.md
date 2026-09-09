# `voiceai --json` fixtures

Captured from `voiceai` 0.1.15 on 2026-08-31 by running each command against a
live organisation, then reduced and de-identified. The **shapes** are real; the
organisation id, the workspace name, the `LEGACY_*` hash and every phone number
are not. Numbers use Ofcom's 07700 900xxx drama range, which is permanently
unallocated, so a reader who dials one reaches nobody.

One thing these captures do **not** prove, and it matters: a curated capability
is not guaranteed to exist. Two organisations were read the same afternoon. One
listed `end_call`, `transfer_call`, `voicemail_detection`, `current_datetime`,
`user_phone_number` and `send_sms`; the other listed only `send_sms`, and
`voiceai tool get end_call` reported it absent. So `tool_list.json` records what
a provisioned organisation looks like, not a floor every organisation meets. The
preflight is right to report an absent builtin rather than assume one, because
the push cannot resolve it either.

Following the precedent in `deploy_test.go`, these live here rather than being
hand-written, because the fields that matter are the ones the tool actually
emits. A struct that guesses at a shape reads zero out of a real account and
reports success.

| File | Command | Notes |
|---|---|---|
| `whoami.json` | `voiceai whoami --json` | `account.name` and `account.email` really are null on a key-only profile |
| `secret_list.json` | `voiceai secret list --json` | reduced from 44 entries. `HALF_MADE_TOKEN` is **synthesised**: every entry on the captured account had a value, and `has_value: false` is a state the code must handle |
| `tool_list.json` | `voiceai tool list --json` | all seven, unmodified. Six are curated capabilities, which is the fact that makes a `builtin:` check positive |
| `mcp_list_empty.json` | `voiceai mcp list --json` | the captured account has no MCP servers, so this is the real response |
| `mcp_list.json` | `voiceai mcp list --json` | **synthesised**, because the captured account returns `[]` and the healthy and unhealthy branches both need cover |
| `mcp_tools.json` | `voiceai mcp tools <server> --json` | **synthesised**, for the same reason |
| `trunks_list.json` | `voiceai trunks list --json` | reduced from 6. `broken_inbound` is **synthesised**: no captured trunk was unusable, and the unusable branch needs cover |
| `tool_get_code.json` | `voiceai tool get check_order --json` | a `code` tool. `code_src` really does carry a shim unmute itself wrote on an earlier push, and that shim is why the mirrored module needs a lint header: `from pydantic import BaseModel` sits at line 24, after code, which is `E402` |
| `tool_get_api_request.json` | `voiceai tool get search_places_text --json` | an `api_request` tool. `declared_secrets` is **empty** while `config.auth.secret_name` names one, which is the field a mirror has to read for this kind |
| `tool_get_curated.json` | `voiceai tool get current_datetime --json` | a curated capability. `source: curated`, `code_src: null` and `arg_schema: {}`, which is why a curated name cannot be mirrored and is refused with `builtin:` as the answer |

The three `tool_get_*` captures were taken from `voiceai` 0.1.16 on 2026-09-03,
against the second of the two organisations this checkout can reach. Only `id`,
`organisation_id` and the `api_request` tool's `url` were changed; every other
byte is the response. They cover the three answers a hosted-tool fetch can get,
and each carries one field a hand-written struct would get wrong:

- a `code` tool's definition is in `code_src` and its schema in `arg_schema`,
  which the platform derived by introspecting the module, so nothing here has to
  parse Python;
- an `api_request` tool's credential is `config.auth.secret_name` and **not**
  `declared_secrets`, which is empty on a tool that plainly needs a token;
- a curated capability answers with the same 19 keys as a real tool and is
  distinguishable only by `source`.

## Spec 007 fixtures: published versions, scoped identities and MCP records

Added 2026-09-08 for the name-based hosted reference. These are **synthesised**,
and they have to be: the commands that would produce them do not exist in
`voiceai` 0.1.16. `contracts/voiceai-deployment.md` in that feature's spec
directory is what they are shaped from, so they are a written contract's fixture
rather than a capture, and the released CLI is what settles the real shape.

**Corrected 2026-09-08 against the backend's own schema**, after a review with
real account reads found two of these shapes wrong. An attachment's trigger
lives under `system.triggers`, not in a top-level `trigger`, and there are
settings beside it a preview has to name; the shapes here now follow
`ToolAttachment` and `McpAttachment` in the platform's `shared_tool_contract`.
The vault listing carries no `kind`, so `secret_list.json` keeps that field only
where it is testing the mismatch branch, and the shared stubs in `deploy_test.go`
answer without it. A stub that carried a field the platform never sends is how a
defect survived a green suite, so a field here is either something the platform
sends or a branch under test, and never both by accident.

Every id is a repeated-digit UUID so a reader can tell one from another at a
glance, and no fixture carries a secret value, an auth token or a real URL: the
hosts are all under `.invalid`, which is reserved and resolves nowhere.

| File | Stands for | Why it exists |
|---|---|---|
| `tool_list_scoped.json` | `voiceai tool list --json`, with `id` and `scope` read | Four cases one listing has to distinguish: a name held at two scopes (`end_call`), a name held twice at the **same** scope (`ambiguous_tool`, which nothing local can choose between), a tool with `latest_version: null` because it was never published, and the two ordinary tools the example references |
| `tool_version_published.json` | `tool get TOOL_ID --version 3 --json` | The immutable envelope: `tool_id`, `version_number`, `content_hash`, `published_at` and `snapshot_json`. The published parameters are `snapshot_json.argument_schema`, which is a different field from the mutable record's `arg_schema` |
| `tool_version_published_v4.json` | the same tool, one version later | The update-preview case: a changed description, a renamed and newly required parameter, and a second declared secret. A schema comparison can say these differ and cannot say the tool behaves the same |
| `tool_get_draft_divergent.json` | `tool get --json` reading the mutable draft | The reason the version getter is a prerequisite at all. Its `arg_schema` renames `query` to `search` and its `declared_secrets` names an entry no published version needs, so a binding checked against it is checked against a contract nothing serves. `is_current_version: false` and `schema_stale: true` are the fields that say so |
| `mcp_get_healthy.json` | `mcp get SERVER_ID --id --json` | A usable record: `capability_status: healthy`, a fresh `capability_observed_at`, `truncated: false`, and a `schema_hash` per tool, which is the value a resolved push carries |
| `mcp_get_stale.json` | the same server, a week old | Fresh-looking status with an observation past its `next_refresh_at`. A timestamp alone cannot prove attachability, which is why status and freshness are read together |
| `mcp_get_failed.json` | a server whose last probe failed | `capability_status: connection_failed` with the reason. A failed discovery cannot prove a tool absent, so this is an unchecked blocker and not a positive answer |
| `mcp_get_truncated.json` | a server with more tools than one probe read | `truncated: true` with one tool listed. Same rule: an incomplete record cannot establish that a selected tool is missing |
| `mcp_get_changed_hash.json` | the same server after a real refresh | One tool's `schema_hash` has changed and a third tool has appeared. A guarded push refuses the stale hash rather than attaching an unchecked snapshot, and the new tool is not attached because the package did not select it |
| `agent_get_current.json` | `agents get AGENT_ID --json` | The replacement-preview baseline: an attachment at an older version, one **edited in the dashboard** with a description, a `system` invocation, a `call_start` trigger under `system.triggers` (which the package cannot declare) and a pre-action message under `execution_policy` in the platform's own segmented shape carrying the sentence the example's own `announce:` declares (so a phantom removal shows up here), one pointing at a tool the package no longer names, and one MCP selection |
| `secret_list_gaps.json` | `voiceai secret list --json` | The three vault states a published contract's requirements meet: present and populated, present and empty, and present under the wrong kind |

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

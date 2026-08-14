# Contract: Update Map

Two halves: the merge blast radius on existing pages, and the content contract
for each new page. Sources of truth at implementation time: the merged working
tree (`internal/spec`, `internal/ir`, `internal/target/table.go`,
`internal/target/prebuilt.go`, the catalogs), merged `docs/SCHEMA.md` N40 and
N41, and the two landed feature specs' `contracts/` directories. The brief's
lists and this file are maps; the fresh grep after the merge wins (FR-030).

## Half one: existing pages the merge touches

Pre-merge grep baseline (research R16), ten pages. For each: what to check,
how the fix is verified.

| Page | What the merge changes there | Verified by |
|---|---|---|
| `reference/targets-yaml` | target field list shrinks to `provider`, `version`, `pins`, `sdk_language`, `connection`, `deployment_region`, `models`; moved fields refused with the new home named | struct read + triggered refusal per moved field |
| `reference/agent-yaml` | `destinations:` arrives at top level (env var names only); `secrets:` rule changes; channel `kind: telephony` stays | struct read + literal-value refusal triggered + scratch validate |
| `reference/secrets` | new rule: every author-written env name is listed; platform-supplied names are not | merged environment contract + a real build's `.env.example` |
| `reference/cli/compile` | any shown compile output or `targets.yaml` snippet matches the merged shape | re-run and re-capture |
| `targets/overview` | no route fields on the target; the target names a connection | struct read + scratch validate |
| `telephony/overview` | the connection owns the route; three shapes named and linked to the new reference page | merged TELEPHONY.md cross-read + scratch validate |
| `telephony/outbound-calls` | `destinations:` in agent.yaml; outbound-reminder's two connection files by their new names | example re-validate + re-compile |
| `transfers/overview` | route fields move; gated transfer refusal now names the connection and its transport | triggered refusal |
| `transfers/livekit` | connection shape in the example snippets | example re-validate + captured output re-run |
| `transfers/pipecat-daily` | carrier-less `daily-sip` connection shape | example re-validate |
| `transfers/pipecat-twilio` | full-route connection shape; refusals re-run | example re-validate + triggered refusal |
| `start/quickstart` | scaffold changed: no `transport:` line, new `connections/phone.yaml` | fresh `unmute init` transcript, re-captured (FR-032) |

(`transfers/livekit` through `start/quickstart` extend the grep's ten with the
two the brief names via the scaffold and checklist; the post-merge grep decides
the final list.)

Also checked, though not in the grep: any page showing a scaffolded
`targets.yaml`, and `telephony/first-phone-call` (its example may have gained a
connection file).

Second-round additions to half one: `telephony/overview` also loses its
`dev --telephony` walkthrough to `dev/telephony` (link left behind);
`deploy/going-live`, `telephony/first-phone-call`, and `reference/secrets`
swap the branded invalid env-name example for a neutral one (FR-044); and
every page linking to `reference/providers` or `telephony/webhooks-and-tunnels`
is re-pointed when those pages retire or move.

## Half two: content contracts for new pages

### build/tools/overview

What a tool is. The six execution blocks by name, with `client` and
`provider_hosted` marked gated per the capability table. A routing table
sending the reader to the right sub page. `interruption` and `effect` taught
once. Assignment: the tool name in an agent's or task's `tools:` list is the
visibility scope. This page's block set is held by the new agreement test
(research R17).

### build/tools/webhook

The everyday case. `url_env`, `path` with `{{variable}}` rendering, `auth`
(bearer and api_key), `input` and `output` schemas, `inject` and why the model
cannot see or overwrite it. Anchor: `examples/salon-support` or
`examples/outbound-reminder`, picked while writing.

### build/tools/python

The `local:` block and its handler. Where handler code lives in the generated
project, what it receives, what it returns, `os.environ` as the credential
seam. Anchor: `examples/outbound-reminder` (`tools/cancel_appointment.py`).
Any Python shown is run and checked with `ruff` and `ty`.

### build/tools/mcp

SCHEMA N40, anchored on `examples/mcp-example` (both targets; LiveKit needs
`sdk_language: python`, pinned 1.6.4). The `mcp:` block shape. The no-contract
rule with the reason (the server announces its own tools at run time), each
illegal field refused individually with file and line, one refusal captured.
`tools:` as an unchecked selection filter. Two files may share a `url_env`.
Scope: Pipecat cannot scope an MCP source to a task (agent-level only, the
table's own words); assignment otherwise unchanged. Every env name reaches
`.env.example`, the startup check, and the compile report, shown from a real
compile.

### build/tools/prebuilt

The `builtin:` block. The registry is closed and holds exactly one entry,
`end_call`, effect fixed to `ends_conversation`, default description that the
author's text is added on top of. Said plainly, no implied catalog. Re-read
the registry at implementation in case the merge changed it.

### build/orchestration/overview

One screen. Names the shapes (handoffs, tasks, task groups) and routes onward.
Names, does not teach: no mechanism explained here.

### build/orchestration/handoffs

The two-agents content reframed: one agent hands the caller to another, the
handoff does not come back, `context` carries history and variables, tool
lists are the guardrail.

### build/orchestration/tasks

Delegation, typed results, `assign` into a variable, what the agent cannot do
while delegated.

### build/orchestration/task-groups

Shared context across tasks. Keeps the real captured LiveKit warning the
current page holds (re-run on the merged tree).

### build/orchestration/choosing-a-structure

The capstone, enhanced. Symptom column paired with the shape that fixes it.
The cost of each shape: a handoff is one way, a task returns, a group shares
context. "Start with more tools before you add a second agent." The delegate
versus transfer distinction the current page already draws.

### dev/overview (updated) and the Development lifecycle group

The group is renamed "Development lifecycle" (label only). The overview
teaches the loop: change the package, `unmute dev`, talk to the agent, read
the logs, iterate. Log-file locations are stated per mode and verified by
running each mode on the merged tree (research R23: `telephony.log` in the
build output directory for phone routes, the compose log path for web dev,
no log file in console mode). `--verbose` is explained as "follow on stderr
too".

### dev/telephony (new; content moved from telephony/overview)

The local phone-call run, explained extremely well: what `dev --telephony`
does and how it happens (the tunnel, the automatic webhook configuration,
the local process, the undo on exit), plus `--no-webhook`, `--public-url`,
and `--to` for outbound tests. Every step re-verified on the merged tree.
`telephony/overview` keeps the concepts and routes and links here for the
run.

### dev/webhooks-and-tunnels (moved from telephony/)

Content as it is, re-checked against the merged code; inbound links swept.

### telephony/twilio (new)

Per research R25: buying a number, finding the account SID and auth token in
the Twilio console, and which connection env name each value fills, in
separate sections for the Pipecat routes and the LiveKit route as this
repo's code defines them. Console steps verified against current Twilio docs
with a fetch date. No carrier-provisioning claims.
`docs/user/learn/twilio-walkthrough.md` is a starting point, never copy to
paste.

### models/stt, models/tts, models/llm, models/turn-detection (new)

Per research R21: vendor integrations per target per role from the merged
catalogs, SLNG first, model names only for SLNG (the ids proven in this
repo), https://docs.slng.ai/models linked for the rest, pass-through stated
for other vendors. The retargeted agreement test holds all four pages. The
turn-detection page states each target's actual mechanism instead of
blending VAD and turn models.

### models/optimization (new)

Per research R22: a scaffolded project uses SLNG models by design, and they
run on the SLNG Execution Layer. Covers the STT Performance Layer (stating
SLNG's own Private Beta status) and TTS Path Optimization only. The Context
Router is never mentioned. Every claim attributed to the SLNG docs with a
link and a fetch date.

### deploy/livekit-cloud and deploy/pipecat-cloud (new)

Per research R24: each page walks the generated project to its platform's
cloud with the platform CLI, written from a real compile's generated README
(`lk cloud auth`, `lk agent create --secrets-file .env`, `lk agent deploy`;
`pipecat cloud secrets set <set> --file .env`, `pipecat cloud deploy`),
re-checked against the platform's current official docs with fetch dates.
Steps not executed here ship attributed and join the report's unverified
list. The reader's generated README stays the runbook of record; these pages
orient and route to it.

### deploy/going-live (updated)

Stays the shared pre-launch checklist. The environment-variable naming rule
keeps its substance but swaps the provider-branded example for a neutral
invalid name the validator really rejects (FR-044); the same fix lands on
`telephony/first-phone-call` and `reference/secrets`.

### reference/connections-yaml

Sibling of the targets reference. The three valid shapes (full route,
receive-only on the one route the contract allows, carrier-less `daily-sip`),
each backed by a validated package. `kind:` not written in a connection file.
One target names one connection; differing transports need two files (the
outbound-reminder split as the worked example). `docs/user/reference/connections.md`
is a starting point, never copy to paste. If the merged loader accepts a
written `kind:` despite the contract, that is a discrepancy for the report,
not a fact for the page.

# Contract: Navigation and Page Inventory

This is the site's public contract: the sidebar order is the story arc, and no
page may assume a concept a later page teaches. Page slugs are final once
approved (they become URLs).

## docs.json navigation (draft)

```json
{
  "$schema": "https://mintlify.com/docs.json",
  "theme": "mint",
  "name": "Unmute by SLNG//",
  "logo": { "light": "/logo/slng.png", "dark": "/logo/slng.png" },
  "favicon": "/logo/slng.png",
  "colors": { "primary": "#F2D852" },
  "navigation": {
    "groups": [
      {
        "group": "Get started",
        "pages": [
          "index",
          "start/installation",
          "start/quickstart",
          "start/how-unmute-works"
        ]
      },
      {
        "group": "Build the agent",
        "pages": [
          "build/one-agent",
          "build/tools",
          "build/variables",
          "build/two-agents",
          "build/tasks",
          "build/task-groups",
          "build/choosing-a-structure"
        ]
      },
      {
        "group": "Run it locally",
        "pages": [
          "dev/overview",
          "dev/console"
        ]
      },
      {
        "group": "Telephony",
        "pages": [
          "telephony/overview",
          "telephony/first-phone-call",
          "telephony/outbound-calls",
          "telephony/webhooks-and-tunnels"
        ]
      },
      {
        "group": "Transfers",
        "pages": [
          "transfers/overview",
          "transfers/livekit",
          "transfers/pipecat-daily",
          "transfers/pipecat-twilio"
        ]
      },
      {
        "group": "Targets",
        "pages": [
          "targets/overview",
          "targets/pipecat",
          "targets/livekit"
        ]
      },
      {
        "group": "Deployment",
        "pages": [
          "deploy/going-live"
        ]
      },
      {
        "group": "Reference",
        "pages": [
          {
            "group": "CLI",
            "pages": [
              "reference/cli/overview",
              "reference/cli/init",
              "reference/cli/validate",
              "reference/cli/compile",
              "reference/cli/dev"
            ]
          },
          "reference/agent-yaml",
          "reference/targets-yaml",
          "reference/providers",
          "reference/variables",
          "reference/secrets"
        ]
      }
    ]
  }
}
```

Notes on the draft: branding is "Unmute by SLNG//" (user decision,
2026-08-14). The logo ships from `images/Logo_SLNG.png`, copied to
`docs-site/logo/slng.png` so the site is self-contained; it serves as navbar
logo and favicon source. `colors.primary` is sampled from the logo's yellow
and gets tuned against docs.slng.ai during implementation (a yellow this
light may need a darker variant for link text contrast; `colors.light` /
`colors.dark` exist for that). No tabs in v1; the site is one sidebar telling
one story. `theme`, `name`, `colors.primary`, `navigation` are the four
required docs.json fields (verified against current Mintlify docs).

## Page inventory

Anchor = the example the page is grounded in. "Teaches" is the new concept a
page may introduce; a page may use anything taught above it and nothing below
it. Anchors marked (confirm) get verified against the example's YAML when the
page is written; if the example does not demonstrate the concept, the gap goes
in the report.

| Page | Job | Anchor | Teaches |
|---|---|---|---|
| index | Why Unmute exists; a taste of the spec; who it is for | none (spec taste validated in scratch) | the problem, "one spec, many runtimes" |
| start/installation | Get the binary | none | install |
| start/quickstart | init → talking agent in the browser, standalone | scaffold from `unmute init` | init, dev (browser), the package layout |
| start/how-unmute-works | load → build → validate → generate, in reader words | none | validate, compile, build/<target> |
| build/one-agent | Smallest real agent: prompt + models | simple-prompt | agent.yaml, entry_agent, models, secrets |
| build/tools | Let the agent do something | simple-prompt (confirmed 2026-08-14) | tools |
| build/variables | Personalize a session | salon-support (confirmed 2026-08-14) | variables, --var, inject |
| build/two-agents | A second agent and handing off | subagents (confirmed 2026-08-14) | agents map, agent_transfer |
| build/tasks | Typed steps with results | multi-task | tasks, delegate |
| build/task-groups | Group tasks that belong together | task-groups | task groups |
| build/choosing-a-structure | Which split to reach for | all four structural examples | comparing tools, tasks, agents |
| dev/overview | The dev loop in depth | any built earlier | --port, --bot-port, --no-open, --verbose, --target and TTY behavior, logs |
| dev/console | Terminal instead of browser | same | --console and what it ignores |
| telephony/overview | Calls in and out; what --telephony automates and undoes | none (routes table from code) | routes, tunnel, webhook auto-config |
| telephony/first-phone-call | Real inbound call, zero config | twilio-telephony-hello | --telephony end to end |
| telephony/outbound-calls | Agent dials out | outbound-reminder | --to, dispatch payload vs --var |
| telephony/webhooks-and-tunnels | Take control of the public URL | twilio-telephony-hello | --public-url, --no-webhook |
| transfers/overview | Warm vs cold transfer to a human | none | transfer concepts |
| transfers/livekit | LiveKit route | livekit-human-transfer (LiveKit only) | per-route specifics |
| transfers/pipecat-daily | Pipecat Daily route | pipecat-human-transfer-daily (Pipecat only) | per-route specifics |
| transfers/pipecat-twilio | Pipecat Twilio route | pipecat-human-transfer-twilio (Pipecat only) | per-route specifics |
| targets/overview | What a target is; targets.yaml | none | targets.yaml basics |
| targets/pipecat | The Pipecat artifact | any Pipecat build | bot.py project shape |
| targets/livekit | The LiveKit artifact | any LiveKit build | agent.py project shape |
| deploy/going-live | From build/<target> to production | generated runbook README | .env.example, Docker image, runbook |
| reference/cli/overview | Command tree, exit codes, warnings to stderr, completion, --version | binary help | none (lookup) |
| reference/cli/init | Full init reference | binary help | none |
| reference/cli/validate | Full validate reference | binary help | none |
| reference/cli/compile | Full compile reference | binary help | none |
| reference/cli/dev | Full dev reference, every flag | binary help | none |
| reference/agent-yaml | Every agent.yaml block | internal/spec structs | none |
| reference/targets-yaml | Every targets.yaml field | internal/spec structs | none |
| reference/providers | Vendors per role per target, SLNG first, SLNG models linked | catalog_*.go + https://docs.slng.ai/models | none |
| reference/variables | Variable types, sources, templating | internal/spec + docs/SCHEMA.md | none |
| reference/secrets | Env-name-only rule, .env.example flow | internal/spec + docs/SCHEMA.md | none |

35 pages. The only content sources allowed per page are the ones named in its
row plus the internal docs listed in FR-014, always re-verified against code.

Amended 2026-08-14 (see `verification-log.md` section 2): the planned
`build/subagents` page named a concept the code does not have, and the example
called `subagents` is the two-agents-with-handoffs package that page four
already teaches. The slot became `build/choosing-a-structure`. Page count
unchanged.

## Hard rules the inventory encodes

- Only Pipecat and LiveKit Agents appear as targets anywhere (FR-011).
- Transfer pages present single-target examples as single-target (FR-022).
- Telephony pages come after the narrative; no narrative page mentions phones.
- Every reference/cli page shows real help output from the built binary.
- SLNG first in every provider list; only SLNG gets exact model names, linked to https://docs.slng.ai/models (FR-023).

---

## Amendment, 2026-08-14: 35 pages become 49

Recorded after feature `009-mintlify-docs-extension` (planned in
`specs/009-mintlify-docs-extension/contracts/navigation.md`, verified in this
directory's `verification-log.md` addendum). Two causes: `origin/pre-release-v1`
landed MCP tool sources (SCHEMA N40) and moved the phone route into the
connection file (SCHEMA N41), and the maintainer asked for four things after
reading the site.

### Groups, in sidebar order

Get started (4), Build the agent (12), Development lifecycle (4), Telephony (4),
Transfers (4), Targets (3), Models (5), Deployment (3), Reference (10). Nine
groups, 49 pages. The nesting is the object-in-pages-array form Reference already
used for CLI.

### Moves, new pages, retirements

| Old path | New path | Why |
|---|---|---|
| `build/one-agent` | `build/your-first-agent` | same content, better name |
| `build/tools` | retired into `build/tools/{overview,webhook}` | one page per way a tool runs |
| (new) | `build/tools/{overview,webhook,python,mcp,prebuilt}` | one page per execution block the `Tool` struct has; `mcp` is N40, anchored on `examples/mcp-example` |
| `build/two-agents` | `build/orchestration/handoffs` | reframed as a concept, not a count |
| `build/tasks`, `build/task-groups`, `build/choosing-a-structure` | `build/orchestration/*` | grouped; the capstone became a decision aid |
| (new) | `build/orchestration/overview` | names the three shapes, routes onward |
| (group rename) | "Run it locally" becomes "Development lifecycle" | label only, slugs stay `dev/*` |
| (new) | `dev/telephony` | the local phone-call run, moved out of `telephony/overview` |
| `telephony/webhooks-and-tunnels` | `dev/webhooks-and-tunnels` | local dev mechanics belong to the lifecycle group |
| (new) | `telephony/twilio` | which Twilio console value fills which connection env name, per route |
| (new) | `models/{stt,tts,llm,turn-detection,optimization}` | catalog-derived role pages plus the SLNG Execution Layer |
| `reference/providers` | retired into `models/*` | its agreement test retargeted in the same change |
| (new) | `deploy/livekit-cloud`, `deploy/pipecat-cloud` | per-platform go-live guides from the emitted runbooks |
| (new) | `reference/connections-yaml` | the connection file as the whole phone route (N41) |

No redirects: the site has never been deployed, and `mint broken-links` holds the
result after every move.

### Rules this amendment adds

- A tools page exists only if the `Tool` struct has that execution block, held by
  `internal/spec/tools_docsite_test.go`.
- The Models role pages carry the catalogue's vendor lists, held by the
  retargeted `internal/target/providers_docsite_test.go`; `models/turn-detection`
  carries no vendor list, because the `turn` role has no catalogue entries, and
  that too is held by a test.
- Execution-layer facts are SLNG's: attributed, linked, and dated, never asserted
  as measured here. The Context Router stage is excluded from the site entirely
  (maintainer decision, 2026-08-14).
- No provider-branded environment variable name is used as an invalid example
  (the neutral `2FACTOR_*` names replace `11LABS_API_KEY`).
- Gate unchanged in form: the count of `.mdx` files under `docs-site/` equals the
  count of page entries in `docs.json`, now at 49.

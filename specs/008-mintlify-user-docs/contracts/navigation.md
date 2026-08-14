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

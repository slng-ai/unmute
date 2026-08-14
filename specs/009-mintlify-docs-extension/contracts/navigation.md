# Contract: Target Navigation (extension of 008 contracts/navigation.md)

Updated 2026-08-14 after two clarification rounds (Models and Execution
Layer; Development lifecycle, go-live guides, Twilio guide). This is the
target state of `docs-site/docs.json` after this extension. Once implemented,
a dated amendment recording this goes into
`specs/008-mintlify-user-docs/contracts/navigation.md`, which remains the
site's living navigation contract.

## The full tree (groups in sidebar order)

```json
{
  "navigation": {
    "groups": [
      { "group": "Get started",
        "pages": ["index", "start/installation", "start/quickstart", "start/how-unmute-works"] },
      { "group": "Build the agent",
        "pages": [
          "build/your-first-agent",
          { "group": "Tools",
            "pages": ["build/tools/overview", "build/tools/webhook", "build/tools/python", "build/tools/mcp", "build/tools/prebuilt"] },
          "build/variables",
          { "group": "Orchestration",
            "pages": ["build/orchestration/overview", "build/orchestration/handoffs", "build/orchestration/tasks", "build/orchestration/task-groups", "build/orchestration/choosing-a-structure"] }
        ] },
      { "group": "Development lifecycle",
        "pages": ["dev/overview", "dev/console", "dev/telephony", "dev/webhooks-and-tunnels"] },
      { "group": "Telephony",
        "pages": ["telephony/overview", "telephony/first-phone-call", "telephony/outbound-calls", "telephony/twilio"] },
      { "group": "Transfers",
        "pages": ["transfers/overview", "transfers/livekit", "transfers/pipecat-daily", "transfers/pipecat-twilio"] },
      { "group": "Targets",
        "pages": ["targets/overview", "targets/pipecat", "targets/livekit"] },
      { "group": "Models",
        "pages": ["models/stt", "models/tts", "models/llm", "models/turn-detection", "models/optimization"] },
      { "group": "Deployment",
        "pages": ["deploy/livekit-cloud", "deploy/pipecat-cloud", "deploy/going-live"] },
      { "group": "Reference",
        "pages": [
          { "group": "CLI",
            "pages": ["reference/cli/overview", "reference/cli/init", "reference/cli/validate", "reference/cli/compile", "reference/cli/dev"] },
          "reference/agent-yaml", "reference/targets-yaml", "reference/connections-yaml", "reference/variables", "reference/secrets"
        ] }
    ]
  }
}
```

The nesting pattern is the object-in-pages-array form Reference already uses
for CLI. Icons stay Lucide. Cards keep `<Columns cols={2}>`.

## Moves, new pages, retirements

| Old path | New path | Content |
|---|---|---|
| `build/one-agent` | `build/your-first-agent` | same content, better name |
| `build/tools` | retired | verified content seeds `tools/overview` and `tools/webhook` |
| (new) | `build/tools/{overview,webhook,python,mcp,prebuilt}` | Tools group (mcp is N40, anchored on examples/mcp-example) |
| `build/two-agents` | `build/orchestration/handoffs` | reframed as a concept |
| `build/tasks`, `build/task-groups`, `build/choosing-a-structure` | `build/orchestration/{tasks,task-groups,choosing-a-structure}` | moved; capstone enhanced into a decision aid |
| (new) | `build/orchestration/overview` | names the shapes, routes onward |
| (group rename) | Dev group label becomes "Development lifecycle" | slugs stay `dev/*` |
| (new) | `dev/telephony` | the local phone-call run, moved out of `telephony/overview` |
| `telephony/webhooks-and-tunnels` | `dev/webhooks-and-tunnels` | local dev mechanics belong to the lifecycle group |
| (new) | `telephony/twilio` | get your details from the Twilio console, per route, per target |
| (new) | `models/{stt,tts,llm,turn-detection,optimization}` | catalog-derived role pages + the Execution Layer story (STT and TTS stages only) |
| `reference/providers` | retired into `models/*` | agreement test retargets in the same change |
| (new) | `deploy/livekit-cloud`, `deploy/pipecat-cloud` | per-platform CLI go-live guides from the emitted runbooks |
| (new) | `reference/connections-yaml` | the connection file as the route (N41) |

No redirects (research R18). Every internal link to a moved or retired page
is updated; `mint broken-links` holds the result.

## Page count

| Group | Pages |
|---|---|
| Get started | 4 |
| Build the agent | 12 |
| Development lifecycle | 4 |
| Telephony | 4 |
| Transfers | 4 |
| Targets | 3 |
| Models | 5 |
| Deployment | 3 |
| Reference | 10 |
| **Total** | **49** |

Gate: the count of `.mdx` files under `docs-site/` equals the count of page
entries in `docs.json`, at 49.

## Adjustment clause

This shape is the maintainer's clarified intent and the default. If the
merged code lacks a concept a page assumes, the page is dropped or reshaped,
and the deviation plus its reason goes in the report addendum (FR-033, the D1
rule).
